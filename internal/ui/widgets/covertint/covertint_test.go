package covertint

import (
	"image"
	"image/color"
	"testing"
)

func solid(c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := range 32 {
		for x := range 32 {
			img.Set(x, y, c)
		}
	}
	return img
}

// 一张蓝色封面取出来的色相是蓝的，灰封面取不出颜色。
func TestADominantHueComesFromTheColoredPixels(t *testing.T) {
	h, ok := Dominant(solid(color.RGBA{30, 80, 220, 255}))
	if !ok || h.H < 210 || h.H > 235 {
		t.Fatalf("蓝色封面取出 %+v ok=%v，色相应在 210~235", h, ok)
	}
	if _, ok := Dominant(solid(color.RGBA{128, 128, 128, 255})); ok {
		t.Error("灰色封面不该取出颜色")
	}
	if _, ok := Dominant(solid(color.RGBA{250, 250, 250, 255})); ok {
		t.Error("近白封面不该取出颜色")
	}
}

// 同一个色相在浅色主题里取暗一点、深色主题里取亮一点，保证压在背景上读得出来。
func TestTheTintIsDarkerOnLightThemesAndBrighterOnDarkOnes(t *testing.T) {
	h := Hue{H: 200, S: 0.9}
	light, dark := h.Color(false), h.Color(true)
	if light == dark || light[0] != '#' || len(light) != 7 {
		t.Fatalf("light=%q dark=%q", light, dark)
	}
}
