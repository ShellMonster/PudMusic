package mobileui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/mobile"
)

type mobileSearchTab uint8

const (
	mobileSearchSongs mobileSearchTab = iota
	mobileSearchMixes
	mobileSearchCreators
	mobileSearchVideos
	mobileSearchTabCount
)

var mobileSearchNames = [mobileSearchTabCount]string{"歌曲", "歌单", "UP 主", "视频"}

type mobileSearchResult struct {
	rows    []app.SearchItem
	mixes   []browseCollection
	started bool
	loading bool
	err     error
	page    int
	more    bool
	cancel  context.CancelFunc
}

type mobileSearchState struct {
	draft, query string
	inputFocus   bool
	readyAt      time.Time
	tab          mobileSearchTab
	sort         [mobileSearchTabCount]app.SearchSort
	results      [mobileSearchTabCount]mobileSearchResult
	sortOpen     bool
	history      mobileSearchHistory
	hot          browseMusic
	mixes        browseMixes
	preview      mobileSearchPreview
}

type mobileSearchPreview struct {
	keyword string
	songs   app.SearchPage
	users   app.SearchPage
	songOK  bool
	userOK  bool
	cancel  context.CancelFunc
}

type mobileSearchHistory struct {
	words   []string
	path    string
	loaded  bool
	started bool
	dirty   bool
	saving  bool
}

func (s *shell) search(c *ui.Context, page *route) {
	state, ok := page.data.(*mobileSearchState)
	if !ok {
		state = &mobileSearchState{}
		page.data = state
	}
	if page.focus {
		state.inputFocus = true
		page.focus = false
	}
	if page.query != "" {
		s.submitMobileSearch(state, page.query)
		page.query = ""
	}
	if !state.history.started {
		s.loadMobileSearchHistory(&state.history)
	}
	palette := tok.Of(c.Theme())
	ui.Column(c).FillWidth().Gap(18).Children(func() {
		ui.Row(c).FillWidth().MinHeight(50).Radius(28).AlignItems(ui.Center).Gap(10).Padding(0, 6, 0, 14).
			Background(palette.Surface2).Children(func() {
			glyph(c, "search", 18, palette.Text2)
			field := ui.TextInputBase(c.Key("search:query"), &state.draft).Label("搜索歌曲、歌手、歌单或 UP 主").
				Placeholder("搜歌、歌手、歌单或 UP 主").PlaceholderColor(palette.Text2).FontSize(13).
				Grow(1).MinWidth(0).Height(50).Search().Keyboard(ui.KeyboardSearch).ReturnKey(ui.ReturnSearch).
				FocusBind(&state.inputFocus, true)
			if field.Changed() {
				keyword := strings.TrimSpace(state.draft)
				state.readyAt = c.Now().Add(375 * time.Millisecond)
				if state.preview.keyword != keyword {
					cancelSearchPreview(state)
				}
				if keyword == "" {
					s.resetMobileSearchResults(state)
					state.query = ""
					s.app.SearchSuggest("")
				}
			}
			if state.draft != "" && iconButton(c, "x", "清除搜索词").Clicked() {
				s.clearMobileSearch(state)
			}
			hit := touchSize()
			submit := ui.ButtonBase(c).Label("搜索").Size(hit, hit).Center().Children(func() {
				glyph(c, "right", 16, palette.AccentText)
			})
			if field.Submitted() || submit.Clicked() {
				s.submitMobileSearch(state, state.draft)
			}
		})
		draft := strings.TrimSpace(state.draft)
		switch {
		case draft != "" && draft != state.query:
			s.mobileSearchSuggestions(c, state, draft)
		case state.query == "" || draft == "":
			s.mobileSearchLanding(c, state)
		default:
			s.mobileSearchResults(c, state)
		}
	})
	if state.sortOpen {
		mobile.ActionSheet(c, &state.sortOpen, mobile.ActionSheetOptions{Title: "排序", Cancel: "取消"}, s.mobileSearchSortActions(state)...)
	}
}

