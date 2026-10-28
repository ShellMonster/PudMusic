// 这一组盯的是**换账号时侧栏上那一片**（issue #398）。
//
// 用户看得见的症状是「侧栏上是上一个账号的歌单」——而这一条要在**画面**上
// 钉它：应用层把旧数据清掉之后，界面必须**重画**。即时模式是按需绘制，
// 只投递不唤醒的话界面会一直停在旧帧（AGENTS.md §4 铁律三）——而那一帧里
// 摆着的正是上一个账号的歌单。
//
// 这一组和应用层那组（`internal/app/favorites_switchaccount_test.go`）分工：
// 应用层那组钉**状态怎么变**，这一组钉**画面上是什么**。

package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	mygoui "github.com/egoist/mygo/ui"
)

// 换账号这一组用例里的两个账号，和它们各自的歌单名。
const (
	switchOldUID = "111"
	switchNewUID = "222"

	switchOldTitle = "旧账号的歌单"
	switchNewTitle = "新账号的歌单"
)

// switchBili 是一台「登录 + 收藏夹」都答的假 B 站。
//
// ⚠️ **登录和收藏夹在同一台上**：全项目一个 `bili` 客户端（见
// `app.MountCollections` 的注释），而登录和收藏夹都要用它——分成两台的话，
// 客户端只能指一处，另一处的请求就打到真的 B 站上去了。
type switchBili struct {
	*httptest.Server

	mu sync.Mutex
	// uid 是扫码成功时下发的那个 uid（DedeUserID）。
	uid string
	// gateOld 卡住**第二次**收藏夹列表请求（那是旧账号那一趟）。
	// nil = 不卡。
	gateOld <-chan struct{}
	// gateNew 卡住**第三次**收藏夹列表请求（那是新账号那一趟）。
	// nil = 不卡。
	gateNew <-chan struct{}
	// arrived 是收藏夹列表接口收到了几个请求（记在闸门**之前**）。
	arrived int
	// oldCount / newCount 是两个账号各自问了几回。
	oldCount, newCount int
}

// newSwitchBili 建一台假 B 站，扫出来的是 uid 那个账号。
func newSwitchBili(t *testing.T, uid string) *switchBili {
	t.Helper()
	b := &switchBili{uid: uid}
	b.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/x/passport-login/web/qrcode/generate":
			writeUIJSON(t, w, 0, "0", map[string]any{
				"url":        "https://passport.bilibili.com/h5-app/passport/login/scan?qrcode_key=fixture",
				"qrcode_key": "fixture-qrcode-key-000000000000000",
			})
		case "/x/passport-login/web/qrcode/poll":
			b.mu.Lock()
			uid := b.uid
			b.mu.Unlock()
			// 真 B 站是在**同一个响应**里把凭据一起下发的，这里照抄那个形态。
			http.SetCookie(w, &http.Cookie{Name: "SESSDATA", Value: "sess-from-switch", Path: "/"})
			http.SetCookie(w, &http.Cookie{Name: "bili_jct", Value: "jct-from-switch", Path: "/"})
			http.SetCookie(w, &http.Cookie{Name: "DedeUserID", Value: uid, Path: "/"})
			writeUIJSON(t, w, 0, "0", map[string]any{
				"code": 0, "message": "fixture",
				"refresh_token": "rt-switch", "timestamp": time.Now().UnixMilli(),
			})
		case "/x/v3/fav/folder/created/list":
			b.createdList(t, w, r)
		case "/x/v3/fav/folder/collected/list":
			writeUIJSON(t, w, 0, "0", map[string]any{
				"count": 0, "list": []map[string]any{},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(b.Close)
	return b
}

// createdList 答「我创建的」那个列表。
//
// ⚠️ **回答按 up_mid 分**：问的是哪个账号，就回那个账号的歌单。这样「侧栏上摆的
// 是谁的歌单」是从**画面上的字**读出来的，而不是从「请求发了几回」推出来的。
//
// ⚠️ **闸门只卡指定的那一趟**：卡住全部的话，第一趟（旧账号的收藏夹要摆上
// 屏幕）也过不去，测的就不是「换账号」而是「拉不到东西」了。
func (b *switchBili) createdList(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	mid := r.URL.Query().Get("up_mid")

	b.mu.Lock()
	b.arrived++
	// ⚠️ **闸门按账号分，不按「第几个请求」分**：请求的先后不是这一组能定的
	// （换人那一下之前旧账号可能已经问过好几回），而「哪一趟该卡住」是说得清的
	// ——旧账号的**第二趟**（换人之前发出去的那一趟）和新账号的**第一趟**。
	var gate <-chan struct{}
	switch mid {
	case switchNewUID:
		b.newCount++
		if b.newCount == 1 {
			gate = b.gateNew
		}
	default:
		b.oldCount++
		if b.oldCount >= 2 {
			gate = b.gateOld
		}
	}
	b.mu.Unlock()
	if gate != nil {
		<-gate
	}

	title := switchOldTitle
	if mid == switchNewUID {
		title = switchNewTitle
	}
	writeUIJSON(t, w, 0, "0", map[string]any{
		"count": 1, "has_more": false,
		"list": []map[string]any{{"id": 11, "title": title, "attr": 0, "media_count": 1}},
	})
}

// setUID 改成「接下来扫出来的是这个 uid」。
func (b *switchBili) setUID(uid string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.uid = uid
}

// setGates 装上那两道闸门。
func (b *switchBili) setGates(oldGate, newGate <-chan struct{}) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.gateOld, b.gateNew = oldGate, newGate
}

// arrivals 是收藏夹列表接口收到了几个请求。
func (b *switchBili) arrivals() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.arrived
}

