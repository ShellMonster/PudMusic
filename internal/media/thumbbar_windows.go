//go:build windows && (amd64 || arm64)

// 任务栏缩略图悬停时那一排按钮：上一首 / 播放·暂停 / 下一首。
//
// ⚠️ 这一份没在 Windows 上跑过（开发这一票的机器是 macOS）。规则在
// thumbbar.go（能测），这里只做翻译。
//
// # 两套机制，别混
//
//	SMTC（smtc_windows.go）→ 媒体浮窗、锁屏、键盘媒体键
//	本文件                 → 鼠标悬停任务栏按钮时，缩略图**下面**的那一小条
//
// 它们在界面上长得像（都是播放/暂停/上下曲），但**完全是两套系统**：SMTC 是
// WinRT 的事件，缩略图这一条是 Win32 的 `WM_COMMAND`。混起来写（比如以为
// 给 SMTC 设了 `IsNextEnabled` 缩略图上也会亮）的话两边都半残，而症状是
// 「浮窗上好好的，任务栏上什么都没有」——反过来也成立。

package media

import (
	"fmt"
	"image"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

// ---- CLSID / IID ----

var (
	// clsidTaskbarList 是 `CLSID_TaskbarList`。它实现在资源管理器那边，
	// 所以只能拿、不能自己实现。
	clsidTaskbarList = mustGUID("56fdf344-fd6d-11d0-958a-006097c9a090")

	// iidITaskbarList3 是 `ITaskbarList3`。
	//
	// ⚠️ 末四段是 `8a5eefaf`。抄成 `8a5eee84`（MyGo 现在内部就是那一串）的
	// 后果是 `CoCreateInstance` 失败，而那一层把失败记成一个标志位就不再重试
	// ——症状是**任务栏上什么都没有**（进度条、覆盖图标、缩略图按钮全都不出现），
	// 而应用其余部分完全正常。
	iidITaskbarList3 = mustGUID("ea1afb91-9e28-4b86-90e9-9e9f8a5eefaf")
)

// ---- `ITaskbarList3` 虚表下标 ----
//
// 抄的是 `shobjidl_core.h` 的声明顺序（`ITaskbarList` 五格 →
// `ITaskbarList2` 一格 → `ITaskbarList3` 十四格），逐格和 tailscale/win 的
// `ITaskbarList3Vtbl` 核对过。
//
// ⚠️ **`SetProgressValue` / `SetProgressState` 在 9 / 10**，缩略图那几个还在
// 它们后面——中间隔着 RegisterTab / UnregisterTab / SetTabOrder / SetTabActive
// 四格。顺序记错的话 `ThumbBarAddButtons` 会把 `hwnd` 当成按钮个数来数，
// 而它**照样返回 S_OK**，症状是「缩略图上没有按钮」。

const (
	taskbarHrInit         = 3
	taskbarThumbBarAdd    = 15
	taskbarThumbBarUpdate = 16
	taskbarThumbBarImages = 17
)

// ---- `THUMBBUTTON` ----
//
// 结构体本身在 `winstruct.go`（不带平台标签：它的尺寸和偏移是能测的，测在那边）。

// `dwMask` 的取值（shobjidl_core.h）。
const (
	thbmImageList = 0x00000001
	thbmToolTip   = 0x00000002
	thbmDisabled  = 0x00000004
)

// `dwFlags` 里的「这个按钮是灭的」。
//
// ⚠️ **和 `THBM_DISABLED` 不是同一个值，两处都得写**：前者负责画成灰色，
// 后者负责让它真的按不动。只写前者的症状很特别——按钮看起来是灰的，
// **按下去照样有反应**（Windows 仍把点击递给我们），于是「没有歌」的
// 时候按上一首会放上一首。
const thumbButtonDisabled = 0x00000004

// ilcColor32 是 `ImageList_Create` 的 `ILC_COLOR32`。
//
// ⚠️ 它是 **0x4**，不是 0x40。写错的话位图被当成索引色的，而我们的颜色索引
// 是 0/1/2/3——于是四个图标全被替换成调色板里的前四个颜色，界面上是一排
// 纯色方块。
const ilcColor32 = 0x00000004

// `GetSystemMetrics` 的小图标尺寸。
const (
	smCXSmIcon = 49
	dpiDefault = 96
)

// taskbarButtonCreated 是那个注册过的窗口消息。资源管理器在**给窗口建好
// 任务栏按钮之后**才发它，而 `ITaskbarList3` 的方法**只能在那之后调**
// （MSDN 原话）。所以我们不是直接调，而是挂一个窗口子类等这条消息。
var taskbarButtonCreated = registerWindowMessage("TaskbarButtonCreated")

// thumbbarSubclassID 是子类回调的编号。它在 `SetWindowSubclass` /
// `RemoveWindowSubclass` / 回调三处之间必须对齐，乱填的话摘不掉。
const thumbbarSubclassID = 0x42495500 // "BIU\x00"

// thumbbarSubclassProcPtr 是那个 Go 函数在系统那边的函数指针。
//
// ⚠️ **必须是包级变量、只建一次**：`syscall.NewCallback` 建出来的东西是给系统
// 回头调的，它的地址在整个进程活着的期间都得有效。每次挂子类都建一个新的
// 不是不行，只是白白占掉一个系统级的回调槽——而那种槽是有上限的，
// 反复开关窗口就能把它用光（用光之后新的回调拿到 0，症状是「后来开的
// 窗口收不到任何消息」）。
//
// 另外那六个参数是 `syscall.NewCallback` 在 Windows 上的**上限**：
// `SUBCLASSPROC(HWND, UINT, WPARAM, LPARAM, UINT_PTR, DWORD_PTR)` 正好六个。
var thumbbarSubclassProcPtr = syscall.NewCallback(thumbbarSubclassProc)

// thumbbar 是任务栏缩略图那一排按钮在这个窗口上的那一份。
//
// 用**窗口子类**而不是自己拿窗口过程，是因为窗口过程归 MyGo 拿走了：
// 要装自己的就得走 `SetWindowSubclass`，它让我们在 MyGo 的窗口过程**之前**
// 收到消息，处理完再 `DefSubclassProc` 交还回去。这是第三方想在别人的窗口上
// 加消息的标准做法。
type thumbbar struct {
	mu   sync.Mutex
	hwnd uintptr

	// list 是资源管理器那边的 `ITaskbarList3`，0 表示建不上。
	list uintptr
	// images 是按钮图标的那张 `HIMAGELIST`，四张按 glyph 的下标排。
	images uintptr
	// subclassed 是子类装上没有。
	subclassed bool
	// added 是按钮贴上去没有。
	added bool

	// on 是按钮被按时叫谁。
	on func(Command, time.Duration)

	// cur 是上一次贴上去的样子（没贴上去时是**想要**的样子）。用来判断
	// 「变了才去改」——`ThumbBarUpdateButtons` 每调一次都是一次跨进程调用，
	// 而 `apply` 在播放时每 5 秒都会被调一次。
	cur     [thumbbarCommandCount]thumbbarButton
	haveCur bool
}

// newThumbbar 在一个窗口上装上缩略图按钮。
//
// **on 可以是 nil**：`attach` 在 `bindWindow` 之前就来是常见顺序（见
// smtc_windows.go），到的时候用 `setHandler` 补上。
func newThumbbar(hwnd uintptr, on func(Command, time.Duration)) (*thumbbar, error) {
	if hwnd == 0 {
		return nil, errPlatform("没有窗口，任务栏缩略图上没有按钮")
	}
	for _, p := range []*syscall.LazyProc{
		procSetWindowSubclass, procDefSubclassProc, procRemoveWindowSubclass,
		procImageListCreate, procImageListAdd, procImageListDestroy,
		procImageListSetAlpha, procCreateDIBSection, procDeleteObject,
		procCoCreateInstance,
	} {
		if p.Find() != nil {
			return nil, errPlatform("这个系统上少了一个任务栏需要的入口函数")
		}
	}

	// ⚠️ **先给一份「没有歌」的样式**：按钮在任务栏准备好之前就要贴一次
	// （见下面那句 ensureAdded 的解释），而那一刻还没有任何一次 `apply`。
	// 用零值的话三个按钮的命令 ID 全是 0——同一排三个一样的 ID，
	// Windows 会把它们当成一个按钮处理，而症状是「按哪个都不对」。
	t := &thumbbar{
		hwnd:    hwnd,
		on:      on,
		cur:     thumbbarButtons(false, false),
		haveCur: true,
	}

	images, err := buildGlyphImageList(iconSize(hwnd))
	if err != nil {
		return nil, err
	}
	t.images = images

	var list uintptr
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidTaskbarList)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidITaskbarList3)),
		uintptr(unsafe.Pointer(&list)))
	if hresultFailed(hr) || list == 0 {
		procImageListDestroy.Call(images)
		return nil, hresultError("拿到任务栏列表", hr)
	}
	t.list = list
	// `HrInit` 必须先调一次：没调过的话后面每个 `ITaskbarList3` 方法都返回
	// `CLASS_E_NOAGGREGATION`。
	if hr := comCall(list, taskbarHrInit); hresultFailed(hr) {
		release(list)
		procImageListDestroy.Call(images)
		return nil, hresultError("初始化任务栏列表", hr)
	}
	if hr := comCall(list, taskbarThumbBarImages, hwnd, images); hresultFailed(hr) {
		// 没有图标列表，按钮就画不出图标。**继续往下走**：图标没了还能有
		// 三个能按的格子，比什么都没有好定位。
		logOnce("缩略图按钮的图标列表挂不上", hresultError("ThumbBarSetImageList", hr))
	}

	// 挂上子类：之后「按钮被按」（`WM_COMMAND`）和那条注册消息都会先到我们
	// 这里。⚠️ **这一步必须在贴按钮之前**——`ThumbBarAddButtons` 在任务栏按钮
	// 还没建好的时候调用是无效的，而它同样返回 S_OK。
	if r, _, _ := procSetWindowSubclass.Call(hwnd,
		thumbbarSubclassProcPtr, thumbbarSubclassID,
		uintptr(unsafe.Pointer(t))); r == 0 {
		release(list)
		procImageListDestroy.Call(images)
		return nil, fmt.Errorf("media: 挂窗口子类失败")
	}
	t.subclassed = true

	// 那条消息**可能已经发过了**：我们是在窗口建出来之后才挂上去的，而任务栏
	// 按钮通常在那之前就建好了。所以这里先试着贴一次——贴不上也不当失败，
	// 等消息到了还会再来一次（ensureAdded）。
	t.ensureAdded()
	return t, nil
}

