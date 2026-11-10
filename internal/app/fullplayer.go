// 全屏播放器要用的两样业务数据：频谱那一行条高，和分集列表。
//
// 它们都**不是**状态，是「现算的」：频谱每一帧从解码器挂点上取（见
// `internal/service/audio/tap.go`），分集是一次网络往返的结果、缓存在 `App` 上。
//
// # 频谱为什么不缓存在 App 上
//
// 条子每帧都在动，缓存一份再等后台来更新，中间那一帧画出来的是**上一次**的
// 频谱。而频谱晚一帧看不出来、晚半秒就看得出来（鼓点）。所以每一帧现取，
// 取的是「此刻正在放的那一帧」对应的那一段（`Player.AudibleFrame`）。
//
// 复用的是同一块切片：频谱是每帧画的，每帧 `make` 一次是白烧。

package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/audio"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// SpectrumBars 返回当前正在放的那一帧的频谱条高度（0~1 的比例，barCount 个）。
//
// 返回的是**复用**的那一块切片：调用方只读，下一帧就被覆盖。要留着就得自己
// 拷走。
//
// 没有歌在放、或者刚 seek 完还没解出那一段时，返回的是一排 0——画出来是
// **什么都没有**（每一格是 0 就不画那一根），而不是一个不知道什么时候的
// 旧频谱。
func (a *App) SpectrumBars(barCount int, heightDip float32) []float32 {
	if barCount <= 0 {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.specBars) < barCount {
		a.specBars = make([]float32, barCount)
	}
	dst := a.specBars[:barCount]
	if a.player == nil {
		clear(dst)
		return dst
	}
	return a.player.Bars(a.player.AudibleFrame(), barCount, heightDip, dst)
}

// SpectrumSource 是「谁在画频谱」的一个来源：满屏播放器、刘海播放器各
// 是一格，位掩码的**第 src 位**就是它那一票。
type SpectrumSource int

const (
	// SpectrumFull 是满屏播放器那条频谱。
	SpectrumFull SpectrumSource = iota
	// SpectrumNotch 是刘海展开卡上那五根柱。
	SpectrumNotch
)

// SetSpectrumWantedFor 记「这个来源这一帧真的在画频谱」：src 那一位置 1
// 是「要」、清 0 是「不要」，**所有来源都不要**才把 FFT 停掉。
//
// # 为什么按来源记账
//
// 「界面在画频谱」这个事实按**窗**分：满屏外壳和刘海外壳是两个 Shell、
// 各自每帧报自己那一份。共用一个原子位的话，两窗各写各的、谁后写谁赢
// ——满屏在画、刘海暂停，刘海那一句 `false` 就把 FFT 关了；反过来全屏
// 不可见时它每帧的 `false` 又会把刘海要的 FFT 关掉。
//
// 判定「在不在画」在画的那一侧做（`fullplayer_spectrum.go` /
// `notch.go`）：那里顺手就有「偏好开着没有、窗口看得见没有、在放没有」
// 三个数。App 只负责把「有没有人要」翻译成「FFT 跑不跑」。
//
// 可以从任意 goroutine 调：本身只动一个原子位和一把短锁。
func (a *App) SetSpectrumWantedFor(src SpectrumSource, wanted bool) {
	bit := int32(1) << uint(src)
	for {
		old := a.spectrumWanted.Load()
		next := old
		if wanted {
			next |= bit
		} else {
			next &^= bit
		}
		if next == old || a.spectrumWanted.CompareAndSwap(old, next) {
			if next == old {
				return
			}
			break
		}
	}
	any := a.spectrumWanted.Load() != 0
	a.mu.Lock()
	p := a.player
	show := a.fullPlayerPrefsValueLocked().ShowSpectrum
	a.mu.Unlock()
	if p != nil {
		p.SetSpectrumEnabled(any && show)
	}
}

// SpectrumWantedFor 报「这个来源此刻有没有报『我在画』」。
//
// 界面那一层每帧照它画没画频谱报账，而「它这一帧还走没走到那句
// `SetSpectrumWantedFor`」本身就是 bug 的高发区（卡收起来、歌没了、窗口
// 失焦都让它不再被调）——测试拿这一位钉「没人画了位就是 0」。
func (a *App) SpectrumWantedFor(src SpectrumSource) bool {
	return a.spectrumWanted.Load()&(int32(1)<<uint(src)) != 0
}

// syncSpectrumLocked 把「FFT 现在该不该跑」重算一遍并推给手上的播放器。
// 须持 a.mu。偏好改了、换新播放器了、界面说有人/没人画了，都归这里统一判。
func (a *App) syncSpectrumLocked(p *audio.Player) {
	if p != nil {
		p.SetSpectrumEnabled(a.spectrumWanted.Load() != 0 && a.fullPlayerPrefsValueLocked().ShowSpectrum)
	}
}

// ---- 分集 ----

