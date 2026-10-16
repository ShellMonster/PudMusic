//go:build !darwin

package media

// 非 macOS 的那一半：一个什么都不做的平台实现。
//
// 不是「以后再说」的空壳——**Windows 的 SMTC 和 Linux 的 MPRIS 走的是同一
// 个 `platform` 接口**（写 info、接命令），加上它们只是这个文件之外的事。
// 而这一层此刻是安全的：会话在那两个平台上就是一个哑巴，`Publish` 什么都不
// 做、`SetHandler` 不会被调用，界面和播放一点都察觉不到。

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

// errPlatform 是「平台没做」这一类错误。
type errPlatform string

func (e errPlatform) Error() string { return "media: " + string(e) }

// noPlatform 是一个什么都不做的平台。
type noPlatform struct{}

func (noPlatform) apply(NowPlaying) {}

func (noPlatform) attach(func(Command, time.Duration)) {}

func (noPlatform) detach() {}
