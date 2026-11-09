// 首页：推荐音乐（原型 `PAGES.home`，`app.js` 538–583 行；DESIGN.md §0′.T2）。
//
// # 这一版摆什么
//
// 一整页往下滚，从上到下：
//
//	问候区     Eyebrow 日期 + 48 号宋体问候语（登录后带昵称）；右边「今天为你挑了 N 首」
//	           「本周已听 hh:mm」，暂停中且有进度时多一枚「继续播放「歌名」m:ss」
//	今日一张   封面取色和底色按 22% 混出的底、大封面、64 号宋体标题、艺人、两首曲目、
//	           「播放」「全部加入队列」（取法见 homepage_hero.go）
//	Tab        推荐 / 榜单 / 新歌（下划线样式），选中的那一档往下画
//
//	推荐   每日推荐（6 张卡片 + 「播放全部」）、最近播放（横向 rail）、你的收藏夹（登录后）
//	榜单   本周最热：大序号 + 封面 + 歌名 + 歌手和播放量 + 升降（homepage_chart.go）
//	新歌   新歌速递：曲目行，那一列写「x 天前上传」
//
// 旧的「发现总览」（横幅、四个入口磁贴、迷你榜、分档横滑）和六个翻页 Tab（音乐 / 鬼畜 /
// 流行 / 热榜 / 原创榜 / 最近播放）整个删掉了：底层接口（`app.Recommend.Feed` 的各档）
// 保留，界面入口只剩原型里有的这三档。
//
// # 取数
//
// 在 homepage_feed.go：每日推荐、本周最热、新歌速递、最近播放四摊，各取第一页。加载中画
// 骨架，出错画空状态加「重试」。
//
// 🔴 铁律二：这一页的 view 开头先 `applyFrameTheme`，不许把主题放到别处设。
// 页面级的 recover 在 `router.go` 的 `Shell.page` 上，这里不用再包一层。
package ui

