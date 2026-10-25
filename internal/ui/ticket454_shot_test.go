// 票 #454 的画面验收：队列抽屉那一行的悬停 / 按下反馈，和「清空播放列表」之前
// 那道确认。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket454Shots
//
// # 两套方案 × 深浅两套
//
// 这一票改的每一层都是「压一层半透明前景色」，而那一层是**近黑**（浅色下）或
// **近白**（深色下）——压出来的方向正好相反。而「正在放」那一行本来就是一层淡
// 主色底（暖纸是锈红、暮色琥珀是琥珀、柔桃是砖红），压上去之后还看不看得见
// 得逐套看。
//
// ⚠️ **一个字节都不许打真 B 站接口**：全部是内存夹具 + 无头渲染。
package ui

import (
	"os"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// TestRenderTicket454Shots 把队列抽屉开着的那一屏存成 PNG（两套方案 × 深浅各一套）。
func TestRenderTicket454Shots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}

	for _, preset := range []app.ThemePreset{app.ThemePresetWarmPaper, app.ThemePresetSoftPeach} {
		for _, dark := range []bool{false, true} {
			shade := "浅色"
			if dark {
				shade = "深色"
			}
			name := string(preset) + "-" + shade
			t.Run(name, func(t *testing.T) {
				st := app.DefaultSettings("")
				st.ThemePreset = preset
				st.ThemeMode = app.ThemeModeLight
				if dark {
					st.ThemeMode = app.ThemeModeDark
				}
				b := newTestShell(t, at(winW, winH), withSettings(st))
				h := &harness{app: b.a, tt: b.tt, s: b.s}

				songs := t.TempDir()
				enqueue(t, h,
					localSong(t, songs, "正在放.flac"),
					localSong(t, songs, "第二首.flac"),
					localSong(t, songs, "第三首.flac"))
				openDrawer(t, h)

				first, second, ok := queueRowRects(h, "第二首")
				if !ok {
					t.Fatalf("抽屉里找不到那两行，文字有 %q", h.tt.Texts())
				}
				// 裁剪范围：头两行（各 64 高）连着上下各留 4 像素。
				cropX := int(first.X) - 4
				cropY := int(first.Y) - 4
				cropW := int(first.W) + 8
				cropH := int(second.Y+second.H) - cropY + 4

				// 01：整窗（指针停在抽屉外面）。
				h.tt.Move(8, 8)
				h.tt.Frame()
				save(t, dir, "01-抽屉整窗-"+name+".png", h.tt)

				// 00：头两行，**指针没指着**——这是对照。
				saveRect(t, dir, "00-两行没指着-"+name+".png", h.tt, cropX, cropY, cropW, cropH)

				// 02：**正在放**那一行悬停——底色之外多压了一层 8% 的前景色。
				h.tt.Move(first.X+first.W/2, first.Y+first.H/2)
				h.tt.Frame()
				saveRect(t, dir, "02-正在放那行悬停-"+name+".png", h.tt, cropX, cropY, cropW, cropH)

				// 03：普通行悬停。
				h.tt.Move(second.X+second.W/2, second.Y+second.H/2)
				h.tt.Frame()
				saveRect(t, dir, "03-普通行悬停-"+name+".png", h.tt, cropX, cropY, cropW, cropH)

				// 04：普通行**按着**（不抬手）——比悬停深一档。
				h.tt.Press(second.X+second.W/2, second.Y+second.H/2)
				h.tt.Frame()
				saveRect(t, dir, "04-普通行按下-"+name+".png", h.tt, cropX, cropY, cropW, cropH)
				h.tt.Release(second.X+second.W/2, second.Y+second.H/2)
				h.tt.Frame()

				// 05：「清空播放列表」之前那道确认。
				if err := h.tt.Click("清空"); err != nil {
					t.Fatalf("点不了「清空」：%v", err)
				}
				h.tt.Frame()
				save(t, dir, "05-清空确认-"+name+".png", h.tt)
			})
		}
	}
}
