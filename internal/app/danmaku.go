// 弹幕这一摊：当前这一集的弹幕是哪些。
//
// # 和分集、赞藏是同一个形状
//
// 「分集列表」（`fullplayer.go` 的 `Parts` / `ensureParts`）和「赞没赞」
// （`like.go`）都是「现算的、缓存在 App 上、换歌就作废」的一摊。弹幕也是同一件
// 事：它属于**某一集**（按 cid 组织），不属于用户，所以按 `<bvid>-<cid>` 缓存、
// 换歌就重来。抄那一套而不是另发明一种，是为了让「换歌之后界面上还留着上一首的
// 弹幕」这个 bug 只有一处可能发生。
//
// # 界面每一帧问一次，不发请求
//
// `Danmaku()` 是每帧调的（满屏播放器要画滚动的弹幕）。它**不阻塞**：那一趟网络
// 往返在后台，回来之后叫醒界面（铁律三），所以界面下一帧就看到了。
//
// ⚠️ **界面那一侧不许自己记「问过了没有」**：那份状态在这一层，换一首歌就该
// 忘掉。抄 `Parts()` 的注释：界面每一帧调一次就行。
//
// # 代次
//
// 换歌的时候旧的那一趟还在路上。慢一拍回来的结果会盖掉新歌的弹幕——症状是
// 「在听 B，屏幕上滚的是 A 的弹幕」，而用户完全看不出发生了什么（弹幕盖住封面
// 的时候本来就不好认）。所以每换一次歌加一代次，回来的时候对一遍，对不上直接
// 丢（和 `lyricGen` / `onlineGen` 是同一件事）。
//
// # 这一层不发请求（缓存那一层也是）
//
// 缓存在 `internal/service/danmaku` 那一层（照 `service/lyrics` 的规矩：它自己
// 只管存和读）。拿数据的那一趟在**应用层**（`ensureDanmaku` 里那个 goroutine）。
package app

import (
	"context"
	"log"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/service/danmaku"
)

// danmakuRetryAfter 是问失败之后隔多久再问一次。
//
// 15 秒是「网络抖动能缓过来、而用户还在盯着这个窗口」的量级（照
// `partsRetryAfter` / `likeRetryAfter` 那两个）。它同时也是**重试的上限频率**：
// 界面每一帧都调 `Danmaku()`，所以没有这个退避的话失败一次就是每秒一发。
const danmakuRetryAfter = 15 * time.Second

// DanmakuItem 是界面上画一条弹幕要的东西。
//
// ⚠️ **它是 `bili.Danmaku` 的一个拷贝，不是那个结构本身**：那一层是服务层的
// 形状（时间 / 模式 / 字号 / 颜色 / 正文 / id），而界面这一层不该直接依赖它
// ——服务层改字段名的时候，界面不该跟着改（也不该编译不过）。
type DanmakuItem struct {
	// ID 是这条弹幕的 id。界面拿它当**稳定且唯一的 key**（动态列表的 key 必须
	// 稳定且唯一：前面插一条会让后面所有条的状态整体错配）。
	ID string
	// TimeMS 是**视频时间**（毫秒）。
	//
	// ⚠️ 我们放的是这个视频的**音轨**，弹幕的时间基准是视频时间，两者 1:1 对齐
	//（没有额外 offset）——所以界面上拿 `State.Position` 去比就行。
	TimeMS int64
	// Mode 是弹幕模式：1 滚动 / 4 底部 / 5 顶部。
	//
	// ⚠️ 实测还有 6（高级）和 7（代码），**没查到它们是什么意思**，但它们确实
	// 会出现。所以这一档是「原样带出来」，不是「只认三个」。
	Mode int
	// FontSize 是字号：25 标准 / 18 小（实测还有 45 / 64 / 70）。
	FontSize int
	// Color 是**十进制 RGB**（B 站给的就是十进制）。16777215 = 白。
	Color uint32
	// Text 是正文。
	Text string
}

