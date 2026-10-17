package bili

// 这一组测搜索那一层：请求参数照抄上游的那几个、返回怎么读、B 站那些「两种形状」
// 的字段怎么认。fixture 的形状来自上游 `web-interface-search-type.ts` 里那份
// `WebSearchTypeData` / `SearchVideoItem` / `SearchUserItem` 类型定义。
//
// **没有任何一条断言是在证明「B 站现在真这么返回」**，那要靠真机回归。

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

// searchFixture 建一台会回搜索结果的假 B 站，顺手把 WBI 的 key 备好（这个接口要
// 签名，见 search.go 的 SearchVideos）。第三个返回值是录下来的请求。
func searchFixture(t *testing.T, data map[string]any) (*fakeBili, *Client, func() []recordedRequest) {
	t.Helper()
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"wbi_img": map[string]string{
				"img_url": "https://i0.hdslb.com/bfs/wbi/" + sampleImgKey + ".png",
				"sub_url": "https://i0.hdslb.com/bfs/wbi/" + sampleSubKey + ".png",
			},
		}))
	})
	f.on(searchPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", data))
	})
	c := f.client(t, Options{Now: func() time.Time { return time.Unix(1702204169, 0) }})
	return f, c, f.requests
}

// videoItem 造一条视频搜索结果。B 站把命中的词包在 `<em>` 里，pic 没有协议。
func videoItem(bvid, title string) map[string]any {
	return map[string]any{
		"type": 1, "id": 1, "author": "某UP", "mid": 9527,
		"bvid": bvid, "title": title,
		"pic":      "//i2.hdslb.com/bfs/archive/" + bvid + ".jpg",
		"play":     12345,
		"duration": "4:20",
	}
}

func userItem(mid int64, uname string) map[string]any {
	return map[string]any{
		"type": 3, "mid": mid, "uname": uname,
		"usign": "签名", "fans": 100, "level": 5,
		"upic": "//i0.hdslb.com/bfs/face/" + uname + ".jpg",
	}
}

// searchRequest 找出录下来的那一次搜索请求。
func searchRequest(t *testing.T, seen []recordedRequest) recordedRequest {
	t.Helper()
	for _, r := range seen {
		if r.Path == searchPath {
			return r
		}
	}
	t.Fatalf("没有发过 %s，录到的是 %v", searchPath, seen)
	return recordedRequest{}
}

// ─────────────────────────────────────────────────────────────────────────────
// 请求参数
// ─────────────────────────────────────────────────────────────────────────────

// TestSearchVideosSendsWhatBiuSends 盯请求参数和上游逐条一样。上游
// `video-list/index.tsx:33-41` 发的是 search_type=video、keyword、page、
// page_size=24、order，外加「仅音乐分区」时的 tids=3。少一个就是搜出来的东西不一样，
// 而界面上看不出任何异样。
func TestSearchVideosSendsWhatBiuSends(t *testing.T) {
	_, c, seen := searchFixture(t, map[string]any{"numResults": 1, "numPages": 1,
		"result": []any{videoItem("BV1", "歌名")}})

	if _, err := c.SearchVideos(context.Background(), VideoQuery{
		Keyword: "周杰伦", Page: 2, MusicOnly: true, Order: VideoOrderClick,
	}); err != nil {
		t.Fatalf("SearchVideos: %v", err)
	}

	q := searchRequest(t, seen()).Query
	for name, want := range map[string]string{
		"search_type": "video",
		"keyword":     "周杰伦",
		"page":        "2",
		"page_size":   "24",
		"order":       "click",
		"tids":        "3",
	} {
		if got := q.Get(name); got != want {
			t.Errorf("%s = %q，want %q（发出去的是 %v）", name, got, want, q)
		}
	}
	// 签名要带：w_rid / wts 是 wbi 那一步加上去的。
	if q.Get("w_rid") == "" || q.Get("wts") == "" {
		t.Errorf("请求没有 WBI 签名（w_rid/wts 缺一个）：%v", q)
	}
}

