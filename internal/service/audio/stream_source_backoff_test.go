package audio

// 这一组盯的是**退避**和**「重连」这个状态**（票 #315）。
//
// # 现象
//
// CDN 抖一下，三次重试在几毫秒里就打完（`recover` 里以前**一个 sleep
// 都没有**），用户只看到一句红字；而重连的那几秒里界面上一个字都没有——
// 「歌停着、界面不动、没有字」。
//
// 这一组钉三件事：
//
//	重试之间**真的隔了**（间隔随次数变长）
//	「准备重试」和「字节回来了」这两件事**报得出去**（界面上那一档靠它）
//	关掉源能**立刻**打断那个等待（不然用户已经走了，后台还在数秒）

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// fastBackoff 是这一组用的退避序列。
//
// ⚠️ **默认的 1 秒 / 2 秒 / 4 秒不能直接用**：那会让这一组慢好几秒，而
// 这一组要验的恰恰是「间隔是不是真的存在、是不是随次数变长」——用几十
// 毫秒的序列，判据才立得住（真正等的是哪几个数，由
// `TestTheDefaultBackoffIsOneTwoFour` 单独钉住）。
func fastBackoff() []time.Duration {
	return []time.Duration{20 * time.Millisecond, 40 * time.Millisecond, 80 * time.Millisecond}
}

// TestTheDefaultBackoffIsOneTwoFour 钉住**默认**退避是哪几个数。
//
// 这一条是给人和给未来的自己看的：界面上写着「正在重连（第 2 次）」，
// 用户等的是多久，得有个地方写着。
//
// 🔴 **总时长封顶 7 秒**：再长用户就走了（他看到的是「歌不动了」，而
// 「等 20 秒」和「等 4 秒」在他的感受里没有区别）。
func TestTheDefaultBackoffIsOneTwoFour(t *testing.T) {
	got := defaultBackoff()
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	if len(got) != len(want) {
		t.Fatalf("默认退避有 %d 档，want %d 档：%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("默认退避第 %d 档 = %v，want %v", i+1, got[i], want[i])
		}
	}
	var sum time.Duration
	for _, d := range got {
		sum += d
	}
	if sum > 7*time.Second {
		t.Errorf("退避总共 %v，比 7 秒还长——用户等不起，会以为应用死了", sum)
	}
}

