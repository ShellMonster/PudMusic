//go:build darwin

package ui

// macOS 刘海窗的原生半层：NSScreen 的几何（刘海宽、菜单栏高）、NSWindow
// 的窗级配置（菜单栏之上、贴屏顶居中）、以及 NSEvent 的全局/本地指针
// 监听（指针动了就叫醒 `poll`——见 `notch.go` 顶上那一段交互）。
//
// 和 `internal/media` 那套 purego ↔ ObjC 是**两套独立的胶水**：那一层
// 声明的签名是媒体会话要的，这里要的（NSScreen/NSWindow/NSEvent）是
// 另一套，形状照抄它（注册全在 init 主线程上、选择子缓存）。

import (
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo"
)

type notchID = objc.ID

type notchNSPoint struct{ X, Y float64 }
type notchNSSize struct{ W, H float64 }
type notchNSRect struct {
	Origin notchNSPoint
	Size   notchNSSize
}
type notchNSEdgeInsets struct{ Top, Left, Bottom, Right float64 }

var (
	notchLoadErr error

	notchMsgSend   uintptr
	notchMsgRect   func(obj notchID, sel objc.SEL) notchNSRect
	notchMsgInsets func(obj notchID, sel objc.SEL) notchNSEdgeInsets
	notchMsgPoint  func(obj notchID, sel objc.SEL) notchNSPoint
	// 入参是 NSRect 的那些消息。NSRect 是「两个 double 组成的结构」的
	// 两倍——arm64 上属于同质浮点聚合，要按**四个 double** 递过去才能进
	// 浮点寄存器；声明成结构的话反射封送按整数寄存器递，值到不了
	// （同 `media` 层 `msgRequestImage` 顶上那一段理由）。
	notchMsgInitTracking func(obj notchID, sel objc.SEL, r notchNSRect, options uint, owner, info notchID) notchID
	notchMsgSetFrame     func(obj notchID, sel objc.SEL, r notchNSRect, display bool)
)

func init() { notchEnsureReady() }

var notchReadyOnce sync.Once

func notchEnsureReady() { notchReadyOnce.Do(notchLoad) }

func notchLoad() {
	h, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		notchLoadErr = err
		return
	}
	if notchMsgSend, err = purego.Dlsym(h, "objc_msgSend"); err != nil {
		notchLoadErr = err
		return
	}
	stret := notchMsgSend
	if runtime.GOARCH == "amd64" {
		if s, err := purego.Dlsym(h, "objc_msgSend_stret"); err == nil {
			stret = s
		}
	}
	purego.RegisterFunc(&notchMsgRect, stret)
	purego.RegisterFunc(&notchMsgInsets, stret)
	purego.RegisterFunc(&notchMsgPoint, notchMsgSend)
	purego.RegisterFunc(&notchMsgInitTracking, notchMsgSend)
	purego.RegisterFunc(&notchMsgSetFrame, notchMsgSend)

	for _, p := range []string{
		"/System/Library/Frameworks/Foundation.framework/Foundation",
		"/System/Library/Frameworks/AppKit.framework/AppKit",
	} {
		if _, err := purego.Dlopen(p, purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
			notchLoadErr = err
			return
		}
	}
}

// ---- 选择子 / 类：缓存（同 media 那层）----

var (
	notchNameMu     sync.RWMutex
	notchSelCache   = map[string]objc.SEL{}
	notchClassCache = map[string]objc.Class{}
)

func notchSel(name string) objc.SEL {
	notchNameMu.RLock()
	s, ok := notchSelCache[name]
	notchNameMu.RUnlock()
	if ok {
		return s
	}
	s = objc.RegisterName(name)
	notchNameMu.Lock()
	notchSelCache[name] = s
	notchNameMu.Unlock()
	return s
}

func notchClass(name string) objc.Class {
	notchNameMu.RLock()
	c, ok := notchClassCache[name]
	notchNameMu.RUnlock()
	if ok {
		return c
	}
	c = objc.GetClass(name)
	notchNameMu.Lock()
	notchClassCache[name] = c
	notchNameMu.Unlock()
	return c
}

//go:uintptrescapes
func notchSend(obj notchID, selector string, args ...uintptr) notchID {
	var a [8]uintptr
	a[0], a[1] = uintptr(obj), uintptr(notchSel(selector))
	n := copy(a[2:], args)
	r, _, _ := purego.SyscallN(notchMsgSend, a[:n+2]...)
	return notchID(r)
}

