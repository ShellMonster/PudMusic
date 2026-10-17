package ui

// 「稍后再看」和「历史记录」这一组跑的是**真实外壳 + 真实 B 站客户端 + 一台假
// B 站服务器**：切到那一页、真的画一帧、真的敲字、真的点预设，然后去问那台
// 假服务器「你收到什么查询了」。
//
// 断言的东西只有两类：**画面上的文字**（这一页有没有真画出来、那一列写的是什
// 么）和**发出去的请求**（筛选条件有没有真的变成查询串）。不断言「调了哪个
// 函数」——那只能证明状态层有那个方法，证明不了界面上真有一个入口。

import (
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

// now 是这一组假时钟的「现在」。写死是为了「今天」永远是那一天：这一组里好几条
// 断言的是日期区间，用真时钟的话跨过零点就红了。
var now = time.Date(2026, 10, 16, 21, 4, 0, 0, time.Local)

// ─────────────────────────────────────────────────────────────────────────────
// 假 B 站
// ─────────────────────────────────────────────────────────────────────────────

// libFake 是一台照着给定页码回答的假 B 站，顺便把收到的查询录下来。
type libFake struct {
	*httptest.Server
	mu sync.Mutex
	// pages 是「路径 → 页码 → 返回」。
	later   map[int]string
	history map[int]string
	// seen 是录下来的请求（路径 + 查询）。
	seen []libSeen
	// fail 让某一条路径开始回业务错误（模拟「B 站拒绝了这一页」）。
	fail map[string]bool
}

// libSeen 是一次请求。
type libSeen struct {
	path  string
	query url.Values
}

func newLibFake(t *testing.T) *libFake {
	t.Helper()
	f := &libFake{later: map[int]string{}, history: map[int]string{}, fail: map[string]bool{}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.seen = append(f.seen, libSeen{path: r.URL.Path, query: r.URL.Query()})
		var body string
		if !f.fail[r.URL.Path] {
			switch r.URL.Path {
			case "/x/v2/history/toview/web":
				body = f.later[pageOf(r.URL.Query())]
			case "/x/web-interface/history/search":
				body = f.history[pageOf(r.URL.Query())]
			case "/x/web-interface/nav":
				body = navBody
			}
		}
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if f.fail[r.URL.Path] {
			_, _ = w.Write([]byte(`{"code":-500,"message":"请求过于频繁","ttl":1}`))
			return
		}
		if body == "" {
			_, _ = w.Write([]byte(`{"code":0,"message":"0","ttl":1,"data":{"count":0,"list":[],"has_more":false}}`))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(f.Close)
	return f
}

// navBody 是签名要用的那一次 nav（`wbi.go` 先打它拿密钥）。
const navBody = `{"code":0,"message":"0","ttl":1,"data":{"isLogin":true,
  "wbi_img":{"img_url":"https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png",
             "sub_url":"https://i0.hdslb.com/bfs/wbi/4932caff0ff746eab6f01bf08b70ac45.png"}}}`

func pageOf(q url.Values) int {
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

// withHistory 装一页历史记录的内容。progress / duration 是同一条歌的进度和总长。
func (f *libFake) withHistory(page int, progress, duration int, titles ...string) *libFake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.history[page] = historyBody(progress, duration, page*100, titles)
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

// failOn 让某一条路径从现在起回业务错误。
func (f *libFake) failOn(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail[path] = true
}

const laterPathAPI = "/x/v2/history/toview/web"
const historyPathAPI = "/x/web-interface/history/search"

// laterBody 造一页稍后再看的返回。**seed 让每一页的行身份互不相同**——翻页
// 撞了身份会被去重吃掉，而那在测试里表现为「第二页明明有内容却没加上」。
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

// historyBody 造一页历史记录的返回。同一首歌的每一条都是不同的观看时刻。
func historyBody(progress, duration, seed int, titles []string) string {
	items := make([]map[string]any, 0, len(titles))
	for i, title := range titles {
		items = append(items, map[string]any{
			"title": title, "cover": "", "author_name": "UP " + title, "author_mid": 11,
			"view_at":  now.Add(-time.Duration(i) * time.Hour).Unix(),
			"progress": progress, "duration": duration,
			"kid":     fmt.Sprintf("archive_%d", 170001+i),
			"history": map[string]any{"bvid": fmt.Sprintf("BVhist%02d", i), "cid": 4001 + i, "business": "archive"},
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

// ─────────────────────────────────────────────────────────────────────────────
// 外壳
// ─────────────────────────────────────────────────────────────────────────────

// libraryShell 造一个停在 path 上的外壳，B 站接口指向 f。
//
// **假时钟**是这里的关键：`p.dates` 的「今天」和防抖都靠它，写死成 `now`，
// 于是「今天」永远是 2026-10-16，而「敲完字等 500ms」也不用真睡。
func libraryShell(t *testing.T, f *libFake, path string) (*mygoui.Tester, *Shell) {
	t.Helper()

	a := app.New()
	t.Cleanup(a.Close)
	cli := bili.NewClient(bili.Options{
		// 用 httptest 自带的那份 transport——它认识那张自签证书。
		Transport: f.Server.Client().Transport,
		CSRF:      func() string { return "the-jct" },
	})
	// ⚠️ 不指到 fixture 上的话这些用例会打到**真的** B 站：不报错、结果还挺
	// 合理，于是测试变成「取决于能不能连上 B 站」。
	cli.TestOnlyPointHostsAtFixture(f.URL)
	a.MountLibrary(cli)

	s := New(a)
	t.Cleanup(s.images.Close)
	t.Cleanup(func() { _ = s.hist.Close() })
	// 理由同 newShell：切页动画在 Tester 里永远走不完，留着它只会让「读到的是
	// 哪一帧」变成随机的。
	s.router.Transition = mygoui.TransitionNone
	s.clock = newFakeClock(now)

	tt := mygoui.NewTester(s.View, 1200, 800)
	tt.Frame()
	s.navigate(path)
	tt.Frame()
	return tt, s
}

// settle 跑到 cond 成立为止。每一轮走一帧，顺带让后台那一支有机会跑。
//
// ⚠️ 不能只 `tt.Frame()` 一次：拉页在后台 goroutine 里，结果要靠唤醒才进下一帧。
func settle(t *testing.T, tt *mygoui.Tester, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		tt.Frame()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等了三秒还没%s，文字有 %q", what, tt.Texts())
		}
		time.Sleep(time.Millisecond)
	}
}

// rowsShown 是画面上画出来的那几行（pagedRows 手上已经拿到的）。
func rowsShown(p *libraryPage) int { return len(p.rows.Rows()) }

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
				"起始日期", "结束日期",
			}, c.want...) {
				if !tt.HasText(want) {
					t.Errorf("%s 上没有 %q，文字有 %q", c.title, want, tt.Texts())
				}
			}

			// 内容真的来了，不是转圈。
			settle(t, tt, "把内容拉回来", func() bool { return rowsShown(s.pageOf(c.path)) == 3 })
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

// TestTheEmptyStateSaysWhyAndHow 钉住「一行都没有」时画的那两句话。
//
// 两句都要在：第一句说这里为什么空，第二句给一个**能做的下一步**。这一页尤其
// 需要第二句——用户常常是筛完了才空的，他要的是「怎么才能不空」，不是再看一遍
// 「这里什么都没有」。
//
// 文案是**从 `routes` 表里那一条拿的**（出处标在那儿），所以这里断言的也是那
// 一条：另抄一份就多一个会对不上的来源，而对不上时用户看不出任何异样。
func TestTheEmptyStateSaysWhyAndHow(t *testing.T) {
	for _, path := range []string{laterPath, historyPath} {
		t.Run(path, func(t *testing.T) {
			// 一页都不给，就是「什么都没有」那一支。
			f := newLibFake(t)
			tt, s := libraryShell(t, f, path)

			e := lookup(path).Empty
			if e.Title == "" || e.Hint == "" {
				t.Fatalf("routes 表里 %s 那一条的空状态不完整：%+v", path, e)
			}
			settle(t, tt, "把空状态画出来", func() bool { return tt.HasText(e.Title) })
			if !tt.HasText(e.Hint) {
				t.Errorf("%s 空着的时候没说「怎么才能不空」，文字有 %q", path, tt.Texts())
			}
			_ = s
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
			s, tt := newShell(t) // ⚠️ 它**不挂** B 站客户端，这正是这一条要的
			tt.Frame()
			s.navigate(path)
			tt.Frame()

			e := lookup(path).Empty
			if !tt.HasText(e.Title) {
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

// TestTheKeywordReachesTheServerAfterTheDebounce 盯「敲的字变成了查询串」。
//
// 两条一起钉：
//   - **敲完不能立刻发**：不防抖的话每敲一个字发一次，B 站的风控不是摆设。
//   - **500ms 之后一定要发**：漏了防抖的唤醒就是「用户敲完了，什么都没发生」——
//     界面上和「B 站忽略了那个词」一模一样。
func TestTheKeywordReachesTheServerAfterTheDebounce(t *testing.T) {
	f := newLibFake(t).withLater(1, 1, "甲")
	tt, s := libraryShell(t, f, laterPath)
	settle(t, tt, "把第一页拉回来", func() bool { return f.callsOf(laterPathAPI) == 1 })

	typeKeyword(t, tt, "周杰伦")
	tt.Frame()

	if got := f.callsOf(laterPathAPI); got != 1 {
		t.Fatalf("敲完字立刻就发了请求（共 %d 次）——防抖没起作用", got)
	}
	// 推过 500ms。⚠️ **必须显式 Advance**：Tester 的钟站着不动，
	// 「框不动了」不等于「时间到了」。
	tt.Frame()
	if got := f.callsOf(laterPathAPI); got != 1 {
		t.Fatalf("还没到 500ms 就发了（共 %d 次）", got)
	}

	// 假时钟在壳上，所以这一步就相当于「又过了半秒」。
	advance(s, 600*time.Millisecond)
	settle(t, tt, "把新条件的那一页拉回来", func() bool { return f.callsOf(laterPathAPI) == 2 })

	q := f.queriesOf(laterPathAPI)[1]
	if got := q.Get("key"); got != "周杰伦" {
		t.Errorf("查询串里 key = %q，want 周杰伦", got)
	}
}

// TestEnterSearchesWithoutWaiting 盯回车不等那 500ms（上游也是 `onKeyDown Enter`）。
func TestEnterSearchesWithoutWaiting(t *testing.T) {
	f := newLibFake(t).withLater(1, 1, "甲")
	tt, _ := libraryShell(t, f, laterPath)
	settle(t, tt, "把第一页拉回来", func() bool { return f.callsOf(laterPathAPI) == 1 })

	typeKeyword(t, tt, "林俊杰")
	tt.TypeKey(0, mygoui.KeyEnter, "\r")
	tt.Frame()
	settle(t, tt, "回车之后立刻就搜了", func() bool { return f.callsOf(laterPathAPI) == 2 })

	if got := f.queriesOf(laterPathAPI)[1].Get("key"); got != "林俊杰" {
		t.Errorf("回车之后查询串里 key = %q", got)
	}
}

// TestChangingTheKeywordThrowsTheOldPageAway 钉住「旧查询的结果作废」。
//
// 用户连点两个词时，第一个请求的结果可能后到。盖掉新条件的结果，界面上就是
// 「搜的是新词、显示的是旧词的歌」——用户无从判断，只能刷新。
func TestChangingTheKeywordThrowsTheOldPageAway(t *testing.T) {
	f := newLibFake(t).withLater(1, 1, "旧词的第一页")
	tt, s := libraryShell(t, f, laterPath)
	settle(t, tt, "把第一页拉回来", func() bool { return rowsShown(&s.laterPage) == 1 })

	// 换成新词，并把新词那一页的内容改掉。
	f.withLater(1, 1, "新词的第一页")
	typeKeyword(t, tt, "新词")
	advance(s, 600*time.Millisecond)
	tt.TypeKey(0, mygoui.KeyEnter, "\r")
	tt.Frame()
	settle(t, tt, "把新条件的那一页拉回来", func() bool {
		return tt.HasText("新词的第一页") && !tt.HasText("旧词的第一页")
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 日期范围
// ─────────────────────────────────────────────────────────────────────────────

// TestTheFourPresetsAskForThatRange 盯四个预设各自算出什么。
//
// 逐个点是因为它们错的方式不一样：今天和昨天差一天（差错了就是「昨天搜不出昨天
// 看过的」，用户以为功能坏了）；近一周是往前 7 天而不是「这一周」。
func TestTheFourPresetsAskForThatRange(t *testing.T) {
	for _, c := range []struct {
		label string
		// wantStart / wantEnd 是那一天的 00:00:00 和 23:59:59（零值 = 不筛）。
		wantStart, wantEnd time.Time
	}{
		{label: "全部时间", wantStart: time.Time{}, wantEnd: time.Time{}},
		{label: "今天", wantStart: day(2026, 10, 16), wantEnd: day(2026, 10, 16).Add(24*time.Hour - time.Second)},
		{label: "昨天", wantStart: day(2026, 10, 15), wantEnd: day(2026, 10, 15).Add(24*time.Hour - time.Second)},
		{label: "近一周", wantStart: day(2026, 10, 9), wantEnd: day(2026, 10, 16).Add(24*time.Hour - time.Second)},
	} {
		t.Run(c.label, func(t *testing.T) {
			f := newLibFake(t).withLater(1, 1, "甲")
			tt, _ := libraryShell(t, f, laterPath)
			settle(t, tt, "把第一页拉回来", func() bool { return f.callsOf(laterPathAPI) == 1 })

			if err := tt.Click(c.label); err != nil {
				t.Fatalf("点不到「%s」：%v，文字有 %q", c.label, err, tt.Texts())
			}
			tt.Frame()
			settle(t, tt, "按新的日期重新拉", func() bool { return f.callsOf(laterPathAPI) == 2 })

			q := f.queriesOf(laterPathAPI)[1]
			gotStart := q.Get("add_time_start")
			gotEnd := q.Get("add_time_end")
			if c.wantStart.IsZero() {
				if gotStart != "" || gotEnd != "" {
					t.Errorf("「%s」不该发日期参数，却发了 %q ~ %q", c.label, gotStart, gotEnd)
				}
				return
			}
			if want := strconv.FormatInt(c.wantStart.Unix(), 10); gotStart != want {
				t.Errorf("add_time_start = %q，want %q", gotStart, want)
			}
			if want := strconv.FormatInt(c.wantEnd.Unix(), 10); gotEnd != want {
				t.Errorf("add_time_end = %q，want %q", gotEnd, want)
			}
		})
	}
}

// TestTheFilterSaysWhatItIsFiltering 盯筛选状态那一句话。
//
// 「全部时间」亮着、两个输入框里却摆着日期——不加这一句话，用户以为今天正在筛。
func TestTheFilterSaysWhatItIsFiltering(t *testing.T) {
	f := newLibFake(t).withLater(1, 1, "甲")
	tt, _ := libraryShell(t, f, laterPath)

	if !tt.HasText("不按日期筛") {
		t.Errorf("默认没有说清不筛，文字有 %q", tt.Texts())
	}
	if err := tt.Click("昨天"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("2026-10-15 ~ 2026-10-15") {
		t.Errorf("选了昨天之后没有说清筛的是哪一段，文字有 %q", tt.Texts())
	}
}

// TestTheTwoDateInputsDriveTheFilter 钉住「手动区间」这一半。
//
// 只点预设的话，「手动挑一天」那条路就没人走过——而它是日期筛选里唯一能让用户
// 挑到**今天没有预设覆盖**的那一段的办法。
func TestTheTwoDateInputsDriveTheFilter(t *testing.T) {
	f := newLibFake(t).withLater(1, 1, "甲")
	tt, _ := libraryShell(t, f, laterPath)
	settle(t, tt, "把第一页拉回来", func() bool { return f.callsOf(laterPathAPI) == 1 })

	// 点开起始日期那一格，它会弹出一个月的日历。
	if err := tt.Click("起始日期"); err != nil {
		t.Fatalf("点不到起始日期：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
	// 日历上是 2026 年 10 月，点 3 号（默认那个月的月初前几行）。
	if !tt.HasText("October 2026") && !tt.HasText("2026年10月") {
		t.Fatalf("日历没打开，文字有 %q", tt.Texts())
	}
	if err := tt.Click("October 3, 2026"); err != nil {
		t.Fatalf("日历上点不到 10 月 3 号：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()

	settle(t, tt, "按手动挑的日期重新拉", func() bool { return f.callsOf(laterPathAPI) == 2 })
	q := f.queriesOf(laterPathAPI)[1]
	if got := q.Get("add_time_start"); got != strconv.FormatInt(day(2026, 10, 3).Unix(), 10) {
		t.Errorf("手动挑了一天之后 add_time_start = %q，want 2026-10-03 那一秒", got)
	}
	// 手动挑过之后四个预设一个都不该亮着。
	if tt.HasText("不按日期筛") {
		t.Error("手动挑了日期，界面上还说着「不按日期筛」")
	}
}

// day 是一天的 00:00:00（本地时区）。
func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

// advance 把假时钟往前推 d，再走一帧。
//
// ⚠️ **必须显式 Advance**：Tester 的钟站着不动（只有 Advance 推得动），
// 「框不动了」不等于「动画停了」——用它当「时间到了」的判据会在动画中途放行。
func advance(s *Shell, d time.Duration) {
	s.clock.(*fakeClock).Advance(d)
}

// ─────────────────────────────────────────────────────────────────────────────
// 播放进度
// ─────────────────────────────────────────────────────────────────────────────

// TestHistoryShowsHowFarYouGot 钉住「播放进度」那一列。
//
// 这是这一页存在的理由（PLAN.md 的 P-04）：没有它，用户看到的是一列标题，得
// 自己回想看过多少，于是每一首都得从头听或者拖进度条试一遍。
func TestHistoryShowsHowFarYouGot(t *testing.T) {
	f := newLibFake(t).withHistory(1, 754, 2700, "看了一半的那首")
	tt, _ := libraryShell(t, f, historyPath)
	settle(t, tt, "把历史记录拉回来", func() bool { return tt.HasText("看了一半的那首") })

	if !tt.HasText("12:34 / 45:00") {
		t.Errorf("没有画出「看了 12:34 / 一共 45:00」，文字有 %q", tt.Texts())
	}
	// 观看时间也要画：精确到分，不精确到秒（上游的 "YYYY-MM-DD HH:mm"）。
	if !tt.HasText("2026-10-16 21:04") {
		t.Errorf("没有画出观看时间，文字有 %q", tt.Texts())
	}
	// 「全部时间」不筛的时候那一列仍然要有东西——它是**这一行的事实**，不是筛选。
	if tt.HasText("不按日期筛") != true {
		t.Error("测试自己出了问题：默认没在筛")
	}
}

// TestAHistoryRowWithNoLengthShowsOnlyWhatWasWatched 盯「没有总时长」的那一种。
//
// 直播、专栏那几类 B 站不给 duration。写「0:00 / 0:00」在屏幕上读起来像坏了。
func TestAHistoryRowWithNoLengthShowsOnlyWhatWasWatched(t *testing.T) {
	f := newLibFake(t).withHistory(1, 754, 0, "某场直播")
	tt, _ := libraryShell(t, f, historyPath)
	settle(t, tt, "把历史记录拉回来", func() bool { return tt.HasText("某场直播") })

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
// ⚠️ **这一条是这个列表存在的理由**：MyGo 默认拿「父 ID + 兄弟序号」当身份，
// 头部插入一行之后，不带 key 的列表选中项会变成旁边那一首。
//
// 判据是**画面上那一行的底色**（选中行是主色底，别的行不是）。只断言状态字段
// 不够——字段对了画面没跟着变，同样算坏。
func TestInsertingARowAtTheHeadKeepsTheChosenSong(t *testing.T) {
	f := newLibFake(t).withLater(1, 3, "第一首", "第二首", "第三首")
	tt, s := libraryShell(t, f, laterPath)
	settle(t, tt, "把第一页拉回来", func() bool { return rowsShown(&s.laterPage) == 3 })

	if err := tt.Click("第二首"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	chosen := rowPixel(t, tt, "第二首")
	if chosen == rowPixel(t, tt, "第三首") {
		t.Fatalf("选中的行和没选中的行是同一个底色（%v），选中根本没画出来", chosen)
	}

	// 在最前面插一行：「新来的一首」排到最上面，原来选中的那首往后挪一位。
	// 直接改已经拿到的行（同一个包里改得到），等价于「筛选变了、内容头部多一行」。
	head := s.laterPage.rows.Rows()[0]
	inserted := head
	inserted.ID = "later:BVnew#170099"
	inserted.Title = "新来的一首"
	inserted.Owner = "UP 新"
	s.laterPage.rows.rows = append([]libraryRow{inserted}, s.laterPage.rows.Rows()...)
	s.laterPage.rows.rev++
	tt.Frame()

	if !tt.HasText("新来的一首") {
		t.Fatalf("新插进来的那一行没画出来，文字有 %q", tt.Texts())
	}
	if got := rowPixel(t, tt, "第二首"); got != chosen {
		t.Errorf("头部插入一行之后「第二首」那一行是 %v，插入前是 %v，want 一样"+
			"（选中的还是那一首）", got, chosen)
	}
	if got := rowPixel(t, tt, "新来的一首"); got == chosen {
		t.Error("新插进来的那一行被画成了选中——选中跟位置走了，没有跟着 ID 走")
	}

	// 拿行问也一样：拿到的还是那一首，不是「第 2 行」。
	got, ok := s.laterPage.rows.Selected()
	if !ok || got.Title != "第二首" {
		t.Errorf("Selected() = (%q, %v)，want (第二首, true)", got.Title, ok)
	}
}

// rowPixel 是那一行的底色。选中行是主色底（Biu 那个绿），别的行不是。
//
// 取样点在**那一行自己的左边内侧**：`tt.Find` 给的框就是整行（不是那行字），
// 所以 `X+2` 落在序号那一列上，而不在窗口底色上——钉死像素坐标的话，侧栏宽度
// 一改就全飘。
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

// TestTwoViewingsOfOneVideoAreTwoRows 盯历史记录里的「同一首歌看过两次」。
//
// 🔴 铁律四的另一个说法：B 站的历史记录里，同一首歌会出现好几条（每一遍观看
// 一条），它们共用一个 bvid。拿 bvid 当身份，那几行的状态就串在一起——点一行，
// 旁边几行一起亮，而用户要点「删除」的到底是哪一条，完全说不清。
func TestTwoViewingsOfOneVideoAreTwoRows(t *testing.T) {
	body := biliOK(map[string]any{"has_more": false, "page": map[string]any{"pn": 1},
		"list": []map[string]any{
			{"title": "同一首歌 第一次", "view_at": now.Unix(), "progress": 100, "duration": 600,
				"kid": "archive_1", "history": map[string]any{"bvid": "BVSAME", "cid": 1, "business": "archive"}},
			{"title": "同一首歌 第二次", "view_at": now.Add(-time.Hour).Unix(), "progress": 200, "duration": 600,
				"kid": "archive_1", "history": map[string]any{"bvid": "BVSAME", "cid": 1, "business": "archive"}},
		}})
	f := newLibFake(t)
	f.mu.Lock()
	f.history[1] = body
	f.mu.Unlock()

	tt, s := libraryShell(t, f, historyPath)
	settle(t, tt, "把两行拉回来", func() bool { return rowsShown(&s.historyPage) == 2 })

	rows := s.historyPage.rows.Rows()
	if rows[0].ID == rows[1].ID {
		t.Fatalf("两行的身份一样（%q）——状态会串在一起", rows[0].ID)
	}
	if rows[0].Item.Bvid != rows[1].Item.Bvid {
		t.Fatal("测试的 fixture 不对：两行应该是同一个 bvid")
	}

	// 选中第一行，第二行不该跟着亮。
	if err := tt.Click("同一首歌 第一次"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	a := rowPixel(t, tt, "同一首歌 第一次")
	b := rowPixel(t, tt, "同一首歌 第二次")
	if a == b {
		t.Errorf("两行共用身份时选中状态串了（都是 %v）", a)
	}
}
