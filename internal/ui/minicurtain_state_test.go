package ui

// 迷你播放器那个「歌词卷帘」的**窗口状态落盘**那一组（票 #536）。
//
// # 这一组量的是什么
//
// `StateKey: "mini-player"` 会把**宽高也记进去**（`mygo/window_state.go:196`
// 的 `captureState`），而抓状态是在关窗那一刻（`closeState()`）。⇒ 卷帘展开
// 着的时候直接关窗，落盘的就是「84 + 卷帘高」——**下次开出来是个透明大窗**，
// 而**那一块透明的地方照样吃点击**（这一扇窗是 `Transparent: true`）。
//
// ⇒ 票面要的那一条是「**收起、关窗、重启后迷你窗仍为 320×84**」。
//
// # 为什么这一组要一个「假窗」
//
// 无头 Tester 里**没有真窗口**（`mygo.NewWindow` 在应用没 ready 时直接
// panic，`mygo/window.go:382`），而 `captureState` 读的是**原生那一边**的
// bounds ⇒ 真窗那一半量不到。
//
// ⇒ 这一组用 `miniWindow` 那一层接口（`minicurtain.go` 里那一段）接一个
// **假窗**：它记住最后一次 `SetSize`，也能手动触发 `OnClose`。于是「关窗
// 钩子有没有把高度改回 84」就变成**可测的**——而**删掉那个钩子，这一条
// 必须红**（那正是评审用负控制抓出来的那类漏洞：删掉钩子之后旧的用例全绿）。
//
// ⚠️ **它走的是生产那条路**：钩子是 `attachMiniWindowLifecycle` 装的，而那
// 一个函数就是生产用的那个（`attachMiniWindow` 调的是它）。

import (
	"testing"

	"github.com/egoist/mygo"
)

// fakeMiniWindow 是 `miniWindow` 那一层接口的一个**测试实现**。
//
// ⚠️ **它记的是「最后一次被设成的高度」**——而那正是 `StateKey` 落盘时会
// 记下来的那个数（`captureState` 读的是原生那一边，而原生那一边听 `SetSize`
// 的）。
type fakeMiniWindow struct {
	width  int
	height int

	// work 是这扇窗所在显示器的工作区。
	//
	// ⚠️ **它必须摆进来**：生产那一档问的是 `mygo.Screen`，而那个头上有
	// `needsApp`（`mygo/app.go:128`）——应用没 ready 的时候调它**直接
	// panic**，而无头 Tester 里压根没有应用。
	work mygo.Rectangle

	// focused 记「有没有被要求成为 key 窗口」。
	focused bool

	// onClose 是关窗之前那个钩子。
	onClose func(e *mygo.CloseEvent)
}

func newFakeMiniWindow() *fakeMiniWindow {
	return &fakeMiniWindow{
		width:  miniWidth,
		height: miniHeight,
		// 一块 1920×1080 的工作区，窗在它左上角——往下放得下那个 236。
		work: mygo.Rectangle{X: 0, Y: 0, Width: 1920, Height: 1080},
	}
}

func (f *fakeMiniWindow) Size() (int, int) { return f.width, f.height }

func (f *fakeMiniWindow) SetSize(width, height int) {
	f.width, f.height = width, height
}

func (f *fakeMiniWindow) Bounds() mygo.Rectangle {
	return mygo.Rectangle{X: 0, Y: 0, Width: f.width, Height: f.height}
}

func (f *fakeMiniWindow) IsDestroyed() bool { return false }

func (f *fakeMiniWindow) Focus() { f.focused = true }

func (f *fakeMiniWindow) WorkArea() mygo.Rectangle { return f.work }

// Native 返回 nil —— 假窗**没有原生那一侧**。
//
// ⚠️ **那几样东西（把 `s.win` 指过去、登记唤醒句柄、接可见性回调）在假窗那一
// 档就跳过了**：它们要的是 `*mygo.Window` 那一套 API，而无头 Tester 里压根没
// 有真窗口。这一组要量的不是「窗关掉之后收尾」，而是**关窗之前那个钩子**。
func (f *fakeMiniWindow) Native() *mygo.Window { return nil }

func (f *fakeMiniWindow) OnClose(fn func(e *mygo.CloseEvent)) (off func()) {
	f.onClose = fn
	return func() { f.onClose = nil }
}

// close 触发「关窗之前」那个钩子——和框架里 `close()` 的顺序一样
// （`mygo/window.go:644`：先 `fire1(&w.onClose, e)`，再 `w.destroy()`）。
func (f *fakeMiniWindow) close() {
	if f.onClose != nil {
		f.onClose(&mygo.CloseEvent{})
	}
}

