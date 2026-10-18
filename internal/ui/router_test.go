package ui

import (
	"bytes"
	"image/png"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mygoui "github.com/egoist/mygo/ui"
)

// 路由这一组测试跑的是 MyGo 的 Tester：和真窗口一模一样的 layout 与绘制，
// 只是画到内存里。点得到、找得到文字，所以「切得动」这件事能自动验。

// newShell 造一个**没挂任何东西**的外壳：没有设置、没有登录、没有 B 站客户端。
// 收尾（定时器、封面缓存、应用、设置/历史/快捷键的落盘）由基座无条件挂上。
func newShell(t *testing.T, opts ...shellOpt) (*Shell, *mygoui.Tester) {
	t.Helper()
	b := newTestShell(t, opts...)
	return b.s, b.tt
}

// settleText 跑到画面上出现 want 为止。
//
// # 为什么搜索页的那几条要它
//
// 搜索页的结果是**异步**回来的：第一页在后台 goroutine 里拉，回来之后还要
// `Invalidate` 唤醒才进下一帧（铁律三）。而「画不画『没有结果』那一支」这个
// 判断读的是**上一帧**的状态（`PagedList.Paged` 是在 `List.View` 收结果之前
// 读的，见 searchpage.go 的 viewSearchResultsBody）。于是：切过去那一帧画的是
// 转圈，结果落地那一帧画的是列表，**再下一帧**才是空状态。
//
// 这里不去数「等几帧就够」——后台那一趟快慢跟机器负载有关，数帧的写法在满载
// 的 CI 上会偶发红，而它红的时候和被测的逻辑一点关系都没有。
func settleText(t *testing.T, tt *mygoui.Tester, want string) {
	t.Helper()
	waitFrames(t, tt, "画面上出现 "+want, func() bool { return tt.HasText(want) })
}

// TestFourteenRoutes 盯住「Biu 那 14 条」这个数：少一条是漏了，多一条是编的。
//
// **扫码登录不在这 14 条里**——它是本项目自己加的第 15 条（对应 Biu
// 挂在导航栏头像下拉里的那个登录入口），所以单独断言，不混进 want。
//
// 兜底的 404 也不是一条能 Match 的模式，而是 viewRoutes 的 default 分支，
// 所以它不在 routes 里。
func TestFourteenRoutes(t *testing.T) {
	if got := len(routes) + 1; got != 15 {
		t.Fatalf("路由一共 %d 条（表里 %d 条 + 兜底 1 条），want 15（Biu 的 14 条 + 扫码登录）", got, len(routes))
	}
	want := []string{
		"/", "/follow", "/later", "/history", "/local-music", "/download-list",
		"/settings", "/search", "/dynamic-feed", "/user/{id}", "/collection/{id}",
		"/empty", "/mini-player",
	}
	for _, w := range want {
		if got := lookup(w).Match; got != w {
			t.Errorf("lookup(%q) 认出的是 %q", w, got)
		}
	}
	if got := lookup("/没这条").Match; got != "*" {
		t.Errorf("不认识的路径落到了 %q，want *（404）", got)
	}
}

// TestParamRoutesMatch 验带参数的路由：/collection/123 要能认出收藏夹那一条。
func TestParamRoutesMatch(t *testing.T) {
	for _, p := range []string{"/user/123", "/collection/456", "/collection/a-b_c"} {
		if got := lookup(p).Title; got != routes[9].Title && got != routes[10].Title {
			t.Errorf("lookup(%q) 认出的是 %q", p, got)
		}
	}
	if got := lookup("/collection/456").Title; got != "收藏夹 / 合集 / 系列" {
		t.Errorf("lookup(/collection/456) = %q", got)
	}
	// 参数路由不进侧栏菜单：它们是从别的页面点进来的。
	for _, r := range routes {
		if strings.Contains(r.Match, "{") && r.InMenu {
			t.Errorf("%s 带参数却进了侧栏菜单", r.Match)
		}
	}
}

