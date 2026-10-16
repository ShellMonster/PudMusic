// UP 主空间：头图、关注按钮、统计，和四个 Tab 各自的数据接口。
//
// # 四个 Tab 是哪四个，一个字都不能改
//
// Biu 上游 `src/pages/user-profile/index.tsx:73-104` 把 Tab 写死成
// 「动态 / 投稿 / 收藏夹 / 合集」四张表：
//
//	动态    /x/polymer/web-dynamic/v1/feed/space    （WBI）
//	投稿    /x/space/wbi/arc/search                 （WBI）
//	收藏夹  /x/v3/fav/folder/created/list
//	合集    /x/polymer/web-space/seasons_series_list
//
// 四个接口的鉴权要求各不相同：前两个**要 WBI 签名**，第三个要 Cookie
// （只有登录用户看得到别人的收藏夹时它才有内容），第四个不要。抄错了的
// 表现是静默的 -403「访问权限不足」，而那句话和「签名算错了」没有任何关系。
//
// # 关系值只有五个，别自己造
//
// B 站的 `relation.attribute` 是这样一套数（上游
// `src/common/constants/relation.ts` 的 `UserRelation` 枚举，原话照抄）：
//
//	0 未关注   1 悄悄关注（已弃用）   2 已关注   6 已互粉   128 已拉黑
//
// ⚠️ **「已关注」是两个值（2 和 6），不是一个。** 只判 2 的话，互粉的 UP 主
// 身上显示的按钮会是「关注」——用户一点，B 站返回的是「你已经关注过他了」
// 而关系一点没变，按钮于是永远回不到「已关注」。这一条在 `Relation.Followed`
// 里有测试钉住。
//
// # 拉黑是一个独立的状态，不是「没关注」
//
// attribute=128 时上游整页换成一张「已拉黑」的图（`space-info.tsx:69-75`），
// 连统计和 Tab 都不画。把 128 当成 0 处理的话，用户看到的是「关注」按钮，
// 按下去发出去的是 act=2（取关），而 B 站对拉黑中的用户返回的是另一个码。
package bili

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// UP 空间这几个接口的路径。散在各处容易写错一个字母，而写错了的表现是
// -403「访问权限不足」——那句话和「路径写错了」没有任何关系。
const (
	// spaceAccPath 是空间详细信息（昵称、头像、头图、等级）。
	spaceAccPath = "/x/space/wbi/acc/info"
	// spaceRelationPath 是「我和这个 UP 的关系」。
	spaceRelationPath = "/x/space/wbi/acc/relation"
	// relationStatPath 是关注数 / 粉丝数。
	relationStatPath = "/x/relation/stat"
	// relationModifyPath 是写接口：关注 / 取关 / 拉黑 / 取消拉黑。
	relationModifyPath = "/x/relation/modify"
	// spaceVideoPath 是「投稿」Tab。
	spaceVideoPath = "/x/space/wbi/arc/search"
	// spaceSeriesPath 是「合集」Tab（合集和系列两个列表，接口一次给全）。
	spaceSeriesPath = "/x/polymer/web-space/seasons_series_list"
	// spaceFavPath 是「收藏夹」Tab（这个人自己创建的收藏夹）。
	spaceFavPath = "/x/v3/fav/folder/created/list"
)

// User 是一个 UP 主的空间头部。
//
// 字段照上游 `src/service/space-wbi-acc-info.ts` 的 `SpaceAccInfoData` 取，
// **只留画得出来的那些**：会员牌子、认证图标、充电入口那些上游画了、这一版
// 没画，先不立字段——立了字段不画，下次翻代码的人会以为漏了。
type User struct {
	// Mid 是这个人的 id。它同时是这一页路由上的那个参数。
	Mid int64
	// Name 是昵称。
	Name string
	// Sign 是个性签名。
	Sign string
	// Face 是头像地址。空串是「没有头像」。
	Face string
	// Banner 是头图地址。**空串是常态**：大部分 UP 主没有设头图，
	// B 站这时候给的是空对象而不是一个占位图。
	Banner string
	// Level 是 B 站等级（0~6），界面上写成 `Lv3`。
	Level int
	// Official 是认证信息。空串是没认证（上游画一个闪电图标）。
	Official string
}

