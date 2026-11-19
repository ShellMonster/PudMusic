package app

import (
	"context"
	"errors"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

func (a *App) AddComment(ctx context.Context, account AccountSnapshot, item Item, root, parent int64, message string) error {
	err := a.writeComment(ctx, account, item, func(client *bili.Client, kind, object int64) error {
		return client.AddComment(ctx, kind, object, root, parent, message)
	})
	if err == nil {
		slot := commentSlotKey(item)
		a.commentsMu.Lock()
		if a.commentsSlot == slot && root > 0 {
			delete(a.commentsReplies, root)
		}
		a.commentsMu.Unlock()
		if a.CommentsSlot() == slot {
			a.RefreshComments()
		}
	}
	return err
}

func (a *App) LikeComment(ctx context.Context, account AccountSnapshot, item Item, reply int64, liked bool) error {
	return a.writeComment(ctx, account, item, func(client *bili.Client, kind, object int64) error {
		return client.LikeComment(ctx, kind, object, reply, liked)
	})
}

func (a *App) writeComment(ctx context.Context, account AccountSnapshot, item Item, send func(*bili.Client, int64, int64) error) error {
	client := a.commentsClient()
	if client == nil || account.SelfMid <= 0 || !a.LoggedIn() {
		return errors.New("登录后才能操作评论")
	}
	a.commentsMu.Lock()
	fetch := a.commentsArchiveFetch
	a.commentsMu.Unlock()
	kind, object, supported, err := a.resolveCommentTarget(ctx, item, fetch)
	if err != nil {
		return err
	}
	if !supported {
		return errCommentsUnavailable
	}
	if !a.accountSnapshotCurrent(account) {
		return ErrStaleGeneration
	}
	if err := send(client, kind, object); err != nil {
		return err
	}
	if !a.accountSnapshotCurrent(account) {
		return ErrStaleGeneration
	}
	return nil
}
