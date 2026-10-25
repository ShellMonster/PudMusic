package ui

import (
	"image/color"
	"os"
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/hover"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/egoist/mygo/ui"
)

// 这一组单独对着**新歌速递那一块**跑（不经过首页那一屏）：断点、翻页、行身份、
// 空状态、点一下放队列。走的是 MyGo 的 Tester，布局和点击是真的。
//
// ⚠️ 无头窗口只画这一块，所以**窗口宽就是容器宽**（首页那边窗口左边还有侧栏，
// 容器窄一截，那几条在 homepage_test.go 里）。断点用例就是靠这一点才量得出
// 「容器到某个宽就换几列」。

// newGridTester 建一块新歌速递并把歌给它，返回那一块和画着它的 Tester。
func newGridTester(t *testing.T, acts list.Actions, songs []list.Music, w, h int) (*newMusicGrid, *ui.Tester) {
	t.Helper()
	g := newNewMusic(homeTestCache(t), acts, func() {})
	g.SetSongs(songs)
	tt := ui.NewTester(g.View, w, h)
	homeFrame(tt)
	return g, tt
}

// cardsOnPage 数**第一页**画着几张卡片：从第 1 首往后数，数到第一张不在画面上
// 为止（画出来的是「那一页那一批」，所以数的是从头连着的那几首）。
func cardsOnPage(t *testing.T, tt *ui.Tester, prefix string) int {
	return cardsFrom(t, tt, prefix, 1)
}

// cardsFrom 数从第 first 首起往后连着画着几张。翻到别的页时用这个——第二页上没有
// 第一页那批歌，`cardsOnPage` 在那儿数出来是 0。
func cardsFrom(t *testing.T, tt *ui.Tester, prefix string, first int) int {
	t.Helper()
	n := 0
	for i := first; i < first+200; i++ {
		r, ok := tt.Find(songName(prefix, i))
		if !ok || r.W == 0 || r.H == 0 {
			break
		}
		n++
	}
	return n
}

// songsDrawn 数那一档的列表画着几行：从第一首往后数，数到不在画面上为止。
func songsDrawn(t *testing.T, tt *ui.Tester, prefix string) int {
	t.Helper()
	n := 0
	for i := 1; i <= 200; i++ {
		r, ok := tt.Find(songName(prefix, i))
		if !ok || r.W == 0 || r.H == 0 {
			break
		}
		n++
	}
	return n
}

// readout 是「几 / 几」那个读数。找不到是空串（那一块没画翻页，就是空着）。
func readout(tt *ui.Tester) string {
	for _, s := range tt.Texts() {
		if strings.Contains(s, " / ") {
			return s
		}
	}
	return ""
}

// ─────────────────────────────────────────────────────────────────────────────
// 断点
// ─────────────────────────────────────────────────────────────────────────────

// TestNewMusicColumnsFollowItsOwnBreakpoints 验新歌速递的分列。
//
// ⚠️ 这些数字是**新歌速递自己那张表**（上游 `new-music-top.tsx:135-141` 的
// `computeColsByBreakpoint`）：1280→6、1024→5、768→4、640→3、比 640 窄→2。
// **它和分页网格那张不是同一张**（那张是 1536→6、1280→5、1024→4、768→3、比 768
// 窄→2，见 widgets/list/grid.go）——抄串了的话用户在一个窗口宽度下看到的列数和
// Biu 不一致，而这正是「照 Biu 扒」这件事要的判据。
//
// 每个断点上下各钉一个宽度：断点本身和断点上方 1 像素是「刚好够到下一档」和
// 「还没够到」的分界，判成 `>=` 还是 `>` 写错了只有这一条看得出来。
//
// 判据是**这一页画着几张卡片**（列数 × 行数），不是「调了哪个函数」。
func TestNewMusicColumnsFollowItsOwnBreakpoints(t *testing.T) {
	for _, c := range []struct {
		w, cols int
	}{
		{500, 2}, {639, 2}, // 比 640 窄：2 列
		{640, 3}, {767, 3}, // 640~767：3 列
		{768, 4}, {1000, 4}, {1023, 4}, // 768~1023：4 列
		{1024, 5}, {1279, 5}, // 1024~1279：5 列
		{1280, 6}, {1600, 6}, // 1280 以上：6 列
	} {
		// 歌给足：每页两行，列数是「这一页几张」除以两行。
		//
		// ⚠️ 窗口给到 1200 高：封面是方的之后，两行有多高跟着格子宽走，最宽那一
		// 档（639 宽只有 2 列、格子 313）要 1143 高才摆得下两行。这一条验的是
		// **列数**，不是行数——行数不够会让它错报成「少了一列」。
		_, tt := newGridTester(t, list.Actions{}, homeSongsOf("新歌", 200), c.w, 1200)
		if got := cardsOnPage(t, tt, "新歌"); got != c.cols*newMusicRows {
			t.Errorf("容器宽 %d 时这一页画着 %d 张卡片，want %d 列 × %d 行",
				c.w, got, c.cols, newMusicRows)
		}
	}
}

