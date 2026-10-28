package app_test

// 弹幕那一摊的端到端测试：走**真的取流那一趟**（cid 是问出来的），确认弹幕
// 拿得到。
//
// # 为什么单独一个文件
//
// `internal/app/danmaku_test.go` 在**包内**（`package app`），它能替掉
// `danmakuFetch`（那一趟），所以它量的是这一层的逻辑。
// 这一份在**包外**（`package app_test`），它替不掉——所以它量的是**生产那一档
// 真跑起来**是什么样子：取流那一趟问出分 P id → 写回 `pending` → 弹幕按那个 cid
// 去问 → 装到界面上。

import (
	"bytes"
	"compress/flate"
	"net/http"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// TestTheDanmakuComesForASongWhoseCidHadToBeAsked 盯「搜索结果那一路」也有弹幕。
//
// ⚠️ **搜索结果只给 bvid**：分 P id 是取流那一趟才问出来的（`withCid`）。而弹幕
// 是按 cid 问的——所以「问出来的那一个 cid 要被用上」这件事必须真的成立，
// 否则搜索结果放的歌**永远没有弹幕**（界面上一条都不滚，而用户看不出任何错处）。
//
// ⚠️ **不许从别处借 cid**（评审提的第一条 major）：同一个 bvid 的新选择（换了
// 分 P）在 cid 还没问出来的时候，别处留着的是**上一分 P** 的 cid——借过来就会把
// 上一分 P 的弹幕当成这一次的答案。
func TestTheDanmakuComesForASongWhoseCidHadToBeAsked(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), 30280, 110836))
	// 分 P 列表：这一稿只有一集，cid 是 777。
	f.on("/x/player/pagelist", func(w http.ResponseWriter, r *http.Request) {
		writeStreamJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": []any{map[string]any{"cid": 777, "page": 1, "part": "第一集"}}})
	})
	// buvid（弹幕接口要它）。
	f.on("/x/frontend/finger/spi", func(w http.ResponseWriter, r *http.Request) {
		writeStreamJSON(w, map[string]any{"code": 0, "message": "ok", "ttl": 1,
			"data": map[string]any{"b_3": "B3DM", "b_4": "B4DM"}})
	})
	var askedOid string
	f.on("/x/v1/dm/list.so", func(w http.ResponseWriter, r *http.Request) {
		askedOid = r.URL.Query().Get("oid")
		var buf bytes.Buffer
		fw, err := flate.NewWriter(&buf, flate.DefaultCompression)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write([]byte(`<i><d p="12.34500,1,25,16777215,1792814614,0,ce48882b,2229049562097370368,10">问出来的那一条</d></i>`))
		_ = fw.Close()
		w.Header().Set("Content-Type", "text/xml")
		w.Header().Set("Content-Encoding", "deflate")
		_, _ = w.Write(buf.Bytes())
	})

	a := newOnlineApp(t, f, t.TempDir())
	// ⚠️ **cid 是空的**：这就是搜索结果那一类（`onlineItem` 的 cid 传空串）。
	if err := a.Enqueue(app.Item{Source: "online", Type: "mv", Bvid: "BV1xx", Title: "在线的一首"}); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "在线的一首")

	waitFor(t, 15*time.Second, "弹幕自己出来了", func() bool {
		return a.Danmaku().Ready
	})
	snap := a.Danmaku()
	if len(snap.Items) != 1 {
		t.Fatalf("拿到 %d 条，want 1：%+v", len(snap.Items), snap.Items)
	}
	if snap.Items[0].Text != "问出来的那一条" {
		t.Errorf("正文是 %q，want 「问出来的那一条」", snap.Items[0].Text)
	}
	if askedOid != "777" {
		t.Errorf("问的是 oid=%q，want 777（取流那一趟问出来的 cid）", askedOid)
	}
}
