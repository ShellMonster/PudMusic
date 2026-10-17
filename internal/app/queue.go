package app

import (
	"fmt"
	"path/filepath"
	"slices"
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
	// Artist 是艺人，在线条目就是 UP 主名（B 站那边叫 ownerName，
	// Biu 的 `updateMediaSession` 把它当 artist 传给系统媒体会话）。
	//
	// **本地文件永远是空的**：读音频内嵌标签那一套没有接，Biu 上游也没有
	// （见 State.CoverURL 那段）。所以这一栏只对在线条目有意义。
	//
	// B 站那几个接口（推荐、稍后再看、历史、收藏夹内容）都带 `owner`，所以
	// 在线条目上它是有值的——播放条上那一行 UP 名就画的是它（见
	// `ui.viewArtist`）。
	Artist string

	// ArtistMid 是 Artist 那一位 UP 主的 mid，也就是播放条上那个 UP 名
	// 要点的 `/user/{id}`（见 `ui.upPath`）。
	//
	// ⚠️ **它和 Artist 分开，而且认不出 UP 主时 Artist 照样有值**：有些来源
	// 只给名字不给 mid（动态流那一批就是）。那种情况下播放条照旧显示 UP 名，
	// 只是那一句**点不动**——把名字藏起来是另一种撒谎（用户看到的是「这一首
	// 没有 UP 主」）。
	//
	// 0 就是「没有编号」，不是一个 UP 主。
	ArtistMid int64
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

// Identity 是这一项的判重标识，也就是 Biu 那个抽屉用来去重的那个键
// （`biu/src/components/music-playlist-drawer/index.tsx` 的 uniqBy）：
//
//	本地 → local:<完整路径>     （Biu 的 local:<id>，本地项的 id 就是路径）
//	MV   → mv:<bvid>
//	音乐 → audio:<sid>
//
// **认不出是哪一首时返回空串**，调用方必须自己拿这个空串当「没法判重」处理，
// 不能拿它去查表或者当行身份。空串不是「谁都跟它一样」——那样会把两首不同的歌
// 并成一行，看起来像是其中一首不见了。真出现这种项时（还没接来源的占位项），
// 抽屉改用它在队列里的下标当身份。
//
// 键里带来源前缀不是多余的：Bvid / Sid / 路径三样都是用户数据，不加前缀的话
// 一个叫 `1293494` 的 sid 和一个叫 `1293494` 的本地文件名会认成同一首。
func (it Item) Identity() string {
	// ⚠️ Source 和 Type 都判（见 IsLocal）：`LocalItem` 两个都填，而手写的
	// Item 常常只填 Type。少判一个的话，本地项会掉到下面那两支里去拿一个空
	// 标识。
	switch {
	case it.IsLocal():
		if it.Path == "" {
			return ""
		}
		return "local:" + it.Path
	case it.Type == "mv":
		if it.Bvid == "" {
			return ""
		}
		return "mv:" + it.Bvid
	case it.Type == "audio":
		if it.Sid == "" {
			return ""
		}
		return "audio:" + it.Sid
	}
	return ""
}

// IsLocal 报这一项是不是本地文件。
//
// 🔴 **Source 和 Type 都判**：两个字段**都有**填成在线、也都有只填 Type 的手写
// 项（`ui` 里从 B 站接口翻成 Item 的那几处都填全了，而将来谁手写一个只填
// `Type: "mv"` 的项就会踩到）。只判一个的话漏掉的那种会被当成本地文件，而症状
// 是「在线的歌在访达里显示」——用户看到的是「这一首没有来处」。
//
// 界面层和下载这一层都问它，**别再各写一份**——两处各写一份的代价是改了
// 这里忘了那里，而本地项认错了的表现是「在访达里显示一个在线的歌」。
func (it Item) IsLocal() bool { return it.Source == "local" || it.Type == "local" }

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

// InsertNext 把一项插到**正在放的那首后面**——「下一首播放」那一下。
//
// 和 Append 的分界就是插在哪：Append 接在**队尾**（用户下次翻到队尾才听得到），
// 这一条插在**当前的下一位**（这一首放完就是它）。
//
// ⚠️ **cur 不动**：插在 cur 的**后面**，所以当前项的下标不变——而改它的话用户正
// 在听的那首会被顶到别处（表现是「按了下一首播放，正在放的歌跳了一首」）。
//
// ⚠️ **cur == -1（还没开始放）时插到最前面**：那时候队列里的东西都还没轮到，
// 新来的这一首理应是下一个。插在队尾的话用户会以为它排在最后。
//
// 队列是空的那一支把 cur 落在 0：没有正在放的那首，「下一首」就是它自己。
// 起播是 `App.EnqueueNext` 的事（它照 `App.Enqueue` 那样判「队列原本空不空」）。
func (q *Queue) InsertNext(item Item) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) == 0 {
		q.items = []Item{item}
		q.cur = 0
		return
	}
	at := 0
	if q.cur >= 0 {
		at = min(q.cur+1, len(q.items))
	}
	q.items = slices.Insert(q.items, at, item)
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

