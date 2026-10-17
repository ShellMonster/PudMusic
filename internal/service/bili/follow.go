// 我的关注那一页要的那几个接口。
//
// # 四个接口
//
//	GET /x/relation/tags                       关注分组（左边那一列 Tab）
//	GET /x/relation/tag?tagid=&pn=&ps=         某一组里有哪些人
//	GET /x/relation/followings?vmid=&pn=&ps=   「全部关注」那一格
//	GET /x/relation/followings/search?…        在我关注的人里按名字搜
//
// 参数、页大小、字段名逐个对着 Biu 的 `src/service/relation-followings.ts` 和
// `src/service/relation-tag.ts` 抄——那一页的四个函数就是这四个接口，**不多不少**。
//
// # ⚠️ `/x/relation/followings` 有两个调用方，参数不一样
//
// 动态流左侧的作者栏要 `order_type=attention`（按最常访问排，见 `dynamic.go` 的
// `Followings`），而这一页的「全部关注」**不给这个参数**（按关注时间排，
// `follow-list/index.tsx:89` 就是这么调的）。给错了不报错、列表还是对的，只是
// 名次变了——而「名次变了」没有任何症状能指出来，所以两处各自写死自己那一份。
//
// # ⚠️ `/x/relation/tag` 的 `data` 就是一个数组，没有总数
//
// 另外三个接口都回 `{"list": […], "total": N}`，只有它回 `[…]`。所以那一格
// **报不出「这一组一共多少人」**，界面上的「还有没有」只能用分组列表里那个
// `count` 来算（`/x/relation/tags` 给的）。Biu 也是这么干的
// （`follow-list/index.tsx:104` 的 `tag?.count ?? 0`）。
//
// **别把它写成 `{"list": …}` 去解**：那样 `json.Unmarshal` 报的是一句
// 「类型不匹配」，和「B 站的返回形状变了」长得一模一样，而 fixture 照着错的合同
// 造的话全绿也证明不了什么（`favorite.go` 文件头记着同一类坑）。
//
// # 未登录一律 -101
//
// 四个都要 Cookie。界面上先判登录态、别发那个注定失败的请求——而 -101 在这一层
// 和「网络断了」长得一模一样（`client.go` 的 `ErrNotLoggedIn` 那段）。
//
// # 头像为什么要改地址
//
// B 站给的头像是原图（几百 KB 的正方形）。这一页一屏摆得下 6 张卡，每张 128
// 见方，**照原地址去取就是几十倍的浪费**，而且先转圈的正是最上面那一排。上游
// 因此在 `follow-list/user-card.tsx:71` 拼了 `@160w_160h_1c_1s.webp`
// （B 站图片服务的裁剪参数）。这里在服务层拼，界面那层拿到就是能直接取的地址。
package bili

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// 关注这一摊的路径。`followingsPath` 不在这儿——它已经写在 `dynamic.go` 里了
// （作者栏和这一页共用同一个接口，见文件头那一段）。
const (
	// relationTagsPath 是关注分组列表。B 站给 -10（特别关注）、0（默认）和用户
	// 自建的那些，按界面上的显示顺序。
	relationTagsPath = "/x/relation/tags"
	// relationTagPath 是某一个分组里的成员明细。**它的 data 是数组**（见文件头）。
	relationTagPath = "/x/relation/tag"
	// relationFollowingsSearchPath 在我关注的人里按名字搜。
	relationFollowingsSearchPath = "/x/relation/followings/search"
)

// followPageSize 是一页几个。Biu 上游的 `PAGE_SIZE`（`follow-list/index.tsx:24`）
// 是 20，四处（全部关注 / 搜索 / 分组明细）**统一用这一个数**。
const followPageSize = 20