// Parts 返回当前这一首的分集列表，顺手把没取过的那一次取回来。
//
// **返回的是缓存**：第一次调的那一趟还没回来时给的是 nil，界面上是「正在问」。
// 问回来了会叫醒界面（铁律三），所以界面每一帧调一次就行，不要自己记「问过了
// 没有」——那份状态在这一层，换一首歌就该忘掉。
//
// 只有 B 站的 mv 才有分集。本地音乐和音乐库那条路一律给 nil，界面上入口也就不摆。
func (a *App) Parts() []bili.VideoPart {
	it, ok := a.CurrentItem()
	if !ok || !it.isOnline() || it.Bvid == "" {
		return nil
	}
	a.ensureParts(it.Bvid)
	return a.partsSnapshot()
}

// CurrentItem 是队列当前项。界面层看「分集入口在不在」用的就是它。
func (a *App) CurrentItem() (Item, bool) {
	if a.queue == nil {
		return Item{}, false
	}
	return a.queue.Current()
}

// partsRetryAfter 是分集问失败之后隔多久再问一次。
//
// 15 秒是「网络抖动能缓过来、而用户还在盯着这个窗口」的量级：界面每一帧都调
// `Parts()`，所以这个数同时也是**重试的上限频率**。
const partsRetryAfter = 15 * time.Second

// PartsWorthAsking 报「当前这一首有没有分集可问」。
//
// 判据是「这是一个 B 站的 mv」，**不是**「已经问到多于一条」：Biu 那边
// `playItem.hasMultiPart` 是播放项自带的字段，我们没有——要问一次才知道。
// 所以在问到之前（正在问 / 问失败）把手也得摆着，否则那句「问不出分集：…」
// 在真机上永远没有机会显示（面板从哪儿进去？）。
func (a *App) PartsWorthAsking() bool {
	it, ok := a.CurrentItem()
	return ok && it.isOnline() && it.Bvid != ""
}

// partsSnapshot 返回分集列表的一份拷贝。
func (a *App) partsSnapshot() []bili.VideoPart {
	a.partsMu.Lock()
	defer a.partsMu.Unlock()
	if len(a.parts) == 0 {
		return nil
	}
	return append([]bili.VideoPart(nil), a.parts...)
}

// PartsErr 是最近一次取分集失败的原因。界面上拿它说「问不出分集」。
func (a *App) PartsErr() error {
	a.partsMu.Lock()
	defer a.partsMu.Unlock()
	return a.partsErr
}

// ensureParts 保证正在问/已经问过 bvid 的分集。
//
// 换稿就重置：`partsBvid` 是这份缓存属于谁，歌换了就得从头问。迟到的结果回来
// 发现 bvid 对不上直接丢（和在线流那边的代次是同一个道理）。
//
// 🔴 **失败之后要能再问**。原来失败和成功一样把 `partsLoaded` 置成 true，于是
// 一次 403 风控 / 一次网络抖动之后，这一首在本次播放期间**再也不会出现分集
// 入口**——而界面上还没有任何地方能说出「问不出分集」（那个把手只在
// `len(parts) > 1` 时才摆，面板进不去）。
//
// 现在：成功才 `partsLoaded`；失败只记下原因，`partsWanted` 放下，于是隔
// `partsRetryAfter` 之后界面下一帧调 `Parts()` 时会重新问一次。
// 界面上那一帧的差别是「问不出分集：…」和「正在问」，用户看得见自己可以再等。
func (a *App) ensureParts(bvid string) {
	a.partsMu.Lock()
	if a.partsBvid != bvid {
		a.partsBvid, a.parts, a.partsLoaded, a.partsWanted = bvid, nil, false, false
		a.partsErr, a.partsTriedAt = nil, time.Time{}
	}
	if cached := a.partsCache[bvid]; len(cached) != 0 {
		a.parts = append([]bili.VideoPart(nil), cached...)
		a.partsLoaded = true
		a.partsWanted = false
		a.partsMu.Unlock()
		a.adoptCurrentParts(bvid, cached)
		return
	}
	if a.partsLoaded || a.partsWanted {
		a.partsMu.Unlock()
		return
	}
	// 退避：每一帧都调 `Parts()`，不等一下的话失败一次就是每秒一发。
	if !a.partsTriedAt.IsZero() && time.Since(a.partsTriedAt) < partsRetryAfter {
		a.partsMu.Unlock()
		return
	}
	a.partsWanted = true
	a.partsTriedAt = time.Now()
	// 清掉上一次的失败原因：这一趟正在问，界面上那句「问不出分集：…」就该换成
	// 「正在问」。问成功了自然什么都没有，问失败了下一次 finishParts 又写回来。
	a.partsErr = nil
	a.partsMu.Unlock()

	// 后台那一趟。取流零件没挂上（`MountStreaming` 还没跑）也算失败：照样记下
	// 原因、照样隔一会儿再问一次，而不是「当问过了」——那样挂着零件之后界面
	// 再也不问，用户等一辈子也等不到分集。
	//
	// `partsFetch` 是那一趟。生产上永远是 nil（走取流零件），测试里替成一个
	// 假函数——**只由界面那一侧读写**（`ensureParts` 在这里就把函数取出来递给
	// 那个 goroutine，goroutine 不碰这个字段），所以不需要锁。
	fetch := a.partsFetch
	if fetch == nil {
		s := a.streamingSnapshot()
		if s == nil || s.Client == nil {
			a.finishParts(bvid, nil, errStreamingMissing)
			return
		}
		fetch = s.Client.VideoPages
	}
	go func() {
		items, err := fetch(context.Background(), bvid)
		a.finishParts(bvid, items, err)
	}()
}

