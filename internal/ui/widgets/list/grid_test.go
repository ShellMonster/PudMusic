package list

import (
	"bytes"
	"fmt"
	"image/color"
	"log"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// 这一组验网格（验收第 1~3 条）：**按 Biu 的四个断点换列数**（不是连续缩放）、
// **跨断点时滚动锚点保住**、**卡片能右键点同一套菜单**。
//
// 跑的是 MyGo 的 Tester，和 list_test.go 同一套：窗口是假的，布局、点击、改窗口
// 大小和系统菜单都是真的。改窗口大小用 `tt.SetSize`，所以「用户拖窗口边」这件事
// 在测试里发生的就是同一件事。
//
// ⚠️ Tester 的高度固定：网格的行高是**量出来**的（歌名折行就高一点），判定
// 「一行放了几首」靠的是歌名落在哪一行上，所以高度要够放得下好几行。

// gridH 是无头窗口的高度。网格每行约 195（140 的封面 + 歌名 + UP 名），700 摆得
// 下三行半，够数清一行有几首、也够验锚点。
const gridH = 700

// gridN 是这些用例里的歌数。够摆满好几十行，所以「顶上那一行」永远是满的
// （不满的是最后一行，量列数时数不到那儿）。
const gridN = 40

// newGrid 造一个网格并把歌给它，返回网格和画着它的 Tester（已经画过几帧）。
func newGrid(t *testing.T, acts Actions, songs []Music, w, h int) (*Grid, *ui.Tester) {
	t.Helper()
	g := NewGrid(newCache(t), acts)
	g.SetSongs(songs)
	tt := ui.NewTester(g.View, w, h)
	tt.Frame()
	frame(tt)
	return g, tt
}

// titleOf 是第 i 首（0 起）的歌名，和 `songsOf` 的编号对得上。
func titleOf(i int) string { return fmt.Sprintf("歌 %d", i+1) }

// gridRowTop 是某一首**所在那一行**的上边缘。
//
// ⚠️ `ListState.Label` 装的是每一行**第一首**的歌名，所以 `tt.Find` 命中那一首
// 时给回来的是**整行**的盒子（x=0、宽满一屏、高是一行的高），别的首命中的是歌名
// 那一小块（小方块，在封面下面）。两种盒子差一个固定的量（封面 + 上边距），
// 这里减掉它，两种盒子就都落回所在行的上边缘。
func gridRowTop(r ui.Rect) float32 {
	if r.H > 100 {
		return r.Y // 整行的盒子
	}
	return r.Y - (gridCoverH + cardPadY)
}

// songOnTopRow 报第 i 首（0 起）在不在**顶上那一行**里。
//
// ⚠️ 找不到和「找到了但是个空盒子」都要算不在：`Tester.Find` 找的是这一帧建出来
// 的元素，虚拟列表会在上下各多建两行给 Tab 和读屏用，那些建在画面外的行，它
// 里面歌名的盒子是空的（宽度 0）。只看「找没找到」会把画面外的那几行也算进来。
func songOnTopRow(t *testing.T, tt *ui.Tester, i int) bool {
	t.Helper()
	r, ok := tt.Find(titleOf(i))
	if !ok || r.W == 0 || r.H == 0 {
		return false
	}
	return gridRowTop(r) < 1
}

// songsOnTopRow 数顶上那一行放了几首。
//
// 从第二首往后数，数到第一首不在那一行为止。歌不够放满一行时数到末尾为止——
// 最后一行不满是正常的（Biu 的 CSS grid 也是这么摆的）。
func songsOnTopRow(t *testing.T, tt *ui.Tester, n int) int {
	t.Helper()
	got := 1
	for i := 1; i < n; i++ {
		if !songOnTopRow(t, tt, i) {
			break
		}
		got++
	}
	return got
}

// ─────────────────────────────────────────────────────────────────────────────
// 断点
// ─────────────────────────────────────────────────────────────────────────────

// TestColumnsFollowTheBiuBreakpoints 是验收第 1 条：列数按 Biu 的四个断点换。
//
// ⚠️ 这些数字是从 Biu 的 `src/components/virtual-grid-page-list/index.tsx` 里
// 那份 `columns` 抄的（`>=1536 → 6`、`>=1280 → 5`、`>=1024 → 4`、`>=768 → 3`、
// 更窄 2 列），一个都不能差。**每个断点上下各钉一个宽度**：断点本身和断点上方
// 1 像素是「刚好够到下一档」和「还没够到」的分界，判成 `>=` 还是 `>` 写错了
// 只有这一条看得出来（PLAN.md 的验收说的也是「800px 三列、1400px 五列」，
// 那是同一张表的两个点）。
func TestColumnsFollowTheBiuBreakpoints(t *testing.T) {
	for _, c := range []struct{ w, cols int }{
		{500, 2}, {767, 2}, // 比 768 窄：2 列
		{768, 3}, {900, 3}, {1023, 3}, // 768~1023：3 列
		{1024, 4}, {1200, 4}, {1279, 4}, // 1024~1279：4 列
		{1280, 5}, {1400, 5}, {1535, 5}, // 1280~1535：5 列
		{1536, 6}, {1900, 6}, // 1536 以上：6 列
	} {
		_, tt := newGrid(t, Actions{}, songsOf(gridN, "UP 主"), c.w, gridH)
		if got := songsOnTopRow(t, tt, gridN); got != c.cols {
			t.Errorf("窗口宽 %d 时顶上那一行放 %d 首，want %d 列", c.w, got, c.cols)
		}
	}
}

// TestTheColumnCountIsAStepNotAContinuousScale 是验收第 1 条里「不是连续缩放」
// 的那半句：`ui.GridView` 是按最小格宽**连续**算列数的
// （`mygo/ui/gridview.go`：`cols = (width+gap)/(minWidth+gap)`），拖窗口的
// 时候列数一跳一跳地变。Biu 要的是分档：两档之间拖到哪都是同一档。
//
// 宽度从 800 一路加到 1023（都在 768~1023 那一档里），列数必须是 3；过了 1024
// 才变 4。中间**不许**出现 4 列以上——连续缩放在这个区间里会从 3 慢慢长到 4。
func TestTheColumnCountIsAStepNotAContinuousScale(t *testing.T) {
	_, tt := newGrid(t, Actions{}, songsOf(gridN, "UP 主"), 800, gridH)
	for _, w := range []int{800, 850, 900, 950, 1000, 1023} {
		tt.SetSize(w, gridH)
		frame(tt)
		if got := songsOnTopRow(t, tt, 16); got != 3 {
			t.Fatalf("宽 %d（768~1023 那一档里）放 %d 列，want 3（分档不该在档内变）", w, got)
		}
	}
	tt.SetSize(1024, gridH)
	frame(tt)
	if got := songsOnTopRow(t, tt, 16); got != 4 {
		t.Errorf("宽 1024 放 %d 列，want 4（够到下一档就该换）", got)
	}
}

// TestColumnsAreEvenlyWideAcrossTheRow 验格子是**等宽**平分的：封面按格子宽画，
// 格子窄一分封面就盖不满、多一分就压到隔壁。
//
// 判据是相邻两格歌名的横向间距（歌名在格子左边、格子之间没有横向内边距，所以歌名
// 的 x 就是格子的 x）：它要正好是「一格的宽 + 一道缝」。Biu 的 CSS grid 是
// `repeat(n, minmax(0,1fr))` 配 `gap: 16`，MyGo 的 `ui.Grid` 按同一个算法分。
func TestColumnsAreEvenlyWideAcrossTheRow(t *testing.T) {
	const w, cols = 1100, 4 // 1100 落在 1024~1279 那一档（4 列）
	_, tt := newGrid(t, Actions{}, songsOf(gridN, "UP 主"), w, gridH)

	want := cellWidth(float32(w), cols) + gridGap
	a, ok := tt.Find(titleOf(0))
	if !ok {
		t.Fatalf("第一首不在画面上，文字有 %q", tt.Texts())
	}
	for i := 1; i < cols; i++ {
		b, ok := tt.Find(titleOf(i))
		if !ok || b.W == 0 {
			t.Fatalf("第 %d 首不在画面上，文字有 %q", i, tt.Texts())
		}
		// 第 0 首命中的是整行的盒子，它的 x 就是第一格的 x（行是顶格对齐的）。
		// 第 i 格比第 0 格远 i 个「一格的宽 + 一道缝」。
		if wantX := a.X + float32(i)*(want); abs32(b.X-wantX) > 0.5 {
			t.Errorf("第 %d 格的歌名在 x=%v，want %v（一格的宽 %v + 一道缝 %v，%d 格）",
				i, b.X, wantX, cellWidth(float32(w), cols), gridGap, i)
		}
	}
}

// TestColumnsFollowTheContainersWidthNotTheWindows 钉住「按**容器**宽算列数」
// 这条被注释称作承重的决定：Biu 量的是网格自己那个 wrapper（`useSize(wrapperRef)`），
// 真机上页面左边有侧栏，容器比窗口窄一截。
//
// ⚠️ 别的用例里窗口**就是**网格容器（网格直接摆在根上），所以它们分不出这两种
// 做法——把 View 里量宽的那句从 `g.width` 换成 `c.Size()`（窗口宽），它们照样全绿。
// 这里在网格左边摆一条 220 宽的侧栏：窗口 1400，容器 1180。按容器算是 4 列
// （1024~1279 那一档），按窗口算是 5 列——差一档。
//
// ⚠️ 网格外面套一层 Column 是为了 Tester 量得到格子：滚动容器直接当 Row 的下一层
// 时，它里面那些文字的盒子量出来是空的（`Tester.Find` 给一个 0×0 的矩形），怎么
// 摆都分不出几列（试过 Row.Fill / Row.FillHeight / Column+Row.Grow 三种）。真窗口
// 里画得好好的，量不到只是无头那套的记录方式。
func TestColumnsFollowTheContainersWidthNotTheWindows(t *testing.T) {
	const winW, sideW = 1400, 220
	g := NewGrid(newCache(t), Actions{})
	g.SetSongs(songsOf(gridN, "UP 主"))

	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Fill().Children(func() {
			ui.Box(c).Width(sideW) // 侧栏，和真窗口一样占着左边一块
			ui.Column(c).Grow(1).FillHeight().Children(func() {
				g.View(c) // 网格吃剩下的 1180
			})
		})
	}, winW, gridH)
	tt.Frame()
	frame(tt)

	if got := songsOnTopRow(t, tt, gridN); got != 4 {
		t.Errorf("窗口 %d 宽、侧栏 %d 宽（容器 %d）时顶上那一行放 %d 首，want 4 列"+
			"（按容器的宽算，不是按窗口的宽）", winW, sideW, winW-sideW, got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 滚动锚点
// ─────────────────────────────────────────────────────────────────────────────

// TestResizingAcrossABreakpointKeepsTheSameSongInView 是验收第 2 条：窗口缩放
// 跨断点时列数正确切换，**用户盯着的那一首歌还在视野里**。
//
// ⚠️ 判据是**同一首歌还在顶上那一行**，不是「滚动量没变」：6 列的第 1000 行是
// 2 列的第 3000 行，滚动量一样的话指的是完全不同的歌。
//
// ⚠️ 特意选了一个**远**的歌单（8000 首）并且在中间：行号要挪 2000 行，而框架按
// key 找回锚点时只在前后各 1000 行里找（`mygo/ui/list.go` 的 listSearch）。所以
// 这一条钉的是**我们自己的那套换算**（keepAnchor），不是框架顺手给的——歌单一小
// 或者只挪两行，框架那条路也能过，测不出问题。
func TestResizingAcrossABreakpointKeepsTheSameSongInView(t *testing.T) {
	// 两个方向都验：缩窄（6→2 列，行号 1000→3000）和拉宽（2→6 列，反过来）。
	for _, c := range []struct{ from, to int }{{1600, 500}, {500, 1600}} {
		const anchor = 6000
		g, tt := newGrid(t, Actions{}, songsOf(8000, "UP 主"), c.from, gridH)

		g.ScrollToItem(anchor, ui.Start)
		frame(tt)
		if !songOnTopRow(t, tt, anchor) {
			t.Fatalf("宽 %d：滚到第 %d 首之后它不在顶上那一行", c.from, anchor+1)
		}

		tt.SetSize(c.to, gridH)
		frame(tt)

		r, ok := tt.Find(titleOf(anchor))
		if !ok || r.W == 0 || r.H == 0 {
			t.Errorf("宽 %d 改成 %d 之后，用户正盯着的那首（%s）不在画面上了",
				c.from, c.to, titleOf(anchor))
			continue
		}
		if top := gridRowTop(r); top > 1 || top < -1 {
			t.Errorf("宽 %d 改成 %d 之后那一首跑到 y=%v 去了，want 0"+
				"（滚动锚点没保住，列数换了歌也跟着跳走了）", c.from, c.to, top)
		}
		// 没有跳回顶部：跳回顶部是「锚点丢了」最常见的那种坏法。
		if top, ok := tt.Find("歌 1"); ok && top.H > 0 {
			t.Errorf("宽 %d 改成 %d 之后画面回到了第一首（滚动锚点丢了）", c.from, c.to)
		}
	}
}

// TestDraggingTheWindowEdgeKeepsTheSameSongInView 是验收第 2 条在**人真正的做法**
// 下的那一半：上面那条走的是「一步跳到某个宽度」，可人拖窗口边是一帧一帧拖过去
// 的，中间要连着跨过好几个断点。
//
// 🔴 这一条钉的是「锚点记的是**那一首歌**，不是「第几行」」：滚动单位是行，歌在
// 第几行是算出来的（歌的下标除以列数），一取整就丢。记行号的话每跨一档丢一次，
// 拖 1600→500 要连丢四次（6→5→4→3→2），误差累加起来用户盯着的那首整个掉出画面
// ——实测漂 6 首、掉到 y=632（窗口才 700 高），而同样宽度一步跳过去它还在。
//
// 判据是**每一拖都还在顶上那一行**，不是「最后还在」：中途掉下去再被后面的帧
// 拉回来，最后一条是绿的，中间那一路用户已经跟丢了。
func TestDraggingTheWindowEdgeKeepsTheSameSongInView(t *testing.T) {
	const anchor = 5000 // 6 列下它是顶上那一行的第 3 格（0 起），不是行首
	g, tt := newGrid(t, Actions{}, songsOf(8000, "UP 主"), 1600, gridH)

	g.ScrollToItem(anchor, ui.Start)
	frame(tt)
	if !songOnTopRow(t, tt, anchor) {
		t.Fatalf("滚到第 %d 首之后它不在顶上那一行", anchor+1)
	}

	// 每帧 20 像素地拖窄：真机上拖窗口边就是这个节奏（一次拖动经过好几档）。
	for w := 1600; w > 500; w -= 20 {
		tt.SetSize(w, gridH)
		frame(tt)
		if !songOnTopRow(t, tt, anchor) {
			t.Fatalf("拖到宽 %d 时，第 %d 首掉出顶上那一行了"+
				"（锚点存的是行号，误差逐跳累加了）", w, anchor+1)
		}
	}
	// 来回拖才是窗口边被反复拽的样子：拖回去也得守住同一首。
	for w := 500; w < 1600; w += 20 {
		tt.SetSize(w, gridH)
		frame(tt)
		if !songOnTopRow(t, tt, anchor) {
			t.Fatalf("拖回宽 %d 时，第 %d 首掉出顶上那一行了", w, anchor+1)
		}
	}
}

// TestResizingInsideOneStepKeepsThePlaceToo 断点**之内**改窗口大小（列数不变）
// 也不能跳：行高会重测一遍（标题按新宽度重新折行），位置要稳。
//
// 这条和上一条不是一回事：上一条换列数要搬锚点，这一条列数没变，搬错了反而是
// 把用户看的那首歌挪走了。
func TestResizingInsideOneStepKeepsThePlaceToo(t *testing.T) {
	const anchor = 600
	g, tt := newGrid(t, Actions{}, songsOf(4000, "UP 主"), 1400, gridH) // 5 列
	g.ScrollToItem(anchor, ui.Start)
	frame(tt)
	if !songOnTopRow(t, tt, anchor) {
		t.Fatalf("滚到第 %d 首之后它不在顶上那一行", anchor+1)
	}

	// 1280~1535 之间，还是 5 列。
	tt.SetSize(1300, gridH)
	frame(tt)

	if !songOnTopRow(t, tt, anchor) {
		t.Errorf("在同一档里把窗口改窄之后，用户盯着的那首（%s）不在顶上那一行了",
			titleOf(anchor))
	}
}

// TestScrollToItemFollowsTheSongNotTheRow 验 `ScrollToItem` 收的是「第几首」：
// 列数换了之后，同一个行号指的是别的歌，拿行号滚过去就滚错歌了。
func TestScrollToItemFollowsTheSongNotTheRow(t *testing.T) {
	const anchor = 300
	g, tt := newGrid(t, Actions{}, songsOf(2000, "UP 主"), 1600, gridH) // 6 列
	g.ScrollToItem(anchor, ui.Start)
	frame(tt)

	tt.SetSize(500, gridH) // 2 列：行号从 50 变成 150
	frame(tt)

	if !songOnTopRow(t, tt, anchor) {
		t.Errorf("换列数之后滚到的是 %s 那一行，不是用户要的第 %d 首",
			titleOf(songAtTopRow(t, tt)), anchor+1)
	}
}

// songAtTopRow 是此刻顶上那一行**第一首**的下标。
func songAtTopRow(t *testing.T, tt *ui.Tester) int {
	t.Helper()
	for i := range 40 {
		if songOnTopRow(t, tt, i) {
			return i
		}
	}
	t.Fatal("顶上那一行找不到任何一首歌")
	return 0
}

// TestDoubleClickingACardPlaysIt 验双击播放，和列表双击那一行是同一个意思
// （Biu 的 `onDoubleClick`）。单击只该选中。
//
// ⚠️ 点两下而不是两下之间停一下：`Tester` 认的是平台的连击数，同一个位置连着
// 两下才算双击。判据是**只播了一次**：双击的实现有两种错法——只认第一下（没播），
// 或者每一遍重建都放一次（放了两三遍，队列被反复替换）。
func TestDoubleClickingACardPlaysIt(t *testing.T) {
	rec := &recorded{}
	g, tt := newGrid(t, rec.actions(), onlineSongs(8), 1100, gridH) // 4 列

	r, ok := tt.Find("在线的歌 C")
	if !ok {
		t.Fatalf("画面上找不到那一格，文字有 %q", tt.Texts())
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	frame(tt)

	if len(rec.played) != 1 {
		t.Fatalf("双击之后收到 %d 次播放，want 1 次（单击只该选中）", len(rec.played))
	}
	if got := rec.played[0][0].Bvid; got != "BV3" {
		t.Errorf("双击第三首放出来的是 %q，want BV3", got)
	}
	// 双击的那一格同时也是选中的那一格（两个手势说的是同一首）。
	if got, ok := g.Selected(); !ok || got.Title != "在线的歌 C" {
		t.Errorf("双击之后选中的不是那一首（%q, %v）", got.Title, ok)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 右键菜单
// ─────────────────────────────────────────────────────────────────────────────

// TestRightClickOnACardOpensTheSameMenuAsAListRow 是验收第 3 条：网格行也能右键
// 点菜单，而且菜单**就是列表那一套**（同一个 `buildSongMenu`，不是照着再写一份）。
//
// ⚠️ 判据是**菜单上真的有哪几项**（`tt.Menu()`，`-` 是分隔）：菜单是这一趟全部
// 建出来的，少建一项那一项就永远点不到。
func TestRightClickOnACardOpensTheSameMenuAsAListRow(t *testing.T) {
	_, tt := newGrid(t, Actions{}, onlineSongs(8), 1100, gridH) // 4 列
	if err := tt.RightClick("在线的歌 C"); err != nil {
		t.Fatal(err)
	}
	got := tt.Menu()
	want := []string{"播放", "下一首播放", "添加到队列", "-", "在 B 站打开", "-", "收藏", "下载音频"}
	if !slices.Equal(got, want) {
		t.Errorf("卡片上的菜单有 %q，want %q", got, want)
	}
}

// TestChoosingPlayOnACardQueuesThatCardAndTheRestAfterIt 钉住菜单挂在**格**上：
// 按的是第三首，放出来的就得是第三首往后那几首。菜单挂到整页共用一份的话，按
// 第三首放出来的是第一首——那比不弹还糟。
func TestChoosingPlayOnACardQueuesThatCardAndTheRestAfterIt(t *testing.T) {
	rec := &recorded{}
	_, tt := newGrid(t, rec.actions(), onlineSongs(8), 1100, gridH)

	if err := tt.RightClick("在线的歌 C"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("播放"); err != nil {
		t.Fatal(err)
	}
	if len(rec.played) != 1 {
		t.Fatalf("收到 %d 次播放，want 1 次", len(rec.played))
	}
	if got := rec.played[0]; len(got) != 6 {
		t.Fatalf("从第三首开始，队列里进了 %d 首，want 6 首（它和它后面那五首）", len(got))
	}
	for i, want := range []string{"BV3", "BV4", "BV5", "BV6", "BV7", "BV8"} {
		if rec.played[0][i].Bvid != want {
			t.Errorf("队列第 %d 首是 %q，want %q", i+1, rec.played[0][i].Bvid, want)
		}
	}
}

// TestChoosingAddOnACardAppendsJustThatSong 验「添加到队列」是加一首，不换队列。
func TestChoosingAddOnACardAppendsJustThatSong(t *testing.T) {
	rec := &recorded{}
	_, tt := newGrid(t, rec.actions(), onlineSongs(8), 1100, gridH)

	if err := tt.RightClick("在线的歌 D"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("添加到队列"); err != nil {
		t.Fatal(err)
	}
	if len(rec.added) != 1 || len(rec.added[0]) != 1 {
		t.Fatalf("「添加到队列」收到 %v，want 只有一首", rec.added)
	}
	if got := rec.added[0][0].Bvid; got != "BV4" {
		t.Errorf("加进队列的是 %q，want BV4", got)
	}
	if len(rec.played) != 0 {
		t.Errorf("「添加到队列」顺手把队列换了（收到 %d 次播放）", len(rec.played))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 🔴 铁律四：选中的身份
// ─────────────────────────────────────────────────────────────────────────────

// TestClickingACardChoosesThatSong 验点哪一格选中哪一首，而且**画出来了**。
//
// ⚠️ 判据有两层：拿到的歌（`Selected()`）和画面上那一格的底色。只断言字段不够——
// 字段对了画面没跟着变，同样算坏。
//
// ⚠️ 点的是「歌 3」这样的**非行首**格子：`ListState.Label` 装的是每一行第一首
// 的歌名，`Tester.Click` 点的是它命中的那个盒子的正中，行首那首命中的是整行的
// 盒子，正中落在**中间那一格**上——点它选中的不是它。
func TestClickingACardChoosesThatSong(t *testing.T) {
	const w, cols = 1100, 4
	g, tt := newGrid(t, Actions{}, songsOf(8, "UP 主"), w, gridH)

	if err := tt.Click("歌 3"); err != nil {
		t.Fatal(err)
	}
	got, ok := g.Selected()
	if !ok || got.Title != "歌 3" {
		t.Fatalf("点了「歌 3」，Selected() = (%q, %v)", got.Title, ok)
	}

	chosen := cardPixel(t, tt, 1100, cols, 2) // 第三格
	other := cardPixel(t, tt, 1100, cols, 3)  // 第四格
	if chosen == other {
		t.Errorf("选中的那一格和没选中的格子是同一个底色（%v），选中根本没画出来", chosen)
	}
}

// TestTheChosenSongSurvivesInsertingOneAtTheHead 钉住网格上的铁律四：在前面插入
// 一首，选中的还是**那一首**，不是原来那一格。
//
// 网格的选中项是自己的字段（框架那一层是行的选中，一个行里有好几首，按不了
// 格子），所以这件事是 `SetSongs` 自己按 ID 重新对位的——不改这里，选中的就是
// 隔壁那一首。
func TestTheChosenSongSurvivesInsertingOneAtTheHead(t *testing.T) {
	songs := songsOf(8, "UP 主")
	g, tt := newGrid(t, Actions{}, songs, 1100, gridH)

	if err := tt.Click("歌 3"); err != nil {
		t.Fatal(err)
	}
	before, ok := g.Selected()
	if !ok {
		t.Fatal("点了「歌 3」却没有选中")
	}

	g.SetSongs(append([]Music{song("new", "新来的", "UP 新")}, songs...))
	frame(tt)

	after, ok := g.Selected()
	if !ok || after.ID != before.ID {
		t.Errorf("头部插入一首之后 Selected() 从 %q 变成了 %q，want 同一首",
			before.Title, after.Title)
	}
}

// TestTheChosenSongGoesAwayWithIt 那一首被删掉之后就没有选中了，不是留在原地
// 指着下一首。这是铁律四的最后一格（列表那边有同款的一条）。
func TestTheChosenSongGoesAwayWithIt(t *testing.T) {
	songs := songsOf(8, "UP 主")
	g, tt := newGrid(t, Actions{}, songs, 1100, gridH)

	if err := tt.Click("歌 3"); err != nil {
		t.Fatal(err)
	}
	if got, ok := g.Selected(); !ok || got.Title != "歌 3" {
		t.Fatalf("点了「歌 3」，Selected() = (%q, %v)", got.Title, ok)
	}

	// 拿掉第三首。
	//
	// ⚠️ 别写 `append(songs[:2], songs[3:]...)`：那会**就地改** songs 背后的
	// 数组，而 `SetSongs` 是直接接管切片不复制的（见它的文档）——改到一半
	// g.songs 指向的那份已经被改坏了，「旧的那一首是谁」就问错了。
	g.SetSongs(slices.Concat(songs[:2], songs[3:]))
	frame(tt)
	if got, ok := g.Selected(); ok {
		t.Errorf("选中的那一首被删掉之后 Selected() = (%q, true)，want 没有选中", got.Title)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 别的几条老规矩
// ─────────────────────────────────────────────────────────────────────────────

// TestThousandsOfSongsOnlyBuildTheOnesInView 验网格也是虚拟的：几千首的首屏只建
// 看得见的那几行。
//
// 判据是**画面上有几个元素**（5000 首全建出来是好几万个文字节点，只建看得见的
// 那几行时是几十个），不是「跑了几毫秒」——那种计时断言在 CI 上必 flaky。
func TestAGridOfThousandsOfSongsOnlyBuildsTheOnesInView(t *testing.T) {
	_, tt := newGrid(t, Actions{}, songsOf(5000, "UP 主"), 1400, gridH)
	if n := len(tt.Texts()); n > 200 {
		t.Errorf("5000 首的首屏画出了 %d 个元素，want ≤ 200（首屏不该建全部行）", n)
	}
	if !songOnTopRow(t, tt, 0) {
		t.Errorf("第一首没画出来，文字有 %q", tt.Texts())
	}
}

// TestAnEmptyGridSaysSo 验空网格有话说，不是一片白板。
func TestAnEmptyGridSaysSo(t *testing.T) {
	_, tt := newGrid(t, Actions{}, nil, 1400, gridH)
	if !tt.HasText("这里还没有歌") {
		t.Errorf("空网格没画空状态，文字有 %q", tt.Texts())
	}
}

// TestACardThatCannotBeDrawnDoesNotTakeTheRestDown 验一格画崩了只废掉那一格，
// 而且崩过的不再重试——**view 里 panic 会穿透 Cocoa 事件循环杀掉整个进程**，
// 这一条钉住的是那个 recover 真的兜得住。
//
// ⚠️ 判据里有一半是「**旁边那几格照画**」：网格一行有六格，兜在行上的话一张坏
// 封面会把同一行里另外五首好的也带走——列表那边一行只有一首，兜在行上就够，
// 网格这边不够，所以兜在格上。
//
// 怎么让一格画崩：用 `NewGrid` 文档里说了「不能这么用」的 nil 缓存。有封面的
// 那一格会在 imgcache.View 里空指针崩掉，没有封面的照画。
func TestACardThatCannotBeDrawnDoesNotTakeTheRestDown(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	g := NewGrid(nil, Actions{})
	g.SetSongs([]Music{
		{ID: "bad1", Title: "画不出来的第一首", Cover: "https://example.invalid/a.png"},
		{ID: "good", Title: "画得出来的一首"},
		{ID: "bad2", Title: "画不出来的第三首", Cover: "https://example.invalid/b.png"},
	})
	tt := ui.NewTester(g.View, 1400, gridH)
	frame(tt)

	if n := strings.Count(strings.Join(tt.Texts(), "|"), "这一格画不出来"); n != 2 {
		t.Errorf("画崩了的两格里有 %d 格画了占位，want 2，文字有 %q", n, tt.Texts())
	}
	if r, ok := tt.Find("画得出来的一首"); !ok || r.W == 0 {
		t.Errorf("一格崩了把同一行里别的格也带走了，文字有 %q", tt.Texts())
	}
	after := strings.Count(buf.String(), "网格格画崩了")
	if after != 2 {
		t.Errorf("崩了 %d 次，want 2（日志是 %q）", after, buf.String())
	}

	// 连着再画四帧：崩过的那两格就该记下来不再崩（每格每次都带一整份栈，
	// 打到日志里看着像崩了，打满了帧率也受不了）。
	frame(tt)
	frame(tt)
	if got := strings.Count(buf.String(), "网格格画崩了"); got != after {
		t.Errorf("再画四帧之后崩了 %d 次，第一次是 %d 次：崩过的格每帧都在重试", got, after)
	}
}

// cardPixel 是第 i 格（0 起，同一行内）的底色。
//
// ⚠️ 取的是**格子最下面那一条**（离格子下边缘 3 像素），不是封面那一块：封面画
// 在格子上面（没有封面时画的是一个底色方块），盖住的是格子自己的底色；格子底下
// 那条 8 像素的上边距里什么都没有，采到的就是这一格刷的底色本身。采文字附近的
// 话会采到抗锯齿的文字边缘，换个字体就飘了。
func cardPixel(t *testing.T, tt *ui.Tester, w, cols, i int) color.RGBA {
	t.Helper()
	first, ok := tt.Find(titleOf(0))
	if !ok {
		t.Fatalf("画面上找不到第一首歌，文字有 %q", tt.Texts())
	}
	cell := cellWidth(float32(w), cols)
	x := int(first.X + float32(i)*(cell+gridGap) + 10)
	y := int(first.Y + first.H - 3)
	if y < 0 || y >= gridH || x < 0 || x >= w {
		t.Fatalf("第 %d 格采样的位置 (%d, %d) 在画面外", i, x, y)
	}
	return tt.Image().RGBAAt(x, y)
}
