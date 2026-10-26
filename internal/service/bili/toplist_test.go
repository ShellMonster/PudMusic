package bili

// 音乐榜单这一组盯三件事：挑最新一期（年份是 map，没有顺序）、参数发对（list_type /
// list_id）、一条挂着稿件的榜单条目翻成可播放的一行（没有 bvid 的丢掉）。fixture 是
// 我们自己造的，真网对不对得上只能靠真机回归。

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestToplistLatestPicksTheNewestPeriodAcrossYears(t *testing.T) {
	f := signedFake(t)
	f.on(toplistPeriodsPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", json.RawMessage(`{"list":{
			"2022":[{"ID":38,"priod":29,"publish_time":1672394399},{"ID":1,"priod":1,"publish_time":1655460091}],
			"2023":[{"ID":76,"priod":48,"publish_time":1683885696},{"ID":74,"priod":47,"publish_time":1683281010}]}}`)))
	})
	c := f.client(t, Options{})

	id, err := c.ToplistLatestID(context.Background(), ToplistOriginal)
	if err != nil {
		t.Fatal(err)
	}
	if id != 76 {
		t.Errorf("最新一期 = %d，want 76", id)
	}
	if got := lastRequestTo(t, f, toplistPeriodsPath).Query.Get("list_type"); got != "2" {
		t.Errorf("原创榜发出去的 list_type = %q，want 2", got)
	}
}

func TestToplistLatestOfAnEmptyBoardIsZero(t *testing.T) {
	f := signedFake(t)
	f.on(toplistPeriodsPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", json.RawMessage(`{"list":{}}`)))
	})
	id, err := f.client(t, Options{}).ToplistLatestID(context.Background(), ToplistHot)
	if err != nil || id != 0 {
		t.Errorf("空榜单 = (%d, %v)，want (0, nil)", id, err)
	}
}

func TestToplistMusicIsParsedInRankOrder(t *testing.T) {
	f := signedFake(t)
	f.on(toplistMusicPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", json.RawMessage(`{"list":[
			{"music_id":"MA1","music_title":"極楽浄土","singer":"GARNiDELiA","mv_bvid":"BV1mv","mv_cover":"http://i0/mv.jpg",
			 "creation_aid":910787823,"creation_bvid":"BV1HM4y1b79Z","creation_cover":"//i1.hdslb.com/c.jpg",
			 "creation_title":"现场","creation_up":110352985,"creation_duration":100,"creation_play":13487670},
			{"music_id":"MA2","music_title":"只有 MV","singer":"B","mv_aid":5,"mv_bvid":"BV1only","mv_cover":"http://i0/only.jpg"},
			{"music_id":"MA3","music_title":"没稿件","singer":"C"}]}`)))
	})
	c := f.client(t, Options{})

	items, err := c.ToplistMusic(context.Background(), 76)
	if err != nil {
		t.Fatal(err)
	}
	if got := lastRequestTo(t, f, toplistMusicPath).Query.Get("list_id"); got != "76" {
		t.Errorf("list_id = %q，want 76", got)
	}
	if len(items) != 2 {
		t.Fatalf("解出来 %d 条，want 2（没有 bvid 的那条要丢）", len(items))
	}
	a := items[0]
	if a.Bvid != "BV1HM4y1b79Z" || a.Aid != 910787823 || a.Title != "極楽浄土" || a.Owner != "GARNiDELiA" {
		t.Errorf("第一条 = %+v", a)
	}
	if a.OwnerMid != 0 {
		t.Errorf("OwnerMid = %d，want 0（歌手不是稿件 UP，不能跳到别人的空间）", a.OwnerMid)
	}
	if a.Cover != "https://i1.hdslb.com/c.jpg" {
		t.Errorf("协议相对封面没补成 https：%q", a.Cover)
	}
	if a.Duration != 100*time.Second || a.View != 13487670 {
		t.Errorf("时长/播放量 = %v / %d", a.Duration, a.View)
	}
	if items[1].Bvid != "BV1only" || items[1].Cover != "http://i0/only.jpg" {
		t.Errorf("没有挂稿件时要退回 MV：%+v", items[1])
	}
}

func TestToplistRejectsAShapeItDoesNotKnow(t *testing.T) {
	f := signedFake(t)
	f.on(toplistMusicPath, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", json.RawMessage(`{"list":"oops"}`)))
	})
	if _, err := f.client(t, Options{}).ToplistMusic(context.Background(), 1); err != errReadRecommend {
		t.Errorf("err = %v，want errReadRecommend", err)
	}
}
