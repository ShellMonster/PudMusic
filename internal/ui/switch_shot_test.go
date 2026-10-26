package ui

// 设置开关的出图：旋钮弹过去的那一下 + 拨开之后右下角那一句（票 BMG-284）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderSwitchShots
//
// # 出哪几张
//
//	01-开关-关着      拨之前（旋钮在左半边，轨道是灰的）
//	02-开关-半路      拨之后 100ms（旋钮在半路上，轨道已经是主色）
//	03-开关-弹完了    拨之后 700ms（旋钮在右半边）
//	04-提示           拨开「音量均衡」之后右下角那一句
//
// # ⚠️ **出图不是审查**
//
// 出图只是材料；**审查**（逐张打开比过五项：布局 / 间距 / 层级 / 密度 / 对齐）
// 才是那道门。差别写在收尾说明里。

import (
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

func TestRenderSwitchShots(t *testing.T) {
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
			tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""), systemIsDark(dark))

			// 把「音量均衡」那一行挪到画面中间：它默认在页面的下半截，而提示浮在
			// 右下角——两样东西叠在一块就看不出开关了。
			sw := settingRect(t, tt, s, "开关：音量均衡")
			s.settings.scroll.Y += sw.Y - 320
			tt.Frame()
			sw = settingRect(t, tt, s, "开关：音量均衡")

			// 01 **关着**：旋钮在左半边。
			save(t, dir, "01-开关-关着-"+name+".png", tt)
			saveHaloCrop(t, dir, "01-开关-关着-"+name+"-近.png", tt,
				int(sw.X)-4, int(sw.Y)-4, int(sw.W)+8, int(sw.H)+8)

			// 02 **半路**：拨下去，推 100ms 的钟——旋钮在中间。
			tt.ClickAt(sw.X+sw.W/2, sw.Y+sw.H/2)
			tt.Advance(100 * time.Millisecond)
			tt.Frame()
			save(t, dir, "02-开关-半路-"+name+".png", tt)
			saveHaloCrop(t, dir, "02-开关-半路-"+name+"-近.png", tt,
				int(sw.X)-4, int(sw.Y)-4, int(sw.W)+8, int(sw.H)+8)

			// 03 **弹完了**：推到底，旋钮在右半边。
			tt.Advance(600 * time.Millisecond)
			tt.Frame()
			save(t, dir, "03-开关-弹完了-"+name+".png", tt)
			saveHaloCrop(t, dir, "03-开关-弹完了-"+name+"-近.png", tt,
				int(sw.X)-4, int(sw.Y)-4, int(sw.W)+8, int(sw.H)+8)

			// 04 **那句提示**：右下角浮着的那一句短提示。
			save(t, dir, "04-提示-"+name+".png", tt)
		})
	}
}
