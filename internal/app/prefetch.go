// 下一首的流地址**提前备好**。
//
// # 这一票要治的症状
//
// `App.onTrackFinished`（自动切歌）在**切歌那一刻才去取流**：歌放完到下一首
// 出声之间有一段空档——取流要跑一趟带 WBI 签名的网络往返（实测 200~400 毫秒），
// 再叠上开解码器、等第一批样本。用户听到的是「切歌 → 空档 → 出声」。
// （后来票 #581 把手动按「下一首」也接到了这一层：`App.step` 用
// `peekPreparedNext` 吃备好的那一份，不再重新取。）
//
// 这一层把那一段挪走：**当前这首快结束的时候，后台把下一首的流地址备好**，
// 真播它的时候直接用（见 `App.takePrefetch`）。
//
// # 三条硬约束（每一条都是别的播放器踩过才知道的）
//
//  1. **必须带提前量，也不能太早**。太早打开下一条 HTTP 连接会撞上服务端的
//     并发连接限制，服务端把**当前**那条流关掉 → 解码器报错 → 提前跳下一首
//     （cliamp 的教训）。这一票只解析地址、**不预下载字节**，所以压根不开下一条
//     CDN 连接；但对 B 站的请求还是要节制。
//  2. **队列一改就作废**。用户拖了一下队列顺序、换了一档、删了一首——
//     「下一首是谁」就变了，而备好的地址是**另一首**的。留着它的话，自动切歌
//     那一刻会把歌放错（症状：放出来的是另一首，而界面上看不出任何异样）。
//  3. **失败要有黑名单**。这一层的触发点挂在 `App.tick` 上（50ms 一拍），
//     没有黑名单的话一次失败的预取会被**每 50ms 重建一次**——那是对 B 站的
//     请求风暴，而且风控面比不预取还大。
//
// # 为什么判据只有一处
//
// 「下一首是谁」只有 `Queue.Advance` 说得出来（见 queue.go 那一节）。预取要
// 提前知道它，就用 `Queue.PeekNext`——而它是 `Advance` 的**同一份代码**
// （`advanceLocked`）的「只算不动」那一支。两处各算一遍的话，随机那一档迟早
// 对不上，而对不上的症状是「备好的地址是另一首的」：歌还是对的（对不上就重新
// 取），但这一票要省的那几百毫秒白费了。

package app

