package list

import (
	"strconv"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// 这一组验表头（票面「三、列表表头」）。
//
// 最要紧的是**对得齐**：表头每一列的横坐标必须和行里那一列一样。判据是量出来的横坐
// 标，不是「调了哪个函数」——对不齐在画面上只是「表头歪了一点」，没人当它坏了。

// durOf 是第 i 首的时长（3:05 起），四首各不相同——量位置时要能认准是哪一行。
func durOf(i int) time.Duration { return time.Duration(185+i) * time.Second }

// headerView 是「一行表头 + 一个列表」的合成画面。
//
// ⚠️ **表头在列表外面**（页面 `Children` 里先 Header 再 View），所以这里也是：表头不
// 在滚动区里，列表上下滚它不动。
func headerView(c *ui.Context, l *List, key string, check bool) {
	ui.Column(c).FillWidth().FillHeight().Children(func() {
		l.Header(c, key)
		l.View(c)
	})
}

// newHeaderList 造一个带表头的列表，返回 Tester。
//
// check 是装不装多选（装了就多一个勾选框那一列）。
func newHeaderList(t *testing.T, v View, check bool) (*List, *ui.Tester) {
	t.Helper()
	acts := (&recorded{}).actions()
	l := New(newCache(t), acts)
	l.SetView(v)
	songs := onlineSongs(5)
	for i := range songs {
		songs[i].Duration = durOf(i)
		songs[i].Owner = "UP " + string(rune('A'+i))
	}
	l.SetSongs(songs)
	if check {
		l.SetSelection(Selection{
			On:     func(int) bool { return false },
			Toggle: func(int) {},
		})
	}
	tt := ui.NewTester(func(c *ui.Context) { headerView(c, l, "hdr", check) }, testerW, testerH)
	tt.Frame()
	return l, tt
}

// TestTheHeaderSaysTheColumnsThisListActuallyHas 钉住表头说的是**这一页真有的列**。
//
// `list.Music` 上没有播放量也没有投稿时间，所以表头也只有三列。摆一列这一页没有内容
// 的表头，比不摆更糟：字都在、位置也在，只是下面永远是空的，用户看不出那是没接还是
// 自己记错了。
func TestTheHeaderSaysTheColumnsThisListActuallyHas(t *testing.T) {
	_, tt := newHeaderList(t, ViewList, false)

	for _, want := range []string{"#", "标题", "时长"} {
		if !shows(tt, want) {
			t.Errorf("表头上没有「%s」，文字有 %q", want, tt.Texts())
		}
	}
	for _, unwanted := range []string{"UP", "播放量", "投稿时间"} {
		if shows(tt, unwanted) {
			t.Errorf("表头上摆着「%s」，而这一页的行里没有那一列", unwanted)
		}
	}
}

// TestTheHeaderColumnsLineUpWithTheRows 钉住「表头和列宽固定对齐」。
//
// 量的就是横坐标：表头那一列的盒子和行里那一列的盒子**一个像素都不能差**。左右两端各
// 量一列——右边那列靠行的右边缘，左边那列吃掉所有余量，两边都对了中间才可能错。
//
// ⚠️ **左边的表头列拿「标题」比、行里拿 UP 名比**：行里那一列是
// `ui.Column(...).AlignItems(ui.Start)`（歌名在上、UP 名在下），UP 名那一块的左边就是
// 这一列的左边缘，和表头「标题」的左边缘是同一个数。
//
// ⚠️ **右边的行里那一列拿时长比，不拿歌名比**：歌名要靠 `Tester.Find(歌名)` 找，而那
// 命中的是 `ui.List` 挂在**整行**上的无障碍盒子（`List.View` 里那句 `l.state.Label`），
// 那个盒子的 X **永远是 0**——量它的话这一条永远绿。
//
// ⚠️ **时长要各行不同**（`durOf`），否则 `Find` 只给回第一行那个，量的是别行。
func TestTheHeaderColumnsLineUpWithTheRows(t *testing.T) {
	for _, v := range []View{ViewList, ViewCompact} {
		_, tt := newHeaderList(t, v, false)

		headHash, ok := tt.Find("#")
		if !ok {
			t.Fatalf("%v：表头上没有「#」，文字有 %q", v, tt.Texts())
		}
		headTitle, ok := tt.Find("标题")
		if !ok {
			t.Fatalf("%v：表头上没有「标题」，文字有 %q", v, tt.Texts())
		}
		headClock, ok := tt.Find("时长")
		if !ok {
			t.Fatalf("%v：表头上没有「时长」，文字有 %q", v, tt.Texts())
		}
		for i := range 3 {
			rowOwner, ok := tt.Find("UP " + string(rune('A'+i)))
			if !ok {
				t.Fatalf("%v：画面上找不到第 %d 行的 UP 名，文字有 %q", v, i+1, tt.Texts())
			}
			rowClock, ok := tt.Find(formatDuration(durOf(i)))
			if !ok {
				t.Fatalf("%v：画面上找不到第 %d 行的时长 %q，文字有 %q",
					v, i+1, formatDuration(durOf(i)), tt.Texts())
			}
			// 「#」那一列也要对：它是**唯一一列会被前面的空位顶偏的**（封面那一列的空
			// 位夹在它和「标题」中间，放错位置的话错的是它，而画面上只是「编号歪了一点」——
			// 第一版就是靠存图才发现的，`TestTheHeaderColumnsLineUpWithTheRows` 那时只
			// 量了左右两端，中间那一列歪了它一声不响）。
			rowNo, ok := tt.Find(strconv.Itoa(i + 1))
			if !ok {
				t.Fatalf("%v：画面上找不到第 %d 行的序号，文字有 %q", v, i+1, tt.Texts())
			}
			if headHash.X != rowNo.X {
				t.Errorf("%v：表头的「#」在 %.1f，行里那个序号在 %.1f——差 %.1f"+
					"（封面那一列的空位不在它和「标题」中间？）",
					v, headHash.X, rowNo.X, headHash.X-rowNo.X)
			}
			if headTitle.X != rowOwner.X {
				t.Errorf("%v：表头的「标题」在 %.1f，行里那一列在 %.1f——差 %.1f",
					v, headTitle.X, rowOwner.X, headTitle.X-rowOwner.X)
			}
			if headClock.X != rowClock.X || headClock.W != rowClock.W {
				t.Errorf("%v：表头的「时长」在 %.1f、宽 %.1f，行里那一列在 %.1f、宽 %.1f"+
					"——差 %.1f，数字列竖着扫不下来",
					v, headClock.X, headClock.W, rowClock.X, rowClock.W, headClock.X-rowClock.X)
			}
		}
	}
}

// TestTheHeaderLinesUpWithTheRowsInMultiSelect 钉住「多选态一开，表头照样对得上」。
//
// 行最左边会多出一个 16 宽的勾选框，表头**必须照着留出同样的位置**——不留的话右边那
// 几列整整差 16，而那种差在画面上只是「多选一开，列名歪了」。
//
// ⚠️ **量左边那一列（标题 / UP 名），不量右边那列（时长）**：时长是最后一块、靠右对
// 齐，它的位置由行的**右边缘**决定。多选在左边加东西，右边缘一动不动——第一版量的就
// 是时长，于是「表头让没让位置」这件事它一点也证明不了。
func TestTheHeaderLinesUpWithTheRowsInMultiSelect(t *testing.T) {
	_, plain := newHeaderList(t, ViewList, false)
	_, multi := newHeaderList(t, ViewList, true)

	before, ok := plain.Find("标题")
	if !ok {
		t.Fatalf("表头上没有「标题」，文字有 %q", plain.Texts())
	}
	after, ok := multi.Find("标题")
	if !ok {
		t.Fatalf("多选态下表头上没有「标题」，文字有 %q", multi.Texts())
	}
	body, ok := multi.Find("UP A")
	if !ok {
		t.Fatalf("多选态下画面上找不到 UP 名，文字有 %q", multi.Texts())
	}

	if after.X != body.X {
		t.Errorf("多选态下表头的「标题」在 %.1f，行里那一列在 %.1f——表头没给那个勾选框"+
			"让位置（不开多选时它在 %.1f，差 %.1f）",
			after.X, body.X, before.X, after.X-body.X)
	}
	if before.X == after.X {
		t.Errorf("开不开多选，表头的「标题」都在 %.1f——行里那一列明明让开了勾选框的宽"+
			"（勾选框 %d 加一个间距 %d）", after.X, 16, int(cardGap))
	}
}

// TestThereIsNoHeaderForTheGrid 钉住「网格那一档不摆表头」。
//
// 一格是一首歌，表头上那几列和格子对不上号，而对不上的表头在界面上不像坏了（`later.go`
// 里那一处先想到过这件事）。
func TestThereIsNoHeaderForTheGrid(t *testing.T) {
	sv := NewSwitchView(newCache(t), (&recorded{}).actions())
	sv.SetSongs(onlineSongs(6))
	sv.SetView(ViewCard)
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).FillWidth().FillHeight().Children(func() {
			sv.Header(c, "hdr")
			sv.View(c)
		})
	}, 1100, gridH)
	tt.Frame()

	for _, want := range []string{"#", "标题", "时长"} {
		if shows(tt, want) {
			t.Errorf("网格那一档也摆了表头（「%s」），文字有 %q", want, tt.Texts())
		}
	}
}

