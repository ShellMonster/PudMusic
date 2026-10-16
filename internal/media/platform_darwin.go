//go:build darwin

package media

import "errors"

// macOS 那一半还没接上时的占位。
//
// 真身是 `mp_darwin.go`（随后的提交补上）：`newPlatform` 在那里返回的是
// `MPNowPlayingInfoCenter` + `MPRemoteCommandCenter`。
//
// 这一版存在的意义是让「平台无关的那一半」先能独立编译、独立测试：Session
// 的去抖规则、命令排队、状态与命令的形状，全都不需要 Objective-C 就断得了。

// errNoDarwinYet 是「macOS 那一半还没接上」。
var errNoDarwinYet = errors.New("media: macOS 那一半还没接上")

func newPlatform(Options) (platform, error) { return nil, errNoDarwinYet }
