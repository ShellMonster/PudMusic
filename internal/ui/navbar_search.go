// 导航栏上的搜索框 + 搜索建议浮层：Biu 的 N-03 / N-04
// （`biu/src/layout/navbar/search/index.tsx`）。
//
// ## 为什么不直接用 `ui.SearchField`
//
// `ui.SearchField` 正好就是 Biu 那个框（放大镜 + 可清空 + Esc 清空，
// `ui/combobox.go:409-450`），差的是最后那条：**它的 Esc 被自己吃掉了**。
// 它的清空按钮在 `afterInput` 里判 `in.Shortcut(0, KeyEscape)`
// （`combobox.go:437`）——输入框是焦点元素，它先处理，浮层那份
// `OverlayShortcut` 就再也等不到这次 Esc。
//
// 实测（无头 Tester，按一次**带字**的 Esc）：
//
//	SearchField   → 输入框看到 Esc 吗：否   浮层关了吗：没有（字被输入框清了，浮层还开着）
//	TextInputBase → 输入框看到 Esc 吗：是   浮层关了吗：关（字还在）
//
// 三种收起时机里有一种是验收项，所以这一处用 `TextInputBase` 自己拼：放大镜
// 和清空按钮照 Biu 画，Esc 交给浮层。
//
// ## 浮层为什么是 PopoverBase 而不是 Popover
//
// 两处都要自己动手：
//
//   - `KeepFocus()`：**必须有**。少了它，点浮层里的一条历史会把焦点抢到那个
//     按钮上（实测），于是「焦点不在搜索框 → 收起」这条判断在按下那一帧就成立，
//     浮层在 Children 之前就没了，那一条根本点不到。KeepFocus 让焦点留在输入框。
//   - 自己画板子的样子：Popover 用主题那套（`ui/widgets.go:611`），我们要的是
//     Biu 的 `bg-content2 rounded-medium shadow-2xl`，而 PopoverBase 正好留了这道口子。
//
// ## 浮层里有两档
//
// 这一档面板里摆的是「**搜索历史**」（本地，Biu 的 `topContent`）加上「**联想词**」
// （远端，Biu 的 `getSearchSuggestMain`）——**两档在同一个面板里**，不是一个浮层
// 套另一个浮层（理由见 `navbar_suggest.go` 的文件头）。远端那一档的防抖、请求、
// 画法都在那一边的文件里。
//
// ## 面板的进场（票 #259）
//
// 联想面板**第一次出现**的那一下有一小段过渡：**淡入 + 从下面升上来 10 DIP**，
// **200ms** + `motion.Unified`（和队列抽屉、侧栏折叠同一档）。
//
// **为什么这一处有，而别的浮层没有**：DESIGN.md §五《不做》写着「菜单 / 小浮层 /
// 确认框 / Toast 的进出场不加——它们是高频操作，加了变噪音」。**那一条对这一处
// 不成立**：联想面板**不是高频操作**——它**只在用户输入时出现**，而且**用户此刻
// 正盯着它**（他刚敲了几个字，正在等联想词）。它不是「一秒好几次」的那种浮层。
// DESIGN.md §五《不做》那一条的例外就写在这一处（2026-10-25 拍板）。
//
// **不抄 QQ音乐那一套**：它的顶栏联想面板是 `max-height` 展开、**600ms**
// ease-out。抄不了——我们的面板高度是**算出来的**（内容多高就多高，
// `suggestPanelMaxH` 只是上限），不是从 0 展开的，而 `max-height` 那一套要求
// 高度从 0 起算。我们做的是**整块面板进场**：淡入 + 轻微上移。
// **时长也不抄那 600ms**：那是 Web 版在慢网络下的补偿，本地是 200ms 那一档。
//
// **只做进场，退场保持瞬消**：框架的 `PopoverBase` 在 `open == false` 时**根本
// 不 build 那个 panel** → 面板直接消失。要做退场就得**自己接管开关状态**
// （自己多管一个「正在退场」的状态 + 延时把 `open` 设成 false），**代价大于
// 收益**；而进场那一下才是用户能感知的「它长出来了」。
//
// ⚠️ **开着的时候内容变了不重播**（联想词返回来那一趟）：**这一场里**只在面板
// **第一次出现**的那一帧播——判据是 `Shell.suggestShown`，而它**只在浮层关掉的
// 时候**复位（见 `viewSuggest` 那里的说明：面板中间空掉一帧**不算**重新开）。
// 这一条挡的是「每一次按键都重播一遍进场」。
package ui