// thumbbarSubclassProc 是窗口子类。它处理两件事，其余一律交还给 MyGo。
//
// ⚠️ **最后一定要 `DefSubclassProc`**：漏掉它的话这个窗口的所有消息都没人
// 处理——症状是窗口还在屏幕上，但点它、拖它、关它全都没反应。
func thumbbarSubclassProc(hwnd, msg, wparam, lparam, idSubclass, refData uintptr) uintptr {
	t := (*thumbbar)(native(refData))

	switch msg {
	case wmRegistered:
		if t != nil && wparam == taskbarButtonCreated {
			// 资源管理器重建了任务栏按钮（它自己重启过，或者这是它第一次
			// 通知我们）。之前贴过的按钮**没了**，重新贴一次。
			t.mu.Lock()
			t.added = false
			t.mu.Unlock()
			t.ensureAdded()
		}
	case wmCommand:
		if t != nil {
			t.onClick(uint32(wparam & 0xFFFF))
		}
	}
	return defSubclassProc(hwnd, msg, wparam, lparam)
}

// WM_REGISTERED / WM_COMMAND。
const (
	wmRegistered = 0x0081
	wmCommand    = 0x0111
)

// onClick 是某个缩略图按钮被按了。
func (t *thumbbar) onClick(id uint32) {
	pos, ok := thumbbarCommandOfID(id)
	if !ok {
		// 同一个窗口上别的 `WM_COMMAND`（原生菜单、别的控件）。**不能当自己的**：
		// 认错了就是「用户点了一下菜单里的东西，歌开始放了」。
		return
	}
	t.mu.Lock()
	// ⚠️ 按**当前贴上去的那一格**发命令，不按此刻真实的状态：系统点的是
	// 用户看见的那个按钮，而图标和状态之间可能差一帧。理由在
	// thumbbar.go 的 commandFor。
	playing := t.cur[pos].Glyph == glyphPause
	on := t.on
	t.mu.Unlock()
	if on == nil {
		return
	}
	on(pos.commandFor(playing), 0)
}

