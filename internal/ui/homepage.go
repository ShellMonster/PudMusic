// 首页：推荐音乐（Biu 的 `/`，`src/pages/music-recommend/`）。
//
// ## 这一版摆什么
//
// 三个 Tab（音乐 / 鬼畜 / 流行）+ 选中那一档的列表。**新歌速递那一块只画在
// 「流行」Tab 上**（上游 `index.tsx:295` 的 `{activeTab === "pop" && <NewMusicTop …
// />}`），另外两个 Tab 上没有它。
//
// ⚠️ **新歌速递那一块是「流行」那一档列表的第 0 行**（`list.Lead`，页内头部）——
// 所以它在列表的滚动区里，**跟着内容一起滚走**，滚一段之后那几张图就不见了
// （上游那一页就是这么摆的：顶图和下面的列表在**同一个滚动容器**里）。此前它是
// 列表的**兄弟**（在滚动容器**外面**），于是滚动列表时它一动不动——票 #369 修的
// 就是这个。Tab 那一排仍在滚动容器**外面**（上游也是），滚列表时它不动。
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
// 🔴 **三档是三个翻页列表**（`list.PagedList`，票 #370）：滚到底自动拉下一段，
// 一直滑一直有（到 B 站没有更多为止），末尾那一行说「没有更多了」。翻页那一套
// （防重入、取消、旧结果作废、容器没填满自动续拉）**在那一层里**，界面层不
// 另写一份。
//
// ## 「正在加载」那一格什么时候才出现（票 #368）
//
// **只有屏幕上真的没内容可画的时候**。切到一档而它还在拉的时候，画面上留着
// **上一档**的内容——所以那一格轮到的次数，比以前少得多（见
// homepage_transition.go 的 `syncSwap`）。
//
// ## 切 Tab 有过渡（票 #368、票 #238）
//
// 内容是单独一个元素（`viewBody`），切 Tab 时它整个换掉，框架按
// `ElementTransition` 的 `Enter` 走一遍——新的那一块从下面 8 DIP 处升上来。
//
// ⚠️ **旧的先淡出、新的后淡进**（票 #238）：退场那一段是**旧的那一块自己的
// 不透明度**（框架的 `Exit` 那一半会复制一份上一帧的样子，那一份要重排旧
// 列表的文字、而且和新的那一块同时在画面上）。形式和那几个数写在
// homepage_transition.go 顶上。
//
// 🔴 铁律二：这一页的 view **不许**把 `SetTheme` 放到别的地方调，见
// `viewHome` 开头那一行和 theme.go。
//
// 🔴 页面级的 recover 在 `router.go` 的 `Shell.page` 上（和其余 14 条路由共用
// 那一道 Guard），这里不用再包一层：view 里 panic 会一路穿出 Cocoa 事件循环把
// 整个进程带走，这一页画的就是那条路上的一环。
package ui

