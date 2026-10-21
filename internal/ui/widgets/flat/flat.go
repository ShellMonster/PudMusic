// Package flat 是「无描边、无常驻底色」那种按钮的配方。
//
// # 为什么它单独一个包
//
// 配方本来在 `internal/ui/flatbutton.go` 里，而 `internal/ui/widgets/list` 也要用
// 同一份（列表每一行末尾那枚「⋯」就是它）。但**界面层在列表包的上面**——
// `internal/ui` import `widgets/list`，反过来 import 是个环，编译期就过不去。
//
// 所以配方挪到这个**叶子包**里：两边都 import 它，而它谁也不 import。
// 这和 `widgets/iconset` 是同一个解法（那里也是因为 `widgets/imgcache` 要图标、
// `internal/ui` 又 import `imgcache` 才把资源挪出去的）。
//
// # ⚠️ 别在别处再写一份「无描边按钮」
//
// 这份配方是 BMG-70 那份视觉调研（`~/.herdr-projects/reports/Biu_MyGo/70/`）的
// 第一条差距的落点（报告 §1.1 记作 **F1**、量级 M、「单条影响面最大」）：
//
//	Biu 怎么做：HeroUI 的 `Button variant="light"`／`"flat"`：**无边框、透明底**，
//	hover 才上一层很淡的底色。
//
//	我们现在什么样：MyGo 的 `ui.Button` 默认就是
//	`Background(t.Surface) + Border(1,t.Border)`。
//
// 写第二份的话，两份会各自漂开——而那种漂开在界面上只是「这一枚比旁边那枚
// 深一点」，没人当它坏了。
package flat

import (
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/egoist/mygo/ui"
)

// 悬停 / 按下那一层压多深。照上游 `bg-foreground/10` / `bg-foreground/20`
// （HeroUI 的 `light` 变体就是这两个数）。
const (
	hoverA = 0.08
	pressA = 0.16
)

// Button 是一个无描边、无常驻底色的按钮，画着 label。
//
// 🔴 **别给它补 `Label(label)`**：可见的字就是它的无障碍名字，补上去会多出一个
// 同样文字的无障碍节点，画面上出现两份（和 `internal/ui` 的 `viewSideMenu` 里
// 那一处同源）。**只有图标按钮要补**——见 `IconButton`。
func Button(c *ui.Context, label string) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).
		Padding(t.Space(1.5), t.Space(3.5)).
		Gap(t.Space(1.5)).
		Radius(t.Radius)
	if label != "" {
		b.Children(func() { ui.Text(c, label).SingleLine() })
	}
	return feel(c, b)
}

// IconButton 是一个**只画一个图标**的扁平按钮。
//
// 🔴 **label 不许省**，理由和 `internal/ui` 的 `icons.go` 一字不差：图标对辅助技术
// 不可见（`mygo/ui/svg.go` 的 Icon 文档：「unless Label names them」），而这个
// 按钮再没有字可念。它同时也是无头 Tester 认得这个按钮的唯一凭据。
//
// 尺寸由调用方的 `Size` / `Padding` 撑——这里给的是「一个画着图标的扁平按钮」，
// 不是「多大一个」。
//
// svg 是要画的那张图。**由调用方给**（而不是这里按名字取）：取图标的表在界面层
// 那一层（`internal/ui` 的 `icons.go`），而控件层随手画一张时用的是
// `iconset.Must`——同一个名字永远是同一个指针，两边不会各画各的。
func IconButton(c *ui.Context, svg *ui.SVG, label string, font float32) ui.Element {
	b := Button(c, "").Label(label)
	b.Children(func() { ui.Icon(c, svg).FontSize(font) })
	return b
}

