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
	"strings"
	"sync"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/rowkey"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scrim"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/shadow"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
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
// （`space-info.tsx:83` 用的 `top_photo_v2.l_200h_img`）。
// 200 正好塞得下头图 + 头像 + 昵称 + 签名 + 三个统计。
const (
	// upHeaderH 是头图区一共多高。Biu 那边是 `h-[200px]`（`space-info.tsx:83`）。
	upHeaderH = 200
	// upAvatarS 是头像边长。Biu 那边是 `h-[140px] w-[140px] flex-none shadow-lg`
	// （`user-profile/space-info.tsx:89`）——**140 是上游的尺寸**，不是「比原来
	// 大一点」估出来的。
	upAvatarS = 140
	// upPad 是头图区的**纵向**内边距，也是 Tab 那一条的左右内边距。Biu 是
	// `py-4`（`space-info.tsx:83`）和内容区那一圈 `p-4`（`user-profile/index.tsx:130`）。
	upPad = space.Px16
	// upPadX 是头图区的**左右**内边距。Biu 是 `px-8`（`space-info.tsx:83`）。
	//
	// ⚠️ **不要拿它去改 Tab 那一条**：上游 Tab 外面是 `p-4`（16），头图才是
	// 32。两处共用一个数的话，Tab 会跟着头图一起往中间缩。
	upPadX = space.Px32

	// upHeaderRowTop 是「信息那一行」（头像 + 名字 + 签名 + 统计）的**上沿**
	// 在头图区自己的坐标（28）。
	//
	// 它 = 头图区 200 - 那一行默认的高（头像 140 + 上下各 16 = 172）。
	//
	// 🔴 **这一行是「贴着上沿」摆的**（`Absolute().Top(upHeaderRowTop)`），
	// **不是贴着下沿**：它一高（**系统字号一放大它就高**）就往下长，而上面的
	// 字因此**不会再往上跑**——上面那一段是留给「整块那一档黑」的
	// （`spaceTextScrim`）。贴着下沿的话，字号一放大名字就跑到那一段里去了
	// （票 #295 的评审在 225% 那档量到的就是它：名字上沿只剩 2.12:1）。
	//
	// ⚠️ **默认字号下画面一个字都不变**：那一行默认 172 高，上沿 28 +
	// 高 172 = 下沿 200 —— 和贴着下沿时落在同一个位置。
	upHeaderRowTop = upHeaderH - upAvatarS - 2*upPad

	// upHeaderTextTop 是「有字的那一带」那一层黑**满档**的高度（44）——
	// 也就是信息那一行的**内容**上沿（上面那一行 + 上面那一格内边距）。
	//
	// ⚠️ **它是一个常数，不是一个算出来的数**：那一行贴着上沿摆（见
	// `upHeaderRowTop`），所以**里面的字永远在 44 以下**——名字是那一列的
	// 第一件东西，而那一列在内容框里是**居中**的，于是它的上沿 ≥ 44。
	// 「算出来」的那一版（量文字的高度推位置）在两处不成立：行高是**字体
	// 自己的事**（换一款字体就差一截），而框架把行与行之间的行距算在
	// **摆出来的那一行**里、不在 `MeasureText` 给的那一档里。
	upHeaderTextTop = upHeaderRowTop + upPad

	// upHeaderScrimFade 是头图区里「有字的那一带」那一层黑**渐变**有多高（12）。
	//
	// ⚠️ **它不是「那一档黑盖住哪一块」**：盖住的是「信息那一行」那一块
	// （从 `upHeaderTextTop` 往下），渐变只是从**满档**的位置往上淡 12 像素。
	// 上面那一段（头图区最上面）**不蒙**——那里就是「整块那一档黑」的原样，
	// 也就是上游那一层本身（浅色 30% → #b2b2b2、深色 50% → #7f7f7f）。
	//
	// 「整块那一档黑在位」那一条量的就是**头图区最上面**那一段。
	upHeaderScrimFade = 12

	// spaceDividerAlpha 是统计之间那条竖线的 alpha（35% 白）。
	//
	// ⚠️ **它也是白字那一族**：主题那一档分隔线在浅色下是 #dbdbdb（亮）、在深色下
	// 是 #3f3f46（暗）——后者压在头图区那块深底上几乎看不见。这里是 35% 的白，
	// 两套主题下都看得见。
	//
	// ⚠️ **它不受 4.5:1 那一档管**：它是**装饰性的分隔线**，不是「读懂内容
	// 所必需」的图形（WCAG 1.4.11 管的是后者）。两个统计的名字本身是字，
	// 那一档已经量过了（`upHeaderTextDim`）。
	spaceDividerAlpha = 0.35
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

// upSpaceMidOf 从路径里认出 UP 主的编号。
//
// ⚠️ **它和 `r.Param("id")` 是同一件事的两条路**：画的时候问路由（那一行是
// 路由自己认出来的），而每帧的副作用那一层手上只有一条路径。
func upSpaceMidOf(path string) (int64, bool) {
	rest, ok := strings.CutPrefix(path, upPathPrefix)
	if !ok {
		return 0, false
	}
	return parseMid(rest)
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
	// notify 说一句话（后台跑完的结果）。nil 时那一句就没了。
	//
	// ⚠️ **语气跟着话一起走**（票 #180）：失败是红的。
	notify func(string, notice.Tone)
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
	// seen 是**已经收进上面两份的行身份**，用来挡翻页重叠。
	//
	// ⚠️ 必须有：B 站翻页是会重叠的（同一页的东西挪了名次就会在两页里各出现
	// 一次），而**两个一模一样的行身份**会让框架按 key 对位时那几行共用一份
	// 状态 —— Tester 里当场 panic，真窗口里选中一个另一个也亮，而界面上看不出
	// 任何异样。`list.PagedList` 早就为此维护了一张 `seen`（`paged.go`），
	// 这一层原来没有，是这次评审的 fixture 撞出来的。
	seen map[string]bool

	// rowKeys 是一张 Tab 一份的「记着行身份那一份」（**预先装箱好的那一份**）。
	//
	// ⚠️ 装箱那一步是要分配的（`runtime.convTstring`），现算的话每帧每行一次
	// （票 #313）。**它自己知道「数据换了」**（见 `rowkey.Memo` 的文件头）：
	// 翻页追加、换 Tab——身份对不上就重算。
	rowKeys [upTabCount]rowkey.Memo[upRowIdent, any]

	// state 是一张 Tab 一份的列表状态。**不能共用**：MyGo 会在
	// 「同一个 ListState 被两个 List 显示」时 panic（`ui/list.go` 的
	// buildList），而共用还会把两张 Tab 的滚动位置搅在一起。
	state [upTabCount]ui.ListState
	// keys 是每一张 Tab 那张列表的键盘接线（游标 + 「↑ 到顶回侧栏」）。
	//
	// ⚠️ **一张 Tab 一份**：列表状态本来就是一张 Tab 一份的（`state` 那个
	// 数组），而三张 Tab 的行数不一样——游标共用一份的话，切到行数少的那张
	// 上就越界了。
	//
	// ⚠️ **这一栏是裸的 `ui.List`**（卡片是自己画的，走不了 `widgets/list`
	// 那个包），所以游标和那一档回调得自己接——不然方向键在内容区里只滚动
	// 不选行，而 ↑ 也回不到侧栏（票 #245 的 E4）。
	keys [upTabCount]listKeys

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
	cursor string
	// sent 是 B 站这一页发了几条（过滤**之前**）。见 upPager.adopt：
	// 「这一页全是画不出来的」和「这一页真的没有」靠它分开。
	sent    int
	hasMore bool
	dropped int
	// dropWhy 是丢掉的理由（人话，一种一句）。**只进日志**，不进界面。
	//
	// ⚠️ 「整页一条都不剩」这种失败在界面上完全看不出来（只有一个空列表
	// 加一个转着的圈），不把理由写进日志，下一个人只能从头再猜一遍
	// （票 #300 的实测就是 `pub_ts` 变成了字符串）。
	dropWhy []string
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
	sp := &upSpace{mid: mid, header: s.app.UpSpaceOf(mid), notify: s.addToast}
	// 拉页器的闭包捕获 mid（不是 sp）：换 UP 主就是换一份 sp，这个闭包里
	// 拿到的永远是对的 UP。
	sp.videos = list.NewPaged(s.images, s.listActions(),
		func(ctx context.Context, page int) (list.Page, error) {
			return s.fetchVideos(ctx, mid, page)
		})
	// ⚠️ **「正在加载」那一格由外面递进去**（票 #138）：分页列表自己那一句是
	// 13px 淡灰缩在列表左上角，而「暂无投稿」是居中 15pt 两行。
	sp.videos.SetEmptyView(loadingCell("正在加载…"))
	// 播放量 + 投稿时间：这一页两个数都有（`UpVideo.Play` / `UpVideo.Created`，
	// 后者来自接口的 `created`——**不是 pubdate**，见那个字段的注释）。
	// `Now` 传的是 `s.now`（闭包），不是 `s.clock.Now`（值）。
	sp.videos.SetColumns(list.Columns{PlayCount: true, PubTime: true, Now: s.now})
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

// stop 停掉在途的那一页，并把闸门清成「从没拉过」。
//
// 只停请求**不作废**已经在回来的那一页 —— 那一页照样会走进 settle 并被
// 追加进来。所以换查询时**必须连 items 一起清**（见 resetIfAccountChanged）。
func (p *upPager) stop() {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	*p = upPager{}
}

// adopt 记一次成功的拉取。
//
// sent 是**这一页 B 站发了几条**（过滤**之前**的条数），hasMore 是它说的
// 「后面还有」。
//
// ⚠️ **「这一页是空的」要按 `sent` 判，不能按「留下了几条」判**：实测踩过——
// 一页里全是转发的动态（全被丢弃）时留下 0 条，拿 0 去判就成了「到底了」，
// 后面那一页**永远不会加载**（第二页有一条正常的，卡在第一页上不动了）。
//
// 用 `sent` 就分开了：`sent == 0` 是真的没有下一页（再拉是空转，界面永远
// 转圈），`sent > 0` 而留下 0 条是「这一页我们画不出来，去问下一页」。
func (p *upPager) adopt(cursor string, sent int, hasMore bool) {
	p.started = true
	p.page++
	if cursor != "" {
		p.cursor = cursor
	}
	p.hasMore = hasMore && sent > 0
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

	// ⚠️ **收结果和起头图那一拉挪到 `syncUpSpace` 那一层了**（票 #161）：满屏
	// 开着的时候这一页不画，而头图和列表的后台结果不能跟着一起停。
	h := sp.header.Snapshot()

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

// syncUpSpace 是 UP 主空间这一页每帧的副作用：收后台投回来的结果、起头图那一
// 拉、起「投稿」那一张 Tab 的列表、起另外三张 Tab 的首屏那一拉。
//
// ⚠️ **它们原来在 `viewUpSpace` 和那几张 Tab 的 view 里**，而这一页在满屏开着
// 的时候不画（票 #161）——不挪的话，用户在满屏里看频谱的那几分钟里，头图和
// 列表的后台结果一直躺在信箱里没人取。
func (s *Shell) syncUpSpace(c *ui.Context) {
	mid, ok := upSpaceMidOf(s.router.Path())
	if !ok {
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
	//
	// ⚠️ **`h.Err != ""` 时绝对不重发**：失败只把 `Loading` 落回 false，
	// `Loaded` 仍是 false（那是「从没成功过」），而这一行每帧都跑——于是
	// 头图一失败就变成一个每帧发一次的请求（实测 300 毫秒发了 138 次），
	// 界面停在「打不开」上还在刷网络。唯一的出口是用户点「再试一次」。
	if !h.Loaded && !h.Loading && h.Err == "" {
		sp.header.Load()
	} else if h.NeedRelation(s.app.SelfMid()) || h.NeedStat(s.app.SelfMid()) {
		// 🔴 关注关系和关注数这两样**都要在缺的时候补查**。
		//
		// 关注关系那条的理由见下面；关注数那条是同一个病根：它是**按账号**
		// 的，而且没查到的时候界面上不能画「0」。
		//
		// 头是按 mid 缓存的，而关系是**按账号**的：没登录时进来过一次，
		// 那次不查关系（那个接口要 Cookie，见 `Load` 里的 askRelation），
		// 头部却已经 `Loaded` 了——于是登录之后界面一直停在「从没查过」的
		// 那份上，已关注的 UP 主画出来是「关注」（按下去是一次取关）。
		sp.header.Load()
	}

	// 「投稿」那一张 Tab 的列表。**只有当前停在那张 Tab 上时才有东西要收**——
	// 别的 Tab 上它压根没发过那一趟（`viewSpaceVideos` 里那一支）。
	//
	// ⚠️ **判的是上一帧的那张 Tab**：这一层的副作用跑在画的前面，而 Tab 是
	// 画的时候才写的。满屏开着的时候用户切不了 Tab，所以读到的就是当前那一张。
	if sp.tab == upTabVideo {
		if !sp.videosStarted {
			sp.videosStarted = true
			sp.videos.Reload()
		}
		sp.videos.SetView(s.displayView())
		sp.videos.Sync(c)
	}

	// 另外三张 Tab 的首屏那一拉。
	p := sp.pagerOf(sp.tab)
	if !p.started && !p.loading {
		if ctx := p.begin(); ctx != nil {
			go s.fetchUp(ctx, sp, sp.tab, p.cursor, p.page)
		}
	}
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
		ui.Row(c).Key(key).Height(upHeaderH).FillWidth().Center().Children(func() {
			ui.Spinner(c).Label("正在打开这个 UP 主的空间")
		})
		return

	case h.Err != "" && !h.Loaded:
		ui.Column(c).Key(key).Height(upHeaderH).FillWidth().Center().Gap(space.Px10).
			AlignItems(ui.Center).Children(func() {
			// ⚠️ **字重不写**（400）：标题那一档是 400（DESIGN.md §二）。
			ui.Text(c, "打不开这个 UP 主的空间").FontSize(scale.Font(c, scale.Size16))
			ui.Text(c, h.Err).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted).MaxLines(3)
			// ⚠️ **失败之后只有人能让它再来一次**：上面那一行每帧都跑，
			// 没有这道门就是一个每帧发一次请求的死循环（见那里的注释）。
			if ui.Button(c, "再试一次").Clicked() {
				sp.header.Load()
			}
		})
		return

	case h.Relation.Blocked():
		// 上游是整页换成一张「已拉黑」的图（`space-info.tsx:69-75`）。这里
		// 保留头像和名字：用户多半只是想确认「我拉黑的是不是这个人」。
		ui.Row(c).Key(key).Height(upHeaderH).FillWidth().Gap(space.Px16).Center().Children(func() {
			s.spaceAvatar(c, h.User.Face, upAvatarS, true)
			ui.Column(c).Gap(space.Px4).Children(func() {
				ui.Text(c, spaceName(h.User)).FontSize(scale.Font(c, scale.Size20)).FontWeight(weightMedium)
				ui.Text(c, "你把他拉黑了，他的内容在这儿看不到").
					FontSize(scale.Font(c, scale.Size14)).TextColor(t.TextMuted)
			})
		})
		return
	}

	// 头图铺满整块 200 高（上游就是这一块自己的 `bg-cover`，不是「上面一条
	// 横幅 + 下面一块底色」），头像和信息压在底边。
	//
	// ⚠️ **信息那一行必须是 `Absolute`**：放进 Column 的流里的话，它和头图
	// 各占一份高度——头像 140 + 上下 16 就是 172，头图只剩 28，画出来是
	// 一条缝。Absolute 让它压在同一块 200 高上，和上游 `items-end` 一致。
	ui.Box(c).Key(key).Height(upHeaderH).FillWidth().Clip().Children(func() {
		// ① 头图。没设头图的 UP 主占大多数（B 站这时候给的是空对象而不是
		// 一张空白图），画一块纯色——**别去画 B 站的占位图**，那看着像加载失败。
		if h.User.Banner != "" {
			s.spaceFill(c, h.User.Banner, upHeaderH)
		} else {
			ui.Box(c).Height(upHeaderH).FillWidth().Background(t.Surface)
		}
		// ② 蒙版：**浅色 30% / 深色 50%**（上游 `space-info.tsx:83-86` 的
		// `overlayOpacity`）。
		//
		// ⚠️ **没有头图的时候也在**：上游那一层是 `bg-cover` 自己的底，
		// 没有图时它是白底 × 30% 黑 = #b2b2b2（上游实图实测值）。
		//
		// ⚠️ **它必须是 `Absolute`**：这一块是 `ui.Box`，而**框架的 Box 是
		// 一列**（`mygo/ui/api_gen.go:709`：「lays its children out in a
		// column」）——不是叠在一起的一摞。非绝对的子元素是**往下排**的，
		// 排在头图下面（还被这一块的 `Clip()` 剪掉），于是蒙版会变成
		// 「头图下面那一块黑」，而不是「压在头图上的那一层黑」。
		// 绝对定位的那一档才是**画在非绝对的那些之上**的（框架的绘制顺序：
		// 先画非绝对的，再画绝对的）。
		ui.Box(c).Absolute().Left(0).Right(0).Top(0).Bottom(0).
			Background(ui.RGBA(0, 0, 0, upHeaderMask(t.Dark)))
		// ③ 文字那一带再压一档（理由和数值的算法见 `spaceTextScrim`）。
		s.spaceTextScrim(c)
		// ④ 头像 + 昵称 + 签名 + 右边那一排关注和统计。圆角留白与间距照上游：
		// `px-8 py-4`、头像和文字 `space-x-4`、文字两行 `space-y-2`。
		//
		// 🔴 **这一行是「贴着上沿」摆的，不是「贴着下沿」**（`upHeaderRowTop`）：
		// 它一高（**系统字号一放大它就高**）就往下长，而**上面的字因此不会
		// 再往上跑**——上面那一段是留给「整块那一档黑」的（`spaceTextScrim`）。
		//
		// ⚠️ **贴着上沿时，默认字号下的画面一个字都不变**：那一行默认就是
		// 172 高（头像 140 + 上下各 16），上沿 28 + 高 172 = 下沿 200 —— 和
		// 「贴着下沿」时落在同一个位置。
		ui.Row(c).Key("space-head-info").Absolute().Left(0).Right(0).Top(upHeaderRowTop).
			Padding(upPad, upPadX).Gap(space.Px16).AlignItems(ui.Center).Children(func() {
			// 头像带 `shadow-lg`（上游 `space-info.tsx:89` 的 `shadow-lg`）。
			s.spaceAvatar(c, h.User.Face, upAvatarS, true)
			ui.Column(c).Gap(space.Px8).Children(func() {
				// ⚠️ **字重是 500（`weightMedium`），不是 600**（票 #241 / 报告
				// F-26）：600 在 `DESIGN.md` §二 的刻度里有那一档却没有用途，
				// 而这三个数字 / 名字正是「比正文重一档」的那一档。理由同
				// `spaceStat` 那处的注释。
				ui.Text(c, spaceName(h.User)).FontSize(scale.Font(c, scale.Size20)).FontWeight(weightMedium).MaxLines(1).Ellipsis("…").TextColor(upHeaderText())
				if h.User.Official != "" {
					// ⚠️ **限成一行**（上游那一行没限，但上游的头图区是**固定
					// 200 高**的，认证那一行一多就把下面那一行顶出画面）：那一行
					// 限成一行，那一列的高度就有个上限，头图区在最大的字号下也
					// 放得下。
					ui.Text(c, "认证："+h.User.Official).FontSize(scale.Font(c, scale.Size12)).
						TextColor(upHeaderTextDim()).MaxLines(1).Ellipsis("…")
				}
				if h.User.Sign != "" {
					// 上游是 `text-sm text-white/80`（`space-info.tsx:110`）：
					// 14，不是 13。
					ui.Text(c, h.User.Sign).FontSize(scale.Font(c, scale.Size14)).TextColor(upHeaderTextDim()).MaxLines(2)
				}
			})
			ui.Box(c).Grow(1)
			s.viewSpaceActions(c, sp, h)
		})
	})
}

// upHeaderMask 是头图区**整块**那一层黑蒙版的 alpha。
//
// 浅色 30%、深色 50%——和上游一样（票 #295 / 报告 L-13）。
func upHeaderMask(dark bool) float32 {
	if dark {
		return scrim.HeaderDark
	}
	return scrim.HeaderLight
}

// upHeaderTextScrim 是**有字的那一带**再压的那一档的 alpha。
//
// 数值是「压到白字 ≥4.5:1」算出来的（算法在 `widgets/scrim` 的注释里）。
func upHeaderTextScrim(dark bool) float32 {
	if dark {
		return scrim.HeaderTextDark
	}
	return scrim.HeaderTextLight
}

// upHeaderText 是头图区上那些字的颜色：**白字，不跟主题翻**。
//
// ⚠️ **上游也是白字**（`space-info.tsx:100` 的 `text-white`、`:110` 的
// `text-white/80`）：头图区是**一张照片**，它永远是「深底」——字要是跟着
// 主题翻，浅色主题下就是深字压深底，那正是这层蒙版要防的那件事。
//
// ⚠️ **别拿 `t.Text` / `t.TextMuted` 代替**：那一档在浅色下是**深色**的
// （#18181b），压在压暗过的照片上就是「深字压深底」。
func upHeaderText() ui.Color {
	return ui.RGB(255, 255, 255)
}

// upHeaderTextDim 是头图区上那些次要的字（认证、签名、统计的名字）。
//
// 92% 白——仓库里「压在深色上的白字」那一档（`scrim.OnScrim`）。
//
// ⚠️ **92% 不是随便取的**：压在最坏情况（纯白头图）的底上，白字是 5.33:1
// / 5.25:1，92% 的白是 **4.80:1 / 4.73:1**——都还在 4.5:1 之上。
func upHeaderTextDim() ui.Color {
	return ui.RGBA(255, 255, 255, scrim.OnScrim)
}

// spaceTextScrim 是头图区里**有字的那一带**再压的一档黑。
//
// ## 为什么需要它
//
// 整块那一档蒙版是**平**的，而头图是**用户上传的任意图片**——最坏情况是
// 一张**纯白**的头图。浅色下 30% 的黑只把纯白压到 #b2b2b2，白字压在上面
// 只有 **2.1:1**（读不出来）；深色下 50% 只压到 #7f7f7f，**4.0:1**（大字够、
// 小字不够）。所以有字的那一带再压一档（数值见 `upHeaderTextScrim`）。
//
// ## 它盖的是哪一块
//
// **整块**那一档蒙版还是上游那一档（浅 30% / 深 50%），多出来的这一档
// **只盖住有字的那一带**——所以「蒙版在位（浅 30% / 深 50%）」那一条量的是
// 头图区**最上面**那一段（那里没有这一档）。
//
// ## 满档落在**信息那一行的内容上沿**（一个常数）
//
// 那一行（头像 + 名字 + 签名 + 统计）是**贴着上沿**摆的（`upHeaderRowTop`），
// 所以**它一高就往下长**，而**里面的字永远在 44 以下**——名字是那一列的第一件
// 东西，那一列在内容框里是**居中**的，于是它的上沿 ≥ 44。
//
// ⚠️ **贴着下沿摆的时候，名字会被顶进渐变的淡区**：那一行贴着下沿，字号一
// 放大就**往上**长——名字的上沿跟着跑（实测：字号 1 倍时在头图区里 71 的
// 地方，1.75 倍 → 44，2 倍 → 27，**2.25 倍（Windows 文本大小的上限）→ 9**），
// 于是它跑进上面那一段（那是「整块那一档黑」，#b2b2b2）里，白字压着它只有
// **2.12:1**——而那正是这张票要防的那件事（票 #295 的评审在 225% 那档量到的
// 就是它）。
//
// ⚠️ **「算出来」的那一版也不行**：行高是**字体自己的事**（换一款字体就差一
// 截），而框架把行与行之间的行距算在**摆出来的那一行**里、不在
// `c.MeasureText` 给的那一档里——按量出来的数推位置，两处都不准。
func (s *Shell) spaceTextScrim(c *ui.Context) {
	alpha := upHeaderTextScrim(c.Theme().Dark)
	// 满档的高度（头图区自己的坐标），渐变往上是 12 像素；
	// 再往上**不蒙**（那是「整块那一档黑」的原样）。
	top := float32(upHeaderTextTop)
	plain := top - upHeaderScrimFade
	ui.Box(c).Absolute().Left(0).Right(0).Top(0).Bottom(0).
		LinearGradient(ui.LinearGradient{Angle: 180, Stops: []ui.GradientStop{
			{Color: ui.Transparent, At: 0},
			{Color: ui.Transparent, At: plain / float32(upHeaderH)},
			{Color: ui.RGBA(0, 0, 0, alpha), At: top / float32(upHeaderH)},
			{Color: ui.RGBA(0, 0, 0, alpha), At: 1},
		}})
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

	ui.Row(c).Key("space-actions").Gap(space.Px16).AlignItems(ui.Center).Children(func() {
		// 关注按钮**只在「登录了、且看的不是自己」时画**（上游
		// `space-info.tsx:100` 的 `Boolean(user?.isLogin) && !isSelf`）。
		// 看自己的时候画一个「关注自己」是 B 站网页版都不做的事。
		if !isSelf {
			switch h.Follow(s.app.SelfMid(), s.app.LoggedIn()) {
			case app.FollowDone:
				// ⚠️ **关注是写接口**（要 CSRF、要网络、一个往返）。在界面线程
				// 上直接调就是每按一次冻一下窗口，所以开后台；
				// `UpSpace.Follow` 自己会叫醒界面（铁律三）。
				if ui.Button(c, "已关注").Clicked() {
					sp.follow(false)
				}
			case app.FollowAskable:
				if ui.PrimaryButton(c, "关注").Clicked() {
					sp.follow(true)
				}
			case app.FollowFailed:
				// 🔴 **没查到就说没查到**。画一个能按的「关注」比不画更糟：
				// 用户按下去是一次**取关**（B 站对没关注过的人「取关」是成功的），
				// 于是他更确信自己关注过——而真相是我们压根没读到。
				ui.Text(c, "关注状态没读到").FontSize(scale.Font(c, scale.Size12)).TextColor(upHeaderTextDim())
				if flatButton(c, "重试").FontSize(scale.Font(c, scale.Size12)).TextColor(upHeaderTextDim()).Clicked() {
					sp.header.RetryRelation()
					sp.header.Load()
				}
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
			// 🔴 **没查到就画「没查到」，不画 0**。粉丝数 0 是一个**假数**
			// （真实值可能是十几万），而它看起来完全正常——这是这一块最难受
			// 的失败方式。等级来自 acc/info、登录就能读到，所以照画。
			if h.StatOK() {
				spaceStat(c, "粉丝数", formatCount(h.Stat.Follower))
				if isSelf {
					spaceDivider(c)
					spaceStat(c, "关注数", formatCount(h.Stat.Following))
				}
				spaceDivider(c)
			} else if h.StatFailed() {
				ui.Text(c, "粉丝数没读到").FontSize(scale.Font(c, scale.Size12)).TextColor(upHeaderTextDim())
				if flatButton(c, "重试").FontSize(scale.Font(c, scale.Size12)).TextColor(upHeaderTextDim()).Clicked() {
					sp.header.RetryRelation()
					sp.header.Load()
				}
			}
			spaceStat(c, "等级", fmt.Sprintf("Lv%d", h.User.Level))
		}
	})
}

// follow 关注或取关（后台跑，失败给一句人话）。
//
// 单独的辅助是为了 `viewSpaceActions` 那两个按钮不各写一遍 goroutine。
func (sp *upSpace) follow(yes bool) {
	go func() {
		if err := sp.header.Follow(context.Background(), yes); err != nil {
			sp.notify("关注没改成："+err.Error(), notice.Danger)
		}
	}()
}

// spaceStat 是一个统计数字（上面是数、下面是名字）。
//
// 字号照上游 `space-info.tsx:131-133`：数字 `text-lg`（18），名字 `text-sm`（14）。
//
// ⚠️ **字重取 500（`weightMedium`），不是上游那个 600**（票 #241 / 报告 F-26）。
// 上游是 `font-semibold`，而 600 在 `DESIGN.md` §二 的刻度里**有这一档却没有
// 用途**——「600 是无主」：没有人能说出这三个数字为什么该是 600。500 才是
// 仓库里唯一被命名的字重，用途正是「比正文重一档但不到标题那么重」。
//
// ⚠️ **字色是白字**（`upHeaderText` / `upHeaderTextDim`）：这一块压在头图区
// 那张照片上，而头图区**永远是深底**——用主题的正文色的话，浅色主题下就是
// 深字压深底。
func spaceStat(c *ui.Context, label, value string) {
	ui.Column(c).Key("space-stat:" + label).Gap(space.Px2).AlignItems(ui.Center).Children(func() {
		ui.Text(c, value).FontSize(scale.Font(c, scale.Size18)).FontWeight(weightMedium).TextColor(upHeaderText())
		ui.Text(c, label).FontSize(scale.Font(c, scale.Size14)).TextColor(upHeaderTextDim())
	})
}

// spaceDivider 是统计之间那条竖线：上游 `Divider orientation="vertical"
// className="h-4"`（`space-info.tsx:136-137`），高 16。
//
// ⚠️ **它也是白字那一族**：主题那一档分隔线在浅色下是 #dbdbdb（亮）、在深色下
// 是 #3f3f46（暗）——后者压在头图区那块深底上几乎看不见。这里是 35% 的白，
// 两套主题下都看得见（图形只要 3:1）。
func spaceDivider(c *ui.Context) {
	ui.Box(c).Width(1).Height(16).Background(ui.RGBA(255, 255, 255, spaceDividerAlpha))
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
//
// withShadow 说要不要带一圈 `shadow-lg`（Tailwind：`0 10px 15px -3px` 与
// `0 4px 6px -4px`，都是 10% 黑）。**只有大号头像带**——上游给 UP 空间头像
// 那一处写了 `shadow-lg`（`space-info.tsx:89`），列表里那些小头像没有；
// 全都加上去的话，一排动态的作者头像会各带一圈影，看起来像浮在卡片上。
func (s *Shell) spaceAvatar(c *ui.Context, face string, side float32, withShadow bool) {
	s.spaceAvatarZoom(c, face, side, withShadow, "", false)
}

// spaceAvatarZoom 是 spaceAvatar 加一个「指针移上去放大」。
//
// zoomKey 非空串时才放大——那一格每一帧都要重建，而动画的值记在**节点的状态**
// 上，身份跨帧不变才有那份状态（见 `hoverzoom.go` 的文件头）。on 是这一帧指针
// 在不在它上面。
//
// ⚠️ **放大的是整个头像（那个圆），不是里面那张图**：上游那一处写的是
// `hover:scale-105`，加在 `<Avatar>` 上（`dynamic-feed/user.tsx:46`），而 Avatar
// 整个是圆的——所以圆自己长大 5%。图在框里放大是另一回事（那是封面那一处）。
func (s *Shell) spaceAvatarZoom(c *ui.Context, face string, side float32, withShadow bool, zoomKey string, on bool) {
	t := c.Theme()
	box := ui.Box(c).Size(side, side).Radius(side / 2).Clip().Label(face)
	if withShadow {
		box = shadow.TailwindLG(box)
	}
	if zoomKey != "" {
		box = hoverZoom(c, box, zoomKey, on)
	}
	box.Children(func() {
		if st := s.images.Get(face); face != "" && st.Ready() {
			ui.Image(c, st.Bitmap).Fit(ui.Cover).Size(side, side)
			return
		}
		ui.Box(c).Size(side, side).Radius(side / 2).Background(t.Surface)
	})
}

// spaceFill 画一张铺满宽度、指定高度的网络图（头图）。
//
// ⚠️ **铺满是 `FillWidth()`（percent 100），不是 `Width(0)`**：`Width(v)` 的
// 单位是 DIP，`Width(0)` 就是**宽 0**（`mygo/ui/element.go:755-756`），而布局里
// 「只要设过宽度就按它算」（`layout.go:312`）。写成 `Width(0)` 的后果是整块
// 头图被压成零宽：昵称只剩一个省略号、头图永远画不出来、统计折成两行。
//
// 形状上不裁圆角：头图是横幅，方角是本来的样子（`space-info.tsx:86` 的
// `bg-cover bg-center` + 一层黑色蒙版）。
func (s *Shell) spaceFill(c *ui.Context, url string, h float32) {
	t := c.Theme()
	ui.Box(c).Height(h).FillWidth().Clip().Children(func() {
		if st := s.images.Get(url); st.Ready() {
			// ⚠️ **`FillWidth().Height(h)` 不能省**：只写 `Fit(ui.Cover)` 的话，
			// 图片按**自己的像素尺寸**摆，小图只铺满左边一截，右边露出底色。
			ui.Image(c, st.Bitmap).Fit(ui.Cover).FillWidth().Height(h)
			return
		}
		ui.Box(c).Height(h).FillWidth().Background(t.Surface)
	})
}

// spaceThumb 画一张指定大小的网络图，四态与 `imgcache.View` 一致
// （空地址画空框、没好画底色、失败画底色），只是形状不跟着主题圆角走。
//
// ⚠️ **带一个 `Label`**：没有它这些图对方块是全隐形的——读屏软件念不出来，
// 无头 Tester 也量不到（它只认 label / text，不认图片）。头像和头图同理。
func (s *Shell) spaceThumb(c *ui.Context, url string, w, h float32) {
	s.spaceThumbZoom(c, url, w, h, "", false)
}

// spaceThumbZoom 是带 hover 放大的那一版封面。
//
// zoomKey 非空串时才放大（理由同 `spaceAvatarZoom`）。on 是这一帧指针在不在
// 它上面。
//
// ⚠️ **放大的是里面那张图，不是外面那个框**：上游那一处是
// `<Image className="h-full w-full transition-transform duration-300
// group-hover:scale-105">`（`user-profile/grid-card/index.tsx:26`），而那个框是
// `overflow-hidden` 的——于是画面上是「图在框里涨大 5%」，**框不动**。
//
// 框要是自己长大，它就会压到旁边的字上（那一行是「封面 + 标题 + 条数」）。
//
// ⚠️ **标签挂在外面的框上**：外面那个框**不放大**（放大的是框里那张图），所以
// 标签挂在框上，量到的就是**框**的尺寸——而框不动正是上游的样子。
func (s *Shell) spaceThumbZoom(c *ui.Context, url string, w, h float32, zoomKey string, on bool) {
	t := c.Theme()
	ui.Box(c).Size(w, h).Clip().Label(url).Children(func() {
		if st := s.images.Get(url); url != "" && st.Ready() {
			img := ui.Image(c, st.Bitmap).Fit(ui.Cover).Size(w, h)
			if zoomKey != "" {
				hoverZoom(c, img, zoomKey, on)
			}
			return
		}
		// ⚠️ **还没下下来的那一格不放大**：放大的是**那张图**，还没下下来的图
		// 没有可放大的东西。上游也一样（`emptyPlaceholder` 是另一个元素，那个
		// `scale-105` 加在 `<Image>` 上）。
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
