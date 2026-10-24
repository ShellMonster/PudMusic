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
// 丢掉的那一条**带着理由**（`Drops` 里的 `DroppedDynamic`）：「界面上一个格子
// 都没有」这件事要能说清是「他真的没发」还是「我们把他发的那些全丢了」——而
// 「全丢了」还有好几种丢法，界面上**每一类各说一句**（见下面那一节）。
// 上游在 `dynamic-feed` 那一页**没有**按类型过滤（它整页都按视频画），本项目
// 在 UP 空间里过滤了 `DYNAMIC_TYPE_AV`
// （`user-profile/dynamic-list/index.tsx:48`）——那是另一套取舍：那一页要的是
// 「这个人发的视频」，这里要的是「这一条画得出来」。
//
// # 丢掉的理由**分了类**，而且每一条都带着**身份**
//
// 丢掉的理由不止一种（`DropReason`）：「B 站返回了一种我们不认识的形态」
// 和「这一条我们读不懂（B 站改了返回的形状）」是**两回事**——后者是应用
// 自己跟不上 B 站的返回。界面上要按类别说话（票 #344），所以这里分类。
//
// 而每一条丢掉的东西还带一个**身份**（`DroppedDynamic.Key`）：B 站翻页
// **会重叠**，同一条画不出来的动态在两页里各出现一次——上层按身份去重，
// 那个数才是「有几条不同的动态」而不是「出现过几次」。
//
// # 翻页是 offset，不是页码
//
// 动态接口下一页的入口是返回里的 `offset` 字段（一个不透明的串），不是
// `pn`。**把它当成页码数会拿到重复的那一页**：B 站不保证 offset 单调，
// 而我们能做的只有「B 站给什么就原样带回去」——所以它在这里是 `string`。
package bili

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	// dynamicThumbPath 是给一条动态点赞 / 取消点赞。
	//
	// ⚠️ **不是 `/x/v2/dynamic/feed/like/thumb`**（那一个要的是稿件 id，评的是
	// 视频）；动态自己评的是这一条，见 `LikeDynamic` 的注释。
	dynamicThumbPath = "/x/dynamic/feed/dyn/thumb"
)

// 顶层 `type` 里我们画得出的三种（上游 `DynamicType` 枚举的三个值）。
//
// ⚠️ 这是**闸门**，见 parseDynamic 的注释：不在这里的一律丢掉，**哪怕带着正文**。
const (
	dynamicTypeWord = "DYNAMIC_TYPE_WORD" // 纯文字
	dynamicTypeDraw = "DYNAMIC_TYPE_DRAW" // 带图
	dynamicTypeAv   = "DYNAMIC_TYPE_AV"   // 投稿视频
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
//
// ⚠️ **这里没有 Cid**：B 站的动态接口给的是稿件本身，不给分 P id。取流接口
// 不给 cid 会取到**第一集**（多 P 投稿的第三集放出来是第一集，且不报错）——
// 所以 `app.playOnline` 前面有一道 `withCid`：缺 cid 就拿 bvid 去问一次。
// 这里**不要**自己编一个 cid 填上。
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
	// Liked 是「我赞了这一条没有」。
	//
	// ⚠️ **未登录时它恒为 false，而那不是「这条没人赞」**：B 站不给未登录的
	// 登录态，`module_stat.like.status` 直接没有（见 rawDynamic）。所以界面上
	// 画赞不画赞是按这一栏，而**赞的数量**（`LikeCount`）在两种情况下都有值。
	// 把两者混成「有没有赞过」，未登录用户会看到一个永远点不动的已赞样式。
	Liked bool
	// LikeCount 是赞的数量。B 站给的是**数**，不是「1.2万」那种写好的字
	// （播放数给的是字，见 `DynamicArchive.Play`）。
	LikeCount int
}

