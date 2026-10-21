// 我的关注那一页：左边是关注的分组，右边是一个网格。
//
// # 摆法是抄 Biu 的 `src/pages/follow-list/index.tsx`
//
// 左边一列 Tab（第一格是「全部关注(N)」，之后每个分组一格），右边一个虚拟网格
// 装着关注的 UP 主，「全部关注」那一格顶上有一个搜索框。上游这一页的网格是
// `VirtualGridPageList` ——**和首页的推荐网格是同一个组件**，所以列数断点、格子
// 间距取的是同一份（`list.GridColumnsFor` / `list.GridGap`），别在这里另写一
// 张断点表。
//
// # ⚠️ 为什么用户卡不直接用 `list.Grid`
//
// `list.Grid` 画的是**一首歌**：封面 / 歌名 / UP 名 / 时长，四个字段都取自
// `list.Music`。一张用户卡是头像 / 昵称 / 签名 / 认证——`list.Music` 上**没有**
// 这几个字段，硬塞进去就是把「一张卡」伪装成「一首歌」，而伪装不了的字段最后
// 要么画成空要么被编出来。共用的是**那一半真的能共用**：列数断点和格子间距
// （`list.GridColumnsFor` / `list.GridGap`），以及换列数时守住滚动锚点那一套
// （`keepAnchor`，见下）。
//
// # 🔴 铁律四：每一格的身份是那个人的 mid，每一行的身份是这一行第一个人的 mid
//
// 理由和 `upspace.go` / `dynamicfeed.go` 一样。**行身份不是行号**：换列数是
// 布局变了、数据没变，同一个行号换了一张卡之后用户盯着的那张会整个跳走。
//
// # 换列数时守住滚动位置
//
// 和 `widgets/list/grid.go` 的 `keepAnchor` 同一个理由、同一套做法：6 列的第 120
// 行是 2 列的第 360 行，滚动量不动的话用户盯着的那张卡整个跳走（拖窗口边的时
// 候正好一帧一帧跨过每一档）。锚点记的是**那个人的 mid**而不是行号——歌（这
// 里是人）在第几行是算出来的，一取整就丢。
//
// # 五种「空」是五句话，一句都不能混
//
// 这一页最容易出的错就是把「没登录」和「没关注任何人」说成同一句：前者要用户
// 去登录，后者去登录也还是空的（他真的一个 UP 主都没关注）。用户读到「暂无
// 内容」时不知道该做什么，于是永远不知道这一页该做什么。另外三句——搜了没搜
// 到、这一组还没有人、这一块拉失败了——各是各的事（见下面那几个 emptyState）。
//
// # 🔴 铁律二：`applyFrameTheme` 每帧在 view 最开头调（理由同 upspace.go）
package ui

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/egoist/mygo/ui"
)

// followPath 是「我的关注」那条路由，和 `router.go` 的 routes 表里登记的是同一个
// 字符串。
//
// 拿来和 `Route.Match` 比，而不是拿 `routes[i]`：那条表一改顺序，这里跟着错，
// 而且错得很安静——这一页退回占位页，没人看得出来（理由同 `laterPath`）。
const followPath = "/follow"

// followPaneW 是左边那一列的宽度。上游是 `w-50`（200）。
const followPaneW = 200

// followCardSide 是一张卡上头像的边长。上游是 `h-32 w-32`（128）。
const followCardSide = 128

// followCardPad 是卡里那圈留白。上游是 `p-4`（16）。
const followCardPad = 16

// followSearchLabel 是搜索框的标签。**抄上游那个 placeholder 一字不差**
// （`follow-list/index.tsx:296`）。
const followSearchLabel = "搜索关注"

// followTabAll 是左边第一格「全部关注」的身份。
//
// ⚠️ **用 `all` 这个串而不是空串**：空串在「分组列表还没拉到」那一档也是空串，
// 而那一档要画的是「还没拉到」，不是「全部关注」。两个都空的话那一帧画出来
// 第一格没有名字，而用户会以为整个分组列表是空的。
const followTabAll = "all"

// followTagTab 是分组那一格的身份。分组 id 可正可负（B 站固定有 -10 特别关注
// 和 0 默认分组），所以身份是带前缀的串而不是那个数本身。
func followTagTab(tagID int) string { return "tag:" + strconv.Itoa(tagID) }

// tagIDOf 是这个 Tab 对应的分组 id。不是分组那一格时 ok=false。
func tagIDOf(tab string) (int, bool) {
	if !strings.HasPrefix(tab, "tag:") {
		return 0, false
	}
	id, err := strconv.Atoi(strings.TrimPrefix(tab, "tag:"))
	if err != nil {
		return 0, false
	}
	return id, true
}

// ─────────────────────────────────────────────────────────────────────────────
// 空状态：五句，互不通用
// ─────────────────────────────────────────────────────────────────────────────

// followNotLoggedIn 是「**还没登录**」。
//
// ⚠️ **它和「没关注任何人」必须是两句**，而且这一句排在前面判：没登录时那个
// 接口一律回 -101，而 -101 在这一层和「网络断了」长得一模一样（`client.go` 的
// `ErrNotLoggedIn`）。说成「还没关注任何人」的话，用户登录完发现还是空的，会
// 以为是登录坏了。
var followNotLoggedIn = emptyState{
	Title: "还没登录",
	Hint:  "关注列表是跟着账号走的——先扫码登录，登录之后你关注的 UP 主会出现在这里",
	Icon:  iconEmptyLogin,
}

// followNoSearchResult 是「**搜了，没搜到**」。
//
// 抄上游 `pages/follow-list` 搜不出东西时落到的那句 `暂无内容`
// （`virtual-grid-page-list` 的兜底），但下面第二句是我们自己写的：上游在
// 这一格压根没有搜索框，所以它没遇到过这一种空。
var followNoSearchResult = emptyState{
	Title: "没搜到",
	Hint:  "换个词再搜一次，或者清空搜索框看全部关注",
	Icon:  iconEmptyGroup,
}

// followEmptyTag 是「**这一组还没有人**」。
//
// ⚠️ 别用「没关注任何人」那一句：这一组空着不代表一个关注都没有（隔壁那几组
// 摆着人呢），说成「还没关注任何人」的话用户会去一个一个分组看。
var followEmptyTag = emptyState{
	Title: "这个分组还没有人",
	Hint:  "在 UP 主空间里点「关注」，关注过的人会出现在「全部关注」那一格",
	Icon:  iconEmptyGroup,
}

