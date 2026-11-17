// 列表行上的「喜欢」：行本身不再画心，状态和动作都搬到右键菜单。
//
// 断言的是用户能观察到的行为：悬停预取、菜单写「喜欢」还是「取消喜欢」、
// 选中之后发出的是 add 还是 del。
package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// hoverListRow 把指针移到那一行的文字上，跑一帧让悬停生效。
func hoverListRow(t *testing.T, tt *mygoui.Tester, title string) {
	t.Helper()
	r, ok := tt.Find(title)
	if !ok {
		t.Fatalf("画面上找不到「%s」，文字有 %q", title, tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
}

// TestTheListRowHasNoHeart 行尾不再有心：悬停之后也找不到「喜欢 歌 一」。
func TestTheListRowHasNoHeart(t *testing.T) {
	f := newFolderBili(t, nil)
	_, tt := likedPageShell(t, f)

	hoverListRow(t, tt, "歌 一")
	if _, ok := tt.Find("喜欢 歌 一"); ok {
		t.Error("列表行上还画着那颗心：喜欢已经搬到右键菜单")
	}
}

// TestHoveringARowPrefetchesTheFavoriteState 悬停时把这一条在绑定歌单里的
// 状态问出来；打开菜单时读缓存，所以已经喜欢的那一行写「取消喜欢」。
func TestHoveringARowPrefetchesTheFavoriteState(t *testing.T) {
	f := newFolderBili(t, nil)
	f.pages = [][]map[string]any{{
		{"id": 42, "title": "我的喜欢", "attr": 2, "media_count": 1, "fav_state": 1},
	}}
	s, tt := likedPageShell(t, f)
	s.app.BindLikedFolder(app.LikedFolder{ID: 42, Title: "我的喜欢"})

	hoverListRow(t, tt, "歌 一")
	waitFrames(t, tt, "悬停预取了喜欢状态", func() bool { return f.arrivals() >= 1 })

	if err := tt.RightClick("歌 一"); err != nil {
		t.Fatalf("右键那一行：%v", err)
	}
	if err := tt.ChooseMenuItem("取消喜欢"); err != nil {
		t.Fatalf("菜单里没有「取消喜欢」：%v（菜单状态没读到缓存的喜欢）", err)
	}

	waitFrames(t, tt, "从绑定歌单移除那一趟", func() bool { return len(f.deals()) == 1 })
	ds := f.deals()
	if len(ds) != 1 || ds[0].get("del_media_ids") != "42" {
		t.Fatalf("deal = %v，want del 42（绑定的那一格）", ds)
	}
}

// TestTheListMenuAddsToTheBoundFolder 状态还不知道 / 不在里面时写「喜欢」，
// 选中之后把这一首加进绑定的那一格。
func TestTheListMenuAddsToTheBoundFolder(t *testing.T) {
	f := newFolderBili(t, nil) // created/list 是空的：绑定的 42 不在里面
	s, tt := likedPageShell(t, f)
	s.app.BindLikedFolder(app.LikedFolder{ID: 42, Title: "我的喜欢"})

	hoverListRow(t, tt, "歌 一")
	waitFrames(t, tt, "悬停预取了喜欢状态", func() bool { return f.arrivals() >= 1 })

	if err := tt.RightClick("歌 一"); err != nil {
		t.Fatalf("右键那一行：%v", err)
	}
	if err := tt.ChooseMenuItem("喜欢"); err != nil {
		t.Fatalf("菜单里没有「喜欢」：%v", err)
	}

	waitFrames(t, tt, "加进绑定歌单那一趟", func() bool { return len(f.deals()) == 1 })
	ds := f.deals()
	if len(ds) != 1 || ds[0].get("add_media_ids") != "42" {
		t.Fatalf("deal = %v，want add 42（绑定的那一格）", ds)
	}
}
