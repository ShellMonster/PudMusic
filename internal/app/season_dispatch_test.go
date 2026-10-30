// 合集 / 系列的内容列表走另外两个接口（票 #683，缺陷 C）。
//
// 这一份钉在 `internal/app` 那一层：`LoadCollectionPage` 按 `ref.Type` 分派，
// 而「分派错了」的症状是**界面上什么都没有**——收藏夹那个接口对合集 / 系列的
// id 只会回一个空列表或者一句 -400，两者在界面上都是「这个容器是空的」。
package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// seasonFixture 是一台只会答「合集 / 系列那两条路径」的假 B 站。
//
// ⚠️ **它故意不答 `/x/v3/fav/resource/list`**：那一条打过来说明分派错了，
// 而这一组要的正是「有没有打错那条路径」（`hits` 数得出来）。
type seasonFixture struct {
	mu   sync.Mutex
	hits map[string]int
	// forms 记下写接口每一次收到的表单（写操作分派那几条用例要看参数）。
	forms map[string]url.Values
}

func (f *seasonFixture) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[path]
}

// serveSeasonFixture 装一台只认那两条路径的假 B 站，并返回它的计数器。
func serveSeasonFixture(t *testing.T, a *App, mid int64) *seasonFixture {
	t.Helper()
	sf := &seasonFixture{hits: map[string]int{}}
	srv := newSeasonServer(t, sf)
	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(srv.URL)
	a.MountCollections(CollectionRefs{Client: cli, Settle: 0})
	if mid > 0 {
		// 元信息那一趟要能拉到（合集接口要 mid，而它从元信息里来）。
		a.collectionCachePut(CollectionInfo{
			Ref: CollectionRef{ID: 1227671, Type: bili.CollectionAlbum}, OwnerMid: mid, Loaded: true,
		})
	}
	return sf
}

// collectionCachePut 直接往元信息缓存里塞一份，省掉那一趟网络。
//
// ⚠️ **这条用例要量的是「分派」**，不是「元信息怎么拉来的」：真跑一遍
// `LoadCollection` 会多打一次 `folder/info`，而那个接口对合集是不是还能用
// 正是这一票没有结论的事。塞一份进去之后，`LoadCollectionPage` 读到的 mid
// 就是确定的。
func (a *App) collectionCachePut(info CollectionInfo) {
	if c := a.collectionsSnapshot(); c != nil {
		c.cache.put(info)
	}
}

// newSeasonServer 起一台假 B 站（合集 / 系列两条路径）。
func newSeasonServer(t *testing.T, sf *seasonFixture) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sf.mu.Lock()
		sf.hits[r.URL.Path]++
		sf.mu.Unlock()
		switch r.URL.Path {
		case "/x/polymer/web-space/seasons_archives_list":
			writeCollectionJSON(t, w, 0, "0", map[string]any{
				"archives": []map[string]any{
					{"aid": 311606079, "bvid": "BVseason1", "title": "合集里的第一集",
						"duration": 261, "pubdate": 1679651747},
				},
				"page": map[string]any{"page_num": 1, "page_size": 20, "total": 1},
			})
		case "/x/series/archives":
			writeCollectionJSON(t, w, 0, "0", map[string]any{
				"archives": []map[string]any{
					{"aid": 695029098, "bvid": "BVseries1", "title": "系列里的第一集",
						"duration": 275, "pubdate": 1677321251},
				},
				"page": map[string]any{"num": 1, "size": 20, "total": 1},
			})
		default:
			writeCollectionJSON(t, w, -400, "这个接口对合集没有意义", nil)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestLoadCollectionPageDispatchesOnTheContainerType 是本缺陷的主干：
// 三类容器各打各的接口，一个都不许打错。
func TestLoadCollectionPageDispatchesOnTheContainerType(t *testing.T) {
	for _, c := range []struct {
		name     string
		ref      CollectionRef
		wantPath string
		wantBvid string
	}{
		{"收藏夹", CollectionRef{ID: 42, Type: bili.CollectionFavorite}, "/x/v3/fav/resource/list", "BVfav"},
		{"合集", CollectionRef{ID: 1227671, Type: bili.CollectionAlbum}, "/x/polymer/web-space/seasons_archives_list", "BVseason1"},
		{"系列", CollectionRef{ID: 534501, Type: bili.CollectionSeries}, "/x/series/archives", "BVseries1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := New()
			t.Cleanup(a.Close)
			sf := serveSeasonFixture(t, a, 900)
			// 收藏夹那一条要用假服务器的 `/x/v3/fav/resource/list`，而
			// `serveSeasonFixture` 那台只认另两条——所以单独顶一份，并让它把
			// 打过的路径记进**同一个计数器**（这样下面那两条判据对三档都成立）。
			if c.ref.Type == bili.CollectionFavorite {
				a.MountCollections(CollectionRefs{Client: favOnlyClient(t, sf), Settle: 0})
			}

			medias, _, err := a.LoadCollectionPage(t.Context(), c.ref, bili.SortByFavTime, "", 1)
			if err != nil {
				t.Fatalf("拉 %s 的内容：%v", c.name, err)
			}
			if len(medias) != 1 || medias[0].Bvid != c.wantBvid {
				t.Fatalf("%s 拿到 %+v，want 一条 %s", c.name, medias, c.wantBvid)
			}
			// 🔴 **打的是不是那一条路径**：打错的话上面那条也可能过（假服务器
			// 对什么路径都回同一份），所以这一条才是判据。
			if got := sf.count(c.wantPath); got != 1 {
				t.Errorf("%s 打了 %q %d 次，want 1 次（打的是这两条之外的路径：%v）",
					c.name, c.wantPath, got, sf.hitsCopy())
			}
			if got := sf.count("/x/v3/fav/resource/list"); c.ref.Type != bili.CollectionFavorite && got != 0 {
				t.Errorf("%s 打了收藏夹的内容接口 %d 次——那个接口对它只会回空列表", c.name, got)
			}
		})
	}
}

