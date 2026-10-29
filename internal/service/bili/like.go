// 点赞：给一首投稿点赞，以及 B 站的「一键三连」。
//
// # 三个接口
//
//	/x/web-interface/archive/relation?bvid=   我赞了没有 / 收藏了没有（GET，换歌时问一次）
//	/x/web-interface/archive/like            赞 / 取消赞（POST + CSRF）
//	/x/web-interface/archive/like/triple     一键三连：赞 + 投币 + 收藏（POST + CSRF）
//
// 形状照 Biu 上游抄：`service/web-interface-archive-like.ts`、
// `web-interface-archive-like-triple.ts`、`web-interface-archive-relation.ts`
// （`store/music-fav.ts` 的 `refreshIsFav` 调的就是第三个）。
//
// # 只有 mv 有这一套
//
// 音乐库里的歌（`Item.Type == "audio"`）在 B 站那边是 `sid`，这三个接口一个都不收它。
// Biu 那边对 audio 是拿 `coll-resource-check` 问「收没收藏」，而**赞**照样给的是
// false（`music-fav.ts` 那一支的 `setIsThumb(false)`）——和这里一致：本地文件和音乐库
// 条目一律「不可赞」，界面那一层因此不摆星标（判据是 `app.Likeable`）。
//
// # ⚠️ 写操作**绝不自动重试**
//
// PLAN §5 的重试表写死了这条（"写操作（点赞/收藏/删除）绝不自动重试"）：一次超时的
// 点赞重试一次，用户就可能拿到两个赞（或者赞完又被取消一次）。这一层于是不重试，
// 失败就把失败说给界面，由界面问用户要不要再来一次。
package bili

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	// pathArchiveRelation 问「我对这一首赞了没有、收藏了没有」。
	//
	// ⚠️ **没登录时它也给 code 0**，只是两个字段都是 false —— 那说的是「不知道」，
	// 不是「没赞」。所以调用方**别把 false 当答案**（见 `ArchiveRelation` 那段），
	// 真要写的时候让 B 站回 -101，那才是真话。
	pathArchiveRelation = "/x/web-interface/archive/relation"
	// pathArchiveLike 是赞 / 取消赞。**POST，要 CSRF**。
	//
	// 🔴 表单字段是 **`like`**（1 赞 / 2 取消赞），不是 `up`——那是**动态**点赞
	// 那个接口的字段。见 `LikeArchive` 那段。
	pathArchiveLike = "/x/web-interface/archive/like"
	// pathArchiveTriple 是一键三连。**POST，要 CSRF**。
	//
	// ⚠️ 它收的是 **aid**，不是 bvid（上游
	// `web-interface-archive-like-triple.ts` 的 `aid?: number`）。bvid → aid
	// 的那一次往返在服务层做，理由同 `favorite.go` 文件头。
	pathArchiveTriple = "/x/web-interface/archive/like/triple"
)

// ArchiveRelation 是「我对这一首做过什么」。
//
// ⚠️ **false 说的是「没有」还是「不知道」，这一层分不出来**：B 站没登录（或者
// Cookie 过期）时 `archive/relation` 照样回 `code 0`，两个字段都是 false。
//
// 所以**没登录的人看到的是「没赞」**，而界面上那两个键（赞星标、收藏）也就照着它
// 摆着——和旁边那个「收藏」键是同一条路：都摆，写的时候让 B 站回 -101，那一句
// 「赞要连着 B 站账号用」会弹出来。我们没有在没登录时把键藏起来，是**故意**跟着
// 现有的收藏键走：藏起来的话用户看到的是「这个功能怎么有时候没有」，而那比一次
// 说清楚的失败更让人困惑。
type ArchiveRelation struct {
	// Liked 是「我赞了这一首没有」。
	Liked bool
	// Favorited 是「我收藏了这一首没有」。
	//
	// 它和三连那一次的 `fav` 是同一件事：B 站那边收藏的是**投稿**，不是「哪个
	// 收藏夹」。我们自己的 `FavoriteSong` 收进的是**某一格**（默认那格），而
	// B 站的收藏关系是「有没有收藏过这一首」——所以两边的口径**不完全一样**，
	// 但界面这一层要问的正是后者（用户看到的是「这一首我收过没有」）。
	Favorited bool
}

