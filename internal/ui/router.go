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
//
// ⚠️ 本阶段只做**占位**：切得动、看得见「还没做」就行。内容是后面阶段的事。
type Route struct {
	// Match 是匹配用的模式，`{id}` 是路径参数。这里写 MyGo 的写法，Biu 写的是
	// `:id`（`src/routes.tsx`），指的是同一件事。
	Match string
	// Title 是 Biu 里这一页的名字，占位页、Toast 和导航栏上都写它。
	Title string
	// InMenu 的进侧栏的默认菜单组（Biu 的 side/default-menu，SD-04）。
	// 参数路由和 404 不进：它们是从别的页面点进来的，不是菜单项。
	InMenu bool
}

// routes 是那 14 条。前面 InMenu 的六条，顺序就是侧栏菜单从上到下的顺序
// （照 Biu 的 SD-04：推荐音乐 / 我的关注 / 稍后再看 / 历史记录 / 本地音乐 /
// 下载记录）。
var routes = []Route{
	{Match: "/", Title: "推荐音乐", InMenu: true},
	{Match: "/follow", Title: "我的关注", InMenu: true},
	{Match: "/later", Title: "稍后再看", InMenu: true},
	{Match: "/history", Title: "历史记录", InMenu: true},
	{Match: "/local-music", Title: "本地音乐", InMenu: true},
	{Match: "/download-list", Title: "下载记录", InMenu: true},
	// Biu 把设置放在头像下拉里（N-08）。占位阶段先挂侧栏底部，等头像那块
	// 做出来再挪回去。
	{Match: "/settings", Title: "设置", InMenu: true},
	{Match: "/search", Title: "搜索"},
	{Match: "/dynamic-feed", Title: "用户动态"},
	{Match: "/user/{id}", Title: "UP 主空间"},
	{Match: "/collection/{id}", Title: "收藏夹 / 合集 / 系列"},
	{Match: "/empty", Title: "空状态"},
	{Match: "/mini-player", Title: "迷你播放器"},
}

// notFound 是兜底那一条：路径谁都不匹配就是它，对应 Biu 的 P-30。
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
func (s *Shell) viewRoutes(c *ui.Context) {
	s.router.View(c, func(r *ui.Route) {
		switch {
		case r.Match("/"):
			s.page(c, r, routes[0])
		case r.Match("/follow"):
			s.page(c, r, routes[1])
		case r.Match("/later"):
			s.page(c, r, routes[2])
		case r.Match("/history"):
			s.page(c, r, routes[3])
		case r.Match("/local-music"):
			s.page(c, r, routes[4])
		case r.Match("/download-list"):
			s.page(c, r, routes[5])
		case r.Match("/settings"):
			s.page(c, r, routes[6])
		case r.Match("/search"):
			s.page(c, r, routes[7])
		case r.Match("/dynamic-feed"):
			s.page(c, r, routes[8])
		case r.Match("/user/{id}"):
			s.page(c, r, routes[9])
		case r.Match("/collection/{id}"):
			s.page(c, r, routes[10])
		case r.Match("/empty"):
			s.page(c, r, routes[11])
		case r.Match("/mini-player"):
			s.page(c, r, routes[12])
		default:
			s.page(c, r, notFound)
		}
	})
}

// page 画一页，外面包一层页面级的 recover。
func (s *Shell) page(c *ui.Context, r *ui.Route, rt Route) {
	Guard(func(c *ui.Context) {
		r.Title(rt.Title)
		s.pageBody(c, r, rt)
	}, s.onPanic)(c)
}

// placeholder 是占位页的内容。**它不是最终的样子**，只是让这一页在切过去
// 之后看得见自己在这儿，以及「还没做」。
func (s *Shell) placeholder(c *ui.Context, r *ui.Route, rt Route) {
	t := c.Theme()
	ui.Column(c).Key("page:" + rt.Match).Grow(1).Padding(28).Gap(10).Children(func() {
		ui.Text(c, rt.Title).FontSize(22).Bold()
		ui.Text(c, "这一页还没做").FontSize(15).TextColor(t.Text)
		ui.Text(c, "阶段 1 只搭路由占位，内容在后面的阶段补。").FontSize(12).TextColor(t.TextMuted)
		ui.Text(c, r.Path()).FontSize(11).TextColor(t.TextMuted)
	})
}

// navigate 换页：先把上一页的提示全清掉，再跳过去，还没做的页面弹一条提示。
//
// 三件事是一起的，缺一条就不对：
//   - 清 Toast：上一页的提示不该挂在下一页上（验收项）。
//   - 清 crash：崩过一页之后切到别的页要能正常画，不是停在错误页里出不来。
//   - 弹 Toast：Biu 的验收是「点未实现的路由弹一条提示，不崩、不白屏」。
//     **已经有内容的那一页不弹**——对着一页真做出来的东西说「还没做」，
//     是在说一件假事，用户只会以为设置页也坏了。
func (s *Shell) navigate(target string) {
	s.clearToasts()
	s.crash = nil
	s.router.Push(target)

	rt := lookup(target)
	if rt.Title == notFound.Title {
		s.addToast("没有「" + target + "」这一页")
		return
	}
	if s.builtPage(rt) == nil {
		s.addToast("「" + rt.Title + "」这一页还没做")
	}
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
