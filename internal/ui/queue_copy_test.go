// 播放列表抽屉的「复制歌名」入口（票 #685）。
//
// # 为什么抽屉也要一个
//
// 抽屉的行**没有**内容列表那一套菜单（`widgets/list` 的「复制歌曲信息」只在
// 列表 / 网格上有），而抽屉里用户同样会想把歌名复制出去（发给别人、去搜这一首）。
// 抽屉今天是唯一能看「这一串还要放什么」的地方，那里一个字都复制不出来。
//
// # 做法：末尾另起一段
//
// 抽屉的菜单五项是照 Biu `music-playlist-drawer/menu.tsx` 的 `getMenus` 抄的，
// 而 Biu 没有「复制」那一项。这一项摆在**最后**（「从列表删除」之后隔一道
// 分隔线），破坏性动作之后紧跟一个只读动作——它不打断前面那五项的顺序
// （那五项有现成的断言盯着，见 `queue_drawer_test.go`）。
package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// TestQueueRowMenuOffersCopyTitle 断**抽屉的行菜单里有「复制歌名」**。
func TestQueueRowMenuOffersCopyTitle(t *testing.T) {
	h := newHarness(t)
	h.app.Queue().Append(onlineSong("BV1", "第一首"))
	h.tt.Frame()
	openDrawer(t, h)

	if err := h.tt.RightClick("第一首"); err != nil {
		t.Fatalf("右键不了这一行：%v", err)
	}
	if !slicesContains(h.tt.Menu(), queueCopyLabel) {
		t.Errorf("菜单里没有 %q：%q", queueCopyLabel, h.tt.Menu())
	}
}

// TestChoosingCopyTitlePutsTheSongNameOnTheClipboard 断**按下去，剪贴板里就是
// 那个歌名**（逐字，没有「歌名：」前缀、没有 UP 名、没有地址）。
//
// ⚠️ **判据是剪贴板的原文**：走菜单而不是直接调函数——菜单是这一趟全部建出来
// 的，少建一项那一项就永远点不到（框架的规矩，见 `menu.go` 顶上那段）。
func TestChoosingCopyTitlePutsTheSongNameOnTheClipboard(t *testing.T) {
	h := newHarness(t)
	h.app.Queue().Append(onlineSong("BV1", "要复制的那一首"))
	h.tt.Frame()
	openDrawer(t, h)

	chooseQueueItem(t, h, "要复制的那一首", queueCopyLabel)

	if got := h.tt.Clipboard(); got != "要复制的那一首" {
		t.Errorf("剪贴板里是 %q，want 只有歌名 %q", got, "要复制的那一首")
	}
	// 说一句（静默的复制会让用户以为按键丢了）。
	if !h.tt.HasText("已复制歌名") {
		t.Errorf("复制完没有说话，文字：%q", h.tt.Texts())
	}
}

// TestQueueCopyTitleCopiesTheShownName 断**复制的是界面上显示的那个名字**。
//
// 抽屉画的是清洗过的歌名（`tclean.Clean`，见 `queue_drawer.go` 那一句），
// 复制出去的要是同一个——同 `widgets/list` 的 `TestCmdCCopiesTheCleanedTitle`。
func TestQueueCopyTitleCopiesTheShownName(t *testing.T) {
	h := newHarness(t)
	h.app.Queue().Append(onlineSong("BV1", "【4K修复】晴天"))
	h.tt.Frame()
	openDrawer(t, h)

	shown := "晴天"
	if _, ok := h.tt.Find(shown); !ok {
		t.Fatalf("抽屉里画的不是清洗过的名字 %q，文字：%q", shown, h.tt.Texts())
	}
	chooseQueueItem(t, h, shown, queueCopyLabel)

	if got := h.tt.Clipboard(); got != shown {
		t.Errorf("剪贴板里是 %q，want %q", got, shown)
	}
}

// TestQueueCmdCDoesNotStealTheClipboard 断**抽屉里按 ⌘C 不会乱复制**。
//
// 🔴 **抽屉的行不记「选中」，也不抢键盘焦点**（点一行就是「从这儿听」，
// 见 `viewQueueRow` 的 `rowEl.Clicked()`）——所以这里没有「选中的那一行」可言。
// 这一条钉的是：那种情况下 ⌘C **一个字节都不写**（剪贴板里原来是什么还是什么）。
//
// ⚠️ **和列表那一档不一样**：列表有框架的 `Selected` 游标，抽屉没有——给它
// 接一个「按 ⌘C 复制当前播放那一首」是另一种行为（用户没指着那一行），不是
// 这一票的范围。
func TestQueueCmdCDoesNotStealTheClipboard(t *testing.T) {
	h := newHarness(t)
	h.app.Queue().Append(onlineSong("BV1", "第一首"))
	h.tt.Frame()
	openDrawer(t, h)

	h.tt.SetClipboard("别处复制来的")
	h.tt.Key(mygoui.Cmd, mygoui.KeyC)
	h.tt.Frame()
	h.tt.Command("copy")
	h.tt.Frame()

	if got := h.tt.Clipboard(); got != "别处复制来的" {
		t.Errorf("抽屉里按 ⌘C 之后剪贴板变成了 %q，want 还是原来那句", got)
	}
}

// 抽屉的菜单项数：原来是 5 项，这一票另起一段加了「复制歌名」——所以是 7 项
// （五项 + 一条分隔线 + 复制歌名）。
//
// 🔴 **这一条钉的是「没有多出别的东西」**（对抗性审查的第 3 条）：原来它只断
// 「7 项 + 最后一项是复制歌名」，于是**把分隔线换成一条重复的真项，四条测试
// 全绿**。现在逐位钉住第 6 项是分隔线（`"-"`，`Tester.Menu` 对
// `MenuItemSeparator` 报的就是这个字）——那一段「另起一段」才真的是另一段。
//
// ⚠️ **前五项由 `queue_drawer_test.go` 那两条逐项比**（它们现在比的也是精确
// 的 7 项整张表）。
func TestQueueRowMenuHasTheCopyTitleAfterTheUpstreamFive(t *testing.T) {
	h := newHarness(t)
	h.app.Queue().Append(app.Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1", Title: "第一首"})
	h.tt.Frame()
	openDrawer(t, h)

	if err := h.tt.RightClick("第一首"); err != nil {
		t.Fatalf("右键不了这一行：%v", err)
	}
	got := h.tt.Menu()
	if len(got) != 7 {
		t.Fatalf("菜单有 %d 项 %q，want 7 项（上游五项 + 分隔线 + 「复制歌名」）", len(got), got)
	}
	// 🔴 **第 6 项必须是分隔线**：换成任何一条真项（哪怕是把「从列表删除」再
	// 写一遍）都是「菜单里多出一样东西」，而用户会看到两段之间没有分界。
	if got[5] != "-" {
		t.Errorf("菜单第 6 项是 %q，want 分隔线 \"-\"（全部：%q）", got[5], got)
	}
	if got[len(got)-1] != queueCopyLabel {
		t.Errorf("菜单最后一项是 %q，want %q（全部：%q）", got[len(got)-1], queueCopyLabel, got)
	}
}
