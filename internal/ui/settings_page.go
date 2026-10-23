// 设置页：Biu 的 `/settings`（`src/pages/settings/`，`index.tsx` 是那一页的壳）。
//
// ## 四个 Tab 是哪四个
//
// 照 Biu 的 `index.tsx:124-138`：`常规设置` / `菜单设置` / `快捷键设置` /
// `代理设置`，连顺序都照抄。每一个 Tab 的内容在各自的文件里（`settings_tab_*.go`），
// 这一份只管壳、共用的行、以及「只读时怎么办」。
//
// ## 每一个控件改的都是**盘上已经有的键**，一个都不新造
//
// 常规/菜单/代理三个 Tab 改的是 `app.Settings`（键名见
// `internal/app/settings_test.go` 的 `biuKeys`）；快捷键 Tab 改的是
// `shortcuts.json`（Biu 自己也是单独一个 store，见 shortcuts.go）。
// 设置页是那些键的入口，不是键名的发明地——用户来回切 Biu 和 MyGo 时丢设置，
// 是最难查的一类数据丢失。
//
// 🔴 本文件里的任何 view **不许**调 `c.SetTheme`：主题每帧开头装一次就够，
// 别的页面照样生效（铁律二，见 theme.go）。
package ui

import (
	"strings"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/platform"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/flat"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// settingsPath 是设置页那条路由，和 router.go 里登记的那一条是同一个字符串。
//
// 拿来和 `Route.Match` 比，而不是拿 `routes[6]`：那条表一改顺序，这里跟着错，
// 而且错得很安静——设置页退回占位页，没人看得出来。
const settingsPath = "/settings"

// goToDownloadDirSetting 去设置页的「下载目录」那一项。
//
// ⚠️ **它不只切页**：设置页有四个 Tab，而「下载目录」在「常规设置」那一格的
// 第三段（要往下滚才看得到）。只把用户送到设置页的话，等于让他自己把「下载
// 记录页的一句话」翻译成「设置页第 N 个 Tab 的第 M 行」（报告 C1）。
//
// 所以这里三件事一起做：切到那一格、**指**着那一行（标题是主色）、让那一页
// 把它滚进视野（见 settingsScrollFocusIntoView）。
//
// ⚠️ **它是从别的页面调过来的**：调用方是下载记录页（那一句「还没有下载
// 目录」）。设置页自己不会指着谁——`settingsFocusNone` 就是「没指」。
func (s *Shell) goToDownloadDirSetting() {
	s.settings.tab = settingsTabGeneral
	s.settings.focus = settingsFocusDownload
	s.settings.focusScrolled = false
	s.settings.focusTries = 0
	s.navigate(settingsPath)
}

// settingsPageState 是设置页自己那一摊，**只由 view 在主线程上读写**。
type settingsPageState struct {
	// tab 是切在第几个 Tab。建在 Shell 上而不是每帧的局部变量：即时模式每一帧
	// 都重画一遍，局部变量下一帧就没了（见 Shell.searchKeyword 的注释）。
	tab int

	// scroll 是这一页的滚动位置。按 Tab 时回到顶上——Biu 的 Tabs 换了面板就是
	// 新的一页，滚到一半切过去会落在半截上。
	scroll ui.ScrollState

	// shownTab 是上一帧画的是第几个 Tab，用它认出「刚按了 Tab」。
	shownTab int

	// focus 是「从别处跳过来时要指的那一项」（见 goToDownloadDirSetting）。
	//
	// ⚠️ **它不是路由**：跳过来的时候用户要的是「那一项」，不是「设置页」——
	// 而设置页有四个 Tab、几十行，「下载目录」在第三段里，不指一下用户还得
	// 自己找（报告 C1：「等于让用户自己把『下载记录页的一句话』翻译成『设置页
	// 第 N 个 Tab 的第 M 行』」）。
	focus settingsFocus

	// focusEl 是被指着的**那一行**的盒子，focusScrolled 记「滚进视野」这件事
	// 做了没有。
	//
	// ⚠️ **滚进视野要跨两帧**：量的是**上一帧**的盒子（`Element.Bounds()` 的
	// 规矩），而刚跳过来那一帧画的是别的页——所以量不到的时候要**再要一帧**。
	focusEl       ui.Element
	focusScrolled bool
	// focusTries 是「量不到、再要一帧」已经试了几次。
	focusTries int

	// scrollEl 是这一页那个滚动容器（`ui.Scroll`）的盒子：量「视野」要靠它
	// （行的盒子是窗口坐标，容器的盒子也是，两个一比就知道它在不在视野里）。
	scrollEl ui.Element

	// descFloor 是说明列这一帧的**下限**（0 = 不给下限）。
	//
	// ⚠️ **每帧算一次**，不是每一行算一次：它取决于**内容区有多宽**
	// （窗口宽 - 侧栏宽 - 内边距），而这一页的每一行都是同一个内容区。
	// 算一次存下来，各行读它（见 `settingsDescFloor`）。
	descFloor float32

	// contentW 是内容那一段（**去掉左右内边距**）的宽。
	//
	// ⚠️ **控件列的宽度要钳在它里面**：跟字号缩放走的那一列（分段控件）在
	// 1.4 倍下是 420，而内容区最窄的那一档（窗口 900 + 侧栏 480）只有 364——
	// 不钳的话那一列就**比内容区还宽**，右边被窗口切掉。
	contentW float32
}

// settingsFocus 是「从别处跳过来时要指的那一项」的身份。
type settingsFocus string

const (
	// settingsFocusNone 是「没指着谁」。
	settingsFocusNone settingsFocus = ""
	// settingsFocusDownload 是「常规设置」里的「下载目录」那一行。
	settingsFocusDownload settingsFocus = "downloadPath"
)

// 四个 Tab。顺序就是从左到右的顺序，和 Biu 那一页一样。
const (
	settingsTabGeneral = iota
	settingsTabMenu
	settingsTabShortcut
	settingsTabProxy
)

// settingsTabNames 是四个 Tab 上的字。照 Biu `index.tsx:125-137` 的 title。
var settingsTabNames = []string{"常规设置", "菜单设置", "快捷键设置", "代理设置"}

// 控件列的宽度。设置页每一行是「左边标题 + 一句说明，右边控件」，**右边那一列
// 全部按同一个基准宽度排**（票 #235 的 F-04）。
//
// 300 是量出来的，不是挑的：主题那三个按钮里「跟随系统」四个汉字最宽，按钮
// 自己还要留内边距。窄了它会被画成「跟随…」——用户看见半个词，第一反应是
// 这个应用坏了，而不是「这个按钮窄了一点」。
//
// # 这一列因此有**一条共同的左边缘**
//
// 此前那一列的左边缘落在四个位置（762 / 801 / 822 / 942），而其中两个控件
// 还没填满自己的盒子——那一列看上去是参差的（UI 报告 §4 的 F-04）。
// 现在每一行的控件都占**同样宽**的一格，右边齐在内容那一段的右沿，
// 左边也就齐在同一条线上（内容右沿往左 `settingsControlW`）。
//
// ⚠️ **开关是那一列里唯一比基准窄的控件**（它天生就那么宽），它靠右排在那
// 一格的右端——开关在表单里本来就该贴着行尾。**其余的控件都正好是基准宽**
// （下拉也是：`selectWidth` 就是 `settingsControlW`），所以整列的左边缘仍然
// 是一条线。
//
// ⚠️ **文本缩放大于 1 的时候，「主题」「窗口关闭」那两行会跟着变宽**
// （它们里面的字要放得下，见 pickTheme 的注释）。那是这一列唯一的不齐，
// 基准宽度说的是**缩放为 1 的时候**。
//
// ⚠️ **三处分段控件走同一个出口**（`settingsSegment`）：设置页只有一种分段控件
// （票 #240 / 报告 §4 的 F-17）。
const (
	settingsControlW = 300
	radiusReadoutW   = 48
)

// settingsDescMinW 是**说明列**的最小宽度（左边标题 + 那一句话）。
//
// # 为什么要有这个下限
//
// 每一行是「左边标题 + 一句说明，右边定宽控件」，**控件那一列是定宽的**，
// 说明那一列吃剩下的——于是窗口越窄，说明被挤得越狠：
// 1200 宽时那一列是 520，900 宽（`WindowOptions.MinWidth` 定的最小尺寸）
// 时只剩 320，**掉掉 38%**；说明文字和控件之间的留白从 351 掉到 151
// （UI 报告 §4 的 F-27）。
//
// 320 是量出来的：说明文字里最长的一句是「内容页排成列表、网格还是紧凑」
// （15 个字，14px 下 210 像素），320 是它的 1.5 倍——再窄它就折行，而那一折
// 让**整行**变高，右边那个控件也跟着往下挪半格，一页设置于是参差不齐。
//
// 🔴 **这个下限是「有条件」的**（`settingsDescFloor`）：只有内容区**放得下**
// 它的时候才给。放不下时**不给**——那一档回到改动前的样子（说明列被压扁，
// 右边那列控件完整可见）。
//
// ⚠️ **为什么不能无条件给**：框架的 flex **没有收缩**——一项的 `shrink` 零值
// 就是 0（`mygo/ui/layout.go` 的 `resolveFlexible`：`factor = shrink × inner`），
// 所以**放不下就是溢出**：控件列会被顶出窗口右缘。于是「说明列至少 320 +
// 控件列至少 260 + 缝 24 = 604」在内容区窄过 604 时，用户看见的是半个按钮。
// （`MinWidth` 还是**硬下限**：`mygo/ui/layout.go` 里 `minW` 一旦存在就接管
// 那一项的自动最小宽，没有才用内容的 min-content 宽——所以 `MinWidth(0)`
// 不是「没有下限」，而是把下限变成 0。）
//
// 合法配置里最窄的那一档是**窗口 900 + 侧栏拉到最大 480**
// （`app.MaxSideMenuWidth`），内容区只有 900 - 480 - 56 = 364 ⇒ 604 放不下。
// 另一档是**窗口 900 + 系统字号 1.4 倍**：控件列跟着字号变宽（420），于是
// 320 + 24 + 420 = 764 > 644 ⇒ 同样放不下。
//
// ⚠️ **报告 §4 那个「掉到 44%」是按一个算错的数算出来的**：它说 900 那一档的
// 「设置栏宽」是 668，于是说明列是 290。真窗口那张 900×560 的截图上量出来的
// 是**内容右沿在 872**（= 900 - 28 的内边距），也就是设置栏 700 宽——**滚动条
// 是浮在上面的，不占宽度**（框架的 `scrollBars` 画在自己的盒子上，
// `mygo/ui/paint.go:1039`）。于是那一档的说明列是 **320**，掉的是 **38%**，
// 不是 44%。所以这一条现在**已经是达标的**，本票做的是**把它钉住**（下面那条
// 测试钉着它），不是把一个已经达标的数再往上推。
const settingsDescMinW = 320

// settingsControlMinW 是**控件列**的最小宽度。
//
// ⚠️ **它是为了「跟随系统」那一档**：四个汉字 14px 下是 56 像素，加上按钮自己
// 的内边距（`flat.Button` 的 14×2）要 84；三个格子加两道 4 的缝就是 260。
// 控件列窄过这个数，「跟随系统」就被裁成「跟随系…」——用户看见半个词，第一反应
// 是这个应用坏了，而不是「这个按钮窄了一点」。
//
// ⚠️ **它不跟字号缩放走**：1.4 倍时「跟随系统」要 106 像素一格，三格加缝是 327
// ——那个数当硬下限的话，内容区窄过 327 + 24 时又会**溢出**。所以下限取**固定**
// 的 260（1 倍字号的最小值），而**基准宽**才跟缩放走。
//
// ⚠️ **它只在控件列被内容区挤窄时才会被撞上**：900 宽（最小尺寸）、默认侧栏
// 那一档是 320 + 24 + 300 = 644（内容那一段的宽），控件列还是基准宽 300；
// 而「侧栏拉到 480」那一档内容区只有 364，控件列被挤到 320.4（`settingsControlFitW`
// 算出来的），还是高于 260 ⇒ 这一档「跟随系统」那三个字也放得下。
const settingsControlMinW = 260

// settingsDescFloor 是说明列这一帧的**下限**。
//
// 🔴 **下限是条件性的**：只有内容区**放得下**它的时候才给（0 = 不给）。
//
// 「放得下」= 内容区（去掉内边距）够不够 `说明列下限 + 缝 + 控件列的基准宽`。
// 放不下时**不给下限**——那一档就回到改动前的样子：说明列被压扁，
// **右边那列控件完整可见**（评审抓的就是这一点：控件被推出窗口右缘、
// 半个按钮看不见，比说明列窄一点坏得多）。
//
// ⚠️ **为什么不能用 `MinWidth` 一给了之**：框架的 flex **没有收缩**——一项的
// `shrink` 零值就是 0（`mygo/ui/layout.go` 的 `resolveFlexible`：
// `factor = shrink × inner`），所以**放不下就是溢出**：控件列被顶出窗口右缘。
//
// ⚠️ **而且 `MinWidth(0)` 不是「没有下限」**：框架里 `minW` 一旦存在就接管那一项
// 的**自动最小宽**（`mygo/ui/layout.go` 的 `flexLayout`：有 `minW` 就用它，
// 没有才用内容的 min-content 宽）。中文可以逐字换行，内容的 min-content 是**一个
// 字宽**；给了 0 就连那一个字都不剩，整行塌成 0 高——那一行在画面上整个消失。
//
// ⚠️ **控件列的基准宽取「分段控件那一族」的**（`scale.Font(c, settingsControlW)`）：
// 它是这一列里唯一跟着字号缩放走的（另外几行都固定 300），所以它是最坏的一档。
func (s *Shell) settingsDescFloor(c *ui.Context, st app.Settings) float32 {
	need := float32(settingsDescMinW) + space.Px24 + scale.Font(c, settingsControlW)
	if s.settingsContentInnerW(c, st) < need {
		return 0
	}
	return settingsDescMinW
}

// settingsContentInnerW 是设置的内容那一段（**去掉左右内边距**）的宽。
//
// 可视区 = 窗口宽 - 侧栏宽；内容段收在 min(缩放后的上限, 可视区) 里（`MaxWidth`
// 是上限，居中）。
func (s *Shell) settingsContentInnerW(c *ui.Context, st app.Settings) float32 {
	winW, _ := c.Size()
	avail := winW - s.settingsSideWidth(st)
	block := s.settingsContentMaxWAt(c)
	if avail < block {
		block = avail
	}
	return block - 2*settingsContentPad
}

// settingsContentMaxWAt 是**这一帧**内容段的上限（**跟着系统字号缩放走**）。
//
// 🔴 **为什么上限要跟着字号走**（票 #244 / 评审）：
//
// 每一行是「左边标题 + 一句说明，右边一枚控件」，而**说明那一句话本身是跟着字号
// 缩放的**（`scale.Font`）。内容段不跟着长的话，字号一大说明就**折行**——而说明
// 折行会让**整行**变高、右边那列控件跟着往下挪半格，一页设置于是参差不齐
// （`settingsDescMinW` 的注释写着这一条要避免）。
//
// 实测（1200 窗口，那句最长的说明「内容页排成列表、网格还是紧凑」）：
//
//	内容段不缩放	1.00 倍 1 行 / 1.25 倍 1 行 / 1.40 倍 1 行 / 1.50 倍 **2 行**
//	                1.75 倍 3 行 / 2.00 倍 5 行
//	内容段跟着缩放	1.00 倍 1 行 / 1.25 倍 1 行 / 1.40 倍 1 行 / 1.50 倍 1 行
//	                1.75 倍 1 行 / 2.00 倍 2 行
//
// 也就是：跟着缩放之后，只有**窗口本身已经不够宽**的那一档（2.0 倍）才折——
// 而那一档在改动之前也折（2 行）✓。
//
// ⚠️ **缩放之后上限会超过可视区**：1.4 倍时上限是 1092，而 1200 宽的窗口只有
// 1000 可视——`MaxWidth` 是上限不是定值，于是那一档的内容段就是**可视区那么
// 宽**（页面左右不留白）。这是对的：字大了就得多给地方。
func (s *Shell) settingsContentMaxWAt(c *ui.Context) float32 {
	return scale.Font(c, settingsContentMaxW)
}

// settingsControlFitW 是控件列在**这一帧**能占的宽（上限）。
//
// 🔴 **为什么必须自己算**：框架的 flex **没有收缩**——一项的 `shrink` 零值是 0
// （`mygo/ui/layout.go` 的 `resolveFlexible`：`factor = shrink × inner`，而
// `shrink` 的零值就是 0），所以**放不下就是溢出**：控件列会被推出窗口右缘，
// 用户看见的是半个按钮。
//
// 算式：控件列 ≤ 内容区宽 - 缝 - 说明列的最小宽。说明列的最小宽是**一个字**
// （中文可以逐字换行，内容的 min-content 就是一个字宽），除非这一帧给了它
// 更大的下限（`settingsDescFloor`）。
//
// ⚠️ **只有跟字号缩放走的那一族走这个算式**（分段控件，1.4 倍时 420）：固定的
// 300 在合法配置里放得下（最窄那一档是窗口 900 + 侧栏 480，内容区 364 >
// 300 + 24 + 一个字）。
func (s *Shell) settingsControlFitW(c *ui.Context, base float32) float32 {
	descMin := scale.Font(c, scale.Size14) // 说明列的最小宽：一个字
	if s.settings.descFloor > 0 {
		descMin = s.settings.descFloor
	}
	room := s.settings.contentW - space.Px24 - descMin
	if room < 0 {
		room = 0
	}
	return min(base, room)
}

// settingsSideWidth 是这一帧侧栏占多宽。
//
// ⚠️ **取两者的较大值**：设置里那个数（`sideTargetWidth`）是**目标**，而拖动时
// 画出来的是指针在哪儿（`s.sideWidth`）；侧栏还有 200ms 的过渡，动画期间实际
// 宽度在动。取大的一档 ⇒ **内容区算得偏小** ⇒ 说明列的下限早一点让位 ⇒
// 控件列不会被推出内容区。
func (s *Shell) settingsSideWidth(st app.Settings) float32 {
	w := sideTargetWidth(st)
	if s.sideWidth > w {
		w = s.sideWidth
	}
	return float32(w)
}

// settingsContentPad 是设置的内容那一段左右与下边的内边距。
//
// 28 不是 Biu 的数（那是 `px-8 py-4` = 32 / 16），是**内容区页边距**那个已经
// 批准的决定（比上游大 4px，调研判「很小」，见票 #124 的「不要动」那一节）。
// 设置页是内容区里的一页，跟着走同一个数才不会在页面之间跳。
//
// ⚠️ **上边距是 0**（票 #166 的 G40）：上游那一页没有 py，我们此前四周都留
// 28，于是设置页的内容比别的页、也比上游低一截。上边距单独收掉，左右和下边
// 仍然是那个数——它是这一栏的内边距，不是页面根的上边距。
const settingsContentPad = space.Px28

// settingsGroupGap 是设置分组之间的间距。DESIGN.md 的
// `spacing.settings_group_gap`：24（上游分节之间那个 `space-y-*` 换算下来的数）。
//
// 它同时用在两处：Tab 内容那一层（分组与分组之间），以及页头（标题、只读提示、
// 主 Tab、内容）那一层。两处此前分别是 18 和 16，各说各的。
const settingsGroupGap = space.SettingsGroupGap

// selectWidth 是设置页那两个下拉框多宽。
//
// ⚠️ **它就是控件列的基准宽度**（`settingsControlW`）：这一列里别的控件都是
// 300 宽，下拉要是还按上游那个 180（`system-settings.tsx:164`），这一列的左
// 边缘就又落在两个位置上了——而那正是票 #235 要消灭的那件事。
//
// ⚠️ 代理那一页的下拉是另一个数（`proxySelectWidth`，240）：那一页是**两列**
// 布局，不在这一列里。
const selectWidth = settingsControlW

// closeWindowOptions 是窗口关闭那两档，顺序就是从左到右的顺序。
//
// 值和 Biu 的 `closeWindowOption` 一字不差（`shared/settings/app-settings.ts:3`）。
//
// ⚠️「隐藏到托盘」这一档在 **macOS 上不是托盘**：那边不建托盘图标（与 Biu
// 一致），它是「收进程序坞，点 Dock 图标回来」。页面上把这件事说出来——一个
// 名字和实际行为对不上的选项，比没有更让人困惑。
var closeWindowOptions = []struct {
	value app.CloseWindowOption
	label string
}{
	{app.CloseWindowHide, "隐藏到托盘"},
	{app.CloseWindowExit, "直接退出"},
}

// settingsContentMaxW 是设置的内容收在多宽里（**1.0 倍字号时**）。
//
// 🔴 **900 是量出来的**：Biu 那一页是 `m-auto mb-6 max-w-[900px] px-8 py-4`
// （`biu/src/pages/settings/index.tsx:121`）。我们原来没有这一层：内容
// `FillWidth()` 之后一路铺到窗口右边缘，于是窗口一宽，整页设置就跟着拉宽——
// 右下角那个代理主机输入框能到 470 像素，而 Biu 那一档是两个 410 的格子。
//
// 「满宽拉到右边缘」在设置这一页上比在别的页上更糟，理由不是审美：那一行是
// 「左边标题 + 右边控件」，控件那一头钉在屏幕边上之后，眼睛从标题扫到控件要
// 横穿整块屏——而设置页是最需要逐行读的一页。
//
// ⚠️ 窄窗口上它**不会**顶到 900：`MaxWidth` 是上限不是定值，可视区多宽它就
// 多宽（最小窗口 900 - 侧栏 200 = 700，两边还各留 28 的内边距）。
//
// # 🔴 **780：两端对齐的那一段空档太长了**（票 #244 / UX 报告 §2 的 D1）
//
// 每一行是「左边标题 + 一句说明，右边一枚控件」，**两端对齐**——说明在左沿、
// 控件钉在右沿，中间空着。900 那一档下实测：说明那句在 x=277..475，控件在
// x=822..1123，**中间空 347 像素**（用户改一个设置，眼睛要在屏幕两端来回横跳
// 300~640 像素；而设置页是最需要逐行读的一页）。
//
// 收到 780 之后：内容那一段（去掉左右内边距）从 844 变成 724，说明列从 520
// 变成 400，那一段空档从 **347 降到 ~230**。
//
// ⚠️ **只改这一个数**：控件列的基准宽（300）、说明列的下限（320）、缝（24）
// 三样都别动——它们是票 #240 钉着的（`settingsDescMinW` / `settingsControlMinW`），
// 动了就把「控件列不会被推出内容区」那条验收弄坏了。
//
// ⚠️ **最窄那一档不受影响**：窗口 900 时可视区只有 700（900 - 侧栏 200），
// 那一档的内容段本来就是 700（`MaxWidth` 是上限），所以 780 和 900 在那一档
// 画出来**一模一样**——这一条收窄只作用在宽窗口上。
//
// ⚠️ **这一帧真正用的是 `settingsContentMaxWAt`**：它跟着系统字号缩放走
// （1.4 倍时上限 1092）——不跟着走的话，字号一大说明就折行，而说明折行会让
// **整行**变高、右边那列控件往下挪半格（那一段的来由见那个函数的注释）。
const settingsContentMaxW = 780

// viewSettings 是设置页本身：标题 + 只读提示 + 四个 Tab。
//
// ⚠️ 外面这一层是 `Scroll`，里面那一层 `Column` 必须 `FillWidth()`。
// MyGo 的 `Scroll` 是「一个带滚动标志的 Box」（`ui/scroll.go` 的 coreScroll），
// **不约束子元素的宽度**——子元素按内容算宽。右边那些固定宽度的控件会被整排顶到
// 窗口外面去，「深色」「直接退出」点不到，用户看见的是一片空白加半个按钮。
// `FillWidth()` 才是把里面那一格钉到可视区宽度上的那一句。
//
// ⚠️ **收在中间靠的是外面那一层 `Center` + 里面那一层 `MaxWidth`**，不是给
// 内边距加数：加内边距只是把内容往里推，右边缘仍然是窗口边缘，而设置页要收的
// 是**内容那一段的宽度**（照 Biu 的 `m-auto`）。
func (s *Shell) viewSettings(c *ui.Context, _ *ui.Route) {
	t := c.Theme()
	st := s.app.SettingsSnapshot()
	// 设置来自更新的版本时一个字都不许写回去。与其让用户按了没反应，不如把
	// 控件全关掉，并说清为什么——跟这一页自己下面那三档主题按钮一个道理
	// （`pickTheme`）。
	//
	// ⚠️ **这一句原来写的是「跟顶栏那三个按钮一个道理」**——顶栏从来没有过
	// 主题按钮组，主题那三档就在这一页自己这儿（`pickTheme`，`app/settings.go`
	// 那边也这么写）。它是在说「别让控件看起来还能按」这件事，而举的例子举错了。
	ro := s.app.SettingsReadOnly()

	// 说明列这一帧的下限（**有条件**：内容区放得下才给，见 `settingsDescFloor`）。
	s.settings.descFloor = s.settingsDescFloor(c, st)
	// 内容那一段的宽（控件列的宽度要钳在它里面）。
	s.settings.contentW = s.settingsContentInnerW(c, st)

	scroll := ui.Scroll(c).Key("settings-scroll").Grow(1).TrackScroll(&s.settings.scroll)
	// 记下这个滚动容器的盒子：把指着的那一行滚进视野时要拿它当「视野」（见
	// settingsScrollFocusIntoView）。
	s.settings.scrollEl = scroll
	scroll.Children(func() {
		// 这一层是 `m-auto`：Column 的横向对齐（跨轴），所以内容那一段在
		// 可视区里居中，而可视区不满 900 时它就是满的（`MaxWidth` 是上限）。
		ui.Column(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(s.settingsContentMaxWAt(c)).
				Padding(space.None, settingsContentPad, settingsContentPad, settingsContentPad).
				Gap(settingsGroupGap).Children(func() {
				ui.Text(c, "设置").FontSize(scale.Font(c, scale.PageTitle))

				if ro {
					ui.Text(c, "设置只读：这份设置来自更新的版本，这一版一个字都不会写回去。").
						FontSize(scale.Font(c, scale.Size12)).TextColor(dangerText(t))
				}

				// ⚠️ **主 Tab 用轨道 + 滑块那一种**（`mainTabs`，见 tabs.go）：
				// 这一排和次级排序那几排不是同一个样子（票 #138）。
				//
				// ⚠️ `AlignSelf(ui.Start)`：Column 的默认跨轴对齐是 Stretch，
				// 不拦一下轨道会横贯整个内容区；上游那一条是包着字的
				//（`inline-flex`）。
				mainTabs(c, &s.settings.tab, tabMD, settingsTabNames...).AlignSelf(ui.Start)

				ui.Column(c).FillWidth().Gap(settingsGroupGap).Children(func() {
					switch s.settings.tab {
					case settingsTabGeneral:
						s.settingsGeneral(c, st, ro)
					case settingsTabMenu:
						s.settingsMenu(c, st, ro)
					case settingsTabShortcut:
						s.settingsShortcuts(c)
					case settingsTabProxy:
						s.settingsProxy(c, st, ro)
					}
				})
			})
		})
	})
	s.trackScrollbar(scroll)

	// 把「指着的那一行」滚进视野。⚠️ **排在内容建完之后**：它量的是上一帧的
	// 盒子，而那个盒子属于这一帧建出来的那个元素（见 settingsScrollFocusIntoView）。
	s.settingsScrollFocusIntoView()

	// 「回到顶部」那一枚。滚过 400 像素才出现（上游的数，见 `widgets/list` 的
	// `BackToTop`）。
	//
	// ⚠️ **这一页滚的是整页**（外面那个 `ui.Scroll`），不是某一栏——所以位置就是
	// 那一个 `ScrollState`。
	list.BackToTop(c, s.settings.scroll.Y, func() { s.settings.scroll.Y = 0 })
}

