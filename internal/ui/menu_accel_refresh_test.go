// 菜单上的加速键跟着设置走（票 #716）。
//
// 这一族断的是**设置改了之后菜单上那一项的加速键跟着改**——那是这一票的
// 全部内容。
//
// ⚠️ **为什么这一条要紧**：菜单的加速键**会先于窗口拿到那个键**（框架自己的
// E2E：`mygo/internal/e2e/menu_shortcuts_windows_test.go:14`——`Ctrl+,` 跑了
// 菜单项，内容区一次都没收到）。
//
// ⇒ 所以菜单上留着旧键的话，**旧键会被菜单吃掉**：用户把「播放/暂停」从 ⌘P
// 改成 ⌘⇧P 之后，按 ⌘P 得到的仍然是那个菜单项的动作，而设置页里 ⌘P 已经
// 不归任何命令了。
package ui

import (
	"testing"
)

// TestChangingAShortcutChangesTheMenuAccelerator 断**改设置 → 菜单跟着改**。
//
// ⚠️ **判据是「菜单项上的加速键」**：不是「按了有没有反应」——那要真窗口。
//
// ⚠️ **走的是生产的那个出口**（`applyShortcut`）：设置页上录一个键就是走它，
// 而「恢复默认」走的是 `applyShortcutChange`（另一条出口，另有���条断它的）。
func TestChangingAShortcutChangesTheMenuAccelerator(t *testing.T) {
	b := newTestShell(t)
	s := b.s
	s.menu = appMenu(s)

	// 默认：⌘P。
	it := s.menu.ItemByID(menuCommandIDPrefix + "togglePlay")
	if it == nil {
		t.Fatal("菜单里没有「播放/暂停」那一项")
	}
	if it.Accelerator != "CmdOrCtrl+P" {
		t.Fatalf("默认的加速键是 %q，want %q", it.Accelerator, "CmdOrCtrl+P")
	}

	// 改成 ⌘⇧P。
	s.applyShortcut(shortcutSlot{id: "togglePlay"}, "CommandOrControl+Shift+P")

	if it.Accelerator != "CmdOrCtrl+Shift+P" {
		t.Errorf("改完之后菜单上的加速键是 %q，want %q", it.Accelerator, "CmdOrCtrl+Shift+P")
	}
}

// TestResettingTheShortcutsChangesTheMenuAccelerator 断**「恢复默认」也刷菜单**。
//
// ⚠️ **「恢复默认」是另一条出口**（`applyShortcutChange`，设置页那个按钮）——
// 而它一次性改八个键。漏了它，用户一键恢复之后菜单上还是旧的键，而那八个键
// **会被菜单吃掉**。
func TestResettingTheShortcutsChangesTheMenuAccelerator(t *testing.T) {
	b := newTestShell(t)
	s := b.s
	s.menu = appMenu(s)

	// 先改一个。
	s.applyShortcut(shortcutSlot{id: "togglePlay"}, "CommandOrControl+Shift+P")
	it := s.menu.ItemByID(menuCommandIDPrefix + "togglePlay")
	if it.Accelerator != "CmdOrCtrl+Shift+P" {
		t.Fatalf("改完之后菜单上的加速键是 %q，want %q", it.Accelerator, "CmdOrCtrl+Shift+P")
	}

	// 恢复默认。
	s.applyShortcutChange("快捷键", s.keys.reset)

	if it.Accelerator != "CmdOrCtrl+P" {
		t.Errorf("恢复默认之后菜单上的加速键是 %q，want %q", it.Accelerator, "CmdOrCtrl+P")
	}
}

// TestClearingAShortcutRemovesTheAcceleratorButKeepsTheItem 断**清空一格之后
// 菜单项还在，只是没有键了**。
//
// ⚠️ **为什么不把菜单项也去掉**：用户清空的是「这一条命令的快捷键」，不是
// 「这一条命令」。命令还在（菜单里那一项还在），只是**不再有键**。
//
// ⚠️ **为什么不能让它带着旧键**：旧键会被菜单吃掉（见文件头），而设置页里
// 那个键已经不归这条命令了 ⇒ 用户按那个键什么也不发生，而菜单上明明写着它。
func TestClearingAShortcutRemovesTheAcceleratorButKeepsTheItem(t *testing.T) {
	b := newTestShell(t)
	s := b.s
	s.menu = appMenu(s)

	s.applyShortcut(shortcutSlot{id: "togglePlay"}, "")

	it := s.menu.ItemByID(menuCommandIDPrefix + "togglePlay")
	if it == nil {
		t.Fatal("清空快捷键之后菜单项不见了：用户清空的是键，不是这条命令")
	}
	if it.Accelerator != "" {
		t.Errorf("清空之后菜单上还有加速键 %q：那个键会被菜单吃掉，而设置页里它已经不归这条命令了",
			it.Accelerator)
	}
	if it.Click == nil {
		t.Error("清空之后菜单项没有 Click：点了什么也不会发生")
	}
}

// TestTheMenuAndTheShortcutFileAgreeOnTheSameKey 断**菜单上的键和设置页里
// 那一格是同一个键**。
//
// ⚠️ **这是这一票的核心不变量**：菜单上的加速键是**从设置里读出来的**
// （`menuAccelFor`），不是另抄一份。而两处要是抄开了，用户按菜单上写的键
// 走的却是另一条命令。
//
// 这一条逐条比对：设置页里那八条的键，和菜单上对应那几项的加速键。
func TestTheMenuAndTheShortcutFileAgreeOnTheSameKey(t *testing.T) {
	b := newTestShell(t)
	s := b.s
	s.menu = appMenu(s)

	for _, it := range s.keys.snapshot().Shortcuts {
		menuItem := s.menu.ItemByID(menuCommandIDPrefix + it.ID)
		if menuItem == nil {
			// 不进菜单的命令（快退快进、音量）在菜单里没有对应项——那是
			// `commandSpecs` 里 `inMenu` 的判断，不在这一条的范围内。
			continue
		}
		want := toMenuAccel(accelerator(it.Shortcut))
		if !want.ok {
			continue
		}
		if menuItem.Accelerator != want.accel {
			t.Errorf("命令 %q：设置页里是 %q，菜单上是 %q——两处不一样",
				it.ID, it.Shortcut, menuItem.Accelerator)
		}
	}
}
