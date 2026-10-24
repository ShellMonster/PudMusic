// 全屏播放器：Biu 的 `components/full-screen-player`（`index.tsx` 438 行）。
//
// 沉浸式听歌那一层：封面从播放条那一行**放大**成全屏背景、指针不动 3 秒之后
// 界面自己藏起来、右边一个把手开着分集面板、偏好面板里能开频谱。
//
// ## 浮在主布局**外面**
//
// `ui.Overlay` + `Absolute`（和 `queue_drawer.go` 同一个形状）：它是铺满整窗的一层，
// 底下那一层照旧在，只是看不见也点不着。塞进 `viewMain` 那个 `Column` 里就成了
// 「窗口矮一块」，而那不是全屏。
//
// ## 打开的那一帧，播放条**不建**
//
// 这是共享元素转场能两头都动的原因，值得写下来：
// 框架的 `Match` 要的是「新出现的那一个**上一帧没被摆过**」
// （`mygo/ui/transition.go:418` 的 `r.fresh = r.laid+1 != rt.frame`）。播放条那个
// 封面在下一帧还会被摆一次（它只是被浮层盖住了），于是它**不算新出现**，
// 满屏那个封面就找不到「上一个」了——不开转场，也不报错，只是封面「啪」地一下
// 就位。所以开着全屏的时候整个播放条都不建：它被一层不透明的背景盖着，少建它
// 界面上一模一样，而满屏那个封面因此是新的。关掉的那一帧反过来，播放条也是新的。
//
// 🔴 铁律二：这里**不调 SetTheme**。它在 `viewMain` 的同一个 build pass 里建
// （`viewMain` 第一行就 `applyFrameTheme` 过），读到的 `c.Theme()` 已经是用户选的
// 那一档。跟 `viewToasts`、`viewQueueDrawer` 一样。
//
// ⚠️ 外面那层 `Guard` 是 `viewMain` 加的（铁律三）：这一层一崩，崩掉的是整个窗口。
package ui

