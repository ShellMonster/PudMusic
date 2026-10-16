// Package media 是系统媒体会话：把「现在在放什么」告诉 macOS，让控制中心和
// 锁屏显示出来，并且把系统从控制中心 / 锁屏 / 耳机线控发回来的播放命令
// 交回给业务层。
//
// # 分成两半，因为只有一半能测
//
//	media.go（平台无关）  状态与命令的形状、什么时候值得再发一次、命令怎么排队
//	*_darwin.go           macOS 那一半：MPNowPlayingInfoCenter + MPRemoteCommandCenter
//
// 平台无关的那一半是**能测的那一半**：去抖规则、命令的排队与分发，全都不需要
// 系统就能断。darwin 那一半只做翻译，翻错了要么系统不认、要么报不出来。
//
// # 它不认识界面，也不认识播放器
//
// 它只认一份自洽的快照（`NowPlaying`）和一个命令（`Command`）。谁把它填上、
// 收到命令之后去干什么，都在包外：`internal/app` 用 `App.SetMediaSink` 和
// `App.MediaCommand` 接线，见 `internal/app/media.go`。
//
// # 为什么纯 Go 自己写
//
// 系统接口是 `MediaPlayer.framework` 里两个 Objective-C 类，框架一行都不用改。
// 调用走 purego（MyGo 已经依赖它），`CGO_ENABLED=0` 也能构建。
// 调研与踩坑记在 `docs/research/GO-MEDIA-SESSION.md`。
package media

import (
	"log"
	"net/http"
	"sync"
	"time"
)

// State 是「现在到底在干什么」。
//
// 一一对应 `MPNowPlayingInfoPlaybackState`。分四个而不是「在放 / 不在放」两个，
// 是因为**停止和暂停对系统是两件事**：暂停的进度条停在原地，停止的进度条回到
// 开头，而用户按了停止之后控制中心的按钮应该回到「播放」。
type State uint8

const (
	// StateNone 是「没有歌在放」。这时不往系统发任何曲目信息——留着上一首的
	// 标题躺在锁屏上，用户看到的就是一个已经关掉的播放器还在放歌。
	StateNone State = iota
	// StatePlaying 正在出声。
	StatePlaying
	// StatePaused 暂停在原地。
	StatePaused
	// StateStopped 停了，并且回到了开头。
	StateStopped
)

// String 是这个状态的名字，给日志看。
func (s State) String() string {
	switch s {
	case StateNone:
		return "none"
	case StatePlaying:
		return "playing"
	case StatePaused:
		return "paused"
	case StateStopped:
		return "stopped"
	}
	return "?"
}

// Command 是系统从控制中心 / 锁屏 / 耳机线控发过来的播放命令。
//
// 只留真的接得住的：快进快退、播放速度、评分这些**不接**——接了就得给
// 一句人话，而这一版没有它们的意思。darwin 那一半会把它们显式关掉
// （`setEnabled:NO`），控制中心才不会摆出按了没反应的按钮。
type Command int

const (
	// CommandPlay 是「开始放」。已经在放的话什么都不用做——
	// 它**不是**切换：用户在控制中心点了播放而歌本来就在放，
	// 按切换就会把它按停。
	CommandPlay Command = iota
	// CommandPause 是「停下来但停在原地」。
	CommandPause
	// CommandToggle 是播放/暂停切换，耳机线控上那一个键走的是它。
	CommandToggle
	// CommandNext 下一首。
	CommandNext
	// CommandPrev 上一首。
	CommandPrev
	// CommandStop 停了并回到开头。
	CommandStop
	// CommandSeek 跳到指定位置。只有它带第二个参数有意义。
	CommandSeek
)

// String 是这个命令的名字，给日志看。
//
// 日志里写清楚**收到的是哪一个**不只是为了好看：耳机线控、控制中心的
// 播放/暂停按钮、键盘上的媒体花键，在 macOS 上全都落到这一组命令里，
// 记下名字才知道那次按的到底是哪条路过来的。
func (c Command) String() string {
	switch c {
	case CommandPlay:
		return "play"
	case CommandPause:
		return "pause"
	case CommandToggle:
		return "toggle"
	case CommandNext:
		return "next"
	case CommandPrev:
		return "prev"
	case CommandStop:
		return "stop"
	case CommandSeek:
		return "seek"
	}
	return "?"
}

