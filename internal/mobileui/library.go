package mobileui

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/ui/iconset"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/mobile"
)

type libraryPages[Item any] struct {
	account app.AccountSnapshot
	items   []Item
	next    int
	more    bool
	loading bool
	err     error
	cancel  context.CancelFunc
	version uint64
}

func loadLibraryPage[Item any](s *shell, state *libraryPages[Item], reset bool, load func(context.Context, int) ([]Item, bool, error)) {
	if reset {
		if state.cancel != nil {
			state.cancel()
		}
		state.items, state.next, state.more = nil, 1, true
	} else if state.loading || !state.more {
		return
	}
	state.version++
	state.loading, state.err = true, nil
	state.account = s.app.AccountSnapshot()
	account, version, page := state.account, state.version, state.next
	ctx, cancel := context.WithCancel(s.ctx)
	state.cancel = cancel
	go func() {
		defer cancel()
		items, more, err := load(ctx, page)
		s.update(func() {
			if state.version != version || s.app.AccountSnapshot() != account {
				return
			}
			state.loading, state.err, state.cancel = false, err, nil
			if err == nil {
				state.items = append(state.items, items...)
				state.next, state.more = page+1, more
			}
		})
	}()
}

func (s *shell) library(c *ui.Context, page *route) {
	switch page.kind {
	case pageMy:
		s.myLibrary(c, page)
	case pageHistory, pageLater:
		s.libraryHistory(c, page)
	case pageLocal:
		s.localMusic(c, page)
	case pageDownloads:
		s.downloadLibrary(c, page)
	case pageFollowing:
		s.followingLibrary(c, page)
	case pageFeed:
		s.feedLibrary(c, page)
	case pageCreator:
		s.libraryCreator(c, page)
	case pageStats:
		s.libraryStats(c, page)
	default:
		libraryMessage(c, "这一页走丢了", "返回上一页继续浏览。")
	}
}

type myLibraryState struct {
	account app.AccountSnapshot
	nav     app.AccountNav
	group   app.FavoriteGroup
	menu    bool
	logout  bool
	bind    bool
	recent  libraryPages[app.LibraryItem]
	local   []app.LibraryItem
	editor  folderEditor
}

type libraryDestination struct {
	kind              pageKind
	label, icon, path string
	requiresLogin     bool
}

var libraryDestinations = []libraryDestination{
	{pageHistory, "最近播放", "history", "/history", false},
	{pageLocal, "本地音乐", "disc", "/local-music", false},
	{pageDownloads, "下载", "download", "/download-list", false},
	{pageLater, "稍后再看", "later", "/later", true},
	{pageFollowing, "我的关注", "users", "/follow", true},
	{pageFeed, "关注动态", "bell", "/dynamic-feed", true},
	{pageStats, "听歌统计", "stats", "/stats", false},
}