// spaceAccData 是 acc/info 的 `data` 段。
type spaceAccData struct {
	Mid       int64  `json:"mid"`
	Name      string `json:"name"`
	Sign      string `json:"sign"`
	Face      string `json:"face"`
	TopPhoto  string `json:"top_photo"`
	TopPhoto2 struct {
		// L200h 是 200 高的那一份（上游用的就是它）。
		L200h string `json:"l_200h_img"`
		// L 是原图。原图能有好几 MB，头图区只有 200 高，用它是白下。
		L string `json:"l_img"`
	} `json:"top_photo_v2"`
	Level    int `json:"level"`
	Official struct {
		// Role 是认证类型：0 是没认证（上游 `Boolean(spaceInfo?.official?.role)`）。
		Role  int    `json:"role"`
		Title string `json:"title"`
	} `json:"official"`
}

// SpaceAcc 问 B 站要一个 UP 主的空间头部。
//
// mid 是那个人的 id。
//
// ⚠️ 这个接口**要 WBI 签名**。不带的话 B 站回 -403「访问权限不足」，
// 而那句话读起来像是「你没权限看这个人」——于是人会以为是 mid 写错了。
func (c *Client) SpaceAcc(ctx context.Context, mid int64) (*User, error) {
	if mid <= 0 {
		return nil, fmt.Errorf("bili: 空间 id 不对（mid=%d）", mid)
	}
	q := url.Values{}
	q.Set("mid", strconv.FormatInt(mid, 10))

	raw, err := c.Do(ctx, Call{Method: "GET", URL: c.hostAPI + spaceAccPath, Query: q, WBI: true})
	if err != nil {
		return nil, err
	}
	var d spaceAccData
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("bili: 空间详情的返回读不懂：%w", err)
	}
	// 接口偶尔不带 mid（老接口有过这种时候），拿请求参数补上：
	// 界面要靠它拼这一页的链接（点作者进 `/user/{mid}`）。
	if d.Mid == 0 {
		d.Mid = mid
	}
	return &User{
		Mid:      d.Mid,
		Name:     d.Name,
		Sign:     d.Sign,
		Face:     d.Face,
		Banner:   bannerOf(d),
		Level:    d.Level,
		Official: officialOf(d),
	}, nil
}

// bannerOf 取头图，优先那份 200 高的。
//
// ⚠️ **不能拿到哪份算哪份**：原图动辄好几 MB，而头图区在屏幕上是 200 高。
// 拿原图的直接后果是「点进一个 UP 主下好几张图」，而那几 MB 一张都没显示。
//
// 两份都没有是常态（没设头图的 UP 主占大多数），这时候给空串——
// 界面画一块纯色，而不是画一张 B 站的空白占位图。
func bannerOf(d spaceAccData) string {
	if d.TopPhoto2.L200h != "" {
		return d.TopPhoto2.L200h
	}
	return d.TopPhoto
}

// officialOf 取认证信息，没认证给空串。
func officialOf(d spaceAccData) string {
	if d.Official.Role == 0 {
		return ""
	}
	return d.Official.Title
}

// Relation 是「我和这个 UP 的关系」，就是 B 站那份 attribute。
type Relation int

const (
	// RelationNone 是没关注。
	RelationNone Relation = 0
	// RelationQuietFollowed 是悄悄关注。B 站已经不用了（上游的注释原话），
	// 留着是为了「读到了别当成别的」，不是为了让界面画它。
	RelationQuietFollowed Relation = 1
	// RelationFollowed 是已关注（单向）。
	RelationFollowed Relation = 2
	// RelationMutual 是互相关注。
	RelationMutual Relation = 6
	// RelationBlocked 是把对方拉黑了。
	RelationBlocked Relation = 128
)

// Followed 报告这个关系算不算「已关注」。
//
// ⚠️ **两个值都算**：2（单向已关注）和 6（互相关注）都是已关注。上游
// `space-info.tsx:33` 写的正是 `[UserRelation.Followed, UserRelation.MutualFollowed]
// .includes(...)`。只判 2 的话互粉的 UP 身上按钮写的是「关注」，按下去
// B 站回的是「你已经关注过他了」而关系一点没变——按钮永远回不到「已关注」。
func (r Relation) Followed() bool {
	return r == RelationFollowed || r == RelationMutual
}

