package ui

import (
	"bytes"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 路由这一组测试跑的是 MyGo 的 Tester：和真窗口一模一样的 layout 与绘制，
// 只是画到内存里。点得到、找得到文字，所以「切得动」这件事能自动验。

func newShell(t *testing.T) (*Shell, *mygoui.Tester) {
	t.Helper()
	a := app.New()
	t.Cleanup(a.Close)
	s := New(a)
	t.Cleanup(s.images.Close)
	// 搜索历史带着一个后台落盘的 goroutine（见 searchhistory.go）。每个外壳
	// 一个，测试里建得比真窗口快得多，不收掉就是一堆挂着的 goroutine。
	t.Cleanup(func() { _ = s.hist.Close() })
	return s, mygoui.NewTester(s.View, 1200, 800)
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

// TestMenuItemsAllSwitch 验侧栏上每一项都切得动。
//
// 「切得动」和「这一页还没做」分开验：已经做了内容的页（设置页）切过去不该
// 再弹「还没做」——对着一页真做出来的东西说它还没做，是在说一件假事。
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
			continue
		}
		if !tt.HasText("这一页还没做") {
			t.Errorf("%q 这一页没有「还没做」的提示，文字有 %q", rt.Title, tt.Texts())
		}
	}
	if menu != 7 {
		t.Errorf("侧栏上有 %d 项，want 7", menu)
	}
}

// TestToastDoesNotSurviveNavigation 是 1.2 的验收项：切路由时上一张的
// Toast 不会残留。
func TestToastDoesNotSurviveNavigation(t *testing.T) {
	s, tt := newShell(t)

	if err := tt.Click("我的关注"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("这一页还没做") {
		t.Fatalf("点菜单没有弹提示，文字有 %q", tt.Texts())
	}

	// 第一张提示还在的时候换页：走的是 navigate，它先清后加。
	if err := tt.Click("历史记录"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	if got := s.visibleToasts(); len(got) != 1 {
		t.Fatalf("换页之后有 %d 条提示，want 1：%q", len(got), toastTexts(got))
	}
	if got := s.visibleToasts()[0].text; !strings.Contains(got, "历史记录") {
		t.Errorf("剩下的提示是 %q，应该是「历史记录」那一条", got)
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

	if _, ok := tt.Find("这一页还没做"); !ok {
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
	s.pageBody = s.placeholder // 假装这一页修好了

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
	if _, ok := tt.Find("/"); !ok {
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
	if _, ok := tt.Find("/download-list"); !ok {
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
	if _, ok := tt.Find("/later"); !ok {
		t.Fatalf("没切到稍后再看，文字有 %q", tt.Texts())
	}

	back, ok := tt.Find("Back")
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
