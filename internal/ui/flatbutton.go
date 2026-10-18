// 扁平按钮：把控件上那圈 1px 描边和那块常驻底色去掉。
//
// # 背景
//
// BMG-70 那份对照 Biu 的视觉调研（`~/.herdr-projects/reports/Biu_MyGo/70/`）的
// 第一条差距就是它，报告 §1.1 记作 **F1**、量级 M、「单条影响面最大」：
//
//	Biu 怎么做：HeroUI 的 `Button variant="light"`／`"flat"`：**无边框、透明底**，
//	hover 才上一层很淡的底色。侧栏菜单项、播放条图标键、倍速键、折叠键全是这一类。
//
//	我们现在什么样：MyGo 的 `ui.Button` 默认就是
//	`Background(t.Surface) + Border(1,t.Border)`，于是**侧栏菜单的每一项、顶栏
//	那三个图标键、倍速键、侧栏折叠键、队列抽屉行尾的「⋯」键**都是描边药丸。
//
// 我们这边那圈描边的出处是 `mygo@v0.5.5/ui/widgets.go` 的 `styleButton`：
// `base, border = t.Surface, t.Border`。
//
// # 为什么自己封，而不是套 `ui.Toolbar`
//
// `ui.Toolbar` 看着正好是「一排无边框的按钮」，但它是 `coreRow` +
// `FocusGroup(Horizontal)` + `role="Toolbar"`（`mygo/ui/toolbar.go:33-35`）——
// 套到侧栏那种**竖排** `Column` 上会把菜单排成横排；而且 `layoutToolbar`
// （同文件 `:135-155`）会把 intrinsic 宽度放不下的控件 collapse 掉、收进末尾
// 那个 `»` 溢出菜单。所以走 `ui.ButtonBase`（`mygo/ui/api_gen.go:759`，注释原文
// 「a button without a look」），照 `styleButton` 的配方给上尺寸，只把**底色和
// 描边**换掉。
//
// # 尺寸照抄 `styleButton`，是刻意的
//
// 内边距、圆角、图标与字之间的缝全部取 `ui.Button` 用的那三个数，于是换上去之后
// **布局一个像素都不动**——侧栏宽度与折叠、播放条 88/16/56、网格断点那些调研
// 已经逐条核过是对的部分（报告 §1.0 那 18 项）不用重新量一遍。变的只有颜色。
//
// # 扁平 ≠ 去掉所有交互反馈
//
// 悬停 / 按下 / 选中三样都还在，去掉的只有那圈描边和那块常驻底色。反过来做的
// 后果是「用户不知道哪里点得动」——这一层反馈不是装饰，是按钮还像按钮的理由。
//
// # 深浅两套主题都在同一段代码里
//
// 调研特别标了「深色下差距比浅色更明显」（报告 §2 基线 D）：深色下那圈
// `t.Border`（`#3f3f46`）压在 `#18181b` 上是一道亮线，每一格都亮一下，一眼就是
// 「另一种控件库」。而同一个 alpha 的淡底深色下几乎看不见，所以选中态那一层
// 深浅给的是两个数（`flatSelected`），不是一个。
package ui

import "github.com/egoist/mygo/ui"

// 悬停 / 按下那一层压多深。照上游 `bg-foreground/10` / `bg-foreground/20`
// （HeroUI 的 `light` 变体就是这两个数）。
const (
	flatHoverA = 0.08
	flatPressA = 0.16
)

// flatButton 是一个无描边、无常驻底色的按钮，画着 label。
//
// 🔴 **别给它补 `Label(label)`**：可见的字就是它的无障碍名字，补上去会多出一个
// 同样文字的无障碍节点，画面上出现两份（和 `sidebar.go` 的 `viewSideMenu` 里
// 那一条同源）。**只有图标按钮要补**——见 `flatIconButton`。
func flatButton(c *ui.Context, label string) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).
		Padding(t.Space(1.5), t.Space(3.5)).
		Gap(t.Space(1.5)).
		Radius(t.Radius)
	if label != "" {
		b.Children(func() { ui.Text(c, label).SingleLine() })
	}
	return flatFeel(c, b)
}

// flatIconButton 是一个**只画一个图标**的扁平按钮。
//
// 🔴 **label 不许省**，理由和 `icons.go` 的 `iconButton` 一字不差：图标对辅助技术
// 不可见（`mygo/ui/svg.go` 的 Icon 文档：「unless Label names them」），而这个
// 按钮再没有字可念。它同时也是无头 Tester 认得这个按钮的唯一凭据。
//
// 尺寸由调用方的 `Size` / `Padding` 撑——这里给的是「一个画着图标的扁平按钮」，
// 不是「多大一个」。
func flatIconButton(c *ui.Context, name, label string, font float32) ui.Element {
	b := flatButton(c, "").Label(label)
	b.Children(func() { ui.Icon(c, icon(name)).FontSize(font) })
	return b
}