// Blocked 报告是不是把对方拉黑了。
func (r Relation) Blocked() bool { return r == RelationBlocked }

// String 是给日志看的名字。界面显示的话另有 `ToggleFollow` 那边的文案。
func (r Relation) String() string {
	switch r {
	case RelationNone:
		return "未关注"
	case RelationQuietFollowed:
		return "悄悄关注（已弃用）"
	case RelationFollowed:
		return "已关注"
	case RelationMutual:
		return "已互粉"
	case RelationBlocked:
		return "已拉黑"
	}
	return fmt.Sprintf("未知的关注状态 %d", int(r))
}

// SpaceRelation 问 B 站「我和这个 UP 现在是什么关系」。
//
// ⚠️ 这个接口**要 WBI 签名，而且没登录会回 -101**。界面要的是「登录了才画
// 关注按钮」，所以未登录这一路应该压根不调它——调了就是把一个能提示的
// 状态换成一个看不懂的 -101。
//
// 用的是 `relation.attribute`（我对他的关系），不是 `be_relation`（他对我的）。
// 两个都在返回里，认错的话「我关注了他」会显示成「他没关注我」。
func (c *Client) SpaceRelation(ctx context.Context, mid int64) (Relation, error) {
	if mid <= 0 {
		return RelationNone, fmt.Errorf("bili: 空间 id 不对（mid=%d）", mid)
	}
	q := url.Values{}
	q.Set("mid", strconv.FormatInt(mid, 10))

	raw, err := c.Do(ctx, Call{Method: "GET", URL: c.hostAPI + spaceRelationPath, Query: q, WBI: true})
	if err != nil {
		return RelationNone, err
	}
	var d struct {
		Relation struct {
			Attribute int `json:"attribute"`
		} `json:"relation"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return RelationNone, fmt.Errorf("bili: 关注状态的返回读不懂：%w", err)
	}
	return Relation(d.Relation.Attribute), nil
}

// RelationStat 是关注数和粉丝数。
//
// ⚠️ 这两个数**没有 follower 字段就当 0，不要报错**：B 站对未登录用户只给
// follower（不给 following）。当成错的话未登录用户根本看不了 UP 主页。
type RelationStat struct {
	Following int64
	Follower  int64
}

// RelationStatOf 问 B 站要一个人的关注数 / 粉丝数。
func (c *Client) RelationStatOf(ctx context.Context, vmid int64) (*RelationStat, error) {
	if vmid <= 0 {
		return nil, fmt.Errorf("bili: 空间 id 不对（vmid=%d）", vmid)
	}
	q := url.Values{}
	q.Set("vmid", strconv.FormatInt(vmid, 10))

	raw, err := c.Do(ctx, Call{Method: "GET", URL: c.hostAPI + relationStatPath, Query: q})
	if err != nil {
		return nil, err
	}
	var d struct {
		Following int64 `json:"following"`
		Follower  int64 `json:"follower"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("bili: 关注数那一份返回读不懂：%w", err)
	}
	return &RelationStat{Following: d.Following, Follower: d.Follower}, nil
}

// RelationAction 是写接口的动作码。
type RelationAction int

const (
	// ActionFollow 是关注。上游 `UserRelationAction.Follow`。
	ActionFollow RelationAction = 1
	// ActionUnfollow 是取关。
	ActionUnfollow RelationAction = 2
	// ActionBlock 是拉黑。
	ActionBlock RelationAction = 5
	// ActionUnblock 是取消拉黑。
	ActionUnblock RelationAction = 6
)

// followSrc 是「从哪儿点的关注」。B 站用它统计来源。
//
// 11 是个人空间（上游 `space-info.tsx` 走的就是从空间点的那一条）。
const followSrc = 11

