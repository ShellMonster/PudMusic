// 应用菜单：macOS 窗口左上角那排菜单。
//
// # 这一份是怎么来的（研究结论，票 #539）
//
// ## `SetMenu` 什么时机调最稳
//
// 框架的 `Application.SetMenu`（`mygo/app.go:331`）做两件事：把菜单记在
// `App` 上、把 `menuSet` 置成 true。装菜单的时机有两条路：
//
//   - **应用还没 ready**（`mygo.App.Run()` 之前）：`handleReady`
//     （`mygo/app.go:239-267`）里有一句 `if !a.menuSet { a.menu = def }`
//     ——`menuSet` 是 true，所以**默认菜单不会被装上**，装的是我们这一份
//     （`backend().SetApplicationMenu(menu.snapshot())`，`app.go:260`）。
//   - **应用已经 ready**：同一句的另一半，`SetMenu` 自己会
//     `onMain(func() { backend().SetApplicationMenu(m.snapshot()) })`
//     （`mygo/app.go:337-339`），当场换上。
//
// 两条路都通，而**第一条更稳**：ready 之前换菜单的话，那一次安装和框架
// 装默认菜单是**同一次**（`handleReady` 里那一行），不会出现「先装默认、
// 再换 ours」的那一帧。
//
// ⚠️ **别在 `Run()` 之后才想起装**：那样菜单是在第一帧之后才换上的，
// 用户看见的是「应用起来了，菜单栏是空的，过一会儿才变成我们的」。
//
// 本项目装在 `installLifecycle` 里（`lifecycle.go`），而那一处在
// `ui.Run` 里、`mygo.App.Run()` **之前**——见 `shell_window.go` 的 `Run`。
//
// ## 默认菜单有哪几栏
//
// `defaultMenu()`（`mygo/menu.go:820`）：
//
//   - **macOS**：应用 / 文件 / 编辑 / 显示 / 窗口，**五栏**。
//   - **Windows / Linux**：**一栏都不装**（没有文档菜单时它返回 nil）。
//
// ## 我们设了自己的之后，那几栏还在不在
//
// **默认菜单是被整个换掉的，不是合并**：`handleReady` 里那句
// `if !a.menuSet { a.menu = def }` 走的是「不设就用默认」这一支——我们
// 设了，就**一栏默认的都没有了**。所以编辑 / 显示 / 窗口那几栏得**自己
// 写回来**（下面 `appMenu` 里的后几栏）。
//
// 幸而角色自带子菜单：`prepareItem`（`mygo/menu.go:328-374`）在
// `Submenu` 为空时会用 `roleSubmenu(role)` 把它填上。所以
// `{Role: mygo.RoleEditMenu}` 就是**框架默认的那一栏编辑菜单**
// （撤销 / 重做 / 剪切 / 复制 / 粘贴 / 粘贴并匹配样式 / 删除 / 全选 +
// 朗读），`{Role: mygo.RoleWindowMenu}` 是最小化 / 缩放 / 前置全部。
//
// ⚠️ **给了自己的 `Submenu` 就只留着角色的名字**（`prepareItem` 只在
// `Submenu` 为空时才用角色自带的那一份）——「显示」那一栏就是这么做的：
// 框架默认的那一栏里有重新载入 / 强制重新载入 / 缩放，而我们的窗口里是
// MyGo 自己画的原生界面，根本没有页面可以载入（按下去什么也不发生）。
//
// ## 快捷键
//
// 加速键的写法是 Electron 那一套（`mygo/internal/accelerator`）：
// `CmdOrCtrl+,` 在 macOS 上是 ⌘,，在别处是 Ctrl+,。⌘W / ⌘Q 这些是
// 角色自带的（`roleDefaults`，`mygo/menu.go:574`），不用自己写。
//
// # ⚠️ 菜单的加速键会**先于窗口**拿到那个键
//
// 这一条决定了这份菜单的整个形状，务必先读：
//
// 框架自己的 E2E 测试写了这件事（`mygo/internal/e2e/menu_shortcuts_windows_test.go:14`）：
// 菜单项的 `Ctrl+,` 跑了，而内容区**一次都没收到**。macOS 上同理——
// AppKit 先问主菜单的 key equivalents，问到了就不往下发给窗口。
//
// ⇒ 所以：
//
//  1. **菜单项的 `Click` 必须走快捷键那一套**（`runShortcut`），
//     不能自己调 `s.app.Toggle()`。理由见 `commands.go` 的文件头。
//  2. **菜单项挂的加速键必须跟着设置走**（`toMenuAccel`，
//     `menu_accel.go`）：用户在设置页改了「播放/暂停」的键，菜单上那一项
//     的加速键要一起改——否则菜单上那个旧键**会吃掉**它，而内容区那边
//     已经不认它了 ⇒ **按了什么也不发生**。
package ui

import (
	"runtime"
	"strconv"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/egoist/mygo"
)

