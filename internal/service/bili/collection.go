// 收藏夹 / 合集 / 系列：一个容器里的歌，和容器自己的那几个开关。
//
// # 为什么单独一个文件
//
// 取流（stream.go）、登录（login.go）、音质（quality.go）关心的是「怎么把一段
// 字节弄到手」；这一组关心的是「B 站那边现在是什么样」。两者的错误分类、
// 重试策略、调用节奏完全不同，混在一起之后两边都要互相让步。
//
// # 这一层只回答问题，不做事
//
// 它不拉代次、不唤醒界面、不决定失败之后该怎么办——那些是 `internal/app`
// 的事（理由同 login.go 的包注释）。所以这里的方法全是**发一次请求就返回**的
// 同步函数，连循环都没有。翻页、刷新、取消都在外面那一层做。
//
// # 三种容器，一个入口
//
// 收藏夹（type=11）、视频合集（21）、视频系列（31）在 B 站那边是三样东西，
// 界面上却是同一页（`/collection/{id}`）。Biu 上游靠 URL 上的 `?type=` 分发
// （`pages/video-collection/index.tsx`），这里改成**显式传** `CollectionType`：
// 类型藏在查询串里的时候，「哪些行为随类型变」就没有一个地方能一眼看全。
//
// # 接口清单（全部照 Biu 上游的 service/ 目录）
//
//	/x/v3/fav/folder/info          容器自己的字段（头图区）
//	/x/v3/fav/resource/list        容器里的内容（分页）
//	/x/v3/fav/folder/fav           收藏这个容器
//	/x/v3/fav/folder/unfav         取消收藏这个容器
//	/x/v3/fav/resource/batch-del   从容器里删掉几个条目（批量）
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

// 收藏夹那几个接口的路径。都带 `/x/v3/fav/` 前缀。
const (
	pathFolderInfo       = "/x/v3/fav/folder/info"
	pathResourceList     = "/x/v3/fav/resource/list"
	pathFolderFav        = "/x/v3/fav/folder/fav"
	pathFolderUnfav      = "/x/v3/fav/folder/unfav"
	pathResourceBatchDel = "/x/v3/fav/resource/batch-del"
)

// platformWeb 是这几个接口要的 `platform` 值。
//
// B 站的收藏夹接口认它：给了 web 回来的内容是给网页端那份，给别的值回来的
// 形状不一样（合集就不一样）。Biu 上游全都硬写 "web"（`service/fav-resource.ts`）。
const platformWeb = "web"

// pageSizeLimit 是内容列表接口一页最多给几条。
//
// ⚠️ **上限是 20，写死 20**（Biu 上游也用 20，`service/fav-resource.ts` 的
// `ps: 20`）。给多了 B 站不报错，只是**默默按 20 截断**——于是翻页从第二页
// 开始就会漏掉一批歌，而界面上看不出任何异样：总数在那儿，列表在增长，
// 唯独中间少了一段。
const pageSizeLimit = 20

// batchDelSize 是「批量删除」一批几个。
//
// ⚠️ B 站的这个接口一次吃不下整个收藏夹（几千条的收藏夹真实存在），
// 而超了之后它**不报错**，只是删掉能删的那些，剩下的原封不动躺在那儿——
// 用户看到的是「删了一半」，而界面只会说成功。所以这里按批发，跟上游
// `common/utils/fav.ts` 拉全量时用的 50 是同一个量级。
const batchDelSize = 50

// CollectionType 是三种容器的类型。值抄 B 站，别自己另编一套。
type CollectionType int

const (
	// CollectionFavorite 是收藏夹（type=11）。也是**默认值**：URL 上没写
	// 类型时按收藏夹处理，和 Biu 的 `Number(type || CollectionType.Favorite)`
	// 一致。
	CollectionFavorite CollectionType = 11
	// CollectionAlbum 是视频合集（type=21）。
	CollectionAlbum CollectionType = 21
	// CollectionSeries 是视频系列（type=31）。
	CollectionSeries CollectionType = 31
)

