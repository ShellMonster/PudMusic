// 设置页那两个「选择」按钮：弹系统文件框挑一个路径，挑回来写进设置。
//
// 做成 `pathPicker` 一个字段，是为了能替掉它——真的那一份要过事件循环
// （MyGo 把弹框请求 post 到主线程再等系统回调），无头 Tester 里没有事件循环，
// 不替掉就只能等上真系统才验得到「选回来写进设置」这条链。和 `autoStartService`
// 是同一个道理，理由见 lifecycle.go 的文件头。
//
// ⚠️ **必须在另一个 goroutine 里调**：主线程进去等就是死等，事件循环卡死。和
// `pickAndEnqueue` 一样（见 playbar.go）。
package ui

import (
	"log"

	"github.com/ShellMonster/PudMusic/internal/platform"
	"github.com/egoist/mygo"
)

// pathPicker 是「挑一条路径」这件事。挑回来的是空串就是用户取消了。
type pathPicker interface {
	pick(parent *mygo.Window, dir bool, title, message string) (string, error)
	// pickConfig 是「挑一份配置文件」（设置页的「导入配置」）。
	pickConfig(parent *mygo.Window) (string, error)
	// saveConfig 是「选一个导出到哪」。
	saveConfig(parent *mygo.Window, defaultName string) (string, error)
}

// systemPathPicker 是真的那一份，也就是 internal/platform 的 `PickPath`。
type systemPathPicker struct{}

func (systemPathPicker) pick(parent *mygo.Window, dir bool, title, message string) (string, error) {
	return platform.PickPath(parent, dir, title, message)
}

func (systemPathPicker) pickConfig(parent *mygo.Window) (string, error) {
	return platform.PickConfigFile(parent)
}

func (systemPathPicker) saveConfig(parent *mygo.Window, defaultName string) (string, error) {
	return platform.SaveConfigAs(parent, defaultName)
}

// pathPrompt 是这一处要问用户的那两行字。
//
// ⚠️ 每一个按钮给**自己**的那一份，不共用一句：下载目录问的是「下载的东西存到哪儿」，
// 本地音乐目录问的是「音乐从哪儿来」，FFmpeg 问的是「那个可执行文件在哪儿」。
// 共用一句就有一边在说错的话，而错的那句是直接弹在系统对话框上的。
var pathPrompt = map[string][2]string{
	"下载目录":      {"选择文件夹", "选一个用来保存下载内容的位置"},
	"FFmpeg 路径": {"选择文件", "选一个 FFmpeg 可执行文件"},
	"本地音乐目录":    {"选择音乐文件夹", "选一个文件夹，它的音频文件会出现在本地音乐里"},
}

// pickPath 弹文件框，把挑回来的路径交给 apply。
//
// ⚠️ 结果要**逐窗口**叫醒：`ui.Services.Invalidate()` 只对当前那个窗口句柄有效
// （wake.go 的 wakeHandle），而这一份改动是全局的设置。迷你播放器窗口如果停在
// 旧帧上，用户看到的还是旧设置。（铁律三。）
func (s *Shell) pickPath(what string, dir bool, apply func(string)) {
	parent := s.win
	picker := s.picker
	title, message := pathPrompt[what][0], pathPrompt[what][1]
	go func() {
		path, err := picker.pick(parent, dir, title, message)
		if err != nil {
			log.Printf("biu: 选%s失败：%v", what, err)
		}
		if path == "" {
			return // 用户取消：什么都不改，也什么都不说
		}
		apply(path)
		s.wake.invalidate()
	}()
}
