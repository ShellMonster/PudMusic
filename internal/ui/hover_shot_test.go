package ui

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/hover"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/egoist/mygo/ui"
)

// 这一族把**列表行 / 网格卡片的 hover 反馈和那一行表头**存成 PNG，留给人眼和 Biu 上游
// 的截图并排看。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderHoverShots
//
// # ⚠️ **深浅两套都要出**
//
// 调研明确说**深色下差距更明显**（上游那张能代表首页的图是深色的）。只出浅色等于没验
// 这一条——而这一族在浅色下和深色下是两个问题：浅色考验的是「压暗 35% 黑之后白色三
// 角还认不认得出」，深色考验的是「封面底部那条 black/90 渐变和主色播放钮在深底上还
// 分不分得开」。
//
// ⚠️ **一个字都不许打真 B 站接口**：全部是内存夹具 + 无头渲染，钥匙串里那份真登录态
// 一次都没被读（和 `flatbutton_test.go` 那一族同一个约定）。

// hoverRow 把指针移到 title 那一行的中间，并把淡入推满。
//
// ⚠️ **两件事一个都不能少**：
//
//  1. **推钟**——MyGo 的 Tester 的钟站着不动（`mygo/ui/headless.go`：「Its clock stands
//     still unless the test moves it with Advance」），不推的话那 200ms 淡入一步也不走，
//     而画面看起来是「淡入已经完成了」；
//  2. **补一帧**——`Advance` 只画它那一刻之前到点的那些帧（刻意的，免得在同一个时刻上
//     白画），所以推到的那一刻上画面还是旧的那一帧。
//
// ⚠️ hover 是**上一帧**的布局决定的（`rt.hover`），移过去之后要再画一帧它才换过来。
func hoverRow(t *testing.T, tt *ui.Tester, title string) {
	t.Helper()
	r, ok := tt.Find(title)
	if !ok {
		t.Fatalf("画面上找不到「%s」，文字有 %q", title, tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	tt.Advance(hover.Fade)
	tt.Frame()
}

// hoverCover 把指针停在那一格上，并把**悬停放大**推满。
//
// ⚠️ **推的是 300 毫秒**（`motion.HoverZoomTime`），不是淡入那 200 毫秒：封面
// 放大那一层是 300ms 的动画，不推满的话存出来的是「放大还没开始」的那一帧。
//
// ⚠️ **指针停在歌名那一行上就行**：那一圈反馈是**整格**的（`onCell` 是那一格
// 的 `Hovered()`），封面在那一格的上面，一样会放大。
func hoverCover(t *testing.T, tt *ui.Tester, title string) {
	t.Helper()
	r, ok := tt.Find(title)
	if !ok {
		t.Fatalf("画面上找不到「%s」，文字有 %q", title, tt.Texts())
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	tt.Advance(motion.HoverZoomTime)
	tt.Frame()
}

// shotLaterPage 存「稍后再看」那一页的两张：平时（看表头那五列）和 hover 一行。
//
// 走 `libraryShellOpts`（内存假 B 站 + **挂一份登录态**），一个真接口都没打。
// ⚠️ **不能像这一族别处那样直接 `newTestShell` + `MountLibrary`**：这一页
// `RequiresLogin`，不挂登录态画的是「先登录」那一支，一个请求都不发——
// 「稍后再看第一页」就永远等不到（这一条在 main 上红了很久，就是这里）。
func shotLaterPage(t *testing.T, dir, name string, dark bool) {
	t.Helper()
	f := newLibFake(t).withLater(1, 6, "甲曲", "乙曲", "丙曲", "丁曲", "戊曲", "己曲")
	tt, s := libraryShellOpts(t, f, laterPath, systemIsDark(dark))
	waitFrames(t, tt, "稍后再看第一页", func() bool {
		return rowsShown(&s.laterPage) == 6
	})

	tt.Move(1180, 770)
	homeFrame(tt)
	save(t, dir, "35-"+name+"-稍后再看-表头.png", tt)

	hoverRow(t, tt, "丙曲")
	save(t, dir, "36-"+name+"-稍后再看-行hover.png", tt)
}
