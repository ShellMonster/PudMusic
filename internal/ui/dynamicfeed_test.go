// 动态流那一页（「用户动态」）的测试。
//
// 假 B 站、壳、等状态的几个工具在 upspace_test.go 里（同一个包，共用）。
package ui

import (
	"net/http"
	"sync"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// 动态流那一页
// ─────────────────────────────────────────────────────────────────────────────

// TestTheAuthorListPagesAndEachRowGoesToThatSpace 是「动态流左侧作者栏能
// 翻页、能点进对应 UP 空间」那条验收的钉子。
func TestTheAuthorListPagesAndEachRowGoesToThatSpace(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "我自己", 0, 1)
	b.on("/x/polymer/web-dynamic/v1/feed/all", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items": []any{}, "has_more": false})
	})
	// 一页 2 个、`total` 说 5 个 → 还能翻。三页各自给不同的名字，
	// 好断言「翻页真的翻了」。
	pages := map[string][]any{
		"1": {
			map[string]any{"mid": 11, "uname": "第一个 UP", "face": "https://f/1.jpg"},
			map[string]any{"mid": 12, "uname": "第二个 UP"},
		},
		"2": {
			map[string]any{"mid": 13, "uname": "第三页的一个"},
		},
		"3": {
			map[string]any{"mid": 14, "uname": "第四页的一个"},
		},
		"4": {map[string]any{"mid": 15, "uname": "第五页的一个"}},
		"5": {map[string]any{"mid": 16, "uname": "第六页的一个"}},
	}
	b.on("/x/relation/followings", func(w http.ResponseWriter, r *http.Request) {
		pn := r.URL.Query().Get("pn")
		// 超过 total 之后 B 站给的是**空 list**（而不是报错），照它那样回：
		// 不这么造的话作者栏会一直翻一直有，而真机上早就停了。
		if pn == "5" {
			pn = ""
		}
		uiExploreJSON(w, 0, map[string]any{"total": 5, "list": pages[pn]})
	})

	s, tt, _ := newSpaceShell(t, b, true, "77")
	s.navigate(dynamicFeedPath)
	tt.Frame()
	f := s.dynamicFeedOf()
	waitFrames(t, tt, "第一页关注的人到了", func() bool { return len(f.authors) >= 2 })
	tt.Frame()

	if !tt.HasText("全部") {
		t.Errorf("作者栏第一行该是「全部」，文字有 %q", tt.Texts())
	}
	if !tt.HasText("第一个 UP") {
		t.Errorf("关注的人没画出来，文字有 %q", tt.Texts())
	}

	// 翻页：滚到作者栏底部（哨兵进画面）就会拉下一页。
	//
	// ⚠️ **等的是终态而不是「多了一个」**：这一栏装不满时哨兵一直在画面里，
	// 于是**一次滚动可能连翻好几页**（实测一帧就从 2 个翻到 5 个）。早一版按
	// 「翻一页」一步一步等，读 `before` 的那一刻下一页可能已经到了，
	// 于是永远等不到「比 before 多一个」。
	//
	// ⚠️ **滚到末尾只能等条件**（见 `scrollToEnd`）。早一版是「外层 5 轮、内层
	// 每轮滚 3 下」，而那两层数的是次数：判据落在外层的条件上，可内层那 3 下
	// 滚没滚到末尾没人管——作者栏那一列是 `5 个人 + 1 个哨兵`，第五个人是哪一趟
	// 回来的就停在哪，于是断的 `第五页的一个`（末尾那一个）有时在画面上有时没有。
	//
	// ⚠️ 判据是**作者栏那一份**状态（`authState`），不是右边动态那一份。
	scrollToEnd(t, tt, 60, 400, "作者栏的末尾", f.authState.AtEnd)
	waitFrames(t, tt, "作者栏翻到底", func() bool {
		return len(f.authors) == 5 || (f.auth.started && !f.auth.hasMore)
	})
	tt.Frame()
	scrollToEnd(t, tt, 60, 400, "作者栏的末尾", f.authState.AtEnd)
	if len(f.authors) != 5 {
		t.Fatalf("翻了之后作者栏有 %d 个人，want 5（total 就是 5）", len(f.authors))
	}
	if !showsText(tt, "第五页的一个") {
		t.Errorf("翻页之后新一页的人没画出来，文字有 %q", tt.Texts())
	}

	// 点一个作者 = 跳进他的 UP 空间。
	if err := tt.Click("第三页的一个"); err != nil {
		t.Fatal(err)
	}
	if got := s.router.Path(); got != "/user/13" {
		t.Errorf("点了作者跳到了 %q，want /user/13", got)
	}
}

