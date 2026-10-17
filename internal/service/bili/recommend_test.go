package bili

// 推荐页那几个接口的这一组盯三件事：
//
//  1. **参数真的发出去了，而且发的是上游那些数**（分区号、页大小、plat、
//     web_location）。少一个的症状是「B 站给了别的分区的推荐」或者「一页只有
//     几条」——而那在界面上和「B 站今天没推什么」长得一模一样。
//  2. **哪几个签名、哪几个带 csrf**。分区推荐不签名的症状是被风控挡成 -403，
//     和「没权限」一模一样。
//  3. **B 站那两种身份字段各自落在哪**（分区推荐给 `trackid`、音乐榜给榜上
//     那一条的 `id`），以及封面是协议相对地址时要补成 https。
//
// ⚠️ 这里**没有一个断言是在证明「B 站真的这么返回」**——fixture 是我们自己
// 造的（见 client_test.go 顶上那一段）。真网对不对得上只能靠真机回归，见任务
// 报告里的「需要真网验，原因是 X」。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

// regionFeedFixture 是一份分区推荐的返回。
//
// 封面故意给协议相对地址（`//i0.hdslb.com/…`）：那是 B 站在这几个接口上真会
// 给的形态，而它原样透出去的话界面上去取图直接失败。
func regionFeedFixture(t *testing.T, n int) string {
	t.Helper()
	items := make([]map[string]any, 0, n)
	for i := range n {
		items = append(items, map[string]any{
			"aid": int64(2000 + i), "bvid": "BV1reg" + string(rune('1'+i)),
			"cid": int64(700 + i), "title": "分区第 " + string(rune('1'+i)) + " 首",
			"cover":    "//i0.hdslb.com/cover" + string(rune('1'+i)) + ".jpg",
			"duration": int64(200 + i),
			"trackid":  "tk" + string(rune('1'+i)),
			"author":   map[string]any{"mid": int64(11 + i), "name": "UP " + string(rune('A'+i))},
			"stat":     map[string]any{"view": int64(999 * (i + 1))},
		})
	}
	raw, err := json.Marshal(map[string]any{"archives": items})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestRegionFeedSendsEveryParamAndSignsIt 盯分区推荐的参数和签名。
func TestRegionFeedSendsEveryParamAndSignsIt(t *testing.T) {
	f := signedFake(t)
	f.on(regionFeedPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", json.RawMessage(regionFeedFixture(t, 2))))
	})
	c := f.client(t, Options{})

	if _, err := c.RegionFeed(context.Background(), RegionFeedQuery{
		Page: 2, Count: 15, Region: RegionMusic,
	}); err != nil {
		t.Fatal(err)
	}

	got := lastRequestTo(t, f, regionFeedPath)
	for _, q := range []struct{ key, want string }{
		{"display_id", "2"}, {"request_cnt", "15"}, {"from_region", "1003"},
		{"device", "web"}, {"plat", "30"}, {"web_location", "333.40138"},
	} {
		if v := got.Query.Get(q.key); v != q.want {
			t.Errorf("%s = %q，want %q（查询串：%v）", q.key, v, q.want, got.Query)
		}
	}
	if got.Query.Get("w_rid") == "" {
		t.Error("分区推荐没有 WBI 签名——上游签了名，不签会被风控挡成 -403")
	}
}

// TestTheGuichuRegionIsItsOwnNumber 盯鬼畜那一档发的分区号。
//
// ⚠️ 这两个数在 B 站的 **v2 分区表**里，不是 `tid`。发音乐那个号到鬼畜那一档
// 不会报错，只会给一批音乐的推荐——用户看到的是「鬼畜 Tab 里全是音乐」。
func TestTheGuichuRegionIsItsOwnNumber(t *testing.T) {
	f := signedFake(t)
	f.on(regionFeedPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", json.RawMessage(regionFeedFixture(t, 1))))
	})
	c := f.client(t, Options{})

	if _, err := c.RegionFeed(context.Background(), RegionFeedQuery{Region: RegionGuichu}); err != nil {
		t.Fatal(err)
	}
	if got := lastRequestTo(t, f, regionFeedPath).Query.Get("from_region"); got != "1007" {
		t.Errorf("鬼畜那一档的 from_region = %q，want 1007", got)
	}
}

