package ui

// 「收藏」在列表右键菜单里那几页（A11）。
//
// 这一组盯的是**接线换没换**：`FavSelect` 是 nil 的那一份操作里，「收藏」按下去
// 只会得到一句「「收藏」还没接 B 站接口」——而接口早就在了
// （`app.FavoriteSong` / `app.DealFolderMedias`），那句话是假的，用户读到它会
// 以为这个应用还没做完。
//
// 所以这里断言的是**弹窗真的开出来了**（`folderselect.go` 那个），而不是
// 「没有说没接」。

import (
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/ui"
)

// favoriteOnAListPage 造一个歌单那一摊挂着的外壳，然后进 path 那一页。
//
// ⚠️ **必须挂歌单**：`openFolderSelect` 没挂时会说「收藏夹还没接上」就返回
// （那是对的），而那样验的就不是「弹窗开没开」而是「挂没挂」。
func favoriteOnAListPage(t *testing.T, f *libFake, path string) (*Shell, *ui.Tester) {
	t.Helper()
	tt, s := libraryShell(t, f, path)

	b := newCollectionBili(t)
	s.app.MountCollections(app.CollectionRefs{Client: collectionClient(t, b), Settle: 0})
	tt.Frame()
	return s, tt
}

// TestFavoritingFromTheLaterListOpensTheDialog 稍后再看那一页。
func TestFavoritingFromTheLaterListOpensTheDialog(t *testing.T) {
	f := newLibFake(t).withLater(1, 1, "周杰伦的歌")
	_, tt := favoriteOnAListPage(t, f, laterPath)
	// ⚠️ **等的是「行画出来了」，不是「请求发出去了」**。两者之间隔着响应 →
	// 解码 → 投递状态 → 唤醒 → 画一帧，机器一忙就赶不上——而下一步
	// `RightClick` 要的正是那一行。原来的判据是 `callsOf(...) == 1`（请求记上了），
	// 它比这一步需要的东西早，于是整包并行跑时偶发地红（症状是
	// `ui: no element shows "…"`）。判据一律是结果，见基座那两条等法（shelltest_test.go）。
	waitFrames(t, tt, "第一页的条目画出来", func() bool { return tt.HasText("周杰伦的歌") })

	if err := tt.RightClick("周杰伦的歌"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("收藏"); err != nil {
		t.Fatal(err)
	}
	// ⚠️ **要等**：这些页面的条目只有 bvid，而弹窗要 avid，所以按下去之后
	// 还要在后台问一次 B 站才开（`app.ResolveFavoriteID`）。只跑一帧的话读到的
	// 是「还没开」。
	waitFrames(t, tt, "收藏夹选择弹窗开出来", func() bool { return tt.HasText("放到收藏夹") })

	if tt.HasText("还没接 B 站接口") {
		t.Errorf("按了「收藏」说的是「还没接」，文字有 %q", tt.Texts())
	}
}

// TestFavoritingFromTheHistoryListOpensTheDialog 历史记录那一页。
//
// 两页共用同一份操作接线（`buildLibraryPages`），而**只有走 `s.listActions()`
// 那一份才有 FavSelect**——所以这一条和上面那一条是同一个 bug 的两个出口，
// 补一个漏一个都会红。
func TestFavoritingFromTheHistoryListOpensTheDialog(t *testing.T) {
	f := newLibFake(t).withHistory(1, 10, 100, "听过的一首")
	_, tt := favoriteOnAListPage(t, f, historyPath)
	// ⚠️ **等的是「行画出来了」，不是「请求发出去了」**。两者之间隔着响应 →
	// 解码 → 投递状态 → 唤醒 → 画一帧，机器一忙就赶不上——而下一步
	// `RightClick` 要的正是那一行。原来的判据是 `callsOf(...) == 1`（请求记上了），
	// 它比这一步需要的东西早，于是整包并行跑时偶发地红（症状是
	// `ui: no element shows "…"`）。判据一律是结果，见基座那两条等法（shelltest_test.go）。
	waitFrames(t, tt, "第一页的条目画出来", func() bool { return tt.HasText("听过的一首") })

	if err := tt.RightClick("听过的一首"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("收藏"); err != nil {
		t.Fatal(err)
	}
	// 要等：这些页面的条目只有 bvid，开弹窗前要在后台问一次稿件 id
	// （见上面那一条的注释）。
	waitFrames(t, tt, "收藏夹选择弹窗开出来", func() bool { return tt.HasText("放到收藏夹") })
}

// TestFavoritingFromTheSearchListOpensTheDialog 搜索结果那一页。
//
// ⚠️ 它和上面两条是**同一个 bug 的三个出口**：三页原来各自建了一份
// `list.AppActions(...)`（`FavSelect` 是 nil），而接口早就接好了。
func TestFavoritingFromTheSearchListOpensTheDialog(t *testing.T) {
	st := newSearchStub(5, 5)
	s, tt := newSearchPage(t, "周杰伦", st)

	// 歌单那一摊得挂上（`openFolderSelect` 没挂时只会说「收藏夹还没接上」）。
	b := newCollectionBili(t)
	s.app.MountCollections(app.CollectionRefs{Client: collectionClient(t, b), Settle: 0})

	// 同样等「行画出来了」而不是「这一页 Loaded 了」——理由见上面那一条。
	waitFrames(t, tt, "结果的行画出来", func() bool { return tt.HasText("第 1 页 第 1 首") })

	if err := tt.RightClick("第 1 页 第 1 首"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("收藏"); err != nil {
		t.Fatal(err)
	}
	// 要等：搜索结果的条目只有 bvid（`bili.VideoHit` 不给 avid），开弹窗前
	// 要在后台问一次稿件 id。
	waitFrames(t, tt, "收藏夹选择弹窗开出来", func() bool { return tt.HasText("放到收藏夹") })
	if tt.HasText("还没接 B 站接口") {
		t.Errorf("按了「收藏」说的是「还没接」，文字有 %q", tt.Texts())
	}
}

// TestFavoritingAUserOnTheSearchPageSaysWhatIsWrong 用户那一 Tab 的「收藏」
// **不是**「还没接 B 站接口」——UP 主不是歌。
func TestFavoritingAUserOnTheSearchPageSaysWhatIsWrong(t *testing.T) {
	s, _ := newShell(t)

	acts := s.searchUserActions()
	if acts.FavSelect == nil || acts.Download == nil {
		t.Fatal("用户那一 Tab 的收藏 / 下载是 nil，按下去会是「还没接 B 站接口」")
	}

	acts.FavSelect(app.Item{Type: searchUserItemType, Bvid: "42"})

	// 认的是**这一句真的提示**：nil 的那一份说的是「还没接 B 站接口」，而接口
	// 早就在了——那句话是假的，用户读到它会以为这个应用还没做完。
	toasts := lastToast(s)
	if !strings.Contains(toasts, "UP 主不是歌") {
		t.Errorf("说的是 %q，没有说清这一项不是歌", toasts)
	}
}

// lastToast 是最近那一句提示。
func lastToast(s *Shell) string {
	s.toastMu.Lock()
	defer s.toastMu.Unlock()
	if len(s.toasts) == 0 {
		return ""
	}
	return s.toasts[len(s.toasts)-1].text
}

// TestFavoritingAnItemWithoutASourceSaysSo 一条 **Source 不是 online** 的项按
// 「收藏」：**说一句就停**，不许递归。
//
// 🔴 这一条盯的是一条**真的会 fatal** 的路：Go 的栈溢出是 `fatal error`，**不可
// recover**，整个进程死掉。
//
// 早一版是这样绕回来的（回调里调 `openFolderSelect`，守卫只判 Bvid / IsLocal，
// 而 `favTarget` 真正卡人的是 `Source == "online"`）：
//
//	openFolderSelect → favTarget 失败 → askFavoriteIDThen → 问出 Sid 填上
//	  → openFolderSelect → favTarget 还是失败（Source 没变）
//	  → askFavoriteIDThen → 这一趟 Sid 已经有了，ResolveFavoriteID 同步返回
//	    → openFolderSelect → ……
//
// 零网络开销的紧递归，几百毫秒把 goroutine 栈撑到 1 GB。今天所有 Item 都填了
// Source 所以够不着——那是运气不是设计。现在回调里走的是 `openFolderSelectNow`
// （它没有兜底那一支），结构上就绕不回来。
func TestFavoritingAnItemWithoutASourceSaysSo(t *testing.T) {
	s, _ := newShell(t)
	b := newCollectionBili(t)
	s.app.MountCollections(app.CollectionRefs{Client: collectionClient(t, b), Settle: 0})

	// ⚠️ Source 是空串、Bvid 非空、Sid 为空：早一版从这一条进去会绕死。
	s.openFolderSelect(app.Item{Bvid: "BV1play", Title: "没有来源的那首"})

	if s.folderSelect != nil {
		t.Fatalf("认不出身份却把弹窗开出来了（rid=%d）", s.folderSelect.rid)
	}
	if got := lastToast(s); got == "" {
		t.Fatal("什么也没说")
	}
	// **一个请求都不该发**：这类项问不出什么来（Source 就不对，而
	// ResolveFavoriteID 补的是 Sid，补完 `favTarget` 还是不认）。
	if n := b.hitsOf("/x/web-interface/view"); n != 0 {
		t.Errorf("为了这一条问了 B 站 %d 次稿件 id", n)
	}
}
