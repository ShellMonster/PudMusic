package mobileui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/iconset"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

type browseOwner struct {
	mid  int64
	name string
}

type browseCollection struct {
	ref   app.CollectionRef
	title string
	cover string
	owner string
	count int
}

type browseMusic struct {
	rows    []app.RecommendItem
	started bool
	loading bool
	err     error
}

type browseMixes struct {
	cards   []browseCollection
	owners  []browseOwner
	started bool
	loading bool
	account uint64
	err     error
}

type discoverState struct {
	daily     browseMusic
	mixes     browseMixes
	refresh   ui.RefreshState
	albumBusy bool
	account   uint64
}

type discoverDetail struct {
	music   browseMusic
	mixes   *browseMixes
	feature browseCollection
	tracks  []app.Item
	page    int
	more    bool
	busy    bool
}

func (s *shell) discoverHomeState(page *route) *discoverState {
	if state, ok := page.data.(*discoverState); ok {
		return state
	}
	state := &discoverState{}
	page.data = state
	return state
}

func (s *shell) discoverRefresh(page *route, scroll ui.Element) {
	if page.kind != pageDiscover {
		return
	}
	state := s.discoverHomeState(page)
	if scroll.PullToRefresh(&state.refresh) {
		if state.daily.loading || state.mixes.loading {
			state.refresh.Refreshing = false
		} else {
			s.loadDiscoverHome(state)
		}
	}
}

func (s *shell) loadDiscoverHome(state *discoverState) {
	if state.daily.loading || state.mixes.loading {
		return
	}
	state.daily.started, state.daily.loading = true, true
	state.mixes.started, state.mixes.loading = true, true
	state.account = s.app.AccountSnapshot().Generation
	state.mixes.account = state.account
	recovering := state.daily.err != nil || state.mixes.err != nil || state.refresh.Refreshing
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
		defer cancel()
		rows, _, dailyErr := s.app.RecommendOf().Feed(ctx, app.HomeTabMusic, 1)
		var cards []browseCollection
		var owners []browseOwner
		err := dailyErr
		if dailyErr == nil {
			owners = recommendOwners(rows)
			s.update(func() {
				if state.daily.err != nil {
					state.mixes.err = nil
				}
				state.daily.loading, state.daily.err = false, nil
				state.daily.rows = rows
			})
			cards, err = s.publicMusicCollections(ctx, owners, "")
		}
		s.update(func() {
			if dailyErr != nil {
				state.daily.loading, state.daily.err = false, dailyErr
				if s.current().data == state && (len(state.daily.rows) > 0 || len(state.mixes.cards) > 0) {
					s.notice = "刷新失败，已保留上次加载的内容"
				}
			} else {
				state.mixes.owners = owners
				if err != nil && len(state.mixes.cards) > 0 && s.current().data == state {
					s.notice = "歌单刷新失败，已保留上次加载的歌单"
				}
			}
			state.mixes.loading, state.mixes.err = false, err
			if err == nil {
				state.mixes.cards = cards
				if recovering {
					s.notice = "已刷新推荐"
				}
			}
			state.refresh.Refreshing = false
		})
	}()
}

func recommendOwners(rows []app.RecommendItem) []browseOwner {
	owners := make([]browseOwner, 0, len(rows))
	for _, row := range rows {
		owners = append(owners, browseOwner{mid: row.OwnerMid, name: row.Owner})
	}
	return owners
}