// headerScrollFar 是一次滚到头的距离，够这一屏滚到底。
const headerScrollFar = 100000

// TestTheHeaderHasALineUnderIt 钉住「表头底下有一条线」。
//
// 判据是**逐像素采样**：表头那一层是「列名那一行 + 底下那条线」两层，而那条线是
// **一整行**几乎每一个像素都不是底色。列名那几个字是**几个**像素不是底色——
// 两者一比就知道哪一行是线。
//
// ⚠️ **线必须在列名下面**：线画在列名上面的话，表头和下面第一行还是粘在一起的
// （那就是改之前的样子），而那种错在画面上只是「表头看着有点怪」。
func TestTheHeaderHasALineUnderIt(t *testing.T) {
	_, tt := newHeaderList(t, ViewList, false)

	img := tt.Image()
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	bg := img.RGBAAt(w-1, h-1) // 右下角是空的：那就是底色

	// 找那一行「几乎整行都不是底色」的像素。
	line, most := -1, 0
	for y := range 64 {
		n := 0
		for x := range w {
			if img.RGBAAt(x, y) != bg {
				n++
			}
		}
		if n > most {
			line, most = y, n
		}
	}
	if most < w*9/10 {
		t.Errorf("表头底下没有一道横线（最满的一行只有 %d/%d 个像素不是底色）", most, w)
	}

	// 线在列名**下面**。
	head, ok := tt.Find("标题")
	if !ok {
		t.Fatalf("表头上没有「标题」，文字有 %q", tt.Texts())
	}
	if float32(line) < head.Y+head.H {
		t.Errorf("那道线在 y=%d，列名那一块在 y=%v~%v——线在列名上面（或压在它身上）",
			line, head.Y, head.Y+head.H)
	}
}

