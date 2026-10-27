// B 站的评论：主列表（`/x/v2/reply/main`）与楼中楼（`/x/v2/reply/reply`）。
//
// # 这一层只管「拿到」
//
// 它不决定界面长什么样、不管缓存、不管失败之后要不要退避——那些是
// `internal/app` 的事（理由同 `collection.go` 的文件头）。所以这里的方法全是
// 「发一次请求就返回」的同步函数：翻页、刷新、取消都在外面那一层做。
//
// # 🔴 三个会让实现静默出错的坑（研究票 #537 的实测结论）
//
// ## 坑一：`Call.Buvid` 绝对不能开，而且要显式写 `Call.NoBuvid`
//
// 实测（同一客户端、同一 URL，只改 Cookie 头；**匿名**客户端）：
//
//	不带 / 只带 foo=bar / 只带 SESSDATA   → 20 条，cursor.is_end = false
//	带 buvid3=<真值>（带不带 buvid4 都一样） →  3 条，cursor.is_end = true
//
// **匿名客户端只要请求头里出现 `buvid3`，B 站就把评论列表截成 3 条并宣称
// 「到底了」。**
// 而本仓库的 `Call.Buvid` 干的正是这件事（`client.go` 里那个字段的注释在说
// 「取流接口没有它时 B 站常把请求当成可疑流量」——**取流要带，评论不能带**）。
//
// 照抄「取流要带 buvid」这条既有习惯就会踩：症状是「评论怎么只有 3 条」，而
// B 站那句「到底了」说得天衣无缝，界面上看不出任何异常。
//
// ### 🔴 「不设 `Call.Buvid`」是**不够**的
//
// jar 里的 cookie 是在 `http.Client.send` 里加的，而那**在运输层之前**
// （`net/http/client.go`：`c.Jar.Cookies(url)` → `req.AddCookie` → `RoundTrip`）。
// 所以「代码里没写 `Buvid: true`」挡不住 jar 里**已经有一枚** buvid3。
//
// jar 里确实会有：登录成功时 B 站下发的**全部** cookie 都被存下来
// （`app.Account.finish` 收下 `IssuedCookies()` 的每一条），而 B 站的 cookie
// 域是 `.bilibili.com`，`api.bilibili.com` 收得到。
//
// ⇒ 评论请求必须显式写 **`NoBuvid: true`**：它在**运输层**把 buvid 那一族
// 从 Cookie 头里摘掉（那里 jar 已经加好了），而**登录态照带**
// （SESSDATA / bili_jct 一个都不动）。见 `client.go` 的 `noBuvidTransport`。
//
// ⚠️ **这条的边界**（2026-10-27 真机回归复验过）：那 3 条是**匿名**客户端
// 的数。**登录态下带 buvid3 不受影响**——同一个 URL、同一枚真 buvid3：
//
//	匿名 + buvid3          →  3 条，is_end = true
//	假 SESSDATA + buvid3   →  3 条，is_end = true
//	**真登录态** + buvid3  → **20 条**，is_end = false
//
// 判据是**登录态成不成立**，不是「Cookie 头里有没有 SESSDATA 这个名字」。
//
// ⇒ **`NoBuvid: true` 仍然是对的**（它往严的那边退），但**别改成「要带」**：
// 多带 buvid3 的坏处是**静默**少拿数据（只剩 3 条），少带的坏处顶多是
// 「B 站觉得这个客户端像机器人」——两边的错法不对称。
//
// ## 坑二：用不带 WBI 的那个接口
//
// `/x/v2/reply/wbi/main` 少了 WBI 签名就 -403。而本仓库的 `signWBI`
// **拿不到 key 时会「原样放行」**（见 `wbi.go`）——那等于发一个**必然被拒**
// 的请求。所以这里选 `/x/v2/reply/main`（不带 WBI），把这一整类问题绕开。
//
// ## 坑三：-403 在评论这一处的意思是「再问一次」
//
// `friendly.go` 里 -403 的话是「没有权限看这一项」，`retry.go` 里它被判成
// **不可重试**。那两句在别的接口上是对的，在评论这一处**是错的**：评论接口的
// -403 多半是「签名没签上 / 被挡了一下」，**是可以重试的**。
//
// ⇒ **本文件自己判 -403**（见 `CommentRecoverable` 与 `CommentFriendly`），
// **不许顺手改全局那张表**——那会波及别的接口（票 #571 的纪律）。
//
// # 坑四：翻页走 `pagination_reply.next_offset`，**不是 `cursor.next`**
//
// 实测（2026-10-27 真机回归，票 #591，只发 GET）：
//
//	最热档（mode=3）  next 是**页码**（1 → 2 → 3 …）
//	最新档（mode=2）  next 是**楼层号**（160595 → 160592 → …，递减）
//	评论档（mode=1）  next 带上去**必定**回
//	                `-400 min_score(1.000000) must be <= max_score(0.000000)`
//
// 根因是 `cursor.support_mode` **恒为 `[2, 3]`，不含 1**——B 站已经不支持
// mode=1 这一档的游标了（它认得 `mode=1`，但给回来的 `next` 是另一套，
// 拿它去翻就撞上了 B 站内部的分数区间）。
//
// 而 `cursor.pagination_reply.next_offset` +
// `pagination_str={"offset":"<next_offset>"}` **三档都能翻**（实测 mode=3 /
// 1 / 2 都翻到了第二页，而且每页内容互不相同）。
//
// ⚠️ **那个偏移量一个字都不能改**：改一个字符 B 站**不报错**——它回的是
// **第一页**（实测：拿一个乱写的 offset 去问，它回 code 0 + 第一页）。于是
// 界面上「加载更多」按下去**什么都不变**。
//
// # 「评论」那一档（`mode=1`）给的就是「最新评论」的同一份
//
// 实测（2026-10-27 真机回归，票 #591）：mode=1 与 mode=2 的内容**逐条相同**
// ——第 1 页 19 条、第 2 页 18 条，rpid 都对得上（oid=170001 与
// oid=114256632618805 两个对象都比过）。
//
// 而 B 站给这两档的 `cursor.name` 是**不同的**（「评论」/「最新评论」）——
// **只看名字根本分不出来**。
//
// ⇒ 界面上**不能**把它们摆成两个不同的档（用户会以为「切了档但没生效」）。
// 判据见 `CommentSort.SameContentAs` 与 `CommentSorts`。
//
// # 老接口 `/x/v2/reply` 不用
//
// 实测它只给 3 条，`pn=2` 起返回 0 条。它是 `pn`/`ps`/`sort` 那一套，
// 而我们要的是游标分页。
//
// # 匿名可用
//
// 实测全程匿名（没有 SESSDATA）三个接口都正常返回。所以评论这一页**不要求
// 登录**。
package bili

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// 评论那几个接口的路径。
const (
	// pathReplyMain 是主列表。**不带 WBI**（见文件头的坑二）。
	pathReplyMain = "/x/v2/reply/main"
	// pathReplyReply 是楼中楼（二级回复）。
	pathReplyReply = "/x/v2/reply/reply"
)

