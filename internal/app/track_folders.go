package app

import (
	"context"
	"errors"
	"strconv"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

type TrackFolders struct {
	RID         int64
	ContentType int
	Choices     []bili.CreatedFolder
}

func (a *App) LoadTrackFolders(ctx context.Context, account AccountSnapshot, item Item) (TrackFolders, error) {
	c := a.collectionsSnapshot()
	if c == nil || account.SelfMid <= 0 || !a.LoggedIn() {
		return TrackFolders{}, errNotLoggedInForFolders
	}
	if !item.isOnline() {
		return TrackFolders{}, errors.New("这首歌没有可收藏的来源")
	}
	client := c.refs.Client
	contentType := bili.MediaVideo
	rid, _ := strconv.ParseInt(item.Sid, 10, 64)
	if item.Type == "audio" {
		contentType = bili.MediaAudio
	} else if rid <= 0 && item.Bvid != "" {
		var err error
		rid, err = client.ArchiveID(ctx, item.Bvid)
		if err != nil {
			return TrackFolders{}, err
		}
	}
	if rid <= 0 {
		return TrackFolders{}, errors.New("这首歌没有可收藏的来源")
	}
	if !a.accountSnapshotCurrent(account) {
		return TrackFolders{}, ErrStaleGeneration
	}
	choices, err := fetchCreatedFolders(ctx, client, account.SelfMid, rid)
	if !a.accountSnapshotCurrent(account) {
		return TrackFolders{}, ErrStaleGeneration
	}
	return TrackFolders{RID: rid, ContentType: contentType, Choices: choices}, err
}

func (a *App) SaveTrackFolders(ctx context.Context, account AccountSnapshot, target TrackFolders, add, remove []int64) error {
	c := a.collectionsSnapshot()
	if c == nil || account.SelfMid <= 0 || !a.LoggedIn() {
		return errNotLoggedInForFolders
	}
	if !a.accountSnapshotCurrent(account) {
		return ErrStaleGeneration
	}
	if len(add) == 0 && len(remove) == 0 {
		return nil
	}
	if err := c.refs.Client.DealFolderMedias(ctx, target.RID, target.ContentType, add, remove); err != nil {
		return err
	}
	if !a.accountSnapshotCurrent(account) {
		return ErrStaleGeneration
	}
	a.forgetFavoritesIfBound(add, remove)
	a.AutoRefreshFavorites()
	return nil
}