// switchShell 造一个「登录 + 收藏夹都指着那台假 B 站」的外壳。
//
// ⚠️ **登录态里要带 Cookies**：收藏夹列表接口问的是 `up_mid`，而那一层从
// **cookie** 里读它（见 `bili.Client.SelfMid` 的注释）。只存 DedeUserID 这个
// 字段的话，客户端那边读出来的 mid 是 0，于是那一趟会说「还没登录」。
func switchShell(t *testing.T, f *switchBili, uid string) (*Shell, *mygoui.Tester) {
	t.Helper()
	mount := mounted(func(t *testing.T, a *app.App) {
		transport := &app.CredentialTransport{
			Base:   f.Server.Client().Transport,
			Policy: app.DefaultInjectPolicy().AllowBiliHost(hostOfFixture(f.URL)),
		}
		cli := bili.NewClient(bili.Options{Transport: transport, CSRF: func() string { return "jct" }})
		cli.TestOnlyPointHostsAtFixture(f.URL)
		store := app.NewMemoryStore("test")
		if err := store.Save(context.Background(), app.Session{
			SESSDATA: "sess", DedeUserID: uid,
			Cookies: []app.Cookie{{Name: "DedeUserID", Value: uid, Path: "/"}},
		}); err != nil {
			t.Fatal(err)
		}
		a.MountLogin(store, cli)
		// 轮询间隔默认 2 秒，测试里等不起。
		a.SetLoginPollInterval(2 * time.Millisecond)
		a.MountCollections(app.CollectionRefs{Client: cli, Settle: 0})
	})
	s, tt := newShell(t, mount)
	s.app.SetWake(s.wake.invalidate)
	return s, tt
}

// TestSwitchingAccountsTakesTheOldAccountsFavoritesOffTheSidebar 钉「换账号之后，
// 侧栏上不许还摆着上一个账号的收藏夹」。
//
// 这一条是 issue #398 说的那个症状本身：**用户看到的是上一个账号的歌单**。
//
// ⚠️ **三段都要看**：
//
//   - 换完人，旧账号那份要从画面上消失（而且界面得**重画**——即时模式不唤醒
//     的话就停在旧帧）；
//   - 旧账号那一趟回来之后**不许**把它填回去（世代守卫）；
//   - 新账号那一趟回来之后，画面上才是新账号的歌单。
func TestSwitchingAccountsTakesTheOldAccountsFavoritesOffTheSidebar(t *testing.T) {
	f := newSwitchBili(t, switchOldUID)
	gateOld := make(chan struct{})
	gateNew := make(chan struct{})
	releaseOld := sync.OnceFunc(func() { close(gateOld) })
	releaseNew := sync.OnceFunc(func() { close(gateNew) })
	// ⚠️ **必须放走**：测试收尾时 `httptest.Server.Close` 要等在途的请求回来，
	// 不放就是整个测试挂到 go test 的上限。
	defer releaseOld()
	defer releaseNew()
	f.setGates(gateOld, gateNew)

	s, tt := switchShell(t, f, switchOldUID)
	tt.Frame()

	// 先让侧栏上摆着旧账号的收藏夹。
	s.app.RefreshFavorites()
	waitFrames(t, tt, "侧栏上出现了旧账号的收藏夹", func() bool {
		return tt.HasText(switchOldTitle)
	})

	// 再刷一次：这一趟卡在飞（闸门），它就是「旧账号那一趟」。
	s.app.RefreshFavorites()
	waitFor(t, "旧账号那一趟到了假服务器", func() bool { return f.arrivals() >= 2 })

	// 换人：已登录的时候再扫一次码，扫出来的是**另一个人**。
	f.setUID(switchNewUID)
	s.app.BeginLogin()
	defer s.app.CancelLogin()
	waitFrames(t, tt, "登录成另一个人", func() bool {
		return s.app.LoginState().Phase == app.LoginDone
	})
	// ⚠️ **旧账号的收藏夹不许还在画面上**。
	waitFrames(t, tt, "旧账号的收藏夹从侧栏上消失了", func() bool {
		return !tt.HasText(switchOldTitle)
	})
	// 而这一片得**说得清是在飞**：那一行是用户唯一看得见的在飞证据。
	waitFrames(t, tt, "侧栏上说正在刷新", func() bool {
		return tt.HasText(refreshLine)
	})
	// 空的那一档也得说对：不是「还没有收藏夹」（那是在替 B 站回答一个我们还没
	// 问过的问题），而是「还没拉收藏夹」。
	if !tt.HasText("还没拉收藏夹") {
		t.Errorf("换账号之后侧栏上没说「还没拉收藏夹」，文字有 %q", tt.Texts())
	}
	// 新账号那一趟要发得出去——不然新账号的收藏夹永远不来。
	waitFor(t, "新账号那一趟到了假服务器", func() bool { return f.arrivals() >= 3 })

	// 放行旧账号那一趟：它回来之后**填不回去**。
	releaseOld()
	waitFrames(t, tt, "旧账号那一趟回来之后画面上仍然是空的", func() bool {
		return !tt.HasText(switchOldTitle) && tt.HasText(refreshLine)
	})

	// 放行新账号那一趟：画面上才是新账号的歌单。
	releaseNew()
	waitFrames(t, tt, "新账号的收藏夹进侧栏了", func() bool {
		return tt.HasText(switchNewTitle)
	})
	if tt.HasText(switchOldTitle) {
		t.Errorf("侧栏上还摆着上一个账号的收藏夹，文字有 %q", tt.Texts())
	}
}
