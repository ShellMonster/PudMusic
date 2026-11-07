package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// loginLayoutShell 建一个停在登录页的外壳（窗口 w×800），不自动起扫码。
func loginLayoutShell(t *testing.T, w int) (*Shell, *mygoui.Tester) {
	t.Helper()
	b := newTestShell(t, at(w, 800), withSettings(app.DefaultSettings("")), stubbed(func(t *testing.T, s *Shell) {
		s.loginAutoDisabled = true
	}))
	b.s.navigate(loginPath)
	waitFrames(t, b.tt, "登录页", func() bool { return b.tt.HasText("扫码登录") })
	settleHome(b.tt)
	homeFrame(b.tt)
	return b.s, b.tt
}

// TestTheLoginCardStacksInANarrowWindow 锁定原型 index.html:943-944：窗口 ≤ 1024 时登录卡单列、
// 居中、二维码和文字之间缝 32；宽窗口两栏、缝 48（曾经恒两栏）。
func TestTheLoginCardStacksInANarrowWindow(t *testing.T) {
	_, narrow := loginLayoutShell(t, 960)
	qr, ok := narrow.Find("登录二维码")
	if !ok {
		t.Fatalf("登录页没有二维码卡，文字有 %q", narrow.Texts())
	}
	// 按 Eyebrow 量（「扫码登录」四个字也是路由标题，Find 会先找到别处）。
	eyebrow, _ := narrow.Find("SIGN IN")
	if eyebrow.Y < qr.Y+qr.H {
		t.Errorf("960 宽时文字列（y %.0f）不在二维码卡（底 %.0f）下面——没有改单列", eyebrow.Y, qr.Y+qr.H)
	}
	if d := (eyebrow.X + eyebrow.W/2) - (qr.X + qr.W/2); d < -2 || d > 2 {
		t.Errorf("960 宽时 Eyebrow 中线和二维码卡中线差 %.1f——没有居中", d)
	}
	if gap := eyebrow.Y - (qr.Y + qr.H); gap < 31 || gap > 33 {
		t.Errorf("二维码卡底到 Eyebrow 顶 %.1f，want 32", gap)
	}

	_, wide := loginLayoutShell(t, 1280)
	qr, _ = wide.Find("登录二维码")
	text, _ := wide.Find("SIGN IN")
	if gap := text.X - (qr.X + qr.W); gap < 47 || gap > 49 {
		t.Errorf("1280 宽时二维码卡右缘到右列 %.1f，want 48（两栏）", gap)
	}
}

// TestTheScanStepPointsWhereTheQRCodeIs 锁住第二步的文案跟着二维码的位置走：单列（≤ 1024，
// 二维码在上）写「扫描上方二维码」，两栏（二维码在左）写「扫描左侧二维码」。
//
// 曾经出错的行为：单列时二维码已经挪到文字上面，第二步还写「扫描左侧二维码」。
func TestTheScanStepPointsWhereTheQRCodeIs(t *testing.T) {
	for _, tc := range []struct {
		w          int
		want, lost string
	}{
		{960, "扫描上方二维码", "扫描左侧二维码"},
		{1024, "扫描上方二维码", "扫描左侧二维码"},
		{1280, "扫描左侧二维码", "扫描上方二维码"},
	} {
		_, tt := loginLayoutShell(t, tc.w)
		step, ok := tt.Find(tc.want)
		if !ok {
			t.Errorf("%d 宽时第二步不是「%s」，文字有 %q", tc.w, tc.want, tt.Texts())
			continue
		}
		if tt.HasText(tc.lost) {
			t.Errorf("%d 宽时还写着「%s」", tc.w, tc.lost)
		}
		// 文案说的方位要和画面一致：二维码确实在第二步的上方 / 左侧。
		qr, _ := tt.Find("登录二维码")
		if tc.w <= loginNarrowMax && qr.Y+qr.H > step.Y {
			t.Errorf("%d 宽时二维码（底 %.0f）不在第二步（顶 %.0f）上方", tc.w, qr.Y+qr.H, step.Y)
		}
		if tc.w > loginNarrowMax && qr.X+qr.W > step.X {
			t.Errorf("%d 宽时二维码（右 %.0f）不在第二步（左 %.0f）左侧", tc.w, qr.X+qr.W, step.X)
		}
	}
}

// TestTheLoginSkipOnlyShowsWithSomewhereToGoBack 锁定原型 app.js:795：「暂不登录」只在有返回
// 目标 / 历史时摆（曾经恒摆，登录页是第一页时点了也回不去）。
func TestTheLoginSkipOnlyShowsWithSomewhereToGoBack(t *testing.T) {
	s, tt := loginLayoutShell(t, 1280)
	if !tt.HasText("暂不登录") {
		t.Fatalf("从首页进登录页（有历史）却没有「暂不登录」，文字有 %q", tt.Texts())
	}
	s.router.Replace(loginPath)
	for s.router.CanGoBack() {
		s.router.Back()
		tt.Frame()
	}
	s.router.Replace(loginPath)
	homeFrame(tt)
	if s.router.CanGoBack() {
		t.Fatal("没能把登录页摆成第一页")
	}
	if tt.HasText("暂不登录") {
		t.Errorf("登录页是第一页（没处可回）时还摆着「暂不登录」")
	}
}

// TestTheLoginTitleUsesThePageTitleLeading 锁定原型 index.html:268 `.h-page { line-height: 1.15 }`：
// 40 号宋体标题的行盒高约 40×1.15 = 46（曾经按正文行高 1.55 排，约 62）。
func TestTheLoginTitleUsesThePageTitleLeading(t *testing.T) {
	_, tt := loginLayoutShell(t, 1280)
	eyebrow, _ := tt.Find("SIGN IN")
	lede, ok := tt.Find("继续你的音乐旅程：登录后同步收藏夹、关注和历史记录，并解锁 Hi-Res 无损音质。")
	if !ok {
		t.Fatalf("登录页没有说明那一段，文字有 %q", tt.Texts())
	}
	// Eyebrow 底 → 说明顶 = 8（标题上）+ 46（标题行盒）+ 8（说明上）。
	if got := lede.Y - (eyebrow.Y + eyebrow.H); got < 60 || got > 64 {
		t.Errorf("Eyebrow 底到说明顶 %.1f，want 约 62（8 + 40×1.15 + 8）", got)
	}
}
