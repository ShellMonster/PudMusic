package bili

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// # 关于这些测试都是 fixture，不是真网
//
// 真 B 站的响应字段（尤其 `nav` 的 `wbi_img`、扫码登录的 `data.code`）
// 本地是造不出来的——造出来的那份只证明「我们读得懂我们造的东西」。
//
// 所以这个文件里**没有一个断言是在证明「B 站真的这么返回」**。它证明的是：
//
//   - 协议形状对不对（状态码在哪一层、哪个码对应哪个态）
//   - 我们的客户端在各种**失败**形态下行为对不对（HTTP 500、HTML、
//     非法 JSON、限流、风控）
//   - WBI 签名算出来的东西跟独立实现一致
//
// 跟真网对不上的部分写在这个文件顶部的 `TestAgainstTheRealBiliAPIIsNotCovered`
// 里——它不是测试，是一个会在真机上跑之前提醒人的注释。
// 真实的接口兼容性只能靠真机回归，见任务报告。

// ─────────────────────────────────────────────────────────────────────────────
// fixture 服务器
// ─────────────────────────────────────────────────────────────────────────────

// fakeBili 是一台假的 B 站服务器。
type fakeBili struct {
	*httptest.Server
	mu sync.Mutex
	// seen 记下每个请求：路径、查询、头、body。
	seen []recordedRequest
	// handlers 按路径给不同的回应。找不到就用 default。
	handlers map[string]http.HandlerFunc
	// cookies 是扫码成功那次要下发的 Set-Cookie。
	cookies []*http.Cookie
}

type recordedRequest struct {
	Path string
	// Method 记下 HTTP 方法。之前只能靠 `Form != nil` 猜是不是 POST，而那只对
	// 带表单的那几个成立——断言「这四个接口全是 GET」需要的就是这一个字段。
	Method string
	Query  url.Values
	Form   url.Values
	Header http.Header
	Body   string
}

func newFakeBili(t *testing.T) *fakeBili {
	t.Helper()
	f := &fakeBili{handlers: map[string]http.HandlerFunc{}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		rec := recordedRequest{
			Path: r.URL.Path, Method: r.Method,
			Query: r.URL.Query(), Header: r.Header.Clone(),
		}
		// ⚠️ **先把 body 读出来，再把它装回去，最后才 ParseForm。**
		//
		// 顺序反了的话，所有 POST 表单的 `rec.Body` 都是**空串**：
		// `Client.Do` 现在给带表单的请求发
		// `Content-Type: application/x-www-form-urlencoded`（那是对着 B 站的
		// servlet 修的根因——少了它 B 站不解析请求体，关注会**静默失败**），
		// 而 `r.ParseForm()` 遇到能解析的表单会把 `r.Body` **读空**（读完不还）。
		// 于是「原文」和「解析出来的表单」只能留一个，而这一份假服务器要
		// **两个都给**：`rec.Form` 给那些断言表单字段的用例，`rec.Body` 给
		// 那些断言「发出去的原文是什么」的用例。
		var sb strings.Builder
		if r.Body != nil {
			_, _ = io.Copy(&sb, r.Body)
			_ = r.Body.Close()
			r.Body = io.NopCloser(strings.NewReader(sb.String()))
		}
		rec.Body = sb.String()
		if r.Method == http.MethodPost {
			_ = r.ParseForm()
			rec.Form = r.PostForm
		}
		f.seen = append(f.seen, rec)
		h, ok := f.handlers[r.URL.Path]
		f.mu.Unlock()

		if ok {
			// 扫码登录成功那一次要把 cookie 种下去。
			f.mu.Lock()
			cs := f.cookies
			f.mu.Unlock()
			for _, c := range cs {
				http.SetCookie(w, c)
			}
			h(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeBili) on(path string, h http.HandlerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[path] = h
}

// requests 返回录下来的请求。
func (f *fakeBili) requests() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedRequest(nil), f.seen...)
}

// client 建一个指向这台假服务器的客户端。
func (f *fakeBili) client(t *testing.T, o Options) *Client {
	t.Helper()
	c := NewClient(o)
	c.hostAPI = f.URL
	c.hostPassport = f.URL
	c.hostMember = f.URL
	c.hostWWW = f.URL
	c.sessionBase = f.URL
	return c
}

// jsonData 造一个 B 站外壳。
func jsonData(t *testing.T, code int, message string, data any) string {
	t.Helper()
	body := map[string]any{"code": code, "message": message, "ttl": 1}
	if data != nil {
		body["data"] = data
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func writeJSON(w http.ResponseWriter, s string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(s))
}

// ─────────────────────────────────────────────────────────────────────────────
// 申请二维码
// ─────────────────────────────────────────────────────────────────────────────

func TestNewQRCodeReadsTheURLAndTheKey(t *testing.T) {
	f := newFakeBili(t)
	const wantURL = "https://passport.bilibili.com/h5-app/passport/login/scan?qrcode_key=abc"
	f.on(pathQRCodeGenerate, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"url":        wantURL,
			"qrcode_key": "8f2c1d4e5a6b7c8d9e0f1a2b3c4d5e6f",
		}))
	})

	qr, err := f.client(t, Options{}).NewQRCode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if qr.URL != wantURL || qr.Key != "8f2c1d4e5a6b7c8d9e0f1a2b3c4d5e6f" {
		t.Errorf("拿到的二维码不对：%+v", qr)
	}
}

