// 首页的取数：每日推荐、本周最热、新歌速递、最近播放四摊 + 登录昵称。
//
// # 为什么单独一个文件
//
// `homepage.go` 管「画成什么样」，这里管「什么时候去要、要到了怎么落地」。规矩和
// `dynamicfeed.go` / `upspace.go` 的闸门一样：后台一条线、信箱一把锁、界面线程在画之前
// 收一次。
//
// # 四摊各取第一页，不翻页
//
// 首页是一屏「精选」（原型：每日推荐 6 张、榜单前 20、新歌速递、最近播放 10 张），不是
// 无限列表，所以每摊只拉第一页；要看更多去各自的页面。
//
//	每日推荐  音乐分区推荐的第一页（`app.HomeTabMusic`），取前 6 首
//	本周最热  B 站音乐站热榜最新一期（`app.HomeTabHot`），取前 20 首；
//	          「今日一张」也从这一摊里按日期选
//	新歌速递  banner + 列表合成的一摊（`Recommend.NewMusic`）
//	最近播放  登录账号的观看历史（`app.HomeTabRecent`）；没登录不发请求
//
// 每摊的 `started` 管「拉过没有」（失败也算拉过）：失败之后只有人点「重试」才会再拉，
// 否则就是每帧一个请求的死循环。
//
// # 🔴 换账号时整份作废
//
// 分区推荐、观看历史都是按账号的。登录态（mid）一变就把四摊全清掉重来，迟到的结果按
// 发出时的 mid 丢掉（和 `dynamicfeed.go` 同一个理由）。
package ui

import (
	"context"
	"errors"
	"log"
	"sync"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
)

// homeKind 是首页取数的四摊。
type homeKind int

const (
	homeDaily  homeKind = iota // 每日推荐
	homeChart                  // 本周最热
	homeNew                    // 新歌速递
	homeRecent                 // 最近播放
	homeKindCount
)

// homeSection 是一摊的加载状态和内容。**只由界面线程读写**。
type homeSection struct {
	// started 是「拉过没有」（失败也算拉过）。
	started bool
	// loading 是「正在拉」。
	loading bool
	// err 是上一次失败的原因（已经翻成人话），成功时是 nil。
	err error
	// songs 是拉回来的歌。
	songs []list.Music
}

// homeResult 是后台拉完之后投进信箱的东西。
type homeResult struct {
	kind  homeKind
	songs []list.Music
	err   error
	// selfMid 是发这一趟时的账号；换账号之后回来的作废。
	selfMid int64
	// name 只有昵称那一趟填。
	name    string
	isName  bool
	dropped int
}

// homeLoads 是首页要的那几个「拉」的活儿。传函数进来而不是存 `*Shell`：这一层只该知道
// 自己要什么，「B 站挂上没有」「拉哪一摊」是接线的事。
type homeLoads struct {
	// mounted 报 B 站客户端挂上没有；没挂上时页面不发请求。
	mounted func() bool
	// feed 拉第 tab 个推荐档的第 page 页。
	feed func(ctx context.Context, tab, page int) ([]app.RecommendItem, int, error)
	// newMusic 拉新歌速递。
	newMusic func(ctx context.Context) ([]app.RecommendItem, int, error)
	// trend 报某首歌相对上一期热榜的升降（正 = 上升）；ok 为 false 是没有上一期的记录。
	trend func(id string) (delta int, ok bool)
	// nickname 问登录账号的昵称（空串 = 没问到）。
	nickname func(ctx context.Context) string
	// selfMid 是当前登录账号（0 = 没登录）。
	selfMid func() int64
	// wake 是「要一帧新画面」。后台投递完不唤醒 = 界面停在旧帧（铁律三）。
	wake func()
}

// homeFeed 是取数那一摊的状态：四摊 + 信箱 + 昵称。
type homeFeed struct {
	sec [homeKindCount]homeSection

	forWho  int64 // 手上这一份是为哪个账号拉的（0 = 没登录）
	haveWho bool

	// name 是登录昵称；nameAsked 是「问过没有」（问一次，换账号清掉）。
	name      string
	nameAsked bool

	boxMu sync.Mutex
	box   []homeResult
}

// initLoads 装上那几个活儿。建一次，见 `Shell.New`。
func (h *homePage) initLoads(l homeLoads) { h.loads = l }

// selfMidOf 问当前登录账号（没登录是 0）。hooks 没装时也是 0。
func selfMidOf(l homeLoads) int64 {
	if l.selfMid == nil {
		return 0
	}
	return l.selfMid()
}

// homeFailureOf 把一次失败翻成一句能直接画给人看的话。翻在落地的那一刻，不在每一帧画的
// 时候（`bili.Friendly` 是服务层的领域知识，而「这一帧在失败」会持续几百帧）。
func homeFailureOf(err error) error {
	if err == nil {
		return nil
	}
	msg := bili.Friendly(err)
	if msg == "" {
		msg = "拉不到这一块的内容，等一会儿再试"
	}
	return errors.New(msg)
}

