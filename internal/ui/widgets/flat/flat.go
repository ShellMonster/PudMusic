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
// # 这一族里也有「名字型链接」那一档
//
// `Link` 是同一个配方的另一个变体：**一句话字 + 悬停下划线**，**没有按钮壳**
// （上游 Biu 的作者名就是 `hover:underline`）。它和 `Button` 分开，是因为
// `Button` 有二十几处调用方（全应用的按钮），动它就是动全应用。
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
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// 悬停 / 按下那一层压多深。照上游 `bg-foreground/10` / `bg-foreground/20`
// （HeroUI 的 `light` 变体就是这两个数）。
const (
	hoverA = 0.08
	pressA = 0.16
)

// Base 是「**这一枚按钮坐着的那块底**」。
//
// # 为什么要有它
//
// 悬停 / 按下的反馈有**两处**是按底算的，而它们原来都是按**页面底色**算的：
//
//  1. 悬停时图形变的那一档主色（`ink.AccentOn` 按 `t.Background` 算）；
//  2. 悬停 / 按下铺的那一层半透明淡底（`t.Text` 压上去）。
//
// 这两处都对「底是页面底色」成立——而**满屏播放器那一行的底是压暗过的封面**：
// 按页面白底算出来的那一档主色（浅色主题下是深绿 `#107032`）压在那上面只有
// **1.0:1**，等于图标**消失**（指针一停上去，那几个图标反而不见了，票 #248）。
//
// # 零值 = 按主题算（和以前一模一样）
//
// `ThemeBase` 是零值：普通页面上的那些键（播放条、顶栏、侧栏、列表行尾的「⋯」）
// 都是它——**它们的行为一个字节都不变**。
//
// 非零 = **按给定的底算**：主色那一档按那块底算（`ink.AccentOnBase`），淡底按
// 那块底的深浅挑黑或白（深底压白、浅底压黑）。
//
// ⚠️ 「有没有给底」的判据是**那一档带不带 alpha**（`A != 0`）：真的底永远是
// **不透明**的，而零值是全透明的（`ui.Color{}`）——所以这两件事不会撞。
type Base struct {
	color ui.Color
}

// BaseOf 是「**按给定的底算**」那一档。
//
// 那块底得是**不透明**的（真的底色就是）：给了带 alpha 的颜色，算出来的对比度
// 会按「合到白底上」那一档算（`ink.Ratio` 的规矩），和屏幕上看到的不一样。
func BaseOf(c ui.Color) Base { return Base{color: c} }

// ThemeBase 是「**按主题算**」那一档（零值）。
//
// 普通页面上的那些键都是它——`ink.AccentOn` 按 `t.Background` 算主色那一档、
// 淡底按主题正文色算，和这一档加进来之前**一模一样**。
var ThemeBase Base

// given 报这一档有没有给底。
func (b Base) given() bool { return b.color.A != 0 }

const (
	// labelPad 是「图标 + 文字」那一类按钮的**左**内边距。
	//
	// ⚠️ **16 是照上游的形状算出来的**：上游那一行的图标左边是
	// `ScrollContainer` 的 `px-4`（16）加上按钮的 `px-2`（8），一共 24；我们这边
	// 没有那一层滚动内边距（侧栏的内容区是**通栏**的），而图标要跟左上角那个
	// logo 对齐——logo 也在 16 处——所以这一格取 16。
	//
	// 差的那 8 是我们和上游的**结构**差别（上游的侧栏内容区有一层 16 的内边距，
	// 我们没有），不是抄漏了。
	labelPad = space.Px16
	// labelGap 是图标格和文字之间的缝（上游 HeroUI 按钮的 `gap-2`）。
	labelGap = space.Px8
)

// Button 是一个无描边、无常驻底色的按钮，画着 label。
//
// 🔴 **别给它补 `Label(label)`**：可见的字就是它的无障碍名字，补上去会多出一个
// 同样文字的无障碍节点，画面上出现两份（和 `internal/ui` 的 `viewSideMenu` 里
// 那一处同源）。**只有图标按钮要补**——见 `IconButton`。
func Button(c *ui.Context, label string) ui.Element {
	return button(c, label, ThemeBase)
}

// button 是 Button 的「底色可传」那一支。
//
// base 只影响**悬停 / 按下铺的那一层淡底**（按底挑黑或白，见 `shadeInk`）——
// 按钮自己的外观（无描边、无常驻底色）两个分支一样。
func button(c *ui.Context, label string, base Base) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).
		Padding(t.Space(1.5), t.Space(3.5)).
		Gap(t.Space(1.5)).
		Radius(t.Radius)
	if label != "" {
		b.Children(func() { ui.Text(c, label).SingleLine() })
	}
	return feel(c, b, base)
}

