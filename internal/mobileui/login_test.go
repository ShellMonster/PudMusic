package mobileui

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/egoist/mygo/ui"
)

// TestMobileQRLoginPollsAndRefreshes covers expiry, cancellation and same-device confirmation on the current QR session.
func TestMobileQRLoginPollsAndRefreshes(t *testing.T) {
	var requests atomic.Int32
	var status atomic.Int32
	status.Store(86090)
	fixture := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/x/passport-login/web/qrcode/generate":
			key := requests.Add(1)
			fmt.Fprintf(w, `{"code":0,"data":{"url":"https://passport.bilibili.com/h5-app/passport/login/scan?qrcode_key=test-%d","qrcode_key":"test-%d"}}`, key, key)
		case "/x/passport-login/web/qrcode/poll":
			code := status.Load()
			if code == 0 && r.URL.Query().Get("qrcode_key") != "test-3" {
				code = 86101
			} else if code == 0 {
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
	if err := tester.Click("继续登录"); err != nil {
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
	if slices.Contains(tester.Texts(), "在 B 站 App 中确认") {
		t.Fatal("expired QR session still offers same-device confirmation")
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
	tester.Frame()
	if err := tester.Click("暂不登录"); err != nil {
		t.Fatal(err)
	}
	if s.current().kind == pageLogin {
		t.Fatal("cancel did not return to the source page")
	}
	if phase := a.LoginState().Phase; phase != app.LoginIdle {
		t.Fatalf("login phase after leaving the route = %v, want idle", phase)
	}
	s.push(route{kind: pageLogin, title: "登录"})
	tester.Frame()
	if err := tester.Click("继续登录"); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for a.LoginState().QR == nil {
		if time.Now().After(deadline) {
			t.Fatal("same-device login did not receive a QR code")
		}
		time.Sleep(time.Millisecond)
	}
	tester.Frame()
	if code, found := tester.Find("B 站登录二维码"); !found || code.X+code.W/2 != 195 {
		t.Fatalf("QR code is not centered in the 390-point login page: %v", code)
	}
	if path := os.Getenv("PUDMUSIC_MOBILE_LOGIN_SCREENSHOT"); path != "" {
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		encodeErr := png.Encode(file, tester.Image())
		if err := errors.Join(encodeErr, file.Close()); err != nil {
			t.Fatal(err)
		}
	}
	tester.FailOpenURL(errors.New("no handler"))
	if err := tester.Click("在 B 站 App 中确认"); err != nil {
		t.Fatal(err)
	}
	tester.Frame()
	if !slices.Contains(tester.Texts(), "未能打开链接，请重试或使用另一台设备扫码。") {
		t.Fatal("failed launch did not show the scan fallback")
	}
	tester.FailOpenURL(nil)
	if err := tester.Click("在 B 站 App 中确认"); err != nil {
		t.Fatal(err)
	}
	const wantLink = "bilibili://browser/?url=https%3A%2F%2Fpassport.bilibili.com%2Fh5-app%2Fpassport%2Flogin%2Fscan%3Fqrcode_key%3Dtest-3"
	if links := tester.OpenedURLs(); len(links) == 0 || links[len(links)-1] != wantLink || requests.Load() != 3 {
		t.Fatal("same-device login did not open the current QR session")
	}
	status.Store(0)
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