// DropReason 是「一条动态我们画不出来」的**那一类**理由。
//
// ⚠️ **分类是为了让界面上那句话说得对**（票 #344）：原来只有一个总数，
// 于是「B 站返回了一种我们不认识的形态」和「这一条我们读不懂（B 站改了
// 返回的形状）」在界面上被说成**同一件事**——而后者那句话对用户是错的：
// B 站不是换了一种形态，是**它返回的形状变了**（那是应用自己的问题，
// 不是那个 UP 主发了什么）。
//
// 界面上每一类各说一句（见 `internal/ui` 的 `dropPhrases`）。
type DropReason uint8

const (
	// DropShapeUnknown 是「B 站返回了一种我们不认识的形态」：转发、剧集、
	// 课程、直播、广告、还有未来新增的那些。
	DropShapeUnknown DropReason = iota
	// DropUnreadable 是「这一条我们读不懂」：`encoding/json` 整个失败
	// （B 站改了返回的形状）。
	DropUnreadable
	// DropNoIdentity 是「没有身份」：`id_str` 是空的，画出来也点不开。
	DropNoIdentity
	// DropEmpty 是「这一条什么也没有」：既没有正文也没有图。
	//
	// ⚠️ **它只管「真的什么也没有」那几条**：正文有的那些**不算**在这儿
	// （投稿动态没有稿件号是**另一类**，见 `DropNoArchive`）——说成「什么也没有」
	// 就是**对用户说错话**：那一条明明带着正文。
	//
	// ⚠️ **投稿动态也要看正文**：稿件号和正文**都是空的**时，它算这一档
	// （见 `parseDynamic` 的投稿分支）。
	DropEmpty
	// DropNoArchive 是「投稿动态没有稿件号」：**正文有，稿件号没有**。
	//
	// ⚠️ **它和「什么也没有」是两回事**（票 #344 的评审）：这一条**带着正文**
	// （UP 写的那段话就在 `desc.text` 里），只是 `major.archive.bvid` 是空的
	// ——放不出来。混进「什么也没有」里的话，界面上那句话对用户是错的。
	//
	// ⚠️ **正文也是空的那几条不算这一档**——它们是「什么也没有」
	// （见 `DropEmpty`）：这一类的名字里带着「投稿」，而界面上那句话说的是
	// 「他发了东西，只是放不出来」。
	DropNoArchive
)

// String 是给日志看的名字。界面上显示的话另有各处自己的文案。
func (r DropReason) String() string {
	switch r {
	case DropShapeUnknown:
		return "形态不认识"
	case DropUnreadable:
		return "读不懂"
	case DropNoIdentity:
		return "没有身份"
	case DropEmpty:
		return "什么也没有"
	case DropNoArchive:
		return "投稿没有稿件号"
	}
	return fmt.Sprintf("未知的丢弃理由 %d", uint8(r))
}

// DroppedDynamic 是一条被丢掉的动态。
type DroppedDynamic struct {
	// Reason 是丢掉的那**一类**理由。
	Reason DropReason
	// Key 是这一条的**身份**，用来跨页去重。
	//
	// ⚠️ **它必须是「同一条就是同一个」**：B 站翻页**会重叠**，同一条画不
	// 出来的动态在两页里各出现一次——按出现次数记的话，界面上那个数会
	// **偏大**（票 #344）。
	Key string
	// Why 是这一条的理由（人话）。**只进日志**，不进界面。
	Why string
}

// DropCounts 是丢掉的理由按类别分的计数。
//
// ⚠️ **分类计数才是能说清原因的那个数**：总数只能说「丢了 N 条」，说不出
// 「丢的是哪一种」（票 #344）。
type DropCounts struct {
	// ShapeUnknown 是「B 站返回了一种我们不认识的形态」的那些。
	ShapeUnknown int
	// Unreadable 是「这一条我们读不懂」的那些。
	Unreadable int
	// NoIdentity 是「没有身份」的那些。
	NoIdentity int
	// Empty 是「这一条什么也没有」的那些。
	Empty int
	// NoArchive 是「投稿动态没有稿件号」的那些（正文有，稿件号没有）。
	NoArchive int
	// Other 是**还不认识的类别**的那些。
	//
	// ⚠️ **它是为了让「加了一类而忘了加一档」不变成静默的**：将来加了第五类
	// 丢弃理由而 `Add` 忘了跟上，那一条**不许凭空消失**——它落进这里，
	// 界面上和日志里都看得见（而 `Add` 原来那个没有 `default` 的 switch
	// 会把它**静默少一档**：总数少了、界面上那句话少了，没有任何报错）。
	Other int
}

