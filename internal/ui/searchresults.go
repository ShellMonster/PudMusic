// 搜索页那三个 Tab 的状态、拉页，以及 B 站结果到列表行的映射。画法在 searchpage.go；
// 这一层全是纯逻辑（拉页、映射、翻页重置），能单独测，view 函数要靠 Tester 才验得动。
//
// # 为什么是三个 Tab（上游只有「视频 / 用户」）
//
// 多出来的「歌曲」是上游那个**默认开着**的「仅音乐分区」开关提上来的：它发的还是同一
// 个 `search_type=video`，只是多了 `tids=3`（`video-list/index.tsx:24` 与 `:40`），
// 也就是用户在上游看到的默认结果本来就是音乐分区。开关和 Tab 说的是同一件事，留两个
// 入口只会让人以为它们不一样。每个 Tab 各有一份分页列表，让「切 Tab 不重置滚动位置」
// 能真的成立（见 searchTab.current）。

package ui

import (
	"context"
	"errors"
	"log"
	"strconv"
	"sync"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/egoist/mygo"
)

// 三个 Tab。顺序就是侧栏／页面上从左到右的顺序。
const (
	// searchTabSong 是「歌曲」：只搜音乐分区（`tids=3`）。
	searchTabSong = iota
	// searchTabVideo 是「视频」：全部分区。
	searchTabVideo
	// searchTabUser 是「用户」。
	searchTabUser
	// searchTabCount 是 Tab 的个数。加 Tab 要改这一处。
	searchTabCount = 3
)

// searchTabNames 是三个 Tab 上的字。
var searchTabNames = [searchTabCount]string{"歌曲", "视频", "用户"}

// videoSortOption 是视频 / 歌曲那一档的排序。字面照抄上游的 `sortOptions`
// （`pages/search/video-list/search-header.tsx:21-27`），顺序也照抄——
// 用户在 Biu 里记熟的是这个次序。
type videoSortOption struct {
	Label string
	Order bili.VideoOrder
}

// searchVideoSorts 是歌曲 / 视频 Tab 的排序。第一档是默认（Biu 的
// `useState("totalrank")`）。
var searchVideoSorts = []videoSortOption{
	{"综合排序", bili.VideoOrderTotalRank},
	{"最多播放", bili.VideoOrderClick},
	{"最新发布", bili.VideoOrderPubdate},
	{"最多弹幕", bili.VideoOrderDanmaku},
	{"最多收藏", bili.VideoOrderStow},
}

// userSortOption 是用户那一档的排序。字面照抄上游
// （`pages/search/user-list/search-header.tsx:12-18`，值在 `utils.ts` 的
// `getSortParams`）。
type userSortOption struct {
	Label string
	Sort  bili.UserSort
}

// searchUserSorts 是用户 Tab 的排序。第一档是默认（Biu 的
// `useState("default")`）。
var searchUserSorts = []userSortOption{
	{"默认排序", bili.UserSortDefault},
	{"粉丝数由高到低", bili.UserSortFansDesc},
	{"粉丝数由低到高", bili.UserSortFansAsc},
	{"Lv等级由高到低", bili.UserSortLevelDesc},
	{"Lv等级由低到高", bili.UserSortLevelAsc},
}

// searchUserIDPrefix 是用户行的身份前缀。mid 本身唯一且稳定，加前缀是为了让
// 「这一行是用户」在 ID 上就看得出来（将来要把三种结果合进同一个列表时有一半行
// 裸着 mid 会分不清）。一首歌的 ID 是 bvid（`BV` 开头），撞不上。
const searchUserIDPrefix = "u:"

// searchUserItemType 是用户行那一项在 `app.Item.Type` 上的值。
//
// 用户不是歌，`app.Item` 里没有它的位置；而菜单那一层的协议只把 `app.Item` 递给回调
// （`widgets/list/menu.go` 的 `doAction`），所以「用户刚按的是哪一个」得能从那一项本身
// 答得出来——`Type` 说清它是什么，`Bvid` 那一格装 mid。
//
// ⚠️ **用户行永远不进播放队列**（`Play` 是跳路由，`Add` 是一句解释），所以这个 Type
// 不会流到判重标识（`queue.go` 的 `streamKey`）里去。
const searchUserItemType = app.ItemTypeUser

