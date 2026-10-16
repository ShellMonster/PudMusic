package app

import (
	"path/filepath"
	"strings"
	"sync"
)

// 最小播放队列：当前项 + 上一首 + 下一首 + 尾部追加。
//
// 只有这四件事。判重、排序、持久化、播放模式（顺序 / 单曲 / 随机）、跨来源
// 都留给后面的阶段——但队列本身不能等：上一首/下一首是播放条上现在就有的两个
// 按钮，界面层已经按「队列存在」的样子写了，先把接口立住，后面接 B 站的
// 来源时不用动播放条。
//
// 队列跟着 App 走：App 一开始就是空队列（见 app.go 的 New）。

// Item 是队列里的一项。
//
// 身份按 Biu 的判重标识来（`_research/biu/src/store/play-list.ts` 的 isSame）：
// 本地项按 id（也就是路径），在线的 mv 按 bvid，audio 按 sid。阶段 1 只有
// 本地项用得上，后四个字段先留好位置——接 B 站时队列的形状不用改。
//
// 字段名沿用 Biu 的叫法，值也沿用：Source 是 "local" / "online"，
// Type 是 "local" / "mv" / "audio"。Cover 对应 Biu 那一侧的 `cover`
// （播放条上取 `playItem.pageCover || playItem.cover`）。
type Item struct {
	Source string // 来源："local" 本地文件，"online" B 站
	Type   string // 类型："local" / "mv" / "audio"
	Bvid   string // 视频 id（mv 用）
	Cid    string // 分 P id（mv 多集时用）
	Sid    string // 音频 id（audio 用）
	Path   string // 本地文件的完整路径
	Title  string // 显示在播放条上的名字
	Cover  string // 封面地址，在线的歌才有
}

// LocalItem 把一个本地音频文件包成一项。
//
// Cover 留空：本地文件不读音频内嵌的图，Biu 上游也不读（见 State.CoverURL）。
// 所以本地音乐在那边也是占位，这里照它来，不拿路径凑一个假地址。
func LocalItem(path string) Item {
	return Item{
		Source: "local",
		Type:   "local",
		Path:   path,
		Title:  strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
	}
}

// Queue 是播放队列。零值不可用，请用 NewQueue。
//
// 末尾的行为照抄 Biu 的 play-list.ts：next 和 prev 都是取模绕回，队列里只有
// 一首歌时按下一首等于从头再播。随机模式和「多集视频要按分集顺序」那些规则
// 现在不做，但它们也只是从 Move 里分出去。
type Queue struct {
	mu    sync.Mutex
	items []Item
	cur   int // 当前项的下标；-1 是还没开始
}

// NewQueue 建一个空队列。
func NewQueue() *Queue { return &Queue{cur: -1} }

// Append 往队列尾部追加，不动当前项。
func (q *Queue) Append(items ...Item) {
	if len(items) == 0 {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, items...)
}

// Len 返回队列里有多少项。
func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// Items 返回队列的快照，按播放顺序。改它不影响队列。
func (q *Queue) Items() []Item {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Item, len(q.items))
	copy(out, q.items)
	return out
}

// Current 返回正在放的那一项。队列空或还没开始时第二个返回值是 false。
func (q *Queue) Current() (Item, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.cur < 0 || q.cur >= len(q.items) {
		return Item{}, false
	}
	return q.items[q.cur], true
}

// Move 从当前项挪 delta 格（+1 下一首，-1 上一首），返回落到的那一项。
//
// 两端都绕回，和 Biu 一样。队列空时返回 false。还没开始播（cur 是 -1）时，
// 正向落在第一首、反向落在最后一首——用户第一次按下一首，看到的应该是第一首
// 而不是直接跳到第二首。
func (q *Queue) Move(delta int) (Item, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := len(q.items)
	if n == 0 {
		return Item{}, false
	}
	if q.cur < 0 || q.cur >= n {
		if delta < 0 {
			q.cur = n - 1
		} else {
			q.cur = 0
		}
		return q.items[q.cur], true
	}
	q.cur = ((q.cur+delta)%n + n) % n
	return q.items[q.cur], true
}

// Reset 把队列换成给定的几项并从第一首开始，返回第一项。给空就是清空。
func (q *Queue) Reset(items ...Item) (Item, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = items
	q.cur = -1
	if len(items) == 0 {
		return Item{}, false
	}
	q.cur = 0
	return items[0], true
}

// ---- App 这一侧的接线 ----
//
// 方法都定义在这里而不是 app.go：一组相关的东西放一个文件，是队列的形状和
// 它在 App 上的操作。

// Queue 返回这个 App 的队列。界面上要看队列长什么样（播放列表抽屉、下一首是
// 谁）就调它。
func (a *App) Queue() *Queue { return a.queue }

