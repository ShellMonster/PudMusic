package ui

import (
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/egoist/mygo/ui"
)

func TestRenderSkeletonSweepShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			_, tt := newShell(t, systemIsDark(dark), withView(func(s *Shell) func(*ui.Context) {
				return func(c *ui.Context) {
					ui.Column(c).FillWidth().Gap(24).Padding(32).Children(func() {
						s.viewSpaceHeaderSkeleton(c, "shot:space", 40)
						p := &pagedRows{layout: list.ViewCard, width: 900}
						p.drawSkeleton(c, "正在加载音乐列表…")
						searchUserSkeleton(c)
					})
				}
			}))
			tt.Frame()
			save(t, dir, name+"-skeleton-sweep.png", tt)
		})
	}
}