// NowPlaying 是一份「现在在放什么」的快照。
//
// 全部字段都能直接比较（没有切片、没有指针），所以去抖就是一次 `==`。
//
// **它是快照不是引用**：darwin 那一半拿着它就能自己把字典拼完，中途不管
// 状态怎么变都不会拼出一个写了一半的字典。
type NowPlaying struct {
	// State 现在到底在干什么。StateNone 时下面这些字段一概不看。
	State State

	// Title 是歌名。锁屏和控制中心上最大那行字。
	Title string

	// Artist 是艺人。空的就不给系统（本地文件没有这个信息，
	// 见 `internal/app` 里 State.Artist 的注释）。
	Artist string

	// ArtworkURL 是封面地址。空的就不给封面。
	ArtworkURL string

	// Duration 是整首有多长。给系统它才知道进度条的刻度。
	Duration time.Duration

	// Position 是已经播到哪儿了。
	Position time.Duration
}

// position 是这个快照里**不算**语义变化的那一项：它每 50ms 都变一次。
//
// 去抖要拿它单独比（见 Session.Publish）。
func (n NowPlaying) identity() NowPlaying {
	n.Position = 0
	return n
}

// Options 是建会话时能给的东西。
type Options struct {
	// Fetch 怎么把一个封面地址变成字节。nil 表示自己走 HTTP GET。
	//
	// 单独留一个口子有两个用处：测试里能塞假封面；以后图片缓存那一层
	// 手上已经有图了，不必为了锁屏再下一遍。
	//
	// ⚠️ 自己接的时候注意凭据：封面在 B 站 CDN 上，**不要**挂任何带
	// SESSDATA 的客户端（理由和取流那一支一样，见 `app/online.go`）。
	Fetch func(url string) ([]byte, error)
}

// defaultFetch 是默认的取图方式。
//
// ⚠️ **Referer 要给**：B 站的图床不给就拒（和 CDN 上的音频一个脾气，真机验过；
// `imgcache` 那边也是这么发的）。Referer 不是凭据——它不含任何能代表用户的东西，
// 而缺了它就是一张都下不来。
//
// 除了 Referer 和 User-Agent，**什么别的都不给**：不带 cookie jar、不带任何
// 凭据。封面地址是公开的 CDN 地址（B 站给了就能看），把 SESSDATA 发给一台
// 存图的机器没有任何理由（`spec.md`「网络」一节，和 `app/online.go` 里的
// 媒体客户端是同一个道理）。
//
// 超时给 15 秒，理由和界面上的封面缓存一样（`imgcache.Options`）。
func defaultFetch(url string) ([]byte, error) {
	const timeout = 15 * time.Second
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", biliReferer)
	req.Header.Set("User-Agent", fetchUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &httpStatusError{Status: resp.Status}
	}
	return readAllLimit(resp.Body)
}

// biliReferer 是取 B 站图床时给的 Referer，和 `imgcache` 那边一致。
const biliReferer = "https://www.bilibili.com/"

// fetchUserAgent 是取图时给的 User-Agent。B 站的 CDN 对空 UA 会挑。
const fetchUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 " +
	"(KHTML, like Gecko) Version/18.0 Safari/605.1.15"

// platform 是平台那一半要提供的东西。
//
// 只留两个动作：**把快照交给系统**、**把系统的命令接上来**。别的都不归它管
// （去抖、排队、分发都在 Session 里），因为那些才是能出错的地方。
type platform interface {
	// apply 把一份快照写给系统。实现要能整体失败——系统没起来的时候
	// 整份媒体会话就是哑的，不该让界面也跟着哑。
	apply(NowPlaying)

	// attach 装上远程命令的处理器。handler 在 Session 的工作协程里跑，
	// 不在系统的回调线程上。
	attach(func(Command, time.Duration))

	// detach 把处理器摘掉，并清掉系统里那份曲目信息。
	detach()
}