// 评论对象的类型（`type` 参数）。值抄 B 站，别自己另编一套。
const (
	// CommentTypeVideo 是视频稿件。**oid 是稿件的 avid**（不是 bvid）。
	CommentTypeVideo = 1
	// CommentTypeAudio 是音乐库里的音频。**oid 是音频的 auid**。
	//
	// ⚠️ 音乐库条目（`Item.Type == "audio"`）**有自己的评论区**，而那里的
	// 评论**真的是在聊这首歌**——所以「视频评论当歌曲评论」的语义错位只发生
	// 在 `mv` 条目上（研究票 #537 报告 §4）。
	CommentTypeAudio = 14
)

// CommentSort 是评论列表的排序方式（`mode` 参数）。
//
// 这三档对应 B 站评论区的「热门评论 / 评论 / 最新评论」，不用另发明。
//
// ⚠️ **「评论」那一档（`mode=1`）不是第三份内容**——它给的就是「最新评论」
// 的同一份（实测，理由见 `CommentSortDefault` 的注释与 `SameContentAs`）。
type CommentSort int

const (
	// CommentSortHot 是「热门评论」（`mode=3`）：只按热度。
	CommentSortHot CommentSort = 3
	// CommentSortDefault 是「评论」（`mode=1`）。
	//
	// 🔴 **它给的就是「最新评论」的同一份内容**（实测 2026-10-27，票 #591）：
	//
	//	第 1 页  mode=1 与 mode=2 的 rpid **逐条相同**（oid=170001 与
	//	        oid=114256632618805 两个对象都比过）
	//	第 2 页  换成 `pagination_str` 翻之后，两档的 rpid 仍**逐条相同**
	//
	// 试过的别的参数也分不开：`sort=0/1/2/3`、`plat=1`、`web_location`——
	// **全都回同一份内容**（`sort` 对这一档完全无效）。
	//
	// ⇒ 这一档**没有「推荐」那种独立内容**。界面上不能把它和「最新评论」
	// 摆成两个不同的东西（摆成两个一模一样的档，用户会以为「切了档但没生效」）
	// ——判据是 `SameContentAs`。
	//
	// ⚠️ **旧注释写的是「热度 + 时间」，那是错的**（AGENTS.md §7 注释不许
	// 说谎）：B 站这一档给的是纯时间序，与「最新评论」一模一样。
	//
	// ⚠️ **别拿 0 当默认值发出去**：实测 `mode=0` 会被 B 站当成 `mode=3`
	// 处理（回的是「热门评论」），而 `mode=99` 直接回 `-400 invalid mode`。
	// 所以「没指定」要落到一个**认得出来的**档上，而不是把 0 原样发出去。
	//
	// ⚠️ **`cursor.support_mode` 恒为 `[2, 3]`，不含 1**（实测）⇒ B 站已经
	// 不给这一档发能用的游标了。它的游标要改走 `pagination_reply.next_offset`
	// （见 `CommentPage.NextOffset` 与 `CommentList`）。
	CommentSortDefault CommentSort = 1
	// CommentSortLatest 是「最新评论」（`mode=2`）：只按时间。
	CommentSortLatest CommentSort = 2
)

// SameContentAs 报「这两档在 B 站那边给的是不是同一份内容」。
//
// 🔴 **「评论」（`mode=1`）和「最新评论」（`mode=2`）是同一份**（实测，理由见
// `CommentSortDefault` 的注释）。⇒ 界面上**不能**把它们摆成两个不同的档：
// 用户会以为「切了档但没生效」。
//
// ⚠️ **这个判据不能省**：B 站那边两档的 `cursor.name` 是不同的（「评论」/
// 「最新评论」），而内容一模一样——只看名字的话根本分不出来。
func (s CommentSort) SameContentAs(other CommentSort) bool {
	return commentSortContentGroup(s) == commentSortContentGroup(other)
}

