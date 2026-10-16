// 搜索页：Biu 的 `/search`（`src/pages/search/`）。
//
// ## 这一版摆什么
//
// 一个搜索框 + 三个 Tab 的结果：**歌曲 / 视频 / 用户**，每个 Tab 一套自己的排序，
// 每个 Tab 一份自己的分页列表。拉页、防重入、取消、容器没填满自动续拉、回顶
// 全是 `widgets/list` 那一层的事（见 searchresults.go），这一页只管摆。
//
// ## 三条容易做错的（PLAN 阶段 5 的验收）
//
//	① 「全部播放」**只追加不替换**（searchPlayAll）
//	② 任一筛选条件变化**自动重置到第 1 页**（searchTab.current）
//	③ 切 Tab **不重置滚动位置**（searchTab.current 的注释）
//
// ①③ 在 searchresults.go 里，那边的注释比这儿长；这里是它们落在屏幕上的地方。
//
// 🔴 本文件里的 view **不许**调 `c.SetTheme`：主题每帧开头装一次就够，别的页面
// 照样生效（铁律二，见 theme.go）。
//
// ## 为什么三个 Tab 而不是上游的两个
//
// 见 searchresults.go 文件头：「歌曲」是上游那个默认开着的「仅音乐分区」开关
// 提上来的（`tids=3`），用户想看全部投稿时切到「视频」。
package ui

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

// searchPath 是搜索页那条路由，和 routes 表里登记的是同一个字符串。
//
// 拿来和 `Route.Match` 比，而不是拿 `routes[i]`：那条表一改顺序，这里跟着错，
// 而且错得很安静——搜索页退回占位页，没人看得出来。
const searchPath = "/search"

// userPathPrefix 是 UP 主空间那条路径的前缀，完整的路径是它加上 mid。
//
// ⚠️ routes 表里登记的是**模式** `/user/{id}`，不是能直接拼的路径。这里留
// 前缀而不是那个模式，是为了不让「拼地址」这件事去处理模式里的 `{…}`——
// 拼错一次的表现是点用户跳到一个 404 页，而 404 页上有倒计时，不点穿的人
// 只当是那一行没反应。
//
// UP 主空间页（PLAN 阶段 5 的 P-11）还没写，这一页是**唯一**用到它的地方。
// 那一页做出来的时候把这个常量搬过去、这里改用它，别在两处各写一个字符串。
const userPathPrefix = "/user/"

// searchQuery 是搜索词在路径里的键。带上它是为了两件事：
//
//   - 输入框里的字能进历史（前进/后退能回到上一次搜的东西）；
//   - 「还没输入」和「搜了没有」这两个状态能从 URL 上分得出来。
//
// Biu 把搜索词放在路由 state 里（`useSearchParams`），URL 上带 query 是同一件事。
//
// ⚠️ **同名不同义要当心**：包级已经有一个 `searchQuery` 是这个字符串常量，
// `searchresults.go` 里那个「一次查询」的**结构体**因此叫 `searchQuerySpec`。
// 两个都叫 searchQuery 而一个是键、一个是查询，读代码的人第一眼一定读错。
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
// ⚠️ 关键词**问 `r`，不要问 `c.Router()`**（换页动画期间离场页会被重建，
// `mygo/router.go:685` 故意不设 `c.router`，问它就是空指针解引用）。理由的
// 全文在下面 keyword 那一段。
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
//
// 摆的顺序照上游（`pages/search/index.tsx:23-58`）：`<h1>搜索【词】的结果</h1>`
// 下面一行 Tab，Tab 下面才是那一档自己的表头（排序 + 全部播放）和列表。
func (s *Shell) viewSearchResults(c *ui.Context, keyword string) {
	st := &s.search

	ui.Column(c).Key("search:results").Grow(1).Gap(searchHeaderGap).
		AlignItems(ui.Stretch).Children(func() {
		ui.Textf(c, "搜索【%s】的结果", keyword).FontSize(18).Bold()

		// 三个 Tab。`ui.Tabs` 直接写 `st.tab`，新的一次点选会把这一帧重建
		// 一遍（`Changed` 的语义），所以**下面取当前 Tab 要在它之后**。
		ui.Tabs(c, &st.tab, searchTabNames[:]...)

		// ⚠️ 取当前 Tab 放在 Tabs 之后：Tab 是在建它的时候被写上的，
		// 放在前面拿到的是上一帧那一个。
		cur := st.tab

		// ⚠️ 排序行也**必须在 `current` 之前画**：排序那排 `ui.Tabs` 同样是
		// 建到它的时候才写 `st.sorts[cur]`。顺序反过来的表现是：用户点了
		// 「最多播放」，这一帧的 `current` 读到的还是旧排序，于是这一帧画的
		// 还是旧顺序，要到**下一帧**才重置回第 1 页——屏幕上就是「按了排序，
		// 画面先抖一下才换」，而中间那一帧还滚回了顶上，用户刚滚到的位置没了，
		// 还说不清是被谁弄没的。
		s.viewSearchToolbar(c, cur)

		// 🔴 换查询 → 重置到第 1 页。在画结果之前做，画出来的就是第一页。
		st.lists[cur].current(searchQuerySpec{keyword: keyword, sort: st.sorts[cur]})

		// 列表要吃掉剩下的高度，自己占 Grow(1)（见 list.go 的 View）。
		// 外面这一层 Grow(1) 是给它留地方：直接摆在根上不给高度的话，它会
		// 按自己的行数去要高度，一屏二十几行会把页面撑出去。
		s.viewSearchResultsBody(c, cur)
	})
}

// viewSearchToolbar 是 Tab 下面那一行：左边排序，右边「全部播放」。
//
// 排序是**每个 Tab 自己一套**（`st.sorts` 按 Tab 分格，见它的注释）。用户的
// Tab 没有「全部播放」——用户不是歌，播不了（上游的用户页也只有排序，没有那个
// 按钮，见 `pages/search/user-list/search-header.tsx`）。
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

// viewSearchResultsBody 是结果那一块。
//
// 三种情形，挨着判：
//
//	① 一行都还没有、而且**此刻没在拉** → 画「没有搜索结果」
//	② 正在拉第一页 → 列表自己画「正在加载…」（paged.go 的 syncList）
//	③ 其余 → 列表
//
// ⚠️ ①这一支**不看有没有出错**，照抄上游：上游的 `retryInitial` 失败时
// `addToast("加载失败")`，而 `list` 仍然是空的，于是下一句
// `!initialLoading && !list?.length` 成立，画的是同一个 `<Empty />`
// （`pages/search/video-list/index.tsx:181`）。也就是**搜不出来的时候用户
// 看见的是「换个词再搜」加一条 Toast**，不是一块错误页。
//
// 拉了半截之后失败是另一回事：那时 `Loaded > 0`，走 ③，末尾那一行由分页列表
// 自己给出「加载失败，点此重试」。
func (s *Shell) viewSearchResultsBody(c *ui.Context, tab int) {
	t := &s.search.lists[tab]
	pg := t.list.Paged()

	if pg.Loaded == 0 && !pg.Loading {
		s.viewEmpty(c, "search:no-results:"+strconv.Itoa(tab), noSearchResults)
		return
	}
	t.list.View(c)
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
