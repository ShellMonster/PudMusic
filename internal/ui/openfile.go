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
// ⚠️ **命令行那一条，框架会先扔掉一部分**：框架的 `fileArgs`
// （`mygo@v0.6.4` `deeplink.go:95-122`）只收「扩展名在关联列表里、而且是
// 一个**存在的普通文件**」的参数。**文件不在了、格式我们放不了、交给我们
// 的是个目录**——这三样在框架那一层就被丢掉了，**一个字都不说**，压根
// 到不了 `OnOpenFile`。
//
// 而这一票要求「收不下的要有用户可见的错误」。所以那一部分**我们自己在
// 命令行这一侧认下来**（`handleLaunchArgs`）：框架丢掉的那些路径由我们
// 自己判、自己说——而**框架会交的那些我们一件都不碰**（碰了就是队列里
// 进两份同一首歌）。
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
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/platform"
	"github.com/ShellMonster/Biu_Go/internal/service/localmusic"
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

// reportOpenFileRejection 把「没收下」这件事**让用户看见**。
//
// # 🔴 为什么不能只写一条 toast
//
// 这一票第二轮评审抓到的那个静默故障就在这儿：**写进 `toasts` 的状态不
// 请求重绘**（`addToast` 原来只写状态、播报、起一条收走的 goroutine，而
// `invalidate` 只在**收走**那一刻才调，见 `toast.go`）。于是：
//
//   - **当前帧上没有那句话**：界面停在旧帧上，用户眼前什么都没变；
//   - **迷你开着的时候更看不到**：主窗口是收着的，而 `viewToasts` **只在
//     主窗口那一支调**（`shell_view.go:258`），迷你那一扇**根本不画提示**。
//
// 所以这一条要做三件事：
//
//  1. **把这句话写进两扇窗都看的那一份状态**（`fileNotice`，见
//     `filenotice.go`）——迷你那一扇也画它，而它和主窗口那一条是**同一份**；
//  2. **要一帧**（`repaint`）——不然新状态要等下一个别的原因碰巧
//     重画界面时才看得见，而按需绘制的界面上「下一个别的原因」可能一直没有；
//  3. **把窗口叫回来**（用户双击一个文件期待的就是「窗口出现」）。
//
// # ⚠️ **迷你开着的时候**它**不让开迷你**
//
// 成功那一档是让开的（那一首要在主窗口上放，见 `handleOpenFile`），而**这一
// 档不让**：迷你常常就是用户眼前那一扇，而**迷你不画 toast**——所以那句话
// 落在收着的主窗口上时，用户眼前什么也没有（症状和「双击没反应」一样）。
//
// 让开迷你的话，那句话只能落在主窗口上，而主窗口是**被叫回来的**（用户会
// 看到它弹出来）——但**迷你那一扇上就什么也没有**了，而它才是用户刚才在看
// 的那一扇。迷你那一扇也画这句话（见 `viewMiniNotice`），于是两扇窗上都有。
//
// ⚠️ **主窗口还没建出来的时候**（应用刚起来）：这句话写在**共享的那一份**
// 上，而主窗口是 `WhenReady` 里才建的——建出来那一帧就会画到它（它待五秒，
// 而建窗口在应用 ready 之后）。这一条在无头环境里量不到（没有真窗口），
// 但**流程是对的**：那句话留在状态里，而状态是每帧从 `s.notice` 读的。
func (s *Shell) reportOpenFileRejection(rej openFileRejection, path string) {
	// 这句话落在**两扇窗共看的那一份**上（见 filenotice.go），而它自己
	// **要一帧**（见 `sayFileNotice`）。
	s.sayFileNotice(openFileRejectText(rej, path), notice.Danger)
	// 窗口叫回来：用户双击一个文件期待的是「窗口出现」，而错误也得有个落脚的
	// 窗口。
	//
	// ⚠️ **迷你开着的时候这一句叫回来的是迷你**（`openWindow` 优先迷你那一扇，
	// 见 lifecycle.go）——那正是用户眼前那一扇，而它现在画着那句话。
	s.openWindow()
}

