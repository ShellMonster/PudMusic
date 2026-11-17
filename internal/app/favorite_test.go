// 「喜欢 = 在绑定的那个收藏夹里」那份 App 级缓存的测试。
//
// 断言的是**用户能观察到的状态**：这一条在不在绑定歌单里、问了几次、换绑之后
// 答案有没有跟着清掉。不碰界面，也不碰真实 B 站（folderFixture 是假的）。
package app

import (
	"context"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// audioFavItem 造一条音乐库条目（audio 的身份是 sid）。
func audioFavItem(sid int64) Item {
	return Item{Source: "online", Type: "audio", Sid: strconv.FormatInt(sid, 10), Title: "音乐库的一首"}
}

// mvFavItem 造一条视频条目（mv 的身份优先 bvid，Sid 有就是 avid）。
func mvFavItem(bvid, sid string) Item {
	return Item{Source: "online", Type: "mv", Bvid: bvid, Sid: sid, Title: "视频的一首"}
}

// waitFavoriteKnown 等某一条的喜欢状态问出结果。
func waitFavoriteKnown(t *testing.T, a *App, it Item) bool {
	t.Helper()
	var fav bool
	waitUntil(t, "喜欢状态问回来", 2*time.Second, func() bool {
		var known bool
		fav, known = a.FavoriteInBound(it)
		return known
	})
	return fav
}

// TestFavoriteInBoundReadsTheBoundFolderFavState 走真实那一条：在「我创建的
// 收藏夹」里翻到绑定的那一格，返回它的 fav_state。
func TestFavoriteInBoundReadsTheBoundFolderFavState(t *testing.T) {
	f := newFolderFixture(t)
	f.pages = [][]map[string]any{{folderOf(42, "我的喜欢", true)}}
	a := likedApp(t, f)
	a.BindLikedFolder(LikedFolder{ID: 42, Title: "我的喜欢"})

	it := audioFavItem(77)
	if fav := waitFavoriteKnown(t, a, it); !fav {
		t.Error("绑定歌单里明明有这一条，说不在里面")
	}
}

// TestFavoriteInBoundForAudioAndMV 两类身份都读得出来。
func TestFavoriteInBoundForAudioAndMV(t *testing.T) {
	f := newFolderFixture(t)
	a := likedApp(t, f)
	a.BindLikedFolder(LikedFolder{ID: 42, Title: "我的喜欢"})
	a.favMember = func(_ context.Context, rid int64, typ int, folder int64) (bool, error) {
		if folder != 42 {
			t.Errorf("问的收藏夹 = %d，want 42", folder)
		}
		return rid == 11 || rid == 12, nil
	}

	if fav := waitFavoriteKnown(t, a, audioFavItem(11)); !fav {
		t.Error("audio 在里面却说不在")
	}
	if fav := waitFavoriteKnown(t, a, mvFavItem("BV1aa", "12")); !fav {
		t.Error("mv 在里面却说不在")
	}
}

// TestFavoriteQueryRunsOncePerItem 同一格问失败 / 还没回来时不许每帧都发，
// 回来之后直接用缓存。
func TestFavoriteQueryRunsOncePerItem(t *testing.T) {
	f := newFolderFixture(t)
	a := likedApp(t, f)
	a.BindLikedFolder(LikedFolder{ID: 42, Title: "我的喜欢"})

	var calls atomic.Int32
	release := make(chan struct{})
	a.favMember = func(context.Context, int64, int, int64) (bool, error) {
		calls.Add(1)
		<-release
		return true, nil
	}

	it := audioFavItem(77)
	for range 20 {
		if _, known := a.FavoriteInBound(it); known {
			t.Fatal("查询还没回来就说知道了")
		}
	}
	waitUntil(t, "第一趟查询发出去", 2*time.Second, func() bool { return calls.Load() == 1 })
	if got := calls.Load(); got != 1 {
		t.Fatalf("查了 %d 次，want 1（同一格只发一趟）", got)
	}

	close(release)
	if fav := waitFavoriteKnown(t, a, it); !fav {
		t.Error("回来之后说不在里面")
	}
	for range 20 {
		a.FavoriteInBound(it)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("知道答案之后又查了 %d 次", got-1)
	}
}

// TestAddingAndRemovingMarkTheCache 写成功之后缓存立刻是最终状态：
// 加一次变真，删一次变假。
func TestAddingAndRemovingMarkTheCache(t *testing.T) {
	f := newFolderFixture(t)
	a := likedApp(t, f)
	a.BindLikedFolder(LikedFolder{ID: 42, Title: "我的喜欢"})
	it := audioFavItem(77)

	a.SetBoundFavorite(it, 77, bili.MediaAudio, true)
	if res := waitLikedResult(t, a, 2*time.Second); !res.OK {
		t.Fatalf("加进绑定歌单失败：%q", res.Text)
	}
	if fav, known := a.FavoriteInBound(it); !known || !fav {
		t.Fatalf("加成功之后 = %v,%v，want true,true", fav, known)
	}

	a.SetBoundFavorite(it, 77, bili.MediaAudio, false)
	if res := waitLikedResult(t, a, 2*time.Second); !res.OK {
		t.Fatalf("从绑定歌单移除失败：%q", res.Text)
	}
	if fav, known := a.FavoriteInBound(it); !known || fav {
		t.Errorf("移除成功之后 = %v,%v，want false,true", fav, known)
	}

	added, deleted := f.deals()
	if len(added) != 2 || added[0] != "42" || added[1] != "" {
		t.Errorf("add_media_ids = %v，want 第一次 42、第二次空", added)
	}
	if len(deleted) != 2 || deleted[0] != "" || deleted[1] != "42" {
		t.Errorf("del_media_ids = %v，want 第一次空、第二次 42", deleted)
	}
}

// TestBindingChangeClearsTheFavoriteCache 换绑 / 解绑之后旧答案作废：
// 缓存清空，下一帧重新问。
func TestBindingChangeClearsTheFavoriteCache(t *testing.T) {
	f := newFolderFixture(t)
	a := likedApp(t, f)
	a.BindLikedFolder(LikedFolder{ID: 42, Title: "旧的"})

	var calls atomic.Int32
	a.favMember = func(context.Context, int64, int, int64) (bool, error) {
		calls.Add(1)
		return true, nil
	}
	it := audioFavItem(77)
	if fav := waitFavoriteKnown(t, a, it); !fav {
		t.Fatal("第一份绑定里明明有这一条")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("换了绑定之前查了 %d 次，want 1", got)
	}

	a.BindLikedFolder(LikedFolder{ID: 43, Title: "新的"})
	if _, known := a.FavoriteInBound(it); known {
		t.Fatal("换绑之后还拿旧歌单的答案")
	}
	waitUntil(t, "换绑之后重新问", 2*time.Second, func() bool { return calls.Load() == 2 })

	a.UnbindLikedFolder()
	if _, known := a.FavoriteInBound(it); !known {
		t.Error("解绑之后应该直接知道「不在任何绑定歌单里」")
	}
}

// TestFavoriteRIDUsesSidThenFallsBackToArchiveID mv 的 rid 解析顺序：
// 有 sid 就用 sid，没有才去问 bvid → avid。
func TestFavoriteRIDUsesSidThenFallsBackToArchiveID(t *testing.T) {
	f := newFolderFixture(t)
	a := likedApp(t, f)
	a.BindLikedFolder(LikedFolder{ID: 42, Title: "我的喜欢"})

	var mu sync.Mutex
	var rids []int64
	a.favMember = func(_ context.Context, rid int64, _ int, _ int64) (bool, error) {
		mu.Lock()
		rids = append(rids, rid)
		mu.Unlock()
		return false, nil
	}

	waitFavoriteKnown(t, a, mvFavItem("BV1aa", "345"))
	waitFavoriteKnown(t, a, mvFavItem("BV1bb", ""))

	mu.Lock()
	defer mu.Unlock()
	if len(rids) != 2 {
		t.Fatalf("查了 %d 次，want 2：%v", len(rids), rids)
	}
	if rids[0] != 345 {
		t.Errorf("带 sid 的 mv 查的 rid = %d，want 345（直接用 sid，不问 bvid）", rids[0])
	}
	if rids[1] != 999 {
		t.Errorf("没有 sid 的 mv 查的 rid = %d，want 999（fixture 的 view 回 aid 999）", rids[1])
	}
}

// TestFavoriteCacheDoesNotMixItems 两条内容的答案互不串。
func TestFavoriteCacheDoesNotMixItems(t *testing.T) {
	f := newFolderFixture(t)
	a := likedApp(t, f)
	a.BindLikedFolder(LikedFolder{ID: 42, Title: "我的喜欢"})
	a.favMember = func(_ context.Context, rid int64, _ int, _ int64) (bool, error) {
		return rid == 111, nil
	}

	if fav := waitFavoriteKnown(t, a, audioFavItem(111)); !fav {
		t.Fatal("第一条在里面却说不在")
	}
	if fav := waitFavoriteKnown(t, a, audioFavItem(222)); fav {
		t.Error("第二条不在里面，却拿到了第一条的答案")
	}
}

// TestAStaleQueryCannotFillTheNewEntryAfterABindingChange 钉「清缓存之后，旧
// 查询回来不许把新绑定那一份填上」。
//
// 清 map 之后同一个 key 会有一份新 entry，代次又从 1 开始；只比 key + gen 的
// 话，旧答案会被当成新答案。entry 指针不同就必须丢。
func TestAStaleQueryCannotFillTheNewEntryAfterABindingChange(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)
	it := audioFavItem(77)
	key := favoriteKey(it)

	a.BindLikedFolder(LikedFolder{ID: 42, Title: "旧的"})
	old := &favoriteEntry{gen: 1}
	a.favMu.Lock()
	a.fav = map[string]*favoriteEntry{key: old}
	a.favMu.Unlock()

	a.BindLikedFolder(LikedFolder{ID: 43, Title: "新的"})
	fresh := &favoriteEntry{gen: 1}
	a.favMu.Lock()
	a.fav = map[string]*favoriteEntry{key: fresh}
	a.favMu.Unlock()

	a.finishFavorite(key, old, 1, true, nil)

	a.favMu.Lock()
	filled := fresh.known || fresh.favorited
	a.favMu.Unlock()
	if filled {
		t.Error("换绑清缓存之后，旧查询把新绑定的那一份填上了")
	}
}