// TestTheHeaderColumnNamesAreBiggerThanTheRowsSecondaryText 钉住「表头的字号比
// 行里的次要文字大」。
//
// ⚠️ **判据是「比行里的次要文字大」而不是「正好多少像素」**：行里的次要文字是
// 12px（UP 名 / 时长），表头是 14px（上游那一行表头的数，`text-sm`）。逐位相等
// 不是「对」的证据（换个系统字体行高就变），而「谁大谁小」是这一条真正要的。
//
// 改之前表头是 12px——和行里的次要文字一样大、一样灰，竖着扫下去时它比下面那一列
// 数字还弱（票面原话）。
func TestTheHeaderColumnNamesAreBiggerThanTheRowsSecondaryText(t *testing.T) {
	_, tt := newHeaderList(t, ViewList, false)

	head, ok := tt.Find("标题")
	if !ok {
		t.Fatalf("表头上没有「标题」，文字有 %q", tt.Texts())
	}
	// 行里的次要文字：UP 名（12px）。
	owner, ok := tt.Find("UP A")
	if !ok {
		t.Fatalf("画面上找不到 UP 名，文字有 %q", tt.Texts())
	}
	if head.H <= owner.H {
		t.Errorf("表头的列名高 %v，行里的次要文字（UP 名）高 %v——列名没有比次要文字大",
			head.H, owner.H)
	}
}

// TestTheHeaderSurvivesScrolling 钉住「表头横向滚动时跟着走」的那一条在竖向滚动上：
// 列表滚下去，表头还在。
//
// ⚠️ 横向本来就没有滚动区——Biu 那一行的列宽是定死的（`1fr` 那一列吃掉剩下的宽），
// 所以宽屏下滚到右边列名自然还在。这一条量的是竖向：表头在**列表外面**（`headerView`
// 里它和 `l.View(c)` 是兄弟），列表往下滚它不动。
func TestTheHeaderSurvivesScrolling(t *testing.T) {
	_, tt := newHeaderList(t, ViewList, false)

	before, ok := tt.Find("时长")
	if !ok {
		t.Fatalf("表头上没有「时长」，文字有 %q", tt.Texts())
	}
	tt.Scroll(testerW/2, testerH/2, 0, headerScrollFar)
	for range 8 {
		tt.Frame()
	}
	after, ok := tt.Find("时长")
	if !ok {
		t.Fatalf("滚下去之后表头不见了：%q", tt.Texts())
	}
	if after.X != before.X || after.Y != before.Y {
		t.Errorf("列表滚下去之后表头动了（%.1f,%.1f → %.1f,%.1f）——它被滚走了",
			before.X, before.Y, after.X, after.Y)
	}
}