// installMenu 装应用菜单。
func (s *Shell) installMenu() {
	// Windows / Linux 上框架默认一条菜单都不装（没有文档菜单时 defaultMenu
	// 返回 nil）。装上去反而多出一条 Biu 没有的菜单栏，而它每一项能干的活
	// ——复制粘贴、退出——在那边都有更短的路（快捷键、任务栏右键）。
	//
	// ⚠️ **在 Windows 上那不是「多一条」而是「窗口布局变了」**：框架的
	// `SetMenu` 在 Windows / Linux 上是**窗口自己的菜单栏**
	//（`mygo/app.go:327-330` 的注释），窗口的可用区域会因此小一条。
	// 这一票是给 macOS 做的，而 Windows 上「设置」已经有一个入口了
	// （头像下拉里那一项，`navbar_avatar.go`），不值得为它改一版布局。
	if runtime.GOOS != "darwin" {
		return
	}
	s.menu = appMenu(s)
	mygo.App.SetMenu(s.menu)
}

// appMenu 是 macOS 上的菜单。
//
// ⚠️ **形状照 iTunes / Apple Music**：音乐播放器该有的七栏
// （应用 / 文件 / 编辑 / 播放 / 歌曲 / 显示 / 窗口）。
//
// ⚠️ **每一项点了做什么，都在 `runShortcut`（`shortcut_dispatch.go`）那一套
// 里**——菜单项自己不实现动作。理由见文件头《菜单的加速键会先于窗口拿到
// 那个键》那一段。
//
// ⚠️ **「编辑」那一栏不许自己写**：框架在 macOS 上先于窗口拿到 ⌘C/⌘V/⌘Z
// （`mygo/ui/input.go:1043` 的 `commandKey`），自己写会和框架打架。
//
// ⚠️ **菜单是应用级的**（`SetMenu` 设在 `mygo.App` 上），而回调拿到的 `win`
// **可能是迷你播放器那一扇**——所以回调里**不看 `win`**，一律走主窗口
// （`openAbout` 里让开迷你 + 叫回主窗口）。
func appMenu(s *Shell) *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		// ⚠️ **第一栏的名字自己写，不靠 `RoleAppMenu` 的默认标签**。
		//
		// 框架的 `roleDefaults(RoleAppMenu)` 给的是 `App.Name()`（`mygo/menu.go:806`），
		// 而 `App.Name()`（`mygo/app.go:316`）在拿不到 bundle 信息时**回落到可执行
		// 文件的名字**——于是：
		//
		//   - 从 `mygo build` 打出来的包：名字是 `mygo.json` 里的「布丁音乐」 ✓
		//   - **源码直接 `go build` 出来的**：名字是「pudmusic」/「pudmusic-e2e」 ✗
		//
		// ⇒ 而**界面上那一律叫「布丁音乐」**（`appName`），窗口标题也是它。
		// 菜单栏第一栏写着「pudmusic」而窗口标题写着「布丁音乐」——**同一个应用两个
		// 名字**，而 macOS 上左上角第一个菜单**永远**是应用名，用户会以为装错了
		// 什么东西。
		//
		// ⇒ 所以自己写死 `appName`（和界面、窗口标题同一份）。
		{ID: menuAppColumnID, Role: mygo.RoleAppMenu, Label: appName, Submenu: appColumn(s)},
		{ID: menuFileColumnID, Role: mygo.RoleFileMenu, Submenu: fileColumn(s)},
		{ID: menuEditColumnID, Role: mygo.RoleEditMenu},
		// ⚠️ **「播放」「歌曲」那两栏必须自己写 `Label`**：框架没有这两个角色，
		// 而菜单栏上那一栏的名字就是**它的 `Label`**
		//（`mygo/internal/darwin/menu.go:143` 的 `buildMenu(it.Submenu, it.Label, …)`）。
		// 角色自带的那几栏（应用 / 文件 / 编辑 / 显示 / 窗口）的名字由角色本地化
		// 给出（`prepareItem` 里 `it.Label == ""` 时才用 `roleDefaults`），
		// **没给 `Label` 的那两栏会是一片空白**。
		{ID: menuPlayColumnID, Label: "播放", Submenu: playColumn(s)},
		{ID: menuSongColumnID, Label: "歌曲", Submenu: songColumn(s)},
		{ID: menuViewColumnID, Role: mygo.RoleViewMenu, Submenu: viewColumn(s)},
		{ID: menuWindowColumnID, Role: mygo.RoleWindowMenu, Submenu: windowColumn(s)},
	})
}

