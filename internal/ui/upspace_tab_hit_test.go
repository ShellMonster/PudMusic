// UP 空间 Tab 条的命中区域。
//
// 这一组验的是**点得到**：空/加载中那一块比正文区高，会从中缝溢到 Tab 条上，
// 把 Tab 的点击接走：切到空的收藏夹 / 合集之后点不动别的 Tab。
package ui

import (
	"net/http"
	"testing"

	mygoui "github.com/egoist/mygo/ui"
)

// clickUpTab 点 Tab 条上 name 那一项可见区域的中心。
//
// ⚠️ **用坐标而不是 `tt.Click(name)`**：侧栏「收藏夹」分组头是同一个词，而这一组
// 要验的正是 Tab 条的命中区域（直接量坐标本来就是这组测试的对象）。
func clickUpTab(t *testing.T, tt *mygoui.Tester, name string) {
	t.Helper()
	tab, ok := tt.AccessByRole(mygoui.RoleTab, name)
	if !ok {
		t.Fatalf("Tab 条上没有「%s」，文字有 %q", name, tt.Texts())
	}
	tt.ClickAt(tab.Bounds.X+tab.Bounds.W/2, tab.Bounds.Y+tab.Bounds.H/2)
	tt.Frame()
}

// TestTheTabBarTakesClicksWhenTheTabsHaveNoRows 钉住用户那条路：投稿、收藏夹、
// 合集都空着时，依次点每一张 Tab，页面真的跟着切。
//
// 曾经出错：空的正文块（1200×800 下从 y≈345 起）盖住 Tab 文字（y 334..383），
// 只有最上面十几像素的留白点得动，看着像卡死。
func TestTheTabBarTakesClicksWhenTheTabsHaveNoRows(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "空的 UP", 0, 1)
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items": []any{uiDynamic("d1", "动态")}, "offset": "", "has_more": false})
	})
	b.on("/x/space/wbi/arc/search", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"list": map[string]any{"vlist": []any{}}, "page": map[string]any{"count": 0}})
	})
	b.on("/x/v3/fav/folder/created/list", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"count": 0, "list": []any{}, "has_more": false})
	})
	b.on("/x/polymer/web-space/seasons_series_list", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items_lists": map[string]any{
			"seasons_list": []any{}, "series_list": []any{}, "page": map[string]any{"total": 0},
		}})
	})

	// 800 是踩到的那一档窗口高度；更高的窗口本来就不复现，钉住它才挡得住回归。
	s, tt, _ := newSpaceShell(t, b, false, "", at(1200, 800))
	sp := openSpace(t, s, tt, 42)
	sp.tab = upTabVideo
	tt.Frame()
	waitFrames(t, tt, "投稿那一页落到空状态", func() bool {
		p := sp.videos.Paged()
		return sp.videosStarted && p.Loaded == 0 && !p.Loading && p.Err == nil
	})

	clickUpTab(t, tt, "收藏夹")
	if sp.tab != upTabCollection {
		t.Fatalf("点「收藏夹」没切过去（tab=%d）", sp.tab)
	}
	waitFrames(t, tt, "收藏夹那一页落地", func() bool { return sp.fav.started && !sp.fav.loading })

	clickUpTab(t, tt, "合集")
	if sp.tab != upTabSeries {
		t.Fatalf("收藏夹里点「合集」没切过去（tab=%d）", sp.tab)
	}
	waitFrames(t, tt, "合集那一页落地", func() bool { return sp.ser.started && !sp.ser.loading })

	clickUpTab(t, tt, "投稿")
	if sp.tab != upTabVideo {
		t.Fatalf("合集里点「投稿」没切过去（tab=%d）", sp.tab)
	}
}

// TestTheTabBarTakesClicksWhileATabLoads 钉住加载态：骨架屏比空状态更高，
// 在它还在拉的时候从别的 Tab 切走必须点得动。
func TestTheTabBarTakesClicksWhileATabLoads(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "刚打开就切走的 UP", 0, 1)
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items": []any{uiDynamic("d1", "动态")}, "offset": "", "has_more": false})
	})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	b.on("/x/polymer/web-space/seasons_series_list", func(w http.ResponseWriter, r *http.Request) {
		<-release
		uiExploreJSON(w, 0, map[string]any{"items_lists": map[string]any{
			"seasons_list": []any{}, "series_list": []any{}, "page": map[string]any{"total": 0},
		}})
	})

	s, tt, _ := newSpaceShell(t, b, false, "", at(1200, 800))
	sp := openSpace(t, s, tt, 42)
	sp.tab = upTabSeries
	tt.Frame()
	waitFor(t, "合集那一页发出请求", func() bool { return sp.ser.loading })
	tt.Frame()

	clickUpTab(t, tt, "投稿")
	if sp.tab != upTabVideo {
		t.Fatalf("合集还在加载时点「投稿」没切过去（tab=%d）", sp.tab)
	}
}
