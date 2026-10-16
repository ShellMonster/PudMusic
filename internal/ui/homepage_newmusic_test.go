package ui

import (
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
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
		_, tt := newGridTester(t, list.Actions{}, homeSongsOf("新歌", 200), c.w, 900)
		if got := cardsOnPage(t, tt, "新歌"); got != c.cols*newMusicRows {
			t.Errorf("容器宽 %d 时这一页画着 %d 张卡片，want %d 列 × %d 行",
				c.w, got, c.cols, newMusicRows)
		}
	}
}

// TestAShortWindowShowsOneRowSoTheListStillHasRoom 验「窗口矮了就只摆一行」。
//
// 上游永远是两行（`new-music-top.tsx:157` 的 `colCount * 2`），因为上游那一整页在
// 一个滚动容器里，滚下去就看到了。我们不行——「流行」那一档的推荐列表是个虚拟
// 列表，它自己占着滚动，整页再套一层 Scroll 的话那个列表就没有确定的视口高度。
//
// 不减行数的后果是量出来的：窗口 560 高（`WindowOptions` 的 `MinHeight`）时，
// 两行卡片 + 顶栏 + Tab 那一排 + 播放条把那一屏占满，**「流行」那一档的列表是
// 0×0，一首歌都看不见**（见 newMusicTwoRowsMinH 上面那三段）。
//
// 两条判据：**卡片是整行数**（只有一行，所以张数等于列数），以及**那一档的列表
// 画得出至少一行**——后者才是这条要救的东西。
func TestAShortWindowShowsOneRowSoTheListStillHasRoom(t *testing.T) {
	s, tt := newHome(t, fullSongs())

	for _, h := range []int{560, 640} { // 都在 newMusicTwoRowsMinH 之下
		tt.SetSize(homeW, h)
		homeFrame(tt)
		clickTab(t, tt, homePopTab)

		cols := newMusicColsFor(s.home.newGrid.width)
		if got := cardsOnPage(t, tt, "新歌"); got != cols {
			t.Errorf("窗口高 %d 时这一页画着 %d 张卡片，want 一行（%d 列）",
				h, got, cols)
		}
		if rows := songsDrawn(t, tt, homeTabs[homePopTab]); rows == 0 {
			t.Errorf("窗口高 %d 时「%s」那一档的列表一首歌都画不出来",
				h, homeTabs[homePopTab])
		}
	}

	// 回到默认尺寸：两行回来了。**这时候那一块已经在画了**（循环里切到「流行」
	// 之后一直没切走），所以这一段断的是「改窗口高度之后行数跟着换」。
	tt.SetSize(homeW, homeH)
	homeFrame(tt)
	if got := cardsOnPage(t, tt, "新歌"); got == 0 || got%newMusicRows != 0 {
		t.Errorf("窗口高 %d 时这一页画着 %d 张卡片，want 两行（整倍数）", homeH, got)
	}
}

// TestResizingTheWindowChangesTheRowCount 钉住「改窗口高度，行数跟着换」。
//
// ⚠️ 这是一条**单独**的用例，不能和上面那条合：上面那条是「先改窗口、再切到
// 流行」，行数在那一块**第一次被画**的时候就是对的（那一帧现读 `c.Size()`）。
// 这一条断的是另一种：那一块早就在画了，用户这时才拖窗口边。
//
// 实现上把窗口高记进字段（`windowH`）的话这里立刻红——记的是 800，用户拖到 560
// 之后还是两行，下面那档的列表被挤成 0×0。这个坑真踩过，所以单开一条。
func TestResizingTheWindowChangesTheRowCount(t *testing.T) {
	s, tt := newHome(t, fullSongs())
	clickTab(t, tt, homePopTab) // 先让这一块开始画

	cols := newMusicColsFor(s.home.newGrid.width)
	if got := cardsOnPage(t, tt, "新歌"); got != cols*newMusicRows {
		t.Fatalf("拖之前这一页画着 %d 张卡片，want %d 列 × %d 行",
			got, cols, newMusicRows)
	}

	// 往下拖：矮到放不下两行。
	tt.SetSize(homeW, 560)
	homeFrame(tt)
	if got := cardsOnPage(t, tt, "新歌"); got != cols {
		t.Errorf("拖矮之后这一页画着 %d 张卡片，want 一行（%d 列）", got, cols)
	}
	if rows := songsDrawn(t, tt, homeTabs[homePopTab]); rows == 0 {
		t.Errorf("拖矮之后「%s」那一档的列表一首歌都画不出来", homeTabs[homePopTab])
	}

	// 再拖回来：两行回来了。
	tt.SetSize(homeW, homeH)
	homeFrame(tt)
	if got := cardsOnPage(t, tt, "新歌"); got != cols*newMusicRows {
		t.Errorf("拖回来之后这一页画着 %d 张卡片，want %d 列 × %d 行",
			got, cols, newMusicRows)
	}
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
	g, tt := newGridTester(t, list.Actions{}, homeSongsOf("新歌", 40), 800, 900)

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
// 高是 newMusicCoverH），歌名在封面下面，UP 名在歌名下面。封面走 `imgcache`，
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

	// 封面那一块：卡片的高 = 封面（newMusicCoverH）+ 两道缝 + 歌名 + UP 名。
	// 量的是**整张卡片的高度**而不是去 `Find` 封面——空地址的封面画的是一个空
	// 框，它没有名字，Tester 找不着（imgcache/view.go 的 drawCover 那一支）。
	// 上下各留一点余量：字体那一行的高度会跟着平台走，写死一个区间判「封面在
	// 上面、占大头」而不是判到像素。
	if h := card.H; h < newMusicCoverH || h > newMusicCoverH+70 {
		t.Errorf("卡片高 %.0f，want 封面 %d 加两行字上下那点（%.0f~%.0f）",
			h, newMusicCoverH, float32(newMusicCoverH), float32(newMusicCoverH+70))
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
