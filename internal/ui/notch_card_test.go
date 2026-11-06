package ui

// 刘海展开播放卡（参考悬浮播放器原型 `prototype/pudmusic-update-notch.html`
// 的 `.playing-body`）的用户可观察行为。
//
// 钉的不是像素，是**用户看见 / 点得动**的那一份：
//
//   - 展开时窗的形状吃到完整播放卡那么高，不再是最初那张 178 的
//     「三列小卡」；
//   - 卡上有：歌名 / 歌手、上一首 / 播放 / 下一首、随机与循环两枚模式键、
//     一条「播放进度」滑块、已播与剩余两个读数；
//   - 剩余时间写成 `-m:ss`（原型 `progress-row` 的右端是「-1:13」）；
//   - 暂停时频谱柱不画、播放键说「播放」；播放中换「暂停」；
//   - 点模式键真的把 PlayMode 拨到那一档（随机 / 循环各是各的键，
//     不是一枚键挨档转）；
//   - 没歌时仍是 idle 那张卡，不是播放器布局。

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// newOpenNotch 建一个「有歌、展开」的刘海外壳和画它的 Tester。
func newOpenNotch(t *testing.T) (*Shell, *mygoui.Tester) {
	t.Helper()
	n, _ := newNotchShell(t)
	if err := enqueueTwo(n.app, t); err != nil {
		t.Fatal(err)
	}
	n.notchSt.open = true
	tt := mygoui.NewTester(n.View, notchWinW, notchWinH)
	n.repaint = tt.Frame
	return n, tt
}

// TestNotchExpandedTargetIsThePlayingCard 钉展开时形状的高度：原型的
// `.notch.expanded` 是刘海高 + 210。
func TestNotchExpandedTargetIsThePlayingCard(t *testing.T) {
	n, _ := newOpenNotch(t)
	w, h := n.notchTarget()
	if w != notchOpenW {
		t.Fatalf("展开的宽是 %v，want %v", w, notchOpenW)
	}
	want := n.notchSt.h0 + notchOpenH
	if h != want {
		t.Fatalf("展开的高是 %v，want %v（刘海 %v + 卡深 %v）", h, want, n.notchSt.h0, notchOpenH)
	}
	// 卡的深要装得下「主行 + 进度行 + 传输行」那三行（原型 playing-body 210），
	// 178 那一档装不下——钉住它就是钉「不再是那张小卡」。
	if notchOpenH < 200 {
		t.Fatalf("展开卡深是 %v，want ≥200（参考卡是 210）", notchOpenH)
	}
}

// TestNotchCardShowsTheTransport 钉展开卡上那五枚传输键都在、读数与滑块都在。
func TestNotchCardShowsTheTransport(t *testing.T) {
	_, tt := newOpenNotch(t)
	tt.Frame()
	for _, label := range []string{"随机播放", "上一首", "下一首", "循环播放", "播放进度"} {
		if _, ok := tt.Find(label); !ok {
			t.Fatalf("展开卡上找不到 %q，文字有 %q", label, tt.Texts())
		}
	}
	// 队列里两首、第一首开始放（enqueueTwo 会直接开播）：播放键此时说「暂停」。
	if _, ok := tt.Find("暂停"); !ok {
		t.Fatalf("正在放的时候播放键该是「暂停」，文字有 %q", tt.Texts())
	}
}

// TestNotchCardModeButtonsSetTheMode 钉两端模式键**各管各的档**：
// ⤨ 那枚按下去进随机档，⤻ 那枚按下去进循环档——不是一枚键挨个转过去
// （原型 `transport` 左右两枚 secondary 就是 shuffle / repeat 两枚独立的键）。
func TestNotchCardModeButtonsSetTheMode(t *testing.T) {
	n, tt := newOpenNotch(t)
	tt.Frame()

	if err := tt.Click("随机播放"); err != nil {
		t.Fatalf("点「随机播放」：%v（文字 %q）", err, tt.Texts())
	}
	tt.Frame()
	if got := n.app.Snapshot().PlayMode; got != app.PlayModeRandom {
		t.Fatalf("点了随机键之后 PlayMode 是 %v，want random", got)
	}

	if err := tt.Click("循环播放"); err != nil {
		t.Fatalf("点「循环播放」：%v（文字 %q）", err, tt.Texts())
	}
	tt.Frame()
	if got := n.app.Snapshot().PlayMode; got != app.PlayModeLoop {
		t.Fatalf("点了循环键之后 PlayMode 是 %v，want loop", got)
	}
}

// TestNotchCardRemainingTime 钉进度行右端写的是**剩余**（`-m:ss`），
// 不是总长。断的是「有一个以减号开头的读数」这个事实，不是具体秒数
// （解码出来的位置有误差）。
func TestNotchCardRemainingTime(t *testing.T) {
	_, tt := newOpenNotch(t)
	tt.Frame()
	found := false
	for _, s := range tt.Texts() {
		if len(s) > 1 && s[0] == '-' && s[1] >= '0' && s[1] <= '9' {
			found = true
		}
	}
	if !found {
		t.Fatalf("展开卡上没有剩余时间读数（-m:ss），文字有 %q", tt.Texts())
	}
}

// TestNotchCardSpectrumOnlyWhilePlaying 钉频谱柱**播放中才画**（原型
// `.playing-eq` 挂在播放态里；暂停时那一格和两翼 eq 一样收起来）。
func TestNotchCardSpectrumOnlyWhilePlaying(t *testing.T) {
	n, tt := newOpenNotch(t)
	tt.Frame()
	if !tt.HasText("频谱") {
		t.Fatalf("播放中展开卡上没有频谱，文字有 %q", tt.Texts())
	}
	n.app.Toggle() // 暂停
	tt.Frame()
	if tt.HasText("频谱") {
		t.Fatalf("暂停之后频谱还在，文字有 %q", tt.Texts())
	}
}

// TestNotchIdleStillTheSmallCard 钉空闲态没变：没歌时展开仍是「未在播放 +
// 打开」那张小卡，不画传输键。
func TestNotchIdleStillTheSmallCard(t *testing.T) {
	n, _ := newNotchShell(t)
	n.notchSt.open = true
	tt := mygoui.NewTester(n.View, notchWinW, notchWinH)
	n.repaint = tt.Frame
	tt.Frame()
	if !tt.HasText("未在播放") {
		t.Fatalf("没歌展开时没有「未在播放」，文字有 %q", tt.Texts())
	}
	if tt.HasText("上一首") || tt.HasText("播放进度") {
		t.Fatalf("空闲卡里不该有播放控件，文字有 %q", tt.Texts())
	}
}

// TestNotchWingsHiddenWhenExpanded 钉展开时顶上那一行（两翼的小封面 + eq）
// 不再画——参考卡里封面走 56 那张大图，顶上再挂一张 20 的就是两张封面。
func TestNotchWingsHiddenWhenExpanded(t *testing.T) {
	n, tt := newOpenNotch(t)
	n.app.Toggle() // 暂停：频谱不收着会一直要帧，动画停不稳
	settleNotch(t, tt)
	// 封面图区域只该出现在 56 的大图那一块：收起时小封面贴着屏顶，
	// 展开时大图在主行（顶边明显更深）。
	box := miniCoverBox(tt)
	if box.Empty() {
		t.Skip("封面还没画上（素材取回是异步的），这一档量不了翼的行")
	}
	if box.Min.Y < int(n.notchSt.h0) {
		t.Fatalf("展开后小封面还贴着屏顶（box=%v）：两翼那行没有收起来", box)
	}
}
