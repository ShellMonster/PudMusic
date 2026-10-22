// 收藏夹 / 合集 / 系列这一组的测试。
//
// ⚠️ 和 client_test.go 顶上那句话一样：这里**没有一个断言在证明「B 站真的
// 这么返回」**。它证明的是协议形状（我们发的参数对不对、我们读的字段对不对）
// 以及**失败形态下**的行为对不对。真接口的兼容性只能靠真机回归。

package bili

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ── 元信息 ─────────────────────────────────────────────────────────────────

// TestCollectionInfoReadsTheFieldsTheHeaderNeeds 盯住头图区要用的那几个字段。
//
// 特别盯 attr：**它是一个位不是枚举**。当枚举读的话「私密」和「默认收藏夹」
// 会整个读反，而默认收藏夹是绝大多数人的第一个——那一下界面上写着
// 「私密收藏夹」，说的是别人的收藏夹。
func TestCollectionInfoReadsTheFieldsTheHeaderNeeds(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathFolderInfo, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("media_id"); got != "12345" {
			t.Errorf("media_id = %q，want 12345", got)
		}
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"id": 12345, "attr": 2, // 2 = 二进制 10：公开、非默认
			"title": "通勤路上", "cover": "https://i0.hdslb.com/cover.jpg",
			"intro": "早上听", "media_count": 42, "fav_state": 1,
			// ⚠️ **头像是协议相对地址**（B 站真机就是这样给的），所以解析出来
			// 的一定是补过协议的（见下面那条断言）。
			"upper": map[string]any{"mid": 900, "name": "UP 小明", "face": "//i0.hdslb.com/bfs/face/x.jpg"},
		}))
	})

	got, err := f.client(t, Options{}).CollectionInfo(context.Background(), 12345, CollectionAlbum)
	if err != nil {
		t.Fatal(err)
	}
	want := CollectionInfo{
		ID: 12345, Title: "通勤路上", Intro: "早上听",
		Cover: "https://i0.hdslb.com/cover.jpg", Type: CollectionAlbum,
		MediaCount: 42, Owner: CollectionOwner{Mid: 900, Name: "UP 小明",
			Face: "https://i0.hdslb.com/bfs/face/x.jpg"},
		Private: false, Default: false, Faved: true,
	}
	if *got != want {
		t.Errorf("CollectionInfo =\n %+v\nwant\n %+v", *got, want)
	}
}

// TestAttrIsBitsNotFlags 单独把 attr 的两种读法各钉一遍。
func TestAttrIsBitsNotFlags(t *testing.T) {
	cases := []struct {
		attr                        int
		private, defaultFolder, fav bool
	}{
		{attr: 0, private: false, defaultFolder: true, fav: false},
		{attr: 1, private: true, defaultFolder: true, fav: false},
		{attr: 2, private: false, defaultFolder: false, fav: false},
		{attr: 3, private: true, defaultFolder: false, fav: true},
	}
	for _, c := range cases {
		f := newFakeBili(t)
		f.on(pathFolderInfo, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, jsonData(t, 0, "0", map[string]any{
				"id": 1, "attr": c.attr, "title": "t",
			}))
		})
		got, err := f.client(t, Options{}).CollectionInfo(context.Background(), 1, CollectionFavorite)
		if err != nil {
			t.Fatal(err)
		}
		if got.Private != c.private || got.Default != c.defaultFolder {
			t.Errorf("attr=%d → 私密 %v / 默认 %v，want %v / %v",
				c.attr, got.Private, got.Default, c.private, c.defaultFolder)
		}
	}
}

// TestCollectionInfoReusesTheCallersType 接口回的 type 不作数，以调用方说的为准。
func TestCollectionInfoReusesTheCallersType(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathFolderInfo, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"id": 1, "title": "t", "type": 11}))
	})
	got, err := f.client(t, Options{}).CollectionInfo(context.Background(), 1, CollectionSeries)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != CollectionSeries {
		t.Errorf("Type = %v，want 系列（以调用方说的为准，接口回的是收藏夹）", got.Type)
	}
}

