package list

import (
	"image/color"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/egoist/mygo/ui"
)

// 这一组钉的是票 #197 拍板的那件事：**点列表行 / 网格格不再整行刷实心主色底**，
// 只保留「正在播放」那一行的主色高亮（flat，不是实心），而键盘走到的那一行有
// 焦点环。
//
// ⚠️ **判据全是画面上的像素**：主题里那个字段是什么值只是「打算画成什么」，画面上
// 那个像素是什么值才是「用户看见的是什么」。而这一票的三个症状**全都是静默的**：
// 实心主色底去掉与否，主色当字色那一档都还是那样——只有逐像素读才分得出来。

// accentOf 是这一套测试用的主色。
//
// ⚠️ **取的是主题里那个值，不是写死的**：这一组要断的是「那一行有没有被刷成主色
// 底」，而「主色是哪个颜色」是主题的事。写死一个绿的话，换了主色的那一版就测不到了。
func accentOf(t *ui.Theme) ui.Color { return t.Accent }

// isAccent 报这个像素是不是**主色底**。
//
// ⚠️ **判的是「和主色几乎一模一样」**，不是「偏主色」：焦点环是半透明的主色压在行沿
// 上的，它偏主色；而实心主色底是**就是**那个主色。两者必须分得开，所以这里比的是
// 差值（通道差之和 ≤ 24）。
func isAccent(p color.RGBA, accent ui.Color) bool {
	d := func(a, b uint8) int {
		if a > b {
			return int(a) - int(b)
		}
		return int(b) - int(a)
	}
	return d(p.R, accent.R)+d(p.G, accent.G)+d(p.B, accent.B) <= 24
}

// TestClickingARowLeavesNoSolidAccentBlock 钉验收第 1 条：点任意一行，整行不再
// 出现实心主色底。
//
// 判据是**那一行的底色**：点过之后它既不是主色（没有实心主色块），也和旁边没点过
// 的那一行**一模一样**（上游点一行不改变它的样子）。
//
// ⚠️ **两条都要断**：只断「不是主色」的话，「刷成别的颜色」（比如深灰）也能过；
// 而「和隔壁那行一样」这一条才是「点一行不改变它的样子」的意思。
func TestClickingARowLeavesNoSolidAccentBlock(t *testing.T) {
	songs := songsOf(4, "UP")
	l, tt := newList(t, Actions{}, songs)
	th := ui.LightTheme()
	accent := accentOf(th)

	if err := tt.Click("歌 2"); err != nil {
		t.Fatal(err)
	}
	frame(tt)

	chosen := rowPixel(t, tt, "歌 2")
	if isAccent(chosen, accent) {
		t.Errorf("点过的那一行是 %v，是实心主色底——票 #197 要的是点一行不再整行刷绿", chosen)
	}
	// ⚠️ **指针挪到别的一行上再比**：点过之后指针还压在被点的那一行上，那一行
	// 带着**悬停**那一档底色（`t.SurfaceHover`）——那是悬停给的，不是「选中」给的。
	// 拿它和另一行比的话，比的是悬停不是选中。
	hoverRow(t, tt, "歌 3")
	frame(tt)

	chosen = rowPixel(t, tt, "歌 2")
	other := rowPixel(t, tt, "歌 4")
	if chosen != other {
		t.Errorf("点过的那一行是 %v，没点过的那一行是 %v：点一行不该改变它的样子", chosen, other)
	}
	// 状态还是选中的（去掉的是**颜色**，不是选中这件事）。
	got, ok := l.Selected()
	if !ok || got.Title != "歌 2" {
		t.Errorf("Selected() = (%q, %v)，want (歌 2, true)", got.Title, ok)
	}
}

// TestThePlayingRowKeepsTheFlatPrimaryHighlight 钉验收第 2 条：「正在播放」那一行
// 仍有主色高亮，**且是 flat 而不是实心**。
//
// 两半都要断：
//
//   - **有**：那一行的歌名是主色当字（`ink.AccentOn`）——flat 的那一档；
//   - **不是实心**：那一行的底色不是主色（实心主色底就是「有高亮」的另一种画法，
//     只断歌名的话，整行刷绿也照样过）。
//
// ⚠️ **两件事分开测**：歌名的颜色和整行的底色是两回事，合在一起测的话，
// 「刷成实心主色底 + 歌名用主色」这种最常见的错法会被歌名那一半盖过去。
func TestThePlayingRowKeepsTheFlatPrimaryHighlight(t *testing.T) {
	songs := onlineSongs(3)
	rec := &recorded{}
	l := New(newCache(t), playingActions(rec, songs[1].Item.Identity()))
	l.SetSongs(songs)
	th := ui.LightTheme()
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(th)
		l.View(c)
	}, testerW, testerH)
	tt.Frame()

	// ① flat 的那一档：歌名是主色（`ink.AccentOn`）。
	r, ok := tt.Find("在线的歌 B")
	if !ok {
		t.Fatalf("画面上找不到「在线的歌 B」，文字有 %q", tt.Texts())
	}
	want := ink.AccentOn(th)
	text := nearestTo(tt.Image(), r, want)
	if !isAccent(text, want) {
		t.Errorf("正在播放那一行的歌名是 %v，want 主色 %v——flat 的高亮没了", text, want)
	}

	// ② 不是实心：那一行的底色不是主色。
	bg := rowPixel(t, tt, "在线的歌 B")
	if isAccent(bg, accentOf(th)) {
		t.Errorf("正在播放那一行的底色是 %v，是实心主色底——要的是 flat（主色当字）", bg)
	}
}