func (s *shell) myLibrary(c *ui.Context, page *route) {
	account := s.app.AccountSnapshot()
	loggedIn := s.app.LoggedIn()
	hidden := s.app.HiddenMenuKeySet()
	state, _ := page.data.(*myLibraryState)
	if state == nil || state.account != account {
		state = &myLibraryState{account: account}
		page.data = state
		weekly, _ := s.app.ListeningStats()
		state.local = libraryLocalHistory(weekly, app.LibraryQuery{})
		if loggedIn {
			go func(target *myLibraryState) {
				nav, err := s.app.AccountNav(s.ctx)
				if err == nil {
					s.update(func() { target.nav = nav })
				}
			}(state)
			if s.app.LibraryOf().Mounted() && !slices.Contains(hidden, "/history") {
				loadLibraryPage(s, &state.recent, true, func(ctx context.Context, page int) ([]app.LibraryItem, bool, error) {
					return s.app.LibraryOf().History(ctx, page, app.LibraryQuery{})
				})
			}
			if s.app.CollectionsMounted() && !s.app.FavoriteState(app.FavoritesCreated).Loaded && !s.app.FavoritesRefreshing() {
				s.app.AutoRefreshFavorites()
			}
		}
	}
	ui.Row(c).FillWidth().Height(52).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, "我的").Grow(1).FontSize(21).Bold()
		label := "登录"
		if loggedIn {
			label = state.nav.Name
			if label == "" {
				label = "我的账号"
			}
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).MinHeight(44).Role(ui.RoleButton).Label(label).OnClick(func() {
			if loggedIn {
				state.menu = true
			} else {
				s.push(route{kind: pageLogin, title: "登录"})
			}
		}).Children(func() {
			if loggedIn {
				cover(c, state.nav.Face, 32, 16)
			} else {
				glyph(c, "user", 20, c.Theme().Text)
			}
			ui.Text(c, label).MaxLines(1).MaxWidth(118).FontSize(13)
			glyph(c, "right", 13, c.Theme().TextMuted)
		})
		iconButton(c, "settings", "设置").OnClick(func() { s.push(route{kind: pageSettings, title: "设置"}) })
	})
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		for _, item := range libraryDestinations[:4] {
			if slices.Contains(hidden, item.path) || item.requiresLogin && !loggedIn {
				continue
			}
			ui.Column(c).Key(item.path).Grow(1).Basis(0).Height(62).Gap(7).Justify(ui.Center).AlignItems(ui.Center).
				Role(ui.RoleButton).Label(item.label).OnClick(func() { s.openLibraryPage(item) }).Children(func() {
				if item.kind == pageLocal {
					ui.Icon(c, iconset.Must("Media/music-2-line.svg")).Size(20, 20).TextColor(c.Theme().Text)
				} else {
					glyph(c, item.icon, 20, c.Theme().Text)
				}
				ui.Text(c, item.label).Bold().FontSize(12).MaxLines(1)
			})
		}
	})
	if !loggedIn {
		var artwork string
		if discover, ok := s.stacks[0][0].data.(*discoverState); ok && len(discover.mixes.cards) > 0 {
			artwork = discover.mixes.cards[0].cover
		}
		s.libraryFeature(c, "folder", "逛逛公开歌单", "登录后可同步喜欢与收藏", artwork, func() { s.push(route{kind: pageMixes, title: "推荐歌单"}) }, nil)
	} else {
		s.likedFeature(c, state)
		s.myFolders(c, state, hidden)
	}
	if !slices.Contains(hidden, "/history") {
		items := state.local
		if len(state.recent.items) != 0 {
			items = state.recent.items
		}
		if len(items) > 0 {
			ui.Column(c).Margin(14, 0, 0).Gap(4).Children(func() {
				if sectionHeading(c, "最近播放", "全部") {
					s.openLibraryPage(libraryDestinations[0])
				}
				var playable []app.Item
				for _, item := range items[:min(3, len(items))] {
					if item.CanPlay {
						playable = append(playable, item.Item)
					}
				}
				ui.Column(c).Gap(2).Children(func() { s.trackList(c, playable) })
			})
		}
	}
	for _, item := range libraryDestinations[4:] {
		if !slices.Contains(hidden, item.path) && (item.kind != pageFeed || !slices.Contains(hidden, "/follow")) && (!item.requiresLogin || loggedIn) {
			libraryRow(c.Key(item.path), "", item.icon, item.label, "", func() { s.openLibraryPage(item) })
		}
	}
	libraryActionSheet(c, &state.menu, mobile.ActionSheetOptions{Title: state.nav.Name, Cancel: "取消"},
		mobile.Action{Label: "刷新收藏夹", OnTap: s.app.RefreshFavorites},
		mobile.Action{Label: "绑定喜欢的歌单", OnTap: func() { state.bind = true }},
		mobile.Action{Label: "退出登录", Destructive: true, Disabled: state.logout, OnTap: func() {
			state.logout = true
			go func() {
				err := s.app.Logout()
				s.update(func() {
					state.logout = false
					if err != nil {
						s.notice = err.Error()
					} else {
						s.notice = "已退出登录"
					}
				})
			}()
		}},
	)
	s.bindLikedFolderSheet(c, state)
	s.renderFolderEditor(c, &state.editor, func() { s.notice = "已新建收藏夹" })
}

