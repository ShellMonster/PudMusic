// 把系统媒体会话挂到 App 上：控制中心 / 锁屏 / 任务栏缩略图上的「正在播放」，
// 以及从那里发回来的播放命令。
//
// 跟 `MountLogin` / `MountStreaming` 是同一个形状的一件事：**在开窗口之前**
// 把零件递过去。理由也是同一个，但这一条还多一层——`media.New()` 在 macOS 上
// 会在那里做 Objective-C 的类注册和方法注册，而**注册必须在还没有 ObjC 回调
// 在跑的时候做**（有回调在跑的时候再注册直接 SIGTRAP）。窗口一开，回调就有了。
//
// Windows 那一半反过来要**等窗口**（它的媒体会话是按窗口算的），所以这里
// 只建会话，绑定窗口那一下由 `Shell.bindMedia` 在窗口出来之后做。
package ui

import (
	"log"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/media"
	"github.com/egoist/mygo"
)

// MountMedia 装好系统媒体会话。**必须在开窗口之前调**。
//
// 返回那个会话：Windows 那边要等窗口建出来之后用它把系统接上
// （见 `Shell.bindMedia`）。
//
// 它做两件事，都是一次性的：
//
//	App 的状态 → 会话   （换歌、暂停、跳位置时推给系统）
//	会话 → App 的动作   （用户按了浮窗上的播放/上一首时回调回来）
//
// 这两件事的**内容**都在 `internal/media` 和 `internal/app` 里，这里只递接线。
// 所以在 macOS 上按一下控制中心的暂停，链路是：系统 → media 的 ObjC 方法 →
// 工作协程 → `App.MediaCommand` → `App.Pause` → 界面那一帧。
//
// 系统媒体会话装不上不算失败：记一条日志继续走，界面、播放、队列都跟它无关。
func MountMedia(a *app.App) *media.Session {
	sess := media.New(media.Options{})
	if !sess.Available() {
		log.Print("biu: 这一轮没有系统媒体会话，控制中心和锁屏上看不到歌")
		return sess
	}
	a.SetMediaSink(sess.Publish)
	sess.SetHandler(a.MediaCommand)
	// 退出时把系统那边收干净。App.Close 也报一次「没有歌了」，两次都发
	// 的是同一个状态，不冲突——而这条兜的是「进程直接没了」的那一种。
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) { sess.Close() })
	return sess
}
