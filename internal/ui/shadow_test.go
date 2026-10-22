// 这一组验**八处阴影**（票 #179）。
//
// # 为什么用「单独画一样东西」的机器，而不是整页
//
// 影是**落在元素外面那一圈**上的，而外面那一圈在整页上是**别的内容**——
// 列表的一行、封面的一角、另一个控件。于是「那一圈有没有变暗」这件事在整页上
// **没法比**：两处的底色本来就不一样。
//
// 所以每一处都单独画：底是一整块纯色，那一样东西摆在中间，四周留出几十像素的
// 空白，影就落在那片空白上。**画的是真的那一份代码**（`viewQueueDrawer`、
// `list.BackToTop`、`spaceAvatar`…），换掉的只是外面那一页。
//
// # 判据是「那一圈比同样的底色暗」
//
// 不是「那个像素等于某个数」——影是渐变的，精确值随主题和位置变。是
// **同样大小的一块空白，一块挨着元素、一块离得远**：挨着的那块必须有暗像素，
// 离得远的那块一个都没有。
//
// ⚠️ **深浅两套都要跑**：HeroUI 的 `shadow="sm|md|lg"` 深浅两套值不一样
// （深色那套多一层 1 像素的白色**内**阴影，浅色那套那一层是外黑边），
// 只跑浅色的话深色下那套值一次都没被验过。
package ui

import (
	"image"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/service/download"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	mygoui "github.com/egoist/mygo/ui"
)

// shadowW / shadowH 是这一组那台机器的窗口大小。
const (
	shadowW = 600
	shadowH = 400
)

// shadowScene 造一台只画一样东西的机器，返回它的 Tester。
//
// draw 拿到外壳和 context，在一块**四周留了 60 像素空白**的纯色底上画那一样
// 东西。留白是影落脚的地方——留得不够的话影落在窗口外面，量不到。
func shadowScene(t *testing.T, dark bool, draw func(s *Shell, c *mygoui.Context)) *mygoui.Tester {
	t.Helper()
	b := newTestShell(t, at(shadowW, shadowH), systemIsDark(dark),
		withView(func(s *Shell) func(*mygoui.Context) {
			return func(c *mygoui.Context) {
				s.wake.set(c)
				// ⚠️ **主题要自己装**：只画这一块的那一支不走 `Shell.View`，
				// 而 MyGo 每帧开头会把主题重置成跟随系统那一份（铁律二）。
				s.applyFrameTheme(c)
				uiBox(c).FillWidth().FillHeight().
					Background(c.Theme().Background).
					Padding(60, 60, 60, 60).Children(func() { draw(s, c) })
			}
		}))
	b.tt.Frame()
	return b.tt
}

// uiBox 是 `ui.Box` 的本地别名——这一组到处在用。
func uiBox(c *mygoui.Context) mygoui.Element { return mygoui.Box(c) }

// assertShadow 断「贴着元素的那一圈比离得远的那一圈暗」。
//
// band 是贴着元素的那块空白，control 是同样大小、离得远的另一块空白。
// tol 是「每个通道都低过这么多才算暗」——影是渐变的，差几个通道值不算。
func assertShadow(t *testing.T, img *image.RGBA, band, control mygoui.Rect, tol int, what string) {
	t.Helper()
	n := darkerIn(img, band, img.RGBAAt(int(control.X), int(control.Y)), tol)
	m := darkerIn(img, control, img.RGBAAt(int(control.X), int(control.Y)), tol)
	if n <= m {
		t.Errorf("%s：贴着它的那一圈有 %d 个暗像素，离得远的那一圈有 %d 个——"+
			"那一圈影没画出来", what, n, m)
	}
}