func (f *seasonFixture) hitsCopy() map[string]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]int, len(f.hits))
	for k, v := range f.hits {
		out[k] = v
	}
	return out
}

// TestSeasonSearchSaysItIsUnsupported 「在合集里按歌名找」要如实说不行。
//
// 🔴 **不许静默忽略那个词**：界面上一片「还是全部内容」看起来像「搜索框坏了」，
// 而用户会一直换词、换词、换词。
func TestSeasonSearchSaysItIsUnsupported(t *testing.T) {
	for _, ref := range []CollectionRef{
		{ID: 1227671, Type: bili.CollectionAlbum},
		{ID: 534501, Type: bili.CollectionSeries},
	} {
		a := New()
		t.Cleanup(a.Close)
		sf := serveSeasonFixture(t, a, 900)

		_, _, err := a.LoadCollectionPage(t.Context(), ref, bili.SortByFavTime, "告白气球", 1)
		if err == nil {
			t.Fatalf("%v：在合集/系列里按歌名找报成功了", ref)
		}
		if got := sf.count("/x/polymer/web-space/seasons_archives_list") + sf.count("/x/series/archives"); got != 0 {
			t.Errorf("%v：说「不支持」却还是发了 %d 个请求", ref, got)
		}
	}
}

// TestSeasonPagesCarryTheMidFromTheHeader 合集 / 系列那一趟要带上创建者的 mid。
//
// 文档把 mid 写成「必要」，而 `CollectionRef` 只有 ID 和 Type——mid 只能从
// 元信息缓存里拿（`CollectionInfo.OwnerMid`）。带不上（=0）的时候 B 站回什么
// 没人验过，所以这一条钉住「带上了」。
func TestSeasonPagesCarryTheMidFromTheHeader(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)
	var gotMid string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("season_id") != "" {
			gotMid = r.URL.Query().Get("mid")
		}
		writeCollectionJSON(t, w, 0, "0", map[string]any{
			"archives": []map[string]any{},
			"page":     map[string]any{"page_num": 1, "page_size": 20, "total": 0},
		})
	}))
	t.Cleanup(srv.Close)
	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(srv.URL)
	a.MountCollections(CollectionRefs{Client: cli, Settle: 0})
	ref := CollectionRef{ID: 1227671, Type: bili.CollectionAlbum}
	a.collectionCachePut(CollectionInfo{Ref: ref, OwnerMid: 900, Loaded: true})

	if _, _, err := a.LoadCollectionPage(context.Background(), ref, bili.SortByFavTime, "", 1); err != nil {
		t.Fatal(err)
	}
	if gotMid != "900" {
		t.Errorf("合集请求带的 mid 是 %q，want 900（创建者的）", gotMid)
	}
}

