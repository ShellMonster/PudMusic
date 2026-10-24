package app

import (
	"errors"
	"fmt"
	"math/rand"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// 最小播放队列：当前项 + 上一首 + 下一首 + 尾部追加，外加**四档播放模式**。
//
// 判重、跨来源留给后面的阶段——但队列本身不能等：上一首/下一首是播放条上
// 现在就有的两个按钮，界面层已经按「队列存在」的样子写了，先把接口立住，
// 后面接 B 站的来源时不用动播放条。播放模式同理，「一首歌放完之后下一首是
// 谁」是播放条上那个模式按钮背后唯一的一件事。
//
// 队列跟着 App 走：App 一开始就是空队列（见 app.go 的 New）。

// ItemTypeUser 是「**一个用户**，不是一首歌」那一项在 `Item.Type` 上的值。
//
// ⚠️ **搜索结果的用户 Tab 那一行用它**（那一行的「播放」是进 UP 主空间，
// 不是放这一首）。它出现在 `Item.Type` 上，是因为菜单那一层的协议只把
// `Item` 递给回调（「用户刚按的是哪一个」得能从那一项本身答得出来）。
//
// 🔴 **判据要把它和「一首歌但放不了」分开**：这一行压根没有「能不能放」这
// 回事，把它当成放不了的歌的话，菜单里那一项会变成灰的，而用户看到的是
// 一句对他毫无意义的话（「这一条不是投稿，放不了」）。
const ItemTypeUser = "user"

// 队列项的**来源**。值和 Biu 那边的字面量一样（`play-list.ts` 的 `source`：
// "local" / "online"），别自己另起一套——这两个值会跟着队列项落进设置文件
// （下载队列、播放历史那几处），改了名字就认不出旧记录了。
//
// ⚠️ **仓库里别处还有写字面量的**（`Item.Source` 的各个赋值处）。改成常量是
// 另一件事（要一次改完所有赋值点，还要过一遍读旧记录的地方），不归这一票。
// **新写的代码用这两个常量**。
const (
	// SourceLocal 是「本地文件」那一档。
	SourceLocal = "local"
	// SourceOnline 是「B 站在线」那一档。
	SourceOnline = "online"
)

// Item 是队列里的一项。
//
// 身份按 Biu 的判重标识来（`_research/biu/src/store/play-list.ts` 的 isSame）：
// 本地项按 id（也就是路径），在线的 mv 按 bvid，audio 按 sid。
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
	// B 站那几个接口（推荐、稍后再看、历史、收藏夹内容、搜索、动态、UP 空间
	// 投稿）都带 `owner`，所以在线条目上它是有值的——播放条上那一行 UP 名画的
	// 就是它（见 `ui.viewArtist`）。
	Artist string

	// ArtistMid 是 Artist 那一位 UP 主的 mid，也就是播放条上那个 UP 名
	// 要点的 `/user/{id}`（见 `ui.upPath`）。
	//
	// ⚠️ **它和 Artist 分开，而且没有编号时 Artist 照样有值**：B 站有的接口
	// 逐条只给 `author` 名字不给 mid（`bili.UpVideo` 那一批就是这样，编号要
	// 由调用方从自己手上那一页的 mid 补）。那种情况下播放条照旧显示 UP 名，
	// 只是那一句**点不动**——把名字藏起来是另一种撒谎（用户看到的是「这一首
	// 没有 UP 主」）。
	//
	// 0 就是「没有编号」，不是一个 UP 主。
	//
	// ⚠️ **每个来源都要往下传它**：推荐 / 稍后再看 / 历史 / 收藏夹内容 / 搜索
	// 结果 / 动态 / UP 空间投稿，七处都带上了。少一个的症状是**从那一页放的
	// 歌在播放条上没有 UP 名**——而界面上看不出坏了（按钮和那两行字都还在，
	// 只是名字那一格空着）。
	ArtistMid int64

	// PageIndex / TotalPage 是多 P 视频里的「第几集 / 共几集」，对应 Biu 的
	// `PlayItem.pageIndex` / `totalPage`。0 表示「这不是分集视频」。
	//
	// 🔴 **今天全仓库没有一处给它们赋过值**，所以这两个数恒为 0。分集抽屉
	// （BMG-47 的 A12，播放条上那个「分集」键）**不走它们**：它问的是
	// `App.Parts()` 那份按 bvid 缓存的 `[]bili.VideoPart`（「正在放的那一集」
	// 是拿每一集的 cid 和当前项的 cid 比出来的），而换集是 `App.PlayPart`
	// **就地改队列里那一项的 Cid**，不插新的项——所以队列里从头到尾只有一项。
	//
	// ⚠️ **正因为队列里不分集，`Identity` 把分集算进去是错的**：同一 bvid 的两集
	// 在它眼里是同一首，这是对的（它们本来就是队列里的同一项）。这一格原来写着
	// 「等分集那一票落地时把 Identity 和抽屉的去重一起改」，而那一票选了另一条路。
	//
	// 那两个数唯一真的读者是「随机时保持分集顺序」那一个开关（见 `Queue.Advance`
	// 的随机分支和 `nextPage`），而它要的是**队列里有分集**——那件事还没做（要做
	// 就得让每一集各占一项，于是上面那条 `Identity` 和抽屉的去重才真的要改）。
	PageIndex int
	TotalPage int
}

// LocalItem 把一个本地音频文件包成一项。
//
// Cover 留空：本地文件不读音频内嵌的图，Biu 上游也不读（见 State.CoverURL）。
// 所以本地音乐在那边也是占位，这里照它来，不拿路径凑一个假地址。
func LocalItem(path string) Item {
	return Item{
		Source: SourceLocal,
		Type:   "local",
		Path:   path,
		Title:  strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
	}
}

