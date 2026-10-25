// 全屏播放器这一组：共享元素转场、3 秒藏界面、分集面板、偏好面板里的频谱开关。
//
// 走 MyGo 的 Tester：和真窗口一模一样的 layout 与 paint，只是画到内存里。
// 点得到、找得到框、读得到像素，所以「点封面能开」「3 秒后界面上一个像素都不剩」
// 「开着分集的面板里列出了三集」这些都是**画面上的事**，不是「调了哪个函数」。
package ui

import (
	"image/color"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// Biu 的那几个数，在这里**写死字面量**，不去用 fullplayer.go 里的同名常量。
//
// 为什么不用那边的常量：布局和期望都用它的话，常量一改两边一起跟着变，测试就
// 发现不了了。它们的真值只来自 Biu 的源码，不来自我们的常量。
const (
	biuRowCoverSize = 52  // 设计规范 v0.1 §7.1：播放条封面 52×52（原 Biu 是 56）
	biuFullCoverW   = 320 // 设计规范 v0.1 §7.5：正方形 320（窗口宽 < 1000 用 240）
	biuFullCoverH   = 320 // 正方形
	biuHideSeconds  = 3   // index.tsx:132 的 scheduleHideUi(3000)
)

// fullWin 是测试窗口的尺寸（newShell 用的也是这个）。
const (
	fullWinW = 1200
	fullWinH = 800
)

// fullTester 建一个开着歌的界面，**假时钟**已经换上。
//
// 为什么换假时钟：那三秒必须是假时钟测的（`PLAN.md` 的验收项），而真等三秒一条
// 测试就是三秒。
//
// ⚠️ **这一份是暂停着的**（票 #468）：满屏的封面现在**在转**，而那一转是
// **常驻**的——框架于是**一直要帧**，「动画停下来了」这一条永远等不到
// （`settleFull` 等的就是它）。所以这一族测试先把歌暂停：封面不转，动画就
// 会停。
//
// 要**正在放**的那一份的测试，自己 `s.app.Toggle()` 接着放（见
// `TestTheSpectrumIsActuallyPaintedWithThatColor`——它要的是解码器真的吐数据）。
func fullTester(t *testing.T) (*Shell, *mygoui.Tester, *fakeClock) {
	t.Helper()
	s, tt := newShell(t)
	// 🔴 在建 Tester **之前**换上：Tester 建好就跑了一帧，那一帧已经按真时钟把
	// 「最后一次指针移动」记成墙钟了，换晚了三秒就成了三秒加一大截。
	fc := newFakeClock(time.Unix(1_700_000_000, 0))
	s.clock = fc

	// 🔴 一律深色主题：`whitePixels` 数的是「画面上接近白的像素」，而浅色主题下
	// 满屏背景本身就是白的，藏没藏界面数出来都一样（实测藏起来还有 11 万个）。
	// 深色下画面上唯一亮的东西就是界面自己画的字和控件——见 whitePixels。
	tt.SetDark(true)

	if err := s.app.Enqueue(app.LocalItem(flacPath(t))); err != nil {
		t.Fatalf("把歌放进队列：%v", err)
	}
	// 等播放器把歌打开（异步，见 playbar_test.go 的 waitFor）。本地的 Open 是
	// 同步的，一帧就够。
	tt.Frame()
	if !s.app.Snapshot().HasTrack {
		t.Fatalf("歌没开起来：%q", s.app.Snapshot().Err)
	}
	// ⚠️ **暂停**（理由见这个函数的注释）。
	s.app.Pause()
	return s, tt, fc
}

// advanceFull 把两个钟都往前推。
//
// ⚠️ **两个都要推**：`s.clock` 是界面层那个（判三秒），Tester 自己还有一个
// （跑动画）。只推一个的话，要么三秒到不了、要么动画停在半路——而动画停在半路
// 的时候画面上仍然有字，看着像「没藏起来」。
func advanceFull(s *Shell, tt *mygoui.Tester, fc *fakeClock, d time.Duration) {
	fc.Advance(d)
	tt.Advance(d)
}

// ---- 共享元素转场 ----

// TestTheFullPlayerControlsAreAllIcons 钉住满屏那一行**五枚都是图标**。
//
// 🔴 **这一条是被评审指出来的**：满屏那一行原来是「图标 — 文字 — 图标」半新半旧
// ——两边的上一首/下一首复用了播放条那几个（图标化之后成了图标），中间那个播放/
// 暂停还是 `ui.PrimaryButton(c, "暂停").Width(96)` 的文字按钮。满屏是用户盯着看得最
// 久的一屏，中间夹一个绿色文字块和两个灰色箭头，怎么看都不像一套。
//
// # 判据是「图形是方的」，不是「那一块有两种颜色」
//
// 「有两种颜色」断不出来：主色按钮的底是主色、字是白的，本来就是两种颜色，
// 而图标是白的、底也是主色——**两种颜色和文字按钮一模一样**。
//
// 能分开的是**图形的宽高比**：一个 48 见方的图标（上游 `size-12 min-w-12` 的
// 盒子，图形正好填满），包围盒接近正方；「暂停」两个字是 26×15 的横条，宽明显
// 大于高。
//
// 另外钉住两件小事：五枚**中心在同一行**（绝对 y 不是判据——那一行是 `Absolute`
// 贴在底部的，它的 y 是「窗口高 − 92」），以及从左到右是
// 「模式 · 上一首 · 播放 · 下一首 · 队列」。
func TestTheFullPlayerControlsAreAllIcons(t *testing.T) {
	s, tt, fc := fullTester(t)
	s.openFullPlayer()
	tt.Frame()
	if !tt.HasText("关闭全屏") {
		t.Fatalf("满屏没开，文字有 %q", tt.Texts())
	}
	advanceFull(s, tt, fc, 400*time.Millisecond) // 过渡走完，那一行的不透明度才是 1
	tt.Frame()

	middle := transportLabel(s.app.Snapshot().Playing)
	want := []string{"播放模式", "上一首", middle, "下一首", "播放列表"}

	var lastX float32
	for i, label := range want {
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("满屏那一行没有 %q，文字有 %q", label, tt.Texts())
		}
		g := scanGlyph(tt, label)
		if g.ink == "" {
			t.Errorf("%q 那一块没画东西（%.1f,%.1f %.1fx%.1f）", label, r.X, r.Y, r.W, r.H)
			continue
		}
		// 方的是图标，横的是字。
		if g.box.W > g.box.H*1.3 {
			t.Errorf("%q 画出来是 %.1f×%.1f 的**横条**（宽明显大于高）——"+
				"那还是文字按钮，满屏这一行成了「图标 — 文字 — 图标」",
				label, g.box.W, g.box.H)
		}
		if i > 0 && r.X <= lastX {
			t.Errorf("%q 的左边缘 %.1f 不在 %q（%.1f）右边——顺序反了",
				label, r.X, want[i-1], lastX)
		}
		lastX = r.X
	}
}