// setHandler 装上「按钮被按时叫谁」。传 nil 是摘掉。
func (t *thumbbar) setHandler(fn func(Command, time.Duration)) {
	t.mu.Lock()
	t.on = fn
	t.mu.Unlock()
}

// apply 把这一条按钮栏贴到窗口上。
func (t *thumbbar) apply(n NowPlaying) {
	want := thumbbarButtons(n.State != StateNone, n.State == StatePlaying)

	t.mu.Lock()
	if t.list == 0 {
		t.mu.Unlock()
		return
	}
	if !t.added {
		// 还没贴上去：先记下**想要**的样子，等 `TaskbarButtonCreated` 到了
		// 用它。不记的话第一首歌的那一版样式就丢了，而那时贴上去的是
		// 「没有歌」那一版——三个键全灰。
		t.cur, t.haveCur = want, true
		t.mu.Unlock()
		return
	}
	if t.haveCur && t.cur == want {
		t.mu.Unlock()
		return
	}
	t.cur, t.haveCur = want, true
	t.mu.Unlock()

	t.push(want)
}

// ensureAdded 把按钮贴上去。贴过就不贴第二次。
//
// **什么时候会贴第二次**：`TaskbarButtonCreated` 那条消息。资源管理器重建了
// 任务栏按钮（它自己重启过）的时候，之前贴的按钮连同那个按钮一起没了，
// 必须重贴——那次 `WM_REGISTERED` 会先把 `added` 清掉再走到这里。
//
// 那不会贴出第二排：任务栏按钮被重建，旧的那一排**跟着没了**，所以第二次
// Add 是往一个空的工具栏上加。MSDN 也没有说同一批 ID 再 Add 一次会替换，
// 所以这里只在这两种情况下 Add。
//
// ⚠️ **`added` 是在调之前就置上的，不是在调成功之后**：任务栏按钮还没建好的
// 那一次调用会返回 S_OK 却是**空操作**，所以「返回成功」证明不了任何事。
// 而如果改成「成功才置」，那么 `newThumbbar` 里那一次和同时到达的
// `WM_REGISTERED` 那一次就可能各贴一遍——症状是缩略图下面出现**六个**
// 按钮，而且每按一次跳两首。
func (t *thumbbar) ensureAdded() {
	t.mu.Lock()
	if t.list == 0 || t.added {
		t.mu.Unlock()
		return
	}
	t.added = true
	want := t.cur
	if !t.haveCur {
		want = thumbbarButtons(false, false)
	}
	t.mu.Unlock()

	t.callButtons(taskbarThumbBarAdd, want)
}