// homeTallH 是「两行摆得下」的一个窗口高。
//
// 封面是方的之后，两行的门槛跟着格子宽走（见 newMusicTwoRowsMinH）；1200 宽、
// 容器 ~960、4 列、格子 231 时要 978 高。1000 刚够，这里取 1000 而不是贴着门槛，
// 留一点余量给字体行高那点平台差。
const homeTallH = 1000

// TestAShortWindowShowsOneRowSoTheListStillHasRoom 验「窗口矮了就只摆一行」。
//
// 上游永远是两行（`new-music-top.tsx:157` 的 `colCount * 2`），因为上游那一整页在
// 一个滚动容器里，滚下去就看到了。我们不行——「流行」那一档的推荐列表是个虚拟
// 列表，它自己占着滚动，整页再套一层 Scroll 的话那个列表就没有确定的视口高度。
//
// 不减行数的后果是量出来的：窗口 560 高（`WindowOptions` 的 `MinHeight`）时，
// 两行卡片 + 顶栏 + Tab 那一排 + 播放条把那一屏占满，**「流行」那一档的列表是
// 0×0，一首歌都看不见**（见 newMusicTwoRowsMinH）。
//
// 判据是**这一页画着几张卡片**（列数 × 应摆的行数），以及**那一档的第一行是完整的
// 64 高**——后者才是这条要救的东西。只看「列表里还有行」不够：视口比一行矮的时候
// 那一行仍然画得出来，只是被裁掉了大半，而 `songsDrawn>0` 照样是绿的（评审在
// 1200×640 / 560 / 930 上都量到过 25 / 16 高的一行）。
//
// 窗口高取这几档：最短的两档、默认、**两行门槛的两侧**（threshold−1 与 threshold）、
// 以及更高的一档。门槛两侧是这一条最容易漏的地方：刚切到两行的那个高度上，列表
// 最容易只剩一条缝。
func TestAShortWindowShowsOneRowSoTheListStillHasRoom(t *testing.T) {
	s, tt := newHome(t, fullSongs())
	clickTab(t, tt, homePopTab) // 先让这一块开始画，好量出容器宽

	cols := newMusicColsFor(s.home.newGrid.width)
	cell := newMusicCellWidth(s.home.newGrid.width, cols)
	threshold := int(newMusicTwoRowsMinH(cell))

	for _, h := range []int{560, 640, homeH, threshold - 1, threshold, homeTallH} {
		tt.SetSize(homeW, h)
		homeFrame(tt)

		rows := newMusicRowsAt(float32(h), cell)
		if got := cardsOnPage(t, tt, "新歌"); got != cols*rows {
			t.Errorf("窗口高 %d 时这一页画着 %d 张卡片，want %d 列 × %d 行",
				h, got, cols, rows)
		}
		assertFirstListRowFull(t, tt, h)
	}
}

// assertFirstListRowFull 断言下面那一档的第一行**没有被视口裁掉**。
//
// 一行的高是 64（`list.listRowHeight`）。被裁的时候 `Find` 仍然找得到它、宽也不是
// 0，只有 H 变小——所以判据只能是高度，不能是「在不在」。
func assertFirstListRowFull(t *testing.T, tt *ui.Tester, h int) {
	t.Helper()
	r, ok := tt.Find(songName(homeTabs[homePopTab], 1))
	if !ok || r.W == 0 {
		t.Fatalf("窗口高 %d 时列表第一行没画出来，文字有 %q", h, tt.Texts())
	}
	if r.H < 63 {
		t.Errorf("窗口高 %d 时列表第一行只有 %.1f 高（完整一行是 64）——被新歌速递那一块挤到视口外裁掉了", h, r.H)
	}
}

