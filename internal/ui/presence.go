// 这一份管的是「用户现在看得到这一扇窗吗」（票 #156 的第二条：失焦/隐藏时
// 不再逐帧画界面）。
//
// # 为什么每个窗口各有一份
//
// 主窗口失焦不等于迷你播放器看不见：用户在别的应用里的时候，主窗口收在后台，
// 而迷你播放器还浮在桌面上（两个窗口互斥，见 miniplayer.go）。所以「看不看得
// 见」只能是**逐窗口**的问题，和唤醒句柄（wake.go）一样各存一份。
//
// # 谁改它
//
// 生产上是窗口自己的生命周期回调（`watchWindow`）：`OnFocus` / `OnBlur` /
// `OnMinimize` / `OnRestore` / `OnHide` / `OnShow`。无头 Tester 里没有真窗口，
// 于是这份状态停在默认值「看得见」——测试要摆「看不见」就直接用 `noteWindow`
// （那正是生产上回调改的那一份状态，不是测试专用的旁路，见 window_seen_test.go）。
//
// # 为什么带锁
//
// 回调在主线程上跑，而 `wakeHandle.invalidate` 可能从任何 goroutine 读它
// （后台投递完状态之后要问「这一扇还要不要画」）。一个 bool 跨线程读写，
// `-race` 会报，真机上是一个同极难复现的旧帧。
package ui

import (
	"sync"

	"github.com/egoist/mygo"
)

// windowPresence 是「这一扇窗此刻看不看得到」的三个位，以及一个「刚回来」的标记。
//
// 三个位拼出「看得见」：
//
//	shown && !minimized && (focused || freeWhileBlurred)
//
// `freeWhileBlurred` 是给迷你播放器的：它是面板（`Panel: true`，票 #162，
// 点它不激活应用），于是它**几乎永远没有焦点**。拿焦点去判它看不看得见的话，
// 用户点一下播放键，进度条就再也不动了——而它明明就浮在用户眼前。
type windowPresence struct {
	mu        sync.Mutex
	shown     bool
	minimized bool
	focused   bool

	// freeWhileBlurred 表示「不聚焦也算用户看得到」。只有迷你播放器是这种。
	//
	// ⚠️ 只在建外壳时（`NewMini`）写一次，之后不再变——它描述的是这扇窗的
	// 身份（面板），不是某一刻的状态。
	freeWhileBlurred bool

	// resumed 是「刚刚从看不见变成看得见，还没画过那一帧」。
	//
	// 它要让恢复的那一帧**一次对齐**：进度、波形本来就是按当前时钟算的，
	// 但歌词的滚动位置是一段弹簧动画的状态，不处理的话恢复之后要从旧位置
	// 慢慢滚过去（票面原话「不是慢慢追」）。见 `lyricsView.centre` 的 snap。
	resumed bool
}

// newWindowPresence 默认「看得见」。
//
// ⚠️ **不能靠零值**：`shown` 的零值是 false，等于「什么都看不见」——而这个
// 结构在没有真窗口的地方（无头 Tester、还没建出窗口的那段启动路径）必须
// 当作看得见，否则那些路径第一帧就停掉全部动画，而画面上看不出任何异样。
func newWindowPresence() windowPresence {
	return windowPresence{shown: true, focused: true}
}

// seenLocked 是 seen 的不加锁版本，调用方必须已经拿着 p.mu。
func (p *windowPresence) seenLocked() bool {
	if !p.shown || p.minimized {
		return false
	}
	return p.focused || p.freeWhileBlurred
}

// seen 报「用户现在看得到这一扇窗」。任何 goroutine 都能调。
func (p *windowPresence) seen() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.seenLocked()
}

// update 按 apply 改状态，并报告这次是不是「从看不见变成了看得见」。
func (p *windowPresence) update(apply func(*windowPresence)) (resumed bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	before := p.seenLocked()
	apply(p)
	after := p.seenLocked()
	if after && !before {
		p.resumed = true
	}
	return after && !before
}

// takeResumed 取走「刚回来」这个标记。只由界面线程（`Shell.View`）调。
func (p *windowPresence) takeResumed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	r := p.resumed
	p.resumed = false
	return r
}

// noteWindow 改一扇窗的可见状态，并在「重新看得见」的那一刻补一帧。
//
// 补这一帧是必须的：看不见的这段时间里，后台照样改了状态（歌往前走、封面下完、
// 列表翻页），而唤醒全被 `wakeHandle` 拦掉了。不补的话，窗口显示出来的第一帧
// 由系统画——那一帧是对的吗？**恰好是**，因为 view 每帧都从 `Snapshot()` 读。
// 但只在「系统会画一帧」的路径上成立（Show / 取消最小化），而 `OnFocus` 这种
// 路径不会自己出帧（窗口本来就在屏幕上，只是焦点回来了）。所以三条路一起补。
func (s *Shell) noteWindow(apply func(*windowPresence)) {
	if !s.presence.update(apply) {
		return
	}
	// ⚠️ 必须用 `invalidateNow`：`invalidate` 会再问一次 `seen()`，而
	// 「刚回来」这一刻正是它说的「看得见」，所以两者在这里结果一样——但用
	// 前者表达的是**故意**绕过闸门，读代码的人不用去推。
	s.wake.invalidateNow()
}

// watchWindow 把一扇真窗口的可见性接到这个外壳上。
//
// 三个维度各挂各的回调，而不是在某一处轮询 `IsFocused/IsVisible/IsMinimized`：
// 看不见的时候没有帧可以轮询（这正是这一票要达到的效果），所以「回来了」只能
// 靠系统推过来。
//
// ⚠️ **没覆盖「被别的应用完全盖住」**：框架的 `host.occluded()` 只对**自发**
// 的动画帧生效（`mygo/ui/runtime.go` 的 `next()`），而外部 `Invalidate` 照样
// 会画；而 `mygo` 没有把「这个窗口被遮住了」暴露给应用层。真机上的表现是：
// 窗口被完全盖住、又没失焦时仍会按 tick 画帧。票面验收列的是失焦/最小化/隐藏
// 三条，这一条留待框架给出 API 之后再补。
func (s *Shell) watchWindow(w *mygo.Window) {
	if w == nil {
		return
	}
	s.noteWindow(func(p *windowPresence) {
		p.shown = w.IsVisible()
		p.minimized = w.IsMinimized()
		p.focused = w.IsFocused()
	})
	w.OnFocus(func() { s.noteWindow(func(p *windowPresence) { p.focused = true }) })
	w.OnBlur(func() { s.noteWindow(func(p *windowPresence) { p.focused = false }) })
	w.OnShow(func() { s.noteWindow(func(p *windowPresence) { p.shown = true }) })
	w.OnHide(func() { s.noteWindow(func(p *windowPresence) { p.shown = false }) })
	w.OnMinimize(func() { s.noteWindow(func(p *windowPresence) { p.minimized = true }) })
	w.OnRestore(func() { s.noteWindow(func(p *windowPresence) { p.minimized = false }) })
}