// push 用 `ThumbBarUpdateButtons` 把一版新样式贴上去。
//
// 换图（播放 ↔ 暂停）和改灰都走这一条：`ThumbBarAddButtons` 是**加**，
// 同一批 ID 再加一遍不会改那三个按钮（那是加出第二排的意思），所以只有
// 「第一次贴上」用 Add，之后一律 Update。
func (t *thumbbar) push(bs [thumbbarCommandCount]thumbbarButton) {
	t.callButtons(taskbarThumbBarUpdate, bs)
}

// callButtons 把一份按钮数组交给 `ITaskbarList3` 的第 slot 个方法。
//
// 失败不重试也不退出：缩略图工具栏是**纯加分项**，它在不在都不影响浮窗、
// 锁屏、播放和界面。
func (t *thumbbar) callButtons(slot int, bs [thumbbarCommandCount]thumbbarButton) {
	buttons, tips := buildThumbButtons(bs)
	if hr := comCall(t.list, slot,
		t.hwnd, uintptr(len(buttons)), uintptr(unsafe.Pointer(&buttons[0]))); hresultFailed(hr) {
		logOnce("任务栏缩略图按钮调用失败", hresultError("ThumbBar", hr))
		return
	}
	// tooltip 是**按指针**递过去的，系统那边会自己拷一份；返回之后我们就可以
	// 收回了。但 Go 不保证这行之前参数还活着，所以显式 KeepAlive 一次。
	runtime.KeepAlive(tips)
	runtime.KeepAlive(buttons)
}