// Enqueue 把几项追加到队列尾部。
//
// 队列还是空的时候，追加的第一项直接开始播——用户点了「加歌」，总得听见声音。
// 已经放过歌了就只是排着，等下一首。
//
// 打头一项打不开就把错误记在播放条上并返回，后面的照排；一首歌打不开不该把
// 整个队列的操作搅黄。
func (a *App) Enqueue(items ...Item) error {
	wasEmpty := a.queue.Len() == 0
	a.queue.Append(items...)
	if !wasEmpty {
		return nil
	}
	item, ok := a.queue.Move(0) // cur 还停在 -1，Move(0) 把它落在第一首
	if !ok {
		return nil
	}
	return a.play(item)
}

// PlayFrom 把队列换成给定的几项，并从第一首开始播。
//
// 这是「在列表里点了一首，从这儿听」那一条路：接进来的从第一首起，往后都是
// 它自己的顺序。跟 Enqueue 的差在两点——Enqueue 是往队尾**追加**（点「加歌」
// 用的），PlayFrom 是**换掉整个队列**（点「播放」用的）；后者还得马上起播，
// 不像 Enqueue 只在队列原本是空的时候才播。
//
// 给空就是什么都不做，**不是清空队列**。清空是另一件事（`Queue.Reset()`），
// 混在一起的表现是：调用方给不出东西时，用户的队列悄悄没了、正在放的那首
// 还在响，播放条和队列对不上。
//
// 打头一项打不开就把队列原样换回去，错误返回：播不了的歌不该把用户原来的
// 队列弄丢，也不该让播放条停在一首放不了的歌上（和 step 里同一个道理）。
func (a *App) PlayFrom(items ...Item) error {
	if len(items) == 0 {
		return nil
	}
	old := a.queue.Items()
	cur, hadCur := a.queue.Current()
	// 给空已经在上面挡掉了，Reset 这里一定放得下（它返回的第一项就是 items[0]）。
	item, _ := a.queue.Reset(items...)
	if err := a.play(item); err != nil {
		// 换回去：Reset 把 cur 落在 0，Move 把当前项挪回它原来在的位置。
		//
		// ⚠️ 挪之前要看找没找着：找不到时 Move(-1) 会绕到**最后**一首（Move 是
		// 取模的），于是「换回去」变成了「跳到队尾」——比不换还糟。
		if _, ok := a.queue.Reset(old...); ok && hadCur {
			if j := indexOfItem(old, cur); j >= 0 {
				a.queue.Move(j)
			}
		}
		return err
	}
	return nil
}

// indexOfItem 是 it 在 items 里的下标，找不到是 -1。
//
// 按整项比：Biu 的判重标识是 {source, type, bvid, cid, sid}，路径也在结构体
// 里，整项相等就意味着是同一首（本地项的标识就是路径，见 Item 的注释）。
func indexOfItem(items []Item, it Item) int {
	for i, x := range items {
		if x == it {
			return i
		}
	}
	return -1
}

// play 放队列里的一项：先开，开了成功再把它的封面地址记到状态上。
//
// 在线条目走 `playOnline`（它自己管代次和后台 goroutine），本地条目还是
// 原来那条同步开文件的路。
//
// 开失败时不动封面：歌没换，还在放上一首，封面也不该跟着换。
func (a *App) play(item Item) error {
	var err error
	if item.isOnline() {
		err = a.playOnline(item)
	} else {
		err = a.Open(item.Path)
	}
	if err != nil {
		return err
	}
	a.setCover(item.Cover)
	return nil
}

// Next 放下一首。到末尾绕回第一首，和 Biu 一致。队列空的时候什么都不做。
func (a *App) Next() { a.step(1) }

// Prev 放上一首。到开头绕回最后一首。队列空的时候什么都不做。
func (a *App) Prev() { a.step(-1) }

// step 在队列里挪 delta 格并放那一首。
//
// 挪过去了却放不了，就退回原来那一首：界面上仍然停在原来那首，按钮也不该在
// 用户眼皮底下换成另一首，而队列的下标要和正在播的那首对得上。
func (a *App) step(delta int) {
	item, ok := a.queue.Move(delta)
	if !ok {
		return
	}
	if item.Path == "" && !item.isOnline() {
		a.queue.Move(-delta)
		a.setErr("这一首还不能放：还没有接来源")
		return
	}
	if err := a.play(item); err != nil {
		a.queue.Move(-delta)
	}
}

// setErr 在播放条上显示一条错误，直到下一次打开成功。
func (a *App) setErr(msg string) {
	a.mu.Lock()
	a.errText = msg
	wake := a.wake
	a.mu.Unlock()
	if wake != nil {
		wake()
	}
}

// CanNext / CanPrev 报告这两个按钮现在该不该亮着。
//
// 队列里只有一首歌时按钮仍然是亮的——Biu 那边的「单曲重播」就是这么做的，
// 按下去用户听到的是同一首从头来，不该表现成按了没反应。
func (a *App) CanNext() bool { return a.canStep() }
func (a *App) CanPrev() bool { return a.canStep() }

func (a *App) canStep() bool {
	a.mu.Lock()
	playing := a.player != nil
	a.mu.Unlock()
	return playing && a.queue.Len() > 0
}
