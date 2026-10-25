package ui

import "testing"

// TestTheGuestFavoritesEmptyStateOffersTheNextStep 钉住「没登录时侧栏收藏夹那一片不是
// 一句孤零零的字」：旁边有一个能点的「去登录」，点了进扫码登录页。
func TestTheGuestFavoritesEmptyStateOffersTheNextStep(t *testing.T) {
	b := newTestShell(t)
	s, tt := b.s, b.tt
	tt.Frame()

	if _, ok := tt.Find("登录后能看到收藏夹"); !ok {
		t.Fatalf("侧栏上没有空状态那一句，文字有 %q", tt.Texts())
	}
	if err := tt.Click("去登录"); err != nil {
		t.Fatalf("点不到「去登录」：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
	if got := s.router.Path(); got != loginPath {
		t.Errorf("点了「去登录」之后在 %q，want %q", got, loginPath)
	}
}