// TestAccountChangeClearsTheFavoriteCache 换账号之后旧账号的喜欢答案不许留。
func TestAccountChangeClearsTheFavoriteCache(t *testing.T) {
	f := newFolderFixture(t)
	a := likedApp(t, f)
	a.BindLikedFolder(LikedFolder{ID: 42, Title: "我的喜欢"})
	a.favMember = func(context.Context, int64, int, int64) (bool, error) { return true, nil }

	it := audioFavItem(77)
	if fav := waitFavoriteKnown(t, a, it); !fav {
		t.Fatal("第一份账号的答案没回来")
	}

	a.onAccountChanged()
	if _, known := a.FavoriteInBound(it); known {
		t.Error("换账号之后还拿上一个账号的答案")
	}
}

// TestChangingTheBoundFolderContentsClearsTheHeartCache 从「收藏」多选框把
// 一条加进 / 拿出绑定歌单之后，红心缓存必须作废重问。
func TestChangingTheBoundFolderContentsClearsTheHeartCache(t *testing.T) {
	f := newFolderFixture(t)
	a := likedApp(t, f)
	a.BindLikedFolder(LikedFolder{ID: 42, Title: "我的喜欢"})
	a.favMember = func(context.Context, int64, int, int64) (bool, error) { return true, nil }

	it := audioFavItem(77)
	if fav := waitFavoriteKnown(t, a, it); !fav {
		t.Fatal("最初的答案没回来")
	}

	// 让重新查询卡住，才看得到「缓存已作废、还没问回来」那一段。
	release := make(chan struct{})
	a.favMember = func(context.Context, int64, int, int64) (bool, error) {
		<-release
		return true, nil
	}
	a.DealFolderMedias(77, bili.MediaAudio, []int64{42}, nil)
	if res := waitFolderResult(t, a, 2*time.Second); !res.OK {
		t.Fatalf("改绑定歌单失败：%q", res.Text)
	}

	if _, known := a.FavoriteInBound(it); known {
		t.Error("绑定歌单里的内容变了，心形缓存还留着旧答案")
	}
	close(release)
}