import (
	"fmt"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/leading"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/title"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

// homePath 是首页那条路由，和 routes 表里登记的是同一个字符串。
const homePath = "/"

// 三个 Tab 的下标，顺序和文字照原型（`PAGES.home` 的 `tabs`）。
const (
	homeTabRec = iota
	homeTabChart
	homeTabNew
)

// homeTabLabels 是 Tab 上的字。
var homeTabLabels = [...]string{"推荐", "榜单", "新歌"}

// 各摊拿几首：原型每日推荐 6 张、最近播放 10 张、榜单 20 行。
const (
	homeDailyCount  = 6
	homeRecentCount = 10
	homeChartCount  = 20
)

// homePage 是首页这一屏自己的状态。建一次，每一帧调 view。
type homePage struct {
	// acts 是这一屏会做的操作（播放、加入队列）。
	acts list.Actions
	// cache 是封面缓存（整个窗口共用一个）。
	cache *imgcache.Cache

	// feed 是取数状态和信箱，见 homepage_feed.go。
	feed  homeFeed
	loads homeLoads

	// tab 是选中的 Tab，`pud.Tabs` 直接写它。
	tab int

	// contentW 是内容区宽（上一帧量到的，0 = 还没量到）。
	contentW float32
	// scroll 是整页的滚动位置。
	scroll ui.ScrollState

	// week / weekAt 是「本周已听」的缓存：统计快照是整份值拷贝，不该每帧取一次。
	week   time.Duration
	weekAt time.Time

	// win 是窗口宽（这一帧的）：原型的媒体查询看的是窗口，不是内容区，见 `homeLayoutFor`。
	win float32

	// hero / heroFor / heroOK 是「今日一张」选出来的那一份和它的依据（见 `heroSongs`）。
	hero    []list.Music
	heroFor heroKey
	heroOK  bool
	// cleanTitles 是首页这份 feed 的显示标题缓存。title.Clean 自己也有
	// 全局缓存，但首页一帧会在 Hero、卡片和最近播放里多次问同一条；
	// 这里按 feed revision 失效，避免每次都跨锁查全局表。
	cleanTitleRevision uint64
	cleanTitles        map[string]string

	// day 是这一帧的时刻（「今日一张」按它选，问候语按它定）。
	day time.Time

	// cur / hasCur / playing 是这一帧「正在放的那一首」（从 `Shell.pageInput` 采的）。
	cur     app.Item
	hasCur  bool
	playing bool
}

// newHomePage 建首页这一摊状态。cache 是封面缓存，acts 是操作；wake 保留给接口兼容
// （取数的唤醒走 `homeLoads.wake`）。
func newHomePage(cache *imgcache.Cache, acts list.Actions, _ func()) *homePage {
	return &homePage{acts: acts, cache: cache}
}

// SetSongs 换掉首页的歌（测试和还没接上的环境用）。三摊一起换，并记为「拉过、没有错」。
//
// ⚠️ 只能由界面线程调：它改的正是 view 每帧读的那份状态。
func (h *homePage) SetSongs(daily, chart, news, recent []list.Music) {
	set := func(k homeKind, songs []list.Music) {
		h.feed.sec[k] = homeSection{started: true, songs: songs}
	}
	set(homeDaily, daily)
	set(homeChart, chart)
	set(homeNew, news)
	set(homeRecent, recent)
	h.feed.revision++
}

// viewHome 画首页。页面的那一道 recover 在 `Shell.page` 上（router.go）。
//
// 🔴 铁律二：页面 view 的头一件正事就是装主题，而且必须在最开头——SetTheme 只影响它之后
// 创建的控件。
func (s *Shell) viewHome(c *ui.Context, _ *ui.Route) {
	s.builds.home++
	s.applyFrameTheme(c)
	s.home.view(c, s)
}

// syncHome 是首页每帧的副作用：收后台投回来的结果、起该起的那一拉。
//
// ⚠️ 它们不在 view 里：满屏播放器开着的时候首页不画，而这两样是「每帧都要跑」的副作用，
// 停在画的那一半里就等于停更。
func (s *Shell) syncHome(_ *ui.Context) {
	h := s.home
	h.adopt()
	h.pull()
}

// view 画首页的内容。
func (h *homePage) view(c *ui.Context, s *Shell) {
	ww, _ := c.Size()
	cw := h.contentW
	if cw <= 0 {
		cw = float32(ww) - tok.SideW
	}
	h.win = float32(ww)
	lay := homeLayoutFor(h.win)
	pad := lay.pad
	// 原型 `.page { max-width: 1280px }`：窗口再宽，内容也停在 1280（含左右页边距）。
	bodyW := cw
	if bodyW > homeMaxW {
		bodyW = homeMaxW
	}
	inner := bodyW - 2*pad

	in := s.pageInput()
	h.day = c.Now()
	h.cur, h.hasCur, h.playing = in.Current, in.HasCurrent, in.Playback.Playing
	h.refreshWeek(s, c.Now())

	ui.Scroll(c).Key("home:scroll").Grow(1).TrackScroll(&h.scroll).Children(func() {
		outer := ui.Column(c).Key("home:page").FillWidth().AlignItems(ui.Center)
		outer.Children(func() {
			ui.Column(c).Key("home:body").Width(bodyW).
				Padding(space.PageTop, pad, space.PageBottom, pad).Children(func() {
				h.viewBody(c, s, inner, in)
			})
		})
		if w := outer.Bounds().W; w > 0 {
			h.contentW = w
		}
	})

	// 「回到顶部」那一枚（原型全局的 `.btt`，index.html 827 / app.js 1627：滚过 400 出现）。
	// 首页滚的是整页这一个 `ui.Scroll`，位置就是 `h.scroll`（同设置页）。
	list.BackToTop(c, h.scroll.Y, func() { h.scroll.Y = 0 })
}

// homeLayout 是首页随窗口宽度换挡的几个数。
//
// ⚠️ **原型的媒体查询看的是窗口宽度（视口），不是内容区**：窗口 ≤ 1280 卡片 5 列、≤ 1180 4 列
// 且去掉曲目行的「专辑」列、榜单一列、≤ 1024 页边距 32（同时侧栏收起）、≤ 760 卡片 2 列
// 页边距 16。`space.PageHorizontal` / `pud.GridCols` 拿的是内容区宽度，换算时把侧栏的 232
// 当成恒定，在 1024 以下侧栏收到 72 之后会差一档，所以首页自己按窗口宽判。
type homeLayout struct {
	pad       float32
	cols      int
	chartCols int
	album     pud.AlbumMode
}

func homeLayoutFor(win float32) homeLayout {
	l := homeLayout{pad: space.Px40, cols: 6, chartCols: 2, album: pud.AlbumShown}
	switch {
	case win > 1280:
	case win > 1180:
		l.cols = 5
	case win > 1024:
		l.cols, l.chartCols, l.album = 4, 1, pud.AlbumHidden
	case win > 760:
		l.cols, l.chartCols, l.album, l.pad = 4, 1, pud.AlbumHidden, space.Px32
	default:
		l.cols, l.chartCols, l.album, l.pad = 2, 1, pud.AlbumHidden, space.Px16
	}
	return l
}

// homeMaxW 是页面最宽（原型 `.page { max-width: 1280px }`）。
const homeMaxW = 1280

// viewBody 画页面里的东西：问候区 → 今日一张 → Tab → 选中那一档。
func (h *homePage) viewBody(c *ui.Context, s *Shell, inner float32, in pageInput) {
	daily, chart := &h.feed.sec[homeDaily], &h.feed.sec[homeChart]
	hero := h.heroSongs()

	// 整页只在「两摊都没有内容」时才换成骨架 / 错误；有一摊就先画一摊，另一摊在自己的位置
	// 上说自己的状态。
	none := len(daily.songs) == 0 && len(chart.songs) == 0
	switch {
	case none && chart.err != nil && daily.err != nil:
		h.viewGreet(c, s, inner, in, 0)
		pud.Empty(c, pud.ArtPlain, "推荐暂时加载不出来", daily.err.Error(), func() {
			if pud.Button(c, "home:retry", pud.Primary, pud.Std, "重试", "refresh").Clicked() {
				h.retry(homeDaily)
				h.retry(homeChart)
			}
		})
		return
	}

	picked := len(hero)
	if n := len(daily.songs); n > 0 {
		picked += min(n, homeDailyCount)
	}
	h.viewGreet(c, s, inner, in, picked)
	h.viewHero(c, s, inner, hero)

	ui.Column(c).FillWidth().Margin(space.Px32, space.None, space.None, space.None).Children(func() {
		pud.Tabs(c, "home:tabs", &h.tab, []pud.Tab{{Label: homeTabLabels[0]}, {Label: homeTabLabels[1]}, {Label: homeTabLabels[2]}})
	})

	switch h.tab {
	case homeTabChart:
		h.viewChart(c, s, inner)
	case homeTabNew:
		h.viewNew(c, s, inner)
	default:
		h.viewRec(c, s, inner)
	}
}

// ── 问候区 ──────────────────────────────────────────────────────────────────

// homeGreeting 是按时段的问候（原型 `greeting()`）。
func homeGreeting(now time.Time) string {
	switch h := now.Hour(); {
	case h < 5:
		return "夜深了"
	case h < 11:
		return "早上好"
	case h < 14:
		return "中午好"
	case h < 18:
		return "下午好"
	}
	return "晚上好"
}

// homeDateText 是 Eyebrow 那一行日期（原型 `today()`）：「11 月 4 日 · 星期三」。
func homeDateText(now time.Time) string {
	wd := "日一二三四五六"
	r := []rune(wd)[now.Weekday()]
	return fmt.Sprintf("%d 月 %d 日 · 星期%c", int(now.Month()), now.Day(), r)
}

// homeClock 把一段时长写成「h:mm」（本周已听，原型 `12:40`）。
func homeClock(d time.Duration) string {
	m := int(d.Round(time.Minute).Minutes())
	return fmt.Sprintf("%d:%02d", m/60, m%60)
}

// refreshWeek 刷新「本周已听」：从本周一到今天的听歌时长。统计快照是整份值拷贝，所以最多
// 每 30 秒取一次。没挂统计就是 0。
func (h *homePage) refreshWeek(s *Shell, now time.Time) {
	if !h.weekAt.IsZero() && now.Sub(h.weekAt) < 30*time.Second {
		return
	}
	h.weekAt = now
	st, ok := s.app.ListeningStats()
	if !ok {
		h.week = 0
		return
	}
	back := (int(now.Weekday()) + 6) % 7 // 周一 = 0
	from := now.AddDate(0, 0, -back).Format("2006-01-02")
	h.week = time.Duration(st.DayRangeMs(from, now.Format("2006-01-02"))) * time.Millisecond
}

// viewGreet 画问候区：左边日期 + 大标题，右边两行数字和「继续播放」。
func (h *homePage) viewGreet(c *ui.Context, s *Shell, inner float32, in pageInput, picked int) {
	p := tok.Of(c.Theme())
	now := c.Now()
	greeting := homeGreeting(now)
	if selfMidOf(h.loads) != 0 && h.feed.name != "" {
		greeting += "，" + h.feed.name
	}
	ui.Row(c).FillWidth().AlignItems(ui.End).Justify(ui.SpaceBetween).Gap(space.Px24).
		Padding(space.Px24, space.None, space.None, space.None).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Children(func() {
			typo.Eyebrow(c, homeDateText(now))
			// 原型 `.h-page { line-height: 1.15; margin: 8px 0 0 }`：框架压不下宋体的自然行高，
			// 用负外边距把多出来的那截补回去（见 `typo.DisplayNatural`）。
			gs := scale.Font(c, scale.GreetTitleFluid(h.win))
			trim := typo.DisplayTrim(gs, 1.15)
			typo.Display(ui.Text(c, greeting), gs).LetterSpacing(gs*0.01).
				Margin(space.Px8-trim, space.None, -trim, space.None)
		})
		ui.Column(c).Shrink(0).AlignItems(ui.End).Children(func() {
			metaLine := func(pre, num, post string) {
				// 数字前后的空格是字间距，行首行尾的空白框架会吃掉，所以用 Gap 摆 4。
				ui.Row(c).AlignItems(ui.Baseline).Gap(space.Px4).Children(func() {
					ui.Text(c, pre).FontSize(scale.Font(c, scale.Size14)).LineHeight(leading.Body).TextColor(p.Text2).SingleLine()
					typo.Mono(ui.Text(c, num)).FontSize(scale.Font(c, scale.Size14)).LineHeight(leading.Body).TextColor(p.Text2).SingleLine()
					if post != "" {
						ui.Text(c, post).FontSize(scale.Font(c, scale.Size14)).LineHeight(leading.Body).TextColor(p.Text2).SingleLine()
					}
				})
			}
			if picked > 0 {
				metaLine("今天为你挑了", fmt.Sprint(picked), "首")
			}
			metaLine("本周已听", homeClock(h.week), "")
			st := in.Playback
			if in.HasCurrent && !st.Playing && st.Position > 0 && st.Err == "" && st.Title != "" {
				name := h.cleanTitle(st.Title)
				ui.Box(c).Margin(space.Px8, space.None, space.None, space.None).Children(func() {
					if pud.ButtonMono(c, "home:resume", pud.Ghost, pud.Sm, "继续播放「"+name+"」", clockText(st.Position), "play").Clicked() {
						s.app.Toggle()
					}
				})
			}
		})
	})
}