// 给「收藏夹那一条子用例」用：一台只答收藏夹内容接口的客户端。
//
// ⚠️ **打过的路径记进 `sf`**：三档容器共用同一个计数器，下面那两条判据才能
// 写成同一个形状（「该打的打了」「不该打的没打」）。
func favOnlyClient(t *testing.T, sf *seasonFixture) *bili.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sf.mu.Lock()
		sf.hits[r.URL.Path]++
		sf.mu.Unlock()
		if r.URL.Path != "/x/v3/fav/resource/list" {
			writeCollectionJSON(t, w, -400, "只认收藏夹那一条", nil)
			return
		}
		writeCollectionJSON(t, w, 0, "0", map[string]any{
			"has_more": false,
			"medias": []map[string]any{
				{"id": 1, "type": 2, "bvid": "BVfav", "title": "收藏夹里的一首",
					"duration": 185, "attr": 0, "pubtime": 1670000000},
			},
		})
	}))
	t.Cleanup(srv.Close)
	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(srv.URL)
	return cli
}

// TestSeasonPagesCarryTheMidEvenBeforeTheHeaderLands 钉住「元信息还没到时内容那一趟
// 带的也是真的 mid」。
//
// 页面第一帧就会去拉内容，而元信息是**另一趟**后台请求——两趟是并发的。元信息
// 没到时 `Collection(ref).OwnerMid` 是 0，直接发出去的话合集接口拿到的是 mid=0
// （文档说也行，但那是旁注、没验证过）。
//
// 🔴 **今天的退路是「问一次合集自己的 meta」**（票 #683 的复审）：`SeasonInfo`
// 就是内容接口的 `page_size=1`，它的 `meta.mid` 就是我们要的那一个。原来那条
// 退路打的是 `folder/info`——对合集必失败，于是这一问每次都白打。
func TestSeasonPagesCarryTheMidEvenBeforeTheHeaderLands(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)
	var contentMids []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/v3/fav/folder/info":
			// 🔴 合集不该走到这里。回一份「成功」会把错遮住，回错才是真的。
			writeCollectionJSON(t, w, -400, "这个接口对合集没有意义", nil)
		case "/x/polymer/web-space/seasons_archives_list":
			if r.URL.Query().Get("page_size") != "1" {
				contentMids = append(contentMids, r.URL.Query().Get("mid"))
			}
			writeCollectionJSON(t, w, 0, "0", map[string]any{
				"archives": []map[string]any{},
				"meta": map[string]any{
					"season_id": 1227671, "mid": 900, "name": "一个合集", "total": 0,
				},
				"page": map[string]any{"page_num": 1, "page_size": 20, "total": 0},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(srv.URL)
	a.MountCollections(CollectionRefs{Client: cli, Settle: 0})

	// ⚠️ **缓存里没有元信息**（模拟「内容那一趟先到」）。
	if _, _, err := a.LoadCollectionPage(context.Background(),
		CollectionRef{ID: 1227671, Type: bili.CollectionAlbum}, bili.SortByFavTime, "", 1); err != nil {
		t.Fatal(err)
	}
	if len(contentMids) == 0 {
		t.Fatal("内容那一趟一次都没发出去")
	}
	if got := contentMids[0]; got != "900" {
		t.Errorf("内容那一趟带的 mid 是 %q，want 900（元信息没到时要先从合集自己的 meta 问出来）", got)
	}
}

// ── 头图区那一趟也按类型分派 ────────────────────────────────────────────────

// TestTheSeasonHeaderComesFromTheMatchingEndpoint 是复审第 4 条的主线。
//
// 头图区那一趟原来无条件打 `/x/v3/fav/folder/info`（只认收藏夹 mlid），于是
// 每开一次合集 / 系列页：请求注定失败、`LoadCollection` 弹一句「打不开这个收藏夹」、
// 头图区永远停在占位。这一条钉的是「打的是这一类自己的接口」＋「一次
// `folder/info` 都不打」。
func TestTheSeasonHeaderComesFromTheMatchingEndpoint(t *testing.T) {
	for _, c := range []struct {
		name     string
		ref      CollectionRef
		wantPath string
	}{
		{"合集", CollectionRef{ID: 1227671, Type: bili.CollectionAlbum}, "/x/polymer/web-space/seasons_archives_list"},
		{"系列", CollectionRef{ID: 534501, Type: bili.CollectionSeries}, "/x/series/series"},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := New()
			t.Cleanup(a.Close)
			sf := &seasonFixture{hits: map[string]int{}}
			srv := newSeasonHeaderServer(t, sf)
			cli := bili.NewClient(bili.Options{})
			cli.TestOnlyPointHostsAtFixture(srv.URL)
			a.MountCollections(CollectionRefs{Client: cli, Settle: 0})

			a.LoadCollection(c.ref)
			waitUntil(t, "头图区元信息进缓存", 2*time.Second, func() bool {
				return a.Collection(c.ref).Loaded
			})

			info := a.Collection(c.ref)
			if info.Title != "一个"+c.name {
				t.Errorf("头图区的标题是 %q，want %q", info.Title, "一个"+c.name)
			}
			if info.Cover == "" {
				t.Error("头图区的封面是空的")
			}
			if info.MediaCount != 7 {
				t.Errorf("头图区的条数是 %d，want 7", info.MediaCount)
			}
			if info.OwnerMid != 900 {
				t.Errorf("头图区的 UP mid 是 %d，want 900", info.OwnerMid)
			}
			// 🔴 一次收藏夹那个接口都不能打（它对合集 / 系列的 id 只会回错）。
			if got := sf.count("/x/v3/fav/folder/info"); got != 0 {
				t.Errorf("%s 的头图区打了 %d 次 folder/info——那个接口对它只会回错，界面于是永远停占位",
					c.name, got)
			}
			if got := sf.count(c.wantPath); got == 0 {
				t.Errorf("%s 的头图区没打 %q（打过的是 %v）", c.name, c.wantPath, sf.hitsCopy())
			}
			// 也不该弹「打不开这个收藏夹」那句红字。
			if n, ok := a.TakeCollectionNotice(c.ref); ok {
				t.Errorf("%s 的头图区弹了一句 %q——它明明拉到了", c.name, n.Text)
			}
		})
	}
}

