package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestLongResumeTitleIsClippedAndAvailableOnHover 断长续播文案不会撑破右侧列，
// 但悬停时仍能看到完整文案。
func TestLongResumeTitleIsClippedAndAvailableOnHover(t *testing.T) {
	const inner = float32(888)
	const name = "一首标题特别特别特别特别特别特别特别特别特别特别特别特别长的歌曲"
	label := "继续播放「" + name + "」"

	b := newTestShell(t, at(960, 200), withView(func(s *Shell) func(*ui.Context) {
		return func(c *ui.Context) {
			s.applyFrameTheme(c)
			in := pageInput{HasCurrent: true}
			in.Playback.Title = name
			in.Playback.Position = 37 * time.Second
			s.home.win = 960
			s.home.week = 12*time.Hour + 40*time.Minute
			s.home.viewGreet(c, s, inner, in, 8)
		}
	}))
	for range 3 {
		b.tt.Frame()
	}

	r, ok := b.tt.Find(label)
	if !ok {
		t.Fatalf("续播按钮没有画出来，文字有 %q", b.tt.Texts())
	}
	maxWidth := min(520, inner*0.46)
	if r.W > maxWidth+1 {
		t.Fatalf("长续播按钮宽 %.1f 超过上限 %.1f", r.W, maxWidth)
	}

	b.tt.Move(r.X+r.W/2, r.Y+r.H/2)
	b.tt.Frame()
	count := 0
	for _, text := range b.tt.Texts() {
		if strings.Contains(text, label) {
			count++
		}
	}
	if count < 2 {
		t.Fatalf("悬停后没有出现完整续播文案，文字有 %q", b.tt.Texts())
	}
	if dir := os.Getenv("BIU_SHOT_DIR"); dir != "" {
		save(t, dir, "home-long-resume-tooltip.png", b.tt)
	}
}
