package app_test

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// 这一组验的是**生产的那一层**（`CredentialTransport`），不是策略本身。
//
// `credentials_test.go` 验的是「白名单长什么样」——纯逻辑，穷举得完。
// 这一组验的是「什么时候调它、调几遍」——那要跑真的 http.Client 才验得到，
// 而「少调一遍」在策略测试里是看不见的。
//
// # 为什么所有 fixture 都用**真主机名**
//
// httptest 起出来的服务器地址全是 `127.0.0.1:某端口`，只差端口。
// 而凭据白名单判的是 hostname——`127.0.0.1` 三个都一样。
//
// 于是「放行起点主机」等于**放行了后面所有跳**，「陌生主机」根本不存在，
// 测试却照样绿：它测到的是「白名单里那台机器能不能拿到凭据」，
// 不是「陌生主机拿不到」。
//
// 所以这里让不同主机名都打在同一台服务器上（拨号时指过去），
// 用的是 `api.bilibili.com` / `i0.hdslb.com` / `evil.example` 这些
// **真的在白名单里 / 真的不在白名单里**的名字。跑的是真策略，
// 不用 `AllowBiliHost` 去凑。

// hostFixture 让不同的主机名都打到同一台服务器上。
type hostFixture struct {
	*httptest.Server
	addr string
	mu   sync.Mutex
	// got 记下每个主机名收到的请求头。
	got map[string]received
}

type received struct {
	Cookie  string
	Referer string
	Host    string
	Auth    string
}

// newHostFixture 建一台按 Host 分发的服务器。
func newHostFixture(t *testing.T, routes map[string]http.HandlerFunc) *hostFixture {
	t.Helper()
	f := &hostFixture{got: map[string]received{}}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if i := strings.LastIndex(host, ":"); i > 0 && !strings.Contains(host[i:], "]") {
			host = host[:i]
		}

		f.mu.Lock()
		f.got[host] = received{
			Cookie:  r.Header.Get("Cookie"),
			Referer: r.Header.Get("Referer"),
			Host:    r.Host,
			Auth:    r.Header.Get("Authorization"),
		}
		f.mu.Unlock()

		if h, ok := routes[host]; ok {
			h(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(f.Close)
	f.addr = strings.TrimPrefix(f.URL, "https://")
	return f
}

// transport 把所有主机名都指到这台服务器。
//
// ⚠️ 证书校验被关掉了：服务器用的是 httptest 那张自签证书，
// 它的 CN 是 example.com，验 `api.bilibili.com` 必然失败。
// **只在测试里这么做**——它验的是「请求上带着什么」，
// 证书链那一层由「本机直连」保证，不经过任何中间人。
func (f *hostFixture) transport() *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, f.addr)
		},
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // 测试用自签证书
	}
}

// client 造一个配好凭据边界的客户端。
func (f *hostFixture) client(t *testing.T, extraBiliHosts ...string) *http.Client {
	t.Helper()
	p := app.DefaultInjectPolicy()
	for _, h := range extraBiliHosts {
		p = p.AllowBiliHost(h)
	}
	tr := &app.CredentialTransport{Base: f.transport(), Policy: p}
	tr.UserAgent = "biu-test/1.0"
	tr.Referer = "https://www.bilibili.com/"
	return tr.Client(nil)
}

// recv 读某台主机收到的请求。
func (f *hostFixture) recv(host string) received {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.got[host]
}

// withCookie 在请求上直接挂凭据头。
//
// ⚠️ 不走 jar：jar 是**按域匹配**的，这里主机名是真的 B 站域名，
// 但没有任何地方真的往 jar 里塞过东西，所以直接挂头才测得到这一层。
// 而且请求上本来就可能因为别的原因带着 Cookie（302 复制过来的、
// 上游代码手动设的），那一层要挡的正是这种情况。
func withCookie(req *http.Request, value string) *http.Request {
	req.Header.Set("Cookie", "SESSDATA="+value+"; other=1")
	req.Header.Set("bili-jct", value)
	req.Header.Set("Authorization", "Bearer "+value)
	req.Header.Set("Proxy-Authorization", "Basic "+value)
	return req
}

const testSecret = "abcdef123456"