// itemAt 是队列里第 i 项，下标越界时是零值。
//
// 「点一行跳过去」要读**目标那项**（和当前项比是不是同一首），所以队列得能按
// 下标读一项——`Items()` 也能，但那是拷一整列。
func (q *Queue) itemAt(i int) (Item, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if i < 0 || i >= len(q.items) {
		return Item{}, false
	}
	return q.items[i], true
}

// CurrentIndex 返回正在放的那一项在队列里的下标，-1 是还没开始。
//
// 界面上「定位当前播放」「这一行是不是正在放」都要下标，不要项：项是值拷贝，
// 拿去比相等只能认出完全相同的那一项（见 indexOfItem），而抽屉里认的是同一首
// 歌（Biu 的 `isSame` 那一层，判重标识，见 Item.Identity）。
func (q *Queue) CurrentIndex() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.cur < 0 || q.cur >= len(q.items) {
		return -1
	}
	return q.cur
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

// RemoveAt 把第 i 项从队列里删掉，返回删掉的是哪一项。下标越界时是 false。
//
// 删完之后 cur 仍然指着**同一首正在放的歌**：
//
//   - 删的是它前面的一行 → cur 往前挪一格，不挪的话指针就落到下一首去了，
//     界面上（播放条 / 抽屉）会显示歌还在放、下标指的却是另一首。
//   - 删的就是它自己 → cur 停在原处，也就是后面那一首；删的是末尾则绕回第一首。
//   - 一首都不剩了 → cur 是 -1。
//
// ⚠️ 这一层**只管队列**。删掉正在放的那首之后到底要不要接着放下一首，是 App
// 那边的事（要开播放器），见 App.RemoveAt。
func (q *Queue) RemoveAt(i int) (Item, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if i < 0 || i >= len(q.items) {
		return Item{}, false
	}
	it := q.items[i]
	q.items = slices.Delete(q.items, i, i+1)

	switch {
	case len(q.items) == 0:
		q.cur = -1
	case q.cur > i:
		q.cur--
	case q.cur == i:
		// 删掉的正是当前项，它原来的下标现在指着后面那一首；删的是末尾就绕回
		// 第一首（和 Move 两端绕回一个道理，见文件头的说明）。
		if q.cur >= len(q.items) {
			q.cur = 0
		}
	}
	return it, true
}

// RemoveIdentity 把队列里**所有**判重标识等于 id 的项删掉，返回删掉的是哪几项、
// 被删的里面有没有正在放的那首、以及有没有删掉东西。id 是空串时一概不删。
//
// 为什么是「全部」而不是「一份」：抽屉那一行是按标识画的，同一首排了两遍只画一行，
// 删一份的话那一行画完还在——用户按了「从列表删除」，什么也没变。Biu 的
// `play-list.ts` 里 `del` 用的就是 `isSame` 删全部。
//
// cur 的算法和 `RemoveAt` 一样（删掉当前项 → 落在后面那一首；删别的 → 跟着往前挪），
// 只是这里要照顾「删掉好几项」：从**后往前**删，下标和 cur 的相对关系才不会在删的
// 过程中变掉。
func (q *Queue) RemoveIdentity(id string) (removed []Item, wasCurrent, ok bool) {
	if id == "" {
		return nil, false, false
	}
	items := q.Items()
	cur := q.CurrentIndex()
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].Identity() != id {
			continue
		}
		it, done := q.RemoveAt(i)
		if !done {
			continue
		}
		if i == cur {
			wasCurrent = true
		}
		removed = append(removed, it)
	}
	slices.Reverse(removed) // 从后往前删的，翻回来才是队列里的顺序
	return removed, wasCurrent, len(removed) > 0
}

