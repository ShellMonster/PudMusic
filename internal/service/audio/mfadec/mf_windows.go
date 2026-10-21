//go:build windows && (amd64 || arm64)

package mfadec

// Media Foundation 的纯 Go 绑定：系统函数的加载、COM 虚表调用，
// 以及「一条专给 COM 用的线程」。
//
// 和 macOS 那边的 `extaf` 是同一个位置、同一个理由——**注册全部在 init 里做完**，
// 运行期不再碰动态解析。
//
// # 这一层真正的风险不在代码，在「抄错常量不报错」
//
// GUID、COM 虚表下标、结构体布局，三样东西写错了都不会崩：
// 一个 HRESULT 说不清，或者干脆「返回 0 但什么都没做」。
// GUID 和虚表下标搬到了平台无关的 `guid.go` 里，由 `guid_test.go`
// 在 macOS 上对着 Windows SDK 的字节逐条钉住；这一份文件里剩下的结构体
// 布局和函数签名，只能靠 Windows 真机——也就是这一票的 CI 作业。

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// hresult 是 Windows 的 HRESULT：成功是非负，失败是负数。
type hresult = int32

const sOK hresult = 0

// mfHResultName 是上面那几个码对应的名字。
//
// 解成名字而不是只打十进制/十六进制：MF 的失败原因**绝大多数**是「这台
// Windows 上没有这个东西」或者「喂进去的东西不对」，而这两种在界面上要
// 显示的话术完全不同。只有 `0x8000FFFF` 那种笼统的才算「不知道」。
var mfHResultName = map[uint32]string{
	hrEInvalidArg:            "E_INVALIDARG",
	hrEOutOfMemory:           "E_OUTOFMEMORY",
	hrENotImpl:               "E_NOTIMPL",
	hrEUnexpected:            "E_UNEXPECTED",
	hrEAccessDenied:          "E_ACCESSDENIED",
	hrENotInitialized:        "CO_E_NOTINITIALIZED",
	hrChangedMode:            "RPC_E_CHANGED_MODE",
	hrMFEndOfStream:          "MF_E_ENDOFSTREAM",
	hrMFNotAccepting:         "MF_E_NOTACCEPTING",
	hrMFInvalidRequest:       "MF_E_INVALIDREQUEST",
	hrMFUnsupportedType:      "MF_E_UNSUPPORTED_BYTESTREAM_TYPE",
	hrTransformNeedMoreInput: "MF_E_TRANSFORM_NEED_MORE_INPUT",
	hrTransformStreamChange:  "MF_E_TRANSFORM_STREAM_CHANGE",
	hrTransformTypeNotSet:    "MF_E_TRANSFORM_TYPE_NOT_SET",
}

// comApartmentErrors 数的是「COM apartment 出问题」的次数。
//
// 这两个 HRESULT（CO_E_NOTINITIALIZED 和 RPC_E_CHANGED_MODE）是这一票
// **最贵的那个坑**的全部症状：COM 在这条线程上没初始化，或者初始化过了
// 但模式不对。MS 的 `CoInitializeEx` 文档逐字写着「每个用 COM 的线程至少
// 调一次，concurrency flag 不匹配返回 RPC_E_CHANGED_MODE」。
//
// Go 侧为什么躲不掉：goroutine 不绑线程；Go 1.14 起的异步抢占能在线性代码
// 中间把 goroutine 换个 M；`syscall.SyscallN` 自己的运行时注释就写着
// 「may reschedule us on to a different M」。三者叠加的后果是**偶发**的，
// 小规模测试跑不出来——所以这个计数是**在负载下反复跑**那条验收的依据。
var comApartmentErrors atomic.Int64

// hresultError 把一个失败的 HRESULT 说成人话。
func hresultError(what string, hr hresult) error {
	if hr >= 0 {
		return nil
	}
	if uint32(hr) == hrENotInitialized || uint32(hr) == hrChangedMode {
		comApartmentErrors.Add(1)
	}
	if name, ok := mfHResultName[uint32(hr)]; ok {
		return fmt.Errorf("mfadec: %s: %s (HRESULT %#08x)", what, name, uint32(hr))
	}
	return fmt.Errorf("mfadec: %s: HRESULT %#08x", what, uint32(hr))
}

// ── 系统函数的加载 ──────────────────────────────────────────────────────────

// DLL **一律从系统目录按全路径加载**，不靠搜索路径。理由和 `internal/media`
// 那一层一样：搜索路径里第一个是**可执行文件所在的目录**，而那个目录用户
// 是能写的。从那里先找到一份同名 DLL 就等于把「加载系统组件」这一步交出去了。
//
// mfplat.dll 和 ole32.dll 都是系统组件，不进 go list -deps，也没有许可证
// 要随包发出去——见 `internal/licensing` 里的说明。

