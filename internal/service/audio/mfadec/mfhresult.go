package mfadec

// Media Foundation 的 HRESULT 常量和 COM 的 apartment 模式常量。
//
// **这一份不带平台标签**：它们就是几个数——而正因为是几个数，**写错了没有任何
// 编译期检查**。所以单独放一份、单独钉一份测试（`mfhresult_test.go`）。
//
// 出处（逐个核过，不是照印象写的）：
//
//   - `um/Mferror.h`——Media Foundation 自己的错误码；
//   - `shared/winerror.h`——通用 HRESULT；
//   - `um/objbase.h`——`tagCOINIT`。

// HRESULT 常量。全部按 `unsigned int` 写，因为失败的 HRESULT 在 int32 里是负数，
// 直接和常量比会编译不过（`0x8000...` 装不进 int32）。
//
// ⚠️⚠️ **这些值是对着 SDK 头文件核过的**：
//
//   - **MF 自己的错误码是 `0xC00Dxxxx`**（`um/Mferror.h`），不是 `0x800Cxxxx`
//     ——后者是 COM 的 HRESULT 里 FACILITY 为 `0xC00` 的**别的**码。
//   - `CO_E_NOTINITIALIZED` / `RPC_E_CHANGED_MODE` 这些**通用**的才是
//     `0x8xxxxxxx`。
//
// 写错的后果**不是报错**，是「正常的等待输入被当成解码失败」：MFT 说「还没攒够」
// 返回 `MF_E_TRANSFORM_NEED_MORE_INPUT`，而这里比的是一个别的码，于是走进失败
// 分支，一次正常的解码被报成失败。
const (
	// 通用 HRESULT（`shared/winerror.h`）。
	hrEInvalidArg     = uint32(0x80070057) // E_INVALIDARG
	hrEOutOfMemory    = uint32(0x8007000E) // E_OUTOFMEMORY
	hrENotImpl        = uint32(0x80004001) // E_NOTIMPL
	hrEUnexpected     = uint32(0x8000FFFF) // E_UNEXPECTED
	hrEAccessDenied   = uint32(0x80070005) // E_ACCESSDENIED
	hrENotInitialized = uint32(0x800401F0) // CO_E_NOTINITIALIZED
	hrChangedMode     = uint32(0x80010106) // RPC_E_CHANGED_MODE

	// Media Foundation 的（`um/Mferror.h`）——**`0xC00Dxxxx`**。
	hrMFEndOfStream     = uint32(0xC00D3E84) // MF_E_END_OF_STREAM
	hrMFNotAccepting    = uint32(0xC00D36B5) // MF_E_NOTACCEPTING
	hrMFInvalidRequest  = uint32(0xC00D36B2) // MF_E_INVALIDREQUEST
	hrMFUnsupportedType = uint32(0xC00D36C4) // MF_E_UNSUPPORTED_BYTESTREAM_TYPE
	// MF_E_TRANSFORM_NEED_MORE_INPUT：这一趟没有输出，得多喂点。
	hrTransformNeedMoreInput = uint32(0xC00D6D72)
	// MF_E_TRANSFORM_STREAM_CHANGE：输出流变了，要重新问一次媒体类型。
	hrTransformStreamChange = uint32(0xC00D6D61)
	// MF_E_TRANSFORM_TYPE_NOT_SET：媒体类型还没设。
	hrTransformTypeNotSet = uint32(0xC00D6D60)
)

// COM 的 apartment 模式（SDK 的 `um/objbase.h`，`tagCOINIT`）。
//
// ⚠️ **MTA 是 0x0，STA 是 0x2**——这两个千万别记反：记反了的话
// `CoInitializeEx(0, coinitMultiThreaded)` 初始化出来的是**单线程** apartment，
// 而这一层要的是多线程的（见 `newComThread` 的注释）。
const (
	// coinitMultiThreaded 是 `COINIT_MULTITHREADED`（MTA）。
	coinitMultiThreaded = 0x0
	// coinitApartmentThreaded 是 `COINIT_APARTMENTTHREADED`（STA）。
	coinitApartmentThreaded = 0x2
)
