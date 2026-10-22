// 首页：推荐音乐（Biu 的 `/`，`src/pages/music-recommend/`）。
//
// ## 这一版摆什么
//
// 三个 Tab（音乐 / 鬼畜 / 流行）+ 新歌速递那一块横向网格。**新歌速递只画在
// 「流行」Tab 上**（上游 `index.tsx:295` 的 `{activeTab === "pop" && <NewMusicTop …
// />}`），另外两个 Tab 上没有它。
//
// 三档的内容是**接上 B 站的推荐接口**之后拉回来的（取数那一摊在
// homepage_feed.go，服务层在 `service/bili/recommend.go`）。**没登录时照拉**：
// 三个接口不登录都给公开的那一批（分区推荐给的是热门，音乐榜和新歌速递本来
// 就是公开榜），所以这一页不因为没登录就换成一句「请先登录」——理由和那一
// 票写下来的取舍见 `app/recommend.go` 的「未登录时显示什么」那一段。
//
// 一档都还没拉到、**而且也没有失败**的时候（刚拉完 B 站给了空的，或者压根
// 没挂上 B 站客户端），画的是 `routes` 表里 `/` 那一条的空状态（`viewEmpty`），
// 不是三个列表各自那句「这里还没有歌」——那句话只说这一屏是空的，没说怎么
// 才能不空，而 `routes` 表里这一条两句话都在。
//
// 🔴 铁律二：这一页的 view **不许**把 `SetTheme` 放到别的地方调，见
// `viewHome` 开头那一行和 theme.go。
//
// 🔴 页面级的 recover 在 `router.go` 的 `Shell.page` 上（和其余 14 条路由共用
// 那一道 Guard），这里不用再包一层：view 里 panic 会一路穿出 Cocoa 事件循环把
// 整个进程带走，这一页画的就是那条路上的一环。
package ui

import (
	"strconv"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/egoist/mygo/ui"
)

// homePath 是首页那条路由，和 routes 表里登记的是同一个字符串。
//
// 拿来和 `Route.Match` 比，而不是拿 `routes[0]`：那条表一改顺序，这里跟着错，
// 而且错得很安静——首页退回空状态页，没人看得出来（和 settings_page.go 的
// `settingsPath` 同一个理由）。
const homePath = "/"

// homeTabCount 是 Tab 的个数。
//
// ⚠️ 用它定数组长度、名字表也是同一份长度（`homeTabs`），不是两处各写一个数：
// 少写一个 Tab 而数组还按三个走，缺的那一屏画的是 nil 指针。Biu 那三个 Tab 是
// 写死的（`index.tsx` 的 `RecommendTabKey` 只有 music / guichu / pop），这里
// 跟着写死。
const homeTabCount = 3

// homeTabs 是那三个 Tab 的名字，从左到右。
//
// 顺序和字面都照 Biu 的 `src/pages/music-recommend/index.tsx:280-282`
// （`<Tab key="music" title="音乐" />`、`guichu` / `鬼畜`、`pop` / `流行`）。
// 顺序换了，用户每次开应用先看到的那一屏就跟着变了，而那是 Biu 的默认 Tab。
var homeTabs = [homeTabCount]string{"音乐", "鬼畜", "流行"}

// homePopTab 是「流行」那一档的下标，新歌速递只画在这里。
//
// 上游：`index.tsx:295`。另外两个 Tab 上没有那一块，不是我们省了——Biu 的新歌
// 速递走的是音乐榜（`pop` 那一档的接口是 `getMusicComprehensiveWebRank`），
// 「音乐」和「鬼畜」走的是分区推荐（`getRegionFeedRcmd`），榜上没有的东西它
// 没有。
const homePopTab = 2

// homeSongs 是首页这一屏要画的两摊歌。
//
// 歌是 `homePage.feed` 拉回来的（见 homepage_feed.go），外部塞进来的走
// `SetSongs`（测试和还没接上的环境用）。
type homeSongs struct {
	// tabs 是三个 Tab 各自的推荐单，下标对着 homeTabs。
	tabs [homeTabCount][]list.Music
	// new 是「新歌速递」那一摊，只在 homePopTab 上画。
	new []list.Music
}