import (
	"github.com/ShellMonster/Biu_Go/internal/app"
	"strconv"
	"strings"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
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
const homeTabCount = 6

// homeTabs 是那三个 Tab 的名字，从左到右。
//
// 顺序和字面都照 Biu 的 `src/pages/music-recommend/index.tsx:280-282`
// （`<Tab key="music" title="音乐" />`、`guichu` / `鬼畜`、`pop` / `流行`）。
// 顺序换了，用户每次开应用先看到的那一屏就跟着变了，而那是 Biu 的默认 Tab。
//
// 后三个是音乐发现那一组：热榜 / 原创榜（B 站音乐站每期的榜）和最近播放（登录账号
// 的观看历史）。**「为你推荐」就是原来的「音乐」那一档**：它拉的是音乐分区的个性化
// 推荐（B 站按登录账号的口味排），本来就是只含音乐的那一份；换名字是因为它的内容
// 是「给这个账号的」，而不是「所有人看到的音乐」。⚠️ 没有用 B 站首页的「每日推荐」：
// 那一份混着大量非音乐视频。
var homeTabs = [homeTabCount]string{"为你推荐", "鬼畜", "流行", "热榜", "原创榜", "最近播放"}

// homeTabNouns 是各档在「正在加载 X」「重试加载 X」「没拉出来的是：X」里的名词。
//
// 和 Tab 上的字分开：「为你推荐」那一档的这几句一直说「音乐推荐」（它拉的是音乐分区
// 推荐），而「热榜推荐」「最近播放推荐」读不通，榜和历史直接说它们自己。
var homeTabNouns = [homeTabCount]string{"音乐推荐", "鬼畜推荐", "流行推荐", "热榜", "原创榜", "最近播放"}

// homePopTab 是「流行」那一档的下标，新歌速递只画在这里。
//
// 上游：`index.tsx:295`。另外两个 Tab 上没有那一块，不是我们省了——Biu 的新歌
// 速递走的是音乐榜（`pop` 那一档的接口是 `getMusicComprehensiveWebRank`），
// 「音乐」和「鬼畜」走的是分区推荐（`getRegionFeedRcmd`），榜上没有的东西它
// 没有。
const homePopTab = 2

// homeRecentTab 是「最近播放」那一档的下标。它登录后才有内容，而且历史接口不给
// 播放量，所以那一列不画。
const homeRecentTab = 5

// homeHotTab / homeOriginalTab 是两张榜的下标：序号那一列在这两档上是名次。
const (
	homeHotTab      = 3
	homeOriginalTab = 4
)

// homeSongs 是首页这一屏要画的两摊歌。
//
// ⚠️ **它是「外面塞进来的一批歌」的格式，不是这一屏此刻的状态**：真正在画面上
// 的是三个 `list.PagedList` 自己手里那一份（翻页往尾部追加），而这一份只在
// `SetSongs` 那一刻被搬进那三个列表；新歌速递那一摊则是这一份管着的。
//
// 歌是拉回来的（取数那一摊在 homepage_feed.go），外部塞进来的走 `SetSongs`
// （测试和还没接上的环境用）。
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

	// lists 是三个 Tab 各自的**翻页列表**。**滚动位置就跟着这三个对象走**，见
	// 文件头下面「切 Tab 的滚动位置」那一段。
	//
	// ⚠️ 是 `list.PagedList` 而不是 `list.List`：滚到底要自动拉下一段（票 #370），
	// 而那一套（防重入、取消、旧结果作废、容器没填满自动续拉）就在这一层里，
	// 别在界面层另写一套。
	//
	// ⚠️ **每个 Tab 各有一份**，所以「切回某个 Tab 还在它原来的位置」是天然的
	// （每一份的滚动位置跟着它自己那个 `ui.ListState` 走）——理由见文件末尾
	// 那一段。
	lists [homeTabCount]*list.PagedList

	// newGrid 是新歌速递那一块横向网格，见 homepage_newmusic.go。
	newGrid *newMusicGrid

	// feed 是新歌速递那一摊的取数状态和那个信箱，见 homepage_feed.go。
	feed homeFeed

	// loads 是「去拉」的那几个活儿（接线在 Shell 那一层，见 homepage_feed.go
	// 的 homeLoads）。
	loads homeLoads

	// tab 是现在选中的那个 Tab，从 0 起，对着 homeTabs。
	//
	// ⚠️ 它就是 `ui.Tabs` 直接写的那一个 int——框架把点中的下标写进来，我们不再
	// 另记一份，也就不会有「两处说的不是同一个」那种错。
	tab int

	// shown 是「此刻画面上画的是哪一档的内容」（票 #368）。
	//
	// ⚠️ **它和 tab 不是同一件事**：切到一档而它还在拉的时候，画面上留着**上一
	// 档**的内容（那就是「流行切进去不闪」那一条，见 homepage_transition.go 的
	// `syncSwap`）。它只在那一档有着落了之后才跟上 `tab`——而**跟着它的那个动
	// 作要分两段**（旧的先淡出、新的后淡进，见 `syncSwap`）。
	shown int

	// swap 是切 Tab 那一段的状态：现在处在哪一段、退到哪一档、这一刻有多透明。
	//
	// ⚠️ **它和 `shown` 是一对**：`shown` 是「画面上画的是哪一档」，`swap` 是
	// 「正在往哪一档换、换到哪一段了」。退场那一段里 `shown` 还是旧的那一档
	// （它在淡出），进了进场那一段才换成新的那一档。详见 homepage_transition.go。
	swap homeSwap

	// want 是「要换过去的那一档」，wantReady 是「它到了没有」。
	//
	// ⚠️ **这一对是 sync 层算的**（见 `syncHome`），**不是 view 层算的**：
	// view 层看到的 `tab` 是**这一帧**才写下的（框架把点中的下标写在画 Tab
	// 那一排的时候），而那一档的请求是**下一帧**才发出去的（`pull()` 在 sync
	// 层里跑）——在 view 层判断「它到了没有」，那一刻它还没开始拉，于是会以为
	// 「它已经有着落了」而立刻换过去，换过去之后屏幕上就是「正在加载 XXX 推
	// 荐」那一格（票 #368 明令不许出现的）。
	want      int
	wantReady bool

	// overview 为真时首页画「发现」总览（homepage_overview.go），不画 Tab 列表。
	overview bool
	// ovScroll / ovWidth 是总览的滚动位置和上一帧量到的内容宽。
	ovScroll ui.ScrollState
	// heroIdx / heroAt 是轮播横幅当前停在第几张、什么时候换上的。
	heroIdx int
	// newOff 是新歌速递那一排从第几首起摆（点左右箭头翻页）。
	newOff  int
	heroAt  time.Time
	ovWidth float32
	// miniTop 是两张榜（热榜、原创榜）的前几名；miniLoaded 记着取它时榜上有几首。
	miniTop    [2][]list.Music
	miniLoaded [2]int
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
	h := &homePage{acts: acts, newGrid: newNewMusic(cache, acts, wake), overview: homeStartsOnOverview}
	// 新歌速递的「重试」回到这一页：那一块自己管不着取数（它只管画），
	// 而重试要能重新拉那一摊。
	h.newGrid.SetRetry(func() { h.retryNew() })
	for i := range h.lists {
		l := list.NewPaged(cache, acts, h.tabFetcher(i))
		// 播放量那一列：首页六个 Tab 里，除了「最近播放」（历史接口不带播放量）
		// 的五个都带着它（`RecommendItem.View`）。
		//
		// ⚠️ **投稿时间不画**：上游首页自己就是 `hidePubTime`
		// （`pages/music-recommend/list.tsx:49` 与 `:60`），而它那一层的
		// `RecommendItem` 也没把 `pubdate` 带上来——照它。
		l.SetColumns(list.Columns{PlayCount: i != homeRecentTab, Rank: i == homeHotTab || i == homeOriginalTab,
			PubTime: i == homeRecentTab, PubTimeTitle: "观看时间"})
		// 「没有更多了」那一行（票 #370 的验收第 2 条）：别的页到底了就是到底了，
		// 而这一屏要说得清——不然用户只看到列表停在半截。
		l.SetEndView(homeEndRow)
		// ⚠️ **失败不另说一句**：这一屏自己有一整块失败态（一句人话 + 一个重试
		// 按钮），再说一遍是噪音，而那一句还带着 B 站的原始码。
		l.SetFailureNotice(false)
		// ⚠️ **三档落在不同的位置上**（票 #368 的内容块按档分了 key，见
		// homepage_transition.go 的 `homeBodyKeys`）：框架把滚动状态记在**元素的
		// 位置**上，而同一个位置轮流画的几份列表会**抢同一份**——切回「音乐」
		// 会落到第 6 首而不是它原来的第 19 首（实测）。
		//
		// 🔴 **不挂 ScrollState**（`list.PagedList.SetScrollState`）：挂着的时候
		// 滚动位置由它记，而它记的是「元素那个位置」的——于是上一档留下的偏移
		// 会拽走这一档（同一件事的另一面，见上面那一条）。
		//
		// 不挂它，那三档就各自按**自己的行锚点**记位置（`ui.ListState`，框架
		// 本来就是按行记的）——而回顶那一枚的偏移改按行换算（列表那一档的行高
		// 是定死的 64，差不到一行）。
		l.SetScrollState(false)
		h.lists[i] = l
	}

	// ⚠️ **「流行」那一档的顶图（新歌速递）是列表的第 0 行**（`list.Lead`，页内
	// 头部）——所以它**跟着列表一起滚走**，滚一段之后那几张图就不见了。
	//
	// 上游：`music-recommend/index.tsx:295` 的
	// `{activeTab === "pop" && <NewMusicTop … />}` 和下面的列表在**同一个滚动
	// 容器**里。此前我们把那一块摆在列表**外面**（它是列表的兄弟，在列表的滚动
	// 容器之外），于是滚动列表时它一动不动——票 #369 修的就是这个。
	//
	// ⚠️ **只有「流行」这一档装它**：另外两档上游本来就没有这一块（见
	// `homePopTab` 的注释），而「别动另外两档」是这一票的一条要求。
	h.lists[homePopTab].SetLead(&list.Lead{
		Draw:   h.viewPopLead,
		Label:  "新歌速递",
		Height: h.newGrid.Height,
	})
	h.lists[homeRecentTab].SetLead(&list.Lead{
		Draw:   h.viewRecentLead,
		Label:  "继续播放",
		Height: func() float32 { return homeRecentLeadH },
	})
	// 🔴 **两张榜的页头也进列表**（2026-10-27，用户要求「顶部改成跟着滚」）：
	// 原先榜单那张头图（`viewBoardCaption`）和表头都在列表**外面**，滚列表时
	// 它们钉在顶上不动——一屏只剩半屏能看歌。现在它们是列表的第 0 行，
	// 跟着内容一起滚走（和「流行」那一档的顶图、UP 空间那四张 Tab 的页内头部
	// 同一个做法，见 `list.Lead`）。
	for _, tab := range [2]int{homeHotTab, homeOriginalTab} {
		h.lists[tab].SetLead(&list.Lead{
			Draw:   func(c *ui.Context) { h.viewBoardLead(c, tab) },
			Label:  "榜单头图",
			Height: func() float32 { return homeBoardHeroH + 12 + 36 },
		})
	}
	return h
}

