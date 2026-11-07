package ui

import "testing"

// TestTheEQSaveRowSitsTwelveBelowTheBands 锁定原型 index.html:798 `.pick-new { margin-top: 12px }`：
// 满屏均衡器弹层里「另存为预设」那一行离频段区 12（频段区自己下 4，所以频段读数底到输入框顶
// 16；曾经上边距是 8，即 12）。
func TestTheEQSaveRowSitsTwelveBelowTheBands(t *testing.T) {
	s, tt, _ := fullTester(t)
	openFull(t, tt)
	tt.Frame()
	if err := tt.Click("均衡器"); err != nil {
		t.Fatalf("点不了均衡器：%v（文字 %q）", err, tt.Texts())
	}
	tt.Frame()
	settleFull(t, tt)
	if !s.full.popEQ {
		t.Fatalf("满屏里没弹出均衡器弹层，文字有 %q", tt.Texts())
	}
	label, ok := tt.Find("16k")
	if !ok {
		t.Fatalf("均衡器弹层上没有 16k 读数，文字有 %q", tt.Texts())
	}
	input, ok := tt.Find("均衡器预设名称")
	if !ok {
		t.Fatalf("找不到预设名输入框，文字有 %q", tt.Texts())
	}
	// 输入框高 36，Find 给的是它自己的盒子。
	if gap := input.Y - (label.Y + label.H); gap < 15.5 || gap > 16.5 {
		t.Errorf("16k 读数底到输入框顶 %.1f，want 16（频段区下 4 + 另存为行上 12）", gap)
	}
}
