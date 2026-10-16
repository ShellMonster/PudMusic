package ui

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// Route 是 Biu 的一条路由。
//
// 14 条全在这儿登记，和 Biu 的 `src/routes.tsx` 一条一条对得上：12 条在
// Layout 里（`/`、`later`、`history`、`follow`、`collection/:id`、
// `user/:id`、`settings`、`download-list`、`dynamic-feed`、`local-music`、
// `search`、`empty`），加上独立窗口的 `mini-player`，再加上兜底的 `*`（404）。
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

// routes 是那 14 条。前面 InMenu 的六条，顺序就是侧栏菜单从上到下的顺序
// （照 Biu 的 SD-04：推荐音乐 / 我的关注 / 稍后再看 / 历史记录 / 本地音乐 /
// 下载记录）。
//
// 每条后面的注释是那句文案的**出处**：Biu 上游在哪儿写的，或者明写「上游没
// 有专属文案」。抄的时候照着抄，别自己编——编出来的一句在这一页对不上的话，
// 就等于把这一页从 Biu 搬过来的那件事做坏了。
var routes = []Route{
	// 上游：`pages/music-recommend/new-music-top.tsx:213` 的那块用 `暂无数据`，
	// 推荐页本身落到共享默认。
	{Match: "/", Title: "推荐音乐", InMenu: true, Empty: emptyState{
		Title: "暂无内容",
		Hint:  "先在顶栏「打开文件」放几首进来，或者搜一首歌",
	}},
	// 上游：关注列表只转给 `components/virtual-grid-page-list`，空的时候是它
	// 的兜底 `暂无内容`（index.tsx:160）。
	{Match: "/follow", Title: "我的关注", InMenu: true, Empty: emptyState{
		Title: "暂无内容",
		Hint:  "登录之后点「关注」关注的 UP 主会出现在这里",
	}},
	// 上游：`pages/later/index.tsx:236`。
	{Match: "/later", Title: "稍后再看", InMenu: true, Empty: emptyState{
		Title: "暂无稍后再看内容",
		Hint:  "登录之后在歌上点「稍后再看」，加进来的会出现在这里",
	}},
	// 上游：`pages/history/index.tsx:284`。
	{Match: "/history", Title: "历史记录", InMenu: true, Empty: emptyState{
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
	// 搜索页上游有**两个** Empty 分支：`pages/search/index.tsx:16` 的「还没输入
	// 搜索词」和 `pages/search/video-list/index.tsx:176` 的「搜了但没有结果」，
	// 画出来都是同一个 `<Empty />`（所以标题一样），接下来该做的事不一样。
	//
	// 这一页现在处在**第一个**分支——它还没有搜索框（搜索是阶段 5 的事）。所以
	// 这里填的是「去输入」那一句；「换个词再搜」那一句是 empty.go 的
	// noSearchResults，跟着搜索功能一起落地，别拿这一条冒充它。
	{Match: "/search", Title: "搜索", Empty: emptyState{
		Title: "暂无内容",
		Hint:  "在上面的搜索框里输入歌名或 UP 主",
	}},
	// 上游：动态流作者栏是 `暂无关注`（`dynamic-feed/author-list.tsx:137`），
	// 主列表那处上游写的是没翻译的 `Empty`（`dynamic-feed/index.tsx:184`），
	// 明显是漏了。同一句中文上游在 `user-profile/dynamic-list/index.tsx:80`
	// 是有的，抄那一处。
	{Match: "/dynamic-feed", Title: "用户动态", Empty: emptyState{
		Title: "暂无动态",
		Hint:  "去「我的关注」关注几个 UP 主，他们的动态会出现在这里",
	}},
	// 上游：`pages/user-profile/dynamic-list/index.tsx:80`。
	{Match: "/user/{id}", Title: "UP 主空间", Empty: emptyState{
		Title: "暂无动态",
		Hint:  "回首页听歌，或者换一个 UP 主的空间看看",
	}},
	// 「没有收藏夹」，文案在 empty.go 的 noFavorites（上游出处也写在那儿）。
	{Match: "/collection/{id}", Title: "收藏夹 / 合集 / 系列", Empty: noFavorites},
	// 空状态落地页（P-29）。上游 `pages/empty/index.tsx:6` 在这里画的是
	// `<Empty />`，也就是共享默认的 `暂无内容`——上游这一页整个就是一张空状态，
	// 除此之外什么也没有。照抄，不拿它当别的场景的替身。
	{Match: "/empty", Title: "空状态", Empty: emptyState{
		Title: "暂无内容",
		Hint:  "随便切到别的页面——每一页空着的时候都是这个样子",
	}},
	// 上游：`pages/mini-player/index.tsx:128`。
	{Match: "/mini-player", Title: "迷你播放器", Empty: emptyState{
		Title: "暂无播放内容",
		Hint:  "在主窗口里点「打开文件」，这边就会显示正在放什么",
	}},
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

// viewRoutes 是 14 条路由的分派。
//
// **每一页外面都包一层 recover**：MyGo 的 Dispatch 和 drain 都没有 recover，
// view 里 panic 会穿透 Cocoa 事件循环直接把进程带走。切路由时 recover 的
// 状态由 navigate 复位，所以一页崩了不会连累后面那些页。
//
// 认页用 `lookup(r.Path())`，不再是一长串 `r.Match(...)`：每页画什么已经由
// routes 表里的 Empty 决定了（见 empty.go），认页和画页合成一处，表里加了
// 一条就自动多一页，不会出现「表里有、switch 里忘了」的那种少一页。
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
//   - 「还没做」：14 条路由现在都有页面视图了——12 条是各自的空状态、设置页是
//     它自己那一页、404 是兜底那一页。对着其中任何一页说「还没做」都是在说一件
//     假事，用户只会以为这一页坏了。
//   - 「没有…这一页」：404 页自己就写着是哪条路径不存在、几秒后回首页，一条
//     两秒就消失的 Toast 反而盖住它。
func (s *Shell) navigate(target string) {
	s.clearToasts()
	s.crash = nil
	s.router.Push(target)
}

// viewNav 是顶部导航栏：后退/前进 + 当前页名 + 拖窗口的那一条。
//
// Biu 的 navbar（N-01~N-09）整条就是拖窗口的区（`window-drag`），搜索框聚焦
// 或用户下拉打开时才切成不拖。这里对应的是那条空白：拖动区只给那一块，按钮
// 还在外面，免得点按钮变成拖窗口。
func (s *Shell) viewNav(c *ui.Context) {
	// 后退/前进也是换页：上一页的提示同样不该留着，崩过的状态同样该复位。
	back := func() { s.clearToasts(); s.crash = nil }

	// ⚠️ 隐藏标题栏的窗口里，红黄绿是叠在内容上面的。左边让出 bar.Left、
	// 右边让出 bar.Right，才不会把它们压住。
	bar := c.TitleBar()

	ui.Row(c).Key("nav").Height(max(bar.Height, 32)).
		Padding(0, bar.Right+8, 0, bar.Left+8).
		Gap(8).AlignItems(ui.Center).Children(func() {
		if ui.BackButton(c, s.router).Clicked() {
			back()
		}
		if ui.ForwardButton(c, s.router).Clicked() {
			back()
		}
		ui.Text(c, lookup(s.router.Path()).Title).FontSize(15).Bold()
		ui.Column(c).Grow(1).DragWindow()
		// 阶段 1 唯一的入口：选一个本地文件。Biu 里对应侧栏的「本地音乐」。
		// 走的是播放条「加歌」那一条路：多选的文件全都进队列，用它打开的歌
		// 上一首/下一首才是亮的（直接调 app.Open 就进不了队列，见 pickAndEnqueue）。
		if ui.Button(c, "打开文件").Clicked() {
			s.pickAndEnqueue()
		}
	})
}
