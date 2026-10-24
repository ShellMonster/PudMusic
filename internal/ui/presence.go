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
//
// ⚠️ **可见性变了就刷那一个开关，两个方向都刷**（票 #392）：界面里有些东西是
// **画的时候**问「这一扇窗看不看得见」的（波形那一格，见 `hover.Wave`）——而
// 那一帧走的是**局部重绘**（只重画、不重建），所以答案得是**活的**：窗口一
// 看不见，那个开关立刻就变，那一格于是不再要帧（票 #156 的那个「失焦还在
// 按帧重画」的风扇问题不会回来）。
//
// ⚠️ 而「刚回来」那一刻还要**补一帧**：看不见的这段时间里后台照样改了状态，
// 而唤醒全被 `wakeHandle` 拦掉了。
func (s *Shell) noteWindow(apply func(*windowPresence)) {
	resumed := s.presence.update(apply)
	// ⚠️ **先刷开关，再决定要不要补帧**：那个开关是「这一帧要不要继续要下一帧」
	// 的唯一判据，而它得在任何一帧画出来之前就是对的。
	s.visibleFlag().Store(s.presence.seen())
	if !resumed {
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
// # 这一层补的是「失焦」——框架管的是「不可见」
//
// 上面三个维度里，**最小化和隐藏框架也管**（`NSWindow.occlusionState` 在这两
// 档不是 visible），**失焦框架不管**：窗口还在屏幕上，只是没有焦点，
// `occlusionState` 仍然是 visible。**失焦这一档是框架管不了的，是这一层补的。**
//
// # 「被别的窗口完全盖住」：框架在帧那一层已经管了，MyGo 不用管
//
// ⚠️ **这里曾经写反了**（票 #393 订正）。原文写的是「框架的 `host.occluded()`
// 只对**自发**的动画帧生效，而外部 `Invalidate` 照样会画」——**与框架的实际
// 行为相反**。框架的真实行为（mygo v0.6.3）：
//
//   - `mygo/ui/runtime.go:685-688`：`surfaceFrame()` 的**第一句**就是
//     `if rt.holdFrame() { return }`——**每一帧**都要先过 `holdFrame()`，不管
//     这一帧是谁要来的。
//   - `mygo/ui/runtime.go:704-717` `holdFrame()`：`rt.host.occluded()` 为真就把
//     这一帧扣住（`rt.held = true`，并把 `rt.redraw` / `rt.repaintDue` 清掉）。
//     **外部 `Invalidate()` 要来的帧同样被扣掉**——「自发动画」和「外面要来的」
//     在这里没有区别。
//   - `mygo/ui/window.go:296-306` `windowHost.occluded()` →
//     `mygo/internal/darwin/surface.go:302-308` `Occluded()`：用的是
//     **`NSWindow.occlusionState`** 的 `occlusionStateVisible` 位——「被完全盖住」
//     「最小化」「隐藏」都不在这一位里。
//   - 唯一的例外：`holdFrame()` 里 `rt.input` 为真时不扣——输入法和无障碍需要
//     那一帧（`mygo/ui/runtime.go:706`）。
//
// 所以「被盖住」这一档**不用在这里加任何代码**：帧在进到 `Shell.View` 之前就被
// 扣住了，这一扇窗连「被叫醒」的机会都没有。**这一层（`presence.go`）补的是
// 框架管不了的那一档：失焦。**
//
// # 停帧之后：框架自己把图形内存还回去（trim）
//
// 帧停了，图形内存不会一直占着。`mygo/internal/gpu/metal/metal.go:1836-1893`：
// 每次 present 之后 `armTrim()`；窗口**连续 2 秒没画**就 `trim()`——
// `releaseTextures()` 把 Metal 纹理全释放，再把 layer 的 `setDrawableSize:` 改成
// 1×1（`trimAfter = 2 * time.Second`）。
//
// ⚠️ **所以「画着的时候占用高」是框架的设计，不是泄漏**：#364 / BMG-211 实测
// **画着时 160~166 MB，停帧 2 秒后 74~79 MB**（三步：`releaseTextures()` 把
// Dirty 变成 Reclaimable → `setDrawableSize: 1×1` 把 IOSurface 缩下去 → 系统
// 逐步回收 Reclaimable）。**别去查「为什么画着的时候 160 MB」——那正是「画着
// 的时候留着、不画了还回去」。**
//
// # 本地怎么验（真窗口，CI 跑不了）
//
// 无头 Tester 造不出「被盖住」：`mygo/ui/headless.go:88` 的 `occluded()` 只看
// `h.hidden`，而它没有对外的开关；而且 Tester 走的是软件渲染器
// （`renderOffscreen`），**根本不经过 Metal**，也就没有 trim。
//
// 真窗口的验法：起两个窗口，一个盖住另一个 → 数帧 → 再用
// `footprint -p <pid>` 看图形侧。⚠️ **footprint 的读数对机器负载敏感**
// （footprint 自己也要占内存），低负载（load 8 以下）才有数；**而帧计数不受
// 负载影响**——所以判据是帧数，footprint 只看**相对变化**。
//
// 本票本地实测（2026-10-25，macOS；纯 mygo 探针，两扇窗 900×650 / 1100×850，
// 一张 1024×1024 的图撑住图形侧）：
//
//	被完全盖住 4 秒（从窗口出现起就盖着）→ 那扇窗一共画了 1 帧
//	露出来 4 秒                          → 画了 180 帧（60 fps）
//	再被盖住 9 秒                        → 头 ~3 秒还在画（macOS 更新
//	                                        `occlusionState` 有延迟），
//	                                        之后一帧都没多画
//
// trim 也确实把图形侧还回去了：`footprint` 的 `IOAccelerator (graphics)` 从
// Dirty 6784 KB 降到 2352 KB（那张图被 `releaseTextures()` 释放了），
// `Owned physical footprint (unmapped) (graphics)` 的 Reclaimable 从 54 MB
// 归零。⚠️ **绝对值跟 MyGo 真实界面的 160 MB 不可比**（探针的界面简单得多），
// **可比的是机制**：画着的时候留着，不画了还回去。
//
// CI 能钉的只有这条链的第一环（「窗口不可见 → 不画帧」），见
// `presence_frames_test.go`。
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
