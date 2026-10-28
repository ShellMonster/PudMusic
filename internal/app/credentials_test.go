package app_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// ─────────────────────────────────────────────────────────────────────────────
// 错误语义
// ─────────────────────────────────────────────────────────────────────────────

// TestStoreErrorSemantics 钉住三种错误的含义——调用方要靠它们决定
// 「提示用户」还是「静默当没登录」。
func TestStoreErrorSemantics(t *testing.T) {
	ctx := context.Background()
	st := app.NewMemoryStore("test")

	// 1. 没登录过是正常状态，不是故障。
	if _, err := st.Load(ctx); !errors.Is(err, app.ErrNotLoggedIn) {
		t.Errorf("空后端 Load 返回 %v，want ErrNotLoggedIn", err)
	}
	// 2. Clear 幂等：本来就没登录也不能报错（退登按钮连点两次）。
	if err := st.Clear(ctx); err != nil {
		t.Errorf("对空后端 Clear 返回 %v，幂等要求是 nil", err)
	}

	// 3. 存进去读得回来，登录态在。
	if err := st.Save(ctx, loggedIn()); err != nil {
		t.Fatal(err)
	}
	s, err := st.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !s.LoggedIn() || s.SESSDATA != "abcdef123456" || s.BiliJCT != "csrf7890" {
		t.Errorf("存回来的登录态不对：%+v", s)
	}
	if err := st.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Load(ctx); !errors.Is(err, app.ErrNotLoggedIn) {
		t.Errorf("Clear 之后 Load 返回 %v，want ErrNotLoggedIn", err)
	}
}

func loggedIn() app.Session {
	return app.Session{
		Cookies: []app.Cookie{
			{Name: app.CookieSESSDATA, Value: "abcdef123456", Domain: ".bilibili.com", Path: "/", Secure: true},
			{Name: app.CookieBiliJCT, Value: "csrf7890", Domain: ".bilibili.com", Path: "/", Secure: true},
			{Name: app.CookieDedeUserID, Value: "4242", Domain: ".bilibili.com", Path: "/"},
		},
		SESSDATA:     "abcdef123456",
		BiliJCT:      "csrf7890",
		RefreshToken: "refresh-token-value-xyz",
	}
}

// TestRestartRestoresTheLogin 验「重启后还能登录」这条。
//
// cookiejar 在内存里，进程一退就没了。真正兜住重启的是落盘的 Session +
// Session.Jar() 重建。
func TestRestartRestoresTheLogin(t *testing.T) {
	ctx := context.Background()
	// 同一个后端 = 同一个「钥匙串」，Store.Load 就是启动时那一步。
	st := app.NewMemoryStore("test")
	if err := st.Save(ctx, loggedIn()); err != nil {
		t.Fatal(err)
	}

	// 第一次进程：登录、拿 jar、发请求。
	m1 := app.NewManager(st)
	if err := m1.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if !m1.Current().LoggedIn() {
		t.Fatal("重启后应该还是登录状态")
	}

	// 第二次进程：只有 Store 和 Manager，jar 是重建的。
	m2 := app.NewManager(st)
	if err := m2.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if !m2.Current().LoggedIn() {
		t.Fatal("重启后丢了登录态")
	}

	// 重建的 jar 里真的有那条 Cookie——不只是状态标志位对了。
	//
	// 这里查 jar 而不是发真的请求：B 站的 Cookie 域是 .bilibili.com，
	// 发给 httptest 的 127.0.0.1 永远发不出去，那测的是 cookiejar 的域规则，
	// 不是我们重建得对不对。
	got := m2.Jar().Cookies(mustURL(t, "https://api.bilibili.com/x/web-interface/nav"))
	var sess string
	for _, c := range got {
		if c.Name == app.CookieSESSDATA {
			sess = c.Value
		}
	}
	if sess != "abcdef123456" {
		t.Errorf("重建的 jar 里没有 SESSDATA，拿到的是 %+v", got)
	}
}