// syncSettings 是设置页每帧的副作用：切走就停录、换 Tab 就回到顶上。
//
// ⚠️ **它们原来在 `viewSettings` 的最开头**，而设置页在满屏开着的时候不画
// （票 #161）——不挪的话，用户在满屏里看频谱的那几分钟里，「正在录快捷键」那一
// 格还记着上一次录到一半的东西。
//
// ⚠️ **读的是上一帧的 `s.settings.tab`**：那一格是 `mainTabs` 在画的时候写的，
// 而这一句原来也在 `mainTabs` 之前——所以挪过来之后读到的还是同一个值，行为
// 一模一样。
func (s *Shell) syncSettings(c *ui.Context) {
	// 切走就停录：`FocusBind` 的「焦点在哪一格」在控件没建出来时会一直等下去，
	// 切回来时焦点又回到那一格，用户在还没准备好的时候敲的第一下键就被录进去了。
	if s.settings.tab != settingsTabShortcut {
		s.recording = shortcutSlot{}
		s.rejected = shortcutSlot{}
	} else {
		// ⚠️ **「这一帧画没画快捷键录制格」这个标志要在这儿也置**：满屏开着的时候
		// 设置页不画，而 `viewShortcuts` 的兜底是「这一帧没画录制格就把正在录的
		// 清掉」——不置的话，用户正在录的那一个快捷键会在满屏开着的时候被静默
		// 取消（而录制期间窗口一个应用内组合键都不登记，用户看不见为什么）。
		//
		// 它原来在 `settingsShortcuts`（画的那一半）里，而那一半在满屏开着的时候
		// 不跑。顺序对得上：`viewShortcuts` 读的是**上一帧**的值，而这一句和画的那
		// 一句落在同一帧里。
		s.recorderShown = true
	}

	// 按 Tab 时回到顶上：Biu 换了面板就是新的一页。
	if last, tab := s.settings.shownTab, s.settings.tab; last != tab {
		s.settings.shownTab, s.settings.scroll.Y = tab, 0
		// ⚠️ **指着的那一项只在它所在的那一格**：换到别的 Tab 就没东西可指了，
		// 而留着它会让下一次跳过来以为已经指过（settingsScrollFocusIntoView
		// 直接跳过）。
		if s.settings.focus != settingsFocusNone && tab != settingsTabGeneral {
			s.settings.focus = settingsFocusNone
		}
	}
}

