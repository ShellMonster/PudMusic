// 搜索页：Biu 的 `/search`（`src/pages/search/`）。
//
// 三个 Tab 的结果（**歌曲 / 视频 / 用户**），每个 Tab 一套自己的排序、一份自己的
// 分页列表。拉页、防重入、取消、自动续拉、回顶全是 `widgets/list` 那一层的事，
// 这一页只管摆；状态、查询、三个反直觉行为在 searchresults.go。
//
// # 这一页**没有输入框**，也没有「搜索」那一行标题
//
// 2026-10-22 人定的（`DESIGN.md` §十.1）：结果页不再放第二个输入框，改词回顶栏
// 那个 280 宽的框就行；上游那一行「搜索」标题也不加（上游那一页只有一句
// `<h1>搜索【{keyword}】的结果</h1>`，`src/pages/search/index.tsx:16-26`）。
//
// 这一条以前是**只改了 DESIGN.md 没改代码**，于是页面上摆着两个框：顶栏那个
// （显示占位「搜索」，空的）和页内那个 420 宽的（装着刚搜的词）——两个框既不
// 左对齐也不右对齐，在屏幕上错开（审计 F-01 / UX B1）。
//
// ⚠️ **删掉页内那个框之后，「当前搜的是什么」在界面上只剩顶栏一个显示**——
// 顶栏那个框里必须显示当前这个词（`navbar_search.go` 的 `syncSearchKeyword`），
// 否则框空着而下面已经是一屏结果，用户会以为框坏了。
//
// 🔴 本文件里的 view **不许**调 `c.SetTheme`：主题每帧开头装一次就够（铁律二，见
// theme.go）。
package ui

