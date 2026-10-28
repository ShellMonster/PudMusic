package ui

// 迷你播放器那个「歌词卷帘」这一组（票 #536）。
//
// # 这一组量的是什么
//
// 卷帘是**同一个窗变高**（BMG-280 的路径 A），所以这一组要断的是四件事：
//
//  1. **点封面能展开 / 收起**（不依赖悬停——迷你窗是 Panel，失焦时收不到
//     鼠标移动，见 `minicurtain.go` 的文件头）；
//  2. **设置开关**：默认开着；关着时点封面**什么也不发生**；展开着的时候
//     把开关关掉 ⇒ **立刻收起**；
//  3. **窗口高度 = 84 + 卷帘高**（这一扇窗是透明的，多出来的那一块照样吃
//     点击）；
//  4. **收起之后状态里记的还是 320×84**（`StateKey` 记宽高）。
//
// # 无头 Tester 量不到的那几样
//
// **真窗口才量得到**的那些（窗口真的变高了、`window-state.json` 里真的写着
// 84、卷帘真的被工作区夹住了）列在 `miniplayer_test.go` 文件末尾那一节，
// 照它那样列，不在这里假装验过。

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// miniCurtainShell 建一扇**能展开卷帘**的迷你播放器外壳。
//
// ⚠️ **工作区是注入的**：卷帘的高度按**那扇窗所在显示器的工作区**夹住，而
// `mygo.Screen` 头上有 `needsApp`（`mygo/app.go:128`）——应用没 ready 的时候
// 调它**直接 panic**，而无头 Tester 里压根没有应用。所以这一档把工作区直接
// 摆进来（`s.miniCurtainWork`），生产那条路走的是真工作区。
func miniCurtainShell(t *testing.T) (*Shell, *app.App) {
	t.Helper()
	// ⚠️ **要挂设置**：那一格开关住在设置里，而 `UpdateSettings` 在没挂盘子的
	// App 上直接报错（「app: 还没有挂上设置，先调 MountSettings」）。
	//
	// ⚠️ **挂在 `newMiniShell` 之前**：那个函数建的是**主窗口**的外壳，而
	// 设置是挂在 **App** 上的——`MountSettings` 必须在任何外壳建好之前调
	// （`shelltest_test.go` 的 `newTestShell` 就是这个次序）。
	a := app.New()
	t.Cleanup(a.Close)
	if err := a.MountSettings(filepath.Join(writableTempDir(t, "settings"), app.SettingsFileName)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.CloseSettings() })
	main := newTestShell(t, onApp(a)).s

	s := NewMini(a, main, nil)
	t.Cleanup(s.closePerWindow)
	s.router.Transition = mygoui.TransitionNone
	// 一块 1920×1080 的工作区，窗在它右上角（离顶 12）——往下能长 1080−12−84
	// ＝ 984，**放得下**那个 236。
	s.miniCurtainWork = miniCurtainRect{X: 0, Y: 0, Width: 1920, Height: 1080}
	return s, a
}

// miniCurtainTester 开一扇**320×(84+卷帘高)**的 Tester。
//
// ⚠️ **为什么不是 320×84**：展开之后窗真的会变高（84 + 236 = 320），而无头
// Tester 的窗口尺寸是**自己那个**——它不跟真窗口走（`mygo/ui/headless.go` 的
// Tester 有自己的 `w` / `h`）。Tester 还是 84 高的话，卷帘那一段被排在**窗口
// 外面**：绝对定位的那一枚「收起」键量出来是 0×0，而歌词那一段在画面上根本
// 画不出来（被裁掉）。
//
// ⚠️ **320 是 84 + 236**：而这正是**真窗口展开后的高度**——所以这一档量到
// 的版面就是用户看到的那一版。
func miniCurtainTester(s *Shell) *mygoui.Tester {
	return mygoui.NewTester(s.View, miniWidth, miniHeight+miniCurtainH)
}

// miniCurtainHasLyrics 断「画面上有没有歌词」。
//
// ⚠️ **判据是歌词文本**：无头 Tester 的 `Find` 找的是「有文字**或有可及
// 名字**」的元素，而歌词那一行是 `ui.Text`——所以它在 `Texts()` 里。
func miniCurtainHasLyrics(tt *mygoui.Tester) bool {
	return tt.HasText("第一句")
}

// ─────────────────────────────────────────────────────────────────────────────
// 展开 / 收起
// ─────────────────────────────────────────────────────────────────────────────

