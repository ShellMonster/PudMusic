package platform

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"sync"
)

// macOS 菜单栏的模板图：黑色 + 透明，系统按菜单栏明暗着色。36×36（18pt@2x）。
//
// 图形是一个音符：实心的符头、竖杆、一道旗。4×4 超采样抗锯齿。

const trayTemplateSize = 36

var (
	trayTemplateOnce sync.Once
	trayTemplatePNG  []byte
)

// TrayTemplatePNG 返回菜单栏模板图的 PNG。
func TrayTemplatePNG() []byte {
	trayTemplateOnce.Do(func() {
		const ss = 4
		n := trayTemplateSize
		img := image.NewNRGBA(image.Rect(0, 0, n, n))
		for y := 0; y < n; y++ {
			for x := 0; x < n; x++ {
				hit := 0
				for sy := 0; sy < ss; sy++ {
					for sx := 0; sx < ss; sx++ {
						fx := (float64(x) + (float64(sx)+0.5)/ss) / float64(n)
						fy := (float64(y) + (float64(sy)+0.5)/ss) / float64(n)
						if inNote(fx, fy) {
							hit++
						}
					}
				}
				if hit > 0 {
					img.SetNRGBA(x, y, color.NRGBA{A: uint8(255 * hit / (ss * ss))})
				}
			}
		}
		var buf bytes.Buffer
		if png.Encode(&buf, img) == nil {
			trayTemplatePNG = buf.Bytes()
		}
	})
	return trayTemplatePNG
}

// inNote 判断单位方格里的点 (x,y) 在不在音符里。
func inNote(x, y float64) bool {
	// 符头：偏转的椭圆。
	hx, hy := x-0.36, y-0.74
	c, s := math.Cos(-0.45), math.Sin(-0.45)
	rx, ry := hx*c-hy*s, hx*s+hy*c
	if (rx*rx)/(0.20*0.20)+(ry*ry)/(0.14*0.14) <= 1 {
		return true
	}
	// 竖杆：从符头右侧一直到顶。
	if x >= 0.50 && x <= 0.59 && y >= 0.14 && y <= 0.74 {
		return true
	}
	// 旗：杆顶向右下弯出去的一道。
	if x >= 0.59 && x <= 0.84 && y >= 0.14 && y <= 0.52 {
		t := (x - 0.59) / 0.25
		top := 0.14 + 0.30*t*t
		return y >= top && y <= top+0.17*(1-0.35*t)
	}
	return false
}
