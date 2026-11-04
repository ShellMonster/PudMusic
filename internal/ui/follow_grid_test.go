// 「我的关注」那一页卡片网格的**间距**和**列数**（原型 `.od-grid` 两列、缝 16）。
//
// 这一页的其余用例在 `follow_test.go`（那里盯的是「哪句话跟哪句话不能混」）；这一份盯的是
// **画面上量的那些数**：行与行之间 16、列与列之间 16、任何窗口宽都是两列（`--od-cols:2`
// 是行内样式，不跟着媒体查询变）。
package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// TestFollowColumnsAreAlwaysTwo 钉住关注页的列数：原型 `.od-grid style="--od-cols:2"`，
// 不分窗口宽都是两列。
func TestFollowColumnsAreAlwaysTwo(t *testing.T) {
	for _, w := range []float32{0, 300, 767, 768, 1023, 1200, 1400, 1900} {
		if got := followColumnsFor(w); got != 2 {
			t.Errorf("容器宽 %v 排 %d 列，want 2", w, got)
		}
	}
}

// TestTheFollowGridHasSixteenBetweenRowsAndColumns 钉住关注页网格的两个缝：卡与卡之间都是 16。
//
// 行间距 = 下一行那张卡的上边 - 上一行那张卡的下边；列间距 = 右边那张卡的左边 - 左边那张卡的右边。
// 卡的盒子取 `UCard` 那枚按钮的无障碍盒子（名字是「打开 … 的空间」）。
func TestTheFollowGridHasSixteenBetweenRowsAndColumns(t *testing.T) {
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	uiFollowList(b, 4, []any{
		uiFollowUser(11, "第一个 UP"), uiFollowUser(12, "第二个 UP"),
		uiFollowUser(13, "第三个 UP"), uiFollowUser(14, "第四个 UP"),
	})
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 0, []any{})

	s, tt, _ := newSpaceShell(t, b, true, "77", at(1400, 900))
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "人到了", func() bool { return len(f.users) == 4 && f.tagStarted })
	waitFrames(t, tt, "列数稳了", func() bool { return f.cols == 2 })
	tt.Frame()

	box := func(u app.FollowUser) mygoui.Rect {
		t.Helper()
		r, ok := tt.Find("打开 " + u.Name + " 的空间")
		if !ok {
			t.Fatalf("「%s」那张卡没画出来，文字有 %q", u.Name, tt.Texts())
		}
		return r
	}
	// ① 行与行之间 16。
	above, below := box(f.users[0]), box(f.users[2])
	if d := below.Y - (above.Y + above.H); absF(d-16) > 0.5 {
		t.Errorf("两行之间的缝是 %v，该是 16（上面 %.2f~%.2f，下面 %.2f~%.2f）",
			d, above.Y, above.Y+above.H, below.Y, below.Y+below.H)
	}
	// ② 列与列之间 16。
	left, right := box(f.users[0]), box(f.users[1])
	if d := right.X - (left.X + left.W); absF(d-16) > 0.5 {
		t.Errorf("两列之间的缝是 %v，该是 16（左边 %.2f~%.2f，右边 %.2f~%.2f）",
			d, left.X, left.X+left.W, right.X, right.X+right.W)
	}
}
