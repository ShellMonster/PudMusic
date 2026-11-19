package mobileui

import (
	"cmp"
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/mobile"
)

type collectionState struct {
	pages                                 libraryPages[bili.Media]
	sort                                  bili.CollectionSort
	keyword, query, status, infoError     string
	fallbackCover                         string
	filter, menu, sorting, batch, confirm bool
	deleteFolder                          bool
	working, following                    bool
	selected                              map[collectionMediaKey]bool
	editor                                folderEditor
}

type collectionMediaKey struct {
	id   int64
	kind int
	bvid string
}

func mediaKey(item bili.Media) collectionMediaKey {
	return collectionMediaKey{item.ID, item.Type, item.Bvid}
}

func (s *shell) collection(c *ui.Context, page *route) {
	ref := page.ref
	if ref.ID <= 0 {
		libraryMessage(c, "这一页走丢了", "找不到这张歌单。")
		return
	}
	if ref.Type == 0 {
		ref.Type = bili.CollectionFavorite
	}
	state, _ := page.data.(*collectionState)
	if state == nil || state.pages.account != s.app.AccountSnapshot() {
		state = &collectionState{}
		page.data = state
		s.app.LoadCollection(ref)
		s.collectionPage(state, ref, true)
	}
	for {
		result, ok := s.app.TakeCollectionNotice(ref)
		if !ok {
			break
		}
		state.following = false
		if result.Kind == app.NoticeCollectionChanged {
			s.collectionPage(state, ref, true)
		} else if result.Tone == notice.Danger && !s.app.Collection(ref).Loaded {
			state.infoError = result.Text
		} else {
			state.status = result.Text
		}
	}
	info := s.app.Collection(ref)
	if info.Private && (!s.app.LoggedIn() || info.OwnerMid != s.app.SelfMid()) {
		if s.app.LoggedIn() {
			libraryMessage(c, "这张歌单仅自己可见", "")
		} else {
			s.libraryLogin(c, "私人收藏夹")
		}
		return
	}
	title := info.Title
	if title == "" {
		title = page.title
	}
	if title == "" {
		title = "歌单"
	}
	if state.fallbackCover == "" && state.query == "" {
		for _, item := range state.pages.items {
			if item.Playable() && !item.Invalid() && item.Cover != "" {
				state.fallbackCover = item.Cover
				break
			}
		}
	}
	imageURL := info.Cover
	if imageURL == "" {
		imageURL = state.fallbackCover
	}
	ui.Row(c).AlignItems(ui.Center).Gap(14).Margin(10, 0, 0).Children(func() {
		cover(c, imageURL, 122, 16)
		ui.Column(c).Grow(1).Gap(6).Children(func() {
			ui.Text(c, title).Bold().FontSize(21).MaxLines(2)
			if info.OwnerName != "" {
				ui.Row(c).Gap(5).AlignItems(ui.Center).MinHeight(30).Label(info.OwnerName).OnClick(func() { s.openCreator(info.OwnerMid, info.OwnerName) }).Children(func() {
					cover(c, info.OwnerFace, 22, 11)
					ui.Text(c, info.OwnerName).FontSize(13).MaxLines(1)
				})
			}
			kind := ref.Type.Label()
			if ref.Type == bili.CollectionFavorite {
				kind = "歌单"
			}
			if info.Private {
				kind += " · 私密"
			}
			if info.Loaded {
				kind += fmt.Sprintf(" · %d 首", info.MediaCount)
			}
			ui.Text(c, kind).FontSize(12).TextColor(c.Theme().TextMuted)
			if info.PlayCount > 0 || info.FavCount > 0 {
				ui.Text(c, fmt.Sprintf("%d 播放 · %d 收藏", info.PlayCount, info.FavCount)).FontSize(12).TextColor(c.Theme().TextMuted)
			}
		})
	})
	if info.Intro != "" {
		ui.Text(c, info.Intro).FontSize(14).TextColor(c.Theme().TextMuted).Margin(4, 0, 8)
	}
	medias := collectionMatches(ref, state.pages.items, state.query)
	playable := app.QueueItemsOfMedias(medias)
	owned := info.OwnerMid > 0 && info.OwnerMid == s.app.SelfMid()
	canEdit := info.Loaded && owned && ref.Type == bili.CollectionFavorite
	ui.Row(c).AlignItems(ui.Center).Gap(5).Margin(3, 0, 0).Children(func() {
		if state.selected == nil {
			label := "播放全部"
			if state.query == "" && info.Loaded {
				label += fmt.Sprintf(" · %d 首", info.MediaCount)
			} else if !state.pages.more {
				label += fmt.Sprintf(" · %d 首", len(playable))
			}
			libraryButton(c, "play", label, true, state.working || state.pages.loading && len(medias) == 0 || state.pages.err != nil || len(playable) == 0 && !state.pages.more, func() { s.playCollection(state, ref, false) })
			ui.Spacer(c).Grow(1)
			if info.Loaded && !owned && s.app.LoggedIn() {
				icon, label := "star", "收藏歌单"
				if info.Faved {
					icon, label = "star-on", "取消收藏"
				}
				iconButton(c, icon, label).Disabled(state.following).OnClick(func() {
					state.following = true
					s.app.SetFavorite(ref, !info.Faved)
				})
			}
			iconButton(c, "more", "歌单更多").OnClick(func() { state.menu = true })
		} else {
			libraryButton(c, "", "全选本页", false, len(medias) == 0, func() {
				for _, item := range medias {
					state.selected[mediaKey(item)] = true
				}
			})
			libraryButton(c, "", fmt.Sprintf("已选 %d 首", len(state.selected)), true, len(state.selected) == 0 || state.working, func() { state.batch = true })
			ui.Spacer(c).Grow(1)
			libraryButton(c, "", "完成", false, false, func() { state.selected = nil })
		}
	})
	if state.filter {
		ui.Row(c).AlignItems(ui.Center).Gap(5).Children(func() {
			libraryInput(c, &state.keyword, "在这张歌单里找歌", func() {
				state.query = strings.TrimSpace(state.keyword)
				s.collectionPage(state, ref, true)
			})
			iconButton(c, "x", "关闭筛选").OnClick(func() {
				state.filter = false
				state.keyword, state.query = "", ""
				state.selected = nil
				s.collectionPage(state, ref, true)
			})
		})
	}
	if state.status != "" {
		ui.Text(c, state.status).FontSize(13).TextColor(c.Theme().TextMuted)
	}
	if state.pages.err != nil || (state.infoError != "" && len(medias) == 0) {
		libraryMessage(c, "加载歌单失败", state.infoError)
		libraryButton(c, "refresh", "重试", false, false, func() {
			state.infoError = ""
			s.app.LoadCollection(ref)
			s.collectionPage(state, ref, len(state.pages.items) == 0)
		})
	}
	if !state.pages.loading && state.pages.err == nil && state.infoError == "" && len(medias) == 0 {
		switch {
		case state.query != "" && !state.pages.more:
			libraryMessage(c, "列表里没有匹配的歌曲", "试试其他歌名或 UP 主昵称。")
			libraryButton(c, "x", "清空筛选", false, false, func() {
				state.keyword, state.query = "", ""
				s.collectionPage(state, ref, true)
			})
		case state.query == "":
			libraryMessage(c, "这张歌单还没有歌曲", "在歌曲的更多操作里选择「添加到收藏夹」。")
			libraryButton(c, "disc", "去发现音乐", false, false, func() { s.push(route{kind: pageMixes, title: "推荐歌单"}) })
		}
	}
	if state.selected == nil {
		if len(playable) == len(medias) {
			ui.Column(c).Gap(2).Children(func() { s.trackList(c, playable) })
		} else {
			playIndex := 0
			textSize := interfaceTextSize(s.app.SettingsSnapshot().FontScale)
			active, activeOK := s.app.Queue().Current()
			for _, item := range medias {
				if item.Playable() && !item.Invalid() {
					s.trackRow(c.Key(mediaKey(item)), playable, playIndex, 38, 54, textSize, activeOK && active.Identity() == playable[playIndex].Identity())
					playIndex++
				} else {
					itemRef := app.ItemOfMedia(item)
					if itemRef.CollectionID > 0 && !item.Invalid() {
						libraryRow(c.Key(mediaKey(item)), item.Cover, "folder", item.Title, "合集", func() {
							s.push(route{kind: pageCollection, title: item.Title, ref: app.CollectionRef{ID: itemRef.CollectionID, Type: bili.CollectionAlbum, OwnerMid: item.OwnerMid}})
						})
					} else {
						libraryRow(c.Key(mediaKey(item)), item.Cover, "disc", item.Title, "此内容无法播放", nil)
					}
				}
			}
		}
	} else {
		for _, item := range medias {
			key := mediaKey(item)
			label := "选择 " + item.Title
			icon := "plus"
			if state.selected[key] {
				label, icon = "取消选择 "+item.Title, "check"
			}
			libraryRow(c.Key(key), "", icon, label, item.Owner, func() {
				if state.selected[key] {
					delete(state.selected, key)
				} else {
					state.selected[key] = true
				}
			})
		}
	}
	if state.pages.loading || state.pages.more && state.pages.err == nil {
		libraryMore(c, state.pages.loading, func() { s.collectionPage(state, ref, false) })
	}
	options := []mobile.Action{
		{Label: "随机播放", Disabled: len(playable) == 0 && (!state.pages.more || state.pages.loading) || state.working, OnTap: func() { s.playCollection(state, ref, true) }},
		{Label: "搜索歌单", OnTap: func() { state.filter = true }},
		{Label: "排序", OnTap: func() { state.sorting = true }},
		{Label: "多选", Disabled: len(medias) == 0, OnTap: func() { state.selected = make(map[collectionMediaKey]bool) }},
	}
	if canEdit {
		options = append(options, mobile.Action{Label: "修改收藏夹", OnTap: func() {
			state.editor = folderEditor{open: true, id: ref.ID, title: info.Title, intro: info.Intro, private: info.Private, cover: info.Cover}
		}})
	}
	if canEdit && s.app.CanDeleteCollection(ref) {
		options = append(options, mobile.Action{Label: "删除收藏夹", Destructive: true, Disabled: state.working, OnTap: func() { state.deleteFolder = true }})
	}
	libraryActionSheet(c, &state.menu, mobile.ActionSheetOptions{Title: title, Cancel: "取消"}, options...)
	sorts := make([]mobile.Action, 0, len(bili.CollectionSorts))
	for _, order := range bili.CollectionSorts {
		label := order.Label()
		if ref.Type != bili.CollectionFavorite && order == bili.SortByFavTime {
			label = "默认顺序"
		}
		sorts = append(sorts, mobile.Action{Label: label, OnTap: func() {
			state.sort = order
			state.selected = nil
			s.collectionPage(state, ref, true)
		}})
	}
	libraryActionSheet(c, &state.sorting, mobile.ActionSheetOptions{Title: "排序", Cancel: "取消"}, sorts...)
	selected := make([]bili.Media, 0, len(state.selected))
	for _, item := range medias {
		if state.selected[mediaKey(item)] {
			selected = append(selected, item)
		}
	}
	actions := []mobile.Action{
		{Label: "加入队列", Disabled: len(app.QueueItemsOfMedias(selected)) == 0, OnTap: func() {
			if err := s.app.Enqueue(app.QueueItemsOfMedias(selected)...); err != nil {
				state.status = err.Error()
			} else {
				state.status = "已加入播放队列"
				state.selected = nil
			}
		}},
		{Label: "下载音频", Disabled: !s.app.DownloaderMounted(), OnTap: func() {
			s.collectionAction(state, "已加入下载队列", func(ctx context.Context) error { return s.app.DownloadMedias(ctx, ref, selected) })
		}},
	}
	if canEdit {
		actions = append(actions, mobile.Action{Label: "从收藏夹移除", Destructive: true, OnTap: func() { state.confirm = true }})
	}
	libraryActionSheet(c, &state.batch, mobile.ActionSheetOptions{Title: fmt.Sprintf("已选 %d 首", len(selected)), Cancel: "取消"}, actions...)
	libraryActionSheet(c, &state.confirm, mobile.ActionSheetOptions{Title: "从收藏夹移除？", Message: fmt.Sprintf("将移除 %d 项内容。", len(selected)), Cancel: "取消"}, mobile.Action{
		Label: "移除", Destructive: true, OnTap: func() {
			s.collectionAction(state, "已从收藏夹移除", func(ctx context.Context) error { return s.app.RemoveMedias(ctx, ref, selected) })
		},
	})
	libraryActionSheet(c, &state.deleteFolder, mobile.ActionSheetOptions{Title: "删除收藏夹？", Message: title + "将从 B 站账号中删除。", Cancel: "取消"}, mobile.Action{
		Label: "删除收藏夹", Destructive: true, OnTap: func() {
			if state.working || !s.app.CanDeleteCollection(ref) {
				return
			}
			state.working = true
			s.app.DeleteCollectionFolder(ref, title, func(text string, ok bool, _ notice.Tone) {
				s.update(func() {
					state.working = false
					if ok && s.current() == page {
						s.back()
					}
					s.notice = text
				})
			})
		},
	})
	s.renderFolderEditor(c, &state.editor, func() { s.app.LoadCollection(ref) })
}

