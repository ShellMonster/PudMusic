package ui

// 首页「发现」总览那三块（横幅 / 新歌速递 / 排行榜）在**三种取数状态**下各自
// 画什么。**这一组盯的是「不许整块消失」**。
//
// # 现象（用户报的）
//
// 首页加载异常时**整片空白**：侧栏、顶栏、四个磁贴都在，而内容区只剩顶上那
// 一行磁贴——「新歌速递」和「排行榜」两块**整块不见了**（连标题都没有）。
//
// # 成因
//
// 三块都有一句「早退」，而那句早退把**「拉失败了」和「没在拉」当成同一件事**
// （`homepage_overview.go`）：
//
//	viewHero      `!loading && started` ⇒ 失败 ⇒ 早退（整块没了）
//	viewNewRow    `!loading && (started || err != nil)` ⇒ 失败 ⇒ 早退
//	viewMiniBoards `!boardLoading()` ⇒ 失败 ⇒ 早退
//
// 而 `boardLoading()` 只在 `pg.Loading` 时为真——**失败那一刻它是 false**
// （`PagedList.apply` 失败分支把 `loading` 置 false、`err` 置上、`blocked` 置
// true）。于是失败时既不摆骨架、也不摆错误提示、也不摆内容 ⇒ 一片空白。
//
// # 这一组钉的三件事
//
//  1. **拉失败时那一块不是空白**：有话可说（错误）+ 能重来（重试）。
//  2. **拉到空是另一件事**：那是「真的没有」，**不是**失败，**不许**摆重试。
//  3. **还在拉时摆骨架**（扫光那一档），不是转圈——总览那几块的形状是已知的
//     （卡片行、榜单三列），见 `widgets/skeleton` 的文件头。
//
// ⚠️ **判据用文字，不用像素**：那一道边线是半透明混色画上去的，精确比色扫不到
// （另一票踩过）。文字和元素框是稳的。

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	mygoui "github.com/egoist/mygo/ui"
)

// overviewFailingShell 造一台**总览那三摊都拉失败**的假 B 站。
//
// ⚠️ **三摊都要失败**：新歌速递那一摊是 `NewMusicBanner` + `NewMusic` 两个请求
// （`app/recommend.go` 的 `NewMusic`），而它两个都失败才报得出错（只有一个失败
// 时它拿另一个的结果当成功）。榜那两档走的是 `ToplistLatest` + `ToplistMusic`
// 两趟（`app/recommend.go` 的 `toplist`）——`ToplistLatest` 失败时整档失败。
//
// ⚠️ **用 -403**：风控在真实环境里最常见（见 `recommend.go` 里「哪几个要签名」
// 那段），而它有一句现成的人话「没有权限看这一项」。
func overviewFailingShell(t *testing.T) (*Shell, *mygoui.Tester) {
	t.Helper()
	b := homeDiscoverFake(t)
	// 新歌速递那一摊（头图位 + 列表，两个都要失败）。
	b.on(uiHomeNewMusic, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, -403, nil)
	})
	b.on(uiHomeNewBanner, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, -403, nil)
	})
	// 两张榜：期数那一趟失败 ⇒ 整档失败。
	b.on(uiHomeToplistPeriods, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, -403, nil)
	})
	// 分区推荐（首页那一档）也失败，让整屏都在失败态里——这样断言「不是空白」
	// 才测的是「最坏的那一档」。
	b.on(uiHomeRegionFeed, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, -403, nil)
	})
	s, tt, _ := newHomeShell(t, b)
	return s, tt
}

// TestTheOverviewNeverGoesBlankWhenTheFetchFails 钉「不许整块消失」这一条。
//
// 判据是**三样都还在画面上**：横幅那一块、新歌速递、排行榜。
//
// ⚠️ **不是「有一行字」就过**：整页只剩一句「这一块没拉出来」也算一片空白
// （用户看到的是「首页空了」）。所以这里要求的是**三块的标题都在**——
// 横幅没有标题，它那一块的存在靠「横幅那一块自己说的话」。
func TestTheOverviewNeverGoesBlankWhenTheFetchFails(t *testing.T) {
	startOnOverview(t)
	_, tt := overviewFailingShell(t)

	// 等失败落地（错误那句是「这一块没拉出来」那一族）。
	waitFrames(t, tt, "拉失败了", func() bool {
		return showsText(tt, overviewFailText)
	})
	homeFrame(tt)

	// ① 排行榜那一块：标题必须在（它是那一块「在不在」的标志）。
	if !showsText(tt, "排行榜") {
		t.Errorf("失败之后「排行榜」那一块整块不见了，文字有 %q", tt.Texts())
	}
	// ② 新歌速递那一块：标题必须在。
	if !showsText(tt, "新歌速递") {
		t.Errorf("失败之后「新歌速递」那一块整块不见了，文字有 %q", tt.Texts())
	}
	// ③ 横幅那一块：它没有标题，靠自己那块内容说话。失败时它要说得出为什么。
	if !showsText(tt, overviewFailText) {
		t.Errorf("失败之后横幅那一块没有说任何话，文字有 %q", tt.Texts())
	}
}

