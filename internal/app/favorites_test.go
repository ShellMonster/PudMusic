package app_test

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// 这一组盯的是「拖完真的存住了」这条验收。
//
// 顺序这件事的坑全在**跨重启**：界面上排对了、下次打开又弹回接口给的顺序，
// 用户只会以为没拖成。所以每一条都落到真的盘上、真的重开一遍。

// favItems 造 n 条条目，ID 从 1 开始，标题是「收藏 1」这种。
func favItems(n int) []app.Favorite {
	out := make([]app.Favorite, n)
	for i := range out {
		out[i] = app.Favorite{ID: int64(i + 1), Title: "收藏 " + strconv.Itoa(i+1), Mid: 42, Type: 2}
	}
	return out
}

// favIDs 是一条列表的 ID，拼成 "1,2,3" 这样，方便断言。
func favIDs(a *app.App, g app.FavoriteGroup) string {
	var out []string
	for _, f := range a.SideFavorites(g) {
		out = append(out, strconv.FormatInt(f.ID, 10))
	}
	return strings.Join(out, ",")
}

// idsOf 是一条存下来的顺序，拼成 "1,2,3" 这样。
func idsOf(v []int64) string {
	out := make([]string, len(v))
	for i, id := range v {
		out[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(out, ",")
}

// favMounted 造一个挂上真设置文件的 App（盘上是空的），返回它和设置文件路径。
func favMounted(t *testing.T) (*app.App, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), app.SettingsFileName)

	a := app.New()
	if err := a.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		a.Close()
		_ = a.CloseSettings()
	})
	return a, path
}