// CommentSorts 是「界面上该摆哪几档排序」。
//
// 🔴 **它只有两档**——「评论」（`mode=1`）那一档**不在里面**（理由见
// `CommentSortDefault` 的注释）：
//
//	「评论」和「最新评论」在 B 站那边给的是**同一份内容**（实测，逐条比过
//	rpid），而 `cursor.support_mode` 恒为 `[2, 3]`，**不含 1**——B 站已经
//	不支持那一档的独立游标了。
//
// ⇒ 把它们摆成两个不同的档，用户切过去会看到**一模一样的内容**，于是以为
// 「切了档但没生效」。
//
// ⚠️ **留的是「最新评论」那一档**（不是「评论」）：`mode=1` 不在 B 站的
// `support_mode` 里，而 `mode=2` 在——**它才是 B 站现在支持的那一档**。
func CommentSorts() []CommentSort {
	return []CommentSort{CommentSortHot, CommentSortLatest}
}

// commentSortContentGroup 是「B 站那边同一份内容」的分组号。
//
// 「评论」和「最新评论」归同一组（实测它们给的是同一份），「热门评论」自己
// 一组。
func commentSortContentGroup(s CommentSort) int {
	switch s {
	case CommentSortDefault, CommentSortLatest:
		return 1
	default:
		return 2
	}
}

// commentPageSizeMax 是主列表一页最多给几条。
//
// ⚠️ **上限是 30**（实测 `ps=5` → 5 条、`ps=20` → 20 条、`ps=30` → 30 条、
// `ps=49` / `ps=100` → **只给 30 条**）。
//
// B 站**不报错**，只是默默按 30 截断——于是翻页从第二页开始会漏掉一批，
// 而界面上看不出任何异样：总数在那儿，列表在增长，唯独中间少了一段。
// 所以这里**按 30 封顶**，不指望 B 站替我们截。
const commentPageSizeMax = 30

// commentReplyPageSizeMax 是楼中楼一页最多给几条。
//
// ⚠️ **上限是 20**：实测 `ps=20` → 20 条，而 `ps=30` / `ps=49` → **也只给
// 20 条**（`page.size` 回的也是 20）。
const commentReplyPageSizeMax = 20

// 评论这一摊自己的哨兵错误。都拿 errors.Is 判。
var (
	// ErrCommentTargetMissing 是「B 站说没有这个对象」（-404）。
	//
	// 它和「0 条评论」**是两回事**，各有一句人话（见 `CommentFriendly`）：
	//
	//	-404      这个稿件/这首歌不在了 → 再翻页也没用
	//	0 条评论   它在，只是没人评论     → 页面该摆「还没有人评论」
	//
	// 混成一句话的话，用户看到「这个对象不在了」会以为歌没了，而歌好好地
	// 摆在那儿——**他会去重新下载一遍**。
	ErrCommentTargetMissing = errors.New("bili: 没有这个评论对象")

	// ErrCommentSectionClosed 是「UP 主把评论区关了」（12061）。
	//
	// 单独一个哨兵是因为它和「0 条评论」也要分开说：这一条是**有人**关掉了它，
	// 而 0 条是**本来就没有**。用户看到「评论区关了」知道白按了；看到
	// 「还没有人评论」会以为是自己来得太早。
	ErrCommentSectionClosed = errors.New("bili: 评论区被关掉了")

	// ErrCommentNotFound 是「没有这一条评论」（12006，楼主那条被删了那种）。
	ErrCommentNotFound = errors.New("bili: 没有这一条评论")

	// ErrCommentNoComments 是「这个对象在，只是**还没有人评论**」。
	//
	// 🔴 **它和 `ErrCommentTargetMissing` 是两回事，各有一句人话**（票 #571）：
	//
	//	对象不在了   → 再问多少次都没有，界面上说「这个对象已经不在了」
	//	还没有人评论 → **它好好地在那儿**，界面上说「还没有人评论」
	//
	// 混成一句话的话，用户看到「这个对象不在了」会以为歌没了，而歌好好地
	// 摆在那儿——**他会去重新下载一遍**。
	//
	// ⚠️ **它不是失败**：B 站回的是 code 0 + 空列表。这一页**照常返回**
	//（`AllCount`、`UpperMid` 那些都还有值），只是**同时**带出这个哨兵，
	// 让调用方能一眼分清「没人评论」和「拉失败了」。
	ErrCommentNoComments = errors.New("bili: 还没有人评论")
)

// CommentEmote 是正文里的一个表情占位符。
//
// B 站的正文里 `[doge]`、`[打call]` 这类占位符对应 `content.emote` 里的一项；
// `URL` 是那张表情图的地址，**可以直接画**。
type CommentEmote struct {
	// ID 是这个表情的编号。
	ID int64 `json:"id"`
	// URL 是表情图的地址。
	URL string `json:"url"`
	// Type 是表情的分类（B 站自己那套编号）。
	Type int `json:"type"`
	// Text 是占位符本身（「[doge]」）。
	Text string `json:"text"`
	// JumpTitle 是表情的名字（「律敬礼」那种）。
	JumpTitle string `json:"jump_title"`
}

// CommentPicture 是评论附带的一张图片。
type CommentPicture struct {
	// URL 是图片的地址（B 站给的是 `http://`，界面那边自己升级成 https）。
	URL string
	// Width / Height 是图片的像素尺寸。
	Width  int
	Height int
	// Size 是文件大小（**KB**，B 站给的就是 KB）。
	Size int64
}

