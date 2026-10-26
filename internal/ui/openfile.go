// 「系统把一个音频文件交给本应用」这一条路。
//
// # 三条入口，同一件事
//
// 用户从访达 / 资源管理器双击一个音频，或者把文件拖到 Dock 图标上，
// 系统会用三种方式把文件交给本应用：
//
//   - **macOS**：Apple Event（`application:openURLs:`），由框架转成
//     `OnOpenFile`；
//   - **Windows / Linux**：命令行参数，由框架按 `mygo.json` 的
//     `fileAssociations` 过滤之后转成 `OnOpenFile`；
//   - **第二个实例**：本应用已经在跑的时候，系统起的那个新进程把命令行
//     通过单实例锁转交给第一个实例（`internal/platform/single_instance.go`），
//     框架再把它转成 `OnOpenFile`。
//
// 三条路都汇到**同一个回调**（`mygo.App.OnOpenFile`），所以这一份只接一次。
//
// ⚠️ **回调必须在 `mygo.App.Run()` 之前注册**（框架的注释写得很明确，
// `mygo/app.go:470-477`）：启动命令行里的文件是在应用 ready 之后交付的，
// 而回调装晚了那一次就永远收不到。这一条由 `installLifecycle` 保证
// （它整个跑在 `Run` 之前，见 `shell_window.go` 的 `Run`）。
//
// # 不新建 app 状态
//
// ⚠️ **收到的文件不新建一份状态**：它排进**正在跑的那个**队列，然后放它。
// 新建一份状态的后果是「应用里有两个播放器、两首歌各放各的」，而用户要的
// 是「双击一首歌，应用放它」。
//
// # 队列语义：行级「立即播放」那一档
//
// 这一份走的是 `EnqueueAndPlayNow`（`DESIGN.md` §十第 4 条那一档）：
// **把这一首追加到队尾，并打断现在放的那首**。它**不清空**用户已经排好的
// 队列——清空队列只有用户明确点了「放这一批 / 这一段」才有（同一节那张表）。
//
// ⚠️ **和「拖进窗口」那一路不一样**：拖进窗口是把文件的**父目录**加进
// 本地音乐（`localmusic_drop.go`），而这里是**直接放这一首**。两件事的
// 用户意图不同：拖进窗口是「这批文件归我管」，双击是「放这一首」。
package ui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/platform"
	"github.com/egoist/mygo"
)

// openFileRejection 是「这个文件没被收下」的那几种情形。
type openFileRejection int

const (
	// openFileOK 是收下了。
	openFileOK openFileRejection = iota
	// openFileMissing 是文件不在了。
	openFileMissing
	// openFileUnreadable 是文件读不了（权限、路径坏了）。
	openFileUnreadable
	// openFileNotRegular 是它不是普通文件（目录、设备、套接字）。
	openFileNotRegular
	// openFileUnsupported 是这个格式我们放不了。
	openFileUnsupported
)

// checkOpenFile 判一个交给我们的路径能不能收下。**纯函数**：不碰状态、
// 不碰界面，于是能直接在测试里喂临时目录和假文件。
//
// ⚠️ **顺序是有讲究的**：先判「在不在」，再判「是不是普通文件」，最后才判
// 「格式认不认」。反过来的话，一个**已经删掉**的 `.mp3` 会被报成「格式不
// 支持」——而真相是「那个文件已经不在了」，两者要用户做的事完全不同。
func checkOpenFile(path string) openFileRejection {
	if strings.TrimSpace(path) == "" {
		return openFileUnreadable
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return openFileMissing
		}
		return openFileUnreadable
	}
	if !info.Mode().IsRegular() {
		return openFileNotRegular
	}
	if !platform.IsAssociatedAudioExt(platform.AudioExtOf(path)) {
		return openFileUnsupported
	}
	return openFileOK
}