// TestTheCoverOpensTheFullScreenPlayer 钉住入口：点播放条上那张封面，全屏开。
//
// 也钉住反过来的那半句：没歌的时候点封面不开全屏（而是说一句人话）。
func TestTheCoverOpensTheFullScreenPlayer(t *testing.T) {
	s, tt, _ := fullTester(t)

	row, ok := tt.Find("当前封面")
	if !ok {
		t.Fatalf("画面上找不到播放条那张封面，文字有 %q", tt.Texts())
	}
	if row.W != biuRowCoverSize || row.H != biuRowCoverSize {
		t.Fatalf("列表行上那张封面是 %vx%v，Biu 是 %d 见方", row.W, row.H, biuRowCoverSize)
	}
	if tt.HasText("关闭全屏") {
		t.Fatal("还没点就已经是全屏了")
	}

	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	if !tt.HasText("关闭全屏") {
		t.Fatalf("点了封面全屏没开，文字有 %q", tt.Texts())
	}
	if !tt.HasText("全屏设置") {
		t.Errorf("顶上那一条没有偏好面板的入口，文字有 %q", tt.Texts())
	}
	if s.full.open != true {
		t.Error("外壳上记的仍然是关着的")
	}
}

// TestTheCoverFliesFromTheRowIntoTheFullScreenBackground 是这一票的核心验收：
// 封面**从列表行放大成全屏背景**，不是「啪」地一下就位。
//
// 这就是 SwiftUI `matchedGeometryEffect` 的等价物（框架 `ElementTransition.Match`
// 自己的注释就是这么写的）：同一个身份（`coverMatch`）记着上一帧它在哪，
// 新出现的那一个从那儿长过来。
//
// 断的是三段：
//
//	刚点完      它还**在**列表行那个框里（转场是从那儿起的）
//	推进 80ms   它已经长大了（真在动）
//	推进 2s     它落在满屏那个框上，384×288、居中
//
// ⚠️ **这一条先把歌暂停了**（票 #468）：满屏的封面现在**在转**，而转起来之后
// 封面那个框的**可见外接矩形**会变大（一个转起来的正方形，外接框是
// 320×(cosθ+sinθ)）——这一条量的是「它落在哪儿、多大」，所以量的是**没转**
// 的那一份。转起来的那一份由 `fullplayer_spin_test.go` 断。
func TestTheCoverFliesFromTheRowIntoTheFullScreenBackground(t *testing.T) {
	s, tt, fc := fullTester(t)
	// ⚠️ **暂停**：满屏的封面在转（票 #468），而这一条量的是封面那个框的
	// 大小和位置——转起来之后那个框的可见外接矩形会变大（见上面的注释）。
	s.app.Pause()

	row, ok := tt.Find("当前封面")
	if !ok {
		t.Fatalf("找不到封面，文字有 %q", tt.Texts())
	}

	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)

	start, ok := tt.Find("当前封面")
	if !ok {
		t.Fatalf("全屏开了却找不到封面，文字有 %q", tt.Texts())
	}
	// 刚点完：Tester 的钟是**冻着**的，动画还没走一步，所以它应该还在原来的框里。
	if abs32(start.W-row.W) > 1 || abs32(start.H-row.H) > 1 {
		t.Errorf("刚点完封面是 %vx%v，列表行上那张是 %vx%v —— 转场没有从那儿起",
			start.W, start.H, row.W, row.H)
	}

	advanceFull(s, tt, fc, 80*time.Millisecond)
	mid, ok := tt.Find("当前封面")
	if !ok {
		t.Fatalf("中途找不到封面，文字有 %q", tt.Texts())
	}
	if mid.W <= row.W {
		t.Errorf("推进 80ms 之后封面还是 %v 宽（起点的 %v），它没有在长大", mid.W, row.W)
	}

	advanceFull(s, tt, fc, 2*time.Second)
	end, ok := tt.Find("当前封面")
	if !ok {
		t.Fatalf("停下来之后找不到封面，文字有 %q", tt.Texts())
	}
	if end.W != biuFullCoverW || end.H != biuFullCoverH {
		t.Errorf("满屏的封面是 %vx%v，Biu 的是 %dx%d（1200×800 的窗口）",
			end.W, end.H, biuFullCoverW, biuFullCoverH)
	}
	// 落位：它**靠左边那一栏的右沿**（上游 `justify-end` + `px-12`，
	// `full-screen-player/index.tsx:331-332`）。
	//
	// ⚠️ **这些数写死在这儿**：布局和期望都用生产代码那个常量的话，常量一改
	// 两边一起变，测试就发现不了了（这个文件的规矩）。
	//
	//	左边那一栏   600（上游 50/50，1200 的一半）
	//	右边留白     48（上游 `px-12`）
	//	封面         384 宽 → 右沿 552、左沿 168、中心 360
	//
	// 上游实图量出来的是 x=168..551（报告 S-22）。
	const (
		wantCoverColW = 600
		wantCoverPad  = 48
	)
	wantCenter := float32(wantCoverColW - wantCoverPad - biuFullCoverW/2)
	if d := abs32(end.X + end.W/2 - wantCenter); d > 1 {
		t.Errorf("满屏封面的中心在 x=%v，want %v（左边那一栏 %d 宽，右边留 %d）",
			end.X+end.W/2, wantCenter, wantCoverColW, wantCoverPad)
	}
	if d := abs32(end.X - (wantCenter - biuFullCoverW/2)); d > 1 {
		t.Errorf("满屏封面的左沿在 x=%v，want %v", end.X, wantCenter-biuFullCoverW/2)
	}
}

