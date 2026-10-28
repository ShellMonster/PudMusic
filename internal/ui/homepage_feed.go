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
// # 三个 Tab 是三个翻页列表，三摊互不合并
//
// **每一档一个 `list.PagedList`**，它自己管翻页（拉页、防重入、取消、旧结果
// 作废、容器没填满自动续拉）——这一层**不另写一套**（票 #370 的验收第 5 条）。
//
// ⚠️ **三摊互不合并**：三个 Tab 用的本来就是两套接口（见
// `service/bili/recommend.go` 的文件头），而用户未必会看另外两档——启动就把三摊
// 都拉一遍，等于用两个用不上的请求换一个「切 Tab 不用等」。**第一次切到哪一档
// 才拉哪一档**，拉过就一直留在那儿（切 Tab 不重来，上游是每切一次重拉一次，见
// 下面「为什么不跟着上游那样每次都重拉」）。
//
// # 为什么不跟着上游那样每次切 Tab 都重拉
//
// 上游 `music-recommend/index.tsx:133-137` 的 `useEffect([activeTab, init])`
// 每切一次 Tab 就重新拉第一页。它能这么干是因为它整个列表是空的开始（翻页
// 状态跟着它那个 `listKey` 一起重建），而我们**一个 Tab 一个翻页列表**、
// 滚动位置跟着那一个对象走（见 `homepage.go`「切 Tab 的滚动位置」那一段）。
// 每切一次重拉会把用户盯着的第 17 首换成另一批的第 17 首——用户盯着的那一首
// 变了，而那个变化他解释不了。
//
// # 🔴 失败之后只有人能让它再来一次
//
// 翻页列表自己闩着这件事（`list.PagedList` 的 `blocked`）：失败之后自动补屏
// 就此停手，唯一的出口是界面上那个「重试」（末尾那一行，或者首页这一屏
// 那一整块失败态里的「重试加载××推荐」）。少了这道门就是一个每帧发一次请求
// 的死循环（`upspace_test.go` 里那个 300 毫秒 138 次的例子就是同一件事）。
//
// # 🔴 换账号时整份作废
//
// 分区推荐是**按账号**的（B 站拿 Cookie 里的 mid 算口味）。带着 A 账号的
// 推荐给 B 账号看，界面上没有任何东西提示这一点，所以登录态一变就把三档
// 全清掉重来（和 `dynamicfeed.go` 的 `resetIfAccountChanged` 同一个理由）。
package ui

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
)

// homeFeed 装新歌速递那一摊的取数状态。
//
// ⚠️ **三个 Tab 不在这一摊里**：它们各有自己的 `list.PagedList`，翻页状态在
// 那一层里（见文件头「三个 Tab 是三个翻页列表」）。
//
// ⚠️ 里面的 `newTab` **只由界面线程读写**（铁律一），跨 goroutine 的只有下面
// 那一个带锁的信箱。
type homeFeed struct {
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
	// songs 是这一摊拉回来的歌。
	songs []list.Music
	// dropped 是画不出来的条数。
	dropped int
	// err 是这一趟的错。
	err error
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
	// tab 拉第 tab 个 Tab 的**第 page 页**推荐（页码从 1 起）。
	tab func(ctx context.Context, tab, page int) ([]app.RecommendItem, int, error)
	// new 拉新歌速递那一摊。
	new func(ctx context.Context) ([]app.RecommendItem, int, error)
	// board 是热榜 / 原创榜最近拉到的那一期（期数、发布时刻）；没拉到时 ok 为 false。
	board func(tab int) (number int, at time.Time, ok bool)
	// selfMid 是当前登录账号（0 = 没登录），用来判断手上这一份该不该作废。
	selfMid func() int64
	// wake 是「要一帧新画面」。后台投递完不唤醒 = 界面停在旧帧（铁律三）。
	wake func()
}

// initLoads 装上那几个活儿。**建一次**，见 newHomePage 与 Shell.New。
func (h *homePage) initLoads(l homeLoads) { h.loads = l }

// ─────────────────────────────────────────────────────────────────────────────
// 接上去的那几个「去拉」
// ─────────────────────────────────────────────────────────────────────────────

// fetchHomeTab 拉第 tab 个 Tab 的**第 page 页**推荐。**跑在后台 goroutine 里**。
//
// ⚠️ 它只认传进来的 `tab` 和 `page`，**不许回去读界面上任何东西**：那一刻
// 用户可能已经切到别的 Tab 了，而这一趟发的是切走之前那一档（回来之后按
// 下标落到那一档，各 Tab 各自一份，互不干扰）。
func (s *Shell) fetchHomeTab(ctx context.Context, tab, page int) ([]app.RecommendItem, int, error) {
	return s.app.RecommendOf().Feed(ctx, tab, page)
}

// fetchHomeNewMusic 拉新歌速递那一摊。**跑在后台 goroutine 里**。
func (s *Shell) fetchHomeNewMusic(ctx context.Context) ([]app.RecommendItem, int, error) {
	return s.app.RecommendOf().NewMusic(ctx)
}

