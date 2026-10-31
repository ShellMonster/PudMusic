package ui

import (
	"image/color"
	"testing"

	"github.com/egoist/mygo/ui"
)

// 这一族钉的是票 #453 的头两条：**顶栏那个搜索框聚焦时有一圈看得见的指示**，
// 而且那一圈**和同应用另一个搜索框是同一档**。
//
// ## 为什么这两条要钉在一起
//
// 顶栏这一枚的框里装的是 `ui.TextInputBase`——**没有描边**；而框架对 `kindInput`
// 一律不画焦点环（`mygo@v0.7.3/ui/paint.go:384` 的 `e.kind != kindInput`）。
// 于是聚焦时唯一的变化是底色 20% → 30%，浅色底上几乎读不出来。
//
// 而同一个应用里另两枚搜索框（收藏夹 / 本地音乐那枚可展开的搜索、歌词搜索框）
// 走的是框架的 `ui.TextInput`——**有 1px 描边，聚焦时描边变主色**。
//
// **同一个动作两种反应**：所以这一票不是「给顶栏加一圈环」，是「照那一档来」。

// TestTheTopBarSearchBoxFocusesLikeTheOtherSearchBox 钉验收第 2 条：
// 顶栏那个搜索框的焦点指示**和同应用另一个搜索框是同一档**。
//
// 判据是**画面上的像素**：两枚都聚焦，量各自描边那一像素——**同一个动作
// （把焦点放进搜索框）必须是同一种反应**。
//
// ⚠️ **「另一个搜索框」量的是框架的 `ui.TextInput`**：收藏夹 / 本地音乐那枚
// 可展开的搜索（`searchbutton.go`）和歌词搜索框（`lyricssearch.go`）都是它，
// 而框架给它画焦点环的口径是 `inputBorder`（`mygo@v0.7.3/ui/form.go:274-282`）：
// **有焦点 → `t.Accent`**。
//
// ⚠️ **深浅两套都量**：描边那一档跟着主题走，而浅色下主色和底色的差比深色下小。
func TestTheTopBarSearchBoxFocusesLikeTheOtherSearchBox(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			s, tt := newShell(t, systemIsDark(dark))
			tt.SetFocused(true)
			tt.Move(900, 500)
			tt.Frame()
			th := BuildTheme(s.app.SettingsSnapshot(), dark)

			// ① 顶栏那一枚：点进去（点完把指针挪开，免得量到的是悬停那一档）。
			if err := tt.Click(searchLabel); err != nil {
				t.Fatalf("点不到搜索框：%v", err)
			}
			tt.Move(900, 500)
			tt.Frame()
			r, ok := tt.Find(searchLabel)
			if !ok {
				t.Fatalf("顶栏上没有搜索框，文字有 %q", tt.Texts())
			}
			mid := int(r.Y + r.H/2)
			// 描边在框里那个采样点的左边一像素（框架的 `Border` 是「盒子里那条线」）。
			borderX := int(r.X) - (searchPadX + iconMd + searchGap) - 1
			got := tt.Image().RGBAAt(clampX(tt.Image(), borderX), mid)

			// ② 框架那一枚：同一个主题、同一个控件。
			//
			// ⚠️ **摆在离左边 20 的地方**：描边是「盒子里那条线」，而输入框自己的
			// 盒子从 20 开始——量 x=19 量到的是框外的留白。
			var text string
			other := ui.NewTester(func(c *ui.Context) {
				c.SetTheme(th)
				ui.Row(c).Padding(20, 20).Children(func() {
					ui.TextInput(c, &text).Label("别的搜索框").Width(240).Height(searchHeight).
						AutoFocus()
				})
			}, 600, 200)
			other.Frame()
			other.Frame()
			otherGot := other.Image().RGBAAt(20, 20+int(searchHeight)/2)

			// 两枚的描边得是**同一个颜色**：都是主色。
			if d := maxDiff(got, rgb(th.Accent)); d > 10 {
				t.Errorf("顶栏那个搜索框聚焦时的描边是 %v，want 主色 %v（差 %d）", got, th.Accent, d)
			}
			if d := maxDiff(otherGot, rgb(th.Accent)); d > 10 {
				t.Errorf("框架那一枚输入框聚焦时的描边是 %v，want 主色 %v（差 %d）", otherGot, th.Accent, d)
			}
			if d := maxDiff(got, otherGot); d > 10 {
				t.Errorf("顶栏那个搜索框的描边 %v 和框架那一枚 %v 不是同一个颜色（差 %d）——"+
					"同一个动作两种反应", got, otherGot, d)
			}
		})
	}
}

// TestTheTopBarSearchBoxHasAVisibleFocusRing 钉验收第 1 条：**聚焦时有一圈看得见
// 的指示**（浅色主题也要看得见）。
//
// 判据是**那一圈和没聚焦时不一样，而且明显不一样**：聚焦前是主题的描边色，
// 聚焦后是主色——两者差得开，用户才知道「焦点在这儿」。
//
// ⚠️ **深浅两套都量**：浅色下主色和描边色的差比深色下小（深色下描边是半透明白），
// 只量一套就只验了一半。
func TestTheTopBarSearchBoxHasAVisibleFocusRing(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			s, tt := newShell(t, systemIsDark(dark))
			tt.SetFocused(true)
			tt.Move(900, 500)
			tt.Frame()
			th := BuildTheme(s.app.SettingsSnapshot(), dark)

			r, ok := tt.Find(searchLabel)
			if !ok {
				t.Fatalf("顶栏上没有搜索框，文字有 %q", tt.Texts())
			}
			mid := int(r.Y + r.H/2)
			borderX := clampX(tt.Image(), int(r.X)-(searchPadX+iconMd+searchGap)-1)

			// ① 静止那一档：描边是主题的描边色（半透明，压在框里那层底上）。
			//
			// ⚠️ **底色要合成两层**：框里那层灰自己也是半透明的（20% 的灰压在
			// 顶栏底色上），描边压的是**合成之后**的那一层。
			idle := tt.Image().RGBAAt(borderX, mid)
			fill := over(searchFieldBg(th, false), th.Background)
			wantIdle := overRGBA(th.Border, fill)
			if d := maxDiff(idle, wantIdle); d > 8 {
				t.Errorf("静止时搜索框的描边是 %v，want %v（主题的描边色压在底色上）", idle, wantIdle)
			}

			// ② 聚焦那一档：描边换主色。
			if err := tt.Click(searchLabel); err != nil {
				t.Fatalf("点不到搜索框：%v", err)
			}
			tt.Move(900, 500)
			tt.Frame()
			focused := tt.Image().RGBAAt(borderX, mid)
			if d := maxDiff(focused, rgb(th.Accent)); d > 10 {
				t.Errorf("聚焦时搜索框的描边是 %v，want 主色 %v（差 %d）", focused, th.Accent, d)
			}
			// ③ 两档要**分得开**：不然这一圈线就白画了。
			if d := maxDiff(focused, idle); d < 8 {
				t.Errorf("聚焦那一档的描边 %v 和静止那一档 %v 几乎一样（差 %d）——"+
					"用户看不出焦点在哪儿", focused, idle, d)
			}
		})
	}
}

// overRGBA 把一层半透明的 fg 合到已经合成好的 bg 上。
//
// ⚠️ **框里那层底自己也是半透明的**（20% 的灰压在顶栏底色上），而描边压的是
// **合成之后**的那一层——所以要合成两次。
func overRGBA(fg ui.Color, bg color.RGBA) color.RGBA {
	return over(fg, ui.Color{R: bg.R, G: bg.G, B: bg.B, A: 255})
}
