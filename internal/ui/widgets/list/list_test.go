package list

import (
	"bytes"
	"fmt"
	"image/color"
	"log"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/egoist/mygo/ui"
)

// 这一组跑的是 MyGo 的 Tester：和真窗口一模一样的布局、点击、滚动和系统菜单，
// 只是画到内存里。所以「几千行只建看得见的那些」「行高定死」「插一行之后
// 选中的还是那一首」都能自动验，不需要人守在窗口前面拖。

// testerW / testerH 是无头窗口的大小。宽度要够宽，序号 / 封面 / 时长那几列才
// 摆得下；高度要够高，摆得下十几行。
const (
	testerW = 520
	testerH = 700
)

// newCache 造一个封面缓存。AddWake 不能省：图下完之后不唤醒，界面会永远停在
// 占位（实测：只投递不唤醒 = 0 帧刷新）。
func newCache(t *testing.T) *imgcache.Cache {
	t.Helper()
	c := imgcache.New(imgcache.Options{})
	c.AddWake(func() {})
	t.Cleanup(c.Close)
	return c
}

// newList 造一个列表并把歌给它，返回列表和画着它的 Tester。
//
// ⚠️ Tester 的尺寸固定：虚拟滚动靠「一屏摆得下几行」这个数判断，换了尺寸上面
// 那几条的判据就得跟着改。
func newList(t *testing.T, acts Actions, songs []Music) (*List, *ui.Tester) {
	t.Helper()
	l := New(newCache(t), acts)
	l.SetSongs(songs)
	tt := ui.NewTester(l.View, testerW, testerH)
	tt.Frame()
	return l, tt
}

// song 是造一行歌。
func song(id, title, owner string) Music {
	return Music{ID: id, Title: title, Owner: owner}
}

// songsOf 造 n 行歌，歌名是「歌 1」到「歌 n」。
func songsOf(n int, owner string) []Music {
	out := make([]Music, n)
	for i := range out {
		out[i] = song("id-"+strconv.Itoa(i), fmt.Sprintf("歌 %d", i+1), owner)
	}
	return out
}

// onScreen 报 title 那一行此刻在不在画面上。
//
// ⚠️ **不能用 `tt.HasText`**：它是**子串**匹配（`ui/headless.go` 的
// `strings.Contains`），「歌 1」在「歌 10」「歌 1000」里都找得到——歌单一长，
// 这些断言就永远为真，等于没写。`tt.Find` 是**全等**匹配（`n.text == s`），
// 而且优先命中行的 Label（我们在 ListState.Label 里装的就是歌名），拿到的
// 正好是**整行**的盒子。
func onScreen(tt *ui.Tester, title string) bool {
	_, ok := tt.Find(title)
	return ok
}

// rowPixel 是某一行的底色。
//
// ⚠️ 取的是**行最左边那一小条**（x=4），不是歌名那一块：x=4 落在行的左内边距
// 里（内边距 8），上下都不挨着字，采到的就是这一行刷的底色本身。采歌名附近的
// 话会采到抗锯齿的文字边缘，换个字体就飘了。
func rowPixel(t *testing.T, tt *ui.Tester, title string) color.RGBA {
	t.Helper()
	r, ok := tt.Find(title)
	if !ok {
		t.Fatalf("画面上找不到「%s」，文字有 %q", title, tt.Texts())
	}
	y := int(r.Y + r.H/2)
	if y < 0 || y >= testerH {
		t.Fatalf("「%s」那一行在画面外（y=%d）", title, y)
	}
	return tt.Image().RGBAAt(4, y)
}

// pitch 是两行在画面上的行距：later 那一行的上边缘离 from 有多远。
func pitch(t *testing.T, tt *ui.Tester, from, later string) float32 {
	t.Helper()
	a, ok := tt.Find(from)
	if !ok {
		t.Fatalf("画面上找不到「%s」，文字有 %q", from, tt.Texts())
	}
	b, ok := tt.Find(later)
	if !ok {
		t.Fatalf("画面上找不到「%s」，文字有 %q", later, tt.Texts())
	}
	return b.Y - a.Y
}

