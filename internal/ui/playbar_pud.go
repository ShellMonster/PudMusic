// 播放条的原型版排布（原型 `.bar` / `renderBar`，DESIGN.md §0′.T1）。
//
// 三列（`1fr 1.3fr 1fr`，缝 24，左右内边距 20，高 84，上边一条 `line`，底色 `side`）：
//
//   - 左：56 封面（悬停出展开键）、歌名 14 / 600 + 歌手 12、赞 / 收藏 / 评论三枚带计数的 36 图标键；
//   - 中：模式、上一首、播放键 44（正文色底）、下一首、睡眠定时，下面是进度条（两端等宽数字）；
//   - 右：音质角标、倍速、分集、下载、歌词、队列、静音、音量（96）。
//
// 每一枚键的状态逻辑（取流、重连、出错、重试、长按三连、睡眠、倍速……）都在原来的函数里没动，
// 这里只管长相和位置。弹层（音质 / 倍速 / 睡眠 / 下载）用 `pud.PopPanel` 的那套样子。
package ui

import (
	"fmt"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
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

// pbKey 是播放条上一枚 36×36 的图标键壳（原型 `.icon-btn`）：圆角 8，悬停 `hover` 底。
// 里面的图形由调用方画。key 必须稳定。
func pbKey(c *ui.Context, label string) ui.Element {
	b := ui.ButtonBase(c).Label(label).Size(tok.IconHit, tok.IconHit).Radius(tok.R8).
		AlignItems(ui.Center).Justify(ui.Center).Shrink(0)
	if b.Hovered() && !b.IsDisabled() {
		b = b.Background(tok.Of(c.Theme()).Hover)
	}
	return b.Transition(motion.Fade)
}

// pbIconColor 是图标键里图形的颜色：平时次要色，悬停正文色，「开着」accent-text。
func pbIconColor(c *ui.Context, hovered, on bool) ui.Color {
	p := tok.Of(c.Theme())
	switch {
	case on:
		return p.AccentText
	case hovered:
		return p.Text
	}
	return p.Text2
}

// pbPopover 开一层播放条上的弹层（原型 `.pop`）：锚在按钮上，默认挂在按钮下面，放不下时框架
// 会翻到上面；播放条在窗口最下面，所以实际总是在按钮上方 12。点外面 / Esc 自动关。
func (s *Shell) pbPopover(c *ui.Context, anchor ui.Element, open *bool, name string, body func()) {
	if !*open {
		return
	}
	c.Key("pb-pop:" + name)
	ui.PopoverBase(c, anchor, open, func(panel ui.Element) {
		p := tok.Of(c.Theme())
		shadow.Pop(panel.
			AttachTo(anchor, ui.AnchorBottomEnd, ui.AnchorTopEnd).
			Margin(space.Px12, space.None, space.None, space.None).
			MinWidth(tok.PopMinW).Padding(space.Px8).Radius(tok.R12).
			Background(p.Surface).Border(1, p.Line).Transition(motion.PopIn).
			Label(name), c.Theme()).Children(body)
	})
}

// viewPlayBar 是整条播放条。
func (s *Shell) viewPlayBar(c *ui.Context, st app.State) {
	s.builds.playbar++
	p := tok.Of(c.Theme())

	// 上边那条 1 像素的线用叠画的（不占高），封面才会落在整数像素上。
	bar := ui.Column(c).Key("playbar").Height(tok.BarH).PaddingX(space.Px20).
		Background(p.Side).DrawOver(func(pp *ui.Painter, r ui.Rect) {
		pp.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: 1}, p.Line, 0)
	})
	bar.Children(func() {
		ui.Grid(c).Fill().ColumnTracks(ui.Fr(1), ui.Fr(1.3), ui.Fr(1)).GapX(space.Px24).
			RowTracks(ui.Fixed(tok.BarH)).Children(func() {
			ui.Row(c).Gap(space.Px12).AlignItems(ui.Center).MinWidth(0).Children(func() {
				s.viewCover(c, st)
				s.viewTrackText(c, st, c.Theme())
				s.viewTrackActions(c, st)
			})
			ui.Column(c).Gap(space.Px4).AlignItems(ui.Center).Justify(ui.Center).
				MinWidth(0).PassThrough().Children(func() {
				s.viewTransport(c, st)
				s.viewProgress(c, st)
			})
			s.viewPlayBarRight(c, st)
		})
	})
}

