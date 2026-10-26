// 推荐页要的那四个接口（Biu 的 `/`，`src/pages/music-recommend/`）。
//
// # 三个 Tab 是**两套**接口，这是上游定的，不是我们分的
//
//	Biu `music-recommend/index.tsx:24-27`
//	  music / guichu → getRegionFeedRcmd  分区推荐
//	  pop            → getMusicComprehensiveWebRank  音乐综合榜
//
// 换句话说「流行」那一档**不是分区推荐的第三档**，它是另一张榜。上游的理由在
// `homepage.go` 的 `homePopTab` 那段注释里也写了：榜上没有的东西新歌速递
// 没有，所以新歌速递只画在「流行」上。抄的时候别把三档写成同一个接口的三个
// `from_region`——B 站根本没有「流行分区推荐」这个说法。
//
// # 参数逐个抄上游，别凭印象写
//
//	RegionFeed（`web-interface-region-feed-rcmd.ts:12-22`）
//	  display_id    页码（**不是 pn**；名字像游标，用的就是页码）
//	  request_cnt   一页几条，上游 15
//	  from_region   主分区号，音乐 1003 / 鬼畜 1007
//	  device        "web"
//	  plat          30
//	  web_location  "333.40138"
//
//	MusicRank（`music-comprehensive-web-rank.ts:8-16`）
//	  pn / ps / web_location "333.1351"
//
// # 🔴 哪几个要签名
//
// 分区推荐**要 WBI 签名**（上游 `useWbi: true`）。不签的表现是被风控挡掉，
// 而挡掉时报的是 -403——和「没权限」长得一模一样。
//
// 音乐榜和两个新歌速递接口**不签名**，但**带 csrf**（上游 `useCSRF: true`）：
// 那个拦截器对 GET 是把 csrf 塞进查询串（`request-interceptors.ts:21-30`），
// 没登录时那个值是空的，发过去也就是少一个参数——不致命，但抄成不传就和
// 上游不是一个请求了。
//
// # 「排序」不在这一层
//
// 三档都**按 B 站给的顺序原样排**，一层都不重排：
//
//   - 分区推荐是**个性化的**推荐流，B 站按你的口味排的顺序本身就是内容。
//     按播放量或者发布时间重排一遍等于把它变成一个热门榜，而「热门」是
//     上面第三档（音乐榜）已经在干的事。
//   - 音乐榜的名次就在返回的顺序里，重排等于把榜变成一锅粥。
//
// 真要改顺序，那是以后的事，且得先想清楚「改了之后这还是不是推荐」。
package bili

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// 这四个接口的路径。
const (
	// regionFeedPath 是分区推荐。**要 WBI 签名**。
	regionFeedPath = "/x/web-interface/region/feed/rcmd"
	// musicRankPath 是音乐综合榜（「流行」那一档的推荐单）。
	musicRankPath = "/x/centralization/interface/music/comprehensive/web/rank"
	// newMusicPath 是新歌速递的列表。
	newMusicPath = "/x/centralization/interface/new/music"
	// newMusicBannerPath 是新歌速递的头图位。**排在列表前面**（见 NewMusic）。
	newMusicBannerPath = "/x/centralization/interface/new/music/banner"
)

// B 站的主分区号。出处：`bilibili-API-collect` 的
// `docs/video/video_zone_v2.md`（音乐 music 1003、鬼畜 kichiku 1007）。
//
// ⚠️ 这两个数**不是** `tid`（旧分区号）。B 站在 v2 里换了号，而分区推荐收的
// 是 v2 那一套——发 17（音乐的旧 tid）不会报错，只会给一个别的分区的推荐。
const (
	// RegionMusic 是音乐区。
	RegionMusic = 1003
	// RegionGuichu 是鬼畜区。
	RegionGuichu = 1007
)

