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

// TestSearchCreatorStripKeepsEightUniqueCreators locks the strip's cap and dedupe:
// DESIGN.md promises 「最多 8 个相关 UP 主」, and one account showing up twice in the
// same page must not draw two cards.
func TestSearchCreatorStripKeepsEightUniqueCreators(t *testing.T) {
	s, tt := newSearchPage(t, "音乐", newSearchStub(24, 24))
	s.search.creators.fetch = func(context.Context, string) ([]app.SearchItem, error) {
		out := make([]app.SearchItem, 0, 11)
		for i := 1; i <= 10; i++ {
			out = append(out, creatorItem(fmt.Sprint(i), fmt.Sprintf("UP%02d", i), "", 0))
		}
		out = append(out, creatorItem("1", "重复的UP", "", 0))
		return out, nil
	}
	waitFrames(t, tt, "前 8 位出现", func() bool { return tt.HasText("UP08") })
	if tt.HasText("UP09") || tt.HasText("UP10") {
		t.Fatalf("相关 UP 主没有截到 8 个：%q", tt.Texts())
	}
	if tt.HasText("重复的UP") {
		t.Fatal("同一个 mid 画了两张卡")
	}
}

// TestSearchCreatorViewAllOpensUserTab locks 「查看全部」切到 UP 主标签.
func TestSearchCreatorViewAllOpensUserTab(t *testing.T) {
	s, tt := newSearchPage(t, "音乐", newSearchStub(24, 24))
	s.search.creators.fetch = func(context.Context, string) ([]app.SearchItem, error) {
		return []app.SearchItem{creatorItem("42", "示例UP", "", 0)}, nil
	}
	waitFrames(t, tt, "相关UP主出现", func() bool { return tt.HasText("示例UP") })
	if err := tt.Click("查看全部 UP 主"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := s.search.tab; got != searchTabUser {
		t.Fatalf("「查看全部」之后的 Tab = %d，want %d", got, searchTabUser)
	}
}

// TestSearchCreatorHeaderKeepsPageGeometry locks that the creator preview does not
// move the page: the eyebrow keeps the page-head offset and the tabs stay put when
// the preview lands (they used to jump ~50px and the header dropped its 24px pad).
func TestSearchCreatorHeaderKeepsPageGeometry(t *testing.T) {
	s, tt := newSearchPage(t, "音乐", newSearchStub(24, 24))
	entered, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	s.search.creators.fetch = func(context.Context, string) ([]app.SearchItem, error) {
		close(entered)
		<-release
		return []app.SearchItem{creatorItem("42", "示例UP", "", 0)}, nil
	}
	tt.Frame()
	<-entered
	tt.Frame()
	headBefore, ok := tt.Find("搜索「音乐」")
	if !ok {
		t.Fatalf("预览还在路上时没有页头小标：%q", tt.Texts())
	}
	tabBefore, ok := tt.Find("UP 主")
	if !ok {
		t.Fatalf("预览还在路上时没有 Tab：%q", tt.Texts())
	}
	close(release)
	waitFrames(t, tt, "示例UP出现", func() bool { return tt.HasText("示例UP") })
	headAfter, _ := tt.Find("搜索「音乐」")
	tabAfter, _ := tt.Find("UP 主")
	if headBefore.Y != headAfter.Y {
		t.Errorf("页头小标的 y 从 %.1f 跳到 %.1f", headBefore.Y, headAfter.Y)
	}
	// 留 4px 余量：骨架里名字那一块固定高，真名字的行盒按字体度量算（差 2px 上下，
	// 其它 rail 骨架也一样）。这里锁的是页头几何和那一排的高度都被占住了——改之前
	// Tab 一下挪约 50px。
	if jump := tabAfter.Y - tabBefore.Y; jump > 4 || jump < -4 {
		t.Errorf("Tab 的 y 从 %.1f 跳到 %.1f", tabBefore.Y, tabAfter.Y)
	}
}

// TestSearchCreatorKeywordKeepsItsCase locks that the eyebrow shows the keyword as
// typed: `typo.Eyebrow` upper-cases, and 「Taylor Swift」 used to render as
// 「TAYLOR SWIFT」.
func TestSearchCreatorKeywordKeepsItsCase(t *testing.T) {
	s, tt := newSearchPage(t, "Taylor Swift", newSearchStub(24, 24))
	s.search.creators.fetch = func(context.Context, string) ([]app.SearchItem, error) {
		return []app.SearchItem{creatorItem("42", "示例UP", "", 0)}, nil
	}
	waitFrames(t, tt, "相关UP主出现", func() bool { return tt.HasText("示例UP") })
	if !tt.HasText("Taylor Swift") {
		t.Fatalf("搜索词的大小写被吃掉了：%q", tt.Texts())
	}
	if tt.HasText("TAYLOR SWIFT") {
		t.Fatal("搜索词被转成了全大写")
	}
}

// TestSearchCreatorRetryHiddenForPermanentFailure locks that the retry entry follows
// `SearchError.Retryable`: a rate-limit answer would fail again on the spot.
func TestSearchCreatorRetryHiddenForPermanentFailure(t *testing.T) {
	s, tt := newSearchPage(t, "音乐", newSearchStub(24, 24))
	s.search.creators.fetch = func(context.Context, string) ([]app.SearchItem, error) {
		return nil, &app.SearchError{Message: "搜索太频繁了", Category: app.SearchErrorTooFrequent, Retryable: false}
	}
	settleText(t, tt, "第 1 页 第 1 首")
	if tt.HasText("相关 UP 主加载失败") {
		t.Fatalf("不可重试的错误也给了重试键：%q", tt.Texts())
	}
}

// TestSearchUserTabReusesCreatorPreview locks that opening the UP 主 tab does not
// repeat the user search the preview already made: B站 throttles search with
// -412/-509, so doubling the request per query turns into 「搜索太频繁」.
//
// The shell has no B站 client here, so the only way `fetchSearchTab` can succeed is
// the cached preview page.
func TestSearchUserTabReusesCreatorPreview(t *testing.T) {
	s, _ := newSearchPage(t, "音乐", newSearchStub(24, 24))
	s.search.creators.storePage("音乐", app.SearchPage{
		Items:   []app.SearchItem{creatorItem("42", "复用的UP", "", 0)},
		Total:   1,
		HasMore: false,
	})
	tab := &s.search.lists[searchTabUser]
	tab.mu.Lock()
	tab.query = searchQuerySpec{keyword: "音乐"}
	tab.mu.Unlock()

	page, err := s.fetchSearchTab(searchTabUser, context.Background(), 1)
	if err != nil {
		t.Fatalf("「用户」标签没有复用预览：%v", err)
	}
	if len(page.Songs) != 1 || page.Songs[0].Title != "复用的UP" {
		t.Fatalf("复用的行 = %+v", page.Songs)
	}
	if _, ok := s.search.creators.takePage("音乐"); ok {
		t.Fatal("预览页被用了两次")
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
