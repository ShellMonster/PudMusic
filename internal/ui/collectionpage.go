// 歌单内容页：Biu 的 `/collection/{id}`（收藏夹 / 合集 / 系列同一页）。
//
// # 这一页摆什么
//
//	头图区     封面、标题、简介、类型 + 条目数、创建者
//	操作条     播放全部 / 添加到播放列表 / 收藏星标 / ⋯ 菜单 / 排序下拉 / 多选开关
//	列表       `widgets/list` 的 PagedList（虚拟滚动 + 定高的行 + 行身份）
//	批量操作条 多选态才出现（`collectionbatch.go`）
//
// 结构照 Biu 的 `src/pages/video-collection/favorites/index.tsx` 的
// `<Header />` + `<Operations />` + 列表三段，文案也照抄那边（见各处注释）。
//
// 这个文件管**这一页长什么样**：拉什么、画在哪、什么排序。多选和批量在
// `collectionbatch.go`（那是「用户一次动了几首」，和摆法是两件事）。
//
// # 最要紧的一条：行身份
//
// 🔴 铁律四：这一页的行身份是**B 站那条内容的 id + 类型**（`mediaKey`），
// 不是序号。自动续拉往列表尾部追加（序号不动，序号够用），但**换排序 / 删除
// 之后整个列表重来**——序号在那种情况下整体错配，用户勾着的那一批会变成
// 另一批。
//
// # 这一页外面包着 Guard
//
// 页级那一道在 `router.go` 的 `Shell.page`（整页崩了换错误页）；
// 列表控件自己还有一道（`list.PagedList` 的 guard.Wrap）。两道不重复。
package ui

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/leading"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// collectionMatch 是这一页的路由模式，和 routes 表里登记的是同一个字符串。
//
// 拿来和 `Route.Match` 比，而不是拿 `routes[10]`：那条表一改顺序，这里跟着
// 错，而且错得很安静——歌单内容页退回占位页，没人看得出来（理由同
// `searchPath`）。
const collectionMatch = "/collection/{id}"

// collectionPathPrefix 是这一页的路径前缀，和 collectionMatch 对得上。
//
// 它同时是 `collectionPath`（侧栏点进去的那条）的前半截——两边必须一样，
// 所以定义放一处。
const collectionPathPrefix = "/collection/"

// collectionTypeQuery 是 URL 上写容器类型的那一个键。
//
// ⚠️ 默认值是「收藏夹」，和 Biu 的 `Number(type || CollectionType.Favorite)`
// 一致。侧栏点进来的收藏夹不带这个参数（见 `collectionPath`），所以默认那一
// 支是最常走的一支。
const collectionTypeQuery = "type"

// confirmKind 是「当前等着用户确认的是哪件事」。
type confirmKind uint8

const (
	// confirmNone 没有要确认的。
	confirmNone confirmKind = iota
	// confirmDelete 是批量删除。
	confirmDelete
	// confirmDownload 是批量下载。
	confirmDownload
	// confirmClearInvalid 是「清除失效内容」。
	confirmClearInvalid
	// confirmDeleteFolder 是「把 B 站上这个收藏夹删掉」。
	confirmDeleteFolder
	// confirmForgetFolder 是「只从本地记录里拿掉这一行」。
	confirmForgetFolder
)

// confirmRequest 是「要不要用户点一下确认」。
type confirmRequest struct {
	kind confirmKind
	open bool
	// count 是这件事会作用在多少条上。放在确认框的标题里——
	// 「确认删除选中的 3 首吗」比「确认删除吗」有用得多。
	count int
	// only 是「只作用在勾上的那些」（true）还是「当前已经加载出来的全部」
	// （false，⋯ 菜单的「下载全部」走那条）。
	only bool
	// name 是「动的是哪一个收藏夹」。**批量那两种是空的**——它们说的不是容器。
	//
	// 🔴 它必须进标题：删整个收藏夹和删一批歌是两件事，而同一个词（「删掉」）
	// 配一个「吗」是最容易让人按错确认的那种框。规格点名了这一条
	// （issue #100：「删除的确认弹窗说清删的是哪一个收藏夹」）。
	name string
}

// collectionMenuItem 是 ⋯ 菜单里的一项。
//
// 是 `favMenuAction` 的别名而不是新的一份结构体：两项的形状一样
// （一个键 + 一个字），而多一份就多一处「两份菜单项表各自漂移」的地方。
type collectionMenuItem = favMenuAction

// mediaKey 是这个页面上**一行歌的身份**。
//
// 🔴 铁律四：这一页的行身份必须是「B 站那条内容的 id + 类型」，不能是序号。
// 两条理由各占一半：
//
//   - 自动续拉在**尾部**追加（序号不动，序号够用）；
//   - 但**删除之后整页重来**——翻到第 800 首的用户勾了 12 首，删掉第 3 首
//     之后整个列表的序号前移一位，那 12 个序号集体指到了别的歌上。
//
// 带 Type 是因为 B 站的 resource id 只在类型内唯一（同 id 的音频和视频能
// 撞上），而批量删除要的正是 `id:type` 这一对（见 `bili.RemoveCollectionMedias`）。
func mediaKey(m bili.Media) string {
	return "bili:" + strconv.FormatInt(m.ID, 10) + ":" + strconv.Itoa(m.Type)
}

// mediaIndex 是「行身份 → 那一条的完整信息」。
//
// 为什么单独一把锁：写它的是**后台拉页的 goroutine**（Fetcher 在那里跑），
// 读它的是界面线程。不上锁 `-race` 一定红，而这种红是偶发的、指向一个
// 「谁也不知道为什么会碰同一张表」的协程。
//
// 为什么需要它：批量删除要的是 B 站的 `resource id + type`，而列表里那一行
// 只有给人看的字段——`list.Music` 那一层不认识 B 站（它连本地文件都画）。
type mediaIndex struct {
	mu sync.RWMutex
	m  map[string]bili.Media
}

func (x *mediaIndex) put(key string, m bili.Media) {
	x.mu.Lock()
	if x.m == nil {
		x.m = map[string]bili.Media{}
	}
	x.m[key] = m
	x.mu.Unlock()
}

func (x *mediaIndex) get(key string) (bili.Media, bool) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	m, ok := x.m[key]
	return m, ok
}

// reset 清空。换了排序或者删完一整页时调——旧的那批已经不在列表里了。
//
// ⚠️ 正在途的那一次拉页还可能往里写（Reload 只作废结果、不掐死 goroutine）。
// 那样写进去的是同 id 同 type 的那一条，而那个值跟排序无关，所以补写一次
// 无害；`reset` 之后表是空的这一层语义不受影响。
func (x *mediaIndex) reset() {
	x.mu.Lock()
	x.m = nil
	x.mu.Unlock()
}

