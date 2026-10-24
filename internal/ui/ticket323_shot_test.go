// 票 #323 的画面验收：分集抽屉里「正在放」的那一集是**淡主色底**（上游
// `bg-primary/20` 的那一层），不是实心主色底。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket323Shots
//
// # 深浅两套都要出
//
// 这一票改的**只有底色**：浅色下那一层是 #d2f7df（压在 #ffffff 上），深色下是
// #193e29（压在 #18181b 上，只差十几个通道）——只出浅色的话深色下那一层根本
// 看不出来。
//
// ⚠️ **指针要停在面板外面**：悬停会把那一行的底换成 `t.SurfaceHover`，而这一张
// 要看的是「正在放」那一行自己的底。
//
// ⚠️ **一个字都不许打真 B 站接口**：全部是内存夹具 + 无头渲染。
package ui

import (
	"os"
	"testing"
)

// TestRenderTicket323Shots 把分集抽屉开着的那一屏存成 PNG（深浅各一套）。
func TestRenderTicket323Shots(t *testing.T) {
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
			s, tt := onlineTester(t, multiPages(), false)
			tt.SetDark(dark)
			partsReady(t, s, tt, 3)
			openPartsDrawer(t, tt)

			// 指针挪到面板外面（见文件头那一段）。
			tt.Move(8, 8)
			tt.Frame()

			save(t, dir, "01-分集抽屉-整窗-"+name+".png", tt)

			// 三行放大：正在放的那一行是淡主色底，旁边两行是面板底。
			first, ok := tt.Find("第一集")
			if !ok {
				t.Fatalf("画面上找不到「第一集」，文字有 %q", tt.Texts())
			}
			last, ok := tt.Find("第 3 集")
			if !ok {
				t.Fatalf("画面上找不到「第 3 集」，文字有 %q", tt.Texts())
			}
			saveRect(t, dir, "02-分集抽屉-三行-"+name+".png", tt,
				int(first.X), int(first.Y)-4, int(first.W),
				int(last.Y+last.H-first.Y+4))
		})
	}
}