// ─────────────────────────────────────────────────────────────────────────────
// 状态
// ─────────────────────────────────────────────────────────────────────────────

// followPage 是这一页的状态。跟着壳走（**只有一份**，这一页没有参数）。
//
// ⚠️ **只由界面线程读写**，所以里面没有一个锁；后台那一条线只往 box 那个带锁
// 的信箱里投，投完叫醒界面（铁律三）。
type followPage struct {
	// ── 左边那一列 ──
	//
	// tags 是关注分组。tagStarted / tagErr 是这一摊自己的状态——它和右边那一格
	// 是两次独立的请求，各拉各的，各失败各的。
	tags       []bili.FollowTag
	tagStarted bool
	tagErr     error
	tagLoading bool

	// tab 是当前停在哪个分组（`followTabAll` 或 `followTagTab(id)`）。
	tab string

	// ── 右边那个网格 ──
	users []bili.FollowUser
	// seen 是**已经收进来的身份**，挡翻页重叠。B 站翻页是会重叠的（同一页的
	// 东西挪了名次就会在两页里各出现一次），而两个一模一样的身份会让框架按
	// key 对位时那几格共用一份状态（理由同 `upSpace.seen`）。
	seen map[string]bool
	// total 是「全部关注(N)」那个 N。**只在不搜的时候更新**——搜出来的 total 是
	// 搜到几个，而那个数挂在「全部关注」旁边会变成一句假话。
	total int

	pager upPager

	// state 是网格的行状态，paneState 是左边那一列的。建一次存着，每帧用
	//（铁律四：换列数是布局变了，每帧新建等于每帧跳回顶上）。
	//
	// ⚠️ **两个 ListState 不能共用**：MyGo 会在「同一个 ListState 被两个 List
	// 显示」时 panic，而共用还会把两边的滚动位置搅在一起。
	state     ui.ListState
	paneState ui.ListState

	// scroll 是右边那一格（那些用户卡片）滚到哪儿了。
	//
	// ⚠️ **它只是给「回到顶部」那一枚用的**：这一格自己滚（卡片是方的，行高跟着
	// 内容走），而那一枚要的是**像素**（上游的阈值是 400 像素），所以位置得由
	// 框架写回来。
	scroll ui.ScrollState

	// width 是这一帧量出来的**容器**宽，cols 是按它算出来的列数。
	//
	// ⚠️ 判据是容器不是窗口（页面左边有侧栏，差一档就是差一列，理由同
	// `widgets/list/grid.go`）。量的是这个列表元素自己的盒子，而 `Bounds()`
	// 报的是**上一帧**的盒子——所以改窗口大小之后列数晚一帧换，和 Biu 那边
	// ResizeObserver 改 state 再重画是同一个节奏。
	width float32
	cols  int
	// anchorMid 是滚动锚点守着的**那一个 mid**（见文件头「换列数时守住滚动位置」）。
	anchorMid int64
	started   bool

	// draft 是搜索框里正在敲的字，applied 是**已经在搜**的那个。
	//
	// 分成两个是因为即时模式每一帧都重画（理由同 `libraryPage.draft/applied`）。
	draft, applied string

	// failed 是画崩了的**卡**（按 mid 记），下一帧不重试：同一格每帧崩一次会
	// 刷爆日志、拖垮帧率，比「这一格画不出来」糟得多。
	failed map[int64]bool

	// selfMid 是这份缓存**为哪个账号**拉的。换账号整页作废（见
	// resetFollowIfAccountChanged）。
	selfMid int64

	// gen 是代次。换分组 / 换搜索词时 +1：在途那一页回来时代次对不上就扔掉。
	gen uint64

	// ── 跨 goroutine 的只有这一小摊 ──
	boxMu sync.Mutex
	box   []followResult
}

// followWhich 这一次拉的是哪一半。
type followWhich uint8

const (
	followTags followWhich = iota
	followUsers
)

// followResult 是后台拉完之后投进信箱的东西。
type followResult struct {
	which followWhich
	// selfMid 是发这一次时的账号。换账号之前发出去的那一页作废。
	selfMid int64
	// gen 只在拉右边那一格时有意义（换分组 / 换词会让代次 +1）。
	gen int
	// tab / keyword 是这一次**是给哪个 Tab、带哪个词**发的。回来时对不上就
	// 扔掉——用户可能已经切走了。
	tab, keyword string

	tags  []bili.FollowTag
	users []bili.FollowUser
	// total 是「一共多少人」。三个接口给总数、分组明细不给（那一档用分组自己的
	// count，发请求之前就在界面线程上取好了，见 tabTotal）。
	total int
	err   error
}

// followPageOf 是这一页的状态，第一次问的时候现建。
func (s *Shell) followPageOf() *followPage {
	if s.follow == nil {
		s.follow = &followPage{tab: followTabAll}
	}
	return s.follow
}

// resetFollowIfAccountChanged 在**账号变了**时把这一页整个作废。
//
// 左边列的是「我关注的人」、分组也是「我的分组」，两样都是**按账号**的。换账号
// 后留着上一份的话，用户在新账号下看的是旧账号的关注——而界面上没有任何东西
// 提示这一点（和 `dynamicfeed.go` 同一个理由）。
func (s *Shell) resetFollowIfAccountChanged(f *followPage) {
	cur := s.app.SelfMid()
	if f.selfMid == cur {
		return
	}
	f.selfMid = cur
	f.tagLoading = false
	f.tags, f.tagStarted, f.tagErr = nil, false, nil
	f.resetUsers()
	// ⚠️ **「全部关注(N)」那个 N 跟着账号走**，所以这里是它该清的地方：换账号
	// 之后新账号的第一页还没落地，而那一个 N 是**上一个账号**的关注数——新账号的
	// 第一页要是恰好拉失败，它会一直挂着，而界面上没有任何东西提示这一点。
	//
	// （它**不在** `resetUsers` 里：换分组、换词都只换「看哪一批」，不换
	// 「我关注了多少人」，那两种情况下清掉它反而是白丢信息。）
	f.total = 0
	f.gen++
}

