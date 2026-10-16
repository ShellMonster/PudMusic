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
//
// ⚠️ **关于 QueryInterface：这里「什么 IID 都认」，是刻意的**，见 qiAll 里。
// 那一处是本文件唯一的说服不了人的地方，也是这一票在 Windows 上最大的
// 未验证项，写清楚在下面。

package media

import (
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
)

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
	switch id {
	case iidIMarshal, iidINoMarshal:
		// 这两个**必须**拒：认了它，COM 会认为这个对象自带封送器，
		// 于是不去做代理，直接在系统那条线程上虚调一个 Go 指针——
		// 症状是随机崩，而且崩在现场之外（栈上是系统内部）。
		*(*uintptr)(native(out)) = 0
		return uintptr(hrNoInterface)
	}
	// 其余的一律认，返回 this 自己（见下面那段「为什么」）。
	//
	// ⚠️ **但对象已经被放掉的时候要拒**：那时候 `handlerRef` 找不到它、返回 0，
	// 而 `this` 这个地址已经是一段被 GC 回收的内存了。返回 S_OK 等于递给系统
	// 一个野指针，而症状是「系统调它的 `Invoke` 时随机崩，栈顶在系统内部，
	// 离这儿十万八千里」。
	if handlerRef(this, 1) == 0 {
		*(*uintptr)(native(out)) = 0
		return uintptr(hrNoInterface)
	}
	*(*uintptr)(native(out)) = this
	return sOK
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

// ⚠️⚠️ **「什么 IID 都认」是这里最需要交代的一处**
//
// 系统在 `add_ButtonPressed` 上会问我们要**哪个**接口的指针，而那个 IID 是
// WinRT 自己算出来的「参数化接口哈希」（`TypedEventHandler<SystemMediaTransport-
// Controls, SystemMediaTransportControlsButtonPressedEventArgs>` 的 SHA-1）。
// 它的算法在公开文档里**没有**，抄来的那一串 GUID 又没法在这台机器上验证
// （见 smtc_windows.go 里那几行 IID 的出处）。
//
// 所以这里选了一个不必知道答案的做法：**除了 IMarshal / INoMarshal 之外，
// 任何 IID 都返回 this 自己**。
//
// 为什么这样是安全的：这个对象在虚表上只有 `Invoke` 一件事可做，而任何
// WinRT 事件委托的 `Invoke` 都落在同一格（第 4 格）。系统拿到的无论是我们
// 期望的那个 IID 还是别的什么，拿到的都是一个能正确响应 `Invoke` 的指针。
//
// 为什么不是「更干净」的做法：把三个委托的 IID 都硬抄进来、逐个比对，抄错
// 的话注册**静默失败**——浮窗上的按钮按下去什么都不发生，而系统那边
// `add_ButtonPressed` 返回 S_OK，日志上什么都看不出来。宁可要一个说得通的
// 「什么都认」，也不要一个抄错了却不吭声的精确匹配。
//
// ⚠️ 这也是**这一票在 Windows 上最需要真机验的一处**：验的方法是「浮窗上
// 按播放，歌开始放」，而不是「代码看着对」。
