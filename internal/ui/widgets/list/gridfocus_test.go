package list

import (
	"image"
	"testing"

	"github.com/egoist/mygo/ui"
)

// 这一族钉的是票 #453：**网格那一档也有焦点环了**。
//
// 网格和列表是**两套控件**（`grid.go` 自己画格子），所以列表那条环在这儿也得
// 单独断一遍——两处各写各的，改漏一边的后果是「列表里有环、网格里没有」。
//
// ⚠️ **判据全是画面上的像素**：主题里那个字段是什么值只是「打算画成什么」，
// 画面上那个像素是什么值才是「用户看见的是什么」。而焦点环这一档的三个症状
// **全都是静默的**：环在不在、有没有画到隔壁那一格上，画面上不逐像素读就
// 分不出来。

// gridFocusWidth 是这些用例里的网格宽度。1100 → 4 列（断点表：>= 1024）。
const gridFocusWidth = 1100

// gridFocusCols 是上面那个宽度下的列数。
const gridFocusCols = 4

// gridFocusY 是量格子左沿时取的那一条扫描线（在封面那一带，躲开圆角）。
const gridFocusY = 20

// TestTheKeyboardCellHasAFocusRing 钉验收第 3 条：键盘走到的那一格有焦点环。
//
// 三层：
//
//  1. **Tab 进网格、按方向键走位**，环落在**走到的**那一格上；
//  2. 环**不在**别的格上（只断「有一处环」的话，环画在整张网格上也能过）；
//  3. **鼠标点一格不算**：焦点不是从键盘来的，那一格什么标记都没有
//     （这正是票 #197 要的样子——上游点一格不改变它的样子）。
func TestTheKeyboardCellHasAFocusRing(t *testing.T) {
	_, tt := newGrid(t, Actions{}, songsOf(8, "UP 主"), gridFocusWidth, gridH)

	// Tab 把焦点交给网格（这一档自己装的 `Focusable`，理由见 grid.go）。
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	before := snap(tt)

	// 方向键走位：落在第一格上（还没选中任何一格时，方向键从第一格开始）。
	tt.Key(0, ui.KeyRight)
	tt.Frame()
	after := snap(tt)

	if !cellEdgeChanged(before, after, 0) {
		t.Errorf("键盘走到的那一格上没有焦点环")
	}
	if cellEdgeChanged(before, after, 1) {
		t.Errorf("隔壁那一格上多了焦点环——环只该在键盘走到的那一格上")
	}
}

// TestClickingACardLeavesNoFocusRing 是上一条的后一半：**鼠标点一格，那一格什么
// 标记都没有**。
//
// ⚠️ **单独一条**：上一条里焦点是从键盘来的（Tab 进来的），这一条里焦点是从
// 指针来的（直接点）。框架分得清这两者（`focusVisible`），而用户看得见的差别
// 就是「点过之后那一格有没有环」。
//
// ⚠️ **点完把指针挪开**：指针压在那���格上时封面有一层悬停反馈（压暗 + 播放
// 钮），那一层会改掉封面边沿的像素——挪开之后比的就只是「环有没有」这一件事。
func TestClickingACardLeavesNoFocusRing(t *testing.T) {
	_, tt := newGrid(t, Actions{}, songsOf(8, "UP 主"), gridFocusWidth, gridH)
	before := snap(tt)

	if err := tt.Click("歌 3"); err != nil {
		t.Fatal(err)
	}
	frame(tt)
	tt.Move(gridFocusWidth-4, gridH-4)
	tt.Frame()
	after := snap(tt)

	if cellEdgeChanged(before, after, 2) {
		t.Errorf("鼠标点过的那一格上多了焦点环——指针点一下不该留下焦点标记")
	}
}

// TestTheGridRingComesBackAfterAMouseClick 钉的是**评审点出来的那一条**：
// 鼠标点过一格之后再用方向键走位，键盘走到的那一格**必须有指示**。
//
// ⚠️ **这一条是「框架那个 `focusVisible` 靠不住」的证据**：框架只在 Tab 的时候把
// `rt.focusVisible` 设成 true，指针按一下会把它清掉，而**方向键不会设回来**——
// 所以「点一格 → 按 →」那一路上，框架的判据一路是 false，屏幕上什么都不画。
//
// 判据是三件事：
//
//  1. 点过之后那一格**没有环**（指针在管，鼠标点一格不改变它的样子）；
//  2. 按了方向键之后，环落在**键盘走到的那一格**上；
//  3. 原来那一格**没有环**了（环跟着键盘走，不留在原地）。
func TestTheGridRingComesBackAfterAMouseClick(t *testing.T) {
	g, tt := newGrid(t, Actions{}, songsOf(8, "UP 主"), gridFocusWidth, gridH)

	// 点第三格：指针在管，那一格什么标记都没有。
	if err := tt.Click("歌 3"); err != nil {
		t.Fatal(err)
	}
	frame(tt)
	tt.Move(gridFocusWidth-4, gridH-4)
	tt.Frame()
	before := snap(tt)
	if cellEdgeChanged(before, snap(tt), 2) {
		t.Fatalf("鼠标点过的那一格上多了焦点环——指针在管的时候不该有")
	}

	// 按方向键：键盘在管了，环得跟着键盘走。
	tt.Key(0, ui.KeyRight)
	tt.Frame()
	after := snap(tt)
	got, ok := g.Selected()
	if !ok || got.Title != "歌 4" {
		t.Fatalf("按了方向键，选中的却是 (%q, %v)", got.Title, ok)
	}
	if !cellEdgeChanged(before, after, 3) {
		t.Errorf("鼠标点过之后按了方向键，键盘走到的那一格上却没有焦点环")
	}
	if cellEdgeChanged(before, after, 2) {
		t.Errorf("原来那一格上还有焦点环——环没有跟着键盘走")
	}
}

