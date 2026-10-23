// 满屏播放器里的歌词：右边那一列。
//
// # 「歌词区本体」是共用的一份
//
// 逐行高亮、放大 1.5 倍、译文叠在原词下面、上下渐隐、正在唱的那一句滚到中间
// ——那些都在 `lyrics.go` 的 `viewLyricLines`，**迷你播放器那一页和满屏这一屏
// 共用同一份代码**。
//
// 两处差的是**画法**（`lyricStyle`）：字色与渐隐盖到的颜色。满屏这一屏压在
// **糊过的封面**上，主题的正文色（浅色主题下是黑的）压在上面会糊进去——
// 所以满屏用的是自己的前景色（白 92% / 黑 92%，跟着底走，见 `fullForeground`）。
//
// # 这一屏与迷你播放器那一页的差别
//
//	字色     满屏自己的前景色；用户在偏好里挑了「歌词字体颜色」就用挑的那个
//	渐隐     **不盖那两条遮罩**：遮罩是盖一层底色，而满屏的底是糊过的封面——
//	         盖出来的是背景上一个看得见的方块。改走「离正在唱的那一句越远
//	         越暗」那一档（`lyricRamp`），效果和上游那条 `maskImage` 一样，
//	         而背景一个像素不动。
//	居中     每一行居中（迷你播放器那一页是左对齐：那一页是歌词纸）
//	空状态   「暂无歌词」+ 一个「找歌词」的键
//	旋钮     **不画**：满屏的右下角是控制条（进度 + 五枚键），而迷你播放器
//	         那一页的右下角是空的
//
// # 自己那一份歌词状态
//
// 满屏用的是 `s.fullLyric`，不是 `s.lyric`（迷你播放器那一页那一份）：两处画
// 的是同一首歌，可**滚到了哪儿是各人各眼的事**——迷你播放器那一页停在用户上次
// 看到的那一句，满屏跟着歌往前走。共用一份的话，两处会互相把对方的滚动位置
// 拽回去（症状是「在满屏里翻歌词，翻一下就被拽回中间」）。
package ui

import (
	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/lyrics"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/egoist/mygo/ui"
)

const (
	// fullLyricsBoxKey 是右边那一列的壳（铁律四）。
	fullLyricsBoxKey = "full:lyrics:box"
	// fullLyricsAreaKey 是歌词区那一块的键（铁律四）。
	fullLyricsAreaKey = "full:lyrics:area"
	// fullLyricsEmptyKey 是「暂无歌词」那一块的键（铁律四）。
	fullLyricsEmptyKey = "full:lyrics:empty"

	// fullLyricsPadX 是歌词那一列左右的留白。
	//
	// ⚠️ **右边那一份是给分集那个把手留的**：那个把手贴在窗口右边（26 宽），
	// 歌词压到它底下去的话两样叠在一起，谁也读不出来。
	fullLyricsPadX = 48
)

// viewFullLyrics 画满屏那一屏的歌词（右边那一列）。
//
// area 是这一列在窗口里的框（见 `viewFullStage`）。
func (s *Shell) viewFullLyrics(c *ui.Context, st app.State, area ui.Rect) {
	snap := s.app.Lyrics()
	lv := &s.fullLyric
	// 🔴 认换歌在这一帧的最开头，早于任何一句决定（理由同 lyrics.go 那一处）。
	lv.onSong(snap.Key)

	active := lyrics.ActiveAt(snap.Rows, lyricNow(st.Position, snap.OffsetMS))
	if active < 0 {
		// 没有歌词：说清楚「没有」，而不是留一块空白（UX 报告 B3）。
		lv.forget()
		s.viewFullLyricsEmpty(c, area)
		return
	}

	ui.Box(c).Key(fullLyricsBoxKey).
		Absolute().Left(area.X).Top(area.Y).Width(area.W).Height(area.H).
		Padding(0, fullLyricsPadX).
		// 名字：读屏软件念得出「满屏歌词」，而无头 Tester 也靠它量这一列的框
		//（分集面板开着时它必须让开面板——那一条要量它的右沿）。
		Label("满屏歌词").
		Children(func() {
			s.viewLyricLines(c, lv, snap, active, lyricStyle{
				color:  s.fullLyricColor(c),
				area:   fullLyricsAreaKey,
				ns:     lyricNSFull,
				center: true,
				ramp:   true,
			})
		})

	// ⚠️ 放在整棵树建完之后：它要用容器和正在唱那一句的框，而那两个是上一帧
	// 量到的（理由同 lyrics.go 里那一处）。
	lv.centre(c, active, lyricShapeOf(snap.Rows), snap.FontSize, s.snapScroll)
}

// viewFullLyricsEmpty 是「这一首没有歌词」那一句。
//
// ⚠️ **它不走 `viewEmpty`**：那一处用的是主题的正文色与次要色，而满屏这一屏
// 压在**糊过的封面**上——浅色主题下主题的正文色是黑的，压在浅色封面上会糊
// 进去。满屏这一屏的字色是 `fullForeground` 那一档（白 92% / 黑 92%，跟着底走）。
//
// 而**「怎么才能不空」那一句必须兑现得了**：这里摆一枚「找歌词」的键，按下去
// 开的就是歌词搜索弹窗（`lyricssearch.go`，那个弹窗画在满屏**上面**，见
// `shell.go` 里 `viewLyricDialogs` 的次序）。
func (s *Shell) viewFullLyricsEmpty(c *ui.Context, area ui.Rect) {
	on, dim := s.fullForeground(c)
	ui.Box(c).Key(fullLyricsEmptyKey).
		Absolute().Left(area.X).Top(area.Y).Width(area.W).Height(area.H).
		Center().Gap(emptyGap).
		// 名字和歌词那一列一样（两者是同一块地方的两档内容）：读屏念得出
		// 「满屏歌词」，而无头 Tester 量这一块的右沿靠它。
		Label("满屏歌词").
		Children(func() {
			ui.Icon(c, icon(noLyrics.Icon)).FontSize(emptyIconSize).TextColor(dim)
			ui.Text(c, noLyrics.Title).FontSize(scale.Font(c, emptyTitleSize)).TextColor(on)
			ui.Text(c, noLyrics.Hint).FontSize(scale.Font(c, emptyHintSize)).TextColor(dim)
			// 字色给满屏那一档：这一枚是浮在糊过的封面上的，主题的字色在浅色
			// 主题下是黑的（压在上面读不出来）。
			if flatButton(c, lyricSearchKnobLabel).TextColor(on).Clicked() {
				s.openLyricSearch()
			}
		})
}

// fullLyricColor 是满屏歌词的字色。
//
// 用户在偏好里挑了「歌词字体颜色」就用挑的那个——那是那个开关存在的意义
// （它以前是个死控件，见票 #236）；没挑就用满屏自己的前景色。
func (s *Shell) fullLyricColor(c *ui.Context) ui.Color {
	on, _ := s.fullForeground(c)
	if hex := s.app.FullPlayerPrefs().LyricsColor; hex != "" {
		if col, ok := parseHexColor(hex); ok {
			return col
		}
	}
	return on
}
