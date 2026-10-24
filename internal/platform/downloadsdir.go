// 默认下载目录：**音乐**目录下的应用子目录。
//
// # 为什么不是系统「下载」目录
//
// 原来落在系统下载目录（macOS 上是 `~/Downloads`）。那儿是**浏览器和安装包**
// 的地方——歌和一堆 `.dmg`、`.zip` 混在一起，用户在自己的下载目录里翻不到
// 刚下的那几首，而「下载」这个词在 Finder 里早就被别的东西占了。
//
// 同类音乐软件都不往那儿扔：
//
//	网易云音乐（桌面）  音乐/网易云音乐/CloudMusic/Music
//	QQ 音乐            音乐/QQMusic
//	Spotify            ~/Music/Spotify/Downloads
//
// 规律是同一条：**「音乐」目录 + 以应用命名的子目录**。所以这里也那么放：
// macOS 是 `~/Music/BiuGo`，Windows 是 `音乐\BiuGo`。
//
// # 老歌不搬家
//
// 已经在 `~/Downloads` 下的歌**留在原地**：这一层只决定**新**下载落到哪儿，
// 不搬、不删、也不提示。搬目录是另一件事（搬不搬、搬了旧路径留不留提示，
// 要人拍板），而搬错了的代价是「数据分成两半」——那种错法是静默的。
package platform

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
)

// DownloadsDirName 是下载目录在「音乐」目录下的子目录名。
//
// ⚠️ **用显示名 `BiuGo`，不要用 `LegacyUserDataDirName`（`Biu MyGo`）**：那是
// **用户数据目录**的名字，它钉死在老名字上是为了老用户升级不丢数据（理由见
// `datadir.go` 顶上那一节）。那是**另一件事**：下载目录是新起的，没有「老用户
// 升级要保住什么」这个问题，跟着显示名走正好。
//
// ⚠️ 也别把它跟 bundle id、钥匙串服务名混起来——那三件各不相干
// （AGENTS.md §5）。这一件是第四件：**新下载落哪儿**。
const DownloadsDirName = "BiuGo"

// DefaultDownloadsDir 返回默认下载目录的路径：**音乐**目录下的应用子目录。
//
// macOS 是 `~/Music/BiuGo`，Windows 是 `音乐\BiuGo`（`mygo` 的 `PathMusic`
// 在两个平台上就是「音乐」目录；Linux 上按 XDG 的 `XDG_MUSIC_DIR`）。
//
// ⚠️ **只算路径，不碰文件系统**：建目录是碰磁盘的事，只在**开应用时**做一次
// （`ui.downloadsDir` 调的是 `EnsureDefaultDownloadsDir`）。落点算完就交出去了
// （`app.Downloads.FallbackDir`），设置页上那一格读的是**交出去的那一份**。
func DefaultDownloadsDir() (string, error) {
	music, err := musicRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(music, DownloadsDirName), nil
}

// musicRoot 是「音乐」目录本身。
//
// ⚠️ **相对路径要挡住**：框架拿不到家目录时给的是 `"Music"` 这样的相对路径，
// 而歌落下去的地方就是**进程的当前工作目录**——双击图标启动的 macOS 应用
// 那是 `/`，用户在 Finder 里看不见，也永远不会去那儿找（同一个坑见
// `app.Downloads.FallbackDir` 的注释）。
func musicRoot() (string, error) {
	music, err := mygo.App.Path(mygo.PathMusic)
	if err != nil {
		return "", fmt.Errorf("拿不到音乐目录：%w", err)
	}
	if !filepath.IsAbs(music) {
		return "", fmt.Errorf("音乐目录不是绝对路径（%q），不知道该往哪儿放", music)
	}
	return music, nil
}

// EnsureDefaultDownloadsDir 是「默认下载目录」这条路的最终答案：算出来，
// 并且**建出来**。
//
// ⚠️ **目录不存在要建**：不建的话第一次下载会失败，而失败是 `os.Create` 报的
// 「no such file or directory」——用户看到的是一句他看不懂的话，实际原因只是
// 「那个文件夹还没建」。
//
// ⚠️ **音乐目录用不了时退回系统下载目录**：家目录拿不到时框架给的是相对
// 路径，那种目录建出来只会让歌落进进程的当前工作目录。系统下载目录是
// **绝对**的（家目录正常时它就是 `~/Downloads`），也是歌以前落的地方——
// 退回它比让歌落进一个诡异的目录强。
//
// ⚠️ **退回而不是「返回空串」**：空串会让 `filepath.Join("", "歌.m4a")` 出来
// 一个**相对路径**（见 `app.Downloads.FallbackDir` 的注释）。真的两个都拿不到
// 时才返回错误，由调用方记一条日志——那时候应用层会**拒绝下载**并说一句人话
// （见 `app.errNoDownloadDir`），而不是把歌写到别处去。
func EnsureDefaultDownloadsDir() (string, error) {
	dir, musicErr := DefaultDownloadsDir()
	if musicErr == nil {
		err := os.MkdirAll(dir, 0o755)
		if err == nil {
			return dir, nil
		}
		musicErr = fmt.Errorf("建下载目录 %s：%w", dir, err)
	}

	dl, err := mygo.App.Path(mygo.PathDownloads)
	if err != nil {
		return "", fmt.Errorf("%w；系统下载目录也拿不到：%v", musicErr, err)
	}
	if !filepath.IsAbs(dl) {
		return "", fmt.Errorf("%w；系统下载目录也不是绝对路径（%q）", musicErr, dl)
	}
	if err := os.MkdirAll(dl, 0o755); err != nil {
		return "", fmt.Errorf("%w；系统下载目录也建不出来：%v", musicErr, err)
	}
	return dl, nil
}
