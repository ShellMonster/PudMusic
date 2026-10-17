// 播放记录上报：把「在放哪一首、放到哪儿了」按节奏写进 B 站。
//
// # 这一页为什么之前永远是空的
//
// 「历史记录」那一页读的是 B 站**服务器**上的历史（`service/bili/history.go`），
// 而我们一次都没往里写过。于是只用我们这个应用、从不开 B 站网页端的用户，
// 那一页永远是空的（审计 A7）。协议层的两个请求在 `service/bili/playreport.go`；
// 这一层管的是另外三件协议层不该知道的事。
//
// # 一、什么时候发
//
// 只有**在线**的投稿有得报：本地文件在 B 站那边没有对应的东西，队列里的
// `audio`（B 站音乐库）那条路还没接上（`online.go` 的 `playableOnline`）。
// 其余的由 `sync` 按「这一刻和上一一刻差在哪」推出来：
//
//	开始播   没有会话 + 在放        → 开一轮会话 + PlayStart
//	切歌     会话记的那首变了      → 上一轮 PlayEnd，本轮另开
//	暂停     在放 → 不在放          → PlayPause（会话留着）
//	继续     不在放 → 在放          → PlayResume
//	播完     真的放到头了          → PlayEnd，played_time 发 -1
//	停止     用户按了停止          → PlayEnd（`App.Stop` 直接调 `stopTrack`）
//	清空     这一首没了            → PlayEnd
//	心跳     在放且离上次够久       → PlayPlaying，每 30 秒一次
//
// **「停止」和「清空」为什么不在 `sync` 里推**：它们都把播放器状态清成
// 「不在放」，而「不在放」这一个信号分不出「用户暂停了」（会话要留着）
// 和「这一首到头了」（会话要结掉）。所以那两处直接调 `stopTrack`——
// 在它们自己知道「这是终止」的那一刻说清楚，比在 `sync` 里猜可靠。
//
// # 二、退登与换人要停
//
// 🔴 **上报是写操作，它写进用户真实的 B 站账号。** 所以一轮会话必须记住
// 「我这一轮是替谁记的」，而每次要发之前都对一遍：账号变了（退登、或者换人
// 登录）就**连结束都不发**——把 PlayEnd 发给一个刚登上的新账号，等于往
// 那个人的历史里塞一条不是他看的记录。
//
// 更要紧的是**不许替新账号把这一首重新开一轮**（`orphan`）：半路换人登录，
// 那一首是**上一个账号**开始听的，而进度是从上一个账号的播放位置接着涨的。
// 记过去等于凭空往那个人的历史里塞一条他没听过的记录。换一首歌就走干净了。
//
// ⚠️ 这里比的是**账号身份**（`DedeUserID`）而不是 `Manager.Generation()`：
// 代次每换一次登录态就涨一，而一次扫码登录（无论成不成功）必然涨一——
// 拿它当判据的话，用户正听着歌、手指点了一下登录，历史记录就从那一刻起
// 静悄悄地不记了，而界面上一个字都不会说。要防的那件事（别把 A 的进度
// 写进 B 的账号）用身份比就够，而且准。
//
// # 三、上报失败不许影响播放
//
// `sync` 跑在**进度条的时钟**上（`app.go` 的 tick），它只读内存、往队列里
// 塞一条，绝不发请求。真正的网络在 `run` 那一条 goroutine 上，一条一条按
// 顺序发。队列满了就**丢掉这一条**（丢的是一条心跳，不是歌），失败只记一条
// 日志。理由和 `app.runOnline` 那句「界面线程上不碰网络」是同一条：网络抖
// 一下不该卡歌，更不该让用户看见一句「播放记录上报失败」。
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

const (
	// reportInterval 是心跳间隔。B 站按 30 秒算会话活跃度，太密了只是浪费。
	reportInterval = 30 * time.Second

	// reportQueue 是待发队列的容量。
	//
	// 队列满的时候**丢掉这一条**，不阻塞 `sync`——那一条是进度，卡它的是歌。
	// 丢心跳的后果只是 B 站那边少一个采样点，而整轮播放记录照样在。
	reportQueue = 32

	// reportTimeout 是单次上报的时间上限。
	//
	// B 站的默认超时是 10 秒，而退登时用户点完就该看到界面动了：给上报留
	// 5 秒，断网时也不会让那一下等很久。
	reportTimeout = 5 * time.Second

	// reportDrain 是退出前等「手上那条真的发出去」的上限。
	//
	// 它只在退出/换客户端那一刻用：那一轮的最后一条 PlayEnd 是这一轮最后
	// 一次机会，不等它就发出去，历史记录里那一条就停在上一个采样点上。
	// 上限摆在那里是为了断网时不把退出卡住——超时就走，那一条丢了只是
	// 少一格进度。
	reportDrain = 800 * time.Millisecond
)

