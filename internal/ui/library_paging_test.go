// 翻页那一层：三重保障、防重入、失败停住、回顶，和防抖那个钟会不会醒到进程退出。
//
// 这些验的都是 `library_rows.go` 那一层在**真实外壳**上的行为（假时钟 + 假 B 站
// 服务器），所以它们住在自己的文件里——页面长什么样、筛选用什么，见
// `librarypage_test.go`。

package ui

import (
	"fmt"
	"testing"
	"time"

	mygoui "github.com/egoist/mygo/ui"
)

// manyTitles 造 n 个标题（「第一页的第 1 首」这种）。
func manyTitles(page, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("第%d页的第%d首", page, i+1)
	}
	return out
}

// TestScrollingToTheEndLoadsTheNextPage 钉住三重保障的②。
//
// ⚠️ **第一页必须比一屏高**：内容一屏摆得下时三重保障的③（容器没填满）会自己把
// 第二页拉上来，根本用不着滚——那条断言就变成在验③了，而②一次都没走到过。
// 40 行 × 64 像素 ≈ 2560，远高过列表那一块。
func TestScrollingToTheEndLoadsTheNextPage(t *testing.T) {
	f := newLibFake(t).
		withLater(1, 60, manyTitles(1, 40)...).
		withLater(2, 60, manyTitles(2, 20)...)
	tt, s := libraryShell(t, f, laterPath)
	settle(t, tt, "把第一页拉回来", func() bool { return rowsShown(&s.laterPage) == 40 })
	if got := f.callsOf(laterPathAPI); got != 1 {
		t.Fatalf("第一页还没滚到底就发了 %d 次请求——那是③，不是我要验的那条", got)
	}

	scrollToBottom(t, tt)
	settle(t, tt, "滚到底之后自动续拉", func() bool { return rowsShown(&s.laterPage) == 60 })
	if got := f.callsOf(laterPathAPI); got != 2 {
		t.Errorf("一共发了 %d 次请求，want 2", got)
	}
	// 第二页接在第一页**后面**（铁律四：尾部追加），所以滚到底看见的是第二页的
	// 最后一条。断言「第一页的第一首还在」用不着——虚拟列表只建看得见的几行，
	// 滚到底之后第一页的行压根不在画面上，拿它当判据会永远红。
	scrollToBottom(t, tt)
	if !tt.HasText("第2页的第20首") {
		t.Errorf("滚到底没看见第二页的最后一条，文字有 %q", tt.Texts())
	}
	if tt.HasText("第1页的第1首") && !tt.HasText("第2页的第20首") {
		t.Error("还停在第一页")
	}
}

// TestRefillLoadsTheNextPageWithoutScrolling 钉住三重保障的③。
//
// **筛选后只剩两页**是最容易漏的那一条：用户没滚过，容器没填满，于是底下留一大
// 片空白还一直转圈。这里第一页只有两行、一屏摆得下，于是它必须自己续拉。
func TestRefillLoadsTheNextPageWithoutScrolling(t *testing.T) {
	f := newLibFake(t).
		withLater(1, 3, "第一页第一首", "第一页第二首").
		withLater(2, 3, "第二页第一首")
	tt, s := libraryShell(t, f, laterPath)
	// ⚠️ 这里**不能**等「正好两行」：③ 要验的就是它自己把第二页拉上来，第一帧
	// 之后可能已经是三行了。等「至少两行」。
	settle(t, tt, "把第一页拉回来", func() bool { return rowsShown(&s.laterPage) >= 2 })

	settle(t, tt, "没滚过也把第二页补上来了", func() bool { return rowsShown(&s.laterPage) == 3 })
	if !tt.HasText("第二页第一首") {
		t.Errorf("第二页没画出来，文字有 %q", tt.Texts())
	}
}

// TestAFailingPageOffersARetry 盯「拉失败」不是永远转圈。
func TestAFailingPageOffersARetry(t *testing.T) {
	f := newLibFake(t).
		withLater(1, 60, manyTitles(1, 40)...).
		withLater(2, 60, manyTitles(2, 20)...)
	tt, s := libraryShell(t, f, laterPath)
	settle(t, tt, "把第一页拉回来", func() bool { return rowsShown(&s.laterPage) == 40 })

	// 从现在起每一页都失败。翻到第二页时它就该停住并让人点重试。
	f.failOn(laterPathAPI)

	scrollToBottom(t, tt)
	settle(t, tt, "页面停在那一个重试按钮上", func() bool { return tt.HasText("加载失败，点此重试") })
	if s.laterPage.rows.Status().Loading {
		t.Error("失败之后还在转圈")
	}
}

// TestTheSearchWatchStopsWhenThePageIsLeft 盯防抖那个钟不会醒到进程退出。
//
// 用户敲了两个字、还没到 500ms 就切走了：那个每秒醒四次的定时器必须跟着停。
// 停不掉的话用户看不出任何异样，只是这个应用从那以后再没省过电。
func TestTheSearchWatchStopsWhenThePageIsLeft(t *testing.T) {
	f := newLibFake(t).withLater(1, 1, "甲")
	tt, s := libraryShell(t, f, laterPath)
	settle(t, tt, "把第一页拉回来", func() bool { return rowsShown(&s.laterPage) == 1 })

	typeKeyword(t, tt, "周")
	if s.searchWatchStop == nil {
		t.Fatal("敲了字却没有排防抖的钟——防抖靠它到点")
	}

	s.navigate("/download-list")
	tt.Frame()
	if s.searchWatchStop != nil {
		t.Error("切走了那个钟还醒着")
	}
	// 醒着的时候它只该醒一次（推进 500ms 会走四跳），醒着的时候推进多少都不会有
	// 新的请求。
	before := f.callsOf(laterPathAPI)
	advance(s, 5*time.Second)
	tt.Frame()
	if got := f.callsOf(laterPathAPI); got != before {
		t.Errorf("切走之后那个防抖还在发请求（%d → %d）", before, got)
	}
}

// scrollToBottom 是「一次滚到底」。
func scrollToBottom(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	for range 4 {
		tt.Scroll(600, 400, 0, 100000)
		tt.Frame()
	}
}
