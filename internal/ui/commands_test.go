// 命令表（票 #713）。
//
// 这一族断的是**表的形状**和**表与设置页的一致性**——不是「点了有没有反应」
// （那是菜单那几族的活）。
//
// ⚠️ **判据不是「菜单上画出了什么」**：菜单是系统画的（AppKit 的 NSMenu），
// 它不在窗口的画面里，截不到也量不到（见 `internal/ui/menu.go` 顶上那一段）。
package ui

import (
	"slices"
	"testing"
)

// TestEveryShortcutCommandIsRegistered 断**设置页的每一条命令都在命令表里**。
//
// ⚠️ **这一条是这一层的地基**：设置页存的是 id，而菜单项按 id 去查那张表。
// 少登记一条 ⇒ 菜单上那一项点了什么也不发生（或者压根不出现），
// 而**界面上一个字都不提**。
//
// 判据是**双向**：表里有而设置页没有的（那是菜单上会出现一个按了没反应的项），
// 和设置页有而表里没有的（那是设置页能录、菜单上找不到）——两个方向都要断。
func TestEveryShortcutCommandIsRegistered(t *testing.T) {
	def := defaultShortcutSettings()
	for _, it := range def.Shortcuts {
		if _, ok := commandByID(it.ID); !ok {
			t.Errorf("设置页有命令 %q，命令表里没有：菜单上找不到它", it.ID)
		}
	}
	for _, c := range commandSpecs {
		if !hasShortcutCommand(def.Shortcuts, c.id) {
			t.Errorf("命令表有命令 %q，设置页里没有：菜单上会有一个按了没反应的项", c.id)
		}
	}
}

// TestTheCommandNamesMatchTheSettingsPage 断**两处的名字一样**。
//
// ⚠️ **为什么这条要紧**：同一个命令在设置页叫「播放/暂停」而在菜单里叫
// 「播放」，用户会以为是两件事——尤其是在「菜单栏里找播放/暂停」的时候。
func TestTheCommandNamesMatchTheSettingsPage(t *testing.T) {
	def := defaultShortcutSettings()
	for _, it := range def.Shortcuts {
		c, ok := commandByID(it.ID)
		if !ok {
			continue // 上面那一条已经报了
		}
		if c.name != it.Name {
			t.Errorf("命令 %q：命令表里叫 %q，设置页里叫 %q——同一个命令两个名字",
				it.ID, c.name, it.Name)
		}
	}
}

// TestTheMenuCommandsAreDerivedFromTheMenuItself 断**「在不在菜单里」由菜单树
// 自己说了算**。
//
// ⚠️ **为什么这条要紧**：这里原来有一个**手写的**「进不进菜单」标志，而那个
// 标志**和菜单实况脱节过**——「快退 5 秒」「快进 5 秒」「音量加」「音量减」
// 四条明明在菜单里，标志却写着不进，于是 `refreshMenuAccel` **不刷它们**。
//
// ⇒ 而脱节的后果很具体：菜单的加速键**先于窗口**拿到那个键（框架自己的
// E2E：`mygo/internal/e2e/menu_shortcuts_windows_test.go:14`），所以菜单上
// 留着一个旧键 ⇒ **那个旧键会被菜单吃掉**，而设置页里它已经不归那条命令了
// ⇒ 用户按了「键失灵」。
//
// 这一条钉的是「**菜单里有的命令项，都会被刷新到**」。
func TestTheMenuCommandsAreDerivedFromTheMenuItself(t *testing.T) {
	b := newTestShell(t)
	s := b.s
	s.menu = appMenu(s)

	got := menuCommandIDs(s.menu)
	if len(got) == 0 {
		t.Fatal("菜单里一个命令项都没有")
	}
	// ⚠️ **每一条都要在菜单树里能找到**：被算成「在菜单里」却找不到，那
	// 「刷新」刷的是一个不存在的东西。
	for _, id := range got {
		if s.menu.ItemByID(menuCommandIDPrefix+id) == nil {
			t.Errorf("命令 %q 被算成「在菜单里」，但菜单树里找不到它", id)
		}
	}
	// ⚠️ **那几条命令都要在菜单里**——它们是「手写标志脱节」时漏掉的那几条。
	for _, id := range []string{"togglePlay", "prev", "next", "seekBack", "seekForward", "volumeUp", "volumeDown"} {
		if !slices.Contains(got, id) {
			t.Errorf("命令 %q 不在菜单里，但菜单上应该有它（它的加速键要跟着设置走）", id)
		}
	}
}