// settingsFocusMargin 是「指着的那一行」滚进视野之后离边留多少。
//
// 16 是「贴着边的那一行看起来像是被切掉了」和「别把无关的东西滚走太多」之间
// 的那个数。
const settingsFocusMargin = 16

// settingsScrollFocusIntoView 把「指着的那一行」滚进视野。
//
// ⚠️ **它必须在这一页的内容建完之后调**：它量的是**上一帧**的盒子
// （`Element.Bounds()` 的规矩），而那个盒子属于**这一帧建出来的那个元素**
// ——跨帧去读上一帧的元素是**用旧把手**（框架会当它过期，详见
// `mygo/ui/frame.go` 的 `expired`）。
//
// ⚠️ **它要跨两帧**：刚跳过来那一帧，上一帧画的是**别的页**，那一行压根没有
// 盒子。所以第一帧量不到，要**再要一帧**——不然就没有下一帧，那一行就永远停在
// 视野外，而用户看到的是「设置页打开了，可这里没有下载目录」。
//
// ⚠️ **最多再要三帧**：万一那一行一直没量到（它不在这一格、或者这一页被换掉
// 了），别把它变成一个每帧都要一帧的死循环。
func (s *Shell) settingsScrollFocusIntoView() {
	st := &s.settings
	if st.focus == settingsFocusNone || st.focusScrolled {
		return
	}
	row, view := st.focusEl, st.scrollEl
	// ⚠️ `Valid()` 读的是「这个把手属于这一帧吗」：不属于就是**上一帧**留下的
	// 那个（这一帧没画那一行），那个盒子不能用来算。
	if !row.Valid() || !view.Valid() || row.Bounds().H <= 0 || view.Bounds().H <= 0 {
		st.focusTries++
		if st.focusTries > settingsFocusTries {
			// 量不到了：放弃，别再要帧（死循环比滚不到更糟）。
			st.focusScrolled = true
			return
		}
		s.wake.invalidate()
		return
	}
	rb, vb := row.Bounds(), view.Bounds()
	// 差多少：上面露出去就往上滚，下面露出去就往下滚。
	need := float32(0)
	if rb.Y < vb.Y {
		need = rb.Y - vb.Y
	} else if rb.Y+rb.H > vb.Y+vb.H {
		need = rb.Y + rb.H - (vb.Y + vb.H)
	}
	if need > 0 {
		st.scroll.Y += need + settingsFocusMargin
	} else if need < 0 {
		st.scroll.Y += need - settingsFocusMargin
	}
	st.focusScrolled = true
}