// Comment 是一条评论。
//
// 字段是照研究票 #537 报告 §3.4 那张**实测字段表**挑的——不是照着自己写的
// 结构体造的。
type Comment struct {
	// Rpid 是这条评论的 id（**评论的主键**）。
	Rpid int64
	// Mid 是发这条评论的人的 mid。
	Mid int64
	// Root 是楼中楼的根评论 id；主列表上永远是 0。
	Root int64
	// Parent 是被回复的那条评论 id；主列表上永远是 0。
	Parent int64
	// Count 是这条评论下的**总回复数**（含楼中楼）。
	Count int64
	// RCount 是这条评论下的**楼中楼条数**（不含「查看更多回复」那几层）。
	RCount int64
	// Like 是点赞数。
	Like int64
	// Ctime 是发表时间（Unix 秒）。
	Ctime int64

	// Uname 是昵称。
	Uname string
	// Avatar 是头像地址。
	//
	// ⚠️ 是 `member.avatar`，**不是 `face`**——实测 `face` 这个键压根不存在。
	Avatar string
	// Level 是用户等级（`member.level_info.current_level`）。
	Level int
	// OfficialType 是认证类型（`member.official_verify.type`）：
	// **-1 = 无认证，0 = 个人认证，1 = 机构认证**。
	OfficialType int
	// OfficialDesc 是认证文案（`member.official_verify.desc`）。
	OfficialDesc string

	// Message 是正文。
	Message string
	// TimeDesc 是 B 站给好的相对时间（「9小时前发布」）——**不用自己算**。
	TimeDesc string
	// SubReplyEntryText 是现成的回复数文案（「共102条回复」）。
	SubReplyEntryText string
	// Location 是 IP 属地。
	//
	// ⚠️ **这个键是条件出现的**：实测（2026-10-27 真机回归，1064 条评论）
	// 只有 254 条带 `reply_control.location`，其余 810 条**压根没有这个键**
	// （不是空串）。而 B 站**给的时候给的就是有内容的**（那 254 条一条
	// 空串都没有）。
	//
	// ⇒ **别拿它判「有没有」**：空串就是「B 站没给」。
	Location string

	// IsUpper 是「这条评论是 UP 主自己发的」。
	//
	// ⚠️ **判据是 `mid == data.upper.mid`，不是 `reply_control.up_reply`。**
	//
	// `up_reply` 的意思是「**UP 主回复过这一条**」，跟「这条是 UP 主发的」是
	// 两回事。实测（2026-10-26，oid=114256632618805）：`upper.mid` = 654552，
	// 而带 `up_reply=true` 的那几条评论 mid 是 294067289 / 594810944 / …——
	// **没有一条等于 UP 主**。拿 `up_reply` 当「是 UP 主发的」，界面上会给几十条
	// 普通用户的评论挂上 UP 主标记。
	IsUpper bool

	// Emotes 是正文里的表情（占位符 → 那张表情图）。
	//
	// ⚠️ 正文里的 `[doge]` 要**拿这里的 key 去换**，直接画正文会看到占位符本身。
	Emotes map[string]CommentEmote
	// Pictures 是评论附带的图片。
	Pictures []CommentPicture
}

// CommentPage 是一页评论（主列表）。
type CommentPage struct {
	// Replies 是这一页的评论。
	Replies []Comment
	// AllCount 是评论总数（`cursor.all_count`）。
	AllCount int64
	// Next 是下一页的游标（`cursor.next`）。
	//
	// ⚠️ **它的含义随排序变**（实测 2026-10-26，oid=170001）：
	//
	//	最热档（mode=3）  next 是**页码**（1 → 2 → 3 …）
	//	最新档（mode=2）  next 是**楼层号**（160595 → 160592 → …，递减）
	//
	// 🔴 **这一栏**不**拿它翻页**——翻页走 `NextOffset`（理由见那一栏）。
	// **别把它带回去**：在「评论」那一档（`mode=1`）上带上去 B 站回
	// `-400 min_score(1.000000) must be <= max_score(0.000000)`（实测）。
	//
	// 它留着是因为 B 站确实给这一栏，而它记着「B 站给的是哪一档的那套游标」
	//（`Mode` 也是同一个 `cursor` 里的东西）。
	Next int64
	// NextOffset 是下一页的偏移量（`cursor.pagination_reply.next_offset`）。
	//
	// # 🔴 **翻页走的是它，不是 `cursor.next`**
	//
	// ⚠️ **`cursor.next` 在「评论」那一档（mode=1）上是坏的**（2026-10-27
	// 真机回归实测）：带上去 B 站回
	// `-400 min_score(1.000000) must be <= max_score(0.000000)`。
	//
	// 根因：`cursor.support_mode` **恒为 `[2, 3]`，不含 1**——B 站已经不支持
	// mode=1 这一档的游标了（它认得 `mode=1`，但给回来的 `next` 是另一套，
	// 拿它去翻就撞上了 B 站内部的分数区间）。
	//
	// 而 `pagination_reply.next_offset` + `pagination_str={"offset":"…"}`
	// **三档都能翻**（实测：mode=3 / 1 / 2 都翻到了第二页，而且每一页的内容
	// 互不相同）。
	//
	// ⚠️ **`next_offset` 是 B 站给的字符串，一个字都别改**（它带 base64 里的
	// `+` `/` `=`）。改一个字符，B 站**不报错**——它回的是**第一页**（实测：
	// 拿一个乱写的 offset 去问，它回 code 0 + 第一页）——于是界面上出现
	// 「加载更多」按下去什么都没变。
	//
	// ⚠️ **空了就是到底了**：`is_end=true` 的那一页没有这个键。
	NextOffset string
	// IsEnd 是「到底了」（`cursor.is_end`）。
	IsEnd bool
	// UpperMid 是这个对象的 UP 主 mid（`data.upper.mid`）。
	//
	// ⚠️ **「这条评论是不是 UP 主发的」就靠它判**（见 `Comment.IsUpper`）。
	UpperMid int64
	// Mode 是 B 站**实际采用的**排序（`cursor.mode`）。
	//
	// ⚠️ **它跟请求里的 `mode` 未必一样**：实测请求 `mode=0` 时 B 站回的是
	// `cursor.mode=3`（热门评论）。所以界面上写「最新评论」要认这一栏，别认
	// 自己请求时传的那个。
	Mode int
	// Name 是 B 站给这一档起的名字（「热门评论」/「评论」/「最新评论」）。
	Name string
}

