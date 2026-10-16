// UP 主空间那一页：头图 + 关注按钮 + 统计 + 四个 Tab。
//
// # 四个 Tab 的名字和顺序是抄来的
//
// Biu 上游 `src/pages/user-profile/index.tsx:73-104` 把 Tab 写死成一张表：
// **动态 / 投稿 / 收藏夹 / 合集**。顺序也是照它的——用户在这个窗口里看过
// 上游，顺序反了他要找的那一格就不在原来的位置上。
//
// 上游还有一条「非本人且对方没公开收藏夹就藏掉收藏夹那格」的规则，它靠
// `getXSpaceSettings` 判断。这一版不查那个接口，于是**四个 Tab 一律都摆**：
// 别人没公开收藏夹时 B 站给的是**空列表而不是错**（见 `bili.SpaceFavFolders`），
// 界面上是「他没公开收藏夹」，而不是「这一格不见了」——后者更像出错了。
//
// # 头图为什么固定在 Tab 上面
//
// 头图是这一页唯一「不在列表里」的东西：它不跟着下面的列表滚，而四个 Tab
// 的内容各自滚动。放进 Tab 的滚动区里的话，滚两下就看不见「这是谁的空间」。
//
// # 🔴 铁律二：`SetTheme` 每帧在 view 最开头调
//
// 见 theme.go：MyGo 每帧开头把主题重置成跟随系统那份，放到「切换按钮的
// 回调」里只调一次会被**静默丢掉**——不报错、不崩溃、不打日志。
//
// # 🔴 铁律四：每一行都要有稳定且唯一的 key
//
// 三张列表（动态、收藏夹、合集）的行身份分别是动态的 `id_str`、
// `coll:<type>:<id>`——**不是序号**。动态是「一页一页往尾部追加」，而
// 刷新时新动态会插到**前面**；拿序号当身份的话，用户盯着的那一条会整个
// 换成旁边那一条，而界面上看不出任何异样。
package ui

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/egoist/mygo/ui"
)

// upPath 是 UP 空间那条路由，和 router.go 的 routes 表里登记的同一个字符串。
const upPath = "/user/{id}"

// upPathPrefix 是 `upPath` 里那个通配名之前的部分。
//
// 为什么不直接写 `"/user/" + mid`：路由表里那一条改了的话这里跟着错，而且错得
// 很安静——空间页退回占位页，没人看得出来。
const upPathPrefix = "/user/"

// 四张 Tab。**顺序照 Biu 上游**（`user-profile/index.tsx:73-104`）。
const (
	upTabDynamic = iota
	upTabVideo
	upTabCollection
	upTabSeries
	upTabCount
)

// upTabNames 是四个 Tab 上的字，顺序同上。
var upTabNames = []string{"动态", "投稿", "收藏夹", "合集"}

// upSeriesPageSize 是「合集」Tab 一页几条。上游 `series/index.tsx:41` 是 20。
const upSeriesPageSize = 20

// 头图区的尺寸。Biu 那边是 `h-[200px]`，头图是 200 高那一份
// （`space-info.tsx:87-97` 用的 `top_photo_v2.l_200h_img`）。
// 200 正好塞得下头图 + 头像 + 昵称 + 签名 + 三个统计。
const (
	// upHeaderH 是头图区一共多高。Biu 那边是 `h-[200px]`（`space-info.tsx:86`）。
	upHeaderH = 200
	// upBannerH 是头图（横幅）那一块多高，剩下的给下面那一行头像/昵称/关注。
	//
	// ⚠️ **不能两个都给 200**：那样横幅吃掉整个头图区，下面那行被压成 0 高，
	// 文字还在（Tester 照样 `Find` 得到）而按钮点不到——「看得见、点不着」
	// 是最难查的那一类。
	upBannerH = upHeaderH - 72 - 2*upPad
	// upAvatarS 是头像边长。
	upAvatarS = 72
	// upPad 是头图区内边距。Biu 是 `px-8 py-4`，这里取一个等价的整数。
	upPad = 16
)

// upPathOf 是 mid 这个 UP 的空间地址。
func upPathOf(mid int64) string { return upPathPrefix + strconv.FormatInt(mid, 10) }

