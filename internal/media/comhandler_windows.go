//go:build windows && (amd64 || arm64)

// 给 COM 递过去的回调对象：一个只有一个方法的 ObjC 式「Go 小子类」。
//
// Windows 上 WinRT 的事件要交一个 `TypedEventHandler<...>` 给系统——那是
// **IUnknown 派生**的接口（不是 IInspectable），虚表上只有第 4 格
// `Invoke(sender, args)` 有意思。我们没法在 Go 里让系统继承一个 Go 类型，
// 所以反过来：造一个内存里长得像一个 COM 对象的结构体，把虚表指针指到
// 一组由 `syscall.NewCallback` 出来的 Go 函数，然后把这个地址当接口指针
// 递给系统。
//
// 和 MyGo 自己那份（`mygo/internal/windows/com.go` 的 handler）同一个形状。

package media

import (
	"log"
	"sync"
	"syscall"
	"unsafe"
)

// IID。抄自 Windows SDK 的 `guiddef.h` / MyGo 的同一段。
var (
	iidIUnknown     = mustGUID("00000000-0000-0000-c000-000000000046")
	iidIAgileObject = mustGUID("94ea2b94-e9cc-49e0-c0ff-ee64ca8f5b90")
	iidIMarshal     = mustGUID("00000003-0000-0000-c000-000000000046")
	iidINoMarshal   = mustGUID("ecc8691b-c1db-4dc0-855e-65f6c551af49")
	// iidWeakReferenceSource 是 `IWeakReferenceSource`：WinRT 在接管一个
	// 事件源时会问它——**不能认**，认了系统拿我们的 `Invoke` 虚表去当
	// `IWeakReferenceSource.GetWeakReference` 调，那一格的返回值被当指针
	// 解引用，崩在系统内部而栈顶在我们这儿（Windows 真机 E2E 实测：
	// `add_ButtonPressed` 里 `Exception 0xc0000005`）。
	iidWeakReferenceSource = mustGUID("00000038-0000-0000-c000-000000000046")

	// 下面两个是 `TypedEventHandler<…>` 的参数化接口 IID（WinRT 按签名
	// 哈希算出来的那一串）：`0557e996…` 是 `<SystemMediaTransportControls,
	// ButtonPressedEventArgs>`，`44e34f15…` 是 `<…,
	// PlaybackPositionChangeRequestedEventArgs>`。抄自 MyGo
	// `internal/windows/nowplaying.go` 的 `smtcHandlerIIDs`——那是同一形状
	// 在 Windows 真机上跑了几百小时、活下来的那份名单。
	iidSmtcButtonHandler = mustGUID("0557e996-7b23-5bae-aa81-ea0d671143a4")
	iidSmtcSeekHandler   = mustGUID("44e34f15-bdc0-50a7-ace4-39e91fb753f1")
)

// smtcHandlerIIDs 是 SMTC 事件 handler **认得下**的 IID 全名单。
//
// ⚠️ **白名单，其余一律 E_NOINTERFACE**。原来「除 IMarshal / INoMarshal 外
// 全认」在 Windows 真机上被否了：`add_ButtonPressed` 期间系统会把我们的
// handler 往 `IWeakReferenceSource` 上 QI——答了 S_OK 之后系统真把它当
// `IWeakReferenceSource` 用，而那张接口的 `GetWeakReference` 恰好落在虚表
// 第 4 格（我们的 `Invoke`），它的返回值被当成一张指针去解引用，就是
// `Exception 0xc0000005`（栈落在 `comCall` 那次 `SyscallN` 里）。
//
// **只认这四张**：IUnknown（COM 之本）、IAgileObject（告诉 COM 这个对象可以
// 在任何线程上被调，不用封送——WinRT 的事件委托就是这么叫我们的）、以及
// 我们真接的那两个 `TypedEventHandler`。其余的——尤其 `IMarshal` /
// `INoMarshal`（认了 COM 就不做代理、直接在系统线程上虚调一个 Go 指针）、
// `IWeakReferenceSource`——一律拒。
var smtcHandlerIIDs = []guid{
	iidIUnknown,
	iidIAgileObject,
	iidSmtcButtonHandler,
	iidSmtcSeekHandler,
}