// tabFetcher 是第 tab 个 Tab 那一档的拉页器。**建一次**，见 newHomePage。
//
// ⚠️ **页码从 1 起**（`list.PagedList` 那一层就是这么问的），而 B 站的两个接口
// 收的都是页码：分区推荐收在 `display_id` 上，音乐榜收在 `pn` 上（见
// `service/bili/recommend.go` 的文件头）。
func (h *homePage) tabFetcher(tab int) list.Fetcher {
	return func(ctx context.Context, page int) (list.Page, error) {
		if h.loads.tab == nil {
			return list.Page{}, errors.New("首页还没接上推荐接口")
		}
		if h.loads.mounted == nil || !h.loads.mounted() {
			// 没有 B 站客户端：给一个**空的、而且「没有更多」**的页。
			//
			// ⚠️ **不是错**：错会走到页面上那一整块失败态（一句「拉不到」+ 一个
			// 重试按钮），而这一屏压根不是拉失败了——是没有 B 站可拉（真机上这一
			// 支不该走到，`main.go` 在开窗口之前就把客户端挂上了）。
			//
			// 而这一档画出来的是**这一页的空状态**（`routes` 表里 `/` 那一条），
			// 那句话说的正是「怎么才能不空」。
			return list.Page{HasMore: false}, nil
		}
		// 最近播放是登录账号的观看历史：没登录就**不发请求**（画面上是「去登录」那
		// 一支，见 `viewTabBody`）。
		if tab == homeRecentTab && selfMidOf(h.loads) == 0 {
			return list.Page{}, nil
		}
		items, dropped, err := h.loads.tab(ctx, tab, page)
		if err != nil {
			// ⚠️ **翻成人话在落地的那一刻**，不在每一帧画的时候（理由见
			// `homeFailureOf`）：翻完的错是给界面看的，原始的错进日志。
			log.Printf("biu: 首页第 %d 档第 %d 页拉失败：%v", tab, page, err)
			return list.Page{}, homeFailureOf(err)
		}
		if dropped > 0 {
			// 不画在界面上（用户看见的是一屏正常的歌），但要留下一条：用户说
			// 「怎么少了几首」的时候，日志里有这一行，而界面上一个字都没有。
			log.Printf("biu: 首页第 %d 档第 %d 页有 %d 条画不出来（没有身份，或者和前面那条重了）",
				tab, page, dropped)
		}
		// ⚠️ **「还有没有」不由这一层说**：B 站这两个接口都不给「还有没有」这一
		// 格，而「一页空的」就是到底了（`list.PagedList` 那一层会照这一句把哨兵
		// 收掉，见它的 `apply`）。所以这里一律说「还有」，让翻页那一层去问下一
		// 页——问回来是空的，它自己就知道到底了。
		return list.Page{Songs: homeMusicOf(items), HasMore: true}, nil
	}
}

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
		st := &h.feed.newTab
		st.loading = false
		st.err = nil
		if r.err != nil {
			st.err = homeFailureOf(r.err)
			continue
		}
		st.dropped = r.dropped
		if r.dropped > 0 {
			// 不画在界面上（用户看见的是一屏正常的歌），但要留下一条：用户说
			// 「怎么少了几首」的时候，日志里有这一行，而界面上一个字都没有。
			log.Printf("biu: 新歌速递有 %d 条画不出来（没有身份，或者和前面那条重了）",
				r.dropped)
		}
		h.newGrid.SetSongs(r.songs)
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

// ─────────────────────────────────────────────────────────────────────────────
// 发：该拉的那几摊拉起来
// ─────────────────────────────────────────────────────────────────────────────

// pull 把「此刻该拉的那几摊」拉起来。**每一帧都跑一遍**（见文件头那些规矩）。
//
// ⚠️ **三个 Tab 不在这里拉**：它们由各自的翻页列表拉（`list.PagedList` 的
// `Sync` 在这一份被画到时起第一拉，滚到底再起下一拉）。这里只管新歌速递
// 那一摊——它只画在「流行」上，而它不是翻页列表（它自己横向翻页）。
func (h *homePage) pull() {
	if h.loads.mounted == nil || !h.loads.mounted() {
		// 没有 B 站客户端。**不发那个注定失败的请求**，页面照它自己的空
		// 状态画——那一句话说的正是「怎么才能不空」，而真机上这一支不该
		// 走到（`main.go` 在开窗口之前就把客户端挂上了）。
		return
	}
	// ⚠️ 换账号了：三档全部作废重来（下面那个函数顺手清了歌）。
	h.resetForAccount()
	if h.tab == homePopTab || h.overview {
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
	h.feed.newTab = homeTab{}
	// ⚠️ **三档用 Reset 而不是 Reload**：Reload 是「清空 + 当场拉第一页」，
	// 而这里只有**正在看的那一档**该发请求——另外两档等用户切过去再拉
	// （那一档的翻页列表被画到时会替它拉第一页）。
	for _, l := range h.lists {
		l.Reset()
	}
	h.newGrid.SetSongs(nil)
}

// retryTab 让第 tab 个 Tab 再拉一次。**人点了「重试」才走这里**。
//
// ⚠️ **走 Reload**：它把那一档清空、回顶、然后从第一页重来（票 #370 的
// 验收第 1 条——失败之后要能从头再来，而不是接着失败的那一页）。
func (h *homePage) retryTab(tab int) {
	h.lists[tab].Reload()
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
	h.goFetch(loads.new, selfMidOf(loads))
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
func (h *homePage) goFetch(fetch homeFetch, selfMid int64) {
	wake := h.loads.wake
	go func() {
		items, dropped, err := fetch(context.Background())
		res := homeResult{dropped: dropped, err: err, selfMid: selfMid}
		if err == nil {
			res.songs = homeMusicOf(items)
		}
		h.feed.boxMu.Lock()
		h.feed.box = append(h.feed.box, res)
		h.feed.boxMu.Unlock()
		// 🔴 铁律三：**投递完必须唤醒**。只投递不唤醒的话界面停在旧帧，
		// 而唤醒只有「结果到了」那一次——不唤醒就是一个永远转圈的页面。
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
			// 播放量：网格卡片封面底部那一条渐变上画的是它（`Music.View`）。
			View:    it.View,
			PubTime: it.ViewAt, Progress: it.Progress,
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