func (s *shell) openLibraryPage(item libraryDestination) {
	title := item.label
	if item.kind == pageDownloads {
		title = "下载记录"
	}
	s.push(route{kind: item.kind, title: title})
}

func (s *shell) likedFeature(c *ui.Context, state *myLibraryState) {
	liked, ok := s.app.LikedFolder()
	if !ok {
		s.libraryFeature(c, "heart", "我喜欢的音乐", "选择收藏夹，保存红心歌曲", "", func() { state.bind = true }, nil)
		return
	}
	favorite := app.Favorite{ID: liked.ID, Title: liked.Title}
	for _, folder := range s.app.SideFavorites(app.FavoritesCreated) {
		if folder.ID == liked.ID {
			favorite = folder
			break
		}
	}
	ref := app.CollectionRefOf(favorite)
	s.libraryFeature(c, "heart", "我喜欢的音乐", fmt.Sprintf("%d 首", favorite.MediaCount), favorite.Cover, func() {
		s.push(route{kind: pageCollection, title: "我喜欢的音乐", ref: ref})
	}, func() {
		s.app.PlayFolder(favorite, func(text string, _ notice.Tone) { s.update(func() { s.notice = text }) })
	})
}

func (s *shell) bindLikedFolderSheet(c *ui.Context, state *myLibraryState) {
	liked, bound := s.app.LikedFolder()
	librarySheet(c, &state.bind, mobile.SheetOptions{Label: "绑定喜欢的歌单", Detents: []float32{0.52, 0.84}}, func() {
		ui.Scroll(c).Fill().Padding(16).Gap(8).Children(func() {
			ui.Text(c, "绑定喜欢的歌单").FontSize(20).Bold()
			ui.Text(c, "点红心的歌曲会保存到这个收藏夹。已有歌曲会保留。").FontSize(13).TextColor(c.Theme().TextMuted)
			for _, folder := range s.app.SideFavorites(app.FavoritesCreated) {
				if !folder.Invalid {
					meta := fmt.Sprintf("%d 首", folder.MediaCount)
					if folder.ID == liked.ID {
						meta += " · 当前绑定"
					}
					libraryRow(c.Key(folder.ID), folder.Cover, "folder", folder.Title, meta, func() {
						info := s.app.Collection(app.CollectionRefOf(folder))
						s.app.BindLikedFolder(app.LikedFolder{ID: folder.ID, Title: folder.Title, Private: info.Private})
						s.notice, state.bind = "已绑定喜欢的歌单", false
					})
				}
			}
			libraryButton(c, "plus", "新建收藏夹", false, !s.app.CollectionsMounted(), func() {
				state.bind = false
				state.editor = folderEditor{open: true}
			})
			if bound {
				libraryButton(c, "x", "取消绑定", false, false, func() {
					s.app.UnbindLikedFolder()
					state.bind = false
				})
			}
		})
	})
}

