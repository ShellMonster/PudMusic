package ui

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// Route 是一条路由。
//
// ⚠️ **口径照这一段的数来，别照旧注释里的数来**：Biu 的 `src/routes.tsx` 在
// Layout 里登记 12 条（`/`、`later`、`history`、`follow`、`collection/:id`、
// `user/:id`、`settings`、`download-list`、`dynamic-feed`、`local-music`、
// `search`、`empty`），加上独立窗口的 `mini-player` 共 13 条；我们另加了第 14 条
// `/login`（上游把登录挂在头像下拉里，我们给它一条路由）。兜底的 `*`（404）
// **不在 `routes` 表里**——它是 `notFound` 那一个单独的值，因为 404 不是一条能
// Match 的模式（见 `viewRoutes`）。
//
// 换句话说：`len(routes) == 14`，加上 `notFound` 一共 15 条（`TestFourteenRoutes`
// 钉的就是这个 15）。
type Route struct {
	// Match 是匹配用的模式，`{id}` 是路径参数。这里写 MyGo 的写法，Biu 写的是
	// `:id`（`src/routes.tsx`），指的是同一件事。
	Match string
	// Title 是 Biu 里这一页的名字，导航栏和窗口标题上都写它。
	Title string
	// InMenu 的进侧栏的默认菜单组（Biu 的 side/default-menu，SD-04）。
	// 参数路由和 404 不进：它们是从别的页面点进来的，不是菜单项。
	InMenu bool
	// Empty 是这一页没有内容时说的话。**每一条都得填**：留空就是这一页画出
	// 来一片白，那正是这一张要消掉的东西（见 empty.go）。
	Empty emptyState
}