// TestCollectionInfoSurfacesTheNotLoggedInError 没登录要说没登录，不是网络错。
func TestCollectionInfoSurfacesTheNotLoggedInError(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathFolderInfo, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, -101, "账号未登录", map[string]any{"isLogin": false}))
	})
	_, err := f.client(t, Options{}).CollectionInfo(context.Background(), 1, CollectionFavorite)
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("err = %v，want ErrNotLoggedIn", err)
	}
}

// TestCollectionInfoRejectsABadID 参数给错的时候要当场说，别发请求。
func TestCollectionInfoRejectsABadID(t *testing.T) {
	f := newFakeBili(t)
	c := f.client(t, Options{})
	if _, err := c.CollectionInfo(context.Background(), 0, CollectionFavorite); err == nil {
		t.Error("id=0 也放过去了")
	}
	if n := len(f.requests()); n != 0 {
		t.Errorf("发出去 %d 个请求，want 0", n)
	}
}

// ── 内容列表 ───────────────────────────────────────────────────────────────

// mediasFixture 造 n 条内容。
func mediasFixture(n int) []map[string]any {
	out := make([]map[string]any, 0, n)
	for i := range n {
		out = append(out, map[string]any{
			"id": 1000 + i, "type": 2, "title": "歌 " + strconv.Itoa(i+1),
			"cover": "c.jpg", "duration": 185, "attr": 0, "page": 1,
			"pubtime": 1700000000,
			"bvid":    "BV1xx411c7mD",
			// ⚠️ `mid` 要在：B 站逐条给了 `upper.mid`，而播放条上那一行 UP 名
			// 要靠它拼 `/user/{id}`。少解析它的话从收藏夹放的歌在播放条上**点
			// 不动**——而名字还在，看上去像好的。
			"upper": map[string]any{"name": "UP " + strconv.Itoa(i), "mid": 900 + i},
		})
	}
	return out
}

// TestCollectionMediasSendsTheSortAsTheOrderParam 三种排序都要真的发出去。
//
// ⚠️ 排序是**接口**排的（Biu 的收藏夹那三个 order 值）。在本地重排的话，
// 分页只对一种排序有效——翻到第二页会拿到「按另一种顺序的前 20 条」。
func TestCollectionMediasSendsTheSortAsTheOrderParam(t *testing.T) {
	for _, sort := range CollectionSorts {
		f := newFakeBili(t)
		f.on(pathResourceList, func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if got, want := q.Get("order"), sort.order(); got != want {
				t.Errorf("order = %q，want %q", got, want)
			}
			if got, want := q.Get("pn"), "1"; got != want {
				t.Errorf("pn = %q，want %q", got, want)
			}
			// ps 被夹在 20：给多了 B 站默默截断，翻页从第二页起会漏一批歌，
			// 而界面上看不出任何异样。
			if got, want := q.Get("ps"), "20"; got != want {
				t.Errorf("ps = %q，want %q（上限 20）", got, want)
			}
			if got := q.Get("tid"); got != "0" {
				t.Errorf("tid = %q，want 0（省了它 B 站会按自己的记忆补一个值）", got)
			}
			if got := q.Get("platform"); got != platformWeb {
				t.Errorf("platform = %q，want %q", got, platformWeb)
			}
			writeJSON(w, jsonData(t, 0, "0", map[string]any{
				"medias": mediasFixture(2), "has_more": true,
			}))
		})

		pg, err := f.client(t, Options{}).CollectionMedias(context.Background(), 7, sort, "", 1, 100)
		if err != nil {
			t.Fatal(err)
		}
		if !pg.HasMore || len(pg.Medias) != 2 {
			t.Fatalf("%v: 一页 = %d 条 hasMore=%v", sort.Label(), len(pg.Medias), pg.HasMore)
		}
		if got, want := pg.Medias[0].Duration, 185*time.Second; got != want {
			t.Errorf("Duration = %v，want %v（B 站给的是秒，当毫秒解就成 0.185 秒）", got, want)
		}
		if got := pg.Medias[0].Owner; got != "UP 0" {
			t.Errorf("Owner = %q", got)
		}
	}
}

