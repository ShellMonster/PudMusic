// 分集弹层右上角那个搜索框：敲词滤列表、× 清词、Esc 收浮层、回车放第一条命中、
// 点开时滚到正在放的那一集。过滤是**纯本地**的——分集是问 B 站问来的，但「在问到
// 的这几集里找」不该再发一趟请求。
//
// 判的都是**用户在画面上看到的**（哪几行还在、哪几行没了、浮层开没开），
// 不直接调 `partRowsMatching`——纯函数对了而 view 没接上的话，滤掉的是空气。
package ui

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// typeInPartSearch 点一下弹层里那个搜索框、往里敲几个字。
func typeInPartSearch(t *testing.T, tt *ui.Tester, text string) {
	t.Helper()
	if err := tt.Click(partSearchLabel); err != nil {
		t.Fatalf("点不到分集搜索框：%v，文字有 %q", err, tt.Texts())
	}
	tt.Type(text)
	tt.Frame()
}

// TestPartSearchFiltersByTitle 断「敲词滤列表」那一条：敲「第二」之后只剩
// 「P2 · 第二集」在画面上，浮层还开着。
func TestPartSearchFiltersByTitle(t *testing.T) {
	s, tt := onlineTester(t, multiPages(), false)
	partsReady(t, s, tt, 3)
	openPartsDrawer(t, tt)

	typeInPartSearch(t, tt, "第二")

	if !tt.HasText("P2 · 第二集") {
		t.Errorf("敲了「第二」之后命中的那一集不在画面上，文字有 %q", tt.Texts())
	}
	for _, gone := range []string{"P1 · 第一集", "P3 · 第 3 集"} {
		if tt.HasText(gone) {
			t.Errorf("敲了「第二」之后 %q 还在画面上——没有滤掉", gone)
		}
	}
	if !s.partOpen {
		t.Error("敲词的时候浮层被收掉了")
	}
}

// TestPartSearchMatchesThePageNumber 断「敲数字按集数滤」：敲「3」命中 P3，
// P1 / P2 都不在。
func TestPartSearchMatchesThePageNumber(t *testing.T) {
	s, tt := onlineTester(t, multiPages(), false)
	partsReady(t, s, tt, 3)
	openPartsDrawer(t, tt)

	typeInPartSearch(t, tt, "3")

	if !tt.HasText("P3 · 第 3 集") {
		t.Errorf("敲了「3」之后 P3 不在画面上，文字有 %q", tt.Texts())
	}
	for _, gone := range []string{"P1 · 第一集", "P2 · 第二集"} {
		if tt.HasText(gone) {
			t.Errorf("敲了「3」之后 %q 还在画面上——没有滤掉", gone)
		}
	}
}

// TestPartSearchWithNoMatchSaysSo 断「一个都滤不到」那一档：画面上说
// 「没有匹配的分集」，而不是留一片空白或一整列错的东西。
func TestPartSearchWithNoMatchSaysSo(t *testing.T) {
	s, tt := onlineTester(t, multiPages(), false)
	partsReady(t, s, tt, 3)
	openPartsDrawer(t, tt)

	typeInPartSearch(t, tt, "根本不存在的一集")

	if !tt.HasText("没有匹配的分集") {
		t.Errorf("滤光了之后画面上没说为什么没有，文字有 %q", tt.Texts())
	}
	for _, gone := range []string{"P1 · 第一集", "P2 · 第二集", "P3 · 第 3 集"} {
		if tt.HasText(gone) {
			t.Errorf("滤光了之后 %q 还在画面上", gone)
		}
	}
}

// TestPartSearchDoesNotRenumber 断「过滤不重新编号」：「P2」滤出来之后还是
// 「P2 · 第二集」，不是顶头那条重新从 P1 排。
func TestPartSearchDoesNotRenumber(t *testing.T) {
	s, tt := onlineTester(t, multiPages(), false)
	partsReady(t, s, tt, 3)
	openPartsDrawer(t, tt)

	typeInPartSearch(t, tt, "第二")

	if !tt.HasText("P2 · 第二集") {
		t.Fatalf("滤完之后那一行不在，文字有 %q", tt.Texts())
	}
	if tt.HasText("P1 · 第二集") {
		t.Error("滤完之后「第二集」被重新编号成了 P1——编号跟着位置走了")
	}
}

// TestPartSearchResetsOnReopen 断「关掉再点开词没了」：再打开时看到的是完整的
// 那一列，不是上一次滤剩的几行。
func TestPartSearchResetsOnReopen(t *testing.T) {
	s, tt := onlineTester(t, multiPages(), false)
	partsReady(t, s, tt, 3)
	openPartsDrawer(t, tt)

	typeInPartSearch(t, tt, "第二")
	if !tt.HasText("P2 · 第二集") || tt.HasText("P1 · 第一集") {
		t.Fatalf("前提没立起来：敲了「第二」之后列表没滤，文字有 %q", tt.Texts())
	}

	// Esc 收浮层（焦点在输入框里，Esc 落到浮层的「按 Esc 关」上）。
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if s.partOpen {
		t.Fatalf("Esc 之后浮层还开着（partOpen=true）")
	}

	openPartsDrawer(t, tt)
	if s.partQuery != "" {
		t.Errorf("重新点开之后搜索框里还留着上次的词 %q", s.partQuery)
	}
	for _, want := range []string{"P1 · 第一集", "P2 · 第二集", "P3 · 第 3 集"} {
		if !tt.HasText(want) {
			t.Errorf("重新点开之后 %q 不在画面上——上次的过滤漏过来了，文字有 %q", want, tt.Texts())
		}
	}
}

