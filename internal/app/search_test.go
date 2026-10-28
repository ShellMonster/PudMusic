package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

const (
	searchFixtureImgKey = "{{SECRET_vh6x7xww}}"
	searchFixtureSubKey = "{{SECRET_x7m5p2qk}}"
)

func TestSearchReturnsApplicationPagesForEveryTab(t *testing.T) {
	var mu sync.Mutex
	var got []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/web-interface/nav":
			writeSearchJSON(w, map[string]any{
				"code": 0,
				"data": map[string]any{"wbi_img": map[string]string{
					"img_url": "https://i0.hdslb.com/bfs/wbi/" + searchFixtureImgKey + ".png",
					"sub_url": "https://i0.hdslb.com/bfs/wbi/" + searchFixtureSubKey + ".png",
				}},
			})
		case "/x/web-interface/wbi/search/type":
			mu.Lock()
			got = append(got, r.URL.Query())
			mu.Unlock()
			searchType := r.URL.Query().Get("search_type")
			result := map[string]any{"bvid": "BV-app-row", "title": "应用层歌曲", "author": "某 UP", "mid": 42, "pic": "//i0.hdslb.com/a.jpg", "duration": "4:20", "play": 17, "pubdate": 1700000000}
			if searchType == "bili_user" {
				result = map[string]any{"mid": 9527, "uname": "应用层用户", "usign": "签名", "upic": "//i0.hdslb.com/u.jpg", "fans": 99, "level": 6}
			}
			writeSearchJSON(w, map[string]any{"code": 0, "data": map[string]any{
				"numResults": 73, "numPages": 3, "result": []any{result},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(server.URL)
	a := app.New()
	defer a.Close()
	a.MountSearch(cli)
	search := a.SearchOf()

	cases := []struct {
		name string
		tab  app.SearchTab
		want string
	}{
		{name: "song", tab: app.SearchTabSong, want: "歌曲"},
		{name: "video", tab: app.SearchTabVideo, want: "视频"},
		{name: "user", tab: app.SearchTabUser, want: "用户"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pg, err := search.Page(context.Background(), app.SearchQuery{
				Tab: tc.tab, Keyword: "周杰伦", Sort: app.SearchSortThird, Page: 2,
			})
			if err != nil {
				t.Fatal(err)
			}
			if pg.Total != 73 || pg.HasMore != true || len(pg.Items) != 1 {
				t.Fatalf("page=%+v", pg)
			}
			row := pg.Items[0]
			if row.ID == "" || row.Item.Title == "" || row.Item.Source != app.SourceOnline {
				t.Fatalf("application row lost stable identity or action item: %+v", row)
			}
			if tc.tab == app.SearchTabUser {
				if row.ID != "u:9527" || row.Item.Type != app.ItemTypeUser || row.Item.Bvid != "9527" {
					t.Fatalf("user row=%+v", row)
				}
			} else if row.ID != "BV-app-row" {
				t.Fatalf("video row id=%q", row.ID)
			}
		})
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) != len(cases) {
		t.Fatalf("got %d search requests, want %d", len(got), len(cases))
	}
	for i, q := range got {
		if q.Get("keyword") != "周杰伦" || q.Get("page") != "2" {
			t.Errorf("request %d query=%v", i, q)
		}
		if q.Get("search_type") == "bili_user" {
			if q.Get("order") != "fans" || q.Get("order_sort") != "1" {
				t.Errorf("user request=%v", q)
			}
		} else if q.Get("order") != "pubdate" {
			t.Errorf("video request=%v", q)
		}
	}
}

func TestSearchClassifiesErrorsForTheUI(t *testing.T) {
	t.Run("network is retryable", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/x/web-interface/nav" {
				writeSearchJSON(w, map[string]any{"code": 0, "data": map[string]any{"wbi_img": map[string]string{
					"img_url": "https://i0.hdslb.com/bfs/wbi/" + searchFixtureImgKey + ".png",
					"sub_url": "https://i0.hdslb.com/bfs/wbi/" + searchFixtureSubKey + ".png",
				}}})
				return
			}
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()
		cli := bili.NewClient(bili.Options{})
		cli.TestOnlyPointHostsAtFixture(server.URL)
		a := app.New()
		defer a.Close()
		a.MountSearch(cli)

		_, err := a.SearchOf().Page(context.Background(), app.SearchQuery{Tab: app.SearchTabSong, Keyword: "x", Page: 1})
		var searchErr *app.SearchError
		if !errors.As(err, &searchErr) {
			t.Fatalf("error=%T %v, want SearchError", err, err)
		}
		if searchErr.Message != "连不上 B 站，检查一下网络" || !searchErr.Retryable {
			t.Fatalf("search error=%+v", searchErr)
		}
		if searchErr.Category != app.SearchErrorNetwork {
			t.Fatalf("search error category=%v, want network", searchErr.Category)
		}
		if !errors.Is(err, bili.ErrNetwork) {
			t.Fatalf("search error lost network cause: %v", err)
		}

	})

	t.Run("business error is not retryable", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/x/web-interface/nav" {
				writeSearchJSON(w, map[string]any{"code": 0, "data": map[string]any{"wbi_img": map[string]string{
					"img_url": "https://i0.hdslb.com/bfs/wbi/" + searchFixtureImgKey + ".png",
					"sub_url": "https://i0.hdslb.com/bfs/wbi/" + searchFixtureSubKey + ".png",
				}}})
				return
			}
			writeSearchJSON(w, map[string]any{"code": -400, "message": "请求错误"})
		}))
		defer server.Close()
		cli := bili.NewClient(bili.Options{})
		cli.TestOnlyPointHostsAtFixture(server.URL)
		a := app.New()
		defer a.Close()
		a.MountSearch(cli)

		_, err := a.SearchOf().Page(context.Background(), app.SearchQuery{Tab: app.SearchTabVideo, Keyword: "x", Page: 1})
		var searchErr *app.SearchError
		if !errors.As(err, &searchErr) {
			t.Fatalf("error=%T %v, want SearchError", err, err)
		}
		if searchErr.Retryable {
			t.Fatalf("business error unexpectedly retryable: %+v", searchErr)
		}
		if searchErr.Category != app.SearchErrorAPI {
			t.Fatalf("search error category=%v, want api", searchErr.Category)
		}
		if searchErr.Message != "B 站拒绝了这次请求" {
			t.Fatalf("search error message=%q", searchErr.Message)
		}

	})
}

func writeSearchJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