// TestNormalizeAlignsTheSeparateFields 验三个单字段和 Cookies 对齐。
//
// 拿旧的 bili_jct 配新的 SESSDATA，写接口会被拒，而且 B 站报的是「风控」
// 不是「CSRF 错了」——极难查，所以这里让 Cookies 当权威。
func TestNormalizeAlignsTheSeparateFields(t *testing.T) {
	s := loggedIn()
	s.Cookies = append(s.Cookies, app.Cookie{Name: app.CookieBiliJCT, Value: "new-csrf-token"})
	s.BiliJCT = "stale-csrf" // 调用方只改了这一处

	s.Normalize(s.SavedAt)
	if s.BiliJCT != "new-csrf-token" {
		t.Errorf("bili_jct 没有以 Cookies 为准，还是 %q", s.BiliJCT)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 在途的续期不能写回已退登的账号（M8 风险登记第 1 条）
// ─────────────────────────────────────────────────────────────────────────────

// TestInFlightRenewalCannotResurrectALoggedOutAccount 是这条风险的完整场景：
// 续期请求发出去 → 用户点了退出登录 → 续期结果回来 → 不能被写回去。
func TestInFlightRenewalCannotResurrectALoggedOutAccount(t *testing.T) {
	ctx := context.Background()
	store := app.NewMemoryStore("test")
	if err := store.Save(ctx, loggedIn()); err != nil {
		t.Fatal(err)
	}

	m := app.NewManager(store)
	if err := m.Load(ctx); err != nil {
		t.Fatal(err)
	}

	// 续期在后台跑起来了。
	lease := m.BeginRenew()

	// 用户在这中间退登了。
	if err := m.Logout(ctx); err != nil {
		t.Fatal(err)
	}

	// 续期结果回来了，带着一个「新的 SESSDATA」。
	err := lease.Commit(ctx, func(s *app.Session) error {
		s.SESSDATA = "resurrected-token"
		return nil
	})
	if !errors.Is(err, app.ErrStaleGeneration) {
		t.Fatalf("过期的续期结果提交返回 %v，want ErrStaleGeneration", err)
	}

	if m.Current().LoggedIn() {
		t.Error("退掉的账号被在途的续期结果写回来了")
	}
	// 后端也得是干净的——不然下次启动又是登录状态。
	if _, err := store.Load(ctx); !errors.Is(err, app.ErrNotLoggedIn) {
		t.Errorf("后端里还留着凭据：%v", err)
	}
}

func TestLeaseCommitAdvancesGeneration(t *testing.T) {
	ctx := context.Background()
	store := app.NewMemoryStore("test")
	if err := store.Save(ctx, loggedIn()); err != nil {
		t.Fatal(err)
	}
	m := app.NewManager(store)
	if err := m.Load(ctx); err != nil {
		t.Fatal(err)
	}
	before := m.Generation()

	lease := m.BeginRenew()
	if err := lease.Commit(ctx, func(s *app.Session) error {
		s.SESSDATA = "fresh"
		s.Cookies = []app.Cookie{{Name: app.CookieSESSDATA, Value: "fresh", Domain: ".bilibili.com"}}
		return nil //nolint:nilerr // 回调就是不改任何东西，测的是代次
	}); err != nil {
		t.Fatal(err)
	}
	if m.Generation() == before {
		t.Error("提交之后代次没有推进")
	}
	if !m.Current().LoggedIn() {
		t.Error("提交之后没登录")
	}
}

func TestManagerUpdateRollsBackWhenSaveFails(t *testing.T) {
	ctx := context.Background()
	store := &failingStore{has: loggedIn()}
	m := app.NewManager(store)
	if err := m.Load(ctx); err != nil {
		t.Fatal(err)
	}
	before := m.Generation()

	if err := m.Update(ctx, func(s *app.Session) error {
		s.SESSDATA = "changed"
		return nil
	}); !errors.Is(err, app.ErrBackend) {
		t.Fatalf("存失败时返回 %v，want ErrBackend", err)
	}
	// 内存里不能留着一个没落盘的登录态：下次启动就是另一个样子，
	// 这种「看起来成功了」的半成品最难查。
	if got := m.Current().SESSDATA; got != "abcdef123456" {
		t.Errorf("存失败之后内存里留着的是 %q，不是存下去之前的那份", got)
	}
	if m.Generation() != before {
		t.Error("存失败不该推进代次")
	}
}

// failingStore 读得出来、存不进去——钥匙串被锁、DPAPI 用户变了这类情况。
type failingStore struct{ has app.Session }

func (*failingStore) Name() string { return "failing" }
func (f *failingStore) Load(context.Context) (app.Session, error) {
	if !f.has.LoggedIn() {
		return app.Session{}, app.ErrNotLoggedIn
	}
	return f.has, nil
}
func (*failingStore) Save(context.Context, app.Session) error { return app.ErrBackend }
func (*failingStore) Clear(context.Context) error             { return nil }

// ─────────────────────────────────────────────────────────────────────────────
// 注入边界：谁能拿到凭据
// ─────────────────────────────────────────────────────────────────────────────

func TestClassifyByRequestKind(t *testing.T) {
	p := app.DefaultInjectPolicy()
	cases := []struct {
		url  string
		want app.CredentialClass
		why  string
	}{
		{"https://api.bilibili.com/x/web-interface/nav", app.CredBiliFull, "B 站读写 API 要完整凭据"},
		{"https://www.bilibili.com/", app.CredBiliFull, "网页主机"},
		{"https://passport.bilibili.com/x", app.CredBiliFull, "登录主机"},
		{"https://music.bilibili.com/", app.CredBiliFull, "音乐主机"},
		{"https://i0.hdslb.com/bfs/archive/x.jpg", app.CredRefererOnly, "封面 CDN 只要 Referer"},
		{"https://upos-sz-mirrorcos.bilivideo.com/x.m4s", app.CredRefererOnly, "音频 CDN 只要 Referer"},
		{"https://interface.music.163.com/api", app.CredRefererOnly, "歌词源明确不给 B 站凭据"},
		{"https://lrclib.net/api/get", app.CredRefererOnly, "LRClib 明确不给 B 站凭据"},
		{"https://api.github.com/repos/x/y/releases", app.CredNone, "自动更新不带 Cookie"},
		{"https://evil.example/", app.CredNone, "陌生主机一律不给"},
	}
	for _, c := range cases {
		if got := p.Classify(c.url); got != c.want {
			t.Errorf("%s 的分类是 %v（%s），want %v", c.url, got, c.why, c.want)
		}
	}
}

// TestClassifyRejectsSuffixConfusion 是这张白名单最重要的一条。
//
// `evil.bilibili.com.attacker.net` 里有 "bilibili.com" 这个子串。
// 在原始 URL 字符串上做子串匹配就会中招，凭据直接送到陌生主机。
// 匹配必须发生在 url.Parse 之后的 hostname 上。
func TestClassifyRejectsSuffixConfusion(t *testing.T) {
	p := app.DefaultInjectPolicy()
	for _, u := range []string{
		"https://evil.bilibili.com.attacker.net/x",
		"https://bilibili.com.attacker.net/x",
		"https://notbilibili.com/x",
		"https://evil-hdslb.com.attacker.net/x",
		"https://bilibili.com@attacker.net/x", // userinfo 里的主机不是主机
	} {
		if got := p.Classify(u); got != app.CredNone {
			t.Errorf("%s 的分类是 %v，凭据会被送到陌生主机", u, got)
		}
	}
}

func TestClassifyPortAndCaseAreIgnored(t *testing.T) {
	p := app.DefaultInjectPolicy()
	if got := p.Classify("https://API.Bilibili.COM:443/x"); got != app.CredBiliFull {
		t.Errorf("大小写和端口不影响主机：%v", got)
	}
}

// TestSanitizeStripsCredentialsFromForeignHosts 验 Sanitize 真的删。
func TestSanitizeStripsCredentialsFromForeignHosts(t *testing.T) {
	p := app.DefaultInjectPolicy()
	req := mustRequest(t, "https://interface.music.163.com/api",
		"Cookie", "SESSDATA=abcdef123456; bili_jct=csrf7890",
		"Authorization", "Bearer nope",
		"Proxy-Authorization", "Basic nope",
		"bili-jct", "csrf7890",
		"SESSDATA", "abcdef123456")

	p.Sanitize(req)

	for _, h := range []string{"Cookie", "Authorization", "Proxy-Authorization", "bili-jct", "SESSDATA"} {
		if got := req.Header.Get(h); got != "" {
			t.Errorf("歌词源上 %s 没被删掉：%q", h, got)
		}
	}
}

// TestSanitizeKeepsCredentialsOnBiliAPI 验反方向：B 站 API 上的不能删。
func TestSanitizeKeepsCredentialsOnBiliAPI(t *testing.T) {
	p := app.DefaultInjectPolicy()
	req := mustRequest(t, "https://api.bilibili.com/x",
		"Cookie", "SESSDATA=abcdef123456",
		"bili-jct", "csrf7890")

	p.Sanitize(req)

	if req.Header.Get("Cookie") == "" {
		t.Error("B 站 API 上的 Cookie 被误删了")
	}
	if req.Header.Get("bili-jct") == "" {
		t.Error("B 站 API 上的 CSRF 被误删了")
	}
}

// TestSanitizeKeepsRefererOnCDN 验 CDN 上 Referer 留着、Cookie 删掉。
func TestSanitizeKeepsRefererOnCDN(t *testing.T) {
	p := app.DefaultInjectPolicy()
	req := mustRequest(t, "https://i0.hdslb.com/bfs/x.jpg",
		"Referer", "https://www.bilibili.com/",
		"Cookie", "SESSDATA=abcdef123456")

	p.Sanitize(req)

	if req.Header.Get("Cookie") != "" {
		t.Error("CDN 上不该带 Cookie")
	}
	if req.Header.Get("Referer") != "https://www.bilibili.com/" {
		t.Error("CDN 上的 Referer 被误删了，给了它就 403")
	}
}

// TestRedirectDowngradeDropsCredentials 验「HTTPS 降级到 HTTP 必须清掉凭据」。
func TestRedirectDowngradeDropsCredentials(t *testing.T) {
	p := app.DefaultInjectPolicy()
	if got := p.RedirectClass(
		mustURL(t, "https://api.bilibili.com/x"),
		mustURL(t, "http://api.bilibili.com/x"),
	); got != app.CredNone {
		t.Errorf("降级到 HTTP 之后凭据级别是 %v，必须是 none", got)
	}
}

func TestRedirectToForeignHostDropsCredentials(t *testing.T) {
	p := app.DefaultInjectPolicy()
	if got := p.RedirectClass(
		mustURL(t, "https://api.bilibili.com/x"),
		mustURL(t, "https://evil.example/steal"),
	); got != app.CredNone {
		t.Errorf("跳到陌生主机后凭据级别是 %v，必须是 none", got)
	}
}

// TestRedirectWithinWhitelistKeepsUpToItsOwnLevel 验白名单内是按目标
// 重新判级（API → CDN 是降级，CDN 拿不到完整 Cookie，但也不该降到零）。
func TestRedirectWithinWhitelistKeepsUpToItsOwnLevel(t *testing.T) {
	p := app.DefaultInjectPolicy()
	if got := p.RedirectClass(
		mustURL(t, "https://api.bilibili.com/x"),
		mustURL(t, "https://i0.hdslb.com/bfs/x.jpg"),
	); got != app.CredRefererOnly {
		t.Errorf("CDN 的级别是 %v，want referer-only", got)
	}
}

// TestPolicyRoundTripThroughRealRedirect 是 `PLAN.md` 阶段 3 的验收用例
// （「用假 HTTP 服务断言 redirect 到陌生 host 时不带 Cookie」）。
//
// 这一条从「策略测试」升级成了**生产那一层**的测试：走的是
// `CredentialTransport` 本身，不是这里临时搭的一个 helper——
// 临时搭的那个验的是「策略能删 Cookie」，而用户遇到的是
// 「整个传输层有没有在每一跳调它」。
func TestPolicyRoundTripThroughRealRedirect(t *testing.T) {
	var cdnGot string
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cdnGot = r.Header.Get("Cookie")
		w.WriteHeader(http.StatusOK)
	}))
	defer cdn.Close()

	// 陌生主机收到了一个 302，把用户往 CDN 送。
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, cdn.URL+"/cover.jpg", http.StatusFound)
	}))
	defer foreign.Close()

	p := app.DefaultInjectPolicy()
	tr := &app.CredentialTransport{Base: http.DefaultTransport, Policy: p}
	jar, _ := cookiejar.New(nil)
	c := tr.Client(jar)

	req, err := http.NewRequest(http.MethodGet, foreign.URL+"/cover.jpg", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", "SESSDATA=abcdef123456")

	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	if got := req.Header.Get("Cookie"); got != "SESSDATA=abcdef123456" {
		t.Errorf("调用方的请求被改了：%q", got)
	}
	if cdnGot != "" {
		t.Errorf("被 302 送过去的 CDN 上有 Cookie：%q", cdnGot)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 脱敏边界：诊断信息里能出现什么
// ─────────────────────────────────────────────────────────────────────────────

// TestRedactorHidesCookieAndProxyPassword 验凭据、签名 URL、代理密码
// 一个都进不了日志。
func TestRedactorHidesCookieAndProxyPassword(t *testing.T) {
	s := loggedIn()
	r := app.ForSession(s)

	// Cookie 头整个换掉——不靠值匹配，靠名字。
	if got := r.Header("Cookie", "SESSDATA=abcdef123456; foo=bar"); got != app.Redacted {
		t.Errorf("Cookie 头打码成了 %q", got)
	}
	if got := r.Header("cookie", "别的"); got != app.Redacted {
		t.Errorf("头名是大小写不敏感的，小写没打码：%q", got)
	}
	for _, h := range []string{"Set-Cookie", "Authorization", "Proxy-Authorization", "bili-jct"} {
		if got := r.Header(h, "whatever"); got != app.Redacted {
			t.Errorf("%s 没打码：%q", h, got)
		}
	}

	// WBI 签名 URL：参数名留着（诊断时要知道有没有签名），值必须打掉。
	signed := "https://music.bilibili.com/audio?fnval=16&w_rid=deadbeef&wts=1728000000&sign=cafebabe"
	got := r.URL(signed)
	if strings.Contains(got, "deadbeef") || strings.Contains(got, "cafebabe") {
		t.Errorf("签名 URL 没打干净：%s", got)
	}
	// Redacted 在 URL 里是百分号编码的，按解码后的值比。
	q := mustURL(t, got).Query()
	for _, k := range []string{"w_rid", "wts", "sign"} {
		if q.Get(k) != app.Redacted {
			t.Errorf("签名参数 %s 的值是 %q，want %q", k, q.Get(k), app.Redacted)
		}
	}
	if q.Get("fnval") != "16" {
		t.Errorf("不敏感的参数被误伤了：%s", got)
	}

	// 兜底：正文里的秘密片段。
	log := "登录失败 sessdata=abcdef123456 csrf=csrf7890 refresh=refresh-token-value-xyz"
	if out := r.Text(log); strings.Contains(out, "abcdef123456") ||
		strings.Contains(out, "csrf7890") || strings.Contains(out, "refresh-token-value-xyz") {
		t.Errorf("正文里的秘密没脱敏：%s", out)
	}
	if !strings.Contains(out2(r, log), "登录失败") {
		t.Error("非秘密的部分被误伤了")
	}
}

// TestRedactorShortCircuitsWithoutSecrets 没登录时不该把日志打成马赛克。
func TestRedactorShortCircuitsWithoutSecrets(t *testing.T) {
	r := app.ForSession(app.Session{})
	if got := r.Text("USER_NOT_FOUND code=-101"); got != "USER_NOT_FOUND code=-101" {
		t.Errorf("没有秘密的时候不该改动文本：%q", got)
	}
}

func TestRedactorHidesUnparsableURL(t *testing.T) {
	r := app.ForSession(loggedIn())
	if got := r.URL("http://[::1"); got != app.Redacted {
		t.Errorf("解不出来的 URL 该整体打码，得到 %q", got)
	}
}

func out2(r *app.Redactor, s string) string { return r.Text(s) }

// ─────────────────────────────────────────────────────────────────────────────

func mustRequest(t *testing.T, rawURL string, kv ...string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(kv); i += 2 {
		req.Header.Set(kv[i], kv[i+1])
	}
	return req
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