func (s *shell) dismissSearchKeyboard() bool {
	page := s.current()
	state, ok := page.data.(*mobileSearchState)
	if page.kind != pageSearch || !ok {
		return false
	}
	if state.sortOpen {
		state.sortOpen = false
		return true
	}
	if state.inputFocus || page.focus {
		state.inputFocus, page.focus = false, false
		return true
	}
	return false
}

func (s *shell) submitMobileSearch(state *mobileSearchState, keyword string) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		s.clearMobileSearch(state)
		return
	}
	state.draft, state.inputFocus = keyword, false
	if state.preview.keyword != keyword {
		cancelSearchPreview(state)
	} else if state.preview.cancel != nil {
		state.preview.cancel()
		state.preview.cancel = nil
	}
	if state.query == keyword {
		return
	}
	s.resetMobileSearchResults(state)
	state.query, state.tab = keyword, mobileSearchSongs
	if s.app.SettingsSnapshot().ShowSearchHistory {
		state.history.words = slices.DeleteFunc(state.history.words, func(word string) bool { return word == keyword })
		state.history.words = append([]string{keyword}, state.history.words...)
		state.history.words = state.history.words[:min(20, len(state.history.words))]
		state.history.dirty = true
		s.saveMobileSearchHistory(&state.history)
	}
}

func (s *shell) clearMobileSearch(state *mobileSearchState) {
	s.resetMobileSearchResults(state)
	cancelSearchPreview(state)
	state.draft, state.query, state.inputFocus = "", "", false
	s.app.SearchSuggest("")
}

func (s *shell) resetMobileSearchResults(state *mobileSearchState) {
	for _, result := range state.results {
		if result.cancel != nil {
			result.cancel()
		}
	}
	state.results = [mobileSearchTabCount]mobileSearchResult{}
	state.sort = [mobileSearchTabCount]app.SearchSort{}
}

func (s *shell) loadMobileSearchHistory(history *mobileSearchHistory) {
	history.started = true
	if s.win == nil {
		history.loaded = true
		return
	}
	go func() {
		var path string
		var words []string
		if dir, err := mygo.App.Path(mygo.PathUserData); err == nil && dir != "" {
			path = filepath.Join(dir, "mobile-search-history.json")
			if data, err := os.ReadFile(path); err == nil {
				_ = json.Unmarshal(data, &words)
			}
		}
		s.update(func() {
			history.path, history.loaded = path, true
			for _, word := range words {
				if len(history.words) == 20 {
					break
				}
				if word != "" && !slices.Contains(history.words, word) {
					history.words = append(history.words, word)
				}
			}
			s.saveMobileSearchHistory(history)
		})
	}()
}

func (s *shell) saveMobileSearchHistory(history *mobileSearchHistory) {
	if !history.loaded || history.path == "" || history.saving || !history.dirty {
		return
	}
	history.saving, history.dirty = true, false
	path, words := history.path, slices.Clone(history.words)
	go func() {
		writeMobileSearchHistory(path, words)
		s.update(func() {
			history.saving = false
			s.saveMobileSearchHistory(history)
		})
	}()
}

func writeMobileSearchHistory(path string, words []string) {
	file, err := os.CreateTemp(filepath.Dir(path), ".mobile-search-*")
	if err != nil {
		return
	}
	defer os.Remove(file.Name())
	err = json.NewEncoder(file).Encode(words)
	closeErr := file.Close()
	if err == nil && closeErr == nil {
		_ = os.Rename(file.Name(), path)
	}
}

