// 这一组验**九处阴影**（票 #179；播放条那一处是票 #221）。
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
//
// # 最后一处不一样：播放条那两道影是**上游那个结构**（票 #221）
//
// 别的八处都是「一档影落在一个元素上」，播放条是**两层**（外层 div 的
// `shadow-2xl` + 里面 Card 的 `shadow="sm"`），所以它多断一条**源码**上的事：
// 值不许写在调用处。
package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
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
	// shadowPad 是那台机器四周留的空白——**影落脚的地方**。
	//
	// 它同时是「那一块东西从哪儿起」的数：留白的左上角就是内容区的原点，
	// 所以单独画的那一样东西摆在 (shadowPad, shadowPad) 上。
	shadowPad = 60
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
					Padding(shadowPad, shadowPad, shadowPad, shadowPad).
					Children(func() { draw(s, c) })
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

			if !dark {
				return
			}
			// ⚠️ **深色下真正把这一格和底色分开的是内缘那道 1 像素的白边**：外面
			// 那一圈是 20% 的黑，压在 `#18181b` 上只差一两个色阶；而那一档影的第三层
			// 在深色下是 `inset`（1 像素白边）——**它会被铺满整格的那个取色板控件
			// 盖住**（评审量过：没有它的时候色卡内缘一路是 `t.Surface`）。
			//
			// 所以这里量的是**内缘那一道线**：它必须比卡面亮。
			//
			// ⚠️ **从离左边两像素的地方开始扫**：再往左是那一格自己的**描边**
			//（`t.Border`，深色下 15% 白叠出来的 #3b3b3d）——它不是影，算上它
			// 这一条就永远绿。
			const swatchX = 60
			interior := img.RGBAAt(swatchX+20, 90)
			lit := false
			for x := swatchX + 2; x < swatchX+10; x++ {
				if c := img.RGBAAt(x, 90); int(c.R) > int(interior.R)+3 {
					lit = true
					break
				}
			}
			if !lit {
				t.Errorf("深色下色卡内缘没有那道 1 像素的白边（卡面是 %v）——"+
					"`shadow=\"sm\"` 深色那一档的内层没画出来", interior)
			}
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

// TestThePlayBarHasTheUpstreamShadow 钉住**播放条**那一道影（票 #221）。
//
// 上游是**两层**（票 #221）：
//
//   - 外层那个 div 的 Tailwind `shadow-2xl`（`layout/index.tsx:48`）
//   - 里面那张 Card 的 HeroUI `shadow="sm"`（`playbar/index.tsx:24`）
//
// 两层落在**同一个矩形**上（那个 div 是 `h-[88px] w-full`，Card 是 `h-full`
// 铺满它），所以合成一层元素上的几道影，值都在 `internal/ui/widgets/shadow`。
//
// # 判据是那两道「线」，不是影有多浓
//
// 影是渐变的，浓度随主题和位置变；**能认出上游那一档的是两条线**：
//
//	深色：Card 那档的第三层是 1 像素的**内白边** → 播放条**第一行**比条身亮
//	      （实测 +5；上游截图那一行是 #1e1e21 = 30，比条身 #18181b 亮 +6）
//	浅色：同一档在浅色下**没有**内层，第三层是 1 像素的**外黑边** → 播放条
//	      **上面那一行**比条身暗（实测 −22）
//
// ⚠️ **深浅两套的第三层不一样**：深色下那条线在条**里**，浅色下那条线在条
// **外**。抄成一样的话，深色下播放条会「糊」在内容区上——上游专门用白边就是
// 为了那个。
//
// ⚠️ **认的是深色那一半**：之前那道自己调的影（朝上、模糊 8）**浅色下也压得
// 暗**（它本来就是朝上洒的），所以浅色那一半分不出新旧；分得出的是深色下
// 那道**内白边**——之前那一道影没有内层，条的第一行和条身一模一样。
func TestThePlayBarHasTheUpstreamShadow(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			tt := shadowScene(t, dark, func(s *Shell, c *mygoui.Context) {
				s.viewPlayBar(c, barState())
			})
			with := tt.Image()
			// 对照：**同一个场景，只少一个播放条**——两者之差就是那道影的贡献。
			without := shadowScene(t, dark, func(s *Shell, c *mygoui.Context) {
			}).Image()

			// 播放条的上沿在 shadowPad 那儿（留白就是影落脚的地方）。
			const top = shadowPad
			assertShadowDarker(t, with, without,
				mygoui.Rect{X: 200, Y: top - 1, W: 200, H: 1}, "播放条")

			// ⚠️ **取样那一列是「封面左边那块内边距」**（封面左缘往左 8 像素）：
			// 那里**任何布局下都没有控件**——写死一个 x 的话，播放条中间那一列
			// 一重排（三键居中那一票，票 #224）就可能取到一枚按钮上，量到的
			// 就不是条自己的底色了。
			cover, ok := tt.Find("当前封面")
			if !ok {
				t.Fatalf("画面上找不到封面：%q", tt.Texts())
			}
			x := int(cover.X) - 8

			// 条身：离上沿远一点的那一行（同一列，控件只在更右边）。
			body := with.RGBAAt(x, top+30)
			rim := with.RGBAAt(x, top)     // 条的**第一行**
			above := with.RGBAAt(x, top-1) // 条**上面**那一行

			if dark {
				// 深色：Card 那档的第三层是 1 像素的**内白边**。
				d := int(rim.R) - int(body.R)
				if d < 3 || d > 12 {
					t.Errorf("深色下播放条第一行是 %v，条身是 %v（亮 %d）——"+
						"上游那一档的内白边是 1 像素白（实测 +5，上游截图是 +6）",
						rim, body, d)
				}
				return
			}
			// 浅色：同一档的第三层是 1 像素的**外黑边**，落在条**外面**。
			d := int(body.R) - int(above.R)
			if d < 10 || d > 60 {
				t.Errorf("浅色下播放条上面那一行是 %v，条身是 %v（暗 %d）——"+
					"上游那一档的外黑边是 1 像素 30%% 黑（实测 22）", above, body, d)
			}
			// 浅色下**不该有内白边**（上游那一档浅色下没有内层）。
			if int(rim.R) != int(body.R) {
				t.Errorf("浅色下播放条第一行是 %v，条身是 %v——"+
					"上游那一档浅色下**没有**内层，多出来的是一层白边", rim, body)
			}
		})
	}
}

