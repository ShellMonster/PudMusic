//go:build !windows || (!amd64 && !arm64)

package mfadec

// 其它平台这一半：**没有**解码器。
//
// Media Foundation 是 Windows 自带的系统组件，别的地方没有对应的东西。所以
// 这一层在 macOS 上只提供「认容器」的能力（`openFrameSource` 是平台无关的，
// 在 macOS 上照样能测），解码那一步一律报「这个平台没有」。
//
// ⚠️ 报的是 `ErrUnsupported` 而不是 `ErrFormat`：前者是「这台机器没这个
// 能力」，后者是「这个文件不对」。上层（`internal/service/audio` 的
// `openDecoder`）靠这个分别决定要不要落回别的解码器——把「没能力」说成
// 「文件坏了」，用户看到的就是一句假话。

// platformAvailable 报「这个平台有没有 Media Foundation」。
func platformAvailable() error { return ErrUnsupported }

// openCodec 开解码器。其它平台上没有解码器可开。
func openCodec(src *frameSource) (aacCodec, error) { return nil, ErrUnsupported }