// Add 记一条丢掉的东西。
//
// ⚠️ **default 那一档不是「忽略」**：它把还不认识的那一类记进 `Other`——
// 宁可界面上多一句「还不认识的类别」，也不要**静默少一档**（见 `Other`）。
func (c *DropCounts) Add(r DropReason) {
	switch r {
	case DropShapeUnknown:
		c.ShapeUnknown++
	case DropUnreadable:
		c.Unreadable++
	case DropNoIdentity:
		c.NoIdentity++
	case DropEmpty:
		c.Empty++
	case DropNoArchive:
		c.NoArchive++
	default:
		c.Other++
	}
}

// Total 是丢掉的总条数。
func (c DropCounts) Total() int {
	return c.ShapeUnknown + c.Unreadable + c.NoIdentity + c.Empty + c.NoArchive + c.Other
}

// DynamicPage 是一页动态。
type DynamicPage struct {
	Items []Dynamic
	// Offset 是下一页的入口。**原样带回接口**，不要解析它。
	Offset string
	// HasMore 后面还有没有。
	HasMore bool
	// Raw 是 B 站这一页**发了**几条（过滤之前的条数）。
	//
	// ⚠️ **必须留着它**：上层要靠它分「这一页真的没有」和「这一页有，但我们
	// 全画不出来」。只看 `len(Items)` 的话，一页全是转发的动态会被当成
	// 「到底了」，于是后面的有效动态**永远不再加载**（实测：第一页一条转发
	// + has_more=true，第二页有一条正常的，卡在第一页上不再翻）。
	Raw int
	// Drops 是这一页丢掉的那几条（带**类别**和**身份**）。
	//
	// ⚠️ **类别是给界面用的**（票 #344）：只有一个总数的话，「B 站返回了
	// 一种我们不认识的形态」和「这一条我们读不懂」在界面上被说成同一件事，
	// 而后者那句话对用户是错的。
	//
	// ⚠️ **`Key` 是给上层去重用的**：B 站翻页会重叠，同一条画不出来的动态
	// 在两页里各出现一次——按「出现次数」记，那个数会偏大。
	Drops []DroppedDynamic
	// DropWhy 是丢掉的理由（人话），**一种一句**。
	//
	// ⚠️ **它不进界面，但进日志**：字段类型一变就是「整页一条都不剩」，
	// 而那种失败在界面上**完全看不出来**（只有一个空列表，一个转着的圈）。
	// 不留下这句话，下一个人只能从头再猜一遍（票 #300 的实测：`pub_ts`
	// 变成了字符串，于是每一页 12 条全被丢掉）。
	DropWhy []string
}

// noteDrop 记一条丢掉的理由（同一种理由只记一次）。
func (p *DynamicPage) noteDrop(why string) {
	if why == "" {
		return
	}
	for _, w := range p.DropWhy {
		if w == why {
			return
		}
	}
	// 理由最多记三条：再多就是「B 站的返回整个变了」，那句话在日志里反而
	// 淹掉了真正的原因。
	if len(p.DropWhy) >= 3 {
		return
	}
	p.DropWhy = append(p.DropWhy, why)
}

// MoreAfter 报「按我们刚发出去的那个游标问，还剩下一页吗」。
//
// ⚠️ **判据是「入口有没有前进」，不是 `has_more` 那一格**：B 站有时候会回
// 一个和上一次**一模一样**的 offset（或者干脆空串）而 `has_more` 仍然是
// true。拿着那个 offset 再问一次，问回来的是**同一页**——于是「再问一页」
// 永远问不完（实测：一页全是转发动态时 60 帧发了 40 次请求，界面上一直转圈，
// 一条内容都出不来）。
//
// 所以「有没有下一页」这件事要问**入口**：入口没动就是没有下一页。
func (p DynamicPage) MoreAfter(used string) bool {
	if !p.HasMore {
		return false
	}
	return p.Offset != "" && p.Offset != used
}