// TestNewQRCodeRendersLocally 验「二维码本地渲染」。
//
// 这条是验收项。二维码的**内容**是一次性的登录授权，
// 拿不到「本地图」这个证据，这条验收就等于没做。
func TestNewQRCodeRendersLocally(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathQRCodeGenerate, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"url":        "https://passport.bilibili.com/h5-app/passport/login/scan?qrcode_key=8f2c1d4e",
			"qrcode_key": "8f2c1d4e5a6b7c8d9e0f1a2b3c4d5e6f",
		}))
	})

	qr, err := f.client(t, Options{}).NewQRCode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	code, err := qr.Render()
	if err != nil {
		t.Fatal(err)
	}
	// 编出来的矩阵要能解回原始地址——这是「图上确实有那条地址」的证据。
	got, err := decodeForTest(code)
	if err != nil {
		t.Fatalf("本地画的二维码读不回来：%v", err)
	}
	if got != qr.URL {
		t.Errorf("二维码里的内容是 %q，want %q", got, qr.URL)
	}

	// 除了两个接口，**一个请求都不该发出去**——图是我们自己算的。
	for _, req := range f.requests() {
		if req.Path != pathQRCodeGenerate {
			t.Errorf("为了画二维码多发了一个请求：%s", req.Path)
		}
	}
}

