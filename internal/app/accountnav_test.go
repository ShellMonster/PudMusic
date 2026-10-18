// 「我是谁、我的头像在哪」那一层的测试（issue #95）。
//
// 这一层只有**一个**闸门和**一个**结果形状，所以这里也只有两条真正要紧的：
// 没登录时一个请求都不发、登录了才拿得到地址。
//
// ⚠️ 界面上那些「未登录长什么样」的用例在 `internal/ui/navbar_avatar_test.go`，
// 那一层量的是画面。这一层量的是**闸门**——它在界面上看不出来：未登录时如果
// 悄悄发了一次 nav，界面上一切正常，只是日志里多出 B 站的 401，而真机联调
// 看到满屏 401 却查不出是谁在问，就是从这条来的。

package app_test

import (
	"context"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
)

// loggedInApp 造一个**已经登录**、并且指着这台假 B 站的 App。
//
// 走的是真那条路（申请二维码 → 扫成功 → 落盘），不是 `TestOnlyAdoptLogin`
// 那样往里塞一份状态：这一组要验的正是「凭据在，B 站也认」时的那一趟，
// 而塞进去的那种登录态没走过 cookie jar。
func loggedInApp(t *testing.T, f *biliFixture) *app.App {
	t.Helper()
	f.onScans(0)
	a := newTestApp(t, f, app.NewMemoryStore("test"))
	a.BeginLogin()
	waitFor(t, waitTimeout, "登录成功", func() bool { return a.LoginState().Phase == app.LoginDone })
	if !a.LoggedIn() {
		t.Fatal("登录没成功，起点就不对")
	}
	return a
}

// TestNobodyLoggedInAsksNobodyAnything 钉住「未登录时一个请求都不发」。
//
// ## 为什么这条得钉
//
// 这一层**返回零值**是对的两种情况之一（没挂登录 / 没登录）。而「没发请求」
// 和「发了、回来的答案是没登录」在返回值上完全分不开——分不开的地方正是
// 顶栏那一格：**它每一帧都被问一次**，一次多余的 nav 在那儿是每秒几十次，
// 凭据过期之后每一次都回 -101。
//
// 所以这条钉的不是返回值，是**那台假 B 站有没有收到请求**。
func TestNobodyLoggedInAsksNobodyAnything(t *testing.T) {
	f := newBiliFixture(t)
	// ⚠️ 这台 fake 明确回了「登录了、头像在这儿」。所以这一条要是偷偷发了，
	// 回来的还是一个**看起来能用**的答案——而不是一个一眼看出来的错误。
	f.onNav(map[string]any{
		"isLogin": true, "mid": 424242, "uname": "不该被问到的人",
		"face": "//i0.hdslb.com/bfs/face/nobody-asked.jpg",
	})
	a := newTestApp(t, f, app.NewMemoryStore("test"))
	if a.LoggedIn() {
		t.Fatal("起点就不对：这个 App 是登录态")
	}

	nav, err := a.AccountNav(context.Background())
	if err != nil {
		t.Fatalf("没登录不该是错误：%v", err)
	}
	if nav != (app.AccountNav{}) {
		t.Errorf("没登录却解出了东西：%+v", nav)
	}
	if n := f.hits(pathNav); n != 0 {
		t.Errorf("没登录也去问了 B 站 %d 次（收到 %v）——顶栏每一帧问一次，"+
			"凭据过期之后这就是每秒几十条 -101 噪声", n, f.requests())
	}
}

// TestARealAccountGetsItsOwnFace 验登录了拿得到**自己**的头像地址。
func TestARealAccountGetsItsOwnFace(t *testing.T) {
	f := newBiliFixture(t)
	f.onNav(map[string]any{
		"isLogin": true, "mid": 424242, "uname": "我",
		"face": "//i0.hdslb.com/bfs/face/mine.jpg",
	})
	a := loggedInApp(t, f)

	nav, err := a.AccountNav(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !nav.LoggedIn {
		t.Error("B 站认这个会话，界面上却被当成没登录")
	}
	// ⚠️ 断言的是**拼好的**地址（补协议 + 裁剪参数），不是原样透传。
	// 服务层那一层已经验过（`bili.TestNavCarriesMyOwnFace`），这里钉的是
	// 「这一层没把它弄丢、也没让它原样穿过去」——界面那层拿到的必须是能直接
	// 去取的那一份。
	if want := "https://i0.hdslb.com/bfs/face/mine.jpg"; len(nav.Face) < len(want) || nav.Face[:len(want)] != want {
		t.Errorf("头像地址是 %q，want %q 开头（协议没补或地址丢了）", nav.Face, want)
	}
	if f.hits(pathNav) == 0 {
		t.Error("登录了却压根没去问 B 站——头像会永远停在等人形")
	}
}

// TestADeadSessionIsNotTheSameAsNobodyBeingLoggedIn 验「B 站不认了」和「压根没登录」
// 在返回值上分得开。
//
// 这是界面那一格**最要紧的一处分岔**（见 `app/accountnav.go` 里 `AccountNav`
// 的注释）：本地有 SESSDATA、B 站说没有——那是「登录过期了，请重新登录」，
// 而不是「你还没登录」。合成一种的话，用户明明登录着却被界面说成没登录，
// 于是去重新扫码，而重新扫码**也没用**，因为闸门认定的是本地那份凭据。
func TestADeadSessionIsNotTheSameAsNobodyBeingLoggedIn(t *testing.T) {
	f := newBiliFixture(t)
	// 本地登录成功（凭据在），B 站那边说这个会话不算数。
	f.onScans(0)
	a := newTestApp(t, f, app.NewMemoryStore("test"))
	a.BeginLogin()
	waitFor(t, waitTimeout, "登录成功", func() bool { return a.LoginState().Phase == app.LoginDone })
	f.onNav(map[string]any{"isLogin": false, "mid": 0})

	nav, err := a.AccountNav(context.Background())
	if err != nil {
		t.Fatalf("会话失效是一个状态，不该是错误：%v", err)
	}
	if nav.LoggedIn {
		t.Error("B 站说不认，界面上却拿到的还是「已登录」")
	}
	if nav.Face != "" {
		t.Errorf("失效的会话却给出了一个头像地址：%q", nav.Face)
	}
}

// TestAccountNavWithoutALoginMountedSaysSo 验没挂登录时是一个能分出来的错。
//
// 「没挂登录」和「没登录」在返回值上分不开，而它们在**排障**上完全不是一件事：
// 真机上报说「头像一直没出来」，一个是应用起错了，另一个是网络的事。
// 这一条钉住它至少有 error 可看。
func TestAccountNavWithoutALoginMountedSaysSo(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	if _, err := a.AccountNav(context.Background()); err == nil {
		t.Error("压根没挂登录却回 nil 错误——分不出「应用起错了」和「没登录」")
	}
}