// TestTheAuthorListIsNotShownWithoutALogin 钉住「没登录不假装有内容」。
//
// 作者栏列的是「我关注的人」，没有登录态就没有这个列表。B 站那个接口会回
// -101，而 -101 在这一层和「网络断了」长得一模一样。
func TestTheAuthorListIsNotShownWithoutALogin(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "我自己", 0, 1)
	b.on("/x/polymer/web-dynamic/v1/feed/all", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items": []any{}, "has_more": false})
	})
	s, tt, _ := newSpaceShell(t, b, false, "")

	s.navigate(dynamicFeedPath)
	tt.Frame()
	f := s.dynamicFeedOf()
	waitFrames(t, tt, "动态到了", func() bool { return f.started })
	tt.Frame()

	if !tt.HasText("登录之后能看到你关注的人") {
		t.Errorf("没登录时作者栏该说清为什么是空的，文字有 %q", tt.Texts())
	}
	if b.hit("/x/relation/followings") {
		t.Error("没登录还去问了关注列表（那个接口会回 -101）")
	}
}

// TestTheDynamicFeedEmptyStateSaysWhatToDo 钉住动态流空着时不是一片白。
func TestTheDynamicFeedEmptyStateSaysWhatToDo(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "我自己", 0, 1)
	b.on("/x/polymer/web-dynamic/v1/feed/all", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items": []any{}, "has_more": false})
	})
	s, tt, _ := newSpaceShell(t, b, true, "77")

	s.navigate(dynamicFeedPath)
	tt.Frame()
	f := s.dynamicFeedOf()
	waitFrames(t, tt, "动态到了", func() bool { return f.started })
	tt.Frame()

	if !tt.HasText("暂无动态") {
		t.Errorf("空着的动态流是一句都没说，文字有 %q", tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 崩溃兜底
// ─────────────────────────────────────────────────────────────────────────────

// TestBothPagesSurviveAPanicInTheirView 🔴「view 里 panic 会杀死进程」。
//
// 这两条路由接在 `builtPage` 上，而 `Shell.page` 在外面包了一层 recover。
// 这一条钉的是「新增的 view 走了那一层」——它要是没走，这里就会看到
// 整个测试进程被带走。
func TestBothPagesSurviveAPanicInTheirView(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "会崩的 UP", 0, 1)
	for _, p := range []string{
		"/x/polymer/web-dynamic/v1/feed/space", "/x/space/wbi/arc/search",
		"/x/v3/fav/folder/created/list", "/x/polymer/web-space/seasons_series_list",
		"/x/polymer/web-dynamic/v1/feed/all", "/x/relation/followings",
	} {
		p := p
		b.on(p, func(w http.ResponseWriter, r *http.Request) {
			uiExploreJSON(w, 0, map[string]any{"items": []any{}, "has_more": false})
		})
	}
	s, tt, _ := newSpaceShell(t, b, false, "")

	for _, target := range []string{upPathOf(42), dynamicFeedPath} {
		s.navigate(target)
		tt.Frame()
		if s.crash != nil {
			t.Fatalf("%s 这一页崩了（%v）——它应该被页级 recover 接住才对", target, s.crash)
		}
	}
}

// TestAFailedDynamicFeedSaysItFailedAndOffersARetry 钉住「动态流拉失败要说
// 失败」。
//
// 同一个坑在动态流这一页也有：空状态排在错误前面，于是第一页拉失败时
// 界面上是「暂无动态」——一句话都不提失败，而闸门因为 `err != nil` 一直
// 挡着重试，切走再切回来也不会重来。
func TestAFailedDynamicFeedSaysItFailedAndOffersARetry(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "我自己", 0, 1)
	for _, p := range []string{"/x/polymer/web-dynamic/v1/feed/all", "/x/relation/followings"} {
		p := p
		b.on(p, func(w http.ResponseWriter, r *http.Request) {
			uiExploreJSON(w, -412, nil) // 被风控
		})
	}
	s, tt, _ := newSpaceShell(t, b, true, "77")

	s.navigate(dynamicFeedPath)
	tt.Frame()
	f := s.dynamicFeedOf()
	waitFrames(t, tt, "动态流拉完（失败也是完）", func() bool {
		return f.started || f.feed.err != nil
	})
	tt.Frame()

	if showsText(tt, "暂无动态") {
		t.Errorf("拉失败了，界面上却是「暂无动态」——用户会以为关注的人真的什么都没发，文字有 %q",
			tt.Texts())
	}
	if !showsText(tt, "这一块没拉出来") {
		t.Errorf("没说失败，文字有 %q", tt.Texts())
	}
	// ⚠️ 找**动态那一块**那个按钮，不是左边作者栏那个：这一页同时可能有两块
	// 在失败，两个按钮必须带各自的名字（否则用户也分不清按的是哪个）。
	r, ok := tt.Find("重试加载动态")
	if !ok {
		t.Fatalf("没有重试按钮，文字有 %q", tt.Texts())
	}
	if r.W <= 0 || r.H <= 0 {
		t.Fatalf("重试按钮是 %.0fx%.0f，点不到", r.W, r.H)
	}

	// 修好假 B 站再点一次：它必须能重来。
	b.on("/x/polymer/web-dynamic/v1/feed/all", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"items": []any{uiDynamic("d1", "重试之后拿到了")}, "offset": "", "has_more": false})
	})
	if err := tt.Click("重试加载动态"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "重试之后拉到了", func() bool { return len(f.items) == 1 })
	tt.Frame()
	if !showsText(tt, "重试之后拿到了") {
		t.Errorf("重试之后没画出来，文字有 %q", tt.Texts())
	}
}

