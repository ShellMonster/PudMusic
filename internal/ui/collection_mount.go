// 把歌单内容页（收藏夹 / 合集 / 系列）挂到 App 上。
//
// 跟 `MountStreaming` 是同一件事：`MountLogin` 建出来的是**那一个** B 站
// 客户端，这一票要用的就是它——再起第二个就等于第二份 cookie jar 和第二份
// 请求头，而那正是 `internal/service/bili` 的包注释里明说要避免的事。
//
// ⚠️ **批量下载没有在这里传 `Downloader`**，而缺的不是那个服务——**服务早就在
// 那里了**：`internal/service/download`（音频下载与断点续传，issue #29 已 CLOSED，
// commit `54596d1`），`main.go` 里 `ui.MountDownloads(a)` 也早就在调。
//
// 缺的是**这一行接线**：`DownloaderMounted()` 读的是 `CollectionRefs.Downloader`
// （`internal/app/collection.go`），这个函数没传它，所以它恒为 false，收藏夹的
// 「批量下载」按钮于是永远是灰的、点下去只会得到一句「还没接上」。
//
// 写这一段是因为「下载服务还没做」这句话在这里站不住：#29 早就关了，服务写好了
// 一整套，而下一个读代码的人会以为下载这一摊还是一张白纸。
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
