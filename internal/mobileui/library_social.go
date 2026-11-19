package mobileui

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/mobile"
)

type followingLibraryState struct {
	pages                libraryPages[app.FollowUser]
	tags                 []app.FollowTag
	group                *app.FollowTag
	keyword, query       string
	sortOpen, sortByName bool
}

func (s *shell) followingLibrary(c *ui.Context, page *route) {
	if !s.app.LoggedIn() {
		s.libraryLogin(c, "关注")
		return
	}
	account := s.app.AccountSnapshot()
	state, _ := page.data.(*followingLibraryState)
	if state == nil || state.pages.account != account {
		state = &followingLibraryState{}
		page.data = state
		s.loadFollowing(state, true)
		s.loadFollowTags(state)
	}
	ui.ScrollHorizontal(c).FillWidth().Height(44).Gap(6).Children(func() {
		librarySegment(c, "全部关注", state.group == nil, func() {
			state.group = nil
			s.loadFollowing(state, true)
		})
		for _, tag := range state.tags {
			librarySegment(c.Key(tag.ID), fmt.Sprintf("%s %d", tag.Name, tag.Count), state.group != nil && state.group.ID == tag.ID, func() {
				state.group = &tag
				s.loadFollowing(state, true)
			})
		}
	})
	ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
		libraryInput(c, &state.keyword, "搜索关注", func() {
			state.query = strings.TrimSpace(state.keyword)
			s.loadFollowing(state, true)
		})
		iconButton(c, "sort", "排序").OnClick(func() { state.sortOpen = true })
	})
	rows := followingRows(state.pages.items, state.query, state.sortByName)
	if len(rows) > 0 {
		ui.Grid(c).Columns(2).Gap(8).Children(func() {
			for _, user := range rows {
				s.followingCard(c.Key(user.Mid), user)
			}
		})
	} else if !state.pages.loading && state.pages.err == nil && !state.pages.more {
		if state.query != "" {
			libraryMessage(c, "没有匹配的关注", "换一个 UP 主昵称试试。")
		} else {
			libraryMessage(c, "这个分组还没有人", "去 UP 主的空间点「关注」，就会出现在这里。")
		}
	}
	if state.pages.err != nil {
		libraryFailure(c, func() { s.loadFollowing(state, false) })
	} else if state.pages.loading || state.pages.more {
		libraryMore(c, state.pages.loading, func() { s.loadFollowing(state, false) })
	}
	libraryActionSheet(c, &state.sortOpen, mobile.ActionSheetOptions{Title: "排序", Cancel: "取消"},
		mobile.Action{Label: "关注时间", OnTap: func() { state.sortByName = false }},
		mobile.Action{Label: "昵称（已加载）", OnTap: func() { state.sortByName = true }},
	)
}

func followingRows(input []app.FollowUser, query string, alphabetical bool) []app.FollowUser {
	query = strings.ToLower(query)
	seen := make(map[int64]bool)
	var rows []app.FollowUser
	for _, user := range input {
		if !seen[user.Mid] && strings.Contains(strings.ToLower(user.Name), query) {
			rows = append(rows, user)
			seen[user.Mid] = true
		}
	}
	if alphabetical {
		slices.SortStableFunc(rows, func(left, right app.FollowUser) int {
			return strings.Compare(strings.ToLower(left.Name), strings.ToLower(right.Name))
		})
	}
	return rows
}

func (s *shell) followingCard(c *ui.Context, user app.FollowUser) {
	ui.Column(c).MinHeight(116).Padding(10).Gap(6).Radius(12).Background(c.Theme().Surface).Role(ui.RoleButton).Label(user.Name).
		OnClick(func() { s.openCreator(user.Mid, user.Name) }).Children(func() {
		cover(c, user.Face, 42, 21)
		ui.Text(c, user.Name).MaxLines(1).Bold().FontSize(14)
		if user.Sign != "" {
			ui.Text(c, user.Sign).MaxLines(2).FontSize(12).TextColor(c.Theme().TextMuted)
		}
		if user.Official != "" {
			ui.Text(c, user.Official).MaxLines(1).FontSize(11).TextColor(c.Theme().TextMuted)
		}
	})
}

