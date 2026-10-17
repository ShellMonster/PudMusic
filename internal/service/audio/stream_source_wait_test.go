package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 这一组盯的是**断网与恢复**：网络断了歌不停，网络回来歌从断点接着响。
//
// 断网有三种形态，这里刻意三种都造，因为它们走的是**三条不同的代码路径**：
//
//	连不上        get 立刻报错          → recover → 等网络
//	连上了没字节  get 挂住不返回        → 看门狗掐断 → recover → 等网络
//	下到一半断了  读到 unexpected EOF   → recover → 等网络
//
// 只造一种的话，「另外两种断网时歌会怎么样」就只能靠人肉相信。

// ─────────────────────────────────────────────────────────────────────────────
// fixture
// ─────────────────────────────────────────────────────────────────────────────

// deadAddr 造一个「连不上」的地址：拿到一个真的监听过的端口，然后把它关掉。
//
// 为什么不用真断网：改路由、改防火墙都要管理员权限，而且都不是测试。
// 一个刚被关掉的监听端口给出的形态跟真断网一样（connection refused），
// 而且可以精确地、随时地开关。
func deadAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := "http://" + l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

// flakyCDN 是一台「会掉线」的 CDN。
//
// `cut` 挡住的是「下到一半断了」，`black` 挡的是「连上了但一个字节都不给」。
// 两者的区别不是形式：前者一趟往返就结束，后者要把连接挂在原地等看门狗
// 掐断（`TestWatchdogCutsADownloadThatStopsMakingProgress` 盯的就是它）。
type flakyCDN struct {
	data []byte

	mu     sync.Mutex
	black  bool                  // 接了请求，一个字节都不给
	cut    int                   // >0 时只发到第几个字节就掐断
	gates  map[int]chan struct{} // 每次请求一个放行闸
	ranges []int64               // 每次请求的 Range 起点
}

func newFlakyCDN(data []byte) *flakyCDN {
	return &flakyCDN{data: data, gates: map[int]chan struct{}{}}
}

func (c *flakyCDN) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	n := len(c.gates) + 1
	gate := make(chan struct{})
	c.gates[n] = gate
	c.ranges = append(c.ranges, rangeStart(r))
	black, cut, data := c.black, c.cut, c.data
	c.mu.Unlock()

	switch {
	case black:
		// 接了请求就再也不吭声。真断网最常见的样子：NAT 超时、代理静默丢弃。
		<-gate
		panic(http.ErrAbortHandler)

	case cut > 0 && rangeStart(r) < int64(cut):
		// ⚠️ Content-Length 声明的是**整段**的长度，实际只发到 cut 就掐断。
		// 声明写实际长度的话客户端读到的是干净的 io.EOF，会被当成「下完了」——
		// 而那正是这里要排除的形态。
		from := rangeStart(r)
		w.Header().Set("Content-Length", fmt.Sprint(int64(len(data))-from))
		if from > 0 {
			w.Header().Set("Content-Range",
				fmt.Sprintf("bytes %d-%d/%d", from, len(data)-1, len(data)))
			w.WriteHeader(http.StatusPartialContent)
		}
		w.Write(data[from:cut])
		w.(http.Flusher).Flush()
		<-gate
		panic(http.ErrAbortHandler)

	default:
		serveBytes(w, r, data, len(data))
		<-gate
	}
}

// goDark 把 CDN 关掉：之后的请求都是「连上了但什么都不给」。
func (c *flakyCDN) goDark() {
	c.mu.Lock()
	c.black = true
	c.cut = 0
	c.mu.Unlock()
	c.releaseAll()
}

// goUp 把 CDN 修好，并把挂着不放的连接掐掉——真网络恢复时那些连接也是断的，
// 客户端要重新发一次才接得上。
func (c *flakyCDN) goUp() {
	c.mu.Lock()
	c.black, c.cut = false, 0
	c.mu.Unlock()
	c.releaseAll()
}

func (c *flakyCDN) releaseAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, g := range c.gates {
		select {
		case <-g:
		default:
			close(g)
		}
	}
}

