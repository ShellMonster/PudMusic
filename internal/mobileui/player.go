package mobileui

import (
	"image"
	"sort"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	desktop "github.com/ShellMonster/PudMusic/internal/ui"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/netimage"
)

type playerSheetKind uint8

const (
	sheetNone playerSheetKind = iota
	sheetQueue
	sheetTrack
	sheetOptions
	sheetSpeed
	sheetQuality
	sheetSleep
	sheetParts
	sheetChapters
	sheetComments
	sheetReplies
	sheetClearQueue
	sheetCover
	sheetVideoQuality
	sheetLyricsOptions
	sheetPrefs
	sheetEQ
	sheetLyricsSearch
	sheetTrackFolders
	sheetDownload
)

type playerView struct {
	visible, lyrics, cinema   bool
	sheet                     playerSheetKind
	parents                   []playerSheetKind
	target                    app.Item
	seek                      float64
	bitmap                    *ui.Bitmap
	bitmapWidth, bitmapHeight int
	danmaku                   desktop.VideoDanmaku
	videoPosition             time.Duration
	videoRefreshUntil         time.Time
	comments                  playerCommentView
	folders                   *playerFolderView
	downloadItem              app.Item
	lyricsScroll              ui.ScrollState
	lyricsKey                 string
	lyricsLine                int
	lyricsManualUntil         time.Time
	hideTranslation           bool
	lyricsQuery               string
	lyricsSource              int
	queueExpanded             bool
}

func (p *playerView) show(kind playerSheetKind) {
	if kind == sheetQueue && p.sheet != sheetQueue {
		p.queueExpanded = false
	}
	if p.sheet != sheetNone && p.sheet != kind {
		p.parents = append(p.parents, p.sheet)
	}
	p.sheet = kind
}

func (p *playerView) back() bool {
	switch {
	case p.sheet != sheetNone:
		p.sheet = sheetNone
		if count := len(p.parents); count != 0 {
			p.sheet = p.parents[count-1]
			p.parents = p.parents[:count-1]
		}
	case p.cinema:
		p.cinema = false
	case p.visible:
		p.visible = false
		p.bitmap = nil
		p.danmaku.Reset()
	default:
		return false
	}
	return true
}

func (s *shell) miniPlayer(c *ui.Context, state app.State) {
	item, _ := s.app.Queue().Current()
	palette := phonePalette(c)
	title := state.Title
	if item.Title != "" && (title == "" || item.Source == app.SourceLocal) {
		title = item.Title
	}
	if title == "" {
		title = "正在加载"
	}
	ui.Column(c.Key("mini-player")).Margin(0, 12, 3, 12).Height(60).Shrink(0).Radius(16).Clip().Background(palette.fill).Children(func() {
		ui.Box(c).FillWidth().Height(2).Draw(func(p *ui.Painter, bounds ui.Rect) {
			if state.Duration > 0 {
				bounds.W *= float32(float64(state.Position) / float64(state.Duration))
				p.Fill(bounds, palette.accent, 0)
			}
		})
		ui.Row(c).Grow(1).Basis(0).FillWidth().PaddingX(8).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Grow(1).Basis(0).MinHeight(touchSize()).Gap(10).AlignItems(ui.Center).Role(ui.RoleButton).
				Label("打开满屏播放器：" + title).Children(func() {
				cover(c, state.CoverURL, 40, 8)
				trackLabel(c, title, item.Artist, 13, false)
			}).OnClick(func() { s.player.visible = true })
			icon, label := playGlyph(state)
			iconButton(c, icon, label).OnClick(s.app.Toggle)
			iconButton(c, "queue", "播放列表").OnClick(func() { s.player.show(sheetQueue) })
		})
	})
}

func playGlyph(state app.State) (string, string) {
	if state.Playing {
		return "pause", "暂停"
	}
	return "play", "播放"
}

func playModeGlyph(mode app.PlayMode) string {
	switch mode {
	case app.PlayModeSequence:
		return "order"
	case app.PlayModeRandom:
		return "shuffle"
	case app.PlayModeSingle:
		return "repeat1"
	default:
		return "repeat"
	}
}

