// 动态流：关注的人在发什么，和某一个 UP 空间里的动态。
//
// # ⚠️ 这一层的头号约定：不认识的动态**丢掉**，不报错
//
// B 站的动态类型**远不止三种**（上游 `src/common/constants/feed.ts` 的
// `DynamicType` 枚举列了 20 多种：转发、剧集、课程、专栏、音乐、直播、
// 收藏夹、竖屏、广告、小程序……）。而这一版只画三种：纯文字、图文、投稿。
//
// 丢掉的那一条用户顶多少看一条，而抛错的话是**整个动态流挂掉**——
// 一条转发就把整页变成错误页，而 B 站每天都在发转发。所以：
//
//   - 单条解析不出来（JSON 读不懂、类型不认识、正文和图都没有）
//     → `parseDynamic` 返回 ok=false，调用方跳过它，页面上什么都不说。
//   - 只有**整个 data 段读不懂**（协议真的变了）才算错，往上抛。
//
// 丢掉的条数记在 `DynamicPage.Dropped` 里：它不是为了给用户看的，是为了
// 「界面上一个格子都没有」这件事能说清是「他真的没发」还是「我们把他的
// 转发全丢了」。上游在 `dynamic-feed` 那一页**没有**按类型过滤（它整页都
// 按视频画），本项目在 UP 空间里过滤了 `DYNAMIC_TYPE_AV`
// （`user-profile/dynamic-list/index.tsx:48`）——那是另一套取舍：那一页要的是
// 「这个人发的视频」，这里要的是「这一条画得出来」。
//
// # 翻页是 offset，不是页码
//
// 动态接口下一页的入口是返回里的 `offset` 字段（一个不透明的串），不是
// `pn`。**把它当成页码数会拿到重复的那一页**：B 站不保证 offset 单调，
// 而我们能做的只有「B 站给什么就原样带回去」——所以它在这里是 `string`。
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

// 动态那几个接口的路径。
const (
	// dynamicFeedAllPath 是全部动态（关注的人在发什么）。
	dynamicFeedAllPath = "/x/polymer/web-dynamic/v1/feed/all"
	// dynamicFeedSpacePath 是某一个 UP 空间里的动态。**要签名**。
	dynamicFeedSpacePath = "/x/polymer/web-dynamic/v1/feed/space"
	// followingsPath 是「我关注的人」分页列表。动态流左侧作者栏用它。
	followingsPath = "/x/relation/followings"
)

// dynamicKind 是 B 站 `major.type` 里我们会遇到的三种。
//
// 我们按 **major 装了什么**判类型，不按 `major.type` 的字符串判：
// 那个字符串 B 站改过一次（`MAJOR_TYPE_OPUS` / `MAJOR_TYPE_DRAW` 是同一个
// 图文类型的新旧两个名字），按它判的话新旧两种会分叉成两种类型，
// 界面上同一个东西画出两种卡片。
const (
	// majorTypeArchive 是投稿视频。
	majorTypeArchive = "MAJOR_TYPE_ARCHIVE"
	// majorTypeOpus 是图文（新名字）。
	majorTypeOpus = "MAJOR_TYPE_OPUS"
	// majorTypeDraw 是图文（旧名字，现在 B 站给的是 opus）。
	majorTypeDraw = "MAJOR_TYPE_DRAW"
)

// DynamicKind 是我们画得出的三种动态。
type DynamicKind uint8

const (
	// DynamicText 是纯文字。
	DynamicText DynamicKind = iota
	// DynamicOpus 是图文：正文 + 若干张图。
	DynamicOpus
	// DynamicVideo 是投稿视频：一张封面 + 标题 + 简介。
	DynamicVideo
)

// String 是给日志看的名字。界面显示的话另有各处自己的文案。
func (k DynamicKind) String() string {
	switch k {
	case DynamicText:
		return "纯文字"
	case DynamicOpus:
		return "图文"
	case DynamicVideo:
		return "投稿视频"
	}
	return fmt.Sprintf("未知的动态类型 %d", uint8(k))
}

// DynamicAuthor 是一条动态的作者。
type DynamicAuthor struct {
	Mid  int64
	Name string
	Face string
}

// DynamicArchive 是一条投稿型动态里那份稿件。
//
// 名字跟着 B 站的 `major.archive` 走，不叫 DynamicVideo —— 那一个名字已经是
// 「动态类型」那一档了（`DynamicVideo` 常量）。
type DynamicArchive struct {
	Bvid  string
	Title string
	Cover string
	// Duration 是时长文字（`"3:20"`），B 站给的是写好的。
	Duration string
	// Play 是播放数的文字。
	Play string
	// Desc 是简介。
	Desc string
}

