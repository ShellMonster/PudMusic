// 分页列表：拉页、防重入、取消，和「容器没填满自动续拉」那三重保障。
//
// 底座是同一包里的 List（虚拟列表 + 音乐卡片）。这一层只管三件事：第几页
// 该拉了、拉的时候别有人再拉、拉回来的行什么时候该继续拉。画法仍然交给 List。
//
// 抄的是 Biu 的 `src/components/virtual-page-list/index.tsx`（PLAN 阶段 4.3，
// C-12）。它用**三条保障**把「容器没填满」这件事补上，缺一条就会在某个场景下
// 停住不动：
//
//	① 首次渲染检查：内容一换就查一次容器填没填满（Biu 的 checkFill effect）
//	② 滚到底：末尾那一行（哨兵）进了画面就拉下一页
//	③ 加载结束后再查一次：锁一放就再查一遍（Biu 的 loading effect）
//
// ⚠️ 最容易漏的是①和③合起来那一条：**筛选后只剩两页**。用户没滚过，容器却
// 没填满，于是底下留一大片空白还一直转圈——那正是这一票验收里点名的失败模式。
//
// 🔴 **这三条的判据是共享的**（见上面「三重保障的判据」那几个纯函数）：
// 评论页（`internal/ui/comments.go`）的底座不一样（行高是量出来的），但
// 「该不该拉下一页」判的是同一件事 ⇒ 它调的是同一份判据，不另写一套。
//
// 另外两条是 Biu 没有、PLAN 的风险栏里点名要补的：
//
//   - **防重入**：Biu 用一个 loadLock ref。三条保障挨着跑（Biu 的 effect 也是
//     挨着的），没有这把锁一次滚动能发三个请求。
//   - **取消与旧结果作废**：连点筛选条件时，旧请求的结果不能盖掉新条件的
//     （代次 gen 就是干这件事的）。
//
// ⚠️ 三条「panic 不能杀进程」的约定，这一层都自己包了：
//
//   - **Fetcher 在后台 goroutine 里**：崩了会直接带走整个进程（那一层 recover
//     接不到），所以 fetch 外面包一层，崩了当成一个错走正常那条路（见 fetchOne）。
//   - **View**：包 guard.Wrap（行级那一层见 List.guarded，页级那一层在
//     `internal/ui/router.go` 的 Shell.page）。
package list

import (
	"context"
	"log"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/ui/guard"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/flat"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/egoist/mygo/ui"
)

// maxWakes 是记着的唤醒句柄上限：一个窗口一条，多出来的挤掉最久没画的那个
// （它已经不画了，句柄留着是空转）。
const maxWakes = 4

// Page 是一次拉页的结果。
type Page struct {
	// Songs 是这一页的歌，**按顺序接在已有那些后面**（重了的会被去掉，见
	// apply）。
	Songs []Music
	// HasMore 后面还有没有。false 就是到底了，自动补屏就此停手。
	HasMore bool
}

// Fetcher 拉一页歌。page 从 1 起。
//
// ctx 是这一页自己的生命周期：换查询或切路由时会被取消，页面据此扔掉在途请求
// （Biu 那边是组件卸载，效果一样）。
//
// ⚠️ 它在**后台 goroutine 里**调：里面不许碰界面状态，要通知界面就往返回值里放。
// 它要是 panic 了，这一页就当失败处理（见 fetchOne）。
type Fetcher func(ctx context.Context, page int) (Page, error)

// Paged 是一页分页列表此刻的状态。页面照它画（转圈 / 重试 / 「还有多少」），
// 测试也照它断言。
type Paged struct {
	// Page 是已经拿到的最后一页。0 是还一页都没有。
	Page int
	// Loaded 是已经拿到、而且画出去的行数（过了过滤器的那几行）。
	Loaded int
	// Hidden 是这一份过滤器拒掉的行数。没装过滤器的列表恒为 0。
	Hidden int
	// Duration 是**收到过的**每一行的时长之和，被滤掉的行也算——表头那个
	// 「总时长」说的是这个容器的总量，不是「当前可见的那一部分」。
	Duration time.Duration
	// HasMore 后面还有没有。
	HasMore bool
	// Loading 是「正在拉」。**失败、超时、返回不对、拉页器崩了都必须是
	// false**——不然界面永远转圈（见 apply）。
	Loading bool
	// Err 是上一次拉页的错，成功时为 nil。
	Err error
}

// Received 是这一份已经收到过的行数（画出去的 + 滤掉的）。
//
// ⚠️ **翻页进不进展看的是它，不是 Loaded**：一页全是被滤掉的行也算进展
// （Pager 的 added==0 停手规则看的是过滤前的行数，见 `NewPagedFiltered`）。
func (p Paged) Received() int { return p.Loaded + p.Hidden }

// Settled 报「这一份已经停稳」：拿到过至少一页、没有一趟在途、后面也没有了
// （或者上一趟失败了——那一步之后等的是人点重试，不是列表自己再拉）。
//
// 它是「全部行都被滤掉了」这个结论的前置闸：只要后面还有页、或者还有一趟
// 在途，就不能先说「这里全是失效内容」。
func (p Paged) Settled() bool {
	return p.Page > 0 && !p.Loading && (!p.HasMore || p.Err != nil)
}