// errSearchNotMounted 是「还没挂上 B 站客户端」。
//
// 只在没走过 `MountLogin` 时出现（生产上 `main.go` 一定先挂），测试里不挂就是
// 这一条。
//
// ⚠️ **这句是给用户看的，不许说「客户端」「接口」**（票 #242，UX 报告 §3-F 的
// F2）：用户不知道应用内部有没有挂上什么客户端，他只知道「搜不出来」。说成
// 「网络不通」也不对——那会让用户去查自己的网，而实际上是他这个应用少了一块。
var errSearchNotMounted = errors.New("搜索暂时用不了")

// searchState 是搜索页记得的全部东西，一个窗口一份。
//
// ⚠️ 挂在 Shell 上而不是建成局部变量：即时模式每一帧都重画一遍，局部变量下一帧
// 就没了。三个 Tab 的滚动位置、已加载的行、代次全靠它跨帧活着。
type searchState struct {
	// tab 是当前选中的 Tab（`ui.Tabs` 直接写它）。
	tab int

	// sorts 是每个 Tab 各自的排序下标。
	//
	// ⚠️ **一个 Tab 一格**，不是一个全局值：Biu 的两个列表各有一份排序状态
	// （`video-list` 一个 `order`、`user-list` 一个 `sortKey`），在「用户」里
	// 按过「Lv等级由高到低」再切回「视频」，那边的排序该还在原处。
	sorts [searchTabCount]int

	// lists 是每个 Tab 那一份分页列表。
	lists [searchTabCount]searchTab

	// fetch 拉一页。**是一个字段而不是写死的方法**：分页列表在 NewPaged 那一刻就把
	// 拉页器拿走了，做成字段之后换的是它背后那一个，闭包每次调都重新读，替换对
	// 已经建好的列表也管用（测试要换成不碰网络的假拉页器）。
	//
	// ⚠️ 它跑在后台 goroutine 里，读的每一样共享状态都得自己护住（见 searchTab.mu）。
	fetch func(tab int, ctx context.Context, page int) (list.Page, error)

	// client 是拉页要用的那一个 B 站客户端，默认是 App 上那一个（`App.BiliClient`）。
	//
	// 做成字段是为了**能在不挂登录的情况下测真的那条拉页路**（`App.BiliClient` 只有
	// 走过 `MountLogin` 之后才有值，而挂登录要碰凭据后端）。同一个理由跟 `fetch`
	// 一样：这一层的依赖是显式的，测试换掉的是它背后那一个。
	client func() *bili.Client
}

// searchTab 是一个 Tab 的分页列表，外加「现在画着的这批结果是谁的」。
type searchTab struct {
	// list 是这一 Tab 的分页列表：翻页、防重入、取消、容器没填满自动续拉、
	// 回顶按钮，全是它的事（`widgets/list/paged.go`）。
	list *list.PagedList

	// kw 是「现在这些结果对应的是哪一条查询」。**只由界面线程读写。**
	kw string

	// mu 护着下面两样：拉页在后台 goroutine 里，界面线程同时在画、在点「全部播放」，
	// 不加锁就是竞态（`go test -race` 会当场报出来）。
	mu sync.Mutex
	// gen 是这一 Tab 的代次，每次换查询 +1（见 current）。
	//
	// 🔴 分页列表自己也有一个代次，但**它管不到下面那个 loaded**：那是这里另抄的一份
	// 「已经拿到手的行」，收结果的那一趟不经过分页列表的 `apply`，所以它的代次检查
	// （`PagedList.apply` 里那句 `r.gen != p.gen`）盖不住它。没有这一道的话：换完关键词
	// 之后**旧的**那一页迟到，用户在列表上已经看不见它了，按「全部播放」却把它一起
	// 加进队列——用户搜的是新词，队列里混着旧词的歌（评审复现：先完成新响应再释放旧
	// 响应，`loaded` 成了 [BVnew, BVold]）。
	gen uint64
	// query 是交给后台那次拉页用的查询。写它的是界面线程（换查询时），读它的
	// 是后台 goroutine。
	query searchQuerySpec
	// loaded 是**已经拿到手的全部行**——「全部播放」要的就是它（不是屏幕上那十几行，
	// 是用户一路翻到现在看过的全部）。
	//
	// ⚠️ 为什么不问分页列表要：它只报 `Paged().Loaded`，不给行本身，而这一层
	// **只读不改**那个包。
	loaded []list.Music
	// total 是这一场搜索命中的**总数**（B 站给的 `numResults`）。
	//
	// ⚠️ **它不是「现在拿到手几行」**：翻页会一直加行，而这一场一共就这么多。
	// 它是给「说给用户听」用的（无障碍，见 `announce.go`）：读屏软件用户看不见
	// 屏幕上那十几行，得有一句「找到 N 个结果」，而那个 N 拿「屏幕上那几行」冒充
	// 就是说了一个假数。
	//
	// 零 = 不知道（B 站没给，或者这一趟还没回来）。
	total int
	// announced 是「这一场搜索的结果已经念过了没有」（无障碍）。
	//
	// ⚠️ **按查询**的：换查询时清零（见 `current`）。
	//
	// ⚠️ **只由界面线程读写**（和 `tab` / `sorts` 一样）：念不念是界面这一侧
	// 的决定，而后台那条线管不着。
	announced bool
}

