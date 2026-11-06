// 「喜欢歌单绑定」在界面这一层的测试。
//
// 「喜欢」是列表行上那颗心（`pud.TrackRow` 的 `ev.Like`），它走
// `list.Actions.Liked`——和右键菜单里的「收藏」（`FavSelect`，多选弹窗）
// 是**两条路**：喜欢进绑定歌单，收藏弹多选框。
//
// 宿主和假 B 站复用 foldertest_test.go 里那一台（folderBili + selectPageShell）。

package ui

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// likeRowHeart 悬停到那一行上，点那颗心。
func likeRowHeart(t *testing.T, tt *mygoui.Tester, title string) {
	t.Helper()
	r, ok := tt.Find(title)
	if !ok {
		t.Fatalf("画面上找不到「%s」，文字有 %q", title, tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if err := tt.Click("喜欢 " + title); err != nil {
		t.Fatalf("行尾没有那颗心：%v（文字 %q）", err, tt.Texts())
	}
}

// likedPageShell 造一个停在歌单页、挂着**真设置**的外壳。
//
// ⚠️ 绑定落盘在设置里，所以这一组必须走 `MountSettings`——不然「绑定之后
// 下次打开还在不在」测不到。
func likedPageShell(t *testing.T, f *folderBili) (*Shell, *mygoui.Tester) {
	t.Helper()
	return likedPageShellAt(t, f, 1200, 800)
}

// likedPageShellAt 同 `likedPageShell`，窗口尺寸可调——设置页那一行在
// 800 高的窗口里要滚下去才点得到。
func likedPageShellAt(t *testing.T, f *folderBili, w, h int) (*Shell, *mygoui.Tester) {
	t.Helper()
	f.lock()
	f.info["media_count"] = 1
	f.unlock()

	sh := newTestShell(t, at(w, h), mounted(func(t *testing.T, a *app.App) {
		if err := a.MountSettings(filepath.Join(t.TempDir(), app.SettingsFileName)); err != nil {
			t.Fatal(err)
		}
		cli := fixtureClient(t, f)
		// 挂一个登录态：「侧栏收藏夹拉过了没有」（`SelfMid` 判据）和
		// `RefreshFavorites` 都要它（同 refreshShell 那一段的 cookie 重装）。
		store := app.NewMemoryStore("test")
		if err := store.Save(context.Background(),
			app.Session{SESSDATA: "sess", DedeUserID: "7"}); err != nil {
			t.Fatal(err)
		}
		a.MountLogin(store, cli)
		if err := cli.SeedCookies([]*http.Cookie{
			{Name: "DedeUserID", Value: "7", Path: "/"},
		}); err != nil {
			t.Fatal(err)
		}
		a.MountCollections(app.CollectionRefs{Client: cli, Settle: 0})
	}))
	s, tt := sh.s, sh.tt
	s.app.SetWake(s.wake.invalidate)
	s.navigate("/collection/42")
	waitSongs(t, tt, s, 1)
	return s, tt
}

// TestFirstLikeAsksWhichFolderToBind 没绑定时点喜欢，弹出来的是「给喜欢
// 找一个专属歌单」那个三选一，**不是**收藏夹多选框。
//
// 🔴 钉的是两条路不要串：多选框的标题是「添加到收藏夹」，三选一的标题
// 是另一句——混掉的话「绑定」这个状态就永远建立不起来，每次喜欢都弹多选。
func TestFirstLikeAsksWhichFolderToBind(t *testing.T) {
	f := newFolderBili(t, nil)
	s, tt := likedPageShell(t, f)

	likeRowHeart(t, tt, "歌 一")
	settleFrame(tt)

	if s.likeBind == nil {
		t.Fatalf("喜欢弹窗没开（文字 %q）", tt.Texts())
	}
	if !tt.HasText("创建新的") || !tt.HasText("选择已有歌单") || !tt.HasText("暂时不用") {
		t.Errorf("三选一不全（文字 %q）", tt.Texts())
	}
	if tt.HasText("添加到收藏夹") {
		t.Error("弹出的是收藏夹多选框——喜欢走了收藏那条路")
	}
	// 一个写请求都不该发：还没选歌单呢。
	if ds := f.deals(); len(ds) != 0 {
		t.Errorf("还没绑就发了 %d 次 deal", len(ds))
	}
}

// TestLikeIntoBoundFolderWritesDirectly 绑定时点喜欢，**不弹窗**，直接
// 把这一首写进绑定的那一格，弹一句「已加入…」。
func TestLikeIntoBoundFolderWritesDirectly(t *testing.T) {
	f := newFolderBili(t, nil)
	s, tt := likedPageShell(t, f)
	s.app.SetFavorites(app.FavoritesCreated, []app.Favorite{{ID: 42, Title: "我的喜欢"}})
	s.app.BindLikedFolder(app.LikedFolder{ID: 42, Title: "我的喜欢"})

	likeRowHeart(t, tt, "歌 一")

	waitFrames(t, tt, "那一趟写出去", func() bool {
		return len(f.deals()) == 1
	})
	settleFrame(tt)

	ds := f.deals()
	if len(ds) != 1 || ds[0].get("add_media_ids") != "42" {
		t.Fatalf("deal = %v，want 一次、add 是 42", ds)
	}
	if s.likeBind != nil || s.folderSelect != nil {
		t.Error("绑定后喜欢不该再弹任何框")
	}
	if !tt.HasText("已加入「我的喜欢」") {
		t.Errorf("没有那句「已加入」（文字 %q）", tt.Texts())
	}
}

// TestChooseExistingFolderBindsAndAdds 「选择已有歌单」→ 勾一个 → 绑定
// 并把这一首放进去。
func TestChooseExistingFolderBindsAndAdds(t *testing.T) {
	f := newFolderBili(t, nil)
	f.pages = [][]map[string]any{folderPage(11, 2)}
	s, tt := likedPageShell(t, f)

	likeRowHeart(t, tt, "歌 一")
	settleFrame(tt)
	if err := tt.Click("选择已有歌单"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)

	waitFrames(t, tt, "收藏夹列表拉回来", func() bool {
		return s.likeBind != nil && len(s.likeBind.list) == 2
	})
	if err := tt.Click("收藏夹 11"); err != nil {
		t.Fatalf("列表里没有那一行：%v（文字 %q）", err, tt.Texts())
	}
	settleFrame(tt)
	if err := tt.Click("绑定并加入"); err != nil {
		t.Fatal(err)
	}

	waitFrames(t, tt, "写那一趟出去", func() bool {
		return len(f.deals()) == 1
	})
	settleFrame(tt)

	bound, ok := s.app.LikedFolder()
	if !ok || bound.ID != 11 {
		t.Fatalf("绑定 = %+v, %v，want id 11", bound, ok)
	}
	ds := f.deals()
	if len(ds) != 1 || ds[0].get("add_media_ids") != "11" {
		t.Fatalf("deal = %v，want add 11", ds)
	}
	if s.likeBind != nil {
		t.Error("绑定成功后弹窗该关了")
	}
}

// TestCreateNewFolderBindsAndAdds 「创建新的」→ 起名 + 选私密 → 建出来、
// 绑上、把这一首放进去。
func TestCreateNewFolderBindsAndAdds(t *testing.T) {
	f := newFolderBili(t, nil)
	s, tt := likedPageShell(t, f)

	likeRowHeart(t, tt, "歌 一")
	settleFrame(tt)
	if err := tt.Click("创建新的"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)

	if err := tt.Click("名称"); err != nil {
		t.Fatalf("找不到名称输入框：%v", err)
	}
	// 输入框默认带着「我的喜欢」：全选清掉再敲，不然名字变成两遍。
	tt.Key(mygoui.Cmd, mygoui.KeyA)
	tt.Key(mygoui.Modifiers(0), mygoui.KeyBackspace)
	tt.Type("我的喜欢")

	// 建收藏夹那一趟。
	if err := tt.Click("创建并加入"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "建完收歌那一趟写出去", func() bool {
		return len(f.deals()) == 1
	})
	settleFrame(tt)

	ws := f.writes()
	if len(ws) != 1 || ws[0].get("title") != "我的喜欢" {
		t.Fatalf("add 表单 = %v，want title 我的喜欢", ws)
	}
	if ws[0].get("privacy") != "1" {
		t.Errorf("privacy = %q，want 1（默认私密）", ws[0].get("privacy"))
	}
	bound, ok := s.app.LikedFolder()
	if !ok || bound.ID != 999 {
		t.Fatalf("绑定 = %+v, %v，want id 999（接口回的新 id）", bound, ok)
	}
	ds := f.deals()
	if len(ds) != 1 || ds[0].get("add_media_ids") != "999" {
		t.Fatalf("deal = %v，want add 999", ds)
	}
}

// TestSkipBindingFallsBackToFolderSelect 「暂时不用」→ 回到原来那个
// 「放进哪些收藏夹」的多选框，**不**建立绑定。
func TestSkipBindingFallsBackToFolderSelect(t *testing.T) {
	f := newFolderBili(t, nil)
	f.pages = [][]map[string]any{folderPage(1, 3)}
	s, tt := likedPageShell(t, f)

	likeRowHeart(t, tt, "歌 一")
	settleFrame(tt)
	if err := tt.Click("暂时不用"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)

	if s.likeBind != nil {
		t.Fatal("三选一弹窗该关了")
	}
	if s.folderSelect == nil {
		t.Fatalf("「暂时不用」之后该开收藏夹多选框（文字 %q）", tt.Texts())
	}
	if _, ok := s.app.LikedFolder(); ok {
		t.Error("「暂时不用」不该建立绑定")
	}
}

// TestGoneBoundFolderFallsBackToSelect 绑定的歌单已经没了 → 解绑 + 回到
// 多选框，不能让喜欢变成一个永远失败的按钮。
func TestGoneBoundFolderFallsBackToSelect(t *testing.T) {
	f := newFolderBili(t, nil)
	// 侧栏那份列表里**没有** 42：created/list 只给 1~3 号。要让 app 层
	// 第一层判据生效（列表拉过了、里面没有），得真拉一遍——`SetFavorites`
	// 塞进去的条目不标 Loaded。
	f.pages = [][]map[string]any{folderPage(1, 3)}
	s, tt := likedPageShell(t, f)
	s.app.BindLikedFolder(app.LikedFolder{ID: 42, Title: "没了的那个"})
	s.app.RefreshFavorites()
	waitFrames(t, tt, "侧栏收藏夹拉完", func() bool {
		return s.app.FavoriteState(app.FavoritesCreated).Loaded
	})

	likeRowHeart(t, tt, "歌 一")

	// 失效这一趟不走网络（侧栏列表拉过了），结果从 `TakeLikedResult` 回来；
	// 而 `flushLikedResults` 每帧都在取走它——所以这里等的是**多选框开出来**
	// （失效 → 解绑 → 回多选框），不是那条结果本身。
	waitFrames(t, tt, "回到多选框", func() bool {
		return s.folderSelect != nil
	})
	settleFrame(tt)

	if _, ok := s.app.LikedFolder(); ok {
		t.Error("绑定失效后该解绑")
	}

}

// TestSettingsShowsAndChangesTheBinding 设置页「菜单」Tab 能看绑定、能
// 更换、能解绑。
func TestSettingsShowsAndChangesTheBinding(t *testing.T) {
	f := newFolderBili(t, nil)
	f.pages = [][]map[string]any{folderPage(11, 2)}
	s, tt := likedPageShellAt(t, f, 1200, 1300)
	s.app.BindLikedFolder(app.LikedFolder{ID: 42, Title: "我的喜欢", Private: true})

	s.goToSettingsTab(settingsTabMenu)
	settleFrame(tt)

	if !tt.HasText("喜欢歌单") {
		t.Fatalf("设置里没有「喜欢歌单」那一行（文字 %q）", tt.Texts())
	}
	if !tt.HasText("我的喜欢") {
		t.Errorf("没显示绑定的名字（文字 %q）", tt.Texts())
	}

	// 更换 → 拉列表 → 勾一个 → 绑定换掉。
	if err := tt.Click("更换"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "收藏夹列表拉回来", func() bool {
		return s.likeBind != nil && len(s.likeBind.list) == 2
	})
	if err := tt.Click("收藏夹 12"); err != nil {
		t.Fatal(err)
	}
	settleFrame(tt)
	if err := tt.Click("绑定"); err != nil {
		t.Fatalf("没有「绑定」按钮（文字 %q）", tt.Texts())
	}
	settleFrame(tt)

	bound, _ := s.app.LikedFolder()
	if bound.ID != 12 {
		t.Fatalf("更换后绑定 = %+v，want id 12", bound)
	}
	// 从设置里换绑定**不**收任何一首歌——那一下只是改设置。
	if ds := f.deals(); len(ds) != 0 {
		t.Errorf("设置里换绑定不该发 deal（发了 %d 次）", len(ds))
	}
}