// TestCollectionMediasCarriesThePlayCountAndTheTimes 钉住收藏夹那一路的三个数。
//
// 字段名照上游 `service/fav-resource.ts` 的 `FavMedia`：
//
//	播放量	`cnt_info.play`（`FavMediaCntInfo { collect, play, danmaku }`）
//	收藏时间	`fav_time`（收藏夹那一页画的就是它，表头写「收藏时间」）
//	发布时间	`pubtime`（合集 / 系列那一页画的）
//
// ⚠️ **没有这个数的那一条必须是零值**：音轨那一条本来就不带 `cnt_info`，
// 编一个 0 出来会让用户在音轨上读到「0 播放」。
func TestCollectionMediasCarriesThePlayCountAndTheTimes(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathResourceList, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"medias": []any{
				map[string]any{
					"id": 1, "type": MediaVideo, "title": "有播放量的", "duration": 185,
					"bvid":     "BV1play",
					"cnt_info": map[string]any{"collect": 3, "play": 123_400, "danmaku": 7},
					"fav_time": 1750000000,
					"pubtime":  1700000000,
				},
				map[string]any{
					// 音轨：没有 cnt_info，也没有那两个时间。
					"id": 2, "type": MediaAudio, "title": "音轨",
				},
			},
			"has_more": false,
		}))
	})

	pg, err := f.client(t, Options{}).CollectionMedias(context.Background(), 7, SortByFavTime, "", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(pg.Medias) != 2 {
		t.Fatalf("读出 %d 条，want 2", len(pg.Medias))
	}
	got := pg.Medias[0]
	if got.View != 123_400 {
		t.Errorf("播放量 %d，want 123400（上游在 cnt_info.play 里）", got.View)
	}
	if got, want := got.FavTime.Unix(), int64(1750000000); got != want {
		t.Errorf("收藏时间 %d，want %d（fav_time）", got, want)
	}
	if got, want := got.PubTime.Unix(), int64(1700000000); got != want {
		t.Errorf("发布时间 %d，want %d（pubtime）", got, want)
	}

	// 音轨那一条：三个数都是零值——**没给就是没给**。
	audio := pg.Medias[1]
	if audio.View != 0 || !audio.FavTime.IsZero() || !audio.PubTime.IsZero() {
		t.Errorf("没有那几个字段的音轨读成了 播放量=%d 收藏=%v 发布=%v，want 全零值"+
			"（编一个 0 出来会让用户在音轨上读到「0 播放」）",
			audio.View, audio.FavTime, audio.PubTime)
	}
}

// TestSortsAreTheThreeBiuUses 盯住「三种排序」这个数——少了是漏了，多了是编的。
func TestSortsAreTheThreeBiuUses(t *testing.T) {
	want := map[CollectionSort]string{
		SortByFavTime: "mtime",
		SortByPlay:    "view",
		SortByPubTime: "pubtime",
	}
	if len(CollectionSorts) != 3 {
		t.Fatalf("有 %d 档排序，want 3", len(CollectionSorts))
	}
	seen := map[string]bool{}
	for _, s := range CollectionSorts {
		o, ok := want[s]
		if !ok {
			t.Fatalf("多出来的一档：%v", s)
		}
		if seen[o] {
			t.Errorf("两档排序发的是同一个 order（%q）", o)
		}
		seen[o] = true
		if got := s.order(); got != o {
			t.Errorf("%v 发出去的 order = %q，want %q", s.Label(), got, o)
		}
	}
	// 默认那一档是「最近收藏」：收藏夹是「我攒下来的东西」。
	if CollectionSorts[0] != SortByFavTime {
		t.Errorf("默认那一档是 %v，want 最近收藏", CollectionSorts[0])
	}
}

// TestAnUnknownSortFallsBackToMtime 排序值错了不会报错，B 站默默按自己的默认排，
// 于是界面上写「最近投稿」而实际给的是另一档。
func TestAnUnknownSortFallsBackToMtime(t *testing.T) {
	if got := CollectionSort(42).order(); got != "mtime" {
		t.Errorf("认不出的排序发出去的是 %q，want mtime", got)
	}
}

// TestPSIsClampedToBStationsLimit 显式传一个超大的 ps 也得夹住。
func TestPSIsClampedToBStationsLimit(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathResourceList, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("ps"); got != "20" {
			t.Errorf("ps = %q，want 20", got)
		}
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"medias": []any{}, "has_more": false}))
	})
	if _, err := f.client(t, Options{}).CollectionMedias(context.Background(), 1, SortByFavTime, "", 1, 5000); err != nil {
		t.Fatal(err)
	}
}