// sayFileNotice 说一句「系统交给我们的那个文件」的话，并**要一帧**。
//
// # ⚠️ **它说两遍**
//
//  1. **一条 toast**（主窗口右下角那张卡片）：用户看惯的是那个位置，而
//     这一句在别的页面上的位置也该和别的提示一样。
//  2. **共享的那一份**（`fileNotice`）：**迷你那一扇也画它**——而迷你
//     **不画 toast**（`viewToasts` 只在主窗口那一支调），而迷你常常就是
//     用户眼前那一扇。少了这一半，「说了话但用户眼前什么也没有」照旧
//     发生，症状和「双击没反应」一模一样。
//
// ⚠️ **两扇窗都要重画**：共享的那一份在**两扇窗**里各画一遍，而**迷你
// 那一扇和主窗口是两次不同的画**——只叫醒一扇的话，另一扇停在旧帧上。
//
// ⚠️ **要一帧是这一条的要害**：状态写进去了，而按需绘制的界面停在旧帧上
// ——那句话就还不在画面上（症状是「用户双击了一个放不了的文件，什么都没
// 发生」，而界面上**连状态都记着**，只有画面上没有）。
//
// ⚠️ **用的是 `repaint`（绕过可见性闸门的那一半）**：用户双击了访达里的
// 一个文件，而**他眼前那一扇窗可能正好是收着的**（迷你开着的时候主窗口
// 收着）——那时候这一帧要是不画，那句话就等用户切回来才看得见（或者
// 看不见）。
func (s *Shell) sayFileNotice(text string, tone notice.Tone) {
	if s.notice == nil {
		s.notice = newFileNotice()
	}
	s.notice.say(text, tone, fileNoticeLifetime)
	// 主窗口那一条 toast（见上面第 1 条）。
	s.addToastFor(text, tone, fileNoticeLifetime)
	// 🔴 **两扇窗都要重画**：那句话在**两扇窗**里各画一遍，而它们是**两次
	// 不同的画**——只叫醒一扇的话，另一扇停在旧帧上，而用户眼前恰恰是
	// **没有**被叫醒的那一扇（迷你开着的时候主窗口收着，反之亦然）。
	if s.repaint != nil {
		s.repaint()
	}
	if s.mini != nil && s.mini.repaint != nil {
		s.mini.repaint()
	}
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
		s.reportOpenFileRejection(rej, path)
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
		s.reportOpenFileFailure(path, err)
		return
	}
	// ⚠️ **「正在放」这一句也要看得见**：它是一条 toast，而**提示不自己
	// 要帧**（见 `reportOpenFileRejection` 头上那一段）——所以这里显式要一帧。
	//
	// ⚠️ **它不进共享的那一份**（`fileNotice`）：那一份是**迷你那一扇**也画
	// 的，而「正在放」是**正常结果**——迷你让开之后用户看的是主窗口，而迷你
	// 那一扇上的「正在放」会在他五分钟之后重新开迷你时**突然冒出来**。
	s.addToastFor("正在放《"+app.LocalItem(path).Title+"》", notice.Success,
		fileNoticeLifetime)
	if s.repaint != nil {
		s.repaint()
	}
}

// reportOpenFileFailure 说「这个文件收下了，但**放不起来**」。
//
// ⚠️ **它和「没收下」是不同的事**：文件在、格式也认（`checkOpenFile` 过了），
// 是**解码器**放不了它（文件坏了、这台机器的解码器解不了这一种容器）。
// 所以那一句说的是「放不了」而不是「打不开」——用户拿到的信息不一样：
// 「打不开」指向文件，「放不了」指向这个文件里的音频。
//
// ⚠️ **可见性与「没收下」那一档一样**：让开迷你、叫回主窗、要一帧。
//
// ⚠️ **它让开迷你，而「没收下」那一档不让**：这一档是「文件**收下了**、
// 可是放不起来」——它已经进队列了，而**歌是在主窗口上放的**（放歌那一档
// 让开迷你，理由见 `handleOpenFile`）。所以迷你让开，而那句话落在**被叫回来
// 的主窗口**那条 toast 上。
//
// ⚠️ **而「没收下」那一档不让开迷你**：那一档**根本没有歌要放**，迷你就是
// 用户眼前那一扇——而迷你**不画 toast**，所以那句话得靠迷你自己也画那一份
// （见 `filenotice.go`）。
func (s *Shell) reportOpenFileFailure(path string, err error) {
	s.dropMini()
	s.openWindow()
	s.sayFileNotice("放不了「"+filepath.Base(path)+"」："+app.FriendlyErrText(err),
		notice.Danger)
}