// DanmakuSnapshot 是弹幕区一帧要读的全部状态。
type DanmakuSnapshot struct {
	// Items 是这一集的弹幕（按 B 站给的顺序，**没有**按时间排）。
	//
	// ⚠️ **「按时间取现在该在屏幕上的那几条」是界面那一层的活**：这一层给的是
	// 「这一集有哪些弹幕」。
	Items []DanmakuItem
	// Ready 报「这一份已经有答案了没有」。
	//
	// ⚠️ **「答案是零条」也是 Ready**：B 站给了一个没有弹幕的 cid，那是一份
	// **完整的答案**，不是「还在问」。界面据此在「什么都没有」和「还在问」之间
	// 选——否则一个没有弹幕的 cid 会让界面永远显示「正在问」。
	//
	// ⚠️ **压根没有弹幕的条目（本地文件 / 音乐库 / 没有 cid）不是 Ready**：那一
	// 种**永远不会有答案**（没有 cid 就问不出来），拿它当「还在问」是撒谎。
	// 界面要区分那两种，靠 `DanmakuWorthAsking()`（照 `PartsWorthAsking` 那一
	// 条路的做法）。
	Ready bool
}

// Danmaku 返回当前这一集的弹幕。**每一帧调一次**。
//
// ⚠️ **它不阻塞**：那一趟网络往返在后台（见 `ensureDanmaku`），回来之后叫醒
// 界面（铁律三）。
//
// ⚠️ **没有 cid 时给的是空快照，而且一个请求都不发**：本地文件和音乐库条目都
// 走这一支，而它们**永远**没有弹幕。
func (a *App) Danmaku() DanmakuSnapshot {
	bvid, cid, ok := a.danmakuTarget()
	if !ok {
		return DanmakuSnapshot{}
	}
	key := danmakuKeyOf(bvid, cid)
	a.ensureDanmaku(key, bvid, cid)

	a.danmakuMu.Lock()
	defer a.danmakuMu.Unlock()
	if a.danmakuKey != key {
		// 这一份还不属于当前这一集（刚换歌、后台那一趟还没回来）。
		//
		// ⚠️ **绝不能把上一首的给出去**：那是最难看的那个 bug——在听 B，屏幕上
		// 滚着 A 的弹幕，而 A 的封面根本不在这屏幕上。
		return DanmakuSnapshot{}
	}
	return DanmakuSnapshot{Items: a.danmakuItems, Ready: a.danmakuReady}
}

// DanmakuWorthAsking 报「当前这一集有没有弹幕可问」。
//
// 判据是「这是一个 B 站的投稿（mv）、而且拿得出 cid」。
//
// ⚠️ **它和 `DanmakuSnapshot.Ready` 是两件事**：那一栏答的是「这一集有没有弹幕」，
// 这一栏答的是「有没有弹幕**可以问**」。本地文件和音乐库条目两栏都是「没有」，
// 而界面要靠这一栏区分「还在问」和「压根没有」。
func (a *App) DanmakuWorthAsking() bool {
	_, _, ok := a.danmakuTarget()
	return ok
}

// DanmakuErr 是最近一次取弹幕失败的原因。界面上拿它说「问不出弹幕」。
func (a *App) DanmakuErr() error {
	a.danmakuMu.Lock()
	defer a.danmakuMu.Unlock()
	return a.danmakuErr
}

// MountDanmaku 挂上弹幕缓存。**在开窗口之前调**（建目录那一步是阻塞 IO）。
//
// 没挂也能跑：弹幕照样从 B 站取（只是每次都要重新拉一遍），而关掉之后不记得。
func (a *App) MountDanmaku(c *danmaku.Cache) {
	a.danmakuMu.Lock()
	a.danmakuCache = c
	a.danmakuMu.Unlock()

	// 挂着的时候已经有歌在放（测试里很常见），那就把这一集补上。
	//
	// ⚠️ **用 `danmakuTarget` 而不是队列项**：搜索结果放的那一首，队列项里没有
	// cid（cid 是取流那一趟补出来的，只留在正在放的那一条上）——只看队列项的话
	// 挂晚了就再也补不上。
	if bvid, cid, ok := a.danmakuTarget(); ok {
		a.ensureDanmaku(danmakuKeyOf(bvid, cid), bvid, cid)
	}
}

