// 音乐榜单（music.bilibili.com/pc/rank 那两张榜）：热榜与原创榜。
//
// 出处：`bilibili-API-collect` 的 `docs/audio/rank.md`。每张榜按期发布，所以取数是
// 两步：先问「有哪几期」（`all_period`），挑出最新的一期，再取那一期的内容
// （`music_list`）。
//
// # 和「流行」那一档（音乐综合榜）不是同一个接口
//
// 「流行」走的是 `music/comprehensive/web/rank`，它是首页自己的综合榜；这里两张榜是
// B 站音乐站的**周榜**，有期数、有名次、有「重回榜单」这类成就文案。两者的条目都
// 带稿件 bvid，能直接进队列。
//
// # 条目怎么翻成一行
//
// 歌名、歌手取榜上那一条的 `music_title` / `singer`；封面、时长、播放量取**挂着的稿件**
// （`creation_*`），稿件没给封面再退回 `mv_cover`。
//
// ⚠️ **歌手和稿件 UP 不是同一个人**（官方现场、翻唱稿件的 UP 不是原唱），所以
// `Owner` 取歌手、`OwnerMid` 留 0——点歌手名跳到稿件 UP 的空间是跳错人，而画面上看不出。
//
// ⚠️ **没有稿件 bvid 的条目丢掉**：取流要 bvid，一行画得出来却放不了不如不画；这和
// 综合榜上「纯音乐条目」的取舍不同——这两张榜上每条本来就挂着稿件，缺 bvid 说明
// 那一条数据不全。
package bili

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"time"
)

const (
	// toplistPeriodsPath 是某张榜的所有期。
	toplistPeriodsPath = "/x/copyright-music-publicity/toplist/all_period"
	// toplistMusicPath 是某一期的内容。
	toplistMusicPath = "/x/copyright-music-publicity/toplist/music_list"
)

// ToplistKind 是哪一张榜。数值就是接口的 `list_type`。
type ToplistKind int

const (
	// ToplistHot 是热榜。
	ToplistHot ToplistKind = 1
	// ToplistOriginal 是原创榜。
	ToplistOriginal ToplistKind = 2
)

// toplistPeriodsData 是 all_period 的 data 段：年份 → 该年各期。
type toplistPeriodsData struct {
	List map[string][]struct {
		ID          int64     `json:"ID"`
		Number      flexInt64 `json:"priod"`
		PublishTime int64     `json:"publish_time"`
	} `json:"list"`
}

// ToplistPeriod 是某张榜的一期：榜单 id、第几期、发布时刻。
type ToplistPeriod struct {
	ID          int64
	Number      int
	PublishedAt time.Time
}

// ToplistLatest 取某张榜**最新一期**。没有任何一期时返回零值（ID 为 0）。
//
// 最新 = 发布时间最大的那一期（不信「最后一个年份的第一项」：年份是 map，没有顺序）。
// 接口里期数那一格的字段名就叫 `priod`（B 站拼错了），照它读。
func (c *Client) ToplistLatest(ctx context.Context, kind ToplistKind) (ToplistPeriod, error) {
	v := url.Values{}
	v.Set("list_type", strconv.Itoa(int(kind)))
	raw, err := c.Do(ctx, Call{Method: "GET", URL: c.hostAPI + toplistPeriodsPath, Query: v, CSRF: true})
	if err != nil {
		return ToplistPeriod{}, err
	}
	var d toplistPeriodsData
	if err := json.Unmarshal(raw, &d); err != nil {
		return ToplistPeriod{}, errReadRecommend
	}
	var best ToplistPeriod
	var at int64
	for _, year := range d.List {
		for _, p := range year {
			if p.PublishTime >= at {
				best = ToplistPeriod{ID: p.ID, Number: int(p.Number)}
				if p.PublishTime > 0 {
					best.PublishedAt = time.Unix(p.PublishTime, 0)
				}
				at = p.PublishTime
			}
		}
	}
	return best, nil
}

// ToplistLatestID 取某张榜最新一期的榜单 id。没有任何一期时返回 0。
func (c *Client) ToplistLatestID(ctx context.Context, kind ToplistKind) (int64, error) {
	p, err := c.ToplistLatest(ctx, kind)
	return p.ID, err
}

// toplistMusicData 是 music_list 的 data 段。
type toplistMusicData struct {
	List []struct {
		MusicID          string    `json:"music_id"`
		MusicTitle       string    `json:"music_title"`
		Singer           string    `json:"singer"`
		MvAid            flexInt64 `json:"mv_aid"`
		MvBvid           string    `json:"mv_bvid"`
		MvCover          string    `json:"mv_cover"`
		CreationAid      flexInt64 `json:"creation_aid"`
		CreationBvid     string    `json:"creation_bvid"`
		CreationCover    string    `json:"creation_cover"`
		CreationTitle    string    `json:"creation_title"`
		CreationDuration int64     `json:"creation_duration"`
		CreationPlay     int64     `json:"creation_play"`
	} `json:"list"`
}

// ToplistMusic 取榜单 id 对应那一期的全部条目，**顺序就是名次**。
func (c *Client) ToplistMusic(ctx context.Context, listID int64) ([]RecommendItem, error) {
	v := url.Values{}
	v.Set("list_id", strconv.FormatInt(listID, 10))
	raw, err := c.Do(ctx, Call{Method: "GET", URL: c.hostAPI + toplistMusicPath, Query: v, CSRF: true})
	if err != nil {
		return nil, err
	}
	var d toplistMusicData
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, errReadRecommend
	}
	return toplistPage(d), nil
}

// toplistPage 把解出来的一期榜变成结果。拆出来是为了能直接喂 fixture。
func toplistPage(d toplistMusicData) []RecommendItem {
	out := make([]RecommendItem, 0, len(d.List))
	for _, it := range d.List {
		bvid, aid := it.CreationBvid, int64(it.CreationAid)
		cover := it.CreationCover
		if bvid == "" {
			bvid, aid = it.MvBvid, int64(it.MvAid)
		}
		if bvid == "" {
			continue
		}
		if cover == "" {
			cover = it.MvCover
		}
		title := it.MusicTitle
		if title == "" {
			title = it.CreationTitle
		}
		out = append(out, RecommendItem{
			Bvid: bvid, Aid: aid, TrackID: it.MusicID,
			Title: decodeEntities(title), Cover: normalizeImageURL(cover),
			Owner: decodeEntities(it.Singer),
			View:  it.CreationPlay, Duration: seconds(it.CreationDuration),
		})
	}
	return out
}
