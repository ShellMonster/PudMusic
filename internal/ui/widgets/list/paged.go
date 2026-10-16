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
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"slices"
	"sync"

	"github.com/ShellMonster/Biu_Go/internal/ui/guard"
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
	// Loaded 是已经拿到的行数。
	Loaded int
	// HasMore 后面还有没有。
	HasMore bool
	// Loading 是「正在拉」。**失败、超时、返回不对、拉页器崩了都必须是
	// false**——不然界面永远转圈（见 apply）。
	Loading bool
	// Err 是上一次拉页的错，成功时为 nil。
	Err error
}

// PagedList 是一个会自己往下翻页的列表。
//
// 建一次，存进页面自己的状态里，每一帧调 View（同 List）。
type PagedList struct {
	// draw 是包了 recover 的画法，见 View。建的时候包一次就够（每帧包一次是
	// 白花开闭包）。
	draw func(*ui.Context)

	list  *List
	fetch Fetcher

	// ── 下面这一摊只由界面线程读写（铁律一）────────────────────────────────

	// items 是已经拿到的全部行。**追加在尾部**，所以每次翻页接的是新的一截，
	// 不会动到上面那些行的身份（铁律四，见 apply）。
	items []Music
	// seen 是 items 里已有的 ID。翻页时两页返回同一首歌很常见（B 站的翻页
	// 本来就可能重叠），不去重就是同一个 ID 出现两次：行身份直接串在一起
	//（Tester 当场 panic，真窗口里那两行共用一份状态——选中一个另一个也亮）。
	seen map[string]bool

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

	// mu 只护着信箱：后台往里投，界面线程在 view 开头取。
	mu  sync.Mutex
	box []pageResult
}

