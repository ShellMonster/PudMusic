// Package imgcache 是网络封面（以及别的网络图片）的缓存。
//
// 一张图分三步走完，每一步之间都用 entry 里的锁隔开：
//
//	view 在主线程   Get() 拿到这一帧该画什么：好了给图，没好给 Loading，失败给 Err
//	后台下载       同一个 url 只会下一次（sync.Map + LoadOrStore），并受在途数限制
//	后台解码       固定几个 worker 的池子，不是每个 url 一个 goroutine
//
// 失败分两类（`isPermanent`）：**永久失败**（404、解码不了）就停在那里，
// 界面一直画失败态；**暂时失败**（网络错误、超时、5xx、429）排一次退避重试，
// 到点之后重新走后台下载那条路（同一组在途槽位和 decode worker），最多
// MaxRetries 次。重试期间状态**仍然是失败态**，不闪回 Loading——`done` 一直
// 立着，只有成功的终态才会把它换成图。
//
// ⚠️ 唤醒：后台填完之后**必须**调注册的回调。不唤醒的话，界面会永远停在占位图
// ——这是实测踩过的坑（只投递不唤醒 = 新状态到了但 0 帧刷新），不是理论风险。
// 有几个窗口注册就通知几个，只通知主窗口的话迷你窗口会停在旧内容。
//
// 落盘那一半（`Options.Dir` 非空才开）：第一次下载成功之后把字节写到应用自己
// 的缓存目录，同一张图第二次启动直接从盘上读，不再打图床。目录有独立的字节
// 上限，按最久未用淘汰；**读盘在后台，写盘在自己的 worker 里**，主线程一次都
// 不碰磁盘（见 `disk.go`）。
//
// ⚠️ `ui.Bitmap` 造出来之后像素就读不回来了（只有 Pixelated / Update / Size
// 三个方法）。所以要缩放、取主色、做模糊占位，都得在 DecodeBitmap 之前做完。
package imgcache

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"
)

// DefaultMaxBytes 是**内存**缓存默认的预算：32 MiB。
// 这一档由人于 2026-10-25 代选，随时可调；启动时 BIU_IMAGE_CACHE_MB 可覆盖它。
// 768×432 的 RGBA 加 mipmap 记账约 1.69 MiB，这份预算约能留 18 张。
//
// ⚠️ **这个数和「存下来的那一张图有多大」是一对**（见 `scale.go`）：
// 按 1920×1080 原图存时一张约 11MB，预算很快就会满。存下来的那一份保持
// 最长边 768（见 `maxStoredSide`），满足常用的 384 DIP 封面在 Retina 上的尺寸。
//
// 预算越小，回滚时越常读盘解码，磁盘未命中才重下。正在显示的图片仍受保护，
// 可暂时超预算，避免「有图 ↔ 占位」反复切换；这不是整个进程的内存上限。
const DefaultMaxBytes int64 = 32 << 20

// DefaultDiskMaxBytes 是**磁盘**缓存默认的字节上限：256MB。
//
// 它和内存那一份是两个预算：盘上存的是压缩后的原始字节（一张 672×378 的 WebP
// 约 30KB），256MB 能放八千多张——按几百张一屏的歌单算，也够很多次启动复用。
// 设上限不是为了省这 256MB，是为了给「一直滚下去」一个确定的终点。
const DefaultDiskMaxBytes int64 = 256 << 20

// visibleFrames 是**画面上正画着的**那几张图能活多久（以帧计）。
//
// 挤缓存（LRU）的时候正画着的那几张是**不许挤**的（票 #358）：挤掉之后它
// 下一帧就退回「正在加载」的占位，于是那一格闪一下。
//
// 为什么是 4 而不是 1：
//
//   - **一帧要跑好几遍 build**（框架最多跑三遍），而「画过」是每一遍都记的，
//     所以按遍数算的话 1 帧就有好几遍；
//   - **两扇窗共用一份缓存**（主窗口和迷你播放器），各自的帧各数各的，
//     而迷你窗口在放歌时每 50ms 醒一次——它数的那些帧也占着这几个名额。
//
// 4 帧在 60fps 下是 60 多毫秒（三四帧画面），够覆盖上面那两样；而再多也没有
// 意思——用户眼睛盯着的就是当前这一帧。
const visibleFrames = 4

// defaultMaxInflight 是同时在途的下载数。B 站图床对同一个 IP 的并发是有限
// 的，歌单一次几十张全放出去会被拒。
const defaultMaxInflight = 6

// maxImageBytes 是一张图能有多大。封面图床给的最大的也就几百 KB，
// 超了说明拿到的不是图。
const maxImageBytes = 8 << 20