// windowBinder 是「平台那一半要一个**具体的窗口**」的那一种。
//
// **不是 `platform` 接口的一部分**，因为 macOS 不需要：那边的
// `MPNowPlayingInfoCenter` 是全局的，会话一建就和窗口无关。而 Windows 的
// SMTC 是**按窗口**的（`GetForWindow(HWND, …)`），任务栏缩略图工具栏更是
// 直接长在某个 HWND 上——所以那边要等窗口建出来。
//
// 用一个可选接口而不是往 `platform` 上加一个方法，是为了不给 macOS 塞一个
// 它永远不会用到、还得注释半天解释「为什么这里是空的」的方法。
type windowBinder interface {
	// bindWindow 接到一个窗口上。实现要能整体失败。
	bindWindow(uintptr) error
}

// BindWindow 把会话接到一个窗口上。h 在 macOS 上是 `NSWindow*`、在 Windows
// 上是 `HWND`——**只有 Windows 那边看它**，别的平台原样忽略（见 windowBinder）。
//
// ## 什么时候调：窗口建出来**之后**
//
// 这一句是 Windows 那一半和 macOS 那一半最大的不同，所以单独说明：
// `ui.MountMedia` 在开窗口之前调（macOS 要在那里注册 Objective-C 的类，
// 有回调在跑的时候再注册会直接 SIGTRAP），而 Windows 这边要等 `ui.Run`
// 里那个窗口建出来才有句柄可用。
//
// 两边都要照顾到，所以顺序是：先 `New`（macOS 在那里把该注册的注册完），
// 窗口出来之后再 `BindWindow`（Windows 在那里把系统会话建起来）。
// 补推的那一下见 smtc_windows.go 的 replayState。
//
// 返回 false 表示这一轮系统那一半没接上（记了日志）。**不影响别的**：界面、
// 播放、队列都跟它无关，会话这时就是个哑巴。
//
// 重复调是安全的：上一次那个窗口会先被收干净。
func (s *Session) BindWindow(h uintptr) bool {
	s.mu.Lock()
	p := s.p
	s.mu.Unlock()

	b, ok := p.(windowBinder)
	if !ok {
		return true
	}
	if err := b.bindWindow(h); err != nil {
		log.Printf("biu: 系统媒体会话没接上这个窗口（浮窗、锁屏、任务栏都没有）：%v", err)
		return false
	}
	return true
}

// Session 是系统媒体会话。
//
// 零值不可用，请用 New。
type Session struct {
	mu sync.Mutex

	// p 是平台那一半。没有它（别的平台、或者系统那一半没起来）时，
	// 整个 Session 是个哑巴：Publish 不报错也不做别的，命令不来。
	p platform

	// on 是收到命令之后叫它。由 `internal/app` 装（`App.MediaCommand`）。
	on func(Command, time.Duration)

	// last / have 是上一份发过的快照，用来去抖。
	last NowPlaying
	have bool

	// pending 是命令队列，worker 是唯一从它取命令的协程。
	//
	// ⚠️ **命令不能直接在系统的回调线程上跑**。系统是在它自己那条线程上
	// 把命令递过来的，而跳位置在在线流那一条路上会发网络请求（跳到还没下
	// 到的位置要去拉 Range），在回调线程上做就是界面卡住。挪到这里之后，
	// 顺带还保证了顺序：连按两下不会变成「播放、暂停」反过来执行。
	pending  chan queued
	closed   chan struct{}
	workerWG sync.WaitGroup
}

// queued 是一条等着执行的命令。
type queued struct {
	cmd Command
	at  time.Duration
}

// New 建一个系统媒体会话。
//
// **要在开窗口之前调**：平台那一半要在那里做 Objective-C 的类注册和方法
// 注册，而注册必须在还没有 ObjC 回调在跑的时候做（实测：有回调在跑的时候
// 再注册会直接 SIGTRAP）。窗口一开，回调就有了。
func New(opts Options) *Session {
	p, err := newPlatform(opts)
	if err != nil {
		// 系统那一半没起来不该拦住应用开窗：媒体会话是加分项，
		// 界面、播放、队列都跟它无关。
		log.Printf("biu: 系统媒体会话没起来，这一轮没有控制中心和锁屏控制：%v", err)
		return newSession(nil)
	}
	return newSession(p)
}