// TestMenuItemsAllSwitch 验侧栏上每一项都切得动，而且切过去有这一页自己的
// 页面——不是空白，也不是一句「还没做」。
//
// 「切得动」和「画的是什么」分开验：已经有内容的页（设置页）画的是它自己，
// 别的页画的是这一页的空状态（routes 表里每一条都带的那句）。
func TestMenuItemsAllSwitch(t *testing.T) {
	s, tt := newShell(t)

	menu := 0
	for _, rt := range routes {
		if !rt.InMenu {
			continue
		}
		menu++
		if err := tt.Click(rt.Title); err != nil {
			t.Fatalf("侧栏上点不到 %q：%v，文字有 %q", rt.Title, err, tt.Texts())
		}
		tt.Frame()

		if got := s.router.Path(); got != rt.Match {
			t.Errorf("点了 %q，当前路径是 %q，文字有 %q", rt.Title, got, tt.Texts())
		}
		if s.builtPage(rt) != nil {
			continue // 这一页有自己的内容（设置页），不画空状态
		}
		if !tt.HasText(rt.Empty.Title) {
			t.Errorf("点了 %q，画面上没有这一页的空状态 %q，文字有 %q", rt.Title, rt.Empty.Title, tt.Texts())
		}
	}
	if menu != 7 {
		t.Errorf("侧栏上有 %d 项，want 7", menu)
	}
}

// TestEveryRouteHasAView 钉住这一张的验收头一条：**14 条路由都能走通，
// 没有一页是空白**。
//
// 怎么算「这一页有东西」：切过去之后画面上得有这一页自己的文案。已经有内容
// 的页（设置页）画的是它自己，其余每一条的文案写死在 routes 表里（出处标在
// 那儿），所以这里逐条断言它真的画出来了；兜底那条是 404 页，断言它说清楚
// 是哪条路径不存在。
func TestEveryRouteHasItsOwnPage(t *testing.T) {
	// 逐条走的路径：参数路由要带一个真的参数，其余照 Match 走。
	paths := map[string]string{
		"/user/{id}":       "/user/12345",
		"/collection/{id}": "/collection/678",
	}
	for _, rt := range routes {
		t.Run(rt.Match, func(t *testing.T) {
			s, tt := newShell(t)
			target, ok := paths[rt.Match]
			if !ok {
				target = rt.Match
			}
			s.navigate(target)
			tt.Frame()

			if s.router.Path() != target {
				t.Fatalf("没切到 %q（在 %q）", target, s.router.Path())
			}
			if !tt.HasText(rt.Title) {
				t.Errorf("页面标题 %q 没画出来，文字有 %q", rt.Title, tt.Texts())
			}
			// 每一页要么有自己的内容（builtPage），要么有一句说得清的空状态。
			// 两者都没有 = 画出来一片白，这正是这一张要消掉的东西。
			if s.builtPage(rt) != nil {
				return // 这一页有自己的内容（设置页），不画空状态
			}
			if rt.Empty.Title == "" {
				t.Fatalf("%s 既没接上 builtPage 又没填空状态文案——画出来就是一片白", rt.Match)
			}
			if !tt.HasText(rt.Empty.Title) {
				t.Errorf("没有画出 %s 的空状态 %q，文字有 %q", rt.Match, rt.Empty.Title, tt.Texts())
			}
			if rt.Empty.Hint == "" {
				t.Fatalf("%s 没说「怎么才能不空」", rt.Match)
			}
			if !tt.HasText(rt.Empty.Hint) {
				t.Errorf("没有画出 %s 的「怎么才能不空」%q，文字有 %q", rt.Match, rt.Empty.Hint, tt.Texts())
			}
		})
	}

	t.Run("404", func(t *testing.T) {
		s, tt := newShell(t)
		s.navigate("/根本没有这一页")
		tt.Frame()

		if s.router.Path() != "/根本没有这一页" {
			t.Fatalf("没停在不存在的路径上（在 %q）", s.router.Path())
		}
		if !tt.HasText("没有「/根本没有这一页」这一页") {
			t.Errorf("404 没有说清楚是哪条路径不存在，文字有 %q", tt.Texts())
		}
		if !tt.HasText("秒后回到主页...") {
			t.Errorf("404 没有说要回首页，文字有 %q", tt.Texts())
		}
	})
}

