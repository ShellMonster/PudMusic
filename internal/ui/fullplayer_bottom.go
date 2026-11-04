// 全屏播放器的底栏：频谱、章节名、进度、控制区（原型 `.full-bottom` / `.full-ctrl`，
// CSS 697–705 行、855–858 行）。
//
//	频谱     64 根柱子，22% 透明度，高 40（关掉时这一行照样占着）
//	章节名   12 号等宽 500，字距 0.08em，text-3，最小高 17
//	进度     左右各 52 的读数（11 号等宽 text-3），中间轨道 4；章节点是 12 的空心圆
//	控制区   左：音质 / 倍速 / 弹幕　中：模式 · 上一首 · 64 播放键 · 下一首 · 睡眠　右：EQ / 偏好 / 迷你
package ui

import (
	"strconv"
	"strings"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/shadow"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

const (
	// fullSpectrumBars 是频谱柱数（原型 `Array.from({length: 64})`）。
	fullSpectrumBars = 64
	// fullSeekReadW 是进度两头读数的宽（`grid-template-columns: 52px 1fr 52px`）。
	fullSeekReadW = 52
	// fullChapDot 是章节点直径（`.full .chap { width: 12px; height: 12px; border: 2px }`）。
	fullChapDot = 12
	// fullCtrlCenterW 是控制区中间那一组的宽：4 个 36 的键 + 64 的播放键 + 4 道 24 的缝。
	fullCtrlCenterW = 4*tok.IconHit + fullPlayHit + 4*24
)

// viewFullBottom 是整条底栏。**位置按底边算**（`Absolute().Bottom(0)`），内边距 8/padX/28。
func (s *Shell) viewFullBottom(c *ui.Context, st app.State, g fullGeo) {
	ui.Column(c).Key("full-bottom").Absolute().Left(0).Right(0).Bottom(0).
		Padding(fullBottomPadTop, g.padX, fullBottomPadBottom, g.padX).Gap(fullBottomGap).
		Children(func() {
			s.viewFullSpectrum(c, st)
			s.viewFullChapter(c, st)
			s.viewFullProgress(c, st)
			s.viewFullControls(c, st, g)
		})
}

// ---- 频谱 ----

// fullSpectrumColor 是频谱柱的颜色：偏好里挑了就用，没挑是 `tok.Full.Spectrum`（22%）。
func (s *Shell) fullSpectrumColor() ui.Color {
	if hex := s.pagePrefs().SpectrumColor; hex != "" {
		if col, ok := parseHexColor(hex); ok {
			return col
		}
	}
	return tok.Full.Spectrum
}

// viewFullSpectrum 画频谱。没开时这一行照样占 40（原型 `visibility: hidden`），只是不画。
func (s *Shell) viewFullSpectrum(c *ui.Context, st app.State) {
	area := ui.Box(c).Key("full-spectrum").FillWidth().Height(fullSpectrumH).Shrink(0).
		Role(ui.RoleImage).Label("频谱")
	if !s.pagePrefs().ShowSpectrum {
		// 没人看频谱：告诉 App 把 FFT 停掉。
		s.app.SetSpectrumWanted(false)
		return
	}
	bars := s.app.SpectrumBars(fullSpectrumBars, fullSpectrumH)
	color := s.fullSpectrumColor()
	// 这一帧画出来了：报一声「真的有人在看」。FFT 跟着这一声走。
	seen := s.presence.seen()
	s.app.SetSpectrumWanted(st.Playing && seen)
	area.Draw(func(p *ui.Painter, r ui.Rect) {
		paintSpectrum(p, r, bars, color)
		// 只在真的在放、而且用户看得到这一扇窗的时候要下一帧（票 #156）。
		if st.Playing && seen {
			p.AnimationFrame()
		}
	})
}

// ---- 章节名 ----

// chapterTitleAt 是播放到 pos 时所在章节的名字；没有章节 / 还没进第一章是空串。
func chapterTitleAt(chs []app.Chapter, pos time.Duration) string {
	title := ""
	for _, ch := range chs {
		if ch.At <= pos {
			title = ch.Title
		}
	}
	return title
}

// viewFullChapter 是章节名那一行：居中、等宽 12、最小高 17（没有章节也占着）。
func (s *Shell) viewFullChapter(c *ui.Context, st app.State) {
	ui.Row(c).Key("full-chap").FillWidth().Height(fullChapH).Shrink(0).Justify(ui.Center).AlignItems(ui.Center).
		Children(func() {
			if t := chapterTitleAt(st.Chapters, st.Position); t != "" {
				fullMono(c, t, scale.Size12, 500, 12*0.08, tok.Full.Text3).Ellipsis("…")
			}
		})
}

// ---- 进度 ----

// fullRangeLook 是满屏进度条的样子（原型 `.range`）：轨道 4，没走的 `line-strong`，已走的
// 平时是正文色、悬停换主色；旋钮 12，只在悬停 / 按下时画。
func fullRangeLook() sliderLook {
	return sliderLook{
		trackW: 4,
		knobW:  12,
		knobH:  12,
		face:   func(*ui.Theme) ui.Color { return tok.Full.Text },
		fill: func(t *ui.Theme) (ui.Color, ui.Color, ui.Color) {
			p := tok.OfFull(t)
			return p.Text, p.Accent, p.LineStrong
		},
	}
}

// viewFullProgress 是那一行进度：当前时间 | 轨道 | 总时长。
//
// ⚠️ 用的还是 `Pressed()` 而不是 `Dragging()`（理由见 playbar.go 的 viewProgress）。这一条另有
// 一份自己的进度句柄（`fullScrub`），不能和播放条上那条共用。滑块不是 `ui.Slider`，是
// `knobSlider`（见 knobslider.go）。
func (s *Shell) viewFullProgress(c *ui.Context, st app.State) {
	p := tok.OfFull(c.Theme())
	known := st.Duration > 0
	chs := st.Chapters
	read := func(text string) {
		typo.Num(typo.Mono(ui.Text(c, text))).FontSize(scale.Font(c, scale.Size11)).TextColor(p.Text3).
			Width(fullSeekReadW).TextAlign(ui.Center).SingleLine().Shrink(0)
	}
	ui.Row(c).Key("full-seek").FillWidth().Height(fullSeekH).Shrink(0).Gap(space.Px12).AlignItems(ui.Center).Children(func() {
		read(clockOrDash(st.Position))
		// 那一根线靠 `live` 自己走（见 `selfSliderLive`）：整棵树只在读数变了时重建。
		// ⚠️ 只在「在放」时给：暂停时位置不动，每帧重画是纯浪费。
		var live sliderLive
		if known && st.Playing {
			dur := float64(st.Duration)
			live = sliderLive{
				frac:    func() float32 { return float32(float64(s.app.PlayPosition()) / dur) },
				visible: s.presence.seen,
			}
		}
		sl := knobSliderLive(c, &s.fullScrub, 0, 1, fullRangeLook(), live).
			Grow(1).Height(fullSeekH).Label("播放进度").Disabled(!known)
		sl = s.withFullChapterDots(sl, chs, st.Duration)
		switch held := sl.Pressed(); {
		case held && !s.fullHeld:
			s.fullHeld = true
			// 记下按下那一帧的指针和位置：松手时拿它们分「点 / 拖」。
			s.fullPressX, _, _ = sl.PointerPosition()
			s.fullPressPos = st.Position
			s.app.ScrubStart()
		case !held && s.fullHeld:
			s.fullHeld = false
			s.app.ScrubEnd(snappedScrub(sl, s.fullScrub, s.fullPressX, s.fullPressPos, st.Duration, chs))
		case !s.fullHeld && known:
			s.fullScrub = float64(st.Position) / float64(st.Duration)
		}
		read(clockOrDash(st.Duration))
	})
}

// withFullChapterDots 在进度条上画章节点：12 的空心圆（`#0B0B0C` 底、2 宽的 60% 纸白描边）。
func (s *Shell) withFullChapterDots(sl ui.Element, chs []app.Chapter, dur time.Duration) ui.Element {
	fracs := chapterDots(chs, dur)
	if fracs == nil {
		return sl
	}
	const pad = fullChapDot / 2
	return sl.DrawOver(func(p *ui.Painter, r ui.Rect) {
		cy := r.Y + r.H/2
		ring := tok.Full.Text.Alpha(0.6)
		for _, f := range fracs {
			x := r.X + pad + (r.W-2*pad)*f
			p.Fill(ui.Rect{X: x - fullChapDot/2, Y: cy - fullChapDot/2, W: fullChapDot, H: fullChapDot}, ring, fullChapDot/2)
			in := float32(fullChapDot - 4)
			p.Fill(ui.Rect{X: x - in/2, Y: cy - in/2, W: in, H: in}, tok.Full.Base, in/2)
		}
	})
}

// ---- 控制区 ----

// fullCtrlColor 是控制区图标键的图形色（原型 `.full-ctrl .icon-btn { color: #d8d7d4 }`）。
var fullCtrlColor = ui.Hex("#D8D7D4")

// viewFullControls 是控制区：三组，两侧各占 (整宽 − 中间) / 2，中间那一组因此正好居中。
func (s *Shell) viewFullControls(c *ui.Context, st app.State, g fullGeo) {
	side := max((g.winW-2*g.padX-fullCtrlCenterW)/2, 0)
	ui.Row(c).Key("full-ctrl").FillWidth().Height(fullPlayHit).Shrink(0).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Key("full-ctrl-left").Width(side).Shrink(0).Gap(space.Px4).AlignItems(ui.Center).Children(func() {
			s.viewFullQuality(c, st)
			s.viewFullRate(c, st)
			s.viewFullDanmakuToggle(c)
		})
		ui.Row(c).Key("full-ctrl-mid").Width(fullCtrlCenterW).Shrink(0).Gap(24).AlignItems(ui.Center).Justify(ui.Center).
			Children(func() {
				s.viewFullTransport(c, st)
			})
		ui.Row(c).Key("full-ctrl-right").Width(side).Shrink(0).Gap(space.Px4).AlignItems(ui.Center).Justify(ui.End).
			Children(func() {
				s.viewFullEQ(c)
				s.viewFullPrefs(c)
				if s.withTip(c, pud.IconBtn(c, "full:mini", "mini", "切换到迷你播放器",
					pud.IconOpts{Color: fullCtrlColor, HoverColor: tok.Full.Text}), "切换到迷你播放器").Clicked() {
					s.toggleMiniPlayer()
				}
			})
	})
}