// routes 是那 14 条。**InMenu 的七条**，顺序就是侧栏菜单从上到下的顺序：前六条
// 照 Biu 的 SD-04（推荐音乐 / 我的关注 / 稍后再看 / 历史记录 / 本地音乐 /
// 下载记录），第七条「设置」是我们自己多摆的（Biu 那边设置只从头像下拉进）。
//
// ⚠️ 「几条」这一段曾经写着「六条」，而表里当时就已经是七条了——`/settings`
// 早就标了 `InMenu: true`。写错的那几天里没有人去数，而**注释里数错了的条数
// 正是下一位读代码的人拿来判断「还差什么」的依据**。要数就数表，别数注释。
//
// 每条后面的注释是那句文案的**出处**：Biu 上游在哪儿写的，或者明写「上游没
// 有专属文案」。抄的时候照着抄，别自己编——编出来的一句在这一页对不上的话，
// 就等于把这一页从 Biu 搬过来的那件事做坏了。
var routes = []Route{
	// 上游：`pages/music-recommend/new-music-top.tsx:213` 的那块用 `暂无数据`，
	// 推荐页本身落到共享默认。
	//
	// ⚠️ 首页已经有自己的内容了（homepage.go 的三个 Tab + 新歌速递，B 站的推荐
	// 接口也接上了），但这一条的 `Empty` **仍然不是一句没人会看到的话**：它是
	// 首页在「一档都没拉到、**而且也没有失败**」时画的那一块（见
	// `homePage.viewTabBody` 的最后一支）——刚拉完 B 站给了空的，或者压根没挂
	// 上 B 站客户端，都落到它。**别照着「已经有内容的页不用 Empty」那条清掉**：
	// 清掉之后首页在拿不到数据时画出来是一片白，而且没有任何测试会红。
	//
	// ⚠️ 它**不是**「加载失败」那一句：失败走的是 `viewLoadError`（一句原因 +
	// 一个重试，见 empty.go 那段「为什么它不能是 viewEmpty 的一个空状态」）。
	// 两件事在界面上必须是两句话——用户读到「暂无内容」会以为 B 站没有推荐
	// 给他，于是永远不会去按那个本该在那儿的重试。
	{Match: "/", Title: "推荐音乐", InMenu: true, Empty: emptyState{
		Title: "暂无内容",
		Hint:  "先在顶栏「打开文件」放几首进来，或者搜一首歌",
	}},
	// ⚠️ 这一条**已经有内容了**（follow.go：左边的分组 Tab + 右边一个虚拟网格 +
	// 页内搜索，四个 B 站关注接口都接上了），所以下面这句是**页面自己里面**画的：
	// 「登录了、一格都没有、也没搜」那一支（`followEmpty`）读的就是它。
	// 出处留着是因为它还是那一句出处——上游 `follow-list/index.tsx` 只把列表转给
	// `components/virtual-grid-page-list`，空的时候是它的兜底 `暂无内容`
	//（index.tsx:160），所以 Title 照抄；Hint 是我们写的。
	//
	// ⚠️ **「还没登录」不是这一句**，是 follow.go 里的 `followNotLoggedIn`：没登录
	// 时那个接口一律回 -101，而说成「还没关注任何人」的话用户登录完发现还是空的，
	// 会以为是登录坏了。两句话必须分开（见 follow.go 文件头）。
	{Match: followPath, Title: "我的关注", InMenu: true, Empty: emptyState{
		Title: "暂无内容",
		Hint:  "登录之后点「关注」关注的 UP 主会出现在这里",
	}},
	// ⚠️ 稍后再看和历史记录已经有自己的内容了（later.go / history.go），但这两
	// 条的 `Empty` **不是一句没人会看到的话**：它们在自己的 view 里画的就是这
	// 一条（`pagedRows.SetEmpty`，走的是列表那一块而不是 `pageContent` 的兜底
	// ——兜底那一支会把上面那一排筛选一起换掉，而用户正是要靠那一排把条件改回去）。
	// 理由和上面首页、下面下载记录那两条一样，别照着「已经有内容的页不用 Empty」
	// 那条把它清掉。
	//
	// 上游：稍后再看 `pages/later/index.tsx:236`，历史记录
	// `pages/history/index.tsx:284`。
	{Match: laterPath, Title: "稍后再看", InMenu: true, Empty: emptyState{
		Title: "暂无稍后再看内容",
		Hint:  "登录之后在歌上点「稍后再看」，加进来的会出现在这里",
	}},
	{Match: historyPath, Title: "历史记录", InMenu: true, Empty: emptyState{
		Title: "暂无历史记录",
		Hint:  "登录之后听过的歌会出现在这里，也可以「清空历史记录」",
	}},
	// 上游：`pages/local-music/index.tsx:263`。
	{Match: "/local-music", Title: "本地音乐", InMenu: true, Empty: emptyState{
		Title: "暂无内容",
		Hint:  "用顶栏的「打开文件」把本地音乐加进来",
	}},
	// 「没有下载记录」，文案在 empty.go 的 noDownloads（上游出处也写在那儿）。
	{Match: "/download-list", Title: "下载记录", InMenu: true, Empty: noDownloads},
	// 设置页在上游不是一张内容页，是一长串表单，**没有空状态可抄**——而这一页
	// 也不是空页面：settings_page.go 已经把设置页本身接进 builtPage 了（主题、
	// 圆角、窗口关闭三项）。所以这一条**不填 Empty**：填了就是一句没人会看到的
	// 话，那一页根本走不到空状态那一支。
	//
	// 哪一页有内容，看 builtPage；哪一页空着，看这里的 Empty。两条都不给的那
	// 一条画出来是一片白，`TestEveryRouteHasAView` 会先红。
	//
	// 另外 Biu 把设置放在头像下拉里（N-08），这里先挂侧栏底部，等头像那块
	// 做出来再挪回去。
	{Match: "/settings", Title: "设置", InMenu: true},
	// 搜索页有搜索框和两个空状态（searchpage.go），所以**不填 Empty**：填了就是
	// 一句没人会看到的话，那一页根本走不到空状态那一支。
	{Match: "/search", Title: "搜索"},
	// ⚠️ 这一条已经接上 `builtPage`（dynamicfeed.go），所以下面这两句空状态
	// **不是留着没用**——是页面在「右边一个格子都没有」时**直接读它**
	// （见 `feedEmpty`）。出处标在这儿是为了让「这句话抄自哪里」只有一个地方。
	//
	// 上游：动态流作者栏是 `暂无关注`（`dynamic-feed/author-list.tsx:137`），
	// 主列表那处上游写的是没翻译的 `Empty`（`dynamic-feed/index.tsx:184`），
	// 明显是漏了。同一句中文上游在 `user-profile/dynamic-list/index.tsx:80`
	// 是有的，抄那一处。
	{Match: "/dynamic-feed", Title: "用户动态", Empty: emptyState{
		Title: "暂无动态",
		Hint:  "去「我的关注」关注几个 UP 主，他们的动态会出现在这里",
	}},
	// ⚠️ 同上：这一条也接上了 `builtPage`（upspace.go），下面这句是「动态」
	// 那一张 Tab 空着时说的话（见 `upTabEmpty`）。出处同上。
	//
	// 上游：`pages/user-profile/dynamic-list/index.tsx:80`。
	{Match: "/user/{id}", Title: "UP 主空间", Empty: emptyState{
		Title: "暂无动态",
		Hint:  "回首页听歌，或者换一个 UP 主的空间看看",
	}},
	// 收藏夹 / 合集 / 系列**现在有内容了**（collectionpage.go：头图区 +
	// 列表 + 批量操作条），所以这一条**不填 Empty**：填了就是一句没人会看到的
	// 话——这一页走不到空状态那一支（`pageContent` 里 builtPage 先命中）。
	//
	// 「没有收藏夹」那句并没有因此作废，它现在画在**这一页自己里面**：
	// 容器拉回来了、里面一条都没有的时候（见 viewCollection 里那一支），
	// 文案还是 `empty.go` 的 noFavorites，上游出处也在那儿。
	{Match: collectionMatch, Title: "收藏夹 / 合集 / 系列"},
	// 空状态落地页（P-29）。上游 `pages/empty/index.tsx:6` 画的就是一个
	// `<Empty />`——这一页整个的用处就是「让人看见空状态长什么样」，上游没给
	// title，所以落回共享默认的「暂无内容」。照抄。
	{Match: "/empty", Title: "空状态", Empty: emptyState{
		Title: "暂无内容",
		Hint:  "随便切到别的页面——每一页空着的时候都是这个样子",
	}},
	// 迷你播放器（nowplaying.go）：没歌时是上游的「暂无播放内容」，有歌时是歌名 +
	// 歌词区。所以**不填 Empty**：填了就是一句没人会看到的话。
	{Match: "/mini-player", Title: "迷你播放器"},
	// 扫码登录。**不加 InMenu**：它不是侧栏菜单项，是从顶栏的登录
	// 按钮进出来的（跟 Biu 把登录挂在头像下拉里是一回事）。
	// 加进菜单的话用户每次想登录都要先在侧栏里找一圈。
	{Match: "/login", Title: "扫码登录"},
}

