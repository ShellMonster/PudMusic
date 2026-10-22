package ui

// 「稍后再看」和「历史记录」这一组跑的是**真实外壳 + 真实 B 站客户端 + 一台假
// B 站服务器**：切到那一页、真的画一帧、真的敲字、真的点预设，然后去问那台假
// 服务器「你收到什么查询了」。
//
// 断言的东西只有两类：**画面上的文字**（这一页有没有真画出来、那一列写的是什么）
// 和**发出去的请求**（筛选条件有没有真的变成查询串）。不断言「调了哪个函数」——那
// 只能证明状态层有那个方法，证明不了界面上真有一个入口。

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	mygoui "github.com/egoist/mygo/ui"
)

// now 是这一组假时钟的「现在」。写死是为了「今天」永远是那一天——好几条断言的是
// 日期区间，用真时钟的话跨过零点就红了。
var now = time.Date(2026, 10, 16, 21, 4, 0, 0, time.Local)

const (
	laterPathAPI   = "/x/v2/history/toview/web"
	historyPathAPI = "/x/web-interface/history/search"

	// navBody 是 WBI 签名要用的那一次 nav（`wbi.go` 先打它拿密钥）。
	navBody = `{"code":0,"message":"0","ttl":1,"data":{"isLogin":true,
	  "wbi_img":{"img_url":"https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png",
	             "sub_url":"https://i0.hdslb.com/bfs/wbi/4932caff0ff746eab6f01bf08b70ac45.png"}}}`

	// refused 是「B 站拒绝了这一页」——业务错误在 HTTP 200 里面。
	refused = `{"code":-500,"message":"请求过于频繁","ttl":1}`
)

// libFake 是一台照着给定页码回答的假 B 站，顺便把收到的查询录下来。
type libFake struct {
	*httptest.Server
	mu      sync.Mutex
	later   map[int]string // 页号 → 返回
	history map[int]string
	seen    []libSeen // 录下来的请求
}

type libSeen struct {
	path  string
	query url.Values
}

func newLibFake(t *testing.T) *libFake {
	t.Helper()
	f := &libFake{later: map[int]string{}, history: map[int]string{}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.seen = append(f.seen, libSeen{path: r.URL.Path, query: r.URL.Query()})
		var body string
		switch r.URL.Path {
		case laterPathAPI:
			body = f.later[pageOfQuery(r.URL.Query())]
		case historyPathAPI:
			body = f.history[pageOfQuery(r.URL.Query())]
		case "/x/web-interface/nav":
			body = navBody
		}
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if body == "" {
			// 没装页码就是「没有这一页」，不是 404：翻页那一层要看到一次
			// 成功但空的结果，而不是一个错。
			body = `{"code":0,"message":"0","ttl":1,"data":{"count":0,"list":[],"has_more":false}}`
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(f.Close)
	return f
}

// 名字叫 pageOfQuery 而不是 pageOf：主干上的 `searchpage_test.go` 有一个**同名**
// 的测试辅助函数 `pageOf(*Shell, int)`。同一个包里同名就是编译期的 redeclared，
// 而那个错和这一票在做的事看起来毫不相干。
func pageOfQuery(q url.Values) int {
	n, _ := strconv.Atoi(q.Get("pn"))
	return n
}

// withLater 装一页稍后再看的内容。**count 是 B 站给的总条数**，不是这一页的条数——
// 「还有没有」就是拿它算的，所以它决定了会不会自动续拉。
func (f *libFake) withLater(page, count int, titles ...string) *libFake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.later[page] = laterBody(count, page*100, titles)
	return f
}

// withHistory 装一页历史记录。progress / duration 是同一条歌的进度和总长。
func (f *libFake) withHistory(page, progress, duration int, titles ...string) *libFake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.history[page] = historyBody(progress, duration, titles)
	return f
}

// queriesOf 返回打给某一条路径的每一次查询（顺序）。
func (f *libFake) queriesOf(path string) []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []url.Values
	for _, s := range f.seen {
		if s.path == path {
			out = append(out, s.query)
		}
	}
	return out
}

// callsOf 是打给某一条路径的请求数。
func (f *libFake) callsOf(path string) int { return len(f.queriesOf(path)) }

