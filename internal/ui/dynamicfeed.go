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
	"github.com/egoist/mygo/ui"
)

// 这一页的路由常量（`dynamicFeedPath`）在 navbar_feed.go：顶栏那个「动态」
// 按钮也是往同一个地址跳，两边共用一份才是「点顶栏和点侧栏到的是同一页」。

// authorPaneW 是左边那一栏的宽度。上游是 `w-64`（256）。
const authorPaneW = 240

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

	// feed 是右边、auth 是左边的翻页闸门（upPager，见 upspace.go）。
	feed upPager
	auth upPager

	// seen 是**已经收进来的身份**，挡翻页重叠（和 `upSpace.seen` 同一个理由：
	// 两个一模一样的行身份会让那几行共用一份状态）。
	seen map[string]bool
	// dropped 是右边丢掉的动态条数。不画在界面上，只用来让「一个格子都没有」
	// 能说清是「真的没有」还是「他发的那些我们画不出来」。
	dropped int
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
	dropped int
	err     error
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
	f.dropped = 0
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
	s.adoptFeed(f)

	// 首屏那两拉，**必须在画之前发出去**。画完再发的话这一帧画出来的是
	// 「正在加载」，要等这一页回来才换掉——而那一段没有任何东西会叫醒界面。
	// 🔴 **换账号了就把这一页整个换掉**：作者栏列的是「**我**关注的人」，
	// 而整个 `dynamicFeed` 是挂在壳上的一份缓存 —— 不换的话退出 A 登录 B，
	// 左边仍然是 A 关注的人（实测：SelfMid 已经是 88，界面上还是「账号77
	// 专属作者」，接口一次都没再请求）。
	s.resetIfAccountChanged(f)

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
	// ⚠️ **作者栏那一拉不在这里发**：它要的是「我关注的人」，没登录时那个接口
	// 一定回 -101，而 -101 在这一层和「网络断了」长得一模一样。发出去的话
	// 那一栏会一直亮着转圈，而界面上看不出原因是「你没登录」。
	// 它在 `viewAuthorPane` 里、判完登录态之后发。

	ui.Row(c).Key("page:" + dynamicFeedPath).Grow(1).FillHeight().Children(func() {
		s.viewAuthorPane(c, f)
		ui.Row(c).Key("feed-items").Grow(1).FillHeight().Children(func() {
			s.viewFeedItems(c, f)
		})
	})
}

// viewAuthorPane 是左边那一栏：标题 + 「全部」+ 关注的人 + 哨兵。
func (s *Shell) viewAuthorPane(c *ui.Context, f *dynamicFeed) {
	t := c.Theme()
	ui.Column(c).Key("feed-authors").Width(authorPaneW).FillHeight().
		Padding(8, 6).Gap(4).Children(func() {
		// 上游这一栏的标题就是「用户动态」（`author-list.tsx:126`）。
		ui.Text(c, "用户动态").FontSize(14).Bold().PaddingX(6)

		if !s.app.LoggedIn() {
			// 没登录**不假装有内容**：作者栏列的是「我关注的人」，没有登录态
			// 就没有这个列表。给一句能指路的，比一个空列表有用。
			ui.Text(c, "登录之后能看到你关注的人").FontSize(12).
				TextColor(t.TextMuted).PaddingX(6)
			return
		}

		// 登录态过了才拉第一页。理由见 viewDynamicFeed 里那一段。
		if !f.auth.started && f.auth.err == nil {
			if ctx := f.auth.begin(); ctx != nil {
				go s.fetchFeed(ctx, f, feedAuthors, f.auth.cursor, f.auth.page)
			}
		}

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
		st.Key = func(i int) any { return authRowKey(f, i) }
		st.Label = func(i int) string { return authRowLabel(f, i) }

		// ⚠️ Key 挂在外面的 Column 上：List 是自己管状态的控件，事后再挂
		// key 会 panic（`ui/element.go` 的 `Key`）。理由见 upspace_tabs.go。
		ui.Column(c).Key("feed-author-list").Grow(1).FillHeight().Children(func() {
			ui.List(c, st, rows).
				Grow(1).FillHeight().
				Rows(func(r ui.ListRow) {
					defer func() {
						if rec := recover(); rec != nil {
							log.Printf("biu: 作者栏那一行画崩了：%v\n%s", rec, debug.Stack())
						}
					}()
					s.viewAuthorRow(r.Context, f, r.Index)
				})
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
	return "auth:" + strconv.FormatInt(f.authors[i-1].Mid, 10)
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
		Gap(8).Padding(6).Radius(6).AlignItems(ui.Center)
	if all {
		// 「全部」是**当前就在看的那一个**：主色底，和侧栏选中收藏夹同一套
		// 颜色（见 sidebar_favorites.go 的 cur 那一支）。
		box = box.Background(t.Accent)
	} else if box.Hovered() {
		box = box.Background(t.SurfaceHover)
	}
	if box.Clicked() && !all {
		s.navigate(upPathOf(mid))
	}
	box.Children(func() {
		if all {
			// 「全部」没有头像。给一个方块底子，一眼能认出「这不是某个人」。
			ui.Box(c).Size(28, 28).Radius(6).Background(t.Surface)
		} else {
			s.spaceAvatar(c, face, 28)
		}
		ui.Text(c, name).Grow(1).FontSize(13).MaxLines(1).Ellipsis("…")
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
		s.viewEmpty(c, "feed:loading", emptyState{Title: "正在加载动态…"})
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
		return f.items[i].ID
	}
	st.Label = func(i int) string {
		if i >= len(f.items) {
			return "加载更多动态"
		}
		return feedRowLabel(f.items[i])
	}

	// ⚠️ Key 挂在外面的 Column 上，理由见上面那一处。
	ui.Column(c).Key("feed-dynamic-list").Grow(1).FillHeight().Children(func() {
		ui.List(c, st, rows).
			Grow(1).FillHeight().FillWidth().
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
				s.viewDynamicCard(r.Context, f.items[r.Index])
			})
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
	if f.dropped > 0 {
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
func feedEmpty(f *dynamicFeed) emptyState {
	if f.dropped > 0 {
		return emptyState{
			Title: "这里没有能显示的动态",
			Icon:  iconEmptyFeed,
			Hint: fmt.Sprintf("B 站的动态有好几种形态，这一版只画得出其中三种"+
				"（纯文字、图文、投稿），刚才那 %d 条都落在别的形态上了", f.dropped),
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
	ui.Row(c).Key("feed-sentinel").Center().Gap(8).Padding(12).Children(func() {
		switch {
		case p.err != nil:
			ui.Text(c, "加载失败："+p.err.Error()).FontSize(13).TextColor(t.TextMuted).MaxLines(1).Ellipsis("…")
			if retry != nil && ui.Button(c, retryLabel).FontSize(13).Clicked() {
				retry()
			}
		case p.loading:
			ui.Spinner(c)
			ui.Text(c, "正在加载…").FontSize(13).TextColor(t.TextMuted)
		default:
			ui.Text(c, hint).FontSize(13).TextColor(t.TextMuted)
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
		r.err, r.dyns, r.hasMore, r.dropped = err, pg.Items, pg.HasMore, pg.Dropped
		r.cursor, r.sent = pg.Offset, pg.Raw
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
			f.dropped = r.dropped
			f.started = true
		}
		// ⚠️ 传的是**这一页**返回了几行，不是累计的：累计的数在「第一页就空」
		// 和「翻到一半这一页空」上分不出来，而后者恰恰是「到头了」。
		p.adopt(r.cursor, r.sent, r.hasMore)
	}
}