// TestMusicOnlyOffAsksForTheWholeSite 是「歌曲」和「视频」那一个参数的差别。
//
// 「歌曲」Tab 带 tids=3、「视频」Tab 不带（见 searchresults.go 文件头）。
// 少了这一支，两个 Tab 会搜出完全一样的东西。
func TestMusicOnlyOffAsksForTheWholeSite(t *testing.T) {
	_, c, seen := searchFixture(t, map[string]any{"numResults": 0, "result": []any{}})

	if _, err := c.SearchVideos(context.Background(), VideoQuery{Keyword: "x", Page: 1}); err != nil {
		t.Fatalf("SearchVideos: %v", err)
	}
	if got := searchRequest(t, seen()).Query.Get("tids"); got != "" {
		t.Errorf("不限音乐分区时还是发了 tids=%q", got)
	}
}

// TestSearchUsersSendsOrderAndOrderSort 盯用户那一档的两个参数（上游
// `user-list/utils.ts` 的 `getSortParams`）。只发 order 不发 order_sort，
// 「粉丝数由低到高」会变成由高到低——而界面上看不出异样。
func TestSearchUsersSendsOrderAndOrderSort(t *testing.T) {
	_, c, seen := searchFixture(t, map[string]any{"numResults": 1, "numPages": 1,
		"result": []any{userItem(7, "某人")}})

	if _, err := c.SearchUsers(context.Background(), UserQuery{
		Keyword: "歌名", Page: 1, Sort: UserSortFansAsc,
	}); err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}

	q := searchRequest(t, seen()).Query
	for k, want := range map[string]string{
		"search_type": "bili_user",
		"keyword":     "歌名",
		"page":        "1",
		"page_size":   "24",
		"order":       "fans",
		"order_sort":  "1",
	} {
		if got := q.Get(k); got != want {
			t.Errorf("%s = %q，want %q", k, got, want)
		}
	}
	// 用户那一档没有分区这个概念（上游的请求里也没有 tids）。
	if got := q.Get("tids"); got != "" {
		t.Errorf("用户搜索里发了 tids=%q", got)
	}
}

// TestUserDefaultSortSendsZeroNotNothing 盯「默认排序」那一档：上游发的是数字 0
// （`utils.ts:15`），拼进查询串是 `order=0`。发空串会被 B 站当成「没给这个参数」。
func TestUserDefaultSortSendsZeroNotNothing(t *testing.T) {
	_, c, seen := searchFixture(t, map[string]any{"result": []any{}})

	if _, err := c.SearchUsers(context.Background(), UserQuery{Keyword: "x", Page: 1}); err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if got := searchRequest(t, seen()).Query.Get("order"); got != "0" {
		t.Errorf("默认排序发的 order=%q，want \"0\"（上游是数字 0）", got)
	}
}

// TestOrderDefaultsToTotalRank 盯零值排序。
func TestOrderDefaultsToTotalRank(t *testing.T) {
	if got := orderOf(""); got != VideoOrderTotalRank {
		t.Errorf("零值排序变成了 %q，want %q", got, VideoOrderTotalRank)
	}
}