func notchSendInt(obj notchID, selector string, args ...uintptr) int {
	return int(notchSend(obj, selector, args...))
}

func notchSendBool(obj notchID, selector string, args ...uintptr) bool {
	return byte(notchSend(obj, selector, args...)) != 0
}

func notchSendRect(obj notchID, selector string) notchNSRect {
	return notchMsgRect(obj, notchSel(selector))
}

func notchSendInsets(obj notchID, selector string) notchNSEdgeInsets {
	return notchMsgInsets(obj, notchSel(selector))
}

func notchSendPoint(obj notchID, selector string) notchNSPoint {
	return notchMsgPoint(obj, notchSel(selector))
}

// ---- 刘海几何 ----

// menuBarScreen 是菜单栏所在那块屏：第一个（`NSScreen.screens` 的第一项）。
func menuBarScreen() notchID {
	screens := notchSend(notchID(notchClass("NSScreen")), "screens")
	if notchSendInt(screens, "count") == 0 {
		return 0
	}
	return notchSend(screens, "objectAtIndex:", uintptr(0))
}

// notchSize 报刘海的宽和高（points），没有刘海返回 false。
// 宽 = 屏宽 - 左右辅助区；高 = `safeAreaInsets.top`。
func notchSize() (w, h float64, ok bool) {
	s := menuBarScreen()
	if s == 0 || !notchSendBool(s, "respondsToSelector:", uintptr(notchSel("safeAreaInsets"))) {
		return 0, 0, false
	}
	insets := notchSendInsets(s, "safeAreaInsets")
	if insets.Top <= 0 {
		return 0, 0, false
	}
	frame := notchSendRect(s, "frame")
	left := notchSendRect(s, "auxiliaryTopLeftArea")
	right := notchSendRect(s, "auxiliaryTopRightArea")
	return frame.Size.W - left.Size.W - right.Size.W, insets.Top, true
}

// menuBarHeight 报菜单栏那块屏的菜单栏高（`frame.maxY - visibleFrame.maxY`）。
func menuBarHeight() float64 {
	s := menuBarScreen()
	if s == 0 {
		return 0
	}
	f := notchSendRect(s, "frame")
	v := notchSendRect(s, "visibleFrame")
	return f.Origin.Y + f.Size.H - (v.Origin.Y + v.Size.H)
}

// raiseAboveMenuBar 把窗抬到菜单栏之上——刘海所在的那一层，每个空间、
// 全屏应用上面都在。
//
// MyGo 的窗默认是 titled（标题栏隐去了但样式位在），AppKit 不让 titled
// 的窗越过菜单栏——所以改成 borderless，只留 nonactivatingPanel 那一位
// （点它不激活应用这件事不能丢）和 fullSizeContentView。
func raiseAboveMenuBar(handle uintptr) {
	win := notchID(handle)
	const nonactivatingPanel, fullSizeContentView = 1 << 7, 1 << 15
	notchSend(win, "setStyleMask:", uintptr(nonactivatingPanel|fullSizeContentView))
	const mainMenuLevel = 24 // NSMainMenuWindowLevel
	notchSend(win, "setLevel:", uintptr(mainMenuLevel+3))
	// canJoinAllSpaces | stationary | fullScreenAuxiliary | ignoresCycle：
	// 每个空间都在、不跟空间切换跑、全屏也贴着、不进 Cmd-Tab。
	notchSend(win, "setCollectionBehavior:", uintptr(1|1<<4|1<<8|1<<6))
	// 关键的一条：NSWindow 的鼠标 tracking 默认只在**应用活跃**时投递
	// mouseMoved/mouseEntered/mouseExited——刘海悬在**别的应用**上面，没有
	// 这一条就永远收不到进出事件。给 MyGo 画 UI 的那个 view 装一个
	// always-in + visible-rect 的 tracking area。
	if view := notchSurfaceView(notchSend(win, "contentView")); view != 0 {
		// NSTrackingEnteredIfLeft | MouseMoved | CursorUpdate | ActiveAlways |
		// InVisibleRect：进出 + 移动 + 光标 + 不挑应用状态 + 用 view 的可见区。
		const options = 0x01 | 0x02 | 0x04 | 0x80 | 0x200
		area := notchSend(notchID(notchClass("NSTrackingArea")), "alloc")
		area = notchMsgInitTracking(area, notchSel("initWithRect:options:owner:userInfo:"),
			notchNSRect{}, uint(options), view, 0)
		notchSend(view, "addTrackingArea:", uintptr(area))
		notchSend(area, "release")
	}
}

