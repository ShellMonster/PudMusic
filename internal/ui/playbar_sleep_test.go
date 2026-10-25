package ui

import "testing"

func TestTheSleepMenuSetsAndClearsTheTimer(t *testing.T) {
	tt, a, s := rateBarTester(t, 900)

	if err := tt.Click("睡眠定时"); err != nil {
		t.Fatalf("点不开睡眠定时：%v（画面：%q）", err, tt.Texts())
	}
	if !tt.HasText("30 分钟后停") || !tt.HasText("放完这一首就停") {
		t.Fatalf("浮层缺档位：%q", tt.Texts())
	}
	if err := tt.Click("30 分钟后停"); err != nil {
		t.Fatal(err)
	}
	if a.Snapshot().SleepAt.IsZero() || s.sleepOpen {
		t.Fatalf("选完之后：SleepAt=%v open=%v", a.Snapshot().SleepAt, s.sleepOpen)
	}
	tt.Frame()

	if err := tt.Click("睡眠定时"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("关闭定时") {
		t.Fatalf("设了定时之后浮层里没有「关闭定时」：%q", tt.Texts())
	}
	if err := tt.Click("关闭定时"); err != nil {
		t.Fatal(err)
	}
	if !a.Snapshot().SleepAt.IsZero() {
		t.Error("点了关闭定时，定时器还在")
	}
}