// newSeasonHeaderServer 是一台会答「头图区那两条路径」的假 B 站。
//
// ⚠️ **`folder/info` 故意回 -400**：真的 B 站对合集 / 系列的 id 就是回错，
// 而这里要是回一份正常的元信息的话，「打错接口」那一半会被假数据遮住。
func newSeasonHeaderServer(t *testing.T, sf *seasonFixture) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sf.mu.Lock()
		sf.hits[r.URL.Path]++
		sf.mu.Unlock()
		switch r.URL.Path {
		case "/x/polymer/web-space/seasons_archives_list":
			writeCollectionJSON(t, w, 0, "0", map[string]any{
				"archives": []map[string]any{},
				"meta": map[string]any{
					"season_id": 1227671, "mid": 900, "name": "一个合集",
					"cover":       "https://i0.hdslb.com/bfs/new_dyn/season.jpg",
					"description": "合集简介", "total": 7,
				},
				"page": map[string]any{"page_num": 1, "page_size": 20, "total": 7},
			})
		case "/x/series/series":
			writeCollectionJSON(t, w, 0, "0", map[string]any{
				"meta": map[string]any{
					"series_id": 534501, "mid": 900, "name": "一个系列", "creator": "UP 小明",
					"cover":       "https://i0.hdslb.com/bfs/new_dyn/series.jpg",
					"description": "系列简介", "total": 7,
				},
				"recent_aids": []int{},
			})
		default:
			writeCollectionJSON(t, w, -400, "这个接口对合集没有意义", nil)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestTheSeasonPageRecordsTheMidFromTheContentPass 钉住复审第 4 条的「退路」那一半：
// 内容那一趟的 `meta.mid` 要记进缓存，于是后面的翻页不再靠「再问一次元信息」。
//
// 原来 `collectionOwnerMid` 在缓存里没有 mid 时当场问一次元信息，而那一趟打的是
// `folder/info`（对合集必失败）——**每翻一页重打一次**。合集内容接口的 `meta`
// 里就带着 mid，第一次拉内容时顺手记下来，后面的页就不用再问。
func TestTheSeasonPageRecordsTheMidFromTheContentPass(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)
	sf := &seasonFixture{hits: map[string]int{}}
	srv := newSeasonHeaderServer(t, sf)
	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(srv.URL)
	a.MountCollections(CollectionRefs{Client: cli, Settle: 0})

	ref := CollectionRef{ID: 1227671, Type: bili.CollectionAlbum}
	// ⚠️ **缓存里没有元信息**（内容那一趟先跑）。
	if _, _, err := a.LoadCollectionPage(context.Background(), ref, bili.SortByFavTime, "", 1); err != nil {
		t.Fatal(err)
	}
	if got := a.Collection(ref).OwnerMid; got != 900 {
		t.Errorf("内容那一趟回来之后缓存里的 mid 是 %d，want 900（meta.mid 要记下来）", got)
	}

	// 第二页：不应该再打一次元信息那几个接口。
	before := sf.hitsCopy()
	if _, _, err := a.LoadCollectionPage(context.Background(), ref, bili.SortByFavTime, "", 2); err != nil {
		t.Fatal(err)
	}
	after := sf.hitsCopy()
	if got := after["/x/v3/fav/folder/info"] - before["/x/v3/fav/folder/info"]; got != 0 {
		t.Errorf("翻第二页时打了 %d 次 folder/info，want 0", got)
	}
	// 封面 / 标题也是 meta 给的，不靠别的接口。
	if info := a.Collection(ref); info.Title != "一个合集" || info.Cover == "" {
		t.Errorf("内容那一趟记下来的元信息不完整：%+v", info)
	}
}