// ModifyRelation 关注 / 取关 / 拉黑 / 取消拉黑。
//
// ⚠️ 这是**写**接口：CSRF 自动带上（`Options.CSRF`），没登录会回 -101。
// 别拿它当读接口用——没登录时它每次都失败，而「按了关注按钮没反应」
// 比「按钮压根不画」难解释得多。
func (c *Client) ModifyRelation(ctx context.Context, fid int64, act RelationAction) error {
	if fid <= 0 {
		return fmt.Errorf("bili: 空间 id 不对（fid=%d）", fid)
	}
	form := url.Values{}
	form.Set("fid", strconv.FormatInt(fid, 10))
	form.Set("act", strconv.Itoa(int(act)))
	form.Set("re_src", strconv.Itoa(followSrc))

	_, err := c.Do(ctx, Call{
		Method: "POST",
		URL:    c.hostAPI + relationModifyPath,
		Form:   form,
		CSRF:   true,
	})
	return err
}

// UpVideo 是「投稿」Tab 里的一个稿件。
type UpVideo struct {
	Bvid  string
	Title string
	Cover string
	// Duration 是时长。B 站给的是已经写好的字符串（`"3:20"`），
	// 这里原样留着——自己从秒数再拼一遍只会拼出跟 B 站不一样的那一种。
	Duration string
	// Play 是播放数的文字（`"1.2万"`），B 站已经格式化好了。
	Play string
	// Description 是简介。
	Description string
	// Cid 是第一集的 cid。
	//
	// ⚠️ **必须有**：B 站的取流接口不给 cid 会取到**第一集**。多 P 投稿的
	// 第三集放出来是第一集，不报错。
	Cid string
}

// UpVideoQuery 是「投稿」Tab 的一次查询。
type UpVideoQuery struct {
	Mid int64
	// Page 从 1 起。
	Page int
	// Size 一页几条。Biu 那边用的是 30。
	Size int
	// Keyword 是投稿标题里的关键词，空串是不过滤。
	Keyword string
	// Order 是排序：`pubdate`（最新）、`click`（播放）、`scores`（评论）。
	Order string
}

// UpVideoPage 是「投稿」Tab 的一页。
type UpVideoPage struct {
	Videos []UpVideo
	// Total 是这个 UP 一共多少投稿。
	Total int
	// HasMore 后面还有没有。
	HasMore bool
}

// upVideoDefaultOrder 是没指定排序时用的那个。上游 `post/index.tsx:27` 的
// 初值就是 `pubdate`（最新发布）。
const upVideoDefaultOrder = "pubdate"

// UpVideoDefaultSize 是一页几条。上游 `post/index.tsx:40` 用的是 30。
const UpVideoDefaultSize = 30

