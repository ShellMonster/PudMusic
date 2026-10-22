// 迷你播放器窗口（SPEC 需求 43~46）：320×100 的透明置顶小窗，贴边悬浮。
//
// # 和 `/mini-player` 那一条路由是两件事
//
// `nowplaying.go` 的 `viewNowPlaying` 是**主窗口里的一页**（歌名 + 歌词），
// 它在路由表里占一条（`router.go` 的第 13 条）。这一票要的是 Biu 的
// `electron/mini-player.ts`：**另一个窗口**——320×100、无边框、透明、置顶，
// 停在屏幕一角，音乐还在放，界面上还控制得了。
//
// Biu 的切法是「主窗口 hide() + 开一个 mini 窗口」，两个窗口互斥
// （`electron/ipc/window.ts:41-47`），所以同一个按钮既是「进迷你」也是「回主窗口」。
//
// # 状态是同一份，窗口是两份
//
// 两个窗口各有各的 `Shell`（一个外壳对一个窗口，`Shell.win`），但**播放状态是
// 同一个 `internal/app.App`**。于是「两个界面上显示不同」这件事要靠两样东西挡住：
//
//  1. 读的是同一个 `App.Snapshot()`，界面层**不缓存**播放状态；
//  2. 改完状态之后**逐窗口**唤醒（铁律三）：`App.AddWake` / `App.AddLocalWake`
//     是挨个叫醒的，`Attach` / `attachMiniWindow` 每个窗口登记一次。
//
// 只叫醒主窗口的话，迷你窗口停在旧内容（实测：只投递不唤醒两个窗口各 +0 帧；
// 只唤醒主窗口则迷你窗口停在旧内容）。
//
// # 三样必须自己做、MyGo 不替我们做的事
//
//  1. **背景**：窗口是 `Transparent: true`，框架会把帧清成透明
//     （`mygo/window.go:433`「Backends keep transparent windows clear」），
//     主题背景**不填**。不自己画一块不透明底的话，这个窗口在桌面上是隐形的
//     ——而「窗口开着却看不见」是最难查的一种错。
//  2. **主题**：铁律二，每一帧第一件事（`applyFrameTheme`）。
//  3. **recover**：view 里 panic 会穿透 Cocoa 事件循环杀掉整个进程，所以
//     `NewMini` 外面包一层 `Guard`。
package ui

