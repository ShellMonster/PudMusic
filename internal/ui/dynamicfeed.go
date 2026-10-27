// 用户动态那一页：左边「我关注的人」，右边他们发的什么。
//
// # 左边那一栏点一下是**跳进那个 UP 的空间**，不是过滤右边
//
// Biu 上游 `dynamic-feed/author-list.tsx` 点作者是把右边的流换成那个人的
// （`handleSelectAuthor` → `getWebDynamicFeedSpace`）。这一版**跳 `/user/{mid}`**
// ——验收写的就是「左侧的作者栏能翻页、**能点进对应 UP 空间**」，而 UP 空间
// 那一页的「动态」Tab 正是这个人的全部动态，跳过去之后想看照样看得到，
// 还能顺手关注一下。过滤那一层留给以后：它是 Biu 的行为，不是这一版的验收。
//
// 第一行是「全部」，选中它就是**不筛**（这一页的本来的样子）。
//
// # 那一栏翻的是「我关注的人」，所以要的是**自己的** mid
//
// 不是页面上那个 UP 的（那个在路由参数里）。没登录就没有这个列表——
// B 站那个接口要 Cookie，返回 -101，而 -101 在这一层和「网络断了」长得
// 一模一样。所以界面上先判登录态，别发那个注定失败的请求。
//
// # 🔴 铁律四：两栏的行都要有稳定且唯一的 key
//
// 右边是动态的 `id_str`，左边是 `auth:<mid>`（「全部」是 `auth:0`，B 站的
// mid 不会是 0，撞不上）。理由和 UP 空间那边一样，见 upspace.go 的文件头。
package ui

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"strconv"
	"sync"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/rowkey"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// 这一页的路由常量（`dynamicFeedPath`）在 navbar_feed.go：顶栏那个「动态」
// 按钮也是往同一个地址跳，两边共用一份才是「点顶栏和点侧栏到的是同一页」。

// authorPaneW 是左边那一栏的宽度。上游是 `w-64`（`author-list.tsx:106`），
// Tailwind 的 64 = 256。
const authorPaneW = 256

// authorAvatarS 是左边那一栏每行头像的边长。上游是 `h-9 w-9`（`user.tsx:46`），
// 9 × 4 = 36。
const authorAvatarS = 36

// paneDividerA 是两栏之间那条分界线的浓度。上游写的是
// `border-divider/40`（`author-list.tsx:107`、`follow-list/index.tsx:204`），
// 也就是分隔线本身再乘 40%。
const paneDividerA = 0.4

// allAuthorsMid 是左边第一行「全部」那一行的 mid。
//
// **用一个页面里不可能出现的数**，而不是空串或者 -1：那一行的身份要和
// 关注的人的行身份在同一个命名空间里（B 站的 mid 是正数），0 撞不上。
const allAuthorsMid = 0

// dynamicFeed 是「用户动态」那一页的状态。
//
// 只有一份（这一页没有参数），跟着壳走。**只由界面线程读写**，没有锁；
// 后台那一条线只往 box 那个带锁的信箱里投。
type dynamicFeed struct {
	// items 是右边已经拿到的动态。
	items []bili.Dynamic
	// authors 是左边已经拿到的关注的人。
	authors []bili.FollowAuthor

	// state 是右边那一栏的列表状态，authState 是左边的。**不能共用**：
	// MyGo 会在「同一个 ListState 被两个 List 显示」时 panic。
	state     ui.ListState
	authState ui.ListState
	// itemRowKeys / authRowKeys 记着两栏每一行的身份（**预先装箱好的那一份**）。
	//
	// ⚠️ 装箱那一步是要分配的（`runtime.convTstring`），现算的话每帧每行一次
	// （票 #313）。**它自己知道「数据换了」**（见 `rowkey.Memo` 的文件头）：
	// 翻页追加、刷新时新的动态插到前面——身份对不上就重算。
	itemRowKeys rowkey.Memo[string, any]
	authRowKeys rowkey.Memo[int64, any]
	// keys 是右边那一栏的键盘接线（游标 + 「↑ 到顶回侧栏」）。
	//
	// ⚠️ **这一栏是裸的 `ui.List`**（卡片是自己画的，走不了 `widgets/list`
	// 那个包），所以游标和那一档回调得自己接——不然方向键在内容区里只滚动
	// 不选行，而 ↑ 也回不到侧栏（票 #245 的 E4）。
	keys listKeys

	// feed 是右边、auth 是左边的翻页闸门（upPager，见 upspace.go）。
	feed upPager
	auth upPager

	// seen 是**已经收进来的身份**，挡翻页重叠（和 `upSpace.seen` 同一个理由：
	// 两个一模一样的行身份会让那几行共用一份状态）。
	seen map[string]bool
	// drops 是右边丢掉的那几条（**按不同的动态记**，见 drops.go）。不画在
	// 界面上，只用来让「一个格子都没有」能说清是「真的没有」还是「他发的
	// 那些我们画不出来」。
	//
	// ⚠️ **按类别分开记**：界面上那句话要按类别说，「读不懂」不能被说成
	// 「形态不认识」（票 #344）。
	drops dropTally
	// started 是右边已经拉过至少一页了。
	started bool
	// selfMid 是**这份缓存是为哪个账号拉的**。作者栏和「关注的动态」都是
	// 按账号的，账号一变这一页整个作废（见 resetIfAccountChanged）。
	selfMid int64

	// 跨 goroutine 的只有这一小摊信箱。
	boxMu sync.Mutex
	box   []feedResult
}