// playReporter 是「往 B 站写播放记录」这件事的全部状态。
type playReporter struct {
	cli *bili.Client
	// mid 返回当前登录账号的 DedeUserID。没挂登录或没登录时是空串。
	mid func() string
	// on 返回设置里「记录播放历史」那个开关。
	on func() bool
	// every 是心跳间隔。正式路径是 reportInterval，测试里改小。
	every time.Duration

	out  chan reportMsg
	stop chan struct{}
	// wg 等那条发送 goroutine 真的走了，`close` 才返回。
	wg sync.WaitGroup

	mu sync.Mutex
	// sess 是当前这一轮会话，nil = 此刻没有在报。
	sess *reportSess
	// gen 是**上报代次**。**每开一轮 +1**：新一轮一开，上一轮还排在队列里的
	// 那些消息对不上号就被丢掉（见 `live`）——用户连点五首，前四首的在途
	// 上报不会补到第五首后面去。
	//
	// ⚠️ **结束一轮的时候不许涨**。理由见 `dropLocked`。
	gen uint64
	// skip 记着「这一首开不了会话」（问不到 aid），别每 50 ms 再问一次。
	skip string
	// orphan 记着「这一首是在别的账号名下开始听的，别替它开新一轮」。
	//
	// 🔴 它防的是票面点名的那一件：**别把退登后的进度写回登录中的账号**。
	// 中途退登再换个人登录，那个人**根本没听过**这首——而进度是从上一个
	// 账号的播放位置接着涨的，记进他的历史记录就等于凭空造了一条。
	// 换一首歌就走干净了（`sync` 第 2 条之后那一句把它清掉）。
	orphan string
	// queued 是「已经进队列、还没发完」的条数。退出前靠它等最后那一条
	// 真的发出去（见 `drain`）。
	queued int
	// closed 是上报已经停了（退登了/应用关了）。置上之后 `sync` 什么都不做。
	closed bool
}

// reportSess 是一轮播放上报的全部上下文。
//
// ⚠️ 它归 `sync` 写（在 r.mu 里），worker 只读一份**快照**，所以加字段时
// 先想清楚谁在写。`aid` 不在这里——那是 worker 查出来的，见 `run`。
type reportSess struct {
	// gen 是这一轮属于第几代上报。
	gen uint64
	// id 是这一轮的会话 id（B 站按它把一堆心跳并成一次观看）。
	id string
	// mid 是这一轮**替谁记的**。见文件头「二、退登与换人要停」。
	mid string
	// bvid / cid 是这一轮记的是哪一稿的哪一集。切歌就是比这两个。
	bvid, cid string
	// startTS 是这一轮开始的 Unix 秒。
	startTS int64
	// maxPlayed 是这一轮到过的最远处。拖回去重听不会把它拽回来，
	// 而 B 站拿它算「看了多久」。
	maxPlayed int64
	// lastAt 是上一次**发出去**任何一条的时刻。心跳按它算间隔。
	lastAt time.Time
	// playing 是「上一次我们告诉 B 站的，是在不在放」。暂停/继续就是比它。
	playing bool
}

// reportKind 是一条待发消息是哪一种。
type reportKind int

const (
	// kindOpen 是「开一轮会话」。它带着这一轮的开始进度。
	kindOpen reportKind = iota
	// kindBeat 是一次心跳（含开始/暂停/继续/结束四种动作）。
	kindBeat
)

// reportMsg 是一条待发消息。
//
// 它是 `sync` 那一刻的**快照**：worker 拿到它的时候那一轮可能早就结束了，
// 所以它带着 gen，worker 发之前要对一遍（见 `live`）。
type reportMsg struct {
	kind  reportKind
	gen   uint64
	bvid  string
	cid   int64
	start int64
	id    string
	mid   string
	// played / dur / max 分别是这一下的进度秒、总时长秒、这一轮到过的最远处秒。
	played int64
	dur    int64
	max    int64
	action bili.PlayAction
}

