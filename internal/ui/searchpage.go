// 搜索页：Biu 的 `/search`（`src/pages/search/`）。
//
// ## 这一版摆什么
//
// 一个搜索框 + 两个空状态。**没有结果列表**——搜索是阶段 5 的活儿
// （PLAN.md 的 P-08~P-11、C-51），后端还没接，所以结果区永远是空的。
//
// 为什么还是要做这一页：任务点名要覆盖「没有搜索结果」这个场景，而上游搜索页上
// 它是**两个不同的 Empty 分支**——
//
//	pages/search/index.tsx:16          还没输入搜索词 → <Empty />
//	pages/search/video-list/index.tsx:176  搜了但没有结果 → <Empty />
//
// 画出来是同一个 `<Empty />`（所以标题一样），但接下来该做的事不一样：一个是
// 「去输入」，一个是「换个词再搜」。没有搜索框的时候只有第一个分支能走到，
// 第二个分支就成了一个只有测试引用的常量——那一支等于没做。
//
// 🔴 本文件里的 view **不许**调 `c.SetTheme`：主题每帧开头装一次就够，别的页面
// 照样生效（铁律二，见 theme.go）。
package ui

import (
	"net/url"
	"strings"

	"github.com/egoist/mygo/ui"
)

// searchPath 是搜索页那条路由，和 routes 表里登记的是同一个字符串。
//
// 拿来和 `Route.Match` 比，而不是拿 `routes[i]`：那条表一改顺序，这里跟着错，
// 而且错得很安静——搜索页退回占位页，没人看得出来。
const searchPath = "/search"

// searchQuery 是搜索词在路径里的键。带上它是为了两件事：
//
//   - 输入框里的字能进历史（前进/后退能回到上一次搜的东西）；
//   - 「还没输入」和「搜了没有」这两个状态能从 URL 上分得出来。
//
// Biu 把搜索词放在路由 state 里（`useSearchParams`），URL 上带 query 是同一件事。
const searchQuery = "q"

// searchIdle 是「还没输入搜索词」这一支：上游 `pages/search/index.tsx:16`。
var searchIdle = emptyState{
	Title: "暂无内容",
	Hint:  "在上面的搜索框里输入歌名或 UP 主",
}

// searchPageMaxWidth 是搜索框的宽度。Biu 搜索页的内容区是 `max-w-3xl`，这里
// 取一个够放下歌名的整数。
const searchPageMaxWidth = 420

// viewSearch 是搜索页：页头 + 搜索框 + 当前状态该画的那一句。
//
// ⚠️ 搜完之后画的是「没有结果」那一支（`noSearchResults`），不是「还在加载」。
// 这一版没有后端，结果区永远是空的——所以这一支就是现在真会发生的事，页面画
// 的和实际状态是对得上的。等到阶段 5 把 P-08 的接口接进来，这一支自然换成
// 「有结果时列结果、没有时画这一句」，页面结构一个字都不用改。
func (s *Shell) viewSearch(c *ui.Context, r *ui.Route) {
	// 🔴 铁律二：页面 view 的头一件正事就是装主题（见 theme.go）。
	s.applyFrameTheme(c)

	// 🔴 **问 `r`，不要问 `c.Router()`。**
	//
	// 换页动画还没走完时，**正在离场的那一页**会被重新建一遍（`Router.View` 的
	// leaving 那一支），而 MyGo 建离场页时**故意不设 `c.router`**
	// （`mygo/router.go:685`），`Context.Router()` 的文档也写着
	// 「or nil outside one」。于是 `c.Router().Query(...)` 在那一帧就是空指针
	// 解引用，整个窗口被换成错误页。
	//
	// 症状：顶栏搜索框回车进这一页之后，只要在滑动还没走完时点返回（或点侧栏
	// 第一项），就崩。别的目标（/follow、/settings…）不崩——只有**滑动型**换页
	// 才重建离场的那一页，而 #46 那个 `s.router.Transition = None` 在生产里是
	// 默认的自动动画。
	//
	// `(*ui.Route).Query` 读的是**它自己那条 entry 的 loc**（`mygo/router.go:447`），
	// 正是这一页自己的查询串：离场重建时拿到的还是同一个 Route，所以既不会崩，
	// 也不会在滑出去的那 250ms 里闪成另一个空状态。
	keyword := r.Query(searchQuery)

	ui.Column(c).Key("page:/search").Grow(1).Padding(28).Gap(16).AlignItems(ui.Stretch).
		Children(func() {
			ui.Text(c, "搜索").FontSize(22).Bold()

			// ⚠️ Label 不只是给测试用的：没有它读屏软件读不出这是一个搜歌名的
			// 输入框，只有旁边那行占位文字（占位文字不是标签）。顺带也是无头
			// Tester 唯一能认出这个控件的东西——它只认 label / text，不认
			// placeholder。
			box := ui.TextInput(c, &s.searchDraft).
				Width(searchPageMaxWidth).
				Label("搜索歌名或 UP 主").
				Placeholder("歌名或 UP 主")
			// 回车 = 搜。没输入就回退到「还没输入」那一支：一条空 query 的 URL
			// 和没搜过是同一件事，别让它停在「搜了没有」上。
			if box.Submitted() {
				kw := strings.TrimSpace(s.searchDraft)
				s.searchDraft = kw
				if kw == "" {
					s.navigate(searchPath)
				} else {
					s.navigate(searchPath + "?" + searchQuery + "=" + url.QueryEscape(kw))
				}
			}

			if keyword == "" {
				s.viewEmpty(c, "search:idle", searchIdle)
				return
			}
			s.viewEmpty(c, "search:no-results", noSearchResults)
		})
}
