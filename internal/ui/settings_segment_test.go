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
	"time"

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

// settingsScenes 是**设置页最坏的几档配置**。
//
// ⚠️ **每一档都是用户碰得到的**：侧栏能拉到 480（`app.MaxSideMenuWidth`），
// 窗口最小是 900（`WindowOptions.MinWidth`），系统字号最大 1.4 倍。
//
// ⚠️ **「侧栏 480」这一档是评审抓出来的那一档**：说明列的下限（320）加上控件列
// 的下限（260）加上缝（24）是 604，而那一档的内容区只有 900 - 480 - 56 = **364**
// ——**放不下**。框架的 flex **没有收缩**（一项的 `shrink` 零值是 0，
// `mygo/ui/layout.go` 的 `resolveFlexible`），所以放不下就是**溢出**：控件列会被
// 推出窗口右缘，用户看见的是半个按钮。
var settingsScenes = []struct {
	name  string
	winW  int
	side  float64
	scale float32
}{
	{"1200 默认", 1200, app.DefaultSideMenuWidth, 1},
	{"900 默认", 900, app.DefaultSideMenuWidth, 1},
	{"900 侧栏 480", 900, app.MaxSideMenuWidth, 1},
	{"900 侧栏 320", 900, 320, 1},
	{"900 字号 1.4", 900, app.DefaultSideMenuWidth, 1.4},
	{"900 侧栏 480 + 字号 1.4", 900, app.MaxSideMenuWidth, 1.4},
	{"870 默认（比最小尺寸还窄）", 870, app.DefaultSideMenuWidth, 1},
}