// parseMid 把路由参数读成一个 mid。
//
// **读不出来是 false 而不是 0**：mid=0 是个合法数字（虽然 B 站上没有这个
// 用户），拿它去发请求的话用户看到的是「这个 UP 主读不出来」——而真正
// 的原因是路径里根本没有一个 id。
func parseMid(s string) (int64, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// ─────────────────────────────────────────────────────────────────────────────
// 状态
// ─────────────────────────────────────────────────────────────────────────────

// upSpace 是「一个 UP 的空间」这一页的状态。
//
// 按 mid 存着（`Shell.upSpaces`）：从动态流点进一个 UP、切走、再点回来，
// 那一页还是热的，不用重新拉一遍头图。
//
// ⚠️ **只由界面线程读写**（view 永远跑在主线程上），所以里面没有一个锁。
// 后台那一条线只做两件事：把结果投进信箱（自己带锁），叫醒界面。
type upSpace struct {
	mid int64
	// tab 是当前停在哪个 Tab。它是 `ui.Tabs` 绑定的那个 int，所以**指针要
	// 稳定**——每帧传 `&sp.tab` 就够（那是这一个字段的地址）。
	tab int
	// header 是头部（昵称/头图/关注/统计）。取数、代次和唤醒在 app 层管。
	header *app.UpSpace

	// videosStarted 是「投稿」Tab 拉过第一页了没有。见 viewSpaceVideos。
	videosStarted bool
	// videos 是「投稿」Tab。直接用 `list.PagedList`——它就是干这个的：
	// 三条翻页保障、防重入、页脚重试、回顶按钮全在里面了。
	videos *list.PagedList

	// 剩下三张 Tab 各有一份翻页闸门。
	dyn upPager
	fav upPager
	ser upPager

	// dynItems 是「动态」Tab 的行。
	dynItems []bili.Dynamic
	// cols 是「收藏夹」和「合集」两张 Tab 的行。其余 Tab 那一格是空的。
	cols [upTabCount][]bili.UpCollection

	// state 是一张 Tab 一份的列表状态。**不能共用**：MyGo 会在
	// 「同一个 ListState 被两个 List 显示」时 panic（`ui/list.go` 的
	// buildList），而共用还会把两张 Tab 的滚动位置搅在一起。
	state [upTabCount]ui.ListState

	// dropped 是三张 Tab 各丢了几条。**不画在界面上**，但它是「这一页
	// 一个格子都没有」唯一能说清原因的东西：>0 是「他发的那些我们画不出
	// 来」，0 是「真的没有」（见 bili/dynamic.go 的文件头）。
	dropped [upTabCount]int

	// ── 跨 goroutine 的只有下面这一小摊 ──
	//
	// 🔴 铁律三：后台投递完**必须叫醒界面**，只投递不唤醒 = 界面停在旧帧
	// （实测 0 帧刷新）。信箱带锁，界面线程在 view 开头取走。
	boxMu sync.Mutex
	box   []upResult
}

// upResult 是后台拉完之后投进信箱的东西。
type upResult struct {
	tab  int
	dyns []bili.Dynamic
	cols []bili.UpCollection
	// cursor 是下一页的入口（动态接口给的 offset）。别的 Tab 是空的。
	cursor  string
	hasMore bool
	dropped int
	err     error
}

// upSpaceFor 是 mid 这个 UP 的空间页状态，第一次问的时候现建。
//
// 按 mid 存着（Shell.upSpaces）：从动态流点进一个 UP、切走、再点回来，
// 那一页还是热的，头图不用重新拉一遍。
func (s *Shell) upSpaceFor(mid int64) *upSpace {
	if s.upSpaces == nil {
		s.upSpaces = map[int64]*upSpace{}
	}
	if sp, ok := s.upSpaces[mid]; ok {
		return sp
	}
	sp := &upSpace{mid: mid, header: s.app.UpSpaceOf(mid)}
	// 拉页器的闭包捕获 mid（不是 sp）：换 UP 主就是换一份 sp，这个闭包里
	// 拿到的永远是对的 UP。
	sp.videos = list.NewPaged(s.images, list.AppActions(s.app, s.addToast),
		func(ctx context.Context, page int) (list.Page, error) {
			return s.fetchVideos(ctx, mid, page)
		})
	s.upSpaces[mid] = sp
	return sp
}

// pagerOf 是第 tab 张 Tab 的闸门。
func (sp *upSpace) pagerOf(tab int) *upPager {
	switch tab {
	case upTabCollection:
		return &sp.fav
	case upTabSeries:
		return &sp.ser
	}
	return &sp.dyn
}

// ─────────────────────────────────────────────────────────────────────────────
// 翻页闸门
// ─────────────────────────────────────────────────────────────────────────────
//
// `list.PagedList` 那三条保障（首屏检查 / 滚到底 / 加载结束后再查）背后的判断
// 就是下面这几行：是不是正在拉、末尾那一行进没进画面。那三条要的是一个
// `*list.List`（音乐卡片），动态卡片和收藏夹卡片是另外两种东西，所以那一层
// 用不上；可复用的判断在这里只写一遍。

// upPager 是一条会往下翻的列表的闸门。
//
// ⚠️ **它自己不知道行是什么**：行在 `upSpace` 里，闸门只管「该不该拉 /
// 在途的还算不算数 / 还有没有 / 下一页的入口在哪」。
type upPager struct {
	// loading 是「正在拉」。**失败之后也必须回到 false**，不然界面永远转圈。
	loading bool
	// hasMore 后面还有没有。第一次拉之前是 false，靠 started 区分。
	hasMore bool
	// started 是「拉过至少一次了」。首屏那一拉靠它。
	started bool
	// err 是上一次失败的错。有它就**不再自动重试**——不然就是自己打自己
	// （解了闸门 → 下一帧看见还没画满 → 又拉 → 又失败）。等用户点重试。
	err error
	// cancel 停掉在途的那一页。
	cancel context.CancelFunc

	// cursor 是下一页的入口。动态是 B 站给的 offset（**不透明的串**，
	// 不能解析、不能数）；合集是页码。收藏夹一次给全，两个都不用。
	cursor string
	page   int
}

// want 报「该拉了吗」。
func (p *upPager) want() bool {
	if p.loading || p.err != nil {
		return false
	}
	return p.hasMore || !p.started
}

// begin 开一页。返回的 ctx 为 nil 表示这一次不用拉。
//
// 🔴 **防重入就是第一个 if**：三条保障会在同一帧里挨着命中（首屏检查、
// 末尾进画面、加载后再查），没有这把锁就是一次滚动三个请求。
func (p *upPager) begin() context.Context {
	if !p.want() {
		return nil
	}
	if p.cancel != nil {
		p.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel, p.loading = cancel, true
	return ctx
}

// settle 后台回来时调一次，解开闸门。
//
// ⚠️ **`loading = false` 必须在这一行无条件执行**：请求失败、超时、返回不对、
// 拉取的代码自己崩了，全走这一条。漏了它就是「界面永远转圈」。
func (p *upPager) settle() {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.loading = false
}

// adopt 记一次成功的拉取。got 是**这一页**返回了几行。
//
// ⚠️ **「这一页是空的」一律收口成到底**：接口在骗人（下一页还没生成）或者
// 已经到头了，再拉就是空转。而空转的代价是界面上永远转圈——
// `settle` 忘了 `loading = false` 时是同一个失败模式。
//
// ⚠️ 判据是**这一页**的几行而不是累计的：累计的数在「第一页就空」和
// 「翻到一半这一页空」上分不出来，而后者恰恰是「到头了」。
func (p *upPager) adopt(cursor string, got int, hasMore bool) {
	p.started = true
	p.page++
	if cursor != "" {
		p.cursor = cursor
	}
	p.hasMore = hasMore && got > 0
	p.err = nil
}

// ─────────────────────────────────────────────────────────────────────────────
// 这一页
// ─────────────────────────────────────────────────────────────────────────────

// viewUpSpace 画 UP 空间那一页。
//
// ⚠️ **外面已经包了一层 recover**（`router.go` 的 `Shell.page`）：MyGo 的
// Dispatch 和 drain 都没有 recover，view 里 panic 会穿透 Cocoa 事件循环
// **直接杀死进程**。这里再加一层不会更安全，只会让同一个 panic 被记两遍。
//
// ⚠️ **问 `r` 不要问 `c.Router()`**：换页动画还在走的时候，正在离场的那一页
// 会被重新建一遍，而 MyGo 建离场页时故意不设 `c.router`，
// `c.Router().Query(...)` 在那一帧就是空指针（`searchpage.go` 有完整的一段）。
func (s *Shell) viewUpSpace(c *ui.Context, r *ui.Route) {
	// 🔴 铁律二：每帧第一件事就是装主题，而且必须在最开头。
	s.applyFrameTheme(c)

	// ⚠️ **认页的是本项目自己那一份匹配**（`router.go` 的 `lookup`，见那里
	// 「认页和画页合成一处」那段）：它认出「这一条路由是哪个」，但 MyGo 的
	// `Route.params` 要**有人调过 `r.Match(pattern)` 之后**才有值。所以这里
	// 补调一次——不补的话 `r.Param("id")` 永远是空串，界面上就是一句
	// 「这个地址里没有 UP 主的编号」，而用户点进来时地址明明是对的。
	r.Match(upPath)

	mid, ok := parseMid(r.Param("id"))
	if !ok {
		// 路由 `/user/{id}` 匹配了，却没解出一个数。**说清楚是哪条路径**，
		// 不给一句「暂无内容」——用户是从一个能点的地方进来的。
		s.viewEmpty(c, "space:bad-id", emptyState{
			Title: "这个地址里没有 UP 主的编号",
			Hint:  "地址是 " + r.Path() + "，回去从动态或关注列表里点一个 UP 主",
		})
		return
	}

	sp := s.upSpaceFor(mid)

	// 先收后台投回来的结果，再画：这样这一帧画的就是已经加上/换掉的那几行，
	// 不用等下一帧（铁律三）。
	s.adoptUp(sp)

	h := sp.header.Snapshot()
	// 首屏那一拉。**必须在画之前发出去**，不是画完再发：画完再发的话这一帧
	// 画出来的是「正在加载」，要等这一页回来才换掉。而那一段没有任何东西会
	// 叫醒界面——唤醒只有「结果到了」那一次。
	if !h.Loaded && !h.Loading {
		sp.header.Load()
	}

	// ⚠️ **切 Tab 什么都不重置**——这是 PLAN 阶段 5 的一条验收（「切 Tab 的
	// 滚动位置恢复与 Biu 一致」），也是 Biu 那边本来的行为（上游那四张 Tab
	// 各自是独立的组件，状态各自留着）。
	//
	// 特别是**不要在这里 reload**：reload 会把闸门清成「第一页」，而已经
	// 拿到的行还在 `dynItems` 里，于是下一次拉回来的是**同一页**，被直接
	// 追加在后面——两个一模一样的行身份，框架按 key 对位时那几行共用一份
	// 状态（Tester 里当场 panic，真窗口里选中一个另一个也亮）。
	// 翻回一张 Tab 只会问「还要不要下一页」，而 `started` 为真时答案是否。

	ui.Column(c).Key("page:" + upPath).Grow(1).FillHeight().Children(func() {
		s.viewSpaceHeader(c, sp, h)
		s.viewSpaceTabs(c, sp)
		ui.Row(c).Key("space-body").Grow(1).FillHeight().Children(func() {
			s.viewSpaceTab(c, sp)
		})
	})
}

// viewSpaceHeader 是头图 + 关注 + 统计。
//
// 四种状态画成四种样子（照 Biu `space-info.tsx`）：加载中 / 没读到（带一个
// 重试）/ 拉黑了 / 正常。**四种都要能看出来是哪一种**——把「拉黑了」画成
// 「没关注」的话，用户按下去发出去的是「取关」，而 B 站对拉黑中的用户回的是
// 另一个码，界面上就成了一句看不懂的错。
func (s *Shell) viewSpaceHeader(c *ui.Context, sp *upSpace, h app.SpaceHeader) {
	t := c.Theme()
	key := "space-header:" + strconv.FormatInt(sp.mid, 10)

	switch {
	case h.Loading && !h.Loaded:
		ui.Row(c).Key(key).Height(upHeaderH).Width(0).Center().Children(func() {
			ui.Spinner(c).Label("正在打开这个 UP 主的空间")
		})
		return

	case h.Err != "" && !h.Loaded:
		ui.Column(c).Key(key).Height(upHeaderH).Width(0).Center().Gap(10).
			AlignItems(ui.Center).Children(func() {
			ui.Text(c, "打不开这个 UP 主的空间").FontSize(16).Bold()
			ui.Text(c, h.Err).FontSize(12).TextColor(t.TextMuted).MaxLines(2).Ellipsis("…")
			if ui.Button(c, "再试一次").Clicked() {
				sp.header.Load()
			}
		})
		return

	case h.Relation.Blocked():
		// 上游是整页换成一张「已拉黑」的图（`space-info.tsx:69-75`）。这里
		// 保留头像和名字：用户多半只是想确认「我拉黑的是不是这个人」。
		ui.Row(c).Key(key).Height(upHeaderH).Width(0).Gap(12).Center().Children(func() {
			s.spaceAvatar(c, h.User.Face, upAvatarS)
			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, spaceName(h.User)).FontSize(20).Bold()
				ui.Text(c, "你把他拉黑了，他的内容在这儿看不到").
					FontSize(13).TextColor(t.TextMuted)
			})
		})
		return
	}

	ui.Column(c).Key(key).Height(upHeaderH).Width(0).Children(func() {
		// ① 头图。没设头图的 UP 主占大多数（B 站这时候给的是空对象而不是
		// 一张空白图），画一块纯色——**别去画 B 站的占位图**，那看着像加载失败。
		if h.User.Banner != "" {
			s.spaceFill(c, h.User.Banner, upBannerH)
		} else {
			ui.Box(c).Height(upBannerH).Width(0).Background(t.Surface)
		}
		// ② 头像 + 昵称 + 签名 + 右边那一排关注和统计。
		ui.Row(c).Key("space-head-info").Grow(1).Width(0).Gap(12).
			Padding(upPad).AlignItems(ui.Center).Children(func() {
			s.spaceAvatar(c, h.User.Face, upAvatarS)
			ui.Column(c).Gap(2).Children(func() {
				ui.Text(c, spaceName(h.User)).FontSize(20).Bold().MaxLines(1).Ellipsis("…")
				if h.User.Official != "" {
					ui.Text(c, "认证："+h.User.Official).FontSize(12).TextColor(t.TextMuted)
				}
				if h.User.Sign != "" {
					ui.Text(c, h.User.Sign).FontSize(13).TextColor(t.TextMuted).MaxLines(2)
				}
			})
			ui.Box(c).Grow(1)
			s.viewSpaceActions(c, sp, h)
		})
	})
}

