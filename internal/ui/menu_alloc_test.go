package ui

import (
	"testing"
)

// TestTheMenuRefreshDoesNotAllocatePerFrame 断**菜单的每帧刷新不分配**。
//
// ⚠️ **为什么这一条要紧**：菜单的勾选态和可用态**每帧**要刷一次
// （`refreshMenuChecked`，见 `viewMain`）——而「每帧分配」是这个仓库盯着的
// 硬指标（`internal/ui/framebudget_test.go` 的 `liveHeapBudget`）。
//
// ⚠️ **这里曾经是每帧 1 次分配**，而且是**两处**：
//
//  1. `SettingsSnapshot()` 把整份设置值拷贝一份——而 `Settings` 里有**指针
//     字段**（音量、通知开关、迷你歌词卷帘）和一个切片，那一次拷贝会**连带
//     拷那些指针指向的东西**。
//  2. 改用窄接口之后**还有一次**：没挂 store 时调 `DefaultSettings("")`，
//     那会**造一份带新指针的完整设置**。
//
// ⇒ 修法是 `App.MenuDisplayState`（`internal/app/settings_mount.go`）：
// 只回菜单要的那两样，没挂 store 时给**写死的默认值**（那两栏的默认值是
// 两个常量，`DefaultSettings` 里也是它们）。
//
// ⚠️ **判据是「每帧 0 次分配」**——不是「不多」。分配次数是这一组里唯一
// **不受机器快慢影响**的数。
func TestTheMenuRefreshDoesNotAllocatePerFrame(t *testing.T) {
	b := newTestShell(t)
	s := b.s
	s.menu = appMenu(s)
	// 先刷一次，让内部那份「上一次的样子」和现在一致（后面量的是稳态）。
	s.refreshMenuChecked()

	allocs := testing.AllocsPerRun(200, func() {
		s.refreshMenuChecked()
	})
	if allocs > 0 {
		t.Errorf("菜单每帧刷一次，稳态每帧分配 %.1f 次，want 0 次", allocs)
	}
}

// TestTheMenuDisplayStateMatchesTheFullSnapshot 断**窄接口和整份设置读的是
// 同一个值**。
//
// ⚠️ **为什么这一条要紧**：窄接口是**另写的一份**（`MenuDisplayState`），
// 而它要是和 `SettingsSnapshot()` 读到的不一样，菜单上那个勾就会停在错的
// 那一档——而界面上那一格显示的是对的。
//
// ⚠️ **判据是「两处一样」**，不是「某一个值对」——那样一个写死的实现也能
// 通过。
func TestTheMenuDisplayStateMatchesTheFullSnapshot(t *testing.T) {
	b := newTestShell(t)
	full := b.a.SettingsSnapshot()
	mode, collapsed := b.a.MenuDisplayState()
	if mode != full.DisplayMode {
		t.Errorf("窄接口读到的显示模式是 %q，整份设置里是 %q", mode, full.DisplayMode)
	}
	if collapsed != full.SideMenuCollapsed {
		t.Errorf("窄接口读到的侧栏折叠是 %v，整份设置里是 %v", collapsed, full.SideMenuCollapsed)
	}
}