// TestNewQRCodeRejectsAnEmptyResult 验 B 站给空结果时报错。
//
// B 站在被风控时会返回 `code:0` 但 `url` 为空。不检查的话
// 用户看到的是一片白——而白屏 QR 码不像错误，像 bug。
func TestNewQRCodeRejectsAnEmptyResult(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathQRCodeGenerate, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"url": "", "qrcode_key": ""}))
	})
	if _, err := f.client(t, Options{}).NewQRCode(context.Background()); err == nil {
		t.Fatal("空结果也当成功返回了，界面上会是一张白图")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 三种状态
// ─────────────────────────────────────────────────────────────────────────────

// TestPollScanCoversTheThreeRealStates 是本文件最重要的一条。
//
// 三个真实分支一个都不能少，少一个用户就卡在转圈里：
//
//	86101 还没扫     → 继续等，**不是**结束
//	86090 已扫待确认 → 继续等，而且界面必须换一句提示
//	86038 过期       → 结束（要重新申请一张）
//	0     成功       → 结束，凭据已经在 cookie 里
func TestPollScanCoversTheThreeRealStates(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
		want ScanState
	}{
		{
			name: "还没扫",
			body: map[string]any{"code": 86101, "message": "未扫码", "url": "", "refresh_token": "", "timestamp": 0},
			want: ScanWaiting,
		},
		{
			name: "已扫待确认",
			body: map[string]any{"code": 86090, "message": "扫码成功未确认", "url": "", "refresh_token": "", "timestamp": 0},
			want: ScanScanned,
		},
		{
			name: "成功",
			body: map[string]any{"code": 0, "message": "0", "url": "https://passport.bili.com/x", "refresh_token": "rt-value", "timestamp": 1728000000000},
			want: ScanConfirmed,
		},
		{
			name: "过期",
			body: map[string]any{"code": 86038, "message": "二维码已失效", "url": "", "refresh_token": "", "timestamp": 0},
			want: ScanExpired,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeBili(t)
			f.on(pathQRCodePoll, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, jsonData(t, 0, "0", c.body))
			})
			res, err := f.client(t, Options{}).PollScan(context.Background(), "some-key")
			if err != nil {
				t.Fatal(err)
			}
			if res.State != c.want {
				t.Errorf("状态是 %v（%s），want %v", res.State, res.State, c.want)
			}
			if c.want == ScanConfirmed {
				if res.RefreshToken != "rt-value" {
					t.Errorf("成功时没拿到 refresh_token：%+v", res)
				}
				if res.LoggedInAt.IsZero() {
					t.Error("成功时没拿到登录时刻")
				}
			}
			if c.want.Terminal() != (c.want == ScanConfirmed || c.want == ScanExpired) {
				t.Errorf("Terminal() 说 %v 不对", c.want.Terminal())
			}
			if c.want.String() == "" {
				t.Error("状态没有对应的一句话")
			}
		})
	}
}

// TestPollReadsTheCodeFromDataNotTheEnvelope 是最容易写错的一条。
//
// **外层 code 恒为 0，真正的状态在 data.code 里。**
// 写成 `if resp.Code == 86038` 的话条件永远不成立，
// 轮询循环永远不会退出——用户看到的就是一个永远转圈的界面。
//
// 这条用 fixture 把外层固定成 0、内层给各个状态码，
// 正好复现「只看外层」的那个 bug。
func TestPollReadsTheCodeFromDataNotTheEnvelope(t *testing.T) {
	for _, inner := range []int{0, 86038, 86090, 86101} {
		f := newFakeBili(t)
		f.on(pathQRCodePoll, func(w http.ResponseWriter, r *http.Request) {
			// 外层恒为 0（真 B 站就是这样的），内层才是状态。
			writeJSON(w, jsonData(t, 0, "0", map[string]any{
				"code": inner, "message": "inner", "refresh_token": "rt",
			}))
		})
		res, err := f.client(t, Options{}).PollScan(context.Background(), "k")
		if err != nil {
			t.Fatal(err)
		}
		want := scanResultOf(qrcodePollData{Code: inner}).State
		if res.State != want {
			t.Errorf("内层 %d 读成了 %v，want %v", inner, res.State, want)
		}
	}
}

// TestPollSendsTheKey 验 qrcode_key 真的发出去了。
func TestPollSendsTheKey(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathQRCodePoll, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"code": 86101}))
	})
	if _, err := f.client(t, Options{}).PollScan(context.Background(), "the-key"); err != nil {
		t.Fatal(err)
	}
	reqs := f.requests()
	if len(reqs) != 1 || reqs[0].Query.Get("qrcode_key") != "the-key" {
		t.Errorf("请求没带上 qrcode_key：%+v", reqs)
	}
}

func TestPollWithoutAKeyDoesNotSendAnything(t *testing.T) {
	f := newFakeBili(t)
	c := f.client(t, Options{})
	if _, err := c.PollScan(context.Background(), ""); err == nil {
		t.Fatal("没有 key 也去轮询了")
	}
	if n := len(f.requests()); n != 0 {
		t.Errorf("没有 key 还是发了 %d 个请求", n)
	}
}

