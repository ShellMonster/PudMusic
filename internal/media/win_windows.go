//go:build windows && (amd64 || arm64)

// Windows 那一半的地基：COM 虚表调用、WinRT 的激活与字符串、几个 Win32 函数。
//
// **一个应用逻辑都不写**：只有「怎么调到第 i 个方法」「HRESULT 算不算失败」
// 「HSTRING 怎么造出来」这类机械活。真正的翻译在 smtc_windows.go 和
// thumbbar_windows.go 里。
//
// 这一层的形状抄的是 MyGo 自己那份（`mygo/internal/windows/com.go`，内部包导
// 进来）。抄它的形状而不是自己发明：那一层是这个项目里唯一已经在 Windows 上
// 跑过几百个小时的纯 Go ↔ COM 通路，它踩过的每一个坑都是付过代价的——
// 比如 `syscall.SyscallN` 而不是裸的 `Syscall`（后者在 arm64 上不传浮点）、
// 比如回调对象必须活到 COM 放掉它为止。
//
// ⚠️ **整套东西没在 Windows 上跑过**（开发这一票的机器是 macOS）。抄来的常量
// （IID、虚表下标、枚举值）来源写在各处注释里，逐条列在报告里。

package media

import (
	"fmt"
	"runtime"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// ---- DLL ----
//
// 系统 DLL **一律从系统目录按全路径加载**，不靠搜索路径。理由和抄来的那段
// 一样：搜索路径里第一个是**可执行文件所在的目录**，而那个目录用户是能写的。
// 从那里先找到一份同名 DLL 就等于把「加载系统控件」这一步交出去了。
//
// 这不是这一票引入的威胁模型（媒体会话只是又多加载了几个系统 DLL），但既然
// 要写这段加载代码，就按已经想清楚的那一种写法来。

// kernel32 是 KnownDLL，不走下面的全路径那一套（它是拿到系统目录的那把梯子）。
var kernel32 = syscall.NewLazyDLL("kernel32.dll")

func systemDirectory() string {
	buf := make([]uint16, syscall.MAX_PATH)
	n, _, _ := kernel32.NewProc("GetSystemDirectoryW").Call(
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 || int(n) > len(buf) {
		return `C:\Windows\System32`
	}
	return syscall.UTF16ToString(buf[:n])
}

var sysDir = systemDirectory()

func systemDLL(name string) *syscall.LazyDLL { return syscall.NewLazyDLL(sysDir + `\` + name) }

var (
	dllWinRT  = systemDLL("combase.dll")    // RoInitialize / RoGetActivationFactory
	dllWinStr = systemDLL("kernelbase.dll")
	dllOle32  = systemDLL("ole32.dll")
	dllUser32 = systemDLL("user32.dll")
	dllGDI32  = systemDLL("gdi32.dll")
	dllComctl = systemDLL("comctl32.dll")
)

// winStringProcs 是 WinRT 字符串那几个函数在哪几本册子里。
//
// `WindowsCreateString` / `WindowsDeleteString` 在 Windows 8 之后被搬过家：
// 早期在 combase.dll，后来主家在 kernelbase.dll，而 apiset
// （api-ms-win-core-winrt-string-l1-1-0）会按调用方重定向。两本都试一遍，
// 取先找到的那一对——
//
// ⚠️ 这么做不是「保险起见」的好习惯，是这里**唯一**一处兜底：函数表里取不到的
// 符号在 `LazyProc.Find()` 里就是一个 error，而 NewProc 拿到的 proc 调下去会
// panic（`LazyProc.Call` 在符号没找到时 panic）。而 panic 的地方是系统媒体
// 会话刚建起来的那一刻，在 COM 回调链上 panic 会直接带走进程——界面、播放、
// 队列全跟着完。一处兜底换掉这一整类风险，值得。
func winStringProcs() (create, del *syscall.LazyProc) {
	for _, d := range []*syscall.LazyDLL{dllWinStr, dllWinRT, dllOle32} {
		c := d.NewProc("WindowsCreateString")
		dl := d.NewProc("WindowsDeleteString")
		if c.Find() == nil && dl.Find() == nil {
			return c, dl
		}
	}
	return dllWinRT.NewProc("WindowsCreateString"), dllWinRT.NewProc("WindowsDeleteString")
}

var (
	procWinStringCreate, procWinStringDelete = winStringProcs()
	procRoInitialize                        = dllWinRT.NewProc("RoInitialize")
	procRoGetFactory                        = dllWinRT.NewProc("RoGetActivationFactory")

	procRegisterWindowMessage  = dllUser32.NewProc("RegisterWindowMessageW")
	procSetWindowSubclass      = dllComctl.NewProc("SetWindowSubclass")
	procRemoveWindowSubclass   = dllComctl.NewProc("RemoveWindowSubclass")
	procDefSubclassProc        = dllComctl.NewProc("DefSubclassProc")
	procGetDpiForWindow        = dllUser32.NewProc("GetDpiForWindow")
	procGetSystemMetricsForDPI = dllUser32.NewProc("GetSystemMetricsForDpi")

	procImageListCreate    = dllComctl.NewProc("ImageList_Create")
	procImageListAdd       = dllComctl.NewProc("ImageList_Add")
	procImageListSetAlpha  = dllComctl.NewProc("ImageList_SetAlphaImage")
	procImageListDestroy   = dllComctl.NewProc("ImageList_Destroy")
	procCreateDIBSection   = dllGDI32.NewProc("CreateDIBSection")
	procDeleteObject       = dllGDI32.NewProc("DeleteObject")
	procCoCreateInstance   = dllOle32.NewProc("CoCreateInstance")
	procGetSystemMetrics   = dllUser32.NewProc("GetSystemMetrics")
)

// ---- GUID ----

// guid 是一个 COM 的 GUID，按 Windows 那边在内存里的字节序存（就是
// `{Data1, Data2, Data3, Data4[8]}` 依次铺开）。
type guid [16]byte

// mustGUID 解析 `xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx` 这种写法。
//
// 为什么用字符串而不是一串十六进制字面量：一串十六进制**抄错了编译得过**，
// 而「看着不像一个 GUID」的一串字符更容易在评审里被看出端倪。抄错 IID 的
// 后果是 `QueryInterface` 返回 `E_NOINTERFACE`，症状是「功能静默地不出现」——
// 界面上、任务栏上、浮窗上都没有任何提示，只有日志里一行 HRESULT。
//
// 字节序是 GUID 的老规矩：`xxxxxxxx` 那 8 个十六进制数字**按小端**铺开，
// `xxxx-xxxx` 那 4 个也是小端，最后 12 个按顺序铺。少铺一层或铺成顺序，
// 编译照样过、`QueryInterface` 照样返回 `E_NOINTERFACE`。
//
// 格式不对就 panic：所有调用点都是包内写死的常量，没有运行期输入。
func mustGUID(s string) guid {
	var g guid
	var b [32]byte
	n := 0
	for i := range len(s) {
		if s[i] == '-' {
			continue
		}
		c := hexVal(s[i])
		if c < 0 {
			panic("media: 不是 GUID：" + s)
		}
		b[n] = byte(c)
		n++
	}
	if n != 32 {
		panic("media: 不是 GUID：" + s)
	}
	// 前 8 个字节是四对十六进制数字，每对按小端换一次。
	for i := range 8 {
		g[i] = b[i*2+1]<<4 | b[i*2]
	}
	// 后 8 个字节是 Data4，按顺序铺。
	copy(g[8:], b[16:])
	return g
}

// hexVal 是一个十六进制字符的数字值，认不出来给 -1。
func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// ---- HRESULT ----

// hresultFailed 是不是一个失败的 HRESULT。
//
// `HRESULT` 是**有符号**的 32 位：成功是 0..0x7FFFFFFF，失败是 0x80000000
// 往上（当成有符号就是负数）。当成无符号比较「是否非零」的话，
// `S_FALSE`（1，表示成功但「不是全都对」）会被判成失败，而 SMTC 的
// `QueryInterface` 正常路径上就是它。
func hresultFailed(hr uintptr) bool { return int32(uint32(hr)) < 0 }

// hresultError 把一个失败的 HRESULT 变成 error。
func hresultError(what string, hr uintptr) error {
	return fmt.Errorf("media: %s 失败（HRESULT %#08x）", what, uint32(hr))
}

// ---- 虚表调用 ----

// comCall 调 obj 的虚表第 i 个方法（0 是 QueryInterface）。
//
// ⚠️ `//go:uintptrescapes` 不能省：传进去的 `uintptr(unsafe.Pointer(&x))`
// 那个 Go 指针必须在调用期间活着，否则栈一增长它就搬家，COM 那边拿到的是
// 野指针。MyGo 的同一层有一模一样的注释。
//
//go:uintptrescapes
func comCall(obj uintptr, i int, args ...uintptr) uintptr {
	// 虚表指针的**第 i 格**就是第 i 个方法。分两段读是因为 `vtbl` 是一个
	// uintptr：直接拿它做指针运算要先过一遍 `native`（理由见那里）。
	vtbl := *(*uintptr)(native(obj))
	fn := *(*uintptr)(native(vtbl + uintptr(i*ptrSize)))
	var a [8]uintptr
	a[0] = obj
	n := copy(a[1:], args)
	r, _, _ := syscall.SyscallN(fn, a[:n+1]...)
	return r
}

// ptrSize 是虚表一格多大（一个指针）。下标乘它得到方法地址。
const ptrSize = int(unsafe.Sizeof(uintptr(0)))

// native 把一个「系统给回来的地址」重新当成指针用。
//
// 绕这一下**不是**为了躲开什么检查，是因为直接写 `unsafe.Pointer(x)`
// （x 是 uintptr）在 Go 的规矩里是**明确禁止**的：那个转换不保证保留指针，
// 走一遍 GC 可能就变了。绕法是把这个 uintptr 变量的位**原地**重新解释成
// 一个指针（不做数值转换），这是唯一在 Windows 上和 C 交换地址时站得住的
// 写法——MyGo 自己那一层也是这么写的（`mygo/internal/windows/win32.go`）。
func native(addr uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&addr)) }

// comSetBool 调一个「收一个 BOOL」的 setter。COM 的 BOOL 就是 4 字节的 0/1。
func comSetBool(obj uintptr, i int, v bool) uintptr {
	n := uintptr(0)
	if v {
		n = 1
	}
	return comCall(obj, i, n)
}

// queryInterface 问 obj 要 iid 那一个接口，返回它。
//
// 返回 0 表示系统那边**没有**这个接口——那在这一层只有两种可能：这个系统
// 版本太老，或者那个 IID 抄错了。两种都不该让应用崩，所以返回 0 由调用点
// 自己决定（降级或者记一条日志）。
func queryInterface(obj uintptr, iid guid) uintptr {
	if obj == 0 {
		return 0
	}
	var out uintptr
	if hresultFailed(comCall(obj, 0, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&out)))) {
		return 0
	}
	return out
}

