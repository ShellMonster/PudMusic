package app

import (
	"context"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// ExploreMounted reports whether the Explore client is mounted.
func (a *App) ExploreMounted() bool { return a.explore() != nil }

func (a *App) FollowTagsForAccount(ctx context.Context, snapshot AccountSnapshot) ([]FollowTag, error) {
	cli, err := a.exploreAt(snapshot)
	if err != nil {
		return nil, err
	}
	values, err := cli.FollowTags(ctx)
	if err != nil {
		return nil, err
	}
	if !a.accountSnapshotCurrent(snapshot) {
		return nil, ErrStaleGeneration
	}
	out := make([]FollowTag, 0, len(values))
	for _, v := range values {
		out = append(out, mapFollowTag(v))
	}
	return out, nil
}

func (a *App) FollowTags(ctx context.Context) ([]FollowTag, error) {
	return a.FollowTagsForAccount(ctx, a.AccountSnapshot())
}

func (a *App) FollowListForAccount(ctx context.Context, snapshot AccountSnapshot, page int) (FollowUsersPage, error) {
	return a.followUsersForAccount(ctx, snapshot, func(cli *bili.Client) (bili.FollowUsersPage, error) {
		return cli.FollowList(ctx, snapshot.SelfMid, page)
	})
}

func (a *App) FollowList(ctx context.Context, vmid int64, page int) (FollowUsersPage, error) {
	snapshot := a.AccountSnapshot()
	snapshot.SelfMid = vmid
	return a.followUsersForAccount(ctx, snapshot, func(cli *bili.Client) (bili.FollowUsersPage, error) {
		return cli.FollowList(ctx, vmid, page)
	})
}

func (a *App) SearchFollowingsForAccount(ctx context.Context, snapshot AccountSnapshot, name string, page int) (FollowUsersPage, error) {
	return a.followUsersForAccount(ctx, snapshot, func(cli *bili.Client) (bili.FollowUsersPage, error) {
		return cli.SearchFollowings(ctx, snapshot.SelfMid, name, page)
	})
}

func (a *App) SearchFollowings(ctx context.Context, vmid int64, name string, page int) (FollowUsersPage, error) {
	snapshot := a.AccountSnapshot()
	snapshot.SelfMid = vmid
	return a.followUsersForAccount(ctx, snapshot, func(cli *bili.Client) (bili.FollowUsersPage, error) {
		return cli.SearchFollowings(ctx, vmid, name, page)
	})
}

func (a *App) FollowTagUsersForAccount(ctx context.Context, snapshot AccountSnapshot, tagID, page int) ([]FollowUser, error) {
	cli, err := a.exploreAt(snapshot)
	if err != nil {
		return nil, err
	}
	values, err := cli.FollowTagUsers(ctx, tagID, page)
	if err != nil {
		return nil, err
	}
	if !a.accountSnapshotCurrent(snapshot) {
		return nil, ErrStaleGeneration
	}
	out := make([]FollowUser, 0, len(values))
	for _, v := range values {
		out = append(out, mapFollowUser(v))
	}
	return out, nil
}

func (a *App) FollowTagUsers(ctx context.Context, tagID, page int) ([]FollowUser, error) {
	return a.FollowTagUsersForAccount(ctx, a.AccountSnapshot(), tagID, page)
}

func (a *App) followUsers(ctx context.Context, fetch func(*bili.Client) (bili.FollowUsersPage, error)) (FollowUsersPage, error) {
	return a.followUsersForAccount(ctx, a.AccountSnapshot(), fetch)
}

func (a *App) followUsersForAccount(ctx context.Context, snapshot AccountSnapshot, fetch func(*bili.Client) (bili.FollowUsersPage, error)) (FollowUsersPage, error) {
	cli, err := a.exploreAt(snapshot)
	if err != nil {
		return FollowUsersPage{}, err
	}
	pg, err := fetch(cli)
	if err != nil {
		return FollowUsersPage{}, err
	}
	if !a.accountSnapshotCurrent(snapshot) {
		return FollowUsersPage{}, ErrStaleGeneration
	}
	out := FollowUsersPage{Total: pg.Total, Users: make([]FollowUser, 0, len(pg.Users))}
	for _, v := range pg.Users {
		out.Users = append(out.Users, mapFollowUser(v))
	}
	return out, nil
}