// viewSpaceActions 是右边那一排：关注按钮 + 粉丝数 + 等级。
//
// 两条画不画的规矩都抄上游：
//   - **关注按钮**只在「登录了、且看的不是自己」时画（`space-info.tsx:100`）；
//   - **「关注数」只在自己看自己时画**（`space-info.tsx:36-40` 的
//     `hidden: !isSelf`）：别人的关注数是 B 站不公开的那一档，画出来是 0。
//
// ⚠️ 上游的统计**只有这三个**——没有获赞数、没有播放数（`space-info.tsx:31-45`
// 的 `stats` 表就三行）。这一版照它，不自己加。
func (s *Shell) viewSpaceActions(c *ui.Context, sp *upSpace, h app.SpaceHeader) {
	isSelf := s.app.SelfMid() == sp.mid

	ui.Row(c).Key("space-actions").Gap(18).AlignItems(ui.Center).Children(func() {
		// 关注按钮**只在「登录了、且看的不是自己」时画**（上游
		// `space-info.tsx:100` 的 `Boolean(user?.isLogin) && !isSelf`）。
		// 看自己的时候画一个「关注自己」是 B 站网页版都不做的事。
		if s.app.LoggedIn() && !isSelf {
			followed := h.Relation.Followed()
			// ⚠️ 关注是**写接口**（要 CSRF、要网络、一个往返）。在界面线程上
			// 直接调就是每按一次冻一下窗口，所以开后台；`UpSpace.Follow`
			// 自己会叫醒界面（铁律三）。
			if spaceFollowButton(c, followed).Clicked() {
				go func() {
					if err := sp.header.Follow(context.Background(), !followed); err != nil {
						s.addToast("关注没改成：" + err.Error())
					}
				}()
			}
		}

		// ⚠️ **整块统计只在登录后画**（上游 `space-info.tsx:125` 的
		// `{user?.isLogin ? stats.map(...) : null}`）。
		//
		// 原因不是「没登录不好看」，是**画出来是个假数**：`/x/relation/stat`
		// 要 Cookie，没登录时 B 站要么回 -101、要么给 0，而界面上会是一个
		// 干干净净的「粉丝数 0」——用户会以为他真的一个粉丝都没有。等级也是
		// 一块（上游把它算在 `stats` 里），跟着一起藏。
		if s.app.LoggedIn() {
			spaceStat(c, "粉丝数", formatCount(h.Stat.Follower))
			if isSelf {
				spaceStat(c, "关注数", formatCount(h.Stat.Following))
			}
			spaceStat(c, "等级", fmt.Sprintf("Lv%d", h.User.Level))
		}
	})
}