// viewFullTransport 是中间那一组：模式、上一首、播放键、下一首、睡眠。
func (s *Shell) viewFullTransport(c *ui.Context, st app.State) {
	mode := pudIconName(playModeIcon(st.PlayMode))
	if s.withTip(c, pud.IconBtn(c, "full:mode", mode, s.full.txt.modeLabel(st.PlayMode),
		pud.IconOpts{Color: fullCtrlColor, HoverColor: tok.Full.Text}), st.PlayMode.Desc()).Clicked() {
		s.app.CyclePlayMode()
		s.refreshMenuChecked(s.app.Snapshot())
	}
	prev := pud.IconBtn(c, "full:prev", "prev", "上一首", pud.IconOpts{Color: fullCtrlColor, HoverColor: tok.Full.Text, Size: 26})
	if !s.app.CanPrev() {
		prev = prev.Disabled(true)
	}
	if prev.Clicked() {
		s.app.Prev()
	}
	s.viewFullPlay(c, st)
	next := pud.IconBtn(c, "full:next", "next", "下一首", pud.IconOpts{Color: fullCtrlColor, HoverColor: tok.Full.Text, Size: 26})
	if !s.app.CanNext() {
		next = next.Disabled(true)
	}
	if next.Clicked() {
		s.app.Next()
	}
	s.viewFullSleep(c, st)
}