// ── 公共小件 ────────────────────────────────────────────────────────────────

// coverOf 是「在方框里画这一张封面」。圆角是组件传进来的那一档（卡片 8、榜单行 4），
// 必须落在 imgcache 最内层：GPU 后端嵌套裁剪只有最内层切圆角，外框的 `Radius().Clip()`
// 只按矩形裁（见 `pud.Cover`）。
func (h *homePage) coverOf(url string) pud.Cover {
	return func(c *ui.Context, w, hh, radius float32) {
		imgcache.ViewRadius(c, h.cache, imgcache.FormatURLProtocol(url), w, hh, radius)
	}
}

// isCur 报这一首是不是正在放的那一首（按 bvid 比；没有 bvid 的条目不算）。
func (h *homePage) isCur(m list.Music) bool {
	return h.hasCur && m.Item.Bvid != "" && m.Item.Bvid == h.cur.Bvid
}

// playFrom 换成从第 i 首往后的一段并开始播（原型 `data-act="play" data-list data-i`）。
// 放不了的条目（没有稿件）在 `playAllItems` 里去掉；起播的是第 i 首之后第一首放得了的。
func (h *homePage) playFrom(s *Shell, songs []list.Music, i int) {
	if i < 0 || i >= len(songs) {
		return
	}
	items, _ := playAllItems(songs[i:])
	if len(items) == 0 {
		s.addToast("这一首放不了（没有可播的稿件）", notice.Warning)
		return
	}
	if h.acts.Play != nil {
		h.acts.Play(items)
	}
}

