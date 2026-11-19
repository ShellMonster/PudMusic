package mobileui

import (
	"fmt"
	"strconv"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

var playerSheetTitles = [...]string{
	sheetQueue: "播放列表", sheetTrack: "歌曲操作", sheetOptions: "更多播放设置",
	sheetSpeed: "播放速度", sheetQuality: "音质", sheetSleep: "睡眠定时", sheetParts: "分集",
	sheetChapters: "章节", sheetComments: "评论", sheetReplies: "评论回复", sheetClearQueue: "清空播放列表",
	sheetCover: "当前封面的操作", sheetVideoQuality: "视频画质", sheetLyricsOptions: "歌词设置",
	sheetPrefs: "满屏播放器", sheetEQ: "均衡器", sheetLyricsSearch: "找歌词",
	sheetTrackFolders: "添加到收藏夹", sheetDownload: "下载选项",
}

func (s *shell) playerSheet(c *ui.Context, state app.State) {
	kind := s.player.sheet
	if kind == sheetNone {
		return
	}
	title := playerSheetTitles[kind]
	palette := phonePalette(c)
	width, height := c.Size()
	_, _, bottomInset, _ := c.SafeArea()
	tall := kind == sheetComments || kind == sheetReplies || kind == sheetQueue || kind == sheetEQ || kind == sheetLyricsSearch || kind == sheetTrackFolders
	sheetHeight := min(height*.65, 520)
	if kind == sheetQueue {
		sheetHeight = min(height*.78, 740)
		if s.player.queueExpanded {
			sheetHeight = height * .92
		}
	} else if tall {
		sheetHeight = min(height*.84, 740)
	} else if kind == sheetClearQueue {
		sheetHeight = min(height, 180)
	}
	ui.Box(c.Key("player-overlay")).Absolute().Top(0).Bottom(0).Left(0).Right(0).Modal().Role(ui.RoleDialog).Label(title).Children(func() {
		ui.Box(c).Absolute().Top(0).Bottom(0).Left(0).Right(0).Background(ui.RGBA(0, 0, 0, .38)).OnClick(func() { s.back() })
		ui.Column(c.Key(kind)).Absolute().Bottom(0).Left(max(0, (width-560)/2)).Width(min(560, width)).MaxHeight(sheetHeight).
			Padding(6, 16, max(12, bottomInset), 16).Radius(24, 24, 0, 0).Clip().Background(palette.surface).Children(func() {
			header := ui.Column(c).FillWidth().Children(func() {
				ui.Box(c).FillWidth().Height(9).Center().Children(func() { ui.Box(c).Size(34, 4).Radius(2).Background(palette.line) })
				ui.Row(c).FillWidth().Height(touchSize()).AlignItems(ui.Center).Children(func() {
					ui.Text(c, title).Grow(1).Basis(0).FontSize(16).FontWeight(650)
					iconButton(c, "x", "关闭"+title).OnClick(func() { s.back() })
				})
			})
			if dx, dy, completed := completedSwipe(header); completed && abs32(dy) >= max(44, 1.5*abs32(dx)) {
				switch {
				case kind == sheetQueue && dy < 0:
					s.player.queueExpanded = true
				case kind == sheetQueue && s.player.queueExpanded:
					s.player.queueExpanded = false
				case dy > 0:
					s.back()
				}
				c.AnimationFrame()
			}
			bodyHeight := sheetHeight - touchSize() - max(12, bottomInset) - 17
			queueHasItems := kind == sheetQueue && s.app.Queue().Snapshot().Len() > 0
			if queueHasItems {
				bodyHeight -= touchSize()
			}
			commentComposer := kind == sheetComments || kind == sheetReplies
			if commentComposer {
				bodyHeight -= touchSize() + 6
			}
			scroll := ui.Scroll(c).FillWidth().MaxHeight(max(50, bodyHeight))
			if tall {
				scroll = scroll.Height(max(50, bodyHeight))
			}
			scroll.Children(func() {
				ui.Column(c).FillWidth().Gap(2).Children(func() { s.playerSheetBody(c, kind, state) })
			})
			if queueHasItems {
				ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
					playerTool(c, "trash", "清空播放列表").OnClick(func() { s.player.show(sheetClearQueue) })
				})
			}
			if commentComposer {
				s.commentComposer(c)
			}
		})
	})
	if kind == sheetTrackFolders && s.player.folders != nil && s.player.folders.editor.open {
		folders := s.player.folders
		s.renderFolderEditor(c, &folders.editor, func() { s.loadPlayerFolders(folders) })
	}
}

