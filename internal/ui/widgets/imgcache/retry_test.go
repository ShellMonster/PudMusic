package imgcache

import (
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// 「暂时失败会自己好起来」这一组。
//
// # 为什么用假钟
//
// 退避的判据是**时间**：1 秒之后试、再 2 秒之后试、封顶之后每 30 秒试一次。
// 真等的测试一条就要半分钟，而且机器一忙，「到点了」和「还没到」就分不清。
// 假钟把「时间走」变成一次 Advance：推多少就是多少，重试到期这件事是
// **确定**的，不受负载影响。
//
// # 假钟的锁
//
// 回调在 Advance 里**出了锁**才跑：回调会去锁 entry，而在锁里跑会和
// `finish` 的「锁 entry → 排定时器」拧成死锁。这条不是理论风险——写测试时
// 就差点这么写。

// fakeClock 是退避用的假钟，只有 Advance 才让时间走。
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	seq    int
	timers []*fakeTimer
}

// fakeTimer 是 fakeClock 排下的一个定时器。
type fakeTimer struct {
	clock   *fakeClock
	due     time.Time
	seq     int
	fn      func()
	stopped bool
	fired   bool
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(1_700_000_000, 0)}
}

func (c *fakeClock) AfterFunc(d time.Duration, fn func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{clock: c, due: c.now.Add(d), seq: c.seq, fn: fn}
	c.seq++
	c.timers = append(c.timers, t)
	return t
}

func (t *fakeTimer) Stop() bool {
	c := t.clock
	c.mu.Lock()
	defer c.mu.Unlock()
	if t.stopped || t.fired {
		return false
	}
	t.stopped = true
	return true
}

// nowTime 报假钟现在几点。HTTP handler 用它记下请求实际发生在哪一刻，
// 于是「间隔是不是递增的」可以直接对着时间断言，不必数次数。
func (c *fakeClock) nowTime() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance 把钟往前推 d，按排定的先后跑掉这一段时间里到期的回调。
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	var due []*fakeTimer
	for _, t := range c.timers {
		if !t.stopped && !t.fired && !t.due.After(now) {
			t.fired = true
			due = append(due, t)
		}
	}
	c.mu.Unlock()

	slices.SortFunc(due, func(a, b *fakeTimer) int { return cmp.Compare(a.seq, b.seq) })
	for _, t := range due {
		t.fn()
	}
}

// newRetryCache 建一个用假钟的缓存。c.Close 排的清理比 srv.Close 晚注册，
// 所以先收缓存、再关服务端。
func newRetryCache(t *testing.T, opts Options, fc *fakeClock) *Cache {
	t.Helper()
	opts.Clock = fc
	c := New(opts)
	t.Cleanup(c.Close)
	return c
}

// armed 报 url 的条目是不是排着下一次重试。
func armed(c *Cache, url string) bool {
	v, ok := c.entries.Load(url)
	if !ok {
		return false
	}
	e := v.(*entry)
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.timer != nil
}

// waitArmed 等到 url 的条目排上下一次重试。⚠️ 必须等它：只有 finish 跑完
// 才会排定时器，而「请求已经打到服务端」不代表 finish 跑完了——不等就
// Advance，那一次会推在空处，后面的断言全对不上。
func waitArmed(t *testing.T, c *Cache, url string) {
	t.Helper()
	waitFor(t, 5*time.Second, "下一次重试排上了", func() bool { return armed(c, url) })
}

// waitHits 等到服务端一共收到 n 个请求。
func waitHits(t *testing.T, hits *atomic.Int32, n int32) {
	t.Helper()
	waitFor(t, 5*time.Second, fmt.Sprintf("服务端收到第 %d 个请求", n), func() bool { return hits.Load() >= n })
}

