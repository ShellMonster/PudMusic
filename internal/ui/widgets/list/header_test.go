package list

import (
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
// 这个列表**没装** `Columns`（零值），所以表头只有三列。摆一列这一页没有内容的
// 表头，比不摆更糟：字都在、位置也在，只是下面永远是空的，用户看不出那是没接还是
// 自己记错了。装了那两列的样子见 `columns_test.go`。
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