func collectionMatches(ref app.CollectionRef, items []bili.Media, query string) []bili.Media {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" || ref.Type == bili.CollectionFavorite {
		return items
	}
	var out []bili.Media
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.Title+" "+item.Owner), query) {
			out = append(out, item)
		}
	}
	return out
}

func (s *shell) collectionPage(state *collectionState, ref app.CollectionRef, reset bool) {
	keyword, order := state.query, state.sort
	if reset {
		state.selected = nil
	}
	loadLibraryPage(s, &state.pages, reset, func(ctx context.Context, page int) ([]bili.Media, bool, error) {
		if ref.Type == bili.CollectionFavorite {
			return s.app.LoadCollectionPage(ctx, ref, order, keyword, page)
		}
		if keyword == "" && order == bili.SortByFavTime {
			return s.app.LoadCollectionPage(ctx, ref, order, "", page)
		}
		var items []bili.Media
		for {
			rows, more, err := s.app.LoadCollectionPage(ctx, ref, order, "", page)
			if err != nil {
				return nil, false, err
			}
			items = append(items, rows...)
			if !more || len(rows) == 0 {
				break
			}
			page++
		}
		items = collectionMatches(ref, items, keyword)
		switch order {
		case bili.SortByPlay:
			slices.SortStableFunc(items, func(left, right bili.Media) int { return cmp.Compare(right.View, left.View) })
		case bili.SortByPubTime:
			slices.SortStableFunc(items, func(left, right bili.Media) int { return right.PubTime.Compare(left.PubTime) })
		}
		return items, false, nil
	})
}

