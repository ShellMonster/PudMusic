// 满屏播放器里「点空白处切换界面显隐」这一组。
//
// # 需求是什么
//
// 满屏那一屏的顶栏（标题 + 设置齿轮）和底下那条播放控制条，点**空白处**能
// **切换**它们的显隐：点一下藏，再点一下露。
//
// # 为什么它得有一个**显式状态**
//
// 原来只有一条「三秒不动就藏」（Biu 的 `scheduleHideUi(3000)`），而那条规则是
// **单向**的：藏起来之后指针一动就全回来。用户要的是「藏了就一直藏着，直到
// 我再点一次」——那是**另一件事**，两者得各自记着，否则「手动藏的」和
// 「三秒藏的」分不开。
//
// # 判据为什么是**像素**而不是「那个布尔」
//
// 界面藏起来的时候那些元素**还在树上**（只是画不出来——`Opacity(0)` + 位移出
// 窗口），所以「元素还在」证明不了任何事。`whitePixels` 数的是「画面上接近白
// 的像素」：深色主题下画面上唯一亮的东西就是界面自己画的字和控件（歌词是
// 内容不是界面，所以这一组把歌词关掉）。
package ui

import (
	"testing"
	"time"

	mygoui "github.com/egoist/mygo/ui"
)

// fullToggleBlank 是「空白处」的一个坐标。
//
// ⚠️ **它必须在空白上**：1200×800 的窗口上，满屏那一屏里
//
//	封面     x≈192..552、y≈265..535（左边那一列，靠右沿）
//	顶栏     y < 64
//	底栏     y > 800-108 = 692
//	歌词     x > 600（右边那一列）
//
// 而 (900, 200) 在**右上角**：既不是封面（封面在左边），也不在顶栏那条里
// （顶栏高 64，200 在它下面），也不在歌词列里（歌词从 x=600 起，但那一列
// 没有控件——歌词是内容，而这一条要量的是**界面**）。
//
// ⚠️ **不能选 (900, 700)**：那儿是底栏（进度 + 控制键）。也不能选
// (600, 400)：那儿**就是封面**（封面的中心），封面为了长按三连调了
// `Pressed()`，它是接点击的那一个——点它落到长按那一套上，不落到 probe 上。
const (
	fullToggleBlankX = 900.0
	fullToggleBlankY = 200.0
)

// ---- 帮手 ----

// fullToggleTester 建一个开着满屏、**没有歌词**的界面。
//
// ⚠️ **歌词要关掉**：歌词是内容不是界面，界面藏起来之后歌词**还在**
// （票 #236，见 `TestTheLyricsStayWhenTheInterfaceHides`）——而这一组量的是
// 「界面上还有没有亮像素」，歌词的字会把量出来的东西搅浑。
func fullToggleTester(t *testing.T) (*Shell, *mygoui.Tester, *fakeClock) {
	t.Helper()
	s, tt, fc := fullTester(t)
	fullNoLyricsPrefs(t, s)
	openFull(t, tt)
	// 指针动一下：满屏开着的时候界面是亮的，而且那一下把「三秒不动」的钟
	// 重新排上了（`syncFullPointer`）。
	tt.Move(fullToggleBlankX, fullToggleBlankY)
	tt.Frame()
	return s, tt, fc
}

// fullToggleClickBlank 点一下空白处，然后**把淡出/淡入走完**。
//
// # ⚠️ **为什么要推钟**
//
// 「藏起来」不是**一瞬间**的事：顶栏和底栏走的是 `AnimatePose`（淡出 + 位移
// 合成一个 pose，`fullBarPose`），而 Tester 的钟**站着不动**——不推钟的话画面
// 上仍然有字（正在淡出的按钮当然还有像素），量出来是「没藏起来」。
//
// 而那 624 个亮像素**不是没藏**：状态已经藏了（`s.fullHidden()` 为真），是那段
// 淡出还没走完。于是这里推的是 **Tester 自己的钟**（跑动画的那个），
// `advanceFull` 的两个钟一起推是给「三秒那条规则」用的。
//
// ⚠️ **推的量要够走完那段动画**：`ui.Smooth` 是 500 毫秒的弹簧（框架的
// `defaultSpringDuration`），所以推一秒。
func fullToggleClickBlank(t *testing.T, s *Shell, tt *mygoui.Tester, fc *fakeClock) {
	t.Helper()
	tt.ClickAt(fullToggleBlankX, fullToggleBlankY)
	// ⚠️ **点完之后要 `tt.Frame()`**：框架把「这一帧建树时问出来的点击」交给下一帧
	// （`mygo/ui/runtime.go` 的 `endPass`：`clickLater`），而视图里的 `Clicked()`
	// 读到的是**这一帧的状态**。少画那一帧，`Clicked()` 问不到东西。
	tt.Frame()
	tt.Frame()
	// 把那段淡出/淡入走完。
	tt.Advance(time.Second)
	tt.Frame()
}

