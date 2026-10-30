package app_test

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

func TestRemountReplacesStateWithoutReusingIt(t *testing.T) {
	a := app.New()
	defer a.Close()
	first := app.SettingsPath(t.TempDir())
	second := app.SettingsPath(t.TempDir())
	if err := a.MountSettings(first); err != nil {
		t.Fatal(err)
	}
	if err := a.SetThemeMode(app.ThemeModeDark); err != nil {
		t.Fatal(err)
	}
	if err := a.MountSettings(second); err != nil {
		t.Fatal(err)
	}
	if got := a.SettingsSnapshot().ThemeMode; got != app.ThemeModeSystem {
		t.Fatalf("重挂载没有替换设置状态：got=%q", got)
	}
}

func TestCloseReleasesMountedAndKeyedState(t *testing.T) {
	a := app.New()
	cli := bili.NewClient(bili.Options{})
	a.MountRecommend(cli)
	a.MountLibrary(cli)
	a.MountExplore(cli)

	recommend := a.RecommendOf()
	library := a.LibraryOf()
	favorites := a.FavoritesOf()
	space := a.UpSpaceOf(77)
	favorites.Replace(app.FavoritesCreated, []app.Favorite{{ID: 1}}, nil)

	a.Close()

	if got := a.RecommendOf(); got == recommend || got.Mounted() {
		t.Fatalf("关闭后推荐状态仍被复用：got=%p old=%p mounted=%v", got, recommend, got.Mounted())
	}
	if got := a.LibraryOf(); got == library || got.Mounted() {
		t.Fatalf("关闭后稍后再看状态仍被复用：got=%p old=%p mounted=%v", got, library, got.Mounted())
	}
	if got := a.FavoritesOf(); got == favorites || len(got.Snapshot(app.FavoritesCreated)) != 0 {
		t.Fatalf("关闭后收藏夹状态仍被复用：got=%p old=%p items=%v", got, favorites, got.Snapshot(app.FavoritesCreated))
	}
	if got := a.UpSpaceOf(77); got == space || got.Snapshot().Loaded {
		t.Fatalf("关闭后 UP 空间状态仍被复用：got=%p old=%p snapshot=%+v", got, space, got.Snapshot())
	}
}
