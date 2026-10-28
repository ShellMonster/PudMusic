package ui

import (
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
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

// TestRenderHoverShots 把 hover 反馈和表头存成 PNG，深浅各一套。
func TestRenderHoverShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	// 指针停在窗口右下那片空地上：不摆 hover 的那几张要的是「平时」。
	away := [2]float32{1180, 770}

	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			// ⚠️ **壳要带 `systemIsDark`**：走 `newShell(t)`（不带这个）的话「深色」
			// 那一档存出来和「浅色」一模一样——第一版就这样，而要验的恰恰是深色。
			// 照 `flatbutton_test.go` 那一族写。
			b := newTestShell(t, systemIsDark(dark))
			s, tt := b.s, b.tt
			// ⚠️ **时长和播放量都要给**：封面底部那条渐变上左边是播放量、右边是时长，
			// 两样都缺的话存出来的图上是一条**空的**黑条——看不出它是渐变、也看不出
			// 播放量那一半有没有接上。而这一页的接口明明两个都给（`homeMusicOf` 照翻）。
			songs := fullSongs()
			for i := range songs.tabs {
				for k := range songs.tabs[i] {
					songs.tabs[i][k].Duration = time.Duration(3+i)*time.Minute +
						time.Duration(5+10*k)*time.Second
					songs.tabs[i][k].View = int64(1200 + 137_000*i + 9_100*k)
				}
			}
			s.home.SetSongs(songs)
			homeFrame(tt)
			mountDisplay(t, s.app)

			// ── 列表档：不 hover（有表头）和 hover 一行 ───────────────────────
			setDisplay(t, s.app, tt, app.DisplayModeList)
			tt.Move(away[0], away[1])
			homeFrame(tt)
			save(t, dir, "30-"+name+"-列表-表头.png", tt)

			hoverRow(t, tt, songName(homeTabs[0], 3))
			save(t, dir, "31-"+name+"-列表-行hover.png", tt)
			tt.Move(away[0], away[1])
			homeFrame(tt)

			// ── 网格档：不 hover 和 hover 一格 ─────────────────────────────────
			//
			// ⚠️ **网格那一档没有表头**（理由见 `rowheader.go` 文件头），所以这里存的
			// 是封面右下角那个圆播放钮和封面底部那条渐变。
			setDisplay(t, s.app, tt, app.DisplayModeCard)
			tt.Move(away[0], away[1])
			homeFrame(tt)
			save(t, dir, "32-"+name+"-网格-平时.png", tt)

			hoverRow(t, tt, songName(homeTabs[0], 3))
			save(t, dir, "33-"+name+"-网格-卡片hover.png", tt)

			// ── 网格卡封面悬停放大（票 #467）────────────────────────────────
			//
			// ⚠️ **要一张完全露在画面里的那一格**：上面那张 hover 的第三格被右边的
			// 滚动条切掉一截，放大的那一部分看不见——那样的图证明不了放大画出来了。
			// ⚠️ **推满 300 毫秒**（`motion.HoverZoomTime`）：封面放大是 300ms 的
			// 动画，不推钟的话存出来的是「放大还没开始」的那一帧。
			tt.Move(away[0], away[1])
			// ⚠️ **推钟**：上一张 hover 的那一格要**淡完**才退干净——不推钟的话
			// 存出来的「平时」那张上，上一格还挂着半淡的那枚钮和那条渐变。
			tt.Advance(motion.HoverZoomTime + hover.Fade)
			homeFrame(tt)
			save(t, dir, "37-"+name+"-网格-封面放大-平时.png", tt)
			hoverCover(t, tt, songName(homeTabs[0], 2))
			save(t, dir, "38-"+name+"-网格-封面放大-hover.png", tt)

			// ── 紧凑档：表头换了一套更窄的列宽，顺带存一张 ─────────────────────
			setDisplay(t, s.app, tt, app.DisplayModeCompact)
			tt.Move(away[0], away[1])
			homeFrame(tt)
			hoverRow(t, tt, songName(homeTabs[0], 3))
			save(t, dir, "34-"+name+"-紧凑-行hover.png", tt)

			// ── 稍后再看：另一套行（自己画的，多两列）和另一套表头 ────────────
			//
			// ⚠️ **这一页必须单独存**：首页那一族走的是 `widgets/list` 的卡片，而这一页的
			// 行是页面自己画的（多播放量 / 加入时间两列），表头也是另一份规格——存图存的
			// 正是「另一套接上了没有」，而那种错在看首页那张图时一点也看不出来。
			shotLaterPage(t, dir, name, dark)
		})
	}
}

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