// selectAt 把当前项挪到第 i 首，返回那一项。i 是 -1 时是「还没开始」，此时没有
// 当前项，返回 false。i 越界只返回 false，**不动 cur**。
//
// **不走 Move 的取模**：Move 两端绕回（那是「上一首 / 下一首」要的），而这里
// 用户指着队列里的某一行，放到别的行上去就是错的那一行。
//
// 不导出：设 cur 只有「跳过去放」这一条路（App.PlayAt），多导出一个就多一处能
// 把队列和播放器对不上的地方。回滚也走它（i 传 -1 就是退回「还没开始」）。
func (q *Queue) selectAt(i int) (Item, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if i < 0 {
		q.cur = -1
		return Item{}, false
	}
	if i >= len(q.items) {
		return Item{}, false
	}
	q.cur = i
	return q.items[i], true
}

// ---- App 这一侧的接线 ----
//
// 方法都定义在这里而不是 app.go：一组相关的东西放一个文件，是队列的形状和
// 它在 App 上的操作。

// Queue 返回这个 App 的队列。界面上要看队列长什么样（播放列表抽屉、下一首是
// 谁）就调它。
func (a *App) Queue() *Queue { return a.queue }

// PlayingItem 返回正在放的那一项——**带得上 B 站身份的那一份**（bvid / sid）。
//
// 播放条上的「收藏」「下载」要的就是它：`State` 那一组只有歌名和封面，
// 而收藏夹弹窗要 avid、下载要 bvid + cid（`internal/ui/folderselect.go` 的
// favTarget、`actions.go` 的 Download）。
//
// ⚠️ **队列里没有当前项时退回本地路径**：那是直接 `Open` 打开的（导航栏的
// 「打开文件」、测试里 `app.Open(path)`）——它压根没进队列，可它同样在放。
// 不退回的话那种情况下播放条上的两个键会静悄悄地消失，而用户刚打开了一首歌。
//
// 在线流没有这一支：在线的歌只可能经 `play` 进来（那必然进队列），而 `path`
// 对在线流是空串（见 `online.go` 的 runOnline）——所以拿 `path` 去兜底不会
// 兜出一首假的在线歌。
func (a *App) PlayingItem() (Item, bool) {
	if it, ok := a.queue.Current(); ok {
		return it, true
	}
	a.mu.Lock()
	path, playing := a.path, a.player != nil
	a.mu.Unlock()
	if !playing || path == "" {
		return Item{}, false
	}
	return LocalItem(path), true
}

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