import (
	"errors"
	"strconv"
	"strings"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
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
//
// ⚠️ **提示指着顶栏那个框**：页内那个输入框 2026-10-22 拍板删了（见文件头），
// 而这一页能搜的地方只剩顶栏——「在上面的搜索框里」指的是一个已经不存在的框。
var searchIdle = emptyState{
	Title: "暂无内容",
	Hint:  "在顶栏的搜索框里输入歌名或 UP 主",
	Icon:  iconEmptySearch,
}

// searchHeaderGap 是页头那几块（标题、Tab、排序）之间的距离。
// Biu 那边是 `py-4`（`pages/search/index.tsx:26`）。
const searchHeaderGap = space.Px12

// viewSearch 是搜索页：当前状态该画的那一块。
//
// 🔴 关键词**问 `r`，不要问 `c.Router()`**：`r` 是**这一页自己**那条 entry，
// 而 `c.Router()` 是路由器的**当前**位置——两者在「正在画的不是当前那一页」时
// 不是一回事。
//
// ⚠️ **为什么这条规矩不能因为「我们不做换页动画了」就丢掉**：框架建离场页时
// **故意不设 `c.router`**（`mygo/ui/router.go:685`），那一帧 `c.Router()` 是空指针，
// `c.Router().Query` 直接把整个窗口换成错误页。我们现在显式设了
// `TransitionNone`（`router.go` 的 `newRouter`），离场页不会被重建，所以那个坑
// 今天踩不到；**但哪天真把换页动画开回来，它立刻就回来**——而症状是「整个窗口
// 变成错误页」，不是「某一页坏了」。
func (s *Shell) viewSearch(c *ui.Context, r *ui.Route) {
	// 🔴 铁律二：页面 view 的头一件正事就是装主题（见 theme.go）。
	s.applyFrameTheme(c)

	keyword := strings.TrimSpace(r.Query(searchQuery))

	// ⚠️ **横向那一格用 `pagePadX`**（七页共用一个数，见 pagerhythm.go）。
	ui.Column(c).Key("page:/search").Grow(1).Padding(space.None, pagePadX).Gap(searchHeaderGap).
		AlignItems(ui.Stretch).Children(func() {
		if keyword == "" {
			// 还没输入：上游 `pages/search/index.tsx:16` 的那一支。
			s.viewEmpty(c, "search:idle", searchIdle)
			return
		}
		s.viewSearchResults(c, keyword)
	})
}

// syncSearch 是搜索页每帧的副作用：换查询就重置到第 1 页、收当前那一张 Tab 的
// 后台结果、起该起的那一拉。
//
// ⚠️ **它们原来散在 `viewSearch` 和 `viewSearchResults` 里**，而这一页在满屏
// 开着的时候不画（票 #161）——不挪的话，用户在满屏里看频谱的那几分钟里，搜索
// 结果的后台数据一直躺在信箱里没人取。
//
// ⚠️ **只同步当前那一张 Tab**：另外两张的列表**从来没打开过**，而
// `PagedList.Sync` 里那一支「一页都还没拉过就发第一拉」会把它们也拉起来——
// 那是用户根本没要的三趟请求。
//
// ⚠️ **草稿那一摊不在这里了**：页内那个输入框 2026-10-22 拍板删了之后，要跟着
// 路由走的那个词在**顶栏**那个框里（`navbar_search.go` 的 `syncSearchKeyword`）。
func (s *Shell) syncSearch(c *ui.Context) {
	st := &s.search
	keyword := strings.TrimSpace(s.router.Query(searchQuery))

	cur := st.tab
	// 🔴 换查询 → 重置到第 1 页（理由见 searchTab.current）。
	//
	// ⚠️ **判的是上一帧的那张 Tab**：这一层的副作用跑在画的前面，而 Tab 是
	// 画的时候才写的。画的那一半里还有一次 `current`（用这一帧的 Tab），两次
	// 各管各的：`current` 自己认得出「已经重置过了」，所以跑两遍不重来。
	st.lists[cur].current(searchQuerySpec{keyword: keyword, sort: st.sorts[cur]})

	// 🔴 设置里的「数据显示样式」在这一帧生效（`s.displayView` 的注释写了为什么
	// 每帧都要调）。
	st.lists[cur].list.SetView(s.displayView())
	st.lists[cur].list.SetHighlight(keyword)

	// 收后台投回来的结果、起该起的那一拉。
	st.lists[cur].list.Sync(c)

	// 念出去：「这一场搜索出结果了」（无障碍，见 announce.go）。
	s.announceSearchResults(cur)
}

// announceSearchResults 在「这一场搜索的结果出来了」的时候念一句。
//
// 🔴 **读屏软件用户看不见那些结果**：他在搜索页上，而结果是一屏列表——他得一行一行
// 走才知道有什么。而「搜完了、一共 N 个」这件事本身**不是画面上的任何一行**
// （页标题写的是「搜索【X】的结果」，那说的是搜的是啥，不是搜到了几个），所以
// 得单独说一句。
//
// ⚠️ **一场只念一次**：翻页会一直加行，而每加一行都念一遍就成了报数。
//
// ⚠️ **报的是命中的总数，不是屏幕上那几行**：见 `searchTab.total` 的注释。
//
// ⚠️ **「一个都没有」不在这儿念**：那一刻（还没开始拉）界面上的状态和「搜完了
// 一个都没有」**长得一模一样**（都是「没有结果」那一格），分不出来——所以这一句
// 只在**拿到了结果**的时候说。
func (s *Shell) announceSearchResults(tab int) {
	t := &s.search.lists[tab]
	pg := t.list.Paged()
	if pg.Loading || pg.Loaded == 0 {
		return
	}
	if t.announced {
		return
	}
	t.announced = true
	n := t.searchTotal()
	if n == 0 {
		// B 站没给总数（或者这一趟还没回来）：拿已经拿到的那些凑一个数，
		// 宁可少说也别说错。
		n = pg.Loaded
	}
	s.announce("找到 " + strconv.Itoa(n) + " 个结果")
}

// viewSearchResults 是搜了词之后的那一块：标题 + 三个 Tab + 排序行 + 结果。
// 摆的顺序照上游（`pages/search/index.tsx:23-58`）。
func (s *Shell) viewSearchResults(c *ui.Context, keyword string) {
	st := &s.search

	ui.Column(c).Key("search:results").Grow(1).Gap(searchHeaderGap).
		AlignItems(ui.Stretch).Children(func() {
		// 这一句是这一页的**页标题**（上游的 `<h1>`，`pages/search/index.tsx:23`）：
		// 24 / 400。
		//
		// ⚠️ **字重不许写**：页标题的字重由框架的默认档（400）给，写上
		// `FontWeight(400)` 也会被 `TestPageTitlesAreNotBold` 拦下——那一条钉的
		// 是「拿 `scale.PageTitle` 当字号的那一句里不许出现字重」，
		// `DESIGN.md` §二「页标题 24 / 400」靠的是**不调** `.Bold()`。
		ui.Textf(c, "搜索【%s】的结果", keyword).FontSize(scale.Font(c, scale.PageTitle))

		// 🔴 **两排 Tabs 都要在 `current` 之前画**：它们都是建到的时候才写
		// `st.tab` / `st.sorts[cur]`（`mygo/ui/base.go` 的 `tabsParts.Tab` 的
		// `choose`：写指针 + 把这一帧标记为重建）。顺序反过来的表现是：用户点了
		// 「最多播放」，这一帧的 `current` 读到的还是旧排序，这一帧画的还是旧
		// 顺序，要到**下一帧**才重置回第 1 页——屏幕上就是「按了排序，画面先抖
		// 一下才换」，而中间那一帧还滚回了顶上，用户刚滚到的位置没了。
		//
		// ⚠️ **这一排是主 Tab（轨道 + 滑块那一种）**，下面 `viewSearchToolbar`
		// 里那一排排序是次级的（框架那一条下划线）——两排不是同一个样子
		//（票 #138）。
		//
		// ⚠️ `AlignSelf(ui.Start)`：外面这个 Column 是 `AlignItems(Stretch)`，
		// 不拦一下的话轨道会被拉成整个内容区的宽，而上游那一条是**包着字的**
		//（`inline-flex`）。其余三处父容器本来就是 Start，不用这一句。
		mainTabs(c, &st.tab, tabMD, searchTabNames[:]...).AlignSelf(ui.Start)
		cur := st.tab
		s.viewSearchToolbar(c, cur)

		// 🔴 换查询 → 重置到第 1 页（理由见 searchTab.current）。
		st.lists[cur].current(searchQuerySpec{keyword: keyword, sort: st.sorts[cur]})

		// 列表要吃掉剩下的高度，自己占 Grow(1)（见 list.go 的 View）。外面这一
		// 层 Grow(1) 是给它留地方：直接摆在根上不给高度的话，它会按自己的行数
		// 去要高度，一屏二十几行会把页面撑出去。
		s.viewSearchResultsBody(c, cur, keyword)
	})
}

// viewSearchToolbar 是 Tab 下面那一行：左边排序，右边「全部播放」。排序是**每个
// Tab 自己一套**（`st.sorts` 按 Tab 分格）。用户的 Tab 没有「全部播放」——用户不是
// 歌，播不了（上游的用户页也只有排序，见 `user-list/search-header.tsx`）。
func (s *Shell) viewSearchToolbar(c *ui.Context, tab int) {
	st := &s.search

	ui.Row(c).Key("search:toolbar:" + strconv.Itoa(tab)).FillWidth().Gap(space.Px12).
		AlignItems(ui.Center).Children(func() {
		ui.Tabs(c, &st.sorts[tab], searchSortLabels(tab)...)

		if tab == searchTabUser {
			return
		}
		// 「全部播放」：Biu 的 `AsyncButton`（`video-list/search-header.tsx:55`），
		// 它禁用的条件是 `initialLoading || list.length === 0`——正在装第一页
		// 或者一首都没有时不让按。
		//
		// ⚠️ **这一枚和首页那一枚是同一枚**（`playAllButton`，票 #244 / UX 报告
		// §2 的 B6）：同一个动作在两个页面上各有一枚入口，此前**只有搜索页有**——
		// 用户在一个页面能按、在另一个页面找不到，会以为是两个不同的功能。
		//
		// ⚠️ **位置和首页不一样是跟上游一致**：上游搜索页那一行右边还有一个
		// 「仅音乐分区」的开关（`justify-between` 把它们推到两头），我们没有那个
		// 开关，所以这一枚留在排序 Tab 右边（票 #288 只点了首页那一行）。
		pg := st.lists[tab].list.Paged()
		if playAllButton(c, pg.Loading || pg.Loaded == 0).Clicked() {
			s.searchPlayAll(&st.lists[tab])
		}
	})
}

// viewSearchResultsBody 是结果那一块。四种状态四种样子。
//
//	正在拉   转圈（列表自己那一格）
//	拉失败   一句原因 + 一个「重试搜索」
//	真的没有 「换个词再搜一次」
//	有结果   列表
//
// ⚠️ **「拉失败」和「没有结果」必须是两件事**（票 #455 / 报告 246 第 27 条）：
// 把失败画成「没有结果」，用户读到的是「B 站没有这个结果，去换个词」——而真实
// 原因可能是网络断了、登录态过期、B 站风控。「换个词」这个建议在网络故障时是
// 错的，而且那一屏**没有任何办法重来**。
//
// ⚠️ **失败走共用的 `viewLoadError`**，不是 `viewEmpty` 的一个空状态：那一格自带
// 一句原因和一个重试按钮（理由在 `empty.go` 顶上那三条）。首页那三个 Tab 拉失败
// 时走的也是它（`homepage.go` 的 `viewTabBody`）——同一个应用里同一个毛病只有
// 一种画法。
//
// ⚠️ **手上已经有结果时不画失败**（`pg.Loaded == 0` 那一半）：那会把用户正在看
// 的一屏换成一句错。翻了半页之后失败是另一回事——那时末尾那一行由分页列表自己
// 给出「加载失败，点此重试」。
//
// ⚠️ **「正在加载」那一格是列表自己画的那一格**（画法是外面递进去的，见
// `loadingCell` 和 `searchresults.go` 的 `init`）：它原来是一句 13pt 淡灰缩在
// 左上角，而「没有结果」是居中 15pt 两行。等的那几秒里那行字缩在角落里，回来
// 后忽然变大、居中、还多了一行（票 #138）。
//
// ⚠️ **加载的时候列表照画，不 return**：后台回来的结果是在列表的 `View` 里收的
// （`adopt`），不画它这一页就永远停在「正在加载」。
func (s *Shell) viewSearchResultsBody(c *ui.Context, tab int, keyword string) {
	t := &s.search.lists[tab]
	pg := t.list.Paged()

	if pg.Err != nil && pg.Loaded == 0 {
		retry := func() { t.list.Reload() }
		retryLabel := "重试搜索"
		if searchErr, ok := searchErrorOf(pg.Err); ok && !searchErr.Retryable {
			retry = nil
			retryLabel = ""
		}
		errText := searchErrorText(pg.Err)
		s.viewLoadError(c, "search:fail:"+strconv.Itoa(tab),
			"搜索「"+keyword+"」", retryLabel, errors.New(errText), retry)

		return
	}

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
	// 这一处自己给规格而不用 `PagedList.Header`：「用户」那一档的行里时长那一列
	// **没有字**（位置留着——行尾那枚「⋯」的位置跟着它走），理由见
	// `searchHeaderSpec`。
	if v := t.list.Layout(); v != list.ViewCard {
		// 列定义**问列表要**（`init` 里装的就是 `searchColumns`）：表头和行
		// 读同一份，只改一处时不会一边多一列、一边少一列。
		list.Header(c, searchHeaderSpec(tab, v, t.list.List().Columns(),
			t.list.List().HasSelection(), "search:header:"+strconv.Itoa(tab)))
	}
	t.list.View(c)
	s.noteScrollbar(t.list.Hot())
}

func searchErrorOf(err error) (*app.SearchError, bool) {
	var searchErr *app.SearchError
	return searchErr, errors.As(err, &searchErr)
}

func searchErrorText(err error) string {
	if searchErr, ok := searchErrorOf(err); ok {
		return notice.SearchErrorText(searchErr.Category)
	}
	return err.Error()
}

// searchHeaderSpec 是这一档 Tab 的表头。
//
// ⚠️ **「用户」那一档没有「时长」**：那一档的行是「序号 / 头像 / 昵称 / 个性签名」
// （见 `searchUserMusic` 的注释），`list.Music.Duration` 是零值，于是行里那一列
// **没有字**——**位置是留着的**（`widgets/list` 的 `viewCard` 无论有没有时长都留着
// 那么宽，因为行尾那枚「⋯」的位置跟着这一列走）——但表头上还写着「时长」的话，
// 用户读到的是一列空的表头，而空表头比没有表头更难解释（看不出那是没数据还是坏了）。
func searchHeaderSpec(tab int, v list.View, cols list.Columns, check bool, key string) list.HeaderSpec {
	spec := list.CardHeaderSpec(v, cols, check, key)
	if tab == searchTabUser {
		// 去掉最后那一列（「时长」）——`CardHeaderSpec` 的顺序是固定的，见它自己。
		spec.Cols = spec.Cols[:len(spec.Cols)-1]
	}
	return spec
}

// searchColumns 是这一档要多画哪两列（行和表头读的是同一份）。
//
// 歌曲 / 视频	播放量 + 投稿时间（应用 SearchItem.View / SearchItem.PubTime）
//
//	用户		两样都没有——用户不是视频，行里也不该凭空多出两列空的
//
// ⚠️ `Now` 传的是 `s.now`（读外壳那颗时钟的闭包），不是 `s.clock.Now`：
// 测试会把 `s.clock` 换成假时钟，而值在取的那一刻就把旧时钟定住了。
func searchColumns(tab int, s *Shell) list.Columns {
	if tab == searchTabUser {
		return list.Columns{}
	}
	return list.Columns{PlayCount: true, PubTime: true, Now: s.now}
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
