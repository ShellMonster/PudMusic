package ui

import (
	"testing"

	"github.com/egoist/mygo"
)

// 窗口骨架那几行。
//
// # 这一组量的是「红黄绿在不在」
//
// SPEC「真窗口只验三件事」的第一件就是它，而它的成因是**两个长得很像的开关**：
//
//	Frameless: true          真无边框——系统控件一起没了，窗口没有红黄绿
//	TitleBarStyle: Hidden    无边框、标题栏自绘、**控件留着**
//
// 框架自己把它们合成一条判断（`mygo/window.go:436`）：
//
//	hiddenTitleBar = !Frameless && (TitleBarStyle == Hidden || HiddenInset)
//
// 所以红黄绿在不在，取决于 `Frameless` 是不是 false——**而 `Frameless` 在
// 窗口骨架里只出现一次**，写错了编译得过、运行不报错、红黄绿就是不见了。
// 真机上发现的时候，界面看着「少点东西」，没人知道少了什么。
//
// 这一条量不到「按钮画在哪儿」（那要真窗口），量的是**开关**——够把那一类错
// 挡在编译之后、真机之前。真机上那一次（AX 树里 `close button` /
// `minimize button` / `full screen button` 三个都在）记在 BMG-43 报告里。
func TestTheWindowKeepsTheSystemControls(t *testing.T) {
	opts := WindowOptions(nil)

	if opts.Frameless {
		t.Error("Frameless 是真无边框：系统控件会被一起删掉，窗口上就没有红黄绿了。" +
			"Biu 用的是「保留控件的隐藏标题栏」，不是这个")
	}
	switch opts.TitleBarStyle {
	case mygo.TitleBarHidden:
		// 要的就是这个：Biu 的 `titleBarStyle: "hidden"`（electron/main.ts:53）。
	case mygo.TitleBarHiddenInset:
		t.Error("TitleBarHiddenInset 只是把控件再往里缩一点，Biu 没做这个差别")
	default:
		t.Errorf("TitleBarStyle = %q，want %q（隐藏标题栏、系统控件留着）",
			opts.TitleBarStyle, mygo.TitleBarHidden)
	}
	// 控件挪了位置也要真的给出去——nil 是「用框架默认的位置」，而默认位置
	// 是按标准标题栏算的，配上自绘的顶栏会明显偏高。
	if opts.TrafficLightPosition == nil {
		t.Error("没有给红黄绿的位置：会用框架按标准标题栏算的默认值，和自绘顶栏对不上")
	}
}

// TestTheWindowOpensAtBiuNumbers 量初始尺寸。
//
// 抄的是 Biu 的 `width: 1200, height: 800`（`electron/main.ts:41-42`）——
// 首页三个 Tab + 新歌速递那一屏的断点就是照这个宽度定的，换了宽度「看起来
// 也对」是最容易过的那种错，所以这里写 Biu 的字面量，不用 shell.go 里的数。
func TestTheWindowOpensAtBiuNumbers(t *testing.T) {
	const (
		biuWidth  = 1200 // electron/main.ts:41
		biuHeight = 800  // electron/main.ts:42
	)
	opts := WindowOptions(nil)
	if opts.Width != biuWidth || opts.Height != biuHeight {
		t.Errorf("窗口是 %d×%d，Biu 是 %d×%d", opts.Width, opts.Height, biuWidth, biuHeight)
	}
	if opts.Title != "BiuGo" {
		t.Errorf("窗口标题是 %q", opts.Title)
	}
	// 断点：布局里那几档是按「窗口至少这么宽」设计的。
	if opts.MinWidth > biuWidth || opts.MinHeight > biuHeight {
		t.Errorf("最小尺寸 %d×%d 比初始尺寸还大，这个窗口永远出不来",
			opts.MinWidth, opts.MinHeight)
	}
}