// TestTheMiniCurtainOpensWhenTheCoverIsClicked 盯「点封面展开」。
//
// ⚠️ **不依赖悬停**：迷你窗是 `Panel`，点它不激活应用，于是它几乎永远不是
// key 窗口，而 MyGo 的悬停跟踪区带 `NSTrackingActiveInKeyWindow`
// （`mygo/internal/darwin/surface.go:162`）——失焦时收不到鼠标移动
// （BMG-280 §2.4）。所以这一条**只按，不移**。
//
// ⚠️ **判据是「歌词区在不在」**：卷帘展开之后画面上才有歌词。而
// `Tester.Click` 是**按下去 + 松开**，而框架的点击要**两遍 pass** 才算
// （`mygo/ui/runtime.go:529` 那个 `for pass := 0; pass < 3`）——所以点完要
// 再画一帧。
func TestTheMiniCurtainOpensWhenTheCoverIsClicked(t *testing.T) {
	s, a := miniCurtainShell(t)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle() // 暂停
	a.PutLyricsFrom("", "[00:00.00]第一句\n[00:05.00]第二句\n", "")
	tt := miniCurtainTester(s)

	// 起点：卷帘是收着的。
	tt.Frame()
	if miniCurtainHasLyrics(tt) {
		t.Fatalf("起点就不对：卷帘还没点就已经有歌词了，文字有 %q", tt.Texts())
	}

	// 点封面。⚠️ **点那一列的正中间**——封面在的时候是 (6,6)..(78,78)，
	// 那一列是 84 宽、84 高（`miniCoverCol` × `miniHeight`）。
	cover, ok := tt.Find("展开歌词")
	if !ok {
		t.Fatalf("画面上找不到封面那一列（它的可及名字是「展开歌词」），文字有 %q", tt.Texts())
	}
	tt.ClickAt(cover.X+cover.W/2, cover.Y+cover.H/2)
	tt.Frame()

	// 推钟让展开那一段动画走完（Tester 的钟站着不动）。
	settleCurtain(t, tt, s, true)

	if !miniCurtainHasLyrics(tt) {
		t.Errorf("点了封面之后卷帘没有展开：画面上没有歌词，文字有 %q", tt.Texts())
	}
	if !s.miniCurtain.open {
		t.Error("卷帘没有处于展开状态（`open` 还是 false）")
	}
}

// TestTheMiniCurtainClosesWhenTheCoverIsClickedAgain 盯「再点封面收起」。
func TestTheMiniCurtainClosesWhenTheCoverIsClickedAgain(t *testing.T) {
	s, a := miniCurtainShell(t)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle()
	a.PutLyricsFrom("", "[00:00.00]第一句\n[00:05.00]第二句\n", "")
	tt := miniCurtainTester(s)

	clickCurtainCover(t, tt)
	settleCurtain(t, tt, s, true)
	if !miniCurtainHasLyrics(tt) {
		t.Fatalf("卷帘没有展开，文字有 %q", tt.Texts())
	}

	// 再点一次。
	cover, ok := tt.Find("展开歌词")
	if !ok {
		t.Fatalf("画面上找不到封面那一列，文字有 %q", tt.Texts())
	}
	tt.ClickAt(cover.X+cover.W/2, cover.Y+cover.H/2)
	tt.Frame()
	settleCurtain(t, tt, s, false)

	if miniCurtainHasLyrics(tt) {
		t.Errorf("再点封面之后卷帘还开着：画面上还有歌词，文字有 %q", tt.Texts())
	}
	if s.miniCurtain.open {
		t.Error("卷帘还处于展开状态（`open` 还是 true）")
	}
}

// TestTheMiniCurtainClosesWithTheCollapseButton 盯「点收起键收起」。
//
// 收起键是**卷帘里**那一枚（`iconChevronUp`），而它是**第三件**收起的东西
// （另两件是「再点封面」和 Esc）。
func TestTheMiniCurtainClosesWithTheCollapseButton(t *testing.T) {
	s, a := miniCurtainShell(t)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle()
	a.PutLyricsFrom("", "[00:00.00]第一句\n[00:05.00]第二句\n", "")
	tt := miniCurtainTester(s)

	clickCurtainCover(t, tt)
	settleCurtain(t, tt, s, true)

	btn, ok := tt.Find("收起歌词")
	if !ok {
		t.Fatalf("画面上找不到收起那一枚，文字有 %q", tt.Texts())
	}
	tt.ClickAt(btn.X+btn.W/2, btn.Y+btn.H/2)
	tt.Frame()
	settleCurtain(t, tt, s, false)

	if miniCurtainHasLyrics(tt) {
		t.Errorf("点了收起之后卷帘还开着：画面上还有歌词，文字有 %q", tt.Texts())
	}
	if s.miniCurtain.open {
		t.Error("卷帘还处于展开状态（`open` 还是 true）")
	}
}