// SpaceVideos 拉一个 UP 主的投稿（一页）。
//
// ⚠️ 这个接口**要 WBI 签名**。
//
// ⚠️ **页码可能对不上 B 站自己的页码**：B 站在 pn 超出范围时回的是空的
// vlist 而不是报错，于是「拉到第 99 页」会拿到一页空数据而页面上还写着
// 「正在加载…」。所以下面用「拿回来是空的 / 比总数少了」两种判据收口，
// 见 upVideoPageFrom。
func (c *Client) SpaceVideos(ctx context.Context, q UpVideoQuery) (UpVideoPage, error) {
	if q.Mid <= 0 {
		return UpVideoPage{}, fmt.Errorf("bili: 空间 id 不对（mid=%d）", q.Mid)
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.Size <= 0 {
		q.Size = UpVideoDefaultSize
	}
	if q.Order == "" {
		q.Order = upVideoDefaultOrder
	}

	params := url.Values{}
	params.Set("mid", strconv.FormatInt(q.Mid, 10))
	params.Set("pn", strconv.Itoa(q.Page))
	params.Set("ps", strconv.Itoa(q.Size))
	params.Set("order", q.Order)
	if k := q.Keyword; k != "" {
		params.Set("keyword", k)
	}

	raw, err := c.Do(ctx, Call{Method: "GET", URL: c.hostAPI + spaceVideoPath, Query: params, WBI: true})
	if err != nil {
		return UpVideoPage{}, err
	}
	return upVideoPageFrom(raw, q)
}

// upVideoData 是投稿搜索的返回形状。
//
// ⚠️ **`list.vlist` 不是数组就当没有**：B 站在这个 UP 没有投稿、或者页码
// 超了的时候回的是 `{"vlist": null}` 而不是 `[]`。当成数组解会让整个
// 「投稿」Tab 变成一句「返回读不懂」。
type upVideoData struct {
	List struct {
		VList []struct {
			Bvid  string `json:"bvid"`
			Title string `json:"title"`
			Pic   string `json:"pic"`
			Play  int    `json:"play"`
			// Length 是 B 站写好的时长文字。数字型也有（B 站偶尔给纯秒数），
			// 两种都要接得住，见 upVideoDuration。
			Length json.RawMessage `json:"length"`
			// Cid 有时是数字有时是字符串。
			Cid         json.RawMessage `json:"cid"`
			Description string          `json:"description"`
			Author      string          `json:"author"`
		} `json:"vlist"`
	} `json:"list"`
	Page struct {
		Count int `json:"count"`
	} `json:"page"`
}

// upVideoPageFrom 把返回变成一页。
func upVideoPageFrom(raw []byte, q UpVideoQuery) (UpVideoPage, error) {
	var d upVideoData
	if err := json.Unmarshal(raw, &d); err != nil {
		// 返回不是我们认识的那个形状。这是协议漂移，重试没用，如实说。
		return UpVideoPage{}, fmt.Errorf("bili: 投稿列表的返回读不懂：%w", err)
	}

	pg := UpVideoPage{Total: d.Page.Count}
	for _, v := range d.List.VList {
		if v.Bvid == "" {
			// 没有 bvid 的条目点不开（取流接口、跳转链接都靠它）。
			// 丢掉比摆一个点不动的卡片好。
			continue
		}
		pg.Videos = append(pg.Videos, UpVideo{
			Bvid:        v.Bvid,
			Title:       v.Title,
			Cover:       v.Pic,
			Duration:    upVideoDuration(v.Length),
			Play:        strconv.Itoa(v.Play),
			Description: v.Description,
			Cid:         upVideoCid(v.Cid),
		})
	}
	// ⚠️ **空页一律收口**。B 站在 pn 超出范围时回的是空的 vlist 而不报错，
	// 照着「count 还没拿够」继续翻的话，界面会永远转圈。
	pg.HasMore = len(pg.Videos) > 0 && q.Page*q.Size < pg.Total
	return pg, nil
}

// upVideoDuration 读时长。
//
// B 站这个字段是**两套格式混着给的**：绝大多数投稿给的是 `"3:20"` 这样的
// 文字，少数（老投稿、某些分区）给的是纯数字的秒数。只按一种解，另一种
// 就变成一张空卡片。
func upVideoDuration(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil {
		return strconv.Itoa(int(n))
	}
	return ""
}

// upVideoCid 读 cid。同样是两套格式（数字 / 字符串）。
func upVideoCid(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String()
	}
	return ""
}

// UpCollection 是一张收藏夹 / 合集 / 系列的卡片。
//
// 三个 Tab 里有两个（收藏夹、合集）画的全是它，所以三种合成一个类型：
// 点下去跳 `/collection/{id}`，而那个路由靠 `?type=` 区分是哪一种。
type UpCollection struct {
	ID    int64
	Title string
	Cover string
	// Count 是里面有多少条（合集是分 P 数，收藏夹是视频数）。
	Count int
	// Type 是 B 站那边那三种：
	//	1 收藏夹   2 合集（video collections）   3 系列（video series）
	Type int
}

// spaceSeriesData 是 seasons_series_list 的返回。
//
// ⚠️ **合集和系列是一个接口里的两个列表**（上游
// `series/index.tsx:55-65` 把 `seasons_list` 和 `series_list` 拼起来）。
// 只取其中一个的话，一半的 UP 主在「合集」Tab 上是空的——而界面上看不出
// 是「他没有合集」还是「我们漏了」。
type spaceSeriesData struct {
	ItemsLists struct {
		SeasonsList []struct {
			Meta struct {
				SeasonID int64  `json:"season_id"`
				Name     string `json:"name"`
				Cover    string `json:"cover"`
				Ptime    int64  `json:"ptime"`
				Total    int    `json:"total"`
			} `json:"meta"`
		} `json:"seasons_list"`
		SeriesList []struct {
			Meta struct {
				SeriesID int64  `json:"series_id"`
				Name     string `json:"name"`
				Cover    string `json:"cover"`
				Ctime    int64  `json:"ctime"`
				Total    int    `json:"total"`
			} `json:"meta"`
		} `json:"series_list"`
		Page struct {
			Total int `json:"total"`
		} `json:"page"`
	} `json:"items_lists"`
}

