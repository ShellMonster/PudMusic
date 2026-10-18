// 把歌词这一摊挂到 App 上：缓存 + 两个搜索源。
//
// 跟 `MountLogin` 是同一件事的两半：那一件决定「B 站 API 怎么调」，这一件决定
// 「歌词源怎么调」。两者共用**同一个**凭据边界（`app.CredentialTransport`），
// 因为两个歌词源本来就在那张白名单的 referer 档里（`app.DefaultInjectPolicy`）——
// **B 站的 Cookie 一个都不给它们**，规则写在代码里而不是只在心里。
package ui

import (
	"log"
	"path/filepath"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/lyrics"
	"github.com/egoist/mygo"
)

// MountLyrics 挂上歌词这一摊。**必须在开窗口之前调**。
//
// 缓存那半边（`a.MountLyrics`）建目录，是阻塞 IO；搜索源那半边不发请求，挂上
// 只是「零件都齐了」这件事看得见一点。
//
// ⚠️ **这两个源共用 `newCredentialTransport()` 那一个网络层**，和 B 站接口用的是
// 同一个（理由和 `MountStreaming` 一样：同一个 jar、同一份请求头、同一张白名单）。
// 另建一个 `http.Client` 意味着凭据这件事有了第二个可能不一样的来源——凭据上
// 「有两个来源」等于「有一个在偷偷生效」。
func MountLyrics(a *app.App) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Printf("biu: 拿不到应用数据目录，这次的歌词存不住：%v", err)
	}
	// 拿不到目录时传空串：缓存那一层对空目录是**只读不写**，歌词照样看得见
	// （只是关掉就没了）。挡在开窗口之外不值当——为一件可有可无的东西让应用起不来，
	// 比它更糟（和 `MountSettings` 同一个判断）。
	a.MountLyrics(lyrics.NewCache(filepath.Join(dir, "lyrics")))

	// ⚠️ **不挂 jar**：`Client(nil)` 里的 nil 是 **cookie jar**，不是「用默认的」。
	// 歌词源跟 B 站完全无关，拿到 SESSDATA 没有任何理由
	//（`app.DefaultInjectPolicy` 把 `music.163.com` / `lrclib.net` 明确列在
	// referer 档，`RedirectClass` 也会把 Cookie / Authorization 全删掉——但那
	// 已经是「发了出去再删」，不如一开始就一个都不给）。
	a.MountLyricSources(
		lyrics.NewNetease(newCredentialTransport().Client(nil)),
		lyrics.NewLRCLib(newCredentialTransport().Client(nil)),
	)
}
