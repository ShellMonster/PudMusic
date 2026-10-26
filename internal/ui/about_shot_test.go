// 关于面板的画面验收（票 #539）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderAboutPanelShots
//
// **深浅两套都出**（DESIGN §8）：弹窗面板的底色、描边和那两档字色在深浅两套
// 里不是同一个数，只看浅色的话深色下那几行字压不住。
//
// ⚠️ **出图是材料，审查才是那道门**（AGENTS.md §1）：这一份图是拿来对着
// DESIGN.md 核那五项的（布局 / 间距 / 层级 / 密度 / 对齐），核完要把差在
// 哪儿一条一条写进收尾说明。
package ui

import (
	"os"
	"testing"
)

// TestRenderAboutPanelShots 把关于面板存成 PNG。
func TestRenderAboutPanelShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}

	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := aboutTestShell(t, "0.1.0", systemIsDark(dark))
			b.s.openAbout()
			b.tt.Frame()
			if !b.tt.HasText(aboutRepoLabel) {
				t.Fatalf("面板没开出来，文字有 %q", b.tt.Texts())
			}
			save(t, dir, "关于面板-"+name+".png", b.tt)
		})
	}
}