// viewBoardLead 是热榜 / 原创榜那一档的页内头部：**榜单头图 + 表头**。
//
// ⚠️ **表头也在里面**：头图说的是下面这些歌是哪张榜，表头说的是下面那些歌
// 叫什么列——两样都是「这一屏的上半」，滚下去就该一起走（同 `viewPopLead`）。
//
// 🔴 **两样各自判各自的**（2026-10-27 的评审）：「这一期是哪一期」那个元信息
// 和「下面那些歌」是**两趟**请求（`loads.board` 与翻页列表）。头图拿不到时把
// 表头也一起藏掉，用户看到的是**一屏没有列名的歌**——而列名和期数没有关系。
// 所以：
//
//	期数信息回来了 → 头图 + 表头
//	期数信息没回来 → 只有表头（那一块照旧钉在顶上这件事不影响：它在列表第 0 行，
//	                  只有一行表头，滚下去就没了）
func (h *homePage) viewBoardLead(c *ui.Context, tab int) {
	showHero := h.boardHeroVisible(tab)
	ui.Column(c).FillWidth().Gap(space.Px12).
		Padding(space.None, space.None, space.Px12).Children(func() {
		if showHero {
			h.viewBoardCaption(c, tab)
		}
		h.lists[tab].Header(c, "home:header:"+strconv.Itoa(tab))
	})
}

// boardHeroVisible 报这一档的榜单头图这一帧画不画。
//
// ⚠️ **判据和 `viewBoardCaption` 那两句早退逐条对齐**：头图那一块和这一句要是
// 各判一套，页内头部的高度就会和「实际画出来的东西」对不上（`Lead.Height` 是
// 一个估的数，见 `list.Lead`）。
func (h *homePage) boardHeroVisible(tab int) bool {
	if (tab != homeHotTab && tab != homeOriginalTab) || h.loads.board == nil {
		return false
	}
	n, at, ok := h.loads.board(tab)
	return ok && boardCaptionText(n, at) != ""
}

