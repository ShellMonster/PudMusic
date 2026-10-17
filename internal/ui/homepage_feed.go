// 首页的取数：三个 Tab 各自的推荐单 + 新歌速递（`viewHome` 的数据那一半）。
//
// # 为什么单独一个文件
//
// `homepage.go` 管的是「画成什么样」，这里管的是「什么时候去要、要到了怎么
// 落地」。分开的理由是后者和 `dynamicfeed.go` / `upspace.go` 的闸门是同一种
// 东西——后台一条线、信箱一把锁、界面线程在画之前收一次。放进 `homepage.go`
// 会把那个文件撑成两个主题，而这一份的规矩（不许重入、失败等人点重试、
// 唤醒）要独立讲清楚。
//
// # 四摊，各自独立
//
// 三个 Tab 各一摊，新歌速递又一摊。**不合并成一次请求**：三个 Tab 用的本来
// 就是两套接口（见 `service/bili/recommend.go` 的文件头），而用户未必会看
// 另外两档——启动就把三摊都拉一遍，等于用两个用不上的请求换一个「切 Tab
// 不用等」。**第一次切到哪一档才拉哪一档**，拉过就一直留在那儿（切 Tab 不
// 重来，上游是每切一次重拉一次，见下面「为什么不跟着上游那样每次都重拉」）。
//
// # 为什么不跟着上游那样每次切 Tab 都重拉
//
// 上游 `music-recommend/index.tsx:133-137` 的 `useEffect([activeTab, init])`
// 每切一次 Tab 就重新拉第一页。它能这么干是因为它整个列表是空的开始（翻页
// 状态跟着它那个 `listKey` 一起重建），而我们**一个 Tab 一个 `list.List`**、
// 滚动位置跟着那一个对象走（见 `homepage.go`「切 Tab 的滚动位置」那一段）。
// 每切一次重拉会把用户盯着的第 17 首换成另一批的第 17 首——用户盯着的那一首
// 变了，而那个变化他解释不了。
//
// # 🔴 失败之后只有人能让它再来一次
//
// `viewHome` 每一帧都会跑一遍 `pull`，而它发不发只看一个数：`started`
// （「拉过没有」，**失败也算**）。失败的那一趟不会把它落回 false，所以那一档
// 不会再自动发，唯一的出口是界面上那个「重试」（`retryTab` / `retryNew`
// 才落它）。少了这道门就是一个每帧发一次请求的死循环（`upspace_test.go` 里
// 那个 300 毫秒 138 次的例子就是同一件事）。
//
// # 🔴 换账号时整份作废
//
// 分区推荐是**按账号**的（B 站拿 Cookie 里的 mid 算口味）。带着 A 账号的
// 推荐给 B 账号看，界面上没有任何东西提示这一点，所以登录态一变就把四摊
// 全清掉重来（和 `dynamicfeed.go` 的 `resetIfAccountChanged` 同一个理由）。
package ui

import (
	"context"
	"errors"
	"log"
	"sync"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
)

// homeNewMusic 是新歌速递那一摊的下标，和三个 Tab 挤在同一个信箱里。
//
// ⚠️ **它不是第四个 Tab**：`homeTabs` 还是三个（`homepage.go`），这一格只在
// 「流行」那一档的顶部画（上游 `index.tsx:295`），而 `homeTabs` 的长度被
// `homeTabCount` 拿去定数组了——往里加第四个会让三个 Tab 的滚动位置测试
// 全错，而那一块根本不是 Tab。
const homeNewMusic = homeTabCount

// homeFeed 装这一页的四摊取数状态。
//
// ⚠️ 里面的 `tabs` / `newTab` **只由界面线程读写**（铁律一），跨 goroutine
// 的只有下面那一个带锁的信箱。
type homeFeed struct {
	tabs    [homeTabCount]homeTab
	newTab  homeTab
	forWho  int64 // 手上这一份是为哪个账号拉的（0 = 没登录）
	haveWho bool

	// ── 跨 goroutine 的只有下面这一小摊 ──
	boxMu sync.Mutex
	box   []homeResult
}