// TestTheCommandOrderIsTheOrderTheUserActsIn 断**表的顺序**。
//
// ⚠️ **顺序就是菜单里的顺序**（`menu.go` 按它摆「播放」那一栏），而菜单栏的
// 顺序是用户的肌肉记忆——播放/暂停、上一首、下一首这个顺序在哪家播放器里
// 都是这样。
func TestTheCommandOrderIsTheOrderTheUserActsIn(t *testing.T) {
	want := []string{"togglePlay", "prev", "next", "toggleMiniMode", "seekBack", "seekForward", "volumeUp", "volumeDown",
		"commandPalette", "favoriteCurrent", "cyclePlayMode", "toggleMainWindow", "toggleQueueDrawer"}
	got := idsOf(commandSpecs)
	if len(got) != len(want) {
		t.Fatalf("命令表有 %d 条（%v），want %d 条（%v）", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("命令表第 %d 条是 %q，want %q（整表：%v）", i+1, got[i], want[i], got)
		}
	}
}

// TestAnUnknownCommandFallsBackToItsID 断**查不到的时候回落成 id**。
//
// ⚠️ **为什么不是空串**：一个空名字的菜单项在 macOS 上是一个点得动但看不出
// 是什么的东西。而设置页的快捷键文件里可能有我们不认识的 id（用户从更新的
// 版本改回来的，或者手写坏的）——那一项照样要有个能认出来的名字。
func TestAnUnknownCommandFallsBackToItsID(t *testing.T) {
	if got := commandName("noSuchCommand"); got != "noSuchCommand" {
		t.Errorf("查不到的命令名字是 %q，want 它的 id", got)
	}
}

// idsOf 是一组命令的 id 列表。
func idsOf(cs []commandSpec) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.id)
	}
	return out
}

// hasShortcutCommand 报这条命令在不在这份快捷键列表里。
func hasShortcutCommand(list []shortcutItem, id string) bool {
	for _, it := range list {
		if it.ID == id {
			return true
		}
	}
	return false
}

// TestTheMenuCanReadEverythingItNeedsToShow 断**菜单要读的那些状态都拿得到**。
//
// ⚠️ **为什么单独断这一条**：菜单里的勾选态（播放模式、显示模式、侧栏折叠）
// 要**运行期刷新**——那些状态随时会变。菜单项不能自己去问应用层的内部结构，
// 只能读那几个公开的取法。
//
// 这一条钉住的是「菜单读的那几样都有一处拿得到」，不是「菜单读对了」——后者是
// 菜单那几族的活（#715）。
func TestTheMenuCanReadEverythingItNeedsToShow(t *testing.T) {
	b := newTestShell(t)

	// 播放模式：菜单的「播放模式」那一档要读它。
	if got := b.a.PlayMode(); got == "" {
		t.Error("读不到播放模式：菜单的「播放模式」那一档读不到")
	}

	// 显示模式：菜单的「列表 / 网格 / 紧凑」那一组要读它。
	if got := b.a.SettingsSnapshot().DisplayMode; got == "" {
		t.Error("读不到显示模式：菜单的「显示」栏读不到")
	}

	// 侧栏折叠：菜单的「显示/隐藏侧栏」那一项要读它。
	if b.a.SettingsSnapshot().SideMenuCollapsed {
		t.Error("刚建好的外壳侧栏是折叠的：新建的设置不该是折叠态")
	}
}
