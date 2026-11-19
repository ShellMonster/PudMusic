package mobileui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/audio"
	"github.com/ShellMonster/PudMusic/internal/service/lyrics"
	"github.com/egoist/mygo/ui"
)

func (s *shell) playerLyrics(c *ui.Context, lyric app.LyricsSnapshot, index int, width, height float32, color string) {
	p := &s.player
	if p.lyricsKey != lyric.Key {
		p.lyricsKey, p.lyricsLine = lyric.Key, -2
		p.lyricsScroll = ui.ScrollState{}
		p.lyricsManualUntil = time.Time{}
	}
	palette := phonePalette(c)
	ink := palette.accent
	if color != "" {
		ink = ui.Hex(color)
	}
	rowHeight := max(56, lyric.FontSize+29)
	ui.Column(c).Width(width).Height(height).PaddingX(24).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "歌词").FontSize(13).Grow(1).TextColor(palette.muted)
			iconButton(c, "settings", "歌词设置").OnClick(func() { p.show(sheetLyricsOptions) })
		})
		if !lyric.HasLyrics {
			label := "暂无歌词"
			if lyric.Loading {
				label = "歌词加载中…"
			}
			ui.Box(c).Grow(1).FillWidth().Center().Children(func() { ui.Text(c, label).FontSize(13) })
			return
		}
		scroll := ui.Scroll(c).Grow(1).Basis(0).FillWidth().TrackScroll(&p.lyricsScroll)
		if scroll.Dragging() {
			p.lyricsManualUntil = c.Now().Add(4 * time.Second)
			p.lyricsLine = -2
		}
		if index != p.lyricsLine && !c.Now().Before(p.lyricsManualUntil) {
			p.lyricsLine = index
			p.lyricsScroll.ScrollTo(0, max(0, float32(index)*rowHeight-height*.30), ui.Smooth)
		}
		scroll.Children(func() {
			for row, line := range lyric.Rows {
				textColor := palette.muted
				if row == index {
					textColor = ink
				}
				ui.Column(c.Key(row)).FillWidth().Height(rowHeight).Justify(ui.Center).Gap(3).Role(ui.RoleButton).Label(line.Text).AccessSelected(row == index).Children(func() {
					ui.Text(c, line.Text).FontSize(max(14, lyric.FontSize)).FontWeight(550).TextColor(textColor).MaxLines(2)
					if !p.hideTranslation && line.Translation != "" {
						ui.Text(c, line.Translation).FontSize(12).TextColor(textColor).MaxLines(1).Ellipsis("…")
					}
				}).OnClick(func() { s.app.SeekTo(line.At + time.Duration(lyric.OffsetMS)*time.Millisecond) })
			}
		})
	})
}

func (s *shell) playerLyricsOptions(c *ui.Context) {
	lyric := s.app.Lyrics()
	if settingSwitch(c, "歌词翻译", !s.player.hideTranslation, false) {
		s.player.hideTranslation = !s.player.hideTranslation
	}
	settingRow(c, "lyric-offset", "歌词偏移", fmt.Sprintf("%+.1f 秒", float64(lyric.OffsetMS)/1000), func() {
		iconButton(c, "minus", "歌词提前 0.5 秒").OnClick(func() { s.app.SetLyricsOffset(lyric.OffsetMS - 500) })
		iconButton(c, "plus", "歌词延后 0.5 秒").OnClick(func() { s.app.SetLyricsOffset(lyric.OffsetMS + 500) })
	})
	settingRow(c, "lyric-size", "歌词字号", fmt.Sprintf("%.0f", lyric.FontSize), func() {
		iconButton(c, "minus", "减小歌词字号").OnClick(func() { s.app.SetLyricsFontSize(lyric.FontSize - 2) })
		iconButton(c, "plus", "增大歌词字号").OnClick(func() { s.app.SetLyricsFontSize(lyric.FontSize + 2) })
	})
	sheetAction(c, "search", "找歌词", "", false, func() { s.player.show(sheetLyricsSearch) })
}

func (s *shell) playerPreferences(c *ui.Context) {
	prefs := s.app.FullPlayerPrefs()
	for _, row := range []struct {
		title string
		value *bool
	}{
		{"显示歌词", &prefs.ShowLyrics}, {"显示频谱图", &prefs.ShowSpectrum}, {"显示封面", &prefs.ShowCover},
		{"显示虚化背景", &prefs.ShowBlurredBackground}, {"显示弹幕", &prefs.ShowDanmaku},
	} {
		settingRow(c, row.title, row.title, "", func() {
			if settingSwitch(c, row.title, *row.value, false) {
				*row.value = !*row.value
				s.app.SetFullPlayerPrefs(prefs)
			}
		})
	}
	for _, group := range []struct {
		title   string
		value   *string
		color   string
		visible bool
	}{
		{"歌词颜色", &prefs.LyricsColor, "#b9c8ff", prefs.ShowLyrics},
		{"频谱颜色", &prefs.SpectrumColor, "#ff5d3d", prefs.ShowSpectrum},
		{"背景颜色", &prefs.BackgroundColor, "#101826", !prefs.ShowBlurredBackground},
	} {
		if group.visible {
			settingSelect(c, group.title, *group.value, []settingChoice[string]{{value: "", label: "默认"}, {value: group.color, label: "自选色"}}, false, func(color string) {
				*group.value = color
				s.app.SetFullPlayerPrefs(prefs)
			})
		}
	}
}