// laterBody 造一页稍后再看的返回。**seed 让每一页的行身份互不相同**——翻页撞了身份
// 会被去重吃掉，而那在测试里表现为「第二页明明有内容却没加上」。
func laterBody(count, seed int, titles []string) string {
	items := make([]map[string]any, 0, len(titles))
	for i, title := range titles {
		items = append(items, map[string]any{
			"aid": int64(170001 + i), "bvid": fmt.Sprintf("BVlater%02d", seed+i), "cid": int64(3001 + i),
			"title": title, "pic": "", "duration": 185, "progress": 0,
			"pubdate": int64(1700000000), "add_at": now.AddDate(0, 0, -i).Unix(), "is_pgc": false,
			"owner": map[string]any{"mid": 11, "name": "UP " + title},
			"stat":  map[string]any{"view": int64(1000 * (i + 1))},
		})
	}
	return biliOK(map[string]any{"count": count, "list": items})
}

// historyBody 造一页历史记录。同一首歌的每一条都是不同的观看时刻——那正是这一页
// 的行身份要解决的问题。
func historyBody(progress, duration int, titles []string) string {
	items := make([]map[string]any, 0, len(titles))
	for i, title := range titles {
		items = append(items, map[string]any{
			"title": title, "cover": "", "author_name": "UP " + title, "author_mid": 11,
			"view_at":  now.Add(-time.Duration(i) * time.Hour).Unix(),
			"progress": progress, "duration": duration,
			// ⚠️ **数字**：上游那个类型是 `kid?: number`，前缀是删除接口自己拼的。
			"kid": float64(170001 + i),
			// **每一条的 bvid 一样**：同一首歌看过两次在 B 站就是这样的两条。
			"history": map[string]any{"bvid": "BVSAME", "cid": 4001 + i, "business": "archive"},
		})
	}
	return biliOK(map[string]any{"has_more": false, "page": map[string]any{"pn": 1}, "list": items})
}

func biliOK(data any) string {
	raw, err := json.Marshal(map[string]any{"code": 0, "message": "0", "ttl": 1, "data": data})
	if err != nil {
		panic(err)
	}
	return string(raw)
}

// libraryShell 造一个停在 path 上的外壳，B 站接口指向 f。
//
// **假时钟**是这里的关键：「今天」永远是 2026-10-16，而「敲完字等 500ms」也不用真睡。
func libraryShell(t *testing.T, f *libFake, path string) (*mygoui.Tester, *Shell) {
	t.Helper()
	return libraryShellOpts(t, f, path)
}

// libraryShellOpts 是 `libraryShell` 加一批外壳选项（出图那一族要 `systemIsDark`）。
func libraryShellOpts(t *testing.T, f *libFake, path string, opts ...shellOpt) (*mygoui.Tester, *Shell) {
	t.Helper()

	b := newTestShell(t, append([]shellOpt{
		mounted(func(t *testing.T, a *app.App) {
			// ⚠️ **同一个客户端**（生产环境就是同一个，见 main.go 那段注释）：
			// 登录态、请求头、WBI 密钥都挂在它身上。
			cli := biliForTest(t, f)
			// ⚠️ **挂一份登录态**（票 #138）：这两页的内容要登录才有
			// （`Route.RequiresLogin`），没登录时页面画的是「先登录」那一支，
			// 一个请求都不发——于是这一组测试（它们要的是**内容**）就永远等不到。
			store := app.NewMemoryStore("test")
			if err := store.Save(context.Background(), app.Session{SESSDATA: "sess"}); err != nil {
				t.Fatal(err)
			}
			a.MountLogin(store, cli)
			a.MountLibrary(cli)
		}),
		withClock(newFakeClock(now))}, opts...)...)
	s, tt := b.s, b.tt
	// 关键词防抖那个钟由基座的 `Shell.Close` 收：以前这里手写一份
	// `searchWatchStop`，而 `TestTheSearchWatchStopsWhenThePageIsLeft` 敲完字就
	// 切走——漏一次就是一条每秒醒四次的 goroutine 到测试进程退出为止。
	tt.Frame()
	s.navigate(path)
	tt.Frame()
	return tt, s
}

// biliForTest 造一个指向假 B 站的真客户端。
func biliForTest(t *testing.T, f *libFake) *bili.Client {
	t.Helper()
	cli := bili.NewClient(bili.Options{
		// 用 httptest 自带的那份 transport——它认识那张自签证书。
		Transport: f.Server.Client().Transport,
		CSRF:      func() string { return "the-jct" },
	})
	// ⚠️ 不指到 fixture 上的话这些用例会打到**真的** B 站：不报错、结果还挺合理，
	// 于是测试变成「取决于能不能连上 B 站」。
	cli.TestOnlyPointHostsAtFixture(f.URL)
	return cli
}