// newPlayReporter 建一个上报器并把发送那条 goroutine 起起来。
func newPlayReporter(cli *bili.Client, mid func() string, on func() bool) *playReporter {
	r := &playReporter{
		cli: cli, mid: mid, on: on,
		every: reportInterval,
		out:   make(chan reportMsg, reportQueue), stop: make(chan struct{}),
	}
	r.wg.Add(1)
	go r.run()
	return r
}

// setInterval 改心跳间隔。**只给测试用**（正式路径是 30 秒）。
func (r *playReporter) setInterval(d time.Duration) {
	r.mu.Lock()
	r.every = d
	r.mu.Unlock()
}

// SetPlayReportInterval 改上报心跳的间隔。
//
// ⚠️ **只给测试用**：正式路径是 `reportInterval`（30 秒）。存在的理由是
// 「等了 30 秒」既慢又不稳，而心跳这一条要在测试里跑很多遍。
func (a *App) SetPlayReportInterval(d time.Duration) {
	a.mu.Lock()
	r := a.report
	a.mu.Unlock()
	if r != nil {
		r.setInterval(d)
	}
}

// mountPlayReport 装上上报器（`MountStreaming` 调，因为只有在线流才报）。
func (a *App) mountPlayReport(cli *bili.Client) {
	if cli == nil {
		return
	}
	// ⚠️ **换客户端时上一条 goroutine 得走掉**，否则它还拿着旧客户端在发，
	// 而那个客户端的 jar 可能已经被换掉了。`close` 会等它真的退出，
	// 所以它一定是在拿着旧客户端发完手上那一条之后才走的。
	//
	// ⚠️ `old` 要**在锁外面**关：`close` 要等一条 goroutine，而那条
	// goroutine 每一拍都要拿 `a.mu`（问设置和登录态）。锁里面等 = 死锁。
	a.mu.Lock()
	old := a.report
	a.report = newPlayReporter(cli, a.accountMid,
		func() bool { return a.SettingsSnapshot().ReportPlayHistory })
	a.mu.Unlock()
	if old != nil {
		old.close()
	}
}

// accountMid 是当前登录账号的 DedeUserID。没挂登录时是空串。
func (a *App) accountMid() string {
	if acc := a.account; acc != nil {
		return acc.Session().DedeUserID
	}
	return ""
}

// ─────────────────────────────────────────────────────────────────────────────
// 决策：在进度条的时钟上，只读内存
// ─────────────────────────────────────────────────────────────────────────────

// reportTick 是进度条那一拍调的那个入口（`app.go` 的 tick）。
//
// ⚠️ **它不发任何网络请求**：位置、时长、在不在放都是从 App 的状态里抄的
// （`tick` 已经把它们写回去了），真正的网络在 `run` 那一条 goroutine 上。
// 理由和 `app.runOnline` 那句「界面线程上不碰网络」是同一条：上报晚一拍
// 没有关系，卡住窗口有。
func (a *App) reportTick(playing, finished bool) {
	a.mu.Lock()
	r := a.report
	var it Item
	if a.online != nil {
		it = a.online.item
	}
	dur, pos := a.duration, a.position
	a.mu.Unlock()
	if r == nil {
		return
	}
	r.sync(it, dur, pos, playing, finished)
}