// TestTheMiniCurtainHeightFollowsTheAnimation 盯「窗口高度跟着动画走」。
//
// ⚠️ **这一条量的是「内容高度」不是「窗口高度」**：无头 Tester 里量不到真窗
// 口的尺寸（Tester 有自己那个 320×84）。而**窗高必须等于内容高**这件事，
// 在无头这一档能验的是另一半——**内容的高度就是 `84 + 卷帘高`**。
//
// ⚠️ **判据是「卷帘那一块画到了哪儿」**：`Tester.Find` 报的是画出来的那一块
// 的框，而卷帘是**绝对定位之外**的一段（它是根 Column 的第二个孩子），所以
// 它的框就是它画出来的那一块。
func TestTheMiniCurtainHeightFollowsTheAnimation(t *testing.T) {
	s, a := miniCurtainShell(t)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle()
	a.PutLyricsFrom("", "[00:00.00]第一句\n[00:05.00]第二句\n", "")
	tt := miniCurtainTester(s)

	clickCurtainCover(t, tt)
	settleCurtain(t, tt, s, true)

	// 收着的时候，迷你卡片那一块是 84 高。
	card, ok := tt.Find("展开歌词")
	if !ok {
		t.Fatalf("画面上找不到封面那一列，文字有 %q", tt.Texts())
	}
	if card.H < miniHeight-0.5 || card.H > miniHeight+0.5 {
		t.Errorf("封面那一列是 %.1f 高，want %d：迷你卡片那一段必须钉死 84"+
			"（它不能跟着卷帘一起长高）", card.H, miniHeight)
	}

	// 展开之后：卷帘那一块是 236 高（工作区放得下，所以是满的那一档）。
	//
	// ⚠️ **判据是「画出来的那一块」**：无头 Tester 量不到真窗口的尺寸，而
	// **内容画到了哪儿**是量得到的——而「窗高 = 内容高」这件事正是要断的那
	// 一条（多出来的那一块透明区在吃点击）。
	curtain, ok := tt.Find("迷你播放器歌词")
	if !ok {
		t.Fatalf("画面上找不到卷帘那一块（它的名字是「迷你播放器歌词」），文字有 %q", tt.Texts())
	}
	if curtain.H < miniCurtainH-1 || curtain.H > miniCurtainH+1 {
		t.Errorf("卷帘画出来是 %.1f 高，want %d", curtain.H, miniCurtainH)
	}
	// ⚠️ **卷帘紧贴在迷你卡片下面**（不重叠、不留缝）：卷帘的上沿就是
	// 迷你行（84 高）的下沿——而这一条是「窗高 = 内容高」的一半。
	if got, want := curtain.Y, float32(miniHeight); got < want-1 || got > want+1 {
		t.Errorf("卷帘的上沿在 y=%.1f，want %.0f：它必须紧贴在迷你卡片下面"+
			"（中间留一道缝的话，窗就比内容高，而多出来的那一块透明区在吃点击）",
			got, want)
	}
	// 宽度：卷帘铺满整窗（320），不窄一截。
	if got, want := curtain.W, float32(miniWidth); got < want-1 || got > want+1 {
		t.Errorf("卷帘是 %.1f 宽，want %.0f：它要铺满整窗", got, want)
	}

	animH := s.miniCurtain.height
	if animH < miniCurtainH-1 || animH > miniCurtainH+1 {
		t.Errorf("卷帘展开之后是 %.1f 高，want %d", animH, miniCurtainH)
	}
	// 窗高 = 84 + 卷帘高。
	if got, want := s.miniCurtainTargetHeight(), miniHeight+miniCurtainH; got != want {
		t.Errorf("窗该有 %d 高，want %d（84 + 卷帘 %d）", got, want, miniCurtainH)
	}
	// ⚠️ **而且窗真的改到了那个高度**：这一句是「窗高跟着动画走」的那一半
	// （`miniCurtainSyncHeight` 里那一句 `SetSize`）。
	if got, want := s.miniCurtain.setHeight, miniHeight+miniCurtainH; got != want {
		t.Errorf("窗高被改成了 %d，want %d（84 + 卷帘 %d）", got, want, miniCurtainH)
	}
	// 收起之后窗高回到 84。
	s.miniCurtainCollapse(tt.Now())
	settleCurtain(t, tt, s, false)
	if got, want := s.miniCurtain.setHeight, miniHeight; got != want {
		t.Errorf("收起之后窗高是 %d，want %d：收起之后窗必须回到 84"+
			"（否则 `StateKey` 记的就是一个更高的尺寸）", got, want)
	}
}

