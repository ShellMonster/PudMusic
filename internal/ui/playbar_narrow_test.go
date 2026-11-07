package ui

import (
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

// pbWideW 是「播放条按宽排摆全部键」的测试窗口宽（> 1024，原型 index.html:938 断点之上）。
const pbWideW = 1200

// pbShell 建一个放着本地 FLAC 的外壳（窗口 w×800）。
func pbShell(t *testing.T, w int) (*Shell, *ui.Tester) {
	t.Helper()
	b := newTestShell(t, at(w, 800), withSettings(app.DefaultSettings("")))
	openATrack(t, b.s)
	homeFrame(b.tt)
	return b.s, b.tt
}

// settlePop 推钟让弹层的 PopIn 走完（Tester 的钟站着不动）。
func settlePop(tt *ui.Tester) {
	tt.Frame()
	tt.Advance(400 * time.Millisecond)
	homeFrame(tt)
}

// TestThePlayBarDropsSecondaryKeysInANarrowWindow 锁定原型 index.html:938-939 / app.js:920-933：
// 窗口 ≤ 1024 时播放条藏起模式、睡眠、倍速、静音和音量滑块，歌词和队列还在；宽窗口全都在。
func TestThePlayBarDropsSecondaryKeysInANarrowWindow(t *testing.T) {
	hidden := []string{"播放模式", "睡眠定时", "播放速率", "静音", "音量"}
	kept := []string{"歌词", "播放列表", "上一首", "下一首"}

	_, wide := pbShell(t, 1280)
	for _, l := range append(append([]string{}, hidden...), kept...) {
		if _, ok := wide.Find(l); !ok {
			t.Errorf("1280 宽时播放条上没有「%s」", l)
		}
	}

	_, narrow := pbShell(t, 960)
	for _, l := range hidden {
		if _, ok := narrow.Find(l); ok {
			t.Errorf("960 宽时播放条上还有「%s」（原型 .hide-md 应该藏起来）", l)
		}
	}
	for _, l := range kept {
		if _, ok := narrow.Find(l); !ok {
			t.Errorf("960 宽时播放条上丢了「%s」", l)
		}
	}
}

// TestTheSleepPopChecksTheCurrentStep 锁定原型 app.js:984：睡眠弹层里当前那一档打勾
// （名字换 accent-text），别的档不打（曾经恒为 false，一档都不勾）。
func TestTheSleepPopChecksTheCurrentStep(t *testing.T) {
	s, tt := pbShell(t, 1280)
	s.app.SetSleepTimer(30 * time.Minute)
	t.Cleanup(s.app.CancelSleepTimer)
	homeFrame(tt)
	if err := tt.Click("睡眠定时"); err != nil {
		t.Fatal(err)
	}
	settlePop(tt)

	accent := tok.Of(BuildTheme(s.app.SettingsSnapshot(), false)).AccentText
	on, ok := tt.Find("睡眠定时 30 分钟后暂停")
	if !ok {
		t.Fatalf("睡眠弹层没有 30 分钟那一档，文字 %q", tt.Texts())
	}
	off, _ := tt.Find("睡眠定时 15 分钟后暂停")
	if n := countNear(tt.Image(), on, accent); n == 0 {
		t.Error("当前那一档（30 分钟）没有打勾 / 没有染 accent-text")
	}
	if n := countNear(tt.Image(), off, accent); n != 0 {
		t.Errorf("没选的 15 分钟那一档有 %d 个 accent 像素", n)
	}
}

// TestPlayBarPopsNearTheRightEdgeAlignRight 锁定原型 app.js:1017：按钮右缘离窗口右边 < 320
// 时弹层贴按钮右缘，否则贴左缘（曾经一律左对齐，靠右的弹层往窗口外挤）。
func TestPlayBarPopsNearTheRightEdgeAlignRight(t *testing.T) {
	s, tt := pbShell(t, 1280)

	rate, _ := tt.Find("播放速率")
	if err := tt.Click("播放速率"); err != nil {
		t.Fatal(err)
	}
	settlePop(tt)
	pop, ok := tt.Find("倍速")
	if !ok {
		t.Fatalf("倍速弹层没开，文字 %q", tt.Texts())
	}
	if d := (pop.X + pop.W) - (rate.X + rate.W); d < -1 || d > 1 {
		t.Errorf("倍速键右缘 %.1f、弹层右缘 %.1f：靠右的弹层该右对齐", rate.X+rate.W, pop.X+pop.W)
	}
	s.rateOpen = false
	homeFrame(tt)

	sleep, _ := tt.Find("睡眠定时")
	if err := tt.Click("睡眠定时"); err != nil {
		t.Fatal(err)
	}
	settlePop(tt)
	// 键和弹层面板同名「睡眠定时」，Find 先找到键；拿第一档的行判左缘（行在面板里左右各缩 8）。
	row, ok := tt.Find("睡眠定时 15 分钟后暂停")
	if !ok || !s.sleepOpen {
		t.Fatalf("睡眠弹层没开")
	}
	if d := row.X - 8 - sleep.X; d < -1 || d > 1 {
		t.Errorf("睡眠键左缘 %.1f、弹层左缘 %.1f：离右边远的弹层该左对齐", sleep.X, row.X-8)
	}
}
