// 票 #240 的判据：设置页只有**一种**分段控件（F-17），说明列有**最小宽度**（F-27）。
//
// 两条都对着**画面**断言，不对着代码断言：
//
//   - F-17 量的是**选中那一档的底色**（三处都该是实心主色）和**没选中的那一档**
//     （三处都该是页面底色，也就是**没有灰底轨道**）；
//   - F-27 量的是**列的宽**（说明列 / 控件列），以及窄窗口下**控件列会不会让位**。
//
// ⚠️ **取样点在按钮的左内边距里**：那一段是纯底色（没有字，也没有抗锯齿的边），
// 量到的是底色本身。取「文字左边 12~6 像素」那一小条——按钮左内边距是 14 宽
// （`flat.Button` 的 `t.Space(3.5)`），再往左就出了按钮。
package ui

import (
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	mygoui "github.com/egoist/mygo/ui"
)

// settingsSegmentSpots 是设置页上那三处分段控件，**每一处的选中档和没选中的档**。
//
// ⚠️ **三处的默认选中档**（`app.DefaultSettings("")`）：
// 「主题」= 跟随系统（`ThemeModeSystem`）、「数据显示样式」= 列表（`DisplayModeList`）、
// 「窗口关闭」= 隐藏到托盘（`CloseWindowHide`）。
var settingsSegmentSpots = []struct {
	what    string
	chosen  string
	notChos string
}{
	{"主题", "跟随系统", "浅色"},
	{"数据显示样式", "列表", "网格"},
	{"窗口关闭", "隐藏到托盘", "直接退出"},
}

// TestTheSettingsPageHasOnlyOneSegmentedControl 钉住「设置页只有一种分段控件」。
//
// # 此前同一页上有两套
//
// 「数据显示样式」是框架的 `ui.Segmented`（**灰底轨道 + 白色滑块**），「主题」和
// 「窗口关闭」是自绘的按钮（**没有轨道，选中的那一档是实心主色底**）。两个并列的
// 分段控件长得不一样，用户一眼看不出它们是同一类东西（Apple HIG「一致性：相同功能
// 的控件外观相同」；UI 报告 §4 的 F-17）。
//
// # 判据是画面上的两个数
//
//   - **选中的那一档**：三处都必须是**实心主色底**（`flatSolid`：底 `t.Accent`）。
//   - **没选中的那一档**：三处都必须是**页面底色**——也就是**没有灰底轨道**。
//     框架 `ui.Segmented` 的轨道是 `t.Surface`（浅色下 #f4f4f5），和页面底色差得开，
//     所以这一条钉得住「灰轨 + 白滑块」那种画法不会回来。
//
// ⚠️ **深浅两套都要过**：主色和页面底色在两套主题里是两组不同的数。
func TestTheSettingsPageHasOnlyOneSegmentedControl(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""), systemIsDark(dark))
			homeFrame(tt)
			theme := BuildTheme(app.DefaultSettings(""), dark)

			for _, spot := range settingsSegmentSpots {
				// ⚠️ 「窗口关闭」那一行在设置页的最下面，得先滚进可视区才能取到像素。
				chosen := settingRect(t, tt, s, spot.chosen)
				other := settingRect(t, tt, s, spot.notChos)

				got := modeOf(bandPixels(tt.Image(), buttonPadSpot(chosen)))
				if !nearRGBA(got, rgbaOf(theme.Accent), 3) {
					t.Errorf("「%s」选中的一档（%q）底色是 %v，want %v（实心主色底）——"+
						"设置页的分段控件只有一种画法（UI 报告 §4 的 F-17）",
						spot.what, spot.chosen, got, rgbaOf(theme.Accent))
				}

				// 没选中的那一档：**没有底色**，就是页面底色。
				gotOther := modeOf(bandPixels(tt.Image(), buttonPadSpot(other)))
				if !nearRGBA(gotOther, rgbaOf(theme.Background), 3) {
					t.Errorf("「%s」没选中的那一档（%q）底色是 %v，want %v（页面底色）——"+
						"框架 `ui.Segmented` 那种灰底轨道不许回到设置页来",
						spot.what, spot.notChos, gotOther, rgbaOf(theme.Background))
				}
			}
		})
	}
}

// buttonPadSpot 是「文字左边那一小块内边距」——纯底色，没有字。
func buttonPadSpot(text mygoui.Rect) mygoui.Rect {
	return mygoui.Rect{
		X: text.X - 12,
		Y: text.Y + text.H/4,
		W: 6,
		H: text.H / 2,
	}
}

// settingsCols 是某一档窗口下设置页那几列的量出来的数。
type settingsCols struct {
	desc      float32 // 说明列的宽
	ctrl      float32 // 控件列的宽
	ctrlRight float32 // 控件列的右沿
	contentR  float32 // 内容那一段（去内边距）的右沿
}