// TestRegionFeedDefaultsAreTheUpstreamOnes 盯零值时补上的那几个数。
//
// 零值补成什么这件事必须是写死的：界面只传「第几个 Tab」和「第一页」，
// 一页几条和页码从 0 还是 1 起全在这一层（页码从 0 传的话 B 站当第一页返回，
// 「第 0 页」和「第 1 页」是同一批数据）。
func TestRegionFeedDefaultsAreTheUpstreamOnes(t *testing.T) {
	f := signedFake(t)
	f.on(regionFeedPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", json.RawMessage(regionFeedFixture(t, 1))))
	})
	c := f.client(t, Options{})

	if _, err := c.RegionFeed(context.Background(), RegionFeedQuery{}); err != nil {
		t.Fatal(err)
	}
	q := lastRequestTo(t, f, regionFeedPath).Query
	if q.Get("display_id") != "1" {
		t.Errorf("零页码发出去的是 %q，want 1", q.Get("display_id"))
	}
	if q.Get("request_cnt") != "15" {
		t.Errorf("零条数发出去的是 %q，want 15（上游 REGION_PAGE_SIZE）", q.Get("request_cnt"))
	}
	if q.Get("from_region") != "1003" {
		t.Errorf("零分区发出去的是 %q，want 1003（音乐）", q.Get("from_region"))
	}
}

// TestRegionFeedIsParsed 盯封面、UP、时长、身份字段各自落在哪。
func TestRegionFeedIsParsed(t *testing.T) {
	f := signedFake(t)
	f.on(regionFeedPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", json.RawMessage(regionFeedFixture(t, 2))))
	})
	c := f.client(t, Options{})

	items, err := c.RegionFeed(context.Background(), RegionFeedQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("解出来 %d 条，want 2", len(items))
	}
	got := items[0]
	if got.Title != "分区第 1 首" {
		t.Errorf("标题 = %q", got.Title)
	}
	// ⚠️ 协议相对地址要补成 https：B 站在这几个接口上就这么给，原样透出去的话
	// 界面上去取图会失败，而那一眼看不出来（画的是一张空框）。
	if want := "https://i0.hdslb.com/cover1.jpg"; got.Cover != want {
		t.Errorf("封面 = %q，want %q", got.Cover, want)
	}
	if got.Owner != "UP A" || got.OwnerMid != 11 {
		t.Errorf("UP = %q/%d，want UP A/11", got.Owner, got.OwnerMid)
	}
	if want := 200 * time.Second; got.Duration != want {
		t.Errorf("时长 %v，want %v", got.Duration, want)
	}
	if got.Bvid != "BV1reg1" || got.Cid != 700 {
		t.Errorf("bvid/cid = %q/%d", got.Bvid, got.Cid)
	}
	// trackid 是分区推荐独有的兜底身份（推的是歌不是稿件）。
	if got.TrackID != "tk1" {
		t.Errorf("trackid = %q，want tk1", got.TrackID)
	}
	if got.View != 999 {
		t.Errorf("播放量 %d，want 999", got.View)
	}
}

