// 顶栏上的「动态」按钮 + 更新红点：Biu 的 N-07
// （`biu/src/layout/navbar/user-feed/index.tsx`）。
//
// Biu 那一支是一个 `useRequest`，`pollingInterval: 300000`、`pollingWhenHidden:
// false`，每 5 分钟问一次「有没有新动态」，有就在图标右上角点一个圆点；点进去
// 看动态时把点清掉。
//
// ## 这个点以前**从来没亮过**，原因是**一件**事
//
// **数据源没接**：`fetch` 是 nil（`newFeedPoller(dynamicFeedInterval, nil)`），于是
// `count` 恒为 0，而画点的那一句是 `if count > 0`——那一枚点从写下来到现在**一次都
// 没有被画出来过**。
//
// ⚠️ **还有一条是另一个症状，别混成一条**：上游把「看过没有」放在内存里
// （`user-feed/index.tsx:12` 的 useState），于是**点掉之后**下一轮又问回同一个
// 数，五分钟之后那个点又回来（用户明明看过了）。那一条解释的是「点了又回来」，
// **不是**「从来没亮过」——我们这里把基准落盘（见 `internal/app/dynamicseen.go`）。
//
// ⚠️ **还有一条查过、它也不是原因**：「问的时机」——Biu 那个 `useRequest` 挂上就发
// 第一次，我们这一版也是（`tick` 里零值的 `next` 就是「马上」）。但接上数据源
// 之后它**必须**是「打开 app 就问一次」，否则用户第一次打开要等五分钟。
//
// ## 「看过」的基准是**一条动态的 id**，不是时间戳
//
// B 站那个「有没有新动态」的接口（/feed/all/update）按一个**基线**算「有多少条
// 比它新」，而那个基线就是**动态 id**。**契约在 Biu 上游源码里**（不用去查社区
// 文档）：`biu/src/service/web-dynamic.ts:35-36` 写着那个参数「默认为上次请求
// 获取全部动态列表 返回的 update_baseline」，而 `:58` 写着那个返回是
// `update_baseline: string; // 更新基线 (第一条记录ID)`——**最新那条动态的 id**。
//
// ⚠️ **传错形状的后果是静默的**：时间戳不是合法的 id，于是 B 站要么回一个错
// （那个点再也不亮），要么当成「比它新的都算」（那个点永远亮着）——用户看到的
// 只是「那个点不对」。
//
// ⚠️ **上游 Biu 永远传 0**（`user-feed/index.tsx:18`），所以它从没暴露过这个
// 参数该传什么形状：那一档永远是「全部」，于是点掉之后五分钟又回来。
//
// ## 查询是**异步**的：界面线程一次都不等它
//
// 查询是一次网络请求，而问它的是 view（界面线程）。在界面线程上等它等于把
// 整个应用冻住十秒——用户看到的是「应用卡住了」，不是「那个点没亮」。所以
// `ask` 立刻返回，结果从后台投进信箱，界面线程下一帧收走（铁律三：投递完
// 必须叫醒）。
//
// ## 节奏是「帧驱动 + 到点叫醒」，两半缺一不可
//
// `tick` 只看「现在到没到下一轮」，问它的是 view——而 view 只在有帧的时候跑。
// 只做这一半的话，窗口静止时 MyGo 根本不画帧，于是**到点了也不会有人去轮询**：
// 红点要等到下一次碰巧有东西重画时才亮，而不是满 5 分钟就亮（实测：静置一分钟
// 灌进 3 条更新，画面上什么都没有）。
//
// 所以另一半是 `arm`：每 `interval` 一次 `EveryFunc`，回调里**只**调
// `wake.invalidate()` 要一帧，下一帧 view 自然就会问一次 `tick`。
//
// ⚠️ 回调里一个字都不许写 Shell 的字段（clock.go 的硬规矩）：定时器跑在后台
// goroutine 上，而界面状态是主线程单点写入的（铁律一）。`wakeHandle.invalidate`
// 自带锁，它内部存的是窗口的弱引用，所以这条唤醒链是并发安全的。
package ui

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"sync"
	"time"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// dynamicFeedInterval 是红点轮询的间隔。Biu 的 `pollingInterval: 300000`
// （`user-feed/index.tsx:24`），就是 5 分钟。
const dynamicFeedInterval = 5 * time.Minute

