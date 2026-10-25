// 「登录成功那一下」这一组：一句提示、回该去的页、那一页不进历史、
// 那一页的临时状态清掉。
//
// # 断言面是**画面 + 路由**
//
// 提示那句是画面上的字（`tt.HasText`），去处是路由（`s.router.Path()`）——
// 后者是「用户看到的是哪一页」唯一的说法（`router_test.go` 那些也是这么
// 断的）。
//
// ⚠️ **不许拿「函数被调了几次」当判据**：那是实现细节。这一组断的是**用户
// 看得见的结果**——提示说了没有、落在哪一页、后退回不回得去登录页。
package ui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// loginFlowTester 造一个停在某条路由上的 Tester，**但不每帧把路由按回去**。
//
// ⚠️ **为什么不能直接用 `loginTester`**（login_test.go 里那一个）：那一个
// 每帧都 `Push("/login")`（它要的是「一直停在登录页」），于是路由永远回
// 不去——而这一组要验的正是「登录成功之后路由**换了**」。
//
// start 是**第一帧**停在哪条路由上。
func loginFlowTester(t *testing.T, a *app.App, start string, opts ...shellOpt) (*mygoui.Tester, *Shell) {
	t.Helper()
	pushed := false
	all := append([]shellOpt{onApp(a), at(900, 600)}, opts...)
	all = append(all, withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			ApplyTheme(c, app.DefaultSettings(""), c.Theme().Dark)
			if !pushed {
				pushed = true
				s.router.Push(start)
			}
			s.View(c)
		}
	}))
	b := newTestShell(t, all...)
	return b.tt, b.s
}

// loginSucceeds 把登录那一轮跑到成功，然后画一帧（那一帧就是「成功那一下」
// 发生的那一帧）。
func loginSucceeds(t *testing.T, tt *mygoui.Tester, a *app.App) {
	t.Helper()
	a.BeginLogin()
	waitLogin(t, a, app.LoginDone)
	tt.Frame()
}

// TestLoginSuccessSaysItAndGoesHome 是这一组的主线。
//
// 用户 2026-10-25 原话：「登录成功后应该提示并且回到首页，现在是还留在
// 页面上」——**提示**和**回到首页**两半都在这里。
func TestLoginSuccessSaysItAndGoesHome(t *testing.T) {
	b := newUIBili(t, 0)
	a := newLoginApp(t, b)
	tt, s := loginFlowTester(t, a, loginPath)
	tt.Frame()

	loginSucceeds(t, tt, a)

	if !tt.HasText("登录成功") {
		t.Errorf("登录成功之后没有那句提示。\n画面上的字是：\n%s", screenText(tt))
	}
	if got := s.router.Path(); got != homePath {
		t.Errorf("登录成功之后停在 %q，want %q（首页）", got, homePath)
	}
}

// TestLoginSuccessHappensOnlyOnce 断「副作用只发生一次」。
//
// ⚠️ **判据是「用户自己走了以后没人把他拽回来」**：登录成功那一下之后，
// 用户自己走到别的页去了——如果那一副作用是每帧都在做的，他会被立刻拽回
// 登录完该去的那一页（而他在别处）。
//
// 这也是「不是每帧跳一次」那个坑的样子：判据写成「现在是已登录就跳」的
// 时候，症状正是**每帧都跳**，而画面上只是「页面乱跳」。
func TestLoginSuccessHappensOnlyOnce(t *testing.T) {
	b := newUIBili(t, 0)
	a := newLoginApp(t, b)
	tt, s := loginFlowTester(t, a, loginPath)
	tt.Frame()

	loginSucceeds(t, tt, a)
	if got := s.router.Path(); got != homePath {
		t.Fatalf("登录成功之后停在 %q，want %q", got, homePath)
	}

	// 用户自己走了。
	s.navigate(settingsPath)
	tt.Frame()
	for range 5 {
		tt.Frame()
	}
	if got := s.router.Path(); got != settingsPath {
		t.Errorf("登录成功那一下之后又把人拽走了：现在停在 %q，want %q", got, settingsPath)
	}
}

