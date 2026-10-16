// 分类搜索：Biu 上游用的就是这一个接口
// （`src/service/web-interface-search-type.ts` 的 `getWebInterfaceWbiSearchType`，
// `GET /x/web-interface/wbi/search/type`）。
//
// # 三个类型，两条请求
//
// 这一层给的是**视频 / 歌曲**和**用户**两种结果：B 站的分类搜索只有这两种在
// 这个项目里用得上（上游的类型定义里还有专栏 / 相簿 / 直播，上游搜索页一个
// 也没用，见 `web-interface-search-type.ts:46-67`）。
//
// 「歌曲」不是 B 站的一个 search_type，它就是**限定在音乐分区的视频**——
// 上游那个默认开着、写着「仅音乐分区」的开关发的就是这个请求
// （`pages/search/video-list/index.tsx:24` 的 `useState(true)` 和 `:40` 的
// `tids: 3`）。上游把它做成开关，本项目把它提成搜索页的第一个 Tab：开关和
// Tab 说的是同一件事，两个入口只会让人以为它们不一样。
//
// # 返回里没有 cid，而在线播放要 cid
//
// 🔴 B 站的搜索结果**只给 bvid，不给分 P id**（上游的 `SearchVideoItem`
// 定义里就没有它，上游要放歌时另外调一次 `/x/web-interface/view` 才拿到
// `pages[].cid`，见 `store/play-list.ts:129` 的 `getMVData`）。
//
// 所以 `VideoHit.Cid` 在大多数情况下是空的，搜索结果放进队列之后在线播放会
// 按「缺稿件 id 或分 P id」拒掉（`app.playableOnline`）——那条限制是有道理的：
// 多 P 投稿不给 cid 会**静默地**取到第一集。这一张只管搜索页，不动在线播放
// 那条路；解析分 P 的活儿跟着在线播放一起补。
//
// B 站偶尔会在结果里带上 `cid`，带上了我们就用（`parseCid`），所以别因为
// 「反正一般是空的」就在接口层把它扔了。

package bili

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// searchPath 是分类搜索接口。Biu 上游也是这一个（`web-interface-search-type.ts:155`）。
const searchPath = "/x/web-interface/wbi/search/type"

// searchPageSize 是每一页取几条。抄上游：两个结果列表都是 `page_size: 24`
// （`video-list/index.tsx:38`、`user-list/index.tsx:32`）。
//
// ⚠️ B 站对这一个接口的上限是 50（`pagesize` 回值最大 50），24 是上游选的，
// 不是接口的顶。改大之前想清楚：每页越多，翻页越少，但「容器没填满自动续拉」
// 补出来的页数也越少（见 `widgets/list/paged.go` 的三重保障）。
const searchPageSize = 24

// searchMusicTid 是音乐分区的 id。上游那一行写死的是 3
// （`video-list/index.tsx:40` 的 `tids: 3`，注释「音乐分区ID为3」）。
const searchMusicTid = 3

// VideoOrder 是视频 / 歌曲结果的排序方式。字符串就是 B 站的 `order` 参数，
// 字面照抄上游的 `sortOptions`（`video-list/search-header.tsx:21-27`）。
type VideoOrder string

const (
	// VideoOrderTotalRank 是「综合排序」，也是上游的默认值。
	VideoOrderTotalRank VideoOrder = "totalrank"
	// VideoOrderClick 是「最多播放」。
	VideoOrderClick VideoOrder = "click"
	// VideoOrderPubdate 是「最新发布」。
	VideoOrderPubdate VideoOrder = "pubdate"
	// VideoOrderDanmaku 是「最多弹幕」。
	VideoOrderDanmaku VideoOrder = "dm"
	// VideoOrderStow 是「最多收藏」。
	VideoOrderStow VideoOrder = "stow"
)

// UserSort 是用户结果的排序方式。B 站的用户搜索用两个参数（`order` +
// `order_sort`），上游把它们收成一个 key（`user-list/utils.ts` 的
// `getSortParams`），这一层照它那样收起来，界面层只管选第几个。
type UserSort struct {
	// Order 是 B 站的 `order`。**默认那一档是 "0" 不是空串**：上游发的是数字
	// 0（`utils.ts:15` 的 `{ order: 0, order_sort: 0 }`），拼进查询串之后是
	// `order=0`。发空串会被 B 站当成「没给」，回的是默认但字段回值对不上。
	Order string
	// OrderSort 是 B 站的 `order_sort`：0 降序、1 升序。
	OrderSort int
}