// sync 是「这一刻和上一刻差在哪」的全部判断。
//
// ⚠️ 它跑在 tick 上，**不许发任何网络请求**：它只往队列里塞消息。理由见
// 文件头「三、上报失败不许影响播放」。
//
// `finished` 是「**真的**放完了」，不是「这一轮从在放变成不在放」——解码
// 失败和断流也是后者，把它们报成播完等于往用户的账号里写一句「他看完了」。
func (r *playReporter) sync(it Item, dur, pos time.Duration, playing, finished bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}

	mid := r.mid()
	cur := r.sess
	// online 是「这一首有得报」：只有**在线投稿**。本地文件在 B 站那边没有
	// 对应的东西，队列里的 `audio` 那条路还没接上（见文件头「一」）。
	online := it.Type == "mv" && it.Bvid != ""
	// 换了一首就把它记过一次的那一笔清掉：那笔的寿命只限那一首，
	// 不清的话用户过两天再放同一首，那一次是能查到的却不报。
	if r.skip != "" && r.skip != it.Bvid {
		r.skip = ""
	}

	// 1. 这一轮归谁记。账号变了就地作废，**连结束都不发**（文件头「二」）。
	if cur != nil && (mid == "" || cur.mid != mid) {
		log.Printf("biu: 账号变了，在放的这一轮播放记录作废（%s → %s）", cur.mid, mid)
		// 🔴 而且**这一首从此不再开新一轮**（见 `orphan` 那一段）：
		// 它是上一个账号开始听的，进度不能记到刚登上的这个账号名下。
		r.orphan = cur.bvid
		r.dropLocked()
		return
	}

	// 2. 切歌 / 清空 / 从在线换成本地文件：上一轮到头了，本轮另开。
	if cur != nil && (!online || it.Bvid != cur.bvid || it.Cid != cur.cid) {
		r.pushLocked(r.beatOf(cur, pos, dur, bili.PlayEnd, false))
		r.dropLocked()
		cur = nil
	}
	// 换了一首就不再是「那首了」——下一首是干干净净的新一轮。
	if r.orphan != "" && r.orphan != it.Bvid {
		r.orphan = ""
	}

	// 3. 开关被关掉了：这一轮就此作废，同样**不发**（用户说了不记）。
	if cur != nil && !r.on() {
		r.dropLocked()
		return
	}

	// 4. 开一轮新的。
	if cur == nil {
		switch {
		case !online || !playing:
			// 🔴 `playing` 这一条不能省：否则「用户按了停止」之后下一拍就会把
			// 刚才 `stopTrack` 关掉的那一轮又开起来，而用户明明已经停了。
			return
		case !r.on():
			// 🔴 开关**关着的时候不许开新一轮**。上面第 3 条管的是「唱到一半
			// 关掉了」——它只能让已经在记的那一轮停下，管不到还没开的。
			// 少了这一条，用户把那个开关关掉之后放什么歌都还在记，而那个
			// 开关就摆在他眼前（`ui/history.go` 的页头）。
			return
		case mid == "":
			return // 没登录：B 站会认下请求，但什么也不会记下来
		case r.skip == it.Bvid:
			return // 这一首问不到 aid，别每 50 ms 再问一次
		case r.orphan == it.Bvid:
			// 🔴 这一首是**上一个账号**开始听的。半路换了账号还替它开一轮，
			// 就是把那个人没听过的进度记到这个人名下——票面点名要防的就是
			// 这一件。换一首歌就走干净了（上面那一句把 orphan 清掉）。
			return
		}
		r.startLocked(it, mid, pos, dur)
		return
	}

	// 5. 会话在跑：先把这一帧的进度记下来（拖回去重听不拽回 maxPlayed）。
	if secs := secsOf(pos); secs > cur.maxPlayed {
		cur.maxPlayed = secs
	}

	switch {
	case finished:
		// 🔴 `played_time` 发 -1，B 站靠它把这一条标成「看完」（接口文档：
		// 「视频播放进度（秒），完成为 -1」）。**只有真放完了才发**。
		r.pushLocked(r.beatOf(cur, pos, dur, bili.PlayEnd, true))
		r.dropLocked()
		return
	case !playing && cur.playing:
		r.pushLocked(r.beatOf(cur, pos, dur, bili.PlayPause, false))
	case playing && !cur.playing:
		r.pushLocked(r.beatOf(cur, pos, dur, bili.PlayResume, false))
	case playing:
		// 心跳按节奏发。**刚发过就别发**：上面那两种已经带上了最新进度，
		// 紧接着再补一条 play_type=0 只是多一次请求。
		if at := time.Now(); at.Sub(cur.lastAt) >= r.every {
			r.pushLocked(r.beatOf(cur, pos, dur, bili.PlayPlaying, false))
		}
	}
	cur.playing = playing
}

