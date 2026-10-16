//go:build darwin

// macOS 上的「收起来 / 叫回来」。
//
// macOS **不做托盘**，与 Biu 一致：收起来是「窗口不见了，Dock 图标还在」，
// 点一下图标就回来。Biu 在 macOS 上也是这么干的（`electron/main.ts:119`
// 只调 `mainWindow.hide()`，托盘那段整个包在 `platform !== "darwin"` 里）。
//
// 顺带处理 ⌘H：那是「隐藏整个应用」而不是「隐藏窗口」，两者的回来方式不一样
// （见 RestoreFromBackground）。用户按了 ⌘H 之后，光把窗口 Show 出来是没有
// 用的——NSApp 还藏在那儿，得先 unhide。
package platform

import "github.com/egoist/mygo"

// HideToBackground 把窗口收起来。应用还在，音乐还在放。
func HideToBackground(w *mygo.Window) {
	if w == nil || w.IsDestroyed() {
		return
	}
	w.Hide()
}

// RestoreFromBackground 把收起来的窗口叫回来，并把它放到前台。
//
// 三步的顺序不能换：先 unhide 整个应用（用户可能按过 ⌘H），再 Show 窗口
// （窗口对藏着的应用 orderFront 是没用的），最后 Focus。
// 已经关掉的窗口直接跳过：拿一个销毁了的窗口调原生方法会在 Cocoa 那边炸。
func RestoreFromBackground(w *mygo.Window) {
	if mygo.App.IsHidden() {
		mygo.App.Show()
	}
	if w != nil && !w.IsDestroyed() {
		w.Show()
	}
	mygo.App.Focus()
}
