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
	// ⚠️ **测试摆的那一份优先**（理由见 `App.danmakuTest`）：界面那一层要
	// 验「画在哪、怎么动」，而生产那一路要一个在线的有 cid 的队列项 + 一趟
	// 真的 HTTP。
	//
	// ⇒ 见到它就直接给，**一个请求都不发**（生产那一段整个被跳过）。
	a.danmakuMu.Lock()
	if a.danmakuTestSet {
		items := a.danmakuTest
		a.danmakuMu.Unlock()
		return DanmakuSnapshot{Items: items, Ready: true}
	}
	a.danmakuMu.Unlock()

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

// SetDanmakuForTest 直接摆一份「当前这一集的弹幕」。**只给测试用**。
//
// # 它绕过的正是生产那一段
//
// 没有 cid 的队列项、生产那一趟 HTTP、代次与缓存——界面那一层要验的是
// 「画在哪、怎么动」，不是「怎么取」（取的那一半是 BMG-305 那一票的事，
// 它自己带证据）。
//
// ⚠️ **摆空的那一份也是「摆」**（`[]DanmakuItem{}`）：那是「这一集没有
// 弹幕」，而「没摆过」是「走生产那一路」——两件事必须分得开。
func (a *App) SetDanmakuForTest(items []DanmakuItem) {
	a.danmakuMu.Lock()
	a.danmakuTestSet = true
	a.danmakuTest = items
	a.danmakuMu.Unlock()
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

// notePendingCid 把「这一次选的到底是哪一集」记到 `pending` 上。
//
// # 为什么要有这一句
//
// B 站的搜索结果只给 bvid，而**分 P id 是取流那一趟才问出来的**（`withCid`），
// 补出来的那一份只留在 `runOnline` 的局部变量里。而「用户刚才按的是哪一首」
// （`a.pending`）记的是**按下去那一刻**的那一项——那时 cid 还是空的。
//
// ⇒ 弹幕这一摊想知道「当前这一集的 cid」，就只能问 `pending`；而 `pending` 的
// cid 得有人写上去——就是这一句。
//
// ⚠️ **代次要对得上才写**：这一句是**取流那一趟**回来的时候写的，而那一趟可能
// 已经过时了（用户早就换歌了）。代次对不上就不写。
//
// 🔴 **bvid 对得上还不够**：**同一个 bvid 可以有不同的分 P**（多 P 的投稿）。
// 用户在「第一集」那一趟还在取流的时候按了「第二集」，于是 `pending` 已经换成
// 第二集了（cid 不同，bvid 相同）——而第一集那一趟回来时 bvid 是对得上的，
// 于是它把**第一集的 cid 写到第二集上**。
//
// 症状和评审提的第一条一模一样：「换了集，屏幕上滚的还是上一集的弹幕」——只是
// 这一次不是「cid 还没问出来」，而是「cid 问出来了、但问的是上一集」。
func (a *App) notePendingCid(gen int, bvid, cid string) {
	if bvid == "" || cid == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.onlineGen != gen {
		// 这一趟已经过时了（用户换歌了 / 这一趟超时了 / 应用退出了）——
		// 它问出来的 cid 是**它自己那一趟**的，不是「现在这一首」的。
		return
	}
	if a.pending.Bvid != bvid {
		// 不是这一首（切歌那一瞬间 `pending` 已经换成新的一首，而这一句是
		// **上一首**的取流回来的那时候写的）——那是上一首的 cid，不是新的。
		return
	}
	if a.pending.Cid == cid {
		// 已经是这一个了。
		return
	}
	a.pending.Cid = cid
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
// # 正在放的是本地文件时一律没有
//
// 队列里那一项可能还是上一首在线的歌（本地文件不进队列），而 `a.online` 在
// 放本地文件时会被清掉（见 `openLocal`）。所以这一支认的是「**正在放的那
// 一首**」，不是「队列当前的那一项」。
//
// # cid 缺了的时候**什么都不给**
//
// B 站的搜索结果只给 bvid，而 cid 是取流那一趟才问出来的（`online.go` 的
// `withCid`）——问出来之后记到 `pending` 上（见 `App.notePendingCid`）。
//
// ⚠️ **问出来之前不许从别处借**：同一个 bvid 的**新选择**（换了分 P）在 cid
// 还没问出来的时候，别处（`a.online`）留着的是**上一分 P** 的 cid——借过来
// 就会把上一分 P 的弹幕当成这一次的答案（见 `danmakuTarget` 里那段）。
func (a *App) danmakuTarget() (bvid, cid string, ok bool) {
	a.mu.Lock()
	pending := a.pending
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
	// 🔴 **只认 `pending` 自己的 cid，不去别处借**。
	//
	// ⚠️ **借是最容易出错的那一种做法**：同一个 bvid 的**新选择**（换了分 P、或者
	// 只是重新按了一遍同一个投稿）在 cid 还没问出来的时候，`pending.Cid` 是空的，
	// 而「正在放的那一条」（`a.online`）里留着的是**上一分 P** 的 cid——借过来
	// 就会把上一分 P 的弹幕当成这一次的答案端出去。
	//
	// 症状是「换了集，弹幕还是上一集的那一批」——而弹幕盖住封面的时候本来就
	// 不好认，用户看不出发生了什么。
	//
	// ⇒ 分 P id 问出来之后**写回 `pending`**（见 `App.notePendingCid`），于是
	// 「这一次选的到底是哪一集」只有一处知道，而那一处就是它自己。
	bvid = pending.Bvid
	cid = pending.Cid
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
	// 🔴 **在途序号**：每发起一趟就加一（**不管 key**）。
	//
	// 代次（`danmakuGen`）答的是「这一趟还属不属于**当前这一集**」——它挡的是
	// 「旧歌的结果盖到新歌上」。而**缓存写入**要挡的是另一件事：**同一个 key**
	// 上，一趟旧的（一开始发出去的）和一趟新的（后来发出去的）会**先后写同一个
	// 缓存文件**——慢的那一趟写得更晚，于是它把新的那份盖掉了。
	//
	// 那一趟在**可见状态**上是按代次丢掉的（对不上就走人），但它**照样会写
	// 盘**——于是盘上留下的是旧的那一份。
	//
	// ⚠️ **症状是「重新进这一集，拿到的是几分钟前的那一批」**：用户看到的是
	// 「弹幕怎么变了」，而他什么都没做。
	//
	// ⇒ 写盘之前对一遍：只有**最后发起的那一趟**能写。
	a.danmakuSeq++
	seq := a.danmakuSeq
	if a.danmakuSeqs == nil {
		a.danmakuSeqs = map[string]int{}
	}
	a.danmakuSeqs[key] = seq
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
				// 这一趟没有写盘（写盘是网络那一趟的事），所以它自己那条
				// 序号**用不上**——留着的话表会一直长（见 `dropDanmakuSeq`）。
				a.dropDanmakuSeq(seq, key)
				return
			}
		}
		if fetch == nil {
			s := a.streamingSnapshot()
			if s == nil || s.Client == nil {
				a.finishDanmaku(gen, key, errStreamingMissing)
				a.dropDanmakuSeq(seq, key)
				return
			}
			fetch = s.Client.Danmaku
		}
		items, err := fetch(context.Background(), cid)
		if err != nil {
			a.finishDanmaku(gen, key, err)
			a.dropDanmakuSeq(seq, key)
			return
		}
		a.installDanmaku(gen, key, items)

		a.saveDanmakuCache(seq, key, bvid, cid, items)
	}()
}

