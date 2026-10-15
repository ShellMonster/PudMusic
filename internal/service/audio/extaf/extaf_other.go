//go:build !darwin

package extaf

import (
	"io"

	mygoaudio "github.com/egoist/mygo/audio"
)

// LiveSources 报告还有几个字节源挂着。这个平台上永远是 0——没有字节源。
func LiveSources() int { return 0 }

// Open 在没有系统解码器的平台上永远失败。
//
// Windows 的路线是另一条（MediaFoundation），还没有实现，所以这里明确报
// 「这个平台没有能力」，而不是把文件悄悄解成静音。返回类型和 macOS 上一致，
// 调用方不需要分平台写两遍。
func Open(io.ReadSeeker) (mygoaudio.Decoder, error) { return nil, ErrUnsupported }