import (
	"math"
	"strconv"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/rowkey"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scrim"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/shadow"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	tclean "github.com/ShellMonster/Biu_Go/internal/ui/widgets/title"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

// fullHideDelay 是「指针不动多久之后把界面藏起来」。
//
// Biu 的 `handleMouseLeave` → `scheduleHideUi(3000)`（`index.tsx:132`）。
// 用**假时钟**测，见 fullplayer_test.go 的 TestTheInterfaceHidesAfterThreeSeconds。
const fullHideDelay = 3 * time.Second

// 全屏播放器上的几个尺寸，出处都在 Biu 那一行里。
//
//	封面宽   max(260, min(窗口宽*0.7, 窗口高*0.48, 520))   index.tsx:187
//	封面高   封面宽 * 0.75（4:3）                          index.tsx:188
//	频谱宽   min(640, max(400, round(窗口宽*0.5)))         index.tsx:189
//	频谱条数 max(48, min(128, round(频谱宽/7.5)))          index.tsx:190
//	频谱高   40                                             index.tsx:377
//	频谱离底 控制条高 + 12；界面藏起来时 24                 index.tsx:370
//	控制条高 由内容算（见 fullControlsHeight）              index.tsx:64,394
//	分集面板 宽 280，高 min(60vh, 420)                     index.tsx:424-426
//	分集行高 32                                             page-list.tsx:35
const (
	fullMinCoverWidth  = 260
	fullMaxCoverWidth  = 520
	fullCoverAspect    = 0.75
	fullSpectrumMinW   = 400
	fullSpectrumMaxW   = 640
	fullSpectrumBarPx  = 7.5
	fullSpectrumBarsLo = 48
	fullSpectrumBarsHi = 128
	fullSpectrumHeight = 40
	fullSpectrumGapY   = space.Px12
	fullSpectrumEdge   = 24

	// 满屏上播放/暂停那一枚的图标边长与点击区。
	//
	// 48 是上游那个数（`full-screen-player/index.tsx:396` 里的
	// `MusicPlayControl`，`music-play-control/index.tsx:23-25` 的
	// `RiPlayCircleFill size={48}`）；盒子也是 48，和播放条上那一枚是同一套
	// （`iconHitPlay`）——满屏这一行以它为主角，盒子不该比播放条小。
	//
	// ⚠️ **以前这里是 34 / 46，理由写的是「满屏这一行本来就以它为主角……再大就
	// 被切」**。那句话把因果说反了：不是「它小所以是主角」，而是满屏这一屏最该
	// 有主角；而被切的原因是**底栏高度没跟着它重算**（当时是 80）。票 #169 把
	// 底栏按上游重排（进度 20 + 缝 8 + 播放键 + 底边距 16），48 完全
	// 放得下。**改底栏的留白时这个高度要一起重算**，别再让它去迁就一个旧框。
	fullPlayGlyph = 28
	fullPlayHit   = 64

	// 顶栏：上游 `px-6 py-4`（`index.tsx:291`）——左右 24、上下 16。
	//
	// fullTopHeight 是它整条的高度：py-4 的上下两份（32）加最高的那个孩子
	// （32 的图标键）。它同时是「藏起来时整条要移出窗口」那个位移量的一半。
	fullTopHeight = 64
	fullTopPadX   = space.Px24
	fullTopPadY   = space.Px16
	// fullTopGap 是关闭键 / 歌名 / 设置之间那道缝：上游的 `space-x-2` = 8。
	fullTopGap = space.Px8
	// fullTopKey 是满屏顶栏上那两枚图标键的点击区边长。
	//
	// 32 的框里，上游那两枚 28 / 22 的图标（`index.tsx:294,313`）每边分别留
	// 2 / 5；图标画多大由调用方给，这里只管点击区。
	fullTopKey = 32

	// 底栏：上游 `px-12 mb-4 max-w-6xl`（`index.tsx:394` 那一层）——
	// 左右 48、离底 16、最宽 1152（6xl = 72rem = 1152px）。
	//
	// fullControlsHeight 是整条底栏的高度，**由内容算出来**：
	// 进度行 20（`knobFullLook` 那个 `Height(t.Space(5))`）
	// + 缝 8（`gap-2`）
	// + 播放键 64（`fullPlayHit`，这一行最高的那一个；设计规范 §5）
	// + 离底 16（`mb-4`）
	// = 108。藏起来的时候整条要移出窗口，靠的也是这个数。
	fullControlsHeight = 108
	fullBottomPadX     = space.Px48
	fullBottomMargin   = space.Px16
	fullBottomMaxW     = 1152
	fullBottomGapY     = space.Px8

	fullPartsWidth = 280
	// fullPartsPanelRadius 是分集面板那一块的圆角。**12**——它是**玻璃面板**，
	// 走的是 HeroUI 那一档（`rounded-2xl`），**不跟全局圆角令牌**：这一块
	// 是浮在满屏之上的一层玻璃，边缘要圆得比内容块明显，而内容块的圆角是
	// 用户可调的（默认 8、上限 24）——跟着它走的话，用户把圆角调到 24，
	// 这一块会变成一个「几乎不圆」的方块，玻璃那一层就看不出是浮着的了。
	fullPartsPanelRadius = 12
	fullPartsHeight      = 420
	fullPartsTabW        = 26
	fullPartsTabH        = 96
	fullPartRowHeight    = 32
)

// fullScrim 是背景上那层压暗。
//
// Biu 那边是「模糊过的封面 + 三层 radial-gradient」（`glassmorphism.ts`），而
// 毛玻璃那一层已经在了（见 fullBackdropMaterial）。压暗这一层留着，因为它管的
// 是**另一件事**：满屏的控件和字全是**白**的（Biu 那边
// `dark: showBlurredBackground || appTheme === "dark"` 强制深色前景）。不压暗的
// 话，一张浅色的歌封面会把白字吃掉——而 Biu 那边靠三层渐变挡着，我们这边靠这一层。
//
// ⚠️ 它压在毛玻璃**上面**（不是下面）：它要压的是糊完的那一层。
//
// ⚠️ **0.55 就是改动前那个数，别为了「更严」去动它**：最坏的一档（封面是纯白）
// 底色是 255×0.45 = 115，而 20px 的歌名（#169 之后是**常规字重**、白 92%）压在
// 它上面实测 **4.31:1**（`TestTheTextOverTheBlurredBackgroundIsStillReadable`
// 量出来的那个数）。WCAG AA 对普通文字的门槛是 4.5:1，差 0.19——而加粗那一档
// 曾经够「大字」的 3:1，票 #169 按上游的 20/400 把字重改成 400 之后，这个理由不再成立。
//
// ⚠️ 满屏里那些 12px 的小字（时间标签，白 62%）更低，是 **2.85:1**。两档一起
// 归**可及性**那一票（issue #139「接上系统的高对比度」：系统在「增加对比度」下
// 另算一套颜色，比在这里硬调一个数对）。要让 12px 也到 4.5，压暗得加到 0.72，
// 而那会让背景暗到看不出毛玻璃——这一屏的主角就没了。
var fullScrim = ui.RGBA(0, 0, 0, scrim.Media)

// fullBackdropScale 是背景那一层的画布缩到窗口的几分之一。
//
// 画布是**像素**：窗口 1200×800 的话画布是 300×200，画上去再放大四倍铺满整窗。
// 四倍这个数不是随便挑的——Biu 那边就是「同一张图放大四到五倍并且明显模糊」
// （`glassmorphism.ts`），而放大之后那些细节本来也读不出来。
//
// ⚠️ **画面上的模糊强度和它无关**：画布上的半径按画布与窗口的比例换算（见
// fullBackdropSigma），所以窗口多大、屏幕多密，糊的程度都是 σ=32 DIP。
const fullBackdropScale = 4

// fullBackdropBlur 是背景那一层糊掉多少（DIP，标准差，和 CSS 的 blur() 一样）。
//
// Biu 那边是「同一张图放大四到五倍并且明显模糊」，而框架的毛玻璃没有按设备分级
// 那套入口（票面写明「不做」），所以这里是**一个固定的、够糊的值**：σ=32 之后
// 封面上任何细节都读不出来，界面上那些白字底下只剩一层底色。
//
// ⚠️ 这是**毛玻璃自己的模糊半径**，不是我们画的等效效果——真正画上去的是框架
// `plugins/glass` 那一份。
const fullBackdropBlur = 32

// fullBackdropSigma 是画布上的模糊半径（画布像素）。
//
// 画布是窗口的 1/4，所以画布上的 σ=8 放到屏幕上正好是 32 DIP——**和画布多大
// 无关**（窗口改了尺寸，画布跟着改，这个数跟着换算）。
func fullBackdropSigma(canvasW, winW float32) float32 {
	if winW <= 0 {
		return fullBackdropBlur
	}
	return fullBackdropBlur * canvasW / winW
}

// fullBackdropMaterial 是背景那一层糊的那一份材质。
//
// ⚠️ **它是包级的一个函数，不是常量**：框架的 `Element.Material` 只往节点上设、
// 没有读回来的办法（`mygo/ui/frame.go:144`），而这一票的验收口径是「背景那一层
// 是毛玻璃材质」——「那一层」总得有个能读的地方。测试读的就是这个函数，所以把
// 它换回一层纯色、或者换成我们自己画的等效效果，那条断言会红。
//
// 里面装的是**框架自带的那一份**：`glass.Blur` 是三端一致的背景模糊（Metal /
// HLSL / GLSL 三套着色器 + CPU 等价实现），不是自绘的等效效果，也不是新增依赖
// （和框架同一个模块，许可清单不变）。
//
// ⚠️ sigma 是**画布上的**半径（见 fullBackdropSigma）。
func fullBackdropMaterial(sigma float32) ui.Material { return glass.Blur{Radius: sigma} }

// fullOnColor / fullDimColor 是压在**毛玻璃背景**上的两级前景色（白 / 白一点七折）。
//
// ⚠️ 关掉虚化背景之后底是用户挑的颜色（或者主题底色），浅色底上白字读不出来
// ——Biu 那边也是这么处理的：`computedForegroundHex` 用 `readableColor(baseBg)`
// 现算一份（`full-screen-player/index.tsx:161-170`）。这一层算这件事的地方是
// `fullForeground`，别在调用点上各写一份。
var (
	fullOnColor  = ui.RGBA(255, 255, 255, 0.92)
	fullDimColor = ui.RGBA(255, 255, 255, 0.62)
)

// fullForeground 是满屏这一屏此刻该用的两级前景色。
//
// 虚化背景开着的时候底是糊过的封面压一层 55% 的黑，白字有底——直接给白。
// 关掉之后底是 `BackgroundColor`（空串 = 主题背景色），浅底要给黑字，否则
// 用户挑了一个浅色背景就看不见歌名和控件了。
//
// 「浅」的判据是**相对亮度**（WCAG 那一套），不是「RGB 加起来过半」：同样是
// 0.5 的阈值，`#00ff00` 这种绿在肉眼上远比 `#0000ff` 亮，加权之后才和观感
// 对得上。
//
// ⚠️ **阈值是 0.179，不是 0.5**：那是「黑字和白字对比度相等」的那个点（解
// `(L+0.05)/0.05 = 1.05/(L+0.05)`）。0.5 的话，一块中灰（L≈0.3）会给白字，
// 对比度只有 3:1，而黑字有 7:1。Biu 用的 color2k `readableColor` 也是 0.179。
func (s *Shell) fullForeground(c *ui.Context) (on, dim ui.Color) {
	p := s.app.FullPlayerPrefs()
	if p.ShowBlurredBackground {
		return fullOnColor, fullDimColor
	}
	bg := c.Theme().Background
	if p.BackgroundColor != "" {
		if col, ok := parseHexColor(p.BackgroundColor); ok {
			bg = col
		}
	}
	if colorLuminance(bg) > 0.179 {
		return ui.RGBA(0, 0, 0, 0.92), ui.RGBA(0, 0, 0, 0.62)
	}
	return fullOnColor, fullDimColor
}

// fullBackground 是关掉虚化背景时铺在底下那一层的颜色。
func (s *Shell) fullBackground(c *ui.Context) ui.Color {
	if hex := s.app.FullPlayerPrefs().BackgroundColor; hex != "" {
		if col, ok := parseHexColor(hex); ok {
			return col
		}
	}
	return c.Theme().Background
}

// colorLuminance 是 WCAG 的相对亮度（0 = 黑，1 = 白）。
func colorLuminance(c ui.Color) float64 {
	lin := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}

// coverMatch 是「封面在两个地方是同一个东西」的身份。
//
// 🔴 **用自己定义的类型**（框架文档明说：「Use a type of your own for the
// identity, so that it is no other's」）。值是当前这一首的判重标识——换一首歌就是
// 另一个身份，于是转场不会把 A 的封面拉到 B 的位置上。
//
// 它就是 SwiftUI `matchedGeometryEffect` 在这一层的等价物：框架 `ElementTransition`
// 的 `Match` 字段自己的注释就是这么写的（`mygo/ui/transition.go:44-48`：
// 「as SwiftUI's matchedGeometryEffect and Motion's layoutId」）——把两处元素认成
// 同一个东西，让它在两个框之间自己长过去。给两个元素同一个 `Match`，新的那个
// 从旧的那个**上一次摆在哪**长过来。
type coverMatch struct{ id string }

// coverTransition 是封面那一次转场。`Snappy` 是框架自带的弹簧。
func coverTransition(id string) ui.ElementTransition {
	return ui.ElementTransition{Animation: &ui.Snappy, Match: coverMatch{id: id}}
}

// fullPlayer 是全屏播放器自己那一摊。**只由 view 在主线程上读写**（铁律一）。
type fullPlayer struct {
	// open 是它开着没有。
	open bool
	// lastMove 是指针最后一次动的时刻。界面藏没藏就是拿它和现在比。
	lastMove time.Time
	// hideStop 是那个「3 秒后叫醒界面」的钟。每一次指针动都停掉重排一个。
	hideStop func()
	// mouseX/mouseY/mouseOK 是上一帧指针在哪儿。认出「它动了没有」靠和它比。
	mouseX, mouseY float32
	mouseOK        bool

	// settings 是偏好面板开着没有（开着的时候不藏界面）。
	settings bool
	// partsOpen 是分集面板开着没有。
	partsOpen bool
	// partsRows 是这一帧要画的那几行，partsList 是它们的列表状态。
	partsRows []partRow
	partsList ui.ListState
	// partsRowKeys 记着分集面板里每一行的身份（**预先装箱好的那一份**）。
	//
	// ⚠️ 装箱那一步是要分配的（`runtime.convTstring`），现算的话每帧每行一次
	// （票 #313）。**它自己知道「数据换了」**（见 `rowkey.Memo` 的文件头）。
	partsRowKeys rowkey.Memo[string, any]

	// bg 是背景那一层的画布：封面糊过一次画进去，稳态下每一帧只是一张图
	// （见 viewFullBackdropCanvas 为什么是画布而不是一层材质）。
	bg *ui.Canvas
	// bgW/bgH 是这张画布的尺寸（窗口改了尺寸要重建）。
	bgW, bgH int
	// bgURL/bgBitmap 是这张画布画的是哪一张封面（地址 + 位图）。变了才重画。
	//
	// ⚠️ **两个字段分开比，不拼成一个字符串**：原先是一个 `fmt.Sprintf` 出来的
	// 键（地址 + 位图指针 + 宽高），于是**每一帧**都要拼一次字符串（一次分配
	// ＋一次格式化），而这一帧本来可以一个字节都不分配。
	bgURL    string
	bgBitmap *ui.Bitmap
	// bgDirty 是「画布要重画」那一下（画它要一个 Painter，只有画的时候有）。
	bgDirty bool
	// bgDraws 是这张画布被重画了几次。**给测试看的**：毛玻璃每帧重渲一遍就是
	// 这里在涨（那在软件渲染上是 80~100 毫秒一帧）。
	bgDraws int

	// spec 是频谱那一块复用的中间量（见 fullplayer_spectrum.go）。
	spec spectrumBuffers
}

// partRow 是分集列表里的一行。
//
// 这一份是**两个入口共用**的行模型：满屏里那个分集面板（`viewFullPartsList`）
// 和播放条上的分集抽屉（`partdrawer.go`）。共用是为了「正在放的那一集」只有一处
// 认——认两处的话，迟早有一处认错，而那正是用户在找的那一句。
type partRow struct {
	key string
	cid string
	// page 是第几集。播放条那个抽屉画它（见 partdrawer.go），满屏那个面板不画
	// （照 Biu 那一支，见 `viewFullPartsList` 的注释）。
	page   int
	title  string
	active bool
}

// viewFullPlayer 画全屏播放器。没开就一个字都不建。
//
// ⚠️ **状态是外面递进来的**（和 `viewPlayBar` 同一个样子）：`viewMain` 那一帧
// 已经取过一份快照了（`st`），这里再取一份就是**两份快照、两个时刻**——播放
// 状态在那一帧里变了的话，底下那条播放条和这一屏画的是不同的歌。
//
// 而它也是**测试能摆一份假状态**的地方（见 framebudget_test.go）：满屏那一档
// 的每帧成本要量的是**画界面**，不是**放歌**——真放歌的话，解码器那条 goroutine
// 会在测量窗口里分掉一大笔（一次性缓冲，约 130 KB），那是音频的成本，不是
// 界面的，而它落在哪一帧还是随机的。
func (s *Shell) viewFullPlayer(c *ui.Context, st app.State) {
	if !s.full.open {
		return
	}
	// 记一笔「满屏建了一遍」——给「一帧建了几遍」那条测试数（见 builds.go）。
	s.builds.full++
	if !st.HasTrack {
		// 🔴 **不能在这里关掉自己**。
		//
		// `HasTrack` 为 false 有两种情形，而第一种非常常见：
		//
		//	换歌的**那一瞬**  在线流是后台 goroutine 取流（`App.playOnline`），
		//	                  这几百毫秒里 `HasTrack` 恒为 false
		//	真的没歌了        用户按了停止、或者换歌换失败了
		//
		// 分集面板换集（`App.PlayPart`）、`App.PlayAt` 换另一首在线歌、
		// `playNextAfterCurrent` 放完自动接下一首——这三条都先 `ClearTrack()`
		// 再异步开流。第一版在这里 `closeFullPlayer()`，于是**每一次换歌都会把满屏
		// 关掉**：用户点一下分集面板就被踢回普通窗口，而这一票分集面板的全部用处
		// 正好没了一半。
		//
		// Biu 那边也是这么处理的：`isOpen` 和 `playItem` 是两件事，内容那一支是
		// `{!playItem ? <Empty/> : <DrawerBody …>}`（`index.tsx:214`）——**开着就
		// 开着**，只是内容换成空状态。我们照抄。
		s.viewFullEmpty(c)
		return
	}

	ui.Overlay(c, func() {
		// 背景先摆：它是整窗的一层，后面那些控件都压在它上面。
		s.viewFullBackdrop(c, st.CoverURL)

		// 铺满整窗的那一层：它报指针的位置（认出「动了没有」），顺便吃掉底下
		// 那一层的点击。
		//
		// ⚠️ 它必须在**背景之上、控件之下**：控件要能拿到点击，而命中测试是从上
		// 往下走的，先撞上的是控件。
		probe := ui.Box(c).Key("full-probe").
			Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Modal()

		s.syncFullPointer(probe)

		hidden := s.fullHidden()
		if hidden {
			// 界藏起来了，光标也不留
			// （Biu 的 `cursor: isUiVisible ? "auto" : "none"`，index.tsx:209）。
			probe = probe.Cursor(ui.CursorNone)
		}

		probe.Children(func() {
			s.viewFullTopBar(c, st, hidden)
			s.viewFullStage(c, st, hidden)
			s.viewFullSpectrum(c, st, hidden)
			s.viewFullControls(c, st, hidden)
			s.viewFullParts(c, hidden)
		})

		if probe.OverlayShortcut(0, ui.KeyEscape) {
			s.closeFullPlayer()
		}
	})

	// 播放列表抽屉。**排在这里**（`Overlay` 之后）才会画在满屏上面：框架的
	// 浮层都挂在同一个 overlay root 上，后建的在上。`viewMain` 那边在全屏开着
	// 的时候跳过它，不然同一个视图会建两遍（key 会撞）。
	//
	// ⚠️ 抽屉底下留出 `playbarHeight` 那一截（见 queue_drawer.go）：满屏里
	// 那一截正好是底栏的进度和控制键，抽屉开着时它们仍然按得动。
	s.viewQueueDrawer(c)
}

// fullNothing 是「满屏开着、此刻没有歌」那一句。
//
// ⚠️ **不能复用 `nowplaying.go` 那一份**：那边那句提示是写给迷你播放器的
// （「在主窗口里点『打开文件』，这边就会显示正在放什么」），而在满屏里主窗口整个
// 被盖住了，照抄过来那句话指向的地方用户根本看不到。
//
// 提示写的是**这一屏里做得到的事**：按 Esc 或者点左上角退出去，主窗口里放歌。
var fullNothing = emptyState{
	Title: "暂无播放内容",
	Hint:  "按 Esc 退回去，在主窗口里放几首歌进来",
	Icon:  iconEmptyMusic,
}

// viewFullEmpty 是「满屏开着、此刻没有歌」的那一屏。
//
// 三样都留着：背景、顶栏（关掉的那一条）、指针那三秒。少任何一样，用户就是被
// 关在一屏没法操作的东西里——而**换歌的那几百毫秒正好会走到这里**。
//
// 空状态的话照 `nowplaying.go` 那一份复用（同一句话，同一个出处）。
func (s *Shell) viewFullEmpty(c *ui.Context) {
	ui.Overlay(c, func() {
		s.viewFullBackdrop(c, "")

		probe := ui.Box(c).Key("full-probe").
			Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Modal()

		s.syncFullPointer(probe)

		hidden := s.fullHidden()
		if hidden {
			probe = probe.Cursor(ui.CursorNone)
		}

		probe.Children(func() {
			s.viewFullTopBar(c, app.State{}, hidden)
			on, dim := s.fullForeground(c)
			ui.Column(c).Key("full-empty").
				Absolute().Left(0).Top(0).Right(0).Bottom(0).
				Center().Gap(space.Px6).Children(func() {
				// ⚠️ **这两行是故意的例外，不走 `emptyCopy`**：满屏这一屏压在
				// 一块有底色的背板上，所以主次色是那块背板的 on / dim
				// （`fullForeground`），而不是主题的 Text / TextMuted——用主题
				// 那两档的话，在浅色封面铺满的屏上这两行会糊进去。字号仍然是
				// 15 / 12，和别处一样。
				ui.Text(c, fullNothing.Title).FontSize(scale.Font(c, scale.Size15)).TextColor(on)
				ui.Text(c, fullNothing.Hint).FontSize(scale.Font(c, scale.Size12)).TextColor(dim)
			})
		})

		if probe.OverlayShortcut(0, ui.KeyEscape) {
			s.closeFullPlayer()
		}
	})

	// 换歌的那几百毫秒里 `HasTrack` 是 false，界面走的是这一支；抽屉得跟着
	// 留着——不然用户刚打开播放列表、点了另一首，抽屉会闪一下不见。
	s.viewQueueDrawer(c)
}

// closeFullPlayer 关掉全屏播放器，顺手把那一摊复位。
//
// ⚠️ 复位要连指针那一摊一起清：不的话下一次打开时 `mouseOK` 还留着上一次的
// 坐标，`syncFullPointer` 会把「指针一动都没动」认成「动了一大跳」。
func (s *Shell) closeFullPlayer() {
	s.disarmFullHide()
	s.full = fullPlayer{}
}

// openFullPlayer 开全屏播放器。
//
// 必须有一首歌在放：播放条上那一张封面没有的时候就没有东西可点，而这儿也还有
// 别处会走这条路（快捷键将来就有），所以这里再判一次。
func (s *Shell) openFullPlayer() {
	if !s.app.Snapshot().HasTrack {
		s.addToast("还没有在放什么，先进一首歌", notice.Warning)
		return
	}
	if s.full.open {
		return
	}
	s.full.open = true
	s.full.settings = false
	s.full.partsOpen = false
	// ⚠️ **播放条上那个分集抽屉也得收掉**：满屏开着的时候播放条**根本不画**
	//（`viewMain` 里 `if !s.full.open`），于是那个键不在、浮层也不建，而
	// `partOpen` 还留着 true。用户从满屏退回普通窗口的那一刻，这一列会自己弹出来
	// ——他什么都没按。
	s.partOpen = false
	s.armFullHide()
}

// ---- 背景 ----

// viewFullBackdrop 是满屏背景：封面糊过一次铺满整窗 + 一层压暗。
//
// 封面取不到（本地音乐没有封面地址，见 `app.State.CoverURL`）时铺主题的底色，
// 不是一片黑——那是「没有这一张图」，不是「加载失败」。
//
// ## 两层的顺序是**要紧**的
//
// 压暗那一层压在糊完的那一层**上面**——它压的是糊完的，白字才有底。反了的话
// 压的是没糊的那一张，底色会亮一档。
func (s *Shell) viewFullBackdrop(c *ui.Context, url string) {
	// 关掉虚化背景：铺一层用户挑的颜色（空串 = 主题背景色），没有封面、没有
	// 压暗——照 Biu 的 `{!showBlurredBackground && <div style={{backgroundColor}}/>}`
	// （`index.tsx:227-229`）。
	if !s.app.FullPlayerPrefs().ShowBlurredBackground {
		ui.Box(c).Key("full-backdrop").
			Absolute().Left(0).Top(0).Right(0).Bottom(0).
			Background(s.fullBackground(c))
		return
	}
	ui.Box(c).Key("full-backdrop").
		Absolute().Left(0).Top(0).Right(0).Bottom(0).
		Background(c.Theme().Background).
		Children(func() {
			// 背景那一层：封面糊过一次，画在一张画布里（见 viewFullBackdropCanvas）。
			//
			// ⚠️ 它**不接管点击**：这一层在 `probe` 底下，而 `probe` 是那个铺满整窗
			// 的 `Modal` 盒子，点击先撞上它（见 viewFullPlayer 的注释）。
			s.viewFullBackdropCanvas(c, url)
			ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Background(fullScrim)
		})
}

// backdropChanged 报「背景那张画布要不要重画」。
//
// 换了封面、换了位图、或者窗口改了尺寸（画布的大小跟着变），才重画。
//
// ## ⚠️ **四个判据分开比，不拼成一个字符串**
//
// 原先是一个 `fmt.Sprintf("%s|%p|%dx%d", …)` 拼出来的键，于是**每一帧**都要
// 拼一次字符串（一次分配 ＋ 一次格式化）——而满屏开着的时候这一帧是**每一帧**
// 都要走的，那一笔于是每帧都付一遍。分开比是零分配的。
func (f *fullPlayer) backdropChanged(url string, bm *ui.Bitmap, w, h int) bool {
	return f.bgURL != url || f.bgBitmap != bm || f.bgW != w || f.bgH != h
}

// viewFullBackdropCanvas 是背景那一层：封面糊过一次画进一张画布，之后每一帧
// 只是把那张画布放大铺上去。
//
// ## 为什么不直接用一层材质
//
// 毛玻璃是**每帧重渲一遍**的（框架的 `Material` 是画的时候才生效）。满窗
// 1200×800 上 σ=32：
//
//   - **GPU**（真程序走的那条：Metal / D3D11 / GL）：几遍小 pass，测不出帧率上的
//     代价（见报告的「帧率」一节）。
//   - **软件渲染**（框架的回退路：`MYGO_GPU=0`、拿不到原生 surface、`metal.New`
//     失败、GPU 掉线后的降级）：每一帧要多花 **80~140 毫秒**，满屏掉到个位数
//     帧/秒（实测 5.7 帧/秒）。
//
// 画布是框架给「画一次、每一帧当图贴」准备的那个口（`mygo/ui/canvas.go`）：封面
// 变了或者窗口改了尺寸才重画一次（**那一次才糊**），稳态下每一帧只是一张图。
// 票面留的那个决策点（「要不要把背景层的结果缓存成位图」）到这里落了地。
//
// ## 糊还是**框架的糊**
//
// `glass.Blur` 的材质原样画进画布（`PaintMaterial`），不是我们自己糊的——
// 「不自绘等效效果」那一条仍然守得住。
func (s *Shell) viewFullBackdropCanvas(c *ui.Context, url string) {
	winW, winH := c.Size()
	st := s.images.Get(url)
	if !st.Ready() {
		// 封面还没来：底下是主题的底色（外头那一层），画布先不动。
		return
	}
	cw := max(int(winW/fullBackdropScale), 1)
	ch := max(int(winH/fullBackdropScale), 1)
	// 换了封面、换了位图、或者窗口改了尺寸（画布的大小跟着变），才重画。
	if s.full.backdropChanged(url, st.Bitmap, cw, ch) {
		if s.full.bg == nil || s.full.bgW != cw || s.full.bgH != ch {
			s.full.bg = ui.NewCanvas(cw, ch)
			s.full.bgW, s.full.bgH = cw, ch
		}
		s.full.bgURL, s.full.bgBitmap = url, st.Bitmap
		s.full.bgDirty = true
	}
	if s.full.bg == nil {
		return
	}
	// 画布的内容要一个 Painter，只有画的时候有：放在 Draw 回调里。
	ui.Box(c).Key("full-backdrop-canvas").
		Absolute().Left(0).Top(0).Right(0).Bottom(0).
		Draw(func(p *ui.Painter, r ui.Rect) {
			if !s.full.bgDirty {
				return
			}
			s.full.bgDirty = false
			s.full.bgDraws++
			box := ui.Rect{W: float32(cw), H: float32(ch)}
			s.full.bg.Draw(p, func(cp *ui.Painter) {
				cp.Image(st.Bitmap, box, ui.Cover)
				fullBackdropMaterial(fullBackdropSigma(float32(cw), winW)).
					PaintMaterial(cp, box, [4]float32{})
			})
		})
	// 把它放大铺满整窗（放大倍数是 fullBackdropScale）。
	ui.Image(c, s.full.bg.Bitmap()).Fill().Fit(ui.FillBox)
}

// ---- 顶上那一条 ----

// viewFullTopBar 是顶上那一条：关闭、歌名、偏好面板。
//
// 排列照 Biu（`index.tsx:291-325`）：左上角收起、旁边是歌名、再旁边是设置。
//
// 🔴 **左边必须让开红黄绿**：隐藏标题栏的窗口里系统控件是叠在内容**上面**的，
// 而满屏把普通顶栏整条盖住了——于是「关闭全屏」那一枚（当时是一枚文字键）正好被黄灯和绿灯压住
// （实测按钮框 x∈[39,91]，红黄绿整簇 x∈[14,66]；现在是 32×32 的图标键，框更窄，
// 但它贴着左边距的时候照样会落进那一簇）。做法照 navbar.go 那一处：
// `c.TitleBar()` 给的 `bar.Left` 就是左边让出的量。
//
// ⚠️ 同一条顶栏还要能拖窗口：浮层盖住整窗，普通顶栏上那两处 `DragWindow`
// （navbar.go）够不着了。
func (s *Shell) viewFullTopBar(c *ui.Context, st app.State, hidden bool) {
	titleBar := c.TitleBar()
	on, _ := s.fullForeground(c)

	shown := fullShown(hidden)
	bar := ui.Row(c).Key("full-top").
		Absolute().Left(0).Right(0).Top(0).Height(max(titleBar.Height, fullTopHeight)).
		Padding(fullTopPadY, fullTopPadX, fullTopPadY, fullTopBarPadLeft(titleBar)).
		Gap(fullTopGap).AlignItems(ui.Center).DragWindow()
	bar = bar.Opacity(bar.AnimateTo("full-top:o", shown, ui.Smooth))
	// ⚠️ 藏起来的时候要**移出窗口**，不只是 Opacity(0)。不移的话那两个看不见的
	// 按钮仍然接点击：停在窗口顶部点一下，满屏就被关掉；点右上角会把偏好面板
	// 凭空开出来。底下那一条 controls 用 `Translate` 推出去了，顶栏原来没做。
	//
	// ⚠️ **方向和底下那一条相反**：controls 在底部，「滑出去」是往下（正的 Y）；
	// 顶栏在顶部，「滑出去」是往上（负的 Y）。写反了的话它滑到整条还在窗口里
	// （正的 `fullTopHeight`），按钮照样点得动。
	bar = bar.Translate(0, -(1-shown)*(titleBar.Height+fullTopHeight))

	// ⚠️ 这一帧**进来时**面板是开着没有：`ui.Popover` 处理「点外面 / 按 Esc 关掉」
	// 是在它自己那一趟里做的，进函数时读到的还是上一帧的值。要判断「刚刚被关掉」
	// 就得先把进来时的值记下来（理由同 shell.go 的 syncRoute）。
	wasOpen := s.full.settings

	bar.Children(func() {
		// 关闭全屏：上游是一枚 `RiArrowDownSLine` 28 的图标键（`index.tsx:294`），
		// 不是写着字的药丸。字色必须自己给（`fullForeground`）：扁平按钮不再像
		// `ui.Button` 那样把 `t.Text` 刷上来，图标是从祖先继承的——而这一条顶栏
		// 压在压暗过的封面上，浅色主题下 `t.Text` 是黑的，黑图标会整个消失在背景里。
		//
		// ⚠️ **不给 `Tooltip`**：这一条顶栏三秒之后会自己藏起来（整条 `Translate`
		// 出窗口），而提示是**浮在整窗之上**的另一个元素——它不跟着那一趟平移走，
		// 于是界面藏好之后还留着一句悬空的话（实测：那一条「第二次没藏干净
		// （138 个亮像素）」）。
		if iconButton(c, iconChevronDown, "关闭全屏", 28).
			Size(fullTopKey, fullTopKey).Padding(space.None, space.Px2).
			TextColor(on).Clicked() {
			s.closeFullPlayer()
		}
		// 歌名：上游是 `text-xl`（20 像素），而且限了宽度（最多占五分之二）。
		//
		// ⚠️ **20 / 400**，不是 20 / 700：上游那个 `h2` 的字重被 Preflight 重置成
		// inherit（`DESIGN.md` 的 `settings_h2` 就是 20/400）。20 → 22 或者加粗
		// 都会让它变成「页头」，而这一行只是歌名。
		//
		// ⚠️ **不许 `Grow(1)`**：上游那一组是 `flex w-full max-w-2/5 items-center
		// space-x-2`（`src/components/full-screen-player/index.tsx:290-321`）——
		// **齿轮是这一组的最后一个子元素，而 `<h2>` 不 grow**，所以齿轮**紧跟在歌名
		// 后面**（8 像素），不是靠右。
		//
		// 原来这里是 `Grow(1)` + `MaxWidth(480)`：歌名框铺满 480，把齿轮顶到
		// x=552（窗口 1200 宽的 **47%** 处），离歌名 421 像素——看起来像一个不属于
		// 任何东西的浮标（审计 F-03）。
		//
		// ⚠️ **这一处的字重要写出来**（`FontWeight(400)`）：票 #169 的
		// `TestTheFullPlayerTitleIsNotBold` 钉的就是**这一句写着 400**（像素那一层
		// 量不出 400 还是 700，能钉住的只有代码里那一个调用）。
		//
		// ⚠️ **别拿它当「页标题不写字重」那一档的样板**：页标题（`scale.PageTitle`）
		// 那一档一个字重都不写（`TestPageTitlesAreNotBold` 钉着），这里是**歌名**
		// 那一档，两者的判据不是同一条。
		ui.Text(c, tclean.Clean(st.Title).Title).FontSize(scale.Font(c, scale.PlayerTitle)).FontWeight(400).TextColor(on).MaxWidth(fullTitleMaxWidth(c)).SingleLine()

		// 同上面那一条：提示不跟着顶栏一起藏起来，所以这里不给 `Tooltip`。
		gear := iconButton(c, iconSettings, "全屏设置", 22).
			Size(iconHit, iconHit).Padding(space.None, space.Px5).
			TextColor(on)
		if gear.Clicked() {
			s.full.settings = !s.full.settings
			if s.full.settings {
				// Biu 那边开着设置就把藏界面的钟拆掉（`scheduleHideUi` 第一行
				// `if (isSettingsOpen) return`）：面板开着的时候把它收走，用户
				// 一个字都还没改就成了「界面不见了」。
				s.disarmFullHide()
			}
		}
		// ⚠️ **自己拼面板**（`PopoverBase`）而不是框架那个 `ui.Popover`：框架
		// 那一版把面板钉在锚点（这枚键的**点击区**）的左沿，而我们要的是面板的
		// 左沿和**图形**的左沿对齐（见 `fullSettingsAlignPad`）。
		//
		// 拼面板的那一支在 `fullplayer_settings.go`（面板的**外观**和**内容**
		// 都在那儿，而自己拼的面板必须和 `popupPanel` 那个出口在同一个文件里——
		// `popup_test.go` 盯着这一条）。
		s.viewFullSettingsPopover(c, gear)
	})

	// 关掉的那一下要把三秒重新算起。
	//
	// ⚠️ 不重排的话症状是「面板一关，界面瞬间就没了」：开着设置时那个钟被拆掉
	// 了（`armFullHide` 排的），而 `lastMove` 还停在面板打开之前——于是
	// `fullHidden` 立刻算出「已经三秒没动」，面板还在的那最后半秒里界面先没了。
	// 而 Biu 那边是「关掉设置面板之后用户还能看着界面调频谱」，不是「一松手就黑」。
	if wasOpen && !s.full.settings {
		s.armFullHide()
	}
}

// fullTopBarPadLeft 是满屏顶栏左边让出多少。
//
// `TitleBar().Left` 是系统控件在左边占掉的宽度（macOS 的红黄绿那一簇）。满屏把
// 普通顶栏整条盖住了，所以这一条**必须自己让**：不写死 `fullBarPadX` 的话，
// 「关闭全屏」那枚键正好压在黄灯和绿灯底下（实测那会儿还是文字键，按钮 x∈[39,91]，红黄绿
// x∈[14,66]）。
//
// 单独拎成一个函数是为了能测：无头 Tester 里 `bar.Left` 恒为 0（那里没有标题栏），
// 量真实窗口那一半得开真窗口，而这一个函数把「跟着 TitleBar 走」这件事钉住了。
func fullTopBarPadLeft(bar ui.TitleBar) float32 { return bar.Left + fullTopPadX }

// fullTitleMaxWidth 是满屏那个歌名最多能有多宽。
//
// 上游限了「标题最多占五分之二」（`max-w-2/5`），照抄。窗口越宽，标题能占的
// 绝对宽度就越大，但**比例**是死的（2/5）。
func fullTitleMaxWidth(c *ui.Context) float32 {
	winW, _ := c.Size()
	return winW * 0.4
}

// ---- 中间那一块：封面（左边）与歌词（右边） ----

// fullStagePadX 是满屏那两栏各自的**左右留白**。
//
// ⚠️ **48 是上游的 `px-12`**：封面那一栏是 `flex h-full w-full items-center px-12`
// （`full-screen-player/index.tsx:331`），歌词那一栏是 `px-12 py-24`
// （`:357`）。两处共用一个数——上游那两行写的是同一个类。
const fullStagePadX = space.Px48

// fullCoverColRatio 是左边那一列（封面）占窗口宽的几成。
//
// ⚠️ **0.5 是上游那个数**：`full-screen-player/index.tsx:331-332` 里封面栏和
// 歌词栏是两个 `w-full` 的兄弟，在 flex 行里**各占一半**（1200 宽的窗口上
// 封面栏 0..600、歌词栏 600..1200）。
//
// 之前这里是 0.42，理由写的是「怕 520 的封面被歌词挤掉」——而**上游那个尺寸式子
// 在 1200×800 上算出来就是 384**（`fullCoverSize`），压根挤不掉：上游敢 50/50
// 是因为它把歌词栏压到 504 净宽也没关系（票 #166 / 报告 S-22）。
const fullCoverColRatio = 0.5

// fullStageRect 是中间那一块的区域：**整窗**。
//
// ⚠️ **上游那两栏是 `h-full`**（`full-screen-player/index.tsx:330-331`），也就是
// 从窗口最上头贯到底——所以封面是**在整窗里垂直居中**的（1200×800 上是
// y=256..543），而歌词那一栏的内容盒是 `py-24` 去掉之后的那一段（96..704），
// **正在唱的那一句落在窗口的正中**（400）。
//
// ⚠️ **底下那一条给频谱留了位置**这件事因此变了：频谱是画在底栏正上方那一段
// 的（`viewFullSpectrum` 的 `Bottom(...)`，656..696），而歌词内容盒一直铺到
// 704——两者在 x=648..900 那一段重叠。**那一段正好是歌词的淡出带**
// （上游那条遮罩 88% 起淡，655..704），所以字已经淡没了，频谱穿过去看不出来。
// 上游是同一个位置关系（它的频谱 670..710、歌词内容盒 96..704）。
func fullStageRect(c *ui.Context) ui.Rect {
	winW, winH := c.Size()
	return ui.Rect{X: 0, Y: 0, W: winW, H: winH}
}

// fullWindowRect 是**整窗**。
//
// ⚠️ **它只给「歌词关着」那一条路径用**：那一路径上封面必须和这一票之前
// **逐像素一样**（整窗居中）。现在它和 `fullStageRect` 是同一块——两栏是
// `h-full`（上游那样），歌词那一栏的内容盒靠 `py-24` 收进去，而封面在整窗里
// 居中。
func fullWindowRect(c *ui.Context) ui.Rect {
	winW, winH := c.Size()
	return ui.Rect{X: 0, Y: 0, W: winW, H: winH}
}

// fullLyricsPartsGap 是歌词那一列与分集面板之间留的缝。
//
// ⚠️ **分集面板是贴右边的一竖条**（280 宽，`viewFullPartsPanel` 的
// `Absolute().Right(0)`），而它开着的时候盖在歌词那一列上面——歌词从中间被
// 切断（评审抓到的那一条）。所以歌词那一列的右边界要**让开它**：让开量是
// 面板宽减去这一份缝。
const fullLyricsPartsGap = space.Px24

// viewFullStage 画中间那一块：左边封面，右边歌词。
//
// # 分不分栏只看「显示歌词」那一个开关
//
// ⚠️ **不看这首歌有没有歌词**：歌词是**异步**到的（缓存读回来、`PutLyrics`
// 都在后台），而布局跟着歌词的有无跳来跳去，用户看着的是封面自己动了一下
// ——而他什么都没做。开关是用户自己按的，布局跟着它走才是可预期的。
//
// # 歌词关着的时候，这一块就是原来那一块
//
// ⚠️ **封面在整窗里居中，和这一票之前逐像素一样**（`fullWindowRect`）——不是
// 在中间那一段里居中：那一段的上边界是顶栏（64），拿它居中封面会**上移 40
// 像素**（评审抓到的那一条：注释写着「一模一样」，而画面上不是）。
//
// 歌词开着的时候封面挪到左边那一列（**靠那一栏的右沿**，上游 `justify-end`），
// 右边让给歌词。
//
// # 三件事各自独立
//
//	歌词关着   封面在**整窗**里居中（和这一票之前逐像素一样）
//	封面关着   左边那一列**不留**：歌词从窗口左边开始（那一列空着，歌词被挤到
//	          右半边，看着像少画了一半）
//	分集面板开着 歌词那一列的右边界**让开面板**
//
// ⚠️ **让开与否只看「面板开着没有」，不看界面藏没藏**（评审抓到的那一条）：
// 界面藏起来的时候面板不画（`viewFullParts` 见到 hidden 就返回），而歌词**还
// 亮着**（歌词是内容不是控件）——那时候把歌词列改回整宽，整列歌词就在界面淡出
// 的那一刻**横向跳 128 像素**（实测：第一句从 x=681 滑到 x=809），指针一动界面
// 回来它又滑回去。宁可右边空一块（那本来就是面板的地盘），也不能让它跳。
func (s *Shell) viewFullStage(c *ui.Context, st app.State, hidden bool) {
	prefs := s.app.FullPlayerPrefs()
	if !prefs.ShowLyrics {
		s.viewFullCover(c, st, hidden, fullWindowRect(c), false)
		return
	}
	stage := fullStageRect(c)
	// 左边那一列：封面关着就不留（否则左边半边的窗宽整块空着）。
	colW := float32(0)
	if prefs.ShowCover {
		colW = stage.W * fullCoverColRatio
	}
	// 歌词那一列的右边界：分集面板开着的时候让开它（面板是浮在歌词上面的，
	// 不让开的话歌词从中间被切断）。
	right := stage.X + stage.W
	if s.full.partsOpen {
		right = stage.X + stage.W - fullPartsWidth + fullLyricsPartsGap
	}
	if prefs.ShowCover {
		// ⚠️ **靠那一栏的右沿**（上游 `justify-end`）：封面右边留 48（上游
		// `px-12`），于是 1200 宽的窗口上封面是 x=168..552——上游是 168..551。
		s.viewFullCover(c, st, hidden,
			ui.Rect{X: stage.X, Y: stage.Y, W: colW, H: stage.H}, true)
	}
	s.viewFullLyrics(c, st, ui.Rect{
		X: stage.X + colW, Y: stage.Y, W: right - (stage.X + colW), H: stage.H,
	}, hidden)
}

// viewFullCover 是满屏那张封面——**和播放条上那一张是同一个元素**。
//
// area 是它待的那一块：歌词开着的时候是左边那一列，关着的时候是整窗。
//
// rightAlign 是「靠那一块的右沿」还是「在那一块里居中」（上游
// `showLyrics ? "justify-end" : "justify-center"`，`index.tsx:332`）。
//
// 🔴 这里的 `Match` 就是 SwiftUI `matchedGeometryEffect` 的等价物，见 coverMatch。
// 它和 `viewCover`（播放条那一张）轮流带同一个 `coverTransition(id)`。
//
// ⚠️ **外面新套的那一层 Box 是长按三连要的**（见 like.go）：进度环要画在封面
// **外面**一圈，而 `imgcache.View` 自己带 `Clip()`（圆角裁剪），环放进去会被
// 裁掉。所以：定位和尺寸搬到外面这层 Box 上，里面那张图照旧带着转场。
//
// ⚠️ **界面藏起来时光标要跟着没，而这一层得自己说**：框架认光标是从指针底下
// 最里层那个元素开始往上找的，而**遇到的第一个「接点击的」元素就停**（认成
// 默认箭头，`runtime.go` 的 `updateCursor`）。这一层为了长按调了 `Pressed()`，
// 于是它是接点击的那一个——指针落在封面正中时 `CursorNone` 根本轮不到探针。
// 实测：满屏指针放在封面正中（600,400）推进 3 秒，光标是箭头而不是 none。
// 照抄探针那一支（`viewFullPlayer` 里 `probe.Cursor(ui.CursorNone)`）就对了。
func (s *Shell) viewFullCover(c *ui.Context, st app.State, hidden bool, area ui.Rect, rightAlign bool) {
	// 「显示封面」关掉就一个字都不建（Biu 的 `{!isLocal && showCover && <Image …>}`
	// ——`index.tsx:328`）。本地那一半的判据（`!isLocal`）不在这一层：本地文件
	// 没有封面时画的是占位图，关掉它照样是「中间不摆东西」，和上游同一个结果。
	if !s.app.FullPlayerPrefs().ShowCover {
		return
	}
	// 靠右沿摆的时候，封面得**装得进那一栏减去左右留白**（上游那一栏是
	// `px-12`，封面窄窗口里被 flex 压窄的也是压到内容盒那么宽）。
	avail := area.W
	if rightAlign {
		avail = area.W - 2*fullStagePadX
	}
	w, h := fullCoverSize(c, avail)
	// ⚠️ **自己算位置**，不要用 `Absolute().Center()`：那个 `Center` 是「把**子**
	// 元素沿着主轴居中」（`mygo/ui/api_gen.go:3006`），不是「把这个元素摆到中间」。
	// 这么写的后果是外层那个 Box 宽高就是封面本身、贴在**它待的那一块的最左边**
	// （x=0），而转场的终点也跟着错。
	x := area.X + (area.W-w)/2
	if rightAlign {
		x = area.X + area.W - fullStagePadX - w
	}
	box := ui.Box(c).Key("full-cover").
		Absolute().Left(x).Top(area.Y+(area.H-h)/2).Size(w, h)
	if hidden {
		box = box.Cursor(ui.CursorNone)
	}
	box.Children(func() {
		imgcache.View(c, s.images, st.CoverURL, w, h).
			Label("当前封面").
			Transition(coverTransition(coverIdentity(s, st)))

		// 长按 2.5 秒 = 一键三连。满屏上**没有**别的赞入口（照抄 Biu：它那边
		// 满屏那张封面是 `pointer-events-none`，三连只在播放条上）——我们把这一层
		// 接进来是因为 SPEC 需求 69 说的是「长按封面」，而满屏上只有这一张封面。
		s.viewLikeRing(c, box.Pressed())
	})

	// 🔴 **每个长按目标都要在这里问一句 `swallowClick`**（播放条那一张在
	// `viewCover` 里问，问完还要看返回值决定要不要做自己的事；这儿没有自己的事，
	// 只是把长按之后松手补发的那一次 click 消费掉）。
	//
	// 第一版这里**漏了**，后果是：满屏上长按三连之后那一次 click 落在这一层而没人
	// 问，于是标记一路活到播放条，把用户接下来的第一次点击吃掉——关掉满屏点封面要点
	// 第二下才开全屏。它自己永远好不了：三连成功之后 `Liked` 变真、`likeArmed` 变
	// false，清标记的那一支再也进不去（`trackLike` 那段有完整的一段）。
	if box.Clicked() {
		s.like.swallowClick()
	}
}

// coverIdentity 是共享元素这一次的身份：当前这一首的判重标识。
//
// 认不出是哪一首时（Identity 返回空串，见它的注释）退回按播放状态拼一个。
// **不能留空**：空串会让「没建」和「建了但身份是空的」长得一样，而转场认的是
// 值的相等——一个空串能和任何一首的空串对上，转场就变成了「从随便哪张封面飞过来」。
func coverIdentity(s *Shell, st app.State) string {
	if cur, ok := s.app.CurrentItem(); ok {
		if id := cur.Identity(); id != "" {
			return id
		}
	}
	return "state:" + st.Path
}

// fullCoverSize 是满屏封面的宽高（4:3）。
//
// Biu：`Math.max(260, Math.min(windowWidth * 0.7, windowHeight * 0.48, 520))`
// 然后 `coverHeight = coverWidth * 0.75`。
//
// ⚠️ **maxW 是「它待的那一块有多宽」**：歌词开着的时候封面只占左边那一列，
// 而上游那个式子算出来的宽度（最多 520）可能比那一列还宽——那就压到歌词上去了。
// 夹一下之后窄窗口里的封面跟着那一列一起变小，而宽高的比例不变。
func fullCoverSize(c *ui.Context, maxW float32) (float32, float32) {
	winW, winH := c.Size()
	w := min(winW*0.7, winH*0.48)
	w = min(max(w, fullMinCoverWidth), fullMaxCoverWidth)
	if maxW > 0 {
		w = min(w, maxW)
	}
	return w, w * fullCoverAspect
}

// ---- 底下那一条 ----

// viewFullControls 是底下那一条：进度 + 播放模式/上一首/播放/下一首/播放列表。
//
// 藏起来的时候整条**滑出去**（Biu 的 `translate-y-full opacity-0`），频谱跟着它
// 走（`index.tsx:370` 的 `bottom: isUiVisible ? controlsHeight + 12 : 24`）。
//
// ## 淡入要和顶栏**一模一样**
//
// 顶栏是 `Opacity(bar.AnimateTo("full-top:o", shown, ui.Smooth))`——不透明度是
// **动画**出来的。这一条原来写的是 `Opacity(shown)`，那是**一下子**变 0 或 1 的。
// 于是界面藏了三秒、指针一动之后：顶栏慢慢淡回来，底下这一整条「啪」地一下出现
// ——同一个界面里两种响应。框架现成的动画写法照抄顶栏那一行就行。
func (s *Shell) viewFullControls(c *ui.Context, st app.State, hidden bool) {
	shown := fullShown(hidden)
	on, dim := s.fullForeground(c)
	bar := ui.Row(c).Key("full-controls").
		Absolute().Left(0).Right(0).Bottom(0).Height(fullControlsHeight).
		Justify(ui.Center).AlignItems(ui.End)
	bar = bar.Opacity(bar.AnimateTo("full-controls:o", shown, ui.Smooth))
	// ⚠️ 位移**不跟着淡入一起动**（顶栏也是这么写的）：藏起来的时候这一条要真的
	// 移出窗口，不然停在窗口底下那 108 像素里的控件仍然接得到点击。
	bar = bar.Translate(0, (1-shown)*fullControlsHeight)

	bar.Children(func() {
		// 上游那一层：`mx-auto mb-4 flex w-full max-w-6xl flex-col items-center
		// gap-2 px-12`（`index.tsx:394`）。三种留白各自管一件事：
		//
		//	px-12       左右 48（`fullBottomPadX`）
		//	mb-4        离底 16（`fullBottomMargin`）
		//	max-w-6xl   最宽 1152，窗口再宽也居中、不摊到两头（`fullBottomMaxW`）
		ui.Column(c).FillWidth().MaxWidth(fullBottomMaxW).
			Padding(space.None, fullBottomPadX, fullBottomMargin, fullBottomPadX).
			Gap(fullBottomGapY).AlignItems(ui.Center).Children(func() {
			s.viewFullProgress(c, st, dim)
			// Biu 那一排是 `MusicPlayMode + MusicPlayControl + OpenPlaylistDrawerButton`
			//（`index.tsx:396-399`），三组之间 `space-x-4`（16），播放控制那一组
			// 内部 `space-x-6`（24，`music-play-control/index.tsx:18`）。
			//
			// ⚠️ **播放列表那一枚以前没摆**，当时的理由写的是「抽屉画在主布局里，
			// 满屏浮层压在它上面，按了什么也不会发生」——那说的是**画在哪一层**，
			// 不是这件事办不到。现在满屏开着的时候抽屉改由这一层画（见
			// `viewFullPlayer` 末尾），于是这一枚真的开得出来。
			//
			// 字色统一给 `on`：这一条压在糊过的封面上，浅色主题下继承来的
			// `t.Text` 是黑的，模式/上下首/队列那四枚会一起消失在背景里。
			// 哪一档配哪一张由 `transportIcon` / `playModeIcon` 说了算，这一行
			// 不自己决定（守卫盯着）。
			ui.Row(c).FillWidth().TextColor(on).Gap(space.Px16).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				s.viewPlayMode(c, st)
				ui.Row(c).Gap(space.Px24).AlignItems(ui.Center).Children(func() {
					s.viewPrevButton(c)
					// 提示走 tooltip.go（时延照上游），不是框架那枚固定的 600ms。
					//
					// 满屏上这一枚和播放条上是同一枚实心强调色圆（规范 §5：64×64，图形 28）。
					// 实心圆的底是强调色、字是 `AccentText`，压在糊过的封面上也读得出来——
					// 旧的「悬停变主色」那一档按应用底色算，在封面上会掉到 1.0:1，所以旧版
					// 才特意不走 `iconButtonPlay`；现在这一枚自己带底，没有那个问题。
					glyph := iconPlayFill
					if st.Playing {
						glyph = iconPauseFill
					}
					if s.withTip(c, iconButtonPlay(c, glyph, transportLabel(st.Playing),
						fullPlayHit, fullPlayGlyph, !st.Playing && !st.HasTrack),
						transportLabel(st.Playing)).Clicked() {
						s.app.Toggle()
					}
					s.viewNextButton(c)
				})
				// 同一处：队列开关的提示也走 tooltip.go。
				if s.withTip(c, iconButton(c, iconQueue, "播放列表", iconMd).
					Size(iconHitMd, iconHitMd).Padding(space.None, space.Px7), drawerTip(s.drawerOpen)).Clicked() {
					s.drawerOpen = !s.drawerOpen
				}
			})
		})
	})
}