// handleLaunchArgs 处理**命令行**里那些「系统交给本应用的文件」。
//
// # 这一条是补框架那一层扔掉的东西
//
// 框架把启动参数里的文件交给 `OnOpenFile` 之前，先过一遍 `fileArgs`
// （`mygo@v0.6.4` `deeplink.go:95-122`）：扩展名不在关联列表里的、**不存在
// 的**、**不是普通文件的**，全被丢掉——**一个字都不说**。
//
// 而这一票要求「收不下的要有用户可见的错误」。所以那些被丢掉的路径
// **我们自己在命令行这一侧认下来**：
//
//   - **本进程启动时的命令行**（`os.Args`）：框架在应用 ready 之后才交付
//     启动参数（`launchArgs`），而那一次交付同样只交「框架收得下的」那些；
//   - **第二个实例转交过来的命令行**（`OnSecondInstance` 的 args）：框架
//     的 `deliverArgs` 一样只交收得下的那些（`single_instance.go:145`）。
//
// # ⚠️ **不能和框架那一半重复处理**
//
// 框架**会**投给 `OnOpenFile` 的那些（存在 + 扩展名在关联列表里 + 是普通
// 文件）**我们一件都不碰**——碰了就是队列里进两份同一首歌（用户双击一个
// 文件，队列里出现两首）。
//
// 判据就是**框架那一段的判据**：`frameworkWouldDeliver` 照抄框架那三行
// （扩展名过滤 + `os.Stat` + `IsRegular`）。**凡是它会交的，跳过**；
// **凡是它不会交的，我们自己看一眼**。
//
// ⚠️ **只认「像文件路径的」参数**：命令行里还有别的（`biu://…`、各种开关），
// 而一个 `biu://settings` 交给 `checkOpenFile` 会得到「读不了它」——那是一句
// **用户根本没做过的错**。所以先按「是不是一个我们该管的路径」筛一遍
// （`looksLikeAudioPath`）。
//
// ⚠️ **扩展名不在关联列表里那一档要说清楚，但只对音频格式说**：`.txt` 那种
// 根本不是音频的文件，双击它和应用没有关系（系统不会把它交给本应用——
// 它不在 `fileAssociations` 里），而我们对它说「格式放不了」是**对着一个
// 真错说话**。所以这一档只对**扩展名像一个音频格式、只是我们放不了**的那些
// 文件说（`.wma`、`.ape`……），其余的**一个字都不说**。
func (s *Shell) handleLaunchArgs(args []string, workingDir string) {
	for _, p := range launchFileCandidates(args, workingDir) {
		s.handleOpenFile(p)
	}
}