// finishParts 记下一次取分集的结果并叫醒界面。
//
// ⚠️ **只有成功才 `partsLoaded = true`**（理由见 ensureParts）。
func (a *App) finishParts(bvid string, items []bili.VideoPart, err error) {
	a.partsMu.Lock()
	if err == nil && len(items) != 0 {
		if a.partsCache == nil {
			a.partsCache = make(map[string][]bili.VideoPart)
		}
		a.partsCache[bvid] = append([]bili.VideoPart(nil), items...)
	}
	if a.partsBvid == bvid {
		a.parts, a.partsErr = append([]bili.VideoPart(nil), items...), err
		a.partsLoaded, a.partsWanted = err == nil, false
	}
	a.partsMu.Unlock()

	if err == nil && len(items) != 0 {
		a.adoptCurrentParts(bvid, items)
	}
	a.mu.Lock()
	wake := a.wake
	a.mu.Unlock()
	if wake != nil {
		wake()
	}
}

func (a *App) cachedParts(bvid string) []bili.VideoPart {
	a.partsMu.Lock()
	defer a.partsMu.Unlock()
	if cached := a.partsCache[bvid]; len(cached) != 0 {
		return append([]bili.VideoPart(nil), cached...)
	}
	return nil
}

func (a *App) adoptCurrentParts(bvid string, parts []bili.VideoPart) {
	if len(parts) <= 1 || a.queue == nil {
		return
	}
	cur, ok := a.queue.Current()
	if !ok || cur.Bvid != bvid || !cur.isOnline() {
		return
	}
	if adopter, ok := any(a.queue).(interface {
		AdoptPages(Item, PageSet) (AdoptPagesResult, bool)
	}); ok {
		adopter.AdoptPages(cur, pageSetFor(cur, parts))
	}
}

func pageSetFor(current Item, parts []bili.VideoPart) PageSet {
	set := PageSet{Bvid: current.Bvid, VideoTitle: current.Title, Parts: make([]PageSpec, 0, len(parts))}
	for i, part := range parts {
		index := part.Page
		if index <= 0 {
			index = i + 1
		}
		set.Parts = append(set.Parts, PageSpec{Cid: part.Cid, Index: index, Title: part.Part})
	}
	return set
}

// PlayPart 换成放 bvid 的某一分集（cid）。
//
// **队列里那一项的 cid 就地改掉**，不是插一首新的。这是**同一项换集**的语义：
// 用户是在「这一条」里挑一集，队列里那一条始终只有一项——「上一首 / 下一首」
// 走的还是队列，用户换完集按「上一首」，期望回到刚才那一集去，而插新的会让他
// 退回的是另一项。
//
// ⚠️ **判重口径从票 #683 起带上 cid**（`Item.Identity` 是 `mv:<bvid>|<cid>`）：
// 所以 `replaceAt` 之后那一项的**身份变了**。这是对的——抽屉要按身份画行，换了
// 一集就是另一行了（同一 bvid 的两集在队列里各占一行，那是用户在历史记录里
// 看到的那种形状）。别拿「身份不变」当这一层的保证。
func (a *App) PlayPart(cid string) error {
	cid = strings.TrimSpace(cid)
	if cid == "" {
		return errors.New("没认出来要放哪一集")
	}
	cur, ok := a.queue.Current()
	if !ok {
		return errors.New("播放队列里没有当前这一首")
	}
	if !cur.isOnline() || cur.Bvid == "" {
		return errors.New("这一首没有分集")
	}
	if cur.Cid == cid {
		return nil // 已经在放这一集
	}
	if adopter, ok := any(a.queue).(interface {
		SelectPage(Item, string) (Item, bool)
	}); ok {
		if next, changed := adopter.SelectPage(cur, cid); changed {
			return a.play(next)
		}
	}
	if strings.HasPrefix(cur.GroupKey, pageGroupPrefix) {
		for i, item := range a.queue.Items() {
			if item.Bvid != cur.Bvid || item.Cid != cid {
				continue
			}
			if next, changed := a.queue.selectAt(i); changed {
				return a.play(next)
			}
			break
		}
	}
	i := a.queue.CurrentIndex()
	if i < 0 {
		return errors.New("播放队列里没有当前这一首")
	}

	cur.Cid = cid
	a.queue.replaceAt(i, cur)
	// 分集属于同一条队列项，保留现有播放器直到新分集装好。这样播放条左侧的
	// 封面、标题和作者不会在取流期间闪成空白；新播放器装好时会在
	// `installOnlinePlayer` 里原子替换旧播放器。
	return a.play(cur)
}
