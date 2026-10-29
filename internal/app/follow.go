package app

import (
	"context"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// ExploreMounted reports whether the Explore client is mounted.
func (a *App) ExploreMounted() bool { return a.explore() != nil }

func (a *App) FollowTags(ctx context.Context) ([]FollowTag, error) {
	cli := a.explore()
	if cli == nil {
		return nil, errExploreMissing
	}
	values, err := cli.FollowTags(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]FollowTag, 0, len(values))
	for _, v := range values {
		out = append(out, mapFollowTag(v))
	}
	return out, nil
}

func (a *App) FollowList(ctx context.Context, vmid int64, page int) (FollowUsersPage, error) {
	return a.followUsers(ctx, func(cli *bili.Client) (bili.FollowUsersPage, error) {
		return cli.FollowList(ctx, vmid, page)
	})
}

func (a *App) SearchFollowings(ctx context.Context, vmid int64, name string, page int) (FollowUsersPage, error) {
	return a.followUsers(ctx, func(cli *bili.Client) (bili.FollowUsersPage, error) {
		return cli.SearchFollowings(ctx, vmid, name, page)
	})
}

func (a *App) FollowTagUsers(ctx context.Context, tagID, page int) ([]FollowUser, error) {
	cli := a.explore()
	if cli == nil {
		return nil, errExploreMissing
	}
	values, err := cli.FollowTagUsers(ctx, tagID, page)
	if err != nil {
		return nil, err
	}
	out := make([]FollowUser, 0, len(values))
	for _, v := range values {
		out = append(out, mapFollowUser(v))
	}
	return out, nil
}

func (a *App) followUsers(ctx context.Context, fetch func(*bili.Client) (bili.FollowUsersPage, error)) (FollowUsersPage, error) {
	cli := a.explore()
	if cli == nil {
		return FollowUsersPage{}, errExploreMissing
	}
	pg, err := fetch(cli)
	if err != nil {
		return FollowUsersPage{}, err
	}
	out := FollowUsersPage{Total: pg.Total, Users: make([]FollowUser, 0, len(pg.Users))}
	for _, v := range pg.Users {
		out.Users = append(out.Users, mapFollowUser(v))
	}
	return out, nil
}
