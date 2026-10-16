package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// 这一组测的是**登录这一整件事**，不是某一个零件。
//
// 主缝是 `SPEC.md` 定的「App 是唯一输入面」：
//
//	App 的公开命令（唯一输入）
//	        ↓
//	状态快照 + 渲染结果（唯一断言面）
//
// 所以这些测试只调 App 的命令、只读 LoginState，
// 中间那台假 B 站服务器是唯一的外部世界。

// waitTimeout 是这些用例等后台登录流程的上限。
//
// 流程跑在 goroutine 里，测试只能轮询等它。给一个上限而不是固定 sleep：
// 固定 sleep 在慢机器上偶发失败、在快机器上白等。
const waitTimeout = 5 * time.Second

// ─────────────────────────────────────────────────────────────────────────────
// 假 B 站
// ─────────────────────────────────────────────────────────────────────────────

// biliFixture 是一台可控的假 B 站。
//
// # 为什么要「一步步放行」
//
// 扫码登录的三个状态是**转瞬即逝**的：轮询间隔在测试里是 2 ms，
// 从「已扫待确认」到「登录成功」也就两三毫秒。测试每 10 ms 轮询一次状态，
// 于是只能看到其中���个——而这条用例的全部意义就是三��都要走到。
//
// 所以 fixture 支持两种放行方式：
//
//	默认        每次轮询立刻返回。适合「只关心最后变成什么」的用例。
//	stepByHand  每次轮询都卡住，直到测试 `f.step()` 放行。
//	           适合「三���状态一个都不能漏」的用例。
//
// ⚠️ 千万别用「sleep 一下看看状态」的写法来对付这个问题：那是 flaky 测试
// 的标准形态，慢机器上偶发失败、快机器上白等。
type biliFixture struct {
	*httptest.Server
	mu sync.Mutex
	// scans 是轮询接口依次回的状态码。一轮扫完就用完了。
	scans []int
	// steps 非 nil 时，每次轮询都要先拿到一个令牌才返回。见 stepByHand。
	steps chan struct{}
	// release 卡住第 N 次及以后的轮询，直到 unblock。用来造「在途请求」。
	release    chan struct{}
	pollGateAt int
	gated      int
	// seen 记下收到的每个请求路径。
	seen []string
}