// TestTheCoverFliesBackWhenItCloses 是反过来的那半：关掉的时候它也长回去。
//
// 这一条断的是「开着全屏的时候播放条不建」那个决定（见 fullplayer.go 顶上）：
// 播放条要是在满屏开着的时候照建，关掉那一帧它就不是「新出现的」，
// 封面回不去——「啪」地一下就没了。
func TestTheCoverFliesBackWhenItCloses(t *testing.T) {
	s, tt, fc := fullTester(t)

	row, _ := tt.Find("当前封面")
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	advanceFull(s, tt, fc, 2*time.Second)

	if err := tt.Click("关闭全屏"); err != nil {
		t.Fatalf("点不了关闭：%v（文字：%q）", err, tt.Texts())
	}
	back, ok := tt.Find("当前封面")
	if !ok {
		t.Fatalf("关掉之后找不到封面，文字有 %q", tt.Texts())
	}
	if abs32(back.W-biuFullCoverW) > 1 {
		t.Errorf("刚关掉封面是 %v 宽，满屏那张是 %v —— 它是从满屏那个框开始长回去的吗",
			back.W, float32(biuFullCoverW))
	}
	advanceFull(s, tt, fc, 2*time.Second)
	end, _ := tt.Find("当前封面")
	if abs32(end.W-biuRowCoverSize) > 1 || abs32(end.H-biuRowCoverSize) > 1 {
		t.Errorf("停下来之后封面是 %vx%v，列表行上那张是 %d 见方",
			end.W, end.H, biuRowCoverSize)
	}
}

