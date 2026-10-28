// 票 #277（板子任务 BMG-277）的出图：**登录成功那一下**和它带出来的那一处
// 画面变化。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket277Shots
//
// # 出三处
//
//  1. **登录成功**：首页 + 右下角那一句「登录成功」。
//  2. **动态页的「去登录」**：那一页没登录时作者栏整个拉不出来，而这一句是
//     唯一能解开它的动作（这一条是这一票新加的入口）。
//  3. **登录页**：二维码那一段**一个字都没动**——出这张是为了看见它没变。
//
// ⚠️ **深浅两套都出**：提示那张卡片是**语义色**（成功 = 绿），而深浅两套下
// 那张卡片的底色不一样；动态页那一栏的次要文字（12px 淡灰）在深色下更淡。
//
// ⚠️ **一个字都不许打真 B 站接口**：全部是内存夹具 + 假 B 站（`httptest`）。
package ui

import (
	"fmt"
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// TestRenderTicket277Shots 把这一票动的三处画面存成 PNG，深浅各一套。
func TestRenderTicket277Shots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}

	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			shotLoginSuccess(t, dir, name, dark)
			shotDynamicLoginEntry(t, dir, name, dark)
			shotTicket277LoginPage(t, dir, name, dark)
		})
	}
}

// shotLoginSuccess 存「登录成功那一下」：首页 + 右下角那一句提示。
func shotLoginSuccess(t *testing.T, dir, name string, dark bool) {
	t.Helper()
	b := newUIBili(t, 0)
	a := newLoginApp(t, b)
	tt, _ := loginFlowTester(t, a, loginPath, systemIsDark(dark))
	tt.Frame()

	a.BeginLogin()
	waitLogin(t, a, app.LoginDone)
	tt.Frame()
	// ⚠️ **推钟**：提示的入场是 250ms 的一段，而 Tester 的钟站着不动——
	// 不推的话它一直停在「刚出现」（几乎全透明），画面上什么都看不见。
	settleToast(t, tt)

	save(t, dir, fmt.Sprintf("277-登录成功-提示-%s.png", name), tt)
}

// shotDynamicLoginEntry 存动态页那一栏：没登录时那句提示 + 「去登录」。
func shotDynamicLoginEntry(t *testing.T, dir, name string, dark bool) {
	t.Helper()
	b := newTestShell(t, systemIsDark(dark), mounted(func(_ *testing.T, a *app.App) {
		cli := bili.NewClient(bili.Options{})
		a.MountLogin(app.NewMemoryStore("test"), cli)
		a.MountExplore(cli)
	}))
	s, tt := b.s, b.tt
	s.navigate(dynamicFeedPath)
	waitFrames(t, tt, "动态页画出来了", func() bool {
		return tt.HasText("登录之后能看到你关注的人")
	})
	tt.Move(600, 780) // 挪到播放条那片空地上，别指着那一栏
	tt.Frame()
	save(t, dir, fmt.Sprintf("277-动态页-去登录-%s.png", name), tt)
	b.Close()
}

// shotLoginPage 存扫码登录那一页（**二维码那一段一个字都没动**）。
func shotTicket277LoginPage(t *testing.T, dir, name string, dark bool) {
	t.Helper()
	b := newUIBili(t, 86101)
	a := newLoginApp(t, b)
	tt, _ := loginFlowTester(t, a, loginPath, systemIsDark(dark))
	tt.Frame()

	a.BeginLogin()
	waitQRCode(t, a)
	waitLogin(t, a, app.LoginWaiting)
	tt.Frame()

	save(t, dir, fmt.Sprintf("277-登录页-二维码-%s.png", name), tt)
}
