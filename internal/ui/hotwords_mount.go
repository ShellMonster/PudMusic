// 搜索框热词这一摊的挂载。
//
// # 和歌词那一摊是同一件事的另一半
//
// 网易云热搜词跟歌词的两个源一样，都在凭据白名单的 **referer 档**里
// （`app.DefaultInjectPolicy` 把 `music.163.com` / `.163.com` 明确列着）——
// B 站的 Cookie 一个都不给它，规则写在代码里而不是只在心里。所以这里也用
// **同一个**凭据边界建出来的网络层（`newCredentialTransport`），理由见
// `lyrics_mount.go` 文件头：凭据这件事不许有第二个可能不一样的来源。
//
// ⚠️ **它不发任何请求**（拉取是界面第一帧问 `Refresh` 的时候才开始的），排在
// 开窗口之前只是「零件都齐了」这件事看得见一点（`main.go`，和 MountLyrics 一批）。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/hotwords"
)

// MountHotwords 把热词这一摊挂上：网易云那个源，走共享的凭据边界。
func MountHotwords(a *app.App) {
	a.MountHotwords(hotwords.New(newCredentialTransport().Client(nil)))
}
