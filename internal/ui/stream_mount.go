// 把在线取流挂到 App 上。
//
// 跟 `MountLogin` 是一件事的两半：那一件决定「B 站 API 怎么调」，这一件
// 决定「B 站的 CDN 怎么调」。两者共用**同一个**凭据边界
// （`app.CredentialTransport`），只是客户端配置不同——差别很要紧，见下。
package ui

import (
	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
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
		// 偏好从设置里读，而不是留个默认值。用户上次选的是无损，
		// 这次启动就该按无损取流——「重启之后又变回自动」是那种没人会说的 bug。
		//
		// ⚠️ 必须在 MountSettings 之后调（main.go 里的顺序就是）。
		Quality: app.PrefToBili(a.SettingsSnapshot().AudioQuality),
	})
}