// viewPlayBarRight 是右列：音质 / 倍速 / 分集 / 下载 / 歌词 / 队列 / 静音 / 音量，靠右、缝 4。
func (s *Shell) viewPlayBarRight(c *ui.Context, st app.State) {
	// 右列靠右、内容比列宽时向左溢出（原型 `.bar-right { justify-content: flex-end }`
	// 的效果，中间那一列因此始终在窗口正中）：绝对定位贴右。
	ui.Row(c).Key("playbar-right").Gap(space.Px4).AlignItems(ui.Center).
		Absolute().Right(0).Top(0).Bottom(0).
		Children(func() {
			s.viewQualityBadge(c, st, c.Theme())
			s.viewRate(c, st)
			s.viewPartButton(c, st)
			if st.HasTrack && st.Source != app.SourceLocal {
				s.viewDownloadButton(c, st)
			}
			s.viewLyricsKey(c, st)
			if s.withTip(c, pud.IconBtn(c, "pb-queue", "queue", "播放列表", pud.IconOpts{On: s.drawerOpen}),
				drawerTip(s.drawerOpen)).Clicked() {
				s.drawerOpen = !s.drawerOpen
			}
			s.viewVolumeButton(c, st)
		})
}

// viewTransport 是中列上面那一排：模式、上一首、播放键、下一首、睡眠定时（缝 12）。
func (s *Shell) viewTransport(c *ui.Context, st app.State) {
	ui.Row(c).Gap(space.Px12).AlignItems(ui.Center).Children(func() {
		s.viewPlayMode(c, st)
		if pud.IconBtn(c, "pb-prev", "prev", "上一首", pud.IconOpts{Size: 22}).
			Disabled(!s.app.CanPrev()).Clicked() {
			s.app.Prev()
		}
		s.viewPlayKey(c, st)
		if pud.IconBtn(c, "pb-next", "next", "下一首", pud.IconOpts{Size: 22}).
			Disabled(!s.app.CanNext()).Clicked() {
			s.app.Next()
		}
		s.viewSleep(c, st)
	})
}

// viewPlayKey 是播放键（原型 `.play-btn`：44 的圆，正文色底、页面底色的图形 20，
// 悬停放大到 1.05、按下 0.94）。出错时换成重试 / 错误键（见 `viewErrorPlayKey`）。
func (s *Shell) viewPlayKey(c *ui.Context, st app.State) {
	if st.Err != "" {
		s.viewErrorPlayKey(c, st)
		return
	}
	p := tok.Of(c.Theme())
	label, name := "播放", "play"
	if st.Playing {
		label, name = "暂停", "pause"
	}
	b := ui.ButtonBase(c).Label(label).Disabled(!st.HasTrack).Size(tok.PlayBtn, tok.PlayBtn).
		Radius(tok.PlayBtn / 2).Background(p.Text).AlignItems(ui.Center).Justify(ui.Center).Shrink(0)
	if !st.HasTrack {
		b = b.Opacity(0.35)
	}
	scaleTo := float32(1)
	switch {
	case b.Pressed() && st.HasTrack:
		scaleTo = 0.94
	case b.Hovered() && st.HasTrack:
		scaleTo = 1.05
	}
	b = b.AnimatePose(ui.Pose{Scale: scaleTo}, ui.Animation{Duration: motion.Fast, Ease: motion.EaseOut})
	b.Children(func() {
		ui.Icon(c, pud.Icon(name)).FontSize(20).TextColor(p.Bg)
	})
	b = s.withLoadHalo(c, b, st, tok.PlayBtn, st.HasTrack)
	if b.Clicked() {
		s.app.Toggle()
	}
}

// viewLyricsKey 是右列的「歌词」键：和全屏播放器的歌词是同一个入口（点了进全屏）。
func (s *Shell) viewLyricsKey(c *ui.Context, st app.State) {
	if !st.HasTrack {
		return
	}
	if s.withTip(c, pud.IconBtn(c, "pb-lyrics", "lyrics", "歌词", pud.IconOpts{}), "歌词").Clicked() {
		s.openFullPlayer()
	}
}

// 播放条右列的「音质」角标（原型 `.q-badge`）：高 24、左右 8、11 号等宽 700，Hi-Res 档主色描边 + accent-text。
func (s *Shell) viewQualityBadge(c *ui.Context, st app.State, t *ui.Theme) {
	if st.Quality == "" {
		return
	}
	p := tok.Of(t)
	label := bili.ChipLabel(st.Quality)
	hires := label == "无损" || label == "杜比"
	text := map[string]string{"无损": "HI-RES", "杜比": "DOLBY"}[label]
	if text == "" {
		text = label
	}
	border, fg := p.LineStrong, p.Text2
	if hires {
		border, fg = p.Accent.Alpha(0.5), p.AccentText
	}
	b := ui.ButtonBase(c).Key("pb-quality").Label("音质："+st.Quality).Height(24).PaddingX(space.Px8).
		Radius(tok.R4).Border(1, border).AlignItems(ui.Center).Shrink(0)
	b.Children(func() {
		typo.Mono(ui.Text(c, text)).FontSize(11).FontWeight(700).LetterSpacing(0.44).TextColor(fg).SingleLine()
	})
	b = s.withTip(c, b, "音质："+st.Quality)
	if b.Clicked() {
		s.qualityOpen = !s.qualityOpen
	}
	s.pbPopover(c, b, &s.qualityOpen, "音质", func() {
		pud.PopHeading(c, "音质")
		for _, q := range qualityChoices {
			on := app.BiliToPref(q) == st.QualityPref
			if pud.PopOpt(c, "pb-q:"+q.Label(), q.Label(), "", on).Clicked() {
				s.qualityOpen = false
				if !on {
					s.app.SetAudioQuality(q)
					qualitySwitchToast(s, q)
				}
			}
		}
	})
}