// TestReorderMovesTheRowToWhereItWasDropped 验拖拽排序本身的语义。
//
// 拖拽是**插到目标那一行的位置**，不是和目标换位置。往后拖时这两种算法的
// 结果一样，往回拖时不一样：换位置会把中间那一行顶到最末尾，而用户看到的是
// 自己那一行停在了手里松开的地方。
func TestReorderMovesTheRowToWhereItWasDropped(t *testing.T) {
	a, _ := favMounted(t)
	a.SetFavorites(app.FavoritesCreated, favItems(5))

	// 往后拖：1 号拖到 3 号那一行。
	moved, err := a.ReorderFavorite(app.FavoritesCreated, 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !moved {
		t.Fatal("1 → 3 报告说没动")
	}
	if got := favIDs(a, app.FavoritesCreated); got != "2,3,1,4,5" {
		t.Errorf("往后拖之后是 %s，want 2,3,1,4,5", got)
	}

	// 往回拖：现在 5 号在最末尾，拖到 1 号那一行。
	//
	// 落点是「1 号那一行的前面」，所以 5 停在 2,3 和 1 之间，而不是跑到最前面
	// ——后者是拖到了第 0 行。两种算法的差别正是在这一条上分出来的。
	if moved, err = a.ReorderFavorite(app.FavoritesCreated, 5, 1); err != nil {
		t.Fatal(err)
	}
	if !moved {
		t.Fatal("5 → 1 报告说没动")
	}
	if got := favIDs(a, app.FavoritesCreated); got != "2,3,5,1,4" {
		t.Errorf("往回拖之后是 %s，want 2,3,5,1,4（和目标换位置的算法会给出 4,2,3,1,5）", got)
	}
}

// TestReorderThatChangesNothingWritesNothing 验拖了个寂寞的时候一个字都不写。
//
// 手指在原地抖一下就拖了一次是常事，而每次都落盘 = 每秒重写一遍设置文件。
// Biu 在这种情况下 `return state`（`favorite.ts:112-114`），我们也一样。
func TestReorderThatChangesNothingWritesNothing(t *testing.T) {
	a, _ := favMounted(t)
	a.SetFavorites(app.FavoritesCreated, favItems(3))

	for _, tc := range []struct {
		name       string
		id, target int64
	}{
		{"拖到自己身上", 2, 2},
		{"拖的这一条不在列表里", 99, 1},
		{"落点不在列表里", 1, 99},
	} {
		t.Run(tc.name, func(t *testing.T) {
			moved, err := a.ReorderFavorite(app.FavoritesCreated, tc.id, tc.target)
			if err != nil {
				t.Fatal(err)
			}
			if moved {
				t.Errorf("%s 报告说动了", tc.name)
			}
		})
	}

	if got := a.SettingsSnapshot().SideMenuCollectionOrder.Created; len(got) != 0 {
		t.Errorf("设置里多出了一条没动过就写下去的顺序：%v", got)
	}
	if got := favIDs(a, app.FavoritesCreated); got != "1,2,3" {
		t.Errorf("列表被改了：%s", got)
	}
}

// TestTheTwoGroupsReorderIndependently 验两组各排各的。
//
// 两组的顺序存在设置的同一个结构体里（`{created, collected}`），一次拖拽
// 写错格子的后果是「我创建的」被动了、「我收藏的」的顺序没了，而且界面上
// 看不出来——两组各自排完之后，第一组那次写全都落在第一组那一格。
func TestTheTwoGroupsReorderIndependently(t *testing.T) {
	a, _ := favMounted(t)
	a.SetFavorites(app.FavoritesCreated, favItems(4))
	a.SetFavorites(app.FavoritesCollected, favItems(3))

	if _, err := a.ReorderFavorite(app.FavoritesCreated, 1, 4); err != nil {
		t.Fatal(err)
	}
	// 两组用同一个 ID 空间，只有各自那一格被写才不会串。
	if moved, _ := a.ReorderFavorite(app.FavoritesCollected, 3, 1); !moved {
		t.Fatal("我收藏的那一组拖不动")
	}

	if got := favIDs(a, app.FavoritesCreated); got != "2,3,4,1" {
		t.Errorf("我创建的排成了 %s，want 2,3,4,1", got)
	}
	if got := favIDs(a, app.FavoritesCollected); got != "3,1,2" {
		t.Errorf("我收藏的排成了 %s，want 3,1,2", got)
	}
	order := a.SettingsSnapshot().SideMenuCollectionOrder
	if got := idsOf(order.Created); got != "2,3,4,1" {
		t.Errorf("设置里我创建的顺序是 %s", got)
	}
	if got := idsOf(order.Collected); got != "3,1,2" {
		t.Errorf("设置里我收藏的顺序是 %s", got)
	}
}

// TestTheOrderSurvivesARestart 是这一张的核心验收：「收藏夹能拖拽排序，
// 顺序落盘，重启还在」。
//
// 走的是真文件：拖 → flush → **关掉 App 重开一个** → 重新喂同一批条目
// （接口每次都是这么给回来的）→ 顺序还在。中间任何一步只在内存里转，
// 这一条都会红。
func TestTheOrderSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	items := favItems(5)

	first := app.New()
	if err := first.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	first.SetFavorites(app.FavoritesCreated, items)
	// 1,2,3,4,5 → 把 1 拖到 4 那一行 → 2,3,4,1,5
	if _, err := first.ReorderFavorite(app.FavoritesCreated, 1, 4); err != nil {
		t.Fatal(err)
	}
	// 再把末尾的 5 拖到最前面（2 那一行）→ 5,2,3,4,1
	if _, err := first.ReorderFavorite(app.FavoritesCreated, 5, 2); err != nil {
		t.Fatal(err)
	}
	if got := favIDs(first, app.FavoritesCreated); got != "5,2,3,4,1" {
		t.Fatalf("两次拖完内存里是 %s，want 5,2,3,4,1", got)
	}
	if err := first.FlushSettings(); err != nil {
		t.Fatal(err)
	}
	if err := first.CloseSettings(); err != nil {
		t.Fatal(err)
	}
	first.Close()

	// 重开。接口给的还是接口给的那个顺序——它不知道用户拖过。
	second := app.New()
	t.Cleanup(func() {
		second.Close()
		_ = second.CloseSettings()
	})
	if err := second.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	second.SetFavorites(app.FavoritesCreated, items)

	if got := favIDs(second, app.FavoritesCreated); got != "5,2,3,4,1" {
		t.Errorf("重启之后顺序是 %s，want 5,2,3,4,1", got)
	}
}

// TestFoldersThatWereNotThereWhenTheOrderWasSavedAreNotLost 验存下的顺序里
// 没有的那几条接在后面，而不是被丢掉。
//
// 用户 A 拖好的顺序，用户 B 登录后看到同一个应用：多的那几条如果被丢掉，
// 侧栏上就是「凭空少了几项」。对应 Biu `applySavedOrder` 的后半段
// （`favorite.ts:41-44`）。
func TestFoldersThatWereNotThereWhenTheOrderWasSavedAreNotLost(t *testing.T) {
	path := filepath.Join(t.TempDir(), app.SettingsFileName)

	first := app.New()
	if err := first.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	first.SetFavorites(app.FavoritesCreated, favItems(3))
	if _, err := first.ReorderFavorite(app.FavoritesCreated, 3, 1); err != nil {
		t.Fatal(err)
	}
	if err := first.FlushSettings(); err != nil {
		t.Fatal(err)
	}
	if err := first.CloseSettings(); err != nil {
		t.Fatal(err)
	}
	first.Close()

	// 接口这次多给了两条（4、5），存下的顺序里没有它们。
	second := app.New()
	t.Cleanup(func() {
		second.Close()
		_ = second.CloseSettings()
	})
	if err := second.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	second.SetFavorites(app.FavoritesCreated, favItems(5))

	if got := favIDs(second, app.FavoritesCreated); got != "3,1,2,4,5" {
		t.Errorf("重启之后顺序是 %s，want 3,1,2,4,5（多的两条接在后面，一条都不能少）", got)
	}
}