// ---- 三秒藏界面 ----

// TestTheInterfaceHidesAfterThreeSeconds 是 SPEC 点名的那一条（用户故事 41）。
//
// ⚠️ **全程假时钟**，一个字都不真等。三秒是在 `s.clock` 上算的，Tester 自己的钟
// 只用来跑那段淡出的动画。
func TestTheInterfaceHidesAfterThreeSeconds(t *testing.T) {
	s, tt, fc := fullTester(t)
	openFull(t, tt)

	// 指针在满屏里动一下——Biu 的 `onMouseMove` 就是这么把界面叫回来的。
	tt.Move(600, 400)
	tt.Frame()

	// 2.9 秒：还在。
	advanceFull(s, tt, fc, biuHideSeconds*time.Second-time.Millisecond*100)
	if s.fullHidden() {
		t.Fatal("才 2.9 秒界面就藏起来了")
	}
	if n := whitePixels(tt); n == 0 {
		t.Error("界面上一个亮像素都没有，它压根没画出来过")
	}

	// 再过 0.1 秒：藏了。这条断的是「三秒」这个数本身——把它改成两秒，
	// 2.9 秒那一条就红。
	advanceFull(s, tt, fc, 200*time.Millisecond)
	if !s.fullHidden() {
		t.Fatalf("满 3 秒界面还露着（lastMove=%v now=%v）",
			s.full.lastMove, s.clock.Now())
	}
	// 光标也跟着没（Biu 的 `cursor: none`，index.tsx:209）。
	if tt.Cursor() != mygoui.CursorNone {
		t.Errorf("界面藏起来了光标还在（%v）", tt.Cursor())
	}
}

// TestTheHiddenInterfaceIsReallyGoneFromTheScreen 是上一条的另一半：断言的是
// **画面**，不是那个布尔。
//
// 淡出是 300ms（Biu 的 `duration-300`），所以得推过淡出再量：一个还在淡出的
// 按钮当然还有像素，而那不是「没藏起来」。
func TestTheHiddenInterfaceIsReallyGoneFromTheScreen(t *testing.T) {
	s, tt, fc := fullTester(t)
	// ⚠️ **歌词是内容，不是界面**：满屏接上歌词之后（票 #236），界面藏起来
	// 歌词**还在**（见 `TestTheLyricsStayWhenTheInterfaceHides`）——而这一条量的是
	// 「界面上还有没有亮像素」，歌词的字也在那儿，会把量出来的东西搅浑。
	// 所以这一条把歌词关掉，量的就是界面本身。
	fullNoLyricsPrefs(t, s)
	openFull(t, tt)
	tt.Move(600, 400)
	tt.Frame()

	if n := whitePixels(tt); n == 0 {
		t.Fatal("界面上一个亮像素都没有，它压根没画出来过")
	}

	// 3 秒到点 + 淡出走完。
	advanceFull(s, tt, fc, biuHideSeconds*time.Second+time.Second)
	if n := whitePixels(tt); n != 0 {
		t.Errorf("界面藏起来之后画面上还有 %d 个亮像素", n)
	}
}