// startLocked 开一轮会话。调用方拿着 r.mu。
func (r *playReporter) startLocked(it Item, mid string, pos, dur time.Duration) {
	id, err := newReportSessionID()
	if err != nil {
		// 造不出会话 id 就这一首不记。造不出来几乎只可能是系统的随机源坏了，
		// 而那本来就不该让用户看见一句话。
		log.Printf("biu: 造不出播放记录的会话 id，这一首不记：%v", err)
		return
	}
	r.gen++
	at := time.Now()
	r.sess = &reportSess{
		gen: r.gen, id: id, mid: mid,
		bvid: it.Bvid, cid: it.Cid,
		startTS: at.Unix(), lastAt: at, playing: true,
		maxPlayed: secsOf(pos),
	}
	r.pushLocked(reportMsg{
		kind: kindOpen, gen: r.gen,
		bvid: it.Bvid, cid: cidOf(it.Cid),
		start: r.sess.startTS, id: id, mid: mid,
		played: secsOf(pos), dur: secsOf(dur), max: r.sess.maxPlayed,
	})
}

// stopTrack 结掉当前这一轮：用户按了停止，或这一首被从队列里拿掉了。
//
// ⚠️ **`pos` 和 `dur` 要由调用方在清零之前抄下来**：`App.Stop` 和
// `App.ClearTrack` 都是先把播放位置清成 0 才走到这里的，用那之后的 0
// 报上去，B 站那边这一条就成了「从头开始听然后立刻停了」。
func (a *App) stopTrack(pos, dur time.Duration) {
	a.mu.Lock()
	r := a.report
	a.mu.Unlock()
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.sess == nil {
		return
	}
	cur := r.sess
	r.pushLocked(r.beatOf(cur, pos, dur, bili.PlayEnd, false))
	r.dropLocked()
}

// dropLocked 丢掉当前这一轮。调用方拿着 r.mu。
//
// 🔴 **这里不许涨代次**：刚塞进队列的那条结束心跳带着的正是**这一轮**的
// 代次，一涨它就在 `live` 那一关被自己的结束通知作废掉了——于是切歌、
// 停止、播完三种情况全都不发 PlayEnd，而 B 站那边这一轮的最后一次进度
// 就停在 30 秒前那个采样点上。
//
// 代次是**开新一轮**时涨的（`startLocked`）。那才是它该管的事：新一轮
// 一开，上一轮还在队列里的消息自动作废，而上一轮**已经排好的**那条结束
// 心跳排在前面，会先发出去。
func (r *playReporter) dropLocked() {
	r.sess = nil
}

// beatOf 按当前进度造一条心跳，顺手把这一轮「上一次发出去的刻」拨到现在。
//
// 顺带那一句是有讲究的：心跳的间隔是按 `lastAt` 算的，而 `lastAt` 必须算
// **上一条发出去的是什么**。暂停那一拍已经带着最新进度了却不拨时钟的话，
// 用户「播一分钟 → 暂停 → 继续」之后，下一拍就会紧跟着补一条 play_type=0，
// 而那一下和刚才那条继续说的是同一件事。
//
// 调用方拿着 r.mu。
func (r *playReporter) beatOf(s *reportSess, pos, dur time.Duration, act bili.PlayAction, finished bool) reportMsg {
	played := secsOf(pos)
	switch {
	case finished:
		played = bili.PlayFinished
	case act == bili.PlayEnd:
		// 🔴 结束那一条报的是「**听到了哪儿**」，不是「此刻的位置」。
		//
		// 切歌那一拍 `App.position` 已经被新歌清成 0 了（`runOnline` 就是
		// 这么写的），照它报出去，B 站那边这一轮就成了「从头开始听就停了」，
		// 而用户明明听到了 3 分 20 秒。用这一轮自己的最高水位就对了——
		// 那本来就是「这一轮听到哪儿了」的答案，不是从别处借来的数。
		played = s.maxPlayed
	}
	s.lastAt = time.Now()
	return reportMsg{
		kind: kindBeat, gen: s.gen,
		bvid: s.bvid, cid: cidOf(s.cid),
		start: s.startTS, id: s.id, mid: s.mid,
		played: played, dur: secsOf(dur), max: s.maxPlayed, action: act,
	}
}

// pushLocked 往队列里塞一条。**塞不进去就丢掉，绝不阻塞**（文件头「三」）。
func (r *playReporter) pushLocked(m reportMsg) {
	select {
	case r.out <- m:
		r.queued++
	default:
		log.Printf("biu: 播放记录队列满了，丢掉一条「%s」", m.action)
	}
}