// TestAFolderTheUserDeletedIsDroppedFromTheSavedOrder 验存下的顺序里已经
// 不存在的 ID 不占位。
//
// 留着的话，界面上那一行前面会有一个空档，后面每加一条新收藏夹都得跟着错位。
func TestAFolderTheUserDeletedIsDroppedFromTheSavedOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	items := favItems(4)

	first := app.New()
	if err := first.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	first.SetFavorites(app.FavoritesCreated, items)
	if _, err := first.ReorderFavorite(app.FavoritesCreated, 1, 4); err != nil {
		t.Fatal(err)
	}
	if err := first.FlushSettings(); err != nil {
		t.Fatal(err)
	}
	if err := first.CloseSettings(); err != nil {
		t.Fatal(err)
	}
	first.Close()

	second := app.New()
	t.Cleanup(func() {
		second.Close()
		_ = second.CloseSettings()
	})
	if err := second.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	// 4 号这次没返回（用户在 B 站那边删了）。
	second.SetFavorites(app.FavoritesCreated, items[:3])

	if got := favIDs(second, app.FavoritesCreated); got != "2,3,1" {
		t.Errorf("顺序是 %s，want 2,3,1", got)
	}
}

// TestNoSavedOrderMeansTheOrderTheApiGave 验还没排过序时接口给什么就是什么。
func TestNoSavedOrderMeansTheOrderTheApiGave(t *testing.T) {
	a, _ := favMounted(t)
	a.SetFavorites(app.FavoritesCreated, favItems(3))
	if got := favIDs(a, app.FavoritesCreated); got != "1,2,3" {
		t.Errorf("顺序是 %s，want 1,2,3", got)
	}
}

// TestAFreshInstallHasTwoEmptyGroups 验空状态：两组都在，都空。
//
// 「还没接接口」是这一阶段的正常状态，不该是「少了点什么」——所以这里钉住
// 两组都是空序列而不是 nil，界面才有东西可画折叠标题。
func TestAFreshInstallHasTwoEmptyGroups(t *testing.T) {
	a, _ := favMounted(t)

	for _, g := range []app.FavoriteGroup{app.FavoritesCreated, app.FavoritesCollected} {
		if got := a.SideFavorites(g); len(got) != 0 {
			t.Errorf("%s 一开始就有 %d 条", g, len(got))
		}
	}
	order := app.DefaultSettings("").SideMenuCollectionOrder
	if order.Created == nil || order.Collected == nil {
		t.Errorf("默认设置的顺序是 %+v，两格都该是空序列而不是 nil", order)
	}
}

// TestTheSidebarWidthIsClampedOnTheWayIn 验侧栏宽度从设置文件进来就被钳住。
//
// 设置文件是用户能直接编辑的，也可能从 B 站那边的导入、手改的备份里来。
// 一个 `sideMenuWidth: 99999` 不该一路穿到画界面的那一行——那样侧栏会把
// 整个窗口吃掉，内容区一点不剩。
func TestTheSidebarWidthIsClampedOnTheWayIn(t *testing.T) {
	for _, tc := range []struct {
		in   float64
		want float64
	}{
		{99999, app.MaxSideMenuWidth},
		{0, app.MinSideMenuWidth},
		{-5, app.MinSideMenuWidth},
		{320, 320},
	} {
		if got := app.ClampSideMenuWidth(tc.in); got != tc.want {
			t.Errorf("ClampSideMenuWidth(%v) = %v，want %v", tc.in, got, tc.want)
		}
	}

	path := filepath.Join(t.TempDir(), app.SettingsFileName)
	write(t, path, `{"schemaVersion":1,"appSettings":{"sideMenuWidth":99999}}`)
	got, err := app.LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.SideMenuWidth != app.MaxSideMenuWidth {
		t.Errorf("读进来的是 %v，want %v", got.SideMenuWidth, app.MaxSideMenuWidth)
	}
}