// ── 写操作也按类型分派（复审第 5 条） ───────────────────────────────────────

// TestFavoritingASeasonFromTheAppHitsTheSeasonEndpoint 是复审第 5 条的整条链：
// 合集页点星标打的是 `fav/season/fav` 并带 `season_id`，`folder/fav` 一次都不打。
func TestFavoritingASeasonFromTheAppHitsTheSeasonEndpoint(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)
	sf := &seasonFixture{hits: map[string]int{}}
	srv := newSeasonWriteServer(t, sf)
	cli := bili.NewClient(bili.Options{CSRF: func() string { return "jct" }})
	cli.TestOnlyPointHostsAtFixture(srv.URL)
	a.MountCollections(CollectionRefs{Client: cli, Settle: 0})

	ref := CollectionRef{ID: 1227671, Type: bili.CollectionAlbum}
	a.SetFavorite(ref, true)
	waitUntil(t, "收藏那一趟打完", 2*time.Second, func() bool {
		return sf.count("/x/v3/fav/season/fav") == 1
	})
	if got := sf.count("/x/v3/fav/folder/fav"); got != 0 {
		t.Errorf("合集页的星标打了 %d 次收藏夹那个接口——那一路会把 season_id 当 media_id 发出去", got)
	}
	if got := sf.lastForm("/x/v3/fav/season/fav").Get("season_id"); got != "1227671" {
		t.Errorf("season_id = %q，want 1227671", got)
	}
}

// TestRemovingMediasFromASeasonIsRefusedWithoutARequest 是复审第 5 条的另一半：
// 合集页的批量删除**直接拒绝、一个请求都不发**。
func TestRemovingMediasFromASeasonIsRefusedWithoutARequest(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)
	sf := &seasonFixture{hits: map[string]int{}}
	srv := newSeasonWriteServer(t, sf)
	cli := bili.NewClient(bili.Options{CSRF: func() string { return "jct" }})
	cli.TestOnlyPointHostsAtFixture(srv.URL)
	a.MountCollections(CollectionRefs{Client: cli, Settle: 0})

	err := a.RemoveMedias(t.Context(),
		CollectionRef{ID: 1227671, Type: bili.CollectionAlbum},
		[]bili.Media{{ID: 11, Type: bili.MediaVideo}})
	if err == nil {
		t.Error("合集里删内容报成功了")
	}
	if got := sf.count("/x/v3/fav/resource/batch-del"); got != 0 {
		t.Errorf("发了 %d 次批量删除，want 0", got)
	}
}

// TestClearingInvalidOnASeasonIsRefusedWithoutARequest 同理。
func TestClearingInvalidOnASeasonIsRefusedWithoutARequest(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)
	sf := &seasonFixture{hits: map[string]int{}}
	srv := newSeasonWriteServer(t, sf)
	cli := bili.NewClient(bili.Options{CSRF: func() string { return "jct" }})
	cli.TestOnlyPointHostsAtFixture(srv.URL)
	a.MountCollections(CollectionRefs{Client: cli, Settle: 0})

	ref := CollectionRef{ID: 1227671, Type: bili.CollectionAlbum}
	a.ClearInvalidMedias(ref)
	// 等一句说法（拒绝也要说一声，不然按下去没反应）。
	n, ok := waitNotice(t, a, ref)
	if !ok {
		t.Fatal("拒绝时一句话都没说")
	}
	if !strings.Contains(n.Text, "清除失效内容") {
		t.Errorf("说的那句是 %q，want 一句说清没有这个动作的话", n.Text)
	}
	if got := sf.count("/x/v3/fav/resource/clean"); got != 0 {
		t.Errorf("发了 %d 次清除失效，want 0", got)
	}
}

// newSeasonWriteServer 是一台会答写接口的假 B 站，并记下每一次的表单。
type seasonWriteFixture struct {
	*seasonFixture
	forms map[string]url.Values
}

func newSeasonWriteServer(t *testing.T, sf *seasonFixture) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sf.mu.Lock()
		sf.hits[r.URL.Path]++
		if sf.forms == nil {
			sf.forms = map[string]url.Values{}
		}
		_ = r.ParseForm()
		sf.forms[r.URL.Path] = r.PostForm
		sf.mu.Unlock()
		writeCollectionJSON(t, w, 0, "0", nil)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// lastForm 报某条路径最后一次收到的表单。
func (f *seasonFixture) lastForm(path string) url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.forms[path]
}