func (s *shell) fullPlayer(c *ui.Context, state app.State) {
	prefs := s.app.FullPlayerPrefs()
	background := phonePalette(c).fill
	if !prefs.ShowBlurredBackground && prefs.BackgroundColor != "" {
		background = ui.Hex(prefs.BackgroundColor)
	}
	width, height := c.Size()
	ui.Box(c.Key("full-player")).Fill().Background(background).Clip().Children(func() {
		if prefs.ShowBlurredBackground && state.CoverURL != "" {
			netimage.Image(c, imgcache.FormatURLProtocol(state.CoverURL), netimage.Options{MaxSide: 120, Placeholder: background}).
				Absolute().Top(-32).Bottom(-32).Left(-32).Right(-32).Blur(38).Opacity(.24)
		}
		ui.Column(c).Fill().Children(func() { s.playerLayout(c, state, prefs, width, height) })
	})
}

func (s *shell) playerLayout(c *ui.Context, state app.State, prefs app.FullPlayerPrefs, width, height float32) {
	ui.Row(c).FillWidth().Height(touchSize()).PaddingX(8).AlignItems(ui.Center).Children(func() {
		iconButton(c, "down", "收起满屏播放器").OnClick(func() { s.back() })
		ui.Box(c).Grow(1).Basis(0).Center().Children(func() { ui.Text(c, "正在播放").FontSize(13).FontWeight(600) })
		iconButton(c, "more", "更多播放设置").OnClick(func() { s.player.show(sheetOptions) })
	})
	if s.player.cinema && s.app.VideoPhase() != app.VideoOff {
		s.playerVideo(c, state, prefs, width, max(100, height-110))
		return
	}
	if width > height {
		ui.Row(c).Grow(1).Basis(0).FillWidth().AlignItems(ui.Center).Children(func() {
			s.swipeablePlayerStage(c, state, prefs, width*.48, max(110, height-100))
			ui.Column(c).Grow(1).Basis(0).Children(func() { s.playerControls(c, state, prefs) })
		})
		return
	}
	ui.Column(c).Grow(1).Basis(0).FillWidth().AlignItems(ui.Center).Padding(max(0, min(38, height-620)), 0, 0, 0).Children(func() {
		s.swipeablePlayerStage(c, state, prefs, width, max(110, height-344))
	})
	s.playerControls(c, state, prefs)
}

func (s *shell) swipeablePlayerStage(c *ui.Context, state app.State, prefs app.FullPlayerPrefs, width, height float32) {
	stage := ui.Box(c).Width(width).Center().Children(func() { s.playerStage(c, state, prefs, width, height) })
	if !prefs.ShowLyrics || s.app.VideoPhase() != app.VideoOff {
		return
	}
	dx, dy, completed := completedSwipe(stage)
	if !completed || abs32(dx) < max(48, 1.5*abs32(dy)) {
		return
	}
	if toLyrics := dx < 0; toLyrics != s.player.lyrics {
		s.player.lyrics = toLyrics
		c.AnimationFrame()
	}
}

func abs32(number float32) float32 { return max(number, -number) }