// collectionPage 是这一页此刻的全部状态。
//
// **只由界面线程读写**（铁律一）。挂在外壳上（`Shell.collection`），换容器时
// 整个换掉。
type collectionPage struct {
	ref app.CollectionRef

	// list 是下面那个分页列表。
	list *list.PagedList

	// listSort 是**这个列表是按哪一档排序建的**。
	//
	// ⚠️ 它和 `sort` 分开存，是因为排序下拉写的是 `sort`、而列表是拿建它
	// 当时那个值建的（见 newCollectionList）。不分开的话就没有一个地方能
	// 判出「下拉变了、列表还没跟上」——表现是选了新排序而内容不变。
	sort     bili.CollectionSort
	listSort bili.CollectionSort

	// search 是那一枚会展开的搜索此刻是收着还是开着。
	search searchButtonState

	// draft 是搜索框里正在敲的字，keyword 是**已经在筛**的那个词。
	//
	// 分成两个是因为即时模式每一帧都重画：直接拿框里的字去筛，用户每敲一个
	// 字就重拉一次（防抖解决的是频率，解决不了「框里的字和生效的词是同一个」
	// 这个混淆）。理由和 `libraryPage` 的那一份一字不差（见 librarypage.go）。
	draft, keyword string

	// seenDraft 是上一帧框里的字，pendingSince 是「敲下去、还没发出去」的起点。
	//
	// 零值 = 没有待发的搜索。防抖的钟靠 `seenDraft` 重打：用户每敲一个字都要
	// 重新开始等那 500ms（上游 `useDebounceFn` 也就是这么做的）。
	seenDraft    string
	pendingSince time.Time

	// submitted 是这一帧搜索框有没有被回车提交。**它必须记在状态上**而不是当成
	// 一个返回值传下去：画完之后还要拿它判要不要换查询，而那一段和画输入框
	// 不在同一个闭包里。
	submitted bool

	// media 是行身份 → 那一条完整信息。见 mediaIndex。
	media mediaIndex

	// multi 是多选态开着没有。
	multi bool
	// picked 是勾上的行身份。
	//
	// ⚠️ 它**只装当前列表里有的行**：换排序 / 刷新时整个清空，而列表往后是
	// 追加的（只会多不会少）。「只增不减」是 `allSelected` 能用长度相等来判
	// 的前提——集合里混进了已经不在列表里的行，长度就会说谎。
	picked map[string]bool

	// confirm 是「要不要用户点一下确认」。
	confirm confirmRequest
}

// newCollectionPage 建这一页的状态。
//
// ⚠️ **不在这里拉第一页**（同 `list.NewPaged` 的理由）：它可能在界面线程
// 之外被调，那时候既没有唤醒句柄也没有画面。第一页由第一帧的 view 拉。
func (s *Shell) newCollectionPage(ref app.CollectionRef) *collectionPage {
	p := &collectionPage{ref: ref, picked: map[string]bool{}}
	p.list = s.newCollectionList(p)
	p.listSort = p.sort
	// 元信息在后台拉，头图区这一帧先画占位（见 viewCollectionHeader）。
	s.app.LoadCollection(ref)
	return p
}

// newCollectionList 按当前的排序建一个分页列表。
//
// ⚠️ **Fetcher 要的是排序「当时那个值」**，所以这里先拷进局部变量再让闭包
// 捕获它。闭包直接读 `p.sort` 的话，那一行是在**后台 goroutine** 里读界面
// 线程的字段：`-race` 直接报，而真机上的表现是「偶尔选了『最多播放』却拉回
// 『最近收藏』的内容」。
//
// **搜索词也是这么拷的**（同一个道理）：词一换，整个列表换掉，而列表是拿建它
// 当时那个词建的。
//
// 换排序时整个换掉列表而不是换 fetcher，同一个道理（换 fetcher 还是闭包读
// 字段）。
func (s *Shell) newCollectionList(p *collectionPage) *list.PagedList {
	acts := s.collectionActions()
	ref, sort, keyword, media := p.ref, p.sort, p.keyword, &p.media
	l := list.NewPaged(s.images, acts, func(ctx context.Context, pageNo int) (list.Page, error) {
		return s.fetchCollectionPage(ctx, ref, sort, keyword, pageNo, media)
	})
	// ⚠️ **「正在加载」那一格由外面递进去**（票 #138）：分页列表自己那一句是
	// 13px 淡灰缩在列表左上角，而这一页的「没有收藏夹」是居中 15pt 两行。
	l.SetEmptyView(loadingCell("正在加载…"))
	// 播放量 + 时间两列：这一页两个数都有（`bili.Media.View` / `FavTime` /
	// `PubTime`）。时间那一列的**表头字和取哪个时间随容器类型变**——两处走
	// `collectionTimeTitle` / `collectionTimeOf` 这一对，别只改一边。
	l.SetColumns(list.Columns{
		PlayCount:    true,
		PubTime:      true,
		PubTimeTitle: collectionTimeTitle(p.ref.Type),
		Now:          s.now,
	})
	return l
}

// collectionActions 是这一页列表的操作，比通用那一份多换了「下载」那一项。
//
// 🔴 **换的是 Download**：`list.AppActions` 给的是「同步、不补 cid」的那一条
// （界面线程上一次网络往返都不许有），而**收藏夹的条目不带 cid**——B 站的内容
// 列表接口只给 `page` 数，不给分 P id（见 `bili.Media`）。不换的话这一页的
// 「下载音频」按下去每一首都是「还没取到分 P id」，而批量下载却是能用的
// （那一路在后台补），同一个页面上两件看起来一样的事一个成一个不成。
//
// 搜索结果那一页**不换**，是有意的：它的条目同样只有 bvid，而那一页要如实
// 拒绝「下不了」（见 `list.AppActions` 的注释）。
func (s *Shell) collectionActions() list.Actions {
	acts := s.listActions()
	acts.Download = func(it app.Item) { s.downloadLater(it, false, "下载音频") }
	return acts
}

// fetchCollectionPage 拉一页内容并翻成列表要的形状，顺带把完整信息记进 media。
//
// ⚠️ **跑在后台 goroutine 里**（`list.PagedList` 的 Fetcher），所以它不许碰
// 界面状态：这里唯一写的东西是 `media`，而它自己带锁（见 mediaIndex）。
//
// keyword 是建这个列表时那个词（见 newCollectionList 的注释）：它是**值**，
// 不是界面字段，所以后台读它不犯铁律一。
func (s *Shell) fetchCollectionPage(ctx context.Context, ref app.CollectionRef, sort bili.CollectionSort, keyword string, pageNo int, media *mediaIndex) (list.Page, error) {
	medias, hasMore, err := s.app.LoadCollectionPage(ctx, ref, sort, keyword, pageNo)
	if err != nil {
		return list.Page{}, err
	}

	songs := make([]list.Music, 0, len(medias))
	for _, m := range medias {
		key := mediaKey(m)
		media.put(key, m)
		songs = append(songs, list.Music{
			ID:       key,
			Title:    m.Title,
			Cover:    m.Cover,
			Owner:    m.Owner,
			Duration: m.Duration,
			Item:     onlineItemOf(m),
			// 播放量：接口的 `cnt_info.play`（`bili.Media.View`）。音轨那一条
			// 本来就没有这个数——零值，界面那一格留空（见 `Music.View`）。
			View: m.View,
			// 时间那一列画哪个时间**随容器类型变**（上游也是分开的）：
			//
			//	收藏夹	`fav_time`（收藏时间，表头写「收藏时间」）
			//	合集 / 系列	`pubtime`（发布时间，表头写「投稿时间」）
			//
			// 拿一个套另一个的话，收藏夹里会写着「投稿时间」而画的是收藏时间
			// ——用户按它判断新旧，读到的是错的。
			PubTime: collectionTimeOf(ref.Type, m),
		})
	}
	return list.Page{Songs: songs, HasMore: hasMore}, nil
}