// TestTheMiniCurtainClosesWithEscape 盯「Esc 收起」。
//
// ⚠️ **这一条量的是「窗口级 Esc」**：迷你窗没有浮层（卷帘是这扇窗自己的一段，
// 不是浮在上面的一层），所以走的是 `c.Shortcut(0, ui.KeyEscape)`——窗口级的
// 键（`mygo/ui/context.go:462`）。
//
// ⚠️ **真机上还有一个限制**：迷你窗是 `Panel`，点它**不激活应用**——于是
// 键盘还在原来那个应用手里，Esc 走的是**那个应用的窗口**，不是这一扇。
// ⇒ **只有迷你窗是 key 窗口的时候 Esc 才到得了**。无头 Tester 里焦点链是空的
// （`mygo/ui/input.go:633` 的 `focusChain` 从 `rt.focused` 往上走，而那一位
// 是 0），所以窗口级那一条直接命中——**这一条量得到的是「键到了这一扇窗
// 之后会发生什么」**，而「键到不到这一扇窗」是真机那一半（记在
// `miniplayer_test.go` 末尾那一节「真窗口才量得到」里）。
func TestTheMiniCurtainClosesWithEscape(t *testing.T) {
	s, a := miniCurtainShell(t)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle()
	a.PutLyricsFrom("", "[00:00.00]第一句\n[00:05.00]第二句\n", "")
	tt := miniCurtainTester(s)

	clickCurtainCover(t, tt)
	settleCurtain(t, tt, s, true)
	if !s.miniCurtain.open {
		t.Fatalf("起点就不对：卷帘没有展开，文字有 %q", tt.Texts())
	}

	tt.Key(0, mygoui.KeyEscape)
	tt.Frame()

	if s.miniCurtain.open {
		t.Error("按了 Esc 之后卷帘还开着：它没有收起来")
	}
	settleCurtain(t, tt, s, false)
	if got := s.miniCurtainTargetHeight(); got != miniHeight {
		t.Errorf("按了 Esc 之后窗该有 %d 高，want %d", got, miniHeight)
	}
	if miniCurtainHasLyrics(tt) {
		t.Errorf("按了 Esc 之后画面上还有歌词：卷帘没有收起来，文字有 %q", tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 设置开关
// ─────────────────────────────────────────────────────────────────────────────

// TestTheMiniLyricsCurtainSwitchIsOnByDefault 盯「默认是开的」。
//
// ⚠️ **判据是设置本身**：用户装上就有这个效果（票 #536）。而「装上就有」
// 这一句的另一半是「**一份老设置文件读出来也是开的**」——那是
// `app.Settings.MiniLyricsCurtainOn()` 的事（nil ⇒ 默认开）。
func TestTheMiniLyricsCurtainSwitchIsOnByDefault(t *testing.T) {
	if !app.DefaultSettings("").MiniLyricsCurtainOn() {
		t.Error("默认设置里那个开关是关的：票 #536 说它默认是开的（2026-10-26 更正）")
	}
	if !(app.Settings{}).MiniLyricsCurtainOn() {
		t.Error("零值设置里那个开关是关的：nil（没设过）必须算「默认开」")
	}
	// 一份**老设置文件**读出来（没有那一格）也必须是开的。
	old := app.DefaultSettings("")
	old.MiniLyricsCurtain = nil
	if !old.MiniLyricsCurtainOn() {
		t.Error("一份没有那一格的老设置读出来是关的：「默认开着」对老用户不成立")
	}
}

// TestTheMiniCurtainDoesNothingWhenTheSwitchIsOff 盯「开关关着时点封面什么
// 也不发生」。
//
// ⚠️ **判据有三条**：歌词没出现、卷帘没开着、**窗高还是 84**。
// 最后那一条是票面点名的那一条（「window-state 记的也是 320×84」）——
// 而无头这一档量不到真窗口，所以这里量的是**那一档的窗高目标**。
func TestTheMiniCurtainDoesNothingWhenTheSwitchIsOff(t *testing.T) {
	s, a := miniCurtainShell(t)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle()
	a.PutLyricsFrom("", "[00:00.00]第一句\n[00:05.00]第二句\n", "")
	if _, err := a.UpdateSettings(func(cur *app.Settings) {
		off := false
		cur.MiniLyricsCurtain = &off
	}); err != nil {
		t.Fatal(err)
	}
	tt := miniCurtainTester(s)
	tt.Frame()

	clickCurtainCover(t, tt)
	tt.Frame()
	tt.Advance(miniCurtainDuration)
	tt.Frame()

	if miniCurtainHasLyrics(tt) {
		t.Errorf("开关关着的时候点封面，卷帘还是展开了：画面上有歌词，文字有 %q", tt.Texts())
	}
	if s.miniCurtain.open {
		t.Error("开关关着的时候点封面，卷帘的状态还是开着的")
	}
	if s.miniCurtain.height != 0 {
		t.Errorf("开关关着的时候点封面，卷帘的高度是 %.1f，want 0", s.miniCurtain.height)
	}
	if got := s.miniCurtainTargetHeight(); got != miniHeight {
		t.Errorf("开关关着的时候窗该有 %d 高，want %d（那一档窗高一个字都不许动）",
			got, miniHeight)
	}
}

// TestTheMiniCurtainCollapsesWhenTheSwitchIsTurnedOff 盯「展开着的时候把开关
// 关掉 ⇒ 立刻收起」。
//
// ⚠️ **判据是「收起了」而不是「下一帧才收起」**：设置改完之后
// `App.UpdateSettings` → `a.invalidate()` **挨个叫醒每个窗口**（铁律三），
// 于是迷你窗下一帧读到的设置已经是关的 ⇒ 收起。
func TestTheMiniCurtainCollapsesWhenTheSwitchIsTurnedOff(t *testing.T) {
	s, a := miniCurtainShell(t)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle()
	a.PutLyricsFrom("", "[00:00.00]第一句\n[00:05.00]第二句\n", "")
	tt := miniCurtainTester(s)

	clickCurtainCover(t, tt)
	settleCurtain(t, tt, s, true)
	if !s.miniCurtain.open {
		t.Fatalf("起点就不对：卷帘没有展开，文字有 %q", tt.Texts())
	}

	// 用户在设置里把开关关掉。
	if _, err := a.UpdateSettings(func(cur *app.Settings) {
		off := false
		cur.MiniLyricsCurtain = &off
	}); err != nil {
		t.Fatal(err)
	}
	// ⚠️ **推钟 + 画帧**：铁律三是「挨个叫醒」，而那一帧要把设置读一遍。
	tt.Advance(16 * time.Millisecond)
	tt.Frame()

	if s.miniCurtain.open {
		t.Error("开关关掉之后卷帘还开着：它没有立刻收起来")
	}
	settleCurtain(t, tt, s, false)
	if got := s.miniCurtainTargetHeight(); got != miniHeight {
		t.Errorf("开关关掉之后窗该有 %d 高，want %d", got, miniHeight)
	}
	if miniCurtainHasLyrics(tt) {
		t.Errorf("开关关掉之后画面上还有歌词：卷帘没有收起来，文字有 %q", tt.Texts())
	}
}

// TestTheMiniCurtainCollapsesAtOnceWithoutBouncingBack 盯「立刻收起」真的是
// **一下子**到位，而不是**先弹回 320 再滑下去**。
//
// ⚠️ **这是这一票踩到的一个真缺陷**：卷帘高度是一个**动画值**，而
// `miniCurtainCollapseNow`（开关关掉 / 关窗那两条路走的）只把
// `height` 归零了——**动画通道里的值还在 236**。于是下一帧那个
// `AnimateTo(0)` 从 236 往 0 滑，窗于是**先弹回 320 高**，再一路缩到 84。
//
// 而那 240ms 里**窗比内容高**：这一扇窗是透明的，**透明的那一块照样吃
// 点击**——底下那个应用的按钮就点不到那 236 像素。而票面要的是「**立刻
// 收起**」，不是「正在收」。
//
// ⚠️ **判据是「那一帧窗高就是 84」**（不是「最后到了 84」）：滑下去那条路
// 最后也会到 84，所以只断终态的话，这个缺陷照红照绿。
func TestTheMiniCurtainCollapsesAtOnceWithoutBouncingBack(t *testing.T) {
	s, a := miniCurtainShell(t)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle()
	a.PutLyricsFrom("", "[00:00.00]第一句\n[00:05.00]第二句\n", "")
	tt := miniCurtainTester(s)

	clickCurtainCover(t, tt)
	settleCurtain(t, tt, s, true)
	if got := s.miniCurtain.setHeight; got != miniHeight+miniCurtainH {
		t.Fatalf("起点就不对：窗高是 %d，want %d", got, miniHeight+miniCurtainH)
	}

	// 「立刻收起」（开关关掉 / 关窗那两条路走的就是它）。
	s.miniCurtainCollapseNow()
	// ⚠️ **只推一帧**（16ms）：滑下去那条路在第一帧还在 236 附近，而
	// 「立刻收起」必须是**这一帧就已经是 84**。
	tt.Advance(16 * time.Millisecond)
	tt.Frame()

	if got := s.miniCurtain.height; got != 0 {
		t.Errorf("立刻收起之后卷帘高 %.1f，want 0：它还在滑下去（窗会先弹回 320）", got)
	}
	if got := s.miniCurtain.setHeight; got != miniHeight {
		t.Errorf("立刻收起之后窗高是 %d，want %d：窗不该先弹回 %d 再滑下来",
			got, miniHeight, miniHeight+miniCurtainH)
	}
	if miniCurtainHasLyrics(tt) {
		t.Errorf("立刻收起之后画面上还有歌词：卷帘没有收起来，文字有 %q", tt.Texts())
	}
}

// TestTheMiniLyricsCurtainSwitchSurvivesARestart 盯「开关落盘」。
//
// ⚠️ **走的是生产那条路**：先写盘、再 `MountSettings` 读回来——和真程序
// 启动时一样（`shelltest_test.go` 的 `withSettings` 就是这一条路）。
func TestTheMiniLyricsCurtainSwitchSurvivesARestart(t *testing.T) {
	// ⚠️ **种子设置是「开」**：这一条要验的是「**用户把它关掉**之后重启还在
	// 关着」，所以起点得是「开」——不然下面那一步关不掉任何东西，这条用例
	// 就成了空转（而「空转」正是评审用负控制抓出来的那一类问题）。
	st := app.DefaultSettings("")
	on := true
	st.MiniLyricsCurtain = &on

	sh := newTestShell(t, withSettings(st))
	if !sh.a.SettingsSnapshot().MiniLyricsCurtainOn() {
		raw, _ := os.ReadFile(sh.settingsPath)
		t.Fatalf("起点就不对：那份设置读出来是关的 path=%s file=%s", sh.settingsPath, string(raw))
	}

	// 🔴 **走生产的改设置那条路**（`App.UpdateSettings` ⇒ store.Update ⇒
	// 后台落盘），**不是**往界面上硬塞一个 Settings 值。
	//
	// ⚠️ **为什么这一句是关键**：改设置的那条路被换掉（不落盘 / 丢弃改动）
	// 的话，下面「重新读一遍盘」读到的还是**开**的 ⇒ 这一条红。
	// 而只断言 `SettingsSnapshot()` 的话，**改动被整条丢弃也照样绿**——
	// 那个快照是从内存里那份读出来的，压根没经过盘。
	if _, err := sh.a.UpdateSettings(func(cur *app.Settings) {
		v := false
		cur.MiniLyricsCurtain = &v
	}); err != nil {
		t.Fatal(err)
	}
	if sh.a.SettingsSnapshot().MiniLyricsCurtainOn() {
		t.Fatal("改完之后快照里那个开关还是开的：设置压根没被改")
	}

	// ⚠️ **落盘**：store 的写盘是后台合并写的（`SettingsStore.loop`），
	// 不等它就立刻读盘，读到的还是**旧内容**。这一句是等它写完。
	if err := sh.a.CloseSettings(); err != nil {
		t.Fatal(err)
	}

	// 🔴 **重新读**那**同一条文件**（模拟重启）：不是「再写一份新的设置
	// 进去读出来」——那是**读自己刚写进去的那一份种子**，压根不经过落盘。
	got, err := app.LoadSettings(sh.settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.MiniLyricsCurtainOn() {
		t.Errorf("重启之后那个开关又变回开的了：它没有落盘"+
			"（读回来的是 %v）", miniCurtainSwitchText(got.MiniLyricsCurtain))
	}
	// ⚠️ **顺带断一下文件里真的写着那一格**：落盘那一层有可能把这一格整个
	// 吞掉（而读回来的是默认值「开」——那样上面那条会红，但这里说得更准）。
	if got.MiniLyricsCurtain == nil {
		t.Error("落盘之后那一份设置里压根没有「miniLyricsCurtain」这一格")
	}
}

// miniCurtainSwitchText 把那个开关的指针值说成人话。
//
// ⚠️ **它报的是「读回来的那一格是开着还是关着」**：而那一格**可能压根没有**
// （`nil` = 没设过 = 默认开）——而**指针的地址对读者毫无意义**（报错里出现
// 一个 `0x4be43a2ea188` 谁也看不懂）。
func miniCurtainSwitchText(v *bool) string {
	if v == nil {
		return "没设过（默认开）"
	}
	if *v {
		return "开"
	}
	return "关"
}

// ─────────────────────────────────────────────────────────────────────────────
// 高度夹住工作区
// ─────────────────────────────────────────────────────────────────────────────

// TestTheMiniCurtainSwitchIsOnTheSettingsPage 盯「设置页上有那一格」。
//
// ⚠️ **这一条量的是「画面上找得到那一格」**：设置页上少一格，用户是**不会知道
// 的**——而那一格是这一票的**开关入口**（票 #536 的补充要求）。而「设置里
// 写了那个键」证明不了界面上真有那一格。
//
// ⚠️ **走的是生产的读盘那条路**（`shellOnSettings` → `withSettings`）：先写盘、
// 再挂上去——和真程序启动时一样。
//
// ⚠️ **它在「常规设置」那一档的「播放」分组里**：设置页有四个 Tab（`常规设置
// / 菜单设置 / 快捷键设置 / 代理设置`），而分组（外观 / 播放 / 下载 / 搜索 /
// 系统）都在**同一个 Tab** 里（`settings_tab_general.go` 的 `settingsGeneral`）。
func TestTheMiniCurtainSwitchIsOnTheSettingsPage(t *testing.T) {
	tt, s, _, _ := shellOnSettings(t, app.DefaultSettings(""))

	// 设置页外面套了一层 `Scroll`，而那一格在「播放」分组里——它在第一屏之下，
	// 所以要**滚进可视区**再找（`settingRect` 就是干这个的）。
	r := settingRect(t, tt, s, "迷你播放器歌词卷帘")
	if r.W <= 0 || r.H <= 0 {
		t.Errorf("「迷你播放器歌词卷帘」那一格没有尺寸：%v", r)
	}
	// 开关自己叫「开关：迷你播放器歌词卷帘」（`pickSwitch` 那一句）。
	if _, ok := tt.Find("开关：迷你播放器歌词卷帘"); !ok {
		t.Errorf("设置页上没有那个开关，文字有 %q", tt.Texts())
	}
}

// TestTheMiniCurtainIsClampedToTheWorkArea 盯「卷帘不超出工作区」。
//
// ⚠️ **这一条量的是纯逻辑**（`miniCurtainHeight`）：无头环境里没有真显示器，
// 而「窗在屏幕下半部时往下长会出屏」这件事正是那一个函数的职责。
func TestTheMiniCurtainIsClampedToTheWorkArea(t *testing.T) {
	work := miniCurtainRect{X: 0, Y: 0, Width: 1920, Height: 1080}
	for _, c := range []struct {
		name string
		win  miniCurtainRect
		want int
	}{
		{"窗口在右上角：放得下 236", miniCurtainRect{X: 1600, Y: 12, Width: 320, Height: 84}, miniCurtainH},
		{"窗口在正中间：够用但不满", miniCurtainRect{X: 800, Y: 400, Width: 320, Height: 84}, miniCurtainH},
		{
			// 窗底离工作区底边只剩 1080 − 900 − 84 = 96，减掉那道 12 的边距
			// ＝ 84 < 120 ⇒ **不展开**。
			name: "窗口在屏幕下半部：放不下就不展开",
			win:  miniCurtainRect{X: 1600, Y: 900, Width: 320, Height: 84},
			want: 0,
		},
		{
			// 剩 1080 − 800 − 84 = 196，减掉那道 12 的边距 ＝ 184：夹到 184
			//（不是 236）。
			name: "窗口在下半部：夹住",
			win:  miniCurtainRect{X: 1600, Y: 800, Width: 320, Height: 84},
			want: 184,
		},
	} {
		if got := miniCurtainHeight(work, c.win); got != c.want {
			t.Errorf("%s：卷帘高 %d，want %d", c.name, got, c.want)
		}
	}

	// 工作区不知道（没有真窗口）⇒ 不展开。
	if got := miniCurtainHeight(miniCurtainRect{}, miniCurtainRect{X: 0, Y: 0, Width: 320, Height: 84}); got != 0 {
		t.Errorf("工作区不知道的时候卷帘高 %d，want 0（放不下就不展开）", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 工具
// ─────────────────────────────────────────────────────────────────────────────

// TestRenderMiniCurtainShots 把迷你播放器的**卷帘**存成 PNG，留给人眼对照。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderMiniCurtainShots
//
// ⚠️ **两张都要出**：收起那一版（320×84）和展开那一版（320×320）——而
// **卷帘那一版才是这一票要看的那一版**（布局 / 间距 / 层级 / 密度 / 对齐，
// AGENTS.md §1《UI 票的验收必须有一次「视觉审查」》）。
//
// ⚠️ **深浅两套都要**：深色下那一列和右边那块的层次差别更明显，而浅色下
// 封面的圆角和卡片底几乎同色（DESIGN.md §八）。
func TestRenderMiniCurtainShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	s, a := miniCurtainShell(t)
	if err := enqueueTwo(a, t); err != nil {
		t.Fatal(err)
	}
	a.Toggle() // 暂停：画面上那个键写着「播放」
	a.PutLyricsFrom("", "[00:00.00]第一句\n[00:05.00]第二句\n[00:10.00]第三句\n", "")
	tt := miniCurtainTester(s)
	tt.Frame()

	// 收起那一版。
	save(t, dir, "mini-curtain-01-light-collapsed.png", tt)

	// 展开那一版。
	clickCurtainCover(t, tt)
	settleCurtain(t, tt, s, true)
	save(t, dir, "mini-curtain-02-light-expanded.png", tt)

	// 深色。
	if _, err := a.UpdateSettings(func(st *app.Settings) { st.ThemeMode = app.ThemeModeDark }); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	save(t, dir, "mini-curtain-03-dark-expanded.png", tt)
}

// clickCurtainCover 点一下封面那一列（展开 / 收起的触发器）。
func clickCurtainCover(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	cover, ok := tt.Find("展开歌词")
	if !ok {
		t.Fatalf("画面上找不到封面那一列（它的可及名字是「展开歌词」），文字有 %q", tt.Texts())
	}
	tt.ClickAt(cover.X+cover.W/2, cover.Y+cover.H/2)
	tt.Frame()
}

// settleCurtain 推到卷帘**到位**为止（开着 / 收着都行）。
//
// ⚠️ **推钟而不是数帧**：这一段是**按时间**走的（240ms 的定时长曲线），
// 而 Tester 的钟站着不动——数帧那种等法会**静默失效**（不报错、不超时、
// 直接放行；AGENTS.md §2）。
//
// ⚠️ **判据是状态而不是时间**：`open` 是**用户意图**那个状态，动画到位
// 的时候它已经是对的了；而**高度**到位是另一件事（动画走完）。
//
// 🔴 **判据是「弹簧停稳了」，不是「没有帧要出」**（票 #307 修 F1 的第二
// 半）：那个「激活应用」的重试窗口（1200ms）开着的时候，迷你窗**每一帧都
// 有帧要出**（重试要有帧源）——而它跟卷帘的动画**没关系**。⇒ 拿「没有帧
// 要出」当「动画走完了」的判据，收起/展开的用例就会**永远等不到**（实测：
// 推了 960ms 的钟还是「卷帘还没有变成开着」）。
//
// ⚠️ **而「高度不再变」也不行**：弹簧是**欠阻尼**的（回弹两三下），而**转
// 向点附近高度几乎不动**（一帧 0.02 像素）——拿它当判据会在**第一个峰**上
// 收手（实测：卷帘停在 233.6 而不是 236）。
//
// ⇒ **判据是弹簧自己那个 `moving`**：它停稳的时候正好把 `p` 钉在目标上
// （`minicurtain_spring.go` 的停止条件那一条）。
func settleCurtain(t *testing.T, tt *mygoui.Tester, s *Shell, open bool) {
	t.Helper()
	const step = 16 * time.Millisecond
	for range 120 {
		tt.Advance(step)
		tt.Frame()
		if openCurtainState(tt) != open {
			// 状态还没对（歌词还没出现 / 还没消失）：接着等。
			continue
		}
		// ⚠️ **弹簧停稳了 = 高度到位了**：它停稳的时候正好把 `p` 钉在目标上。
		if !s.miniCurtain.spring.moving {
			return
		}
	}
	t.Fatalf("推了 %v 的钟，卷帘还没有变成「%v」（高度 %v，弹簧还在动：%v）",
		120*step, open, s.miniCurtain.height, s.miniCurtain.spring.moving)
}

// openCurtainState 报画面上卷帘是开着还是收着。
//
// ⚠️ **它量的是画面**（歌词在不在），不是那个状态字段——用例要断的是
// **用户看得见的那件事**，而状态字段是实现。
func openCurtainState(tt *mygoui.Tester) bool {
	return tt.HasText("第一句")
}