// Link 是「名字型链接」那一档：**一句话字**，指针落上去时底下长出一道**下划线**，
// **没有按钮壳**——没有底色、没有描边、没有圆角、没有胶囊。
//
// # 它和 `Button` 的差别就是那三层壳
//
// `Button` 是「左右各留一段内边距 + 圆角」：名字因此被一个**胶囊**包着，
// 而指针落上去时 `feel` 再往那个胶囊上压一层半透明前景色——**悬停时那块底牌
// 才显出来**。用户 2026-10-25 说的就是这一处：「那个可点击的效果的不要做成
// 一个胶囊一样的包着那个名称的，用正常的下划线就行，像 biu 里的那样」。
//
// 上游 Biu 的作者名是 `hover:underline`（动态流条目上的 `User` 那一格）——
// **没有按钮壳、没有底色、没有胶囊**，指针落上去时名字底下长出一道下划线。
//
// # 为什么还是一枚**按钮**（`ui.ButtonBase`），而不是一段文字
//
// 「点得动」得**真的能点**：命中区、无障碍名字、键盘可达三样都还在。
// `ui.ButtonBase` 给的就是这三样——它 `Focusable`（Tab 能走到、回车 / 空格能
// 按下去）、`Clicked()` 认指针也认键盘，而辅助技术从它里面的字读出名字
// （`RoleLink` 是叶子角色，名字取自 `innerText`）。
//
// 而它**自己不带任何外观**：底色 / 描边 / 圆角全是 `ui.Button`（框架的
// `styleButton`）加的，`ButtonBase` 一样都没有。
//
// ⚠️ **别用 `ui.Text(...).OnClick(...)` 那一套**：那样的一段字**拿不到键盘焦点**
// （`Focusable` 没设），于是它只能用鼠标点——而「键盘可达」是这一档要保住的东西。
//
// # 悬停反馈是「一道下划线」，颜色跟着主题走
//
// 下划线画在 `ink.AccentOn(theme)` 那一档主色上：它**从当前主色现算**
// （主色是用户可设的），深浅两套都 ≥4.5:1（`widgets/ink` 的 `AccentOn` 保证），
// 而主题方案换了它跟着换——**这里没有一个写死的绿**。
//
// ⚠️ **下划线的颜色和字自己的颜色是两回事**：字色由调用方给（正文色 / 次要色），
// 而下划线走 `DecorationColor`——那是节点上另一个字段。所以调用方在 `Link` 之后
// 链一个 `.TextColor(...)` **不会**把悬停时那道下划线改掉。
//
// 🔴 **别动 `Button` 的本体**：它有二十几处调用方（全应用的按钮），改它就是改
// 全应用。**这一档是另开的一个变体**，不是把 `Button` 改掉。
func Link(c *ui.Context, label string) ui.Element {
	// `ui.ButtonBase` = 一行、内容居中、能拿焦点、能点——**没有底色 / 描边 / 圆角**。
	//
	// ⚠️ **内容左对齐**（`Justify(ui.Start)`）：名字都在左对齐的那一列里，而框架
	// 的按钮内容默认居中——居中的话那句话会飘到那一列的中间（理由和
	// `internal/ui/playbar.go` 的 `viewArtist` 顶上那一段一样）。
	b := ui.ButtonBase(c).
		Padding(space.None, space.None).
		Justify(ui.Start).
		Cursor(ui.CursorPointer).
		Role(ui.RoleLink)
	if label != "" {
		b.Children(func() { ui.Text(c, label).SingleLine() })
	}
	return linkFeel(c, b)
}