// addRef / release 加一个引用 / 放掉一个。
//
// **两者都不看返回值**：AddRef 的返回值只对「我到底是不是最后一个」有意义，
// 而这里从头到尾都不靠引用计数判断生死（见 windowsPlatform 的 close），
// 记那个数反而会让人以为它是准的。
func addRef(obj uintptr) {
	if obj != 0 {
		comCall(obj, 1)
	}
}

func release(obj uintptr) {
	if obj != 0 {
		comCall(obj, 2)
	}
}

// ---- WinRT 激活 ----

// roInitializeMTA 是 `RO_INIT_MULTITHREADED`。
//
// ⚠️ **返回值一律不当失败**。三种结果都接受：S_OK 是我们把它初始化了；
// S_FALSE 是它早就是 MTA 了；`RPC_E_CHANGED_MODE` 是这条线程已经用别的模式
// （多半是 UI 线程上的 STA）初始化过了——**那种情况下 COM 照样是就绪的**，
// 而下面那次 `RoGetActivationFactory` 照样能用。
//
// 之所以必须调：不调的话 `RoGetActivationFactory` 可能返回
// `CO_E_NOTINITIALIZED`，而那会让整个系统媒体会话哑掉，且**界面这边看不出
// 任何异样**（会话是个哑巴，见 media.go 的 Available）。
func roInitialize() {
	if procRoInitialize.Find() != nil {
		return
	}
	procRoInitialize.Call(uintptr(roInitMultiThreaded))
}

