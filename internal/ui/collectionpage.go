// 歌单内容页：Biu 的 `/collection/{id}`（收藏夹 / 合集 / 系列同一页）。
//
// # 这一页摆什么
//
//	头图区     封面、标题、简介、类型 + 条目数、创建者
//	操作条     播放全部 / 添加到播放列表 / 收藏星标 / ⋯ 菜单 / 排序下拉 / 多选开关
//	列表       `widgets/list` 的 PagedList（虚拟滚动 + 变高行 + 行身份）
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

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
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
// 换排序时整个换掉列表而不是换 fetcher，同一个道理（换 fetcher 还是闭包读
// 字段）。
func (s *Shell) newCollectionList(p *collectionPage) *list.PagedList {
	acts := s.collectionActions()
	ref, sort, media := p.ref, p.sort, &p.media
	return list.NewPaged(s.images, acts, func(ctx context.Context, pageNo int) (list.Page, error) {
		return s.fetchCollectionPage(ctx, ref, sort, pageNo, media)
	})
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
func (s *Shell) fetchCollectionPage(ctx context.Context, ref app.CollectionRef, sort bili.CollectionSort, pageNo int, media *mediaIndex) (list.Page, error) {
	medias, hasMore, err := s.app.LoadCollectionPage(ctx, ref, sort, pageNo)
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
		})
	}
	return list.Page{Songs: songs, HasMore: hasMore}, nil
}

// onlineItemOf 把一条 B 站内容翻成播放队列认的那一项。
//
// ⚠️ **合集（21）和电影（24）也给 Source="online"**，但 Type 空着：它们没有
// 音频流，而 `app.playableOnline` 对 Type 空的会回一句「这一首还不能放」，
// 那句话说的就是事实。给它们编一个 Type 的话错误信息会指向另一个原因
// （见 `app.playableOnline` 的注释）。
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

	ref := collectionRefOf(r)
	if !s.app.CollectionsMounted() {
		// 没挂接口时画得出来（头图区是空的、列表一直转圈），但要告诉用户
		// 为什么——「点了没反应」比「没做」更难解释。
		s.viewEmpty(c, "collection:not-mounted", emptyState{
			Title: "暂时打不开收藏夹",
			Hint:  "这一页要 B 站接口，它还没挂上",
		})
		return
	}

	p := s.collectionFor(ref)
	s.flushCollectionNotice(p)
	info := s.app.Collection(ref)

	ui.Column(c).Key("page:collection:" + ref.Key()).Grow(1).Children(func() {
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
		default:
			// 🔴 设置里的「数据显示样式」在这一帧生效（理由同搜索页那一处）。
			//
			// ⚠️ 它**换不掉**多选接线（`attachSelection` 装的那一份挂在两份控件上，
			// 见 `widgets/list/switch.go`），所以网格那一档点勾照样能批量删。
			p.list.SetView(s.displayView())
			// 列表自己占了 Grow(1)（见 `list.List.View`），所以多选态下面那条
			// 批量操作条自然被它顶到最下面。
			p.list.View(c)
			if p.multi {
				s.viewCollectionBatchBar(c, p)
			}
		}
	})
	s.viewCollectionConfirm(c, p)
}

