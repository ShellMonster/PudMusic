// 把下载挂到 App 上。
//
// 跟 `MountStreaming` 是一件事的两半：那一件决定「B 站的 CDN 怎么调」，
// 这一件决定「下载的队列存在哪儿」。**同一个**凭据边界、同一个 http.Client——
// 换一套的话，「凭据注入有明确边界」这条就漏了，而漏在哪一天没人知道。
package ui

import (
	"log"
	"path/filepath"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/download"
	"github.com/egoist/mygo"
)

// MountDownloads 装好下载。**必须在开窗口之前调**（`download.New` 要读盘）。
//
// 读盘放这儿而不是放进 `Run` 里：它是几百 KB 的 JSON 解析 + 一次
// `stat`，放开窗路径上的表现是窗口先出来、队列半秒后才出现在界面上。
func MountDownloads(a *app.App) error {
	// ⚠️ 和 `MountStreaming` 里那个 media 是**同一个**：带凭据边界、不挂
	// jar、Referer 照给。这里重造一个的话，两条路的边界就可能不一样，
	// 而「B 站的 CDN 拿到的凭据域」是同一条规则。
	media := newCredentialTransport().Client(nil)

	// 退出前把队列落盘。挂 `OnWillQuit` 而不是等 `App.Close`：窗口全关
	// 但进程还在（收起到托盘）的那条路上 `App.Close` 不跑，而那正好是
	// 「用户下了两首、收起来、第二天开机」的场景。
	//
	// 同步写：队列不大（几 KB），而它是「下到哪儿了」的唯一一份，
	// 这一次写失败就是用户重下一次。`App.Close` 也会写一次，是第二道。
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		if err := a.SaveDownloads(); err != nil {
			log.Printf("biu: 退出时保存下载队列失败：%v", err)
		}
	})

	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		// 不返回错：拿不到目录只是**存不住队列**，下载本身还能用
		// （这一次的歌照样下得完，只是下次开应用接不上）。挡在这里反而
		// 让整个下载功能用不了。
		log.Printf("biu: 拿不到应用数据目录，下载队列这次存不住：%v", err)
		return a.MountDownloads(app.Downloads{Media: media, FallbackDir: downloadsDir()})
	}

	return a.MountDownloads(app.Downloads{
		Media:       media,
		QueueFile:   filepath.Join(dir, download.QueueFileName),
		FallbackDir: downloadsDir(),
	})
}

// downloadsDir 是「用户还没选过下载目录」时的去处。
//
// 设置里 `downloadPath` 的默认值是空串，而 `filepath.Join("", "歌.m4a")`
// 出来的是**相对路径**——歌会落到进程的当前工作目录里（双击图标启动时
// 那是 `/`），用户在 Finder 里根本看不见，也永远不会去那儿找。
//
// 拿不到就返回空串：`MountDownloads` 不因这个失败，而真的下下去时
// `os.Create` 会因为目录不存在而报错，用户在记录页上看到的是一句人话，
// 比悄悄落到一个诡异的目录里好。
func downloadsDir() string {
	dir, err := mygo.App.Path(mygo.PathDownloads)
	if err != nil {
		log.Printf("biu: 拿不到系统下载目录，下载没地方落：%v", err)
		return ""
	}
	return dir
}
