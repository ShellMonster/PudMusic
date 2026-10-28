// 界面层用的扁平按钮：**无描边、无常驻底色**。
//
// # 配方在 `widgets/flat` 里，不在这里
//
// 这份配方是 BMG-70 那份视觉调研（`~/.herdr-projects/reports/Biu_MyGo/70/`）的
// 第一条差距的落点（报告 §1.1 记作 **F1**、量级 M、「单条影响面最大」）：
//
//	Biu 怎么做：HeroUI 的 `Button variant="light"`／`"flat"`：**无边框、透明底**，
//	hover 才上一层很淡的底色。侧栏菜单项、播放条图标键、倍速键、折叠键全是这一类。
//
//	我们现在什么样：MyGo 的 `ui.Button` 默认就是
//	`Background(t.Surface) + Border(1,t.Border)`，于是**侧栏菜单的每一项、顶栏
//	那三个图标键、倍速键、侧栏折叠键、队列抽屉行尾的「⋯」键**都是描边药丸。
//
// 我们这边那圈描边的出处是 `mygo@v0.6.3/ui/widgets.go` 的 `styleButton`：
// `base, border = t.Surface, t.Border`。
//
// **配方本身在 `internal/ui/widgets/flat`**：列表包（`widgets/list`）每一行末尾
// 那枚「⋯」要用同一份，而界面层在列表包的上面——反过来 import 是个环。理由和
// 那边的取舍写在那个包的文件头上。
//
// # 尺寸照抄 `styleButton`，是刻意的
//
// 内边距、圆角、图标与字之间的缝全部取 `ui.Button` 用的那三个数，于是换上去之后
// **布局一个像素都不动**——侧栏宽度与折叠、播放条 76/16/52、网格断点那些调研
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
// `t.Border`（票 #172 之后是 15% 白叠出来的 `#3b3b3d`）压在 `#18181b` 上是
// 一道亮线，每一格都亮一下，一眼就是「另一种控件库」。而同一个 alpha 的淡底
// 深色下几乎看不见，所以选中态那一层深浅给的是两个数（`flatSelected`），不是
// 一个。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/flat"
	"github.com/egoist/mygo/ui"
)

// flatButton 是一个无描边、无常驻底色的按钮，画着 label。
//
// 🔴 **别给它补 `Label(label)`**：可见的字就是它的无障碍名字，补上去会多出一个
// 同样文字的无障碍节点，画面上出现两份（和 `sidebar.go` 的 `viewSideMenu` 里
// 那一处同源）。**只有图标按钮要补**——见 `flatIconButton`。
func flatButton(c *ui.Context, label string) ui.Element {
	return flat.Button(c, label)
}

// flatLink 是「名字型链接」那一档：**一句话字**，悬停时底下长出一道下划线，
// **没有按钮壳**（没有底色、没有胶囊、没有描边）。
//
// 上游 Biu 的作者名是 `hover:underline`——用户 2026-10-25 说的「不要做成一个
// 胶囊一样的包着那个名称的，用正常的下划线就行」就是这一档。
//
// 🔴 **别给它补 `Label(label)`**：可见的字就是它的无障碍名字（同 `flatButton`）。
//
// 配方、理由、键盘可达那一段，都在 `widgets/flat` 的 `Link` 上面写着。
func flatLink(c *ui.Context, label string) ui.Element {
	return flat.Link(c, label)
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
	return flat.IconButton(c, icon(name), label, font)
}

// flatIconButtonAccent 是 flatIconButton 加上「**悬停时图形变主色**」。
//
// 上游 `IconButton` 的 `hover:text-primary`（报告 §3.5）。理由、颜色那一档怎么取、
// 以及「禁用的状态必须从这里给」的原因，都在 `widgets/flat` 的 `IconButtonAccent`
// 上面写着。
func flatIconButtonAccent(c *ui.Context, name, label string, font float32, disabled bool) ui.Element {
	return flat.IconButtonAccent(c, icon(name), label, font, disabled)
}