// resetUsers 清掉右边那一格：行、翻页闸门、代次、回顶。
//
// ⚠️ **必须在换分组 / 换词之后调，而且行也要一起清**：`pager.stop` 只是让在途
// 的那一页不落地（`upPager` 那段的注释），已经拿到的行还留在 `users` 里，
// 于是下一次拉回来的是同一页，被直接追加在后面——两格一模一样的身份，框架按
// key 对位时它们共用一份状态。
//
// ⚠️ **`f.total` 不在这儿清**：「全部关注(N)」那个 N 说的是「**这个账号**关注了
// 多少人」，它跟着账号走，不跟着「停在哪个分组 / 有没有在搜」走——Biu 那边
// 换分组、搜词它都还在（`follow-list/index.tsx:224`），所以那两种情况下清掉它
// 是白丢一个信息。要清的是**换账号**，见 `resetFollowIfAccountChanged`。
func (f *followPage) resetUsers() {
	f.pager.stop()
	f.users = nil
	f.seen = nil
	f.pager = upPager{}
	f.state.ScrollTo(0, ui.Start)
}

// ─────────────────────────────────────────────────────────────────────────────
// 画这一页
// ─────────────────────────────────────────────────────────────────────────────

// viewFollow 画「我的关注」这一页。
//
// ⚠️ **外面已经包了一层 recover**（`router.go` 的 `Shell.page`）。
// ⚠️ **问 `r` 不要问 `c.Router()`**（理由同 `upspace.go`）。
//
// ⚠️ **这一页没有页标题，而名字是有的**（票 #127）：它写在**左边那一列顶上**
// ——`followPaneTitle` 给出「全部关注」/ 那一组的名字 / 「我的关注」
// （理由见那个函数）。上游那一页的 `<h1>` 给的也是同一句话
// （`src/pages/follow-list/index.tsx:206`：`全部关注` / 分组名 / `我的关注`），
// 只是上游把它摆在右上角、我们摆在左边那一列的头顶上——那一列本来就是分组
// 列表，「现在是哪一组」和「哪一列」是同一件事。
// 顶栏那一处页名票 #127 去掉之后，这一页的名字就只剩这一处，**不重复**。
func (s *Shell) viewFollow(c *ui.Context, _ *ui.Route) {
	// 🔴 铁律二：每帧第一件事就是装主题，而且必须在最开头。
	s.applyFrameTheme(c)

	f := s.followPageOf()

	// 先收后台投回来的结果，再画：这样这一帧画的就是已经加上/换掉的那几格，
	// 不用等下一帧（铁律三）。
	s.adoptFollow(f)
	s.resetFollowIfAccountChanged(f)

	// ⚠️ **没挂上客户端就说清，而不是发一个注定失败的请求**（理由见
	// viewLibraryMissing）。这一支画的是**路由表里那一句**加上「没接上」这个
	// 这一次特别的原因——从别的页面切过来的那条路按 `lookup(path).Empty.Title`
	// 认「切过去了」，少画那一句就断了。
	if !s.app.ExploreMounted() {
		s.viewPageEmptyWithReason(c, "page:"+followPath, "我的关注", lookup(followPath).Empty)
		return
	}

	// 🔴 **没登录不假装有内容**。这一页的四个接口全都要 Cookie，没登录时它们
	// 一律回 -101，而 -101 和「网络断了」在这一层一模一样。所以先判登录态、
	// 一个请求都不发（理由见文件头「五种空」）。
	//
	// ⚠️ **旗标在路由表里**（`Route.RequiresLogin`），而「要不要摆进侧栏」是
	// 另一个标志（`Route.HideInMenuWhenLoggedOut`）——两件事，两个标志（票 #138）。
	if lookup(followPath).RequiresLogin && !s.app.LoggedIn() {
		s.viewNeedsLogin(c, "page:"+followPath, followNotLoggedIn)
		return
	}

	// 分组那一列的第一拉。**必须在画之前发出去**（理由同 viewDynamicFeed）。
	//
	// 🔴 **`f.tagErr == nil` 是必需的一道闸**，和右边那一格的 `upPager.want()`
	// 里那个 `p.err != nil` 是同一件事（见 `upspace.go` 的 `want`）。漏了它就是
	// 一个停不下来的循环，而且**不靠用户点就自己转起来**：
	//
	// 这一段每帧都跑 → 失败 → 下面每一行都有 `s.wake.invalidate()`（铁律三）→
	// 要来一帧新画面 → 下一帧又满足条件 → 再发一次。
	//
	// 实测：假 B 站一直回 -412，60 帧里这一支被打 **186 次**。
	// 触发条件全是日常的：SESSDATA 过期（本地看着还登录着，B 站回 -101）、
	// -412/-352 风控、网络抖一下。所以这一格和右边那一格必须是**同一道**闸。
	if !f.tagStarted && !f.tagLoading && f.tagErr == nil {
		f.tagLoading = true
		go s.fetchFollowTags(context.Background(), f)
	}
	// 右边第一格的第一拉。同上，而且**只在「一页都还没拉过」时发**——这一段
	// 每帧都跑，不设这道门就是第一页一落地、下一帧又拉一页。
	if !f.pager.started && !f.pager.loading {
		if ctx := f.pager.begin(); ctx != nil {
			go s.fetchFollowUsers(ctx, f, f.gen, f.tab, f.applied, f.pager.page, len(f.users), f.tabTotal())
		}
	}

	ui.Row(c).Key("page:" + followPath).Grow(1).FillHeight().Children(func() {
		s.viewFollowPane(c, f)
		ui.Column(c).Key("follow-body").Grow(1).FillHeight().Padding(16).
			Gap(8).Children(func() {
			s.viewFollowSearch(c, f)
			s.viewFollowGrid(c, f)
		})
	})

	// 「回到顶部」那一枚。滚过 400 像素才出现（上游的数，见 `widgets/list` 的
	// `BackToTop`）。
	//
	// ⚠️ **这一页右边那一格自己滚**（那些用户卡片是方的，行高跟着内容走），所以
	// 位置是像素而不是行数——挂一个 `ScrollState` 上去，让框架把偏移写回来。
	list.BackToTop(c, f.scroll.Y, f.scrollTop)
}

// scrollTop 回到顶上。
//
// ⚠️ **两样都要归零**：那一份 `ScrollState`（像素）和列表自己的行锚点。少了前者
// 按钮按下去像是没反应，少了后者列表还停在原来那一行（见 `widgets/list` 的
// `PagedList.ScrollTop`，那一条也是两样都写）。
func (f *followPage) scrollTop() {
	f.scroll.Y = 0
	f.state.ScrollTo(0, ui.Start)
}