// TestMovingThePointerBringsTheInterfaceBack 是「移动时回来」那半句。
func TestMovingThePointerBringsTheInterfaceBack(t *testing.T) {
	s, tt, fc := fullTester(t)
	openFull(t, tt)
	tt.Move(600, 400)
	tt.Frame()

	advanceFull(s, tt, fc, biuHideSeconds*time.Second+time.Second)
	if !s.fullHidden() {
		t.Fatal("没有到 3 秒界面就自己露出来了")
	}

	tt.Move(604, 404) // 动了
	tt.Frame()
	if s.fullHidden() {
		t.Error("指针动了界面还藏着")
	}
	// 再等三秒它又藏回去——「回来」不是「再也不藏」。
	advanceFull(s, tt, fc, biuHideSeconds*time.Second+time.Second)
	if !s.fullHidden() {
		t.Error("指针停了之后界面没有再藏起来")
	}
}

// TestTheSettingsPanelKeepsTheInterfaceUp 是 Biu 的 `if (isSettingsOpen) return`：
// 偏好面板开着的时候不藏界面。
//
// 少了这一条的症状很好认：用户点开设置面板想调频谱，面板「自己没了」。
func TestTheSettingsPanelKeepsTheInterfaceUp(t *testing.T) {
	s, tt, fc := fullTester(t)
	openFull(t, tt)
	tt.Move(600, 400)
	tt.Frame()

	if err := tt.Click("全屏设置"); err != nil {
		t.Fatalf("点不了偏好面板：%v（文字：%q）", err, tt.Texts())
	}
	tt.Frame()
	if !tt.HasText("显示频谱图") {
		t.Fatalf("偏好面板没开，文字有 %q", tt.Texts())
	}

	advanceFull(s, tt, fc, biuHideSeconds*time.Second+time.Second)
	if s.fullHidden() {
		t.Fatal("偏好面板开着，界面却藏起来了")
	}

	// 关掉面板之后，三秒要**重新算起**。
	//
	// ⚠️ 不重新排的症状是「面板一关，界面瞬间就没了」：开着设置时那个钟被拆掉了，
	// 而 `lastMove` 还停在面板打开之前，`fullHidden` 于是立刻算出「已经三秒没动」。
	// Biu 那边是关掉设置面板之后还能看着界面继续调，不是「一松手就黑」。
	tt.Key(0, mygoui.KeyEscape)
	tt.Frame()
	tt.Frame()
	if tt.HasText("显示频谱图") {
		t.Fatalf("Esc 没关掉偏好面板，文字有 %q", tt.Texts())
	}
	if s.fullHidden() {
		t.Error("刚关掉面板界面就藏起来了")
	}
	advanceFull(s, tt, fc, biuHideSeconds*time.Second+time.Second)
	if !s.fullHidden() {
		t.Error("关掉面板之后三秒到了界面还是亮着 —— 那个钟没有重新排上")
	}
}

// TestEscapeClosesTheFullScreen 断关掉的那三条路里最容易漏的一条。
func TestEscapeClosesTheFullScreen(t *testing.T) {
	s, tt, _ := fullTester(t)
	openFull(t, tt)

	tt.Key(0, mygoui.KeyEscape)
	tt.Frame()
	if s.full.open {
		t.Errorf("按了 Esc 全屏还开着，文字有 %q", tt.Texts())
	}
	if tt.HasText("关闭全屏") {
		t.Errorf("关掉之后界面上还有满屏那些字，文字有 %q", tt.Texts())
	}
}