// TestPageIsClampedToOneWhenItIsZero 盯页码：零值不是「第一页」而是「没给」——B 站对
// page=0 回的是一份空结果，页面上就成了「搜了但什么都没有」，而用户明明什么都没做错。
func TestPageIsClampedToOneWhenItIsZero(t *testing.T) {
	_, c, seen := searchFixture(t, map[string]any{"result": []any{}})

	if _, err := c.SearchVideos(context.Background(), VideoQuery{Keyword: "x", Page: 0}); err != nil {
		t.Fatalf("SearchVideos: %v", err)
	}
	if got := searchRequest(t, seen()).Query.Get("page"); got != "1" {
		t.Errorf("page=0 发出去是 %q，want \"1\"", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 读返回
// ─────────────────────────────────────────────────────────────────────────────

// TestSearchVideosReadsTheResult 是「返回读对了」那一条：标题剥掉 `<em>`（不然界面上
// 是一排尖括号）、封面补上协议（不然是永远转圈的方块）、时长认得出「4:20」。
func TestSearchVideosReadsTheResult(t *testing.T) {
	_, c, _ := searchFixture(t, map[string]any{"numResults": 1, "numPages": 1,
		"result": []any{videoItem("BV1MbznYoEk1", "周杰伦<em class=\"keyword\">晴天</em>live")}})

	pg, err := c.SearchVideos(context.Background(), VideoQuery{Keyword: "晴天", Page: 1})
	if err != nil {
		t.Fatalf("SearchVideos: %v", err)
	}
	if len(pg.Hits) != 1 {
		t.Fatalf("读出 %d 条，want 1", len(pg.Hits))
	}
	h := pg.Hits[0]
	if h.Title != "周杰伦晴天live" {
		t.Errorf("标题 %q，没剥掉 <em> 高亮", h.Title)
	}
	if h.Cover != "https://i2.hdslb.com/bfs/archive/BV1MbznYoEk1.jpg" {
		t.Errorf("封面 %q，没补协议头", h.Cover)
	}
	if h.Owner != "某UP" || h.Mid != 9527 {
		t.Errorf("UP 名/mid 读错了：%q / %d", h.Owner, h.Mid)
	}
	if h.Duration != 4*time.Minute+20*time.Second {
		t.Errorf("时长 %v，want 4m20s", h.Duration)
	}
	if h.Play != 12345 {
		t.Errorf("播放量 %d，want 12345", h.Play)
	}
	if pg.HasMore {
		t.Errorf("只有 1 条却说有下一页")
	}
}

// TestSearchUsersReadsTheResult 盯用户那几项。
func TestSearchUsersReadsTheResult(t *testing.T) {
	_, c, _ := searchFixture(t, map[string]any{"numResults": 1, "numPages": 1,
		"result": []any{userItem(9527, "周杰伦")}})

	pg, err := c.SearchUsers(context.Background(), UserQuery{Keyword: "周", Page: 1})
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(pg.Hits) != 1 {
		t.Fatalf("读出 %d 条，want 1", len(pg.Hits))
	}
	h := pg.Hits[0]
	if h.Mid != 9527 || h.Name != "周杰伦" || h.Sign != "签名" || h.Fans != 100 || h.Level != 5 {
		t.Errorf("读错了：%+v", h)
	}
	if h.Avatar != "https://i0.hdslb.com/bfs/face/周杰伦.jpg" {
		t.Errorf("头像 %q，没补协议头", h.Avatar)
	}
}

// TestHitsWithoutAnIdentityAreDropped 盯「没有身份的行不许进列表」：🔴 铁律四要行身份
// **稳定且唯一**，空 bvid / mid=0 落进列表就是两行共用一份状态——选中一个另一个也
// 亮着，而界面上看不出任何异样。
func TestHitsWithoutAnIdentityAreDropped(t *testing.T) {
	noBvid := videoItem("BV1", "好的")
	delete(noBvid, "bvid")
	noMid := userItem(0, "某人")

	_, c, _ := searchFixture(t, map[string]any{"numResults": 9, "numPages": 9,
		"result": []any{noBvid, videoItem("BV2", "另一首"), noBvid}})

	pg, err := c.SearchVideos(context.Background(), VideoQuery{Keyword: "x", Page: 1})
	if err != nil {
		t.Fatalf("SearchVideos: %v", err)
	}
	if len(pg.Hits) != 1 || pg.Hits[0].Bvid != "BV2" {
		t.Fatalf("读出 %+v，want 只留 BV2 那一首", pg.Hits)
	}
	// 一页空 + 还有总数 → 不许说「还有」，不然就是一个永远转圈的自动续拉。
	if pg.HasMore {
		t.Errorf("这一页一条都没有却说还有下一页")
	}

	_, c2, _ := searchFixture(t, map[string]any{"numResults": 1, "result": []any{noMid}})
	up, err := c2.SearchUsers(context.Background(), UserQuery{Keyword: "x", Page: 1})
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(up.Hits) != 0 {
		t.Errorf("mid=0 的用户没被丢掉：%+v", up.Hits)
	}
}

// TestSearchReportsBiliErrorCodes 盯 B 站的业务错误没被吞掉。风控（-412）这一档必须
// 原样浮上去：`app` 那层靠 `errors.Is(err, ErrRiskControl)` 判断该不该提示用户，吞掉
// 它就变成一句看不懂的「加载失败」。
func TestSearchReportsBiliErrorCodes(t *testing.T) {
	f := newFakeBili(t)
	f.on(searchPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, -412, "请求被拦截", nil))
	})
	c := f.client(t, Options{Now: func() time.Time { return time.Unix(1702204169, 0) }})
	// WBI 的 key 要先拿到，发不发得出去都无所谓——这里要的是业务错误那一步。
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"wbi_img": map[string]string{
				"img_url": "https://i0.hdslb.com/bfs/wbi/" + sampleImgKey + ".png",
				"sub_url": "https://i0.hdslb.com/bfs/wbi/" + sampleSubKey + ".png",
			},
		}))
	})

	_, err := c.SearchVideos(context.Background(), VideoQuery{Keyword: "x", Page: 1})
	if err == nil {
		t.Fatalf("风控被吞掉了")
	}
	if !errors.Is(err, ErrRiskControl) {
		t.Errorf("err = %v，want 一个认得出是风控的错", err)
	}
}