// viewFollowPane 是左边那一列：标题 + 「全部关注(N)」+ 每个分组一格。
//
// ⚠️ **左边的行和右边的格是两个 `ListState`，不能共用**（MyGo 会在同一个
// ListState 被两个 List 显示时 panic）。
func (s *Shell) viewFollowPane(c *ui.Context, f *followPage) {
	t := c.Theme()
	ui.Column(c).Key("follow-pane").Width(followPaneW).FillHeight().
		Padding(8, 6).Gap(4).Children(func() {
		// ⚠️ **失败也要说失败**：`tagErr` 有值的时候下面那一支已经把分组列表
		// 换成了错误提示，而标题还在——用户点「重试」时看得出自己重试的是
		// 哪一摊。
		ui.Text(c, followPaneTitle(f)).FontSize(scale.Font(c, 14)).Bold().PaddingX(6)

		// ⚠️ **「拉失败了」必须排在「空」前面**：反过来的话第一页失败时左边
		// 是一片「还没有分组」，而 `tagStarted` 仍是 false、`tagErr` 非 nil，
		// 切走再切回来也不会重拉——用户会以为他真的一个分组都没建过。
		if !f.tagStarted && f.tagErr != nil {
			ui.Text(c, "分组没拉出来："+f.tagErr.Error()).FontSize(scale.Font(c, 12)).
				TextColor(t.TextMuted).PaddingX(6)
			if ui.Button(c, "重试拉分组").FontSize(scale.Font(c, 12)).Clicked() {
				f.tagErr, f.tagLoading = nil, true
				go s.fetchFollowTags(context.Background(), f)
			}
			return
		}
		// 🔴 **「没拉到」和「一个分组都没有」是两件事**，界面上是两句话：
		// 前者要按重试，后者不用（那是真的没有分组，B 站那会儿返回空数组）。
		if !f.tagStarted {
			ui.Text(c, "正在加载分组…").FontSize(scale.Font(c, 12)).TextColor(t.TextMuted).PaddingX(6)
			return
		}

		// ⚠️ Key 挂在外面的 Column 上：List 是自己管状态的控件，事后再挂 key
		// 会 panic（理由同 `dynamicfeed.go`）。
		ui.Column(c).Key("follow-pane-list").Grow(1).FillHeight().Children(func() {
			pane := ui.List(c, &f.paneState, len(f.tags)+1).
				Grow(1).FillHeight().
				Rows(func(r ui.ListRow) { s.viewFollowPaneRow(r.Context, f, r.Index) })
			s.trackScrollbar(pane)
		})
	})
}

// followPaneTitle 是左边顶上那一行的名字。
//
// ⚠️ **每一档都说得出是哪一档**：分组没拉到 / 拉失败了 / 停在「全部关注」/
// 停在某一组。Biu 上游那一支（`index.tsx:206`）在这几种情况下都塌成同一个
// 「我的关注」，而左上角突然换名字是最难自己发现的那一类毛病。
func followPaneTitle(f *followPage) string {
	if !f.tagStarted {
		return "我的关注"
	}
	if f.tab == followTabAll {
		return "全部关注"
	}
	for _, t := range f.tags {
		if followTagTab(t.ID) == f.tab {
			if t.Name != "" {
				return t.Name
			}
			return "这一组"
		}
	}
	// 分组被 B 站删掉了（我们停在一个已经不存在的那一格上）。说「这一组」而不是
	// 塌回「我的关注」：塌回去的话用户以为自己在看全部关注。
	return "这一组"
}

// viewFollowPaneRow 画左边的一行。
//
// 行身份是**这个分组是谁**（`followTabAll` / `followTagTab(id)`），不是序号
// （铁律四）。B 站的分组 id 可正可负，撞不上 `all` 那个串。
func (s *Shell) viewFollowPaneRow(c *ui.Context, f *followPage, i int) {
	t := c.Theme()

	tab, label := followTabAll, fmt.Sprintf("全部关注%s", countSuffix(f.total))
	if i > 0 && i-1 < len(f.tags) {
		tg := f.tags[i-1]
		// 分组 id 可正可负，身份带前缀（见 followTagTab）。
		tab, label = followTagTab(tg.ID), tg.Name+countSuffix(tg.Count)
		if label == "" {
			label = "这一组"
		}
	}

	box := ui.Row(c).Grow(1).Key("follow-tab:" + tab).Gap(6).Padding(6).Radius(6).
		AlignItems(ui.Center)
	if tab == f.tab {
		// 当前正在看的那一格：主色底，和侧栏选中收藏夹同一套颜色（见
		// sidebar_favorites.go 的 cur 那一支）。
		box = box.Background(t.Accent)
	} else if box.Hovered() {
		box = box.Background(t.SurfaceHover)
	}
	// ⚠️ **点了就换查询，而且换查询必须重置行**（见 resetUsers 的注释）。
	if box.Clicked() {
		s.selectFollowTab(f, tab)
	}
	box.Children(func() {
		ui.Text(c, label).Grow(1).FontSize(scale.Font(c, 13)).MaxLines(1).Ellipsis("…")
	})
}

// countSuffix 是 Tab 名字后面那个括号。**0 就不画**（上游 `index.tsx:224` 是
// `allCount ? … : ""`）——一个空的「()」在界面上像加载坏了。
func countSuffix(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("(%d)", n)
}

// selectFollowTab 切到某一格。
//
// ⚠️ **切到已经停着的那一格什么都不做**：清一次行的话，用户点回刚才看的那一格
// 就要重新拉一遍、滚动位置也回顶——而他没换任何条件。
func (s *Shell) selectFollowTab(f *followPage, tab string) {
	if f.tab == tab {
		return
	}
	f.tab = tab
	// ⚠️ **换分组顺手把搜索词放掉**：搜索框只在「全部关注」那一格画
	//（上游 `index.tsx:280`），而那个词是按「全部关注」那条接口搜出来的。
	// 留着它的话切回「全部关注」时用户会看到上次搜的那一批，而他记得自己没搜过。
	if tab != followTabAll {
		f.applied, f.draft = "", ""
	}
	f.gen++
	f.resetUsers()
}