var (
	mfPlat    = systemDLL("mfplat.dll")
	ole32     = systemDLL("ole32.dll")
	kernel32d = kernel32DLL()
)

var (
	procMFStartup            = mfPlat.NewProc("MFStartup")
	procMFCreateMediaType    = mfPlat.NewProc("MFCreateMediaType")
	procMFCreateSample       = mfPlat.NewProc("MFCreateSample")
	procMFCreateMemoryBuffer = mfPlat.NewProc("MFCreateMemoryBuffer")
	procCoInitializeEx       = ole32.NewProc("CoInitializeEx")
	procCoUninitialize       = ole32.NewProc("CoUninitialize")
	procCoCreateInstance     = ole32.NewProc("CoCreateInstance")
	procGetCurrentThreadID   = kernel32d.NewProc("GetCurrentThreadId")
)

// mfVersion 是 `MF_VERSION`。
//
// SDK 的 `um/mfapi.h` 在 WINVER >= _WIN32_WINNT_WIN7 时是
// `MF_SDK_VERSION = 0x00020070`。这个值是**编译期**定死的，不同 Windows
// 版本能接受的 MF_VERSION 不同，传太老的新版会返回 E_INVALIDARG。
const mfVersion = 0x00020070

// mfStartupOnce 保证 MFStartup 只调一次。
//
// ⚠️ **不调 MFShutdown**：MFShutdown 要和 MFStartup 同一个线程成对调用，
// 而解码器的 Close 可能来自任何一个 goroutine。少调一次的实际后果是进程
// 退出时 MF 的几块全局内存没释放——进程都要没了，这件事不值得为它引入
// 一条跨线程的配对约束。文档里也明写 MFShutdown 是「不需要就别调」。
var mfStartupOnce sync.Once

// mfStartupErr 是 MFStartup 的结果（只会有一次）。
var mfStartupErr error

// startMF 初始化 Media Foundation，进程一辈子做一次。
//
// 它在**第一条 COM 线程**上调用：MF 的对象要在初始化过 COM 的线程上用，
// 而 MFStartup 自己只管进程级的全局状态。
func startMF() error {
	mfStartupOnce.Do(func() {
		if procMFStartup.Find() != nil {
			mfStartupErr = fmt.Errorf("mfadec: 这个系统上没有 MFStartup（缺 mfplat.dll？）")
			return
		}
		hr, _, _ := procMFStartup.Call(mfVersion, 0)
		if int32(uint32(hr)) < 0 {
			mfStartupErr = hresultError("MFStartup", hresult(int32(uint32(hr))))
		}
	})
	return mfStartupErr
}

// ── COM 虚表调用 ────────────────────────────────────────────────────────────

// ptrSize 是虚表一格多大（一个指针）。下标乘它得到方法地址。
const ptrSize = int(unsafe.Sizeof(uintptr(0)))

// native 把一个「系统给回来的地址」重新当成指针用。
//
// 绕这一下**不是**为了躲开什么检查，是因为直接写 `unsafe.Pointer(x)`
// （x 是 uintptr）在 Go 的规矩里是**明确禁止的**：那个转换不保证保留指针，
// 走一遍 GC 可能就变了。绕法是把这个 uintptr 变量的位**原地**重新解释成
// 一个指针（不做数值转换），这是唯一在 Windows 上和 C 交换地址时站得住的
// 写法——`internal/media` 那一层也是这么写的。
func native(addr uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&addr)) }

// comCall 调 obj 的虚表第 i 个方法（0 是 QueryInterface）。
//
// ⚠️ `//go:uintptrescapes` 不能省：传进去的 `uintptr(unsafe.Pointer(&x))`
// 那个 Go 指针必须在调用期间活着，否则栈一增长它就搬家，COM 那边拿到的是
// 野指针。
//
//go:uintptrescapes
func comCall(obj uintptr, i int, args ...uintptr) uintptr {
	// 虚表指针的**第 i 格**就是第 i 个方法。分两段读是因为 `vtbl` 是一个
	// uintptr：直接拿它做指针运算要先过一遍 `native`。
	vtbl := *(*uintptr)(native(obj))
	fn := *(*uintptr)(native(vtbl + uintptr(i*ptrSize)))
	// ⚠️ 19 格：`syscall.SyscallN` 在 Windows 上支持到 18 个参数（外加 this）。
	// 原来这里是 8 格，超出的参数会被 `copy` **静默丢掉**——那不是「少传一个
	// 参数」，是「传了一个错的参数」，而且不报错。
	var a [19]uintptr
	a[0] = obj
	n := copy(a[1:], args)
	r, _, _ := syscall.SyscallN(fn, a[:n+1]...)
	return r
}

