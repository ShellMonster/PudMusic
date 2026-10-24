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
//	         盖出来的是背景上一个看得见的方块。改走**上游那条遮罩的语义**
//	         （行在容器的上下沿附近时淡出，滑到中间时是全亮的），只是**逐行**
//	         算（框架没有遮罩那一层）——背景一个像素都不动。
//	对齐     **跟着上游**：封面开着的时候**左对齐**（上游 `centered={!showCover}`，
//	         那些句子的左沿咬在内容盒的左沿上），封面关掉的时候居中。
//	留白     内容盒上下各 96（上游 `py-24`）、左右各 48（上游 `px-12`）
//	空状态   「暂无歌词」+ 一个「找歌词」的键
//	旋钮     **右下角浮着三枚**（字号 / 偏移 / 找歌词），和上游一样
//
// # 右下角那三枚旋钮挂在**内容盒**上
//
// 上游那三枚是 `absolute right-6 bottom-6`（`components/lyrics/index.tsx:381`），
// 而那个 `right-6` 是相对**歌词内容盒**（`px-12 py-24` 去掉内边距之后的那一块）
// 算的：1200×800 的窗口上内容盒是 648..1152，于是三枚的**右沿是 1128**、
// **底沿是 680**。
//
// ⚠️ **挂在壳上算出来的是 1176**（差那一份 48 的左右留白）——框架的绝对定位是
// 相对**父节点的 padding box**（`mygo/ui/layout.go:1170` 的 `layoutAbsolute`），
// 而 padding box 是**含内边距**的那一层。所以这一屏多了一个 `fullLyricsContentKey`
// 那一块：内边距在壳上，旋钮挂在内容盒上。
//
// # 自己那一份歌词状态
//
// 满屏用的是 `s.fullLyric`，不是 `s.lyric`（迷你播放器那一页那一份）：两处画
// 的是同一首歌，可**滚到了哪儿是各人各眼的事**——迷你播放器那一页停在用户上次
// 看到的那一句，满屏跟着歌往前走。共用一份的话，两处会互相把对方的滚动位置
// 拽回去（症状是「在满屏里翻歌词，翻一下就被拽回中间」）。
//
// 那一列旋钮的浮层状态也跟着分家（`s.fullLyric.fontOpen` / `offsetOpen`），
// 理由同上：两个窗口各记各的。
package ui