// TestTheKeyboardRowHasAFocusRing 钉验收第 3 条：键盘走到的那一行有焦点环。
//
// 三层：
//
//  1. **Tab 进列表、按方向键走位**，环落在**走到的**那一行上；
//  2. 环**不在**别的行上（只断「有一处环」的话，环画在整张列表上也能过）；
//  3. **鼠标点一行不算**：焦点不是从键盘来的，那一行什么标记都没有
//     （这正是票 #197 要的样子——上游点一行不改变它的样子）。
func TestTheKeyboardRowHasAFocusRing(t *testing.T) {
	songs := songsOf(3, "UP")
	_, tt := newList(t, Actions{}, songs)

	// Tab 把焦点交给列表（框架给有游标的列表 `Focusable`）。
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	tt.Frame()

	if !ringAt(t, tt, "歌 1") {
		t.Errorf("键盘走到「歌 1」了，那一行上沿没有焦点环")
	}
	if ringAt(t, tt, "歌 2") {
		t.Errorf("「歌 2」那一行上沿有焦点环——环只该在键盘走到的那一行上")
	}

	// 往下再走一行：环跟着走。
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if !ringAt(t, tt, "歌 2") {
		t.Errorf("键盘走到「歌 2」了，那一行上沿没有焦点环")
	}
	if ringAt(t, tt, "歌 1") {
		t.Errorf("「歌 1」那一行上沿还有焦点环——环没有跟着走")
	}
}

// TestClickingARowLeavesNoFocusRing 是上一条的后一半：**鼠标点一行，那一行什么标记
// 都没有**。
//
// ⚠️ **单独一条**：上一条里焦点是从键盘来的（Tab 进来的），这一条里焦点是从指针
// 来的（直接点）。框架分得清这两者（`focusVisible`），而用户看得见的差别就是
// 「点过之后那一行有没有环」。
func TestClickingARowLeavesNoFocusRing(t *testing.T) {
	songs := songsOf(3, "UP")
	_, tt := newList(t, Actions{}, songs)

	if err := tt.Click("歌 2"); err != nil {
		t.Fatal(err)
	}
	frame(tt)

	if ringAt(t, tt, "歌 2") {
		t.Errorf("鼠标点过的那一行上沿有焦点环——指针点一下不该留下焦点标记")
	}
}

// TestClickingACardLeavesNoSolidAccentBlock 是上面那一条的**网格**那一档。
//
// ⚠️ 网格和列表是**两套控件**（`grid.go` 自己画格子），所以列表那条绿在这儿也得
// 单独断一遍——两处各写各的，改漏一边的后果是「列表里不刷了，网格里还刷着」。
func TestClickingACardLeavesNoSolidAccentBlock(t *testing.T) {
	const w, cols = 1100, 4
	g, tt := newGrid(t, Actions{}, songsOf(8, "UP 主"), w, gridH)
	th := ui.LightTheme()
	accent := accentOf(th)

	if err := tt.Click("歌 3"); err != nil {
		t.Fatal(err)
	}
	frame(tt)

	chosen := cardPixel(t, tt, w, cols, 2) // 第三格
	if isAccent(chosen, accent) {
		t.Errorf("点过的那一格是 %v，是实心主色底——票 #197 要的是点一格不再整格刷绿", chosen)
	}
	other := cardPixel(t, tt, w, cols, 3) // 第四格
	if chosen != other {
		t.Errorf("点过的那一格是 %v，没点过的那一格是 %v：点一格不该改变它的样子", chosen, other)
	}
	got, ok := g.Selected()
	if !ok || got.Title != "歌 3" {
		t.Errorf("Selected() = (%q, %v)，want (歌 3, true)", got.Title, ok)
	}
}

// hoverRow 把指针挪到 title 那一行上。
//
// ⚠️ **挪走是必要的**：指针压在哪一行，哪一行就带**悬停**那一档底色
// （`t.SurfaceHover`）。拿悬停着的那一行和另一行比颜色，比的是悬停不是选中。
func hoverRow(t *testing.T, tt *ui.Tester, title string) {
	t.Helper()
	r, ok := tt.Find(title)
	if !ok {
		t.Fatalf("画面上找不到「%s」，文字有 %q", title, tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
}

// ringAt 报那一行的上沿有没有焦点环。
//
// 判据是**那个像素和这一行的底色不是一回事**：焦点环是主题里那个 Focus 色
// （半透明的主色）压在行沿上的，而没有环的那一行上沿就是行自己的底色。
//
// ⚠️ **不看「离主色多近」**：焦点环是半透明的，它离底色比离主色近；而主色换一个
// 色相（绿的、黄的）那一套判据就飘了。这里量的是「和底色差了多少」——和主色是
// 什么颜色无关。
func ringAt(t *testing.T, tt *ui.Tester, title string) bool {
	t.Helper()
	r, ok := tt.Find(title)
	if !ok {
		t.Fatalf("画面上找不到「%s」，文字有 %q", title, tt.Texts())
	}
	x := int(r.X + r.W/2)
	// 环画在行的最上沿那两像素上（`selection.go` 的 rowFocusRing：往里收 3 像素）。
	y := int(r.Y)
	// 这一行自己的底色：往下挪 6 像素，避开环，也还在行的内边距里（8）。
	bg := tt.Image().RGBAAt(x, y+6)
	return tt.Image().RGBAAt(x, y) != bg
}
