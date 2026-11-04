// T1 外壳（侧栏 / 顶栏 / 播放条 / 弹层 / Toast / 命令面板）的画面验收。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderT1ShellShots
//
// 深浅两套都出，逐张和 ref/ 里的原型截图对照。数据全是合成的，没有真实封面。
package ui

import (
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// t1BarState 是外壳截图里播放条上那一首（合成数据，不带封面）。
func t1BarState() app.State {
	return app.State{
		HasTrack: true, Title: "渺小", Artist: "田馥甄 · 夜航电台", ArtistMid: 12345,
		Source: app.SourceOnline, BiliURL: "https://www.bilibili.com/video/BV1xx411c7mD",
		Quality: "Hi-Res", Rate: 1, PlayMode: app.PlayModeLoop, Volume: 0.8,
		Duration: 4*time.Minute + 24*time.Second, Position: 37 * time.Second,
		Playing: true,
	}
}

// t1Shell 造一个整窗外壳：和 `Shell.viewMain` 同一条画法，但播放状态用合成的 st。
func t1Shell(t *testing.T, dark bool, st app.State, opts ...shellOpt) *testShell {
	t.Helper()
	all := append([]shellOpt{
		systemIsDark(dark), loggedInWith("42"),
		withPrefs(mygoui.Preferences{ReduceMotion: true, TextScale: 1}),
		withView(func(s *Shell) func(*mygoui.Context) {
			return func(c *mygoui.Context) {
				s.wake.set(c)
				s.applyFrameTheme(c)
				s.syncUnderlay(c)
				s.drawUnderlay(c, st)
				s.guardAt("命令面板", s.viewCommandPalette)(c)
				s.viewToasts(c)
			}
		}),
	}, opts...)
	b := newTestShell(t, all...)
	homeFrame(b.tt)
	return b
}

func TestRenderT1ShellShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{true, false} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			b := t1Shell(t, dark, t1BarState())
			save(t, dir, name+"-home-rec.png", b.tt)
		})
	}
}