// searchQuerySpec 是一次查询的两样东西：词和排序。
type searchQuerySpec struct {
	keyword string
	sort    int
}

// init 把三个 Tab 建起来。Shell.New 调它。
func (st *searchState) init(s *Shell) {
	st.fetch, st.client = s.fetchSearchTab, s.app.BiliClient
	for i := range st.lists {
		st.lists[i].list = list.NewPaged(s.images, s.searchActions(i), s.searchFetcher(i))
		// ⚠️ **「正在加载」那一格由外面递进去**（票 #138）：分页列表自己那一句是
		// 13px 淡灰缩在列表左上角，而这一页的「没有结果」是居中 15pt 两行——
		// 等的那几秒里那行字和回来之后那一格对不上。
		st.lists[i].list.SetEmptyView(loadingCell("正在加载…"))
		// 播放量 / 投稿时间：歌曲和视频两档有，用户那一档两样都没有（见
		// `searchColumns`）。**装在这里而不是每一帧**：一档 Tab 的列不会变。
		st.lists[i].list.SetColumns(searchColumns(i, s))
	}
}

// searchFetcher 交给分页列表：每次都转手问 `searchState.fetch`，所以测试换掉
// `st.fetch` 之后**已经建好的**列表也跟着换。
func (s *Shell) searchFetcher(tab int) list.Fetcher {
	return func(ctx context.Context, page int) (list.Page, error) {
		return s.search.fetch(tab, ctx, page)
	}
}

