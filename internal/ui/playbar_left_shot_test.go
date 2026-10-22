package ui

import (
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	mygoui "github.com/egoist/mygo/ui"
)

// TestRenderPlayBarLeftShots 存图：播放条左边重排之后的样子，深浅各一套。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderPlayBarLeftShots
//
// # 这一族要看的是「左边那一块的三段」
//
//	封面 │ 歌名·UP名 │ 分集·收藏·赞（横着并排的一行）
//
// ⚠️ **深浅两套都要出**：那一段的底色跟着主题走，而三枚键的图形在浅色下是
// 灰的、深色下是浅灰——只看一套的话，「键在不在」这件事根本看不出来。
//
// ⚠️ **必须有歌**：没歌的时候第三段一格都不画（`viewTrackActions` 第一行就
// return），而这一票要看的正是那三枚键。
//
// ⚠️ **用的是多 P 那一份**：分集那一枚只在多 P 时摆（`viewPartButton`），而
// 「三枚横排」这句话要三枚都在才看得出「横排」。
//
// ⚠️ **只存播放条那 88 高**：整窗的截图在 `chrome_shot_test.go` 的 01-整窗-
// 那一族里已经有了，这一族看的是播放条**自己**。
func TestRenderPlayBarLeftShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			s, tt := onlineTester(t, multiPages(), false, at(1200, 560))
			partsReady(t, s, tt, 3)
			// ⚠️ `onlineTester` 固定深色（它那一族测的是深色下的行为），
			// 这里要两套，所以自己翻回去——主题是**每帧**现读的（`applyFrameTheme`）。
			tt.SetDark(dark)
			tt.Frame()

			saveCrop(t, dir, "01-播放条-左边三段-"+name+".png", tt, 560-playbarHeight, playbarHeight)

			// 02 **最小窗口**（900）：那一档歌名最短（两个多字），而进度行离
			// 左列右端最近——「三段有没有互相压住」在这一档最看得出来。
			small, stt := onlineTester(t, multiPages(), false, at(minWindowW, minWindowH))
			partsReady(t, small, stt, 3)
			stt.SetDark(dark)
			stt.Frame()
			saveCrop(t, dir, "02-播放条-最小窗口-"+name+".png", stt, minWindowH-playbarHeight, playbarHeight)
		})
	}
}

// saveCrop 存画面下面 h 高那一段（从 y0 起）。
//
// 播放条在真窗口里是**贴着底**的，而整窗的截图对「左边那一块」来说太杂——
// 所以这一族只存播放条自己那一段。
func saveCrop(t *testing.T, dir, name string, tt *mygoui.Tester, y0, h int) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	img := tt.Image()
	b := img.Bounds()
	if y0 < 0 {
		y0 = 0
	}
	if y0+h > b.Dy() {
		h = b.Dy() - y0
	}
	sub := image.NewRGBA(image.Rect(0, 0, b.Dx(), h))
	draw.Draw(sub, sub.Bounds(), img, image.Pt(0, y0), draw.Src)

	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, sub); err != nil {
		t.Fatal(err)
	}
	t.Logf("存了 %s", filepath.Join(dir, name))
}