// collectionTimeOf 是这一条该画进「时间」那一列的时间。
//
// 按容器类型分开取，**不拿一个套另一个**（上游也是分开的）：
//
//	收藏夹	`fav_time`（上游 `favorites/list.tsx:63`，表头写「收藏时间」）
//	合集 / 系列	`pubtime`（上游 `collections/list.tsx:75` / `series/list.tsx:84`）
//
// 零值照旧传出去（界面上那一格留空）：那一条没给这个时间，编一个
// 「收藏时间 = 发布时间」比空着更坏。
func collectionTimeOf(typ bili.CollectionType, m bili.Media) time.Time {
	if typ == bili.CollectionFavorite {
		return m.FavTime
	}
	return m.PubTime
}

// collectionTimeTitle 是时间那一列的表头字。和 `collectionTimeOf` 是同一
// 件事的两半，**必须一起改**：收藏夹表头写「收藏时间」，合集 / 系列写
// 「投稿时间」。
func collectionTimeTitle(typ bili.CollectionType) string {
	if typ == bili.CollectionFavorite {
		return "收藏时间"
	}
	return "投稿时间"
}

// onlineItemOf 把一条 B 站内容翻成播放队列认的那一项。
//
// ⚠️ **合集（21）和电影（24）也给 Source="online"**，Type 是 `"collection"`：
// 它们没有音频流，而 `app.playableOnline` 对这一类会回一句「这一条是
// 合集/剧集那一类，B 站没有给它们单独的音频流」——那一类**在界面上压根不给
// 播放入口**（见 `app.UnplayableMenuNote`）。
//
// 🔴 **Type 不能空着**：空 Type 在判据里和「没挂稿件的推荐条目」是同一种，
// 于是那句话就成了「这一条不是投稿」——而用户看到的是收藏夹里的一个合集，
// 他该知道的是「打开里面的单集就能放」。
//
// 换算本身在 `app.ItemOfMedia`——**全项目只有那一份**，而批量下载那边也要用
// （`app.EnqueueDownload`）。这里留一个调用是为了那一处调用点不用改。
func onlineItemOf(m bili.Media) app.Item { return app.ItemOfMedia(m) }

// collectionFor 拿（或建）这个容器此刻的那一份页面状态。
//
// ⚠️ **换容器就整个换掉**：留着上一份的话，收藏夹 A 的勾选、排序、滚动位置
// 会原样搬到收藏夹 B 上——而两个容器里的歌根本不是同一批。判断「换没换」
// 用的是 `CollectionRef` 自己的相等（`Key()` 把 id 和类型都算进去了）。
func (s *Shell) collectionFor(ref app.CollectionRef) *collectionPage {
	if s.collection != nil && s.collection.ref == ref {
		return s.collection
	}
	s.collection = s.newCollectionPage(ref)
	return s.collection
}

// ── 视图 ───────────────────────────────────────────────────────────────────

// viewCollection 是这一页。外面由 `Shell.page` 那一道 Guard 包着。
//
// 🔴 铁律二：**每一帧的第一件事就是装主题**，而且必须在最开头。MyGo 每帧开头
// 把主题重置成跟随系统那份（`ui/context.go`），放到切换按钮的回调里只调一次
// 会被**静默丢掉**（不报错、不崩溃、不打日志，就是主题悄悄跳回系统色）。
func (s *Shell) viewCollection(c *ui.Context, r *ui.Route) {
	s.applyFrameTheme(c)

	// 「刚才那个收藏夹真的删掉了，所以该走开了」——**消费在界面线程的这一帧**。
	//
	// ⚠️ 排在主题之后、这一页别的逻辑之前：它会 `navigate`，而那一帧画出来的
	// 还是当前这一页（`Router` 下一帧才切），所以用户看不出多了一帧。
	//
	// ⚠️ **为什么不是直接在后台回调里 navigate**：那一趟跑在 goroutine 里，而
	// `navigate` 和 `s.collection = nil` 都是界面状态（铁律一）。见 `takeLeave`。
	s.takeLeave()

	ref := collectionRefOf(r)
	if !s.app.CollectionsMounted() {
		// 没挂接口时画得出来（头图区是空的、列表一直转圈），但要告诉用户
		// 为什么——「点了没反应」比「没做」更难解释。
		s.viewEmpty(c, "collection:not-mounted", emptyState{
			Title: "暂时打不开收藏夹",
			Hint:  "这一页现在打不开，稍后再试试",
			// ⚠️ 走的是「拉不出来」那一支的插画（警告），不是空状态那一枚
			// 收藏夹——接口没挂上是失败，不是「那个夹子本来就是空的」。
			Icon: iconFailed,
		})
		return
	}

	p := s.collectionFor(ref)
	info := s.app.Collection(ref)

	// ⚠️ **横向那一格在**根**上，不在下面那几块里**：这一页原来是头图区、操作条
	// 各自留 24，而下面那个列表**一点没留**——于是表头和列表行比头图区的标题还
	// 靠左 24 像素，同一页里两条左缘对不齐（上游那边列表是在 `px-4` 里的，见
	// `pages/video-collection/favorites/index.tsx:436`）。
	//
	// 收到根上之后，这一页的每一样东西都从同一条左缘开始（票 #138）。
	ui.Column(c).Key("page:collection:"+ref.Key()).Grow(1).Padding(space.None, pagePadX).Children(func() {
		s.viewCollectionHeader(c, p)
		s.viewCollectionOps(c, p)

		// 容器拉回来了、里面一条都没有：画「没有收藏夹」那一句
		//（`empty.go` 的 noFavorites，上游出处也在那儿）。
		//
		// ⚠️ 判据是 **MediaCount 为 0** 而不是「列表此刻一行都没有」：
		// 后者在翻页加载的那一瞬间也为真，那样会让整页内容闪一下空状态
		// ——用户点进收藏夹、列表转了两帧、中间整个内容区变成一句
		// 「暂无内容」。
		switch {
		case info.Loaded && info.MediaCount == 0:
			s.viewEmpty(c, "collection:empty", noFavorites)
		case p.keyword != "" && p.list.Paged().Loaded == 0 && !p.list.Paged().Loading:
			// 搜了，**一条都没有**：说清楚，而不是留一块空白。
			//
			// ⚠️ 判据是「**词已经发出去了**」而不是「列表此刻一行都没有」：
			// 换词的这一瞬间列表是空的（还在拉），那时候画「换个词再搜一次」
			// 是骗人——用户刚敲完词。
			//
			// 用的就是搜索页那一句（`searchpage.go` 的 `viewSearchResultsBody`）：
			// 「暂无内容 / 换个词再搜一次，或者把关键词缩短一点」——同一件事，
			// 同一句话。
			s.viewEmpty(c, "collection:no-results", noSearchResults)
		default:
			// ⚠️ **第一页在途时，这一格由列表自己画**（画法是外面递进去的，
			// 见 `newCollectionList` 和 `loadingCell`）：它和「没有收藏夹」那一格
			// 是同一个出口（票 #138）。
			//
			// ⚠️ **首屏只说一次「正在加载…」**：头图区那个大标题原来写的是
			// 「正在加载…」（22px 粗体），下面列表又画一句 13pt 淡灰的——同一屏
			// 两个加载提示，相隔一百多像素。现在头图区写的是**这一类容器的名字**
			// （「收藏夹」/「视频合集」/「视频系列」，见 `collectionTitleText`），
			// 加载那句话只由内容区这一格说。
			//
			// 🔴 设置里的「数据显示样式」在这一帧生效（理由同搜索页那一处）。
			//
			// ⚠️ 它**换不掉**多选接线（`attachSelection` 装的那一份挂在两份控件上，
			// 见 `widgets/list/switch.go`），所以网格那一档点勾照样能批量删。
			p.list.SetView(s.displayView())
			// ⚠️ **表头在列表外面**（`rowheader.go` 文件头）：它是 `View` 的兄弟，所以
			// 列表往下滚它不动。它自己会判多选态要不要给勾选框让位置（`HeaderSpec` 的
			// Lead），所以这一处不用管多选。
			p.list.Header(c, "collection:header")
			// 列表自己占了 Grow(1)（见 `list.List.View`），所以多选态下面那条
			// 批量操作条自然被它顶到最下面。
			p.list.View(c)
			s.noteScrollbar(p.list.Hot())
			if p.multi {
				s.viewCollectionBatchBar(c, p)
			}
		}
	})
	s.viewCollectionConfirm(c, p)
}