const (
	// regionFeedDefaultCount 是分区推荐一页几条。抄上游的
	// `REGION_PAGE_SIZE = 15`（`music-recommend/index.tsx:22`）。
	regionFeedDefaultCount = 15
	// musicRankDefaultCount 是音乐榜一页几条。抄上游的 `PAGE_SIZE = 20`
	// （`music-recommend/index.tsx:20`）。
	musicRankDefaultCount = 20

	// regionFeedWebLocation 是分区推荐的 web_location。抄上游
	// `REGION_WEB_LOCATION = "333.40138"`。
	regionFeedWebLocation = "333.40138"
	// musicWebLocation 是音乐榜 / 新歌速递那几个的 web_location。抄上游
	// `music-comprehensive-web-rank.ts` 的默认 333.1351 和
	// `web-interface-new-music.ts` 里那个一样的 333.1351。
	musicWebLocation = "333.1351"
)

// RecommendItem 是一条推荐。
//
// ⚠️ **这里没有 ID 字段**：行身份（🔴 铁律四要的「稳定且唯一」）由上一层按
// 一条写死的顺序算出来（`app.RecommendItem.ID`），理由和 `library.go` 文件头
// 那一条一样——「什么算一条」是同一件事的两个界面共用的判断，放在这一层
// 就只有一处写法。这一层只负责把 B 站给的几个身份字段原样带上来。
type RecommendItem struct {
	// Bvid 是稿件 id。**有它才放得了这一首**（见 `app.playableOnline`）。
	Bvid string
	// Aid 是稿件的数字 id。收藏要用它（`/x/v2/fav/resource/deal` 收的是 rid）。
	Aid int64
	// TrackID 是「拿不到稿件时还能用的身份」：分区推荐给的是 `trackid`
	// （推的是歌不是稿件），音乐榜给的是榜上那一条的 `id`。
	//
	// 它排在 Bvid / Aid 后面（见 app 层那个 ID 方法），所以它只在上面两个
	// 都缺的时候才轮上——而那种条目**画得出来、放不了**。
	TrackID string
	// Cid 是分 P id。取流不给它会取到第一集（多 P 投稿放错集且不报错）。
	Cid int64
	// Title / Cover / Owner 是卡片上那三行。
	Title string
	Cover string
	Owner string
	// OwnerMid 是 UP 的 mid（收藏夹、回空间要用）。
	OwnerMid int64
	// View 是播放量。分区推荐和音乐榜都给，歌单不给。
	View int64
	// Duration 是时长。音乐榜那个是**秒**（`related_archive.duration`）。
	Duration time.Duration
}

// errReadRecommend 是「HTTP 200、code 0，但 data 的形状不是我们认识的那个」。
//
// 跟网络错、风控都不是一回事：重试同一个请求只会再拿到同一段看不懂的 JSON。
// 分开是为了界面上说得出「B 站改了接口」而不是「网络不好，换个网试试」。
var errReadRecommend = errors.New("bili: 推荐接口的返回读不懂")

// flexInt64 是「B 站有时发字符串、有时发数字」的那种 id。
//
// # 为什么这几个字段要它
//
// B 站同一个接口里就混着两种形态。真实样件（Biu 上游 `src/service/service.md`
// 里 `music/comprehensive/web/rank` 那一份**原样**返回的参数）:
//
//	"cid": "32447137298"          ← 字符串
//	"aid": "115182114246511"      ← 字符串
//	"uid": 384580074              ← 数字
//	"vv_count": 75, "duration": 179, "id": 3593581
//
// 同一族的 `musician/list` 连 `uid` 也是字符串（`"uid": "1490901245"`）。
//
// ⚠️ **按数字解会整档拉不出来**：`json.Unmarshal` 撞上字符串直接报
// `cannot unmarshal string into Go struct field ... of type int64`，而我们把
// 任何解码错都归成 `errReadRecommend`——于是「流行」那一档永远画成「这一块
// 没拉出来 / 推荐接口的返回读不懂」，而新歌速递的列表那一半**静默**丢掉（两半
// 里只成功一半是预期行为，见 `app.Recommend.NewMusic`）。
//
// 症状与原因隔了三层（服务层 → 中间层 → 界面），所以**这里两种形态都认**，
// 而不是照着某一份样件把字段改成字符串——B 站哪天换回去也不会再坏一次。
//
// ⚠️ 只有**那些真会变成字符串**的字段用它（aid / cid / uid）。播放量、时长、
// 分数在两份样件里都是数字，保持 `int64`——满屏容错类型等于没有类型。
type flexInt64 int64