// feedResult 是后台拉完之后投进信箱的东西。
type feedResult struct {
	which int
	// selfMid 是发这一次时的账号。换账号之后回来的那一页**作废**——
	// `stop` 能停请求，但停不掉已经在回来的那一页。
	selfMid int64 // feedAuthors / feedItems
	dyns    []bili.Dynamic
	authors []bili.FollowAuthor
	cursor  string
	// sent 是 B 站这一页发了几条（过滤之前）。见 upPager.adopt。
	sent    int
	hasMore bool
	// drops 是这一页丢掉的那几条（带类别和身份）。
	//
	// ⚠️ **身份是给上层去重用的**：B 站翻页会重叠，同一条画不出来的动态在
	// 两页里各出现一次——按出现次数记，那个数会偏大（票 #344）。
	drops []bili.DroppedDynamic
	// dropWhy 是丢掉的理由（人话，一种一句）。**只进日志**，不进界面。
	dropWhy []string
	err     error
	// baseline 是这一页的「更新基线」（= 最新那条动态的 id）。只有**第一页**有
	// （翻页的那些页里最新的一条更旧）。
	//
	// ⚠️ **它是动态 id，不是时间戳**：B 站「有没有新动态」那个接口
	// （/feed/all/update）的 update_baseline 要的就是它（票 #480）。
	baseline string
}

// 这一页的两条闸门各自是哪一条。
const (
	feedItems = iota
	feedAuthors
)

// dynamicFeedOf 是这一页的状态，第一次问的时候现建。
func (s *Shell) dynamicFeedOf() *dynamicFeed {
	if s.feedPage == nil {
		s.feedPage = &dynamicFeed{}
	}
	return s.feedPage
}

// resetIfAccountChanged 在**账号变了**时把这一页整个作废。
//
// 为什么必须整个作废而不是只清作者栏：左边列的是「我关注的人」，右边
// `/feed/all` 也是「我关注的人的动态」——两样都是**按账号**的。换账号后
// 留着上一份的话，用户在新账号下看的是旧账号的内容，而界面上没有任何
// 东西提示这一点。
//
// 顺带把那两条在途请求作废掉（闸门 `stop` 会 cancel + 清掉状态）。
func (s *Shell) resetIfAccountChanged(f *dynamicFeed) {
	cur := s.app.SelfMid()
	if f.selfMid == cur {
		return
	}
	f.selfMid = cur
	f.auth.stop()
	f.feed.stop()
	f.auth = upPager{}
	f.feed = upPager{}
	f.authors = nil
	f.items = nil
	f.drops = dropTally{}
	f.started = false
	f.seen = nil
}

// feedPager 是这一栏的闸门。
func (f *dynamicFeed) feedPager(which int) *upPager {
	if which == feedAuthors {
		return &f.auth
	}
	return &f.feed
}

// viewDynamicFeed 画「用户动态」那一页。
//
// ⚠️ **外面已经包了一层 recover**（`router.go` 的 `Shell.page`）：MyGo 的
// Dispatch 和 drain 都没有 recover，view 里 panic 会穿透 Cocoa 事件循环
// **直接杀死进程**。
func (s *Shell) viewDynamicFeed(c *ui.Context, _ *ui.Route) {
	// 🔴 铁律二：每帧第一件事就是装主题，而且必须在最开头。
	s.applyFrameTheme(c)

	f := s.dynamicFeedOf()

	ui.Row(c).Key("page:" + dynamicFeedPath).Grow(1).FillHeight().Children(func() {
		s.viewAuthorPane(c, f)
		// 动态卡片左右各 32：上游是外层 `px-4` 再套一层 `px-4`
		// （`index.tsx:142` 的 ScrollContainer + 里面那个 `px-4 py-4`），
		// 16 + 16 = 32。卡自己那一条的左右内边距因此是 0。
		//
		// ⚠️ **上沿那 16 是同一个 `py-4`**：上游那一层 `px-4 py-4` 上下也是 16，
		// 而 UP 空间那一页的 Tab 面板是 `px-0 py-4`——**两页内容离上一段的距离
		// 是同一个数**（票 #416：「两页口径一致」）。
		ui.Column(c).Key("feed-items").Grow(1).FillHeight().
			Padding(space.Px16, space.Px32, space.None, space.Px32).Children(func() {
			s.viewFeedItems(c, f)
		})
	})
}