// viewFullProgress 是满屏上那一行进度。
//
// ⚠️ 用的还是 `Pressed()` 而不是 `Dragging()`（理由见 playbar.go 的 viewProgress）。
// 这一条另有一份自己的进度句柄（`fullScrub`），不能和播放条上那条共用——两处会
// 各自按住、各自 seek，拖一条的时候另一条跟着跳。
//
// ⚠️ 滑块**不是** `ui.Slider`，是 `knobSlider`（见 knobslider.go）：框架那一颗
// 旋钮被按住时会褪成一片黑边透镜，在满屏这种永远深着的底上等于消失——用户
// 按住旋钮拖，旋钮反而不见了。
func (s *Shell) viewFullProgress(c *ui.Context, st app.State, dim ui.Color) {
	known := st.Duration > 0
	ui.Row(c).FillWidth().Gap(space.Px10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, clockOrDash(st.Position)).FontSize(scale.Font(c, scale.Size12)).TextColor(dim).
			MinWidth(48).MaxLines(1)
		sl := knobSlider(c, &s.fullScrub, 0, 1).Grow(1).Label("全屏播放进度").Disabled(!known)
		switch held := sl.Pressed(); {
		case held && !s.fullHeld:
			s.fullHeld = true
			s.app.ScrubStart()
		case !held && s.fullHeld:
			s.fullHeld = false
			s.app.ScrubEnd(s.fullScrub)
		case !s.fullHeld && known:
			s.fullScrub = float64(st.Position) / float64(st.Duration)
		}
		total := clockOrDash(st.Duration)
		ui.Text(c, total).FontSize(scale.Font(c, scale.Size12)).TextColor(dim).MinWidth(48).MaxLines(1)
	})
}