// dynamicDotSize 是红点多大。**8**。
//
// ⚠️ **上游那个数没写在 Biu 的源码里**：Biu 那一枚是 HeroUI 的
// `<Badge isDot size="sm">`（`user-feed/index.tsx:52-60`），圆点的大小在
// HeroUI 的主题里（sm 档是 `h-2 min-w-2`，也就是 8×8）。8 也是顶栏这个高度里
// 点得出来又不抢戏的一个数——**两边对上了**，所以就用它。
const dynamicDotSize = 8

// dynamicDotNudge 是那个点的中心从「角上」往里挪多少。
//
// 上游那一枚挂在按钮的右上角，还带了一句 `translate-x-1/3 -translate-y-1/3`
// （`user-feed/index.tsx:59`）：圆点的**右上角**在按钮的右上角，往右挪三分之一、
// 往上挪三分之一——于是圆点的**中心**落在角上往里 1/6 的地方。
//
// `Attach(AnchorTopRight, AnchorCenter)` 把圆点的中心摆在角上，所以这里要往里挪
// 六分之一（8 的六分之一是 1.33）。⚠️ **MyGo 的正负号和 CSS 的 inset 一样**：
// `Right(1.33)` 是往**左**挪、`Top(1.33)` 是往**下**挪。
const dynamicDotNudge = float32(dynamicDotSize) / 6

// dynamicFeedPath 是用户动态那一页的路由，和 router.go 里登记的是同一个字符串。
//
// 拿字符串比而不是拿 `routes[8]`：那条表一改顺序这里跟着错，而且错得很安静。
const dynamicFeedPath = "/dynamic-feed"

// feedPoller 是「动态更新红点」的节奏。
//
// **只由 view 在主线程上读写**，不跨 goroutine（和 Shell 里其余的界面状态一样）——
// 除了底下那一个信箱：后台那一趟把结果投进去，界面线程下一帧收走。
type feedPoller struct {
	// interval 是两轮之间隔多久。
	interval time.Duration

	// ask 发起一次查询，**立刻返回**。
	//
	// ⚠️ **它不许阻塞**：调它的是 view（界面线程），而查询是一次网络请求——
	// 在界面线程上等它等于把整个应用冻住（用户看到的是「卡住了」，不是「那个点
	// 没亮」）。
	//
	// 结果通过 done 回来，**done 是在后台 goroutine 上调的**：界面线程在下一帧
	// 收走它（见 drain）。
	ask func(done func(count int, err error))

	// catchUpAsk 是「用户追上来了」那一趟：拉一次动态流的第一页，回它最新那条
	// 动态的 id。
	//
	// ⚠️ **它和 poll 是两回事**：poll 问「比基线新的有几条」，catchUp 问「现在最新
	// 的是哪一条」——后者是**基线本身**的来源（基线是动态 id，不是时间戳）。
	//
	// ⚠️ **为什么点那个点要问它**：用户点那个点的时候他在说「我看到这儿了」，而
	// 「用户动态」那一页可能十分钟前就开着（它自己不刷新）——于是光靠那一页记下来
	// 的 id 是旧的，B 站会一直说「还有新的」，**那个点每五分钟回来一次**。
	catchUpAsk func(done func(id string, err error))

	// wake 是「结果回来了，叫醒界面」那一路。
	//
	// ⚠️ **它只在建这个节奏的时候写一次**（newFeedPoller），之后**只读**。
	//
	// 为什么不能「每帧装一次」（原来的写法）：那一格是**裸的共享字段**——
	// 界面线程每帧写它、后台那一趟的回调读它，而两者之间没有锁、也没有
	// happens-before，`-race` 直接报竞态。
	//
	// ⚠️ **而且那个写法还有一个更要命的症状**：第一帧里 `tick` 先发起那一趟
	// （`arm` 排在它后面），于是后台读到的还是 **nil** —— 唤醒被跳过，那个结果
	// 躺在信箱里没人取，要等下一次有帧才亮（下一帧可能就是五分钟之后）。
	// 那正是这一票要治的「看不见」。
	//
	// 现在它**建的时候就有**，而后台那一趟**不读这一格**：start 把值拷进闭包，
	// 闭包带的是那份拷贝（见 start）。
	wake func()

	// stop 是「到点叫醒界面」那个定时器的停法，nil 表示没装。
	//
	// 只在主线程上读写（arm / disarm 都由 view 调），后台那个定时器只调 wake，
	// 不碰这里——铁律一。
	stop func()

	// next 是下一次该轮询的时刻。零值表示「马上就该轮一次」——Biu 那个
	// useRequest 挂上就发第一次，不是等 5 分钟才发。
	next time.Time
	// count 是上一次拿回来的条数，0 就不画红点。
	count int

	// busy 是「有一趟在途」。
	//
	// ⚠️ **它挡的是「问得太勤」**：一轮的网络往返比五秒钟短得多，这一趟在途
	// 的时候下一轮又到了的话，不挡就是两个请求叠着回来——而回来的那个数是按
	// **发出去那一刻**的基准算的，后发的那一趟才准。
	busy bool

	// gen 是「用户清过点」的次数。
	//
	// 清点时 +1，在途的那一趟带着它发出去，回来时对不上就作废——用户已经看过
	// 了，那一趟的数是「他还没看的时候」的，拿它把点点亮就是「点了又冒出来」。
	gen int

	// login 是上一次问的时候登录态是什么。变了就把下一轮提前（见 syncFeedDot）。
	login bool

	// catchingUp 是「有一趟 catchUp 在途」。
	catchingUp bool

	// box 是后台投回来的结果。
	boxMu sync.Mutex
	box   []feedUpdate
}