// roInitMultiThreaded 是 `RO_INIT_MULTITHREADED`。
const roInitMultiThreaded = 0x1

// createInstance 是 `IActivationFactory::CreateInstance` 的虚表下标。
const createInstance = 6

// iidActivationFactory 是 `Windows.Foundation.IActivationFactory`。
//
// ⚠️ **要用 `RoGetActivationFactory` 而不是 `RoActivateInstance` 建实例**：
// 后者在 STA 线程上返回 `RPC_E_CHANGED_MODE`，而这一票最要紧的就是别在 UI
// 线程上失败。MS 文档给桌面应用的那一段「集成 SMTC」用的也是前一种。
var iidActivationFactory = mustGUID("00000035-0000-0000-c000-000000000046")

// activationFactory 取一个运行时类的激活工厂，在 iid 那一个接口上。
//
// ⚠️ **这一步不是建实例**，它拿到的就是那个工厂对象本身。有一处就是靠这个
// 特性做的：SMTC 的工厂对象同时实现了 `IActivationFactory` 和
// `ISystemMediaTransportControlsInterop`，而我们要的是**后一个**——桌面应用
// 拿到媒体会话的正规途径就是直接问这个工厂要 `GetForWindow`。
func activationFactory(class string, iid guid) (uintptr, error) {
	name, err := winString(class)
	if err != nil {
		return 0, err
	}
	defer name.Free()

	var factory uintptr
	hr, _, _ := procRoGetFactory.Call(
		name.Handle(),
		uintptr(unsafe.Pointer(&iid)),
		uintptr(unsafe.Pointer(&factory)))
	if hresultFailed(hr) || factory == 0 {
		return 0, hresultError("取 "+class+" 的激活工厂", hr)
	}
	return factory, nil
}

