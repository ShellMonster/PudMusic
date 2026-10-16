//go:build !darwin && !windows

// Linux（以及别的还没发的平台）上的「收起来 / 叫回来」。
//
// Linux 不首发，这里的实现只保证「能编译、语义说得通」：收起来的窗口由
// 再启动一个实例（单实例锁）唤回来，程序本身不留在那里抢锁以外的地方。
// 真要在 Linux 上发，这一格要重新过一遍：那边没有 Dock 可点，托盘也没有，
// 「收起来」基本等于「用户找不到你了」。
package platform

import "github.com/egoist/mygo"

// HideToBackground 把窗口收起来。应用还在，音乐还在放。
func HideToBackground(w *mygo.Window) {
	if w == nil || w.IsDestroyed() {
		return
	}
	w.Hide()
}

// RestoreFromBackground 把收起来的窗口叫回来并聚焦。
func RestoreFromBackground(w *mygo.Window) {
	if w != nil && !w.IsDestroyed() {
		w.Show()
		w.Focus()
	}
	mygo.App.Focus()
}
