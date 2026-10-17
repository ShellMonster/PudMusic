// 稍后再看：Biu 的 `/later` 页（`biu/src/pages/later/`）用的两个接口。
//
// 这一层只做三件事：把查询条件拼成查询串、把 B 站的返回解成结构体、把秒和
// 毫秒统一成 `time.Time`。**不判「还有没有」**——那是翻页那一层的事（见
// `internal/ui/pagedrows.go`），它同时管着去重和换条件重来。
//
// # 参数抄 Biu 的 `src/service/history-toview-list.ts`
//
//	pn / ps / viewed / add_time_start / add_time_end / arc_max_duration /
//	arc_min_duration / key
//
// 只发我们用得上的那几个：时长上下限 Biu 也没接线（页面上没有对应控件），
// 发一个恒为 0 的参数没有意义，`viewed` 只有「清除已看完」那一条路要，等
// 那个功能做了再开——那时候把它做成一个字段，不要写死在查询里。
//
// # 时间单位
//
// B 站收的是 **Unix 秒**；Biu 那边传的是毫秒（`Math.floor(start / 1000)`），
// 因为 HeroUI 的日期选择器给毫秒。Go 这一层用 `time.Time`，秒由这一层换，
// 调用方永远不看见秒数——否则「毫秒当秒发出去」要等线上才发现（差 1000 倍，
// 表现为「日期筛选永远查不到东西」，不报错）。
package bili

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"time"
)

const (
	// toViewListPath 是「稍后再看」列表。**带 WBI 签名**（Biu 的
	// `useWbi: true`）。
	toViewListPath = "/x/v2/history/toview/web"

	// toViewDelPath 是从稍后再看里移除。
	toViewDelPath = "/x/v2/history/toview/del"

	// toViewMaxPageSize 是 B 站给的一页上限。Biu 那边写的是「默认 20，
	// 最大 50」（history-toview-list.ts 的注释），超过会退回去。
	toViewMaxPageSize = 50
)

// ToViewQuery 是「稍后再看」一页的查询条件。
//
// From / To 是**加进稍后再看**的时间区间，零值是不筛。调用方给的是
// `time.Time`（见文件开头「时间单位」）。
type ToViewQuery struct {
	// Page 是页码，从 1 起。**不是可选的默认值**：页码从 0 开始传的话
	// B 站会当第一页返回，于是「第 0 页」和「第 1 页」返回同一批数据，
	// 翻页那一层的去重会把它吞掉，表现为「一直停在第一页」。
	Page int
	// PageSize 是一页几条。零值按 Biu 的默认 20，大的夹到 50。
	PageSize int
	// Keyword 是搜索词。Biu 那边传的是 `key`。
	Keyword string
	// From / To 是时间区间的两端（闭区间），零值 = 那头不筛。
	From, To time.Time
}

// ToViewItem 是稍后再看里的一个条目。
//
// 字段照 Biu 的 `ToViewVideoItem` 取，**只留我们画得出来、并且播放用得上的**：
// 留着没用的一堆字段（`dynamic`、`desc`、`rights`、`dimension`…）只会让人
// 以为它们参与了判断。
type ToViewItem struct {
	// Aid 是稿件 id，**删一条时用**（`/x/v2/history/toview/del` 收的是 aid
	// 而不是 bvid）。
	Aid int64
	// Bvid / Cid 是播放要用的。B 站的 cid 是**分 P 的 id**：不给它会取到
	// 第一集，不报错，只是放错了那一集（见 `internal/app/online.go` 的
	// `playableOnline`）。
	Bvid string
	Cid  int64
	// Title / Pic / OwnerName / OwnerMid 是这一行要画的。
	Title     string
	Pic       string
	OwnerName string
	OwnerMid  int64
	// Duration 是总时长，Progress 是已经看了多少。
	Duration time.Duration
	Progress time.Duration
	// PubDate 是投稿时间，AddAt 是加进稍后再看的时间。Biu 的稍后再看行画的是
	// `pubTime`（list.tsx 的 `formatSecondsToDate(item.pubdate)`）。
	PubDate time.Time
	AddAt   time.Time
	// View 是播放量。Biu 的稍后再看表头有一列「播放」。
	View int64
	// IsPGC 是 PGC（番剧 / 纪录片）。Biu 对这一类不给「播放」入口——
	// 取流那一套对 PGC 不通（见 `playableOnline`）。界面上据此不给「播放」。
	IsPGC bool
}

// toViewListData 是列表接口的 data 段。count 是**总数**，页面据此判还有没有。
type toViewListData struct {
	Count int `json:"count"`
	List  []struct {
		Aid      int64  `json:"aid"`
		Bvid     string `json:"bvid"`
		Cid      int64  `json:"cid"`
		Title    string `json:"title"`
		Pic      string `json:"pic"`
		Duration int64  `json:"duration"` // 秒
		Progress int64  `json:"progress"` // 秒
		Pubdate  int64  `json:"pubdate"`  // Unix 秒
		AddAt    int64  `json:"add_at"`   // Unix 秒
		IsPGC    bool   `json:"is_pgc"`
		Owner    struct {
			Mid  int64  `json:"mid"`
			Name string `json:"name"`
		} `json:"owner"`
		Stat struct {
			View int64 `json:"view"`
		} `json:"stat"`
	} `json:"list"`
}

