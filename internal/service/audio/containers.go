package audio

import (
	"io"

	"github.com/ShellMonster/Biu_Go/internal/service/audio/extaf"
	"github.com/ShellMonster/Biu_Go/internal/service/audio/mfadec"

	mygoaudio "github.com/egoist/mygo/audio"
)

// 交给 macOS 系统解码器的容器。
//
// 走 MyGo 的格式表，而不是在 Open 里加一段扩展名 switch：AAC、ALAC、MP4 里的
// FLAC 全是同一条 ExtAudioFile 链路，按文件头认容器比按扩展名可靠——Biu 下载
// 下来的音频有的叫 .m4a 有的叫 .m4s，内容却一样。
//
// 这张表是**本地文件和在线流共用**的：`player.go` 的 `openDecoder` 是唯一认
// 容器的地方，它先查这张表，认不出来再试 flacdec。两条路调的是同一个函数。
//
// MyGo 的表只能对一个字面量做匹配，所以有几个容器形状就注册几条：
//
//   - ftyp 开头：正常封装的 MP4 家族（.m4a / .mp4）
//   - styp 开头：分片流（DASH 那种 .m4s）
//   - 0xFF 0xFx：裸 ADTS，也就是很多人说的「.aac」——没有容器，头两字节就是
//     同步字。MyGo 的通配符只能整字节，没法表达「0xF0~0xFF」，所以这里写成
//     "\xff??"（首字节 0xFF，后两字节不管），比精确匹配松，认错了会在 Open
//     里明确报错，不会解出坏数据。
//
// ⚠️ 裸 ADTS 能解这件事是本线程实测出来的，和 `verify-streaming.md` §1.1 的
// 结论相反：那份报告测的是 ExtAudioFileOpenURL，那条路确实打不开；换成
// AudioFileOpenWithCallbacks + WrapAudioFileID 之后同一个文件能正常解码。
func init() {
	mygoaudio.RegisterFormat("mp4", "????ftyp", openExtAudioFile)
	mygoaudio.RegisterFormat("mp4", "????styp", openExtAudioFile)
	mygoaudio.RegisterFormat("aac", "\xff??", openExtAudioFile)
}

// platformDecoderOpen 是「平台解码器」的入口。
//
// 生产代码里它就是 `extaf.Open`。把它做成变量只有一个理由：**测试要模拟
// 「这个平台没有系统解码器」**（Windows 的样子）——`extaf.Open` 在 macOS 上
// 永远成功，于是「平台不可用 → 落回 flacdec」那条路径在 macOS 上**一条都
// 测不到**，而 macOS 恰恰是本仓库唯一能跑测试的平台。
//
// ⚠️ 它是**包级变量**：换掉它的那条测试必须自己串行（不要 `t.Parallel`），
// 同包其它测试会看见换掉之后的值。
var platformDecoderOpen = openWithPlatformDecoder

// openWithPlatformDecoder 用平台自带的解码器打开 r。
//
// `extaf.Open` 在两个平台上的返回类型不一样（macOS 是 `*extaf.Decoder`，其它
// 平台是 `mygoaudio.Decoder`），这里统一成接口再交给上面那个变量。
func openWithPlatformDecoder(r io.ReadSeeker) (mygoaudio.Decoder, error) {
	d, err := extaf.Open(r)
	if err != nil {
		return nil, err
	}
	return d, nil
}

// openExtAudioFile 是注册进 MyGo 格式表的那个入口。
//
// 它转手给 `platformDecoderOpen`（而不是自己调 `extaf.Open`），这样测试换掉
// 平台解码器时，注册表这一条路也跟着换——落回逻辑断的正是这里。
func openExtAudioFile(r io.ReadSeeker) (mygoaudio.Decoder, error) {
	return platformDecoderOpen(r)
}

// mediaFoundationDecoderOpen 是「Windows 自带的解码器」的入口。
//
// 生产代码里它就是 `mfadec.Open`（Media Foundation 的 AAC MFT）。做成变量
// 只有一个理由：**测试要模拟「这台机器没有 Media Foundation」**——真 Windows
// 上它恒可用，macOS 上恒不可用，不注入的话这条路径在唯一能跑测试的这个平台上
// **一条都测不到**（和 `platformDecoderOpen` 同一个道理）。
//
// ⚠️ 它是**包级变量**：换掉它的那条测试必须自己串行（不要 `t.Parallel`），
// 同包其它测试会看见换掉之后的值。
var mediaFoundationDecoderOpen = openWithMediaFoundation

// openWithMediaFoundation 用 Windows 的 Media Foundation 打开 r。
//
// 它解两种容器：MP4 家族（`.m4a` / `.m4s`，B 站有损流那种）和裸 ADTS
// （`.aac`）。别的平台上没有 Media Foundation，`mfadec.Open` 会返回
// `mfadec.ErrUnsupported`——那一层是平台无关的，所以这一行在 macOS 上也
// 编得过、也跑得起来（只是永远走不到成功那一支）。
func openWithMediaFoundation(r io.ReadSeeker) (mygoaudio.Decoder, error) {
	d, err := mfadec.Open(r)
	if err != nil {
		return nil, err
	}
	return d, nil
}