// Label 是这一类容器在界面上写的名字。Biu 的头图区就是这么写的
// （`pages/video-collection/header/index.tsx:88-96`）。
//
// ⚠️ 合集是「视频合集」、系列是「视频系列」，都带「视频」两个字；
// 收藏夹那边要带上公开/私密，所以那一处不是本函数，是 header 自己拼的
// （见 `Private`）。
func (t CollectionType) Label() string {
	switch t {
	case CollectionAlbum:
		return "视频合集"
	case CollectionSeries:
		return "视频系列"
	case CollectionFavorite:
		return "收藏夹"
	}
	return "收藏夹"
}

// Valid 报 t 是不是真的一种容器类型。
//
// 类型是从 URL 的查询串里来的（`/collection/123?type=21`），别的地方不保证。
// 不认的按收藏夹处理而不是报错：用户点的是侧栏里的收藏夹，URL 上多带一个
// 认不出的 type 不该让整页变成错误页。
func (t CollectionType) Valid() bool {
	switch t {
	case CollectionFavorite, CollectionAlbum, CollectionSeries:
		return true
	}
	return false
}

// ParseCollectionType 从 URL 上的数字读出容器类型。认不出的（和没写的）
// 是收藏夹——理由见 Valid。
func ParseCollectionType(s string) CollectionType {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return CollectionFavorite
	}
	t := CollectionType(n)
	if !t.Valid() {
		return CollectionFavorite
	}
	return t
}

// CollectionSort 是内容列表的排序方式。
//
// ⚠️ **这三个是收藏夹的三个排序**（Biu `pages/video-collection/favorites/index.tsx:454-458`），
// 而且它们是**传给接口的**（`order` 参数），不是前端本地排。别拿合集那套
// 本地排的实现来套——那边只有 `pubtime`/`play`/`collect` 三个，而且排序键是
// 返回数据里的字段。
type CollectionSort int

const (
	// SortByFavTime 是「最近收藏」（`order=mtime`）。**默认的那一档**：
	// 收藏夹是「我攒下来的东西」，最近攒的排在最前面才对得上直觉。
	SortByFavTime CollectionSort = iota
	// SortByPlay 是「最多播放」（`order=view`）。
	SortByPlay
	// SortByPubTime 是「最近投稿」（`order=pubtime`）。
	SortByPubTime
)

// CollectionSorts 是界面上那三个排序，**按显示顺序**排。
//
// 顺序照 Biu（最近收藏 / 最多播放 / 最近投稿）。它是界面画那个下拉时的唯一
// 数据源：别在别处再写一份排序表，两份一漂就是「下拉里第三项和实际发出去的
// order 对不上」，而那个错在界面上完全看不出来。
var CollectionSorts = []CollectionSort{SortByFavTime, SortByPlay, SortByPubTime}

// Label 是这一档排序在界面上写的字。
func (s CollectionSort) Label() string {
	switch s {
	case SortByPlay:
		return "最多播放"
	case SortByPubTime:
		return "最近投稿"
	case SortByFavTime:
		return "最近收藏"
	}
	return "最近收藏"
}

// order 是发给接口的 `order` 值。
//
// 零值（认不出的）回落到 `mtime`：排序值错了不会报错，B 站默默按自己的默认
// 排序返回，于是界面上写「最近投稿」而实际给的是另一档。
func (s CollectionSort) order() string {
	switch s {
	case SortByPlay:
		return "view"
	case SortByPubTime:
		return "pubtime"
	case SortByFavTime:
		return "mtime"
	}
	return "mtime"
}

// CollectionOwner 是容器的创建者。
type CollectionOwner struct {
	Mid  int64
	Name string
	Face string
}

