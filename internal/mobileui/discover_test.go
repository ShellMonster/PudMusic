package mobileui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/egoist/mygo/ui"
)

// TestMobileDiscoverOpensLiveCollectionAndDailyRoutes checks that the on-screen cards and shortcuts navigate with the returned collection identity.
func TestMobileDiscoverOpensLiveCollectionAndDailyRoutes(t *testing.T) {
	s, wait := mobileBrowseFixture(t)
	tester := ui.NewTester(func(c *ui.Context) {
		ui.Scroll(c).Fill().Children(func() {
			ui.Column(c).FillWidth().Padding(8, 16).Children(func() {
				page := s.current()
				if page.kind == pageCollection {
					ui.Text(c, page.title)
					return
				}
				s.discover(c, page)
			})
		})
	}, 390, 740)
	wait(tester, "公开音乐合集")
	mobileLibraryShot(t, tester, "guest-discover.png")
	for _, label := range []string{"每日推荐", "排行榜", "新歌速递", "歌单", "播放今日一张"} {
		if _, ok := tester.Find(label); !ok {
			t.Fatalf("missing Discover action %q", label)
		}
	}
	if err := tester.Click("公开音乐合集"); err != nil {
		t.Fatal(err)
	}
	ref := s.current().ref
	if s.current().kind != pageCollection || ref.ID != 101 || ref.OwnerMid != 42 || ref.Type != bili.CollectionAlbum {
		t.Fatalf("collection route=%+v", s.current())
	}
	s.back()
	tester.Frame()
	if err := tester.Click("打开今日一张 公开音乐合集"); err != nil {
		t.Fatal(err)
	}
	wait(tester, "合集曲目")
	s.back()
	tester.Frame()
	if err := tester.Click("每日推荐"); err != nil {
		t.Fatal(err)
	}
	wait(tester, "推荐曲目")
	if s.current().kind != pageDaily {
		t.Fatalf("daily shortcut opened %v", s.current().kind)
	}
}

// TestMobileDiscoverRetryKeepsAvailableContent covers a shared outage, a partial outage, and a failed refresh of cached content.
func TestMobileDiscoverRetryKeepsAvailableContent(t *testing.T) {
	var failFeed, failMixes atomic.Bool
	var feedRequests atomic.Int32
	failFeed.Store(true)
	s, wait := mobileBrowseFixtureWith(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/x/web-interface/region/feed/rcmd" {
			feedRequests.Add(1)
		}
		if (r.URL.Path == "/x/web-interface/region/feed/rcmd" && failFeed.Load()) ||
			(r.URL.Path == "/x/polymer/web-space/seasons_series_list" && failMixes.Load()) {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return true
		}
		return false
	})
	tester := ui.NewTester(s.view, 390, 844)
	wait(tester, "暂时无法加载推荐")
	mobileLibraryShot(t, tester, "discover-recovery-light.png")
	tester.SetDark(true)
	mobileLibraryShot(t, tester, "discover-recovery-dark.png")
	tester.SetSize(320, 740)
	mobileLibraryShot(t, tester, "discover-recovery-dark-narrow.png")
	tester.SetSize(390, 844)
	tester.SetDark(false)
	for _, label := range []string{"搜索歌曲、歌手、歌单", "本地音乐", "下载记录", "重新加载"} {
		if _, found := tester.Find(label); !found {
			t.Fatalf("discover outage hid %q", label)
		}
	}
	for _, label := range []string{"本地音乐", "下载记录"} {
		if err := tester.Click(label); err != nil {
			t.Fatal(err)
		}
		if s.current().title != label {
			t.Fatalf("%q opened %q", label, s.current().title)
		}
		if err := tester.Click("返回"); err != nil {
			t.Fatal(err)
		}
	}
	if slices.Contains(tester.Texts(), "暂时没加载出来") {
		t.Fatal("shared outage also rendered a section-level failure")
	}
	failFeed.Store(false)
	failMixes.Store(true)
	if err := tester.Click("重新加载"); err != nil {
		t.Fatal(err)
	}
	wait(tester, "推荐歌单暂时不可用")
	mobileLibraryShot(t, tester, "discover-recovery-partial.png")
	if !tester.HasText("推荐曲目") || tester.HasText("暂时无法加载推荐") {
		t.Fatal("a collection outage hid the available recommendations")
	}
	failMixes.Store(false)
	before := feedRequests.Load()
	if err := tester.Click("重试歌单"); err != nil {
		t.Fatal(err)
	}
	wait(tester, "公开音乐合集")
	if feedRequests.Load() != before {
		t.Fatal("retrying only playlists also refetched the available tracks")
	}
	localAudio := filepath.Join("..", "service", "audio", "testdata", "gapless.mp3")
	s.app.TestOnlyNoAutoLyrics()
	if err := s.app.Open(localAudio); err != nil {
		t.Fatal(err)
	}
	playing := s.app.Snapshot()
	failFeed.Store(true)
	s.loadDiscoverHome(s.discoverHomeState(s.current()))
	wait(tester, "重试刷新")
	mobileLibraryShot(t, tester, "discover-recovery-refresh.png")
	if !tester.HasText("公开音乐合集") || !tester.HasText("推荐曲目") || tester.HasText("暂时无法加载推荐") {
		t.Fatal("a refresh error replaced cached recommendations")
	}
	if current := s.app.Snapshot(); current.Path != playing.Path || !current.Playing || current.Position < playing.Position {
		t.Fatal("refresh failure interrupted the current playback")
	}
}