// flatIconButtonAccentOnBase 是 flatIconButtonAccent 的「**底色可传**」变体：
// 悬停时图形变的那一档主色按**给定的底**算，而不是按 `t.Background` 算。
//
// 满屏播放器那一行（上一首 / 下一首 / 播放模式 / 队列开关）的底是**压暗过的
// 封面**：按页面白底算出来的那一档主色（浅色主题下是深绿 `#107032`）压在那上面
// 只有 **1.0:1**——指针一停上去，那几个图标**反而不见了**（票 #248）。
func flatIconButtonAccentOnBase(c *ui.Context, name, label string, font float32, disabled bool, base flat.Base) ui.Element {
	return flat.IconButtonAccentOnBase(c, icon(name), label, font, disabled, base)
}

// flatIconLabelButton 是一个「图标 + 文字」的扁平按钮：图标在一个 cell×cell 的
// 格子里，文字在格子右边，内容**左对齐**。
//
// 侧栏菜单项是它（上游 HeroUI 的 Button md：左边 `startContent` 是 18 的图标，
// 右边是名字，整行 40 高）。
//
// ⚠️ **别给它补 `Label(label)`**：可见的字就是它的无障碍名字（同 flatButton）。
func flatIconLabelButton(c *ui.Context, name, label string, font, cell float32) ui.Element {
	return flat.IconLabelButton(c, icon(name), label, font, cell)
}

// flatIconCellButton 是一个**只有图标格**的扁平按钮：整个按钮就是那一个
// cell×cell 的格子，图标在它中间。
//
// 侧栏折叠态那一列是它（上游折叠态摆的是一个 `h-10 w-10` 的图标格）。
//
// 🔴 **label 不许省**，理由和 `icons.go` 的 `iconButton` 一字不差：图标对辅助技术
// 不可见，而这个按钮再没有字可念。它同时也是无头 Tester 认得这个按钮的唯一凭据。
func flatIconCellButton(c *ui.Context, name, label string, font, cell float32) ui.Element {
	return flat.IconCellButton(c, icon(name), label, font, cell)
}

// flatCurrentBg 是 flatCurrent 那一层**底色**。
//
// ⚠️ **深浅两套是同一个数**（票 #239，审计 F-15）：以前浅色那一档是**半透明**的
// （主色 14%），屏幕上的数是它合到身下那块底板之后的结果，所以断言那一格的
// 颜色时得先知道底板是谁（见 `currentitem_test.go`）。现在是**不透明**的中性灰，
// 断言时直接比那个数。
func flatCurrentBg(t *ui.Theme) ui.Color {
	// 设计规范 §2 的 `accentSoft`：强调色的淡底，不再是中性灰（`flatSelected`）。
	// 字色仍由 `flatCurrentText` 按**这一层底**现算（`ink.Ensure` ≥ 4.5:1），
	// 所以换底不会让字读不出来。上面那一大段关于「中性灰底」的历史说明是 Biu 时代的。
	return accentSoft(t)
}

// flatCurrentText 是「**当前在哪**」那一档**没选中**时的字色——正文色。
//
// ⚠️ **它只剩这一个分支**：选中那一档已经统一成**实心主色底**
// （`flatSolidCurrent`，字取 `t.AccentText`），那一支不再经过这里。现在还在用
// 它的只有设置页只读档（`settings_page.go` 的 `pickSelected`：禁用时字取
// 正文色）。
func flatCurrentText(t *ui.Theme, current bool) ui.Color {
	return t.Text
}

