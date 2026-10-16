// Package imgcache 是网络封面（以及别的网络图片）的缓存。
//
// 一张图分三步走完，每一步之间都用 entry 里的锁隔开：
//
//	view 在主线程   Get() 拿到这一帧该画什么：好了给图，没好给 Loading，失败给 Err
//	后台下载       同一个 url 只会下一次（sync.Map + LoadOrStore），并受在途数限制
//	后台解码       固定几个 worker 的池子，不是每个 url 一个 goroutine
//
// ⚠️ 唤醒：后台填完之后**必须**调注册的回调。不唤醒的话，界面会永远停在占位图
// ——这是实测踩过的坑（只投递不唤醒 = 新状态到了但 0 帧刷新），不是理论风险。
// 有几个窗口注册就通知几个，只通知主窗口的话迷你窗口会停在旧内容。
//
// ⚠️ `ui.Bitmap` 造出来之后像素就读不回来了（只有 Pixelated / Update / Size
// 三个方法）。所以要缩放、取主色、做模糊占位，都得在 DecodeBitmap 之前做完。
package imgcache

import (
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

// ErrClosed 是缓存已经关掉之后还来取图。
var ErrClosed = errors.New("imgcache: 已关闭")

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

	entries  sync.Map // url → *entry，兼作「这个 url 已经有人在下了」的去重表
	inflight chan struct{}

	mu  sync.Mutex
	lru *lru

	wakeMu sync.Mutex
	wakes  []func()

	// lifeWG 数着后台的下载 goroutine。start/cancelAll 拿 lifeMu，所以
	// 「加一个」和「等所有」不会撞上（sync.WaitGroup 复用踩过这个坑）。
	lifeMu sync.Mutex
	lifeWG sync.WaitGroup
	ctx    context.Context
	stop   context.CancelFunc

	decodeWG sync.WaitGroup
	closed   bool
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

	c := &Cache{
		client:   opts.Client,
		ua:       opts.UserAgent,
		dir:      opts.Dir,
		lru:      newLRU(opts.MaxBytes),
		decodeCh: make(chan job, opts.Decoders*4),
		inflight: make(chan struct{}, opts.MaxInflight),
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

// start 起一次后台下载。已经关掉了就返回 false。只会由 LoadOrStore 的赢家调。
func (c *Cache) start(e *entry) bool {
	c.lifeMu.Lock()
	defer c.lifeMu.Unlock()
	if c.closed {
		return false
	}
	ctx, cancel := context.WithCancel(c.ctx)
	e.mu.Lock()
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
		e.cancelNow()
	}
}

// Close 停掉在途的下载和解码 worker。已经拿到的图还能继续画。
func (c *Cache) Close() {
	c.lifeMu.Lock()
	if c.closed {
		c.lifeMu.Unlock()
		return
	}
	c.closed = true
	c.stop() // 派生自 c.ctx 的每一个请求 context 一起取消
	c.lifeMu.Unlock()

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

	mu      sync.Mutex
	bm      *ui.Bitmap
	err     error
	done    bool // 终态：成功或失败，不再变化
	dropped bool // 被取消或挤掉了，结果回来也没人要
	bytes   int64
	ctx     context.Context
	cancel  context.CancelFunc
}

func (e *entry) state() State {
	e.mu.Lock()
	defer e.mu.Unlock()
	switch {
	case e.bm != nil:
		return State{Bitmap: e.bm}
	case e.done:
		return State{Err: e.err}
	default:
		return State{Loading: true}
	}
}

func (e *entry) cancelNow() {
	e.mu.Lock()
	cancel := e.cancel
	e.mu.Unlock()
	if cancel != nil {
		cancel()
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
		return nil, err
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
		return nil, fmt.Errorf("imgcache: %s HTTP %d", url, resp.StatusCode)
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
			c.finish(j.e, bm, err)
		case <-c.ctx.Done():
			return
		}
	}
}

// finish 把一次加载落到终态，记账，唤醒所有窗口。
func (c *Cache) finish(e *entry, bm *ui.Bitmap, err error) {
	e.mu.Lock()
	if e.dropped || e.done {
		// 已经被取消/挤掉了，或者已经有结果了。别把晚到的图塞回去。
		e.mu.Unlock()
		return
	}
	e.bm, e.err, e.done = bm, err, true
	e.mu.Unlock()

	// 解码失败的图不记账，条目留着好让界面画失败态（重试是调用方的事）。
	c.mu.Lock()
	evicted := c.lru.resize(e, bytesOf(bm))
	for _, victim := range evicted {
		c.entries.Delete(victim.url)
	}
	c.mu.Unlock()

	for _, victim := range evicted {
		victim.discard()
	}
	c.notify()
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

// discard 标记一个 entry 的结果没人要了，晚到的解码结果直接扔掉。
func (e *entry) discard() {
	e.mu.Lock()
	e.dropped = true
	cancel := e.cancel
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
