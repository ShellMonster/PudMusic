// 波形那一格的出图：深浅各一套。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderWaveShots
//
// # 为什么单独出这一张
//
// 波形是**唯一一处每帧都在动**的界面元素（它的相位是拿钟算的），而这一票
// (#314) 动的是它**跨帧要用的那一份状态**（本地存储的键）——那一份状态
// 动错了，画面上的症状是「波形不跳了」或者「波形每一帧都从头开始跳」。
//
// ⚠️ **深浅两套都要出**：波形是主色画的，而主色在浅底和深底上认得出的程度
// 不一样——只出一套等于没验这一条。
//
// # ⚠️ 这一张**不画整窗**
//
// 画整窗的话那一格波形只有几个像素，看不出「四根条子是不是错开四分之一轮」。
// 这里单独画那一格，窗口也小一点——存出来就是那一格本身。
package ui

import (
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/hover"
	mygoui "github.com/egoist/mygo/ui"
)

// TestRenderWaveShots 把「正在放」那个跳动波形存成 PNG，深浅各一套。
func TestRenderWaveShots(t *testing.T) {
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
			// ⚠️ **壳要带 `systemIsDark`**：不带的话「深色」那一档存出来和
			// 「浅色」一模一样（`hover_shot_test.go` 那一族踩过这个）。
			//
			// ⚠️ **窗口要小**：这一张存的是那一格波形本身，不是整窗。
			b := newTestShell(t, at(240, 120), systemIsDark(dark),
				withView(func(s *Shell) func(*mygoui.Context) {
					return func(c *mygoui.Context) {
						// 主题照常装（铁律一）：波形是主色画的，而主色来自设置。
						s.applyFrameTheme(c)
						mygoui.Box(c).Size(240, 120).Children(func() {
							// 波形画宽一点（列表里是 40，这里 120）：存出来的那
							// 几根条子才看得出「四根错开四分之一轮」。
							hover.Wave(c, "wave", 120, c.Theme().Accent, "甲")
						})
					}
				}))
			b.tt.Frame()
			save(t, dir, "35-波形-"+name+".png", b.tt)
		})
	}
}
