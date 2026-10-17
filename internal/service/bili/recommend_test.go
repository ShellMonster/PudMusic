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

// rankFixture 是一份音乐榜的返回，**照着 B 站真实样件的形态**（Biu 上游
// `src/service/service.md` 里 `music/comprehensive/web/rank` 那一份原样返回的参数）：
// **`aid` / `cid` / `related_archive.cid` 都是 JSON 字符串**，`uid` / `vv_count` /
// `duration` / `id` 是数字。
//
// ⚠️ 这一份 fixture 的形态是被真网样件钉过的：最早这里三项都写成了数字，于是
// 「流行」那一档在真网上整档解码失败（`cannot unmarshal string into … int64`），
// 而 26 条用例一条都没抓得到——它们喂的全是自己编的数字。
func rankFixture(t *testing.T) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"list": []map[string]any{
		{
			"id": int64(3593581), "music_title": "I Met A Boy", "music_id": "MA584087591096195920",
			"cid": "32447137298", "aid": "115182114246511", "bvid": "BV1DFHdzjEkK",
			"cover": "//i0.hdslb.com/rank.jpg", "author": "Mimi Webb", "score": int64(212),
			"related_archive": map[string]any{
				"bvid": "BV1Nhp4z4EVa", "cid": "115219862986158", "aid": "115219862986158",
				"cover": "http://i0.hdslb.com/bfs/archive/f7e.jpg",
				"title": "【MV】【Mimi Webb】「I Met A Boy」",
				"uid":   int64(384580074), "username": "fallinforjulia",
				"vv_count": int64(75), "duration": int64(179), "fname": "MV",
			},
		},
		{ // 纯音乐条目：没有稿件，歌名/封面/UP 名全靠顶层那几个（这里也是空的）。
			"id": int64(3593582), "music_title": "", "cover": "", "author": "",
			"bvid": "", "aid": "", "cid": "",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// rankFixtureNumbers 是同一份榜，**id 全写成数字**。
//
// B 站在这几个 id 上是两种形态混着发的，所以「认字符串」和「认数字」都要认。
func rankFixtureNumbers(t *testing.T) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"list": []map[string]any{{
		"id": int64(31), "music_title": "数字形态的那首", "cover": "//i0.hdslb.com/rank.jpg",
		"author": "榜上作者", "bvid": "BV1top", "aid": int64(31), "cid": int64(31),
		"related_archive": map[string]any{
			"bvid": "BV1arc", "cid": int64(99), "cover": "//i0.hdslb.com/arc.jpg",
			"title": "数字形态的稿件", "uid": int64(77), "username": "稿件作者",
			"vv_count": int64(4242), "duration": int64(185),
		},
	}}})
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
	if withArchive.Title != "【MV】【Mimi Webb】「I Met A Boy」" {
		t.Errorf("标题 = %q，want 挂着的稿件的标题（顶层那个 music_title 靠后）", withArchive.Title)
	}
	if withArchive.Owner != "fallinforjulia" || withArchive.OwnerMid != 384580074 {
		t.Errorf("UP = %q/%d，want fallinforjulia/384580074", withArchive.Owner, withArchive.OwnerMid)
	}
	if withArchive.Bvid != "BV1Nhp4z4EVa" || withArchive.Cid != 115219862986158 {
		t.Errorf("bvid/cid = %q/%d，want BV1Nhp4z4EVa/115219862986158", withArchive.Bvid, withArchive.Cid)
	}
	// ⚠️ **这几个 id 在真实返回里是字符串**（`"aid": "115182114246511"`）。按数字解的
	// 时候整档会解不出来，而症状是「这一块没拉出来」——离原因隔了三层。
	if withArchive.Aid != 115182114246511 {
		t.Errorf("aid = %d，want 115182114246511（真实返回里它是字符串）", withArchive.Aid)
	}
	// ⚠️ 真实样件里这个封面是 **`http://`** 开头的，而 `normalizeImageURL` 只给
	// 协议相对的（`//…`）补 https，**原样留着 http**：那是 Biu 上游
	// `common/utils/url.ts` 的同一套规矩（以 http 开头的原样返回），而原生窗口
	// 里明文取图没有问题（也不带凭据，imgcache 走的是它自己那个客户端）。
	if want := "http://i0.hdslb.com/bfs/archive/f7e.jpg"; withArchive.Cover != want {
		t.Errorf("封面 = %q，want %q", withArchive.Cover, want)
	}
	if withArchive.View != 75 {
		t.Errorf("播放量 = %d，want 75（挂着的稿件的 vv_count）", withArchive.View)
	}
	if want := 179 * time.Second; withArchive.Duration != want {
		t.Errorf("时长 = %v，want %v", withArchive.Duration, want)
	}

	// 没有稿件的那一条：兜底身份是榜上那一条自己的 id。
	bare := items[1]
	if bare.TrackID != "3593582" {
		t.Errorf("没有稿件时兜底身份 = %q，want 3593582（榜上那一条的 id）", bare.TrackID)
	}
	if bare.Bvid != "" {
		t.Errorf("没有稿件却解出了 bvid = %q", bare.Bvid)
	}
}

