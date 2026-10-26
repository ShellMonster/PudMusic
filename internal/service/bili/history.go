// 历史记录：Biu 的 `/history` 页（`biu/src/pages/history/`）用的三个接口。
//
// # 这一页的接口和「稍后再看」不是一对
//
// 名字听着像，其实是两套：B 站的稍后再看走 `/x/v2/history/toview/web`，
// 历史记录走 `/x/web-interface/history/search`，两者**没有共享的字段**——
// 历史记录那一页要的是 `progress`（看了多少）和 `view_at`（什么时候看的），
// 稍后再看要的是 `add_at`（什么时候加进来的）和 `stat.view`（播放量）。
// 所以这里是两份模型，不是「一个模型两个别名」。
//
// # 分页是页码，不是游标
//
// 这个接口叫 `cursor`，B 站也确实给了 `max` / `view_at` 两个游标字段，但
// Biu 上游**只用了 `pn`**（web-interface-history-search.ts 的 params 只有
// `pn?: number`），我们照抄。理由是它和稍后再看一致、而且它给了 `has_more`
// 直接答「还有没有」，少一次自己推断。将来真要按游标翻，在这一层换，
// 调用方不认。
//
// # business 固定 archive
//
// 这个接口按业务类型分（投稿 / 直播 / 专栏），Biu 的历史记录页写死
// `business: "archive"`（history/index.tsx:47）。我们把它做成字段而不是
// 写死，是为了「按直播筛」那一天不用改这一层；但**现在只有一个值**，
// 别拿它当「什么都能筛」。
package bili

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// historySearchPath 是历史记录列表。
	historySearchPath = "/x/web-interface/history/search"
	// historyClearPath 是清空历史记录。
	historyClearPath = "/x/v2/history/clear"
	// historyDeletePath 是删掉一条历史记录。
	historyDeletePath = "/x/v2/history/delete"
)

// historyMaxPageSize 是一页几条。B 站这个接口默认给 20（给 `ps` 的话最多
// 20），Biu 那边压根没传 ps，所以这里也只留一个默认值、**不开放 ps**：
// 多写一个字段就多一个「这一票支持了吗」的问题，而 Biu 上游没有。
const historyDefaultPageSize = 20

// errReadHistory 是「HTTP 200、code 0，但 data 的形状变了」。理由同
// errReadLater：重试没有意义，界面上也不该说成网络错。
var errReadHistory = errors.New("bili: 历史记录接口的返回读不懂")

// HistoryQuery 是历史记录一页的查询条件。
type HistoryQuery struct {
	// Page 是页码，从 1 起（理由同 ToViewQuery.Page）。
	Page int
	// Keyword 是搜索词。Biu 那边传的是 `keyword`（**不是** `key`——
	// 稍后再看是 `key`，这里少一个字母，写错的话筛选框永远筛不出东西，
	// 而且不报错）。
	Keyword string
	// Business 是业务类型。零值按 archive（投稿）算，那是 Biu 那一页要的。
	Business string
	// From / To 是**观看**的时间区间，零值是不筛。
	From, To time.Time
}

// HistoryItem 是历史记录里的一条。
//
// ⚠️ **Kid 和 Bvid 是两回事，删和放分别用它们**：删除接口收的是
// `{业务类型}_{目标id}`（`archive_12345`，见 history-delete.ts），播放要的是
// `history.bvid`。拿 bvid 去删不会报错——B 站会回 0（"成功"）而什么都没删掉。
type HistoryItem struct {
	Title string
	Cover string
	// Bvid / Cid 是播放要用的（cid 是分 P id，见 ToViewItem）。
	Bvid string
	Cid  int64
	// Business 是这一条属于哪种业务（archive / pgc / article…）。
	Business string
	// Kid 是删这一条要用的标识，形如 `archive_12345`。
	Kid string
	// AuthorName / AuthorMid 是 UP 名。
	AuthorName string
	AuthorMid  int64
	// ViewAt 是什么时候看的，Progress 是看了多少，Duration 是总时长。
	//
	// **这一组三个是这一页存在的理由**（PLAN.md 的 P-04「播放进度」）：
	// 没有它们，用户看到的是一列标题，得自己回想看过多少。
	ViewAt   time.Time
	Progress time.Duration
	Duration time.Duration
}

// historySearchData 是列表接口的 data 段。
type historySearchData struct {
	HasMore bool `json:"has_more"`
	List    []struct {
		Title    string `json:"title"`
		Cover    string `json:"cover"`
		AuthorN  string `json:"author_name"`
		AuthorM  int64  `json:"author_mid"`
		ViewAt   int64  `json:"view_at"`  // Unix 秒
		Progress int64  `json:"progress"` // 秒
		Duration int64  `json:"duration"` // 秒
		Kid      any    `json:"kid"`      // 数字或字符串，见 kidOf
		History  struct {
			Bvid     string `json:"bvid"`
			Cid      int64  `json:"cid"`
			Business string `json:"business"`
		} `json:"history"`
	} `json:"list"`
}