// settingsFocusTries 是「量不到就再要一帧」的次数上限。
const settingsFocusTries = 3

// settingSection 是一组设置上面的那个标题。Biu 用的是 `<h2>`（system-settings.tsx
// 的「外观」「播放」「下载」…），`text-xl` 也就是 **20px**，字重同样被 Preflight
// 重置为 inherit——**不加粗**。
//
// ⚠️ 这一句原来写着「Biu 用的是 `<h2>`」就完了，既没写字号也没写字重，于是这一
// 组标题在我们这边一路是 16/700：比上游小一档、还粗两档（票 #166 的 G06）。
func (s *Shell) settingSection(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(scale.Font(c, scale.Size20)).Padding(space.None, space.None, space.Px4, space.None)
}

// settingSectionEnd 是两组设置之间那条分隔线。Biu 的 `<Divider />`。
func (s *Shell) settingSectionEnd(c *ui.Context) {
	ui.Divider(c).FillWidth()
}

// settingRow 是设置项的一行：左边标题加一句说明，右边控件。
//
// 排版照 Biu 那一行（`flex items-center justify-between`，左边 `mr-6 space-y-1`
// 的标题和说明，右边固定宽度的控件）。两边的伸缩比例照抄：文字吃掉剩下的
// 宽度，说明换行了控件也不会被挤走。
func (s *Shell) settingRow(c *ui.Context, title, desc string, control func()) {
	s.settingRowAt(c, "", title, desc, control)
}