func (s *shell) playCollection(state *collectionState, ref app.CollectionRef, shuffle bool) {
	if state.working {
		return
	}
	state.working = true
	s.collectionPlayRequest++
	request := s.collectionPlayRequest
	queueRevision := s.app.Queue().Snapshot().Revision()
	account := s.app.AccountSnapshot()
	order, query := state.sort, state.query
	items, more, next := slices.Clone(state.pages.items), state.pages.more, state.pages.next
	go func() {
		var err error
		for more && err == nil {
			var page []bili.Media
			keyword := query
			if ref.Type != bili.CollectionFavorite {
				keyword = ""
			}
			page, more, err = s.app.LoadCollectionPage(s.ctx, ref, order, keyword, next)
			items = append(items, page...)
			next++
		}
		playable := app.QueueItemsOfMedias(collectionMatches(ref, items, query))
		if shuffle {
			rand.Shuffle(len(playable), func(left, right int) { playable[left], playable[right] = playable[right], playable[left] })
		}
		s.update(func() {
			state.working = false
			if s.app.AccountSnapshot() != account || s.collectionPlayRequest != request || s.app.Queue().Snapshot().Revision() != queueRevision {
				return
			}
			if err != nil {
				state.status = "歌单没有加载完，请重试。"
			} else if len(playable) > 0 {
				s.play(playable, 0)
			} else {
				state.status = "这张歌单没有可播放的歌曲。"
			}
		})
	}()
}