func (s *shell) libraryFeature(c *ui.Context, icon, title, subtitle, imageURL string, open, play func()) {
	theme := c.Theme()
	ui.Row(c).Margin(4, 0, 8).Padding(9).Radius(16).Gap(12).Height(80).AlignItems(ui.Center).
		Background(theme.Background.Mix(theme.Accent, 0.10)).Children(func() {
		ui.Row(c).Grow(1).AlignItems(ui.Center).Gap(12).Role(ui.RoleButton).Label(title).OnClick(open).Children(func() {
			if imageURL != "" {
				cover(c, imageURL, 62, 12)
			} else {
				ui.Box(c).Size(62, 62).Radius(12).Background(theme.Background.Mix(theme.Accent, 0.18)).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
					glyph(c, icon, 20, theme.Accent)
				})
			}
			ui.Column(c).Grow(1).Gap(4).Children(func() {
				ui.Text(c, title).Bold().FontSize(14)
				ui.Text(c, subtitle).FontSize(12).TextColor(theme.TextMuted).MaxLines(2)
			})
		})
		if play != nil {
			iconButton(c, "play", "播放我喜欢的音乐").Radius(22).Background(theme.Background).OnClick(play)
		} else {
			glyph(c, "right", 18, theme.TextMuted).Margin(0, 5, 0, 0)
		}
	})
}

func (s *shell) myFolders(c *ui.Context, state *myLibraryState, hidden []string) {
	ui.Row(c).AlignItems(ui.Center).Height(44).Children(func() {
		ui.Text(c, "我的歌单").Grow(1).FontSize(16).Bold()
		iconButton(c, "plus", "新建收藏夹").OnClick(func() { state.editor = folderEditor{open: true} })
	})
	ui.Row(c).Gap(10).Children(func() {
		for index, title := range []string{"创建", "收藏"} {
			group := app.FavoriteGroup(index)
			librarySegment(c, title, state.group == group, func() { state.group = group })
		}
	})
	folders := s.app.SideFavorites(state.group)
	status := s.app.FavoriteState(state.group)
	shown := 0
	for _, folder := range folders {
		if slices.Contains(hidden, strconv.FormatInt(folder.ID, 10)) {
			continue
		}
		shown++
		meta := fmt.Sprintf("%d 首", folder.MediaCount)
		ref := app.CollectionRefOf(folder)
		var open func()
		if folder.Invalid {
			meta = "收藏夹已失效"
		} else {
			open = func() { s.push(route{kind: pageCollection, title: folder.Title, ref: ref}) }
		}
		libraryRow(c.Key(ref.Key()), folder.Cover, "folder", folder.Title, meta, open)
	}
	if shown == 0 {
		switch {
		case status.Err != "":
			libraryMessage(c, "加载歌单失败", "")
			libraryButton(c, "refresh", "重试", false, false, s.app.RefreshFavorites)
		case !status.Loaded && s.app.FavoritesRefreshing():
			libraryMore(c, true, nil)
		default:
			ui.Text(c, "这里还没有歌单").FontSize(14).TextColor(c.Theme().TextMuted).PaddingY(10)
		}
	}
}

type libraryHistoryState struct {
	pages                    libraryPages[app.LibraryItem]
	query                    app.LibraryQuery
	keyword, status          string
	dateMenu, dates, confirm bool
	from, to                 time.Time
	target                   app.LibraryItem
	working, clearingViewed  bool
	local                    app.ListeningStats
}

