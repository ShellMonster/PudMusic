// 搜索页那三个 Tab 的状态、拉页，以及 B 站结果到列表行的映射。
//
// 画法在 searchpage.go，这一层只有「记着什么、怎么拉」。两边分开是因为这一层
// 全是纯逻辑（拉页、映射、翻页重置），能单独测；而 searchpage.go 里那些 view
// 函数要靠 MyGo 的 Tester 才验得动。
//
// # 三个 Tab，三个分页列表
//
// 上游搜索页只有「视频 / 用户」两个 Tab（`pages/search/search-type.tsx`）。
// 本项目多一个「歌曲」，来源是上游那个**默认开着**的「仅音乐分区」开关：
// 上游发的是同一个 `search_type=video` 的请求加上 `tids=3`
// （`pages/search/video-list/index.tsx:24` 与 `:40`），也就是说用户在上游看到的
// 默认结果本来就是音乐分区。把这个默认视图提成第一个 Tab 之后，开关就不必
// 再存在了——开关和 Tab 说的是同一件事，留两个入口只会让人以为它们不一样。
//
// 每个 Tab 各有一份分页列表（`list.PagedList`），**不是一份数据三种摆法**。
// 这么定是为了「切 Tab 不重置滚动位置」这一条能真的成立（见 searchTab.current
// 的注释）。

package ui

import (
	"context"
	"errors"
	"strconv"
	"sync"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
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

// searchUserIDPrefix 是用户行的身份前缀。
//
// 🔴 行身份必须是**稳定且唯一**的（铁律四）。mid 自己是唯一且稳定的，加前缀是
// 为了让「这一行是用户」这件事在 ID 上就看得出来——将来某一处要用正则从 ID
// 里分辨类型（或者要把三种结果合进同一个列表），有一半行裸着 mid 就会分不清。
// 一首歌的 ID 是 bvid（`BV` 开头），撞不上。
const searchUserIDPrefix = "u:"

// searchUserItemType 是用户行那一项在 `app.Item.Type` 上的值。
//
// 用户不是歌，`app.Item` 里没有它的位置。菜单那一层的协议只把 `app.Item`
// 递给回调（`widgets/list/menu.go` 的 `doAction`），所以「用户刚按的是哪一个」
// 得能从那一项本身答得出来——`Type` 说清它是什么，`Bvid` 那一格装 mid。
//
// ⚠️ **用户行永远不进播放队列**：它的 `Play` 是跳路由，`Add` 是说一句为什么
// 不能加（见 searchUserActions）。所以这个 Type 不会流到 `app.Item` 的判重
// 标识（`queue.go` 的 `streamKey`）里去，也就不存在「同一个人被当成两首」的问题。
const searchUserItemType = "user"

// errSearchNotMounted 是「还没挂上 B 站客户端」。
//
// 只在没走过 `MountLogin` 时出现（生产上 `main.go` 一定先挂），测试里不挂就是
// 这一条。说清楚是「没接上」而不是「网络不通」，两种要办的事完全不一样。
var errSearchNotMounted = errors.New("搜索还没接上 B 站客户端")

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

	// fetch 拉一页。**是一个字段而不是写死的方法**：测试要换成不碰网络的假
	// 拉页器，而分页列表在 NewPaged 那一刻就把拉页器拿走了——做成字段之后
	// 换的是它背后那一个，闭包每次调都重新读，替换对已经建好的列表也管用。
	//
	// ⚠️ 它跑在后台 goroutine 里，读的每一样共享状态都得自己护住
	// （见 searchTab.mu）。
	fetch func(tab int, ctx context.Context, page int) (list.Page, error)
}

// searchTab 是一个 Tab 的分页列表，外加「现在画着的这批结果是谁的」。
type searchTab struct {
	// list 是这一 Tab 的分页列表：翻页、防重入、取消、容器没填满自动续拉、
	// 回顶按钮，全是它的事（`widgets/list/paged.go`）。
	list *list.PagedList

	// kw 是「现在这些结果对应的是哪一条查询」。**只由界面线程读写。**
	kw string

	// mu 护着下面两样。拉页在后台 goroutine 里，界面线程同时在画、在点
	// 「全部播放」，不加锁就是竞态（`go test -race` 会当场报出来）。
	mu sync.Mutex
	// query 是交给后台那次拉页用的查询。写它的是界面线程（换查询时），读它的
	// 是后台 goroutine。
	query searchQuerySpec
	// loaded 是**已经拿到手的全部行**，「全部播放」要的就是它（不是页面上的
	// 十几行，是用户一路翻到现在看过的全部）。
	//
	// ⚠️ 为什么不问分页列表要：它只报「加载了多少行」（`Paged().Loaded`），
	// 不给行本身，而这一层**只读不改**那个包。
	loaded []list.Music
}

