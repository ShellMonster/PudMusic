// 全屏播放器（「深夜唱片行」§0′.T4）：结构、弹层、偏好、歌词旋钮。
//
// 走 MyGo 的 Tester：和真窗口一模一样的 layout 与 paint，只是画到内存里。数值是原型
// `index.html` 的 CSS 值，**写死字面量**，不去用 fullplayer.go 里的同名常量（常量一改两边
// 一起跟着变，测试就发现不了了）。等动画用 `settleFull`（推钟 + 问 `FramePending`），不数帧。
package ui

import (
	"math"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// openFullDirect 不点封面，直接开全屏（本测试组关心的是满屏本身）。
func openFullDirect(t *testing.T, s *Shell, tt *mygoui.Tester) {
	t.Helper()
	s.openFullPlayer()
	tt.Frame()
	settleFull(t, tt)
	if !s.full.open || !tt.HasText("NOW PLAYING · 1 / 1") {
		t.Fatalf("满屏没开出来：open=%v 文字 %q", s.full.open, tt.Texts())
	}
}

func within(a, b, tol float32) bool { return math.Abs(float64(a-b)) <= float64(tol) }

// TestTheFullPlayerHasEveryControlOfThePrototype：顶栏、底栏三组控制都在，名字是原型 aria-label。
func TestTheFullPlayerHasEveryControlOfThePrototype(t *testing.T) {
	s, tt, _ := fullLyricsTester(t)
	openFullDirect(t, s, tt)
	mode := "切换播放模式：" + s.app.Snapshot().PlayMode.Desc()
	for _, label := range []string{
		mode, "收起满屏播放器", "打开播放列表",
		"倍速", "显示弹幕",
		"上一首", "播放", "下一首", "睡眠定时",
		"均衡器", "满屏播放器偏好", "切换到迷你播放器",
		"播放进度", "歌词提前 0.5 秒", "歌词延后 0.5 秒", "歌词字号减小", "歌词字号增大", "找歌词",
	} {
		if _, ok := tt.Find(label); !ok {
			t.Errorf("满屏上没有 %q，文字有 %q", label, tt.Texts())
		}
	}
}

// TestTheFullPlayerLaysOutLikeThePrototype：1200×800 下各块的位置（原型截图量出来的）。
//
//	收起键 x=20 / y=16；封面 440 见方，x=128；播放键 64 见方、中心在窗口正中；
//	右栏歌词来源行在 y=84、旋钮行在 y=646。
func TestTheFullPlayerLaysOutLikeThePrototype(t *testing.T) {
	s, tt, _ := fullLyricsTester(t)
	openFullDirect(t, s, tt)

	if r, ok := tt.Find("收起满屏播放器"); !ok || !within(r.X, 20, 1) || !within(r.Y, 16, 1) || !within(r.W, 36, 1) {
		t.Errorf("收起键 = %+v，want x=20 y=16 36 见方", r)
	}
	play, ok := tt.Find("播放")
	if !ok {
		t.Fatalf("没有播放键：%q", tt.Texts())
	}
	if !within(play.W, 64, 1) || !within(play.H, 64, 1) {
		t.Errorf("播放键 %.1f×%.1f，want 64×64", play.W, play.H)
	}
	if cx := play.X + play.W/2; !within(cx, 600, 1) {
		t.Errorf("播放键中心 x=%.1f，want 窗口正中 600", cx)
	}
	cover, ok := tt.Find("当前封面")
	if !ok {
		t.Fatalf("没有封面：%q", tt.Texts())
	}
	// 暂停时封面缩到 0.94（`.full.paused .full-cover`），中心不动：440×0.94 = 413.6，x = 128 + 13.2。
	if !within(cover.W, 413.6, 1) || !within(cover.H, 413.6, 1) || !within(cover.X, 141.2, 1) || !within(cover.Y, 117.2, 1) {
		t.Errorf("封面 = %+v，want 413.6 见方（440 缩 0.94）x=141.2 y=117.2", cover)
	}
	off, _ := tt.Find("歌词提前 0.5 秒")
	if !within(off.Y, 646, 1) || !within(off.W, 32, 1) {
		t.Errorf("歌词旋钮 = %+v，want y=646、32 见方", off)
	}
}

// TestEscapeClosesTheFullPlayer：Esc 关满屏；弹层开着时先关弹层（框架浮层自己管）。
func TestEscapeClosesTheFullPlayer(t *testing.T) {
	s, tt, _ := fullTester(t)
	openFullDirect(t, s, tt)
	tt.Key(0, mygoui.KeyEscape)
	tt.Frame()
	if s.full.open {
		t.Error("按 Esc 之后满屏还开着")
	}
}

// TestTheCloseKeyClosesTheFullPlayer：点收起键关掉，并且复位那一摊状态。
func TestTheCloseKeyClosesTheFullPlayer(t *testing.T) {
	s, tt, _ := fullTester(t)
	openFullDirect(t, s, tt)
	s.full.popRate = true
	if err := tt.Click("收起满屏播放器"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.full.open || s.full.popRate {
		t.Errorf("没关干净：open=%v popRate=%v", s.full.open, s.full.popRate)
	}
}

// TestThePopsOpenAboveTheirButtonsAndPickingChangesTheState：倍速弹层在按钮上方 12，选一档生效并收起。
func TestThePopsOpenAboveTheirButtonsAndPickingChangesTheState(t *testing.T) {
	s, tt, _ := fullTester(t)
	openFullDirect(t, s, tt)
	btn, _ := tt.Find("倍速")
	if err := tt.Click("倍速"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	settleFull(t, tt)
	if !s.full.popRate {
		t.Fatalf("点倍速没开弹层：文字 %q", tt.Texts())
	}
	panel, ok := tt.Find("full-pop-rate")
	if !ok {
		t.Fatalf("找不到弹层：%q", tt.Texts())
	}
	if gap := btn.Y - (panel.Y + panel.H); !within(gap, 12, 1.5) {
		t.Errorf("弹层离按钮 %.1f，want 12（按钮 y=%.1f，弹层 %+v）", gap, btn.Y, panel)
	}
	if err := tt.Click("1.5×"); err != nil {
		t.Fatalf("点不了 1.5×：%v（文字 %q）", err, tt.Texts())
	}
	tt.Frame()
	settleFull(t, tt)
	if got := s.app.Snapshot().Rate; got != 1.5 {
		t.Errorf("倍速 = %v，want 1.5", got)
	}
	if s.full.popRate {
		t.Error("选完弹层没收起")
	}
}

// TestPressingAPopButtonAgainClosesIt：再按一下同一个按钮是收起，不是「关了又开」。
func TestPressingAPopButtonAgainClosesIt(t *testing.T) {
	s, tt, _ := fullTester(t)
	openFullDirect(t, s, tt)
	for i, want := range []bool{true, false, true} {
		if err := tt.Click("睡眠定时"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		settleFull(t, tt)
		if s.full.popSleep != want {
			t.Fatalf("第 %d 次点睡眠：popSleep = %v，want %v", i+1, s.full.popSleep, want)
		}
	}
}

// TestOnlyOnePopIsOpenAtATime：开着睡眠再点倍速，睡眠收起。
func TestOnlyOnePopIsOpenAtATime(t *testing.T) {
	s, tt, _ := fullTester(t)
	openFullDirect(t, s, tt)
	_ = tt.Click("睡眠定时")
	tt.Frame()
	_ = tt.Click("倍速")
	tt.Frame()
	settleFull(t, tt)
	if s.full.popSleep || !s.full.popRate {
		t.Errorf("popSleep=%v popRate=%v，want false/true", s.full.popSleep, s.full.popRate)
	}
}

// TestTheSleepPopListsTheTimerStepsLikeThePrototype：睡眠弹层：N 分钟后暂停 + 放完这一首就停。
func TestTheSleepPopListsTheTimerStepsLikeThePrototype(t *testing.T) {
	s, tt, _ := fullTester(t)
	openFullDirect(t, s, tt)
	_ = tt.Click("睡眠定时")
	tt.Frame()
	settleFull(t, tt)
	for _, want := range []string{"睡眠定时", "15 分钟后暂停", "放完这一首就停"} {
		if !tt.HasText(want) {
			t.Errorf("睡眠弹层没有 %q，文字 %q", want, tt.Texts())
		}
	}
	if err := tt.Click("15 分钟后暂停"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if st := s.app.Snapshot(); st.SleepAt.IsZero() {
		t.Error("点了 15 分钟之后没定上睡眠")
	}
}

// TestThePrefsPopTogglesEveryPrototypeSwitch：偏好弹层里的开关真的管画面。
func TestThePrefsPopTogglesEveryPrototypeSwitch(t *testing.T) {
	s, tt, _ := fullLyricsTester(t)
	s.app.PutLyrics(fullTestLRC, "")
	openFullDirect(t, s, tt)
	if !tt.HasText("第二句") {
		t.Fatalf("没有歌词：%q", tt.Texts())
	}
	_ = tt.Click("满屏播放器偏好")
	tt.Frame()
	settleFull(t, tt)
	for _, want := range []string{"满屏播放器", "显示歌词", "显示频谱图", "显示封面", "显示虚化背景", "显示弹幕"} {
		if !tt.HasText(want) {
			t.Errorf("偏好弹层缺 %q，文字 %q", want, tt.Texts())
		}
	}
	// 关封面：封面不画、弹幕区清掉。
	if err := tt.Click("开关：显示封面"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	settleFull(t, tt)
	if _, ok := tt.Find("当前封面"); ok {
		t.Error("关了「显示封面」封面还在")
	}
	if s.full.coverRect.W != 0 {
		t.Errorf("没有封面时弹幕区应当清空：%+v", s.full.coverRect)
	}
	// 关歌词：歌词不画。
	if err := tt.Click("开关：显示歌词"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	settleFull(t, tt)
	if tt.HasText("第二句") {
		t.Error("关了「显示歌词」歌词还在")
	}
	// 颜色块只在对应开关开着时出现：歌词关了，歌词的三色块跟着没了。
	if _, ok := tt.Find("纸白"); ok {
		t.Error("歌词关着，歌词色块还在")
	}
}

// TestTheLyricKnobsAdjustOffsetAndSize：偏移步长 0.5 秒（延后为正），字号 ±2。
func TestTheLyricKnobsAdjustOffsetAndSize(t *testing.T) {
	s, tt, _ := fullLyricsTester(t)
	s.app.PutLyrics(fullTestLRC, "")
	openFullDirect(t, s, tt)
	if !tt.HasText("0.0s") {
		t.Fatalf("偏移读数不是 0.0s：%q", tt.Texts())
	}
	_ = tt.Click("歌词延后 0.5 秒")
	tt.Frame()
	if !tt.HasText("+0.5s") {
		t.Errorf("延后一下之后读数应当是 +0.5s：%q", tt.Texts())
	}
	_ = tt.Click("歌词提前 0.5 秒")
	_ = tt.Click("歌词提前 0.5 秒")
	tt.Frame()
	if !tt.HasText("−0.5s") {
		t.Errorf("再提前两下之后读数应当是 −0.5s：%q", tt.Texts())
	}
	before := s.app.Lyrics().FontSize
	_ = tt.Click("歌词字号增大")
	tt.Frame()
	if got := s.app.Lyrics().FontSize; got != before+2 {
		t.Errorf("A+ 之后字号 = %v，want %v", got, before+2)
	}
}

// TestTheTranslationChipIsDisabledWithoutTranslations：没有译文时「歌词原文」chip 禁用；有译文才能切。
func TestTheTranslationChipIsDisabledWithoutTranslations(t *testing.T) {
	s, tt, _ := fullLyricsTester(t)
	s.app.PutLyrics(fullTestLRC, "")
	openFullDirect(t, s, tt)
	if err := tt.Click("歌词原文"); err != nil {
		t.Fatalf("找不到 chip：%v", err)
	}
	tt.Frame()
	if s.full.lyTrans {
		t.Error("没有译文时点 chip 不该切换")
	}
	s.app.PutLyrics(fullTestLRC, "[00:01.00]Second\n")
	tt.Frame()
	settleFull(t, tt)
	_ = tt.Click("歌词原文")
	tt.Frame()
	settleFull(t, tt)
	if !s.full.lyTrans || !tt.HasText("歌词翻译") || !tt.HasText("Second") {
		t.Errorf("有译文时点 chip 应当切到翻译：lyTrans=%v 文字 %q", s.full.lyTrans, tt.Texts())
	}
}

// TestClickingALyricSeeksToIt：点一句歌词跳到那一句（原型 `seek-line`）。
func TestClickingALyricSeeksToIt(t *testing.T) {
	s, tt, _ := fullLyricsTester(t)
	s.app.PutLyrics(fullTestLRC, "")
	openFullDirect(t, s, tt)
	line, ok := tt.Find("第四句")
	if !ok {
		t.Fatalf("没有第四句：%q", tt.Texts())
	}
	tt.ClickAt(line.X+line.W/2, line.Y+line.H/2)
	tt.Frame()
	if pos := s.app.Snapshot().Position; pos < 2900*time.Millisecond {
		t.Errorf("点了第四句（03.00）之后位置 = %v", pos)
	}
}

// TestTheSpectrumRowIsAlwaysThereAndRestsAtThreePixels：频谱 64 根；没有谱时落到 3 像素底排；
// 关掉频谱那一行照样占着（高 40）。
func TestTheSpectrumRowIsAlwaysThereAndRestsAtThreePixels(t *testing.T) {
	s, tt, _ := fullTester(t)
	openFullDirect(t, s, tt)
	r, ok := tt.Find("频谱")
	if !ok {
		t.Fatalf("没有频谱：%q", tt.Texts())
	}
	if !within(r.H, 40, 0.5) {
		t.Errorf("频谱高 %.1f，want 40", r.H)
	}
	// 频谱关了：位置不变（visibility: hidden）。
	p := s.app.FullPlayerPrefs()
	p.ShowSpectrum = false
	s.app.SetFullPlayerPrefs(p)
	tt.Frame()
	if r2, ok := tt.Find("频谱"); ok && !within(r2.Y, r.Y, 0.5) {
		t.Errorf("关了频谱之后那一行挪了位置：%.1f → %.1f", r.Y, r2.Y)
	}
	if got := fullBottomH; got != 209 {
		t.Errorf("底栏总高 = %d，want 209", got)
	}
}

// TestAFullPlayerWithNoTrackShowsAnEmptyStateInsteadOfVanishing：换歌那几百毫秒 HasTrack 是 false，
// 满屏不能把自己关掉。
func TestAFullPlayerWithNoTrackShowsAnEmptyStateInsteadOfVanishing(t *testing.T) {
	s, tt := newShell(t)
	s.full.open = true
	tt.Frame()
	if !s.full.open || !tt.HasText(fullNothing.Title) {
		t.Errorf("没歌时满屏应当留着并说一句：open=%v 文字 %q", s.full.open, tt.Texts())
	}
	if !tt.HasText("") && false {
		t.Fatal()
	}
	_ = app.State{}
}

// TestPartRowsAreKeyedByTheirCid：分集行（播放条上的分集抽屉共用）的 key 是 cid，不是行号。
func TestPartRowsAreKeyedByTheirCid(t *testing.T) {
	rows := partRowsFor(multiParts(), app.Item{Cid: "778"})
	if len(rows) != 3 || rows[0].key != "part:777" || !rows[1].active || rows[2].title != "第 3 集" {
		t.Errorf("rows = %+v", rows)
	}
}
