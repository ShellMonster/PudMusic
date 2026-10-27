// 评论请求在**生产那一层**的网络配置下也不带 buvid3。
//
// # 为什么这一组不在 `internal/service/bili` 里
//
// `bili` 包里那几条用例验的是「`bili.Client` 自己摘掉了 buvid3」。而**生产**
// 上 `bili.Client` 的运输层不是裸的 `http.Transport`，是
// `app.CredentialTransport`（它管凭据边界、重定向逐跳重判）。
//
// 两层叠起来的顺序是：
//
//	jar 加 cookie → noBuvidTransport 摘 buvid → CredentialTransport 判凭据边界
//
// 中间任何一层把头改回去，这条就漏。所以这一组用**真的**
// `CredentialTransport` + **真的**登录流程（扫码 → 下发 cookie → 落盘 →
// 装回 jar）跑一遍，看服务端到底收到了什么。
package app_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// commentReplyMain 是评论主列表那条路。
const commentReplyMain = "/x/v2/reply/main"

// TestTheLoginCookiesDoNotMakeTheCommentRequestCarryBuvid 钉住「登录之后拉评论，
// 请求里没有 buvid3，而登录态照带」。
//
// 🔴 **这是上一轮评审指出来的那个洞**：`app.Account.finish` 收下
// `IssuedCookies()` 的**每一条** cookie（B 站登录时下发的那一批里就有
// buvid3），而 jar 里的 cookie 是在运输层之前加上的——所以光「代码里没写
// `Call.Buvid`」**挡不住**它。
//
// 症状是「登录之后评论只有 3 条」，而 B 站那句「到底了」说得天衣无缝。
func TestTheLoginCookiesDoNotMakeTheCommentRequestCarryBuvid(t *testing.T) {
	f := newBiliFixture(t)
	// 扫一次就成功（默认是「还没扫」，那样永远等不到登录成功）。
	f.onScans(0)
	// 🔴 **扫码成功那一次要多下发一枚 buvid3**：这正是真实情况
	// （B 站登录时下发的 cookie 里有它），而 `app.Account.finish` 会把它
	// **全部**存下来。
	f.onScanCookie(&http.Cookie{Name: "buvid3", Value: "BUVID3-FROM-LOGIN", Path: "/"})
	f.on(commentReplyMain, func(w http.ResponseWriter, r *http.Request) {
		f.recordCommentCookie(r.Header.Get("Cookie"))
		writeFixtureJSON(t, w, 0, "0", map[string]any{
			"cursor": map[string]any{
				"is_begin": true, "prev": 0, "next": 2, "is_end": false,
				"mode": 3, "all_count": 0, "name": "热门评论",
			},
			"replies": []any{},
			"upper":   map[string]any{"mid": 0},
		})
	})

	a, cli := newTestAppAndClient(t, f, app.NewMemoryStore("test"), nil)
	a.BeginLogin()
	waitFor(t, waitTimeout, "登录成功", func() bool { return a.LoggedIn() })

	// ⚠️ **确认登录态里真的有一枚 buvid3**：不然下面那条断言是**空的**
	//（证明不了「摘掉了」，只证明「本来就没有」）。
	if !sessionHasCookie(a.Session(), "buvid3") {
		t.Fatal("登录之后落盘的凭据里没有 buvid3，这条用例证明不了任何东西")
	}

	if _, err := cli.CommentList(context.Background(),
		bili.CommentTypeVideo, 170001, int(bili.CommentSortHot), "", 20); err != nil {
		t.Fatal(err)
	}

	got := f.commentCookiesSeen()
	if len(got) == 0 {
		t.Fatal("评论请求没打到 fixture 上")
	}
	ck := got[len(got)-1]
	if strings.Contains(ck, "buvid") {
		t.Errorf("评论请求带出了 buvid 那一族：%q（B 站会因此只给 3 条）", ck)
	}
	// ⚠️ **登录态必须还在**：摘的是设备标识，不是凭据。
	if !strings.Contains(ck, "SESSDATA=sess-from-fixture") {
		t.Errorf("登录态被一起摘掉了：%q", ck)
	}
}

// sessionHasCookie 报这份登录态里有没有这个 cookie。
func sessionHasCookie(s app.Session, name string) bool {
	for _, c := range s.Cookies {
		if c.Name == name {
			return true
		}
	}
	return false
}
