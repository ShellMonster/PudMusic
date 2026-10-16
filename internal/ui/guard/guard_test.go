package guard

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestWrapKeepsAPanicOutOfTheProcess 是这个包存在的理由：
// MyGo 的事件循环没有 recover，view 里 panic 会一路穿上去把进程带走。
// 这一行要是没接住，测试进程自己就没了。
func TestWrapKeepsAPanicOutOfTheProcess(t *testing.T) {
	var got any
	calls := 0

	draw := Wrap(func(c *ui.Context) {
		calls++
		if calls == 1 {
			panic("画崩了")
		}
		ui.Text(c, "画出来了")
	}, func(r any) { got = r })

	// NewTester 自己就跑过一帧，那一帧里 view panic 了。
	tt := ui.NewTester(draw, 200, 100)
	if got != "画崩了" {
		t.Fatalf("recover 收到的是 %v，want 画崩了", got)
	}

	// 崩掉的那一帧只建了一半，下一帧才是完整的——所以 onPanic 里必须自己
	// 再要一帧（internal/ui 的 Shell.onPanic 就是这么做的）。
	tt.Frame()
	if !tt.HasText("画出来了") {
		t.Errorf("panic 之后下一帧还是画不出来，文字有 %q", tt.Texts())
	}
}

// TestWrapLeavesAPanickingFrameAloneIfTheCallerDoesNotAskForANextOne
// 说的是 recover 的另一半责任：**它只负责接住，不负责画面**。
// 要不要再要一帧是 onPanic 的事（Shell 要了，因为半张画面更糟）。
func TestWrapLeavesAPanickingFrameAloneIfTheCallerDoesNotAskForANextOne(t *testing.T) {
	calls := 0
	draw := Wrap(func(c *ui.Context) {
		calls++
		ui.Text(c, "画出来了")
	}, func(any) {}) // 不做任何事

	tt := ui.NewTester(draw, 200, 100)
	if calls != 1 {
		t.Errorf("view 被调了 %d 次，want 1：onPanic 没有要帧，Wrap 也不该多要", calls)
	}
	if !tt.HasText("画出来了") {
		t.Errorf("没 panic 的那一帧反而画不出来，文字有 %q", tt.Texts())
	}
}
