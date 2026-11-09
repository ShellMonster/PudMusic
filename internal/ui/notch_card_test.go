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
	"context"
	"net/http"
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

// newOpenNotchWithCover 是 `newOpenNotch` 加上一张封面：
// 「两翼收没收」那一档要靠封面在画面上的位置量——没封面那一档等于
// 「尺子根本没到」。走 `enqueueTwoWithCover` 的 gated 服务：封面在后台
// 取的那一趟是**真的异步**，用它等「画上来了」而不是 `t.Skip`。
func newOpenNotchWithCover(t *testing.T) (*Shell, *mygoui.Tester) {
	t.Helper()
	n, _ := newNotchShell(t)
	if err := enqueueTwoWithCover(n.app, t); err != nil {
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

// TestNotchHeartReportsOnTheMainShell 钉「刘海里那枚心形按下去说的是什么」。
//
// 心形是**喜欢歌单绑定入口**（`likeCurrent` → 绑定流程），不是 B 站
// 点赞——喜欢歌单原型把「赞」从播放器上去掉了。
//
// 这条顺带钉「提示落在画 toast 的那一扇」：没放歌时心形按下去说的是
// 「这一首还没有取回来」，那句要走 `s.main` 那份 toasts——刘海窗自己压根
// 不画 toast（`viewToasts` 只在主窗那一支调）。
func TestNotchHeartReportsOnTheMainShell(t *testing.T) {
	n, main := newNotchShell(t)

	// ⚠️ **没放歌**：心形按下去说的是「这一首还没有取回来」，而那句话必须
	// 落在主窗口的 toasts 上，不是刘海外壳自己的（它那一扇不画 toast）。
	n.likeCurrent()

	if got := notchToastTexts(n); len(got) != 0 {
		t.Fatalf("提示写进了刘海外壳自己的 toasts %q：它那一扇不画 toast，这句话永远没有画面", got)
	}
	got := notchToastTexts(main)
	if len(got) == 0 {
		t.Fatal("提示没有落到主窗口的 toasts 上：用户按了心形、什么提示都没看见")
	}
}

// TestNotchHeartRunsTheLikeBindingFlowOnTheMainShell 钉「刘海点心形走的是
// **喜欢歌单绑定流程**，且弹窗开在**主壳**上」。
//
// 🔴 **`viewFolderDialogs` 只在主窗口那一支调**：刘海外壳自己那份
// `likeBind` / `folderSelect` 永远没人画——弹窗写到副壳上等于开了一个
// 看不见的窗（和 toast 写进副壳同一种静默失败）。这条盯的是
// `main.likeBind` 真的摆出来了（没绑定 → 三选一），而不是绕回旧多选框
// （`folderSelect`）——那一条正是本票拦的「播放器心形绕过绑定流程」。
func TestNotchHeartRunsTheLikeBindingFlowOnTheMainShell(t *testing.T) {
	// ⚠️ **fixture 先于 `newNotchShell` 建**：`t.Cleanup` 是 LIFO——fixture
	// 晚于主壳建的话，测试收尾时服务器先关、Shell 后关，挂在它上面的
	// 后台往返全被「connection refused」。
	f := newBiliFixture(t, 1)
	fb := newFolderBili(t, nil)
	n, main := newNotchShell(t)

	// 挂歌单 + 放一首在线的歌（心形要 `PlayingItem` + `CollectionsMounted`；
	// 在线播要 `MountStreaming`——不然 `Enqueue` 回「在线播放暂时用不了」）。
	main.app.MountStreaming(app.Streaming{
		Client: f.client(), Media: f.Client(), TempDir: t.TempDir(),
	})
	// ⚠️ **绑定流程要登录态**：`LoadCreatedFolders` 要 `SelfMid`（问「我创建
	// 了哪些收藏夹」），没登录它直接回「还没登录」——三选一就开不出来。
	cli := fixtureClient(t, fb)
	store := app.NewMemoryStore("test")
	if err := store.Save(context.Background(), app.Session{SESSDATA: "sess", DedeUserID: "7"}); err != nil {
		t.Fatal(err)
	}
	main.app.MountLogin(store, cli)
	if err := cli.SeedCookies([]*http.Cookie{{Name: "DedeUserID", Value: "7", Path: "/"}}); err != nil {
		t.Fatal(err)
	}
	main.app.MountCollections(app.CollectionRefs{Client: cli, Settle: 0})
	// ⚠️ **`Sid` 必须带上**：`favTarget` 认的是它（avid）——不带的话
	// `likeItem` 先去后台问 bvid → avid，那一趟还要等，而这条盯的
	// 是「弹窗开在哪个壳上」，不是解析那一路。
	if err := main.app.Enqueue(app.Item{
		Source: "online", Type: "mv", Bvid: "BV1probe", Cid: "1", Sid: "999",
		Title: "刘海里要喜欢的那一首", Artist: "UP", ArtistMid: 12345,
	}); err != nil {
		t.Fatalf("把在线的那首放进队列：%v", err)
	}
	waitFor(t, "在线那首开始播", func() bool { return main.app.Snapshot().HasTrack })

	n.likeCurrent()

	if main.likeBind == nil || !main.likeBind.open {
		t.Fatalf("刘海点心形之后主壳的绑定三选一没开（likeBind = %+v）", main.likeBind)
	}
	if main.folderSelect != nil {
		t.Error("刘海点心形直接弹了收藏夹多选框：它绕过了绑定流程")
	}
	if n.likeBind != nil || n.folderSelect != nil {
		t.Error("弹窗写进了刘海外壳自己那一份：那一扇不画它，用户永远看不见")
	}
}

// TestNotchWingsHiddenWhenExpanded 钉展开时顶上那一行（两翼的小封面 + eq）
// 不再画——参考卡里封面走 56 那张大图，顶上再挂一张 20 的就是两张封面。
func TestNotchWingsHiddenWhenExpanded(t *testing.T) {
	n, tt := newOpenNotchWithCover(t)
	n.app.Toggle() // 暂停：频谱不收着会一直要帧，动画停不稳
	// ⚠️ **展开这张卡上，封面那一格必须真的画出来**——它是「两翼收没收」
	// 的尺子：封面只出现在两翼那一行和主行大图两处，扫不到它说明这一档
	// 根本没到「有封面」的状态，后面的几何断言等于白量。
	waitFrames(t, tt, "封面画上来了", func() bool {
		return !miniCoverBox(tt).Empty()
	})
	settleNotch(t, tt)
	box := miniCoverBox(tt)
	if box.Empty() {
		t.Fatal("settle 之后封面不见了：这一档量不了翼的行")
	}
	if box.Min.Y < int(n.notchSt.h0) {
		t.Fatalf("展开后小封面还贴着屏顶（box=%v）：两翼那行没有收起来", box)
	}
}

// TestNotchSetModeRefreshesTheMenuCheck 钉刘海的模式键切档之后，**应用菜单上
// 那组单选的勾选跟着动**——菜单是系统画的、不会自己变（`refreshMenuChecked`），
// 而刘海外壳自己没有 `s.menu`（菜单只有主窗那一份，`adoptShared` 不接它）：
// 在刘海壳上直接刷会把勾选写到一份没挂出去的菜单上，用户看到的那份永远不更新。
//
// ⚠️ **所以要往 `s.main` 那一扇转**——和 `toggleNotchPlayer`（notch.go）同一条
// 路由：凡是「改整进程一份的东西」，都从刘海外壳上转去主窗外壳做。
func TestNotchSetModeRefreshesTheMenuCheck(t *testing.T) {
	n, main := newNotchShell(t)
	main.menu = appMenu(main)
	main.refreshMenuChecked(main.app.Snapshot())

	n.notchSetMode(app.PlayModeRandom)

	if !menuItemChecked(main.menu, menuPlayModeIDPrefix+string(app.PlayModeRandom)) {
		t.Error("刘海里切到随机之后，应用菜单上「随机」那一项没打钩")
	}
	if menuItemChecked(main.menu, menuPlayModeIDPrefix+string(app.PlayModeLoop)) {
		t.Error("刘海里切到随机之后，应用菜单上「循环」那一项还打着钩")
	}
}

// TestNotchSetModeWithoutMainDoesNotPanic 钉刘海外壳没有主窗那一半（`s.main`
// 是 nil，比如测试里手搓出来的零值外壳）时切档不炸。
func TestNotchSetModeWithoutMainDoesNotPanic(t *testing.T) {
	n, _ := newNotchShell(t)
	n.main = nil
	n.notchSetMode(app.PlayModeRandom)
	if got := n.app.Snapshot().PlayMode; got != app.PlayModeRandom {
		t.Fatalf("PlayMode 是 %v，want random", got)
	}
}

// TestNotchTitleUsesTheCleanedName 断刘海那一行的歌名是**清洗过的**
// （`tclean.Clean`），和播放条同一份口径——之前它直接画 `st.Title`，
// B 站的「【4K修复】…(官方MV)」一整串原样挂在刘海上。
func TestNotchTitleUsesTheCleanedName(t *testing.T) {
	b := newTestShell(t, withView(func(s *Shell) func(*mygoui.Context) {
		return func(c *mygoui.Context) {
			s.applyFrameTheme(c)
			s.viewNotchCardMain(c, app.State{HasTrack: true, Title: "【4K修复】晴天 (官方MV)", Artist: "周杰伦"})
		}
	}))
	b.tt.Frame()

	if !hasExactText(b.tt, "晴天") {
		t.Fatalf("刘海上没有清洗后的歌名，文字有 %q", b.tt.Texts())
	}
	if countText(b.tt, "【4K修复】晴天 (官方MV)") != 0 {
		t.Fatalf("刘海上还挂着原始标题，文字有 %q", b.tt.Texts())
	}
}