// TestEmptyStateCopyIsNotBlank 钉住「每一条空着的路由都要有一句说得清的
// 交代」：表里任何一条漏了标题，那一页画出来就是一片白，而且没有测试会红。
//
// 已经有内容的路由（设置页）跳过——它画的是自己的页面，用不到空状态，
// 这一条里给它塞一句空状态反而是留了一句没人会看到的话。
func TestEmptyStateCopyIsNotBlank(t *testing.T) {
	s, _ := newShell(t)
	for _, rt := range routes {
		if s.builtPage(rt) != nil {
			continue
		}
		if strings.TrimSpace(rt.Empty.Title) == "" {
			t.Errorf("%s 没有空状态标题", rt.Match)
		}
		if strings.TrimSpace(rt.Empty.Hint) == "" {
			t.Errorf("%s 没有说「怎么才能不空」", rt.Match)
		}
	}
	// 兜底那条自己的页（notfound.go）不是空状态，单独确认它有自己的文案。
	if notFound.Empty.Title != "" || notFound.Empty.Hint != "" {
		t.Errorf("404 不该走通用空状态，它有自己的页（notfound.go）：%+v", notFound.Empty)
	}
}

// TestToastDoesNotSurviveNavigation 是 1.2 的验收项：切路由时上一张的
// Toast 不会残留。
func TestToastDoesNotSurviveNavigation(t *testing.T) {
	s, tt := newShell(t)

	s.addToast("上一页的提示")
	tt.Frame()
	if !tt.HasText("上一页的提示") {
		t.Fatalf("提示没画出来，文字有 %q", tt.Texts())
	}

	if err := tt.Click("历史记录"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	if got := s.visibleToasts(); len(got) != 0 {
		t.Fatalf("换页之后还留着 %d 条提示：%q", len(got), toastTexts(got))
	}
	if tt.HasText("上一页的提示") {
		t.Errorf("换页之后提示还挂在画面上，文字有 %q", tt.Texts())
	}
}

// TestNavigationDoesNotToast「还没做」验没了：13 条路由都有页面视图了，再弹
// 一句「这一页还没做」等于用占位话盖住真页面（Biu 那边也是切过去就有页面）。
func TestNavigationDoesNotToast(t *testing.T) {
	s, tt := newShell(t)

	for _, rt := range routes {
		if err := tt.Click(rt.Title); err != nil {
			continue // 不在侧栏上的（搜索、动态、参数路由）本来就点不到
		}
		tt.Frame()
	}

	s.navigate("/search")
	tt.Frame()

	for _, text := range s.visibleToasts() {
		if strings.Contains(text.text, "还没做") {
			t.Errorf("换页还弹「还没做」：%q", text.text)
		}
	}
	if tt.HasText("这一页还没做") {
		t.Errorf("画面上还有「这一页还没做」，文字有 %q", tt.Texts())
	}
}

// TestUnknownRouteShowsThe404Page 验兜底。
func TestUnknownRouteShowsThe404Page(t *testing.T) {
	s, tt := newShell(t)
	s.navigate("/根本没有这一页")
	tt.Frame()

	if !tt.HasText("页面不存在") {
		t.Errorf("没有落到 404 页，文字有 %q", tt.Texts())
	}
	if !tt.HasText("没有「/根本没有这一页」这一页") {
		t.Errorf("404 没有说清楚，文字有 %q", tt.Texts())
	}
}

// TestPagePanicIsCaught 是铁律四的验收点：页面里 panic，进程不崩，
// 显示错误页，日志里有记录。
func TestPagePanicIsCaught(t *testing.T) {
	s, tt := newShell(t)

	// 把日志接到内存里，免得这一条 panic 糊在测试输出上，同时能验「记了日志」。
	var logBuf bytes.Buffer
	restore := logOutput(&logBuf)
	t.Cleanup(restore)

	// 换掉画页面内容的那一支，走的正是 viewRoutes 里 14 条路由用的那条 Guard。
	s.pageBody = func(*mygoui.Context, *mygoui.Route, Route) { panic("这一页画崩了") }

	// 起始页是首页，它现在有自己的一屏（三个 Tab），判据是那一排 Tab 里的第一个。
	// ⚠️ 别再拿 `routes[0].Empty.Title` 当「起始页画出来了」的判据：首页接上内容
	// 之后那一句话画在**每一个空着的 Tab 里面**，而这一屏的其余页面也都画
	// 「暂无内容」（那是上游的共享默认文案），这个断言于是永远为真——它已经不再
	// 断「起始页画出来了」，只是在别处找一个说得通的字。
	if _, ok := tt.Find(homeTabs[0]); !ok {
		t.Fatalf("起始页没画出来，文字有 %q", tt.Texts())
	}
	tt.Frame()
	if s.crash == nil {
		t.Fatal("页面 panic 了，外壳没有记住")
	}
	if !strings.Contains(errString(s.crash), "画崩了") {
		t.Errorf("记住的 panic 是 %v", s.crash)
	}
	if !tt.HasText("界面出错了") {
		t.Errorf("没有切到错误页，文字有 %q", tt.Texts())
	}
	if !tt.HasText("这一页画崩了") {
		t.Errorf("错误页上没有崩掉的原因，文字有 %q", tt.Texts())
	}

	logged := logBuf.String()
	if !strings.Contains(logged, "这一页画崩了") {
		t.Errorf("日志里没有崩掉的原因：%q", logged)
	}
	if !strings.Contains(logged, "goroutine") {
		t.Errorf("日志里没有栈：%q", logged)
	}
}

// logOutput 把标准日志接到 buf 上，返回一个还原函数。
func logOutput(buf *bytes.Buffer) func() {
	old := log.Writer()
	flags := log.Flags()
	log.SetOutput(buf)
	log.SetFlags(0)
	return func() { log.SetOutput(old); log.SetFlags(flags) }
}

// TestPanicDoesNotBlockNavigation 验崩溃是**页面级**的：换一页就恢复正常。
func TestPanicDoesNotBlockNavigation(t *testing.T) {
	s, tt := newShell(t)
	s.pageBody = func(*mygoui.Context, *mygoui.Route, Route) { panic("炸了") }
	tt.Frame()
	if s.crash == nil {
		t.Fatal("没有记住崩溃")
	}

	// 错误页上要有一条走得出去的路：崩的是某一页，不一定非得是它自己。
	if !tt.HasText("回到首页") {
		t.Errorf("错误页上没有「回到首页」，文字有 %q", tt.Texts())
	}
	s.pageBody = s.pageContent // 假装这一页修好了

	if err := tt.Click("回到首页"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	if s.crash != nil {
		t.Errorf("换页之后崩溃状态没复位：%v", s.crash)
	}
	if tt.HasText("界面出错了") {
		t.Errorf("换页之后还在错误页，文字有 %q", tt.Texts())
	}
	// 同上：判首页画出来了没有，看它自己的那一排 Tab。
	if _, ok := tt.Find(homeTabs[0]); !ok {
		t.Errorf("没有回到首页，文字有 %q", tt.Texts())
	}
}

// TestNavigateResetsTheCrash 验切路由会把崩溃状态复位——不然一页崩了之后
// 整个窗口都出不来。
func TestNavigateResetsTheCrash(t *testing.T) {
	s, tt := newShell(t)
	s.crash = "之前崩过"

	s.navigate("/download-list")
	tt.Frame()

	if s.crash != nil {
		t.Errorf("换页之后崩溃状态没复位：%v", s.crash)
	}
	if tt.HasText("界面出错了") {
		t.Errorf("换页之后还在错误页，文字有 %q", tt.Texts())
	}
	if _, ok := tt.Find(lookup("/download-list").Empty.Title); !ok {
		t.Errorf("没有切到下载记录，文字有 %q", tt.Texts())
	}
}

// TestBackReturnsToThePreviousPage 验历史能退回去，退回去时提示也不残留。
func TestBackReturnsToThePreviousPage(t *testing.T) {
	s, tt := newShell(t)

	if err := tt.Click("稍后再看"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	// 认的是**页面自己的标题**。这一页后来接上了自己的 view（later.go），空状态
	// 由那一页自己说，`Route.Empty` 里那一句已经拿掉了——认空状态那句的话，
	// 这一页再画得再好也会被判成「没切过去」。
	if _, ok := tt.Find(lookup("/later").Title); !ok {
		t.Fatalf("没切到稍后再看，文字有 %q", tt.Texts())
	}

	back, ok := tt.Find(navBackLabel)
	if !ok {
		t.Fatalf("导航栏上没有后退，文字有 %q", tt.Texts())
	}
	tt.ClickAt(back.X+back.W/2, back.Y+back.H/2)
	tt.Frame()

	if got := s.router.Path(); got != "/" {
		t.Errorf("后退之后在 %q，want /", got)
	}
	if got := s.visibleToasts(); len(got) != 0 {
		t.Errorf("后退之后还留着 %d 条提示：%q", len(got), toastTexts(got))
	}
}

func toastTexts(list []toast) []string {
	out := make([]string, len(list))
	for i, t := range list {
		out[i] = t.text
	}
	return out
}

// TestRenderShots 把画面存成 PNG，留给人眼对照。它跑的是 MyGo 的 Tester，
// 走的是和真窗口一模一样的 layout + paint，只是画到内存里而不是屏幕上。
//
//	go test ./internal/ui/ -run TestRenderShots
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderShots
func TestRenderShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	s, tt := newShell(t)

	// 不做切页动画：Tester 一帧就画完，250ms 的滑动停在半路，存出来的图上
	// 上一页的文字还压在左边（看着像渲染错了，其实是动画没走完）。
	s.router.Transition = mygoui.TransitionNone

	save(t, dir, "10-首页.png", tt)
	if err := tt.Click("我的关注"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	save(t, dir, "11-切路由带提示.png", tt)

	// 提示自己到点收走之后的样子。
	s.clearToasts()
	tt.Frame()
	save(t, dir, "12-切路由清掉上一页提示.png", tt)

	// 一页崩了之后的样子：程序活着，出事的那块换一张说明页。
	s.crash = "这一页画崩了"
	tt.Frame()
	save(t, dir, "13-错误页.png", tt)

	// 空状态与 404：这一张的正面。倒计时用的是假时钟，存图时才推一下，
	// 否则图上永远是「3 秒后回到主页...」。
	s.crash = nil
	s.clock = newFakeClock(time.Unix(2026, 0))
	s.navigate("/local-music")
	tt.Frame()
	save(t, dir, "14-本地音乐空状态.png", tt)
	// 搜索页的两个分支：还没输入 / 搜了没有结果。
	s.navigate("/search")
	tt.Frame()
	save(t, dir, "14a-搜索-还没输入.png", tt)
	s.navigate("/search?q=" + url.QueryEscape("周杰伦"))
	tt.Frame()
	save(t, dir, "14a2-搜索-没有结果.png", tt)

	// 空状态落地页：上游的默认空状态。
	s.navigate("/empty")
	tt.Frame()
	save(t, dir, "14b-空状态落地页.png", tt)
	// 已经有内容的那一页：设置页不画空状态，画的是它自己。
	s.navigate("/settings")
	tt.Frame()
	save(t, dir, "14c-设置页.png", tt)

	s.navigate("/根本没有这一页")
	tt.Frame()
	save(t, dir, "15-404-倒计时3秒.png", tt)
	s.clock.(*fakeClock).Advance(2 * time.Second)
	tt.Frame()
	save(t, dir, "16-404-倒计时1秒.png", tt)
	s.clock.(*fakeClock).Advance(time.Second)
	tt.Frame()
	save(t, dir, "17-404后自动回首页.png", tt)

	// 歌词那一支：先打开一首真的歌，再进迷你播放器（nowplaying.go）。
	openATrack(t, s)
	s.navigate("/mini-player")
	tt.Frame()
	save(t, dir, "19-迷你播放器-没有歌词.png", tt)

	// 深色下同一批页面——顺带看一眼页面 view 装没装主题。
	tt.SetDark(true)
	s.navigate("/download-list")
	tt.Frame()
	save(t, dir, "18-深色下的空状态.png", tt)
}

func save(t *testing.T, dir, name string, tt *mygoui.Tester) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
	t.Logf("存了 %s", filepath.Join(dir, name))
}