// spaceFollowButton 是那个关注 / 已关注按钮。
//
// 已关注时是普通按钮（不是主色）：Biu 那边也是 `color={isFollow ? "success"
// : "default"}`——「取消关注」不该长得像「关注他」。
func spaceFollowButton(c *ui.Context, followed bool) ui.Element {
	if followed {
		return ui.Button(c, "已关注")
	}
	return ui.PrimaryButton(c, "关注")
}

// spaceStat 是一个统计数字（上面是数、下面是名字）。
func spaceStat(c *ui.Context, label, value string) {
	t := c.Theme()
	ui.Column(c).Key("space-stat:" + label).Gap(2).AlignItems(ui.Center).Children(func() {
		ui.Text(c, value).FontSize(17).Bold()
		ui.Text(c, label).FontSize(12).TextColor(t.TextMuted)
	})
}

// spaceName 是这个 UP 显示在标题上的名字。还没加载时给一个占位，而不是空串
// ——空名字在标题栏上是一个连宽度都没有的东西，而这一帧用户正等着知道这是谁。
func spaceName(u app.User) string {
	if u.Name == "" {
		return "这个 UP 主"
	}
	return u.Name
}

// spaceAvatar 画头像。**没有头像画一个圆底色**，不是画一个「加载失败」。
func (s *Shell) spaceAvatar(c *ui.Context, face string, side float32) {
	t := c.Theme()
	ui.Box(c).Size(side, side).Radius(side / 2).Clip().Children(func() {
		if st := s.images.Get(face); face != "" && st.Ready() {
			ui.Image(c, st.Bitmap).Fit(ui.Cover).Size(side, side)
			return
		}
		ui.Box(c).Size(side, side).Radius(side / 2).Background(t.Surface)
	})
}