// fetchSearchTab 是真的那一份拉页器（`searchState.fetch` 的默认值）。
func (s *Shell) fetchSearchTab(tab int, ctx context.Context, page int) (list.Page, error) {
	t := &s.search.lists[tab]
	t.mu.Lock()
	q, gen := t.query, t.gen
	t.mu.Unlock()

	st := &s.search
	cli := st.client()
	if cli == nil {
		return list.Page{}, errSearchNotMounted
	}

	var (
		songs   []list.Music
		hasMore bool
		total   int
		err     error
	)
	if tab == searchTabUser {
		ask := searchUserQueryOf(q)
		ask.Page = page
		var pg bili.UserPage
		pg, err = cli.SearchUsers(ctx, ask)
		songs, hasMore, total = searchUserMusics(pg.Hits), pg.HasMore, pg.Total
	} else {
		ask := searchVideoQueryOf(tab, q)
		ask.Page = page
		var pg bili.VideoPage
		pg, err = cli.SearchVideos(ctx, ask)
		songs, hasMore, total = searchVideoMusics(pg.Hits), pg.HasMore, pg.Total
	}
	if err != nil {
		// ⚠️ **说人话**：界面上原样摆的就是这一句（失败那一格画的是 `err.Error()`，
		// 那条 Toast 也是），而 B 站客户端的错是「bili: 网络没通: GET /x/y: dial
		// tcp …」这种给开发者看的东西。`bili.Friendly` 把它翻成「连不上 B 站，
		// 检查一下网络」——首页那一档也是这么办的（`homepage_feed.go` 的
		// `homeFailureOf`）。
		//
		// 🔴 **原始的那一句要进日志**：翻过去的那一句有时候是「出了点问题（详情看
		// 日志）」——那句话指的日志里得有这件事，而分页列表那边记的是**翻过之后**
		// 的那一句（`widgets/list/paged.go` 的 `apply`）。所以原始的那一份在这里
		// 记一份。
		log.Printf("biu: 搜索拉页失败（%s，第 %d 页）：%v", searchTabNames[tab], page, err)
		return list.Page{}, searchFailureOf(err)
	}

	// 留一份给「全部播放」。去重在按下按钮那一刻做（见 searchPlayAll）。
	//
	// 🔴 **代次对不上就整个丢掉**：换查询之后旧的那一页还会回来（cancel 只停请求，
	// 已经回来的那一趟照样给结果），它已经不该出现在任何地方了——分页列表会靠自己的
	// 代次把它作废，而这一份是绕过分页列表收进来的。
	t.mu.Lock()
	if gen == t.gen {
		// B 站翻页会重叠，同一个 bvid 落进两页里是常事，原样收着、按下去那一刻再去重。
		t.loaded = append(t.loaded, songs...)
		t.total = total
	}
	t.mu.Unlock()

	return list.Page{Songs: songs, HasMore: hasMore}, nil
}

// searchFailureOf 把 B 站客户端那一句错翻成一句**用户看得懂**的话。
//
// ⚠️ **这一句会原样出现在界面上**：搜索页的失败那一格画的是 `err.Error()`，而
// 那条 Toast 也是。`bili.Friendly` 负责翻（-101 → 「登录已经过期了，重新扫码
// 登录一下」、`ErrNetwork` → 「连不上 B 站，检查一下网络」），而它也**绝不把
// 地址原样打出去**（取流地址带签名，出现在界面上等于把那条流公开了）。
//
// ⚠️ **空的那一句有兜底**：`bili.Friendly` 对「用户自己取消了」返回空串（那一趟
// 不该在界面上留话），而这儿是**一次真的失败**——空着的话界面上就只剩一个图标
// 和一个重试按钮，一句原因都没有。
func searchFailureOf(err error) error {
	if err == nil {
		return nil
	}
	msg := bili.Friendly(err)
	if msg == "" {
		msg = "拉不到这一场搜索，等一会儿再试"
	}
	return errors.New(msg)
}

// searchVideoQueryOf 把「歌曲 / 视频」那一档的查询变成一次 B 站请求。
//
// 🔴 「歌曲」与「视频」的差别**只有 MusicOnly 这一个字段**（见文件头）。它单独拎成
// 一个纯函数，是因为这一条正是「三个 Tab 各搜各的」的全部要害，而它在真请求里
// （网络、WBI 签名）没法一眼验出来。
func searchVideoQueryOf(tab int, q searchQuerySpec) bili.VideoQuery {
	return bili.VideoQuery{
		Keyword: q.keyword,
		// 🔴 「歌曲」Tab 限定音乐分区，「视频」Tab 全都要。写反了两个 Tab 会
		// 搜出完全一样的东西，而界面上看不出任何异样。
		MusicOnly: tab == searchTabSong,
		Order:     searchVideoSorts[sortOf(tab, q.sort)].Order,
	}
}

// searchUserQueryOf 把用户那一档的查询变成一次 B 站请求。
func searchUserQueryOf(q searchQuerySpec) bili.UserQuery {
	return bili.UserQuery{
		Keyword: q.keyword,
		Sort:    searchUserSorts[sortOf(searchTabUser, q.sort)].Sort,
	}
}

// sortOf 把排序下标夹进这一 Tab 的范围。正常情况下它总在范围内（`ui.Tabs` 只给
// 那几个标签写值），夹一道是因为这里读的是**后台 goroutine**：越界会是一个看不懂
// 的崩溃，而不是一句「这一档排序不存在」。
func sortOf(tab, i int) int {
	n := searchTabSortCount(tab)
	if i < 0 || i >= n {
		return 0
	}
	return i
}