// 判重标识的三个前缀。**和 `Item.Identity()` 认的完全一样**，而落盘那一格
// （`playback.items`）也用它们——所以「落盘时写的」和「恢复时认的」永远是同一
// 个东西。
//
// ⚠️ **前缀不是多余的**：bvid / sid / 路径三样都是用户数据，不加前缀的话一个
// 叫 `1293494` 的 sid 和一个叫 `1293494` 的本地文件名会认成同一首。
const (
	// playbackIDLocal 是本地文件那一档：`local:<完整路径>`。
	playbackIDLocal = "local:"
	// playbackIDMV 是投稿那一档：`mv:<bvid>`。
	playbackIDMV = "mv:"
	// playbackIDAudio 是音乐库那一档：`audio:<sid>`。
	playbackIDAudio = "audio:"
)

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
		return playbackIDLocal + it.Path
	case it.Type == "mv":
		if it.Bvid == "" {
			return ""
		}
		return playbackIDMV + it.Bvid
	case it.Type == "audio":
		if it.Sid == "" {
			return ""
		}
		return playbackIDAudio + it.Sid
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
// 一首歌时按下一首等于从头再播。
//
// **「一首歌放完之后下一首是谁」不走 Move，走 Advance**：Move 是用户按了
// 「下一首」（按顺序挪一格、两端绕回，和档位无关），Advance 是**歌自己放完
// 了**（按当前档位决定，见下面「播放模式」那一节）。分界是 Biu 那边也分开的：
// `audio.onended` 才看 playMode，手动的 next 按钮不看（`next()` 里三档
// 共用同一个分支）。
type Queue struct {
	mu    sync.Mutex
	items []Item
	cur   int // 当前项的下标；-1 是还没开始

	// onChange 是「队列变了」的钩子。
	//
	// 它由 App 装上（见 New）：队列的形状一变，落盘那一格就得跟着变
	// （见 playback_state.go）。
	//
	// ⚠️ **它是在 `q.mu` 外面调的**：它要拿 App 那把锁，而 App 那边有别的路
	// 是「先拿 App 的锁再拿队列的锁」（见 `Snapshot`），凑齐了就是一次死锁。
	//
	// ⚠️ **装上之后不许再换**：换了就有一处读到的是旧的。
	//
	// ⚠️ **后来加的「改队列」的方法也要调 `changed()`**：漏掉一个的后果是
	// 「用户拖过的队列没存下来」（重启之后回到旧的顺序）。
	onChange func()

	// mode 和 keepPages 归**这把锁**管，不归 App 的 `a.mu` 管：它们是队列的
	// 形状（下一首是谁要靠它们算），而 `a.mu` 管的是播放器。界面每帧读的
	// `State` 因此只是多拿一把互不相干的锁，不会把播放器那一趟（它可能正
	// 卡在一次网络读上）也拖进来。
	mode PlayMode
	// keepPages 的默认是 true，照 Biu 的初始值（`play-list.ts:373`）。
	keepPages bool
	// rnd 是随机模式挑歌用的随机源。见 RandSource 的注释——为什么不能
	// 用包级的那个。
	rnd RandSource

	// bag 和 history 是**随机模式那两样**，见 queue_shuffle.go：
	//
	//	bag     洗牌袋——「下一首是谁」。一列打乱过的歌，一首一首地发。
	//	history 历史栈——「上一首是谁」。放过的那些首，倒着退回来用。
	//
	// **不在随机模式时它们是空的**（换档时清掉，见 SetPlayMode）：其余三档的
	// 「下一首是谁」是纯按索引算的（见 Queue.Advance），这两样在那些档位上
	// 没有读者。
	//
	// ⚠️ **它们记的是判重标识（`Item.Identity`），不是下标**：下标会随着队列
	// 的改动（加歌、删歌、重排）全变，而判重标识不会。将来队列要落盘时，落盘
	// 的键也是判重标识——两边对得上，这两样不用推倒重来。
	bag     []string
	history []string

	// undo 记着上一次「挪动」是怎么做的，好退回去（见 stepUndo 和 UndoStep）。
	//
	// 放失败的时候要退：挪过去了却放不了，就退回原来那一首——界面上仍然停在
	// 原来那首，播放条上也不该在用户眼皮底下换成另一首。
	undo stepUndo
}

// NewQueue 建一个空队列，随机源按当前时间播种。
//
// 用包级的 `rand` 就不对：全局那个没法在测试里定住，于是「随机」会变成一条
// 偶尔绿偶尔红的测试。测试和任何想要可复现乱序的调用方走 NewQueueWithRand。
func NewQueue() *Queue {
	return NewQueueWithRand(rand.New(rand.NewSource(time.Now().UnixNano())))
}

// NewQueueWithRand 建一个空队列，随机模式挑歌走给定的随机源。
//
// 同一个源必然挑出同一串歌，所以「随机这一档下一首是 c」这种断言才能一次
// 一次地钉死，而不是偶尔红一次。
func NewQueueWithRand(r RandSource) *Queue {
	return &Queue{cur: -1, mode: DefaultPlayMode, keepPages: true, rnd: r}
}

// changed 告诉外面「队列变了」。**必须在 `q.mu` 外面调**（理由见 onChange）。
//
// 它只是通知，不改队列——所以「通知」和「改动」之间没有半途改完的状态。
func (q *Queue) changed() {
	if q.onChange != nil {
		q.onChange()
	}
}