// TestLoginSuccessLeavesTheLoginPageOutOfTheHistory 断「登录页不进历史」。
//
// ⚠️ **判据是「后退回不到登录页」**：用户按「后退」要回到**他登录之前**在
// 的那一页，而不是登录页（那里是登录成功之前的样子）。
//
// 用的是 `Replace` 而不是 `Push`（见 `finishLogin`）：`Replace` 把当前那一条
// **换掉**，于是历史里根本没有登录页这一条。
func TestLoginSuccessLeavesTheLoginPageOutOfTheHistory(t *testing.T) {
	b := newUIBili(t, 0)
	a := newLoginApp(t, b)
	tt, s := loginFlowTester(t, a, homePath)
	tt.Frame()

	// 先走到别的页去——这样「后退」有一个**该去的地方**，而那个地方不是登录页。
	s.navigate(settingsPath)
	tt.Frame()
	s.goLogin(homePath)
	tt.Frame()
	if got := s.router.Path(); got != loginPath {
		t.Fatalf("点了登录之后停在 %q，want %q", got, loginPath)
	}

	loginSucceeds(t, tt, a)
	if got := s.router.Path(); got != homePath {
		t.Fatalf("登录成功之后停在 %q，want %q", got, homePath)
	}

	s.router.Back()
	tt.Frame()
	if got := s.router.Path(); got != settingsPath {
		t.Errorf("后退之后停在 %q，want %q（登录之前那一页）——登录页留在历史里了", got, settingsPath)
	}
}

// TestLoginSuccessClearsTheLoginPagesTemporaryState 断「登录页的临时状态
// 清掉了」。
//
// 三样：二维码那张位图（**GPU 纹理**，留在壳上就一直占着显存）、状态里
// 那张二维码本身、还有轮询。
//
// ⚠️ **轮询那一样断在应用层**：它在 `app.Account` 那一层，登录成功的时候
// 那一轮 `run` 自己就返回了（`ticker` 跟着 `defer` 收掉）。界面这一侧
// 看得见的是「不再 busy 了」——`LoginState.Busy()` 说的是「这一轮还在跑」。
func TestLoginSuccessClearsTheLoginPagesTemporaryState(t *testing.T) {
	b := newUIBili(t, 0)
	a := newLoginApp(t, b)
	tt, s := loginFlowTester(t, a, loginPath)
	tt.Frame()

	a.BeginLogin()
	waitQRCode(t, a)
	tt.Frame()
	if s.qrBitmap == nil {
		t.Fatal("二维码出来了，位图却是 nil——这一条没验到东西")
	}

	loginSucceeds(t, tt, a)

	if s.qrBitmap != nil || s.qrDimBitmap != nil || s.qrFor != nil {
		t.Errorf("登录成功之后登录页的位图还留着（bitmap %p / dim %p / 码 %p）——"+
			"显存一直占着，而登录页已经不在了",
			s.qrBitmap, s.qrDimBitmap, s.qrFor)
	}
	if st := a.LoginState(); st.QR != nil {
		t.Error("登录成功了，状态里那张二维码还挂着")
	}
	if st := a.LoginState(); st.Busy() {
		t.Error("登录成功了，轮询还在跑")
	}
}