// TestMountingCollectionsClearsTheFailedBackoff 钉「收藏夹挂上 / 登录成功之后，
// 之前失败留下的退避要作废」。
//
// 没挂上时那一问会失败并退避 15 秒；挂上之后如果不作废，心形会一直空心到
// 退避到点，而用户看到的是「明明登录了却点不亮」。
func TestMountingCollectionsClearsTheFailedBackoff(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)
	if err := a.MountSettings(filepath.Join(t.TempDir(), SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	a.BindLikedFolder(LikedFolder{ID: 42, Title: "我的喜欢"})
	it := audioFavItem(77)
	key := favoriteKey(it)

	a.FavoriteInBound(it) // 还没挂收藏夹：这一问会失败
	waitUntil(t, "那次失败落完", 2*time.Second, func() bool {
		a.favMu.Lock()
		defer a.favMu.Unlock()
		e := a.fav[key]
		return e != nil && !e.wanted
	})

	a.MountCollections(CollectionRefs{Client: bili.NewClient(bili.Options{})})
	a.favMember = func(context.Context, int64, int, int64) (bool, error) { return true, nil }
	if fav := waitFavoriteKnown(t, a, it); !fav {
		t.Error("收藏夹挂上之后没有重新问，还停在失败退避里")
	}
}

// TestAStaleEpochCannotMarkTheCache 钉「代次检查和回填在同一把锁里」：
// 清过缓存之后，旧代次的写回执不许把旧答案写回去。
func TestAStaleEpochCannotMarkTheCache(t *testing.T) {
	f := newFolderFixture(t)
	a := likedApp(t, f)
	a.BindLikedFolder(LikedFolder{ID: 42, Title: "我的喜欢"})
	it := audioFavItem(77)
	key := favoriteKey(it)

	old := a.favoriteEpoch()
	a.clearFavorites()
	if a.markFavoriteIfEpoch(it, true, old) {
		t.Error("旧代次回填成功了：清过缓存之后旧答案又被写回去")
	}
	a.favMu.Lock()
	_, exists := a.fav[key]
	a.favMu.Unlock()
	if exists {
		t.Error("旧代次回填还是建了缓存条目")
	}

	if !a.markFavoriteIfEpoch(it, true, a.favoriteEpoch()) {
		t.Error("当前代次回填失败")
	}
	if fav, known := a.FavoriteInBound(it); !known || !fav {
		t.Errorf("当前代次回填之后 = %v,%v，want true,true", fav, known)
	}
}

// TestAAccountChangeDropsALateMissing 钉「换账号之后，旧账号那条迟到的
// Missing 不许解掉新账号的绑定」。
//
// 本地绑定 id 可能没变，光比 Folder 拦不住；回执带的是发出那一趟的缓存
// 代次，换账号会让代次 +1。
func TestAAccountChangeDropsALateMissing(t *testing.T) {
	f := newFolderFixture(t)
	a := likedApp(t, f)
	a.SetFavorites(FavoritesCreated, []Favorite{{ID: 1, Title: "别的"}})
	markFavoritesLoaded(a)
	a.BindLikedFolder(LikedFolder{ID: 42, Title: "我的喜欢"})

	// 侧栏列表里没有 42：这一趟当场判 Missing，结果在邮箱里。
	a.SetBoundFavorite(audioFavItem(77), 77, bili.MediaAudio, true)
	a.onAccountChanged()
	if res, ok := a.TakeLikedResult(); ok {
		t.Errorf("换账号之后旧账号的 Missing 被交给界面了：%+v", res)
	}
}

// TestTheFavoriteCacheIsBounded 钉「浏览搜索结果时这份缓存不会无限长」：
// 超过上限先挤掉最旧的一条。
func TestTheFavoriteCacheIsBounded(t *testing.T) {
	f := newFolderFixture(t)
	a := likedApp(t, f)
	a.BindLikedFolder(LikedFolder{ID: 42, Title: "我的喜欢"})
	a.favMember = func(context.Context, int64, int, int64) (bool, error) { return false, nil }

	a.favMu.Lock()
	a.fav = map[string]*favoriteEntry{}
	for i := 0; i < favoriteCacheMax; i++ {
		a.fav["mv|BVold"+strconv.Itoa(i)] = &favoriteEntry{
			known: true, triedAt: time.Unix(int64(i), 0),
		}
	}
	a.favMu.Unlock()

	a.FavoriteInBound(mvFavItem("BVnew", ""))

	a.favMu.Lock()
	n := len(a.fav)
	_, hasNew := a.fav["mv|BVnew"]
	a.favMu.Unlock()
	if n > favoriteCacheMax {
		t.Errorf("缓存有 %d 条，超过上限 %d", n, favoriteCacheMax)
	}
	if !hasNew {
		t.Error("新条目没进缓存：挤掉的不该是刚问的这一条")
	}
}

// TestCanFavoriteMatchesTheUsableIdentities 菜单的摆法判据：在线的 mv / audio
// 可以，本地文件和用户行不行。
func TestCanFavoriteMatchesTheUsableIdentities(t *testing.T) {
	cases := []struct {
		name string
		it   Item
		want bool
	}{
		{"在线 mv（bvid）", mvFavItem("BV1xx", ""), true},
		{"在线 mv（只有 sid）", mvFavItem("", "9"), true},
		{"在线 audio（sid）", audioFavItem(7), true},
		{"本地文件", Item{Source: "local", Type: "mv", Bvid: "BV1xx", Path: "/tmp/a.mp3"}, false},
		{"用户行", Item{Source: "online", Type: "user", Bvid: "42"}, false},
		{"没有身份", Item{Source: "online", Type: "mv"}, false},
	}
	for _, tc := range cases {
		if got := CanFavorite(tc.it); got != tc.want {
			t.Errorf("%s：CanFavorite = %v，want %v", tc.name, got, tc.want)
		}
	}
}