// ---- 指针与那个三秒 ----

// syncFullPointer 认出「指针动了没有」，动了就把藏界面的钟重排一个。
//
// **不要在这里写别的**：定时器的回调跑在后台（真时钟）或测试 goroutine（假时钟）
// 上，而界面状态是主线程单点写入的（铁律一）。回调只调 `wake.invalidate`，
// 到没到点由 `fullHidden` 现算。
func (s *Shell) syncFullPointer(probe ui.Element) {
	x, y, over := probe.PointerPosition()
	moved := over && (!s.full.mouseOK || x != s.full.mouseX || y != s.full.mouseY)
	s.full.mouseX, s.full.mouseY, s.full.mouseOK = x, y, over
	if moved {
		s.armFullHide()
	}
}

// armFullHide 记下「刚刚动过」并排一个三秒后叫醒界面的钟。
func (s *Shell) armFullHide() {
	s.full.lastMove = s.clock.Now()
	s.disarmFullHide()
	s.full.hideStop = s.clock.EveryFunc(fullHideDelay, s.wake.invalidate)
}

// disarmFullHide 停掉那个钟。
func (s *Shell) disarmFullHide() {
	if s.full.hideStop != nil {
		s.full.hideStop()
		s.full.hideStop = nil
	}
}

// fullHidden 问「界面现在该藏起来没有」。
//
// 开着偏好面板的时候不藏（Biu 的 `if (isSettingsOpen) return`）。
func (s *Shell) fullHidden() bool {
	if s.full.settings {
		return false
	}
	if s.full.lastMove.IsZero() {
		return false
	}
	return s.clock.Now().Sub(s.full.lastMove) >= fullHideDelay
}

