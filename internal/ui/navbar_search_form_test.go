package ui

import (
	"image"
	"image/color"
	"testing"

	"github.com/egoist/mygo/ui"
)

// 这一组量的是 #165 那三条外观：顶栏搜索框是 **280×40、无边框、浅灰底**
// （hover / focus 抬到 30%），历史是**可关闭的灰色 Chip**，历史标题 **14/400**。
//
// 判据全部是画面：高度从像素里量、颜色按主题算出来再和像素比。上游 HeroUI 那
// 几个类名（`bg-default-400/20`、`solid` Chip）是源码换算，这里量的是我们自己
// 画出来的像素——两套主题各量一遍，因为深色那一档换的是另一个灰。

// TestSearchFieldLooksLikeAnInput 验搜索框的形态。
func TestSearchFieldLooksLikeAnInput(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			s, tt := newShell(t, systemIsDark(dark))
			tt.SetFocused(true)
			// 指针先放到内容区，别让 idle 那一档带上悬停。
			tt.Move(900, 500)
			tt.Frame()

			r, ok := tt.Find(searchLabel)
			if !ok {
				t.Fatalf("顶栏上没有搜索框，文字有 %q", tt.Texts())
			}
			th := BuildTheme(s.app.SettingsSnapshot(), dark)
			bg := rgb(th.Background)
			idle := over(searchFieldBg(th, false), th.Background)
			hot := over(searchFieldBg(th, true), th.Background)

			// 量的那一列落在放大镜和输入框之间那道缝里（框里没有字也没有图标）。
			col := int(r.X) - searchGap/2
			img := tt.Image()
			top, bottom := verticalRun(img, col, int(r.Y+r.H/2), bg)
			if h := bottom - top + 1; abs(h-searchHeight) > 1 {
				t.Fatalf("搜索框高 %d（y %d..%d），want %d", h, top, bottom, searchHeight)
			}
			mid := (top + bottom) / 2

			// ① 静止：浅灰底。既要是算出来的那个 20% 灰，也要和顶栏底色分得出来。
			got := img.RGBAAt(col, mid)
			if maxDiff(got, idle) > 2 {
				t.Errorf("搜索框静止底色是 %v，want %v（default-400/20 压在底色上）", got, idle)
			}
			if maxDiff(got, bg) < 8 {
				t.Errorf("搜索框底色 %v 和顶栏底色 %v 几乎一样——看不出这是一块输入区", got, bg)
			}

			// ② 悬停：底色抬到 30%，不是描边变色。
			tt.Move(float32(col), float32(mid))
			tt.Frame()
			got = tt.Image().RGBAAt(col, mid)
			if maxDiff(got, hot) > 2 {
				t.Errorf("悬停时搜索框底色是 %v，want %v（default-400/30）", got, hot)
			}
			if maxDiff(hot, idle) < 5 {
				t.Errorf("悬停和静止的底色差不到 5 个通道（%v / %v）——用户看不出它是活的",
					idle, hot)
			}

			// ③ 焦点：同一档 30%。点完把指针挪开，免得量到的是悬停。
			if err := tt.Click(searchLabel); err != nil {
				t.Fatalf("点不到搜索框：%v", err)
			}
			tt.Move(900, 500)
			tt.Frame()
			got = tt.Image().RGBAAt(col, mid)
			if maxDiff(got, hot) > 2 {
				t.Errorf("聚焦时搜索框底色是 %v，want %v（和悬停同一档，30%%）", got, hot)
			}
			if maxDiff(hot, idle) < 5 {
				t.Errorf("聚焦没有把底色抬起来（%v / %v）", idle, hot)
			}

			// ④ 没有描边：左缘那一条要和框里同一个颜色。焦点那一档尤其要看——
			// 改之前这里是一圈主色绿，是最容易在重构成「底色」时漏掉的一条。
			rowLeft := int(r.X) - (searchPadX + iconMd + searchGap)
			inside := tt.Image().RGBAAt(rowLeft+3, mid)
			if maxDiff(inside, hot) > 6 {
				t.Fatalf("框里那个采样点是 %v，want %v——左缘位置算错了，下面的描边判据不成立",
					inside, hot)
			}
			nearFill, nearBorder, nearAccent := 1<<30, 1<<30, 1<<30
			for x := rowLeft - 1; x <= rowLeft+3; x++ {
				p := tt.Image().RGBAAt(clampX(tt.Image(), x), mid)
				nearFill = min(nearFill, distRGBA(p, ui.Color{R: hot.R, G: hot.G, B: hot.B, A: 255}))
				nearBorder = min(nearBorder, distRGBA(p, th.Border))
				nearAccent = min(nearAccent, distRGBA(p, th.Accent))
			}
			if nearBorder < nearFill || nearAccent < nearFill {
				t.Errorf("搜索框左缘（x=%d）最像的是描边/主色而不是底色（距底色 %d、描边 %d、主色 %d）——"+
					"框上还留着一圈边框", rowLeft, nearFill, nearBorder, nearAccent)
			}
		})
	}
}

