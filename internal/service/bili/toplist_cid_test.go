package bili

// 榜单那一档的分 P id（票：首页榜单点不动）。
//
// # 为什么这一族存在
//
// B 站音乐榜的 `music_list` 接口**给了**分 P id：每一条都有
// `creation_first_cid`。而我们原来没解析它，于是 `Cid` 是零值，往下走变成
// 字符串 `"0"`——B 站把它当一个真的分 P id 收下，回 -400「请求错误」。
//
// 症状是「首页榜单上的歌点了放不出来」，而界面上看不出是哪一步坏的。
//
// 这一族盯三件事：
//
//  1. 榜单那条**要带着分 P id**（`creation_first_cid` 解析出来了）；
//  2. 分 P id 是 **0 的时候要空着**，而不是字符串 `"0"`——`"0"` 会被当成一个
//     真的分 P id 传下去（`withCid` 判的是 `Cid != ""`）；
//  3. **没有 bvid 的那一条照旧丢掉**（取流要 bvid，画得出来却放不了不如不画）。

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// TestToplistKeepsTheFirstPartId 盯榜单那一档带着分 P id。
//
// ⚠️ **字段名是 `creation_first_cid`**：B 站叫「第一集的分 P id」，不是
// `creation_cid`。名字对不上就是解析不到，而解析不到的表现是「接口返回正常、
// 就是这一条放不出来」。
func TestToplistKeepsTheFirstPartId(t *testing.T) {
	f := signedFake(t)
	f.on(toplistMusicPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", json.RawMessage(`{"list":[
			{"music_id":"MA1","music_title":"一首歌","singer":"歌手",
			 "creation_aid":910787823,"creation_bvid":"BV1HM4y1b79Z",
			 "creation_first_cid":27922599802,"creation_duration":100}]}`)))
	})
	items, err := f.client(t, Options{}).ToplistMusic(context.Background(), 76)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("解出来 %d 条，want 1", len(items))
	}
	if items[0].Cid != 27922599802 {
		t.Errorf("分 P id = %d，want 27922599802（榜单接口给了 creation_first_cid，"+
			"不解析的话这一条放不出来）", items[0].Cid)
	}
}

// TestToplistWithoutAPartIdHasNoCid 盯**没有分 P id 时是零值**（不是别的数）。
//
// 这一条和上一条是一对：上一条说「有就要拿到」，这一条说「没有就干净地没有」。
func TestToplistWithoutAPartIdHasNoCid(t *testing.T) {
	f := signedFake(t)
	f.on(toplistMusicPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", json.RawMessage(`{"list":[
			{"music_id":"MA1","music_title":"一首歌","singer":"歌手",
			 "creation_bvid":"BV1HM4y1b79Z"}]}`)))
	})
	items, err := f.client(t, Options{}).ToplistMusic(context.Background(), 76)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("解出来 %d 条，want 1", len(items))
	}
	if items[0].Cid != 0 {
		t.Errorf("分 P id = %d，want 0", items[0].Cid)
	}
}

// TestToplistDropsTheEntryWithoutAVideo 盯没有 bvid 的那一条照旧丢掉。
//
// ⚠️ **这一条不能因为上面那两票而松掉**：加了分 P id 之后，「这一条能不能
// 画出来」和「这一条能不能放」仍然是两件事——bvid 是取流要的（`PlayURL` 把
// 它列为必填），分 P id 是分 P 要的。
func TestToplistDropsTheEntryWithoutAVideo(t *testing.T) {
	f := signedFake(t)
	f.on(toplistMusicPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", json.RawMessage(`{"list":[
			{"music_id":"MA1","music_title":"有稿件","singer":"A",
			 "creation_bvid":"BV1ok","creation_first_cid":123},
			{"music_id":"MA2","music_title":"没稿件","singer":"B",
			 "creation_first_cid":456}]}`)))
	})
	items, err := f.client(t, Options{}).ToplistMusic(context.Background(), 76)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("解出来 %d 条，want 1（没有 bvid 的那条要丢）", len(items))
	}
	if items[0].Bvid != "BV1ok" {
		t.Errorf("留下来的是 %q，want BV1ok", items[0].Bvid)
	}
}