// enqueue 把一批歌加到队列末尾（「全部加入队列」）。
func (h *homePage) enqueue(songs []list.Music) {
	items, _ := playAllItems(songs)
	if len(items) > 0 && h.acts.Add != nil {
		h.acts.Add(items)
	}
}

// cellW 是 n 列、缝为 gap 时一格的宽。
func cellW(w float32, n int, gap float32) float32 { return pud.CellWidth(w, n, gap) }

// homePlays 是播放量的字（"12.3万"）；零值（这一项没有播放量）是空串。
func homePlays(n int64) string { return list.PlayCountText(n) }

// homeDaysAgo 是新歌速递那一列的字：「3 天前 上传」（原型 `daysAgo(t.pub) + ' 上传'`，中间有空格）。上架日期缺失时说「最近上传」。
func homeDaysAgo(t, now time.Time) string {
	if t.IsZero() {
		return "最近上传"
	}
	ty, tm, td := t.Date()
	ny, nm, nd := now.Date()
	a := time.Date(ty, tm, td, 0, 0, 0, 0, time.UTC)
	b := time.Date(ny, nm, nd, 0, 0, 0, 0, time.UTC)
	d := int(b.Sub(a) / (24 * time.Hour))
	switch {
	case d <= 0:
		return "今天 上传"
	case d == 1:
		return "昨天 上传"
	}
	return fmt.Sprintf("%d 天前 上传", d)
}

