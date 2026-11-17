package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
)

// TestHotArtistSearchOpensResults locks the real chart singer's search action.
func TestHotArtistSearchOpensResults(t *testing.T) {
	b := newTestShell(t)
	b.s.home.SetSongs(nil, []list.Music{
		{ID: "one", Title: "歌一", Owner: "陈奕迅", Item: app.Item{Bvid: "BV1"}},
		{ID: "two", Title: "歌二", Owner: "陈奕迅", Item: app.Item{Bvid: "BV2"}},
		{ID: "three", Title: "歌三", Owner: "王菲", Item: app.Item{Bvid: "BV3"}},
	}, nil, nil)
	b.tt.Frame()
	b.tt.Scroll(700, 400, 0, 500)
	waitFrames(t, b.tt, "热榜歌手", func() bool { return b.tt.HasText("热门歌手") && b.tt.HasText("陈奕迅") })
	if err := b.tt.Click("搜索 陈奕迅"); err != nil {
		t.Fatal(err)
	}
	b.tt.Frame()
	if got := b.s.router.Query(searchQuery); got != "陈奕迅" {
		t.Fatalf("搜索词 = %q", got)
	}
	if got := b.s.router.Location(); !strings.HasPrefix(got, searchPath+"?") {
		t.Fatalf("页面 = %q", got)
	}
}

// TestRenderHotArtistsShots renders the chart artist row in both themes and window widths.
func TestRenderHotArtistsShots(t *testing.T) {
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
			name := fmt.Sprintf("home-artists-%t-%d", dark, width)
			t.Run(name, func(t *testing.T) {
				b := newTestShell(t, systemIsDark(dark), at(width, 800))
				cache := imgcache.New(imgcache.Options{Client: srv.Client()})
				t.Cleanup(cache.Close)
				b.s.home.cache = cache
				chart := make([]list.Music, 6)
				for i := range chart {
					chart[i] = list.Music{ID: fmt.Sprintf("chart%d", i), Title: fmt.Sprintf("歌曲%d", i), Owner: fmt.Sprintf("歌手%d", i), Cover: srv.URL + "/cover.jpg", Item: app.Item{Bvid: fmt.Sprintf("BV%d", i)}}
				}
				b.s.home.SetSongs(nil, chart, nil, nil)
				b.tt.Frame()
				waitFrames(t, b.tt, "热门歌手出现", func() bool { return b.tt.HasText("热门歌手") })
				b.tt.Scroll(float32(width)/2, 400, 0, 400)
				b.tt.Frame()
				waitFrames(t, b.tt, "歌手入口进入视口", func() bool {
					r, ok := b.tt.Find("搜索 歌手0")
					return ok && r.Y >= 0 && r.Y+r.H < 716
				})
				first, ok := b.tt.Find("搜索 歌手0")
				if !ok {
					t.Fatal("找不到第一位歌手")
				}
				second, ok := b.tt.Find("搜索 歌手1")
				if !ok {
					t.Fatal("找不到第二位歌手")
				}
				wantWidth := float32(126)
				if width <= 760 {
					wantWidth = 100
				}
				if first.W != wantWidth {
					t.Fatalf("歌手项宽 %.1f，want %.1f", first.W, wantWidth)
				}
				t.Logf("歌手项宽 %.1f，横向间距 %.1f", first.W, second.X-first.X-first.W)
				save(t, dir, name+".png", b.tt)
			})
		}
	}
}
