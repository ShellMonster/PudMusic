package mobileui

import (
	"fmt"
	"strings"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/egoist/mygo/ui"
)

type playerCommentView struct {
	track, draft, replyName  string
	account                  app.AccountSnapshot
	root, parent             int64
	thread                   bili.Comment
	focused, sending, filter bool
	likes                    map[int64]bool
	pending                  map[int64]bool
	expanded                 map[int64]bool
}

func (s *shell) playerComments(c *ui.Context, replies bool) {
	item, _ := s.app.CurrentItem()
	account := s.app.AccountSnapshot()
	view := &s.player.comments
	if view.track != item.Identity() || view.account != account {
		wasOpen := view.track != ""
		*view = playerCommentView{track: item.Identity(), account: account, likes: map[int64]bool{}, pending: map[int64]bool{}, expanded: map[int64]bool{}}
		if wasOpen && replies {
			s.player.back()
			return
		}
	}
	if replies {
		s.commentRow(c, item, view.thread, true)
		replies := s.app.CommentReplies(view.thread.Rpid)
		if replies == nil {
			ui.Spinner(c).Label("加载评论回复")
		}
		for _, comment := range replies {
			s.commentRow(c, item, comment, true)
		}
		return
	}
	s.app.EnsureComments()
	ui.Text(c, "这首歌的评论 · "+item.Title).FontSize(12).TextColor(phonePalette(c).muted).MaxLines(1).Ellipsis("…")
	ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.Center).Children(func() {
		for _, choice := range []struct {
			value bili.CommentSort
			label string
		}{{bili.CommentSortHot, "最热"}, {bili.CommentSortLatest, "最新"}} {
			label := choice.label
			selected := choice.value == s.app.CommentsSort()
			if selected {
				label = "✓ " + label
			}
			playerTool(c, "sort", label).AccessSelected(selected).OnClick(func() { s.app.SetCommentsSort(choice.value) })
		}
		playerTool(c, "search", "仅看提及歌名").AccessSelected(view.filter).OnClick(func() { view.filter = !view.filter })
		iconButton(c, "refresh", "刷新评论").OnClick(s.app.RefreshComments)
	})
	comments := s.app.Comments()
	switch {
	case len(comments) > 0:
		visible := 0
		for _, comment := range comments {
			if !view.filter || strings.Contains(comment.Message, item.Title) {
				s.commentRow(c, item, comment, false)
				visible++
			}
		}
		if visible == 0 {
			ui.Text(c, "当前没有提及歌名的评论").FontSize(13)
		}
	case s.app.CommentsLoading():
		ui.Spinner(c).Label("加载评论")
	case s.app.CommentsErr() != nil:
		ui.Text(c, s.app.CommentsErr().Error()).FontSize(13).MaxLines(3)
	default:
		ui.Text(c, "暂无评论").FontSize(13)
	}
	if s.app.CommentsHasMore() {
		sheetAction(c, "down", "加载更多评论", "", false, func() { s.app.LoadMoreComments() })
	}
}

func (s *shell) commentRow(c *ui.Context, item app.Item, comment bili.Comment, inThread bool) {
	palette := phonePalette(c)
	view := &s.player.comments
	ui.Row(c.Key(comment.Rpid)).FillWidth().Gap(9).PaddingY(8).Children(func() {
		cover(c, comment.Avatar, 34, 17)
		ui.Column(c).Grow(1).Basis(0).Gap(4).Children(func() {
			ui.Text(c, comment.Uname).FontSize(13).FontWeight(600).MaxLines(1)
			message := ui.Text(c, comment.Message).FontSize(13).Wrap()
			if !view.expanded[comment.Rpid] && !inThread {
				message.MaxLines(3).Ellipsis("…")
			}
			if len([]rune(comment.Message)) > 90 && !inThread {
				label := "展开"
				if view.expanded[comment.Rpid] {
					label = "收起"
				}
				playerTool(c, "down", label).OnClick(func() { view.expanded[comment.Rpid] = !view.expanded[comment.Rpid] })
			}
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Text(c, time.Unix(comment.Ctime, 0).Format("01-02")).FontSize(12).TextColor(palette.muted)
				s.commentLike(c, item, comment)
				playerTool(c, "comment", "回复").OnClick(func() {
					if !s.app.LoggedIn() {
						s.openPlayerLogin()
						return
					}
					view.root = comment.Root
					if view.root == 0 {
						view.root = comment.Rpid
					}
					view.parent, view.replyName, view.focused = comment.Rpid, comment.Uname, true
				})
			})
			if !inThread && comment.Count > 0 {
				playerTool(c, "down", fmt.Sprintf("%d 条回复", comment.Count)).OnClick(func() {
					view.thread = comment
					s.player.show(sheetReplies)
				})
			}
		})
	})
}