// TestTransportStripsCredentialsFromForeignHosts 验陌生主机收不到凭据。
//
// 「陌生」用**看起来很像** B 站的名字来扮演：
// `api.bilibili.com.evil.net` 含有 "bilibili.com" 这个子串，
// 在原始 URL 字符串上做子串匹配就会中招。
func TestTransportStripsCredentialsFromForeignHosts(t *testing.T) {
	f := newHostFixture(t, nil)
	c := f.client(t)

	for _, host := range []string{
		"evil.example",
		"api.bilibili.com.evil.net",
		"notbilibili.com",
	} {
		res, err := c.Do(withCookie(mustReq(t, "https://"+host+"/steal"), testSecret))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()

		got := f.recv(host)
		if got.Cookie != "" {
			t.Errorf("%s 收到了 Cookie：%q", host, got.Cookie)
		}
		if got.Auth != "" {
			t.Errorf("%s 收到了 Authorization：%q", host, got.Auth)
		}
	}
}

// TestTransportKeepsCredentialsOnBiliAPI 是反方向：B 站 API 上的不能删。
func TestTransportKeepsCredentialsOnBiliAPI(t *testing.T) {
	f := newHostFixture(t, nil)
	c := f.client(t)

	res, err := c.Do(withCookie(mustReq(t, "https://api.bilibili.com/x/web-interface/nav"), testSecret))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	got := f.recv("api.bilibili.com")
	if !strings.Contains(got.Cookie, "SESSDATA="+testSecret) {
		t.Errorf("B 站 API 上没有 SESSDATA，拿到的是 %q", got.Cookie)
	}
	if got.Auth == "" {
		t.Error("B 站 API 上的 Authorization 被误删了")
	}
}

// TestTransportKeepsRefererOnCDNButNotTheCookie 验 CDN 那一档。
//
// CDN 在白名单里，但它**只该拿到 Referer**。
// 把 CDN 错放进「完整凭据」档的后果是：把 B 站的 SESSDATA 发给一台
// 存静态文件的机器。B 站改 CDN 域名的概率远大于新开 API 子域。
func TestTransportKeepsRefererOnCDNButNotTheCookie(t *testing.T) {
	f := newHostFixture(t, nil)
	c := f.client(t)

	res, err := c.Do(withCookie(mustReq(t, "https://i0.hdslb.com/bfs/archive/cover.jpg"), testSecret))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	got := f.recv("i0.hdslb.com")
	if got.Cookie != "" {
		t.Errorf("CDN 上收到了 Cookie：%q", got.Cookie)
	}
	if got.Referer == "" {
		t.Error("CDN 上没有 Referer，给了它就 403")
	}
}

// TestTransportGivesLyricSourcesNoBilibiliCredential 验歌词源。
//
// `interface.music.163.com` / `lrclib.net` 在白名单里，
// 但它们跟 B 站毫无关系——B 站的凭据一个都不能给。
func TestTransportGivesLyricSourcesNoBilibiliCredential(t *testing.T) {
	f := newHostFixture(t, nil)
	c := f.client(t)

	for _, host := range []string{"interface.music.163.com", "lrclib.net"} {
		res, err := c.Do(withCookie(mustReq(t, "https://"+host+"/api"), testSecret))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()

		got := f.recv(host)
		if got.Cookie != "" {
			t.Errorf("歌词源 %s 收到了 B 站的 Cookie：%q", host, got.Cookie)
		}
		if got.Referer == "" {
			t.Errorf("歌词源 %s 没有 Referer", host)
		}
	}
}

// TestTransportRechecksEveryRedirectHop 是 `PLAN.md` 阶段 3 的验收用例：
// 「用假 HTTP 服务断言 redirect 到陌生 host 时不带 Cookie」。
//
// 这一条最容易写错的地方是「只在第一个请求上判一次」。
// B 站和中间 CDN 的 302 是常态，一个请求走三四跳很常见；
// 不逐跳重判就等于把凭据跟着 302 送到了别处。
func TestTransportRechecksEveryRedirectHop(t *testing.T) {
	f := newHostFixture(t, map[string]http.HandlerFunc{
		"api.bilibili.com": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://evil.example/hop", http.StatusFound)
		},
		"evil.example": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://i0.hdslb.com/bfs/cover.jpg", http.StatusFound)
		},
		"i0.hdslb.com": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) },
	})
	c := f.client(t)

	res, err := c.Do(withCookie(mustReq(t, "https://api.bilibili.com/x/web-interface/nav"), testSecret))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	if !strings.Contains(f.recv("api.bilibili.com").Cookie, "SESSDATA="+testSecret) {
		t.Fatalf("起点就没有凭据，测的不是这件事：%q", f.recv("api.bilibili.com").Cookie)
	}
	if got := f.recv("evil.example").Cookie; got != "" {
		t.Errorf("中间那台陌生主机收到了 Cookie：%q", got)
	}
	if got := f.recv("i0.hdslb.com").Cookie; got != "" {
		t.Errorf("被 302 送过去的 CDN 上有 Cookie：%q", got)
	}
}