// settingRowAt 是 settingRow 加上「这一行的身份」。
//
// 身份是给「从别处跳过来要指着一行」用的（见 settingsFocus）：**被指着的
// 那一行，标题是主色**——它和旁边那些行一眼就分得开，而那一行多半在屏幕
// 下面，滚过去之后用户第一眼看见的就是它。
//
// 空身份（`settingRow` 走的那条）就还是普通的一行。
func (s *Shell) settingRowAt(c *ui.Context, key, title, desc string, control func()) {
	t := c.Theme()
	focused := key != "" && s.settings.focus == settingsFocus(key)
	s.settingRowElAt(c, focused, func() {
		txt := ui.Text(c, title).FontSize(scale.Font(c, scale.Size16)).FontWeight(weightMedium)
		if focused {
			// ⚠️ **主色当字色取 `ink.AccentOn`**：浅色下主色本身压在白底上读
			// 不出来（flatbutton.go 那一大段），而「指着的那一行」恰恰是用户
			// 要找的那一行——它必须一眼看得见。
			txt.TextColor(ink.AccentOn(t))
		}
	}, desc, control)
}

// settingRowEl 是 settingRow 那一行，标题那一格换成自己画的。
//
// 什么时候要它：标题**不是一段字**的时候——「关于应用」那一行要的是「当前版本
// 0.1.0 → 1.2.0」这样一个两段、还带颜色的东西（照 Biu 的
// `system-settings.tsx` 那一段）。硬塞进 `settingRow` 的字符串参数里就只剩一种
// 颜色，而那一段新版本号用主色正是「有更新」这件事唯一的视觉标记。
//
// 其余的行一律用 `settingRow`：那一个签名足够的地方不要多一个。
func (s *Shell) settingRowEl(c *ui.Context, title func(), desc string, control func()) {
	s.settingRowElAt(c, false, title, desc, control)
}

