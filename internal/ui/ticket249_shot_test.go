// 票 #249 的画面验收：图标键**悬停时图形变主色**（上游的 `hover:text-primary`）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket249Shots
//
// # 出几张
//
//   - **顶栏**：指针停在「打开文件」上——那一枚要**变主色**，而旁边三枚（迷你 /
//     动态 / 登录）要**不变**（没指着的那几枚还是前景色）。这一张是「统一成一档」
//     最直接的证据：改之前这一枚和播放条上那些是两种画法。
//   - **队列抽屉**：指针停在行尾那枚「⋯」上——它要变主色，而**整行同时亮着**
//     （行自己是悬停反馈），两处要能分得开。
//
// # 深浅两套 × 两套主题方案
//
// 主色是**用户可设的**，而主题方案（暖纸 / 暮光 / 柔雾 / 经典）各带一套配色——
// 「悬停变主色」那一档是**从当前主色现算**的（`ink.AccentOn`），所以方案换了它
// 跟着换。这里出**暖纸**（默认）和**暮光**两套 × 深浅，一共四张。
package ui

import (
	"os"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
)

// shotName 给这一轮存图起个名：方案 + 深浅。
func shotName(preset app.ThemePreset, dark bool) string {
	name := string(preset)
	if dark {
		name += "-深色"
	} else {
		name += "-浅色"
	}
	return name
}

// TestRenderTicket249Shots 出这一票的对照图。
//
// ⚠️ **一个字都不许打真 B 站接口**：全部是内存夹具 + 无头渲染，钥匙串里那份真登录
// 态一次都没被读。
func TestRenderTicket249Shots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}

	for _, preset := range []app.ThemePreset{app.ThemePresetWarmPaper, app.ThemePresetDuskAmber} {
		for _, dark := range []bool{false, true} {
			name := shotName(preset, dark)

			// 顶栏：指针停在「打开文件」上。
			_, tt := navBarShell(t, preset, dark)
			btn, ok := tt.Find("打开文件")
			if !ok {
				t.Fatalf("顶栏上找不到「打开文件」，文字有 %q", tt.Texts())
			}
			tt.Move(btn.X+btn.W/2, btn.Y+btn.H/2)
			tt.Frame()
			save(t, dir, "01-顶栏-悬停在打开文件上-"+name+".png", tt)

			// 队列抽屉：指针停在行尾那枚「⋯」上。
			h := drawerShell(t, preset, dark)
			op, ok := h.tt.Find(list.OperationLabel("队列里的第二首"))
			if !ok {
				t.Fatalf("抽屉里找不到行尾那枚「⋯」，文字有 %q", h.tt.Texts())
			}
			h.tt.Move(op.X+op.W/2, op.Y+op.H/2)
			h.tt.Frame()
			save(t, dir, "02-队列抽屉-悬停在行尾的「⋯」上-"+name+".png", h.tt)
		}
	}
}