// TestUnknownScanCodeDoesNotEndTheFlow 验不认识的码不猜。
//
// 猜成「已过期」的话，用户明明已经扫了却被告知二维码失效，
// 于是退出重来——而真实原因（B 站加了个新状态）永远查不出来。
func TestUnknownScanCodeDoesNotEndTheFlow(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathQRCodePoll, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"code": 99999, "message": "未来"}))
	})
	res, err := f.client(t, Options{}).PollScan(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if res.State.Terminal() {
		t.Errorf("不认识的码 %v 被当成了终态，轮询会停", res.State)
	}
	if !strings.Contains(res.Message, "99999") {
		t.Errorf("不认识的码没有留痕，排查不了：%q", res.Message)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// cookie：扫码成功之后凭据从哪来
// ─────────────────────────────────────────────────────────────────────────────

// TestScanSuccessLandsTheCookiesInTheJar 验「B 站说成功了，凭据就在 jar 里」。
func TestScanSuccessLandsTheCookiesInTheJar(t *testing.T) {
	f := newFakeBili(t)
	// 不写 Domain：cookiejar 按域匹配，fixture 的请求打在 127.0.0.1 上，
	// 写了 .bilibili 就会被丢掉，于是测试会看到「B 站明明下了 cookie，
	// jar 里却没有」。真实环境里那个域是对的（请求打在
	// passport.bilibili.com 上），fixture 里必须去掉才能测到同一条路径。
	f.cookies = []*http.Cookie{
		{Name: "SESSDATA", Value: "sess-value-123456", Path: "/"},
		{Name: "bili_jct", Value: "jct-value-123456", Path: "/"},
	}
	f.on(pathQRCodePoll, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"code": 0, "refresh_token": "rt-123456", "timestamp": 1728000000000,
		}))
	})
	c := f.client(t, Options{})
	res, err := c.PollScan(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if res.State != ScanConfirmed {
		t.Fatalf("状态是 %v", res.State)
	}

	got := map[string]string{}
	for _, ck := range c.SessionCookies() {
		got[ck.Name] = ck.Value
	}
	if got["SESSDATA"] != "sess-value-123456" || got["bili_jct"] != "jct-value-123456" {
		t.Errorf("jar 里没有登录 cookie：%+v", got)
	}
}

// TestSeedCookiesReplacesRatherThanAdds 验换 jar 是**整体替换**。
//
// 这一条是「退登会清掉凭据」的落点：`http.CookieJar` 没有删除接口，
// 逐条删要遍历所有域名猜路径，漏一条就是「退登没退干净」，
// 而现象是用户在共享电脑上以为已经退出，B 站那边会话还有效。
func TestSeedCookiesReplacesRatherThanAdds(t *testing.T) {
	c := NewClient(Options{})
	old := []*http.Cookie{{Name: "SESSDATA", Value: "old-1234567890", Path: "/"}}
	if err := c.SeedCookies(old); err != nil {
		t.Fatal(err)
	}
	if got := c.SessionCookies(); len(got) == 0 {
		t.Fatal("装上去之后 jar 是空的")
	}

	if err := c.SeedCookies(nil); err != nil {
		t.Fatal(err)
	}
	if got := c.SessionCookies(); len(got) != 0 {
		t.Errorf("换 jar 之后还剩 %d 条 cookie：%+v", len(got), got)
	}

	fresh := []*http.Cookie{{Name: "SESSDATA", Value: "new-0987654321", Path: "/"}}
	if err := c.SeedCookies(fresh); err != nil {
		t.Fatal(err)
	}
	got := c.SessionCookies()
	if len(got) != 1 || got[0].Value != "new-0987654321" {
		t.Errorf("换新 jar 之后是 %+v", got)
	}
}