// TestTheSettingsControlColumnIsNeverCutOff 钉住「控件列不会被推出内容区」。
//
// # 这是评审抓出来的那条回归
//
// 给说明列加了下限（`settingsDescMinW` = 320）之后，在**内容区放不下**
// 320 + 24 + 260 = 604 的那些配置里（窗口 900 + 侧栏拉到 480、内容区只有 364；
// 或者 1.4 倍字号下控件列要 420），说明列的**硬下限**把控件列**推出窗口右缘**：
// 主题那排、显示样式那排、色卡、字体下拉全被切掉——而基线（改动前）在同样的
// 配置下是**完整可见**的。
//
// # 判据是「控件列的右沿 ≤ 内容那一段的右沿」
//
// 这一条比「说明列有多宽」要紧：说明列窄一点只是难读，控件被切掉是**没法用**。
//
// ⚠️ **量法**：用 `settingRect` 把「主题模式」那一排**滚进可视区**再量它的盒子
// ——不滚的话量到的是旧一帧的盒子，在滚动过的页面上会量错（评审那一版就量错过）。
func TestTheSettingsControlColumnIsNeverCutOff(t *testing.T) {
	for _, sc := range settingsScenes {
		t.Run(sc.name, func(t *testing.T) {
			tt, s, _, _ := shellOnSettings(t, sideSceneSettings(sc.side), at(sc.winW, 800),
				withPrefs(mygoui.Preferences{TextScale: sc.scale}))
			// 侧栏宽度有 200ms 的过渡，推钟让它到位（否则量的是动画中间那一档）。
			for range 4 {
				tt.Advance(200 * time.Millisecond)
				tt.Frame()
			}
			homeFrame(tt)

			ctrl := settingRect(t, tt, s, "主题模式")
			right := settingsContentRight(sc.winW, sc.side)
			if ctrl.X+ctrl.W > right+1 {
				t.Errorf("控件列的右沿在 %.1f，而内容那一段的右沿是 %.1f——"+
					"控件被推出内容区了（窗口 %d / 侧栏 %.0f / 字号 %.1f）",
					ctrl.X+ctrl.W, right, sc.winW, sc.side, sc.scale)
			}
			if ctrl.W < settingsControlMinW-0.5 {
				t.Errorf("控件列只有 %.1f 宽，want ≥ %.1f——窄过这个数，"+
					"「跟随系统」那一档会被裁成半个词",
					ctrl.W, float32(settingsControlMinW))
			}
		})
	}
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
//   - **900（最小尺寸）与 1200**：说明列不小于 `settingsDescMinW`；
//   - **900 相对 1200 那一档**：说明列掉不超过 44%（票面那条验收）；
//   - **放不下的那些档**（侧栏 480 / 1.4 倍字号）：说明列让位，但**控件列完整**
//     （上一条钉的是那一条）。
//
// ⚠️ **下限是条件性的**：内容区放不下 320 + 缝 + 控件列基准宽的时候**不给**
// 下限——那一档宁可说明列窄一点，也不能把控件推出窗口（`settingsDescFloor`
// 的注释写着整段来由）。
func TestTheSettingsDescriptionColumnKeepsItsWidth(t *testing.T) {
	wide := settingsDescWidth(t, 1200, app.DefaultSideMenuWidth, 1)
	if wide <= 0 {
		t.Fatalf("1200 那一档的说明列没量出来")
	}

	// 票面那条验收：最小尺寸那一档的说明列掉不超过 44%。
	floor := wide * 0.56
	if float32(settingsDescMinW) < floor {
		t.Errorf("说明列的下限 %.1f 比 1200 那一档的 56%%（%.1f）还小——"+
			"「掉到 44%%」那条验收过不去", float32(settingsDescMinW), floor)
	}

	for _, sc := range settingsScenes {
		if sc.winW < 900 {
			continue // 那一档说明列本来就放不下下限（见 `settingsDescFloor`）
		}
		desc := settingsDescWidth(t, sc.winW, sc.side, sc.scale)
		if sc.side == app.DefaultSideMenuWidth && sc.scale == 1 {
			// 默认侧栏 + 1.0 倍字号：内容区放得下 ⇒ 下限生效。
			if desc < settingsDescMinW {
				t.Errorf("%s：说明列是 %.1f 宽，want ≥ %.1f——说明列有最小宽度"+
					"（UI 报告 §4 的 F-27）", sc.name, desc, float32(settingsDescMinW))
			}
			if desc < floor {
				t.Errorf("%s：说明列是 %.1f 宽，1200 那一档是 %.1f——掉到 %.0f%%，"+
					"比 44%% 还狠（UI 报告 §4 的 F-27）",
					sc.name, desc, wide, 100*(1-desc/wide))
			}
		}
	}
}

// sideSceneSettings 造一份「侧栏宽是 scene 那一档」的设置。
func sideSceneSettings(side float64) app.Settings {
	st := app.DefaultSettings("")
	st.SideMenuWidth = side
	return st
}

// settingsContentRight 是内容那一段（**去掉左右内边距**）的右沿。
//
// ⚠️ **按窗口坐标算，不受滚动影响**（设置页滚的是纵向）。
func settingsContentRight(winW int, side float64) float32 {
	avail := float32(winW) - float32(side)
	block := float32(settingsContentMaxW)
	if avail < block {
		block = avail
	}
	// 内容那一段居中：左右各留 (可视区 - 内容段) / 2。
	left := float32(side) + (avail-block)/2 + settingsContentPad
	return left + block - 2*settingsContentPad
}

// settingsDescWidth 量某一档配置下设置页的说明列有多宽。
//
// ⚠️ **说明列的宽是推出来的**：它没有自己的元素（左边那个 `Column` 没有 Label），
// 所以按「控件列的左沿 - 缝 - 内容那一段的内左沿」算。
func settingsDescWidth(t *testing.T, winW int, side float64, scale float32) float32 {
	t.Helper()
	tt, s, _, _ := shellOnSettings(t, sideSceneSettings(side), at(winW, 800),
		withPrefs(mygoui.Preferences{TextScale: scale}))
	for range 4 {
		tt.Advance(200 * time.Millisecond)
		tt.Frame()
	}
	homeFrame(tt)

	head, ok := tt.Find("外观")
	if !ok {
		t.Fatalf("窗口 %d：设置页上没有「外观」那个分组标题，文字有 %q", winW, tt.Texts())
	}
	ctrl := settingRect(t, tt, s, "主题模式")
	// 「外观」那个标题可能被滚出可视区了 ⇒ 用它的横坐标（不受纵向滚动影响）。
	return ctrl.X - space.Px24 - head.X
}

// TestTheProxyTypeSelectKeepsItsOwnWidth 钉住「代理页那个下拉还是 240 宽」。
//
// ⚠️ **它不归设置页那一列管**：代理页是**两列布局**，那一格宽是上游的
// `w-[240px]`（`proxySelectWidth`）。给 `enumSelect` 顺手加的
// `MinWidth(settingsControlMinW)`（260）会把那个下拉撑成 260——右沿比它上面
// 那行标题多出 20 像素，右边缘不再对齐（评审抓出来的那条 minor）。
//
// ⚠️ **所以下限是每一格自己给的**（`enumSelect` 的 `minW` 参数）：设置页那一列
// 给 260，代理页给 0（不设）。
func TestTheProxyTypeSelectKeepsItsOwnWidth(t *testing.T) {
	tt, _, _, _ := shellOnSettings(t, app.DefaultSettings(""))
	openSettingsTab(t, tt, "代理设置")

	sel, ok := tt.Find("选择代理类型")
	if !ok {
		t.Fatalf("代理页上没有「选择代理类型」那个下拉，文字有 %q", tt.Texts())
	}
	if sel.W != float32(proxySelectWidth) {
		t.Errorf("代理类型那个下拉是 %.1f 宽，want %.1f——它是两列布局里的一格，"+
			"宽度是上游的 w-[240px]，不归设置页那一列的下限管",
			sel.W, float32(proxySelectWidth))
	}
}