import (
	"net/url"
	"strings"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/motion"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

const (
	// searchWidth 是搜索框多宽。Biu 是 `w-[280px]`（`search/index.tsx:73`）。
	// 设计规范 v0.1 §7.4：宽 240、高 28、胶囊形（原 Biu 是 280 / 40 / 主题圆角）。
	searchWidth = 240

	// suggestWidth 是建议浮层多宽。Biu 是 `w-[360px]`（同文件 :101）。
	//
	// 比搜索框宽是 Biu 的原样：里面那几行是「搜索历史」的标题加一排 chip，
	// 按输入框那么宽排不下。
	suggestWidth = 360

	// searchLabel 是搜索框的无障碍名字，也是测试认得它的凭据——无头 Tester 的
	// `Find` 找的是「显示着这个字或者标着这个名字」的元素。
	searchLabel = "搜索"

	// searchActionLabel 是那枚放大镜的无障碍名字。
	//
	// 🔴 **它不能也叫「搜索」**（票 BMG-253）：输入框自己就带那个名字，而
	// `Tester.Find` 认的是**逐字相等**——两个同名的话它会认到**先画的那一个**
	// （放大镜画在输入框前面），于是「点一下搜索框」那一下就点到了放大镜上
	// （而放大镜是「拿框里的词去搜」）。
	searchActionLabel = "开始搜索"

	// searchHeight 是搜索框多高。Biu 那个 Input 是 md 档（`h-10`，
	// `search/index.tsx:72-96`；Tailwind 1 单位 = 4px）——40，顶栏 64 高，
	// 上下各留 12。
	searchHeight = 28

	// searchRadius 是搜索框的圆角：胶囊（高度的一半）。刻意不跟主题圆角走——胶囊是这个框的形状，
	// 用户把全局圆角拖到 0 它也还是胶囊。
	searchRadius = searchHeight / 2

	// searchPadX / searchGap 是搜索框里那三样之间的留白：左右内边距、放大镜与
	// 输入框之间的缝。上游 Input 的 md 档是 `px-3 gap-3`（12 / 12）——
	// 12 + 18 的放大镜 + 12，输入框的字从框左边 42 起（上游的放大镜是 16，
	// 我们这几枚图标统一 18，见 icons.go 的 iconMd）。
	searchPadX = space.Px12
	searchGap  = space.Px12

	// 搜索框的底色：上游 `bg-default-400/20 dark:bg-default-500/20`
	// （`search/index.tsx:97`），hover / focus 各抬到 30%。default-400 / 500
	// 就是 HeroUI 那套灰里的 `#a1a1aa` / `#71717a`（报告 03 逐字对过）。
	//
	// ⚠️ **深浅两套不是同一个灰**：浅色下用 400，深色下用 500——同一个 400 压在
	// `#18181b` 上比底色亮一大截，框会像一块浮起来的板。
	searchGrayLightR, searchGrayLightG, searchGrayLightB         = 0xa1, 0xa1, 0xaa
	searchGrayDarkR, searchGrayDarkG, searchGrayDarkB            = 0x71, 0x71, 0x7a
	searchBgIdle                                         float32 = 0.20
	searchBgHot                                          float32 = 0.30

	// 那个叉：图标边长和点击区。Biu 的 `isClearable` 是一个 16 的叉，
	// 这里的叉小一点（12）——40 高的框里放 16 的叉也不挤，但 12 和旁边的
	// 放大镜（18）同一档，两个图形不会一大一小。
	iconCloseGlyph = 12
	iconCloseHit   = 26
)

// searchFieldBg 是搜索框这一刻的底色：静止 20%，指针压在上面或者有焦点时 30%。
//
// 判据是**底色**，不是描边：上游那一条 `classNames.inputWrapper` 里只有
// `hover:bg-…/30` 和 `group-data-[focus=true]:bg-…/30`，没有 border 那一项。
// 浮层的开关仍然只认「按了 / 敲了字」，这里只画样子（见 viewSearchBox）。
func searchFieldBg(t *ui.Theme, hot bool) ui.Color {
	a := searchBgIdle
	if hot {
		a = searchBgHot
	}
	if t.Dark {
		return ui.RGBA(searchGrayDarkR, searchGrayDarkG, searchGrayDarkB, a)
	}
	return ui.RGBA(searchGrayLightR, searchGrayLightG, searchGrayLightB, a)
}

// searchFieldBorder 是搜索框这一刻的描边色。
//
// # ⚠️ **口径照框架那一份，别自己另立一套**
//
// 框架的 `ui.TextInput`（收藏夹 / 本地音乐那枚可展开的搜索、歌词搜索框、
// 设置页那几个输入框）用的是 `mygo@v0.6.3/ui/form.go` 的 `inputBorder`：
//
//	有焦点 → `t.Accent`；指针压在上面 → `t.Border.Mix(t.Text, 0.25)`；否则 `t.Border`
//
// 顶栏这一枚是**同应用里的另一个搜索框**，所以它得是**同一档**——
// 同一个动作（把焦点放进搜索框）只有一种反应。
//
// # 为什么顶栏这一枚原来一点指示都没有
//
// 框里那个输入框是 `ui.TextInputBase`——**没有描边**；而框架对 `kindInput`
// 一律不画焦点环（`mygo@v0.6.3/ui/paint.go:334` 的 `e.kind != kindInput`）。
// 于是聚焦时唯一的变化是底色 20% → 30%，浅色底上几乎读不出来。
//
// ⚠️ **描边画在外面那一格上，不画在输入框上**：这一格（`row`）才是用户看见的
// 那个框——输入框在它里面，只占中间那一段；描边画在输入框上会是一小截，
// 跟胶囊的形状对不上。
//
// ⚠️ **描边取的是主题里那个原值**（`t.Accent`），不是 `ink.AccentOn` 那一档：
// 描边是**图形**不是字，图形不受 4.5:1 约束（`widgets/ink` 的 `SemanticOn`
// 那一段写着这一条），而框架那一档用的也是原值——两处必须一模一样。
func searchFieldBorder(t *ui.Theme, focused, hovered bool) ui.Color {
	switch {
	case focused:
		return t.Accent
	case hovered:
		return t.Border.Mix(t.Text, 0.25)
	default:
		return t.Border
	}
}

// 搜索页那条路由是 `searchPath`（在 searchpage.go 里），这里直接用它，不另立一份
// 常量：两条路由同一个字符串，两个定义迟早会分家。

// viewSearchBox 是顶栏上的搜索框 + 建议浮层。
//
// ⚠️ 名字里的 Box 不能省：searchpage.go 有一个 `viewSearch`，那是**搜索页**
// （一个页面 view，签名都差一个 `*ui.Route`）。顶栏这个是框，页面那个是页。
func (s *Shell) viewSearchBox(c *ui.Context) {
	t := c.Theme()

	// 🔴 **框里要显示当前这一页搜的词**（理由见 `syncSearchKeyword`）。
	s.syncSearchKeyword()

	// ⚠️ 底色在 Children **之后**才定：判定要用的两个状态里，焦点这一位是
	// Children 里才量到的（`field.Focused()`），而指针那一位要等这一格建出来
	// 才能问（`row.Hovered()`）。先铺一层静止档，再按这一帧的实况盖上去——
	// 元素在 view 里改样式，画的是改完的那一份。
	//
	// ⚠️ **`MinWidth(0)` 不能省**（票 #286）：顶栏现在只盖内容区，而内容区最窄
	// 那一档是「窗口 900 − 侧栏 480 = 420」——那一排控件（两个箭头 + 右边五枚
	// 键 + 间隙）要 320，剩下给搜索框的只有八十来像素。
	//
	// **而 flex 的默认最小宽度是「内容的最小宽度」**：搜索框写死了
	// `Width(280)`，于是它的最小宽度也是 280（`mygo/ui/layout.go` 的
	// `intrinsic`：宽度写死时，内容多小都按那个宽度算）——它一步都不肯让，
	// 右边那几枚键于是被挤出窗口（实测「迷你」「动态」「头像」「登录」四枚
	// 落在 x=904 之后，而窗口只有 900 宽）。
	//
	// 钉成 0 之后它能缩到八十来像素（放大镜和字都还在），右边那几枚键留在
	// 窗口里。平时不受影响：内容区够宽时它还是 280（`Width` 写死的那个宽
	// 只在**放不下**的时候才让）。
	row := ui.Row(c).Key("search").Width(searchWidth).Height(searchHeight).
		MinWidth(0).
		Radius(searchRadius).Background(searchFieldBg(t, false)).
		// 🔴 **描边是这一档的焦点指示**（理由见 searchFieldBorder）：
		// 框里那个输入框是 `TextInputBase`——没有描边，而框架对输入框一律不画
		// 焦点环，所以不在这儿画一圈的话，聚焦时**一点指示都没有**。
		//
		// ⚠️ 描边是**画在这一格的边沿上**（框架的 `Border` 是「盒子里那条线」，
		// 不是盒外那一圈），所以这一格的内容往里让 1 像素——胶囊的形状不变。
		Border(1, searchFieldBorder(t, false, false)).
		Padding(space.None, searchPadX).Gap(searchGap).AlignItems(ui.Center)
	row.Children(func() {
		// 放大镜是**真按钮**：点它 = 拿框里的词去搜（票 BMG-253 / 报告第 4 条）。
		//
		// 它原先是**纯装饰**——不可点、永远是同一个淡灰色，而用户点它只是把光标
		// 放进输入框（结果碰巧对，但界面上没有任何东西说明它是个入口）。
		//
		// ⚠️ **命中区是整列的高**（`searchHeight`）而**宽还是图标那么宽**
		// （`iconMd`）：宽一多一个字就往右挪，而那一列的宽是上游算好的
		// （`px-3 gap-3` = 12 + 18 + 12，见上面 `searchPadX` / `searchGap`）。
		//
		// ⚠️ **悬停时图形染主色**（`flatIconButtonAccent`）：它是「图形换色」那一档
		// （上游 HeroUI 的 `hover:text-primary`），和全应用那些图标键同一套。
		mag := flatIconButtonAccent(c, iconSearch, searchActionLabel, iconMd, false).
			Size(iconMd, searchHeight).Padding(space.None).
			TextColor(t.TextMuted)

		// ⚠️ **不许给输入框加 Key**：Key 只能挂在「创建时没有处理输入」的控件上，
		// 挂上去直接 panic（`ui/element.go:658`）。外面这一格有 Key 就够了。
		field := ui.TextInputBase(c, &s.searchKeyword).
			Label(searchLabel).Placeholder(searchLabel).
			Grow(1).Shrink(0)

		// Esc 收浮层。浮层开着就吃掉这一次 Esc（字留着），浮层没开就还回
		// 输入框——那一次 Esc 该清字。
		//
		// ⚠️ 这里只改 Shell 的字段，不碰输入框里的字：`OnTextKey` 的回调跑在
		// 两帧之间（`ui/textinput.go:121`），那一刻拿着的 Element 已经过期，
		// 要改输入框里的字只能通过 `k.SetText`。我们要的恰恰是不改它。
		field.OnTextKey(func(k ui.TextKey) bool {
			if k.Key != ui.KeyEscape || !s.searchSuggest {
				return false
			}
			s.searchSuggest = false
			return true
		})

		// 浮层什么时候开：**敲了一个字**、**点了一下这个框**——照 Biu 的
		// onValueChange 和 onClick 两条（`search/index.tsx:86-90`）。
		//
		// ⚠️ Biu 还有第三条 onFocus，**这里故意不要**：见下面「失焦」那一支的
		// 说明——「焦点回来了」在这套浮层上是一个假信号。
		wasFocused := s.searchFocused
		focused := field.Focused()
		s.searchFocused = focused

		// 🔴 **填上去的那个词：点进来直接敲字是「替换」，不是「追加」**。
		//
		// 框里那个词有的时候是我们**填上去的**（深链接进来的 /search?q=周杰伦，
		// 或者翻到另一条历史），不是用户自己敲的——那时候用户点进框里敲一个字，
		// 期望的是「就搜这个字」，而实际得到的是「周杰伦林」。
		//
		// 所以**刚拿到焦点的时候把整个词选上**：接着敲的字就把它盖掉了（浏览器
		// 对自动填进去的值也是这么做的）。
		//
		// ⚠️ **只对「填上去的」那一次**：用户自己敲出来的词不许动——他在框里
		// 敲到一半要改，替他全选上等于把他敲的字吃掉。
		if focused && !wasFocused && s.searchKeywordFilled {
			field.SetTextSelection(0, len([]rune(s.searchKeyword)))
			s.searchKeywordFilled = false
		}
		if field.Changed() {
			// 用户自己敲了 → 框里那个词从此是他的。
			s.searchKeywordFilled = false
		}

		switch {
		case field.Changed(), field.Pressed():
			s.searchSuggest = true
		case s.searchSuggest && !focused:
			// 失焦。走到这里时焦点要么被别的控件拿走了（Tab 走神），要么被别处
			// 点的什么引走了——两者的共同点是「这个框里的话不打算接着搜了」。
			//
			// ⚠️ 点浮层里的一条**不算**这儿：面板上的 KeepFocus 让焦点留在框里。
			//
			// 🔴 这里面藏着一个坑，也是「不要按焦点开浮层」的全部理由：
			// Popover 是 overlay，打开时 MyGo 记下了「当时谁有焦点」
			// （`ui/widgets.go:446` 的 openOverlay）。所以我们把 open 置成
			// false 的**那一帧**，浮层就没了，帧末的 restoreFocus
			// （`ui/scope.go:143`）发现「现在谁都没焦点」——它会把焦点**还回**
			// 搜索框。下一帧就又是「刚拿到焦点」，浮层重新打开。
			// 实测：点顶栏空白处关不掉浮层，它关一下又自己回来。
			//
			// 只认「按了」和「敲了字」，焦点自己回来就开不了——它只是个残留状态。
			s.searchSuggest = false
		}

		// 回车 = 拿这个词去搜。Biu 的 submitSearch。
		if field.Submitted() {
			s.submitSearch(s.searchKeyword)
		}

		// 点那枚放大镜 = 拿这个词去搜（和回车是同一件事，票 BMG-253）。
		//
		// ⚠️ **它排在输入框之后问**：按下那一枚会把键盘焦点从输入框拿走（框架的
		// 规矩：指针按下的那条链上第一个可聚焦的元素拿到焦点），而框里那个词
		// 是这一格建好之后才最新的。
		if mag.Clicked() {
			s.submitSearch(s.searchKeyword)
		}

		// 联想词那一档的防抖。**排在 `searchSuggest` 定下来之后**——它认的就是这一位
		// （浮层没开就不问，见 `syncSuggest`）。
		s.syncSuggest(s.clock.Now())

		if s.searchKeyword != "" {
			s.viewSearchClear(c)
		}
		s.viewSuggest(c, field)
	})

	// 指针压在框上、或者框拿着焦点：底色抬到 30%（上游那两条 `…/30`）。
	// `s.searchFocused` 在 Children 里刚被这一帧的 `field.Focused()` 写过，
	// 所以点下去的那一帧就是深的那一档，不用等下一帧。
	//
	// 🔴 **描边跟着一起换**（理由见 `searchFieldBorder`）：聚焦那一档是主色，
	// 指针压上去那一档是 `t.Border` 压深——和框架的 `inputBorder` 同一个口径。
	hovered := row.Hovered()
	if s.searchFocused || hovered {
		row.Background(searchFieldBg(t, true))
	}
	row.BorderColor(searchFieldBorder(t, s.searchFocused, hovered))
}

// viewSuggest 是搜索建议浮层。**两档都在这个面板里**：搜索历史 + 联想词。
//
// ⚠️ **设置只管历史那一档，不管联想词**。Biu 的 topContent 是
// `showSearchHistory && items.length > 0`（`search/index.tsx:121-124`）——那个设置
// 管的是「我不想看见我搜过什么」，而联想词是 B 站这次给的、跟设置无关；把它一起
// 关掉的话，用户在一个只想关掉历史的人那儿连联想都看不到，而界面上没有任何地方
// 能让他知道还有这一档。
//
// ⚠️ **两档都没有时不摆面板**：用户看见的是一块突然出现的白板，而不是「最近搜过
// 这些」。
//
// 🔴 **面板里的内容必须能滚，而且高度上限跟着窗口走**（评审在这一条上钉过一次）。
//
// 两档加起来能有多高：历史最多 `searchHistoryShown`(10) 条，联想最多
// `suggestLimit`(10) 条，而历史那几条是**折行**的（长的词条一个 chip 就占掉一行
// 的一半）。实测在应用允许的最小窗口（900×560）上灌满两边之后，最后一条联想被排到
// y=735 —— 而屏幕只有 560 高：它**画在屏幕底下，点不着，也滚不到**（面板原本不是
// 一个滚动容器）。
//
// # 进场（票 #259）
//
// 面板**第一次出现**的那一帧播一段进场：淡入 + 从下面升上来 10 DIP。
// 判据是 `Shell.suggestShown`——**开着的时候内容变了不重播**（联想词返回来
// 那一趟不重播），理由见文件头《面板的进场》那一段。
func (s *Shell) viewSuggest(c *ui.Context, field ui.Element) {
	if !s.searchSuggest {
		// 面板没了：下一回它再出现的时候，那一下又是「进场」。
		s.suggestShown = false
		return
	}
	history := s.hist.list()
	showHistory := s.app.SettingsSnapshot().ShowSearchHistory && len(history) > 0
	state := s.app.SuggestState()
	terms := state.Items
	// ⚠️ **「没拉到」也算这一档有东西**（票 #455 / 报告 246 第 12 条）：那一档
	// 摆一句自己的话（`suggestFailedNote`），而不是空着——空着的话「B 站没给
	// 联想」和「这一趟没拉到」在画面上长得一模一样。
	failed := state.Failed && len(terms) == 0
	if !showHistory && len(terms) == 0 && !failed {
		// 面板**还不存在**（两档都空着）：等有内容了、它第一次出现在画面上，
		// 那一下才是「进场」。
		//
		// 🔴 **这里不许把 `suggestShown` 放回 false**：新的一趟请求会把上一批词
		// 清空（`internal/app/searchsuggest.go` 的 `s.query, s.items, s.loading =
		// keyword, nil, true`），于是**「没有搜索历史」的用户每敲一个字，面板都会
		// 空掉一帧**（框架不摆那个 panel）——按「面板此刻在不在画面上」判的话，
		// **每一次按键都会重播一遍 200ms 的进场**（票面 AC 第 4 条要挡的正是
		// 这个）。
		//
		// 所以这一位是「**这一场**播过了没有」，**只在浮层关掉的时候**才复位
		// （见上面那一支）。
		return
	}
	t := c.Theme()
	_, winH := c.Size()

	// 面板**这一场里第一次出现**的那一帧才播进场。
	//
	// ⚠️ **判据是「这一场播过了没有」，不是「开关有没有翻」**：用户敲了字、
	// 开关翻成 true 的那一帧面板可能还空着（联想词还在路上）——那时候没有
	// 面板可播；等词回来、面板第一次出现，那一下才是用户看得见的那一下。
	justOpened := !s.suggestShown
	s.suggestShown = true

	ui.PopoverBase(c, field, &s.searchSuggest, func(panel ui.Element) {
		// KeepFocus 的理由见文件头。少这一句，浮层里的条目点不到。
		p := popupPanel(t, panel.KeepFocus().
			Width(suggestWidth).Padding(space.Px8))

		// ---- 进场：淡入 + 升上来 10 DIP（票 #259） ----
		//
		// ⚠️ **第一帧要 Jump 一下**：框架的规矩是「一个值从它的第一个目标开始」
		// （`mygo/ui/motion.go` 的 channel：第一次 `AnimateTo` 直接把值给成
		// 目标），不先 Jump 的话面板会**直接出现在**它该在的地方——没有那一下。
		//
		// ⚠️ **「减少动态效果」时直接到终点**：框架的 `AnimateTo` 自己读
		// `Preferences.ReduceMotion`，为真时它把值直接给成目标（同一帧里
		// Jump 掉的那个起点也被它盖掉）——所以这一处**不用自己判**。
		if justOpened {
			p.Jump(suggestEnterFadeKey, 0)
			p.Jump(suggestEnterRiseKey, suggestEnterRise)
		}
		fade := p.AnimateTo(suggestEnterFadeKey, 1, suggestEnterAnim)
		rise := p.AnimateTo(suggestEnterRiseKey, 0, suggestEnterAnim)
		//
		// ⚠️ **位移动的是布局位置，不是 transform**（DESIGN.md §五《不做》：
		// 「动画不要用 transform 动大子树……要动就动布局位置」）。面板挂在
		// 搜索框下面（`AttachTo`），它的**上边距**就是它离搜索框多远——那一格
		// 从 10 走到 0，面板就从下面 10 DIP 升到位。
		//
		// （队列抽屉动的是面板的**右边距**，同一个办法，见 queue_drawer.go。）
		//
		// ⚠️ **不透明度是画在面板自己身上的**：那圈影跟着面板一起淡——影要是
		// 不淡，面板淡入的时候底下会先浮出一块方形阴影。
		p = p.Opacity(fade).Margin(rise, space.None, space.None, space.None)

		p.Children(func() {
			// 🔴 **这一层 `Scroll` 是「最下面那几条点得着」的全部**：MaxHeight 把它
			// 压在屏幕之内，超出去的部分靠滚。没有它，少掉的是「词看得见」——而用户
			// 看到的是一个**列表突然断在屏幕边上**，既不知道下面还有，也不知道能滚。
			suggestScroll := ui.Scroll(c).Key(suggestScrollKey).FillWidth().
				MaxHeight(suggestPanelMaxH(winH)).
				TrackScroll(&s.terms.scroll)
			suggestScroll.Children(func() {
				// 历史在上、联想在下（Biu 的 topContent 在结果列表上面，见
				// `search/index.tsx:121-146`）。中间那一道空隙是「这是两档」最直接的
				// 一句提示：没有它时两栏连着排，看起来像一栏里混着两批词。
				if showHistory {
					s.viewSuggestHeader(c)
					ui.Row(c).Key("suggest-history").Wrap().Gap(space.Px6).Children(func() {
						for _, item := range history {
							s.viewHistoryChip(c, item)
						}
					})
				}
				if len(terms) > 0 || failed {
					if showHistory {
						ui.Box(c).FillWidth().Height(suggestTermsGap)
					}
					s.viewSuggestTerms(c, terms, failed)
				}
			})
			s.trackScrollbar(suggestScroll)
		})
	})
}

// ---- 联想面板的进场（票 #259） ----
//
// 理由和出处见文件头《面板的进场》那一段（DESIGN.md §五《不做》那一条的例外）。

// suggestEnterDuration 是面板进场那一段的时长：**200ms**。
//
// ⚠️ **和队列抽屉、侧栏折叠同一档**（DESIGN.md §五《逐处》那张表）：200ms +
// `motion.Unified`。**不抄 QQ音乐那 600ms**——那是 Web 版在慢网络下的补偿。
const suggestEnterDuration = 200 * time.Millisecond

// suggestEnterRise 是面板进场时从下面升上来多少（DIP）。
//
// ⚠️ **10 在「读得出动了、读不出方向」那一档**（票面要的是 8~12）：再大就成
// 了一次「滑动」（DESIGN.md §五《规则》第 5 条），再小就只是「凭空出现」。
const suggestEnterRise = 10

// suggestEnterAnim 是面板进场那一段动画：200ms + 全应用那一条统一曲线。
//
// ⚠️ **它是包级变量，不能每帧现造**：`motion.Unified` 是一个闭包，每帧调一次
// 就是**每帧一个分配**——而每帧分配是我们唯一的性能判据
// （`internal/ui/framebudget_test.go`）。
var suggestEnterAnim = ui.Animation{Duration: suggestEnterDuration, Ease: motion.Unified}

// 两条通道的键：淡入一条、位移一条。
//
// ⚠️ **键必须是常量字符串**：框架把动画的值记在元素的 state 里，键是 `any`，
// 而非常量字符串每帧装箱一次就是每帧一个分配（同上）。
const (
	suggestEnterFadeKey = "suggest:enter:fade"
	suggestEnterRiseKey = "suggest:enter:rise"
)

// syncSearchKeyword 让顶栏那个框里显示**当前这一页搜的词**。
//
// # 为什么需要它
//
// 页内那个输入框 2026-10-22 拍板删了（`DESIGN.md` §十.1，见 searchpage.go 的
// 文件头），于是「当前搜的是什么」在界面上**只剩顶栏这一个显示**——框里空着而
// 下面已经是一屏结果，用户会以为框坏了（而改词也只能回顶栏）。
//
// # 判据是「路由上的词换了吗」，不是「框是不是空的」
//
//   - 用户正在顶栏里敲字 → 路由没变 → **一个字都不动他的**；
//   - 用户把框里的字删干净 → 路由没变 → **框不许自己弹回来**（票 #138 的评审
//     抓过这一条：弹回来之后他接着敲的字会被粘在旧词后面）；
//   - 搜了另一个词 / 翻到了另一条历史 → 路由上的词变了 → 框里那个是旧的了，
//     换掉。
//
// ⚠️ **路由上没有词的那一支直接跳过**：去别的页面（路由上的词变成空）不是
// 「换了词」，只是这一页不搜东西了——那一支要是也写，框里用户正在敲的字就会
// 在他切页回来时被他自己搜的那个词盖掉（票 #138 抓的就是这一条的反面）。
//
// ⚠️ **写的是框，不是路由**：路由上的词是「这一页在搜什么」，框里的是「用户
// 正在敲什么」——用户敲到一半没回车，那一句就还没算数。
func (s *Shell) syncSearchKeyword() {
	kw := strings.TrimSpace(s.router.Query(searchQuery))
	if kw == "" || kw == s.searchKeywordFor {
		return
	}
	s.searchKeywordFor = kw
	if s.searchKeyword == kw {
		// 框里那个词**就是**这个（用户自己敲的、刚搜完）：它是他自己的，不动，
		// 也不标成「填上去的」。
		s.searchKeywordFilled = false
		return
	}
	// 框里那个词不是这个（深链接 / 翻历史）：换掉，并且**记下它是填上去的**——
	// 用户点进来直接敲字的时候要替换，不是追加（见 `viewSearchBox` 里那一段）。
	s.searchKeyword = kw
	s.searchKeywordFilled = true
}

// suggestPanelMaxH 是这个面板最多能有多高（滚动容器的上限）。
//
// ⚠️ **跟着窗口高走，而不是写死一个数**：写死的话有两种坏法——窗口更矮时还是溢出，
// 窗口更高时白白少摆几条。真窗口改大小之后的第一帧就是按这个算的，所以它和实际
// 摆出来的高度是一致的（`c.Size()` 是窗口尺寸，见 fullplayer.go 的用法）。
func suggestPanelMaxH(winH float32) float32 {
	h := winH - suggestPanelTop - suggestPanelMargin
	// 窗口极矮时也不能矮到只剩三四行：那样「翻得动」变成「翻得累」，而联想词这一档
	// 十有八九是要被翻过去的。
	if h < suggestPanelFloor {
		return suggestPanelFloor
	}
	return h
}

// clearHistoryTip 是「清除全部」那句提示（票 BMG-253）。
//
// ⚠️ **它要说清两件事**：删掉的是什么、能不能撤。删历史**没有撤销**——
// B 站那边没有这一条路，用户自己删掉的那一条再也搜不回来。
const clearHistoryTip = "清除全部搜索历史，删掉之后不能撤销"

// viewSuggestHeader 是浮层顶上那一行：「搜索历史」和「清除全部」。
//
// 「清除全部」照 Biu 的 `clearSearchHistory`（`search/index.tsx:122-138`）。
//
// 🔴 **它是这一块唯一的不可逆动作**（票 BMG-253）：它一次性删掉全部搜索历史，
// 而 B 站那边没有「撤销」——所以它照**本仓库其它破坏性动作的画法**来：
// 一枚**垃圾桶图标** + 一句提示。
//
// ⚠️ **最贴的那一处 precedent 是「清空播放列表」**（`queue_drawer.go` 的
// `viewQueueHeader`）：那一样也是**一次删掉一整份列表**、一样没有撤销，而它是
// 一枚垃圾桶图标 + 一句提示，**没有确认框**。删搜索历史比清空播放列表轻，
// 加一道模态反而是给一个轻动作加重量。
//
// （另一族——删收藏夹 / 删选中的歌 / 删下载记录 / 删本地文件——都走确认框，
// 但那几样删的是**用户自己的东西**，不是应用自己记的一张小表。）
//
// ⚠️ **提示要说清它删的是什么、能不能撤**：原来它**一句提示都没有**，用户
// 只看见一个 12px 的淡灰字，点了才知道自己删掉了什么。
func (s *Shell) viewSuggestHeader(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Key("suggest-header").FillWidth().Gap(space.Px8).
		AlignItems(ui.Center).Padding(space.Px2, space.Px4, space.Px6, space.Px4).Children(func() {
		// 14/400（上游 `text-sm`，`search/index.tsx:126`）。**不许加粗**：
		// 上游那一行是正文那一档，靠字号和上面那排 chip 分开，不靠字重。
		ui.Text(c, "搜索历史").FontSize(scale.Font(c, scale.Size14)).Grow(1)
		// ⚠️ **字还是 12**（票面量过：标题 14、这一枚 12，见
		// `navbar_search_form_test.go` 的那条用例）——所以图标那一枚也是 14
		// 那一档的小图标，而不是 18 的大图标（18 会把这一行撑高）。
		clear := flatButton(c, "")
		clear.Children(func() {
			ui.Icon(c, icon(iconDelete)).FontSize(iconSm)
			ui.Text(c, "清除全部").FontSize(scale.Font(c, scale.Size12))
		})
		if s.withTip(c, clear, clearHistoryTip).TextColor(t.TextMuted).Clicked() {
			s.hist.clear()
		}
	})
}

// 那枚灰色 Chip 的几个数。上游是 HeroUI 的 `Chip size="sm" radius="md"
// isCloseable`（`search/index.tsx:142-166`）：
//
//	底色    solid 变体的 default：浅色 default-300 `#d4d4d8`、深色 default-700
//	        `#3f3f46`（HeroUI 那套 default 色阶；深色这个数是从上游截图里播放条
//	        那枚「无损」chip 逐像素量的，`_research/biu/screenshots/main.png`）
//	高度    24（`h-6`）
//	内边距  左右 8（`px-2`）
//	圆角    上游 `radius="md"` 映射到 HeroUI v2 的 `rounded-medium`，那是 **12**。
//	        我们没有照抄：DESIGN.md 只给浮层定了 `radius.popover: 8`，chip 这
//	        一格跟着**全局可调圆角**（`t.Radius`，默认 8）走，用户调到 12 就是
//	        上游那个数。
//
// ⚠️ **别再给它加描边**：上游 solid Chip 没有 border，一眼看过去就是一块很淡的
// 灰。加一圈 `t.Border` 之后它又会变回「带框的方块」——那正是这张票要收掉的。
const (
	historyChipLightR, historyChipLightG, historyChipLightB uint8 = 0xd4, 0xd4, 0xd8
	historyChipDarkR, historyChipDarkG, historyChipDarkB    uint8 = 0x3f, 0x3f, 0x46

	// historyChipPadRight 是 chip 右边那 8 像素：给关闭键一圈和左面对称的留白。
	historyChipPadRight = space.Px8
	// historyChipLabelPadX 是标签按钮**左边**的内边距，也是 chip 左半圈那 8
	// 像素的归属：指针落在上面点得中这条历史，而不是只有字本身可点。
	//
	// 右边只留 2，和 chip 的 gap 合起来是字到关闭键的 8（票 BMG-253 把它从 4
	// 放大了——两个后果完全相反的动作原来只靠 2 像素分开）。
	historyChipLabelPadX   = space.Px8
	historyChipLabelPadEnd = space.Px2
	// historyChipPadY 是 chip 上下那一层留白。
	//
	// 🔴 **现在是 0**（票 BMG-253）：chip 的高由**关闭键那一格**（24）撑着，
	// 而标签按钮也占满这 24（它自己的上下内边距是 3）——两个按钮的热区一样高，
	// 谁也不比谁矮一截。原来是 3：标签按钮只占中间那 18，关闭键也是 18，
	// 而它的图形只有 12。
	historyChipPadY = space.None
	// historyChipLabelPadY 是标签按钮的上下内边距：它和关闭键一样占满 chip 的
	// 24 高（文字那 18 + 上下各 3）。
	historyChipLabelPadY = space.Px3
	// historyChipClose 是右边那个关闭键的点击区。
	//
	// 🔴 **24 而不是 18**（票 BMG-253）：那 18 是「12 的图标 + 每边 3」，
	// 而 chip 里的标签按钮占着 24 高——两个热区一个高一格矮，而矮的那个偏偏是
	// 图形最小的那一个（12 的叉对 12 的字）。现在关闭键占满 chip 整个高，
	// 和标签按钮**一样高**。
	historyChipClose = 24
	// historyChipGap 是标签按钮和关闭键之间那道缝。
	//
	// 🔴 **6 而不是 2**（票 BMG-253）：这两个按钮的后果**完全相反**（一个去搜
	// 这一条，一个删掉这一条），而它们原来只靠 2 像素分开——指针偏一点就删了。
	historyChipGap = space.Px6
)

// historyChipBg 是那枚灰色 Chip 的底色。深浅两套是 HeroUI default 色阶的两头。
func historyChipBg(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.RGBA(historyChipDarkR, historyChipDarkG, historyChipDarkB, 1)
	}
	return ui.RGBA(historyChipLightR, historyChipLightG, historyChipLightB, 1)
}