func (s *shell) libraryHistory(c *ui.Context, page *route) {
	later, loggedIn := page.kind == pageLater, s.app.LoggedIn()
	if later && !loggedIn {
		s.libraryLogin(c, "稍后再看")
		return
	}
	state, _ := page.data.(*libraryHistoryState)
	account := s.app.AccountSnapshot()
	if state == nil || state.pages.account != account {
		state = &libraryHistoryState{}
		state.pages.account = account
		page.data = state
		if loggedIn {
			s.loadLibraryHistory(state, later, true)
		} else {
			state.local, _ = s.app.ListeningStats()
		}
	}
	items := state.pages.items
	if !loggedIn {
		items = libraryLocalHistory(state.local, state.query)
	}
	var playable []app.Item
	for _, item := range items {
		if item.CanPlay {
			playable = append(playable, item.Item)
		}
	}
	if later {
		ui.Text(c, "先存下来，等有空的时候再听。").FontSize(14).TextColor(c.Theme().TextMuted)
		ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
			libraryButton(c, "play", "播放全部", true, (len(playable) == 0 && (!state.pages.more || state.pages.loading)) || state.working, func() { s.playLater(state) })
			libraryButton(c, "trash", "删除已看完的", false, len(items) == 0 || state.working, func() {
				state.confirm, state.clearingViewed, state.target = true, true, app.LibraryItem{}
			})
		})
	} else {
		ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
			libraryInput(c, &state.keyword, "搜索标题 / UP 主名称", func() {
				state.query.Keyword = strings.TrimSpace(state.keyword)
				if loggedIn {
					s.loadLibraryHistory(state, false, true)
				}
			})
			iconButton(c, "calendar", "按日期筛选").OnClick(func() { state.dateMenu = true })
		})
		if !state.query.From.IsZero() || !state.query.To.IsZero() {
			ui.Text(c, state.query.From.Format("01-02")+" 至 "+state.query.To.Format("01-02")).FontSize(12).TextColor(c.Theme().TextMuted)
		}
	}
	if state.status != "" {
		ui.Text(c, state.status).FontSize(13).TextColor(c.Theme().TextMuted)
	}
	if state.pages.err != nil {
		libraryMessage(c, "加载失败", "检查网络后重试。")
		libraryButton(c, "refresh", "重试", false, false, func() { s.loadLibraryHistory(state, later, false) })
	} else if len(items) == 0 && !state.pages.loading && !state.pages.more {
		switch {
		case later:
			libraryMessage(c, "暂无稍后再看内容", "在歌曲的更多操作里选择「稍后再看」。")
			libraryButton(c, "disc", "去推荐页看看", false, false, func() { s.switchTab(0) })
		case state.query.Keyword != "":
			libraryMessage(c, "没有匹配的记录", "换一个关键词试试。")
		case !state.query.From.IsZero():
			libraryMessage(c, "这段时间没有收听记录", "换一个时间范围看看。")
		default:
			libraryMessage(c, "暂无历史记录", "播放过的歌曲会按日期记录在这里。")
			if !loggedIn {
				libraryButton(c, "user", "登录查看 B 站历史", false, false, func() { s.push(route{kind: pageLogin, title: "登录"}) })
			}
		}
	}
	var lastDate string
	for _, item := range items {
		if !later {
			day := item.ViewedAt.Local().Format("2006-01-02")
			if day != lastDate {
				lastDate = day
				ui.Text(c, libraryDayName(item.ViewedAt)).Margin(8, 0, 0).FontSize(14).Bold()
			}
		}
		ui.Row(c).Key(item.ID).AlignItems(ui.Center).Children(func() {
			meta := libraryHistorySubtitle(item, later)
			var play func()
			if item.CanPlay {
				play = func() { s.play([]app.Item{item.Item}, 0) }
			}
			libraryRow(c, item.Cover, "disc", item.Title, meta, play).Grow(1).Basis(0)
			if loggedIn {
				iconButton(c, "more", "移除 "+item.Title).Disabled(state.working).OnClick(func() {
					state.confirm, state.clearingViewed, state.target = true, false, item
				})
			}
		})
	}
	if state.pages.loading || state.pages.more && state.pages.err == nil {
		libraryMore(c, state.pages.loading, func() { s.loadLibraryHistory(state, later, false) })
	}
	s.historyDateSheet(c, state, loggedIn)
	confirmation := "移除这条记录？"
	if state.clearingViewed {
		confirmation = "删除已看完的内容？"
	}
	libraryActionSheet(c, &state.confirm, mobile.ActionSheetOptions{Title: confirmation, Message: state.target.Title, Cancel: "取消"},
		mobile.Action{Label: "移除", Destructive: true, OnTap: func() { s.removeLibraryHistory(state, later) }},
	)
}