// searchTabSortCount 是 tab 那一档有多少个排序。
func searchTabSortCount(tab int) int {
	if tab == searchTabUser {
		return len(searchUserSorts)
	}
	return len(searchVideoSorts)
}

// searchSpecKey 是「现在这些结果对应的是哪一条查询」那个键。
//
// ⚠️ **里面没有 Tab**：放进去的话切 Tab 就成了「查询变了」，每次切都重新拉一遍并且
// 回到顶上，「切 Tab 不重置滚动位置」就废了（searchTab.current）。
func searchSpecKey(q searchQuerySpec) string {
	return q.keyword + "\x00" + strconv.Itoa(q.sort)
}

// current 是「拿当前这一 Tab 的状态，顺手判断要不要重头再来」。
//
// 🔴 **任一筛选条件变化 → 自动重置到第 1 页**（验收第 2 条）：判据是「画着的这些
// 结果已经不是你此刻要的那一批了」——关键词变了或者排序变了。做法是调
// `PagedList.Reload`，它停掉在途请求、清空已加载的行、回顶、从第 1 页重来（见
// `widgets/list/paged.go`）。上游是 `useEffect` 依赖 `[keyword, musicOnly, order]`。
//
// ⚠️ **换查询那一帧，这一层会顺手多拉一页**（已知）：`Reload` 先把行清空，而分页
// 列表的「容器没填满自动续拉」是拿**上一帧**量到的可见行判断的（`paged.go` 的
// `afterFrame` 读 `ListState.Visible`，MyGo 的注释写着「报的是上一帧画出来的行」）。
// 用户刚滚到底部时那一份是「40~48 行」，于是它以为「末尾已经画在屏幕上了」又拉一
// 页。有没有拉到取决于那一帧的布局时序，**时好时坏**——所以别拿「一共加载了几行」
// 当这一条的判据。根因在 `widgets/list/paged.go`，本票对那个包只读不改；真要修是
// 在 `afterFrame` 里加一道「刚刚换过查询」的闸门，和它已有的 `blocked` 同一类。
//
// 只有界面线程能调。
func (t *searchTab) current(q searchQuerySpec) {
	key := searchSpecKey(q)
	if t.kw == key {
		return
	}
	t.kw = key
	t.mu.Lock()
	t.gen++ // 作废旧查询在途的那几趟，见上面 gen 的注释
	t.query, t.loaded = q, nil
	t.total = 0
	// 换查询 = 换一场搜索，而「这一场已经念过了」是**按场**记的（见
	// `announced` 的注释）。不清零的话，新一场搜完了也不会念。
	t.announced = false
	t.mu.Unlock()
	t.list.Reload()
}

// searchTotal 是这一场搜索命中的总数。零 = 还不知道。
func (t *searchTab) searchTotal() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.total
}

// snapshot 是已经拿到的全部行，一份拷贝。「全部播放」按的正是它。
func (t *searchTab) snapshot() []list.Music {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]list.Music(nil), t.loaded...)
}

// searchVideoMusic 把一条视频结果变成一行。
//
// 🔴 **身份是 bvid**，不是序号：一个 bvid 在 B 站上只对应一个稿件，而翻页会
// 把同一个 bvid 送到两页里（见 paged.go 的去重）。拿序号当身份的话，前一页多
// 出来的行会让后面所有行的选中状态整体错配（铁律四）。
func searchVideoMusic(hit bili.VideoHit) list.Music {
	return list.Music{
		ID:       hit.Bvid,
		Title:    hit.Title,
		Cover:    hit.Cover,
		Owner:    hit.Owner,
		Duration: hit.Duration,
		// 播放量：列表那一列和网格卡片封面底部那一条渐变上画的都是它
		//（`Music.View`）。
		View: hit.Play,
		// 投稿时间：搜索页那一列画的是接口的 `pubdate`（见 `Music.PubTime`）。
		PubTime: hit.PubDate,
		// ⚠️ Cid 大多数情况是空的（B 站的搜索结果不给分 P id，见
		// `service/bili/search.go` 文件头）。空着的时候在线播放会按「缺稿件 id
		// 或分 P id」拒掉并说清为什么，那条限制不许为了「搜出来的歌能放」而放松。
		Item: app.Item{
			Source: "online",
			Type:   "mv",
			Bvid:   hit.Bvid,
			Cid:    hit.Cid,
			Title:  hit.Title,
			Cover:  hit.Cover,
			// UP 名 + 编号：播放条上那一句要显示它、点它进他的空间
			//（`app.Item.Artist`）。`VideoHit` 两样都带，往下传就是了。
			Artist:    hit.Owner,
			ArtistMid: hit.Mid,
		},
	}
}