// dynamicEnvelope 是动态接口的外壳。
//
// ⚠️ **外壳那几格也是容错的**：外壳解不开 = **整页都没了**（而不是少一行），
// 那是这一层最贵的一种失败——`offset` 改成数字、`has_more` 改成 0/1 都
// 能把它弄成那样（理由同 flexjson.go 的文件头）。
type dynamicEnvelope struct {
	Items   []json.RawMessage `json:"items"`
	Offset  flexString        `json:"offset"`
	HasMore flexBool          `json:"has_more"`
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
	pg, err := dynamicPageFrom(raw)
	if err != nil {
		return DynamicPage{}, err
	}
	// ⚠️ **`has_more` 不可信**：入口没前进就是没有下一页（见 MoreAfter）。
	pg.HasMore = pg.MoreAfter(offset)
	return pg, nil
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
	pg, err := dynamicPageFrom(raw)
	if err != nil {
		return DynamicPage{}, err
	}
	// ⚠️ **`has_more` 不可信**：入口没前进就是没有下一页（见 MoreAfter）。
	pg.HasMore = pg.MoreAfter(offset)
	return pg, nil
}

// LikeDynamic 给一条动态点赞（like=true）或取消点赞。**写操作**。
//
// # 评的是「这一条动态」，不是它里面那个视频
//
// B 站有**两个**点赞接口，长得像、评的不是一回事：
//
//	POST /x/v2/dynamic/feed/like/thumb     给**稿件**点赞（参数 bvid / up）
//	POST /x/dynamic/feed/dyn/thumb          给**这一条动态**点赞（dyn_id_str / up）
//
// 这里用第二个。理由：动态卡片上那一下，评的是这条动态——用户在评论区看到的
// 那个赞；而 `major.archive` 那一份是这个动态**转发**的投稿，在动态流里赞它，
// 赞数不会记在这条动态上。反过来用第一个，动态流里每张投稿卡片赞的都是同一
// 个视频，多个 UP 转发同一支片子时那个数字还会自己往上加。
//
// up 的取值照抄接口清单（`docs/dynamic/action.md`）：1 点赞、2 取消点赞。
// **0 是切换，我们不用它**——切换在界面重画之后会点成反的，用户连点两下等于
// 没点。
//
// ⚠️ **请求体是 JSON，不是表单**：这一条收的是
// `{"dyn_id_str":"…","up":1}`。写成表单的话 B 站回的是「参数错误」
// （4100001）而不是 csrf 校验失败，排障方向从第一步就错了。
//
// csrf 走查询串和 `bili-jct` 头（`Call.CSRF` 已经放了两处），**体里没有**。
func (c *Client) LikeDynamic(ctx context.Context, dynID string, like bool) error {
	dynID = strings.TrimSpace(dynID)
	if dynID == "" {
		return fmt.Errorf("bili: 动态 id 是空的")
	}
	up := 1
	if !like {
		up = 2
	}
	if _, err := c.Do(ctx, Call{
		Method: "POST", URL: c.hostAPI + dynamicThumbPath, CSRF: true,
		JSON: map[string]any{"dyn_id_str": dynID, "up": up},
	}); err != nil {
		return fmt.Errorf("bili: 给动态 %s %s: %w", dynID, likeOrUnlike(like), err)
	}
	return nil
}

// likeOrUnlike 是日志里那句话的动词。
func likeOrUnlike(like bool) string {
	if like {
		return "点赞"
	}
	return "取消点赞"
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

	pg := DynamicPage{Offset: env.Offset.String(), HasMore: env.HasMore.Bool(), Raw: len(env.Items)}
	for _, item := range env.Items {
		d, drop, ok := parseDynamic(item)
		if !ok {
			// ⚠️ **这里不再记「这一页丢了几个」**：那个数是**死的**——没有人读它
			// （上层按 `Drops` 里的 `Key` 跨页去重、按 `DropCounts` 分类说话）。
			// 留着它就是「一个注释说它有用、其实没人读」的字段（票 #344 的评审）。
			pg.Drops = append(pg.Drops, drop)
			pg.noteDrop(drop.Why)
			continue
		}
		pg.Items = append(pg.Items, d)
	}
	return pg, nil
}