// TestTheOverviewSaysItFailedAndOffersARetry 钉「失败那一档要有重试」。
//
// ⚠️ **重试要能点得到**：0×0 的按钮 `Find` 找得到文字，点下去什么都不会发生
// （见 `homepage_feed_test.go` 那一条的注释）。
//
// ⚠️ **榜那两枚在窗子外面**：「排行榜」那一块在总览的最下面，而 1200×800 的窗
// 装不下它——那一列的重试按钮**被裁到 0×0**（量出来是这样）。所以这里先滚到
// 那一块再量。⚠️ **滚是必要的，不是保险**：不滚的话量到的是 0×0，而那不是
// 「按钮坏了」，是「它在窗外」。
func TestTheOverviewSaysItFailedAndOffersARetry(t *testing.T) {
	startOnOverview(t)
	_, tt := overviewFailingShell(t)

	waitFrames(t, tt, "拉失败了", func() bool {
		return showsText(tt, overviewFailText)
	})
	homeFrame(tt)

	// 新歌速递那一块的重试（那一摊自己管自己，见 `newMusicGrid` 的三态）。
	if _, ok := tt.Find("重试加载新歌速递"); !ok {
		t.Errorf("新歌速递那一块没有重试按钮，文字有 %q", tt.Texts())
	}
	// 榜那两档的重试。**两档各自一枚**（它们是两次请求，说清按的是哪一档）。
	tt.Scroll(homeW/2, homeH/2, 0, scrollFar)
	homeFrame(tt)
	for _, tab := range []int{homeHotTab, homeOriginalTab} {
		label := "重试加载" + homeTabs[tab]
		r, ok := tt.Find(label)
		if !ok {
			t.Errorf("没有 %q 那一枚重试，文字有 %q", label, tt.Texts())
			continue
		}
		if r.W <= 0 || r.H <= 0 {
			t.Errorf("%q 是 %.0fx%.0f，点不到", label, r.W, r.H)
		}
	}
}

// TestTheOverviewRetryStartsTheRequestAgain 钉「点重试能重新发起请求」。
//
// ⚠️ **断的是「请求数变多」而不是「画面变了」**：画面上的字要等那一趟回来才
// 换，而那一趟回来之后画出来的是歌——那是「成功」那一档。这里只断「又发了一趟」。
//
// ⚠️ **要先滚到那一枚**：它在「排行榜」那一块里，而那一块在窗子下面（不滚的
// 话 `Click` 会说「outside the window」——见 mygo 的 `targetCenter`）。
func TestTheOverviewRetryStartsTheRequestAgain(t *testing.T) {
	startOnOverview(t)
	b := homeDiscoverFake(t)
	b.on(uiHomeToplistPeriods, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, -403, nil)
	})
	_, tt, _ := newHomeShell(t, b)

	waitFrames(t, tt, "榜拉失败了", func() bool {
		return showsText(tt, overviewFailText)
	})
	homeFrame(tt)

	before := b.countHit(uiHomeToplistPeriods)
	if before == 0 {
		t.Fatal("榜那一趟压根没发出去过")
	}
	tt.Scroll(homeW/2, homeH/2, 0, scrollFar)
	homeFrame(tt)
	if err := tt.Click("重试加载" + homeTabs[homeHotTab]); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "重试之后又发了一趟", func() bool {
		return b.countHit(uiHomeToplistPeriods) > before
	})
}