// syncCollection 是收藏夹 / 合集 / 系列这一页每帧的副作用：收「刚才那个容器真的
// 删掉了」那一趟、把后台攒下的话发成 Toast、收列表的后台结果、起该起的那一拉。
//
// ⚠️ **它们原来在 `viewCollection` 的开头**，而这一页在满屏开着的时候不画
// （票 #161）——不挪的话，用户在满屏里看频谱的那几分钟里，列表的后台结果一直
// 躺在信箱里没人取，关掉满屏之后那一页还是「正在加载」。
//
// ⚠️ **换排序 / 换关键词那两条不在这里**：它们要读这一帧刚建出来的下拉和输入框
// （`viewCollectionSort` / `viewCollectionSearch`），而满屏开着的时候用户碰不到
// 它们。
func (s *Shell) syncCollection(c *ui.Context) {
	// 「刚才那个收藏夹真的删掉了，所以该走开了」——**消费在界面线程的这一帧**。
	s.takeLeave()

	ref := collectionRefOfPath(s.router.Path(), s.router.Query(collectionTypeQuery))
	if !s.app.CollectionsMounted() {
		return
	}
	p := s.collectionFor(ref)

	// ⚠️ **满屏开着的时候这一页不画 → 那个 500ms 的防抖钟没人推进**：不清它的话
	// `syncSearchWatch` 会一直判「该醒着」，每 125ms 叫醒一次窗口，而那一帧画的是
	// 满屏（白醒）。理由和 `syncLibraryPage` 那一处一字不差。
	if s.underlayHidden() {
		p.pendingSince = time.Time{}
	}

	// 后台攒下的话发成 Toast（取走而不是读）。
	s.flushCollectionNotice(p)

	// 🔴 设置里的「数据显示样式」在这一帧生效（理由同搜索页那一处）。
	p.list.SetView(s.displayView())

	// 收后台投回来的结果、起该起的那一拉。
	p.list.Sync(c)
}

// flushCollectionNotice 把后台攒下的话发成 Toast。
//
// **每帧开头调一次，而且取走**：取过就不会再说第二遍，不取的话同一句会在
// 每一帧都冒一次 Toast（见 `app.CollectionNotice`）。
//
// ⚠️ `NoticeCollectionChanged` 的那一条还要顺手把列表重来一遍——**而且只能
// 在这里**（界面线程）。别改成在动作那一侧 reload：那一次拉回的是还没改完
// 的数据（理由见那个常量）。
func (s *Shell) flushCollectionNotice(p *collectionPage) {
	n, ok := s.app.TakeCollectionNotice(p.ref)
	if !ok {
		return
	}
	s.addToast(n.Text, n.Tone)
	if n.Kind == app.NoticeCollectionChanged {
		s.reloadCollection(p)
	}
}

// collectionRefOf 从路由上读出是哪个容器。
//
// ⚠️ **id 是自己从路径上切出来的，不走 `r.Param("id")`**。`Param` 读的是
// 「上一次 `Route.Match` 匹配时记下的通配值」——而本项目认页用的是自己的
// `lookup`（router.go 的注释写明「不再是一长串 `r.Match(...)`」），从来没有
// 调过 `Match`。于是 `Param` 恒为空串，页面上是「收藏夹 id 不对（0）」。
//
// id 读不出来（不是数字）时是 0，接口会回 -400，界面上是一句
// 「打不开这个收藏夹」——比画一张空白页好。
func collectionRefOf(r *ui.Route) app.CollectionRef {
	return collectionRefOfPath(r.Path(), r.Query(collectionTypeQuery))
}

// collectionRefOfPath 从路径和查询串上读出是哪个容器。
//
// ⚠️ **它和 `collectionRefOf` 是同一件事的两条路**：画的时候问路由，而每帧的
// 副作用那一层手上只有一条路径和一个查询串。
func collectionRefOfPath(path, query string) app.CollectionRef {
	id := int64(0)
	if rest, ok := strings.CutPrefix(path, collectionPathPrefix); ok {
		// 后半截（真有了「合集里的某一集」那种子路径时）不参与识别：
		// 那一段是哪一条由下一页自己读，这里认的是容器本身。
		head, _, _ := strings.Cut(rest, "/")
		id, _ = strconv.ParseInt(head, 10, 64)
	}
	return app.CollectionRef{ID: id, Type: bili.ParseCollectionType(query)}
}

// ── 头图区 ─────────────────────────────────────────────────────────────────

// 头图区那几个尺寸。照 Biu 的 `header/index.tsx`：封面 200×168（不是方的）。
const (
	collectionCoverW = 200
	collectionCoverH = 168

	// collectionHeadGap 是那三行字之间的行距：`space-y-4` = **16**
	// （上游同一个文件 :53-113）。
	collectionHeadGap = space.Px16

	// 创建者是 `User` 组件那一枚圆头像。票面给的是 **24**。
	collectionOwnerAvatarSize  = 24
	collectionOwnerAvatarLabel = "创建者头像"
)

// collectionOpsButtonH 是操作条上那枚主按钮的高。上游的 `Button` 默认 size
// 是 md = `h-10`，也就是 **40**。旁边那几枚图标键仍然是 `iconHitMd`（32）——
// 那一排的宽度在最小窗口上已经贴着边（见 `viewCollectionOps`），再把图标键
// 撑到 40 就会把这一排挤出去。
const collectionOpsButtonH = 40

