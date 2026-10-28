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

// TintSide 是取色那一趟要的那张小图的**长边**有多少像素。
//
// # 为什么是 96
//
// 它是量出来的（60 张真封面，等比缩放后与原图取色的色相偏差）：
//
//	长边 48 → max 31.52°  avg 1.68°  ← 开始飘
//	长边 64 → max 15.96°  avg 1.04°
//	长边 96 → max  8.27°  avg 0.90°  ← 收住了
//	长边 200 → max 7.91°  avg 0.80°  ← 再大只是白花下载和解码
//
// 96 那一档够小（解码 0.26ms，而原图 276ms~1375ms），也够稳。
const TintSide = 96

// scalingFor 给出这个封面地址该拼的**缩放参数**，空串表示不用拼。
//
// # 两套语法，各按各的来
//
//	网易云（`music.126.net`）  `?param=WxH`   —— 查询参数
//	B 站图床（`hdslb.com`）     `@WxH`          —— 路径后缀
//
// 别把 B 站那一套套到网易云上（它的图床不认），也别反过来。
//
// # ⚠️ **B 站那一套不能带 `_1c`**
//
// `_1c` 是**中心裁剪**：切掉画面两边，把图裁成正方形。而歌单封面本来就是
// 横图（B 站默认 16:9），主色常常在边上——中心裁剪之后取到的可能是另一个
// 颜色。实测 60 张真封面：
//
//	中心裁正方（`@96w_96h_1c`）  max |Δhue| 172.9°  avg 10.88°  ← 另一个颜色
//	中心裁 16:9（`@96w_54h_1c`）  max |Δhue|  61.5°  avg  4.18°
//	等比缩放  （`@96w_96h`）      max |Δhue|   8.27° avg  0.90°  ← 要这个
//
// B 站图床的语义已实测确认：`@96w_96h` 给的是 96×54（原图 1672×940，
// 等比缩放），`@96w_96h_1c` 给的是 96×96（中心裁剪）。
//
// # 已经带过参数的地址**原样返回**
//
// 图床自己裁好的那一档（`…jpg@672w_378h_1c.webp`）再拼一次就成了两个
// `@`——那是坏地址，返回 HTTP 400。
func scalingFor(cover string) string {
	if cover == "" {
		return ""
	}
	u, err := url.Parse(cover)
	if err != nil {
		return ""
	}
	// 网易云：查询参数。⚠️ **已经带过查询串的不动它**（那可能是别的参数）。
	if strings.Contains(u.Host, "music.126.net") {
		if u.RawQuery != "" {
			return ""
		}
		return "param=48y48"
	}
	// B 站图床：路径后缀。
	//
	// ⚠️ **只认 `/bfs/`**：B 站图床的图都在那条路径下，而别的域名（用户头像
	// 挂在 `/bfs/face/` 下面，但也有走别的路径的）不认这一套参数——拼上去
	// 就是坏地址。
	if !strings.Contains(u.Host, "hdslb.com") || !strings.Contains(u.Path, "/bfs/") {
		return ""
	}
	if strings.Contains(u.Path, "@") {
		return ""
	}
	return fmt.Sprintf("@%dw_%dh", TintSide, TintSide)
}

// Fetch 下载封面的小图并取主色调。
func Fetch(ctx context.Context, client *http.Client, cover string) (Hue, bool, error) {
	u, err := url.Parse(cover)
	if err != nil {
		return Hue{}, false, err
	}
	// 按图床各自的语法要一张**小图**——取一个颜色不需要原图，而原图解码要
	// 几百毫秒到一秒多（理由见 `scalingFor`）。
	switch s := scalingFor(cover); {
	case s == "":
		// 不该拼的地址：原样取。
	case strings.HasPrefix(s, "@"):
		// `@` 在 URL 的 Path 里不会被转义（`url.URL.String` 只转义
		// 保留字符之外的），拼在 Path 末尾就行。
		u.Path += s
	default:
		// 网易云那一套是查询参数。
		u.RawQuery = s
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
