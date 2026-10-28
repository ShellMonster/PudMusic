package ui

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/covertint"
	"github.com/egoist/mygo/ui"
)

// coverTint 记着封面取出来的色相，换封面时在后台重新取。
//
// # 它记两样东西
//
//	「当前这一张封面的色」——那是界面上要用的那一档
//	「按 URL 记下来的一张表」——那是**缓存**
//
// 从前只有第一样，所以切走再切回同一个歌单要重新下载、重新解码、
// 重新取色一遍（用户看到的是「光晕又闪了一次」）。现在有第二样，
// 同一个 URL 第二次要色**不再发网络请求**。
//
// # ⚠️ 缓存是**按 URL** 的，不是「一个格子」
//
// 一个格子的话，「换成别的封面」会把前一个的色冲掉——于是切回来还是要
// 重下。表才有缓存的意义。
type coverTint struct {
	mu       sync.Mutex
	url      string
	hue, sat float64

	// cache 是「URL → 取到的色」那一张表。
	//
	// ⚠️ **它的值是「那个色相和饱和度」，取不到色（饱和度 0）也记**——
	// 否则每次 `sync` 都会对一个取不到色的封面重新发一次请求，而那张封面
	// （黑白封面、坏地址）永远是取不到色的，于是**每一帧都发一次网络请求**。
	cache map[string]tintColor

	// inflight 是「正在取色中的那些 URL」。
	//
	// ⚠️ **它必须在锁外面读**：一个 URL 已经在取了，就不要再起第二个
	// （那一趟是网络往返 + 解码，起两个就是白花一倍）。
	inflight map[string]bool

	// glowKey / glowCol 是满屏光晕那一色的缓存：同一对 (色相, 饱和度) 只算一次，
	// 每帧再格式化一遍十六进制串、再解析回来是白花的分配。
	glowKey [2]float64
	glowCol ui.Color
	glowOK  bool
}

// tintColor 是一个封面取出来的色。
type tintColor struct {
	hue, sat float64

	// at 是「上次用到它是什么时候」——淘汰最久没用的那一条靠它。
	//
	// ⚠️ **不能靠 `coverTint.url` 判**：那只是「当前这一张」，不是「上一次
	// 用到的是哪一张」。
	at uint64
}

// tintCacheLimit 是那张表最多留多少条。
//
// ⚠️ **必须有上限**：那是内存，而歌单的封面会一个个看下去——没有上限的话
// 看一天就能攒下几万条。
//
// 64 条是够的：用户同时会打开的歌单就那么几个，而表是**按最近用的排**的
// （见 `putCached`），挤掉的总是最久没用的那一个。
const tintCacheLimit = 64

// tintClock 是「用过几个封面了」那个计数器（淘汰最久未用的靠它）。
//
// ⚠️ **单调递增**：同一张封面被反复用到时，它的 `at` 每次都要更新成更大的
// 值——否则它会被当成「最久没用」被挤掉，而它明明刚刚才用过。
var tintClock atomic.Uint64

// tintClient 拉封面字节用的客户端：走全应用唯一的网络层（代理设置、
// 凭据边界都归它管），超时保留自己那一档——取色是锦上添花，等不起。
var tintClient = func() *http.Client {
	c := newCredentialTransport().Client(nil)
	c.Timeout = 10 * time.Second
	return c
}()

