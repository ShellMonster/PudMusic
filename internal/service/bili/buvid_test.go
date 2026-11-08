package bili

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const pathSPI = "/x/frontend/finger/spi"

// buvidFixture 是一台答 spi 和一个要 buvid 的接口的假 B 站。
func buvidFixture(t *testing.T, spi http.HandlerFunc) *fakeBili {
	t.Helper()
	f := newFakeBili(t)
	f.on(pathSPI, spi)
	f.on("/x/stream", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{}))
	})
	return f
}

func spiOK(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]string{"b_3": "B3", "b_4": "B4"}))
	}
}

func askStream(t *testing.T, c *Client, ctx context.Context) {
	t.Helper()
	if _, err := c.Do(ctx, Call{URL: c.hostAPI + "/x/stream", Buvid: true}); err != nil {
		t.Fatal(err)
	}
}

// lastStreamCookie 是最近一次 /x/stream 请求带的 Cookie 头。
func lastStreamCookie(f *fakeBili) string {
	cookie := ""
	for _, rec := range f.requests() {
		if rec.Path == "/x/stream" {
			cookie = rec.Header.Get("Cookie")
		}
	}
	return cookie
}

// TestFailedBuvidIsNotRefetchedForAWhile 钉「取 buvid 失败后一分钟内不再去取」：
// 断网时每个取流请求不用各等一次超时，请求照样发出去（不带 buvid）。
func TestFailedBuvidIsNotRefetchedForAWhile(t *testing.T) {
	var mu sync.Mutex
	fail := true
	f := buvidFixture(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		spiOK(t)(w, r)
	})
	now := time.Unix(1702204169, 0)
	c := f.client(t, Options{Now: func() time.Time { return now }})

	for range 3 {
		askStream(t, c, context.Background())
	}
	if n := countPath(f, pathSPI); n != 1 {
		t.Errorf("失败之后又取了：spi 问了 %d 次，want 1", n)
	}
	if got := lastStreamCookie(f); strings.Contains(got, "buvid3") {
		t.Errorf("没取到 buvid 却带了 %q", got)
	}

	mu.Lock()
	fail = false
	mu.Unlock()
	now = now.Add(buvidRetryAfter + time.Second)
	askStream(t, c, context.Background())
	if n := countPath(f, pathSPI); n != 2 {
		t.Errorf("过了一分钟没有再取（spi %d 次）", n)
	}
	if got := lastStreamCookie(f); !strings.Contains(got, "buvid3=B3") || !strings.Contains(got, "buvid4=B4") {
		t.Errorf("再取成功后 Cookie = %q，want 带上 buvid3/buvid4", got)
	}
}

// TestCanceledBuvidFetchIsNotCachedAsAFailure 钉「调用方自己取消的那一次不算失败」：
// 用户停掉一首歌，不该让下一首一分钟内都拿不到 buvid。
func TestCanceledBuvidFetchIsNotCachedAsAFailure(t *testing.T) {
	f := buvidFixture(t, spiOK(t))
	c := f.client(t, Options{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := c.buvidCookie(ctx); got != "" {
		t.Fatalf("取消了还取到了 %q", got)
	}

	askStream(t, c, context.Background())
	if got := lastStreamCookie(f); !strings.Contains(got, "buvid3=B3") {
		t.Errorf("取消之后的下一次 Cookie = %q，want 带上 buvid3", got)
	}
}

// TestConcurrentBuvidCallersShareOneFetch 钉「同时来的请求只取一次 buvid、都拿到它」，用 -race 跑。
func TestConcurrentBuvidCallersShareOneFetch(t *testing.T) {
	f := buvidFixture(t, func(w http.ResponseWriter, r *http.Request) {
		// 慢一点，让并发真的撞上。
		time.Sleep(20 * time.Millisecond)
		spiOK(t)(w, r)
	})
	c := f.client(t, Options{})

	const n = 12
	got := make([]string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i] = c.buvidCookie(context.Background())
		}()
	}
	wg.Wait()

	if k := countPath(f, pathSPI); k != 1 {
		t.Errorf("%d 个并发调用问了 %d 次 spi，want 1", n, k)
	}
	for i, g := range got {
		if g != "buvid3=B3; buvid4=B4" {
			t.Errorf("第 %d 个拿到 %q", i, g)
		}
	}
}