// ─────────────────────────────────────────────────────────────────────────────
// 解码：中间那些结构体
// ─────────────────────────────────────────────────────────────────────────────

// commentReplyData 是主列表的 `data` 段。
type commentReplyData struct {
	Cursor struct {
		Next  int64 `json:"next"`
		IsEnd bool  `json:"is_end"`
		// ⚠️ **`all_count` 是条件出现的**（实测 2026-10-27：「一条评论都没有」
		// 那一档压根没有这个键，不是 0）。
		AllCount int64  `json:"all_count"`
		Mode     int    `json:"mode"`
		Name     string `json:"name"`
		// PaginationReply 是**能翻页的那一组游标**（理由见
		// `CommentPage.NextOffset`）。
		//
		// ⚠️ **`next_offset` 是个字符串**（带 base64 里的 `+` `/` `=`），
		// 不是数字——按数字解会让这一整页读不出来。
		PaginationReply struct {
			NextOffset string `json:"next_offset"`
		} `json:"pagination_reply"`
	} `json:"cursor"`
	Replies []commentReply `json:"replies"`
	Upper   struct {
		Mid int64 `json:"mid"`
	} `json:"upper"`
}

// commentReply 是 B 站返回的一条评论的**原始形状**。
//
// ⚠️ **它照抄的是真接口的形状**（研究票 #537 报告 §3.4 那张实测字段表），
// 不是照着 `Comment` 造的——中间这一层存在的意义就是**把真形状和我们要的
// 形状隔开**：B 站改字段名时改的是这一层，`Comment` 不动。
type commentReply struct {
	Rpid   int64 `json:"rpid"`
	Mid    int64 `json:"mid"`
	Root   int64 `json:"root"`
	Parent int64 `json:"parent"`
	Count  int64 `json:"count"`
	RCount int64 `json:"rcount"`
	Like   int64 `json:"like"`
	Ctime  int64 `json:"ctime"`

	Member struct {
		Mid   string `json:"mid"`
		Uname string `json:"uname"`
		// ⚠️ 头像在 `avatar`，**不在 `face`**（实测 `face` 这个键不存在）。
		Avatar    string `json:"avatar"`
		LevelInfo struct {
			CurrentLevel int `json:"current_level"`
		} `json:"level_info"`
		OfficialVerify struct {
			Type int    `json:"type"`
			Desc string `json:"desc"`
		} `json:"official_verify"`
	} `json:"member"`

	Content struct {
		Message string                  `json:"message"`
		Emote   map[string]CommentEmote `json:"emote"`
		// ⚠️ **`pictures` 走的是原始那一层**（`rawCommentPicture`），因为
		// `img_size` 的类型 B 站改过（见那个类型的注释）。
		Pictures []rawCommentPicture `json:"pictures"`
	} `json:"content"`

	ReplyControl struct {
		TimeDesc string `json:"time_desc"`
		Location string `json:"location"`
		// ⚠️ `up_reply` 是「UP 主回复过这一条」，**不是「这条是 UP 主发的」**。
		// 判「是 UP 主发的」靠 `mid == data.upper.mid`（见 `Comment.IsUpper`）。
		UpReply           bool   `json:"up_reply"`
		SubReplyEntryText string `json:"sub_reply_entry_text"`
	} `json:"reply_control"`
}

// commentSubReplyData 是楼中楼的 `data` 段。
type commentSubReplyData struct {
	Page struct {
		Num   int `json:"num"`
		Size  int `json:"size"`
		Count int `json:"count"`
	} `json:"page"`
	Replies []commentReply `json:"replies"`
	Upper   struct {
		Mid int64 `json:"mid"`
	} `json:"upper"`
}

// toComment 把一条原始评论变成我们要的那一份。
//
// upperMid 是这个对象的 UP 主 mid（`data.upper.mid`）——**判「这条是不是 UP
// 主发的」就靠它**（理由见 `Comment.IsUpper`）。
func (r commentReply) toComment(upperMid int64) Comment {
	c := Comment{
		Rpid:   r.Rpid,
		Mid:    r.Mid,
		Root:   r.Root,
		Parent: r.Parent,
		Count:  r.Count,
		RCount: r.RCount,
		Like:   r.Like,
		Ctime:  r.Ctime,

		Uname:  r.Member.Uname,
		Avatar: r.Member.Avatar,
		Level:  r.Member.LevelInfo.CurrentLevel,
		// ⚠️ **没有认证时 B 站给的是 -1**，那不是「认证类型 0」。
		OfficialType: r.Member.OfficialVerify.Type,
		OfficialDesc: r.Member.OfficialVerify.Desc,

		Message:           r.Content.Message,
		TimeDesc:          r.ReplyControl.TimeDesc,
		SubReplyEntryText: r.ReplyControl.SubReplyEntryText,
		Location:          r.ReplyControl.Location,

		// ⚠️ **判据是 mid == upperMid**，不是 `reply_control.up_reply`。
		IsUpper: r.Mid == upperMid && upperMid != 0,

		Emotes: r.Content.Emote,
	}
	for _, p := range r.Content.Pictures {
		c.Pictures = append(c.Pictures, p.toPicture())
	}
	return c
}

