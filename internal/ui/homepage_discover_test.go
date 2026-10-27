package ui

// 首页「热榜 / 原创榜 / 最近播放」三个 Tab。取数的参数和翻译由 app 层、服务层那两组
// 钉（`internal/app/recommend_discover_test.go`）；这里盯画面：点进去有榜上的歌、
// 没登录时最近播放不发请求而是「去登录」。全部走假 B 站，不碰真账号。

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

const (
	uiHomeToplistPeriods = "/x/copyright-music-publicity/toplist/all_period"
	uiHomeToplistMusic   = "/x/copyright-music-publicity/toplist/music_list"
	uiHomeHistory        = "/x/web-interface/history/search"
)

func homeDiscoverFake(t *testing.T) *uiExplore {
	t.Helper()
	b := newUIExplore(t)
	homeArchives(b)
	b.on(uiHomeToplistPeriods, func(w http.ResponseWriter, r *http.Request) {
		id := 8
		if r.URL.Query().Get("list_type") == "2" {
			id = 9
		}
		uiExploreJSON(w, 0, map[string]any{"list": map[string]any{
			"2024": []map[string]any{{"ID": id, "priod": 3, "publish_time": 200}},
		}})
	})
	b.on(uiHomeToplistMusic, func(w http.ResponseWriter, r *http.Request) {
		title := "热榜第一名"
		if r.URL.Query().Get("list_id") == "9" {
			title = "原创榜第一名"
		}
		uiExploreJSON(w, 0, map[string]any{"list": []map[string]any{{
			"music_id": "MA1", "music_title": title, "singer": "榜上歌手",
			"creation_bvid": "BV1" + r.URL.Query().Get("list_id"), "creation_aid": 1,
			"creation_cover": "https://f/board.jpg", "creation_duration": 120, "creation_play": 5,
		}}})
	})
	b.on(uiHomeHistory, func(w http.ResponseWriter, _ *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"has_more": false, "list": []map[string]any{}})
	})
	return b
}

func TestTheBoardTabsShowTheLatestPeriodOfTheirOwnBoard(t *testing.T) {
	b := homeDiscoverFake(t)
	_, tt, _ := newHomeShell(t, b)
	waitFrames(t, tt, "第一档", func() bool { return showsText(tt, "音乐 推的第一首") })

	for _, tc := range []struct {
		tab  int
		want string
	}{
		{3, "热榜第一名"}, {4, "原创榜第一名"},
	} {
		if err := tt.Click(homeTabs[tc.tab]); err != nil {
			t.Fatalf("点不到 %q：%v，文字有 %q", homeTabs[tc.tab], err, tt.Texts())
		}
		waitFramesClock(t, tt, homeTabs[tc.tab]+"拉回来了", func() bool { return showsText(tt, tc.want) })
	}
}

func TestRecentPlaysAsksToLogInAndSendsNothingWhenLoggedOut(t *testing.T) {
	b := homeDiscoverFake(t)
	_, tt, _ := newHomeShell(t, b)
	waitFrames(t, tt, "第一档", func() bool { return showsText(tt, "音乐 推的第一首") })

	if err := tt.Click(homeTabs[homeRecentTab]); err != nil {
		t.Fatalf("点不到最近播放：%v，文字有 %q", err, tt.Texts())
	}
	waitFramesClock(t, tt, "去登录那一支", func() bool { return showsText(tt, "去登录") })
	if b.hit(uiHomeHistory) {
		t.Error("没登录却去拉了历史记录")
	}
}