// homeEndRow 是末尾「没有更多了」那一行。
//
// ⚠️ **它画在列表里面**（末尾那一行就是哨兵那一行），所以它跟着列表一起滚——
// 停在列表的最下面，而不是浮在窗上。
func homeEndRow(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).AlignSelf(ui.Stretch).Center().Padding(space.Px8).Children(func() {
		ui.Text(c, list.EndLabel).FontSize(scale.Font(c, scale.Size13)).TextColor(t.TextMuted)
	})
}

// SetSongs 换掉首页这一屏的歌。三个 Tab 的列表和新歌速递那一块一起换。
//
// ⚠️ **不碰滚动位置**：滚动是那一屏的事，换歌不该把用户推回顶上。`list.List`
// 自己的 `SetSongs` 也是这个规矩——行身份跟着 ID 走（铁律四），用户盯着的那一
// 首还是那一首。
//
// ⚠️ **翻页那一层由它自己管**（`list.PagedList`）：装进去的这一批算「第一页」，
// 而「后面还有没有」由它下一次拉页说了算。
//
// ⚠️ 只能由界面线程调（就是跑 view 的那个线程）：它改的正是 view 每帧读的那份
// 状态，后台投递结果回来之后要落地也得靠 `ui.Services.Invalidate()` 先叫醒界面。
func (h *homePage) SetSongs(songs homeSongs) {
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
//
// ⚠️ **「要换到哪一档、它到了没有」在这里算**（`h.want` / `h.wantReady`，见下面
// 那三句）：这一层跑在 view 之前，而那一档的请求也是在这里发出去的——**只有
// 在这里才认得出「它还在拉」**（票 #368：「切到一档而它还在拉」时屏幕上留着
// 上一档，不出现「正在加载 XXX 推荐」那一格）。
//
// ⚠️ **切 Tab 那一段过渡本身不在这里**（票 #238）：它要看的是**这一帧的时刻**
// （框架的钟），所以它跟着画面走，在 view 那一半里（见 homepage_transition.go
// 的 `syncSwap`）。
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

	// 🔴 **只同步选中那一档**：另外两档等用户切过去再拉（那一档的 Sync 会
	// 替它拉第一页）。
	//
	// ⚠️ **这一句必须在画之前**：画的时候读到的是 `PagedList.Paged()`，而那是
	// **收完后台结果之后**的数——晚一步的话，这一帧画的是上一帧的状态（第一帧
	// 是「正在加载」，而其实歌已经回来了）。
	//
	// ⚠️ **选中那一档不一定画在画面上**（票 #368：切过去而它还在拉的时候，
	// 画面上留着上一档）——但**它得有人收结果、有人起第一拉**，所以同步的是
	// 它，而画出来的那一档由它自己的 `View` 同步（见 `PagedList.view`）。
	h.lists[h.tab].Sync(c)
	if h.overview {
		// 总览要用两张榜的前几名：没切到那两档也得有人替它们起第一拉。
		h.lists[homeHotTab].Sync(c)
		h.lists[homeOriginalTab].Sync(c)
		h.refreshMini()
	}

	// 算「要换到哪一档」和「它到了没有」。
	//
	// ⚠️ **这一层算，不在 view 里算**（票 #238 的评审指出来的）：sync 层跑在
	// view 之前，而框架把点中的下标写在**画 Tab 那一排**的时候（tabs.go）——
	// 所以此刻 `h.tab` 已经是用户刚点的那一档（上一帧写下的），而**上面那一句
	// `Sync`** 刚把那一档的请求发了出去（`tabPending` 于是看得见「还在拉」）。
	//
	// **「切到一档而它还在拉」就是在这儿认出来的**：在 view 里认的话，那一帧
	// 的请求**还没发出去**（还在拉还是假），于是会以为「它已经有着落了」
	// 而立刻换过去——而换过去之后，屏幕上就是「正在加载 XXX 推荐」那一格
	//（票 #368 明令不许出现的）。
	//
	// ⚠️ **还在拉就留着旧的那一档**（票 #368：「流行」切进去不闪）。
	h.want, h.wantReady = h.tab, !h.tabPending(h.tab)
}