// fullToggleAdvance 把两个钟一起往前推（理由见 `advanceFull`）。
func fullToggleAdvance(s *Shell, tt *mygoui.Tester, fc *fakeClock, d time.Duration) {
	advanceFull(s, tt, fc, d)
}

// ---- 1. 点空白 → 藏起来 ----

// TestClickingBlankHidesTheFullPlayerInterface 钉住「点空白处界面藏起来」。
//
// 判据是**画面**：藏起来之后界面上一个亮像素都不剩（深色主题下歌词已关掉，
// 画面上唯一亮的就是界面自己画的字和控件）。
func TestClickingBlankHidesTheFullPlayerInterface(t *testing.T) {
	s, tt, fc := fullToggleTester(t)

	if n := whitePixels(tt); n == 0 {
		t.Fatal("界面上一个亮像素都没有，它压根没画出来过")
	}

	fullToggleClickBlank(t, s, tt, fc)

	if n := whitePixels(tt); n != 0 {
		t.Fatalf("点了空白之后界面上还有 %d 个亮像素 —— 没藏起来", n)
	}
	if !tt.HasText("关闭全屏") {
		// ⚠️ **这一条是「还在树上但看不见」，不是「被拆了」**：框架的淡出是
		// 不透明度，元素一直都在。所以这里**不能**拿「找不到那个文字」当判据
		// ——它找得到，只是画不出来。
		t.Log("顶栏那个键已经从树上不见了（淡出没有拆元素，这一层只是顺带记一笔）")
	}
}

// ---- 2. 再点空白 → 回来 ----

// TestClickingBlankAgainBringsTheInterfaceBack 钉住「这是**切换**不是「点一下就藏」。
//
// ⚠️ **这一条是需求的核心**：用户明确说了「切换」。第一版如果只做了「点一下
// 藏」，这条会红——而症状很隐蔽：用户点第二下，什么也没发生（界面还是藏着的），
// 于是「切换」变成了「单向」。
func TestClickingBlankAgainBringsTheInterfaceBack(t *testing.T) {
	s, tt, fc := fullToggleTester(t)

	// ① 点一下 → 藏。
	fullToggleClickBlank(t, s, tt, fc)
	if n := whitePixels(tt); n != 0 {
		t.Fatalf("第一次点空白之后界面上还有 %d 个亮像素 —— 没藏起来", n)
	}

	// ② 再点一下 → 回来。
	//
	// ⚠️ **要点在同一个地方**：探针是铺满整窗的，任何坐标都在它上面——但
	// 藏起来的时候**指针一动**会让界面按「三秒」那条规则回来（那是另一档
	// 状态，见 `fullHidden` 的注释）。而我们钉的是「手动切换」这一档：
	// 手动藏着的时候，指针动**不**把它叫回来（见下面那条测试）。
	fullToggleClickBlank(t, s, tt, fc)
	if n := whitePixels(tt); n == 0 {
		t.Fatal("第二次点空白之后界面还是藏着的 —— 这是「切换」，不是「点一下就藏」")
	}
}

// ---- 3. 三秒自动隐藏仍然工作 ----

// TestTheThreeSecondHideStillWorks 钉住「加了手动切换之后，三秒那条还在」。
//
// ⚠️ **测「三秒隐藏」要两个钟都推**：`s.clock` 判三秒，Tester 自己那个跑动画。
// 只推一个的话，要么三秒到不了、要么动画停在半路——而动画停在半路的时候
// 画面上仍然有字，看着像「没藏起来」（见 `advanceFull` 的注释）。
func TestTheThreeSecondHideStillWorks(t *testing.T) {
	s, tt, fc := fullToggleTester(t)

	fullToggleAdvance(s, tt, fc, biuHideSeconds*time.Second+time.Second)
	if !s.fullHidden() {
		t.Fatalf("三秒到了界面还露着（lastMove=%v now=%v）", s.full.lastMove, s.clock.Now())
	}
	if n := whitePixels(tt); n != 0 {
		t.Errorf("三秒之后界面上还有 %d 个亮像素", n)
	}
}