// TestHasMoreStopsAtTheLastPage 盯翻到底就停手。
func TestHasMoreStopsAtTheLastPage(t *testing.T) {
	const total = 30 // 两页（24 + 6）
	for _, c := range []struct {
		page int
		want bool
	}{
		{1, true}, {2, false}, {3, false},
	} {
		if got := searchHasMore(c.page, searchPageSize, total); got != c.want {
			t.Errorf("第 %d 页 hasMore = %v，want %v", c.page, got, c.want)
		}
	}
}

// TestHasMoreStopsAtThePageCap 盯页码上限也算到底（评审的 major 那一条）。
//
// `pageOf` 把第 101 页夹成第 100 页，于是 B 站每次都回**同一份**末页数据。只按
// 「总数」判的话这一页永远算「还有」——翻页列表就一直拉第 100 页，一条新的都拿不到，
// 而每一次都是一次带 WBI 签名的往返（评审实测：传 100/101/102 三趟，录到的请求全是
// page=100，三次 HasMore 都是 true）。
func TestHasMoreStopsAtThePageCap(t *testing.T) {
	const total = 10000 // B 站报的总数：100 页 × 100 条，远超一页 24 条
	// 还没到上限的那些照旧说「还有」——第 99 页之后还有第 100 页，那一页是真的新数据。
	if !searchHasMore(99, searchPageSize, total) {
		t.Errorf("第 99 页就说到底了，第 100 页还拿得到新数据")
	}
	for _, page := range []int{100, 101, 102} {
		if searchHasMore(page, searchPageSize, total) {
			t.Errorf("第 %d 页说还有下一页（夹到上限之后拿回来的还是它）", page)
		}
	}
}

// TestPagesAboveTheCapAllAskForTheSameOne 钉住「夹页码」这一支本身：传 100/101/102
// 三趟，录到的请求必须**都是** page=100。
//
// 这条不是重复上一条：上一条钉的是「别再说还有」，这一条钉的是「夹了要真的夹」。
func TestPagesAboveTheCapAllAskForTheSameOne(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"wbi_img": map[string]string{
				"img_url": "https://i0.hdslb.com/bfs/wbi/" + sampleImgKey + ".png",
				"sub_url": "https://i0.hdslb.com/bfs/wbi/" + sampleSubKey + ".png",
			},
		}))
	})
	f.on(searchPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"numResults": 10000, "numPages": 100,
			"result": []any{videoItem("BV1", "末页"), videoItem("BV2", "末页第二首")},
		}))
	})
	c := f.client(t, Options{Now: func() time.Time { return time.Unix(1702204169, 0) }})

	for _, page := range []int{100, 101, 102} {
		if _, err := c.SearchVideos(context.Background(), VideoQuery{Keyword: "x", Page: page}); err != nil {
			t.Fatalf("第 %d 页：%v", page, err)
		}
	}
	n := 0
	for _, r := range f.requests() {
		if r.Path != searchPath {
			continue
		}
		n++
		if got := r.Query.Get("page"); got != "100" {
			t.Errorf("第 %d 趟请求发的 page=%q，want 100", n, got)
		}
	}
	if n != 3 {
		t.Errorf("录到 %d 趟搜索请求，want 3", n)
	}
}