// syncDynamicFeed 是「用户动态」这一页每帧的副作用：收后台投回来的结果、换账号
// 就把这一页整个换掉、起该起的那两拉（作者栏 + 动态列表）。
//
// ⚠️ **它们原来散在这一页的 view 和作者栏那一栏里**，而这一页在满屏开着的时候
// 不画（票 #161）——不挪的话，用户在满屏里看频谱的那几分钟里，动态列表的后台
// 结果一直躺在信箱里没人取。
func (s *Shell) syncDynamicFeed(c *ui.Context) {
	f := s.dynamicFeedOf()
	s.adoptFeed(f)

	// 🔴 **换账号了就把这一页整个换掉**：作者栏列的是「**我**关注的人」，
	// 而整个 `dynamicFeed` 是挂在壳上的一份缓存 —— 不换的话退出 A 登录 B，
	// 左边仍然是 A 关注的人（实测：SelfMid 已经是 88，界面上还是「账号77
	// 专属作者」，接口一次都没再请求）。
	s.resetIfAccountChanged(f)

	// 首屏那两拉，**必须在画之前发出去**。画完再发的话这一帧画出来的是
	// 「正在加载」，要等这一页回来才换掉——而那一段没有任何东西会叫醒界面。
	//
	// ⚠️ **只在「一页都还没拉过」时发**（理由同 `viewSpaceList` 里那一段）：
	// 这一段每帧都跑，不设这道门就是第一页一落地、下一帧又拉一页，用户
	// 什么都没滚就把后面的页全拉完了。
	//
	// ⚠️ **cursor 和 page 在这里读**（界面线程），不在后台那条线上读：
	// 闸门是界面线程的状态（铁律一），后台碰它就是数据竞争。
	if !f.started && !f.feed.loading {
		if ctx := f.feed.begin(); ctx != nil {
			go s.fetchFeed(ctx, f, feedItems, f.feed.cursor, f.feed.page)
		}
	}

	// ⚠️ **作者栏那一拉要的是「我关注的人」，没登录时那个接口一定回 -101**，
	// 而 -101 在这一层和「网络断了」长得一模一样。发出去的话那一栏会一直亮着
	// 转圈，而界面上看不出原因是「你没登录」。
	if s.app.LoggedIn() && !f.auth.started && f.auth.err == nil {
		if ctx := f.auth.begin(); ctx != nil {
			go s.fetchFeed(ctx, f, feedAuthors, f.auth.cursor, f.auth.page)
		}
	}
}