// windowColumn 是「窗口」那一栏。
//
// ⚠️ **迷你播放器那一项在这一栏，不在「显示」栏**——这是**三家播放器一致**的
// 做法（实测本机装着的三个，2026-11-01）：
//
//   - Apple Music：`窗口 ▸ 迷你播放程序 ⌥⌘M` / `窗口 ▸ 切换到迷你播放程序 ⌥⌘M`
//   - QQ音乐：`窗口 ▸ 切换到迷你窗口 ⇧⌘S`
//   - 网易云音乐：`窗口 ▸ 切换至迷你模式 ⌘M`
//
// ⇒ 而「显示」那一栏放的是**界面的样子**（视图档位、侧栏）。
//
// ⚠️ **框架的「窗口」栏是 `RoleWindowMenu`**，它自带「最小化 / 缩放 /
// 前置全部」（`mygo/menu.go:934` 的 `roleSubmenu(RoleWindowMenu)`）。
// 给了自己的 `Submenu` 就**只留着角色的名字**，所以那三项得**自己写回来**。
func windowColumn(s *Shell) []*mygo.MenuItem {
	return []*mygo.MenuItem{
		// ⚠️ **迷你播放器那一项是「命令项 + 勾选型」**：它既是一个命令（走
		// `runShortcut`，于是有加速键、而且加速键跟着设置走），又是一个勾选态
		//（迷你窗开着的时候打着钩）。
		//
		// ⚠️ **为什么不用 `menuCommandItem` 那一档**：那一档造出来的是**普通项**，
		// 而这一项要**勾选态**——`MenuItemCheckbox` 在框架里被点的时候会**自己
		// 先翻勾选**（`mygo/menu.go:563` 的 `menuItemClicked`：`case MenuItemCheckbox:
		// it.SetChecked(!it.IsChecked())`），而我们要的勾选态是「迷你窗开着没有」
		// 这个**真状态**，不是「用户点过」。
		// ⇒ 所以自己写这一项：勾选态由 `refreshMenuChecked` 按真状态刷。
		//
		// ⚠️ **加速键照样要挂**（否则设置页里明明有一个键，而菜单上一片空白）。
		//
		// ⚠️ **它的加速键是 ⌘⇧M**（不是 ⌘M）：⌘M 是 macOS 的「最小化」
		//（框架「窗口」栏里那一项带的键，`mygo/menu.go:743`），而**菜单的加速键
		// 先于窗口拿到那个键**——抢它的结果是**我们这个永远不响**。
		// 详见 `internal/ui/shortcuts.go` 的 `shiftedShortcuts`。
		{ID: menuCommandIDPrefix + "toggleMiniMode", Label: "显示/隐藏迷你播放器",
			Type: mygo.MenuItemCheckbox,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				s.onShortcut("toggleMiniMode")
			},
			Accelerator: s.menuAccelFor("toggleMiniMode"),
		},
		mygo.Separator(), {ID: menuCommandIDPrefix + "toggleNotchMode", Label: "显示/隐藏刘海",
			Type: mygo.MenuItemCheckbox,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				s.onShortcut("toggleNotchMode")
			},
			Accelerator: s.menuAccelFor("toggleNotchMode"),
		},
		mygo.Separator(),
		// 框架「窗口」栏自带的那三项。
		{Role: mygo.RoleMinimize},
		{Role: mygo.RoleZoom},
		mygo.Separator(),
		{Role: mygo.RoleFront},
	}
}

// appColumn 是第一栏（应用菜单）。
//
// ⚠️ **第一栏是应用菜单**，它的名字是**应用名**（`RoleAppMenu` 的默认标签
// 就是 `App.Name()`）——macOS 上左上角第一个菜单永远写着应用的名字。
func appColumn(s *Shell) []*mygo.MenuItem {
	return []*mygo.MenuItem{
		// 「关于布丁音乐」。
		//
		// ⚠️ **不用 `RoleAbout`**：那个角色在 macOS 上走的是系统的
		// `orderFrontStandardAboutPanel:`（`mygo/internal/darwin/menu.go:38`），
		// 弹出来的是**系统那个关于面板**——它只有应用名、版本、版权和一段
		// 纯文本，**仓库链接点不了，第三方许可也没有入口**。而这两样是
		// 「关于」被点开的主要理由。所以这一项是自定义的 + `Click`。
		//
		// ⚠️ **菜单是应用级的**（`SetMenu` 设在 `mygo.App` 上），而回调
		// 拿到的 `win` **可能是迷你播放器那一扇**——所以回调里**不看
		// `win`**，一律走主窗口（`openAbout` 里让开迷你 + 叫回主窗口）。
		{ID: menuAboutID, Label: "关于" + appName, Click: func(*mygo.MenuItem, *mygo.Window) {
			s.openAbout()
		}},
		// ⚠️ **「关于」和「设置…」之间有一条分隔线**：原生应用都是这样
		// （实测访达 / Edge / Typora / ChatGPT：「关于 X」/ --- / 「设置…」/
		// --- / …）。少了它，那两项挤在一起，看起来像一组。
		mygo.Separator(),
		// 「设置…」。
		//
		// ⌘, 是 macOS 的惯例（系统设置 / 偏好设置都是 ⌘,）。写法是
		// `CmdOrCtrl+,`——在 macOS 上是 ⌘,，在别处是 Ctrl,+。
		{ID: menuSettingsID, Label: "设置…", Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) {
			s.openSettingsFromMenu()
		}},
		mygo.Separator(),
		// ⚠️ **「服务」那一项不写**：框架默认菜单里有它（`RoleServices`），
		// 但那一条**点了什么也不会发生**——macOS 的服务菜单要由
		// `setServicesMenu:` 注册（`mygo/internal/darwin/menu.go:142-149`），
		// 而那一句只在一个**带子菜单**的 services 项上跑。我们没有服务，
		// 于是那一项是一个**按了没反应**的亮着的东西——比没有更糟。
		//
		// 隐藏 / 隐藏其他 / 显示全部：框架的默认应用菜单里就是这三项
		//（`roleSubmenu(RoleAppMenu)`，`mygo/menu.go:723-734`）。
		{Role: mygo.RoleHide},
		{Role: mygo.RoleHideOthers},
		{Role: mygo.RoleUnhide},
		mygo.Separator(),
		// ⌘Q 是「把窗口收起来之后想真的退出」唯一的路（见 planWindowClose）。
		{Role: mygo.RoleQuit},
	}
}