// viewFullPlay 是 64 的大播放键（原型 `.full-ctrl .play-btn`：纸白底、深色图形 26；悬停 1.05、
// 按下 0.94）。
func (s *Shell) viewFullPlay(c *ui.Context, st app.State) {
	disabled := !st.Playing && !st.HasTrack
	name := "play"
	if st.Playing {
		name = "pause"
	}
	b := ui.ButtonBase(c).Key("full:play").Label(transportLabel(st.Playing)).Disabled(disabled).
		Size(fullPlayHit, fullPlayHit).Radius(fullPlayHit / 2).Shrink(0).AlignItems(ui.Center).Justify(ui.Center).
		Background(tok.Full.PlayBg)
	to := float32(1)
	switch {
	case b.Pressed() && !disabled:
		to = 0.94
	case b.Hovered() && !disabled:
		to = 1.05
	}
	b = b.AnimatePose(ui.Pose{Scale: to}, ui.Animation{Duration: motion.Fast, Ease: motion.EaseOut})
	b.Children(func() {
		ui.Icon(c, pud.Icon(name)).FontSize(fullPlayGlyph).TextColor(tok.Full.PlayInk)
	})
	// 🔴 取流的时候它外面多绕一圈光（票 #271）：它是**加**上去的，按钮照旧是播放 / 暂停。
	b = s.withLoadHalo(c, b, st, fullPlayHit, !disabled)
	if s.withTip(c, b, transportLabel(st.Playing)).Clicked() {
		s.app.Toggle()
	}
}

