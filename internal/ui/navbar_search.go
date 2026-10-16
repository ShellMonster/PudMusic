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
// ## 300ms 防抖在哪儿
//
// Biu 的 `debounceWait: 300`（`search/index.tsx:49`）是加在**远端搜索建议**上的
// （`getSearchSuggestMain`）。这一版还没有搜索接口，浮层里摆的只有本地的历史
// 条目——本地数据不需要防抖，装一个防抖只是让界面慢半拍。所以这一处**故意没有**
// 防抖，接上接口时再加（它属于搜索接口那张卡片）。
package ui

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

const (
	// searchWidth 是搜索框多宽。Biu 是 `w-[280px]`（`search/index.tsx:73`）。
	searchWidth = 280

	// suggestWidth 是建议浮层多宽。Biu 是 `w-[360px]`（同文件 :101）。
	//
	// 比搜索框宽是 Biu 的原样：里面那几行是「搜索历史」的标题加一排 chip，
	// 按输入框那么宽排不下。
	suggestWidth = 360

	// searchLabel 是搜索框的无障碍名字，也是测试认得它的凭据——无头 Tester 的
	// `Find` 找的是「显示着这个字或者标着这个名字」的元素。
	searchLabel = "搜索"

	// searchHeight 是搜索框多高。Biu 那边是 `h-10`（40），顶栏只让出 32~38，
	// 这里取 28 上下正好放下放大镜、一行字和清空按钮。
	searchHeight = 28
)

// 搜索页那条路由是 `searchPath`（在 searchpage.go 里），这里直接用它，不另立一份
// 常量：两条路由同一个字符串，两个定义迟早会分家。

// viewSearchBox 是顶栏上的搜索框 + 建议浮层。
//
// ⚠️ 名字里的 Box 不能省：searchpage.go 有一个 `viewSearch`，那是**搜索页**
// （一个页面 view，签名都差一个 `*ui.Route`）。顶栏这个是框，页面那个是页。
func (s *Shell) viewSearchBox(c *ui.Context) {
	t := c.Theme()
	// 焦点在输入框上时把边框染成主色（Biu 是 `group-data-[focus=true]`）。
	// 读的是**上一帧**的 searchFocused——边框样式跟在这一帧的判断后面走，
	// 差一帧看不出来。
	border := t.Border
	if s.searchFocused {
		border = t.Accent
	}

	ui.Row(c).Key("search").Width(searchWidth).Height(searchHeight).
		Radius(t.Radius).Background(t.Surface).Border(1, border).
		Padding(0, 8).Gap(6).AlignItems(ui.Center).Children(func() {
		drawMagnifier(c, t.TextMuted)

		// ⚠️ **不许给输入框加 Key**：Key 只能挂在「创建时没有处理输入」的控件上，
		// 挂上去直接 panic（`ui/element.go:537`）。外面这一格有 Key 就够了。
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
		focused := field.Focused()
		s.searchFocused = focused
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

		if s.searchKeyword != "" {
			s.viewSearchClear(c)
		}
		s.viewSuggest(c, field)
	})
}

// viewSuggest 是搜索建议浮层：设置里开着、并且有历史，才摆。
//
// Biu 的 topContent 是 `showSearchHistory && items.length > 0`
// （`search/index.tsx:121-124`）——**设置只管显不显示，不管存不存**。
func (s *Shell) viewSuggest(c *ui.Context, field ui.Element) {
	if !s.searchSuggest {
		return
	}
	history := s.hist.list()
	if !s.app.SettingsSnapshot().ShowSearchHistory || len(history) == 0 {
		// 一个都没有还摆一块空板子没有意义：用户看见的是一块突然出现的白板，
		// 而不是「最近搜过这些」。
		//
		// 真接上搜索接口之后，这里摆 Biu 的 emptyContent「暂无搜索建议」。
		return
	}
	t := c.Theme()

	ui.PopoverBase(c, field, &s.searchSuggest, func(panel ui.Element) {
		// KeepFocus 的理由见文件头。少这一句，浮层里的条目点不到。
		panel.KeepFocus().
			Width(suggestWidth).Radius(t.Radius+2).
			Background(t.Background).Border(1, t.Border).
			Shadow(0, 6, 20, 0, suggestShadow(t)).
			Padding(8).Children(func() {
			s.viewSuggestHeader(c)
			ui.Row(c).Key("suggest-history").Wrap().Gap(6).Children(func() {
				for _, item := range history {
					s.viewHistoryChip(c, item)
				}
			})
		})
	})
}

