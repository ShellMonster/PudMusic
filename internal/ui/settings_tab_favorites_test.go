package ui

import (
	"slices"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// 设置页「收藏夹」那一组：把侧栏里被藏起来的收藏夹放回来。
//
// 右键菜单里「隐藏」早就有了（`sidebar_favorites.go`），缺的是**放回来的入口**——
// 藏掉之后那一行从侧栏上消失，用户没有第二个地方能把它找回来。

// TestHiddenFavoritesShowUpOnTheSettingsTab 验藏起来的收藏夹**摆得出来**：
// 侧栏上看不见它了，而设置页那一组里它的勾选框是空的。
func TestHiddenFavoritesShowUpOnTheSettingsTab(t *testing.T) {
	st := app.DefaultSettings("")
	st.HiddenMenuKeys = []string{"42"}
	tt, s, _, _ := shellOnSettings(t, st)

	s.app.SetFavorites(app.FavoritesCreated, []app.Favorite{
		{ID: 42, Title: "藏起来的"},
		{ID: 7, Title: "看得见的"},
	})
	tt.Frame()

	// 侧栏上只剩看得见的那个。
	if countText(tt, "藏起来的") != 0 {
		t.Fatalf("藏起来的收藏夹还摆在侧栏上：%q", tt.Texts())
	}
	if countText(tt, "看得见的") == 0 {
		t.Fatalf("侧栏上没有「看得见的」：%q", tt.Texts())
	}

	// 设置页两个都摆——藏起来的那个是「勾掉」的状态。
	openSettingsTab(t, tt, "菜单设置")
	for _, want := range []string{"收藏夹", "我创建的", "藏起来的", "看得见的"} {
		if !hasExactText(tt, want) {
			t.Fatalf("设置页上没有 %q，文字有 %q", want, tt.Texts())
		}
	}
}

// TestUnhidingAFavoriteFromTheSettingsTab 验勾回来那一下**真的把它放回侧栏**，
// 而且 `hiddenMenuKeys` 里那笔划掉了。
func TestUnhidingAFavoriteFromTheSettingsTab(t *testing.T) {
	st := app.DefaultSettings("")
	st.HiddenMenuKeys = []string{"42"}
	tt, s, a, _ := shellOnSettings(t, st)

	s.app.SetFavorites(app.FavoritesCreated, []app.Favorite{{ID: 42, Title: "藏起来的"}})
	tt.Frame()
	openSettingsTab(t, tt, "菜单设置")

	clickCheckbox(t, tt, "在侧栏里显示收藏夹「藏起来的」")
	if got := a.SettingsSnapshot().HiddenMenuKeys; len(got) != 0 {
		t.Fatalf("勾回来之后还藏着 %q", got)
	}
	if !hasExactText(tt, "藏起来的") {
		t.Fatalf("放回侧栏之后侧栏上没有它：%q", tt.Texts())
	}
}

// TestHidingAFavoriteFromTheSettingsTab 验设置页上勾掉也是同一条路：
// 设置里藏掉了，侧栏上那一行跟着消失——**不用先藏再刷**。
func TestHidingAFavoriteFromTheSettingsTab(t *testing.T) {
	tt, s, a, _ := shellOnSettings(t, app.DefaultSettings(""))

	s.app.SetFavorites(app.FavoritesCreated, []app.Favorite{{ID: 7, Title: "看得见的"}})
	tt.Frame()
	openSettingsTab(t, tt, "菜单设置")

	clickCheckbox(t, tt, "在侧栏里显示收藏夹「看得见的」")
	if got := a.SettingsSnapshot().HiddenMenuKeys; !slices.Contains(got, "7") {
		t.Fatalf("勾掉之后没藏进设置：%q", got)
	}
	// 侧栏和设置页各摆过一次「看得见的」的字；藏掉之后侧栏那次没了，只剩
	// 设置页复选框那处。
	if got := countText(tt, "看得见的"); got != 1 {
		t.Fatalf("藏掉之后画面上有 %d 处「看得见的」，want 1（只剩复选框）", got)
	}
}

// TestAnOrphanedHiddenFavoriteIsStillReachable 验收藏夹已经不在接口返回里、
// 可 `hiddenMenuKeys` 还留着它的 id 的时候，那一笔**还能从设置页清掉**。
// 不然它就永远躺在设置文件里——用户在 B 站删了收藏夹，这边永远删不掉那笔。
func TestAnOrphanedHiddenFavoriteIsStillReachable(t *testing.T) {
	st := app.DefaultSettings("")
	st.HiddenMenuKeys = []string{"99999"}
	tt, _, a, _ := shellOnSettings(t, st)

	// 接口那边一个收藏夹都没有（SetFavorites 都不调），只有名单里这一笔。
	openSettingsTab(t, tt, "菜单设置")
	if !hasExactText(tt, "收藏夹 99999（已不在列表里）") {
		t.Fatalf("孤儿的那笔没摆出来，文字有 %q", tt.Texts())
	}
	clickCheckbox(t, tt, "取消隐藏已不在列表里的收藏夹 99999")
	if got := a.SettingsSnapshot().HiddenMenuKeys; len(got) != 0 {
		t.Fatalf("勾上之后孤儿的那笔还在：%q", got)
	}
}

// TestMenuVisibilitySettingsLeaveFavoritesAlone 验上面「系统默认菜单」那组
// 的勾选**不动收藏夹那几笔**——`hiddenMenuKeys` 是一份名单两伙人在写
// （`mergeHiddenMenuKeys` 防的那件事）。
func TestMenuVisibilitySettingsLeaveFavoritesAlone(t *testing.T) {
	st := app.DefaultSettings("")
	st.HiddenMenuKeys = []string{"42"}
	tt, _, a, _ := shellOnSettings(t, st)
	openSettingsTab(t, tt, "菜单设置")

	clickCheckbox(t, tt, "在侧栏里显示「推荐音乐」")
	if got := a.SettingsSnapshot().HiddenMenuKeys; !slices.Contains(got, "42") {
		t.Fatalf("菜单的勾选把收藏夹那笔抹掉了：%q", got)
	}
}
