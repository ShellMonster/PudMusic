package hotwords

// 网易云热搜词那一个接口的测试。全部走本地 fixture，不碰真的 music.163.com。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// fixture 是一台只会回一份固定 body 的服务器，把收到的请求记下来。
type fixture struct {
	*httptest.Server

	mu     sync.Mutex
	status int    // 非 0 时一律回这个状态码
	body   string // 非 0 status 时忽略
	seen   *http.Request
}

func newFixture(t *testing.T, status int, body string) *fixture {
	t.Helper()
	f := &fixture{status: status, body: body}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.seen = r
		status, body := f.status, f.body
		f.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fixture) last() *http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seen
}

func clientAt(t *testing.T, base string) *Client {
	t.Helper()
	c := New(nil)
	c.TestOnlyPointHostsAtFixture(base)
	return c
}

// TestFetchReturnsTheWordsInTheOrderGiven 钉最基本的：词按网易云给的顺序回来，
// 空白剥掉、重复去掉、空串丢掉。
func TestFetchReturnsTheWordsInTheOrderGiven(t *testing.T) {
	f := newFixture(t, 0, `{"code":200,"result":{"hots":[
		{"first":"季候风"},{"first":" The Weeknd "},{"first":""},
		{"first":"季候风"},{"first":"我不难过"}]}}`)

	got, err := clientAt(t, f.URL).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"季候风", "The Weeknd", "我不难过"}
	if len(got) != len(want) {
		t.Fatalf("拿到 %q，want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 个词 = %q，want %q", i, got[i], want[i])
		}
	}
}

// TestFetchAsksForSongsAndCapsTheList 钉请求本身：type=1 是「按歌曲搜」（和歌词
// 那一层同一个语义），limit 顶在我们自己要的数上。不带 type 的请求网易云直接
// 回 400「参数错误」（实测）。
func TestFetchAsksForSongsAndCapsTheList(t *testing.T) {
	f := newFixture(t, 0, `{"code":200,"result":{"hots":[{"first":"词"}]}}`)

	if _, err := clientAt(t, f.URL).Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := f.last()
	if r == nil {
		t.Fatal("没发请求")
	}
	if got := r.URL.Query().Get("type"); got != "1" {
		t.Errorf("type = %q，want 1（按歌曲搜）", got)
	}
	if got := r.URL.Query().Get("limit"); got == "" {
		t.Error("没带 limit")
	}
	for k, want := range map[string]string{
		"Referer": "https://music.163.com/",
		"Origin":  "https://music.163.com",
	} {
		if got := r.Header.Get(k); got != want {
			t.Errorf("%s = %q，want %q（网易云没有 Referer 会拒）", k, got, want)
		}
	}
}

// TestFetchCapsTheWordCount 钉条数上限：接口给多少都不照单全收。
func TestFetchCapsTheWordCount(t *testing.T) {
	body := `{"code":200,"result":{"hots":[`
	for i := range 30 {
		if i > 0 {
			body += ","
		}
		body += `{"first":"词"}`
	}
	body += `]}}`
	f := newFixture(t, 0, body)

	got, err := clientAt(t, f.URL).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > hotwordsLimit {
		t.Errorf("拿到 %d 条，want 不超过 %d", len(got), hotwordsLimit)
	}
}

// TestAnEmptyListIsNotAnError 钉「一个都没有」和「拉不到」是两回事。
// 网易云状态异常时给的形状五花八门：`result` 缺失、`hots` 是 null、hots 是空数组
// ——三者都算「这一趟没给词」，不算错误（是不是「拉不到」由 HTTP/code/形状说了算）。
func TestAnEmptyListIsNotAnError(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"hots 空数组", `{"code":200,"result":{"hots":[]}}`},
		{"hots 是 null", `{"code":200,"result":{"hots":null}}`},
		{"result 缺失", `{"code":200}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, 0, tc.body)
			got, err := clientAt(t, f.URL).Fetch(context.Background())
			if err != nil {
				t.Fatalf("%s 不该是错误：%v", tc.name, err)
			}
			if len(got) != 0 {
				t.Errorf("%s 拿到 %q，want 空", tc.name, got)
			}
		})
	}
}

// TestFailuresAreErrors 钉失败档：HTTP 非 200、body code 非 200、形状读不懂
// （网易云状态异常时给过 HTML 错误页和空 body，都落在这一档）。这一层不静默
// ——「拉不到」和「没有词」的分家在这一层做完，上面才有的分。
func TestFailuresAreErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"HTTP 500", 500, `{"code":200,"result":{"hots":[{"first":"词"}]}}`},
		{"body code 400", 0, `{"code":400,"msg":"参数错误"}`},
		{"code 缺失", 0, `{"result":{"hots":[{"first":"词"}]}}`},
		{"HTML 错误页", 0, `<html><body>oops</body></html>`},
		{"空 body", 0, ``},
		{"形状读不懂", 0, `{"code":200,"result":{"hots":"不是数组"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, tc.status, tc.body)
			if _, err := clientAt(t, f.URL).Fetch(context.Background()); err == nil {
				t.Errorf("%s 竟然不是错误", tc.name)
			}
		})
	}
}
