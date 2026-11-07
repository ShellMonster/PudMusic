package pud

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/leading"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

// ledeMaxW 是页头导语的最宽（原型 `max-width: 60ch`，14 号字约 560）。
const ledeMaxW = 560

// PageHead 是页头（原型 `.page-head`）：左边 Eyebrow + 宋体标题 + 导语，右边
// 操作区，底对齐；上 24 下 8 的内边距，左右两块隔 24。标题行高 1.15（`.h-page`），导语行高 1.55（正文）。
//
// winW 是**窗口宽**（标题字号按原型 `clamp(32px, 3.4vw, 48px)` 随窗口宽流动，`scale.PageTitleFluid`；
// 1200 宽的窗口里是 40.8）；actions 可为 nil，里面直接放 `pud.Button`（原型 `.page-head .actions` 缝 8）。
//
//	pud.PageHead(c, "Library", "本地音乐", "把电脑里的歌拖进来……", s.winW, func() {
//		pud.Button(c, "import", pud.Ghost, pud.Std, "导入", "import")
//	})
func PageHead(c *ui.Context, eyebrow, title, lede string, winW float32, actions func()) {
	var ledeFn func()
	if lede != "" {
		p := pal(c)
		ledeFn = func() {
			text(c, lede, scale.Size14, p.Text2).LineHeight(leading.Body).MaxWidth(ledeMaxW).Margin(space.Px8, space.None, space.None, space.None)
		}
	}
	PageHeadFn(c, eyebrow, title, winW, ledeFn, actions)
}

// PageHeadFn 是 `PageHead` 的一般形式：导语由调用方画（里面要放链接、等宽数字之类）。
// ledeFn 里的内容会摆在标题下面；上边距 8 由调用方自己给。
func PageHeadFn(c *ui.Context, eyebrow, title string, winW float32, ledeFn, actions func()) {
	ui.Row(c).FillWidth().AlignItems(ui.End).Gap(space.Px24).Padding(space.Px24, space.None, space.Px8, space.None).
		Children(func() {
			ui.Column(c).Grow(1).Shrink(1).Children(func() {
				if eyebrow != "" {
					typo.Eyebrow(c, eyebrow)
				}
				// 原型 `.h-page { line-height: 1.15; letter-spacing: .01em; margin: 8px 0 0 }`：框架压不下
				// 宋体的自然行高（1.437），用负外边距把多出来的那截补回去（见 `typo.DisplayTrim`）。
				size := scale.Font(c, scale.PageTitleFluid(winW))
				trim := typo.DisplayTrim(size, leading.PageTitle)
				top := -trim
				if eyebrow != "" {
					top += space.Px8
				}
				typo.Display(ui.Text(c, title), size).LineHeight(leading.PageTitle).LetterSpacing(size*0.01).
					Margin(top, space.None, -trim, space.None)
				if ledeFn != nil {
					ledeFn()
				}
			})
			if actions != nil {
				ui.Row(c).Gap(space.Px8).AlignItems(ui.Center).Shrink(0).Children(actions)
			}
		})
}

// SecHead 是分区头（原型 `.sec-head`）：24 号宋体标题 + 说明（13 号 `faint` = text-3）+ 右端「更多」，基线
// 对齐、缝 16，下面离内容 20（`space.SectionHeadGap`）。more 为空就不画「更多」；
// 返回「更多」被点了没有。key 给「更多」那一枚。
func SecHead(c *ui.Context, key, title, note, more string) (moreClicked bool) {
	return SecHeadIcon(c, key, title, note, more, "right")
}

// SecHeadIcon 是 SecHead 的「更多」换图标版：原型首页「播放全部」前面是 14 的播放图标，
// 「全部」后面是右箭头。icon 是原型图标名，画在字的右边（"right"）；要画在左边传
// 带 "<" 前缀的名字，例如 "<play"。
func SecHeadIcon(c *ui.Context, key, title, note, more, icon string) (moreClicked bool) {
	p := pal(c)
	left := len(icon) > 0 && icon[0] == '<'
	if left {
		icon = icon[1:]
	}
	ui.Row(c).FillWidth().AlignItems(ui.Baseline).Gap(space.Px16).Margin(space.None, space.None, space.SectionHeadGap, space.None).
		Children(func() {
			typo.Display(ui.Text(c, title), scale.Font(c, scale.SectionTitle)).LineHeight(leading.SectionTitle).SingleLine()
			if note != "" {
				text(c, note, scale.Size13, p.Text3).SingleLine().Shrink(1)
			}
			ui.Spacer(c)
			if more != "" {
				b := ui.ButtonBase(c).Key(key).Gap(space.Px4).AlignItems(ui.Center)
				fg := p.Text2
				if b.Hovered() {
					fg = p.Text
				}
				b.Children(func() {
					if left && icon != "" {
						glyph(c, icon, 14, fg)
					}
					text(c, more, scale.Size13, fg).SingleLine()
					if !left && icon != "" {
						glyph(c, icon, 14, fg)
					}
				})
				moreClicked = b.Clicked()
			}
		})
	return moreClicked
}