// stopAccount 退登时停掉上报（`App.Logout` 调）。
//
// ⚠️ **不发结束**：这一轮记的是刚刚退掉的那个账号，而 jar 刚被清空，
// 发出去的那一下到 B 站那里是一次匿名请求。
func (r *playReporter) stopAccount() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	// 🔴 正在放的那一首标记成「别的账号开始的」（见 `orphan`）。少了这一句，
	// 用户退登之后原地换个人登录，这一首会替新账号重新开一轮——而那个人
	// 根本没听过它，进度是从上一个账号的播放位置接着涨的。
	if r.sess != nil {
		r.orphan = r.sess.bvid
	}
	r.gen++
	r.sess = nil
}

// finishOnClose 结掉这一轮，然后收工（`App.Close` 调）。
//
// ⚠️ **先结这一轮再停**：`close` 一执行那条 goroutine 就不会再发了，而退出
// 这一刻用户多半正听到一半——那一条 PlayEnd 是这一轮最后一次把进度写进
// B 站的机会。
func (r *playReporter) finishOnClose(pos, dur time.Duration) {
	r.mu.Lock()
	if !r.closed && r.sess != nil {
		r.pushLocked(r.beatOf(r.sess, pos, dur, bili.PlayEnd, false))
		r.dropLocked()
	}
	r.mu.Unlock()
	r.close()
}

