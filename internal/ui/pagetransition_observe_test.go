package ui

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/hover"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/shimmer"
	mygoui "github.com/egoist/mygo/ui"
)

// 这一组是**出图**用的（票 #469）：把「路由换页有没有过渡」这件事**拍成帧**，
// 让人能一张一张对着看，而不是只读一行数字。
//
// ⚠️ **它不是回归测试**：帧是给人看的，判据在 `router_transition_test.go`
// 那一组（那里量的是像素差，不依赖机器负载）。
//
// 出图靠 `BIU_PAGE_TRANSITION_SHOTS` 这个环境变量指定目录；没设就跳过。
// 两种情况都出：**开着动画**（`TransitionAuto`，也就是框架默认的那条兜底，
// 也就是这一票查出来的「它一直在跑」）和**关掉**（生产那一档）。
//
// 用法：
//
//	BIU_PAGE_TRANSITION_SHOTS=/tmp/shots go test ./internal/ui/ -run TestPageTransitionShots -v

// observeShell 造一个**走生产路**的外壳（不改生产的任何取值）。
func observeShell(t *testing.T, dark bool) (*Shell, *mygoui.Tester) {
	t.Helper()
	orig := coverCacheDir
	coverCacheDir = func() string { return t.TempDir() }
	t.Cleanup(func() { coverCacheDir = orig })

	flag := hover.NewVisibleFlag()
	hover.SetVisibleFlag(flag)
	shimmer.SetVisibleFlag(flag)
	a := app.New()
	s := New(a)
	tt := mygoui.NewTester(s.View, 1200, 800)
	tt.SetDark(dark)
	t.Cleanup(s.Close)
	return s, tt
}

func savePNG(t *testing.T, path string, img *image.RGBA) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// TestPageTransitionShots 出图：换页的每一帧。
func TestPageTransitionShots(t *testing.T) {
	out := os.Getenv("BIU_PAGE_TRANSITION_SHOTS")
	if out == "" {
		t.Skip("设 BIU_PAGE_TRANSITION_SHOTS 才会出图")
	}

	cases := []struct {
		name string
		from string
		to   string
	}{
		{"进深", "/", "/settings"},
		{"平级", "/settings", "/local-music"},
		{"退栈", "/settings", "/"},
	}

	for _, dark := range []bool{false, true} {
		theme := "浅色"
		if dark {
			theme = "深色"
		}
		for _, anim := range []bool{true, false} {
			mode := "开着动画"
			if !anim {
				mode = "关掉动画"
			}
			for _, c := range cases {
				s, tt := observeShell(t, dark)
				if anim {
					// 故意打开框架默认的那条兜底（生产那一档是「不做」）。
					s.router.Transition = mygoui.TransitionAuto
				}
				dir := filepath.Join(out, theme+"-"+mode+"-"+c.name)

				s.router.Push(c.from)
				tt.Frame()
				savePNG(t, filepath.Join(dir, "00-出发.png"), copyImg(tt.Image()))

				s.router.Push(c.to)
				tt.Frame()
				prev := copyImg(tt.Image())
				savePNG(t, filepath.Join(dir, "01-换页那一帧.png"), prev)

				for i := 1; i <= 20; i++ {
					tt.Advance(16 * time.Millisecond)
					tt.Frame()
					cur := copyImg(tt.Image())
					savePNG(t, filepath.Join(dir, fmt.Sprintf("%02d-t%03dms.png", i+1, i*16)), cur)
					t.Logf("%s·%s·%s t=%3dms  与上一帧差 %7d 像素  pending=%v",
						theme, mode, c.name, i*16, diffImg(prev, cur), tt.FramePending())
					prev = cur
				}
			}
		}
	}
}

// diffImg 报两幅图不一样的像素数。
func diffImg(a, b *image.RGBA) int {
	n := 0
	for y := 0; y < a.Rect.Dy() && y < b.Rect.Dy(); y++ {
		for x := 0; x < a.Rect.Dx() && x < b.Rect.Dx(); x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				n++
			}
		}
	}
	return n
}

// copyImg 复制一幅图（`tt.Image()` 给的是内部那张，下一帧就被覆盖了）。
func copyImg(src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, src.Rect.Dx(), src.Rect.Dy()))
	for y := 0; y < src.Rect.Dy(); y++ {
		for x := 0; x < src.Rect.Dx(); x++ {
			dst.Set(x, y, src.RGBAAt(x, y))
		}
	}
	return dst
}
