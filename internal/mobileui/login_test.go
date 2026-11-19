package mobileui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/egoist/mygo/ui"
)

// TestMobileQRLoginPollsAndRefreshes exercises scan, expiry, cancel and confirmation against a local service.
func TestMobileQRLoginPollsAndRefreshes(t *testing.T) {
	var requests atomic.Int32
	var status atomic.Int32
	status.Store(86090)
	fixture := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/x/passport-login/web/qrcode/generate":
			key := requests.Add(1)
			fmt.Fprintf(w, `{"code":0,"data":{"url":"https://example.test/login/%d","qrcode_key":"test-%d"}}`, key, key)
		case "/x/passport-login/web/qrcode/poll":
			code := status.Load()
			if code == 0 {
				http.SetCookie(w, &http.Cookie{Name: "SESSDATA", Value: "synthetic-mobile-session", Path: "/"})
			}
			fmt.Fprintf(w, `{"code":0,"data":{"code":%d}}`, code)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fixture.Close)
	a := app.New()
	t.Cleanup(a.Close)
	client := bili.NewClient(bili.Options{Transport: fixture.Client().Transport})
	client.TestOnlyPointHostsAtFixture(fixture.URL)
	store := app.NewMemoryStore("fixture")
	a.MountLogin(store, client)
	a.SetLoginPollInterval(2 * time.Millisecond)
	s := newShell(context.Background(), a)
	s.push(route{kind: pageLogin, title: "登录"})
	tester := ui.NewTester(func(c *ui.Context) {
		if page := s.current(); page.kind == pageLogin {
			ui.Column(c).FillWidth().PaddingX(16).Children(func() { s.login(c, page) })
		}
	}, 390, 700)
	if err := tester.Click("使用另一台设备扫码"); err != nil {
		t.Fatal(err)
	}
	waitForMobileLogin(t, a, app.LoginScanned)
	tester.Frame()
	if !slices.Contains(tester.Texts(), "已扫码，等待确认…") {
		t.Fatalf("missing scan confirmation status in %q", tester.Texts())
	}
	status.Store(86038)
	waitForMobileLogin(t, a, app.LoginExpired)
	tester.Frame()
	if !slices.Contains(tester.Texts(), "二维码已过期，请刷新二维码") {
		t.Fatalf("missing expired status in %q", tester.Texts())
	}
	status.Store(86101)
	if err := tester.Click("刷新二维码"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for requests.Load() != 2 || a.LoginState().QR == nil {
		if time.Now().After(deadline) {
			t.Fatal("refresh did not request a new QR code")
		}
		time.Sleep(time.Millisecond)
	}
	if err := tester.Click("暂不登录"); err != nil {
		t.Fatal(err)
	}
	if s.current().kind == pageLogin {
		t.Fatal("cancel did not return to the source page")
	}
	if phase := a.LoginState().Phase; phase != app.LoginIdle {
		t.Fatalf("login phase after leaving the route = %v, want idle", phase)
	}
	status.Store(0)
	s.push(route{kind: pageLogin, title: "登录"})
	tester.Frame()
	if err := tester.Click("使用另一台设备扫码"); err != nil {
		t.Fatal(err)
	}
	waitForMobileLogin(t, a, app.LoginDone)
	tester.Frame()
	if !slices.Contains(tester.Texts(), "登录成功") {
		t.Fatalf("missing successful login status in %q", tester.Texts())
	}
	if err := tester.Click("返回"); err != nil {
		t.Fatal(err)
	}
	stored, err := store.Load(context.Background())
	if err != nil || !stored.LoggedIn() || !a.LoggedIn() || s.current().kind == pageLogin {
		t.Fatalf("confirmed login did not save credentials and return to source: %v", err)
	}
}

func waitForMobileLogin(t *testing.T, a *app.App, phase app.LoginPhase) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for a.LoginState().Phase != phase {
		if time.Now().After(deadline) {
			t.Fatalf("login phase = %v, want %v", a.LoginState().Phase, phase)
		}
		time.Sleep(time.Millisecond)
	}
}