// errReadLater 是「HTTP 200、code 0，但 data 的形状不是我们认识的那个」。
//
// 它跟网络错、风控都**不是**一回事：重试同一个请求只会再拿到同一段看不懂的
// JSON。分开是为了界面上说得出「B 站改了接口」而不是「网络不好，换个网试试」——
// 后者会让用户白折腾。
var errReadLater = errors.New("bili: 稍后再看接口的返回读不懂")

// ToViewPage 是一页稍后再看的结果。
type ToViewPage struct {
	Items []ToViewItem
	// Count 是 B 站给的**总条数**，不是这一页返回了几条。用它判「还有没有」
	// 比「这一页不满」准：B 站偶尔会为了一些原因少给一条。
	Count int
}

// ToViewList 拉一页稍后再看。
func (c *Client) ToViewList(ctx context.Context, q ToViewQuery) (ToViewPage, error) {
	ps := q.PageSize
	if ps <= 0 {
		ps = toViewDefaultPageSize
	}
	if ps > toViewMaxPageSize {
		ps = toViewMaxPageSize
	}

	v := url.Values{}
	v.Set("pn", strconv.Itoa(max(q.Page, 1)))
	v.Set("ps", strconv.Itoa(ps))
	if q.Keyword != "" {
		v.Set("key", q.Keyword)
	}
	addRange(v, q.From, q.To)

	raw, err := c.Do(ctx, Call{
		Method: "GET",
		URL:    c.hostAPI + toViewListPath,
		Query:  v,
		WBI:    true,
	})
	if err != nil {
		return ToViewPage{}, err
	}

	var d toViewListData
	if err := json.Unmarshal(raw, &d); err != nil {
		// HTTP 200、code 0，但 data 的形状变了。不要当成网络错——重试没用。
		return ToViewPage{}, errReadLater
	}
	return toViewPageOf(&d), nil
}

// toViewDefaultPageSize 是一页几条。Biu 的 `PAGE_SIZE = 20`（later/index.tsx:21）。
const toViewDefaultPageSize = 20

// toViewPageOf 把解出来的 data 变成结果。拆出来是为了能直接喂 fixture。
func toViewPageOf(d *toViewListData) ToViewPage {
	out := ToViewPage{Count: d.Count, Items: make([]ToViewItem, 0, len(d.List))}
	for _, it := range d.List {
		out.Items = append(out.Items, ToViewItem{
			Aid: it.Aid, Bvid: it.Bvid, Cid: it.Cid,
			Title: it.Title, Pic: it.Pic,
			OwnerName: it.Owner.Name, OwnerMid: it.Owner.Mid,
			Duration: seconds(it.Duration), Progress: seconds(it.Progress),
			PubDate: unixSeconds(it.Pubdate), AddAt: unixSeconds(it.AddAt),
			View: it.Stat.View, IsPGC: it.IsPGC,
		})
	}
	return out
}

// ToViewRemove 从稍后再看里移除一条（按 aid）。
func (c *Client) ToViewRemove(ctx context.Context, aid int64) error {
	form := url.Values{}
	form.Set("aid", strconv.FormatInt(aid, 10))
	return c.postLater(ctx, toViewDelPath, form)
}

// postLater 发一个稍后再看的写请求。
//
// ⚠️ CSRF 必带：不带的话一律 -111，而 B 站对同一个 -111 有时给的是「风控」，
// 两者混在一起极难查（见 `Client.setHeaders`）。
func (c *Client) postLater(ctx context.Context, path string, form url.Values) error {
	_, err := c.Do(ctx, Call{
		Method: "POST",
		URL:    c.hostAPI + path,
		Form:   form,
		CSRF:   true,
	})
	return err
}

// seconds 把 B 站的秒数换成一个时长。B 站的 duration 有时是 -1（未知），
// 负值原样保留会画成 "-1s"，所以夹到 0——界面层「没有就不画那一列」。
func seconds(v int64) time.Duration {
	if v <= 0 {
		return 0
	}
	return time.Duration(v) * time.Second
}

// unixSeconds 把 B 站的 Unix 秒换成一个时刻。0 / 负值是「没有」，返回零值。
func unixSeconds(v int64) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	return time.Unix(v, 0)
}

// addRange 把时间区间写进查询串，零值那头不发。
//
// ⚠️ B 站对这两个参数是 `0 = 不筛`，所以发不发没有区别——**发**是为了让
// 请求录得出来、测试能断言「今天」真的变成了查询串里的两个数。不发的话，
// 「日期筛选根本没接」和「接了但传的是 0」在测试里长得一模一样。
func addRange(v url.Values, from, to time.Time) {
	if !from.IsZero() {
		v.Set("add_time_start", strconv.FormatInt(from.Unix(), 10))
	}
	if !to.IsZero() {
		v.Set("add_time_end", strconv.FormatInt(to.Unix(), 10))
	}
}
