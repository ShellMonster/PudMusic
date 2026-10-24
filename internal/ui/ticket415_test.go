package ui

// 票 #415 的判据：迷你窗的注释还按 320×100 旧版面写（①），以及换悬停目标时
// 两块提示浮层叠在一起（②）。
//
// # ② 现在的机制
//
// 提示是**逐个元素** build 的：每个带提示的元素在自己的 build 里登记一份，
// **这一遍 build 走完之后**才挑出该画的那一枚（`tooltip.go` 的 `pickTip` /
// `drawTip`）。三档：指针**正停着**的那一枚（它挤掉别的）、指针已经移开还在
// 收尾期里的最近那一枚、焦点那一枚。
//
// ⚠️ **这一套是票 #420（PR #425）带来的**，而 #415 的那条评审意见（PR #386 的
// F6）是在它**之前**提的——那一轮实测时两块提示确实叠着（出图
// `mini-mute-tooltip-overlap.png`：「已静音（音量 20%）」压着「取消静音」）。
// 现在迷你窗里换悬停目标只留一块，所以这一组量的是**它没退回去**。

import (
	"image/color"
	"testing"

	mygoui "github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// ②　迷你窗里换悬停目标时只留一块提示
// ─────────────────────────────────────────────────────────────────────────────

// TestTheMiniShowsOneTooltipAtATime 钉住「迷你窗里指针从一枚挪到另一枚，画面上
// 只留一块提示」。
//
// # 它挡的是什么
//
// 票 #345（PR #386）加的那枚**静音键紧挨着音量滑块**——提示从滑块挪到静音键
// 时两块黑底叠在一起，「取消静音」被压在下面读不出来（那一轮的实现里，两枚
// 悬停提示各自 `shown`，谁也不让谁）。
//
// # 判据是**上一块不再留在画面上**
//
// ⚠️ **数的是「取消静音」出现几次**：静音键的**可及名字**也是这一句，所以提示
// 出来的时候它是**两次**（名字一次、提示一次）——只出现一次就是提示没出来，
// 出现三次就是有两块提示。
//
// ⚠️ **音量那一枚的提示是独一份的字**（「已静音（音量 20%）」）：所以「它还在
// 屏幕上」这件事数得清。
func TestTheMiniShowsOneTooltipAtATime(t *testing.T) {
	s, a := newMiniShell(t, nil)
	// 静音 + 音量 20%：于是音量滑块那枚的提示是「已静音（音量 20%）」，
	// 静音键那枚是「取消静音」——两句都不与别的控件的名字重名。
	a.SetVolume(0.2)
	a.ToggleMute()
	tt := miniTester(s)
	settleMini(t, tt, "静音读回来了", func() bool { return a.Snapshot().Muted })

	const (
		volTip  = "已静音（音量 20%）"
		muteTip = "取消静音"
	)

	// 指针停在音量滑块上：它那一枚的提示出来。
	bar, ok := tt.Find("音量")
	if !ok {
		t.Fatalf("画面上找不到音量条，文字有 %q", tt.Texts())
	}
	tt.Move(bar.X+bar.W/2, bar.Y+bar.H/2)
	tt.Frame()
	if !tt.HasText(volTip) {
		t.Fatalf("指针停到音量滑块上，%q 那枚提示没出来（文字有 %q）", volTip, tt.Texts())
	}

	// 挪到静音键上。
	mute, ok := tt.Find(muteTip)
	if !ok {
		t.Fatalf("画面上找不到静音键，文字有 %q", tt.Texts())
	}
	tt.Move(mute.X+mute.W/2, mute.Y+mute.H/2)
	tt.Frame()

	// ⚠️ **上一块要立刻走**：它还在那 500ms 的收尾期里，但那 500ms 是「没有新的
	// 提示来时」的语义——新的来了就不该等（`tooltip.go` 的 `pickTip` 第一档）。
	if tt.HasText(volTip) {
		t.Errorf("上一枚的提示还挂在屏幕上（文字有 %q）——"+
			"指针落到另一枚上时，上一枚该立刻走", tt.Texts())
	}
	if n := countExactText(tt, muteTip); n != 2 {
		t.Errorf("指针停到静音键上，%q 出现了 %d 次（文字有 %q）——"+
			"该是两次（键的名字一次、提示一次）", muteTip, n, tt.Texts())
	}

	// 再挪回滑块上：这一对反着来一遍（同一套机制的两个方向都断）。
	//
	// ⚠️ **数的是出现次数，不是「在不在」**：静音键的**可及名字**本来就写着
	// 「取消静音」，它一直都在画面上——提示没了之后这一句仍然出现**一次**。
	tt.Move(bar.X+bar.W/2, bar.Y+bar.H/2)
	tt.Frame()
	if n := countExactText(tt, muteTip); n != 1 {
		t.Errorf("挪回音量滑块上，%q 出现了 %d 次（文字有 %q）——"+
			"该只剩一次（键的名字），提示那一次该收掉", muteTip, n, tt.Texts())
	}
	if !tt.HasText(volTip) {
		t.Errorf("挪回音量滑块上，它那一枚的提示没出来（文字有 %q）", tt.Texts())
	}
}

// TestTheMiniTooltipStaysInsideTheWindow 钉住「迷你窗那一枚提示**在窗内**」。
//
// ⚠️ **这一条不是「有没有提示」，是「看不看得见」**：迷你窗只有 320×84，而
// 音量滑块和静音键都贴着下沿（y=50..78）。提示摆在锚点下面的话整个落在窗口
// 之外——**一个字都看不见**，而「看不见的提示」和「没有提示」在画面上是一回事。
//
// 框架会把摆不下的提示**翻到锚点的另一边**（`mygo/ui/layout.go` 的
// `alongTarget`），翻上去之后落在锚点的上沿——**还在窗内**。
//
// ⚠️ **判据量的是画面上那块提示底**：提示是**反色底**（`tooltip.go` 的
// `newTipPanel`：底取 `t.Inverse`）。浅色底下反色就是字色，于是窗里别处也有
// 同样的像素（图标、字）——但它们**一行连不成 40 像素那么宽的一段**，而提示底
// 是一整块实色（那一句话的宽）。
func TestTheMiniTooltipStaysInsideTheWindow(t *testing.T) {
	s, a := newMiniShell(t, nil)
	a.SetVolume(0.2)
	a.ToggleMute()
	tt := miniTester(s)
	settleMini(t, tt, "静音读回来了", func() bool { return a.Snapshot().Muted })

	mute, ok := tt.Find("取消静音")
	if !ok {
		t.Fatalf("画面上找不到静音键，文字有 %q", tt.Texts())
	}
	tt.Move(mute.X+mute.W/2, mute.Y+mute.H/2)
	tt.Frame()

	// ⚠️ **主题要跟画面上那一套是同一个**：提示底取的是 `t.Inverse`，而它是
	// 跟着主题走的（浅色底 = 字色，深色底 = 卡面色）。
	theme := BuildTheme(s.app.SettingsSnapshot(), false)
	inverse := color.RGBA{R: theme.Inverse.R, G: theme.Inverse.G,
		B: theme.Inverse.B, A: theme.Inverse.A}

	box, ok := miniTipBox(tt, inverse)
	if !ok {
		t.Fatalf("画面上找不到那一块提示底（文字有 %q）", tt.Texts())
	}
	if box.X < 0 || box.Y < 0 || box.X+box.W > miniWidth || box.Y+box.H > miniHeight {
		t.Errorf("那一枚提示画在 %v，而迷你窗只有 %d×%d——它落在窗外，用户看不见",
			box, miniWidth, miniHeight)
	}
}

// miniTipBox 量出迷你窗上那一块提示底的外框（找的是那个颜色**一行连成 ≥ 40
// 像素**的地方）。
//
// ⚠️ **为什么按「连续宽度」筛**：浅色底下提示底和字色同色，于是窗里到处是那个
// 颜色——图标、字、播放键那个实心圆都是。但**一行连成 40 像素宽的一段**的只有
// 提示底（那句话的宽），别的都细（播放键那个圆是 24 宽）。
//
// ⚠️ **判据是外框，不是每一块**：提示底是圆角的，第一行和最后一行短一些——
// 所以量的是「有一行宽到 ≥ 40 的那些行」的并集。
func miniTipBox(tt *mygoui.Tester, fill color.RGBA) (mygoui.Rect, bool) {
	img := tt.Image()
	maxX, maxY := img.Rect.Max.X, img.Rect.Max.Y
	minX, minY, farX, farY := -1, -1, -1, -1
	for y := range maxY {
		run := -1
		for x := 0; x <= maxX; x++ {
			same := x < maxX && img.RGBAAt(x, y) == fill
			if same {
				if run < 0 {
					run = x
				}
				continue
			}
			if run >= 0 && x-run >= 40 {
				if minX < 0 || run < minX {
					minX = run
				}
				if farX < 0 || x-1 > farX {
					farX = x - 1
				}
				if minY < 0 || y < minY {
					minY = y
				}
				if farY < 0 || y > farY {
					farY = y
				}
			}
			run = -1
		}
	}
	if minX < 0 {
		return mygoui.Rect{}, false
	}
	return mygoui.Rect{
		X: float32(minX), Y: float32(minY),
		W: float32(farX - minX + 1), H: float32(farY - minY + 1),
	}, true
}
