package audio

import (
	"io"

	"github.com/ShellMonster/Biu_Go/internal/service/audio/extaf"

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

func openExtAudioFile(r io.ReadSeeker) (mygoaudio.Decoder, error) {
	d, err := extaf.Open(r)
	if err != nil {
		return nil, err
	}
	return d, nil
}
