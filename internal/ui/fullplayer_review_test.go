// 第一轮评审提的三条界面毛病，各配一条「把它改回去就红」的断言。
//
// 三条的共同形状：**画面上看得见、界面上说不清**的那一类。
//
//  1. 悬停浮层盖住整块左半边播放条（`Absolute` 是相对父容器的）
//  2. 满屏顶栏压在 macOS 的红黄绿下面，而且整条不能拖窗口
//  3. 界面藏起来之后顶栏那两个看不见的按钮仍然接点击
package ui

import (
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// ---- 1. 悬停浮层的范围 ----

// TestTheHoverLayerCoversOnlyTheCover 钉住悬停浮层只盖那张 56×56 的封面。
//
// `Absolute()` 是相对**父容器**摆的，而这一行的父容器是「封面 + 歌名」那个
// `Grow(1)` 的 Row。第一版没有外面那一层，于是浮层铺的是整块左半边播放条：
// 实测那块黑从 x≈8 铺到 x≈1048，歌名被压在底下，⤢ 落在 x≈540 而不是落在封面上。
//
// 判据用**两帧的差**：不悬停那一帧和悬停那一帧之间变了颜色的像素，必须全部落在
// 封面的框里。这条不认颜色——蒙层是半透明黑，叠在什么上面出来的颜色随主题变。
func TestTheHoverLayerCoversOnlyTheCover(t *testing.T) {
	s, tt, _ := fullTester(t)

	// ⚠️ **先把「在不在播」定下来**：这一条比的是**两帧之间变了哪些像素**，而满屏
	// 底下那个播放键画的是「播放 / 暂停」两个字——**歌刚开始播的那一瞬它换一张图**，
	// 而那一换在封面**外面**，于是这一条报的是「浮层盖过了封面」，症状离真因隔了
	// 三层（真因是那两个字换了）。
	//
	// 做法是**把歌按停**：按停之后那个键不再换，歌也不会放到头（放到头它又换回去）。
	// 这一条要验的是「悬停浮层只盖住封面」，播不播跟它没关系 ✓。
	waitFor(t, "歌开始播了", func() bool { return s.app.Snapshot().Playing })
	tt.Frame()
	s.app.Toggle() // 暂停：那个键从此不动
	tt.Frame()

	cover, ok := tt.Find("当前封面")
	if !ok {
		t.Fatalf("找不到封面，文字有 %q", tt.Texts())
	}
	idle := snapshot(tt)

	tt.Move(cover.X+cover.W/2, cover.Y+cover.H/2)
	tt.Frame()
	hovered := snapshot(tt)

	changed := changedPixels(idle, hovered)
	if len(changed) == 0 {
		t.Fatal("指针悬停在封面上，画面一个像素都没变 —— 悬停反馈压根没有")
	}
	// 封面框往外放一像素（抗锯齿会在边上渗出去一像素）。
	inside := image.Rect(
		int(cover.X)-1, int(cover.Y)-1,
		int(cover.X+cover.W)+1, int(cover.Y+cover.H)+1,
	)
	for _, p := range changed {
		if p.X < inside.Min.X || p.X >= inside.Max.X ||
			p.Y < inside.Min.Y || p.Y >= inside.Max.Y {
			t.Fatalf("悬停时 %v 变了，可它在封面 %v 外面 —— 浮层盖过了封面",
				p, cover)
		}
	}
}

// TestTheTitleNextToTheCoverIsNotBlackedOut 是上一条的另一半：量歌名那一侧。
//
// 「浮层只盖封面」和「歌名没被压黑」是同一件事的两面，但分开说更清楚症状：
// 用户看到的是歌名那里也黑了一块。
func TestTheTitleNextToTheCoverIsNotBlackedOut(t *testing.T) {
	_, tt, _ := fullTester(t)

	cover, ok := tt.Find("当前封面")
	if !ok {
		t.Fatalf("找不到封面，文字有 %q", tt.Texts())
	}
	idle := snapshot(tt)

	tt.Move(cover.X+cover.W/2, cover.Y+cover.H/2)
	tt.Frame()
	hovered := snapshot(tt)

	// 封面右边那一列（从封面右沿到歌名那一段的中点）不许有任何一个像素变。
	right := cover.X + cover.W
	limit := right + coverGap + 40
	for y := int(cover.Y); y < int(cover.Y+cover.H); y++ {
		for x := int(right + 1); x < int(limit) && x < hovered.Bounds().Dx(); x++ {
			if idle.RGBAAt(x, y) != hovered.RGBAAt(x, y) {
				t.Fatalf("歌名那一侧 (%d,%d) 悬停时变了色 —— 浮层盖过去了", x, y)
			}
		}
	}
}

// ---- 2. 顶栏让开红黄绿 + 能拖窗口 ----

// TestTheFullTopBarPadFollowsTheTitleBar 钉住左边距**跟着框架给的那个数走**。
//
// 隐藏标题栏的窗口里，红黄绿是叠在内容**上面**的（`mygo/ui/context.go:349-358`）。
// 而满屏把普通顶栏整条盖住了——于是「关闭全屏」那一枚（当时是一枚文字键）正好被黄灯和绿灯压住
// （实测按钮框 x∈[39,91]，红黄绿整簇 x∈[14,66]；现在是 32×32 的图标键，框更窄，
// 但它贴着左边距的时候照样会落进那一簇）。
//
// 做法照 `navbar.go` 那一处：`c.TitleBar()` 的 `bar.Left` 就是左边要让出的量。
//
// ⚠️ **Tester 默认没有标题栏**（`bar.Left` 是 0），而这一条整个要验的就是
// 「有标题栏的时候让开了」——所以这里显式装一个 macOS 那样的进去
// （`tt.SetTitleBar`）。光量函数不算数：把 view 里那一行改回写死 `fullTopPadX`，
// 函数本身照样对，但 view 早就不听它的了（实测：那样改了这一条还是绿的）。
func TestTheFullTopBarPadFollowsTheTitleBar(t *testing.T) {
	_, tt, _ := fullTester(t)
	openFull(t, tt)

	// 无头环境（没有标题栏）：按钮贴着左边距。
	if btn, ok := tt.Find("关闭全屏"); !ok {
		t.Fatalf("找不到「关闭全屏」，文字有 %q", tt.Texts())
	} else if btn.X < fullTopPadX {
		t.Errorf("没有标题栏时按钮在 x=%v，want ≥ %v", btn.X, fullTopPadX)
	}

	// 装一个 macOS 那样的标题栏进去：左边整簇红黄绿让出 46。
	const barLeft, barHeight = float32(46), float32(38)
	tt.SetTitleBar(ui.TitleBar{Height: barHeight, Left: barLeft})
	tt.Frame()

	btn, ok := tt.Find("关闭全屏")
	if !ok {
		t.Fatalf("装上标题栏之后找不到「关闭全屏」，文字有 %q", tt.Texts())
	}
	if want := barLeft + fullTopPadX; btn.X < want {
		t.Errorf("「关闭全屏」在 x=%v，want ≥ %v —— 红黄绿整簇占 x∈[0,%v]，被压住了",
			btn.X, want, barLeft)
	}
	// 顶栏本身也要够高，红黄绿那一行才不是浮在半空里。
	if want := fullTopBarPadLeft(ui.TitleBar{Left: barLeft}); want != barLeft+fullTopPadX {
		t.Fatalf("fullTopBarPadLeft 算出了 %v，want %v", want, barLeft+fullTopPadX)
	}
}

// TestTheFullTopBarDragsWithoutStealingTheButtons 钉住拖动区和按钮的关系。
//
// 满屏浮层盖住整窗，普通顶栏上那两处 `DragWindow` 够不着了，所以这一条要自己
// 带一个。而 `DragWindow` 只有在**元素自己不接收别的东西**时才生效
// （`mygo/ui/input.go:361`：`target.flags&(interactive&^flagDragWindow) == 0`），
// 所以要盯的是「加上它之后按钮照样点得动」——挂错元素的表现正是按钮按不动。
//
// ⚠️ **「窗口真的被拖动」在无头环境里量不到**：`mygo/ui/headless.go` 的
// `startDrag` 和 `titleBarDoubleClicked` 都是空实现，Tester 也不记窗口移动。
// 拿掉 DragWindow 这一条**照样绿**——它钉的是「拖动区不吃点击」，拖动本身要
// 真窗口才验得到（见报告）。
func TestTheFullTopBarDragsWithoutStealingTheButtons(t *testing.T) {
	s, tt, _ := fullTester(t)
	openFull(t, tt)

	bar, ok := tt.Find("关闭全屏")
	if !ok {
		t.Fatalf("找不到「关闭全屏」，文字有 %q", tt.Texts())
	}
	// 拖动区在按钮右边那一大块空处。按下拖一段再松手：不该按到任何东西，
	// 满屏也不该动。
	tt.Press(bar.X+bar.W+120, bar.Y+bar.H/2)
	for i := 1; i <= 4; i++ {
		tt.Move(bar.X+bar.W+120+float32(i)*10, bar.Y+bar.H/2)
	}
	tt.Release(bar.X+bar.W+160, bar.Y+bar.H/2)
	tt.Frame()

	if !s.full.open {
		t.Fatal("在拖动区上按了一下就把满屏关掉了")
	}
	// 按钮自己照旧点得动（这是「拖动区不吃点击」的那一半）。
	if err := tt.Click("关闭全屏"); err != nil {
		t.Fatalf("点不了「关闭全屏」：%v（文字：%q）", err, tt.Texts())
	}
	tt.Frame()
	if s.full.open {
		t.Error("顶栏带 DragWindow 之后按钮点不动了")
	}
}

// ---- 3. 藏起来之后不许再接点击 ----

// TestTheHiddenTopBarTakesNoClicks 钉住「看不见 = 点不到」。
//
// 只做 Opacity 的话那两个按钮仍然在窗口里、仍然接点击：停在窗口顶部点一下满屏
// 就被关掉，点右上角会把偏好面板凭空开出来。底下那一条 controls 一直用
// `Translate` 把它推出窗口，顶栏原来没做。
//
// 判据是按它**藏起来之前**那个坐标点下去。
func TestTheHiddenTopBarTakesNoClicks(t *testing.T) {
	s, tt, fc := fullTester(t)
	openFull(t, tt)
	tt.Move(600, 400)
	tt.Frame()

	closeBtn, ok := tt.Find("关闭全屏")
	if !ok {
		t.Fatalf("找不到「关闭全屏」，文字有 %q", tt.Texts())
	}
	gearBtn, ok := tt.Find("全屏设置")
	if !ok {
		t.Fatalf("找不到「全屏设置」，文字有 %q", tt.Texts())
	}

	// 三秒到点 + 淡出走完。
	advanceFull(s, tt, fc, biuHideSeconds*time.Second+time.Second)
	if n := whitePixels(tt); n != 0 {
		t.Fatalf("界面还没藏干净（%d 个亮像素），这一条量不到「看不见还在接点击」", n)
	}

	// 按「关闭全屏」藏起来之前那个位置。
	//
	// ⚠️ 这一次点击**本身会把指针挪过去**，于是界面合法地又露出来（Biu 的
	// `onMouseMove` 就是这么把界面叫回来的）。所以量的是**点击那一刻**：顶栏还
	// 在窗口外面，那一下必须什么也没按到。
	tt.ClickAt(closeBtn.X+closeBtn.W/2, closeBtn.Y+closeBtn.H/2)
	tt.Frame()
	if !s.full.open {
		t.Fatal("界面藏着，「关闭全屏」那个位置一点还是把满屏关掉了")
	}
	if s.full.settings || tt.HasText("显示频谱图") {
		t.Fatal("界面藏着，点一下就把偏好面板开出来了")
	}

	// 再藏一次，然后按「全屏设置」藏起来之前那个位置。
	advanceFull(s, tt, fc, biuHideSeconds*time.Second+time.Second)
	if n := whitePixels(tt); n != 0 {
		t.Fatalf("第二次没藏干净（%d 个亮像素）", n)
	}
	tt.ClickAt(gearBtn.X+gearBtn.W/2, gearBtn.Y+gearBtn.H/2)
	tt.Frame()
	if s.full.settings || tt.HasText("显示频谱图") {
		t.Error("界面藏着，点右上角还是把偏好面板凭空开出来了")
	}
}

// ---- 帮手 ----

// snapshot 是这一帧画面的一份拷贝。
//
// `tt.Image()` 给的是宿主内部那张图自己，直接留到下一帧会被覆盖。
func snapshot(tt *ui.Tester) *image.RGBA {
	src := tt.Image()
	cp := image.NewRGBA(src.Rect)
	copy(cp.Pix, src.Pix)
	return cp
}

// changedPixels 是两帧之间颜色变了的那些像素。
func changedPixels(a, b *image.RGBA) []image.Point {
	var out []image.Point
	for y := a.Rect.Min.Y; y < a.Rect.Max.Y; y++ {
		for x := a.Rect.Min.X; x < a.Rect.Max.X; x++ {
			if color.RGBAModel.Convert(a.RGBAAt(x, y)) != color.RGBAModel.Convert(b.RGBAAt(x, y)) {
				out = append(out, image.Pt(x, y))
			}
		}
	}
	return out
}

// ---- 4. 悬停之后点封面正中要能进去 ----

// TestClickingTheCentreOfTheHoveredCoverOpensTheFullPlayer 钉住**最自然那个手势**。
//
// 「把鼠标移过去、点一下」进全屏——而悬停浮层那个 ⤢ 正好就落在封面正中。
//
// 机制（`mygo/ui/input.go:140-154` 的 `appendHitChain`）：命中链是「最上面那个
// 含点的框 + 它**自己的父链**」，**兄弟不在链上**。而 `flagPassThrough` 只跳过
// 那一个自己的命中框、子节点照登记（`runtime.go:1046`）——于是浮层那个 Row 虽然
// PassThrough 了，它里面的 `⤢` 文字照样登记，而可点的 `imgcache.View` 是浮层的
// **兄弟**，永远不在链上。表现是：点封面**边角**能开（那儿没有文字），点**正中**
// 什么也不会发生。
//
// 三种点法都验：悬停后点正中（这次修的）、悬停后点边角（本来就对，别弄坏）、
// 不悬停直接点正中（本来也对）。
func TestClickingTheCentreOfTheHoveredCoverOpensTheFullPlayer(t *testing.T) {
	s, tt, _ := fullTester(t)

	cover, ok := tt.Find("当前封面")
	if !ok {
		t.Fatalf("找不到封面，文字有 %q", tt.Texts())
	}
	mid := [2]float32{cover.X + cover.W/2, cover.Y + cover.H/2}

	// ① 悬停之后点正中 —— ⤢ 就落在这个点上。
	tt.Move(mid[0], mid[1])
	tt.Frame()
	tt.ClickAt(mid[0], mid[1])
	tt.Frame()
	if !s.full.open {
		t.Errorf("悬停之后点封面正中没能进全屏 —— 浮层里那个 ⤢ 把点击吃掉了")
	}

	// ② 悬停之后点边角（那一带没有文字）。
	s.closeFullPlayer()
	tt.Frame()
	tt.Move(mid[0], mid[1])
	tt.Frame()
	tt.ClickAt(cover.X+3, cover.Y+3)
	tt.Frame()
	if !s.full.open {
		t.Errorf("悬停之后点封面边角没能进全屏")
	}

	// ③ 不悬停直接点正中（压根没有浮层）。
	s.closeFullPlayer()
	tt.Frame()
	tt.Move(cover.X+cover.W-3, cover.Y-20) // 挪到窗口上面去，别悬停在封面上
	tt.Frame()
	tt.ClickAt(mid[0], mid[1])
	tt.Frame()
	if !s.full.open {
		t.Errorf("不悬停直接点封面正中没能进全屏")
	}
}
