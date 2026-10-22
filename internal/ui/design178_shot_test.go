// 票 #178 的画面验收：页面首屏那一枚居中的转圈，和按钮「在忙」那一档。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderDesign178Shots
//
// **深浅两套都出**（DESIGN §8）：深色下那一枚转圈是次要色（灰），在深底上
// 比在浅底上更淡，而按钮那一档的「灰下去」在两套里也不是同一个数。
package ui

import (
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestRenderDesign178Shots 把加载态和「在忙」的按钮存成 PNG，留给人眼和上游
// 并排看。
func TestRenderDesign178Shots(t *testing.T) {
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
			// ── 搜索页的首屏：那一枚转圈落在结果区里 40vh 那一格 ─────────
			b := newTestShell(t, blockedBiliOpts(t, true)...)
			b.tt.SetDark(dark)
			b.s.navigate(searchPath + "?q=" + "%E5%91%A8%E6%9D%B0%E4%BC%A6")
			b.tt.Frame()
			b.tt.Frame()
			b.tt.Frame()
			b.tt.Move(1180, 770)
			b.tt.Frame()
			save(t, dir, "99-加载态-搜索页-转圈-"+name+".png", b.tt)

			// ── 稍后再看的首屏：同一枚转圈（同一份画法）──────────────────
			lb := newTestShell(t, blockedBiliOpts(t, true)...)
			lb.tt.SetDark(dark)
			lb.s.navigate(laterPath)
			lb.tt.Frame()
			lb.tt.Frame()
			lb.tt.Frame()
			lb.tt.Move(1180, 770)
			lb.tt.Frame()
			save(t, dir, "100-加载态-稍后再看-转圈-"+name+".png", lb.tt)

			// ── 按钮「在忙」那一档：转起来 + 灰下去 ─────────────────────
			bb := newTestShell(t, at(900, 400), systemIsDark(dark), withView(func(s *Shell) func(*ui.Context) {
				return func(c *ui.Context) {
					s.applyFrameTheme(c)
					ui.Row(c).Gap(20).Padding(30).AlignItems(ui.Center).Children(func() {
						ui.PrimaryButton(c, "上传")
						busyPrimaryButton(c, "上传中…")
						ui.PrimaryButton(c, "提交")
						busyPrimaryButton(c, "提交中…")
					})
				}
			}))
			bb.tt.Frame()
			save(t, dir, "101-按钮在忙-转起来与灰下去-"+name+".png", bb.tt)
		})
	}
}
