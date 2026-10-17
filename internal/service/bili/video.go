// 分 P 列表：B 站的搜索结果只给 bvid，要放哪一集得另外问一次。
//
// # 为什么需要它
//
// B 站的分类搜索**不返回分 P id**（`search.go` 的文件头有整段说明）。而取流接口
// 要 cid 才是「放的是哪一集」，不给就按第一集算——多 P 投稿放出来的和用户点的
// 不是一回事。
//
// 上游 Biu 放歌时正是这么做的：`store/play-list.ts:129` 的 `getMVData(bvid)` 调
// `/x/web-interface/view` 拿 `pages[].cid`，一个 bvid 拆成队列里的几项。本项目
// 在**放歌的那一刻**问一次（见 `app.runOnline`），而不是把一页 24 条结果全问一遍
// ——用户多半只会听其中一首，为另外 23 首各发一次请求不划算。
//
// 用的是 `/x/player/pagelist` 而不是 `/x/web-interface/view`：两者给的分 P 列表是同
// 一份，前者是专门的接口（上游 `components/video-pages-download-select-modal` 也用它，
// 见 `src/service/player-pagelist.ts`），返回里没有标题、封面、统计那些用不上的字段。

package bili

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
)

// videoPagesPath 是分 P 列表接口。上游 `src/service/player-pagelist.ts` 用的也是它。
const videoPagesPath = "/x/player/pagelist"

// ErrNoVideo 是「B 站说没有这个稿件」（-404，或 200 但列表是空的）。
var ErrNoVideo = errors.New("bili: 没有这个稿件")

// VideoPart 是一个分 P（B 站那边就叫 part）。
//
// 名字不叫 VideoPage 是因为 `search.go` 里那个 VideoPage 是「一页搜索结果」，
// 两个概念没关系，撞名只会在读代码的时候读错。
type VideoPart struct {
	// Cid 是这一集的 id。
	Cid string
	// Page 是第几集，从 1 起。
	Page int
	// Part 是这一集的标题。
	Part string
}

// videoPagesData 是 `data` 段。B 站这个接口有两种返回形状（少分 P 的稿件只给一个
// 扁平的 `data`），两种都在 `pagesOf` 里认。
type videoPagesData struct {
	Cid   int64  `json:"cid"`
	Page  int    `json:"page"`
	Part  string `json:"part"`
	Pages []struct {
		Cid  int64  `json:"cid"`
		Page int    `json:"page"`
		Part string `json:"part"`
	} `json:"pages"`
}

// VideoPages 问 B 站这个稿件有哪几个分 P。
//
// 一次都没给（`-404`，或者 200 但列表是空的）返回 `ErrNoVideo`——那和「网络断了」
// 要办的事不一样，调用方要分开说。
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
		// 🔴 `-404` 是「B 站说没有这个稿件」，不是网络错也不是风控：调用方要分开说
		// （对一个已经删掉的稿件一直重试是没用的）。
		var api *APIError
		if errors.As(err, &api) && api.Code == -404 {
			return nil, fmt.Errorf("%w：%s（%s）", ErrNoVideo, bvid, api.Message)
		}
		return nil, err
	}
	var d videoPagesData
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("bili: 分 P 列表的返回读不懂：%w", err)
	}
	if len(d.Pages) == 0 {
		if d.Cid == 0 {
			return nil, fmt.Errorf("%w：%s", ErrNoVideo, bvid)
		}
		// 只有一个分 P 的稿件，B 站有时把信息平铺在 data 上而没有 pages 数组。
		return []VideoPart{{Cid: strconv.FormatInt(d.Cid, 10), Page: max(d.Page, 1), Part: d.Part}}, nil
	}
	out := make([]VideoPart, 0, len(d.Pages))
	for _, p := range d.Pages {
		if p.Cid <= 0 {
			continue // 没有 cid 的那一项没法用来取流（传下去的是别人的集）
		}
		out = append(out, VideoPart{
			Cid:  strconv.FormatInt(p.Cid, 10),
			Page: max(p.Page, 1),
			Part: p.Part,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w：%s 的分 P 列表里没有一个带 cid 的", ErrNoVideo, bvid)
	}
	return out, nil
}

// FirstVideoCid 是这个稿件**第一集**的分 P id，也就是调用方压根没给 cid 时该用的那一个。
//
// ⚠️ 这是「按第一集播」的兜底，不是「用户就想要第一集」：多 P 投稿不给 cid 时
// 放出来的是第一集（`app.runOnline` 会先问一次 `VideoPages` 把真正该用的那一集补上，
// 这里只是补不上时的退路）。
func (c *Client) FirstVideoCid(ctx context.Context, bvid string) (string, error) {
	pages, err := c.VideoPages(ctx, bvid)
	if err != nil {
		return "", err
	}
	return pages[0].Cid, nil
}