// rawCommentPicture 是 B 站返回的一张图片的**原始形状**。
//
// 🔴 **`img_size` 的类型 B 站改过，而按 `int` 解会让整页评论读不出来。**
//
// 实测（2026-10-26，只读探针）：同一个接口、同一个字段，两页返回的
// `img_size` 分别是 `283.87`（**小数**）、`109.92`、`236.8` 和 `57.02`——
// 而别的页子里它是整数。`encoding/json` 撞上类型对不上会让**整个反序列化
// 失败**，于是「一条评论带一张图」就变成「**整页 30 条评论一条都读不出来**」，
// 而界面上只是一个空列表（没有报错、没有提示、什么都没有）。
//
// ⇒ 用 `flexInt`（`flexjson.go` 里那个「读不出来就当零值、**绝不**报错」的
// 类型）解它。这跟 `flexjson.go` 文件头记的是同一类事故。
type rawCommentPicture struct {
	URL    string  `json:"img_src"`
	Width  flexInt `json:"img_width"`
	Height flexInt `json:"img_height"`
	// ⚠️ **KB，不是字节**——B 站给的就是 KB（`283.87` = 283.87 KB）。
	Size flexInt `json:"img_size"`
}

// toPicture 把原始那张图片变成我们要的那一份。
func (p rawCommentPicture) toPicture() CommentPicture {
	return CommentPicture{
		URL:    p.URL,
		Width:  int(p.Width.Int64()),
		Height: int(p.Height.Int64()),
		Size:   p.Size.Int64(),
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 两个接口
// ─────────────────────────────────────────────────────────────────────────────

// commentPagination 是 `pagination_str` 那个查询参数的形状。
//
// ⚠️ **它不是 JSON 随便拼的**：B 站认的是 `{"offset":"…"}` 这一个键，而
// **带一个不是 JSON 的串回去是 `-400 请求错误`**（实测）。
//
// ⚠️ **offset 原样带**：它是 B 站给的字符串（含 base64 的 `+` `/` `=`），
// 一个字都不能改（改了的症状见 `CommentList` 的注释）。
type commentPagination struct {
	Offset string `json:"offset"`
}

// CommentList 拉一页评论（主列表）。
//
// typ 是对象类型（`CommentTypeVideo` / `CommentTypeAudio`），oid 是那个对象的
// id（**视频是 avid，音频是 auid**——不是 bvid）。
//
// mode 是排序（`CommentSortHot` / `CommentSortDefault` / `CommentSortLatest`）；
// offset 是上一页给的**偏移量**（`CommentPage.NextOffset`，**第一页传空串**）；
// ps 是每页几条（**最多 30**）。
//
// # 🔴 翻页走的是 `offset`，不是 `cursor.next`
//
// 实测 2026-10-27（票 #591）：`cursor.next` 在「评论」那一档（`mode=1`）上
// **必定失败**——B 站回
// `-400 min_score(1.000000) must be <= max_score(0.000000)`。
//
// 根因是 `cursor.support_mode` **恒为 `[2, 3]`，不含 1**：B 站已经不支持
// mode=1 这一档的游标了。
//
// 而 `cursor.pagination_reply.next_offset` +
// `pagination_str={"offset":"<next_offset>"}` **三档都能翻**（实测 mode=3 /
// 1 / 2 都翻到了第二页，且每页内容互不相同）。
//
// ⇒ **`next` 这个参数整个换掉了**：留着一个「带上去就必挂」的参数在签名里，
// 下一个人一定会用它。
//
// ⚠️ **offset 一个字都别改**：它带 base64 里的 `+` `/` `=`。改一个字符，B 站
// **不报错**——它回的是**第一页**（实测：拿一个乱写的 offset 去问，它回
// code 0 + 第一页）——于是界面上「加载更多」按下去什么都没变。
//
// # 🔴 `Call.Buvid` 这里**绝对不设**
//
// 理由见文件头的坑一：带 `buvid3` 会让 B 站把列表砍到 3 条并宣称「到底了」。
// 本仓库「取流要带 buvid」的既有习惯**不能照抄到这里**。
//
// # 🔴 `Call.NoBuvid` 这里**必须写 true**
//
// 光「不设 `Buvid`」**挡不住 jar 里已经有的 buvid3**——jar 的 cookie 是在
// 运输层之前加上的（理由见文件头坑一）。而 jar 里确实会有 buvid3（登录时
// B 站下发的那批 cookie 全被存了下来）。
//
// # 🔴 `Call.WBI` 这里也**不设**
//
// 用的是不带 WBI 的 `/x/v2/reply/main`（理由见文件头的坑二）。
func (c *Client) CommentList(ctx context.Context, typ, oid int64, mode int, offset string, ps int) (CommentPage, error) {
	if oid <= 0 {
		return CommentPage{}, fmt.Errorf("%w: oid 不对（%d）", ErrCommentTargetMissing, oid)
	}
	ps = clampCommentPageSize(ps, commentPageSizeMax)
	// 「没指定排序」要落到一个**认得出来的**档上（见 `CommentSort` 的注释）。
	mode = normalizeCommentMode(mode)

	q := url.Values{
		"type": {strconv.FormatInt(typ, 10)},
		"oid":  {strconv.FormatInt(oid, 10)},
		"mode": {strconv.Itoa(mode)},
		"ps":   {strconv.Itoa(ps)},
	}
	// ⚠️ **`pagination_str` 只在有 offset 的时候才带**。
	//
	// ⚠️ **带了空的 offset 是不对的**：实测 `pagination_str={"offset":""}`
	// 回的是**第一页**（code 0 + 第一页那 19 条），不是错误——而空串的意思
	// 本来是「从头」。所以这里**只在 offset 非空时才带**。
	//
	// ⚠️ **`pagination_str` 必须是合法的 JSON**：实测传一个不是 JSON 的串，
	// B 站回 `-400 请求错误`（不是「读不懂」，所以别指望它自己纠正）。
	if offset != "" {
		// ⚠️ **用 `json.Marshal` 编，不要手拼**：offset 里有 `+` `/` `=`，
		// 手拼一个字符串进去要处理引号转义，而 `json.Marshal` 一次搞定。
		b, err := json.Marshal(commentPagination{Offset: offset})
		if err != nil {
			return CommentPage{}, fmt.Errorf("bili: 翻页游标编不出来: %w", err)
		}
		q.Set("pagination_str", string(b))
	}

	raw, err := c.Do(ctx, Call{
		Method: "GET",
		URL:    c.hostAPI + pathReplyMain,
		Query:  q,
		// ⚠️ **Buvid 故意不设、WBI 故意不设**——理由见文件头那三个坑。
		//
		// ⚠️ **`NoBuvid` 一定要写 true**：光「不设 Buvid」挡不住 jar 里已有的
		// buvid3（jar 的 cookie 在运输层之前就加上了，理由见文件头坑一）。
		NoBuvid: true,
	})
	if err != nil {
		return CommentPage{}, wrapCommentError(err)
	}

	var d commentReplyData
	if err := json.Unmarshal(raw, &d); err != nil {
		return CommentPage{}, fmt.Errorf("bili: 评论列表读不懂: %w", err)
	}
	page := CommentPage{
		AllCount:   d.Cursor.AllCount,
		Next:       d.Cursor.Next,
		NextOffset: d.Cursor.PaginationReply.NextOffset,
		IsEnd:      d.Cursor.IsEnd,
		UpperMid:   d.Upper.Mid,
		Mode:       d.Cursor.Mode,
		Name:       d.Cursor.Name,
	}
	for _, r := range d.Replies {
		page.Replies = append(page.Replies, r.toComment(d.Upper.Mid))
	}
	// ⚠️ **「还没有人评论」也带一个哨兵出来**——它和「对象不在了」必须能分开
	// （理由见 `ErrCommentNoComments`）。
	//
	// 这一页**照常返回**（`AllCount` / `UpperMid` 那些都还有值），所以调用方
	// 既能画一个空列表，也能分清「没人评论」和「拉失败了」。
	//
	// ⚠️ **只在 B 站说「到底了」的时候才算「没有评论」**：翻页途中遇到一个
	// 空页而 `is_end` 还是 false，那是**翻页没翻完**（还要继续拉），不是
	// 「这个对象一条评论都没有」。混起来的话，界面会在翻到一半时忽然说
	// 「还没有人评论」。
	if len(page.Replies) == 0 && page.IsEnd {
		return page, ErrCommentNoComments
	}
	return page, nil
}

// CommentReplies 拉一层的楼中楼。
//
// root 是那条被回复的主评论 id（`Comment.Rpid`），pn 是页码（**从 1 起**），
// ps 是每页几条（**最多 20**——B 站给多了也只给 20）。
//
// 返回这一页的评论，和**那一层的总回复数**（`page.count`）。
func (c *Client) CommentReplies(ctx context.Context, typ, oid, root int64, pn, ps int) ([]Comment, int, error) {
	if oid <= 0 {
		return nil, 0, fmt.Errorf("%w: oid 不对（%d）", ErrCommentTargetMissing, oid)
	}
	if root <= 0 {
		return nil, 0, fmt.Errorf("%w: root 不对（%d）", ErrCommentNotFound, root)
	}
	if pn <= 0 {
		pn = 1
	}
	ps = clampCommentPageSize(ps, commentReplyPageSizeMax)

	q := url.Values{
		"type": {strconv.FormatInt(typ, 10)},
		"oid":  {strconv.FormatInt(oid, 10)},
		"root": {strconv.FormatInt(root, 10)},
		"pn":   {strconv.Itoa(pn)},
		"ps":   {strconv.Itoa(ps)},
	}
	raw, err := c.Do(ctx, Call{
		Method: "GET",
		URL:    c.hostAPI + pathReplyReply,
		Query:  q,
		// ⚠️ **Buvid 故意不设、WBI 故意不设**——理由见文件头那三个坑。
		//
		// ⚠️ **`NoBuvid` 一定要写 true**：光「不设 Buvid」挡不住 jar 里已有的
		// buvid3（jar 的 cookie 在运输层之前就加上了，理由见文件头坑一）。
		NoBuvid: true,
	})
	if err != nil {
		return nil, 0, wrapCommentError(err)
	}

	var d commentSubReplyData
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, 0, fmt.Errorf("bili: 楼中楼读不懂: %w", err)
	}
	out := make([]Comment, 0, len(d.Replies))
	for _, r := range d.Replies {
		out = append(out, r.toComment(d.Upper.Mid))
	}
	// ⚠️ 同 `CommentList`：这一层**压根没人回**（`page.count` 是 0）时，
	// 也是「还没有人评论」，也带哨兵。
	//
	// ⚠️ **但 `page.count > 0` 而这一页是空的不算**——那是「翻到这一页刚好
	// 没有了」，不是「这一层没人回」。混起来的话，界面会在楼中楼翻到一半时
	// 忽然说「还没有人评论」。
	if len(out) == 0 && d.Page.Count == 0 {
		return out, d.Page.Count, ErrCommentNoComments
	}
	return out, d.Page.Count, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 错误分类：-403 在评论这一处单独判
// ─────────────────────────────────────────────────────────────────────────────

// commentRetryableCodes 是**评论这一处**判成「可重试」的业务码。
//
// 🔴 **为什么 -403 在这里要单独判**：全局那张表（`retry.go`）把 -403 判成
// **不可重试**（`codeMessages[-403]` = 「没有权限看这一项」）。那在别的接口上
// 是对的，在评论接口上是**错的**——评论接口的 -403 多半是「签名没签上 /
// 被挡了一下」，**再问一次有可能好**。
//
// ⚠️ **不许把它加进 `retry.go` 的全局表**：那会波及别的接口（票 #571 的
// 纪律）。评论这一处的判据走 `CommentRecoverable`。
var commentRetryableCodes = map[int]bool{
	-403: true,
}

// CommentRecoverable 报这个评论相关的错**现在立刻再问一次有没有用**。
//
// ⚠️ **它是评论这一处的判据，不是全局那张表**（理由见 `commentRetryableCodes`）。
// 别的接口仍然用 `Recoverable`。
func CommentRecoverable(err error) bool {
	if err == nil {
		return false
	}
	// 「对象不在了」「评论区关了」「没有这一条」再问多少次都一样。
	if errors.Is(err, ErrCommentTargetMissing) ||
		errors.Is(err, ErrCommentSectionClosed) ||
		errors.Is(err, ErrCommentNotFound) {
		return false
	}
	// ⚠️ **「还没有人评论」不算失败**，所以也不给「立刻重试」那枚键——
	// 这一条压根没有错，用户要的是「一个空列表」而不是「再问一次」。
	if errors.Is(err, ErrCommentNoComments) {
		return false
	}
	var api *APIError
	if errors.As(err, &api) {
		return commentRetryableCodes[api.Code]
	}
	// 别的（网络、超时、截断）走全局那张表——那些判据跟接口无关。
	return Recoverable(err)
}

// CommentFriendly 把评论这一处的错翻成一句用户看得懂的话。
//
// ⚠️ **它跟 `Friendly` 是两回事**：全局那张表里 -403 的话是「没有权限看这一
// 项」，那在评论这一处是**错的**（见文件头的坑三）。所以评论这一处自己说。
//
// 「对象不存在」和「0 条评论」**各有一句**（票 #571 的验收）：混成一句的话，
// 用户看到「这个对象不在了」会以为歌没了，而歌好好地摆在那儿。
func CommentFriendly(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, ErrCommentTargetMissing):
		return "这个对象已经不在了，看不到它的评论"
	case errors.Is(err, ErrCommentSectionClosed):
		return "这个评论区被 UP 主关掉了"
	case errors.Is(err, ErrCommentNotFound):
		return "这一条评论已经不在了"
	case errors.Is(err, ErrCommentNoComments):
		// ⚠️ **这一句必须和「对象不在了」那句话不一样**（票 #571 的验收）。
		//
		// 这一条**不是失败**——对象好好地在那儿，只是没人评论。所以这句只说
		// 「还没有人评论」，**一个字都不提「不在了」**。
		return "还没有人评论"
	}
	var api *APIError
	if errors.As(err, &api) && api.Code == -403 {
		// 评论接口的 -403 多半是「被挡了一下 / 签名没签上」——**再问一次有可能好**，
		// 所以这句话要带上下一步，而全局那句「没有权限看这一项」只会让人干等。
		return "B 站这次挡住了评论请求，再试一次就好"
	}
	return Friendly(err)
}