// linkFeel 铺上「悬停时那道下划线」。
//
// ⚠️ **「铺不铺」在建元素时问**（理由同 `feel`）：指针位置不进事件循环就不变，
// 而 `Element` 上没有 `styleFn`，挂不进 paint 回调。
func linkFeel(c *ui.Context, e ui.Element) ui.Element {
	if e.Hovered() {
		e = e.Underline().DecorationColor(ink.AccentOn(c.Theme()))
	}
	return e
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
//
// ⚠️ **按下时图标缩到 0.96**（`PressScale`）——纯图标键才缩，理由见 `press.go`。
func IconButton(c *ui.Context, svg *ui.SVG, label string, font float32) ui.Element {
	b := button(c, "", ThemeBase).Label(label)
	b.Children(func() {
		ic := ui.Icon(c, svg).FontSize(font)
		// ⚠️ **问的是按钮的按着没有**（理由见 press.go）。
		ic = PressScale(ic, b.Pressed(), false)
	})
	return b
}

// IconButtonAccent 是 IconButton 加上「**悬停时图形变主色**」。
//
// 上游 `src/components/icon-button/index.tsx:16`：
// `className={twMerge("hover:text-primary text-inherit", className)}` —— 悬停时
// 图形从前景色变成主色（报告 §3.5）。**不只是底下多一层淡底**：图形本身换色。
//
// 🔴 **染的是图形自己，不是按钮**：按钮上可能还有调用方给的颜色（赞 / 收藏 /
// 音量那几枚各有一档），染在按钮上会被那些 `.TextColor(...)` 盖掉——而盖掉的
// 那一半正好是「悬停变主色」要的东西。
//
// ⚠️ **主色当字色那一档取 `ink.AccentOn`**：浅色下主色是 #1ed760，压在白底上
// 只有 1.9:1（`widgets/ink` 的文件头写着这件事），所以取那个「读得出来」的变体；
// 深色下就是主色本身。理由和 `internal/ui/flatbutton.go` 的 `flatCurrent` 是同一
// 条：**主色当底和主色当字是两件事**。
//
// ⚠️ **禁用的状态必须从这里给**，不能靠调用方链 `.Disabled(...)`：悬停是**建元素
// 的时候**问的（`Hovered()` 在禁用的键上恒为 false，见 `mygo/ui/input.go:1113`），
// 而链式调用是在返回**之后**才把禁用设上去——问早了，一枚**禁用**的键会用主色
// 告诉用户「这里点得动」（票 #217 的评审查出来过同款的一条）。
//
// ⚠️ **它只给「底是应用底色」的地方用**：主色当字色那一档是按 `t.Background`
// 算的，而满屏播放器那一行的底是**压暗过的封面**——浅色下那一档压在上面只有
// 1.0:1（等于看不见，见票 #217 的评审）。所以那一行**不用这一档**，它用
// `IconButtonAccentOnBase`（底色由调用方给）。
func IconButtonAccent(c *ui.Context, svg *ui.SVG, label string, font float32, disabled bool) ui.Element {
	return iconButtonAccent(c, svg, label, font, disabled, ThemeBase)
}

// IconButtonAccentOnBase 是 IconButtonAccent 的「**底色可传**」变体：悬停时图形
// 变的那一档主色按**给定的底**算，而不是按 `t.Background` 算。
//
// # 为什么需要它
//
// 满屏播放器那一行（上一首 / 下一首 / 播放模式 / 队列开关）的底是**压暗过的
// 封面**，而按页面白底算出来的那一档主色（浅色主题下是深绿 `#107032`）压在那
// 上面只有 **1.0:1**——指针一停上去，那几个图标**反而不见了**（票 #248）。
//
// 按给定的底算出来的那一档，在**任何一档封面上**都读得出来（那一档是按最亮的
// 封面算的，见 `internal/ui/fullplayer.go` 的 `fullRowBase`）。
//
// ⚠️ **图形那一档按 3:1 算**（WCAG 1.4.11 非文字对比度），不是正文那一档的 4.5：
// 这是一枚 22 像素的图标，而 4.5 会把它推到「几乎纯白」——那样它和平时那一档
// （白 92%）几乎看不出分别，悬停反馈等于没有。
//
// ⚠️ **那 3:1 是「图形 vs 那一块底」**：悬停时图形底下还多一层**淡底**（8% 的白，
// 上游 `bg-foreground/10` 就是这么一层），而它是**悬停反馈本身**——它把这一枚
// 底下的一切都压低一档。所以图形的**局部**对比度（图形 vs 淡底）是 **2.65:1**
// （实测），而它对**那一块底**是 3.10:1。
//
// 🔴 **深底上「色相」和「亮度对比」只能要一个**：那一行的图标平时是白 92%
// （对底 4.31:1），换成主色就必然掉一档亮度（色相越明显，亮度掉得越多）。
// 我们选**色相**（白 → 亮薄荷绿 / 亮桃色，那是「这一枚活着」的信号）加上**淡底**
// 那一层，而不是选亮度——上游 Biu 的 `hover:text-primary` 也是同一个取舍。
// 相对**改之前**（浅色主题下悬停是深绿压深底 1.31:1，等于看不见）这是一大截。
func IconButtonAccentOnBase(c *ui.Context, svg *ui.SVG, label string, font float32, disabled bool, base Base) ui.Element {
	return iconButtonAccent(c, svg, label, font, disabled, base)
}

// iconButtonAccent 是一枚「悬停时图形变主色」的扁平图标键（底色可传）。
func iconButtonAccent(c *ui.Context, svg *ui.SVG, label string, font float32, disabled bool, base Base) ui.Element {
	b := button(c, "", base).Label(label).Disabled(disabled)
	b.Children(func() {
		ic := ui.Icon(c, svg).FontSize(font)
		if col, ok := iconAccentOnHover(c, disabled, b.Hovered(), base); ok {
			ic = ic.TextColor(col)
		}
		ic = PressScale(ic, b.Pressed(), false)
	})
	return b
}

// IconAccentOnHover 是「悬停时图形变主色」那一档字色。
//
// ⚠️ **问的是按钮的悬停**：图标的悬停要问它自己，而它那时候还没长在按钮里面
// （`Children` 的回调是在建的时候跑的），问出来的是「它在按钮外面」。
//
// ⚠️ **不该染色时返回 ok=false**，调用方就**别设**颜色——那样图标接着继承按钮
// 的字色（赞键那种「没赞是灰的、赞了是主色的」就是靠这一条）。
//
// 这条判断单独拿出来，是因为**图标键不止一种形状**：赞键那一枚是「图标 + 底下
// 一圈背景」两层（见 `internal/ui/like.go`），它自己也要这一档主色——而「哪一档
// 主色」只有这一个决定的地方。
func IconAccentOnHover(c *ui.Context, disabled, hovered bool) (ui.Color, bool) {
	return iconAccentOnHover(c, disabled, hovered, ThemeBase)
}

// IconAccentOnHoverOnBase 是 IconAccentOnHover 的「**底色可传**」变体：悬停时
// 图形变的那一档主色按**给定的底**算。
//
// ⚠️ **给了底就按底算，不看主题深浅**：主题深浅管的是「页面上字压着的那块底」，
// 而这里的底是调用方给的那一块——浅色主题下满屏播放器那一行也坐在深底上。
func IconAccentOnHoverOnBase(c *ui.Context, disabled, hovered bool, base Base) (ui.Color, bool) {
	return iconAccentOnHover(c, disabled, hovered, base)
}

// iconAccentOnHover 是「悬停时图形变主色」那一档字色（底色可传）。
//
// ⚠️ **不该染色时返回 ok=false**，调用方就**别设**颜色——那样图标接着继承按钮
// 的字色（赞键那种「没赞是灰的、赞了是主色的」就是靠这一条）。
//
// 这条判断单独拿出来，是因为**图标键不止一种形状**：赞键那一枚是「图标 + 底下
// 一圈背景」两层（见 `internal/ui/like.go`），它自己也要这一档主色——而「哪一档
// 主色」只有这一个决定的地方。
func iconAccentOnHover(c *ui.Context, disabled, hovered bool, base Base) (ui.Color, bool) {
	if disabled || !hovered {
		return ui.Color{}, false
	}
	if !base.given() {
		return ink.AccentOn(c.Theme()), true
	}
	return ink.AccentOnBase(c.Theme(), base.color, ink.GraphicContrast), true
}

// IconLabelButton 是一个「图标 + 文字」的扁平按钮：图标在一个 cell×cell 的
// **格子**里，文字在格子右边。
//
// 格子是给「图标要成列」的那些地方用的（侧栏菜单项）：图标自己只有 font 那么
// 大（上游 18），而上游把那一格留成了 40×40（HeroUI 的 Button md 是 `h-10`，
// 折叠态那个图标格也是 `h-10 w-10`）——**格子才是那一行的高度**，图标在格子
// 里居中。
//
// 内容**左对齐**（`Justify(Start)`）：框架的按钮默认把内容居中，而上游那一行是
// `justify-start`——图标要成列，内容居中就没有列了。
//
// 🔴 **别给它补 `Label(label)`**：可见的字就是它的无障碍名字（同 `Button`）。
func IconLabelButton(c *ui.Context, svg *ui.SVG, label string, font, cell float32) ui.Element {
	b := Button(c, "").
		Height(cell).
		Padding(space.None, labelPad).
		Gap(labelGap).
		Radius(c.Theme().Radius).
		Justify(ui.Start)
	b.Children(func() {
		ui.Column(c).Size(cell, cell).AlignItems(ui.Center).Justify(ui.Center).
			Children(func() {
				ic := ui.Icon(c, svg).FontSize(font)
				ic = PressScale(ic, b.Pressed(), false)
			})
		ui.Text(c, label).SingleLine()
	})
	return b
}

// IconCellButton 是一个**只有图标格**的扁平按钮：整个按钮就是那一个 cell×cell
// 的格子，图标在它中间。
//
// 这是折叠态那一列要的形状（上游折叠态摆的是一个 `h-10 w-10` 的 Avatar，
// 图标是它的 fallback）。和 `IconButton` 的差别是**尺寸**：那一个由调用方的
// `Size` / `Padding` 撑，这一个把尺寸定死成格子。
//
// ⚠️ **内边距必须清零**：`Button` 默认给的是 `Padding(6, 14)`，而格子里只有
// 一枚 font 那么大的图标——14 的内边距会把 40 的格子掏成 12 宽，图标直接被裁。
func IconCellButton(c *ui.Context, svg *ui.SVG, label string, font, cell float32) ui.Element {
	b := Button(c, "").Label(label).Size(cell, cell).Padding(space.None, space.None)
	b.Children(func() {
		ic := ui.Icon(c, svg).FontSize(font)
		ic = PressScale(ic, b.Pressed(), false)
	})
	return b
}

// Dimmed 给一个**已经禁用**的控件换上「禁用态也读得出来」的字色。
//
// # 为什么需要它
//
// 框架对禁用元素的做法是整个元素乘 50% 不透明度（`mygo/ui/paint.go:209-212`）。
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
func Feel(c *ui.Context, e ui.Element) ui.Element { return feel(c, e, ThemeBase) }

// feel 铺上「悬停 / 按下」的那一层反馈。
//
// ⚠️ **「铺不铺」在建元素时问，「是不是禁用」在画的时候问**——见 `shade`。
//
// ⚠️ 它是在建元素时算的：`Element` 上没有 `styleFn`，而框架自己的 `styleButton`
// 是把同一段逻辑挂进 `styleFn`（paint 时跑）才拿到「按下的那一帧立刻变深」。
// 这里靠的是指针位置**不进事件循环就不变**（`mygo/ui/input.go:265` 的
// `setHover`），于是画面上的效果一样。本包早就在用这个写法
// （`internal/ui/navbar_suggest.go` 的 `viewSuggestTerm`）。
func feel(c *ui.Context, e ui.Element, base Base) ui.Element {
	t := c.Theme()
	switch {
	case e.Pressed():
		e = e.DrawOver(shade(e, t, pressA, base))
	case e.Hovered():
		e = e.DrawOver(shade(e, t, hoverA, base))
	}
	return e
}

// shade 是「悬停 / 按下」那一层：往按钮上压一层半透明的前景色。
//
// 为什么压前景色而不是换成一个底色（框架的 Toolbar 按钮用的就是
// `t.SurfaceHover`）：换成底色的话，选中态那一层主色淡底会被整块顶掉——指针一
// 落上去，「当前在哪」就整个不见了。而压一层前景色在**任何**底上都成立：透明底
// 上是变灰，主色淡底上是变深，和上游 `bg-foreground/10` 是同一个做法。
func shade(b ui.Element, t *ui.Theme, a float32, base Base) func(p *ui.Painter, r ui.Rect) {
	fg, radius := shadeInk(t, base).Alpha(a), t.Radius
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

// shadeInk 是「悬停 / 按下那一层」该压什么色。
//
// # 按主题算（默认）
//
// 主题正文色 `t.Text`——和这一层加进来之前一模一样：浅色主题下是黑的（压在白
// 底上是变灰）、深色主题下是亮的（压在深底上是变亮）。
//
// # 按给定的底算
//
// **按底的深浅挑黑或白**（`ink.PivotLuminance` 是「黑白两个字对比度相等」的那
// 个点，和满屏播放器挑前景色用的是同一个数）。
//
// ⚠️ **按主题挑在深底上是错的**：浅色主题的正文色是黑的，而满屏播放器那一行
// 坐着的是**压暗过的封面**——8% 的黑压在那一层上画面上几乎看不出变化（那一行
// 的悬停反馈等于没有，票 #248）。深底压白、浅底压黑，两边都看得出变化。
func shadeInk(t *ui.Theme, base Base) ui.Color {
	if !base.given() {
		return t.Text
	}
	if ink.Luminance(base.color) > ink.PivotLuminance {
		// 浅底 → 压黑。
		return ui.Color{A: 255}
	}
	// 深底 → 压白。
	return ui.Color{R: 255, G: 255, B: 255, A: 255}
}
