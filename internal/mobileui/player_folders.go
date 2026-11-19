package mobileui

import (
	"context"
	"fmt"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/egoist/mygo/ui"
)

type playerFolderView struct {
	item            app.Item
	account         app.AccountSnapshot
	target          app.TrackFolders
	picked          map[int64]bool
	loading, saving bool
	err             string
	editor          folderEditor
}

func (s *shell) openPlayerFolders(item app.Item) {
	if !s.app.LoggedIn() {
		s.openPlayerLogin()
		return
	}
	view := &playerFolderView{item: item, account: s.app.AccountSnapshot(), picked: map[int64]bool{}}
	s.player.folders = view
	s.player.show(sheetTrackFolders)
	s.loadPlayerFolders(view)
}

func (s *shell) loadPlayerFolders(view *playerFolderView) {
	view.loading, view.err = true, ""
	go func() {
		target, err := s.app.LoadTrackFolders(s.ctx, view.account, view.item)
		s.update(func() {
			if s.player.folders != view {
				return
			}
			view.loading = false
			if err != nil {
				view.err = bili.Friendly(err)
				return
			}
			for _, folder := range target.Choices {
				if _, chose := view.picked[folder.ID]; !chose {
					view.picked[folder.ID] = folder.Faved
				}
			}
			view.target = target
		})
	}()
}

func (s *shell) playerFolderChoices(c *ui.Context) {
	view := s.player.folders
	if view == nil {
		return
	}
	ui.Text(c, view.item.Title).FontSize(13).MaxLines(2)
	if view.account != s.app.AccountSnapshot() {
		ui.Text(c, "登录状态已变化，请重新打开收藏夹选择。").FontSize(13)
		return
	}
	if view.loading {
		ui.Spinner(c).Label("正在加载收藏夹")
		return
	}
	if view.err != "" {
		ui.Text(c, view.err).FontSize(13).Wrap()
	}
	if view.target.RID == 0 {
		sheetAction(c, "refresh", "重试", "", false, func() { s.loadPlayerFolders(view) })
		return
	}
	if !view.saving {
		sheetAction(c, "plus", "新建收藏夹", "", false, func() { view.editor = folderEditor{open: true} })
	}
	var add, remove []int64
	for _, folder := range view.target.Choices {
		picked := view.picked[folder.ID]
		if picked && !folder.Faved {
			add = append(add, folder.ID)
		}
		if !picked && folder.Faved {
			remove = append(remove, folder.ID)
		}
		ui.Box(c.Key(folder.ID)).FillWidth().Disabled(view.saving).Children(func() {
			sheetAction(c, "folder", folder.Title, fmt.Sprintf("%d 首", folder.MediaCount), picked, func() {
				if !view.saving {
					view.picked[folder.ID] = !picked
				}
			})
		})
	}
	settingsAction(c, "保存收藏夹选择", true, true).Disabled(view.saving || len(add)+len(remove) == 0).OnClick(func() {
		view.saving, view.err = true, ""
		target := view.target
		go func(ctx context.Context) {
			err := s.app.SaveTrackFolders(ctx, view.account, target, add, remove)
			s.update(func() {
				if s.player.folders != view {
					return
				}
				view.saving = false
				if err != nil {
					view.err = bili.Friendly(err)
					return
				}
				if s.player.sheet == sheetTrackFolders {
					s.player.back()
				}
				s.notice = "收藏夹已更新"
			})
		}(s.ctx)
	})
}
