package imgcache

import (
	"github.com/egoist/mygo/ui"
)

// View 画一张网络图：好了画图，没好画占位，失败画失败态。
//
// 这是缓存唯一的界面出口，所有要显示网络图的地方都走它——三种状态怎么画、
// 要占多大地方，只在这里定一次。
//
// ⚠️ 调用方必须先用 Cache.AddWake 注册一路唤醒，否则图下完之后界面会永远
// 停在占位（实测：只投递不唤醒 = 0 帧刷新）。Shell.New 里已经注册了。
func View(c *ui.Context, cache *Cache, url string, w, h float32) ui.Element {
	// 圆角跟着主题走，不写死数字：Biu 那边的 radius="md" 也是跟着主题的，
	// 不是常量。主题的 Radius 来自设置里的 app.Settings.BorderRadius
	// （ui.BuildTheme 里落到 t.Radius），用户在设置页调圆角时封面跟着变。
	return ui.Box(c).Size(w, h).Radius(c.Theme().Radius).Clip().Children(func() {
		drawCover(c, cache, url, w, h)
	})
}

// drawCover 是 View 里面的那一支，分成四种情形。
func drawCover(c *ui.Context, cache *Cache, url string, w, h float32) {
	t := c.Theme()

	// 没有地址：画个空框。压根没有东西可显示，不是「加载失败」。
	if url == "" {
		ui.Column(c).Size(w, h).Background(t.Surface)
		return
	}

	switch st := cache.Get(url); {
	case st.Ready():
		ui.Image(c, st.Bitmap).Fit(ui.Cover).Size(w, h)
	case st.Loading:
		// 占位：Biu 在图没来的时候是一个带边框的空框（`components/image` 的
		// emptyPlaceholder 分支）。这里用底色 + 一行小字，别让用户以为界面卡了。
		ui.Column(c).Size(w, h).Center().Background(t.Surface).Children(func() {
			ui.Text(c, "载入中").FontSize(11).TextColor(t.TextMuted)
		})
	default:
		ui.Column(c).Size(w, h).Center().Background(t.Surface).Children(func() {
			ui.Text(c, "封面加载失败").FontSize(11).TextColor(t.TextMuted)
		})
	}
}
