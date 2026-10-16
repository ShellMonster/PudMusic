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
		Hint:  "音乐榜和分区推荐还没有接进来",
	}},
	// 上游：关注列表只转给 `components/virtual-grid-page-list`，空的时候是它
	// 的兜底 `暂无内容`（index.tsx:160）。
	{Match: "/follow", Title: "我的关注", InMenu: true, Empty: emptyState{
		Title: "暂无内容",
		Hint:  "登录之后关注的 UP 主会出现在这里",
	}},
	// 上游：`pages/later/index.tsx:236`。
	{Match: "/later", Title: "稍后再看", InMenu: true, Empty: emptyState{
		Title: "暂无稍后再看内容",
		Hint:  "登录之后点过「稍后再看」的歌会出现在这里",
	}},
	// 上游：`pages/history/index.tsx:284`。
	{Match: "/history", Title: "历史记录", InMenu: true, Empty: emptyState{
		Title: "暂无历史记录",
		Hint:  "登录之后听过的歌会出现在这里",
	}},
	// 上游：`pages/local-music/index.tsx:263`。
	{Match: "/local-music", Title: "本地音乐", InMenu: true, Empty: emptyState{
		Title: "暂无内容",
		Hint:  "用顶栏的「打开文件」把本地音乐加进来",
	}},
	// 上游：`pages/download-list/index.tsx:155` 的 `emptyContent`。
	{Match: "/download-list", Title: "下载记录", InMenu: true, Empty: emptyState{
		Title: "暂无内容",
		Hint:  "下载过的音乐会出现在这里",
	}},
	// 设置页在上游不是一张内容页，是一长串表单，**没有空状态可抄**。这里直说
	// 设置现在在哪儿：主题档位还挂在顶栏下面那一条上（settings.go 的
	// viewThemeSwitch 自己就是这么写的），搬完才回这一页。
	//
	// 另外 Biu 把设置放在头像下拉里（N-08），这里先挂侧栏底部，等头像那块
	// 做出来再挪回去。
	{Match: "/settings", Title: "设置", InMenu: true, Empty: emptyState{
		Title: "设置项还在顶栏下面",
		Hint:  "主题档位就在顶栏下面那条上切，设置页搬完就回这里",
	}},
	// 上游：`pages/search/index.tsx:19`，没输入搜索词时落的就是共享默认。
	{Match: "/search", Title: "搜索", Empty: emptyState{
		Title: "暂无内容",
		Hint:  "输入歌名或 UP 主就能搜",
	}},
	// 上游：动态流作者栏是 `暂无关注`（`dynamic-feed/author-list.tsx:137`），
	// 主列表那处上游写的是没翻译的 `Empty`（`dynamic-feed/index.tsx:184`），
	// 明显是漏了。同一句中文上游在 `user-profile/dynamic-list/index.tsx:80`
	// 是有的，抄那一处。
	{Match: "/dynamic-feed", Title: "用户动态", Empty: emptyState{
		Title: "暂无动态",
		Hint:  "关注的 UP 主发了新动态会出现在这里",
	}},
	// 上游：`pages/user-profile/dynamic-list/index.tsx:80`。
	{Match: "/user/{id}", Title: "UP 主空间", Empty: emptyState{
		Title: "暂无动态",
		Hint:  "这个 UP 主还没有发过动态",
	}},
	// 上游：收藏夹 / 合集 / 系列都落到 `components/virtual-page-list` 的兜底
	// `暂无内容`（index.tsx:146）——收藏夹自己一个字都没有。
	{Match: "/collection/{id}", Title: "收藏夹 / 合集 / 系列", Empty: emptyState{
		Title: "暂无内容",
		Hint:  "这里还没有收藏",
	}},
	// 空状态落地页（P-29）。上游 `pages/empty/index.tsx:6` 在这里画的是
	// `<Empty />`，也就是共享默认的 `暂无内容`——上游这一页整个就是一张空状态，
	// 除此之外什么也没有。我们这里挂的是**歌词**那一支（`暂无歌词`）：上游唯一
	// 一条不来自共享默认的空状态文案，放在这一页上正好是它该在的地方，也让任务
	// 点名的「没有歌词」这个场景在应用里真的画得出来。歌词面板是阶段 6 的东西，
	// 到时候把这一件搬过去，这一页退回共享默认。
	{Match: "/empty", Title: "空状态", Empty: lyricsEmpty},
	// 上游：`pages/mini-player/index.tsx:128`。
	{Match: "/mini-player", Title: "迷你播放器", Empty: emptyState{
		Title: "暂无播放内容",
		Hint:  "还没有歌在放",
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

// renderPage 是画页面内容的那一支（New 把它装成 pageBody，测试也正是替掉它
// 来验「页面里 panic 进程不崩」）。
//
// 13 条路由走同一条路：这一页自己的空状态。真正的内容是后面的阶段接进来的，
// 接的时候把 viewEmpty 换成真列表就行，外面的 key、recover、主题一个字不动。
func (s *Shell) renderPage(c *ui.Context, r *ui.Route, rt Route) {
	if rt == notFound {
		s.viewNotFound(c, r.Path())
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
// ⚠️ 这里**不再**弹「这一页还没做」了：13 条路由都有页面视图了（Biu 那边也是
// 切过去就有页面），再弹一条等于用一句占位话盖住真页面。
func (s *Shell) navigate(target string) {
	s.clearToasts()
	s.crash = nil
	s.router.Push(target)
}

// viewSide 是左侧栏：品牌 + 菜单。
//
// Biu 的侧栏展开 200px、折叠 72px，右缘还有一条可拖的把手（SD-01/SD-02）。
// 那些是后面的事，这里先摆一个固定宽度的。
//
// ⚠️ 必须自己拉满高：Row 默认把子元素**居中**跨轴摆放（`Align` 默认不是
// Stretch），不写 `AlignSelf(Stretch)` 的话这一栏会按内容高度浮在中间。
func (s *Shell) viewSide(c *ui.Context) {
	t := c.Theme()
	cur := s.router.Path()

	ui.Column(c).Key("side").Width(200).AlignSelf(ui.Stretch).
		Padding(12).Gap(4).Background(t.Surface).Children(func() {
		// 红绿灯那一条已经被顶栏让出来了，侧栏从它下面开始，不用再顶开。
		ui.Text(c, "Biu").FontSize(18).Bold().Padding(8, 8, 12, 8)

		// ⚠️ Key 给的是这个 Column，不是里面每个 Button —— 构造之后再 Key
		// 的坑见铁律四。菜单项之间靠顺序区分，本来也不需要 key。
		ui.Column(c).Key("side-menu").Gap(2).Children(func() {
			for _, rt := range routes {
				if !rt.InMenu {
					continue
				}
				item := ui.Button(c, rt.Title).Grow(1)
				if rt.Match == cur {
					item = item.Background(t.Accent)
				}
				if item.Clicked() {
					s.navigate(rt.Match)
				}
			}
		})

		ui.Column(c).Grow(1)
		ui.Text(c, "Biu MyGo").FontSize(11).TextColor(t.TextMuted).Padding(8)
	})
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