// danmakuKeyOf 是这一份弹幕的缓存键（也是「这一份属于谁」的判据）。
//
// ⚠️ **bvid 和 cid 都要在**：弹幕是**按分 P** 组织的，同一个投稿的每一集弹幕
// 都不一样，只按 bvid 认的话多 P 的视频会串。
func danmakuKeyOf(bvid, cid string) string { return bvid + "-" + cid }

// danmakuIdentity 报一个队列项有没有弹幕可问，以及它是哪一集。
func danmakuIdentity(it Item) (bvid, cid string, ok bool) {
	if !it.isOnline() || it.Type != "mv" || it.Bvid == "" || it.Cid == "" {
		return "", "", false
	}
	return it.Bvid, it.Cid, true
}

// danmakuTarget 报「当前正在放的这一集，它的弹幕是谁的」。
//
// # 判据
//
//	在线 + 是投稿（mv）+ 有 bvid + 拿得出 cid
//
// ⚠️ **本地文件和音乐库条目一律没有弹幕**（第一版不开音乐库那条路）：本地文件
// 没有 cid；音乐库条目的 cid 在 `SubtitleCid` 那一栏（那是「它背后那个投稿」的
// 身份，而**第一版不开它**——理由见 `Item.SubtitleBvid` 的注释）。
//
// # cid 缺了的时候往「正在放的那一条」借
//
// 队列里那一项**可能还没有 cid**：B 站的搜索结果只给 bvid，而 cid 是取流那一趟
// 问出来的（`online.go` 的 `withCid`），补出来的那一份**只留在正在放的那一条上**
// （`onlineTrack.item`），队列里那一项还是空的。
//
// ⇒ 只看队列项的话，搜索结果放的歌**永远没有弹幕**（界面上一条都不滚，而用户
// 看不到任何错处）。所以 cid 空的时候往正在放的那一条借——它的 bvid 要和队列
// 当前项对得上，否则就是另一首歌的 cid（那更糟）。
//
// # 正在放的是本地文件时一律没有
//
// 队列里那一项可能还是上一首在线的歌（本地文件不进队列），而 `a.online` 在
// 放本地文件时会被清掉（见 `openLocal`）。所以这一支认的是「**正在放的那
// 一首**」，不是「队列当前的那一项」。
func (a *App) danmakuTarget() (bvid, cid string, ok bool) {
	a.mu.Lock()
	pending := a.pending
	online := a.online
	a.mu.Unlock()

	// 🔴 **认的是「正在放 / 正在准备放的那一首」，不是「队列当前的那一项」**。
	//
	// 这两样不一样，而不一样的那几种情况都得出同一个答案：**没有弹幕**。
	//
	//	本地文件   `pending` 是空的（本地文件不进队列，见 `openLocal`）
	//	停了       `ClearTrack` 把 `pending` 清空（这一首到此为止）
	//	删了当前曲 `ClearTrack` 同样清空
	//
	// 队列当前的那一项在那些情况下**还指着上一首在线的歌**——照它认的话，用户
	// 放的是本地文件，屏幕上滚的却是上一首的弹幕。
	//
	// ⚠️ **`pending` 记的是「用户按的是哪一首」**（见 `playOnlineFrom`），而它在
	// 切歌那一刻就换成了新的那一首——所以它不会慢一拍。
	if !pending.isOnline() || pending.Type != "mv" || pending.Bvid == "" {
		return "", "", false
	}
	cur := pending
	bvid = cur.Bvid
	cid = cur.Cid
	if cid == "" && online != nil && online.item.Bvid == bvid {
		// cid 缺了的时候往正在放的那一条借：取流那一趟补出来的那一份只留在那里
		//（队列项里那份还是空的，见 `withCid` 的注释）。
		//
		// ⚠️ **bvid 要对得上**：切歌的那一瞬间 `a.online` 还是**旧的那一首**
		//（新流要等取完了才装上），那时候拿它的 cid 就是拿上一集的。
		cid = online.item.Cid
	}
	if cid == "" {
		return "", "", false
	}
	return bvid, cid, true
}