// viewFollowSearch 是「全部关注」那一格顶上那个搜索框。
//
// ⚠️ **只在「全部关注」那一格画**，照上游（`index.tsx:280` 的
// `activeTab === "all" &&`）：B 站的搜索接口搜的是「我关注的人」的全集，
// 搜不出「某一组里的谁」——摆一个在那儿，用户搜一个人搜不到，而这个人就在
// 眼前这一组里。
//
// **回车才搜，不防抖**：上游就是 `onKeyDown Enter → onSearchSubmit`
// （`index.tsx:298-300`），没有 `useDebounceFn`（那是 later / history 两页的
// 做法）。所以这里也不加——加了就和上游不是一回事了，而多一个 500ms 的钟要
// 有人去停它。
func (s *Shell) viewFollowSearch(c *ui.Context, f *followPage) {
	if f.tab != followTabAll {
		return
	}
	t := c.Theme()
	ui.Row(c).Key("follow-search").FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		// ⚠️ Label 不能省：它不只是给测试用的——没有它读屏软件读不出这是一个搜
		// 关注对象的输入框（占位文字不是标签），无头 Tester 也只认 label / text。
		box := ui.TextInput(c, &f.draft).Grow(1).MaxWidth(keywordBoxWidth).
			Label(followSearchLabel).Placeholder(followSearchLabel)
		if box.Submitted() {
			s.searchFollow(f, strings.TrimSpace(f.draft))
		}
		// ⚠️ **清空按钮只在真的搜着的时候才在**：Biu 那个 `isClearable` 在框是空的
		// 时候也画一颗，画出来按下去什么也没发生。
		if f.applied != "" {
			ui.Text(c, "正在搜「"+f.applied+"」").FontSize(scale.Font(c, 12)).TextColor(t.TextMuted).
				MaxLines(1).Ellipsis("…")
			if ui.Button(c, "清空搜索").FontSize(scale.Font(c, 12)).Clicked() {
				f.draft = ""
				s.searchFollow(f, "")
			}
		}
	})
}

// searchFollow 把生效的词换掉，从第 1 页重来。
//
// ⚠️ **同一个词不重拉**：回车搜了「周杰伦」再回车一次，B 站会给一模一样的
// 20 条，而界面上那一瞬间是空的 + 清掉选中项。
func (s *Shell) searchFollow(f *followPage, keyword string) {
	if keyword == f.applied {
		return
	}
	f.applied = keyword
	f.gen++
	f.resetUsers()
}

// viewFollowGrid 是右边那个网格。
func (s *Shell) viewFollowGrid(c *ui.Context, f *followPage) {
	// ⚠️ **「拉失败了」必须排在「空」前面**：第一页失败时界面上是「没关注任何
	// 人」——一句话都不提失败，而 `started` 仍是 false、`err` 非 nil，切走再切回来
	// 也不会重拉，用户看到的是「我真的一个 UP 主都没关注」。
	if !f.pager.started && f.pager.err != nil {
		s.viewLoadError(c, "follow-err:users", "关注的 UP 主", "重试加载关注的人", f.pager.err, func() {
			f.pager.err = nil
			if ctx := f.pager.begin(); ctx != nil {
				go s.fetchFollowUsers(ctx, f, f.gen, f.tab, f.applied, f.pager.page, len(f.users), f.tabTotal())
			}
		})
		return
	}
	if !f.pager.started && f.pager.loading {
		// ⚠️ **走 `viewLoading`（和「没有结果」同一个出口）**：这一格原来是一句
		// 13pt 淡灰缩在左边，而「没关注任何人」是居中 15pt——用户等的那几秒里
		// 那一行字和回来之后那一格对不上（票 #138）。
		s.viewLoading(c, "follow:loading", "正在加载关注的人…")
		return
	}
	// 🔴 **「一格都没有」也分几种**：见下面 `followEmpty`。合成一句的话，用户
	// 会把「搜错了」当成「B 站没给他推荐」。
	if len(f.users) == 0 && !f.pager.hasMore {
		e := followEmpty(f)
		s.viewEmpty(c, "follow:"+followEmptyKey(f), e)
		return
	}

	w := f.width
	if w <= 0 {
		// 头一帧还没量出容器（`Bounds` 量的是上一帧的盒子），先拿窗口的宽顶着。
		// 那一帧的列数是**估的**，下一帧量到真的就换过去了——和 Biu 那边
		// ResizeObserver 晚一帧是同一个节奏。
		w, _ = c.Size()
	}
	cols := list.GridColumnsFor(w)
	// ⚠️ 必须在换掉 `f.cols` **之前**算锚点：keepFollowAnchor 里的行号是用**旧**
	// 的列数换算的。`f.cols = cols` 在这一段的末尾（画完之后），不在这里。
	s.keepFollowAnchor(f, cols)

	rows := (len(f.users) + cols - 1) / cols
	// 哨兵行：末尾多出来的那一行，滚到它就拉下一页。
	// **它占了行高**，所以「还在不在画面里」这件事才问得出来（和
	// `dynamicfeed.go` 的那一支同一个理由）。
	sentinel := f.pager.started && (f.pager.hasMore || f.pager.err != nil)
	n := rows
	if sentinel {
		n++
	}

	st := &f.state
	// 🔴 铁律四：**行的身份是这一行第一个人的 mid**，不是行号。行号是布局算出来
	// 的，换一次列数全变；而用户的滚动位置和选中状态是跟着身份走的。
	st.Key = func(i int) any {
		if i >= rows {
			return upSentinelKey{}
		}
		return followRowKey(f.users, i, cols)
	}
	st.Label = func(i int) string {
		if i >= rows {
			return "加载更多关注的人"
		}
		return followRowLabel(f.users, i, cols)
	}

	// ⚠️ Key 挂在外面的 Column 上，理由见 `dynamicfeed.go`。
	ui.Column(c).Key("follow-grid").Grow(1).FillHeight().FillWidth().Children(func() {
		e := ui.List(c, st, n).Grow(1).FillWidth().
			// ⚠️ **挂上 `ScrollState`**：这一格自己滚（那些卡片是方的，行高跟着内容
			// 走），而「回到顶部」那一枚要的是**像素**（上游的阈值是 400 像素）。
			// 这一页**没有 Tab**，列表不会被换掉，所以挂着它不会和行锚点打架
			//（理由见 `homepage.go` 里「切 Tab 的滚动位置」那一段）。
			TrackScroll(&f.scroll).
			Rows(func(r ui.ListRow) { s.viewFollowRow(r.Context, f, r.Index, cols) })
		s.trackScrollbar(e)
		// 量这一帧的容器宽，下一帧用。`Bounds` 报的是**上一帧**的盒子。
		if bw := e.Bounds().W; bw > 0 {
			f.width = bw
		}
	})
	f.cols, f.started = cols, true

	// 滚到底就拉下一页；容器没填满也拉（首屏那一拉在 view 开头已经发过了）。
	//
	// ⚠️ 闸门和代次都在**界面线程**上读（铁律一），后台那条线碰它就是数据竞争。
	if f.pager.want() {
		if _, last := st.Visible(); last >= n-1 {
			if ctx := f.pager.begin(); ctx != nil {
				go s.fetchFollowUsers(ctx, f, f.gen, f.tab, f.applied, f.pager.page, len(f.users), f.tabTotal())
			}
		}
	}
}