// TestTheSettingsDescriptionColumnKeepsItsWidth 钉住「说明列有最小宽度」。
//
// # 那一列原来是被挤扁的
//
// 每一行是「左边标题 + 一句说明，右边定宽控件」，控件那一列是定宽的（300），
// 说明那一列吃剩下的——窗口越窄说明被挤得越狠：1200 宽时那一列是 520，900 宽
// （`WindowOptions.MinWidth` 定的最小尺寸）时只剩 320，**掉掉 38%**；说明文字和
// 控件之间的留白从 351 掉到 151（UI 报告 §4 的 F-27）。
//
// # 判据
//
//   - **900（最小尺寸）**：说明列不小于 `settingsDescMinW`，控件列还是基准宽 300，
//     控件的右沿**没有超出**内容那一段的右沿（没被推出内容区）；
//   - **900 相对 1200 那一档**：说明列掉不超过 44%（票面那条验收）；
//   - **870**：说明列**仍然**是 `settingsDescMinW`——这一条是**下限真的生效**的证据。
//     900 那一档上 320 + 24 + 300 正好是内容那一段的宽，下限**刚好不触发**；要看见
//     它触发，得再窄一点。870 是**刚好**放得下「说明列 320 + 缝 24 + 控件列 260」
//     的那个宽度，比最小窗口窄 30。
func TestTheSettingsDescriptionColumnKeepsItsWidth(t *testing.T) {
	const sideW = 200 // 默认侧栏宽（app.DefaultSideMenuWidth）

	wide := settingsColsAt(t, 1200, sideW)
	if wide.desc <= 0 {
		t.Fatalf("1200 那一档的说明列没量出来")
	}

	// 票面那条验收：最小尺寸那一档的说明列掉不超过 44%。
	floor := wide.desc * 0.56
	if float32(settingsDescMinW) < floor {
		t.Errorf("说明列的下限 %.1f 比 1200 那一档的 56%%（%.1f）还小——"+
			"「掉到 44%%」那条验收过不去", float32(settingsDescMinW), floor)
	}

	for _, c := range []struct {
		winW int
		// wantMin 是这一档说明列的下限。
		wantMin float32
	}{
		{900, settingsDescMinW}, // 最小尺寸
		{870, settingsDescMinW}, // 更窄：下限要生效
	} {
		got := settingsColsAt(t, c.winW, sideW)
		if got.desc < c.wantMin {
			t.Errorf("窗口 %d：说明列是 %.1f 宽，want ≥ %.1f——说明列有最小宽度，"+
				"控件列会让位（UI 报告 §4 的 F-27）",
				c.winW, got.desc, c.wantMin)
		}
		if got.desc < floor {
			t.Errorf("窗口 %d：说明列是 %.1f 宽，1200 那一档是 %.1f——掉到 %.0f%%，"+
				"比 44%% 还狠（UI 报告 §4 的 F-27）",
				c.winW, got.desc, wide.desc, 100*(1-got.desc/wide.desc))
		}
		// 900 那一档：控件列还是基准宽，右沿齐内容那一段的右沿。
		if c.winW == 900 {
			if got.ctrl != float32(settingsControlW) {
				t.Errorf("窗口 %d：控件列是 %.1f 宽，want %.1f——最小尺寸那一档"+
					"控件列还是基准宽", c.winW, got.ctrl, float32(settingsControlW))
			}
			if got.ctrlRight > got.contentR+1 {
				t.Errorf("窗口 %d：控件的右沿在 %.1f，而内容那一段的右沿是 %.1f——"+
					"控件被推出内容区了", c.winW, got.ctrlRight, got.contentR)
			}
		}
	}
}

// settingsColsAt 量某一档窗口下设置页的说明列、控件列和内容那一段的右沿。
//
// ⚠️ **说明列的宽是推出来的**：它没有自己的元素（左边那个 `Column` 没有 Label），
// 所以按「控件列的左沿 - 缝 - 内容那一段的内左沿」算。
func settingsColsAt(t *testing.T, winW, sideW int) settingsCols {
	t.Helper()
	tt, _, _, _ := shellOnSettings(t, app.DefaultSettings(""), at(winW, 800))
	homeFrame(tt)

	head, ok := tt.Find("外观")
	if !ok {
		t.Fatalf("窗口 %d：设置页上没有「外观」那个分组标题，文字有 %q", winW, tt.Texts())
	}
	// 标题在内容那一段的内边距之内，所以它的左沿就是内容那一段的**内**左沿。
	contentLeft := head.X

	themeRow, ok := tt.Find("主题模式")
	if !ok {
		t.Fatalf("窗口 %d：设置页上没有「主题模式」那一排，文字有 %q", winW, tt.Texts())
	}

	// 内容那一段的右沿：那一段是居中的，宽是 min(上限, 可视区)。
	avail := float32(winW - sideW)
	block := float32(settingsContentMaxW)
	if avail < block {
		block = avail
	}
	return settingsCols{
		desc:      themeRow.X - space.Px24 - contentLeft,
		ctrl:      themeRow.W,
		ctrlRight: themeRow.X + themeRow.W,
		contentR:  contentLeft + block - settingsContentPad,
	}
}
