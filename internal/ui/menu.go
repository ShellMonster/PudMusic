// 应用菜单：把只对网页有意义的那几项拿掉。
//
// ## 为什么菜单还要专门管一遍
//
// 框架在 macOS 上默认装的那份菜单里，「显示」那一栏是 `RoleViewMenu`：
// 重新载入 ⌘R、强制重新载入 ⇧⌘R、缩放一组（`mygo/menu.go` 的 roleSubmenu）。
// 窗口里是 MyGo 自己画的原生界面，根本没有页面可以载入，框架碰到这些角色
// 只会直接 return（`mygo/menu.go:508` 那几行）——菜单项还在、快捷键还占着，
// 按下去什么都不发生。
//
// Biu 的做法是吞掉按键（`electron/main.ts` 的 before-input-event 拦 ⌘R）。
// 我们更干脆：那一项根本不出现。让用户去按一个按了没反应的东西，比没有这个
// 功能更糟——它会让用户以为界面卡死了。
//
// 「新窗口」这一条不用管：框架默认的那几份菜单里没有这一项，我们自己也只开
// 一个窗口（见 `Shell.openWindow`）。
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
	if runtime.GOOS != "darwin" {
		return
	}
	mygo.App.SetMenu(appMenu())
}

// appMenu 是 macOS 上的菜单。
//
// 除了「显示」那一栏，其余照抄框架的默认菜单：那套结构是 macOS 用户按
// ⌘⇧A、⌘W、⌘Q 的肌肉记忆，自己重排一遍只会更难用。
//
// 留着的那几项是有用的：App 菜单里的「退出 ⌘Q」是「把窗口收起来之后想真的
// 退出」唯一的路（见 planWindowClose），编辑菜单是后面搜索框要用的。
func appMenu() *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},  // 关于 / 服务 / 隐藏 / 退出
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
