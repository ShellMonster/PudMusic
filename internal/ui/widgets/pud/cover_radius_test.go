package pud

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

// TestEveryCoverSlotHandsItsRadiusToTheInnermostImage 锁住「组件把自己框的圆角交给画图的那一层」：
// 卡片 / 视频卡 8、曲目行 4（原型 `.t-cover`）、头像半边长。
//
// 曾经出错的行为：组件只在外框上 `Radius(r).Clip()`，里面的图圆角给 0（或主题的 8）；GPU 后端
// 嵌套裁剪只有最内层切圆角，外框只按矩形裁，于是真机上卡片封面是直角、曲目行封面是 8 而不是 4。
// 无头截图的 CPU 光栅把两层覆盖率相乘，看不出这件事，所以这里不比像素，只看传进去的圆角。
func TestEveryCoverSlotHandsItsRadiusToTheInnermostImage(t *testing.T) {
	got := map[string]float32{}
	rec := func(name string) Cover {
		return func(c *ui.Context, w, h, radius float32) {
			got[name] = radius
			ui.Box(c).Size(w, h).Radius(radius)
		}
	}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Children(func() {
			CoverCard(c, "cover", "封面卡", "", 120, rec("CoverCard"), false)
			ColCard(c, "col", "收藏夹卡", "", 120, rec("ColCard"))
			VideoCard(c, "video", Video{Title: "视频卡", Cover: rec("VideoCard")}, 160)
			TrackRow(c, "track", Track{Title: "曲目行", Cover: rec("TrackRow")}, 900)
			Avatar(c, "头像", 56, ui.Color{}, rec("Avatar"))
		})
	}, 1000, 900)
	tt.Frame()

	want := map[string]float32{
		"CoverCard": tok.R8,
		"ColCard":   tok.R8,
		"VideoCard": tok.R8,
		"TrackRow":  tok.R4,
		"Avatar":    28,
	}
	for name, r := range want {
		g, ok := got[name]
		if !ok {
			t.Errorf("%s 没有调用封面回调", name)
			continue
		}
		if g != r {
			t.Errorf("%s 交给最内层图的圆角 = %v，想要 %v", name, g, r)
		}
	}
}
