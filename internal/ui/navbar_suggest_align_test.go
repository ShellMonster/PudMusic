package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
)

// TestTheSuggestSectionsHaveNoExtraGap 锁定原型 index.html:239 / app.js:494：历史那排 chip 下面
// 是 12 的内边距，下一段标题自带上 4，中间不再多插一个 8 高的空盒（曾经段间是 12+8+4=24）。
func TestTheSuggestSectionsHaveNoExtraGap(t *testing.T) {
	f := newBiliFixture(t, 1)
	f.setSuggestTerms("周杰", "周杰伦")
	s, tt := newSuggestShell(t, f)
	s.hist.add("林俊杰")
	tt.Frame()
	openSuggestWithTerm(t, s, tt)

	chip, ok := tt.Find("林俊杰")
	if !ok {
		t.Fatalf("浮层上没有历史 chip，文字有 %q", tt.Texts())
	}
	head, ok := tt.Find(suggestTermsHeading)
	if !ok {
		t.Fatalf("浮层上没有「%s」标题，文字有 %q", suggestTermsHeading, tt.Texts())
	}
	chipBottom := chip.Y + chip.H/2 + tok.ChipH/2
	if d := head.Y - chipBottom; d < 15 || d > 17 {
		t.Errorf("chip 底到下一段标题字顶 %.1f，want 16（12 + 标题上 4）", d)
	}
}

// TestTheHistoryChipHoverMatchesThePrototype 锁定原型 index.html:242-243 `.chip`：悬停整枚 chip
// 换 `press` 底（曾经只换字色），文字到 × 之间 6（曾经 4）。
func TestTheHistoryChipHoverMatchesThePrototype(t *testing.T) {
	f := newBiliFixture(t, 1)
	s, tt := newSuggestShell(t, f, systemIsDark(true))
	s.hist.add("林俊杰")
	tt.Frame()
	if err := tt.Click(searchLabel); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "历史 chip 出来", func() bool { _, ok := tt.Find("林俊杰"); return ok })
	settleSuggestEnter(t, tt)

	label, _ := tt.Find("林俊杰")
	del, ok := tt.Find("删除「林俊杰」")
	if !ok {
		t.Fatalf("chip 上没有删除键，文字有 %q", tt.Texts())
	}
	if d := del.X - (label.X + label.W); d < 5.5 || d > 6.5 {
		t.Errorf("文字右缘到 × 左缘 %.1f，want 6（原型 .chip gap）", d)
	}

	th := BuildTheme(s.app.SettingsSnapshot(), true)
	p := tok.Of(th)
	x, y := clampX(tt.Image(), int(label.X)-4), int(label.Y+label.H/2)
	idle := tt.Image().RGBAAt(x, y)
	if maxDiff(idle, rgb(p.Surface2)) > 3 {
		t.Fatalf("没悬停时 chip 底是 %v，want surface-2 %v", idle, rgb(p.Surface2))
	}
	tt.Move(label.X+label.W/2, label.Y+label.H/2)
	tt.Frame()
	hot := tt.Image().RGBAAt(x, y)
	want := over(p.Press, p.Surface)
	if maxDiff(hot, want) > 3 {
		t.Errorf("悬停时 chip 底是 %v，want press 压在 surface 上 %v", hot, want)
	}
}