// searchQuerySpec 是一次查询的两样东西：词和排序。
type searchQuerySpec struct {
	keyword string
	sort    int
}

// init 把三个 Tab 建起来。Shell.New 调它。
func (st *searchState) init(s *Shell) {
	st.fetch = s.fetchSearchTab
	for i := range st.lists {
		st.lists[i].list = list.NewPaged(s.images, s.searchActions(i), s.searchFetcher(i))
	}
}

// searchFetcher 是交给分页列表的那个拉页器：它每次都转手问 `searchState.fetch`，
// 所以测试把 `st.fetch` 换成假的那一份之后**已经建好的**列表也跟着换。
func (s *Shell) searchFetcher(tab int) list.Fetcher {
	return func(ctx context.Context, page int) (list.Page, error) {
		return s.search.fetch(tab, ctx, page)
	}
}

// fetchSearchTab 是真的那一份拉页器（`searchState.fetch` 的默认值）。
func (s *Shell) fetchSearchTab(tab int, ctx context.Context, page int) (list.Page, error) {
	t := &s.search.lists[tab]
	t.mu.Lock()
	q := t.query
	t.mu.Unlock()

	cli := s.app.BiliClient()
	if cli == nil {
		return list.Page{}, errSearchNotMounted
	}

	var (
		songs   []list.Music
		hasMore bool
		err     error
	)
	if tab == searchTabUser {
		ask := searchUserQueryOf(q)
		ask.Page = page
		var pg bili.UserPage
		pg, err = cli.SearchUsers(ctx, ask)
		songs, hasMore = searchUserMusics(pg.Hits), pg.HasMore
	} else {
		ask := searchVideoQueryOf(tab, q)
		ask.Page = page
		var pg bili.VideoPage
		pg, err = cli.SearchVideos(ctx, ask)
		songs, hasMore = searchVideoMusics(pg.Hits), pg.HasMore
	}
	if err != nil {
		return list.Page{}, err
	}

	// 留一份给「全部播放」。去重在按下按钮那一刻做（见 searchPlayAll），
	// 这里照原样收着：B 站翻页会重叠，两页同一首歌是常事。
	t.mu.Lock()
	t.loaded = append(t.loaded, songs...)
	t.mu.Unlock()

	return list.Page{Songs: songs, HasMore: hasMore}, nil
}

// searchVideoQueryOf 把「歌曲 / 视频」那一档的查询变成一次 B 站请求。
//
// 🔴 「歌曲」与「视频」的差别**只有 MusicOnly 这一个字段**（见文件头）。把它
// 单独拎成一个纯函数，是因为这一条正是「三个 Tab 各搜各的」的全部要害，而它在
// 真请求里（网络、WBI 签名）没法一眼验出来。
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

// sortOf 把排序下标夹进这一 Tab 的范围。
//
// 正常情况下它总在范围内（`ui.Tabs` 只给那几个标签写值），夹一道是因为这里
// 读的是**后台 goroutine**：万一哪一处算错，越界会是一个看不懂的崩溃，而不是
// 一句「这一档排序不存在」。
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
// ⚠️ **里面没有 Tab**：见 searchTab.current 的注释——放进去的话，切 Tab 就成了
// 「查询变了」，于是每次切都重新拉一遍并且回到顶上，那条「切 Tab 不重置滚动
// 位置」就废了。
func searchSpecKey(q searchQuerySpec) string {
	return q.keyword + "\x00" + strconv.Itoa(q.sort)
}