// rawOpus 是**新格式**的图文（`MAJOR_TYPE_OPUS`）。
type rawOpus struct {
	Summary struct {
		Text flexString `json:"text"`
	} `json:"summary"`
	Pics []rawPic `json:"pics"`
}

// rawDraw 是**旧格式**的图文（`MAJOR_TYPE_DRAW` 时代）：图在 `items` 里，
// 正文**没有单独一个字段**（就在图里，或者干脆只有图）。
//
// ⚠️ **两种都要认**：B 站把这个类型从 `draw` 改名成了 `opus`，而**老动态的
// 返回里仍然是 `draw`**。只认 opus 的症状不是「少一张图」而是**整条消失**：
// 旧格式的图文常常没有 `desc.text`（正文在图里），认不出来就成了 `dropped`
// ——明明有九张图，界面上一个格子都没有。
type rawDraw struct {
	Items []rawPic `json:"items"`
}

// rawPic 是图文里的一张图。
//
// ⚠️ **B 站对同一批图给过两个字段名**：老的那套（`MAJOR_TYPE_DRAW` 时代）
// 叫 `src`，新的（`MAJOR_TYPE_OPUS`）叫 `url_src`。两个都认，取到哪个算
// 哪个——只认一个的话，另一批图文在界面上是「一张图都没有」的空卡片。
type rawPic struct {
	Src    flexString `json:"src"`
	URLSrc flexString `json:"url_src"`
}

// URL 是这张图的地址，两个字段名里取有值的那个。
func (p rawPic) URL() string {
	if p.URLSrc != "" {
		return p.URLSrc.String()
	}
	return p.Src.String()
}

// rawDynamic 是解析一条动态时真正要读的那些字段。
//
// ⚠️ **只声明用得着的**：B 站这一条动态返回几百个字段（module_more、
// module_interaction、module_fold、module_dispute…），全声明进来等于把
// 「B 站加了一个字段」变成一次编译失败。这里少一个字段的代价是界面上少
// 一样东西，那是可以后补的。
//
// ⚠️ **每一个叶子字段都是容错类型**（`flexString` / `flexInt` / `flexBool`，
// 见 flexjson.go）：**类型对不上会让 `encoding/json` 整个失败**，而失败的
// 那一条会被当成「读不懂」丢掉——一个字段改了类型就是**整页一条都不剩**，
// 而界面上只是一个空列表（实测：`module_author.pub_ts` 变成了字符串，
// 于是每一页 12 条全丢，票 #300）。
type rawDynamic struct {
	IDStr flexString `json:"id_str"`
	// Type 是动态的大类（`DYNAMIC_TYPE_WORD` 那些）。**这里不用它**：
	// 判类型看的是 major 装了什么，见 parseDynamic 的注释。
	Type    flexString `json:"type"`
	Modules struct {
		Author struct {
			Mid     flexInt    `json:"mid"`
			Name    flexString `json:"name"`
			Face    flexString `json:"face"`
			PubTime flexString `json:"pub_time"`
			// ⚠️ **B 站把这个字段从数字改成了字符串**（实测 2026-10-24：
			// `"pub_ts": "1792720812"`）。它读不出来时 `pub_time` 还在，
			// 而它读不出来却让整条动态被带走，是这一页一条都不剩的原因。
			PubTS flexInt `json:"pub_ts"`
		} `json:"module_author"`
		// Stat 是这一条被互动了多少（`modules.module_stat`）。
		//
		// ⚠️ **未登录时整个 `module_stat` 都没有**：那时候三个字段全是零值，
		// 分不出「没人赞」和「没登录看不到」，所以 `Liked` 只在登录了的时候
		// 才有意义（见 Dynamic.Liked 的注释）。
		Stat struct {
			Like struct {
				Count  flexInt  `json:"count"`
				Status flexBool `json:"status"`
			} `json:"like"`
		} `json:"module_stat"`
		Dynamic struct {
			Desc struct {
				Text flexString `json:"text"`
			} `json:"desc"`
			Major struct {
				Type    flexString `json:"type"`
				Archive *struct {
					Bvid        flexString `json:"bvid"`
					Title       flexString `json:"title"`
					Cover       flexString `json:"cover"`
					Desc        flexString `json:"desc"`
					DurationTxt flexString `json:"duration_text"`
					Stat        struct {
						Play flexString `json:"play"`
					} `json:"stat"`
				} `json:"archive"`
				Opus *rawOpus `json:"opus"`
				// Draw 是**旧格式**的图文（`MAJOR_TYPE_DRAW` 时代），见 rawDraw。
				Draw *rawDraw `json:"draw"`
			} `json:"major"`
		} `json:"module_dynamic"`
	} `json:"modules"`
}