// release 放掉一个 COM 对象的最后一个引用。
func release(obj uintptr) {
	if obj == 0 {
		return
	}
	comCall(obj, vtblRelease)
}

// ── 一条专给 COM 用的线程 ───────────────────────────────────────────────────

// comThread 是**一条钉住的 OS 线程**，COM 的所有调用都在它上面发生。
//
// # 为什么不能「随手在当前 goroutine 上调 COM」
//
// 微软 `CoInitializeEx` 的文档逐字：**每个用 COM 的线程至少调一次**，
// concurrency flag 不匹配返回 `RPC_E_CHANGED_MODE`。COM 的 apartment 是
// **按线程**分的，不是按进程，也不是按 goroutine——而 Go 的 goroutine
// 不绑线程。三条叠加起来：
//
//   - goroutine 不绑线程；
//   - Go 1.14 起的异步抢占能在线性代码中间把 goroutine 换个 M
//     （所以「我这个函数没有 channel 所以线程不会变」是错的）；
//   - `syscall.SyscallN` 自己的运行时注释就写着
//     「may reschedule us on to a different M」。
//
// 后果是**偶发**的 `CO_E_NOTINITIALIZED` / `RPC_E_CHANGED_MODE`，
// 小规模测试跑不出来。
//
// # 这条线程怎么保证不被换掉
//
// `runtime.LockOSThread()` 把这个 goroutine 钉在它当前那条 OS 线程上。
// 本类型里，worker 在**两次工作之间**是阻塞在 Go 的 channel 上的
// （不是阻塞在 Windows 的系统调用里）——Go 的 channel 等待不离开 M，
// 而被钉住的 goroutine 恢复运行时**必须**回到它自己那条 M
// （运行时的 `lockedm` 就是干这个的）。
//
// ⚠️ 这不是**证明**，是**尽量**：`comCall` 走的是 `syscall.SyscallN`，
// 那是一次系统调用，理论上慢调用可能让 M 被系统抢走。所以这里**量**它：
// 每做一次工作就比一次线程 ID，换过就记一笔（`switches`）。
// CI 上那条「在负载下反复跑」的验收看的就是这个数。
type comThread struct {
	jobs chan func()
	// closed 一关，worker 就收摊，而 `do` 也不再往里投活。
	//
	// ⚠️ 原来这里是「`quit` 一关就返回」，而 `do` 是**无条件**往 `jobs` 里投：
	// worker 的 select 可能挑了 `quit` 那一支，于是投活的那一方**永远等不到
	// 回音**——`do` 死等，调用方跟着挂住。
	closed chan struct{}
	done   chan struct{}

	// threadID 是初始化时那条线程的 ID。
	threadID uint32
	// switches 数的是「做工作的时候发现自己换线程了」的次数。
	switches atomic.Int64
	// works 是这个线程一共做了多少件事。
	works atomic.Int64
}

// newComThread 起一条专给 COM 用的线程。
func newComThread() (*comThread, error) {
	t := &comThread{
		jobs:   make(chan func()),
		closed: make(chan struct{}),
		done:   make(chan struct{}),
	}
	ready := make(chan error, 1)
	go func() {
		defer close(t.done)
		// ⚠️ 这一句是**整个设计的地基**：不钉住，worker 随时可能换 M，
		// 而 COM 的 apartment 是按线程分的。
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		// 用 MTA 而不是 STA：MFT 内部有工作线程，STA 会把它们泵到自己队列上，
		// 而在没有消息循环的解码线程上那些消息没人泵——表现是
		// `ProcessOutput` 永远不返回。
		//
		// ⚠️ **MTA 是 0x0，STA 是 0x2**（`um/objbase.h` 的 `tagCOINIT`）——
		// 这两个千万别记反，记反了这条线程其实是 STA，而 STA 正是要避开的那个。
		hr := coInitializeEx(0, coinitMultiThreaded)
		switch {
		case hr >= 0:
			// S_OK（第一次）或 S_FALSE（这条线程已经是 MTA 了）。
			// 两种都要 CoUninitialize 配平——**每一次成功的初始化都得配一次**。
			defer coUninitialize()
		case uint32(hr) == hrChangedMode:
			// 这条线程已经被人初始化成 STA 了（多半是 UI 线程）。
			// COM 仍然是就绪的，能用；但**那个初始化不是我们做的**，
			// 不能由我们来 CoUninitialize。
			comApartmentErrors.Add(1)
		default:
			ready <- hresultError("CoInitializeEx", hr)
			return
		}

		if err := startMF(); err != nil {
			ready <- err
			return
		}

		t.threadID = currentThreadID()
		ready <- nil

		for {
			select {
			case job := <-t.jobs:
				// 量一下：这一趟是不是还在原来那条线程上。
				if id := currentThreadID(); id != t.threadID {
					t.switches.Add(1)
				}
				t.works.Add(1)
				job()
			case <-t.closed:
				return
			}
		}
	}()

	if err := <-ready; err != nil {
		close(t.closed)
		<-t.done
		return nil, err
	}
	return t, nil
}

