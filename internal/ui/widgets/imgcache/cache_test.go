package imgcache

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 这组测试跑的是真 HTTP（httptest）、真解码（image/png 编出来的字节）、
// 真并发（后台 goroutine + 计时轮询）。没有网络也没有 MyGo 窗口，
// 但走的是 Get → 下载 → 解码 → 记账 → 唤醒这条完整链路。

// pngBytes 编一张 w×h 的 PNG，当封面用。
func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0x80, A: 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func loaded(s State) bool  { return s.Ready() }
func failed(s State) bool  { return !s.Loading && s.Err != nil }
func started(s State) bool { return s.Loading }

// waitState 轮询到某张图落到 want 的状态。
func waitState(t *testing.T, c *Cache, url string, d time.Duration, want func(State) bool) State {
	t.Helper()
	deadline := time.Now().Add(d)
	var last State
	for time.Now().Before(deadline) {
		last = c.Get(url)
		if want(last) {
			return last
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等了 %v，%s 还是 Loading=%v Err=%v", d, url, last.Loading, last.Err)
	return last
}

// waitFor 轮询到 cond 成立。
func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等了 %v，%s 还没发生", d, what)
}

// TestGetDownloadsOncePerURL 是 1.11 的第一道验收：重复 URL 不重复下载。
func TestGetDownloadsOncePerURL(t *testing.T) {
	var hits atomic.Int32
	body := pngBytes(t, 64, 64)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Write(body)
	}))
	defer srv.Close()

	c := New(Options{})
	t.Cleanup(c.Close)
	url := srv.URL + "/cover.png"

	// 同一个 url 同时要十次——歌单里同一首歌的封面被几个 widget 看着是常事。
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() { defer wg.Done(); c.Get(url) }()
	}
	wg.Wait()

	waitState(t, c, url, 5*time.Second, loaded)
	if n := hits.Load(); n != 1 {
		t.Errorf("同一个 url 下了 %d 次，应该只有 1 次", n)
	}
}

// TestLoadingUntilReady 验占位态到就绪态的过渡：第一帧一定是占位。
func TestLoadingUntilReady(t *testing.T) {
	var arrived atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		arrived.Add(1)
		<-release
		w.Write(pngBytes(t, 32, 32))
	}))
	defer srv.Close()

	c := New(Options{})
	t.Cleanup(c.Close)
	url := srv.URL + "/slow.png"

	if s := c.Get(url); !started(s) || s.Ready() {
		t.Fatalf("第一帧应该是占位，拿到 %+v", s)
	}
	waitFor(t, 5*time.Second, "请求发出去了", func() bool { return arrived.Load() > 0 })
	close(release)

	s := waitState(t, c, url, 5*time.Second, loaded)
	if w, h := s.Bitmap.Size(); w != 32 || h != 32 {
		t.Errorf("解出来的图是 %dx%d，want 32x32", w, h)
	}
}

// TestLRUEvictsByBytes 验按字节上限挤，不是按张数。
//
// 一张 200×200 的图解码后是 200*200*4 = 160000 字节 RGBA，mipmap 再加 1/3，
// 合计约 213333。预算给两张半，第三张就该把第一张挤掉。
func TestLRUEvictsByBytes(t *testing.T) {
	var hits atomic.Int32
	body := pngBytes(t, 200, 200)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Write(body)
	}))
	defer srv.Close()

	c := New(Options{MaxBytes: 213333 * 2})
	t.Cleanup(c.Close)

	first := srv.URL + "/a.png"
	for _, u := range []string{first, srv.URL + "/b.png"} {
		waitState(t, c, u, 5*time.Second, loaded)
	}
	if used := c.UsedBytes(); used > 213333*2 {
		t.Fatalf("两张图之后占了 %d 字节，超预算了", used)
	}

	c3rd := srv.URL + "/c.png"
	waitState(t, c, c3rd, 5*time.Second, loaded)

	if used := c.UsedBytes(); used > 213333*2 {
		t.Errorf("挤过之后还占着 %d 字节，超预算了", used)
	}
	// 被挤掉的那张要真的重新下，而不是拿一张旧的糊着。
	before := hits.Load()
	c.Get(first)
	waitFor(t, 5*time.Second, "被挤掉的那张重新下了一次", func() bool { return hits.Load() > before })

	// 重新装回 a 之后，预算又满了，轮到它把 c 挤掉（最近最少用）。
	// c 是刚用过的，不该再下。
	before = hits.Load()
	c.Get(c3rd)
	time.Sleep(100 * time.Millisecond)
	if hits.Load() != before {
		t.Errorf("刚用过的 c 又下了一次，命中数 %d→%d", before, hits.Load())
	}
}

// TestCancelReallyCancels 验取消是真的取消，不是等它下完再扔。
func TestCancelReallyCancels(t *testing.T) {
	var arrived, canceled atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrived.Add(1)
		<-r.Context().Done()
		canceled.Add(1)
	}))
	defer srv.Close()

	c := New(Options{})
	t.Cleanup(c.Close)
	url := srv.URL + "/huge.png"

	if !started(c.Get(url)) {
		t.Fatal("第一帧应该是占位")
	}
	// 等请求真的发出去了再取消，否则测的是「取消了一个还没开始的请求」。
	waitFor(t, 5*time.Second, "请求发出去了", func() bool { return arrived.Load() > 0 })
	c.Cancel(url)

	waitFor(t, 5*time.Second, "服务端看到请求被取消", func() bool { return canceled.Load() > 0 })
	if s := c.Get(url); !started(s) {
		t.Errorf("取消之后再来一次应该重新开始，拿到 %+v", s)
	}
}