// openFileRejectText 把「没收下」翻成一句给用户看的话。
//
// ⚠️ **每一句都要说「还能怎么办」**，不能只说「不行」：
//
//	不在了	→ 那首歌没了，或者被移到别处了
//	读不了	→ 权限 / 路径的问题
//	不是普通文件 → 你交给我的是个文件夹（或者别的东西）
//	格式不支持 → 我们放不了这个格式，而**哪些能放**要写出来
//
// 最后一档特别要写清楚**支持哪些**：用户手里可能是一首 `.wma`（我们放不了
// 那个格式），而他界面上还有别的歌可听——只说「格式不支持」的话，他以为
// 整个应用都不放歌了。
func openFileRejectText(rej openFileRejection, path string) string {
	name := filepath.Base(path)
	switch rej {
	case openFileMissing:
		return "打不开「" + name + "」：那个文件已经不在原来的位置上了"
	case openFileUnreadable:
		return "打不开「" + name + "」：读不了它（权限或者路径有问题）"
	case openFileNotRegular:
		return "「" + name + "」不是一个文件（是文件夹或者别的东西），放不了"
	case openFileUnsupported:
		return "「" + name + "」这个格式我们放不了（支持 " +
			supportedExtsText() + "）"
	}
	return "打不开「" + name + "」"
}

// supportedExtsText 把「支持哪些格式」写成一句人话：`.mp3 .flac …`。
//
// ⚠️ **带点**：用户手里拿着的文件名是 `老歌.wma`，说「支持 mp3 flac」的话他
// 对不上号——而这一句的全部用处就是让他知道**换成什么才行**。
func supportedExtsText() string {
	exts := make([]string, 0, len(platform.AssociatedAudioExts))
	for _, ext := range platform.AssociatedAudioExts {
		exts = append(exts, "."+ext)
	}
	return strings.Join(exts, " ")
}

// handleOpenFile 接住系统交给本应用的一个文件。
//
// **主线程调**（框架的 `OnOpenFile` 回调在主线程上发）。
//
// ⚠️ **先让开迷你播放器**：迷你开着的时候主窗口是收着的，而这一份要在
// **主窗口**上放歌（迷你那一扇只画播放器自己）。不让开的话，歌在后台放起来
// 了，而用户看见的还是迷你那一扇、界面上什么也没变——那和「点了没反应」
// 是同一种体验。理由和 `handleDeepLink` 一样（见 `lifecycle.go`）。
func (s *Shell) handleOpenFile(path string) {
	rej := checkOpenFile(path)
	if rej != openFileOK {
		s.addToast(openFileRejectText(rej, path), notice.Danger)
		return
	}
	s.dropMini()
	// ⚠️ **主窗要叫回来**：用户双击一个文件期待的是「窗口出现、歌放起来」。
	// 而迷你让开之后主窗口是收着的（两个窗口互斥，见 `miniplayer.go`）——
	// 不叫回来的话，歌在后台放起来了，用户眼前还是空的。
	//
	// ⚠️ **这一步在放歌之前**：先让用户看见窗口，再让声音出来。顺序反了的话，
	// 声音出来的时候窗口还没出现，那半秒里用户以为双击没反应。
	s.openWindow()
	// 行级「立即播放」那一档：追加到队尾并打断现在放的那首，
	// **不清空**用户已经排好的队列（DESIGN.md §十第 4 条）。
	if err := s.app.EnqueueAndPlayNow(app.LocalItem(path)); err != nil {
		// 放不开（文件坏了、格式这台机器解不了）：说清楚是**哪个文件**放不了。
		// ⚠️ 这一句是**追加**在队列里的（`EnqueueAndPlayNow` 契约：起播失败
		// 保留新加的那一首、把当前项退回原位），所以用户还能自己换一首。
		s.addToast("放不了「"+filepath.Base(path)+"」："+app.FriendlyErrText(err), notice.Danger)
		return
	}
	s.addToast("正在放《"+app.LocalItem(path).Title+"》", notice.Success)
}

// installOpenFile 把「系统交给我们的文件」这一条接上。**要在 `Run` 之前调**。
//
// ⚠️ **装在 `installLifecycle` 里，不是在 `Run` 里**：那条回调要在应用
// ready 之前就装好（启动命令行里的文件是在 ready 之后交付的），而
// `installLifecycle` 整个跑在 `mygo.App.Run()` 之前（见 `shell_window.go`）。
//
// ⚠️ **框架的 `OnOpenFile` 只认文件关联里登记过的扩展名**
// （`mygo/deeplink.go` 的 `fileArgs` 按 `packageFileExtensions` 过滤启动
// 参数）。而 macOS 那条路（Apple Event）**不过那一层过滤**——所以一个
// `.wma` 也可能被系统递过来（用户从别的应用把它拖到我们图标上，而那个
// 应用不挑扩展名）。那一档由 `checkOpenFile` 挡住并且说清楚。
func (s *Shell) installOpenFile() {
	mygo.App.OnOpenFile(func(path string) { s.handleOpenFile(path) })
}
