// 分集弹层右上角那个搜索框：敲词滤列表、× 清词、Esc 收浮层、回车放第一条命中、
// 点开时滚到正在放的那一集。过滤是**纯本地**的——分集是问 B 站问来的，但「在问到
// 的这几集里找」不该再发一趟请求。
//
// 判的都是**用户在画面上看到的**（哪几行还在、哪几行没了、浮层开没开），
// 不直接调 `partRowsMatching`——纯函数对了而 view 没接上的话，滤掉的是空气。
package ui

import (
	"testing"

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
