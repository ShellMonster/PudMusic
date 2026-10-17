package localmusic

import (
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/audio"
)

// readHeadOrNil 是 `readHead` 的容错版：读不出来给 nil，让调用方走「没有标签」。
//
// 标签读不出来**不是错误**：那只是这一首退回文件名显示。不该让一个读不动的
// 文件把整次扫描记一笔。
func readHeadOrNil(path string) []byte {
	b, err := readHead(path)
	if err != nil {
		return nil
	}
	return b
}

// ProbeDuration 从文件自己的容器里读时长。读不出来给 0。
//
// 实现放在 `audio.ProbeDuration`，不在这里：认容器的那张注册表是
// `internal/service/audio` 的（`openDecoder`），再抄一份就会漂移——漂移的那
// 一半正好是「扫出来说能放，但扫不出时长」。
//
// 这里的壳只做两件事：把错误吞成零值，以及说清**这是容器时长不是标签时长**
// （见 ProbeDuration 那边的注释）。
func ProbeDuration(path string) time.Duration {
	d, err := audio.ProbeDuration(path)
	if err != nil {
		// 打不出来是常事（不支持的容器、坏文件、变长码率的 MP3），不记日志：
		// 一次扫描几千个文件，日志会被刷爆，而界面上那一栏画「-」就够了。
		return 0
	}
	return d
}