// Tab 是下划线 Tab 的一项：Label 加可选的计数（原型 `.tab small`，12 号等宽 text-3）。
type Tab struct {
	Label string
	Count string
}

// Tabs 是下划线 Tab（原型 `.tabs` / `.tab`）：项与项隔 28、上下 12、16 号次要色；
// 选中项正文色 600，底下一条 2 像素主色线（进场从 0.3 宽展开淡入，`tab-in`）；
// 整排底下一条 line。selected 是当前选中的序号，点了就改它；返回这一帧是否改了。
func Tabs(c *ui.Context, key string, selected *int, tabs []Tab) (changed bool) {
	p := pal(c)
	ui.Row(c).Key(key).FillWidth().Gap(space.Px28).AlignItems(ui.End).
		Draw(func(pp *ui.Painter, r ui.Rect) {
			pp.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - 1, W: r.W, H: 1}, p.Line, 0)
		}).
		Children(func() {
			for i, tb := range tabs {
				on := i == *selected
				b := ui.ButtonBase(c).Key(key + ":" + tb.Label).PaddingY(space.Px12).Gap(space.Px4).
					AlignItems(ui.Baseline).Label(tb.Label)
				fg := p.Text2
				if on || b.Hovered() {
					fg = p.Text
				}
				b = b.Transition(motion.Fade)
				b.Children(func() {
					t := text(c, tb.Label, scale.Size16, fg).SingleLine()
					if on {
						t.FontWeight(600)
					}
					if tb.Count != "" {
						typo.Mono(ui.Text(c, tb.Count)).FontSize(scale.Font(c, scale.Size12)).
							TextColor(p.Text3).SingleLine()
					}
					if on {
						tabBar(c, key+":bar", p.Accent)
					}
				})
				if b.Clicked() && !on {
					*selected = i
					changed = true
				}
			}
		})
	return changed
}

// tabBarEnterScaleX 是选中下划线进场时的起始横向比例（原型 index.html:302
// `@keyframes tab-in { from { transform: scaleX(.3); opacity: 0 } }`）。
const tabBarEnterScaleX = 0.3

type tabBarScaleKey struct{}

// tabBar 画选中 Tab 下面那条 2 高的主色下划线，进场**只横向**从 30% 展开、同时淡入（240ms ease-out）。
//
// 框架的进场 `ui.Motion.Scale` 是横竖一起缩的（2 高的线会先变成 0.6 高再长回来），做不出原型的
// `scaleX`；所以淡入交给 `Transition`，横向比例用 `AnimateTo` 自己推，再 `Scale(sx, 1)` 以中心为原点
// 只缩横向。下划线换 Tab 时是新挂上的元素（在另一枚 Tab 里面），状态从头来，所以每次换 Tab 都重播。
// 系统「减少动态效果」时 `AnimateTo` 直接到终点。
func tabBar(c *ui.Context, key string, col ui.Color) ui.Element {
	bar := ui.Box(c).Key(key).Absolute().Left(0).Right(0).Bottom(-1).Height(2).
		Radius(tok.R2).Background(col).
		Transition(ui.ElementTransition{
			Duration: motion.Med, Ease: motion.EaseOut,
			Enter: &ui.Motion{Opacity: 0},
		})
	fresh := ui.Local(bar, tabBarScaleKey{}, func() bool { return true })
	if *fresh {
		*fresh = false
		bar.Jump(tabBarScaleKey{}, tabBarEnterScaleX)
	}
	sx := bar.AnimateTo(tabBarScaleKey{}, 1, ui.Animation{Duration: motion.Med, Ease: motion.EaseOut})
	if sx != 1 {
		bar = bar.Scale(sx, 1)
	}
	return bar
}