// saveDanmakuCache 把这一份存进缓存。**锁外调**（写盘是阻塞 IO）。
//
// # 两道闸
//
//  1. **在途序号**：只有**最后发起的那一趟**能写。慢一拍的那一趟（同 key）
//     写得更晚，会把新的那份盖掉——症状是「重新进这一集，拿到的是几分钟前的
//     那一批」。
//  2. **应用还在吗**：应用退出之后不许再写盘（那个目录可能已经被删了，而写
//     不下去只会多一条日志）。
//
// ⚠️ **零条也存**：B 站说「这个 cid 没有弹幕」是一份**完整的答案**，而不存
// 的话每进一次就要重新问一次。
func (a *App) saveDanmakuCache(seq int, key, bvid, cid string, items []bili.Danmaku) {
	// 🔴 **写盘走这把专锁**：「检查我是不是最新的一趟」和「真的写下去」必须
	// 在**同一个临界区**里——分开的话，中间发起的新一趟会让旧的那一趟以为
	// 自己还是最新的，于是它写得更晚、把新的盖掉。
	//
	// ⚠️ **它不是 `danmakuMu`**：写盘是阻塞 IO，而 `danmakuMu` 是界面每一帧
	// 都要的（`App.Danmaku`）。
	a.danmakuWriteMu.Lock()
	defer a.danmakuWriteMu.Unlock()

	a.danmakuMu.Lock()
	cache := a.danmakuCache
	// 🔴 **按 key 对序号**：这一集最后发起的那一趟才算数。
	//
	// ⚠️ **不能拿全局序号对**：全局的序号会把「另一集的新一趟」也算成「比这一集
	// 新」，于是切到另一集之后，上一集那份就落不了盘——而下次进上一集就得重新
	// 拉一次（那就不是「7 天内再进是瞬时的」了）。
	latest, known := a.danmakuSeqs[key]
	stale := !known || seq != latest
	a.danmakuMu.Unlock()
	if cache == nil || stale {
		// ⚠️ **过时的那一趟不许写**（它会把新写的那份盖掉）。
		//
		// ⚠️ **它的序号得收掉**：那一趟不会再写盘了，而留着就一直占着表。
		// （过时的这一趟 `dropDanmakuSeq` 是空操作——表里那条是**新的一趟**的。）
		a.dropDanmakuSeq(seq, key)
		return
	}
	a.mu.Lock()
	closed := a.closed
	a.mu.Unlock()
	if closed {
		// 应用退出了：这一趟不再写盘，序号也收掉。
		a.dropDanmakuSeq(seq, key)
		return
	}
	if err := cache.Save(bvid, cid, danmaku.Entry{
		Items:     items,
		FetchedAt: time.Now(),
	}); err != nil {
		log.Printf("biu: 弹幕缓存写不下去：%v", err)
	}
	// 写完了就把这一条从表里拿掉：留着的话它会一直长（用户听过的每一集都留
	// 一条）。
	//
	// ⚠️ **只删自己那一条**：别的 key 的序号不能碰。
	a.dropDanmakuSeq(seq, key)
}