// 重试的默认旋钮。1+2+4+8+16 = 31 秒内试五次，之后当作永久失败：
// 网络抖动一两秒就过去了，这个节奏足够；而一张真的不存在的图也不会被
// 一直要下去。
const (
	defaultRetryBase  = time.Second
	defaultRetryMax   = 30 * time.Second
	defaultMaxRetries = 5
	// defaultMaxFailed 是失败记录的条数上限。失败条目没有字节数，LRU 挤不掉
	// 它们，所以得单独设一道上限——列表里滚过几屏坏地址不该把进程撑大。
	defaultMaxFailed = 256
)

// ErrClosed 是缓存已经关掉之后还来取图。
var ErrClosed = errors.New("imgcache: 已关闭")

// Clock 是退避定时用的钟。生产上用真实时间；测试换成假钟，「过一会儿」
// 就不必真的等。
type Clock interface {
	// AfterFunc 在 d 之后调 f，返回一个可以停掉的 Timer。
	AfterFunc(d time.Duration, f func()) Timer
}

// Timer 是 Clock 排下的一个定时器。
type Timer interface {
	// Stop 停掉定时器。已经触发过就返回 false。
	Stop() bool
}

// systemClock 是 Clock 的真实时间实现。
type systemClock struct{}

func (systemClock) AfterFunc(d time.Duration, f func()) Timer { return time.AfterFunc(d, f) }

// Options 是建缓存时能调的旋钮。零值就是推荐值。
type Options struct {
	// MaxBytes 是**内存**缓存预算；正值优先，零值读 BIU_IMAGE_CACHE_MB，
	// 未配置或配置无效时用 DefaultMaxBytes。正在显示的图片仍受保护，可超预算。
	MaxBytes int64
	// Dir 是磁盘缓存目录，空就是不落盘——零值就是「全在内存里」，和以前一样。
	//
	// 落盘之后同一张图第二次启动不用再下。⚠️ 调用方要给**应用自己的缓存路径
	// 下的一个专用子目录**（`mygo.App.Path(mygo.PathCache)` 底下），不要指向
	// 用户文件目录：这个目录会被整个用来做缓存，超出上限的文件会被删掉。
	Dir string
	// DiskMaxBytes 是磁盘缓存的字节上限，零值是 DefaultDiskMaxBytes。
	// 只在 Dir 非空时有意义。
	DiskMaxBytes int64
	// MaxInflight 是同时在途的下载数，零值是 6。
	MaxInflight int
	// Decoders 是解码 worker 数，零值是 min(核数, 4)。
	Decoders int
	// Client 是下载用的 HTTP 客户端，零值用一个 15 秒超时的。
	Client *http.Client
	// UserAgent 是请求头里的 UA，B 站图床会拒空的。
	UserAgent string

	// RetryBase 是第一次暂时失败之后等多久重试，零值 1s；之后每次翻倍。
	RetryBase time.Duration
	// RetryMax 是退避间隔的封顶，零值 30s。
	RetryMax time.Duration
	// MaxRetries 是一张图最多自动重试几次，零值 5；负数表示不重试。
	MaxRetries int
	// MaxFailed 是失败记录的条数上限，零值 256。到上限之后最老的一条会被
	// 忘掉（下次再取会从头开始）。
	MaxFailed int
	// Clock 是退避定时用的钟，零值用真实时间。测试传假钟推进时间。
	Clock Clock
}

// State 是一张图这一帧的状态。view 照它画：Bitmap 有就画图，Loading 画占位，
// Err 非空画失败态。
type State struct {
	Bitmap  *ui.Bitmap
	Loading bool
	Err     error
}

// Ready 报这张图能不能画。
func (s State) Ready() bool { return s.Bitmap != nil }

// Cache 是图片缓存。整个界面共用一个，别每个页面建一个。
//
// ⚠️ **用着它的几方要各自 Retain / Close**（引用计数）：迷你窗口借的是主窗口
// 那一份（一个磁盘缓存目录只能有一本账），而这两扇窗的寿命不一样。谁最后
// 放开谁就关。
type Cache struct {
	client *http.Client
	ua     string
	// disk 是落盘那一半，nil = 不落盘（Options.Dir 为空）。
	disk     *diskCache
	decodeCh chan job

	clock      Clock
	retryBase  time.Duration
	retryMax   time.Duration
	maxRetries int

	entries  sync.Map // url → *entry，兼作「这个 url 已经有人在下了」的去重表
	inflight chan struct{}

	mu  sync.Mutex
	lru *lru

	// frame 是**画到第几帧**了，0 = 还没开始数（`BeginFrame` 一次都没调过）。
	//
	// ⚠️ **后台线程要读它**（挤缓存的那一处在解码 worker 上），所以它是原子的。
	frame atomic.Uint64
	// frameToken 是上一帧的时刻，用来判「这是不是新的一帧」。
	//
	// ⚠️ **只有界面线程动它**（`BeginFrame` 只在 view 里调），所以不用锁。
	frameToken time.Time

	// failed 是失败条目的账：失败不占字节，LRU 挤不掉，所以单独有一道上限
	// （maxFailed）。failOrder 老的在前，超限时从老的开始忘。
	failed    map[*entry]*list.Element
	failOrder *list.List
	maxFailed int

	wakeMu sync.Mutex
	wakes  []func()

	// lifeWG 数着后台的下载 goroutine。start/cancelAll 拿 lifeMu，所以
	// 「加一个」和「等所有」不会撞上（sync.WaitGroup 复用踩过这个坑）。
	lifeMu sync.Mutex
	lifeWG sync.WaitGroup
	ctx    context.Context
	stop   context.CancelFunc
	closed atomic.Bool

	// refs 是**用着这份缓存的人数**。建缓存算一份（`New`），
	// `Retain` 再加一份，`Close` 放开一份——真的关掉要等最后一份放开。
	//
	// 为什么要有它：迷你窗口借的是主窗口那份封面缓存（一个磁盘缓存目录只能有
	// 一本账），而这两扇窗的寿命不一样。没有引用计数的话，主窗口那个外壳先
	// 收尾（`Shell.Close` → `images.Close`）就会把迷你那份一起带走——迷你之后
	// 每要一张图都是 `ErrClosed` 永久失败态，而界面上的症状只是「图不出来」。
	refs atomic.Int64

	decodeWG sync.WaitGroup
}