// ---- 频谱的开关 ----

// TestTheSpectrumIsOffUntilTheUserTurnsItOn 断「默认是关的」。
//
// Biu 的 `defaultSettings.showSpectrum: false`。开着全屏什么开关都没碰的时候，
// 频谱那个区域**压根不该存在**——不是「画了一片空」。
func TestTheSpectrumIsOffUntilTheUserTurnsItOn(t *testing.T) {
	s, tt, _ := fullTester(t)
	openFull(t, tt)

	if _, ok := tt.Find("频谱"); ok {
		t.Fatalf("没开开关频谱就画出来了")
	}
	if err := tt.Click("全屏设置"); err != nil {
		t.Fatalf("点不了偏好面板：%v（文字：%q）", err, tt.Texts())
	}
	tt.Frame()
	// ⚠️ 点的是**开关**（命名是「开关：那一行的标题」），不是左边那行标题的
	// 文字：后者是兄弟节点，点它什么也不会发生。
	if err := tt.Click("开关：显示频谱图"); err != nil {
		t.Fatalf("点不了频谱开关：%v（文字：%q）", err, tt.Texts())
	}
	tt.Frame()
	tt.Frame()
	if _, ok := tt.Find("频谱"); !ok {
		t.Fatalf("开了开关频谱还是没有，文字有 %q", tt.Texts())
	}
	// 偏好也要存住：这一帧是全屏开着的那一帧，下一次开全屏还是得画。
	if !s.app.FullPlayerPrefs().ShowSpectrum {
		t.Error("偏好里没有记下频谱开着")
	}
}

// ---- 频谱画出来是什么颜色 ----

// TestTheSpectrumColorFallsBackToTheDefault 断「默认色是 #666」。
//
// Biu 的默认值存的是字符串 `"currentColor"`，画的时候再判断一次是不是它、是就
// 落到 `#666`（`audio-waveform/index.tsx:110`）。我们在读偏好那一步就把那次判断
// 提前了（见 `app.FullPlayerPrefs`），于是这一层拿到的要么是 `#666`、要么是用户
// 挑的那个。
//
// 认不出来的串也落到 `#666`：照着画的话，界面上是一条**纯黑**的频谱摆在深色
// 背景上——看着像「没画出来」，而用户是在用着一个自己挑过的颜色。
func TestTheSpectrumColorFallsBackToTheDefault(t *testing.T) {
	want := mygoui.Color{R: 0x66, G: 0x66, B: 0x66, A: 255}
	for _, hex := range []string{"", "#666", "666", "红", "#12345"} {
		if got := spectrumColorOf(hex); got != want {
			t.Errorf("颜色 %q 画成了 %v，want #666", hex, got)
		}
	}
	if got := spectrumColorOf("#ff0000"); got != (mygoui.Color{R: 255, A: 255}) {
		t.Errorf("用户挑的红色没被照用：%v", got)
	}
}

