// 「喜欢 = 在绑定的那个收藏夹里」那份 App 级缓存的测试。
//
// 断言的是**用户能观察到的状态**：这一条在不在绑定歌单里、问了几次、换绑之后
// 答案有没有跟着清掉。不碰界面，也不碰真实 B 站（folderFixture 是假的）。
package app

import (
	"context"
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