// homeCardSub 给卡片副题用：「歌手 · 12.3万 播放」，缺哪样就少说哪样。
func homeCardSub(m list.Music) string {
	plays := homePlays(m.View)
	switch {
	case m.Owner != "" && plays != "":
		return m.Owner + " · " + plays + " 播放"
	case m.Owner != "":
		return m.Owner
	case plays != "":
		return plays + " 播放"
	default:
		return ""
	}
}

// cleanTitle 返回当前首页 feed 的显示歌名。feed revision 变化时整份失效，
// 因为同一个 ID 可能被刷新成新的原标题。
func (h *homePage) cleanTitle(raw string) string {
	if h.cleanTitleRevision != h.feed.revision {
		h.cleanTitleRevision = h.feed.revision
		clear(h.cleanTitles)
	}
	if h.cleanTitles == nil {
		h.cleanTitles = make(map[string]string)
	}
	if got, ok := h.cleanTitles[raw]; ok {
		return got
	}
	got := title.Clean(raw).Title
	h.cleanTitles[raw] = got
	return got
}

// ── 推荐 ───────────────────────────────────────────────────────────────────

// viewRec 画「推荐」这一档：每日推荐、最近播放、你的收藏夹。
func (h *homePage) viewRec(c *ui.Context, s *Shell, inner float32) {
	lay := homeLayoutFor(h.win)
	gap := float32(space.GridGap)
	if h.win <= 760 {
		gap = space.Px16
	}
	cols := lay.cols
	cw := cellW(inner, cols, gap)

	daily := &h.feed.sec[homeDaily]
	songs := daily.songs
	if len(songs) > homeDailyCount {
		songs = songs[:homeDailyCount]
	}

	h.section(c, func() {
		if pud.SecHeadIcon(c, "home:daily:all", "每日推荐", "根据你最近的收听，每天 6 首", "播放全部", "<play") {
			h.playFrom(s, songs, 0)
		}
		switch {
		case len(songs) > 0:
			h.cardGrid(c, "daily", songs, cols, cw, gap, s)
		case daily.err != nil:
			h.sectionError(c, "home:daily:retry", daily.err, func() { h.retry(homeDaily) })
		case !h.waiting(homeDaily):
			pud.Empty(c, pud.ArtPlain, "今天还没有推荐", "B 站暂时没有给出推荐，过一会儿再来看看。", nil)
		default:
			h.skeletonGrid(c, "daily", cols, cw, gap)
		}
	})

	// 最近播放。
	recent := &h.feed.sec[homeRecent]
	loggedIn := selfMidOf(h.loads) != 0
	h.section(c, func() {
		more := ""
		if loggedIn {
			more = "全部"
		}
		if pud.SecHead(c, "home:recent:all", "最近播放", "", more) {
			s.navigate(historyPath)
		}
		rs := recent.songs
		if len(rs) > homeRecentCount {
			rs = rs[:homeRecentCount]
		}
		switch {
		case len(rs) > 0:
			h.rail(c, rs, inner, s)
		case recent.err != nil && loggedIn:
			h.sectionError(c, "home:recent:retry", recent.err, func() { h.retry(homeRecent) })
		case loggedIn && h.waiting(homeRecent):
			h.skeletonRail(c, inner)
		default:
			pud.Empty(c, pud.ArtClock, "还没有最近播放", "放过的歌会出现在这里。", nil)
		}
	})

	// 你的收藏夹：登录后才有。
	if loggedIn {
		h.viewFolders(c, s, inner, cols, cw, gap)
	}
}

