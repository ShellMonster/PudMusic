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

	"github.com/ShellMonster/Biu_Go/internal/app"
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
	waitFrames(t, tt, "把第一页拉回来", func() bool { return rowsShown(&s.laterPage) == 40 })
	if got := f.callsOf(laterPathAPI); got != 1 {
		t.Fatalf("第一页还没滚到底就发了 %d 次请求——那是③，不是我要验的那条", got)
	}

	scrollToBottom(t, tt)
	waitFrames(t, tt, "滚到底之后自动续拉", func() bool { return rowsShown(&s.laterPage) == 60 })
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
	waitFrames(t, tt, "把第一页拉回来", func() bool { return rowsShown(&s.laterPage) >= 2 })

	waitFrames(t, tt, "没滚过也把第二页补上来了", func() bool { return rowsShown(&s.laterPage) == 3 })
	if !tt.HasText("第二页第一首") {
		t.Errorf("第二页没画出来，文字有 %q", tt.Texts())
	}
}

// TestAFailingPageOffersARetry 盯「拉失败」不是永远转圈。
func TestAFailingPageOffersARetry(t *testing.T) {
	// 第二页装成一次业务错误：B 站的错都在 HTTP 200 里面。
	f := newLibFake(t).withLater(1, 60, manyTitles(1, 40)...)
	f.withLater(2, 60, manyTitles(2, 20)...)
	f.mu.Lock()
	f.later[2] = refused
	f.mu.Unlock()

	tt, s := libraryShell(t, f, laterPath)
	waitFrames(t, tt, "把第一页拉回来", func() bool { return rowsShown(&s.laterPage) == 40 })

	scrollToBottom(t, tt)
	waitFrames(t, tt, "页面停在那一个重试按钮上", func() bool { return tt.HasText("加载失败，点此重试") })
	if s.laterPage.rows.Status().Loading {
		t.Error("失败之后还在转圈")
	}
}

// TestTheDebounceRestartsOnEveryKeystroke 盯防抖是从**最后一个字**算起的。
//
// 用户慢慢打的时候（「周」想了两秒才接着打「杰伦」），从第一个字算起会在中途发出去
// 一个还没输完的词——上游 `useDebounceFn(wait: 500)` 是每个输入都重置那个钟的。
//
// 判据是**距离最后一次输入过了多久**，不是「有没有搜过」。
func TestTheDebounceRestartsOnEveryKeystroke(t *testing.T) {
	f := newLibFake(t).withLater(1, 1, "甲")
	tt, s := libraryShell(t, f, laterPath)
	waitFrames(t, tt, "把第一页拉回来", func() bool { return f.callsOf(laterPathAPI) == 1 })

	typeKeyword(t, tt, "周")
	advance(s, 400*time.Millisecond)
	tt.Frame()
	// 又敲了一个字，框里现在是「周杰」。
	// ⚠️ 这一下重新 `typeKeyword`（点一下 + `Type`）而不是接着敲：无头 Tester 的
	// `Type` 是**替换**框里的内容，`Compose` 是输入法事件（在桌面上是空操作），
	// 所以「用户又敲了一个字」在这里唯一能如实建模的就是「框里的字变成了新的那句」。
	// 这一层看到的也正是这个：`draft` 变了。
	typeKeyword(t, tt, "周杰") // 上一个字的 400ms 已经走掉了，这一下从头再等
	advance(s, 125*time.Millisecond)
	tt.Frame()

	// ⚠️ 请求是**异步**的（`pagedRows` 在后台 goroutine 里拉），所以这里必须给它
	// 一点时间再问「有没有发出去」。不等就是拿一个还没落地的请求去断言「没发」——
	// 那条断言永远不会红，而它正是这条测试的全部意义。
	time.Sleep(150 * time.Millisecond)
	if got := f.callsOf(laterPathAPI); got != 1 {
		t.Fatalf("离最后一次输入才 125ms 就搜了（共 %d 次请求）——防抖的钟没有重打", got)
	}

	// 再等够 500ms 才真的搜出去。
	advance(s, 400*time.Millisecond)
	waitFrames(t, tt, "距最后一次输入满 500ms 之后搜出去", func() bool { return f.callsOf(laterPathAPI) == 2 })
	if got := f.queriesOf(laterPathAPI)[1].Get("key"); got != "周杰" {
		t.Errorf("搜出去的词是 %q，want 周杰（两个字的都要）", got)
	}
}

