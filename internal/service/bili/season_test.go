// 合集（type=21）与系列（type=31）的内容列表走的是**另外两个接口**（票 #683）。
//
// # 用户报的那句话
//
// 「有些歌 B 站是一个视频下多个子集，点有些按钮时点里面的歌单好像播放不了」。
// 合集 / 系列那一页原来永远打 `/x/v3/fav/resource/list`（那是**收藏夹**的内容
// 接口，要的是 `media_id`），而 B 站对合集 / 系列 id 发到那个接口上只会回一个
// 空列表或者一句 -400——**界面上是一页空白，没有任何错误**。
//
// # 两个接口（照 bilibili-API-collect 的 `docs/video/collection.md`）
//
//	合集	GET /x/polymer/web-space/seasons_archives_list
//		mid（必要，创建者的 mid，文档写「也可以是任意的非负整数」）、
//		season_id（必要）、page_num、page_size、sort_reverse
//	系列	GET /x/series/archives
//		mid（必要）、series_id（必要）、pn、ps、sort（desc / asc）
//
// ⚠️ **两个接口的返回形状不一样**，但**条目那一半是一样的**：都有一个
// `archives` 数组，每一项带 `aid` / `bvid` / `title` / `pic` / `duration` /
// `pubdate` / `stat.view`。合集另有一层 `meta`（标题、封面、总数），系列的
// 元信息在 `/x/series/series`（那是另一件事，不归这条）。
package bili

import (
	"context"
	"net/http"
	"testing"
)

// TestSeasonArchivesHitsTheSeasonEndpoint 钉住合集走对路径、带对参数。
func TestSeasonArchivesHitsTheSeasonEndpoint(t *testing.T) {
	f := newFakeBili(t)
	f.on("/x/polymer/web-space/seasons_archives_list", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if got := q.Get("mid"); got != "900" {
			t.Errorf("mid = %q，want 900（合集的创建者）", got)
		}
		if got := q.Get("season_id"); got != "1227671" {
			t.Errorf("season_id = %q，want 1227671", got)
		}
		if got := q.Get("page_num"); got != "2" {
			t.Errorf("page_num = %q，want 2", got)
		}
		if got := q.Get("page_size"); got != "20" {
			t.Errorf("page_size = %q，want 20", got)
		}
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"archives": []map[string]any{
				{"aid": 311606079, "bvid": "BV1XN411K7g9", "title": "第一集",
					"pic": "http://i2.hdslb.com/bfs/archive/a.jpg", "duration": 261,
					"pubdate": 1679651747, "stat": map[string]any{"view": 12145}},
			},
			// 第 2 页、一页 20、共 **41** 条：这一页拿到 1 条（只造了 1 条），
			// 后面还有 40 条没拉到 → HasMore 必须是 true。
			"page": map[string]any{"page_num": 2, "page_size": 20, "total": 41},
		}))
	})

	pg, err := f.client(t, Options{}).SeasonArchives(context.Background(), 900, 1227671, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(pg.Medias) != 1 {
		t.Fatalf("拿到了 %d 条，want 1", len(pg.Medias))
	}
	m := pg.Medias[0]
	if m.Bvid != "BV1XN411K7g9" || m.Title != "第一集" || m.Type != MediaVideo {
		t.Errorf("条目读歪了：%+v", m)
	}
	if m.ID != 311606079 {
		t.Errorf("ID（avid）= %d，want 311606079", m.ID)
	}
	if m.Duration.Seconds() != 261 {
		t.Errorf("时长 = %v，want 261 秒", m.Duration)
	}
	if m.View != 12145 {
		t.Errorf("播放量 = %d，want 12145", m.View)
	}
	if m.PubTime.Unix() != 1679651747 {
		t.Errorf("发布时间 = %v，want 1679651747", m.PubTime)
	}
	// 第 2 页、一页 20、共 41 → 后面还有（第 3 页）。
	if !pg.HasMore {
		t.Error("第 2 页还有一条没拉到，HasMore 却是 false")
	}
}

// TestSeriesArchivesHitsTheSeriesEndpoint 钉住系列走另一条路径、参数名也不同
// （`pn` / `ps` 而不是 `page_num` / `page_size`）。
func TestSeriesArchivesHitsTheSeriesEndpoint(t *testing.T) {
	f := newFakeBili(t)
	f.on("/x/series/archives", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if got := q.Get("mid"); got != "900" {
			t.Errorf("mid = %q，want 900", got)
		}
		if got := q.Get("series_id"); got != "534501" {
			t.Errorf("series_id = %q，want 534501", got)
		}
		if got := q.Get("pn"); got != "1" {
			t.Errorf("pn = %q，want 1（系列这一路叫 pn，不是 page_num）", got)
		}
		if got := q.Get("ps"); got != "20" {
			t.Errorf("ps = %q，want 20（系列这一路叫 ps，不是 page_size）", got)
		}
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"archives": []map[string]any{
				{"aid": 695029098, "bvid": "BV1k24y1J78X", "title": "下：自动建造",
					"pic": "http://i1.hdslb.com/bfs/archive/b.jpg", "duration": 275,
					"pubdate": 1677321251, "stat": map[string]any{"view": 26555}},
			},
			"page": map[string]any{"num": 1, "size": 20, "total": 1},
		}))
	})

	pg, err := f.client(t, Options{}).SeriesArchives(context.Background(), 900, 534501, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(pg.Medias) != 1 {
		t.Fatalf("拿到了 %d 条，want 1", len(pg.Medias))
	}
	m := pg.Medias[0]
	if m.Bvid != "BV1k24y1J78X" || m.Title != "下：自动建造" || m.Type != MediaVideo {
		t.Errorf("条目读歪了：%+v", m)
	}
	if pg.HasMore {
		t.Error("第 1 页就把 1 条全给了，HasMore 却是 true")
	}
}

// TestSeasonArchivesRejectsAnEmptySeasonID 没有 season_id 就不发请求
// （发了也是 B 站回一句 -400，而用户看到的是「这个合集是空的」）。
func TestSeasonArchivesRejectsAnEmptySeasonID(t *testing.T) {
	f := newFakeBili(t)
	c := f.client(t, Options{})
	if _, err := c.SeasonArchives(context.Background(), 900, 0, 1, 0); err == nil {
		t.Error("season_id 是 0 还发了请求")
	}
	if _, err := c.SeriesArchives(context.Background(), 900, 0, 1, 0); err == nil {
		t.Error("series_id 是 0 还发了请求")
	}
	if n := len(f.requests()); n != 0 {
		t.Errorf("发了 %d 个请求，want 0", n)
	}
}