// UnmarshalJSON 认字符串和数字两种写法。
func (f *flexInt64) UnmarshalJSON(b []byte) error {
	raw := strings.Trim(string(b), `"`)
	if raw == "" || raw == "null" {
		*f = 0
		return nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		// 报错而不是当 0：一个不是数字的 id 当成 0，界面上是「这一条没有身份」
		// 被丢掉，而用户看到的是「B 站今天没推东西」。
		return fmt.Errorf("这个 id 读不懂：%q", raw)
	}
	*f = flexInt64(n)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 分区推荐（音乐 / 鬼畜两个 Tab）
// ─────────────────────────────────────────────────────────────────────────────

// RegionFeedQuery 是分区推荐一页的查询条件。
type RegionFeedQuery struct {
	// Page 是页码，从 1 起。发到 `display_id` 上。
	//
	// ⚠️ **它不是游标**：名字听着像，但上游传的就是页码
	// （`music-recommend/index.tsx:101` 的 `display_id: pn`）。跟着传游标
	// 会拿到随机的一页。
	Page int
	// Count 是一页几条。零值按上游的 15。
	Count int
	// Region 是主分区号，零值按音乐（1003）。
	//
	// 用 `RegionMusic` / `RegionGuichu`，别写裸数字——那两个数在 B 站的
	// v2 分区表里，而裸数字在这份文件之外就没人知道它是哪一区了。
	Region int
}

// regionFeedData 是分区推荐的 data 段。
//
// `archives` 是**可空**的：B 站在这一档放不下东西时给 `null` 而不是 `[]`，
// 而 `json.Unmarshal` 解到 `null` 会把切片留成 nil——那是「没有」，不是错。
type regionFeedData struct {
	Archives []struct {
		Aid      flexInt64 `json:"aid"`
		Bvid     string    `json:"bvid"`
		Cid      flexInt64 `json:"cid"`
		Title    string    `json:"title"`
		Cover    string    `json:"cover"`
		Duration int64     `json:"duration"` // 秒
		TrackID  string    `json:"trackid"`
		Author   struct {
			Mid  flexInt64 `json:"mid"`
			Name string    `json:"name"`
		} `json:"author"`
		Stat struct {
			View int64 `json:"view"`
		} `json:"stat"`
	} `json:"archives"`
}

// RegionFeed 拉一页分区推荐。
//
// **要 WBI 签名**：WBI 的密钥是先打一次 nav 拿的，所以没签名跑不起来的
// 表现是「签名这一步就报错」，连真正的请求都没发出去。
func (c *Client) RegionFeed(ctx context.Context, q RegionFeedQuery) ([]RecommendItem, error) {
	region := q.Region
	if region == 0 {
		region = RegionMusic
	}
	count := q.Count
	if count <= 0 {
		count = regionFeedDefaultCount
	}

	v := url.Values{}
	v.Set("display_id", strconv.Itoa(max(q.Page, 1)))
	v.Set("request_cnt", strconv.Itoa(count))
	v.Set("from_region", strconv.Itoa(region))
	v.Set("device", "web")
	v.Set("plat", "30")
	v.Set("web_location", regionFeedWebLocation)

	raw, err := c.Do(ctx, Call{
		Method: "GET",
		URL:    c.hostAPI + regionFeedPath,
		Query:  v,
		WBI:    true,
	})
	if err != nil {
		return nil, err
	}

	var d regionFeedData
	if err := json.Unmarshal(raw, &d); err != nil {
		// HTTP 200、code 0，但 data 的形状变了。不要当成网络错——重试没用。
		return nil, errReadRecommend
	}
	return regionFeedPage(d), nil
}

// regionFeedPage 把解出来的分区推荐变成结果。
//
// 拆出来是为了能直接喂 fixture（和 `toViewPageOf` 同一个理由）。
func regionFeedPage(d regionFeedData) []RecommendItem {
	out := make([]RecommendItem, 0, len(d.Archives))
	for _, it := range d.Archives {
		out = append(out, RecommendItem{
			Bvid: it.Bvid, Aid: int64(it.Aid), TrackID: it.TrackID, Cid: int64(it.Cid),
			Title: decodeEntities(it.Title), Cover: normalizeImageURL(it.Cover),
			Owner: decodeEntities(it.Author.Name), OwnerMid: int64(it.Author.Mid),
			View: it.Stat.View, Duration: seconds(it.Duration),
		})
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// 音乐综合榜（「流行」那一档）
// ─────────────────────────────────────────────────────────────────────────────

// MusicRankQuery 是音乐榜一页的查询条件。
type MusicRankQuery struct {
	// Page / PageSize 就是上游的 pn / ps。Page 从 1 起。
	Page     int
	PageSize int
}

// musicRankData 是音乐榜的 data 段。
type musicRankData struct {
	List []struct {
		Id         int64     `json:"id"`
		MusicID    string    `json:"music_id"`
		MusicTitle string    `json:"music_title"`
		Bvid       string    `json:"bvid"`
		Aid        flexInt64 `json:"aid"`
		Cid        flexInt64 `json:"cid"`
		Cover      string    `json:"cover"`
		Author     string    `json:"author"`
		// RelatedArchive 是这个榜条目挂的那条稿件。
		//
		// ⚠️ **它经常是空的**：榜上有些条目是纯音乐条目（歌单/OST），B 站
		// 没给它挂稿件。这时歌名、封面、UP 名都退回顶层那几个字段——而顶层
		// 那几个大多数时候也是空的，于是那种条目**什么都画不出来**。
		RelatedArchive *struct {
			Bvid     string    `json:"bvid"`
			Cid      flexInt64 `json:"cid"`
			Cover    string    `json:"cover"`
			Title    string    `json:"title"`
			UID      flexInt64 `json:"uid"`
			Username string    `json:"username"`
			VvCount  int64     `json:"vv_count"`
			Duration int64     `json:"duration"` // 秒
		} `json:"related_archive"`
	} `json:"list"`
}

// MusicRank 拉一页音乐综合榜。
//
// **不签名**（上游那一处没写 `useWbi`）。这一层不发 csrf——上游也没发。
func (c *Client) MusicRank(ctx context.Context, q MusicRankQuery) ([]RecommendItem, error) {
	ps := q.PageSize
	if ps <= 0 {
		ps = musicRankDefaultCount
	}

	v := url.Values{}
	v.Set("pn", strconv.Itoa(max(q.Page, 1)))
	v.Set("ps", strconv.Itoa(ps))
	v.Set("web_location", musicWebLocation)

	raw, err := c.Do(ctx, Call{
		Method: "GET",
		URL:    c.hostAPI + musicRankPath,
		Query:  v,
	})
	if err != nil {
		return nil, err
	}

	var d musicRankData
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, errReadRecommend
	}
	return musicRankPage(d), nil
}

// musicRankPage 把解出来的音乐榜变成结果。拆出来是为了能直接喂 fixture。
func musicRankPage(d musicRankData) []RecommendItem {
	out := make([]RecommendItem, 0, len(d.List))
	for _, it := range d.List {
		item := RecommendItem{
			Bvid: it.Bvid, Aid: int64(it.Aid), Cid: int64(it.Cid),
			Title: decodeEntities(it.MusicTitle), Cover: normalizeImageURL(it.Cover),
			Owner: decodeEntities(it.Author),
			// 榜上那一条自己的 id：上面两个身份都缺的时候它是最后一个。
			TrackID: strconv.FormatInt(it.Id, 10),
		}
		// 挂着的稿件优先：歌名、封面、UP 名、时长、播放量以它为准
		//（上游 `normalizeRankItem` 的同一个取舍，music-recommend/index.tsx:31-45）。
		if ra := it.RelatedArchive; ra != nil {
			if ra.Title != "" {
				item.Title = ra.Title
			}
			if ra.Cover != "" {
				item.Cover = normalizeImageURL(ra.Cover)
			}
			if ra.Bvid != "" {
				item.Bvid = ra.Bvid
			}
			if ra.Cid != 0 {
				item.Cid = int64(ra.Cid)
			}
			// 🔴 **UP 名和它的编号必须来自同一处**（票 #360 的评审）。
			//
			// 榜上那一条的顶层 `author` 和挂着的那条稿件的 `username` **可能是
			// 两个人**（榜上有些条目挂的是别人的稿件）。而编号只有稿件那一处给
			// （`uid`）。名字取顶层的、编号取稿件的，界面上点那个名字就会跳到
			// **另一个人的空间**——而画面上看不出任何异样（那行字还是那个字）。
			//
			// 所以：**名字换成稿件那一处的，编号也跟着换**；换不了（稿件没给名字）
			// 就**两个都不取**——那一行照样显示顶层那个 UP 名，只是**点不动**
			//（`app.Item.ArtistMid` 的注释写着这一条：没有编号时名字照样有值）。
			if ra.Username != "" {
				item.Owner = ra.Username
				item.OwnerMid = int64(ra.UID)
			}
			item.View = ra.VvCount
			item.Duration = seconds(ra.Duration)
		}
		out = append(out, item)
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// 新歌速递
// ─────────────────────────────────────────────────────────────────────────────

// newMusicData 是新歌速递**列表**的 data 段。
type newMusicData struct {
	List []struct {
		Id         int64     `json:"id"`
		MusicID    string    `json:"music_id"`
		MusicTitle string    `json:"music_title"`
		Bvid       string    `json:"bvid"`
		Aid        flexInt64 `json:"aid"`
		Cid        flexInt64 `json:"cid"`
		Cover      string    `json:"cover"`
		Author     string    `json:"author"`
	} `json:"list"`
}

// newMusicBannerData 是新歌速递**头图位**的 data 段。
//
// ⚠️ 它的标题字段叫 `archive_title`，不是 `music_title`——B 站这个接口把
// 歌和稿件分开了，而上游正是因此写了两段 normalize
// （`new-music-top.tsx:66-73` 与 `:75-88`）。
type newMusicBannerData struct {
	List []struct {
		MusicID      string    `json:"music_id"`
		ArchiveTitle string    `json:"archive_title"`
		Bvid         string    `json:"bvid"`
		Aid          flexInt64 `json:"aid"`
		Cid          flexInt64 `json:"cid"`
		Cover        string    `json:"cover"`
		Author       string    `json:"author"`
	} `json:"list"`
}

// NewMusic 拉新歌速递的列表那一半。
//
// 另一半是 `NewMusicBanner`，**两个都要**：上游把它们并发发出去，然后按
// 「头图位在前、列表在后、按身份去重」的顺序合成一摊
// （`new-music-top.tsx:58-104`）。合在哪一层见 `app.Recommend.NewMusic`。
func (c *Client) NewMusic(ctx context.Context) ([]RecommendItem, error) {
	v := url.Values{}
	v.Set("plat", "2")
	v.Set("web_location", musicWebLocation)

	raw, err := c.Do(ctx, Call{
		Method: "GET",
		URL:    c.hostAPI + newMusicPath,
		Query:  v,
		CSRF:   true,
	})
	if err != nil {
		return nil, err
	}

	var d newMusicData
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, errReadRecommend
	}
	out := make([]RecommendItem, 0, len(d.List))
	for _, it := range d.List {
		out = append(out, RecommendItem{
			Bvid: it.Bvid, Aid: int64(it.Aid), Cid: int64(it.Cid),
			Title: decodeEntities(it.MusicTitle), Cover: normalizeImageURL(it.Cover),
			Owner: decodeEntities(it.Author),
			// 身份的最后那一格：这个接口给的 `id` 是歌的 id，不是稿件的。
			TrackID: strconv.FormatInt(it.Id, 10),
		})
	}
	return out, nil
}

// NewMusicBanner 拉新歌速递的头图位那一半。
func (c *Client) NewMusicBanner(ctx context.Context) ([]RecommendItem, error) {
	v := url.Values{}
	v.Set("web_location", musicWebLocation)

	raw, err := c.Do(ctx, Call{
		Method: "GET",
		URL:    c.hostAPI + newMusicBannerPath,
		Query:  v,
		CSRF:   true,
	})
	if err != nil {
		return nil, err
	}

	var d newMusicBannerData
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, errReadRecommend
	}
	out := make([]RecommendItem, 0, len(d.List))
	for _, it := range d.List {
		out = append(out, RecommendItem{
			Bvid: it.Bvid, Aid: int64(it.Aid), Cid: int64(it.Cid),
			Title: decodeEntities(it.ArchiveTitle), Cover: normalizeImageURL(it.Cover),
			Owner:   it.Author,
			TrackID: it.MusicID,
		})
	}
	return out, nil
}