// Dimmed 给一个**已经禁用**的控件换上「禁用态也读得出来」的字色。
//
// # 为什么需要它
//
// 框架对禁用元素的做法是整个元素乘 50% 不透明度（`mygo/ui/paint.go:154-156`）。
// 只读设置页那三个主题按钮正好撞在这上面：深色下选中那一档的字是主色绿，绿乘
// 完 0.5 压在选中底上只剩 **2.6:1**——用户以为「这三个选项在深色模式下消失了」，
// 而不是「现在是只读的」。`ink.Disabled` 就是反着算的那个字色：它在乘完 0.5
// **之后**仍 ≥3:1。
//
// # 调用时机
//
// 🔴 **必须在调用方 `.Disabled(...)` 之后调**，因为按钮是在 `Button` 返回之后
// 才被禁用的（`internal/ui/settings_page.go` 那两处就是）。这和 `shade` 里
// 「禁用没有要在画的时候问」是同一个坑的两种表现：那一处靠 paint 回调里再问，
// 这一处靠**显式排在 `.Disabled` 之后**——建按钮的时候问，答案恒是「没禁用」，
// 于是只读的键照样染着主色。
//
// 这一层不改按钮，只改它的文字色：底色、描边、悬停反馈仍然由框架的 50% 说了算，
// 「禁用」的手感是一致的。
func Dimmed(c *ui.Context, e ui.Element) ui.Element {
	if e.IsDisabled() {
		e = e.TextColor(ink.Disabled(c.Theme()))
	}
	return e
}

// Feel 铺上「悬停 / 按下」的那一层反馈。
//
// ⚠️ **这是给「不是按钮、但也要有反馈」的扁平控件用的**（票 #138 的主 Tab 就是
// 这么做的）：按钮那一份 `Button` 自己带，而 Tab 是 `ui.TabsBase` 给的，我们
// 拿不到它的 `styleFn`（那是框架内部的）。
//
// 用法：`e = flat.Feel(c, e)`，放在别的样式**之后**（选中那一层的底色要先铺好，
// 悬停那层是压在它上面的）。
func Feel(c *ui.Context, e ui.Element) ui.Element { return feel(c, e) }

// feel 铺上「悬停 / 按下」的那一层反馈。
//
// ⚠️ **「铺不铺」在建元素时问，「是不是禁用」在画的时候问**——见 `shade`。
//
// ⚠️ 它是在建元素时算的：`Element` 上没有 `styleFn`，而框架自己的 `styleButton`
// 是把同一段逻辑挂进 `styleFn`（paint 时跑）才拿到「按下的那一帧立刻变深」。
// 这里靠的是指针位置**不进事件循环就不变**（`mygo/ui/input.go:227` 的
// `setHover`），于是画面上的效果一样。本包早就在用这个写法
// （`internal/ui/navbar_suggest.go` 的 `viewSuggestTerm`）。
func feel(c *ui.Context, e ui.Element) ui.Element {
	t := c.Theme()
	switch {
	case e.Pressed():
		e = e.DrawOver(shade(e, t, pressA))
	case e.Hovered():
		e = e.DrawOver(shade(e, t, hoverA))
	}
	return e
}

// shade 是「悬停 / 按下」那一层：往按钮上压一层半透明的前景色。
//
// 为什么压前景色而不是换成一个底色（框架的 Toolbar 按钮用的就是
// `t.SurfaceHover`）：换成底色的话，选中态那一层主色淡底会被整块顶掉——指针一
// 落上去，「当前在哪」就整个不见了。而压一层前景色在**任何**底上都成立：透明底
// 上是变灰，主色淡底上是变深，和上游 `bg-foreground/10` 是同一个做法。
func shade(b ui.Element, t *ui.Theme, a float32) func(p *ui.Painter, r ui.Rect) {
	fg, radius := t.Text.Alpha(a), t.Radius
	// 🔴 **「禁用没有」必须在画的时候问，不能在建按钮的时候问。**
	//
	// 调用方是在 `Button` 返回**之后**才 `.Disabled(...)` 的（`internal/ui` 的
	// `settings_page.go` 那一处就是），而 `feel` 跑在它之前——建元素那一刻问出来的
	// `IsDisabled()` 恒是 false。于是只读设置里的禁用按钮，指针落上去照样铺一层
	// 悬停底：**等于告诉用户这里点得动**（实测那层底是正常键的一半深，通道差 9，
	// 正常键是 18）。
	//
	// 框架自己的 `styleButton` 没这个问题：它把这一段挂进 `styleFn`，paint 时才跑。
	// 这里拿不到 `styleFn`，所以闭包多带一个 `b`，**在 paint 的那一刻**再问一遍
	// （`Element.IsDisabled` 读的是节点此刻的 flag，不是建它那一刻的）。
	return func(p *ui.Painter, r ui.Rect) {
		if b.IsDisabled() {
			return
		}
		p.Fill(r, fg, radius)
	}
}