// TestSeedCookiesBeforeAnythingWorks 没登录时 jar 为空，不是崩溃。
func TestSeedCookiesBeforeAnythingWorks(t *testing.T) {
	c := NewClient(Options{})
	if got := c.SessionCookies(); got != nil {
		t.Errorf("还没装 cookie 就读出了 %+v", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 错误分类
// ─────────────────────────────────────────────────────────────────────────────

func TestErrorCodesMapToSentinels(t *testing.T) {
	cases := []struct {
		code int
		want error
	}{
		{-101, ErrNotLoggedIn},
		{-111, ErrCSRF},
		{-352, ErrRiskControl},
		{-412, ErrRiskControl},
		{-509, ErrTooFrequent},
	}
	for _, c := range cases {
		f := newFakeBili(t)
		code := c.code
		f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, jsonData(t, code, "some message", nil))
		})
		_, err := f.client(t, Options{}).Nav(context.Background())
		if !errors.Is(err, c.want) {
			t.Errorf("code=%d 得到 %v，want %v", c.code, err, c.want)
		}
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Errorf("code=%d 不是一个 *APIError：%T", c.code, err)
		} else if apiErr.Code != c.code {
			t.Errorf("APIError.Code 是 %d，want %d", apiErr.Code, c.code)
		}
	}
}

// TestUnknownCodeIsJustAnAPIError 不认识的码不该被当成「没登录」。
func TestUnknownCodeIsJustAnAPIError(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, -400, "请求错误", nil))
	})
	_, err := f.client(t, Options{}).Nav(context.Background())
	if errors.Is(err, ErrNotLoggedIn) {
		t.Error("-400 被当成了没登录")
	}
	if !strings.Contains(err.Error(), "-400") {
		t.Errorf("错误文本里没有错误码：%v", err)
	}
}

// TestNonJSONResponseIsRiskControl 验被风控时的形态。
//
// B 站被风控会返回一页 HTML。报成「网络错误」的话用户会去查网线，
// 而真实原因是「等一会儿再试」。
func TestNonJSONResponseIsRiskControl(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>412 Precondition Failed</body></html>"))
	})
	_, err := f.client(t, Options{}).Nav(context.Background())
	if !errors.Is(err, ErrRiskControl) {
		t.Errorf("拿到 HTML 时返回 %v，want ErrRiskControl", err)
	}
}

// TestHTTPErrorIsANetworkError 验非 2xx 的归类。
func TestHTTPErrorIsANetworkError(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	_, err := f.client(t, Options{}).Nav(context.Background())
	if !errors.Is(err, ErrNetwork) {
		t.Errorf("HTTP 502 返回 %v，want ErrNetwork", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 请求头、CSRF
// ─────────────────────────────────────────────────────────────────────────────

func TestEveryRequestCarriesTheBrowserHeaders(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"isLogin": false}))
	})
	f.client(t, Options{}).Nav(context.Background())

	reqs := f.requests()
	if len(reqs) != 1 {
		t.Fatalf("发了 %d 个请求", len(reqs))
	}
	h := reqs[0].Header
	for name, want := range map[string]string{
		"User-Agent": DefaultUserAgent,
		"Referer":    DefaultReferer,
		"Origin":     "https://www.bilibili.com",
	} {
		if got := h.Get(name); got != want {
			t.Errorf("%s 是 %q，want %q", name, got, want)
		}
	}
}

// TestCSRFIsInjectedOnWriteCalls 验写接口自动带 csrf。
//
// 两个地方都要带：查询参数和 `bili-jct` 头。
// 只带一个的话 B 站返回的是 -111「CSRF 校验失败」，
// 而同一个 -111 在别的场合表示风控，极难查。
func TestCSRFIsInjectedOnWriteCalls(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathExit, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", nil))
	})
	c := f.client(t, Options{CSRF: func() string { return "the-jct-value" }})
	c.Logout(context.Background())

	reqs := f.requests()
	if len(reqs) != 1 {
		t.Fatalf("发了 %d 个请求", len(reqs))
	}
	if got := reqs[0].Query.Get("csrf"); got != "the-jct-value" {
		t.Errorf("csrf 参数是 %q", got)
	}
	if got := reqs[0].Header.Get(CSRFHeaderName); got != "the-jct-value" {
		t.Errorf("%s 头是 %q", CSRFHeaderName, got)
	}
}