// fullShown 是「看得见」的比例：0 全藏、1 全显。
func fullShown(hidden bool) float32 {
	if hidden {
		return 0
	}
	return 1
}

// ---- 分集面板 ----

// viewFullParts 是右边那个把手和它开出来的分集面板。
//
// 界面藏起来的时候把手也藏起来，面板开着的时候把手让位。
//
// 🔴 **把手在「还没问到」和「问失败」时也得摆着。**
//
// Biu 的条件是 `playItem.hasMultiPart`（`index.tsx:404`），而那是播放项自带的
// 字段——我们没有，得问一次才知道。所以第一版照抄成 `len(parts) > 1`，后果是：
// 一次 403 之后列表空 → 把手不摆 → 面板进不去 → 那句「问不出分集：…」
// **在真机上是死的**（只有测试里直接写 `s.full.partsOpen = true` 才看得到）。
//
// 现在：`问到多于一条`、或者`一条都还没问到`（正在问 / 问失败）时都摆。
// 只在**确知只有一条**的时候收起来——那时候确实没有「选」的意思。
func (s *Shell) viewFullParts(c *ui.Context, hidden bool) {
	parts := s.app.Parts()
	cur, _ := s.app.CurrentItem()
	s.full.partsRows = partRowsFor(parts, cur)

	if hidden {
		return
	}
	if s.full.partsOpen {
		s.viewFullPartsPanel(c)
		return
	}
	if len(parts) > 1 || (len(parts) == 0 && s.app.PartsWorthAsking()) {
		_, winH := c.Size()
		_, dim := s.fullForeground(c)
		// 把手是一枚 `RiArrowLeftSLine` 24 的图标键（`page-list.tsx` 那一支，
		// `full-screen-player/index.tsx:406-415`）——26×96 的那条竖把手是它的
		// **框**，不是按钮的样子：没有描边、没有底色，只有悬停时那层淡底。
		// 字色同顶栏，理由也同顶栏（压在压暗过的封面上）。
		// 也不给 `Tooltip`：界面一藏，这条把手跟着不画，悬空的那句提示会留下来。
		tab := iconButton(c, iconSideCollapse, "显示分集列表", 24).
			Absolute().Right(0).Top(winH/2-fullPartsTabH/2).
			Width(fullPartsTabW).Height(fullPartsTabH).Padding(space.None, space.Px1).
			TextColor(dim)
		if tab.Clicked() {
			s.full.partsOpen = true
		}
	}
}