// newSession 拿一个已经装好的平台建会话。New 和测试都走它。
func newSession(p platform) *Session {
	s := &Session{
		p:       p,
		pending: make(chan queued, 16),
		closed:  make(chan struct{}),
	}
	if p != nil {
		s.workerWG.Add(1)
		go s.work()
	}
	return s
}

// Available 报告这一轮系统媒体会话是不是真的装上了。
//
// 没有它的时候 Session 是个哑巴：Publish 什么都不做、系统也不会发命令来。
// 调用方（界面层）用它决定要不要记一条日志——「装不上」和「装上了但没反应」
// 是两回事，前者要有人说。
func (s *Session) Available() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.p != nil
}

// SetHandler 装上「系统发来命令时叫谁」。整个进程装一次。
func (s *Session) SetHandler(fn func(Command, time.Duration)) {
	s.mu.Lock()
	s.on = fn
	p := s.p
	s.mu.Unlock()
	if p != nil && fn != nil {
		p.attach(s.dispatch)
	}
}

// dispatch 是平台那一半叫的入口：把命令排进队列就立刻返回。
//
// 返回得这么快是有意的：系统的回调线程不是我们的线程（多半是主线程），
// 在那里等一个可能要走网络的活儿就是界面卡住。
func (s *Session) dispatch(cmd Command, at time.Duration) {
	q := queued{cmd: cmd, at: at}
	select {
	case s.pending <- q:
		return
	default:
	}
	// 队列满了（连按了几十下的用户才会走到这儿）。不能丢——丢一条「暂停」
	// 的后果是歌还在放而按钮显示暂停，比卡一下界面糟得多；也不能直接阻塞，
	// 那就卡在系统的回调线程上了。丢给一个专门的协程去等。
	go func() {
		select {
		case s.pending <- q:
		case <-s.closed:
		}
	}()
}

// work 是唯一执行命令的协程。
func (s *Session) work() {
	defer s.workerWG.Done()
	for {
		select {
		case <-s.closed:
			return
		case q := <-s.pending:
			// ⚠️ 这一句不能省：`select` 在两个 channel 都ready 时是**随机挑**
			// 的，所以「队列里已经有一条」和「正在关闭」同时成立时，它可能
			// 挑中队列那条然后照样执行一遍。退出流程走完之后再往一个正在拆
			// 的东西上动手，后果是关不掉或者崩在收尾里。
			select {
			case <-s.closed:
				return
			default:
			}
			s.mu.Lock()
			fn := s.on
			s.mu.Unlock()
			if fn == nil {
				continue
			}
			fn(q.cmd, q.at)
		}
	}
}

// seekThreshold 是「位置跳了这么多就算是一次跳转」的界线。
//
// 系统会从上一次报的位置和播放速率**自己外推**进度（Apple 头文件原话：
// "the elapsed time will be automatically extrapolated … so updating this
// property frequently is not required"）。所以每 50ms 报一次位置是白费的，
// 但一次跳转不报的话，锁屏上的进度条会慢慢飘走——外推的是**系统的时间**，
// 我们这边真要换流、卡一下解码，它就不知道了。
//
// 取 2 秒：正常推进一次是几十毫秒，远不到；拖一下进度条、或者 5 秒一次的
// 校正，都稳稳超过。
const seekThreshold = 2 * time.Second

// Publish 把「现在在放什么」交给系统。
//
// ⚠️ **位置之外的一切都一样的时候，这次 Publish 什么都不会发。** 规则在
// shouldPublish 里，是这一层最需要测的部分（见 media_test.go）：写多了
// 系统白白重建字典，而写少了锁屏上的进度条会停在原地不动。
//
// 第一次调用一定会发：没有「上一份」可比的时候不算变化。
func (s *Session) Publish(n NowPlaying) {
	s.mu.Lock()
	send := s.shouldPublishLocked(n)
	if send {
		s.last, s.have = n, true
	}
	p := s.p
	s.mu.Unlock()

	if !send || p == nil {
		return
	}
	p.apply(n)
}