// settle 跑到 cond 成立为止。每一轮走一帧，顺带让后台那一支有机会跑。
//
// ⚠️ 不能只 `tt.Frame()` 一次：拉页在后台 goroutine 里，结果要靠唤醒才进下一帧。
// rowsShown 是已经拿到的行数。
func rowsShown(p *libraryPage) int { return len(p.rows.Rows()) }

// pageOf 是路径对应的那一页。
func (s *Shell) pageOf(path string) *libraryPage {
	switch path {
	case laterPath:
		return &s.laterPage
	case historyPath:
		return &s.historyPage
	}
	panic("测试用错了路径：" + path)
}

// typeKeyword 在那一页的搜索框里敲字（走真实的点击 + 真实输入）。
func typeKeyword(t *testing.T, tt *mygoui.Tester, text string) {
	t.Helper()
	box, ok := tt.Find(keywordBoxLabel)
	if !ok {
		t.Fatalf("搜索框没画出来，文字有 %q", tt.Texts())
	}
	tt.ClickAt(box.X+box.W/2, box.Y+box.H/2)
	tt.Frame()
	tt.Type(text)
	tt.Frame()
}

// advance 把假时钟往前推 d，再走一帧。
//
// ⚠️ **必须显式 Advance**：Tester 的钟站着不动（只有 Advance 推得动），「框不动
// 了」不等于「动画停了」——用它当「时间到了」的判据会在动画中途放行。
func advance(s *Shell, d time.Duration) { s.clock.(*fakeClock).Advance(d) }

// day 是一天的 00:00:00（本地时区）。
func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.Local) }

// ─────────────────────────────────────────────────────────────────────────────
// 两页都画出来
// ─────────────────────────────────────────────────────────────────────────────

// TestBothLibraryPagesDrawTheirOwnContent 钉住「这两页不再是一张空状态」。
//
// 逐项点名是因为最可能的失败是「画出来了，但少了一项」——用户看见一个没有日期
// 筛选的稍后再看，不会知道还有。
func TestBothLibraryPagesDrawTheirOwnContent(t *testing.T) {
	for _, c := range []struct {
		path  string
		title string
		// want 是这一页表头上特有的那几列。
		want []string
	}{
		{laterPath, "稍后再看", []string{"播放量", "加入时间", "时长"}},
		{historyPath, "历史记录", []string{"播放进度", "观看时间"}},
	} {
		t.Run(c.title, func(t *testing.T) {
			f := newLibFake(t).
				withLater(1, 3, "甲", "乙", "丙").
				withHistory(1, 754, 2700, "甲", "乙", "丙")
			tt, s := libraryShell(t, f, c.path)

			// 侧栏上点得进去。
			if err := tt.Click(c.title); err != nil {
				t.Fatalf("侧栏上点不到「%s」：%v，文字有 %q", c.title, err, tt.Texts())
			}
			tt.Frame()
			if s.router.Path() != c.path {
				t.Fatalf("点侧栏之后在 %q，want %q", s.router.Path(), c.path)
			}

			for _, want := range append([]string{
				c.title, keywordBoxLabel,
				"全部时间", "今天", "昨天", "近一周", "不按日期筛",
				dateRangeLabel,
			}, c.want...) {
				if !tt.HasText(want) {
					t.Errorf("%s 上没有 %q，文字有 %q", c.title, want, tt.Texts())
				}
			}

			// 内容真的来了，不是转圈。
			waitFrames(t, tt, "把内容拉回来", func() bool { return rowsShown(s.pageOf(c.path)) == 3 })
			for _, title := range []string{"甲", "乙", "丙"} {
				if !tt.HasText(title) {
					t.Errorf("%s 的第 %q 首没画出来，文字有 %q", c.title, title, tt.Texts())
				}
			}
			// 两页接上了自己的 view，不是退回空状态那一支。
			if s.builtPage(lookup(c.path)) == nil {
				t.Errorf("%s 没接上 builtPage", c.path)
			}
		})
	}
}