// # 三重保障的判据（这一份是共享的）
//
// 三重保障（①首次渲染检查 / ②滚到底 / ③容器没填满）判的是同一件事：
// **这一帧该不该拉下一页**。底座不一样的时候（音乐卡片列表 vs 评论那种
// 变高正文），判据必须只有一份——两份判据迟早分叉，而分叉出来的那种差异
// 极难查（一个列表会补屏，另一个不会，而且两边看着都「对」）。
//
// ⇒ 判据写成下面这几个纯函数，**谁用都调它**：
//
//   - 这一份自己（`PagedList`：见 Sync / afterFrame）
//   - 评论页（`internal/ui/comments.go`）：底座是框架原生的 `ui.List`（行高是
//     量出来的），但**三重保障的判据是同一份**。
//
// ⚠️ **判据是纯函数**：它只看传进来的状态，不碰界面状态，也不发请求——
// 拉不拉是调用方的事。

// PagingState 是「拉页那几样状态」。
//
// ⚠️ **它不掺任何界面上的东西**（行号、画面、滚动位置）：那些由判据的另外
// 几个参数带进来（见 `DecideNextPage`）。
type PagingState struct {
	// Fetched 是「已经拿到过至少一页」。
	Fetched bool
	// Loading 是「有一趟在路上」。
	Loading bool
	// Blocked 是「上一次失败了，自动补屏先停在这儿，等人点重试」。
	Blocked bool
	// HasMore 是「后面还有没有」。
	HasMore bool
}

// PagingAction 是「这一帧该做什么」。
type PagingAction int

const (
	// PagingNone 是「什么都不做」。
	PagingNone PagingAction = iota
	// PagingLoadFirst 是「去拉第一页」（保障①首次渲染检查）。
	PagingLoadFirst
	// PagingLoadNext 是「去拉下一页」（保障②滚到底 / ③容器没填满）。
	PagingLoadNext
)

// PagingDecision 是判据的结果。
type PagingDecision struct {
	// Action 是该做什么。
	Action PagingAction
	// Scrolled 是「用户是滚过去的」（保障②）还是「容器自己没填满」（保障③）。
	//
	// ⚠️ **它不影响拉不拉**，只影响「为什么拉这一趟」的说法（记日志、界面
	// 上的一句话）——两支都是「该拉就拉」。
	Scrolled bool
}

// MayLoadNext 报「按状态看，这一帧有可能拉下一页」。
//
// 它是②③的前置闸门：**正在拉 / 上一趟失败了 / 已经到底了**这三种情形下
// 不该再拉。
func MayLoadNext(st PagingState) bool {
	return !st.Loading && !st.Blocked && st.HasMore
}

// DecideFirstPage 判保障①：这一帧该不该去拉第一页。
//
// ⚠️ **已经拿到过就不拉**（`Fetched`）：而「拿到过」包含**一条都没有**
// ——那一档是「还没有人评论」，不是「还没问过」。
func DecideFirstPage(st PagingState) PagingDecision {
	if st.Fetched || st.Loading || st.Blocked {
		return PagingDecision{}
	}
	return PagingDecision{Action: PagingLoadFirst}
}

// DecideNextPage 判保障②③：这一帧该不该去拉下一页。
//
// first/last 是**上一帧画出来**的那一段行号（`ListState.Visible()` 报的），
// rows 是这个列表的总行数（**含末尾那一行哨兵**）。
//
// ⚠️ **哨兵那一行进没进画面就是全部**：它在画面外 ⇒ 等用户滚过去；它在
// 画面里 ⇒ 该拉了（至于用户滚没滚过，只影响「为什么拉」那句话）。
func DecideNextPage(st PagingState, first, last, rows int) PagingDecision {
	if !MayLoadNext(st) {
		return PagingDecision{}
	}
	// 末尾还在画面外：什么都不做，等用户滚过去。
	if rows <= 0 || last < rows-1 {
		return PagingDecision{}
	}
	return PagingDecision{Action: PagingLoadNext, Scrolled: first > 0}
}