// viewAuthorPane 是左边那一栏：标题 + 「全部」+ 关注的人 + 哨兵。
func (s *Shell) viewAuthorPane(c *ui.Context, f *dynamicFeed) {
	t := c.Theme()
	ui.Column(c).Key("feed-authors").Width(authorPaneW).FillHeight().
		Padding(space.Px8, space.Px8).Gap(space.Px4).
		BorderWidth(0, 1, 0, 0).BorderColor(t.Border.Alpha(paneDividerA)).Children(func() {
		// 上游这一栏的标题就是「用户动态」（`author-list.tsx:126`），左右
		// 再缩 16（`px-4`）；这一栏自己已经有 8 的横向内边距，所以这里
		// 只再补 8。
		// ⚠️ **字重不写**（400）：这一栏的标题是 14/400（DESIGN.md §二）。
		ui.Text(c, "用户动态").FontSize(scale.Font(c, scale.Size14)).PaddingX(space.Px8)
		if !s.app.LoggedIn() {
			// 没登录**不假装有内容**：作者栏列的是「我关注的人」，没有登录态
			// 就没有这个列表。给一句能指路的，比一个空列表有用。
			ui.Text(c, "登录之后能看到你关注的人").FontSize(scale.Font(c, scale.Size12)).
				TextColor(t.TextMuted).PaddingX(space.Px8)
			// ⚠️ **再给一个能立刻做完的事**（票 #277）：这一页没登录时作者栏
			// 整个拉不出来，而「去登录」是唯一能解开它的动作——只有一句
			// 「登录之后能看到你关注的人」的话，用户得自己再去顶栏找那枚图标。
			//
			// ⚠️ **它带一个「登录完去哪」**：从这一页进去的，登录完**回这一页**
			// ——用户就是来看动态的，登录完落回首页等于把他白绕一圈
			// （这一条是票 #277 点名的那个例外：别的入口回首页）。
			//
			// ⚠️ **读屏的名字要能和侧栏那一枚分清**：侧栏收藏夹区没登录时也有
			// 一枚「去登录」，归宿是首页（`sidebar_favorites.go` 的
			// `favEmptyAction`）。两枚同名不同宿，读屏软件念出来是一样的——
			// 这里给它一个说出去哪的名字。测试定位这一枚也靠它
			// （`TestLoginFromTheDynamicPageComesBackToTheDynamicPage`）。
			if ui.Button(c, "去登录").Label("去登录，登录后回到动态页").Clicked() {
				s.goLogin(dynamicFeedPath)
			}
			return
		}

		// 登录态过了才拉第一页。理由见 viewDynamicFeed 里那一段。
		//
		// ⚠️ **那一拉挪到 `syncDynamicFeed` 那一层了**（票 #161）：满屏开着的时候
		// 这一页不画，而作者栏这一趟不能跟着一起停。

		// ⚠️ **作者栏第一页拉失败也要说失败**：上面那一支是「登录之后能看到
		// 你关注的人」，那是**没登录**的话；而拉失败了界面上一句话都不提，
		// 切走再切回来也不会重拉（`want()` 因为 `err != nil` 一直挡着）。
		auth := &f.auth
		if !auth.started && auth.err != nil {
			s.viewLoadError(c, "feed-err:authors", "关注的人", "重试加载关注的人", auth.err, func() {
				auth.err = nil
				if ctx := auth.begin(); ctx != nil {
					go s.fetchFeed(ctx, f, feedAuthors, auth.cursor, auth.page)
				}
			})
			return
		}
		sentinel := auth.started && (auth.hasMore || auth.err != nil)
		rows := len(f.authors) + 1 // 「全部」那一行永远在
		if sentinel {
			rows++
		}

		st := &f.authState
		// 🔴 铁律四：行身份是「这一行是谁」，不是序号。「全部」是
		// `auth:0`，关注的人是 `auth:<mid>`（B 站的 mid 不会是 0，撞不上）。
		// ⚠️ 身份在 `authRowKey` 里算——它自己记着那一份（见那个函数的注释）。
		st.Key = func(i int) any { return authRowKey(f, i) }
		st.Label = func(i int) string { return authRowLabel(f, i) }

		// ⚠️ Key 挂在外面的 Column 上：List 是自己管状态的控件，事后再挂
		// key 会 panic（`ui/element.go` 的 `Key`）。理由见 upspace_tabs.go。
		ui.Column(c).Key("feed-author-list").Grow(1).FillHeight().Children(func() {
			// 登记「内容区里当前那一个能被方向键走位的」（票 #245 的 E4）。
			// 作者栏先登记、动态栏后登记，于是动态栏赢——它是这一页的主内容。
			s.registerContentFocus(list.HandleFocus(&st.Handle))
			authorList := ui.List(c, st, rows).
				Grow(1).FillHeight().
				Rows(func(r ui.ListRow) {
					defer func() {
						if rec := recover(); rec != nil {
							log.Printf("biu: 作者栏那一行画崩了：%v\n%s", rec, debug.Stack())
						}
					}()
					s.viewAuthorRow(r.Context, f, r.Index)
				})
			s.trackScrollbar(authorList)
		})

		if auth.want() {
			if _, last := st.Visible(); last >= rows-1 {
				if ctx := auth.begin(); ctx != nil {
					go s.fetchFeed(ctx, f, feedAuthors, auth.cursor, auth.page)
				}
			}
		}
	})
}

// authRowKey 是作者栏第 i 行的身份（铁律四）。
func authRowKey(f *dynamicFeed, i int) any {
	if i <= 0 {
		return "auth:0"
	}
	if i-1 >= len(f.authors) {
		return upSentinelKey{}
	}
	// ⚠️ **身份是预先装箱好的那一份**（`f.authRowKeys`）：理由见那个字段的注释。
	//
	// ⚠️ **按「第几个人」记**：那一行的身份跟着 mid 走，而 mid 不会因为多了一个
	// 人而变——只有换了人才变。
	return f.authRowKeys.At(i-1, f.authors[i-1].Mid, authIdentity)
}

// authIdentity 是作者栏一行的身份（铁律四）：`auth:<mid>`。
//
// ⚠️ **「全部」那一行不走这里**：它的身份是 "auth:0"，而那一行在
// `authRowKey` 的 `i <= 0` 那一支就直接给了——**压根不进 memo**。所以这里
// 的 raw 永远是「某一个人」的 mid，而 B 站的 mid 不会是 0（撞不上 "auth:0"）。
//
// 这一步会分配（拼字符串 + 装箱）——所以它只在 mid 变了的时候才走
// （见 `rowkey.Memo` 的文件头）。
func authIdentity(mid int64) any {
	return "auth:" + strconv.FormatInt(mid, 10)
}

// authRowLabel 是作者栏第 i 行给无障碍用的名字。
func authRowLabel(f *dynamicFeed, i int) string {
	switch {
	case i <= 0:
		return "全部"
	case i-1 < len(f.authors):
		return f.authors[i-1].Name
	}
	return "加载更多关注的人"
}