// TestTheLibraryPagesEmptyStateSaysWhyAndHow 钉住这两页「一行都没有」时画的两句话。
//
// 名字带 `TheLibraryPages` 是因为主干上已经有一个同名的（`localmusic_test.go`
// 的那一条，测的是本地音乐那一页）。
//
// 两句都要在：第一句说这里为什么空，第二句给一个**能做的下一步**。这一页尤其
// 需要第二句——用户常常是筛完了才空的，他要的是「怎么才能不空」。
//
// 文案是**从 `routes` 表里那一条拿的**（出处标在那儿），所以这里断言的也是那一
// 条：另抄一份就多一个会对不上的来源，而对不上时用户看不出任何异样。
func TestTheLibraryPagesEmptyStateSaysWhyAndHow(t *testing.T) {
	for _, path := range []string{laterPath, historyPath} {
		t.Run(path, func(t *testing.T) {
			// 一页都不给，就是「什么都没有」那一支。
			tt, _ := libraryShell(t, newLibFake(t), path)

			e := lookup(path).Empty
			if e.Title == "" || e.Hint == "" {
				t.Fatalf("routes 表里 %s 那一条的空状态不完整：%+v", path, e)
			}
			waitFrames(t, tt, "把空状态画出来", func() bool { return tt.HasText(e.Title) })
			if !tt.HasText(e.Hint) {
				t.Errorf("%s 空着的时候没说「怎么才能不空」，文字有 %q", path, tt.Texts())
			}
		})
	}
}