func (s *shell) loadLibraryHistory(state *libraryHistoryState, later, reset bool) {
	query := state.query
	offset := len(state.pages.items)
	if reset {
		offset = 0
	}
	loadLibraryPage(s, &state.pages, reset, func(ctx context.Context, page int) ([]app.LibraryItem, bool, error) {
		if later {
			items, count, err := s.app.LibraryOf().ToView(ctx, page, query)
			return items, len(items) != 0 && offset+len(items) < count, err
		}
		return s.app.LibraryOf().History(ctx, page, query)
	})
}

func (s *shell) playLater(state *libraryHistoryState) {
	if state.working {
		return
	}
	state.working = true
	items, next, more, account := slices.Clone(state.pages.items), state.pages.next, state.pages.more, state.pages.account
	go func() {
		var err error
		for more && err == nil {
			var rows []app.LibraryItem
			var total int
			rows, total, err = s.app.LibraryOf().ToView(s.ctx, next, app.LibraryQuery{})
			items = append(items, rows...)
			more = len(rows) != 0 && len(items) < total
			next++
		}
		s.update(func() {
			state.working = false
			if s.app.AccountSnapshot() != account {
				return
			}
			if err != nil {
				state.status = "稍后再看没有加载完，请重试。"
				return
			}
			var playable []app.Item
			for _, item := range items {
				if item.CanPlay {
					playable = append(playable, item.Item)
				}
			}
			if len(playable) > 0 {
				s.play(playable, 0)
			} else {
				state.status = "没有可播放的歌曲。"
			}
		})
	}()
}

func (s *shell) removeLibraryHistory(state *libraryHistoryState, later bool) {
	state.working = true
	target, clear, account := state.target, state.clearingViewed, state.pages.account
	go func() {
		var err error
		switch {
		case clear:
			err = s.app.LibraryOf().ClearViewedLater(s.ctx)
		case later:
			err = s.app.LibraryOf().RemoveLater(s.ctx, target.ID)
		default:
			err = s.app.LibraryOf().RemoveHistory(s.ctx, target.ID)
		}
		s.update(func() {
			state.working = false
			if s.app.AccountSnapshot() != account {
				return
			}
			if err != nil {
				state.status = "移除失败，请重试。"
				return
			}
			state.status, state.target = "已移除", app.LibraryItem{}
			s.loadLibraryHistory(state, later, true)
		})
	}()
}

func (s *shell) historyDateSheet(c *ui.Context, state *libraryHistoryState, loggedIn bool) {
	now := time.Now().Local()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	apply := func(from, to time.Time) {
		state.query.From, state.query.To = from, to
		if loggedIn {
			s.loadLibraryHistory(state, false, true)
		}
	}
	libraryActionSheet(c, &state.dateMenu, mobile.ActionSheetOptions{Title: "选择时间范围", Cancel: "取消"},
		mobile.Action{Label: "全部时间", OnTap: func() { apply(time.Time{}, time.Time{}) }},
		mobile.Action{Label: "今天", OnTap: func() { apply(today, now) }},
		mobile.Action{Label: "最近 7 天", OnTap: func() { apply(today.AddDate(0, 0, -6), now) }},
		mobile.Action{Label: "最近 30 天", OnTap: func() { apply(today.AddDate(0, 0, -29), now) }},
		mobile.Action{Label: "自定义日期", OnTap: func() {
			state.from, state.to = today.AddDate(0, 0, -6), today
			state.dates = true
		}},
	)
	librarySheet(c, &state.dates, mobile.SheetOptions{Label: "自定义日期", Detents: []float32{0.82, 0.95}}, func() {
		ui.Scroll(c).Fill().Padding(16).Gap(6).Children(func() {
			ui.Text(c, "开始日期").Bold()
			mobile.DatePicker(c.Key("from"), &state.from)
			ui.Text(c, "结束日期").Bold()
			mobile.DatePicker(c.Key("to"), &state.to)
			libraryButton(c, "", "应用日期", true, state.to.Before(state.from), func() {
				state.dates = false
				apply(state.from, state.to.AddDate(0, 0, 1).Add(-time.Second))
			})
		})
	})
}

