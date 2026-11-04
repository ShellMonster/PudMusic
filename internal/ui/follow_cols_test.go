// 「我的关注」页的结构：左边分组栏宽 200、和右边隔 40（原型 `.split`），窗口 ≤ 1024 时变单列。
package ui

import "testing"

// TestTheFollowGroupColumnIs200WideAndSplitGapIs40 钉住分组栏的两个数。
func TestTheFollowGroupColumnIs200WideAndSplitGapIs40(t *testing.T) {
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	uiFollowList(b, 2, []any{uiFollowUser(11, "第一个 UP"), uiFollowUser(12, "第二个 UP")})
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 0, []any{})

	s, tt, _ := newSpaceShell(t, b, true, "77", at(1400, 900))
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "人到了", func() bool { return len(f.users) == 2 && f.tagStarted })
	tt.Frame()

	group, ok := tt.Find("全部关注")
	if !ok {
		t.Fatalf("画面上找不到分组栏的「全部关注」，文字有 %q", tt.Texts())
	}
	// 分组按钮撑满分组栏（200 宽）。
	if group.W != followGroupW {
		t.Errorf("分组按钮宽 %.1f，want %.0f（`.group-col` 200）", group.W, followGroupW)
	}
	card, ok := tt.Find("打开 第一个 UP 的空间")
	if !ok {
		t.Fatalf("画面上找不到第一张卡，文字有 %q", tt.Texts())
	}
	if gap := card.X - (group.X + group.W); absF(gap-40) > 0.5 {
		t.Errorf("分组栏和卡片区之间是 %.1f，want 40（`.split` 的 gap）", gap)
	}
}

// TestTheFollowSplitTurnsIntoOneColumnOnNarrowWindows 钉住窗口 ≤ 1024 时的单列：分组栏摆到
// 卡片区**上面**（横着一排），不再占左边那一栏。
func TestTheFollowSplitTurnsIntoOneColumnOnNarrowWindows(t *testing.T) {
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	uiFollowList(b, 2, []any{uiFollowUser(11, "第一个 UP"), uiFollowUser(12, "第二个 UP")})
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 0, []any{})

	s, tt, _ := newSpaceShell(t, b, true, "77", at(1000, 800))
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "人到了", func() bool { return len(f.users) == 2 && f.tagStarted })
	tt.Frame()

	group, ok := tt.Find("全部关注")
	if !ok {
		t.Fatalf("画面上找不到分组栏的「全部关注」，文字有 %q", tt.Texts())
	}
	card, ok := tt.Find("打开 第一个 UP 的空间")
	if !ok {
		t.Fatalf("画面上找不到第一张卡，文字有 %q", tt.Texts())
	}
	if card.Y <= group.Y+group.H {
		t.Errorf("卡片在 y=%.1f，分组按钮底边在 %.1f——窄窗口下分组栏该在卡片上面", card.Y, group.Y+group.H)
	}
}