// homePage 是首页这一屏自己的状态。建一次，每一帧调 View。
//
// 和 widgets/list 里的 List 同一个道理：**建一次存着，不要每一帧新建**，否则
// 滚动位置和选中项每帧都从头开始（`ui.ListState` 的注释：「Give each List a
// ListState of its own; set its fields before building the List」）。
type homePage struct {
	// acts 是这一屏会做的操作：双击、右键菜单、点一下新歌速递的那一格。
	acts list.Actions

	// lists 是三个 Tab 各自的列表。**滚动位置就跟着这三个对象走**，见文件头
	// 下面「切 Tab 的滚动位置」那一段。
	//
	// ⚠️ 是 `SwitchView` 而不是 `list.List`：设置里的「数据显示样式」要能在
	// 网格 / 紧凑 / 列表之间换，而网格那一档是另一个控件（`widgets/list/switch.go`
	// 顶上写明了为什么不是让 `List` 自己画）。三个 Tab 各有一份，切档时
	// **每个 Tab 各自记着自己停在哪**。
	lists [homeTabCount]*list.SwitchView

	// newGrid 是新歌速递那一块横向网格，见 homepage_newmusic.go。
	newGrid *newMusicGrid

	// songs 是这一屏要画的歌，由 SetSongs 换。三个列表和那一块网格都读它。
	songs homeSongs

	// feed 是四摊取数的状态（三个 Tab + 新歌速递）和那个信箱，见
	// homepage_feed.go。
	feed homeFeed

	// loads 是「去拉」的那几个活儿（接线在 Shell 那一层，见 homepage_feed.go
	// 的 homeLoads）。
	loads homeLoads

	// tab 是现在选中的那个 Tab，从 0 起，对着 homeTabs。
	//
	// ⚠️ 它就是 `ui.Tabs` 直接写的那一个 int——框架把点中的下标写进来，我们不再
	// 另记一份，也就不会有「两处说的不是同一个」那种错。
	tab int
}

// newHomePage 建首页这一摊状态。
//
// cache 是封面缓存（整个窗口共用一个，Shell 建好之后传进来），acts 是操作。
// 两者都要传，理由和 list.New 一样：封面走 imgcache.View，操作走双击和右键
// 菜单，缺一个这一屏就只能看不能动。
//
// wake 是「要一帧新画面」的动作：新歌速递那一块自己包了一层 recover，崩了靠它
// 要下一帧（那一帧只建了一半，画出来是半张，见 guard 包的注释）。
func newHomePage(cache *imgcache.Cache, acts list.Actions, wake func()) *homePage {
	h := &homePage{acts: acts, newGrid: newNewMusic(cache, acts, wake)}
	// 新歌速递的「重试」回到这一页：那一块自己管不着取数（它只管画），
	// 而重试要能重新拉那一摊。
	h.newGrid.SetRetry(func() { h.retryNew() })
	for i := range h.lists {
		l := list.NewSwitchView(cache, acts)
		// 空状态**不在这里说**：一句都没接上时是**这一页**的空状态（routes 表里
		// `/` 那一条，见 viewHome）。让三个列表都说一遍自己那句「这里还没有歌」，
		// 叠在那一页的空状态上就是两句话在讲同一件事。
		l.SetEmpty("")
		// 播放量那一列：首页三个 Tab 的接口都带着它（`RecommendItem.View`）。
		//
		// ⚠️ **投稿时间不画**：上游首页自己就是 `hidePubTime`
		// （`pages/music-recommend/list.tsx:49` 与 `:60`），而它那一层的
		// `RecommendItem` 也没把 `pubdate` 带上来——照它。
		l.SetColumns(list.Columns{PlayCount: true})
		// ⚠️ **这里不挂 `ScrollState`**：给一个 `List` 挂上它会和它自己按行记的
		// 位置**打架**——TrackScroll 每一帧按那个像素数把容器摆过去，而
		// `ui.ListState` 记的是「第几行 + 那一行离顶多远」，两个数换算的那一帧里
		// 锚点会被拽偏。实测：同一个列表滚到第 19 首、切走再切回来，挂上
		// ScrollState 时回到第 6 首（Y 从 1200 变成 359）。
		//
		// 「回顶按钮」要的「滚过多少像素」不靠它：列表那一档的行高是**定死的 64**，
		// 「顶上那一行是第几行 × 64」就是滚过的像素数（`list.List.ScrollOffset`）。
		h.lists[i] = l
	}
	return h
}