// settingRowElAt 是 settingRowEl 加上「这一行正被指着」。
//
// ⚠️ **被指着的时候这一行的盒子要留下来**（`focusEl`）：滚进视野要量它
// （见 settingsScrollFocusIntoView）。
func (s *Shell) settingRowElAt(c *ui.Context, focused bool, title func(), desc string, control func()) {
	t := c.Theme()
	row := ui.Row(c).FillWidth().Gap(space.Px24).AlignItems(ui.Center)
	if focused {
		s.settings.focusEl = row
	}
	row.Children(func() {
		// ⚠️ **说明列有最小宽度**（`settingsDescMinW`）：控件那一列是定宽的，
		// 说明这一列吃剩下的——没有这个下限，窗口一窄说明就被压扁（理由见那个
		// 常量的注释）。
		//
		// ⚠️ **下限是有条件的**（`s.settings.descFloor`，每帧算一次）：内容区
		// 放不下它的时候**不给**——那一档宁可说明列窄一点，也不能把右边那列
		// 控件顶出窗口（`settingsDescFloor` 的注释写着整段来由）。
		//
		// 🔴 **下限是 0 的时候不许链 `MinWidth(0)`**：框架里 `minW` 一旦存在就
		// 接管这一项的**自动最小宽**（`mygo/ui/layout.go` 的 `flexLayout`：
		// 有 `minW` 就用它，没有才用内容的 min-content 宽）。于是
		// `MinWidth(0)` 会把说明列的最小宽变成 **0**——中文可以逐字换行，
		// 内容的 min-content 是一个字宽（19.6），给了 0 就连那一个字都不剩，
		// 整行塌成 0 高，**「主题」那一行在画面上整个消失**。
		descCol := ui.Column(c).Grow(1)
		if s.settings.descFloor > 0 {
			descCol = descCol.MinWidth(s.settings.descFloor)
		}
		descCol.Children(func() {
			title()
			ui.Text(c, desc).FontSize(scale.Font(c, scale.Size14)).TextColor(t.TextMuted).Wrap()
		})
		control()
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 共用的控件
// ─────────────────────────────────────────────────────────────────────────────

// settingsSegment 是设置页的分段控件：一排互斥的档位，选中的那一档铺实心主色底。
//
// 🔴 **设置页只有这一种分段控件**（票 #240 / UI 报告 §4 的 F-17）。
//
// 此前同一页上有**两套**分段控件：框架的 `ui.Segmented`（**灰底轨道 + 白色滑块**，
// 「数据显示样式」那一格）和自绘的三个按钮（**没有轨道，选中的那一档是实心主色底**，
// 「主题」「窗口关闭」那两格）。两个并列的分段控件长得不一样，用户一眼看不出它们
// 是同一类东西（Apple HIG「一致性：相同功能的控件外观相同」）。现在三处都走这里。
//
// # 为什么统一到「实心主色底」这一档，而不是换成框架的 `ui.Segmented`
//
//   - **「被选中的选项 = 实心主色底 + `t.AccentText` 字」是已经拍板的**
//     （票 #239 / 审计 F-14，DESIGN.md §四）。那一节还明写「**F-17 没做之前，别把
//     这一档当成定案**」——本票就是 F-17，本票来定案，而定案的方向是**沿用已定的
//     那一档**，不是再添第三种。
//   - **框架 `ui.Segmented` 没有滑块动画**：它的每一段各自换背景
//     （`mygo@v0.6.2/ui/toggle.go` 的 `coreSegmented`），没有动画通道。有 250ms
//     滑动动画的是**我们自己的 `mainTabs`**（票 #171），这一页从来没用过它。
//   - **它填不满控件列那一格**：`ui.Segmented` 的宽度按内容算（`Shrink(0)`），
//     换成它，那一列的左边缘就又落在两个位置上——那正是票 #235 刚消灭掉的那件事。
//   - **「数据显示样式」那一格下面就是预览图**，选中的那张**描主色边**
//     （`previewOne`）；换成白色滑块，同一格里就有两种「选中」信号。
//
// # 宽度跟着字号缩放走
//
// 「跟随系统」四个汉字最宽：1.4 倍时它连内边距要 106 像素，三个各 100 的格子
// 装不下，选中项会被裁成「跟随系…」（评审在 1.4 倍下量到过）。所以这一族的宽度
// 是 `scale.Font(c, settingsControlW)`，不是 `settingsControlW`。
//
// # 只读时
//
// 选中那一档换成中性灰（`pickSelected`），字换成禁用灰（`flat.Dimmed`）——
// 实心主色底乘完框架的 50% 不透明度之后字压在上面只剩 2.6:1，只读时用户会以为
// 「这几个选项消失了」（`pickSelected` 的注释写着整段来由）。
//
// # w 是这一排多宽
//
// 0 = **填满父那一格**：「数据显示样式」那一格外面还套着一列预览（切换在上、
// 预览在下），两样东西必须同宽，而那一格收窄时预览要跟着收窄。别的两处给
// 控件列的基准宽。
func (s *Shell) settingsSegment(c *ui.Context, key, group string, w float32, labels []string, chosen int, ro bool, pick func(i int)) {
	// ⚠️ **那个 `Label(group)` 是给这一排起个组名**：读屏软件要知道这几个键是一组
	// （互斥的几档），而 `Tester.Find` 也靠它量这一排的盒子——控件列的左边是不是
	// 一条线，就靠它断（见 `TestTheSettingsControlsShareOneBaseline`）。
	t := c.Theme()
	row := ui.Row(c).Key(key).Label(group).Gap(space.Px4)
	if w > 0 {
		// ⚠️ **按内容区算宽度**（`settingsControlFitW`）：这一族的宽度跟着字号
		// 缩放走（1.4 倍时 420），而内容区最窄的那一档（窗口 900 + 侧栏拉到
		// 480）只有 364——不按内容区算，右边那一截会被窗口切掉。
		row = row.Width(s.settingsControlFitW(c, w))
	} else {
		row = row.FillWidth()
	}
	// ⚠️ **下限**：行放不下说明列的最小宽度时，这一列先收窄（框架的 flex 会先
	// 满足说明列那个 `MinWidth`）——但不低于 `settingsControlMinW`，不然「跟随
	// 系统」那一档会被裁成半个词（那个常量的注释写着整段来由）。
	row = row.MinWidth(settingsControlMinW)
	row.Children(func() {
		for i, text := range labels {
			b := flatButton(c, text).Grow(1).Radius(t.Radius).Disabled(ro)
			if i == chosen {
				b = s.pickSelected(c, b, ro)
			}
			// ⚠️ 排在 `.Disabled(ro)` 和上面那一层 **之后**：禁用前那一档的字乘完
			// 框架的 50% 只剩 2.6:1——只读时用户会以为这几个选项消失了。
			// `flat.Dimmed` 里的注释写了整个来由。
			b = flat.Dimmed(c, b)
			if b.Clicked() {
				pick(i)
			}
		}
	})
}

// pickTheme 是主题那三档。
//
// 换完之后 `applyFrameTheme` 下一帧就装上新主题：改档位 → UpdateSettings →
// invalidate → 重画，一帧之内的事。走 `applySetting` 而不是 `SetThemeMode`，
// 是为了和另外十几项共用同一条报错提示。
//
// ⚠️ **它和另外两处分段控件走同一个出口**（`settingsSegment`）：设置页只有一种
// 分段控件（票 #240 / 报告 F-17）。选中的那一档是实心主色底（`flatSolid`）——
// 这是「被选中的选项」在应用里**唯一**的画法（票 #239，审计 F-14）：下拉项的
// 高亮也是它（`settings_enum.go` / `settings_font.go` / `collectionpage.go` 三处都走
// 同一个出口）。
//
// 上游 Biu 这一排是 HeroUI 的 `RadioGroup`，而 HeroUI 的 `Radio` 选中项就是
// `bg-primary` 实心底 + `text-primary-foreground`——所以这一档是照上游的。
func (s *Shell) pickTheme(c *ui.Context, st app.Settings, ro bool) {
	chosen := 0
	labels := make([]string, len(themeModes))
	for i, m := range themeModes {
		labels[i] = m.label
		if m.mode == st.ThemeMode {
			chosen = i
		}
	}
	s.settingsSegment(c, "setting:themeMode", "主题模式", scale.Font(c, settingsControlW),
		labels, chosen, ro, func(i int) {
			mode := themeModes[i].mode
			s.applySetting("主题", func(cur *app.Settings) { cur.ThemeMode = mode })
		})
}

// pickSelected 是「被选中的选项」那一档：**实心主色底 + `t.AccentText` 字**。
//
// ⚠️ **只读（`ro`）时换成中性灰那一档**（`flatCurrent`）：框架对禁用元素是**整个
// 乘 50% 不透明度**（`mygo/ui/paint.go:209-212`），于是实心主色底也跟着淡——
// 底变成「主色 × 0.5 叠页面底色」，字换成 `ink.Disabled` 之后压在上面只有
// **2.6:1（深色）/ 2.9:1（浅色）**，掉到 3:1 以下（那一档是非正文要的门槛）。
//
// 中性灰那一档乘完 50% 之后是 **4.43:1（深色）/ 3.28:1（浅色）** —— 读得出来，
// 而**只读的时候这一档本来就不该那么响**。
//
// ⚠️ **画面上那一格是「灰底 + 灰字」，一点绿都没有**：字是 `flat.Dimmed` 换的
// （`ink.Disabled`），而它排在 `flatCurrent` **之后**——所以主色那一档的字在只读
// 时被盖掉了。实测：浅色底 #ededef / 字 #828285，深色底 #2b2b2e / 字 #909092。
//
// ⚠️ **别照着「只读时是灰底 + 主色的字」去写文档**（评审抓出来过一条）：那一档
// 的**底**是中性灰，**字**是禁用灰——主色在只读时**根本不出现**。
//
// ⚠️ **这一档是设置页三处分段控件共用的**（`settingsSegment`）：设置页只有一种
// 分段控件（票 #240 / 报告 §4 的 F-17）。
func (s *Shell) pickSelected(c *ui.Context, b ui.Element, ro bool) ui.Element {
	if ro {
		return flatCurrent(c, b)
	}
	return flatSolid(c, b)
}

// pickCloseWindow 是窗口关闭那两档。照 Biu 用的是横排单选（RadioGroup
// orientation="horizontal"），我们用同一排按钮，选中的那个上主色。
//
// ⚠️ **它和「主题」「数据显示样式」那两处走同一个出口**（`settingsSegment`）：
// 设置页只有一种分段控件（票 #240 / 报告 F-17）。
func (s *Shell) pickCloseWindow(c *ui.Context, st app.Settings, ro bool) {
	chosen := 0
	labels := make([]string, len(closeWindowOptions))
	for i, opt := range closeWindowOptions {
		labels[i] = opt.label
		if opt.value == st.CloseWindowOption {
			chosen = i
		}
	}
	s.settingsSegment(c, "setting:closeWindowOption", "窗口关闭方式", scale.Font(c, settingsControlW),
		labels, chosen, ro, func(i int) {
			v := closeWindowOptions[i].value
			s.applySetting("窗口关闭", func(cur *app.Settings) { cur.CloseWindowOption = v })
		})
}

// pickAutoStart 是开机自启那个开关。
//
// 它和别的那几项不一样：**不是改一个键就完了**，得真的去系统里注册/注销登录项
// （macOS 是 SMAppService / LaunchAgent，Windows 是注册表 Run 项）。所以走的是
// `Shell.SetAutoStart`——先动系统、成功了才写设置，顺序反了会留下
// 「设置说开、系统说关」，而启动时的对齐只补这一种（见 lifecycle.go 的注释）。
//
// 动不了的时候（没打包成 bundle、用户没给权限）弹一条提示，**设置一个字都不改**：
// 界面上亮着一个实际上没开的开关，比暗着更坏——用户会以为明天开机它自己就起来了。
func (s *Shell) pickAutoStart(c *ui.Context, st app.Settings, ro bool) {
	on := st.AutoStart
	// ⚠️ 这里**不能** .Key()。Switch 的旋钮是 `Animate("knob", …)`，动画状态记在
	// 控件节点上；给它一个 Key 等于每帧换一个新节点，动画每帧从头开始——于是这一页
	// 永远停不下来（实测：加了 Key 之后 `settleSettingsPage` 跑满 60 帧还在滑，
	// 去掉就好了）。上面那两排按钮能加 Key，是因为 Row 本身没有动画。
	// ⚠️ 那个 `Label` 不能省，原因见 `pickSwitch`：开关自己一个字都没有。
	//
	// 用 `staticSwitch` 而不是 `ui.Switch`：Biu 那一页每一个开关都写了
	// `disableAnimation`，而 `ui.Switch` 的 140ms 旋钮动画没有关掉的开关——见
	// static_switch.go，那一节顺带说明了它为什么还很贵。
	sw := staticSwitch(c, &on).Label("开关：开机自启").Disabled(ro)
	if !sw.Changed() {
		return
	}
	if err := s.SetAutoStart(on); err != nil {
		s.addToast("开机自启没设上："+strings.TrimPrefix(err.Error(), "app: "), notice.Danger)
		// 下一帧 `on` 会从设置里重新读（没写成功，所以还是旧值），开关自己弹回去。
	}
}

// pickTrayNote 是 macOS 上「没有托盘」那句话。只在该说的话的时候摆。
func (s *Shell) pickTrayNote(c *ui.Context) {
	if platform.TraySupported() {
		return
	}
	// macOS 上没有托盘（与 Biu 一致），「隐藏到托盘」这一格在这儿是「收进 Dock」。
	// 得说出来，不然用户以为 Biu 有而我们没做。
	ui.Text(c, "macOS 上不建托盘图标（与 Biu 一致）：「隐藏到托盘」是收进程序坞，"+
		"图标还在，点一下就回来。").FontSize(scale.Font(c, scale.Size12)).TextColor(c.Theme().TextMuted)
}

// ─────────────────────────────────────────────────────────────────────────────

// applySetting 改一项设置；写不出去就弹一条提示。
//
// **提示不能省**：静默地丢掉是这一页最坏的失败方式——用户以为改了，下次打开
// 又变回去，中间屏幕上什么也没发生过。要报的不是「按了没反应」，是「存不住」：
// 落盘失败时档位在内存里是改成功了的（这一点是对的，别回滚），丢的是磁盘上
// 那一份，用户下次启动看到的才是旧值。
func (s *Shell) applySetting(what string, fn func(*app.Settings)) {
	if _, err := s.app.UpdateSettings(fn); err != nil {
		s.addToast(what+"没存上："+strings.TrimPrefix(err.Error(), "app: "), notice.Danger)
	}
}