// viewHistoryChip 是浮层里的一条历史。
//
// Biu 那一排是可关的 Chip（`isCloseable`，`:142-166`）：点它去搜这条，点它右边
// 的 × 只删这一条。这里同样拆成两个按钮，中间那个标签才是去搜的那一个。
//
// ⚠️ **形状是一枚 chip，不是一行带框的方块**：灰底、无描边、24 高。里面那两个
// 按钮是扁平的（`flatButton` / `flatIconButton`），只在指针压上去时铺一层很淡的
// 反馈；chip 自己的灰色底一直留着，所以它仍然是一个「小块」，而不是两枚按钮。
func (s *Shell) viewHistoryChip(c *ui.Context, item searchItem) {
	t := c.Theme()
	label := searchHistorySummary(item.Value)

	ui.Row(c).Key("history:"+item.Value).Radius(t.Radius).
		Background(historyChipBg(t)).
		// 左边不留内边距：那 8 像素归标签按钮，点了才落在这一条上。右边留 8，
		// 给关闭键一圈和左面对称的留白。
		//
		// ⚠️ **上下不留**（`historyChipPadY`）：chip 的高由关闭键那一格撑着，
		// 标签按钮也占满这 24——两个热区一样高。
		Padding(historyChipPadY, historyChipPadRight, historyChipPadY, space.None).
		Gap(historyChipGap).AlignItems(ui.Center).Children(func() {
		// ⚠️ 标签按钮的上下内边距是 3（不是 `flatButton` 默认的 6）：套在 chip
		// 里的话默认那个 6 会把 24 高撑到 30 以上，而它要和关闭键一样占满这 24。
		if flatButton(c, label).FontSize(scale.Font(c, scale.Size12)).
			Padding(historyChipLabelPadY, historyChipLabelPadEnd,
				historyChipLabelPadY, historyChipLabelPadX).Clicked() {
			// ⚠️ **先把词填回框里**（Biu 的 `setValue(item.value)`，
			// `search/index.tsx:154`）。不填的话用户点完历史，框里还是原来敲的
			// 半截字，而页面那边搜的是这一条——两处对不上，按返回再回来还会
			// 看到那个错的词。
			s.searchKeyword = item.Value
			s.submitSearch(item.Value)
		}
		// 删这一条。Biu 的 onClose。
		//
		// ⚠️ 无障碍名字写全，别只留一个「×」：读屏软件念出来就是「乘号」，用户
		// 根本不知道它在删哪一条。画面上是上游那枚叉图标。
		if flatIconButton(c, iconClose, "删除「"+label+"」", iconCloseGlyph).
			Size(historyChipClose, historyChipClose).Padding(space.None).
			TextColor(t.TextMuted).Clicked() {
			s.hist.remove(item.Value)
		}
	})
}

