// 推荐页（首页）要的那几个接口的中间层。
//
// 和 `library.go` 是同一层、同一件事：**界面层不发网络请求**（SPEC 的四层
// 依赖单向），所以首页不能把 `bili.Client` 拿在手里。这一层做三件事：
//
//   - **给出行身份**（`RecommendItem.ID`）。🔴 铁律四要的是「稳定且唯一」，
//     而 B 站给回来的身份字段有三个（bvid / aid / 各自的兜底 id），挑哪个、
//     挑不到怎么办，必须只有一处写法——理由和 `LibraryItem.ID` 那段一样。
//   - **合成新歌速递**（banner + list 两半，按上游的顺序去重）。
//   - **把这一首变成能播的 `Item`**（和 `onlineItem` 是同一种东西）。
//
// ## 挂载
//
// 跟 `MountLibrary` / `MountExplore` 一样在**开窗口之前**挂（`main.go`），
// 递的还是 `MountLogin` 建的那**同一个**客户端。挂没挂由 `Mounted()` 说话，
// 界面据此说清「还没接上」，而不是画一个永远空的页面。
//
// ## 未登录时显示什么
//
// **照拉，不做登录引导。** 三个接口在没登录时都给得出东西：分区推荐给的是
// 热门那一批，音乐榜和新歌速递本来就是公开榜。所以这一页开箱就有内容，
// 而「登录之前先把整页换成一句请登录」是拿一个不必要的门槛换零收益——
// 用户连这应用是干什么的都还不知道。
//
// 登录之后那一份**更合口味**（分区推荐按 mid 算），所以换账号时这一页要重来
// 一遍（见 `internal/ui/homepage_feed.go` 的 `resetForAccount`）。这只是
// 「换账号重来」，不是「没登录不给看」。
//
// ⚠️ 万一 B 站改主意开始要登录（-101），那一支走的是**通用**的失败态：
// 一句人话 + 一个「重试」（`bili.Friendly` 把 -101 翻成「登录已经过期了，
// 重新扫码登录一下」）。**不在这一层为它单开一个分支**——真发生了那句话已经
// 说清了该做什么，而一个专用分支还得跟着 B 站改。
package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// errRecommendMissing 是「首页还没挂上 B 站客户端」。
var errRecommendMissing = errors.New("app: 首页还没挂上 B 站客户端")

// Recommend 是首页要的接口的依赖。零值不挂——界面要判空。
type Recommend struct {
	// Client 是 B 站客户端。**全项目那一份**（`MountLogin` 建的那一个）。
	Client *bili.Client
}

// Mounted 报这一摊挂上没有。界面靠它说清「还没接上」。
func (r *Recommend) Mounted() bool { return r != nil && r.Client != nil }

// RecommendItem 是首页一行（三个 Tab 的推荐单、还有新歌速递那一格）。
//
// 画得出来的字段直接对着 `list.Music` 那一组，这一层不认识界面那一层的
// 类型，界面层负责翻（见 `internal/ui/homepage_feed.go`）。
type RecommendItem struct {
	// ID 是这一行的身份，🔴 铁律四：稳定且唯一。**拿不到就是空串**，
	// 空串的行由界面丢掉（画一行没有身份的歌 = 和别的行共用状态）。
	ID string

	Title string
	Cover string
	// Owner 是 UP 名，空串时那一行不画它。
	Owner    string
	OwnerMid int64
	// Duration 是时长。B 站给的是整数秒，秒在这一层换成 `time.Duration`
	//（见文件头「把秒翻成 Duration」那一层的老规矩）。
	Duration time.Duration
	// View 是播放量。
	View int64
	// Item 是播放和右键菜单真正用的那一项。已是 `Source: "online"`。
	//
	// ⚠️ **榜上有些条目没有稿件**（纯音乐条目，`related_archive` 是空的），
	// 那种行画得出来（身份走榜上那一条的 id）而**放不了**：没有 bvid，取流要
	// bvid。
	//
	// 界面上**在按之前就不给入口**：`widgets/list` 那一份共用菜单（搜索结果、
	// 新歌速递、首页都用它）会把「播放 / 下一首播放 / 添加到队列」按成灰的，
	// 并在菜单末尾补一句「为什么」和「还能怎么办」（见 `menuItems` 与
	// `app.UnplayableMenuNote`）。上游 Biu 在这一页上不给纯音乐条目置灰，
	// 而用户看到的是一个按了没反应的入口——那比灰掉更难解释。
	Item Item
}