// close 收掉：摘子类、销毁位图列表、放掉 `ITaskbarList3`。
//
// **先摘子类再放列表**：反过来会有一个窗口消息落到一个已经放掉的
// `ITaskbarList3` 上，那是一个野指针。
func (t *thumbbar) close() {
	t.mu.Lock()
	list, images, hwnd, subclassed := t.list, t.images, t.hwnd, t.subclassed
	t.list, t.images, t.subclassed, t.added = 0, 0, false, false
	t.mu.Unlock()

	if subclassed && hwnd != 0 {
		procRemoveWindowSubclass.Call(hwnd, thumbbarSubclassProcPtr, thumbbarSubclassID)
	}
	if images != 0 {
		procImageListDestroy.Call(images)
	}
	release(list)
}

// ---- 按钮数组 ----

// buildThumbButtons 把描述翻成 Win32 的结构体数组。
//
// ⚠️ 返回的第二样是那些 tooltip 的 UTF-16 缓冲，**必须在系统调用期间活着**——
// 它们是按指针递过去的，不是一份拷贝。所以每次调用都新建一批，
// 收尾由 freeThumbTips 放掉。
func buildThumbButtons(bs [thumbbarCommandCount]thumbbarButton) ([]thumbbutton, []*uint16) {
	out := make([]thumbbutton, 0, len(bs))
	tips := make([]*uint16, 0, len(bs))
	for _, b := range bs {
		tip := utf16z(tipOf(b.Command))
		out = append(out, thumbbutton{
			dwMask:  thbmImageList | thbmToolTip,
			iID:     b.ID,
			// iBitmap 是**图标在图片列表里的下标**，而图标是按 glyph 的
			// 编号放的（play=0、pause=1、prev=2、next=3）——所以这里直接
			// 拿 Glyph 当下标用，两边共用一套编号，见 thumbbar_glyph.go。
			iBitmap: uint32(b.Glyph),
			szTip:   uintptr(unsafe.Pointer(tip)),
			dwFlags: thumbButtonFlags(b.Enabled),
		})
		tips = append(tips, tip)
	}
	return out, tips
}

// thumbButtonFlags 灭的按钮两处都写上（理由见 thumbButtonDisabled 那段）。
func thumbButtonFlags(enabled bool) uint32 {
	if enabled {
		return 0
	}
	return thumbButtonDisabled
}

