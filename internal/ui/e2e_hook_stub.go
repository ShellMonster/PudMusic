//go:build !e2e

package ui

// e2eRegisterShell 的零成本替身：不带 `e2e` 标签时什么也不是（见
// `e2e_hook.go`——那条缝只对带 `-tags e2e` 编出来的测试二进制存在）。
func e2eRegisterShell(*Shell) {}

// e2eNoteLaunchArgs 的替身：不带 e2e 标签时不记任何事。
func e2eNoteLaunchArgs([]string) {}