// recommendItems 把 B 站那一份翻成界面那一行，并按身份去重。
//
// ⚠️ **去重是必须的，不是锦上添花**：B 站的推荐流会在同一页里给出同一个
// 稿件（它自己会去重，但那是它那一份；两页合并、或者换一次刷新就可能撞上），
// 而**两个一模一样的行身份**会让框架按 key 对位时那两行共用一份状态——
// Tester 里当场 panic，真窗口里选中一个另一个也亮，界面上看不出任何异样。
//
// dropped 是丢掉的条数。**不画在界面上**（用户看见的是一屏正常的歌），只
// 用来在日志里说清「B 站给了 N 条，我们画出来 N-k 条」。
func recommendItems(in []bili.RecommendItem) (out []RecommendItem, dropped int) {
	seen := make(map[string]bool, len(in))
	for _, it := range in {
		id := recommendID(it)
		if id == "" {
			dropped++
			continue
		}
		if seen[id] {
			dropped++
			continue
		}
		seen[id] = true
		out = append(out, RecommendItem{
			ID: id, Title: it.Title, Cover: it.Cover,
			Owner: it.Owner, OwnerMid: it.OwnerMid,
			Duration: it.Duration, View: it.View,
			Item: recommendItem(it),
		})
	}
	return out, dropped
}

// recommendID 算一条推荐的行身份，**顺序只有这一处**。
//
//	bvid → aid → TrackID（B 站给的那个兜底 id）
//
// 和上游的 `item.aid ?? item.bvid ?? trackid ?? fallbackId`
// （`music-recommend/index.tsx:57`）**故意不一样**：上游把 aid 排在 bvid 前面，
// 而这一页的行身份在别处也是 bvid（`LibraryItem.ID`、右键菜单里收藏用的 rid
// 两边都要）。摆成同一个顺序的话，同一首歌在推荐页和在稍后再看会是两个身份，
// 而 `queue` 那边的判重是按 bvid 的。
//
// ⚠️ **三个都认不出来时返回空串，不是拿序号顶上**：序号是「父 ID + 兄弟
// 序号」那一套，前面插一行会让后面所有行的状态整体错配（铁律四）。
func recommendID(it bili.RecommendItem) string {
	switch {
	case it.Bvid != "":
		return it.Bvid
	case it.Aid > 0:
		return "aid:" + strconv.FormatInt(it.Aid, 10)
	case it.TrackID != "":
		return "id:" + it.TrackID
	}
	return ""
}

// recommendItem 把一条推荐变成能进队列的那一项。
//
// 没有 bvid 时给的是 `Source: "online"`、Bvid 空串的项——它进不了队列，
// 而界面上在按之前就不给它播放入口（见 `RecommendItem.Item` 上那一段）。
func recommendItem(it bili.RecommendItem) Item {
	return onlineItem(it.Bvid, it.Cid, it.Title, it.Cover, it.Owner, it.OwnerMid)
}

// ─────────────────────────────────────────────────────────────────────────────
// 三个 Tab 各自的取数
// ─────────────────────────────────────────────────────────────────────────────

// HomeTab 是首页那三个 Tab 的下标，**和界面层的下标一模一样**。
//
// ⚠️ 别在这个包另排一份顺序：界面按 `homeTabs` 的下标取歌，两份顺序一旦
// 分叉，「流行」那一档画出来的是「音乐」的推荐，而界面上一个错都没有。
const (
	// HomeTabMusic 是「音乐」那一档：音乐分区推荐。
	HomeTabMusic = 0
	// HomeTabGuichu 是「鬼畜」那一档：鬼畜分区推荐。
	HomeTabGuichu = 1
	// HomeTabPop 是「流行」那一档：音乐综合榜（**不是第三个分区**，见
	// `service/bili/recommend.go` 的文件头）。
	HomeTabPop = 2
)