// 用户排序的五档，字面照抄上游 `user-list/utils.ts` 的 `getSortParams`。
var (
	// UserSortDefault 是「默认排序」。上游是 `{order: 0, order_sort: 0}`。
	UserSortDefault = UserSort{Order: "0", OrderSort: 0}
	// UserSortFansDesc 是「粉丝数由高到低」。
	UserSortFansDesc = UserSort{Order: "fans", OrderSort: 0}
	// UserSortFansAsc 是「粉丝数由低到高」。
	UserSortFansAsc = UserSort{Order: "fans", OrderSort: 1}
	// UserSortLevelDesc 是「Lv等级由高到低」。
	UserSortLevelDesc = UserSort{Order: "level", OrderSort: 0}
	// UserSortLevelAsc 是「Lv等级由低到高」。
	UserSortLevelAsc = UserSort{Order: "level", OrderSort: 1}
)

// VideoQuery 是视频 / 歌曲结果的一次查询。
type VideoQuery struct {
	// Keyword 是搜索词。
	Keyword string
	// Page 是第几页，从 1 起。
	Page int
	// MusicOnly 限定在音乐分区（`tids=3`）。这就是上游那个默认开着的
	// 「仅音乐分区」开关，本项目把它做成了搜索页的第一个 Tab。
	MusicOnly bool
	// Order 是排序方式。零值是综合排序（`totalrank`）。
	Order VideoOrder
}

// UserQuery 是用户结果的一次查询。
type UserQuery struct {
	// Keyword 是搜索词。
	Keyword string
	// Page 是第几页，从 1 起。
	Page int
	// Sort 是排序方式。零值是默认排序。
	Sort UserSort
}

// VideoHit 是一条视频 / 歌曲搜索结果。
type VideoHit struct {
	// Bvid 是稿件 id。这一页所有行拿它当身份（铁律四）。
	Bvid string
	// Cid 是分 P id。**大多数情况是空的**，见文件头那一段。
	Cid string
	// Title 是标题。B 站把命中的词包在 `<em class="keyword">` 里，这里已经
	// 剥掉了标签（见 stripHTML）。
	Title string
	// Owner 是 UP 名。
	Owner string
	// Cover 是封面地址，已经补全了协议头（见 normalizeImageURL）。
	Cover string
	// Duration 是时长，认不出来时是零值。
	Duration time.Duration
	// Mid 是 UP 主 id。
	Mid int64
	// Play 是播放量。
	Play int64
}

// UserHit 是一条用户搜索结果。
type UserHit struct {
	// Mid 是用户 id，也是这一行的身份。
	Mid int64
	// Name 是昵称。
	Name string
	// Sign 是个性签名，空串是 B 站没填。
	Sign string
	// Avatar 是头像地址，已经补全了协议头。
	Avatar string
	// Fans 是粉丝数。
	Fans int64
	// Level 是 Lv 等级，零值是 B 站没给。
	Level int
}

// VideoPage 是一页视频 / 歌曲结果。
type VideoPage struct {
	Hits []VideoHit
	// HasMore 后面还有没有。翻到底之后是 false。
	HasMore bool
}

// UserPage 是一页用户结果。
type UserPage struct {
	Hits []UserHit
	// HasMore 后面还有没有。
	HasMore bool
}

// searchRaw 是这个接口的 `data` 段。
//
// 只声明用得着的字段：B 站的搜索结果一个类型就有二十几个字段，全声明进来
// 等于把接口的形状抄一遍，改一个字段就要跟着改（`nav` 那份已经这么干过一次，
// 见 wbi.go 的 navData）。
type searchRaw struct {
	// NumResults 是命中的总数。翻页靠它判断还有没有。
	NumResults int `json:"numResults"`
	// NumPages 是总页数。
	NumPages int `json:"numPages"`
	// Result 是这一页的结果。视频和用户是同一个数组、字段各取一半，
	// 所以两种查询共用这一个结构（见 searchItem）。
	Result []searchItem `json:"result"`
}

// searchItem 是结果数组里的一项。
//
// 视频和用户的字段并排放在一个结构体里，因为 B 站**就是同一个数组**：
// `search_type` 换了之后，字段也换了，但形状没换。拆成两个结构体再加一层
// 转换，能少写十几行，代价是要写两份几乎一样的解码——这里选前者。
type searchItem struct {
	// —— 视频 / 歌曲那几项 ——
	Aid   int64  `json:"aid"`
	Bvid  string `json:"bvid"`
	Cid   int64  `json:"cid"`
	Title string `json:"title"`
	// Author 是 UP 名。B 站这个字段就叫 author，不叫 owner。
	Author string `json:"author"`
	Pic    string `json:"pic"`
	Mid    int64  `json:"mid"`
	Play   int64  `json:"play"`
	// Duration 是**两种形状**：数字（秒）或字符串（"4:20"）。见 parseDuration。
	Duration json.RawMessage `json:"duration"`

	// —— 用户那几项 ——
	Uname string `json:"uname"`
	Usign string `json:"usign"`
	Fans  int64  `json:"fans"`
	Level int    `json:"level"`
	Upic  string `json:"upic"`
}