// sync 报当前封面的色相和饱和度；封面换了就起一次后台下载，取到后叫醒界面重画。
// 还没取到（或封面没有颜色）时饱和度是 0，主题沿用原来的主色。
//
// ⚠️ **命中缓存时直接返回，不起任何后台活**——切走再切回同一个歌单时，
// 那层光晕立刻就在（票 #623）。
func (t *coverTint) sync(url string, wake func()) (hue, sat float64) {
	if url == "" {
		t.mu.Lock()
		t.url = ""
		t.hue, t.sat = 0, 0
		t.mu.Unlock()
		return 0, 0
	}

	// 缓存里有的：直接用。
	t.mu.Lock()
	if c, ok := t.cache[url]; ok {
		// ⚠️ **每次命中都要把 `at` 推到最新**：否则这一条会被当成「最久
		// 没用」挤掉，而它明明刚刚才用过。
		c.at = tintClock.Add(1)
		t.cache[url] = c
		t.url, t.hue, t.sat = url, c.hue, c.sat
		t.mu.Unlock()
		return c.hue, c.sat
	}
	// 已经在取了：别起第二个，等那一趟回来。
	if t.inflight[url] {
		t.url = url
		t.mu.Unlock()
		return 0, 0
	}
	t.url = url
	t.hue, t.sat = 0, 0
	if t.inflight == nil {
		t.inflight = map[string]bool{}
	}
	t.inflight[url] = true
	t.mu.Unlock()

	go t.fetch(url, wake)
	return 0, 0
}

// fetch 取一次色，取到后记进缓存、叫醒界面。
func (t *coverTint) fetch(url string, wake func()) {
	h, ok, err := covertint.Fetch(context.Background(), tintClient, url)
	t.mu.Lock()
	delete(t.inflight, url)
	if err == nil {
		// ⚠️ **取不到色也记**（饱和度 0）：那张封面（黑白封面、坏地址）永远
		// 取不到色，不记的话每次 `sync` 都会重发一次请求。
		t.putCached(url, tintColor{hue: h.H, sat: h.S})
	}
	// ⚠️ **只有「当前要的还是这一张」才写当前那一档**（理由同从前）：
	// 取色是在后台跑的，用户可能已经切到别的封面去了。
	if t.url == url {
		t.hue, t.sat = h.H, h.S
		if !ok {
			t.hue, t.sat = 0, 0
		}
	}
	t.mu.Unlock()
	if err != nil {
		return
	}
	wake()
}

// putCached 把一个色记进那张表（**调用方已经拿着锁**）。
func (t *coverTint) putCached(url string, c tintColor) {
	if t.cache == nil {
		t.cache = map[string]tintColor{}
	}
	if _, ok := t.cache[url]; !ok && len(t.cache) >= tintCacheLimit {
		t.evictOldestLocked()
	}
	c.at = tintClock.Add(1)
	t.cache[url] = c
}

// evictOldestLocked 挤掉最久没用的那一条（**调用方已经拿着锁**）。
//
// ⚠️ **「最久没用」靠 `t.url` 判不出来**（那只是「当前这一张」），
// 所以这张表自己记一条「上次用到是什么时候」。
func (t *coverTint) evictOldestLocked() {
	oldest, found := "", false
	var oldestAt uint64
	for u, c := range t.cache {
		if !found || c.at < oldestAt {
			oldest, oldestAt, found = u, c.at, true
		}
	}
	if found {
		delete(t.cache, oldest)
	}
}

// cached 查缓存里有没有这个 URL 的色。
func (t *coverTint) cached(url string) (hue, sat float64, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c, found := t.cache[url]
	return c.hue, c.sat, found
}

// cacheSize 是那张表现在有多少条。
func (t *coverTint) cacheSize() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.cache)
}

// idle 报「没有正在取色的了」。
func (t *coverTint) idle() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.inflight) == 0
}

// glow 是封面主色在满屏背景上的光晕色（和 sync 同一套取色）。取不到色时 ok 为 false。
//
// wake 由调用方预先备好传进来：现场写 `s.wake.invalidate` 每帧会建一个闭包。
func (t *coverTint) glow(url string, wake func()) (ui.Color, bool) {
	hue, sat := t.sync(url, wake)
	if sat <= 0 {
		return ui.Color{}, false
	}
	key := [2]float64{hue, sat}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.glowKey != key {
		t.glowKey = key
		t.glowCol, t.glowOK = parseHexColor(covertint.Hue{H: hue, S: sat}.Color(true))
	}
	return t.glowCol, t.glowOK
}
