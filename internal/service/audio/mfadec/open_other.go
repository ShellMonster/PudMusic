//go:build (!windows || (!amd64 && !arm64)) && (!android || !cgo)

package mfadec

// 其它平台这一半：**没有**解码器。
//
// Windows 和开启 cgo 的 Android 分别有自己的系统解码器。这里在其余构建中
// 只提供平台无关的拆帧能力。
//
// ⚠️ 报的是 `ErrUnsupported` 而不是 `ErrFormat`：前者是「这台机器没这个
// 能力」，后者是「这个文件不对」。上层（`internal/service/audio` 的
// `openDecoder`）靠这个分别决定要不要落回别的解码器——把「没能力」说成
// 「文件坏了」，用户看到的就是一句假话。

// platformAvailable 报「这个构建有没有 AAC 后端」。
func platformAvailable() error { return ErrUnsupported }

const codecKeepsPriming = false

// openCodec 开解码器。其它平台上没有解码器可开。
func openCodec(src *frameSource) (aacCodec, error) { return nil, ErrUnsupported }
