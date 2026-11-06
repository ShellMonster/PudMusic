// 「喜欢歌单绑定」这一摊的业务层测试。
//
// **package app 而不是 app_test**：和 foldermanage_test.go 同一个理由——要钉
// 「绑定落在设置里」「失败要分得清是「夹子没了」还是「网络坏了」」这些内部
// 约定，而它们没有导出。
//
// ⚠️ fixture 是假的 B 站（folderFixture 加了一条 deal 可控失败的缝），真写
// 接口一个都不碰（AGENTS.md：测试默认用合成数据）。

package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// likedApp 装一个「挂着设置 + 指着假 B 站」的 App。
//
// 绑定是落在设置里的，所以这一组必须走 `MountSettings` 那条真路——不然
// 「绑定之后重启还在不在」这件事测不到。
func likedApp(t *testing.T, f *folderFixture) *App {
	t.Helper()
	a := folderApp(t, f)
	if err := a.MountSettings(filepath.Join(t.TempDir(), SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	return a
}

// TestLikedFolderBindingLandsInSettings 绑定之后，设置里就有那一格；
// 解绑之后就回到「没绑」。
//
// 🔴 钉的是「绑定是一个本地状态，落盘在设置里」：重启之后还要认得它，
// 所以读写只能走 `UpdateSettings` 那一条（后台合并写），不能是 App 上一个
// 内存字段——内存字段重启就没了，用户每次启动都被问一遍「绑定哪个歌单」。
func TestLikedFolderBindingLandsInSettings(t *testing.T) {
	f := newFolderFixture(t)
	a := likedApp(t, f)

	if _, ok := a.LikedFolder(); ok {
		t.Fatal("新装的应用不该有绑定")
	}

	a.BindLikedFolder(LikedFolder{ID: 42, Title: "我的喜欢", Private: true})
	got, ok := a.LikedFolder()
	if !ok {
		t.Fatal("绑定了却说没绑")
	}
	if got.ID != 42 || got.Title != "我的喜欢" || !got.Private {
		t.Errorf("绑回来的 = %+v，want {42 我的喜欢 私密}", got)
	}
	if st := a.SettingsSnapshot(); st.LikedFolder.ID != 42 {
		t.Errorf("设置里的 likedFolder.id = %d，want 42", st.LikedFolder.ID)
	}

	a.UnbindLikedFolder()
	if _, ok := a.LikedFolder(); ok {
		t.Error("解绑了还说自己绑着")
	}
}

// TestLikedFolderBindingSurvivesARestart 绑定落在盘上：换一个 App 挂同一
// 份设置文件，绑定的歌单还在。
func TestLikedFolderBindingSurvivesARestart(t *testing.T) {
	f := newFolderFixture(t)
	path := filepath.Join(t.TempDir(), SettingsFileName)

	a := folderApp(t, f)
	if err := a.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	a.BindLikedFolder(LikedFolder{ID: 42, Title: "我的喜欢"})
	if err := a.FlushSettings(); err != nil {
		t.Fatal(err)
	}
	a.Close()

	a2 := folderApp(t, f)
	if err := a2.MountSettings(path); err != nil {
		t.Fatal(err)
	}
	got, ok := a2.LikedFolder()
	if !ok || got.ID != 42 {
		t.Fatalf("重启后绑定 = %+v, %v，want id 42", got, ok)
	}
}

// TestFavoriteIntoBoundWritesToTheBoundFolder 绑定时喜欢这一首，直接写进
// 绑定的那一格，不弹选择框。
//
// 🔴 钉的是「写的那一份 add_media_ids 是**绑定的那一个**」：默认收藏夹
// 那一套（`FavoriteSong` 挑第一格）在这儿是错的——用户绑定的是「我的喜欢」，
// 歌却进了一个他没选的格子，而界面上还说是「已加入我的喜欢」。
func TestFavoriteIntoBoundWritesToTheBoundFolder(t *testing.T) {
	f := newFolderFixture(t)
	a := likedApp(t, f)
	a.SetFavorites(FavoritesCreated, []Favorite{{ID: 42, Title: "我的喜欢"}})
	a.BindLikedFolder(LikedFolder{ID: 42, Title: "我的喜欢"})

	a.FavoriteIntoBound(77, bili.MediaAudio)
	res := waitLikedResult(t, a, 2*time.Second)
	if !res.OK {
		t.Fatalf("收进绑定歌单失败了：%q", res.Text)
	}
	added, _ := f.deals()
	if len(added) != 1 || added[0] != "42" {
		t.Fatalf("add_media_ids = %v，want [42]", added)
	}
	if _, ok := a.LikedFolder(); !ok {
		t.Error("成功之后绑定不该被清掉")
	}
}

// TestFavoriteIntoBoundDetectsAGoneFolder 绑定的那一格已经没了（B 站上删
// 了，或者换过账号）时，要报「绑定失效」，**不能**只报「收藏失败」。
//
// 🔴 这一档和普通失败是两件事：普通失败（断网）重试就好，绑定失效要让
// 界面回到「选哪个收藏夹」——分不清的话，用户每次喜欢都收到一句「收藏
// 失败」，却不知道自己该去设置里解绑。
//
// 判据是两层（见 `FavoriteIntoBound` 的注释）：
//   - 侧栏那份列表**已经拉过**、里面没有绑定的那一个 id → 当场判失效；
//   - 列表还没拉过，就照写——B 站说「这一格不存在」（-400）也算失效。
func TestFavoriteIntoBoundDetectsAGoneFolder(t *testing.T) {
	f := newFolderFixture(t)
	a := likedApp(t, f)
	// 侧栏「我创建的」拉完了，里面没有 42 → 绑定的那个已经不在了。
	a.SetFavorites(FavoritesCreated, []Favorite{{ID: 1, Title: "甲"}})
	markFavoritesLoaded(a)
	a.BindLikedFolder(LikedFolder{ID: 42, Title: "我的喜欢"})

	a.FavoriteIntoBound(77, bili.MediaAudio)
	res := waitLikedResult(t, a, 2*time.Second)
	if res.OK {
		t.Fatal("绑定的是个已经不存在的歌单，不该成功")
	}
	if !res.Missing {
		t.Errorf("Missing = false，want true（绑定失效要单独说出来）")
	}
	// 列表已经明说没有它了，一个写请求都不该发。
	if added, _ := f.deals(); len(added) != 0 {
		t.Errorf("明知歌单没了还是发了 %d 次 deal", len(added))
	}
}

// TestFavoriteIntoBoundStillWritesWhenSidebarHasNotLoaded 侧栏列表还没拉
// 过时不能先判失效——「还没拉」和「真没有」是两件事（AGENTS.md §4）。照写，
// B 站说不存在才算失效。
func TestFavoriteIntoBoundStillWritesWhenSidebarHasNotLoaded(t *testing.T) {
	f := newFolderFixture(t)
	f.failDeal(-400, "收藏夹不存在")
	a := likedApp(t, f)
	a.BindLikedFolder(LikedFolder{ID: 42, Title: "我的喜欢"})

	a.FavoriteIntoBound(77, bili.MediaAudio)
	res := waitLikedResult(t, a, 2*time.Second)
	if res.OK || !res.Missing {
		t.Fatalf("结果 = %+v，want 失败且 Missing", res)
	}
	if added, _ := f.deals(); len(added) != 1 {
		t.Errorf("列表没拉过时该照写一次（发了 %d 次）", len(added))
	}
}

// TestFavoriteIntoBoundNetworkFailureKeepsTheBinding 网络坏了只是「这次没
// 收进去」，不是「绑定失效」——不能顺手把绑定解了，不然一次断网就把用户
// 配好的歌单弄丢。
func TestFavoriteIntoBoundNetworkFailureKeepsTheBinding(t *testing.T) {
	f := newFolderFixture(t)
	f.failDeal(-500, "服务器开小差了")
	a := likedApp(t, f)
	a.BindLikedFolder(LikedFolder{ID: 42, Title: "我的喜欢"})

	a.FavoriteIntoBound(77, bili.MediaAudio)
	res := waitLikedResult(t, a, 2*time.Second)
	if res.OK {
		t.Fatal("服务器回 -500，不该算成功")
	}
	if res.Missing {
		t.Error("一次网络错误不该说成「绑定失效」")
	}
}

// markFavoritesLoaded 把「我创建的」那一组标成「拉过、且拉到了」。测试用：
// 绑定的失效判据里有一条是「列表拉过了、里面没有」。
func markFavoritesLoaded(a *App) {
	fav := a.FavoritesOf()
	gen, _ := fav.tryBeginRefresh()
	defer fav.finishQuietly(gen)
	u := FavoriteGroupsUpdate{
		Items: [favoriteGroupCount][]Favorite{
			FavoritesCreated:   fav.Snapshot(FavoritesCreated),
			FavoritesCollected: fav.Snapshot(FavoritesCollected),
		},
	}
	for i := range u.States {
		u.States[i] = FavoriteState{Loaded: true}
	}
	fav.Commit(gen, u)
}

// waitLikedResult 等一条喜欢结果回来。
func waitLikedResult(t *testing.T, a *App, d time.Duration) LikedResult {
	t.Helper()
	var res LikedResult
	waitUntil(t, "喜欢那一趟回来", d, func() bool {
		var ok bool
		res, ok = a.TakeLikedResult()
		return ok
	})
	return res
}
