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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/egoist/mygo/ui"
)

// TestRenderTicket244Shots 把这一票改过的五处各存一张 PNG，深浅各一套。
func TestRenderTicket244Shots(t *testing.T) {
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
			// ── D1：设置页那一段空档 ──────────────────────────────
			//
			// ⚠️ **两档窗口都要出**：内容那一段收在 780 之后，窄窗口（900）那一档
			// 本来就放不下 780（可视区只有 700），画出来和收窄之前**一模一样**——
			// 这一票只作用在宽窗口上，而那正是空档最长的那一档。
			for _, winW := range []int{1200, 900} {
				b := newTestShell(t, at(winW, 800), systemIsDark(dark),
					withSettings(app.DefaultSettings("")))
				tt := b.tt
				openSettings(t, tt)
				homeFrame(tt)
				save(t, dir, fmt.Sprintf("90-设置页-整窗-%d-%s.png", winW, name), tt)

				// 「外观」那一段放大：说明和右边那枚控件之间的空档在那儿。
				head, ok := tt.Find("外观")
				if !ok {
					t.Fatalf("设置页上没有「外观」那个分组标题，文字有 %q", tt.Texts())
				}
				x0 := int(head.X) - 40
				saveRect(t, dir, fmt.Sprintf("91-设置页-外观段-%d-%s.png", winW, name), tt,
					x0, int(head.Y)-12, winW-x0-8, 300)
				b.Close()
			}

			// ── B6 + E1 + E2：首页（长歌名 + 「全部播放」+ 有边的封面）──
			saveHomeShot(t, dir, dark, name)

			// ── B6：搜索页那一枚「全部播放」（和首页那一枚对比）────────
			st := newSearchStub(10, 20)
			_, searchTT := newSearchPage(t, "周杰伦", st)
			waitFor(t, "搜索结果出来了", func() bool {
				searchTT.Frame()
				return searchTT.HasText(playAllLabel)
			})
			save(t, dir, "94-搜索页-全部播放-"+name+".png", searchTT)

			// ── B5：迷你窗那枚提示 ──────────────────────────────────
			saveMiniTipShot(t, dir, dark, name)
		})
	}
}

// saveHomeShot 存首页那一张：长歌名、首页的「全部播放」、有边的那几张封面。
//
// ⚠️ **封面走本机 `httptest`**：三张纯色图（近黑 / 白 / 中灰），而这一票要验的
// 正是「深色下近黑的那一张有没有边」。
func saveHomeShot(t *testing.T, dir string, dark bool, name string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/black.png":
			w.Write(coverShotPNG(t, color.RGBA{0x12, 0x14, 0x1a, 0xff}))
		case "/white.png":
			w.Write(coverShotPNG(t, color.RGBA{0xff, 0xff, 0xff, 0xff}))
		default:
			w.Write(coverShotPNG(t, color.RGBA{0x80, 0x86, 0x92, 0xff}))
		}
	}))
	defer srv.Close()

	b := newTestShell(t, systemIsDark(dark))
	s, tt := b.s, b.tt

	// ⚠️ **歌名要长**：E1 那一档量的是「一个 630 像素的歌名不再占满整行中段」。
	songs := []list.Music{
		{ID: "a", Title: "这是一首名字特别特别长的歌，长到一行绝对放不下，你得拖到后面才能看见最后那几个字", Owner: "一个名字也很长的 UP 主——UP 主名字比歌名还长一点点点点点点", Cover: srv.URL + "/black.png"},
		{ID: "b", Title: "短歌名", Owner: "UP 主", Cover: srv.URL + "/white.png"},
		{ID: "c", Title: "中等长度的歌名——刚好在边界上晃一晃，看看会不会被裁得很难看", Owner: "另一个 UP 主", Cover: srv.URL + "/gray.png"},
	}
	s.home.SetSongs(homeSongs{tabs: [homeTabCount][]list.Music{0: songs}})

	// ⚠️ **等封面真的画上来**：图是异步的，图还没到的时候画面上是占位（那一档
	// 本来就有边，量不出「有图那一档也描边」这件事）。
	waitFor(t, "封面画上来了", func() bool {
		tt.Frame()
		return tt.HasText("这是一首名字特别特别长的歌")
	})
	waitFor(t, "三张封面都到了", func() bool {
		tt.Frame()
		return coverArrived(tt)
	})
	save(t, dir, "92-首页-长歌名与全部播放-"+name+".png", tt)
	// 那一块放大：歌名的省略号、右边那几列、封面上那一圈边。
	saveRect(t, dir, "93-首页-长歌名放大-"+name+".png", tt, 200, 60, 1000, 260)
	b.Close()
}

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