func (s *shell) commentLike(c *ui.Context, item app.Item, comment bili.Comment) {
	view := &s.player.comments
	liked, exists := view.likes[comment.Rpid]
	if !exists {
		liked = comment.Liked
	}
	icon, count := "thumb", comment.Like
	if liked {
		icon = "thumb-on"
	}
	if liked && !comment.Liked {
		count++
	}
	if !liked && comment.Liked {
		count--
	}
	playerTool(c, icon, fmt.Sprintf("%d 赞", max(0, count))).AccessSelected(liked).Disabled(view.pending[comment.Rpid]).OnClick(func() {
		if !s.app.LoggedIn() {
			s.openPlayerLogin()
			return
		}
		view.pending[comment.Rpid] = true
		account, track := view.account, view.track
		go func() {
			err := s.app.LikeComment(s.ctx, account, item, comment.Rpid, !liked)
			s.update(func() {
				if view.track != track || view.account != account {
					return
				}
				delete(view.pending, comment.Rpid)
				if err == nil {
					view.likes[comment.Rpid] = !liked
				} else {
					s.notice = bili.Friendly(err)
				}
			})
		}()
	})
}

func (s *shell) commentComposer(c *ui.Context) {
	view := &s.player.comments
	if !s.app.LoggedIn() {
		sheetAction(c, "user", "登录后发表评论", "", false, s.openPlayerLogin)
		return
	}
	placeholder := "写评论…"
	if view.replyName != "" {
		placeholder = "回复 " + view.replyName
	}
	ui.Row(c).FillWidth().MinHeight(touchSize()).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.TextInputBase(c, &view.draft).Label("评论内容").Placeholder(placeholder).Grow(1).Basis(0).FontSize(13).
			MinHeight(touchSize()).FocusBind(&view.focused, false).OnSubmit(s.submitMobileComment)
		if view.replyName != "" {
			iconButton(c, "x", "取消回复").OnClick(func() { view.root, view.parent, view.replyName = 0, 0, "" })
		}
		playerTool(c, "comment", "发送").Disabled(view.sending || strings.TrimSpace(view.draft) == "").OnClick(s.submitMobileComment)
	})
}

func (s *shell) submitMobileComment() {
	view := &s.player.comments
	message := strings.TrimSpace(view.draft)
	item, ok := s.app.CurrentItem()
	if !ok || item.Identity() != view.track || view.sending || message == "" {
		return
	}
	root, parent, account, track := view.root, view.parent, view.account, view.track
	if root == 0 && s.player.sheet == sheetReplies {
		root, parent = view.thread.Rpid, view.thread.Rpid
	}
	view.sending = true
	go func() {
		err := s.app.AddComment(s.ctx, account, item, root, parent, message)
		s.update(func() {
			if view.track != track || view.account != account {
				return
			}
			view.sending = false
			if err != nil {
				s.notice = bili.Friendly(err)
				return
			}
			if strings.TrimSpace(view.draft) == message {
				view.draft = ""
			}
			view.root, view.parent, view.replyName, view.focused = 0, 0, "", false
			s.notice = "已发布"
		})
	}()
}

func (s *shell) openPlayerLogin() {
	s.closePlayerSheets()
	s.player.visible = false
	s.app.StopVideo()
	s.push(route{kind: pageLogin, title: "登录"})
}