// fileColumn 是「文件」那一栏。
//
// ⚠️ **「关闭窗口 ⌘W」留成角色**（`RoleClose`）——它是框架自带的，而它带的
// 那个键（⌘W）跟「关掉时怎么办」那个设置是一起的（见 `planWindowClose`）。
func fileColumn(s *Shell) []*mygo.MenuItem {
	return []*mygo.MenuItem{
		// 「本地音乐」和「下载记录」：去那两页。
		//
		// ⚠️ **为什么是「去那一页」而不是「弹文件框选」**：本地音乐页里已经有
		// 一个「添加文件夹」的入口，而菜单这一项要做的是**让用户能一眼到
		// 那页去**——和「下载记录」一样是「跳到某一页」。
		{ID: menuLocalMusicID, Label: "本地音乐", Click: func(*mygo.MenuItem, *mygo.Window) {
			s.openPage(localMusicPath)
		}},
		{ID: menuDownloadListID, Label: "下载记录", Click: func(*mygo.MenuItem, *mygo.Window) {
			s.openPage(downloadListPath)
		}},
		mygo.Separator(),
		{Role: mygo.RoleClose},
	}
}

// playColumn 是「播放」那一栏。
//
// ⚠️ **每一项都走 `runShortcut`**——菜单项自己不实现动作。理由见文件头
// 《菜单的加速键会先于窗口拿到那个键》那一段：菜单项的加速键会**抢**走
// 那个键，所以菜单项和快捷键必须是**同一份实现**，否则用户按菜单和按
// 快捷键的结果会不一样。
//
// ⚠️ **加速键从设置里读**（`toMenuAccel`）：用户在设置页改了键，菜单上
// 那一项跟着改。
func playColumn(s *Shell) []*mygo.MenuItem {
	items := []*mygo.MenuItem{
		menuCommandItem(s, "togglePlay", "播放/暂停"),
		menuCommandItem(s, "prev", "上一首"),
		menuCommandItem(s, "next", "下一首"),
		mygo.Separator(),
		// 快退 / 快进。
		//
		// ⚠️ **它们默认的键是 ⇧⌘← / ⇧⌘→**（`shortcuts.go` 的
		// `inAppOnlyShortcuts`），而 ⌘← 在菜单里是「上一首」——所以这两项
		// **各自带自己的键**，不会跟上面那两项撞。
		menuCommandItem(s, "seekBack", "快退 5 秒"),
		menuCommandItem(s, "seekForward", "快进 5 秒"),
		mygo.Separator(),
	}
	items = append(items, playModeItem(s)...)
	items = append(items, mygo.Separator())
	items = append(items, menuCommandItem(s, "volumeUp", "音量加"))
	items = append(items, menuCommandItem(s, "volumeDown", "音量减"))
	items = append(items, &mygo.MenuItem{
		ID: menuMuteID, Label: "静音", Click: func(*mygo.MenuItem, *mygo.Window) {
			s.app.ToggleMute()
		},
	})
	return items
}

// playModeItem 是「播放模式」那一组（顺序 / 循环 / 随机 / 单曲）。
//
// ⚠️ **做成四个平铺的单选项**，不收进子菜单。
//
// ⚠️ **这一条和 Apple Music 相反**（它的「Controls」栏里「Shuffle ▸」和
// 「Repeat ▸」是**两个带子菜单的项**，「Repeat」那一档是 Off / All / One）——
// 但我们这边平铺是**有理由的**：我们的「播放」栏已经有十来项，再收一层子菜单
// 要点两下才够得着；而四行平铺在 macOS 的菜单里也不难看。
//
// ⚠️ **别照抄 Apple Music 就以为对了**：我们那四档是**互斥**的（顺序 / 循环 /
// 随机 / 单曲，任意时刻只有一个），而 Music 的 Shuffle 和 Repeat 是**两个
// 正交的开关**——照抄它的形状反而会让「随机 + 单曲」这种组合没法表达。
//
// ⚠️ **勾选态是运行期刷的**（`refreshMenuChecked`）——播放模式随时会变
// （用户按播放条上那枚键、或者从这里切档）。
func playModeItem(s *Shell) []*mygo.MenuItem {
	items := make([]*mygo.MenuItem, 0, len(app.PlayModes))
	for _, m := range app.PlayModes {
		mode := m
		items = append(items, &mygo.MenuItem{
			ID:    menuPlayModeIDPrefix + string(mode),
			Label: mode.Label(),
			Type:  mygo.MenuItemRadio,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				s.app.SetPlayMode(mode)
				// ⚠️ **立刻把勾选态刷一遍**（不等下一帧）。
				//
				// 框架在调我们这个 `Click` **之前**已经改过勾选态了
				//（`mygo/menu.go:569` 的 `menuItemClicked`：对 radio 项先
				// `SetChecked(true)`，而那一支会先把**同组所有项**置为 false）。
				// ⇒ 框架那一下之后，菜单上是「全灭 → 点亮这一项」。
				//
				// 而**我们要的勾选态是「当前是哪一档」这个真状态**，不是「用户
				// 点过哪一档」——那两样在「点已经勾上的那一项」时不一样。
				//
				// ⚠️ **为什么这里刷了就够**：框架那一次和这一次**都是投到主线程**
				// 的（`MenuItem.update` → `postMain`，`mygo/menu.go:315`），
				// 而框架的**先投**、我们的**后投** ⇒ 主线程上**我们的后执行 =
				// 后赢**。所以顺序是确定的，不靠运气。
				s.refreshMenuChecked(s.app.Snapshot())
			},
		})
	}
	return items
}