// followRowKey 是第 i 行的身份（铁律四）：这一行**第一个**人的 mid。
//
// ⚠️ 一行里塞的是 cols 张卡，而**两个一模一样的行身份**会让框架按 key 对位时
// 那几行共用一份状态——Tester 里当场 panic，真窗口里选中一格隔壁那格也亮着。
// 「这一行第一个人」跟着数据走，所以天然唯一（`seen` 已经把重复的人滤掉了）。
func followRowKey(users []bili.FollowUser, i, cols int) string {
	k := i * cols
	if k >= len(users) {
		return "follow:empty-row"
	}
	return "follow:" + strconv.FormatInt(users[k].Mid, 10)
}

// followRowLabel 是第 i 行给无障碍和「打字选中」用的名字。
func followRowLabel(users []bili.FollowUser, i, cols int) string {
	k := i * cols
	if k >= len(users) {
		return "加载更多关注的人"
	}
	if n := users[k].Name; n != "" {
		return n
	}
	return "这个 UP 主"
}

// keepFollowAnchor 是「换列数时滚动锚点保持」的那一段（理由见文件头）。
//
// 什么时候换锚：锚点守着的那个人**还在画面上就不换**（换列数不该把用户盯着的那
// 张卡换成别的）；他滚出画面了（用户自己滚走了）才换人守，换的是这一刻顶上那一
// 行的第一张卡。
//
// ⚠️ 记的是 mid 而不是行号：人在第几行是**算出来的**（下标除以列数），一取整
// 就丢。记行号的话每跨一档丢一次，而人拖窗口边正是一帧一帧跨过去的——
// 6→5→4→3→2 连丢四次，用户盯着的那张卡就整个掉出画面了。
func (s *Shell) keepFollowAnchor(f *followPage, cols int) {
	if !f.started || cols == f.cols || len(f.users) == 0 {
		return
	}
	first, last := f.state.Visible()
	if last < first {
		// 还没量出顶上那一行（头几帧，或者这一帧刚被换掉）。没有可记的锚点，
		// 硬滚反而会跳。
		return
	}
	j := followIndexOfMid(f.users, f.anchorMid)
	// ⚠️ 这里的行号都除以 f.cols —— **旧**的列数：人在旧布局里的位置就是那个下标。
	if j < 0 || j/f.cols < first || j/f.cols > last {
		if j = first * f.cols; j >= len(f.users) {
			return // 顶上那一行是空的（数据没了），没有可守的
		}
		f.anchorMid = f.users[j].Mid
	}
	f.state.ScrollTo(j/cols, ui.Start)
}

// followIndexOfMid 是这一页里那个 mid 的下标，没有是 -1。
func followIndexOfMid(users []bili.FollowUser, mid int64) int {
	if mid <= 0 {
		return -1
	}
	for i := range users {
		if users[i].Mid == mid {
			return i
		}
	}
	return -1
}

// viewFollowRow 画网格的一行：一行 cols 张卡。
//
// ⚠️ **整行要包一层 `ui.Grid`**：List 给每一行的容器是**竖着摆**的（那正是列表
// 的形状），不包的话六张卡会叠成一列——而这一页要的是网格。包法照
// `widgets/list/grid.go` 的 `viewRow`：`Columns(cols)` + `Gap(list.GridGap)`，
// **间距取的是那一份**，不然首页的 6 列和这里的 6 列在同一个窗口宽度下差好几像素。
//
// ⚠️ **每一格自己兜一层**（viewFollowCardGuarded）：格子里 panic 会一路穿出
// Cocoa 事件循环把整个进程带走。按行兜的话一行有六格，一张坏头像要带走五张
// 好的（理由同 `widgets/list/grid.go` 的 `viewRow`）。
func (s *Shell) viewFollowRow(c *ui.Context, f *followPage, i, cols int) {
	first := i * cols
	if first >= len(f.users) {
		s.viewFollowSentinel(c, f)
		return
	}
	// ⚠️ **Stretch**：格子要撑满这一行的高，卡的留白才跟着这一行算而不是自己
	// 缩成 0（理由同 `list.Grid.viewRow`）。
	ui.Grid(c).AlignSelf(ui.Stretch).Columns(cols).Gap(list.GridGap).Children(func() {
		for k := range cols {
			j := first + k
			if j >= len(f.users) {
				// 最后一行不满：剩下的格子空着（CSS grid 也是这么摆的，空格子
				// 不占位也不画边框）。
				return
			}
			s.viewFollowCardGuarded(c, f, j)
		}
	})
}

// viewFollowCardGuarded 画一张卡，画崩了只废掉这一张。
func (s *Shell) viewFollowCardGuarded(c *ui.Context, f *followPage, i int) {
	u := f.users[i]
	if f.failed[u.Mid] {
		ui.Text(c, "这一格画不出来").FontSize(scale.Font(c, 13)).TextColor(c.Theme().TextMuted).
			Padding(followCardPad).MaxLines(1)
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			if f.failed == nil {
				f.failed = map[int64]bool{}
			}
			f.failed[u.Mid] = true
			log.Printf("biu: 关注页那张卡画崩了（%d）：%v\n%s", u.Mid, rec, debug.Stack())
		}
	}()
	s.viewFollowCard(c, u)
}