// view 画首页的内容。
//
// 摆法是「一列两块」：Tab 那一排 → 内容那一块。内容那一块里面是那一档的列表。
//
// ⚠️ **内容是单独一个元素**（`viewBody`，票 #368）：切 Tab 时它整个换掉，
// 框架靠这个认出「旧的退了、新的进了」，于是有过渡；而它换的是**画面上那一
// 档**（`h.shown`），不一定是选中那一档（`h.tab`）——选中那一档还在拉的时候，
// 画面上留着上一档的内容（见 `syncSwap`）。
//
// 下面那一块给了 Grow(1) 的是列表，`list.List` 自己就占了 Grow(1)（见
// widgets/list 的 List.View），所以它拿走了剩下的高度。
//
// ⚠️ **「流行」那一档的顶图（新歌速递）不在这一列里**：它是**列表的第 0 行**
// （页内头部，见 `list.Lead`），所以它在列表的滚动区里——跟着内容一起滚走
// （上游那一页就是这么摆的，`music-recommend/index.tsx`）。
//
// s 是拿来画这一页的空状态和失败状态的（`viewEmpty` / `viewLoadError`）。
//
// ⚠️ **取数不在这里**（`adopt` / `pull` / 装「数据显示样式」那三样）：它们挪到
// `syncHome` 那一层了（票 #161）——满屏开着的时候这一页不画，而那三样是「每帧
// 都要跑」的副作用，停在画的那一半里就等于停更。
func (h *homePage) view(c *ui.Context, s *Shell) {
	if h.overview {
		ui.Column(c).Key("page:"+homePath).Grow(1).Padding(space.None, pagePadX).Gap(space.Px12).
			AlignItems(ui.Stretch).Children(func() {
			h.viewOverview(c, s)
		})
		return
	}
	// ⚠️ **横向那一格用 `pagePadX`**（七页共用一个数，见 pagerhythm.go）。
	ui.Column(c).Key("page:"+homePath).Grow(1).Padding(space.None, pagePadX).Gap(space.Px12).
		AlignItems(ui.Stretch).Children(func() {
		h.viewTabs(c, s)

		// ⚠️ **内容是单独一个元素**（`viewBody`，票 #368）：切 Tab 时它整个换掉，
		// 框架靠这个认出「旧的退了、新的进了」，于是有过渡；而它换的是**画面上
		// 那一档**（`h.shown`），不一定是选中那一档（`h.tab`）——选中那一档还在
		// 拉的时候，画面上留着上一档的内容（见 `syncShown`）。
		h.viewBody(c, s)
	})
}

// viewPopLead 画「流行」那一档的**页内头部**：新歌速递那一块 + 列表的表头。
//
// ⚠️ **它画在列表的滚动区里**（列表的第 0 行），所以它跟着内容一起滚走——
// 滚一段之后那几张图就不见了（上游那一页就是这么摆的：
// `music-recommend/index.tsx` 的 `<NewMusicTop/>` 和下面的列表在同一个滚动容器里）。
//
// ⚠️ **表头也在里面**：表头说的是**下面那些歌**，而新歌速递在它们**上面**——
// 表头要是留在列表外面（它在另外两档上就是外面，见 `rowheader.go` 的文件头），
// 它就会跑到新歌速递**上面**去，隔着一堆卡片说「下面这些是歌」。
//
// ⚠️ 代价是**这一档的表头会跟着滚走**（另外两档的表头是钉住的）：这一屏的上半
// 是「顶图 + 表头」这一整块，滚下去就该一起走——上游那一屏的上半也是这么滚的。
func (h *homePage) viewPopLead(c *ui.Context) {
	ui.Column(c).FillWidth().Gap(space.Px12).
		// ⚠️ 底下留一道 12 的缝：表头到第一首歌之间原来有那道缝（页面那一列的
		// `Gap`），表头搬进页内头部之后它得跟着走。
		Padding(space.None, space.None, space.Px12).
		Children(func() {
			h.newGrid.View(c)
			h.lists[homePopTab].Header(c, "home:header:"+strconv.Itoa(homePopTab))
		})
}

// withPopTop 画「顶图 + 下面那一块状态」。「流行」那一档的两样一起放进一个滚动
// 容器：新歌速递是两行大封面，窗口不高时状态那一块会被挤到窗外，滚动容器让它够得着。
// 别的档没有顶图，直接画状态。
func (h *homePage) withPopTop(c *ui.Context, tab int, body func()) {
	if tab != homePopTab {
		body()
		return
	}
	ui.Scroll(c).Key("home:pop:state").Grow(1).Children(func() {
		ui.Column(c).FillWidth().Gap(space.Px12).Children(func() {
			h.viewPopTop(c, tab)
			body()
		})
	})
}

// viewPopTop 画新歌速递那一块——**不在列表里**的那一次。
//
// ⚠️ **什么时候才走这里**：「流行」那一档的列表**一首歌都没有**的时候（拉失败 /
// 正在拉 / 真的没有）——那三种状态下压根没有列表可挂，它就自己站在上面，位置和
// 它挂在列表里时一样（都在 Tab 那一排下面）。
//
// ⚠️ **tab 是「画面上画的是哪一档」**，不一定等于选中那一档（票 #368，见
// `syncSwap`）——它和这一段里别的地方一样按 tab 取。
//
// ⚠️ **另外两档一律不画**：上游的新歌速递只在「流行」那一档（`index.tsx:295`）。
func (h *homePage) viewPopTop(c *ui.Context, tab int) {
	if tab != homePopTab {
		return
	}
	h.newGrid.View(c)
}