// partRowsFor 是分集列表这一帧要画的那几行。
//
// ⚠️ 铁律四：行的 key 是**这一集的 cid**，不是行号（见 queue_drawer.go 的
// queueRowKey）。同一个 cid 只会有一行，所以它足够唯一；而拿行号当身份的话，
// 往前插一行会让后面所有行的「正在放」整体错配。
//
// 前缀是 `part:` 而不是 `full-part:`：两个入口在**不同的父容器**里（满屏面板和
// 播放条浮层），key 本来就不会撞，而名字得跟着现在真正的用处走（issue #91 的
// 用户故事 37）。
func partRowsFor(parts []bili.VideoPart, cur app.Item) []partRow {
	rows := make([]partRow, 0, len(parts))
	for _, p := range parts {
		rows = append(rows, partRow{
			key:    "part:" + p.Cid,
			cid:    p.Cid,
			page:   p.Page,
			title:  partTitle(p),
			active: p.Cid == cur.Cid,
		})
	}
	return rows
}

// partTitle 是一行显示的字。
//
// 分 P 的标题是空的（B 站有时候不给）时说「第 N 集」，不是把行留成空白——空标题
// 在列表里和「这一行没画出来」长得一模一样。
func partTitle(p bili.VideoPart) string {
	if p.Part != "" {
		return p.Part
	}
	return "第 " + strconv.Itoa(p.Page) + " 集"
}