// Dynamic 是一条**画得出来**的动态。
//
// ⚠️ `ID` 是这一行的身份（铁律四）。它取的是 B 站的 `id_str` 而不是下标：
// 动态流是「拿一页追加一页」，而 B 站在两页之间会把新动态插到前面——
// 拿序号当身份的话，用户盯着的那条会整个换成旁边那一条。
type Dynamic struct {
	ID     string
	Kind   DynamicKind
	Author DynamicAuthor
	// Time 是发布时间。优先用 B 站给的文字（`pub_time`，形如「2 小时前」），
	// 它是算过了的；没有才退回 Unix 秒。
	Time string
	// Text 是正文。
	Text string
	// Pics 是图文的图。空串不是「没图」而是「这一条没有能画的东西」，
	// 由 Kind 决定画不画。
	Pics []string
	// Video 是投稿。Kind 不为 DynamicVideo 时是 nil。
	Video *DynamicArchive
}

// DynamicPage 是一页动态。
type DynamicPage struct {
	Items []Dynamic
	// Offset 是下一页的入口。**原样带回接口**，不要解析它。
	Offset string
	// HasMore 后面还有没有。
	HasMore bool
	// Dropped 是这一页里有几条我们画不出来、丢掉了。
	//
	// 它不进界面（用户不需要知道 B 站有多少种动态），但它是「这一页一个格子
	// 都没有」这件事唯一能说清原因的东西：Dropped > 0 说明是他发的那些我们
	// 都画不出来，而不是「他没发」。
	Dropped int
}

// dynamicEnvelope 是动态接口的外壳。
type dynamicEnvelope struct {
	Items   []json.RawMessage `json:"items"`
	Offset  string            `json:"offset"`
	HasMore bool              `json:"has_more"`
}

// DynamicFeed 拉「关注的人在发什么」，一页。
//
// offset 空串表示第一页。⚠️ 这个接口**不要签名**（上游
// `web-dynamic.ts:331-334` 没传 useWbi）。
func (c *Client) DynamicFeed(ctx context.Context, offset string) (DynamicPage, error) {
	params := url.Values{}
	// 上游这两个参数是写死的（`dynamic-feed/index.tsx:63-72`）：
	// type=video 是「按视频那条取」，platform=web 告诉 B 站我们要网页版的形状。
	// 不给的话 B 站会给 App 的形状，字段位置不一样。
	params.Set("type", "video")
	params.Set("platform", "web")
	if offset != "" {
		params.Set("offset", offset)
	}

	raw, err := c.Do(ctx, Call{Method: "GET", URL: c.hostAPI + dynamicFeedAllPath, Query: params})
	if err != nil {
		return DynamicPage{}, err
	}
	return dynamicPageFrom(raw)
}

// SpaceDynamics 拉某一个 UP 空间里的动态，一页。
//
// ⚠️ 这个接口**要签名**（上游 `web-dynamic.ts:350-354` 传了 useWbi，
// 而 feed/all 没有）——这条差别是这一层最容易抄错的一处。
func (c *Client) SpaceDynamics(ctx context.Context, mid int64, offset string) (DynamicPage, error) {
	if mid <= 0 {
		return DynamicPage{}, fmt.Errorf("bili: 空间 id 不对（mid=%d）", mid)
	}
	params := url.Values{}
	params.Set("host_mid", strconv.FormatInt(mid, 10))
	params.Set("type", "video")
	params.Set("platform", "web")
	if offset != "" {
		params.Set("offset", offset)
	}

	raw, err := c.Do(ctx, Call{
		Method: "GET", URL: c.hostAPI + dynamicFeedSpacePath, Query: params, WBI: true,
	})
	if err != nil {
		return DynamicPage{}, err
	}
	return dynamicPageFrom(raw)
}

// dynamicPageFrom 把一页返回变成 DynamicPage。
//
// ⚠️ **单条读不懂不算错**：这是那个包住「整个动态流」的约定，见文件头。
// 一条转发、一条广告、一条 B 站新加的类型，都只是这一页少一行。
func dynamicPageFrom(raw []byte) (DynamicPage, error) {
	var env dynamicEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		// 连 items 数组都解不出来 = 协议真的变了，重试没用，如实说。
		return DynamicPage{}, fmt.Errorf("bili: 动态流的返回读不懂：%w", err)
	}

	pg := DynamicPage{Offset: env.Offset, HasMore: env.HasMore}
	for _, item := range env.Items {
		d, ok := parseDynamic(item)
		if !ok {
			pg.Dropped++
			continue
		}
		pg.Items = append(pg.Items, d)
	}
	return pg, nil
}

