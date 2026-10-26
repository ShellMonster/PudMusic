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

				// 没选中的那一档：自己**没有底色**，露出的是这一排共用的那条轨道
				// （设计规范 §7.9：轨道 = 背景色混 6% 的文字色）。三处走同一个出口，
				// 所以轨道色也只有一种；框架 `ui.Segmented` 的白色滑块不回到设置页。
				track := theme.Background.Mix(theme.Text, 0.06)
				gotOther := modeOf(bandPixels(tt.Image(), buttonPadSpot(other)))
				if !nearRGBA(gotOther, rgbaOf(track), 3) {
					t.Errorf("「%s」没选中的那一档（%q）底色是 %v，want %v（轨道色）",
						spot.what, spot.notChos, gotOther, rgbaOf(track))
				}
			}
		})
	}
}

// buttonPadSpot 是「文字右边那一小块内边距」——纯底色，没有字。
//
// ⚠️ **取样点挪到了文字右边**（票 #297 之前取的是左边）：那一排现在每一档都带
// 一枚 18px 图标，而图标在**文字左边**——左边那一段量到的是图标的墨迹，不是
// 底色（墨迹和底色的差是「图标深浅」那件事，量出来就废了）。
// 右边那一段是按钮自己的内边距（图标那一档 10 像素，纯文字那一档 14 像素），
// 是纯底色。
func buttonPadSpot(text mygoui.Rect) mygoui.Rect {
	return mygoui.Rect{
		X: text.X + text.W + 2,
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

// TestTheSettingsControlColumnIsNeverCutOff 钉住「控件列不会被推出分组卡片」。
//
// # 这是评审抓出来的那条回归 + 2026-10-26 的那条
//
// 给说明列加了硬下限（当年是 320）之后，在**内容区放不下**的那些配置里
// （窗口 900 + 侧栏拉到 480、内容区只有 364；或者 1.4 倍字号），说明列的硬下限
// 把控件列**推出窗口右缘**：主题那排、显示样式那排、色卡、字体下拉全被切掉。
// 后来说明全部删掉了（#530），但控件列里的控件还是**写死基准宽**——2026-10-26
// 实测 900 宽的下拉把分组卡片右缘溢出 14 像素（卡片左右各 17 的内边距没算进
// 宽度算式）。现在这一列里**每一个控件**都走 `settingsControlFitW`（按卡片里
// 那一行算）。
//
// # 判据是「控件列的右沿 ≤ 分组卡片的内右沿」
//
// 这一条比「控件有多宽」要紧：控件窄一点只是难看，控件被切掉是**没法用**。
// 右沿按**卡片内缘**算（内容段右沿往里一个卡片内边距）——宽度算式按哪张边，
// 判据就按哪张边，不然量不出「挤出卡片」那种坏。
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
			// 分组卡片的内右沿：内容段右沿往里一个卡片内边距（16 内边距 + 1 描边）。
			right := settingsContentRight(sc.winW, sc.side, sc.scale) - settingsCardInset
			if ctrl.X+ctrl.W > right+1 {
				t.Errorf("控件列的右沿在 %.1f，而分组卡片的内右沿是 %.1f——"+
					"控件被推出卡片了（窗口 %d / 侧栏 %.0f / 字号 %.1f）",
					ctrl.X+ctrl.W, right, sc.winW, sc.side, sc.scale)
			}
		})
	}
}

// TestTheSettingsControlColumnKeepsItsFullWidth 钉住「默认配置下控件列拿到完整的
// 基准宽」。
//
// # 它接替的是「说明列有最小宽度」那条
//
// 说明列的下限（F-27）护的是「左边那句话别被挤扁」。#530 把设置页的说明全部
// 删了，那一列不复存在，下限也跟着休眠（`settingRowElAt` 只在说明存在时给）。
// 这一条反过来护**另一头**：说明没了，控件列不该再被任何东西挤着——默认的几档
// 配置下，主题那排必须是完整基准宽（340；1.4 倍字号按缩放抬到 476），宽了装不下
// 四档、窄了「跟随系统」就被裁成半个词。
//
// ⚠️ **极限档（侧栏拉到 480）不在此列**：那一档控件列就是放不下 340，让它按
// `settingsControlFitW` 收窄（上一条钉着它不溢出）。收窄之后标签裁半是刻意选的
// 让步，见 `settingsSegment`。
func TestTheSettingsControlColumnKeepsItsFullWidth(t *testing.T) {
	for _, sc := range []struct {
		name  string
		winW  int
		side  float64
		scale float32
	}{
		{"1200 默认", 1200, app.DefaultSideMenuWidth, 1},
		{"900 默认", 900, app.DefaultSideMenuWidth, 1},
		{"900 侧栏 320", 900, 320, 1},
		{"900 字号 1.4", 900, app.DefaultSideMenuWidth, 1.4},
	} {
		t.Run(sc.name, func(t *testing.T) {
			tt, s, _, _ := shellOnSettings(t, sideSceneSettings(sc.side), at(sc.winW, 800),
				withPrefs(mygoui.Preferences{TextScale: sc.scale}))
			for range 4 {
				tt.Advance(200 * time.Millisecond)
				tt.Frame()
			}
			homeFrame(tt)

			ctrl := settingRect(t, tt, s, "主题模式")
			want := float32(settingsControlW) * sc.scale
			if ctrl.W < want-0.5 {
				t.Errorf("控件列只有 %.1f 宽，want ≥ %.1f——默认配置下主题那排四档"+
					"要完整的基准宽，窄了「跟随系统」就被裁成半个词",
					ctrl.W, want)
			}
		})
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
//
// ⚠️ **上限跟着字号缩放走**（`settingsContentMaxWAt`）：这里算的是**这一帧**的
// 宽度，所以字号那一档也得算进去（`scale` 是系统的文本缩放，1 = 不缩放）。
func settingsContentRight(winW int, side float64, scale float32) float32 {
	avail := float32(winW) - float32(side)
	block := settingsContentMaxW * scale
	if avail < block {
		block = avail
	}
	// 内容那一段居中：左右各留 (可视区 - 内容段) / 2。
	left := float32(side) + (avail-block)/2 + settingsContentPad
	return left + block - 2*settingsContentPad
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