// tipOf 是某个命令在缩略图按钮上悬停时显示的那句话。
//
// 没有歌的时候三个键都是灰的，而 Windows 会**照样显示 tooltip**
// ——所以这三句必须写得在「按不了」的时候也说得通（而不是「正在播放」）。
func tipOf(cmd Command) string {
	switch cmd {
	case CommandPrev:
		return "上一首"
	case CommandNext:
		return "下一首"
	case CommandPlay:
		return "播放"
	case CommandPause:
		return "暂停"
	case CommandStop:
		return "停止"
	}
	return "播放"
}

// ---- 图标位图列表 ----

// buildGlyphImageList 建一个 32 位深、带透明通道的图标列表，四张按 glyph 的
// 下标排（play=0、pause=1、prev=2、next=3）。
//
// ⚠️ **为什么不用 `ImageList_AddMasked` 加一张 1 位掩码**：那样画出来的是
// 「黑色笔画」，而 Windows 的任务栏在深浅两套主题下**都是深色**的，黑色笔画
// 在上面等于看不见。而掩码那一版的透明是「不画」，底色其实能透出来——
// 问题在于掩码把「笔画」定义成黑色了。32 位 + alpha 是这里唯一同时解决
// 两件事的做法。
func buildGlyphImageList(size int) (uintptr, error) {
	himl, _, _ := procImageListCreate.Call(
		uintptr(size), uintptr(size), ilcColor32, uintptr(glyphCount), uintptr(glyphCount))
	if himl == 0 {
		return 0, errPlatform("建不出按钮的图标列表")
	}
	for g := thumbbarGlyph(0); g < glyphCount; g++ {
		hbm, err := glyphBitmapHandle(g.scaled(size))
		if err != nil {
			procImageListDestroy.Call(himl)
			return 0, err
		}
		// `ImageList_Add` 会把像素拷进列表，所以位图可以马上放掉。
		idx, _, _ := procImageListAdd.Call(himl, hbm, 0)
		procDeleteObject.Call(hbm)
		if idx == 0xFFFFFFFF {
			procImageListDestroy.Call(himl)
			return 0, errPlatform("图标进不了列表")
		}
		// ⚠️ **这一步不能省**：32 位深并不自动等于「带 alpha」。`ImageList_Add`
		// 之后列表里那张图的高字节是不确定的（BI_RGB 的 32 位位图按定义就
		// 没有有效的 alpha 通道），而任务栏那个深色底会**原样透过来**——于是一张
		// 白三角图标周围糊着一圈说不上来的颜色。`ImageList_SetAlphaImage`
		// 是文档里为这件事给的那一个函数。
		if r, _, _ := procImageListSetAlpha.Call(himl, idx, hbm); r == 0 {
			// 只是图标变成实心了，不是致命错。记一条，接着往下走。
			logOnce("按钮图标的透明通道没设上，图标周围可能糊着一圈底色")
		}
		// ⚠️ **DeleteObject 必须在 `ImageList_SetAlphaImage` 之后**：那个函数读的是
		// 这张位图的像素，提前放掉的话它读的是一段已经还给系统的内存——
		// 结果是 alpha 变成随机的，症状是「图标有时候透明有时候不透明」，
		// 而且看着像渲染问题，不会有人往这儿想。
		// `ImageList_Add` 已经把像素拷进列表了，所以这张位图到此为止。
		procDeleteObject.Call(hbm)
	}
	return himl, nil
}

