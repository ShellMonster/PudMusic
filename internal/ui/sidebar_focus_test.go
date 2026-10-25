package ui

import (
	"image"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// 这一族钉的是票 #453 的第三条：**侧栏那一列键盘走得到，而且看得出焦点在哪儿**。
//
// 报告（BMG-246 的 `audit-side-settings.md` 的 (b) 一节「键盘那一路」）把这一条
// 列成**四个断口**的头一条：**侧栏的收藏夹列表键盘完全走不到**——
//
//   - 展开态：收藏夹那一行是 `ui.Column`，`Clicked()` 只设 `flagClickable`，
//     **不设 `flagFocusable`**（`mygo/ui/input.go:1042`），于是 Tab 跳过它。
//   - 折叠态：收藏夹只有那层浮层里看得见，而浮层里那一行同样不可聚焦——
//     **折叠侧栏之后收藏夹整类都走不到**。
//
// 修法是**一个出口**（`sidebar.go` 的 `sideFocusable`）：让这些行可聚焦，于是
// 框架给它们画焦点环——和侧栏菜单项、顶栏那几枚图标键**同一档**。

// TestTheSidebarFavoritesAreReachableByKeyboard 钉「展开态那一列键盘走得到」。
//
// 三件事：
//
//  1. **Tab 走得到**（走不到的话，键盘用户根本到不了收藏夹）；
//  2. **走得到就看得出**：那一行四周有一圈像素变了——框架给可聚焦元素画的
//     焦点环（`mygo/ui/paint.go:334`）；
//  3. **回车按得动**：框架给「有焦点而且可点」的元素补上那一下
//     （`mygo/ui/input.go:741`），于是回车进那个收藏夹。
func TestTheSidebarFavoritesAreReachableByKeyboard(t *testing.T) {
	s, _, tt, _ := favShell(t, 2, 1)
	tt.SetFocused(true)
	tt.Frame()
	before := snapshot(tt)

	// ① Tab 走得到那一行。
	//
	// ⚠️ **每按一次就问一次**：Tab 会绕圈，走过头就停在别处了。
	if !tabTo(t, tt, "我建的 1") {
		t.Fatalf("Tab 走不到侧栏的收藏夹行，文字有 %q", tt.Texts())
	}

	// ② 焦点环看得见：那一行四周有像素变了。
	after := snapshot(tt)
	row, ok := tt.Find("我建的 1")
	if !ok {
		t.Fatalf("侧栏上没有「我建的 1」那一行，文字有 %q", tt.Texts())
	}
	if n := pixelsAround(changedPixels(before, after), row); n == 0 {
		t.Errorf("焦点落在「我建的 1」那一行上，而那一行四周一个像素都没变——" +
			"键盘用户看不出焦点在哪儿")
	}

	// ③ 回车按得动：进那个收藏夹。
	tt.Key(0, mygoui.KeyEnter)
	tt.Frame()
	tt.Frame()
	want := collectionPathOf(app.Favorite{ID: 1, Type: 2})
	if got := s.router.Path(); got != want {
		t.Errorf("回车之后停在 %q，want %q（进那个收藏夹）", got, want)
	}
}

// TestTheCollapsedFavoritesPanelRowsAreReachableByKeyboard 钉「折叠态那一列
// 键盘走得到」。
//
// ⚠️ **折叠态的收藏夹只有那层浮层里看得见**：所以这里量的是**浮层里那一行**
// 可不可聚焦——折叠侧栏之后收藏夹整类都走不到的话，键盘用户就彻底用不了
// 收藏夹了。
func TestTheCollapsedFavoritesPanelRowsAreReachableByKeyboard(t *testing.T) {
	_, _, tt, _ := favShell(t, 2, 0)
	tt.SetFocused(true)
	tt.Frame()

	// 先把侧栏折起来（点那一枚折叠键）。
	if err := tt.Click("折叠侧栏"); err != nil {
		t.Fatalf("点不到折叠侧栏那一枚：%v", err)
	}
	tt.Frame()
	tt.Frame()

	// 再点开那枚收藏夹图标（折叠态它是一枚图标键）。
	if err := tt.Click("收藏夹"); err != nil {
		t.Fatalf("点不到折叠态的收藏夹图标：%v", err)
	}
	tt.Frame()
	if !tt.HasText("我建的 1") {
		t.Fatalf("浮层里没有那些收藏夹，文字有 %q", tt.Texts())
	}
	before := snapshot(tt)

	if !tabTo(t, tt, "我建的 1") {
		t.Fatalf("Tab 走不到浮层里的收藏夹行，文字有 %q", tt.Texts())
	}

	after := snapshot(tt)
	row, ok := tt.Find("我建的 1")
	if !ok {
		t.Fatalf("浮层里没有「我建的 1」那一行，文字有 %q", tt.Texts())
	}
	if n := pixelsAround(changedPixels(before, after), row); n == 0 {
		t.Errorf("焦点落在浮层里「我建的 1」那一行上，而那一行四周一个像素都没变——" +
			"键盘用户看不出焦点在哪儿")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 小工具
// ─────────────────────────────────────────────────────────────────────────────

// tabTo 一路按 Tab，直到焦点落在 label 那个元素上（或者按够了次数）。
//
// ⚠️ **每按一次就问一次**：Tab 会绕圈——先问再按的话，走过头就停在别处了，
// 而那一轮再按就回不来了。
func tabTo(t *testing.T, tt *mygoui.Tester, label string) bool {
	t.Helper()
	for range 40 {
		tt.Key(0, mygoui.KeyTab)
		tt.Frame()
		if tt.Focused(label) {
			return true
		}
	}
	return false
}

// pixelsAround 是那些落在 r 四周（上下左右各留 12 像素）里的像素有几个。
//
// ⚠️ **留 12 像素**：框架那一圈焦点环画在元素**外面** 3 像素
// （`mygo/ui/paint.go` 的 `FocusRing`），而 `Tester.Find` 给的是**文字**的盒子
// （不是整行的盒子）——所以环落在这一个字盒子的外头一点点的位置。
func pixelsAround(pts []image.Point, r mygoui.Rect) int {
	n := 0
	for _, p := range pts {
		if p.X >= int(r.X)-12 && p.X <= int(r.X+r.W)+12 &&
			p.Y >= int(r.Y)-12 && p.Y <= int(r.Y+r.H)+12 {
			n++
		}
	}
	return n
}