// TestTransientFailureRetriesWithGrowingBackoff 是这一票的主线：500 之后
// 到期真的再请求，间隔一次比一次长、到封顶就不再长，次数到上限就停。
//
// 假钟让每一步都是确定的：推到一个间隔的**前一毫秒**，请求数不许变；
// 再推那一毫秒，正好多一次。
func TestTransientFailureRetriesWithGrowingBackoff(t *testing.T) {
	fc := newFakeClock()
	var hits atomic.Int32
	var mu sync.Mutex
	var at []time.Time

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// 先记时间再计数：waitHits 一看到计数到了，就得保证这一条也记完了。
		mu.Lock()
		at = append(at, fc.nowTime())
		mu.Unlock()
		hits.Add(1)
		http.Error(w, "图床忙", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := newRetryCache(t, Options{
		RetryBase:  100 * time.Millisecond,
		RetryMax:   250 * time.Millisecond,
		MaxRetries: 4,
	}, fc)
	url := srv.URL + "/cover.png"

	c.Get(url)
	waitState(t, c, url, 5*time.Second, failed)
	waitArmed(t, c, url)
	if n := hits.Load(); n != 1 {
		t.Fatalf("第一次失败之前发了 %d 个请求，want 1", n)
	}

	// 退避期间反复读同一张图：状态是失败（不是 Loading），而且**不会**
	// 因为「又有人要看了」而立刻重下。
	for range 5 {
		if s := c.Get(url); !failed(s) {
			t.Fatalf("退避期间读到 %+v，失败态不该变（也不能闪回 Loading）", s)
		}
	}
	time.Sleep(50 * time.Millisecond)
	if n := hits.Load(); n != 1 {
		t.Fatalf("退避期间又下了 %d 次，退避等于没有", n-1)
	}

	// 100ms → 200ms → 250ms（封顶）→ 250ms（封顶）。
	want := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		250 * time.Millisecond,
		250 * time.Millisecond,
	}
	for i, d := range want {
		fc.Advance(d - time.Millisecond)
		if n := hits.Load(); n != int32(i+1) {
			t.Fatalf("第 %d 次重试提前发了：间隔还没到，请求数已经是 %d", i+1, n)
		}
		fc.Advance(time.Millisecond)
		waitHits(t, &hits, int32(i+2))
		if i < len(want)-1 {
			waitArmed(t, c, url)
		}
	}

	// 间隔对得上假钟：100/200/250/250。
	mu.Lock()
	got := slices.Clone(at)
	mu.Unlock()
	if len(got) != len(want)+1 {
		t.Fatalf("一共发了 %d 个请求，want %d", len(got), len(want)+1)
	}
	for i, d := range want {
		if gap := got[i+1].Sub(got[i]); gap != d {
			t.Errorf("第 %d 次重试隔了 %v，want %v", i+1, gap, d)
		}
	}

	// 第 5 次失败（初次 + 4 次重试）到上限：停在那里，再推一天也不再发。
	waitFor(t, 5*time.Second, "最后一次失败落到终态", func() bool { return failed(c.Get(url)) })
	time.Sleep(50 * time.Millisecond)
	if armed(c, url) {
		t.Error("次数到上限之后还排着重试")
	}
	fc.Advance(24 * time.Hour)
	time.Sleep(50 * time.Millisecond)
	if n := hits.Load(); n != int32(len(want)+1) {
		t.Errorf("到上限之后又发了 %d 个请求", n-int32(len(want)+1))
	}
	if s := c.Get(url); !failed(s) {
		t.Errorf("到上限之后应该是失败终态，拿到 %+v", s)
	}
}

// TestPermanentFailureIsNotRetried 验「这张图就是没有」不再要第二遍。
//
// 两类永久失败各验一条：404（服务端说没有）和解码失败（字节不是图）。
// 410 走同一个分类，用单元测试钉住，不必再起一个服务端。
func TestPermanentFailureIsNotRetried(t *testing.T) {
	cases := []struct {
		name string
		code int
		body string
	}{
		{"404", http.StatusNotFound, "没了"},
		{"解码失败", http.StatusOK, "这不是一张图"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fc := newFakeClock()
			var hits atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				hits.Add(1)
				if tc.code != http.StatusOK {
					http.Error(w, tc.body, tc.code)
					return
				}
				w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)

			c := newRetryCache(t, Options{}, fc)
			url := srv.URL + "/cover.png"
			c.Get(url)
			waitState(t, c, url, 5*time.Second, failed)

			// 给 finish 收尾一点时间：万一把永久失败错排成重试，
			// 定时器在这段里就会挂上。
			time.Sleep(50 * time.Millisecond)
			if armed(c, url) {
				t.Fatal("永久失败也排了重试")
			}
			fc.Advance(24 * time.Hour)
			time.Sleep(50 * time.Millisecond)
			if n := hits.Load(); n != 1 {
				t.Errorf("永久失败之后又发了 %d 个请求", n-1)
			}
			if s := c.Get(url); !failed(s) {
				t.Errorf("永久失败应该一直画失败态，拿到 %+v", s)
			}
		})
	}
}