// dropDanmakuSeq 把这一趟的序号从表里拿掉。
//
// ⚠️ **只删自己那一条**：别的 key 的序号不能碰（那一集可能还有一趟在途）。
//
// ⚠️ **序号用完了就要删**：那一趟**没写盘**（缓存命中 / 问失败 / 应用退出了）
// 时它就永远不会写盘了，而那条序号留着就一直占着——表会随着听过的每一集一直
// 长（用户听一天歌就是几百条）。
func (a *App) dropDanmakuSeq(seq int, key string) {
	a.danmakuMu.Lock()
	defer a.danmakuMu.Unlock()
	if a.danmakuSeqs[key] == seq {
		delete(a.danmakuSeqs, key)
	}
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
// # 它保证的是「Close 返回之后，缓存不再被写」
//
// ⚠️ **代次加一只管可见状态**：在途的那一趟回来时会发现代次变了，可见状态直接
// 丢掉——但**它照样会去写盘**。所以光靠代次是不够的（那一版的注释就是那么写的，
// 而那句话不成立）。
//
// ⇒ 这里持**写盘那把锁**：在途的那一趟要么已经写完了（那这一次 Close 等它），
// 要么还没开始写（那它拿到锁时看见的是「已经关了」，于是走人）。
//
// ⚠️ **持锁期间不许拿 `a.mu`**：写盘那一段会拿 `a.mu`（查 `closed`），而
// `App.Close` 是在**放开 `a.mu` 之后**才调这里的——所以这里拿 `a.mu` 不会死锁，
// 但顺序必须是这样（先 `a.mu` 后这把锁，反过来就死锁了）。
func (a *App) closeDanmaku() {
	// 先把代次加一：在途的那一趟回来时，可见状态直接丢掉。
	a.danmakuMu.Lock()
	a.danmakuGen++
	a.danmakuWanted = false
	a.danmakuMu.Unlock()

	// 再等在途的写盘（如果有）：它写完之后，这一层就再也不写了。
	a.danmakuWriteMu.Lock()
	a.danmakuWriteMu.Unlock()
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
