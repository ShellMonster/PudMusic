package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/egoist/mygo/ui"
)

func TestPageHeadTitleDoesNotTooltipItsWhitespace(t *testing.T) {
	b := newTestShell(t, at(1200, 500), withView(func(s *Shell) func(*ui.Context) {
		return func(c *ui.Context) {
			s.applyFrameTheme(c)
			pud.PageHead(c, "Feed", "用户动态", "关注的 UP 主最近分享了什么。", 1200, nil)
		}
	}))

	b.tt.Frame()
	title, ok := b.tt.Find("用户动态")
	if !ok {
		t.Fatalf("页头没有找到标题，文字有 %q", b.tt.Texts())
	}

	b.tt.Move(title.X+title.W-2, title.Y+title.H/2)
	b.tt.Frame()

	count := 0
	for _, text := range b.tt.Texts() {
		if text == "用户动态" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("鼠标停在页头标题右侧空白处时出现了 tooltip，标题出现 %d 次，文字有 %q",
			count, b.tt.Texts())
	}
}
