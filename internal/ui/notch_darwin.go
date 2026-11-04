//go:build darwin

package ui

// macOS 刘海窗的原生半层：NSScreen 的几何（刘海宽、菜单栏高）、NSWindow
// 的窗级配置（菜单栏之上、贴屏顶居中）、以及一个 CoreGraphics 事件 tap
// （指针进了刘海区就叫醒界面——见 notch.go 顶上那一段交互）。
//
// 和 `internal/media` 那套 purego ↔ ObjC 是**两套独立的胶水**，形状照抄它
// （自己持有、用完 release、不碰 autorelease），但不共用：那一层声明的签名
// 是媒体会话要的，这里要的（NSScreen/NSWindow/CGEvent）是另一套。
//
// ⚠️ **ObjC 消息只能在那条钉住的线程上发**——`media.onObjCThread` 是不导出
// 的，所以本层自己再起一条，形状一样（`objcthread_darwin.go` 顶上那段理由
// 原样成立：Go 的 goroutine 随时会换 OS 线程）。

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

	notchMsgSend uintptr
	// 返回值是结构的那些消息。arm64 上同质聚合走寄存器；amd64 上 >16B 的
	// 结构才需要 stret——NSRect 32B 在 amd64 上要 `objc_msgSend_stret`，
	// 这一层用它拿 frame/visibleFrame。
	notchMsgRect      func(obj notchID, sel objc.SEL) notchNSRect
	notchMsgInsets    func(obj notchID, sel objc.SEL) notchNSEdgeInsets
	notchMsgPoint     func(obj notchID, sel objc.SEL) notchNSPoint
	notchMsgRectFloat func(obj notchID, sel objc.SEL, x, y, w, h float64)

	// CoreGraphics：事件 tap 和显示器高度。
	cgMainDisplayID      func() uint32
	cgDisplayPixelsHigh  func(d uint32) uint32
	cgEventTapCreate     func(tap, place, options, mask uintptr, cb uintptr, info unsafe.Pointer) uintptr
	cgEventTapEnable     func(tap uintptr, enable bool)
	cgEventGetLocation   func(ev uintptr) notchNSPoint
	cfMachPortInvalidate func(port uintptr)
	cfRunLoopGetMain     func() uintptr
	cfRunLoopAddSource   func(rl, source, mode uintptr)
	cfRunLoopModeKCF     uintptr // kCFRunLoopCommonModes
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
	purego.RegisterFunc(&notchMsgRectFloat, notchMsgSend)

	cg := mustLib("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics")
	mustLib("/System/Library/Frameworks/Foundation.framework/Foundation")
	mustLib("/System/Library/Frameworks/AppKit.framework/AppKit")
	cf := mustLib("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation")
	if cg == 0 || cf == 0 {
		return
	}
	purego.RegisterLibFunc(&cgMainDisplayID, cg, "CGMainDisplayID")
	purego.RegisterLibFunc(&cgDisplayPixelsHigh, cg, "CGDisplayPixelsHigh")
	purego.RegisterLibFunc(&cgEventGetLocation, cg, "CGEventGetLocation")
	purego.RegisterLibFunc(&cgEventTapCreate, cg, "CGEventTapCreate")
	purego.RegisterLibFunc(&cgEventTapEnable, cg, "CGEventTapEnable")
	purego.RegisterLibFunc(&cfMachPortInvalidate, cf, "CFMachPortInvalidate")
	purego.RegisterLibFunc(&cfRunLoopGetMain, cf, "CFRunLoopGetMain")
	purego.RegisterLibFunc(&cfRunLoopAddSource, cf, "CFRunLoopAddSource")
	// kCFRunLoopCommonModes 是一个 CFString 全局。
	if p, err := purego.Dlsym(cf, "kCFRunLoopCommonModes"); err == nil {
		// ⚠️ Dlsym 交回来的是**这个符号的地址**，而 kCFRunLoopCommonModes 是个
		// CFString 全局——所以解一次才拿到它存的值。
		addr := p
		cfRunLoopModeKCF = **(**uintptr)(unsafe.Pointer(&addr))
	}
}

func mustLib(path string) uintptr {
	h, err := purego.Dlopen(path, purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		notchLoadErr = err
		return 0
	}
	return h
}

// ---- 选择子 / 类：缓存（同 media 那层，这里的那张表会小得多）----

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

func notchSendRect(obj notchID, selector string) notchNSRect {
	return notchMsgRect(obj, notchSel(selector))
}

func notchSendInsets(obj notchID, selector string) notchNSEdgeInsets {
	return notchMsgInsets(obj, notchSel(selector))
}