// viewCollectionHeader 是顶部头图区：封面 + 标题 + 简介 + 类型和条数 + 创建者。
//
// 文案和排列照抄 `src/pages/video-collection/header/index.tsx:53-113`：右边那一
// 列是 `space-y-4`，标题 `text-3xl font-bold`，下面三行都是 `text-sm`（14）。
func (s *Shell) viewCollectionHeader(c *ui.Context, p *collectionPage) {
	t := c.Theme()
	info := s.app.Collection(p.ref)

	// ⚠️ **横向那一格用 `pagePadX`**（七页共用一个数，见 pagerhythm.go）。
	ui.Row(c).Key("collection-header").Gap(space.Px16).Padding(space.Px20, space.None, space.Px8).
		Children(func() {
			// ① 封面。空地址时 imgcache 画空框（没给封面的收藏夹真的有）。
			imgcache.View(c, s.images, info.Cover, collectionCoverW, collectionCoverH)

			ui.Column(c).Grow(1).AlignItems(ui.Start).Gap(collectionHeadGap).Children(func() {
				// ⚠️ **这个标题是 30（`scale.CollectionTitle`），不是页标题那个数**：
				// 头图区的大标题抄上游 `text-3xl`，见 pagerhythm.go 那段。
				ui.Text(c, collectionTitleText(p.ref.Type, info)).FontSize(scale.Font(c, scale.CollectionTitle)).Bold().
					MaxLines(2).Ellipsis("…")

				// ② 简介。Biu 是 `line-clamp-1`，有才画。
				if info.Intro != "" {
					ui.Text(c, info.Intro).FontSize(scale.Font(c, scale.CollectionCopy)).LineHeight(leading.Copy).
						TextColor(t.TextMuted).MaxLines(1).Ellipsis("…")
				}

				// ③ 类型 + 条目数。Biu 写的是「{公开|私密}收藏夹 • N 条视频」
				// （`header/index.tsx:88-96`）——**条目数永远说「条视频」**，
				// 上游连音频也是这么写的，这里照抄。
				ui.Text(c, collectionKindText(p.ref.Type, info.Private)+" • "+
					strconv.Itoa(info.MediaCount)+" 条视频").
					FontSize(scale.Font(c, scale.CollectionCopy)).LineHeight(leading.Copy).
					TextColor(t.TextMuted).MaxLines(1)

				// ④ 创建者：一枚 24 的圆头像 + 14 的昵称（上游是一个 `User`）。
				if info.OwnerName != "" {
					ui.Row(c).Gap(space.Px8).Center().Children(func() {
						s.viewCollectionOwnerAvatar(c, info.OwnerFace, info.OwnerMid)
						// 🔴 **名字是一枚可点的名字型链接**（票 BMG-253）：这一行
						// 原先**整个都点不动**——用户看到「某某创建的」却进不去他的
						// 空间，而**同一页的列表里每一行的 UP 名是能点进去的**
						// （一页两套）。
						//
						// ⚠️ **走 `list.OwnerName`**：那是列表里那一枚的**同一个
						// 出口**——同一档反馈、同一套下划线、同一句提示。两处各画
						// 各的的话，同一个 UP 名在一处有下划线、在另一处没有。
						list.OwnerName(c, info.OwnerName, info.OwnerName,
							list.OwnerTipText(info.OwnerName), info.OwnerMid,
							scale.Font(c, scale.CollectionCopy), leading.Copy,
							t.TextMuted, s.ownerActions())
					})
				}
			})
		})
}

// viewCollectionOwnerAvatar 是创建者那一枚圆头像（`collectionOwnerAvatarSize`）。
//
// 地址空着、还没下回来、或者下失败时画一块同色的圆占位——**头像这一格不消失**：
// 它是「创建者」那一行的开头，消失之后那一行只剩一个名字，左边凭空空出一格；
// 而占位的大小和真头像一样，换上来的时候那一行不会动。
//
// 🔴 **它是一枚按钮**（票 BMG-253）：这一行原先整个都点不动——用户看到「某某
// 创建的」却进不去他的空间。悬停时外面多一圈主色的环（和顶栏那个头像同一套，
// 见 `avatarHoverRing`），所以它**看起来也是能点的**。
//
// ⚠️ **没有编号时它不是按钮**：那一页压根没给创建者的编号，而点它只能跳到一个
// 空路径（理由同 `list.OwnerName` 那一段：把入口画成能点的、点下去却到不了
// 地方，比不画更坏）。
func (s *Shell) viewCollectionOwnerAvatar(c *ui.Context, face string, mid int64) {
	t := c.Theme()
	side := float32(collectionOwnerAvatarSize)
	// 那一枚圆（占位 / 真图都在里面）。
	//
	// ⚠️ **名字只挂一处**：可点的那一格把名字挂在**按钮**上（下面），里面这一
	// 格就不挂——两处都挂的话辅助技术会念两遍，而画面上只有一枚圆。
	draw := func(label string) {
		ui.Box(c).Size(side, side).Radius(side / 2).Clip().
			Background(t.Surface).Label(label).Children(func() {
			if st := s.images.Get(face); face != "" && st.Ready() {
				ui.Image(c, st.Bitmap).Fit(ui.Cover).Size(side, side)
			}
		})
	}
	if mid <= 0 {
		draw(collectionOwnerAvatarLabel)
		return
	}
	hit := ui.ButtonBase(c).Size(side, side).Padding(space.None).
		Label(collectionOwnerAvatarLabel)
	// ⚠️ hover 要在 Children **之前**问（理由同顶栏那个头像）。
	hit = avatarHoverRing(c, hit, hit.Hovered(), side)
	hit.Children(func() { draw("") })
	if hit.Clicked() {
		s.openOwnerSpace(mid)
	}
}

// collectionTitleText 是头图区那个大标题。
//
// ⚠️ **还在拉的时候写「收藏夹 / 视频合集 / 视频系列」，不写「正在加载…」**：
// 这一页的加载态由**内容区那一格**说（见 `viewCollection` 里加载那一支），而
// 头图区再说一遍就是首屏**两个「正在加载…」**——一个 30px 粗体、一个 15px
// 淡灰，相隔一百多像素（那正是这一票要消掉的东西）。
//
// 而写类型名也不是留一个空标题：空标题会在那儿留一个空的 h1，用户会以为这个
// 收藏夹真的没名字。
func collectionTitleText(typ bili.CollectionType, info app.CollectionInfo) string {
	switch {
	case !info.Loaded:
		return typ.Label()
	case info.Title == "":
		return "（没名字的收藏夹）"
	}
	return info.Title
}

// collectionKindText 是头图区第三行那句话的前半截。
//
// ⚠️ 收藏夹要带上公开 / 私密（Biu 的 `isPrivateFav(attr) ? "私密" : "公开"`），
// 合集和系列那边上游不写这两个字，照抄。
func collectionKindText(typ bili.CollectionType, private bool) string {
	if typ != bili.CollectionFavorite {
		return typ.Label()
	}
	if private {
		return "私密收藏夹"
	}
	return "公开收藏夹"
}

// ── 操作条 ─────────────────────────────────────────────────────────────────