func (s *shell) loadFollowTags(state *followingLibraryState) {
	account := s.app.AccountSnapshot()
	go func() {
		tags, err := s.app.FollowTagsForAccount(s.ctx, account)
		if err == nil {
			s.update(func() {
				if account == s.app.AccountSnapshot() {
					state.tags = tags
				}
			})
		}
	}()
}

func (s *shell) loadFollowing(state *followingLibraryState, reset bool) {
	account := s.app.AccountSnapshot()
	query, group := state.query, state.group
	loaded := len(state.pages.items)
	if reset {
		loaded = 0
	}
	loadLibraryPage(s, &state.pages, reset, func(ctx context.Context, page int) ([]app.FollowUser, bool, error) {
		if group != nil {
			users, err := s.app.FollowTagUsersForAccount(ctx, account, group.ID, page)
			return users, len(users) != 0 && loaded+len(users) < group.Count, err
		}
		var response app.FollowUsersPage
		var err error
		if query == "" {
			response, err = s.app.FollowListForAccount(ctx, account, page)
		} else {
			response, err = s.app.SearchFollowingsForAccount(ctx, account, query, page)
		}
		return response.Users, len(response.Users) != 0 && loaded+len(response.Users) < response.Total, err
	})
}

type dynamicLibraryState struct {
	pages   libraryPages[app.DynamicPage]
	pending map[string]bool
	liked   map[string]bool
}

type feedLibraryState struct {
	feed    dynamicLibraryState
	authors libraryPages[app.FollowAuthor]
	author  int64
}

func (s *shell) feedLibrary(c *ui.Context, page *route) {
	if !s.app.LoggedIn() {
		s.libraryLogin(c, "关注动态")
		return
	}
	account := s.app.AccountSnapshot()
	state, _ := page.data.(*feedLibraryState)
	if state == nil || state.feed.pages.account != account {
		state = &feedLibraryState{}
		page.data = state
		s.loadDynamicFeed(&state.feed, 0, true)
		s.loadFeedAuthors(state, true)
	}
	palette := phonePalette(c)
	ui.Column(c).FillWidth().Gap(6).Padding(8, 0, 10).Children(func() {
		ui.Text(c, "用户动态").FontSize(21).Bold().Role(ui.RoleHeading)
		ui.Text(c, "关注的 UP 主最近分享了什么。").FontSize(13).TextColor(palette.muted)
	})
	ui.ScrollHorizontal(c).FillWidth().Height(touchSize()).Gap(8).Children(func() {
		feedAuthorChip(c, "全部", state.author == 0, func() {
			state.author = 0
			s.loadDynamicFeed(&state.feed, 0, true)
		})
		seen := make(map[int64]bool)
		for _, author := range state.authors.items {
			if seen[author.Mid] {
				continue
			}
			seen[author.Mid] = true
			feedAuthorChip(c.Key(author.Mid), author.Name, state.author == author.Mid, func() {
				state.author = author.Mid
				s.loadDynamicFeed(&state.feed, author.Mid, true)
			})
		}
		if state.authors.more && !state.authors.loading {
			label := "更多 UP 主"
			if state.authors.err != nil {
				label = "重试加载 UP 主"
			}
			feedAuthorChip(c, label, false, func() { s.loadFeedAuthors(state, false) })
		}
	})
	if state.feed.pages.err != nil && len(state.feed.pages.items) == 0 {
		ui.Column(c.Key("feed-network-error")).FillWidth().Padding(60, 12, 24).Gap(10).AlignItems(ui.Center).Children(func() {
			statusArtwork(c, "network")
			typo.Display(ui.Text(c, "网络连接异常"), 18).Role(ui.RoleStatus)
			ui.Text(c, "检查网络连接后重试。").FontSize(13).TextColor(palette.muted)
			if browseAction(c, "refresh", "重试", false) {
				s.loadDynamicFeed(&state.feed, state.author, false)
			}
		})
		return
	}
	s.dynamicRows(c, &state.feed, state.author)
}