// spaceFill 画一张铺满宽度、指定高度的网络图（头图）。
//
// 形状上不裁圆角：头图是横幅，方角是本来的样子（`space-info.tsx:86` 的
// `bg-cover bg-center` + 一层黑色蒙版）。
func (s *Shell) spaceFill(c *ui.Context, url string, h float32) {
	t := c.Theme()
	ui.Box(c).Height(h).Width(0).Clip().Children(func() {
		if st := s.images.Get(url); st.Ready() {
			ui.Image(c, st.Bitmap).Fit(ui.Cover)
			return
		}
		ui.Box(c).Height(h).Width(0).Background(t.Surface)
	})
}

// spaceThumb 画一张指定大小的网络图，四态与 `imgcache.View` 一致
// （空地址画空框、没好画底色、失败画底色），只是形状不跟着主题圆角走。
func (s *Shell) spaceThumb(c *ui.Context, url string, w, h float32) {
	t := c.Theme()
	ui.Box(c).Size(w, h).Clip().Children(func() {
		if st := s.images.Get(url); url != "" && st.Ready() {
			ui.Image(c, st.Bitmap).Fit(ui.Cover).Size(w, h)
			return
		}
		ui.Box(c).Size(w, h).Background(t.Surface)
	})
}

// formatCount 把一个人数写成 B 站那种「1.2万」。
//
// **给人读的，不是给机器比的**：9999 写成「9999」占四位，12000 写成「1.2万」
// 占三位，头图那一行窄的时候差的就是这一点。B 站的 `formatNumber`
// 一万以上用「万」、一亿以上用「亿」。
func formatCount(n int64) string {
	switch {
	case n >= 100000000:
		return fmt.Sprintf("%.1f亿", float64(n)/1e8)
	case n >= 10000:
		return fmt.Sprintf("%.1f万", float64(n)/1e4)
	default:
		return strconv.FormatInt(n, 10)
	}
}