// rawPic 是图文里的一张图。
//
// ⚠️ **B 站对同一批图给过两个字段名**：老的那套（`MAJOR_TYPE_DRAW` 时代）
// 叫 `src`，新的（`MAJOR_TYPE_OPUS`）叫 `url_src`。两个都认，取到哪个算
// 哪个——只认一个的话，另一批图文在界面上是「一张图都没有」的空卡片。
type rawPic struct {
	Src    string `json:"src"`
	URLSrc string `json:"url_src"`
}

// URL 是这张图的地址，两个字段名里取有值的那个。
func (p rawPic) URL() string {
	if p.URLSrc != "" {
		return p.URLSrc
	}
	return p.Src
}

// rawDynamic 是解析一条动态时真正要读的那些字段。
//
// ⚠️ **只声明用得着的**：B 站这一条动态返回几百个字段（module_more、
// module_interaction、module_fold、module_dispute…），全声明进来等于把
// 「B 站加了一个字段」变成一次编译失败。这里少一个字段的代价是界面上少
// 一样东西，那是可以后补的。
type rawDynamic struct {
	IDStr string `json:"id_str"`
	// Type 是动态的大类（`DYNAMIC_TYPE_WORD` 那些）。**这里不用它**：
	// 判类型看的是 major 装了什么，见 parseDynamic 的注释。
	Type    string `json:"type"`
	Modules struct {
		Author struct {
			Mid     int64  `json:"mid"`
			Name    string `json:"name"`
			Face    string `json:"face"`
			PubTime string `json:"pub_time"`
			PubTS   int64  `json:"pub_ts"`
		} `json:"module_author"`
		Dynamic struct {
			Desc struct {
				Text string `json:"text"`
			} `json:"desc"`
			Major struct {
				Type    string `json:"type"`
				Archive *struct {
					Bvid        string `json:"bvid"`
					Title       string `json:"title"`
					Cover       string `json:"cover"`
					Desc        string `json:"desc"`
					DurationTxt string `json:"duration_text"`
					Stat        struct {
						Play string `json:"play"`
					} `json:"stat"`
				} `json:"archive"`
				Opus *struct {
					Summary struct {
						Text string `json:"text"`
					} `json:"summary"`
					Pics []rawPic `json:"pics"`
				} `json:"opus"`
			} `json:"major"`
		} `json:"module_dynamic"`
	} `json:"modules"`
}

// parseDynamic 把一条原始动态变成 Dynamic。第二个返回值是「画得出来吗」。
//
// # 判类型按 major 装了什么，不按 `major.type` 的字符串
//
// B 站把同一个图文类型写过两个名字（`MAJOR_TYPE_DRAW` → `MAJOR_TYPE_OPUS`）。
// 按字符串判的话，同一个东西在新旧两种返回里会分叉成两种类型，界面上是两种
// 卡片。而 `archive` / `opus` 这两个**字段本身**是稳定的：图文一定有 pics，
// 投稿一定有 bvid。
//
// # 什么都不画的和画不出来的，都返回 false
//
// 三种类型都判完之后，如果正文是空、图是空、视频也是 nil——
// 那就是一条我们没有对应画法的（比如「他转发了一条我们画不出来的动态」，
// 转发件里只留了个壳）。返回 false，界面上不占一格。
func parseDynamic(raw json.RawMessage) (Dynamic, bool) {
	var r rawDynamic
	if err := json.Unmarshal(raw, &r); err != nil {
		// 这一条的 JSON 读不懂。不算错——B 站的返回是整页一个数组，
		// 一条坏掉不该让整页都没了。
		return Dynamic{}, false
	}
	if r.IDStr == "" {
		// 没有 id 的行没有身份（铁律四），画出来也点不开。
		return Dynamic{}, false
	}

	d := Dynamic{
		ID: r.IDStr,
		Author: DynamicAuthor{
			Mid:  r.Modules.Author.Mid,
			Name: r.Modules.Author.Name,
			Face: r.Modules.Author.Face,
		},
		Time: dynamicTime(r.Modules.Author.PubTime, r.Modules.Author.PubTS),
		Text: strings.TrimSpace(r.Modules.Dynamic.Desc.Text),
	}

	major := r.Modules.Dynamic.Major

	// ① 投稿视频。三种里优先级最高：一个动态里同时有图和视频时，
	// 画视频那一块（它是这条动态真正在推的东西）。
	if a := major.Archive; a != nil && a.Bvid != "" {
		d.Kind = DynamicVideo
		d.Video = &DynamicArchive{
			Bvid: a.Bvid, Title: a.Title, Cover: a.Cover,
			Duration: a.DurationTxt, Play: a.Stat.Play, Desc: a.Desc,
		}
		if d.Text == "" {
			d.Text = strings.TrimSpace(a.Desc)
		}
		return d, true
	}

	// ② 图文。正文**二选一**，不是两段拼起来。
	//
	// ⚠️ 这是实测过的：上游两处都是 `desc?.text || opus?.summary?.text`
	// （`dynamic-feed/item.tsx:33`、`user-profile/dynamic-list/item.tsx:46`），
	// 而 B 站的正常返回里 `desc.text` 和 `opus.summary.text` 是**同一段话**。
	// 拼起来（`desc + "\n" + summary`）的结果是界面上同一句话出现两遍。
	// 只在 `desc` 为空时才去取 summary。
	if o := major.Opus; o != nil {
		pics := make([]string, 0, len(o.Pics))
		for _, p := range o.Pics {
			if u := p.URL(); u != "" {
				pics = append(pics, u)
			}
		}
		if d.Text == "" {
			d.Text = strings.TrimSpace(o.Summary.Text)
		}
		if len(pics) == 0 && d.Text == "" {
			// 声明是图文但既没图也没字：画出来是一个空卡片，比不画更糟。
			return Dynamic{}, false
		}
		d.Kind = DynamicOpus
		d.Pics = pics
		return d, true
	}

	// ③ 纯文字。没有正文也没有图也没有视频——那就是我们画不出来的那种
	// （转发件、直播开播、小程序…）。丢掉。
	if d.Text == "" {
		return Dynamic{}, false
	}
	d.Kind = DynamicText
	return d, true
}