func (s *shell) collectionAction(state *collectionState, success string, action func(context.Context) error) {
	if state.working {
		return
	}
	state.working = true
	go func() {
		err := action(s.ctx)
		s.update(func() {
			state.working = false
			if err != nil {
				state.status = err.Error()
				return
			}
			state.selected, state.status = nil, success
		})
	}()
}

type folderEditor struct {
	open, saving, private bool
	id                    int64
	title, intro, status  string
	cover                 string
	crop                  folderCoverCrop
}

func (s *shell) renderFolderEditor(c *ui.Context, state *folderEditor, saved func()) {
	if state.crop.uploading {
		if imageURL, ok := s.app.TakeCoverURL(); ok {
			state.cover = imageURL
			state.crop = folderCoverCrop{}
		}
	}
	if state.saving || state.crop.uploading {
		for {
			result, ok := s.app.TakeFolderResult()
			if !ok {
				break
			}
			if state.crop.uploading && result.Op == app.FolderOpCover {
				state.crop.uploading, state.crop.status = false, result.Text
				break
			}
			if !state.saving {
				continue
			}
			if state.id == 0 && result.Op != app.FolderOpCreate || state.id != 0 && result.Op != app.FolderOpEdit {
				continue
			}
			state.saving, state.status = false, result.Text
			if result.OK {
				state.open = false
				saved()
			}
			break
		}
	}
	title := "新建收藏夹"
	if state.id != 0 {
		title = "修改收藏夹"
	}
	librarySheet(c, &state.open, mobile.SheetOptions{Label: title, Detents: []float32{0.67, 0.92}, NoDismiss: state.saving || state.crop.picking || state.crop.uploading}, func() {
		ui.Scroll(c).Fill().Padding(16).Gap(10).Children(func() {
			ui.Text(c, title).Bold().FontSize(20)
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				cover(c, state.cover, 50, 8)
				libraryButton(c, "image", "选择封面", false, state.saving || state.crop.picking || state.crop.uploading || !s.app.CollectionsMounted(), func() { s.pickFolderCover(state) })
			})
			ui.TextInput(c, &state.title).Placeholder("名称（必填，最多 20 字）").Label("收藏夹名称").Height(44)
			ui.TextArea(c, &state.intro).Placeholder("简介（最多 200 字）").Label("收藏夹简介").Height(90)
			ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
				ui.Text(c, "设为私密").Grow(1)
				ui.Switch(c, &state.private).Label("设为私密")
			})
			if state.status != "" {
				ui.Text(c, state.status).FontSize(13).TextColor(c.Theme().Danger)
			}
			ui.Row(c).Gap(8).Children(func() {
				libraryButton(c, "", "取消", false, state.saving || state.crop.picking || state.crop.uploading, func() { state.open = false })
				libraryButton(c, "", "保存", true, state.saving || state.crop.picking || state.crop.uploading || strings.TrimSpace(state.title) == "", func() {
					draft := bili.FolderDraft{Title: state.title, Intro: state.intro, Cover: state.cover}
					if state.private {
						draft.Privacy = 1
					}
					state.saving = true
					if state.id == 0 {
						s.app.CreateFolder(draft)
					} else {
						s.app.EditFolder(state.id, draft)
					}
				})
			})
		})
	})
	s.folderCoverSheet(c, state)
}

func (s *shell) openCreator(mid int64, title string) {
	if mid > 0 {
		s.push(route{kind: pageCreator, id: strconv.FormatInt(mid, 10), title: title})
	}
}