// wrapCommentError 把 B 站的业务错误码翻成评论这一处的哨兵。
//
// ⚠️ **只翻评论这一处有的那几种**，别的照原样返回（`*APIError`）——全局的
// 错误分类一个字都不改。
func wrapCommentError(err error) error {
	if err == nil {
		return nil
	}
	var api *APIError
	if !errors.As(err, &api) {
		return err
	}
	switch api.Code {
	case -404:
		// 「啥都木有」——**对象不存在**（不是「0 条评论」）。
		return fmt.Errorf("%w: %s", ErrCommentTargetMissing, api.Endpoint)
	case 12061:
		// 「UP主已关闭评论区」。
		return fmt.Errorf("%w: %s", ErrCommentSectionClosed, api.Endpoint)
	case 12006:
		// 「没有该评论」——楼主那条被删了。
		return fmt.Errorf("%w: %s", ErrCommentNotFound, api.Endpoint)
	}
	return err
}

// clampCommentPageSize 把每页条数封顶到 B 站给的上限。
//
// ⚠️ **不封顶的话 B 站不报错，只是默默截断**：翻页从第二页开始会漏掉一批，
// 而界面上看不出任何异样（总数在那儿，列表在增长，唯独中间少了一段）。
func clampCommentPageSize(ps, max int) int {
	if ps <= 0 {
		return max
	}
	if ps > max {
		return max
	}
	return ps
}