// viewCollectionOps 是头图区下面那一排：左边四个动作 + 多选，右边「按歌名找 +
// 排序」。项和顺序照 Biu 的 `src/pages/video-collection/operation/index.tsx:33-56`
// （上游右边就是 `SearchWithSort`）。主按钮是上游 `Button` 默认的 md 档：**40 高**。
//
// # 两组之间必须有让位机制（这一条是机器钉着的）
//
// 两组的宽度**跟着系统字号缩放走**：排序下拉的自然宽在 1.0 倍下是 83.2，1.4 倍
// 下是 116.5；「播放全部」「多选」那两串字也一样。而最快的那一档（窗口 900 +
// 侧栏 480）内容区只有 **388**——字号一大，两组就装不下了。
//
// ⚠️ **装不下的时候左边那一组自己换行**（`Wrap`）：多选开关落到第二行，那一排
// 于是高一点，但**两组永远不重叠**。实测（评审第 1 条的同一组数）：
//
//	字号 1.25 侧栏 480：换行之前多选的右缘 750.6、搜索的左缘 744.0（重叠 6.6）；
//	                换行之后多选的右缘 636.1、搜索的左缘 744.0（不重叠）。
//	字号 1.40 侧栏 480：换行之前重叠 30.7；换行之后多选的右缘 631.8、搜索 731.5。
//
// 换行的**代价**是那一排在最挤的那一档会高出一行——比「两个控件叠在一起」好得多。
//
// # 右边那一组不许被挤扁
//
// 排序下拉**必须有最小宽**（`collectionSortMinW`，按字号缩放跟着长）：没有它，
// 框架挤不下时先扣它——实测被裁成 66.6 宽、右缘跑到 900（已经出窗口），最后
// 一个字和那个「▾」都不见了。搜索是那个该让位的，它的下限是
// `collectionSearchMinWidth`（40）。见 `viewCollectionSort`。
func (s *Shell) viewCollectionOps(c *ui.Context, p *collectionPage) {
	t := c.Theme()

	// ⚠️ **横向那一格用 `pagePadX`**（七页共用一个数，见 pagerhythm.go）。
	//
	// ⚠️ **右边那一组自己 `Grow(1)` + `Justify(End)`，不是外面一个 `Spacer`**：
	// `Spacer` 自己也是一个子元素，它两边各占一道 8 的缝——最窄那一档里那 16
	// 像素正好把搜索挤到下限以下（实测多出 8.6）。让右边那一组把剩下的空地
	// 全吃掉、内容靠右，既不留缝，搜索框又有一个能长到 240 的地方。
	ui.Row(c).Key("collection-ops").FillWidth().Center().
		Padding(space.None, space.None, space.Px12).Children(func() {
		// ⚠️ **`Wrap` 是这一排的让位机制**（理由见文件头那两段）：这一组放不
		// 下时它自己换行，而不是让内容溢出到右边那一组身上。
		ui.Row(c).Gap(space.Px8).Center().Wrap().Children(func() {
			// 上游那枚主按钮是 `Button` 默认的 md 档：**40 高**。
			if ui.PrimaryButton(c, "播放全部").Height(collectionOpsButtonH).Clicked() {
				s.playWholeCollection(p)
			}
			// 上游是一枚 `size="md" variant="flat"` 的图标键
			// （`operation/index.tsx:44`，tooltip「添加到播放列表」）。
			if s.withTip(c, flatIconButton(c, iconEnqueue, "添加到播放列表", iconMd).
				Size(iconHitMd, iconHitMd).Padding(space.None, space.Px7), "添加到播放列表").Clicked() {
				s.addWholeCollectionToQueue(p)
			}
			s.viewCollectionStar(c, p)
			s.viewCollectionMore(c, p)

			// 多选开关。多选态开着时这一排还在，只是列表左边多了一列勾。
			// 它是文字键（那是「一个模式」，不是一个动作），但同样不走
			// `ui.Button`——那一圈描边在这一排上是唯一的异类。
			multi := flatButton(c, "多选")
			if p.multi {
				multi = multi.Background(t.SurfaceHover)
			}
			if multi.Clicked() {
				s.toggleCollectionMulti(p)
			}
		})

		// 右边「按歌名找 + 排序」，照上游 `SearchWithSort` 的次序。
		//
		// ⚠️ **`Grow(1)` 是给搜索框留的那块地**：它自己上限 240，而 `Grow` 只
		// 在有富余的父宽度里才长。少了这一句，输入框永远是 40 宽（实测截图里
		// 一个字都填不满）。`Justify(End)` 让这一组贴着页边距。
		ui.Row(c).Grow(1).Gap(space.Px8).Center().Justify(ui.End).Children(func() {
			s.viewCollectionSearch(c, p)
			s.viewCollectionSort(c, p)
		})
	})
}

// viewCollectionStar 是收藏 / 取消收藏那个星标。
//
// Biu 是实心 / 空心两个图标（`operation/fav-toggle.tsx:12-20`），而且是
// `isIconOnly size="md" variant="flat"` 的一枚图标键。我们照它——图标集里
// `iconStar` / `iconStarFilled` 就是那两个文件，画哪一张仍由 `favIcon` 说了算
// （状态图标唯一出口那条规矩）。
//
// ⚠️ Biu 那一侧对「视频系列」和「自己创建的收藏夹」不画这个按钮
// （`operation/index.tsx:47-49`）。这一页拿不到「是不是自己创建的」
// （`fav_state` 说的是这个容器有没有被别人收藏），所以两种都画——
// 让服务端拒绝比在界面上猜一个条件准。
func (s *Shell) viewCollectionStar(c *ui.Context, p *collectionPage) {
	info := s.app.Collection(p.ref)
	tip := "收藏这个收藏夹"
	if info.Faved {
		tip = "取消收藏"
	}
	if s.withTip(c, flatIconButton(c, favIcon(info.Faved), tip, iconMd).
		Size(iconHitMd, iconHitMd).Padding(space.None, space.Px7), tip).Clicked() {
		s.setCollectionFav(p, !info.Faved)
	}
}

// viewCollectionMore 是那个 ⋯ 菜单。
//
// 项照 Biu 的 `operation/menu.tsx:37-100`：下载全部音频 / 下载全部视频 /
// 清除失效内容 / 删除。「下载」两条走的是同一个接口（B 站给的是同一条音频
// 流，「音频 / 视频」是**下载器**那边的分别），所以现在它们是同一条路。
//
// ⚠️ **「删除」改成了「删除收藏夹」，而且旁边多了一项「从侧栏移除」**——
// 上游那两项都没有。理由是规格点名要「删 B 站那边的」和「只删本地记录」摆成
// **两个选项**（issue #100，父规格 #91 的第 28 条）：只摆一个「删除」的话，
// 用户按之前分不清那一下到底是 B 站上的收藏夹没了，还是只是这一侧不显示了。
func (s *Shell) viewCollectionMore(c *ui.Context, p *collectionPage) {
	items := s.collectionMoreItemsFor(p)
	// 上游是一枚 `isIconOnly variant="flat"` 的 ⋯（`operation/menu.tsx:114`）。
	// `.Menu()` 本身就是「按钮 + 菜单」（`mygo/ui/menu.go` 顶上那个例子就是用
	// `ButtonBase` 拼的），所以扁平配方接得上——`ui.MenuButton` 那一版会自己
	// 画一圈描边和一枚 ▾，放在这一排新扁平的三枚旁边就它一个不是。
	s.withTip(c, flatIconButton(c, iconMore, "更多操作", iconMd).
		Size(iconHitMd, iconHitMd).Padding(space.None, space.Px7), "更多").
		Menu(func(m *ui.Menu) {
			for _, item := range items {
				if m.Item(item.label).Chosen() {
					s.collectionMoreAction(p, item)
					return
				}
			}
		})
}