// viewRate 是倍速键（原型：44 宽的等宽 12 号 `1×`）。
func (s *Shell) viewRate(c *ui.Context, st app.State) {
	p := tok.Of(c.Theme())
	b := ui.ButtonBase(c).Key("pb-rate").Label("播放速率").Size(44, tok.IconHit).Radius(tok.R8).
		AlignItems(ui.Center).Justify(ui.Center).Shrink(0)
	fg := p.Text2
	if b.Hovered() {
		b = b.Background(p.Hover)
		fg = p.Text
	}
	b.Children(func() {
		typo.Mono(ui.Text(c, rateLabel(st.Rate))).FontSize(12).TextColor(fg).SingleLine()
	})
	if b.Clicked() {
		s.rateOpen = !s.rateOpen
	}
	s.pbPopover(c, b, &s.rateOpen, "倍速", func() {
		pud.PopHeading(c, "倍速")
		for _, r := range app.RateSteps {
			on := r == st.Rate
			if pud.PopOpt(c, "pb-rate:"+rateLabel(r), rateLabel(r), "", on).Label("播放速率 " + rateLabel(r)).Clicked() {
				s.rateOpen = false
				s.app.SetRate(r)
			}
		}
	})
}

// viewSleep 是睡眠定时键（月亮；定时开着时图形 accent-text）和它的弹层。
func (s *Shell) viewSleep(c *ui.Context, st app.State) {
	b := s.withTip(c, pud.IconBtn(c, "pb-sleep", "moon", "睡眠定时", pud.IconOpts{On: sleepActive(st)}), "")
	if b.Clicked() {
		s.sleepOpen = !s.sleepOpen
	}
	s.pbPopover(c, b, &s.sleepOpen, "睡眠定时", func() {
		title := "睡眠定时"
		if sleepActive(st) {
			title += " · " + sleepRemaining(st)
		}
		pud.PopHeading(c, title)
		for _, d := range app.SleepTimerSteps {
			d := d
			label := fmt.Sprintf("%d 分钟后暂停", int(d/time.Minute))
			if pud.PopOpt(c, "pb-sleep:"+label, label, "", false).Label("睡眠定时 " + label).Clicked() {
				s.sleepOpen = false
				s.app.SetSleepTimer(d)
			}
		}
		if pud.PopOpt(c, "pb-sleep:track", "放完这一首就暂停", "", st.SleepAfterTrack).Label("睡眠定时 放完这一首就停").Clicked() {
			s.sleepOpen = false
			s.app.SleepAfterTrack()
		}
		if sleepActive(st) {
			if pud.PopItem(c, "pb-sleep:off", "关闭定时", true).Label("睡眠定时 关闭定时").Clicked() {
				s.sleepOpen = false
				s.app.CancelSleepTimer()
			}
		}
	})
}

// viewDownloadButton 是下载键和它的弹层（原型 `dl`：下载音频 / 下载视频）。
func (s *Shell) viewDownloadButton(c *ui.Context, st app.State) {
	b := s.withTip(c, pud.IconBtn(c, "pb-download", "download", "下载当前歌曲", pud.IconOpts{}), "")
	if b.Clicked() {
		s.dlOpen = !s.dlOpen
	}
	s.pbPopover(c, b, &s.dlOpen, "下载", func() {
		pud.PopHeading(c, "下载当前歌曲")
		it, ok := s.app.PlayingItem()
		if pud.PopItem(c, "pb-dl:audio", "下载音频", false).Clicked() {
			s.dlOpen = false
			if !ok {
				s.addToast("这一首还没有取回来，下载不了", notice.Warning)
			} else {
				s.downloadLater(it, false, "下载音频")
			}
		}
		if pud.PopItem(c, "pb-dl:video", "下载视频", false).Clicked() {
			s.dlOpen = false
			if !ok {
				s.addToast("这一首还没有取回来，下载不了", notice.Warning)
			} else {
				s.downloadLater(it, true, "下载视频")
			}
		}
	})
}

// pbScaleFont 是播放条上文字的字号（过系统缩放）。
func pbScaleFont(c *ui.Context, size float32) float32 { return scale.Font(c, size) }