// TestThePlayBarShadowComesFromTheShadowPackage 钉住「播放条那道影的值只写在
// `internal/ui/widgets/shadow` 里」（DESIGN.md §四·五：「调用处不许现调
// `Shadow(...)`」）。
//
// 三样都要：
//
//  1. `playbar.go` 里没有**元素级**的 `Shadow(...)` / `InsetShadow(...)` 调用
//  2. `playbar.go` 走的是那个包（调 `shadow.PlayBar`）
//  3. `widgets/shadow.PlayBar` 同时用了 `Tailwind2XL` 和 `HeroSmall`——
//     **上游那两层都在**
//
// ⚠️ **第 3 条只能是源码级的**：外层那档 `shadow-2xl` 是**朝下**洒的
// （+25、模糊 50），主体落在播放条**下面**——而播放条贴着窗口底边，于是那一档
// 在真窗口里几乎看不见（深色下连一个色阶都不到）。画面上量它，量出来的是
// 「它没有出现」，量不出「它是不是两档」。
//
// ⚠️ **第 1 条认的是元素级那套 API**（`Shadow(x, y, blur, spread, c)`，
// 五个参数）：画笔那一套是 `p.Shadow(rect, radius, x, y, blur, spread, c)`，
// 七个参数——那是框架画一枚控件的影用的，跟「上游那几档的值写在哪」无关。
func TestThePlayBarShadowComesFromTheShadowPackage(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "playbar.go", nil, 0)
	if err != nil {
		t.Fatalf("解析 playbar.go 失败：%v", err)
	}

	// 1. 不许现调元素级的影。
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || len(call.Args) != 5 {
			return true
		}
		if sel.Sel.Name == "Shadow" || sel.Sel.Name == "InsetShadow" {
			t.Errorf("playbar.go 里现调了 %s(...)——上游那几档的值只写在 "+
				"`internal/ui/widgets/shadow` 里（DESIGN.md §四·五）",
				sel.Sel.Name)
		}
		return true
	})

	// 2. 走的是那个包。
	imported := false
	for _, imp := range f.Imports {
		if imp.Path.Value == `"github.com/ShellMonster/Biu_Go/internal/ui/widgets/shadow"` {
			imported = true
		}
	}
	if !imported {
		t.Error("playbar.go 没有 import `internal/ui/widgets/shadow`——" +
			"播放条那道影该从那个包里取")
	}
	if !callsName(f, "PlayBar") {
		t.Error("playbar.go 没有调 `shadow.PlayBar(...)`——" +
			"播放条那两道影（外层 2xl + Card 的 sm）是上游那个结构，取值走那一个出口")
	}

	// 3. 那一个出口里，两层都在。
	sf, err := parser.ParseFile(fset, "widgets/shadow/shadow.go", nil, 0)
	if err != nil {
		t.Fatalf("解析 widgets/shadow/shadow.go 失败：%v", err)
	}
	var playBar *ast.FuncDecl
	for _, decl := range sf.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == "PlayBar" {
			playBar = fn
		}
	}
	if playBar == nil {
		t.Fatal("widgets/shadow 里没有 PlayBar——播放条那两道影的出口不见了")
	}
	for _, want := range []string{"Tailwind2XL", "HeroSmall"} {
		if !callsName(playBar, want) {
			t.Errorf("widgets/shadow.PlayBar 里没有 %s——上游播放条是**两层**"+
				"（外层 div 的 shadow-2xl + Card 的 shadow=\"sm\"），少一层就不是上游那一档了",
				want)
		}
	}
}

// callsName 报这个语法树里有没有调过那个名字的函数。
//
// ⚠️ **认的是「被调用的名字」**，不认是谁调的：包里的函数是 `Tailwind2XL(e)`
// 那样直接调的，外面是 `shadow.PlayBar(...)` 那样带包名的——两种都算。
func callsName(n ast.Node, name string) bool {
	found := false
	ast.Inspect(n, func(m ast.Node) bool {
		if found {
			return false
		}
		call, ok := m.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			found = fn.Name == name
		case *ast.SelectorExpr:
			found = fn.Sel.Name == name
		}
		return !found
	})
	return found
}