// TestPageZeroBecomesOne 下标从 1 起，传 0 是用户还没翻过页。
func TestPageZeroBecomesOne(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathResourceList, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("pn"); got != "1" {
			t.Errorf("pn = %q，want 1", got)
		}
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"medias": []any{}, "has_more": false}))
	})
	if _, err := f.client(t, Options{}).CollectionMedias(context.Background(), 1, SortByFavTime, "", 0, 0); err != nil {
		t.Fatal(err)
	}
}

// ── 收藏 / 取消收藏 ────────────────────────────────────────────────────────

// TestFavoriteAndUnfavoritePostToTheirOwnEndpoints 两个写接口别搞反了。
func TestFavoriteAndUnfavoritePostToTheirOwnEndpoints(t *testing.T) {
	for _, c := range []struct {
		name string
		call func(*Client) error
		want string
	}{
		{"收藏", func(c *Client) error { return c.FavoriteCollection(context.Background(), 42) }, pathFolderFav},
		{"取消收藏", func(c *Client) error { return c.UnfavoriteCollection(context.Background(), 42) }, pathFolderUnfav},
	} {
		f := newFakeBili(t)
		f.on(c.want, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("%s 用的方法是 %s，want POST", c.name, r.Method)
			}
			writeJSON(w, jsonData(t, 0, "0", nil))
		})
		if err := c.call(f.client(t, Options{})); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		reqs := f.requests()
		if len(reqs) != 1 || reqs[0].Path != c.want {
			t.Fatalf("%s 打到了 %v，want %s", c.name, reqs, c.want)
		}
		if got := reqs[0].Form.Get("media_id"); got != "42" {
			t.Errorf("%s 的 media_id = %q", c.name, got)
		}
	}
}

// TestCSRFGoesIntoTheFormBodyToo B 站 web 端校验的是表单体里那个 csrf
// （上游的 request-interceptors 就是塞进 POST 的 data 里的）。少一处就是 -111。
func TestCSRFGoesIntoTheFormBodyToo(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathFolderFav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", nil))
	})
	c := f.client(t, Options{CSRF: func() string { return "jct-fixture" }})
	if err := c.FavoriteCollection(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
	got := f.requests()[0]
	if v := got.Form.Get("csrf"); v != "jct-fixture" {
		t.Errorf("表单体里的 csrf = %q，want jct-fixture", v)
	}
	if v := got.Query.Get("csrf"); v != "jct-fixture" {
		t.Errorf("查询串里的 csrf = %q", v)
	}
	if v := got.Header.Get(CSRFHeaderName); v != "jct-fixture" {
		t.Errorf("%s 头 = %q", CSRFHeaderName, v)
	}
}

// TestWriteCallDoesNotMutateTheCallersForm 就地改那张表的话，csrf 会被带进下一次调用。
func TestWriteCallDoesNotMutateTheCallersForm(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathFolderFav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", nil))
	})
	form := url.Values{}
	form.Set("media_id", "1")
	c := f.client(t, Options{CSRF: func() string { return "jct" }})
	if _, err := c.Do(context.Background(), c.writeCall(f.URL+pathFolderFav, form)); err != nil {
		t.Fatal(err)
	}
	if _, ok := form["csrf"]; ok {
		t.Error("writeCall 就地改了调用方给的那张表")
	}
}

// ── 批量删除 ───────────────────────────────────────────────────────────────

// TestRemoveSendsIDAndTypePairs 只给 id 的话 B 站删不掉任何东西**而且不报错**。
func TestRemoveSendsIDAndTypePairs(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathResourceBatchDel, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", nil))
	})
	medias := []Media{
		{ID: 11, Type: MediaVideo}, {ID: 22, Type: MediaAudio},
	}
	if err := f.client(t, Options{}).RemoveCollectionMedias(context.Background(), 42, medias); err != nil {
		t.Fatal(err)
	}
	got := f.requests()[0]
	if want := "11:2,22:12"; got.Form.Get("resources") != want {
		t.Errorf("resources = %q，want %q", got.Form.Get("resources"), want)
	}
	if v := got.Form.Get("media_id"); v != "42" {
		t.Errorf("media_id = %q", v)
	}
}

