package ui

import (
	"fmt"
	"image"
	"image/color"
	"testing"

	mygoui "github.com/egoist/mygo/ui"
)

// inkPos 找出 box 里「离底色远得明显」的像素（图形的那一圈）。
func inkPos(img *image.RGBA, box mygoui.Rect, bg color.RGBA, slack int) [][2]int {
	var out [][2]int
	for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(box.Y+box.H), img.Rect.Max.Y); y++ {
		for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
			p := img.RGBAAt(x, y)
			if absInt(int(p.R)-int(bg.R)) > slack || absInt(int(p.G)-int(bg.G)) > slack ||
				absInt(int(p.B)-int(bg.B)) > slack {
				out = append(out, [2]int{x, y})
			}
		}
	}
	return out
}

func modeAt(img *image.RGBA, pos [][2]int) color.RGBA {
	var s []color.RGBA
	for _, p := range pos {
		s = append(s, img.RGBAAt(p[0], p[1]))
	}
	if len(s) == 0 {
		return color.RGBA{}
	}
	return modeOf(s)
}

func TestDbgDisabledInk(t *testing.T) {
	for _, dark := range []bool{false, true} {
		tt, s := playbarTester(t, fullBarState(), 1200, 0, dark)
		bg := rgb(BuildTheme(s.app.SettingsSnapshot(), dark).Background)
		btn, _ := tt.Find("上一首")
		tt.Move(600, 20)
		tt.Frame()
		idle := tt.Image()
		tt.Move(btn.X+btn.W/2, btn.Y+btn.H/2)
		tt.Frame()
		lit := tt.Image()

		fmt.Printf("dark=%v bg=%v\n", dark, bg)
		fmt.Printf("  idle inkColor=%v\n", inkColor(idle, btn, bg))
		fmt.Printf("  lit  inkColor=%v\n", inkColor(lit, btn, bg))
		for _, slack := range []int{20, 30, 50} {
			pos := inkPos(idle, btn, bg, slack)
			fmt.Printf("  slack=%d n=%d idleMode=%v litMode=%v\n", slack, len(pos), modeAt(idle, pos), modeAt(lit, pos))
		}
	}
}
