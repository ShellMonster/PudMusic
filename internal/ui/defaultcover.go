package ui

import (
	"bytes"
	"image/png"
	"sync"

	"github.com/egoist/mygo/ui"
)

var defaultCoverBitmap = sync.OnceValue(func() *ui.Bitmap {
	b, err := artFile.ReadFile("art/default-cover.png")
	if err != nil {
		return nil
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil
	}
	return ui.NewBitmap(img)
})

// defaultCover 画「歌单一首有封面的歌都没有」时的默认封面（方形，圆角跟主题）。
func defaultCover(c *ui.Context, size float32) {
	defaultCoverRadius(c, size, c.Theme().Radius)
}

// defaultCoverRadius 同 defaultCover，圆角由调用方给（侧栏收藏夹那枚 32 是原型 `.fav-cover`
// 的 R4，index.html:198）。
func defaultCoverRadius(c *ui.Context, size, radius float32) {
	bm := defaultCoverBitmap()
	if bm == nil {
		return
	}
	ui.Image(c, bm).Fit(ui.Cover).Size(size, size).Radius(radius)
}