func (s *shell) playerSheetBody(c *ui.Context, kind playerSheetKind, state app.State) {
	item, _ := s.app.Queue().Current()
	finish := func(action func()) func() { return func() { action(); s.player.back() } }
	switch kind {
	case sheetQueue:
		s.playerQueue(c)
	case sheetTrack:
		s.trackActions(c, s.player.target)
	case sheetOptions:
		if s.app.VideoAvailable() {
			sheetAction(c, "video", "看视频", "", false, func() { s.closePlayerSheets(); s.app.StartVideo() })
		}
		for _, choice := range []struct {
			kind playerSheetKind
			icon string
		}{
			{sheetSleep, "moon"}, {sheetSpeed, "play"}, {sheetEQ, "eq"}, {sheetPrefs, "settings"},
			{sheetLyricsSearch, "lyrics"}, {sheetCover, "image"},
		} {
			sheetAction(c, choice.icon, playerSheetTitles[choice.kind], "", false, func() { s.player.show(choice.kind) })
		}
		if item.Source == app.SourceOnline {
			sheetAction(c, "download", "下载当前歌曲", "", false, func() { s.openPlayerDownload(item) })
			sheetAction(c, "star", "添加到收藏夹…", "", false, func() { s.openPlayerFolders(item) })
		}
		if len(s.app.Parts()) > 1 {
			sheetAction(c, "parts", "分集", "", false, func() { s.player.show(sheetParts) })
		}
		if item.ArtistMid > 0 {
			sheetAction(c, "user", "打开 UP 主空间", "", false, func() {
				s.closePlayerSheets()
				s.player.visible = false
				s.app.StopVideo()
				s.push(route{kind: pageCreator, title: item.Artist, id: strconv.FormatInt(item.ArtistMid, 10)})
			})
		}
	case sheetSpeed:
		for _, speed := range []float64{.5, .75, 1, 1.25, 1.5, 1.75, 2} {
			sheetAction(c, "", fmt.Sprintf("%g×", speed), "", speed == state.Rate, finish(func() { s.app.SetRate(speed) }))
		}
	case sheetQuality:
		for _, quality := range []bili.AudioQuality{bili.AudioLossless, bili.AudioDolby, bili.AudioHigh, bili.AudioMedium, bili.AudioLow} {
			sheetAction(c, "", quality.Label(), "", quality == s.app.AudioQuality(), finish(func() { s.app.SetAudioQuality(quality) }))
		}
	case sheetSleep:
		for _, duration := range app.SleepTimerSteps {
			sheetAction(c, "", fmt.Sprintf("%d 分钟", int(duration/time.Minute)), "", state.SleepFor == duration,
				finish(func() { s.app.SetSleepTimer(duration) }))
		}
		sheetAction(c, "", "放完这一首就停", "", state.SleepAfterTrack, finish(s.app.SleepAfterTrack))
		sheetAction(c, "", "关闭定时", "", state.SleepAt.IsZero() && !state.SleepAfterTrack, finish(s.app.CancelSleepTimer))
	case sheetParts:
		for _, part := range s.app.Parts() {
			sheetAction(c, "", part.Part, timeLabel(part.Duration), item.Cid == part.Cid, finish(func() { s.report(s.app.PlayPart(part.Cid)) }))
		}
	case sheetChapters:
		for index, chapter := range state.Chapters {
			active := state.Position >= chapter.At && (index == len(state.Chapters)-1 || state.Position < state.Chapters[index+1].At)
			sheetAction(c, "", chapter.Title, timeLabel(chapter.At), active, finish(func() { s.app.SeekTo(chapter.At) }))
		}
	case sheetComments, sheetReplies:
		s.playerComments(c, kind == sheetReplies)
	case sheetClearQueue:
		ui.Text(c, "确定清空当前播放列表？").FontSize(13)
		sheetAction(c, "trash", "确认清空", "", false, func() { s.app.ClearQueue(); s.closePlayerSheets() })
	case sheetCover:
		if s.app.VideoAvailable() {
			sheetAction(c, "video", "看视频", "", false, func() { s.closePlayerSheets(); s.app.StartVideo() })
		}
		if item.Cover != "" {
			sheetAction(c, "open", "查看封面原图", "", false, func() { c.OpenURL(item.Cover) })
			sheetAction(c, "copy", "复制封面链接", "", false, finish(func() { mygo.Clipboard.WriteText(item.Cover); s.notice = "已复制封面链接" }))
		}
		if !s.app.VideoAvailable() && item.Cover == "" {
			ui.Text(c, "这首歌没有可用封面").FontSize(13)
		}
	case sheetVideoQuality:
		qualities, current := s.app.VideoQualities()
		if len(qualities) == 0 {
			ui.Text(c, "正在获取可用画质…").FontSize(13)
		}
		for _, quality := range qualities {
			sheetAction(c, "", quality.Label(), "", quality == current, finish(func() { s.player.bitmap = nil; s.app.SetVideoQuality(quality) }))
		}
	case sheetLyricsOptions:
		s.playerLyricsOptions(c)
	case sheetPrefs:
		s.playerPreferences(c)
	case sheetEQ:
		s.playerEqualizer(c)
	case sheetLyricsSearch:
		s.playerLyricsSearch(c)
	case sheetTrackFolders:
		s.playerFolderChoices(c)
	case sheetDownload:
		s.playerDownloadOptions(c)
	}
}