// HistoryPage 是一页历史记录。
type HistoryPage struct {
	Items []HistoryItem
	// HasMore 是 B 站直接给的。**别用「这一页不满」推**：筛选后最后几页常常
	// 是满的，推出来的「还有」会让翻页那一层多跑一趟空请求。
	HasMore bool
}

// HistorySearch 拉一页历史记录。
func (c *Client) HistorySearch(ctx context.Context, q HistoryQuery) (HistoryPage, error) {
	business := q.Business
	if business == "" {
		business = historyBusinessArchive
	}

	v := url.Values{}
	v.Set("pn", strconv.Itoa(max(q.Page, 1)))
	v.Set("ps", strconv.Itoa(historyDefaultPageSize))
	v.Set("business", business)
	if q.Keyword != "" {
		v.Set("keyword", q.Keyword)
	}
	addRange(v, q.From, q.To)

	// ⚠️ 这个接口**不要** WBI 签名：Biu 上游没签（web-interface-history-search.ts
	// 的 `apiRequest.get` 没传 useWbi），签了多一次 nav 往返，而且签名失败
	// 时的报错（-403「访问权限不足」）和真的没权限一模一样。
	raw, err := c.Do(ctx, Call{
		Method: "GET",
		URL:    c.hostAPI + historySearchPath,
		Query:  v,
	})
	if err != nil {
		return HistoryPage{}, err
	}

	var d historySearchData
	if err := json.Unmarshal(raw, &d); err != nil {
		return HistoryPage{}, errReadHistory
	}
	return historyPageOf(&d), nil
}

// historyBusinessArchive 是「投稿」——Biu 那一页固定要的那一种。
const historyBusinessArchive = "archive"

// historyPageOf 把解出来的 data 变成结果。
func historyPageOf(d *historySearchData) HistoryPage {
	out := HistoryPage{HasMore: d.HasMore, Items: make([]HistoryItem, 0, len(d.List))}
	for _, it := range d.List {
		business := it.History.Business
		if business == "" {
			business = historyBusinessArchive
		}
		out.Items = append(out.Items, HistoryItem{
			Title: decodeEntities(it.Title), Cover: it.Cover,
			Bvid: it.History.Bvid, Cid: it.History.Cid,
			Business:   business,
			Kid:        kidOf(it.Kid, business),
			AuthorName: it.AuthorN, AuthorMid: it.AuthorM,
			ViewAt:   unixSeconds(it.ViewAt),
			Progress: seconds(it.Progress),
			Duration: seconds(it.Duration),
		})
	}
	return out
}

// kidOf 是这一条删除要用的 `{业务类型}_{目标id}`。
//
// 🔴 **前缀是接口的一部分，不是装饰**：上游拼的是
// `${item.history.business}_${item.kid}`（`pages/history/index.tsx:223`），而 B 站的
// `kid` 字段本身给的是**光秃秃的目标 id**（上游那个类型就是 `kid?: number`）。
// 只把数字发过去的话 B 站认不出那是哪一类记录——而它**回 code 0**，
// 用户看着那一行消失了、刷新之后它又回来了。
//
// B 站给 `kid` 有三种样子：数字、字符串、干脆没有（那个字段是可选的）。三种都处理：
// 数字和没带前缀的字符串都补上 `{business}_`；已经带了的原样用（别补成
// `archive_archive_123`）。
//
// 两者都拿不到就**返回空串**：要从 bvid 反解出 aid 得抄 B 站那套 58 进制表，抄错
// 一位就是删掉别人的东西，而这种错在界面上完全看不出来。空串传到界面上就是
// 「这一条不给『删除』这一项」，这是能说的实话。
func kidOf(raw any, business string) string {
	if business == "" {
		return ""
	}
	var id string
	switch v := raw.(type) {
	case string:
		id = v
	case float64:
		id = strconv.FormatInt(int64(v), 10)
	}
	if id == "" {
		return ""
	}
	if strings.HasPrefix(id, business+"_") {
		return id
	}
	return business + "_" + id
}

// HistoryDelete 删掉一条历史记录（kid 形如 `archive_12345`）。
func (c *Client) HistoryDelete(ctx context.Context, kid string) error {
	form := url.Values{}
	form.Set("kid", kid)
	return c.postHistory(ctx, historyDeletePath, form)
}

// postHistory 发一个历史记录的写请求。CSRF 必带（同 postLater 的理由）。
func (c *Client) postHistory(ctx context.Context, path string, form url.Values) error {
	_, err := c.Do(ctx, Call{
		Method: "POST",
		URL:    c.hostAPI + path,
		Form:   form,
		CSRF:   true,
	})
	return err
}