// SearchVideos 查一页视频 / 歌曲结果。
//
// ⚠️ **要 WBI 签名**：这个接口不带 `w_rid` 会被 B 站按未签名请求处理，实测
// 直接被风控挡掉。上游也标着 `useWbi: true`（`web-interface-search-type.ts:157`）。
func (c *Client) SearchVideos(ctx context.Context, q VideoQuery) (VideoPage, error) {
	raw, err := c.search(ctx, url.Values{
		"search_type": {"video"},
		"keyword":     {q.Keyword},
		"page":        {strconv.Itoa(pageOf(q.Page))},
		"page_size":   {strconv.Itoa(searchPageSize)},
		"order":       {string(orderOf(q.Order))},
	}, q.MusicOnly)
	if err != nil {
		return VideoPage{}, err
	}

	out := VideoPage{Hits: make([]VideoHit, 0, len(raw.Result))}
	for _, it := range raw.Result {
		if it.Bvid == "" {
			// 没有 bvid 的项在这一页里没法当一行（铁律四：身份要稳定且唯一，
			// 而空串不行）。B 站的搜索结果偶尔混进没有 bvid 的条目，跳过它，
			// 不要让一行空身份进列表——那会让两行共用一份状态。
			continue
		}
		out.Hits = append(out.Hits, VideoHit{
			Bvid:     it.Bvid,
			Cid:      parseCid(it.Cid),
			Title:    stripHTML(it.Title),
			Owner:    it.Author,
			Cover:    normalizeImageURL(it.Pic),
			Duration: parseDuration(it.Duration),
			Mid:      it.Mid,
			Play:     it.Play,
		})
	}
	out.HasMore = searchHasMore(q.Page, len(out.Hits), raw.NumResults)
	return out, nil
}

// SearchUsers 查一页用户结果。
func (c *Client) SearchUsers(ctx context.Context, q UserQuery) (UserPage, error) {
	raw, err := c.search(ctx, url.Values{
		"search_type": {"bili_user"},
		"keyword":     {q.Keyword},
		"page":        {strconv.Itoa(pageOf(q.Page))},
		"page_size":   {strconv.Itoa(searchPageSize)},
		"order":       {userOrderOf(q.Sort)},
		"order_sort":  {strconv.Itoa(q.Sort.OrderSort)},
	}, false)
	if err != nil {
		return UserPage{}, err
	}

	out := UserPage{Hits: make([]UserHit, 0, len(raw.Result))}
	for _, it := range raw.Result {
		if it.Mid == 0 {
			continue // 和视频那边同一个理由：没有 mid 的没法当一行
		}
		out.Hits = append(out.Hits, UserHit{
			Mid:    it.Mid,
			Name:   it.Uname,
			Sign:   it.Usign,
			Avatar: normalizeImageURL(it.Upic),
			Fans:   it.Fans,
			Level:  it.Level,
		})
	}
	out.HasMore = searchHasMore(q.Page, len(out.Hits), raw.NumResults)
	return out, nil
}

// search 是两个查询共用的那一次请求。
//
// musicOnly 只对视频有意义（音乐分区），用户那边忽略它——上游的
// `search_type=bili_user` 请求里也没有 `tids`（`user-list/index.tsx:29-35`）。
func (c *Client) search(ctx context.Context, q url.Values, musicOnly bool) (*searchRaw, error) {
	if musicOnly {
		q.Set("tids", strconv.Itoa(searchMusicTid))
	}
	raw, err := c.Do(ctx, Call{
		Method: "GET",
		URL:    c.hostAPI + searchPath,
		Query:  q,
		WBI:    true,
	})
	if err != nil {
		return nil, err
	}
	var d searchRaw
	if err := json.Unmarshal(raw, &d); err != nil {
		// 回的是 200、code 也是 0，但 data 的形状不认识。重试没用——
		// 这是 B 站改接口改出来的，写法同 stream.go 的 refFrom。
		return nil, fmt.Errorf("bili: 搜索接口的返回读不懂：%w", err)
	}
	return &d, nil
}

// pageOf 把页码夹到 1~100。B 站的这个接口页码上限是 100（`numPages` 最大
// 50 是 `page_size=20` 时算出来的），超了它返回的是一份重复的第一页。
func pageOf(page int) int {
	if page < 1 {
		return 1
	}
	if page > searchMaxPage {
		return searchMaxPage
	}
	return page
}

// searchMaxPage 是这一个接口的页码上限。
const searchMaxPage = 100

// orderOf 把零值换成综合排序。
func orderOf(o VideoOrder) VideoOrder {
	if o == "" {
		return VideoOrderTotalRank
	}
	return o
}