// SetSongs 换掉首页这一屏的歌。三个 Tab 的列表和新歌速递那一块一起换。
//
// ⚠️ **不碰滚动位置**：滚动是那一屏的事，换歌不该把用户推回顶上。`list.List`
// 自己的 `SetSongs` 也是这个规矩——行身份跟着 ID 走（铁律四），用户盯着的那一
// 首还是那一首。重新拉一个 Tab 的第一页时该推回顶上，那一条留给接接口的那一
// 张（`List.ScrollTo(0, ui.Start)` 一句就够，见 widgets/list 的 List.ScrollTo）。
//
// ⚠️ 只能由界面线程调（就是跑 view 的那个线程）：它改的正是 view 每帧读的那份
// 状态，后台投递结果回来之后要落地也得靠 `ui.Services.Invalidate()` 先叫醒界面。
func (h *homePage) SetSongs(songs homeSongs) {
	h.songs = songs
	for i, l := range h.lists {
		l.SetSongs(songs.tabs[i])
	}
	h.newGrid.SetSongs(songs.new)
}

// viewHome 画首页。页面的那一道 recover 在 `Shell.page` 上（router.go）。
//
// 🔴 铁律二：页面 view 的头一件正事就是装主题，而且必须在最开头——SetTheme 只
// 影响它之后创建的控件。不装的话 MyGo 在帧开头重置好的跟随系统那份就是这一帧
// 真正用的那份，用户选了深色也只在这一帧是对的。
//
// ⚠️ **这一页没有页标题，而这是刻意的**（票 #127）。别的每一页都自己写着名字
// （设置 / 下载记录 / 历史记录…），而这一页没有——
//
//   - **上游的推荐页也没有**：Biu 那个 `src/pages/music-recommend/` 里唯一的
//     `<h1>` 在「新歌速递」那一块里面（`new-music-top.tsx:178`），页面本身
//     没有大标题。上游顶栏也没有页名（票 #127 把我们顶栏那一处去掉了）。
//   - **「我在哪」由侧栏说**：侧栏菜单里那一项「推荐音乐」高亮着（`viewSideMenu`
//     的 `flatCurrent`），而上游也是靠侧栏高亮说这一件事。
//
// ⚠️ **别为了「看着完整」给它补一个大标题**：那是一个上游没有的新元素，而且
// 这一页顶上已经有三个 Tab 了。真的想要一个页名，走**侧栏那一项**。
func (s *Shell) viewHome(c *ui.Context, _ *ui.Route) {
	// 记一笔「首页建了一遍」——给「一帧建了几遍」那条测试数（见 builds.go）。
	s.builds.home++
	s.applyFrameTheme(c)
	s.home.view(c, s)
}

// syncHome 是首页每帧的副作用：收后台投回来的结果、起该起的那一拉、装上设置里
// 那个「数据显示样式」。
//
// ⚠️ **它们原来在 `homePage.view` 的最开头**，而首页在满屏开着的时候不画
// （票 #161）——不挪的话，用户在满屏里看频谱的那几分钟里，首页的后台结果一直
// 落在信箱里没人取，关掉满屏之后要再等一趟才补上。
func (s *Shell) syncHome(c *ui.Context) {
	h := s.home
	// 先收后台投回来的结果，再发该发的那一摊（画之前发：画完再发的话那一帧画
	// 的是「正在加载」，要等这一页回来才换掉）。
	h.adopt()
	h.pull()

	// 🔴 设置里的「数据显示样式」在**画之前**装给这一屏的三个列表：忘了这一句的
	// 症状是「设置改了、首页没变」——而首页是用户打开应用看到的第一页，最先发现
	// 「这个设置没用」的就是这儿。
	view := s.displayView()
	for _, l := range h.lists {
		l.SetView(view)
	}
}