// feedUpdate 是后台那一趟回来的结果。
type feedUpdate struct {
	// count 是「比基线新的那几条」。
	count int
	// err 是这一趟的错。错的那几趟**不改 count**（见 drain）。
	err error
	// gen 是发出去那一刻的「清点次数」。
	gen int
	// id 是「此刻最新那条动态的 id」（catchUp 那一趟）。
	id string
	// caughtUp 标出「这是 catchUp 那一趟」。
	caughtUp bool
}

// newFeedPoller 建一个节奏。ask 为 nil 就是一个永远问不出东西的空拉。
//
// wake 是「结果回来了叫醒界面」那一路，**建的时候就装好**（理由见 wake 那一格）。
func newFeedPoller(
	interval time.Duration,
	ask func(done func(count int, err error)),
	catchUpAsk func(done func(id string, err error)),
	wake func(),
) *feedPoller {
	return &feedPoller{interval: interval, ask: ask, catchUpAsk: catchUpAsk, wake: wake}
}

// tick 到点就轮一次，返回这一帧该画几个红点。
//
// 下一轮的时刻从**轮完这一刻**起算（`now + interval`），不是从上一轮推——
// 两种算法在准点时一模一样，差的是「轮完这一次花了 300ms」之后：按上一轮推会
// 让实际间隔越缩越短，最后快到连着发。
func (p *feedPoller) tick(c clockSource) int {
	p.drain()
	now := c.Now()
	if now.Before(p.next) {
		return p.count
	}
	p.next = now.Add(p.interval)
	p.start()
	return p.count
}

// start 发起一次查询。**立刻返回**。
func (p *feedPoller) start() {
	if p.ask == nil || p.busy {
		return
	}
	p.busy = true
	gen := p.gen
	// ⚠️ **把唤醒那一路拷进闭包**：后台那一趟读的是这份**拷贝**，不是 `p.wake`
	// 那一格——于是这一格从头到尾只有界面线程碰它（`-race` 干净，见 wake 那一格）。
	wake := p.wake
	p.ask(func(count int, err error) {
		p.boxMu.Lock()
		p.box = append(p.box, feedUpdate{count: count, err: err, gen: gen})
		p.boxMu.Unlock()
		// 🔴 铁律三：投递完**必须**唤醒。只投递不唤醒 = 界面停在旧帧。
		if wake != nil {
			wake()
		}
	})
}

// catchUp 发起「用户追上来了」那一趟：拉一次动态流的第一页，拿它最新那条的 id。
//
// ⚠️ **它不问「有没有新的」**：它问的是「现在最新的是哪一条」——那个 id 是**基线
// 本身**（B 站的 update_baseline 要的就是动态 id，不是时间戳）。
//
// ⚠️ **基线是应用层落盘的**（`App.CatchUpDynamicSeen`）：这一层只管「它回来了」
// ——回来之后把点清掉，并且把在途的那一趟作废（它问的是旧的基线）。
func (p *feedPoller) catchUp() {
	if p.catchUpAsk == nil || p.catchingUp {
		return
	}
	p.catchingUp = true
	gen := p.gen
	p.catchUpAsk(func(id string, err error) {
		p.boxMu.Lock()
		p.box = append(p.box, feedUpdate{id: id, err: err, gen: gen, caughtUp: true})
		p.boxMu.Unlock()
		if p.wake != nil {
			p.wake()
		}
	})
}