// New 建一个缓存。
func New(opts Options) *Cache {
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = configuredMaxBytes()
	}
	if opts.DiskMaxBytes <= 0 {
		opts.DiskMaxBytes = DefaultDiskMaxBytes
	}
	if opts.MaxInflight <= 0 {
		opts.MaxInflight = defaultMaxInflight
	}
	if opts.Decoders <= 0 {
		opts.Decoders = min(runtime.NumCPU(), 4)
	}
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: 15 * time.Second}
	}
	if opts.UserAgent == "" {
		opts.UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	}
	if opts.RetryBase <= 0 {
		opts.RetryBase = defaultRetryBase
	}
	if opts.RetryMax <= 0 {
		opts.RetryMax = defaultRetryMax
	}
	if opts.MaxRetries == 0 {
		opts.MaxRetries = defaultMaxRetries
	} else if opts.MaxRetries < 0 {
		opts.MaxRetries = 0
	}
	if opts.MaxFailed <= 0 {
		opts.MaxFailed = defaultMaxFailed
	}
	if opts.Clock == nil {
		opts.Clock = systemClock{}
	}

	c := &Cache{
		client:     opts.Client,
		ua:         opts.UserAgent,
		lru:        newLRU(opts.MaxBytes),
		decodeCh:   make(chan job, opts.Decoders*4),
		inflight:   make(chan struct{}, opts.MaxInflight),
		clock:      opts.Clock,
		retryBase:  opts.RetryBase,
		retryMax:   opts.RetryMax,
		maxRetries: opts.MaxRetries,
		maxFailed:  opts.MaxFailed,
		failed:     make(map[*entry]*list.Element),
		failOrder:  list.New(),
	}
	c.ctx, c.stop = context.WithCancel(context.Background())
	c.refs.Store(1) // 建缓存的人算一份使用者
	if opts.Dir != "" {
		if err := os.MkdirAll(opts.Dir, 0o755); err == nil {
			// 目录建好了才算「要落盘」。建不了就当没配，封面照常从网上下来。
			c.disk = newDiskCache(opts.Dir, opts.DiskMaxBytes)
		}
	}
	for range opts.Decoders {
		c.decodeWG.Add(1)
		go c.decodeWorker()
	}
	return c
}

// AddWake 注册一路「有图好了，叫我重画」的回调，返回注销函数。
//
// 每个窗口注册一次。有几个窗口就通知几个 —— 只通知主窗口的话，迷你窗口会
// 一直停在旧内容（实测）。回调可能从任何 goroutine 调。
func (c *Cache) AddWake(fn func()) (remove func()) {
	c.wakeMu.Lock()
	id := len(c.wakes)
	c.wakes = append(c.wakes, fn)
	c.wakeMu.Unlock()
	return func() {
		c.wakeMu.Lock()
		if id < len(c.wakes) {
			c.wakes[id] = nil
		}
		c.wakeMu.Unlock()
	}
}

// notify 叫醒所有注册过的窗口。任何 goroutine 都能调。
func (c *Cache) notify() {
	c.wakeMu.Lock()
	fns := slices.Clone(c.wakes)
	c.wakeMu.Unlock()
	for _, fn := range fns {
		if fn != nil {
			fn()
		}
	}
}