// pudIconName 把 `Pud/xxx.svg` 的路径还原成 `pud.Icon` 要的名字。
func pudIconName(path string) string {
	return strings.TrimSuffix(strings.TrimPrefix(path, "Pud/"), ".svg")
}

// viewFullDanmakuToggle 是弹幕开关（原型 `fp-danmaku`，`aria-pressed`）。
//
// ⚠️ 原型里 `.icon-btn[aria-pressed="true"]` 的 accent-text 被后面同权重的 `.full-ctrl .icon-btn
// { color: #d8d7d4 }` 盖掉了，满屏里开 / 关**看不出区别**（参考截图里弹幕键是浅色）——照原型不画
// 开着的颜色；睡眠键同理。
func (s *Shell) viewFullDanmakuToggle(c *ui.Context) {
	on := s.pagePrefs().ShowDanmaku
	label := "显示弹幕"
	if s.withTip(c, pud.IconBtn(c, "full:danmaku", "danmaku", label,
		pud.IconOpts{Color: fullCtrlColor, HoverColor: tok.Full.Text}), label).Clicked() {
		s.setFullPrefs(func(p *app.FullPlayerPrefs) { p.ShowDanmaku = !on })
	}
}

// ---- 音质 / 倍速 / 睡眠 ----

// fullQualityLabel 是音质角标上的字：Hi-Res 写 `HI-RES`，其余是短版（无损 / 杜比 / 192K）。
func fullQualityLabel(q string) (label string, hires bool) {
	if strings.Contains(q, "Hi-Res") {
		return "HI-RES", true
	}
	return strings.ToUpper(bili.ChipLabel(q)), strings.Contains(q, "无损") || strings.HasPrefix(q, "杜比")
}

// viewFullQuality 是音质角标键 + 它开出来的「音质」弹层。本地文件没有音质，不画。
func (s *Shell) viewFullQuality(c *ui.Context, st app.State) {
	if st.Quality == "" {
		return
	}
	p := tok.OfFull(c.Theme())
	label, hires := fullQualityLabel(st.Quality)
	b := ui.ButtonBase(c).Key("full:quality").Label("音质：" + st.Quality).Height(28).PaddingX(space.Px10).
		Radius(tok.R4).AlignItems(ui.Center).Shrink(0)
	border, fg := p.LineStrong, p.Text2
	if hires {
		border, fg = p.Accent.Alpha(0.5), p.AccentText
	}
	b = b.Border(1, border).Transition(motion.Fade)
	b.Children(func() {
		fullMono(c, label, 10, 700, 0.4, fg)
	})
	s.fullPop(c, popQuality, b, "full-pop-quality", false, false, func() {
		pud.PopHeading(c, "音质")
		cur := app.BiliToPref
		for _, q := range qualityChoices {
			q := q
			if pud.PopOpt(c, "full:q:"+q.Label(), q.Label(), "", cur(q) == st.QualityPref).Clicked() {
				s.closeFullPops()
				if cur(q) != st.QualityPref {
					s.app.SetAudioQuality(q)
					qualitySwitchToast(s, q)
				}
			}
		}
	})
}

// fullRateText 是倍速的字：`1×` / `1.25×`。
func fullRateText(r float64) string {
	return strconv.FormatFloat(r, 'f', -1, 64) + "×"
}

// viewFullRate 是倍速键（宽 48 的等宽 12 号字）+「倍速」弹层。
func (s *Shell) viewFullRate(c *ui.Context, st app.State) {
	p := tok.OfFull(c.Theme())
	b := ui.ButtonBase(c).Key("full:rate").Label("倍速").Size(48, tok.IconHit).Radius(tok.R8).Shrink(0).
		AlignItems(ui.Center).Justify(ui.Center)
	if b.Hovered() {
		b = b.Background(p.Hover)
	}
	b = b.Transition(motion.Fade)
	b.Children(func() {
		fullMono(c, s.full.txt.rateText(st.Rate), scale.Size12, 400, 0, fullCtrlColor)
	})
	s.fullPop(c, popRate, b, "full-pop-rate", false, false, func() {
		pud.PopHeading(c, "倍速")
		for _, r := range app.RateSteps {
			r := r
			if pud.PopOpt(c, "full:r:"+fullRateText(r), fullRateText(r), "", r == st.Rate).Clicked() {
				s.closeFullPops()
				s.app.SetRate(r)
			}
		}
	})
}