func (s *shell) publicMusicCollections(ctx context.Context, owners []browseOwner, keyword string) ([]browseCollection, error) {
	const ownerLimit = 4
	seenOwners := make(map[int64]bool, ownerLimit)
	seenCards := make(map[string]bool)
	var cards []browseCollection
	var lastErr error
	succeeded := false
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	for _, owner := range owners {
		if owner.mid <= 0 || seenOwners[owner.mid] {
			continue
		}
		if len(seenOwners) == ownerLimit || ctx.Err() != nil {
			break
		}
		seenOwners[owner.mid] = true
		page, err := s.app.UpSeries(ctx, owner.mid, 1, 12)
		if err != nil {
			lastErr = err
			continue
		}
		succeeded = true
		for _, item := range page.Items {
			if item.ID <= 0 || item.Title == "" || !strings.Contains(strings.ToLower(item.Title), keyword) {
				continue
			}
			ref := app.CollectionRef{ID: item.ID, OwnerMid: owner.mid}
			switch item.Type {
			case 2:
				ref.Type = bili.CollectionAlbum
			case 3:
				ref.Type = bili.CollectionSeries
			default:
				continue
			}
			if seenCards[ref.Key()] {
				continue
			}
			seenCards[ref.Key()] = true
			cards = append(cards, browseCollection{ref: ref, title: item.Title, cover: item.Cover, owner: owner.name, count: item.Count})
		}
	}
	if !succeeded {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, lastErr
	}
	return cards, nil
}

func (s *shell) discover(c *ui.Context, page *route) {
	if page.kind != pageDiscover {
		s.discoverSubpage(c, page)
		return
	}
	state := s.discoverHomeState(page)
	if !state.daily.started || s.discoverAccountChanged(state.account, state.daily.err) {
		s.loadDiscoverHome(state)
	} else if state.daily.err == nil && s.discoverAccountChanged(state.mixes.account, state.mixes.err) {
		s.loadDiscoverMixes(&state.mixes)
	}
	palette := tok.Of(c.Theme())
	ui.Column(c).FillWidth().Gap(18).Children(func() {
		if ui.ButtonBase(c).Label("搜索歌曲、歌手、歌单").FillWidth().MinHeight(max(46, touchSize())).Radius(28).
			Background(palette.Surface2).Padding(0, 15).Children(func() {
			ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
				glyph(c, "search", 18, palette.Text2)
				ui.Text(c, "搜索歌曲、歌手、歌单").FontSize(13).TextColor(palette.Text2)
			})
		}).Clicked() {
			s.openSearch()
		}
		tracks := recommendTracks(state.daily.rows)
		retry := func() { s.loadDiscoverHome(state) }
		if state.daily.err != nil && len(tracks) == 0 && len(state.mixes.cards) == 0 {
			s.discoverUnavailable(c, state, retry)
			return
		}
		ui.Row(c).FillWidth().Gap(4).Children(func() {
			for _, item := range []struct {
				kind  pageKind
				title string
				icon  string
			}{{pageDaily, "每日推荐", "calendar"}, {pageChart, "排行榜", "stats"}, {pageNews, "新歌速递", "music"}, {pageMixes, "歌单", "queue"}} {
				if ui.ButtonBase(c).Label(item.title).Grow(1).Width(0).Radius(12).Padding(0, 0, 4, 0).
					Children(func() {
						ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(4).Children(func() {
							ui.Box(c).Size(39, 39).Background(palette.AccentSoft).Radius(20).Center().Children(func() {
								if item.icon == "music" {
									ui.Icon(c, iconset.Must("Media/music-2-line.svg")).Size(22, 22).TextColor(palette.AccentText)
								} else {
									glyph(c, item.icon, 22, palette.AccentText)
								}
							})
							ui.Text(c, item.title).FontSize(12).Bold().SingleLine().Ellipsis("…")
						})
					}).Clicked() {
					s.push(route{kind: item.kind, title: item.title, data: &discoverDetail{mixes: &state.mixes}})
				}
			}
		})
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			if state.daily.err == nil && state.mixes.err != nil && len(state.mixes.cards) == 0 {
				sectionHeading(c, "推荐歌单", "")
				s.discoverMixesNotice(c.Key("discover-mixes-error"), &state.mixes)
				return
			}
			if sectionHeading(c, "推荐歌单", "更多") {
				s.push(route{kind: pageMixes, title: "推荐歌单", data: &discoverDetail{mixes: &state.mixes}})
			}
			if state.daily.err == nil || len(state.mixes.cards) > 0 {
				s.renderMixes(c, &state.mixes, 3, retry)
			}
		})
		feature, single := discoverFeatured(state.mixes.cards, tracks)
		if feature.title != "" || state.mixes.loading {
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				sectionHeading(c, "今日一张", "")
				if feature.title == "" {
					ui.Box(c).FillWidth().Height(80).Radius(16).Background(palette.Bg)
					return
				}
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Padding(9).Radius(16).Background(palette.Bg).Children(func() {
					open := ui.ButtonBase(c).Label("打开今日一张 " + feature.title).Grow(1).Width(0).Children(func() {
						ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
							cover(c, feature.cover, 62, 10)
							ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
								ui.Text(c, feature.title).FontSize(16).Bold().SingleLine().Ellipsis("…")
								ui.Text(c, collectionSubtitle(feature)).FontSize(12).TextColor(palette.Text2).SingleLine().Ellipsis("…")
							})
						})
					})
					if open.Clicked() {
						detail := &discoverDetail{feature: feature}
						if single.Bvid != "" {
							detail.music.started, detail.tracks = true, []app.Item{single}
						}
						s.push(route{kind: pageAlbum, title: "今日一张", ref: feature.ref, data: detail})
					}
					hit := touchSize()
					if ui.ButtonBase(c).Size(hit, hit).Center().Radius(hit / 2).Background(palette.Surface).
						Label("播放今日一张").Disabled(state.albumBusy).Children(func() {
						glyph(c, "play", 18, palette.AccentText)
					}).Clicked() {
						if single.Bvid != "" {
							s.play([]app.Item{single}, 0)
						} else {
							s.playFeatured(feature, &state.albumBusy, false)
						}
					}
				})
			})
		}
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			if sectionHeading(c, "挑一首听", "查看全部") {
				s.push(route{kind: pageDaily, title: "每日推荐"})
			}
			if len(tracks) > 0 {
				s.trackList(c, tracks[:min(3, len(tracks))])
			} else if state.daily.loading {
				browseTrackPlaceholders(c, 3)
			} else if state.daily.err == nil {
				browseStatus(c, false, state.daily.err, "暂无推荐歌曲", retry)
			}
		})
		if state.daily.err != nil {
			s.discoverRetryNotice(c.Key("discover-refresh-error"), state.daily.err, "刷新失败", "已保留上次加载的内容。", "重试刷新", state.daily.loading || state.mixes.loading, retry)
		} else if state.mixes.err != nil && len(state.mixes.cards) > 0 {
			s.discoverMixesNotice(c.Key("discover-mixes-error"), &state.mixes)
		}
	})
}

