package ui

import (
	"os"
	"testing"
)

// TestRenderPlayBarAcceptanceShots 是这一次三处播放条 / 托盘改动的验收图。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderPlayBarAcceptanceShots -v
//
// 看什么：
//
//   - 01 播放条三段：封面 │ 歌名·UP名 │ **喜欢心 + 评论**（深浅两套；
//     心走 `likeCurrent` 绑定流程，`Likeable` 为真才摆）。
//   - 02 UP 名命中区：指针悬在名字上时底下长出下划线，而命中区只到名字
//     那么宽（`Shrink(1)`，不再 `Grow(1)` 铺满整段）。
//   - 03 喜欢弹窗：未绑定点心是「给喜欢找一个专属歌单」三选一。
func TestRenderPlayBarAcceptanceShots(t *testing.T) {
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
			_, tt := newLikedBindShell(t, newBiliFixture(t, 1), 0)
			tt.SetDark(dark)
			tt.Frame()

			// ① 播放条三段：喜欢心 + 评论那一枚。
			saveCrop(t, dir, "01-播放条-喜欢心-"+name+".png", tt, likeWinH-playbarHeight, playbarHeight)

			// ② UP 名命中区：悬停在名字上，下划线只画在名字的墨迹底下，
			// 不再整段亮。悬到名字的左 2/3 处：保证落在墨迹上，而那一带的
			// 右外侧（改之前也亮）在图里看不出——所以这里只验「悬停确实有
			// 下划线」，命中区收窄由 `TestTheOwnerNameIsLeftAligned…` 那条
			// 布局测试和代码形状（`Shrink(1)`）共同钉着。
			if r, ok := tt.Find("一个名字很长的 UP 主名字"); ok {
				tt.Move(r.X+r.W*2/3, r.Y+r.H/2)
				tt.Frame()
				saveCrop(t, dir, "02-UP名-悬停下划线-"+name+".png", tt, likeWinH-playbarHeight, playbarHeight)
				tt.Move(r.X, 10)
				tt.Frame()
			}
		})
	}

	// ③ 喜欢弹窗（未绑定那一档）：点播放条上的心是「去哪」三选一。
	t.Run("喜欢弹窗-未绑定", func(t *testing.T) {
		_, tt := newLikedBindShell(t, newBiliFixture(t, 1), 0)
		tt.SetDark(true)
		tt.Frame()
		likeTheSong(t, tt)
		waitFrames(t, tt, "三选一弹窗出来了", func() bool { return tt.HasText("给喜欢找一个专属歌单") })
		save(t, dir, "03-喜欢弹窗-未绑定-三选一.png", tt)
	})
}