// homeTab 是一摊的加载状态。
type homeTab struct {
	// started 是「拉过没有」（**失败也算拉过**）。自动拉取的条件就是它，
	// 见文件头「失败之后只有人能让它再来一次」。
	started bool
	// loading 是「正在拉」。防重入的第一道：连点三下重试不会发三个请求。
	loading bool
	// err 是上一次失败的原因（**已经翻成人话**，见 `homeFailureOf`），
	// 成功时是 nil。界面拿它画那一块。
	err error
	// dropped 是 B 站给了、但我们画不出来的条数（没有身份、或者和前面那条
	// 重了）。**不画在界面上**：用户看见的是一屏正常的歌，而一个「少了三首」
	// 的说明只会让他以为漏了东西。它只进日志（见 adopt）。
	dropped int
}

// homeResult 是后台拉完之后投进信箱的东西。
type homeResult struct {
	// which 是哪一摊：0..2 是三个 Tab，homeNewMusic 是新歌速递。
	which   int
	songs   []list.Music
	dropped int
	err     error
	// selfMid 是发这一次时的账号。换账号之后回来的那一摊**作废**——
	// 停请求停不掉已经在回来的那一趟（和 `dynamicfeed.go` 同一句话）。
	selfMid int64
}

// homeLoads 是这一页要的那几个「拉」的活儿。
//
// 传函数进来而不是把 `*Shell` 存进 `homePage`：那一层只该知道自己要什么，
// 而「B 站客户端挂上没有」「这次要拉哪一档」是接线的事。
type homeLoads struct {
	// mounted 报 B 站客户端挂上没有。没挂上时页面**不发请求**，照它自己的
	// 空状态画（见 viewHome 里那段）。
	mounted func() bool
	// tab 拉第 tab 个 Tab 的一页推荐。
	tab func(ctx context.Context, tab int) ([]app.RecommendItem, int, error)
	// new 拉新歌速递那一摊。
	new func(ctx context.Context) ([]app.RecommendItem, int, error)
	// selfMid 是当前登录账号（0 = 没登录），用来判断手上这一份该不该作废。
	selfMid func() int64
	// wake 是「要一帧新画面」。后台投递完不唤醒 = 界面停在旧帧（铁律三）。
	wake func()
}

// initLoads 装上那几个活儿。**建一次**，见 newHomePage 与 Shell.New。
func (h *homePage) initLoads(l homeLoads) { h.loads = l }

// ─────────────────────────────────────────────────────────────────────────────
// 接上去的那两个「去拉」
// ─────────────────────────────────────────────────────────────────────────────

// fetchHomeTab 拉第 tab 个 Tab 的一页推荐。**跑在后台 goroutine 里**。
//
// ⚠️ 它只认传进来的 `tab` 和 `ctx`，**不许回去读界面上任何东西**：那一刻
// 用户可能已经切到别的 Tab 了，而这一趟发的是切走之前那一档（回来之后按
// 下标落到那一档，各 Tab 各自一份，互不干扰）。
func (s *Shell) fetchHomeTab(ctx context.Context, tab int) ([]app.RecommendItem, int, error) {
	return s.app.RecommendOf().Feed(ctx, tab, homeFirstPage)
}

// fetchHomeNewMusic 拉新歌速递那一摊。**跑在后台 goroutine 里**。
func (s *Shell) fetchHomeNewMusic(ctx context.Context) ([]app.RecommendItem, int, error) {
	return s.app.RecommendOf().NewMusic(ctx)
}

// homeFirstPage 是首页拉的那一页（从 1 起）。
//
// ⚠️ **只拉第一页**：上游是滚到底再拉下一段（`music-recommend/index.tsx:154-162`
// 的 `loadMore`），而那要的是一个带三条翻页保障的分页列表（`list.PagedList`）。
// 首页这一屏现在用的是三个各记各的滚动位置的 `list.List`（见 homepage.go
// 「切 Tab 的滚动位置」），把它们换成翻页列表就是换掉那三个对象——而
// `TestSwitchingBackRestoresThatTabsScrollPosition` 钉的就是「切回某个 Tab
// 还在它原来的位置」。等要做「往下拉还有」时再说，那时这两件事要在同一次
// 改动里解决。
const homeFirstPage = 1

// ─────────────────────────────────────────────────────────────────────────────
// 收：后台的结果落地
// ─────────────────────────────────────────────────────────────────────────────

