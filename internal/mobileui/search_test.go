package mobileui

import (
	"testing"
	"time"

	desktop "github.com/ShellMonster/PudMusic/internal/ui"
	"github.com/egoist/mygo/ui"
)

// TestMobileSearchSubmitsAndShowsDistinctResultTypes checks native search focus, suggestions, and the four real result paths.
func TestMobileSearchSubmitsAndShowsDistinctResultTypes(t *testing.T) {
	s, wait := mobileBrowseFixture(t)
	s.openSearch()
	tester := ui.NewTester(func(c *ui.Context) {
		desktop.ApplyTheme(c, s.app.SettingsSnapshot(), c.Theme().Dark)
		ui.Scroll(c).Fill().Children(func() {
			ui.Column(c).FillWidth().Padding(8, 16).Children(func() {
				page := s.current()
				if page.kind == pageSearch {
					s.search(c, page)
				}
			})
		})
	}, 390, 740)
	const input = "搜索歌曲、歌手、歌单或 UP 主"
	if !tester.Focused(input) {
		t.Fatal("opening search did not focus its input")
	}
	tester.Type("音乐")
	tester.Advance(time.Second)
	wait(tester, "音乐联想")
	tester.Key(0, ui.KeyEnter)
	wait(tester, "歌曲结果")
	mobileLibraryShot(t, tester, "search-fixture.png")
	if tester.Focused(input) {
		t.Fatal("submission did not dismiss input focus")
	}
	if err := tester.Click("排序"); err != nil {
		t.Fatal(err)
	}
	for until := tester.Now().Add(time.Second); tester.FramePending() && tester.Now().Before(until); {
		tester.Advance(10 * time.Millisecond)
	}
	if err := tester.Click("最新发布"); err != nil {
		t.Fatal(err)
	}
	wait(tester, "最新歌曲结果")
	for _, tab := range []struct{ label, want string }{
		{"歌单", "公开音乐合集"}, {"UP 主", "搜索到的 UP 主"}, {"视频", "视频结果"},
	} {
		if err := tester.Click(tab.label); err != nil {
			t.Fatal(err)
		}
		wait(tester, tab.want)
		if access, found := tester.AccessByRole(ui.RoleTab, tab.label); !found || !access.Selected {
			t.Fatalf("%q is not marked as the selected result tab", tab.label)
		}
		if tab.label == "歌单" {
			mobileLibraryShot(t, tester, "search-playlist-selected-light.png")
			tester.SetDark(true)
			mobileLibraryShot(t, tester, "search-playlist-selected-dark.png")
			tester.SetDark(false)
		}
	}
	if err := tester.Click("清除搜索词"); err != nil {
		t.Fatal(err)
	}
	wait(tester, "搜索历史")
	if err := tester.Click("删除搜索记录 音乐"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tester.Find("删除搜索记录 音乐"); ok {
		t.Fatal("removed search term still appears in history")
	}
}