// flatCurrent 是「这一项正选中」的那一层：一块中性的底，深色下再把字染成主色。
//
// 原来我们侧栏那一行刷的是**实心主色**（`sidebar.go` 的 `Background(t.Accent)`），
// 那是调研点名的那一处（报告 §2 基线 B 标注 3）。上游那一格长什么样，
// `_research/biu/screenshots/home.png` 上量得到：**中性灰底 + 主色的字**——
// pill 是 `#27272c`，字取最亮那一处是 `#66cc8a`，两者差 **7.7:1**。
//
// # 🔴 **字色分深浅，不是笔误，是 `#1ed760` 这一档绿决定的**
//
// 主色在两套主题里是同一个数（`app.DefaultPrimaryColor`），而它是**亮绿**：
//
//   - 深色下亮绿压深灰够亮（7.7:1；WCAG AA 只要 4.5）→ 字染主色，逐字照上游。
//   - 浅色下亮绿压**任何**浅底都不到 2:1（压在选中底上 1.55:1，压在纯白上更低）
//     → 拿它当字色等于把这一行读不出来。
//
// 所以浅色那一支**只换底、不动字**：底是主色很淡的一层，字还是 `t.Text`
//（14.6:1）。
//
// # 底下为什么是中性灰而不是主色淡底
//
// 绿字压绿底是同一支颜色压自己：浅色下那一格从改前的 9.2:1 掉到 1.55:1，直接不
// 达标；而上游那一格本来就是中性灰。`TestTheSelectedRowKeepsItsContrast`
// 逐条钉着这几个数。
//
// **「没选中」就是不调它**：那一支一个字都不改，颜色继续由 `flatFeel` 按悬停 /
// 按下决定。
func flatCurrent(c *ui.Context, e ui.Element) ui.Element {
	t := c.Theme()
	if t.Dark {
		return e.Background(flatSelected(t)).TextColor(t.Accent)
	}
	return e.Background(t.Accent.Alpha(0.14))
}

// flatSelected 是选中态那一层**中性**的底。
//
// 往 `t.SurfaceHover` 的方向再走 6%：只取 `t.SurfaceHover` 的话，选中的那一格和
// **悬停**的那一格底色一模一样（两个都是它），而「当前在哪」是侧栏里唯一的方向标，
// 被一次悬停盖掉比多画一层底更坏。多走的那 6% 正好拉开：浅色 #e9e9ec → #dcdce0、
// 深色 #323236 → #3e3e42，两边都还只是「很淡的一层底」。
//
// ⚠️ **`Mix` 走的是 sRGB**（`mygo/ui/color.go:105`，不是线性光），所以那 6% 在两套
// 主题里换算出来的差值一样（各 12~13 个通道）。
func flatSelected(t *ui.Theme) ui.Color {
	return t.SurfaceHover.Mix(t.Text, 0.06)
}

// flatFeel 铺上「悬停 / 按下」的那一层反馈。
//
// ⚠️ **「铺不铺」在建元素时问，「是不是禁用」在画的时候问**——见 `flatShade`。
//
// ⚠️ 它是在建元素时算的：`Element` 上没有 `styleFn`，而框架自己的 `styleButton`
// 是把同一段逻辑挂进 `styleFn`（paint 时跑）才拿到「按下的那一帧立刻变深」。
// 这里靠的是指针位置**不进事件循环就不变**（`mygo/ui/input.go:227` 的
// `setHover`），于是画面上的效果一样。本包早就在用这个写法
//（`navbar_suggest.go` 的 `viewSuggestTerm`）。
func flatFeel(c *ui.Context, e ui.Element) ui.Element {
	t := c.Theme()
	switch {
	case e.Pressed():
		e = e.DrawOver(flatShade(e, t, flatPressA))
	case e.Hovered():
		e = e.DrawOver(flatShade(e, t, flatHoverA))
	}
	return e
}


// flatShade 是悬停 / 按下那一层：往按钮上压一层半透明的前景色。
//
// 为什么压前景色而不是换成一个底色（框架的 Toolbar 按钮用的就是
// `t.SurfaceHover`）：换成底色的话，选中态那一层主色淡底会被整块顶掉——指针一
// 落上去，「当前在哪」就整个不见了。而压一层前景色在**任何**底上都成立：透明底
// 上是变灰，主色淡底上是变深，和上游 `bg-foreground/10` 是同一个做法。
func flatShade(b ui.Element, t *ui.Theme, a float32) func(p *ui.Painter, r ui.Rect) {
	shade, radius := t.Text.Alpha(a), t.Radius
	// 🔴 **「禁用没有」必须在画的时候问，不能在建按钮的时候问。**
	//
	// 调用方是在 `flatButton` 返回**之后**才 `.Disabled(...)` 的
	//（`settings_page.go` 的主题 / 窗口关闭两排、`settings_tab_general.go` 的两个
	// 「选择」都是这个写法），而 `flatFeel` 跑在它之前——建元素那一刻问出来的
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
		p.Fill(r, shade, radius)
	}
}