// viewAuthorRow 画作者栏的一行。
//
// **点一行 = 跳进那个 UP 的空间**（见文件头）。「全部」那一行点了不动——
// 它本来就是这一页本来的样子。
func (s *Shell) viewAuthorRow(c *ui.Context, f *dynamicFeed, i int) {
	t := c.Theme()

	if i >= len(f.authors)+1 {
		// ⚠️ **`auth := f.auth` 是复制，不是指针**（早一版就这么写的）：
		// 复制品上的 `err = nil` / `begin()` 全都写在副本上，真实的闸门
		// `loading` 仍是 false、`err` 仍非 nil —— 于是界面上那句失败一直挂着、
		// 按钮一直可点，而每点一次就**并发发出一个重复的 pn 请求**
		//（两次请求的回来顺序不保证，后到的把先到的结果盖掉）。
		auth := f.feedPager(feedAuthors)
		s.viewFeedSentinel(c, auth, "继续往下滚，加载更多关注的人", "重试加载关注的人", func() {
			auth.err = nil
			if ctx := auth.begin(); ctx != nil {
				go s.fetchFeed(ctx, f, feedAuthors, auth.cursor, auth.page)
			}
		})
		return
	}

	all := i == 0
	name := "全部"
	face := ""
	mid := int64(allAuthorsMid)
	if !all {
		a := f.authors[i-1]
		name, face, mid = a.Name, a.Face, a.Mid
		if name == "" {
			name = "这个 UP 主"
		}
	}

	box := ui.Row(c).Grow(1).Key("feed-author:" + strconv.FormatInt(mid, 10)).
		Gap(space.Px12).Padding(space.Px8).Radius(t.Radius).AlignItems(ui.Center)
	// ⚠️ hover 要在 Children **之前**问（理由同 `widgets/list` 的 grid.go）。
	on := box.Hovered()
	if all {
		// 「全部」是**当前就在看的那一个**：和侧栏菜单当前项**同一套**画法
		// （`flatCurrent`——中性灰底 + 主色的字，深浅两套同一个数）。
		//
		// ⚠️ **原来这里是实心主色**（`Background(t.Accent)`，一块 248×50 的实心
		// 绿）：那是审计 F-02 / F-06 点名的那一处——同一个屏上左栏是实心绿、
		// 侧栏是淡底，两种画法并排，而实心那一块是那一页纯主色像素最多的地方。
		box = flatCurrent(c, box)
	} else if on {
		box = box.Background(t.SurfaceHover)
	}
	if box.Clicked() && !all {
		s.navigate(upPathOf(mid))
	}
	box.Children(func() {
		if all {
			// 「全部」没有头像。给一个方块底子，一眼能认出「这不是某个人」。
			ui.Box(c).Size(authorAvatarS, authorAvatarS).Radius(t.Radius).Background(t.Surface)
		} else {
			// ⚠️ **指针移上去时那个圆头像放大 5%**：上游作者栏那一行是
			// `<Avatar className="h-9 w-9 shrink-0 rounded-full transition-transform
			// hover:scale-105">`（`dynamic-feed/user.tsx:46`）。放大的是**整个圆**
			// （上游那个类加在 Avatar 上），不是里面那张图。
			s.spaceAvatarZoom(c, face, authorAvatarS, false,
				"feed-author-zoom:"+strconv.FormatInt(mid, 10), on)
		}
		ui.Text(c, name).Grow(1).FontSize(scale.Font(c, scale.Size14)).MaxLines(1).Ellipsis("…")
	})
}

