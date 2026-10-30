// 把在线取流挂到 App 上。
//
// 跟 `MountLogin` 是一件事的两半：那一件决定「B 站 API 怎么调」，这一件
// 决定「B 站的 CDN 怎么调」。两者共用**同一个**凭据边界
// （`app.CredentialTransport`），只是客户端配置不同——差别很要紧，见下。
package ui

import (
	"path/filepath"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// MountStreaming 装好在线播放。**必须在开窗口之前调**。
func MountStreaming(a *app.App, cli *bili.Client) {
	// ⚠️ `Client(nil)` 里的 nil 是 **cookie jar**，不是「用默认的」。
	// B 站 CDN 跟 B 站 API 不是同一个凭据域：SESSDATA 发给一台存音频文件的
	// CDN 没有任何理由（`spec.md`「网络」一节）。
	//
	// 但 Referer 要给——B 站的 CDN 不给就 403，而 Referer 不是凭据。
	// 这两条边界都由同一个 `CredentialTransport` 兜着，所以 CDN 就算
	// 302 跳到别的主机上，那一跳也会自己重新判一遍（`app.RedirectClass`）。
	media := newCredentialTransport().Client(nil)

	a.MountStreaming(app.Streaming{
		Client: cli,
		Media:  media,
		// 🔴 **字节缓存落在用户数据目录底下**（2026-10-30）：下过的音频字节
		// 留在盘上，切回同一首时**不再走网络**。
		//
		// ⚠️ **不能是系统临时目录**：临时目录会被系统清掉，那缓存就白留了。
		// （跟评论 / 弹幕 / 歌词缓存是同一类东西，见 `lyrics_mount.go`。）
		//
		// ⚠️ **空目录进、空目录出**：拿不到用户数据目录时，缓存整个关掉——
		// 那只是「每次都重新下」，不该挡着应用开起来（理由同
		// `lyrics_mount.go` 的 `lyricsCacheDir`）。
		CacheDir: streamCacheDir(appDataDir()),
		// 偏好从设置里读，而不是留个默认值。用户上次选的是无损，
		// 这次启动就该按无损取流——「重启之后又变回默认档」是那种没人会说的 bug。
		// （设置文件里存的是老版本的 `"auto"` 也没关系：`PrefToBili` 对认不出的
		// 值一律给最高有损，见那张函数的注释。）
		//
		// ⚠️ 必须在 MountSettings 之后调（main.go 里的顺序就是）。
		Quality: app.PrefToBili(a.SettingsSnapshot().AudioQuality),
	})
}

// streamCacheDirName 是音频字节缓存放在应用数据目录底下的那一个文件夹。
const streamCacheDirName = "streamaudio"

// streamCacheDir 是音频字节缓存落在哪儿。**空目录进、空目录出**。
//
// 🔴 **不能写成 `filepath.Join(dir, streamCacheDirName)` 了事**：空目录时
// `Join` 返回的是**相对路径** `"streamaudio"`（不是空串！），而缓存只把
// **空串**当成「不落盘」。于是「拿不到数据目录」这一种情况下，缓存会在
// **进程的当前工作目录**里建出 `streamaudio/` 并真的写盘——应用数据没拿到，
// 却往用户碰巧启动它的那个目录里写了几百 MB 的音频。
//
// （这一条照抄 `lyrics_mount.go` 的 `lyricsCacheDir`。）
func streamCacheDir(userDataDir string) string {
	if userDataDir == "" {
		return ""
	}
	return filepath.Join(userDataDir, streamCacheDirName)
}
