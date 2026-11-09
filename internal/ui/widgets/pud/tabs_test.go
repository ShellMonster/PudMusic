package pud

import (
	"image"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/egoist/mygo/ui"
)

// colCover 是第 x 列在 [y0, y1) 里主色铺了多少（每个像素按「离白多远 ÷ 主色离白多远」折成 0～1，
// 一列加起来；满的 2 像素线 ≈ 2）。底是白的，所以半透明、半覆盖都按比例落进来。
func colCover(img *image.RGBA, accent ui.Color, x, y0, y1 int) float64 {
	sum := 0.0
	for y := y0; y < y1; y++ {
		px := img.RGBAAt(x, y)
		sum += float64(255-int(px.G)) / float64(255-int(accent.G))
	}
	return sum
}

// TestTheTabUnderlineGrowsOnlySideways 锁住选中下划线的进场是**只横向**展开（原型 index.html:302
// `scaleX(.3)`），竖向一直是满的 2 像素。
//
// 曾经出错的行为：进场用框架 `ui.Motion{Scale: 0.3}`，横竖一起缩，2 高的线先变成 0.6 高。
//
// 判法：进场走到早段（时刻 t），横向比例 s 由线宽 ÷ 终点线宽量出来，淡入进度 e = (s − 0.3) / 0.7
// （两者同一条曲线）。只横向缩时，中间那一列的覆盖量 = e × 终点；横竖一起缩时还要再乘 s。
// 量出来的覆盖比离哪一个模型近，就是哪一种。
func TestTheTabUnderlineGrowsOnlySideways(t *testing.T) {
	sel := 0
	var accent ui.Color
	tt := ui.NewTester(func(c *ui.Context) {
		accent = pal(c).Accent
		ui.Box(c).Size(600, 120).Background(ui.RGB(255, 255, 255)).Padding(20).Children(func() {
			Tabs(c, "tabs", &sel, []Tab{{Label: "推荐推荐推荐"}, {Label: "榜单榜单榜单"}})
		})
	}, 600, 120)
	tt.Frame()
	r, ok := tt.Find("榜单榜单榜单")
	if !ok {
		t.Fatalf("没找到第二个 Tab，文字有 %q", tt.Texts())
	}
	if err := tt.Click("榜单榜单榜单"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	// `Find` 按无障碍名字找到的是整枚 Tab 键（含上下 12 的内边距）；下划线贴着键的下沿
	// （Bottom -1），只扫下沿上下 4 像素，碰不到字形。
	y0, y1 := int(r.Y+r.H)-4, int(r.Y+r.H)+4
	cx := int(r.X + r.W/2)
	// 底线（整行 1 像素 line）在下划线之外也有，拿 Tab 右边远处的一列当基线扣掉。
	base := func(img *image.RGBA) float64 { return colCover(img, accent, int(r.X+r.W)+60, y0, y1) }
	measure := func() (width int, center float64) {
		img := tt.Image()
		b := base(img)
		center = colCover(img, accent, cx, y0, y1) - b
		for x := int(r.X) - 8; x < int(r.X+r.W)+8; x++ {
			if colCover(img, accent, x, y0, y1)-b > 0.5*center {
				width++
			}
		}
		return width, center
	}

	tt.Advance(motion.Med / 5)
	tt.Frame()
	midW, midC := measure()
	for i := 0; i < 8 && tt.FramePending(); i++ {
		tt.Advance(100 * time.Millisecond)
		tt.Frame()
	}
	endW, endC := measure()
	if endW == 0 || endC < 1 {
		t.Fatalf("走完之后没量到下划线（宽 %d、中列覆盖 %.2f）", endW, endC)
	}
	s := float64(midW) / float64(endW)
	if s <= tabBarEnterScaleX-0.05 || s >= 0.97 {
		t.Fatalf("早段线宽 %d / 终点 %d = %.2f——该在 0.3～1 之间（取样时刻不对，测不出东西）", midW, endW, s)
	}
	e := (s - tabBarEnterScaleX) / (1 - tabBarEnterScaleX)
	got := midC / endC
	sideways, uniform := e, e*s
	if diff(got, sideways) >= diff(got, uniform) {
		t.Errorf("早段中列覆盖比 %.2f：更像横竖一起缩（%.2f）而不是只横向（%.2f）；横向比例 %.2f",
			got, uniform, sideways, s)
	}
}

func TestTabsUseFrameworkKeyboardNavigation(t *testing.T) {
	selected := 0
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Size(400, 80).Children(func() {
			Tabs(c, "tabs", &selected, []Tab{{Label: "推荐"}, {Label: "榜单"}, {Label: "动态"}})
		})
	}, 400, 80)

	tt.Frame()
	if err := tt.Click("推荐"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyRight)
	if selected != 1 {
		t.Fatalf("Right selected tab %d, want 1", selected)
	}
	tt.Key(0, ui.KeyEnd)
	if selected != 2 {
		t.Fatalf("End selected tab %d, want 2", selected)
	}
	tt.Key(0, ui.KeyHome)
	if selected != 0 {
		t.Fatalf("Home selected tab %d, want 0", selected)
	}
}

func diff(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}