// parseDynamic 把一条原始动态变成 Dynamic。第二个返回值是**丢掉的那一条**
// 的信息（画得出来时是零值），第三个是「画得出来吗」。
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
// 🔴 **先按 `type` 筛，再按 major 认细节。**
//
// 为什么不能「认不出 major 就当纯文字」：实测踩过——**带正文的转发**和
// **未知类型**（B 站新加的形态）都带 `desc.text`，那一支会把它们画成一张
// 纯文字卡片，而要求是「其余类型**静默丢弃**」。一张转发被当成他自己发的
// 一句话，比不画更糟（用户会以为是原创）。
//
// 所以 `type` 是**闸门**：只有上游 `DynamicType` 枚举里那三种能过
// （`DYNAMIC_TYPE_AV` / `DYNAMIC_TYPE_WORD` / `DYNAMIC_TYPE_DRAW`），
// 其余（FORWARD / PGC / COURSES / LIVE / AD / 未来新增的…）一律丢掉。
// 过了闸门再用 `major` 认细节——**顶层 `type` 稳，`major.type` 换过名**
// （同一个图文类型叫过 `MAJOR_TYPE_DRAW` 和 `MAJOR_TYPE_OPUS`），
// 认错了会分叉成两种卡片。
//
// # 「丢掉」也要说得出理由，而且**说得准**
//
// 丢掉的**理由**进**日志**：「整页一条都不剩」这种失败在界面上**完全
// 看不出来**（只有一个空列表），不留下这句话，下一个人只能从头再猜一遍
// （票 #300）。
//
// 而界面上要的是**类别**（票 #344）：「B 站返回了一种我们不认识的形态」和
// 「这一条我们读不懂（B 站改了返回的形状）」是**两回事**，说成同一句就是
// 对用户说错话——后者是应用自己跟不上 B 站的返回，不是那个 UP 主发了什么。
func parseDynamic(raw json.RawMessage) (Dynamic, DroppedDynamic, bool) {
	var r rawDynamic
	if err := json.Unmarshal(raw, &r); err != nil {
		// 这一条的 JSON 读不懂。不算错——B 站的返回是整页一个数组，
		// 一条坏掉不该让整页都没了。
		//
		// ⚠️ **这一类是「读不懂」，不是「形态不认识」**：B 站给的是它**一直
		// 在给**的那种形态，是**我们读不出来**——界面上的话必须分开说
		// （票 #344）。
		return Dynamic{}, DroppedDynamic{
			Reason: DropUnreadable,
			Key:    dropKeyOf(&r, raw),
			Why:    "这一条读不懂（B 站改了返回的形状）：" + err.Error(),
		}, false
	}
	if r.IDStr == "" {
		// 没有 id 的行没有身份（铁律四），画出来也点不开。
		return Dynamic{}, DroppedDynamic{
			Reason: DropNoIdentity,
			Key:    dropKeyOf(&r, raw),
			Why:    "没有身份（id_str 是空的）",
		}, false
	}

	// 🔴 闸门。**先判 type，再碰别的**。
	switch string(r.Type) {
	case dynamicTypeWord, dynamicTypeDraw, dynamicTypeAv:
		// 三种画得出的，继续往下认细节。
	default:
		// 转发 / 剧集 / 课程 / 直播 / 广告 / 未来新增的形态……
		// 一律丢掉，**哪怕它带着正文**（转发件就有 desc.text）。
		return Dynamic{}, DroppedDynamic{
			Reason: DropShapeUnknown,
			Key:    dropKeyOf(&r, raw),
			Why:    "不是这一版画得出的形态（" + r.Type.String() + "）",
		}, false
	}

	d := Dynamic{
		ID: r.IDStr.String(),
		Author: DynamicAuthor{
			Mid:  r.Modules.Author.Mid.Int64(),
			Name: r.Modules.Author.Name.String(),
			Face: r.Modules.Author.Face.String(),
		},
		Time:      dynamicTime(r.Modules.Author.PubTime.String(), r.Modules.Author.PubTS.Int64()),
		Text:      strings.TrimSpace(r.Modules.Dynamic.Desc.Text.String()),
		Liked:     r.Modules.Stat.Like.Status.Bool(),
		LikeCount: max(int(r.Modules.Stat.Like.Count.Int64()), 0),
	}

	major := r.Modules.Dynamic.Major

	// ① 投稿视频（`DYNAMIC_TYPE_AV`）。少了 bvid 就放不出来，丢掉。
	if string(r.Type) == dynamicTypeAv {
		a := major.Archive
		if a == nil || a.Bvid == "" {
			// 🔴 **有没有正文，决定它是哪一类**（票 #344 的评审）：
			//
			//	正文**也是空的**	→ 它**什么也没有**（`DropEmpty`）：那一档说的是
			//			「他什么都没发」；
			//	正文**有**	→ 它是「投稿没有稿件号」（`DropNoArchive`）：他发了
			//			东西，只是我们放不出来。
			//
			// ⚠️ **正文的口径和画得出来的那一支是同一个**：`desc.text`，空了
			// 再退到视频的简介（`archive.desc`）——见下面 `d.Text` 那两句。
			// 不看正文就说「带着正文」的话，一条**什么也没有**的投稿动态也会被
			// 说成「是投稿却放不出来」（票 #344 的评审查出来的）。
			text := d.Text
			if text == "" && a != nil {
				text = strings.TrimSpace(a.Desc.String())
			}
			if text == "" {
				return Dynamic{}, DroppedDynamic{
					Reason: DropEmpty,
					Key:    dropKeyOf(&r, raw),
					Why:    "投稿动态既没有稿件号也没有正文",
				}, false
			}
			return Dynamic{}, DroppedDynamic{
				Reason: DropNoArchive,
				Key:    dropKeyOf(&r, raw),
				Why:    "投稿动态没有稿件号（bvid 是空的）",
			}, false
		}
		d.Kind = DynamicVideo
		d.Video = &DynamicArchive{
			Bvid: a.Bvid.String(), Title: a.Title.String(), Cover: a.Cover.String(),
			Duration: a.DurationTxt.String(), Play: a.Stat.Play.String(), Desc: a.Desc.String(),
		}
		if d.Text == "" {
			d.Text = strings.TrimSpace(a.Desc.String())
		}
		return d, DroppedDynamic{}, true
	}

	// ② 图文（`DYNAMIC_TYPE_DRAW`）。正文**二选一**，不是两段拼起来。
	//
	// ⚠️ 这是实测过的：上游两处都是 `desc?.text || opus?.summary?.text`
	// （`dynamic-feed/item.tsx:33`、`user-profile/dynamic-list/item.tsx:46`），
	// 而 B 站的正常返回里 `desc.text` 和 `opus.summary.text` 是**同一段话**。
	// 拼起来（`desc + "\n" + summary`）的结果是界面上同一句话出现两遍。
	{
		pics, summary := picsOf(major.Opus, major.Draw)
		if len(pics) > 0 || summary != "" {
			if d.Text == "" {
				d.Text = summary
			}
			if len(pics) == 0 && d.Text == "" {
				// 声明是图文但既没图也没字：画出来是一个空卡片，比不画更糟。
				return Dynamic{}, DroppedDynamic{
					Reason: DropEmpty,
					Key:    dropKeyOf(&r, raw),
					Why:    "图文动态既没有图也没有正文",
				}, false
			}
			d.Kind = DynamicOpus
			d.Pics = pics
			return d, DroppedDynamic{}, true
		}
	}

	// ③ 纯文字（`DYNAMIC_TYPE_WORD`）。
	if d.Text == "" {
		// 声明是文字但正文是空的：画出来是一个空卡片，比不画更糟。
		return Dynamic{}, DroppedDynamic{
			Reason: DropEmpty,
			Key:    dropKeyOf(&r, raw),
			Why:    "文字动态没有正文",
		}, false
	}
	d.Kind = DynamicText
	return d, DroppedDynamic{}, true
}