func (s *shell) discoverAccountChanged(generation uint64, err error) bool {
	return errors.Is(err, bili.ErrNotLoggedIn) && s.app.LoggedIn() && s.app.AccountSnapshot().Generation != generation
}

func (s *shell) discoverMixesNotice(c *ui.Context, mixes *browseMixes) {
	detail := "其他推荐仍可浏览。"
	if len(mixes.cards) > 0 {
		detail = "已保留上次加载的歌单。"
	}
	s.discoverRetryNotice(c, mixes.err, "推荐歌单暂时不可用", detail, "重试歌单", mixes.loading, func() { s.loadDiscoverMixes(mixes) })
}

func (s *shell) discoverRetryNotice(c *ui.Context, err error, title, detail, action string, busy bool, retry func()) {
	icon := "network"
	if errors.Is(err, bili.ErrNotLoggedIn) {
		icon, title, action = "lock", "登录后可查看推荐", "去登录"
		retry = func() { s.push(route{kind: pageLogin, title: "登录"}) }
	}
	discoverNotice(c, icon, title, detail, action, busy, retry)
}

func (s *shell) discoverUnavailable(c *ui.Context, state *discoverState, retry func()) {
	requiresLogin := errors.Is(state.daily.err, bili.ErrNotLoggedIn)
	icon, title, detail, action := "network", "暂时无法加载推荐", "检查网络后重试，也可以搜索公开音乐。", "重新加载"
	if requiresLogin {
		icon, title, detail, action = "lock", "登录后可查看推荐", "也可以继续搜索公开音乐。", "去登录"
		retry = func() { s.push(route{kind: pageLogin, title: "登录"}) }
	}
	palette := phonePalette(c)
	ui.Column(c.Key("discover-unavailable")).FillWidth().Padding(20, 12, 22).Radius(16).
		Background(palette.fill).Gap(10).AlignItems(ui.Center).Children(func() {
		statusArtwork(c, icon)
		typo.Display(ui.Text(c, title), 19).Role(ui.RoleHeading)
		ui.Text(c, detail).FontSize(13).TextColor(palette.muted).TextAlign(ui.Center)
		label := action
		if state.daily.loading || state.mixes.loading {
			label = "正在加载…"
		}
		if settingsAction(c, label, true, false).Disabled(label != action).Clicked() {
			retry()
		}
	})
	ui.Column(c.Key("discover-local")).FillWidth().Gap(6).Children(func() {
		sectionHeading(c, "本机可用", "")
		libraryRow(c.Key("local"), "", "disc", "本地音乐", "打开设备里的音频文件", func() { s.openLibraryPage(libraryDestinations[1]) })
		libraryRow(c.Key("downloads"), "", "download", "下载记录", "播放已下载的音乐", func() { s.openLibraryPage(libraryDestinations[2]) })
	})
}