// flatSolid 是「**被选中的选项**」那一档：一块**实心主色**，字取 `t.AccentText`。
//
// # 它和 `flatCurrent` 是两件事
//
// `flatCurrent` 是「**当前在哪**」那一档（中性灰底 + 主色的字，浅色下那个主色
// 是压深的那一档）——侧栏菜单、左栏那三处都是它。
//
// ⚠️ **队列抽屉的「正在播放」那一行不在这一档里**（票 #294，2026-10-24）：它走
// 的是「淡主色底」（`playingrow.go` 的 `playingRowBg`）——那一层淡底是状态识别，
// 一眼看出哪一首在放。（分集抽屉的「正在放」那一行也是那一档，票 #323。）
//
// 而 `flatSolid` 是「**被选中的选项**」那一档（实心主色底 + `AccentText` 字）——
// 设置页那些**选项**（主题三档、数据显示样式三档、窗口关闭两档、下拉项的高亮）都是它。
// 注意设置页那三处分段控件是**同一个出口**（`settingsSegment`，票 #240 / 报告 §4 的
// F-17）：设置页只有一种分段控件。
//
// 上游这两处都是实心的：`color="primary" variant="solid"`
// （`components/music-rate/index.tsx:26-46`，票 #220 / 报告 §3.10；HeroUI 的
// `Radio` 选中项也是 `bg-primary` 实心底）。
//
// 差在哪：实心主色底上，字**不能**是主色（绿字压绿底 = 同一支颜色压自己，
// 浅色下 1.55:1）——所以字取 `t.AccentText`：那是框架按主色的亮度挑出来的黑或白
// （`mygo/ui` 的 `applyAccent`），**在主色底上一定读得出来**。
//
// 底是主色、字是 `AccentText`——这和「主色当字色」是两回事（后者取
// `ink.AccentOn` / `ink.Ensure`，那是**浅底**上要用的那一档）。
//
// ⚠️ **禁用时别用这一档**：框架对禁用元素是**整个乘 50% 不透明度**，实心主色底
// 跟着淡下来之后，字（`ink.Disabled`）压在上面只剩 2.6:1 / 2.9:1，掉到 3:1
// 以下。设置页那两处档位在只读时改用 `flatCurrent`——见 `settings_page.go` 的
// `pickSelected`。
//
// ⚠️ **只读那一档的画面上是「灰底 + 灰字」**：`flat.Dimmed` 排在后面，把
// `flatCurrent` 铺的主色那一档的字换成了禁用灰，所以**主色在只读时不出现**。
func flatSolid(c *ui.Context, e ui.Element) ui.Element {
	t := c.Theme()
	return e.Background(t.Accent).TextColor(t.AccentText)
}

// flatSolidCurrent 是「**当前在哪**」的**实心主色底**那一档（票：用户要
// 「深底色 + 白字」）。
//
// # 它和 `flatCurrent`（淡底）是两件事
//
// `flatCurrent` 是「主色淡底 + 按那层底现算的字」——它和侧栏底色的对比度只有
// **1.09:1 ~ 1.29:1**（实测暖纸浅色 / 靛夜深色），**几乎分不出来**。而「当前
// 在哪」是侧栏里唯一的方向标。
//
// 这一档是**实心主色底**，和它挨着的侧栏底色差着 **4.34:1 ~ 6.72:1**。
//
// # 字色：`t.AccentText`，不是写死的白字
//
// ⚠️ **主色是用户可设的，而深色方案的主色是亮色**（暖纸深色 `#E58A5A`）——
// 白字压上去只有 **2.59:1**，读不出来。而 `t.AccentText` 是框架按主色亮度挑出来
// 的黑或白（`applyAccent`，`theme.go`），**在主色底上一定读得出来**
// （实测 5.24:1 ~ 9.23:1）。
//
// ⚠️ **浅色下它多半就是白字**（经典浅色的主色 `#1ED760` 亮，`AccentText` 是
// **近黑** `#18181B`——9.23:1）。所以「深底色 + 白字」这个说法在**浅色方案**下
// 成立，在经典和深色方案下是**深底 + 深字**。这不是妥协，是 WCAG：
// 亮绿底上的白字只有 1.92:1。
//
// # 次要字（收藏夹那行的「N 首」）
//
// ⚠️ **`t.TextMuted` 压在主色底上只有 1.02:1 ~ 1.34:1**——完全读不出来。
// 所以那一行的小字要按主色底重算（`flatSolidCurrentMutedText`）。
//
// # 和 `flatSolid`（「被选中的选项」）的差别
//
// `flatSolid` 是设置页那些**选项**（主题三档、下拉项高亮）——那些是**值**，
// 而这一档是**位置**。两者碰巧都是实心主色底，所以这一档不另起一个名字，
// 但**它多带两样**：一是次要字那一档（`flatSolidCurrentMutedText`），二是
// 悬停 / 按下要跟着主色走（见 `flatSolidCurrentFeel`）。
func flatSolidCurrent(c *ui.Context, e ui.Element) ui.Element {
	t := c.Theme()
	return e.Background(t.Accent).TextColor(flatSolidCurrentText(t))
}