// TestARetryWaitsBeforeItRetries 盯**退避真的存在**。
//
// 判据是**两次请求之间隔了多久**：这是「用户等得到」和「瞬间打完」的分界。
// 以前 `recover` 里一个 sleep 都没有，三次重试在几毫秒里就完了。
func TestARetryWaitsBeforeItRetries(t *testing.T) {
	data := payload(32 << 10)

	var mu sync.Mutex
	var hits []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, time.Now())
		n := len(hits)
		mu.Unlock()
		if n == 1 {
			// 第一次：地址过期了。
			http.Error(w, "地址失效了", http.StatusForbidden)
			return
		}
		serveBytes(w, r, data, len(data))
	}))
	t.Cleanup(srv.Close)

	backoff := fastBackoff()
	var (
		evMu       sync.Mutex
		attempts   []int
		waits      []time.Duration
		reconnects int
	)
	s, err := NewStreamSource(StreamSourceOptions{
		URLs:    []string{srv.URL},
		Client:  srv.Client(),
		TempDir: t.TempDir(),
		Backoff: backoff,
		// 换流：主地址过期了就换一条（这里换回同一条，好让第二次请求成功）。
		Refetch: func(context.Context, int64) *StreamReply {
			return &StreamReply{URLs: []string{srv.URL}}
		},
		OnRetry: func(attempt int, wait time.Duration) {
			evMu.Lock()
			attempts = append(attempts, attempt)
			waits = append(waits, wait)
			evMu.Unlock()
		},
		OnReconnected: func() {
			evMu.Lock()
			reconnects++
			evMu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	got, err := io.ReadAll(s)
	if err != nil {
		t.Fatalf("重试之后应该能下完：%v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("下到的字节不对：%d 字节，want %d", len(got), len(data))
	}

	mu.Lock()
	got_hits := append([]time.Time(nil), hits...)
	mu.Unlock()
	if len(got_hits) != 2 {
		t.Fatalf("服务器被打了 %d 次，want 2 次", len(got_hits))
	}
	gap := got_hits[1].Sub(got_hits[0])
	if gap < backoff[0] {
		t.Errorf("两次请求只隔了 %v，比退避 %v 还短——退避没起作用", gap, backoff[0])
	}

	evMu.Lock()
	defer evMu.Unlock()
	if len(attempts) != 1 || attempts[0] != 1 {
		t.Errorf("报了 %d 次「第几次重试」：%v，want 一次且是第 1 次", len(attempts), attempts)
	}
	if len(waits) != 1 || waits[0] != backoff[0] {
		t.Errorf("报出来的等待时长 = %v，want %v", waits, backoff[0])
	}
	if reconnects != 1 {
		t.Errorf("「字节回来了」报了 %d 次，want 1 次", reconnects)
	}
}

// TestTheBackoffGrowsWithEveryRetry 盯**间隔随次数变长**。
//
// 这是「指数退避」和「固定间隔」的分界：CDN 抖一下的时候，等 20 毫秒和
// 等 20 毫秒没有区别，等 4 秒才有意义——网络得缓一会儿。
func TestTheBackoffGrowsWithEveryRetry(t *testing.T) {
	data := payload(32 << 10)

	var mu sync.Mutex
	var hits []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits = append(hits, time.Now())
		n := len(hits)
		mu.Unlock()
		if n <= 3 {
			http.Error(w, "地址失效了", http.StatusForbidden)
			return
		}
		serveBytes(w, r, data, len(data))
	}))
	t.Cleanup(srv.Close)

	backoff := fastBackoff()
	var (
		evMu     sync.Mutex
		attempts []int
		waits    []time.Duration
	)
	s, err := NewStreamSource(StreamSourceOptions{
		URLs:    []string{srv.URL},
		Client:  srv.Client(),
		TempDir: t.TempDir(),
		Backoff: backoff,
		Refetch: func(context.Context, int64) *StreamReply {
			return &StreamReply{URLs: []string{srv.URL}}
		},
		OnRetry: func(attempt int, wait time.Duration) {
			evMu.Lock()
			attempts = append(attempts, attempt)
			waits = append(waits, wait)
			evMu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	if _, err := io.ReadAll(s); err != nil {
		t.Fatalf("重试之后应该能下完：%v", err)
	}

	evMu.Lock()
	defer evMu.Unlock()
	if len(waits) != len(backoff) {
		t.Fatalf("退了 %d 次，want %d 次：%v", len(waits), len(backoff), waits)
	}
	for i, w := range waits {
		if w != backoff[i] {
			t.Errorf("第 %d 次的等待 = %v，want %v", i+1, w, backoff[i])
		}
		if attempts[i] != i+1 {
			t.Errorf("第 %d 次报的「第几次」= %d", i+1, attempts[i])
		}
	}
}

// TestTheBackoffStopsAtItsLastValue 盯**序列用完了用最后一个**。
//
// 不是变成 0：变成 0 就等于「后面几次不等了」，那还是瞬间打完——只是慢了一档。
func TestTheBackoffStopsAtItsLastValue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "地址失效了", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	var (
		evMu  sync.Mutex
		waits []time.Duration
	)
	s, err := NewStreamSource(StreamSourceOptions{
		URLs:       []string{srv.URL},
		Client:     srv.Client(),
		TempDir:    t.TempDir(),
		Backoff:    []time.Duration{15 * time.Millisecond},
		MaxRefresh: 3,
		Refetch:    func(context.Context, int64) *StreamReply { return &StreamReply{URLs: []string{srv.URL}} },
		OnRetry: func(_ int, wait time.Duration) {
			evMu.Lock()
			waits = append(waits, wait)
			evMu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	_, _ = io.ReadAll(s)

	evMu.Lock()
	defer evMu.Unlock()
	if len(waits) != 3 {
		t.Fatalf("退了 %d 次，want 3 次：%v", len(waits), waits)
	}
	for i, w := range waits {
		if w != 15*time.Millisecond {
			t.Errorf("第 %d 次的等待 = %v，want 15ms（序列只有一个值，后面的都该用它）", i+1, w)
		}
	}
}

// TestTheSourceGivesUpAfterTheBackoffIsUsedUp 盯**重连失败有明确结果**。
//
// 用完了退避就要**老实报错**：不能一直退、也不能一声不吭。
func TestTheSourceGivesUpAfterTheBackoffIsUsedUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "地址失效了", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	s, err := NewStreamSource(StreamSourceOptions{
		URLs:       []string{srv.URL},
		Client:     srv.Client(),
		TempDir:    t.TempDir(),
		Backoff:    []time.Duration{5 * time.Millisecond},
		MaxRefresh: 1,
		Refetch:    func(context.Context, int64) *StreamReply { return &StreamReply{URLs: []string{srv.URL}} },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	_, _ = io.ReadAll(s)

	err = s.Err()
	if err == nil {
		t.Fatal("退避用完了却什么都没说")
	}
	if errors.Is(err, io.EOF) {
		t.Errorf("中途拉不动被说成了「放完了」：%v", err)
	}
	if !errors.Is(err, ErrStreamDead) {
		t.Errorf("err = %v，没有包 ErrStreamDead", err)
	}
}

// TestTheRetryNumberCountsThisRoundOnly 盯**「第 N 次」的口径**（票 #378）。
//
// 界面上写的是「正在重连（第 N 次）」，而那个 N 必须是「**这一轮断流**里的
// 第几次重试」——一次断流恢复之后，下一轮从第 1 次重新数。
//
// 🔴 **以前那个 N 是「这条源累计换过几次流」**：一次断流恢复之后，下一次
// 断流的**第一次**重试会显示「第 2 次」——用户读到的是「我刚才已经重试过
// 一次了」，而实际上**这一轮是第一次**。
//
// 这一条造的形状是「断 → 恢复 → 再断」：
//
//	第一次请求	地址过期（403）→ 这一轮的第 1 次重试
//	第二次请求	发一半就断——**字节是到了的**，所以这一轮算「恢复过」
//	第三次请求	接着下完
//
// 于是两次断流都是「第 1 次」。旧口径下第二次会报「第 2 次」。
func TestTheRetryNumberCountsThisRoundOnly(t *testing.T) {
	data := payload(64 << 10)

	var (
		mu   sync.Mutex
		hits int
	)
	// 掐断要等测试确认「那半截字节到了客户端」再放行（理由见 `cutAfter`
	// 那段注释）：不这样的话，客户端看到的就成了一次「什么都没下到」的
	// 失败——那样测的就不是「这一轮恢复过了」。
	gate := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		n := hits
		mu.Unlock()
		switch n {
		case 1:
			// 地址过期了：这一轮的第 1 次重试。
			http.Error(w, "地址失效了", http.StatusForbidden)
		case 2:
			// 发一半就断。**字节是到了的**——那一轮因此算「恢复过」。
			from := rangeStart(r)
			w.Header().Set("Content-Length", fmt.Sprint(int64(len(data))-from))
			if from > 0 {
				w.Header().Set("Content-Range",
					fmt.Sprintf("bytes %d-%d/%d", from, len(data)-1, len(data)))
				w.WriteHeader(http.StatusPartialContent)
			}
			w.Write(data[from : len(data)/2])
			w.(http.Flusher).Flush()
			<-gate
			panic(http.ErrAbortHandler)
		default:
			serveBytes(w, r, data, len(data))
		}
	}))
	t.Cleanup(srv.Close)

	var (
		evMu       sync.Mutex
		attempts   []int
		reconnects int
	)
	s, err := NewStreamSource(StreamSourceOptions{
		URLs:    []string{srv.URL},
		Client:  srv.Client(),
		TempDir: t.TempDir(),
		Backoff: fastBackoff(),
		Refetch: func(context.Context, int64) *StreamReply {
			return &StreamReply{URLs: []string{srv.URL}}
		},
		OnRetry: func(attempt int, wait time.Duration) {
			evMu.Lock()
			attempts = append(attempts, attempt)
			evMu.Unlock()
		},
		OnReconnected: func() {
			evMu.Lock()
			reconnects++
			evMu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	// 先把头几个字节读出来，确认它们到了，再放行掐断。
	buf := make([]byte, 16)
	if _, err := io.ReadFull(s, buf); err != nil {
		t.Fatalf("头几个字节没到：%v", err)
	}
	close(gate)

	got, err := io.ReadAll(s)
	if err != nil {
		t.Fatalf("重试之后应该能下完：%v", err)
	}
	if !bytes.Equal(got, data[len(buf):]) {
		t.Errorf("下到的字节不对：%d 字节，want %d", len(got), len(data)-len(buf))
	}

	evMu.Lock()
	defer evMu.Unlock()
	// 🔴 **两次断流都是「第 1 次」**：字节回来过（这一轮过去了），下一次
	// 断流重新数。
	want := []int{1, 1}
	if len(attempts) != len(want) {
		t.Fatalf("报了 %d 次「第几次重试」：%v，want %v", len(attempts), attempts, want)
	}
	for i, at := range attempts {
		if at != want[i] {
			t.Errorf("第 %d 次断流报的是「第 %d 次」，want 第 %d 次（这一轮重新数）",
				i+1, at, want[i])
		}
	}
	if reconnects != 2 {
		// 两次恢复：第二次请求发来的那半截（这一轮过去了），第三次请求发完的
		// 那半截（下一轮也过去了）。
		t.Errorf("「字节回来了」报了 %d 次，want 2 次（两轮各自恢复过一次）", reconnects)
	}
}

// TestCloseCancelsTheBackoffWait 盯**那个等待是可打断的**。
//
// 不打断的话：用户切了歌，源还在那儿数着 4 秒；而 `Close` 会等那两个
// goroutine 退出——于是「切歌」这一下会卡住整整一个退避时长。
func TestCloseCancelsTheBackoffWait(t *testing.T) {
	var mu sync.Mutex
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		http.Error(w, "地址失效了", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	s, err := NewStreamSource(StreamSourceOptions{
		URLs:    []string{srv.URL},
		Client:  srv.Client(),
		TempDir: t.TempDir(),
		// 一个很长的退避：Close 必须不等它。
		Backoff: []time.Duration{30 * time.Second},
		Refetch: func(context.Context, int64) *StreamReply { return &StreamReply{URLs: []string{srv.URL}} },
	})
	if err != nil {
		t.Fatal(err)
	}

	// 等它真的进了那个等待（第一次请求打过了）。
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := hits
		mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	// 再给它一点时间，好确定它已经进了退避。
	time.Sleep(50 * time.Millisecond)

	start := time.Now()
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Errorf("Close 用了 %v——那个等待没被打断", el)
	}

	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Errorf("服务器被打了 %d 次，want 1 次（退避被打断之后不该再试）", hits)
	}
}