func libraryLocalHistory(stats app.ListeningStats, query app.LibraryQuery) []app.LibraryItem {
	var items []app.LibraryItem
	keyword := strings.ToLower(strings.TrimSpace(query.Keyword))
	for day, report := range stats.Days {
		date, err := time.ParseInLocation("2006-01-02", day, time.Local)
		if err != nil || report == nil || !query.From.IsZero() && date.Before(query.From) || !query.To.IsZero() && date.After(query.To) {
			continue
		}
		for key, track := range report.Tracks {
			if track == nil || !strings.Contains(strings.ToLower(track.Title+" "+track.Artist), keyword) {
				continue
			}
			item, err := app.ItemOfStatsKey(key, track)
			items = append(items, app.LibraryItem{ID: day + ":" + key, Title: track.Title, Cover: item.Cover, Owner: track.Artist, ViewedAt: date, CanPlay: err == nil, Item: item})
		}
	}
	slices.SortFunc(items, func(left, right app.LibraryItem) int {
		if order := right.ViewedAt.Compare(left.ViewedAt); order != 0 {
			return order
		}
		return strings.Compare(left.ID, right.ID)
	})
	return items
}

func libraryDayName(date time.Time) string {
	date = date.Local()
	now := time.Now().Local()
	switch date.Format("2006-01-02") {
	case now.Format("2006-01-02"):
		return "今天"
	case now.AddDate(0, 0, -1).Format("2006-01-02"):
		return "昨天"
	default:
		return fmt.Sprintf("%d 月 %d 日", date.Month(), date.Day())
	}
}

func libraryHistorySubtitle(item app.LibraryItem, later bool) string {
	if !item.CanPlay {
		return item.CannotPlay
	}
	if strings.HasPrefix(item.ID, "history:") {
		return strings.TrimPrefix(item.Owner+" · "+item.ViewedAt.Local().Format("15:04")+" 播放", " · ")
	}
	if !later {
		return item.Owner
	}
	progress := "还没看"
	if item.Progress < 0 || item.Duration > 0 && item.Progress >= item.Duration {
		progress = "已看完"
	} else if item.Progress > 0 {
		progress = fmt.Sprintf("看到 %d:%02d", item.Progress/time.Minute, item.Progress/time.Second%60)
	}
	return strings.TrimPrefix(item.Owner+" · "+progress, " · ")
}

func (s *shell) libraryLogin(c *ui.Context, name string) {
	palette := phonePalette(c)
	ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(38, 16, 24).Gap(12).Children(func() {
		ui.Box(c).Size(48, 48).Radius(12).Background(palette.field).Center().Children(func() {
			glyph(c, "disc", 24, palette.muted)
		})
		ui.Text(c, "登录后查看"+name).FontSize(16).FontWeight(600).Role(ui.RoleHeading)
		if settingsAction(c, "去登录", true, false).Clicked() {
			s.push(route{kind: pageLogin, title: "登录"})
		}
	})
}

func libraryMessage(c *ui.Context, title, subtitle string) {
	ui.Column(c).Padding(10, 0, 12).Gap(5).Children(func() {
		ui.Text(c, title).FontSize(16).Bold()
		if subtitle != "" {
			ui.Text(c, subtitle).FontSize(13).TextColor(c.Theme().TextMuted)
		}
	})
}

type sheetFrameKey struct{ open *bool }

type sheetFrameState struct{ open, settle bool }

func requestSheetFrame(c *ui.Context, open *bool) {
	state := ui.Local(c.Root(), sheetFrameKey{open}, func() sheetFrameState { return sheetFrameState{} })
	edge := state.open != *open
	if edge || state.settle {
		c.AnimationFrame()
	}
	state.open, state.settle = *open, edge
}