// viewFeedItems 是右边那一片动态。
func (s *Shell) viewFeedItems(c *ui.Context, f *dynamicFeed) {
	feed := &f.feed

	// ⚠️ **「拉失败了」必须排在「空」前面**：反过来的话第一页拉失败时
	// 界面上是「暂无动态」——一句话都不提失败，而 `started` 仍是 false、
	// `err` 非 nil，切走再切回来也不会重拉，用户看到的是「关注的人
	// 真的什么都没发」。
	if !f.started && feed.err != nil {
		s.viewLoadError(c, "feed-err:items", "动态列表", "重试加载动态", feed.err, func() {
			feed.err = nil
			if ctx := feed.begin(); ctx != nil {
				go s.fetchFeed(ctx, f, feedItems, feed.cursor, feed.page)
			}
		})
		return
	}
	if !f.started && feed.loading {
		// 一条都都没有：第一页在途。
		s.applyFrameTheme(c)
		drawPostSkeletons(c, "feed:loading", "正在加载动态…")
		return
	}
	// 🔴 **「一行都没有」也分两种**：没有下一页了 → 画空状态；**还有下一页**
	// → 继续画哨兵那一行。实测踩过的坑：一页全是画不出来的（全被丢弃）时
	// 留下 0 行，早一版在这里画空状态并 return，哨兵根本没被画出来，
	// 「滚到底」和「容器没填满」两条保障一个都问不到 —— 后面的有效动态
	// 永远不再加载。
	keepLooking := f.started && feed.hasMore
	if len(f.items) == 0 && !keepLooking {
		s.viewEmpty(c, "feed:"+feedEmptyKey(f), feedEmpty(f))
		return
	}

	// 哨兵行：末尾多出来的那一行，滚到它就拉下一页。
	// **它占了行高**，所以「还在不在画面里」这件事才问得出来（Biu 的
	// `count = hasMore ? items.length + 1 : items.length`）。
	sentinel := f.started && (feed.hasMore || feed.err != nil)
	rows := len(f.items)
	if sentinel {
		rows++
	}

	st := &f.state
	// 🔴 铁律四：行身份是 B 站的 `id_str`，不是序号。动态是往尾部追加的，
	// 而刷新时新动态会插到**前面**——拿序号当身份的话，用户盯着的那一条
	// 会整个换成旁边那一条，而界面上看不出任何异样。
	st.Key = func(i int) any {
		if i >= len(f.items) {
			return upSentinelKey{}
		}
		// ⚠️ **身份是预先装箱好的那一份**（`f.itemRowKeys`）：理由见那个字段的注释。
		return f.itemRowKeys.At(i, f.items[i].ID, rowkey.Identity)
	}
	st.Label = func(i int) string {
		if i >= len(f.items) {
			return "加载更多动态"
		}
		return feedRowLabel(f.items[i])
	}

	// ⚠️ Key 挂在外面的 Column 上，理由见上面那一处。
	ui.Column(c).Key("feed-dynamic-list").Grow(1).FillHeight().Children(func() {
		// 「有 N 条我们画不出来」那一行（理由同 upspace_tabs.go 那一处）。
		//
		// ⚠️ **只在「一条都没画出来」时出现**：有内容的时候插一行字只是
		// 噪音；而一条都没有的时候，用户看到的是**一片空白加一个转着的
		// 圈**——像是坏了，而真相是「他们发的那些我们画不出来」。
		if len(f.items) == 0 && f.drops.total() > 0 {
			s.viewDroppedNote(c, f.drops.counts)
		}
		// 登记「内容区里当前那一个能被方向键走位的」（票 #245 的 E4）。
		//
		// ⚠️ **登记的是 `&f.keys` 而不是裸的句柄**：这一份的 `TakeFocus` 还要
		// 把游标摆到第一行（走进来就在第一条上）。
		s.registerContentFocus(&f.keys)
		// 键盘接线：游标 + 「↑ 到顶回侧栏」（票 #245 的 E4）。
		f.keys.wire(c, st, s.focusSide)
		dynList := ui.List(c, st, rows).
			Grow(1).FillHeight().FillWidth().
			// ⚠️ **听一听输入**：为了知道「键盘往回走到顶上了」。
			// 返回 false 是刻意的：自己不吃这个事件，方向键挪游标、点击选行、
			// Tab 换焦点都还在。
			HandleInput(f.keys.keys.Input).
			// 焦点环只留在「还没选中任何一行」的时候（和 `widgets/list` 同一档）。
			FocusRing(f.keys.sel < 0).
			Rows(func(r ui.ListRow) {
				defer func() {
					if rec := recover(); rec != nil {
						log.Printf("biu: 动态那一行画崩了：%v\n%s", rec, debug.Stack())
					}
				}()
				if r.Index >= len(f.items) {
					s.viewFeedSentinel(r.Context, feed, "继续往下滚，加载更多动态", "重试加载动态", func() {
						feed.err = nil
						if ctx := feed.begin(); ctx != nil {
							go s.fetchFeed(ctx, f, feedItems, feed.cursor, feed.page)
						}
					})
					return
				}
				s.viewDynamicCard(r.Context, f.items[r.Index],
					f.keys.selected(r.Index), f.keys.ring(r.Context))
			})
		// ⚠️ **行间距由列表给**（上游 `mb-2` = 8）：卡片自己的下外边距**不能**
		// 留着——那一段属于**行**（不在卡片里），而框架给「指针压着的那一行」铺
		// 一层 `t.SurfaceHover`，留着它卡片底下就露出那一层（暖纸主题下是淡棕）。
		// 理由和 `upspace_tabs.go` 里那一处一样（票 #416）。
		dynList = dynList.GapY(space.Px8)
		s.trackScrollbar(dynList)
	})

	// 滚到底就拉下一页；容器没填满也拉（首屏那一拉在 view 开头已经发过了）。
	if feed.want() {
		if _, last := st.Visible(); last >= rows-1 {
			if ctx := feed.begin(); ctx != nil {
				go s.fetchFeed(ctx, f, feedItems, feed.cursor, feed.page)
			}
		}
	}
}

