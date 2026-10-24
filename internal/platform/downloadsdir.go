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
//	QQ音乐            音乐/QQMusic
//	Spotify            ~/Music/Spotify/Downloads
//
// 规律是同一条：**「音乐」目录 + 以应用命名的子目录**。所以这里也那么放。
//
// # 老歌不搬家
//
// 已经在 `~/Downloads` 下的歌**留在原地**：这一层只决定**新**下载落到哪儿，
// 不搬、不删、也不提示。搬目录是另一件事（搬不搬、搬了旧路径留不留提示，
// 要人拍板），而搬错了的代价是「数据分成两半」——那种错法是静默的。
package platform

import (
	"fmt"
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
// # 各平台上它是哪一条路径
//
//	macOS    ~/Music/BiuGo
//	Windows  C:\Users\<用户名>\Music\BiuGo
//	Linux    $XDG_MUSIC_DIR/BiuGo（退回 ~/Music/BiuGo）
//
// ⚠️ **Windows 上那条路径是「英文名的 Music」，不是「音乐」**：框架的
// `PathMusic` 只把一张写死的英文名表拼在家目录下面（`mygo@v0.6.3/paths.go`
// 的 `userDir()`），**不走系统的已知文件夹 API**——而中文 Windows 上真正的
// 那个文件夹叫 `C:\Users\<用户名>\音乐`。所以在中文 Windows 上，应用会**另造**
// 一个 `Music` 文件夹，资源管理器里它**不是**「音乐」那一栏（见下面那条）。
//
// ⚠️ **macOS 上也是拼出来的 `~/Music`，不查用户改过位置的「音乐」文件夹**：
// macOS 允许在「系统设置 → 通用 → 储存空间 → 位置」里把「音乐」挪走，而框架
// 不走 `NSFileManager` 的 `URLForDirectory:`——那种用户的歌会落在一个新造的
// `~/Music/BiuGo` 里，访达里它不是「音乐」那一栏。票面点名的是
// `mygo.App.Path(mygo.PathMusic)`，所以这就是这一条路的窄口子：**要在这两个
// 平台上做对，得改走系统的已知文件夹 API**（那是另一票的事）。
//
// # 这一层**只算路径，不碰文件系统**
//
// 建目录是碰磁盘的事，而它**建在真的要落盘的那一刻**（`service/download` 的
// `ensureDir`）——不是开应用的时候。开应用就建的话，**光是打开应用就会在用户
// 的磁盘上留下一个文件夹**，测试里也一样（那不是临时的目录，是用户自己的）。
func DefaultDownloadsDir() (string, error) {
	music, musicErr := musicRoot()
	if musicErr == nil {
		return filepath.Join(music, DownloadsDirName), nil
	}

	// ⚠️ **音乐目录用不了时退回系统下载目录**：家目录拿不到时框架给的是
	// `"Music"` 这样的**相对**路径，而那种落点会让歌落进进程的当前工作目录
	// ——双击图标启动的 macOS 应用那是 `/`，用户在 Finder 里看不见，也永远
	// 不会去那儿找。系统下载目录是**绝对**的（家目录正常时它就是
	// `~/Downloads`），也是歌以前落的地方——退回它比让歌落进一个诡异的目录强。
	//
	// ⚠️ **退回而不是「返回空串」**：空串会让 `filepath.Join("", "歌.m4a")` 出来
	// 一个**相对路径**（见 `app.Downloads.FallbackDir` 的注释）。真的两个都拿不到
	// 时才返回错误，由调用方记一条日志——那时候应用层会**拒绝下载**并说一句
	// 人话（见 `app.errNoDownloadDir`），而不是把歌写到别处去。
	dl, err := mygo.App.Path(mygo.PathDownloads)
	if err != nil {
		return "", fmt.Errorf("%w；系统下载目录也拿不到：%v", musicErr, err)
	}
	if !filepath.IsAbs(dl) {
		return "", fmt.Errorf("%w；系统下载目录也不是绝对路径（%q）", musicErr, dl)
	}
	return dl, nil
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