// CollectionInfo 是容器自己的那些字段——顶部头图区画的就是它。
type CollectionInfo struct {
	ID    int64
	Title string
	// Intro 是简介。Biu 是 `line-clamp-1`，空着就不画。
	Intro string
	Cover string
	// Type 是这一类容器。B 站的接口也回一个，但**以 URL 上带的为准**：
	// 用户从侧栏点进来的收藏夹，接口回的类型对不上时以用户点的那个为准。
	Type       CollectionType
	MediaCount int
	Owner      CollectionOwner
	Private    bool
	Default    bool
	Faved      bool
}

// folderInfoData 是 `data` 段。
type folderInfoData struct {
	ID         int64  `json:"id"`
	Attr       int    `json:"attr"`
	Title      string `json:"title"`
	Cover      string `json:"cover"`
	Intro      string `json:"intro"`
	Type       int    `json:"type"`
	MediaCount int    `json:"media_count"`
	FavState   int    `json:"fav_state"`
	Upper      struct {
		Mid  int64  `json:"mid"`
		Name string `json:"name"`
		Face string `json:"face"`
	} `json:"upper"`
}

// CollectionInfo 取容器自己的字段。
//
// ctx 是这一次的生命周期（切走页面就该取消）。typ 是调用方认为它是哪一类
// 容器——接口回的类型字段在这里不采信，理由见 CollectionInfo.Type。
func (c *Client) CollectionInfo(ctx context.Context, id int64, typ CollectionType) (*CollectionInfo, error) {
	if id <= 0 {
		return nil, fmt.Errorf("bili: 收藏夹 id 不对（%d）", id)
	}
	raw, err := c.Do(ctx, Call{
		Method: "GET",
		URL:    c.hostAPI + pathFolderInfo,
		Query:  url.Values{"media_id": {strconv.FormatInt(id, 10)}},
	})
	if err != nil {
		return nil, err
	}

	var d folderInfoData
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("bili: 收藏夹信息的返回读不懂：%w", err)
	}
	if !typ.Valid() {
		typ = CollectionFavorite
	}
	// ⚠️ **attr 是位，不是枚举**：第 0 位是公开性（1 = 私密），第 1 位是
	// 「是不是默认收藏夹」（`common/utils/fav.ts` 的 isPrivateFav /
	// isDefaultFav）。当枚举读的话两个都读反，而默认收藏夹是绝大多数人的
	// 第一个——「默认收藏夹被当成私密」在界面上就是头图区写着「私密收藏夹」。
	return &CollectionInfo{
		ID:         d.ID,
		Title:      d.Title,
		Intro:      d.Intro,
		Cover:      d.Cover,
		Type:       typ,
		MediaCount: d.MediaCount,
		Owner:      CollectionOwner{Mid: d.Upper.Mid, Name: d.Upper.Name, Face: d.Upper.Face},
		Private:    d.Attr&1 == 1,
		Default:    (d.Attr>>1)&1 == 0,
		Faved:      d.FavState == 1,
	}, nil
}

// Media 是容器里的一个条目。
//
// 名字跟着 B 站那边（`FavMedia`）：「合集里的一条」在两边都叫 media。
type Media struct {
	// ID 是内容 id：视频稿件是 avid，音频是 auid。**删除时要用它**，
	// 而且要和 Type 一起用（见 RemoveCollectionMedias）。
	ID int64
	// Type 是 B 站的内容类型。删和放都只认这两个（见 Playable）。
	Type int
	// Bvid 是视频稿件的 bvid。音频条目是空串。
	Bvid string
	// Page 是分 P 数。
	Page  int
	Title string
	Cover string
	Owner string
	// OwnerMid 是那个 UP 主的 mid。播放条上点 UP 名进 `/user/{id}` 用它。
	//
	// 0 是「B 站这一次没给」，不是「没有 UP 主」——名字还可能是有的，那一句
	// 照旧显示，只是点不动（见 `app.Item.ArtistMid`）。
	OwnerMid int64
	Duration time.Duration

	// Attr 是有效性：0 正常，1/9 是失效（UP 自己删了、或者换成了别的链接）。
	//
	// ⚠️ 失效的条目**照样出现在列表里**，Biu 也照样画出来。直接不画的话
	// 用户会发现「收藏夹里的歌自己少了几首」而且不知道为什么——那是比看见
	// 一条灰掉的失效内容更难解释的。
	Attr int
}