// feedEmptyKey 是空状态的 key。带上「有没有丢弃」那一档，所以两句话不共用
// 一个 key——同一个 key 上换文案，读屏软件读到的是同一句话。
func feedEmptyKey(f *dynamicFeed) string {
	if f.drops.total() > 0 {
		return "dropped"
	}
	return "none"
}

// feedEmpty 是右边一个格子都没有时说的话。
//
// 「真的没有」那一支**读路由表**（`router.go` 里那一条，出处和上游的对应
// 都记在那儿），不在这儿抄一遍——抄一遍就是两句话各改各的。
//
// ⚠️ **「有丢弃」和「真的没有」必须说两句话**：B 站的动态有二十多种形态，
// 这一版只画三种。说成「暂无动态」的话，用户会以为关注的人都没发过。
//
// ⚠️ **按类别说**（票 #344）：「形态不认识」和「我们读不懂」是**两回事**。
func feedEmpty(f *dynamicFeed) emptyState {
	if f.drops.total() > 0 {
		return emptyState{
			Title: "这里没有能显示的动态",
			Icon:  iconEmptyFeed,
			Hint:  dropSentence(f.drops.counts),
			Info:  true,
		}
	}
	return lookup(dynamicFeedPath).Empty
}

// feedRowLabel 是一条动态给无障碍和「打字选中」用的名字。
//
// 和 UP 空间那边那一份（`upRowLabel`）是同一个东西，但**不共用**：那边的
// 签名吃的是 `*upSpace`，而这一页的状态挂在壳上不是挂在某个 UP 上。
// 两边各写三行，比为了复用去改另一个文件的签名便宜。
func feedRowLabel(d bili.Dynamic) string {
	if d.Kind == bili.DynamicVideo && d.Video != nil && d.Video.Title != "" {
		return d.Video.Title
	}
	if d.Text != "" {
		return d.Text
	}
	return d.Author.Name
}