func (s *shell) mobileSearchLanding(c *ui.Context, state *mobileSearchState) {
	if !state.hot.started {
		s.loadSearchHot(&state.hot)
	}
	if !state.mixes.started {
		s.loadDiscoverMixes(&state.mixes)
	}
	if state.history.loaded && s.app.SettingsSnapshot().ShowSearchHistory && len(state.history.words) > 0 {
		s.mobileSearchHistoryView(c, state)
	}
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		sectionHeading(c, "热搜", "")
		if len(state.hot.rows) == 0 {
			if state.hot.loading {
				s.searchHotGrid(c, state, nil)
				return
			}
			browseStatus(c, state.hot.loading, state.hot.err, "暂无热搜", func() { s.loadSearchHot(&state.hot) })
			return
		}
		s.searchHotGrid(c, state, state.hot.rows)
	})
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		sectionHeading(c, "热门歌手", "")
		s.searchSingers(c, state, state.hot.rows)
	})
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		sectionHeading(c, "找张歌单", "")
		s.renderMixes(c, &state.mixes, 3, func() { s.loadDiscoverMixes(&state.mixes) })
	})
}

func (s *shell) mobileSearchHistoryView(c *ui.Context, state *mobileSearchState) {
	palette := tok.Of(c.Theme())
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "搜索历史").FontSize(16).Bold().Grow(1)
			if iconButton(c, "trash", "清空搜索历史").Clicked() {
				state.history.words, state.history.dirty = nil, true
				s.saveMobileSearchHistory(&state.history)
			}
		})
		ui.ScrollHorizontal(c.Key("search:history")).FillWidth().Gap(8).Children(func() {
			for _, word := range state.history.words {
				ui.Row(c).Key("history:" + word).MinHeight(touchSize()).Shrink(0).Radius(24).Background(palette.Surface2).Children(func() {
					if ui.ButtonBase(c).Label(word).Padding(0, 0, 0, 10).Children(func() {
						ui.Text(c, word).FontSize(13).TextColor(palette.Text2).SingleLine().Ellipsis("…").MaxWidth(150)
					}).Clicked() {
						s.submitMobileSearch(state, word)
					}
					if iconButton(c, "x", "删除搜索记录 "+word).Clicked() {
						state.history.words = slices.DeleteFunc(slices.Clone(state.history.words), func(item string) bool { return item == word })
						state.history.dirty = true
						s.saveMobileSearchHistory(&state.history)
					}
				})
			}
		})
	})
}

func (s *shell) loadSearchHot(hot *browseMusic) {
	if hot.loading {
		return
	}
	hot.started, hot.loading = true, true
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
		defer cancel()
		rows, _, err := s.app.RecommendOf().Feed(ctx, app.HomeTabHot, 1)
		s.update(func() { hot.rows, hot.err, hot.loading = rows, err, false })
	}()
}

func (s *shell) searchHotGrid(c *ui.Context, state *mobileSearchState, rows []app.RecommendItem) {
	palette := tok.Of(c.Theme())
	var terms []string
	for _, row := range rows {
		if row.Title != "" && !slices.Contains(terms, row.Title) {
			terms = append(terms, row.Title)
			if len(terms) == 6 {
				break
			}
		}
	}
	if len(terms) == 0 {
		terms = make([]string, 6)
	}
	ui.Row(c).FillWidth().Gap(10).Padding(8, 12).Radius(16).Background(palette.Bg).Children(func() {
		for start := 0; start < len(terms); start += 3 {
			ui.Column(c).Grow(1).Width(0).MinWidth(0).Children(func() {
				for index, term := range terms[start:min(start+3, len(terms))] {
					if term == "" {
						ui.Row(c).FillWidth().MinHeight(touchSize()).AlignItems(ui.Center).Children(func() {
							ui.Box(c).Size(76, 13).Radius(5).Background(palette.Surface2)
						})
						continue
					}
					rank := start + index + 1
					foreground := palette.Text
					if rank <= 3 {
						foreground = palette.AccentText
					}
					if ui.ButtonBase(c).Key("hot:" + term).Label(term).FillWidth().MinHeight(touchSize()).Children(func() {
						ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
							ui.Text(c, strconv.Itoa(rank)).FontSize(13).Bold().TextColor(foreground)
							ui.Text(c, term).Grow(1).MinWidth(0).FontSize(13).Bold().SingleLine().Ellipsis("…")
						})
					}).Clicked() {
						s.submitMobileSearch(state, term)
					}
				}
			})
		}
	})
}

