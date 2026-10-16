package bili

// 「历史记录」这一组盯的是四件事：
//
//  1. **参数**（pn / keyword / business / add_time_*）。少一个症状同上：筛不动。
//  2. **keyword 不是 key**。稍后再看是 `key`、历史记录是 `keyword`，写错的话
//     筛选框永远筛不出东西，**而且不报错**——只有把查询串录下来才看得出来。
//  3. **不带 WBI 签名**。上游没签；多签一次是白白的 nav 往返，而签名失败时报的
//     是 -403「访问权限不足」，和真的没权限一模一样。
//  4. **kid 拼得对**。删除接口收的是 `{业务}_{目标}`，拿 bvid 去删不会报错——
//     B 站回 code 0，什么都没删掉，而用户以为删掉了。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// historyFixture 造一份历史记录的返回。n 条，每条的 bvid 一样、kid 不一样
// ——**这正是真实情况**：同一首歌看过两次在 B 站是两条记录。
func historyFixture(t *testing.T, n int, hasMore bool) string {
	t.Helper()
	items := make([]map[string]any, 0, n)
	for i := range n {
		items = append(items, map[string]any{
			"title": "听过第 " + string(rune('1'+i)) + " 个", "cover": "http://h/c.jpg",
			"author_name": "UP " + string(rune('A'+i)), "author_mid": 900 + i,
			"view_at":  int64(1_700_000_000 + i*3600),
			"progress": int64(754 + i), "duration": int64(2700),
			"kid": "archive_" + string(rune('a'+i)),
			"history": map[string]any{
				"bvid": "BV1xx411c7m0B", "cid": 4000 + i, "business": "archive",
			},
		})
	}
	raw, err := json.Marshal(map[string]any{
		"has_more": hasMore,
		"page":     map[string]any{"pn": 1, "total": 9},
		"list":     items,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestHistorySearchSendsEveryFilter 盯参数（重点是 keyword 不是 key）。
func TestHistorySearchSendsEveryFilter(t *testing.T) {
	f := newFakeBili(t)
	f.on(historySearchPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", json.RawMessage(historyFixture(t, 1, true))))
	})
	c := f.client(t, Options{})

	_, err := c.HistorySearch(context.Background(), HistoryQuery{
		Page: 3, Keyword: "林俊杰",
		From: time.Unix(1_700_000_000, 0), To: time.Unix(1_700_086_400, 0),
	})
	if err != nil {
		t.Fatal(err)
	}

	var got recordedRequest
	for _, rec := range f.requests() {
		if rec.Path == historySearchPath {
			got = rec
		}
	}
	if got.Path == "" {
		t.Fatal("没有发出历史记录的请求")
	}
	for _, q := range []struct{ key, want string }{
		{"pn", "3"}, {"business", "archive"}, {"keyword", "林俊杰"},
		{"add_time_start", "1700000000"}, {"add_time_end", "1700086400"},
	} {
		if v := got.Query.Get(q.key); v != q.want {
			t.Errorf("%s = %q，want %q（查询串：%v）", q.key, v, q.want, got.Query)
		}
	}
	if got.Query.Get("key") != "" {
		t.Error("发了 key——这个接口的参数名是 keyword，发错了筛选框就永远筛不动")
	}
}

// TestHistorySearchIsNotSigned 钉住「这个接口不签 WBI」。
//
// 签了会多一次 nav 往返，而签名失败时的 -403 跟真的没权限分不开。判据是
// 「请求里没有 w_rid」。
func TestHistorySearchIsNotSigned(t *testing.T) {
	f := newFakeBili(t)
	f.on(historySearchPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", json.RawMessage(historyFixture(t, 1, false))))
	})
	c := f.client(t, Options{})

	if _, err := c.HistorySearch(context.Background(), HistoryQuery{Page: 1}); err != nil {
		t.Fatal(err)
	}
	for _, rec := range f.requests() {
		if rec.Path == historySearchPath && rec.Query.Get("w_rid") != "" {
			t.Error("历史记录这个接口上游没签 WBI，这里多签了一次 nav 往返")
		}
	}
}

// TestHistoryPageIsParsed 盯解出来的那一行，尤其是播放进度（这一页存在的理由）。
func TestHistoryPageIsParsed(t *testing.T) {
	f := newFakeBili(t)
	f.on(historySearchPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", json.RawMessage(historyFixture(t, 2, true))))
	})
	c := f.client(t, Options{})

	pg, err := c.HistorySearch(context.Background(), HistoryQuery{Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !pg.HasMore {
		t.Error("has_more = false，接口给的是 true")
	}
	got := pg.Items[0]
	if want := 754 * time.Second; got.Progress != want {
		t.Errorf("播放进度 %v，want %v", got.Progress, want)
	}
	if want := 45 * time.Minute; got.Duration != want {
		t.Errorf("总时长 %v，want %v", got.Duration, want)
	}
	if got.Kid != "archive_a" {
		t.Errorf("kid = %q，want archive_a", got.Kid)
	}
	if got.Bvid != "BV1xx411c7m0B" || got.AuthorName != "UP A" {
		t.Errorf("bvid/UP = %q/%q", got.Bvid, got.AuthorName)
	}
	if want := time.Unix(1_700_000_000, 0); !got.ViewAt.Equal(want) {
		t.Errorf("观看时间 %v，want %v", got.ViewAt, want)
	}
	// 两条记录的 bvid 一样、kid 不一样——这是「同一首歌看过两次」的真实形状。
	if pg.Items[0].Kid == pg.Items[1].Kid {
		t.Fatal("两条记录的 kid 一样，测试的 fixture 不对")
	}
}

// TestHistoryKidIsBuiltWhenBiliOmitsIt 盯 kid 缺失时怎么拼。
//
// B 站的 kid 有时是数字、有时是字符串、有时候干脆没有。三种都得拼出一份能删的，
// 拼不出来就该说「删不了」——发一个错的上去，B 站回 0（成功）而什么都没删。
func TestHistoryKidIsBuiltWhenBiliOmitsIt(t *testing.T) {
	for _, c := range []struct {
		name string
		raw  any
		want string
	}{
		{"数字", float64(170001), "170001"},
		{"字符串", "archive_999", "archive_999"},
		{"没有", nil, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeBili(t)
			f.on(historySearchPath, func(w http.ResponseWriter, _ *http.Request) {
				item := map[string]any{
					"title": "x", "history": map[string]any{
						"bvid": "BV1xx411c7m0C", "cid": 1, "business": "archive",
					},
				}
				if c.raw != nil {
					item["kid"] = c.raw
				}
				writeJSON(w, jsonData(t, 0, "0", map[string]any{
					"has_more": false, "list": []map[string]any{item},
				}))
			})
			cli := f.client(t, Options{})

			pg, err := cli.HistorySearch(context.Background(), HistoryQuery{Page: 1})
			if err != nil {
				t.Fatal(err)
			}
			if got := pg.Items[0].Kid; got != c.want {
				t.Errorf("kid = %q，want %q", got, c.want)
			}
		})
	}
}

// TestHistoryKidIsEmptyWhenNothingIdentifiesIt 盯「拼不出来就是空串」。
func TestHistoryKidIsEmptyWhenNothingIdentifiesIt(t *testing.T) {
	f := newFakeBili(t)
	f.on(historySearchPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"has_more": false,
			"list": []map[string]any{{
				"title": "一场直播", "history": map[string]any{"business": "live"},
			}},
		}))
	})
	c := f.client(t, Options{})

	pg, err := c.HistorySearch(context.Background(), HistoryQuery{Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got := pg.Items[0].Kid; got != "" {
		t.Errorf("kid = %q，want 空串（界面上据此不给「删除」这一项）", got)
	}
}

// TestHistoryDeleteAndClear 盯两个写接口：POST、CSRF、body。
func TestHistoryDeleteAndClear(t *testing.T) {
	f := newFakeBili(t)
	f.on(historyDeletePath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", 0))
	})
	f.on(historyClearPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", 0))
	})
	c := f.client(t, Options{CSRF: func() string { return "the-jct" }})

	if err := c.HistoryDelete(context.Background(), "archive_170001"); err != nil {
		t.Fatal(err)
	}
	if err := c.HistoryClear(context.Background()); err != nil {
		t.Fatal(err)
	}
	// ⚠️ 断言 **body 原文**而不是 `rec.Form`：bili.Client 发表单时不带
	// Content-Type，假服务器那边的 `ParseForm` 解析不出 body。
	seen := map[string]string{}
	for _, rec := range f.requests() {
		switch rec.Path {
		case historyDeletePath, historyClearPath:
			seen[rec.Path] = rec.Body
			if rec.Header.Get(CSRFHeaderName) != "the-jct" {
				t.Errorf("%s 没带 CSRF 头，B 站会回 -111", rec.Path)
			}
		}
	}
	if !strings.Contains(seen[historyDeletePath], "kid=archive_170001") {
		t.Errorf("删除的 body 里没有 kid，原文是 %q", seen[historyDeletePath])
	}
	if _, ok := seen[historyClearPath]; !ok {
		t.Error("没有发出清空的请求")
	}
}

// TestHistoryUnreadableDataIsItsOwnError 盯 200 + code 0 但 data 变了。
func TestHistoryUnreadableDataIsItsOwnError(t *testing.T) {
	f := newFakeBili(t)
	f.on(historySearchPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", "这不是对象"))
	})
	c := f.client(t, Options{})

	_, err := c.HistorySearch(context.Background(), HistoryQuery{Page: 1})
	if !errors.Is(err, errReadHistory) {
		t.Fatalf("err = %v，want errReadHistory", err)
	}
}