// PagedList 是一个会自己往下翻页的列表。
//
// 建一次，存进页面自己的状态里，每一帧调 View（同 List）。
type PagedList struct {
	// draw 是包了 recover 的画法，见 View。建的时候包一次就够（每帧包一次是
	// 白花开闭包）。
	draw func(*ui.Context)

	// surf 是底下那一对控件：设置里的「数据显示样式」三档由 `SwitchView` 分发
	// （见 view.go）。所以这一层**不问**
	// 「现在画的是哪一个」，问的都是「末尾那行进没进画面」这种和摆法无关的事。
	//
	// 名字用 surf（surface，「面」）而不是 view：这一层已经有 `view` 那个方法了。
	surf  *SwitchView
	pager *Pager[Music, struct{}]

	// ── 下面这一摊只由界面线程读写（铁律一）────────────────────────────────

	// items 是已经拿到、**画出去**的行。**追加在尾部**，所以每次翻页接的是新的一截，
	// 不会动到上面那些行的身份（铁律四，见 apply）。
	//
	// ⚠️ **被 keep 滤掉的行不进来**（装了过滤器的时候）：List / Grid 拿到的
	// 就是可见行，选择、批量、播放全部都不用认识「隐藏」这回事。
	items []Music

	// keep 是这一份的行过滤器（nil = 每行都留，和现在一样的行为）。
	//
	// ⚠️ **建的时候定死**（见 `NewPagedFiltered`）：换策略就是换一份列表，
	// 和换排序、换搜索词是同一条现有路——所以没有 setter。
	keep func(Music) bool

	// hidden 是这一份滤掉的行数；duration 是**收到过的**每一行的时长之和
	// （被滤掉的也算）。两个都只由界面线程写，跟着这一份实例走——重建列表
	// 就是新的一份，旧实例的在途结果写不到新实例上。
	hidden   int
	duration time.Duration

	page    int
	hasMore bool
	loading bool
	err     error

	// blocked 记着「上一页拉失败了，自动补屏先停在这儿」。
	//
	// 没有它就会变成一个自己打自己的循环：失败 → 解锁 → 下一帧看见容器还是
	// 没填满 → 又拉 → 又失败（PLAN 风险栏里 Biu 那份就缺这一条：解锁之后
	// checkFill 会立刻再拉一次）。用户点末尾那行的「重试」才继续。
	blocked bool

	// emptyView 是「第一页在途时那一格」的画法（见 SetEmptyView）。nil 是没装，
	// 那一格退回那句 13px 的「正在加载…」。
	//
	// ⚠️ **它只管「第一页在途」这一档**，别的一概不管（见 `syncList`）。
	emptyView func(c *ui.Context)

	// endView 是「**没有更多了**」那一行的画法（见 SetEndView）。nil 是没装——
	// 那一行就不画（**别的页就是这样**：它们到底了就是到底了，界面上不多说一句）。
	endView func(c *ui.Context)

	// lastEndShown 是上一帧末尾那一行是不是「没有更多了」那一档。
	//
	// 🔴 **它挡的是「换画法之后不重新吸附」**（2026-10-29 的视觉走查）：框架
	// 每一帧都是从**上一帧**的布局出发的，而末尾那一行是这一帧才换成「没有
	// 更多了」的——不主动滚一下的话，它按上一帧那档的估计高度摆，用户看到的是
	// 末尾那一行只露出半截（800 / 900 高的窗口上实测墨迹 11 行只剩 5 行），
	// 再滚一次才变对。
	lastEndShown bool

	// failureNotice 报「拉页失败时要不要说一句」（见 SetFailureNotice）。默认要。
	failureNotice bool

	// scroll 是这个列表滚到哪儿了。TrackScroll 每帧把偏移写回来，我们要靠它
	// 判断「滚过 400 像素没有」，也要靠它切路由时回顶。
	//
	// ⚠️ **它只在这一份挂了 ScrollState 时才有数**（见 SetScrollState）。
	scroll ui.ScrollState

	// scrollState 报这一份要不要挂那个 ScrollState（见 SetScrollState）。
	// 默认要。
	scrollState bool

	// rev 是 items 每变一次 +1。View 只在它变了才 SetSongs（SetSongs 要扫一遍
	// 查重复 ID，几千行每帧扫一遍是白花的开销）。
	rev int
	// shownRev 是上一次交给 List 的那一版的 rev。
	shownRev int

	// ── 跨 goroutine 的只有下面这两小摊 ──────────────────────────────────

	// wakes 是**每个画过这个列表的窗口**一条。deliver 全部叫醒：只叫醒一个的
	// 话，另一个窗口停在旧帧（实测：只投递不唤醒 = 0 帧刷新）。
	//
	// 认窗口靠 svc：weak.Pointer 的比较规则是「同一个窗口的两次 Services
	// 相等，换一个窗口就不相等」，所以同一窗口每帧重登记只占一条。
	wakeMu sync.Mutex
	wakes  []wakeEntry
	// frame 是登记了几次，用来认出「最久没画的那一个」。
	frame uint64
}

// wakeEntry 是一个窗口的唤醒句柄。
type wakeEntry struct {
	// svc 是这个窗口的身份（认它用），wake 是叫醒它的动作。
	svc  ui.Services
	wake func()
	// seen 是这个窗口上一次登记时的帧号。
	seen uint64
}

// NewPaged 建一个会自己翻页的列表。
//
// cache 和 acts 的意思同 New（封面缓存、这一页会做的操作）。fetch 是拉页的
// 那一支，见 Fetcher。
//
// ⚠️ **不在这里拉第一页**：NewPaged 可能在界面线程之外被调（页面的状态在
// 构造器里建），那时候还没有唤醒句柄也没有画面。第一页由第一帧的 view 拉。
func NewPaged(cache *imgcache.Cache, acts Actions, fetch Fetcher) *PagedList {
	return NewPagedFiltered(cache, acts, fetch, nil)
}

// NewPagedFiltered 是 NewPaged 加一个**建的时候就定死的**行过滤器。
//
// keep 在**界面线程**上跑，位置在 Pager 去重之后、List 看到之前。被它拒掉的
// 行记进 `Paged.Hidden`、不进 `items`，但**仍然算翻页进展**（apply 返回的是
// 过滤前的行数，Pager 的 added==0 停手规则看的就是那个数）——于是一页全是
// 被滤掉的行不会把自动补屏钉死。
//
// ⚠️ **没有 setter**：换策略就是换一份列表，和换排序、换搜索词是同一条现有
// 路（调用方重建 `PagedList`）。隐藏数天然属于一份列表实例。
func NewPagedFiltered(cache *imgcache.Cache, acts Actions, fetch Fetcher, keep func(Music) bool) *PagedList {
	p := &PagedList{
		surf: NewSwitchView(cache, acts), keep: keep,
		hasMore: true, failureNotice: true, scrollState: true,
	}
	p.pager = NewPager[Music, struct{}](func(ctx context.Context, page, _ int, _ struct{}) (PagerPage[Music], error) {
		pg, err := fetch(ctx, page)
		return PagerPage[Music]{Items: pg.Songs, HasMore: pg.HasMore}, err
	}, func(m Music) string { return m.ID }, func() struct{} { return struct{}{} })
	p.pager.SetApply(func(pg PagerPage[Music]) int {
		for _, m := range pg.Items {
			p.duration += m.Duration
			if p.keep != nil && !p.keep(m) {
				p.hidden++
				continue
			}
			p.items = append(p.items, m)
		}
		p.page = p.pager.Status().Page
		p.hasMore = pg.HasMore
		p.loading = false
		p.err = nil
		p.blocked = false
		p.rev++
		// 🔴 **返回过滤前的行数**：Pager 把 0 当「这一页没带来新行」当场停手。
		// 一页全被滤掉的行**也是进展**——后面可能还有没被滤掉的，滤成 0 返回
		// 就把「继续翻」停在了这一页上。
		return len(pg.Items)
	})
	p.pager.SetFailureHandler(func(page int, why PagerTrigger, err error) {
		p.page = p.pager.Status().Page
		p.loading = false
		p.err = err
		p.blocked = true
		if p.failureNotice {
			notify(p.surf.List().acts, loadFailToast(page, err), notice.Danger)
		}
	})
	p.draw = guard.Wrap(p.view, p.onPanic)
	return p
}