// viewFeedSentinel 是末尾那一行：还有就拉，拉失败让人点重试。
//
// retry 是「点了再拉一次」那个动作。它**只能由人触发**：有 err 在闸门就一直
// 挡着（见 `upPager.want`），不然就是自己打自己——解了闸门 → 下一帧看见
// 容器还没填满 → 又拉 → 又失败。
func (s *Shell) viewFeedSentinel(c *ui.Context, p *upPager, hint, retryLabel string, retry func()) {
	t := c.Theme()
	ui.Row(c).Key("feed-sentinel").Center().Gap(space.Px8).Padding(space.Px12).Children(func() {
		switch {
		case p.err != nil:
			ui.Text(c, "加载失败："+p.err.Error()).FontSize(scale.Font(c, scale.Size13)).TextColor(t.TextMuted).MaxLines(1).Ellipsis("…")
			// 哨兵那一行上的重试：扁平（错误页那一档是 `viewLoadError`，不动）。
			if retry != nil && flatButton(c, retryLabel).FontSize(scale.Font(c, scale.Size13)).Clicked() {
				retry()
			}
		case p.loading:
			ui.Spinner(c)
			ui.Text(c, "正在加载…").FontSize(scale.Font(c, scale.Size13)).TextColor(t.TextMuted)
		default:
			ui.Text(c, hint).FontSize(scale.Font(c, scale.Size13)).TextColor(t.TextMuted)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 取数
// ─────────────────────────────────────────────────────────────────────────────

// fetchFeed 拉右边（动态）或左边（关注的人）的一页。
//
// ⚠️ **在后台 goroutine 里跑，而且自己 recover**：界面那一层的 recover
// （行级的、页级的）一个都接不到它——Go 里没人在跑那个 goroutine，而这一段
// 正是「解析接口返回」的地方。B 站改一个字段就是在这一行炸。
// cursor 和 page 是**调用方（界面线程）**在开这一拉那一刻读出来的闸门状态，
// 不是这里自己读的——闸门是界面线程的状态，后台碰它就是数据竞争。
func (s *Shell) fetchFeed(ctx context.Context, f *dynamicFeed, which int, cursor string, page int) {
	var r feedResult
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("biu: 拉动态流崩了：%v\n%s", rec, debug.Stack())
			r.err = fmt.Errorf("拉动态的代码崩了：%v", rec)
		}
		// 🔴 铁律三：投递完**必须**唤醒。只投递不唤醒 = 界面停在旧帧。
		f.boxMu.Lock()
		f.box = append(f.box, r)
		f.boxMu.Unlock()
		s.wake.invalidate()
	}()

	r = feedResult{which: which, selfMid: s.app.SelfMid()}

	switch which {
	case feedAuthors:
		pg, err := s.app.FollowedAuthors(ctx, s.app.SelfMid(), page+1)
		r.err, r.authors, r.hasMore, r.sent = err, pg.Authors, pg.HasMore, len(pg.Authors)
	default:
		pg, err := s.app.DynamicFeed(ctx, cursor)
		r.err, r.dyns, r.hasMore, r.drops = err, pg.Items, pg.HasMore, pg.Drops
		r.cursor, r.sent = pg.Offset, pg.Raw
		r.dropWhy = pg.DropWhy
		// ⚠️ **第一页 = 用户看到的是最新的那几条**：那一页的 `update_baseline` 就是
		// 「此刻最新那条动态的 id」（B 站给的是动态 id，不是时间戳），顶栏那个红点
		// 下一次问的时候要知道「用户看到哪儿了」（票 #480）。
		//
		// ⚠️ **只有第一页有**：翻页的那些页里最新的一条更旧，记上去会让基准往回
		// 退（而基准只往前挪，见 app.MarkDynamicSeen）。
		//
		// ⚠️ **落盘不在这里**：那是界面状态（铁律一），落盘在 adoptFeed 那一层
		// （那一层跑在界面线程上）。
		if cursor == "" {
			r.baseline = pg.Baseline
		}
	}

	// 🔴 **每一趟都留一句计数**（理由同 upspace_fetch.go 那一处）：界面上那个
	// 转着的圈**分不出**是「同一页被反复要了」还是「真的在翻页」（票 #300）。
	//
	// ⚠️ **丢掉的那几条要按类别分开写**（票 #344）：「形态不认识」和「读不懂」
	// 是两回事，合在一起就看不出是 B 站换了形态还是我们读不懂。
	if r.err == nil {
		log.Printf("biu: 拉动态流：B 站发了 %d 条，留下 %d 条，丢掉 %d 条（%s），还有下一页=%v",
			r.sent, len(r.dyns)+len(r.authors), len(r.drops),
			dropCountText(dropCountsOf(r.drops)), r.hasMore)
		for _, why := range r.dropWhy {
			log.Printf("biu: 拉动态流丢掉一条：%s", why)
		}
	}
}

// adoptFeed 收走后台投回来的结果。**只能由界面线程调**。
func (s *Shell) adoptFeed(f *dynamicFeed) {
	f.boxMu.Lock()
	box := f.box
	f.box = nil
	f.boxMu.Unlock()
	if len(box) == 0 {
		return
	}

	for _, r := range box {
		if r.selfMid != f.selfMid {
			// 换账号之前发出去的那一页。它说的是**上一个账号**的关注的人。
			f.feedPager(r.which).settle()
			continue
		}
		p := f.feedPager(r.which)
		// ⚠️ **`settle` 必须先调**：失败、超时、返回不对、拉取的代码崩了，
		// 全走这一条。漏了它那一格会永远停在「正在加载」。
		p.settle()
		if r.err != nil {
			p.err = r.err
			log.Printf("biu: 动态流拉失败了：%v", r.err)
			continue
		}
		// 🔴 铁律四：追加是**尾部**，不动已有行的位置，所以「正在看的那
		// 一条」翻完页还是它。
		if f.seen == nil {
			f.seen = map[string]bool{}
		}
		for _, d := range r.dyns {
			if f.seen["dyn:"+d.ID] {
				continue
			}
			f.seen["dyn:"+d.ID] = true
			f.items = append(f.items, d)
		}
		for _, a := range r.authors {
			k := "auth:" + strconv.FormatInt(a.Mid, 10)
			if f.seen[k] {
				continue
			}
			f.seen[k] = true
			f.authors = append(f.authors, a)
		}
		if r.which == feedItems {
			// ⚠️ **「用户看到这儿了」**：第一页里最新那条动态的 id 就是「看过基准」
			// （B 站的 update_baseline 要的就是它，见 app/dynamicseen.go）。
			//
			// 而**那个点要一并清掉**：用户此刻正在看的就是最新的那几条——所以
			// 「比它更新的」一条也没有，那个点不该亮着。
			//
			// ⚠️ **这一段跑在界面线程上**（adoptFeed 是 view 调的），而落盘那件事
			// 要在界面状态里做（铁律一）。
			if r.baseline != "" {
				s.app.MarkDynamicSeen(r.baseline)
				s.feed.clear()
			}
			// ⚠️ **丢掉的是累计的，不是这一页的**：翻了好几页各丢几条就是几十条，
			// 界面上那句话说的是「一共有多少条没显示」。
			//
			// ⚠️ **而且是按「不同的动态」累计的**（票 #344）：B 站翻页会重叠，
			// 同一批画不出来的动态在两页里各出现一次——按出现次数记的话，那个
			// 数会**偏大**。
			f.drops.add(r.drops)
			f.started = true
		}
		// ⚠️ 传的是**这一页**返回了几行，不是累计的：累计的数在「第一页就空」
		// 和「翻到一半这一页空」上分不出来，而后者恰恰是「到头了」。
		p.adopt(r.cursor, r.sent, r.hasMore)
	}
}