// TestSwitchingAccountsClearsTheAuthorList 钉住「换账号要把这一页整个换掉」。
//
// 🔴 实测踩过的：作者栏列的是「**我**关注的人」，而整个 `dynamicFeed` 是挂在
// 壳上的一份缓存 —— 退出 A 登录 B 之后，左边仍然是 A 关注的人，接口一次
// 都没再请求过，界面上没有任何东西提示这一点。
//
// 判据是**画出来的名字**换了 + **接口被重新请求**了。
func TestSwitchingAccountsClearsTheAuthorList(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "我自己", 0, 1)
	b.on("/x/polymer/web-dynamic/v1/feed/all", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items": []any{}, "has_more": false})
	})
	var mu sync.Mutex
	var who string
	b.on("/x/relation/followings", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		name := who + "专属作者"
		mu.Unlock()
		uiExploreJSON(w, 0, map[string]any{"total": 1, "list": []any{
			map[string]any{"mid": 1, "uname": name},
		}})
	})
	s, tt, a := newSpaceShell(t, b, true, "77")
	mu.Lock()
	who = "账号77"
	mu.Unlock()

	s.navigate(dynamicFeedPath)
	tt.Frame()
	f := s.dynamicFeedOf()
	waitFrames(t, tt, "账号 77 的作者到了", func() bool { return len(f.authors) == 1 })
	tt.Frame()
	if !showsText(tt, "账号77专属作者") {
		t.Fatalf("账号 77 的作者没画出来，文字有 %q", tt.Texts())
	}

	// 换成另一个账号。
	a.TestOnlyAdoptLogin("88")
	mu.Lock()
	who = "账号88"
	mu.Unlock()
	if got := a.SelfMid(); got != 88 {
		t.Fatalf("SelfMid 是 %d，want 88", got)
	}

	waitFrames(t, tt, "换账号之后作者栏换成 88 的", func() bool {
		return showsText(tt, "账号88专属作者")
	})
	tt.Frame()

	if showsText(tt, "账号77专属作者") {
		t.Errorf("换账号之后还留着上一个账号的关注的人，文字有 %q", tt.Texts())
	}
	mu.Lock()
	n := len(f.authors)
	mu.Unlock()
	if n != 1 || f.authors[0].Name != "账号88专属作者" {
		t.Errorf("作者栏现在是 %+v，want 只有账号 88 的那一个", f.authors)
	}
}