// assertShadowDarker 说「加上影之后，贴着它的那一圈比不加影时暗」。
//
// with 是带影的那张，without 是不带影的那张——**别的什么都不差**，所以两者之差
// 就是那一圈影的贡献。
//
// ⚠️ **为什么有一处要用这个而不是上一条那个**：深色下背景是 `#18181b`，而
// Tailwind 那一档影只有 **10% 黑**——压在深底上只差**一个色阶**（实测 24 → 23）。
// 「比远处那块暗」那种比法在深色下要一个色阶以上才数得出来，于是那一处明明画了
// 影却说它没画。**两��画法差的那一点就是影本身**，量它就量得到。
func assertShadowDarker(t *testing.T, with, without *image.RGBA, band mygoui.Rect, what string) {
	t.Helper()
	n := 0
	for y := int(band.Y); y < int(band.Y+band.H); y++ {
		for x := int(band.X); x < int(band.X+band.W); x++ {
			a, b := with.RGBAAt(x, y), without.RGBAAt(x, y)
			if int(a.R) < int(b.R) || int(a.G) < int(b.G) || int(a.B) < int(b.B) {
				n++
			}
		}
	}
	if n == 0 {
		t.Errorf("%s：加上影之后贴着它的那一圈一个像素都没变暗——影没画出来", what)
	}
}

// TestTheQueueDrawerHasAShadow 钉住**队列抽屉**那一圈影。
//
// 上游是 HeroUI 的 `<Drawer radius="md" shadow="md">`
// （`components/music-playlist-drawer/index.tsx:116`）——Drawer 是拿 Modal 画的，
// 那一档影落在**面板本身**上。
//
// ⚠️ **影落在抽屉的左边**：HeroUI 那一档的横向偏移是 0，而抽屉贴在窗口右边，
// 右半边在窗口外面——所以看的是**左边**那一列。
func TestTheQueueDrawerHasAShadow(t *testing.T) {
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
			left := float32(shadowW - queueDrawerWidth)
			assertShadow(t, img,
				mygoui.Rect{X: left - 10, Y: 140, W: 10, H: 20},
				mygoui.Rect{X: 20, Y: 140, W: 10, H: 20},
				2, "队列抽屉")
		})
	}
}

// TestTheBackToTopButtonHasAShadow 钉住**回顶**那一枚的影。
//
// 上游是 `shadow-xl shadow-black/30`（`components/scroll-container/index.tsx:82`）
// ——**30% 黑**、两档（`0 20px 25px -5px` 与 `0 8px 10px -6px`）。
//
// ⚠️ **看的是按钮**下面**那一块**：那一档影的纵向偏移是 +20，主体落在按钮底下
// （按钮底下 20 到 55 之间），而上面那一层只往上糊了 12 像素。
func TestTheBackToTopButtonHasAShadow(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt := shadowScene(t, dark, func(_ *Shell, c *mygoui.Context) {
				// 滚过 400 像素才出现（上游的数）：这里直接给一个够大的偏移。
				list.BackToTop(c, 500, func() {})
			})
			img := tt.Image()
			// 按钮是 40 见方，贴在窗口右边 40、离底 120。
			bx := float32(shadowW - 40 - 40)
			by := float32(shadowH - 120 - 40)
			assertShadow(t, img,
				mygoui.Rect{X: bx + 10, Y: by + 42, W: 20, H: 8},
				mygoui.Rect{X: bx + 10, Y: by - 40, W: 20, H: 8},
				2, "回顶按钮")
		})
	}
}

// TestTheDownloadTableHasAShadow 钉住**下载表**外面那张卡的影。
//
// 上游是 `<Card radius="md" shadow="sm">`（`pages/download-list/index.tsx:105`）。
//
// ⚠️ **卡片要有一张底**：上游那张卡是 `bg-content1` + 影，而影是**盒子外面**那一圈
// ——没有底的话那一圈影画在一块透明的东西上，画面上只是「一圈淡淡的灰」。
func TestTheDownloadTableHasAShadow(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			rows := []download.Snapshot{dlSnapshot("第一首", download.StatusDone, 100, 100)}
			s, _ := newRows(t, rows...)
			tt := newTestShell(t, at(shadowW, shadowH), systemIsDark(dark),
				withView(func(sh *Shell) func(*mygoui.Context) {
					return func(c *mygoui.Context) {
						sh.wake.set(c)
						sh.applyFrameTheme(c)
						uiBox(c).FillWidth().FillHeight().
							Background(c.Theme().Background).
							Padding(60, 60, 60, 60).Children(func() {
							sh.downloads.sync(sh.downloads.rowsFn())
							sh.downloads.viewTable(c, sh)
						})
					}
				})).tt
			tt.Frame()

			img := tt.Image()
			// 卡片是 Grow(1) 的，占满留白之后那一块——左缘就在留白的边上。
			assertShadow(t, img,
				mygoui.Rect{X: 50, Y: 200, W: 10, H: 20},
				mygoui.Rect{X: 20, Y: 200, W: 10, H: 20},
				2, "下载表那张卡")
			_ = s
		})
	}
}