// List 是底下那个列表。页面要选中项、要滚到某一行、要接多选时用它。
//
// ⚠️ **网格那一档画的时候它照样有歌**（见 `SwitchView` 的文件头），所以 `At` /
// `Len` 两档都对：收藏夹那一页的多选 / 全选 / 批量删除走的正是这条，而那些代码不
// 知道此刻画的是哪一档。
func (p *PagedList) List() *List { return p.surf.List() }

// SetHighlight 设歌名里要标出的搜索词，传空串去掉。
func (p *PagedList) SetHighlight(q string) { p.surf.List().SetHighlight(q) }

// SetSelection 装多选那一摊接线（`Selection`），传零值去掉。
//
// ⚠️ **别用 `p.List().SetSelection(...)`**：那只装到了列表那半边，而网格那一档
// 画的是 `Grid`——格子上的勾选框是 `Grid.sel2` 画的（`grid.go` 的
// `viewCellCheckbox`）。漏了网格这半边的症状是「切到网格之后一格勾选框都没有，
// 逐首勾完全做不到，只剩全选 / 不选」——而全选走的是 `Len()` / `At()`，不经过
// 画法，所以它照样能用，于是界面上看着「多选还是好的」，只是九成用法没了。
//
// 这一条是 `attachSelection`（`internal/ui/collectionbatch.go`）唯一的正确出口。
//
// ⚠️ 只能由界面线程调。
func (p *PagedList) SetSelection(sel Selection) { p.surf.SetSelection(sel) }

// Selected 返回正在画的那一份里选中的那首。没有选中是 ok=false。
//
// ⚠️ 认的是**正在画的那一份**：用户能看见的光标在它上面（理由见
// `SwitchView.Selected`）。
func (p *PagedList) Selected() (Music, bool) { return p.surf.Selected() }

// SetView 换摆法。**每一帧都要调**：设置随时会变，而忘了调的后果是「改了设置
// 这一页没变」——正是本票要消灭的那一种。
func (p *PagedList) SetView(v View) { p.surf.SetView(v) }

// SetAlbumMode 见 `List.SetAlbumMode`。
func (p *PagedList) SetAlbumMode(m pud.AlbumMode) { p.surf.List().SetAlbumMode(m) }

// SetVideoCards 把网格那一档的格子画成原型的视频卡（UP 空间「投稿」，4 / 3 / 1 列）。
func (p *PagedList) SetVideoCards(on bool) { p.surf.SetVideoCards(on) }

// SetInsetX 见 `List.SetInsetX`。
func (p *PagedList) SetInsetX(x float32) { p.surf.SetInsetX(x) }

// SetColumns 换这一页多画哪两列（播放量 / 时间）。理由和行、表头那一份
// 都在 `Columns` 上（行和表头读同一份，别只装一边）。
//
// ⚠️ 只能由界面线程调。
func (p *PagedList) SetColumns(c Columns) { p.surf.SetColumns(c) }

// SetLead 装「这一份歌上面那一块」（页内头部），传 nil 去掉。
//
// ⚠️ **它占列表的第 0 行**，所以下面那些歌在框架那一层的行号整体挪了一格——
// 而这一份对外的 API（`Selected` / `At`）收的仍然是**歌的下标**。
//
// ⚠️ 只能由界面线程调。
func (p *PagedList) SetLead(lead *Lead) { p.surf.SetLead(lead) }

// SetEmptyView 换掉「**第一页在途时那一格**」的画法。
//
// ⚠️ **它只管这一档**：拉失败了、或者已经有歌了，那一格都不归它管（见
// `syncList`）。装上它的目的是让「正在加载…」那一格和「没有结果」那一格是
// **同一个形状**——而后者住在应用层（`internal/ui`），这一层够不着它的画法，
// 所以由外面把画法递进来。
//
// ⚠️ **传 nil 是退回那句 13px 的话**（这一层原本的样子）。
//
// ⚠️ 只能由界面线程调。
func (p *PagedList) SetEmptyView(fn func(c *ui.Context)) { p.emptyView = fn }

// SetEndView 换掉「**没有更多了**」那一行的画法。
//
// ⚠️ **不装就没有那一行**：别的页（收藏夹、稍后再看、历史……）到底了就是到底了，
// 界面上不多说一句话——而首页那一屏要说得清（票 #370 的验收第 2 条：「不是无限
// 转圈、不是空白」）。
//
// ⚠️ **它只在「一行歌都没有」之外的那一档管用**：一行都没有的时候整份内容是
// 空状态（由页面自己画），末尾那一行压根不画（见 `syncList`）。
//
// ⚠️ 只能由界面线程调。
func (p *PagedList) SetEndView(fn func(c *ui.Context)) { p.endView = fn }

// SetFailureNotice 装「拉页失败时要不要说一句」（Toast）。
//
// ⚠️ **默认要**：末尾那一行只说「加载失败，点此重试」，不说为什么——别的页
// （收藏夹那一族）就靠这一句告诉用户出了什么事。
//
// 而**页面自己有一整块失败态**的页（首页那一屏：一句话 + 一个重试按钮）把
// 它关掉：同一件事说两遍是噪音，而 Toast 那一句还带着 B 站的原始码。
//
// ⚠️ 只能由界面线程调。
func (p *PagedList) SetFailureNotice(on bool) { p.failureNotice = on }