func (s *shell) searchSingers(c *ui.Context, state *mobileSearchState, rows []app.RecommendItem) {
	palette := tok.Of(c.Theme())
	seen := make(map[string]bool, 4)
	ui.Row(c).FillWidth().Gap(6).Children(func() {
		for _, row := range rows {
			name := strings.TrimSpace(row.Owner)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			if ui.ButtonBase(c).Key("singer:" + name).Label("搜索歌手 " + name).Grow(1).Width(0).MinWidth(0).Children(func() {
				ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(7).Children(func() {
					cover(c, row.Cover, 64, 32)
					ui.Text(c, name).FontSize(13).Bold().SingleLine().Ellipsis("…")
				})
			}).Clicked() {
				s.submitMobileSearch(state, name)
			}
			if len(seen) == 4 {
				break
			}
		}
		if len(seen) == 0 {
			if state.hot.loading {
				for range 4 {
					ui.Column(c).Grow(1).Width(0).Gap(7).AlignItems(ui.Center).Children(func() {
						ui.Box(c).Size(64, 64).Radius(32).Background(palette.Surface2)
						ui.Box(c).Size(38, 13).Radius(4).Background(palette.Surface2)
					})
				}
			} else {
				ui.Text(c, "暂无热门歌手").FontSize(13).TextColor(palette.Text2)
			}
		}
	})
}

func (s *shell) mobileSearchSuggestions(c *ui.Context, state *mobileSearchState, keyword string) {
	if delay := state.readyAt.Sub(c.Now()); delay > 0 {
		c.After(delay)
	} else {
		s.app.SearchSuggest(keyword)
		if state.preview.keyword != keyword {
			s.loadSearchPreview(state, keyword)
		}
	}
	if browseAction(c, "search", "搜索「"+keyword+"」", false) {
		s.submitMobileSearch(state, keyword)
	}
	suggestions := s.app.SuggestState()
	if suggestions.Query != keyword {
		return
	}
	if suggestions.Failed {
		ui.Text(c, "暂时无法获取联想词").FontSize(13).TextColor(tok.Of(c.Theme()).Text2)
	}
	for _, word := range suggestions.Items {
		if word != keyword && browseAction(c, "search", word, false) {
			s.submitMobileSearch(state, word)
		}
	}
	if tracks := searchTracks(state.preview.songs.Items); len(tracks) > 0 {
		sectionHeading(c, "歌曲", "")
		s.trackList(c, tracks[:min(6, len(tracks))])
	}
	if users := state.preview.users.Items; len(users) > 0 {
		sectionHeading(c, "UP 主", "")
		s.searchCreatorRows(c, users[:min(2, len(users))])
	}
}

func cancelSearchPreview(state *mobileSearchState) {
	if state.preview.cancel != nil {
		state.preview.cancel()
	}
	state.preview = mobileSearchPreview{}
}

func (s *shell) loadSearchPreview(state *mobileSearchState, keyword string) {
	cancelSearchPreview(state)
	ctx, cancel := context.WithTimeout(s.ctx, 12*time.Second)
	state.preview = mobileSearchPreview{keyword: keyword, cancel: cancel}
	go func() {
		search := s.app.SearchOf()
		songs, songErr := search.Page(ctx, app.SearchQuery{Tab: app.SearchTabSong, Keyword: keyword, Page: 1})
		users, userErr := search.Page(ctx, app.SearchQuery{Tab: app.SearchTabUser, Keyword: keyword, Page: 1})
		songs.Items = appendSearchRows(nil, songs.Items)
		users.Items = appendSearchRows(nil, users.Items)
		s.update(func() {
			defer cancel()
			if ctx.Err() != nil {
				return
			}
			state.preview = mobileSearchPreview{keyword: keyword, songs: songs, users: users, songOK: songErr == nil, userOK: userErr == nil}
		})
	}()
}

