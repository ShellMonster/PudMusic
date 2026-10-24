// 票 #311 的画面验收：播放条上那一枚音量图标的三档（响 / 小 / 静音），
// 以及「静音的时候音量还是 20%」时画的是静音那张。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket311Shots
//
// # 深浅两套都要出
//
// 这一枚是**单色**图标（跟着主题走），而深浅两套里那两种颜色差得远——
// 浅色下静音那张和底色挨得太近，不出两套就看不出它糊没糊。
//
// ⚠️ **一个字都不许打真 B 站接口**：这一张只画播放条那一块。
package ui

import (
	"fmt"
	"os"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// TestRenderTicket311Shots 把音量那一枚的三档各存一张 PNG，深浅各一套。
func TestRenderTicket311Shots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	const winW = 1200
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			for _, c := range []struct {
				label string
				st    app.State
			}{
				// 响（80%）。
				{"响", app.State{HasTrack: true, Title: "一首歌", Artist: "某个 UP 主",
					Volume: 0.8, Rate: 1}},
				// 小（20%）。
				{"小", app.State{HasTrack: true, Title: "一首歌", Artist: "某个 UP 主",
					Volume: 0.2, Rate: 1}},
				// 静音：**音量还是 20%**（静音是它自己的一格，票 #311）。
				{"静音", app.State{HasTrack: true, Title: "一首歌", Artist: "某个 UP 主",
					Volume: 0.2, Muted: true, Rate: 1}},
			} {
				st := c.st
				b := newTestShell(t, at(winW, coverWinH), systemIsDark(dark),
					withView(func(s *Shell) func(*mygoui.Context) {
						return func(c *mygoui.Context) {
							s.wake.set(c)
							s.viewPlayBar(c, st)
						}
					}))
				tt := b.tt
				tt.Frame()

				// 整窗一张。
				save(t, dir, fmt.Sprintf("10-播放条-音量%s-%s.png", c.label, name), tt)

				// 再裁一张音量那一枚：图标小，裁出来才看得清。
				//
				// ⚠️ **那一枚的位置是问出来的**，不是算出来的——右边那一组里
				// 模式 / 下载 / 队列 / 音量 / 倍速五枚的间距会跟着字号和主题变。
				r, ok := volumeKeyOnTheBar(tt)
				if !ok {
					t.Fatalf("播放条上找不到音量那一枚，文字有 %q", tt.Texts())
				}
				saveRect(t, dir, fmt.Sprintf("11-音量图标-%s-%s.png", c.label, name), tt,
					int(r.X)-14, int(r.Y)-14, int(r.W)+28, int(r.H)+28)
				b.Close()
			}
		})
	}
}
