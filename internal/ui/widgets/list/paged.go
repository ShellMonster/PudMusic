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
// 另外两条是 Biu 没有、PLAN 的风险栏里点名要补的：
//
//   - **防重入**：Biu 用一个 loadLock ref。三条保障挨着跑（Biu 的 effect 也是
//     挨着的），没有这把锁一次滚动能发三个请求。
//   - **取消与旧结果作废**：连点筛选条件时，旧请求的结果不能盖掉新条件的
//     （代次 gen 就是干这个的）。
package list

import (
	"context"
	"log"
	"sync"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/egoist/mygo/ui"
)

// backTopPx 是滚过多少像素才出现回顶按钮。
//
// **400 是死数，照抄** Biu `src/components/scroll-container/index.tsx` 的
// `setShowBackToTop((viewport?.scrollTop ?? 0) > 400)`。
const backTopPx = 400

// 回顶按钮摆在哪儿。Biu 是 `fixed right-10 bottom-[120px]`：右边 40（`right-10`
// 是 2.5rem），离底 120（躲开播放条）。我们照抄这两个数。
const (
	backTopRight = 40
	backTopUp    = 120
	// backTopRadius 是按钮的圆角：Biu 的 `radius="full"` 是药丸形，圆角给到
	// 比按钮高度大就等于它。
	backTopRadius = 999
)

