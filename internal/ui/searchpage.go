// 搜索页：Biu 的 `/search`（`src/pages/search/`）。
//
// 一个搜索框 + 三个 Tab 的结果（**歌曲 / 视频 / 用户**），每个 Tab 一套自己的排序、
// 一份自己的分页列表。拉页、防重入、取消、自动续拉、回顶全是 `widgets/list` 那一层
// 的事，这一页只管摆；状态、查询、三个反直觉行为在 searchresults.go。
//
// 🔴 本文件里的 view **不许**调 `c.SetTheme`：主题每帧开头装一次就够（铁律二，见
// theme.go）。
package ui

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/egoist/mygo/ui"
)

// searchPath 是搜索页那条路由，和 routes 表里登记的是同一个字符串（拿来和
// `Route.Match` 比，不是拿 `routes[i]`：表一改顺序这里跟着错，错得很安静）。
const searchPath = "/search"

// userPathPrefix 是 UP 主空间那条路径的前缀，完整的路径是它加上 mid。
//
// ⚠️ routes 表里登记的是**模式** `/user/{id}`，不是能直接拼的路径——留前缀是为了
// 不让「拼地址」去处理模式里的 `{…}`。拼错一次的表现是点用户跳到 404 页，而
// 404 页上有倒计时，不点穿的人只当是那一行没反应。
//
// UP 主空间页（PLAN 阶段 5 的 P-11）还没写，这一页是**唯一**用到它的地方；那一页
// 做出来时把这个常量搬过去、这里改用它。
const userPathPrefix = "/user/"

// searchQuery 是搜索词在路径里的键。带上它是为了两件事：输入框里的字能进历史
// （前进/后退能回到上一次搜的东西），以及「还没输入」和「搜了没有」这两个状态
// 能从 URL 上分得出来。Biu 把搜索词放在路由 state 里（`useSearchParams`），URL
// 上带 query 是同一件事。
//
// ⚠️ **同名不同义要当心**：`searchresults.go` 里那个「一次查询」的**结构体**因此
// 叫 `searchQuerySpec`——两个都叫 searchQuery 而一个是键、一个是查询，读代码的
// 人第一眼一定读错。
const searchQuery = "q"

// searchIdle 是「还没输入搜索词」这一支：上游 `pages/search/index.tsx:16`。
var searchIdle = emptyState{
	Title: "暂无内容",
	Hint:  "在上面的搜索框里输入歌名或 UP 主",
}

// searchPageMaxWidth 是搜索框的宽度。Biu 搜索页的内容区是 `max-w-3xl`，这里
// 取一个够放下歌名的整数。
const searchPageMaxWidth = 420

// searchHeaderGap 是页头那几块（标题、搜索框、Tab、排序）之间的距离。
// Biu 那边是 `py-4`（`pages/search/index.tsx:26`）。
const searchHeaderGap = 12

// viewSearch 是搜索页：页头 + 搜索框 + 当前状态该画的那一块。
//
// 🔴 关键词**问 `r`，不要问 `c.Router()`**：换页动画期间离场的那一页会被重建一遍
// （`Router.View` 的 leaving 那一支），而 MyGo 建离场页时**故意不设 `c.router`**
// （`mygo/router.go:685`），`c.Router().Query` 在那一帧就是空指针解引用，整个窗口
// 被换成错误页。`(*ui.Route).Query` 读的是它自己那条 entry 的 loc，正是这一页自己
// 的查询串。
func (s *Shell) viewSearch(c *ui.Context, r *ui.Route) {
	// 🔴 铁律二：页面 view 的头一件正事就是装主题（见 theme.go）。
	s.applyFrameTheme(c)

	keyword := strings.TrimSpace(r.Query(searchQuery))

	ui.Column(c).Key("page:/search").Grow(1).Padding(28).Gap(searchHeaderGap).
		AlignItems(ui.Stretch).Children(func() {
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
			// 还没输入：上游 `pages/search/index.tsx:16` 的那一支。
			s.viewEmpty(c, "search:idle", searchIdle)
			return
		}
		s.viewSearchResults(c, keyword)
	})
}

// viewSearchResults 是搜了词之后的那一块：标题 + 三个 Tab + 排序行 + 结果。
// 摆的顺序照上游（`pages/search/index.tsx:23-58`）。
func (s *Shell) viewSearchResults(c *ui.Context, keyword string) {
	st := &s.search

	ui.Column(c).Key("search:results").Grow(1).Gap(searchHeaderGap).
		AlignItems(ui.Stretch).Children(func() {
		ui.Textf(c, "搜索【%s】的结果", keyword).FontSize(18).Bold()

		// 🔴 **两排 Tabs 都要在 `current` 之前画**：它们都是建到的时候才写
		// `st.tab` / `st.sorts[cur]`（`mygo/ui/base.go` 的 `tabsParts.Tab` 的
		// `choose`：写指针 + 把这一帧标记为重建）。顺序反过来的表现是：用户点了
		// 「最多播放」，这一帧的 `current` 读到的还是旧排序，这一帧画的还是旧
		// 顺序，要到**下一帧**才重置回第 1 页——屏幕上就是「按了排序，画面先抖
		// 一下才换」，而中间那一帧还滚回了顶上，用户刚滚到的位置没了。
		ui.Tabs(c, &st.tab, searchTabNames[:]...)
		cur := st.tab
		s.viewSearchToolbar(c, cur)

		// 🔴 换查询 → 重置到第 1 页（理由见 searchTab.current）。
		st.lists[cur].current(searchQuerySpec{keyword: keyword, sort: st.sorts[cur]})

		// 列表要吃掉剩下的高度，自己占 Grow(1)（见 list.go 的 View）。外面这一
		// 层 Grow(1) 是给它留地方：直接摆在根上不给高度的话，它会按自己的行数
		// 去要高度，一屏二十几行会把页面撑出去。
		s.viewSearchResultsBody(c, cur)
	})
}

