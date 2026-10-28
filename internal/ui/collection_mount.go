// 把歌单内容页（收藏夹 / 合集 / 系列）挂到 App 上。
//
// 跟 `MountStreaming` 是同一件事：`MountLogin` 建出来的是**那一个** B 站
// 客户端，这一票要用的就是它——再起第二个就等于第二份 cookie jar 和第二份
// 请求头，而那正是 `internal/service/bili` 的包注释里明说要避免的事。
//
// # 批量下载接的就是 App 自己
//
// `Downloader` 传的是 `a`——下载队列本来就挂在 App 上（`MountDownloads`），
// 所以这一层不需要中间人（见 `app.EnqueueDownload`）。真正在这一层发生的只有
// 一件事：**把两个 Mount 排在一起**，于是「开窗口之前该挂的都挂完了」看得见。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// MountCollections 装好歌单这一摊。**必须在开窗口之前调**。
//
// 它自己不读盘也不发请求，所以顺序上没那么严；放这儿是为了和另外两个
// Mount 排在一起——「开窗口之前该挂的都挂完了」这件事看得见。
//
// ⚠️ `Downloader` 传的是 `a` 自己，而它要用的下载队列是 `MountDownloads` 挂的
// 那一个。**两个 Mount 谁先谁后都行**：批量下载是用户点了按钮才发生的，那时
// 队列早就挂好了；而真没挂上时 `App.Download` 会如实说「下载还没挂上」。
func MountCollections(a *app.App, cli *bili.Client) {
	a.MountCollections(app.CollectionRefs{Client: cli, Downloader: a})
}
