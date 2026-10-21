package ui

import (
	"testing"
	"time"
)

// TestTooltipDelayIsMeasured 量框架 tooltip 的两档时延，把「对齐不了」这件事
// 钉在屏幕上，而不是只写在注释里。
//
// # 上游那三档是什么
//
// Biu 用的是 HeroUI 的 `Tooltip`，它有两个 props：
//
//	delay       指针停多久出提示，默认 0；上游只有音量那一处显式写了 300
//	             （`music-volume/index.tsx:119`）
//	closeDelay  指针离开后停多久收，默认 500；上游的歌单菜单 / 动态 / 更新 /
//	             新建收藏夹四处显式写了 0（`menu-item.tsx:78` 等）
//
// 于是上游实际用到的是 **0 / 300 / 500** 三档（报告 06 的 Tooltip 行）。
//
// # 框架这一侧量出来是两件事
//
//	出提示  固定 600ms（`mygo@v0.5.5/ui/tooltip.go:12` 的 tooltipDelay），
//	        没有按调用点改的口子
//	收提示  指针一离开、下一帧就没了（close delay 0）
//
// 也就是说：出提示比上游最慢的那一档（500）还慢 100ms，收提示只对得上
// 「closeDelay 0」那一批。**这一条没有对齐**——`Tooltip` / `TooltipBase` 的
// 公开 API 里没有 delay 参数，要改只能自己重画一个浮层，那是另一张票的事。
// 这里把实测记下来（票 #165 的 G44），下一轮要动时按这两个数改。
func TestTooltipDelayIsMeasured(t *testing.T) {
	_, tt := newShell(t)
	tt.SetFocused(true)
	tt.Frame()

	// 顶栏那个「打开文件」的提示是单独一句话，和按钮自己的名字不同，找得到。
	r, ok := tt.Find("打开文件")
	if !ok {
		t.Fatalf("顶栏上没有「打开文件」，文字有 %q", tt.Texts())
	}
	const tip = "打开本地音频文件"
	tt.Move(r.X+r.W/2, r.Y+r.H/2)

	// 599ms：还不到 600，提示不能出来。
	tt.Advance(599 * time.Millisecond)
	tt.Frame()
	if tt.HasText(tip) {
		t.Errorf("指针只停了 599ms 提示就出来了——框架的出提示时延已经不是 600ms，"+
			"票面记的那个数要重测，文字有 %q", tt.Texts())
	}

	// 再走 2ms（总共 601ms）：出了。
	tt.Advance(2 * time.Millisecond)
	tt.Frame()
	if !tt.HasText(tip) {
		t.Fatalf("指针停了 601ms 提示还没出来，文字有 %q", tt.Texts())
	}

	// 指针离开：框架没有 closeDelay，下一帧就该收掉。
	tt.Move(600, 400)
	tt.Frame()
	if tt.HasText(tip) {
		t.Errorf("指针离开之后提示还挂着——框架有了 closeDelay，上面记的「收提示 0」要重测")
	}
}