// view 画首页的内容。
//
// 摆法是「一列三块」：Tab 那一排 →（只在流行上）新歌速递 → 选中那一屏的列表。
// 下面那一块给了 Grow(1) 的是列表，`list.List` 自己就占了 Grow(1)（见
// widgets/list 的 List.View），所以它拿走了剩下的高度。
//
// 头两句是取数：**先收**后台投回来的结果（铁律三：不收的话这一帧画的是
// 旧内容，而唤醒只有「结果到了」那一次），**再发**该发的那一摊（画之前
// 发：画完再发的话这一帧画的是「正在加载」，要等这一页回来才换掉）。
// 两句的规矩见 homepage_feed.go 的文件头。
//
// s 是拿来画这一页的空状态和失败状态的（`viewEmpty` / `viewLoadError`）。
//
// ⚠️ **取数不在这里**（`adopt` / `pull` / 装「数据显示样式」那三样）：它们挪到
// `syncHome` 那一层了（票 #161）——满屏开着的时候这一页不画，而那三样是「每帧
// 都要跑」的副作用，停在画的那一半里就等于停更。
func (h *homePage) view(c *ui.Context, s *Shell) {
	// ⚠️ **横向那一格用 `pagePadX`**（七页共用一个数，见 pagerhythm.go）。
	ui.Column(c).Key("page:"+homePath).Grow(1).Padding(0, pagePadX).Gap(12).
		AlignItems(ui.Stretch).Children(func() {
		h.viewTabs(c)

		// 新歌速递只画在「流行」这一屏的顶部，别的地方一律不画（上游
		// index.tsx:295）。它自己那三态（拉取中 / 拉不到 / 正常）是它内部
		// 的事，这里只把这一摊的状态递过去。
		if h.tab == homePopTab {
			h.newGrid.SetStatus(h.feed.loadingOf(homeNewMusic), h.feed.newTab.err)
			h.newGrid.View(c)
		}

		h.viewTabBody(c, s)
	})
}

// viewTabBody 画选中那一档的内容。四种状态四种样子。
//
// ⚠️ **「没拉到」和「没有内容」是两件事，界面上必须分开**：
//
//	正在拉   转圈（上游 index.tsx:284-288 的 Spinner）
//	拉失败   一句原因 + 一个「重试」
//	真的没有 routes 表里 `/` 那一条的空状态
//	有歌     列表
//
// 把「拉失败」画成「没有内容」，用户读到的是「B 站没有推荐给我」，
// 于是他永远不会去点那个本该在那儿的「重试」——而 B 站的风控、断网、
// 登录态过期全都会走到那一支。
func (h *homePage) viewTabBody(c *ui.Context, s *Shell) {
	songs := h.songs.tabs[h.tab]
	err := h.feed.stateOf(h.tab).err
	switch {
	case err != nil && len(songs) == 0:
		// ⚠️ `len(songs) == 0` 那一半：手上已经有歌的时候不画失败（那会把
		// 用户正在看的一屏换成一句错）。首页每档只拉一次，所以「有歌又失败」
		// 只可能出现在一次失败的重试之后。
		//
		// 用共用的 `viewLoadError`（不是 `viewEmpty` 的一个空状态），理由在
		// empty.go 那一段：失败必须有重试按钮，而且不能被「暂无内容」盖住。
		tab := h.tab
		s.viewLoadError(c, "home:fail:"+homeTabs[tab], homeTabs[tab]+"推荐", "重试加载"+homeTabs[tab]+"推荐",
			err, func() { h.retryTab(tab) })
	case err == nil && h.feed.loadingOf(h.tab) && len(songs) == 0:
		// ⚠️ 走**共用**的那一格（`drawLoading`）：上游首页那一屏也是
		// `h-[40vh]` 那一格里的转圈（`src/pages/music-recommend/index.tsx:319`），
		// 而这一票把其余几页也统一成那一格（票 #178，见 `loading.go`）。
		drawLoading(c, "home:loading", "正在加载"+homeTabs[h.tab]+"推荐")
	case len(songs) == 0:
		// ⚠️ 走 `viewEmpty`（`routes` 表里 `/` 那一条，出处和其余 13 条写在
		// 一起），不是让 `list.List` 说它自己那句「这里还没有歌」：那句只说
		// 这一屏是空的，那一页的两句话连着说了「为什么空」和「怎么才能不空」。
		s.viewEmpty(c, "home:empty", lookup(homePath).Empty)
	default:
		// ⚠️ **表头在列表外面**（`rowheader.go` 文件头）：它是 `View` 的兄弟，所以列表往
		// 下滚它不动，宽屏下滚到右边列名也还在。
		h.lists[h.tab].Header(c, "home:header:"+strconv.Itoa(h.tab))
		h.lists[h.tab].View(c)
		s.noteScrollbar(h.lists[h.tab].Hot())

		// 「回到顶部」那一枚。滚过 400 像素才出现（上游的数，见
		// `widgets/list` 的 `BackToTop`）。
		//
		// ⚠️ **给的是当前这一档的那一份位置**（`ScrollOffset`）：给错了的话按钮会在
		// 没滚过的页面上冒出来。
		list.BackToTop(c, h.lists[h.tab].ScrollOffset(), h.scrollTop)
	}
}

