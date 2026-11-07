package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// TestTheSettingsNavFitsANarrowWindow 锁定原型 index.html:931-933：窗口 ≤ 1024 时设置导航横排，
// 每项按内容宽、项间缝 8，六项都摆得下、点得到（曾经第一项无条件撑满整行，其余项被挤出窗口）。
func TestTheSettingsNavFitsANarrowWindow(t *testing.T) {
	b := newTestShell(t, at(960, 800), withSettings(app.DefaultSettings("")))
	s, tt := b.s, b.tt
	s.navigate(settingsPath)
	waitFrames(t, tt, "设置页", func() bool { return tt.HasText("常规") })
	homeFrame(tt)

	var prev float32
	for i, tb := range settingsTabs {
		r, ok := tt.Find(tb.label)
		if !ok {
			t.Fatalf("导航里没有「%s」", tb.label)
		}
		if r.X+r.W > 960 {
			t.Errorf("「%s」右缘 %.0f 出了 960 宽的窗口", tb.label, r.X+r.W)
		}
		if r.W > 200 {
			t.Errorf("「%s」宽 %.0f——横排时每项该按内容宽，不该撑满", tb.label, r.W)
		}
		if i > 0 {
			if gap := r.X - prev; gap < 7.5 || gap > 8.5 {
				t.Errorf("「%s」和前一项之间 %.1f，want 8", tb.label, gap)
			}
		}
		prev = r.X + r.W
	}
	if err := tt.Click("关于"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.settings.tab != settingsTabAbout {
		t.Errorf("点了「关于」，当前 Tab 是 %d", s.settings.tab)
	}
}