// sOK 是 `S_OK`：成功。
const sOK = 0

// handler 是一个 COM 对象：只有一个方法（Invoke）加 IUnknown 那三个。
type handler struct {
	// vtbl 必须在第一个字段：系统那边把我们的地址当 `IUnknown*` 用，
	// `QueryInterface` 里那第一句就是把它当虚表指针解引用。
	vtbl *[4]uintptr
	refs int32
	fn   func(sender, args uintptr)
}

var (
	handlersMu sync.Mutex
	handlers   = map[uintptr]*handler{}
)

// handlerVtbl 是所有回调对象共用的一张虚表。四个回调在 init 里建一次。
//
// ⚠️ `syscall.NewCallback` 建出来的地址**必须在整个进程活着的期间都有效**，
// 所以它们是包级变量而不是每次新建一个 handler 就重新建一遍。
var handlerVtbl = [4]uintptr{
	syscall.NewCallback(genericQI),
	syscall.NewCallback(genericAddRef),
	syscall.NewCallback(genericRelease),
	syscall.NewCallback(genericInvoke),
}

// genericQI 是所有回调对象的 QueryInterface。
//
// `riid` 是 `REFIID`，也就是「指向一个 GUID 的指针」。
func genericQI(this, riid, out uintptr) uintptr {
	if out == 0 {
		return ePointer
	}
	id := *(*guid)(native(riid))
	// ⚠️ **白名单**：只有名单上的 IID 才返回 this，其余一律
	// E_NOINTERFACE——尤其 `IWeakReferenceSource`（见 `smtcHandlerIIDs`
	// 头上那段），那是 Windows 真机 `add_ButtonPressed` 里 0xc0000005
	// 的根。判据走 `handlerAccepts` 那个接缝，好让测试能盯住「生产上
	// 用的真是白名单这一份」。
	if !handlerAccepts(id) {
		*(*uintptr)(native(out)) = 0
		return uintptr(hrNoInterface)
	}
	// ⚠️ **对象已经被放掉的时候要拒**：那时候 `handlerRef` 找不到它、
	// 返回 0，而 `this` 这个地址已经是一段被 GC 回收的内存了。返回 S_OK
	// 等于递给系统一个野指针。
	if handlerRef(this, 1) == 0 {
		*(*uintptr)(native(out)) = 0
		return uintptr(hrNoInterface)
	}
	*(*uintptr)(native(out)) = this
	return sOK
}

// handlerAccepts 报「这个 IID 该让我们的 handler 认下来吗」。
//
// ⚠️ **它是变量，不是内联的 `slices.Contains`**：这样测试能把生产这份
// 换成「记下被问过哪些 IID」的探针，于是能断「生产 QI 真的走白名单、
// 而且名单里没把 `IWeakReferenceSource` / `IMarshal` 放进去」。
var handlerAccepts = func(id guid) bool {
	for _, ok := range smtcHandlerIIDs {
		if id == ok {
			return true
		}
	}
	return false
}

// genericAddRef / genericRelease 是引用计数。
func genericAddRef(this uintptr) uintptr { return uintptr(handlerRef(this, 1)) }

func genericRelease(this uintptr) uintptr { return uintptr(handlerRef(this, -1)) }