// ---- 4. 设置面板开着时不藏 ----

// TestTheSettingsPanelKeepsTheInterfaceUpAfterTheToggle 钉住
// Biu 的 `if (isSettingsOpen) return`（见 `fullHidden`）。
//
// ⚠️ **偏好面板开着的时候，点空白**也不该把界面藏起来——面板开着时藏界面，
// 用户一个字都还没改就成了「界面不见了」（Biu 那边也是这么处理的：
// `scheduleHideUi` 第一行就 `if (isSettingsOpen) return`）。
//
// ⚠️ **这一条不钉「面板有没有被关掉」**：框架那个浮层自己管「按外面关掉」
// （`PopoverBase` 的 `PressedOutside` 问「指针按在了不是这个元素的地方」——
// 按在满屏的空白上就是「按在外面」），于是**面板本来就会被那一按关掉**。
// 那是**既有行为**，和这一票无关（实测：这一票的实现动之前，点空白面板也关）。
// 那一按关掉面板之后，用户再点一次齿轮它就回来了——而 Biu 那边也一样
// （上游的 Popover 是 `isOpen={isSettingsOpen && isUiVisible}`）。
//
// ⇒ 所以这一条只钉**该钉的那一条**：界面**没有**被藏起来。
func TestTheSettingsPanelKeepsTheInterfaceUpAfterTheToggle(t *testing.T) {
	s, tt, fc := fullToggleTester(t)

	if err := tt.Click("全屏设置"); err != nil {
		t.Fatalf("点不了偏好面板：%v（文字：%q）", err, tt.Texts())
	}
	tt.Frame()
	if !tt.HasText("显示频谱图") {
		t.Fatalf("偏好面板没开，文字有 %q", tt.Texts())
	}

	// 点空白：界面**不该**藏（面板开着的时候没有「藏」这一说）。
	fullToggleClickBlank(t, s, tt, fc)
	if s.fullHidden() {
		t.Fatal("偏好面板开着，点空白却把界面藏起来了")
	}
	if s.full.uiHiddenByUser {
		t.Fatal("偏好面板开着，点空白却把「手动藏着」那一档翻过去了")
	}
	if n := whitePixels(tt); n == 0 {
		t.Fatal("界面被藏起来了（画面上一个亮像素都没有）")
	}
}

// ---- 5. 手动藏着的时候，指针动**不**把它叫回来 ----

// TestMovingThePointerDoesNotBringBackAManuallyHiddenInterface 钉住
// 「手动藏着就一直藏着，直到再点一次空白」。
//
// # 为什么是这一档，而不是「指针一动就回来」
//
// 三秒那条规则是**单向**的：Biu 那边 `onMouseMove` 每次都把界面叫回来，所以
// 「藏了就一直藏着」在那边根本做不到。而用户要的是**手动切换**——手动藏起来的
// 界面如果「指针一动就回来」，那用户刚藏好、鼠标还在动（手正要往别处去），
// 界面就自己冒出来了：用户什么也没点，而界面变了——那是**界面自己动了**，
// 不是用户要的。
//
// ⇒ 所以手动藏的那一档**不受指针影响**：它一直藏着，直到用户**再点一次空白**。
// 代价是：用户手动藏起来之后，得再点一下才知道界面在哪儿（那一下就是出口）。
//
// ⚠️ **这条和「三秒那条」不冲突**：三秒藏的那一档，指针一动**照旧**回来
// （`TestMovingThePointerBringsTheInterfaceBack` 钉着）。区别只在「是用户藏的
// 还是钟藏的」。
func TestMovingThePointerDoesNotBringBackAManuallyHiddenInterface(t *testing.T) {
	s, tt, fc := fullToggleTester(t)

	// 手动藏起来。
	fullToggleClickBlank(t, s, tt, fc)
	if !s.fullHidden() {
		t.Fatal("点空白之后界面没有藏起来")
	}

	// 指针动：手动藏的这一档**不**被叫回来。
	tt.Move(fullToggleBlankX+40, fullToggleBlankY+40)
	tt.Frame()
	if !s.fullHidden() {
		t.Fatal("指针一动，手动藏起来的界面自己回来了 —— 手动这一档不该受指针影响")
	}
	if n := whitePixels(tt); n != 0 {
		t.Errorf("指针一动之后界面上又有了 %d 个亮像素", n)
	}

	// 三秒到了也**不**改变它（手动藏着就一直藏着）。
	fullToggleAdvance(s, tt, fc, biuHideSeconds*time.Second+time.Second)
	if !s.fullHidden() {
		t.Fatal("手动藏着的界面被三秒那条规则改掉了")
	}

	// 再点一次空白 → 回来。
	fullToggleClickBlank(t, s, tt, fc)
	if s.fullHidden() {
		t.Fatal("再点一次空白，界面没有回来")
	}
	if n := whitePixels(tt); n == 0 {
		t.Fatal("界面回来了，可画面上一个亮像素都没有")
	}
}