// songColumn 是「歌曲」那一栏。
//
// ⚠️ **这几项在看当前这一首**：没歌的时候「赞」「下载当前歌曲」这些**该是
// 禁用的**——给一个按了没用的键比不给更糟（用户按两次发现还是那样，就不再
// 信这个应用了）。理由同 `comments_entry.go` 的文件头。
func songColumn(s *Shell) []*mygo.MenuItem {
	return []*mygo.MenuItem{
		// ⚠️ **「添加到收藏夹…」而不是「喜欢当前歌曲」**：这一项开的是**一个
		// 弹窗**（问「放到哪些收藏夹」），而用户读「喜欢当前歌曲」会以为按一下
		// 就放进去了——按完发现弹出一个要选文件夹的窗，那就是「这个应用不听话」。
		//
		// 省略号是 macOS 的写法：**这一项开了一个窗**（对话框）。
		{ID: menuFavoriteID, Label: "添加到收藏夹…", Click: func(*mygo.MenuItem, *mygo.Window) {
			s.favoriteCurrent()
		}},
		{ID: menuDownloadID, Label: "下载当前歌曲", Click: func(*mygo.MenuItem, *mygo.Window) {
			s.downloadCurrent()
		}},
		mygo.Separator(),
		{ID: menuCommentsID, Label: "显示评论", Click: func(*mygo.MenuItem, *mygo.Window) {
			s.openComments()
		}},
		{ID: menuArtistSpaceID, Label: "打开 UP 主空间", Click: func(*mygo.MenuItem, *mygo.Window) {
			s.openArtistSpace()
		}},
	}
}

// viewColumn 是「显示」那一栏。
//
// ⚠️ **角色留着、菜单项换掉**：`prepareItem` 只在 Submenu 为空时才用角色
// 自带的那一份（`mygo/menu.go:344`），菜单名的本地化仍然靠角色。
//
// ⚠️ **框架默认的那一栏里有重新载入 / 强制重新载入 / 缩放**，而我们的窗口
// 里是 MyGo 自己画的原生界面，根本没有页面可以载入（按下去什么也不发生）。
func viewColumn(s *Shell) []*mygo.MenuItem {
	items := make([]*mygo.MenuItem, 0, 10)
	for _, m := range displayModes {
		mode := m.mode
		items = append(items, &mygo.MenuItem{
			ID:    menuDisplayModeIDPrefix + string(mode),
			Label: m.label,
			Type:  mygo.MenuItemRadio,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				s.applySetting("显示样式", func(cur *app.Settings) { cur.DisplayMode = mode })
				// ⚠️ **立刻把勾选态刷一遍**（理由同 `playModeItem` 那一段：
				// 框架在调我们这个 `Click` 之前已经改过勾选态了，而我们后投的
				// 这一次在后执行 = 后赢）。
				s.refreshMenuChecked(s.app.Snapshot())
			},
		})
	}
	items = append(items, mygo.Separator())
	items = append(items, &mygo.MenuItem{
		ID: menuSidebarID, Label: "显示/隐藏侧栏", Type: mygo.MenuItemCheckbox,
		Click: func(*mygo.MenuItem, *mygo.Window) {
			s.toggleSidebar()
		},
	})
	items = append(items, mygo.Separator())
	// ⚠️ **「开发者工具」只在开发构建里**：框架有 `mygo.IsDev()`
	//（`mygo/dev.go:12`）——发布构建由 `mygo build` 注入 `production=1`
	//（`scripts/release.sh` 走的就是 `mygo build`）。
	//
	// 正式版的菜单栏上摆着「开发者工具」（还占着 ⌥⌘I）不是 macOS 惯例——
	// Chrome / Edge / Safari 的发布版菜单里都没有它。
	if mygo.IsDev() {
		items = append(items, &mygo.MenuItem{Role: mygo.RoleToggleDevTools})
	}
	items = append(items, &mygo.MenuItem{Role: mygo.RoleToggleFullScreen})
	return items
}