func (s *shell) playerStage(c *ui.Context, state app.State, prefs app.FullPlayerPrefs, width, height float32) {
	if s.app.VideoPhase() != app.VideoOff {
		videoWidth := min(width-32, height*16/9)
		s.playerVideo(c, state, prefs, videoWidth, videoWidth*9/16)
		return
	}
	lyric := s.app.Lyrics()
	position := state.Position - time.Duration(lyric.OffsetMS)*time.Millisecond
	index := sort.Search(len(lyric.Rows), func(index int) bool { return lyric.Rows[index].At > position }) - 1
	if s.player.lyrics && prefs.ShowLyrics {
		s.playerLyrics(c, lyric, index, width, height, prefs.LyricsColor)
		return
	}
	ui.Column(c).Width(width).MaxHeight(height).Center().Gap(9).Children(func() {
		if prefs.ShowCover {
			size := max(80, min(width*.82, 329, height-65))
			ui.Box(c).Role(ui.RoleButton).Label("当前封面的操作").Size(size, size).Children(func() {
				cover(c, state.CoverURL, size, 16)
			}).OnClick(func() { s.player.show(sheetCover) })
		}
		if prefs.ShowSpectrum {
			color := phonePalette(c).muted
			if prefs.SpectrumColor != "" {
				color = ui.Hex(prefs.SpectrumColor)
			}
			ui.Box(c).Width(min(width*.75, 284)).Height(22).Draw(func(p *ui.Painter, bounds ui.Rect) {
				for index, height := range s.app.SpectrumBars(48, bounds.H) {
					step := bounds.W / 48
					p.Fill(ui.Rect{X: bounds.X + float32(index)*step, Y: bounds.Y + (bounds.H-height)/2, W: step / 2, H: height}, color, 1)
				}
				if state.Playing {
					p.AnimationFrame()
				}
			})
		}
		if prefs.ShowLyrics {
			line := "暂无歌词"
			if lyric.Loading && !lyric.HasLyrics {
				line = "歌词加载中…"
			} else if index >= 0 {
				line = lyric.Rows[index].Text
			}
			ui.Box(c).Width(width - 32).MinHeight(32).Center().Role(ui.RoleButton).Label("展开歌词").Children(func() {
				ui.Text(c, line).FontSize(13).MaxLines(1).Ellipsis("…").TextColor(phonePalette(c).muted)
			}).OnClick(func() { s.player.lyrics = true })
		}
	})
}

func (s *shell) playerControls(c *ui.Context, state app.State, prefs app.FullPlayerPrefs) {
	palette := phonePalette(c)
	item, _ := s.app.Queue().Current()
	width, height := c.Size()
	side := float32(16)
	if width <= height {
		side = max(side, (width-330)/2)
	}
	ui.Column(c).FillWidth().Padding(8, side, 12, side).Gap(8).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Basis(0).Gap(3).Children(func() {
				title := state.Title
				if item.Title != "" && (title == "" || item.Source == app.SourceLocal) {
					title = item.Title
				}
				ui.Text(c, title).FontSize(21).FontWeight(650).MaxLines(2).Ellipsis("…")
				if item.Artist != "" {
					ui.Text(c, item.Artist).FontSize(13).TextColor(palette.muted).MaxLines(1).Ellipsis("…")
				}
			})
			if s.app.Likeable() {
				liked := s.app.LikeState().Liked
				icon := "heart"
				if liked {
					icon = "heart-on"
				}
				iconButton(c, icon, "喜欢").AccessSelected(liked).OnClick(func() { s.app.SetLike(!liked, s.playerResult("")) })
			}
		})
		if state.Err != "" {
			ui.Text(c, state.Err).FontSize(12).TextColor(palette.accent).MaxLines(2)
		}
		s.playerProgress(c, state)
		ui.Row(c).FillWidth().Height(66).Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
			mode := s.app.PlayMode()
			iconButton(c, playModeGlyph(mode), mode.Desc()).OnClick(s.app.CyclePlayMode)
			iconButton(c, "prev", "上一首").OnClick(s.app.Prev)
			icon, label := playGlyph(state)
			ui.Box(c).Size(62, 62).Radius(31).Center().Background(palette.text).Role(ui.RoleButton).Label(label).Children(func() {
				glyph(c, icon, 26, palette.surface)
			}).OnClick(s.app.Toggle)
			iconButton(c, "next", "下一首").OnClick(s.app.Next)
			iconButton(c, "queue", "播放列表").OnClick(func() { s.player.show(sheetQueue) })
		})
		ui.Row(c).FillWidth().Center().Gap(8).Children(func() {
			if s.app.CommentsWorthAsking() {
				playerTool(c, "comment", "评论").OnClick(func() {
					if s.app.LoggedIn() {
						s.app.RefreshComments()
					} else {
						s.app.EnsureComments()
					}
					s.player.show(sheetComments)
				})
			}
			if prefs.ShowLyrics {
				label := "歌词"
				if s.player.lyrics {
					label = "封面"
				}
				playerTool(c, "lyrics", label).OnClick(func() { s.app.StopVideo(); s.player.lyrics = !s.player.lyrics })
			}
			if item.Source == app.SourceOnline {
				playerTool(c, "eq", s.app.AudioQuality().Label()).OnClick(func() { s.player.show(sheetQuality) })
			}
		})
	})
}