import (
	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/lyrics"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

const (
	// fullLyricsBoxKey 是右边那一列的壳（铁律四）。
	fullLyricsBoxKey = "full:lyrics:box"
	// fullLyricsContentKey 是壳里**内容那一块**的键（铁律四）。
	//
	// ⚠️ **它和壳分开，是因为右下角那三枚旋钮要挂在内容那一块上**：上游那个
	// `right-6` 是相对内容盒算的，挂在壳上会差那一份左右留白（见文件头）。
	fullLyricsContentKey = "full:lyrics:content"
	// fullLyricsAreaKey 是歌词区那一块的键（铁律四）。
	fullLyricsAreaKey = "full:lyrics:area"
	// fullLyricsEmptyKey 是「暂无歌词」那一块的键（铁律四）。
	fullLyricsEmptyKey = "full:lyrics:empty"

	// fullLyricsPadY 是歌词那一列**上下**的留白。
	//
	// ⚠️ **96 是上游的 `py-24`**（`full-screen-player/index.tsx:357`：歌词容器
	// `px-12 py-24`）。它管的是内容盒的上下沿：800 高的窗口上内容盒是 96..704，
	// 于是**正在唱的那一句落在窗口的正中**（400）——上游也是正中。
	//
	// 左右那一份是 `fullStagePadX`（48，上游的 `px-12`），两处共用一个数。
	fullLyricsPadY = space.Px96
)

// viewFullLyrics 画满屏那一屏的歌词（右边那一列）。
//
// area 是这一列在窗口里的框（见 `viewFullStage`）。
//
// hidden 是界面藏起来没有：藏起来的时候**不画那三枚旋钮**——它们是**控件**
// （上游是 `showControls={isUiVisible}`），而歌词是**内容**（上游的歌词容器不
// 跟着藏）。
func (s *Shell) viewFullLyrics(c *ui.Context, st app.State, area ui.Rect, hidden bool) {
	snap := s.app.Lyrics()
	lv := &s.fullLyric
	// 🔴 认换歌在这一帧的最开头，早于任何一句决定（理由同 lyrics.go 那一处）。
	lv.onSong(snap.Key)

	active := lyrics.ActiveAt(snap.Rows, lyricNow(st.Position, snap.OffsetMS))
	if active < 0 {
		// 没有歌词：说清楚「没有」，而不是留一块空白（UX 报告 B3）。
		lv.forget()
	}

	// ⚠️ **分栏只看「显示封面」那一个开关**（上游 `centered={!showCover}`）：
	// 封面开着的时候歌词是**左对齐**的一列（那些句子的左沿咬在内容盒的左沿上），
	// 封面关掉的时候歌词占满整窗，于是居中。
	centered := !s.app.FullPlayerPrefs().ShowCover

	ui.Box(c).Key(fullLyricsBoxKey).
		Absolute().Left(area.X).Top(area.Y).Width(area.W).Height(area.H).
		Padding(fullLyricsPadY, fullStagePadX).
		Children(func() {
			// 内容盒：上游的 `px-12 py-24` 去掉内边距之后的那一块。
			//
			// ⚠️ **名字挂在这块上，不挂在外壳上**：读屏软件念得出「满屏歌词」，
			// 而无头 Tester 也靠它量这一列的框——量的是**字画到哪儿**（内容盒），
			// 不是外壳（含留白）到哪儿。歌词的淡出也是按内容盒算的（上游那条
			// 遮罩是按**容器**算的），两者必须是同一块。
			ui.Box(c).Key(fullLyricsContentKey).Grow(1).FillWidth().
				Label("满屏歌词").
				Children(func() {
					if active < 0 {
						// 空状态：没有滚动容器，旋钮列就挂在内容盒上（位置一样）。
						s.viewFullLyricsEmpty(c)
						if !hidden {
							s.viewFullLyricKnobs(c, snap, lv)
						}
						return
					}
					s.viewLyricLines(c, lv, snap, active, lyricStyle{
						color:  s.fullLyricColor(c),
						area:   fullLyricsAreaKey,
						ns:     lyricNSFull,
						center: centered,
						// ⚠️ **0**：上游的行没有左右内边距，于是左对齐的那些句子
						// 左沿就咬在内容盒的左沿上（1200 宽的窗口上是 x=648）。
						padX: fullLyricBarPad(centered),
						bar:  !centered,
						mask: true,
						// ⚠️ **旋钮列画在滚动容器里面**（理由见 `lyricStyle.overlay`）：
						// 挂在外面（滚动容器的兄弟）的话，滚轮落在它身上——包括两枚
						// 之间的缝——就滚不动歌词了。
						overlay: func(c *ui.Context) {
							if !hidden {
								s.viewFullLyricKnobs(c, snap, lv)
							}
						},
					})
				})
		})

	if active < 0 {
		return
	}
	// ⚠️ 放在整棵树建完之后：它要用容器和正在唱那一句的框，而那两个是上一帧
	// 量到的（理由同 lyrics.go 里那一处）。
	lv.centre(c, active, lyricShapeOf(snap.Rows), snap.FontSize, s.snapScroll)
}

// viewFullLyricKnobs 画满屏那一屏右下角浮着的那三枚旋钮。
//
// ⚠️ **它们是控件，界面藏起来的时候不画**（上游 `showControls={isUiVisible}`）：
// 指针不动三秒之后整屏的控件都收走，而歌词留着（歌词是内容）。
//
// ⚠️ **前景色给满屏那一档**：这一列压在糊过的封面上，主题的字色在浅色主题下是
// 黑的（压在那块灰底上只有 3.8:1，读不出来）。那枚图标是这一档，那层圆底是
// 它的 20%（上游 `bg-foreground/20`）。
//
// ⚠️ **三枚都画**（上游 `index.tsx:380-398` 那一层在没歌词时也摆）：字号和偏移
// 改的是**存下来的那个值**，下一首歌有歌词时就按新的画了——它们无处可附，可
// 它们也不是死的。
func (s *Shell) viewFullLyricKnobs(c *ui.Context, snap app.LyricsSnapshot, lv *lyricsView) {
	on, _ := s.fullForeground(c)
	s.viewLyricKnobColumnWith(c, snap, true, lv, lyricKnobFull, on)
}

// viewFullLyricsEmpty 是「这一首没有歌词」那一句。
//
// ⚠️ **它不走 `viewEmpty`**：那一处用的是主题的正文色与次要色，而满屏这一屏
// 压在**糊过的封面**上——浅色主题下主题的正文色是黑的，压在浅色封面上会糊
// 进去。满屏这一屏的字色是 `fullForeground` 那一档（白 92% / 黑 92%，跟着底走）。
//
// ⚠️ **它画在内容盒里**（壳 → 内容盒 → 这一块），所以它居中的那个点是内容盒的
// 中心（1200 宽的窗口上是 900）——上游那个「暂无歌词」也是居中在内容盒里的。
//
// # 这里**不再摆**一枚「找歌词」
//
// 「怎么才能不空」那一句必须兑现得了——找歌词的入口**有**，就是右下角那一列
// 旋钮里的「找歌词」（按下去开的就是歌词搜索弹窗，`lyricssearch.go`，那个弹窗
// 画在满屏**上面**，见 `shell.go` 里 `viewLyricDialogs` 的次序）。
//
// ⚠️ **两个一模一样的「找歌词」摆在同一屏上是噪音**（评审抓到的那一条）：中间
// 一个、右下角一个，用户会以为是两个不同的动作。上游那一屏只有一个入口
// （右下角那一枚），而主窗口「正在播放」那一页也是同一个样子（那一列旋钮是它
// 唯一的入口）——所以这里跟上游、跟自己那一页都一致。
func (s *Shell) viewFullLyricsEmpty(c *ui.Context) {
	on, dim := s.fullForeground(c)
	ui.Box(c).Key(fullLyricsEmptyKey).
		Grow(1).FillWidth().Center().Gap(emptyGap).
		Children(func() {
			ui.Icon(c, icon(noLyrics.Icon)).FontSize(emptyIconSize).TextColor(dim)
			ui.Text(c, noLyrics.Title).FontSize(scale.Font(c, scale.EmptyTitle)).TextColor(on)
			ui.Text(c, noLyrics.Hint).FontSize(scale.Font(c, scale.EmptyHint)).TextColor(dim)
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

// fullLyricBarPad 给左对齐的歌词让出竖线那一列（居中时不需要）。
func fullLyricBarPad(centered bool) float32 {
	if centered {
		return 0
	}
	return 8
}