// launchFileCandidates 从命令行里挑出「**该由我们说一声**」的那些路径。
//
// **纯函数**：不碰状态、不碰界面，于是能直接在测试里喂假的命令行。
//
// 返回的是**框架会丢掉、而用户该得到一句话**的那些路径。两种：
//
//   - 扩展名在我们放得起的关联列表里、但文件**不在了**或者**不是普通文件**；
//   - 扩展名**像音频**、但我们放不了（`.wma` 那一档）。
//
// ⚠️ **框架会交的那些一律不在这里**：扩展名在关联列表里、文件在、又是普通
// 文件——那一条由框架投给 `OnOpenFile`，我们再处理一遍就是队列里两份。
func launchFileCandidates(args []string, workingDir string) []string {
	var out []string
	for _, raw := range args {
		p, ok := launchArgPath(raw, workingDir)
		if !ok {
			continue
		}
		if !looksLikeHandedOverPath(p, filepath.IsAbs(raw)) {
			continue
		}
		if frameworkWouldDeliver(p) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// launchArgPath 把命令行里的一个参数翻成本地路径。
//
// 第二个返回值是「这个参数是一个我们该看的路径吗」——`biu://settings` 那种
// **别的 scheme** 返回 false。
//
// ⚠️ **`file://` URL 要翻成路径**：框架的 `fileArgs` 有这一段
// （`mygo@v0.6.4` `deeplink.go:100-104`——「Linux 的启动器可能传文件
// URL」），而**我们这一遍必须翻得一模一样**：不翻的话
// `os.Stat("file:///…")` 失败，我们会说「那个文件已经不在原来的位置上了」
// ——而那个文件明明在。
//
// ⚠️ **Windows 的盘符不是 scheme**：`C:\歌.flac` 里 `url.Parse` 认出的
// scheme 是 `c`——**一个字母**。框架的 `urlArgs` 有同一份判断
// （`len(scheme) > 1`，`deeplink.go:87`），所以这里照抄：scheme 只有一个
// 字母时**当作路径**。
func launchArgPath(raw string, workingDir string) (string, bool) {
	p := raw
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" {
		if u.Scheme != "file" {
			// 别的 scheme（`biu://…`）：框架自己会投给 `OnOpenURL`，而我们要
			// 说的是「文件放不了」——**不是**这一件事。
			return "", false
		}
		p = filepath.FromSlash(u.Path)
		if runtime.GOOS == "windows" {
			p = strings.TrimPrefix(p, `\`) // file:///C:/x
		}
	}
	if !filepath.IsAbs(p) && workingDir != "" {
		p = filepath.Join(workingDir, p)
	}
	return p, true
}

// looksLikeHandedOverPath 报「这个参数像不像**系统交给我们的一个文件**」。
//
// # 判据三条
//
//  1. **扩展名是我们放得起的关联格式**（`.mp3` 那一档）：那一定是「有人把
//     一个音频交给本应用」，而框架丢掉的那些（文件不在了、不是普通文件）
//     **必须由我们说**。
//
//  2. **扩展名是我们放不了、但确实是一种音频格式**（`.wma` 那一档）：用户
//     交给我们一个文件而我们放不了——**说清楚支持哪些**正是他要的。
//
//  3. **它是一个已经存在的目录，而参数是绝对路径**：那说明有人把**一个
//     文件夹**交给我们了（访达里拖一个文件夹到图标上就是这一档），而
//     「不是一个文件」是用户该知道的一句话。
//
// ⚠️ **第 3 条为什么要「绝对路径」这个限制**：命令行里还有各种开关
// （`--verbose`），而**一个相对的名字拼上工作目录之后可能碰巧撞上一个
// 目录**——那不是用户交给我们的东西，而是我们猜的。绝对路径没有这个
// 歧义：程序自己发的相对路径极少，而**系统交给我们的那些（资源管理器
// 双击）一律是绝对路径**。
//
// ⚠️ **第 3 条为什么不算「对着用户没做的事说话」**：一个**存在的目录**
// 出现在**绝对路径**的位置上，除了「用户交给我们一个文件夹」没有别的
// 解释。而 `checkOpenFile` 的判据顺序是「在不在 → 是不是普通文件 → 格式
// 认不认」（见那个函数的注释），所以一个目录落到的是「不是一个文件」
// 那一档——**而那一档正是这一票要求的三类错误之一**。
func looksLikeHandedOverPath(path string, wasAbsolute bool) bool {
	ext := platform.AudioExtOf(path)
	if looksLikeKnownAudioExt(ext) {
		return true
	}
	if !wasAbsolute {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// frameworkWouldDeliver 报「框架会不会把这个路径投给 `OnOpenFile`」。
//
// ⚠️ **照抄框架那三行**（`mygo@v0.6.4` `deeplink.go:95-122` 的 `fileArgs`）：
// 扩展名在关联列表里、**文件在**、**是普通文件**——三条都满足才交。
//
// **为什么要照抄而不是调框架那个函数**：那个函数是包内未导出的（我们
// `go:linkname` 那条路在这个仓库里不合适），而**照抄错了的后果是「同一
// 首歌进队列两次」**——那比「少说一句话」更坏。
func frameworkWouldDeliver(path string) bool {
	if !platform.IsAssociatedAudioExt(platform.AudioExtOf(path)) {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// looksLikeKnownAudioExt 报「这个扩展名像不像一个**音频**格式」。
//
// ⚠️ **这一条把「放不了」那一档限死在「音频格式」上**：一个 `.txt` 被双击
// 进来和应用没关系（系统根本不会把它交给本应用——它不在 `fileAssociations`
// 里），而我们对它说「这个格式我们放不了」是**对着一件用户没做的事报错**。
//
// 而 `.wma` 那种**确实是一种音频格式、只是我们放不了**的，双击它的人期待
// 的是「应用给我放这首」——说清楚「我们放不了这个格式、支持哪些」正是他要的。
//
// 判据用**两份名单的并集**：关联的那一份（我们放得起的）+ 扫描白名单
// （我们列得出来但放不了的，`.wma` 在那儿）。两份分别在
// `internal/platform` 和 `internal/service/localmusic`。
func looksLikeKnownAudioExt(ext string) bool {
	if platform.IsAssociatedAudioExt(ext) {
		return true
	}
	for _, e := range localmusic.Extensions {
		if strings.TrimPrefix(strings.ToLower(e), ".") == ext {
			return true
		}
	}
	return false
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
//
// # ⚠️ **这一条必须能被测试验到「真的装上了」**
//
// 这一票第二轮评审指出：原来的那条测试**先手工调 `installOpenFile`、再
// 直接调 `handleOpenFile`**，于是**把 `lifecycle.go` 里那一行
// `s.installOpenFile()` 删掉，测试照样绿**——而「Run 之前漏装 hook」正是
// 这一票最要紧、也最静默的故障（症状是「双击一个文件，应用起来了什么也
// 没放」，而且**一句报错都没有**）。
//
// 所以这一条走一个**可替换的接缝**（`openFileHooks`）：生产上它真的调
// `mygo.App.OnOpenFile`，而测试可以换掉它、断言「生产那条路上真的装上了」。
func (s *Shell) installOpenFile() {
	openFileHooks.onOpenFile(func(path string) { s.handleOpenFile(path) })
	// ⚠️ **本进程启动时的命令行**：框架在应用 ready 之后才交付启动参数
	// （`launchArgs`），而那一次交付**只交框架收得下的那些**——我们要在
	// 旁边自己认一遍（见 `handleLaunchArgs` 的注释）。
	//
	// ⚠️ **用 `WhenReady` 而不是现在就调**：启动参数里的路径要等到应用
	// ready 之后才交付（框架那一半也是那时候），而**我们这一半要排在
	// 框架那一半的前面**——否则「框架放得下的」那一档会被处理两遍
	//（队列里两份同一首歌）。
	//
	// ⚠️ **`WhenReady` 的回调是排队执行的**：框架的 `handleReady` 先跑
	// 我们的 `WhenReady` 回调，再 `postMain(launchArgs)`（框架的注释写得很
	// 清楚：`mygo/app.go:266`）——而 `postMain` 是**异步**的。所以我们这一
	// 遍在框架那一遍**之前**跑。
	mygo.App.WhenReady(func() {
		s.handleLaunchArgs(os.Args[1:], launchWorkingDir())
	})
}

// launchWorkingDir 报「本进程启动时的工作目录」。
//
// ⚠️ **相对路径要相对于它**：用户在访达里双击一个文件时，系统起的那个
// 进程的工作目录**不是用户以为的那个目录**（macOS 上是 `/`），而命令行
// 里的相对路径是相对于工作目录的。框架那一半用的是 `mygo` 自己的
// `startDir`（`mygo@v0.6.4` `deeplink.go:190`），而那个是**包内未导出**的
// ——所以我们自己取一次。
func launchWorkingDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

// openFileHooks 是「系统把文件交给本应用」那几条接线**可替换的接缝**。
//
// # 为什么要有这一层
//
// 框架的 `mygo.App` 是**包级单例**，而 `OnOpenFile` / `OnSecondInstance`
// 都是**注册到那个单例上**的：无头 Tester 里跑不了真的 `App.Run()`（跑了
// 就把整个测试进程的应用状态搅了），所以**测不到「生产那条路上真的装上了
// hook」**。
//
// 而「Run 之前漏装 hook」恰恰是这一票最要紧的静默故障：症状是「双击一个
// 文件，应用起来了而什么也没放」，**一句报错都没有**。原来那条测试
// （先手工 `installOpenFile`、再直接调 `handleOpenFile`）**抓不住它**——
// 把 `lifecycle.go` 里那一行删掉，测试照样绿（第二轮评审实测过）。
//
// 所以这里把「注册」这一步做成**可替换的接缝**：生产上它真的调框架那两
// 个 API，而测试可以换掉它、**断言注册发生了、而且注册的是我们自己的
// 那一半**。
var openFileHooks = openFileHookSet{
	onOpenFile: func(fn func(path string)) func() {
		return mygo.App.OnOpenFile(fn)
	},
	onSecondInstance: func(fn func(args []string, workingDir string)) func() {
		return mygo.App.OnSecondInstance(fn)
	},
}

// openFileHookSet 是那两个注册动作。做成一个结构体而不是几个包级变量，
// 是为了**换的时候一起换**（漏换一半的话，测试量到的是「一半装了」，
// 而那正是它该抓的东西）。
type openFileHookSet struct {
	// onOpenFile 注册「系统交给我们一个文件」那一档。返回摘掉它的动作。
	onOpenFile func(fn func(path string)) func()
	// onSecondInstance 注册「第二个实例把命令行转交过来了」那一档。
	// 返回摘掉它的动作。
	//
	// ⚠️ **它和生产那一条是同一个接缝**：第二个实例转交过来的命令行里
	// **也可能有文件**，而**框架只交它收得下的那些**（见 `handleLaunchArgs`）。
	// 而「框架在 `OnSecondInstance` 之后才 `deliverArgs`」
	//（`mygo@v0.6.4` `single_instance.go:145`）——所以**我们这一遍在前**，
	// 那正是我们要的顺序。
	onSecondInstance func(fn func(args []string, workingDir string)) func()
}
