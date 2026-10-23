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

// 控件的宽度。Biu 那一页右边固定 360px（`w-[360px]`），三档切换占 300。
//
// 300 是量出来的，不是挑的：主题那三个按钮里「跟随系统」四个汉字最宽，按钮
// 自己还要留内边距。窄了它会被画成「跟随…」——用户看见半个词，第一反应是
// 这个应用坏了，而不是「这个按钮窄了一点」。
const (
	segWidth       = 300
	radiusWidth    = 360
	radiusReadoutW = 48
)

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

// selectWidth 是下拉框多宽。Biu 是 `w-[180px]`（`system-settings.tsx:164`），
// 代理那一页是 `w-[240px]`（`proxy-settings.tsx:43`）。
const selectWidth = 180

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

// settingsContentMaxW 是设置的内容收在多宽里。
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
const settingsContentMaxW = 900

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

	scroll := ui.Scroll(c).Key("settings-scroll").Grow(1).TrackScroll(&s.settings.scroll)
	// 记下这个滚动容器的盒子：把指着的那一行滚进视野时要拿它当「视野」（见
	// settingsScrollFocusIntoView）。
	s.settings.scrollEl = scroll
	scroll.Children(func() {
		// 这一层是 `m-auto`：Column 的横向对齐（跨轴），所以内容那一段在
		// 可视区里居中，而可视区不满 900 时它就是满的（`MaxWidth` 是上限）。
		ui.Column(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(settingsContentMaxW).
				Padding(space.None, settingsContentPad, settingsContentPad, settingsContentPad).
				Gap(settingsGroupGap).Children(func() {
				ui.Text(c, "设置").FontSize(scale.Font(c, pageTitleSize))

				if ro {
					ui.Text(c, "设置只读：这份设置来自更新的版本，这一版一个字都不会写回去。").
						FontSize(scale.Font(c, 12)).TextColor(t.Danger)
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
	ui.Text(c, title).FontSize(scale.Font(c, 20)).Padding(space.None, space.None, space.Px4, space.None)
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
		txt := ui.Text(c, title).FontSize(scale.Font(c, 16)).FontWeight(weightMedium)
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
		ui.Column(c).Grow(1).Children(func() {
			title()
			ui.Text(c, desc).FontSize(scale.Font(c, 14)).TextColor(t.TextMuted).Wrap()
		})
		control()
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 共用的控件
// ─────────────────────────────────────────────────────────────────────────────

// pickTheme 是主题那三档。
//
// 换完之后 `applyFrameTheme` 下一帧就装上新主题：改档位 → UpdateSettings →
// invalidate → 重画，一帧之内的事。走 `applySetting` 而不是 `SetThemeMode`，
// 是为了和另外十几项共用同一条报错提示。
func (s *Shell) pickTheme(c *ui.Context, st app.Settings, ro bool) {
	// ⚠️ 宽度跟着字号缩放走：1.4 倍时四个字要 106 像素，三个各 100 的格子
	// 装不下，选中项会被裁成「跟随系…」（评审在 1.4 倍下量到过）。
	ui.Row(c).Key("setting:themeMode").Width(scale.Font(c, segWidth)).Gap(space.Px4).Children(func() {
		for _, opt := range themeModes {
			b := flatButton(c, opt.label).Grow(1).Radius(6).Disabled(ro)
			if opt.mode == st.ThemeMode {
				b = flatCurrent(c, b)
			}
			// ⚠️ 排在 `.Disabled(ro)` 和 `flatCurrent` **之后**：禁用前那一档的
			// 字是主色（深色下），乘完框架的 50% 只有 2.6:1——只读时用户会以为
			// 这三个选项消失了。`flat.Dimmed` 里的注释写了整个来由。
			b = flat.Dimmed(c, b)
			if b.Clicked() {
				s.applySetting("主题", func(cur *app.Settings) { cur.ThemeMode = opt.mode })
			}
		}
	})
}

// pickCloseWindow 是窗口关闭那两档。照 Biu 用的是横排单选（RadioGroup
// orientation="horizontal"），我们用同一排按钮，选中的那个上主色。
func (s *Shell) pickCloseWindow(c *ui.Context, st app.Settings, ro bool) {
	ui.Row(c).Key("setting:closeWindowOption").Width(scale.Font(c, segWidth)).Gap(space.Px4).Children(func() {
		for _, opt := range closeWindowOptions {
			b := flatButton(c, opt.label).Grow(1).Radius(6).Disabled(ro)
			if opt.value == st.CloseWindowOption {
				b = flatCurrent(c, b)
			}
			b = flat.Dimmed(c, b)
			if b.Clicked() {
				s.applySetting("窗口关闭", func(cur *app.Settings) { cur.CloseWindowOption = opt.value })
			}
		}
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
		"图标还在，点一下就回来。").FontSize(scale.Font(c, 12)).TextColor(c.Theme().TextMuted)
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