// SetScrollState 装「这一份要不要挂一个 ScrollState」（滚动位置写进它）。
//
// ⚠️ **默认要**：回顶按钮靠它判「滚过 400 像素没有」——那是**精确的像素数**。
//
// ⚠️ **关掉它的场合：同一个位置上有几份列表轮流画，而它们的位置要各自记着**
// （首页那三个 Tab）。挂着的时候，框架把滚动位置记在**元素的位置**上，而
// 轮流占着同一个位置的几份列表会抢同一份：切回「音乐」落到第 6 首而不是它
// 原来的第 19 首（实测，票 #370）。
//
// 关掉它之后，那几份列表各自按**自己的行锚点**记位置（`ui.ListState`，框架
// 本来就是按行记的），互不干扰；而回顶那一枚的偏移改按行换算（列表那一档的
// 行高是定死的 64，差不到一行——见 `SwitchView.ScrollOffset`）。
//
// ⚠️ 只能由界面线程调。
func (p *PagedList) SetScrollState(on bool) { p.scrollState = on }

// ScrollOffset 是这一屏滚过了多少像素。回顶按钮判断「滚过 400 像素没有」用它。
//
// ⚠️ **没挂 ScrollState 的那些（见 SetScrollState）按行换算**：列表那一档的
// 行高是定死的 64，网格那一档是估的（见 `SwitchView.ScrollOffset`）。
func (p *PagedList) ScrollOffset() float32 {
	if p.scrollState {
		return p.scroll.Y
	}
	return p.surf.ScrollOffset()
}

// SetSongs 直接把已经拿到的行换掉，**不拉页**。
//
// ⚠️ **它是「外面已经有内容」的场合用的**（首页那一屏被外面塞进来一批歌、
// 测试）。装完之后这一份摆成「已经有一页、后面没有」的样子：下一次 Sync 不会
// 再去拉第一页（`page` 不是 0）。
//
// ⚠️ **换查询的正路是 Reload**（清空 + 从第一页重来 + 当场拉）。
//
// ⚠️ 只能由界面线程调，同 Reload。
func (p *PagedList) SetSongs(songs []Music) {
	// ⚠️ **种进 pager 的是原始行**（含被滤掉的）：它们的 key 以后翻页照样算
	// 见过。而 items 只收可见行，hidden / duration 跟着这一份实例重新算。
	visible := make([]Music, 0, len(songs))
	p.hidden = 0
	p.duration = 0
	for _, m := range songs {
		p.duration += m.Duration
		if p.keep != nil && !p.keep(m) {
			p.hidden++
			continue
		}
		visible = append(visible, m)
	}
	p.items = visible
	p.pager.Seed(songs)
	p.page = 1
	p.hasMore = false
	p.err = nil
	p.blocked = false
	// ⚠️ **换了一批歌，上一批的「已经到底」不算数**：不复位的话，上一批看到底
	// 之后换歌，新一批的结果在 `syncList` 跑到 `hasMore` 那一支之前就落地时，
	// `lastEndShown` 还是 true，该重新吸附的时候不吸附（同一操作随帧序给出两种
	// 结果）。
	p.lastEndShown = false
	p.rev++
}

// Layout 返回此刻的摆法。测试照它断言「这一页真的换了视图」。
func (p *PagedList) Layout() View { return p.surf.Layout() }

// Paged 返回这一页分页列表此刻的状态。
func (p *PagedList) Paged() Paged {
	return Paged{
		Page: p.page, Loaded: len(p.items), Hidden: p.hidden, Duration: p.duration,
		HasMore: p.hasMore,
		Loading: p.loading, Err: p.err,
	}
}

// First 返回已经拿到的第一行，不复制其余分页内容。
//
// 首页总览只需要一张磁贴的首项；整页动作仍使用 Items。只能由界面线程调。
func (p *PagedList) First() (Music, bool) {
	if len(p.items) == 0 {
		return Music{}, false
	}
	return p.items[0], true
}

// Items 返回已经拿到的全部行的一份拷贝（给「播放全部」这类整页动作用）。
func (p *PagedList) Items() []Music { return append([]Music(nil), p.items...) }

// Reload 换查询：关键词变了、筛选变了、或者整页换了一个路由。
//
// 停掉在途请求、清空已加载的行、回顶，然后从第 1 页重来。
//
// ⚠️ **旧结果作废**靠的是这里 bump 的代次：在途的那一页回来时代次对不上，
// 直接扔掉（PLAN 风险栏：「用户连点筛选条件时，旧请求的结果不能覆盖新条件的
// 结果」）。光 cancel 是不够的——请求已经回来了、正在往信箱里投的时候，
// cancel 拦不住它。
//
// ⚠️ **它当场拉第一页**：Reload 有可能是在 view 之外调的（比如后台事件），
// 那时候不一定还有下一帧。要「清空但等这一份被画到时再拉」，用 Reset。
//
// ⚠️ 只能由界面线程调。
func (p *PagedList) Reload() {
	p.Reset()
	p.pager.Reload()
}

// Reset 清空已加载的行，让这一份从第一页重来——**但不当场拉**。
//
// ⚠️ **它和 Reload 的分界就在「当场拉不拉」**：Reload 当场拉（调用方知道要什么），
// 而 Reset 只把状态摆回「一页都还没有」——**下一次 Sync（这一份被画到时）才拉
// 第一页**。
//
// 首页换账号时三档都要重来，而**只有正在看的那一档该发请求**：另外两档等用户
// 切过去再拉（那一档的 Sync 会替它拉第一页）。
//
// ⚠️ 只能由界面线程调。
func (p *PagedList) Reset() {
	p.pager.Reset()

	p.items = nil
	p.hidden = 0
	p.duration = 0
	p.page = 0
	p.hasMore = true
	p.err = nil
	p.blocked = false
	p.lastEndShown = false
	p.rev++
	p.ScrollTop()
}

