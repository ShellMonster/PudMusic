// 「选中」这一层：选中的一行看起来该是什么样。
//
// # 这一层为什么单独一个文件
//
// 票 #197（2026-10-22 拍板）把「选中」这件事整个翻了过来：**点一行不再整行刷
// 实心主色底**。上游压根没有这一层——点一行不改变它的样子，而我们原来点一下就
// 是一整块绿。
//
// 翻过来之后，「选中」这一层只剩两件事：
//
//  1. **盖掉框架那一块实心主色底**。框架的 `ui.List` 给选中的行铺一整块主色底
//     （`mygo/ui/table.go` 的 node.Selected：`e.Background(t.Accent)`），那一层
//     在库里，我们改不动——所以选中的行自己刷一层底色把它盖掉。
//  2. **键盘走到哪一行，那一行有一圈焦点环**。焦点环是保留的（DESIGN.md §七：
//     「焦点环：保留，不要为了对齐去掉」），只是它落在**那一行**上，而不是框架
//     默认画在整张列表上的那一圈。
//
// # 「正在放」是另一件事
//
// 「正在放」那一行的主色高亮**不在这一层**：它是 flat 的（主色当字色，见
// `card.go` 的 `rowColors`），跟着声音走，不跟着光标走。选中这一层只管「选中」。
//
// # 哪些调用方
//
// `widgets/list` 的列表那一行（`card.go`）和网格那一格（`grid.go`），以及
// `internal/ui` 里自己画行的两页（稍后再看 / 历史记录）——它们说的都是同一件事，
// 所以**走同一个出口**，别各处写一份（理由同 `card.go` 文件头那句「同一个应用里
// 两套列表写着两种说法，用户会以为是两个意思」）。
package list

import (
	"github.com/egoist/mygo/ui"
)

// SelectedRow 把「选中」这一层加到这一行上。
//
// **没选中就原样还回去**：一个字节都不多画——选中的行和没选中的行长得**一个像素
// 都不差**（票 #197：点一行不再整行刷绿）。
//
// 选中时做两件事（理由见文件头）：
//
//  1. 刷一层底色盖掉框架那一块实心主色（`rowCover`）；
//  2. 键盘走到这一行时，给它一圈焦点环（`rowFocusRing`）。
//
// state 是这张列表的状态：焦点是不是从键盘来的，问的是它（`FocusVisible`）。
//
// ⚠️ **两个都要 `selected`**：没选中的行不该有环，而鼠标点过之后那一行连环都
// 不该有（焦点不是从键盘来的）。
func SelectedRow(row ui.Element, c *ui.Context, t *ui.Theme, state *ui.ListState,
	selected, hovered bool) ui.Element {
	if !selected {
		return row
	}
	row = row.Background(rowCover(t, hovered))
	if FocusVisible(c, state) {
		row = rowFocusRing(row, t)
	}
	return row
}

// FocusVisible 报这张列表此刻是不是「焦点从键盘来的」。
//
// # 为什么问的是**列表**而不是某一帧的那一行
//
// 焦点在框架里落在**列表这个元素**上（`mygo/ui/list.go` 的 buildList：列表有游标
// 时 `e.Focusable()`），方向键挪的是那个游标（`*ListState.Selected`）。所以
// 「键盘走到哪一行」= 「列表有焦点、焦点是键盘带来的，而那一行是选中的」。
//
// # 怎么问的
//
// `ListState.Handle` 绑的就是列表这个元素（`mygo/ui/list.go` 的 buildList 里
// `wrapElement(owner).Bind(&s.Handle)`，而它**在建行之前**就绑好了），所以
// `c.Resolve` 拿回来的就是它；`FocusVisible()` 报的是「有焦点 && 焦点是键盘
// 带来的」（`mygo/ui/input.go`：指针点一下会把它清掉）。
func FocusVisible(c *ui.Context, state *ui.ListState) bool {
	return c.Resolve(state.Handle).FocusVisible()
}

// rowCover 是「选中那一行」刷在行身上的底色。
//
// # 盖的是什么色
//
// **页面底色**（`t.Background`）。列表直接摆在内容区里，内容区就是页面底色
// （`internal/ui/theme.go` 的那一层，`chromebg_test.go` 的
// `TestTheChromeSharesOneBackground` 钉着它）——所以盖上去之后，选中的行和旁边
// 没选中的行**一个像素都不差**，看不出有人动过手。
//
// ⚠️ **悬停时换成 `t.SurfaceHover`**：没选中的行被指针压住时，框架刷的就是那个
// 色（`mygo/ui/list.go` 的 styleFn：`w.bg = t.SurfaceHover`）。选中这一行也得跟
// 它一样，否则「指针停在选中行上」和「停在别的行上」是两种反馈。
//
// ⚠️ **别改成主色**：那就是「实心主色底」，票面说的正是去掉它。
func rowCover(t *ui.Theme, hovered bool) ui.Color {
	if hovered {
		return t.SurfaceHover
	}
	return t.Background
}

// rowFocusRing 是「键盘走到哪一行」那一圈环。
//
// # 判据是**焦点是不是从键盘来的**
//
// 指针点一下会把框架那个标志清掉（`mygo/ui/input.go`：`rt.focusVisible = false`），
// 于是鼠标点过之后那一行什么标记都没有——**这正是票 #197 要的样子**（上游点一行
// 不给它任何标记）。而 Tab 进来之后按方向键走位，环跟着走。
//
// # 为什么自己画
//
// 框架自己那一圈是**画在整张列表上**的（`ringShown`），不是画在行上；票面要的是
// 「键盘走到哪一行都要有可见焦点指示」——指示得落在**那一行**上。
//
// ⚠️ **环往里收 3 像素**：画在行沿上的话会盖到隔壁那一行（两行紧挨着），而框架
// 画行焦点环时也是收这 3 像素（`mygo/ui/list.go` 的 `FocusRing(Rect{r.X + 3, …})`）。
func rowFocusRing(row ui.Element, t *ui.Theme) ui.Element {
	return row.DrawOver(func(p *ui.Painter, r ui.Rect) {
		p.FocusRing(ui.Rect{X: r.X + 3, Y: r.Y + 3, W: r.W - 6, H: r.H - 6},
			[4]float32{t.Radius, t.Radius, t.Radius, t.Radius})
	})
}
