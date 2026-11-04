// 全屏播放器的右栏：歌词来源 + 原文 / 翻译 chip、歌词、旋钮（原型 `.full-right`、
// `.ly-head`、`.lyrics`、`.knobs`，CSS 679–694 行、879–887 行）。
//
// # 「歌词区本体」是共用的一份
//
// 逐行高亮、滚到中间、译文叠在原词下面都在 `lyrics.go` 的 `viewLyricLines`，迷你播放器那一页
// 和这里共用；差别是画法（`lyricStyle`）：宋体、当前句放大 1.18、非当前句 38%（悬停 70%）、
// 可点（点一句跳到那一句）。
//
// ⚠️ **随包宋体只有 Bold 一档**（`typo`），所以非当前句也是粗的；原型里非当前句是常规字重。
//
// # 自己那一份歌词状态
//
// 满屏用的是 `s.fullLyric`，不是 `s.lyric`（迷你播放器那一页那一份）：两处画的是同一首歌，
// 可**滚到了哪儿是各人各眼的事**——共用一份的话两处会互相把对方的滚动位置拽回去。
package ui

import (
	"strconv"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/lyrics"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

const (
	// fullLyricsBoxKey 是歌词那一块的壳（铁律四）。
	fullLyricsBoxKey = "full:lyrics:box"
	// fullLyricsAreaKey 是歌词区那一块的键（铁律四）。
	fullLyricsAreaKey = "full:lyrics:area"
	// fullLyricsEmptyKey 是「暂无歌词」那一块的键。
	fullLyricsEmptyKey = "full:lyrics:empty"
	// fullLyricsLoadingKey 是「歌词还在路上」那一块的键（和空状态分开：说的是两件事）。
	fullLyricsLoadingKey = "full:lyrics:loading"

	// fullLyricActiveScale 是当前句的放大倍数（原型 `.ly.cur { transform: scale(1.18) }`）。
	fullLyricActiveScale = 1.18
	// fullLyricBaseAdd 是歌词字号的差：App 里歌词字号默认 20，原型默认 24（`S.lySize`，当前句
	// 24 × 1.18 ≈ 28），所以画的时候 +4；A− / A+ 照常 ±2，范围是原型的 18–34（App 字号 14–30）。
	fullLyricBaseAdd = 4
	// fullLyricFontMin / Max 是 A− / A+ 的范围（App 字号；原型 `Math.max(18, …)` / `Math.min(34, …)`）。
	fullLyricFontMin = 14
	fullLyricFontMax = 30
	// fullLyricPadY 是每一句上下的内边距（原型 `.ly { padding: 10px 0 }`）。
	fullLyricPadY = 10
	// fullLyricOffsetStepMS 是偏移的步长：原型是 0.5 秒。
	fullLyricOffsetStepMS = 500
	// fullLyricFontStep 是 A− / A+ 每按一下改的字号（App 字号，原型是 ±2）。
	fullLyricFontStep = 2
)

// viewFullRight 画右栏：歌词来源那一行、歌词、旋钮。
func (s *Shell) viewFullRight(c *ui.Context, st app.State, g fullGeo) {
	snap := s.app.Lyrics()
	s.viewFullLyHead(c, snap, g)
	s.viewFullLyrics(c, st, snap, g)
	s.viewFullKnobs(c, snap, g)
}

// viewFullLyHead 是歌词来源说明 + 「歌词原文 / 歌词翻译」chip（原型 `.ly-head`）。
//
// ⚠️ 歌词里没有任何一句带译文时 chip 照画但禁用（框架的 `Disabled`），不是藏起来——
// 用户得知道这个功能在、只是这一首没有。
func (s *Shell) viewFullLyHead(c *ui.Context, snap app.LyricsSnapshot, g fullGeo) {
	p := tok.OfFull(c.Theme())
	r := g.lyHead
	ui.Row(c).Key("full:ly-head").Absolute().Left(r.X).Top(r.Y).Width(r.W).Height(r.H).
		Gap(space.Px12).AlignItems(ui.Center).Children(func() {
		note := lyricOriginText(snap.Origin)
		if note == "" {
			note = "歌词"
		}
		ui.Box(c).Grow(1).Children(func() {
			fullMono(c, note, scale.Size12, 400, 12*0.1, p.Text3)
		})
		has := lyricsHaveTranslation(snap.Rows)
		label := "歌词原文"
		if s.full.lyTrans && has {
			label = "歌词翻译"
		}
		chip := pud.Chip(c, "full:ly-trans", label, "translate", s.full.lyTrans && has).
			Height(26).Radius(13)
		// 原型：高 26、surface 8% 白底；选中 = 纸白底深字（`.ly-head .chip[aria-pressed]`）。
		if !has {
			chip = chip.Disabled(true)
		}
		if chip.Clicked() {
			s.full.lyTrans = !s.full.lyTrans
		}
	})
}

// lyricsHaveTranslation 报这份歌词里有没有哪一句带译文。
func lyricsHaveTranslation(rows []lyrics.Row) bool {
	for _, r := range rows {
		if r.Translation != "" {
			return true
		}
	}
	return false
}

// viewFullLyrics 画歌词本体（或者「歌词还在路上」/「暂无歌词」）。
func (s *Shell) viewFullLyrics(c *ui.Context, st app.State, snap app.LyricsSnapshot, g fullGeo) {
	lv := &s.fullLyric
	// 🔴 认换歌在这一帧的最开头，早于任何一句决定（理由同 lyrics.go 那一处）。
	lv.onSong(snap.Key)

	active := lyrics.ActiveAt(snap.Rows, lyricNow(st.Position, snap.OffsetMS))
	if active < 0 {
		// 没有歌词：说清楚「没有」，而不是留一块空白（UX 报告 B3）。
		lv.forget()
	}
	r := g.lyrics
	box := ui.Column(c).Key(fullLyricsBoxKey).
		Absolute().Left(r.X).Top(r.Y).Width(r.W).Height(r.H).Label("满屏歌词")
	box.Children(func() {
		if active < 0 {
			// ⚠️ **两档要分开**：换歌之后歌词要先读盘、等字幕、必要时自动找一次——那段时间里
			// 画「暂无歌词」是结论而不是过程。
			if snap.Loading {
				s.viewFullLyricsLoading(c)
			} else {
				s.viewFullLyricsEmpty(c)
			}
			return
		}
		fg := s.fullLyricColor()
		lv.anim = &fullLyricScrollAnim
		big := snap
		big.FontSize += fullLyricBaseAdd
		s.viewLyricLines(c, lv, big, active, lyricStyle{
			color:        fg,
			muted:        tok.Full.Text3,
			area:         fullLyricsAreaKey,
			ns:           lyricNSFull,
			mask:         true,
			face:         fullLyricFace,
			padY:         fullLyricPadY,
			gap:          -1,
			lineMul:      1.5,
			activeScale:  fullLyricActiveScale,
			idleAlpha:    0.38,
			hoverAlpha:   0.70,
			hideOrigin:   true,
			hideTrans:    !s.full.lyTrans,
			transRatio:   0.6,
			transGap:     2,
			transWeight:  400,
			maskAlpha:    fullLyricMask,
			presenceAnim: &fullLyricPresenceAnim,
			pick:         s.fullLyricPick,
		})
	})
	if active < 0 {
		return
	}
	// ⚠️ 放在整棵树建完之后：它要用容器和正在唱那一句的框，而那两个是上一帧量到的。
	lv.centre(c, active, lyricShapeOf(snap.Rows), snap.FontSize+fullLyricBaseAdd, s.snapScroll)
}

// fullLyricFace 是歌词这一行用的字体和字重：原型 `.ly` 是宋体、常规字重，当前句 700
// （`.ly.cur { font-weight: 700 }`）。
//
// ⚠️ **字体的取值只在这一处**：随包宋体有 Regular（400）和 Bold（700）两档，同一个族名，
// 框架按字重挑；非当前句 400 就是宋体常规，当前句 700。
func fullLyricFace(active bool) (family string, weight int) {
	_ = typo.Register()
	if active {
		return typo.DisplayFamily, 700
	}
	return typo.DisplayFamily, 400
}

// fullLyricMask 是歌词每一行的不透明度随位置的变化（原型 `mask-image: linear-gradient(transparent,
// #000 18%, #000 82%, transparent)`）：上下各 18% 线性淡出。
func fullLyricMask(rel float32) float32 {
	switch {
	case rel <= 0 || rel >= 1:
		return 0
	case rel < 0.18:
		return rel / 0.18
	case rel > 0.82:
		return (1 - rel) / 0.18
	}
	return 1
}

// fullLyricScrollAnim 是滚到中间那一下：原型 `transform 600ms cubic-bezier(.25, 1.2, .4, 1)`
// （带一点回弹）。
var fullLyricScrollAnim = ui.Animation{Duration: 600 * time.Millisecond, Ease: ui.CubicBezier(0.25, 1.2, 0.4, 1)}

// fullLyricPresenceAnim 是当前句放大 / 变亮的过渡：原型颜色 `--t-med`、缩放 `--t-slow`，
// 都是 ease-out；一个过渡只有一个时长，取慢的那一档。
var fullLyricPresenceAnim = ui.Animation{Duration: motion.Slow, Ease: motion.EaseOut}

// fullLyricPick 是点了某一句：跳到那一句开头（原型 `seek-line`）。
func (s *Shell) fullLyricPick(row lyrics.Row) {
	snap := s.app.Lyrics()
	s.app.SeekTo(row.At - lyricNow(0, snap.OffsetMS))
}

// viewFullLyricsEmpty 是「这一首没有歌词」那一句。找歌词的入口是下面旋钮行里的放大镜。
func (s *Shell) viewFullLyricsEmpty(c *ui.Context) {
	ui.Box(c).Key(fullLyricsEmptyKey).Grow(1).FillWidth().Center().Gap(emptyGap).Children(func() {
		ui.Icon(c, icon(noLyrics.Icon)).FontSize(emptyIconSize).TextColor(tok.Full.Text3)
		ui.Text(c, noLyrics.Title).FontSize(scale.Font(c, scale.EmptyTitle)).TextColor(tok.Full.Text)
	})
}

// viewFullLyricsLoading 是「这一首的歌词还在路上」：只有一枚转圈，不摆插画（摆了用户会把
// 「还没好」读成「没有」）。
func (s *Shell) viewFullLyricsLoading(c *ui.Context) {
	ui.Box(c).Key(fullLyricsLoadingKey).Grow(1).FillWidth().Center().Children(func() {
		ui.Spinner(c).Size(loadingSpinnerSize, loadingSpinnerSize).
			TextColor(tok.Full.Text).Label(lyricsLoadingText)
	})
}

// fullLyricColor 是满屏歌词的字色：用户在偏好里挑了就用挑的那个，没挑是纸白 `#ECEBE8`。
func (s *Shell) fullLyricColor() ui.Color {
	if hex := s.pagePrefs().LyricsColor; hex != "" {
		if col, ok := parseHexColor(hex); ok {
			return col
		}
	}
	return tok.Full.Text
}

// viewFullKnobs 是歌词下面那一行旋钮（原型 `.knobs`）：偏移 −  数值  +，字号 A− A+，找歌词。
//
// 偏移显示的是**延后的量**（原型 `S.lyOff`）：「−」= 歌词提前 0.5 秒，「+」= 延后 0.5 秒。
// App 里的偏移是反的（正数 = 同一播放位置落在更后一句 = 歌词更早），所以这里取反。
func (s *Shell) viewFullKnobs(c *ui.Context, snap app.LyricsSnapshot, g fullGeo) {
	p := tok.OfFull(c.Theme())
	r := g.knobs
	ui.Row(c).Key("full:knobs").Absolute().Left(r.X).Top(r.Y).Width(r.W).Height(r.H).
		Gap(space.Px8).AlignItems(ui.Center).Label("歌词旋钮").Children(func() {
		if fullKnob(c, "full:ly-off", "歌词提前 0.5 秒", "−", "").Clicked() {
			s.app.SetLyricsOffset(snap.OffsetMS + fullLyricOffsetStepMS)
		}
		fullMono(c, s.full.txt.offsetText(snap.OffsetMS), scale.Size12, 500, 0, p.Text3).
			MinWidth(44).TextAlign(ui.Center)
		if fullKnob(c, "full:ly-on", "歌词延后 0.5 秒", "+", "").Clicked() {
			s.app.SetLyricsOffset(snap.OffsetMS - fullLyricOffsetStepMS)
		}
		ui.Box(c).Width(12).Shrink(0)
		if fullKnob(c, "full:ly-small", "歌词字号减小", "A−", "").Clicked() {
			s.app.SetLyricsFontSize(max(snap.FontSize-fullLyricFontStep, fullLyricFontMin))
		}
		if fullKnob(c, "full:ly-big", "歌词字号增大", "A+", "").Clicked() {
			s.app.SetLyricsFontSize(min(snap.FontSize+fullLyricFontStep, fullLyricFontMax))
		}
		ui.Box(c).Width(12).Shrink(0)
		if fullKnob(c, "full:ly-find", "找歌词", "", "search").Clicked() {
			s.openLyricSearch()
		}
	})
}

// fullOffsetText 是偏移的读数：`+0.5s` / `−0.5s` / `0.0s`（延后为正）。
func fullOffsetText(ms int) string {
	v := float64(-ms) / 1000
	switch {
	case v > 0:
		return "+" + fmtSeconds(v)
	case v < 0:
		return "−" + fmtSeconds(-v)
	}
	return "0.0s"
}

func fmtSeconds(v float64) string {
	n := int(v*10 + 0.5)
	return strconv.Itoa(n/10) + "." + strconv.Itoa(n%10) + "s"
}

// fullKnob 是一枚旋钮（原型 `.knob`）：32 的圆、surface 8% 白底、悬停 16%，里面是 12 号等宽 600 的字
// 或 16 的图标。
func fullKnob(c *ui.Context, key, label, txt, icon string) ui.Element {
	b := ui.ButtonBase(c).Key(key).Label(label).Size(32, 32).Radius(16).Shrink(0).
		AlignItems(ui.Center).Justify(ui.Center)
	bg := ui.RGBA(255, 255, 255, 0.08)
	if b.Hovered() {
		bg = ui.RGBA(255, 255, 255, 0.16)
	}
	b = b.Background(bg).Transition(motion.Fade)
	b.Children(func() {
		if icon != "" {
			ui.Icon(c, pud.Icon(icon)).FontSize(16).TextColor(tok.Full.Text)
			return
		}
		fullMono(c, txt, scale.Size12, 600, 0, tok.Full.Text)
	})
	return b
}
