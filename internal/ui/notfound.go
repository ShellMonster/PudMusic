// 404 页：路径谁都不匹配时的那一页（Biu 的 P-30）。
//
// Biu 的 `src/pages/not-found/index.tsx` 就是一个 `<Empty />` 加一个从 3 数到
// 0 的倒计时（`useState(3)` + 每秒 `useInterval(…, 1000)` 减 1），到 0 跳 `/`。
// 我们这一页比上游多两样：**说清楚是哪条路径不存在**（上游只知道「没匹配上」），
// 和**一枚立刻能按的「回到主页」**（上游只有那个倒计时，见 viewNotFound）。
//
// 倒计时那一行的文案用上游删掉之前那一版（a6c48e1）的 `{count} 秒后回到主页...`：
// 三个是 ASCII 的点，不是 `…`，别顺手改成省略号。
package ui

import (
	"strconv"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/egoist/mygo/ui"
)

const (
	// notFoundRedirect 是 404 页停多久就自己回首页。上游是 3 秒。
	notFoundRedirect = 3 * time.Second
	// notFoundTick 是倒计时每一跳的间隔，也就是上游那个每秒减 1 的
	// `useInterval(…, 1000)`。每一跳都要叫醒界面，否则数字会卡在 3 不动。
	notFoundTick = time.Second
)

// syncRoute 是「换页了」这件事唯一的收口，也是那个倒计时唯一的驱动。
//
// **收在这里而不是 navigate**：后退/前进按钮（viewNav 里那对 BackButton /
// ForwardButton）走的是 Router 自己的历史，不经过 navigate。漏了这一处，
// 「404 页 3 秒后跳首页」就会在用户已经退回别的页之后照样把他拽回首页。
//
// 这一段跑在主线程的 view 里，所以改这些字段不违反铁律一；后台那一跳只调
// wakeHandle.invalidate，不碰它们（见 clock.go 顶上那条硬规矩）。
func (s *Shell) syncRoute() {
	// 到点了就回首页。**必须在这一帧的主线程上做**：`ui.Router.Push` 不是线程
	// 安全的（ui/router.go 的注释就写着只能从主线程改状态），后台那一跳因此
	// 只负责叫醒界面，跳页这件事留给这儿。
	if !s.notFoundAt.IsZero() && s.clock.Now().Sub(s.notFoundAt) >= notFoundRedirect {
		s.disarmNotFound()
		s.navigate("/")
	}

	path := s.router.Path()
	if path == s.shownPath {
		return
	}
	s.shownPath = path
	if lookup(path) == notFound {
		s.armNotFound()
	} else {
		s.disarmNotFound()
	}
}

// armNotFound 记下进 404 的那一刻，起那个每秒一跳的倒计时。
//
// 先收旧的：404 → 另一个 404（`/没有这条` → `/也没有这条`）也算换页，会走到
// 这里来。少了这一行，上一次那个每秒一跳的定时器就没人收了，它会一直醒到
// 窗口关掉。
//
// 那一跳回调是 `s.wake.invalidate`——只叫醒，不改任何状态。要真等到 3 秒、
// 要算还剩几秒，都是下一帧的 syncRoute / viewNotFound 在主线程上拿时钟现算。
func (s *Shell) armNotFound() {
	// ⚠️ 关掉之后不许再装（理由见 navbar_feed.go 里 `s.feed.arm` 那一句）：这一句
	// 是「画到 404 那一页」就会到的，而 `Close` 收掉的是**一个**已经装上的定时器。
	if s.closed {
		return
	}
	s.disarmNotFound()
	s.notFoundAt = s.clock.Now()
	s.notFoundStop = s.clock.EveryFunc(notFoundTick, s.wake.invalidate)
}

// disarmNotFound 收掉倒计时。离开 404、或者到点跳首页时调。
func (s *Shell) disarmNotFound() {
	if s.notFoundStop != nil {
		s.notFoundStop()
		s.notFoundStop = nil
	}
	s.notFoundAt = time.Time{}
}

// notFoundLeft 是倒计时上还剩几秒，从 3 数到 0。
//
// 按经过的时间现算，不记着「已经减了几次」：真时钟上那一跳和这一帧之间差几毫秒
// 是随机的，记次数会出现「数字写着 2、实际只剩 0.4 秒」。
func (s *Shell) notFoundLeft() int {
	left := notFoundRedirect - s.clock.Now().Sub(s.notFoundAt)
	if left <= 0 {
		return 0
	}
	// 向上取整：刚进来 0.1 秒就该写着 3，而不是 2。
	n := int(left / time.Second)
	if left%time.Second != 0 {
		n++
	}
	if n > int(notFoundRedirect/time.Second) {
		n = int(notFoundRedirect / time.Second)
	}
	return n
}

// viewNotFound 是 404 页。
//
// 四样：哪一页不存在（就是刚才敲进去的那个路径）、上游那句空状态、还剩几秒
// 回首页、以及**一枚立刻能按的「回到主页」**。第一行是这里唯一非上游的字：上游
// 那一页只知道「没匹配上」，不知道用户敲的是什么，404 不说清楚是哪一条就不成立。
//
// # 为什么多一枚按钮（票 #242，UX 报告 §3-A 的 A4）
//
// 原来这一页**只有一条出路**：等 3 秒自动跳首页。而 404 一般是**用户点错了链接**
// ——他多半想去别的页，不是主页。于是那个倒计时把他**丢到主页**，他刚才想找的
// 东西没了；而如果那个钟没推进（框架的钟在某些路径下站着不动），他就**永远停
// 在这一页**——页面上又没有按钮可以自救。
//
// 现在倒计时照旧走（上游那个行为不改），但**多一条立刻能走的路**。
//
// ⚠️ **按下去要先把倒计时收掉**：不收的话，那一秒恰好跨过 3 秒的话，倒计时会
// 在用户已经回首页之后**再把他拽一次**（虽然拽去的还是首页，但那是第二次跳转）。
func (s *Shell) viewNotFound(c *ui.Context, path string) {
	// 和其它页面 view 一样，头一件正事是装主题（铁律二，见 viewEmpty）。
	s.applyFrameTheme(c)

	t := c.Theme()
	ui.Column(c).Key("page:not-found").Grow(1).Center().Gap(8).Padding(20, 28).
		Children(func() {
			ui.Text(c, "404").FontSize(scale.Font(c, 28)).Bold().TextColor(t.TextMuted)
			// ⚠️ **限宽**：路径是用户敲进来的，长度不封顶——不限宽的话一条长
			// 路径会把这一行撑出窗口，而 404 这一页上又没有别的东西能提示
			// 「刚才敲的那一长串是个地址」。
			ui.Text(c, "没有「"+path+"」这一页").FontSize(scale.Font(c, 15)).MaxWidth(520)
			ui.Text(c, notFoundCountdown(s.notFoundLeft())).FontSize(scale.Font(c, 12)).TextColor(t.TextMuted)
			if ui.PrimaryButton(c, "回到主页").Clicked() {
				s.disarmNotFound()
				s.navigate("/")
			}
		})
}

// notFoundCountdown 是倒计时那一行。字面照上游 a6c48e1 抄的，只是把数换成参数。
func notFoundCountdown(left int) string {
	return strconv.Itoa(left) + " 秒后回到主页..."
}