// TestAnEmptyRegionFeedIsNotAFailure 盯「B 站给了空数组」。
//
// B 站在放不下东西时给 `[]`、有时给 `null`，两种都是「没有」——不是错。
// 判错的话界面上是「加载失败 + 重试」，而重试多少次都是空的。
func TestAnEmptyRegionFeedIsNotAFailure(t *testing.T) {
	f := signedFake(t)
	f.on(regionFeedPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"archives": nil}))
	})
	c := f.client(t, Options{})

	items, err := c.RegionFeed(context.Background(), RegionFeedQuery{})
	if err != nil {
		t.Fatalf("空列表算错了：%v", err)
	}
	if len(items) != 0 {
		t.Errorf("空列表解出来 %d 条", len(items))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 音乐榜（「流行」那一档）
// ─────────────────────────────────────────────────────────────────────────────

// rankFixture 是一份音乐榜的返回：**第一条挂着稿件，第二条没有**。
func rankFixture(t *testing.T) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"list": []map[string]any{
		{
			"id": int64(31), "music_title": "榜上那首歌", "cover": "//i0.hdslb.com/rank.jpg",
			"author": "榜上作者", "bvid": "BV1top", "aid": int64(31), "cid": int64(31),
			"related_archive": map[string]any{
				"bvid": "BV1arc", "cid": int64(99), "cover": "//i0.hdslb.com/arc.jpg",
				"title": "挂着的稿件", "uid": int64(77), "username": "稿件作者",
				"vv_count": int64(4242), "duration": int64(185),
			},
		},
		{ // 纯音乐条目：没有稿件，歌名/封面/UP 名全靠顶层那几个（这里也是空的）。
			"id": int64(32), "music_title": "", "cover": "", "author": "",
			"bvid": "", "aid": int64(0), "cid": int64(0),
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestMusicRankSendsItsOwnParams 盯「流行」那一档用的是**另一套**参数。
//
// 它走的是音乐榜（pn/ps/333.1351），不是分区推荐——写成同一套的话用户点
// 「流行」看到的是第三批分区推荐，而上游那儿根本不是那么回事。
func TestMusicRankSendsItsOwnParams(t *testing.T) {
	f := newFakeBili(t)
	f.on(musicRankPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", json.RawMessage(rankFixture(t))))
	})
	c := f.client(t, Options{})

	if _, err := c.MusicRank(context.Background(), MusicRankQuery{Page: 2, PageSize: 20}); err != nil {
		t.Fatal(err)
	}
	got := lastRequestTo(t, f, musicRankPath)
	for _, q := range []struct{ key, want string }{
		{"pn", "2"}, {"ps", "20"}, {"web_location", "333.1351"},
	} {
		if v := got.Query.Get(q.key); v != q.want {
			t.Errorf("%s = %q，want %q", q.key, v, q.want)
		}
	}
	if got.Query.Get("w_rid") != "" {
		t.Error("音乐榜不该签名——上游那一处没写 useWbi")
	}
}

// TestMusicRankPrefersTheRelatedArchive 盯「挂着的稿件优先」。
//
// 榜上有些条目挂着一整条稿件（歌是投稿里的），有些没有。上游
// （`music-recommend/index.tsx:31-45`）在挂着的那些上取稿件的标题/封面/UP 名，
// 挂着的 cid 也换过去——取流要 cid，不换会取到第一集。
func TestMusicRankPrefersTheRelatedArchive(t *testing.T) {
	f := newFakeBili(t)
	f.on(musicRankPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", json.RawMessage(rankFixture(t))))
	})
	c := f.client(t, Options{})

	items, err := c.MusicRank(context.Background(), MusicRankQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("解出来 %d 条，want 2", len(items))
	}
	withArchive := items[0]
	if withArchive.Title != "挂着的稿件" {
		t.Errorf("标题 = %q，want 挂着的稿件（顶层那个 music_title 靠后）", withArchive.Title)
	}
	if withArchive.Owner != "稿件作者" || withArchive.OwnerMid != 77 {
		t.Errorf("UP = %q/%d，want 稿件作者/77", withArchive.Owner, withArchive.OwnerMid)
	}
	if withArchive.Bvid != "BV1arc" || withArchive.Cid != 99 {
		t.Errorf("bvid/cid = %q/%d，want BV1arc/99", withArchive.Bvid, withArchive.Cid)
	}
	if want := "https://i0.hdslb.com/arc.jpg"; withArchive.Cover != want {
		t.Errorf("封面 = %q，want %q", withArchive.Cover, want)
	}
	if withArchive.View != 4242 {
		t.Errorf("播放量 = %d，want 4242（挂着的稿件的 vv_count）", withArchive.View)
	}
	if want := 185 * time.Second; withArchive.Duration != want {
		t.Errorf("时长 = %v，want %v", withArchive.Duration, want)
	}

	// 没有稿件的那一条：兜底身份是榜上那一条自己的 id。
	bare := items[1]
	if bare.TrackID != "32" {
		t.Errorf("没有稿件时兜底身份 = %q，want 32（榜上那一条的 id）", bare.TrackID)
	}
	if bare.Bvid != "" {
		t.Errorf("没有稿件却解出了 bvid = %q", bare.Bvid)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 新歌速递
// ─────────────────────────────────────────────────────────────────────────────

// TestNewMusicSendsItsParamsAndCSRF 盯新歌速递那两个接口的查询串。
//
// ⚠️ **csrf 是上游要的**（`useCSRF: true`）。没登录时那个值是空的，发过去
// 也就是少一个参数——但抄成不传就和上游不是一个请求了，而 B 站哪天开始
// 拿它当开关的时候我们会是唯一一个不传的。
func TestNewMusicSendsItsParamsAndCSRF(t *testing.T) {
	f := newFakeBili(t)
	f.on(newMusicPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"list": []map[string]any{
			{"id": int64(5), "music_title": "新歌", "cover": "//i0.hdslb.com/n.jpg",
				"author": "新歌作者", "bvid": "BV1new", "aid": int64(5), "cid": int64(5)},
		}}))
	})
	f.on(newMusicBannerPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"list": []map[string]any{
			{"music_id": "m1", "archive_title": "头图位那首", "cover": "//i0.hdslb.com/b.jpg",
				"author": "头图作者", "bvid": "BV1ban", "aid": int64(6), "cid": int64(6)},
		}}))
	})
	c := f.client(t, Options{CSRF: func() string { return "the-jct" }})

	items, err := c.NewMusic(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Title != "新歌" {
		t.Fatalf("新歌列表解错了：%+v", items)
	}
	q := lastRequestTo(t, f, newMusicPath).Query
	if q.Get("plat") != "2" || q.Get("web_location") != "333.1351" {
		t.Errorf("新歌列表的查询串 = %v", q)
	}
	if q.Get("csrf") != "the-jct" {
		t.Errorf("csrf = %q，want the-jct（上游 useCSRF 对 GET 是塞进查询串）", q.Get("csrf"))
	}

	// ⚠️ 头图位的标题字段叫 `archive_title`，不是 `music_title`——按后者读
	// 出来的是空串，界面上是一排没有歌名的卡片。
	banner, err := c.NewMusicBanner(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(banner) != 1 || banner[0].Title != "头图位那首" {
		t.Fatalf("头图位解错了：%+v", banner)
	}
	if q := lastRequestTo(t, f, newMusicBannerPath).Query; q.Get("csrf") != "the-jct" {
		t.Errorf("头图位的 csrf = %q，want the-jct", q.Get("csrf"))
	}
	if banner[0].TrackID != "m1" {
		t.Errorf("头图位的兜底身份 = %q，want m1（music_id）", banner[0].TrackID)
	}
}

// TestAnUnreadableRecommendPayloadIsNotANetworkError 盯「读不懂 data」。
//
// HTTP 200、code 0，而 data 是个字符串。判成网络错的话界面上说的是
// 「连不上 B 站，检查一下网络」——用户会换网、换 DNS、白折腾一场，而真相是
// B 站改了协议（重试多少次都是同一段看不懂的 JSON）。
func TestAnUnreadableRecommendPayloadIsNotANetworkError(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		call       func(*Client) error
	}{
		{"分区推荐", regionFeedPath, func(c *Client) error {
			_, err := c.RegionFeed(context.Background(), RegionFeedQuery{})
			return err
		}},
		{"音乐榜", musicRankPath, func(c *Client) error {
			_, err := c.MusicRank(context.Background(), MusicRankQuery{})
			return err
		}},
		{"新歌列表", newMusicPath, func(c *Client) error {
			_, err := c.NewMusic(context.Background())
			return err
		}},
		{"新歌头图位", newMusicBannerPath, func(c *Client) error {
			_, err := c.NewMusicBanner(context.Background())
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := signedFake(t)
			f.on(tc.path, func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, jsonData(t, 0, "0", "这不是我们认识的那个形状"))
			})
			err := tc.call(f.client(t, Options{}))
			if err == nil {
				t.Fatal("读不懂的 data 被当成了成功")
			}
			if !errors.Is(err, errReadRecommend) {
				t.Errorf("错认错了：%v（want errReadRecommend）", err)
			}
			if errors.Is(err, ErrNetwork) {
				t.Error("读不懂的返回被算成了网络错——用户会去换网络，而真相是 B 站改了协议")
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 失败分类（这一组盯的是「用户看到的到底是不是人话」）
// ─────────────────────────────────────────────────────────────────────────────

// TestTheRecommendFailuresSaySomethingUseful 盯四类失败各自翻成什么。
//
// 这一条是**跨包**的（它用 `Friendly`），因为界面层画的正是那句翻好的话。
// 它要回答的是：同一个码在推荐页上和在播放条上说的是同一件事吗。
func TestTheRecommendFailuresSaySomethingUseful(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		want string
	}{
		{"风控", -352, codeMessages[-352]},
		// ⚠️ -412 说的是**风控那一句**：`APIError.Is` 把 -352 和 -412 归成同一
		// 类（都别重试），而 `Friendly` 按类给话。所以这两句不一样是**故意**
		// 的——处置一样的话就该说同一句话。
		{"被拦下", -412, codeMessages[-352]},
		{"限流", -509, codeMessages[-509]},
		{"登录态没了", -101, codeMessages[-101]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := signedFake(t)
			f.on(regionFeedPath, func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, jsonData(t, tc.code, "B 站的原话", nil))
			})
			_, err := f.client(t, Options{}).RegionFeed(context.Background(), RegionFeedQuery{})
			if err == nil {
				t.Fatal("非零 code 被当成了成功")
			}
			if got := Friendly(err); got != tc.want {
				t.Errorf("界面上会显示 %q，want %q", got, tc.want)
			}
		})
	}
}

// lastRequestTo 是打给某条路径的最后一次请求。录下来的第一条是 WBI 密钥那次
// （打的是 nav），所以这里必须按路径筛，不能拿第一条。
func lastRequestTo(t *testing.T, f *fakeBili, path string) recordedRequest {
	t.Helper()
	var got recordedRequest
	for _, rec := range f.requests() {
		if rec.Path == path {
			got = rec
		}
	}
	if got.Path == "" {
		t.Fatalf("没有发出 %s 的请求", path)
	}
	return got
}