// ScrollTop 回到顶上。切路由和点回顶按钮都走它。
//
// 两条都写是有原因的：`scroll` 是这一帧记下的偏移，列表自己按行的位置算锚点
// （mygo 的 ListState 注释：「A List keeps its place by its rows instead」），
// 两条都给才不管列表是刚建的还是一直在画的都是顶上。
//
// Biu 那边是平滑滚上去（`behavior: "smooth"`）。这里直接置 0：回顶是一个
// 「换页了」的动作，不是「往下看」，滑过去反而晃眼；想平滑就换成
// `p.scroll.ScrollTo(0, 0, ui.Smooth)`。
//
// ⚠️ 只能由界面线程调。
func (p *PagedList) ScrollTop() {
	p.scroll.Y = 0
	p.surf.scrollTop()
}

// View 画这一页。页面在每一帧开头调一次，装好 Grow(1) 的位置（同 List.View）。
//
// 🔴 外面包着 recover（`guard.Wrap`，建的时候包一次，见 NewPaged）：view 里
// panic 会一路穿到 Cocoa 事件循环上去把整个进程带走。画在页面里时页级那道
// Guard（`internal/ui` 的 `Shell.page`）也接得住，但这个控件不是只给页面用的
// ——它自己这一道必须有，包一层是这里最便宜的保险。
// TakeFocus 把键盘焦点交给底下那一对控件此刻画的那一档（票 #245 的 E4）。
func (p *PagedList) TakeFocus() { p.surf.TakeFocus() }

// SelectFirst 把选中项放到第一首（见 `List.SelectFirst`）。
//
// ⚠️ 只能由界面线程调。
func (p *PagedList) SelectFirst() { p.surf.SelectFirst() }

func (p *PagedList) View(c *ui.Context) { p.draw(c) }

// onPanic 记一笔并要一帧。
//
// 要下一帧是必须的：recover 接住的是 build pass 中间，那一帧剩下的部分没建，
// 画出来是半张（`internal/ui` 的 Shell.onPanic 补 Invalidate 是同一个道理）。
//
// ⚠️ 这里**不换错误页**：坏的是这一个控件，页面上别的东西照常用。这和行级
// `List.guarded` 是同一个取舍（「几千行里坏一行」好过「整页变成错误页」）。
func (p *PagedList) onPanic(rec any) {
	log.Printf("pudmusic: 分页列表画崩了：%v\n%s", rec, debug.Stack())
	p.invalidate()
}

// Sync 是这一份分页列表每帧的**副作用**：登记唤醒句柄、收后台投回来的结果、
// 起该起的那一拉。
//
// ⚠️ **它和 `view` 的分工**：这一份列表的副作用（收结果、起第一拉）不能跟着
// 画一起停——满屏播放器开着的时候底层那一页不画（票 #161），而这一份列表的
// 结果一直落在信箱里没人取的话，用户关掉满屏看到的是「打开那一刻的样子」。
// 所以副作用在这一层，画在 `view` 里。
//
// ⚠️ **二重保障（②滚到底、③容器没填满）不在这里**：那两条都要用户先滚过去，
// 而满屏开着的时候用户滚不动底下那一页。它们留在 `view` 里，跟着画一起停。
//
// ⚠️ **副作用**：它们要读 `ListState.Visible()`，而那一句会置框架的 `read` 标志
// （`mygo/ui/list.go:225`）——不读它，框架就不会因为「可见区变了」自动补那一帧
// （`mygo/ui/list.go:1394-1400`）。满屏期间那一帧本来就是白补（画的是满屏），
// 而恢复之后的第一帧 `Visible()` 报的还是满屏之前的旧行号，「末尾还在画面外」
// 那个判断会晚一帧才成立。
func (p *PagedList) Sync(c *ui.Context) {
	p.setWake(c.Services())
	p.pager.Sync(c.Services())
	p.syncPagerState()
}

func (p *PagedList) syncPagerState() {
	st := p.pager.Status()
	p.page, p.hasMore = st.Page, st.HasMore
	p.loading, p.err, p.blocked = st.Loading, st.Err, st.Blocked
}

// view 是真的画法，View 是它外面那道 recover。
func (p *PagedList) view(c *ui.Context) {
	p.Sync(c)

	p.syncList()
	p.surf.View(c)

	// ②③ 在这里跑（必须排在 List.View 之后：Visible 报的是**上一帧**的行，
	// mygo 的 ListState.Visible 注释：「the row the list showed in the last frame」）。
	p.afterFrame()
	p.viewBackToTop(c)
}