// viewFollowCard 画一张卡。
//
// 结构照 Biu 的 `follow-list/user-card.tsx`：头像一块，下面昵称，再下面签名
// （`line-clamp-2`）。**整张卡点一下 = 跳进那个 UP 的空间**（上游 `onPress`
// 就是 `navigate(/user/${u.mid})`）。
//
// ⚠️ **认证那一块画成一行字而不是头像右下角那枚闪电**：上游那一枚是
// `RiVerifiedFill`，而我们的图标资源里**没有那一枚**（这一票补的是界面上一处一处
// 用得上的那一批，不是一个分类整个搬进来）。`official_verify.desc` 我们已经解出来
// 了（`bili/follow.go`），摆一行字是最诚实的画法。
func (s *Shell) viewFollowCard(c *ui.Context, u bili.FollowUser) {
	t := c.Theme()
	name := u.Name
	if name == "" {
		// 没有昵称就画一个占位，不是空串：空昵称在卡上是**一块什么都没有**，
		// 而这一帧用户正等着知道这是谁（理由同 `spaceName`）。
		name = "这个 UP 主"
	}

	// ⚠️ **格子的底色跟着这一格自己**（不是 `ui.List` 那一行的选中底）——行是
	// 布局，用户认的是「我点的是这张卡」。
	card := ui.Column(c).Grow(1).Radius(t.Radius).AlignSelf(ui.Stretch).
		AlignItems(ui.Center).Gap(6).Padding(followCardPad).
		OnClick(func() { s.navigate(upPathOf(u.Mid)) })
	if card.Hovered() {
		card = card.Background(t.SurfaceHover)
	}

	card.Children(func() {
		// 头像。上游是 `rounded-full`，所以是圆的（`spaceAvatar` 画的也是圆的，
		// 而且它四态齐全、还带 Label——无头 Tester 量得到）。
		s.spaceAvatar(c, u.Face, followCardSide)
		ui.Text(c, name).FontSize(scale.Font(c, 15)).MaxLines(1).Ellipsis("…")
		if u.Official != "" {
			ui.Text(c, u.Official).FontSize(scale.Font(c, 12)).TextColor(t.TextMuted).
				MaxLines(1).Ellipsis("…")
		}
		// ⚠️ **签名空的时候整块不画**：Biu 那一格是空的，我们这里留一个 6 的
		// 间距 + 一段空白，看起来像卡少了一截（理由同 `list.Grid` 里时长那一列）。
		if u.Sign != "" {
			ui.Text(c, u.Sign).FontSize(scale.Font(c, 12)).LineHeight(1.4).TextColor(t.TextMuted).
				TextAlign(ui.Center).MaxLines(2).Ellipsis("…")
		}
	})
}

// viewFollowSentinel 是末尾那一行：还有就拉，拉失败让人点重试。
func (s *Shell) viewFollowSentinel(c *ui.Context, f *followPage) {
	t := c.Theme()
	ui.Row(c).AlignSelf(ui.Stretch).Center().Gap(8).Padding(12).Children(func() {
		switch {
		case f.pager.err != nil:
			ui.Text(c, "加载失败："+f.pager.err.Error()).FontSize(scale.Font(c, 13)).
				TextColor(t.TextMuted).MaxLines(1).Ellipsis("…")
			if ui.Button(c, "重试加载关注的人").FontSize(scale.Font(c, 13)).Clicked() {
				f.pager.err = nil
				if ctx := f.pager.begin(); ctx != nil {
					go s.fetchFollowUsers(context.Background(), f, f.gen, f.tab, f.applied,
						f.pager.page, len(f.users), f.tabTotal())
				}
			}
		case f.pager.loading:
			ui.Spinner(c).Label("正在加载")
			ui.Text(c, "正在加载…").FontSize(scale.Font(c, 13)).TextColor(t.TextMuted)
		default:
			ui.Text(c, "继续往下滚，加载更多关注的人").FontSize(scale.Font(c, 13)).TextColor(t.TextMuted)
		}
	})
}

// followEmptyKey 是空状态的 key。带上是哪一种，于是**同一个 key 上不会换文案**
// ——读屏软件读到的就是同一句话。
func followEmptyKey(f *followPage) string {
	switch {
	case f.applied != "":
		return "search"
	case f.tab != followTabAll:
		return "tag"
	}
	return "none"
}

// followEmpty 是右边一格都没有时说的那句话。
//
// 「一个关注都没有」那一支**读路由表**（`router.go` 里 `/follow` 那一条，出处
// 和上游的对应都记在那儿），不在这儿抄一遍——抄一遍就是两句话各改各的。
func followEmpty(f *followPage) emptyState {
	switch {
	case f.applied != "":
		return followNoSearchResult
	case f.tab != followTabAll:
		return followEmptyTag
	}
	return lookup(followPath).Empty
}

// ─────────────────────────────────────────────────────────────────────────────
// 取数
// ─────────────────────────────────────────────────────────────────────────────

// fetchFollowTags 拉关注分组那一列。
//
// ⚠️ **在后台 goroutine 里跑，而且自己 recover**：界面那一层的 recover（格级的、
// 页级的）一个都接不到它——Go 里没人在跑那个 goroutine，而这一段正是「解析接口
// 返回」的地方。B 站改一个字段就是在这一行炸。
func (s *Shell) fetchFollowTags(ctx context.Context, f *followPage) {
	var r followResult
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("biu: 拉关注分组崩了：%v\n%s", rec, debug.Stack())
			r.err = fmt.Errorf("拉关注分组的代码崩了：%v", rec)
		}
		// 🔴 铁律三：投递完**必须**唤醒。只投递不唤醒 = 界面停在旧帧。
		f.boxMu.Lock()
		f.box = append(f.box, r)
		f.boxMu.Unlock()
		s.wake.invalidate()
	}()

	r = followResult{which: followTags, selfMid: s.app.SelfMid()}
	r.tags, r.err = s.app.FollowTags(ctx)
}

