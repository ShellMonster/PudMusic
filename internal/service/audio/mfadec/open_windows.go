//go:build windows && (amd64 || arm64)

package mfadec

// Windows 这一半：解码器是 Media Foundation 的 AAC MFT。
//
// 这里是**唯一**碰平台的地方。`decoder.go` 那一层只看得见 `aacCodec` 这
// 五个方法，所以它能在没有 Windows 的机器上编译、也能用假的解码器测。

// platformAvailable 报「这个平台有没有 Media Foundation」。
//
// Windows 上有——但只在 amd64 和 arm64 上：本包的 COM 绑定是手写的
// `syscall.SyscallN`（`mf_windows.go`），没有 386 的那一份，所以其它架构
// 一律当「没有」。
func platformAvailable() error { return nil }

// openCodec 开一个 Media Foundation 的 AAC 解码器。
//
// 它试几种输入配置（`MF_MT_USER_DATA` 的两种形状、输入类型先给还是后给），
// 直到 MFT 认下一种——`transform_windows.go` 的 `tryOpen` 记着每一种的
// 答案。
func openCodec(src *frameSource) (aacCodec, error) {
	c, err := OpenAACCodec(src.rate, src.chans, src.asc, src.payloadType)
	if err != nil {
		return nil, err
	}
	return c, nil
}
