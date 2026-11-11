// 播放条「已缓冲」那一截的出图材料：深浅两套。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderPlaybarBufferedShots
package ui

import (
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/egoist/mygo/ui"
)

func TestRenderPlaybarBufferedShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		b := newTestShell(t, at(760, 140), systemIsDark(dark), withView(func(s *Shell) func(*ui.Context) {
			return func(c *ui.Context) {
				s.applyFrameTheme(c)
				ui.Column(c).Padding(20).Children(func() {
					s.viewProgress(c, app.State{
						HasTrack: true,
						Duration: 4 * time.Minute,
						Position: time.Minute,
						Buffered: 150 * time.Second,
						Chapters: []app.Chapter{
							{At: 45 * time.Second, End: 90 * time.Second, Title: "第一段"},
							{At: 150 * time.Second, End: 210 * time.Second, Title: "第二段"},
						},
					})
				})
			}
		}))
		b.tt.Frame()
		save(t, dir, "播放条-已缓冲-"+name+".png", b.tt)
	}
}