// TestLoginFromTheDynamicPageComesBackToTheDynamicPage 断「从「动态」那一路
// 进去的，登录完去动态页」。
//
// ⚠️ **入口是动态页上那个「去登录」**（`dynamicfeed.go` 的作者栏）：那一页
// 没登录时作者栏整个拉不出来，而「去登录」是唯一能解开它的动作。
//
// ⚠️ **这一条和「别的入口去首页」是一对**：从顶栏那枚进去的落首页，从
// 动态页进去的落动态页——**去处是入口自己说的**（`goLogin(after)`）。
func TestLoginFromTheDynamicPageComesBackToTheDynamicPage(t *testing.T) {
	b := newUIBili(t, 0)
	a := newLoginApp(t, b)
	tt, s := loginFlowTester(t, a, dynamicFeedPath)
	tt.Frame()

	if err := tt.Click("去登录"); err != nil {
		t.Fatalf("动态页上点不到「去登录」：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
	if got := s.router.Path(); got != loginPath {
		t.Fatalf("点了「去登录」之后停在 %q，want %q", got, loginPath)
	}

	loginSucceeds(t, tt, a)

	if got := s.router.Path(); got != dynamicFeedPath {
		t.Errorf("从动态页进去登录的，登录完停在 %q，want %q（动态页）", got, dynamicFeedPath)
	}
}

// TestLoginFromTheTopBarGoesHome 断「别的入口去首页」。
//
// 顶栏那枚「登录」是**最泛的那个入口**，所以去处是首页（用户 2026-10-25
// 原话：「登录成功后应该提示并且回到首页」）。
func TestLoginFromTheTopBarGoesHome(t *testing.T) {
	b := newUIBili(t, 0)
	a := newLoginApp(t, b)
	tt, s := loginFlowTester(t, a, homePath)
	tt.Frame()

	if err := tt.Click("登录"); err != nil {
		t.Fatalf("顶栏上点不到那枚登录键：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
	if got := s.router.Path(); got != loginPath {
		t.Fatalf("点了登录之后停在 %q，want %q", got, loginPath)
	}

	loginSucceeds(t, tt, a)

	if got := s.router.Path(); got != homePath {
		t.Errorf("从顶栏进去登录的，登录完停在 %q，want %q（首页）", got, homePath)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 三个边界
// ─────────────────────────────────────────────────────────────────────────────

// TestAFailedLoginStaysOnTheLoginPage 断边界一：**登录失败**留在登录页。
//
// ⚠️ 失败那一声是「登录成功」的**反面**：不弹那句提示，也不跳。
//
// 这一条用的是「扫码成功了、却不发凭据」那台假 B 站——它落在
// `app.Account.finish` 的那一支上：登录态存不下来之前，**登录态压根没
// 立起来**，于是判失败（不落一份登不进去的凭据）。
func TestAFailedLoginStaysOnTheLoginPage(t *testing.T) {
	b := newUIBiliWithoutCredentials(t)
	a := newLoginApp(t, b)
	tt, s := loginFlowTester(t, a, loginPath)
	tt.Frame()

	a.BeginLogin()
	waitLogin(t, a, app.LoginFailed)
	tt.Frame()

	if tt.HasText("登录成功") {
		t.Errorf("登录失败了却说了「登录成功」。\n画面上的字是：\n%s", screenText(tt))
	}
	if got := s.router.Path(); got != loginPath {
		t.Errorf("登录失败之后停在 %q，want %q（留在登录页）", got, loginPath)
	}
}

// TestComingBackToTheLoginPageWhenAlreadyLoggedIn 断边界二：**已登录**时再进
// `/login`。
//
// 那一页照旧（显示已登录状态、给「去首页」和「换一个账号」），而**不弹
// 「登录成功」、也不跳**——因为判据是**相位的那一次边沿**，不是「现在是
// 已登录」。
//
// ⚠️ **判据是边沿，不是状态**：拿「现在是已登录」当判据的话，这一条会
// 立刻炸——而那正是「每帧跳一次」那个坑。
//
// ⚠️ **两种「已登录」都要断**：**这一场登录刚成功**（相位 = `LoginDone`）
// 和**启动时从盘上读回凭据**（相位还是 `LoginIdle`，而 `LoggedIn()` 已经是
// 真）——后者才是「应用一开就误判成登录成功过一次」那个坑。
func TestComingBackToTheLoginPageWhenAlreadyLoggedIn(t *testing.T) {
	t.Run("这一场刚登录成功", func(t *testing.T) {
		b := newUIBili(t, 0)
		a := newLoginApp(t, b)
		tt, s := loginFlowTester(t, a, loginPath)
		tt.Frame()

		loginSucceeds(t, tt, a)
		if got := s.router.Path(); got != homePath {
			t.Fatalf("登录成功之后停在 %q，want %q", got, homePath)
		}

		// 用户又回到登录页（已登录时进得去：那一页给的是「换一个账号」）。
		s.navigate(loginPath)
		tt.Frame()
		for range 5 {
			tt.Frame()
		}

		if tt.HasText("登录成功") {
			t.Errorf("已登录时再进登录页又说了一遍「登录成功」。\n画面上的字是：\n%s", screenText(tt))
		}
		if got := s.router.Path(); got != loginPath {
			t.Errorf("已登录时再进登录页，却被跳走了：现在停在 %q，want %q", got, loginPath)
		}
	})

	t.Run("启动时从盘上读回凭据", func(t *testing.T) {
		b := newTestShell(t, loggedInWith("7"))
		s, tt := b.s, b.tt
		if !s.app.LoggedIn() {
			t.Fatal("这个外壳本该是已登录的")
		}

		s.navigate(loginPath)
		tt.Frame()
		for range 5 {
			tt.Frame()
		}

		if tt.HasText("登录成功") {
			t.Errorf("一开应用就说了「登录成功」——那是把读回来的凭据当成了刚登录。\n画面上的字是：\n%s", screenText(tt))
		}
		if got := s.router.Path(); got != loginPath {
			t.Errorf("已登录时进登录页，却被跳走了：现在停在 %q，want %q", got, loginPath)
		}
	})
}

// TestTheUserWhoLeftTheLoginPageIsNotDraggedAway 断边界三：**登录成功的那一刻
// 用户已经切到别的页面了**。
//
// 那一句话要说（登录**真的**成功了，那是全局的事），但**不把他从当前页拽
// 走**——他明明是自己走开的，把他拽走是抢他的操作。
func TestTheUserWhoLeftTheLoginPageIsNotDraggedAway(t *testing.T) {
	b := newUIBili(t, 0)
	a := newLoginApp(t, b)
	tt, s := loginFlowTester(t, a, loginPath)
	tt.Frame()

	a.BeginLogin()
	waitQRCode(t, a)
	tt.Frame()

	// 用户扫完码之前自己走了。
	s.navigate(settingsPath)
	tt.Frame()

	waitLogin(t, a, app.LoginDone)
	tt.Frame()
	for range 3 {
		tt.Frame()
	}

	if got := s.router.Path(); got != settingsPath {
		t.Errorf("人已经走了还被拽走：现在停在 %q，want %q", got, settingsPath)
	}
	if !tt.HasText("登录成功") {
		t.Errorf("人已经走了，那句「登录成功」还是要说的。\n画面上的字是：\n%s", screenText(tt))
	}
}

// newUIBiliWithoutCredentials 是一台「说登录成功了、却不发凭据」的假 B 站。
//
// 它落在 `app.Account.finish` 的那一支上：扫码成功了，可 jar 里没有
// SESSDATA——应用层于是判**登录失败**（不落一份登不进去的凭据，下次启动
// 一直 -101）。
func newUIBiliWithoutCredentials(t *testing.T) *uiBili {
	t.Helper()
	b := &uiBili{sticky: 0}
	b.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/passport-login/web/qrcode/generate":
			writeUIJSON(t, w, 0, "0", map[string]any{
				"url":        "https://passport.bilibili.com/h5-app/passport/login/scan?qrcode_key=fixture",
				"qrcode_key": "fixture-qrcode-key-000000000000000",
			})
		case "/x/passport-login/web/qrcode/poll":
			// ⚠️ **不发 SESSDATA**：B 站说成功了，而凭据没来。
			writeUIJSON(t, w, 0, "0", map[string]any{
				"code": 0, "message": "fixture",
				"refresh_token": "rt-ui", "timestamp": time.Now().UnixMilli(),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(b.Close)
	b.addr = b.Server.Listener.Addr().String()
	return b
}