// menuCommandItem 是「一条命令」在菜单里的那一项。
//
// ⚠️ **动作走 `runShortcut`**（`s.onShortcut`）而不是菜单项自己实现——
// 见文件头《菜单的加速键会先于窗口拿到那个键》那一段。
//
// ⚠️ **加速键从设置里读**（`toMenuAccel`）：翻译不出来就**不显示**那个键，
// 但**命令本身照常在菜单里**（用户清空了一格快捷键，菜单项不该跟着消失）。
func menuCommandItem(s *Shell, id, label string) *mygo.MenuItem {
	it := &mygo.MenuItem{
		ID:    menuCommandIDPrefix + id,
		Label: label,
		Click: func(*mygo.MenuItem, *mygo.Window) { s.onShortcut(id) },
	}
	if acc := s.menuAccelFor(id); acc != "" {
		it.Accelerator = acc
	}
	return it
}

// menuAccelFor 查一条命令在菜单上该显示的加速键。
//
// ⚠️ **读的是设置页存的那一份**（`shortcuts.go` 的 `snapshot().Shortcuts`），
// 不是命令表里的默认值——用户在设置页改过之后菜单要跟着变。
//
// ⚠️ **翻译不出来就返回空串**（`toMenuAccel` 那一支）：菜单上不显示那个键，
// 但**命令本身还在**——用户清空了一格快捷键，菜单项不该跟着消失。
func (s *Shell) menuAccelFor(id string) string {
	for _, it := range s.keys.snapshot().Shortcuts {
		if it.ID != id {
			continue
		}
		if got := toMenuAccel(accelerator(it.Shortcut)); got.ok {
			return got.accel
		}
		return ""
	}
	return ""
}

// refreshMenuChecked 刷菜单里的勾选态和可用态。
//
// ⚠️ **为什么要在运行期刷**：菜单是系统画的，它**不在窗口的画面里**，所以
// 那些状态（播放模式、显示模式、侧栏折叠、迷你窗开着没有、当前有没有歌）
// 变了之后，菜单上不会自己变——得我们伸手去改（`MenuItem.SetChecked`、
// `SetEnabled`）。
//
// ⚠️ **不在菜单里自己变**：框架的 `SetChecked` / `SetEnabled` 每次都要
// 往主线程投递一个任务（`mygo/menu.go:315` 的 `update` → `postMain`）——
// 而这一段是**每帧调一次**的（见 `viewMain`），每帧投一次主线程任务就是
// 每帧一次无谓的跨线程往返。
//
// ⇒ 所以这里**比一遍再改**（`menuState`）：只有真的变了才动手。
//
// ⚠️ **什么时候调**：`viewMain` 每帧调一次（那是「状态随时会变」的那几样
// 的兜底），加上几个明确的点（改设置、切档、开迷你窗——那些地方立刻就要看到）。
//
// ⚠️ **快照是外面传进来的**（`viewMain` 取的那一份）：`Snapshot()` **会分配**
// （章节切片 + B 站链接字符串），而这一帧已经取过一次了——再取一份就是白花。
func (s *Shell) refreshMenuChecked(st app.State) {
	if s.menu == nil {
		return
	}
	// ⚠️ **只取菜单要的那两样**（`MenuDisplayState`），不拷整份设置——
	// `SettingsSnapshot()` 每帧调一次是**一次堆分配**（三十几个字段的值拷贝），
	// 而菜单每帧只想问两样。实测这一段从每帧 1 次分配降到 0 次。
	display, sidebarCollapsed := s.app.MenuDisplayState()

	// 播放模式。
	mode := s.app.PlayMode()
	// 侧栏展开着 = 勾上了「显示/隐藏侧栏」。
	sidebar := !sidebarCollapsed
	// 迷你窗开着 = 勾上了「显示/隐藏迷你播放器」。
	mini := s.mini != nil
	// 刘海播放器开着 = 勾上了设置里的开关。刘海外壳启动后常驻，
	// 所以不能用 `s.notch != nil` 判断启用状态。
	notch := s.app.NotchPlayerEnabled()
	// 「歌曲」那一栏：有歌才有那几项。
	hasTrack := st.HasTrack
	// ⚠️ **「显示评论」有自己的判据**（理由见下面改的那几行）：它要这一首
	// **有评论可问**。
	commentsWorthAsking := s.app.CommentsWorthAsking()
	// 「打开 UP 主空间」要 UP 主的 mid。
	hasArtist := hasTrack && st.ArtistMid > 0

	cur := s.menuState
	if cur.playMode == mode && cur.displayMode == display && cur.sidebar == sidebar &&
		cur.mini == mini && cur.notch == notch && cur.hasTrack == hasTrack && cur.hasArtist == hasArtist &&
		cur.commentsWorthAsking == commentsWorthAsking {
		// ⚠️ **一样就什么都不做**：每帧都往主线程投一次任务，是白花的钱。
		return
	}
	s.menuState = menuState{
		playMode: mode, displayMode: display, sidebar: sidebar,
		mini: mini, notch: notch, hasTrack: hasTrack, hasArtist: hasArtist,
		commentsWorthAsking: commentsWorthAsking,
	}

	// 播放模式那一组。
	for _, m := range app.PlayModes {
		if it := s.menu.ItemByID(menuPlayModeIDPrefix + string(m)); it != nil {
			it.SetChecked(m == mode)
		}
	}

	// 显示模式那一组。
	for _, m := range displayModes {
		if it := s.menu.ItemByID(menuDisplayModeIDPrefix + string(m.mode)); it != nil {
			it.SetChecked(m.mode == display)
		}
	}

	// 侧栏和迷你窗那两个勾选型。
	if it := s.menu.ItemByID(menuSidebarID); it != nil {
		it.SetChecked(sidebar)
	}
	if it := s.menu.ItemByID(menuCommandIDPrefix + "toggleMiniMode"); it != nil {
		it.SetChecked(mini)
	}
	if it := s.menu.ItemByID(menuCommandIDPrefix + "toggleNotchMode"); it != nil {
		it.SetChecked(notch)
	}

	// ⚠️ **「歌曲」那一栏的判据不是一个「有没有歌」**：每一项要什么，各有各的。
	//
	//   - 「显示评论」要 `CommentsWorthAsking()`——本地文件在 B 站上压根没有
	//     对应的对象，问一个认不出来的 oid 的症状是「静默拿到空列表」。
	//   - 「收藏」「下载」只要有歌就行。
	//
	// ⚠️ **界面上同一个功能用的是同一套判据**（`comments_entry.go` 的
	// `viewCommentsButton` 用 `CommentsWorthAsking()`——本地文件**根本不摆
	// 那个键**）。⇒ 菜单和界面必须用同一套，否则「界面上没有这一枚、菜单里
	// 却有一个按了报错」——而按一个点了报错的键比没有那个键更糟（用户按两次
	// 发现还是那样，就不再信这个应用了）。
	if it := s.menu.ItemByID(menuCommentsID); it != nil {
		it.SetEnabled(commentsWorthAsking)
	}
	for _, id := range []string{menuFavoriteID, menuDownloadID} {
		if it := s.menu.ItemByID(id); it != nil {
			it.SetEnabled(hasTrack)
		}
	}
	if it := s.menu.ItemByID(menuArtistSpaceID); it != nil {
		it.SetEnabled(hasArtist)
	}
}