// drain 收走后台投回来的结果。**只能由界面线程调**。
func (p *feedPoller) drain() {
	p.boxMu.Lock()
	box := p.box
	p.box = nil
	p.boxMu.Unlock()
	if len(box) == 0 {
		return
	}
	for _, u := range box {
		if u.caughtUp {
			p.catchingUp = false
			if u.err != nil {
				log.Printf("biu: 追动态失败了：%v", u.err)
				continue
			}
			// ⚠️ **用户追上了**：那个点不该亮。
			//
			// 而基线本身已经由应用层落盘了（App.CatchUpDynamicSeen）——这一层
			// 只管「把它清掉」和「让在途的那一趟作废」（那一趟问的是旧的基线，
			// 拿它的答案把点点亮就是「点了又冒出来」）。
			p.count = 0
			p.gen++
			continue
		}
		p.busy = false
		if u.gen != p.gen {
			// 用户在这一趟回来之前看过动态了：那一趟的数是旧的，作废。
			continue
		}
		if u.err != nil {
			// ⚠️ **失败不改 count**：一次网络抖动不该把用户已经看见的点弄掉。
			// （上游是 `onError` 就 `setUpdateCount(0)`——那是「问不到就当没有」，
			// 于是断网时那个点会闪一下又没了，而用户明明还有没看的动态。）
			log.Printf("biu: 问「有没有新动态」失败了：%v", u.err)
			continue
		}
		p.count = max(u.count, 0)
	}
}

// clear 把红点清掉。用户点进去看动态之后就不该再点第二次。
func (p *feedPoller) clear() {
	p.count = 0
	// ⚠️ **不清 next**：清了就变成「立刻再轮一次」，而这一轮是刚轮完的。
	// Biu 也是只 `setUpdateCount(0)`（`user-feed/index.tsx:38`）。
	//
	// ⚠️ **gen +1**：在途的那一趟回来时带着旧的 gen，作废——那一趟的数是
	// 「用户还没看的时候」的，拿它把点点亮就是「点了又冒出来」。
	p.gen++
}

// arm 装上「到点叫醒界面」的那一路，已经装过就什么都不做。
//
// 每一帧都调是安全的：装上了就直接返回。装完的那一趟从**这一刻**起每 interval
// 醒一次，所以定时器和轮询时刻始终差着一个恒定的偏移（第一次装上的那一帧），
// 而那个偏移小到看不见。
//
// ⚠️ **它不收唤醒函数当参数**：那一格（`p.wake`）建的时候就有，每帧再传一份
// 过来等于每帧造一个方法值（它会逃到堆上——顶栏那一排每帧两次，帧预算那一条
// 盯着分配数）。
func (p *feedPoller) arm(c clockSource) {
	if p.stop != nil {
		return
	}
	p.stop = c.EveryFunc(p.interval, p.wake)
}

// disarm 停掉那一路。窗口真关掉之后 `Invalidate` 对它是 no-op，但它还会一直每
// 五分钟醒一次到进程退出，所以要在收尾的地方停掉（见 shell_window.go 的 Attach）。
func (p *feedPoller) disarm() {
	if p.stop != nil {
		p.stop()
		p.stop = nil
	}
}

// syncFeedDot 是红点那个轮询的每帧副作用：收后台的结果、问一次有多少条、
// 装上「到点叫醒界面」的那一路。
//
// ⚠️ **它原来在 `viewDynamicDot` 里**，而顶栏在满屏开着的时候不画（票 #161）——
// 不挪的话，用户在满屏里看频谱的那几分钟里，红点那个「每五分钟问一次」就停了，
// 关掉满屏之后要再等五分钟才补上。挪到这里它就一直问着。
//
// ⚠️ **它不碰 `ui.*`**：这一层只做「问一次 + 装钟」，画红点那一枚是顶栏的事。
func (s *Shell) syncFeedDot() {
	// 登录态变了就把下一轮提前：**刚登录上就该问一次**，而不是干等五分钟。
	//
	// ⚠️ 用户扫码登录之后最想看的就是「有没有新动态」；而没登录时那一趟压根
	// 不发（见 askNewDynamics），所以不提前的话他要等满五分钟。
	loggedIn := s.app.LoggedIn()
	if s.feed.login != loggedIn {
		s.feed.login = loggedIn
		s.feed.next = time.Time{}
	}
	s.feed.tick(s.clock)
	// ⚠️ 这一句是「窗口静止时也会轮询」的全部保证：光靠上面那一行，静止的窗口
	// 永远没人问。到点的那一趟把界面叫醒，下一帧上面就会问。
	//
	// ⚠️ **关了之后不许再装**（`s.closed`）。`Close` 收掉的是一个具体装上的
	// 定时器，而这一句是**每帧**都会到的——所以只要关掉之后还有一帧画到
	// （无头 Tester 里 `Close` 之后再 `Frame()` 就会，见
	// `close_while_loading_test.go`），它就会在这儿再装一个，而 `Close` 已经走过
	// 一趟、不会再走。那一个就挂到测试进程结束（真窗口里 OnClosed 之后不会再有帧，
	// 可这条不该靠「恰好不会」来对）。
	if !s.closed {
		s.feed.arm(s.clock)
	}
}