// Append 往队列尾部追加，不动当前项。
func (q *Queue) Append(items ...Item) {
	if len(items) == 0 {
		return
	}
	q.mu.Lock()
	q.items = append(q.items, items...)
	q.mu.Unlock()
	q.changed()
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

	if len(q.items) == 0 {
		q.items = []Item{item}
		q.cur = 0
		q.mu.Unlock()
		q.changed()
		return
	}
	at := 0
	if q.cur >= 0 {
		at = min(q.cur+1, len(q.items))
	}
	q.items = slices.Insert(q.items, at, item)
	// ⚠️ **袋子不用动**：它记的是判重标识，而插一首歌不改别的歌的身份
	// （见 Queue 里 bag 那段注释）。
	q.mu.Unlock()
	q.changed()
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
//
// ⚠️ **它只挪播放指针，不管随机模式那两样**（洗牌袋、历史栈）。按「下一首 /
// 上一首」要走 MoveNext / MovePrev——那两条才看播放模式（见它们的注释）。
// 这个裸的挪法留给「把当前项指到队列里的第几项」那种用法（见 App.PlayFrom
// 的回滚）。
func (q *Queue) Move(delta int) (Item, bool) {
	q.mu.Lock()
	it, ok := q.moveIndexLocked(delta)
	q.mu.Unlock()
	if ok {
		q.changed()
	}
	return it, ok
}

// moveIndexLocked 是 Move 的那一份（锁已经拿好了）。
//
// ⚠️ **不动随机模式那两样**：它挪的是**指针**，不是「下一首是谁」。
func (q *Queue) moveIndexLocked(delta int) (Item, bool) {
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

// ---- 「下一首 / 上一首」那两条路（看播放模式） ----
//
// 「按下一首」和「按上一首」这两个按钮**不是**「指针挪一格」：
//
//   - 顺序 / 循环 / 单曲三档：确实是挪一格（见 moveIndexLocked）。
//   - **随机那一档**：下一首是**从洗牌袋里抽**（不重复、不遗漏），上一首是
//     **从历史栈里退**（回到刚才放过的那首）。这两样见 queue_shuffle.go。
//
// 为什么随机那一档不能按索引走：按索引走的话「下一首」永远是队列里的下一项，
// 而这一档是用户**特意挑**的——它得真的随机。而「上一首」按索引退一格退回去的
// 是队列里的上一项，可能根本没放过（随机模式下用户听到的次序和队列的次序是
// 两件事）。
//
// ⚠️ **这两条和 Move 的差别不只是随机模式**：它们会把「刚才那首」记进历史栈，
// 所以放失败的时候要能退回去（见 UndoStep）。

// MoveNext 是「按下一首」。
//
// 随机那一档**从洗牌袋里抽**（不重复、不遗漏），其余各档按索引往后挪一格。
//
// 抽到的那一首会把**刚才那首压进历史栈**——于是「上一首」能回到它（见
// MovePrev）。队列里只有一首歌时没有第二首可抽，重放它。
func (q *Queue) MoveNext() (Item, bool) {
	q.mu.Lock()
	it, ok := q.moveNextLocked()
	q.mu.Unlock()
	if ok {
		// 当前项变了：落盘那一格得跟着变（changed 必须在锁外面调，见它的注释）。
		q.changed()
	}
	return it, ok
}

// moveNextLocked 是 MoveNext 的那一份（锁已经拿好了）。
func (q *Queue) moveNextLocked() (Item, bool) {
	n := len(q.items)
	if n == 0 {
		return Item{}, false
	}
	prevCur := q.cur
	if q.mode != PlayModeRandom {
		it, ok := q.moveIndexLocked(1)
		q.undo = stepUndo{ok: ok, prevCur: prevCur}
		return it, ok
	}
	if n == 1 {
		// 只有一首歌：没有第二首可抽，重放它。
		if q.cur < 0 {
			q.cur = 0
		}
		q.undo = stepUndo{ok: true, prevCur: prevCur}
		return q.items[q.cur], true
	}
	i, ok := q.bagNextLocked()
	if !ok {
		q.undo = stepUndo{}
		return Item{}, false
	}
	pushed := q.setCurLocked(i, true)
	q.undo = stepUndo{ok: true, prevCur: prevCur, drawn: q.items[i].Identity(), popHist: pushed}
	return q.items[i], true
}

// MovePrev 是「按上一首」。
//
// 随机那一档**走历史栈**（回到刚才放过的那首），其余各档按索引往前退一格。
//
// 历史栈是空的（刚切到随机模式、或者还没放过任何一首）时退回按索引退一格：
// 那是「上一首」本来就没有的样子，不该什么都不做——用户按了按钮没反应，
// 而界面上也没有任何一句话说「没有上一首」。
func (q *Queue) MovePrev() (Item, bool) {
	q.mu.Lock()
	it, ok := q.movePrevLocked()
	q.mu.Unlock()
	if ok {
		// 当前项变了：落盘那一格得跟着变（changed 必须在锁外面调，见它的注释）。
		q.changed()
	}
	return it, ok
}

// movePrevLocked 是 MovePrev 的那一份（锁已经拿好了）。
func (q *Queue) movePrevLocked() (Item, bool) {
	n := len(q.items)
	if n == 0 {
		return Item{}, false
	}
	prevCur := q.cur
	if q.mode == PlayModeRandom {
		if it, at, ok := q.historyPopLocked(); ok {
			q.setCurLocked(at, false)
			q.undo = stepUndo{ok: true, prevCur: prevCur, pushHist: true, histIt: it.Identity()}
			return it, true
		}
	}
	it, ok := q.moveIndexLocked(-1)
	q.undo = stepUndo{ok: ok, prevCur: prevCur}
	return it, ok
}

// stepUndo 记着上一次「挪动」是怎么做的，好退回去。
//
// 放失败的时候要退（见 App.step 和 App.onTrackFinished）：挪过去了却放不了，
// 就退回原来那一首——界面上仍然停在原来那首，播放条上也不该在用户眼皮底下
// 换成另一首。
//
// 三条路的退法不一样（随机抽的、随机退的、按索引挪的），所以这里把「退回去
// 要做什么」一件件记下来，而不是「再挪回去」——随机那一档没有「再挪回去」
// 这回事。
type stepUndo struct {
	// ok 是「上一次挪动可以退」。挪不动（队列是空的）时是 false。
	ok bool
	// prevCur 是挪之前的当前下标（-1 是「还没开始」）。
	prevCur int
	// drawn 是从洗牌袋里抽出来的那一首的**判重标识**（空串 = 不是随机抽的）。
	//
	// 退的时候它要**放回袋子头**：它这一轮还没轮到。
	drawn string
	// popHist 是「历史栈顶上多压了一首」——退的时候要拿走。
	popHist bool
	// pushHist 是「历史栈顶上少了一首」——退的时候要放回去。
	pushHist bool
	// histIt 是放回去的那一首的判重标识（pushHist 时才有意义）。
	histIt string
}

// UndoStep 把上一次 MoveNext / MovePrev / Advance / 跳过去放退回去。
//
// 放失败时用（见 App.step、App.onTrackFinished 和 App.PlayAt）。返回有没有
// 退成——退不成说明上一次根本没有可退的挪动。
func (q *Queue) UndoStep() bool {
	q.mu.Lock()
	ok := q.undoStepLocked()
	q.mu.Unlock()
	if ok {
		// 当前项退回去了：落盘那一格得跟着变（changed 必须在锁外面调）。
		q.changed()
	}
	return ok
}

// undoStepLocked 是 UndoStep 的那一份（锁已经拿好了）。
func (q *Queue) undoStepLocked() bool {
	u := q.undo
	if !u.ok {
		return false
	}
	q.undo = stepUndo{}
	q.cur = u.prevCur
	if u.drawn != "" {
		// 抽出来的那一首放回袋子头。
		q.bag = append(q.bag, "")
		copy(q.bag[1:], q.bag)
		q.bag[0] = u.drawn
	}
	if u.popHist && len(q.history) > 0 {
		q.history = q.history[:len(q.history)-1]
	}
	if u.pushHist {
		q.historyPushLocked(u.histIt)
	}
	return true
}

// setCurLocked 把当前项挪到第 i 项，顺带把随机模式那两样对齐。
//
// 随机模式里当前项变了要说两件事：
//
//   - **历史栈**：刚才那首压进去（于是「上一首」能回到它）。pushHist 是 false
//     的时候不压——「上一首」自己就是从历史栈里取出来的，那时候再压回去就成
//     了栈在来回弹（退一次前进一次，永远退不回原处）。
//   - **洗牌袋**：新的一首挪到袋尾（它已经在放了，这一轮最后再放它；不挪的话
//     这一轮的头一个可能就是它，用户听到的是「刚放完又放一遍」）。
//
// 返回「有没有往历史栈里压过一项」——调用方要把它记进 undo（退的时候要拿走）。
func (q *Queue) setCurLocked(i int, pushHist bool) bool {
	old := q.cur
	if old == i {
		return false
	}
	pushed := false
	if q.mode == PlayModeRandom {
		oldID, newID := q.identityAtLocked(old), q.identityAtLocked(i)
		// ⚠️ **同一首的另一份不算「放过一首歌」**：队列里同一首排了两遍时，用户
		// 点抽屉里那一行（画的是第一份，而正在放的是第二份），指针挪过去了，
		// 歌还是那一首——压进历史栈的话，「上一首」退回来的是同一首，用户会
		// 以为按钮坏了。
		if pushHist && oldID != "" && oldID != newID {
			q.historyPushLocked(oldID)
			pushed = true
		}
		q.bagMoveToEndLocked(newID)
	}
	q.cur = i
	return pushed
}

// Reset 把队列换成给定的几项并从第一首开始，返回第一项。给空就是清空。
func (q *Queue) Reset(items ...Item) (Item, bool) {
	q.mu.Lock()
	q.items = items
	q.cur = -1
	// 队列整个换掉了：洗牌袋里那些歌不再是这一列的，历史栈里那些也不再是
	// 「刚放过的」。
	q.bag = nil
	q.history = q.history[:0]
	q.undo = stepUndo{}
	if len(items) == 0 {
		q.mu.Unlock()
		q.changed()
		return Item{}, false
	}
	q.cur = 0
	it := items[0]
	q.mu.Unlock()
	q.changed()
	return it, true
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
	if i < 0 || i >= len(q.items) {
		q.mu.Unlock()
		return Item{}, false
	}
	it := q.items[i]
	q.items = slices.Delete(q.items, i, i+1)
	// ⚠️ **袋子不用动**：它记的是判重标识，而删掉一首歌不改别的歌的身份
	// （被删掉的那一首在袋子里还留着，抽到它时对不上队列，跳过就是——见
	// queue_shuffle.go 的 bagNextLocked）。

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
	q.mu.Unlock()
	q.changed()
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

// replaceAt 把第 i 项就地换成 item。队列位置和当前项都**不动**。
//
// 只有换分集用得到（`App.PlayPart`）：同一个 bvid 的另一集在判重上是同一首，
// 插一项新的会让抽屉和队列里同时出现同一首。
//
// 不导出：它和 `selectAt` 一样只许「App.PlayPart」这一条路调，多导出一个就多一处
// 能把队列和播放器对不上的地方。i 越界是 false 且什么都不动。
func (q *Queue) replaceAt(i int, item Item) bool {
	q.mu.Lock()
	if i < 0 || i >= len(q.items) {
		q.mu.Unlock()
		return false
	}
	q.items[i] = item
	q.mu.Unlock()
	q.changed()
	return true
}

// selectAt 把当前项挪到第 i 首，返回那一项。i 是 -1 时是「还没开始」，此时没有
// 当前项，返回 false。i 越界只返回 false，**不动 cur**。
//
// **不走 Move 的取模**：Move 两端绕回（那是「上一首 / 下一首」要的），而这里
// 用户指着队列里的某一行，放到别的行上去就是错的那一行。
//
// ⚠️ **随机模式里这一跳也算「放过一首歌」**：刚才那首压进历史栈，于是「上一首」
// 能回到它（见 setCurLocked）。
//
// 不导出：设 cur 只有「跳过去放」这一条路（App.PlayAt），多导出一个就多一处能
// 把队列和播放器对不上的地方。回滚也走它（i 传 -1 就是退回「还没开始」）。
func (q *Queue) selectAt(i int) (Item, bool) {
	q.mu.Lock()
	if i < 0 {
		q.cur = -1
		q.undo = stepUndo{}
		q.mu.Unlock()
		q.changed()
		return Item{}, false
	}
	if i >= len(q.items) {
		q.mu.Unlock()
		return Item{}, false
	}
	prevCur := q.cur
	pushed := q.setCurLocked(i, true)
	q.undo = stepUndo{ok: true, prevCur: prevCur, popHist: pushed}
	it := q.items[i]
	q.mu.Unlock()
	q.changed()
	return it, true
}

// ---- 播放模式 ----
//
// 四档，档位、文案、默认档和切换顺序全部照 Biu 的
// `_research/biu/src/common/constants/audio.tsx:27-48`（`PlayMode` 枚举 +
// `getPlayModeList`）：
//
//	顺序播放 Sequence → 循环播放 Loop → 随机播放 Random → 单曲播放 Single
//
// **「循环」的两种含义在这里是分开的两档，不是同一档的两个开关**：
//
//   - 循环播放 = **列表循环**：放完一首往后挪，走到队尾绕回第一首。
//   - 单曲播放 = **单曲循环**：放完还是同一首，从头再放一遍。
//
// 合起来说成「循环」的时候，监听是错的——用户说的「循环」到底指「别停」
// 还是「别跳」，得他自己选。Biu 分成两档，我们照它分。
//
// 逐档抄过来的行为（Biu 的 `audio.onended` + `next()`，都在
// `src/store/play-list.ts`）：
//
//	顺序  cur+1，**走到队尾就停**（不绕回）。队列里只有一首歌也是停。
//	循环  cur+1，走到队尾绕回第一首。只有一首歌时绕回的还是它，重头再放。
//	随机  抽。抽之前先问一句「保持分集顺序」要不要听（见 nextPageLocked）。
//	单曲  不挪，还是这一首，从头再放。
//
// 另有两条是 Biu 有、我们**故意不抄**的：
//
//   - 单曲那一档在 Biu 是靠 `audio.loop = true` 交给浏览器的（onended 直接
//     return）。我们不在播放器上开循环：那样「下一首是谁」的决定权就跑到
//     音频引擎里去了，而这一层的验收是「四档各能说清下一首是哪首」。
//     单曲在这里是 Advance 给出的一个结论（重放当前这一首），播放器只负责
//     「从 0 开始放」。
//   - Biu 的随机是每次 `shuffle(整个列表)` 再找当前那首在洗过之后的位置、
//     取它后面那一首。它依赖「上一次洗出来的顺序」，所以同一首歌连着两次
//     放完可能落到同一首上。**我们用洗牌袋**（见 queue_shuffle.go）：一列
//     打乱过的歌，一首一首地发，发完一列再打乱一列——不重复、不遗漏。

// PlayMode 是播放模式。
//
// Biu 那边的 `PlayMode` 是 1..4 的数字；我们落盘用这四个词，因为设置文件是
// 用户能直接编辑的（见 settings.go 的文件头）。**档位顺序、文案和默认档照
// 它**——Biu 的 `play-list.ts:978` 那份持久化里的键名我们原样沿用。
type PlayMode string

const (
	PlayModeSequence PlayMode = "sequence"
	PlayModeLoop     PlayMode = "loop"
	PlayModeRandom   PlayMode = "random"
	PlayModeSingle   PlayMode = "single"
)

// DefaultPlayMode 是刚装上时的档位。Biu 的初始值是 `PlayMode.Loop`
// （`play-list.ts:370`）。
//
// 挑它而不是顺序，是因为「一首歌放完了应用就停住」正是这一票要治的那个症状
// （对照审计 BMG-47 的 A5）。默认档就该是不用手动点下一首的那一档。
const DefaultPlayMode = PlayModeLoop

// PlayModes 是四档按切换顺序排的那一条。
//
// 播放条上那个按钮每按一次就沿着它往前挪一格，挪到最后回到第一档——Biu 的
// `togglePlayMode` 就是 `playModeList[(i+1) % length]`。**顺序和这里一致，
// 界面上按几下能转回同一档才是同一件事。**
var PlayModes = []PlayMode{PlayModeSequence, PlayModeLoop, PlayModeRandom, PlayModeSingle}

// Desc 是这一档在界面上说的那句完整的话，文案照 Biu 的 `getPlayModeList`。
//
// ⚠️ **认不出的档位说默认档那句话，不说空串**。`State` 是界面每一帧现读的
// 值拷贝，而测试和未来的调用方会手写 `app.State{...}`（那时候没有这一格，
// 它是零值）——说空串的话播放条上就有一个**没有字的按钮**，而空按钮不报错、
// 不消失，只是「按下去不知道换成了什么」。
func (m PlayMode) Desc() string {
	switch m {
	case PlayModeSequence:
		return "顺序播放"
	case PlayModeLoop:
		return "循环播放"
	case PlayModeRandom:
		return "随机播放"
	case PlayModeSingle:
		return "单曲播放"
	}
	// 认不出的（空串、手写坏的）说默认档那句话，见上面的说明。
	return "循环播放"
}

// Label 是播放条按钮上那两个字（顺序 / 循环 / 随机 / 单曲）。
//
// 比 Desc 短是因为播放条右边已经挤不下了（见 playbar.go 里那一段关于
// 「放不下第四个控件」的说明）：四个汉字的按钮要 64 以上，而歌名在 760 宽
// 的窗口里只剩 100 出头。**完整的那句在提示和右键那一列里给全**，短标签不
// 负责把话说圆——它是「这一档是哪一族」，不是这一档的全部。
func (m PlayMode) Label() string {
	switch m {
	case PlayModeSequence:
		return "顺序"
	case PlayModeLoop:
		return "循环"
	case PlayModeRandom:
		return "随机"
	case PlayModeSingle:
		return "单曲"
	}
	// 同上：认不出时说默认档，不说空串（见 Desc 那段）。
	return "循环"
}

// Next 是按一下按钮之后的那一档：从最后一档回到第一档。
//
// 认不出的档位（设置文件里手写了一个不认识的词，`normalize` 之后不该出现，
// 但 `SetPlayMode` 谁都能调）落到第一档，而不是停着不动——停着不动的话
// 按那个按钮看起来就是坏的。
func (m PlayMode) Next() PlayMode {
	i := slices.Index(PlayModes, m)
	return PlayModes[(i+1)%len(PlayModes)]
}

// RandSource 是随机模式挑歌用的随机源。方法形状照 `math/rand` 的 Rand，
// 少一个方法就少一份能在测试里替换的接缝。
type RandSource interface {
	Intn(n int) int
}

// AdvanceKind 是一首歌放完之后，队列给出的**结论**那一档。
type AdvanceKind int

const (
	// AdvanceStop：停下来，不放下一首（队列是空的、还没开始放、或者顺序播放
	// 走到了队尾）。
	AdvanceStop AdvanceKind = iota
	// AdvancePlay：接着放 Item 那首。
	AdvancePlay
	// AdvanceReplay：把当前这一首**从头**再放一遍（单曲播放，或者随机/循环时
	// 队列里只有这一首）。
	AdvanceReplay
)

// Advance 是一次「放完了」的结论。Kind 是 AdvanceStop 时 Item 是零值。
type Advance struct {
	Kind AdvanceKind
	Item Item
}

// Advance 是「这一首自然放完了，接下来怎么办」，并把当前项挪到该放的那首上。
//
// **下一首是哪首，只有这里说了算。** 界面层拿这个结论去放，App 那边也只调
// 它——谁要是自己按 `cur+1` 算一遍，随机和单曲两档立刻就错，而那两档错的
// 表现还恰好是最不容易被发现的（放了一首用户以为在听的歌）。
//
// 「自然放完」这个前提由上层判定（`App.onTrackFinished`），这里只管
// 「放完了以后下一首是谁」。
//
// 挪 cur 是为了让播放列表抽屉的高亮跟着走：换了下一首却不挪高亮，抽屉上
// 高亮的是新歌、响着的还是**刚放完的那首**。挪不了（结论是停下或重放）就
// 一步都不动。放失败要退回原来那首——`App.onTrackFinished` 拿 prev 回滚。
func (q *Queue) Advance() Advance {
	q.mu.Lock()

	n := len(q.items)
	// 队列是空的、或者压根没开始放：没有「下一首」这件事可决定。
	if n == 0 || q.cur < 0 || q.cur >= n {
		q.undo = stepUndo{}
		q.mu.Unlock()
		return Advance{Kind: AdvanceStop}
	}
	// oldCur 是挪之前的当前下标：放失败要退回它（见 UndoStep）。
	oldCur := q.cur
	// 这一次 Advance 之前的挪动已经退不掉了（那是上一次的账）。
	q.undo = stepUndo{}

	var d Advance
	switch q.mode {
	case PlayModeSequence:
		// 顺序播放：往后挪一格，**走到队尾就停**。
		//
		// ⚠️ 不绕回是这一档和「循环」唯一真能听出来的差别，而 Biu 就是这么
		// 做的（`audio.onended` 里那句 `playMode === Sequence && currentIndex
		// === length-1` 就停下并回到开头）。绕回去的话顺序和循环就成了一档，
		// 而用户点「顺序播放」通常正是为了听完这一串就停。
		//
		// 队列里只有一首歌时它就是队尾，所以也是停（和 Biu 一致）。
		if q.cur == n-1 {
			d = Advance{Kind: AdvanceStop}
			break
		}
		q.cur++
		q.undo = stepUndo{ok: true, prevCur: oldCur}
		d = Advance{Kind: AdvancePlay, Item: q.items[q.cur]}

	case PlayModeSingle:
		// 单曲循环：一步都不挪，同一首从头再来。
		d = Advance{Kind: AdvanceReplay, Item: q.items[q.cur]}

	case PlayModeRandom:
		if next, ok := q.nextPageLocked(); ok {
			// 跟着分集走（见 nextPageLocked）。这一首也算「放过」：压进历史栈，
			// 「上一首」才回得去。
			pushed := q.setCurLocked(next, true)
			q.undo = stepUndo{ok: true, prevCur: oldCur, popHist: pushed}
			d = Advance{Kind: AdvancePlay, Item: q.items[q.cur]}
			break
		}
		if n == 1 {
			d = Advance{Kind: AdvanceReplay, Item: q.items[q.cur]}
			break
		}
		// 从洗牌袋里抽：不重复、不遗漏（见 queue_shuffle.go）。
		i, ok := q.bagNextLocked()
		if !ok {
			d = Advance{Kind: AdvanceReplay, Item: q.items[q.cur]}
			break
		}
		pushed := q.setCurLocked(i, true)
		q.undo = stepUndo{ok: true, prevCur: oldCur, drawn: q.items[i].Identity(), popHist: pushed}
		d = Advance{Kind: AdvancePlay, Item: q.items[q.cur]}

	default:
		// 循环播放（列表循环）：往后挪一格，**走到队尾绕回第一首**。
		next := (q.cur + 1) % n
		if next == q.cur {
			// 绕回的还是它（队列里只有一首歌）：这一步没有挪，重头再放。
			d = Advance{Kind: AdvanceReplay, Item: q.items[q.cur]}
			break
		}
		q.cur = next
		q.undo = stepUndo{ok: true, prevCur: oldCur}
		d = Advance{Kind: AdvancePlay, Item: q.items[q.cur]}
	}

	q.mu.Unlock()
	// 当前项变了（或者至少「结论」变了），落盘那一格得跟着变。
	q.changed()
	return d
}

// nextPageLocked 找「同一个视频的下一分集」在队列里的下标，没有是 false。
//
// 这是「随机时保持分集顺序」那一个开关（`keepPages`）做的事，抄的是 Biu
// `next()` 的随机分支开头那几句：当前这首是多集视频的某一集、而且**不是最后
// 一集**吗？是的话就去放下一集，**不洗牌**。
//
// 为什么这值得单独一个开关：多 P 视频的队列（连播、分集收藏）里同一首会有
// 好几行，随机一打散它们就彻底没了——用户在听第三集，下一首可能跳到别的
// 视频的第一集去，而「这是同一个视频的下一集」这件事用户是看得出来的。
//
// 三件事都认才算「有下一集」：开着这个开关、当前项带着分集信息、下一集真的
// 在队列里。少认一件就落回正常的随机——**找不到就随机，而不是停下来**。
func (q *Queue) nextPageLocked() (int, bool) {
	if !q.keepPages {
		return 0, false
	}
	cur := q.items[q.cur]
	// PageIndex 从 1 起：0 是「这不是分集视频 / 还没接分集」。
	if cur.PageIndex <= 0 || cur.PageIndex >= cur.TotalPage {
		return 0, false
	}
	for i, it := range q.items {
		if it.Bvid == cur.Bvid && it.PageIndex == cur.PageIndex+1 {
			return i, true
		}
	}
	return 0, false
}

// PlayMode 是当前档位。
func (q *Queue) PlayMode() PlayMode {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.mode
}

// SetPlayMode 换档位。**立刻生效**：下一首是谁是每次 Advance 现算的，没有
// 「开歌时读一次」的缓存，所以换完档不用重开任何一首歌。
//
// 认不出的档位落到默认档（和设置文件里手写错的值一个待遇，见 normalize）：
// 认不出的档位如果原样留着，随机那一支会掉进 `Advance` 的 default（循环），
// 界面上却显示着一句空的话——用户看到的是「按钮上是空的，行为是循环」。
//
// ⚠️ **换档时随机模式那两样都清掉**（洗牌袋、历史栈）：它们只在随机模式下
// 有读者，而「刚才放过谁」在别的档位下是按索引算的（见 MoveNext / MovePrev）。
// 不清的话用户从随机切回循环、再切回随机，会看到一袋上一轮剩的歌。
func (q *Queue) SetPlayMode(m PlayMode) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !slices.Contains(PlayModes, m) {
		m = DefaultPlayMode
	}
	q.mode = m
	q.bagClearLocked()
	q.historyClearLocked()
	q.undo = stepUndo{}
}

// KeepPagesOrderInRandom 报「随机时保持分集顺序」是不是开着。
func (q *Queue) KeepPagesOrderInRandom() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.keepPages
}

// SetKeepPagesOrderInRandom 设「随机时保持分集顺序」。同样立刻生效。
func (q *Queue) SetKeepPagesOrderInRandom(v bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.keepPages = v
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

// EnqueueAndPlay 把几项**按顺序追加**到队列末尾，并从**追加的第一项**开始播。
//
// 这是「全部播放」那一枚的语义（票 #299，2026-10-24 人定的）：**这一页的歌
// 按顺序加进队列并播放**。
//
// # 和 Enqueue 的差别只有一处：起播的那一首
//
// `Enqueue` 只在队列原本是空的时候起播第一项（那是「加歌」：用户加了几首，
// 队列空着就顺手放起来）；这一条**总是**从这一批的第一项开始播——用户按的是
// 「把这一页放一遍」，不是「往队尾塞几首」。
//
// ⚠️ **追加在队尾，播放从这一批的第一项开始**：所以这一页的歌放完之后，队列
// 会绕回前面那几首（队列两端绕回，见 `Queue.Move`）。这是「追加」+「从这一页的
// 第一首开始」这两句话的字面意思——**别偷偷改成插到当前那一首后面**，那是
// 「下一首播放」那一档的事。
//
// 打头这一项打不开就把错误记在播放条上并返回，**队列留着**（用户要的就是
// 它们），播放条退回原来那一首：歌已经进队列了，不该因为第一首放不开就当
// 没发生过。
func (a *App) EnqueueAndPlay(items ...Item) error {
	if len(items) == 0 {
		return nil
	}
	start := a.queue.Len()
	a.queue.Append(items...)
	item, ok := a.queue.selectAt(start)
	if !ok {
		return nil
	}
	if err := a.play(item); err != nil {
		// 退回原来那一首：队列的下标要和正在放的那首对得上（和 `step` 同一个
		// 道理——用户点的是这一页，不该因为放不开就被放到另一首上）。
		//
		// ⚠️ 退的是**刚才那一跳**（UndoStep），不是「再指回去」：随机模式里这
		// 一跳把刚才那首压进了历史栈。
		a.queue.UndoStep()
		return err
	}
	return nil
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
		// 退回刚才那一首（cur 是 -1 时就是退回「还没开始」）。
		//
		// ⚠️ 退的是**刚才那一跳**（UndoStep），不是「再指回去」：随机模式里这一
		// 跳把刚才那首压进了历史栈，再指回去会把它留在栈里——「上一首」就会
		// 退到一首用户根本没在听的歌上。
		a.queue.UndoStep()
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
//
// ⚠️ **随机那一档不走「指针挪一格」**：下一首从洗牌袋里抽、上一首走历史栈
// （见 Queue.MoveNext / MovePrev）。退回去也是退那一次挪动（UndoStep），不是
// 「再挪回去」——随机那一档没有「再挪回去」这回事。
func (a *App) step(delta int) {
	var item Item
	var ok bool
	if delta < 0 {
		item, ok = a.queue.MovePrev()
	} else {
		item, ok = a.queue.MoveNext()
	}
	if !ok {
		return
	}
	if item.Path == "" && !item.isOnline() {
		a.queue.UndoStep()
		a.setErr(errNoSource.Error())
		return
	}
	if err := a.play(item); err != nil {
		a.queue.UndoStep()
	}
}

// ---- 播放模式在 App 上的接线 ----
//
// 界面上只做两件事：读 State 画出当前是哪一档、按一下按钮调 CyclePlayMode。
// **界面层不许自己算下一首**——它连 `Queue` 都拿不到下一首是谁，
// `Queue.Advance` 是唯一那一条路（见那一节的说明）。

// PlayMode 返回当前档位。
func (a *App) PlayMode() PlayMode { return a.queue.PlayMode() }

// SetPlayMode 切到某一档，并记进设置。
//
// **立刻生效，不用重开歌**：下一首是谁是每次 Advance 现算的，没有「开歌时
// 读一次」的缓存。用户在第 3 首放完之前换档，第 3 首放完走的就是新档的规则。
func (a *App) SetPlayMode(m PlayMode) {
	a.queue.SetPlayMode(m)
	a.rememberPlayMode()
	// ⚠️ 这一句不能省：`rememberPlayMode` 走的是 UpdateSettings，而没挂设置
	// 时它直接就走了（连唤醒都没有）。播放条上那个按钮必须**当场**换掉字，
	// 不能等下一次别的东西碰巧把界面叫醒。
	a.invalidate()
}

// CyclePlayMode 按一下播放条上那个按钮：切到下一档。
//
// 档序照 Biu 的 `getPlayModeList`（顺序 → 循环 → 随机 → 单曲 → 回顺序），
// 所以界面上按几下能转回同一档，和 Biu 是同一件事。
func (a *App) CyclePlayMode() { a.SetPlayMode(a.queue.PlayMode().Next()) }

// SetKeepPagesOrderInRandom 设「随机时保持分集顺序」，并记进设置。
func (a *App) SetKeepPagesOrderInRandom(v bool) {
	a.queue.SetKeepPagesOrderInRandom(v)
	a.rememberPlayMode()
	a.invalidate() // 理由同 SetPlayMode：那个开关在浮层里，点完得当场收起来。
}

// rememberPlayMode 把这两个开关落到设置里，下次启动还是这一档。
//
// 存不住不算错，理由和 `rememberAudioQuality` 是同一条：设置层自己有入口能
// 看到「这次改的设置存不住」（见 App.TakeSettingsError），在这儿拦一次只会
// 让换模式这件事本身失败——那比不记住更糟。
func (a *App) rememberPlayMode() {
	mode, keep := a.queue.PlayMode(), a.queue.KeepPagesOrderInRandom()
	_, _ = a.UpdateSettings(func(s *Settings) {
		s.PlayMode = mode
		s.ShouldKeepPagesOrderInRandomPlayMode = keep
	})
}

// onTrackFinished 是「这一首自然放完了」那一条路：按当前的播放模式决定
// 接着放哪首，还是停下来。
//
// ⚠️ **调用它之前必须已经确认「是自然放完」**（见 app.go 的 tick）：
// 暂停、停止、解码失败、断网在 `Playing()` 眼里全都是「不在放了」。判据是
// 播放器的字节真的读到了头（`audio.Player.Ended`）而且没有报错——这一条
// 少一半的后果就是「用户按了暂停，下一首歌自己放了起来」。
//
// 三种结论（`Queue.Advance` 给的）分别落在三件事上：
//
//   - 放下一个：走 `play`，和 `step` 一样是换播放器。放不开就把队列指针退回
//     放完那首（随机那一档尤其需要退回：退回去的路是 `UndoStep`，不是
//     `Move`——随机那一档是从洗牌袋里抽的，退回去要把抽出来的那首放回袋子里）。
//   - 重放当前这首：同一个播放器 `Seek(0)` + `Play`。**不开播放器上的循环**
//     （见 PlayMode 那节的说明）——决定权在这一层，不在音频引擎里。
//   - 停下来：`Seek(0)` + `Pause`，停在开头等用户。这抄的是 Biu 的
//     `audio.currentTime = 0; audio.pause()`。
func (a *App) onTrackFinished() {
	a.mu.Lock()
	p, want := a.player, a.wantPlay
	a.mu.Unlock()
	if p == nil || !want {
		return
	}

	switch d := a.queue.Advance(); d.Kind {
	case AdvanceReplay:
		// MyGo 的 `Play()` 对一个放完的播放器就是从头开始（它自己会 seek(0)），
		// 这里再显式 seek 一次：界面上的进度条读的是 `a.position`，不等解码
		// goroutine 回过劲来的那一帧。
		p.Seek(0)
		p.Play()
		a.mu.Lock()
		a.position, a.playing, a.wantPlay = 0, true, true
		a.stopped = false
		wake := a.wake
		a.mu.Unlock()
		if wake != nil {
			wake()
		}

	case AdvanceStop:
		p.Seek(0)
		p.Pause()
		a.mu.Lock()
		// ⚠️ **wantPlay 不动**。它是「用户按过播放」，不是「播放器此刻在响」。
		// 落成 false 的话两处会跟着坏：
		//
		//   - `ScrubEnd`：拖回中间就不再接着播（「往回拖必须能接着播」是验收项
		//     5，`internal/ui/playbar_test.go` 的 `TestDragToEndDoesNotCrash`
		//     钉着这一句）；
		//   - `tick` 的 `live`：位置会被读，可停下来之后位置恒为 0，白读而已。
		//
		// 而它该表示的「这一串放完了」由 `a.stopped` 记着，系统那边（锁屏上的
		// 播放/停止）也已经报成 stopped。
		a.position, a.playing = 0, false
		a.stopped = true
		wake := a.wake
		a.mu.Unlock()
		if wake != nil {
			wake()
		}

	case AdvancePlay:
		if d.Item.Path == "" && !d.Item.isOnline() {
			a.queue.UndoStep()
			a.setErr(errNoSource.Error())
			return
		}
		if err := a.play(d.Item); err != nil {
			a.queue.UndoStep()
			a.setErr(fmt.Sprintf("接不上下一首：%v", err))
			return
		}
	}

	// 锁屏上的那一首得跟着换（新放的这首、或者这一首从头）。
	a.publishMedia()
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
//
// **这一句是不可重试的**：界面上不给「重试」那一枚键。能重试的错误走
// `setErrText(msg, true)`——`failOnline` 按错误本身判，不靠这里。
func (a *App) setErr(msg string) { a.setErrText(msg, false) }

// setErrText 在播放条上显示一句错误，并记下它**再试一次有没有用**。
//
// 两个值必须一起落：界面上「打不开：……」这句话和「给不给重试那一枚键」
// 是**同一件事的两半**。只落前一半的话，界面上要么给一个按了也没用的
// 重试，要么把「网络断了」说成死路。
func (a *App) setErrText(msg string, retryable bool) {
	a.setErrTextDetail(msg, msg, retryable)
}

// setErrTextDetail 在播放条上显示一句错误，**短的��屏、长的进悬停提示**。
//
// 🔴 **为什么要分两栏**（票 #282 的评审）：播放条上那一行只有 140 像素宽
// （1200 宽的窗口，实测），12 号字放得下十来个字——一句 50 个字的话在屏幕
// 上只剩「打不开：取流…」这样的碎片，那不是「一句能看懂的话」。
//
// 三个值必须一起落：界面上「打不开：……」这句话、「给不给重试那一枚键」
// 和「悬停提示里那一句」是**同一件事的三半**。只落前一半的话，界面上要么
// 给一个按了也没用的重试，要么把「网络断了」说成死路。
func (a *App) setErrTextDetail(msg, detail string, retryable bool) {
	a.mu.Lock()
	a.errText = msg
	a.errDetail = detail
	a.errRetryable = retryable
	wake := a.wake
	a.mu.Unlock()
	// 🔴 **念出去（无障碍）**：播放条上那一行字**读屏软件用户看不见**——他们
	// 不知道歌停了，更不知道是为什么停的。而这一句是「刚刚发生了什么」，
	// 不是「这一枚是什么」。
	//
	// ⚠️ **空话不念**：`a.setErr("")` 是「把上一句错收掉」（见 online.go），
	// 那一句话不该变成一句「播放失败：」。
	//
	// ⚠️ **排在解锁之后**：`Announce` 走的是自己那把锁，而这一段不该在握着
	// `a.mu` 的时候去拿它（那把锁的规矩见 app.go 的 `wakeMu` 那段注释）。
	if msg != "" {
		a.announce("播放失败：" + msg)
	}
	if wake != nil {
		wake()
	}
}

// clearErrLocked 清掉播放条上那一句错误。**必须在持锁时调**。
//
// 🔴 **三个值一起清**：只清 `errText` 而留着 `errRetryable`，界面上就变成
// 「没有错误了，但重试那一枚键还亮着」——而那枚键按下去会重放上一首，
// 用户会以为是自己按错了。
func (a *App) clearErrLocked() {
	a.errText = ""
	a.errDetail = ""
	a.errRetryable = false
}

// Retry 再放一次上一次用户想放的那一首。
//
// 它是给「取流失败（可恢复）」用的：网络抖了、地址过期了这一类再试一次就有
// 可能好，而界面在播放条上给用户一枚「重试」键。
//
// ⚠️ **不可重试的失败界面上不给这一枚键**：B 站说这一条没有音频流的时候，
// 重试一万次还是那一句，而用户按了两次发现还是那样，就不再信这个应用了。
//
// 🔴 **风控和限流也不给这一枚键**（票 #316 / #346）：它是**立刻**再取一次流
// （这里没有退避），所以按下去就是再撞一次墙——风控和限流要等的是时间，不是
// 「再问一次」。那一句文案说了「等一会儿再试」：**过一会儿用户自己再放一次**。
//
// 放的还是**用户刚才按的那一首**（`pending`），不是队列里当前的那一首——
// 取流失败时队列常常已经滚回去了，而用户想重试的是他刚才点的那首。
func (a *App) Retry() error {
	a.mu.Lock()
	it := a.pending
	a.mu.Unlock()
	if !it.isOnline() {
		// ⚠️ **这一句也要答两问**：它跳出来的时候是在 Toast 上，而 Toast
		// 上只有这一句话——用户看到「没有可以重试的那一首」却不知道
		// 该干什么，就等于把问题推回给他。
		return errors.New("没有可以重试的那一首了（它已经不在播放队列里）。换一首放，或者重新搜一次")
	}
	// 同步就能判出来的错（这一条压根没有可播的身份）在这里就报回去，
	// 界面上会把它当成一次新的失败说出来。
	return a.playOnline(it)
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