// viewSuggestHeader 是浮层顶上那一行：「搜索历史」和「清除全部」。
//
// 「清除全部」照 Biu 的 `clearSearchHistory`（`search/index.tsx:122-138`）。
func (s *Shell) viewSuggestHeader(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Key("suggest-header").FillWidth().Gap(8).
		AlignItems(ui.Center).Padding(2, 4, 6, 4).Children(func() {
		ui.Text(c, "搜索历史").FontSize(13).Bold().Grow(1)
		if ui.Button(c, "清除全部").FontSize(12).TextColor(t.TextMuted).Clicked() {
			s.hist.clear()
		}
	})
}

// viewHistoryChip 是浮层里的一条历史。
//
// Biu 那一排是可关的 Chip（`isCloseable`，`:142-166`）：点它去搜这条，点它右边
// 的 × 只删这一条。这里同样拆成两个按钮，中间那个标签才是去搜的那一个。
func (s *Shell) viewHistoryChip(c *ui.Context, item searchItem) {
	t := c.Theme()
	label := searchHistorySummary(item.Value)

	ui.Row(c).Key("history:"+item.Value).Radius(t.Radius).
		Background(t.Surface).Border(1, t.Border).
		Padding(2, 4, 2, 8).Gap(4).AlignItems(ui.Center).Children(func() {
		if ui.Button(c, label).FontSize(12).Clicked() {
			s.submitSearch(item.Value)
		}
		// 删这一条。Biu 的 onClose。
		//
		// ⚠️ 无障碍名字写全，别只留一个「×」：读屏软件念出来就是「乘号」，用户
		// 根本不知道它在删哪一条。画面上还是那个叉。
		if ui.Button(c, "×").Label("删除「" + label + "」").
			FontSize(12).TextColor(t.TextMuted).Clicked() {
			s.hist.remove(item.Value)
		}
	})
}

// submitSearch 是「拿这个词去搜」：记进历史、切到搜索页、收起浮层。
//
// 逐条照 Biu 的 `submitSearch`（`search/index.tsx:52-61`）：空词不进历史；
// 已经记过的那条挪到最前（`add` 里做的）；已经在搜索页就不再切一次。
func (s *Shell) submitSearch(keyword string) {
	if strings.TrimSpace(keyword) == "" {
		return
	}
	s.hist.add(keyword)
	s.searchSuggest = false
	if s.router.Path() != searchPath {
		s.navigate(searchPath)
	}
}

// viewSearchClear 是搜索框右边那个清空按钮（Biu 的 `isClearable`）。
//
// 它在浮层**外面**，所以点它落不到面板里的任何东西上。
func (s *Shell) viewSearchClear(c *ui.Context) {
	if ui.Button(c, "清空").FontSize(11).TextColor(c.Theme().TextMuted).Clicked() {
		s.searchKeyword = ""
	}
}

// drawMagnifier 是搜索框左边那个放大镜。
//
// Biu 用 `<RiSearchLine size={16}/>`，本项目还没有图标资源。照 MyGo 自己那个
// 放大镜的画法（`ui/combobox.go:459` 的 magnifier）画一个，省掉一整个图标资源
// 目录，也不用为一个圆圈加一条依赖。
func drawMagnifier(c *ui.Context, color ui.Color) {
	const size = 14
	ui.Box(c).Size(size, size).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		var ring ui.Path
		ring.Circle(r.X+r.W*0.4, r.Y+r.H*0.4, r.W*0.28)
		p.StrokePath(&ring, 1.4, color)
		p.Line(r.X+r.W*0.64, r.Y+r.H*0.64, r.X+r.W*0.9, r.Y+r.H*0.9, 1.4, color)
	})
}

// suggestShadow 是浮层那圈影子的颜色。Biu 是 `shadow-2xl`，跟着主题分深浅。
func suggestShadow(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.Color{A: 90}
	}
	return ui.Color{A: 46}
}
