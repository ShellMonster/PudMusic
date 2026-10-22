// 界面骨架的三块底色：侧栏、内容区、播放条在深浅两套主题里都取**同一个**来源。
//
// 这一族钉的是票 #127 的头一条。改之前我们侧栏和播放条取 `t.Surface`、内容区
// 取 `t.Background`：浅色下那两个色差得开（`#f4f4f5` / `#ffffff`），一眼看过去
// 像「有层次」；**深色下差的是 `#27272a` / `#18181b`**——深浅两个通道各跳一级，
// 侧栏、内容区、播放条于是变成**三条带**，而它看上去完全正常：没有报错、没有
// 红字、每一块各自都合法。
//
// 所以判据全是**画面上的像素**，而且深浅两套都要出：只量浅色等于没量。
// `internal/ui` 里已有的 `TestImportingADarkThemeActuallyTurnsTheWholeWindowDark`
// 也读这一处的像素，两条各管一头（那条管「导入的主题有没有生效」，这一族管
// 「生效之后那三块是不是同一个颜色」）。
//
// ⚠️ 采样点一律选**空白处**，不落在任何一个控件上。落在一个控件上时读到的是
// 那个控件的颜色，而那一帧**看着像是「背景没统一」**，而真因离着三层远。
package ui

import (
	"image/color"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/ui"
)

// 三块底色的采样点。窗口是 1200×800（`newTestShell` 的默认），三块各取一个。
//
// 每一个都挑在**控件之间**的空处：侧栏中段（菜单和折叠键之间）、内容区右边
// （设置页收进 900 之后右边那块空地）、播放条左半（封面和中间那一列之间）。
const (
	// shotWinW / shotWinH 是这一族采样时那扇窗口的尺寸，也就是
	// `newTestShell` 不给 `at()` 时的默认。写成常量而不是散在算式里：下面
	// 好几个坐标是从它推出来的，写死一遍就有一处要改。
	shotWinW, shotWinH = 1200, 800

	sideProbeX, sideProbeY       = 100, 500
	contentProbeX, contentProbeY = 1160, 400
	// playbarProbeX 是播放条左半、封面和中间那一列之间那一块空地。
	//
	// ⚠️ **它原来在 x=600**（中间那一列），而那一列的进度行正好铺在那儿——
	// 票 #217 把播放键从 58 缩到 48 之后，中间那一列的**内容矮了 10 像素**，
	// 进度行跟着往上挪了 10，于是 x=600 那一格**落在了轨道上**，读出来的是
	// 轨道的颜色（不是底色），这一族就红了。
	//
	// 所以采样点挪到**封面右边、中间那一列左边**那一块空地：那里两套布局下都
	// 是空的（这一族自己的规矩：**采样点一律选空白处**）。
	playbarProbeX, playbarY = 150, 780

	// drawerProbe 是播放列表抽屉面板上的一个空白处：面板从
	// 窗口右缘往左 384（`queueDrawerWidth`），所以 x=1160 在里面。
	drawerProbeX, drawerProbeY = 1160, 300
)

// probeColor 是画面上那一个点的颜色。
func probeColor(tt *ui.Tester, x, y int) color.RGBA { return tt.Image().RGBAAt(x, y) }

// TestTheChromeSharesOneBackground 钉住侧栏、内容区、播放条是同一个底色。
//
// ⚠️ **深浅两套都要跑，而且 want 从当前主题现算**，不写死一个十六进制值。
// 写死的话改一次主题引擎就得回来改这一条，而「忘了改」的那一次是**恒绿**。
//
// 两套都跑不是为了「浅色那一套更严」——**浅色也抓得住**：断言是逐字节相等，
// `#f4f4f5` 和 `#ffffff` 差 11 也照样红（实测：把侧栏改回 `t.Surface`，浅色和
// 深色两个子测试各报一次）。两套都跑是为了**不让这一族自己只盯着深色**：这一票的
// 症状在深色下才明显（三个通道各跳一级，肉眼一眼是三条带），浅色下差 11、肉眼
// 未必看得出——而「靠肉眼看不看得出」正是这一族要换成读像素的理由。
func TestTheChromeSharesOneBackground(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newTestShell(t, systemIsDark(dark), at(shotWinW, shotWinH))
			s, tt := b.s, b.tt

			wantRGBA := rgbaOf(BuildTheme(s.app.SettingsSnapshot(), dark).Background)

			probes := []struct {
				what string
				x, y int
			}{
				{"侧栏", sideProbeX, sideProbeY},
				{"内容区", contentProbeX, contentProbeY},
				{"播放条", playbarProbeX, playbarY},
			}
			for _, p := range probes {
				if got := probeColor(tt, p.x, p.y); got != wantRGBA {
					t.Errorf("%s那一块 (%d,%d) 是 %v，want %v——底色和窗口不是同一个来源，"+
						"深色下界面会裂成三条带", p.what, p.x, p.y, got, wantRGBA)
				}
			}
		})
	}
}