// askNewDynamics 发起一次「有没有新动态」的查询。
//
// ⚠️ **没登录时一个请求都不发**：动态流是「我关注的人在发什么」，没有登录态
// 就没有这个列表，而 B 站对那个接口回 -101——那个 -101 在别的层和「网络断了」
// 长得一模一样。
//
// ⚠️ **查询在后台 goroutine 里跑**：界面线程一次都不等它（理由见 feedPoller
// 那一段的「查询是异步的」）。
func (s *Shell) askNewDynamics(done func(count int, err error)) {
	if !s.app.LoggedIn() {
		done(0, nil)
		return
	}
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("biu: 问「有没有新动态」崩了：%v\n%s", rec, debug.Stack())
				done(0, fmt.Errorf("问新动态的代码崩了：%v", rec))
			}
		}()
		n, err := s.app.NewDynamicCount(context.Background())
		done(n, err)
	}()
}

// askCatchUp 发起「用户追上来了」那一趟：拉一次动态流的第一页，拿它最新那条的
// id 当「看过基准」。
//
// ⚠️ **它在后台 goroutine 里跑**：那是一次网络请求，而在界面线程上等它等于把
// 整个应用冻住（理由见 feedPoller 那一段的「查询是异步的」）。
//
// ⚠️ **落盘是应用层做的**（`App.CatchUpDynamicSeen`）：它知道「基线」是什么、
// 落在哪；这一层只管「它回来了没有」。
func (s *Shell) askCatchUp(done func(id string, err error)) {
	if !s.app.LoggedIn() {
		done("", nil)
		return
	}
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("biu: 追动态崩了：%v\n%s", rec, debug.Stack())
				done("", fmt.Errorf("追动态的代码崩了：%v", rec))
			}
		}()
		id, err := s.app.CatchUpDynamicSeen(context.Background())
		done(id, err)
	}()
}

// viewDynamicDot 是顶栏上的「动态」那一格：有新动态时右上角一个圆点。
func (s *Shell) viewDynamicDot(c *ui.Context) {
	s.syncFeedDot()

	// Biu 只在登录后摆这一格（`navbar/index.tsx:38` 的 `{Boolean(user?.isLogin) && ...}`）。
	// 这里**没按登录态隐藏**——理由是**这一格自己的**：红点这一格不点它也只是一次
	// 轮询，没登录时轮回来的是 0，红点自然不亮；藏掉它省不掉那一趟请求，只会让
	// 顶栏右边少一格。
	//
	// ⚠️ **它和头像下拉那几项不是同一个理由了**（票 #260）：头像下拉改成照上游
	// 那样**按登录态隐藏**了（已登录没有「登录」，未登录没有「个人资料」「退出
	// 登录」，见 `navbar_avatar.go` 的文件头），而这一格**仍然一直都在**。
	// 那个取舍是这一格自己定的，不是跟着下拉一起定的。
	//
	// 代价说清楚：登录之后这一格还在，而 Biu 那时候已经把它藏了。
	ui.Row(c).Key("nav-feed").Gap(space.Px4).AlignItems(ui.Start).Children(func() {
		ui.Column(c).Key("nav-feed-btn").Children(func() {
			// 上游那一枚是 user-feed 的 `Tooltip closeDelay={0}`
			// （`navbar/user-feed/index.tsx:50`）：菜单那一档，指针一走就收。
			//
			// ⚠️ **走 `iconKey`（悬停时图形变主色）**，不是 `iconButton`（票 #249）：
			// 顶栏这一排和播放条上是同一个「图标键」，配方得是同一档。
			if s.withMenuTip(c, iconKey(c, iconFeed, "动态", iconMd, false).
				Size(iconHitMd, iconHitMd).Padding(space.None, space.Px7), "动态").Clicked() {
				// Biu 的 handleOpen：清掉红点，再去看动态。
				s.feed.clear()
				// ⚠️ **还要把「看到哪儿了」记下来**：只清本地那个数的话，下一轮
				// 又问回同一个数，点过五分钟那个点又冒出来（上游就是那样）。
				//
				// 而「看到哪儿了」是**一条动态的 id**（B 站的 update_baseline 要的
				// 是那个，不是什么时间戳）——它从「此刻最新那条动态」那儿来。
				//
				// ⚠️ **只有那一页已经开着的时候才要现拉一次**：那一页开着的时候它
				// 自己不会刷新（它只在第一次进去时拉第一页），于是「看过基准」停在
				// 旧的那一条 → B 站一直说「还有新的」→ **那个点每五分钟回来一次**
				//（票面「点了不该马上又冒出来」）。
				//
				// 而那一页**还没开过**的时候不用管：点过去它自己会拉第一页，而那一页
				// 的 update_baseline 就是基准（见 dynamicfeed.go 的 adoptFeed）。
				if s.dynamicFeedOf().started {
					s.feed.catchUp()
				}
				s.navigate(dynamicFeedPath)
			}
			// ⚠️ **这里读的是 `s.feed.count`，不是刚进来时记下的那一份**：这一帧
			// 里上面那一句可能刚把它清掉，而手里那份是清之前的——于是「点一下就
			// 清掉」要等到下一帧才生效（画面上那个点会闪一下）。
			if s.feed.count > 0 {
				s.viewDynamicDotMark(c, s.feed.count)
			}
		})
	})
}