// TestPartSearchClearButton 断那个 ×：有词时点它，词清了、整列回来、焦点还在
// 输入框里（清完接着敲不用再点一下）。
func TestPartSearchClearButton(t *testing.T) {
	s, tt := onlineTester(t, multiPages(), false)
	partsReady(t, s, tt, 3)
	openPartsDrawer(t, tt)

	typeInPartSearch(t, tt, "第二")
	if !tt.HasText("P2 · 第二集") || tt.HasText("P1 · 第一集") {
		t.Fatalf("前提没立起来：敲了「第二」之后列表没滤，文字有 %q", tt.Texts())
	}

	if err := tt.Click("清空搜索"); err != nil {
		t.Fatalf("有词的时候找不到 / 点不了清空键：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()

	if s.partQuery != "" {
		t.Errorf("点了清空之后搜索框里还有词 %q", s.partQuery)
	}
	for _, want := range []string{"P1 · 第一集", "P2 · 第二集", "P3 · 第 3 集"} {
		if !tt.HasText(want) {
			t.Errorf("点了清空之后 %q 没有回来，文字有 %q", want, tt.Texts())
		}
	}

	// 焦点该还在输入框里：清完直接接着敲，不用再点一下。
	tt.Type("3")
	tt.Frame()
	if s.partQuery != "3" {
		t.Fatalf("清空之后接着敲没进输入框（词是 %q）——焦点丢在那个 × 上了", s.partQuery)
	}
	if !tt.HasText("P3 · 第 3 集") || tt.HasText("P1 · 第一集") {
		t.Errorf("清空之后接着敲「3」没有重新滤，文字有 %q", tt.Texts())
	}
}

// TestPartSearchEnterPlaysTheFirstMatch 断「回车放第一条命中」：敲「第二」按回车，
// 切到 778（第二集），浮层收掉——和点那一行同一个结果。
func TestPartSearchEnterPlaysTheFirstMatch(t *testing.T) {
	s, tt := onlineTester(t, multiPages(), false)
	partsReady(t, s, tt, 3)
	openPartsDrawer(t, tt)

	typeInPartSearch(t, tt, "第二")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()

	if s.partOpen {
		t.Error("回车之后浮层还开着")
	}
	waitFrames(t, tt, "换到第二集", func() bool {
		it, ok := s.app.CurrentItem()
		return ok && it.Cid == "778"
	})
}

// TestPartSearchEnterWithNoMatchDoesNothing 断「一个都滤不到时回车不动作」：
// 不换集、不收浮层——画面上唯一的变化是该没有。
func TestPartSearchEnterWithNoMatchDoesNothing(t *testing.T) {
	s, tt := onlineTester(t, multiPages(), false)
	partsReady(t, s, tt, 3)
	openPartsDrawer(t, tt)

	typeInPartSearch(t, tt, "根本不存在的一集")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()

	if !s.partOpen {
		t.Error("没命中的时候回车把浮层收掉了")
	}
	if it, ok := s.app.CurrentItem(); !ok || it.Cid != "777" {
		t.Errorf("没命中的时候回车换了集（当前 = %#v）", it)
	}
}

// manyPages 是一份**二十 P** 的回包（cids 701..720）：multiPages 只有三集，
// 三集摆不满一屏，「滚到正在放的那一集」没有东西可滚。
func manyPages() []any {
	pages := make([]any, 20)
	for i := range pages {
		pages[i] = map[string]any{
			"cid": 701 + i, "page": i + 1, "part": fmt.Sprintf("第 %d 段", i+1), "duration": 200 + i,
		}
	}
	return pages
}

// TestRenderPartSearchShots 是视觉审查用的三张图（BIU_SHOT_DIR 指了目录才画）：
// 全量、过滤中、一个都没命中。深浅各一套。
//
// ⚠️ 存图之前要**推钟**：浮层是 `PopIn` 渐变进场，光画帧的话透明度还是 0，
// 存下来是一张看不见浮层的图。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui -run TestRenderPartSearchShots
func TestRenderPartSearchShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{true, false} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			s, tt := onlineTester(t, multiPages(), false, at(1200, 800))
			partsReady(t, s, tt, 3)
			// onlineTester 自己会 `SetDark(true)`，所以深浅要在这儿覆盖一次。
			tt.SetDark(dark)
			openPartsDrawer(t, tt)
			tt.Advance(400 * time.Millisecond)
			tt.Frame()
			save(t, dir, name+"-part-search.png", tt)

			typeInPartSearch(t, tt, "第二")
			tt.Advance(300 * time.Millisecond)
			tt.Frame()
			save(t, dir, name+"-part-search-filtered.png", tt)

			if err := tt.Click("清空搜索"); err != nil {
				t.Fatalf("点不了清空键：%v", err)
			}
			typeInPartSearch(t, tt, "根本没有")
			tt.Advance(300 * time.Millisecond)
			tt.Frame()
			save(t, dir, name+"-part-search-empty.png", tt)
		})
	}
}