func feedAuthorChip(c *ui.Context, label string, selected bool, tap func()) {
	palette := phonePalette(c)
	fill, ink := palette.field, palette.muted
	if selected {
		fill, ink = palette.text, palette.surface
	}
	ui.ButtonBase(c).Label(label).AccessSelected(selected).Height(touchSize()).PaddingX(12).Radius(touchSize() / 2).
		Background(fill).Center().OnClick(tap).Children(func() {
		ui.Text(c, label).FontSize(13).TextColor(ink).SingleLine()
	})
}

func (s *shell) loadFeedAuthors(state *feedLibraryState, reset bool) {
	account := s.app.AccountSnapshot()
	loadLibraryPage(s, &state.authors, reset, func(ctx context.Context, page int) ([]app.FollowAuthor, bool, error) {
		result, err := s.app.FollowedAuthorsForAccount(ctx, account, page)
		return result.Authors, result.HasMore, err
	})
}

func (s *shell) loadDynamicFeed(state *dynamicLibraryState, mid int64, reset bool) {
	var offset string
	if !reset && len(state.pages.items) != 0 {
		offset = state.pages.items[len(state.pages.items)-1].Offset
	}
	if state.pending == nil {
		state.pending, state.liked = make(map[string]bool), make(map[string]bool)
	}
	account := s.app.AccountSnapshot()
	loadLibraryPage(s, &state.pages, reset, func(ctx context.Context, _ int) ([]app.DynamicPage, bool, error) {
		var result app.DynamicPage
		var err error
		if mid == 0 {
			result, err = s.app.DynamicFeedForAccount(ctx, account, offset)
		} else {
			result, err = s.app.UpDynamics(ctx, mid, offset)
		}
		return []app.DynamicPage{result}, result.HasMore && result.Offset != "" && result.Offset != offset, err
	})
}

func (s *shell) dynamicRows(c *ui.Context, state *dynamicLibraryState, mid int64) {
	seen := make(map[string]bool)
	for _, data := range state.pages.items {
		for _, dynamic := range data.Items {
			if !seen[dynamic.ID] {
				seen[dynamic.ID] = true
				s.dynamicCard(c.Key(dynamic.ID), state, dynamic)
			}
		}
	}
	if state.pages.err != nil {
		title, detail := "暂时无法加载动态", "检查网络连接后重试。"
		if len(seen) > 0 {
			title, detail = "暂时无法加载更多动态", "已加载的动态仍可查看。"
		}
		discoverNotice(c.Key("dynamic-load-error"), "network", title, detail, "重试", false, func() { s.loadDynamicFeed(state, mid, false) })
	} else if state.pages.more || state.pages.loading {
		libraryMore(c, state.pages.loading, func() { s.loadDynamicFeed(state, mid, false) })
	} else if len(seen) == 0 {
		libraryMessage(c, "这里没有能显示的动态", "关注的 UP 主发布的新动态会显示在这里。")
	}
}