// refreshMenuAccel 刷新菜单上那些命令项的加速键。
//
// ⚠️ **为什么必须刷**：菜单的加速键**会先于窗口拿到那个键**（框架自己的
// E2E：`mygo/internal/e2e/menu_shortcuts_windows_test.go:14`——`Ctrl+,` 跑了
// 菜单项，内容区一次都没收到）。
//
// ⇒ 所以用户在设置页把「播放/暂停」从 ⌘P 改成 ⌘⇧P 之后，菜单上还留着 ⌘P
// 的话，**⌘P 仍然会被那个菜单项吃掉**——而设置页里 ⌘P 已经不归任何命令了。
// 于是用户按 ⌘P 得到的动作，是菜单项自己那一份，而不是设置页说的「这个键
// 现在不归谁」。
//
// ⇒ 所以：**设置一改，菜单上的加速键就跟着改**。
//
// ⚠️ **用 `SetAccelerator` 而不是重建菜单**：框架支持运行期改
// （`mygo/menu.go:304`），而重建菜单会让用户看见菜单栏闪一下。
func (s *Shell) refreshMenuAccel() {
	if s.menu == nil {
		return
	}
	// ⚠️ **按菜单树里实际有的那些命令项刷**，不按一张手写的清单——
	// 理由见 `menuCommandIDs` 的注释（手写的那份和实况脱节过，而脱节的后果是
	// 「旧键被菜单吃掉」）。
	for _, id := range menuCommandIDs(s.menu) {
		it := s.menu.ItemByID(menuCommandIDPrefix + id)
		if it == nil {
			continue
		}
		it.SetAccelerator(s.menuAccelFor(id))
	}
}

// menuState 是「菜单上那些勾选态和可用态」在**上一次刷新时**的样子。
//
// ⚠️ **它的用途是「比一遍再改」**（见 `refreshMenuChecked`）——不是状态本身
// 的来源。状态每次都现读（`app.Snapshot` / `SettingsSnapshot`），这一份只是
// 用来判「变了没有」。
type menuState struct {
	playMode    app.PlayMode
	displayMode app.DisplayMode
	sidebar     bool
	mini        bool
	notch       bool
	hasTrack    bool
	hasArtist   bool
	// commentsWorthAsking 是「当前这一首有没有评论可问」（`App.CommentsWorthAsking`）。
	commentsWorthAsking bool
}

