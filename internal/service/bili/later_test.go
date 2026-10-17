package bili

// 「稍后再看」这一组盯的是三件事：
//
//  1. **参数真的发出去了**（pn / ps / key / add_time_*）。少一个的症状是「日期
//     筛选点下去没反应」，而那在界面上和「B 站忽略了它」长得一模一样。
//  2. **WBI 签名带上了**。这个接口上游签了名（`useWbi: true`）；不签的表现是
//     被风控挡掉，而挡掉时报的是 -403，跟「没权限」一模一样。
//  3. **时间单位是秒不是毫秒**。差 1000 倍，表现为「日期筛选永远查不到东西」
//     而且不报错——只有把查询串录下来比对才看得出来。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// signedFake 是**带 WBI 签名**的假 B 站。
//
// 「稍后再看」这个接口要签名（Biu 的 `useWbi: true`），签名之前要先打一次 nav
// 拿密钥——nav 打不通的话签名这步直接就报错了，连真正的请求都发不出去。所以
// 这一组每一条都得用这个，而不是 newFakeBili。
func signedFake(t *testing.T) *fakeBili {
	t.Helper()
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"isLogin": true,
			"wbi_img": map[string]string{
				"img_url": "https://i0.hdslb.com/bfs/wbi/" + sampleImgKey + ".png",
				"sub_url": "https://i0.hdslb.com/bfs/wbi/" + sampleSubKey + ".png",
			},
		}))
	})
	return f
}

// toViewFixture 是一份「稍后再看」的返回。B 站给的是秒，这里原样给秒。
func toViewFixture(t *testing.T, count, n int) string {
	t.Helper()
	items := make([]map[string]any, 0, n)
	for i := range n {
		items = append(items, map[string]any{
			"aid": int64(170000 + i), "bvid": "BV1xx411c7m0A", "cid": int64(3000 + i),
			"title": "稍后再看第 " + string(rune('1'+i)) + " 条", "pic": "http://h/p" + string(rune('1'+i)) + ".jpg",
			"duration": int64(185 + i), "progress": int64(60 * i), "pubdate": int64(1700000000), "add_at": int64(1700000100),
			"is_pgc": i == 1,
			"owner":  map[string]any{"mid": int64(900 + i), "name": "UP " + string(rune('A'+i))},
			"stat":   map[string]any{"view": int64(12345 * (i + 1))},
		})
	}
	raw, err := json.Marshal(map[string]any{"count": count, "list": items})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestToViewListSendsEveryFilter 盯参数和签名。
func TestToViewListSendsEveryFilter(t *testing.T) {
	f := signedFake(t)
	f.on(toViewListPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", json.RawMessage(toViewFixture(t, 40, 2))))
	})
	c := f.client(t, Options{})

	from := time.Unix(1_700_000_000, 0)
	to := time.Unix(1_700_086_400, 0)
	if _, err := c.ToViewList(context.Background(), ToViewQuery{
		Page: 2, PageSize: 20, Keyword: "周杰伦", From: from, To: to,
	}); err != nil {
		t.Fatal(err)
	}

	var got recordedRequest
	for _, rec := range f.requests() {
		if rec.Path == toViewListPath {
			got = rec
		}
	}
	if got.Path == "" {
		t.Fatal("没有发出稍后再看的请求")
	}
	for _, q := range []struct{ key, want string }{
		{"pn", "2"}, {"ps", "20"}, {"key", "周杰伦"},
		{"add_time_start", "1700000000"}, {"add_time_end", "1700086400"},
	} {
		if v := got.Query.Get(q.key); v != q.want {
			t.Errorf("%s = %q，want %q（查询串：%v）", q.key, v, q.want, got.Query)
		}
	}
	if got.Query.Get("w_rid") == "" {
		t.Error("请求没有 WBI 签名——上游这个接口签了名，不签会被风控挡成 -403")
	}
}

// TestToViewPageSizeIsClamped 钉住一页最多 50 条。
//
// 不夹的话 B 站会自己降回去，而那是**静默**的：界面照样拉，页数比想的少一截。
func TestToViewPageSizeIsClamped(t *testing.T) {
	f := signedFake(t)
	f.on(toViewListPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", json.RawMessage(toViewFixture(t, 1, 1))))
	})
	c := f.client(t, Options{})

	if _, err := c.ToViewList(context.Background(), ToViewQuery{Page: 1, PageSize: 999}); err != nil {
		t.Fatal(err)
	}
	for _, rec := range f.requests() {
		if rec.Path == toViewListPath && rec.Query.Get("ps") != "50" {
			t.Errorf("ps = %q，want 50", rec.Query.Get("ps"))
		}
	}
}