func discoverNotice(c *ui.Context, icon, title, detail, action string, busy bool, retry func()) {
	palette := phonePalette(c)
	ui.Row(c).FillWidth().Padding(8, 10).Gap(8).Radius(12).Background(palette.fill).AlignItems(ui.Center).Children(func() {
		if icon == "lock" {
			glyph(c, icon, 18, palette.muted)
		} else {
			networkUnavailableIcon(c, palette.muted)
		}
		ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, title).FontSize(14).FontWeight(600).Role(ui.RoleStatus)
			ui.Text(c, detail).FontSize(12).TextColor(palette.muted)
		})
		text := action
		if busy {
			text = "正在重试…"
		}
		if ui.ButtonBase(c).Key(action).Label(action).MinHeight(touchSize()).MinWidth(touchSize()).PaddingX(4).Shrink(0).
			Disabled(busy).Center().Children(func() { ui.Text(c, text).FontSize(13).FontWeight(600).TextColor(palette.accent) }).Clicked() {
			retry()
		}
	})
}

func discoverFeatured(cards []browseCollection, tracks []app.Item) (browseCollection, app.Item) {
	if len(cards) > 0 {
		return cards[0], app.Item{}
	}
	for _, track := range tracks {
		if track.Cover != "" {
			return browseCollection{title: track.Title, cover: track.Cover, owner: track.Artist, count: 1}, track
		}
	}
	if len(tracks) > 0 {
		track := tracks[0]
		return browseCollection{title: track.Title, owner: track.Artist, count: 1}, track
	}
	return browseCollection{}, app.Item{}
}

func (s *shell) renderMixes(c *ui.Context, mixes *browseMixes, maximum int, retry func()) {
	if len(mixes.cards) != 0 {
		s.collectionGrid(c, mixes.cards[:min(maximum, len(mixes.cards))])
		return
	}
	if mixes.loading {
		width, _ := c.Size()
		size := collectionGridSize(width)
		palette := tok.Of(c.Theme())
		ui.Row(c).FillWidth().Gap(10).Children(func() {
			for range 3 {
				ui.Column(c).Width(size).Gap(6).Children(func() {
					ui.Box(c).Size(size, size).Radius(12).Background(palette.Surface2)
					ui.Box(c).Size(size*0.7, 13).Radius(4).Background(palette.Surface2)
					ui.Box(c).Size(size*0.5, 12).Radius(4).Background(palette.Surface2)
				})
			}
		})
		return
	}
	browseStatus(c, false, mixes.err, "暂无公开歌单", retry)
}

func collectionGridSize(width float32) float32 {
	return (max(0, width-52)) / 3
}

