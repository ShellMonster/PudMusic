package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

// navBottomStrip 是顶栏最底下那 1 像素、内容列里那一段（左右各让 24，躲开侧栏缘线和窗口边）。
func navBottomStrip(t *testing.T, s *Shell, tt *ui.Tester) ui.Rect {
	t.Helper()
	back, ok := tt.Find(navBackLabel)
	if !ok {
		t.Fatalf("找不到后退键，文字有 %q", tt.Texts())
	}
	bottom := back.Y + back.H/2 + navMinHeight/2
	w := float32(tt.Image().Bounds().Dx())
	x := s.sideDrawnW + 24
	return ui.Rect{X: x, Y: bottom - 1, W: w - 24 - x, H: 1}
}

// TestTheTopBarRuleAppearsOnlyAfterScrolling 锁定原型 `.top.scrolled`（index.html:219-220、
// app.js:1686）：内容区在顶上时顶栏下边没有线，滚下去露出 1 像素 `line`，滚回顶上又消失。
func TestTheTopBarRuleAppearsOnlyAfterScrolling(t *testing.T) {
	tt, s, _, _ := shellWithSettings(t, app.ThemeModeDark, false)
	s.navigate(settingsPath)
	tt.Frame()
	tt.Frame()

	line := tok.Of(BuildTheme(s.app.SettingsSnapshot(), true)).Line
	strip := navBottomStrip(t, s, tt)
	half := int(strip.W / 2)
	if n := countNear(tt.Image(), strip, line); n > half {
		t.Fatalf("还没滚，顶栏底边已经有 %d/%d 个 line 色像素", n, int(strip.W))
	}

	tt.Scroll(450, 300, 0, 600)
	waitFrames(t, tt, "滚下去之后顶栏底边出现 line", func() bool {
		return countNear(tt.Image(), strip, line) > half
	})

	tt.Scroll(450, 300, 0, -4000)
	waitFrames(t, tt, "滚回顶上之后顶栏底线消失", func() bool {
		return countNear(tt.Image(), strip, line) <= half
	})
}