// TestTheSpectrumIsActuallyPaintedWithThatColor 是上一条的下半截：断言的是
// **画面上真的涂了这个颜色**。
//
// 走的是真链路：真 FLAC 文件 → 真解码链 → 真挂点 → 真 FFT → 真画。判据是
// 频谱那条带上有没有接近 #666 的像素。
func TestTheSpectrumIsActuallyPaintedWithThatColor(t *testing.T) {
	s, tt, _ := fullTester(t)
	// ⚠️ **接着放**（`fullTester` 给的是暂停着的那一份）：解码器要真的吐出来
	// 几段才有频谱（它跑在自己的 goroutine 上，和 Tester 的钟没关系）。
	s.app.Toggle()
	s.app.SetFullPlayerPrefs(app.FullPlayerPrefs{ShowSpectrum: true})
	openFull(t, tt)

	// 解码器要真的吐出来几段才有频谱（它跑在自己的 goroutine 上，和 Tester 的
	// 钟没关系）。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if bars := s.app.SpectrumBars(48, 40); bars[0] > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if bars := s.app.SpectrumBars(48, 40); bars[0] == 0 {
		t.Fatalf("两秒了还是一排 0（画面上什么都画不出来）：%v", bars[:4])
	}

	// 只在**频谱自己那个框**里数：整个窗口里别处也有 #666 一族的主题色（按钮
	// 的边框、滑块的槽），扫全窗口的话这条断言量的是「界面上有没有灰色」，
	// 把频谱整个涂成别的颜色它照样绿。
	box, ok := tt.Find("频谱")
	if !ok {
		t.Fatalf("画面上找不到频谱那个区域，文字有 %q", tt.Texts())
	}
	const want = 500
	if n := pixelsNear(tt, box, mygoui.Color{R: 0x66, G: 0x66, B: 0x66, A: 255}, 4); n < want {
		t.Errorf("频谱那个 %vx%v 的框里只有 %d 个接近 #666 的像素，want ≥ %d —— 条子没画出来，或者画的不是那个颜色",
			box.W, box.H, n, want)
	}
	// 光靠上一条不够：把条子涂成白色，抗锯齿的边上照样会路过一圈灰，
	// 「框里有几个 #666」照样成立（实测：白色条子时那一条照样绿）。
	if n := pixelsNear(tt, box, mygoui.Color{R: 255, G: 255, B: 255, A: 255}, 4); n != 0 {
		t.Errorf("频谱那个框里有 %d 个纯白像素 —— 条子画的不是 #666", n)
	}
}

// ---- 帮手 ----

// pixelsNear 是 box 里接近 want 的像素个数（tol 是每一通道的余量）。
func pixelsNear(tt *mygoui.Tester, box mygoui.Rect, want mygoui.Color, tol int) int {
	img := tt.Image()
	n := 0
	for y := max(int(box.Y), img.Rect.Min.Y); y < min(int(box.Y+box.H), img.Rect.Max.Y); y++ {
		for x := max(int(box.X), img.Rect.Min.X); x < min(int(box.X+box.W), img.Rect.Max.X); x++ {
			p := img.RGBAAt(x, y)
			if absDiff(p.R, want.R) <= tol && absDiff(p.G, want.G) <= tol &&
				absDiff(p.B, want.B) <= tol {
				n++
			}
		}
	}
	return n
}

func absDiff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

// openFull 点开全屏（点播放条上那张封面）。
func openFull(t *testing.T, tt *mygoui.Tester) {
	t.Helper()
	row, ok := tt.Find("当前封面")
	if !ok {
		t.Fatalf("找不到封面，文字有 %q", tt.Texts())
	}
	tt.ClickAt(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()
	if !tt.HasText("关闭全屏") {
		t.Fatalf("全屏没开，文字有 %q", tt.Texts())
	}
}

// whitePixels 是画面上「接近白」的像素个数。
//
// 为什么是数白像素而不是找某一个元素的框：界面藏起来之后那些元素**还在**，
// 只是画不出来（`Opacity(0)`）。所以「元素还在」证明不了任何事，「一个亮像素都
// 没有」才算。
//
// 为什么白色只可能来自界面：这一组测试一律把主题设成深色（见 fullTester 的调用
// 处），于是主题自己的 Surface / Text 都是深的，满屏背景是 `Theme().Background`
// 再压一层 55% 的黑，封面占位是 `t.Surface`。画面上唯一亮的东西就是界面自己
// 画的字和控件。
func whitePixels(tt *mygoui.Tester) int {
	img := tt.Image()
	n := 0
	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
		for x := img.Rect.Min.X; x < img.Rect.Max.X; x++ {
			p := img.RGBAAt(x, y)
			if p.R > 190 && p.G > 190 && p.B > 190 {
				n++
			}
		}
	}
	return n
}

// 确认这一组用的是深色主题（whitePixels 的前提）。
func TestFullPlayerTestsRunOnTheDarkTheme(t *testing.T) {
	_, tt, _ := fullTester(t)
	openFull(t, tt)
	// 深色主题下背景不该是白的（那会让 whitePixels 数到一大片，藏没藏都数到）。
	if c := tt.Image().RGBAAt(5, 5); c.R > 190 && c.G > 190 && c.B > 190 {
		t.Errorf("深色主题下满屏背景是白的 %v，白像素计数就不可信了", color.RGBAModel.Convert(c))
	}
}