// TestTheChromeStillHasASeparationFromTheBackground 钉住「统一底色」没有把层次
// 一起抹掉：侧栏右缘那条线还在，播放条上缘那道阴影还在。
//
// 这一条和上一条**互为反面**，缺一不可：上一条说三处底色**一样**，这一条说
// 它们**分得开**。只做前者的话，最省事的实现是「把整个窗口刷成一个颜色」，那
// 样第一族会全绿，而界面上侧栏和内容区之间、播放条和内容区之间，一条边都没有
// ——浅色下尤其致命：两个都是 `#ffffff`。
//
// Biu 靠的就是这两样，底色同源、分界另有办法：侧栏是 `border-divider/30 border-r-1`
// （`layout/side/index.tsx:145`），播放条是外层那个 `shadow-2xl`
// （`layout/index.tsx:48`）。
func TestTheChromeStillHasASeparationFromTheBackground(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newTestShell(t, systemIsDark(dark), at(shotWinW, shotWinH))
			s, tt := b.s, b.tt

			bg := rgbaOf(BuildTheme(s.app.SettingsSnapshot(), dark).Background)

			// 侧栏右缘那条竖线：分隔线画在侧栏框的**内侧**（`Border` 的文档：
			// 「draws a border of width DIPs inside the element's edges」），
			// 所以它在 x = 侧栏宽 − 1 那一列，而它右边那一列已经是内容区了。
			lineX := int(app.DefaultSideMenuWidth) - 1
			if line := probeColor(tt, lineX, sideProbeY); line == bg {
				t.Errorf("侧栏右缘 x=%d 那一条是底色 %v——分隔线没了，"+
					"浅色下侧栏和内容区会完全一样", lineX, line)
			}
			if got := probeColor(tt, lineX+1, sideProbeY); got != bg {
				t.Errorf("分隔线右边那一列是 %v，want 底色 %v——分隔线画在了侧栏外面", got, bg)
			}

			// 播放条上缘那道阴影。播放条贴在窗口**底下**，所以它的上缘在
			// y = 窗口高 − 88；阴影往上洒，往上十几像素要有一段不是底色的过渡。
			// 查的是「有没有」，不是「暗多少」——阴影的浓度跟着主题走，深浅两套
			// 的数不一样（实测浅色下最深到 215、深色下最深到 20，而底色分别是
			// 255 和 24）。
			top := shotWinH - playbarHeight
			shadowed := false
			for y := top - 16; y < top; y++ {
				if probeColor(tt, playbarProbeX, y) != bg {
					shadowed = true
					break
				}
			}
			if !shadowed {
				t.Errorf("播放条上缘往上 16 像素全是底色 %v——那道阴影没了，"+
					"播放条会和内容区糊成一片（浅色下两边都是白）", bg)
			}
		})
	}
}

// TestCardsAndDialogsStillStandOffTheBackground 钉住「统一底色」没有把卡片和
// 弹窗也一起**刷没**：抽屉那一块面板和底色同色，但分开的正是它左缘那一圈。
//
// ⚠️ **这一条的判据跟着票 #172 换了**：面板的底色原来取 `t.Surface`（深色下
// 比底色亮一档），现在取 `t.Background`——上游那一枚 Drawer 没有自己的底色类，
// 走的是 HeroUI Drawer 的默认 `bg-background`，而上游主题把 `background` 这个
// 色定义成了**页面底色**（`src/common/constants/theme.ts`：深色 #17181a、浅色
// #ffffff）。所以它和底下那一页是**同一个颜色**，DESIGN.md §四说的正是这一条：
// 「深色层次：卡片与背景几乎同色，靠阴影分层——不要用『比背景亮一级』的板」。
//
// 于是「和背景区分开」这件事改由**影**和**那条分隔线**负责（上游 HeroUI 那一档
// `shadow="md"` 深色下有一层 1 像素的白色**内**影，深底上那才是真正看得见的
// 边界）。本条只断「面板那一块 = 底色」和「面板左缘那一列 ≠ 底色」两件事；
// 影的浓度在 `shadow_test.go` 里那一条钉着。
func TestCardsAndDialogsStillStandOffTheBackground(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newTestShell(t, systemIsDark(dark), at(shotWinW, shotWinH))
			s, tt := b.s, b.tt

			bg := rgbaOf(BuildTheme(s.app.SettingsSnapshot(), dark).Background)
			s.drawerOpen = true
			waitFrames(t, tt, "抽屉开出来了", func() bool {
				_, ok := tt.Find("播放列表")
				return ok
			})

			// ① 面板那一块和页面底色**一样**（上游的 `bg-background`）。
			if got := probeColor(tt, drawerProbeX, drawerProbeY); got != bg {
				t.Errorf("播放列表抽屉面板 (%d,%d) 是 %v，want %v（页面底色）——"+
					"上游那一枚 Drawer 走的是 `bg-background`，和底下那一页同色，"+
					"分开的��是那圈影", drawerProbeX, drawerProbeY, got, bg)
			}

			// ② 面板左缘那一列**不是**底色——那是分隔线和影，没有它这块面板就
			// 真的和背后糊成一片了（深色下尤其）。
			edgeX := shotWinW - queueDrawerWidth
			edge := probeColor(tt, edgeX, drawerProbeY)
			if maxDiff(edge, bg) <= 2 {
				t.Errorf("抽屉左缘 x=%d 那一列是 %v，和底色 %v 一样——"+
					"面板和背后之间一条边都没有", edgeX, edge, bg)
			}
		})
	}
}