// TestTheGridRingStaysInsideItsCell 钉验收第 5 条：**焦点环往里收 3 像素**。
//
// ⚠️ **不收会盖到隔壁那一格**：框架默认那一圈是画在元素**外面**的
// （`mygo/ui/paint.go` 的 `FocusRing`：往外 3 像素），而网格两格之间只有 16
// 像素的缝——画在外面的话，环会压到隔壁那一格的边上。
//
// 判据是**两格之间那条缝**：那一格有没有环，缝里的像素都得是原来的底色。
func TestTheGridRingStaysInsideItsCell(t *testing.T) {
	_, tt := newGrid(t, Actions{}, songsOf(8, "UP 主"), gridFocusWidth, gridH)

	tt.Key(0, ui.KeyTab)
	tt.Frame()
	before := snap(tt)

	tt.Key(0, ui.KeyRight)
	tt.Frame()
	after := snap(tt)

	cell := int(cellWidth(gridFocusWidth, gridFocusCols))
	// 缝在第一格和第二格之间：[cell, cell+gridGap)。
	for x := cell; x < cell+int(gridGap); x++ {
		if before.RGBAAt(x, gridFocusY) != after.RGBAAt(x, gridFocusY) {
			t.Errorf("两格之间那条缝的 x=%d 那一列变了（%v → %v）——"+
				"环画到了隔壁那一格边上", x,
				before.RGBAAt(x, gridFocusY), after.RGBAAt(x, gridFocusY))
			break
		}
	}
}

// TestTheGridArrowKeysWalkTheCells 钉「键盘走位」本身：方向键按网格的二维走。
//
// ⚠️ **左右不绕行**：从一行的最后一格按 → 不跳到下一行的第一格（那是 ↓ 的事）。
// 绕行的话「按了 → 却跳到下一行」和「按 ↓」是同一个动作。
func TestTheGridArrowKeysWalkTheCells(t *testing.T) {
	g, tt := newGrid(t, Actions{}, songsOf(8, "UP 主"), gridFocusWidth, gridH)

	tt.Key(0, ui.KeyTab)
	tt.Frame()
	// 还没选中任何一格时，方向键落在**第一格**（和列表那一档同一个规矩）。
	tt.Key(0, ui.KeyRight)
	tt.Frame()
	if got, ok := g.Selected(); !ok || got.Title != "歌 1" {
		t.Fatalf("按了方向键，选中的却是 (%q, %v)", got.Title, ok)
	}

	// →：同一行的下一格。
	tt.Key(0, ui.KeyRight)
	tt.Frame()
	if got, ok := g.Selected(); !ok || got.Title != "歌 2" {
		t.Fatalf("按了 →，选中的却是 (%q, %v)", got.Title, ok)
	}

	// ↓：下面那一行的**同一列**（4 列 → 往下 4 首）。
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if got, ok := g.Selected(); !ok || got.Title != "歌 6" {
		t.Fatalf("按了 ↓，选中的却是 (%q, %v)，want 歌 6（下一行的同一列）", got.Title, ok)
	}

	// ↑：回到上面那一行。
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if got, ok := g.Selected(); !ok || got.Title != "歌 2" {
		t.Fatalf("按了 ↑，选中的却是 (%q, %v)，want 歌 2", got.Title, ok)
	}

	// ←：同一行的前一格。
	tt.Key(0, ui.KeyLeft)
	tt.Frame()
	if got, ok := g.Selected(); !ok || got.Title != "歌 1" {
		t.Fatalf("按了 ←，选中的却是 (%q, %v)，want 歌 1", got.Title, ok)
	}
}