// TestTheSearchWatchStopsWhenThePageIsLeft 盯防抖那个钟不会醒到进程退出。
//
// 用户敲了两个字、还没到 500ms 就切走了：那个每秒醒四次的定时器必须跟着停。
// 停不掉的话用户看不出任何异样，只是这个应用从那以后再没省过电。
func TestTheSearchWatchStopsWhenThePageIsLeft(t *testing.T) {
	f := newLibFake(t).withLater(1, 1, "甲")
	tt, s := libraryShell(t, f, laterPath)
	waitFrames(t, tt, "把第一页拉回来", func() bool { return rowsShown(&s.laterPage) == 1 })

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

// TestChangingTheFilterWhilePagesAreFetchingIsNotADataRace 盯快速换条件那条路上的
// 数据竞争。
//
// 🔴 这条**只有 `-race` 才验得出来**：拉页在后台 goroutine 里，而关键词和日期范围
// 是界面上随时在改的字段。后台去读它们就是竞争，界面上多半是「某一次筛选的结果配上
// 了另一次的词」——那种错不看 `-race` 永远发现不了，而且不崩不报错。
//
// 做法和评审给的那条一样：连着换几百次条件，每一次都走一遍生产的
// `Reload → load → 后台 fetch` 那条路。
func TestChangingTheFilterWhilePagesAreFetchingIsNotADataRace(t *testing.T) {
	for _, path := range []string{laterPath, historyPath} {
		t.Run(path, func(t *testing.T) {
			f := newLibFake(t).
				withLater(1, 40, manyTitles(1, 40)...).
				withHistory(1, 100, 600, manyTitles(1, 40)...)
			tt, s := libraryShell(t, f, path)
			p := s.pageOf(path)
			waitFrames(t, tt, "把第一页拉回来", func() bool { return rowsShown(p) > 0 })

			// 每一轮都是「换条件 → Reload → 后台 fetch 正在跑 → 再换」。
			for i := range 200 {
				p.dates.setPreset(i%4, s.clock.Now())
				p.draft, p.seenDraft = fmt.Sprintf("词%d", i), ""
				p.applyKeyword(p.draft)
				p.apply()
				tt.Frame()
			}
			waitFrames(t, tt, "最后那一页落地", func() bool { return !p.rows.Status().Loading })
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 窄窗口
// ─────────────────────────────────────────────────────────────────────────────

// TestTheFiltersDoNotOverlapAtTheMinimumWindowWidth 盯 900 宽（`WindowOptions.MinWidth`）。
//
// 上游把日期选择器和搜索框摆在同一排（`justify-between`），那一排在我们的页面区
// 摆不下——**后果不是挤一挤，是叠在一起**：结束日期的框和搜索框重叠，点结束日期点
// 的是搜索框，它的日历打不开。所以搜索框另起一行（`viewFilters`）。
//
// 判据是**两个控件的矩形不相交**：元素都「在画面上」，只有量几何才发现得了它压住了
// 另一个。
func TestTheFiltersDoNotOverlapAtTheMinimumWindowWidth(t *testing.T) {
	f := newLibFake(t).withLater(1, 1, "甲")
	tt, _ := libraryShell(t, f, laterPath)

	for _, w := range []int{900, 1000, 1200} {
		tt.SetSize(w, 800)
		tt.Frame()

		end, ok := tt.Find("结束日期")
		if !ok {
			t.Fatalf("宽度 %d 下找不到结束日期，文字有 %q", w, tt.Texts())
		}
		box, ok := tt.Find(keywordBoxLabel)
		if !ok {
			t.Fatalf("宽度 %d 下找不到搜索框，文字有 %q", w, tt.Texts())
		}
		if overlap(end, box) {
			t.Errorf("宽度 %d 下搜索框盖住了结束日期（搜索框 %+v，结束日期 %+v）", w, box, end)
		}
	}
}

// TestTheEndDateOpensItsCalendarAtTheMinimumWindowWidth 钉住「点得开」。
//
// 光量不重叠不够：控件在、位置对，可点击落在别人身上，它那个日历照样打不开——
// 而用户缩小窗口正是为了点点什么。
func TestTheEndDateOpensItsCalendarAtTheMinimumWindowWidth(t *testing.T) {
	f := newLibFake(t).withLater(1, 1, "甲")
	tt, _ := libraryShell(t, f, laterPath)
	tt.SetSize(900, 800)
	tt.Frame()

	if err := tt.Click("结束日期"); err != nil {
		t.Fatalf("点不到结束日期：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
	if !tt.HasText("October 2026") && !tt.HasText("2026年10月") {
		t.Errorf("900 宽下点结束日期没有打开日历，文字有 %q", tt.Texts())
	}
}

// overlap 报两个矩形有没有交叠（边挨着不算）。
func overlap(a, b mygoui.Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

// ─────────────────────────────────────────────────────────────────────────────
// 「记录播放历史」那个开关
// ─────────────────────────────────────────────────────────────────────────────

// TestTheReportHistorySwitchReallyWritesTheSetting 盯那个开关改的是**盘上那份**。
//
// 🔴 它的失败方式最阴：只绑定局部变量、不写设置，用户按了开关弹回去，界面上看不出
// 任何异样——「按了没反应」里最难查的一种。判据是设置里的那个键，不是画面上开关的
// 位置。
func TestTheReportHistorySwitchReallyWritesTheSetting(t *testing.T) {
	f := newLibFake(t).withHistory(1, 100, 600, "听过的那首")

	// 挂一份真的设置（和 `shellOnSettings` 同一套路），否则 applySetting 写不出去。
	cli := biliForTest(t, f)
	b := newTestShell(t,
		withSettings(app.DefaultSettings("")),
		mounted(func(_ *testing.T, a *app.App) { a.MountLibrary(cli) }),
		withClock(newFakeClock(now)))
	a, s, tt := b.a, b.s, b.tt
	path := b.settingsPath
	tt.Frame()
	s.navigate(historyPath)
	tt.Frame()

	if !a.SettingsSnapshot().ReportPlayHistory {
		t.Fatalf("起点就不对：设置里 reportPlayHistory 已经是关的")
	}
	if err := tt.Click("记录播放历史"); err != nil {
		t.Fatalf("点不到那个开关：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()

	if a.SettingsSnapshot().ReportPlayHistory {
		t.Error("点了「记录播放历史」，设置里那个键没变——开关只改了一个局部变量")
	}
	// 存住了：下一次打开这个应用还是关的（第一遍写进内存，第二遍落盘）。
	if err := a.FlushSettings(); err != nil {
		t.Fatal(err)
	}
	saved, err := app.LoadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ReportPlayHistory {
		t.Error("那个键没有落盘——关掉窗口再打开它又回来了")
	}
}
