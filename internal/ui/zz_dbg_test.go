package ui

import (
	"testing"
	"time"
)

func TestZZDbgPresence(t *testing.T) {
	s, tt, _ := fullTester(t)
	openFull(t, tt)
	tt.Advance(800 * time.Millisecond)
	before := s.builds.full
	s.noteWindow(func(p *windowPresence) { p.focused = false })
	tt.Advance(200 * time.Millisecond)
	t.Logf("放+看不见：FramePending=%v 重建了 %d 遍", tt.FramePending(), s.builds.full-before)
	// 再把歌按停（旋转整个关掉），看帧还来不来
	s.app.Pause()
	tt.Advance(200 * time.Millisecond)
	t.Logf("暂停+看不见：FramePending=%v 重建了 %d 遍", tt.FramePending(), s.builds.full-before)
}