// scrollTop 回到顶上。
//
// ⚠️ **回的是列表自己的行锚点**（`ListState.ScrollTo`）：这一页的位置由三个
// `ListState` 各自记着（见文件末尾「切 Tab 的滚动位置」那一段），而回顶按钮的
// 位置是**按行算**出来的（`ScrollOffset`）——不用另有一份像素要归零。
func (h *homePage) scrollTop() {
	h.lists[h.tab].ScrollTo(0, ui.Start)
}

// viewTabs 画那三个 Tab。
//
// ⚠️ Biu 把 Tab 那一排和右边一个「全部播放」按钮放在同一个 `justify-between`
// 的行里（`index.tsx:262-294`）。按钮**这一版不摆**：它是 `addList`（往播放
// 列表里**追加**，不换队列也不起播，Biu 的 `handlePlayAll` 就是这么写的），而
// 我们的队列只有「从这一首往后播」和「追加到末尾」（`app.Queue` 到今天只有
// 那两条路）。要摆它就得先决定「全部播放」到底是哪一种——而那是另一个决定，
// 不该在接数据源这一票里顺手定下来。摆一个连语义都还没定的按钮不如不摆。
func (h *homePage) viewTabs(c *ui.Context) {
	ui.Row(c).Key("home:tabbar").FillWidth().AlignItems(ui.Center).Children(func() {
		// 🔴 Key 要写在 Tab 的构造**之前**：Tab 里面是个 List，它的状态在
		// 构造的时候就已经读过了，构造之后再 `.Key()` 会 panic（`ui/element.go`
		// 的 Key 注释：自带输入的控件都不能构造后 Key，只能构造前 `Context.Key`）。
		// 中间的语句会把它抢走，所以这两句要紧挨着。
		c.Key("home:tabs")
		// ⚠️ **主 Tab 用轨道 + 滑块那一种**（`mainTabs`，见 tabs.go）：这一排
		// 和次级排序那几排不是同一个样子（票 #138）。
		//
		// 首页这一排上游是 `size="lg"`（44），其余三处是 md（40）。
		mainTabs(c, &h.tab, tabLG, homeTabs[:]...)
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 切 Tab 的滚动位置
// ─────────────────────────────────────────────────────────────────────────────
//
// 「切回某个 Tab 时它还在原来的位置」是这么来的，**没有一行存-取-还的代码**：
//
// 每个 Tab 一个 `list.List`，滚动位置就跟着那一个对象走。`ui.ListState` 把
// 「顶上那一行是第几行、它离列表顶多远、每一行量出来多高」全记在它自己的字段里
// （`mygo/ui/list.go` 的 ListState：「The place: the first row in view, and how
// far the list is scrolled past its top」），而它由 `list.List` 自己拿着。切 Tab
// 时换掉的只是「画哪一个 `*list.List`」，用户看到的就回到那一屏。
//
// 为什么不能三个 Tab 共用一个 `list.List`：那样切回时同一个 `ui.ListState` 里
// 顶上那一行是**另一个 Tab 的第几行**，而顶上那首歌会随换 Tab 换掉——用户滚到
// 第 17 首、切去「鬼畜」、再切回来，看到的是「鬼畜」的第 17 首，或者干脆回到
// 顶上。`TestSwitchingBackRestoresThatTabsScrollPosition` 钉的就是这一条。
//
// ⚠️ **别拿 `ui.ScrollState` 去「存像素、切回来再 ScrollTo 回去」**：那是在已经
// 记对了的锚点上抢方向盘。实测过一次——三个列表各挂一个 `ScrollState`
// （`List.SetScroll`），滚到第 19 首（Y=1200）、切走再切回来，Y 变成 359，顶上
// 那首变成第 6 首：TrackScroll 按像素把容器摆过去，`ListState` 按行把锚点算回
// 去，两个数换算的那一帧里锚点被拽偏了。不挂 `ScrollState`（`newHomePage` 里
// 特意不调 `SetScroll`）同一个用例一次就过。
//
// 真要按像素对外说（**回顶按钮**判断「滚过 400 像素没有」），**也别挂
// `ScrollState`**——挂了就撞上上面那一条。列表那一档的行高是**定死的 64**，
// 所以「顶上那一行是第几行 × 64」就是滚过的像素数，一个不差（见
// `list.List.ScrollOffset`）。网格那一档的行高跟着内容走，那一档是**估**的
// （见 `list.Grid.ScrollOffset`）。