// TestMobileDiscoverOffersLoginWhenRequired verifies the action when Bilibili explicitly requires an account.
func TestMobileDiscoverOffersLoginWhenRequired(t *testing.T) {
	s, wait := mobileBrowseFixtureWith(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/x/web-interface/region/feed/rcmd" {
			return false
		}
		fmt.Fprint(w, `{"code":-101,"message":"账号未登录"}`)
		return true
	})
	tester := ui.NewTester(s.view, 390, 844)
	wait(tester, "登录后可查看推荐")
	if err := tester.Click("去登录"); err != nil {
		t.Fatal(err)
	}
	if s.current().kind != pageLogin {
		t.Fatal("the account requirement did not lead to login")
	}
}

// TestMobileGuestFeedLoginReturnsToFeed verifies login recovery and that failed pagination keeps the previous posts.
func TestMobileGuestFeedLoginReturnsToFeed(t *testing.T) {
	var failInitial, failNext atomic.Bool
	failInitial.Store(true)
	failNext.Store(true)
	s, wait := mobileBrowseFixtureWith(t, func(w http.ResponseWriter, r *http.Request) bool {
		switch r.URL.Path {
		case "/x/passport-login/web/qrcode/generate":
			fmt.Fprint(w, `{"code":0,"data":{"url":"https://account.bilibili.com/h5/account-h5/auth/scan-web?qrcode_key=demo","qrcode_key":"demo"}}`)
		case "/x/passport-login/web/qrcode/poll":
			http.SetCookie(w, &http.Cookie{Name: "SESSDATA", Value: "synthetic-feed-session", Path: "/"})
			http.SetCookie(w, &http.Cookie{Name: "DedeUserID", Value: "42", Path: "/"})
			fmt.Fprint(w, `{"code":0,"data":{"code":0}}`)
		case "/x/relation/followings":
			fmt.Fprint(w, `{"code":0,"data":{"list":[],"total":0}}`)
		case "/x/v3/fav/folder/created/list-all", "/x/v3/fav/folder/created/list", "/x/v3/fav/folder/collected/list":
			fmt.Fprint(w, `{"code":0,"data":{"list":[],"count":0,"has_more":false}}`)
		case "/x/polymer/web-dynamic/v1/feed/all":
			next := r.URL.Query().Get("offset") != ""
			if (!next && failInitial.Load()) || (next && failNext.Load()) {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				break
			}
			id, text, offset := "first", "第一条动态", "next"
			if next {
				id, text, offset = "second", "第二条动态", ""
			}
			fmt.Fprintf(w, `{"code":0,"data":{"items":[{"id_str":%q,"type":"DYNAMIC_TYPE_WORD","modules":{"module_author":{"mid":42,"name":"音乐 UP 主"},"module_dynamic":{"desc":{"text":%q}}}}],"has_more":%t,"offset":%q}}`, id, text, !next, offset)
		default:
			return false
		}
		return true
	})
	s.app.SetLoginPollInterval(2 * time.Millisecond)
	s.switchTab(2)
	tester := ui.NewTester(s.view, 390, 844)
	mobileLibraryShot(t, tester, "guest-my.png")
	for _, label := range []string{"访客", "最近播放", "本地音乐", "下载", "逛逛公开歌单", "我的关注", "关注动态", "稍后再看", "听歌统计"} {
		if _, ok := tester.Find(label); !ok {
			t.Fatalf("guest My page is missing %q", label)
		}
	}
	for _, action := range []string{"关注动态", "去登录", "继续登录"} {
		if err := tester.Click(action); err != nil {
			t.Fatal(err)
		}
	}
	wait(tester, "登录成功")
	if err := tester.Click("返回"); err != nil {
		t.Fatal(err)
	}
	wait(tester, "暂时无法加载动态")
	mobileLibraryShot(t, tester, "feed-recovery-initial.png")
	failInitial.Store(false)
	if err := tester.Click("重试"); err != nil {
		t.Fatal(err)
	}
	wait(tester, "第一条动态")
	if err := tester.Click("加载更多"); err != nil {
		t.Fatal(err)
	}
	wait(tester, "暂时无法加载更多动态")
	if !tester.HasText("第一条动态") {
		t.Fatal("loading the next page hid the first post")
	}
	mobileLibraryShot(t, tester, "feed-recovery-pagination.png")
	failNext.Store(false)
	if err := tester.Click("重试"); err != nil {
		t.Fatal(err)
	}
	wait(tester, "第二条动态")
}