// collectionMoreBaseItems 是 ⋯ 菜单里**每一页都有**的那几项。文案照 Biu。
var collectionMoreBaseItems = []collectionMenuItem{
	{key: "download-audio", label: "下载全部音频"},
	{key: "download-video", label: "下载全部视频"},
}

// collectionMoreOwnerItems 是**自己创建的收藏夹**才摆的那几项。
//
// ⚠️ **判断的依据是「能不能删」**（`app.CanDeleteCollection`），不是「这是不是
// 收藏夹」。照上游 `menu.tsx` 的 `show: isCreatedBySelf && !isDefaultFav(attr)`：
// 别人建的收藏夹摆不出删除（B 站不给这个接口），B 站发下来的那个默认收藏夹
// 也摆不出（它同样删不掉）。
var collectionMoreOwnerItems = []collectionMenuItem{
	{key: "clear-invalid", label: "清除失效内容"},
	{key: "delete-folder", label: "删除收藏夹"},
}

// collectionMoreForgetItem 是「只删本地记录」那一项。
//
// ⚠️ **它对每一页都摆得出来**，不限于自己创建的：对别人的收藏夹，它恰恰是
// 唯一能整掉它的办法（一个字节都不往 B 站发）。Biu 上游没有这一项——上游
// 侧栏那个「隐藏」作用一样，但没有和「删除」并排摆出来过。
var collectionMoreForgetItem = collectionMenuItem{key: "forget-folder", label: "从侧栏移除"}

// collectionMoreItemsFor 是 ⋯ 菜单此刻摆得出来的那些项，**按显示顺序**。
//
// ⚠️ **每次返回一份新的**：往那张包级切片上 append，就是让「多开一次菜单」
// 变成一个跨帧的副作用，而那种错没有一处测试会红。
//
// ⚠️ **元信息还没回来时，下面三项一个都摆不出来**：它们全都要收藏夹的名字，
// 而名字在那之前是「正在加载…」。把那句话念进一个不可撤销的确认框里是最糟的
// 一种——用户读到「删掉「正在加载…」吗」只会点确定。留两个下载项（B 站那边
// 的歌还没到，点了也只能下载到零首，那条路径自己会说清楚）。
func (s *Shell) collectionMoreItemsFor(p *collectionPage) []collectionMenuItem {
	info := s.app.Collection(p.ref)
	items := make([]collectionMenuItem, 0, len(collectionMoreBaseItems)+len(collectionMoreOwnerItems)+1)
	items = append(items, collectionMoreBaseItems...)
	if !info.Loaded {
		return items
	}
	if s.app.CanDeleteCollection(p.ref) {
		items = append(items, collectionMoreOwnerItems...)
	}
	return append(items, collectionMoreForgetItem)
}

// collectionMoreAction 是 ⋯ 菜单里那一项被选中之后做的事。
//
// 🔴 **这一层一个都不动手**：真正那一下都在 `viewCollectionConfirm` 里——确认框
// 是系统模态，在它上面点任何别的控件都不该生效（理由同 `askConfirm`）。
// 三件新接上的动作全部走同一道门，理由是它们全部是不可撤销的写操作。
func (s *Shell) collectionMoreAction(p *collectionPage, item collectionMenuItem) {
	switch item.key {
	case "download-audio", "download-video":
		// 「全部」也要问一次：它是这一票的验收项，而且下载会占磁盘和带宽，
		// 按错一次就是几百兆白下。
		s.askConfirm(p, confirmDownload, p.list.List().Len(), false)
	case "clear-invalid":
		s.askConfirmAbout(p, confirmClearInvalid, 0, false, collectionTitleText(p.ref.Type, s.app.Collection(p.ref)))
	case "delete-folder":
		s.askConfirmAbout(p, confirmDeleteFolder, 0, false, collectionTitleText(p.ref.Type, s.app.Collection(p.ref)))
	case "forget-folder":
		s.askConfirmAbout(p, confirmForgetFolder, 0, false, collectionTitleText(p.ref.Type, s.app.Collection(p.ref)))
	default:
		// ⚠️ **不许说「还没接接口」**（票 #242，UX 报告 §3-F 的 F2）：那是把
		// 实现细节说给用户听。这一支是「按了一个我们还没接上的动作」，用户
		// 需要知道的只是「这一下现在按不了」。
		s.addToast("「"+item.label+"」现在还不能用", notice.Warning)
	}
}

// viewCollectionSort 是排序下拉。
//
// ⚠️ 用 `SelectBase` 而不是 `ui.Select`：后者收的是 `[]string`，而排序在
// B 站那边是一个枚举。拿字去当排序，中间就多一处「显示的字」和「发给接口的
// order」可能对不上的地方——那个错在界面上完全看不出来。
//
// 换档之后**这里整个换掉下面那个列表**（见 newCollectionList 的注释）：排序是
// 接口排的，本地重排会同时踩两个坑（分页只对一种排序有效、行身份整体错位）。
func (s *Shell) viewCollectionSort(c *ui.Context, p *collectionPage) {
	t := c.Theme()
	sel := ui.SelectBase[bili.CollectionSort](c, &p.sort)

	trigger := sel.Trigger.Padding(space.Px4, space.Px10).Radius(t.Radius).Border(1, t.Border).
		MinWidth(scale.Font(c, collectionSortMinW))
	trigger.Children(func() {
		ui.Text(c, p.sort.Label()).SingleLine()
		ui.Text(c, " ▾").TextColor(t.TextMuted)
	})
	trigger.Label("排序：" + p.sort.Label())

	sel.Popup(func(panel ui.Element) {
		// 外观走共享出口（`popupPanel`）：这一处原来只有底色和描边，没有
		// 阴影——和旁边框架自己弹的浮层差一层影子。
		popupPanel(t, panel.Padding(space.Px4))
		for _, sort := range bili.CollectionSorts {
			item := sel.Item(sort).Padding(space.Px6, space.Px10).Radius(t.Radius)
			switch {
			case item.Highlighted():
				// 高亮走「被选中的选项」那一个出口（实心主色底 + `t.AccentText`
				// 字，票 #239，审计 F-14）。
				item = flatSolid(c, item)
			case sort == p.sort:
				// 当前值（不是「选中」）：淡底、没有主色。
				item = item.Background(t.Surface)
			}
			item.Children(func() { ui.Text(c, sort.Label()).SingleLine() })
		}
	})

	// ⚠️ 判据是「下拉那一档」和「列表是按哪一档建的」不一样，不是
	// `trigger.Changed()`：Changed 只在**点了那一帧**为真，而重画这一页
	// （切标签页、窗口从后台回来）时那一帧已经过去了，列表会永远停在旧排序。
	if p.sort != p.listSort {
		s.setCollectionSort(p)
	}
}