func (s *shell) dynamicCard(c *ui.Context, state *dynamicLibraryState, dynamic app.Dynamic) {
	liked, changed := state.liked[dynamic.ID]
	if !changed {
		liked = dynamic.Liked
	}
	ui.Column(c).FillWidth().Radius(12).Background(c.Theme().Surface).Padding(12).Gap(8).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).MinHeight(44).Role(ui.RoleButton).Label(dynamic.Author.Name).
			OnClick(func() { s.openCreator(dynamic.Author.Mid, dynamic.Author.Name) }).Children(func() {
			cover(c, dynamic.Author.Face, 34, 17)
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, dynamic.Author.Name).FontSize(14).Bold().MaxLines(1)
				ui.Text(c, dynamic.Time).FontSize(12).TextColor(c.Theme().TextMuted)
			})
		})
		if dynamic.Text != "" {
			ui.Text(c, dynamic.Text).FontSize(14)
		}
		if len(dynamic.Pics) > 0 {
			ui.ScrollHorizontal(c).Height(92).FillWidth().Gap(6).Children(func() {
				for _, imageURL := range dynamic.Pics {
					cover(c, imageURL, 92, 10)
				}
			})
		}
		if video := dynamic.Video; video != nil {
			item := app.Item{Source: app.SourceOnline, Type: "mv", Bvid: video.Bvid, Title: video.Title, Cover: video.Cover, Artist: dynamic.Author.Name, ArtistMid: dynamic.Author.Mid}
			s.trackList(c, []app.Item{item})
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			icon, label := "thumb", "点赞"
			if liked {
				icon, label = "thumb-on", "取消点赞"
			}
			count := dynamic.LikeCount
			if changed && liked != dynamic.Liked {
				if liked {
					count++
				} else {
					count = max(0, count-1)
				}
			}
			libraryButton(c, icon, fmt.Sprintf("%s %d", label, count), false, state.pending[dynamic.ID], func() {
				if !s.app.LoggedIn() {
					s.push(route{kind: pageLogin, title: "登录"})
					return
				}
				state.pending[dynamic.ID], state.liked[dynamic.ID] = true, !liked
				s.app.LikeDynamicForAccount(s.app.AccountSnapshot(), dynamic.ID, !liked, func(err error) {
					s.update(func() {
						delete(state.pending, dynamic.ID)
						if err != nil {
							state.liked[dynamic.ID] = liked
							s.notice = "点赞操作失败，请重试。"
						}
					})
				})
			})
			if _, err := strconv.ParseUint(dynamic.ID, 10, 64); err == nil {
				libraryButton(c, "comment", "查看评论", false, false, func() { c.OpenURL("https://t.bilibili.com/" + dynamic.ID) })
			}
		})
	})
}

type libraryCreatorState struct {
	account       app.AccountSnapshot
	space         *app.UpSpace
	tab           int
	order         string
	sort, working bool
	videos        libraryPages[app.SpaceVideo]
	posts         dynamicLibraryState
	folders       libraryPages[app.SpaceCollection]
	series        libraryPages[app.SpaceCollection]
}

func (s *shell) libraryCreator(c *ui.Context, page *route) {
	mid, err := strconv.ParseInt(page.id, 10, 64)
	if err != nil || mid <= 0 {
		libraryMessage(c, "这一页走丢了", "无法找到这位 UP 主。")
		return
	}
	account := s.app.AccountSnapshot()
	state, _ := page.data.(*libraryCreatorState)
	if state == nil || state.account != account {
		state = &libraryCreatorState{account: account, space: s.app.UpSpaceOf(mid), order: "pubdate"}
		page.data = state
		state.space.Load()
		s.loadCreatorTab(state, true)
	}
	header := state.space.Snapshot()
	if header.Err != "" && !header.Loaded {
		libraryMessage(c, "加载 UP 主失败", header.Err)
		libraryButton(c, "refresh", "重试", false, false, state.space.Load)
	} else {
		s.creatorHeader(c, state, header, page.title)
	}
	ui.ScrollHorizontal(c).FillWidth().Height(44).Gap(6).Children(func() {
		for index, label := range []string{"投稿", "动态", "收藏夹", "合集"} {
			librarySegment(c, label, state.tab == index, func() {
				state.tab = index
				s.loadCreatorTab(state, true)
			})
		}
	})
	switch state.tab {
	case 0:
		s.creatorVideos(c, state, header.User.Name)
	case 1:
		s.dynamicRows(c, &state.posts, mid)
	case 2:
		s.creatorFolders(c, state, &state.folders, "他还没有公开收藏夹", bili.CollectionFavorite)
	case 3:
		s.creatorFolders(c, state, &state.series, "还没有合集", bili.CollectionAlbum)
	}
}

