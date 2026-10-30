package app

// 收藏夹那一枚封面的兜底：**接口给的封面用不了时，改用夹里第一首还有效、
// 还有封面的内容的封面**。
//
// # 它为什么在业务层
//
// 那一趟要拉容器的第一页（`LoadCollectionPage`），而「哪些条目算数」是业务
// 判据（`Playable` + `Attr == 0` + `Cover != ""`）。界面层只该拿到一个地址，
// 不该自己判断哪一首算数。
//
// # 🔴 这一组最要紧的是「缓存键是容器，不是 id」
//
// 同一个 id 在**收藏夹（11）/ 合集（21）/ 系列（31）**下是三个不同的容器
// （`CollectionRef.Key` 就是 `"<id>:<type>"`）。只按 id 记的话，用户先打开
// 收藏夹 A、再打开 id 相同的合集，第二边拿到的会是**第一边的封面**——而症状
// 是「合集头图显示的是收藏夹里那首歌的封面」，离真因隔着一整套缓存。
//
// ⚠️ 全部走内存里的假 B 站（`newCollectionFixture`），一条真接口都不打。

import (
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// fallbackMedias 是「第一首失效、第二首有封面、第三首也有封面」那一页。
//
// ⚠️ **失效那一首的封面也给上**：真实返回里失效条目常常还带着一个已经 404 的
// 地址——兜底要按 `Attr` 判，不能因为「它有个地址」就当它可用。
func fallbackMedias() []bili.Media {
	return []bili.Media{
		{ID: 1, Type: bili.MediaVideo, Title: "失效的那首", Attr: 1,
			Cover: "https://i0.hdslb.com/bfs/dead.jpg"},
		{ID: 2, Type: bili.MediaVideo, Title: "第二首", Attr: 0,
			Cover: "https://i0.hdslb.com/bfs/second.jpg"},
		{ID: 3, Type: bili.MediaVideo, Title: "第三首", Attr: 0,
			Cover: "https://i0.hdslb.com/bfs/third.jpg"},
	}
}

// TestFolderFallbackCoverUsesTheFirstPlayableCover 第一首失效就跳过它，
// 用第二首那张；而且同一个容器**只拉一次**第一页。
func TestFolderFallbackCoverUsesTheFirstPlayableCover(t *testing.T) {
	f := newCollectionFixture(t)
	f.medias = fallbackMedias()
	a := New()
	t.Cleanup(a.Close)
	withFixture(t, a, f, 0)

	ref := CollectionRef{ID: 42, Type: bili.CollectionFavorite}
	if got := a.FolderFallbackCover(ref); got != "" {
		t.Fatalf("第一趟就返回了 %q——兜底是后台拉的，第一趟只能是空串", got)
	}

	var got string
	waitUntil(t, "兜底封面拉回来", 2*time.Second, func() bool {
		got = a.FolderFallbackCover(ref)
		return got != ""
	})
	if want := "https://i0.hdslb.com/bfs/second.jpg"; got != want {
		t.Fatalf("兜底用的是 %q，want %q（跳过失效那一首，取第二首）", got, want)
	}

	// 同一个容器再问二十遍：一顿都不该多发。
	for range 20 {
		if again := a.FolderFallbackCover(ref); again != got {
			t.Fatalf("再问一次拿到 %q，want %q", again, got)
		}
	}
	if n := f.listHits(); n != 1 {
		t.Fatalf("同一个容器拉了 %d 次第一页，want 1（每夹只拉一次）", n)
	}
}

// TestFolderFallbackCoverStaysEmptyWhenNothingIsPlayable 一首能用的都没有时
// 返回空串（界面画默认封面），而且**记下这个空结果**——不然每一帧都会重拉。
func TestFolderFallbackCoverStaysEmptyWhenNothingIsPlayable(t *testing.T) {
	f := newCollectionFixture(t)
	f.medias = []bili.Media{
		{ID: 1, Type: bili.MediaVideo, Title: "失效一", Attr: 1},
		{ID: 2, Type: bili.MediaVideo, Title: "失效二", Attr: 9},
		// 有效但没有封面：它同样算不上「有封面可用」。
		{ID: 3, Type: bili.MediaVideo, Title: "没封面的", Attr: 0},
	}
	a := New()
	t.Cleanup(a.Close)
	withFixture(t, a, f, 0)

	ref := CollectionRef{ID: 42, Type: bili.CollectionFavorite}
	a.FolderFallbackCover(ref)
	waitUntil(t, "第一页拉完（空结果记下了）", 2*time.Second, func() bool {
		a.folderCovers.mu.Lock()
		defer a.folderCovers.mu.Unlock()
		_, done := a.folderCovers.done[ref.Key()]
		return done && !a.folderCovers.busy[ref.Key()]
	})

	if got := a.FolderFallbackCover(ref); got != "" {
		t.Fatalf("一个能用的封面都没有，却返回了 %q", got)
	}
	if n := f.listHits(); n != 1 {
		t.Fatalf("空结果没记下：拉了 %d 次第一页，want 1", n)
	}
}

// TestFolderFallbackCoverKeepsEachContainerApart 同一个 id 的两个容器
// **各拉各的**——缓存键是 `"<id>:<type>"`，不是 id。
//
// 🔴 **这一条钉的就是那个串档**：只按 id 记的话，第二个容器会被第一个容器的
// `busy` / `done` 挡住，一次请求都不发（而它拿到的还是第一个容器的封面）。
func TestFolderFallbackCoverKeepsEachContainerApart(t *testing.T) {
	f := newCollectionFixture(t)
	f.medias = fallbackMedias()
	a := New()
	t.Cleanup(a.Close)
	withFixture(t, a, f, 0)

	favorite := CollectionRef{ID: 42, Type: bili.CollectionFavorite}
	album := CollectionRef{ID: 42, Type: bili.CollectionAlbum}

	// 两个容器各问一次（都在后台拉）。
	a.FolderFallbackCover(favorite)
	a.FolderFallbackCover(album)

	waitUntil(t, "两个容器都拉完", 2*time.Second, func() bool {
		a.folderCovers.mu.Lock()
		defer a.folderCovers.mu.Unlock()
		_, favDone := a.folderCovers.done[favorite.Key()]
		_, albumDone := a.folderCovers.done[album.Key()]
		return favDone && albumDone &&
			!a.folderCovers.busy[favorite.Key()] && !a.folderCovers.busy[album.Key()]
	})

	if n := f.listHits(); n != 2 {
		t.Fatalf("同一个 id 的两个容器一共拉了 %d 次第一页，want 2——"+
			"缓存键只按 id 记会让两个容器串成一份", n)
	}
	for _, ref := range []CollectionRef{favorite, album} {
		if got := a.FolderFallbackCover(ref); got == "" {
			t.Errorf("%v 那一份兜底是空的——它的第一页里有可用的封面", ref)
		}
	}
}

// TestFolderFallbackCoverIgnoresAnEmptyRef 空 ref（id <= 0）直接回空串，
// 而且**不发任何请求**——那一条路径是「页面还没读到 id」时的那一帧。
func TestFolderFallbackCoverIgnoresAnEmptyRef(t *testing.T) {
	f := newCollectionFixture(t)
	f.medias = fallbackMedias()
	a := New()
	t.Cleanup(a.Close)
	withFixture(t, a, f, 0)

	for _, ref := range []CollectionRef{{}, {ID: 0, Type: bili.CollectionAlbum}, {ID: -3}} {
		if got := a.FolderFallbackCover(ref); got != "" {
			t.Errorf("%v 是空 ref，却返回了 %q", ref, got)
		}
	}
	if n := f.listHits(); n != 0 {
		t.Fatalf("空 ref 发了 %d 次请求，want 0", n)
	}
}

// TestCollectionRefOfTreatsAnUnknownTypeAsAFavorite 侧栏那一行翻成容器引用时，
// **认不出的类型按收藏夹处理**（和 URL 上的 `type` 同一个口径）。
//
// 🔴 **这一条钉的是那个换算只有一份**：界面层如果自己拼
// `CollectionRef{ID: f.ID, Type: CollectionType(f.Type)}`，它会漏掉这条兜底，
// 于是同一个收藏夹在侧栏和页面里被当成两个容器（两个缓存键、两趟请求）。
func TestCollectionRefOfTreatsAnUnknownTypeAsAFavorite(t *testing.T) {
	cases := []struct {
		name string
		in   Favorite
		want CollectionRef
	}{
		{"收藏夹", Favorite{ID: 7, Type: int(bili.CollectionFavorite)},
			CollectionRef{ID: 7, Type: bili.CollectionFavorite}},
		{"合集", Favorite{ID: 7, Type: int(bili.CollectionAlbum)},
			CollectionRef{ID: 7, Type: bili.CollectionAlbum}},
		{"系列", Favorite{ID: 7, Type: int(bili.CollectionSeries)},
			CollectionRef{ID: 7, Type: bili.CollectionSeries}},
		{"零值（没填类型）", Favorite{ID: 7},
			CollectionRef{ID: 7, Type: bili.CollectionFavorite}},
		{"认不出的类型", Favorite{ID: 7, Type: 99},
			CollectionRef{ID: 7, Type: bili.CollectionFavorite}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CollectionRefOf(tc.in); got != tc.want {
				t.Fatalf("CollectionRefOf(%+v) = %v，want %v", tc.in, got, tc.want)
			}
		})
	}
}

// listHits 数内容列表那一页被拉了几回。
func (f *collectionFixture) listHits() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, p := range f.paths {
		if p == "/x/v3/fav/resource/list" {
			n++
		}
	}
	return n
}