// TestPartsDrawerScrollsToTheEpisodeThatIsPlaying 断「点开时滚到正在放的那一集」：
// 播到第 12 集再点开，那一列**滑过去**停在中间——不是一跳到位。
//
// 判的是**真的滑过**：`partScroll.Y` 在两帧之间要走一段又停一段（至少一帧
// Y 是 0 和终点之间的中间值）——一跳到位的话永远量不到中间那一帧。
// 落定之后，正在放那一行的框要**整行落在「分集列表」面板的垂直范围里**。
//
// ⚠️ **不等固定帧数**：滑多久是动画的事，判据是「在飞（Y 在途中）→ 落定
// （Y 连停几帧）」这个顺序；200 帧 × 16ms 是给慢机器留的保险丝，不是成功判据。
func TestPartsDrawerScrollsToTheEpisodeThatIsPlaying(t *testing.T) {
	s, tt := onlineTester(t, manyPages(), false)
	partsReady(t, s, tt, 20)

	if err := s.app.PlayPart("712"); err != nil {
		t.Fatalf("放第 12 集：%v", err)
	}
	waitFrames(t, tt, "换到第 12 集", func() bool {
		it, ok := s.app.CurrentItem()
		return ok && it.Cid == "712"
	})

	openPartsDrawer(t, tt)

	// 先走到「Y 离开了 0」：滚动发生了。
	// ⚠️ 判据是 Y 这个**可观察结果**，不是 `Scrolling()`——`ScrollIntoView` 的
	// Smooth 走的是容器自己的 wheelGlide（`mygo/ui/wheel.go` 的 glideScroll），
	// 不写进 TrackScroll 那份的 glide，`Scrolling()` 对它恒假。
	moved := false
	for range 200 {
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
		if s.partScroll.Y > 0 {
			moved = true
			break
		}
	}
	if !moved {
		t.Fatalf("200 帧之后 partScroll.Y 还是 %.1f——点开没有滚到正在放的那一集",
			s.partScroll.Y)
	}

	// 等它落定：Y 连停 3 帧算稳（到位之后框架还要再花一帧才对得准，和
	// settleLyrics 同一条）。中途记下 Y 走过的中间值——滑过去的话至少有一帧
	// 既不是 0 也不是终点。
	calm := 0
	lastY, midY := s.partScroll.Y, float32(-1)
	settled := false
	for range 200 {
		tt.Advance(16 * time.Millisecond)
		tt.Frame()
		if widthGap(s.partScroll.Y-lastY) > 0.5 {
			midY = s.partScroll.Y
			lastY, calm = s.partScroll.Y, 0
			continue
		}
		if calm++; calm >= 3 {
			settled = true
			break
		}
	}
	if !settled {
		t.Fatalf("200 帧之后那一列还没落定（Y=%.1f）", s.partScroll.Y)
	}
	if midY < 0 {
		t.Fatalf("partScroll.Y 从 0 直接落到 %.1f，一帧中间值都没量到——"+
			"「滚到那一集」是一跳到位，不是滑过去的", s.partScroll.Y)
	}

	panel, ok := tt.Find("分集列表")
	if !ok {
		t.Fatalf("画面上找不到「分集列表」这一块，文字有 %q", tt.Texts())
	}
	active, ok := tt.Find("P12 · 第 12 段")
	if !ok {
		t.Fatalf("浮层里没有正在放的那一行「P12 · 第 12 段」，文字有 %q", tt.Texts())
	}
	// 整行落在面板的**垂直范围**里（不是只露个顶）：ScrollCenter 要的是居中，
	// 居中之后行的顶和底都在面板顶底之间。
	if active.Y < panel.Y || active.Y+active.H > panel.Y+panel.H {
		t.Errorf("正在放那一行的框是 %v，面板是 %v——它没有整行落在面板里",
			active, panel)
	}
	// ScrollCenter 要的是**居中**：顶对齐（ScrollStart）也能过上面那一条，
	// 但那会把上下文全丢在下面。面板中心比滚动区中心高半个表头，所以容差
	// 取面板高的四分之一。
	if gap := widthGap((active.Y + active.H/2) - (panel.Y + panel.H/2)); gap > panel.H/4 {
		t.Errorf("正在放那一行的中线离面板中线 %.1f（面板高 %.1f）——没有居中，"+
			"看起来是贴到了顶 / 底", gap, panel.H)
	}
}