// Get 返回 url 这一帧该画什么。第一次见这个 url 就会起一次后台下载，
// 同一个 url 重复调多少次都只下一次。
//
// 暂时失败排着退避重试时，这里照样只报失败态：Get 自己**从不**发起请求，
// 重试由后台定时器到点后重新入队。所以滚列表时一帧里掉多少张失败封面，
// 这一帧都不会多做一次网络或磁盘 I/O。
//
// ⚠️ **每一次 Get 都是「这一张正画在画面上」**：挤缓存时正画着的那几张不许
// 挤（票 #358，理由见 `entry.drawn`）。
func (c *Cache) Get(url string) State {
	if url == "" {
		return State{}
	}
	v, loaded := c.entries.LoadOrStore(url, &entry{url: url})
	e := v.(*entry)

	c.mu.Lock()
	c.lru.touch(e)
	// 记一笔「这一张这一帧画着」：挤缓存时正画着的那几张不许挤（票 #358，
	// 理由见 `entry.drawn`）。
	e.drawn = c.frame.Load()
	c.mu.Unlock()

	if !loaded && !c.start(e) {
		c.forget(e)
		return State{Err: ErrClosed}
	}
	return e.state()
}

// BeginFrame 告诉缓存「新的一帧开始了」。
//
// now 是**这一帧的时刻**（`c.Now()`）：同一帧的几次 build pass 给的是同一个
// 时刻，所以一帧只算一次——框架一帧最多跑三遍 view，三遍都调 BeginFrame
// 的话，帧号会多跳两跳，「画面上正画着」那几个名额就不够了。
//
// # 为什么要有它
//
// 挤缓存（LRU）不能挤掉**画面上正画着**的那一张：挤掉之后它下一帧就退回
// 「正在加载」的占位，于是那一格闪一下（票 #358：开队列抽屉的时候，中间
// 那一列的封面来回闪——抽屉里那十几行第一次露出来时全都要封面，字节上限
// 立刻超了，挤的循环一路挤到画面上正画着的那几张）。
//
// 有了帧号，缓存就知道「哪几张是这一帧画着的」，挤的时候跳过它们。
//
// # 不调会怎样
//
// 帧号停在 0，**挤的行为和从前一模一样**（库自己的老测试、还没接上这一句的
// 调用方都是这样）。所以这一句是「加了更好」而不是「必须加」。
//
// # ⚠️ **缓存是 nil 时这一句什么都不做**
//
// 真窗口上缓存从来不是 nil（`Shell` 一定建一个），但**测试会把它摘掉**：
// `downloadlist_ops_test.go` 的 `TestOneBrokenCellDoesNotBlankOutThePage`
// 摘掉它是为了让「有封面的那一格」崩在**格子里**（而不是崩在整页上）。
//
// 而这一句在 `Shell.viewMain` 里、**每一帧**都调——缓存是 nil 的话它会**先**
// 把整页带崩（nil 接收者上的字段访问），于是那一格还没画就被页级那道 Guard
// 接走了，而那条用例要的正是「一格崩、整页不崩」。
//
// 所以这里先判一次 nil：**这一句是「让挤缓存更准」的，不是「这一句不许崩」**。
func (c *Cache) BeginFrame(now time.Time) {
	if c == nil {
		return
	}
	if c.frameToken.Equal(now) {
		return
	}
	c.frameToken = now
	c.frame.Add(1)
}

// SetMaxBytes 改这份缓存的字节上限。
//
// 改小之后**下一次记账**（有一张图解码完）就会按新的上限挤——已经记好的账
// 不动，所以它不是「立刻把缓存缩到那个数」。
// n <= 0 与 New(Options{}) 一致，重新读取环境配置，未配置则用默认值。
//
// 测试把预算调低，就能复现票 #358 那种「字节不够」的局面，
// 不必为填满默认预算生成几十兆封面。
func (c *Cache) SetMaxBytes(n int64) {
	if n <= 0 {
		n = configuredMaxBytes()
	}
	c.mu.Lock()
	c.lru.max = n
	c.mu.Unlock()
}

// start 起一次后台下载。已经关掉了就返回 false。由 LoadOrStore 的赢家
// 或一次到期的重试调（重试和第一次下载走的是同一条路：同一组在途槽位、
// 同一个下载 → 解码流程）。
func (c *Cache) start(e *entry) bool {
	c.lifeMu.Lock()
	defer c.lifeMu.Unlock()
	if c.closed.Load() {
		return false
	}
	ctx, cancel := context.WithCancel(c.ctx)
	e.mu.Lock()
	if e.cancel != nil {
		// 上一轮（如果有）的 context 收掉，别让它挂在父 context 上。
		e.cancel()
	}
	e.ctx, e.cancel = ctx, cancel
	e.mu.Unlock()

	c.lifeWG.Add(1)
	go func() {
		defer c.lifeWG.Done()
		c.fetch(e)
	}()
	return true
}

// Cancel 停掉 url 正在进行的下载并把它从缓存里拿掉。下次再 Get 会重新开始。
//
// 真的取消：请求带的就是这个 context，服务端收得到。
func (c *Cache) Cancel(url string) {
	if v, ok := c.entries.LoadAndDelete(url); ok {
		e := v.(*entry)
		c.forget(e)
		c.dropFailure(e)
		e.discard()
	}
}

