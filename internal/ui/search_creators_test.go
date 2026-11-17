package ui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync/atomic"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

func creatorItem(id, name, cover string, v bili.Verification) app.SearchItem {
	return app.SearchItem{ID: "u:" + id, Title: name, Cover: cover, Verification: v,
		Item: app.Item{Type: app.ItemTypeUser, Bvid: id, Title: name, Cover: cover}}
}

// TestSearchCreatorsAreClickable locks the visible creator preview and its in-app navigation.
func TestSearchCreatorsAreClickable(t *testing.T) {
	s, tt := newSearchPage(t, "音乐", newSearchStub(24, 24))
	s.search.creators.fetch = func(context.Context, string) ([]app.SearchItem, error) {
		return []app.SearchItem{creatorItem("42", "测试UP", "", bili.VerificationPersonal), creatorItem("43", "另一个UP", "", bili.VerificationOrganization)}, nil
	}
	waitFrames(t, tt, "相关UP主", func() bool { return tt.HasText("测试UP") && tt.HasText("另一个UP") })
	if err := tt.Click("打开 测试UP 的空间"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := s.router.Location(); got != "/user/42" {
		t.Fatalf("打开空间 = %q", got)
	}
}

// TestSearchCreatorPreviewIgnoresStaleResults locks the current query against a delayed previous result.
func TestSearchCreatorPreviewIgnoresStaleResults(t *testing.T) {
	s, tt := newSearchPage(t, "旧词", newSearchStub(24, 24))
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	s.search.creators.fetch = func(_ context.Context, q string) ([]app.SearchItem, error) {
		if q == "旧词" {
			close(entered)
			<-release
			close(done)
			return []app.SearchItem{creatorItem("1", "旧UP", "", 0)}, nil
		}
		return []app.SearchItem{creatorItem("2", "新UP", "", 0)}, nil
	}
	tt.Frame()
	<-entered
	s.navigate(searchPath + "?" + searchQuery + "=" + url.QueryEscape("新词"))
	waitFrames(t, tt, "新UP", func() bool { return tt.HasText("新UP") })
	close(release)
	<-done
	tt.Frame()
	if tt.HasText("旧UP") {
		t.Fatal("旧搜索的UP主覆盖了当前搜索")
	}
}

// TestLeavingSearchCancelsCreatorRequest prevents a hidden page from keeping its lookup alive.
func TestLeavingSearchCancelsCreatorRequest(t *testing.T) {
	s, tt := newSearchPage(t, "音乐", newSearchStub(24, 24))
	started, canceled := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s.search.creators.fetch = func(ctx context.Context, _ string) ([]app.SearchItem, error) {
		if calls.Add(1) > 1 {
			return []app.SearchItem{creatorItem("42", "重新进入的UP", "", 0)}, nil
		}
		close(started)
		<-ctx.Done()
		close(canceled)
		return nil, ctx.Err()
	}
	tt.Frame()
	<-started
	s.navigate(localMusicPath)
	tt.Frame()
	waitFor(t, "离开搜索页后取消查询", func() bool {
		select {
		case <-canceled:
			return true
		default:
			return false
		}
	})
	s.submitSearch("音乐")
	waitFrames(t, tt, "重新进入的UP", func() bool { return tt.HasText("重新进入的UP") })
}

// TestSearchCreatorFailureDoesNotHideSongs locks the independent failure behavior.
func TestSearchCreatorFailureDoesNotHideSongs(t *testing.T) {
	s, tt := newSearchPage(t, "音乐", newSearchStub(24, 24))
	s.search.creators.fetch = func(context.Context, string) ([]app.SearchItem, error) { return nil, errors.New("离线") }
	settleText(t, tt, "第 1 页 第 1 首")
	if !tt.HasText("第 1 页 第 1 首") {
		t.Fatal("UP主失败时歌曲消失")
	}
}

// TestSearchCreatorRetryRestoresResults locks recovery after a failed creator lookup.
func TestSearchCreatorRetryRestoresResults(t *testing.T) {
	s, tt := newSearchPage(t, "音乐", newSearchStub(24, 24))
	var calls atomic.Int32
	s.search.creators.fetch = func(context.Context, string) ([]app.SearchItem, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("离线")
		}
		return []app.SearchItem{creatorItem("42", "恢复的UP", "", 0)}, nil
	}
	waitFrames(t, tt, "重试入口", func() bool { return tt.HasText("相关 UP 主加载失败，点击重试") })
	if err := tt.Click("重试加载相关 UP 主"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "恢复的UP", func() bool { return tt.HasText("恢复的UP") })
	if got := calls.Load(); got != 2 {
		t.Fatalf("查询次数 = %d，want 2", got)
	}
}

// TestRepeatingSearchRetriesCreators locks recovery when the user searches for the same keyword again.
func TestRepeatingSearchRetriesCreators(t *testing.T) {
	s, tt := newSearchPage(t, "音乐", newSearchStub(24, 24))
	var calls atomic.Int32
	s.search.creators.fetch = func(context.Context, string) ([]app.SearchItem, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("离线")
		}
		return []app.SearchItem{creatorItem("42", "重新搜索的UP", "", 0)}, nil
	}
	waitFrames(t, tt, "加载失败", func() bool { return tt.HasText("相关 UP 主加载失败，点击重试") })
	s.navigate(localMusicPath)
	tt.Frame()
	s.submitSearch("音乐")
	waitFrames(t, tt, "重新搜索的UP", func() bool { return tt.HasText("重新搜索的UP") })
	if got := calls.Load(); got != 2 {
		t.Fatalf("查询次数 = %d，want 2", got)
	}
}

// TestRenderSearchCreatorShots renders the creator strip in light and dark themes at two window widths.
func TestRenderSearchCreatorShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(synthCoverJPEG(42))
	}))
	defer srv.Close()
	for _, dark := range []bool{false, true} {
		for _, width := range []int{1200, 760} {
			name := fmt.Sprintf("search-creators-%t-%d", dark, width)
			t.Run(name, func(t *testing.T) {
				s, tt := newSearchPage(t, "示例歌手", newSearchStub(24, 24), systemIsDark(dark), at(width, 800))
				s.search.creators.fetch = func(context.Context, string) ([]app.SearchItem, error) {
					var out []app.SearchItem
					for i := range 6 {
						out = append(out, creatorItem(fmt.Sprint(i+1), fmt.Sprintf("示例UP%d", i+1), srv.URL+fmt.Sprintf("/%d.jpg", i), bili.Verification(i%3)))
					}
					return out, nil
				}
				waitFrames(t, tt, "账号出现", func() bool { return tt.HasText("示例UP1") })
				waitFrames(t, tt, "歌曲出现", func() bool { return tt.HasText("第 1 页 第 1 首") })
				waitFrames(t, tt, "头像加载", func() bool { return s.pageImages().Get(srv.URL + "/0.jpg").Ready() })
				first, ok := tt.Find("打开 示例UP1 的空间")
				if !ok {
					t.Fatal("找不到第一位 UP 主")
				}
				second, ok := tt.Find("打开 示例UP2 的空间")
				if !ok {
					t.Fatal("找不到第二位 UP 主")
				}
				t.Logf("UP 主项宽 %.1f，横向间距 %.1f", first.W, second.X-first.X-first.W)
				save(t, dir, name+".png", tt)
			})
		}
	}
}