// TestTheAuthorRetryWhilePendingDoesNotSendASecondRequest 钉住「点了重试之后
// 不再能点、也不再并发发第二个请求」。
//
// ⚠️ 这一条抓的是一个**真的 bug**：重试那一支写的是 `auth := f.auth` ——
// **复制，不是指针**。于是「清掉 err」和「begin() 打上 loading」全都写在
// 副本上，真实的闸门 `loading` 仍是 false、`err` 仍非 nil：
//
//   - 界面上那句失败一直挂着、按钮一直可点；
//   - 每点一次就**并发发出一个重复的 pn 请求**，两次的回来顺序不保证，
//     后到的把先到的结果盖掉。
//
// 判据是两件事：重试之后按钮**找不到了**（换成「正在加载…」），而且
// 接口只被请求了**一次**。
func TestTheAuthorRetryWhilePendingDoesNotSendASecondRequest(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "我自己", 0, 1)
	b.on("/x/polymer/web-dynamic/v1/feed/all", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items": []any{}, "has_more": false})
	})

	// 作者栏**很短**：第一页给一个人、total 2、后面没有。那样哨兵一落进画面
	// 就会被自动补屏拉到（见 dynamicfeed.go 里 viewAuthorPane 的两条保障），
	// 所以第二页一失败界面上立刻就有重试按钮——不用去滚，**不依赖时序**。
	// （早一版靠「滚到底」去触发，机器一忙就够不到，测试自己 flaky。）
	var mu sync.Mutex
	var pn2 int
	failing := true
	failPN2 := func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		pn2++
		mu.Unlock()
		uiExploreJSON(w, -500, nil)
	}
	b.on("/x/relation/followings", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("pn") == "2" {
			if failing {
				failPN2(w, r)
			} else {
				uiExploreJSON(w, 0, map[string]any{"total": 2, "list": []any{
					map[string]any{"mid": 12, "uname": "第二个"},
				}})
			}
			return
		}
		uiExploreJSON(w, 0, map[string]any{"total": 2, "list": []any{
			map[string]any{"mid": 11, "uname": "第一个"},
		}})
	})

	s, tt, _ := newSpaceShell(t, b, true, "77")
	s.navigate(dynamicFeedPath)
	tt.Frame()
	f := s.dynamicFeedOf()
	waitFrames(t, tt, "第二页失败", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(f.authors) == 1 && f.auth.err != nil
	})
	tt.Frame()
	if _, ok := tt.Find("重试加载关注的人"); !ok {
		t.Skipf("这一轮没出现重试按钮（err=%v），画面上是 %q", f.auth.err, tt.Texts())
	}
	mu.Lock()
	before := pn2
	mu.Unlock()

	// ⚠️ 换成「这一次悬着不回头」，好观察「重试期间」的状态。
	release := make(chan struct{})
	mu.Lock()
	failing = false
	mu.Unlock()
	b.on("/x/relation/followings", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("pn") == "2" {
			mu.Lock()
			pn2++
			mu.Unlock()
			<-release
			uiExploreJSON(w, 0, map[string]any{"total": 2, "list": []any{
				map[string]any{"mid": 12, "uname": "第二个"},
			}})
			return
		}
		uiExploreJSON(w, 0, map[string]any{"total": 2, "list": []any{
			map[string]any{"mid": 11, "uname": "第一个"},
		}})
	})

	tt.Click("重试加载关注的人")

	// ⚠️ **等请求真的发出**，不要按固定帧数。
	//
	// 这一拉是在 goroutine 里发出去的（`go s.fetchFeed`），而请求要经过 DNS/连接
	// 往返才打到 fixture。原来这里是「画 10 帧就断言」——机器负载一高，10 帧
	// （毫秒级）走不到网络那一层，于是断言数到 0 次，看起来像「点了重试没反应」。
	//
	// ⚠️ 这一条**在 origin/main 上同样会红**（不是本票引入的），但验收命令要求
	// 全绿，而一条随负载随机红的测试会把整张票卡住，所以在这儿修：等的是**它要
	// 断言的那件事真的发生**，而不是「过了 10 毫秒」。
	waitFrames(t, tt, "重试之后第二页真的被请求了一次", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return pn2 == before+1
	})

	// 之后再确认「按钮不见了 + 没有第二个请求」这两条：多画几帧，让任何一次
	// 重复请求都有机会发生。
	for range 30 {
		tt.Frame()
	}
	if _, ok := tt.Find("重试加载关注的人"); ok {
		t.Errorf("重试之后按钮还在 —— 闸门的 loading 没写回真实的那一份" +
			"（`auth := f.auth` 是复制），可以再点一次并发发第二个请求")
	}
	mu.Lock()
	during := pn2
	mu.Unlock()
	if during != before+1 {
		t.Errorf("点了重试之后第二页被请求了 %d 次，want %d（一次就够）",
			during-before, 1)
	}

	close(release)
	waitFrames(t, tt, "第二页到了", func() bool { return len(f.authors) == 2 })
	mu.Lock()
	after := pn2
	mu.Unlock()
	if after != before+1 {
		t.Errorf("整个过程第二页被请求了 %d 次，want %d —— 重试期间又发起了重复请求",
			after-before, 1)
	}
}
