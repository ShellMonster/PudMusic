// 时间轴的出图材料：已缓冲（轨道下细线）和弹幕热度（轨道上柱子）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTimelineShots
package ui

import (
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/egoist/mygo/ui"
)

func TestRenderTimelineShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	b := newTestShell(t, at(760, 140), systemIsDark(true), withView(func(s *Shell) func(*ui.Context) {
		return func(c *ui.Context) {
			ui.Column(c).Padding(20).Children(func() {
				s.viewProgress(c, app.State{
					HasTrack: true,
					Duration: 4 * time.Minute,
					Position: time.Minute,
					Buffered: 150 * time.Second,
				})
			})
		}
	}))
	b.a.SetDanmakuForTest([]app.DanmakuItem{
		{ID: "a", TimeMS: 0},
		{ID: "b", TimeMS: 10_000},
		{ID: "c", TimeMS: 12_000},
		{ID: "d", TimeMS: 13_000},
		{ID: "e", TimeMS: 62_000},
		{ID: "f", TimeMS: 63_000},
		{ID: "g", TimeMS: 64_000},
		{ID: "h", TimeMS: 65_000},
		{ID: "i", TimeMS: 66_000},
		{ID: "j", TimeMS: 150_000},
	})
	b.tt.Frame()
	save(t, dir, "dark-timeline.png", b.tt)
}
