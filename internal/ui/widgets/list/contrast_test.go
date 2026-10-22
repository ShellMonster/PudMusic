package list

import (
	"image"
	"image/color"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/egoist/mygo/ui"
)

// 「正在播放」那一行的歌名是**主色当字**：浅色下要一个与底色 ≥4.5:1 的变体。
//
// 这一条量的是屏幕上的像素，不是 `rowColors` 的返回值：返回对了而没画上去、
// 或者画到了别的层上，同一种「读不出来」用户一样看得见。主色换几个再量一遍
// ——写死一个绿的实现在「主色是红色」那一次会红。
func TestThePlayingTitleIsReadableAsText(t *testing.T) {
	accents := []string{
		"#1ed760", // Biu 的绿（默认）
		"#ff0000", // 深红
		"#00ffff", // 青
		"#808080", // 灰
	}
	for _, hex := range accents {
		t.Run(hex, func(t *testing.T) {
			th := ui.LightTheme()
			th.Accent = ui.Hex(hex)
			// AccentText 的算法照 `internal/ui/theme.go` 的 `applyAccent`：
			// 主色够亮就用深色字。列表这一行用不到它，但主题是一整份。
			if l := 0.2126*float32(th.Accent.R) + 0.7152*float32(th.Accent.G) +
				0.0722*float32(th.Accent.B); l > 165 {
				th.AccentText = ui.Color{R: 24, G: 24, B: 27, A: 255}
			}

			songs := onlineSongs(3)
			rec := &recorded{}
			l := New(newCache(t), playingActions(rec, songs[1].Item.Identity()))
			l.SetSongs(songs)
			tt := ui.NewTester(func(c *ui.Context) {
				c.SetTheme(th)
				l.View(c)
			}, 400, 300)
			tt.Frame()

			r, ok := tt.Find("在线的歌 B")
			if !ok {
				t.Fatalf("画面上找不到「在线的歌 B」，文字有 %q", tt.Texts())
			}
			bg := tt.Image().RGBAAt(2, 2)
			want := ink.AccentOn(th)
			text := nearestTo(tt.Image(), r, want)
			if got := ratioOf(text, bg); got < 4.5 {
				t.Errorf("正在播放那一行的歌名 %v 压底色 %v 只有 %.2f:1（< 4.5）",
					text, bg, got)
			}
		})
	}
}

// TestTheDarkPlayingTitleKeepsTheAccent 钉住「深色下不变」：深色下那个绿原样用，
// 压深灰是 9.2:1，已经够。
func TestTheDarkPlayingTitleKeepsTheAccent(t *testing.T) {
	songs := onlineSongs(3)
	rec := &recorded{}
	l := New(newCache(t), playingActions(rec, songs[1].Item.Identity()))
	l.SetSongs(songs)
	th := ui.DarkTheme()
	th.Accent = ui.Hex("#1ed760")
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(th)
		l.View(c)
	}, 400, 300)
	tt.Frame()

	r, ok := tt.Find("在线的歌 B")
	if !ok {
		t.Fatalf("画面上找不到「在线的歌 B」，文字有 %q", tt.Texts())
	}
	want := ui.Hex("#1ed760")
	text := nearestTo(tt.Image(), r, want)
	// 深色下那个绿是 (30,215,96)；抗锯齿会掺进底色，取「离它最近」的那一点
	// 就是它。
	if d := colorDist(text, color.RGBA{R: want.R, G: want.G, B: want.B}); d > 30 {
		t.Errorf("深色下正在播放的歌名是 %v，want 主色原样 %v（差 %d）", text, want, d)
	}
}

// nearestTo 在这一行里找离 want 最近的那个像素。
//
// ⚠️ **不能取「离底色最远」**：`Find` 给回来的是**整行**（行的无障碍名是歌名），
// 同一行里还有白色的 UP 名——深色下「离底色最远」挑中的是那个白字。也不能按
// 「最绿」挑：主色是红色时它一个像素都挑不中。按**期望的那个颜色**找最近的一笔，
// 对任何主色都成立。
func nearestTo(img *image.RGBA, r ui.Rect, want ui.Color) color.RGBA {
	target := color.RGBA{R: want.R, G: want.G, B: want.B, A: 255}
	best, bestD := color.RGBA{}, 1<<30
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			if !(image.Point{x, y}).In(img.Rect) {
				continue
			}
			c := img.RGBAAt(x, y)
			if d := colorDist(c, target); d < bestD {
				best, bestD = c, d
			}
		}
	}
	return best
}

// ratioOf 是两个像素的 WCAG 对比度。
func ratioOf(a, b color.RGBA) float64 {
	toUI := func(c color.RGBA) ui.Color { return ui.Color{R: c.R, G: c.G, B: c.B, A: 255} }
	return ink.Ratio(toUI(a), toUI(b))
}

// colorDist 是两个像素的通道差之和。
func colorDist(a, b color.RGBA) int {
	d := func(x, y uint8) int {
		if x > y {
			return int(x) - int(y)
		}
		return int(y) - int(x)
	}
	return d(a.R, b.R) + d(a.G, b.G) + d(a.B, b.B)
}