// adopt 收走信箱里的结果。**必须在画之前调**（铁律三那一半）。
func (h *homePage) adopt() {
	h.feed.boxMu.Lock()
	box := h.feed.box
	h.feed.box = nil
	h.feed.boxMu.Unlock()

	for _, r := range box {
		// ⚠️ 迟到的结果直接丢：这一趟是为另一个账号发的（`pull` 里记的）。
		// 留着它就是「A 账号的推荐出现在 B 账号的屏幕上」。
		if r.selfMid != selfMidOf(h.loads) {
			continue
		}
		st := h.feed.stateOf(r.which)
		st.loading = false
		st.err = nil
		if r.err != nil {
			st.err = homeFailureOf(r.err)
			continue
		}
		st.dropped = r.dropped
		if r.dropped > 0 {
			// 不画在界面上（用户看见的是一屏正常的歌），但要留下一条：用户说
			//「怎么少了几首」的时候，日志里有这一行，而界面上一个字都没有。
			log.Printf("biu: 首页第 %d 摊有 %d 条画不出来（没有身份，或者和前面那条重了）",
				r.which, r.dropped)
		}
		h.adoptSongs(r.which, r.songs)
	}
}

// homeFailureOf 把一次失败的错翻成**一句能直接画给人看的话**。
//
// ⚠️ 翻在**落地的那一刻**，不在每一帧画的时候：B 站的码到「该怎么办」的
// 映射是服务层的领域知识（`bili.Friendly`），而「这一帧在失败」这件事会
// 一直持续几百帧——每一帧重新翻一遍就是每一帧重新判一次 `errors.Is`。
//
// 兜底那一句（`Friendly` 自己返回空串时）给的是「等一会儿再试」：它唯一
// 返回空串的情形是「用户自己停掉的」（`context.Canceled`），而首页这几趟
// 没有人停得掉——真出现了也不能画一个空白。
func homeFailureOf(err error) error {
	if err == nil {
		return nil
	}
	msg := bili.Friendly(err)
	if msg == "" {
		msg = "拉不到这一块的推荐，等一会儿再试"
	}
	return errors.New(msg)
}

// adoptSongs 把一摊换掉。**换歌不动滚动位置**，理由同 `homePage.SetSongs`。
func (h *homePage) adoptSongs(which int, songs []list.Music) {
	if which == homeNewMusic {
		h.songs.new = songs
		h.newGrid.SetSongs(songs)
		return
	}
	h.songs.tabs[which] = songs
	h.lists[which].SetSongs(songs)
}

// ─────────────────────────────────────────────────────────────────────────────
// 发：该拉的那几摊拉起来
// ─────────────────────────────────────────────────────────────────────────────

// pull 把「此刻该拉的那几摊」拉起来。**每一帧都跑一遍**（见文件头那些规矩）。
//
// 只拉正在看的那一档；「流行」那一档顺带拉新歌速递（它只画在那里）。
func (h *homePage) pull() {
	if h.loads.mounted == nil || !h.loads.mounted() {
		// 没有 B 站客户端。**不发那个注定失败的请求**，页面照它自己的空
		// 状态画——那一句话说的正是「怎么才能不空」，而真机上这一支不该
		// 走到（`main.go` 在开窗口之前就把客户端挂上了）。
		return
	}
	// ⚠️ 换账号了：四摊全部作废重来（下面那个函数顺手清了歌）。
	h.resetForAccount()
	h.pullTab(h.tab)
	if h.tab == homePopTab {
		h.pullNew()
	}
}

// resetForAccount 在**账号变了**时把整份作废。
//
// 判据是 mid 而不是「登录 / 没登录」：同一个人重新登录一次（换了 SESSDATA
// 但 mid 没变）推荐不该整个重来，而换成另一个账号就该重来。
//
// ⚠️ **第一次只是记下来，不清任何东西**：那一刻一份都还没拉过，而清歌会
// 把外面塞进来的那一批（测试里 `SetSongs` 塞的）也清掉——那是「作废一个
// 从来没有过的东西」。
func (h *homePage) resetForAccount() {
	mid := selfMidOf(h.loads)
	if !h.feed.haveWho {
		h.feed.forWho, h.feed.haveWho = mid, true
		return
	}
	if h.feed.forWho == mid {
		return
	}
	h.feed.forWho = mid
	h.feed.tabs = [homeTabCount]homeTab{}
	h.feed.newTab = homeTab{}
	h.songs = homeSongs{}
	for _, l := range h.lists {
		l.SetSongs(nil)
	}
	h.newGrid.SetSongs(nil)
}

