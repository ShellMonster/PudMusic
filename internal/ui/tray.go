// 托盘图标（macOS 是菜单栏图标）和它的菜单。
//
// # 菜单项对着 Biu 的 `electron/windows/tray.ts` 一条一条对
//
// Biu 那份是：播放/暂停、上一曲、下一曲、显示/隐藏界面、分隔线、设置、
// 退出程序。**少一项就是「菜单项不齐」**——用户把窗口收进托盘之后，托盘
// 那几项就是唯一的入口。
//
// ⚠️ **两处和上游不一样，都是有意的**：
//
//  1. **多了最上面那一行「正在播放：歌名 - 艺人」**——上游没有。它是这一栏
//     存在的理由（用户点开菜单第一眼要看的就是「现在在放什么」）。
//  2. **「显示/隐藏界面」写成「显示/隐藏主窗口」**——上游那个词是 Windows
//     上的说法（「界面」），而 macOS 上说「窗口」。
//
// ⚠️ **上游 Biu 在 macOS 上压根不建这个图标**
// （`_research/biu/electron/main.ts:161` 是 `if (process.platform !== "darwin")`）——
// 所以这一栏在 macOS 上**没有上游可对齐**，得按 macOS 的形状来。
//
// # ⚠️ 每一项都走 `runShortcut`（`s.onShortcut`）
//
// 这一份菜单**和应用菜单重复**（都有播放/上一首/下一首）——那是**故意的**：
// 应用菜单那一栏带快捷键、是主窗口的补充；状态栏图标是「点开就有」的快捷方式，
// 它的用处是**窗口收起来之后**——而窗口收起来之后，状态栏图标是**唯一**
// 还在的入口（菜单栏那一列还在，但用户不会想到去那儿找）。
//
// ⚠️ **别拿「某家播放器也这样」当理由**：Spotify 的 macOS 桌面版**有没有
// 原生菜单栏**这件事查不到可靠出处（官方 dmg 的下载���址全部 404 / 410，
// 本机也没装），所以**不拿它举例**。能查实的是「窗口收起之后要有个入口」
// 这件事本身——而那是上游 Biu 的托盘菜单在做的事。
//
// ⇒ 但**动作必须是同一份实现**：菜单项不自己调 `s.app.Toggle()`，而是走
// `s.onShortcut`（`commands.go` 的文件头解释了为什么：菜单的加速键会先于
// 窗口拿到那个键，所以同一个动作不能有两份实现）。
package ui

import (
	"log"
	"runtime"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/platform"
	"github.com/egoist/mygo"
)