// TestAnUnmountedLibraryStillShowsTheEmptyState 盯「没挂上 B 站客户端」那一支。
//
// 那一支不能只说「没接上」就完事：用户是从侧栏点过来的，他先要看到这一页平时
// 空着的那一句（「这里为什么空」），**然后**才是这一次特别的原因。少画前一句，
// 从别的页面切过来的那条路（`homepage_test.go`、`settings_test.go` 都按
// `lookup(path).Empty.Title` 认「切过去了」）就断了。
//
// 这一条用的是 `newShell`——它**不挂** B 站客户端。
func TestAnUnmountedLibraryStillShowsTheEmptyState(t *testing.T) {
	for _, path := range []string{laterPath, historyPath} {
		t.Run(path, func(t *testing.T) {
			s, tt := newShell(t)
			tt.Frame()
			s.navigate(path)
			tt.Frame()

			if !tt.HasText(lookup(path).Empty.Title) {
				t.Errorf("%s 没挂上接口时连「这里为什么空」都没说，文字有 %q", path, tt.Texts())
			}
			if !tt.HasText("还没接上 B 站接口") {
				t.Errorf("%s 没挂上接口时说清了为什么，文字有 %q", path, tt.Texts())
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 关键词
// ─────────────────────────────────────────────────────────────────────────────

// TestTheKeywordReachesTheServerAfterTheDebounce 盯「敲的字变成了查询串」。
//
// 两条一起钉：**敲完不能立刻发**（不防抖的话每敲一个字发一次，B 站的风控不是摆设），
// **500ms 之后一定要发**（漏了防抖的唤醒就是「用户敲完了什么都没发生」——界面上和
// 「B 站忽略了那个词」一模一样）。
func TestTheKeywordReachesTheServerAfterTheDebounce(t *testing.T) {
	f := newLibFake(t).withLater(1, 1, "甲")
	tt, s := libraryShell(t, f, laterPath)
	waitFrames(t, tt, "把第一页拉回来", func() bool { return f.callsOf(laterPathAPI) == 1 })

	typeKeyword(t, tt, "周杰伦")
	tt.Frame()
	if got := f.callsOf(laterPathAPI); got != 1 {
		t.Fatalf("敲完字立刻就发了请求（共 %d 次）——防抖没起作用", got)
	}
	if got := f.callsOf(laterPathAPI); got != 1 {
		t.Fatalf("还没到 500ms 就发了（共 %d 次）", got)
	}

	advance(s, 600*time.Millisecond)
	waitFrames(t, tt, "把新条件的那一页拉回来", func() bool { return f.callsOf(laterPathAPI) == 2 })
	if got := f.queriesOf(laterPathAPI)[1].Get("key"); got != "周杰伦" {
		t.Errorf("查询串里 key = %q，want 周杰伦", got)
	}
}

// TestEnterSearchesWithoutWaiting 盯回车不等那 500ms（上游也是 `onKeyDown Enter`）。
func TestEnterSearchesWithoutWaiting(t *testing.T) {
	f := newLibFake(t).withLater(1, 1, "甲")
	tt, _ := libraryShell(t, f, laterPath)
	waitFrames(t, tt, "把第一页拉回来", func() bool { return f.callsOf(laterPathAPI) == 1 })

	typeKeyword(t, tt, "林俊杰")
	tt.TypeKey(0, mygoui.KeyEnter, "\r")
	tt.Frame()
	waitFrames(t, tt, "回车之后立刻就搜了", func() bool { return f.callsOf(laterPathAPI) == 2 })
	if got := f.queriesOf(laterPathAPI)[1].Get("key"); got != "林俊杰" {
		t.Errorf("回车之后查询串里 key = %q", got)
	}
}

// TestChangingTheKeywordThrowsTheOldPageAway 钉住「旧查询的结果作废」。
//
// 用户连点两个词时，第一个请求的结果可能后到。盖掉新条件的结果，界面上就是「搜的
// 是新词、显示的是旧词的歌」——用户无从判断，只能刷新。
func TestChangingTheKeywordThrowsTheOldPageAway(t *testing.T) {
	f := newLibFake(t).withLater(1, 1, "旧词的第一页")
	tt, s := libraryShell(t, f, laterPath)
	waitFrames(t, tt, "把第一页拉回来", func() bool { return rowsShown(&s.laterPage) == 1 })

	f.withLater(1, 1, "新词的第一页")
	typeKeyword(t, tt, "新词")
	advance(s, 600*time.Millisecond)
	tt.TypeKey(0, mygoui.KeyEnter, "\r")
	tt.Frame()
	waitFrames(t, tt, "把新条件的那一页拉回来", func() bool {
		return tt.HasText("新词的第一页") && !tt.HasText("旧词的第一页")
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 日期范围
// ─────────────────────────────────────────────────────────────────────────────

// TestTheFourPresetsAskForThatRange 盯四个预设各自算出什么。
//
// 逐个点是因为它们错的方式不一样：今天和昨天差一天（差错了就是「昨天搜不出昨天看
// 过的」，用户以为功能坏了）；近一周是往前 7 天而不是「这一周」。
//
// 顺带钉住筛选状态那一句话：选了「全部时间」之后两个输入框里仍然摆着日期，只看框里
// 的字会以为今天正在筛。
func TestTheFourPresetsAskForThatRange(t *testing.T) {
	for _, c := range []struct {
		label string
		// wantStart / wantEnd 是那一天的 00:00:00 和 23:59:59（零值 = 不筛）。
		wantStart, wantEnd time.Time
		// wantSummary 是界面上那句「现在正在筛什么」。
		wantSummary string
	}{
		{"全部时间", time.Time{}, time.Time{}, "不按日期筛"},
		{"今天", day(2026, 10, 16), day(2026, 10, 16).Add(24*time.Hour - time.Second), "2026-10-16 ~ 2026-10-16"},
		{"昨天", day(2026, 10, 15), day(2026, 10, 15).Add(24*time.Hour - time.Second), "2026-10-15 ~ 2026-10-15"},
		{"近一周", day(2026, 10, 9), day(2026, 10, 16).Add(24*time.Hour - time.Second), "2026-10-09 ~ 2026-10-16"},
	} {
		t.Run(c.label, func(t *testing.T) {
			f := newLibFake(t).withLater(1, 1, "甲")
			tt, _ := libraryShell(t, f, laterPath)
			waitFrames(t, tt, "把第一页拉回来", func() bool { return f.callsOf(laterPathAPI) == 1 })

			if !tt.HasText("不按日期筛") {
				t.Errorf("默认没有说清不筛，文字有 %q", tt.Texts())
			}
			if err := tt.Click(c.label); err != nil {
				t.Fatalf("点不到「%s」：%v，文字有 %q", c.label, err, tt.Texts())
			}
			tt.Frame()
			waitFrames(t, tt, "按新的日期重新拉", func() bool { return f.callsOf(laterPathAPI) == 2 })

			q := f.queriesOf(laterPathAPI)[1]
			gotStart, gotEnd := q.Get("add_time_start"), q.Get("add_time_end")
			if c.wantStart.IsZero() {
				if gotStart != "" || gotEnd != "" {
					t.Errorf("「%s」不该发日期参数，却发了 %q ~ %q", c.label, gotStart, gotEnd)
				}
			} else {
				if want := strconv.FormatInt(c.wantStart.Unix(), 10); gotStart != want {
					t.Errorf("add_time_start = %q，want %q", gotStart, want)
				}
				if want := strconv.FormatInt(c.wantEnd.Unix(), 10); gotEnd != want {
					t.Errorf("add_time_end = %q，want %q", gotEnd, want)
				}
			}
			if !tt.HasText(c.wantSummary) {
				t.Errorf("界面上没说清现在筛的是哪一段，文字有 %q", tt.Texts())
			}
		})
	}
}

// TestTheRangePickerDrivesTheFilter 钉住「手动区间」那一半。
//
// 只点预设的话「手动挑一天」那条路就没人走过——而它是日期筛选里唯一能让用户挑到
// **今天没有预设覆盖**的那一段的办法。
//
// ⚠️ **这里点的是双月日历里的日子**（票 #176 / G34 换掉了原来那两个日期输入
// 框）：点一下是**起始**，再点一下是**结束**，两次点完才换条件。
func TestTheRangePickerDrivesTheFilter(t *testing.T) {
	f := newLibFake(t).withLater(1, 1, "甲")
	tt, _ := libraryShell(t, f, laterPath)
	waitFrames(t, tt, "把第一页拉回来", func() bool { return f.callsOf(laterPathAPI) == 1 })

	// 点开日期范围那一块，它会弹出**两个月**的日历。
	if err := tt.Click(dateRangeLabel); err != nil {
		t.Fatalf("点不到日期范围：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
	for _, want := range []string{"2026年10月", "2026年11月"} {
		if !tt.HasText(want) {
			t.Fatalf("日历没打开（缺 %q），文字有 %q", want, tt.Texts())
		}
	}

	// 先点起始：2026-10-03。
	if err := tt.Click("2026年10月3日"); err != nil {
		t.Fatalf("日历上点不到 10 月 3 号：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
	// ⚠️ **只点起始还不换条件**：区间还没挑完，这时候重拉一次列表是白拉的。
	if got := f.callsOf(laterPathAPI); got != 1 {
		t.Fatalf("只挑了起始就重拉了列表（%d 次），区间还没挑完", got)
	}

	// 再点结束：2026-10-20。
	if err := tt.Click("2026年10月20日"); err != nil {
		t.Fatalf("日历上点不到 10 月 20 号：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()

	waitFrames(t, tt, "按手动挑的日期重新拉", func() bool { return f.callsOf(laterPathAPI) == 2 })
	q := f.queriesOf(laterPathAPI)[1]
	if got := q.Get("add_time_start"); got != strconv.FormatInt(day(2026, 10, 3).Unix(), 10) {
		t.Errorf("手动挑了区间之后 add_time_start = %q，want 2026-10-03 那一秒", got)
	}
	// 结束是那一天那 23:59:59（闭区间，见 endOfDay 的注释）。
	if got, want := q.Get("add_time_end"), strconv.FormatInt(day(2026, 10, 20).Add(24*time.Hour-time.Second).Unix(), 10); got != want {
		t.Errorf("手动挑了区间之后 add_time_end = %q，want %q", got, want)
	}
	// 手动挑过之后四个预设一个都不该亮着，也不该还说着「不按日期筛」。
	if tt.HasText("不按日期筛") {
		t.Error("手动挑了日期，界面上还说着「不按日期筛」")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 播放进度
// ─────────────────────────────────────────────────────────────────────────────

// TestHistoryShowsHowFarYouGot 钉住「播放进度」那一列。
//
// 这是这一页存在的理由（PLAN.md 的 P-04）：没有它，用户看到的是一列标题，得自己
// 回想看过多少，于是每一首都得从头听或者拖进度条试一遍。
func TestHistoryShowsHowFarYouGot(t *testing.T) {
	f := newLibFake(t).withHistory(1, 754, 2700, "看了一半的那首")
	tt, _ := libraryShell(t, f, historyPath)
	waitFrames(t, tt, "把历史记录拉回来", func() bool { return tt.HasText("看了一半的那首") })

	if !tt.HasText("12:34 / 45:00") {
		t.Errorf("没有画出「看了 12:34 / 一共 45:00」，文字有 %q", tt.Texts())
	}
	// 观看时间也要画：精确到分，不精确到秒（上游的 "YYYY-MM-DD HH:mm"）。
	if !tt.HasText("2026-10-16 21:04") {
		t.Errorf("没有画出观看时间，文字有 %q", tt.Texts())
	}
}

// TestAHistoryRowThatWasNeverPlayedShowsZeroProgress 盯「零秒」那一种。
//
// 🔴 **零秒要写成「0:00」，不能留空**：留空的话这一行和一行「看完了」在屏幕上写得
// 一样（都只剩总时长），而这一列存在的全部理由就是让人一眼看出这两个的区别。
// 上游的 formatProgress 是 `formatDuration(prog > 0 ? prog : 0)`，也就是零秒写 0:00。
func TestAHistoryRowThatWasNeverPlayedShowsZeroProgress(t *testing.T) {
	f := newLibFake(t).withHistory(1, 0, 2700, "刚点开就退出的")
	tt, _ := libraryShell(t, f, historyPath)
	waitFrames(t, tt, "把历史记录拉回来", func() bool { return tt.HasText("刚点开就退出的") })

	if !tt.HasText("0:00 / 45:00") {
		t.Errorf("零秒的进度没有写成「0:00 / 45:00」，文字有 %q", tt.Texts())
	}
	if tt.HasText("45:00") && !tt.HasText("0:00 / 45:00") {
		t.Errorf("进度列只剩总时长，看不出有没有听过，文字有 %q", tt.Texts())
	}
}

// TestAHistoryRowWithNoLengthShowsOnlyWhatWasWatched 盯「没有总时长」的那一种。
//
// 直播、专栏那几类 B 站不给 duration。写「0:00 / 0:00」在屏幕上读起来像坏了。
func TestAHistoryRowWithNoLengthShowsOnlyWhatWasWatched(t *testing.T) {
	f := newLibFake(t).withHistory(1, 754, 0, "某场直播")
	tt, _ := libraryShell(t, f, historyPath)
	waitFrames(t, tt, "把历史记录拉回来", func() bool { return tt.HasText("某场直播") })

	if !tt.HasText("12:34") {
		t.Errorf("没有画出看过多少，文字有 %q", tt.Texts())
	}
	if tt.HasText("12:34 / ") {
		t.Errorf("没有总时长却画了「/ 」，文字有 %q", tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 🔴 铁律四：行的身份
// ─────────────────────────────────────────────────────────────────────────────

// TestInsertingARowAtTheHeadKeepsTheChosenSong 钉住行有稳定且唯一的身份。
//
// ⚠️ **这一条是这个列表存在的理由**：MyGo 默认拿「父 ID + 兄弟序号」当身份，头部
// 插入一行之后，不带 key 的列表选中项会变成旁边那一首。
//
// 判据是**画面上那一行的焦点环**（票 #197 之后，选中不再刷实心主色底，环就是
// 「选中画出来了」唯一的证据）。只断言状态字段不够——字段对了画面没跟着变，同样
// 算坏。
//
// ⚠️ **用键盘而不是鼠标**：鼠标点过的那一行**什么标记都没有**（上游点一行不改变它的
// 样子，票 #197 就是这么定的），所以「选中画出来了」这件事只有键盘那条路说得清。
func TestInsertingARowAtTheHeadKeepsTheChosenSong(t *testing.T) {
	f := newLibFake(t).withLater(1, 3, "第一首", "第二首", "第三首")
	tt, s := libraryShell(t, f, laterPath)
	waitFrames(t, tt, "把第一页拉回来", func() bool { return rowsShown(&s.laterPage) == 3 })

	// Tab 把焦点交给列表，方向键往下走两次，落在「第二首」上。
	focusList(t, tt, "第一首")
	tt.Key(0, mygoui.KeyDown)
	tt.Frame()
	tt.Key(0, mygoui.KeyDown)
	tt.Frame()
	if !rowHasRing(t, tt, "第二首") {
		t.Fatalf("键盘走到「第二首」了，那一行上沿却没有焦点环")
	}

	// 在最前面插一行：「新来的一首」排到最上面，原来选中的那首往后挪一位。
	// 直接改已经拿到的行（同一个包里改得到），等价于「筛选变了、内容头部多一行」。
	head := s.laterPage.rows.Rows()[0]
	inserted := head
	inserted.ID, inserted.Title, inserted.Owner = "later:BVnew#170099", "新来的一首", "UP 新"
	s.laterPage.rows.rows = append([]libraryRow{inserted}, s.laterPage.rows.Rows()...)
	s.laterPage.rows.rev++
	tt.Frame()

	if !tt.HasText("新来的一首") {
		t.Fatalf("新插进来的那一行没画出来，文字有 %q", tt.Texts())
	}
	if !rowHasRing(t, tt, "第二首") {
		t.Errorf("头部插入一行之后「第二首」那一行上沿没有焦点环了——" +
			"光标跟着位置走了，没有跟着 ID 走")
	}
	if rowHasRing(t, tt, "新来的一首") {
		t.Error("新插进来的那一行被画成了选中——光标跟着位置走了，没有跟着 ID 走")
	}
	// 拿行问也一样：拿到的还是那一首，不是「第 2 行」。
	if got, ok := s.laterPage.rows.Selected(); !ok || got.Title != "第二首" {
		t.Errorf("Selected() = (%q, %v)，want (第二首, true)", got.Title, ok)
	}
}

// TestTheLaterRowKeepsItsRingAfterAMouseClick 钉的是**评审点出来的那一条**：
// 鼠标点过一行之后再用方向键走位，键盘走到的那一行**必须有指示**。
//
// ⚠️ **这一条是「框架那个 `focusVisible` 靠不住」的证据**：框架只在 Tab 的时候把
// `rt.focusVisible` 设成 true，指针按一下会把它清掉，而**方向键不会设回来**——
// 所以「点一行 → 按 Down」那一路上，框架的判据一路是 false，屏幕上什么都不画。
// 改动之前那一路是好的（实心主色底跟着键盘走），所以不能只满足于框架的判据。
//
// ⚠️ **这一页自己画行**（`library_rows.go` 的文件头写着为什么不能走
// `widgets/list`），所以它得自己证明一遍，不能靠 `widgets/list` 那一条。
func TestTheLaterRowKeepsItsRingAfterAMouseClick(t *testing.T) {
	f := newLibFake(t).withLater(1, 3, "第一首", "第二首", "第三首")
	tt, s := libraryShell(t, f, laterPath)
	waitFrames(t, tt, "把第一页拉回来", func() bool { return rowsShown(&s.laterPage) == 3 })

	// 点第二行：指针在管，那一行什么标记都没有。
	if err := tt.Click("第二首"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if rowHasRing(t, tt, "第二首") {
		t.Errorf("鼠标点过的那一行上沿有焦点环——指针在管的时候不该有")
	}

	// 按方向键：键盘在管了，环得跟着键盘走。
	tt.Key(0, mygoui.KeyDown)
	tt.Frame()
	if !rowHasRing(t, tt, "第三首") {
		t.Errorf("鼠标点过之后按了方向键，键盘走到「第三首」了，那一行上沿却没有焦点环")
	}
	if rowHasRing(t, tt, "第二首") {
		t.Errorf("「第二首」那一行上沿还有焦点环——环没有跟着键盘走")
	}
}

// TestTwoViewingsOfOneVideoAreTwoRows 盯历史记录里的「同一首歌看过两次」。
//
// 🔴 铁律四的另一个说法：两条记录共用一个 bvid。拿 bvid 当身份，那几行的状态就串
// 在一起——点一行，旁边几行一起亮，而用户要点「删除」的到底是哪一条，完全说不清。
func TestTwoViewingsOfOneVideoAreTwoRows(t *testing.T) {
	f := newLibFake(t).withHistory(1, 100, 600, "同一首歌 第一次", "同一首歌 第二次")
	tt, s := libraryShell(t, f, historyPath)
	waitFrames(t, tt, "把两行拉回来", func() bool { return rowsShown(&s.historyPage) == 2 })

	rows := s.historyPage.rows.Rows()
	if rows[0].Item.Bvid != rows[1].Item.Bvid {
		t.Fatal("测试的 fixture 不对：两行应该是同一个 bvid")
	}
	if rows[0].ID == rows[1].ID {
		t.Fatalf("两行的身份一样（%q）——状态会串在一起", rows[0].ID)
	}

	// 键盘走到第二行：环得落在**第二行**上。两行共用身份时，框架会按身份把游标
	// 挪回第一行——那一行是「共用身份」时状态串在一起的证据。
	//
	// ⚠️ **用键盘而不是鼠标**：鼠标点过的那一行什么标记都没有（票 #197：点一行不
	// 改变它的样子），所以「选中画出来了」只有键盘那条路说得清。
	focusList(t, tt, "同一首歌 第一次")
	tt.Key(0, mygoui.KeyDown)
	tt.Frame()
	tt.Key(0, mygoui.KeyDown)
	tt.Frame()
	if !rowHasRing(t, tt, "同一首歌 第二次") {
		t.Errorf("两行共用身份时选中状态串了——环没有落在「同一首歌 第二次」那一行上")
	}
}

// rowPixel 是那一行的底色。
//
// ⚠️ **选中不再有实心主色底**（票 #197）：所以这个数**量不出「选中」**——选中的
// 那一行和没选中的那一行长得一模一样。「选中画出来了」现在只有一件事说得清：
// **键盘走到的那一行有一圈焦点环**（见 `rowHasRing`）。
//
// 取样点在**那一行自己的左边内侧**：`tt.Find` 给的框就是整行（不是那行字），所以
// `X+2` 落在序号那一列上，而不在窗口底色上——钉死像素坐标的话，侧栏宽度一改就
// 全飘。
func rowPixel(t *testing.T, tt *mygoui.Tester, title string) color.RGBA {
	t.Helper()
	r, ok := tt.Find(title)
	if !ok {
		t.Fatalf("画面上找不到「%s」，文字有 %q", title, tt.Texts())
	}
	y, x := int(r.Y+r.H/2), int(r.X)+2
	if y < 0 || y >= 800 || x < 0 || x >= 1200 {
		t.Fatalf("「%s」那一行在画面外（x=%d y=%d）", title, x, y)
	}
	return tt.Image().RGBAAt(x, y)
}