// dropKeyOf 是一条丢掉的东西的**身份**（跨页去重用的）。
//
// ⚠️ **它必须是「同一条就是同一个」**：B 站翻页**会重叠**，同一条画不出来
// 的动态在两页里各出现一次——按「出现次数」记的话，界面上那个数会**偏大**
// （票 #344）。
//
// 取的顺序是「`id_str` → 稿件号 → 原文的指纹」：
//   - **`id_str` 是这一条动态自己的身份**（画得出来的那几条也是用它当身份
//     的，见 `Dynamic.ID`），翻页重叠时它一样；
//   - 没有 `id_str` 的那一条（本来就要因为「没有身份」被丢掉）退到稿件号；
//   - 再没有（连 JSON 都读不开）就用**原文的指纹**——重叠时那一条原文也是
//     一样的，指纹也就一样。
//
// ⚠️ **不要拿 bvid 当身份**：一条转发和它转的那条投稿**是两条动态**，而
// 它们的稿件号是同一个——按 bvid 记会把两条数成一条。
func dropKeyOf(r *rawDynamic, raw json.RawMessage) string {
	if id := r.IDStr.String(); id != "" {
		return "id:" + id
	}
	if a := r.Modules.Dynamic.Major.Archive; a != nil {
		if bvid := a.Bvid.String(); bvid != "" {
			return "bvid:" + bvid
		}
	}
	return "raw:" + dropFingerprint(raw)
}

