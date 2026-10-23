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
// 我们这边那圈描边的出处是 `mygo@v0.6.2/ui/widgets.go` 的 `styleButton`：
// `base, border = t.Surface, t.Border`。
//
// **配方本身在 `internal/ui/widgets/flat`**：列表包（`widgets/list`）每一行末尾
// 那枚「⋯」要用同一份，而界面层在列表包的上面——反过来 import 是个环。理由和
// 那边的取舍写在那个包的文件头上。
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
// `t.Border`（票 #172 之后是 15% 白叠出来的 `#3b3b3d`）压在 `#18181b` 上是
// 一道亮线，每一格都亮一下，一眼就是「另一种控件库」。而同一个 alpha 的淡底
// 深色下几乎看不见，所以选中态那一层深浅给的是两个数（`flatSelected`），不是
// 一个。
package ui

import (
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/flat"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
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

// flatCurrent 是「这一项正选中」的那一层：**一块中性的底 + 主色的字**。
//
// 原来我们侧栏那一行刷的是**实心主色**（`sidebar.go` 的 `Background(t.Accent)`），
// 那是调研点名的那处（报告 §2 基线 B 标注 3）。上游那一格长什么样，
// `_research/biu/screenshots/home.png` 上量得到：**中性灰底 + 主色的字**——
// pill 是 `#27272c`，字取最亮那一处是 `#66cc8a`，两者差 **7.7:1**。
//
// # 🔴 **深浅两套现在是同一个东西**（票 #239，审计 F-15）
//
// 改之前浅色那一支是**主色当底**（主色 14% 那一层淡绿底 + 正文色），深色那一支
// 是**主色当字**（中性灰底 + 亮绿的字）——**同一个控件，主色的角色在两套主题里
// 换了**：浅色下整个侧栏因此「更绿」（那一格是 `#dff9e9`，整窗 6881 像素 =
// 0.72%），深色下同一格是 `#3e3e41`。
//
// 现在两套都是「**中性灰底 + 主色的字**」：
//
//   - **底**：深浅两套都是 `flatSelected`（浅色 #dcdcdf / 深色 #3e3e41）。
//   - **字**：深色下是主色本身（亮绿压深灰 5.55:1，够）；浅色下是**从主色现算的
//     一个压深变体**（`ink.Ensure`，≥4.5:1；默认主色下算出来是 #107032）
//     ——主色（`#1ed760`）压浅底只有 1.4:1，直接用等于把这一行读不出来，而浅色下
//     主色当字必须压深是
//     DESIGN.md §七 明写的规矩（「我们比上游好、不要改回去」）。
//
// ⚠️ **主色的角色统一成「字」**：两套里它都不当底了。浅色下那一格从一大块淡绿
// 变成一小段深绿字——主色的出现率降一截，而整份主题里主色终于只有一个角色。
//
// # 🔴 和「正在播放」那一支现在**是**同一件事
//
// 改之前这两支是相反的：这里是「主色当底」，列表行 / 网格卡片的「正在播放」是
// 「主色当字」（`ink.AccentOn`）。底换掉之后，两支合了：都是「中性底 + 主色的
// 字」，浅色下那个字都是压深的那一档。
//
// 底下为什么是中性灰而不是主色淡底：绿字压绿底是同一支颜色压自己——浅色下那一格
// 从改前的 9.2:1 掉到 1.55:1，直接不达标；而上游那一格本来就是中性灰。
// `TestTheSelectedRowKeepsItsContrast` 逐条钉着这几个数。
//
// **「没选中」就是不调它**：那一支一个字都不改，颜色继续由 `flat` 那个包里的
// 反馈层按悬停 / 按下决定。
func flatCurrent(c *ui.Context, e ui.Element) ui.Element {
	t := c.Theme()
	return e.Background(flatCurrentBg(t)).TextColor(flatCurrentText(t, true))
}

// flatCurrentBg 是 flatCurrent 那一层**底色**。
//
// ⚠️ **深浅两套是同一个数**（票 #239，审计 F-15）：以前浅色那一档是**半透明**的
// （主色 14%），屏幕上的数是它合到身下那块底板之后的结果，所以断言那一格的
// 颜色时得先知道底板是谁（见 `currentitem_test.go`）。现在是**不透明**的中性灰，
// 断言时直接比那个数。
func flatCurrentBg(t *ui.Theme) ui.Color {
	return flatSelected(t)
}

// flatCurrentText 是 flatCurrent 配的那一档**字色**。
//
// 之所以单列一个函数：有些地方的字是**自己画**的（不是从父元素继承）——
// 收藏夹那一行的歌名就是（`sidebar_favorites.go` 的 `rowTextColor`），队列抽屉
// 那一行也是（`queue_drawer.go` 的 `playlistTextColor`）。那几处必须和这一档
// **同一个数**，否则「底换了、字没换」——浅色下就是一行灰底上的深色字和别处的
// 主色字，深色下则是灰底上的黑字。
//
// ⚠️ **浅色那一支是「压深的主色」，不是正文色**：主色（亮绿）压在浅底上不到
// 2:1，当字色等于把这一行读不出来（见上面那一大段）。而「主色压深到 ≥4.5:1」
// 是 DESIGN.md §七 定的规矩——浅色下它算出来是一个深绿（默认主色下是
// `#107032`），深色下就是主色本身。
//
// 主色是**用户可设的**，所以那个变体每次都从当前主色现算（`ink.Ensure`），
// 这里没有一个写死的绿。
func flatCurrentText(t *ui.Theme, current bool) ui.Color {
	if !current {
		return t.Text
	}
	if t.Dark {
		return t.Accent
	}
	return ink.Ensure(t.Accent, flatCurrentBg(t), 4.5)
}

// flatSolid 是「**被选中的选项**」那一档：一块**实心主色**，字取 `t.AccentText`。
//
// # 它和 `flatCurrent` 是两件事
//
// `flatCurrent` 是「**当前在哪**」那一档（中性灰底 + 主色的字，浅色下那个主色
// 是压深的那一档）——侧栏菜单、左栏那三处、队列抽屉的「正在播放」都是它。
//
// 而 `flatSolid` 是「**被选中的选项**」那一档（实心主色底 + `AccentText` 字）——
// 设置页那些**选项**（主题三档、窗口关闭两档、下拉项的高亮）都是它。
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
