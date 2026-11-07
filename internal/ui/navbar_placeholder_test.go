package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
)

// TestTheSearchPlaceholderIsTextThree 锁定原型 index.html:236 `input::placeholder { color: var(--text-3) }`：
// 空着的搜索框里占位字画成 text-3（框架默认是 TextMuted = text-2，曾经就是那一档）。
func TestTheSearchPlaceholderIsTextThree(t *testing.T) {
	s, tt := newShell(t, systemIsDark(true))
	tt.Move(900, 500)
	tt.Frame()
	r, ok := tt.Find(searchLabel)
	if !ok {
		t.Fatalf("顶栏上没有搜索框，文字有 %q", tt.Texts())
	}
	if got := s.hotwordPlaceholder(); got != searchPlaceholder {
		t.Fatalf("没热词时占位是 %q，want %q", got, searchPlaceholder)
	}
	p := tok.Of(BuildTheme(s.app.SettingsSnapshot(), true))
	if n := countNear(tt.Image(), r, p.Text3); n == 0 {
		t.Error("搜索框里一个 text-3 像素都没有——占位字没画成 text-3")
	}
	// 占位字最亮也就是 text-3；有 text-2 那么亮的像素，说明还在用框架默认色。
	if n := countNear(tt.Image(), r, p.Text2); n != 0 {
		t.Errorf("搜索框里有 %d 个 text-2 像素——占位字还是框架默认的 TextMuted", n)
	}
}
