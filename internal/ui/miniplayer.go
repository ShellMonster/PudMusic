// 迷你播放器窗口（SPEC 需求 43~46）：380 宽的透明置顶小窗，贴边悬浮（原型 `.mini`，
// DESIGN.md §0′.T5）。
//
// # 和 `/mini-player` 那一条路由是两件事
//
// `nowplaying.go` 的 `viewNowPlaying` 是**主窗口里的一页**（歌名 + 歌词），
// 它在路由表里占一条（`router.go` 的第 13 条）。这一票要的是 Biu 的
// `electron/mini-player.ts`：**另一个窗口**——无边框、透明、置顶，
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
//     （`mygo/window.go:442`「Backends keep transparent windows clear」），
//     主题背景**不填**。不自己画一块不透明底的话，这个窗口在桌面上是隐形的
//     ——而「窗口开着却看不见」是最难查的一种错。
//  2. **主题**：铁律二，每一帧第一件事（`applyFrameTheme`）。
//  3. **recover**：view 里 panic 会穿透 Cocoa 事件循环杀掉整个进程，所以
//     `NewMini` 外面包一层 `Guard`。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/platform"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	tclean "github.com/ShellMonster/PudMusic/internal/ui/widgets/title"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// 迷你播放器窗口的尺寸（原型 `.mini`，DESIGN.md §0′.T5）：**380 宽的一张卡片**，
// 收起时 194 高，歌词卷帘在卡片**里面**往下展开 220（见 `minicurtain.go`）。
//
// 194 是原型各段高度加起来的数：上下描边各 1 + 拖动条 44（上内边距 8 + 36 的图标键）
// + 主区 120（内边距上下各 16 + 封面 72 的那一行撑到 88）+ 进度行 16 + 底内边距 12。
const (
	miniWidth  = 380
	miniHeight = 1 + miniDragH + miniTopH + miniSeekH + miniSeekPadB + 1

	// miniRadius 是卡片圆角（原型 `.mini { border-radius: 16px }`）。
	miniRadius = 16

	// miniDragH 是顶部拖动条那一行的高：上内边距 8 + 36 的图标键。
	miniDragH = 8 + 36
	// miniTopH 是封面 + 歌名 + 控制键那一块：上下内边距各 16，内容 88
	// （歌名 24 + 歌手 20 + 控制行上边距 8 + 36 的键）。
	miniTopH     = 16 + miniInfoH + 16
	miniInfoH    = 24 + 20 + 8 + 36
	miniSeekH    = 16
	miniSeekPadB = 12

	// miniCover 是封面边长（`.mini-cover` 72，圆角 8）。
	miniCover = 72
	// miniTimeW 是进度条两端那两格时间的宽（`.mini .seek` 的 40）。
	miniTimeW = 40
	// miniTitleWeight 是歌名的字重（原型 `<strong>` 是 700，系统字体里 600 与 700 同一档）。
	miniTitleWeight = 600
	// miniPlayBtn 是播放键（`.mini-ctl .play-btn` 36 的实心圆，图标 16）。
	miniPlayBtn = 36
)

