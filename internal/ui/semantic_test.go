// 这一组验**票 #172**：语义色、深色层次、分隔线（DESIGN.md §四「颜色与状态」）。
//
// # 三件事，三组判据
//
//  1. **三个语义色和上游一致**——危险 `#f31260`、警告 `#f5a524`、成功
//     `#17c964`，深浅两套**同一个值**（上游那三档本身不分深浅）。
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

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/download"
	mygoui "github.com/egoist/mygo/ui"
)

// 上游那三个语义色和那道分隔线（DESIGN.md §四）。
//
// ⚠️ **写死在这里，而不是从主题里取**：这一组要断的就是「和上游一致」，
// 从主题里取就变成自己跟自己比了。
const (
	upstreamDanger  = "#f31260"
	upstreamWarning = "#f5a524"
	upstreamSuccess = "#17c964"
	// upstreamDividerLight 是浅色下的分隔线。
	upstreamDividerLight = "#dbdbdb"
	// upstreamDividerDarkA 是深色下那道线的浓度：15% 白。
	upstreamDividerDarkA = 0.15
)

// ─────────────────────────────────────────────────────────────────────────────
// ① 三个语义色
// ─────────────────────────────────────────────────────────────────────────────

// TestTheSemanticColorsMatchUpstream 断主题里那三个语义色就是上游那三个值。
//
// ⚠️ **深浅两套是同一个值**：上游 HeroUI 的 `danger` / `warning` / `success`
// 本身不分深浅。改成一深一浅就是「自己发明了一个上游没有的配色」。
func TestTheSemanticColorsMatchUpstream(t *testing.T) {
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
				{"danger", th.Danger, upstreamDanger},
				{"warning", th.Warning, upstreamWarning},
				{"success", th.Success, upstreamSuccess},
			} {
				if c.got != mygoui.Hex(c.want) {
					t.Errorf("%s 是 %v，want %s（上游的色相）", c.what, c.got, c.want)
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
				{"色-危险", upstreamDanger},
				{"色-警告", upstreamWarning},
				{"色-成功", upstreamSuccess},
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

// TestTheDividerMatchesUpstream 断分隔线那一个值和上游一致。
//
// 浅色：#dbdbdb（上游 `divider_light`，写死的）。
//
// 深色：**15% 白叠在当前的底色上**（上游 `divider_dark` =
// `rgba(255,255,255,0.15)`）。底色是默认的 `#18181b` 时叠出来是 `#3b3b3d`
// ——**不是**一个写死的数：用户自己挑了底色之后，那道线要跟着**那个**底色重算。
// 写死就是「按默认底色算的线压在用户挑的底上」。
func TestTheDividerMatchesUpstream(t *testing.T) {
	light := BuildTheme(app.DefaultSettings(""), false)
	if want := mygoui.Hex(upstreamDividerLight); light.Border != want {
		t.Errorf("浅色下的分隔线是 %v，want %v（上游的 divider_light）", light.Border, want)
	}

	dark := BuildTheme(app.DefaultSettings(""), true)
	// 15% 白叠在默认底色 #18181b 上：24 + (255-24)×0.15 = 58.65 → 59（0x3b），
	// 蓝通道 27 + (255-27)×0.15 = 61.2 → 61（0x3d）。
	if want := mygoui.RGB(59, 59, 61); dark.Border != want {
		t.Errorf("深色下的分隔线是 %v，want %v（15%% 白叠在 #18181b 上）", dark.Border, want)
	}

	// 挑一个别的底色：分隔线必须跟着重算，而不是还守着默认底色那个数。
	custom := app.DefaultSettings("")
	custom.BackgroundColor = "#202024"
	custom.ThemeMode = app.ThemeModeDark
	cd := BuildTheme(custom, true)
	want := mygoui.RGB(32, 32, 36).Mix(mygoui.RGB(255, 255, 255), upstreamDividerDarkA)
	if cd.Border != want {
		t.Errorf("换了底色之后分隔线是 %v，want %v（15%% 白叠在新底色上）", cd.Border, want)
	}
}

// TestTheDividerReachesThePixels 断那道线真的画成了那个颜色。
//
// 取的是**队列抽屉左缘那条 1 像素的描边**（`BorderStartWidth(1)` +
// `BorderColor(t.Border)`）：它就是分隔线那一个颜色本身，不是别的什么近似色，
// 而且是一条**竖直的直线**——采样点好定，��用躲开圆角那一段。
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
func TestTheCardIsThePageColor(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt := downloadCardScene(t, dark)
			img := tt.Image()
			// 卡在留白之后：x ∈ [60, 540]、y ∈ [60, 340]。表格只占顶上那一段，
			// 再下面那一块是**卡自己的底**。
			inside := img.RGBAAt(300, 250)
			outside := img.RGBAAt(30, 250)
			if inside != outside {
				t.Errorf("卡里那一块是 %v，卡外那一块是 %v——深色下它们应当是同一个颜色"+
					"（层次靠影，不靠底色）", inside, outside)
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