// current 是「拿当前这一 Tab 的状态，顺手判断要不要重头再来」。
//
// 🔴 **任一筛选条件变化 → 自动重置到第 1 页**（验收第 2 条）：判据是「画着的
// 这些结果已经不是你此刻要的那一批了」——关键词变了或者排序变了。做法是调
// `PagedList.Reload`，它停掉在途请求、清空已加载的行、回顶、从第 1 页重来
// （见 `widgets/list/paged.go`）。上游是 `useEffect` 依赖 `[keyword, musicOnly,
// order]`，同一件事。
//
// ⚠️ **换查询那一帧，这一层会顺手多拉一页**（已知，写在这儿免得以后有人以为是
// 哪写错了）：`Reload` 先把行清空，而分页列表的「容器没填满自动续拉」是拿
// **上一帧**量到的可见行来判断的（`paged.go` 的 `afterFrame` 读
// `ListState.Visible`，MyGo 的注释写着「报的是上一帧画出来的行」）。用户刚滚到
// 底部时那一份是「40~48 行」，于是换查询之后它会以为「末尾已经画在屏幕上了」
// 又拉一页。有没有拉到取决于那一帧的布局时序，**时好时坏**——所以别拿「一共
// 加载了几行」当这一条的判据，判据是「新排序发出去的第一趟是第 1 页」和
// 「画面回到了那一页的头」（见 searchpage_test.go 那两条用例）。
//
// 根因在 `widgets/list/paged.go` 那一层，本票对那个包只读不改。真要修是在
// `afterFrame` 里加一道「刚刚换过查询」的闸门，和它已有的 `blocked` 同一类。
//
// 只有界面线程能调。
func (t *searchTab) current(q searchQuerySpec) {
	key := searchSpecKey(q)
	if t.kw == key {
		return
	}
	t.kw = key
	t.mu.Lock()
	t.query, t.loaded = q, nil
	t.mu.Unlock()
	t.list.Reload()
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
// 歌曲 / 视频就是 `list.AppActions`（右键、双击、加队列、在 B 站打开）；
// 用户那一 Tab 换一份——用户不是歌，「播放」是进他的空间，「加进队列」照实
// 说一句为什么不行。
func (s *Shell) searchActions(tab int) list.Actions {
	if tab == searchTabUser {
		return s.searchUserActions()
	}
	return list.AppActions(s.app, s.addToast)
}

// searchUserActions 是用户 Tab 的那一份操作。
func (s *Shell) searchUserActions() list.Actions {
	return list.Actions{
		// 双击 / 菜单「播放」= 进他的 UP 主空间（Biu 的
		// `user-card.tsx:51`：`onPress={() => navigate(`/user/${u.mid}`)}`）。
		Play: s.openSearchUser,
		Add: func([]app.Item) {
			s.addToast("UP 主不是歌，加不进队列；进他的空间听他的歌")
		},
		Open: func(item app.Item) {
			mid, ok := searchUserMid(item)
			if !ok {
				s.addToast("这一项不是一个 UP 主")
				return
			}
			if err := mygo.Shell.OpenExternal(searchUserSpaceURL(mid)); err != nil {
				s.addToast("打不开：" + err.Error())
			}
		},
		Notify: s.addToast,
	}
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
// 🔴 **只追加，不替换**（验收第 1 条）。上游那一行的原话写在注释里：它把当前
// 已加载的列表全量映射之后调 `addList`，**不是 `playList`**
// （`pages/search/video-list/index.tsx:89-101`）。本项目里这就是
// `App.Enqueue`（往队尾追加）而不是 `App.PlayFrom`（换掉整个队列并从第一首
// 起播）。差别在界面上看得见：按了之后正在放的那首**不换**，新的一首排在它后面。
func (s *Shell) searchPlayAll(t *searchTab) {
	songs := t.snapshot()
	if len(songs) == 0 {
		return
	}

	// 去重：B 站翻页会重叠，同一个 bvid 落进 loaded 两次就是同一个条目进队列
	// 两次，用户按下一首会连着听到同一首。
	items := make([]app.Item, 0, len(songs))
	seen := make(map[string]bool, len(songs))
	for _, m := range songs {
		if seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		items = append(items, m.Item)
	}
	if len(items) == 0 {
		return
	}

	if err := s.app.Enqueue(items...); err != nil {
		s.addToast("加不进队列：" + err.Error())
		return
	}
	// 话照抄上游（`video-list/index.tsx:100`）。
	s.addToast("已添加 " + strconv.Itoa(len(items)) + " 首到播放列表")
}