// submitSearch 是「拿这个词去搜」：记进历史、带着词切到搜索页、收起浮层。
//
// 逐条照 Biu 的 `submitSearch`（`search/index.tsx:52-61`）：空词不进历史；
// 已经记过的那条挪到最前（`add` 里做的）。
//
// ⚠️ **词要跟着 URL 走**（`?q=…`），Biu 也是 `navigate("/search?q=" + keyword)`。
// 不带的话用户回车之后落在搜索页的「还没输入」那一支：正文写着「在上面的搜索框里
// 输入歌名或 UP 主」，而他刚敲的词还留在顶栏那个框里——从页面上看就是一句话也搜
// 不成。搜索页从 URL 上取 `q`（见 searchpage.go 的 `searchQuery`），所以这里
// 是那条链路的另一半，两头必须一起改。
//
// ⚠️ 已经在搜索页上时**不重发** `navigate`：同一个 target 再 push 一次会在历史里
// 连着留两条一样的（`Router.Push` 不去重），返回键要按两下才回得去。
//
// ⚠️ **空词 = 没有这一场搜索**：在搜索页上就回到「还没输入」那一支（页内那个框
// 2026-10-22 删掉之前就是这个行为——清空之后回车回到「没搜」）。不回的话，用户把
// 框里的字删干净又按回车，界面上什么也不发生，而下面还是上一场搜索的结果。
func (s *Shell) submitSearch(keyword string) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		if s.router.Path() == searchPath {
			s.navigate(searchPath)
		}
		return
	}
	s.hist.add(keyword)
	s.searchSuggest = false
	target := searchPath + "?" + searchQuery + "=" + url.QueryEscape(keyword)
	if s.router.Location() == target {
		return
	}
	s.navigate(target)
}

