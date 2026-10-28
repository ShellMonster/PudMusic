package ui

// 侧栏「当前在哪」改成**实心主色底**（票：用户要「深底色 + 白字」）。
//
// # 为什么改
//
// 原来那一档是 `accentSoft` 淡底（主色 14% / 深色 18%），而实测它和侧栏底色
// 的对比度只有 **1.09:1**（暖纸浅色）到 **1.29:1**（靛夜深色）——**几乎分不
// 出来**。用户要的是「一眼看出当前选的是哪一个」。
//
// 顺带修一个**算错了底板**的缺陷：`accentSoft` 拿的是**页面底色**
// `t.Background` 去混主色，而这一行实际压在**侧栏底色**（方案的 `sidebar`）
// 上——两者不是同一个色（暖纸浅色下 `#F8F4ED` vs `#EFE9DE`）。所以那一档底色
// 算得比设计意图更淡。
//
// # 口径
//
// 这一档是「**当前在哪**」——侧栏菜单当前项 + 侧栏收藏夹当前行，**同一列的
// 同一件事**，所以两处**同一个出口**。
//
// ⚠️ **字色不能写死白字**：主色是用户可设的，而深色方案的主色是**亮色**
// （暖纸深色 `#E58A5A`），白字压上去只有 **2.59:1**——读不出来。所以字取
// `t.AccentText`：那是框架按主色亮度挑出来的黑或白（`applyAccent`），**在主色
// 底上一定读得出来**。
//
// ⚠️ **次要字（「N 首」）也要按底重算**：`t.TextMuted` 压在实心主色底上只有
// **1.02:1 ~ 1.34:1**，完全读不出来。

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/ink"
	mygoui "github.com/egoist/mygo/ui"
)

// TestTheSelectedSidebarRowIsSolid 断**选中那一行是实心主色底**。
//
// ⚠️ **深浅两套都要断**：实心底这一档的字色在深浅两套里是**反的**（浅色白字、
// 深色近黑字），而底色两边都是主色。
func TestTheSelectedSidebarRowIsSolid(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			s, _, tt, _ := favShell(t, 2, 0, systemIsDark(dark))
			s.navigate(collectionPath(1))
			tt.Frame()

			cur, ok := tt.Find("我建的 1")
			if !ok {
				t.Fatalf("侧栏没有「我建的 1」那一行，文字有 %q", tt.Texts())
			}
			sib, ok := tt.Find("我建的 2")
			if !ok {
				t.Fatalf("侧栏没有「我建的 2」那一行，文字有 %q", tt.Texts())
			}
			theme := BuildTheme(app.DefaultSettings(""), dark)

			// 底色：就是主色本身。
			curPx := bandPixels(tt.Image(), cur)
			got := modeOf(curPx)
			if !nearRGBA(got, rgbaOf(theme.Accent), 3) {
				t.Errorf("选中那一行的底色是 %v，want %v（实心主色底）", got, theme.Accent)
			}
			// 旁边那一格**不该**也是主色底（不然「当前在哪」又没信号了）。
			sibPx := modeOf(bandPixels(tt.Image(), sib))
			if nearRGBA(sibPx, rgbaOf(theme.Accent), 3) {
				t.Errorf("没选中那一行也是主色底 %v——那就没有「当前」这一说了", sibPx)
			}
		})
	}
}

// TestTheSelectedSidebarRowTextIsReadable 断**字压得住那个底**。
//
// ⚠️ **判据是对比度，不是「等于某个数」**：主色是用户可设的，写死一个数的话
// 用户换主色就失效了。
//
// ⚠️ **次要字（「N 首」）也算**：它在实心主色底上压 `t.TextMuted` 只有
// 1.02:1 ~ 1.34:1——完全读不出来。
func TestTheSelectedSidebarRowTextIsReadable(t *testing.T) {
	for _, preset := range []app.ThemePreset{
		app.ThemePresetClassic, app.ThemePresetWarmPaper,
		app.ThemePresetDuskAmber, app.ThemePresetSoftPeach,
		app.ThemePresetCeladon, app.ThemePresetInkPaper,
		app.ThemePresetSandalwood, app.ThemePresetIndigoNight,
	} {
		for _, dark := range []bool{false, true} {
			shade := "浅色"
			if dark {
				shade = "深色"
			}
			st := app.DefaultSettings("")
			st.ThemePreset = preset
			th := BuildTheme(st, dark)
			for _, c := range []struct {
				what string
				from mygoui.Color
			}{
				{"收藏夹名（主色上的字）", flatSolidCurrentText(th)},
				{"N 首（次要字，按那层底重算）", flatSolidCurrentMutedText(th)},
			} {
				if r := ink.Ratio(c.from, th.Accent); r < 4.5 {
					t.Errorf("%s / %s：%v 压主色底 %v 只有 %.2f:1（< 4.5）",
						preset, shade, c.from, th.Accent, r)
				}
			}
		}
	}
}

// TestTheSelectedSidebarRowStandsOutFromTheSidebar 断**选中那一行和侧栏底色
// 分得开**。
//
// ⚠️ **这一条是这一票的**：「当前在哪」是这一列里唯一的方向标，而它和旁边
// 那一格的对比度只有 1.09:1（暖纸浅色）——用户看不出当前选的是哪一个。
//
// 判据取 **≥ 1.3:1**：实心主色底对侧栏底是 4.34:1 ~ 6.72:1，而淡底那一档是
// 1.09:1 ~ 1.29:1 —— 1.3 是把两档分开的那个线。
func TestTheSelectedSidebarRowStandsOutFromTheSidebar(t *testing.T) {
	for _, preset := range []app.ThemePreset{
		app.ThemePresetClassic, app.ThemePresetWarmPaper,
		app.ThemePresetDuskAmber, app.ThemePresetSoftPeach,
		app.ThemePresetCeladon, app.ThemePresetInkPaper,
		app.ThemePresetSandalwood, app.ThemePresetIndigoNight,
	} {
		for _, dark := range []bool{false, true} {
			shade := "浅色"
			if dark {
				shade = "深色"
			}
			st := app.DefaultSettings("")
			st.ThemePreset = preset
			th := BuildTheme(st, dark)
			sidebar := th.Background
			if sk, _, ok := presetSkinFor(preset, dark); ok {
				sidebar = mygoui.Hex(sk.sidebar)
			}
			if r := ink.Ratio(th.Accent, sidebar); r < 1.3 {
				t.Errorf("%s / %s：选中那一行的底（主色 %v）压侧栏底 %v 只有 %.2f:1（< 1.3）",
					preset, shade, th.Accent, sidebar, r)
			}
		}
	}
}