// FollowTag 是左边那一列里的一格。
type FollowTag struct {
	// ID 是分组 id。B 站固定给出两个不是用户建的：
	// **-10 特别关注**、**0 默认分组**（其余是正数）。
	ID int
	// Name 是分组名。
	Name string
	// Count 是这一组里有多少人。**B 站上次统计的数字**。
	//
	// ⚠️ 分组明细那个接口不返回总数，「还有没有」全靠它（见文件头）。用户刚在
	// 别的窗口里关注了人，这一格要等到下次重新拉分组列表才准——而这一页每次
	// 换 Tab / 搜词都会重拉一次列表，所以偏差最多一帧。
	Count int
	// Tip 是提示信息。空串时界面上不画它。
	Tip string
}

// FollowUser 是右边网格里的一格（关注的一个人）。
//
// 这一个类型同时解四个接口的成员：**它们给的用户字段基本一样**
// （`mid` / `uname` / `face` / `sign` / `official_verify`，见上游那两个 TS 文件），
// 分成四个类型只会让界面在四份几乎一样的字段之间来回翻。
type FollowUser struct {
	Mid  int64
	Name string
	// Face 是头像地址。已经在这一层补好了协议、拼好了裁剪参数（见文件头）。
	Face string
	// Sign 是签名。空串时界面上不画它（Biu 是 `line-clamp-2`，空的就是一格空白）。
	Sign string
	// Official 是认证信息（`official_verify.desc`）。空串时不画。
	Official string
	// Special 是「特别关注」（-10 那一组）。这一版界面不用它，留着是因为
	// 「特别关注」这个分组本身就在列表里，不认它的话那一格会和别的组长得一样。
	Special bool
}

// FollowUsersPage 是「一页关注的人」加上「一共多少人」。
//
// ⚠️ **这里没有 hasMore，界面上自己算**——而且要拿**累计加载数**去算，不是这一页
// 的人数：总数 25、每页 20，第二页发回来 5 个，拿这一页的 5 个去比 25 永远是
// 「还有」，于是界面上会一直转圈、一直拉空页。
//
// 那一比必须发生**界面线程上**：已加载多少是界面状态，后台 goroutine 去读它就是
// 数据竞争（而这种错在界面上多半表现为「某一次筛选的结果配上了另一次的词」）。
// 所以这一层只报总数，怎么算是调用方的活——`internal/ui/follow.go` 的
// `fetchFollowUsers` 就是照 `later.go` 的 `fetchLater` 那个写法做的。
type FollowUsersPage struct {
	Users []FollowUser
	// Total 是 B 站报的总数。**分组明细那个接口报不出来，是 0**（见文件头），
	// 那种情况下界面改用分组自己的 `Count`。
	Total int
}

