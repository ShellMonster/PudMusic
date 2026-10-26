// 「这一趟不要 buvid」那一层的测试。
//
// # 为什么单独一个文件
//
// 这一层是**运输层**的活儿（摘 Cookie 头），而仓库里其它测试盯的都是
// 「接口回了什么」。混在一起会让「这一层到底有没有生效」看不出来——
// 而它**静默失效**的后果是「评论只剩 3 条」。
package bili

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// roundTripperFunc 把一个函数变成 RoundTripper。
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestNoBuvidTransportStripsTheBuvidFamily 验运输层那一层真的把 buvid 摘掉了。
//
// 🔴 **为什么摘 cookie 必须在运输层**：jar 里的 cookie 是在
// `http.Client.send` 里加的，而那**在运输层之前**
// （`net/http/client.go`：`c.Jar.Cookies(url)` → `req.AddCookie` →
// `RoundTrip`）。所以「在 `Do` 里删头」是**删不到的**——那一刻头还没长出来。
func TestNoBuvidTransportStripsTheBuvidFamily(t *testing.T) {
	for _, tc := range []struct {
		what string
		raw  string
		want string
	}{
		{"buvid3", "SESSDATA=abc; buvid3=XYZ", "SESSDATA=abc"},
		{"buvid4", "SESSDATA=abc; buvid4=XYZ", "SESSDATA=abc"},
		{"没有 3 的 buvid", "SESSDATA=abc; buvid=XYZ", "SESSDATA=abc"},
		{"buvid 在前", "buvid3=XYZ; SESSDATA=abc", "SESSDATA=abc"},
		{"引号的值原样留着", `SESSDATA="a;b"; buvid3=XYZ`, `SESSDATA="a;b"`},
		{"b_nut 留着", "b_nut=173; buvid3=XYZ", "b_nut=173"},
	} {
		t.Run(tc.what, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, "https://api.bilibili.com/x", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Cookie", tc.raw)
			stripBuvidCookies(req)
			if got := req.Header.Get("Cookie"); got != tc.want {
				t.Errorf("Cookie = %q，want %q", got, tc.want)
			}
		})
	}
}

// TestNoBuvidTransportKeepsTheLoginState 验摘设备标识**不摘凭据**。
//
// SESSDATA / bili_jct 少了任何一个，登录用户看到的评论会比匿名用户还少。
func TestNoBuvidTransportKeepsTheLoginState(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://api.bilibili.com/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", "SESSDATA=abc; bili_jct=def; buvid3=XYZ")
	stripBuvidCookies(req)
	got := req.Header.Get("Cookie")
	if !strings.Contains(got, "SESSDATA=abc") || !strings.Contains(got, "bili_jct=def") {
		t.Errorf("登录态被一起摘掉了：%q", got)
	}
}

// TestNoBuvidTransportDoesNotTouchTheCallersRequest 验它**不改**调用方的
// Request。
//
// RoundTripper 的约定是不能改调用方传进来的 Request——直接改的话调用方那边
// 看到的头会凭空少一样，而那种错极难查。
func TestNoBuvidTransportDoesNotTouchTheCallersRequest(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://api.bilibili.com/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", "SESSDATA=abc; buvid3=XYZ")
	tr := &noBuvidTransport{base: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("{}")),
			Header:     http.Header{},
		}, nil
	})}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got := req.Header.Get("Cookie"); !strings.Contains(got, "buvid3=XYZ") {
		t.Errorf("调用方那个请求被改了：%q", got)
	}
}

// TestNoBuvidWinsOverBuvid 验两个开关都写时 **`NoBuvid` 赢**。
//
// 多带 buvid3 的后果是**静默**少拿数据（评论列表只剩 3 条），少带顶多是
// B 站觉得这个客户端「像机器人」——两边的错法不对称，所以往严的那边退。
func TestNoBuvidWinsOverBuvid(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"isLogin": false}))
	})
	f.on("/x/frontend/finger/spi", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "ok", map[string]any{
			"b_3": "BUVID3-FROM-SPI",
			"b_4": "BUVID4-FROM-SPI",
		}))
	})
	c := f.client(t, Options{})
	if err := c.SeedCookies([]*http.Cookie{
		{Name: "SESSDATA", Value: "abc"},
		{Name: "buvid3", Value: "JAR-BUVID"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Do(context.Background(), Call{
		Method:  "GET",
		URL:     f.URL + pathNav,
		Buvid:   true,
		NoBuvid: true,
	}); err != nil {
		t.Fatal(err)
	}
	reqs := f.requests()
	if len(reqs) == 0 {
		t.Fatal("没有发请求")
	}
	ck := reqs[len(reqs)-1].Header.Get("Cookie")
	if strings.Contains(ck, "buvid") {
		t.Errorf("NoBuvid 没赢：Cookie = %q", ck)
	}
}

// TestTheCommentPathIsTheOnlyOneThatAsksForNoBuvid 盯住「这个开关只用在
// 评论那两个接口上」。
//
// ⚠️ **别让它扩散**：带 buvid 的接口（取流）**要**带它。开关一旦被顺手
// 复制到别的接口上，取流就会开始只回一个 v_voucher。
func TestTheCommentPathIsTheOnlyOneThatAsksForNoBuvid(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"isLogin": false}))
	})
	c := f.client(t, Options{})
	if err := c.SeedCookies([]*http.Cookie{
		{Name: "SESSDATA", Value: "abc"},
		{Name: "buvid3", Value: "JAR-BUVID"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Do(context.Background(), Call{
		Method: "GET",
		URL:    f.URL + pathNav,
	}); err != nil {
		t.Fatal(err)
	}
	reqs := f.requests()
	if len(reqs) == 0 {
		t.Fatal("没有发请求")
	}
	// 没开 NoBuvid 时，jar 里的东西**照发**——这一层是「显式不要」，
	// 不是「默认都要摘」。
	ck := reqs[len(reqs)-1].Header.Get("Cookie")
	if !strings.Contains(ck, "buvid3=JAR-BUVID") {
		t.Errorf("没开 NoBuvid 的那一趟把 jar 里的 buvid3 摘了：%q", ck)
	}
}

// TestTheNoBuvidPathIsSafeToCallConcurrently 盯住「这一层是并发安全的」。
//
// ⚠️ **为什么值得单独钉一条**：这一层每次调用都**复制一份** `http.Client`
// （为了换掉 Transport 而不动 jar / Timeout / CheckRedirect）。复制本身是
// 安全的（`http.Client` 里没有锁），但「同一份 jar 被多个 goroutine 读」
// 不是——所以必须用 `-race` 跑这一条才有意义。
//
// 症状不是报错，而是**偶发**少一条 buvid 摘干净、多一条 jar 没被摘。
func TestTheNoBuvidPathIsSafeToCallConcurrently(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"isLogin": false}))
	})
	c := f.client(t, Options{})
	if err := c.SeedCookies([]*http.Cookie{
		{Name: "SESSDATA", Value: "abc"},
		{Name: "buvid3", Value: "JAR-BUVID"},
	}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Do(context.Background(), Call{
				Method:  "GET",
				URL:     f.URL + pathNav,
				NoBuvid: true,
			}); err != nil {
				t.Errorf("并发调用报错：%v", err)
			}
		}()
	}
	wg.Wait()

	for _, rec := range f.requests() {
		if ck := rec.Header.Get("Cookie"); strings.Contains(ck, "buvid") {
			t.Errorf("并发时有一趟带出了 buvid：%q", ck)
		}
	}
}