func notchSetRect(obj notchID, selector string, x, y, w, h float64) {
	notchMsgRectFloat(obj, notchSel(selector), x, y, w, h)
}

func notchArrayItems(arr notchID) []notchID {
	n := notchSendInt(arr, "count")
	out := make([]notchID, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, notchSend(arr, "objectAtIndex:", uintptr(i)))
	}
	return out
}

// ---- 刘海几何 ----

// notchScreenInfo 是一块屏上「刘海那一块」要的全部几何。
type notchScreenInfo struct {
	// HasNotch 是 `safeAreaInsets.top > 0`。没有刘海的屏上，这扇窗是「贴
	// 菜单栏下面的一条」而不是「盖住刘海」。
	HasNotch bool
	// NotchWidth 是刘海本身的宽（`frame.width - 左右辅助区`）。取不到辅助区
	// 时退回 200（无刘海屏上那条也是这个量级）。
	NotchWidth float64
	// MenuBarHeight 是菜单栏的实际高度（`frame.maxY - visibleFrame.maxY`）。
	// 取不到时按有没有刘海退回 38 / 24。
	MenuBarHeight float64
	// Frame / VisibleFrame 是这一块屏的 Cocoa 坐标（左下角原点）。
	Frame        notchNSRect
	VisibleFrame notchNSRect
	// PrimaryScreenHeight 是主屏高，用来把 Cocoa 坐标翻成屏幕坐标（框架
	// 的 `rectFromMac` 翻的是同一份）。
	PrimaryScreenHeight float64
}

// notchScreenInfos 量所有屏的刘海几何。**必须在 objc 线程上**（`notchOnObjCThread`）。
func notchScreenInfos() []notchScreenInfo {
	screens := notchArrayItems(notchSend(notchID(notchClass("NSScreen")), "screens"))
	if len(screens) == 0 {
		return nil
	}
	primaryH := cgDisplayPixelsHigh(cgMainDisplayID())
	out := make([]notchScreenInfo, 0, len(screens))
	for _, s := range screens {
		frame := notchSendRect(s, "frame")
		visible := notchSendRect(s, "visibleFrame")
		insets := notchSendInsets(s, "safeAreaInsets")
		info := notchScreenInfo{
			Frame:               frame,
			VisibleFrame:        visible,
			HasNotch:            insets.Top > 0,
			PrimaryScreenHeight: float64(primaryH),
		}
		// 刘海宽 = 屏宽 - 左右辅助区。取不到辅助区时退回 200。
		left := notchSendRect(s, "auxiliaryTopLeftArea")
		right := notchSendRect(s, "auxiliaryTopRightArea")
		if left.Size.W > 0 && right.Size.W > 0 {
			info.NotchWidth = frame.Size.W - left.Size.W - right.Size.W + 4
		} else {
			info.NotchWidth = 200
		}
		if menu := frame.Origin.Y + frame.Size.H - (visible.Origin.Y + visible.Size.H); menu > 0 {
			info.MenuBarHeight = menu
		} else if info.HasNotch {
			info.MenuBarHeight = 38
		} else {
			info.MenuBarHeight = 24
		}
		out = append(out, info)
	}
	return out
}

// notchMainScreenInfo 只量主屏（menu bar 所在那块）。
func notchMainScreenInfo() (notchScreenInfo, bool) {
	infos := notchScreenInfos()
	if len(infos) == 0 {
		return notchScreenInfo{}, false
	}
	return infos[0], true
}

// ---- NSWindow 的配置 ----

const (
	// nsMainMenuWindowLevel 是 NSMainMenuWindowLevel（菜单栏那层）。刘海窗要
	// 比它再高两档，不然菜单栏一盖就把它挡住了。
	nsMainMenuWindowLevel = 24
	// NSWindowCollectionBehavior 那几位：每个空间都出现、不跟空间切换跑、
	// 不进 Cmd-Tab 循环、全屏时也贴着——和 boring.notch 同款组合。
	nsCollCanJoinAllSpaces    = 1 << 0
	nsCollStationary          = 1 << 4
	nsCollIgnoresCycle        = 1 << 6
	nsCollFullScreenAuxiliary = 1 << 8
)

// notchConfigureWindow 把那扇窗改成「刘海窗」的样子。
//
// win 是 `mygo.Window.NativeHandle()` 交出来的 NSWindow*。**必须在 objc
// 线程上调**（`notchOnObjCThread`）。
//
// 干的事只有三件：窗级（菜单栏之上）、全空间出现、不可移动。
// 点击它不激活应用这件事由 mygo 的 `Panel: true` 给了（`styleNonactivatingPanel`）。
func notchConfigureWindow(win uintptr) {
	w := notchID(win)
	notchSend(w, "setLevel:", uintptr(nsMainMenuWindowLevel+2))
	notchSend(w, "setCollectionBehavior:", uintptr(
		nsCollCanJoinAllSpaces|nsCollStationary|nsCollIgnoresCycle|nsCollFullScreenAuxiliary,
	))
	notchSend(w, "setMovable:", 0)
}