// startDanmaku 报「现在放的是这一集」，把那一趟发出去。
//
// 换歌的时候起（照 `fetchBiliSubtitle` 那一档的写法：**在后台**，不拦播放）。
func (a *App) startDanmaku(it Item) {
	bvid, cid, ok := danmakuIdentity(it)
	if !ok {
		// 换成了没有弹幕的那一种（本地文件 / 音乐库）：**旧的作废**。
		//
		// ⚠️ **这一句不能省**：不清的话，界面上会继续滚上一首的弹幕，而用户
		// 刚换的歌压根没有弹幕——那看起来像「弹幕卡住了」。
		a.danmakuMu.Lock()
		if a.danmakuKey != "" {
			a.danmakuKey = ""
			a.danmakuGen++
			a.danmakuItems = nil
			a.danmakuReady = false
			a.danmakuLoaded = false
			a.danmakuWanted = false
			a.danmakuErr = nil
			a.danmakuTriedAt = time.Time{}
		}
		a.danmakuMu.Unlock()
		return
	}
	a.ensureDanmaku(danmakuKeyOf(bvid, cid), bvid, cid)
}

// ensureDanmaku 保证正在问 / 已经问过这一集的弹幕。
//
// 换歌就重置（和 `ensureParts` 同一条）：`danmakuKey` 是这份缓存属于谁，
// 迟到的结果发现对不上直接丢。
//
// ⚠️ **防重入靠的是 `danmakuWanted`**：界面每一帧都调 `Danmaku()`，而那一趟
// 是网络往返——不挡住的话一次播放能发几百个请求（B 站会风控，而症状是「怎么
// 问不出弹幕」）。
func (a *App) ensureDanmaku(key, bvid, cid string) {
	a.danmakuMu.Lock()
	if a.danmakuKey != key {
		// 换集了：旧的作废（代次加一，那一头回来时对不上就走人）。
		a.danmakuKey = key
		a.danmakuGen++
		a.danmakuItems = nil
		a.danmakuReady = false
		a.danmakuLoaded = false
		a.danmakuWanted = false
		a.danmakuErr = nil
		a.danmakuTriedAt = time.Time{}
	}
	if a.danmakuLoaded || a.danmakuWanted {
		a.danmakuMu.Unlock()
		return
	}
	// 退避：每一帧都调 `Danmaku()`，不等一下的话失败一次就是每秒一发。
	if !a.danmakuTriedAt.IsZero() && time.Since(a.danmakuTriedAt) < danmakuRetryAfter {
		a.danmakuMu.Unlock()
		return
	}
	a.danmakuWanted = true
	a.danmakuTriedAt = time.Now()
	// 清掉上一次的失败原因：这一趟正在问，界面上那句「问不出弹幕」就该撤了。
	a.danmakuErr = nil
	gen := a.danmakuGen
	cache := a.danmakuCache
	// `danmakuFetch` 是那一趟。生产上永远是 nil（走取流零件上的
	// `bili.Client.Danmaku`），测试里替成一个假函数。
	//
	// ⚠️ **在锁里取出来，goroutine 不碰这个字段**（和 `partsFetch` / `likeFetch`
	// 同一条）：那一趟跑在后台，而它是**同一个字段**——不取出来递给它的话，
	// 它就得自己再去读，那才是竞态。
	fetch := a.danmakuFetch
	a.danmakuMu.Unlock()

	// 后台那一趟。**缓存先查**：查到了就**不发请求**（这一集 7 天内再进是瞬时的）。
	go func() {
		if cache != nil {
			if e, ok := cache.Load(bvid, cid); ok {
				// ⚠️ **缓存命中不许重写缓存**：那会把「取回来的时刻」刷新成
				// 现在，于是这份缓存**永远不会过期**——而过期的意义正是
				// 「过一阵子重新拉一次」。
				a.installDanmaku(gen, key, e.Items)
				return
			}
		}
		if fetch == nil {
			s := a.streamingSnapshot()
			if s == nil || s.Client == nil {
				a.finishDanmaku(gen, key, errStreamingMissing)
				return
			}
			fetch = s.Client.Danmaku
		}
		items, err := fetch(context.Background(), cid)
		if err != nil {
			a.finishDanmaku(gen, key, err)
			return
		}
		a.installDanmaku(gen, key, items)

		// 存进缓存（**锁外**：写盘是阻塞 IO，持有 `danmakuMu` 去等磁盘会把
		// 界面那一帧冻住）。
		//
		// ⚠️ **零条也存**：B 站说「这个 cid 没有弹幕」是一份**完整的答案**，而
		// 不存的话每进一次就要重新问一次。
		if cache != nil {
			if err := cache.Save(bvid, cid, danmaku.Entry{
				Items:     items,
				FetchedAt: time.Now(),
			}); err != nil {
				log.Printf("biu: 弹幕缓存写不下去：%v", err)
			}
		}
	}()
}

