//go:build !darwin

package ui

import "github.com/egoist/mygo"

// 非 macOS：刘海窗是 macOS 独有的交互（刘海只长在 MacBook 上）。这层是给
// `notch.go` 里那几个「真窗才有的动作」准备的空实现——无头测试和别的平台
// 都不会走到它们。

type notchNSPoint struct{ X, Y float64 }

func notchConfigure(w *mygo.Window)  {}
func notchReposition(w *mygo.Window) {}
func notchStartWatch(s *Shell)       {}
func notchStopWatch()                {}
