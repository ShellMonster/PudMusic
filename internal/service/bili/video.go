// 分 P 列表：B 站的搜索结果只给 bvid，要放哪一集得另外问一次。
//
// # 为什么需要它
//
// B 站的分类搜索**不返回分 P id**（`search.go` 的文件头有整段说明）。而取流接口要
// cid 才是「放的是哪一集」——上游把 cid 列为**必填**
// （`_research/biu/src/service/player-playurl.ts:9`，`cid: number | string`，那行没有
// `?`）。
//
// 上游 Biu 放歌时正是这么做的：`store/play-list.ts:129` 的 `getMVData(bvid)` 查一次
// 分 P。本项目在**放歌的那一刻**问（见 `app.runOnline` 的 `withCid`），而不是把一页
// 24 条结果全问一遍——用户多半只会听其中一首，为另外 23 首各发一次请求不划算。
//
// 用的是 `/x/player/pagelist` 而不是 `/x/web-interface/view`：两者给的分 P 列表是同
// 一份，前者是专门的接口（上游 `components/video-pages-download-select-modal` 也用它，
// 见 `src/service/player-pagelist.ts`）。
//
// # 🔴 响应形状：**data 是数组**
//
// `data: PlayerPagelistItem[]`——上游 `src/service/player-pagelist.ts:18` 的
// `PlayerPagelistResponse` 就是这么写的，元素是同文件 :22 的 `PlayerPagelistItem`。
//
// 这里第一版把它当成了**对象**（`data.pages[]`），而 fixture 也照着那个错形状造，
// 于是全绿；真接口回数组时直接 `json: cannot unmarshal array into Go value`，补 cid
// 静默失败，歌照样放不出来（评审抓到的就是这一条）。所以 fixture 必须照着**真接口的
// 形状**造，见 `video_test.go` 与 `internal/ui/searchbili_test.go` 里的
// `serveVideoPages`。

package bili

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// videoPagesPath 是分 P 列表接口。上游 `src/service/player-pagelist.ts:51` 用的也是它。
const videoPagesPath = "/x/player/pagelist"

// ErrNoVideo 是「B 站说没有这个稿件」（`-404`，或者 200 但列表是空的）。
var ErrNoVideo = errors.New("bili: 没有这个稿件")

// ErrNoPart 是「问不出这一集是哪一集」。
//
// 单独一个哨兵是因为**它和「网络断了」要分开说**：底下多半真的是网络/风控，但用户
// 看到「连不上 B 站」时不知道该做什么，而这一条有明确的下一步（等网络好，或者换个
// 词重新搜，那一条可能就带 cid 了）。`Friendly` 靠它把两句话说开。
var ErrNoPart = errors.New("bili: 问不出这一集是哪一集")

// VideoPart 是一个分 P（B 站那边就叫 part）。
//
// 名字不叫 VideoPage 是因为 `search.go` 里那个 VideoPage 是「一页搜索结果」，两个概念
// 没关系，撞名只会在读代码的时候读错。
type VideoPart struct {
	// Cid 是这一集的 id。
	Cid string
	// Page 是第几集，从 1 起。
	Page int
	// Part 是这一集的标题。
	Part string
	// Duration 是这一集的时长。它就在返回里（上游 `PlayerPagelistItem.duration`，
	// 单位秒），留着是因为将来「选集」那一块要用。
	Duration time.Duration
}

// pagelistItem 是 `data` 数组里的一项，字段照上游的 `PlayerPagelistItem`
// （`player-pagelist.ts:22-32`）取用得着的几项。
//
// ⚠️ `dimension` / `first_frame` / `weblink` / `vid` / `from` 上游也声明了，这里不收：
// 这一层只为了拿 cid，把一堆用不上的字段抄进来等于把接口的形状再抄一遍。
type pagelistItem struct {
	Cid      int64  `json:"cid"`
	Page     int    `json:"page"`
	Part     string `json:"part"`
	Duration int    `json:"duration"` // 秒
}

// VideoPages 问 B 站这个稿件有哪几个分 P。
//
// 空数组、`-404`、或者全都没有 cid，返回 `ErrNoVideo`——那和「网络断了」要办的事
// 不一样，调用方要分开说（对一个已经删掉的稿件一直重试是没用的）。
func (c *Client) VideoPages(ctx context.Context, bvid string) ([]VideoPart, error) {
	if bvid == "" {
		return nil, fmt.Errorf("%w：稿件 id 是空的", ErrNoVideo)
	}
	raw, err := c.Do(ctx, Call{
		Method: "GET",
		URL:    c.hostAPI + videoPagesPath,
		Query:  url.Values{"bvid": {bvid}},
	})
	if err != nil {
		var api *APIError
		if errors.As(err, &api) && api.Code == -404 {
			return nil, fmt.Errorf("%w：%s（%s）", ErrNoVideo, bvid, api.Message)
		}
		return nil, err
	}

	// 🔴 **数组**（上游 `player-pagelist.ts:18`）。写成对象的话真接口回来直接报
	// `cannot unmarshal array into Go value of type ...`；那一层把错吞掉的话，症状是
	// 「补 cid 静默失败、歌照样放不出来」——第一版就是这么错的，所以这里也把形状写
	// 在错误里：读不懂时看一眼就知道是形状变了。
	var items []pagelistItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("bili: 分 P 列表的返回读不懂（data 是数组）：%w", err)
	}

	out := make([]VideoPart, 0, len(items))
	for _, it := range items {
		if it.Cid <= 0 {
			// 没有 cid 的那一项没法用来取流：传下去的是别人的集。
			continue
		}
		out = append(out, VideoPart{
			Cid:      strconv.FormatInt(it.Cid, 10),
			Page:     max(it.Page, 1),
			Part:     it.Part,
			Duration: time.Duration(max(it.Duration, 0)) * time.Second,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w：%s 的分 P 列表里没有一个带 cid 的", ErrNoVideo, bvid)
	}
	return out, nil
}

// FirstVideoCid 是这个稿件**第一集**的分 P id，也就是「压根没给 cid 时该用的那一个」。
func (c *Client) FirstVideoCid(ctx context.Context, bvid string) (string, error) {
	pages, err := c.VideoPages(ctx, bvid)
	if err != nil {
		return "", err
	}
	return pages[0].Cid, nil
}
