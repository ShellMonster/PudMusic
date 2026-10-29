package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// 三档「背景清晰度」各出一张图（票 #666）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderBackdropClarityShots
//
// # 为什么要出图
//
// 这一票改的是**画布比例 + 模糊半径 + 源图尺寸**三样联动的东西。三档之间
// 差多少、「高清」到底值不值，只有看图才知道。
//
// ⚠️ **出图是材料，审查才是那道门**（AGENTS.md §1）：对着**同一张封面**在三档
// 下的样子比——要能看出「流畅」的细节糊没了、「高清」看得出封面是什么。
//
// ⚠️ **封面要有结构**（不是纯色）：纯色在哪个清晰度下都是纯色，三档出图会
// 一模一样，视觉审查就核不出东西。
func TestRenderBackdropClarityShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	body := structuredCoverPNG(t, 1200)
	for _, c := range []struct {
		name    string
		clarity app.BackdropClarity
	}{
		{"流畅", app.BackdropClaritySmooth},
		{"清晰", app.BackdropClarityClear},
		{"高清", app.BackdropClarityHD},
	} {
		t.Run(c.name, func(t *testing.T) {
			// ⚠️ **清晰度要在建外壳的时候就带上**（走 `withSettings` 那条生产路）：
			// 背景是「封面变了或尺寸变了才重画」，而设置页那一格改完也是靠
			// 「下一帧重画」生效——出图要拍到用户真的会看到的那一帧。
			//
			// ⚠️ 而 `fullTesterWithCoverPNG` 用的是**没挂设置**的基座，所以这里
			// 照它的样子搭一遍、但把设置带上。
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Write(body)
			}))
			t.Cleanup(srv.Close)

			st := app.DefaultSettings("")
			st.BackdropClarity = c.clarity
			s, tt := newShell(t, withSettings(st))
			// 深色：满屏那几层的层次（压暗层、毛玻璃、白字）在深色下看得最清。
			tt.SetDark(true)
			item := app.LocalItem(flacPath(t))
			item.Cover = srv.URL + "/cover.png"
			if err := s.app.Enqueue(item); err != nil {
				t.Fatalf("把歌放进队列：%v", err)
			}
			tt.Frame()
			waitFrames(t, tt, "封面下回来了", func() bool {
				return s.images.Get(s.app.Snapshot().CoverURL).Ready()
			})
			openFull(t, tt)
			waitFrames(t, tt, "背景糊好了", func() bool { return s.full.bgDraws > 0 })
			save(t, dir, "背景清晰度-"+c.name+".png", tt)
		})
	}
}

// structuredCoverPNG 画一张有结构的封面：四象限不同色 + 斜条纹。
//
// ⚠️ **斜条纹是「糊掉多少」的刻度**：条纹还在 = 清楚，条纹糊成渐变 = 糊了。
func structuredCoverPNG(t *testing.T, side int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			var c color.RGBA
			switch {
			case x < side/2 && y < side/2:
				c = color.RGBA{220, 60, 40, 255}
			case x >= side/2 && y < side/2:
				c = color.RGBA{40, 160, 80, 255}
			case x < side/2 && y >= side/2:
				c = color.RGBA{50, 90, 220, 255}
			default:
				c = color.RGBA{230, 200, 60, 255}
			}
			if ((x+y)/24)%2 == 0 {
				c = color.RGBA{c.R / 2, c.G / 2, c.B / 2, 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
