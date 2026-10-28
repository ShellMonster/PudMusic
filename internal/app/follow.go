// 「我的关注」那一页要的那几个接口的中间层。
//
// # 这一层几乎不做事，是故意的
//
// 界面层不发网络请求（SPEC 的四层依赖单向），所以这一页不能把 `bili.Client`
// 拿在手里。但这一页的四个接口**没有一件需要加工**：返回的东西原样画得出来，
// 头像地址和总数在服务层已经算好了（见 `bili/follow.go` 的文件头）。
//
// 所以这里只有四个薄壳 + 一个「挂上没有」。真要往这一层加加工之前先想一遍：
// 加工要的是**界面**的知识（怎么画、画多大），而那一层的知识放在
// `internal/ui/follow.go` 更近。
//
// # 挂的是哪个客户端
//
// 和 `explore.go` 的作者栏**同一个**（`MountExplore` 装的那一个，也就是
// `MountLogin` 建的那唯一一个）。另起一个挂载点的话，「登录成功了，一打开关注
// 页就 -101」——因为两个 jar 两份请求头，而且两个里只有一个装了 cookie。
package app

import (
	"context"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// ExploreMounted 报探索那一摊（UP 空间 / 动态流 / **我的关注**）挂上没有。
//
// 界面靠它说清「还没接上 B 站接口」，而不是画一个永远空的页面（理由见
// `viewLibraryMissing`）。
func (a *App) ExploreMounted() bool { return a.explore() != nil }

// FollowTags 列「我关注的人」分了哪些组（左边那一列 Tab）。
func (a *App) FollowTags(ctx context.Context) ([]bili.FollowTag, error) {
	cli := a.explore()
	if cli == nil {
		return nil, errExploreMissing
	}
	return cli.FollowTags(ctx)
}

// FollowList 拉「全部关注」那一格。vmid 是自己的 mid，page 从 1 起。
func (a *App) FollowList(ctx context.Context, vmid int64, page int) (bili.FollowUsersPage, error) {
	cli := a.explore()
	if cli == nil {
		return bili.FollowUsersPage{}, errExploreMissing
	}
	return cli.FollowList(ctx, vmid, page)
}

// SearchFollowings 在我关注的人里按名字搜。page 从 1 起。
func (a *App) SearchFollowings(ctx context.Context, vmid int64, name string, page int) (bili.FollowUsersPage, error) {
	cli := a.explore()
	if cli == nil {
		return bili.FollowUsersPage{}, errExploreMissing
	}
	return cli.SearchFollowings(ctx, vmid, name, page)
}

// FollowTagUsers 拉某一个分组里的人。page 从 1 起。
//
// ⚠️ **这一条没有 hasMore**：B 站那个接口不报总数（`bili/follow.go` 文件头），
// 「还有没有」要界面拿分组自己的 `Count` 去比已加载的人数。
func (a *App) FollowTagUsers(ctx context.Context, tagID, page int) ([]bili.FollowUser, error) {
	cli := a.explore()
	if cli == nil {
		return nil, errExploreMissing
	}
	return cli.FollowTagUsers(ctx, tagID, page)
}