// servedRanges 返回每一次请求的 Range 起点。**只有测试收尾时调。**
func (c *flakyCDN) servedRanges() []int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]int64(nil), c.ranges...)
}

// waitWaiting 等源亮起缓冲态，到点还没亮就**直接判失败**。
//
// ⚠️ 不能用 `waitUntil`：它到点是静默返回的，于是「缓冲态从头到尾没亮过」
// 和「亮过了」在输出里长得一模一样。这份文件里的每一条断言都建立在
// 「缓冲态确实亮过」上，所以这一层必须是会喊的。
func waitWaiting(t *testing.T, s *StreamSource, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if s.Waiting() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%v 内没有亮起缓冲态（Err = %v）——断网之后它不是在等网络，而是"+
		"当场把歌判死了，或者压根没发现出了问题", d, s.Err())
}

// waitDone 等源走到终态，到点还没到就**直接判失败**。
func waitDone(t *testing.T, s *StreamSource, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if s.Err() != nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%v 内既没下完也没报错（Waiting = %v，下到 %d/%d 字节）——"+
		"它卡在一个既不算成功也不算失败的状态里，界面上会一直写着「缓冲中……」",
		d, s.Waiting(), s.Filled(), s.Size())
}

// ─────────────────────────────────────────────────────────────────────────────
// 主线
// ─────────────────────────────────────────────────────────────────────────────

