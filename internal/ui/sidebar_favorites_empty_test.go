package ui

// 这一组盯的是**侧栏里那两组收藏夹**：
//
//   - 空状态**只有一句**，而且**缩进对**（报告 C3）；
//   - 侧栏折叠起来的时候，收藏夹**不是整组消失**（报告 C4）；
//   - 空状态那一句**说得清是哪种空**：「还没拉过」「拉失败了」「真的一 个
//     都没有」在画面上不能长得一样（issue #362）。
//
// 出处是 UX 审计报告（`~/.herdr-projects/reports/Biu_MyGo/125/report.md`，
// 只查不改）。

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
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

// favEmptyShell 造一个「收藏夹**已经拉过**、而且真的一个都没有」的外壳。
//
// ⚠️ **要走真的那一趟**：空状态那句话是**拉过之后**才说得准的
// （「还没有收藏夹」这一档的前提是「B 站那边确实没有」），所以这里让假 B 站
// 回一个空列表，而不是直接往内存里塞一个空列表——塞进去的话，那一句会
// 变成「还没拉收藏夹」，而这一组要验的是「拉过了、真的没有」那一句。
func favEmptyShell(t *testing.T) (*Shell, *ui.Tester) {
	t.Helper()
	f := newFolderBili(t, nil)
	f.pages = [][]map[string]any{{}}
	s, tt := refreshShell(t, f, "7")
	s.app.RefreshFavorites()
	// ⚠️ **两组都要等到**：空状态那句话是按**两组**的状态算出来的（见
	// `favEmptyText`），而刷新是拉两个接口。只等「我创建的」那一组的话，
	// 有一帧是「一组拉到了、另一组还在飞」——那句话在那一帧里是
	// 「还没拉收藏夹」，于是这一条会飘。
	waitFrames(t, tt, "收藏夹拉完了（而且真的一个都没有）", func() bool {
		return s.app.FavoriteState(app.FavoritesCreated).Loaded &&
			s.app.FavoriteState(app.FavoritesCollected).Loaded
	})
	tt.Frame()
	return s, tt
}

// TestTheSidebarSaysThereAreNoFavoritesOnlyOnce 断侧栏里那一句「还没有收藏夹」
// **只有一句**。
//
// 原来两组各顶一句**一模一样**的话，用户读到两句一样的话，得自己判断「这是说
// 一组还是两组都没有」（报告 C3）。
func TestTheSidebarSaysThereAreNoFavoritesOnlyOnce(t *testing.T) {
	_, tt := favEmptyShell(t)

	n := 0
	for _, s := range tt.Texts() {
		if s == "还没有收藏夹。" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("侧栏上有 %d 句「还没有收藏夹」，want 1（两组各一句的话，用户得自己"+
			"判断那是说一组还是两组都没有），文字有 %q", n, tt.Texts())
	}
}

// TestTheSidebarDoesNotSayThereAreNoFavoritesBeforeAnythingWasFetched 断
// **还没拉过**的时候不说「还没有收藏夹」。
//
// ⚠️ **这是这一票最重要的一条**：那句话说的是「B 站那边确实没有」。还没去问
// 的时候说它，是在替 B 站回答一个我们没问过的问题——而用户读到的是
// 「我没有收藏夹」。
func TestTheSidebarDoesNotSayThereAreNoFavoritesBeforeAnythingWasFetched(t *testing.T) {
	_, tt := newShell(t, withSettings(app.DefaultSettings("")))
	tt.Frame()

	if tt.HasText("还没有收藏夹") {
		t.Errorf("还没拉就说「还没有收藏夹」，文字有 %q", tt.Texts())
	}
	// 没登录时该说清楚「登录之后才有」——那是用户能做的事。
	if !tt.HasText("登录后能看到收藏夹") {
		t.Errorf("没登录时侧栏上没有说「登录后能看到收藏夹」，文字有 %q", tt.Texts())
	}
}

// TestTheSidebarSaysTheFetchFailedInsteadOfThereAreNoFavorites 断**拉失败**
// 在画面上不是一句「还没有收藏夹」。
//
// ⚠️ **这是这一票的第二重要的一条**：拉失败和「真的没有」在画面上必须
// 分开——摆一句「还没有收藏夹」在拉失败的时候是撒谎，而用户会据此以为
// 自己一个收藏夹都没有。
func TestTheSidebarSaysTheFetchFailedInsteadOfThereAreNoFavorites(t *testing.T) {
	f := newFolderBili(t, nil)
	f.failTheList()

	s, tt := refreshShell(t, f, "7")
	tt.Frame()

	s.app.RefreshFavorites()
	waitFrames(t, tt, "拉失败被记下来了", func() bool {
		return s.app.FavoriteState(app.FavoritesCreated).Err != ""
	})
	tt.Frame()

	if tt.HasText("还没有收藏夹") {
		t.Errorf("拉失败了还说「还没有收藏夹」，文字有 %q", tt.Texts())
	}
	// ⚠️ **必须说得出「可以重试」**：那是用户知道自己该怎么办的唯一线索。
	if !tt.HasText("拉不到收藏夹，可以重试") {
		t.Errorf("拉失败时侧栏上没有说「拉不到收藏夹，可以重试」，文字有 %q", tt.Texts())
	}
}

// TestRefreshingWithoutALoginDoesNotSayTheFetchFailed 断「没登录」**不是**
// 「拉失败」。
//
// ⚠️ 用户该做的是**登录**，不是重试——而侧栏上摆一句「拉不到收藏夹，可以
// 重试」的话，他按了重试还是进不去。
func TestRefreshingWithoutALoginDoesNotSayTheFetchFailed(t *testing.T) {
	f := newFolderBili(t, nil)
	s, tt := refreshShell(t, f, "") // mid 是空串 = 没登录
	tt.Frame()

	s.app.RefreshFavorites()
	waitFrames(t, tt, "说清楚还没登录", func() bool {
		return tt.HasText("还没登录，登录之后才能刷新收藏夹")
	})
	tt.Frame()

	if tt.HasText("拉不到收藏夹，可以重试") {
		t.Errorf("没登录却说「拉不到收藏夹，可以重试」，文字有 %q", tt.Texts())
	}
	if !tt.HasText("登录后能看到收藏夹") {
		t.Errorf("没登录时侧栏上没有说「登录后能看到收藏夹」，文字有 %q", tt.Texts())
	}
}