// viewFullPartsPanel 是开出来的分集面板。
func (s *Shell) viewFullPartsPanel(c *ui.Context) {
	t := c.Theme()
	_, winH := c.Size()
	h := min(winH*0.6, fullPartsHeight)
	// 玻璃那一层：**票面「毛玻璃的 Tint 配方」那四样**，一样都不少。
	//
	// ⚠️ 原来这一格是 `ui.RGBA(255, 255, 255, 0.12)`——一层十二个百分点的白，
	// 压在糊过的封面上，**白字读不出来**（封面一亮，字就没了）。
	//
	// 那四样各管一件事：`Tint` 管「字读不读得清」（掺 96% 背景色，底下透出
	// 的是模糊的轮廓，不是内容的细节），`TextColor` 管「文字跟不跟主题走」，
	// `Border` 管「玻璃的边在浅底上有没有」，`Shadow` 管「它浮不浮得起来」。
	//
	// ⚠️ **影的值从 `widgets/shadow` 取**（`shadow.Sheet`）：值只写一处
	// （DESIGN.md §四·五，票 #239 把这一处收进去了）。
	panel := shadow.Sheet(ui.Column(c).Key("full-parts-panel").
		Absolute().Right(0).Top(winH/2-h/2).
		Width(fullPartsWidth).Height(h).
		Radius(fullPartsPanelRadius).Role(ui.RoleDialog).Label("分集列表").
		Material(glass.Glass{Tint: t.Background.Alpha(0.96)}).
		TextColor(t.Text).
		Border(1, t.Background.Mix(t.Text, 0.25)),
		t)

	panel.Children(func() {
		ui.Row(c).FillWidth().Padding(space.Px10, space.Px12).Gap(space.Px8).AlignItems(ui.Center).Children(func() {
			// 字色跟着面板走（`TextColor(t.Text)`），这里不再自己定。
			// ⚠️ **字重不写**（400）：面板标题那一档是 400（DESIGN.md §二），
			// 层次靠字号拉开，不靠重量。
			ui.Text(c, "分集").FontSize(scale.Font(c, scale.Size14)).Grow(1)
			// 上游是一枚 `RiArrowRightSLine` 16 的扁平图标键（`page-list.tsx:47`）。
			// 字色不用给：面板自己 `TextColor(t.Text)`，图标跟着它走。
			if s.withTip(c, iconButton(c, iconSideExpand, "收起分集", 16).
				Size(24, 24).Padding(space.None, space.Px4), "收起分集").Clicked() {
				s.full.partsOpen = false
			}
		})
		switch {
		case len(s.full.partsRows) == 0:
			// 问不到就说问不到，并说清为什么（Biu 那边是一个空列表）。
			why := "正在问这一首有哪几集"
			if err := s.app.PartsErr(); err != nil {
				why = "问不出分集：" + err.Error()
			}
			ui.Text(c, why).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted).Padding(space.Px12)
		default:
			s.viewFullPartsList(c)
		}
	})
}

