// 把歌单内容页（收藏夹 / 合集 / 系列）挂到 App 上。
//
// 跟 `MountStreaming` 是同一件事：`MountLogin` 建出来的是**那一个** B 站
// 客户端，这一票要用的就是它——再起第二个就等于第二份 cookie jar 和第二份
// 请求头，而那正是 `internal/service/bili` 的包注释里明说要避免的事。
//
// ⚠️ **批量下载没有在这里传 `Downloader`**：issue #29 那一票还没开工。接口在
// `app.Downloader`，接上的时候是这个函数多一行的事（见那里的注释）。
package ui

import (
	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// MountCollections 装好歌单这一摊。**必须在开窗口之前调**。
//
// 它自己不读盘也不发请求，所以顺序上没那么严；放这儿是为了和另外两个
// Mount 排在一起——「开窗口之前该挂的都挂完了」这件事看得见。
func MountCollections(a *app.App, cli *bili.Client) {
	a.MountCollections(app.CollectionRefs{Client: cli})
}