// TestTheSettingsColorSwatchHasAShadow 钉住**设置里的色卡**那一圈影。
//
// 上游是 `<Card as="div" isPressable radius="md" shadow="sm"
// className="border-default w-[100px] border">`（`pages/settings/color-settings.tsx:24`）。
func TestTheSettingsColorSwatchHasAShadow(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt := shadowScene(t, dark, func(s *Shell, c *mygoui.Context) {
				s.pickColors(c, app.Settings{
					PrimaryColor: app.DefaultPrimaryColor,
				}, false)
			})
			img := tt.Image()
			// 色卡是 100×40，摆在留白之后的第一格。
			assertShadow(t, img,
				mygoui.Rect{X: 60 - 10, Y: 80, W: 10, H: 20},
				mygoui.Rect{X: 20, Y: 80, W: 10, H: 20},
				2, "设置里那块色卡")
		})
	}
}

// TestTheFollowCardHasAShadow 钉住**关注卡**那一圈影。
//
// 上游是 HeroUI 的 `<Card radius="md" isHoverable isPressable>`
// （`pages/follow-list/user-card.tsx:60`）——HeroUI 的 Card **默认就是
// `shadow="md"`**（`@heroui/theme` 的 card 主题 `defaultVariants.shadow = "md"`）。
func TestTheFollowCardHasAShadow(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt := shadowScene(t, dark, func(s *Shell, c *mygoui.Context) {
				s.viewFollowCard(c, bili.FollowUser{
					Mid: 11, Name: "一个 UP 主", Sign: "签名",
					Face: "https://f/author-11.jpg",
				})
			})
			img := tt.Image()
			// 卡片是 Grow(1) 的，占满留白之后那一块。
			assertShadow(t, img,
				mygoui.Rect{X: 60 - 10, Y: 200, W: 10, H: 20},
				mygoui.Rect{X: 20, Y: 200, W: 10, H: 20},
				2, "关注卡")
		})
	}
}

// TestTheSpaceAvatarHasAShadow 钉住**UP 空间那个头像**那一圈影。
//
// 上游是 `shadow-lg`（`user-profile/space-info.tsx:89` 的
// `h-[140px] w-[140px] flex-none shadow-lg`）——**两层**
// （`0 10px 15px -3px` 与 `0 4px 6px -4px`）。
//
// ⚠️ **看的是头像底下那一块**：那一档影的纵向偏移是 +10，主体落在头像下面。
func TestTheSpaceAvatarHasAShadow(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			// 两次渲染：一次带影、一次不带。别的什么都不差。
			with := shadowScene(t, dark, func(s *Shell, c *mygoui.Context) {
				s.spaceAvatar(c, "https://f/face-42.jpg", upAvatarS, true)
			}).Image()
			without := shadowScene(t, dark, func(s *Shell, c *mygoui.Context) {
				s.spaceAvatar(c, "https://f/face-42.jpg", upAvatarS, false)
			}).Image()
			// 头像在留白之后，140 见方；影落在它**下面**那一块。
			assertShadowDarker(t, with, without,
				mygoui.Rect{X: 80, Y: 60 + upAvatarS + 1, W: 100, H: 10},
				"UP 空间那个头像")
		})
	}
}