// Page 是一次拉页的结果。
type Page struct {
	// Songs 是这一页的歌，**按顺序接在已有那些后面**。
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
type Fetcher func(ctx context.Context, page int) (Page, error)

// Paged 是一页分页列表此刻的状态。页面照它画（转圈 / 重试 / 「还有多少」），
// 测试也照它断言。
type Paged struct {
	// Page 是已经拿到的最后一页。0 是还一页都没有。
	Page int
	// Loaded 是已经拿到的行数。
	Loaded int
	// HasMore 后面还有没有。
	HasMore bool
	// Loading 是「正在拉」。**失败、超时、返回不对都必须是 false**——不然界面
	// 永远转圈（见 apply）。
	Loading bool
	// Err 是上一次拉页的错，成功时为 nil。
	Err error
}

// PagedList 是一个会自己往下翻页的列表。
//
// 建一次，存进页面自己的状态里，每一帧调 View（同 List）。
type PagedList struct {
	list  *List
	fetch Fetcher

	// ── 下面这一摊只由界面线程读写（铁律一）────────────────────────────────

	// items 是已经拿到的全部行。**追加在尾部**，所以每次翻页接的是新的一截，
	// 不会动到上面那些行的身份（铁律四，见 apply）。
	items   []Music
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

	// gen 是代次。每次换查询 +1：在途请求的结果回来时代次对不上就扔掉。
	// 这是 PLAN 点名要补的「旧查询结果作废」——Biu 只靠组件重建挡着。
	gen    uint64
	cancel context.CancelFunc
	// why 是当前这一页是被哪条保障叫起来的（见 trigger）。
	why trigger

	// scroll 是这个列表滚到哪儿了。TrackScroll 每帧把偏移写回来，我们要靠它
	// 判断「滚过 400 像素没有」，也要靠它切路由时回顶。
	scroll ui.ScrollState

	// rev 是 items 每变一次 +1。View 只在它变了才 SetSongs（SetSongs 要扫一遍
	// 查重复 ID，几千行每帧扫一遍是白花的开销）。
	rev int
	// shownRev 是上一次交给 List 的那一版的 rev。
	shownRev int

	// wake 是后台投递完结果之后叫醒界面的那个回调（铁律三）。每帧在 view
	// 开头重新登记，用的是这一帧的窗口句柄。
	wakeMu sync.Mutex
	wake   func()

	// ── 跨 goroutine 的只有下面这一小摊：后台只往信箱里投，界面线程取 ──────

	mu  sync.Mutex
	box []pageResult
}

// pageResult 是后台拉完一页之后投进信箱的东西。
type pageResult struct {
	// gen 是发这一页时的代次。对不上就是换查询之前的那个请求，作废。
	gen  uint64
	page int
	pg   Page
	err  error
}

// NewPaged 建一个会自己翻页的列表。
//
// cache 和 acts 的意思同 New（封面缓存、这一页会做的操作）。fetch 是拉页的
// 那一支，见 Fetcher。
func NewPaged(cache *imgcache.Cache, acts Actions, fetch Fetcher) *PagedList {
	p := &PagedList{
		list:  New(cache, acts),
		fetch: fetch,
	}
	return p
}

// List 是底下那个列表。页面要选中项、要滚到某一行时用它。
func (p *PagedList) List() *List { return p.list }

// Paged 返回这一页分页列表此刻的状态。
func (p *PagedList) Paged() Paged {
	return Paged{
		Page: p.page, Loaded: len(p.items), HasMore: p.hasMore,
		Loading: p.loading, Err: p.err,
	}
}

// Reload 换查询：关键词变了、筛选变了、或者整页换了一个路由。
//
// 停掉在途请求、清空已加载的行、回顶，然后从第 1 页重来。
//
// ⚠️ **旧结果作废**靠的是这里 bump 的代次：在途的那一页回来时代次对不上，
// 直接扔掉（PLAN 风险栏：「用户连点筛选条件时，旧请求的结果不能覆盖新条件的
// 结果」）。光 cancel 是不够的——请求已经回来了、正在往信箱里投的时候，
// cancel 拦不住它。
//
// ⚠️ 只能由界面线程调。
func (p *PagedList) Reload() {
	p.stop() // 含 p.loading = false
	p.gen++
	p.items = nil
	p.page = 0
	p.hasMore = true
	p.err = nil
	p.blocked = false
	p.rev++
	p.ScrollTop()
	// 当场拉第一页，不等下一帧：Reload 有可能是在 view 之外调的（比如后台
	// 事件），那时候不一定还有下一帧。
	p.load(trigFirstRender)
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
	p.list.ScrollTo(0, ui.Start)
}

// View 画这一页。页面在每一帧开头调一次，装好 Grow(1) 的位置（同 List.View）。
//
// ⚠️ **这里没有 `ui.Guard`**：`Guard` 住在 `internal/ui`，而这个包是界面的
// 底座（界面层将来 import 它，不是反过来），所以这一层拿不到它——好在也不
// 需要：它是**页面的一部分**，页面级的 recover 本来就兜着它
// （`internal/ui/router.go` 的 `Shell.page` 给每一页都包了一层）。本包自己
// 手里那一层是行级的（`List.guarded`），两层各管各的，不重复。
func (p *PagedList) View(c *ui.Context) {
	// 🔴 铁律三：把这一帧的唤醒句柄重新登记上。后台投递完结果要靠它叫醒界面，
	// 不唤醒就是「结果到了、画面停在旧帧」（实测 0 帧刷新）。
	p.setWake(c.Services().Invalidate)

	// 先收后台投回来的页，再画：这样这一帧画的就是**已经加上**的那几行，
	// 不用等下一帧。
	p.adopt()

	if p.rev != p.shownRev {
		p.list.SetSongs(p.items)
		p.shownRev = p.rev
	}
	// 一行都没有的时候，页脚就是唯一那一行：这时别再让空状态说「这里还没有歌」
	// ——它和页脚的话会打架（一句说没有、一句在转圈）。
	if len(p.items) == 0 && p.loading {
		p.list.SetEmpty("正在加载…")
	} else {
		p.list.SetEmpty("")
	}
	// 末尾那一行**只在还有下一页（或刚失败要人重试）时才有**（Biu 的
	// `count = hasMore ? items.length + 1 : items.length`）：拉到底之后它就不该
	// 再占一行空白。
	if p.hasMore || p.blocked {
		p.list.SetFooter(p.footerView)
	} else {
		p.list.SetFooter(nil)
	}
	// 滚动位置要写回来：回顶按钮靠它，三重保障里「在不在顶上」也靠它。
	p.list.SetScroll(&p.scroll)
	p.list.View(c)

	// 三重保障在这里面跑（必须在 List.View 之后：它读的是这一帧画出来的行）。
	p.afterFrame()
	p.viewBackToTop(c)
}

// adopt 取走信箱里的结果，一条一条落到界面的状态上。只能由界面线程调。
func (p *PagedList) adopt() {
	p.mu.Lock()
	box := p.box
	p.box = nil
	p.mu.Unlock()

	for _, r := range box {
		p.apply(r)
	}
}

// apply 落一页结果。
//
// 🔴 **锁在这一行解开，而且只有这一行解**（`p.loading = false`）：请求失败、
// 超时、返回格式不对（fetch 自己把那个错报出来），全走这一条。漏了它就是
// 「界面永远转圈」——验收里点名要钉住的那一条。
func (p *PagedList) apply(r pageResult) {
	if r.gen != p.gen {
		// 换查询之前发出去的那一页。作废，别让它盖掉新条件的结果。
		return
	}
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.loading = false

	if r.err != nil {
		// 解了锁，但**不再自动重试**：不然就是自己打自己（见 blocked）。
		p.err = r.err
		p.blocked = true
		log.Printf("biu: 第 %d 页拉失败（触发它的保障：%s）：%v", r.page, p.why, r.err)
		p.list.notify("加载更多失败：" + r.err.Error())
		return
	}

	p.err = nil
	p.blocked = false
	p.page = r.page
	p.hasMore = r.pg.HasMore
	if len(r.pg.Songs) == 0 {
		// 一页空的还说「还有」：接口在骗人，或者已经到头了。再拉就是空转。
		p.hasMore = false
	}
	// 🔴 铁律四：追加是**尾部**插 key（筛选后重建才是头部插，两条规则不一样）。
	// 尾部追加不动已有行的位置，所以「选中的那一首」翻完页还是它。
	p.items = append(p.items, r.pg.Songs...)
	p.rev++
}

// trigger 是「这次翻页是被哪一条保障叫起来的」。三条走的是同一段代码，
// 记下来只是为了**出错时说得清是哪一条**（见 apply 的那行日志）。
type trigger uint8

const (
	// trigFirstRender 是①首次渲染检查：这一页刚建起来，先要有东西看。
	trigFirstRender trigger = iota
	// trigScrollEnd 是②滚到底：末尾那一行进画面了，而且用户是滚过去的。
	trigScrollEnd
	// trigRefill 是③加载结束后再检一次（顺带也是拖窗口变高变矮之后的那次）。
	trigRefill
)

func (t trigger) String() string {
	switch t {
	case trigFirstRender:
		return "首次渲染"
	case trigScrollEnd:
		return "滚到底"
	default:
		return "容器没填满"
	}
}

// afterFrame 是三重保障。每一帧跑一次——三条保障不是三个不同的钩子，是同一个
// 判断（末尾那一行进没进画面）在不同时刻的三个取值，所以这里每帧问一遍：
// 「一页刚落地那一帧」和「刚拖过窗口大小那一帧」自然就问到了。
//
// ⚠️ 必须排在 List.View 之后：Visible 报的是**上一帧画出来**的行
// （mygo 的 ListState.Visible 注释：「the first and the last row the list
// showed in the last frame」）。
func (p *PagedList) afterFrame() {
	// ① 首次渲染检查：还一页都没有就先拉第一页。列在开关前面是故意的——
	// 还没拉过时 hasMore 是 false（还没人说过还有没有），拿它当闸门会把第一页
	// 也关掉。
	if p.page == 0 {
		p.load(trigFirstRender)
		return
	}
	// 防重入闸门：正在拉、上一页失败了、或者已经到底，都不再拉。
	if p.loading || p.blocked || !p.hasMore {
		return
	}

	first, last := p.list.state.Visible()
	n := p.list.rows()
	switch {
	case last < n-1:
		// 末尾还在画面外。什么都不做，等用户滚过去。
	case first > 0:
		// ② 滚到底：哨兵行进了画面，而且列表不在顶上（是用户滚过去的，
		// 不是它自己没填满）。
		p.load(trigScrollEnd)
	default:
		// ③ 加载结束后再检一次：还在顶上、末尾已经画在屏幕上 = 全部内容一屏
		// 摆得下。**这一支就是「筛选后只剩两页」的救命的那一支**：用户没滚过，
		// 少的就是它。
		p.load(trigRefill)
	}
}

// load 拉下一页。三重保障和用户点重试最后都走这里。
//
// 🔴 防重入就是第二行那把锁：三条保障会在同一帧里先后命中（Biu 那三个 effect
// 也是挨着跑的），没有它就是一次滚动三个请求。
func (p *PagedList) load(why trigger) {
	if p.loading || p.blocked || p.fetch == nil {
		return
	}
	// 第一页（page == 0）还没有「还有没有」可问，一律拉；往后由 hasMore 说。
	if p.page > 0 && !p.hasMore {
		return
	}

	next := p.page + 1
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel, p.loading, p.err, p.why = cancel, true, nil, why
	gen := p.gen // 代次只在界面线程上写，这里取一份给后台

	go func() {
		defer cancel()
		pg, err := p.fetch(ctx, next)
		p.deliver(pageResult{gen: gen, page: next, pg: pg, err: err})
	}()
}

// deliver 把一页结果投进信箱，然后叫醒界面。任何 goroutine 都能调。
//
// 🔴 铁律三：投递完**必须**唤醒。不唤醒就是「结果到了、画面停在旧帧」
// （实测 0 帧刷新）。
func (p *PagedList) deliver(r pageResult) {
	p.mu.Lock()
	p.box = append(p.box, r)
	p.mu.Unlock()

	p.wakeMu.Lock()
	wake := p.wake
	p.wakeMu.Unlock()
	if wake != nil {
		wake()
	}
}

// setWake 登记唤醒回调。每帧在 view 开头调（句柄是窗口的弱引用，窗口关了
// 自动失效）。
func (p *PagedList) setWake(fn func()) {
	p.wakeMu.Lock()
	p.wake = fn
	p.wakeMu.Unlock()
}

// stop 停掉在途的那一页。它只是**不让结果落地**：真正的作废靠 gen（Reload 里
// bump 的那个）。停请求是为了别让服务端白做一遍。
func (p *PagedList) stop() {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.loading = false
}

// footerView 画末尾那一行：还有就拉，没在拉就空着（但**占位**），拉失败就让人
// 点重试。
//
// 空着也占位是有意的：它是哨兵，滚到它就拉下一页（Biu 的 `count = hasMore ?
// items.length + 1 : items.length`）。把它藏起来的话「滚到底」就永远触发不了。
func (p *PagedList) footerView(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).AlignSelf(ui.Stretch).Center().Gap(cardGap).Padding(cardPadY, cardPadX).
		Children(func() {
			if p.blocked {
				if ui.Button(c, "加载失败，点此重试").FontSize(13).Clicked() {
					p.blocked = false
					p.load(trigRefill)
				}
				return
			}
			if p.loading {
				ui.Spinner(c).Label(footerLabel)
				ui.Text(c, "正在加载…").FontSize(13).TextColor(t.TextMuted)
			}
		})
}

// viewBackToTop 画回顶按钮：滚过 400 像素才出现。
func (p *PagedList) viewBackToTop(c *ui.Context) {
	// MaxY 是这一帧量出来的最大滚动量：0 就是内容还没列表高，压根滚不动。
	if p.scroll.Y <= backTopPx || p.scroll.MaxY <= 0 {
		return
	}
	t := c.Theme()
	// Overlay 把按钮浮在整窗之上（不占布局），Biu 的 `fixed` 就是这个意思。
	// Radius 给一个大数就是 Biu 的 `radius="full"`（药丸形）。
	ui.Overlay(c, func() {
		if ui.Button(c, "返回顶部").Absolute().
			Right(backTopRight).Bottom(backTopUp).
			Radius(backTopRadius).Background(t.SurfaceHover).Clicked() {
			p.ScrollTop()
		}
	})
}
