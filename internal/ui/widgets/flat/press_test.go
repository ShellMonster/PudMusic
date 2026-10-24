// 「按下时缩一点」那一档的测试。
//
// # 这个数是手册给的，不是量出来的
//
// 手册 ⓪：「按钮 active 如果要缩小，一般 **0.96** 甚至更大就够了；如果是放大，
// 一般 **1.04** 就够了。」——所以这两条用例钉的是**那两个数**，不是「缩了一点」。
package flat

import (
	"math"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/ui/iconset"
	"github.com/egoist/mygo/ui"
)

// pressIcon 是这几条用例画的那枚图标。
func pressIcon() *ui.SVG { return iconset.Must("Design/grid-line.svg") }

// TestThePressScaleShrinksTo096 钉住「按下时缩到 0.96」这个数。
//
// ⚠️ **中间那一帧要推进去**：动画是 120ms 的一段，而 Tester 的钟站着不动——
// 不推的话量到的是「还没开始缩」的那一帧（1.0），于是这一条量到的不是「缩到
// 0.96」而是「没缩」。
func TestThePressScaleShrinksTo096(t *testing.T) {
	var v float32
	pressed := false
	tt := ui.NewTester(func(c *ui.Context) {
		ic := ui.Icon(c, pressIcon()).FontSize(18)
		v = PressScaleValue(ic, pressed, false)
	}, 100, 100)
	tt.Frame()
	if v != 1 {
		t.Fatalf("没按着的时候是 %v，want 1（不缩）", v)
	}

	pressed = true
	// ⚠️ **动画是从「按着」被看见的那一帧才开始的**：框架的通道在**目标变了**的
	// 那一刻才开始走（`mygo/ui/motion.go`），而目标变的是这一帧——所以这一帧
	// 量到的还是 1（它刚要开始缩），下一帧才看得出在缩。
	tt.Advance(60 * time.Millisecond)
	tt.Frame()
	tt.Advance(60 * time.Millisecond)
	tt.Frame()
	if v >= 1 {
		t.Errorf("按下去 120ms 之后还是 %v，want < 1（已经在缩了）", v)
	}
	if v < PressShrink {
		t.Errorf("按下去 120ms 之后是 %v，want >= %v（不能缩过头）", v, PressShrink)
	}

	tt.Advance(120 * time.Millisecond)
	tt.Frame()
	if math.Abs(float64(v)-PressShrink) > 0.001 {
		t.Errorf("按到底之后是 %v，want %v", v, PressShrink)
	}
}

// TestThePressScaleGrowsTo104 钉住「放大那一档是 1.04」。
//
// 赞键按下去是**放大**（往外给一个赞，按下去让它涨一点），所以这一档也得有用。
func TestThePressScaleGrowsTo104(t *testing.T) {
	var v float32
	tt := ui.NewTester(func(c *ui.Context) {
		ic := ui.Icon(c, pressIcon()).FontSize(18)
		v = PressScaleValue(ic, true, true)
	}, 100, 100)
	tt.Frame()
	if math.Abs(float64(v)-PressGrow) > 0.001 {
		t.Errorf("按着的时候是 %v，want %v", v, PressGrow)
	}
}

// TestTheIconActuallyShrinksWhenPressed 验「缩放真的画出来了」。
//
// ⚠️ **量的是画出来的盒子**：`Tester.Find` 给回的是**变换后**的盒子
// （`mygo/ui/runtime.go` 的 `visualRect`），而 `Scale` 不改布局——所以这一条量
// 的是**画出来的大小**，不是布局盒。
//
// ⚠️ **图标要有个 label 才找得到**：`Find` 找的是「显示着这段文字、或者带着这个
// label」的元素，而图标对辅助技术是不可见的（`mygo/ui/svg.go`）——这里给它一个
// label 只是为了量它（真界面里没有这个 label）。
func TestTheIconActuallyShrinksWhenPressed(t *testing.T) {
	pressed := false
	tt := ui.NewTester(func(c *ui.Context) {
		ic := ui.Icon(c, pressIcon()).FontSize(18).Label("glyph")
		ic = PressScale(ic, pressed, false)
	}, 100, 100)
	tt.Frame()
	r0, ok := tt.Find("glyph")
	if !ok {
		t.Fatalf("图标没画出来")
	}

	pressed = true
	tt.Advance(200 * time.Millisecond)
	tt.Frame()
	r1, ok := tt.Find("glyph")
	if !ok {
		t.Fatalf("图标不见了")
	}
	want := r0.W * PressShrink
	if math.Abs(float64(r1.W-want)) > 1 {
		t.Errorf("按到底之后图标宽 %v，want %v（%v × %v）", r1.W, want, r0.W, PressShrink)
	}
}
