// 票 #194 的画面验收：右下角那三枚旋钮现在是 **⌀32 的圆钮**，圆里只有一枚图标
// （上游那三枚：Aa / 时钟 / T 形框）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket194Shots
//
// # 两处都要出
//
// 这一列画在**两处**（主窗口「正在播放」那一页、满屏播放器那一屏），而两处的
// **底**不一样：那一页底下是应用底色，满屏那一屏底下是糊过的封面——那层
// **前景色的 20%** 压在两种底上读出来的深浅不是一个数。
//
// 满屏那一屏的图在 `fullplayer_lyrics_test.go` 里（`63-满屏播放器-歌词-*.png`），
// 这一张补的是主窗口那一页——它此前只有一张「没有歌词」的（`19-迷你播放器-没有
// 歌词.png`，那一支只摆得着「找歌词」一枚）。
//
// # 深浅两套 + 一套方案都要出
//
// 圆底是**前景色**的 20%，而前景色在深浅两套里差得很远（黑 vs 白）——只出一套
// 就看不出那层底压得住压不住。
//
// ⚠️ **还要看一套方案**（设计规范 v0.1 的三套配色 + 经典一共四套，票面点名要
// 「暖纸/暮光/柔雾里挑一套也看一眼」）：钮上的颜色**走主题令牌**（前景色那
// 一档），所以换一套方案它跟着换——而这一张就是「它跟着换」的证据。
//
// ⚠️ **一个字都不许打真 B 站接口**：全是内存夹具 + 无头渲染。
package ui

import (
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// TestRenderTicket194Shots 把「正在播放」那一页的旋钮那一列存成 PNG。
//
// 深浅两套是**经典**（原来的 Biu 绿外观），再加深浅两套**暖纸**（默认方案）。
func TestRenderTicket194Shots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}

	for _, c := range []struct {
		label  string
		preset app.ThemePreset
		dark   bool
	}{
		{"经典", app.ThemePresetClassic, false},
		{"经典", app.ThemePresetClassic, true},
		{"暖纸", app.ThemePresetWarmPaper, false},
		{"暖纸", app.ThemePresetWarmPaper, true},
	} {
		name := "浅色"
		if c.dark {
			name = "深色"
		}
		t.Run(c.label+"-"+name, func(t *testing.T) {
			st := app.DefaultSettings("")
			st.ThemePreset = c.preset
			if c.dark {
				st.ThemeMode = app.ThemeModeDark
			}
			// 有歌词：三枚都在（字号 / 偏移 / 找歌词）。
			s, tt := lyricShell(t, withSettings(st))
			putLyrics(s, lyricsOfN(20), "")
			settleLyrics(t, s, tt)
			save(t, dir, "67-正在播放-歌词旋钮-"+c.label+"-"+name+".png", tt)

			// 裁一张旋钮那一列：⌀32 的圆钮在整窗图上太小，裁出来才看得清
			// 是不是圆的、圆里是不是只有一枚图标。
			//
			// ⚠️ **那一列的框是问出来的**：它在歌词区的右下角，位置跟着歌词区
			// 那一栏的留白走，算不如问。
			col, ok := tt.Find(lyricKnobColumnName)
			if !ok {
				t.Fatalf("画面上找不到那一列旋钮，文字有 %q", tt.Texts())
			}
			saveRect(t, dir, "68-正在播放-旋钮那一列-"+c.label+"-"+name+".png", tt,
				int(col.X)-12, int(col.Y)-12, int(col.W)+24, int(col.H)+24)
		})
	}
}