// TestResizingTheWindowChangesTheRowCount 钉住「改窗口高度，行数跟着换」。
//
// ⚠️ 这是一条**单独**的用例，不能和上面那条合：上面那条是「先切到流行、再改
// 窗口」，行数在那一块**第一次被画**的时候就是对的（那一帧现读 `c.Size()`）。
// 这一条断的是另一种：那一块早就在画了，用户这时才拖窗口边。
//
// 实现上把窗口高记进字段（`windowH`）的话这里立刻红——记的是 1000，用户拖到 560
// 之后还是两行，下面那档的列表被挤成 0×0。这个坑真踩过，所以单开一条。
func TestResizingTheWindowChangesTheRowCount(t *testing.T) {
	s, tt := newHome(t, fullSongs())
	tt.SetSize(homeW, homeTallH)
	homeFrame(tt)
	clickTab(t, tt, homePopTab) // 先让这一块开始画

	cols := newMusicColsFor(s.home.newGrid.width)
	if got := cardsOnPage(t, tt, "新歌"); got != cols*newMusicRows {
		t.Fatalf("拖之前这一页画着 %d 张卡片，want %d 列 × %d 行",
			got, cols, newMusicRows)
	}
	assertFirstListRowFull(t, tt, homeTallH)

	// 往下拖：矮到放不下两行，列表的第一行还得是完整的。
	tt.SetSize(homeW, 560)
	homeFrame(tt)
	if got := cardsOnPage(t, tt, "新歌"); got != cols {
		t.Errorf("拖矮之后这一页画着 %d 张卡片，want 一行（%d 列）", got, cols)
	}
	assertFirstListRowFull(t, tt, 560)

	// 再拖回去：两行回来了，列表照样完整。
	tt.SetSize(homeW, homeTallH)
	homeFrame(tt)
	if got := cardsOnPage(t, tt, "新歌"); got != cols*newMusicRows {
		t.Errorf("拖回来之后这一页画着 %d 张卡片，want %d 列 × %d 行",
			got, cols, newMusicRows)
	}
	assertFirstListRowFull(t, tt, homeTallH)
}

// ─────────────────────────────────────────────────────────────────────────────
// 翻页
// ─────────────────────────────────────────────────────────────────────────────

// TestNewMusicPagesSideways 验左右翻页：点「下一页」换一批歌、读数跟着变、到了
// 最后一页那个按钮就按不动了（上游 `:183-199` 的两个 `isDisabled`）。
//
// 顺带钉住「每一页画的是**那一页**的歌」而不只是「画的东西变了」：第二页摆的
// 头一首就是第 9 首，翻回去又是第一页那批。
func TestNewMusicPagesSideways(t *testing.T) {
	const size = 8 // 容器 800 → 4 列，每页 4 × 2 = 8 首
	g, tt := newGridTester(t, list.Actions{}, homeSongsOf("新歌", 20), 800, 900)

	if got, want := readout(tt), "1 / 3"; got != want {
		t.Fatalf("第一页的读数是 %q，want %q", got, want)
	}
	if got := cardsOnPage(t, tt, "新歌"); got != size {
		t.Errorf("第一页画着 %d 张，want %d", got, size)
	}

	if err := tt.Click("下一页"); err != nil {
		t.Fatal(err)
	}
	homeFrame(tt)
	if got, want := readout(tt), "2 / 3"; got != want {
		t.Errorf("翻到第二页读数是 %q，want %q", got, want)
	}
	if r, ok := tt.Find(songName("新歌", size+1)); !ok || r.W == 0 {
		t.Errorf("第二页的头一首不是第 %d 首，文字有 %q", size+1, tt.Texts())
	}
	if _, ok := tt.Find(songName("新歌", 1)); ok {
		t.Errorf("第二页上还画着第一页的歌，文字有 %q", tt.Texts())
	}

	// 一路往前翻，翻到最后一页就该停在那一页。
	for range 5 {
		if err := tt.Click("下一页"); err != nil {
			t.Fatal(err)
		}
		homeFrame(tt)
	}
	if got, want := readout(tt), "3 / 3"; got != want {
		t.Errorf("翻到最后读数是 %q，want %q（按不动的按钮不该把页码推过去）", got, want)
	}
	if g.page != 3 {
		t.Errorf("停在第 %d 页，want 3", g.page)
	}
	// 第三页只有 4 首（20 − 16），剩下的格子空着，不画也不占位。
	if got := cardsFrom(t, tt, "新歌", 2*size+1); got != 4 {
		t.Errorf("最后一页画着 %d 张，want 4（20 首，前两页各 8）", got)
	}

	// 一路翻回来。
	for range 5 {
		if err := tt.Click("上一页"); err != nil {
			t.Fatal(err)
		}
		homeFrame(tt)
	}
	if got, want := readout(tt), "1 / 3"; got != want {
		t.Errorf("翻回第一页读数是 %q，want %q", got, want)
	}
	if g.page != 1 {
		t.Errorf("翻回来停在第 %d 页，want 1", g.page)
	}
}

