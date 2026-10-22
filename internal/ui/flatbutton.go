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
// （14.6:1）。
//
// # 🔴 和「正在播放」那一支不是同一件事，别把两边改成一样
//
// 这一处是「主色当**底**」：浅色下不满意的其实是字压在主色（或主色淡层）上，
// 处理办法是**不动字、只换底**（底用中性灰或很淡的一层主色）。
//
// 列表行 / 网格卡片的「正在播放」是「主色当**字**」：那里没有主色底，字直接压
// 在页面底色上，处理办法是**从主色算一个与底色的对比度 ≥4.5:1 的变体**当字色
// （`widgets/ink` 的 `AccentOn`），主色本身不动。浅色下那个变体比主色深一档。
//
// 两个方向恰好相反，因为要保的东西不同：这里要保「主色的那层底还在」，那里要保
// 「这一行读得出来」。把这里的字改成 `AccentOn`（或把那里改成只换底）都会在
// 另一处重新制造同一个毛病——票 #139 逐条量过。
//
// # 底下为什么是中性灰而不是主色淡底
//
// 绿字压绿底是同一支颜色压自己：浅色下那一格从改前的 9.2:1 掉到 1.55:1，直接不
// 达标；而上游那一格本来就是中性灰。`TestTheSelectedRowKeepsItsContrast`
// 逐条钉着这几个数。
//
// **「没选中」就是不调它**：那一支一个字都不改，颜色继续由 `flat` 那个包里的
// 反馈层按悬停 / 按下决定。
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