// 容器里那几种内容类型。B 站的 `FavMedia.type`。
const (
	// MediaVideo 是视频稿件（type=2）。
	MediaVideo = 2
	// MediaAudio 是音频（type=12）。
	MediaAudio = 12
)

// Playable 报这个条目是不是能放的那种（视频稿件 / 音频）。
//
// 合集（21）和电影（24）也在收藏夹里，它们没有音频流。照 Biu 的做法
// （`canNotPlay = ![2, 12].includes(type)`）把它们排除在批量操作之外，
// 而不是放进去再报错。
func (m Media) Playable() bool { return m.Type == MediaVideo || m.Type == MediaAudio }

// resourceListData 是内容列表接口的 `data` 段。
type resourceListData struct {
	HasMore bool `json:"has_more"`
	Medias  []struct {
		ID       int64  `json:"id"`
		Type     int    `json:"type"`
		Title    string `json:"title"`
		Cover    string `json:"cover"`
		Duration int    `json:"duration"`
		Attr     int    `json:"attr"`
		Page     int    `json:"page"`
		CTime    int64  `json:"ctime"`
		PubTime  int64  `json:"pubtime"`
		Bvid     string `json:"bvid"`
		Upper    struct {
			Name string `json:"name"`
			Mid  int64  `json:"mid"`
		} `json:"upper"`
	} `json:"medias"`
}

// CollectionPage 是一页内容。
type CollectionPage struct {
	// Medias 是这一页的条目。**顺序就是 B 站给的顺序**（已经排过序了）。
	Medias []Media
	// HasMore 后面还有没有。
	HasMore bool
}

// CollectionMedias 取容器里的一页内容，page 从 1 起。
//
// ps 是要几条，传 0 用默认的 20。**大于 20 的部分会被 B 站默默截掉**，
// 所以这里夹一道（见 pageSizeLimit）。
func (c *Client) CollectionMedias(ctx context.Context, id int64, sort CollectionSort, page, ps int) (*CollectionPage, error) {
	if id <= 0 {
		return nil, fmt.Errorf("bili: 收藏夹 id 不对（%d）", id)
	}
	if page < 1 {
		page = 1
	}
	if ps <= 0 || ps > pageSizeLimit {
		ps = pageSizeLimit
	}

	q := url.Values{
		"media_id": {strconv.FormatInt(id, 10)},
		"platform": {platformWeb},
		"pn":       {strconv.Itoa(page)},
		"ps":       {strconv.Itoa(ps)},
		// tid=0 / type=0 是「不限分区 / 不限内容类型」。**不能省**：省了
		// B 站按自己的记忆补一个值，而那个值会悄悄把一部分条目过滤掉。
		"tid":  {"0"},
		"type": {"0"},
		// 排序是接口排的，见 CollectionSort 的注释。
		"order": {sort.order()},
	}
	raw, err := c.Do(ctx, Call{Method: "GET", URL: c.hostAPI + pathResourceList, Query: q})
	if err != nil {
		return nil, err
	}

	var d resourceListData
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("bili: 收藏夹内容的返回读不懂：%w", err)
	}

	out := make([]Media, 0, len(d.Medias))
	for _, m := range d.Medias {
		out = append(out, Media{
			ID: m.ID, Type: m.Type, Bvid: m.Bvid, Page: m.Page,
			Title: m.Title, Cover: m.Cover, Owner: m.Upper.Name, OwnerMid: m.Upper.Mid,
			// ⚠️ B 站的 duration 是**秒**（浮点）。当毫秒解的话一首三分钟的
			// 歌会显示成 3 秒，而且界面上没有任何地方会露出错。
			Duration: time.Duration(float64(m.Duration) * float64(time.Second)),
			Attr:     m.Attr,
		})
	}
	return &CollectionPage{Medias: out, HasMore: d.HasMore}, nil
}

