package mobileui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
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
	mobileLibraryShot(t, tester, "discover-fixture.png")
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

func mobileBrowseFixture(t *testing.T) (*shell, func(*ui.Tester, string)) {
	t.Helper()
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