// TestNoCSRFWhenLoggedOut 登录状态下不塞空 csrf。
func TestNoCSRFWhenLoggedOut(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathExit, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", nil))
	})
	f.client(t, Options{CSRF: func() string { return "" }}).Logout(context.Background())

	if got := f.requests()[0].Header.Get(CSRFHeaderName); got != "" {
		t.Errorf("没登录也塞了 CSRF 头：%q", got)
	}
}

// TestOnRequestSeesTheFullURL 验回调拿到的地址是拼好的。
//
// 它是**未脱敏**的（签名参数都在里面）——脱敏是回调自己的责任，
// 见 app.RequestLogger。
func TestOnRequestSeesTheFullURL(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"isLogin": false}))
	})
	var seen string
	f.client(t, Options{OnRequest: func(method, rawURL string) {
		seen = method + " " + rawURL
	}}).Nav(context.Background())

	if !strings.Contains(seen, pathNav) {
		t.Errorf("回调没拿到完整地址：%q", seen)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// nav：登录态判定
// ─────────────────────────────────────────────────────────────────────────────

func TestNavReadsIsLoginWithoutErroring(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"isLogin": true, "mid": 424242, "uname": "某用户",
		}))
	})
	nav, err := f.client(t, Options{}).Nav(context.Background())
	if err != nil {
		t.Fatalf("有登录态时 nav 报了错：%v", err)
	}
	if !nav.LoggedIn || nav.Mid != 424242 || nav.Uname != "某用户" {
		t.Errorf("nav 解错了：%+v", nav)
	}
}

// TestNavNotLoggedInIsNotAnError 验「没登录」是正常状态。
func TestNavNotLoggedInIsNotAnError(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"isLogin": false}))
	})
	nav, err := f.client(t, Options{}).Nav(context.Background())
	if err != nil {
		t.Fatalf("没登录不该是错误：%v", err)
	}
	if nav.LoggedIn {
		t.Error("没登录却报成登录了")
	}
}

// TestNavCarriesMyOwnFace 验 nav 顺带把我自己的头像带回来（issue #95）。
//
// ⚠️ **钉的是「已经拼好了」而不是「原样透传」**：B 站给的是协议相对的原图，
// 补协议和拼裁剪参数是这一层的活（和 `FollowUser.Face` 同一份做法，见
// follow.go 的文件头）。写成「拿到什么给什么」的话，界面那层就得抄一遍这段，
// 而漏掉裁剪参数的后果是**顶栏那个 28 见方的一格去下几百 KB 的正方形原图**——
// 不报错、不变慢、只是每次都下。
func TestNavCarriesMyOwnFace(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{
			"isLogin": true, "mid": 2, "face": "//i0.hdslb.com/bfs/face/mine.jpg",
		}))
	})
	nav, err := f.client(t, Options{}).Nav(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := "https://i0.hdslb.com/bfs/face/mine.jpg@" + avatarSize + ".webp"
	if nav.Face != want {
		t.Errorf("头像地址是 %q，want %q（协议没补或裁剪参数没拼）", nav.Face, want)
	}
}