// FollowTags 列「我关注的人」分了哪些组。
//
// 一个组都没有（B 站返回空数组）是**正常状态**，不是错误：这是一个刚注册、
// 一个关注都没点的账号。B iu 上游把这一格画成「暂无内容」，我们把它说成
// 「还没关注任何人」——见 `internal/ui/follow.go` 的 `followNoneYet`。
//
// ⚠️ 未登录返回 -101（见文件头）。
func (c *Client) FollowTags(ctx context.Context) ([]FollowTag, error) {
	raw, err := c.Do(ctx, Call{Method: "GET", URL: c.hostAPI + relationTagsPath})
	if err != nil {
		return nil, err
	}
	var list []struct {
		Tagid int    `json:"tagid"`
		Name  string `json:"name"`
		Count int    `json:"count"`
		Tip   string `json:"tip"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("bili: 关注分组的返回读不懂：%w", err)
	}
	out := make([]FollowTag, 0, len(list))
	for _, t := range list {
		out = append(out, FollowTag{ID: t.Tagid, Name: t.Name, Count: t.Count, Tip: t.Tip})
	}
	return out, nil
}

// FollowList 拉「全部关注」那一格。
//
// vmid 是**自己的** mid。page 从 1 起。
//
// ⚠️ **不给 `order_type`**（见文件头）：作者栏那一列要「按最常访问」，这一页
// 要上游那个「按关注顺序」。给了就是 B 站照着排而界面没说要排。
func (c *Client) FollowList(ctx context.Context, vmid int64, page int) (FollowUsersPage, error) {
	if vmid <= 0 {
		// 没有自己的 mid 就问不出「我关注了谁」。这不是网络错误，界面据此说
		// 「还没登录」而不是「拉不到」。
		return FollowUsersPage{}, ErrNotLoggedIn
	}
	q := url.Values{
		"vmid": {strconv.FormatInt(vmid, 10)},
		"pn":   {strconv.Itoa(pageNum(page))},
		"ps":   {strconv.Itoa(followPageSize)},
	}
	raw, err := c.Do(ctx, Call{Method: "GET", URL: c.hostAPI + followingsPath, Query: q})
	if err != nil {
		return FollowUsersPage{}, err
	}
	var d struct {
		Total int `json:"total"`
		List  []struct {
			Mid     int64  `json:"mid"`
			Uname   string `json:"uname"`
			Face    string `json:"face"`
			Sign    string `json:"sign"`
			Special int    `json:"special"`
			Verify  *struct {
				Type int    `json:"type"`
				Desc string `json:"desc"`
			} `json:"official_verify"`
		} `json:"list"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return FollowUsersPage{}, fmt.Errorf("bili: 关注列表的返回读不懂：%w", err)
	}
	users := make([]FollowUser, 0, len(d.List))
	for _, u := range d.List {
		if u.Mid == 0 {
			continue
		}
		users = append(users, FollowUser{
			Mid: u.Mid, Name: u.Uname, Sign: u.Sign,
			Face:     avatarURL(u.Face),
			Special:  u.Special == 1,
			Official: officialDesc(u.Verify),
		})
	}
	return FollowUsersPage{Users: users, Total: d.Total}, nil
}