// notchPositionWindow 把窗贴到主屏顶边正中间（Cocoa 坐标）。
// winW/winH 是窗的 DIP 尺寸。**必须在 objc 线程上调**。
func notchPositionWindow(win uintptr, winW, winH float64) {
	info, ok := notchMainScreenInfo()
	if !ok {
		return
	}
	x := info.Frame.Origin.X + info.Frame.Size.W/2 - winW/2
	y := info.Frame.Origin.Y + info.Frame.Size.H - winH
	w := notchID(win)
	notchSetRect(w, "setFrame:display:", x, y, winW, winH)
}

// notchSetWindowInteractive 切「吃点击 / 不吃点击」。收起的时候窗只有
// 2px 高，这条额外保险收着的那段不挡桌面。
func notchSetWindowInteractive(win uintptr, interactive bool) {
	w := notchID(win)
	var v uintptr
	if !interactive {
		v = 1
	}
	notchSend(w, "setIgnoresMouseEvents:", v)
}

// ---- 指针监听（CGEvent tap）----
//
// 刘海窗的 hover 不是 `ui.Hovered()` 能给的：指针进了物理刘海的那一块时，
// 它**根本还没进窗**（窗只有 2px 高，藏在刘海下缘）。所以这条 tap 监听全屏
// 鼠标移动，进了「刘海那一条」就叫醒界面——见 `notch.go` 的 `notchPoll`。

var notchTapTap uintptr
var notchTapCallback func(notchNSPoint)

//export notchTapHandle
func notchTapHandle(_ uintptr, _ uintptr, ev uintptr, _ unsafe.Pointer) uintptr {
	if notchTapCallback != nil {
		notchTapCallback(cgEventGetLocation(ev))
	}
	return ev
}

// notchStartPointerWatch 起一个全局指针监听。
//
// ⚠️ **CGEvent tap 要主 RunLoop**：这个 tap 挂在**主 RunLoop** 的
// `kCFRunLoopCommonModes` 上——AppKit 那条主线程的 run loop 会驱动它。
// 可以在 objc 线程上建（`notchOnObjCThread`），它驱动的还是主 RunLoop。
//
// ⚠️ **listen-only**（`kCGEventTapOptionListenOnly`）：只看不改，不要辅助
// 功能权限——「改事件」才要，「看」不要。
func notchStartPointerWatch(cb func(notchNSPoint)) bool {
	notchEnsureReady()
	if notchLoadErr != nil {
		return false
	}
	notchTapCallback = cb
	// mask：mouseMoved(5) + leftMouseDragged(6) + rightMouseDragged(7) +
	// otherMouseDragged(27)。只关心移动和拖动（拖文件进刘海）。
	mask := uintptr(1<<5 | 1<<6 | 1<<7 | 1<<27)
	tap := cgEventTapCreate(
		0, // kCGSessionEventTap
		0, // kCGHeadInsertEventTap
		1, // kCGEventTapOptionListenOnly
		mask,
		purego.NewCallback(notchTapHandle),
		nil,
	)
	if tap == 0 {
		return false
	}
	notchTapTap = tap
	rl := cfRunLoopGetMain()
	if rl != 0 && cfRunLoopModeKCF != 0 {
		cfRunLoopAddSource(rl, tap, cfRunLoopModeKCF)
	}
	cgEventTapEnable(tap, true)
	return true
}

func notchStopPointerWatch() {
	if notchTapTap == 0 {
		return
	}
	cgEventTapEnable(notchTapTap, false)
	cfMachPortInvalidate(notchTapTap)
	notchTapTap = 0
	notchTapCallback = nil
}

// ---- objc 线程（形状同 `media.objcthread_darwin.go`）----

var (
	notchObjcOnce    sync.Once
	notchObjcJobs    chan func()
	notchPthreadSelf func() uint64
)

func notchPthreadSelfFn() uint64 {
	if notchPthreadSelf == nil {
		h, _ := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if h != 0 {
			if a, err := purego.Dlsym(h, "pthread_self"); err == nil {
				purego.RegisterFunc(&notchPthreadSelf, a)
			}
		}
	}
	if notchPthreadSelf == nil {
		return 0
	}
	return notchPthreadSelf()
}