func (s *shell) creatorHeader(c *ui.Context, state *libraryCreatorState, header app.SpaceHeaderSnapshot, fallback string) {
	name := header.User.Name
	if name == "" {
		name = fallback
	}
	if name == "" {
		name = "UP 主空间"
	}
	ui.Column(c).Padding(12).Gap(8).Radius(16).Background(c.Theme().Surface).Children(func() {
		cover(c, header.User.Face, 64, 32)
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, name).FontSize(21).Bold().MaxLines(1)
			if header.User.Level > 0 {
				ui.Text(c, fmt.Sprintf("LV%d", header.User.Level)).FontSize(12).Bold().TextColor(c.Theme().Accent)
			}
		})
		if header.User.Sign != "" {
			ui.Text(c, header.User.Sign).FontSize(13).TextColor(c.Theme().TextMuted)
		}
		if header.StatOK() {
			ui.Row(c).Gap(14).Children(func() {
				for _, metric := range []struct {
					name  string
					value int64
				}{{"粉丝", header.Stat.Follower}, {"关注", header.Stat.Following}, {"获赞", header.Stat.Likes}} {
					ui.Column(c).Gap(2).Children(func() {
						ui.Text(c, fmt.Sprint(metric.value)).FontSize(15).Bold()
						ui.Text(c, metric.name).FontSize(12).TextColor(c.Theme().TextMuted)
					})
				}
			})
		}
		ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
			if state.space.Mid() != s.app.SelfMid() {
				follow := header.Follow(s.app.SelfMid(), s.app.LoggedIn())
				switch follow {
				case app.FollowAskable, app.FollowDone:
					label, icon := "关注", "plus"
					if follow == app.FollowDone {
						label, icon = "已关注", "check"
					}
					libraryButton(c, icon, label, follow == app.FollowAskable, state.working, func() { s.creatorFollow(state, follow != app.FollowDone) })
				case app.FollowFailed:
					libraryButton(c, "refresh", "重试关注状态", false, false, state.space.RetryRelation)
				}
			}
			libraryButton(c, "play", "播放投稿", false, len(state.videos.items) == 0, func() { s.play(creatorVideoItems(state.videos.items, state.space.Mid(), name), 0) })
			iconButton(c, "open", "在 B 站打开").OnClick(func() { c.OpenURL("https://space.bilibili.com/" + strconv.FormatInt(state.space.Mid(), 10)) })
		})
	})
}

func (s *shell) creatorFollow(state *libraryCreatorState, follow bool) {
	state.working = true
	go func() {
		err := state.space.FollowForAccount(s.ctx, state.account, follow)
		s.update(func() {
			state.working = false
			if err != nil && state.account == s.app.AccountSnapshot() {
				s.notice = "关注操作失败，请重试。"
			}
		})
	}()
}

func (s *shell) loadCreatorTab(state *libraryCreatorState, reset bool) {
	mid := state.space.Mid()
	switch state.tab {
	case 0:
		order := state.order
		loadLibraryPage(s, &state.videos, reset, func(ctx context.Context, page int) ([]app.SpaceVideo, bool, error) {
			result, err := s.app.UpVideos(ctx, app.SpaceVideoQuery{Mid: mid, Page: page, Order: order})
			return result.Videos, result.HasMore, err
		})
	case 1:
		s.loadDynamicFeed(&state.posts, mid, reset)
	case 2:
		loadLibraryPage(s, &state.folders, reset, func(ctx context.Context, page int) ([]app.SpaceCollection, bool, error) {
			result, err := s.app.UpCollections(ctx, mid, page)
			return result.Items, result.HasMore, err
		})
	case 3:
		loadLibraryPage(s, &state.series, reset, func(ctx context.Context, page int) ([]app.SpaceCollection, bool, error) {
			result, err := s.app.UpSeries(ctx, mid, page, 0)
			return result.Items, result.HasMore, err
		})
	}
}

