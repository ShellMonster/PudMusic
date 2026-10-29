// 「我的关注」那一页那个网格的**间距**。
//
// 这一页的其余用例在 `follow_test.go`（那里盯的是「哪句话跟哪句话不能混」）；
// 这一份盯的是**画面上量的那些数**：行与行之间 16、列与列之间 16。
//
// # 根因
//
// 每一行是**一个独立的 `ui.Grid`**（`follow.go` 的 `viewFollowRow`），而
// `Gap(list.GridGap)` **只在这一行内部生效**——也就是**列与列之间**。行与行之间
// 是**列表的两行**，原来一点缝都没有，卡片背靠背（用户 2026-10-25 的截图）。
//
// 上游那一个 `ROW_GAP = 16` 同时当地面上两个角色：**行间距**（`estimateSize` 加
// `height: size - ROW_GAP`）和**列间距**（`gap: ${ROW_GAP}px`）。我们原来只做对
// 了后者。
//
// # 最后一行底下没有多那 16
//
// 框架的列表摆行的时候是「下一行的顶 = 这一行的底 + 缝」，而**内容的高度**是
// `所有行高 + (行数-1)*缝`——末尾那一份它自己减掉了。所以最后一行底下**不多**
// 那 16（上游是画的时候把 `size - ROW_GAP` 减掉，同一个意思）。
//
// 这一条**不是量的**，是框架那一行的算法（`mygo/ui/list.go` 的 `layoutList`）；
// 这里写下来是为了说清楚它。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/app"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	mygoui "github.com/egoist/mygo/ui"
)

// TestTheFollowGridHasSixteenBetweenRowsAndColumns 钉住关注页那个网格的两个缝。
//
// # 量的就是「卡与卡之间」的那一段
//
// 行间距 = 下一行那张卡的**上边** - 上一行那张卡的**下边**；
// 列间距 = 右边那张卡的**左边** - 左边那张卡的**右边**。
//
// # 卡自己的盒子怎么算出来的
//
// 卡本身**查不到**（无头 Tester 只能按文字或标签找元素，而卡是一个没有文字的
// 列），但卡里几样东西查得到，而它们的摆法是死的（理由见 `followCardBox`）。
//
// # 为什么列数要等它稳
//
// 列数是按**上一帧**量出来的容器宽算的（`followPage.width` →
// `list.GridColumnsFor`），头一帧按一个还没量准的宽排成 5 列，下一帧量到真的
// 才排成 3 列（理由同 `follow.go` 里 `keepFollowAnchor` 那段）。不等它稳的话，
// 量到的是**上一档列数**摆出来的画面。
func TestTheFollowGridHasSixteenBetweenRowsAndColumns(t *testing.T) {
	b := newUIExplore(t)
	uiFollowTags(b, map[int]any{0: "默认分组"})
	// 六个 UP 主：窗口摆得下三列，于是**两行**——行间距量两次，列间距量两次。
	uiFollowList(b, 6, []any{
		uiFollowUser(11, "第一个 UP"), uiFollowUser(12, "第二个 UP"),
		uiFollowUser(13, "第三个 UP"), uiFollowUser(14, "第四个 UP"),
		uiFollowUser(15, "第五个 UP"), uiFollowUser(16, "第六个 UP"),
	})
	uiFollowSearch(b, "", nil)
	uiFollowTag(b, 0, []any{})

	s, tt, _ := newSpaceShell(t, b, true, "77", at(1400, 900))
	s.navigate(followPath)
	tt.Frame()
	f := s.followPageOf()
	waitFrames(t, tt, "人到了", func() bool { return len(f.users) == 6 && f.tagStarted })
	waitFrames(t, tt, "列数稳了", func() bool { return f.cols == list.GridColumnsFor(f.width) })
	tt.Frame()

	if f.cols != 3 {
		t.Fatalf("网格宽 %v 应该是 3 列，实际是 %d 列", f.width, f.cols)
	}
	if len(f.users) != 6 {
		t.Fatalf("要六个人才量得出两行，实际是 %d 个", len(f.users))
	}
	// 一列有多宽：网格自己算的那一份（`list.CellWidth` 和 `ui.Grid` 是同一个
	// 算法，所以算出来的数正好是这一格实际拿到的宽）。
	cardW := list.CellWidth(f.width, f.cols)

	// ① 行与行之间 16。
	for row := 1; row < len(f.users)/f.cols; row++ {
		above := followCardBox(t, tt, f.users[(row-1)*f.cols], cardW)
		below := followCardBox(t, tt, f.users[row*f.cols], cardW)
		if d := below.Y - (above.Y + above.H); absF(d-16) > 0.5 {
			t.Errorf("第 %d 行和第 %d 行之间的缝是 %v，该是 16（上面那行 %.2f~%.2f，下面那行 %.2f~%.2f）",
				row-1, row, d, above.Y, above.Y+above.H, below.Y, below.Y+below.H)
		}
	}

	// ② 列与列之间还是 16（没被动）。
	for col := 1; col < f.cols; col++ {
		left := followCardBox(t, tt, f.users[col-1], cardW)
		right := followCardBox(t, tt, f.users[col], cardW)
		if d := right.X - (left.X + left.W); absF(d-16) > 0.5 {
			t.Errorf("第 %d 列和第 %d 列之间的缝是 %v，该是 16（左边那张 %.2f~%.2f，右边那张 %.2f~%.2f）",
				col-1, col, d, left.X, left.X+left.W, right.X, right.X+right.W)
		}
	}

	// ③ 卡里那圈留白还是 16。
	//
	// ⚠️ **这一条盯的是「缝是从哪儿来的」**：这一票的 16 是**行与行之间**的，
	// 不是把卡里的留白加大凑出来的——留白加大了的话，卡和页面底之间也一起撑开
	// 了，而那不是行间距（票面第 2 条）。
	//
	// 顺带一提：① 量出来的行缝正好是 16 这件事本身也钉住了它——留白要是被加大
	// 过，行缝就会量成 0。
	if followCardPad != 16 {
		t.Errorf("卡里那圈留白是 %v，该是 16——这一票的缝是行间距，不是卡片内边距", followCardPad)
	}
}

