// 首页的画面验收（T2）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderHomeShots
//
// 深浅两套、三个 Tab 各出两张：1200×800（和原型 `ref/*-home-*.png` 同尺寸，只看折线以上）
// 和 1200×1700（整页一次看全，和原型拉高窗口截的长图对照）。数据全是合成的：封面是本机
// `httptest` 给的渐变图，歌名是示例名，不用任何真实专辑图。
package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	mygoui "github.com/egoist/mygo/ui"
)

// synthCoverJPEG 是第 seed 张合成封面：一块带对角渐变的方图，每个 seed 一种色相。
func synthCoverJPEG(seed int) []byte {
	const n = 480
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	hue := float64(seed*47%360) / 360
	r0, g0, b0 := hsv(hue, 0.55, 0.78)
	r1, g1, b1 := hsv(hue+0.08, 0.7, 0.25)
	for y := range n {
		for x := range n {
			t := float64(x+y) / float64(2*n)
			img.Set(x, y, color.RGBA{
				R: uint8(float64(r0)*(1-t) + float64(r1)*t),
				G: uint8(float64(g0)*(1-t) + float64(g1)*t),
				B: uint8(float64(b0)*(1-t) + float64(b1)*t), A: 255})
		}
	}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80})
	return buf.Bytes()
}

// hsv 把 h∈[0,1)、s、v∈[0,1] 换成 0–255 的 RGB。
func hsv(h, s, v float64) (r, g, b uint8) {
	h -= float64(int(h))
	i := int(h * 6)
	f := h*6 - float64(i)
	p, q, t := v*(1-s), v*(1-f*s), v*(1-(1-f)*s)
	var rr, gg, bb float64
	switch i % 6 {
	case 0:
		rr, gg, bb = v, t, p
	case 1:
		rr, gg, bb = q, v, p
	case 2:
		rr, gg, bb = p, v, t
	case 3:
		rr, gg, bb = p, q, v
	case 4:
		rr, gg, bb = t, p, v
	default:
		rr, gg, bb = v, p, q
	}
	return uint8(rr * 255), uint8(gg * 255), uint8(bb * 255)
}

// homeShotSongs 造 n 首合成歌：歌名「<prefix>N」，封面指向本机那台服务。
func homeShotSongs(prefix, base string, n int, plays int64) []list.Music {
	out := make([]list.Music, n)
	for i := range out {
		id := prefix + strconv.Itoa(i+1)
		out[i] = list.Music{
			ID: id, Title: prefix + " " + strconv.Itoa(i+1), Owner: "示例歌手 " + strconv.Itoa(i%7+1),
			Cover: base + "/c/" + strconv.Itoa(len(prefix)*13+i) + ".jpg", View: plays - int64(i)*plays/int64(2*n),
			Duration: time.Duration(180+i*7) * time.Second,
			PubTime:  time.Now().AddDate(0, 0, -(i%9 + 1)),
			Item:     app.Item{Title: prefix + " " + strconv.Itoa(i+1), Bvid: "BV1shot" + id},
		}
	}
	return out
}

// TestRenderHomeShots 把首页存成 PNG。
func TestRenderHomeShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seed, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/c/"), ".jpg"))
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(synthCoverJPEG(seed))
	}))
	defer srv.Close()

	for _, dark := range []bool{true, false} {
		theme := "light"
		if dark {
			theme = "dark"
		}
		for _, tab := range []struct {
			n    int
			name string
		}{{homeTabRec, "rec"}, {homeTabChart, "chart"}, {homeTabNew, "new"}} {
			for _, h := range []int{800, 2100} {
				name := fmt.Sprintf("%s-home-%s", theme, tab.name)
				if h > 800 {
					name += "-tall"
				}
				t.Run(name, func(t *testing.T) {
					b := newTestShell(t, systemIsDark(dark), at(1200, h))
					s, tt := b.s, b.tt
					cache := imgcache.New(imgcache.Options{Client: srv.Client()})
					cache.AddWake(func() {})
					t.Cleanup(cache.Close)
					s.home.cache = cache

					s.home.loads.selfMid = func() int64 { return 7 }
					s.home.loads.trend = func(id string) (int, bool) {
						n, _ := strconv.Atoi(strings.TrimPrefix(id, "榜上"))
						switch {
						case n%5 == 0:
							return 0, false
						case n%3 == 0:
							return -(n % 4) - 1, true
						}
						return n%5 + 1, true
					}
					s.home.feed.name = "阿布"
					s.home.SetSongs(homeShotSongs("推荐", srv.URL, 8, 400000),
						homeShotSongs("榜上", srv.URL, 20, 3000000),
						homeShotSongs("新歌", srv.URL, 10, 90000),
						homeShotSongs("最近", srv.URL, 10, 0))
					s.app.FavoritesOf().Replace(app.FavoritesCreated, []app.Favorite{
						{ID: 1, Title: "深夜循环", MediaCount: 9}, {ID: 2, Title: "通勤路上", MediaCount: 24},
						{ID: 3, Title: "独立与摇滚", MediaCount: 6}, {ID: 4, Title: "雨天", MediaCount: 12},
					}, nil)
					s.home.tab = tab.n
					for range 10 {
						homeFrame(tt)
						time.Sleep(60 * time.Millisecond)
					}
					settleHome(tt)
					save(t, dir, name+".png", tt)
				})
			}
		}
	}
}

// TestRenderHomeGreetWithResume 单独出问候区：暂停中且有进度时右边多一枚「继续播放」。
// 它依赖真实的播放状态（外壳每帧自己采），所以绕开外壳、直接用一份合成的 pageInput 画
// 问候区那一块。
func TestRenderHomeGreetWithResume(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{true, false} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			view := func(s *Shell) func(*mygoui.Context) {
				return func(c *mygoui.Context) {
					s.applyFrameTheme(c)
					in := pageInput{HasCurrent: true}
					in.Playback.Title = "示例歌曲"
					in.Playback.Position = 37 * time.Second
					s.home.win = 1200
					s.home.feed.name = "阿布"
					s.home.week = 12*time.Hour + 40*time.Minute
					mygoui.Column(c).FillWidth().FillHeight().Background(tok.Of(c.Theme()).Bg).
						Padding(space.PageTop, 40, 0, 40).Children(func() {
						s.home.viewGreet(c, s, 888, in, 8)
					})
				}
			}
			b := newTestShell(t, at(960, 200), systemIsDark(dark), withView(view))
			b.s.home.loads.selfMid = func() int64 { return 7 }
			for range 5 {
				b.tt.Frame()
			}
			save(t, dir, name+"-home-greet-resume.png", b.tt)
		})
	}
}