// FavoriteCollection 收藏这个容器（星标点亮）。
func (c *Client) FavoriteCollection(ctx context.Context, id int64) error {
	return c.setCollectionFav(ctx, pathFolderFav, id)
}

// UnfavoriteCollection 取消收藏这个容器。
func (c *Client) UnfavoriteCollection(ctx context.Context, id int64) error {
	return c.setCollectionFav(ctx, pathFolderUnfav, id)
}

func (c *Client) setCollectionFav(ctx context.Context, path string, id int64) error {
	if id <= 0 {
		return fmt.Errorf("bili: 收藏夹 id 不对（%d）", id)
	}
	_, err := c.Do(ctx, c.writeCall(c.hostAPI+path, url.Values{
		"media_id": {strconv.FormatInt(id, 10)},
		"platform": {platformWeb},
	}))
	if err != nil {
		return fmt.Errorf("bili: %s %d: %w", path, id, err)
	}
	return nil
}

// RemoveCollectionMedias 把这几个条目从容器里删掉。
//
// ⚠️ **id 必须和 type 一起给**（B 站的接口收的是 `resources`，格式是
// `id:type,id:type`）。只给 id 的话 B 站删不掉任何东西**而且不报错**，
// 表现是「点了删除，刷新之后一条不少」——界面上看起来就像删除根本没生效。
//
// 合集 / 系列（21 / 31）不支持这个接口：只有收藏夹能删内容。传进来的
// 非收藏夹 id 会原样发出去，B 站回什么就是什么。
//
// 空的 medias 是 no-op：空串 `resources` 发过去 B 站会回 -400，而界面
// 上「没勾任何东西就点删除」根本不该发请求。
func (c *Client) RemoveCollectionMedias(ctx context.Context, id int64, medias []Media) error {
	if id <= 0 {
		return fmt.Errorf("bili: 收藏夹 id 不对（%d）", id)
	}
	if len(medias) == 0 {
		return nil
	}

	// 分批。⚠️ 一次发几千条的结果不是报错，是**只删掉一部分**（见 batchDelSize）。
	for start := 0; start < len(medias); start += batchDelSize {
		end := min(start+batchDelSize, len(medias))
		batch := make([]string, 0, end-start)
		for _, m := range medias[start:end] {
			batch = append(batch, strconv.FormatInt(m.ID, 10)+":"+strconv.Itoa(m.Type))
		}
		_, err := c.Do(ctx, c.writeCall(c.hostAPI+pathResourceBatchDel, url.Values{
			"media_id":  {strconv.FormatInt(id, 10)},
			"resources": {strings.Join(batch, ",")},
			"platform":  {platformWeb},
		}))
		if err != nil {
			return fmt.Errorf("bili: 从收藏夹 %d 里删内容（第 %d 条起）: %w", id, start+1, err)
		}
	}
	return nil
}

// writeCall 造一个写接口的调用：CSRF 同时进查询串、请求头和表单体。
//
// ⚠️ 三处都放是有原因的，别当成冗余：
//   - 查询串和 `bili-jct` 头是 `Client.Do` 里 `Call.CSRF` 已经在做的；
//   - **表单体这一处是 B 站 web 端真正校验的地方**——上游的
//     `request-interceptors.ts` 就是把 csrf 塞进 POST 的 body 里的。
//
// 少任何一处，B 站的答案都是 -111（`ErrCSRF`）。而 -111 在真机上有一半
// 的概率被报成「风控」（见 ErrCSRF 的注释），于是排障方向从一开始就错了。
func (c *Client) writeCall(u string, form url.Values) Call {
	if c.csrf != nil {
		if v := c.csrf(); v != "" {
			// Clone 之后再写：调用方给的那张表是它在别处还会用的东西，
			// 就地改会把 csrf 带进下一次调用。
			form = form.Clone()
			form.Set("csrf", v)
		}
	}
	return Call{Method: "POST", URL: u, Form: form, CSRF: true}
}