// glyphBitmapHandle 把一张 RGBA 变成一个能塞进 `ImageList_Add` 的 HBITMAP。
//
// ⚠️ **`Height` 是负数**（自上而下）。Windows 的位图默认是自下而上的，而我们的
// `image.RGBA` 是自上而下的；不写负号就得把每一行反过来——不写的话四个图标
// 全是上下颠倒的，而**上下颠倒的播放三角看起来仍然像个三角**，这个错能一路
// 活到评审结束。
//
// ⚠️ **`Compression` 是 BI_RGB（0），不是 BI_BITFIELDS**：alpha 在 BI_RGB 的
// 32 位位图里是「高字节，文档上说不清是什么」，而我们要的正是把它当真 alpha
// 用，所以这里让 `ImageList_SetAlphaImage` 去读它。
func glyphBitmapHandle(img *image.RGBA) (uintptr, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return 0, errPlatform("图标位图是空的")
	}

	bi := bitmapInfoHeader{
		Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:       int32(w),
		Height:      -int32(h),
		Planes:      1,
		BitCount:    32,
		Compression: 0, // BI_RGB
		SizeImage:   uint32(w * h * 4),
	}
	var bits uintptr
	hbmp, _, _ := procCreateDIBSection.Call(
		0, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if hbmp == 0 || bits == 0 {
		return 0, errPlatform("建不出图标位图")
	}

	// RGBA 是 R,G,B,A；DIB 的 32 位像素是 B,G,R,A。逐像素换前三个字节，
	// alpha **原样留着**（BI_RGB 那一版它没有被预乘，这正是我们要的）。
	row := unsafe.Slice((*byte)(native(bits)), w*h*4)
	for y := range h {
		o := y * w * 4
		for x := range w {
			s := img.PixOffset(x, y)
			row[o+x*4+0] = img.Pix[s+2]
			row[o+x*4+1] = img.Pix[s+1]
			row[o+x*4+2] = img.Pix[s+0]
			row[o+x*4+3] = img.Pix[s+3]
		}
	}
	return hbmp, nil
}

// iconSize 是这个窗口的小图标尺寸（像素）。
//
// 跟着显示缩放走：`GetDpiForWindow` / `GetSystemMetricsForDpi` 都是 Windows
// 10 1607 才有的，没有就退回 96 DPI（那就是 150% 缩放上图标偏小的原因，
// 而那只是小，不是不见）。
func iconSize(hwnd uintptr) int {
	dpi := uintptr(dpiDefault)
	if procGetDpiForWindow.Find() == nil {
		if d, _, _ := procGetDpiForWindow.Call(hwnd); d > 0 {
			dpi = d
		}
	}
	if procGetSystemMetricsForDPI.Find() == nil {
		if n, _, _ := procGetSystemMetricsForDPI.Call(smCXSmIcon, dpi); n > 0 {
			return int(n)
		}
	}
	if procGetSystemMetrics.Find() == nil {
		if n, _, _ := procGetSystemMetrics.Call(smCXSmIcon); n > 0 {
			return int(n)
		}
	}
	return glyphSize
}

// ---- 小工具 ----

// clsctxInprocServer 是 `CLSCTX_INPROC_SERVER`（1）：在进程内建这个对象。
const clsctxInprocServer = 1

// utf16z 把一个 Go 字符串变成一个以 0 结尾的 UTF-16 切片。
//
// 它交给系统之后**必须还活着**（Win32 那边存的是指针，不是一份拷贝），
// 所以每个 tooltip 各要一份，不共用同一个底层数组。
func utf16z(s string) *uint16 {
	units := utf16.Encode([]rune(s))
	out := make([]uint16, len(units)+1)
	copy(out, units)
	return &out[0]
}

// registerWindowMessage 注册一个窗口消息，返回它的编号。
//
// 用 `RegisterWindowMessageW` 而不是写死 `WM_USER + n`：那个编号是**每个进程
// 各不相同**的（由系统分配），写死的话会和别的库撞，而撞上的症状是
// 「某一天这条消息发给了别人」。
func registerWindowMessage(name string) uintptr {
	s := utf16z(name)
	id, _, _ := procRegisterWindowMessage.Call(uintptr(unsafe.Pointer(s)))
	runtime.KeepAlive(s)
	return id
}

// defSubclassProc 把一个消息交还给原来的窗口过程。
func defSubclassProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	r, _, _ := procDefSubclassProc.Call(hwnd, msg, wparam, lparam)
	return r
}