// drain 等手上那些真的发出去，最多等 limit。
//
// 没有它的话退出那一刻排队里的最后一条会跟着进程一起消失。
func (r *playReporter) drain(limit time.Duration) {
	end := time.Now().Add(limit)
	for {
		r.mu.Lock()
		n := r.queued
		r.mu.Unlock()
		if n <= 0 || !time.Now().Before(end) {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// close 停掉发送那条 goroutine。可以重复调。
func (r *playReporter) close() {
	// ⚠️ **先把手上的发完再叫停**：叫停了这条 goroutine 就只走 `<-r.stop`
	// 那一支，排队里的东西会跟着进程一起消失。
	r.drain(reportDrain)

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	r.sess = nil
	close(r.stop)
	r.mu.Unlock()
	r.wg.Wait()
}

// ─────────────────────────────────────────────────────────────────────────────
// 发送：自己一条 goroutine，绝不在 tick 上
// ─────────────────────────────────────────────────────────────────────────────

// run 是唯一往 B 站发请求的地方。
//
// **一次只有它自己在跑**，所以「开一轮」里查出来的 aid 可以放进局部变量
// 给后面的心跳用——那两件事必须按顺序，而单条 goroutine 天然有序。
func (r *playReporter) run() {
	defer r.wg.Done()

	var curGen uint64
	var curAid int64

	for {
		select {
		case <-r.stop:
			return
		case m := <-r.out:
			curGen, curAid = r.deliver(m, curGen, curAid)
			r.sentOne()
		}
	}
}

// deliver 发一条消息，返回下一轮该带着的代次与 aid。
//
// ⚠️ **发之前对一遍代次和账号**：这一条是 `sync` 那一刻的快照，而它从队列里
// 出来时那一轮可能早就结束了（快进快出五首）。对不上就**安静丢掉**——
// 那一条记的是一首已经不听的歌，而 B 站会把它算进历史记录。
func (r *playReporter) deliver(m reportMsg, curGen uint64, curAid int64) (uint64, int64) {
	if !r.live(m) {
		return curGen, curAid
	}

	ctx, cancel := context.WithTimeout(context.Background(), reportTimeout)
	defer cancel()

	switch m.kind {
	case kindOpen:
		// aid 要另外问一次（bvid → aid，`ArchiveID`）。问不到就是这一首
		// 不记——记 `skip` 是为了别每 50 ms 再问一次。
		aid, err := r.cli.ArchiveID(ctx, m.bvid)
		if err != nil {
			log.Printf("biu: 问不到这一首的稿件 id，播放记录不记：%v", err)
			r.markSkipped(m.bvid)
			return curGen, curAid
		}
		curGen, curAid = m.gen, aid

		err = r.cli.PlayReportOpen(ctx, bili.PlayReportOpen{
			Mid: midOf(m.mid), Aid: aid, Bvid: m.bvid, Cid: m.cid,
			Session: m.id, StartTS: m.start,
		})
		if err != nil {
			log.Printf("biu: 播放记录开不了头：%v", err)
			r.markSkipped(m.bvid)
			return curGen, curAid
		}
		// 开完头紧接着补一条 PlayStart：「开始播」这件事 B 站那边是记在
		// 心跳的 play_type=1 上的，光有那一发 click 是不够的。
		r.sendBeat(ctx, curAid, m, bili.PlayStart)

	case kindBeat:
		if m.gen != curGen {
			return curGen, curAid // 这一轮的开头还没发出去（或者已经过去了）
		}
		r.sendBeat(ctx, curAid, m, m.action)
	}
	return curGen, curAid
}

// sendBeat 发一条心跳，失败只记日志。
func (r *playReporter) sendBeat(ctx context.Context, aid int64, m reportMsg, act bili.PlayAction) {
	err := r.cli.PlayReportBeat(ctx, bili.PlayReportBeat{
		Mid: midOf(m.mid), Aid: aid, Bvid: m.bvid, Cid: m.cid,
		Played: m.played, Realtime: m.played, RealPlayed: m.played,
		VideoDuration: m.dur, MaxPlayed: m.max,
		StartTS: m.start, Session: m.id, Action: act,
	})
	if err != nil {
		// ⚠️ **只记日志，不上报、不重试、不打断播放**（文件头「三」）。
		// 用户在地铁里不该为一条上报失败看见任何东西。
		log.Printf("biu: 播放记录发不出去（%s）：%v", act, err)
	}
}

// sentOne 记一条已经发完（哪怕是被代次丢掉的）。给 `drain` 数数用。
func (r *playReporter) sentOne() {
	r.mu.Lock()
	if r.queued > 0 {
		r.queued--
	}
	r.mu.Unlock()
}

// live 报这条消息发出去还有没有意义（这一轮还在、账号没变）。
func (r *playReporter) live(m reportMsg) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	if m.gen != r.gen {
		// ⚠️ **上一轮的结束那一条放行一格**。
		//
		// 切歌是在同一拍里做完两件事的：把上一轮结束掉、把这一轮开起来，
		// 而代次是在「开新一轮」那一刻涨的。于是 worker 捡到那条结束时
		// 代次已经过去了——不放行的话，**每一次切歌都不会发 PlayEnd**，
		// B 站那边每一首的最后一次进度都停在 30 秒前那个采样点上。
		//
		// 只给结束那一条放一格，而且只放一格：连开头一起放的话，用户飞快
		// 连点五首时前四首都会被开成一轮，B 站那边看过去是同一首歌听了五遍。
		if m.gen+1 != r.gen || m.action != bili.PlayEnd {
			return false
		}
	}
	// ⚠️ **再对一遍账号**：从 `sync` 塞进队列到这一行之间隔着一次网络往返
	//（问 aid），用户完全有时间在这中间退登。
	return m.mid != "" && m.mid == r.mid()
}

// markSkipped 记下「这一首开不了会话」，别反复问同一个稿件 id。
func (r *playReporter) markSkipped(bvid string) {
	r.mu.Lock()
	r.skip = bvid
	r.mu.Unlock()
}

// newReportSessionID 造一轮播放的会话 id。
//
// 一串**不带分隔符的小写十六进制**（32 个字符）——这个形状是 B 站那边对
// 会话 id 的约定，上游是 `crypto.randomUUID().replace(/-/g, "")`。
func newReportSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// secsOf 是时长换算成秒（B 站那几个接口都用秒）。
//
// ⚠️ **向下取整，不要四舍五入**：刚放 0.9 秒时发 1 秒，用户的进度条上会
// 出现一条「进度比位置靠前」的记录。
func secsOf(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return int64(d / time.Second)
}

// cidOf 把队列项里的分 P id（字符串）翻成上报要用的数字。
//
// 翻不出来就是 0，而 0 在协议层是「不发这一栏」——B 站拿 bvid 一样认得
// 是哪一稿。
func cidOf(s string) int64 { return parseInt64(s) }

// midOf 把 DedeUserID（字符串）翻成上报要用的数字。翻不出来是 0。
func midOf(s string) int64 { return parseInt64(s) }

func parseInt64(s string) int64 {
	if s == "" {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
