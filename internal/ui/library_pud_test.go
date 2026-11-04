package ui

// 稍后再看 / 历史记录的原型样式（T7）：行高、「专辑」列的文字、按日分组、日期范围弹层。

import (
	"testing"
)

// TestTheLaterRowsAreSixtyHighAndShowHowFarYouGot 钉住稍后再看的行：高 60（原型 `.trow`），
// 「专辑」那一列写「还没看 / 看到 m:ss / 已看完」。
func TestTheLaterRowsAreSixtyHighAndShowHowFarYouGot(t *testing.T) {
	f := newLibFake(t).withLater(1, 2, "甲", "乙")
	tt, s := libraryShell(t, f, laterPath)
	waitFrames(t, tt, "把内容拉回来", func() bool { return rowsShown(&s.laterPage) == 2 })
	tt.Frame()

	a, ok1 := tt.Find("甲")
	b, ok2 := tt.Find("乙")
	if !ok1 || !ok2 {
		t.Fatalf("行没画出来，文字有 %q", tt.Texts())
	}
	if d := b.Y - a.Y; d < 59.5 || d > 60.5 {
		t.Errorf("两行相隔 %.1f，want 60（原型 .trow 高 60）", d)
	}
	if !tt.HasText("还没看") {
		t.Errorf("「专辑」那一列没有写「还没看」，文字有 %q", tt.Texts())
	}
	for _, want := range []string{"稍后再看", "播放全部", "播放 / 弹幕"} {
		if !tt.HasText(want) {
			t.Errorf("稍后再看上没有 %q", want)
		}
	}
}

// TestTheHistoryIsGroupedByDay 钉住历史记录按日分组：组头「今天」「N 首」，行里是「HH:MM 播放」，
// 且没有曲目表头。
func TestTheHistoryIsGroupedByDay(t *testing.T) {
	f := newLibFake(t).withHistory(1, 754, 2700, "甲", "乙")
	tt, s := libraryShell(t, f, historyPath)
	waitFrames(t, tt, "把内容拉回来", func() bool { return rowsShown(&s.historyPage) == 2 })
	tt.Frame()

	for _, want := range []string{"今天", "2 首", "21:04 播放", "全部时间"} {
		if !tt.HasText(want) {
			t.Errorf("历史记录上没有 %q，文字有 %q", want, tt.Texts())
		}
	}
	if tt.HasText("播放 / 弹幕") {
		t.Errorf("历史记录不该有曲目表头（原型 noHead），文字有 %q", tt.Texts())
	}
}

// TestTheDateRangePopoverListsThePresets 钉住日期范围弹层：按钮上写「全部时间」，点开后是
// 原型的五个预设加开始 / 结束两个输入框。
func TestTheDateRangePopoverListsThePresets(t *testing.T) {
	f := newLibFake(t).withHistory(1, 754, 2700, "甲")
	tt, _ := libraryShell(t, f, historyPath)
	tt.Frame()
	if err := tt.Click("全部时间"); err != nil {
		t.Fatalf("点不到日期范围：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
	for _, want := range []string{"按日期筛", "今天", "最近 7 天", "最近 14 天", "最近 30 天", "不按日期筛", "应用"} {
		if !tt.HasText(want) {
			t.Errorf("弹层里没有 %q，文字有 %q", want, tt.Texts())
		}
	}
}