// SearchFollowings 在我关注的人里按名字搜。
//
// ⚠️ **界面上「没搜」走的是 `FollowList` 而不是这里**，即使词是空的：两条接口
// 给的是同一种形状，可是「全部关注」那个 N 是要一直挂在左边 Tab 上的
// （`follow-list/index.tsx:224`），只有不搜的那一条稳定。搜了却没结果时要显示的
// 是「没搜到」而不是「关注列表是空的」，所以这一条**只管搜过之后那一种**。
func (c *Client) SearchFollowings(ctx context.Context, vmid int64, name string, page int) (FollowUsersPage, error) {
	if vmid <= 0 {
		return FollowUsersPage{}, ErrNotLoggedIn
	}
	q := url.Values{
		"vmid": {strconv.FormatInt(vmid, 10)},
		"pn":   {strconv.Itoa(pageNum(page))},
		"ps":   {strconv.Itoa(followPageSize)},
	}
	// ⚠️ **空词也发出去**：少了 name 的话 B 站当成「不筛」，回的是全部——于是
	// 「搜了个空格」和「没搜」在界面上长得一模一样，而总数还会跳。发出去至少
	// 是一条能对得上账的请求（界面那边把搜过和没搜过分成两句话，见文件头）。
	if name != "" {
		q.Set("name", name)
	}
	raw, err := c.Do(ctx, Call{Method: "GET", URL: c.hostAPI + relationFollowingsSearchPath, Query: q})
	if err != nil {
		return FollowUsersPage{}, err
	}
	var d struct {
		Total int `json:"total"`
		List  []struct {
			Mid     int64  `json:"mid"`
			Uname   string `json:"uname"`
			Face    string `json:"face"`
			Sign    string `json:"sign"`
			Special int    `json:"special"`
			Verify  *struct {
				Type int    `json:"type"`
				Desc string `json:"desc"`
			} `json:"official_verify"`
		} `json:"list"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return FollowUsersPage{}, fmt.Errorf("bili: 搜索关注的返回读不懂：%w", err)
	}
	users := make([]FollowUser, 0, len(d.List))
	for _, u := range d.List {
		if u.Mid == 0 {
			continue
		}
		users = append(users, FollowUser{
			Mid: u.Mid, Name: u.Uname, Sign: u.Sign,
			Face:     avatarURL(u.Face),
			Special:  u.Special == 1,
			Official: officialDesc(u.Verify),
		})
	}
	return FollowUsersPage{Users: users, Total: d.Total}, nil
}

// FollowTagUsers 拉某一个分组里的人。page 从 1 起。
//
// ⚠️ **返回的只是这一页，没有 hasMore**：B 站这个接口不报总数（见文件头），
// 而「还有没有」要拿分组列表里那个 `count` 来比——那是调用方手里才有的东西。
// 在这里编一个 hasMore 就只能按「这一页满不满」猜，而满不满和到底了分不开
// （`FollowList` 那一段的注释是同一条理由）。
//
// tagid 是 0（默认分组）或 -10（特别关注）或者用户自建的那些，正负都合法，
// **所以不能拿「非零」当「有」的判据**。
func (c *Client) FollowTagUsers(ctx context.Context, tagID, page int) ([]FollowUser, error) {
	q := url.Values{
		"tagid": {strconv.Itoa(tagID)},
		"pn":    {strconv.Itoa(pageNum(page))},
		"ps":    {strconv.Itoa(followPageSize)},
	}
	raw, err := c.Do(ctx, Call{Method: "GET", URL: c.hostAPI + relationTagPath, Query: q})
	if err != nil {
		return nil, err
	}
	// ⚠️ **这里解的是一个数组**，不是 `{"list": […]}`（见文件头）。
	var list []struct {
		Mid     int64  `json:"mid"`
		Uname   string `json:"uname"`
		Face    string `json:"face"`
		Sign    string `json:"sign"`
		Special int    `json:"special"`
		Verify  *struct {
			Type int    `json:"type"`
			Desc string `json:"desc"`
		} `json:"official_verify"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("bili: 关注分组明细的返回读不懂：%w", err)
	}
	out := make([]FollowUser, 0, len(list))
	for _, u := range list {
		if u.Mid == 0 {
			continue
		}
		out = append(out, FollowUser{
			Mid: u.Mid, Name: u.Uname, Sign: u.Sign,
			Face:     avatarURL(u.Face),
			Special:  u.Special == 1,
			Official: officialDesc(u.Verify),
		})
	}
	return out, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 那几样换算
// ─────────────────────────────────────────────────────────────────────────────

// pageNum 把调用方的页码收拾成「至少 1」。
//
// 界面上的页码本来就是从 1 起的，而**0 是一个很容易从别处漏进来的值**
// （比如拿「已加载页数」直接当下一页）。B 站把 pn=0 当第一页，
// 于是症状是「第一页和第 0 页是同一批数据」——不报错，只是某一页白拉一遍。
func pageNum(page int) int {
	if page < 1 {
		return 1
	}
	return page
}

// avatarSize 是头像请求的大小。B 站图片服务按 `@<宽>w_<高>h_<模式>` 裁。
//
// 取 160 而不是卡片上的 128：高两倍是为了在 2 倍屏上不糊，而 B 站的头图服务
// 最小的那一档就是 160。
const avatarSize = "160w_160h_1c_1s"

// avatarURL 把 B 站给的头像地址变成能直接去取的。
//
// 两件事都在这里做完，界面拿到就是能用的：补协议（B 站这一族接口常给
// `//i0.hdslb.com/…`，缺协议会被当成普通路径去读盘）、拼裁剪参数。
func avatarURL(raw string) string {
	u := normalizeImageURL(raw)
	if u == "" {
		return ""
	}
	return u + "@" + avatarSize + ".webp"
}

// officialDesc 是认证那一句话，没有就是空串。
//
// ⚠️ **只看 `desc`，不看 `type`**：`type == -1` 是「没有认证」，而 B 站偶尔给
// 一个 -1 却带着一句 `desc`。照 type 判的话那一格认证画出来是一句空字符串，
// 而界面上那个位置会留一段空白——看起来像布局坏了。
func officialDesc(v *struct {
	Type int    `json:"type"`
	Desc string `json:"desc"`
}) string {
	if v == nil {
		return ""
	}
	return v.Desc
}