func TestRecentPlaysWithNoHistoryHasItsOwnEmptyState(t *testing.T) {
	b := homeDiscoverFake(t)
	store := app.NewMemoryStore("home-test")
	if err := store.Save(context.Background(), app.Session{SESSDATA: "sess", DedeUserID: "42"}); err != nil {
		t.Fatal(err)
	}
	sh := newTestShell(t, at(homeW, homeH), mounted(func(_ *testing.T, a *app.App) {
		transport := &app.CredentialTransport{Base: b.Server.Client().Transport, Policy: app.DefaultInjectPolicy()}
		cli := bili.NewClient(bili.Options{Transport: transport})
		cli.TestOnlyPointHostsAtFixture(b.URL)
		a.MountLogin(store, cli)
		a.MountRecommend(cli)
	}))
	tt := sh.tt
	waitFrames(t, tt, "第一档", func() bool { return showsText(tt, "音乐 推的第一首") })

	if err := tt.Click(homeTabs[homeRecentTab]); err != nil {
		t.Fatalf("点不到最近播放：%v，文字有 %q", err, tt.Texts())
	}
	waitFramesClock(t, tt, "最近播放的空状态", func() bool { return showsText(tt, recentEmpty.Title) })
	if !showsText(tt, "去听推荐") {
		t.Errorf("最近播放空了却没有「去听推荐」那一步：%q", tt.Texts())
	}
}

func TestTheBoardCaptionNamesThePeriodAndTheUpdateDate(t *testing.T) {
	at := time.Date(2024, 5, 12, 8, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		n    int
		at   time.Time
		want string
	}{
		{3, at, "第 3 期 · 2024-05-12 更新"}, {3, time.Time{}, "第 3 期"},
		{0, at, "2024-05-12 更新"}, {0, time.Time{}, ""},
	} {
		if got := boardCaptionText(tc.n, tc.at); got != tc.want {
			t.Errorf("boardCaptionText(%d, %v) = %q，want %q", tc.n, tc.at, got, tc.want)
		}
	}
}

func TestTheBoardTabShowsItsPeriodAboveTheList(t *testing.T) {
	b := homeDiscoverFake(t)
	_, tt, _ := newHomeShell(t, b)
	waitFrames(t, tt, "第一档", func() bool { return showsText(tt, "音乐 推的第一首") })
	if err := tt.Click(homeTabs[homeHotTab]); err != nil {
		t.Fatalf("点不到热榜：%v", err)
	}
	waitFramesClock(t, tt, "热榜的期数", func() bool { return showsText(tt, "第 3 期 · 1970-01-01 更新") })
}

// TestTheSixTabsAndPlayAllFitAtTheSmallestWindow 量最小窗口下 Tab 那一排：六个 Tab 一个
// 挨一个不重叠，而且「全部播放」没有被挤出右沿。
func TestTheSixTabsAndPlayAllFitAtTheSmallestWindow(t *testing.T) {
	b := homeDiscoverFake(t)
	sh := newTestShell(t, at(minWindowW, minWindowH), mounted(func(_ *testing.T, a *app.App) {
		cli := bili.NewClient(bili.Options{Transport: b.Server.Client().Transport})
		cli.TestOnlyPointHostsAtFixture(b.URL)
		a.MountLogin(app.NewMemoryStore("home-test"), cli)
		a.MountRecommend(cli)
	}))
	tt := sh.tt
	homeFrame(tt)

	prevRight := float32(-1)
	for _, name := range homeTabs {
		r, ok := tt.Find(name)
		if !ok {
			t.Fatalf("最小窗口下找不到 Tab %q，文字有 %q", name, tt.Texts())
		}
		if r.X < prevRight {
			t.Errorf("Tab %q 的左沿 %.0f 压在前一个的右沿 %.0f 上", name, r.X, prevRight)
		}
		prevRight = r.X + r.W
	}
	pa, ok := tt.Find("全部播放")
	if !ok || pa.X < prevRight || pa.X+pa.W > float32(minWindowW) {
		t.Errorf("「全部播放」= %+v，要在最后一个 Tab（右沿 %.0f）右边、窗口（%d）以内", pa, prevRight, minWindowW)
	}
}

func TestTheEmptySearchBoxOffersWhatEveryoneIsListeningTo(t *testing.T) {
	b := homeDiscoverFake(t)
	_, tt, _ := newHomeShell(t, b)
	waitFrames(t, tt, "第一档", func() bool { return showsText(tt, "音乐 推的第一首") })
	if err := tt.Click(searchLabel); err != nil {
		t.Fatalf("点不到搜索框：%v", err)
	}
	waitFramesClock(t, tt, "大家在听", func() bool { return showsText(tt, suggestHotHeading) && showsText(tt, "热榜第一名") })
}