// HomeRegionOf 是 Tab 对应的分区号。**「流行」那一档没有分区**——它走榜。
//
// 认不出来的下标一律当音乐（0），而不是报一个错：这一栏在界面上是按
// `homeTabs` 的下标取的，认不出来说明界面那一份已经错了，而这一层再喊
// 一嗓子只是让那个错更吵。
func HomeRegionOf(tab int) int {
	if tab == HomeTabGuichu {
		return bili.RegionGuichu
	}
	return bili.RegionMusic
}

// Feed 拉第 tab 个 Tab 的第一页推荐。
//
// ⚠️ **不重排**（理由在 `service/bili/recommend.go` 的文件头）：分区推荐是
// 个性化的流，B 站给的顺序就是内容；音乐榜的名次就在顺序里。
//
// Page 是页码。首页只拉第一页（上游那一页的懒加载是滚到底再拉下一段，
// 那是 `list.PagedList` 的活，见 `internal/ui/homepage_feed.go` 的注释）。
func (r *Recommend) Feed(ctx context.Context, tab, page int) ([]RecommendItem, int, error) {
	if !r.Mounted() {
		return nil, 0, errRecommendMissing
	}
	if tab == HomeTabPop {
		items, err := r.Client.MusicRank(ctx, bili.MusicRankQuery{Page: page})
		if err != nil {
			return nil, 0, err
		}
		out, dropped := recommendItems(items)
		return out, dropped, nil
	}
	items, err := r.Client.RegionFeed(ctx, bili.RegionFeedQuery{
		Page: page, Region: HomeRegionOf(tab),
	})
	if err != nil {
		return nil, 0, err
	}
	out, dropped := recommendItems(items)
	return out, dropped, nil
}

// NewMusic 拉新歌速递那一摊：头图位 + 列表，两半**并发**发出去。
//
// 合成规则照上游（`new-music-top.tsx:58-104`）：**头图位在前、列表在后**，
// 按身份去重。串行发的话最坏要等两个往返，而界面上是「正在加载」转两个圈
// 才画出来——并发的代价只是多一个在途请求，这两个都是只读的。
//
// ⚠️ **一失败不算整摊失败**：上游两个请求各自 catch，一半拿到了就画一半。
// 这里照它——一半新歌比没有新歌好，而且那一半照样能点。
func (r *Recommend) NewMusic(ctx context.Context) ([]RecommendItem, int, error) {
	if !r.Mounted() {
		return nil, 0, errRecommendMissing
	}
	type half struct {
		items []bili.RecommendItem
		err   error
	}
	banner := make(chan half, 1)
	list := make(chan half, 1)
	go func() {
		it, err := r.Client.NewMusicBanner(ctx)
		banner <- half{it, err}
	}()
	go func() {
		it, err := r.Client.NewMusic(ctx)
		list <- half{it, err}
	}()
	b, l := <-banner, <-list

	switch {
	case b.err != nil && l.err != nil:
		return nil, 0, fmt.Errorf("新歌速递：%w", l.err)
	case b.err != nil:
		items, dropped := recommendItems(l.items)
		return items, dropped, nil
	case l.err != nil:
		items, dropped := recommendItems(b.items)
		return items, dropped, nil
	}
	joined := make([]bili.RecommendItem, 0, len(b.items)+len(l.items))
	joined = append(joined, b.items...)
	joined = append(joined, l.items...)
	items, dropped := recommendItems(joined)
	return items, dropped, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 挂到 App 上
// ─────────────────────────────────────────────────────────────────────────────

// mountedRecommend 是每个 App 挂着的这一摊。
var mountedRecommend sync.Map // *App → *Recommend

// MountRecommend 把首页要的那几个接口挂到 App 上。**在开窗口之前调**。
func (a *App) MountRecommend(cli *bili.Client) {
	mountedRecommend.Store(a, &Recommend{Client: cli})
}

// RecommendOf 是这个 App 的首页这一摊。还没挂过就现建一个空的：取数会回
// errRecommendMissing，界面据此说清「没接上」。
func (a *App) RecommendOf() *Recommend {
	if v, ok := mountedRecommend.Load(a); ok {
		return v.(*Recommend)
	}
	r := &Recommend{}
	mountedRecommend.Store(a, r)
	return r
}