// viewFullSleep 是睡眠键（月亮，定了之后图形是 accent-text）+「睡眠定时」弹层。
func (s *Shell) viewFullSleep(c *ui.Context, st app.State) {
	active := sleepActive(st)
	b := pud.IconBtn(c, "full:sleep", "moon", "睡眠定时", pud.IconOpts{Color: fullCtrlColor, HoverColor: tok.Full.Text})
	s.fullPop(c, popSleep, b, "full-pop-sleep", false, false, func() {
		head := "睡眠定时"
		if active {
			head += " · " + sleepRemaining(st)
		}
		pud.PopHeading(c, head)
		for _, d := range app.SleepTimerSteps {
			d := d
			if pud.PopOpt(c, "full:sl:"+d.String(), strconv.Itoa(int(d/time.Minute))+" 分钟后暂停", "", false).Clicked() {
				s.closeFullPops()
				s.app.SetSleepTimer(d)
			}
		}
		if pud.PopOpt(c, "full:sl:track", "放完这一首就停", "", st.SleepAfterTrack).Clicked() {
			s.closeFullPops()
			s.app.SleepAfterTrack()
		}
		if active {
			if pud.PopItem(c, "full:sl:off", "关闭定时", false).Clicked() {
				s.closeFullPops()
				s.app.CancelSleepTimer()
			}
		}
	})
}

// ---- 弹层的公共部分 ----

// 五个弹层的序号（`fullPlayer.popWas` / `popHeld` 的下标）。
const (
	popQuality = iota
	popRate
	popSleep
	popEQ
	popPrefs
)

// closeFullPops 收掉全部弹层（同一时刻只开一个）。
func (s *Shell) closeFullPops() {
	f := &s.full
	f.popQuality, f.popRate, f.popSleep, f.popEQ, f.popPrefs = false, false, false, false, false
}

// fullPopFlag 是第 which 个弹层的开关。
func (s *Shell) fullPopFlag(which int) *bool {
	f := &s.full
	return [...]*bool{&f.popQuality, &f.popRate, &f.popSleep, &f.popEQ, &f.popPrefs}[which]
}

// fullPop 把一个弹层挂在 anchor 上（原型 `openPop`：`.pop` 在按钮**上方 12**，左或右对齐）。
//
// which 是弹层序号（见上面的常量）；wide 是宽的那一档（最小宽 300，EQ / 偏好）；alignEnd 是
// 右对齐（右边那一组按钮的弹层向左展开，原型 `rect.right > winRect.right - 320 → right: 0`）。
//
// ⚠️ **「再按一下收起」要自己记**：弹层是「按在外面就关」的，按在它自己的按钮上也算外面，
// 按下那一帧它就被关掉了——松手那一下 `Clicked` 如果照 `!*flag` 翻，会把刚关掉的又开开。
// 所以按下那一帧记下「那时开着没有」，松手时取反的是它。
func (s *Shell) fullPop(c *ui.Context, which int, anchor ui.Element, key string, wide, alignEnd bool, content func()) {
	flag := s.fullPopFlag(which)
	f := &s.full
	if held := anchor.Pressed(); held && !f.popHeld[which] {
		f.popWas[which] = *flag
		f.popHeld[which] = true
	} else if !held {
		f.popHeld[which] = false
	}
	if anchor.Clicked() {
		was := f.popWas[which]
		s.closeFullPops()
		*flag = !was
	}
	ui.PopoverBase(c, anchor, flag, func(panel ui.Element) {
		p := tok.OfFull(c.Theme())
		at, self := ui.AnchorTopStart, ui.AnchorBottomStart
		if alignEnd {
			at, self = ui.AnchorTopEnd, ui.AnchorBottomEnd
		}
		minW := float32(tok.PopMinW)
		if wide {
			minW = 300
		}
		panel = panel.Key(key).AttachTo(anchor, at, self).Margin(0, 0, space.Px12, 0).
			MinWidth(minW).Padding(space.Px8).Radius(tok.R12).
			Background(p.Surface).Border(1, p.Line).Transition(motion.PopIn).Label(key)
		shadow.Pop(panel, c.Theme())
		ui.Column(c).Children(content)
	})
}