// dynamicTime 是发布时间。
//
// 优先用 B 站给的 `pub_time`（「3 分钟前」「昨天 20:15」）——那是它算好、
// 跟着本地时区翻过一遍的，自己从 Unix 秒算一遍只会和它差一个时区。
// 没有就退回 Unix 秒；两个都没有给空串，界面上那一句就不画。
func dynamicTime(pubTime string, pubTS int64) string {
	if pubTime != "" {
		return pubTime
	}
	if pubTS > 0 {
		return time.Unix(pubTS, 0).Format("2006-01-02 15:04")
	}
	return ""
}

// FollowAuthor 是动态流左侧作者栏里的一行。
type FollowAuthor struct {
	Mid  int64
	Name string
	Face string
}

// FollowPage 是「我关注的人」的一页。
type FollowPage struct {
	Authors []FollowAuthor
	// Total 一共关注了多少个。
	Total int
	// HasMore 后面还有没有。
	HasMore bool
}

// followingsDefaultSize 是一页几个作者。上游 `author-list.tsx:47` 用的是 20。
const followingsDefaultSize = 20

// Followings 拉「我关注的人」的一页。
//
// vmid 是**自己的** mid（不是页面上那个 UP 的）——作者栏列的是「我关注的人」。
// page 从 1 起。
//
// ⚠️ 未登录返回 -101。作者栏是「我关注的人」，没有登录态就没有这个列表。
func (c *Client) Followings(ctx context.Context, vmid int64, page int) (FollowPage, error) {
	if vmid <= 0 {
		return FollowPage{}, fmt.Errorf("bili: 关注列表要自己的 id（vmid=%d）", vmid)
	}
	if page < 1 {
		page = 1
	}
	params := url.Values{}
	params.Set("vmid", strconv.FormatInt(vmid, 10))
	params.Set("pn", strconv.Itoa(page))
	params.Set("ps", strconv.Itoa(followingsDefaultSize))
	// order_type=attention 是「按最常访问排」（上游 `author-list.tsx:47`）。
	// 不给的话按关注时间排，作者栏一打开是一串很久以前的 UP。
	params.Set("order_type", "attention")

	raw, err := c.Do(ctx, Call{Method: "GET", URL: c.hostAPI + followingsPath, Query: params})
	if err != nil {
		return FollowPage{}, err
	}

	var d struct {
		Total int `json:"total"`
		List  []struct {
			Mid   int64  `json:"mid"`
			Uname string `json:"uname"`
			Face  string `json:"face"`
		} `json:"list"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return FollowPage{}, fmt.Errorf("bili: 关注列表的返回读不懂：%w", err)
	}

	out := FollowPage{Total: d.Total}
	for _, u := range d.List {
		if u.Mid == 0 {
			continue
		}
		out.Authors = append(out.Authors, FollowAuthor{Mid: u.Mid, Name: u.Uname, Face: u.Face})
	}
	// ⚠️ 空页一律收口，和投稿那边同一条理由：B 站在页码超了的时候给的是空
	// list 而不是报错，不收口的话作者栏会永远转圈。
	out.HasMore = len(out.Authors) > 0 && len(out.Authors) < d.Total
	return out, nil
}