// TestTransportDropsCredentialsOnHTTPSDowngrade 是「逐跳重判」抓不到的那一类。
//
// 主机名没变、白名单认得它——所以只看主机的话凭据就顺着明文链路走了，
// SESSDATA 被一个中间人明文读走。
//
// `RedirectClass` 里那条「HTTPS 降级到 HTTP 一律清空」就是为了这个；
// 而它必须在**每一跳**都判，不能只在入口判一次。
func TestTransportDropsCredentialsOnHTTPSDowngrade(t *testing.T) {
	var plainSaw string
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		plainSaw = r.Header.Get("Cookie")
		w.WriteHeader(http.StatusOK)
	}))
	defer plain.Close()

	// 明文服务器也在白名单里——测试要验的正是
	//「白名单认它，但协议不认」。
	host, _, err := net.SplitHostPort(strings.TrimPrefix(plain.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	c := &app.CredentialTransport{
		Base:   &http.Transport{},
		Policy: app.DefaultInjectPolicy().AllowBiliHost(host),
	}
	c.UserAgent = "biu-test/1.0"

	res, err := c.Client(nil).Do(withCookie(mustReq(t, plain.URL+"/x"), testSecret))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	if plainSaw != "" {
		t.Errorf("明文链路上收到了 Cookie：%q", plainSaw)
	}
}

// TestTransportDoesNotMutateTheCallersRequest 验它不改调用方的请求。
//
// RoundTripper 的约定是不能改传进来的 Request。直接改的话，调用方那边
// 看到的请求凭空少了几个头，而「我明明设了 Cookie」极难查。
func TestTransportDoesNotMutateTheCallersRequest(t *testing.T) {
	f := newHostFixture(t, nil)
	tr := &app.CredentialTransport{Base: f.transport(), Policy: app.DefaultInjectPolicy()}

	req := withCookie(mustReq(t, "https://evil.example/x"), testSecret)
	res, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	if req.Header.Get("Cookie") == "" {
		t.Error("调用方的请求被改了：Cookie 被删了")
	}
	if req.Header.Get("bili-jct") == "" {
		t.Error("调用方的请求被改了：bili-jct 被删了")
	}
}

// TestTransportAddsTheDeclarationHeaders 验 UA / Referer 每次都带。
func TestTransportAddsTheDeclarationHeaders(t *testing.T) {
	f := newHostFixture(t, nil)
	c := f.client(t)

	res, err := c.Do(mustReq(t, "https://api.bilibili.com/x"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	got := f.recv("api.bilibili.com")
	if got.Referer != "https://www.bilibili.com/" {
		t.Errorf("Referer 是 %q", got.Referer)
	}
	if got.Host == "" {
		t.Error("Host 丢了")
	}
}

// TestTransportStopsRedirectLoops 验重定向有上限。
//
// 改成「不限制」的那天，一个跳转环就能把界面线程挂死。
func TestTransportStopsRedirectLoops(t *testing.T) {
	f := newHostFixture(t, map[string]http.HandlerFunc{
		"api.bilibili.com": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://api.bilibili.com/loop", http.StatusFound)
		},
	})
	c := f.client(t)

	res, err := c.Do(mustReq(t, "https://api.bilibili.com/loop"))
	if err == nil {
		res.Body.Close()
		t.Fatal("跳转环没有被拦住")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 请求日志：脱敏
// ─────────────────────────────────────────────────────────────────────────────

// TestRequestLoggerNeverWritesCredentials 是「凭据不会出现在日志里」。
//
// 四样东西各自都会出现在一个请求里：Cookie（含 SESSDATA）、CSRF 头、
// WBI 签名（w_rid/wts/sign）、以及 refresh_token。
// 诊断包是用户**主动**发到 issue 上的，所以这条是硬要求。
func TestRequestLoggerNeverWritesCredentials(t *testing.T) {
	var log strings.Builder
	sess := app.Session{
		SESSDATA:     "sessdata-value-0123456789",
		BiliJCT:      "bili-jct-value-0123456789",
		RefreshToken: "refresh-token-value-0123456789",
	}
	logger := app.NewRequestLogger(&log, func() []string { return sess.Secrets() })

	logger.Log("GET", "https://api.bilibili.com/x/space/wbi/arc/search?mid=1&w_rid=deadbeefdeadbeef&wts=1702204169")
	logger.Log("POST", "https://api.bilibili.com/x/v2/fav/resource/deal?access_key=nope&sign=cafecafe")

	out := log.String()
	for _, secret := range []string{
		"sessdata-value-0123456789",
		"bili-jct-value-0123456789",
		"refresh-token-value-0123456789",
		"deadbeefdeadbeef",
		"1702204169",
	} {
		if strings.Contains(out, secret) {
			t.Errorf("日志里出现了 %q：\n%s", secret, out)
		}
	}
	if !strings.Contains(out, "w_rid=") {
		t.Error("参数名被打掉了：诊断时要看得见「这条请求带没带签名」")
	}
	if !strings.Contains(out, "mid=1") {
		t.Error("不敏感的参数被误伤了")
	}
}

// TestRequestLoggerHidesTheQRCodeKey 验 qrcode_key 不进日志。
//
// 它是扫码登录那一轮的**一次性授权**：拿着它就能轮询出那张二维码对应的
// 凭据。它跟 w_rid 一样属于「拼出来的地址本身就是凭据」那一类，
// 而开调试日志的恰恰是「登录出了问题」的时候——正是它最该被打掉的时候。
func TestRequestLoggerHidesTheQRCodeKey(t *testing.T) {
	var log strings.Builder
	logger := app.NewRequestLogger(&log, nil)

	logger.Log("GET", "https://passport.bilibili.com/x/passport-login/web/qrcode/poll"+
		"?qrcode_key=AbCdEf1234567890XYZqrstuvwxYZ012345")

	out := log.String()
	if strings.Contains(out, "AbCdEf1234567890XYZqrstuvwxYZ012345") {
		t.Errorf("qrcode_key 原样进了日志：\n%s", out)
	}
	if !strings.Contains(out, "qrcode_key=") {
		t.Errorf("参数名也被打掉了，诊断时要看得见「这一轮轮询用的是哪张码」：\n%s", out)
	}
}

// TestRequestLoggerFollowsTheCurrentSession 验脱敏名单跟着登录态走。
//
// 拿一个建好就缓存的名单的话，**登录之后**所有请求的 Cookie 值都会
// 原样进日志——那正是「导出诊断信息里出现凭据」最常见的成因。
func TestRequestLoggerFollowsTheCurrentSession(t *testing.T) {
	var log strings.Builder
	current := app.Session{}
	logger := app.NewRequestLogger(&log, func() []string { return current.Secrets() })

	// 没登录时：不该有秘密被误伤。
	logger.Log("GET", "https://api.bilibili.com/x/web-interface/nav?mid=1")
	if strings.Contains(log.String(), app.Redacted) {
		t.Errorf("没登录却把日志打成马赛克了：%s", log.String())
	}

	// 登录之后：同一个回调要立刻认得新出现的凭据。
	log.Reset()
	const justLoggedIn = "just-logged-in-secret-0123456789"
	current = app.Session{SESSDATA: justLoggedIn}
	logger.Log("GET", "https://api.bilibili.com/x/selftest?note="+justLoggedIn)
	if strings.Contains(log.String(), justLoggedIn) {
		t.Errorf("登录之后新出现的凭据进了日志：%s", log.String())
	}

	// 退登之后：名单立刻变空，新出现的文本不再被误伤。
	//
	// 这一条验的是「名单**跟着**登录态走」而不是「名单建一次就不动」。
	// 两边都不泄露（多脱一点是安全的），但一个建好就缓存的名单意味着
	// **登录之后**新出现的凭据根本没进名单——那才是泄露的那一侧。
	log.Reset()
	current = app.Session{}
	logger.Log("GET", "https://api.bilibili.com/x/selftest?note=退登后不该再脱敏")
	if strings.Contains(log.String(), app.Redacted) {
		t.Errorf("退登之后还在脱敏：%s", log.String())
	}
}

// TestRequestLoggerWithNoWriterDoesNothing 验没写入目标时不炸。
func TestRequestLoggerWithNoWriterDoesNothing(t *testing.T) {
	app.NewRequestLogger(nil, nil).Log("GET", "https://api.bilibili.com/x")
	var nilLogger *app.RequestLogger
	nilLogger.Log("GET", "https://api.bilibili.com/x")
}

// TestConcurrentLoggingIsSafe 验并发打日志不数据竞争。
func TestConcurrentLoggingIsSafe(t *testing.T) {
	var log strings.Builder
	logger := app.NewRequestLogger(&lockWriter{w: &log}, nil)

	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.Log("GET", "https://api.bilibili.com/x?w_rid=abc&i="+string(rune('0'+i)))
		}()
	}
	wg.Wait()
	if !strings.Contains(log.String(), "w_rid=") {
		t.Errorf("日志是空的：%q", log.String())
	}
}

// lockWriter 让并发写也要过一遍 race 检测器。
type lockWriter struct {
	mu sync.Mutex
	w  *strings.Builder
}

func (l *lockWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// ─────────────────────────────────────────────────────────────────────────────
// 诊断导出
// ─────────────────────────────────────────────────────────────────────────────

// TestDiagnosticsUserIDIsActuallyThere 验 userId 不是恒为 <redacted>。
//
// DedeUserID 是**公开**的数字账号标识（B 站每个账号的页面上都印着它），
// 诊断导出把它放出来是给排障的人用的：「用户报的问题是不是这个账号」。
//
// 它以前有 8~10 位，够得上 Secrets() 里「长到值得脱敏」的门槛，
// 于是每一次导出都是 <redacted>——字段等于白留。
func TestDiagnosticsUserIDIsActuallyThere(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	store := app.NewMemoryStore("test")
	ctx := context.Background()
	if err := store.Save(ctx, app.Session{
		Cookies: []app.Cookie{
			{Name: app.CookieDedeUserID, Value: "4242424242", Path: "/"},
			{Name: app.CookieSESSDATA, Value: "sess-secret-0123456789", Path: "/"},
		},
		DedeUserID: "4242424242",
	}); err != nil {
		t.Fatal(err)
	}
	a.MountLogin(store, nil)

	d := app.NewDiagnostics("test")
	d.Attach(a)
	if !d.LoggedIn {
		t.Fatal("没有登录，验不了 userId")
	}
	if d.UserID != "4242424242" {
		t.Errorf("诊断里的 userId 是 %q，want 4242424242（它不是凭据，不该被打掉）", d.UserID)
	}

	blob, err := d.JSON(a.Session())
	if err != nil {
		t.Fatal(err)
	}
	out := string(blob)
	if !strings.Contains(out, "4242424242") {
		t.Errorf("导出的 JSON 里没有 userId：\n%s", out)
	}
	// 但 SESSDATA 照样不许出现——「不是凭据」说的是 UID，不是「都放行」。
	if strings.Contains(out, "sess-secret-0123456789") {
		t.Errorf("放行 userId 的同时也把 SESSDATA 放出去了：\n%s", out)
	}
}

// TestDiagnosticsExportCarriesNoCredential 是验收原文那一条：
// 「导出的 JSON 里 grep 不到敏感字段名」。
func TestDiagnosticsExportCarriesNoCredential(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	a.MountLogin(app.NewMemoryStore("test"), nil)

	d := app.NewDiagnostics("Biu MyGo 测试版")
	d.Attach(a)
	blob, err := d.JSON(app.Session{
		SESSDATA:     "sessdata-secret-0123456789",
		BiliJCT:      "jct-secret-0123456789",
		RefreshToken: "refresh-secret-0123456789",
	})
	if err != nil {
		t.Fatal(err)
	}
	out := string(blob)

	// 一、值一个都不许出现。
	for _, secret := range []string{
		"sessdata-secret-0123456789",
		"jct-secret-0123456789",
		"refresh-secret-0123456789",
	} {
		if strings.Contains(out, secret) {
			t.Errorf("诊断导出里出现了凭据 %q：\n%s", secret, out)
		}
	}
	// 二、字段名也不许出现——出现一个就说明有人加了个不设防的字段。
	for _, name := range []string{
		"SESSDATA", "bili_jct", "refresh_token", "refreshToken",
		"cookie", "Cookie", "authorization", "proxyPassword", "w_rid",
	} {
		if strings.Contains(out, name) {
			t.Errorf("诊断导出里出现了敏感字段名 %q：\n%s", name, out)
		}
	}
	// 三、有用的信息还在。
	if !strings.Contains(out, "credentialBackend") {
		t.Error("连凭据后端的名字都没导出，排障时无从下手")
	}
	if !strings.Contains(out, "goVersion") {
		t.Error("没导出 Go 版本")
	}
}

// TestDiagnosticsRedactTextForLogs 验顺带导出的日志也过脱敏。
func TestDiagnosticsRedactTextForLogs(t *testing.T) {
	d := app.NewDiagnostics("test")
	s := app.Session{SESSDATA: "sess-in-a-log-line-0123456789"}
	got := d.RedactText(s, "请求失败，携带的凭据是 sess-in-a-log-line-0123456789")
	if strings.Contains(got, "sess-in-a-log-line-0123456789") {
		t.Errorf("日志里的凭据没脱敏：%s", got)
	}
	if !strings.Contains(got, "请求失败") {
		t.Error("非秘密的部分被误伤了")
	}
}

// ─────────────────────────────────────────────────────────────────────────────

func mustReq(t *testing.T, rawURL string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}
