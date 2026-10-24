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
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/platform"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
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
// # 落在「音乐」目录下的应用子目录
//
// 原来是系统下载目录（macOS 上是 `~/Downloads`）——那儿是浏览器和安装包的地方，
// 歌和一堆 `.dmg` 混在一起，用户在自己的下载目录里翻不到刚下的那几首。
// 同类音乐软件都不往那儿扔（网易云、QQ 音乐、Spotify 都是「音乐目录 +
// 以应用命名的子目录」），所以这里也那么放：macOS 是 `~/Music/BiuGo`，
// Windows 是 `音乐\BiuGo`（理由见 `platform/downloadsdir.go` 顶上那一节）。
//
// ⚠️ **目录不存在要建**（`platform.EnsureDefaultDownloadsDir`）：不建的话第一次
// 下载会失败，而失败是 `os.Create` 报的「no such file or directory」——用户看到
// 的是一句他看不懂的话，实际原因只是「那个文件夹还没建」。
//
// ⚠️ **设置里手动选过的目录照旧**：这一条只是「没选过」时的兜底，选过的走
// `Settings.DownloadPath`（见 `app.downloadDir`）。
//
// 拿不到就返回空串：`MountDownloads` 不因这个失败。而空串**不会再让歌落到一个
// 诡异的目录里**——应用层会**拒绝下载**并说一句人话（见 `app.errNoDownloadDir`），
// 界面上说「还没有下载目录」，用户照着做就是去设置里选一个。
func downloadsDir() string {
	dir, err := platform.EnsureDefaultDownloadsDir()
	if err != nil {
		log.Printf("biu: 拿不到默认下载目录，下载没地方落：%v", err)
		return ""
	}
	return dir
}

// ── 界面上那几个下载按钮 ─────────────────────────────────────────────────────

// downloadLater 是「下载音频 / 下载视频」按钮的**异步**那一条。**立刻返回**。
//
// # 和 `queue_drawer.go` 的 startDownload 有什么不一样
//
// 那一条是同步的（`App.Download`），界面线程上一次网络往返都不许有，所以它
// 只对**手里已经有 cid 的条目**有用（队列里的歌都是从播放过来的，cid 一定在）。
//
// 这一条是给**手里没有 cid 的条目**用的：收藏夹的行（`bili.Media` 不带 cid）
// 和动态卡片（`major.archive` 不带 cid）。补 cid 要问 B 站一次分 P，所以走
// `App.DownloadItem`——它在后台补，结果回来才说话。
//
// ⚠️ **别把这一条改成同步的**：那会在用户按下按钮的那一帧冻住窗口几百毫秒，
// 而症状是「点一下下载整个应用卡一下」，很难往回找。
//
// label 是菜单 / 按钮上写的那几个字，进失败的话里——菜单里有五项，只说
// 「下载不了」的话用户不知道说的是哪一项。
func (s *Shell) downloadLater(it app.Item, video bool, label string) {
	done := func(err error) {
		s.sayDownloadDone(label, err)
		s.wake.invalidate()
	}
	if video {
		s.app.DownloadItemVideo(it, done)
		return
	}
	s.app.DownloadItem(it, done)
}

// sayDownloadDone 是异步下载跑完之后说的那句话。
//
// ⚠️ **它在后台 goroutine 上被调**：Toast 自己带锁（`Shell.toastMu`），
// 而 `wake.invalidate` 只是叫醒界面，两者都不碰界面状态（铁律一）。
//
// 🔴 **走 `bili.Friendly` 而不是 `err.Error()`**。两件事：
//
//   - `err.Error()` 是 Go 的原话。取流那一路的错会带着「bili: 问不出这一集是哪一集
//     （BV1xx）」这种内部说法，而 `Friendly` 给的是「等网络好一些」——
//     **两句对同一件事的说法不一样时，用户只能照着错的那句去做**。
//   - 更要紧的是**地址**：取流的错里可能带一条带 `upsig` 签名的 CDN 地址，
//     而 `Friendly` 末尾那道「不许把地址原样打出去」就是为它准备的
//     （见 `service/bili/friendly.go` 的注释）。
func (s *Shell) sayDownloadDone(label string, err error) {
	switch {
	case err == nil:
		s.addToast("已加进下载队列", notice.Success)
	default:
		s.addToast(label+"没成："+bili.Friendly(err), notice.Danger)
	}
}