// TestTheGridHandsTheFocusBackAtTheTop 钉「↑ 在顶上一行时把焦点交回侧栏」
// （票 #245 的 E4）——网格这一档也要有这一档。
//
// ⚠️ **判据是「交回的那一下」**：框架的方向键事件在两遍 build 之间送达，而游标
// 是在 build 之后挪的——所以「松开 ↑ 的那一刻」不等于「↑ 挪完之后」。这一档靠
// `KeyboardMode` 自己记的那份时间坐标（理由见 `selection.go`）。
func TestTheGridHandsTheFocusBackAtTheTop(t *testing.T) {
	out := false
	acts := Actions{FocusOut: func() { out = true }}
	g, tt := newGrid(t, acts, songsOf(8, "UP 主"), gridFocusWidth, gridH)

	tt.Key(0, ui.KeyTab)
	tt.Frame()
	// 方向键走到第一格（网格的第一行第一列）。
	tt.Key(0, ui.KeyRight)
	tt.Frame()
	tt.Key(0, ui.KeyRight)
	tt.Frame()
	if out {
		t.Fatalf("还没到顶上就把焦点交回侧栏了")
	}

	// ↑：从第一格往上是**顶上一行**——网格的第一行上面没有格子，所以这一次
	// 挪不动，焦点交回侧栏。
	tt.KeyDown(ui.KeyUp)
	tt.Frame()
	tt.KeyUp(ui.KeyUp)
	tt.Frame()
	tt.Frame()
	if !out {
		t.Errorf("在顶上按了 ↑，焦点没有交回侧栏（网格那一档没接住这一档）")
	}
	// 游标还停在原来那一格上（挪不动就是挪不动）。
	if got, ok := g.Selected(); !ok || got.Title != "歌 2" {
		t.Errorf("在顶上按了 ↑，选中的却是 (%q, %v)，want 歌 2（挪不动就是不动）", got.Title, ok)
	}
}

// TestEnterPlaysTheCellTheCursorIsOn 钉「回车放歌」——和列表那一档**同一个手势**
// （列表的 `OnSubmit` 走的是 `playFrom`，网格的双击走的也是它）。
//
// ⚠️ **判据是「放出去的那一批」**：从游标那一首**往后**（`playFrom` 的口径），
// 不是只放那一首。
func TestEnterPlaysTheCellTheCursorIsOn(t *testing.T) {
	songs := songsOf(8, "UP 主")
	rec := &recorded{}
	g, tt := newGrid(t, rec.actions(), songs, gridFocusWidth, gridH)

	tt.Key(0, ui.KeyTab)
	tt.Frame()
	// 走到第二格。
	tt.Key(0, ui.KeyRight)
	tt.Frame()
	tt.Key(0, ui.KeyRight)
	tt.Frame()
	if got, ok := g.Selected(); !ok || got.Title != "歌 2" {
		t.Fatalf("游标在 %q（%v），want 歌 2", got.Title, ok)
	}

	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	tt.Frame()

	if len(rec.played) != 1 {
		t.Fatalf("回车放了 %d 次，want 1 次", len(rec.played))
	}
	got := rec.played[0]
	if len(got) != len(songs)-1 {
		t.Fatalf("放出去的是 %d 首，want %d 首（从游标那一首往后）", len(got), len(songs)-1)
	}
	if got[0].Identity() != songs[1].Item.Identity() {
		t.Errorf("放出去的第一首是 %v，want 游标那一首 %v", got[0].Identity(), songs[1].Item.Identity())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 量的那几样
// ─────────────────────────────────────────────────────────────────────────────

// snap 是这一帧画面的**副本**。
//
// ⚠️ **必须复制**：`Tester.Image()` 给的是画布那一块缓冲的**视图**
// （`mygo/ui/headless.go` 的 `image()`：直接拿画布的 `Pix`），不复制的话
// 「之前那一帧」和「现在这一帧」是同一块内存，比出来永远是「一样」。
func snap(tt *ui.Tester) *image.RGBA {
	src := tt.Image()
	dst := image.NewRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}

// cellEdgeChanged 报第 i 格（0 起，同一行内）的**左沿**在两张图之间变了没有。
//
// ⚠️ **量的是左沿那两像素**：焦点环是往里收 3 像素之后画在格子边沿上的
// （`selection.go` 的 `rowFocusRing` + 框架那圈往外 3 像素的画法，两者一抵消
// 正好落在格子自己的边沿上），所以有环的时候格子最左边那两像素是环的颜色。
//
// ⚠️ **比的是「变没变」而不是「是不是环那个颜色」**：环是半透明的（框架的主题
// 里 `Focus` 带 alpha），压在什么底上就是什么颜色——写死一个蓝的话，换一套
// 主题（或者用户自己挑的主色）就飘了。
func cellEdgeChanged(before, after *image.RGBA, i int) bool {
	x := cellLeft(i)
	return before.RGBAAt(x, gridFocusY) != after.RGBAAt(x, gridFocusY) ||
		before.RGBAAt(x+1, gridFocusY) != after.RGBAAt(x+1, gridFocusY)
}

// cellLeft 是第 i 格（0 起，同一行内）的左边沿在哪。
//
// ⚠️ **格子是布局，列宽是算出来的**：行里几首、每首多宽，都由容器的宽分档
// 决定（`columnsFor` / `cellWidth`）——所以这里照那一个算法算，不量。
func cellLeft(i int) int {
	cell := cellWidth(gridFocusWidth, gridFocusCols)
	return int(float32(i) * (cell + gridGap))
}