// TestRemoveChunksABigFolder 一次发几千条的结果不是报错，是**只删掉一部分**。
func TestRemoveChunksABigFolder(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathResourceBatchDel, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", nil))
	})
	medias := make([]Media, batchDelSize*2+7)
	for i := range medias {
		medias[i] = Media{ID: int64(1000 + i), Type: MediaVideo}
	}
	if err := f.client(t, Options{}).RemoveCollectionMedias(context.Background(), 42, medias); err != nil {
		t.Fatal(err)
	}
	reqs := f.requests()
	if len(reqs) != 3 {
		t.Fatalf("发了 %d 批，want 3（%d = 50+50+7）", len(reqs), len(medias))
	}
	for i, r := range reqs {
		if n := len(strings.Split(r.Form.Get("resources"), ",")); n != wantBatchSize(i) {
			t.Errorf("第 %d 批有 %d 条，want %d", i+1, n, wantBatchSize(i))
		}
	}
}

// wantBatchSize 是第 i 批该有几条（i 从 0 起）。
func wantBatchSize(i int) int {
	switch i {
	case 0, 1:
		return batchDelSize
	default:
		return 7
	}
}

// TestRemoveNothingSendsNothing 空请求 B 站回 -400，而「没勾任何东西就点删除」
// 根本不该发请求。
func TestRemoveNothingSendsNothing(t *testing.T) {
	f := newFakeBili(t)
	if err := f.client(t, Options{}).RemoveCollectionMedias(context.Background(), 42, nil); err != nil {
		t.Fatal(err)
	}
	if n := len(f.requests()); n != 0 {
		t.Errorf("发了 %d 个请求，want 0", n)
	}
}

// TestRemoveStopsAtTheFirstFailure 中间那批失败要说清楚是第几条起。
func TestRemoveStopsAtTheFirstFailure(t *testing.T) {
	f := newFakeBili(t)
	var calls int
	f.on(pathResourceBatchDel, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 2 {
			writeJSON(w, jsonData(t, -400, "请求错误", nil))
			return
		}
		writeJSON(w, jsonData(t, 0, "0", nil))
	})
	medias := make([]Media, batchDelSize+1)
	for i := range medias {
		medias[i] = Media{ID: int64(i), Type: MediaVideo}
	}
	err := f.client(t, Options{}).RemoveCollectionMedias(context.Background(), 42, medias)
	if err == nil {
		t.Fatal("第二批失败了还报成功")
	}
	if !strings.Contains(err.Error(), "第 51 条起") {
		t.Errorf("错里没说是第几条起：%v", err)
	}
	if calls != 2 {
		t.Errorf("失败之后还发了 %d 次，want 2", calls)
	}
}

// ── 清除失效内容 ───────────────────────────────────────────────────────────

// TestCleanSendsOnlyTheFolderID 清除失效内容是**服务端的一个动作**。
//
// 🔴 这一条钉的是「**不发 resources**」：最容易走偏的实现是照着
// `RemoveCollectionMedias` 写一遍——先在本地按 `Attr` 筛出失效的，再发一批
// `resources`。那一条路在几个地方都会静默出错（`Attr` 认不全、翻页期间列表
// 会变、一个几千首的收藏夹要几十趟请求），而症状都是同一个：清完还剩几个。
//
// 所以这里断言的是「**一个 media_id 就够了**」——多发的每一个字段都是本地
// 推断出来的、而 B 站自己知道得更准。多给了不会报错，所以这个错不会自己暴露。
func TestCleanSendsOnlyTheFolderID(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathResourceClean, func(w http.ResponseWriter, r *http.Request) {
		if got := r.PostForm.Get("media_id"); got != "12345" {
			t.Errorf("media_id = %q，want 12345", got)
		}
		if v := r.PostForm.Get("resources"); v != "" {
			t.Errorf("发了 resources = %q——清除失效内容该由 B 站自己认，本地筛一遍必然漏", v)
		}
		writeJSON(w, jsonData(t, 0, "0", 0))
	})

	c := f.client(t, Options{CSRF: func() string { return "jct-fixture" }})
	if err := c.CleanInvalidResources(context.Background(), 12345); err != nil {
		t.Fatal(err)
	}
	reqs := f.requests()
	if len(reqs) != 1 {
		t.Fatalf("发了 %d 个请求，want 1", len(reqs))
	}
	if reqs[0].Method != http.MethodPost {
		t.Errorf("用的是 %s，want POST", reqs[0].Method)
	}
	// ⚠️ 这里验的是「**它走的是 `writeCall`**」而不是 CSRF 机制本身（机制有
	// `TestCSRFGoesIntoTheFormBodyToo` 钉着）：自己拼一个 `Call` 出来的话，
	// 少一处 csrf 是 B 站回 -111 而不是本地报错——而真机上一半会被报成
	// 「风控」，排障方向从一开始就错了。
	if got := reqs[0].Form.Get("csrf"); got != "jct-fixture" {
		t.Errorf("表单体里没有 csrf（没走 writeCall？）= %q", got)
	}
}