// installDanmaku 把一份弹幕装到状态里并叫醒界面。
//
// ⚠️ **代次对不上就丢**：换歌之后旧的那一趟还会回来，回来的时候这一集已经换了，
// 把它装上去就是「在听 B，滚着 A 的弹幕」。
func (a *App) installDanmaku(gen int, key string, items []bili.Danmaku) {
	a.danmakuMu.Lock()
	if a.danmakuGen != gen || a.danmakuKey != key {
		// 又换歌了 / 应用在退。这一份已经过时，丢掉。
		a.danmakuDropped.Add(1)
		a.danmakuMu.Unlock()
		return
	}
	a.danmakuWanted = false
	a.danmakuItems = danmakuItemsOf(items)
	a.danmakuReady = true
	a.danmakuLoaded = true
	a.danmakuErr = nil
	a.danmakuMu.Unlock()

	// 🔴 铁律三：后台投递完状态**必须**叫醒界面。只投递不唤醒的话，弹幕永远
	// 不出来，而没有任何报错。
	if wake := a.currentWake(); wake != nil {
		wake()
	}
}

// finishDanmaku 记下一次取弹幕失败，并叫醒界面。
//
// ⚠️ **代次对不上就丢**：换歌之后旧的那一趟还会回来，而它失败的原因对新的
// 那一集是不相干的（界面上那句「问不出弹幕」得说新的那一集的事）。
func (a *App) finishDanmaku(gen int, key string, err error) {
	a.danmakuMu.Lock()
	if a.danmakuGen != gen || a.danmakuKey != key {
		a.danmakuDropped.Add(1)
		a.danmakuMu.Unlock()
		return
	}
	a.danmakuWanted = false
	a.danmakuErr = err
	a.danmakuMu.Unlock()

	log.Printf("biu: 弹幕问不出来：%v", err)

	// 🔴 铁律三：状态变了（哪怕是「失败了」）也要叫醒界面——否则界面上那句
	// 「问不出弹幕」永远出不来。
	if wake := a.currentWake(); wake != nil {
		wake()
	}
}

// closeDanmaku 在应用退出时把弹幕这一摊收掉。
//
// ⚠️ **代次加一**：在途的那一趟回来时会发现代次变了，直接走人——于是它不会在
// 应用退出之后还往盘上写一份缓存（那个目录可能已经被删了）。
func (a *App) closeDanmaku() {
	a.danmakuMu.Lock()
	a.danmakuGen++
	a.danmakuWanted = false
	a.danmakuMu.Unlock()
}

// danmakuItemsOf 把服务层那一份变成界面要的那一份。
func danmakuItemsOf(items []bili.Danmaku) []DanmakuItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]DanmakuItem, len(items))
	for i, d := range items {
		out[i] = DanmakuItem{
			ID:       d.ID,
			TimeMS:   d.TimeMS,
			Mode:     d.Mode,
			FontSize: d.FontSize,
			Color:    d.Color,
			Text:     d.Text,
		}
	}
	return out
}
