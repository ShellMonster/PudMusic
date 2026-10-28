package ui

import (
	"math"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/egoist/mygo/ui"
)

// TestTheFontKnobShowsTheStoredSizeUnderTextScale 钉住「滑块显示的是**存下来的
// 那个字号**，不是乘过 TextScale 的那个」。
//
// 评审量到过的坏法：存值 20、系统文本 1.25 倍时，浮层里的滑块拿 `scale.Font(20)=25`
// 当值——拇指停在 25，拖一格（step 1）到 26 就把 26 存回去，下一帧歌词按 32.5 画、
// 拇指跳到 32.5，而底下那行标签还写着「字号 26 px」。三样东西三个数。
//
// 判据是**拇指画在哪儿**：按存值算出来的位置和缩放后的位置差 12.8 像素（滑块高
// 112、thumb 20、范围 12~48），一量就分得开。
func TestTheFontKnobShowsTheStoredSizeUnderTextScale(t *testing.T) {
	s, tt := lyricShell(t)
	putLyrics(s, lyricsOfN(20), "")
	settleLyrics(t, s, tt)
	// 系统文本调到 1.25 倍，再把画面画一帧——偏好在帧里读，晚一帧就对不上了。
	tt.SetPreferences(ui.Preferences{TextScale: 1.25})
	tt.Frame()

	openLyricPopover(t, s, tt, lyricFontKnobName)

	r, ok := tt.Find(lyricFontSliderKey)
	if !ok {
		t.Fatalf("字号滑块没有摆出来，文字有 %q", tt.Texts())
	}
	stored := float64(s.app.Lyrics().FontSize)

	// 圆钮是整行宽的一条主色，找出所有接近主色的像素，取它们 y 的中心。
	accent := BuildTheme(s.app.SettingsSnapshot(), false).Accent
	img := tt.Image()
	sumY, n := 0.0, 0
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			if distRGBA(img.RGBAAt(x, y), accent) < 20 {
				sumY += float64(y)
				n++
			}
		}
	}
	if n == 0 {
		t.Fatalf("滑块里没找到主色圆钮（r=%v）", r)
	}
	cy := sumY / float64(n)

	at := (stored - app.MinLyricsFontSize) / (app.MaxLyricsFontSize - app.MinLyricsFontSize)
	at = math.Max(0, math.Min(1, at))
	trackY := float64(r.Y) + lyricSliderThumb/2
	trackH := float64(r.H) - lyricSliderThumb
	want := trackY + trackH*(1-at)

	if math.Abs(cy-want) > 3 {
		t.Errorf("存值 %.0f 时圆钮在 y=%.1f，按存值该在 %.1f（差 %.1f）——"+
			"滑块用的是缩放后的字号（TextScale 1.25 下会算成 %.0f）",
			stored, cy, want, cy-want, stored*1.25)
	}

	// 拖一格：存值只能动 1（step），不该按缩放后的值跳。
	dragLyricSliderTo(t, tt, lyricFontSliderKey, float32(stored+1))
	settleLyrics(t, s, tt)
	if got := s.app.Lyrics().FontSize; got != float32(stored+1) {
		t.Errorf("拖一格之后存下来的字号是 %v，want %v", got, stored+1)
	}
}