func (s *shell) creatorVideos(c *ui.Context, state *libraryCreatorState, name string) {
	ui.Row(c).Gap(6).AlignItems(ui.Center).Justify(ui.End).Children(func() {
		iconButton(c, "sort", "投稿排序").OnClick(func() { state.sort = true })
	})
	items := creatorVideoItems(state.videos.items, state.space.Mid(), name)
	if len(items) > 0 {
		s.trackList(c, items)
	}
	if state.videos.err != nil {
		libraryFailure(c, func() { s.loadCreatorTab(state, false) })
	} else if state.videos.loading || state.videos.more {
		libraryMore(c, state.videos.loading, func() { s.loadCreatorTab(state, false) })
	} else if len(items) == 0 {
		libraryMessage(c, "暂无投稿", "")
	}
	var actions []mobile.Action
	for _, order := range []struct{ id, name string }{{"pubdate", "最新发布"}, {"click", "最多播放"}, {"scores", "最多评论"}} {
		actions = append(actions, mobile.Action{Label: order.name, OnTap: func() {
			state.order = order.id
			s.loadCreatorTab(state, true)
		}})
	}
	libraryActionSheet(c, &state.sort, mobile.ActionSheetOptions{Title: "投稿排序", Cancel: "取消"}, actions...)
}

func creatorVideoItems(videos []app.SpaceVideo, owner int64, fallback string) []app.Item {
	var items []app.Item
	for _, video := range videos {
		if video.Bvid == "" {
			continue
		}
		artist := video.Author
		if artist == "" {
			artist = fallback
		}
		items = append(items, app.Item{Source: app.SourceOnline, Type: "mv", Bvid: video.Bvid, Cid: video.Cid, Cover: video.Cover, Title: video.Title, Artist: artist, ArtistMid: owner})
	}
	return items
}

func (s *shell) creatorFolders(c *ui.Context, state *libraryCreatorState, pages *libraryPages[app.SpaceCollection], empty string, fallback bili.CollectionType) {
	seen := make(map[string]bool)
	for _, item := range pages.items {
		kind := fallback
		if item.Type != 0 {
			kind = bili.ParseCollectionType(strconv.Itoa(item.Type))
		}
		ref := app.CollectionRef{ID: item.ID, Type: kind, OwnerMid: state.space.Mid()}
		if seen[ref.Key()] {
			continue
		}
		seen[ref.Key()] = true
		libraryRow(c.Key(ref.Key()), item.Cover, "folder", item.Title, fmt.Sprintf("%d 首", item.Count), func() {
			s.push(route{kind: pageCollection, title: item.Title, ref: ref})
		})
	}
	if pages.err != nil {
		libraryFailure(c, func() { s.loadCreatorTab(state, false) })
	} else if pages.loading || pages.more {
		libraryMore(c, pages.loading, func() { s.loadCreatorTab(state, false) })
	} else if len(seen) == 0 {
		libraryMessage(c, empty, "")
	}
}

func libraryFailure(c *ui.Context, retry func()) {
	libraryMessage(c, "加载失败", "检查网络后重试。")
	libraryButton(c, "refresh", "重试", false, false, retry)
}

func (s *shell) libraryRefresh(page *route, scroll ui.Element) {
	var loading bool
	var refresh func()
	switch state := page.data.(type) {
	case *libraryHistoryState:
		loading = state.pages.loading
		refresh = func() {
			if s.app.LoggedIn() {
				s.loadLibraryHistory(state, page.kind == pageLater, true)
			} else {
				state.local, _ = s.app.ListeningStats()
			}
		}
	case *myLibraryState:
		loading = s.app.FavoritesRefreshing()
		refresh = func() {
			if s.app.LoggedIn() {
				s.app.RefreshFavorites()
			}
			stats, _ := s.app.ListeningStats()
			state.local = libraryLocalHistory(stats, app.LibraryQuery{})
		}
	case *followingLibraryState:
		loading = state.pages.loading
		refresh = func() { s.loadFollowing(state, true); s.loadFollowTags(state) }
	case *feedLibraryState:
		loading = state.feed.pages.loading
		refresh = func() { s.loadDynamicFeed(&state.feed, state.author, true) }
	case *localLibraryState:
		loading = s.app.LocalMusicState().Scanning
		refresh = s.app.RescanLocalMusic
	case *downloadLibraryState:
		refresh = func() { _ = s.app.DownloadSnapshotList() }
	}
	if refresh != nil {
		state := ui.Local(scroll, "library-refresh", func() ui.RefreshState { return ui.RefreshState{} })
		state.Refreshing = state.Refreshing && loading
		if scroll.PullToRefresh(state) {
			refresh()
		}
	}
}
