// 喜欢心形的视觉验收图：播放条上那一枚在深浅两套主题、实心 / 空心两档下的样子。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderLikedHeartStates
//
// 画的就是播放条用的那一枚 `pud.IconBtn`（`heart` / `heart-on`，空心次要色、
// 实心主色），铺在页面底色上——浅色和深色下都要读得出来。
package ui

import (
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	mygoui "github.com/egoist/mygo/ui"
)

// heartShotView 是「只摆这一枚心」的那一帧。
func heartShotView(filled bool) func(*Shell) func(*mygoui.Context) {
	return func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.applyFrameTheme(c)
			p := tok.Of(c.Theme())
			name, label := "heart", "喜欢"
			if filled {
				name, label = "heart-on", "取消喜欢"
			}
			mygoui.Column(c).Fill().Background(p.Bg).Center().Children(func() {
				pud.IconBtn(c, "shot-heart", name, label, pud.IconOpts{Row: true, On: filled})
			})
		}
	}
}

// TestRenderLikedHeartStates 出四张图：浅色 / 深色 × 空心 / 实心。
func TestRenderLikedHeartStates(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		theme := "light"
		if dark {
			theme = "dark"
		}
		for _, filled := range []bool{false, true} {
			state := "outline"
			if filled {
				state = "filled"
			}
			t.Run(theme+"-"+state, func(t *testing.T) {
				b := newTestShell(t, at(160, 120), systemIsDark(dark), withView(heartShotView(filled)))
				b.tt.Frame()
				for range 30 {
					if !b.tt.FramePending() {
						break
					}
					b.tt.Advance(16e6)
				}
				save(t, dir, "liked-heart-"+theme+"-"+state+".png", b.tt)
			})
		}
	}
}