// viewTabBody 画**那一档**的内容。四种状态四种样子。

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
//
// ⚠️ **状态是翻页列表自己说的**（`list.PagedList.Paged()`）：拉页、防重入、
// 取消都在那一层里，而这一层只管「此刻画面上该是什么样子」。
//
// ⚠️ **tab 是「画面上画的是哪一档」，不一定等于选中那一档**（票 #368）：选中那
// 一档还在拉的时候，画面上画的是上一档（见 `syncSwap`）——所以这一段里
// 列表、表头、滚动条、回顶按钮**全都按 tab 取**，不能按 `h.tab` 取。
func (h *homePage) viewTabBody(c *ui.Context, s *Shell, tab int) {
	pg := h.lists[tab].Paged()
	switch {
	case tab == homeRecentTab && h.loads.mounted != nil && h.loads.mounted() && selfMidOf(h.loads) == 0:
		// ⚠️ **先于下面几支**：没登录时那一档一个请求都没发，「一首都没有」说成
		// 「暂无内容」会让用户以为 B 站没记着他听过什么。
		s.viewNeedsLogin(c, "home:recent:login", recentNotLoggedIn)
	case pg.Err != nil && pg.Loaded == 0:
		// ⚠️ `pg.Loaded == 0` 那一半：手上已经有歌的时候不画失败（那会把
		// 用户正在看的一屏换成一句错）。第一页失败之后「已经有歌」是不可能的，
		// 而后面那些页失败时失败的是**那一页**（末尾那一行有它自己的重试）。
		//
		// 用共用的 `viewLoadError`（不是 `viewEmpty` 的一个空状态），理由在
		// empty.go 那一段：失败必须有重试按钮，而且不能被「暂无内容」盖住。
		//
		// ⚠️ **顶图先画**：这一档没有列表可挂（列表一根歌都没有），而新歌速递
		// 那一块是**这一屏的内容**（上游 `index.tsx:295`），它不因为下面那一档
		// 没拉到就消失。
		h.withPopTop(c, tab, func() {
			s.viewLoadError(c, "home:fail:"+homeTabs[tab], homeTabNouns[tab], "重试加载"+homeTabNouns[tab],
				pg.Err, func() { h.retryTab(tab) })
		})
	case pg.Loading && pg.Loaded == 0:
		// ⚠️ 走**共用**的那一格（`drawLoading`）：上游首页那一屏也是
		// `h-[40vh]` 那一格里的转圈（`src/pages/music-recommend/index.tsx:319`），
		// 而这一票把其余几页也统一成那一格（票 #178，见 `loading.go`）。
		//
		// ⚠️ **这一格只在真的没内容可画时才出现**（票 #368）：切到一档而它还在
		// 拉的时候，画面上留着**上一档**的内容（见 `syncSwap`），所以这里轮到
		// 「正在加载」的时候，屏幕上确实一首歌都没有。
		//
		// ⚠️ **顶图先画**（理由同上面那一支）。
		h.withPopTop(c, tab, func() { drawLoading(c, "home:loading", "正在加载"+homeTabNouns[tab]) })
	case pg.Loaded == 0:
		// ⚠️ 走 `viewEmpty`（`routes` 表里 `/` 那一条，出处和其余 13 条写在
		// 一起），不是让 `list.List` 说它自己那句「这里还没有歌」：那句只说
		// 这一屏是空的，那一页的两句话连着说了「为什么空」和「怎么才能不空」。
		//
		// ⚠️ **顶图先画**（理由同上面那一支）。
		//
		// ⚠️ **这一格摆一个能点的下一步**：路由表里的 `Empty` 是包级静态数据，
		// 装不了闭包（`emptyAction.Do` 认得「这一个壳」），所以在这里把动作
		// 挂上——和 `downloadsEmptyState` 给「还没有下载目录」挂「去设置」
		// 是同一个做法。路由表里那句 Hint 写的是「打开文件…或者搜一首歌」，
		// 而 `emptyCopy` 的口径是「有能点的下一步就不画提示」（`empty.go`），
		// 按钮选的就是那句里的头一个动作；搜就走顶栏那一个框。
		// ⚠️ **不摆这一枚的话用户只能对着「暂无内容」猜**：未登录、或 B 站
		// 返回了空的那一档，这一格就是屏幕上唯一的东西。
		e := lookup(homePath).Empty
		e.Action = &emptyAction{Label: "打开文件", Do: s.pickAndEnqueue}
		if tab == homeRecentTab {
			e = recentEmpty
			e.Action = &emptyAction{Label: "去听推荐", Do: func() { h.tab = 0 }}
		}
		h.withPopTop(c, tab, func() { s.viewEmpty(c, "home:empty", e) })
	default:
		// ⚠️ **表头在列表外面**（`rowheader.go` 文件头）：它是 `View` 的兄弟，所以列表往
		// 下滚它不动，宽屏下滚到右边列名也还在。
		//
		// ⚠️ **两个例外**：这两档的表头在**页内头部里面**（跟着顶上那一块一起滚走）：
		//
		//	「流行」   新歌速递那一块（`viewPopLead`）
		//	两张榜     榜单头图 + 表头（`viewBoardLead`，2026-10-27 用户要求
		//	           把固定的顶栏改成跟着滚）
		//
		// 表头说的是下面那些歌，而顶上那一块在它们上面——表头留在列表外面就会
		// 跑到顶上那一块上面去。
		if tab != homePopTab && tab != homeRecentTab && tab != homeHotTab && tab != homeOriginalTab {
			h.lists[tab].Header(c, "home:header:"+strconv.Itoa(tab))
		}
		h.lists[tab].View(c)
		s.noteScrollbar(h.lists[tab].Hot())

		// 「回到顶部」那一枚**由画着的那一档自己画**（`list.PagedList` 里面那
		// 一份 `BackToTop`）：滚过 400 像素才出现，浮在整窗右下（上游的数，见
		// `widgets/list` 的 `BackToTop`）。
		//
		// ⚠️ **别在这里再画一枚**：画出来的那一枚给的是「这一档」的数，而三档
		// 一起画的话按钮就是三枚。按它回的是**画面上画着那一档**自己的位置
		// （见 `PagedList.ScrollTop`）——这正好是票面要的那一条。
	}
}

