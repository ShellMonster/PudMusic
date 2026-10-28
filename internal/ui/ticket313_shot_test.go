// 票 #313 的画面验收：行身份和静态显示文本「记好一份」之后，画面必须一模一样。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket313Shots
//
// # 这一票动的是「看不见」的那一层
//
// 这一票把**每帧每行现算**的几样东西记好了一份：行身份（装进 `any` 那一步）、
// 时长、播放量、行尾那枚「⋯」的无障碍名字。所以画面上**不该有任何变化**——
// 而那正是要验的：**记好的那一份要是记错了**（换了歌没跟上、删了一行没跟上），
// 画面上就会出现**上一首的时长**、**上一首的播放量**，而那比慢糟糕得多
// （用户看见的是一首 3:07 的歌在标着 4:21）。
//
// # 三处都要出
//
//   - 首页列表：三样都在（时长、播放量、行尾那枚「⋯」）；
//   - 队列抽屉：行身份那一列（抽屉里的行身份跟着歌走，不跟着位置走）；
//   - 稍后再看：那一页自己画行（它走的是 `library_rows.go` 那一套，
//     和首页那套不是同一份代码）。
//
// # 深浅两套都要出
//
// 播放量和时长那两列是**次级色**——深色下它们和底色更接近，看漏了看不出来；
// 而行尾那枚「⋯」的字色跟着歌名那一行走，两套主题里是两种颜色。
//
// ⚠️ **一个字都不许打真 B 站接口**：这一票的数据全是内存夹具 + 本机
// `httptest`（理由见 AGENTS §6）。
package ui

import (
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
)

// TestRenderTicket313Shots 把这一票动过的那三处各存一张 PNG，深浅各一套。
func TestRenderTicket313Shots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			saveTicket313Home(t, dir, dark, name)
			saveTicket313Drawer(t, dir, dark, name)
			saveTicket313Later(t, dir, dark, name)
		})
	}
}

// saveTicket313Home 出首页那一屏：几行歌，每行都带时长和播放量。
func saveTicket313Home(t *testing.T, dir string, dark bool, name string) {
	t.Helper()
	b := newTestShell(t, systemIsDark(dark), withSettings(app.DefaultSettings("")))
	s, tt := b.s, b.tt

	// ⚠️ **每一行都要带时长和播放量**：不然那两列是空的，而「空的那一列」验不出
	// 「记好的那一份有没有记错」。
	songs := []list.Music{
		{ID: "a", Title: "第一首", Owner: "UP 一", Duration: 3*time.Minute + 7*time.Second, View: 12345},
		{ID: "b", Title: "第二首", Owner: "UP 二", Duration: 1*time.Hour + 2*time.Minute + 3*time.Second, View: 1234567},
		{ID: "c", Title: "第三首", Owner: "UP 三", Duration: 45 * time.Second, View: 999},
		{ID: "d", Title: "第四首", Owner: "UP 四", Duration: 0, View: 0},
	}
	s.home.SetSongs(homeSongs{tabs: [homeTabCount][]list.Music{0: songs}})

	waitFor(t, "首页那几行画出来了", func() bool {
		tt.Frame()
		return tt.HasText("第一首")
	})
	homeFrame(tt)
	save(t, dir, "01-首页列表-"+name+".png", tt)
	// 右边那几列放大：时长、播放量、行尾那枚「⋯」都在那儿。
	saveRect(t, dir, "02-首页列表-右边几列放大-"+name+".png", tt, 700, 150, 500, 220)
	b.Close()
}

// saveTicket313Drawer 出队列抽屉开着的那一屏。
func saveTicket313Drawer(t *testing.T, dir string, dark bool, name string) {
	t.Helper()
	b := newTestShell(t, systemIsDark(dark), withSettings(app.DefaultSettings("")))
	s, tt := b.s, b.tt

	// ⚠️ **歌名和时长都要不一样**：抽屉里的行身份跟着**歌**走（不跟着位置走），
	// 而「记好的那一份」记错的时候，画面上最先露馅的就是**上一行的时长**。
	items := []app.Item{
		{Source: "local", Type: "audio", Title: "抽屉里的第一首", Path: "/tmp/a.flac"},
		{Source: "local", Type: "audio", Title: "抽屉里的第二首", Path: "/tmp/b.flac"},
		{Source: "local", Type: "audio", Title: "抽屉里的第三首", Path: "/tmp/c.flac"},
	}
	s.app.Queue().Reset(items...)
	s.drawerOpen = true

	waitFor(t, "抽屉画出来了", func() bool {
		tt.Frame()
		return tt.HasText("抽屉里的第一首")
	})
	homeFrame(tt)
	save(t, dir, "03-队列抽屉-"+name+".png", tt)
	saveRect(t, dir, "04-队列抽屉-放大-"+name+".png", tt, 820, 120, 380, 260)
	b.Close()
}

// saveTicket313Later 出稍后再看那一页。
//
// ⚠️ **这一页自己画行**（走 `library_rows.go` 那一套，和首页那套不是同一份
// 代码），而这一票在那一套上也动了三样东西（播放量、时长、行尾那枚「⋯」）。
func saveTicket313Later(t *testing.T, dir string, dark bool, name string) {
	t.Helper()
	f := newLibFake(t).withLater(1, 3, "甲", "乙", "丙")
	tt, s := libraryShellOpts(t, f, laterPath, systemIsDark(dark))

	waitFrames(t, tt, "把第一页拉回来", func() bool { return f.callsOf(laterPathAPI) == 1 })
	waitFor(t, "那几行画出来了", func() bool {
		tt.Frame()
		return tt.HasText("甲")
	})
	homeFrame(tt)
	save(t, dir, "05-稍后再看-"+name+".png", tt)
	saveRect(t, dir, "06-稍后再看-右边几列放大-"+name+".png", tt, 700, 150, 500, 220)
	_ = s
}
