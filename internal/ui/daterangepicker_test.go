package ui

// 这一组盯的是**双月日期选择器**（票 #176 / G34）。
//
// 判据是**画面上的东西**：弹出来的是不是**两个月**、翻月是不是**两块一起
// 动**、挑过的那一天有没有被**高亮**、点一个更早的日子是不是**从头再挑**。
//
// 「挑完会不会真的换条件」那一条在 `librarypage_test.go` 的
// `TestTheRangePickerDrivesTheFilter`（它要连着网络夹具）。

import (
	"image/color"
	"strconv"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/egoist/mygo/ui"
)

// openRangePicker 是「点开日期范围那一块」那一下。
func openRangePicker(t *testing.T, tt *ui.Tester) {
	t.Helper()
	if err := tt.Click(dateRangeLabel); err != nil {
		t.Fatalf("点不到日期范围：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
}

// TestTheRangePickerShowsTwoMonths 钉住「弹出来的是**两个月**」。
//
// 上游是 `visibleMonths={2}`（`date-range-picker/index.tsx:80`）。
//
// ⚠️ **两个月都要在窗口里**：面板有 500 宽，而窗口最窄是 900——面板要是往右
// 长，右半边（正是右边那个月）会被裁掉，而**裁掉的那一半看不出来**：用户
// 看到的是「这个日历只有一个月」。
func TestTheRangePickerShowsTwoMonths(t *testing.T) {
	tt, s := libraryShell(t, newLibFake(t).withLater(1, 1, "甲"), laterPath)
	waitFrames(t, tt, "把第一页拉回来", func() bool { return rowsShown(s.pageOf(laterPath)) > 0 })

	openRangePicker(t, tt)
	for _, want := range []string{"2026年10月", "2026年11月"} {
		if !tt.HasText(want) {
			t.Fatalf("日历里没有 %q，文字有 %q", want, tt.Texts())
		}
	}
	// 右边那个月的最后一天也要**点得到**（坐标在窗口里）。
	last, ok := tt.Find("2026年11月30日")
	if !ok {
		t.Fatalf("右边那个月没有画出来，文字有 %q", tt.Texts())
	}
	if last.X+last.W > 900 {
		t.Errorf("右边那个月的最后一天在窗口外面（右边 %v，窗口 900）", last.X+last.W)
	}
}

// TestTheTwoMonthsMoveTogether 钉住「翻月是**两块一起动**」。
//
// 上游那两个月的导航是一套（不是各翻各的）——而这一条最容易被做成「左边那个
// 月自己翻、右边那个月不动」。
func TestTheTwoMonthsMoveTogether(t *testing.T) {
	tt, s := libraryShell(t, newLibFake(t).withLater(1, 1, "甲"), laterPath)
	waitFrames(t, tt, "把第一页拉回来", func() bool { return rowsShown(s.pageOf(laterPath)) > 0 })

	openRangePicker(t, tt)
	if err := tt.Click("›"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"2026年11月", "2026年12月"} {
		if !tt.HasText(want) {
			t.Fatalf("往后翻了一个月之后没有 %q，文字有 %q", want, tt.Texts())
		}
	}
	if tt.HasText("2026年10月") {
		t.Errorf("往后翻了之后左边那个月还是 10 月，文字有 %q", tt.Texts())
	}

	// 再翻回来。
	if err := tt.Click("‹"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"2026年10月", "2026年11月"} {
		if !tt.HasText(want) {
			t.Fatalf("翻回来之后没有 %q，文字有 %q", want, tt.Texts())
		}
	}
}

// TestThePickedDayIsHighlighted 钉住「挑过的那一天被高亮」。
//
// 上游那个范围选择器把区间的**两头**画成实心主色、中间画成主色 20% 的一层
// （`data-[selected=true]`）。这里断的是**头**——它最显眼，也最容易漏。
//
// ⚠️ **取的是格子左边那一小块**：格子中间是那个日子（字是主色上的白字），
// 中心量到的是**字**不是底。
func TestThePickedDayIsHighlighted(t *testing.T) {
	tt, s := libraryShell(t, newLibFake(t).withLater(1, 1, "甲"), laterPath)
	waitFrames(t, tt, "把第一页拉回来", func() bool { return rowsShown(s.pageOf(laterPath)) > 0 })

	openRangePicker(t, tt)
	if err := tt.Click("2026年10月3日"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	// ⚠️ **先把指针挪开**：无头 Tester 的指针停在最后点的那一格上，而悬停
	// 那一层是盖在底色上的（`widgets/flat` 的 `feel`：正文色 8% 的一层）——
	// 那一格量出来是 {30 200 90} 而不是主色 {30 215 96}，差的是那一层。
	tt.Move(5, 5)
	tt.Frame()

	cell, ok := tt.Find("2026年10月3日")
	if !ok {
		t.Fatalf("日历上没有 10 月 3 号，文字有 %q", tt.Texts())
	}
	accent := BuildTheme(app.DefaultSettings(""), false).Accent
	got := probeColor(tt, int(cell.X)+3, int(cell.Y+cell.H/2))
	if maxDiff(got, color.RGBA{R: accent.R, G: accent.G, B: accent.B, A: accent.A}) > probeTolerance {
		t.Errorf("挑过的那一天是 %v，want 主色 %v——起始那天没有被高亮", got, accent)
	}
}

// TestNothingIsHighlightedWhileNothingIsFiltered 钉住「没在筛的时候日历上
// 什么都不亮」。
//
// ⚠️ **这一条防的是「今天被高亮」那一种骗**：不筛的时候 `from` / `to` 里摆着
// 的是今天（`setPreset` 摆的，好让用户知道从哪儿开始改），而用户看着日历上
// 今天被高亮，会以为今天正在筛——而实际上什么都没筛。
//
// 上游那边是同一个道理：手动挑的那一份在选预设时就被清空了
// （`date-range-picker/index.tsx:17` 的 `setDateRange(null)`）。
func TestNothingIsHighlightedWhileNothingIsFiltered(t *testing.T) {
	tt, s := libraryShell(t, newLibFake(t).withLater(1, 1, "甲"), laterPath)
	waitFrames(t, tt, "把第一页拉回来", func() bool { return rowsShown(s.pageOf(laterPath)) > 0 })

	// 前提：真的没在筛。
	if !tt.HasText("不按日期筛") {
		t.Fatalf("默认就没有说清不筛，文字有 %q", tt.Texts())
	}
	openRangePicker(t, tt)

	// ⚠️ **先把指针挪开**（悬停那一层会盖在底色上，见上面那条）。
	tt.Move(5, 5)
	tt.Frame()

	accent := BuildTheme(app.DefaultSettings(""), false).Accent
	want := color.RGBA{R: accent.R, G: accent.G, B: accent.B, A: accent.A}
	for _, day := range []string{"2026年10月16日", "2026年10月17日", "2026年10月18日"} {
		cell, ok := tt.Find(day)
		if !ok {
			t.Fatalf("日历上没有 %q，文字有 %q", day, tt.Texts())
		}
		got := probeColor(tt, int(cell.X)+3, int(cell.Y+cell.H/2))
		if maxDiff(got, want) <= probeTolerance {
			t.Errorf("%s 那一格是主色 %v——没在筛的时候日历上不该高亮任何一天", day, got)
		}
	}
}

// TestAManualRangeThatEqualsTodayIsNotDropped 钉住「手动挑的那一段**恰好就是
// 默认摆着的那一天**时，不会被静默丢掉」。
//
// 🔴 **这是评审挑出来的一条**：不筛的时候 `from` / `to` 里摆着的就是今天
// （`setPreset` 摆的，好让用户知道从哪儿开始改），而用户手动挑的恰好也是今天
// 的话，挑完那四个值**一个都没变**——于是「变了没」那一句认不出来，这一次手动
// 挑被**静默丢掉**：页面不重拉、还写着「不按日期筛」、触发键还写着「选择日期
// 范围」。用户明明挑了「今天」，而界面上说什么都没发生。
//
// 判据：**查询真的带上了那两天**（`add_time_start` / `add_time_end`），而且
// 界面上不再写着「不按日期筛」。
//
// ⚠️ 「今天」是**筛选器自己摆着的那一天**，测试里从它的状态读出来——
// 不是从系统时钟算的：`buildLibraryPages` 是在 `New` 里跑的（那时时钟还是
// 真时钟），而这一页的假时钟是外壳建好之后才换上的，所以「摆着的那一天」
// 和「页面时钟说的今天」**不是同一天**。读状态才准。
func TestAManualRangeThatEqualsTodayIsNotDropped(t *testing.T) {
	f := newLibFake(t).withLater(1, 1, "甲")
	tt, s := libraryShell(t, f, laterPath)
	waitFrames(t, tt, "把第一页拉回来", func() bool { return f.callsOf(laterPathAPI) == 1 })

	// 前提：真的没在筛（而默认摆着的就是那一天）。
	if !tt.HasText("不按日期筛") {
		t.Fatalf("默认就没有说清不筛，文字有 %q", tt.Texts())
	}
	d := s.pageOf(laterPath).dates
	today := startOfDay(d.from)
	dayLabel := today.Format("2006年1月2日")

	openRangePicker(t, tt)
	// 挑那一天两次：起始和结束都是它。
	if err := tt.Click(dayLabel); err != nil {
		t.Fatalf("日历上点不到 %q：%v，文字有 %q", dayLabel, err, tt.Texts())
	}
	tt.Frame()
	if err := tt.Click(dayLabel); err != nil {
		t.Fatalf("日历上点不到 %q（第二下）：%v，文字有 %q", dayLabel, err, tt.Texts())
	}
	tt.Frame()

	waitFrames(t, tt, "按手动挑的日期重新拉", func() bool { return f.callsOf(laterPathAPI) == 2 })
	q := f.queriesOf(laterPathAPI)[1]
	if got, want := q.Get("add_time_start"), strconv.FormatInt(today.Unix(), 10); got != want {
		t.Errorf("手动挑了那一天之后 add_time_start = %q，want %q——用户的选择被丢掉了",
			got, want)
	}
	if got, want := q.Get("add_time_end"),
		strconv.FormatInt(endOfDay(today).Unix(), 10); got != want {
		t.Errorf("手动挑了那一天之后 add_time_end = %q，want %q——用户的选择被丢掉了",
			got, want)
	}
	if tt.HasText("不按日期筛") {
		t.Errorf("手动挑了那一天，界面上还说着「不按日期筛」，文字有 %q", tt.Texts())
	}
	if want := today.Format("2006-01-02") + " ~ " + today.Format("2006-01-02"); !tt.HasText(want) {
		t.Errorf("手动挑了那一天，界面上没写出 %q，文字有 %q", want, tt.Texts())
	}
}

// TestPickingAnEarlierDayStartsOver 钉住「第二次点到了更早的一天 → 从头再挑」。
//
// 上游的 DateRangePicker 也是这样：第二次点的那天**早于**第一次点的那天时，
// 它把新点的当成新的起始（而不是把区间翻过来）。
//
// ⚠️ **翻过来是更坏的一种**：用户点「3 号」再点「20 号」是要 3 号到 20 号；
// 而他先点 20 号再点 3 号，意思多半也是「3 号开始」——把区间翻成 20 号到
// 3 号（**空的**）是用户根本没要的东西，而那种错在界面上完全看不出来
// （区间那一段照样高亮着）。
func TestPickingAnEarlierDayStartsOver(t *testing.T) {
	tt, s := libraryShell(t, newLibFake(t).withLater(1, 1, "甲"), laterPath)
	waitFrames(t, tt, "把第一页拉回来", func() bool { return rowsShown(s.pageOf(laterPath)) > 0 })

	openRangePicker(t, tt)
	if err := tt.Click("2026年10月20日"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("2026年10月3日"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	// ⚠️ 先把指针挪开（悬停那一层会盖在底色上，见上面那条）。
	tt.Move(5, 5)
	tt.Frame()

	// 起始变成了 3 号——**高亮在 3 号上**，而不在 20 号上。
	accent := BuildTheme(app.DefaultSettings(""), false).Accent
	early, ok := tt.Find("2026年10月3日")
	if !ok {
		t.Fatalf("日历上没有 10 月 3 号，文字有 %q", tt.Texts())
	}
	late, ok := tt.Find("2026年10月20日")
	if !ok {
		t.Fatalf("日历上没有 10 月 20 号，文字有 %q", tt.Texts())
	}
	earlyPx := probeColor(tt, int(early.X)+3, int(early.Y+early.H/2))
	latePx := probeColor(tt, int(late.X)+3, int(late.Y+late.H/2))
	if maxDiff(earlyPx, color.RGBA{R: accent.R, G: accent.G, B: accent.B, A: accent.A}) > probeTolerance {
		t.Errorf("3 号那格是 %v，want 主色 %v——点了更早的一天之后没有从头再挑", earlyPx, accent)
	}
	if maxDiff(latePx, color.RGBA{R: accent.R, G: accent.G, B: accent.B, A: accent.A}) <= probeTolerance {
		t.Errorf("20 号那格还是主色（%v）——它不该再是区间的一头", latePx)
	}
}
