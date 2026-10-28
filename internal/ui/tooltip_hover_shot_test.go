// 票 #420 的画面验收：播放条右边那一组里「播放模式」和「下载」两枚挨着，
// 两枚悬停提示叠在一起的那一处。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket420Shots
//
// # 深浅两套都要出
//
// 提示是**反色底**（深色界面上是一块浅色卡片，浅色界面上是一块深色卡片）——
// 两套底色差得远，只出一套看不出提示有没有叠在一起。
//
// ⚠️ **一个字都不许打真 B 站接口**：这一张只画播放条那一块，歌名和 UP 名是
// 钉死的假数据。
package ui

import (
	"fmt"
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/egoist/mygo/ui"
	mygoui "github.com/egoist/mygo/ui"
)

// TestRenderTicket420Shots 存两张图：指针停在「播放模式」上、指针挪到「下载」
// 上，深浅各一套。
func TestRenderTicket420Shots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	const winW, winH = 1200, 400
	st := app.State{HasTrack: true, Title: "一首歌", Artist: "某个 UP 主",
		Volume: 0.8, Rate: 1, Source: "online", PlayMode: app.PlayModeLoop}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newTestShell(t, at(winW, winH), systemIsDark(dark),
				withView(func(s *Shell) func(*mygoui.Context) {
					return func(c *mygoui.Context) {
						s.wake.set(c)
						// ⚠️ **播放条摆在窗口下沿**：提示是摆在锚点**上方**的，
						// 播放条贴着窗口顶的话提示会被裁掉。
						ui.Column(c).Fill().Children(func() {
							ui.Box(c).Grow(1)
							s.viewPlayBar(c, st)
						})
					}
				}))
			tt := b.tt
			tt.Frame()

			// ① 指针停在「播放模式」上：它那一枚的提示。
			hoverOn(t, tt, "播放模式")
			save(t, dir, fmt.Sprintf("20-播放条-提示-播放模式-%s.png", name), tt)

			// ② 挪到「下载」上：屏幕上该**只剩**「下载」那一枚提示。
			r, ok := tt.Find("下载")
			if !ok {
				t.Fatalf("播放条上找不到「下载」那一枚，文字有 %q", tt.Texts())
			}
			tt.Move(r.X+r.W/2, r.Y+r.H/2)
			tt.Frame()
			save(t, dir, fmt.Sprintf("21-播放条-提示-下载-%s.png", name), tt)
			b.Close()
		})
	}
}
