package ui

// 「墓碑」在侧栏上的样子：失效的那一条**照画**（不画的话用户会发现
// 「收藏夹自己少了一个」而且不知道为什么），但灰掉、点不动、行上写着
// 「已失效」，右键菜单里只剩「取消收藏」「隐藏」还能点。
//
// 判据全在界面上：Tester 点它一下，路由不动；右键弹出的菜单里
// 「播放」「添加到播放列表」是灰的（`ChooseMenuItem` 会拒绝灰项）。

import (
	"slices"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/egoist/mygo/ui"
)

// tombstoneShell 建一个侧栏上有一条失效收藏夹的外壳。
func tombstoneShell(t *testing.T) (*Shell, *app.App, *ui.Tester) {
	t.Helper()
	s, a, tt, _ := favShell(t, 0, 0)
	a.SetFavorites(app.FavoritesCollected, []app.Favorite{
		{ID: 901, Title: "失效的那个", Mid: 78, Type: 11, Invalid: true},
		{ID: 902, Title: "还在的那个", Mid: 79, Type: 11},
	})
	tt.Frame()
	return s, a, tt
}

// TestATombstoneRowIsMarkedAndCannotBeOpened 钉住两件事：行上写着「已失效」，
// 而点它**不进去**。
func TestATombstoneRowIsMarkedAndCannotBeOpened(t *testing.T) {
	s, _, tt := tombstoneShell(t)

	if !tt.HasText("已失效") {
		t.Fatalf("失效的那一行没有标出来，文字有 %q", tt.Texts())
	}
	if err := tt.Click("失效的那个"); err == nil {
		tt.Frame()
		if got := s.router.Path(); got == collectionPath(901) {
			t.Errorf("失效的那一行点进去了（路径是 %q）", got)
		}
	}
	// ⚠️ **还在的那一条照样点得动**：失效只能影响它自己那一行。
	if err := tt.Click("还在的那个"); err != nil {
		t.Fatalf("点「还在的那个」：%v", err)
	}
	tt.Frame()
	if got := s.router.Path(); got != collectionPath(902) {
		t.Errorf("点完停在 %q，want %q", got, collectionPath(902))
	}
}

// TestATombstoneMenuHasNoPlayEntries 钉住右键菜单：
// 「播放」「添加到播放列表」是灰的，「取消收藏」「隐藏」还在。
func TestATombstoneMenuHasNoPlayEntries(t *testing.T) {
	_, _, tt := tombstoneShell(t)

	if err := tt.RightClick("失效的那个"); err != nil {
		t.Fatal(err)
	}
	menu := tt.Menu()
	for _, want := range []string{"取消收藏", "隐藏"} {
		if !slices.Contains(menu, want) {
			t.Errorf("右键菜单里没有 %q：菜单是 %v", want, menu)
		}
	}
	for _, gone := range []string{"播放", "添加到播放列表"} {
		if err := tt.ChooseMenuItem(gone); err == nil {
			t.Errorf("失效行的右键菜单里 %q 还能点（菜单是 %v）", gone, menu)
		}
	}
	tt.CloseMenu()
}
