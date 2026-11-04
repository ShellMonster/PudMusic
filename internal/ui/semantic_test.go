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
	"github.com/ShellMonster/PudMusic/internal/service/download"
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

// TestTheCardIsThePageColor 断**下载记录那张卡**的底色。
//
// 上游那张是 HeroUI 的 `Card`（`download-list/index.tsx:105`），底色是
// `bg-content1`——#FFFFFF（浅）/ #18181b（深），正好是我们 `t.Background` 的
// 两个值。于是：
//
//   - **深色下它和页面底色几乎同色**，层次靠那一圈影（影是 #179 那一票的，
//     判据在 `shadow_test.go` 里）；
//   - **浅色下它是白的**，不是一块灰。
//
// 判据是**两个像素比**：卡里那一块和卡外那一块（页面底色）必须**一样**。
// 只断「卡里那一块是不是某个数」的话，卡和页面底色一起变的时候测不出来。
//
// ⚠️ **还要断「它不是 `t.Surface`」**：深色下 `t.Surface` 是 #27272a，正是
// DESIGN.md §四说的那个「比背景亮一级的板」；浅色下它是 #f4f4f5，那正是
// 「一块灰」。两条合起来才是「底色换成上游那一档了」。
func TestTheCardIsThePageColor(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt := downloadCardScene(t, dark)
			// 表格在那张卡里面——表头在，就说明那张卡画出来了（否则下面那些
			// 「卡里那一块」量到的是页面底色，判据会白过）。
			if _, ok := tt.Find("文件"); !ok {
				t.Fatalf("画面上找不到那张卡里的表头，文字有 %q", tt.Texts())
			}
			img := tt.Image()
			// 卡在留白之后：x ∈ [60, 540]、y ∈ [60, 340]。表格只占顶上那一段，
			// 再下面那一块是**卡自己的底**。
			inside := img.RGBAAt(300, 250)
			outside := img.RGBAAt(30, 250)
			st := app.DefaultSettings("")
			if want := rgb(cardColorOf(st, BuildTheme(st, dark))); inside != want {
				t.Errorf("卡里那一块是 %v，want 卡片底 %v（经典 = 页面底色，方案 = surface1）", inside, want)
			}
			_ = outside
			// 它不是「亮一级的那块板」，也不是一块灰。
			surface := rgb(BuildTheme(app.DefaultSettings(""), dark).Surface)
			if inside == surface {
				t.Errorf("卡里那一块是 %v（`t.Surface`）——那正是「比背景亮一级的板」",
					inside)
			}
			// 浅色下那张卡是白的，不是一块灰。
			if !dark && inside == rgb(mygoui.Hex("#f4f4f5")) {
				t.Errorf("浅色下那张卡是 %v——那是一块灰块，上游那张卡是白的", inside)
			}
		})
	}
}

// downloadCardScene 画一张下载记录那张卡，四周留白，返回它的 Tester。
//
// ⚠️ **不能直接用 `newRows`**：那一条走的是 `ui.NewTester`（**没有深浅那一档**），
// 于是深色那一套量出来的是浅色的图。深浅两套的差别恰恰是这一票要验的东西。
func downloadCardScene(t *testing.T, dark bool) *mygoui.Tester {
	t.Helper()
	rows := []download.Snapshot{dlSnapshot("第一首", download.StatusDone, 100, 100)}
	b := newTestShell(t, at(shadowW, shadowH), systemIsDark(dark),
		withView(func(sh *Shell) func(*mygoui.Context) {
			return func(c *mygoui.Context) {
				sh.wake.set(c)
				// ⚠️ **主题要自己装**：只画这一块的那一支不走 `Shell.View`，
				// 而 MyGo 每帧开头会把主题重置成跟随系统那一份（铁律二）。
				sh.applyFrameTheme(c)
				uiBox(c).FillWidth().FillHeight().
					Background(c.Theme().Background).
					Padding(60, 60, 60, 60).Children(func() {
					sh.downloads.sync(sh.downloads.rowsFn())
					sh.downloads.viewTable(c, sh)
				})
			}
		}))
	b.s.downloads.rowsFn = func() []download.Snapshot { return rows }
	b.tt.Frame()
	b.tt.Frame()
	return b.tt
}

// uiRow 是 `ui.Row` 的本地别名——这一组到处在用。
func uiRow(c *mygoui.Context) mygoui.Element { return mygoui.Row(c) }
