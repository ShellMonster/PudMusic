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

	sideProbeX, sideProbeY  = 100, 500
	contentProbeX, contentY = 1160, 400
	playbarProbeX, playbarY = 600, 780

	// drawerProbe 是播放列表抽屉面板上的一个空白处：面板从
	// 窗口右缘往左 384（`queueDrawerWidth`），所以 x=1160 在里面。
	drawerProbeX, drawerProbeY = 1160, 300
)

// probeColor 是画面上那一个点的颜色。
func probeColor(tt *ui.Tester, x, y int) color.RGBA { return tt.Image().RGBAAt(x, y) }

// TestTheChromeSharesOneBackground 钉住侧栏、内容区、播放条是同一个底色。
//
// ⚠️ **深浅两套都要跑，而且值是从当前主题算出来的**——不这么做的话，「统一了」
// 这件事在深色下照样能悄悄回退：把侧栏那一行改回 `t.Surface`，浅色那一档仍然
// 全绿（`#f4f4f5` 和 `#ffffff` 差 11，肉眼都未必看得出，而断言是逐字节相等的
// ——那正是它会红的地方）。深色下那两个色差 15 个通道，是这一族存在的理由。
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
				{"内容区", contentProbeX, contentY},
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
// （`layout/side/index.tsx:151-152`），播放条是外层那个 `shadow-2xl`
// （`layout/index.tsx:47`）。
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
// 弹窗也一起刷平：抽屉那一块面板仍然是 `t.Surface`，比底色亮一档。
//
// 这一条是上一条的另一半：底色统一之后，「和背景区分开」这件事全靠 `Surface`
// 了。把它也刷成底色的话，弹窗在深色下就是一块什么都看不见的板子——而用户
// 看到的是「弹窗没打开」，不会有人想到是它的底色和背后一样。
func TestCardsAndDialogsStillStandOffTheBackground(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newTestShell(t, systemIsDark(dark), at(shotWinW, shotWinH))
			s, tt := b.s, b.tt

			want := rgbaOf(BuildTheme(s.app.SettingsSnapshot(), dark).Surface)
			s.drawerOpen = true
			waitFrames(t, tt, "抽屉开出来了", func() bool {
				_, ok := tt.Find("播放列表")
				return ok
			})

			if got := probeColor(tt, drawerProbeX, drawerProbeY); got != want {
				t.Errorf("播放列表抽屉面板 (%d,%d) 是 %v，want %v（Surface）——"+
					"底色统一之后，面板要靠它跟背景分开",
					drawerProbeX, drawerProbeY, got, want)
			}
		})
	}
}
