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
//	/x/v3/fav/resource/clean       清掉容器里所有失效的条目
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
	pathResourceClean    = "/x/v3/fav/resource/clean"
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
		Title:      decodeEntities(d.Title),
		Intro:      d.Intro,
		Cover:      normalizeImageURL(d.Cover),
		Type:       typ,
		MediaCount: d.MediaCount,
		// ⚠️ **头像要补协议头**（`normalizeImageURL`）：B 站给的 `upper.face` 是
		// **协议相对地址**（`//i0.hdslb.com/bfs/face/…`），而封面缓存是用
		// `http.Client` 抓的——那种地址在 Go 那边是 `unsupported protocol scheme ""`，
		// 抓不到，于是界面上那一枚头像永远是一块空圆。
		//
		// ⚠️ **仓库里消费图片地址的地方都要补**：这一条原先只点了三处
		// （`search.go` 的 `normalizeImageURL`、`follow.go` 的 `avatarURL`、UP 空间），
		// 而漏了的一大片（动态流、稍后再看、历史、音乐库、收藏夹列表）现在也补上了；
		// `imgcache` 在下载那一层另有一道兜底（`absoluteURL`），所以以后漏一处也
		// 不会再变成「永久失败的空框」。
		Owner: CollectionOwner{Mid: d.Upper.Mid, Name: d.Upper.Name,
			Face: normalizeImageURL(d.Upper.Face)},
		Private: d.Attr&1 == 1,
		Default: (d.Attr>>1)&1 == 0,
		Faved:   d.FavState == 1,
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

	// View 是播放量。收藏夹那个接口把它放在 `cnt_info.play` 里
	// （上游 `service/fav-resource.ts` 的 `FavMediaCntInfo`：
	// `{ collect, play, danmaku }`）。
	//
	// ⚠️ **零值是「这一次没有这个数」，不是「零播放」**：音轨（type=12）
	// 本来就没有播放量，界面那一格留空。别把它改成一个编出来的 0——那会让
	// 用户在音轨上读到「0 播放」，而收藏夹页最要紧的判断依据就是这一列。
	View int64

	// FavTime 是**收藏时间**（`fav_time`，Unix 秒）。
	//
	// 收藏夹那一页表头写的是「收藏时间」、画的是它（上游
	// `pages/video-collection/favorites/list.tsx:63` 的
	// `formatSecondsToDate(item.fav_time)`）。零值是「这一次没给」。
	FavTime time.Time

	// PubTime 是**发布时间**（`pubtime`，Unix 秒）。
	//
	// 合集 / 系列那一页表头写的是「投稿时间」、画的是它（上游
	// `collections/list.tsx:75` 与 `series/list.tsx:84`）。零值是「这一次
	// 没给」——比如这一条没有发布时间时，那一格留空，不拿别的时间顶上。
	PubTime time.Time
}

// 容器里那几种内容类型。B 站的 `FavMedia.type`。
const (
	// MediaVideo 是视频稿件（type=2）。
	MediaVideo = 2
	// MediaAudio 是音频（type=12）。
	MediaAudio = 12
	// MediaCollection 是合集（type=21）。
	//
	// 🔴 **它和 `MediaMovie` 一样没有音频流**（见 `Playable`），而它们在
	// 收藏夹里是**画得出来的行**——所以界面上要能说清「这一条是合集/电影
	// 那一类」，而不是笼统的「这一条不是投稿」（那是**没挂稿件的推荐条目**
	// 才该说的话，两者的下一步也不一样：合集是「打开里面的单集」）。
	MediaCollection = 21
	// MediaMovie 是电影（type=24）。
	MediaMovie = 24
)

// Playable 报这个条目是不是能放的那种（视频稿件 / 音频）。
//
// 合集（21）和电影（24）也在收藏夹里，它们没有音频流。照 Biu 的做法
// （`canNotPlay = ![2, 12].includes(type)`）把它们排除在批量操作之外，
// 而不是放进去再报错。
//
// 🔴 **判据和「我们真能放」必须一致**。这一条说能放的，界面上就会照着画、
// 照着进队列——而真去播的时候只有 `app.streamFor` 那两条路能放：
//
//	MediaVideo → /x/player/wbi/playurl
//	MediaAudio → /audio/music-service-c/url
//
// 以前抄了 Biu 的判据却只接了第一条，于是 type=12 一路绿过所有测试，
// 按播放必然报错（「判据抄对了，但那一路没跟上——可放就成了界面上的谎言」）。
// 要往这里加一种类型，就得在 `app.streamFor` 里同时加一条取流路径；
// `TestEveryPlayableEntryCanActuallyBeStreamed` 盯着这件事。
func (m Media) Playable() bool { return m.Type == MediaVideo || m.Type == MediaAudio }