// TestSearchHistoryIsAGreyChip 验历史那一条是可关闭的灰色 Chip。
func TestSearchHistoryIsAGreyChip(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			s, tt := newShell(t, systemIsDark(dark))
			tt.SetFocused(true)
			s.hist.add("周杰伦")
			s.hist.add("林俊杰")
			tt.Frame()

			if err := tt.Click(searchLabel); err != nil {
				t.Fatalf("点不到搜索框：%v", err)
			}
			tt.Frame()

			chip, ok := tt.Find("周杰伦")
			if !ok {
				t.Fatalf("浮层上没有那条历史，文字有 %q", tt.Texts())
			}
			th := BuildTheme(s.app.SettingsSnapshot(), dark)
			panel := rgb(th.Background)
			want := rgb(historyChipBg(th))

			// ① 灰底：chip 左内边距里那一块就是底色。深浅两套都不是面板底色。
			mid := int(chip.Y + chip.H/2)
			got := tt.Image().RGBAAt(int(chip.X)-4, mid)
			if maxDiff(got, want) > 2 {
				t.Errorf("历史 chip 底色是 %v，want %v（HeroUI solid default）", got, want)
			}
			// HeroUI 那套 default 色阶是锌灰，带一点点蓝（深色 #3f3f46 的三个
			// 通道差 7）；只要不是彩色的，都算这一档的「灰」。
			if spread := channelSpread(got); spread > 8 {
				t.Errorf("历史 chip 底色 %v 不是灰的（三个通道差 %d）", got, spread)
			}
			if maxDiff(got, panel) < 8 {
				t.Errorf("历史 chip 的底 %v 和浮层底色 %v 几乎一样——它看上去还是一行字，不是一块 chip",
					got, panel)
			}

			// ② 高度 24（上游 h-6）：在 chip 中间那一列竖直量。
			cx := int(chip.X + chip.W/2)
			top, bottom := verticalRun(tt.Image(), cx, mid, panel)
			if h := bottom - top + 1; abs(h-24) > 1 {
				t.Errorf("历史 chip 高 %d（y %d..%d），want 24", h, top, bottom)
			}

			// ③ 可关闭：那个叉有自己的一条无障碍名字，点得准也删得掉。
			if _, ok := tt.Find("删除「周杰伦」"); !ok {
				t.Errorf("历史 chip 上没有关闭键，文字有 %q", tt.Texts())
			}

			// ④ 左边那 8 像素归标签按钮，点在它的内边距上也点得中这条历史
			// （上游整个 chip 可点；右半圈留给关闭键）。
			tt.ClickAt(float32(int(chip.X)-4), float32(mid))
			tt.Frame()
			if got := s.searchKeyword; got != "周杰伦" {
				t.Errorf("点 chip 左内边距之后框里是 %q，want 周杰伦——那一段没接在标签按钮上", got)
			}
			if got := s.router.Path(); got != searchPath {
				t.Errorf("点 chip 左内边距之后在 %q，want %q", got, searchPath)
			}
		})
	}
}

// TestSearchHistoryHeadingIsFourteenRegular 验历史标题 14/400。
//
// 400 那一半由代码里的「没有 Bold()」保证；画面上能量的是字号：同一行里
// 「清除全部」是 12，标题比它高一档（14 的墨迹高度多 2.36 像素，13 只多 1.18）。
func TestSearchHistoryHeadingIsFourteenRegular(t *testing.T) {
	s, tt := newShell(t)
	tt.SetFocused(true)
	s.hist.add("周杰伦")
	tt.Frame()
	if err := tt.Click(searchLabel); err != nil {
		t.Fatalf("点不到搜索框：%v", err)
	}
	tt.Frame()

	head, ok := tt.Find("搜索历史")
	if !ok {
		t.Fatalf("浮层上没有历史标题，文字有 %q", tt.Texts())
	}
	clear, ok := tt.Find("清除全部")
	if !ok {
		t.Fatalf("浮层上没有「清除全部」，文字有 %q", tt.Texts())
	}
	if d := head.H - clear.H; d < 1.6 {
		t.Errorf("历史标题高 %.2f、12 号的「清除全部」高 %.2f（差 %.2f）——"+
			"标题还是 13 号（13 与 12 的墨迹高度只差 1.18）", head.H, clear.H, d)
	}
}

// ---- 这一组用的小工具 ----

// verticalRun 从 mid 开始往上、往下找「不是 bg」的那一段，返回首末行。
//
// 只用来量顶栏那个搜索框：它四周留白都是顶栏底色，所以一段连续的非底色就是
// 框本身。指针悬停改变的是框的底色，不会改变这一段的范围。
func verticalRun(img *image.RGBA, x, mid int, bg color.RGBA) (top, bottom int) {
	top, bottom = mid, mid
	for top > 0 && img.RGBAAt(x, top-1) != bg {
		top--
	}
	for bottom < img.Rect.Max.Y-1 && img.RGBAAt(x, bottom+1) != bg {
		bottom++
	}
	return top, bottom
}

// over 把一层半透明的 fg 合到 bg 上，得到画面上该有的那个像素。
func over(fg, bg ui.Color) color.RGBA {
	a := float32(fg.A) / 255
	mix := func(f, b uint8) uint8 { return uint8(float32(f)*a + float32(b)*(1-a) + 0.5) }
	return color.RGBA{R: mix(fg.R, bg.R), G: mix(fg.G, bg.G), B: mix(fg.B, bg.B), A: 255}
}

func rgb(c ui.Color) color.RGBA { return color.RGBA{R: c.R, G: c.G, B: c.B, A: 255} }

func channelSpread(c color.RGBA) int {
	lo := min(c.R, min(c.G, c.B))
	hi := max(c.R, max(c.G, c.B))
	return int(hi - lo)
}
