// 托盘图标（macOS 是菜单栏图标）和它的菜单。
//
// 菜单项照 Biu 的 `electron/windows/tray.ts` 一条一条对：播放/暂停、上一曲、
// 下一曲、显示/隐藏界面、分隔线、设置、退出程序。少一项就是「菜单项不齐」——
// 用户把窗口收进托盘之后，托盘那几项就是唯一的入口。
package ui

import (
	"log"
	"runtime"

	"github.com/ShellMonster/Biu_Go/internal/platform"
	"github.com/egoist/mygo"
)

// installTray 建托盘。**要在 mygo.App.WhenReady 之后调**。
//
// 建不出来只记日志不停下来：托盘是这个平台的「隐藏之后怎么回来」，但它不是
// 「能不能起来」。因为一个图标建不出来就把整个应用卡在起不来的状态，是本末倒置。
func (s *Shell) installTray() {
	icon, template := platform.TrayIcon()
	tray, err := platform.NewTray(icon, template, s.trayMenu())
	if err != nil {
		log.Printf("biu: 托盘图标建不出来，隐藏之后只能再启动一个应用唤回来：%v", err)
		return
	}
	s.tray = tray
	// 左键点图标 = 显示/隐藏，和 Biu 一样（`tray.ts` 的 `tray.on("click")`）。
	// macOS 上点图标直接弹菜单，菜单里有「显示/隐藏界面」，不另挂。
	if platform.TraySupported() && runtime.GOOS != "darwin" {
		tray.OnClick(func() { s.toggleWindow() })
	}
}

// trayMenu 是托盘右键（Windows 上是左键）弹出的菜单。
func (s *Shell) trayMenu() *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: "播放/暂停", Click: func(*mygo.MenuItem, *mygo.Window) { s.app.Toggle() }},
		{Label: "上一曲", Click: func(*mygo.MenuItem, *mygo.Window) { s.app.Prev() }},
		{Label: "下一曲", Click: func(*mygo.MenuItem, *mygo.Window) { s.app.Next() }},
		{Label: "显示/隐藏界面", Click: func(*mygo.MenuItem, *mygo.Window) { s.toggleWindow() }},
		mygo.Separator(),
		{Label: "设置", Click: func(*mygo.MenuItem, *mygo.Window) {
			s.openWindow()
			s.navigate("/settings")
		}},
		{Label: "退出程序", Click: func(*mygo.MenuItem, *mygo.Window) {
			// 走 App.Quit 而不是直接退：退出流程要经过 OnBeforeQuit → 挨个关窗口
			// → OnWillQuit 存设置。绕过它，用户刚改的设置就没了。
			mygo.App.Quit()
		}},
	})
}

// toggleWindow 是「显示 / 隐藏」那个来回切。
//
// 窗口不在了（已经真关掉了）就当显示处理：`openWindow` 会重新开一个。
// 判断用 `IsVisible` 而不是「进程在不在」——托盘存在的意义就是窗口可以不在，
// 但应用一直在。
func (s *Shell) toggleWindow() {
	if s.win == nil || s.win.IsDestroyed() {
		s.openWindow()
		return
	}
	if s.win.IsVisible() {
		platform.HideToBackground(s.win)
		return
	}
	platform.RestoreFromBackground(s.win)
}