// setCollectionSort 换排序：清掉勾选和旧排序的内容，从第一页按新排序重来。
func (s *Shell) setCollectionSort(p *collectionPage) {
	p.listSort = p.sort
	s.reloadCollection(p)
}

// ── 按歌名找 ────────────────────────────────────────────────────────────────

// collectionSearchWidth 是展开之后那个输入框的**上限**。
//
// 240 是上游 `search-button/index.tsx` 的 `animate={{ width: 240 }}`（它从 40
// 展开到 240）。收起时只有一枚 `iconHitMd`（32）的放大镜。
const collectionSearchWidth = 240

// collectionSearchMinWidth 是展开后那个输入框的**最小**宽。
//
// 40 是这么倒推的：窗口 900 + 侧栏 480 那一档，内容区 388；操作条左边五件 +
// 七道 8 的缝 + 排序下拉的自然宽（83.2）一共 **356.6**，留给输入框的只有
// 388 − 356.6 ≈ **40**（差 0.6 落在排序下拉的描边余量里）。再大一点排序下拉
// 就轮到被裁了，而那一格是用户唯一看得见的说明（裁掉之后它变成一个认不出的
// 方块）。
//
// ⚠️ **排序下拉自己也钉了最小宽**（`collectionSortMinW`）：不然框架会先把
// 它压扁，而搜索还有 40 的下限——两个都「不能被裁」的控件抢同一块地时，
// 先被裁的永远是没有下限的那个。
const collectionSearchMinWidth = 40

// collectionSortMinW 是排序下拉的**最小**宽：它的自然宽（「最近收藏 ▾」四个
// 汉字 + 内边距 + 描边），实测 83.2。
//
// ⚠️ **不给它下限的话它会被搜索挤扁**：框架分配不下时缩的是没有下限的那个，
// 实测在 900 + 侧栏 480 那一档被压到 66.6 宽，最后一个字和那个「▾」都不见了。
// 下面 `viewCollectionSort` 把它按**字号缩放**挂上去（系统字号 1.4 倍时自然宽
// 也长大，写死 83.2 会把字裁掉）。
const collectionSortMinW = 83.2

// collectionSearchLabel 是那一枚搜索的名字，兼做展开后输入框空着时画的那几个字。
//
// ⚠️ **Label 不能省**：它不只是给测试用的——没有它读屏软件读不出这是一个搜
// 歌名的控件（占位文字不是标签），无头 Tester 也只认 label / text。
const collectionSearchLabel = "搜索歌名"

// viewCollectionSearch 是「按歌名找」，**一枚会展开的按钮**。
//
// 上游那一排的右边是 `SearchWithSort`（`components/search-with-sort/index.tsx`），
// 里面是「搜索 + 排序」两样；我们之前只抄了排序那一半，于是这一页**只能缩小
// 到「哪一档排序」，缩不到「某几首」**——一个 800 首歌的收藏夹里，用户只能靠
// 滚和看。后来补上了搜索，但补成了一个**常驻 180 的输入框**；上游那个是收起
// 时一枚放大镜、点开才展开（`components/search-button/index.tsx`）。
//
// ⚠️ **筛是接口筛的**（`bili.CollectionMedias` 顶上那一段）：本地筛只能筛已经
// 拉下来的那 20 首，搜「告白气球」在第 300 首的话，界面上的表现是「搜了没反应」。
//
// ⚠️ **回车立刻搜，防抖 500ms**：抄上游（`components/search-button/index.tsx`
// 的 `useDebounceFn(..., { wait: 500 })`），也和这一页的邻居一致（`librarypage.go`
// 的 `searchDebounce`）。回车不等那 500ms——用户按下回车就是想现在搜。
func (s *Shell) viewCollectionSearch(c *ui.Context, p *collectionPage) {
	box := expandableSearch(c, s.tip, &p.search, &p.draft, collectionSearchLabel,
		collectionSearchMinWidth, collectionSearchWidth, s.pageJustShown())
	p.submitted = box.Submitted()

	// 判据和排序那一处一个写法：这一帧条件变了就换查询。
	if s.collectionSearchChanged(p) {
		s.reloadCollection(p)
	}
}

// collectionSearchChanged 报这一帧搜索条件变了没，变了就换查询。
//
// **防抖的钟在用户每敲一个字的时候重新打**：上一次敲的已经过去 400ms、这一下又
// 敲了一个字，那一下就得从头再等 500ms，而不是把上一个字的 400ms 接着算——
// 否则慢慢打的时候会在中途就发出去一个还没输完的词（上游 `useDebounceFn` 就是
// 每个输入都重置那个钟）。回车是例外，立刻换。
//
// 这一段和 `libraryPage.keywordChanged` 是同一套（理由见 librarypage.go 的文件头）。
func (s *Shell) collectionSearchChanged(p *collectionPage) bool {
	now := s.clock.Now()
	draft := strings.TrimSpace(p.draft)
	switch {
	case p.submitted:
		p.seenDraft = draft
		return s.applyCollectionKeyword(p, draft)
	case draft != p.seenDraft:
		// 用户又敲了一个字：把钟重打，这一帧不搜。
		p.seenDraft, p.pendingSince = draft, now
		return false
	case p.pendingSince.IsZero():
		return false
	case now.Sub(p.pendingSince) >= searchDebounce:
		return s.applyCollectionKeyword(p, draft)
	}
	return false
}

// applyCollectionKeyword 把生效的词换掉。
//
// **同一个词不重拉**：回车搜了「告白气球」再回车一次，B 站会给一模一样的
// 20 条，而界面上那一瞬间是空的 + 清掉选中项。
func (s *Shell) applyCollectionKeyword(p *collectionPage, kw string) bool {
	p.pendingSince = time.Time{}
	if kw == p.keyword {
		return false
	}
	p.keyword = kw
	return true
}

// collectionSearching 报「收藏夹页那个 500ms 的钟要不要醒着」。
//
// 它是 `syncSearchWatch` 的一个 `want`（理由见 librarypage.go 里那一个）：
// **不许为这一档再排一个定时器**——同一个「等用户把词敲完」在代码里出现两个
// 出处，改一个忘了另一个时界面上一点也看不出来。
//
// ⚠️ **离开这一页只是不醒那个钟，不是把那个「还没发出去的搜索」丢掉**：
// `draft` 里还留着用户敲的那个词，而列表还没筛——回来的时候那一帧它就发出去
// 了（那一帧 `now - pendingSince` 早过了 500ms）。**清掉它的话，用户再点开
// 那个搜索时框里就是空的**，而列表按着一个他看不见的词筛着。同一栏的邻居
// （`libraryPage`）也是这么做的。
func (s *Shell) collectionSearching() bool {
	p := s.collection
	if p == nil {
		return false
	}
	if !strings.HasPrefix(s.router.Path(), collectionPathPrefix) {
		return false
	}
	return !p.pendingSince.IsZero()
}
