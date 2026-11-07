package ui

// 刘海播放器的画面验收（T5，DESIGN.md §0′.T5）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderNotchShots
//
// 出图：深 / 浅 × 展开（对应参考图 `ref/*-notch.png`）+ 收起（两翼）+ 没歌。刘海永远
// 是黑的，深浅两套的差别只在主色（eq 柱、「开启」键）。数据全是合成的：本地测试
// 音频的文件名当歌名、纯色封面。

import (
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// settleNotch 推钟到刘海的形状动画停稳：推钟 + 问 `FramePending`，不数帧。
func settleNotch(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	for range 240 {
		tt.Advance(time.Second / 60)
		tt.Frame()
		if !tt.FramePending() {
			return
		}
	}
	t.Fatal("推了 4 秒的钟，刘海的动画还没停")
}

// TestRenderNotchShots 把刘海存成 PNG，留给人眼对照原型参考图。
func TestRenderNotchShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{true, false} {
		theme := "light"
		if dark {
			theme = "dark"
		}
		t.Run(theme, func(t *testing.T) {
			// 起始设置里直接给深浅档：刘海不随主题，浅色那张必须和深色那张同一套配色。
			n, _ := newNotchShellWith(t, app.Settings{ThemeMode: app.ThemeMode(theme)})
			if err := enqueueTwoWithCover(n.app, t); err != nil {
				t.Fatal(err)
			}
			n.app.Toggle() // 暂停：eq 柱空着（和参考图一致）
			tt := mygoui.NewTester(n.View, notchWinW, notchWinH)
			n.repaint = tt.Frame
			waitFor(t, "封面画上来了", func() bool {
				tt.Frame()
				return !miniCoverBox(tt).Empty()
			})
			settleNotch(t, tt)
			save(t, dir, theme+"-notch-collapsed-paused.png", tt)

			n.app.Toggle() // 放：两翼右边的 eq 柱跳起来（它一直在循环，所以不等「停」，推几帧就拍）
			tt.Frame()
			for range 30 {
				tt.Advance(time.Second / 60)
				tt.Frame()
			}
			save(t, dir, theme+"-notch-collapsed-playing.png", tt)

			// 先在暂停态把展开动画收稳再开播：直接开播就拍的话，形状还在
			// 240ms ease-out 的中间，卡片透明度/位移落在
			// 半路——每张图的高度都不一样，对照参考图就是在量一个随机相位。
			n.app.Toggle() // 先停：eq 柱和频谱都不要帧了，形状动画才收得稳。
			n.notchSt.open = true
			tt.Frame()
			settleNotch(t, tt)
			n.app.Toggle() // 再放：展开卡的频谱柱跳起来（它一直在走，推几帧就拍）。
			for range 30 {
				tt.Advance(time.Second / 60)
				tt.Frame()
			}
			save(t, dir, theme+"-notch-playing.png", tt)

			n.app.Toggle() // 再停：展开态暂停那一档也出一张（频谱收起来）。
			tt.Frame()
			settleNotch(t, tt)
			save(t, dir, theme+"-notch.png", tt)
		})
	}

	// 没歌：展开是那张「未在播放 + 打开」的小卡（深浅各一张：刘海不随主题，两张应当一样）。
	for _, mode := range []app.ThemeMode{app.ThemeModeLight, app.ThemeModeDark} {
		t.Run("idle-"+string(mode), func(t *testing.T) {
			n, _ := newNotchShellWith(t, app.Settings{ThemeMode: mode})
			n.notchSt.open = true
			tt := mygoui.NewTester(n.View, notchWinW, notchWinH)
			n.repaint = tt.Frame
			settleNotch(t, tt)
			save(t, dir, string(mode)+"-notch-idle.png", tt)
		})
	}
}