// homeMusicOf 把推荐层的一行翻成列表那一行。ID 原样带过去（身份只有
// `app.recommendID` 那一处算法，铁律四）。
//
// 时间那一格：最近播放填的是观看时刻，新歌速递填的是上架日期，两样不会同时有。
func homeMusicOf(items []app.RecommendItem) []list.Music {
	out := make([]list.Music, 0, len(items))
	for _, it := range items {
		at := it.ViewAt
		if at.IsZero() {
			at = it.PublishedAt
		}
		out = append(out, list.Music{
			ID: it.ID, Title: it.Title, Cover: it.Cover, Owner: it.Owner,
			Duration: it.Duration, Item: it.Item, View: it.View,
			PubTime: at, Progress: it.Progress,
		})
	}
	return out
}

// adopt 收走信箱里的结果。必须在画之前调（铁律三那一半）。
func (h *homePage) adopt() {
	h.feed.boxMu.Lock()
	box := h.feed.box
	h.feed.box = nil
	h.feed.boxMu.Unlock()

	for _, r := range box {
		// 迟到的结果直接丢：这一趟是为另一个账号发的。
		if r.selfMid != selfMidOf(h.loads) {
			continue
		}
		if r.isName {
			h.feed.name = r.name
			continue
		}
		st := &h.feed.sec[r.kind]
		st.loading = false
		if r.err != nil {
			st.err = homeFailureOf(r.err)
			continue
		}
		st.err = nil
		st.songs = r.songs
		if r.dropped > 0 {
			// 不画在界面上（用户看见的是一屏正常的歌），但留一条日志。
			log.Printf("pudmusic: 首页第 %d 摊有 %d 条画不出来（没有身份，或者和前面那条重了）", r.kind, r.dropped)
		}
	}
}

// pull 把此刻该拉的几摊拉起来。每帧都跑一遍：已经拉过 / 正在拉的直接返回。
//
// 每日推荐和本周最热一进首页就拉（问候区的数字、「今日一张」都要它们）；新歌速递等用户
// 点到那个 Tab 才拉；最近播放登录了才拉。
func (h *homePage) pull() {
	if h.loads.mounted == nil || !h.loads.mounted() {
		return
	}
	h.resetForAccount()
	h.pullKind(homeDaily)
	h.pullKind(homeChart)
	if h.tab == homeTabNew {
		h.pullKind(homeNew)
	}
	if selfMidOf(h.loads) != 0 {
		h.pullKind(homeRecent)
		h.pullName()
	}
}

// resetForAccount 在账号变了时把整份作废。判据是 mid：同一个人重新登录（换了
// SESSDATA 但 mid 没变）不重来，换成另一个账号才重来。第一次只是记下来，不清任何东西
// （外面塞进来的那一批——测试里 `SetSongs`——也在这一刻之前）。
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
	h.feed.sec = [homeKindCount]homeSection{}
	h.feed.name, h.feed.nameAsked = "", false
}

// retry 让一摊再拉一次。人点了「重试」才走这里。
func (h *homePage) retry(k homeKind) {
	st := &h.feed.sec[k]
	st.started, st.err = false, nil
	h.pullKind(k)
}

// pullKind 拉一摊（还没拉过才拉）。
func (h *homePage) pullKind(k homeKind) {
	st := &h.feed.sec[k]
	if st.started || st.loading {
		return
	}
	loads := h.loads
	var fetch func(ctx context.Context) ([]app.RecommendItem, int, error)
	switch k {
	case homeDaily:
		fetch = func(ctx context.Context) ([]app.RecommendItem, int, error) {
			return loads.feed(ctx, app.HomeTabMusic, 1)
		}
	case homeChart:
		fetch = func(ctx context.Context) ([]app.RecommendItem, int, error) {
			return loads.feed(ctx, app.HomeTabHot, 1)
		}
	case homeRecent:
		fetch = func(ctx context.Context) ([]app.RecommendItem, int, error) {
			return loads.feed(ctx, app.HomeTabRecent, 1)
		}
	case homeNew:
		fetch = loads.newMusic
	}
	if fetch == nil || (k != homeNew && loads.feed == nil) {
		return
	}
	st.started, st.loading, st.err = true, true, nil
	mid := selfMidOf(loads)
	go func() {
		items, dropped, err := fetch(context.Background())
		res := homeResult{kind: k, err: err, selfMid: mid, dropped: dropped}
		if err != nil {
			log.Printf("pudmusic: 首页第 %d 摊拉失败：%v", k, err)
		} else {
			res.songs = homeMusicOf(items)
		}
		h.post(res)
	}()
}

// pullName 问一次登录昵称（问过就不再问，换账号清掉重问）。
func (h *homePage) pullName() {
	if h.feed.nameAsked || h.loads.nickname == nil {
		return
	}
	h.feed.nameAsked = true
	loads := h.loads
	mid := selfMidOf(loads)
	go func() {
		h.post(homeResult{isName: true, name: loads.nickname(context.Background()), selfMid: mid})
	}()
}

// post 把结果投进信箱并叫醒界面。🔴 铁律三：投递完必须唤醒。
func (h *homePage) post(r homeResult) {
	h.feed.boxMu.Lock()
	h.feed.box = append(h.feed.box, r)
	h.feed.boxMu.Unlock()
	if h.loads.wake != nil {
		h.loads.wake()
	}
}