// TestCleanRefusesNobody 收藏夹 id 不对就报错，不发请求。
func TestCleanRefusesNobody(t *testing.T) {
	f := newFakeBili(t)
	if err := f.client(t, Options{}).CleanInvalidResources(context.Background(), 0); err == nil {
		t.Fatal("media_id=0 也发出去了，应该报错")
	}
	if n := len(f.requests()); n != 0 {
		t.Errorf("发出了 %d 个请求，want 0", n)
	}
}

// TestCleanSaysWhichFolderItWas 清失败时那句话里要带着收藏夹 id。
//
// 用户点的是「清除失效内容」这一项，而收藏夹可能有几十个——一句「清不掉」
// 不够他去 B 站那边确认是哪一个出了问题。
func TestCleanSaysWhichFolderItWas(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathResourceClean, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, -400, "请求错误", nil))
	})

	err := f.client(t, Options{}).CleanInvalidResources(context.Background(), 12345)
	if err == nil {
		t.Fatal("B 站回错了还报成功")
	}
	if !strings.Contains(err.Error(), "12345") {
		t.Errorf("错里没说是哪个收藏夹：%v", err)
	}
}

// ── 类型与文案 ─────────────────────────────────────────────────────────────

// TestCollectionTypeLabelsAreBiuS 三个类型的名字是头图区第三行要写的那几个字。
func TestCollectionTypeLabelsAreBiuS(t *testing.T) {
	want := map[CollectionType]string{
		CollectionFavorite: "收藏夹",
		CollectionAlbum:    "视频合集",
		CollectionSeries:   "视频系列",
	}
	for typ, label := range want {
		if got := typ.Label(); got != label {
			t.Errorf("%d 的名字是 %q，want %q", typ, got, label)
		}
	}
	if CollectionFavorite != 11 || CollectionAlbum != 21 || CollectionSeries != 31 {
		t.Errorf("三个类型的 id 是 %d/%d/%d，want 11/21/31（抄 B 站的，别改）",
			CollectionFavorite, CollectionAlbum, CollectionSeries)
	}
}

// TestParseCollectionTypeFallsBackToFavorite 类型是从 URL 的查询串里来的。
func TestParseCollectionTypeFallsBackToFavorite(t *testing.T) {
	for in, want := range map[string]CollectionType{
		"11": CollectionFavorite, "21": CollectionAlbum, "31": CollectionSeries,
		"": CollectionFavorite, " 21 ": CollectionAlbum,
		"2":   CollectionFavorite, // PLAN 里写的旧编号：认不出，回收藏夹
		"abc": CollectionFavorite,
	} {
		if got := ParseCollectionType(in); got != want {
			t.Errorf("ParseCollectionType(%q) = %v，want %v", in, got, want)
		}
	}
}

// TestOnlyVideoAndAudioArePlayable 合集和电影在收藏夹里也能出现，但它们没有音频流。
func TestOnlyVideoAndAudioArePlayable(t *testing.T) {
	if !(Media{Type: MediaVideo}).Playable() {
		t.Error("视频稿件不该被判成放不了")
	}
	if !(Media{Type: MediaAudio}).Playable() {
		t.Error("音频不该被判成放不了")
	}
	for _, typ := range []int{21, 24} {
		if (Media{Type: typ}).Playable() {
			t.Errorf("type=%d 被判成能放了，它没有音频流", typ)
		}
	}
}