// TestToViewPageIsParsed 盯解出来的那一行对不对。
func TestToViewPageIsParsed(t *testing.T) {
	f := signedFake(t)
	f.on(toViewListPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", json.RawMessage(toViewFixture(t, 40, 2))))
	})
	c := f.client(t, Options{})

	pg, err := c.ToViewList(context.Background(), ToViewQuery{Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if pg.Count != 40 {
		t.Errorf("count = %d，want 40", pg.Count)
	}
	if len(pg.Items) != 2 {
		t.Fatalf("解出来 %d 条，want 2", len(pg.Items))
	}
	got := pg.Items[0]
	// 秒换算成 Duration：185 秒 = 3m05s。画出来错一位是「1850000000ns」那种。
	if want := 185 * time.Second; got.Duration != want {
		t.Errorf("时长 %v，want %v", got.Duration, want)
	}
	if got.OwnerName != "UP A" || got.OwnerMid != 900 {
		t.Errorf("UP = %q/%d，want UP A/900", got.OwnerName, got.OwnerMid)
	}
	if got.View != 12345 {
		t.Errorf("播放量 %d，want 12345", got.View)
	}
	if got.Bvid != "BV1xx411c7m0A" || got.Cid != 3000 {
		t.Errorf("bvid/cid = %q/%d", got.Bvid, got.Cid)
	}
	// 第二个是 PGC——界面上据此不给「播放」。
	if !pg.Items[1].IsPGC {
		t.Error("第二条是 PGC（is_pgc=true），解出来却不是")
	}
}

// TestToViewNegativeNumbersBecomeZero 盯「B 站给 -1」这种值。
//
// duration 有时是 -1（未知）。原样透出去的话界面会画出「-1s」，那不是「不知道」
// 那一支，是画错了。
func TestToViewNegativeNumbersBecomeZero(t *testing.T) {
	f := signedFake(t)
	f.on(toViewListPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"count": 1,
			"list": []map[string]any{{
				"aid": 1, "bvid": "BV1", "title": "x",
				"duration": -1, "progress": -1, "pubdate": 0, "add_at": 0,
				"owner": map[string]any{"mid": 1, "name": "u"},
				"stat":  map[string]any{"view": 0},
			}},
		}))
	})
	c := f.client(t, Options{})

	pg, err := c.ToViewList(context.Background(), ToViewQuery{Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	it := pg.Items[0]
	if it.Duration != 0 || it.Progress != 0 {
		t.Errorf("时长/进度 = %v/%v，want 0/0", it.Duration, it.Progress)
	}
	if !it.PubDate.IsZero() || !it.AddAt.IsZero() {
		t.Errorf("时间没夹成零值：%v / %v", it.PubDate, it.AddAt)
	}
}

// TestToViewRemovePostsTheAid 盯移除：POST、带上 csrf、body 里是 aid。
func TestToViewRemovePostsTheAid(t *testing.T) {
	f := signedFake(t)
	f.on(toViewDelPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", nil))
	})
	c := f.client(t, Options{CSRF: func() string { return "the-jct" }})

	if err := c.ToViewRemove(context.Background(), 170001); err != nil {
		t.Fatal(err)
	}
	var got recordedRequest
	for _, rec := range f.requests() {
		if rec.Path == toViewDelPath {
			got = rec
		}
	}
	if got.Path == "" {
		t.Fatal("没有发出移除的请求")
	}
	// 断言 **body 原文**。`Client.Do` 现在给表单请求带
	// `Content-Type: application/x-www-form-urlencoded`（那是对着 B 站 servlet
	// 修的根因：少了它 B 站不解析请求体，「移除」会**静默失败**），而
	// `r.ParseForm()` 遇到能解析的表单会把 body 读空 —— 假服务器那一侧
	// （`client_test.go` 的 `newFakeBili`）因此**先读原文、再装回去、最后
	// ParseForm**，两样都留得住。所以这里既可以断 `rec.Body` 也可以断
	// `rec.Form`；断原文是为了顺带钉住「它确实是用表单发出去的」。
	if !strings.Contains(got.Body, "aid=170001") {
		t.Errorf("body 里没有 aid=170001，原文是 %q", got.Body)
	}
	if got.Header.Get(CSRFHeaderName) != "the-jct" {
		t.Errorf("没有带 CSRF 头（%s），B 站会回 -111", CSRFHeaderName)
	}
}

// TestToViewNotLoggedIn 盯 -101 走出去之后还是那个可判的错。
//
// 界面上靠 `errors.Is(err, ErrNotLoggedIn)` 说「重新登录」，认不出来就退成
// 「连不上 B 站」——用户会去检查网络，而真正的原因是登录态没了。
func TestToViewNotLoggedIn(t *testing.T) {
	f := signedFake(t)
	f.on(toViewListPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, -101, "账号未登录", nil))
	})
	c := f.client(t, Options{})

	_, err := c.ToViewList(context.Background(), ToViewQuery{Page: 1})
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("err = %v，want ErrNotLoggedIn", err)
	}
	if msg := Friendly(err); msg != codeMessages[-101] {
		t.Errorf("给用户看的话是 %q，want %q", msg, codeMessages[-101])
	}
}

// TestToViewUnreadableDataIsItsOwnError 盯「200 + code 0 但 data 变了」。
//
// 这不是网络错，重试没有意义；说成网络错会让用户白折腾（换网、清缓存、等）。
func TestToViewUnreadableDataIsItsOwnError(t *testing.T) {
	f := signedFake(t)
	f.on(toViewListPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", "这不是对象"))
	})
	c := f.client(t, Options{})

	_, err := c.ToViewList(context.Background(), ToViewQuery{Page: 1})
	if !errors.Is(err, errReadLater) {
		t.Fatalf("err = %v，want errReadLater", err)
	}
	if errors.Is(err, ErrNetwork) || errors.Is(err, ErrRiskControl) {
		t.Error("data 读不懂被说成了网络/风控——用户会去换网，白折腾")
	}
}