// activate 建一个运行时类的实例，返回它在 iid 那一个接口上的指针。
func activate(class string, iid guid) (uintptr, error) {
	factory, err := activationFactory(class, iidActivationFactory)
	if err != nil {
		return 0, err
	}
	defer release(factory)

	// `IActivationFactory` 是 `IInspectable` 派生的：`CreateInstance` 是第 6 格
	// （前面 IUnknown 三格加 IInspectable 三格）。
	var raw uintptr
	if hr := comCall(factory, createInstance, uintptr(unsafe.Pointer(&raw))); hresultFailed(hr) || raw == 0 {
		return 0, hresultError("创建 "+class+" 的实例", hr)
	}
	defer release(raw)

	obj := queryInterface(raw, iid)
	if obj == 0 {
		return 0, errPlatform(class + " 上没有我们要的那个接口（IID 抄错了？）")
	}
	return obj, nil
}

// ---- HSTRING ----

// winStr 是一个 WinRT 的 HSTRING（不可变 UTF-16 串）。
type winStr struct{ h uintptr }

// winString 把一个 Go 字符串变成 HSTRING。
func winString(s string) (winStr, error) {
	if procWinStringCreate.Find() != nil {
		return winStr{}, fmt.Errorf("media: 这个系统上没有 WindowsCreateString")
	}
	if s == "" {
		// 空串是合法的 HSTRING，但造它要走一遍 UTF-16 编码；直接给 0
		// （空 HSTRING）省掉这一次，也让「空就是没有」在下面几处好判。
		return winStr{}, nil
	}
	units := utf16.Encode([]rune(s))
	if len(units) == 0 {
		return winStr{}, nil
	}
	var h uintptr
	hr, _, _ := procWinStringCreate.Call(
		uintptr(unsafe.Pointer(&units[0])), uintptr(len(units)),
		uintptr(unsafe.Pointer(&h)))
	runtime.KeepAlive(units)
	if hresultFailed(hr) {
		return winStr{}, hresultError("造 HSTRING", hr)
	}
	return winStr{h: h}, nil
}

// Handle 是那个 HSTRING 指针。0 表示空串。
func (s winStr) Handle() uintptr { return s.h }

// Free 把 HSTRING 还回去。
func (s winStr) Free() {
	if s.h != 0 && procWinStringDelete.Find() == nil {
		procWinStringDelete.Call(s.h)
	}
	s.h = 0
}

// ---- HRESULT 常量 ----

// 几个要认的 HRESULT（winerror.h / objbase.h）。
const (
	// hrNoInterface 是 `E_NOINTERFACE`：那个 IID 这个对象没有。**本层最常见
	// 的一个错误就是抄错 IID**，症状就是它。
	hrNoInterface = 0x80004002
	// hrChangedMode 是 `RPC_E_CHANGED_MODE`：这条线程的 COM 模式已经定了，
	// 而且和我们要的不一样。这不是错误（见 roInitialize）。
	hrChangedMode = 0x80010106
)
