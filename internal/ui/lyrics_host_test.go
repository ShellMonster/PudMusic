package ui

// 歌词测试的宿主（原来借 `/mini-player` 那一页——主窗口里的「正在播放」页，T1 按原型删掉了）。
// 这里只是把「歌名 + 歌词区」摆成一页，让歌词那一族用例有地方量；它不是路由，也不在产品里。

import (
	"testing"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	mygoui "github.com/egoist/mygo/ui"
)

// lyricHost 是歌词用例的整窗：主题 + 一页（歌名 + 歌词）+ 歌词弹窗 + 提示。
func lyricHost(s *Shell) func(*mygoui.Context) {
	return func(c *mygoui.Context) {
		s.wake.set(c)
		// 和 `Shell.View` 一样：窗口从看不见回来的那一帧，让「有中间状态的动画」一次到位。
		if now := c.Now(); s.snapScroll && !now.Equal(s.snapAt) {
			s.snapScroll = false
		}
		if s.presence.takeResumed() {
			s.snapScroll, s.snapAt = true, c.Now()
		}
		s.applyFrameTheme(c)
		st := s.app.Snapshot()
		mygoui.Column(c).Key("lyric-host").FillWidth().FillHeight().Background(tok.Of(c.Theme()).Bg).Children(func() {
			if !st.HasTrack {
				return
			}
			mygoui.Column(c).Key("page:lyric-host").Grow(1).Padding(space.Px28).Gap(space.Px12).Children(func() {
				mygoui.Text(c, st.Title).FontSize(scale.Font(c, scale.PlayerTitle))
				s.viewLyrics(c, s.app.Lyrics(), st.Position)
			})
		})
		s.guardAt("歌词弹窗", s.viewLyricDialogs)(c)
		s.viewToasts(c)
	}
}

// newLyricHostShell 造一个用 `lyricHost` 当整窗的外壳。
func newLyricHostShell(t *testing.T, opts ...shellOpt) (*Shell, *mygoui.Tester) {
	t.Helper()
	b := newTestShell(t, append(opts, withView(lyricHost))...)
	return b.s, b.tt
}