// notchOnObjCThread 把 fn 放到钉住的那条线程上跑并等它跑完。
// 理由同 `media.onObjCThread`：ObjC 有一堆每线程状态，不能随手一个 goroutine 上发消息。
func notchOnObjCThread(fn func()) {
	notchObjcStart()
	done := make(chan struct{})
	notchObjcJobs <- func() { defer close(done); fn() }
	<-done
}

func notchObjcStart() {
	notchObjcOnce.Do(func() {
		notchObjcJobs = make(chan func(), 64)
		go func() {
			runtime.LockOSThread()
			for job := range notchObjcJobs {
				job()
			}
		}()
	})
}

// ---- 几何换算：把 Cocoa 坐标翻成 mygo 那套「主屏左上角原点」 ----

// notchRegion 是「刘海那一条」的矩形（屏幕坐标）：屏顶正中、刘海宽、菜单栏高。
func (s notchScreenInfo) notchRegion() (x, y, w, h float64) {
	w = s.NotchWidth
	h = s.MenuBarHeight
	x = s.Frame.Origin.X + s.Frame.Size.W/2 - w/2
	// 菜单栏贴着屏顶：cocoaTop = frame.maxY，翻过来 screen-y = primaryH - maxY = 0
	//（主屏）；副屏是它自己的顶部那条。
	y = s.PrimaryScreenHeight - (s.Frame.Origin.Y + s.Frame.Size.H)
	return x, y, w, h
}

// notchTriggerRegion 是「指针进了就展开」的那一条：刘海宽 + 两侧余量、
// 菜单栏高 + 下方余量（比刘海本身宽一点，把指针在刘海边缘那几像素也算进去）。
func (s notchScreenInfo) notchTriggerRegion() (x, y, w, h float64) {
	const side, below = 60, 4
	w = s.NotchWidth + 2*side
	h = s.MenuBarHeight + below
	x = s.Frame.Origin.X + s.Frame.Size.W/2 - w/2
	// 屏顶往下多探 below 个像素：指针贴着刘海下沿那几像素也算「在刘海」。
	y = s.PrimaryScreenHeight - (s.Frame.Origin.Y + s.Frame.Size.H)
	return x, y, w, h
}

// notchPointInRegion 报一个 Cocoa 点是不是落在矩形内（坐标都换成屏幕系）。
func notchPointInRegion(p notchNSPoint, x, y, w, h, primaryH float64) bool {
	sy := primaryH - p.Y // cocoa y -> screen y（向下为正）
	return p.X >= x && p.X <= x+w && sy >= y && sy <= y+h
}

// ---- 从 mygo.Window 过来的一层薄封装（ui 里调的就是它们）----

// notchConfigure 把刘海窗的原生配置装上：窗级、全空间、贴屏顶居中。
// 在建窗之后调一次（`ensureNotchWindow`）。
func notchConfigure(w *mygo.Window) {
	if w == nil {
		return
	}
	win := w.NativeHandle()
	if win == 0 {
		return
	}
	nw, nh := w.Size()
	notchOnObjCThread(func() {
		notchConfigureWindow(win)
		notchPositionWindow(win, float64(nw), float64(nh))
	})
}

// notchReposition 把窗重新贴回屏顶正中（尺寸改过之后调，见 notchApplySize）。
func notchReposition(w *mygo.Window) {
	if w == nil {
		return
	}
	nw, nh := w.Size()
	notchOnObjCThread(func() {
		notchPositionWindow(w.NativeHandle(), float64(nw), float64(nh))
	})
}

// notchStartWatch 起那个全局指针 tap：报上来的点写进这个外壳的
// `notch.lastPointer`/`hasPointer`（`notchTick` 再拿去算开合）。
//
// ⚠️ **回调在 tap 那条线程上跑**（主 RunLoop），而 `notch.lastPointer` 归
// 主线程读写——这里用一把小锁（tap 那边持有时间只是一行赋值）。
func notchStartWatch(s *Shell) {
	notchStartPointerWatch(func(p notchNSPoint) {
		s.notchMu.Lock()
		s.notchSt.lastPointer = p
		s.notchSt.hasPointer = true
		s.notchSt.pointerIn = notchPointInCurrentRegion(p)
		s.notchMu.Unlock()
	})
}

func notchStopWatch() { notchStopPointerWatch() }

// notchPointInCurrentRegion 报一个点在不在「刘海触发区」里（主屏那一块）。
func notchPointInCurrentRegion(p notchNSPoint) bool {
	info, ok := notchMainScreenInfo()
	if !ok {
		return false
	}
	x, y, w, h := info.notchTriggerRegion()
	return notchPointInRegion(p, x, y, w, h, info.PrimaryScreenHeight)
}