// dropFingerprint 是「连身份都读不出来」时用的指纹。
//
// 用**原文**（去掉空白后）的哈希：翻页重叠时同一条动态的原文是一样的，
// 指纹也就一样——于是它被数一次，而不是两次。
func dropFingerprint(raw json.RawMessage) string {
	b := bytes.TrimSpace(raw)
	var buf bytes.Buffer
	if err := json.Compact(&buf, b); err != nil {
		// 原文本身不是合法 JSON（那正是「读不懂」的那一种）：就用它的字节。
		buf.Reset()
		buf.Write(b)
	}
	sum := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(sum[:8])
}

// picsOf 取图文的图和摘要，**新旧两种格式都认**。
//
//	新（`MAJOR_TYPE_OPUS`）：`major.opus.pics[]`，图地址是 `url_src` 或 `src`
//	旧（`MAJOR_TYPE_DRAW`）：`major.draw.items[]`，图地址是 `src`
//
// 两者取到都算数：新格式优先（它同时带 summary），旧格式兜底。
// ⚠️ 少认一种的症状不是「少一张图」，而是**整条消失**：旧格式的图文常常
// 没有 `desc.text`（正文在图里），认不出来就变成 `dropped`。
func picsOf(opus *rawOpus, draw *rawDraw) ([]string, string) {
	var (
		items []rawPic
		text  string
	)
	if opus != nil {
		items, text = opus.Pics, opus.Summary.Text.String()
	}
	if len(items) == 0 && draw != nil {
		items = draw.Items
	}
	pics := make([]string, 0, len(items))
	for _, p := range items {
		if u := p.URL(); u != "" {
			pics = append(pics, u)
		}
	}
	return pics, strings.TrimSpace(text)
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