// notFound 是兜底那一条：路径谁都不匹配就是它，对应 Biu 的 P-30。
//
// 它没有 Empty：这一页有自己的 view（notfound.go），说清楚是哪条路径不存在、
// 还剩几秒回首页，比一句「暂无内容」有用得多。
var notFound = Route{Match: "*", Title: "页面不存在"}

// lookup 按路径找出它属于哪一条路由，照 `{id}` 是通配的规矩走。
func lookup(path string) Route {
	p := "/" + strings.Trim(path, "/")
	for _, r := range routes {
		if matchPath(r.Match, p) {
			return r
		}
	}
	return notFound
}

// matchPath 报 path 是不是 pattern 的实例。`{x}` 是通配的一段，`{x...}`
// 是通配剩下的全部（这一版还没用到，但规则先摆在这）。
func matchPath(pattern, path string) bool {
	pp, sp := strings.Split(pattern, "/"), strings.Split(path, "/")
	if len(sp) == 0 || (len(pp) == 1 && pp[0] == "") {
		// "/" 匹配 "/"
		return len(sp) == 1 && sp[0] == ""
	}
	for i, seg := range pp {
		if i >= len(sp) {
			return false
		}
		if rest, ok := strings.CutPrefix(seg, "{"); ok && strings.HasSuffix(rest, "...") {
			return true
		}
		if seg == sp[i] {
			continue
		}
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			continue
		}
		return false
	}
	return len(pp) == len(sp)
}