// wakeEntry 是一个窗口的唤醒句柄。
type wakeEntry struct {
	// svc 是这个窗口的身份（认它用），wake 是叫醒它的动作。
	svc  ui.Services
	wake func()
	// seen 是这个窗口上一次登记时的帧号。
	seen uint64
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
//
// ⚠️ **不在这里拉第一页**：NewPaged 可能在界面线程之外被调（页面的状态在
// 构造器里建），那时候还没有唤醒句柄也没有画面。第一页由第一帧的 view 拉。
func NewPaged(cache *imgcache.Cache, acts Actions, fetch Fetcher) *PagedList {
	p := &PagedList{
		list:  New(cache, acts),
		fetch: fetch,
		// 「还有没有」要等有人问过才知道。在那之前一律当作还有——不然第一帧
		// 会以为到底了，连页脚那一行都不画。
		hasMore: true,
		seen:    map[string]bool{},
	}
	p.draw = guard.Wrap(p.view, p.onPanic)
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
	p.seen = map[string]bool{}
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
// 🔴 外面包着 recover（`guard.Wrap`，建的时候包一次，见 NewPaged）：view 里
// panic 会一路穿到 Cocoa 事件循环上去把整个进程带走。画在页面里时页级那道
// Guard（`internal/ui` 的 `Shell.page`）也接得住，但这个控件不是只给页面用的
// ——它自己这一道必须有，包一层是这里最便宜的保险。
func (p *PagedList) View(c *ui.Context) { p.draw(c) }

// onPanic 记一笔并要一帧。
//
// 要下一帧是必须的：recover 接住的是 build pass 中间，那一帧剩下的部分没建，
// 画出来是半张（`internal/ui` 的 Shell.onPanic 补 Invalidate 是同一个道理）。
//
// ⚠️ 这里**不换错误页**：坏的是这一个控件，页面上别的东西照常用。这和行级
// `List.guarded` 是同一个取舍（「几千行里坏一行」好过「整页变成错误页」）。
func (p *PagedList) onPanic(rec any) {
	log.Printf("biu: 分页列表画崩了：%v\n%s", rec, debug.Stack())
	p.invalidate()
}

// view 是真的画法，View 是它外面那道 recover。
func (p *PagedList) view(c *ui.Context) {
	// 🔴 铁律三：把这一帧的窗口登记进去。后台投递完结果要靠它叫醒界面，
	// 不唤醒就是「结果到了、画面停在旧帧」（实测 0 帧刷新）。
	p.setWake(c.Services())

	// 先收后台投回来的页，再画：这样这一帧画的就是**已经加上**的那几行，
	// 不用等下一帧。
	p.adopt()

	// ① 首次渲染检查。**必须在画之前发出去**，不是画完再发：
	//   - 画完再发的话，这一帧画出来的是「这里还没有歌」，要等这一页回来才
	//     换掉——用户第一眼看到的是空列表（这条有测试钉住，见 paged_test.go 的
	//     TestTheVeryFirstFrameAlreadySaysLoading）。
	//   - 而且那一段没有任何东西会叫醒界面：唤醒只有「结果到了」那一次。
	if p.page == 0 {
		p.load(trigFirstRender)
	}

	p.syncList()
	p.list.View(c)

	// ②③ 在这里跑（必须排在 List.View 之后：Visible 报的是**上一帧**的行，
	// mygo 的 ListState.Visible 注释：「the row the list showed in the last frame」）。
	p.afterFrame()
	p.viewBackToTop(c)
}

// syncList 把这一帧该给 List 的东西给过去。只在真变了的时候才 SetSongs。
func (p *PagedList) syncList() {
	if p.rev != p.shownRev {
		p.list.SetSongs(p.items)
		p.shownRev = p.rev
	}

	switch {
	case len(p.items) == 0 && p.loading:
		// 一行都还没有：第一页在途。这时候列表里那一句就是它全部的内容，
		// 页脚先收起来——同一句摆两遍（空状态一句 + 页脚一句）是噪音。
		p.list.SetEmpty("正在加载…")
		p.list.SetFooter(nil)
	case len(p.items) == 0 && p.blocked:
		// 一行都没有还失败了：页面上就留那一个重试按钮，一句话都不多说。
		p.list.SetEmpty("")
		p.list.SetFooter(p.footerView)
	default:
		p.list.SetEmpty("")
		// 末尾那一行**只在还有下一页（或刚失败要人重试）时才有**（Biu 的
		// `count = hasMore ? items.length + 1 : items.length`）：拉到底之后它
		// 就不该再占一行空白。
		if p.hasMore || p.blocked {
			p.list.SetFooter(p.footerView)
		} else {
			p.list.SetFooter(nil)
		}
	}

	// 滚动位置要写回来：回顶按钮靠它，三重保障里「在不在顶上」也靠它。
	p.list.SetScroll(&p.scroll)
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
// 超时、返回格式不对、拉页器自己崩了（fetchOne 把 panic 变成了错），全走这一
// 条。漏了它就是「界面永远转圈」——验收里点名要钉住的那一条。
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
		log.Printf("biu: 第 %d 页拉失败（%s）：%v", r.page, p.why, r.err)
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
	// 🔴 铁律四：追加是**尾部**插 key（筛选后重建才是头部插，两条规则不一样），
	// 尾部追加不动已有行的位置，所以「选中的那一首」翻完页还是它。
	//
	// 重了的在这里去掉：翻页重叠是常事（同一首歌挪了名次就会出现在两页里），
	// 两个一样的 ID 就是两行共用一份状态——选中一个另一个也亮。
	for _, m := range r.pg.Songs {
		if p.seen[m.ID] {
			continue
		}
		p.seen[m.ID] = true
		p.items = append(p.items, m)
	}
	p.rev++
}

// trigger 是「这次翻页是被哪一条叫起来的」。三条保障走的是同一段代码，记下来
// 只是为了**出错时说得清是哪一条**（见 apply 的那行日志）。
type trigger uint8

const (
	// trigFirstRender 是①首次渲染检查：这一页刚建起来，先要有东西看。
	trigFirstRender trigger = iota
	// trigScrollEnd 是②滚到底：末尾那一行进画面了，而且用户是滚过去的。
	trigScrollEnd
	// trigRefill 是③加载结束后再检一次（顺带也是拖窗口变高变矮之后的那次）。
	trigRefill
	// trigRetry 是用户点了页脚那行的「重试」。它不是保障，是人。
	trigRetry
)

func (t trigger) String() string {
	switch t {
	case trigFirstRender:
		return "首次渲染检查叫的"
	case trigScrollEnd:
		return "滚到底叫的"
	case trigRetry:
		return "用户点了重试"
	default:
		return "容器没填满叫的"
	}
}

// afterFrame 是三重保障的②③（①在 view 开头，见那里）。每一帧跑一次——三条保障
// 不是三个不同的钩子，是同一个判断（末尾那一行进没进画面）在不同时刻的三个取
// 值，所以这里每帧问一遍：「一页刚落地那一帧」和「刚拖过窗口大小那一帧」自然
// 就问到了。
//
// ⚠️ 必须排在 List.View 之后：Visible 报的是**上一帧画出来**的行。
func (p *PagedList) afterFrame() {
	// 防重入闸门：正在拉、上一页失败了、或者已经到底，都不再拉。
	// （还一页都没有的那一条不在这里，见 view。）
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
		pg, err := p.fetchOne(ctx, next)
		p.deliver(pageResult{gen: gen, page: next, pg: pg, err: err})
	}()
}

// errFetcherPanic 是拉页器自己崩了之后变成的那个错。
var errFetcherPanic = errors.New("拉页的代码崩了")

// fetchOne 调 fetch，**崩了也变成一个错**。
//
// 🔴 不包这一层就是进程当场没了：fetch 跑在后台 goroutine 里，界面那一层的
// recover（行级的、页级的、这个 view 自己的）一个都接不到它——Go 里没人在
// 跑那个 goroutine。而 Fetcher 正是「解析接口返回」的那一段，接口一变就在
// 这里炸（Biu 的三个 effect 里没有这一层，它是同步的所以碰不到）。
//
// 变成错之后就走 apply 那条正常路：解锁、记一笔、页脚上出现重试——
// 正好是「返回格式不对」那一条要的行为。
func (p *PagedList) fetchOne(ctx context.Context, page int) (pg Page, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("biu: 第 %d 页的拉页器崩了：%v\n%s", page, rec, debug.Stack())
			pg, err = Page{}, fmt.Errorf("%w：%v", errFetcherPanic, rec)
		}
	}()
	return p.fetch(ctx, page)
}

// deliver 把一页结果投进信箱，然后叫醒界面。任何 goroutine 都能调。
//
// 🔴 铁律三：投递完**必须**唤醒。不唤醒就是「结果到了、画面停在旧帧」
// （实测 0 帧刷新）；只唤醒一个窗口就是另一个窗口停在旧帧，所以是逐窗口
// 通知。
func (p *PagedList) deliver(r pageResult) {
	p.mu.Lock()
	p.box = append(p.box, r)
	p.mu.Unlock()

	p.invalidate()
}

// setWake 登记这一帧的窗口（每个窗口一条，见 wakes）。只能由界面线程调。
func (p *PagedList) setWake(svc ui.Services) {
	wake := svc.Invalidate // 方法值：一个窗口一条，所以每帧一个闭包不算多

	p.wakeMu.Lock()
	defer p.wakeMu.Unlock()
	p.frame++
	for i := range p.wakes {
		if p.wakes[i].svc == svc {
			p.wakes[i].wake, p.wakes[i].seen = wake, p.frame
			return
		}
	}
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
					p.load(trigRetry)
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