// followCardBox 是「我的关注」那一张卡的盒子，**从卡里能查到的东西**算出来的。
//
// # 为什么这么算
//
// 卡本身查不到（见上），但卡里几样东西查得到，而它们的摆法是死的：
//
//   - **头像是卡里最上面那一样**（上面是卡里那圈留白 `followCardPad`）；
//   - **签名是卡里最后一样**（下面也是那圈留白；底下那条 hover 操作条是
//     `Absolute()` 的，不占流）；
//   - 卡里的东西**居中**（`AlignItems(ui.Center)`），所以**头像的中心就是卡的
//     中心**。
//
// 于是：卡的上边 = 头像的上边 - 留白；卡的下边 = 签名的下边 + 留白；卡的左边 =
// 头像的中心 - 卡宽的一半。
//
// # cardW 是调用方给的
//
// 卡有多宽是**网格**算的（`list.CellWidth`），而卡自己不会说。
func followCardBox(t *testing.T, tt *mygoui.Tester, u app.FollowUser, cardW float32) mygoui.Rect {
	t.Helper()
	av, ok := tt.Find(u.Face)
	if !ok {
		t.Fatalf("「%s」的头像没画出来，文字有 %q", u.Name, tt.Texts())
	}
	sign, ok := tt.Find(u.Name + " 的签名")
	if !ok {
		t.Fatalf("「%s」的签名没画出来，文字有 %q", u.Name, tt.Texts())
	}
	top := av.Y - followCardPad
	return mygoui.Rect{
		X: av.X + av.W/2 - cardW/2,
		Y: top,
		W: cardW,
		H: sign.Y + sign.H + followCardPad - top,
	}
}
