// 「复制歌曲信息」「复制封面」：播放条封面和满屏封面的右键菜单。
//
// 文字在菜单被选中那一帧现拼，封面的 PNG 在后台 goroutine 里取、编码，写剪贴板再回
// 到界面线程——稳态每帧不多做任何事（菜单回调只在弹出和选中时跑）。
package ui

import (
	"context"
	"strings"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/egoist/mygo/ui"
)

// currentSongInfo 是「复制歌曲信息」放进剪贴板的文字：歌名 - 歌手，时长，然后是来处
// （B 站的稿件号和链接，或本地文件路径）。没有的字段整行不写。
func currentSongInfo(st app.State, it app.Item) string {
	title := st.Title
	if artist := strings.TrimSpace(it.Artist); artist != "" {
		title += " - " + artist
	}
	lines := []string{title}
	if d := clockText(st.Duration); d != "" {
		lines = append(lines, "时长 "+d)
	}
	switch {
	case it.Bvid != "":
		lines = append(lines, it.Bvid)
		if link := app.BiliLink(it); link != "" {
			lines = append(lines, link)
		}
	case st.Path != "":
		lines = append(lines, st.Path)
	}
	return strings.Join(lines, "\n")
}

// coverMenu 建封面右键菜单。没有封面时「复制封面」灰着，并说明原因。
func (s *Shell) coverMenu(m *ui.Menu) {
	st := s.app.Snapshot()
	if !st.HasTrack {
		return
	}
	if m.Item("复制歌曲信息").Chosen() {
		it, _ := s.app.CurrentItem()
		s.wake.writeClipboard(currentSongInfo(st, it))
		s.addToast("已复制歌曲信息", notice.Success)
	}
	if st.CoverURL == "" {
		m.Item("复制封面（这首歌没有封面）").Disabled(true)
		return
	}
	if m.Item("复制封面").Chosen() {
		s.copyCover(st.CoverURL)
	}
}

// copyCover 在后台取封面 PNG，成功后在界面线程写剪贴板；失败说一句。
func (s *Shell) copyCover(url string) {
	win := s.win
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		b, err := s.images.PNG(ctx, url)
		if err != nil {
			s.addToast("复制封面失败：封面没取到或解不开", notice.Danger)
			s.wake.invalidate()
			return
		}
		write := func() {
			s.wake.writePNG(b)
			s.addToast("已复制封面", notice.Success)
		}
		if win == nil {
			write()
			return
		}
		win.Update(write)
	}()
}