// openPage 是「从菜单跳到某一页」。
//
// ⚠️ **要让开迷你**：路由表是**主窗口**那一份（`navigate` 改的是 `s.router`），
// 迷你那一扇有自己的外壳和自己的路由。在迷你开着的时候换路由，主窗口那一页
// 是换了，而用户眼前还是迷你——于是「点了本地音乐，什么都没发生」。
// 理由同 `openSettingsFromMenu`（`about.go:117`）。
func (s *Shell) openPage(path string) {
	s.dropMini()
	s.navigate(path)
	s.restoreMainWindow()
}

// favoriteCurrent 是「喜欢」那条路：菜单「添加到收藏夹…」、迷你窗和刘海
// 那枚心形都走它——开「把这一首放进哪些收藏夹」那个弹窗。
//
// ⚠️ **开的是「放到哪些收藏夹」那个弹窗**，不是直接放进去——直接放要先把
// 稿件 id 问出来，那要一轮网络往返且**静悄悄失败**（用户按了没反应，又不
// 知道问的是谁）。
//
// ⚠️ **提示要落在画 toast 的那一扇上**：`viewToasts` 只在主窗口那一支调——
// 迷你窗和刘海窗都不画 toast。从它们那两扇上点心形时，`s` 是那个副壳，
// 直接 `s.addToast` 会把那句话写进一扇永远不画它的窗（静默失败）。
func (s *Shell) favoriteCurrent() {
	it, ok := s.app.PlayingItem()
	toastShell := s
	if s.main != nil {
		toastShell = s.main
	}
	if !ok {
		toastShell.addToast("这一首还没有取回来，收藏不了", notice.Warning)
		return
	}
	s.openFolderSelect(it)
}

// downloadCurrent 是菜单里「下载当前歌曲」那一项做的事。
//
// ⚠️ **走 `downloadLater`**（`download_mount.go`）——它是**异步**的那一条，
// 而界面线程上一次网络往返都不许有。
func (s *Shell) downloadCurrent() {
	it, ok := s.app.PlayingItem()
	if !ok {
		return
	}
	s.downloadLater(it, false, "下载音频")
}

// openComments 是菜单里「显示评论」那一项做的事。
//
// ⚠️ **开抽屉，不是跳路由**（票 #706）：评论内容是**右侧抽屉**，不是独立页。
// 理由见 `comments_entry.go` 的文件头。
func (s *Shell) openComments() {
	if !s.app.CommentsWorthAsking() {
		return
	}
	s.closeFullPlayer()
	s.commentsOpen = true
	s.wake.invalidate()
}

// openArtistSpace 是菜单里「打开 UP 主空间」那一项做的事。
func (s *Shell) openArtistSpace() {
	st := s.app.Snapshot()
	if !st.HasTrack || st.ArtistMid <= 0 {
		return
	}
	s.openPage(upPathPrefix + strconv.FormatInt(st.ArtistMid, 10))
}

// 菜单项的 id。框架用 `Menu.ItemByID` 找它们（`mygo/menu.go:185`），
// 而**测试要靠 id 认出「哪一项」**——按标签找的话，改一次文案就得改一次测试。
const (
	// menuAppColumnID 是第一栏（应用菜单）。
	menuAppColumnID = "column.app"
	// menuFileColumnID 是「文件」那一栏。
	menuFileColumnID = "column.file"
	// menuEditColumnID 是「编辑」那一栏。
	menuEditColumnID = "column.edit"
	// menuPlayColumnID 是「播放」那一栏。
	menuPlayColumnID = "column.play"
	// menuSongColumnID 是「歌曲」那一栏。
	menuSongColumnID = "column.song"
	// menuViewColumnID 是「显示」那一栏。
	menuViewColumnID = "column.view"
	// menuWindowColumnID 是「窗口」那一栏。
	menuWindowColumnID = "column.window"

	// menuAboutID 是「关于布丁音乐」那一项。
	menuAboutID = "app.about"
	// menuSettingsID 是「设置…」那一项。
	menuSettingsID = "app.settings"

	// menuLocalMusicID 是「本地音乐」那一项。
	menuLocalMusicID = "file.localMusic"
	// menuDownloadListID 是「下载记录」那一项。
	menuDownloadListID = "file.downloadList"

	// menuCommandIDPrefix 是「一条命令」那一项 id 的前缀。
	menuCommandIDPrefix = "cmd."
	// menuPlayModeIDPrefix 是「播放模式」那几项 id 的前缀。
	menuPlayModeIDPrefix = "playmode."
	// menuDisplayModeIDPrefix 是「显示模式」那几项 id 的前缀。
	menuDisplayModeIDPrefix = "displaymode."

	// menuFavoriteID 是「收藏当前歌曲」那一项。
	menuFavoriteID = "song.favorite"
	// menuDownloadID 是「下载当前歌曲」那一项。
	menuDownloadID = "song.download"
	// menuCommentsID 是「显示评论」那一项。
	menuCommentsID = "song.comments"
	// menuArtistSpaceID 是「打开 UP 主空间」那一项。
	menuArtistSpaceID = "song.artistSpace"

	// menuMuteID 是「静音」那一项。
	menuMuteID = "play.mute"
	// menuSidebarID 是「显示/隐藏侧栏」那一项。
	menuSidebarID = "view.sidebar"
)
