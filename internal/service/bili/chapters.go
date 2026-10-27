// 投稿的章节（B 站管它叫「分段看点」）：来自播放器信息接口
// `/x/player/wbi/v2` 的 `data.view_points`——和字幕（`subtitle.go`）是**同一个
// 响应**里的两段。
//
// # 上游有类型、没有界面
//
// 上游 Biu 的 `src/service/web-player.ts` 定义了 `WebPlayerViewPoint`
// （`content?` / `from?`（开始秒）/ `to?`（结束秒）），但它只拿这个接口取字幕，
// **没有任何地方渲染 view_points**。所以点画成什么样、点击怎么吸附，没有上游
// 可抄——那是界面这一侧自己的决定（票 #585）。
//
// # 只有投稿有
//
// view_points 挂在 bvid+cid 上：音乐库那一路（`/audio/music-service-c/url`）
// 的音频时长和它背后投稿对不上，本地文件更没有——调用方要自己保证只对投稿
// 条目问（`Item.Bvid` 和 `Item.Cid` 都不空的那一路）。
package bili

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"time"
)

// Chapter 是投稿的一个章节。
type Chapter struct {
	// From 是这一章的起点（也是进度条上那个点画的地方）。
	From time.Duration
	// To 是这一章的终点。下一章的 From 和它通常是同一个时刻。
	To time.Duration
	// Title 是章节名。B 站不保证给。
	Title string
}

// viewPointRaw 是 `view_points` 里的原始一行。
//
// from / to 是**秒**（上游类型里也是「开始秒」「结束秒」），B 站给的是小数。
type viewPointRaw struct {
	From    float64 `json:"from"`
	To      float64 `json:"to"`
	Content string  `json:"content"`
}

// viewPointsData 是播放器信息响应**data 段**里和章节有关的那一段——
// `Client.Do` 回的就是 data 段（`playurlData` 也是这个层级）。别的一段
// （字幕）在 `subtitle.go` 那边另有一个形状——两个解析各取所需，互不干扰。
type viewPointsData struct {
	ViewPoints []viewPointRaw `json:"view_points"`
}

// Chapters 问 B 站要一个 bvid+cid 的章节列表（按起点排序）。
//
// 空列表 = 这一条没有章节（**不是错误**）：B 站对没有章节的投稿给的是空的
// `view_points`，或者干脆不给这个字段——两种都落到这里。
func (c *Client) Chapters(ctx context.Context, bvid, cid string) ([]Chapter, error) {
	if bvid == "" || cid == "" {
		// 不是网络错也不是重试能救的：调用方没给全。
		return nil, nil
	}
	raw, err := c.Do(ctx, Call{
		Method: "GET",
		URL:    c.hostAPI + "/x/player/wbi/v2",
		Query:  url.Values{"bvid": {bvid}, "cid": {cid}},
		WBI:    true,
		Buvid:  true,
	})
	if err != nil {
		return nil, err
	}
	var d viewPointsData
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("bili: 播放器信息的返回读不懂：%w", err)
	}
	out := make([]Chapter, 0, len(d.ViewPoints))
	for _, vp := range d.ViewPoints {
		// 坏行丢掉：负的起点、终点在起点之前的，画到进度条上都是一个
		// 说不通的点。排序放在收集之后——B 站一般给的是有序的，这里不赌。
		if vp.From < 0 || vp.To < vp.From {
			continue
		}
		out = append(out, Chapter{
			From:  time.Duration(vp.From * float64(time.Second)),
			To:    time.Duration(vp.To * float64(time.Second)),
			Title: vp.Content,
		})
	}
	if len(out) == 0 {
		return nil, nil
	}
	sort.Slice(out, func(i, j int) bool { return out[i].From < out[j].From })
	return out, nil
}