// do 把一段代码放到那条 COM 线程上跑，等它跑完。
//
// 线程已经收摊的话，这一趟**什么也不做**就返回（不报错）——调用方拿到的
// 是「没做」而不是「做了一半」。
func (t *comThread) do(fn func()) {
	done := make(chan struct{})
	job := func() {
		defer close(done)
		fn()
	}
	select {
	case t.jobs <- job:
		<-done
	case <-t.closed:
		// 线程没了，这一趟没做。
	}
}

// workCount 是这个线程一共做了多少件事（诊断用）。
func (t *comThread) workCount() int64 { return t.works.Load() }

// close 把那条线程收掉。
//
// **只能调一次**——再调一次就是 `close of closed channel` 的 panic。
func (t *comThread) close() {
	close(t.closed)
	<-t.done
}

// ── 绑好的系统函数 ──────────────────────────────────────────────────────────

// coInitializeEx 是 `CoInitializeEx(LPV Reserved, DWORD dwCoInit)`。
//
//go:uintptrescapes
func coInitializeEx(reserved uintptr, flags uint32) hresult {
	hr, _, _ := procCoInitializeEx.Call(reserved, uintptr(flags))
	return hresult(int32(uint32(hr)))
}

// coUninitialize 是 `CoUninitialize()`。
func coUninitialize() {
	procCoUninitialize.Call()
}

// coCreateInstance 是 `CoCreateInstance(REFCLSID, LPUNKNOWN, DWORD, REFIID, LPVOID*)`。
//
// ⚠️ `CLSCTX_INPROC_SERVER` = 1：MFT 是进程内的。
//
//go:uintptrescapes
func coCreateInstance(clsid, iid *guid, out *uintptr) hresult {
	hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(clsid)),
		0, // pUnkOuter
		1, // CLSCTX_INPROC_SERVER
		uintptr(unsafe.Pointer(iid)),
		uintptr(unsafe.Pointer(out)))
	return hresult(int32(uint32(hr)))
}

// mfCreateMediaType 是 `MFCreateMediaType(IMFMediaType**)`。
//
//go:uintptrescapes
func mfCreateMediaType(out *uintptr) hresult {
	hr, _, _ := procMFCreateMediaType.Call(uintptr(unsafe.Pointer(out)))
	return hresult(int32(uint32(hr)))
}

// mfCreateSample 是 `MFCreateSample(IMFSample**)`。
//
//go:uintptrescapes
func mfCreateSample(out *uintptr) hresult {
	hr, _, _ := procMFCreateSample.Call(uintptr(unsafe.Pointer(out)))
	return hresult(int32(uint32(hr)))
}

// mfCreateMemoryBuffer 是 `MFCreateMemoryBuffer(DWORD cbMaxLength, IMFMediaBuffer**)`。
//
//go:uintptrescapes
func mfCreateMemoryBuffer(maxLength uint32, out *uintptr) hresult {
	hr, _, _ := procMFCreateMemoryBuffer.Call(uintptr(maxLength), uintptr(unsafe.Pointer(out)))
	return hresult(int32(uint32(hr)))
}

// currentThreadID 是 `GetCurrentThreadId()`。
func currentThreadID() uint32 {
	id, _, _ := procGetCurrentThreadID.Call()
	return uint32(id)
}

// ── 媒体类型上的那几个属性 ───────────────────────────────────────────────────

// comHR 把 comCall 的返回值当成 HRESULT。
//
// COM 的返回值在返回寄存器的低 32 位里（x64 上是 RAX），而 HRESULT 是
// **有符号**的 32 位：失败的那些在 int32 里是负数。截到 32 位再按有符号
// 解释，才对得上 `E_INVALIDARG` 那些常量。
func comHR(r uintptr) hresult { return hresult(int32(uint32(r))) }

