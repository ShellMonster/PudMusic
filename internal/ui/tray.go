// 托盘图标（macOS 是菜单栏图标）和它的菜单。
//
// # 菜单项照 Biu 的 `electron/windows/tray.ts` 一条一条对：播放/暂停、上一曲、
// 下一曲、显示/隐藏界面、分隔线、设置、退出程序。少一项就是「菜单项不齐」——
// 用户把窗口收进托盘之后，托盘那几项就是唯一的入口。
//
// # ⚠️ 每一项都走 `runShortcut`（`s.onShortcut`）
//
// 这一份菜单**和应用菜单重复**（都有播放/上一首/下一首）——那是**故意的**：
// Spotify 就是两处都有。区别是：应用菜单那一栏带快捷键、是主窗口的补充；
// 状态栏图标是「点开就有」的快捷方式。
//
// ⇒ 但**动作必须是同一份实现**：菜单项不自己调 `s.app.Toggle()`，而是走
// `s.onShortcut`（`commands.go` 的文件头解释了为什么：菜单的加速键会先于
// 窗口拿到那个键，所以同一个动作不能有两份实现）。
package ui

import (
	"log"
	"runtime"

	"github.com/ShellMonster/PudMusic/internal/platform"
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
//
// ⚠️ **形状是音乐播放器的形状**：最上面先说「现在在放什么」，然后是播放控制，
// 然后是窗口的那些去处，最后是设置和退出。
//
// ⚠️ **「正在播放」那一行是禁用项**：它只是给人看的，而 macOS 上一个禁用项
// 是灰的——正好是「这一行不可点」的样子。
func (s *Shell) trayMenu() *mygo.Menu {
	items := []*mygo.MenuItem{
		// 「正在播放：歌名 - 艺人」。
		//
		// ⚠️ **没有歌的时候说清楚**：留一条空的那一行，用户看到的是「这个应用
		// 什么都没在放」——而那到底是「没歌」还是「菜单坏了」分不清。
		{ID: trayNowPlayingID, Label: s.trayNowPlayingLabel(), Disabled: true},
		mygo.Separator(),
		// ⚠️ **走 `s.onShortcut`**（不是直接调 `s.app.Toggle()`）——理由见
		// 文件头。
		{ID: trayCommandIDPrefix + "togglePlay", Label: "播放/暂停", Click: func(*mygo.MenuItem, *mygo.Window) {
			s.onShortcut("togglePlay")
		}},
		{ID: trayCommandIDPrefix + "prev", Label: "上一曲", Click: func(*mygo.MenuItem, *mygo.Window) {
			s.onShortcut("prev")
		}},
		{ID: trayCommandIDPrefix + "next", Label: "下一曲", Click: func(*mygo.MenuItem, *mygo.Window) {
			s.onShortcut("next")
		}},
		mygo.Separator(),
		{ID: trayToggleWindowID, Label: "显示/隐藏界面", Click: func(*mygo.MenuItem, *mygo.Window) {
			s.toggleWindow()
		}},
		{ID: trayOpenWindowID, Label: "打开主窗口", Click: func(*mygo.MenuItem, *mygo.Window) {
			s.openWindow()
		}},
		{ID: trayMiniID, Label: "迷你播放器", Click: func(*mygo.MenuItem, *mygo.Window) {
			s.toggleMiniPlayer()
		}},
		mygo.Separator(),
		{ID: traySettingsID, Label: "设置", Click: func(*mygo.MenuItem, *mygo.Window) {
			s.openWindow()
			s.navigate("/settings")
		}},
		{ID: trayQuitID, Label: "退出程序", Click: func(*mygo.MenuItem, *mygo.Window) {
			// 走 App.Quit 而不是直接退：退出流程要经过 OnBeforeQuit → 挨个关窗口
			// → OnWillQuit 存设置。绕过它，用户刚改的设置就没了。
			mygo.App.Quit()
		}},
	}
	return mygo.NewMenu(items)
}

// trayNowPlayingLabel 是状态栏菜单最上面那一行：「正在播放：歌名 - 艺人」。
//
// ⚠️ **没有歌的时候说清楚**（见 `trayMenu` 里那一项的注释）。
//
// ⚠️ **艺人没有就只说歌名**：本地文件没有艺人信息（`app.State.Artist` 的
// 注释），而「歌名 - 」那一串尾巴上的破折号是给用户看的噪音。
func (s *Shell) trayNowPlayingLabel() string {
	st := s.app.Snapshot()
	if !st.HasTrack {
		return "没有在放"
	}
	title := st.Title
	if title == "" {
		title = "未知曲目"
	}
	if st.Artist == "" {
		return "正在播放：" + title
	}
	return "正在播放：" + title + " - " + st.Artist
}

// 状态栏菜单项的 id。
const (
	// trayNowPlayingID 是「正在播放」那一行。
	trayNowPlayingID = "tray.nowPlaying"
	// trayCommandIDPrefix 是「一条命令」那一项 id 的前缀。
	trayCommandIDPrefix = "tray.cmd."
	// trayToggleWindowID 是「显示/隐藏界面」那一项。
	trayToggleWindowID = "tray.toggleWindow"
	// trayOpenWindowID 是「打开主窗口」那一项。
	trayOpenWindowID = "tray.openWindow"
	// trayMiniID 是「迷你播放器」那一项。
	trayMiniID = "tray.mini"
	// traySettingsID 是「设置」那一项。
	traySettingsID = "tray.settings"
	// trayQuitID 是「退出程序」那一项。
	trayQuitID = "tray.quit"
)

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