// fetchFollowUsers 拉右边那一格的一页。
//
// ⚠️ **gen / tab / keyword / page / loaded / count 全都是调用方（界面线程）在开
// 这一拉那一刻传进来的**，不是这里自己读的：闸门、代次和已加载的那些格都是界面
// 线程的状态，后台碰它就是数据竞争（铁律一）。
//
// 🔴 **gen 也在这一份里**——它是「换分组 / 换搜索词时 +1」的那个代次，而下面
// 拿它填进 `followResult` 就是**读**它。第一版这里是 `gen: int(f.gen)`，直接
// 在 goroutine 里读界面线程正在写的那个字段：`go test -race` 一抓一个准
// （Read at follow.go:889 / Previous write at selectFollowTab）。
//
// 🔴 **loaded 在这儿是有用的，不是多传**：B 站那三个接口给的是**总数**，而
// 「还有没有」要拿**累计加载数**去比总数——总数 25、每页 20，第二页发回来 5 个，
// 拿这一页的 5 个去比 25 永远是「还有」，于是界面上一直转圈、一直拉空页。
//
// 而「累计加载了多少」是界面状态，后台读它就是竞争。所以它在**界面线程上**取一
// 份带过来（和 `library_rows.go` 的 `load` 传 `loaded` 是同一个做法）。
func (s *Shell) fetchFollowUsers(ctx context.Context, f *followPage, gen uint64, tab, keyword string, page, loaded, count int) {
	var r followResult
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("biu: 拉关注的人崩了：%v\n%s", rec, debug.Stack())
			r.err = fmt.Errorf("拉关注的人的代码崩了：%v", rec)
		}
		f.boxMu.Lock()
		f.box = append(f.box, r)
		f.boxMu.Unlock()
		s.wake.invalidate()
	}()

	r = followResult{which: followUsers, selfMid: s.app.SelfMid(), gen: int(gen), tab: tab, keyword: keyword}
	self := s.app.SelfMid()

	if tagID, ok := tagIDOf(tab); ok {
		// ⚠️ 分组明细那个接口**不报总数**，所以总数那一项用的是分组自己的 count
		//（理由见 `bili/follow.go` 文件头）。它在 `f.tags` 里，而那是界面状态——
		// 所以**也在界面线程上取一份**带进来（`count` 那一个参数）。
		users, err := s.app.FollowTagUsers(ctx, tagID, page+1)
		r.err = err
		if err == nil {
			r.users, r.total = users, count
		}
		return
	}

	var pg bili.FollowUsersPage
	if keyword != "" {
		pg, r.err = s.app.SearchFollowings(ctx, self, keyword, page+1)
	} else {
		pg, r.err = s.app.FollowList(ctx, self, page+1)
	}
	if r.err != nil {
		return
	}
	r.users, r.total = pg.Users, pg.Total
}

// tabTotal 是**分组那一格**一共多少人。**只由界面线程调**（它读 `f.tags`）。
//
// ⚠️ 「全部关注」和搜索那两格的总数是**接口自己报的**，在结果回来之后才拿到；
// 这里的返回值只喂给分组明细那一支（`FollowTagUsers` 不报总数，理由见
// `bili/follow.go` 文件头）。停在别的格子上时给 0，而那一支也用不到它。
//
// ⚠️ 分组列表还没拉到、或者这一格已经从列表里消失时给 0。0 的意思是「后面
// 没有」——那一格于是画完一页就停，不会一直拉空页。
func (f *followPage) tabTotal() int {
	tagID, ok := tagIDOf(f.tab)
	if !ok {
		return f.total
	}
	for _, t := range f.tags {
		if t.ID == tagID {
			return t.Count
		}
	}
	return 0
}

// adoptFollow 收走后台投回来的结果。**只能由界面线程调**。
func (s *Shell) adoptFollow(f *followPage) {
	f.boxMu.Lock()
	box := f.box
	f.box = nil
	f.boxMu.Unlock()
	if len(box) == 0 {
		return
	}

	for _, r := range box {
		// ⚠️ **`settle` / 清 loading 必须无条件执行**：请求失败、超时、返回不对、
		// 拉取的代码自己崩了，全走这一条。漏了它那一格会永远停在「正在加载」。
		if r.selfMid != f.selfMid {
			// 换账号之前发出去的那一页（`stop` 停不掉已经在回来的那一页）。
			if r.which == followTags {
				f.tagLoading = false
			} else {
				f.pager.settle()
			}
			continue
		}
		if r.which == followTags {
			f.tagLoading = false
			if r.err != nil {
				f.tagErr = r.err
				log.Printf("biu: 拉关注分组失败了：%v", r.err)
				continue
			}
			f.tags, f.tagStarted, f.tagErr = r.tags, true, nil
			continue
		}

		// 右边那一格：代次对不上就是**用户已经换分组 / 换词了**，这一页作废。
		if r.gen != int(f.gen) || r.tab != f.tab || r.keyword != f.applied {
			f.pager.settle()
			continue
		}
		f.pager.settle()
		if r.err != nil {
			f.pager.err = r.err
			log.Printf("biu: 拉关注的人失败了：%v", r.err)
			continue
		}
		// 🔴 铁律四：追加是**尾部**，不动已有格的位置，所以「正在看的那张卡」
		// 翻完页还是它。
		if f.seen == nil {
			f.seen = map[string]bool{}
		}
		for _, u := range r.users {
			k := "follow:" + strconv.FormatInt(u.Mid, 10)
			if f.seen[k] {
				continue
			}
			f.seen[k] = true
			f.users = append(f.users, u)
		}
		// ⚠️ **「全部关注(N)」那个 N 只在不搜的时候换**：搜出来的 total 是搜到
		// 几个，把它挂在「全部关注」旁边就是一句假话。
		if r.tab == followTabAll && r.keyword == "" {
			f.total = r.total
		}
		// 🔴 **「还有没有」在这里算，而且拿累计加载数去算**（理由见
		// `fetchFollowUsers` 顶上那一段）。两个条件缺一不可：
		//   - 这一页**一个都没发** → 到头了。B 站在页码超了的时候给的是空 list
		//     而不是报错，而总数不会因此变，于是光比「累计 < 总数」会永远转圈。
		//   - 累计还没追上总数 → 还有。
		//
		// ⚠️ 第二个参数是 `len(r.users)`，**不是**「去重之后新收进来几个」。
		// `upPager.adopt` 里那个 `sent` 说的是「B 站发了几个、过滤**之前**」
		//（见 `upspace.go` 的 `adopt`：一页全是转发的动态、全被丢弃时留下 0 条，
		// 拿 0 去判就成了「到底了」）。传去重后的个数是同一个坑的另一种形状：
		// **B 站翻页整页重叠**时那 20 个人全是上一页那些、去重后一个不留，于是
		// 「还有没有」被判成「没有了」——而第 3 页上还有 19 个新的。
		// 症状：Tab 上写着 (59)、画面上只有 20 张卡，而且**没有加载更多的入口**
		// （末尾哨兵那一行也不画了）。
		f.pager.adopt("", len(r.users), len(r.users) > 0 && len(f.users) < r.total)
	}
}