// Retain 多一份使用者。借别人那份缓存时调它——**借的那一份活得至少和借它的
// 人一样久**，所以借之前必须 Retain。
//
// 已经关掉的缓存不用 Retain（借了也没用）。
func (c *Cache) Retain() {
	for {
		n := c.refs.Load()
		if n <= 0 {
			return // 已经关掉了
		}
		if c.refs.CompareAndSwap(n, n+1) {
			return
		}
	}
}

// Close 放开自己那一份使用权；**最后一份放开时才真的关掉**。
//
// ⚠️ 以前 Close 是「立刻关掉」。现在是引用计数，因为迷你窗口借的是主窗口那份
// 封面缓存（一个磁盘缓存目录只能有一本账），而这两扇窗的寿命不一样：主窗口
// 那个外壳先收尾就把迷你那份一起带走的话，迷你之后每要一张图都是 `ErrClosed`
// 永久失败态——界面上的症状只是「图不出来」，没有一句报错。
//
// 可以重复调：多调一次不多减（引用数到 0 之后就是空操作）。
func (c *Cache) Close() {
	for {
		n := c.refs.Load()
		if n <= 0 {
			return // 已经关过了
		}
		if !c.refs.CompareAndSwap(n, n-1) {
			continue
		}
		if n > 1 {
			return // 还有别人用着
		}
		break
	}

	c.lifeMu.Lock()
	if c.closed.Load() {
		c.lifeMu.Unlock()
		return
	}
	c.closed.Store(true)
	c.stop() // 派生自 c.ctx 的每一个请求 context 一起取消
	c.lifeMu.Unlock()

	// 排着的重试也一起收掉：不然关了缓存之后还会在后台醒过来。
	c.entries.Range(func(_, v any) bool {
		v.(*entry).stopTimer()
		return true
	})
	c.lifeWG.Wait()
	c.decodeWG.Wait()
	// 排着队的写盘在这里落完再走：落盘有一份延迟，不 Flush 的话「刚下完就关」
	// 那一份会丢，而「重启之后不用再下」正是它要保证的。
	if c.disk != nil {
		c.disk.close()
	}
}

// forget 把 e 从 LRU 表里拿掉并记账归零。
func (c *Cache) forget(e *entry) {
	c.mu.Lock()
	c.lru.drop(e)
	c.mu.Unlock()
}

// UsedBytes 是现在缓存里占着多少字节。设置页和测试用它看预算。
func (c *Cache) UsedBytes() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lru.used
}

// entry 是一次图片加载的槽位：在主线程建，由后台 goroutine 填。
type entry struct {
	url string

	mu       sync.Mutex
	bm       *ui.Bitmap
	err      error
	done     bool // 已经落到过终态；暂时失败时重试会排上，但 done 不撤
	dropped  bool // 被取消或挤掉了，结果回来也没人要
	skipDisk bool // 盘上那份解不开，这一张从此不再从盘上读（见 decodeWorker）
	bytes    int64
	ctx      context.Context
	cancel   context.CancelFunc
	failures int   // 连续失败了几次（含第一次），成功就归零
	timer    Timer // 排着的下一次重试；nil 表示没排

	// drawn 是**最后一次被画到是在第几帧**（见 `Cache.BeginFrame`）。
	//
	// 挤缓存的时候要认出「画面上正画着」的那几张——挤掉它们，用户看见的就是
	// 那一格闪一下（票 #358）。
	//
	// ⚠️ **零是「没被画过」**：帧号从 1 起数（`BeginFrame` 第一次调用之后），
	// 所以零和「第一帧之前」是同一件事——那时候没有「画面上」这一说，挤的
	// 行为和从前一模一样。
	drawn uint64
}

// onScreen 报这一张**是不是画面上正画着的**（frame 是这一帧的号）。
//
// ⚠️ **帧号还没开始数的时候（frame == 0）一律报 false**：那是没有 `BeginFrame`
// 的用法（库自己的老测试、别的调用方），挤的行为和从前一样。
func (e *entry) onScreen(frame uint64) bool {
	if frame == 0 || e.drawn == 0 {
		return false
	}
	return frame >= e.drawn && frame-e.drawn <= visibleFrames
}

// isDropped 报这个条目是不是已经被取消/挤掉了。晚到的结果不用再管它。
func (e *entry) isDropped() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.dropped
}

// diskSkipped 报这一张已经不再从盘上读了。
func (e *entry) diskSkipped() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.skipDisk
}

// skipDiskFromNowOn 记下「这一张不再从盘上读」。
//
// 为什么要有这一格：盘上那份解不开的时候，正常的做法是「删掉它，回去下一次
// 真的」。可是**删不掉**的时候（目录只读、文件被占着），「读 → 解码失败 →
// 删 → 再读」就是一个没有出口的循环：一个解码 worker 100% 占着 CPU，那张图
// 永远停在「加载中」（既不出图也不报失败），而网络一次都没被试过。
//
// 所以：盘上那份一坏，这一张在本次运行内就**不再从盘上读**。下一次读盘的机会
// 是下次启动——那时那一份要么被清掉了，要么又坏一次（而这一次同样会走网络）。
func (e *entry) skipDiskFromNowOn() {
	e.mu.Lock()
	e.skipDisk = true
	e.mu.Unlock()
}

