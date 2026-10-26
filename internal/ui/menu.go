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
//     （`app.go:337-339`），当场换上。
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
// 写回来**（下面 `appMenu` 里的后四栏）。
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
package ui

import (
	"runtime"

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
	// （`mygo/app.go:327-330` 的注释），窗口的可用区域会因此小一条。
	// 这一票是给 macOS 做的，而 Windows 上「设置」已经有一个入口了
	// （头像下拉里那一项，`navbar_avatar.go`），不值得为它改一版布局。
	if runtime.GOOS != "darwin" {
		return
	}
	mygo.App.SetMenu(appMenu(s.openAbout, s.openSettingsFromMenu))
}

// appMenu 是 macOS 上的菜单。about / settings 是那两项点了要做的事。
//
// 除了「应用」和「显示」那两栏，其余照抄框架的默认菜单：那套结构是 macOS 用户按
// ⌘⇧A、⌘W、⌘Q 的肌肉记忆，自己重排一遍只会更难用。
//
// 留着的那几项是有用的：App 菜单里的「退出 ⌘Q」是「把窗口收起来之后想真的
// 退出」唯一的路（见 planWindowClose），编辑菜单是后面搜索框要用的。
//
// ⚠️ **第一栏是应用菜单**，它的名字是**应用名**（`RoleAppMenu` 的默认标签
// 就是 `App.Name()`）——macOS 上左上角第一个菜单永远写着应用的名字。
func appMenu(about, settings func()) *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu, Submenu: []*mygo.MenuItem{
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
				about()
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
				settings()
			}},
			mygo.Separator(),
			// ⚠️ **「服务」那一项不写**：框架默认菜单里有它（`RoleServices`），
			// 但那一条**点了什么也不会发生**——macOS 的服务菜单要由
			// `setServicesMenu:` 注册（`mygo/internal/darwin/menu.go:142-149`），
			// 而那一句只在一个**带子菜单**的 services 项上跑。我们没有服务，
			// 于是那一项是一个**按了没反应**的亮着的东西——比没有更糟。
			//
			// 隐藏 / 隐藏其他 / 显示全部：框架的默认应用菜单里就是这三项
			// （`roleSubmenu(RoleAppMenu)`，`mygo/menu.go:723-734`）。
			{Role: mygo.RoleHide},
			{Role: mygo.RoleHideOthers},
			{Role: mygo.RoleUnhide},
			mygo.Separator(),
			// ⌘Q 是「把窗口收起来之后想真的退出」唯一的路（见 planWindowClose）。
			{Role: mygo.RoleQuit},
		}},
		{Role: mygo.RoleFileMenu}, // 关闭窗口 ⌘W（现在也遵守「关掉时怎么办」那个设置）
		{Role: mygo.RoleEditMenu}, // 撤销 / 剪切 / 复制 / 粘贴 / 全选
		{Role: mygo.RoleViewMenu, Submenu: []*mygo.MenuItem{
			// 角色留着、菜单项换掉：`prepareItem` 只在 Submenu 为空时才用角色
			// 自带的那一份（`mygo/menu.go:344`），菜单名的本地化仍然靠角色。
			{Role: mygo.RoleToggleDevTools},
			mygo.Separator(),
			{Role: mygo.RoleToggleFullScreen},
		}},
		{Role: mygo.RoleWindowMenu}, // 最小化 / 缩放 / 前置全部
	})
}

// 菜单项的 id。框架用 `Menu.ItemByID` 找它们（`mygo/menu.go:185`），
// 而**测试要靠 id 认出「哪一项」**——按标签找的话，改一次文案就得改一次测试。
const (
	// menuAboutID 是「关于布丁音乐」那一项。
	menuAboutID = "app.about"
	// menuSettingsID 是「设置…」那一项。
	menuSettingsID = "app.settings"
)
