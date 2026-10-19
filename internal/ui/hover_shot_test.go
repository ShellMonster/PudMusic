package ui

import (
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/hover"
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
			// ⚠️ **歌名后面要带时长**：`homeSongsOf` 不给时长，而封面底部那条渐变上写着
			// 时长——没有它的话渐变是空的一条（左边那个播放量我们压根没有，见
			// `widgets/list/grid.go`），存出来的图上根本看不出那条渐变在。
			songs := fullSongs()
			for i := range songs.tabs {
				for k := range songs.tabs[i] {
					songs.tabs[i][k].Duration = time.Duration(3+i)*time.Minute +
						time.Duration(5+10*k)*time.Second
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

			// ── 紧凑档：表头换了一套更窄的列宽，顺带存一张 ─────────────────────
			setDisplay(t, s.app, tt, app.DisplayModeCompact)
			tt.Move(away[0], away[1])
			homeFrame(tt)
			hoverRow(t, tt, songName(homeTabs[0], 3))
			save(t, dir, "34-"+name+"-紧凑-行hover.png", tt)
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