// TestFailureClassification 钉状态码的分类：4xx 里除了 408/429 都不再试，
// 5xx 和限流等会儿再来；解码失败不重试，网络层的错误重试。
func TestFailureClassification(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		permanent bool
	}{
		{"403", &statusError{code: http.StatusForbidden}, true},
		{"404", &statusError{code: http.StatusNotFound}, true},
		{"410", &statusError{code: http.StatusGone}, true},
		{"408", &statusError{code: http.StatusRequestTimeout}, false},
		{"429", &statusError{code: http.StatusTooManyRequests}, false},
		{"500", &statusError{code: http.StatusInternalServerError}, false},
		{"503", &statusError{code: http.StatusServiceUnavailable}, false},
		{"解码", &permanentError{err: errors.New("坏字节")}, true},
		{"断网", errors.New("connection reset by peer"), false},
		{"超时", errors.New("i/o timeout"), false},
	}
	for _, tc := range cases {
		if got := isPermanent(tc.err); got != tc.permanent {
			t.Errorf("%s：isPermanent=%v，want %v", tc.name, got, tc.permanent)
		}
	}
}

// TestRetrySuccessFlipsStateAndWakes 验重试成功之后界面从失败态变成图，
// 而且重试在途时**仍然是失败态**（不闪回 Loading），成功后照常逐窗口唤醒。
func TestRetrySuccessFlipsStateAndWakes(t *testing.T) {
	fc := newFakeClock()
	var hits atomic.Int32
	gate := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			http.Error(w, "图床忙", http.StatusServiceUnavailable)
			return
		}
		// 第二次故意卡住：测试要在「重试真在飞」的那一刻读状态。
		<-gate
		w.Write(pngBytes(t, 32, 32))
	}))
	t.Cleanup(srv.Close)

	c := newRetryCache(t, Options{RetryBase: time.Second}, fc)

	// 断言失败提前退出时也别把 handler 永远挂在 gate 上（srv.Close 会等它）。
	var gateOnce sync.Once
	openGate := func() { gateOnce.Do(func() { close(gate) }) }
	t.Cleanup(openGate)

	var woke atomic.Int32
	c.AddWake(func() { woke.Add(1) })
	url := srv.URL + "/cover.png"

	c.Get(url)
	waitState(t, c, url, 5*time.Second, failed)
	waitArmed(t, c, url)
	wokeBefore := woke.Load()

	fc.Advance(time.Second)
	waitHits(t, &hits, 2)

	// 重试已经在飞：状态还是失败，不是「载入中」。
	if s := c.Get(url); s.Loading || s.Err == nil {
		t.Fatalf("重试在途时读到 %+v，应该一直是失败态", s)
	}

	openGate()
	waitState(t, c, url, 5*time.Second, loaded)
	if s := c.Get(url); !s.Ready() {
		t.Fatalf("重试成功之后还是 %+v", s)
	}

	// 成功之后不再排重试，失败记录也摘掉了。
	if armed(c, url) {
		t.Error("成功之后还排着重试")
	}
	c.mu.Lock()
	records := len(c.failed)
	c.mu.Unlock()
	if records != 0 {
		t.Errorf("成功之后失败表里还剩 %d 条", records)
	}

	// 逐窗口唤醒（AGENTS 铁律 3）：这次成功也得叫醒窗口。
	waitFor(t, time.Second, "重试成功后窗口被叫醒", func() bool { return woke.Load() > wokeBefore })
}

// TestTheCoverBecomesAnImageAfterARetry 验界面那一头：那一格退避期间一直是
// 失败态（描边框），重试成功之后换成图。
//
// 这是 `TestRetrySuccessFlipsStateAndWakes` 的画面版：状态对了不代表画对了，
// 而这一票用户能感到的变化**只有**这一格从「拿不到」变成图。
func TestTheCoverBecomesAnImageAfterARetry(t *testing.T) {
	fc := newFakeClock()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hits.Add(1) == 1 {
			http.Error(w, "图床忙", http.StatusServiceUnavailable)
			return
		}
		w.Write(pngBytes(t, 40, 40))
	}))
	t.Cleanup(srv.Close)

	c := newRetryCache(t, Options{RetryBase: time.Second}, fc)
	url := srv.URL + "/cover.png"
	tt := ui.NewTester(func(ctx *ui.Context) { View(ctx, c, url, 200, 120) }, 320, 200)

	// 第一帧起请求；等它落到失败态。
	tt.Frame()
	waitFor(t, 5*time.Second, "失败态画出来了", func() bool {
		tt.Frame()
		return c.Get(url).Err != nil && hasBorder(tt.Image(), 200, 120)
	})
	failed := fingerprint(tt.Image(), 200, 120)
	waitArmed(t, c, url)

	// 退避期间再画几帧：还是那一圈描边框，没有闪回骨架屏。
	for range 3 {
		tt.Frame()
		if !hasBorder(tt.Image(), 200, 120) {
			t.Fatal("退避期间失败态变成了加载中")
		}
	}

	fc.Advance(time.Second)
	waitFor(t, 5*time.Second, "重试成功后那一格换成了图", func() bool {
		tt.Frame()
		return c.Get(url).Ready()
	})
	tt.Frame()
	if sameColors(fingerprint(tt.Image(), 200, 120), failed) {
		t.Error("重试成功之后画面还是失败态那一格")
	}
}

