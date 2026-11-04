// 票 #244 的画面验收：设置页两端对齐（D1）、迷你窗音量滑块的悬停提示（B5）、
// 「全部播放」两处画法统一（B6）、超长歌名不占满整行中段（E1）、深色下黑封面
// 有边（E2）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket244Shots
//
// # 深浅两套都要出
//
// 这一票改的每一处**在深浅其中一套上才看得出来**：封面上那一圈边在深色下才
// 是「一道边界」（浅色下白封面和行底本来就接近）；迷你窗那枚提示是**反色底**，
// 两套主题里反的是两种颜色。
//
// ⚠️ **一个字都不许打真 B 站接口**：封面走本机 `httptest`，歌是内存夹具。
package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/egoist/mygo/ui"
)

// coverArrived 报首页那三张封面是不是都换成图了。
//
// ⚠️ **量的是「图的颜色」**：封面那一格在没到的时候是占位（描边框 + 一枚图标），
// 到了之后是**一整块纯色**——而现在纯色那块外面还多了一圈 1 像素的边（票 #244
// 的 E2），所以判据是「那一格里有三种颜色」（黑 / 白 / 灰三张封面的颜色）。
func coverArrived(tt *ui.Tester) bool {
	seen := map[string]bool{}
	img := tt.Image()
	// 那一列封面（48 宽）在 x≈269..317，三行在 y≈176..350。
	for y := 178; y < 348; y++ {
		for x := 272; x < 315; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			seen[fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)] = true
		}
	}
	return seen["#12141a"] && seen["#ffffff"] && seen["#808692"]
}

// coverShotPNG 造一张纯色 PNG。
func coverShotPNG(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 48, 48))
	for y := range 48 {
		for x := range 48 {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// saveMiniTipShot 存迷你窗那一张：**指针停在音量滑块上**，提示要看得见。
//
// ⚠️ **提示得在窗内**：这一扇窗只有 320×100，而音量滑块在第一行——摆在上面的话
// 整个提示落在窗外，用户一个字也看不见（票 #244 的 B5）。
func saveMiniTipShot(t *testing.T, dir string, dark bool, name string) {
	t.Helper()
	a := app.New()
	if err := a.MountSettings(filepath.Join(writableTempDir(t, "settings"), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close(); _ = a.CloseSettings() })
	if dark {
		if _, err := a.UpdateSettings(func(st *app.Settings) { st.ThemeMode = app.ThemeModeDark }); err != nil {
			t.Fatal(err)
		}
	}
	main := newTestShell(t, onApp(a)).s
	s := NewMini(a, main, nil)
	t.Cleanup(s.closePerWindow)

	if err := enqueueTwoWithCover(a, t); err != nil {
		t.Fatal(err)
	}
	a.SetVolume(0.8)

	tt := miniTester(s)
	waitFor(t, "封面画上来了", func() bool {
		tt.Frame()
		return !miniCoverBox(tt).Empty()
	})
	// 指针停到音量滑块上。
	bar, ok := tt.Find("音量")
	if !ok {
		t.Fatalf("迷你窗上找不到音量条，文字有 %q", tt.Texts())
	}
	tt.Move(bar.X+bar.W/2, bar.Y+bar.H/2)
	for range 3 {
		tt.Frame()
	}
	save(t, dir, "95-迷你窗-音量提示-"+name+".png", tt)
}