// syncList 把这一帧该给底下那一对控件的东西给过去。只在真变了的时候才 SetSongs。
func (p *PagedList) syncList() {
	if p.rev != p.shownRev {
		p.surf.SetSongs(p.items)
		p.shownRev = p.rev
	}

	switch {
	case len(p.items) == 0 && p.loading:
		// 一行都还没有：第一页在途。这时候列表里那一句就是它全部的内容，
		// 页脚先收起来——同一句摆两遍（空状态一句 + 页脚一句）是噪音。
		//
		// ⚠️ **外面递了画法就用那一格**（`SetEmptyView`）：应用层要「正在加载」
		// 那一格和「没有结果」那一格是同一个形状（居中、15px、同一套留白），
		// 而这一层够不着那个画法。没递就用这一层原本那句 13px 的话。
		if p.emptyView != nil {
			p.surf.SetEmptyView(p.emptyView)
		} else {
			p.surf.SetEmpty("正在加载…")
		}
		p.surf.SetFooter(nil)
	case len(p.items) == 0 && p.blocked:
		// 一行都没有还失败了：页面上就留那一个重试按钮，一句话都不多说。
		p.surf.SetEmptyView(nil)
		p.surf.SetEmpty("")
		p.surf.SetFooter(p.footerView)
		p.surf.SetFooterLabel(footerLabel)
	default:
		p.surf.SetEmptyView(nil)
		p.surf.SetEmpty("")
		// 末尾那一行**只在还有下一页（或刚失败要人重试）时才有**（Biu 的
		// `count = hasMore ? items.length + 1 : items.length`）：拉到底之后它
		// 就不该再占一行空白。
		//
		// ⚠️ **到头了的那一行由外面递进来**（`SetEndView`）：没递就没有那一行
		// （别的页到底了就是到底了），递了就把「没有更多了」说清楚——不然用户
		// 只看到列表停在半截，不知道是加载坏了还是真的到头了。
		switch {
		case p.hasMore || p.blocked:
			p.surf.SetFooter(p.footerView)
			p.surf.SetFooterLabel(footerLabel)
			p.lastEndShown = false
		case p.endView != nil:
			p.surf.SetFooter(p.endView)
			p.surf.SetFooterLabel(EndLabel)
			// 🔴 **刚换成「没有更多了」、而且用户本来就停在底部时，重新吸附一次**
			// （2026-10-29 的视觉走查）：框架每一帧从**上一帧**的布局出发，而这一
			// 行是这一帧才换的画法、高度和上一档（转圈）不一样。不重新吸附的话它
			// 按上一帧那档的估计高度摆，末尾一行只露出半截——用户滚到底看到的就是
			// 被切的那一屏，得再滚一次才正常。
			//
			// ⚠️ **判据是「上一帧末尾已经在画面里」（`AtEnd()`），不是「刚换过去」**
			// （对抗性审查抓到的）：只看「刚换过去」的话，**用户没在底部也会被拽
			// 到底**——滚到一半往回翻、上一页的回包落地那一帧，列表会从当前位置
			// 直接跳到底（实测 offset 0 → 1200.9）；窗口高、一页装得下时自动补屏
			// 也会有同样的问题（打开页面看到的是列表末尾，不是开头）。
			//
			// ⚠️ **只在刚换过去的那一帧叫**（`lastEndShown` 记着上一帧是哪一档）：
			// 每帧都叫的话，用户往回滚一点就会被一直拽回底上，那一页就滚不动了。
			//
			// ⚠️ **换查询 / 换歌之后要复位**（`Reset` / `SetSongs` 两处都清）：
			// 不复位的话，上一次「到底」之后重新加载，新结果在 `syncList` 跑到
			// `hasMore` 那一支之前就落地时，`lastEndShown` 还是 true，该吸附的
			// 时候不吸附——同一操作随帧序给出两种结果。
			if !p.lastEndShown {
				if p.surf.List().AtEnd() {
					p.surf.ScrollToEnd()
				}
				p.lastEndShown = true
			}
		default:
			p.surf.SetFooter(nil)
			p.lastEndShown = false
		}
	}

	// 滚动位置要写回来：回顶按钮靠它（判「滚过 400 像素没有」）。
	//
	// ⚠️ **没挂的那几份不写**（见 SetScrollState）：它们各自按行锚点记位置。
	if p.scrollState {
		p.surf.SetScroll(&p.scroll)
	}
}

// pagingState 把这一份此刻的状态摊成判据要的形状。
//
// ⚠️ **Fetched 是「已经拿到过至少一页」**（`page > 0`）：而「一页是空的」也
// 算拿到过（`apply` 里照样 `p.page = r.page`）——那一档是「没有更多了」，
// 不是「还没问过」。
func (p *PagedList) pagingState() PagingState {
	return p.pager.PagingState()
}

// afterFrame 是三重保障的②③（①在 view 开头，见那里）。每一帧跑一次——三条保障
// 不是三个不同的钩子，是同一个判断（末尾那一行进没进画面）在不同时刻的三个取
// 值，所以这里每帧问一遍：「一页刚落地那一帧」和「刚拖过窗口大小那一帧」自然
// 就问到了。
//
// ⚠️ 必须排在 List.View 之后：Visible 报的是**上一帧画出来**的行。
func (p *PagedList) afterFrame() {
	st := p.pagingState()

	// 防重入闸门：正在拉、上一页失败了、或者已经到底，都不再拉。
	// （还一页都没有的那一条不在这里，见 view。）
	if !MayLoadNext(st) {
		return
	}

	// 🔴 **一行可见的都没有、但已经收到过行**：哨兵行被页内头部推到画面外了，
	// 而下面没有任何一行能把它顶上来——等用户滚过去也等不到，因为他面前连
	// 一行都没得看（装了过滤器的列表一页全被滤掉就是这样）。这时候不等
	// 几何判据，直接补屏——拉到一行可见的落地、或者后面真的没有为止。
	if len(p.items) == 0 && p.hidden > 0 {
		p.pager.Load(PagerRefill)
		return
	}

	first, last := p.surf.visible()
	n := p.surf.rows()
	d := DecideNextPage(st, first, last, n)
	if d.Action != PagingLoadNext {
		// 末尾还在画面外（或者列表里一行都没有）。什么都不做，等用户滚过去。
		return
	}
	if d.Scrolled {
		// ② 滚到底：哨兵行进了画面，而且列表不在顶上（是用户滚过去的，
		// 不是它自己没填满）。
		p.pager.Load(PagerScrollEnd)
		return
	}
	// ③ 加载结束后再检一次：还在顶上、末尾已经画在屏幕上 = 全部内容一屏
	// 摆得下。**这一支就是「筛选后只剩两页」的救命的那一支**：用户没滚过，
	// 少的就是它。
	p.pager.Load(PagerRefill)

}

