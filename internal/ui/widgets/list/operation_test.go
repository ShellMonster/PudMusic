package list

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

// 这一组验行尾那枚「⋯」。
//
// 最要紧的是**它和右键菜单是同一份**：上游那两处（`music-list-item/index.tsx`
// 的右键和 `music-list-item/operation.tsx` 的「⋯」）传的是**同一个 `menus` 数组**，
// 我们这边走的是框架的菜单按钮和 `ContextMenu`，两条路挂的是**同一个函数**
// （`menu.go` 的 `buildSongMenu`）。写第二份菜单的话，两边迟早会对不上——而那种错
// 在界面上看不出来（两边都能点，只是点的不是同一件事）。
//
// 判据是**真的开一次菜单、列出它上面有哪些项**：菜单是这一趟全部建出来的，少建
// 一项那一项就永远点不到（`mygo/ui/context-menu.md`）。

// TestTheOperationButtonOpensTheSameMenuAsTheRightClick 钉住「两条路给的是同一
// 批动作」。
//
// ⚠️ **两个 Tester 各开一份**：菜单开着的时候再点别处，框架那边还记着上一份菜单
// 开着，量到的就可能是被关掉的那一份。
func TestTheOperationButtonOpensTheSameMenuAsTheRightClick(t *testing.T) {
	// 右键这一行。
	_, ttRight := newList(t, Actions{}, onlineSongs(3))
	if err := rightClickLabel(ttRight, "在线的歌 B"); err != nil {
		t.Fatal(err)
	}
	byRight := ttRight.Menu()
	if len(byRight) == 0 {
		t.Fatalf("右键没有开出菜单")
	}

	// 悬停这一行，点行尾那枚「⋯」（pud 行的操作键悬停才出现）。
	_, ttButton := newList(t, Actions{}, onlineSongs(3))
	hoverRowOf(t, ttButton, "在线的歌 B")
	if err := clickLabel(ttButton, rowMoreLabel("在线的歌 B")); err != nil {
		t.Fatalf("点不到行尾那枚「⋯」：%v", err)
	}
	byButton := ttButton.Menu()

	if !slices.Equal(byButton, byRight) {
		t.Errorf("「⋯」开出来的菜单是 %q，右键开出来的是 %q——两条路必须给同一批动作",
			byButton, byRight)
	}
}

// TestTheOperationButtonActsOnTheSongItSitsOn 钉住「⋯」做的是**它那一行**的事。
//
// 挂错了行的话，用户点第二行的「⋯」放出来的是第一首——而那比不弹还糟。
func TestTheOperationButtonActsOnTheSongItSitsOn(t *testing.T) {
	rec := &recorded{}
	_, tt := newList(t, rec.actions(), onlineSongs(5))

	hoverRowOf(t, tt, "在线的歌 C")
	if err := clickLabel(tt, rowMoreLabel("在线的歌 C")); err != nil {
		t.Fatalf("点不到行尾那枚「⋯」：%v", err)
	}
	if err := tt.ChooseMenuItem("从这一首往后听"); err != nil {
		t.Fatal(err)
	}
	if len(rec.played) != 1 {
		t.Fatalf("收到 %d 次播放，want 1 次", len(rec.played))
	}
	if got := bvidOf(rec.played[0]); !slices.Equal(got, []string{"BV3", "BV4", "BV5"}) {
		t.Errorf("换进队列的是 %v，want [BV3 BV4 BV5]（「⋯」那一行往后）", got)
	}
}

// rowMoreLabel 是 pud 行尾那枚「⋯」的名字（`pud.TrackRow` 的 `IconBtn`）。
func rowMoreLabel(title string) string { return "更多操作：" + title }

// hoverRowOf 把指针移到这一行上：pud 行的「⋯」、喜欢、移除这几枚平时隐藏，悬停才出现。
func hoverRowOf(t *testing.T, tt *ui.Tester, title string) {
	t.Helper()
	r, ok := tt.Find(title)
	if !ok {
		t.Fatalf("画面上找不到「%s」，文字有 %q", title, tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
}

// TestEveryRowHasAnOperationButton 钉住「**每一行**悬停时行尾都有那枚「⋯」」。
//
// 少一行就少一个入口：用户不会想到「那一行没有是因为它特殊」。
func TestEveryRowHasAnOperationButton(t *testing.T) {
	_, tt := newList(t, Actions{}, onlineSongs(4))

	for i := range 4 {
		title := "在线的歌 " + string(rune('A'+i))
		hoverRowOf(t, tt, title)
		if _, ok := tt.Find(rowMoreLabel(title)); !ok {
			t.Errorf("第 %d 行悬停时行尾没有那枚「⋯」，文字有 %q", i+1, tt.Texts())
		}
	}
}

// TestTheGridCellHasAnOperationButton 钉住网格那一格**也有**那枚「⋯」。
//
// 网格那一档是另一种摆法，但「一首歌能做的那几件事够不够得到」是同一件事——
// 触摸板用户没有右键，不知道有右键菜单的人只当这一格是只读的。
func TestTheGridCellHasAnOperationButton(t *testing.T) {
	g, tt := newGrid(t, Actions{}, onlineSongs(6), 1100, gridH)
	_ = g

	for i := range 3 {
		title := "在线的歌 " + string(rune('A'+i))
		if _, ok := tt.Find(OperationLabel(title)); !ok {
			t.Errorf("网格第 %d 格没有那枚「⋯」，文字有 %q", i+1, tt.Texts())
		}
	}
}

// TestTheGridOperationButtonOpensTheSameMenuAsTheRightClick 网格那一格的两条路
// 也要是同一批动作。
func TestTheGridOperationButtonOpensTheSameMenuAsTheRightClick(t *testing.T) {
	// 右键那一格。
	_, ttRight := newGrid(t, Actions{}, onlineSongs(6), 1100, gridH)
	if err := rightClickLabel(ttRight, "在线的歌 B"); err != nil {
		t.Fatal(err)
	}
	byRight := ttRight.Menu()
	if len(byRight) == 0 {
		t.Fatalf("右键没有开出菜单")
	}

	// 点那一格右上角那枚「⋯」。
	_, ttButton := newGrid(t, Actions{}, onlineSongs(6), 1100, gridH)
	if err := clickLabel(ttButton, OperationLabel("在线的歌 B")); err != nil {
		t.Fatalf("点不到格子上角那枚「⋯」：%v", err)
	}
	byButton := ttButton.Menu()

	if !slices.Equal(byButton, byRight) {
		t.Errorf("格子上角那枚「⋯」开出来的菜单是 %q，右键开出来的是 %q",
			byButton, byRight)
	}
}
