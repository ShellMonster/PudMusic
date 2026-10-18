package ui

import (
	"strings"
	"testing"

	mygoui "github.com/egoist/mygo/ui"
)

// TestGuardKeepsPanicOutOfTheProcess 验的是阶段 1 最重要的一条约定：
// view 里 panic 不能杀进程。
//
// MyGo 的 Dispatch（app.go:576）和 loop.drain 都没有 recover，panic 会一路
// 穿到 Cocoa 事件循环上去，把整个进程带走。Biu 原来靠 React 的 ErrorBoundary
// 兜住，这里必须用 recover 兜同一个位置的效果。
func TestGuardKeepsPanicOutOfTheProcess(t *testing.T) {
	var got any
	calls := 0

	draw := Guard(func(c *mygoui.Context) {
		calls++
		if calls == 1 {
			panic("这一页画崩了")
		}
		mygoui.Text(c, "画出来了")
	}, func(r any) { got = r })

	// 第一帧：view panic。这行要是没接住，测试进程自己就没了。
	tt := mygoui.NewTester(draw, 200, 100)
	if got != "这一页画崩了" {
		t.Fatalf("recover 收到的是 %v，want 这一页画崩了", got)
	}
	// 崩掉的那一帧只建了一半（runFrame 的 pass 在 recover 处就断了，画的是
	// 半成品）。要下一帧才是完整的——这也正是 Shell.onPanic 里补一次
	// Invalidate 的原因。
	tt.Frame()
	if !tt.HasText("画出来了") {
		t.Errorf("panic 之后下一帧还是画不出来，文字有 %q", tt.Texts())
	}
}

// TestShellFallsBackToTheErrorPage 验错误页本身画得出来、也点得动。
func TestShellFallsBackToTheErrorPage(t *testing.T) {
	// ⚠️ `crash` 必须在**第一帧之前**设上，而 `mygo.NewTester` 造完就画了一帧——
	// 所以这件事得走 `stubbed`（它在造 Tester 之前跑），不能建完外壳再赋值。
	b := newTestShell(t, at(700, 400),
		stubbed(func(_ *testing.T, s *Shell) { s.crash = "坏掉了" }))
	s, tt := b.s, b.tt
	if !tt.HasText("界面出错了") {
		t.Fatalf("没有画错误页，文字有 %q", tt.Texts())
	}
	if !tt.HasText("坏掉了") {
		t.Errorf("错误页上没有崩掉的原因，文字有 %q", tt.Texts())
	}
	if _, ok := tt.Find("重新载入"); !ok {
		t.Errorf("错误页上没有「重新载入」，文字有 %q", tt.Texts())
	}

	if err := tt.Click("重新载入"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.crash != nil {
		t.Error("点了「重新载入」之后还停在错误页")
	}
	if !tt.HasText("还没有打开文件") {
		t.Errorf("重载之后没回到正常画面，文字有 %q", tt.Texts())
	}
}

// TestViewSurvivesAPanicInTheRealShell 用真实的 Shell 走一遍：让 viewMain
// panic 之后，Shell 自己要切到错误页，并且下一帧恢复正常画面。
func TestViewSurvivesAPanicInTheRealShell(t *testing.T) {
	s := newTestShell(t, at(700, 400)).s
	// 换成一支一画就炸的 view，走的还是 New 里那条 Guard + onPanic 的路。
	s.draw = Guard(func(c *mygoui.Context) { panic("画播放条的时候炸了") }, s.onPanic)

	tt := mygoui.NewTester(s.View, 700, 400)
	if s.crash == nil {
		t.Fatal("view 里 panic 了，外壳没有记住")
	}
	if !strings.Contains(errString(s.crash), "炸了") {
		t.Errorf("记住的 panic 是 %v", s.crash)
	}
	// 错误页是 s.View 在下一帧画的，这一帧还在建。
	tt.Frame()
	if !tt.HasText("界面出错了") {
		t.Errorf("没有切到错误页，文字有 %q", tt.Texts())
	}
}

func errString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