// notchSurfaceView 找 MyGo 画原生 UI 的那个 view（MyGoSurfaceView）。
func notchSurfaceView(v notchID) notchID {
	if v == 0 {
		return 0
	}
	if notchSendBool(v, "isKindOfClass:", uintptr(notchClass("MyGoSurfaceView"))) {
		return v
	}
	subs := notchSend(v, "subviews")
	for i := 0; i < notchSendInt(subs, "count"); i++ {
		if found := notchSurfaceView(notchSend(subs, "objectAtIndex:", uintptr(i))); found != 0 {
			return found
		}
	}
	return 0
}

// pinToTop 把 w×h points 的窗贴到菜单栏所在屏的顶边正中——
// AppKit 自己不会把窗放到那么高（菜单栏挡着），所以要 setFrame。
func pinToTop(handle uintptr, w, h float64) {
	screen := menuBarScreen()
	if screen == 0 {
		return
	}
	f := notchSendRect(screen, "frame")
	r := notchNSRect{
		notchNSPoint{f.Origin.X + (f.Size.W-w)/2, f.Origin.Y + f.Size.H - h},
		notchNSSize{w, h},
	}
	notchMsgSetFrame(notchID(handle), notchSel("setFrame:display:"), r, true)
}

// ---- 指针监听：NSEvent 全局/本地 monitor ----
//
// 刘海的 hover 不能靠 `ui.Hovered()`：指针进了物理刘海那一块时，它**根本
// 还没进窗**（窗画的东西收在刘海底下，那一条不归窗管）。所以这里挂两个
// monitor——global 看应用外的移动，local 看应用内的——指针一动就回调，
// 回调里再去算「在不在触发区」（`notch.go` 的 `poll`）。

// watchPointer 起指针监听，fn 在**主线程**上被调（NSEvent monitor 的回调
// 就在投递它的那条线程上，而 AppKit 的事件都在主线程）。stop 摘掉两个
// monitor。ok 为 false 是挂不上（不该发生）。
func watchPointer(fn func()) (stop func(), ok bool) {
	// mouseMoved + leftMouseDragged + rightMouseDragged + otherMouseDragged：
	// 移动和拖动（拖文件进刘海）都算。
	const mask = uint64(1<<5 | 1<<6 | 1<<7 | 1<<27)
	events := notchID(notchClass("NSEvent"))
	global := objc.NewBlock(func(_ objc.Block, _ objc.ID) { fn() })
	local := objc.NewBlock(func(_ objc.Block, ev objc.ID) objc.ID { fn(); return ev })
	g := notchSend(events, "addGlobalMonitorForEventsMatchingMask:handler:", uintptr(mask), uintptr(unsafe.Pointer(&global)))
	l := notchSend(events, "addLocalMonitorForEventsMatchingMask:handler:", uintptr(mask), uintptr(unsafe.Pointer(&local)))
	notchSend(g, "retain")
	notchSend(l, "retain")
	return func() {
		for _, m := range []notchID{g, l} {
			if m != 0 {
				notchSend(events, "removeMonitor:", uintptr(m))
				notchSend(m, "release")
			}
		}
		global.Release()
		local.Release()
	}, g != 0 && l != 0
}

// ---- 从 mygo.Window 过来的一层薄封装 ----

// notchConfigure 把刘海窗的原生配置装上：抬到菜单栏之上、贴屏顶居中。
// 在建窗之后调一次。
func notchConfigure(w *mygo.Window) {
	if w == nil {
		return
	}
	win := w.NativeHandle()
	if win == 0 {
		return
	}
	nw, nh := w.Size()
	raiseAboveMenuBar(win)
	pinToTop(win, float64(nw), float64(nh))
}

// notchReposition 把窗重新贴回屏顶正中（显示器变了之后调）。
func notchReposition(w *mygo.Window) {
	if w == nil {
		return
	}
	nw, nh := w.Size()
	pinToTop(w.NativeHandle(), float64(nw), float64(nh))
}

// notchStartWatch 起指针监听：报上来的「指针动了」直接调 `s.notchPoll`
// （事件驱动，不用定时器）。
func notchStartWatch(s *Shell) {
	stop, ok := watchPointer(func() { s.notchPoll() })
	s.notchSt.unwatch = stop
	s.notchSt.watched = ok
}

// notchStopWatch 摘掉监听。
func notchStopWatch(s *Shell) {
	if s.notchSt.unwatch != nil {
		s.notchSt.unwatch()
		s.notchSt.unwatch = nil
		s.notchSt.watched = false
	}
}
