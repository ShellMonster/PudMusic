package ui

import (
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
)

// TestNarrowHomeShowsHeroAndArtistSearch 锁定窄窗口可查看主视觉并通过热门歌手进入搜索。此前重复的主视觉 key 会让首页崩溃。
func TestNarrowHomeShowsHeroAndArtistSearch(t *testing.T) {
	b := newTestShell(t, at(760, 800))
	b.s.home.SetSongs(nil, []list.Music{
		{ID: "chart1", Title: "榜单歌曲", Owner: "示例歌手", Item: app.Item{Bvid: "BV1"}},
	}, nil, nil)
	b.tt.Frame()
	if !b.tt.HasText("榜单歌曲") {
		t.Fatalf("窄窗口没有显示主视觉，画面文字：%q", b.tt.Texts())
	}
	b.tt.Scroll(400, 400, 0, 700)
	waitFrames(t, b.tt, "热门歌手入口", func() bool {
		r, ok := b.tt.Find("搜索 示例歌手")
		return ok && r.H > 0 && r.Y >= 0 && r.Y+r.H < 716
	})
	if err := b.tt.Click("搜索 示例歌手"); err != nil {
		t.Fatal(err)
	}
	b.tt.Frame()
	if !strings.HasPrefix(b.s.router.Location(), searchPath+"?") || b.s.router.Query(searchQuery) != "示例歌手" {
		t.Fatalf("点击后没有进入对应搜索页：%s", b.s.router.Location())
	}
}