func searchVideoMusics(hits []bili.VideoHit) []list.Music {
	if len(hits) == 0 {
		return nil
	}
	out := make([]list.Music, len(hits))
	for i, h := range hits {
		out[i] = searchVideoMusic(h)
	}
	return out
}

// searchUserMusic 把一条用户结果变成一行。
//
// 上游的用户是一张 240 高的大卡片（`user-list/index.tsx:127` 的
// `rowHeight={240}`，竖着摆头像、等级、粉丝数、关注按钮）。这里摆的是列表的
// 一行：序号 + 头像 + 昵称 + 个性签名。头像那一格还是 48 见方
// （`widgets/list/card.go`），所以看起来比上游那张卡片小得多——省掉的是关注
// 按钮（要一个还没接的 B 站写接口）和竖着摆版，用户按 UP 名的意图是一样的。
func searchUserMusic(hit bili.UserHit) list.Music {
	return list.Music{
		ID:    searchUserIDPrefix + strconv.FormatInt(hit.Mid, 10),
		Title: hit.Name,
		Cover: hit.Avatar,
		// 个性签名摆在 UP 名那一列（Biu 的音乐卡片那里放的就是 UP 名）。
		Owner: hit.Sign,
		// ⚠️ **没有 View，而且本来也不会有**：用户不是视频，没有播放量（播放量在
		// `VideoHit.Play` 上，见上面那个 `searchVideoMusic`）。所以网格卡片封面
		// 底部那一条渐变上**左半边不画**（理由见 `list.Music.View`）。
		Item: app.Item{
			Source: "online",
			Type:   searchUserItemType,
			// 装的是 mid，理由见 searchUserItemType 的注释。
			Bvid:  strconv.FormatInt(hit.Mid, 10),
			Title: hit.Name,
			Cover: hit.Avatar,
		},
	}
}

func searchUserMusics(hits []bili.UserHit) []list.Music {
	if len(hits) == 0 {
		return nil
	}
	out := make([]list.Music, len(hits))
	for i, h := range hits {
		out[i] = searchUserMusic(h)
	}
	return out
}

// searchUserMid 认出「这一项是用户，mid 是多少」。
func searchUserMid(item app.Item) (string, bool) {
	if item.Type != searchUserItemType || item.Bvid == "" {
		return "", false
	}
	return item.Bvid, true
}

// searchUserSpaceURL 是这个 UP 主在 B 站上的空间地址。
func searchUserSpaceURL(mid string) string {
	return "https://space.bilibili.com/" + mid
}

// searchActions 是这一 Tab 的列表会做的操作。
//
// 歌曲 / 视频就是 `s.listActions()`（右键、双击、加队列、收藏、在 B 站打开、
// 下载）；用户那一 Tab 换一份——用户不是歌，「播放」是进他的空间，「加进队列」
// 照实说一句为什么不行。
//
// ⚠️ **歌曲 / 视频这一份的「下载」是接上了的，而结果多半是「下不了」**：
// B 站的搜索接口只给 bvid、不给 cid（`bili.SearchVideo`），而下载按分 P 存
// 文件。所以那一项按下去会得到一句「还没取到分 P id」——**这是如实的，不是
// 没接**。别为了「让它看起来能用」把 cid 偷偷补上：搜索结果里的条目是**搜出来的
// 片段**，用户按「下载音频」时想下的是他搜到的那一支，而补出来的是那支稿件的
// 第一集（多 P 的第三集也是第一集）。
func (s *Shell) searchActions(tab int) list.Actions {
	if tab == searchTabUser {
		return s.searchUserActions()
	}
	return s.listActions()
}