func (s *shell) mobileSearchResults(c *ui.Context, state *mobileSearchState) {
	palette := tok.Of(c.Theme())
	ui.Row(c).FillWidth().Children(func() {
		for index, name := range mobileSearchNames {
			tab := mobileSearchTab(index)
			color, line := palette.Text2, ui.Color{}
			if tab == state.tab {
				color, line = palette.AccentText, palette.Accent
			}
			if ui.ButtonBase(c).Key("search:result-tab:" + name).Role(ui.RoleTab).Label(name).AccessSelected(tab == state.tab).Grow(1).Width(0).MinHeight(touchSize()).Children(func() {
				ui.Column(c).FillWidth().Children(func() {
					ui.Box(c).FillWidth().MinHeight(touchSize() - 2).Center().Children(func() { ui.Text(c, name).FontSize(13).Bold().TextColor(color) })
					ui.Box(c).FillWidth().Height(2).Background(line)
				})
			}).Clicked() {
				state.tab = tab
			}
		}
	})
	result := &state.results[state.tab]
	load := func() { s.loadMobileSearchResult(state, state.tab) }
	if !result.started {
		load()
	}
	if len(result.rows) == 0 && len(result.mixes) == 0 {
		if result.loading || result.err != nil {
			var searchErr *app.SearchError
			if errors.As(result.err, &searchErr) && !searchErr.Retryable {
				ui.Text(c, searchErr.Message).FontSize(13).TextColor(palette.Text2)
				return
			}
			browseStatus(c, result.loading, result.err, "", load)
			return
		}
		if state.tab == mobileSearchMixes {
			ui.Text(c, "未在相关 UP 主的公开合集中找到匹配歌单").FontSize(13).TextColor(palette.Text2)
		} else {
			ui.Text(c, "没有找到相关"+mobileSearchNames[state.tab]).FontSize(13).TextColor(palette.Text2)
		}
		if browseAction(c, "x", "清空搜索", false) {
			s.clearMobileSearch(state)
		}
		return
	}
	tracks := searchTracks(result.rows)
	if state.tab != mobileSearchMixes {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			if len(tracks) > 0 && browseAction(c, "play", "播放全部", true) {
				s.play(tracks, 0)
			}
			ui.Spacer(c).Grow(1)
			if iconButton(c, "sort", "排序").Clicked() {
				state.sortOpen = true
			}
		})
	}
	switch state.tab {
	case mobileSearchMixes:
		s.collectionGrid(c, result.mixes)
	case mobileSearchCreators:
		s.searchCreatorRows(c, result.rows)
	case mobileSearchVideos:
		s.searchVideoRows(c, result.rows, tracks)
	default:
		s.trackList(c, tracks)
	}
	if result.more {
		browseMore(c, result.loading, result.err, load)
	}
}

