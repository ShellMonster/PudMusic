package ui

// 联想面板的**外观**（2026-11 重设计，照用户给的参考图）：
//
//   - 两档标题带图标：「历史搜索」前一只钟、「搜索联想」前一枚放大镜，右上角是
//     垃圾桶 +「清空全部」；
//   - 联想词每一行左边一枚放大镜；
//   - 命中的那几个字用**主色当字色**那一档（`ink.AccentOn`，随用户主色现算、对底
//     ≥4.5:1），不再是上游写死的粉 #F54180。
//
// 判据是画面：图标用「字左边那一段里有没有次要色的墨迹」量（和「清空全部」那枚
// 垃圾桶同一个办法，见 navbar_search_form_test.go）；命中色用「命中段里有没有主色
// 墨迹、后缀里没有」量。

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

// openSuggestWithTerm 敲一个词、等联想词回来、把进场推完。只配一条联想词
// 「周杰伦」：命中段「周杰」和后缀「伦」各是一个独立的 Text，`Find` 能分别找到。
func openSuggestWithTerm(t *testing.T, s *Shell, tt *ui.Tester) {
	t.Helper()
	typeInSearch(t, tt, "周杰")
	settleSuggestDebounce(s, tt)
	waitFrames(t, tt, "联想词那一档出来", func() bool { return tt.HasText("伦") })
	settleSuggestEnter(t, tt)
}

// leftOf 是某段字左边那一格（图标该在的位置）：图标 `iconSm` 加一道缝。
func leftOf(r ui.Rect) ui.Rect {
	return ui.Rect{X: r.X - (iconSm + 10), Y: r.Y, W: iconSm + 6, H: r.H}
}

// TestTheSuggestHitIsPaintedInTheAccentInk 钉住命中段的字色是 `ink.AccentOn`。
func TestTheSuggestHitIsPaintedInTheAccentInk(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			f := newBiliFixture(t, 1)
			f.setSuggestTerms("周杰", "周杰伦")
			s, tt := newSuggestShell(t, f, systemIsDark(dark))
			tt.Frame()
			openSuggestWithTerm(t, s, tt)

			hit, ok := tt.Find("周杰")
			if !ok {
				t.Fatalf("浮层上没有命中段「周杰」，文字有 %q", tt.Texts())
			}
			post, ok := tt.Find("伦")
			if !ok {
				t.Fatalf("浮层上没有后缀「伦」，文字有 %q", tt.Texts())
			}
			th := BuildTheme(s.app.SettingsSnapshot(), dark)
			accent := tok.Of(th).AccentText
			if n := countNear(tt.Image(), hit, accent); n == 0 {
				t.Errorf("命中段「周杰」里一个主色（%v）像素都没有——命中段没有染主色", accent)
			}
			if n := countNear(tt.Image(), post, accent); n != 0 {
				t.Errorf("后缀「伦」里有 %d 个主色像素——染色染过了头", n)
			}
			pink := ui.Color{R: 0xF5, G: 0x41, B: 0x80, A: 255}
			if n := countNear(tt.Image(), hit, pink); n != 0 {
				t.Errorf("命中段里还有 %d 个上游那个粉（#F54180）的像素——该换成主题主色了", n)
			}
		})
	}
}