// genericInvoke 是所有回调对象的 Invoke。
//
// `sender` 是事件源（SMTC 那个对象），`args` 是事件参数。
// 第三个参数 `out`（返回用的那个指针）这里不碰：WinRT 的处理器返回值
// 一般是 void，而 `result` 那个槽系统那边也不读。
func genericInvoke(this, sender, args, out uintptr) uintptr {
	handlersMu.Lock()
	h := handlers[this]
	handlersMu.Unlock()
	if h == nil {
		// 对象已经被放掉了。系统理论上不该在这时候调，但真出现过——
		// 返回 S_OK，别在这里 panic（panic 会穿透 COM 的栈回到系统，
		// 直接带走进程）。
		return sOK
	}
	// ⚠️ **handler 里的 panic 绝不能穿透这一层**。这一层是
	// `syscall.NewCallback` 的边界：panic 越过它就不是「Go panic 沿路
	// recover」了，是落进 runtime 的 callback 封装里 `throw`——整个进程
	// 当场死掉（栈上只剩 `runtime.callbackWrap`，看不出是哪个回调）。
	// 在这里 recover 一次，等于把「handler 里的一个错」降级成一条日志。
	defer func() {
		if r := recover(); r != nil {
			log.Printf("pudmusic: 系统媒体回调里的一次错误已经拦住（没让它穿过 COM 边界）：%v", r)
		}
	}()
	h.fn(sender, args)
	return sOK
}

// handlerRef 改引用计数，归零时把对象从表里摘掉（这样 Go 才能回收它）。
func handlerRef(this uintptr, delta int32) int32 {
	handlersMu.Lock()
	defer handlersMu.Unlock()
	h := handlers[this]
	if h == nil {
		return 0
	}
	h.refs += delta
	if h.refs <= 0 {
		delete(handlers, this)
		return 0
	}
	return h.refs
}

// newHandler 造一个回调对象，持有**一个**引用。
//
// 交给系统之后系统自己会 AddRef（QI 成功时），所以这一份是我们自己的。
// 交出去之前的那一份由 withHandler 放掉——次序反了会漏一个引用，症状是
// 这个对象永远不回收（一个几十字节的泄漏，每秒一次就很难看）。
func newHandler(fn func(sender, args uintptr)) uintptr {
	h := &handler{vtbl: &handlerVtbl, refs: 1, fn: fn}
	p := uintptr(unsafe.Pointer(h))
	handlersMu.Lock()
	handlers[p] = h
	handlersMu.Unlock()
	return p
}

// withHandler 造一个回调对象交给 call，用完放掉我们自己那一份。
func withHandler(fn func(sender, args uintptr), call func(h uintptr) uintptr) uintptr {
	h := newHandler(fn)
	defer release(h)
	return call(h)
}

// ePointer 是 `E_POINTER`：参数不对。
const ePointer = 0x80004003

// ⚠️⚠️ **「什么 IID 都认」那一版改动的经过**（防有人再改回去）：
//
// 这里曾经「除了 IMarshal / INoMarshal 之外，任何 IID 都返回 this 自己」——
// 理由是 `TypedEventHandler` 的参数化 IID 由 WinRT 哈希算出、文档里查不到，
// 「什么都认」就不必猜它。
//
// **那个做法在 Windows 真机上被否了**：`add_ButtonPressed` 同步 QI 我们的
// handler 问 `IWeakReferenceSource`（`00000038-…`），我们答 S_OK 递回
// this——系统于是把虚表第 4 格当 `IWeakReferenceSource.GetWeakReference`
// 调，那一格是我们的 `genericInvoke`，拿到的 `out` 槽被当返回值指针解引用，
// 就是 `Exception 0xc0000005`（栈顶落在 `comCall` 那次 `SyscallN` 上，见
// `win_windows.go` 里 `comCall`）。
//
// 所以现在按 MyGo 自己那份的白名单来（`smtcHandlerIIDs`）：只认
// IUnknown / IAgileObject / 两个 `TypedEventHandler`，其余一律
// E_NOINTERFACE。名单里那两个 GUID 出处和 MyGo `nowplaying.go` 是同一份——
// 它在 Windows 上跑了几百小时，这俩哈希是对的。