func (s *shell) loadMobileSearchResult(state *mobileSearchState, tab mobileSearchTab) {
	result := &state.results[tab]
	if result.loading || state.query == "" {
		return
	}
	page, keyword, sort := result.page+1, state.query, state.sort[tab]
	if page == 1 && sort == app.SearchSortDefault && state.preview.keyword == keyword {
		preview, available := state.preview.songs, tab == mobileSearchSongs && state.preview.songOK
		if tab == mobileSearchCreators {
			preview, available = state.preview.users, state.preview.userOK
		}
		if available {
			result.started, result.page, result.more = true, page, preview.HasMore
			result.rows = appendSearchRows(nil, preview.Items)
			return
		}
	}
	ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
	result.cancel = cancel
	result.started, result.loading, result.err = true, true, nil
	sources := state.preview
	if sources.keyword != keyword {
		sources = mobileSearchPreview{}
	}
	if result := state.results[mobileSearchSongs]; result.page > 0 && !sources.songOK {
		sources.songs.Items, sources.songOK = result.rows, true
	}
	if result := state.results[mobileSearchCreators]; result.page > 0 && !sources.userOK {
		sources.users.Items, sources.userOK = result.rows, true
	}
	go func() {
		var found app.SearchPage
		var mixes []browseCollection
		var err error
		search := s.app.SearchOf()
		switch tab {
		case mobileSearchMixes:
			mixes, err = s.findSearchCollections(ctx, search, keyword, sources)
		default:
			serverTab := app.SearchTabSong
			if tab == mobileSearchCreators {
				serverTab = app.SearchTabUser
			} else if tab == mobileSearchVideos {
				serverTab = app.SearchTabVideo
			}
			found, err = search.Page(ctx, app.SearchQuery{Tab: serverTab, Keyword: keyword, Sort: sort, Page: page})
		}
		s.update(func() {
			defer cancel()
			if errors.Is(ctx.Err(), context.Canceled) {
				return
			}
			result.cancel = nil
			result.loading, result.err = false, err
			if err == nil {
				result.rows = appendSearchRows(result.rows, found.Items)
				result.mixes = mixes
				result.page, result.more = page, found.HasMore
			}
		})
	}()
}

func appendSearchRows(existing, incoming []app.SearchItem) []app.SearchItem {
	seen := make(map[string]bool, len(existing)+len(incoming))
	for _, row := range existing {
		seen[row.ID] = true
	}
	for _, row := range incoming {
		if row.ID != "" && !seen[row.ID] {
			seen[row.ID] = true
			existing = append(existing, row)
		}
	}
	return existing
}

func (s *shell) findSearchCollections(ctx context.Context, search *app.Search, keyword string, sources mobileSearchPreview) ([]browseCollection, error) {
	creators, songs := sources.users, sources.songs
	var creatorErr, songErr error
	if !sources.userOK {
		creators, creatorErr = search.Page(ctx, app.SearchQuery{Tab: app.SearchTabUser, Keyword: keyword, Page: 1})
	}
	if !sources.songOK {
		songs, songErr = search.Page(ctx, app.SearchQuery{Tab: app.SearchTabSong, Keyword: keyword, Page: 1})
	}
	if creatorErr != nil && songErr != nil {
		return nil, songErr
	}
	owners := make([]browseOwner, 0, len(songs.Items)+len(creators.Items))
	for index, row := range creators.Items {
		mid, _ := strconv.ParseInt(row.Item.Bvid, 10, 64)
		owners = append(owners, browseOwner{mid: mid, name: row.Title})
		if index == 1 {
			break
		}
	}
	for _, row := range songs.Items {
		owners = append(owners, browseOwner{mid: row.OwnerMid, name: row.Owner})
	}
	return s.publicMusicCollections(ctx, owners, keyword)
}

func searchTracks(rows []app.SearchItem) []app.Item {
	tracks := make([]app.Item, 0, len(rows))
	for _, row := range rows {
		if row.Item.Type != app.ItemTypeUser && row.Item.Bvid != "" {
			tracks = append(tracks, row.Item)
		}
	}
	return tracks
}

func (s *shell) searchCreatorRows(c *ui.Context, rows []app.SearchItem) {
	palette := tok.Of(c.Theme())
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		for start := 0; start < len(rows); start += 2 {
			ui.Row(c).FillWidth().Gap(8).Children(func() {
				for _, row := range rows[start:min(start+2, len(rows))] {
					if ui.ButtonBase(c).Key("creator:" + row.ID).Label("打开 " + row.Title + " 的空间").Grow(1).Width(0).MinWidth(0).
						MinHeight(112).Padding(10).Radius(12).Background(palette.Bg).Children(func() {
						ui.Column(c).FillWidth().Gap(4).Children(func() {
							ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
								cover(c, row.Cover, 42, 21)
								ui.Spacer(c).Grow(1)
								if row.Level > 0 {
									ui.Text(c, fmt.Sprintf("LV%d", row.Level)).FontSize(11).TextColor(palette.Text2)
								}
							})
							ui.Text(c, row.Title).FontSize(14).Bold().SingleLine().Ellipsis("…")
							ui.Text(c, fmt.Sprintf("%d 粉丝", row.Fans)).FontSize(12).TextColor(palette.Text2)
							if row.Owner != "" {
								ui.Text(c, row.Owner).FontSize(12).TextColor(palette.Text2).SingleLine().Ellipsis("…")
							}
						})
					}).Clicked() {
						s.dismissSearchKeyboard()
						s.push(route{kind: pageCreator, id: row.Item.Bvid, title: row.Title})
					}
				}
				if len(rows)-start == 1 {
					ui.Spacer(c).Grow(1).Width(0)
				}
			})
		}
	})
}