// boardCaptionText 是榜单页顶上那一行：第几期、哪天更新。缺哪样就少说哪样。
// 期数带上「周榜」——热榜 / 原创榜取的都是 B 站音乐站的**周榜**最新一期，
// 不说的话用户分不清「第 N 期」的节奏是什么。
func boardCaptionText(number int, at time.Time) string {
	var parts []string
	if number > 0 {
		parts = append(parts, "第 "+strconv.Itoa(number)+" 期（周榜）")
	}
	if !at.IsZero() {
		parts = append(parts, at.Format("2006-01-02")+" 更新")
	}
	return strings.Join(parts, " · ")
}

// viewBoardCaption 在热榜 / 原创榜的列表上面画期数和更新时间。别的档不画；
// 还没拉到那一期的信息时也不画（不摆一行空话）。
func (h *homePage) viewBoardCaption(c *ui.Context, tab int) {
	if (tab != homeHotTab && tab != homeOriginalTab) || h.loads.board == nil {
		return
	}
	n, at, ok := h.loads.board(tab)
	if !ok {
		return
	}
	text := boardCaptionText(n, at)
	if text == "" {
		return
	}
	// 榜单头图：和总览上那张磁贴同一套渐变，大字榜名 + 期数更新日期，
	// 让二级页一眼认得出是从哪张磁贴进来的。
	var from, to ui.Color
	for _, tile := range homeTiles {
		if tile.tab == tab {
			from, to = tile.from, tile.to
		}
	}
	t := c.Theme()
	ui.Row(c).Key("home:board:hero").FillWidth().Height(homeBoardHeroH).Radius(t.Radius+4).
		Gradient(from, to, 135).Padding(space.Px16, space.Px24).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Gap(space.Px6).Children(func() {
			ui.Text(c, homeTabs[tab]).FontSize(scale.Font(c, scale.Size28)).Bold().TextColor(ui.RGBA(255, 255, 255, 1)).MaxLines(1)
			ui.Text(c, text).Key("home:board:caption").FontSize(scale.Font(c, scale.Size13)).
				TextColor(ui.RGBA(255, 255, 255, 0.82)).MaxLines(1)
		})
		items := h.lists[tab].Items()
		if len(items) == 0 || h.acts.Add == nil {
			return
		}
		btn := ui.Row(c).Key("home:board:enqueue").AlignItems(ui.Center).Gap(space.Px6).
			Padding(space.Px8, space.Px16).Radius(20).Background(ui.RGBA(255, 255, 255, 0.2)).
			Focusable().Role(ui.RoleButton).Label("加入播放列表").OnClick(func() {
			its := make([]app.Item, 0, len(items))
			for _, m := range items {
				its = append(its, m.Item)
			}
			h.acts.Add(its)
		})
		if btn.Hovered() {
			btn = btn.Background(ui.RGBA(255, 255, 255, 0.32))
		}
		btn.Children(func() {
			ui.Icon(c, icon(iconEnqueue)).FontSize(18).TextColor(ui.RGBA(255, 255, 255, 1))
			ui.Text(c, "加入播放列表").FontSize(scale.Font(c, scale.Size14)).TextColor(ui.RGBA(255, 255, 255, 1))
		})
	})
}