// ---- 6. 藏起来之后那些按钮点不到 ----

// TestTheHiddenInterfaceTakesNoClicksAfterAManualToggle 钉住
// 「看不见 = 点不到」在**手动**这一档上也成立。
//
// ⚠️ 已有 `TestTheHiddenTopBarTakesNoClicks` 钉的是**三秒**那一档。手动藏
// 走的是同一套 `AnimatePose`（淡出 + 位移出窗口），所以这一条钉的是**手动**
// 这一档——两档的机制一样，但状态来源不同（一个是钟，一个是用户的点击），
// 而「藏起来时必须移出窗口」那条是**两档共用的**。
//
// 判据是按它**藏起来之前**那个坐标点下去：顶栏已经在窗口外面，那一下必须
// 什么也没按到。
func TestTheHiddenInterfaceTakesNoClicksAfterAManualToggle(t *testing.T) {
	s, tt, fc := fullToggleTester(t)

	closeBtn, ok := tt.Find("关闭全屏")
	if !ok {
		t.Fatalf("找不到「关闭全屏」，文字有 %q", tt.Texts())
	}
	gearBtn, ok := tt.Find("全屏设置")
	if !ok {
		t.Fatalf("找不到「全屏设置」，文字有 %q", tt.Texts())
	}

	// 手动藏起来。
	fullToggleClickBlank(t, s, tt, fc)
	if n := whitePixels(tt); n != 0 {
		t.Fatalf("界面还没藏干净（%d 个亮像素）", n)
	}

	// 按「关闭全屏」藏起来之前那个位置。
	tt.ClickAt(closeBtn.X+closeBtn.W/2, closeBtn.Y+closeBtn.H/2)
	tt.Frame()
	if !s.full.open {
		t.Fatal("界面藏着，「关闭全屏」那个位置一点还是把满屏关掉了")
	}
	if s.full.settings {
		t.Fatal("界面藏着，点一下就把偏好面板开出来了")
	}

	// 再藏一次，然后按「全屏设置」藏起来之前那个位置。
	fullToggleClickBlank(t, s, tt, fc)
	if n := whitePixels(tt); n != 0 {
		t.Fatalf("第二次没藏干净（%d 个亮像素）", n)
	}
	tt.ClickAt(gearBtn.X+gearBtn.W/2, gearBtn.Y+gearBtn.H/2)
	tt.Frame()
	if s.full.settings {
		t.Error("界面藏着，点右上角还是把偏好面板凭空开出来了")
	}
}

// ---- 7. 光标 ----

// TestTheCursorIsGoneWhenTheInterfaceIsHiddenByHand 钉住
// 「藏起来的时候光标也没了」。
//
// ⚠️ **这一条钉的是探针那一支**：框架认光标是**从指针底下最里层那个元素
// 开始往上找**，而遇到的**第一个「接点击的」元素就停**（认成默认箭头，
// `mygo/ui/runtime.go` 的 `updateCursor`）。加了 `Clicked()` 之后探针就成了
// 那个「接点击的」元素——于是光标会停在探针上，`CursorNone` 轮不到。
//
// 满屏上还有一处：封面那一层为了长按三连调了 `Pressed()`，它也是接点击的
// 那一枚（`fullplayer.go` 的 `viewFullCover`）。指针落在**封面**上时，
// 探针那一支的 `CursorNone` 根本轮不到——所以那一层也得自己说
// `CursorNone`（代码里已经是这样写的，这一条钉住它别被改回去）。
func TestTheCursorIsGoneWhenTheInterfaceIsHiddenByHand(t *testing.T) {
	s, tt, fc := fullToggleTester(t)

	// 指针在空白处：光标是默认箭头（界面露着的时候它不该是 none）。
	if c := tt.Cursor(); c == mygoui.CursorNone {
		t.Fatalf("界面露着，光标却是 none（%v）", c)
	}

	fullToggleClickBlank(t, s, tt, fc)

	if c := tt.Cursor(); c != mygoui.CursorNone {
		t.Errorf("界面藏起来了光标还在（%v）", c)
	}
}