// viewRoutes 是全部路由的分派。
//
// **每一页外面都包一层 recover**：MyGo 的 Dispatch 和 drain 都没有 recover，
// view 里 panic 会穿透 Cocoa 事件循环直接把进程带走。切路由时 recover 的
// 状态由 navigate 复位，所以一页崩了不会连累后面那些页。
//
// 认页用 `lookup(r.Path())`，不再是一长串 `r.Match(...)`：认页和画页合成一处，
// 表里加了一条就自动多一页，不会出现「表里有、switch 里忘了」的那种少一页。
//
// 画什么由 pageContent 决定（见下）：已经有内容的页走 builtPage，其余走这一页
// 自己的空状态。
func (s *Shell) viewRoutes(c *ui.Context) {
	// 换页的收口（404 那个 3 秒倒计时就在这儿驱动），必须在画页之前。
	s.syncRoute()

	s.router.View(c, func(r *ui.Route) {
		s.page(c, r, lookup(r.Path()))
	})
}

// page 画一页，外面包一层页面级的 recover。
func (s *Shell) page(c *ui.Context, r *ui.Route, rt Route) {
	Guard(func(c *ui.Context) {
		r.Title(rt.Title)
		s.pageBody(c, r, rt)
	}, s.onPanic)(c)
}

// builtPage 是「这一页已经有内容了」的路由和画法；还没做就返回 nil。
//
// 它同时是那几页不写空状态的理由：接了 builtPage 的路由，`Route.Empty` 就用不
// 上了，留着等于一句没人会看到的话（`TestEmptyStateCopySaysWhyAndHow` 会把这种
// 「既没内容、Empty 又是空的」路由揪出来）。
func (s *Shell) builtPage(rt Route) func(*ui.Context, *ui.Route) {
	switch rt.Match {
	case homePath:
		// 首页（homepage.go）：三个 Tab + 新歌速递。
		//
		// ⚠️ 这一页的空状态**在它自己里面**画（`viewHome` 里那一支 `viewEmpty`），
		// 不是走 `pageContent` 的兜底：上面那一排 Tab 要一直在，兜底那一支会把
		// Tab 一起换掉。所以 `Empty` 留在这儿是有用的，别当成「已经有内容了就该
		// 清空」——`TestTheHomePageStillSaysWhyItIsEmpty` 钉的就是它。
		return s.viewHome
	case settingsPath:
		return s.viewSettings
	case searchPath:
		// 搜索页有搜索框，两个空状态从 URL 上的 q 分得出来（searchpage.go）。
		return s.viewSearch
	case miniPlayerPath:
		// 正在放什么 + 它的歌词（nowplaying.go）。
		return s.viewNowPlaying
	case downloadListPath:
		// 下载记录（downloadlist.go）：表格 + 三段进度 + 五种操作 + 确认弹窗。
		//
		// ⚠️ 它和首页一样，**空状态画在自己里面**（一条记录都没有时画
		// `routes` 表里 `/download-list` 那一条），所以那条的 `Empty` 仍然
		// 有用，别当成「已经有内容了就该清掉」——`TestTheEmptyListStillSaysWhyItIsEmpty`
		// 钉的就是它。
		return s.viewDownloadList
	case loginPath:
		// 扫码登录（login.go）。
		return s.viewLogin
	case collectionMatch:
		// 歌单内容页：收藏夹 / 合集 / 系列（collectionpage.go）。
		return s.viewCollection
	case upPath:
		// UP 主空间：头图 + 关注 + 统计 + 四个 Tab（upspace.go）。
		//
		// ⚠️ 这是**参数路由**：`upPath` 是 `"/user/{id}"`，那一条的
		// `Route.Empty` 从此用不上了——留着等于一句没人会看到的话
		// （`TestEmptyStateCopySaysWhyAndHow` 会把这种「既没内容、Empty
		// 又是空的」路由揪出来）。空状态在 upspace_tabs.go 里按 Tab 分。
		return s.viewUpSpace
	case dynamicFeedPath:
		// 用户动态：左边关注的人、右边动态（dynamicfeed.go）。
		return s.viewDynamicFeed
	case laterPath:
		// 稍后再看：关键词 + 日期范围 + 会自己翻页的列表（later.go）。
		return s.viewLater
	case historyPath:
		// 历史记录：同一副骨架，多一列播放进度（history.go）。
		return s.viewHistory
	case localMusicPath:
		// 本地音乐（localmusic.go）：表格 / 列表 / 网格三档 + 每行的
		// 「打开文件夹」和「删除文件」。
		return s.viewLocalMusic
	case followPath:
		// 我的关注（follow.go）：左边分组 Tab + 右边虚拟网格 + 页内搜索。
		//
		// ⚠️ **空状态在它自己里面画，而且分五种**：「没登录」「没关注任何人」
		// 「搜了没搜到」「这一组还没有人」「这一块拉失败了」（见 follow.go 文件头）。
		// 所以上面 `routes` 表里那一条的 `Empty` 仍然有用——它就是
		// 「没关注任何人」那一支，别当成「已经有内容了就该清掉」。
		return s.viewFollow
	}
	return nil
}