func (s *shell) collectionGrid(c *ui.Context, cards []browseCollection) {
	palette := tok.Of(c.Theme())
	width, _ := c.Size()
	size := collectionGridSize(width)
	ui.Column(c).FillWidth().Gap(12).Children(func() {
		for start := 0; start < len(cards); start += 3 {
			ui.Row(c).FillWidth().Gap(10).Children(func() {
				for _, card := range cards[start:min(start+3, len(cards))] {
					if ui.ButtonBase(c).Key("mix:" + card.ref.Key()).Label(card.title).Width(size).Radius(12).Children(func() {
						ui.Column(c).FillWidth().Gap(5).Children(func() {
							cover(c, card.cover, size, 12)
							ui.Text(c, card.title).FontSize(13).FontWeight(550).SingleLine().Ellipsis("…")
							ui.Text(c, fmt.Sprintf("%s · %d 首", mixKindLabel(card), card.count)).FontSize(12).TextColor(palette.Text2).SingleLine().Ellipsis("…")
						})
					}).Clicked() {
						s.push(route{kind: pageCollection, title: card.title, ref: card.ref})
					}
				}
			})
		}
	})
}

func mixKindLabel(card browseCollection) string {
	if card.ref.Type == bili.CollectionSeries {
		return "系列"
	}
	if card.ref.Type == bili.CollectionAlbum {
		return "合集"
	}
	return "单曲"
}

func collectionSubtitle(card browseCollection) string {
	if card.owner == "" {
		return fmt.Sprintf("%s · %d 首", mixKindLabel(card), card.count)
	}
	return fmt.Sprintf("%s · %d 首", card.owner, card.count)
}

func (s *shell) playFeatured(feature browseCollection, busy *bool, enqueue bool) {
	if *busy {
		return
	}
	*busy = true
	if !enqueue {
		s.collectionPlayRequest++
	}
	request, intent := s.collectionPlayRequest, s.app.PlaybackIntent()
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
		defer cancel()
		var tracks []app.Item
		var err error
		var more bool
		for page := 1; page <= 10 && len(tracks) < 200; page++ {
			medias, hasMore, loadErr := s.app.LoadCollectionPage(ctx, feature.ref, bili.SortByFavTime, "", page)
			tracks = appendBrowseTracks(tracks, app.QueueItemsOfMedias(medias))
			more, err = hasMore, loadErr
			if !more || err != nil || len(medias) == 0 {
				break
			}
		}
		s.update(func() {
			*busy = false
			switch {
			case !enqueue && (s.collectionPlayRequest != request || s.app.PlaybackIntent() != intent):
				return
			case err != nil:
				s.notice = "暂时无法加载这张歌单"
			case len(tracks) == 0:
				s.notice = "这张歌单暂无可播放歌曲"
			case enqueue:
				s.report(s.app.Enqueue(tracks...))
			default:
				s.play(tracks, 0)
			}
			if err == nil && more && s.notice == "" {
				s.notice = fmt.Sprintf("已加入 %d 首歌曲，歌单中还有更多", len(tracks))
			}
		})
	}()
}

func (s *shell) discoverSubpage(c *ui.Context, page *route) {
	state, ok := page.data.(*discoverDetail)
	if !ok {
		state = &discoverDetail{}
		page.data = state
	}
	kind := page.kind
	if kind == pageMixes {
		if state.mixes == nil {
			state.mixes = &browseMixes{}
		}
		if !state.mixes.started || s.discoverAccountChanged(state.mixes.account, state.mixes.err) {
			s.loadDiscoverMixes(state.mixes)
		}
		if state.mixes.err != nil {
			if len(state.mixes.cards) > 0 {
				s.collectionGrid(c, state.mixes.cards)
			}
			s.discoverMixesNotice(c, state.mixes)
			return
		}
		s.renderMixes(c, state.mixes, len(state.mixes.cards), func() { s.loadDiscoverMixes(state.mixes) })
		return
	}
	load := func() { s.loadDiscoverDetail(state, kind) }
	if !state.music.started {
		load()
	}
	tracks := state.tracks
	if len(tracks) == 0 {
		browseStatus(c, state.music.loading, state.music.err, "暂无可播放歌曲", load)
		return
	}
	palette := tok.Of(c.Theme())
	coverURL, title, subtitle := tracks[0].Cover, page.title, fmt.Sprintf("%d 首", len(tracks))
	if kind == pageAlbum {
		coverURL, title, subtitle = state.feature.cover, state.feature.title, collectionSubtitle(state.feature)
	}
	ui.Column(c).FillWidth().Gap(12).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
			cover(c, coverURL, 62, 12)
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				ui.Text(c, title).FontSize(20).Bold().SingleLine().Ellipsis("…")
				ui.Text(c, subtitle).FontSize(12).TextColor(palette.Text2)
			})
		})
		ui.Row(c).FillWidth().Gap(8).Children(func() {
			if browseAction(c, "play", "播放全部", true) {
				if kind == pageAlbum && state.more {
					s.playFeatured(state.feature, &state.busy, false)
				} else {
					s.play(tracks, 0)
				}
			}
			if kind == pageAlbum && browseAction(c, "plus", "加入队列", false) {
				if state.more {
					s.playFeatured(state.feature, &state.busy, true)
				} else {
					s.report(s.app.Enqueue(tracks...))
				}
			}
		})
		if kind == pageChart {
			s.discoverRankedTracks(c, tracks)
		} else {
			s.trackList(c, tracks)
		}
		if state.more {
			browseMore(c, state.music.loading, state.music.err, load)
		}
	})
}