// TestShrinkingTheSongsBringsThePageBack 验换歌之后**页码夹回还存在的范围内**。
//
// 停在第 4 页，然后换成一页都放不下的那么少：留着旧页码的话用户看到的是一个空
// 网格加两个转不动的箭头（上游也是这么处理的，`new-music-top.tsx:160-165` 那个
// effect 干的同一件事）。
func TestShrinkingTheSongsBringsThePageBack(t *testing.T) {
	// 窗口给 1000 高：这一条要按「每页 8 首」翻到第 4 页（方封面两行）。
	g, tt := newGridTester(t, list.Actions{}, homeSongsOf("新歌", 40), 800, 1000)

	for range 3 {
		if err := tt.Click("下一页"); err != nil {
			t.Fatal(err)
		}
		homeFrame(tt)
	}
	if g.page != 4 {
		t.Fatalf("停在第 %d 页，want 4", g.page)
	}

	g.SetSongs(homeSongsOf("新歌", 1))
	homeFrame(tt)
	if g.page != 1 {
		t.Errorf("换成一首之后停在第 %d 页，want 1", g.page)
	}
	if got, want := readout(tt), "1 / 1"; got != want {
		t.Errorf("读数是 %q，want %q", got, want)
	}
}

// TestNoSongsSaysSoAndHidesThePager 验空状态：照上游那句「暂无数据」，并且**整个
// 翻页那一块都不画**。
//
// ⚠️ 上游空的时候画的是「1 / 0」加两个转不动的箭头（`new-music-top.tsx:181-199`
// 只在 `newLoading` 的时候藏）。空网格上摆一个 1 / 0，用户只会以为界面坏了，所以
// 这里藏掉——这一条是本项目自己的取舍，不是照抄，出处写在 viewHeader 上。
func TestNoSongsSaysSoAndHidesThePager(t *testing.T) {
	_, tt := newGridTester(t, list.Actions{}, nil, 800, 900)

	if !shows(tt, "暂无数据") {
		t.Errorf("没有画出「暂无数据」，文字有 %q", tt.Texts())
	}
	if !shows(tt, "新歌速递") {
		t.Errorf("连标题都没画，文字有 %q", tt.Texts())
	}
	if readout(tt) != "" {
		t.Errorf("一首都没有却画了翻页读数 %q", readout(tt))
	}
	if shows(tt, "上一页") || shows(tt, "下一页") {
		t.Errorf("一首都没有却画了翻页按钮，文字有 %q", tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 行身份（铁律四的第 4 条）
// ─────────────────────────────────────────────────────────────────────────────

// TestInsertingAtTheHeadKeepsTheFocusOnTheSameSong 钉住卡片按**ID** 给身份。
//
// 默认的身份是「父 ID + 兄弟序号」（`ui/element.go` 的 Key：「identifies the
// element among its siblings by k instead of by its position」）。新歌速递换歌时新
// 的一首会插在**头上**（上游按发布时间排），那样后面所有卡片的身份就整体错配了
// ——这里的表现是：用户 Tab 到第三张，刷新一下，焦点跑到**别的**那一张上去了。
//
// 判据是**焦点**：焦点是这一组里唯一一个「跟着这一格走」的可见状态（上游那张
// 卡片是 `role="button" tabIndex={0}`，我们照它做成可聚焦的，见 viewCard）。
func TestInsertingAtTheHeadKeepsTheFocusOnTheSameSong(t *testing.T) {
	g, tt := newGridTester(t, list.Actions{}, homeSongsOf("新歌", 8), 800, 900)

	// Tab 到第三张卡片上。
	focused := false
	for range 20 {
		if tt.Focused(songName("新歌", 3)) {
			focused = true
			break
		}
		tt.Key(0, ui.KeyTab)
		homeFrame(tt)
	}
	if !focused {
		t.Fatalf("Tab 了 20 下焦点还没到第三张卡片上")
	}

	// 头上插一首：按 ID 给身份的话，焦点跟着那一首走，落在第三**格**上。
	g.SetSongs(append(homeSongsOf("新歌头", 1), homeSongsOf("新歌", 8)...))
	homeFrame(tt)

	if !tt.Focused(songName("新歌", 3)) {
		t.Errorf("头上插一首之后焦点不在 %q 上了", songName("新歌", 3))
	}
	if tt.Focused(songName("新歌", 4)) {
		t.Errorf("焦点跑到位置上那一张（%q）去了：卡片是按第几格认身份的",
			songName("新歌", 4))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 卡片上画的东西
// ─────────────────────────────────────────────────────────────────────────────

// TestANewMusicCardShowsTheCoverTheTitleAndTheOwner 是这一票的第三条的后一半：
// 卡片显示**封面、歌名、UP 名**。
//
// 判据是画面上的东西和它们的**确切位置**：封面是卡片最上面那一块（宽度撑满一格、
// **宽高相等**），歌名在封面下面，UP 名在歌名下面。封面走 `imgcache`，
// 它的四态（空地址 / 载入中 / 就绪 / 失败）是那边的事
// （widgets/imgcache/view_test.go 验过），这里只验接进来之后画对了地方。
func TestANewMusicCardShowsTheCoverTheTitleAndTheOwner(t *testing.T) {
	_, tt := newGridTester(t, list.Actions{}, []list.Music{{
		ID:    "n1",
		Title: "夜空中最亮的星",
		Owner: "逃跑计划",
	}}, 800, 900)

	card, ok := tt.Find("夜空中最亮的星")
	if !ok || card.W == 0 {
		t.Fatalf("歌名没画出来，文字有 %q", tt.Texts())
	}
	owner, ok := tt.Find("逃跑计划")
	if !ok || owner.W == 0 {
		t.Fatalf("UP 名没画出来，文字有 %q", tt.Texts())
	}
	if owner.X != card.X {
		t.Errorf("UP 名在 x=%.0f、卡片在 x=%.0f：它没有和卡片左边对齐", owner.X, card.X)
	}
	if owner.Y <= card.Y {
		t.Errorf("UP 名在 y=%.0f、卡片在 y=%.0f：它没有在卡片下面", owner.Y, card.Y)
	}

	// 封面是方的：封面那一块的宽高都是格子的宽。
	//
	// 量法是**从 hover 时的播放钮倒推封面那两条边**：播放钮贴着封面右下角，离
	// 右边和下边各 `btnInset`（8，上游 `music-card/index.tsx:39-47`）。量整张
	// 卡片的高不行——它还包括下面两行字，不是封面。
	side := newMusicCellWidth(800, newMusicColsFor(800))
	hoverRow(t, tt, "夜空中最亮的星")
	btn, ok := tt.Find(hover.PlayLabel("夜空中最亮的星"))
	if !ok {
		t.Fatalf("hover 之后封面上没有播放钮：%q", tt.Texts())
	}
	coverW := btn.X + btn.W + 8 - card.X
	coverH := btn.Y + btn.H + 8 - card.Y
	if coverW < side-1 || coverW > side+1 {
		t.Errorf("封面宽 %.1f，want 格子的宽 %v——它没有占满一格", coverW, side)
	}
	if coverH < coverW-1 || coverH > coverW+1 {
		t.Errorf("封面 %.1f 宽、%.1f 高——不是方的（宽高该相等）", coverW, coverH)
	}

	// 卡片的高 = 封面（格子的宽）+ 两道缝 + 歌名 + UP 名。
	// 量的是**整张卡片的高度**而不是去 `Find` 封面——空地址的封面画的是一个空
	// 框，它没有名字，Tester 找不着（imgcache/view.go 的 drawCover 那一支）。
	// 上下各留一点余量：字体那一行的高度会跟着平台走，写死一个区间判「封面在
	// 上面、占大头」而不是判到像素。
	if h := card.H; h < side || h > side+90 {
		t.Errorf("卡片高 %.0f，want 封面 %v 加两行字上下那点（%v~%v）",
			h, side, side, side+90)
	}
}

// TestTheCoverStaysSquareAtTheMinimumWindowHeight 钉住最小窗口那一档的封面。
//
// 1200×560（`WindowOptions` 的 `MinHeight`）时，整宽 231 的封面 + 顶栏 / Tab /
// 播放条会把下面的列表挤成 0×0，所以 `newMusicCoverSide` 把它压到 108。**压小之后
// 它必须还是方的**——票面的判据是「宽高相等」，不是「一定占满格子」。
func TestTheCoverStaysSquareAtTheMinimumWindowHeight(t *testing.T) {
	_, tt := newHome(t, fullSongs())
	tt.SetSize(homeW, 560)
	homeFrame(tt)
	clickTab(t, tt, homePopTab)
	homeFrame(tt)

	title := songName("新歌", 1)
	card, ok := tt.Find(title)
	if !ok || card.W == 0 {
		t.Fatalf("最小窗口下找不到第一张卡片，文字有 %q", tt.Texts())
	}
	hoverRow(t, tt, title)
	btn, ok := tt.Find(hover.PlayLabel(title))
	if !ok {
		t.Fatalf("hover 之后封面上没有播放钮：%q", tt.Texts())
	}

	coverW := btn.X + btn.W + 8 - card.X
	coverH := btn.Y + btn.H + 8 - card.Y
	if coverW <= 0 || coverH <= 0 {
		t.Fatalf("封面量出来是 %.1f×%.1f——播放钮和卡片不在同一格上", coverW, coverH)
	}
	if coverH < coverW-1 || coverH > coverW+1 {
		t.Errorf("最小窗口下封面 %.1f 宽、%.1f 高——不是方的", coverW, coverH)
	}
}

// TestASongWithoutAnOwnerDoesNotDrawOne 验没有 UP 名时不画空位（Biu 的
// `new-music-top.tsx:253` 的 `{item.author || item.date && …}`）。
func TestASongWithoutAnOwnerDoesNotDrawOne(t *testing.T) {
	_, tt := newGridTester(t, list.Actions{}, []list.Music{{
		ID:    "n1",
		Title: "没有 UP 名的歌",
	}}, 800, 900)

	if !shows(tt, "没有 UP 名的歌") {
		t.Errorf("歌名没画出来，文字有 %q", tt.Texts())
	}
}

// TestClickingANewMusicCardQueuesExactlyThatSong 验点一下那一格会做的事：**放
// 这一首**（上游 `new-music-top.tsx:22-36` 的 `handlePlay` 走
// `usePlayList.play({…})`，换掉整个队列从这一首开始），不是「从它往后」。
//
// 判据是**真的被要求放的那几项**，不是「调了哪个函数」。
func TestClickingANewMusicCardQueuesExactlyThatSong(t *testing.T) {
	var got [][]app.Item
	acts := list.Actions{Play: func(items []app.Item) { got = append(got, items) }}
	_, tt := newGridTester(t, acts, homeSongsOf("新歌", 8), 800, 900)

	if err := tt.Click(songName("新歌", 3)); err != nil {
		t.Fatal(err)
	}
	homeFrame(tt)

	if len(got) != 1 {
		t.Fatalf("点了一下发出了 %d 次播放，want 1 次", len(got))
	}
	if len(got[0]) != 1 {
		t.Fatalf("放进去 %d 首，want 1 首（点的是哪一格就是哪一首，不是从它往后）", len(got[0]))
	}
	if title := got[0][0].Title; title != songName("新歌", 3) {
		t.Errorf("放进去的是 %q，want %q", title, songName("新歌", 3))
	}
}

// TestTheNewMusicHeadingIsTwentyFourPixels 钉住票面那条「标题 24」。
//
// 判据是**行高**：16 号字的行高约 19，24 号约 29——差一号在行高上看得出来，而
// `Tester` 不暴露字号。DESIGN.md 的「页标题 24/400」：上游这一处是整页唯一的
// `<h1>`（`new-music-top.tsx:178`），字号 24、字重 400。
func TestTheNewMusicHeadingIsTwentyFourPixels(t *testing.T) {
	_, tt := newGridTester(t, list.Actions{}, homeSongsOf("新歌", 4), 800, 900)

	head, ok := tt.Find("新歌速递")
	if !ok || head.W == 0 {
		t.Fatalf("标题没画出来，文字有 %q", tt.Texts())
	}
	if head.H < 26 {
		t.Errorf("「新歌速递」的行高 %.1f，want 24 号字那档（约 29；16 号只有 19）",
			head.H)
	}
}

// TestTheNewMusicPagerIsThirtyTwoPixels 钉住票面那条「翻页换 32 图标箭头」。
//
// 判据是**点击区**：两枚都是 32×32。图标本身仍是 16（上游 `RiArrow*SLine` 的
// size 是 16），32 是交给手指和鼠标的那一块。
func TestTheNewMusicPagerIsThirtyTwoPixels(t *testing.T) {
	_, tt := newGridTester(t, list.Actions{}, homeSongsOf("新歌", 20), 800, 900)

	for _, label := range []string{"上一页", "下一页"} {
		r, ok := tt.Find(label)
		if !ok || r.W == 0 {
			t.Fatalf("翻页键 %q 没画出来，文字有 %q", label, tt.Texts())
		}
		if r.W < newMusicArrowW-0.5 || r.H < newMusicArrowW-0.5 {
			t.Errorf("翻页键 %q 是 %.1f×%.1f，want %d×%d",
				label, r.W, r.H, newMusicArrowW, newMusicArrowW)
		}
	}
}

// TestHoveringANewMusicCardShowsThePlayButtonTheGradientAndTheAccentTitle 是票面
// 最后一条：**hover 后播放钮与渐变出现在画面上，标题变主色**。
//
// 三样分开量，因为它们是三件独立会坏的事：
//
//   - 播放钮：`Tester.Find` 认标签（`hover.PlayLabel`），指针不在时找不到；
//   - 渐变：**逐像素**——封面底部那一点在 hover 之后明显变暗（black/90 压上来）；
//   - 标题：**逐像素**——离 `ink.AccentOn` 最近的那一笔，在 hover 之后明显更近。
//
// ⚠️ 推钟要按 AGENTS §2：`hoverRow` 用 `Advance(hover.Fade)`，不数帧。
func TestHoveringANewMusicCardShowsThePlayButtonTheGradientAndTheAccentTitle(t *testing.T) {
	th := ui.LightTheme()
	th.Accent = ui.Hex("#1ed760")

	songs := homeSongsOf("新歌", 8)
	g := newNewMusic(homeTestCache(t), list.Actions{}, func() {})
	g.SetSongs(songs)
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(th)
		g.View(c)
	}, 800, 900)
	homeFrame(tt)

	title := songName("新歌", 3)
	card, ok := tt.Find(title)
	if !ok || card.W == 0 {
		t.Fatalf("画面上找不到第三张卡片，文字有 %q", tt.Texts())
	}
	if _, ok := tt.Find(hover.PlayLabel(title)); ok {
		t.Fatalf("指针还没移上去，卡片上就有播放钮了：%q", tt.Texts())
	}

	side := newMusicCoverSide(900, newMusicCellWidth(800, newMusicColsFor(800)))
	// 封面底边往上留一点，避开圆角；横向取中间，避开右下角那个播放钮。
	px := int(card.X + side/2)
	py := int(card.Y + side - 4)
	idle := tt.Image().RGBAAt(px, py)

	// 标题那一块：卡片从封面往下 6 是歌名。量的不是 `Find` 回来的那个盒子——
	// 那个是整张卡片（Label 挂在 card 上），会把封面也算进去。
	titleRect := ui.Rect{X: card.X, Y: card.Y + side + 6, W: card.W, H: 26}
	want := ink.AccentOn(th)
	target := color.RGBA{R: want.R, G: want.G, B: want.B, A: 255}
	before := nearestPixel(tt.Image(), titleRect, want)

	hoverRow(t, tt, title)

	if _, ok := tt.Find(hover.PlayLabel(title)); !ok {
		t.Fatalf("hover 之后封面上没有播放钮：%q", tt.Texts())
	}
	if lit := tt.Image().RGBAAt(px, py); luminanceRGBA(lit) >= luminanceRGBA(idle)-20 {
		t.Errorf("封面底部那一点 hover 前后是 %v → %v：渐变没有压上来（black/90 该把它压暗一大截）",
			idle, lit)
	}
	after := nearestPixel(tt.Image(), titleRect, want)
	if colorDist(after, target) >= colorDist(before, target) {
		t.Errorf("hover 之后歌名离主色 %v 反倒更远了（%v → %v）——标题没有变主色",
			target, before, after)
	}
	if d := colorDist(after, target); d > 60 {
		t.Errorf("hover 之后歌名上离主色最近的像素是 %v（差 %d），want 明显是主色",
			after, d)
	}
}

// TestClickingTheHoverPlayButtonQueuesExactlyThatSong 钉住 hover 播放钮的语义：
// 和点卡片一样，**只放这一首**（不是网格卡片那枚「从这一首往后接」）。
func TestClickingTheHoverPlayButtonQueuesExactlyThatSong(t *testing.T) {
	var got [][]app.Item
	acts := list.Actions{Play: func(items []app.Item) { got = append(got, items) }}
	songs := homeSongsOf("新歌", 8)
	_, tt := newGridTester(t, acts, songs, 800, 900)

	hoverRow(t, tt, songs[2].Title)
	if err := tt.Click(hover.PlayLabel(songs[2].Title)); err != nil {
		t.Fatalf("点不了 hover 播放钮：%v（画面：%q）", err, tt.Texts())
	}
	homeFrame(tt)

	if len(got) != 1 {
		t.Fatalf("点了一下发出了 %d 次播放，want 1 次（播放钮和卡片单击不是两下）", len(got))
	}
	if len(got[0]) != 1 || got[0][0].Title != songs[2].Title {
		t.Errorf("放进去的是 %+v，want 只有 %q 这一首", got[0], songs[2].Title)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 出图
// ─────────────────────────────────────────────────────────────────────────────

// TestRenderNewMusicShots 把新歌速递平时和 hover 两种状态存成 PNG，深浅各一套。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderNewMusicShots
//
// ⚠️ **深浅两套都要出**（DESIGN.md §八）：深色下那条 black/90 渐变和主色播放钮
// 压在同色卡片上，差距比浅色下更明显。
//
// ⚠️ **一个字都不许打真 B 站接口**：全部是内存夹具 + 无头渲染，和 hover_shot_test.go
// 那一族同一个约定。
func TestRenderNewMusicShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}

	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newTestShell(t, systemIsDark(dark))
			s, tt := b.s, b.tt
			s.home.SetSongs(fullSongs())
			homeFrame(tt)
			clickTab(t, tt, homePopTab)
			homeFrame(tt)

			// 指针停在窗口右下那片空地上：第一张要的是「平时」。
			tt.Move(1180, 770)
			homeFrame(tt)
			save(t, dir, "37-"+name+"-新歌速递-平时.png", tt)

			hoverRow(t, tt, songName("新歌", 2))
			save(t, dir, "38-"+name+"-新歌速递-卡片hover.png", tt)
		})
	}
}