// flatSolidCurrentText 是实心主色底上该用的**字色**。
//
// 就是 `t.AccentText`——框架按主色亮度挑出来的黑或白，在主色底上一定读得出来
// （理由见 `flatSolidCurrent`）。
func flatSolidCurrentText(t *ui.Theme) ui.Color {
	return t.AccentText
}

// flatSolidCurrentMutedText 是实心主色底上该用的**次要字色**（收藏夹那行的
// 「N 首」）。
//
// 🔴 **不能直接用 `t.TextMuted`**：它压在主色底上只有 1.02:1 ~ 1.34:1——完全
// 读不出来。所以从 `t.AccentText` 出发，**朝主色混一点**压暗一档。
//
// ⚠️ **那个数是量出来的，不是拍的**：混 30% 只有 3.43:1（暖纸浅色）——而
// 「N 首」是 12 号字，不算 WCAG 的「大字」，判据是 **4.5:1**。所以上限是
// **10%**（最差 4.58:1，暮光琥珀浅色）；取 **8%** 是留一点余量（最差
// 4.70:1）。
//
// ⚠️ **这个余量只对内置的八套方案成立**：主色是用户可设的，用户挑一个中间调
// 的饱和色（比如亮青 `#10c8ff`）时，`AccentText` 自己都只有 2:1 上下——那是
// `applyAccent` 那一层的事（它现在按 WCAG 亮度挑黑白，见 `theme.go`），不是
// 这一层能兜住的。这一层兜的是「内置方案的八档全过」。
//
// ⚠️ **8% 够不够看出它是次要的**：弱——它和名字那一档（`AccentText`）只差
// 同一支色上的一档深浅，真正的层级信号是**字号**（14 对 12）。要更明显的
// 次要层级就得牺牲对比度，两者只能取一头。
func flatSolidCurrentMutedText(t *ui.Theme) ui.Color {
	return t.AccentText.Mix(t.Accent, 0.08)
}

// flatSolidCurrentFeel 是实心主色底那一档的**悬停 / 按下**反馈：**换主色自己的
// hover / pressed 档**。
//
// 🔴 **不能压前景色**（`flat.Feel` 那一层 8% / 16% 的半透明前景）：实测压在
// 主色底上只有 **1.15:1 ~ 1.19:1**（悬停）——指针落上去和不落几乎分不出来。
// 也不能用 `flat.FeelOnBase`（按底色深浅挑黑白再压）——同样的量级。
//
// ⚠️ **这一档是「换底」，不是「压一层」**：和 `iconButtonPlay`（主色圆底悬停
// 变 `AccentHover`、按下变 `AccentPressed`）同一个做法。它和 `flat.Feel` 注释
// 里那条「换底会顶掉选中层」不冲突——这一层**本身就是**选中那一层，换成的
// 还是主色（亮/暗一档），「当前在哪」的信号没有走。
func flatSolidCurrentFeel(c *ui.Context, e ui.Element) ui.Element {
	t := c.Theme()
	switch {
	case e.Pressed():
		return e.Background(t.AccentPressed)
	case e.Hovered():
		return e.Background(t.AccentHover)
	}
	return e
}

// flatSelected 是选中态那一层**中性**的底。
//
// 往 `t.SurfaceHover` 的方向再走 6%：只取 `t.SurfaceHover` 的话，选中的那一格和
// **悬停**的那一格底色一模一样（两个都是它），而「当前在哪」是侧栏里唯一的方向标，
// 被一次悬停盖掉比多画一层底更坏。多走的那 6% 正好拉开：浅色 #e9e9ec → #dcdcdf、
// 深色 #323236 → #3e3e41，两边都还只是「很淡的一层底」。
//
// ⚠️ **`Mix` 走的是 sRGB**（`mygo/ui/color.go:105`，不是线性光），所以那 6% 在两套
// 主题里换算出来的差值一样（各 12~13 个通道）。
func flatSelected(t *ui.Theme) ui.Color {
	return t.SurfaceHover.Mix(t.Text, 0.06)
}