// installTray 建托盘。**要在 mygo.App.WhenReady 之后调**。
//
// 建不出来只记日志不停下来：托盘是这个平台的「隐藏之后怎么回来」，但它不是
// 「能不能起来」。因为一个图标建不出来就把整个应用卡在起不来的状态，是本末倒置。
func (s *Shell) installTray() {
	icon, template := platform.TrayIcon()
	menu := s.trayMenu()
	tray, err := platform.NewTray(icon, template, menu)
	if err != nil {
		log.Printf("pudmusic: 托盘图标建不出来，隐藏之后只能再启动一个应用唤回来：%v", err)
		return
	}
	s.tray = tray
	// ⚠️ **留着菜单**：它只建一次，而里面「正在播放」那一行要**运行期刷**
	//（见 refreshTrayMenu）——不留着就刷不了。
	s.trayMenuRef = menu
	// 左键点图标 = 显示/隐藏，和 Biu 一样（`tray.ts` 的 `tray.on("click")`）。
	// macOS 上点图标直接弹菜单，菜单里有「显示/隐藏主窗口」，不另挂。
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
//
// ⚠️ **这一份菜单只建一次**（`installTray`），所以「正在播放」那一行**不能靠
// 建菜单时求值**——那样它永远是**启动那一刻**的那首，用户换歌之后菜单顶部还挂
// 着旧歌名。⇒ 那一行由 `refreshTrayMenu` 在运行期刷（见那里）。
func (s *Shell) trayMenu() *mygo.Menu {
	items := []*mygo.MenuItem{
		// 「正在播放：歌名 - 艺人」。
		//
		// ⚠️ **没有歌的时候说清楚**：留一条空的那一行，用户看到的是「这个应用
		// 什么都没在放」——而那到底是「没歌」还是「菜单坏了」分不清。
		{ID: trayNowPlayingID, Label: s.trayNowPlayingLabel(s.app.Snapshot()), Disabled: true},
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
		// ⚠️ **只有「显示/隐藏主窗口」这一项**，不要再加一个「显示主窗口」。
		//
		// ⚠️ **为什么两个会重复**：`openWindow`（`lifecycle.go:173`）在迷你
		// 开着的时候是「把迷你那一扇叫到前面」，而 `toggleWindow` 在迷你开着
		// 的时候是「把主窗口藏起来」——**两项都是「让用户看见点什么」，而用户
		// 看见的那个东西不一样**（一个是迷你，一个是主窗口）。
		//
		// ⇒ 所以**一项就够**：菜单项写「显示/隐藏主窗口」，点了它就在
		// 「把主窗口叫回来」和「把主窗口藏起来」之间来回。
		{ID: trayToggleWindowID, Label: "显示/隐藏主窗口", Click: func(*mygo.MenuItem, *mygo.Window) {
			s.toggleWindow()
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

// refreshTrayMenu 刷状态栏菜单上那些会变的东西。
//
// ⚠️ **为什么需要它**：托盘那一份菜单**只建一次**（`installTray`），而
// 「正在播放」那一行是**会变的**（换歌、暂停、没歌了）。
// ⇒ 如果只在建菜单时求值，那一行会永远是**启动那一刻**的那首——
// 而它恰恰是用户点开菜单**第一眼**看到的那一行。
//
// ⚠️ **不重建菜单**：重建会让状态栏图标闪一下（框架的 `Tray.SetMenu` 是
// 换掉整份菜单，`mygo/modules.go:752`）。所以只改那一行的文案
// （`MenuItem.SetLabel`）和那几项的可用态。
//
// ⚠️ **「比一遍再改」**：这一段每帧调一次，而 `SetLabel` / `SetEnabled`
// 每次都要往主线程投一个任务（同 `refreshMenuChecked` 的理由）。
func (s *Shell) refreshTrayMenu(st app.State) {
	if s.trayMenuRef == nil {
		return
	}
	label := s.trayNowPlayingLabel(st)
	hasTrack := st.HasTrack

	cur := s.trayState
	if cur.label == label && cur.hasTrack == hasTrack {
		return
	}
	s.trayState = trayState{label: label, hasTrack: hasTrack}

	if it := s.trayMenuRef.ItemByID(trayNowPlayingID); it != nil {
		it.SetLabel(label)
	}
	// ⚠️ **没歌的时候那三项该是禁用的**：给一个按了没反应的键比不给更糟。
	// 而应用菜单那边是这么做的（`refreshMenuChecked`）——**两处要一致**，
	// 否则用户从状态栏点「播放/暂停」而什么都没发生，会以为是菜单坏了。
	for _, id := range []string{"togglePlay", "prev", "next"} {
		if it := s.trayMenuRef.ItemByID(trayCommandIDPrefix + id); it != nil {
			it.SetEnabled(hasTrack)
		}
	}
}

// trayState 是状态栏菜单上那些会变的东西在**上一次刷新时**的样子。
//
// ⚠️ **它是用来「比一遍再改」的**（见 `refreshTrayMenu`）。
type trayState struct {
	label    string
	hasTrack bool
}

// trayNowPlayingLabel 是状态栏菜单最上面那一行：「正在播放：歌名 - 艺人」。
//
// ⚠️ **没有歌的时候说清楚**（见 `trayMenu` 里那一项的注释）。
//
// ⚠️ **艺人没有就只说歌名**：本地文件没有艺人信息（`app.State.Artist` 的
// 注释），而「歌名 - 」那一串尾巴上的破折号是给用户看的噪音。
func (s *Shell) trayNowPlayingLabel(st app.State) string {
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
