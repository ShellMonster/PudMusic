// 命令表：「用户做了一件事」这件事，在项目里**只定义一次**。
//
// # 为什么要有这一层
//
// 这一层是票 #713 立的，起因是一个具体的坑：
//
// **菜单的 accelerator 会先于窗口拿到那个键。** 框架自己的 E2E 测试写了这件事
// （`mygo/internal/e2e/menu_shortcuts_windows_test.go:14`）：`Ctrl+,` 跑了菜单项，
// 而内容区**一次都没收到**。
//
// ⇒ 所以「菜单项也带 ⌘P」一旦做了，菜单项的 `Click` 就**必须**走快捷键那一套，
// 不能自己调 `s.app.Toggle()`。否则同一个动作有两份实现：
//
//   - 改一处忘另一处 → 两边漂
//   - 两边的语义不一样（一个切歌、一个只切界面）→ 用户按菜单和按快捷键
//     结果不同，而**界面上一个字都不提**
//
// 托盘菜单今天就是第三份实现（`internal/ui/tray.go` 的 `trayMenu()` 直接调
// `s.app.Toggle()`），应用菜单是第四份。
//
// # 这一层是什么
//
// 一张表：每条命令有 id、显示名、以及「能不能出现在菜单里」。
// **动作不在这一层**——动作在 `runShortcut`（`shortcut_dispatch.go`），那一层
// 知道怎么跟应用层说话；这一层只回答「有哪些命令、它们叫什么」。
//
// ⚠️ **表里的 id 必须和 `shortcuts.go` 的 `defaultShortcutSettings` 里那些一致**：
// 设置页存的是 id，而菜单项按 id 去查那张表。两边对不上就是「菜单上那一项点了
// 什么也不发生」——而那正是这一层要消灭的症状。
//
// # 这一层只管应用内那一份快捷键
//
// 全局那一份（`shortcut_global.go`）是**同一个 id 的另一个键**（默认多一个 ⌥），
// 它管的是「应用不在前台时也要能响应」。菜单上显示的键是**应用内那一份**——
// 菜单在应用里被点开的时候应用就是在前台。
package ui

import "github.com/egoist/mygo"

// commandSpec 是「一条命令」的登记。
//
// ⚠️ **动作不在这里**：这里只放「这条命令叫什么」。
// 动作在 `runShortcut`（`shortcut_dispatch.go`）——那一层知道怎么跟应用层说话，
// 而这一层要能被 `menu.go` 和 `tray.go` 用，而那两个文件**不该**知道应用层的
// 那些方法。
type commandSpec struct {
	// id 是这条命令的标识。**和设置页存的那一份是同一个**（见文件头）。
	id string
	// name 是它在界面上显示的名字。
	name string
}

// commandSpecs 是命令表本体。
//
// ⚠️ **顺序就是菜单里的顺序**（`menu.go` 按它摆「播放」那一栏）。
// 所以它按「用户做一件事的顺序」排，不按字母排。
//
// ⚠️ **改这一张表就要改 `shortcuts.go` 的 `defaultShortcutNames`**：那一张是
// 设置页上显示的名字，而这一张是菜单上显示的。**两处要一样**——同一个命令在
// 设置页叫「播放/暂停」而在菜单里叫「播放」，用户会以为是两件事。
//
// ⚠️ **这里不再有「这条命令进不进菜单」那一栏**：它原来是个手写标志，
// 而那个标志**和菜单实况脱节过**——「快退 5 秒」「快进 5 秒」「音量加」
// 「音量减」四条明明在菜单里，标志却写着不进，于是
// `refreshMenuAccel`（`menu.go`）**不刷它们**：用户在设置页改了键，菜单上
// 那个旧键还在——而菜单的加速键**先于窗口**拿到那个键（见 `menu.go` 的
// 文件头），于是旧键被菜单吃掉，按了做的是菜单项的动作，而设置页里那个键
// 已经不归这条命令了。
//
// ⇒ 现在**「在不在菜单里」由菜单树自己说了算**（见 `menuCommandIDs`）。
var commandSpecs = []commandSpec{
	{id: "togglePlay", name: "播放/暂停"},
	{id: "prev", name: "上一首"},
	{id: "next", name: "下一首"},
	{id: "toggleMiniMode", name: "显示/隐藏迷你播放器"},
	{id: "seekBack", name: "快退 5 秒"},
	{id: "seekForward", name: "快进 5 秒"},
	{id: "volumeUp", name: "音量加"},
	{id: "volumeDown", name: "音量减"},
}

// commandByID 按 id 查一条命令。查不到返回零值（`ok` 为 false）。
//
// ⚠️ **查不到不是错误**：设置页的快捷键文件里可能有我们这一版不认识的 id
// （用户从更新的版本改回来的，或者手写坏的）。`runShortcut` 对认不出来的 id
// 什么都不做（`shortcut_dispatch.go` 的 switch 末尾），这里也一样。
func commandByID(id string) (commandSpec, bool) {
	for _, c := range commandSpecs {
		if c.id == id {
			return c, true
		}
	}
	return commandSpec{}, false
}

// commandName 按 id 查这条命令的名字。
//
// 查不到时**回落到 id 本身**而不是空串：一个空名字的菜单项在 macOS 上是
// 一个点得动但看不出是什么的东西。
func commandName(id string) string {
	if c, ok := commandByID(id); ok {
		return c.name
	}
	return id
}

// menuCommandIDs 是「菜单里那些命令项」的 id，按 `commandSpecs` 的顺序。
//
// ⚠️ **判据是「菜单树里有没有这一项」**，不是一张手写的清单。
//
// ⚠️ **为什么不能手写**：手写的那一份**和实况脱节过**（见 `commandSpecs`
// 顶上那段）。而脱节的后果很具体：菜单的加速键**先于窗口**拿到那个键，
// 所以菜单上留着一个旧键 ⇒ **那个旧键会被菜单吃掉**，而设置页里它已经不归
// 那条命令了 ⇒ 用户按了「键失灵」。
//
// ⇒ 所以这里**从菜单树里数**（`Menu.ItemByID` 是**递归**找的，
// `mygo/menu.go:226` 的 `itemByID`）。
func menuCommandIDs(m *mygo.Menu) []string {
	if m == nil {
		return nil
	}
	out := make([]string, 0, len(commandSpecs))
	for _, c := range commandSpecs {
		if m.ItemByID(menuCommandIDPrefix+c.id) != nil {
			out = append(out, c.id)
		}
	}
	return out
}