// MiniWindowOptions 是迷你播放器窗口的样子。view 是这个窗口每帧要画的界面。
//
// ⚠️ **`Frameless: true` 在这里是想要的**，和主窗口正相反：主窗口要的是
// 「红黄绿留着」（`window.go` 里 `hiddenTitleBar = !Frameless && …` 的那道
// 判断），而 Biu 的迷你窗口 `frame: false`，压根没有系统控件——它自己画一整块。
// 这里走真无边框。
//
// # Panel：点它不激活应用（票 #162）
//
// `Panel: true` 让这扇窗在 macOS 上是带 nonactivating 样式的 `NSPanel`
// （框架的 `MyGoPanel`）：显示和聚焦时能拿键盘，但**应用保持不活动**——用户
// 在别的应用里按迷你播放器的播放键，不会被拽出那个应用。它是普通窗口时的
// 症状正好相反：点一下播放键，前台应用整个切走。
//
// 只开 Panel **不**把应用变成 accessory：程序坞图标、菜单栏、主窗口全都照旧
// （票面明说「盖在别人的全屏应用上」不做，那要 accessory，是产品决定）。
//
// ⚠️ **Panel 和 `StateKey` 必须一起验。** 面板在语义上不能最大化，而「不能
// 缩放 / 不能最大化」正是下面那一段实测出来的、让 `StateKey` 一条都存不下来
// 的那条路（`isZoomed` 恒为真）。这一步在真窗口上量过：见函数末尾「本机实测」
// 那一段。
//
// # ⚠️ 为什么**没有** `DisableResize`（Biu 那边有）
//
// 这不是抄漏了，是照抄会让 `StateKey` 整个失效。实测出来的链路，三步：
//
//  1. `DisableResize` 在 macOS 上就是从 `styleMask` 里去掉 `NSWindowStyleMask-
//     Resizable`（`mygo/internal/darwin/window.go:123`）。
//
//  2. 一个没有 resizable 位的窗口**没有「标准框」可言**，`isZoomed` 恒为真
//     （`mygo/internal/darwin/window.go:830` 那句 `sendBool(w.win, "isZoomed")`）。
//
//  3. `StateKey` 存不存位置，判的是 `!Maximized`
//     （`mygo/window_state.go:195` 的 `captureState`）：
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
//	min==max（钉死大小）             IsMaximized=true    ← 也钉不住
//
// ⚠️ 最后一行的结论**与具体数值无关**（那一轮量的是当时那扇窗的尺寸，票 #357
// 之后窗是 320×84）：钉死大小就是「不可缩放」，而不可缩放的窗口 `isZoomed`
// 恒为真——机制一样，结论一样。
//
// 另外两条试过也没用：`w.Unmaximize()` 对不可缩放的窗口是空操作；建完再
// `SetResizable(true) → Unmaximize() → SetResizable(false)`，改回去的那一下
// `isZoomed` 又变回真。
//
// 代价：用户能把这个窗口**往大拖**。而那没有后果——右边那一列是 `Grow(1)`，
// 宽出来的部分全给歌名；**缩不到更小**由 `MinWidth/MinHeight` 兜住，三行控件
// 不会被裁掉。
//
// # 本机实测（票 #162，macOS，真窗口）
//
// Panel 打开之后，用**生产这份选项**（`MiniWindowOptions`）建真窗口量过：
//
//	styleMask   0x808b，含 `NSWindowStyleMaskNonactivatingPanel`（1<<7）
//	            ——「点它不激活应用」就是这一位（0x1 titled / 0x2 closable /
//	            0x8 resizable / 0x80 nonactivatingPanel / 0x800 fullSizeContentView）
//	isZoomed    false，建出来和显示之后各量一次——上面那个坑没有因为 Panel 重现
//	位置记忆    拖到 (260,200) → 关窗 → `window-state.json` 里有 mini-player 键
//	            → **新进程**再建，`Bounds()` 就是 (260,200)
//
// 「点它不激活应用」的最后一跳只有人能验：自动化只证明了样式位在，没有真的用
// 鼠标去点（点一下会改动用户当前的前台应用）。收尾说明里把这两种证据分开写了。
func MiniWindowOptions(view func(*ui.Context)) mygo.WindowOptions {
	return mygo.WindowOptions{
		Title:  "布丁音乐迷你播放器",
		Width:  miniWidth,
		Height: miniHeight,
		// 🔴 `Transparent` 必须配着自己画的背景，理由见文件头那一条。
		// 单开这一个开关而忘了画底，窗口在桌面上是隐形的。
		Transparent: true,
		// 🔴 **毛玻璃**（`popover` 那一档）：窗后面的东西（桌面壁纸、别的
		// 应用的窗）糊完垫在页面底下——两张板（卡片 / 歌词板）的底是
		// **半透明**的（`miniPanelAlpha`），磨砂透出来。
		//
		// ⚠️ **整窗一个材质**：卡片区也一起透——那是设计要的样子（目标图
		// 那张卡片本身就是半透的）；macOS 26+ 走 `NSGlassEffectView` 的
		// `popover` 档，更早的系统走 `NSVisualEffectView`（`mygo/internal/
		// darwin/window.go` 的 `vibrancyMaterials`）。
		Vibrancy: mygo.VibrancyPopover,
		// Biu：`alwaysOnTop: true, skipTaskbar: true`。
		// 「置顶」是它作为悬浮控件的本体；「不进任务栏 / 程序坞」是为了不跟
		// 主窗口抢同一个格子。面板语义已经隐含这两条（`mygo/window.go` 的
		// `platformOptions` 里 `if o.Panel { … AlwaysOnTop, SkipTaskbar = true, true }`），
		// 这里照写是把意图留在选项上，两处一致。
		AlwaysOnTop: true,
		SkipTaskbar: true,
		// 票 #162：面板语义——显示 / 聚焦给键盘，但不激活应用。理由和
		// 「位置记忆」那一半的风险见函数头上那一段。
		Panel:     true,
		Frameless: true,
		// Panel 也会把这三条强制成 false（同一个 `platformOptions`）。照写不冲突，
		// 而且「迷你窗不要最大化 / 最小化 / 全屏」这件事不该只靠 Panel 的语义转述。
		DisableMinimize:   true,
		DisableMaximize:   true,
		DisableFullScreen: true,
		// ⚠️ **这里故意不给 `DisableResize`，而 Biu 那边是 `resizable: false`
		// ——照抄它会让「拖到哪就记到哪」整个失效。** 机制见函数上面那一段。
		//
		// 「不能缩」改用 **MinWidth/MinHeight** 保证：用户永远缩不到比 320×84
		// 更小，那三行控件也就永远不会被裁掉。能往大拖——那没有后果，右边那一列
		// 是 `Grow(1)`，宽出来的部分全给歌名。
		MinWidth:  miniWidth,
		MinHeight: miniHeight,
		// 停在用户上次关掉它的地方（和主窗口那个 `StateKey: "main"` 同一套机制）。
		// **靠上面那个「不给 DisableResize」才成立**，理由和实测表见函数上面
		// 那两段。
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
// ⚠️ **work 是哪一块屏的工作区**：现在它跟着**指针所在那块屏**走
// （`miniPlanFirst`，票 #555）——而**主显示器**只是「问不到系统」时的兜底。
// 传零值就退回 (0,0)，让框架去居中——单元测试里没有真显示器，走的就是这一支。
func miniCorner(work mygo.Rectangle) (x, y int) {
	if work.Width <= 0 || work.Height <= 0 {
		return 0, 0
	}
	r := miniCornerRect(work, mygo.Rectangle{Width: miniWidth, Height: miniHeight})
	return r.X, r.Y
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
	// 迷你播放器是面板语义（`MiniWindowOptions` 的 `Panel: true`，票 #162）：
	// 它浮在桌面上时用户看得到，但点它不激活应用，所以「有没有焦点」不能拿来
	// 判它看不看得见（见 presence.go 的 freeWhileBlurred）。
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
	// 「系统交给我们的那个文件」那一句接主窗口那一份。
	//
	// ⚠️ **必须是同一份**：那句话是**一件事**（系统交给我们一个文件，我们
	// 收下了没有），而**两扇窗都要画它**——迷你那一扇不画 toast（`viewToasts`
	// 只在主窗口那一支调），而迷你常常就是用户眼前那一扇。
	//
	// 两份的话，主窗口那一份上的话迷你看不见，而迷你那一份上的话主窗口
	// 看不见——于是「用户眼前那一扇没有那句话」照旧发生，而症状一模一样。
	if main.notice != nil && main.notice != s.notice {
		s.notice = main.notice
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
	// 🔴 主窗口只是被藏起来，`closePerWindow` 不会跑（回来还要用它），所以
	// 「藏起来就看不见的东西」得在这儿自己收——热词轮换的表不收的话，它每
	// 5 秒叫醒一次一扇看不见的窗（`wakeHandle` 会把帧丢掉，代价只是一次空醒，
	// 但表该收）。回到主窗口重画首帧时照常装回（见 navbar_hotword.go）。
	s.stopHotword()
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
// `mygo.NewWindow` 在还没 ready 时是直接 panic 的，`mygo/window.go:382`）。
func (s *Shell) ensureMiniWindow() {
	// 🔴 **排队时就把目标外壳抓住**（评审抓到的那个）：`WhenReady` 是**下一拍**
	// 才跑的，而同一拍里「关 → 再开」会把 `s.mini` 换成**另一个**外壳——闭包里
	// 那时再读 `s.mini`，读到的就是新外壳，于是给**新外壳**挂一扇窗，而
	// `dropMini` 关的却是**旧**那一扇。结果：两扇真窗都活着、新外壳的 `miniWin`
	// 被第二个 `attach` 又挂了一遍，**第一扇变成收不回来的孤儿**。
	//
	// ⇒ 抓住**这一次想挂给谁**，闭包里比对——`s.mini` 换了人（或者那一扇
	// 已经有窗了）就整个放弃，别把第二扇窗挂上去。
	mini := s.mini
	mygo.App.WhenReady(func() {
		// 排队的这段时间里它可能已经被人关掉了（快捷键连按两下），或者换成
		// 另一个外壳了（同一拍里「关 → 再开」）。
		if mini == nil || s.mini != mini || mini.miniWin != nil {
			return
		}
		opts := MiniWindowOptions(mini.View)
		// `mygo.Screen` 头上有 `needsApp`，所以位置只能在 ready 之后取。
		//
		// ⚠️ **跟着指针走**（票 #555）：以前这一句取的是**主显示器**的工作
		// 区——于是在第二块屏上点「迷你」的人，窗却开在主屏上，而**他看不见
		// 它**（症状和「点了没反应」一模一样）。
		opts.X, opts.Y = miniFirstPosition()
		mini.attachMiniWindow(mygo.NewWindow(opts))
		// 🔴 **位置恢复之后要再判一次**（票 #555）：`StateKey` 记住的位置会
		// **盖掉**上面那两句（`mygo/window_state.go` 的 `restoreWindowState`），
		// 而那一块屏要是已经被拔了（或者改了分辨率），窗就停在**谁都看不见**
		// 的地方——所以建出来之后立刻按当下的显示器重新判一次。
		mini.miniReconcileDisplayNow()
	})
}

// miniFirstPosition 算迷你窗**第一次**该在哪儿：跟着指针走。
//
// ⚠️ **问不到系统时退回主显示器**（`miniCorner`）：那只是兜底——正常路径上
// 这一句在 `App.WhenReady` 之后跑，而那时候系统是问得到的。
func miniFirstPosition() (x, y int) {
	displays := miniDisplayList()
	cursor, ok := miniCursorPoint()
	if len(displays) == 0 || !ok {
		return miniCorner(mygo.Screen.PrimaryDisplay().WorkArea)
	}
	plan := miniPlanFirst(displays, cursor, mygo.Rectangle{Width: miniWidth, Height: miniHeight})
	if plan.Rect.Width == 0 {
		return miniCorner(mygo.Screen.PrimaryDisplay().WorkArea)
	}
	return plan.Rect.X, plan.Rect.Y
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
	s.attachMiniWindowTo(miniWindowOf{w: w})
}

// attachMiniWindowTo 把迷你窗口接到它的外壳上。
//
// ⚠️ **它接的是 `miniWindow` 那一层，而不是 `*mygo.Window`**：于是无头那一档
// 能用一个**假窗**走**同一条生产路径**——而「关窗之前把窗高改回 84 ⇒
// `StateKey` 落盘的是 320×84」这一条**量得到**（真窗那一档读的是原生那一
// 边的 bounds，见 `minicurtain.go` 里 `miniWindow` 那一段）。
//
// ⚠️ **假窗没有原生那一侧**：那几样（把 `s.win` 指过去、登记唤醒句柄、接可见
// 性回调）**只对真窗成立**，所以它们走 `w.Native()` 那一格——假窗给 nil，那
// 几样就跳过（而无头 Tester 里本来也没有真窗口可登记）。
func (s *Shell) attachMiniWindowTo(w miniWindow) {
	native := w.Native()
	s.win = native
	// ⚠️ **这一层接口也给迷你窗备着**：卷帘要改的是**原生尺寸**，而无头
	// Tester 里没有真窗口——「关窗之前把高度改回 84 ⇒ 落盘的是 320×84」
	// 这一条要靠它才**测得到**（理由见 `minicurtain.go` 里 `miniWindow`
	// 那一段）。
	s.miniWin = w
	s.bindMiniWindow()
	// 这扇窗的可见性同主窗口一样由系统推过来，但「有没有焦点」不参与判断：
	// 它是 Panel，点它不激活应用（票 #162），于是它几乎永远失焦——拿焦点判的
	// 话，用户点一下播放键进度条就不再动了（见 presence.go）。
	if native != nil {
		s.watchWindow(native)
		// ⚠️ **「显示器变了」那一档**（票 #555）：插上 / 拔掉一块屏、或者改
		// 分辨率之后，窗原来那个位置可能已经没人看得见了——这一档把它夹回
		// 还剩下的那块屏的工作区里。
		//
		// ⚠️ **退订句柄要留着**：这个订阅是**全局**的（`mygo.Screen` 上的一个
		// 列表），而迷你窗会开开关关——不摘的话，每开一次就多留一个回调，
		// 而那个回调还**拿着这一扇窗**（窗关了它照样跑）。
		s.miniDisplaysOff = mygo.Screen.OnDisplaysChanged(func() { s.miniOnDisplaysChanged() })
		// ⚠️ **attach 那一刻就记下「应用此刻是前台吗」**：`miniCurtainActivate`
		// 里 `need` 算的是「点封面那一刻应用**不是**前台」，而这个标志只在两个
		// 应用级回调里维护——attach 时它总是 false，于是**应用本来就是前台**的
		// 时候也被错算成「需要激活」，那个 1200ms 的重试窗口于是白开一次
		// （每帧多要一帧、每 120ms 再激活一次已经激活的应用）。
		s.miniCurtainAppActive = mygo.App.IsReady() && mygo.App.IsActive()
	}

	// 🔴 **窗被挪动了 ⇒ 重新判它落在哪块屏上**（票 #555）。
	//
	// ⚠️ **它装在 `miniWindow` 那一层上**，而不是 `*mygo.Window`——于是无头
	// 那一档能手动触发它（`minidisplays_test.go` 里就是这么验「窗不会掉到
	// 屏幕外」的）。
	//
	// ⚠️ **它不跟用户对着干**：只有一块屏都看不见这扇窗的时候才把它拉回来
	// （理由见 `miniOnWindowMoved`）。
	//
	// ⚠️ **它的返回值不存**：`OnMove` 是**逐窗口**的（挂在 `w.listeners`
	// 那一列上），窗 `destroy` 时整列跟着死——不像下面两个 `mygo.App`
	// 全局回调要手动摘，这一条窗没了它自己就没了。
	w.OnMove(func() { s.miniOnWindowMoved() })

	// 🔴 **应用真的成了前台的时候，再抢一次 key 窗口**（票 #536，评审实测
	// 抓到的那个 Esc 路径）。
	//
	// ⚠️ **为什么不能只靠展开那一下的两句**：`miniCurtainActivate` 里
	// `activateIgnoringOtherApps:` 是**异步**的（系统给别的应用一个「别抢
	// 回去」的机会），而 `makeKeyAndOrderFront:` 只在**应用已经是前台**的时候
	// 才把这一扇变成 key 窗口。⇒ **实测四轮里有一轮**点封面之后应用压根没被
	// 激活（`active=false`），那一轮 Esc 就到不了这一扇窗。
	//
	// ⇒ 这一档是那个**兜底**：应用真的成了前台（`DidBecomeActive`）时，
	// **再抢一次**——那时候 `makeKeyAndOrderFront:` **一定**成。
	//
	// ⚠️ **它装在迷你这个外壳上**，不是主窗口那个：卷帘只在迷你窗里，而
	// `Shell.miniCurtainWantsKey` 也是迷你这一份的状态。
	//
	// ⚠️ **退订句柄要留着**：这两个订阅是**全局**的（`mygo.App` 上的回调列表），
	// 而迷你窗会开开关关——不摘的话，每开一次就多留两个回调，而它们各自还
	// **拿着这一个外壳**（窗关了它照样跑，每来一次应用激活就对着一个死外壳
	// 调 `Focus`）。
	s.miniAppActiveOff = mygo.App.OnDidBecomeActive(func() { s.miniCurtainEnsureKey() })

	// 🔴 **应用退出前台时，收手**（票 #307 修 F1）。
	//
	// ⚠️ **为什么**：重试激活那一档（`miniCurtainFocusTick`）在「应用还没成
	// 前台」的时候会**反复调 `activateIgnoringOtherApps:`**——而用户要是
	// 已经**自己切到别的应用**去了，我们还在重试 ⇒ **把用户从他自己那件事
	// 上拽走**。
	//
	// ⚠️ **这一档只做「收手」**：应用**真的**成了前台时，`OnDidBecomeActive`
	// 那一档会把 `miniCurtainAppActive` 置真，重试于是自动停；用户又切走时，
	// 这一档把它置回假——而**重试的窗口本身**（`miniCurtainFocusUntil`）
	// 只有 1200ms，早就过了。
	s.miniAppResignOff = mygo.App.OnDidResignActive(func() { s.miniCurtainAppActive = false })

	// ⚠️ **逐窗口的那些生命周期钩子装在这一层上**（`miniWindow` 那一层接口），
	// 而不是直接装在 `*mygo.Window` 上——理由见 `attachMiniWindowLifecycle`
	// 那一段。
	s.attachMiniWindowLifecycle(s.miniWin)

	if native == nil {
		// ⚠️ **假窗没有 `OnClosed` 那一格**：它要的是 `*mygo.Window` 那一套
		// API，而无头那一档压根没有真窗口——所以「窗关掉之后收尾」那几样在
		// 那一档走不到（它们量的是真窗的生命周期，见 `miniplayer_test.go`
		// 末尾那一节「真窗口才量得到」）。
		return
	}
	native.OnClosed(func() {
		if s.win == native {
			s.win = nil
		}
		if _, ok := s.miniWin.(miniWindowOf); ok {
			s.miniWin = nil
		}
		// ⚠️ **退订「显示器变了」那一档**（票 #555）：理由见上面装它的那一段。
		if s.miniDisplaysOff != nil {
			s.miniDisplaysOff()
			s.miniDisplaysOff = nil
		}
		// ⚠️ **退订那两个应用级回调**（同一个理由）：它们是 `mygo.App` 上的全局
		// 列表，闭包各自**拿着这一个外壳**——不摘的话，窗关了它照样跑，每开一次
		// 迷你就多挂两条死回调。
		if s.miniAppActiveOff != nil {
			s.miniAppActiveOff()
			s.miniAppActiveOff = nil
		}
		if s.miniAppResignOff != nil {
			s.miniAppResignOff()
			s.miniAppResignOff = nil
		}
		// 唤醒句柄的摘除在 `closePerWindow` 里（`releaseMini` 走的就是它）。
		s.releaseMini()
		// ⚠️ **主窗口要叫回来**。它是「进迷你」那一步收起来的（`openMiniPlayer`），
		// 而这一条除了点「退出」之外还有别的路会走到：macOS 上的 ⌘W、Windows 上
		// 的 Alt+F4。用户从那些路把迷你窗口关掉之后，主窗口还收着——应用还活着、
		// 音乐还在放，而屏幕上什么都没有了（连 Dock 上的窗口列表都是空的）。
		//
		// ⚠️ **走 `dropMini` 那条路这一支到不了**：它先摘 `mini.main = nil` 再
		// `Close`——`OnClosed` 跑到这一行时 `s.main` 已经是 nil，守卫恒假。
		// 那条路上叫回主窗的是 `closeMiniPlayer` 自己（`dropMini` 返回 true
		// 它就调 `restoreMainWindow`），**这一支只给 ⌘W / Alt+F4 那两条不经过
		// `dropMini` 的路兜底**——`main` 那时候还挂着。
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

// attachMiniWindowLifecycle 装迷你窗那些**装在 `miniWindow` 那一层上**的
// 生命周期钩子。
//
// 🔴 **它为什么单独拆出来、而且接的是 `miniWindow` 而不是 `*mygo.Window`**：
// 无头 Tester 里**没有真窗口**（`mygo.NewWindow` 在应用没 ready 时直接
// panic，`mygo/window.go:382`），而「关窗之前把窗高改回 84 ⇒ `StateKey` 落盘
// 的是 320×84」这一条**在真窗那一档量不到**（`captureState` 读的是原生那一
// 边的 bounds）。
//
// ⇒ 抽出这一层接口，测试用一个**假窗**走**同一条生产路径**（这个函数本身），
// 而「**删掉那个钩子，那条用例必须红**」正是靠这个——那正是评审用负控制抓到
// 的那类漏洞（删掉钩子之后旧的用例全绿）。
func (s *Shell) attachMiniWindowLifecycle(w miniWindow) {
	if w == nil {
		return
	}
	// 🔴 **关窗之前先把卷帘收掉**（票 #536）。
	//
	// 理由是 `StateKey`：**宽高也记在里面**（`mygo/window_state.go:196` 的
	// `captureState`），而抓状态是在关窗那一刻（`closeState()`，`window.go`
	// 的 `destroy` 先调它）。⇒ 展开着的时候直接关窗，落盘的就是「84 + 卷帘
	// 高」，下次开出来是个 320×(84+236) 的透明大窗——而**那一块透明的地方照样
	// 吃点击**（这一扇窗是 `Transparent: true`）。
	//
	// ⚠️ **钩的是 `OnClose` 而不是 `OnClosed`**：`OnClosed` 跑的时候原生那一边
	// 已经关掉了（`w.native == nil`），那时候 `SetSize` 不生效（`Window.do`
	// 里那个 `if w.native != nil`）。`OnClose` 在关窗**之前**跑（`mygo/window.go`
	// 的 `close()`：先 `fire1(&w.onClose, e)`，再 `w.destroy()`），改尺寸还来得及。
	w.OnClose(func(e *mygo.CloseEvent) {
		// ⚠️ **「立刻收起」而不是「滑下去」**：窗马上就要没了，滑下去那 240ms
		// 是白滑的——而更要紧的是**落盘的尺寸**（`StateKey` 抓的是关窗那一
		// 刻的宽高）。
		s.miniCurtainCollapseNow()
	})

	// 🔴 **窗被改尺寸了 ⇒ 判「是不是用户拖的」**（Bug 2a）。
	//
	// ⚠️ **它认不出「是我们 `SetSize` 的还是用户拖窗缘的」**：原生
	// `windowDidResize:` 对两者都叫——判据是「报上来的高度是不是我们刚设
	// 的那个」（`s.miniCurtain.setHeight`），见 `miniOnWindowResized`。
	//
	// ⚠️ **它装在 `miniWindow` 那一层上**（同 `OnMove`）：无头那一档要能
	// 手动触发它（`fakeMiniWindow.resize`）。
	w.OnResize(func() { s.miniOnWindowResized() })
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
	// 🔴 **收尾之前把卷帘收掉**（票 #536）：这一扇窗要关了，而
	// `StateKey` 会在关窗那一刻记下**宽高**——展开着关窗的话，落盘的就是
	// 「84 + 卷帘高」，下次开出来是个透明大窗。
	//
	// ⚠️ **这一句在 `closePerWindow` 之前**：那一句会把 `s.win` 置成 nil
	// （`Shell.Close` 里 `s.win = nil`），而改窗口尺寸要那扇窗还在。
	s.miniCurtainCollapseNow()
	s.closePerWindow()
}

// viewMini 是迷你播放器那一帧（原型 `syncMini`，DESIGN.md §0′.T5）。
//
// 一张 380 宽的卡片：顶部拖动条（`PUDMUSIC · MINI` + 回到主窗口）、封面 + 歌名 +
// 控制键、进度行，歌词卷帘在卡片里面往下展开。窗口本身是透明 + 毛玻璃的，
// 卡片画满整扇窗，圆角 16、描边 `line`。
//
// 🔴 铁律二：第一件事装主题，而且必须在最开头（`SetTheme` 只影响它之后创建的
// 控件，而 MyGo 每帧开头把主题重置成跟随系统那份，见 theme.go）。
func (s *Shell) viewMini(c *ui.Context) {
	s.applyFrameTheme(c)
	// 封面缓存要知道「新的一帧开始了」——理由见 `Shell.viewMain` 里那一句
	// （票 #358）。迷你窗口和主窗口共用一份缓存，两边各数各的帧。
	s.images.BeginFrame(c.Now())
	// 应用内快捷键：**和主窗口走同一条**（`viewShortcuts`），次序也一样（主题之后、
	// 其它每帧钩子之前）。不登记的话，设置页那句「应用内那一列：窗口在前台时按它
	// 就有用」在迷你窗口在前台时就是假的。
	s.viewShortcuts(c)

	st := s.app.Snapshot()
	t := c.Theme()
	p := tok.Of(t)

	// 🔴 **开关关着的时候，卷帘这一整套压根不该被触发**（票 #536）：设置页
	// 那一格被用户关掉之后，点封面**什么也不发生**；展开着的时候把开关关掉 ⇒
	// **立刻收起**（设置改完之后 `App.UpdateSettings` 会挨个叫醒每个窗口，这一扇
	// 下一帧读到的就是关的）。
	curtainOn := miniCurtainEnabled(s.app.SettingsSnapshot())
	if !curtainOn && s.miniCurtain.open {
		s.miniCurtainCollapseNow()
	}

	// 🔴 **「系统交给我们的那个文件，收下了没有」那一句话**（见 filenotice.go）：
	// **它占掉整张卡片**（迷你那一扇不画 toast，而迷你常常就是用户眼前那一扇）。
	noticeText, noticeTone, hasNotice := s.miniNotice()

	// 卡片底：surface（封面取色开着时染一点）。窗开了毛玻璃
	// （`MiniWindowOptions.Vibrancy`），全不透的底会把糊完的桌面整个盖掉，所以
	// 底是半透明的（`miniPanelAlpha`）。有那句话的时候换语义色。
	cardBG := s.coverWash(t, p.Surface).Alpha(miniPanelAlpha)
	noticeFG := t.Text
	if hasNotice {
		cardBG, noticeFG = toastColors(noticeTone, t)
		cardBG = cardBG.Alpha(miniPanelAlpha)
		// ⚠️ **有提示的时候窗高必须回到 194**（这一扇窗是透明的，而透明的那一块
		// 照样吃点击）：提示占的是整张收起的卡片，歌词卷帘收着。判据是 `open`
		// 而不是高度 > 0：展开的那一拍 `open` 已经是真，高度要到下一帧才 > 0。
		if s.miniCurtain.open {
			s.miniCurtainCollapseNow()
		}
	}

	// 根是透明的 `Fill()`：迷你窗故意没给 `DisableResize`（见 `MiniWindowOptions`），
	// 用户可以把窗往高里拖，多出来的那一块是窗自己的透明区。卡片只画到
	// 「194 + 卷帘高」。
	ui.Column(c).Key("mini-root").Fill().Children(func() {
		// ⚠️ **高度那一段推在这里**：它要建的元素得是根的子节点；也在
		// `applyFrameTheme` 之后（收起的那一段画的是当前这一帧的主题）。
		s.miniCurtainSyncHeight(c)
		h := s.miniCurtain.height
		if hasNotice {
			h = 0
		}
		// 卡片：圆角 16、描边 line、surface 底。拖动区挂在卡片上——指针落在
		// 控件之外的任何地方都是在拖窗口（命中链从最里层往外排，按钮自己先收到点击）。
		ui.Column(c).Key("mini-card").FillWidth().
			Height(float32(miniCurtainWinHeight(h))).
			Radius(miniRadius).Background(cardBG).Border(1, p.Line).Clip().
			DragWindow().Children(func() {
			if hasNotice {
				s.viewMiniNotice(c, noticeText, noticeFG)
				return
			}
			// 里面照样逐块包一层 Guard（铁律四）：崩掉封面那一块不该把控制键一起带走。
			s.guardAt("迷你播放器顶栏", func(c *ui.Context) { s.viewMiniDragBar(c, p) })(c)
			s.guardAt("迷你播放器主区", func(c *ui.Context) { s.viewMiniTop(c, st, p, curtainOn) })(c)
			s.guardAt("迷你播放器进度", func(c *ui.Context) { s.viewMiniSeek(c, st, p) })(c)
			s.viewMiniCurtain(c, st, t, p, h)
		})
	})
}

// miniNotice 报此刻迷你那一扇要显示的那句话（没有就报 false）。
//
// ⚠️ **它和主窗口那一条是两套**：主窗口那边是一条 toast（右下角那张卡片，
// 见 `viewToasts`），而迷你这一扇**不画 toast**——所以这一句另有一份
// **两扇窗共看**的状态（见 `filenotice.go`）。
func (s *Shell) miniNotice() (text string, tone notice.Tone, ok bool) {
	if s.notice == nil {
		return "", 0, false
	}
	return s.notice.current()
}

// viewMiniNotice 画「系统交给我们的那个文件」那一句。**它占掉整个迷你窗**。
//
// ⚠️ **底色是外层那张卡片画的**（`viewMini` 里按语义色换掉 `t.Surface`），
// 而这一段只画那句话——所以它不需要自己的底，也不会盖掉圆角。
func (s *Shell) viewMiniNotice(c *ui.Context, text string, fg ui.Color) {
	// ⚠️ **三行封顶**：这一扇只有 84 高（减去上下 12 的内边距还剩 60，
	// 而 12 号字一行约 16）——最多三行半。而那一句话最长的一档是
	// 「这个格式我们放不了（支持 .mp3 .flac .wav …）」，四行放不下。
	//
	// 放不下就**省略号**（而不是把字压到窗外）：用户看到的是「这句话被截了」，
	// 而完整的那一句在主窗口那条 toast 上（两扇窗看的是同一件事）。
	ui.Column(c).Key("mini-notice").Grow(1).FillHeight().
		Padding(space.Px12, space.Px12, space.Px12, space.Px12).
		Justify(ui.Center).Children(func() {
		ui.Text(c, text).FontSize(scale.Font(c, scale.Size12)).
			FontWeight(weightMedium).TextColor(fg).MaxLines(3).Ellipsis("…")
	})
}

// viewMiniDragBar 是顶部拖动条（原型 `.mini-drag`）：左边 `PUDMUSIC · MINI`
// （11 号等宽、字距 0.14em、text-3），右边回到主窗口的图标键（16 号图标）。
// 内边距 8 / 8 / 0 / 16，整行 44 高。
func (s *Shell) viewMiniDragBar(c *ui.Context, p tok.Palette) {
	ui.Row(c).Key("mini-drag").FillWidth().Padding(8, 8, 0, 16).AlignItems(ui.Center).
		Children(func() {
			typo.Mono(ui.Text(c, "PUDMUSIC · MINI")).FontSize(11).LetterSpacing(11 * 0.14).
				TextColor(p.Text3).SingleLine()
			ui.Spacer(c).Grow(1)
			// 回到主窗口：迷你 ↔ 主窗口是同一个开关的两头，图标说的是那件事。
			if s.withTip(c, pud.IconBtn(c, "mini-back", "expand", "切换mini/完整模式", pud.IconOpts{Size: 16}),
				"关掉迷你播放器，回到主窗口").Clicked() {
				if s.toggleMini != nil {
					s.toggleMini()
				}
			}
		})
}

// viewMiniTop 是封面 + 歌名 / 歌手 / 控制键那一块（原型 `.mini-top`）：
// 内边距 16、列间距 16、垂直居中；封面 72（圆角 8，点它展开 / 收起卷帘），
// 右边一列 24 + 20 + 8 + 36。
//
// ⚠️ **每一行、每一个键都给了 `Key`**（铁律四）：播放和暂停是两个不同的按钮
// （`st.Playing` 分支），歌从没有变成有的时候整列换一遍——没有 key 的话这些换位
// 会让后面每个控件的状态整体错配一格。
func (s *Shell) viewMiniTop(c *ui.Context, st app.State, p tok.Palette, curtainOn bool) {
	ui.Row(c).Key("mini-top").FillWidth().Padding(16).Gap(16).AlignItems(ui.Center).
		Children(func() {
			s.viewMiniCover(c, st, p)
			ui.Column(c).Key("mini-info").Grow(1).MinWidth(0).Children(func() {
				title := tclean.Clean(st.Title).Title
				col := p.Text
				if !st.HasTrack {
					// 抄 Biu 的 `暂无播放内容`（`pages/mini-player/index.tsx:128`）。
					title = nothingPlaying.Title
					col = p.Text2
				}
				// 原型 `<strong>` 15 号；系统字体里 600 与 700 是同一档。
				ui.Text(c, title).Key("mini-title").FontSize(scale.Font(c, 15)).FontWeight(miniTitleWeight).
					FixedLineHeight(24).MaxLines(1).Ellipsis("…").TextColor(col)
				ui.Text(c, st.Artist).Key("mini-artist").FontSize(scale.Font(c, scale.Size12)).
					FixedLineHeight(20).MaxLines(1).Ellipsis("…").TextColor(p.Text2)
				s.viewMiniControls(c, st, p, curtainOn)
			})
		})
}

// viewMiniCover 是左边 72 的封面（原型 `.mini-cover`）。它同时是**卷帘的触发器**
// （票 #536）：点它展开 / 收起歌词卷帘。
//
// ⚠️ **`Clicked()` 挂在外层那个 `Box` 上**：框架的命中链是从最里层往外排的，点击
// 落在链上第一个 interactive 的元素上，所以触发器要挂在包住封面的那一层，不额外再
// 包一层按钮。没封面时那一格只画 surface-2 的占位底（原型 `.mini-cover` 的 background）。
func (s *Shell) viewMiniCover(c *ui.Context, st app.State, p tok.Palette) {
	trigger := ui.Box(c).Key(miniCurtainCoverKey).Size(miniCover, miniCover).
		Radius(tok.R8).Background(p.Surface2).Clip().Shrink(0).
		Role(ui.RoleButton).Focusable().
		Label(miniCurtainCoverLabel(s.miniCurtain.open)).Description(st.Title).
		Children(func() {
			if st.CoverURL == "" {
				return
			}
			imgcache.ViewTitled(c, s.images, st.CoverURL, st.Title, miniCover, miniCover)
		})
	if trigger.Clicked() {
		s.miniCurtainToggle(c.Now())
	}
}

// viewMiniControls 是控制行（原型 `.mini-ctl`）：上边距 8、间距 4，上一首 / 播放 /
// 下一首，空隙，喜欢、歌词。
func (s *Shell) viewMiniControls(c *ui.Context, st app.State, p tok.Palette, curtainOn bool) {
	ui.Row(c).Key("mini-ctl").FillWidth().Margin(8, 0, 0, 0).Gap(4).AlignItems(ui.Center).
		Children(func() {
			// 能不能按看的是队列，和播放条上那两个键是同一个判据（`viewStepButtons`）。
			if pud.IconBtn(c, "mini-prev", "prev", "上一首", pud.IconOpts{}).
				Disabled(!s.app.CanPrev()).Clicked() {
				s.app.Prev()
			}
			s.viewMiniPlay(c, st)
			if pud.IconBtn(c, "mini-next", "next", "下一首", pud.IconOpts{}).
				Disabled(!s.app.CanNext()).Clicked() {
				s.app.Next()
			}
			ui.Spacer(c).Grow(1)
			// 喜欢：原型只在这首歌能点赞的时候画（`TR[t.id] ? … : ''`）。真数据取
			// 「赞」（`App.Likeable` / `LikeState`），和播放条上那一枚同一条路。
			if st.HasTrack && s.app.Likeable() {
				liked := s.app.LikeState().Liked
				name, label := "heart", "赞"
				if liked {
					name, label = "heart-on", "已赞"
				}
				if pud.IconBtn(c, "mini-like", name, label, pud.IconOpts{On: liked}).Clicked() {
					s.likeCurrent()
				}
			}
			// 歌词：和点封面是同一个开关；卷帘展开时 accent-text（`aria-pressed`）。
			if curtainOn {
				if pud.IconBtn(c, "mini-lyrics", "lyrics", "歌词", pud.IconOpts{On: s.miniCurtain.open}).Clicked() {
					s.miniCurtainToggle(c.Now())
				}
			}
		})
}

// viewMiniPlay 是播放 / 暂停（原型 `.mini-ctl .play-btn`）：36 的实心圆，正文色底、
// bg 色图标（16）。
//
// ⚠️ **key 按角色给（`mini-play`），不按按钮上的字**：字在「播放」和「暂停」之间
// 换来换去，而 key 若跟着字变，这个按钮每换一次字就换一个新身份，它的按下/悬停
// 状态跟着重来一次——界面上看不出坏了，只是按下去偶尔要按两下。
//
// 没歌时「播放」是灰的（35% 不透明度）：Biu 那边是 `isDisabled={!title}`。
func (s *Shell) viewMiniPlay(c *ui.Context, st app.State) {
	p := tok.Of(c.Theme())
	name := "play"
	if st.Playing {
		name = "pause"
	}
	b := ui.ButtonBase(c).Key("mini-play").Size(miniPlayBtn, miniPlayBtn).Radius(miniPlayBtn / 2).
		Background(p.Text).AlignItems(ui.Center).Justify(ui.Center).Shrink(0).
		Label(transportLabel(st.Playing)).Disabled(!st.HasTrack && !st.Playing)
	if b.IsDisabled() {
		b = b.Opacity(0.35)
	}
	b.Children(func() {
		g := ui.Icon(c, pud.Icon(name)).FontSize(16).TextColor(p.Bg)
		scaleTo := float32(1)
		if b.Pressed() && !b.IsDisabled() {
			scaleTo = 0.94
		}
		g.AnimatePose(ui.Pose{Scale: scaleTo}, ui.Animation{Duration: motion.Fast, Ease: motion.EaseOut})
	})
	// 🔴 **取流的时候它外面多绕一圈光**（票 #271）：这一扇窗里一个字都不说在
	// 取流，所以这一圈光是这儿唯一的取流提示。光圈绕着那枚 36 的圆；键是灰的时候
	// 不画（框架对禁用的控件整个乘 0.5 不透明度，见 loadhalo.go）。
	b = s.withLoadHalo(c, b, st, miniPlayBtn, st.HasTrack)
	if b.Clicked() {
		s.app.Toggle()
	}
}

// viewMiniSeek 是进度行（原型 `.mini .seek`）：内边距 0 16 12、列 40 / 1fr / 40、
// 间距 12；两端是 11 号等宽 text-3 的时间，中间是 4 高的轨道（悬停换主色）。
//
// 拖动的分工和播放条一样（`playbar.go` 的 `viewProgress`）：按住的时候值归
// 用户，松手才 seek。不这么分的话后台那个每 50ms 写回一次位置的时钟会和拖动
// 打架，症状是拖不动，或者松手又弹回去。
//
// ⚠️ 按住看的是 `Pressed()`，不是 `Dragging()`：后者对普通滑块永远是 false
// （理由见 `viewProgress`）。章节点画在滑块自己的画法之上（`withChapterDotsCore`），
// 松手那一下按「点 / 拖」分流（票 #585，规则见 `chapters.go`）。
func (s *Shell) viewMiniSeek(c *ui.Context, st app.State, p tok.Palette) {
	known := st.Duration > 0
	chs := st.Chapters
	ui.Row(c).Key("mini-seek").FillWidth().Padding(0, 16, miniSeekPadB, 16).Gap(12).AlignItems(ui.Center).
		Children(func() {
			typo.Mono(ui.Text(c, clockOrDash(st.Position))).Key("mini-cur").FontSize(11).
				TextColor(p.Text3).Width(miniTimeW).TextAlign(ui.Center).SingleLine()
			// key 装在 `c` 上：滑块的状态是建出来那一刻就初始化的。
			sl := selfSlider(c.Key("mini-progress"), &s.miniScrub, 0, 1, knobHoverLook).Grow(1).
				Height(miniSeekH).Label("播放进度").Disabled(!known)
			sl = withChapterDotsCore(sl, chs, st.Duration)
			switch held := sl.Pressed(); {
			case held && !s.miniHeld:
				s.miniHeld = true
				// 记下按下那一帧的指针和位置：松手时拿它们分「点 / 拖」。
				s.miniPressX, _, _ = sl.PointerPosition()
				s.miniPressPos = st.Position
				s.app.ScrubStart()
			case !held && s.miniHeld:
				s.miniHeld = false
				s.app.ScrubEnd(snappedScrub(sl, s.miniScrub, s.miniPressX, s.miniPressPos, st.Duration, chs))
			case !s.miniHeld && known:
				s.miniScrub = float64(st.Position) / float64(st.Duration)
			}
			typo.Mono(ui.Text(c, clockOrDash(st.Duration))).Key("mini-total").FontSize(11).
				TextColor(p.Text3).Width(miniTimeW).TextAlign(ui.Center).SingleLine()
		})
}
