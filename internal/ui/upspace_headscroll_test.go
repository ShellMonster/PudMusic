package ui

import (
	"fmt"
	"net/http"
	"testing"
)

// TestTheSpaceHeaderScrollsAwayWithEveryTab 钉住「头图区跟着列表一起滚走」。
//
// 上游整页是一个滚动容器，头图（横幅、头像、名字、关注）和下面的列表在一起，滚一段头图
// 就出屏。我们此前把头图摆在列表**外面**，于是滚列表时头图钉在顶上不动，列表从它底下
// 滑过去。判据是画面：滚动之后昵称的 y 变小了（出了屏幕或被推到上面去）。
func TestTheSpaceHeaderScrollsAwayWithEveryTab(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "滚动测试的 UP", 0, 1)
	var dyn, vids, cols []any
	for i := 0; i < 40; i++ {
		dyn = append(dyn, uiDynamic(fmt.Sprintf("d%d", i), fmt.Sprintf("第%d条动态", i)))
		vids = append(vids, map[string]any{"bvid": fmt.Sprintf("BV%d", i), "title": fmt.Sprintf("投稿%d", i), "length": "3:20", "cid": "9"})
		cols = append(cols, map[string]any{"id": 100 + i, "title": fmt.Sprintf("收藏夹%d", i), "media_count": 3})
	}
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items": dyn, "offset": "", "has_more": false})
	})
	b.on("/x/space/wbi/arc/search", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"list": map[string]any{"vlist": vids}, "page": map[string]any{"count": len(vids)}})
	})
	b.on("/x/v3/fav/folder/created/list", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"list": cols})
	})
	b.on("/x/polymer/web-space/seasons_series_list", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items_lists": map[string]any{"seasons_list": []any{}, "page": map[string]any{"total": 0}}})
	})

	s, tt, _ := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]

	for _, tab := range []int{upTabVideo, upTabDynamic, upTabCollection} {
		sp.tab = tab
		tt.Frame()
		waitFrames(t, tt, "Tab "+upTabNames[tab]+" 加载完", func() bool {
			if tab == upTabVideo {
				return sp.videos.Paged().Loaded > 0
			}
			return sp.pagerOf(tab).started && upRowCount(sp, tab) > 0
		})
		tt.Frame()
		before, ok := tt.Find("滚动测试的 UP")
		if !ok {
			t.Fatalf("[%s] 画面上找不到昵称，文字有 %q", upTabNames[tab], tt.Texts())
		}
		tt.Scroll(600, 500, 0, 300)
		tt.Frame()
		after, ok := tt.Find("滚动测试的 UP")
		if ok && after.Y >= before.Y-100 {
			t.Errorf("[%s] 滚了 300 之后昵称 y 从 %.1f 变成 %.1f——头图钉在顶上没跟着滚", upTabNames[tab], before.Y, after.Y)
		}
	}
}
