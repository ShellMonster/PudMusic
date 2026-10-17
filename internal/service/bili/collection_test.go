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
			"upper": map[string]any{"mid": 900, "name": "UP 小明", "face": "f.jpg"},
		}))
	})

	got, err := f.client(t, Options{}).CollectionInfo(context.Background(), 12345, CollectionAlbum)
	if err != nil {
		t.Fatal(err)
	}
	want := CollectionInfo{
		ID: 12345, Title: "通勤路上", Intro: "早上听",
		Cover: "https://i0.hdslb.com/cover.jpg", Type: CollectionAlbum,
		MediaCount: 42, Owner: CollectionOwner{Mid: 900, Name: "UP 小明", Face: "f.jpg"},
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
			"bvid":    "BV1xx411c7mD", "upper": map[string]any{"name": "UP " + strconv.Itoa(i)},
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

		pg, err := f.client(t, Options{}).CollectionMedias(context.Background(), 7, sort, 1, 100)
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
	if _, err := f.client(t, Options{}).CollectionMedias(context.Background(), 1, SortByFavTime, 1, 5000); err != nil {
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
	if _, err := f.client(t, Options{}).CollectionMedias(context.Background(), 1, SortByFavTime, 0, 0); err != nil {
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
	pg, err := f.client(t, Options{}).CollectionMedias(context.Background(), 1, SortByFavTime, 1, 0)
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