// TestAnEmptyOverviewIsNotAFailure 钉「真的没有」和「拉不到」是两件事。
//
// B 站返回空列表**不是失败**：界面上该说「还没有」，而**不许**摆一个重试
// （按多少次都是空的）。这一条和 `viewTabBody` 那四种状态是一个口径。
func TestAnEmptyOverviewIsNotAFailure(t *testing.T) {
	startOnOverview(t)
	b := homeDiscoverFake(t)
	// 新歌速递：空的。
	b.on(uiHomeNewMusic, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"list": []map[string]any{}})
	})
	b.on(uiHomeNewBanner, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"list": []map[string]any{}})
	})
	// 榜：空的（期数有了，条目是空的）。
	b.on(uiHomeToplistMusic, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"list": []map[string]any{}})
	})
	_, tt, _ := newHomeShell(t, b)

	waitFrames(t, tt, "空的那几摊都回来了", func() bool {
		return showsText(tt, "新歌速递") && showsText(tt, "排行榜")
	})
	homeFrame(tt)

	// 空的那几摊**不许**被说成「拉不到」。
	if showsText(tt, overviewFailText) {
		t.Errorf("空的推荐被说成了「拉不到」，文字有 %q", tt.Texts())
	}
	// ⚠️ **不许摆重试**：按多少次都是空的。
	if _, ok := tt.Find("重试加载新歌速递"); ok {
		t.Error("空的新歌速递上摆了一个重试按钮")
	}
	for _, tab := range []int{homeHotTab, homeOriginalTab} {
		if _, ok := tt.Find("重试加载" + homeTabs[tab]); ok {
			t.Errorf("空的%s上摆了一个重试按钮", homeTabs[tab])
		}
	}
}

// blockedHomeShell 造一台**推荐那几摊永远不回**的假 B 站（外加 nav 回，
// 因为签名要它）。
//
// ⚠️ **和 `blockedBiliOpts` 不是同一台**：那一台挂的是 `MountLibrary` /
// `MountExplore` / `MountCollections`，**没有挂 `MountRecommend`**——于是首页那
// 几摊压根不发请求（`RecommendOf().Mounted()` 是假），总览上摆的是「还没有
// 内容」那一档，**不是**「还在拉」。这一台补上 `MountRecommend`。
func blockedHomeShell(t *testing.T) *testShell {
	t.Helper()
	hold := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/x/web-interface/nav" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"message":"0","ttl":1,"data":{"isLogin":true,
			  "wbi_img":{"img_url":"https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png",
			             "sub_url":"https://i0.hdslb.com/bfs/wbi/4932caff0ff746aeb6f01bf08b70ac45.png"}}}`))
			return
		}
		<-hold
	}))
	t.Cleanup(func() { close(hold); srv.Close() })

	transport := &app.CredentialTransport{
		Base:   srv.Client().Transport,
		Policy: app.DefaultInjectPolicy().AllowBiliHost(hostOfFixture(srv.URL)),
	}
	cli := bili.NewClient(bili.Options{Transport: transport})
	cli.TestOnlyPointHostsAtFixture(srv.URL)
	return newTestShell(t, at(homeW, homeH), mounted(func(_ *testing.T, a *app.App) {
		a.MountLogin(app.NewMemoryStore("home-test"), cli)
		a.MountRecommend(cli)
	}))
}

// TestTheOverviewShowsSkeletonsWhileItIsStillLoading 钉「还在拉」那一档摆骨架。
//
// ⚠️ **判据是那块骨架自己的无障碍名字**（`loadingBoardText` /
// `loadingNewMusicText`）：骨架是一块一块灰，没有字可读——而它的名字就是
// 「正在加载排行榜」那一句（见 `skeleton` 那一族的 `Label`）。
//
// ⚠️ **不用固定帧数**：这一条等的是「条件成立」，不是「过了几帧」。
func TestTheOverviewShowsSkeletonsWhileItIsStillLoading(t *testing.T) {
	startOnOverview(t)
	b := blockedHomeShell(t)
	tt := b.tt

	waitFrames(t, tt, "总览那几块在拉", func() bool {
		return showsText(tt, loadingBoardText) || showsText(tt, loadingNewMusicText)
	})
	homeFrame(tt)

	if !showsText(tt, loadingNewMusicText) {
		t.Errorf("总览上没有「%s」那一块骨架，文字有 %q", loadingNewMusicText, tt.Texts())
	}
	if !showsText(tt, loadingBoardText) {
		t.Errorf("总览上没有「%s」那一块骨架，文字有 %q", loadingBoardText, tt.Texts())
	}
}