// shouldPublishLocked 判这一次要不要真发。必须在持锁时调。
func (s *Session) shouldPublishLocked(n NowPlaying) bool {
	if !s.have {
		return true
	}
	prev := s.last
	// 歌名、艺人、封面、时长、状态：任何一样变了都要重发——这些是锁屏上
	// 看得见的字段，不重发就一直显示旧的。
	if prev.identity() != n.identity() {
		return true
	}
	// 不在放的时候，系统不会外推：位置变了不重发，进度条就永远停在旧位置。
	if n.State != StatePlaying && prev.Position != n.Position {
		return true
	}
	// 在放的时候，只有跳得够远才重发（见 seekThreshold）。
	if n.State == StatePlaying && absDuration(n.Position-prev.Position) >= seekThreshold {
		return true
	}
	return false
}

// absDuration 是两个时长之差的绝对值。
func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// ---- 系统的词汇 ----
//
// 这一段**刻意留在平台无关的那一半**：值是 macOS 的，可它们是这一层对外
// 的词汇表的一部分（「在放」在系统那边是哪一个数、处理器返回什么算「收到了」）。
// 放在这里就能在没有 MediaPlayer 的机器上断映射对不对，而断映射的后果
// （控制中心状态一直停在「暂停」）在真机上要按半天才知道。

// `MPNowPlayingInfoPlaybackState` 的值。
//
// ⚠️ macOS 独有，而且**必设**：Apple 头文件原话是 macOS 上
// "playback state cannot be determined by the application's audio session"。
// 不设的话控制中心的按钮状态会错，远程控制可能整个失灵。
const (
	sysStateUnknown     = 0
	sysStatePlaying     = 1
	sysStatePaused      = 2
	sysStateStopped     = 3
	sysStateInterrupted = 4
)

// playbackStateNumber 把我们的话翻成系统的话。
//
// StateNone 翻成 **Stopped** 而不是 Unknown：没有歌的时候，系统那边最
// 该知道的是「这个应用现在什么都没在干」，而 Unknown 在控制中心上会
// 留下一个不知道该显示播放还是暂停的按钮。
func playbackStateNumber(s State) int {
	switch s {
	case StatePlaying:
		return sysStatePlaying
	case StatePaused:
		return sysStatePaused
	case StateStopped, StateNone:
		return sysStateStopped
	}
	return sysStateUnknown
}

// `MPRemoteCommandHandlerStatus` 的值。
//
// ⚠️ 成功是 **0**，不是 1。写错的话系统会把每一次成功的处理当成失败，
// 而症状是「按了没反应」——连日志都不会有。
const (
	// statusSuccess 是「收到了，接下了」。
	statusSuccess = 0
	// statusNoActionableItem 是「现在没有能操作的东西」。
	statusNoActionableItem = 110
	// statusCommandFailed 是「收到了但做不了」。
	statusCommandFailed = 200
)

// Clear 告诉系统「没有歌了」，并把远程命令摘掉。
//
// 退出前一定要调：不清的话锁屏上会留着一首已经不存在的歌，而控制中心
// 上的播放键还按下去还能收到命令。
func (s *Session) Clear() {
	s.mu.Lock()
	s.last, s.have = NowPlaying{}, false
	p := s.p
	s.mu.Unlock()
	if p == nil {
		return
	}
	p.detach()
	// ⚠️ 「告诉系统没有歌了」这件事**必须**在这里发，不能指望平台那一半在
	// detach 里顺手做：那是个约定而不是契约，平台实现一改，锁屏上就留着
	// 一首已经不存在的歌——而界面这边一点异样都看不出来。
	p.apply(NowPlaying{State: StateNone})
}

// Close 收掉整个会话。可以重复调用。
func (s *Session) Close() {
	s.mu.Lock()
	if s.closed == nil {
		s.mu.Unlock()
		return
	}
	select {
	case <-s.closed:
		s.mu.Unlock()
		return
	default:
	}
	close(s.closed)
	s.mu.Unlock()

	s.Clear()
	s.workerWG.Wait()
}