// EnqueueNext 把一项排在**正在放的那首后面**（「下一首播放」）。
//
// 和 `Enqueue` 的分界只在插的位置：那个接在队尾，这一个接在当前的下一位。
//
// 队列原本是空的时候直接起播——和 `Enqueue` 同一句理由：用户点了「下一首播放」，
// 队列里什么都没有，他总得听见声音。
func (a *App) EnqueueNext(item Item) error {
	wasEmpty := a.queue.Len() == 0
	a.queue.InsertNext(item)
	if !wasEmpty {
		return nil
	}
	it, ok := a.queue.Current()
	if !ok {
		return nil
	}
	return a.play(it)
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

// PlayAt 把当前项挪到队列里的第 i 首并放它。队列里没有这一项时返回错误。
//
// 这是抽屉里点一行的那条路：**不换队列**（那是 PlayFrom），只是跳过去。
// 放不了就退回原来那一首，和 step 同一个道理——用户点的是这一首，不能因为放
// 不开就把他放到了另一首上，而抽屉里的高亮还得跟着下标走。
//
// 点**正在放**的那一首什么都不做：Biu 的 `playListItem` 也是开头一句
// `if (get().playId === id) return`。重头播是播放条上「播放」那个键的活。
//
// ⚠️ 「正在放」有两层，两层都要：
//
//  1. **真的���歌在放**（`hasTrack`）。只看队列的当前项的话，「接歌失败 → 停下来」
//     之后当前项还指着那一首，于是再点它永远是什么也不做——文件补回来了、用户按的
//     也是「播放」，却再也放不出来。
//  2. **同一首**（判重标识），不是同一个下标。队列 `[A, A]` 时抽屉去重只画一行
//     （第一份），而正在放的可能正是**第二份**；按下标认，这一行会被当成「另一首」
//     而从头重播——用户点的就是他正在听的那一行。
func (a *App) PlayAt(i int) error {
	if i < 0 || i >= a.queue.Len() {
		return fmt.Errorf("队列里没有第 %d 首", i+1)
	}
	target, _ := a.queue.itemAt(i)
	cur := a.queue.CurrentIndex()
	if a.hasTrack() {
		playing, ok := a.queue.Current()
		switch {
		case cur == i:
			return nil // 同一项、正在放
		case ok && sameSong(playing, target):
			// 同一首的另一份：把队列的当前项指过去，**不重播**。指过去是为了
			// 「下一首」从这一行后面接着走。
			a.queue.selectAt(i)
			return nil
		}
		if target.isOnline() {
			// 换成在线的一首：**先把还在响的那首停掉**。在线流是后台建的（取流要
			// 几百毫秒的网络往返），开它的那一刻界面上还没有新歌——**还响着的是上
			// 一首**，而抽屉已经按新的当前项把高亮挪走了。用户看到的是「播放条上是
			// 旧的那首、抽屉里高亮的是新的那一首」，而真正出声的偏偏还是旧的。
			//
			// 下一首打不开时（`failOnline` 只报一句错）也就不会「旧歌继续响」——
			// 那时候什么都没有在放，抽屉也不高亮任何一行，两边说的是同一件事。
			//
			// 本地项不用这样：`Open` 是同步的，换歌和换歌之间没有缝。
			a.ClearTrack()
		}
	}
	item, _ := a.queue.selectAt(i)
	if err := a.play(item); err != nil {
		a.queue.selectAt(cur) // cur 是 -1 时就是退回「还没开始」
		return err
	}
	return nil
}

// sameSong 报两项是不是同一首（判重标识相同）。
//
// **认不出是哪一首的（标识是空串）一律当不同**：两首都认不出的歌在标识上长得一样，
// 当成同一首就会把「点一行」变成「什么都不做」。
func sameSong(x, y Item) bool {
	id := x.Identity()
	return id != "" && id == y.Identity()
}

// IsPlaying 报这一项是不是**正在放的那首**。
//
// 列表上「正在播放」的高亮认的就是它（见 `widgets/list` 的 Actions.IsPlaying）。
//
// 🔴 **按判重标识认，不按下标，也不按 `Music.ID`**：
//
//   - **不按下标**：队列会重排——「下一首播放」插在当前后面、拖拽排序、随机模式
//     哪一个都在动它。当前项的下标跟着重排走，而列表里的下标是另一套，两边
//     永远不会对上。症状是「高亮的那一行在跳」。
//   - **不按 `Music.ID`**：那是**行的**身份（铁律四），为了在前面插一行时不串状态
//     而存在，它和「这是不是同一首歌」是两回事（历史记录里同一首歌看过两次是
//     两行，`Music.ID` 不同而它们是同一首）。
//
// 认不出是哪一首的（`Identity` 是空串）一律当「不在放」：那是「还没接来源的
// 占位项」，把它当正在放的话界面上会出现一个说不清自己在指哪里的高亮。
func (a *App) IsPlaying(it Item) bool {
	id := it.Identity()
	if id == "" {
		return false
	}
	cur, ok := a.queue.Current()
	if !ok {
		return false
	}
	// ⚠️ 认不出正在放那首的标识（空串）时不能拿它和 id 比：那样 id 非空、
	// cur 为空，两边不一样是对的；反过来 id 为空已经在上面挡住了。
	return cur.Identity() == id
}

// hasTrack 报告现在有没有歌在放（「播放器还开着」就是有）。
func (a *App) hasTrack() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.player != nil
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

// play 放队列里的一项：先开，开了成功再把它的封面地址和艺人记到状态上。
//
// 在线条目走 `playOnline`（它自己管代次和后台 goroutine），本地条目还是
// 原来那条同步开文件的路。
//
// 开失败时不动封面和艺人：歌没换，还在放上一首，它们也不该跟着换。
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
	a.setArtist(item.Artist, item.ArtistMid)
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

// RemoveAt 把队列里第 i 项删掉。队列里没有这一项时返回错误。
//
// 删掉的正是正在放的那首时：后面还有歌就接着放下去（原处是末尾则绕回第一首），
// 一首都不剩就停下来。
//
// 接着放的那首**打不开**时停下来，并且返回一句说清了的错误。停下来不是认输，是
// 别无选择：让刚才那首继续响的话，队列里已经没有它、播放条上还写着它的名字、
// 抽屉把下一首高亮成正在放——三处各说各话，正是 App.ClearTrack 要避免的那种不一致。
// 错误里带上「已经删掉了」是为了让调用方别再说「删不掉」：删是真的删掉了，
// 说它没删是另一回事。
func (a *App) RemoveAt(i int) error {
	cur := a.queue.CurrentIndex()
	if _, ok := a.queue.RemoveAt(i); !ok {
		return fmt.Errorf("队列里没有第 %d 首", i+1)
	}
	if cur != i {
		return nil
	}
	return a.playNextAfterCurrent()
}

// RemoveSongFromList 把队列里**所有**和它同一首的项删掉，返回删掉了几份。
//
// 这是抽屉那一行「从列表删除」接的那一条：那一行是**按判重标识**画的，同一首排了
// 两遍就只画一行——只删一份的话那一行画完还在（顶上的计数也不变），用户会以为按了
// 没反应。Biu 的 `play-list.ts` 里 `del` 用 `isSame` 删掉全部同身份项，就是这个道理。
//
// 认不出是哪一首的项（`Identity` 是空串）**一概不删**：两首都认不出来的歌在标识上
// 长得一样，按一次删掉两首不同的歌比按了没反应糟得多。
func (a *App) RemoveSongFromList(it Item) (int, error) {
	removed, wasCurrent, ok := a.queue.RemoveIdentity(it.Identity())
	if !ok {
		return 0, fmt.Errorf("队列里没有「%s」", it.Title)
	}
	if !wasCurrent {
		return len(removed), nil
	}
	return len(removed), a.playNextAfterCurrent()
}

// playNextAfterCurrent 是「当前项已经不在队列里了，接着放下一首」那一条。
//
// 两条路（RemoveAt / RemoveSongFromList）都走它，因为**漏了哪一条，界面上都是
// 「队列里没有它、歌还在响、播放条和抽屉各说各话」**。
//
//  1. **下一首是在线的 → 先把正在响的停掉再开新的。** 在线流是后台建的（取流要几百
//     毫秒的网络往返），开它的那一刻界面上还没有新歌，**还响着的是刚刚从队列里删掉
//     的那首**。本地项不用这样：`Open` 是同步的，换歌和换歌之间没有缝。先调
//     `ClearTrack` 正好合适——它加一次播放代次，把后面这次开流和上一次隔开。
//  2. **下一首打不开 → 停下来，并说一句说清了的错误。** 停下来不是认输，是别无选择。
//     错误里带上「已经删掉了」是为了让调用方别再说「删不掉」：删是真的删掉了。
func (a *App) playNextAfterCurrent() error {
	item, ok := a.queue.Current()
	if !ok {
		// 一首都不剩了。
		a.ClearTrack()
		return nil
	}
	if item.isOnline() {
		a.ClearTrack()
	}
	if err := a.play(item); err != nil {
		a.ClearTrack()
		return fmt.Errorf("已经删掉了，但接不上下一首：%w", err)
	}
	return nil
}

// ClearQueue 清空队列并停下来。
//
// 停了还得把当前曲目清干净（见 ClearTrack）：只停声的话，播放条上还是那首歌的名字，
// 队列里已经空了。
func (a *App) ClearQueue() {
	a.queue.Reset()
	a.ClearTrack()
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