// frame 连跑几帧。
//
// ⚠️ 一帧不够：`ScrollTo` 是在**布局时**才滚到那一行的，而行高是量出来的，
// 量完才知道那一条落在哪儿（`mygo/ui/list.go` 的 ScrollTo 注释原话：「the
// list scrolls as the frame is laid out, to where the row is once it is
// measured」）。第一帧滚过去，第二帧才是滚到位之后的样子。
func frame(tt *ui.Tester) {
	for range 3 {
		tt.Frame()
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 虚拟滚动
// ─────────────────────────────────────────────────────────────────────────────

// TestThousandsOfSongsOnlyBuildTheOnesInView 是验收第 1 条：几千行的列表
// 首屏不加载全部行。
//
// ⚠️ 判据是**画面上有几个元素**，不是「跑了几毫秒」：5000 行全建出来的话画面
// 上有一万五千个文字节点，只建看得见的十几行时是几十个。差三个数量级，不受
// 机器快慢影响（那种计时断言在 CI 上必 flaky）。
//
// 顺带钉住「滚下去还是只建看得见的那些」：只验首屏的话，一个「首屏虚拟、滚起来
// 就全建」的实现照样能过。
func TestThousandsOfSongsOnlyBuildTheOnesInView(t *testing.T) {
	l, tt := newList(t, Actions{}, songsOf(5000, "UP"))

	if n := len(tt.Texts()); n > 200 {
		t.Errorf("5000 行的首屏画出了 %d 个元素，want ≤ 200（首屏不该建全部行）", n)
	}
	if !onScreen(tt, "歌 1") {
		t.Errorf("第一行没画出来，文字有 %q", tt.Texts())
	}
	if onScreen(tt, "歌 5000") {
		t.Error("最后一行在首屏就画出来了")
	}

	l.ScrollTo(2500, ui.Start)
	frame(tt)
	if n := len(tt.Texts()); n > 200 {
		t.Errorf("滚到第 2500 行之后画出了 %d 个元素，want ≤ 200", n)
	}
	if !onScreen(tt, "歌 2501") {
		t.Errorf("滚过去之后第 2501 行没画出来，文字有 %q", tt.Texts())
	}
	if onScreen(tt, "歌 1") {
		t.Error("滚到第 2500 行，第一行还画在画面上")
	}
}

// TestALongListScrollsToTheRightRowEvenWithVeryLongTitles 是「滚到半路仍然落在
// 该落的那一行」那条验收的长列表那一半。
//
// ⚠️ 这一条和 `TestEveryRowIsTheSameHeightNoMatterHowLongTheTitle` 不是同一件事：
// 那条只比两行的高低，这条走的是**几千行里滚到中间**那条路——滚到第 2501 首的
// 时候，后面的行都还没被量过，位置是估的，估错了要靠量出来的那一批把它推回去。
//
// 判据是**画面上的位置**：顶上的那一行就该是第 2501 首，紧跟它的那行按**行高**
// 摆下去（差值 64），不是按平均值。
func TestALongListScrollsToTheRightRowEvenWithVeryLongTitles(t *testing.T) {
	const n = 5000
	long := strings.Repeat("很长的歌名", 10)
	songs := make([]Music, n)
	for i := range songs {
		title := "歌 " + strconv.Itoa(i+1)
		if i%2 == 0 { // 偶数行的歌名长到要截断
			title = long + strconv.Itoa(i+1)
		}
		songs[i] = song("id-"+strconv.Itoa(i), title, "某个 UP 主")
	}

	l := New(newCache(t), Actions{})
	l.SetSongs(songs)
	tt := ui.NewTester(l.View, testerW, testerH)
	tt.Frame()

	l.ScrollTo(2500, ui.Start)
	frame(tt)

	top, ok := tt.Find(long + "2501")
	if !ok {
		t.Fatalf("滚到第 2501 首之后它没在画面上，文字有 %q", tt.Texts())
	}
	if top.Y > 2 {
		t.Errorf("顶上那一行在 y=%v，want 0（滚到 2500 却没有落在那里）", top.Y)
	}
	next, ok := tt.Find("歌 2502")
	if !ok {
		t.Fatalf("紧跟的那一行没在画面上，文字有 %q", tt.Texts())
	}
	// 差值是**行高**（64），不是全表的平均值——歌名再长也不改这一条。
	if got := next.Y - top.Y; got < 63 || got > 65 {
		t.Errorf("第一行到第二行隔了 %v，want 64（行高是定死的一个值）", got)
	}
	if onScreen(tt, "歌 1") {
		t.Error("滚到第 2501 首，第一行还画在画面上")
	}
}

// TestAnEmptyListSaysSo 验空列表有话说，不是一片白板。
func TestAnEmptyListSaysSo(t *testing.T) {
	_, tt := newList(t, Actions{}, nil)
	if !tt.HasText("这里还没有歌") {
		t.Errorf("空列表没画空状态，文字有 %q", tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 行高
// ─────────────────────────────────────────────────────────────────────────────

// TestEveryRowIsTheSameHeightNoMatterHowLongTheTitle 钉住「**行高是定死的一个
// 值**，歌名长也不变高」。
//
// 判据是**画面上相邻两行隔多远**（渲染位置）和**每一行自己有多高**，不是「行高
// 字段是多少」：写死了没真的用上，两行的 y 就还是不对。
//
// ⚠️ 这一条曾经是**反的**：原来歌名折到第二行、整行就高一截（64 对 76），一屏能
// 看几首歌会随着歌名长短变来变去。上游那一行的歌名是 `truncate`（一行），网格
// 卡片那一格才是两行——见 `card.go` 的文件头。
//
// ⚠️ **还要断「歌名真的只占一行」**：行高写死之后，歌名折成两行只会把第二行挤掉
// （看不见，但确实在那儿）。所以断的是**UP 名在行内的位置**：歌名折行的话 UP 名
// 会被往下推那么一行。
func TestEveryRowIsTheSameHeightNoMatterHowLongTheTitle(t *testing.T) {
	// 够长，一行摆不下。三首各带一个不一样的字：`tt.Find` 找的是第一次出现的
	// 那个，三首歌名一样的话第二、三行量到的还是第一行的位置。
	long := strings.Repeat("很长很长的歌名", 6)

	_, tt := newList(t, Actions{}, []Music{
		song("s1", "歌 1", "UP 甲"),
		song("s2", long+"乙", "UP 乙"),
		song("s3", "歌 3", "UP 丙"),
	})

	// ① 三行一样高，而且就是 64。
	for _, title := range []string{"歌 1", long + "乙", "歌 3"} {
		r, ok := tt.Find(title)
		if !ok {
			t.Fatalf("画面上找不到 %q，文字有 %q", title, tt.Texts())
		}
		if r.H != listRowHeight {
			t.Errorf("「%s」那一行高 %v，want %v（行高是定死的一个值）",
				title, r.H, listRowHeight)
		}
	}

	// ② 相邻两行隔的也是行高。
	//
	// ⚠️ 留 0.5 的余量：量出来的位置是浮点排版的结果，逐位相等不是「对齐」的
	// 证据（同一屏里两次排版的末位可能差一丁点）。这里要证的是「没脱节」。
	if got := pitch(t, tt, "歌 1", long+"乙"); got < listRowHeight-0.5 || got > listRowHeight+0.5 {
		t.Errorf("第一行到第二行隔了 %v，want %v（行高是定死的一个值）", got, listRowHeight)
	}

	// ③ 歌名只占一行：UP 名在行内的位置和短歌名那一行一样。
	//
	// ⚠️ 量的是**UP 名那一块**（不是行）：歌名要是折了行，UP 名就被推下去了。
	shortRow, ok := tt.Find("歌 1")
	if !ok {
		t.Fatalf("画面上找不到「歌 1」，文字有 %q", tt.Texts())
	}
	shortOwner, ok := tt.Find("UP 甲")
	if !ok {
		t.Fatalf("画面上找不到「UP 甲」，文字有 %q", tt.Texts())
	}
	longRow, ok := tt.Find(long + "乙")
	if !ok {
		t.Fatalf("画面上找不到长歌名那一行，文字有 %q", tt.Texts())
	}
	longOwner, ok := tt.Find("UP 乙")
	if !ok {
		t.Fatalf("画面上找不到「UP 乙」，文字有 %q", tt.Texts())
	}
	shortOff := shortOwner.Y - shortRow.Y
	longOff := longOwner.Y - longRow.Y
	if abs32(shortOff-longOff) > 0.5 {
		t.Errorf("长歌名那一行的 UP 名比短歌名那一行低了 %v——歌名折了行（want 一样）",
			longOff-shortOff)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 🔴 铁律四：行的身份
// ─────────────────────────────────────────────────────────────────────────────

// TestInsertingARowAtTheHeadKeepsTheChosenSong 是验收第 3 条，也是这个包
// 存在的理由：行有稳定且唯一的标识，在前面插入一行，后面所有行的状态不错配。
//
// ⚠️ 这一条是**实测踩过的坑**：MyGo 默认拿「父 ID + 兄弟序号」当身份，头部插入
// 一行之后，不带 key 的列表选中项会变成旁边另一首。对照见下面那条。
//
// 判据是**画面上那一行的底色**：选中行是主色底（`ui/table.go` 的
// node.Selected），别的行不是。只断言状态字段不够——字段对了画面没跟着变，
// 同样算坏。
func TestInsertingARowAtTheHeadKeepsTheChosenSong(t *testing.T) {
	songs := []Music{
		song("a", "第一首", "UP 甲"),
		song("b", "第二首", "UP 乙"),
		song("c", "第三首", "UP 丙"),
	}
	l, tt := newList(t, Actions{}, songs)

	if err := tt.Click("第二首"); err != nil {
		t.Fatal(err)
	}
	chosen := rowPixel(t, tt, "第二首")
	if chosen == rowPixel(t, tt, "第三首") {
		t.Fatalf("选中的行和没选中的行是同一个底色（%v），选中根本没画出来", chosen)
	}

	// 在最前面插一行：「新来的一首」排到最上面，原来选中的那首往后挪一位。
	l.SetSongs(append([]Music{song("new", "新来的一首", "UP 新")}, songs...))
	frame(tt)

	if got := rowPixel(t, tt, "第二首"); got != chosen {
		t.Errorf("头部插入一行之后「第二首」那一行是 %v，插入前是 %v，want 一样"+
			"（选中的还是那一首）", got, chosen)
	}
	if got := rowPixel(t, tt, "新来的一首"); got == chosen {
		t.Error("新插进来的那一行被画成了选中——选中跟位置走了，没有跟着 ID 走")
	}

	// 拿歌问也一样：拿到的还是那一首，不是「第 2 行」。
	got, ok := l.Selected()
	if !ok || got.ID != "b" {
		t.Errorf("Selected() = (%q, %v)，want (b, true)", got.ID, ok)
	}
}

// TestWithoutKeysTheChosenSongDoesSlide 是上一条的反面：不带 Key 的列表，
// 头部插入一行之后选中项真的会变成旁边那一首。
//
// 它把一个**没有装 Key** 的裸 `ui.List` 原样摆出来当对照。谁要觉得
// `View` 里装 Key 是多余的、想「简化」掉，翻开这条就知道代价是什么——而且它
// 自己会核对对照还成立：对不上就说明框架改了行为，上面那条的说服力得重估。
func TestWithoutKeysTheChosenSongDoesSlide(t *testing.T) {
	ids := []string{"a", "b", "c"}
	sel := 1
	var s ui.ListState // ⚠️ 没有 s.Key：行身份就是序号
	tt := ui.NewTester(func(c *ui.Context) {
		s.Selected = &sel
		ui.List(c, &s, len(ids), func(i int) {
			ui.Text(c, ids[i]).Height(30)
		}).Grow(1)
	}, 300, 300)
	tt.Frame()

	ids = append([]string{"z", "y"}, ids...)
	tt.Frame()

	if sel == 3 && ids[sel] == "b" {
		t.Error("不带 Key 的列表居然保住了选中——这条对照不成立了，" +
			"TestInsertingARowAtTheHeadKeepsTheChosenSong 得重新想钉法")
	}
	t.Logf("对照成立：不带 Key 时头部插入两行，sel=%d 现在指的是 %q（不再是 b）", sel, ids[sel])
}

// TestRowKeysMustBeUnique 钉住「唯一」那半句，两层：
//
//   - **Tester 里当场炸**：重复的 ID 在 Tester 里 panic 并报出代码行号
//     （`ui/diagnostics.go`）。这一层是回归基线。
//   - **真窗口里会说话**：真窗口没有那个诊断，重复的 ID 只是让那几行共用一份
//     状态——选中一个另一个也亮着（铁律四要的正好反过来）。`SetSongs` 里那条
//     警告是唯一的提示，所以它也得验。
func TestRowKeysMustBeUnique(t *testing.T) {
	dupes := []Music{
		song("same", "第一首", "UP 甲"),
		song("same", "第二首", "UP 乙"),
	}

	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("两行用了同一个 ID，列表却一声不吭")
			}
		}()
		_, _ = newList(t, Actions{}, dupes)
	}()

	if !strings.Contains(buf.String(), "同一个 ID") {
		t.Errorf("重复 ID 没有被 SetSongs 说出来，日志是 %q", buf.String())
	}
}

// TestSelectedKeepsNamingTheSameSongAcrossARefresh 走一遍公开的取法：选中项
// 是**一首歌**，不是「第几行」。
//
// 和 TestInsertingARowAtTheHeadKeepsTheChosenSong 是一件事的两面：那条看画面
// （那一行的底色还是不是主色），这条看调用方拿到的返回值——页面拿它去干什么
// （播放、下载、查看 UP 主）全靠这个。
func TestSelectedKeepsNamingTheSameSongAcrossARefresh(t *testing.T) {
	songs := songsOf(3, "UP")
	l, tt := newList(t, Actions{}, songs)

	if err := tt.Click("歌 3"); err != nil {
		t.Fatal(err)
	}
	before, ok := l.Selected()
	if !ok || before.Title != "歌 3" {
		t.Fatalf("点了「歌 3」，Selected() = (%q, %v)", before.Title, ok)
	}

	// 「自动续拉」把新的一页接到最前面——Biu 的分页虚拟列表就是这样长的。
	l.SetSongs(append([]Music{song("new", "新来的", "UP 新")}, songs...))
	frame(tt)

	after, ok := l.Selected()
	if !ok || after.ID != before.ID {
		t.Errorf("头部插入一行之后 Selected() 从 %q 变成了 %q，want 同一首", before.Title, after.Title)
	}
	if l.sel != 3 {
		t.Errorf("它的下标从 2 变成了 %d，want 3（下标本来就该跟着挪，这是对的）", l.sel)
	}
}

// abs32 是两个行距相减取绝对值。
func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// TestTheSelectionGoesAwayWithTheSongItNames 验选中那一首被删掉之后，选中就
// 没有了——不是留在原地指着下一首，也不是指向已经不存在的那一行。
//
// 这是铁律四的最后一格：`Selected()` 给出去的是一首歌，歌没了就没有了。
// （框架按 key 重新对位，找不到就把下标落成 -1；这里钉住结果。）
func TestTheSelectionGoesAwayWithTheSongItNames(t *testing.T) {
	songs := songsOf(5, "UP")
	l, tt := newList(t, Actions{}, songs)

	if err := tt.Click("歌 3"); err != nil {
		t.Fatal(err)
	}
	if got, _ := l.Selected(); got.ID != "id-2" {
		t.Fatalf("点了「歌 3」，Selected() = %q", got.ID)
	}

	// 那一首被删掉了（歌单被换小），选中不该改指别人。
	l.SetSongs(songs[:2])
	frame(tt)
	if got, ok := l.Selected(); ok {
		t.Errorf("选中的那一首被删掉之后 Selected() 还给着 %q，want 没有选中", got.ID)
	}
}

// TestAnEmptyListAfterASelectionDoesNotCrash 验「选中着 → 数据清空」不炸。
//
// 换页、筛选没结果、网络错误都走这条路。下标还停在刚才那一行，Selected 一取
// 就是越界——这一条钉的是它返回「没有选中」而不是崩。
func TestAnEmptyListAfterASelectionDoesNotCrash(t *testing.T) {
	l, tt := newList(t, Actions{}, songsOf(5, "UP"))
	if err := tt.Click("歌 3"); err != nil {
		t.Fatal(err)
	}

	l.SetSongs(nil)
	frame(tt)

	if got, ok := l.Selected(); ok {
		t.Errorf("列表空了之后 Selected() = (%q, true)，want 没有选中", got.ID)
	}
	if !tt.HasText("这里还没有歌") {
		t.Errorf("列表空了却没画空状态，文字有 %q", tt.Texts())
	}
}

// TestARowThatCannotBeDrawnDoesNotTakeTheRestDown 验行级 recover 真的兜得住，
// 而且崩过的行**不再重试**。
//
// ⚠️ 怎么让一行画崩：用 `New` 文档里说了「不能这么用」的 nil 缓存。有封面的
// 那一行会在 imgcache.View 里空指针崩掉，没有封面的照画——正好把「只废掉那一行」
// 这件事验出来。
//
// 这一条要是挂了，表现是把整个进程带走：MyGo 的事件循环没有 recover，view 里的
// panic 穿出去就是进程没了（铁律三的由来）。
//
// ⚠️ 日志是收进 buf 的，不往 stderr 吐：这一行每次跑都带一整份 `debug.Stack()`，
// 打到 CI 日志里看着像崩了。同时它也是「不重试」的判据——重试一次就多一行日志。
func TestARowThatCannotBeDrawnDoesNotTakeTheRestDown(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	l := New(nil, Actions{})
	l.SetSongs([]Music{
		{ID: "bad1", Title: "画不出来的第一首", Cover: "https://example.invalid/a.png"},
		{ID: "good", Title: "画得出来的一首"},
		{ID: "bad2", Title: "画不出来的第三首", Cover: "https://example.invalid/b.png"},
	})
	tt := ui.NewTester(l.View, testerW, testerH)
	tt.Frame()

	if n := strings.Count(strings.Join(tt.Texts(), "|"), "这一行画不出来"); n != 2 {
		t.Errorf("画崩的两行里有 %d 行画了占位，want 2，文字有 %q", n, tt.Texts())
	}
	if !onScreen(tt, "画得出来的一首") {
		t.Errorf("一行崩了把别的行也带走了，文字有 %q", tt.Texts())
	}
	after := buf.String()

	// 连着再画四帧：那两行崩过就该记下来不再崩（每帧每行各打一份栈，帧率和日志
	// 都受不了）。日志条数不变 = 没重试。
	frame(tt)
	frame(tt)
	if got, want := strings.Count(buf.String(), "列表行画崩了"), strings.Count(after, "列表行画崩了"); got != want {
		t.Errorf("再画四帧之后崩了 %d 次，第一次是 %d 次：崩过的行每帧都在重试", got, want)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 基准
// ─────────────────────────────────────────────────────────────────────────────

// BenchmarkRedrawAListOfFiveThousand 是一张 5000 行的列表重画一帧的代价。
//
// 收它不是为了好看，是因为 `PLAN.md` 阶段 4 明写着「阶段 4 必须自己在真窗口里
// 跑一个大歌单基准」——框架官方那个「一百万行每帧 14.5 µs」是 CPU 渲染后端 +
// Tester + GOMAXPROCS=1 测的，而且没有一个 fixture 是真的界面。
//
// 这个是**无头**的，比真窗口乐观，所以只用来盯数量级：行高的测量要是哪天
// 变成了「每一帧把所有行重新量一遍」，这里会从微秒级掉到毫秒级。
// 跑：go test -bench=. -benchtime=20x ./internal/ui/widgets/list/
func BenchmarkRedrawAListOfFiveThousand(b *testing.B) {
	l := New(imgcache.New(imgcache.Options{}), Actions{})
	l.SetSongs(songsOf(5000, "某个 UP 主"))

	tt := ui.NewTester(l.View, testerW, testerH)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		tt.Frame()
	}
}