func (e *entry) state() State {
	e.mu.Lock()
	defer e.mu.Unlock()
	switch {
	case e.bm != nil:
		return State{Bitmap: e.bm}
	case e.done:
		// 暂时失败排着重试时也走这一支：界面上还是失败态，不闪回 Loading。
		return State{Err: e.err}
	default:
		return State{Loading: true}
	}
}

// stopTimer 停掉排着的重试（如果有）。
func (e *entry) stopTimer() {
	e.mu.Lock()
	timer := e.timer
	e.timer = nil
	e.mu.Unlock()
	if timer != nil {
		timer.Stop()
	}
}

// job 是一张已经拿到字节、等着解码的图。
type job struct {
	e    *entry
	data []byte
	// fromDisk 报这批字节是从盘上读的。盘上那份解不开时不能当成「这张图就是
	// 坏的」——先删掉它，回去下一次真的（见 decodeWorker）。
	fromDisk bool
}

// fetch 拿到一张图的字节：先看磁盘缓存，没有才走网络。
//
// ⚠️ **读盘不占在途名额**：名额是给网络下载的，盘上命中根本不用打图床。要是
// 先抢名额再读盘，重启之后一屏几十张封面会跟正在下载的那几条一起排队。
func (c *Cache) fetch(e *entry) {
	name := c.diskName(e.url)
	fromDisk := false
	var data []byte
	// ⚠️ 盘上那份坏过就不要再读了（`skipDisk`）：删不掉的时候「读→失败→再读」
	// 是一个没有出口的循环，见 `entry.skipDiskFromNowOn`。
	if name != "" && !e.diskSkipped() {
		data, fromDisk = c.disk.open(name)
	}
	if !fromDisk {
		select {
		case c.inflight <- struct{}{}:
		case <-e.ctx.Done():
			return
		}
		defer func() { <-c.inflight }()

		var err error
		data, err = c.download(e.ctx, e.url)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				c.finish(e, nil, err)
			}
			return
		}
	}
	select {
	case c.decodeCh <- job{e: e, data: data, fromDisk: fromDisk}:
	case <-e.ctx.Done():
	}
}

// download 从网络拿这张图的字节。
func (c *Cache) download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		// 地址本身不合法：再试多少次也是同一个结果。
		return nil, &permanentError{err: err}
	}
	// B 站图床要 Referer，没有会被拒。
	req.Header.Set("Referer", "https://www.bilibili.com/")
	req.Header.Set("User-Agent", c.ua)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &statusError{url: url, code: resp.StatusCode}
	}

	return io.ReadAll(io.LimitReader(resp.Body, maxImageBytes))
}

