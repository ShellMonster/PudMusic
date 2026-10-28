// 这一块是**窗口与生命周期**：一个外壳怎么挂到窗口上、窗口怎么开、关的时候
// 收掉什么、以及退出流程怎么走。
//
// 这一块和另外三块之间没有互相依赖的顺序——它们都是 `Shell` 的方法，同一个包，
// 互相调得到。搬到一起只是为了「找一件事只看一个文件」。
//
//   - `Close` / `closePerWindow` —— 收尾：定时器、唤醒句柄、封面缓存、落盘。
//   - `bindMedia` —— 把系统媒体会话接到窗口上（只有 Windows 那边看它）。
//   - `Attach` —— 窗口的生命周期回调（显示/隐藏、拖文件、进全屏、关闭）。
//   - `Run` —— 开出一个窗口并跑起来（含单实例锁、恢复上次播放、启动那一摊）。
//   - `WindowOptions` —— 主窗口的样子。
package ui

import (
	"log"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/media"
	"github.com/ShellMonster/PudMusic/internal/platform"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Close 收掉这个外壳占着的一切：四个定时器、封面缓存、应用状态，以及设置 /
// 搜索历史 / 快捷键的落盘收尾。**可以重复调。**
//
// ## 为什么它得是一个方法，而不是一段挂在窗口回调里的代码
//
// 这一段收尾以前只长在 `Attach` 的 `OnClosed` 上，而 `OnClosed` 只在「挂着的那个
// 窗口真的关掉了」的时候才跑。于是**持有这些资源的其实不是窗口、是外壳**，而外壳
// 却只有一条能收尾的路：
//
//   - 快捷键的后台写循环（`shortcutKeys.loop`）、红点那个每五分钟一跳的定时器、
//     404 那个每一秒一跳的定时器、搜索防抖那个钟——一律没人收。
//   - 每一处都是**一个外壳一份**，而这条路上一个都不会走到。
//
// 真进程退出时这不算什么（进程一死它们跟着死）。真正付账的是**不退出进程的那些
// 路**：无头测试里一个外壳就是一个 `Tester`，几百条测试建几百个外壳，每一个都带
// 一个写循环和两个定时器跑到测试二进制结束；而「建了外壳但窗口最终没建出来」的那
// 些启动路径（`openWindow` 排在 `WhenReady` 队列里，ready 之前就返回）也是同一
// 个形状。
//
// 收尾因此挂在**外壳**上而不是窗口上：窗口关了就调它（`Attach` 的 `OnClosed`），
// 没建出窗口的那条路也调它，测试的 `t.Cleanup` 还是调它——三处走的是同一份代码，
// 不会再有「窗口的回调忘了谁」这种事。
func (s *Shell) Close() {
	// ⚠️ **这里不提前 return**。底下每一项各自都是幂等的（定时器那几个是
	// 「判 nil → 调 → 置 nil」，`images.Close` / `app.Close` / `hist.Close` /
	// `keys.Close` 各自都有 `closed` 判断），所以再走一遍既无害、又是必须的：
	// 万一关掉之后还有一帧画到了顶栏（无头 Tester 里 `Close` 之后再 `Frame()`
	// 就会，见 `close_while_loading_test.go`），那三个「装定时器」的地方要看
	// `s.closed` 才肯装——但路径上还有别的呢，「再走一遍」就是最后一道网。
	//
	// 早先这里写的是「`closed` 就 return」，而它挡住的正是这件事：`Close` 走过一趟
	// 之后装的定时器，第二次 `Close` 收不掉（`feed.arm` 那一处在关掉后仍会被每帧触
	// 发），于是它一路挂到测试进程结束。
	s.win = nil

	// 这一扇窗自己占着的东西——**只收这一扇的**。迷你播放器那个外壳走的是这条路
	// 而不是 `Close`：它不能停播放、不能关设置（那是整个应用的事）。
	//
	// 所以这一段被拆出来了：`Close` 收完它再收应用层那几样，而迷你窗口只调它。
	// 不拆的话迷你那一份就得把下面这四行抄一遍——抄的那份迟早和这里不一样。
	s.closePerWindow()

	// 停播放、关解码器和文件句柄、停后台时钟。
	s.app.Close()

	// 关窗就收尾设置：停掉后台写盘，并把还欠着的改动同步落完。
	//
	// MountSettings 里那个 OnWillQuit 的 Flush 兜的是「退进程」这条路；
	// 这一句兜的是「关窗口」——托盘早就在了（issue #28 已 CLOSED：
	// `tray.go` / `single_instance.go` / 开机自启），所以「关窗口」早就
	// 不等于「退进程」了，那条 OnWillQuit 并不保证会跑，两句要一起看。
	//
	// `CloseSettings` 可以重复调（内部先 LoadAndDelete），所以窗口关了之后
	// 进程再退、再调一次都不会出错。
	if err := s.app.CloseSettings(); err != nil {
		log.Printf("biu: 关窗时保存设置失败：%v", err)
	}
	// 搜索历史与快捷键那两样在 `closePerWindow` 里收了（它们是逐窗口的）。
}

// closePerWindow 收掉**这一扇窗自己**占着的东西：三个定时器、唤醒句柄、封面缓存、
// 搜索历史和快捷键的落盘。**可以重复调**（底下每一项各自幂等）。
//
// ## 为什么它和 `Close` 分开
//
// `Close` 是**整个应用**的收尾——它还 `s.app.Close()`（停播放、关解码器）、关设置。
// 而迷你播放器是**第二个窗口**，它那个外壳关掉时那些一概不能做：关掉一扇小窗就把正
// 在放的歌停掉，而主窗口明明还开着（症状是「从迷你切回来，音乐没了」，和真因隔得
// 很远）。所以它走这一条。
//
// 另一半（两个窗口共用同一个 App 那一层）压根不该由任何一个窗口来收——那是
// `App.Close` 的事，由主窗口那一侧的 `Close` 触发。
func (s *Shell) closePerWindow() {
	// 🔴 **这道闸门在这一段里，不在 `Close`**：三个「装定时器」的地方（`feed.arm`、
	// `armNotFound`、`syncSearchWatch`）看的是 `s.closed`，而迷你播放器那个外壳走的
	// 是这一段——它收完就该和主窗口那个一样「不再装任何定时器」，哪怕之后还有一帧
	// 画到（无头 Tester 里关掉之后再 `Frame()` 就会，见 `close_while_loading_test.go`）。
	s.closed = true

	// 三个定时器。全是「窗口没了之后唤醒对它是 no-op，留着不致命，但会一直醒到
	// 进程退出」那一类。
	//
	// ⚠️ 搜索防抖那个钟**不在** viewMain 的 syncSearchWatch 管辖范围内：
	// 那一句只保证「用户切走时它会停」，而「外壳没了」它管不着。
	if s.searchWatchStop != nil {
		s.searchWatchStop()
		s.searchWatchStop = nil
	}
	s.feed.disarm()    // 红点，每五分钟一跳（navbar_feed.go）
	s.disarmNotFound() // 404 倒计时，每一秒一跳（notfound.go）
	s.disarmVolIdle()  // 音量浮层那个到点叫醒界面的钟（playbar_volume.go）

	// 🔴 摘掉这一条唤醒句柄（铁律三那个逐窗口的列表）。摘掉之后调它是 no-op，
	// 而不摘不致命——但「进迷你 / 退出迷你」来回十次之后那个列表就有十条，
	// 每改一次状态就多走十趟空转。
	if s.wakeRemove != nil {
		s.wakeRemove()
		s.wakeRemove = nil
	}

	// 摘掉这一扇窗在应用层那条播报登记（理由同上面那条唤醒句柄）。
	if s.announceRemove != nil {
		s.announceRemove()
		s.announceRemove = nil
	}

	// 把这一扇窗从「活跃态」那本账上摘掉（理由见 notification.go 的
	// `releaseNotification`）：不摘的话那本账会记着一扇已经没了的窗，而它
	// 「有焦点」的话——再也弹不出系统通知。
	s.releaseNotification()

	// 封面缓存：先摘掉这一扇窗的唤醒登记（借来的那一份活得比这一扇窗久），
	// 再放开自己那一份使用权。
	//
	// ⚠️ **放开的不是「关掉」**：迷你窗口借的是主窗口那份封面缓存（一个磁盘
	// 缓存目录只能有一本账），引用计数到 0 才真的关。所以主窗口那个外壳先收
	// 尾时迷你那份照样能用——不然迷你之后每要一张图都是 `ErrClosed` 永久失败
	// 态，界面上的症状只是「图不出来」。
	//
	// `imagesReleased` 保证**只放开一次**：`closePerWindow` 可以重复调（关窗、
	// 退迷你、测试收尾都会调它），多放开一次会把别人那份一起带走。
	if s.imagesWakeRemove != nil {
		s.imagesWakeRemove()
		s.imagesWakeRemove = nil
	}
	if !s.imagesReleased {
		s.imagesReleased = true
		s.images.Close()
	}

	// 🔴 摘掉「本地库变了」这一条（`New` 里登记的）。不摘的话「进迷你 / 退出迷你」
	// 来回十次之后那个列表就有十条，每扫一遍库就多走一趟空转——而这一条属于**已经
	// 收掉的**那个外壳。
	if s.localWakeRemove != nil {
		s.localWakeRemove()
		s.localWakeRemove = nil
	}

	// 搜索历史与快捷键：写盘是后台合并的，最后一下有可能还等在退避窗口里。
	//
	// ⚠️ **借来的那一份不能收**（`shared`）：它们此刻属于另一个外壳，而后台写盘
	// 循环停掉之后，主窗口在设置页改的快捷键就再也落不了盘。这里收的只是**自己开的**
	// 那两份。
	if s.shared {
		return
	}
	_ = s.hist.Close()
	_ = s.keys.Close()
}

// bindMedia 把系统媒体会话接到这个窗口上。
//
// **只有 Windows 那一边看它**：那边的媒体会话是按窗口算的（`GetForWindow`），
// 任务栏缩略图上的按钮更是直接长在某个 HWND 上，而窗口要到这里才有句柄。
// macOS 那边是全局的，绑定是一次空操作——所以它不能在 MountMedia 里做
// （那时候窗口还不存在）。
//
// 重复调安全：`Session.BindWindow` 会先把上一个窗口收干净。
func (s *Shell) bindMedia(w *mygo.Window) {
	if s.media == nil || w == nil {
		return
	}
	// NativeHandle 在 macOS 上是 NSWindow*、Linux 上是 GtkWindow*，
	// 那边 bindWindow 原样忽略（见 media.windowBinder）。
	s.media.BindWindow(w.NativeHandle())
}

// Attach 把外壳接到窗口上：装唤醒回调，装关闭策略，窗口真关掉时收尾。
//
// ⚠️ **这一整套只给主窗口用**。迷你播放器窗口走 `attachMiniWindow`：它**不**能
// 装下面那个 `OnClosed`（那是「整个应用收尾」——停播放、关设置、存盘），套上去
// 的话关掉迷你窗口会把正在放的歌停掉，而主窗口明明还开着。
//
// 一个窗口上的生命周期回调全在这儿，以后新增的（显示/隐藏、拖拽文件…）也挂这里：
// 分成两处的话，总有一个回调会在换窗口的时候漏装，而漏装之后的症状
// （「窗口没关干净」「图下完了界面不动」）都跟漏装的那一行毫无关系，极难查。
func (s *Shell) Attach(w *mygo.Window) {
	s.win = w
	// 「用户看不看得到这一扇窗」从这一刻起由系统推过来（票 #156）：
	// 失焦、最小化、隐藏各自会更新 `presence`，重新看得见时补一帧。
	s.watchWindow(w)
	// 🔴 铁律三：**逐窗口**登记，App 那边挨个叫醒。只叫醒主窗口的话迷你窗口停在
	// 旧内容（实测：只投递不唤醒两个窗口各 +0 帧；只唤醒主窗口则迷你窗口停在旧内容）。
	s.wakeRemove = s.app.AddWake(s.wake.invalidate)

	// 从访达 / 资源管理器拖进来的文件：音频进本地音乐，别的说清楚为什么
	// （见 `localmusic_drop.go`）。挂在这里而不是 `New`：`Attach` 才是窗口
	// 生命周期回调的入口，而且这一套只给主窗口——迷你播放器那一扇不画本地
	// 音乐页，也没有画 toast 的地方。
	w.OnFileDrop(func(e *mygo.FileDropEvent) { s.handleFileDrop(e.Paths) })

	// 进 / 出全屏：侧栏左上角那个品牌块在 macOS 非全屏时要多留一格（上游的
	// `pt-8`，见 `sidebar.go` 的 `sideBrandTopPad`）。
	//
	// ⚠️ **改完要叫醒一帧**：这一格是画出来的，状态变了而画面没重画的话，
	// 用户看到的还是进全屏之前的样子（而窗口在进全屏那一刻本来就会重画，
	// 但**出**全屏那一帧不一定）。
	w.OnEnterFullScreen(func() { s.fullScreen = true; s.wake.invalidate() })
	w.OnLeaveFullScreen(func() { s.fullScreen, s.videoMadeFull = false, false; s.wake.invalidate() })

	w.OnClose(func(e *mygo.CloseEvent) {
		opt := s.app.SettingsSnapshot().CloseWindowOption
		// 记一份：OnClosed 会把设置的挂载摘掉，之后再问就只剩默认值了（见
		// installLifecycle 里 OnWindowAllClosed 那段的注释）。
		s.closeOption = opt
		if planWindowClose(s.quitting, opt) != closeToBackground {
			return
		}
		e.PreventDefault()
		platform.HideToBackground(w)
	})

	// ⚠️ 下面这条是**真的关掉窗口**才跑的收尾：关封面缓存、停播放、关 App。
	// 「收起来」被上面拦在 OnClose 里了，压根到不了这里——所以关窗口时选
	// 「隐藏」，播放不会停、应用不会死。这是两条不同的路，别混。
	//
	// 收尾本身是 `Shell.Close`（见它的注释：以前这一段只长在这儿，于是没挂到
	// 窗口上的外壳永远收不掉那几个定时器）。
	//
	// ⚠️ 所以这个回调**不在 OnClose** 里：选「隐藏到托盘」时 OnClosed 压根不跑
	// （窗口只是收起来了，应用还活着），那时候红点和 404 的倒计时都该继续走
	// ——用户把窗口叫回来时本来就该看到已经回首页的样子，不是还停在一个正在
	// 倒计时的 404 上。
	w.OnClosed(func() {
		if s.win == w {
			s.win = nil
		}
		// 摘掉这一条唤醒句柄（铁律三那个列表）。
		//
		// ⚠️ 这一句 `Close()` 里也有，但**这里还要有一份**：`Close` 是「整个外壳
		// 收尾」，而迷你窗口那个外壳**不能**调它（它会 `s.app.Close()` 把正在放
		// 的歌停掉）。两处走的是同一个 `s.wakeRemove`，摘掉之后第二次调是空操作。
		if s.wakeRemove != nil {
			s.wakeRemove()
			s.wakeRemove = nil
		}
		s.Close()
	})
}

// Run 开出一个窗口并跑起来。窗口骨架归界面层管：以后要加迷你播放器、
// 要改标题栏、要调窗口尺寸，都在这一处。
//
// sess 是 `MountMedia` 递过来的那个系统媒体会话，可以是 nil（那一轮没装上，
// 或者调用方不想要）。**它得在这一层而不是在 MountMedia 里绑窗口**：
// Windows 的媒体会话是按窗口算的，而窗口在这里才建出来——见 bindMedia。
func Run(a *app.App, sess *media.Session) error {
	// 单实例锁必须在 Run 之前拿：Run 之后事件循环才起来，那时候两个实例
	// 同时抢就是竞态，谁抢到全看调度。抢不到的那个，框架已经把它的命令行
	// 转交给第一个实例了（`platform.Lock`），自己立刻退出——**不能留在后台**
	// 当一个没有窗口的僵尸进程。
	if !platform.Lock() {
		return nil
	}

	s := New(a)
	s.media = sess
	// 主窗口那个外壳登记一下（见 notification.go 的 `mainShellRef`）：系统通知
	// 的那一句提示得由它弹——迷你播放器画的是「正在放什么」，而通知指向的是
	// 主窗里的下载记录页。
	setMainShell(s)
	// 真的那一份全局快捷键**在这里**装上，而不是在 `New` 里：`mygo.GlobalShortcut`
	// 的两个方法都要走事件循环，而 `New` 的调用方还有一整套测试（无头 Tester 没有
	// 事件循环，`Register` 在那儿是直接 panic 的）。`Run` 是唯一一个真的有事件循环
	// 的地方，而它是 main.go 唯一调用的那一个。
	s.hotkeys = systemGlobalShortcuts{}
	// 搜索历史读盘是阻塞 IO，放在建窗口之前：早读进来早能用，也省得第一帧
	// 是在一个空历史上画出来的（那也是 mountSearchHistory 的用途之一）。
	s.mountSearchHistory()
	s.mountShortcuts()
	// 应用的语言：简体中文。**要在 Run 之前**——应用一开起来，框架就把语言
	// 定下来，而菜单是那之后按那个语言重新贴一次文案的（见 language.go）。
	installLanguage()
	// 把语言**现在就**定下来，好让紧接着建的那份菜单从头就是中文的。
	// ⚠️ 这一句**只能在主线程上调**（生产路上就是主线程）——理由见
	// language.go 的 `resolveLanguageNow`。
	resolveLanguageNow()
	// 菜单、退出、第二个实例、deep link 全在这里装。必须在 Run 之前，
	// 否则启动命令行里的 biu:// 会在回调装好之前就被交付掉。
	s.installLifecycle()

	// 「上次听到哪儿」恢复：队列、当前项和播放位置都从上次退出时的那份恢复，
	// 恢复出来是**暂停**的（启动时不该突然出声——上游 Biu 和 Dopamine 都这样）。
	//
	// ⚠️ **排在 `New(a)` 之后**：播报句柄是 `New` 装的（见它那儿的
	// `AddAnnounce`），排在它前面的话，那句「已恢复上次的位置」送不出去——
	// 恢复是启动时唯一会用它的地方。
	//
	// ⚠️ **排在开窗口之前**：恢复完的第一帧就该是恢复后的样子（歌名和进度条
	// 都在那儿），而不是先画一帧空的再换掉。读盘 / 开解码器是阻塞 IO，排在
	// 建窗口之前也是为了这个（理由同上面那句 `mountSearchHistory`）。
	//
	// 失败不挡启动：恢复不出来就照常开一个空队列的应用。
	if err := a.RestorePlayback(); err != nil {
		log.Printf("biu: 恢复上次播放的状态失败：%v", err)
	}

	mygo.App.WhenReady(func() {
		// 🔴 **冷启动那一段的「应用就绪」时刻**（票 #478）：这一句之前是
		// 「进程起来 → 读盘 + 读凭据 + 挂零件」，之后才是「建窗口」。
		// 两段分开量，才答得出「慢的是读盘还是画窗口」。
		a.NoteReady()
		// scheme 在这里登记而不是 Run 之前：登记要走系统 API，事件循环起来
		// 之后才接得住；而它又必须早于框架那次启动参数的交付——handleReady
		// 把交付排在所有 WhenReady 回调之后（`mygo/app.go:266`），所以正好。
		if err := platform.RegisterScheme(); err != nil {
			// macOS 上没打包成 bundle 就必然登记不上，这是预期的，不是启动失败。
			log.Printf("biu: 登记 %s:// 失败，从别的程序点这个链接不一定唤得起本应用：%v",
				platform.URLScheme, err)
		}
		// 托盘也在这儿建：框架建托盘要过事件循环（NewTray 在没 ready 时 panic）。
		// 托盘要在开窗口**之前**建：用户从托盘点「显示/隐藏」时窗口已经在那里，
		// 顺序反了的话头几秒点托盘是没有反应的。
		s.installTray()
		s.syncAutoStart()
		// 全局快捷键也在这儿登记，理由和上面两件一样：`mygo.GlobalShortcut.Register`
		// 头上有 `needsApp`，应用没 ready 时调它直接 panic（`modules.go:550`）。
		// 排在 `openWindow` 之前：窗口还没建的时候注册失败要弹的那句提示，
		// 第一帧就能画出来（提示挂在壳上，见 addToast）。
		s.syncGlobalShortcuts()
		// 本地音乐目录的文件监听。⚠️ 必须在 `WhenReady` 里：`mygo.WatchFiles`
		// 在应用还没 ready 的时候调会 panic（框架的 `needsApp`，
		// `mygo/app.go:128`），而 `Run` 之前是那一刻。
		a.StartLocalMusicWatch()
		// 开机时如果已经登录，顺手把侧栏的收藏夹拉一次。
		//
		// 用户不该为了看见自己的收藏夹，先去头像下拉里按「刷新数据」——那是一
		// 个没人会自己想到的操作，而侧栏上长期空着会被当成「我没有收藏夹」。
		//
		// ⚠️ **排在单实例锁之后**（锁在 `Run` 的第一句）：锁没拿到的话这个进程
		// 马上就要退出，而这一趟是往 B 站发请求——第二个实例白发两次请求，
		// 而用户那边什么也看不出来。
		//
		// ⚠️ **排在开窗口之前**：拉回来赶得上的话，第一帧上就有收藏夹；
		// 赶不上的话那一行「正在刷新收藏夹…」会在（见 `viewFavoritesRefreshing`），
		// 回来之后界面会被叫醒。
		//
		// ⚠️ **没登录时它一个请求都不发**（见 `app.AutoRefreshFavorites`）。
		a.AutoRefreshFavorites()
		// 后台自动查更新。**必须在 `WhenReady` 里**：第一次查询要走网络，而
		// `WhenReady` 之前事件循环还没转起来，那时候发请求等于在一条还没开始
		// 跑的路上走（框架自己也把「需要事件循环」的 API 挡在 ready 之前，见
		// 上面 `StartLocalMusicWatch` 那一条）。
		//
		// 停掉它的是 `App.Close`（`updater.close`），而 `Close` 挂在退出流程上——
		// 所以关掉应用之后不会有下一次查询在后台跑着。
		a.StartAutoChecks()
		s.openWindow()
	})
	return mygo.App.Run()
}

// WindowOptions 是主窗口的样子。view 是这个窗口每帧要画的界面。
//
// ⚠️ macOS 上要的是**保留红绿灯的隐藏标题栏**，不是 `Frameless: true`——
// `Frameless` 会把系统控件一起删掉，红黄绿就没了。Biu 是无边框、标题栏自
// 绘、系统控件留在上面，MyGo 的 `TitleBarHidden` 说的就是同一件事
// （`window.go` 里 `hiddenTitleBar = !Frameless && (TitleBarHidden || …)`）。
// `TitleBarHiddenInset` 只是把控件再往里缩一点，Biu 没做这个差别。
func WindowOptions(view func(*ui.Context)) mygo.WindowOptions {
	return mygo.WindowOptions{
		Title:     appName,
		Width:     1200,
		Height:    800,
		MinWidth:  900,
		MinHeight: 560,
		// 整条顶栏可拖窗口，控件叠在它上面。
		TitleBarStyle:        mygo.TitleBarHidden,
		TrafficLightPosition: &mygo.Point{X: 14, Y: 18},
		// 窗口停在用户上次关掉它的地方。
		StateKey: "main",
		// 窗口里是 MyGo 自己画的界面，不是网页。
		Content: ui.View(view),
	}
}