func (s *shell) searchVideoRows(c *ui.Context, rows []app.SearchItem, tracks []app.Item) {
	palette := tok.Of(c.Theme())
	mode := s.app.SettingsSnapshot().DisplayMode
	width, _ := c.Size()
	mediaWidth, mediaHeight := float32(48), float32(48)
	if mode == app.DisplayModeCard {
		mediaWidth = max(1, width-32-touchSize())
		mediaHeight = mediaWidth * 9 / 16
	} else if mode == app.DisplayModeCompact {
		mediaWidth, mediaHeight = 32, 32
	}
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		for index, row := range rows {
			ui.Row(c).Key("video:" + row.ID).FillWidth().AlignItems(ui.End).Children(func() {
				button := ui.ButtonBase(c).Label("播放 " + row.Title).Grow(1).Width(0).MinHeight(touchSize()).Radius(8).Children(func() {
					var layout ui.Element
					if mode == app.DisplayModeCard {
						layout = ui.Column(c).FillWidth().Gap(6)
					} else {
						layout = ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10)
					}
					layout.Children(func() {
						media := ui.Box(c).Size(mediaWidth, mediaHeight).Shrink(0).Radius(8).Clip().Center()
						if mode == app.DisplayModeCard {
							media = media.FillWidth()
						}
						media.Children(func() { cover(c, row.Cover, mediaWidth, 0) })
						ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
							ui.Text(c, row.Title).FontSize(13).Bold().SingleLine().Ellipsis("…")
							ui.Text(c, fmt.Sprintf("%s · %d 播放", row.Owner, row.View)).FontSize(12).TextColor(palette.Text2).SingleLine().Ellipsis("…")
						})
					})
				})
				if button.Clicked() {
					s.playSearchVideo(tracks, index)
				}
				more := iconButton(c, "more", "更多操作："+row.Title)
				if button.LongPressed() || more.Clicked() {
					s.player.target = row.Item
					s.player.show(sheetTrack)
				}
			})
		}
	})
}

func (s *shell) playSearchVideo(tracks []app.Item, index int) {
	if err := s.app.PlayFromIndex(tracks, index); err != nil {
		s.report(err)
		return
	}
	if s.app.VideoAvailable() {
		s.player.visible = true
		s.app.StartVideo()
	}
}

func (s *shell) mobileSearchSortActions(state *mobileSearchState) []mobile.Action {
	labels := []string{"综合排序", "播放最多", "最新发布", "弹幕最多", "收藏最多"}
	if state.tab == mobileSearchCreators {
		labels = []string{"默认排序", "粉丝最多", "粉丝最少", "等级最高", "等级最低"}
	}
	tab := state.tab
	actions := make([]mobile.Action, 0, len(labels))
	for index, label := range labels {
		sort := app.SearchSort(index)
		if sort == state.sort[tab] {
			label += "（当前）"
		}
		actions = append(actions, mobile.Action{Label: label, OnTap: func() {
			if sort == state.sort[tab] {
				return
			}
			result := &state.results[tab]
			if result.cancel != nil {
				result.cancel()
			}
			*result = mobileSearchResult{}
			state.sort[tab] = sort
		}})
	}
	return actions
}