func (s *shell) discoverRankedTracks(c *ui.Context, tracks []app.Item) {
	palette := tok.Of(c.Theme())
	active, hasActive := s.app.Queue().Current()
	for index, track := range tracks {
		ui.Row(c.Key(track.Identity())).FillWidth().MinHeight(58).Gap(4).AlignItems(ui.Center).Children(func() {
			color := palette.Text2
			if index < 3 {
				color = palette.AccentText
			}
			ui.Text(c, fmt.Sprint(index+1)).Width(26).TextColor(color).FontSize(13).Bold()
			item := ui.Row(c).Grow(1).Basis(0).MinHeight(touchSize()).Gap(10).AlignItems(ui.Center).
				Role(ui.RoleButton).Label(track.Title).Children(func() {
				cover(c, track.Cover, 48, 8)
				trackLabel(c, track.Title, track.Artist, 13, hasActive && active.Identity() == track.Identity())
			})
			if item.Clicked() {
				s.play(tracks, index)
			}
			more := iconButton(c, "more", "更多操作："+track.Title)
			if item.LongPressed() || more.Clicked() {
				s.player.target = track
				s.player.show(sheetTrack)
			}
		})
	}
}

func (s *shell) loadDiscoverMixes(mixes *browseMixes) {
	if mixes.loading {
		return
	}
	mixes.started, mixes.loading = true, true
	mixes.account = s.app.AccountSnapshot().Generation
	owners := mixes.owners
	recovering := mixes.err != nil
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
		defer cancel()
		var err error
		if owners == nil {
			var rows []app.RecommendItem
			rows, _, err = s.app.RecommendOf().Feed(ctx, app.HomeTabMusic, 1)
			if err == nil {
				owners = recommendOwners(rows)
			}
		}
		var cards []browseCollection
		if err == nil {
			cards, err = s.publicMusicCollections(ctx, owners, "")
		}
		s.update(func() {
			mixes.loading, mixes.err = false, err
			mixes.owners = owners
			if err == nil {
				mixes.cards = cards
				if recovering {
					s.notice = "已刷新推荐歌单"
				}
			}
		})
	}()
}

func (s *shell) loadDiscoverDetail(state *discoverDetail, kind pageKind) {
	if state.music.loading {
		return
	}
	state.music.started, state.music.loading, state.music.err = true, true, nil
	page, ref := state.page+1, state.feature.ref
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
		defer cancel()
		var tracks []app.Item
		var more bool
		var err error
		if kind == pageAlbum {
			medias, hasMore, fetchErr := s.app.LoadCollectionPage(ctx, ref, bili.SortByFavTime, "", page)
			tracks, more, err = app.QueueItemsOfMedias(medias), hasMore, fetchErr
		} else {
			var rows []app.RecommendItem
			switch kind {
			case pageChart:
				rows, _, err = s.app.RecommendOf().Feed(ctx, app.HomeTabHot, page)
			case pageNews:
				rows, _, err = s.app.RecommendOf().NewMusic(ctx)
			default:
				rows, _, err = s.app.RecommendOf().Feed(ctx, app.HomeTabMusic, page)
				more = len(rows) > 0
			}
			tracks = recommendTracks(rows)
		}
		s.update(func() {
			state.music.loading, state.music.err = false, err
			if err == nil {
				state.page, state.more = page, more
				state.tracks = appendBrowseTracks(state.tracks, tracks)
			}
		})
	}()
}