// ArchiveRelation 问「我对这一首赞了没有、收藏了没有」。
//
// bvid 是空的直接回零值：**这不是错误**，本地文件和音乐库条目压根没有 bvid，
// 而界面问的是「这一首能不能赞」（见文件头）。
func (c *Client) ArchiveRelation(ctx context.Context, bvid string) (ArchiveRelation, error) {
	bvid = strings.TrimSpace(bvid)
	if bvid == "" {
		return ArchiveRelation{}, nil
	}
	raw, err := c.Do(ctx, Call{
		Method: "GET", URL: c.hostAPI + pathArchiveRelation,
		Query: url.Values{"bvid": {bvid}},
	})
	if err != nil {
		return ArchiveRelation{}, err
	}
	var d struct {
		Like     bool `json:"like"`
		Favorite bool `json:"favorite"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return ArchiveRelation{}, fmt.Errorf("bili: 投稿关系读不懂: %w", err)
	}
	return ArchiveRelation{Liked: d.Like, Favorited: d.Favorite}, nil
}

// LikeArchive 给一首投稿点赞（like=true）或取消点赞。**写操作**。
//
// 🔴 **表单字段叫 `like`，不叫 `up`**：web 端 `/x/web-interface/archive/like`
// 的参数表（`bilibili-API-collect` 的 `docs/video/action.md`「点赞视频（web端）」）
// 只有 `like`；`up` 是**动态**点赞那几个接口（`/x/dynamic/feed/dyn/thumb`、APP 端
// `/x/v2/view/like`）的字段。发成 `up` 时 B 站回 -400，用户看到的是 `friendly.go`
// 那句「B 站拒绝了这次请求」——而真相是我们发错了字段名。
//
// # 和 LikeDynamic 的区别：**字段名不同**
//
// 两个都是「赞 / 取消赞」，取值也都是 1/2，但**收的字段名不一样**：这边的 web 端
// 稿件接口收 `like`，动态那个接口（`LikeDynamic`，JSON 体）收 `up`。所以两处
// 的常量、变量名各按各的口径叫，不要互相抄——这一票的根因就是从动态那边抄过来的。
//
// like 的取值照 B 站：1 点赞、2 取消点赞。**0 是切换，我们不用它**——切换在界面
// 重画之后会点成反的（那一帧画出来的是旧状态），用户连点两下等于没点。
//
// ⚠️ **不重试**：见文件头。一次超时的点赞重试一次，用户手上就可能是两个赞。
func (c *Client) LikeArchive(ctx context.Context, bvid string, like bool) error {
	bvid = strings.TrimSpace(bvid)
	if bvid == "" {
		return ErrNoVideo
	}
	// ⚠️ 变量名不叫 `like`：函数参数已经占了那个名字（`like bool`）。
	likeVal := 1
	if !like {
		likeVal = 2
	}
	form := url.Values{
		"bvid": {bvid},
		"like": {strconv.Itoa(likeVal)},
	}
	if _, err := c.Do(ctx, Call{
		Method: "POST", URL: c.hostAPI + pathArchiveLike, Form: form, CSRF: true,
	}); err != nil {
		return fmt.Errorf("bili: 给投稿 %s %s: %w", bvid, likeOrUnlike(like), err)
	}
	return nil
}

// TripleResult 是「一键三连做完之后，B 站说现在是什么样」。
//
// 🔴 **这三栏是 B 站给的，不是我猜的**：长按那一把做完，界面上的赞、投币、收藏
// 三样都要按它更新（照抄 `music-thumb/index.tsx` 的
// `setIsThumb(res.data.like); setIsFav(res.data.fav)`）。界面自己把三样都置成
// 「已做」的话，B 站要是拒绝了投币（硬币不够、投过了）界面上就写着「投了」而用户
// 手上没有——而用户看得见有没有花出去那一枚币。
//
// 回包里还有一栏 `multiply`（连击数），这里**不读**：界面不用它，而读一个不用
// 的字段只会让人以为将来一定要用（真要显示「你今天连了几次」时再加，那时才对着
// 回包抄一次）。
type TripleResult struct {
	// Liked / Coined / Favorited 是三连之后这三样的状态。
	Liked     bool
	Coined    bool
	Favorited bool
}

// LikeTriple 一键三连：赞 + 投币 + 收藏。**写操作**。
//
// ⚠️ **收的是 aid**，bvid → aid 的往返在这里做（`ArchiveID`），不在界面上——
// 和 `favorite.go` 是同一条理由：界面那层不该知道「这个接口要的是另一个 id」。
//
// ⚠️ **不重试**：见文件头。三连是三件事，一次重试要么是赞了两次，要么是投了两枚
// 币（那是要花真金白银的）。
func (c *Client) LikeTriple(ctx context.Context, bvid string) (TripleResult, error) {
	bvid = strings.TrimSpace(bvid)
	if bvid == "" {
		return TripleResult{}, ErrNoVideo
	}
	aid, err := c.ArchiveID(ctx, bvid)
	if err != nil {
		return TripleResult{}, err
	}
	//
	// ⚠️ **只给 aid**：Biu 上游只给这一个（`web-interface-archive-like-triple.ts`
	// 的 `postWebInterfaceArchiveLikeTriple({aid})`，参数表只有 `aid`），而它在
	// 真实账号上天天用。B 站网页版那枚「三连」按钮另外带 `up=1&up_coin=1`，那是
	// 网页版为了让用户能只赞不投而留的口子；这个接口的默认就是「赞 + 投一枚币 +
	// 收藏」，多给两个参数没有好处，而**照抄上游已经跑通的那一份**风险更小。
	form := url.Values{"aid": {strconv.FormatInt(aid, 10)}}
	raw, err := c.Do(ctx, Call{
		Method: "POST", URL: c.hostAPI + pathArchiveTriple, Form: form, CSRF: true,
	})
	if err != nil {
		return TripleResult{}, fmt.Errorf("bili: 给投稿 %s 一键三连: %w", bvid, err)
	}
	var d struct {
		Like bool `json:"like"`
		Coin bool `json:"coin"`
		Fav  bool `json:"fav"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return TripleResult{}, fmt.Errorf("bili: 一键三连的返回读不懂: %w", err)
	}
	return TripleResult{Liked: d.Like, Coined: d.Coin, Favorited: d.Fav}, nil
}
