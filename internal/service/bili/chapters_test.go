package bili

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// 这一组测的是「投稿的章节」是怎么从播放器信息接口里读出来的。
//
// # 素材是照着上游定义的样子造的
//
// `view_points` 挂在 `/x/player/wbi/v2` 的 `data` 上（上游
// `src/service/web-player.ts` 的 `WebPlayerViewPoint`：`from` 是开始秒、`to`
// 是结束秒、`content` 是章节名）。上游只定义了类型没有渲染方，所以这一组
// 证明的是**我们读得懂这一段**，不是「B 站真的这么给」——后者照旧只能真机回归。

// chaptersClient 建一台会回指定播放器信息响应的假 B 站。
func chaptersClient(t *testing.T, body string) (*Client, *fakeBili) {
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
	f.on("/x/player/wbi/v2", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, body)
	})
	return f.client(t, Options{Now: func() time.Time { return time.Unix(1702204169, 0) }}), f
}

// playerInfoEnvelope 造一份播放器信息的外壳（不走 jsonData：表里的素材是
// 常量，造它们的时候还没有测试在跑）。
func playerInfoEnvelope(data map[string]any) string {
	b, err := json.Marshal(map[string]any{"code": 0, "message": "0", "ttl": 1, "data": data})
	if err != nil {
		panic(err)
	}
	return string(b)
}

// playerInfoWithChapters 造一份带章节的播放器信息响应。points 里给的是
// 「秒 → 原样」的行，包括 B 站不该给、但我们得扛住的坏行。
func playerInfoWithChapters(points []map[string]any) string {
	return playerInfoEnvelope(map[string]any{
		"bvid":        "BV1",
		"cid":         1,
		"view_points": points,
	})
}

func TestChaptersParseViewPoints(t *testing.T) {
	c, _ := chaptersClient(t, playerInfoWithChapters([]map[string]any{
		// 🔴 **乱序 + 坏行混在一起**：B 站一般给的是有序的，这里不赌——
		// 乱序的要排好，负起点和终点在起点之前的坏行要丢掉。
		{"from": 12.5, "to": 107, "content": "前奏"},
		{"from": 0, "to": 12.5, "content": "开场"},
		{"from": -1, "to": 5, "content": "负起点"},
		{"from": 300, "to": 200, "content": "终点在起点之前"},
		{"from": 107.25, "to": 300, "content": "正题"},
	}))
	got, err := c.Chapters(context.Background(), "BV1", "1")
	if err != nil {
		t.Fatal(err)
	}
	want := []Chapter{
		{From: 0, To: 12500 * time.Millisecond, Title: "开场"},
		{From: 12500 * time.Millisecond, To: 107 * time.Second, Title: "前奏"},
		{From: 107250 * time.Millisecond, To: 300 * time.Second, Title: "正题"},
	}
	if len(got) != len(want) {
		t.Fatalf("章节有 %d 个，want %d 个：%+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 章 = %+v，want %+v", i, got[i], want[i])
		}
	}
}

// TestChaptersAsksThePlayerInfoEndpoint 盯「问的是哪一个接口、带的是哪两个参数」：
// 章节和字幕在**同一个**接口上，而它不在取流响应里——问错了地方永远拿不到。
func TestChaptersAsksThePlayerInfoEndpoint(t *testing.T) {
	c, f := chaptersClient(t, playerInfoWithChapters([]map[string]any{
		{"from": 0, "to": 10, "content": "一段"},
	}))
	if _, err := c.Chapters(context.Background(), "BV1", "1"); err != nil {
		t.Fatal(err)
	}
	reqs := f.requests()
	if len(reqs) != 3 { // nav（WBI 钥匙）+ spi（buvid）+ 播放器信息
		t.Fatalf("发了 %d 趟请求，want 3：%+v", len(reqs), reqs)
	}
	last := reqs[len(reqs)-1]
	if last.Path != "/x/player/wbi/v2" {
		t.Errorf("问的是 %q，want /x/player/wbi/v2", last.Path)
	}
	if last.Query.Get("bvid") != "BV1" || last.Query.Get("cid") != "1" {
		t.Errorf("参数没带全：bvid=%q cid=%q", last.Query.Get("bvid"), last.Query.Get("cid"))
	}
}

// TestChaptersWithoutViewPointsGivesNone 盯「没有章节不是错误」：B 站对没有
// 章节的投稿要么不给这个字段，要么给一个空表——两种都得落到空列表。
func TestChaptersWithoutViewPointsGivesNone(t *testing.T) {
	for name, body := range map[string]string{
		"字段整个没有": playerInfoEnvelope(map[string]any{"bvid": "BV1", "cid": 1}),
		"空表":     playerInfoWithChapters(nil),
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := chaptersClient(t, body)
			got, err := c.Chapters(context.Background(), "BV1", "1")
			if err != nil {
				t.Fatalf("没有章节不该是错误：%v", err)
			}
			if got != nil {
				t.Errorf("章节 = %+v，want 空", got)
			}
		})
	}
}

// TestChaptersWithoutIdentityDoesNotAsk 盯「没给全身份就不发请求」：和字幕那一路
// 同一条规矩——bvid 或 cid 空着的时候，发出去的请求只会得到一个看不懂的回答。
func TestChaptersWithoutIdentityDoesNotAsk(t *testing.T) {
	c, f := chaptersClient(t, playerInfoWithChapters([]map[string]any{
		{"from": 0, "to": 10, "content": "一段"},
	}))
	for _, id := range [2][2]string{{"", "1"}, {"BV1", ""}} {
		got, err := c.Chapters(context.Background(), id[0], id[1])
		if err != nil || got != nil {
			t.Errorf("身份没给全还给 %+v（%v），want 空且无错", got, err)
		}
	}
	if reqs := f.requests(); len(reqs) != 0 {
		t.Errorf("发了 %d 趟请求，want 0（不该问播放器信息）", len(reqs))
	}
}