// viewSearchToolbar 是 Tab 下面那一行：左边排序，右边「全部播放」。排序是**每个
// Tab 自己一套**（`st.sorts` 按 Tab 分格）。用户的 Tab 没有「全部播放」——用户不是
// 歌，播不了（上游的用户页也只有排序，见 `user-list/search-header.tsx`）。
func (s *Shell) viewSearchToolbar(c *ui.Context, tab int) {
	st := &s.search

	ui.Row(c).Key("search:toolbar:" + strconv.Itoa(tab)).FillWidth().Gap(12).
		AlignItems(ui.Center).Children(func() {
		ui.Tabs(c, &st.sorts[tab], searchSortLabels(tab)...)

		if tab == searchTabUser {
			return
		}
		// 「全部播放」：Biu 的 `AsyncButton`（`video-list/search-header.tsx:55`），
		// 它禁用的条件是 `initialLoading || list.length === 0`——正在装第一页
		// 或者一首都没有时不让按。
		pg := st.lists[tab].list.Paged()
		if ui.PrimaryButton(c, "全部播放").
			Disabled(pg.Loading || pg.Loaded == 0).
			Clicked() {
			s.searchPlayAll(&st.lists[tab])
		}
	})
}

// viewSearchResultsBody 是结果那一块。一行都没有、而且此刻没在拉 → 画「没有搜索
// 结果」；正在拉 → 列表自己画「正在加载…」（paged.go 的 syncList）；其余 → 列表。
//
// ⚠️ 第一支**不看有没有出错**，照抄上游：它的 `retryInitial` 失败时
// `addToast("加载失败")`，而 `list` 仍然是空的，于是下一句 `!initialLoading &&
// !list?.length` 成立，画的是同一个 `<Empty />`（`video-list/index.tsx:181`）——
// **搜不出来时用户看见的是「换个词再搜」加一条 Toast**，不是一块错误页。
//
// 拉了半截之后失败是另一回事：那时 `Loaded > 0`，末尾那一行由分页列表自己给出
// 「加载失败，点此重试」。
func (s *Shell) viewSearchResultsBody(c *ui.Context, tab int) {
	t := &s.search.lists[tab]
	pg := t.list.Paged()

	if pg.Loaded == 0 && !pg.Loading {
		s.viewEmpty(c, "search:no-results:"+strconv.Itoa(tab), noSearchResults)
		return
	}
	// 🔴 设置里的「数据显示样式」在这一帧生效（`s.displayView` 的注释写了为什么
	// 每帧都要调）。三个 Tab 各调各的：三份列表各记着自己停在哪，切 Tab 不重置。
	t.list.SetView(s.displayView())
	// ⚠️ **表头在列表外面**（`rowheader.go` 文件头）：它是 `View` 的兄弟，所以列表往
	// 下滚它不动，宽屏下滚到右边列名也还在。
	//
	// 这一处自己给规格而不用 `PagedList.Header`：「用户」那一档的行没有时长那一列
	// （理由见 `searchHeaderSpec`）。
	if v := t.list.Layout(); v != list.ViewCard {
		list.Header(c, searchHeaderSpec(tab, v,
			t.list.List().HasSelection(), "search:header:"+strconv.Itoa(tab)))
	}
	t.list.View(c)
}

// searchHeaderSpec 是这一档 Tab 的表头。
//
// ⚠️ **「用户」那一档没有「时长」**：那一档的行是「序号 / 头像 / 昵称 / 个性签名」
// （见 `searchUserMusic` 的注释），`list.Music.Duration` 是零值，于是行里那一列**根
// 本不画**——表头上还写着「时长」的话，用户读到的是一列空的表头，而空表头比没有表头更
// 难解释（看不出那是没数据还是坏了）。
func searchHeaderSpec(tab int, v list.View, check bool, key string) list.HeaderSpec {
	spec := list.CardHeaderSpec(v, check, key)
	if tab == searchTabUser {
		// 去掉最后那一列（「时长」）——`CardHeaderSpec` 的顺序是固定的，见它自己。
		spec.Cols = spec.Cols[:len(spec.Cols)-1]
	}
	return spec
}

// searchSortLabels 是 tab 那一档的排序标签，按 Biu 的顺序。
func searchSortLabels(tab int) []string {
	if tab == searchTabUser {
		out := make([]string, len(searchUserSorts))
		for i, o := range searchUserSorts {
			out[i] = o.Label
		}
		return out
	}
	out := make([]string, len(searchVideoSorts))
	for i, o := range searchVideoSorts {
		out[i] = o.Label
	}
	return out
}