import (
	"context"
	"fmt"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// prefetchLead 是提前量：当前这一首还剩多久的时候开始备下一首的地址。
//
// # 为什么是 15 秒
//
// 两个约束同时压着它。
//
// **① 早到用户听不出空档**
//
// 空档 = 取流那一趟网络往返（实测 200~400ms）+ 开解码器 + 等第一批样本。
// 提前量必须**盖住最坏情况**，否则备到一半这一首就放完了——那样预取白做。
// B 站取流接口自己的超时是 10 秒一段，预取最多两段（问分 P + 取流），
// 15 秒盖得住绝大多数情况；超时的那些退回正常路径（照样能放，只是没有无缝）。
//
// **② 不能太早**
//
// cliamp 的教训是「太早打开下一条 HTTP 连接会撞上服务端的并发连接限制」。
// **这一票只解析地址、不预下载字节**（见文件头），所以压根不开下一条 CDN
// 连接——那是另一件事（预下载 = 占磁盘 + 多一条连接），这一票不做。
// 剩下的那点风险是「对 B 站多打一次请求」，而这一份地址**就是真播时要用的
// 那一份**（真播时直接用它，不重新取）——所以正常情况下请求数**没有增加**。
//
// 参考：yt-music-player 的 prefetchLead 是 20 秒（它是**整段下载**，风险那一档
// 更重）；cliamp 是 3 秒（那是**流的剩余时间**）。
//
// ⚠️ **别把它调成 0**：那样预取就退化成了「切歌那一刻才取」——也就是现在的
// 样子，而这一票多出来的那些复杂度就全白费了。
const prefetchLead = 15 * time.Second

// prefetchTimeout 是**这一趟预取**最多等多久。
//
// 它比提前量略长一点点：预取慢一点也不影响什么（真播那一趟会自己重新取），
// 但它必须**有上限**——没有上限的话，一次卡住的预取会一直挂着，而它占着
// 一个 goroutine 和一条连接。
//
// ⚠️ 取 15 秒（和提前量一样）：B 站接口自己 10 秒一段就超时了，所以两段
// （问分 P + 取流）最多 20 秒——这一条把最坏的那一段掐掉，剩下的走「取流失败
// → 黑名单 → 真播时重新取」那条路。
const prefetchTimeout = 15 * time.Second

// prefetchState 是「备下一首」那一摊的状态。
type prefetchState int

const (
	// prefetchIdle：没有在备，也没有备好。
	//
	// ⚠️ **它只是零值**（iota 的头一个）：入口总是先落成「正在备」，而
	// 「压根没有在备」在 App 上是 `a.prefetch == nil`。
	prefetchIdle prefetchState = iota
	// prefetchRunning：正在备（后台那一趟网络往返还没回来）。
	prefetchRunning
	// prefetchReady：备好了，等着真播它。
	prefetchReady
	// prefetchFailed：备失败了（**黑名单**：这一首不重试，见 prefetch.go 的
	// 「失败要有黑名单」那一节）。
	prefetchFailed
)

// prefetchEntry 是「备好的下一首」那一档。
type prefetchEntry struct {
	state prefetchState
	// item 是备给的那一项——**解析过的那一份**（投稿那一路 cid 已经补齐）。
	item Item
	// ref 是备好的流地址。
	ref *bili.StreamRef
	// err 是失败的原因（state 是 prefetchFailed 时才有）。
	err error
	// cancel 是「掐掉在途的那一趟」（context.CancelFunc，可以调任意次）。
	//
	// ⚠️ **它是为了「这一份没人要了」而存在的**：队列变了、应用退出了——
	// 那时候这一趟请求再回来也已经没人要，而它还占着一条连接。
	cancel func()
}

// maybePrefetch 是「现在要不要开始备下一首」那一次判断。
//
// 它挂在 `App.tick` 上（50ms 一拍，见 app.go）——那是这一层天然的挂点：
// 界面那一帧要什么它不管，而「还剩多久」这件事只有那一拍知道。
func (a *App) maybePrefetch(pos, dur time.Duration) {
	// 时长拿不到 → 不知道这一首什么时候结束 → 没法判断「快结束了」。
	if dur <= 0 {
		return
	}
	// 还早：太早备就是白打一次请求（见 prefetchLead 那段「不能太早」）。
	if dur-pos > prefetchLead {
		return
	}

	a.mu.Lock()
	p, want, online, loading, closed := a.player, a.wantPlay, a.online, a.loading, a.closed
	a.mu.Unlock()
	if closed || p == nil || online == nil {
		return
	}
	// ⚠️ **暂停时不预取**：用户停着，备好了也白备（这十几秒里他可能换歌、
	// 退出、关机），而多打一次请求就是多一分风控面。
	if !want {
		return
	}
	// 正在换歌：这一趟取流还没完，再开一条并发连接没有好处。
	if loading {
		return
	}

	// 🔴 **先问「手上有没有」再问「下一首是谁」**。
	//
	// 预告（`Queue.PeekNext`）在随机那一档是 **O(队列长度) 次字符串分配**
	// （每一项的判重标识都要拼一遍，见 `Item.Identity`），而这一拍每 50ms
	// 来一次——歌单长的话那是一笔看不见的开销（它在后台时钟里，不进
	// framebudget 的判据，所以量不出来）。
	//
	// 而「手上那份还对不对」有保证：**队列一变就作废**（见 invalidatePrefetch），
	// 所以手上还留着它，就说明队列没变 → 那一份一直是对的 → 不用再预告一次。
	// （真要换下一首的时候，队列一定也变了 → 那一份会被作废 → 下一拍重新预告。）
	if a.prefetchSettled() {
		return
	}

	// 下一首是谁——**和真播的那一首同一个来源**（见 Queue.PeekNext）。
	d, known := a.queue.PeekNext()
	if !known || d.Kind != AdvancePlay {
		return
	}
	next := d.Item
	// 本地文件不用备：它压根不走取流（`App.Open` 是同步开文件的）。
	if !next.isOnline() {
		return
	}
	// ⚠️ **压根放不了的（合集、剧集、用户那一行）不备**：备它就是白打一次请求
	// （而那一行压根没有音频流，见 `playableOnline`）。
	if err := playableOnline(next); err != nil {
		return
	}
	// 🔴 **失效的也不备**（票 #624）：那一首的稿件已经被 UP 主删了，而备它
	// 就是**白打一次带签名的取流往返**（实测 200~400ms）——而那一趟注定失败。
	//
	// ⚠️ **判据是 `CanQueue`**（一处）：失效这个事实在 `Item.Invalid` 上，
	// 而它和队列入口那一道闸用的是同一个判据。
	if !CanQueue(next) {
		return
	}
	// 和正在放的那一首是同一首 → 没什么可备的（单曲循环、队列里同一首排了两遍）。
	if sameStreamTarget(online.item, next) {
		return
	}

	a.startPrefetch(next)
}

// prefetchSettled 报「现在没什么可做的」：手上已经有一份（备好的 / 在备的 /
// 黑名单里的），或者整个预取停着（风控、限流那几类）。
//
// ⚠️ **它是「不用再问下一首是谁」的依据**：队列一变就作废，所以手上还留着
// 一份，就说明队列没变（见 maybePrefetch 里那段）。
func (a *App) prefetchSettled() bool {
	a.prefetchMu.Lock()
	defer a.prefetchMu.Unlock()
	return a.prefetch != nil || a.prefetchOff
}

// startPrefetch 发起一趟预取。
//
// ⚠️ **它在锁外面调网络**：这一层绝不占着 `a.mu` 跑网络（那是界面每帧都要
// 的那把锁，见 App 里 a.mu 那段注释）。
func (a *App) startPrefetch(it Item) {
	a.prefetchMu.Lock()
	if a.prefetchOff {
		// 整个预取停着（见 prefetchFailed 那一段）：再打就是往同一面墙上撞。
		a.prefetchMu.Unlock()
		return
	}
	if a.prefetch != nil {
		// 手上已经有一份了（这一拍刚被别人填上的）：不重复发起。
		//
		// ⚠️ **「哪一份」在这里不重要**：只要有一份在，就说明队列没变
		// （队列变了会作废，见 invalidatePrefetch），而下一首是谁也已经
		// 算过了（见 maybePrefetch）。
		a.prefetchMu.Unlock()
		return
	}
	a.prefetchGen++
	gen := a.prefetchGen
	// 🔴 **这一趟可以被掐掉**：队列变了、或者应用退出了，那一趟再回来也没人
	// 要（而它还占着一条连接）。`context.WithCancel` 的 cancel 可以调任意次。
	ctx, cancel := context.WithCancel(context.Background())
	a.prefetch = &prefetchEntry{state: prefetchRunning, item: it, cancel: cancel}
	a.prefetchMu.Unlock()

	go a.runPrefetch(gen, ctx, it)
}

// runPrefetch 是后台那一趟：把下一首的流地址备好。
//
// 🔴 **它只解析地址，不预下载字节**（报告 P8 的建议）：
//
//   - 我们的 `StreamSource` 会**在盘上落一个临时文件**
//     （`internal/service/audio/stream_source.go`），预下载字节 = 占磁盘 +
//     多一条 CDN 连接——那是另一件事，这一票不做。
//   - 只解析地址的话，多出来的只有「对 B 站多打一次取流请求」这一件事，
//     而它换来的正是「切歌到出声」那一段空档。
//
// ⚠️ **它不许碰界面上的任何东西**：不许弹窗、不许写播放条上的红字、不许动
// 「正在取流」那一档、更不许动播放代次（`onlineGen`）——它是**旁观者**，
// 真播那一首的路一点都不改。失败了就安静退回（见 prefetchFailed）。
func (a *App) runPrefetch(gen int, ctx context.Context, it Item) {
	s := a.streamingSnapshot()
	if s == nil {
		a.prefetchFailed(gen, it, errStreamingMissing)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, prefetchTimeout)
	defer cancel()

	// 解析：投稿那一路缺 cid 就问一次（搜索结果全都是这样，见 online.go 的
	// withCid 那段注释）。
	//
	// ⚠️ **音乐库那一路不问元信息**：取流只要 sid（`AudioStreamURL(sid)`），
	// 而元信息是**锦上添花**（作者名、封面）——真播那一趟会问（`withSongInfo`），
	// 这里再问一遍就是白打一次请求。
	resolved := it
	if it.Type != "audio" {
		var err error
		resolved, err = a.withCid(ctx, s, it)
		if err != nil {
			a.prefetchFailed(gen, it, err)
			return
		}
	}

	// 取流地址。
	//
	// ⚠️ **它不走 `resolve`**：那个函数会往「播放前预判」那个缓存里写，
	// 而那个缓存是**手动切歌也要问**的——写进去的话手动切歌就不重新取流了
	// （而手动切歌重新取流是这一票定下的规矩，见 onTrackFinished）。
	ref, err := a.streamFor(ctx, s, resolved, s.Quality)
	if err != nil {
		a.prefetchFailed(gen, it, err)
		return
	}
	// ⚠️ **判空**：拿到一个空的引用的话下面那几处就会 deref 一个 nil（而那是
	// **握着锁** panic 的那种错——见 invalidatePrefetch 那段注释）。
	if ref == nil {
		a.prefetchFailed(gen, it, fmt.Errorf("%w：取流接口给了一个空的地址", bili.ErrNoStream))
		return
	}
	// 🔴 **地址要真的是这一首的**——但**只有音乐库那一路查得了**：
	//
	//   - 音乐库（sid）：`ref.Sid` 填的是**接口回来的那个**（见
	//     `bili.refFromSongURL`——它拿返回里的 sid 填，拿不到才退回请求里那个）。
	//     版权曲换了地址、接口给回另一首的流，这一比就拦住了。
	//   - 投稿（bvid/cid）：**接口不回身份**。`bili.PlayURL` 是拿**请求里的**
	//     bvid/cid 填 `StreamRef` 的（见 `bili.refFrom`），所以这一比是**拿请求
	//     和它自己比**——结构上不可能为假，**那一路这里是一道空闸**。
	//
	// ⚠️ **所以别把这一比当成投稿那一路的防线**：那一路「静默给另一首的流」
	// 挡不住（**正常播放那条路也挡不住**——那边压根没有这道比）。真要挡，得等
	// B 站把身份放进响应里，或者从 CDN 地址上认。
	//
	// ⚠️ 留着它是为了音乐库那一路：那一路的身份是**接口给的**，比得了。
	if !sameItem(ref, resolved) {
		a.prefetchFailed(gen, it, fmt.Errorf("%w：取回来的这条流不是这一首的", bili.ErrNoStream))
		return
	}
	// ⚠️ **快过期的地址不备**：B 站的 deadline 是个粗粒度的开关，而
	// `Usable` 留了两分钟提前量（见 bili.StreamRef.Usable）。备一份马上要过期的
	// 地址，真播时还得重新取——那这一趟就白做了。
	if !ref.Usable(time.Now()) {
		a.prefetchFailed(gen, it, fmt.Errorf("%w：备好的地址马上就要过期了", bili.ErrStreamExpired))
		return
	}

	a.prefetchMu.Lock()
	if a.prefetchGen != gen {
		// 已经不是这一趟了（队列变了、换歌了、或者又发起了一趟）：这一份没人要了。
		a.prefetchMu.Unlock()
		return
	}
	a.prefetch = &prefetchEntry{state: prefetchReady, item: resolved, ref: ref}
	a.prefetchMu.Unlock()

	a.logf("biu: 下一首的流地址备好了：%q（%s）", resolved.Title, ref.Label)
}

// prefetchFailed 记一次预取失败，并把它变成**黑名单**。
//
// 🔴 **失败必须无声**：这一层是旁观者，失败了就退回正常路径（真播那一趟按
// 正常路径再取一次），界面上一个字都不冒——用户没点任何东西，不该被一句
// 红字骂。
//
// 🔴 **不可重试的错额外把整个预取停掉**：`bili.Recoverable`（票 #346，全项目
// 唯一那处「现在立刻再问一次有没有用」的判据）判成不可重试的那几类——风控、
// 限流、压根没有音频流、要会员——「立刻再问一次」不会给出不一样的答案。
// 而预取存在的意义只是省那几百毫秒，**拿「多一分风控面」去换它不划算**。
//
// 停到什么时候？**下一首开始**（见 takePrefetch）：那时候风控多半已经过去了。
func (a *App) prefetchFailed(gen int, it Item, err error) {
	a.prefetchMu.Lock()
	if a.prefetchGen != gen {
		a.prefetchMu.Unlock()
		return
	}
	a.prefetch = &prefetchEntry{state: prefetchFailed, item: it, err: err}
	if !bili.Recoverable(err) {
		a.prefetchOff = true
	}
	a.prefetchMu.Unlock()

	a.logf("biu: 下一首的流地址备不出来（安静退回真播那一趟）：%q：%v", it.Title, err)
}

// peekPreparedNext 把「备好的那一份」的**值**摸出来，不消费——预取层的
// 状态（代次、在途的取消、黑名单）一动不动。
//
// 它是「下一首按钮」那一头（`App.step`）用的。直接用 `takePrefetch` 不行：
// 队列的 `MoveNext` 自己就会作废预取（「下一首变了」的钩子），而消费式的
// 取用必须赶在它**之前**、那时候又还不知道下一首是谁——所以只能先把值
// 摸出来，挪完队列再比对（比对判据同 `takePrefetch` 的那一半）。
// 挪完之后 `MoveNext` 的作废把状态清干净，两头不冲突。
//
// 返回的 ref 为 nil 表示手上没有备好的那一份（没备过、在备、备失败）。
func (a *App) peekPreparedNext() (Item, *bili.StreamRef) {
	a.prefetchMu.Lock()
	defer a.prefetchMu.Unlock()
	if a.prefetch == nil || a.prefetch.state != prefetchReady {
		return Item{}, nil
	}
	return a.prefetch.item, a.prefetch.ref
}

// TestOnlyPrefetchReady 报「下一首的地址**已经备好了**」。**只给测试用**。
//
// ⚠️ **为什么测试需要它**：预取有**两段**——
//
//	请求发出去了   → 取流接口的调用次数 +1（**这是「在途」**）
//	地址回来了     → 预取状态落成 `prefetchReady`（**这是「就绪」**）
//
// 而「按下一首吃不吃预取」这件事**只在就绪的时候才成立**：在途的时候
// 手上压根没有地址可用，按下一首照常重新取一次流是**对的**。
//
// ⚠️ **别拿「请求发出去了」当「备好了」**：那两件事之间有一整个网络往返
// （B 站取流接口带签名，几十到几百毫秒），机器一忙那一拍就过去了 ⇒
// 拿着「在途」去按下一首 ⇒ 必然重新取流 ⇒ 判据（取流次数）多一次。
//
// 名字里带 TestOnly 是为了 grep 一下就找得到（理由同 `SetVideoOpener`）。
func (a *App) TestOnlyPrefetchReady() bool {
	a.prefetchMu.Lock()
	defer a.prefetchMu.Unlock()
	return a.prefetch != nil && a.prefetch.state == prefetchReady
}

// takePrefetch 取出「备好的那一份」，给真播下一首用。
//
// 它同时是**黑名单的出口**：备失败的那一份在这里被丢掉（状态清空），于是
// 真播那一趟走正常路径重新取一次（失败无声退回）。
//
// ⚠️ **拿回来的地址先跟当前项和下一项各比一次**（Strawberry 那一套）：
// 备好的那份是**几秒前**算出来的「下一首」，而队列在这几秒里可能变了
// （重排、换档、删歌）。对不上就丢掉——宁可多取一次，也不能把**另一首**的
// 地址当成这一首的放出去。
//
// 两次比的是不同的东西：**当前项**那一半在备的时候比过（和正在放的那一首
// 是同一首就不备，见 maybePrefetch），**下一项**这一半在这里比（备的那一份
// 是不是该播的那一首），而**地址本身**是不是这一首的，在取回来的时候就比过
// 了（`sameItem`，见 runPrefetch）。
//
// ⚠️ **它同时清掉全局停用那一档**：下一首开始了，那一次失败（或被丢弃）
// 不该压着后面几首——「这一首失败就不重试」说的是这一首，不是以后每一首。
//
// 🔴 **只有自动切歌走这里**（`onTrackFinished` 的 `AdvancePlay` 那一支）。
// 手动按下一首走的是 `step`，它用 `peekPreparedNext`——见那边的注释。
func (a *App) takePrefetch(next Item) (Item, *bili.StreamRef, bool) {
	a.prefetchMu.Lock()
	e := a.prefetch
	a.prefetch = nil
	// ⚠️ **代次也要加**：在途的那一趟（还没回来）回来之后对不上代次，自己走人
	// ——它备的是**旧的**下一首，留着就是「队列变了还拿旧地址」。
	a.prefetchGen++
	// 黑名单到头了：下一首开始了，那一次失败（或被丢弃）不该压着后面几首——
	// 「这一首失败就不重试」说的是这一首，不是以后每一首。
	a.prefetchOff = false
	a.prefetchMu.Unlock()

	// 🔴 **在途的那一趟掐掉**：它备的是**旧的**下一首，没人要了。
	//
	// ⚠️ **判空**：`e` 可能是 nil（压根没备过）——而这里 panic 的话是**握着锁**
	// panic，那把锁就永远不开了（后面每一次预取都卡在上面，而症状是「整个
	// 应用冻住」）。
	if e != nil && e.cancel != nil {
		e.cancel()
	}

	if e == nil || e.state != prefetchReady {
		return next, nil, false
	}
	// 不是同一首 → 丢掉。
	if !sameStreamTarget(e.item, next) {
		return next, nil, false
	}
	// 快过期了 → 放弃（真播那一趟会重新取一条新的）。
	if !e.ref.Usable(time.Now()) {
		return next, nil, false
	}
	return e.item, e.ref, true
}

// invalidatePrefetch 是队列那一头的钩子：「下一首变了，那份预取作废」。
//
// 🔴 **代次也要加**：不然**在途**的那一趟回来之后照样会把它存进去——而它备的
// 是**旧**的下一首。队列变了还拿旧地址，就是「把预取的结果播出去」。
//
// ⚠️ **它不碰 `a.mu`**：这一层有自己的锁（见 App 里 prefetchMu 那段注释），
// 而 `a.mu` 是界面每帧都要的那把——在这儿抢它，界面那一帧的等待时间就变成
// 了「队列那一头的动作」。
func (a *App) invalidatePrefetch() {
	a.prefetchMu.Lock()
	e := a.prefetch
	a.prefetch = nil
	a.prefetchGen++
	// 失效意味着下一首的身份已经重新决定。失败抑制只属于旧目标，不能
	// 压住新目标的预取；这同时覆盖队列变化和手动下一首。
	a.prefetchOff = false
	a.prefetchMu.Unlock()

	// 🔴 **在途的那一趟掐掉**：没人要它了，而它还占着一条连接。
	//
	// ⚠️ **判空**（理由同 takePrefetch 那一处）：这里 panic 的话是**握着锁**
	// panic，那把锁就永远不开了。
	if e != nil && e.cancel != nil {
		e.cancel()
	}
}

// sameStreamTarget 报两项是不是**同一段流的同一个目标**。
//
// ⚠️ **它比的不是 `Item.Identity()`**：那个标识在**两边都知道 cid 时**认得出
// 分集，而在有一边不知道时（列表行没有 cid、队列项补出来了）它判的是「两首」
// ——那个口径对**队列身份**是对的（见 `trackKey`），对「用户说的是不是这一段」
// 却不是（票 #683）。
//
// 比的是取流要用的那几样：
//
//	mv     bvid 一样，cid 也一样（**有一个缺 cid 时不比 cid**——那是问出来的，
//	       而问出来的那一个已经被填进备好的那一份里了）
//	audio  sid 一样
//
// 🔴 **先判种类**：`ItemOfMedia` 对**所有**类型都填 `Sid = Media.ID`
// （`collection_download.go`），而 avid 和 auid 是同一个数值空间——不判种类的话
// 队列里正在放的 auid=12345 和收藏夹里 avid=12345 的那一行会被认成同一首：
// 列表上那一行在跳、点它播的是「音乐库那一首」。种类不同一律不同，不比后面那几样。
//
// ⚠️ **认不出是哪一首的（bvid 和 sid 都是空串）一律当不同**：那种项压根放不了，
// 而当成同一首的话，一份来路不明的地址会被当成「这一首的」。
//
// # 谁在用它（票 #683 之后有三个调用方）
//
//	预取（`maybePrefetch` / `takePrefetch`）	备好的那一份是不是这一首
//	`IsPlaying`	列表上那一行是不是正在放的这一首（行没有 cid、队列项有）
//	`Queue.indexOfStreamTarget`	「点它」找的是不是队列里那一项（同上）
//
// ⚠️ **后两个都是「有一边不知道 cid」的场景**，前一个两边都知道（备的时候已经
// 补过 cid）。三种都要的是「不知道的那一边不比」——所以判据只留这一份。
func sameStreamTarget(x, y Item) bool {
	xAudio, yAudio := x.Type == "audio", y.Type == "audio"
	// 🔴 **种类不同一律不同**（理由见上面那段）：audio 只跟 audio 比，mv 只跟 mv 比。
	if xAudio != yAudio {
		return false
	}
	if xAudio {
		return x.Sid != "" && x.Sid == y.Sid
	}
	if x.Bvid == "" || x.Bvid != y.Bvid {
		return false
	}
	if x.Cid != "" && y.Cid != "" {
		return x.Cid == y.Cid
	}
	return true
}