func recommendTracks(rows []app.RecommendItem) []app.Item {
	tracks := make([]app.Item, 0, len(rows))
	for _, row := range rows {
		if row.Item.Bvid != "" {
			tracks = append(tracks, row.Item)
		}
	}
	return tracks
}

func appendBrowseTracks(existing, incoming []app.Item) []app.Item {
	seen := make(map[string]bool, len(existing)+len(incoming))
	for _, track := range existing {
		seen[track.Identity()] = true
	}
	for _, track := range incoming {
		identity := track.Identity()
		if identity == "" || seen[identity] {
			continue
		}
		seen[identity] = true
		existing = append(existing, track)
	}
	return existing
}

func browseStatus(c *ui.Context, loading bool, err error, empty string, retry func()) {
	if !loading && err != nil {
		browseStatusRow(c, "暂时没加载出来", "重试", retry)
		return
	}
	palette := tok.Of(c.Theme())
	ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(8).Padding(18, 8).Children(func() {
		switch {
		case loading:
			ui.Text(c, "正在加载…").FontSize(13).TextColor(palette.Text2)
		default:
			ui.Text(c, empty).FontSize(13).TextColor(palette.Text2)
		}
	})
}

func browseStatusRow(c *ui.Context, message, action string, run func()) {
	palette := phonePalette(c)
	ui.Row(c).FillWidth().MinHeight(touchSize()+12).Padding(6, 8, 6, 14).Gap(8).
		Radius(12).Background(palette.fill).AlignItems(ui.Center).Children(func() {
		ui.Text(c, message).FontSize(13).TextColor(palette.muted).Grow(1).MinWidth(0).Role(ui.RoleStatus)
		if run != nil {
			browseStatusButton(c, action, action, run)
		}
	})
}

func browseStatusButton(c *ui.Context, text, label string, run func()) {
	if ui.ButtonBase(c).Label(label).MinHeight(touchSize()).MinWidth(touchSize()).PaddingX(8).Shrink(0).Center().Children(func() {
		ui.Text(c, text).FontSize(13).FontWeight(600).TextColor(phonePalette(c).accent)
	}).Clicked() {
		run()
	}
}

func browseMore(c *ui.Context, loading bool, err error, load func()) {
	if loading {
		browseStatus(c, true, nil, "", nil)
		return
	}
	if err != nil {
		browseStatus(c, false, err, "", load)
		return
	}
	if browseAction(c, "down", "加载更多", false) {
		load()
	}
}

func browseTrackPlaceholders(c *ui.Context, count int) {
	palette := tok.Of(c.Theme())
	for range count {
		ui.Row(c).FillWidth().MinHeight(58).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(48, 48).Radius(8).Background(palette.Surface2)
			ui.Column(c).Gap(4).Children(func() {
				ui.Box(c).Size(100, 13).Radius(4).Background(palette.Surface2)
				ui.Box(c).Size(72, 12).Radius(4).Background(palette.Surface2)
			})
		})
	}
}

func browseAction(c *ui.Context, icon, label string, primary bool) bool {
	palette := tok.Of(c.Theme())
	background, foreground := palette.Surface2, palette.Text
	if primary {
		background, foreground = palette.Accent, palette.AccentInk
	}
	return ui.ButtonBase(c).Label(label).MinHeight(touchSize()).MinWidth(touchSize()).Padding(0, 12).Radius(22).Background(background).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			if icon != "" {
				glyph(c, icon, 15, foreground)
			}
			ui.Text(c, label).FontSize(13).Bold().TextColor(foreground)
		})
	}).Clicked()
}