// loadFailToast 是拉页失败时弹的那一句。
//
// ⚠️ **第一页失败不说「加载更多」**：那一页还没加载过任何东西，说「加载更多失败」
// 说的是一件用户当下没在做的事（他还没有什么可加载更多的）。第一页失败时这一屏
// **一个结果都没有**，用户读到的是「加载失败」——那才是真的。
//
// ⚠️ 后面的页仍然说「加载更多」：那几页确实是接着往下加载时失败的，而那一档
// 已经有结果在屏幕上了，说「加载失败」会让人以为整页都没了。
//
// 🔴 **原始错串里的 `biu:` 要剥掉**（票 #683 的复审）：那是我们自己的包名前缀
// （服务层各自包一层 `bili:` / `app:`，再被 `biu:` 收口），而这一句是**给用户
// 看的**——用户不知道「biu」是什么，读到的是一句看起来像内部日志的话。这一族
// 提示在所有分页列表上都用，所以前缀在这里一次剥干净。
func loadFailToast(page int, err error) string {
	msg := strings.TrimSpace(strings.TrimPrefix(err.Error(), "pudmusic: "))
	if msg == "" {
		// 空串（理论上不会有）也要给一句人话，别让「加载失败：」后面空着。
		msg = "出了点问题"
	}
	if page <= 1 {
		return "加载失败：" + msg
	}
	return "加载更多失败：" + msg
}

// setWake 登记这一帧的窗口（每个窗口一条，见 wakes）。只能由界面线程调。
func (p *PagedList) setWake(svc ui.Services) {
	p.wakeMu.Lock()
	defer p.wakeMu.Unlock()
	p.frame++
	for i := range p.wakes {
		if p.wakes[i].svc == svc {
			// ⚠️ **那个方法值只在真的要登记的时候才做**（`svc.Invalidate` 是堆上
			// 的一个闭包）：每帧都做一个就是每帧一次分配，而这一屏有逐帧分配的
			// 预算（见 `internal/ui/framebudget_test.go`）。
			p.wakes[i].seen = p.frame
			return
		}
	}
	// 一个窗口一条，所以这里每帧一个闭包不算多——**但它只在这一条真的登记
	// 的时候才做**（见上面那一句）。
	wake := svc.Invalidate
	if len(p.wakes) >= maxWakes {
		// 挤掉最久没画的那一个：那个窗口已经不画了，它的句柄留着是空转。
		oldest := 0
		for i := range p.wakes {
			if p.wakes[i].seen < p.wakes[oldest].seen {
				oldest = i
			}
		}
		p.wakes = append(p.wakes[:oldest], p.wakes[oldest+1:]...)
	}
	p.wakes = append(p.wakes, wakeEntry{svc: svc, wake: wake, seen: p.frame})
}

// invalidate 叫醒所有画过这个列表的窗口。任何 goroutine 都能调。
func (p *PagedList) invalidate() {
	p.wakeMu.Lock()
	wakes := slices.Clone(p.wakes)
	p.wakeMu.Unlock()

	for _, w := range wakes {
		if w.wake != nil {
			w.wake()
		}
	}
}

// footerView 画末尾那一行：还有就拉，没在拉就空着（但**占位**），拉失败就让人
// 点重试。
//
// 空着也占位是有意的：它是哨兵，滚到它就拉下一页（Biu 的 `count = hasMore ?
// items.length + 1 : items.length`）。把它藏起来的话「滚到底」就永远触发不了。
func (p *PagedList) footerView(c *ui.Context) {
	t := c.Theme()
	// ⚠️ 盖回页面底色（理由同 `LayoutRow`）：这一行是翻页哨兵，不是一条
	// 列表项——框架的行悬停底铺上来时，转圈和「加载失败」会骑在一块
	// 灰底上（网格那一档同型，见 `grid.go` 的 `viewFooterRow`）。
	row := ui.Row(c).AlignSelf(ui.Stretch).Center().Gap(rowGap).Padding(cardPadY, cardPadX)
	row = LayoutRow(row, t)
	row.Children(func() {
		if p.blocked {
			// 这是**行里的一句重试**，不是错误页上那颗主按钮：它跟着列表
			// 最后一行出现，用扁平配方（无常驻底、无描边），悬停才有一层淡底。
			if flat.Button(c, "加载失败，点此重试").FontSize(scale.Font(c, scale.Size13)).Clicked() {
				p.pager.Retry()
			}
			return
		}
		if p.loading {
			ui.Spinner(c).Label(footerLabel)
			ui.Text(c, "正在加载…").FontSize(scale.Font(c, scale.Size13)).TextColor(t.TextMuted)
		}
	})
}

// viewBackToTop 画回顶按钮：滚过 400 像素才出现。
//
// ⚠️ **画法在 `backtotop.go` 那一个出口**：这一枚原先在两个地方各写了一份（本包
// 这一份，和界面层那两页自己的分页列表），两份的数一样、代码不一样——改一份就有
// 一页对不上。
func (p *PagedList) viewBackToTop(c *ui.Context) {
	// ⚠️ **偏移是 0 就是「压根没滚过」**：内容还没列表高的时候 `MaxY` 是 0，
	// 而 `Y` 也是 0（见 `ScrollOffset`）——两种「没滚」在这里是同一件事。
	off := p.ScrollOffset()
	if off <= 0 {
		return
	}
	BackToTop(c, off, p.ScrollTop)
}