func libraryActionSheet(c *ui.Context, open *bool, options mobile.ActionSheetOptions, actions ...mobile.Action) {
	requestSheetFrame(c, open)
	mobile.ActionSheet(c, open, options, actions...)
}

func librarySheet(c *ui.Context, open *bool, options mobile.SheetOptions, body func()) {
	requestSheetFrame(c, open)
	mobile.Sheet(c, open, options, body)
}

func libraryMore(c *ui.Context, loading bool, more func()) {
	if loading {
		ui.Text(c, "正在加载…").FontSize(13).TextColor(c.Theme().TextMuted).PaddingY(14)
	} else {
		libraryButton(c, "down", "加载更多", false, false, more)
	}
}

func libraryInput(c *ui.Context, value *string, placeholder string, submit func()) {
	theme := c.Theme()
	ui.Row(c).Basis(0).Grow(1).Gap(8).AlignItems(ui.Center).Height(44).PaddingX(12).Radius(13).Background(theme.Surface).Children(func() {
		glyph(c, "search", 17, theme.TextMuted)
		input := ui.TextInputBase(c, value).Grow(1).Basis(0).FontSize(14).Height(44).Placeholder(placeholder).Label(placeholder).
			Search().Keyboard(ui.KeyboardSearch).ReturnKey(ui.ReturnSearch)
		if input.Submitted() {
			submit()
		}
	})
}

func libraryButton(c *ui.Context, icon, label string, primary, disabled bool, tap func()) ui.Element {
	theme := c.Theme()
	bg, fg := theme.Surface, theme.Text
	if primary {
		bg, fg = theme.Accent, theme.AccentText
	}
	return ui.Row(c).AlignSelf(ui.Start).AlignItems(ui.Center).Justify(ui.Center).Gap(6).MinHeight(44).PaddingX(14).
		Radius(22).Background(bg).Role(ui.RoleButton).Label(label).Disabled(disabled).OnClick(tap).Children(func() {
		if icon != "" {
			glyph(c, icon, 14, fg)
		}
		ui.Text(c, label).FontSize(13).TextColor(fg).Bold().MaxLines(1)
	})
}

func librarySegment(c *ui.Context, label string, selected bool, tap func()) {
	theme := c.Theme()
	fg, line := theme.TextMuted, theme.Background
	if selected {
		fg, line = theme.Accent, theme.Accent
	}
	ui.Column(c).MinHeight(44).Padding(10, 10, 0).Gap(6).Role(ui.RoleButton).Label(label).OnClick(tap).Children(func() {
		ui.Text(c, label).FontSize(13).TextColor(fg).MaxLines(1)
		ui.Spacer(c).Grow(1)
		ui.Box(c).FillWidth().Height(2).Background(line)
	})
}

func libraryRow(c *ui.Context, imageURL, icon, title, subtitle string, tap func()) ui.Element {
	theme := c.Theme()
	row := ui.Row(c).FillWidth().MinHeight(48).Gap(12).AlignItems(ui.Center)
	if tap != nil {
		row.Role(ui.RoleButton).Label(title).OnClick(tap)
	}
	return row.Children(func() {
		if imageURL != "" || subtitle != "" {
			if imageURL != "" {
				cover(c, imageURL, 48, 10)
			} else {
				ui.Box(c).AlignItems(ui.Center).Justify(ui.Center).Size(48, 48).Radius(10).Background(theme.Surface).Children(func() {
					glyph(c, icon, 20, theme.TextMuted)
				})
			}
		} else {
			glyph(c, icon, 20, theme.TextMuted)
		}
		ui.Column(c).Grow(1).Basis(0).Gap(3).Children(func() {
			ui.Text(c, title).FontSize(14).Bold().MaxLines(1)
			if subtitle != "" {
				ui.Text(c, subtitle).FontSize(12).TextColor(theme.TextMuted).MaxLines(2)
			}
		})
		if tap != nil {
			glyph(c, "right", 14, theme.TextMuted)
		}
	})
}
