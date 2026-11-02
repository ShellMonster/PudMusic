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

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/ink"
)

// TestTheHistoryChipIsAPill 钉住 chip 的角是半圆（半径 = 高的一半，一枚药丸）。
//
// 量法是两个点：chip 的**顶行左右两端**要透出浮层底色（药丸的角在那里各缺
// 一块，方角那一行是满的），而**中行左端**是满底（确认缺的那块是角、不是漏画）。
// chip 的上下沿在关闭键右边那段留白里量（半径之内），左右用 label 的矩形反推。
// 判据不能落在「正好是半圆」上——抗锯齿会把边缘那几排混成底色和 chip 色之间。
func TestTheHistoryChipIsAPill(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			s, tt := newShell(t, systemIsDark(dark))
			tt.SetFocused(true)
			s.hist.add("周杰伦")
			tt.Frame()
			if err := tt.Click(searchLabel); err != nil {
				t.Fatalf("点不到搜索框：%v", err)
			}
			tt.Frame()
			settleSuggestEnter(t, tt)

			label, ok := tt.Find("周杰伦")
			if !ok {
				t.Fatalf("浮层上没有那条历史，文字有 %q", tt.Texts())
			}
			th := BuildTheme(s.app.SettingsSnapshot(), dark)
			panel := rgb(th.Background)
			chip := rgb(historyChipBg(th))
			img := tt.Image()

			// chip 自己的上下沿：在关闭键右边那一段留白那一列上量（那里在半径
			// 之内，只有它自己的底色，不会被角上的圆缺咬掉）。
			close, ok := tt.Find("删除「周杰伦」")
			if !ok {
				t.Fatalf("浮层上没有那个关闭键，文字有 %q", tt.Texts())
			}
			chipTop, chipBottom := verticalRun(img, int(close.X+close.W+4),
				int(label.Y+label.H/2), panel)
			// 用 label 的矩形反推 chip 的左右：左边 8（labelPadX），右边
			// 关闭键（24）+ 缝（6）+ 右内边距（8）。
			left := int(label.X) - historyChipLabelPadX
			right := int(label.X+label.W) + historyChipLabelPadEnd +
				historyChipGap + historyChipClose + historyChipPadRight
			mid := (chipTop + chipBottom) / 2

			// 判据是**两个点**，不落在「正好是半圆」上（抗锯齿会把边缘那一排
			// 混成底色和 chip 色之间）：
			// ① 顶行（chipTop+1，抗锯齿往里让一行）的左右两端透出浮层底色——
			//    药丸的角在那里各缺一块，方角那一行是满的；
			topRow := chipTop + 1
			if got := img.RGBAAt(clampX(img, left+1), topRow); maxDiff(got, panel) > 4 {
				t.Errorf("chip 顶行左端是 %v，want 透出浮层底色 %v——左上角没圆（不是药丸）",
					got, panel)
			}
			if got := img.RGBAAt(clampX(img, right-2), topRow); maxDiff(got, panel) > 4 {
				t.Errorf("chip 顶行右端是 %v，want 透出浮层底色 %v——右上角没圆（不是药丸）",
					got, panel)
			}
			// ② 中行（半径处）左端是满的 chip 底——确认缺的那块真的是角、不是漏画；
			if got := img.RGBAAt(clampX(img, left+1), mid); maxDiff(got, chip) > 4 {
				t.Errorf("chip 中行左端是 %v，want 满底 %v——chip 左边少了一块，不是圆角",
					got, chip)
			}
			// ③ 圆缺的深度 ≈ 半径（12）：顶行往右 12 像素处（半径圆心那一列）
			// 附近已回到 chip 底。这卡住「只圆了 2」那种假圆角。
			if got := img.RGBAAt(clampX(img, left+historyChipRadius+1), topRow+1); maxDiff(got, chip) > 6 {
				t.Errorf("chip 顶行往右 %d 像素处是 %v，want %v——圆角太浅，不是半圆药丸",
					historyChipRadius+1, got, chip)
			}
		})
	}
}

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
			panel := rgb(th.Background)
			soft := rgb(accentSoft(th))
			accent := ink.AccentOn(th)

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
			// 字染主色：label 那一格里要有主色墨迹。
			if n := countNear(img, label, accent); n == 0 {
				t.Errorf("敲的词和历史一字不差时 label 里没有主色（%v）墨迹——字没染", accent)
			}
			// 另一枚 chip 还是灰的。
			other, ok := tt.Find("林俊杰")
			if !ok {
				t.Fatalf("浮层上没有另一条历史，文字有 %q", tt.Texts())
			}
			grey := rgb(historyChipBg(th))
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