// viewDynamicDotMark 是那个红点本身。
//
// # 它在图标的**右上角**，不是一个摆在旁边的圆点
//
// 上游是 `<Badge isDot color="warning" shape="circle" size="sm">` 套在按钮外面
// （`biu/src/layout/navbar/user-feed/index.tsx:52-60`）：HeroUI 的 Badge 落在
// 按钮的右上角，还带了一句 `translate-x-1/3 -translate-y-1/3`——于是圆点的
// **中心**落在角上往里六分之一的地方（见 dynamicDotNudge）。
//
// ⚠️ **原来那一枚画在图标的右边**——它是那一行里的另一个孩子，中间隔着 4 像素。
// 那是「旁边有个小圆点」，不是「右上角有个点」：用户看不见，而这一票要的就是
// 「一眼能看见」。
func (s *Shell) viewDynamicDotMark(c *ui.Context, count int) {
	t := c.Theme()
	ui.Box(c).Key("nav-feed-dot").
		// 落在按钮的右上角（`Attach` 把这个点的中心摆在父盒子的那个角上）。
		Attach(ui.AnchorTopRight, ui.AnchorCenter).
		// ⚠️ **必须 PassThrough**：这个点画在按钮**上面**，而 MyGo 的命中链是
		// 「最上面那个元素 + 它的祖先」（`mygo/ui/input.go` 的 appendHitChain），
		// **兄弟不参与**——于是这个点那 8×8 底下的按钮收不到指针事件，
		// 「动态」那一枚的右上角变成死区（点那个看得见的红点没反应）。
		//
		// ⚠️ **指针停在它上面时按钮也不亮**（同一个理由）：它吃掉了 hover。
		PassThrough().
		// 上游那 `translate-x-1/3 -translate-y-1/3` 的结果：中心往角里挪六分之一。
		// ⚠️ **MyGo 的正负号和 CSS 的 inset 一样**：`Right(1.33)` 是往**左**挪，
		// `Top(1.33)` 是往**下**挪——两样加起来就是「往角里」。
		Right(dynamicDotNudge).Top(dynamicDotNudge).
		Size(dynamicDotSize, dynamicDotSize).
		Radius(dynamicDotSize / 2).
		// 颜色是**主题里那一个 warning**（`t.Warning`）：上游那一枚是 HeroUI 的
		// `color="warning"`，而规矩是「图形色（图标、圆点、描边）用主题里那一个
		// 原值」——见 semantic.go 那三条规矩的第 3 条。
		//
		// ⚠️ **它不是每个方案都是 #f5a524**：经典那一档就是上游那个数，而主题
		// 方案（暖纸 / 暮光 / 柔雾）各有自己的那一档（暖纸的浅色下是深棕
		// #9a5b06）。那是整个应用的配色，不是这一枚点自己发明的。
		Background(t.Warning).
		// 画面上是一个点（Biu 的 `isDot`），给无障碍和测试留一句话：
		// 有几条。Label 不画在屏幕上，但读屏软件和 `Tester.HasText` 都认它。
		Label(fmt.Sprintf("%d 条新动态", count))
}