// section 是一个分区：上面隔 48（原型 `.sec { margin-top: 48px }`）。
func (h *homePage) section(c *ui.Context, body func()) {
	ui.Column(c).FillWidth().Margin(space.SectionGap, space.None, space.None, space.None).Children(body)
}

// sectionError 是一块拉失败的位置上画的「说清为什么 + 重试」。
func (h *homePage) sectionError(c *ui.Context, key string, err error, retry func()) {
	pud.Empty(c, pud.ArtPlain, "这一块没拉出来", err.Error(), func() {
		if pud.Button(c, key, pud.Ghost, pud.Std, "重试", "refresh").Clicked() {
			retry()
		}
	})
}

// cardGrid 画一张卡片网格（原型 `.card-grid`）：cols 列、格间距 gap，行与行也隔 gap。
func (h *homePage) cardGrid(c *ui.Context, kind string, songs []list.Music, cols int, cw, gap float32, s *Shell) {
	ui.Column(c).FillWidth().Gap(gap).Children(func() {
		for start := 0; start < len(songs); start += cols {
			end := min(start+cols, len(songs))
			ui.Row(c).Key("home:" + kind + ":row:" + songs[start].ID).Gap(gap).Children(func() {
				for i := start; i < end; i++ {
					m := songs[i]
					ev := pud.CoverCard(c, "home:"+kind+":card:"+m.ID, h.cleanTitle(m.Title),
						homeCardSub(m), cw, h.coverOf(m.Cover), h.isCur(m))
					if ev.Open || ev.Play {
						h.playFrom(s, songs, i)
					}
				}
			})
		}
	})
}

// skeletonGrid 是卡片网格的骨架：一行，封面 + 两行字。
func (h *homePage) skeletonGrid(c *ui.Context, kind string, cols int, cw, gap float32) {
	ui.Row(c).FillWidth().Gap(gap).Children(func() {
		for i := 0; i < cols; i++ {
			ui.Column(c).Key("home:" + kind + ":sk:" + fmt.Sprint(i)).Width(cw).Children(func() {
				pud.Skeleton(c, fmt.Sprintf("home:%s:sk:%d:cover", kind, i), cw, cw, tok.R8)
				pud.Skeleton(c, fmt.Sprintf("home:%s:sk:%d:t", kind, i), cw*0.7, 14, tok.R4).
					Margin(space.Px12, space.None, space.None, space.None)
				pud.Skeleton(c, fmt.Sprintf("home:%s:sk:%d:s", kind, i), cw*0.45, 13, tok.R4).
					Margin(space.Px6, space.None, space.None, space.None)
			})
		}
	})
}

// homeRailCardW 是 rail 里每张卡的宽（原型 `.rail-wrap .card { width: 176px }`）。
const homeRailCardW = 176

