// 动态红点那一枚的出图：深浅两套。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderDynamicDotShots
//
// ⚠️ **深浅两套都要出**：那个点是**图形色**（规矩见 semantic.go 第 3 条：图形色
// 用主题里那一个原值），而主题方案各有自己的那一档——浅色下和深色下不是同一
// 个数，只出一套的话另一套根本看不出来。
//
// ⚠️ **一个字都不许打真 B 站接口**：问的那一只是换掉的（同步回话），其余全是
// 内存夹具 + 无头渲染。
package ui

import (
	"os"
	"testing"
)

// TestRenderDynamicDotShots 把「顶栏右上角那个点」存成 PNG（深浅各一张）。
func TestRenderDynamicDotShots(t *testing.T) {
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
			s, tt := newShell(t, systemIsDark(dark))
			tt.SetFocused(true)
			// 假问：同步回话，说有 3 条没看过的。
			//
			// ⚠️ **换上假时钟**：外壳建好时已经画过一帧，那一帧走的是真时钟，
			// 它已经把「下一次该轮的时刻」推到五分钟之后了——不拨回零值，下面
			// 那一帧不会问，那个点也就不会出来（理由见 navbar_test.go 的
			// fakeClockOn）。
			fakeClockOn(s, fakeClockStart)
			s.feed.ask = func(done func(int, error)) { done(3, nil) }

			tt.Frame()
			icon, ok := tt.Find("动态")
			if !ok {
				t.Fatalf("顶栏上没有「动态」那一枚，文字有 %q", tt.Texts())
			}
			dot, ok := tt.Find("3 条新动态")
			if !ok {
				t.Fatalf("那个点没画出来，文字有 %q", tt.Texts())
			}
			// 只存右上角那一小块：那个点就 8 像素，整张图里几乎看不见。
			x0 := int(icon.X) - 12
			y0 := int(icon.Y) - 12
			w := int(icon.W) + 24
			h := int(icon.H) + 24
			saveRect(t, dir, "动态红点-"+name+".png", tt, x0, y0, w, h)
			t.Logf("那个点在 (%v, %v)", dot.X, dot.Y)
		})
	}
}
