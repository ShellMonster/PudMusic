//go:build !darwin

package ui

import "github.com/egoist/mygo"

// 非 macOS：刘海窗是 macOS 独有的交互。这层给 `notch.go` 里那几个
// 「真窗才有的动作」备的空实现。

type notchNSPoint struct{ X, Y float64 }

func notchSize() (w, h float64, ok bool) { return 0, 0, false }
func menuBarHeight() float64             { return 0 }
func notchConfigure(w *mygo.Window)      {}
func notchReposition(w *mygo.Window)     {}
func notchStartWatch(s *Shell)           {}
func notchStopWatch(s *Shell)            {}