// userOrderOf 把零值换成默认排序的 order。
//
// ⚠️ **零值必须换成 "0" 而不是空串**：`UserQuery.Sort` 的零值是「没设」，而
// B 站的默认排序参数写的是数字 0（上游发的是 `{order: 0, order_sort: 0}`，
// 见 `user-list/utils.ts:15`）。发空串等于「没给这个参数」，B 站回的是另一条
// 路径——界面上看不出异样，只是「默认排序」那一档名不副实。
func userOrderOf(s UserSort) string {
	if s.Order == "" {
		return UserSortDefault.Order
	}
	return s.Order
}

// searchHasMore 判断还有没有下一页。
//
// 判据和上游一样是「已拿到的条数 < 总数」（`video-list/index.tsx:54` 的
// `setHasMore(newList.length < total)`），只是改成用页码算而不是累加已加载的
// 条数：分页列表自己会去重，已加载数可能被去重改小，用它判断会在最后一页
// 之前就停手。
//
// ⚠️ **一页空就说到底了**：接口返回 `numResults: 9999` 而这一页 `result`
// 是空数组的时候（B 站在被风控边缘偶尔这么干），没有它就是无限翻页。
func searchHasMore(page, got, total int) bool {
	return got > 0 && page*searchPageSize < total
}

// parseCid 把 B 站那个数字 cid 变成字符串；0 是「没有」。
//
// 用字符串是因为 `app.Item.Cid` 是字符串，而在线播放比对的是字符串
// （`online.go` 的 `refetch` 拿它和取流回来的 cid 比）。0 变成空串，
// 不变成 "0"——"0" 会被当成一个真的分 P id 传下去，取回来的是别人的集。
func parseCid(cid int64) string {
	if cid <= 0 {
		return ""
	}
	return strconv.FormatInt(cid, 10)
}

// parseDuration 认 B 站的两种时长形状：数字（秒）与字符串（"m:ss" / "h:mm:ss"）。
//
// 认不出来时给零值——调用方（`list.Music.Duration`）把零值当成「没有这一列」
// 不画，画出一个空的时长格子比不画难看得多。**不许猜**：猜出来的时长会跟着
// 播放条一起骗人。
func parseDuration(raw json.RawMessage) time.Duration {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return 0
	}
	// 数字：秒。
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		if n <= 0 {
			return 0
		}
		return time.Duration(n * float64(time.Second))
	}
	// 字符串：m:ss 或 h:mm:ss。**先把 JSON 那对引号摘掉**——直接按文本处理的话
	// 第一段是 `"4`，ParseFloat 失败，整条被当成「认不出来」。
	var str string
	if err := json.Unmarshal(raw, &str); err != nil {
		return 0
	}
	parts := strings.Split(str, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0
	}
	var total float64
	for i, p := range parts {
		// 权重要**从右边起算**，不能正着乘十的幂——"4:20" 的 20 是 20 秒。
		w := 1.0
		for range len(parts) - 1 - i {
			w *= 60
		}
		v, err := strconv.ParseFloat(p, 64)
		if err != nil || v < 0 {
			return 0
		}
		total += v * w
	}
	if total <= 0 {
		return 0
	}
	return time.Duration(total * float64(time.Second))
}

// stripHTML 剥掉标题里的标签。
//
// 🔴 **不能省**：B 站把命中的搜索词包在 `<em class="keyword">` 里，界面上
// 直接显示就是一排尖括号（`BIU-UI-INVENTORY.md` 的坑表里写着「标题剥 HTML」）。
// 上游走的是 DOM 解析（`common/utils/str.ts`），这里用剥标签的写法，理由是
// 这一层是**纯 Go 的解析层**，不该为了一个显示问题把 DOM 搬进来。
//
// ⚠️ **只认成对的尖括号**：一个孤零零的 `<`（标题里真有的「a < b」）后面要是
// 没有 `>`，它就当普通字符留着。按「见到 `<` 就进标签」那种写法写的话，
// 「a < b」会变成「a  」——后面那一整段连同词尾一起没了。
func stripHTML(s string) string {
	if !strings.Contains(s, "<") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != '<' {
			b.WriteByte(s[i])
			i++
			continue
		}
		end := strings.IndexByte(s[i:], '>')
		if end < 0 {
			// 没闭上的 '<':是标题里真有的字符,原样留着。
			b.WriteString(s[i:])
			break
		}
		i += end + 1
	}
	return b.String()
}

// normalizeImageURL 给 B 站的图片地址补上协议。
//
// B 站回的是 `//i2.hdslb.com/...`，**没有协议**。图片缓存拿到一个没有协议的
// 地址会当成一个普通路径去读盘（或者干脆当成加载失败），界面上是一个永远
// 转圈的方块。上游为此有一整个 `formatUrlProtocol`（`common/utils/url.ts:6`）。
func normalizeImageURL(raw string) string {
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	return "https://" + raw
}
