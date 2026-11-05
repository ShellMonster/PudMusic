package ui

// 历史 chip 的**形状与状态**（2026-11 重设计，照用户给的参考图）：
//
//   - **药丸**：角是半圆（半径 = 高的一半），不是跟着 `t.Radius` 走的 8。
//     和胶囊搜索框（`searchRadius = searchHeight/2`）同一个理由——那是这枚
//     chip 的**形状**，用户把全局圆角拖到 0 它也还是药丸；
//   - **当前词高亮**：chip 上的词和框里正在敲的一字不差时，它换成「正在输入」
//     那一档——`accentSoft` 的底 + `ink.AccentOn` 的字（和选中行同一套色），
//     不再是那块灰。
//
// 判据是画面：药丸用量角——角上一行只有中间一小段是 chip 的底、两端透出浮层
// 底色（8 的方角那一行几乎铺满）；高亮直接量底色和字色。两套主题各量一遍
// （`accentSoft` / `AccentOn` 深浅各算一份）。

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
)

// TestTheChipMatchingTheInputStandsOut 钉住「正在敲的那个词」那一档：
// 底是 `accentSoft`、字是 `ink.AccentOn`；敲到一半（不匹配）和别的 chip 都还是灰的。
func TestTheChipMatchingTheInputStandsOut(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			f := newBiliFixture(t, 1)
			s, tt := newSuggestShell(t, f, systemIsDark(dark))
			s.hist.add("周杰伦")
			s.hist.add("林俊杰")
			tt.Frame()

			th := BuildTheme(s.app.SettingsSnapshot(), dark)
			pal := tok.Of(th)
			panel := rgb(th.Background)
			soft := rgb(pal.AccentSoft) // 原型：匹配输入的 chip 换 accent-soft 底
			text := pal.Text            // 字是正文色（原型 .chip 悬停 / 选中同款）

			// ① 一字不差：chip「周杰伦」换成高亮那一档。
			typeInSearch(t, tt, "周杰伦")
			// 联想词防抖要等结果回来才画那一档；这一条只量历史，不用等联想——
			// 直接等 chip 出现（面板开着的信号）。
			waitFrames(t, tt, "历史那排出来", func() bool {
				_, ok := tt.Find("林俊杰")
				return ok
			})
			settleSuggestEnter(t, tt)

			label, ok := tt.Find("周杰伦")
			if !ok {
				t.Fatalf("浮层上没有那条历史，文字有 %q", tt.Texts())
			}
			img := tt.Image()
			mid := int(label.Y + label.H/2)
			// 高亮档的底：label 左边那一块就是 chip 的底。
			if got := img.RGBAAt(clampX(img, int(label.X)-4), mid); maxDiff(got, soft) > 3 {
				t.Errorf("敲的词和历史一字不差时 chip 底是 %v，want %v（accentSoft）——"+
					"「正在输入」那一档没亮", got, soft)
			}
			// 字换成正文色：label 那一格里要有正文色墨迹。
			if n := countNear(img, label, text); n == 0 {
				t.Errorf("敲的词和历史一字不差时 label 里没有正文色（%v）墨迹——字没换", text)
			}
			// 另一枚 chip 还是灰的。
			other, ok := tt.Find("林俊杰")
			if !ok {
				t.Fatalf("浮层上没有另一条历史，文字有 %q", tt.Texts())
			}
			grey := rgb(pal.Surface2) // 原型 .chip 底
			if got := img.RGBAAt(clampX(img, int(other.X)-4), int(other.Y+other.H/2)); maxDiff(got, grey) > 3 {
				t.Errorf("没敲中的 chip 底是 %v，want %v（灰）——高亮染过了头", got, grey)
			}

			// ② 敲到一半：退回灰底（面板仍在开着的状态再敲一个字）。
			typeInSearch(t, tt, "x")
			waitFrames(t, tt, "chip 退回灰底", func() bool {
				img := tt.Image()
				l, ok := tt.Find("周杰伦")
				if !ok {
					return false
				}
				got := img.RGBAAt(clampX(img, int(l.X)-4), int(l.Y+l.H/2))
				return got != soft && maxDiff(got, panel) > 8
			})
			if got := tt.Image().RGBAAt(clampX(img, int(label.X)-4), mid); maxDiff(got, grey) > 3 {
				t.Errorf("敲到一半时 chip 底是 %v，want 退回 %v（灰）——高亮该跟着输入走", got, grey)
			}
		})
	}
}