// TestAnEmptyPageAlwaysEndsIt 盯「一页空的还说还有」：B 站在风控边缘偶尔会回
// `numResults: 9999` 而 `result` 是空数组，没有这条判据就一直拉下去。
func TestAnEmptyPageAlwaysEndsIt(t *testing.T) {
	if searchHasMore(1, 0, 9999) {
		t.Errorf("一页空的还说还有下一页")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 字段形状
// ─────────────────────────────────────────────────────────────────────────────

// TestStripHTML 盯剥标签。两种边界各错一次的后果都不小：漏剥一整页标题上都是尖括号；
// 把**不成对**的「<」也当标签吃掉，「a < b」就变成了「a」。
func TestStripHTML(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"普通标题", "普通标题"},
		{`<em class="keyword">晴天</em>`, "晴天"},
		{"周杰伦<em class=\"keyword\">晴天</em>现场版", "周杰伦晴天现场版"},
		{"a < b", "a < b"},
		{"a > b", "a > b"},
		{"<em>没闭上", "没闭上"},
		{"", ""},
	} {
		if got := stripHTML(c.in); got != c.want {
			t.Errorf("stripHTML(%q) = %q，want %q", c.in, got, c.want)
		}
	}
}

// TestParseDuration 盯 B 站的两种时长形状。认不出来时给零值而不是猜：猜出来的时长会
// 跟着播放条一起骗人。
func TestParseDuration(t *testing.T) {
	for _, c := range []struct {
		in   string
		want time.Duration
	}{
		{`260`, 260 * time.Second},
		{`260.5`, 260*time.Second + 500*time.Millisecond},
		{`"4:20"`, 4*time.Minute + 20*time.Second},
		{`"1:02:03"`, time.Hour + 2*time.Minute + 3*time.Second},
		{`""`, 0},
		{`null`, 0},
		{`"abc"`, 0},
		{`"4"`, 0},
		{`"-5"`, 0},
	} {
		if got := parseDuration([]byte(c.in)); got != c.want {
			t.Errorf("parseDuration(%s) = %v，want %v", c.in, got, c.want)
		}
	}
}

// TestNormalizeImageURL 盯补协议头：B 站回的是 `//i2.hdslb.com/...`，少这一步的表现
// 是封面永远转圈。
func TestNormalizeImageURL(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"//i2.hdslb.com/a.jpg", "https://i2.hdslb.com/a.jpg"},
		{"https://i2.hdslb.com/a.jpg", "https://i2.hdslb.com/a.jpg"},
		{"http://i2.hdslb.com/a.jpg", "http://i2.hdslb.com/a.jpg"},
		{"i2.hdslb.com/a.jpg", "https://i2.hdslb.com/a.jpg"},
		{"", ""},
	} {
		if got := normalizeImageURL(c.in); got != c.want {
			t.Errorf("normalizeImageURL(%q) = %q，want %q", c.in, got, c.want)
		}
	}
}

// TestParseCid 盯那个「大多数情况是空的」分 P id：0 必须变成**空串**而不是 "0"——传
// 下去的是一个真的分 P id，取回来的是别人的集，而界面上什么异样都看不出来。
func TestParseCid(t *testing.T) {
	for _, c := range []struct {
		in   int64
		want string
	}{{29165486316, "29165486316"}, {0, ""}, {-1, ""}} {
		if got := parseCid(c.in); got != c.want {
			t.Errorf("parseCid(%d) = %q，want %q", c.in, got, c.want)
		}
	}
}

// TestSearchVideoHitsCarryNoCidWhenBiliSendsNone 把「搜索结果没有分 P id」钉在用例
// 里。它是**已知**的限制：搜出来的歌现在放不了（`app.playableOnline` 会拒掉并说清为
// 什么）。钉住它是为了以后有人补上分 P 解析时，这一处从「空」变成「有值」是**有意的
// 改动**，而不是「B 站突然给了」这种说不清的偶然。
func TestSearchVideoHitsCarryNoCidWhenBiliSendsNone(t *testing.T) {
	_, c, _ := searchFixture(t, map[string]any{"numResults": 1,
		"result": []any{videoItem("BV1", "一首")}})

	pg, err := c.SearchVideos(context.Background(), VideoQuery{Keyword: "x", Page: 1})
	if err != nil {
		t.Fatalf("SearchVideos: %v", err)
	}
	if got := pg.Hits[0].Cid; got != "" {
		t.Errorf("B 站没给 cid，这一层却读出了 %q", got)
	}
}
