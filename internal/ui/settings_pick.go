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

	"github.com/ShellMonster/Biu_Go/internal/platform"
	"github.com/egoist/mygo"
)

// pathPicker 是「挑一条路径」这件事。挑回来的是空串就是用户取消了。
type pathPicker interface {
	pick(parent *mygo.Window, dir bool) (string, error)
}

// systemPathPicker 是真的那一份，也就是 internal/platform 那两个函数。
type systemPathPicker struct{}

func (systemPathPicker) pick(parent *mygo.Window, dir bool) (string, error) {
	return platform.PickPath(parent, dir)
}

// pickPath 弹文件框，把挑回来的路径交给 apply。
//
// ⚠️ 结果要**逐窗口**叫醒：`ui.Services.Invalidate()` 只对当前那个窗口句柄有效
// （wake.go 的 wakeHandle），而这一份改动是全局的设置。迷你播放器窗口如果停在
// 旧帧上，用户看到的还是旧设置。（铁律三。）
func (s *Shell) pickPath(what string, dir bool, apply func(string)) {
	parent := s.win
	picker := s.picker
	go func() {
		path, err := picker.pick(parent, dir)
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
