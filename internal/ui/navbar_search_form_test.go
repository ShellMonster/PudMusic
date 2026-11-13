package ui

import (
	"image"
	"image/color"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

// 这一组量的是顶栏搜索框的外观：**480×36、1px 描边、浅灰底**（hover / focus
// 抬到 30%，聚焦描边换主色），历史是**可关闭的灰色 Chip**，历史标题 **14/400**。
// （#165 时是 280×40 无边框，2026-11 重设计改成现在这一档。）
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

			// ② 悬停：底色不变（原型 index.html:232 只有 `:focus-within` 才换 surface 底；
			// 曾经悬停也换成聚焦那一档）。
			tt.Move(float32(col), float32(mid))
			tt.Frame()
			got = tt.Image().RGBAAt(col, mid)
			if maxDiff(got, idle) > 2 {
				t.Errorf("悬停时搜索框底色是 %v，want 静止那一档 %v（悬停不换底）", got, idle)
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

			// ④ 描边：**聚焦那一档是主色**（票 #453）。
			//
			// ⚠️ **这一条以前是反的**：改之前这里断言「没有描边」——那是「只有
			// 底色」的画法。而框里那个输入框是 `ui.TextInputBase`（**没有描边**），
			// 框架对输入框一律不画焦点环，于是聚焦时**一点指示都没有**：同一个
			// 应用里另一个搜索框（收藏夹 / 本地音乐那枚可展开的搜索）聚焦时描边
			// 变主色——**同一个动作两种反应**。这一票把它补回来。
			//
			// ⚠️ **描边画在这一格的边沿上**（框架的 `Border` 是「盒子里那条线」），
			// 所以它在框里那个采样点的**左边一像素**。
			rowLeft := int(r.X) - (searchPadX + int(tok.IconSm) + searchGap)
			inside := tt.Image().RGBAAt(rowLeft+3, mid)
			if maxDiff(inside, hot) > 6 {
				t.Fatalf("框里那个采样点是 %v，want %v——左缘位置算错了，下面的描边判据不成立",
					inside, hot)
			}
			borderX := clampX(tt.Image(), rowLeft-1)
			gotBorder := tt.Image().RGBAAt(borderX, mid)
			// 主色是**不透明**的，所以聚焦那一档的描边就该是主色本身（抗锯齿会
			// 差一两个通道）。
			// 原型 `.search-box:focus-within`：描边 `line-strong`。
			if d := maxDiff(gotBorder, rgb(tok.Of(th).LineStrong)); d > 10 {
				t.Errorf("聚焦时搜索框的描边是 %v，want line-strong %v（差 %d）——"+
					"聚焦没有看得见的指示", gotBorder, tok.Of(th).LineStrong, d)
			}
			// 描边和框里那层底**分得开**：不然这一圈线就白画了。
			if d := maxDiff(gotBorder, hot); d < 8 {
				t.Errorf("聚焦时搜索框的描边 %v 和框里的底色 %v 几乎一样（差 %d）——"+
					"这一圈线看不出来", gotBorder, hot, d)
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
			// ⚠️ **要把面板的进场推完**（票 #259）：浮层现在进场是 200ms 的一段
			// （淡入 + 升上来），而 Tester 的钟站着不动——不推它的话画面停在
			// 进场的第一帧，而那一帧**面板上一个字都没画**（不透明度是 0）。
			// 这一条量的是面板上的像素，所以必须先让它淡进来。
			settleSuggestEnter(t, tt)

			chip, ok := tt.Find("周杰伦")
			if !ok {
				t.Fatalf("浮层上没有那条历史，文字有 %q", tt.Texts())
			}
			th := BuildTheme(s.app.SettingsSnapshot(), dark)
			panel := rgb(tok.Of(th).Surface)
			want := rgb(tok.Of(th).Surface2)

			// ① 灰底：chip 左内边距里那一块就是底色。深浅两套都不是面板底色。
			mid := int(chip.Y + chip.H/2)
			got := tt.Image().RGBAAt(int(chip.X)-4, mid)
			if maxDiff(got, want) > 2 {
				t.Errorf("历史 chip 底色是 %v，want %v（HeroUI solid default）", got, want)
			}
			// HeroUI 那套 default 色阶是锌灰，带一点点蓝（深色 #3f3f46 的三个
			// 通道差 7）；只要不是彩色的，都算这一档的「灰」。
			// 「深夜唱片行」的灰带一点冷暖（三个通道差到 ~20），所以放到 24。
			limit := 24
			if spread := channelSpread(got); spread > limit {
				t.Errorf("历史 chip 底色 %v 不是灰的（三个通道差 %d）", got, spread)
			}
			if maxDiff(got, panel) < 8 {
				t.Errorf("历史 chip 的底 %v 和浮层底色 %v 几乎一样——它看上去还是一行字，不是一块 chip",
					got, panel)
			}

			// ② 高度 30（原型 `.chip`）：在 chip 中间那一列竖直量。
			cx := int(chip.X + chip.W/2)
			top, bottom := verticalRun(tt.Image(), cx, mid, panel)
			if h := bottom - top + 1; abs(h-30) > 1 {
				t.Errorf("历史 chip 高 %d（y %d..%d），want 30", h, top, bottom)
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

// TestTheClearAllButtonShowsATrashBin 验「清空搜索历史」键：原型（`openSuggest` 的
// `<button data-act="clear-recent">清空搜索历史</button>`）是一枚 12px 的纯文字键，没有垃圾桶图标，
// 所以这里断的是：键在、带悬停提示、点一下真的清掉全部历史。名字沿用旧的，台账里登记的就是它。
func TestTheClearAllButtonShowsATrashBin(t *testing.T) {
	s, tt := newShell(t, systemIsDark(false))
	tt.SetFocused(true)
	s.hist.add("周杰伦")
	tt.Frame()
	if err := tt.Click(searchLabel); err != nil {
		t.Fatalf("点不到搜索框：%v", err)
	}
	tt.Frame()
	settleSuggestEnter(t, tt)

	if _, ok := tt.Find(suggestClearLabel); !ok {
		t.Fatalf("浮层上没有「%s」，文字有 %q", suggestClearLabel, tt.Texts())
	}
	if err := tt.Click(suggestClearLabel); err != nil {
		t.Fatalf("点不到「%s」：%v", suggestClearLabel, err)
	}
	tt.Frame()
	if n := len(s.hist.list()); n != 0 {
		t.Errorf("点了「%s」之后历史里还有 %d 条", suggestClearLabel, n)
	}
	if !tt.HasText("已清空搜索历史") {
		t.Errorf("清空之后没有说一句，文字有 %q", tt.Texts())
	}
}

// ---- 这一组用的小工具 ----

// countNear 数那一格里有多少个像素**接近** want（三通道相加的差 ≤ 40）。
//
// 只用来量「那一格里有没有某个颜色的墨迹」：抗锯齿的边缘像素不会算进去，
// 而图形最浓的那几个像素是算得进去的。
func countNear(img *image.RGBA, r ui.Rect, want ui.Color) int {
	n := 0
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			if distRGBA(img.RGBAAt(clampX(img, x), y), want) <= 40 {
				n++
			}
		}
	}
	return n
}

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