// viewTabs 画那三个 Tab 和「全部播放」。
//
// # 「全部播放」现在摆了（票 #244 / UX 报告 §2 的 B6）
//
// 首页和搜索页共用 playAllButton 与 playAllSongs；两处都换成当前这一批并从
// 第一首开始播（票 #337，2026-10-24）。「从这一首往后听」是行菜单里的区段替换。
//
// ⚠️ **「全部播放」贴在内容区的右沿**（票 #288 / UX 报告 C-04）。
//
// 上游那一行是 `flex items-center justify-between`（`index.tsx:262-294`）：Tab
// 靠左、「全部播放」靠右，**右沿=x=1184**（1200 宽的窗、内容区右边距 16）。
// 我们此前是「紧跟在 Tab 后面」（x=419），右半屏空着——同一个动作既不像「这一页
// 的操作」也不像「全局操作」。
//
// 🔴 **右沿靠 `Justify(SpaceBetween)`，不是靠算像素**：那一行是 `FillWidth()` 的，
// 它有多宽是内容区说了算（`pagePadX` = 16 是七页共用的那一个数），于是
// 「贴在右沿」这件事跟着内容区走，窗口一改它跟着改。
func (h *homePage) viewTabs(c *ui.Context, s *Shell) {
	// ⚠️ **缝是 12**：和搜索页那一排一样（`viewSearchToolbar` 的 `Gap(space.Px12)`）——
	// 那是两样东西之间的**最小**缝；这一行靠 `SpaceBetween` 撑开，所以实际隔着
	// 整个右半屏（上游那一行也是 `justify-between`）。
	//
	// ⚠️ **搜索页那一枚是「紧跟着排序 Tab」**（上游搜索页那一行右边还有一个「仅
	// 音乐分区」的开关，我们没有，所以它不贴右沿）——两个页面上的同一个动作，
	// 位置不一样是**跟上游一致**的，不是漏改。
	ui.Row(c).Key("home:tabbar").FillWidth().Gap(space.Px12).AlignItems(ui.Center).
		Justify(ui.SpaceBetween).Children(func() {
		// 🔴 Key 要写在 Tab 的构造**之前**：Tab 里面是个 List，它的状态在
		// 构造的时候就已经读过了，构造之后再 `.Key()` 会 panic（`ui/element.go`
		// 的 Key 注释：自带输入的控件都不能构造后 Key，只能构造前 `Context.Key`）。
		// 中间的语句会把它抢走，所以这两句要紧挨着。
		// ⚠️ **主 Tab 用轨道 + 滑块那一种**（`mainTabs`，见 tabs.go）：这一排
		// 和次级排序那几排不是同一个样子（票 #138）。
		//
		// 首页这一排上游是 `size="lg"`（44），其余三处是 md（40）。
		ui.Row(c).Gap(space.Px8).AlignItems(ui.Center).Children(func() {
			if iconKey(c, iconSideCollapse, "返回发现", 20, false).Size(36, 36).Padding(space.None, space.Px8).Clicked() {
				h.backToOverview()
			}
			c.Key("home:tabs")
			mainTabs(c, &h.tab, tabLG, homeTabs[:]...)
		})

		// 「全部播放」：这一档的歌（**当前这一档 Tab 的**，不是三个 Tab 加起来）。
		//
		// ⚠️ **禁用的条件**：一首都没有的时候不让按（上游那一枚也是这个条件，
		// `video-list/search-header.tsx:55`）。而**正在往下拉的那一页不关它**——
		// 手上已经有歌了就能放，用户不必等这一页回来。
		pg := h.lists[h.tab].Paged()
		if playAllButton(c, pg.Loaded == 0).Clicked() {
			// ⚠️ **歌是按下去那一刻才取的**：`PagedList.Items()` 是一份拷贝，
			// 每帧取一次就是每帧一次大分配（而这一屏有逐帧分配的预算，见
			// `framebudget_test.go`）。
			s.playAllSongs(h.lists[h.tab].Items())
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 切 Tab 的滚动位置
// ─────────────────────────────────────────────────────────────────────────────
//
// 「切回某个 Tab 时它还在原来的位置」是这么来的，**没有一行存-取-还的代码**：
//
// 每个 Tab 一个翻页列表（`list.PagedList`），滚动位置就跟着那一个对象走。
// `ui.ListState` 把「顶上那一行是第几行、它离列表顶多远、每一行量出来多高」全记
// 在它自己的字段里（`mygo/ui/list.go` 的 ListState：「The place: the first row in
// view, and how far the list is scrolled past its top」），而它由底下那个
// `list.List` 自己拿着。切 Tab 时换掉的只是「画哪一个列表」，用户看到的就回到
// 那一屏。
//
// 为什么不能三个 Tab 共用一个列表：那样切回时同一个 `ui.ListState` 里顶上那一行
// 是**另一个 Tab 的第几行**，而顶上那首歌会随换 Tab 换掉——用户滚到第 17 首、
// 切去「鬼畜」、再切回来，看到的是「鬼畜」的第 17 首，或者干脆回到顶上。
// `TestSwitchingBackRestoresThatTabsScrollPosition` 钉的就是这一条。
//
// ⚠️ **换成翻页列表没有动这一条**：三个 Tab 仍然是三个各记各的列表（换的是
// 列表**外面**那一层：翻页、防重入、取消），而滚动位置住在**列表里面**那个
// `ui.ListState` 上——换外面那一层不换里面那一个。
//
// ⚠️ **别拿 `ui.ScrollState` 去「存像素、切回来再 ScrollTo 回去」**：那是在已经
// 记对了的锚点上抢方向盘。实测过一次——三个列表各挂一个 `ScrollState`
// （`List.SetScroll`），滚到第 19 首（Y=1200）、切走再切回来，Y 变成 359，顶上
// 那首变成第 6 首：TrackScroll 按像素把容器摆过去，`ListState` 按行把锚点算回
// 去，两个数换算的那一帧里锚点被拽偏了。
//
// 🔴 **而这一页那三档偏偏都不挂 ScrollState**（`list.PagedList.SetScrollState`）：
// 挂上它的时候滚动位置由它记，而它记的是「**元素那个位置**」的——于是上一档
// 留下的偏移会拽走这一档（实测：切回「音乐」落到第 6 首而不是它原来的第 19 首）。
// 不挂它，三档就各自按**自己的行锚点**记位置（`ui.ListState`，框架本来就是按行
// 记的）——互不干扰。
//
// ⚠️ **三档落在不同的元素位置上**（票 #368 的内容块按档分了 key，见
// `homepage_transition.go` 的 `homeBodyKeys`）：框架把滚动状态记在**元素的位置**
// 上，而同一个位置轮流画的几份列表会**抢同一份**。这一条是上面那条的前提——
// 两档抢同一份的话，不挂 ScrollState 也一样会被拽走。
//
// ⚠️ **代价是回顶那一枚的偏移改按行换算**（列表那一档的行高是定死的 64，
// 所以差不到一行；网格那一档是估的，见 `list.Grid.ScrollOffset`）。而这一页
// 的列表**够长**（一直往下滑一直有），所以「滚过 400 像素」这个门槛滚得到。
//
// 真要按像素对外说（**回顶按钮**判断「滚过 400 像素没有」），**读那一档自己的
// 那一份**——`list.PagedList` 里的回顶按钮就是这么画的。
