package mfadec

// 把 HRESULT 和 apartment 模式的常量**逐个**对着 SDK 头文件钉住。
//
// # 为什么值得单立一条
//
// 这些是**几个数**，写错了没有任何编译期检查。而写错的后果**不是报错**，是
// 「正常的状态被当成失败」：
//
//   - `MF_E_TRANSFORM_NEED_MORE_INPUT` 写错 → MFT 说「还没攒够」时走进失败
//     分支，**一次正常的解码被报成失败**；
//   - `COINIT_MULTITHREADED` 写错 → 那条 COM 线程其实是 **STA**，而 STA 正是
//     这一层要避开的（消息没人泵，`ProcessOutput` 永远不返回）。
//
// 这两条**都发生过**（见 `mfhresult.go` 顶上那段注释），所以这里钉住。
//
// 出处：`um/Mferror.h`、`shared/winerror.h`、`um/objbase.h`。

import "testing"

func TestTheHRESULTsAreWhatTheSDKDefines(t *testing.T) {
	cases := []struct {
		name string
		got  uint32
		sdk  uint32
	}{
		// shared/winerror.h
		{"E_INVALIDARG", hrEInvalidArg, 0x80070057},
		{"E_OUTOFMEMORY", hrEOutOfMemory, 0x8007000E},
		{"E_NOTIMPL", hrENotImpl, 0x80004001},
		{"E_UNEXPECTED", hrEUnexpected, 0x8000FFFF},
		{"E_ACCESSDENIED", hrEAccessDenied, 0x80070005},
		{"CO_E_NOTINITIALIZED", hrENotInitialized, 0x800401F0},
		{"RPC_E_CHANGED_MODE", hrChangedMode, 0x80010106},
		// um/Mferror.h —— **MF 的错误码是 0xC00Dxxxx**
		{"MF_E_END_OF_STREAM", hrMFEndOfStream, 0xC00D3E84},
		{"MF_E_NOTACCEPTING", hrMFNotAccepting, 0xC00D36B5},
		{"MF_E_INVALIDREQUEST", hrMFInvalidRequest, 0xC00D36B2},
		{"MF_E_UNSUPPORTED_BYTESTREAM_TYPE", hrMFUnsupportedType, 0xC00D36C4},
		{"MF_E_TRANSFORM_NEED_MORE_INPUT", hrTransformNeedMoreInput, 0xC00D6D72},
		{"MF_E_TRANSFORM_STREAM_CHANGE", hrTransformStreamChange, 0xC00D6D61},
		{"MF_E_TRANSFORM_TYPE_NOT_SET", hrTransformTypeNotSet, 0xC00D6D60},
	}
	for _, c := range cases {
		if c.got != c.sdk {
			t.Errorf("%s = %#08x，SDK 里是 %#08x", c.name, c.got, c.sdk)
		}
	}
}

// TestTheMFErrorCodesAreInTheC00DFacility 钉住「MF 的错误码是 0xC00D 这一段」。
//
// 写成 `0x800Cxxxx` 的那几个（NEED_MORE_INPUT、STREAM_CHANGE、TYPE_NOT_SET）
// 是**别的地方**的码——它们长得像，但每一个都对应着另一个意思的 HRESULT。
func TestTheMFErrorCodesAreInTheC00DFacility(t *testing.T) {
	for name, v := range map[string]uint32{
		"MF_E_END_OF_STREAM":               hrMFEndOfStream,
		"MF_E_NOTACCEPTING":                hrMFNotAccepting,
		"MF_E_INVALIDREQUEST":              hrMFInvalidRequest,
		"MF_E_UNSUPPORTED_BYTESTREAM_TYPE": hrMFUnsupportedType,
		"MF_E_TRANSFORM_NEED_MORE_INPUT":   hrTransformNeedMoreInput,
		"MF_E_TRANSFORM_STREAM_CHANGE":     hrTransformStreamChange,
		"MF_E_TRANSFORM_TYPE_NOT_SET":      hrTransformTypeNotSet,
	} {
		if v&0xFFFF0000 != 0xC00D0000 {
			t.Errorf("%s = %#08x，MF 的错误码该在 0xC00D 这一段里", name, v)
		}
	}
}

// TestTheApartmentModesAreWhatTheSDKDefines 钉住 STA / MTA 的取值。
//
// ⚠️ **MTA 是 0x0，STA 是 0x2**——记反了的话那条 COM 线程其实是单线程的。
func TestTheApartmentModesAreWhatTheSDKDefines(t *testing.T) {
	if coinitMultiThreaded != 0x0 {
		t.Errorf("COINIT_MULTITHREADED = %#x，SDK 里是 0x0", coinitMultiThreaded)
	}
	if coinitApartmentThreaded != 0x2 {
		t.Errorf("COINIT_APARTMENTTHREADED = %#x，SDK 里是 0x2", coinitApartmentThreaded)
	}
	// 这两个必须**不相等**：把它们记成同一个数，代码里就没有「用 MTA 而不是
	// STA」这回事了。
	if coinitMultiThreaded == coinitApartmentThreaded {
		t.Error("MTA 和 STA 成了同一个值")
	}
}