// rail 画最近播放那一排横向滑动的卡片（原型 `.rail-wrap .od-rail`：缝 20）。
func (h *homePage) rail(c *ui.Context, songs []list.Music, inner float32, s *Shell) {
	// 原型 `.rail-wrap { margin-inline: -40px }` + `.od-rail { --od-rail-pad: 40px }`：rail 往两边
	// 各探出一个页边距，卡片一直滑到窗口边缘，起点仍和页内容左对齐（里面再垫回一个页边距）。
	pad := homeLayoutFor(h.win).pad
	ui.ScrollHorizontal(c).Key("home:rail").Width(inner+2*pad).Gap(space.Px20).
		Margin(space.None, -pad, space.None, -pad).Padding(space.None, pad, space.Px8, pad).
		Children(func() {
			for i, m := range songs {
				ev := pud.CoverCard(c, "home:recent:card:"+m.ID, h.cleanTitle(m.Title),
					m.Owner, homeRailCardW, h.coverOf(m.Cover), h.isCur(m))
				if ev.Open || ev.Play {
					h.playFrom(s, songs, i)
				}
			}
		})
}

// skeletonRail 是 rail 的骨架。
func (h *homePage) skeletonRail(c *ui.Context, inner float32) {
	n := int(inner/(homeRailCardW+20)) + 1
	ui.Row(c).FillWidth().Gap(space.Px20).Children(func() {
		for i := 0; i < n; i++ {
			ui.Column(c).Key(fmt.Sprintf("home:rail:sk:%d", i)).Width(homeRailCardW).Children(func() {
				pud.Skeleton(c, fmt.Sprintf("home:rail:sk:%d:cover", i), homeRailCardW, homeRailCardW, tok.R8)
				pud.Skeleton(c, fmt.Sprintf("home:rail:sk:%d:t", i), homeRailCardW*0.7, 14, tok.R4).
					Margin(space.Px12, space.None, space.None, space.None)
			})
		}
	})
}

// homeFolderSub 是收藏夹卡的副题（原型 `colCard`）：「收藏夹 · 9 首」，B 站的合集 / 系列写
// 「合集 · N 首」「系列 · N 首」。数是 B 站上次统计的，不是实时的。
func homeFolderSub(f app.Favorite) string {
	kind := "收藏夹"
	switch f.Type {
	case 21:
		kind = "合集"
	case 31:
		kind = "系列"
	}
	return kind + " · " + favCountText(f)
}

// homeFolder 是「你的收藏夹」里的一张卡：收藏夹本身 + 它在哪一组。
type homeFolder struct {
	g app.FavoriteGroup
	f app.Favorite
}

// viewFolders 画「你的收藏夹」：我创建的在前、我收藏的在后（原型 `mineIds` + `subscribed`）。
func (h *homePage) viewFolders(c *ui.Context, s *Shell, inner float32, cols int, cw, gap float32) {
	hidden := hiddenMenuKeys(s.app.HiddenMenuKeySet())
	var folders []homeFolder
	for _, g := range [...]app.FavoriteGroup{app.FavoritesCreated, app.FavoritesCollected} {
		for _, f := range visibleFavorites(s.app.FavoritesOf().Snapshot(g), hidden) {
			folders = append(folders, homeFolder{g, f})
		}
	}
	// 一个收藏夹都没有就整块不画：原型里这一块永远有卡片，只剩一个标题的空块比没有更怪；
	// 侧栏已经在说「还没有收藏夹」了，这里不再说第二遍。
	if len(folders) == 0 {
		return
	}
	h.section(c, func() {
		pud.SecHead(c, "home:folders:more", "你的收藏夹", "", "")
		ui.Column(c).FillWidth().Gap(gap).Children(func() {
			for start := 0; start < len(folders); start += cols {
				end := min(start+cols, len(folders))
				ui.Row(c).Key(fmt.Sprintf("home:folders:row:%d", start)).Gap(gap).Children(func() {
					for _, hf := range folders[start:end] {
						var cover pud.Cover
						if hf.f.Cover != "" {
							cover = h.coverOf(imgcache.CoverURL(hf.f.Cover, imgcache.ParamsCover))
						}
						if pud.ColCard(c, "home:"+favRowKey(hf.g, hf.f.ID), hf.f.Title, homeFolderSub(hf.f), cw, cover) {
							s.navigate(collectionPathOf(hf.f))
						}
					}
				})
			}
		})
	})
}