// pageContent 画一页的内容（New 把它装成 pageBody，测试也正是替掉它来验
// 「页面里 panic 进程不崩」）。
//
// 三样，谁先谁后：
//   - **404**：它有自己的页（notfound.go），说清楚是哪条路径不存在、还剩几秒回
//     首页。比一条 Toast 有用得多——Toast 两秒就没了，而 404 上那一页一直在。
//   - **已经有内容的页**：settings_page.go 的 builtPage 把它自己的 view 接
//     进来（设置页是第一个）。接进来的页不用再走空状态那一支。
//   - **别的**：这一页自己的空状态——routes 表里每一条都带一句「这里为什么空
//     / 怎么才能不空」，出处标在那儿（见 empty.go）。真正的内容是后面的阶段
//     接进来的，接的时候在这一支前面加一个 builtPage 就行，外面的 key、
//     recover、主题一个字不动。
//
// 三样都空不了：一条路由既没接上 builtPage、Empty 又是空的，那一页画出来就是
// 一片白，`TestEveryRouteHasAView` 会先红。
func (s *Shell) pageContent(c *ui.Context, r *ui.Route, rt Route) {
	if rt == notFound {
		s.viewNotFound(c, r.Path())
		return
	}
	if body := s.builtPage(rt); body != nil {
		body(c, r)
		return
	}
	s.viewEmpty(c, "page:"+rt.Match, rt.Empty)
}

// navigate 换页：先把上一页的提示全清掉，再跳过去。
//
// 两件事是一起的，缺一条就不对：
//   - 清 Toast：上一页的提示不该挂在下一页上（验收项）。
//   - 清 crash：崩过一页之后切到别的页要能正常画，不是停在错误页里出不来。
//
// ⚠️ 这里**一条 Toast 都不弹**了（原型那版会弹「这一页还没做」和
// 「没有…这一页」）：
//
//   - 「还没做」：`routes` 那 14 条里**已经有 13 条有自己的画法**（`builtPage`），
//     加上兜底的 404 那一页，一共 14 页有内容；剩下那一条走的是这一页自己的空状态
//     ——`/empty`（它就是空状态落地页，按设计该空）。对着其中任何一页说「这一页
//     还没做」都是在说一件假事，用户只会以为这一页坏了。
//
//     ⚠️ **这个 13 是算出来的**：上一轮把 `/follow` 接上画法之后，`main.go` 的抬头、
//     README 和**这一段**还都留着接上之前的那个数，也还把 `/follow` 列在「没有
//     画法」那一边——而全仓没有任何一条测试读这三处，所以那三句假话一路留着。
//     `TestTheCountsInTheProseMatchTheCountsInTheCode` 就是为这件事加的：它拿
//     `builtPage` 数一遍真值，再回头核这三处散文。
//
//   - 「没有…这一页」：404 页自己就写着是哪条路径不存在、几秒后回首页，一条
//     两秒就消失的 Toast 反而盖住它。
func (s *Shell) navigate(target string) {
	s.clearToasts()
	s.crash = nil
	s.router.Push(target)
}
