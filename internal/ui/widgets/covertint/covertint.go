// Package covertint 从一张封面里取一个能当主题主色的颜色。
package covertint

import (
	"context"
	"fmt"
	"image"
	_ "image/jpeg" // 封面是 jpeg / png
	_ "image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Hue 是封面里的主色调：色相（0~360）和饱和度（0~1）。亮度由主题自己定，所以这里不给。
type Hue struct{ H, S float64 }

// Dominant 取一张图的主色调：按饱和度加权平均色相，灰和近黑近白的像素几乎不算。
// 整张图都没颜色（黑白封面）时返回 false。
func Dominant(img image.Image) (Hue, bool) {
	b := img.Bounds()
	var x, y, weight, sat float64
	step := max(1, min(b.Dx(), b.Dy())/48)
	for py := b.Min.Y; py < b.Max.Y; py += step {
		for px := b.Min.X; px < b.Max.X; px += step {
			r, g, bl, _ := img.At(px, py).RGBA()
			h, s, l := hsl(float64(r)/65535, float64(g)/65535, float64(bl)/65535)
			w := s * s * (1 - math.Abs(2*l-1))
			a := h * math.Pi / 180
			x, y = x+w*math.Cos(a), y+w*math.Sin(a)
			weight, sat = weight+w, sat+w*s
		}
	}
	if weight < 0.5 {
		return Hue{}, false
	}
	h := math.Atan2(y, x) * 180 / math.Pi
	if h < 0 {
		h += 360
	}
	return Hue{H: h, S: sat / weight}, true
}

// Fetch 下载封面的小图并取主色调。
func Fetch(ctx context.Context, client *http.Client, cover string) (Hue, bool, error) {
	u, err := url.Parse(cover)
	if err != nil {
		return Hue{}, false, err
	}
	// 网易云图片服务按 ?param=WxH 缩，取一张小的就够了。
	if strings.Contains(u.Host, "music.126.net") && u.RawQuery == "" {
		u.RawQuery = "param=48y48"
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Hue{}, false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Hue{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Hue{}, false, fmt.Errorf("covertint: HTTP %d", resp.StatusCode)
	}
	img, _, err := image.Decode(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Hue{}, false, err
	}
	h, ok := Dominant(img)
	return h, ok, nil
}

// Color 把色相配上一个适合 dark / 浅色主题的亮度，返回 #rrggbb。
// 饱和度夹在 0.45~0.8：太灰的封面也要有点颜色，太艳的会刺眼。
func (h Hue) Color(dark bool) string {
	s := min(max(h.S, 0.45), 0.8)
	l := 0.40
	if dark {
		l = 0.64
	}
	r, g, b := rgb(h.H, s, l)
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

func hsl(r, g, b float64) (h, s, l float64) {
	mx, mn := max(r, g, b), min(r, g, b)
	l = (mx + mn) / 2
	d := mx - mn
	if d == 0 {
		return 0, 0, l
	}
	s = d / (1 - math.Abs(2*l-1))
	switch mx {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h, s, l
}

func rgb(h, s, l float64) (uint8, uint8, uint8) {
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	f := func(v float64) uint8 { return uint8(math.Round((v + m) * 255)) }
	return f(r), f(g), f(b)
}
