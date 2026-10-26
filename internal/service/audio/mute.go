// 测试之下把播放器的最后一级换成哑的：`go test` 跑起来的那些 Play() 不进扬声器。
//
// # 为什么要有这一级
//
// macOS 的输出是框架的 Audio Queue，接的是**默认输出设备**——扬声器（
// `mygo/audio/output_darwin.go` 的文件头写着这件事）。而测试里有一批**真的会
// `Play()` 的用例**：淡入 / 音量那一族放的是假解码器的**台阶波**（每读一段换
// 一个幅度，谱上是密密的一片跳变，又长又刺），app / 界面那一族的播放链路用例
// 会把 flacdec 的**测试音**真解真放。每跑一次 `go test`，喇叭就把这些过一遍
// ——「测试的时候喇叭嘟嘟嘟嘟响很久」就是它们。
//
// 框架有一个「测试模式」（`audio.output.go` 的 `testMode`：开了就不碰设备），
// 但 v0.6.3 **没有导出它**，包外打不开。所以在这里自己包：`testing.Testing()`
// 在 `go test` 之下为真，把最后一级换成 `muteDecoder`——采样全零，设备照开
// （省得改播放器的状态机），出声没有。
//
// 🔴 **哑在最末一级，别处不许哑**：tap（抽样的那一份）、增益曲线、播放位置
// 全都住在这一级的**上游**——各族测试断言的东西一个采样都不变。别为了「更省」
// 把哑挪到解码器上：tap 断言的是解码器给的样本，哑在那里整族全红。
package audio

import (
	"io"
	"testing"

	mygoaudio "github.com/egoist/mygo/audio"
)

// New 之上包的那一层：测试之下返回哑的最后一级。
func maybeMuted(src mygoaudio.Decoder) mygoaudio.Decoder {
	if testing.Testing() {
		return muteDecoder{src}
	}
	return src
}

// muteDecoder 是「采样全零」的那一级：Read 照常推进上游的位置、照常透传
// io.EOF 和错误，只是把读到的样本抹成 0。其余方法（采样率 / 声道 / 长度 /
// 跳位置）原样转发。
//
// 🔴 **Close 必须转发**：MyGo 收尾解码链靠的是对它拿到的那个 Decoder 做
// `io.Closer` 断言（`mygo/audio/source.go` 的 decode goroutine 退出时）——
// 不转发的话整条关资源的链条在这里断掉，extaf 的 AudioToolbox 句柄一个都
// 放不掉（`handles_test.go` 那两条当场红）。varispeed 那一层因为同一件事
// 写过同一句注释（`rate.go` 的 `Close`）。
type muteDecoder struct {
	mygoaudio.Decoder
}

func (m muteDecoder) Read(buf []float32) (int, error) {
	n, err := m.Decoder.Read(buf)
	clear(buf[:n])
	return n, err
}

func (m muteDecoder) Close() error {
	if c, ok := m.Decoder.(io.Closer); ok {
		return c.Close()
	}
	return nil
}
