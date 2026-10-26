package app

// 首页「热榜 / 原创榜 / 最近播放」三档的取数。全部走 HTTP fixture（假 B 站），
// 不碰真账号：最近播放读的是真实登录态的观看历史。

import (
	"context"
	"net/url"
	"testing"
	"time"
)

const (
	homeToplistPeriodsPath = "/x/copyright-music-publicity/toplist/all_period"
	homeToplistMusicPath   = "/x/copyright-music-publicity/toplist/music_list"
	homeHistoryPath        = "/x/web-interface/history/search"
)

func toplistHandler(path string, q url.Values) (int, string) {
	switch path {
	case homeToplistPeriodsPath:
		if q.Get("list_type") == "2" {
			return 0, `{"list":{"2024":[{"ID":9,"priod":3,"publish_time":200},{"ID":5,"priod":2,"publish_time":100}]}}`
		}
		return 0, `{"list":{"2024":[{"ID":8,"priod":3,"publish_time":200},{"ID":4,"priod":2,"publish_time":100}]}}`
	case homeToplistMusicPath:
		return 0, `{"list":[
			{"music_id":"MA1","music_title":"第一名","singer":"歌手","creation_bvid":"BV1top","creation_aid":"11","creation_cover":"https://c/t.jpg","creation_duration":120,"creation_play":99},
			{"music_id":"MA2","music_title":"重复","singer":"歌手","creation_bvid":"BV1top"},
			{"music_id":"MA3","music_title":"无稿件","singer":"歌手"}]}`
	}
	return -404, "没有这个接口"
}

func TestTheTwoBoardsEachAskForTheirOwnLatestPeriod(t *testing.T) {
	f := newHomeBili(t)
	f.on(toplistHandler)
	r := recommendOn(t, f)

	for _, tc := range []struct {
		tab        int
		wantType   string
		wantListID string
	}{{HomeTabHot, "1", "8"}, {HomeTabOriginal, "2", "9"}} {
		items, dropped, err := r.Feed(context.Background(), tc.tab, 1)
		if err != nil {
			t.Fatalf("第 %d 档：%v", tc.tab, err)
		}
		if got := f.lastQuery(homeToplistPeriodsPath).Get("list_type"); got != tc.wantType {
			t.Errorf("第 %d 档 list_type = %q，want %q", tc.tab, got, tc.wantType)
		}
		if got := f.lastQuery(homeToplistMusicPath).Get("list_id"); got != tc.wantListID {
			t.Errorf("第 %d 档 list_id = %q，want %q", tc.tab, got, tc.wantListID)
		}
		if len(items) != 1 || items[0].ID != "BV1top" || items[0].Item.Bvid != "BV1top" || items[0].Item.Source != "online" {
			t.Errorf("第 %d 档条目 = %+v", tc.tab, items)
		}
		if dropped != 1 {
			t.Errorf("dropped = %d，want 1（没有 bvid 的在服务层就丢了，重复的那条算在这里）", dropped)
		}
	}
}

func TestABoardIsOnePageSoThePageAfterItIsEmptyAndFree(t *testing.T) {
	f := newHomeBili(t)
	f.on(toplistHandler)
	r := recommendOn(t, f)

	items, _, err := r.Feed(context.Background(), HomeTabHot, 2)
	if err != nil || len(items) != 0 {
		t.Fatalf("第 2 页 = (%v, %v)，want 空且无错", items, err)
	}
	if n := f.calls(homeToplistPeriodsPath) + f.calls(homeToplistMusicPath); n != 0 {
		t.Errorf("第 2 页发了 %d 个请求，want 0", n)
	}
}

func TestAnEmptyBoardIsAnEmptyPageNotAFailure(t *testing.T) {
	f := newHomeBili(t)
	f.on(func(path string, _ url.Values) (int, string) {
		if path == homeToplistPeriodsPath {
			return 0, `{"list":{}}`
		}
		return -404, "没有这个接口"
	})
	items, _, err := recommendOn(t, f).Feed(context.Background(), HomeTabOriginal, 1)
	if err != nil || len(items) != 0 {
		t.Errorf("空榜 = (%v, %v)，want 空且无错", items, err)
	}
	if f.calls(homeToplistMusicPath) != 0 {
		t.Error("没有任何一期时不该再去取内容")
	}
}

func TestRecentPlaysReadTheHistoryPageByPage(t *testing.T) {
	f := newHomeBili(t)
	f.on(func(path string, _ url.Values) (int, string) {
		if path != homeHistoryPath {
			return -404, "没有这个接口"
		}
		return 0, `{"has_more":true,"list":[
			{"title":"看过的歌","cover":"https://c/h.jpg","author_name":"某 UP","author_mid":7,"view_at":1700000000,"progress":30,"duration":200,"kid":1,"history":{"bvid":"BV1seen","cid":5,"business":"archive"}},
			{"title":"没 bvid","history":{"business":"archive"}}]}`
	})
	items, dropped, err := recommendOn(t, f).Feed(context.Background(), HomeTabRecent, 3)
	if err != nil {
		t.Fatal(err)
	}
	q := f.lastQuery(homeHistoryPath)
	if q.Get("pn") != "3" || q.Get("business") != "archive" {
		t.Errorf("历史记录的查询串 = %v，want pn=3 business=archive", q)
	}
	if len(items) != 1 || dropped != 1 {
		t.Fatalf("items=%d dropped=%d，want 1 / 1", len(items), dropped)
	}
	it := items[0]
	if it.ID != "BV1seen" || it.Item.Bvid != "BV1seen" || it.Item.Cid != "5" || it.Owner != "某 UP" ||
		it.OwnerMid != 7 || it.Duration != 200*time.Second {
		t.Errorf("条目 = %+v", it)
	}
}

func TestRecentPlaysFailureIsReportedNotHidden(t *testing.T) {
	f := newHomeBili(t)
	f.on(func(string, url.Values) (int, string) { return -101, "null" })
	if _, _, err := recommendOn(t, f).Feed(context.Background(), HomeTabRecent, 1); err == nil {
		t.Error("B 站回 -101 时应当把错交给界面，而不是吞成空列表")
	}
}

func TestHotSongTitlesComeFromTheHotBoardAndCacheForAWhile(t *testing.T) {
	f := newHomeBili(t)
	f.on(toplistHandler)
	r := recommendOn(t, f)
	done := make(chan struct{}, 4)
	wake := func() { done <- struct{}{} }

	r.RefreshHotSongs(wake)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("热榜歌名没拉回来")
	}
	if got := r.HotSongTitles(); len(got) != 1 || got[0] != "第一名" {
		t.Errorf("大家在听 = %q，want [第一名]", got)
	}
	n := f.calls(homeToplistMusicPath)
	r.RefreshHotSongs(wake)
	time.Sleep(50 * time.Millisecond)
	if f.calls(homeToplistMusicPath) != n {
		t.Error("缓存期内又去拉了一遍热榜")
	}
}