// TestNavWithoutAFaceGivesNothingToShow 验 B 站没给地址时是空串，而不是半个地址。
//
// 「补协议」那一支最危险的是给空串补协议：结果是 `https:`，那是个能过
// `strings.HasPrefix(s, "http")` 的假地址，缓存会拿它去下、下不来、
// 于是界面上是一个永远转圈的头像。
func TestNavWithoutAFaceGivesNothingToShow(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, jsonData(t, 0, "0", map[string]any{"isLogin": true, "mid": 2}))
	})
	nav, err := f.client(t, Options{}).Nav(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if nav.Face != "" {
		t.Errorf("B 站没给头像却解出 %q", nav.Face)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 超时与取消
// ─────────────────────────────────────────────────────────────────────────────

func TestSlowServerIsCutOffByTheTimeout(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(3 * time.Second):
		case <-r.Context().Done():
		}
	})
	c := f.client(t, Options{})
	c.http.Timeout = 80 * time.Millisecond

	start := time.Now()
	_, err := c.Nav(context.Background())
	if err == nil {
		t.Fatal("超时了却没报错")
	}
	if !errors.Is(err, ErrNetwork) {
		t.Errorf("超时返回 %v，want ErrNetwork", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("等了 %v 才返回，超时没生效", d)
	}
}

func TestCancelStopsTheRequest(t *testing.T) {
	f := newFakeBili(t)
	f.on(pathNav, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(3 * time.Second):
		case <-r.Context().Done():
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	if _, err := f.client(t, Options{}).Nav(ctx); err == nil {
		t.Fatal("取消了却没报错")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 还没被 fixture 覆盖的部分
// ─────────────────────────────────────────────────────────────────────────────

// TestAgainstTheRealBiliAPIIsNotCovered 是一条**记录性质**的测试。
//
// 它把「这些测试证明不了什么」写成代码里的断言，免得哪天有人
// 看到满屏绿的测试就以为「登录这块验过了，可以发」。
func TestAgainstTheRealBiliAPIIsNotCovered(t *testing.T) {
	t.Log("以下都只有 fixture 覆盖，必须真机回归：")
	t.Log("  1. B 站实际的字段名和嵌套层次（这里照 Biu 上游的 TS 类型抄的）")
	t.Log("  2. wbi_img 的两个 key 真的能解出可用的混合密钥")
	t.Log("  3. 三个状态码在 2026 年还是这几个")
	t.Log("  4. 扫码成功那次响应里真的带 SESSDATA（fixture 里是我们自己种的）")
	t.Log("  5. 本地画的二维码 B 站 App 真的扫得出来")
	t.Log("  6. 本机网络能不能连到 passport.bilibili.com")
}

// TestACancelledRequestStillLooksCancelled 钉住「被取消」这件事在包出去之后还
// 认得出来。
//
// 断的是错误链而不是错误消息：`errors.Is(err, context.Canceled)` 必须成立。
//
// ⚠️ 这一句现在只是末尾一个动词的差别（`%v` → `%w`，见 `Client.Do` 那一段的
// 注释），而它决定的是**下载记录页上按「暂停」之后显示什么**：下载队列判
// 「这一项是用户停掉的、不是失败」靠的就是它，而取流途中那一段（几百毫秒）
// 恰恰是最容易被用户按停的时候——用 `%v` 时那一刻界面上是一行红色的「任务出错」
// 加一颗「重试」。
func TestACancelledRequestStillLooksCancelled(t *testing.T) {
	f := newFakeBili(t)
	// 一直挂着，直到这一发请求被取消为止。
	f.on(pathNav, func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	c := f.client(t, Options{})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// 等请求真的发出去了再取消（否则可能取消得太早，连请求都没造出来）。
		for range 2000 {
			if len(f.requests()) > 0 {
				break
			}
			time.Sleep(time.Millisecond)
		}
		cancel()
	}()
	defer cancel()

	_, err := c.Nav(ctx)
	if err == nil {
		t.Fatal("请求被取消了却没报错")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("取消之后 errors.Is(err, context.Canceled) 不成立：%v", err)
	}
	// 另一头仍然要成立：这一层包它就是为了分清「网断了」和别的。
	if !errors.Is(err, ErrNetwork) {
		t.Errorf("取消之后 errors.Is(err, ErrNetwork) 不成立了：%v", err)
	}
}