// TestANetworkOutageBuffersAndResumesFromTheSameByte 盯这件事的全貌。
//
// 断网 → 亮缓冲态、**不报错** → 网络回来 → **从同一个字节**接着下完。
//
// 断「重连之后 Range 的起点」，不是「有没有报错」：从头重下这个 bug 看起来
// **完全正常**（歌在响、没有红字、进度条照走），只有拿 Range 去量才发现
// 前面那一大截白下了。而用户真正在意的也正是这个——重下一遍意味着他要
// 在同一首歌上再等一遍。
func TestANetworkOutageBuffersAndResumesFromTheSameByte(t *testing.T) {
	data := payload(64 << 10)
	cut := 16 << 10

	cdn := newFlakyCDN(data)
	cdn.cut = cut
	srv := httptest.NewServer(cdn)
	t.Cleanup(func() { cdn.releaseAll(); srv.Close() })

	dir := t.TempDir()
	s, err := NewStreamSource(StreamSourceOptions{
		URLs:    []string{srv.URL + "/a.flac"},
		Size:    int64(len(data)),
		Client:  srv.Client(),
		TempDir: dir,
		Refetch: func(context.Context, int64) *StreamReply { return nil },
		// 缓冲预算给得宽：这一条要验的是「恢复之后接得上」，不是「等多久放弃」。
		MaxWait: 30 * time.Second,
		// ⚠️ StallTimeout 必须给短：断网这一段是「连上了但一个字节都不给」，
		// 那种连接不会自己报错，是看门狗掐断它。默认 20 秒的话这条用例
		// 会有 20 秒在等一个早该发生的失败。
		StallTimeout:    time.Second,
		RetryBackoff:    20 * time.Millisecond,
		MaxRetryBackoff: 500 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	if _, err := io.ReadFull(s, make([]byte, cut)); err != nil {
		t.Fatalf("前 %d 字节没下到：%v", cut, err)
	}
	if got := s.Filled(); got != int64(cut) {
		t.Fatalf("下到了 %d 字节，want %d", got, cut)
	}

	// 断网。挂着的那个 handler 一起掐掉，泵这才看得见「断了」。
	cdn.goDark()

	waitWaiting(t, s, 15*time.Second)

	// ⚠️ 断网期间 `Err()` 必须是 nil，而且不能沾 io.EOF。
	//
	// 这是整件事最容易做坏的地方：解码器阻塞在 Read 上，一旦那边返回了
	// 任何错误，MyGo 就把它折成「读到文件尾」（`mygo/audio/source.go`），
	// 歌就在那一帧悄悄结束——用户看到的是「歌放完了」，而他只是断了网。
	if got := s.Err(); got != nil {
		t.Fatalf("断网就报了终态错误：%v——歌会在那一帧被当成放完", got)
	}
	if cause := s.WaitCause(); cause == nil {
		t.Error("缓冲着却没有记下为什么拉不动（诊断和日志要用）")
	}
	if s.Filled() != int64(cut) {
		t.Errorf("断网之后缓存长度变成了 %d，want %d", s.Filled(), cut)
	}

	// 网络回来。
	cdn.goUp()

	waitDone(t, s, 20*time.Second)
	if err := s.Err(); !errors.Is(err, io.EOF) {
		t.Fatalf("网络恢复之后 err = %v，want io.EOF（下完了）", err)
	}
	if s.Waiting() {
		t.Error("网络恢复了缓冲态还亮着——界面上会一直写「缓冲中……」")
	}
	if s.Filled() != int64(len(data)) {
		t.Errorf("下满了 %d 字节，want %d", s.Filled(), len(data))
	}

	// ⚠️ 核心断言：**恢复之后的每一次请求都从断的那个字节要**。
	//
	// 前面那一截已经在缓存里了，重下它不但白等，还会让用户多花一倍时间。
	ranges := cdn.servedRanges()
	if len(ranges) < 2 {
		t.Fatalf("只请求了 %d 次（%v），验不了「从断点接上」", len(ranges), ranges)
	}
	for i, from := range ranges[1:] {
		if from != int64(cut) {
			t.Errorf("恢复之后的第 %d 次请求从第 %d 字节开始，want %d"+
				"（缓存里已经有前面那一截了，重下是白等）", i, from, cut)
		}
	}
}

// TestThePlaybackIsNotAffectedByAWait 盯缓冲期间读的人不报错。
//
// 上层（解码 goroutine）就阻塞在 `Read` 上。缓冲期间它必须继续等，
// 不能拿到 EOF 也不能拿到任何错误——MyGo 会把任何一个错误折成
// 「读到文件尾」，那就是「歌放完了」。
func TestThePlaybackIsNotAffectedByAWait(t *testing.T) {
	cdn := newFlakyCDN(payload(64 << 10))
	cdn.cut = 8 << 10
	srv := httptest.NewServer(cdn)
	t.Cleanup(func() { cdn.releaseAll(); srv.Close() })

	s, err := NewStreamSource(StreamSourceOptions{
		URLs:         []string{srv.URL + "/a.flac"},
		Client:       srv.Client(),
		TempDir:      t.TempDir(),
		Size:         int64(len(cdn.data)),
		Refetch:      func(context.Context, int64) *StreamReply { return nil },
		MaxWait:      30 * time.Second,
		StallTimeout: time.Second,
		RetryBackoff: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	if _, err := io.ReadFull(s, make([]byte, 8<<10)); err != nil {
		t.Fatal(err)
	}
	cdn.goDark()
	waitWaiting(t, s, 15*time.Second)

	// 再读一段：它该**继续等着**，而不是返回错误或者 0 字节。
	type result struct {
		n   int
		err error
	}
	res := make(chan result, 1)
	go func() {
		n, err := s.Read(make([]byte, 4096))
		res <- result{n, err}
	}()

	select {
	case got := <-res:
		t.Fatalf("断网期间 Read 立刻返回了 n=%d err=%v——"+
			"播放的人会以为流到头了", got.n, got.err)
	case <-time.After(2 * time.Second):
		// 还在等：这正是缓冲该有的样子。
	}

	cdn.goUp()
	select {
	case got := <-res:
		if got.err != nil {
			t.Fatalf("网络恢复之后 Read 报错：%v", got.err)
		}
		if got.n == 0 {
			t.Fatal("网络恢复之后 Read 什么也没读到")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("网络恢复之后 Read 一直没回来")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 缓冲本身的几条性质
// ─────────────────────────────────────────────────────────────────────────────

// TestCloseWakesASourceThatIsWaiting 盯「缓冲中的源也能立刻关掉」。
//
// 这一条是**切歌不卡住**的全部保证。退避最长 8 秒：不叫醒的话，
// 连点五首就是四十秒的假死，而缓存文件要等退避睡醒才删。
//
// 断的是**时间**：退避给到 30 秒，Close 必须在 2 秒内回来。
func TestCloseWakesASourceThatIsWaiting(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStreamSource(StreamSourceOptions{
		URLs:            []string{deadAddr(t) + "/a.flac"},
		Client:          http.DefaultClient,
		TempDir:         dir,
		Refetch:         func(context.Context, int64) *StreamReply { return nil },
		MaxWait:         time.Hour, // 永远等不到
		RetryBackoff:    30 * time.Second,
		MaxRetryBackoff: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitWaiting(t, s, 10*time.Second)

	start := time.Now()
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("Close 花了 %v 才回来——它把整个退避睡完了", took)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("缓冲中的源关掉了，缓存文件还留着：%v", names)
	}
}

// TestTheBackoffGrowsSoItDoesNotHammerTheServer 盯退避是真的在退。
//
// 不退避的话「等网络」就是一个拿毫秒当单位的循环去撞 CDN 和取流接口。
// B 站在风控状态下会让每一次取流都返回一个马上失效的地址，这么撞下去
// 只是把它自己撞封。
//
// 断的是**请求次数**：连不上的地址一秒之内最多被打十几次；没有退避的话
// 是上千次。
func TestTheBackoffGrowsSoItDoesNotHammerTheServer(t *testing.T) {
	var attempts atomic.Int32
	s, err := NewStreamSource(StreamSourceOptions{
		URLs:   []string{"http://127.0.0.1:1/a.flac"},
		Client: http.DefaultClient,
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			attempts.Add(1)
			return nil, errors.New("dial tcp: network is unreachable")
		}),
		TempDir:         t.TempDir(),
		MaxWait:         time.Hour,
		RetryBackoff:    30 * time.Millisecond,
		MaxRetryBackoff: 200 * time.Millisecond,
		Refetch:         func(context.Context, int64) *StreamReply { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	waitWaiting(t, s, 5*time.Second)
	time.Sleep(time.Second)

	// 退避序列 30、60、120、200、200…：一秒里最多十来轮。
	// 没有退避的话这一秒里会有几百轮。
	if n := int(attempts.Load()); n > 25 {
		t.Errorf("断了一秒打了 %d 次请求，没有在退避", n)
	}
	if n := int(attempts.Load()); n < 4 {
		t.Errorf("只打了 %d 次就再也不试了，网络恢复之后它接不上", n)
	}
}

// roundTripperFunc 把一个函数变成 http.RoundTripper。
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestAStreamThatNeverRecoversSaysSoEventually 盯「缓冲有上限」。
//
// 没有这一条，一个永远拉不动的流会让播放条上的「缓冲中……」永远亮着，
// 用户既不知道歌完了没有，也等不到一句交代。
func TestAStreamThatNeverRecoversSaysSoEventually(t *testing.T) {
	s, err := NewStreamSource(StreamSourceOptions{
		URLs:   []string{deadAddr(t) + "/a.flac"},
		Client: http.DefaultClient,
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial tcp: network is unreachable")
		}),
		TempDir:      t.TempDir(),
		MaxWait:      200 * time.Millisecond, // 等一小会儿就放弃
		RetryBackoff: 20 * time.Millisecond,
		Refetch:      func(context.Context, int64) *StreamReply { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	done := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(s)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("一个字节都没下到，却报「读完了」")
		}
		if !errors.Is(err, ErrStreamDead) {
			t.Errorf("err = %v，want 包着 ErrStreamDead", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("缓冲预算用完了还在等，永远等不到头")
	}
	if s.Waiting() {
		t.Error("已经放弃了缓冲态还亮着——界面上会一边「正在恢复」一边「打不开」")
	}
}

// TestAServerThatSaidNoIsNotWaitedFor 盯「该放弃的别等」。
//
// 服务器回了 403：这条地址不管用了，能变的只有**地址本身**，而换流已经
// 试过 maxRefr 次了。再等一分钟也还是 403——那一分钟里用户对着
// 「缓冲中……」什么都做不了，最后拿到的还是同一句错误。
//
// 断的是**时间**：403 的流必须秒级报错，而不是等满缓冲预算。
func TestAServerThatSaidNoIsNotWaitedFor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "地址失效了", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	s, err := NewStreamSource(StreamSourceOptions{
		URLs:    []string{srv.URL + "/a.flac"},
		Client:  srv.Client(),
		TempDir: t.TempDir(),
		Refetch: func(context.Context, int64) *StreamReply {
			return &StreamReply{URLs: []string{srv.URL + "/b.flac"}}
		},
		MaxRefresh:      1,
		MaxWait:         time.Hour, // 一旦进缓冲态就等于卡一整年
		RetryBackoff:    time.Hour,
		MaxRetryBackoff: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	start := time.Now()
	_, err = io.ReadAll(s)
	took := time.Since(start)

	if err == nil {
		t.Fatal("403 被当成了读完了")
	}
	if !errors.Is(err, ErrStreamDead) {
		t.Errorf("err = %v，want 包着 ErrStreamDead", err)
	}
	if !strings.Contains(err.Error(), "失效") {
		t.Errorf("err = %v，没有说清是「地址失效了」", err)
	}
	if took > 5*time.Second {
		t.Errorf("403 之后等了 %v 才报错——服务器说了「不行」，等是没有用的", took)
	}
}

// TestWithoutARefetchThereIsNothingToWaitFor 盯最小用法不进缓冲态。
//
// 没配 `Refetch` 就等于没有「换一条流」这条后路，而「等网络」的全部内容
// 就是把同一个请求原样重发。调用方既然只要「一段字节」，就不该被悄悄拖进
// 一个可能等一分钟的状态。
func TestWithoutARefetchThereIsNothingToWaitFor(t *testing.T) {
	s, err := NewStreamSource(StreamSourceOptions{
		URLs:   []string{deadAddr(t) + "/a.flac"},
		Client: http.DefaultClient,
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial tcp: network is unreachable")
		}),
		TempDir:         t.TempDir(),
		MaxWait:         time.Hour,
		RetryBackoff:    time.Hour,
		MaxRetryBackoff: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	done := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(s)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, ErrStreamDead) {
			t.Errorf("err = %v，want 包着 ErrStreamDead", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("没有配 Refetch 也进了缓冲态")
	}
}

// TestProgressShortensTheNextWait 盯「网络回来一次就重新从短的开始等」。
//
// 退避档位会被进展重置，而**总预算不会**（见 stream_source_wait.go）。
// 不重置档位的话，一次短暂抖动之后要等满 8 秒才敢再试——那不是保守，
// 那是迟钝。
func TestProgressShortensTheNextWait(t *testing.T) {
	s, err := NewStreamSource(StreamSourceOptions{
		URLs:   []string{deadAddr(t) + "/a.flac"},
		Client: http.DefaultClient,
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial tcp: network is unreachable")
		}),
		TempDir:         t.TempDir(),
		MaxWait:         time.Hour,
		RetryBackoff:    time.Millisecond,
		MaxRetryBackoff: time.Second,
		Refetch:         func(context.Context, int64) *StreamReply { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	// 退到高档位再验重置。`waitStep >= 3` 之前就动手的话量到的是 1，
	// 那样「涨到几」这个断言什么也证明不了。
	deadline := time.Now().Add(10 * time.Second)
	for {
		s.mu.Lock()
		step := s.waitStep
		s.mu.Unlock()
		if step >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("退避档位只涨到 %d，它压根没有在退避", step)
		}
		time.Sleep(5 * time.Millisecond)
	}

	s.mu.Lock()
	before := s.waitStep
	s.noteProgressLocked()
	after := s.waitStep
	s.mu.Unlock()

	if before < 3 {
		t.Fatalf("退避档位只涨到 %d，测不到重置", before)
	}
	if after != 0 {
		t.Errorf("有进展之后退避档位还是 %d，want 0"+
			"（网络回来一次就该重新从短的开始等）", after)
	}
}