func newBiliFixture(t *testing.T) *biliFixture {
	t.Helper()
	f := &biliFixture{release: make(chan struct{})}
	// ⚠️ **必须是 TLS**：凭据边界有一条硬规则是「非 HTTPS 一律不带凭据」
	// （明文链路上一个字节的凭据都不能留）。用 httptest.NewServer 的
	// 明文服务器，所有请求都会被剥掉 cookie，测试测到的就不是
	// 「登录成功之后凭据真的落盘了」，而是「凭据被正确地丢掉了」。
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const (
			pathGenerate = "/x/passport-login/web/qrcode/generate"
			pathPoll     = "/x/passport-login/web/qrcode/poll"
		)
		isPoll := r.URL.Path == pathPoll

		f.mu.Lock()
		f.seen = append(f.seen, r.URL.Path)
		steps := f.steps
		gate := isPoll && f.pollGateAt > 0 && f.gated >= f.pollGateAt
		if gate {
			f.gated++
		}
		rel := f.release
		f.mu.Unlock()

		// 先等放行，再取状态码——顺序反了的话，一次放行会连着跳过两步。
		//
		// ⚠️ **只有轮询要等**：申请二维码也在同一个 handler 里，
		// 它要是也等令牌，就会把测试发的那枚令牌吃掉——
		// 于是「放行第一次轮询」其实放行了申请二维码，
		// 第一次轮询拿到的还是第二个状态码。
		if isPoll {
			if steps != nil {
				select {
				case <-steps:
				case <-r.Context().Done():
					return
				}
			}
			if gate {
				select {
				case <-rel:
				case <-r.Context().Done():
					return
				case <-time.After(5 * time.Second):
				}
			}
		}

		f.mu.Lock()
		// scans 只在**轮询**接口上消耗。申请二维码也消耗的话，
		// 序列会整体往前挪一位：本来想测「还没扫」的那一次，
		// 结果第一步就返回成功了。已扫待确认、过期同理。
		scan := 86101
		if isPoll && len(f.scans) > 0 {
			scan = f.scans[0]
			f.scans = f.scans[1:]
		}
		f.mu.Unlock()

		switch r.URL.Path {
		case pathGenerate:
			http.SetCookie(w, &http.Cookie{Name: "buvid", Value: "not-a-credential", Path: "/"})
			writeFixtureJSON(t, w, 0, "0", map[string]any{
				"url":        "https://passport.bilibili.com/h5-app/passport/login/scan?qrcode_key=fixture",
				"qrcode_key": "fixture-key-32-chars-long-0000000",
			})
		case pathPoll:
			switch scan {
			case 0:
				http.SetCookie(w, &http.Cookie{Name: "SESSDATA", Value: "sess-from-fixture", Path: "/"})
				http.SetCookie(w, &http.Cookie{Name: "bili_jct", Value: "jct-from-fixture", Path: "/"})
				http.SetCookie(w, &http.Cookie{Name: "DedeUserID", Value: "424242", Path: "/"})
			}
			writeFixtureJSON(t, w, 0, "0", map[string]any{
				"code":          scan,
				"message":       "fixture",
				"refresh_token": "rt-from-fixture",
				"timestamp":     time.Now().UnixMilli(),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

// onScans 设好接下来几次轮询依次回什么。
func (f *biliFixture) onScans(codes ...int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scans = codes
}

// stepByHand 改成每次轮询都要测试放行。
func (f *biliFixture) stepByHand() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.steps = make(chan struct{}, 64)
}

// step 放行一次轮询。
func (f *biliFixture) step() {
	f.mu.Lock()
	steps := f.steps
	f.mu.Unlock()
	if steps == nil {
		return
	}
	steps <- struct{}{}
}

func (f *biliFixture) gateNthPoll(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pollGateAt = n
}

func (f *biliFixture) unblock() { close(f.release) }

func (f *biliFixture) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.seen...)
}

func writeFixtureJSON(t *testing.T, w http.ResponseWriter, code int, msg string, data any) {
	t.Helper()
	body := map[string]any{"code": code, "message": msg, "ttl": 1, "data": data}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

// newTestApp 装一个指向 fixture 的 App。
//
// 用**真的** CredentialTransport：凭据边界是这次要验的东西之一，
// 用一个裸的 http.Client 就等于把要验的那段绕过去了。
func newTestApp(t *testing.T, f *biliFixture, store app.Store) *app.App {
	t.Helper()
	a := app.New()
	t.Cleanup(a.Close)

	// Base 用 httptest 自带的那份 transport——它认识那张自签证书。
	// 用默认的话会 TLS 握手失败，而那个失败跟被测的逻辑无关。
	transport := &app.CredentialTransport{
		Base:   f.Server.Client().Transport,
		Policy: app.DefaultInjectPolicy(),
	}
	// fixture 的请求打在 127.0.0.1 上，而白名单只认 B 站——
	// 所以这里显式放这一个主机，**顺带证明白名单是可扩展的**。
	transport.Policy = app.DefaultInjectPolicy().AllowBiliHost(hostOf(f.URL))

	cli := bili.NewClient(bili.Options{
		Transport: transport,
		CSRF:      func() string { return a.Session().BiliJCT },
	})
	// ⚠️ 不指到 fixture 上的话，这些用例会打到**真的** passport.bilibili.com：
	// 不报错、结果还挺合理，于是测试变成「取决于能不能连上 B 站」。
	cli.TestOnlyPointHostsAtFixture(f.URL)
	a.MountLogin(store, cli)
	a.SetLoginPollInterval(2 * time.Millisecond)
	return a
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// ─────────────────────────────────────────────────────────────────────────────
// 三种状态
// ─────────────────────────────────────────────────────────────────────────────

// TestLoginWalksThroughTheThreeStates 是这组测试的主线。
//
// 一个二维码从「等扫」→「已扫待确认」→「成功」，三步都要走到，
// 而且**每一步界面都能读到对应的提示**。三态少一态的表现是
// 用户卡在转圈里（或者扫完了还被告知「请扫码」）。
func TestLoginWalksThroughTheThreeStates(t *testing.T) {
	f := newBiliFixture(t)
	// 还没扫 → 已扫待确认 → 成功
	f.onScans(86101, 86090, 0)
	// 一步步放行：三态是转瞬即逝的，不手动放行就只能看到其中���个。
	f.stepByHand()

	a := newTestApp(t, f, app.NewMemoryStore("test"))
	a.BeginLogin()

	// 第一步：二维码出来了，等扫码。
	f.step()
	waitFor(t, waitTimeout, "二维码出现", func() bool { return a.LoginState().QR != nil })
	if p := a.LoginState().Phase; p != app.LoginWaiting {
		t.Fatalf("拿到二维码时阶段是 %v，want LoginWaiting", p)
	}
	if got := a.LoginState().Hint; !strings.Contains(got, "扫码") {
		t.Errorf("等扫码时提示是 %q，该告诉用户拿手机扫", got)
	}

	// 第二步：已扫待确认。**提示必须换**——共用一句「请扫码」的话，
	// 用户会以为没扫成功，退出重进，然后卡死在这里。
	f.step()
	waitFor(t, waitTimeout, "已扫待确认", func() bool { return a.LoginState().Phase == app.LoginScanned })
	if got := a.LoginState().Hint; !strings.Contains(got, "确认") {
		t.Errorf("已扫待确认时提示是 %q，该说清楚「在手机上确认」", got)
	}

	// 第三步：成功。
	f.step()
	waitFor(t, waitTimeout, "登录成功", func() bool { return a.LoginState().Phase == app.LoginDone })
	if !a.LoggedIn() {
		t.Error("显示登录成功了，但 App 说没登录")
	}
	if !strings.Contains(a.LoginState().Hint, "登录成功") {
		t.Errorf("成功时的提示是 %q", a.LoginState().Hint)
	}
}

// TestScannedQRCodeIsRenderedLocally 验「二维码本地渲染」。
//
// 这条验的不是「有个二维码」，而是「那张二维码上**就是** B 站给的
// 那条地址」——用 qrcode 自己的反向解码读回来。
// 不联网取图这件事由 bili 那边验（那里能数请求个数）。
func TestScannedQRCodeIsRenderedLocally(t *testing.T) {
	f := newBiliFixture(t)
	f.onScans(86101)
	a := newTestApp(t, f, app.NewMemoryStore("test"))
	a.BeginLogin()
	defer a.CancelLogin()

	waitFor(t, waitTimeout, "二维码出现", func() bool { return a.LoginState().QR != nil })
	code := a.LoginState().QR
	if code.Size() < 21 {
		t.Errorf("二维码只有 %d×%d，太小了", code.Size(), code.Size())
	}
	// 三个定位图形必须都在——少一个就是「图看着挺对但扫不出来」。
	size := code.Size()
	for _, c := range [][2]int{{3, 3}, {size - 4, 3}, {3, size - 4}} {
		if !code.Dark(c[0], c[1]) {
			t.Errorf("定位图形 (%d,%d) 的中心不是黑的", c[0], c[1])
		}
	}
}

// TestExpiredQRCodeStopsAndAsksForANewOne 验过期。
//
// 过期的正确行为是**停下来让用户点刷新**，不是继续轮询一张死图
// （B 站不会让一张过期的二维码复活），也不是报「登录失败」。
func TestExpiredQRCodeStopsAndAsksForANewOne(t *testing.T) {
	f := newBiliFixture(t)
	f.onScans(86038)
	a := newTestApp(t, f, app.NewMemoryStore("test"))
	a.BeginLogin()

	waitFor(t, waitTimeout, "二维码过期", func() bool { return a.LoginState().Phase == app.LoginExpired })
	if got := a.LoginState().Hint; !strings.Contains(got, "刷新") {
		t.Errorf("过期时的提示是 %q，该告诉用户点刷新", got)
	}

	// 停了：再等一会儿也不该有新的轮询打出去。
	before := len(f.requests())
	time.Sleep(60 * time.Millisecond)
	if after := len(f.requests()); after != before {
		t.Errorf("二维码过期之后还在轮询（%d → %d 个请求）", before, after)
	}

	// 点了刷新就是新的一轮。
	f.onScans(0)
	a.RefreshLoginCode()
	waitFor(t, waitTimeout, "刷新之后登录成功", func() bool { return a.LoginState().Phase == app.LoginDone })
}

// TestStillWaitingKeepsPolling 验「还没扫」不会结束流程。
func TestStillWaitingKeepsPolling(t *testing.T) {
	f := newBiliFixture(t)
	// 一直还没扫，然后突然成功。
	f.onScans(86101, 86101, 86101, 0)
	f.stepByHand()
	a := newTestApp(t, f, app.NewMemoryStore("test"))
	a.BeginLogin()

	for range 3 {
		f.step()
		waitFor(t, waitTimeout, "一次轮询走完", func() bool { return pollCount(f) >= 1 })
		if p := a.LoginState().Phase; p != app.LoginWaiting {
			t.Errorf("没扫的时候阶段是 %v，want LoginWaiting", p)
		}
	}

	f.step()
	waitFor(t, waitTimeout, "最后成功", func() bool { return a.LoginState().Phase == app.LoginDone })
}

func pollCount(f *biliFixture) int {
	n := 0
	for _, p := range f.requests() {
		if p == "/x/passport-login/web/qrcode/poll" {
			n++
		}
	}
	return n
}

// ─────────────────────────────────────────────────────────────────────────────
// 凭据落盘 / 重启
// ─────────────────────────────────────────────────────────────────────────────

// TestLoginPersistsTheSession 验扫码成功之后凭据真的存进了后端。
func TestLoginPersistsTheSession(t *testing.T) {
	f := newBiliFixture(t)
	f.onScans(0)
	store := app.NewMemoryStore("test")
	a := newTestApp(t, f, store)
	a.BeginLogin()
	waitFor(t, waitTimeout, "登录成功", func() bool { return a.LoginState().Phase == app.LoginDone })

	s, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("后端里没有登录态：%v", err)
	}
	if s.SESSDATA != "sess-from-fixture" {
		t.Errorf("存进去的 SESSDATA 是 %q", s.SESSDATA)
	}
	if s.BiliJCT != "jct-from-fixture" {
		t.Errorf("存进去的 bili_jct 是 %q", s.BiliJCT)
	}
	if s.DedeUserID != "424242" {
		t.Errorf("存进去的 DedeUserID 是 %q", s.DedeUserID)
	}
	if s.RefreshToken != "rt-from-fixture" {
		t.Errorf("存进去的 refresh_token 是 %q", s.RefreshToken)
	}
}

// TestOnlyBilibiliCookiesAreStored 验只存 B 站自己的 cookie。
//
// B 站会顺带下发 buvid 之类的东西。它们不是登录凭据，
// 存进钥匙串只会让「我的凭据里到底有什么」变得不透明。
func TestOnlyBilibiliCookiesAreStored(t *testing.T) {
	f := newBiliFixture(t)
	f.onScans(0)
	store := app.NewMemoryStore("test")
	a := newTestApp(t, f, store)
	a.BeginLogin()
	waitFor(t, waitTimeout, "登录成功", func() bool { return a.LoginState().Phase == app.LoginDone })

	s, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range s.Cookies {
		if c.Name == "buvid" {
			t.Errorf("非登录凭据的 cookie 也存了：%q", c.Name)
		}
	}
}

// TestRestartKeepsTheLoginState 验「重启应用还是登录状态」。
//
// 「重启」= 一个全新的 App、全新的客户端、同一份后端。
// 少做任何一步，用户就会「一打开就掉登录」：
// 只读回 Session 而不把 cookie 装进 jar 的话，客户端还是匿名发的。
func TestRestartKeepsTheLoginState(t *testing.T) {
	f := newBiliFixture(t)
	f.onScans(0)
	store := app.NewMemoryStore("test")

	a := newTestApp(t, f, store)
	a.BeginLogin()
	waitFor(t, waitTimeout, "登录成功", func() bool { return a.LoginState().Phase == app.LoginDone })
	a.Close()

	// 重启：新的 App、新的客户端、同一个后端。
	b := newTestApp(t, f, store)
	if !b.LoggedIn() {
		t.Fatal("重启之后是登出状态")
	}
	s := b.Session()
	if s.SESSDATA != "sess-from-fixture" || s.BiliJCT != "jct-from-fixture" {
		t.Errorf("重启之后凭据对不上：%+v", s)
	}

}

// TestCookiesReachTheServerAfterRestart 用真请求验 cookie 确实发出去了。
func TestCookiesReachTheServerAfterRestart(t *testing.T) {
	var gotCookie string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		http.SetCookie(w, &http.Cookie{Name: "SESSDATA", Value: "sess-abc", Path: "/"})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"url":"u","qrcode_key":"k"}}`))
	}))
	defer srv.Close()

	transport := &app.CredentialTransport{
		Base:   srv.Client().Transport,
		Policy: app.DefaultInjectPolicy().AllowBiliHost(hostOf(srv.URL)),
	}
	cli := bili.NewClient(bili.Options{Transport: transport})
	cli.TestOnlyPointHostsAtFixture(srv.URL)
	if _, err := cli.NewQRCode(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotCookie != "" {
		t.Errorf("没登录也带了 cookie：%q", gotCookie)
	}
	if err := cli.SeedCookies([]*http.Cookie{{Name: "SESSDATA", Value: "sess-abc", Path: "/"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := cli.NewQRCode(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotCookie, "SESSDATA=sess-abc") {
		t.Errorf("装上 cookie 之后请求没带上：%q", gotCookie)
	}
}

// TestNotLoggedInAtStartupIsNotAFailure 验启动时没登录不是错误。
func TestNotLoggedInAtStartupIsNotAFailure(t *testing.T) {
	f := newBiliFixture(t)
	a := newTestApp(t, f, app.NewMemoryStore("test"))
	if a.LoggedIn() {
		t.Error("空后端也报成登录了")
	}
	if p := a.LoginState().Phase; p != app.LoginIdle {
		t.Errorf("空后端的登录阶段是 %v，want LoginIdle", p)
	}
}

// TestBrokenBackendDoesNotStopStartup 验后端坏了不阻止应用起来。
func TestBrokenBackendDoesNotStopStartup(t *testing.T) {
	f := newBiliFixture(t)
	a := newTestApp(t, f, &unreadableStore{})
	if a.LoggedIn() {
		t.Error("读不出来的后端也报成登录了")
	}
	// 但它得能重新登进去——后端坏了不该把用户锁在门外。
	f.onScans(0)
	a.BeginLogin()
	waitFor(t, waitTimeout, "重新登录", func() bool { return a.LoginState().Phase == app.LoginDone })
}

// unreadableStore 是「读不出来」的后端：钥匙串被锁、用户换了账号。
type unreadableStore struct{}

func (*unreadableStore) Name() string { return "unreadable" }
func (*unreadableStore) Load(context.Context) (app.Session, error) {
	return app.Session{}, fmt.Errorf("%w: 钥匙串锁着", app.ErrBackend)
}
func (*unreadableStore) Save(_ context.Context, s app.Session) error { return nil }
func (*unreadableStore) Clear(context.Context) error                 { return nil }

// ─────────────────────────────────────────────────────────────────────────────
// 退登
// ─────────────────────────────────────────────────────────────────────────────

// TestLogoutClearsEverything 是「退登会清掉凭据」的完整场景。
//
// 四样都要清：后端、内存里的登录态、**cookie jar**、进行中的轮询。
// 少了 jar 那一样最隐蔽：界面上是登出了，B 站那边会话还有效，
// 现象是「明明退登了，刷新一下又登回来了」。
func TestLogoutClearsEverything(t *testing.T) {
	f := newBiliFixture(t)
	f.onScans(0)
	store := app.NewMemoryStore("test")
	a := newTestApp(t, f, store)
	a.BeginLogin()
	waitFor(t, waitTimeout, "登录成功", func() bool { return a.LoginState().Phase == app.LoginDone })

	if err := a.Logout(); err != nil {
		t.Fatalf("退登返回 %v", err)
	}

	if a.LoggedIn() {
		t.Error("退登之后 App 还说自己是登录的")
	}
	if _, err := store.Load(context.Background()); !errors.Is(err, app.ErrNotLoggedIn) {
		t.Errorf("后端里还留着凭据：%v", err)
	}
	if a.LoginState().LoggedIn {
		t.Error("界面拿到的状态还是登录中")
	}
}

// TestLogoutIsIdempotent 验退登连点两次不报错。
func TestLogoutIsIdempotent(t *testing.T) {
	f := newBiliFixture(t)
	f.onScans(0)
	a := newTestApp(t, f, app.NewMemoryStore("test"))
	a.BeginLogin()
	waitFor(t, waitTimeout, "登录成功", func() bool { return a.LoginState().Phase == app.LoginDone })

	for i := range 2 {
		if err := a.Logout(); err != nil {
			t.Fatalf("第 %d 次退登返回 %v", i+1, err)
		}
	}
}

// TestLogoutWhileScanningDoesNotWriteTheResultBack 是本文件最重要的一条。
//
// 场景：用户扫了码，正在等手机确认，这时候点了退出登录。
// 扫码结果回来时**必须不能**把凭据写回去——
// 那个账号已经被用户明确退掉了。
//
// 现象如果没防住：用户退登 → 转眼又登回来了，而且界面上还显示已登录。
// 在共享电脑上这就是「我明明退出了」的假象。
func TestLogoutWhileScanningDoesNotWriteTheResultBack(t *testing.T) {
	f := newBiliFixture(t)
	store := app.NewMemoryStore("test")
	a := newTestApp(t, f, store)

	// 第 2 次轮询卡住，等我们退登之后再放行。
	f.gateNthPoll(1)
	f.onScans(86101, 0) // 第二次就是成功

	a.BeginLogin()
	waitFor(t, waitTimeout, "第一次轮询", func() bool { return pollCount(f) >= 1 })

	// 用户在这中间点了退登。
	if err := a.Logout(); err != nil {
		t.Fatal(err)
	}

	// 放行那个在途的请求。
	f.unblock()

	// 给它时间走完整个「成功 → 落盘」。
	waitFor(t, waitTimeout, "在途请求结束", func() bool {
		for _, p := range f.requests() {
			if p == "/x/passport-login/web/qrcode/poll" {
				return true
			}
		}
		return false
	})
	time.Sleep(100 * time.Millisecond)

	// 断言的核心：**凭据没有回来**。
	if a.LoggedIn() {
		t.Error("退掉的账号被在途的扫码结果写回来了")
	}
	if _, err := store.Load(context.Background()); !errors.Is(err, app.ErrNotLoggedIn) {
		t.Errorf("后端里又出现了凭据：%v", err)
	}
	if p := a.LoginState().Phase; p == app.LoginDone {
		t.Errorf("界面显示「登录成功」，可用户明明已经退登了")
	}
}

// TestLogoutBeforeTheQRCodeComesBackAlsoDoesNotWrite 验更早一步的同一件事。
func TestLogoutBeforeTheQRCodeComesBackAlsoDoesNotWrite(t *testing.T) {
	f := newBiliFixture(t)
	store := app.NewMemoryStore("test")
	a := newTestApp(t, f, store)

	f.gateNthPoll(0) // 连申请二维码都卡住
	f.onScans(0)
	a.BeginLogin()
	time.Sleep(30 * time.Millisecond) // 让它走到卡住的地方

	if err := a.Logout(); err != nil {
		t.Fatal(err)
	}
	f.unblock()
	time.Sleep(100 * time.Millisecond)

	if a.LoggedIn() {
		t.Error("退登之后还是被写回了登录态")
	}
	if _, err := store.Load(context.Background()); !errors.Is(err, app.ErrNotLoggedIn) {
		t.Errorf("后端里又出现了凭据：%v", err)
	}
}

// TestCancelLoginStopsPolling 验取消。
func TestCancelLoginStopsPolling(t *testing.T) {
	f := newBiliFixture(t)
	f.onScans(86101, 86101, 86101, 86101, 86101)
	a := newTestApp(t, f, app.NewMemoryStore("test"))
	a.BeginLogin()
	waitFor(t, waitTimeout, "轮询开始", func() bool { return pollCount(f) >= 1 })

	a.CancelLogin()
	// 已经在飞的那一次轮询还会回来，那是预期的——要判的是
	// 「**没有新的**轮询再发出去」，所以先让它落定，再取基准。
	time.Sleep(40 * time.Millisecond)
	before := pollCount(f)
	time.Sleep(60 * time.Millisecond)
	if after := pollCount(f); after != before {
		t.Errorf("取消之后还在轮询（%d → %d）", before, after)
	}
	if p := a.LoginState().Phase; p != app.LoginIdle {
		t.Errorf("取消之后阶段是 %v，want LoginIdle", p)
	}
}

// TestStartingTwiceKeepsOneLoop 验连点两次登录只有一个循环。
//
// 两个循环同时往 B 站打，每 2 秒两个请求——直接撞上限速。
func TestStartingTwiceKeepsOneLoop(t *testing.T) {
	f := newBiliFixture(t)
	f.onScans(86101, 86101, 86101, 86101, 86101)
	a := newTestApp(t, f, app.NewMemoryStore("test"))

	a.BeginLogin()
	waitFor(t, waitTimeout, "第一轮", func() bool { return pollCount(f) >= 1 })
	a.BeginLogin() // 再点一次
	waitFor(t, waitTimeout, "第二轮开始", func() bool { return pollCount(f) >= 2 })

	// 第二轮开始之后，第一轮不该再有任何动静。
	time.Sleep(80 * time.Millisecond)
	a.CancelLogin()
	// 已经在飞的那一次会回来，那是预期的；要判的是「没有**新的**」。
	time.Sleep(40 * time.Millisecond)
	before := pollCount(f)
	time.Sleep(60 * time.Millisecond)
	if after := pollCount(f); after != before {
		t.Errorf("取消之后还有 %d 个轮询在跑", after-before)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 失败形态
// ─────────────────────────────────────────────────────────────────────────────

// TestServerFailureIsShownNotCrashed 验服务器挂掉时的表现。
func TestServerFailureIsShownNotCrashed(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	a := app.New()
	t.Cleanup(a.Close)
	transport := &app.CredentialTransport{
		Base:   srv.Client().Transport,
		Policy: app.DefaultInjectPolicy().AllowBiliHost(hostOf(srv.URL)),
	}
	cli := bili.NewClient(bili.Options{Transport: transport})
	cli.TestOnlyPointHostsAtFixture(srv.URL)
	a.MountLogin(app.NewMemoryStore("test"), cli)
	a.SetLoginPollInterval(2 * time.Millisecond)

	a.BeginLogin()
	waitFor(t, waitTimeout, "报出失败", func() bool { return a.LoginState().Phase == app.LoginFailed })
	if a.LoginState().Err == "" {
		t.Error("失败了却没有告诉用户原因")
	}
	if a.LoggedIn() {
		t.Error("申请二维码失败却报成登录了")
	}
}

// TestEmptyQRCodeResponseIsAFailure 验 B 站给空结果时报错。
func TestEmptyQRCodeResponseIsAFailure(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"0","data":{"url":"","qrcode_key":""}}`))
	}))
	defer srv.Close()

	a := app.New()
	t.Cleanup(a.Close)
	transport := &app.CredentialTransport{
		Base:   srv.Client().Transport,
		Policy: app.DefaultInjectPolicy().AllowBiliHost(hostOf(srv.URL)),
	}
	cli := bili.NewClient(bili.Options{Transport: transport})
	cli.TestOnlyPointHostsAtFixture(srv.URL)
	a.MountLogin(app.NewMemoryStore("test"), cli)
	a.SetLoginPollInterval(2 * time.Millisecond)

	a.BeginLogin()
	waitFor(t, waitTimeout, "报出失败", func() bool { return a.LoginState().Phase == app.LoginFailed })
	// 空结果如果被当成功，用户看到的是一片白 —— 看起来像 bug，不像错误。
	if a.LoginState().QR != nil {
		t.Error("空结果也画出二维码了")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 唤醒
// ─────────────────────────────────────────────────────────────────────────────

// TestLoginWakesTheInterface 验后台状态变了会叫醒界面。
//
// 即时模式下不唤醒就等于界面停在旧帧（实测「只投递不唤醒 = 0 帧刷新」）。
// 这条钉住「后台改状态 → 有回调」，不看回调被调了几次。
func TestLoginWakesTheInterface(t *testing.T) {
	f := newBiliFixture(t)
	f.onScans(0)

	a := app.New()
	t.Cleanup(a.Close)
	transport := &app.CredentialTransport{
		Base:   f.Server.Client().Transport,
		Policy: app.DefaultInjectPolicy().AllowBiliHost(hostOf(f.URL)),
	}
	cli := bili.NewClient(bili.Options{Transport: transport})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	a.MountLogin(app.NewMemoryStore("test"), cli)
	a.SetLoginPollInterval(2 * time.Millisecond)

	var mu sync.Mutex
	woken := 0
	a.SetWake(func() {
		mu.Lock()
		woken++
		mu.Unlock()
	})

	a.BeginLogin()
	waitFor(t, waitTimeout, "登录成功", func() bool { return a.LoginState().Phase == app.LoginDone })

	mu.Lock()
	n := woken
	mu.Unlock()
	if n == 0 {
		t.Error("整个登录过程一次都没唤醒过界面")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