// Invalid 报这个条目**已经失效**（稿件被 UP 主删了、或者换成了别的链接）。
//
// # 判据是「attr 不是 0」
//
// B 站给的值：`0` 正常、`1` 其他原因删除、`9` UP 自己删除（上游
// `service/fav-resource.ts:95` 的注释）。所以判据是「不是 0」，不是
// 「是 1 或者是 9」——后者在 B 站加一个新取值时会静默失效。
//
// # 🔴 **它和 `Playable` 是两件事，不许合并**
//
//	`Playable`	这一条**有没有音频流**（合集 / 电影没有）
//	`Invalid`	这一条**还在不在**（稿件没了）
//
// 两者都要「放不了」，但**说的不是同一句话**：合集是「打开里面的单集
// 就能放」，失效是「这一条已经没了」——用户照着做的下一步不一样。
// 合成一个判据的话，说明文字只能取一个，而另一个就说了假话。
//
// # 失效的条目**照样画在列表里**
//
// 这是 B 站给的事实（上游也照样画，注释见 `Media.Attr`）。直接不画的话
// 用户会发现「收藏夹里的歌自己少了几首」而且不知道为什么——那比看见一条
// 标着「已失效」的内容更难解释。
//
// 而**放它进队列更糟**：那会让应用为一条已经不存在的稿件白跑一趟取流
// （每一首失效歌都要等一次超时），而上游是**取流之前就滤掉**的
// （`src/common/utils/fav.ts:55` 的 `.filter(item => ... && item.attr === 0)`）。
func (m Media) Invalid() bool { return m.Attr != 0 }

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
		FavTime  int64  `json:"fav_time"`
		Bvid     string `json:"bvid"`
		Upper    struct {
			Name string `json:"name"`
			Mid  int64  `json:"mid"`
		} `json:"upper"`
		// CntInfo 是状态数。播放量在 `play` 里（上游 `FavMediaCntInfo`）。
		// 音轨那一条可能没有这个形状——零值就是「没有」，界面留空。
		CntInfo struct {
			Play int64 `json:"play"`
		} `json:"cnt_info"`
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
//
// keyword 是按歌名找的那个词，空串就是不过滤。
//
// # 为什么按歌名找是**接口**筛的，不是本地筛
//
// 收藏夹一页只有 20 条（见 pageSizeLimit），而一个收藏夹能有几百上千首。
// 本地筛只能筛**已经拉下来的那几页**——用户在一个 800 首歌的收藏夹里搜
// 「告白气球」，本地筛的结果是「前 20 首里没有」，而真相是它在第 300 首。
// 症状是「搜了没反应」，而界面上没有任何地方说得出为什么。
//
// B 站这个接口自己就支持 `keyword`（上游 `service/fav-resource.ts:9` 的
// `FavResourceListRequestParams`），所以词直接发给它。
func (c *Client) CollectionMedias(ctx context.Context, id int64, sort CollectionSort, keyword string, page, ps int) (*CollectionPage, error) {
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
	// ⚠️ **空词不发这个参数**（上游 `service/fav-resource.ts` 的
	// `keyword: keywordValue || undefined` 是同一个意思）：发一个空的
	// `keyword=` 和不发它**不一定是一回事**，而多带一个参数没有好处。
	if keyword != "" {
		q.Set("keyword", keyword)
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
			Title: decodeEntities(m.Title), Cover: normalizeImageURL(m.Cover), Owner: decodeEntities(m.Upper.Name), OwnerMid: m.Upper.Mid,
			// ⚠️ B 站的 duration 是**秒**（浮点）。当毫秒解的话一首三分钟的
			// 歌会显示成 3 秒，而且界面上没有任何地方会露出错。
			Duration: time.Duration(float64(m.Duration) * float64(time.Second)),
			Attr:     m.Attr,
			// 播放量在 `cnt_info.play`（上游 `FavMediaCntInfo`）。零值是
			// 「没有这个数」——音轨那一条本来就没有，界面留空（见 Media.View）。
			View: m.CntInfo.Play,
			// 两个时间都是 Unix 秒；`unixSeconds` 把 0 / 负数收成零值，
			// 于是「没给」和「1970 年」分得开。
			FavTime: unixSeconds(m.FavTime),
			PubTime: unixSeconds(m.PubTime),
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

// CleanInvalidResources 让 B 站把这个收藏夹里**已经失效**的条目清掉。
//
// # 为什么不在本地筛一遍再删
//
// 最直观的写法是「拉全量 → 按 `Media.Attr` 挑出失效的 → `RemoveCollectionMedias`」，
// 而那个写法有**三个各自都会出错的地方**：
//
//  1. `Attr` 认得的只有 0 / 1 / 9。B 站后来又加过别的失效标记，照 `Attr` 挑会
//     漏掉一批——表现是「点完还有几个灰的」，而用户以为已经清干净了。
//  2. 要翻完所有页。一个几千首的收藏夹是几十趟请求（20 一页），这一趟跑几十秒。
//  3. 翻页期间**列表会变**（用户自己在删、或者 B 站那边过期），于是翻到的不是
//     全量——漏掉的那几条和第 1 条叠在一起，症状一样。
//
// 服务端这一个动作只带一个 `media_id`，B 站自己认得哪几条失效了（照 Biu 上游
// 的 `postFavResourceClean`，`service/fav-resource-clean.ts`）。它认得的比我们多。
//
// ⚠️ **只对收藏夹有效**：和 `RemoveCollectionMedias` 同一个道理，合集 / 系列是
// 建在稿件上的，B 站没有这个动作。
//
// **界面那边只判所有权、不判容器类型**（见 `app.CanDeleteCollection` 的注释，
// 以及 `DeleteFolder` 上「界面上不做类型判断」那段）：自己建的合集 / 系列上，
// 这一项照样摆得出来，而 B 站会回一句它不认识。合集 / 系列在 URL 上是
// `/collection/{id}?type=21`，而**今天界面上没有一行点得进去会拼出那个查询串**
// ——侧栏「我收藏的」那一组只收收藏夹（见 `app.collectedFavorites` 的注释：
// 合集的内容列表是另一套接口，本项目没接，摆进去是死的路）。要手敲 URL 才碰
// 得到，所以这一条现在只是**注释里的话必须是真的**，不是一个活着的缺陷。
func (c *Client) CleanInvalidResources(ctx context.Context, id int64) error {
	if id <= 0 {
		return fmt.Errorf("bili: 收藏夹 id 不对（%d）", id)
	}
	// ⚠️ **不带 platform**：上游这一个也没带（B 站不认它），多给不报错但那
	// 一次请求的报文就和上游的不一样了，而两个一漂就得分不清是谁的问题。
	if _, err := c.Do(ctx, c.writeCall(c.hostAPI+pathResourceClean, url.Values{
		"media_id": {strconv.FormatInt(id, 10)},
	})); err != nil {
		return fmt.Errorf("bili: %s %d: %w", pathResourceClean, id, err)
	}
	return nil
}

// writeCall 造一个写接口的调用，**并额外把 csrf 显式塞进表单体**。
//
// ⚠️ **表单体这一处是 `writeCall` 自己做的，不是 `Call.CSRF` 的自动行为**：
// `Call{CSRF: true}` 只把 csrf 放进查询串和 `bili-jct` 头（`Client.send` 与
// `setHeaders`），`Form` 那张表原样发出去。所以这里才要 `form.Set("csrf", v)`。
//
// 这么写是照 Biu 上游的 `request-interceptors.ts`——它把 csrf 塞进 POST 的
// body 里。而**同一份代码里另有一族写接口没放体里那份**（直接写
// `Call{Form: ..., CSRF: true}` 的 `LikeArchive` / `LikeTriple` / `dealFavMedia` /
// `PlayReportOpen`），它们在真机上也通。⇒ 体里这一份是「照上游抄」，不是
// 「少它就 -111」：两种写法并存，别把这句话套到别处。
//
// ⚠️ **-111（`ErrCSRF`）在真机上有一半的概率被报成「风控」**（见 ErrCSRF 的
// 注释），所以这一族真撞上 -111 时先怀疑 csrf 有没有到，别一开始就往风控查。
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