// viewSearchClear 是搜索框右边那个清空按钮（Biu 的 `isClearable`）。
//
// 它在浮层**外面**，所以点它落不到面板里的任何东西上。
//
// ⚠️ 画面上是那个叉，**无障碍名字仍然是「清空」**：叉对读屏软件念出来是「乘号」，
// 用户听不出它在清什么（同一个道理见 viewHistoryChip 里那个「删除「…」」）。
//
// 🔴 **在搜索页上它清的是「这一场搜索」，不只是框里那几个字**（票 #234 的评审
// 抓出来的那一条）：页内那个输入框删掉之后，「清空」只剩这一个入口，而清完框
// 而下面还是「搜索【周杰伦】的结果」和一屏结果——那正是 F-01 要消掉的那种
// 「框和结果对不上」。所以在搜索页上按它就回到「还没输入」那一支。
//
// ⚠️ **别的页面上它只清框**：首页上按它不该把用户带到搜索页去。
func (s *Shell) viewSearchClear(c *ui.Context) {
	onSearch := s.router.Path() == searchPath
	tip := "清空搜索框"
	if onSearch {
		tip = "清空搜索"
	}
	if s.withTip(c, iconButton(c, iconClose, "清空", iconCloseGlyph).
		Size(iconCloseHit, iconCloseHit).Padding(space.None, space.Px7).
		TextColor(c.Theme().TextMuted), tip).Clicked() {
		s.searchKeyword = ""
		s.searchKeywordFilled = false
		if onSearch {
			// 回到「还没输入」那一支：框空了，结果也就该没有。
			s.navigate(searchPath)
		}
	}
}
