//go:build windows

// Windows 上的「收起来 / 叫回来」。
//
// Biu 在 Windows 上是收进托盘的，托盘那一块不在本期范围内（见 issue #28 的
// 说明：macOS 不创建托盘，也不要顺手加一个）。所以这里收的是**任务栏按钮**：
// `App.Hide()` 会把当前可见的窗口全藏起来并记在 `b.hidden` 里，任务栏上
// 于是什么都不剩——和 Biu 收进托盘之后的样子一致，只是回来的入口不一样：
// 托盘图标这时还没有，唤起靠再启动一个实例（单实例锁会把请求转给这个进程）。
//
// `RestoreFromBackground` 里那两步必须都有：`App.Hide` 记下的窗口是用
// `ShowInactive`（不着焦）叫回来的，只调 `App.Show` 的话窗口回来了却拿不到
// 焦点，用户还得再点一下。
package platform

import "github.com/egoist/mygo"

// HideToBackground 把窗口收进后台：窗口看不见，任务栏按钮也一起收走。
func HideToBackground(w *mygo.Window) {
	if w == nil || w.IsDestroyed() {
		return
	}
	w.Hide()
	mygo.App.Hide()
}

// RestoreFromBackground 把收进后台的窗口叫回来并聚焦。
func RestoreFromBackground(w *mygo.Window) {
	mygo.App.Show()
	if w != nil && !w.IsDestroyed() {
		w.Show()
		w.Focus()
	}
	mygo.App.Focus()
}