func mobileBrowseFixture(t *testing.T) (*shell, func(*ui.Tester, string)) {
	t.Helper()
	return mobileBrowseFixtureWith(t, nil)
}

func mobileBrowseFixtureWith(t *testing.T, intercept func(http.ResponseWriter, *http.Request) bool) (*shell, func(*ui.Tester, string)) {
	t.Helper()
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if intercept != nil && intercept(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		q := r.URL.Query()
		var data string
		switch r.URL.Path {
		case "/x/web-interface/nav":
			data = `{"wbi_img":{"img_url":"https://test.invalid/0123456789abcdef0123456789abcdef.png","sub_url":"https://test.invalid/abcdef0123456789abcdef0123456789.png"}}`
		case "/x/web-interface/region/feed/rcmd":
			data = `{"archives":[{"bvid":"BVtestDaily","cid":9,"title":"推荐曲目","author":{"mid":42,"name":"音乐 UP 主"}}]}`
		case "/x/polymer/web-space/seasons_series_list":
			data = `{"items_lists":{"page":{"total":0}}}`
			if q.Get("mid") == "42" {
				data = `{"items_lists":{"seasons_list":[{"meta":{"season_id":101,"name":"公开音乐合集","total":1}}],"page":{"total":1}}}`
			}
		case "/x/polymer/web-space/seasons_archives_list":
			if q.Get("mid") != "42" || q.Get("season_id") != "101" {
				t.Errorf("collection archive request: %s", q.Encode())
			}
			data = `{"archives":[{"aid":2,"bvid":"BVtestAlbum","title":"合集曲目","duration":60}],"page":{"page_num":1,"page_size":20,"total":1}}`
		case "/x/copyright-music-publicity/toplist/all_period":
			data = `{"list":{"2026":[{"ID":9,"priod":1,"publish_time":1760000000}]}}`
		case "/x/copyright-music-publicity/toplist/music_list":
			data = `{"list":[{"music_id":"100","music_title":"音乐","singer":"测试歌手","mv_bvid":"BVtestHot","creation_first_cid":8}]}`
		case "/main/suggest":
			data = `{"tag":[{"term":"音乐联想"}]}`
		case "/x/web-interface/wbi/search/type":
			switch {
			case q.Get("search_type") == "bili_user":
				data = `{"numResults":1,"result":[{"mid":42,"uname":"搜索到的 UP 主","fans":50}]}`
			case q.Get("tids") == "":
				data = `{"numResults":1,"result":[{"bvid":"BVtestVideo","title":"视频结果","author":"音乐 UP 主","mid":42}]}`
			case q.Get("order") == "pubdate":
				data = `{"numResults":1,"result":[{"bvid":"BVtestLatest","title":"最新歌曲结果","author":"音乐 UP 主","mid":42}]}`
			default:
				data = `{"numResults":1,"result":[{"bvid":"BVtestSong","title":"歌曲结果","author":"音乐 UP 主","mid":42}]}`
			}
		default:
			t.Errorf("unexpected fixture path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `{"code":0,"data":%s}`, data)
	}))
	t.Cleanup(fixture.Close)
	a := app.New()
	t.Cleanup(a.Close)
	client := bili.NewClient(bili.Options{Transport: fixture.Client().Transport})
	client.TestOnlyPointHostsAtFixture(fixture.URL)
	a.MountLogin(app.NewMemoryStore("mobile fixture"), client)
	a.MountSearch(client)
	a.MountRecommend(client)
	a.MountExplore(client)
	a.MountCollections(app.CollectionRefs{Client: client})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := newShell(ctx, a)
	updates := make(chan func(), 32)
	s.dispatch = func(update func()) {
		select {
		case updates <- update:
		case <-ctx.Done():
		}
	}
	t.Cleanup(a.AddWake(func() { s.dispatch(func() {}) }))
	wait := func(tester *ui.Tester, text string) {
		t.Helper()
		deadline := time.NewTimer(3 * time.Second)
		defer deadline.Stop()
		for {
			tester.Frame()
			if slices.Contains(tester.Texts(), text) {
				return
			}
			select {
			case update := <-updates:
				update()
			case <-deadline.C:
				t.Fatalf("missing %q in %q", text, tester.Texts())
			}
		}
	}
	return s, wait
}
