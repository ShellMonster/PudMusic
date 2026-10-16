//go:build (!darwin && !windows) || (windows && !amd64 && !arm64)

package media

// macOS 和 Windows 之外的平台：一个什么都不做的平台实现。
//
// Windows 那一半在 smtc_windows.go（SMTC + 任务栏缩略图按钮），macOS 那一半
// 在 mp_darwin.go。**两个平台各占一个文件、互不引用**——它们各自要查的系统
// 头文件没有一份是共通的，而混在一起的结果就是「改 macOS 的代码去修 Windows
// 的 bug」这种谁也查不出来的错。
//
// 而这一层此刻是安全的：会话在这些平台上就是一个哑巴，`Publish` 什么都不做、
// `SetHandler` 不会被调用，界面和播放一点都察觉不到。

import "time"

// newPlatform 返回一个什么都不做的平台实现。
//
// 返回错误而不是 nil：调用方（Session）会把这件事记成一条日志，
// 「这一轮系统媒体会话不可用」比「装上了但什么都不发生」好查。
func newPlatform(Options) (platform, error) {
	return noPlatform{}, errUnsupported
}

// errUnsupported 是「这个平台上还没有媒体会话」。
var errUnsupported = errPlatform("这个平台上还没有系统媒体会话")

// noPlatform 是一个什么都不做的平台。
type noPlatform struct{}

func (noPlatform) apply(NowPlaying) {}

func (noPlatform) attach(func(Command, time.Duration)) {}

func (noPlatform) detach() {}