// setGUID 给一个 IMFAttributes（媒体类型也是）写一个 GUID 属性。
//
//go:uintptrescapes
func setGUID(attrs uintptr, key *guid, value *guid) hresult {
	return comHR(comCall(attrs, vtblAttributesSetGUID,
		uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(value))))
}

// setUINT32 给一个 IMFAttributes 写一个 UINT32 属性。
//
//go:uintptrescapes
func setUINT32(attrs uintptr, key *guid, value uint32) hresult {
	return comHR(comCall(attrs, vtblAttributesSetUINT32,
		uintptr(unsafe.Pointer(key)), uintptr(value)))
}

// setBlob 给一个 IMFAttributes 写一个 blob 属性。
//
//go:uintptrescapes
func setBlob(attrs uintptr, key *guid, value []byte) hresult {
	if len(value) == 0 {
		return sOK
	}
	return comHR(comCall(attrs, vtblAttributesSetBlob,
		uintptr(unsafe.Pointer(key)),
		uintptr(unsafe.Pointer(&value[0])),
		uintptr(len(value))))
}

// getUINT32 从一个 IMFAttributes 上读一个 UINT32 属性。
//
// 读不到**不算错**：MF 不会给每种媒体类型都填满所有属性，而我们要的那两三个
// 恰好是音频必填的。调用方拿 (false, nil) 当「这个属性不存在」。
//
//go:uintptrescapes
func getUINT32(attrs uintptr, key *guid) (uint32, bool, hresult) {
	if attrs == 0 {
		return 0, false, sOK
	}
	var v uint32
	hr := comHR(comCall(attrs, vtblAttributesGetUINT32,
		uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&v))))
	if hr < 0 {
		// E_INVALIDARG 就是「这个键不存在」，不是失败。
		if uint32(hr) == hrEInvalidArg {
			return 0, false, sOK
		}
		return 0, false, hr
	}
	return v, true, sOK
}

// getGUID 从一个 IMFAttributes 上读一个 GUID 属性。
//
//go:uintptrescapes
func getGUID(attrs uintptr, key *guid) (guid, bool, hresult) {
	if attrs == 0 {
		return guid{}, false, sOK
	}
	var v guid
	hr := comHR(comCall(attrs, vtblAttributesGetGUID,
		uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&v))))
	if hr < 0 {
		if uint32(hr) == hrEInvalidArg {
			return guid{}, false, sOK
		}
		return guid{}, false, hr
	}
	return v, true, sOK
}

// ── 样本与缓冲 ──────────────────────────────────────────────────────────────

// mftOutputDataBuffer 是 SDK 的 `MFT_OUTPUT_DATA_BUFFER`。
//
// ⚠️ 布局要按 x64 的自然对齐摆：`dwStreamID` 是 4 字节，后面那个指针要 8 字节
// 对齐，所以中间空 4 字节；`dwStatus` 又是 4 字节，`pEvents` 指针再要 8 字节
// 对齐，中间又空 4 字节。整个结构 32 字节。
type mftOutputDataBuffer struct {
	StreamID uint32
	_        uint32
	Sample   uintptr
	Status   uint32
	_        uint32
	Events   uintptr
}

// mftOutputStreamInfo 是 SDK 的 `MFT_OUTPUT_STREAM_INFO`。
//
// ⚠️ 布局：`um/mftransform.h` 里它是**三个 DWORD**——
//
//	typedef struct _MFT_OUTPUT_STREAM_INFO
//	    {
//	    DWORD dwFlags;
//	    DWORD cbSize;
//	    DWORD cbAlignment;
//	    } MFT_OUTPUT_STREAM_INFO;
//
// 这和 DirectShow 那个同名结构（带 `NumOutputBuffers` 和
// `BytesPerOutputSample`）**不是一回事**，抄错的话读到的全是垃圾。
type mftOutputStreamInfo struct {
	Flags     uint32
	Size      uint32
	Alignment uint32
}

// MFT_OUTPUT_STREAM_INFO 的 dwFlags。
const (
	// mftOutputStreamProvidesSamples：MFT 自己分配样本。
	mftOutputStreamProvidesSamples = 0x00000100
	// mftOutputStreamCanProvideSamples：调用方**可以**自己给一个样本；
	// 给 NULL 的话 MFT 会自己分配。
	mftOutputStreamCanProvideSamples = 0x00000200
)

// MFT_OUTPUT_DATA_BUFFER 的 dwStatus。
const (
	mftOutputDataIncomplete   = 0x01000000
	mftOutputDataFormatChange = 0x00000100
	mftOutputDataStreamEnd    = 0x00000200
	mftOutputDataNoSample     = 0x00000300
)