// TestTheCursorIsGoneOnTheCoverTooWhenTheInterfaceIsHidden 钉住封面那一支的
// `CursorNone`。
//
// ⚠️ **上一条钉的是探针那一支**，而这一条钉的是**封面**那一支——两条都得有，
// 因为它们互不相干：
//
// 框架认光标是**从指针底下最里层那个元素开始往上找**，而遇到的**第一个「接
// 点击的」元素就停**（认成默认箭头，`mygo/ui/runtime.go` 的 `updateCursor`）。
//
//	指针在空白处   探针是那个「接点击的」→ 探针的 `CursorNone` 生效
//	指针在封面上   封面是那个「接点击的」（它为了长按三连调了 `Pressed()`）
//	              → **探针那一支根本轮不到**，封面自己得说 `CursorNone`
//
// ⚠️ 封面那个是**代码里本来就写着**的（`viewFullCover` 里 `if hidden { box =
// box.Cursor(ui.CursorNone) }`）——这一条钉住它别被改回去，也钉住「加了
// `probe.Clicked()` 之后它还生效」。
func TestTheCursorIsGoneOnTheCoverTooWhenTheInterfaceIsHidden(t *testing.T) {
	s, tt, fc := fullToggleTester(t)

	// 指针挪到封面正中——满屏开着的时候封面在整窗正中（歌词关着，
	// `viewFullStage` 走的是 `fullWindowRect` 那一支）。
	tt.Move(600, 400)
	tt.Frame()
	if c := tt.Cursor(); c == mygoui.CursorNone {
		t.Fatalf("界面露着，指针在封面上，光标却是 none（%v）", c)
	}

	// 手动藏起来（藏的是**界面**，封面是**内容**——它还在）。
	fullToggleClickBlank(t, s, tt, fc)

	if c := tt.Cursor(); c != mygoui.CursorNone {
		t.Errorf("界面藏起来了，指针在封面上，光标还在（%v）", c)
	}
}

// ---- 8. 空状态 ----

// TestTheEmptyFullPlayerKeepsItsCloseButton 钉住「满屏开着、此刻没有歌」
// 那一屏**不藏界面**。
//
// ⚠️ **为什么这一条要钉**：空状态那一屏上除了顶栏那个「关闭全屏」就只剩一句
// 「暂无播放内容」——**没有可藏的东西**（没有底栏、没有频谱、没有中间那一块）。
// 而藏掉那个关闭键 = 把用户关在一屏里出不来：那一屏连「按 Esc 退回去」那句
// 提示都没画（`viewFullEmpty` 只画了标题）。
//
// ⇒ 所以手动藏的那一档**不往空状态漏**：用户手动藏过一次界面，之后换歌的那
// 几百毫秒（`HasTrack` 恒为 false，正好走空状态）里那个关闭键得**还在**。
//
// ⚠️ **换歌那几百毫秒会走空状态**，而那不是「用户想看空状态」——所以这不能
// 靠「用户主动停歌」来保证。
func TestTheEmptyFullPlayerKeepsItsCloseButton(t *testing.T) {
	s, tt, fc := fullToggleTester(t)

	// 手动把界面藏起来。
	fullToggleClickBlank(t, s, tt, fc)
	if !s.full.uiHiddenByUser {
		t.Fatal("点空白之后界面没有藏起来")
	}

	// 歌没了 → 走空状态那一支（`viewFullEmpty`）。
	s.app.ClearTrack()
	tt.Frame()
	tt.Frame()

	// 关闭键得**还在**（不是画不出来，是**还在**）。
	if !tt.HasText("关闭全屏") {
		t.Fatalf("空状态那一屏把「关闭全屏」藏起来了，文字有 %q", tt.Texts())
	}
	// 空状态**不藏界面**——所以 `fullHidden` 在那一支是假。
	if s.fullHiddenWith(false) {
		t.Error("空状态那一屏把界面藏起来了——它上面只剩一个关闭键，藏了用户就出不来了")
	}
	// 而**手动那一档还记着**：歌回来之后界面仍然是藏着的（用户没改主意）。
	if !s.full.uiHiddenByUser {
		t.Error("空状态那一支把「手动藏着」那一档冲掉了")
	}
}
