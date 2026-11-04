// 票 #249 的画面验收：图标键**悬停时图形变主色**（上游的 `hover:text-primary`）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket249Shots
//
// # 出几张
//
//   - **顶栏**：指针停在「迷你」上——那一枚要**变主色**，而旁边没指着的几枚（命令面板 /
//     深浅切换）要**不变**（还是前景色）。
//   - **队列抽屉**：指针停在行尾那枚「移除」上——它要变主色，而**整行同时亮着**
//     （行自己是悬停反馈），两处要能分得开。
//
// # 深浅两套 × 两种强调色
//
// 主色是**用户可设的**。「悬停变主色」那一档是**从当前主色现算**的（`ink.AccentOn`），
// 所以强调色换了它跟着换。这里出**朱砂**（默认）和**琥珀**两种 × 深浅，一共四组。
package ui

import (
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// shotName 给这一轮存图起个名：强调色 + 深浅。
func shotName(preset app.Accent, dark bool) string {
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
// ⚠️ **一个字都不许打真 B 站接口**：全部是内存夹具 + 无头渲染，本机那份真登录
// 态一次都没被读。
func TestRenderTicket249Shots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}

	for _, preset := range []app.Accent{app.AccentCinnabar, app.AccentAmber} {
		for _, dark := range []bool{false, true} {
			name := shotName(preset, dark)

			// 顶栏：指针停在「迷你」上（按钮名是「迷你」，提示是「迷你播放器」）。
			_, tt := navBarShell(t, preset, dark)
			btn, ok := tt.Find("迷你")
			if !ok {
				t.Fatalf("顶栏上找不到「迷你」，文字有 %q", tt.Texts())
			}
			tt.Move(btn.X+btn.W/2, btn.Y+btn.H/2)
			tt.Frame()
			save(t, dir, "01-顶栏-悬停在迷你上-"+name+".png", tt)

			// 队列抽屉：指针停在行尾那枚「移除」上（无障碍名字是「从播放列表移除 <歌名>」）。
			h := drawerShell(t, preset, dark)
			op, ok := h.tt.Find("从播放列表移除 队列里的第二首")
			if !ok {
				t.Fatalf("抽屉里找不到行尾那枚「移除」，文字有 %q", h.tt.Texts())
			}
			h.tt.Move(op.X+op.W/2, op.Y+op.H/2)
			h.tt.Frame()
			save(t, dir, "02-队列抽屉-悬停在行尾的移除键上-"+name+".png", h.tt)
		}
	}
}