func (s *shell) closePlayerSheets() {
	s.player.sheet = sheetNone
	s.player.parents = nil
}

func sheetAction(c *ui.Context, icon, title, subtitle string, selected bool, action func()) {
	palette := phonePalette(c)
	ui.Row(c).FillWidth().MinHeight(touchSize()).PaddingX(5).AlignItems(ui.Center).Gap(9).Role(ui.RoleButton).Label(title).AccessSelected(selected).Children(func() {
		if icon != "" {
			glyph(c, icon, 17, palette.muted)
		}
		ui.Text(c, title).Grow(1).Basis(0).FontSize(13).MaxLines(2).Ellipsis("…")
		if subtitle != "" {
			ui.Text(c, subtitle).FontSize(12).TextColor(palette.muted).MaxLines(1)
		}
		if selected {
			glyph(c, "check", 16, palette.accent)
		}
	}).OnClick(action)
}

func (s *shell) playerQueue(c *ui.Context) {
	queue := s.app.Queue()
	view := queue.Snapshot()
	mode := view.PlayMode()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
		ui.Text(c, fmt.Sprintf("%d 首", view.Len())).FontSize(12).Grow(1).TextColor(phonePalette(c).muted)
		playerTool(c, playModeGlyph(mode), mode.Desc()).OnClick(s.app.CyclePlayMode)
	})
	occurrences := make(map[string]int, view.Len())
	for index := 0; index < view.Len(); index++ {
		item, _ := view.At(index)
		current := view.CurrentIndex() == index
		label := ""
		switch {
		case current:
			label = "正在播放"
		case index == view.CurrentIndex()+1:
			label = "接下来"
		case index == 0:
			label = "已播放"
		}
		id := item.Identity()
		occurrences[id]++
		key := c.Key(id + ":" + strconv.Itoa(occurrences[id]))
		if label != "" {
			ui.Text(c.Key("group:"+strconv.Itoa(index)), label).FontSize(12).TextColor(phonePalette(c).muted).Margin(5, 0, 0, 0)
		}
		const rowHeight = float32(58)
		row := ui.Row(key).Height(rowHeight).FillWidth().AlignItems(ui.Center).Gap(4).Children(func() {
			iconButton(c, "grip", "移动 "+item.Title).Drag(playerQueueDrag{index, view.Revision()}).OnClick(func() {
				queue.MoveAtBefore(index, index-1)
			})
			ui.Row(c).Grow(1).Basis(0).MinHeight(touchSize()).AlignItems(ui.Center).Gap(8).Role(ui.RoleButton).Label(item.Title).AccessSelected(current).Children(func() {
				cover(c, item.Cover, 48, 8)
				trackLabel(c, item.Title, item.Artist, 13, current)
			}).OnClick(func() { s.report(s.app.PlayAt(index)) })
			if current {
				ui.Box(c).Size(touchSize(), touchSize()).Center().Children(func() { glyph(c, "eq", 18, phonePalette(c).accent) })
			} else {
				iconButton(c, "x", "移除 "+item.Title).OnClick(func() { s.report(s.app.RemoveAt(index)) })
			}
		})
		if view.Len() < 2 {
			continue
		}
		if drag, over := ui.DragOver[playerQueueDrag](row); over && drag.index != index {
			_, y, _ := row.PointerPosition()
			accent := phonePalette(c).accent
			row.DrawOver(func(painter *ui.Painter, bounds ui.Rect) {
				if y >= rowHeight/2 {
					bounds.Y += bounds.H - 2
				}
				bounds.H = 2
				painter.Fill(bounds, accent, 1)
			})
		}
		if drag, dropped := ui.Drop[playerQueueDrag](row); dropped && drag.revision == queue.Snapshot().Revision() {
			before := index
			if _, y, _ := row.PointerPosition(); y >= rowHeight/2 {
				before++
			}
			queue.MoveAtBefore(drag.index, before)
		}
	}
	if view.Len() == 0 {
		ui.Text(c, "暂无播放内容").FontSize(13)
	}
}