func (s *shell) playerProgress(c *ui.Context, state app.State) {
	if state.Duration > 0 {
		s.player.seek = float64(state.Position) / float64(state.Duration)
	}
	ui.Slider(c, &s.player.seek, 0, 1).FillWidth().Height(26).Label("播放进度").Disabled(state.Duration <= 0).OnChange(func() {
		s.app.SeekTo(time.Duration(s.player.seek * float64(state.Duration)))
	})
	ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
		muted := phonePalette(c).muted
		ui.Text(c, timeLabel(state.Position)).FontSize(12).TextColor(muted)
		if len(state.Chapters) > 0 {
			playerTool(c, "parts", "章节").OnClick(func() { s.player.show(sheetChapters) })
		}
		ui.Text(c, timeLabel(state.Duration)).FontSize(12).TextColor(muted)
	})
}

func playerTool(c *ui.Context, icon, label string) ui.Element {
	return ui.Row(c).MinHeight(touchSize()).PaddingX(6).AlignItems(ui.Center).Gap(5).Role(ui.RoleButton).Label(label).Children(func() {
		color := phonePalette(c).muted
		glyph(c, icon, 15, color)
		ui.Text(c, label).FontSize(12).TextColor(color)
	})
}

func (s *shell) playerResult(message string) func(error) {
	return func(err error) {
		s.update(func() {
			if err != nil {
				s.report(err)
			} else if message != "" {
				s.notice = message
			}
		})
	}
}

func (s *shell) playerVideo(c *ui.Context, state app.State, prefs app.FullPlayerPrefs, width, height float32) {
	p := &s.player
	if p.videoPosition != state.Position {
		p.videoPosition, p.videoRefreshUntil = state.Position, c.Now().Add(time.Second)
	}
	ui.Column(c).Width(width).Center().Children(func() {
		if err := s.app.VideoError(); err != nil {
			ui.Text(c, err.Error()).FontSize(13).MaxLines(2)
		} else {
			video := ui.Box(c).Size(width, height).Radius(12).Role(ui.RoleButton).Label("切换视频全屏").Draw(func(painter *ui.Painter, bounds ui.Rect) {
				if frame, ok := s.app.VideoFrame(s.app.PlayPosition()); ok {
					pixels := &image.RGBA{Pix: frame.Pix, Stride: frame.W * 4, Rect: image.Rect(0, 0, frame.W, frame.H)}
					if p.bitmap == nil || p.bitmapWidth != frame.W || p.bitmapHeight != frame.H {
						p.bitmap, p.bitmapWidth, p.bitmapHeight = ui.NewBitmap(pixels), frame.W, frame.H
					} else {
						p.bitmap.Update(pixels)
					}
				}
				painter.Fill(bounds, ui.RGB(0, 0, 0), 12)
				if p.bitmap != nil {
					painter.Clip(bounds, 12, func() { painter.Image(p.bitmap, bounds, ui.Contain) })
				}
				if state.Playing || p.bitmap == nil || painter.Now().Before(p.videoRefreshUntil) {
					painter.AnimationFrame()
				}
			}).OnClick(func() { p.cinema = !p.cinema })
			if prefs.ShowDanmaku {
				p.danmaku.Draw(c, video, state, s.app.Danmaku().Items, width, height)
			} else {
				p.danmaku.Reset()
			}
		}
		ui.Row(c).Center().Children(func() {
			playerTool(c, "video", "视频画质").OnClick(func() { p.show(sheetVideoQuality) })
			playerTool(c, "x", "关闭视频").OnClick(func() {
				p.bitmap, p.cinema = nil, false
				p.danmaku.Reset()
				s.app.StopVideo()
			})
		})
	})
}