// searchUserActions 是用户 Tab 的那一份操作。
func (s *Shell) searchUserActions() list.Actions {
	acts := list.Actions{
		// 双击 / 菜单「播放」= 进他的 UP 主空间（Biu 的
		// `user-card.tsx:51`：`onPress={() => navigate(`/user/${u.mid}`)}`）。
		Play: s.openSearchUser,
		// 「立即播放」照实说一句为什么不**能用**：菜单是列表和网格共用的一份
		// （`widgets/list` 的 menuItems），这一项在用户 Tab 照样摆着。给 nil
		// 的话它会落到那句「现在还不能用」——而它其实早接上了（UP 主确实不是歌，
		// 和下面几项是同一个理由）。
		PlayNow: func(app.Item) {
			s.addToast("UP 主不是歌，没有东西能立即播放；进他的空间听他的歌", notice.Warning)
		},
		Add: func([]app.Item) {
			s.addToast("UP 主不是歌，加不进队列；进他的空间听他的歌", notice.Warning)
		},
		// 「下一首播放」照实说一句为什么不**能用**：菜单是列表和网格共用的一份
		// （`widgets/list` 的 menuItems），这一项在用户 Tab 照样摆着。给 nil
		// 的话它会落到那句「现在还不能用」——而它其实早接上了（UP 主确实不是歌，
		// 和上面两项是同一个理由）。
		PlayNext: func(app.Item) {
			s.addToast("UP 主不是歌，没有东西可排；进他的空间听他的歌", notice.Warning)
		},
		Open: func(item app.Item) {
			mid, ok := searchUserMid(item)
			if !ok {
				s.addToast("这一项不是一个 UP 主", notice.Warning)
				return
			}
			if err := mygo.Shell.OpenExternal(searchUserSpaceURL(mid)); err != nil {
				s.addToast("打不开："+err.Error(), notice.Danger)
			}
		},
		// ⚠️ 收藏 / 下载这两项**给的不是 nil**：给 nil 的话 `doAction` 会说
		// 「现在还不能用」——那是一句假话（接口早就在了），而用户会以为这
		// 个应用还没做完。UP 主不是歌，这两句是真的。
		FavSelect: func(app.Item) {
			s.addToast("UP 主不是歌，没有东西可收藏；进他的空间收藏他的歌", notice.Warning)
		},
		Download: func(app.Item) {
			s.addToast("UP 主不是视频，没有东西可下载", notice.Warning)
		},
		Notify: s.addToast,
	}
	// 焦点交接（票 #245 的 E4）：这一份是**另写**的（用户不是歌），所以焦点那
	// 两项也得自己装——漏了的话用户 Tab 的列表就进不去焦点。
	acts.Focus = s.registerContentFocus
	acts.FocusOut = s.focusSide
	return acts
}

// openSearchUser 跳到某个 UP 主的空间。
//
// items 是从那一行往后的一整段（`playFrom` 的协议，见 `widgets/list/shared.go`），
// 所以取**第一个**——那才是用户按下去的那一行。
func (s *Shell) openSearchUser(items []app.Item) {
	if len(items) == 0 {
		return
	}
	mid, ok := searchUserMid(items[0])
	if !ok {
		return
	}
	s.navigate(userPathPrefix + mid)
}

// searchPlayAll 是「全部播放」。
//
// ⚠️ **它和首页那一枚走同一个出口**（`playAllSongs`，票 #244 / UX 报告 §2 的
// B6）：同一个动作在两个页面上各有一枚入口，而此前**只有搜索页有**——用户在
// 一个页面能按、在另一个页面找不到，会以为是两个不同的功能。
//
// 票 #337（2026-10-24）：换成已经加载的这一批并从第一首播。
// 上游 v1.16 搜索页曾走 addList，本项目在 playAllSongs 中统一成 PlayFrom。
func (s *Shell) searchPlayAll(t *searchTab) {
	s.playAllSongs(t.snapshot())
}
