package ui

// 这一组盯的是**侧栏里那两组收藏夹**：
//
//   - 空状态**只有一句**，而且**缩进对**（报告 C3）；
//   - 侧栏折叠起来的时候，收藏夹**不是整组消失**（报告 C4）。
//
// 出处是 UX 审计报告（`~/.herdr-projects/reports/Biu_MyGo/125/report.md`，
// 只查不改）。

import (
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/ui"
)

// collapsedSidebar 造一个**侧栏折叠着**的外壳。
func collapsedSidebar(t *testing.T) (*Shell, *ui.Tester) {
	t.Helper()
	st := app.DefaultSettings("")
	st.SideMenuCollapsed = true
	s, tt := newShell(t, withSettings(st))
	tt.Frame()
	return s, tt
}

// TestTheSidebarSaysThereAreNoFavoritesOnlyOnce 断侧栏里那一句「还没有收藏夹」
// **只有一句**。
//
// 原来两组各顶一句**一模一样**的话，用户读到两句一样的话，得自己判断「这是说
// 一组还是两组都没有」（报告 C3）。
func TestTheSidebarSaysThereAreNoFavoritesOnlyOnce(t *testing.T) {
	_, tt := newShell(t, withSettings(app.DefaultSettings("")))
	tt.Frame()

	n := 0
	for _, s := range tt.Texts() {
		if s == "还没有收藏夹" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("侧栏上有 %d 句「还没有收藏夹」，want 1（两组各一句的话，用户得自己"+
			"判断那是说一组还是两组都没有），文字有 %q", n, tt.Texts())
	}
}

// TestTheEmptyFavoritesLineIsIndentedLikeItsGroup 断那一句**缩进对**。
//
// 它原来顶格在侧栏最左边（x=0），而分组标题在 x=43 / x=65（**两个还不一样**
// ——标题那一枚是 Grow(1) 的按钮，框架把按钮内容居中，于是「我创建的」那组
// 右边多一个「+」，它的标题就往左挪了 22 像素）。一句话顶格在最左边，看着像侧栏
// 底部的一句独立说明，而不属于任何一组（报告 C3）。
func TestTheEmptyFavoritesLineIsIndentedLikeItsGroup(t *testing.T) {
	_, tt := newShell(t, withSettings(app.DefaultSettings("")))
	tt.Frame()

	title, ok := tt.Find("▾ 我创建的")
	if !ok {
		title, ok = tt.Find("▸ 我创建的")
	}
	if !ok {
		t.Fatalf("侧栏上没有「我创建的」那一组，文字有 %q", tt.Texts())
	}
	line, ok := tt.Find("还没有收藏夹")
	if !ok {
		t.Fatalf("侧栏上没有「还没有收藏夹」那一句，文字有 %q", tt.Texts())
	}
	if line.X != title.X {
		t.Errorf("那一句起头在 x=%.1f，而分组标题在 x=%.1f——它不属于任何一组",
			line.X, title.X)
	}
}

// TestBothFavoriteGroupTitlesStartAtTheSamePlace 断两个分组标题**起头的位置
// 一样**。
//
// 原来「我创建的」在 x=43、「我收藏的」在 x=65：标题那一枚是 Grow(1) 的按钮，
// 而框架的按钮把内容**居中**，于是「我创建的」那组右边多一个「+」，它的标题
// 就往左挪了 22 像素。同一个侧栏里两个标题不在一条竖线上。
func TestBothFavoriteGroupTitlesStartAtTheSamePlace(t *testing.T) {
	_, tt := newShell(t, withSettings(app.DefaultSettings("")))
	tt.Frame()

	created, ok1 := tt.Find("▾ 我创建的")
	if !ok1 {
		created, ok1 = tt.Find("▸ 我创建的")
	}
	collected, ok2 := tt.Find("▾ 我收藏的")
	if !ok2 {
		collected, ok2 = tt.Find("▸ 我收藏的")
	}
	if !ok1 || !ok2 {
		t.Fatalf("侧栏上没有那两个分组标题，文字有 %q", tt.Texts())
	}
	if created.X != collected.X {
		t.Errorf("两个分组标题起头在 x=%.1f 和 x=%.1f——它们不在一条竖线上",
			created.X, collected.X)
	}
}

// TestTheCollapsedSidebarStillHasTheFavorites 断折叠态**还有**收藏夹的入口。
//
// 折叠态原来干脆不画收藏夹，而折叠侧栏是**给屏幕小的人用的**（900 宽的窗口、
// 13 寸屏）——恰恰是这类用户，收藏夹往往是他们最常去的地方。折叠一下，侧栏
// 就少了一整类入口，而界面上**没有任何提示说「收藏夹折叠后不见了」**，用户
// 会以为收藏夹没了（报告 C4）。
func TestTheCollapsedSidebarStillHasTheFavorites(t *testing.T) {
	s, tt := collapsedSidebar(t)

	if _, ok := tt.Find(favCollapsedName); !ok {
		t.Fatalf("折叠态的侧栏上没有「收藏夹」那一枚，文字有 %q", tt.Texts())
	}
	// 点开它：浮层里列出两组收藏夹。
	if err := tt.Click(favCollapsedName); err != nil {
		t.Fatalf("点「收藏夹」那一枚：%v", err)
	}
	tt.Frame()
	for _, want := range []string{"我创建的", "我收藏的"} {
		if !tt.HasText(want) {
			t.Errorf("浮层里没有 %q 那一组，文字有 %q", want, tt.Texts())
		}
	}
	// 点一个收藏夹就进去（那一层是入口，不是说明）。
	s.app.SetFavorites(app.FavoritesCreated, []app.Favorite{{ID: 7, Title: "歌单 甲"}})
	tt.Frame()
	if err := tt.Click("歌单 甲"); err != nil {
		t.Fatalf("点浮层里那一个收藏夹：%v（文字有 %q）", err, tt.Texts())
	}
	tt.Frame()
	if got := s.router.Path(); got != collectionPath(7) {
		t.Errorf("点完停在 %q，want %q", got, collectionPath(7))
	}
}
