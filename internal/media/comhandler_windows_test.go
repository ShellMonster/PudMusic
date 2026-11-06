//go:build windows && (amd64 || arm64)

package media

import (
	"testing"
	"unsafe"
)

// guidPtr / uintptrPtr 是测试把局部变量的地址当 `uintptr` 递给小帮手。
// `genericQI` 的 `riid` 是 `REFIID`（一个 GUID 指针）、`out` 是 `void**`。
func guidPtr(g *guid) uintptr       { return uintptr(unsafe.Pointer(g)) }
func uintptrPtr(p *uintptr) uintptr { return uintptr(unsafe.Pointer(p)) }

// TestHandlerQIRefusesWhatTheSystemAsksForAtAddTime 钉住「`add_ButtonPressed`
// 期间系统会同步 QI 我们的 handler 问 `IWeakReferenceSource`，而它必须被拒」。
//
// ⚠️ 这是 Windows 真机 `Exception 0xc0000005` 的根：`genericQI` 曾经
// 「除 IMarshal / INoMarshal 外全认」，把 `IWeakReferenceSource` 也答成
// S_OK、递回 this——系统拿虚表第 4 格当 `GetWeakReference` 调，那一格是
// `genericInvoke`（不是 `GetWeakReference`），它的 `out` 槽被当返回值指针
// 解引用，一解引用就崩在 `comCall` 里。
//
// 生产上生效的是 `genericQI` → `handlerAccepts` → `smtcHandlerIIDs` 这一条
// 链，所以这条测的是**同一份白名单**：断「认的全在名单里、名单里
// 没有的一律拒」。
func TestHandlerQIRefusesWhatTheSystemAsksForAtAddTime(t *testing.T) {
	h := newHandler(func(_, _ uintptr) {})
	t.Cleanup(func() { release(h) })

	// 白名单里的每一个都该拿到 S_OK。
	for _, id := range smtcHandlerIIDs {
		id := id
		var out uintptr
		if hr := genericQI(h, guidPtr(&id), uintptrPtr(&out)); hr != sOK {
			t.Errorf("白名单里的 IID %#x 被拒了（HRESULT %#08x）：系统那边会拿不到 handler",
				id, uint32(hr))
		}
	}

	// `IWeakReferenceSource` / IMarshal / INoMarshal / 一个陌生 IID 一律拒——
	// 这些是「系统拿我们的 Invoke 虚表去当别的接口用」的那几张。
	for _, id := range []guid{
		iidWeakReferenceSource,
		iidIMarshal,
		iidINoMarshal,
		mustGUID("11111111-2222-3333-4444-555555555555"),
	} {
		id := id
		var out uintptr
		if hr := genericQI(h, guidPtr(&id), uintptrPtr(&out)); hr != uintptr(hrNoInterface) {
			t.Errorf("该拒的 IID %#x 拿到了 %#08x：系统会把它当别的接口虚调",
				id, uint32(hr))
		}
		if out != 0 {
			t.Errorf("该拒的 IID %#x 递回了非空指针 %#x", id, out)
		}
	}
}

// TestHandlerInvokeCannotPropagateAPanic 钉住「handler 里的 panic 不穿过
// COM 回调边界」。
//
// `syscall.NewCallback` 的边界上 panic 不是 Go panic——落进 runtime 的
// callback 封装里是 `throw`，整个进程当场死。`genericInvoke` 必须把它拦
// 下来，这才是「回调里一个错 ≠ 应用崩」的那一道闸。
func TestHandlerInvokeCannotPropagateAPanic(t *testing.T) {
	h := newHandler(func(_, _ uintptr) { panic("handler 里炸了") })
	t.Cleanup(func() { release(h) })

	// 跑下去就是 panic 没被吞——测试自己也过不了 recover，能到这就够。
	genericInvoke(h, 0, 0, 0)
}