// pullTab 拉第 tab 个 Tab（还没拉过才拉）。
//
// 「重试」也走这里：`retry` 把 started 落回 false，这一帧再调一次 pull 就
// 发出去了——**只有这一条路能重发**，见文件头「失败之后只有人能让它再来一次」。
func (h *homePage) pullTab(tab int) {
	st := &h.feed.tabs[tab]
	if st.started || st.loading || h.loads.tab == nil {
		return
	}
	st.started, st.loading = true, true
	st.err = nil
	mid := selfMidOf(h.loads)
	loads := h.loads
	h.goFetch(tab, func(ctx context.Context) ([]app.RecommendItem, int, error) {
		return loads.tab(ctx, tab)
	}, mid)
}

// retryTab 让第 tab 个 Tab 再拉一次。**人点了「重试」才走这里**。
func (h *homePage) retryTab(tab int) {
	st := &h.feed.tabs[tab]
	st.started, st.err = false, nil
	h.pullTab(tab)
}

// pullNew 拉新歌速递（还没拉过才拉）。
func (h *homePage) pullNew() {
	st := &h.feed.newTab
	if st.started || st.loading || h.loads.new == nil {
		return
	}
	st.started, st.loading = true, true
	st.err = nil
	loads := h.loads
	h.goFetch(homeNewMusic, loads.new, selfMidOf(loads))
}

// retryNew 让新歌速递再拉一次。**人点了「重试」才走这里**。
func (h *homePage) retryNew() {
	st := &h.feed.newTab
	st.started, st.err = false, nil
	h.pullNew()
}

// goFetch 发一次拉取，结果投进信箱，然后叫醒界面。
//
// ⚠️ 界面线程**不碰** `st`（`loading` 那些字段）：它正在这一帧的画里被读，
// 而后台那条线还在跑——两边都写就是一处数据竞争（`-race` 一定会报）。
// 后台只碰信箱那把锁。
func (h *homePage) goFetch(which int, fetch homeFetch, selfMid int64) {
	wake := h.loads.wake
	go func() {
		items, dropped, err := fetch(context.Background())
		res := homeResult{which: which, dropped: dropped, err: err, selfMid: selfMid}
		if err == nil {
			res.songs = homeMusicOf(items)
		}
		h.feed.boxMu.Lock()
		h.feed.box = append(h.feed.box, res)
		h.feed.boxMu.Unlock()
		// 🔴 铁律三：**投递完必须唤醒**。只投递不唤醒的话界面停在旧帧，
		// 而唤醒只有「结果到了」这一次——不唤醒就是一个永远转圈的页面。
		if wake != nil {
			wake()
		}
	}()
}

// homeFetch 是「拉某一摊」的活儿。跑在后台 goroutine 里。
type homeFetch func(ctx context.Context) ([]app.RecommendItem, int, error)

// homeMusicOf 把这一层的一行翻成列表那一行。
//
// ⚠️ **ID 原样带过去**（`app.RecommendItem.ID` 算好的）：铁律四要的是
// 「稳定且唯一」，而身份只有那一处算法（见 `app/recommend.go` 的
// `recommendID`）。这一层要是自己再拼一个，两处就会分叉。
func homeMusicOf(items []app.RecommendItem) []list.Music {
	out := make([]list.Music, 0, len(items))
	for _, it := range items {
		out = append(out, list.Music{
			ID: it.ID, Title: it.Title, Cover: it.Cover, Owner: it.Owner,
			Duration: it.Duration, Item: it.Item,
		})
	}
	return out
}

// selfMidOf 问当前登录账号（没登录是 0）。hooks 没装时也是 0。
func selfMidOf(l homeLoads) int64 {
	if l.selfMid == nil {
		return 0
	}
	return l.selfMid()
}

// stateOf 是某一摊此刻的状态。
func (f *homeFeed) stateOf(which int) *homeTab {
	if which == homeNewMusic {
		return &f.newTab
	}
	return &f.tabs[which]
}

// loadingOf 是那一摊此刻有没有在拉。view 每帧读它。
func (f *homeFeed) loadingOf(which int) bool { return f.stateOf(which).loading }