// viewFullPartsList 是面板里那一列。
func (s *Shell) viewFullPartsList(c *ui.Context) {
	t := c.Theme()
	rows := s.full.partsRows
	// ⚠️ **身份是预先装箱好的那一份**（`s.full.partsRowKeys`）：理由见
	// `fullplayer.go` 里那个字段的注释（票 #313）。
	s.full.partsList.Key = func(i int) any {
		return s.full.partsRowKeys.At(i, rows[i].key, rowkey.Identity)
	}
	s.full.partsList.Label = func(i int) string { return rows[i].title }

	partsList := ui.List(c, &s.full.partsList, len(rows), func(i int) {
		r := rows[i]
		row := ui.Column(c).Key(r.key).Grow(1).Height(fullPartRowHeight).
			PaddingX(space.Px12).Radius(t.Radius)
		switch {
		case r.active:
			// ⚠️ **这一处和上游还差着一个数**（票 #323 只管播放条上那个分集抽屉）：
			// 上游是 `data-[active=true]:bg-primary/20`——**主色 20% 那一层淡底**；
			// 我们这里是**一层中性色**（跟着主题的字色走，不是写死白）：面板那一层
			// 是玻璃，玻璃在深浅两套下是不同的底，白底和黑底要反过来。
			//
			// 播放条上那个分集抽屉已经按上游改成淡主色底了（`partdrawer.go` 的
			// `viewPartRow`，配方见 `playingrow.go`）；这一处还欠着——**别把这两处
			// 当成同一档**。
			row = row.Background(t.Text.Alpha(0.22))
		case row.Hovered():
			row = row.Background(t.Text.Alpha(0.10))
		}
		row.Children(func() {
			// 字色跟着面板走（`TextColor(t.Text)`），这里不再自己定。
			ui.Text(c, r.title).FontSize(scale.Font(c, scale.Size13)).SingleLine()
		})
		if row.Clicked() {
			// 选完就收：Biu 的 PageList `onPressItem={onClose}`。
			s.full.partsOpen = false
			if err := s.app.PlayPart(r.cid); err != nil {
				s.addToast(err.Error(), notice.Danger)
			}
		}
	}).Grow(1)
	s.trackScrollbar(partsList)
}
