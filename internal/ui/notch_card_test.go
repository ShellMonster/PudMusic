package ui

// 刘海展开播放卡（规格见 DESIGN.md「刘海播放器」一节）的用户可观察行为。
//
// 钉的不是像素，是**用户看见 / 点得动**的那一份：
//
//   - 展开时窗的形状吃到完整播放卡那么高（刘海高 + 210），不再是
//     收起那张小卡；
//   - 卡上有：歌名 / 歌手、上一首 / 播放 / 下一首、随机与循环两枚模式键、
//     一条「播放进度」滑块、已播与剩余两个读数；
//   - 剩余时间写成 `-m:ss`（进度行右端是「还剩多久」，不是总长）；
//   - 暂停时频谱柱不画、播放键说「播放」；播放中换「暂停」；
//   - 点模式键真的把 PlayMode 拨到那一档（随机 / 循环各是各的键，
//     不是一枚键挨档转）；
//   - 没歌时仍是 idle 那张卡，不是播放器布局。

import (
	"strings"
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

// TestNotchExpandedTargetIsThePlayingCard 钉展开时形状的高度：
// 展开卡是刘海高 + 210（DESIGN.md「刘海播放器」）。
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
	// 卡的深要装得下「主行 + 进度行 + 传输行」那三行（参考卡深 210），
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
// （传输行左右两端是 shuffle / repeat 两枚独立的键，不是一枚键挨个转）。
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

// TestNotchCardModeButtonsToggleBackToSequence 钉两端模式键**再点一下回顺序档**：
// 「随机」和「循环」是开关不是单向门——亮着的那枚再点一下要熄掉，回到顺序。
// 单向门的话用户点了随机就永远回不去顺序（循环键只会把它拨到循环档）。
func TestNotchCardModeButtonsToggleBackToSequence(t *testing.T) {
	n, tt := newOpenNotch(t)
	tt.Frame()

	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatalf("点 %q：%v（文字 %q）", label, err, tt.Texts())
		}
		tt.Frame()
	}

	click("随机播放")
	if got := n.app.Snapshot().PlayMode; got != app.PlayModeRandom {
		t.Fatalf("点了随机键之后 PlayMode 是 %v，want random", got)
	}
	click("随机播放")
	if got := n.app.Snapshot().PlayMode; got != app.PlayModeSequence {
		t.Fatalf("再点一次随机键之后 PlayMode 是 %v，want 回顺序档", got)
	}

	click("循环播放")
	if got := n.app.Snapshot().PlayMode; got != app.PlayModeLoop {
		t.Fatalf("点了循环键之后 PlayMode 是 %v，want loop", got)
	}
	click("循环播放")
	if got := n.app.Snapshot().PlayMode; got != app.PlayModeSequence {
		t.Fatalf("再点一次循环键之后 PlayMode 是 %v，want 回顺序档", got)
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

// TestNotchCardSpectrumOnlyWhilePlaying 钉频谱柱**播放中才画**
// （暂停时那一格和两翼 eq 一样收起来）。
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

// notchFFT 拉一个钉 `SpectrumNotch` 位的小工具：wanted 位是应用层那本账，
// 「FFT 跑不跑」由它推出去（`SetSpectrumWantedFor`）。钉的是位本身——
// 「那一帧还走没走到清它的那一句」正是要抓的泄漏。
func notchFFT(t *testing.T, n *Shell) func() bool {
	t.Helper()
	return func() bool {
		return n.app.SpectrumWantedFor(app.SpectrumNotch)
	}
}

// TestNotchSpectrumWantedReleasedWhenCardGone 钉 `SpectrumNotch` 位**每帧都
// 按「这一帧到底画没画那张卡」记账**——不只靠「正在放」那个开关。
//
// 记账点原来在 `viewNotchSpectrum` 里：`open` 关掉、歌切没了、窗口失焦这几
// 档全都**不再走到它**，于是那一位置了 1 之后没人清——FFT 就一直跑（
// 对抗评审的 P0：wanted 位泄漏）。现在把「要画频谱」挪到 `viewNotch` 顶层
// 汇总（open && HasTrack && Playing && seen），「不画」那几种归宿都由
// 同一句清掉。
func TestNotchSpectrumWantedReleasedWhenCardGone(t *testing.T) {
	n, tt := newOpenNotch(t)
	fft := notchFFT(t, n)

	tt.Frame()
	if !fft() {
		t.Fatal("展开 + 正在放，FFT 却停着（wanted 没报上去）")
	}

	// 指针走开：收起的那一档展开卡根本不建，`SpectrumNotch` 位必须当场清掉。
	n.notchSt.open = false
	tt.Frame()
	if fft() {
		t.Fatal("刘海收起之后 FFT 还在跑：`SpectrumNotch` 位没人清")
	}

	// 歌切没了（队列清空）：HasTrack 掉了，同样要清位。
	n.notchSt.open = true
	tt.Frame()
	if !fft() {
		t.Fatal("重新展开 + 正在放，FFT 没跟着回来")
	}
	n.app.ClearQueue()
	tt.Frame()
	if fft() {
		t.Fatal("没歌之后 FFT 还在跑：`SpectrumNotch` 位没人清")
	}
}

// TestNotchSpectrumWantedReleasedWhenHidden 钉**窗口看不见**那一档：
// `seen()` 变假之后，频谱那一格照样要把自己那一票收回来。
// 漏了它的话窗被藏起/失焦之后 FFT 还烧着（这正是「看不见的窗还在要帧」
// 那一类问题的频谱版）。
func TestNotchSpectrumWantedReleasedWhenHidden(t *testing.T) {
	n, tt := newOpenNotch(t)
	fft := notchFFT(t, n)

	tt.Frame()
	if !fft() {
		t.Fatal("展开 + 正在放，FFT 却停着")
	}

	// 窗口藏了（设置关掉刘海）：`noteWindow` 把 seen 拨成假，那一帧
	// 之后 `SpectrumNotch` 位必须是 0。
	n.noteWindow(func(p *windowPresence) { p.shown = false })
	tt.Frame()
	if fft() {
		t.Fatal("窗口藏了之后 FFT 还在跑：`SpectrumNotch` 位没人清")
	}
}

// TestNotchSpectrumWantedReleasedOnClose 钉**收尾那一路**：`releaseNotch`
// （窗口关了走的那条）和 `notchSetEnabled(false)`（设置关掉）都得把
// `SpectrumNotch` 位清掉——不然那一路收走之后 FFT 永远停在开。
func TestNotchSpectrumWantedReleasedOnClose(t *testing.T) {
	n, tt := newOpenNotch(t)
	fft := notchFFT(t, n)

	tt.Frame()
	if !fft() {
		t.Fatal("展开 + 正在放，FFT 却停着")
	}
	n.releaseNotch()
	if fft() {
		t.Fatal("releaseNotch 之后 FFT 还在跑：`SpectrumNotch` 位没清")
	}

	// 再把 wanted 推上去一次，走 `notchSetEnabled(false)` 那一路。
	n2, tt2 := newOpenNotch(t)
	fft2 := notchFFT(t, n2)
	tt2.Frame()
	if !fft2() {
		t.Fatal("展开 + 正在放，FFT 却停着")
	}
	// 设成「开着」才能让 SetEnabled(false) 走「藏窗」那一支；无头里没有
	// 真窗，win 是 nil——置位的清账不依赖窗口在不在。
	n2.app.UpdateSettings(func(st *app.Settings) { st.NotchPlayer = boolPtr(true) })
	n2.notchSetEnabled(false)
	if fft2() {
		t.Fatal("notchSetEnabled(false) 之后 FFT 还在跑：`SpectrumNotch` 位没清")
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

// notchToastTexts 把这一个外壳此刻挂着的 toast 文案抄出来（量「那句提示
// 落在哪一扇窗上」用）。
func notchToastTexts(s *Shell) []string {
	s.toastMu.Lock()
	defer s.toastMu.Unlock()
	out := make([]string, 0, len(s.toasts))
	for _, t := range s.toasts {
		out = append(out, t.text)
	}
	return out
}

// TestNotchLikeFailureToastsOnTheMainWindow 钉「刘海里赞失败」那句提示
// **落在主窗口**（画 toast 的那一扇），不是落在刘海那个外壳上——刘海窗
// 压根不画 toast（`viewToasts` 只在主窗那一支调），写进它自己的 `toasts`
// 等于那句「赞不了」永远没有画面（对抗评审：静默失败）。
//
// 路由规则和迷你窗同一条（`filenotice.go` 顶上那一节）：`addToast` 在
// 刘海外壳上被调到时，要转去 `s.main` 那一份。
func TestNotchLikeFailureToastsOnTheMainWindow(t *testing.T) {
	f := newBiliFixture(t, 1)
	f.onLike(false, false, false)

	n, main := newNotchShell(t)
	// 在线那首放进共享 App（刘海外壳和主窗共用同一个 App）。
	n.app.MountStreaming(app.Streaming{
		Client: f.client(), Media: f.Client(), TempDir: t.TempDir(),
	})
	if err := n.app.Enqueue(likeItem()); err != nil {
		t.Fatalf("把在线的那首放进队列：%v", err)
	}
	waitFor(t, "在线那首开始播", func() bool { return n.app.Snapshot().HasTrack })
	// 「赞没赞」那一问是界面每帧调 `LikeState` 触发的——这里没有帧，手动问
	// 一次再等到回来（`LikeAsked` 才是真回来的那个标记）。
	n.app.LikeState()
	waitFor(t, "赞没赞问回来了", func() bool { return n.app.LikeAsked() })

	// 赞那一趟换成失败：B 站回「没登录」，`SetLike` 的 done 拿到 err。
	f.failLike()

	n.likeCurrent()
	waitFor(t, "赞失败那一趟跑完了", func() bool {
		return len(notchToastTexts(main)) > 0 || len(notchToastTexts(n)) > 0
	})

	if got := notchToastTexts(n); len(got) != 0 {
		t.Fatalf("「赞不了」写进了刘海外壳自己的 toasts %q：它那一扇不画 toast，这句话永远没有画面", got)
	}
	got := notchToastTexts(main)
	if len(got) == 0 {
		t.Fatal("「赞不了」没有落到主窗口的 toasts 上：用户按了赞、什么提示都没看见")
	}
	found := false
	for _, text := range got {
		if strings.HasPrefix(text, "赞不了") {
			found = true
		}
	}
	if !found {
		t.Fatalf("主窗口的 toasts 里没有「赞不了」那句：%q", got)
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