// flushCollectionNotice 把后台攒下的话发成 Toast。
//
// **每帧开头调一次，而且取走**：取过就不会再说第二遍，不取的话同一句会在
// 每一帧都冒一次 Toast（见 `app.CollectionNotice`）。
func (s *Shell) flushCollectionNotice(p *collectionPage) {
	if n, ok := s.app.TakeCollectionNotice(p.ref); ok {
		s.addToast(n.Text)
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
	id := int64(0)
	if rest, ok := strings.CutPrefix(r.Path(), collectionPathPrefix); ok {
		// 后半截（真有了「合集里的某一集」那种子路径时）不参与识别：
		// 那一段是哪一条由下一页自己读，这里认的是容器本身。
		head, _, _ := strings.Cut(rest, "/")
		id, _ = strconv.ParseInt(head, 10, 64)
	}
	return app.CollectionRef{ID: id, Type: bili.ParseCollectionType(r.Query(collectionTypeQuery))}
}

// ── 头图区 ─────────────────────────────────────────────────────────────────

// 头图区那几个尺寸。照 Biu 的 `header/index.tsx`：封面 200×168（不是方的）。
const (
	collectionCoverW = 200
	collectionCoverH = 168
)

// viewCollectionHeader 是顶部头图区：封面 + 标题 + 简介 + 类型和条数 + 创建者。
//
// 文案和排列照抄 `src/pages/video-collection/header/index.tsx:53-113`。
func (s *Shell) viewCollectionHeader(c *ui.Context, p *collectionPage) {
	t := c.Theme()
	info := s.app.Collection(p.ref)

	ui.Row(c).Key("collection-header").Gap(16).Padding(20, 24, 8).
		Children(func() {
			// ① 封面。空地址时 imgcache 画空框（没给封面的收藏夹真的有）。
			imgcache.View(c, s.images, info.Cover, collectionCoverW, collectionCoverH)

			ui.Column(c).Grow(1).AlignItems(ui.Start).Gap(6).Children(func() {
				ui.Text(c, collectionTitleText(info)).FontSize(22).Bold().
					MaxLines(2).Ellipsis("…")

				// ② 简介。Biu 是 `line-clamp-1`，有才画。
				if info.Intro != "" {
					ui.Text(c, info.Intro).FontSize(13).TextColor(t.TextMuted).
						MaxLines(1).Ellipsis("…")
				}

				// ③ 类型 + 条目数。Biu 写的是「{公开|私密}收藏夹 • N 条视频」
				//（`header/index.tsx:88-96`）——**条目数永远说「条视频」**，
				// 上游连音频也是这么写的，这里照抄。
				ui.Text(c, collectionKindText(p.ref.Type, info.Private)+" • "+
					strconv.Itoa(info.MediaCount)+" 条视频").
					FontSize(12).TextColor(t.TextMuted).MaxLines(1)

				// ④ 创建者。Biu 是一张头像加昵称；这一票没接头像地址
				//（`bili.CollectionInfo` 有 `Owner.Face`，界面先不画——
				// 一行字能说清是谁，一个空方块不能）。
				if info.OwnerName != "" {
					ui.Text(c, info.OwnerName).FontSize(12).TextColor(t.TextMuted).
						MaxLines(1).Ellipsis("…")
				}
			})
		})
}

// collectionTitleText 是头图区那个大标题。
//
// ⚠️ 还在拉的时候写「正在加载…」而不是空串：空标题会在那儿留一个空的 h1，
// 用户会以为这个收藏夹真的没名字。
func collectionTitleText(info app.CollectionInfo) string {
	switch {
	case !info.Loaded:
		return "正在加载…"
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

// viewCollectionOps 是头图区下面那一排：左边四个动作，右边多选开关 + 排序。
//
// 项和顺序照 Biu 的 `src/pages/video-collection/operation/index.tsx:33-56`。
func (s *Shell) viewCollectionOps(c *ui.Context, p *collectionPage) {
	t := c.Theme()

	ui.Row(c).Key("collection-ops").Center().Gap(8).Padding(0, 24, 12).
		Children(func() {
			if ui.PrimaryButton(c, "播放全部").Clicked() {
				s.playWholeCollection(p)
			}
			if ui.Button(c, "添加到播放列表").Clicked() {
				s.addWholeCollectionToQueue(p)
			}
			s.viewCollectionStar(c, p)
			s.viewCollectionMore(c, p)

			ui.Spacer(c)

			// 多选开关。多选态开着时这一排还在，只是列表左边多了一列勾。
			multi := ui.Button(c, "多选")
			if p.multi {
				multi = multi.Background(t.SurfaceHover)
			}
			if multi.Clicked() {
				s.toggleCollectionMulti(p)
			}

			s.viewCollectionSort(c, p)
		})
}

// viewCollectionStar 是收藏 / 取消收藏那个星标。
//
// Biu 是实心 / 空心两个图标（`operation/fav-toggle.tsx:12-20`）——MyGo 没带
// 图标集，拿一个字代替：★ 收藏着、☆ 没收藏着。
//
// ⚠️ Biu 那一侧对「视频系列」和「自己创建的收藏夹」不画这个按钮
// （`operation/index.tsx:47-49`）。这一页拿不到「是不是自己创建的」
// （`fav_state` 说的是这个容器有没有被别人收藏），所以两种都画——
// 让服务端拒绝比在界面上猜一个条件准。
func (s *Shell) viewCollectionStar(c *ui.Context, p *collectionPage) {
	info := s.app.Collection(p.ref)
	mark, tip := "☆", "收藏这个收藏夹"
	if info.Faved {
		mark, tip = "★", "取消收藏"
	}
	if ui.Button(c, mark).Tooltip(tip).Label(tip).Clicked() {
		s.setCollectionFav(p, !info.Faved)
	}
}

// viewCollectionMore 是那个 ⋯ 菜单。
//
// 项照 Biu 的 `operation/menu.tsx:37-100`：下载全部音频 / 下载全部视频 /
// 清除失效内容 / 删除。「下载」两条走的是同一个接口（B 站给的是同一条音频
// 流，「音频 / 视频」是**下载器**那边的分别），所以现在它们是同一条路。
func (s *Shell) viewCollectionMore(c *ui.Context, p *collectionPage) {
	ui.MenuButton(c, "更多", func(m *ui.Menu) {
		for _, item := range collectionMoreItems {
			if m.Item(item.label).Chosen() {
				s.collectionMoreAction(p, item)
				return
			}
		}
	}).Label("更多操作")
}

// collectionMoreItems 是 ⋯ 菜单里的项。文案照 Biu。
var collectionMoreItems = []collectionMenuItem{
	{key: "download-audio", label: "下载全部音频"},
	{key: "download-video", label: "下载全部视频"},
	{key: "clear-invalid", label: "清除失效内容"},
	{key: "delete", label: "删除"},
}

// collectionMoreAction 是 ⋯ 菜单里那一项被选中之后做的事。
func (s *Shell) collectionMoreAction(p *collectionPage, item collectionMenuItem) {
	switch item.key {
	case "download-audio", "download-video":
		// 「全部」也要问一次：它是这一票的验收项，而且下载会占磁盘和带宽，
		// 按错一次就是几百兆白下。
		s.askConfirm(p, confirmDownload, p.list.List().Len(), false)
	case "clear-invalid":
		// ⚠️ 真做这一条要靠 attr 认出「失效」，而 attr 在 `bili.Media` 里有
		// （Attr 字段）。这里没接是因为它还要一次「拉全量再逐个删」——
		// 半做（只删掉已经加载出来的那几条失效的）比不做更糟：用户会以为
		// 收藏夹干净了，而翻到没加载的那几页还有一堆。
		s.addToast("「清除失效内容」还没接收藏夹接口")
	default:
		s.addToast("「" + item.label + "」还没接收藏夹接口")
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

	trigger := sel.Trigger.Padding(4, 10).Radius(t.Radius).Border(1, t.Border)
	trigger.Children(func() {
		ui.Text(c, p.sort.Label()).SingleLine()
		ui.Text(c, " ▾").TextColor(t.TextMuted)
	})
	trigger.Label("排序：" + p.sort.Label())

	sel.Popup(func(panel ui.Element) {
		panel.Padding(4).Radius(t.Radius).Background(t.Background).Border(1, t.Border)
		for _, sort := range bili.CollectionSorts {
			item := sel.Item(sort).Padding(6, 10).Radius(t.Radius)
			switch {
			case item.Highlighted():
				item.Background(t.Accent).TextColor(t.AccentText)
			case sort == p.sort:
				item.Background(t.Surface)
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
