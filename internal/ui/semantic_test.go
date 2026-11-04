// 这一组验语义色、卡片层次、分隔线。2026-11 起数值以「深夜唱片行」为准
// （DESIGN.md §0′、`widgets/tok`）：语义色和分隔线深浅各一份。
//
// # 三件事，三组判据
//
//  1. **三个语义色是令牌表那三档**——design-tokens.html 表 2-1 的落地值
//     （浅色小字也过 4.5:1），深浅各一份。
//  2. **深色下卡片和背景几乎同色，靠影分层**；**浅色下卡片不是灰块**
//     （上游 HeroUI 的 `bg-content1` 是 #FFFFFF / #18181b，正好是我们
//     `t.Background` 的两个值）。
//  3. **分隔线对齐上游**——深色是 15% 白叠在底色上，浅色是 #dbdbdb。
//
// # 判据都落在「画面上那个颜色是不是这个数」
//
// 主题里那个字段是什么值，只是「我们打算画成什么」；**画面上那个像素是什么
// 值**才是「用户看见的是什么」。所以每一件事都两头断：先断主题里的值，再断
// 画出来的那一块。
//
// ⚠️ **深浅两套都要跑**：深色下那三件事的差别最大（卡片和背景同色、分隔线是
// 叠出来的），只跑浅色的话深色下那一面一次都没被验过。
package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 令牌表里那三个语义色和那道分隔线（[浅色, 深色]）。
//
// ⚠️ **写死在这里，而不是从主题里取**：这一组要断的就是「和令牌表一致」，
// 从主题里取就变成自己跟自己比了。
var (
	tokenDanger  = [2]string{"#C93B3B", "#F0716B"}
	tokenWarning = [2]string{"#9A5B06", "#F2B04A"}
	tokenSuccess = [2]string{"#2E7D4F", "#5BC287"}
	tokenLine    = [2]string{"#E0E1E3", "#26282D"}
)

func shadeIdx(dark bool) int {
	if dark {
		return 1
	}
	return 0
}

// ─────────────────────────────────────────────────────────────────────────────
// ① 三个语义色
// ─────────────────────────────────────────────────────────────────────────────

// TestTheSemanticColorsMatchTheTokens 断主题里那三个语义色就是令牌表那三档。
func TestTheSemanticColorsMatchTheTokens(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			th := BuildTheme(app.DefaultSettings(""), dark)
			for _, c := range []struct {
				what string
				got  mygoui.Color
				want string
			}{
				{"danger", th.Danger, tokenDanger[shadeIdx(dark)]},
				{"warning", th.Warning, tokenWarning[shadeIdx(dark)]},
				{"success", th.Success, tokenSuccess[shadeIdx(dark)]},
			} {
				if c.got != mygoui.Hex(c.want) {
					t.Errorf("%s 是 %v，want %s（令牌表）", c.what, c.got, c.want)
				}
			}
		})
	}
}

// TestTheSemanticColorsReachThePixels 断那三个值真的画到了画面上。
//
// 画三块**实心**的色块（主题里那三个颜色），然后逐像素读回来——
// 主题里的值和画面上的值之间隔着框架的绘制那一层，那一层也可能把颜色改了
// （透明度、混合、宽色域），所以两头都得断。
func TestTheSemanticColorsReachThePixels(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt := shadowScene(t, dark, func(_ *Shell, c *mygoui.Context) {
				uiRow(c).Gap(10).Children(func() {
					for _, s := range []struct {
						label string
						color func(*mygoui.Theme) mygoui.Color
					}{
						{"色-危险", func(t *mygoui.Theme) mygoui.Color { return t.Danger }},
						{"色-警告", func(t *mygoui.Theme) mygoui.Color { return t.Warning }},
						{"色-成功", func(t *mygoui.Theme) mygoui.Color { return t.Success }},
					} {
						col := s.color(c.Theme())
						mygoui.Box(c).Size(60, 60).Background(col).Label(s.label)
					}
				})
			})
			img := tt.Image()
			for _, s := range []struct {
				label string
				want  string
			}{
				{"色-危险", tokenDanger[shadeIdx(dark)]},
				{"色-警告", tokenWarning[shadeIdx(dark)]},
				{"色-成功", tokenSuccess[shadeIdx(dark)]},
			} {
				r, ok := tt.Find(s.label)
				if !ok {
					t.Fatalf("画面上找不到那一块 %s", s.label)
				}
				got := img.RGBAAt(int(r.X+r.W/2), int(r.Y+r.H/2))
				if want := mygoui.Hex(s.want); got != rgb(want) {
					t.Errorf("%s 画出来是 %v，want %v（上游的色相）", s.label, got, want)
				}
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ② 分隔线
// ─────────────────────────────────────────────────────────────────────────────

// TestTheDividerMatchesTheTokens 断分隔线就是 `--line`（浅 #E0E1E3 / 深 #26282D）。
func TestTheDividerMatchesTheTokens(t *testing.T) {
	for _, dark := range []bool{false, true} {
		th := BuildTheme(app.DefaultSettings(""), dark)
		if want := mygoui.Hex(tokenLine[shadeIdx(dark)]); th.Border != want {
			t.Errorf("深色=%v：分隔线是 %v，want %v（--line）", dark, th.Border, want)
		}
	}
}

// TestTheDividerReachesThePixels 断那道线真的画成了那个颜色。
//
// 取的是**队列抽屉左缘那条 1 像素的描边**（`BorderStartWidth(1)` +
// `BorderColor(t.Border)`）：它就是分隔线那一个颜色本身，不是别的什么近似色，
// 而且是一条**竖直的直线**——采样点好定，不用躲开圆角那一段。
func TestTheDividerReachesThePixels(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt := shadowScene(t, dark, func(s *Shell, c *mygoui.Context) {
				s.drawerOpen = true
				s.viewQueueDrawer(c)
			})
			img := tt.Image()
			th := BuildTheme(app.DefaultSettings(""), dark)
			want := rgb(th.Border)
			// 抽屉贴在窗口右边，宽度 queueDrawerWidth，左缘就是那条线。
			got := img.RGBAAt(shadowW-queueDrawerWidth, 200)
			if got != want {
				t.Errorf("抽屉左缘那条线是 %v，want %v（主题里的分隔线）", got, want)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ③ 深色层次：卡片靠影，浅色卡片不是灰块
// ─────────────────────────────────────────────────────────────────────────────

// uiRow 是 `ui.Row` 的本地别名——这一组到处在用。
func uiRow(c *mygoui.Context) mygoui.Element { return mygoui.Row(c) }