// TestRenderPopTabShots 把「流行」那一屏存成 PNG：顶图在顶上那张，和滚一段之后
// 顶图滚走那张。深浅各一套，外加三个主题方案各看一眼。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderPopTabShots
//
// ⚠️ **深浅两套都要出**（DESIGN.md §八），而**主题方案也要各看一眼**（§零）：
// 那一块的颜色全是主题令牌出来的（没有写死的颜色），所以每一套都要看一眼。
func TestRenderPopTabShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}

	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			b := newTestShell(t, systemIsDark(dark))
			s, tt := b.s, b.tt
			s.home.SetSongs(fullSongs())
			homeFrame(tt)
			clickTab(t, tt, homePopTab)
			homeFrame(tt)

			// 指针停在窗口右下那片空地上：第一张要的是「平时」。
			tt.Move(1180, 770)
			homeFrame(tt)
			save(t, dir, "39-"+name+"-流行-顶图在顶上.png", tt)

			// 滚一段：顶图跟着滚走了（票 #369 的头一条）。
			tt.Scroll(homeW/2, homeH-150, 0, 1200)
			homeFrame(tt)
			save(t, dir, "40-"+name+"-流行-顶图滚走之后.png", tt)
		})
	}

	// 三个主题方案各看一眼（暖纸 / 暮光 / 柔雾）。经典是默认外观，浅色那张就是它。
	for _, preset := range []app.ThemePreset{
		app.ThemePresetWarmPaper, app.ThemePresetDuskAmber, app.ThemePresetSoftPeach,
	} {
		t.Run(string(preset), func(t *testing.T) {
			st := app.DefaultSettings("")
			st.ThemePreset = preset
			st.ThemeMode = app.ThemeModeLight
			b := newTestShell(t, withSettings(st))
			s, tt := b.s, b.tt
			s.home.SetSongs(fullSongs())
			homeFrame(tt)
			clickTab(t, tt, homePopTab)
			homeFrame(tt)
			tt.Move(1180, 770)
			homeFrame(tt)
			save(t, dir, "41-"+string(preset)+"-流行-顶图在顶上.png", tt)
		})
	}
}