import (
	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/platform"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// 迷你播放器窗口的尺寸，照 Biu 的 `electron/mini-player.ts:16-17`：
//
//	width: 320, height: 100
//
// ⚠️ **不许放大**：它要的是「不占屏幕但还能控制播放」（SPEC 需求 43），而 320
// 正是 Biu 把封面和右边那一列塞下之后仍然不显挤的宽度。
const (
	miniWidth  = 320
	miniHeight = 100

	// miniPad 是外圈留白。Biu 那边留白来自圆角那一圈，这里留 6：让圆角那半圈
	// 透明边看得出来（否则「窗口是透明的」这件事在画面上一点都看不出来）。
	miniPad = 6

	// miniCoverCol 是封面那一列的宽。Biu 那边是 100（`CoverView` 的 `w-[100px]`），
	// 右边只剩 220；我们取 64，把省下来的 36 留给歌名——那一行现在还有音量条
	// （上游没有，用户定的），而控制那一行图标化之后只要 88 宽（账在
	// `viewMiniTransport`）。
	miniCoverCol = 64
	// miniCover 是封面本身的边长 = miniCoverCol - 左右各一个 miniPad
	//（封面那一列的内边距是 `Padding(miniPad, 0, miniPad, miniPad)`）。
	miniCover = miniCoverCol - 2*miniPad

	// miniTitleMinW 是歌名那一格最少留多宽。
	//
	// ⚠️ **没有它，歌名会被挤成 0 宽**：右边那一列的宽是 320 - 64（封面）-
	// 12（列的内边距）= 244，扣掉音量 80、退出那个键 28 和两道 6 的缝之后剩 124。
	// 够画七八个字，而**界面上看不出坏了**（字还在那儿，只是没了）。60 是「还
	// 认得出是哪首」的下限——留到 60 是给更窄的窗口兜底。
	miniTitleMinW = 60

	// miniVolumeW 是音量条的宽度。拿 80 是「还能看出音量是一到几成」的下限。
	miniVolumeW = 80

	// miniBackBtnW 是「回到主窗口」那个键（`iconExpand`）的点击区边长：
	// 18 的图标 + 每边 5。
	//
	// 上游那一枚是 16 的 `RiExpandDiagonalLine`（`pages/mini-player/index.tsx:203-212`），
	// 我们取 18 是为了和旁边那两个同尺寸——同一行里的图标键忽大忽小，看着像漏画了。
	miniBackBtnW = 28

	// miniStepW 是上一首 / 下一首的点击区边长：18 的图标 + 每边 5
	// （上游 `size={18}`，`pages/mini-player/index.tsx:176,189`）。
	//
	// ⚠️ **原来是 64 宽的文字键**，三个汉字 39（13 号字）扛着，窄一个字节就画成
	// 「上一…」。图标化之后那笔账整笔勾销：28 的点击区比 64 窄一半还多，而图形
	// 不再有「被截成偏旁」这回事（见 `TestTheMiniKeysAreIcons`）。
	miniStepW = 28

	// miniPlayW 是播放 / 暂停那个键的点击区边长：28 的图标 + 每边 2
	// （上游 `size={28}`，`pages/mini-player/index.tsx:186`）。
	//
	// 它是这一行里最大的一枚，在 320×100 那个小窗里当视觉重心——和播放条上
	// 那一枚（48）是同一个安排，只是尺寸照上游的迷你播放器。
	miniPlayW = 32
)

// MiniWindowOptions 是迷你播放器窗口的样子。view 是这个窗口每帧要画的界面。
//
// ⚠️ **`Frameless: true` 在这里是想要的**，和主窗口正相反：主窗口要的是
// 「红黄绿留着」（`window.go` 里 `hiddenTitleBar = !Frameless && …` 的那道
// 判断），而 Biu 的迷你窗口 `frame: false`，压根没有系统控件——它自己画一整块。
// 这里走真无边框。
//
// # ⚠️ 为什么**没有** `DisableResize`（Biu 那边有）
//
// 这不是抄漏了，是照抄会让 `StateKey` 整个失效。实测出来的链路，三步：
//
//  1. `DisableResize` 在 macOS 上就是从 `styleMask` 里去掉 `NSWindowStyleMask-
//     Resizable`（`mygo/internal/darwin/window.go:123`）。
//
//  2. 一个没有 resizable 位的窗口**没有「标准框」可言**，`isZoomed` 恒为真
//     （`mygo/internal/darwin/window.go:779` 那句 `sendBool(w.win, "isZoomed")`）。
//
//  3. `StateKey` 存不存位置，判的是 `!Maximized`
//     （`mygo/window_state.go:188` 的 `captureState`）：
//
//     if !st.Maximized && !st.FullScreen && !n.IsMinimized() { 记下 X/Y/W/H }
//     if st.Width <= 0 || st.Height <= 0 { return }               // 新键直接不存
//
// 于是那个窗口**一条都存不下来**：`window-state.json` 里连 `"mini-player"` 这个
// 键都不会有，下次开还是走 `X/Y` 的默认值，也就是每次都回到 `miniCorner()` 算出来
// 的右上角——而注释还在说「记住了」。
//
// 四个变体各建一扇**真窗**问 `IsMaximized()`（本机实测，`HOME` 重定向 + 死代理，
// 不打任何真接口）：
//
//	Frameless + DisableResize        IsMaximized=true    ← 存不下来
//	Frameless + 去掉 DisableResize   IsMaximized=false   ← 存得下来
//	不 Frameless + DisableResize     IsMaximized=true    ← 与无边框无关
//	min==max==320×100（钉死大小）     IsMaximized=true    ← 也钉不住
//
// 另外两条试过也没用：`w.Unmaximize()` 对不可缩放的窗口是空操作；建完再
// `SetResizable(true) → Unmaximize() → SetResizable(false)`，改回去的那一下
// `isZoomed` 又变回真。
//
// 代价：用户能把这个窗口**往大拖**。而那没有后果——右边那一列是 `Grow(1)`，
// 宽出来的部分全给歌名；**缩不到更小**由 `MinWidth/MinHeight` 兜住，三行控件
// 不会被裁掉。
func MiniWindowOptions(view func(*ui.Context)) mygo.WindowOptions {
	return mygo.WindowOptions{
		Title:  "BiuGo 迷你播放器",
		Width:  miniWidth,
		Height: miniHeight,
		// 🔴 `Transparent` 必须配着自己画的背景，理由见文件头那一条。
		// 单开这一个开关而忘了画底，窗口在桌面上是隐形的。
		Transparent: true,
		// Biu：`alwaysOnTop: true, skipTaskbar: true`。
		// 「置顶」是它作为悬浮控件的本体；「不进任务栏 / 程序坞」是为了不跟
		// 主窗口抢同一个格子。
		AlwaysOnTop:       true,
		SkipTaskbar:       true,
		Frameless:         true,
		DisableMinimize:   true,
		DisableMaximize:   true,
		DisableFullScreen: true,
		// ⚠️ **这里故意不给 `DisableResize`，而 Biu 那边是 `resizable: false`
		// ——照抄它会让「拖到哪就记到哪」整个失效。** 机制见函数上面那一段。
		//
		// 「不能缩」改用 **MinWidth/MinHeight** 保证：用户永远缩不到比 320×100
		// 更小，那三行控件也就永远不会被裁掉。能往大拖——那没有后果，右边那一列
		// 是 `Grow(1)`，宽出来的部分全给歌名。
		MinWidth:  miniWidth,
		MinHeight: miniHeight,
		// 停在用户上次关掉它的地方（和主窗口那个 `StateKey: "main"` 同一套机制）。
		// **靠上面那个 `MinWidth` 才成立**，理由见函数上面那一段。
		StateKey: "mini-player",
		Content:  ui.View(view),
	}
}

// miniCorner 是迷你窗口**第一次**出现时停在屏幕的哪个角。
//
// ⚠️ 它只管第一次：窗口建过之后 `StateKey` 记住了位置，用户拖到哪儿就停到
// 哪儿（`mygo/window.go` 的 WindowState 那一段会盖掉 X/Y）。
//
// ⚠️ **这一句依赖 `MiniWindowOptions` 里那个「不给 `DisableResize`」**——给了它
// `StateKey` 一条都存不下来（`isZoomed` 恒为真，见那个函数的实测表），于是每次都
// 回到这个角落，而这一段还在说「记住了」。改那一个开关之前先看那段。
//
// 右上角 + 12 的边距：那一块是「工作区里最不挡事」的位置——左上角被菜单栏占着、
// 底部被程序坞占着。Biu 那边是居中（`center: true`）**而且不记位置**（一个普通
// BrowserWindow 没有位置持久化），我们两条都要：第一次贴边，之后跟着用户走。
//
// work 是主显示器的工作区（`mygo.Screen.PrimaryDisplay().WorkArea`）。传零值就
// 退回 (0,0)，让框架去居中——单元测试里没有真显示器，走的就是这一支。
func miniCorner(work mygo.Rectangle) (x, y int) {
	const margin = 12
	if work.Width <= 0 || work.Height <= 0 {
		return 0, 0
	}
	return work.X + work.Width - miniWidth - margin, work.Y + margin
}

// NewMini 建迷你播放器那个外壳。**状态和主窗口共用同一个 App。**
//
// 和 `New` 的差别有三处，都是「这个窗口画的东西不一样 / 不是自己的东西」带来的：
// `draw` 换成迷你那一帧、`toggleMini` 接上主窗口那个开关、以及**快捷键和搜索历史
// 接成主窗口那一份**。
//
// ⚠️ **main 是必填的，不是可选的。** 快捷键和搜索历史是「整进程一份」的东西：同一
// 份设置、同一个后台写盘循环。而 `New` 建出来的是 `newShortcutKeys("")`——**出厂
// 默认值**，读不到盘（`shortcuts.go` 里 `path == ""` 就直接用 `defaultShortcutSettings`），
// 而 `mountShortcuts` 只在主窗口的 `Run` 里调过一次。
//
// 于是曾经有一个真缺陷：迷你窗口每帧登记的是**出厂默认**的那几条，用户在设置页
// 配的和删掉的完全不生效——他删掉的默认键在小窗里照响，他真配的键小窗不认。
// 真机量过：把音量加减改成 ⌘⇧↑/⌘⇧↓ 之后，主窗口认 ⌘⇧↓、小窗认 ⌘↓。
//
// 所以这里接的是主窗口**那一个实例**（不是再读一遍盘）：设置页改了它立刻就见效，
// 而 `closePerWindow` 也不会因为小窗关掉而停掉主窗口那份的写盘（见 `shared`）。
func NewMini(a *app.App, main *Shell, toggle func()) *Shell {
	// 封面缓存先建成**只在内存里**的那一份：下面 `adoptShared` 会把它换成主
	// 窗口那一份。按真目录建的话，每进一次迷你白扫一遍磁盘缓存（见 newShellWithImages）。
	s := newShellWithImages(a, "")
	// 迷你播放器是面板语义：它浮在桌面上时用户看得到，但点它不激活应用，
	// 所以「有没有焦点」不能拿来判它看不看得见（见 presence.go 的
	// freeWhileBlurred）。
	s.presence.update(func(p *windowPresence) { p.freeWhileBlurred = true })
	if main != nil {
		s.adoptShared(main)
	}
	s.draw = s.guardAt("迷你播放器", s.viewMini)
	s.toggleMini = toggle
	return s
}

// adoptShared 把「整进程只有一份」的那几样（封面缓存、快捷键、搜索历史）接到
// 另一个外壳上，并把自己开的那几份收掉。
//
// ⚠️ **`New` 刚建出来的那几份要真的收掉**：`newShortcutKeys` 和
// `newSearchHistory` 各自起了一个后台写盘 goroutine，封面缓存有一个磁盘 writer
// 和一组解码 worker，不收就全都一直跑到进程退出（而这一份从此没人用）。
func (s *Shell) adoptShared(main *Shell) {
	// 封面缓存接主窗口那一份：落盘的磁盘缓存**一个目录只能有一个记账人**。
	// 两个实例各写各的，字节上限就变成了「各自以为的上限」，而且各自启动时
	// 扫一遍目录、各自按自己的账删文件——一个实例刚写进去的图可能被另一个
	// 按它过时的账挤掉。共用一份顺带让两扇窗不再重复下载同一张图。
	if main.images != nil && main.images != s.images {
		// 收掉 `New` 刚建的那一份（它的磁盘 writer 和解码 worker 都在跑）。
		if s.imagesWakeRemove != nil {
			s.imagesWakeRemove()
			s.imagesWakeRemove = nil
		}
		s.images.Close()
		s.images = main.images
		// ⚠️ **借之前必须 Retain**：封面缓存是引用计数的，借的那一份活得至少
		// 和借它的人一样久。不 Retain 的话，主窗口那个外壳先收尾
		// （`Shell.Close` → 放开引用）就会把这份一起关掉——迷你之后每要一张图
		// 都是 `ErrClosed` 永久失败态，界面上只是「图不出来」。
		main.images.Retain()
		s.imagesWakeRemove = s.images.AddWake(s.wake.invalidate)
		s.shared = true
	}
	if main.keys != nil && main.keys != s.keys {
		// 收掉 `New` 刚建的那一份（不报错：这一份从没被用过，也就没有要落盘的东西）。
		_ = s.keys.Close()
		s.keys = main.keys
		s.shared = true
	}
	if main.hist != nil && main.hist != s.hist {
		_ = s.hist.Close()
		s.hist = main.hist
		s.shared = true
	}
}

// openMiniPlayer 开出迷你播放器那个窗口，并把主窗口收起来。
//
// 收主窗口不是可有可无的：Biu 就是这么干的（`electron/ipc/window.ts:43`）。两个
// 都开着的话用户看到的是「同一个应用两个窗口」，而单实例的意义（见 `openWindow`
// 的注释）就是「从程序坞再点一下图标只弹一个窗口」。
func (s *Shell) openMiniPlayer() {
	if s.mini == nil {
		s.newMiniShell()
		s.ensureMiniWindow()
	}
	if w := s.win; w != nil && !w.IsDestroyed() {
		platform.HideToBackground(w)
	}
}

// newMiniShell 建出迷你那个外壳，并和主窗口接上（`s.mini` 指过去、反向指回来）。
// **不建窗口**——建窗口要事件循环，那一半在 `ensureMiniWindow`。
//
// ⚠️ **已经有一个就原样还回去，绝不再建一个**：连按两下「迷你」会开出两个迷你
// 窗口，而两个迷你窗口各放一首歌是比「两个主窗口」更难解释的一种错。
//
// 拆出来是为了能测：「两扇窗共享同一份状态」是这一票最要紧的一条（铁律三），
// 而它必须能**不靠真窗口**摆出来——`mygo.NewWindow` 在应用没 ready 时直接 panic。
func (s *Shell) newMiniShell() *Shell {
	if s.mini != nil {
		return s.mini
	}
	mini := NewMini(s.app, s, s.toggleMiniPlayer)
	// 迷你窗口关掉之后主窗口得知道它没了，否则下一次按「迷你」什么也不发生。
	mini.main = s
	s.mini = mini
	mini.bindMiniWindow()
	return mini
}

// closeMiniPlayer 关掉迷你播放器，把主窗口叫回来。
func (s *Shell) closeMiniPlayer() {
	if s.dropMini() {
		s.restoreMainWindow()
	}
}

// dropMini 关掉迷你播放器那个窗口（真的 Close），返回「刚才确实开着」。
//
// 「把主窗口叫回来」那一步没有算在这里：它要走系统（`RestoreFromBackground`，
// 或者压根没有窗口时的 `openWindow`），而单元测试里没有事件循环，调不了也量不到。
// 剩下的一半是纯逻辑——关窗、主窗口那一侧知不知道它没了——所以留在这一半里测。
func (s *Shell) dropMini() bool {
	mini := s.mini
	if mini == nil {
		return false
	}
	s.mini = nil
	mini.main = nil
	if w := mini.win; w != nil && !w.IsDestroyed() {
		w.Close()
	}
	return true
}

// restoreMainWindow 把主窗口叫回来（迷你期间它是收起来的）。
func (s *Shell) restoreMainWindow() {
	if s.win != nil && !s.win.IsDestroyed() {
		platform.RestoreFromBackground(s.win)
		return
	}
	// 迷你开着的时候主窗口也可能被用户从程序坞叫走又关掉了，那就没有窗口可叫。
	s.openWindow()
}

// ensureMiniWindow 真的把迷你窗口开出来。**只能在应用 ready 之后调**
// （`openMiniPlayer` 排的那条 `WhenReady` 就是为这个，见 `ensureWindow` 的注释：
// `mygo.NewWindow` 在还没 ready 时是直接 panic 的，`mygo/window.go:374`）。
func (s *Shell) ensureMiniWindow() {
	mygo.App.WhenReady(func() {
		// 排队的这段时间里它可能已经被人关掉了（快捷键连按两下）。
		mini := s.mini
		if mini == nil {
			return
		}
		opts := MiniWindowOptions(mini.View)
		// `mygo.Screen` 头上有 `needsApp`，所以位置只能在 ready 之后取。
		opts.X, opts.Y = miniCorner(mygo.Screen.PrimaryDisplay().WorkArea)
		mini.attachMiniWindow(mygo.NewWindow(opts))
	})
}

// bindMiniWindow 是「这个外壳现在代表一扇画着迷你播放器的窗口」那一半：
// 登记自己那条唤醒句柄（铁律三）。
//
// 和窗口本身拆开，是因为**这一半才是两扇窗口共一份状态的那根线**，而它测得到
// （`miniplayer_test.go` 里两个 Tester 走的就是它）；另一半要事件循环。
//
// ⚠️ **重复调是空操作**：生产里它被调两次——建外壳时一次（`newMiniShell`，那时
// 窗口还没建，而唤醒句柄要早点接上），窗口真建出来时再一次（`attachMiniWindow`）。
// 不判一下的话 `App` 那个列表里就有两条同一个外壳的，而摘的时候只摘掉后一条，
// 前一条留到进程退出——每改一次状态就多走一趟空转。
func (s *Shell) bindMiniWindow() {
	if s.wakeRemove != nil {
		return
	}
	s.wakeRemove = s.app.AddWake(s.wake.invalidate)
}

// attachMiniWindow 把迷你窗口接到它的外壳上。
//
// 🔴 **这里一个字都不复用 `Shell.Attach` 那套**，而且是有原因的：主窗口的
// `OnClosed` 是「整个应用收尾」——停播放、关设置、存搜索历史和快捷键（见
// `Attach`）。迷你窗口套上那一套的话，**关掉迷你窗口会把正在放的歌停掉**，
// 而主窗口明明还开着。症状是「从迷你切回来，音乐没了」，和真因隔得很远。
//
// 迷你窗口的 `OnClosed` 因此只做它自己的三件事：摘掉那条唤醒、收掉它自己那几摊
// **逐窗口**的资源（`New` 给每个外壳都建了一份，见 `releaseMini`）、以及告诉主窗口。
func (s *Shell) attachMiniWindow(w *mygo.Window) {
	s.win = w
	s.bindMiniWindow()
	// 这扇窗的可见性同主窗口一样由系统推过来，但「有没有焦点」不参与判断：
	// 它是面板语义，点它不激活应用（票 #156 第六条），于是它几乎永远失焦
	// ——拿焦点判的话，用户点一下播放键进度条就不再动了（见 presence.go）。
	s.watchWindow(w)

	w.OnClosed(func() {
		if s.win == w {
			s.win = nil
		}
		// 唤醒句柄的摘除在 `closePerWindow` 里（`releaseMini` 走的就是它）。
		s.releaseMini()
		// ⚠️ **主窗口要叫回来**。它是「进迷你」那一步收起来的（`openMiniPlayer`），
		// 而这一条除了点「退出」之外还有别的路会走到：macOS 上的 ⌘W、Windows 上
		// 的 Alt+F4。用户从那些路把迷你窗口关掉之后，主窗口还收着——应用还活着、
		// 音乐还在放，而屏幕上什么都没有了（连 Dock 上的窗口列表都是空的）。
		//
		// 退出流程里**不叫**：那时候正在挨个关窗口，再把主窗口 Show 出来是反着的
		// （`Shell.quitting`，见 `Attach` 里同一句的注释）。
		if s.main != nil && s.main.mini == s {
			s.main.mini = nil
			if !s.main.quitting {
				s.main.restoreMainWindow()
			}
		}
	})
}

// releaseMini 收掉迷你那个外壳自己那几摊**逐窗口**的资源。
//
// ⚠️ 它**不能**调 `Close`：那是整个应用的收尾（会 `s.app.Close()` 把正在放的歌停
// 掉、关设置），而迷你窗口只是另一扇窗——关掉它而主窗口还开着，音乐不该停。
// 它走的是 `closePerWindow`（主窗口那一侧的 `Close` 收完它之后才收应用层那几样）。
//
// 为什么需要收：`New` 给**每个**外壳都建了一份封面缓存（它起解码 goroutine）、内存里
// 的搜索历史和快捷键（各有一个后台写盘的 goroutine）、红点那个定时器。迷你窗口压根
// 不画那些页面，而它们要是不收，「进迷你 / 退出迷你」来回十次之后就是十份挂着的
// goroutine 到进程退出为止。
func (s *Shell) releaseMini() {
	s.closePerWindow()
}

// viewMini 是迷你播放器那一帧。
//
// 结构照 Biu 的 `pages/mini-player/index.tsx:119-217`：封面在左，右边竖着摆
// 三行——标题、进度、控制。多的两样是 Biu 没有而这一票要求的：**音量**和**回到
// 主窗口**。音量是用户定的（上游迷你播放器没有，我们保留）；回到主窗口走的是
// 上游那个 ⤢（`iconExpand`）——图标集里本来就有它，画出来和自己画一个字形一样
// 准。
//
// 🔴 铁律二：第一件事装主题，而且必须在最开头（`SetTheme` 只影响它之后创建的
// 控件，而 MyGo 每帧开头把主题重置成跟随系统那份，见 theme.go）。
func (s *Shell) viewMini(c *ui.Context) {
	s.applyFrameTheme(c)
	// 应用内快捷键：**和主窗口走同一条**（`viewShortcuts`），次序也一样（主题之后、
	// 其它每帧钩子之前）。
	//
	// ⚠️ 不登记的后果不是「少几个键」，而是**设置页上那句话变成假的**：那一页写着
	// 「应用内那一列：窗口在前台时按它就有用」，而迷你窗口是用户缩小之后最常停在
	// 前面的那一扇。少了这一句，⌘P / ⌘← / ⌘→ / ⌘↑ / ⌘↓ 在小窗在前台时一条都不响，
	// 而界面上一个字都不提。
	s.viewShortcuts(c)

	st := s.app.Snapshot()
	t := c.Theme()

	// ⚠️ **底下这一层背景不是装饰**：窗口是透明的（`MiniWindowOptions`），框架
	// 把帧清成透明，所以这一块不透明底是我们**唯一**的底。不画的话这个窗口在
	// 桌面上是隐形的。
	//
	// 它同时是**拖动区**：这一层上挂了 `DragWindow`，指针落在控件之外的任何
	// 地方都是在拖窗口（Biu 那边的 `window-drag` / `window-no-drag` 两种 class，
	// 在 MyGo 里就是这一条命中链的差别，见 `navbar.go` 文件头那一段）。
	// 底用 `Surface` 而不是 `Background`：这个窗口要的就是「一张卡片浮在桌面上」，
	// 而主窗口里播放条那一块**现在取的是 `Background`**（票 #127 之后它和内容区
	// 同源）——所以这两块**不再同一族**了，而且它们本来就不该是：主窗口的播放条
	// 是窗口骨架的一部分，而这个迷你窗整个就是一张卡片。
	// 圆角跟着主题走，用户在设置页调圆角时这一块跟着变，和别处一致。
	ui.Row(c).Key("mini").Fill().Radius(t.Radius).
		Background(t.Surface).DragWindow().Children(func() {
		// 里面照样逐块包一层 Guard（铁律四）：崩掉封面那一块不该把控制键一起带走。
		s.guardAt("迷你播放器封面", func(c *ui.Context) { s.viewMiniCover(c, st) })(c)
		s.guardAt("迷你播放器控制键", func(c *ui.Context) { s.viewMiniControls(c, st, t) })(c)
	})
}

// viewMiniCover 是左边那一列封面。
//
// Biu 那边那一列是 `h-full w-[100px]`，封面**顶到窗口上下边**（`pages/mini-player/
// index.tsx:28`）。这里是同样的形状——列占满整窗高——但封面本身是 52×52 的方图，
// 在列里垂直居中：320 宽放不下 100 宽的封面而右边还塞得下歌名、音量、三个图标键
// （账在 `miniCoverCol`）。
//
// ⚠️ 有封面才画。没歌时 Biu 那边是 `if (!cover) return null`——右边那一列铺满
// 整窗。一个空框在那儿比没有更让人以为图加载坏了。
func (s *Shell) viewMiniCover(c *ui.Context, st app.State) {
	if st.CoverURL == "" {
		return
	}
	ui.Row(c).Key("mini-cover-col").Width(miniCoverCol).Height(miniHeight).
		Padding(miniPad, 0, miniPad, miniPad).AlignItems(ui.Center).Children(func() {
		imgcache.View(c, s.images, st.CoverURL, miniCover, miniCover)
	})
}

// viewMiniControls 是封面右边那一列：标题 + 音量 + 退出、进度、控制键。
//
// ⚠️ **每一行、每一个键都给了 `Key`**（铁律四）。默认的元素身份是「父 ID +
// 兄弟序号」，而这一列的形状**会变**：播放和暂停是两个不同的按钮（`st.Playing`
// 分支），歌从没有变成有的时候整列换一遍，Biu 那边那个 ⤢ 我们换成了一个固定
// 位置的「退出」。没有 key 的话这些换位会让后面每个控件的状态整体错配一格
// ——而**界面上看不出坏了**：按钮都还在那儿，只是按「上一首」按到了「下一首」。
func (s *Shell) viewMiniControls(c *ui.Context, st app.State, t *ui.Theme) {
	// ⚠️ **垂直居中**而不是顶对齐：三行的内容一共 78 高，而这一列有 88（100 减去
	// 上下各 6）。顶对齐的话底下空 16、上面空 6，整块看上去是往下坠的——而它贴在
	// 屏幕边上，那 16 像素的空白会显得像没画完。
	ui.Column(c).Key("mini-controls").Grow(1).
		Padding(miniPad, miniPad, miniPad, miniPad).Justify(ui.Center).
		Gap(4).Children(func() {
		s.viewMiniTitleRow(c, st, t)
		s.viewMiniProgress(c, st)
		s.viewMiniTransport(c, st)
	})
}

// viewMiniTitleRow 是第一行：歌名、音量、回到主窗口。
func (s *Shell) viewMiniTitleRow(c *ui.Context, st app.State, t *ui.Theme) {
	ui.Row(c).Key("mini-title-row").FillWidth().Gap(6).AlignItems(ui.Center).
		Children(func() {
			title := st.Title
			if !st.HasTrack {
				// 抄 Biu 的 `暂无播放内容`（`pages/mini-player/index.tsx:128`）。
				title = nothingPlaying.Title
			}
			ui.Text(c, title).Key("mini-title").Grow(1).MinWidth(miniTitleMinW).
				FontSize(scale.Font(c, 13)).Bold().MaxLines(1).Ellipsis("…").
				TextColor(textColor(t, st.HasTrack))

			// 音量在这一行而不是控制那一行：Biu 那边没有音量，而控制那一行只摆
			// 三个键（上一首 / 播放 / 下一首），一行里再塞一根滑块会让那三个键
			// 和它挤成一团（账在 `viewMiniTransport`）。
			// ⚠️ **key 装在 `c` 上、不是装在 `Slider` 返回的那个元素上**：滑块建出来
			// 的那一刻它的状态就初始化了，而 `Element.Key` 只认「还没初始化状态」的
			// 元素（`mygo/ui/element.go:611` 直接 panic）。`Context.Key` 是为这类复合
			// 控件准备的：它在控件被建出来**之前**把身份交给下一个。
			vol := st.Volume
			if ui.Slider(c.Key("mini-volume"), &vol, 0, 1).Width(miniVolumeW).
				Height(16).Label("音量").Changed() {
				s.app.SetVolume(vol)
			}

			// 回到主窗口。上游是同一样东西的「展开」图标
			// （`RiExpandDiagonalLine`，`pages/mini-player/index.tsx:203-212`）——
			// 迷你 ↔ 主窗口是同一个开关的两头，图标说的是那件事。
			if s.withTip(c, flatIconButton(c, iconExpand, "退出", 18).
				Key("mini-back").Size(miniBackBtnW, miniBackBtnW).Padding(0, 5),
				"关掉迷你播放器，回到主窗口").Clicked() {
				if s.toggleMini != nil {
					s.toggleMini()
				}
			}
		})
}

// viewMiniProgress 是第二行：那条进度条。
//
// 拖动的分工和播放条一样（`playbar.go` 的 `viewProgress`）：按住的时候值归
// 用户，松手才 seek。不这么分的话后台那个每 50ms 写回一次位置的时钟会和拖动
// 打架，症状是拖不动，或者松手又弹回去。
//
// ⚠️ 按住看的是 `Pressed()`，不是 `Dragging()`：后者报的是 Drag / Draggable
// 两套机制，普通 Slider 对它永远是 false（实测：按下滑块移动 200 DIP，
// Dragging 仍为 false，Pressed 为 true）。理由见 `viewProgress`。
func (s *Shell) viewMiniProgress(c *ui.Context, st app.State) {
	known := st.Duration > 0

	// key 装在 `c` 上：滑块的状态是建出来那一刻就初始化的（见上面那一条）。
	sl := ui.Slider(c.Key("mini-progress"), &s.miniScrub, 0, 1).Grow(1).
		Height(16).Label("播放进度").Disabled(!known)
	switch held := sl.Pressed(); {
	case held && !s.miniHeld:
		s.miniHeld = true
		s.app.ScrubStart()
	case !held && s.miniHeld:
		s.miniHeld = false
		s.app.ScrubEnd(s.miniScrub)
	case !s.miniHeld && known:
		s.miniScrub = float64(st.Position) / float64(st.Duration)
	}
}

// viewMiniTransport 是第三行：上一首、播放/暂停、下一首。
//
// ⚠️ **这一行是 320 宽那一笔账的全部**：三个键 28 + 32 + 28 = 88，两道 6 的
// 缝 12，一共 100，而右边那一列的宽是 320 - 64（封面列）- 12（列的内边距）
// = 244。余量 144。
//
// ⚠️ **这一行以前是一笔紧账**：三个汉字键 64 + 72 + 64 = 200，余量只有 32，
// 再窄一点就有一个键被画成「上一…」——而**无头 Tester 读的是控件的文本**，被截
// 的只是画出来的那几个字，所有断言照样全绿。图标化之后那笔账整笔勾销（图形不
// 会再被裁成偏旁），`TestTheMiniKeysAreIcons` 断的是「这三个键的框就是图标键的
// 框，不是被挤扁的文字键」。
//
// ⚠️ **余量出来了就得居中**（`Justify(ui.Center)`）：左对齐的话这三个键挤在
// 244 宽的左边 100 像素里、右边空 144，**播放键落在窗口左侧**，而它上面那条进度
// 条横跨整列——两颗「中心」对不上，一眼就是「没排过版」。上游那一行是
// `justify-between`，因为它那三组（模式 / 控制 / 展开）各占一头；我们这一行只有
// 中间那一组，居中就是同一个意思：播放键在进度条中点底下。
func (s *Shell) viewMiniTransport(c *ui.Context, st app.State) {
	ui.Row(c).Key("mini-transport").FillWidth().Gap(6).AlignItems(ui.Center).
		Justify(ui.Center).
		Children(func() {
			// 能不能按看的是队列，和播放条上那两个键是同一个判据（`viewStepButtons`）。
			if s.withTip(c, flatIconButton(c, iconPrev, "上一首", 18).
				Key("mini-prev").Size(miniStepW, miniStepW).Padding(0, 5).
				Disabled(!s.app.CanPrev()), "上一首").Clicked() {
				s.app.Prev()
			}
			s.viewMiniPlay(c, st)
			if s.withTip(c, flatIconButton(c, iconNext, "下一首", 18).
				Key("mini-next").Size(miniStepW, miniStepW).Padding(0, 5).
				Disabled(!s.app.CanNext()), "下一首").Clicked() {
				s.app.Next()
			}
		})
}

// viewMiniPlay 是播放 / 暂停。
//
// ⚠️ **key 按角色给（`mini-play`），不按按钮上的字**。字在「播放」和「暂停」之间
// 换来换去，而 key 若跟着字变，这个按钮每换一次字就换一个新身份，它的按下/悬停
// 状态跟着重来一次——**界面上看不出坏了**，只是按下去偶尔要按两下。
//
// 图标也照抄 Biu：同一个键换图标（`RiPauseCircleFill` / `RiPlayCircleFill`，
// `pages/mini-player/index.tsx:187`），画哪一张仍由 `transportIcon` 说了算。
//
// ⚠️ **它不再是主色按钮**：上游在迷你播放器里也是 `variant="light"`（图标本身
// 是实心圆，重量已经有了）。主色只在顶部那一档出现，小窗上再摆一块绿底就是两个
// 主角。
func (s *Shell) viewMiniPlay(c *ui.Context, st app.State) {
	const style = miniPlayW

	if st.Playing {
		if s.withTip(c, flatIconButton(c, transportIcon(true), transportLabel(true), 28).
			Key("mini-play").Size(style, style).Padding(0, 2), "暂停").Clicked() {
			s.app.Toggle()
		}
		return
	}
	// 没歌时「播放」是灰的：Biu 那边是 `isDisabled={!title}`
	// （`pages/mini-player/index.tsx:139`）。一个亮着而按了没反应的键比一个灰的
	// 键更容易让人以为坏了。
	if s.withTip(c, flatIconButton(c, transportIcon(false), transportLabel(false), 28).
		Key("mini-play").Size(style, style).Padding(0, 2).
		Disabled(!st.HasTrack), "播放").Clicked() {
		s.app.Toggle()
	}
}
