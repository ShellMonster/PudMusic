package app

import "github.com/ShellMonster/PudMusic/internal/service/bili"

// BiliClient 是这个 App 用的**那一个** B 站客户端，没挂登录时是 nil。
//
// 为什么放在 App 上而不是让界面层自己存一份：全项目只能有一个 B 站客户端
// （`service/bili` 包头那句话：WBI 签名只有一份、cookie jar 只有一份、请求头
// 只有一份）。登录那一摊建它（`ui.MountLogin`），在线取流用**它**
// （`ui.MountStreaming(a, cli)`）。搜索页要发 B 站的请求，也得用**它**——
// 界面层另存一份等于同一个 jar 有两个主人，退登之后搜索那一路还留着旧 cookie。
//
// 存在的理由和 `FavoritesOf` 那份一样：字段写在 `app.go` 里，而这一层的新
// 方法不必等 `app.go` 那一格。方法写在哪个文件里都行，字段不行。
//
// ⚠️ **只能在 MountLogin 之后读**。它没挂锁，因为 MountLogin 一定在开窗口
// **之前**调（读钥匙串是阻塞 IO，见 `MountLogin` 的注释），那时还没有界面线程
// 去和它抢。没挂过登录时它就是 nil，调用方要判空。
func (a *App) BiliClient() *bili.Client {
	if a.account == nil {
		return nil
	}
	return a.account.cli
}