// normalizeCommentMode 把「没指定排序」落到一个**认得出来的**档上。
//
// ⚠️ **不能把 0 原样发出去**：实测 `mode=0` B 站会当成 `mode=3`（热门评论）
// 处理——那还行；**但 `mode=99` 直接回 `-400 invalid mode`**。所以这里认不出的
// 值一律回落到「热门评论」那一档（B 站自己的默认值）。
func normalizeCommentMode(mode int) int {
	switch mode {
	case int(CommentSortHot), int(CommentSortDefault), int(CommentSortLatest):
		return mode
	}
	return int(CommentSortHot)
}

// CommentText 把正文里的表情占位符换成能看懂的标记。
//
// ⚠️ **不是必须的**：界面要画表情图的话直接用 `Comment.Emotes` 换。这个
// 函数的用处是**给不能画图的地方**（比如搜索、通知）一句能读的话。
func (c Comment) CommentText() string {
	if len(c.Emotes) == 0 {
		return c.Message
	}
	msg := c.Message
	for placeholder, emote := range c.Emotes {
		if placeholder == "" {
			continue
		}
		label := emote.JumpTitle
		if label == "" {
			label = strings.Trim(placeholder, "[]")
		}
		msg = strings.ReplaceAll(msg, placeholder, "["+label+"]")
	}
	return msg
}
