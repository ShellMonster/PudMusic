// 首页：推荐音乐（Biu 的 `/`，`src/pages/music-recommend/`）。
//
// ## 这一版摆什么
//
// 三个 Tab（音乐 / 鬼畜 / 流行）+ 新歌速递那一块横向网格。**新歌速递只画在
// 「流行」Tab 上**（上游 `index.tsx:295` 的 `{activeTab === "pop" && <NewMusicTop …
// />}`），另外两个 Tab 上没有它。
//
// **还没有歌**：B 站的推荐接口还没接（PLAN 阶段 5 的 P-08 起），所以三个 Tab 的
// 列表和那一块网格现在都是空的。空着的时候画的是 `routes` 表里 `/` 那一条的空
// 状态（`viewEmpty`），不是三个列表各自那句「这里还没有歌」——那句话只说这一屏
// 是空的，没说怎么才能不空，而 `routes` 表里这一条两句话都在。
//
// 🔴 铁律二：这一页的 view **不许**把 `SetTheme` 放到别的地方调，见
// `viewHome` 开头那一行和 theme.go。
//
// 🔴 页面级的 recover 在 `router.go` 的 `Shell.page` 上（和其余 14 条路由共用
// 那一道 Guard），这里不用再包一层：view 里 panic 会一路穿出 Cocoa 事件循环把
// 整个进程带走，这一页画的就是那条路上的一环。
package ui

import (
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
// 现在没有任何地方填它（B 站接口还没接）：留着这个形状，是因为接上接口的那一
// 张只需要在拿到结果之后调一次 `homePage.SetSongs`，页面结构一个字都不用改。
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
	lists [homeTabCount]*list.List

	// newGrid 是新歌速递那一块横向网格，见 homepage_newmusic.go。
	newGrid *newMusicGrid

	// songs 是这一屏要画的歌，由 SetSongs 换。三个列表和那一块网格都读它。
	songs homeSongs

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
	for i := range h.lists {
		l := list.New(cache, acts)
		// 空状态**不在这里说**：一句都没接上时是**这一页**的空状态（routes 表里
		// `/` 那一条，见 viewHome）。让三个列表都说一遍自己那句「这里还没有歌」，
		// 叠在那一页的空状态上就是两句话在讲同一件事。
		l.SetEmpty("")
		// ⚠️ **这里不调 SetScroll**（`widgets/list` 有那一档，是给「回顶按钮」
		// 和分页列表的判断用的）：给一个 `List` 挂上 `ScrollState` 会和它自己按行
		// 记的位置**打架**——TrackScroll 每一帧按那个像素数把容器摆过去，而
		// `ui.ListState` 记的是「第几行 + 那一行离顶多远」，两个数换算的那一帧里
		// 锚点会被拽偏。实测：同一个列表滚到第 17 首、切走再切回来，挂上
		// ScrollState 时回到第 5 首（Y 从 1200 变成 437），不挂时回到第 17 首。
		// 「切 Tab 回到原位」靠的是三个 `list.List` 各自那份 `ListState`，不靠像素。
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
func (s *Shell) viewHome(c *ui.Context, _ *ui.Route) {
	s.applyFrameTheme(c)
	s.home.view(c, s)
}

// view 画首页的内容。
//
// 摆法是「一列三块」：Tab 那一排 →（只在流行上）新歌速递 → 选中那一屏的列表。
// 下面那一块给了 Grow(1) 的是列表，`list.List` 自己就占了 Grow(1)（见
// widgets/list 的 List.View），所以它拿走了剩下的高度。
//
// s 是拿来画这一页的空状态的（`viewEmpty`），别的地方都用不上它。
func (h *homePage) view(c *ui.Context, s *Shell) {
	ui.Column(c).Key("page:" + homePath).Grow(1).Padding(20).Gap(12).
		AlignItems(ui.Stretch).Children(func() {
		h.viewTabs(c)

		// 新歌速递只画在「流行」这一屏的顶部，别的地方一律不画（上游
		// index.tsx:295）。
		if h.tab == homePopTab {
			h.newGrid.View(c)
		}

		// 选中那一屏的内容。一行歌都没有时是**这一页**的空状态。
		//
		// ⚠️ 走 `viewEmpty`（`routes` 表里 `/` 那一条，出处和其余 13 条写在
		// 一起），不是让 `list.List` 说它自己那句「这里还没有歌」：那句只说
		// 这一屏是空的，那一页的两句话连着说了「为什么空」和「怎么才能不空」。
		// 接口接上之后这一支自然走不到，页面结构一个字都不用改。
		if len(h.songs.tabs[h.tab]) == 0 {
			s.viewEmpty(c, "home:empty", lookup(homePath).Empty)
			return
		}
		h.lists[h.tab].View(c)
	})
}

// viewTabs 画那三个 Tab。
//
// ⚠️ Biu 把 Tab 那一排和右边一个「全部播放」按钮放在同一个 `justify-between`
// 的行里（`index.tsx:262-294`）。按钮**这一版不摆**：它是 `addList`（往播放
// 列表里**追加**，不换队列也不起播，Biu 的 `handlePlayAll` 就是这么写的），而
// 这一屏现在一行歌都没有，按下去除了弹一句「暂无可播放内容」什么都不会发生。
// 摆一个只能这样用的按钮不如不摆——和 widgets/list 里「收藏/下载两项会说话、
// 不装死」是同一个取舍的做法，区别是这里那句话连信息都没有。
func (h *homePage) viewTabs(c *ui.Context) {
	ui.Row(c).Key("home:tabbar").FillWidth().AlignItems(ui.Center).Children(func() {
		// 🔴 Key 要写在 `ui.Tabs` 的构造**之前**：Tabs 里面是个 List，它的状态在
		// 构造的时候就已经读过了，构造之后再 `.Key()` 会 panic（`ui/element.go`
		// 的 Key 注释：自带输入的控件都不能构造后 Key，只能构造前 `Context.Key`）。
		// 中间的语句会把它抢走，所以这两句要紧挨着。
		c.Key("home:tabs")
		ui.Tabs(c, &h.tab, homeTabs[:]...).Grow(1)
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
// （`List.SetScroll`），滚到第 17 首（Y=1200）、切走再切回来，Y 变成 437，顶上
// 那首变成第 5 首：TrackScroll 按像素把容器摆过去，`ListState` 按行把锚点算回
// 去，两个数换算的那一帧里锚点被拽偏了。不挂 `ScrollState`（`newHomePage` 里
// 特意不调 `SetScroll`）同一个用例一次就过。
//
// 真要按像素对外说（回顶按钮、分页列表判断「滚过 400 像素没有」），那是在
// **那一个**列表上挂 `ScrollState`，不是拿来当三个 Tab 的位置备份——那两件事的
// 区别在 widgets/list 的 PagedList 里就是同一个 `scroll`：它给的是当前这一屏的
// 那一个列表，而「切 Tab 的位置」在那边是三个 `ListState`。
