// 顶栏上的「动态」按钮 + 更新红点：Biu 的 N-07
// （`biu/src/layout/navbar/user-feed/index.tsx`）。
//
// Biu 那一支是一个 `useRequest`，`pollingInterval: 300000`、`pollingWhenHidden:
// false`，每 5 分钟问一次「有没有新动态」，有就在图标右上角点一个圆点；点进去
// 看动态时把点清掉。
//
// ## 这里的节奏是拿**假时钟**测的，不是真等
//
// SPEC 的 Testing Decisions 把假时钟列成每一阶段都要留下的回归证据之一
// （「点赞长按 2.5 秒、全屏鼠标移出 3 秒、Toast 2 秒消失」是同一类）。
//
// 时钟走的是界面层那一个（`clock.go` 的 `clockSource`），**不是自己再带一个**：
// 那个文件的头一句话是「界面层所有『等一会儿』都从这里走」，这里另立一份就等于
// 同一个包里两个时刻的来源——测试换上假时钟之后红点还按真时钟走，而那恰恰是它
// 最难查的那种错（不报错，只是节奏不对）。
//
// 所以 `tick` 收一个 `clockSource` 当参数：换一个 Shell.clock，这个节奏跟着换，
// 不用记得再去改第二个地方。5 分钟的节奏在测试里是毫秒级的，**不用 time.Sleep**。
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
// 自带锁，它内部存的是窗口的弱引用，所以这条唤醒链是并���安全的。
package ui

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
)

// dynamicFeedInterval 是红点轮询的间隔。Biu 的 `pollingInterval: 300000`
// （`user-feed/index.tsx:24`），就是 5 分钟。
const dynamicFeedInterval = 5 * time.Minute

// dynamicDotSize 是红点多大。Biu 的 Badge `isDot`，尺寸没露出来；8 是顶栏这个
// 高度里点得出来又不抢戏的一个数。
const dynamicDotSize = 8

// dynamicFeedPath 是用户动态那一页的路由，和 router.go 里登记的是同一个字符串。
//
// 拿字符串比而不是拿 `routes[8]`：那条表一改顺序这里跟着错，而且错得很安静。
const dynamicFeedPath = "/dynamic-feed"

// feedPoller 是「动态更新红点」的节奏。
//
// **只由 view 在主线程上读写**，不跨 goroutine（和 Shell 里其余的界面状态一样）。
type feedPoller struct {
	// interval 是两轮之间隔多久。
	interval time.Duration
	// fetch 拉一次更新，返回有几条没看过的。
	//
	// 这一版还没有 B 站接口，所以它默认返回 0。**接上真接口之后它会变成异步的**，
	// 那时候结果回来必须叫醒界面（`s.wake.invalidate()`），否则红点停在旧值——
	// 铁律三实测过：只投递不唤醒 = 0 帧刷新。
	fetch func() int

	// next 是下一次该轮询的时刻。零值表示「马上就该轮一次」——Biu 那个
	// useRequest 挂上就发第一次，不是等 5 分钟才发。
	next time.Time
	// count 是上一次拿回来的条数，0 就不画红点。
	count int
	// stop 是「到点叫醒界面」那个定时器的停法，nil 表示没装。
	//
	// 只在主线程上读写（arm / disarm 都由 view 调），后台那个定时器只调 wake，
	// 不碰这里——铁律一。
	stop func()
}

// newFeedPoller 建一个节奏。fetch 为 nil 就是一个永远返回 0 的空拉。
func newFeedPoller(interval time.Duration, fetch func() int) *feedPoller {
	if fetch == nil {
		fetch = func() int { return 0 }
	}
	return &feedPoller{interval: interval, fetch: fetch}
}

// tick 到点就轮一次，返回这一帧该画几个红点。
//
// 下一轮的时刻从**轮完这一刻**起算（`now + interval`），不是从上一轮推——
// 两种算法在准点时一模一样，差的是「轮完这一次花了 300ms」之后：按上一轮推会
// 让实际间隔越缩越短，最后快到连着发。
func (p *feedPoller) tick(c clockSource) int {
	now := c.Now()
	if now.Before(p.next) {
		return p.count
	}
	p.next = now.Add(p.interval)
	p.count = max(p.fetch(), 0)
	return p.count
}

