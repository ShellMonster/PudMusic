package app_test

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
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

// TestReorderPutsTheRowInFrontOfTheDropPoint 钉住拖拽排序的**语义**：
// 落点是「插到第 to 行前面」，不是「挪到第 to 行的位置」。
//
// 差一格的那个坑：Biu 的 `reorderList` 是 `splice(to, 0, moved)`，to 取目标
// **删之前**的下标，语义是「挪到目标的最终位置」。从目标前面拖到后面时，按旧
// 下标插进去会越过目标一行——屏幕上是指示线画在目标**上边缘**、落下去却停在
// 目标**下面**，而且错的顺序会立刻被写进设置文件。
//
// 往下拖和往上拖要分开看，因为只有往下拖会错：往上拖时 from > before，删除
// 不影响 before 的下标，两种算法碰巧一样。所以这张表两头都摆上。
func TestReorderPutsTheRowInFrontOfTheDropPoint(t *testing.T) {
	for _, tc := range []struct {
		name   string
		id     int64 // 拖哪一条
		before int64 // 放到哪一条前面；0 = 最后面
		want   string
	}{
		{"往下拖一格", 1, 3, "2,1,3,4"},
		{"往下拖两格", 1, 4, "2,3,1,4"},
		{"往下拖到最末", 1, 0, "2,3,4,1"},
		{"往上拖一格", 4, 2, "1,4,2,3"},
		{"往上拖到最前", 4, 1, "4,1,2,3"},
		{"从中间往下拖", 2, 4, "1,3,2,4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := favMounted(t)
			a.SetFavorites(app.FavoritesCreated, favItems(4))

			moved, err := a.ReorderFavorite(app.FavoritesCreated, tc.id, tc.before)
			if err != nil {
				t.Fatal(err)
			}
			if !moved {
				t.Fatal("报告说没动")
			}
			if got := favIDs(a, app.FavoritesCreated); got != tc.want {
				t.Errorf("顺序是 %s，want %s", got, tc.want)
			}
			// 落盘的必须是屏幕上那个顺序，不是另一个。
			if got := idsOf(a.SettingsSnapshot().SideMenuCollectionOrder.Created); got != tc.want {
				t.Errorf("落到盘上的顺序是 %s，want %s", got, tc.want)
			}
		})
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

	// 1 挪到 4 那一行的**前面**（不是它的位置）：2,3,1,4。
	if got := favIDs(a, app.FavoritesCreated); got != "2,3,1,4" {
		t.Errorf("我创建的排成了 %s，want 2,3,1,4", got)
	}
	if got := favIDs(a, app.FavoritesCollected); got != "3,1,2" {
		t.Errorf("我收藏的排成了 %s，want 3,1,2", got)
	}
	order := a.SettingsSnapshot().SideMenuCollectionOrder
	if got := idsOf(order.Created); got != "2,3,1,4" {
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
	// 1,2,3,4,5 → 把 1 挪到 4 那一行前面 → 2,1,3,4,5
	if _, err := first.ReorderFavorite(app.FavoritesCreated, 1, 4); err != nil {
		t.Fatal(err)
	}
	// 再把末尾的 5 挪到最前面（2 那一行前面）→ 5,2,3,1,4
	if _, err := first.ReorderFavorite(app.FavoritesCreated, 5, 2); err != nil {
		t.Fatal(err)
	}
	if got := favIDs(first, app.FavoritesCreated); got != "5,2,3,1,4" {
		t.Fatalf("两次拖完内存里是 %s，want 5,2,3,1,4", got)
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

	if got := favIDs(second, app.FavoritesCreated); got != "5,2,3,1,4" {
		t.Errorf("重启之后顺序是 %s，want 5,2,3,1,4", got)
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

// TestRenamingAFolderKeepsItsCount 钉住**改一次名字不会把数量抹成 0**。
//
// 建 / 改收藏夹那两个接口**不返回 media_count**（`bili.FolderInfo` 没有那一格），
// 所以侧栏那一行被换回去的时候，数量是 0——而这里的 0 是「不知道」，不是
// 「真的 0 首」。直接换上去的话，用户改一次收藏夹的名字，侧栏上那个
// 「32 首」就变成了「0 首」，而那个收藏夹里明明还有 32 首。
//
// ⚠️ **新建的那一格还是 0**：它没有旧值可沿用，而它确实是空的。
func TestRenamingAFolderKeepsItsCount(t *testing.T) {
	a, _ := favMounted(t)
	a.SetFavorites(app.FavoritesCreated, []app.Favorite{{
		ID: 1, Title: "夏日午后", Mid: 42, Type: 11,
		Cover: "http://i0.hdslb.com/bfs/album/mine.jpg", MediaCount: 32,
	}})

	// 改完名字之后接口回的那一份：没有数量（0），封面有。
	a.FavoritesOf().Upsert(app.FavoritesCreated, app.Favorite{
		ID: 1, Title: "夏日午后（改过）", Mid: 42, Type: 11,
		Cover: "http://i0.hdslb.com/bfs/album/mine.jpg",
	})

	got := a.SideFavorites(app.FavoritesCreated)
	if len(got) != 1 {
		t.Fatalf("侧栏上有 %d 条，want 1", len(got))
	}
	if got[0].Title != "夏日午后（改过）" {
		t.Errorf("标题是 %q，want 改过之后那个", got[0].Title)
	}
	if got[0].MediaCount != 32 {
		t.Errorf("改完名字之后数量是 %d，want 32（建/改接口不返回 media_count，"+
			"0 在这里是「不知道」）", got[0].MediaCount)
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
// 设置文件是用户能直接编辑的，也可能从 Biu 的旧导出、手改的备份里来。
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
		{280, 280},
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

// TestAMissingSidebarWidthFallsBackTo200 而不是下限 160。
//
// 「文件里没有这一格」和「写了但越界」是两件事：Biu 的兜底是
// `sideMenuWidth ?? 200`（`biu/src/layout/side/index.tsx:49`），不是 160。
// 而 normalize 跑完就会把结果写回去——所以一旦这里给了 160，任何一份缺这一格
// 的设置文件（Biu 的旧导出、手写的）在**第一次打开**时就变成 160，Biu 那边是
// 200，两边对不上，而且再也回不去。
func TestAMissingSidebarWidthFallsBackTo200(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
	}{
		{"没有这一格", `{"schemaVersion":1,"appSettings":{"themeMode":"dark"}}`},
		{"写了 0", `{"schemaVersion":1,"appSettings":{"sideMenuWidth":0}}`},
		{"写了负数", `{"schemaVersion":1,"appSettings":{"sideMenuWidth":-5}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), app.SettingsFileName)
			write(t, path, tc.content)

			got, err := app.LoadSettings(path)
			if err != nil {
				t.Fatal(err)
			}
			if got.SideMenuWidth != app.DefaultSideMenuWidth {
				t.Errorf("兜底是 %v，want %v（Biu 的 `?? %v`）",
					got.SideMenuWidth, app.DefaultSideMenuWidth, app.DefaultSideMenuWidth)
			}
		})
	}
}