func (s *shell) playerEqualizer(c *ui.Context) {
	settings := s.app.SettingsSnapshot()
	save := func(change func(*app.Settings)) { _, err := s.app.UpdateSettings(change); s.report(err) }
	for _, preset := range append(slices.Clone(audio.EQPresets), settings.EQCustomPresets...) {
		custom := !slices.ContainsFunc(audio.EQPresets, func(item audio.EQPreset) bool { return item.ID == preset.ID })
		selected := settings.EQCustomOn && custom && settings.EQCustomGains == preset.Gains ||
			!settings.EQCustomOn && !custom && audio.EQPresetByID(settings.EQPreset).ID == preset.ID
		sheetAction(c, "eq", preset.Name, "", selected, func() {
			save(func(next *app.Settings) {
				next.EQCustomOn = custom
				if custom {
					next.EQCustomGains = preset.Gains
				} else {
					next.EQPreset = preset.ID
				}
			})
		})
	}
	gains := settings.EQGains()
	for band, frequency := range audio.EQBands {
		gain := gains[band]
		settingRow(c, fmt.Sprintf("eq:%d", band), fmt.Sprintf("%g Hz", frequency), fmt.Sprintf("%+.0f dB", gain), func() {
			ui.Slider(c, &gain, -12, 12).Width(140).Label(fmt.Sprintf("%g Hz 频段", frequency)).OnChange(func() {
				gains[band] = gain
				save(func(next *app.Settings) { next.EQCustomOn, next.EQCustomGains = true, gains })
			})
		})
	}
}

func (s *shell) playerLyricsSearch(c *ui.Context) {
	preview := s.app.LyricPreview()
	if preview.Open {
		ui.Text(c, preview.Title).FontSize(14).FontWeight(600)
		sheetAction(c, "left", "返回搜索结果", "", false, s.app.CloseLyricPreview)
		switch {
		case preview.Loading:
			ui.Spinner(c).Label("加载歌词预览")
		case preview.Err != "":
			ui.Text(c, preview.Err).FontSize(13)
		case preview.Adoptable:
			sheetAction(c, "check", "采用歌词", "", false, func() {
				if message := s.app.AdoptLyricPreview(); message != "" {
					s.notice = message
					return
				}
				s.player.lyrics = true
				s.closePlayerSheets()
			})
			for index, line := range lyrics.Parse(preview.LRC) {
				ui.Text(c.Key(index), line.Text).FontSize(13).MarginY(4)
			}
		default:
			ui.Text(c, "这一条没有歌词").FontSize(13)
		}
		return
	}
	sources := s.app.LyricSources()
	if len(sources) == 0 {
		ui.Text(c, "暂无可用歌词来源").FontSize(13)
		return
	}
	search := func() { s.app.SearchLyrics(s.player.lyricsSource, strings.TrimSpace(s.player.lyricsQuery)) }
	ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
		ui.TextInputBase(c, &s.player.lyricsQuery).Label("歌词关键词").Placeholder("输入歌名搜索").Grow(1).Height(44).FontSize(13).OnSubmit(search)
		iconButton(c, "search", "搜索歌词").OnClick(search)
	})
	ui.Row(c).FillWidth().Children(func() {
		for index, source := range sources {
			playerTool(c, "lyrics", source.Name).Selected(index == s.player.lyricsSource).OnClick(func() {
				s.player.lyricsSource = index
				if strings.TrimSpace(s.player.lyricsQuery) != "" {
					search()
				}
			})
		}
	})
	results := s.app.LyricSearch()
	switch {
	case results.Loading:
		ui.Spinner(c).Label("搜索歌词中")
	case results.Err != "":
		ui.Text(c, results.Err).FontSize(13)
	case results.Searched && len(results.Candidates) == 0:
		ui.Text(c, "没有找到歌词").FontSize(13)
	default:
		for _, candidate := range results.Candidates {
			sheetAction(c, "lyrics", candidate.Title, candidate.Artist, false, func() { s.app.OpenLyricPreview(results.Tab, candidate) })
		}
	}
}