// TestFailedLoadGivesErr 验失败态：非 200 变成 Err，不会无限重试。
func TestFailedLoadGivesErr(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, "没了", http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(Options{})
	t.Cleanup(c.Close)

	waitState(t, c, srv.URL+"/404.png", 5*time.Second, failed)
	if n := hits.Load(); n != 1 {
		t.Errorf("失败之后又试了 %d 次，应该就一次", n)
	}
}

// TestCorruptBytesFail 验下回来的不是图会走失败态，而不是把解码器搞崩。
func TestCorruptBytesFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("这不是一张图"))
	}))
	defer srv.Close()

	c := New(Options{})
	t.Cleanup(c.Close)

	waitState(t, c, srv.URL+"/broken.png", 5*time.Second, failed)
	if used := c.UsedBytes(); used != 0 {
		t.Errorf("解失败的图也记了 %d 字节", used)
	}
}

// TestEveryWindowGetsWoken 验多窗口逐个通知。
//
// 铁律：只通知主窗口的话，迷你窗口会一直停在占位图上。实测踩过。
func TestEveryWindowGetsWoken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(pngBytes(t, 32, 32))
	}))
	defer srv.Close()

	c := New(Options{})
	t.Cleanup(c.Close)

	var main, gone atomic.Int32
	c.AddWake(func() { main.Add(1) })
	c.AddWake(func() { gone.Add(1) })() // 注册完就注销

	waitState(t, c, srv.URL+"/x.png", 5*time.Second, loaded)
	waitFor(t, time.Second, "窗口被叫醒", func() bool { return main.Load() > 0 })
	if gone.Load() != 0 {
		t.Errorf("注销掉的窗口还被叫醒了 %d 次", gone.Load())
	}
}

// TestWakeCountCountsTheLiveOnes 验 WakeCount 数的是还挂着的那些。
//
// WakeCount 是给接上唤醒的那一方自查用的（它拿不到窗口，没法从界面那边看回调
// 有没有被调过），所以它自己得先站得住：注销掉的不能再算进去。
func TestWakeCountCountsTheLiveOnes(t *testing.T) {
	c := New(Options{})
	t.Cleanup(c.Close)

	if got := c.WakeCount(); got != 0 {
		t.Fatalf("刚建好的缓存数出 %d 路唤醒", got)
	}
	removeMain := c.AddWake(func() {})
	removeMini := c.AddWake(func() {})
	if got := c.WakeCount(); got != 2 {
		t.Errorf("注册两路之后数出 %d 路", got)
	}
	removeMini()
	if got := c.WakeCount(); got != 1 {
		t.Errorf("注销一路之后数出 %d 路", got)
	}
	removeMain()
	if got := c.WakeCount(); got != 0 {
		t.Errorf("全注销之后数出 %d 路", got)
	}
}

// TestDiskCacheSkipsRedownload 验落盘之后不用再下一次。
func TestDiskCacheSkipsRedownload(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Write(pngBytes(t, 32, 32))
	}))
	defer srv.Close()

	url := srv.URL + "/disk.png"
	dir := t.TempDir()

	first := New(Options{Dir: dir})
	waitState(t, first, url, 5*time.Second, loaded)
	first.Close()

	second := New(Options{Dir: dir})
	t.Cleanup(second.Close)
	waitState(t, second, url, 5*time.Second, loaded)

	if n := hits.Load(); n != 1 {
		t.Errorf("第二次启动又下了一次，一共 %d 次", n)
	}
}

// TestClosedCacheRefuses 验关掉之后不来新的活。
func TestClosedCacheRefuses(t *testing.T) {
	c := New(Options{})
	c.Close()
	c.Close() // 重复调用不许炸

	if s := c.Get("https://example.invalid/x.png"); s.Err == nil {
		t.Errorf("关掉的缓存应该给 Err，拿到 %+v", s)
	}
}

// TestMaxInflightBoundsConcurrency 验在途下载数真的限住了。
func TestMaxInflightBoundsConcurrency(t *testing.T) {
	var live, peak atomic.Int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := live.Add(1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		<-release
		live.Add(-1)
		w.Write(pngBytes(t, 8, 8))
	}))
	defer srv.Close()

	c := New(Options{MaxInflight: 2})
	t.Cleanup(c.Close)
	for i := range 8 {
		c.Get(srv.URL + "/" + strconv.Itoa(i) + ".png")
	}
	waitFor(t, 5*time.Second, "请求都发出去", func() bool { return live.Load() >= 2 })
	time.Sleep(100 * time.Millisecond)

	if n := peak.Load(); n > 2 {
		t.Errorf("同时在途 %d 个，限流是 2", n)
	}
	close(release)
}

// TestCancelFreesTheInflightSlot 验取消会把在途名额放出来。
func TestCancelFreesTheInflightSlot(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-block
	}))
	defer srv.Close()

	c := New(Options{MaxInflight: 1})
	t.Cleanup(c.Close)

	url := srv.URL + "/1.png"
	c.Get(url)
	waitFor(t, 5*time.Second, "在途名额被占住", func() bool { return len(c.inflight) == 1 })

	c.Cancel(url)
	waitFor(t, 5*time.Second, "在途名额放出来", func() bool { return len(c.inflight) == 0 })
	close(block)
}