// Source 拿这张图的**原始字节**（下回来的那份，不是解码缩放之后的那份）。
//
// # 为什么要有这一条
//
// `ui.Bitmap` 的像素**读不回来**（`mygo/ui/bitmap.go`：只有 Pixelated /
// Update / Size 三个方法），而界面上的封面正是拿字节解出来的那个 Bitmap。
// 所以要把封面**原样**交出去（比如放进剪贴板），只有两条路：
//
//	① 重新下一遍 —— 浪费一次网络往返，还可能拿到的和界面上那一张不是同一份
//	② 留住原始字节 —— 就是这一条
//
// # 它怎么找字节
//
// 先问磁盘缓存（和 `fetch` 同一个顺序）：同一张图启动第二次不用再打图床。
// 盘上没有就下一次——**和 `fetch` 走的是同一个 `download`**（Referer、UA、
// 字节上限都在那儿）。
//
// ⚠️ **它不占在途名额**（和 `fetch` 同一个理由）：这一条是**用户明确要**的
// 一次性动作，不是一次滚列表时几十张图一起要。
//
// ⚠️ **它不记进 LRU**：内存缓存记的是「解码之后占多少字节」，而这一条给的
// 是原始字节（压缩过的），两者不同量纲，混进同一本账里上限就废了。
//
// ⚠️ **不落盘的那一份不缓存**：拿到的字节直接交给调用方，调用方用完就扔。
func (c *Cache) Source(url string) ([]byte, error) {
	if url == "" {
		return nil, errors.New("imgcache: 没有地址")
	}
	if c.closed.Load() {
		return nil, ErrClosed
	}
	// 盘上那份：和 `fetch` 一样先问盘（读盘只该由后台 goroutine 调，这一处
	// 的调用方在后台，见界面层那一处注释）。
	if name := c.diskName(url); name != "" {
		if data, ok := c.disk.open(name); ok {
			return data, nil
		}
	}
	data, err := c.download(c.ctx, url)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// SourceImage 拿这张图**摆正之后**的那一份（`image.Image`）。
//
// # 为什么要有这一条
//
// 要把封面**原样交出去**（比如放进剪贴板）只有两条路：
//
//	① 重新下一遍 —— 浪费一次网络往返，还可能拿到的和界面上那一张不是同一份
//	② 留住原始字节 —— `Source` 就是这一条
//
// 而从字节到「一张能直接编码的图」中间还差一步：**EXIF 摆正**。界面上摆的是
// 摆正后的那一张（框架的 `ui.DecodeBitmap` 按 EXIF 转），而从原始字节重新解的
// 时候得**自己转**，否则交出去的是一张横躺着的图。
//
// ⚠️ **摆正这一段放在这个包里**：EXIF 的解析和四种格式的解码器注册都在这儿
// （`scale.go` 顶上那几个 `_ "image/..."` 导入），在外面重写一遍就是两份会走偏
// 的实现。
//
// ⚠️ **不缩放**：这一条给的是**原图**，不是存进缓存的那一份（那个缩到最长边
// 768）。剪贴板要的是原图。
//
// ⚠️ **只该由后台 goroutine 调**：读盘和下载都在这一条里，而读盘只该由后台调
// （见 `disk.go` 的线程模型）。
func (c *Cache) SourceImage(url string) (image.Image, error) {
	raw, err := c.Source(url)
	if err != nil {
		return nil, err
	}
	return decodeOriented(raw)
}

// diskName 是这个 url 在磁盘缓存里的文件名，空表示不落盘。
func (c *Cache) diskName(url string) string {
	if c.disk == nil {
		return ""
	}
	return diskNameFor(url)
}

// decodeWorker 是解码池的一个 worker。
//
// 解码是纯 CPU 的（五种格式全是软解，GPU 加速的只是绘制），放后台放心。
// 池子固定大小：一屏几十张图要是一张图一个 goroutine，CPU 会被抢干净。
func (c *Cache) decodeWorker() {
	defer c.decodeWG.Done()
	for {
		select {
		case j := <-c.decodeCh:
			bm, err := decodeStored(j.data)
			if err != nil {
				if j.fromDisk && !j.e.isDropped() && !c.closed.Load() {
					// 盘上那份解不开（上一个版本留下的、或者被外部写坏了）：
					// 删掉它，当作没命中，回去下一次真的。不能把坏字节当成
					// 「这张图就是坏的」——那会让一张好封面从此显示失败。
					//
					// ⚠️ **删不掉也不能再读**：目录只读的时候「读 → 解码失败 →
					// 删（失败）→ 再读」是一个没有出口的循环——一个解码 worker
					// 100% 占着 CPU，那张图永远停在「加载中」（既不出图也不报
					// 失败），而网络一次都没被试过。所以这一张从此刻起不再从盘
					// 上读（`skipDisk`），下一次直接走网络。
					if rmErr := c.disk.remove(c.diskName(j.e.url)); rmErr != nil {
						log.Printf("biu: 删不掉磁盘缓存里那份坏封面（%s）：%v", j.e.url, rmErr)
					}
					j.e.skipDiskFromNowOn()
					if c.start(j.e) {
						continue
					}
				}
				// 网络拿回来的字节解不开：同一段字节再解一百次也一样。
				err = &permanentError{err: err}
			} else {
				// 只把**解出来过**的字节写盘：下回来的东西万一是错误页或者
				// 截断的响应，写进去就成了「每次启动都先撞一次坏缓存」。
				// 写盘排在 finish 之前：状态变成「有图」时这一笔已经在队列里，
				// Close 会把它落完（测试靠这个判「重启之后不用再下」）。
				if !j.fromDisk && c.disk != nil && !c.closed.Load() {
					c.disk.write(c.diskName(j.e.url), j.data)
				}
			}
			c.finish(j.e, bm, err)
		case <-c.ctx.Done():
			return
		}
	}
}

// finish 把一次尝试的结果落到 entry 上，记账，唤醒所有窗口。
//
// 失败分两条路：永久失败就此停下；暂时失败排一次退避重试。重试期间
// `done` 不撤——界面画的仍然是失败态，到点之后由 retry 重新走后台下载。
func (c *Cache) finish(e *entry, bm *ui.Bitmap, err error) {
	e.mu.Lock()
	if e.dropped || (e.done && e.err == nil) {
		// 被取消/挤掉了，或者已经有图了。别把晚到的结果塞回去。
		e.mu.Unlock()
		return
	}

	if bm != nil {
		e.bm, e.err, e.done = bm, nil, true
		e.failures = 0
		e.mu.Unlock()

		c.dropFailure(e)
		c.account(e, bm)
		c.notify()
		return
	}

	e.err, e.done = err, true
	e.failures++
	failures := e.failures
	e.mu.Unlock()

	// 先把失败记进账，再排重试：定时器到点可能立刻跑起来（假钟就是这么
	// 测的），那时条目必须已经在失败表里。
	c.noteFailure(e)

	if !isPermanent(err) && failures <= c.maxRetries && !c.closed.Load() {
		delay := c.backoff(failures)
		e.mu.Lock()
		if !e.dropped {
			e.timer = c.clock.AfterFunc(delay, func() { c.retry(e) })
		}
		e.mu.Unlock()
	}
	c.notify()
}

// account 把一张图的字节数记进 LRU，并把超预算的挤掉。
func (c *Cache) account(e *entry, bm *ui.Bitmap) {
	c.mu.Lock()
	evicted := c.lru.resize(e, bytesOf(bm), c.frame.Load())
	for _, victim := range evicted {
		c.entries.CompareAndDelete(victim.url, victim)
	}
	c.mu.Unlock()

	for _, victim := range evicted {
		victim.discard()
	}
}

// backoff 是第 failures 次失败之后等多久：base 起步，每次翻倍，封顶 retryMax。
func (c *Cache) backoff(failures int) time.Duration {
	if c.retryBase >= c.retryMax {
		return c.retryMax
	}
	d := c.retryBase
	for i := 1; i < failures; i++ {
		if d >= c.retryMax/2 {
			return c.retryMax
		}
		d *= 2
	}
	return d
}

// retry 是一次到期的重试：重新走后台下载那条路，和第一次加载没有区别
// ——在途名额、下载 goroutine、解码 worker 都是同一组。
func (c *Cache) retry(e *entry) {
	e.mu.Lock()
	if e.dropped || e.bm != nil {
		e.mu.Unlock()
		return
	}
	e.timer = nil
	e.mu.Unlock()

	c.start(e)
}

// noteFailure 把一次失败记进失败表，并守住条数上限。
//
// 失败条目没有字节数，LRU 永远不会挤掉它们；不设上限的话，列表里滚过几屏
// 坏地址就会一直留在表里。超限时把最老的一条整个忘掉——下次再 Get 会当成
// 第一次加载重新来。
func (c *Cache) noteFailure(e *entry) {
	c.mu.Lock()
	if _, ok := c.failed[e]; !ok {
		c.failed[e] = c.failOrder.PushBack(e)
	}
	var victim *entry
	if len(c.failed) > c.maxFailed {
		el := c.failOrder.Front()
		victim = el.Value.(*entry)
		c.failOrder.Remove(el)
		delete(c.failed, victim)
		// 失败条目是 0 字节，LRU 的字节上限挤不掉它——从 URL 表里也要删掉，
		// 不然「失败记录有上限」只限住了 failed，LRU 的 byURL 照样一直长。
		c.lru.drop(victim)
		c.entries.CompareAndDelete(victim.url, victim)
	}
	c.mu.Unlock()

	if victim != nil {
		victim.discard()
	}
}

// dropFailure 把 e 从失败表里摘掉（重试成功或者被取消了）。
func (c *Cache) dropFailure(e *entry) {
	c.mu.Lock()
	if el, ok := c.failed[e]; ok {
		c.failOrder.Remove(el)
		delete(c.failed, e)
	}
	c.mu.Unlock()
}

// bytesOf 是一张图解码之后占多少字节：RGBA 加上 mipmap 链（反复减半，约 1/3）。
func bytesOf(bm *ui.Bitmap) int64 {
	if bm == nil {
		return 0
	}
	w, h := bm.Size()
	px := int64(w) * int64(h) * 4
	return px + px/3
}

// discard 标记一个 entry 的结果没人要了，晚到的解码结果直接扔掉，
// 排着的重试也停掉。
func (e *entry) discard() {
	e.mu.Lock()
	e.dropped = true
	cancel := e.cancel
	timer := e.timer
	e.timer = nil
	e.mu.Unlock()
	if timer != nil {
		timer.Stop()
	}
	if cancel != nil {
		cancel()
	}
}

// statusError 是非 200 的响应。重不重试看状态码：4xx 里除了 408/429 都是
// 「这张图就是没有」，5xx 和限流是「等会儿再来」。
type statusError struct {
	url  string
	code int
}

func (e *statusError) Error() string { return fmt.Sprintf("imgcache: %s HTTP %d", e.url, e.code) }

// transient 报这个状态码值不值得再试。
func (e *statusError) transient() bool {
	switch e.code {
	case http.StatusRequestTimeout, http.StatusTooManyRequests:
		return true
	}
	return e.code >= 500
}

// permanentError 是一个「再试也没用」的失败（解码不了、地址不合法）。
type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// isPermanent 报一次失败要不要就此停下。认不出来的按暂时失败处理：
// 网络那一层的错误（断网、超时、连接被掐）都没有自己的类型，而它们
// 正是重试要救的那一类。
func isPermanent(err error) bool {
	var pe *permanentError
	if errors.As(err, &pe) {
		return true
	}
	var se *statusError
	if errors.As(err, &se) {
		return !se.transient()
	}
	return false
}