// clear 把红点清掉。用户点进去看动态之后就不该再点第二次。
func (p *feedPoller) clear() {
	p.count = 0
	// ⚠️ **不清 next**：清了就变成「立刻再轮一次」，而这一轮是刚轮完的。
	// Biu 也是只 `setUpdateCount(0)`（`user-feed/index.tsx:38`）。
}

// arm 装上「到点叫醒界面」的那一路，已经装过就什么都不做。
//
// 每一帧都调是安全的：装上了就直接返回。装完的那一趟从**这一刻**起每 interval
// 醒一次，所以定时器和轮询时刻始终差着一个恒定的偏移（第一次装上的那一帧），
// 而那个偏移小到看不见。
func (p *feedPoller) arm(c clockSource, wake func()) {
	if p.stop != nil {
		return
	}
	p.stop = c.EveryFunc(p.interval, wake)
}

// disarm 停掉那一路。窗口真关掉之后 `Invalidate` 对它是 no-op，但它还会一直每
// 五分钟醒一次到进程退出，所以要在收尾的地方停掉（见 shell.go 的 Attach）。
func (p *feedPoller) disarm() {
	if p.stop != nil {
		p.stop()
		p.stop = nil
	}
}

// viewDynamicDot 是顶栏上的「动态」那一格：有新动态时右上角一个圆点。
func (s *Shell) viewDynamicDot(c *ui.Context) {
	t := c.Theme()
	count := s.feed.tick(s.clock)
	// ⚠️ 这一句是「窗口静止时也会轮询」的全部保证：光靠上面那一行，静止的窗口
	// 永远没人问。到点的那一趟把界面叫醒，下一帧上面就会问。
	//
	// ⚠️ **关了之后不许再装**（`s.closed`）。`Close` 收掉的是一个具体装上的
	// 定时器，而这一句是**每帧**都会到的——所以只要关掉之后还有一帧画到顶栏
	// （无头 Tester 里 `Close` 之后再 `Frame()` 就会，见
	// `close_while_loading_test.go`），它就会在这儿再装一个，而 `Close` 已经走过
	// 一趟、不会再走。那一个就挂到测试进程结束（真窗口里 OnClosed 之后不会再有帧，
	// 可这条不该靠「恰好不会」来对）。
	if !s.closed {
		s.feed.arm(s.clock, s.wake.invalidate)
	}

	// Biu 只在登录后摆这一格（`navbar/index.tsx:38` 的 `{Boolean(user?.isLogin) && ...}`）。
	// 这里**没按登录态隐藏**——和头像下拉那六项同一个理由（见 navbar_avatar.go
	// 文件头）：菜单/顶栏的形状不随登录态变来变去。而且红点这一格不点它也只
	// 是一次轮询，没登录时轮回来的是 0，红点自然不亮。
	//
	// 代价说清楚：登录之后这一格还在，而 Biu 那时候已经把它藏了。
	ui.Row(c).Key("nav-feed").Gap(4).AlignItems(ui.Start).Children(func() {
		ui.Column(c).Key("nav-feed-btn").Children(func() {
			// 上游这一枚是 user-feed 的 `Tooltip closeDelay={0}`
			// （`navbar/user-feed/index.tsx:50`）：菜单那一档，指针一走就收。
			if s.withMenuTip(c, iconButton(c, iconFeed, "动态", iconMd).
				Size(iconHitMd, iconHitMd).Padding(0, 5), "动态").Clicked() {
				// Biu 的 handleOpen：清掉红点，再去看动态。
				s.feed.clear()
				s.navigate(dynamicFeedPath)
			}
		})
		if count > 0 {
			// 画面上是一个点（Biu 的 `isDot`），给无障碍和测试留一句话：
			// 有几条。Label 不画在屏幕上，但读屏软件和 `Tester.HasText` 都认它。
			ui.Box(c).Size(dynamicDotSize, dynamicDotSize).
				Radius(dynamicDotSize / 2).Background(t.Warning).
				Label(fmt.Sprintf("%d 条新动态", count))
		}
	})
}