type playerQueueDrag struct {
	index    int
	revision uint64
}

func (s *shell) trackActions(c *ui.Context, item app.Item) {
	ui.Row(c).MinHeight(54).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
		cover(c, item.Cover, 44, 8)
		trackLabel(c, item.Title, item.Artist, 13, false)
	})
	for _, action := range []struct {
		icon, title string
		run         func() error
	}{
		{"play", "立即播放", func() error { return s.app.EnqueueAndPlayNow(item) }},
		{"next1", "下一首播放", func() error { return s.app.EnqueueNext(item) }},
		{"queue", "添加到播放列表", func() error { return s.app.Enqueue(item) }},
	} {
		sheetAction(c, action.icon, action.title, "", false, func() {
			if err := action.run(); err != nil {
				s.report(err)
				return
			}
			s.player.back()
		})
	}
	if item.Source != app.SourceOnline {
		return
	}
	sheetAction(c, "star", "添加到收藏夹…", "", false, func() { s.openPlayerFolders(item) })
	sheetAction(c, "download", "下载", "", false, func() { s.openPlayerDownload(item) })
	if item.Bvid != "" {
		link := "https://www.bilibili.com/video/" + item.Bvid
		sheetAction(c, "open", "在 B 站打开", "", false, func() { c.OpenURL(link) })
		sheetAction(c, "link", "分享", "", false, func() {
			go func() { s.playerResult("")(mygo.Share(mygo.ShareOptions{Text: item.Title, URL: link})) }()
		})
	}
}

func (s *shell) openPlayerDownload(item app.Item) {
	s.player.downloadItem = item
	s.player.show(sheetDownload)
}

func (s *shell) playerDownloadOptions(c *ui.Context) {
	item := s.player.downloadItem
	ui.Text(c, item.Title).FontSize(13).MaxLines(2)
	sheetAction(c, "download", "下载音频", s.app.AudioQuality().Label(), false, func() {
		s.player.back()
		s.app.DownloadItem(item, s.playerResult("已添加下载任务"))
	})
	if item.Bvid != "" {
		sheetAction(c, "video", "下载视频", "", false, func() {
			s.player.back()
			s.app.DownloadItemVideo(item, s.playerResult("已添加下载任务"))
		})
	}
}
