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
// ⚠️ `ui.Bitmap` 造出来之后像素就读不回来了（只有 Pixelated / Update / Size
// 三个方法）。所以要缩放、取主色、做模糊占位，都得在 DecodeBitmap 之前做完。
package imgcache

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"
)

// DefaultMaxBytes 是缓存默认的字节上限。Biu 一次能列出几十张封面，64MB 大约
// 相当于 50 张 672×378 的，够一屏用又不至于让歌单一滚就吃掉几百 MB。
const DefaultMaxBytes int64 = 64 << 20

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
	// MaxBytes 是缓存的字节上限，零值是 DefaultMaxBytes。
	MaxBytes int64
	// Dir 是磁盘缓存目录，空就是不落盘。落盘之后同一张图第二次启动不用再下。
	Dir string
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
type Cache struct {
	client   *http.Client
	ua       string
	dir      string
	decodeCh chan job

	clock      Clock
	retryBase  time.Duration
	retryMax   time.Duration
	maxRetries int

	entries  sync.Map // url → *entry，兼作「这个 url 已经有人在下了」的去重表
	inflight chan struct{}

	mu  sync.Mutex
	lru *lru

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

	decodeWG sync.WaitGroup
}

// New 建一个缓存。
func New(opts Options) *Cache {
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = DefaultMaxBytes
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
		dir:        opts.Dir,
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
	if c.dir != "" {
		if err := os.MkdirAll(c.dir, 0o755); err != nil {
			c.dir = "" // 落不了盘就当没配，不影响下载
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
func (c *Cache) Get(url string) State {
	if url == "" {
		return State{}
	}
	v, loaded := c.entries.LoadOrStore(url, &entry{url: url})
	e := v.(*entry)

	c.mu.Lock()
	c.lru.touch(e)
	c.mu.Unlock()

	if !loaded && !c.start(e) {
		c.forget(e)
		return State{Err: ErrClosed}
	}
	return e.state()
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

// Close 停掉在途的下载、排着的重试和解码 worker。已经拿到的图还能继续画。
func (c *Cache) Close() {
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
	bytes    int64
	ctx      context.Context
	cancel   context.CancelFunc
	failures int   // 连续失败了几次（含第一次），成功就归零
	timer    Timer // 排着的下一次重试；nil 表示没排
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

// job 是一张已经下完、等着解码的图。
type job struct {
	e    *entry
	data []byte
}

// fetch 下载一张图：先看磁盘缓存，没有才走网络。在途数限流拿不到名额就一直
// 等，直到轮到自己或者被取消。
func (c *Cache) fetch(e *entry) {
	select {
	case c.inflight <- struct{}{}:
	case <-e.ctx.Done():
		return
	}
	defer func() { <-c.inflight }()

	data, err := c.read(e.ctx, e.url)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			c.finish(e, nil, err)
		}
		return
	}
	select {
	case c.decodeCh <- job{e: e, data: data}:
	case <-e.ctx.Done():
	}
}

// read 拿到这张图的字节。磁盘缓存里有就直接用，没有才下；下了顺手落盘。
func (c *Cache) read(ctx context.Context, url string) ([]byte, error) {
	if path := c.path(url); path != "" {
		if data, err := os.ReadFile(path); err == nil {
			return data, nil
		}
	}

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

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes))
	if err != nil {
		return nil, err
	}
	if path := c.path(url); path != "" {
		_ = os.WriteFile(path, data, 0o644)
	}
	return data, nil
}

// path 是这个 url 在磁盘缓存里的文件名，空表示不落盘。
func (c *Cache) path(url string) string {
	if c.dir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(url))
	return filepath.Join(c.dir, hex.EncodeToString(sum[:16])+".img")
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
			bm, err := ui.DecodeBitmap(j.data)
			if err != nil {
				// 解码不了就是解码不了：同一段字节再解一百次也一样。
				err = &permanentError{err: err}
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
	evicted := c.lru.resize(e, bytesOf(bm))
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