// TestCloseDropsAWaitingRetry 验缓存关掉之后，排着的重试不再醒过来。
//
// 关窗口之后还能在后台发请求，属于「关了还在干活」那一类症状；而重试是
// 定时器，不主动停的话它会一直等到点。
func TestCloseDropsAWaitingRetry(t *testing.T) {
	fc := newFakeClock()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, "图床忙", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := newRetryCache(t, Options{RetryBase: time.Second}, fc)
	url := srv.URL + "/cover.png"
	c.Get(url)
	waitState(t, c, url, 5*time.Second, failed)
	waitArmed(t, c, url)

	c.Close()
	fc.Advance(24 * time.Hour)
	time.Sleep(50 * time.Millisecond)
	if n := hits.Load(); n != 1 {
		t.Errorf("关掉缓存之后又发了 %d 个请求", n-1)
	}
}

// TestFailureRecordsAreBounded 验失败记录有数量上限：大量坏地址不会
// 一直堆在表里；被忘掉的那一张下次再要会当成第一次重新来。
func TestFailureRecordsAreBounded(t *testing.T) {
	fc := newFakeClock()
	var mu sync.Mutex
	perURL := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		perURL[r.URL.Path]++
		mu.Unlock()
		http.Error(w, "没了", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	c := newRetryCache(t, Options{MaxFailed: 3}, fc)

	urls := make([]string, 5)
	for i := range urls {
		urls[i] = fmt.Sprintf("%s/%c.png", srv.URL, 'a'+i)
		// 一张一张来，失败表的先后才是确定的。
		c.Get(urls[i])
		waitState(t, c, urls[i], 5*time.Second, failed)
	}

	c.mu.Lock()
	records := len(c.failed)
	inLRU := len(c.lru.byURL)
	_, oldestKept := c.entries.Load(urls[0])
	c.mu.Unlock()
	if records != 3 {
		t.Fatalf("失败表里有 %d 条，上限是 3", records)
	}
	// 失败条目是 0 字节，LRU 自己挤不掉；被忘掉的必须连 URL 表一起删，
	// 否则「有上限」只限住了一半，内存照样随坏地址增长。
	if inLRU != 3 {
		t.Errorf("LRU 的 URL 表里有 %d 条，want 3", inLRU)
	}
	if oldestKept {
		t.Error("最老的那条没有被忘掉")
	}

	// 被忘掉的那张：再要一次会重新请求，不再是「失败了就永远失败」。
	mu.Lock()
	before := perURL["/a.png"]
	mu.Unlock()
	c.Get(urls[0])
	waitFor(t, 5*time.Second, "被忘掉的那张重新下了一次", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return perURL["/a.png"] > before
	})
}

// TestRetriesRespectTheInflightLimit 验一批重试同时到期时，走的仍然是
// 同一组在途槽位：不会因为「都到点了」就把图床打爆。
func TestRetriesRespectTheInflightLimit(t *testing.T) {
	fc := newFakeClock()
	var total, live, peak atomic.Int32
	gate := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if total.Add(1) <= 3 {
			// 三张图的初次加载：立刻失败，好让三张都排上重试。
			http.Error(w, "图床忙", http.StatusInternalServerError)
			return
		}
		// 重试：卡在这里，数同时在飞的有几个。
		n := live.Add(1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		<-gate
		live.Add(-1)
		http.Error(w, "图床忙", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := newRetryCache(t, Options{
		MaxInflight: 1,
		RetryBase:   100 * time.Millisecond,
		MaxRetries:  1,
	}, fc)

	// 同上：提前失败时先把 handler 放掉，别让 srv.Close 等到天荒地老。
	var gateOnce sync.Once
	openGate := func() { gateOnce.Do(func() { close(gate) }) }
	t.Cleanup(openGate)

	for _, u := range []string{srv.URL + "/1.png", srv.URL + "/2.png", srv.URL + "/3.png"} {
		c.Get(u)
		waitState(t, c, u, 5*time.Second, failed)
		waitArmed(t, c, u)
	}

	// 三张同时到期：三条重试都会想下载，但在途名额只有一个。
	fc.Advance(100 * time.Millisecond)
	waitFor(t, 5*time.Second, "重试真的开始了", func() bool { return live.Load() >= 1 })
	time.Sleep(100 * time.Millisecond)
	if n := peak.Load(); n > 1 {
		t.Errorf("同时在途 %d 个重试，限制是 1", n)
	}
	openGate()
}