// TestTheMiniCurtainLeavesACollapsedHeightInTheWindowState 盯「关窗之前把窗
// 高改回 84 ⇒ 落盘的是 320×84」。
//
// ⚠️ **判据是「关窗那一刻窗有多高」**：那正是 `StateKey` 会记下来的那个数
// （`captureState` 读原生那一边）。而这一条要断的正是**那个钩子有没有跑**
// ——删掉 `installMiniWindowPreClose` 装的那一个钩子，这一条**必须红**。
//
// ⚠️ **它走的是生产那一条路**：`attachMiniWindowTo` 就是 `attachMiniWindow`
// 调的那个函数（不是测试另写的一个）——于是「**生产那一侧忘了装这个钩子**」
// 这一种回归会让这一条**红**。
func TestTheMiniCurtainLeavesACollapsedHeightInTheWindowState(t *testing.T) {
	s, a := miniCurtainShell(t)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle()
	a.PutLyricsFrom("", "[00:00.00]第一句\n[00:05.00]第二句\n", "")
	tt := miniCurtainTester(s)
	tt.Frame()

	// 🔴 **假窗接上**：它替代的是**真窗**（无头环境里没有），而钩子装在
	// `miniWindow` 那一层上——于是测试走的是**同一条生产路径**
	// （`attachMiniWindowTo`，就是 `attachMiniWindow` 调的那个）。
	fake := newFakeMiniWindow()
	s.attachMiniWindowTo(fake)

	// 展开：窗真的变高了（`miniCurtainSetHeight` 走的是那一层接口）。
	clickCurtainCover(t, tt)
	settleCurtain(t, tt, true)
	if !s.miniCurtain.open {
		t.Fatalf("起点就不对：卷帘没有展开，文字有 %q", tt.Texts())
	}
	if got, want := fake.height, miniHeight+miniCurtainH; got != want {
		t.Fatalf("展开之后窗高是 %d，want %d：这一条要断的是「展开着关窗」那一半",
			got, want)
	}

	// 🔴 **关窗**（展开着）：钩子必须把高度改回 84，**然后**原生那一边才
	// 抓状态（`mygo/window.go:660` 的 `destroy()` 先调 `closeState()`）。
	fake.close()

	if got, want := fake.height, miniHeight; got != want {
		t.Errorf("展开着关窗之后窗高是 %d，want %d：落盘的会是一个 %d 高"+
			"的透明大窗（`StateKey` 记宽高，而透明的那一块照样吃点击）",
			got, want, got)
	}
	if got, want := fake.width, miniWidth; got != want {
		t.Errorf("关窗之后窗宽是 %d，want %d：宽度一个字都不许动", got, want)
	}
}

// TestTheMiniCurtainFocusesItsWindowWhenItOpens 盯「展开的那一下把键盘抢到
// 这一扇窗上」。
//
// ⚠️ **它要的是两样，缺一样 Esc 就到不了**：① 应用成为前台（不然系统把键
// 送给原来那个应用）；② **这一扇成为 key 窗口**（不然前台应用的 key 窗口
// 是别的窗）。
//
// ⚠️ **这一条断的是第②样**（`w.Focus()` 那一句）：第①样（`mygo.App.Focus()`）
// 在无头环境里量不到（应用没 ready），而**第②样是「这一扇窗」自己的事**
// ——它恰恰是评审在真机上抓到的那一半（评审实测：点封面之后迷你窗仍是
// 301 高、访达仍在前台，按 Esc 到不了这一扇窗）。
func TestTheMiniCurtainFocusesItsWindowWhenItOpens(t *testing.T) {
	s, a := miniCurtainShell(t)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle()
	a.PutLyricsFrom("", "[00:00.00]第一句\n[00:05.00]第二句\n", "")
	tt := miniCurtainTester(s)
	tt.Frame()

	fake := newFakeMiniWindow()
	s.miniWin = fake

	clickCurtainCover(t, tt)
	settleCurtain(t, tt, true)
	if !s.miniCurtain.open {
		t.Fatalf("起点就不对：卷帘没有展开，文字有 %q", tt.Texts())
	}

	if !fake.focused {
		t.Error("展开之后这一扇窗没有成为 key 窗口：Esc 到不了它" +
			"（窗口级的 `c.Shortcut(0, ui.KeyEscape)` 要的是「键盘在这一扇窗上」）")
	}
}