// TestInvalidContentsStillComeBack 失效的条目照样出现在列表里（Biu 也照样画），
// 直接不画的话用户会发现「收藏夹里的歌自己少了几首」。
func TestInvalidContentsStillComeBack(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathResourceList, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"has_more": false,
			"medias": []map[string]any{
				{"id": 1, "type": 2, "title": "好的", "duration": 10, "attr": 0},
				{"id": 2, "type": 2, "title": "UP 删了", "duration": 20, "attr": 9},
			},
		}))
	})
	pg, err := f.client(t, Options{}).CollectionMedias(context.Background(), 1, SortByFavTime, "", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(pg.Medias) != 2 {
		t.Fatalf("拿到了 %d 条，want 2", len(pg.Medias))
	}
	if pg.Medias[1].Attr == 0 {
		t.Error("失效那条的 Attr 没读出来")
	}
}

// TestACollectionMediaCarriesTheOwnerID 钉住 `upper.mid` 真的被解析下来。
//
// 播放条上那一行 UP 名要靠它拼 `/user/{id}`（`app.Item.ArtistMid`）。少了它
// 症状是**半个功能**：名字照旧显示、点下去没反应——而界面上看不出坏了。
func TestACollectionMediaCarriesTheOwnerID(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathResourceList, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"medias": mediasFixture(2), "has_more": false,
		}))
	})

	pg, err := f.client(t, Options{}).CollectionMedias(context.Background(), 7, SortByFavTime, "", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(pg.Medias) != 2 {
		t.Fatalf("拿到 %d 条内容，want 2 条", len(pg.Medias))
	}
	for i, m := range pg.Medias {
		if m.Owner != "UP "+strconv.Itoa(i) {
			t.Errorf("第 %d 条的 UP 名是 %q，want %q", i, m.Owner, "UP "+strconv.Itoa(i))
		}
		if m.OwnerMid != int64(900+i) {
			t.Errorf("第 %d 条的 UP 编号是 %d，want %d——播放条上那一句点不动",
				i, m.OwnerMid, 900+i)
		}
	}
}

// TestTheOwnerFaceGetsItsProtocol 钉住「创建者头像的地址是能抓的」。
//
// ⚠️ **B 站给的是协议相对地址**（`//i0.hdslb.com/bfs/face/…`），而封面缓存
// 是拿 `http.Client` 抓的——那种地址在 Go 那边是
// `unsupported protocol scheme ""`，抓回来的是一张空图，而界面上那一枚 24×24
// 的头像于是**永远是一块空圆**（盒子还是 24×24，所以量盒子抓不到这件事）。
//
// 仓库里另外三处消费同一个字段的地方都补了协议（`search.go` 的
// `normalizeImageURL`、`follow.go` 的 `avatarURL`、UP 空间那一处），只有收藏夹这一处
// 漏了——所以这里补上，并且**逐档钉住**。
func TestTheOwnerFaceGetsItsProtocol(t *testing.T) {
	for _, c := range []struct {
		raw, want string
	}{
		{"//i0.hdslb.com/bfs/face/x.jpg", "https://i0.hdslb.com/bfs/face/x.jpg"},
		{"http://i0.hdslb.com/bfs/face/x.jpg", "http://i0.hdslb.com/bfs/face/x.jpg"},
		{"https://i0.hdslb.com/bfs/face/x.jpg", "https://i0.hdslb.com/bfs/face/x.jpg"},
		{"", ""},
	} {
		f := newFakeBili(t)
		f.on(pathFolderInfo, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, jsonData(t, 0, "0", map[string]any{
				"id": 12345, "attr": 2, "title": "通勤路上", "cover": "",
				"intro": "早上听", "media_count": 1, "fav_state": 0,
				"upper": map[string]any{"mid": 900, "name": "UP 小明", "face": c.raw},
			}))
		})
		got, err := f.client(t, Options{}).CollectionInfo(context.Background(), 12345, CollectionAlbum)
		if err != nil {
			t.Fatal(err)
		}
		if got.Owner.Face != c.want {
			t.Errorf("face %q → %q，want %q", c.raw, got.Owner.Face, c.want)
		}
	}
}