// CollectionType 那三个值，照 B 站的定义。
const (
	collectionTypeFav    = 1 // 收藏夹
	collectionTypeSeason = 2 // 合集
	collectionTypeSeries = 3 // 系列
)

// SpaceSeriesPage 是「合集」Tab 的一页。
type SpaceSeriesPage struct {
	Collections []UpCollection
	HasMore     bool
}

// SpaceSeries 拉一个 UP 主的合集和系列（一页）。
//
// page 从 1 起，size 默认 20（上游 `series/index.tsx:41` 用的是 20）。
//
// ⚠️ 这个接口**不要签名**——B 站对它既不要求也不校验，多签一次不会更安全。
func (c *Client) SpaceSeries(ctx context.Context, mid int64, page, size int) (SpaceSeriesPage, error) {
	if mid <= 0 {
		return SpaceSeriesPage{}, fmt.Errorf("bili: 空间 id 不对（mid=%d）", mid)
	}
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}

	params := url.Values{}
	params.Set("mid", strconv.FormatInt(mid, 10))
	params.Set("page_num", strconv.Itoa(page))
	params.Set("page_size", strconv.Itoa(size))
	params.Set("web_location", "0.0")

	raw, err := c.Do(ctx, Call{Method: "GET", URL: c.hostAPI + spaceSeriesPath, Query: params})
	if err != nil {
		return SpaceSeriesPage{}, err
	}
	var d spaceSeriesData
	if err := json.Unmarshal(raw, &d); err != nil {
		return SpaceSeriesPage{}, fmt.Errorf("bili: 合集列表的返回读不懂：%w", err)
	}

	out := SpaceSeriesPage{}
	for _, s := range d.ItemsLists.SeasonsList {
		out.Collections = append(out.Collections, UpCollection{
			ID: s.Meta.SeasonID, Title: s.Meta.Name, Cover: s.Meta.Cover,
			Count: s.Meta.Total, Type: collectionTypeSeason,
		})
	}
	for _, s := range d.ItemsLists.SeriesList {
		out.Collections = append(out.Collections, UpCollection{
			ID: s.Meta.SeriesID, Title: s.Meta.Name, Cover: s.Meta.Cover,
			Count: s.Meta.Total, Type: collectionTypeSeries,
		})
	}
	total := d.ItemsLists.Page.Total
	out.HasMore = len(out.Collections) > 0 && page*size < total
	return out, nil
}

// SpaceFavFolders 拉一个 UP 主**自己创建的**收藏夹。
//
// ⚠️ 这个接口**一次给全，没有翻页参数**——所以返回里没有 HasMore 这回事，
// 调用方别给它加一页「还在加载」。（上游 `favorites/index.tsx:30` 也只调一次。）
//
// ⚠️ 未登录调用会回 -101。别人空间的收藏夹要对方自己开了「公开」才有内容，
// B 站在这种情况下给的是**空列表而不是错**——所以界面上要能说清
// 「他没公开收藏夹」和「加载失败」的区别。
func (c *Client) SpaceFavFolders(ctx context.Context, mid int64) ([]UpCollection, error) {
	if mid <= 0 {
		return nil, fmt.Errorf("bili: 空间 id 不对（mid=%d）", mid)
	}
	params := url.Values{}
	params.Set("up_mid", strconv.FormatInt(mid, 10))

	raw, err := c.Do(ctx, Call{Method: "GET", URL: c.hostAPI + spaceFavPath, Query: params})
	if err != nil {
		return nil, err
	}
	var d struct {
		List []struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
			Media int    `json:"media_count"`
		} `json:"list"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("bili: 收藏夹列表的返回读不懂：%w", err)
	}
	out := make([]UpCollection, 0, len(d.List))
	for _, f := range d.List {
		if f.ID == 0 {
			continue
		}
		out = append(out, UpCollection{
			ID: f.ID, Title: f.Title, Count: f.Media, Type: collectionTypeFav,
		})
	}
	return out, nil
}