// TestTheIDsAlsoComeAsNumbers 盯「两种形态都认」。
//
// ⚠️ 这一条和上一条是一对：B 站在这几个 id 上混着发两种形态，所以**不能**照着
// 某一份样件把字段统一改成字符串——那样另一天 B 站换回去，「流行」那一档又会
// 整档解不出来，而症状还是那句离原因很远的「这一块没拉出来」。
func TestTheIDsAlsoComeAsNumbers(t *testing.T) {
	f := newFakeBili(t)
	f.on(musicRankPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", json.RawMessage(rankFixtureNumbers(t))))
	})
	c := f.client(t, Options{})

	items, err := c.MusicRank(context.Background(), MusicRankQuery{})
	if err != nil {
		t.Fatalf("数字形态的 id 解不出来：%v", err)
	}
	if len(items) != 1 {
		t.Fatalf("解出来 %d 条，want 1", len(items))
	}
	if items[0].Aid != 31 || items[0].Cid != 99 || items[0].OwnerMid != 77 {
		t.Errorf("aid/cid/uid = %d/%d/%d，want 31/99/77", items[0].Aid, items[0].Cid, items[0].OwnerMid)
	}
}

// TestAnIDThatIsNeitherANumberNorAStringIsUnreadable 盯「不是数字」的那一种。
//
// 当成 0 的话，界面上是那一条被悄悄丢掉（没有身份），而用户看到的是「B 站今天
// 没推东西」——错在哪儿一个字都看不出来。
func TestAnIDThatIsNeitherANumberNorAStringIsUnreadable(t *testing.T) {
	f := newFakeBili(t)
	f.on(musicRankPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"list": []map[string]any{
			{"id": int64(1), "music_title": "x", "bvid": "BV1x", "aid": "不是数字", "cid": int64(1)},
		}}))
	})
	_, err := f.client(t, Options{}).MusicRank(context.Background(), MusicRankQuery{})
	if err == nil {
		t.Fatal("一个读不懂的 id 被当成了 0")
	}
	if !errors.Is(err, errReadRecommend) {
		t.Errorf("错认错了：%v（want errReadRecommend）", err)
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
	// ⚠️ aid / cid 写成**字符串**：这一族的接口真会这么发（同族的
	// `musician/list` 连 uid 都是字符串，见 flexInt64 的注释）。写数字的话这条
	// 用例测的是一个 B 站不发的东西。
	f.on(newMusicPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"list": []map[string]any{
			{"id": int64(5), "music_title": "新歌", "cover": "//i0.hdslb.com/n.jpg",
				"author": "新歌作者", "bvid": "BV1new", "aid": "115182114246511", "cid": "32447137298"},
		}}))
	})
	f.on(newMusicBannerPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"list": []map[string]any{
			{"music_id": "m1", "archive_title": "头图位那首", "cover": "//i0.hdslb.com/b.jpg",
				"author": "头图作者", "bvid": "BV1ban", "aid": "115219862986158", "cid": "115219862986158"},
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
	// 字符串形态的 aid / cid 也要真的落到结构体上。
	if items[0].Aid != 115182114246511 || items[0].Cid != 32447137298 {
		t.Errorf("aid/cid = %d/%d，want 115182114246511/32447137298（真实返回里它们是字符串）",
			items[0].Aid, items[0].Cid)
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
