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
// （`mygo/ui/transition.go:412` 的 `r.fresh = r.laid+1 != rt.frame`）。播放条那个
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
	"fmt"
	"strconv"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/imgcache"
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
//	控制条高 80（offsetHeight 兜底那个值）                  index.tsx:64
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
	fullSpectrumGapY   = 12
	fullSpectrumEdge   = 24
	fullControlsHeight = 80

	// 满屏上播放/暂停那一枚的图标边长与点击区。
	//
	// ⚠️ 它比播放条上那一枚（`iconPlayBtn` 30 / `iconHitPlay` 40）大一号，因为
	// 满屏这一行本来就以它为主角；而 `fullControlsHeight` 只有 80，再大就被切。
	// 46 + 上下各 12 的内边距 = 70，落在 80 里还留 10。
	//
	// 🔴 **以前这里是 `ui.PrimaryButton(c, "暂停").Width(96)`** —— 一个文字按钮，
	// 而两边的上一首/下一首共用播放条那几个（已经是图标了）。那一行于是变成
	// 「图标 — 文字 — 图标」**半新半旧**：满屏是用户盯着看最久的一屏，中间夹一个
	// 绿色文字块和两个灰色箭头，怎么看都不像一套。
	fullPlayGlyph     = 34
	fullPlayHit       = 46
	fullBarHeight     = 56
	fullBarPadX       = 24
	fullBarPadY       = 12
	fullPartsWidth    = 280
	fullPartsHeight   = 420
	fullPartsTabW     = 26
	fullPartsTabH     = 96
	fullPartRowHeight = 32
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
// ⚠️ **0.60 不是随手挑的**：最坏的一档（封面是纯白）底色是 255×0.40 = 102。
// 20px 粗体的歌名（白 92%）压在它上面是 **5.17:1**——WCAG AA 对普通文字的门槛是
// 4.5:1（20px 粗体算「大字」，那一档只要 3:1，这里取的是更严的那一档）。
// 0.55 的时候是 4.31:1，差 0.19。
//
// ⚠️ 那些 12px 的小字（时间标签，白 62%）是 3.32:1（0.55 的时候 2.85:1）——它们
// 是**次要**的字，本来就比歌名淡一档；要让它们也到 4.5:1，压暗得加到 0.72，而
// 那会让背景暗到看不出毛玻璃。那是**可及性**那一票的事（issue #139「接上系统的高
// 对比度」：系统在「增加对比度」下另算一套颜色）。
var fullScrim = ui.RGBA(0, 0, 0, 0.60)

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

// fullOnColor / fullDimColor 是压在背景上的两级前景色（白 / 白一点七折）。
var (
	fullOnColor  = ui.RGBA(255, 255, 255, 0.92)
	fullDimColor = ui.RGBA(255, 255, 255, 0.62)
)

// coverMatch 是「封面在两个地方是同一个东西」的身份。
//
// 🔴 **用自己定义的类型**（框架文档明说：「Use a type of your own for the
// identity, so that it is no other's」）。值是当前这一首的判重标识——换一首歌就是
// 另一个身份，于是转场不会把 A 的封面拉到 B 的位置上。
//
// 它就是 SwiftUI `matchedGeometryEffect` 在这一层的等价物：框架 `ElementTransition`
// 的 `Match` 字段自己的注释就是这么写的（`mygo/ui/transition.go:42-46`：
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

	// bg 是背景那一层的画布：封面糊过一次画进去，稳态下每一帧只是一张图
	// （见 viewFullBackdropCanvas 为什么是画布而不是一层材质）。
	bg *ui.Canvas
	// bgW/bgH 是这张画布的尺寸（窗口改了尺寸要重建）。
	bgW, bgH int
	// bgKey 是这张画布画的是哪一张封面（地址 + 位图 + 尺寸）。变了才重画。
	bgKey string
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
func (s *Shell) viewFullPlayer(c *ui.Context) {
	if !s.full.open {
		return
	}
	st := s.app.Snapshot()
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
			s.viewFullCover(c, st, hidden)
			s.viewFullSpectrum(c, st, hidden)
			s.viewFullControls(c, st, hidden)
			s.viewFullParts(c, hidden)
		})

		if probe.OverlayShortcut(0, ui.KeyEscape) {
			s.closeFullPlayer()
		}
	})
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
			ui.Column(c).Key("full-empty").
				Absolute().Left(0).Top(0).Right(0).Bottom(0).
				Center().Gap(6).Children(func() {
				// ⚠️ **这两行是故意的例外，不走 `emptyCopy`**：满屏这一屏压在
				// 一块有底色的背板上，所以主次色是那块背板的 on / dim
				// （`fullOnColor` / `fullDimColor`），而不是主题的 Text /
				// TextMuted——用主题那两档的话，在浅色封面铺满的屏上这两行
				// 会糊进去。字号仍然是 15 / 12，和别处一样。
				ui.Text(c, fullNothing.Title).FontSize(15).TextColor(fullOnColor)
				ui.Text(c, fullNothing.Hint).FontSize(12).TextColor(fullDimColor)
			})
		})

		if probe.OverlayShortcut(0, ui.KeyEscape) {
			s.closeFullPlayer()
		}
	})
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
		s.addToast("还没有在放什么，先进一首歌")
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
	key := fmt.Sprintf("%s|%p|%dx%d", url, st.Bitmap, cw, ch)
	if s.full.bgKey != key {
		if s.full.bg == nil || s.full.bgW != cw || s.full.bgH != ch {
			s.full.bg = ui.NewCanvas(cw, ch)
			s.full.bgW, s.full.bgH = cw, ch
		}
		s.full.bgKey, s.full.bgDirty = key, true
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
// 而满屏把普通顶栏整条盖住了——于是「关闭全屏」的前两个字正好被黄灯和绿灯压住
// （实测按钮框 x∈[39,91]，红黄绿整簇 x∈[14,66]）。做法照 navbar.go 那一处：
// `c.TitleBar()` 给的 `bar.Left` 就是左边让出的量。
//
// ⚠️ 同一条顶栏还要能拖窗口：浮层盖住整窗，普通顶栏上那两处 `DragWindow`
// （navbar.go）够不着了。
func (s *Shell) viewFullTopBar(c *ui.Context, st app.State, hidden bool) {
	titleBar := c.TitleBar()

	shown := fullShown(hidden)
	bar := ui.Row(c).Key("full-top").
		Absolute().Left(0).Right(0).Top(0).Height(max(titleBar.Height, fullBarHeight)).
		Padding(fullBarPadY, fullBarPadX, fullBarPadY, fullTopBarPadLeft(titleBar)).
		Gap(12).AlignItems(ui.Center).DragWindow()
	bar = bar.Opacity(bar.AnimateTo("full-top:o", shown, ui.Smooth))
	// ⚠️ 藏起来的时候要**移出窗口**，不只是 Opacity(0)。不移的话那两个看不见的
	// 按钮仍然接点击：停在窗口顶部点一下，满屏就被关掉；点右上角会把偏好面板
	// 凭空开出来。底下那一条 controls 用 `Translate` 推出去了，顶栏原来没做。
	//
	// ⚠️ **方向和底下那一条相反**：controls 在底部，「滑出去」是往下（正的 Y）；
	// 顶栏在顶部，「滑出去」是往上（负的 Y）。写正了的话它滑到 y=56——还在窗口
	// 里，按钮照样点得动。
	bar = bar.Translate(0, -(1-shown)*(titleBar.Height+fullBarHeight))

	// ⚠️ 这一帧**进来时**面板是开着没有：`ui.Popover` 处理「点外面 / 按 Esc 关掉」
	// 是在它自己那一趟里做的，进函数时读到的还是上一帧的值。要判断「刚刚被关掉」
	// 就得先把进来时的值记下来（理由同 shell.go 的 syncRoute）。
	wasOpen := s.full.settings

	bar.Children(func() {
		if ui.Button(c, "关闭全屏").FontSize(13).Clicked() {
			s.closeFullPlayer()
		}
		// 歌名：上游是 `text-xl`（20 像素），而且限了宽度（最多占五分之二）。
		//
		// ⚠️ 18 → 20 **差两号**：18 那个是 `text-lg`。宽度那条也照抄（见
		// fullTitleMaxWidth）。
		ui.Text(c, st.Title).FontSize(20).Bold().TextColor(fullOnColor).
			Grow(1).MaxWidth(fullTitleMaxWidth(c)).SingleLine()

		gear := ui.Button(c, "全屏设置").FontSize(13)
		if gear.Clicked() {
			s.full.settings = !s.full.settings
			if s.full.settings {
				// Biu 那边开着设置就把藏界面的钟拆掉（`scheduleHideUi` 第一行
				// `if (isSettingsOpen) return`）：面板开着的时候把它收走，用户
				// 一个字都还没改就成了「界面不见了」。
				s.disarmFullHide()
			}
		}
		ui.Popover(c, gear, &s.full.settings, func() {
			s.viewFullSettings(c)
		})
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
// 「关闭全屏」的前两个字正好压在黄灯和绿灯底下（实测按钮 x∈[39,91]，红黄绿
// x∈[14,66]）。
//
// 单独拎成一个函数是为了能测：无头 Tester 里 `bar.Left` 恒为 0（那里没有标题栏），
// 量真实窗口那一半得开真窗口，而这一个函数把「跟着 TitleBar 走」这件事钉住了。
func fullTopBarPadLeft(bar ui.TitleBar) float32 { return bar.Left + fullBarPadX }

// fullTitleMaxWidth 是满屏那个歌名最多能有多宽。
//
// 上游限了「标题最多占五分之二」（`max-w-2/5`），照抄。窗口越宽，标题能占的
// 绝对宽度就越大，但**比例**是死的（2/5）。
func fullTitleMaxWidth(c *ui.Context) float32 {
	winW, _ := c.Size()
	return winW * 0.4
}

// ---- 封面（共享元素） ----

// viewFullCover 是满屏中间那张封面——**和播放条上那一张是同一个元素**。
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
func (s *Shell) viewFullCover(c *ui.Context, st app.State, hidden bool) {
	winW, winH := c.Size()
	w, h := fullCoverSize(c)
	// ⚠️ **自己算位置**，不要用 `Absolute().Center()`：那个 `Center` 是「把**子**
	// 元素沿着主轴居中」（`mygo/ui/api_gen.go:3006`），不是「把这个元素摆到中间」。
	// 这么写的后果是外层那个 Box 宽高就是封面本身、贴在左边 x=0，
	// 界面上是「封面靠左」，而转场的终点也跟着错。
	box := ui.Box(c).Key("full-cover").
		Absolute().Left((winW-w)/2).Top((winH-h)/2).Size(w, h)
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
func fullCoverSize(c *ui.Context) (float32, float32) {
	winW, winH := c.Size()
	w := min(winW*0.7, winH*0.48)
	w = min(max(w, fullMinCoverWidth), fullMaxCoverWidth)
	return w, w * fullCoverAspect
}

// ---- 底下那一条 ----

// viewFullControls 是底下那一条：进度 + 上一首/播放/下一首。
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
	bar := ui.Row(c).Key("full-controls").
		Absolute().Left(0).Right(0).Bottom(0).Height(fullControlsHeight).
		PaddingY(12).PaddingX(24).Justify(ui.Center).AlignItems(ui.Center)
	bar = bar.Opacity(bar.AnimateTo("full-controls:o", shown, ui.Smooth))
	// ⚠️ 位移**不跟着淡入一起动**（顶栏也是这么写的）：藏起来的时候这一条要真的
	// 移出窗口，不然停在窗口底下那 80 像素里的控件仍然接得到点击。
	bar = bar.Translate(0, (1-shown)*fullControlsHeight)

	bar.Children(func() {
		ui.Column(c).FillWidth().Grow(1).Gap(6).Children(func() {
			s.viewFullProgress(c, st)
			ui.Row(c).FillWidth().Gap(16).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				// Biu 那一排是 `MusicPlayMode + MusicPlayControl + OpenPlaylistDrawerButton`
				//（`index.tsx:396-399`）。两个我们没摆：
				//
				//   播放模式      队列那边只做了顺序，别的模式还没做
				//   播放列表抽屉  抽屉画在**主布局**里（queue_drawer.go），而主布局在
				//                满屏浮层**底下**——摆一个「播放列表」按钮，用户按了
				//                什么也不会发生，而界面上看不出坏了
				// 三枚都是图标（和播放条上共用同一批，见 icons.go）。哪一档配哪一张
				// 由 `transportIcon` 说了算——这一行不自己决定（守卫盯着）。
				s.viewPrevButton(c)
				if iconButtonPrimary(c, transportIcon(st.Playing),
					transportLabel(st.Playing), fullPlayGlyph).
					Size(fullPlayHit, fullPlayHit).Padding(0, 6).
					Tooltip(transportLabel(st.Playing)).
					Disabled(!st.Playing && !st.HasTrack).Clicked() {
					s.app.Toggle()
				}
				s.viewNextButton(c)
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
func (s *Shell) viewFullProgress(c *ui.Context, st app.State) {
	known := st.Duration > 0
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, clock(st.Position)).FontSize(12).TextColor(fullDimColor).Width(48)
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
		total := clock(st.Duration)
		if !known {
			total = "--:--"
		}
		ui.Text(c, total).FontSize(12).TextColor(fullDimColor).Width(48)
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
		tab := ui.Button(c, "显示分集列表").FontSize(11).
			Absolute().Right(0).Top(winH/2 - fullPartsTabH/2).
			Width(fullPartsTabW).Height(fullPartsTabH)
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
	panel := ui.Column(c).Key("full-parts-panel").
		Absolute().Right(0).Top(winH/2-h/2).
		Width(fullPartsWidth).Height(h).
		Radius(12).Role(ui.RoleDialog).Label("分集列表").
		// 玻璃那一层：**票面「毛玻璃的 Tint 配方」那四样**，一样都不少。
		//
		// ⚠️ 原来这一格是 `ui.RGBA(255, 255, 255, 0.12)`——一层十二个百分点的白，
		// 压在糊过的封面上，**白字读不出来**（封面一亮，字就没了）。
		//
		// 那四样各管一件事：`Tint` 管「字读不读得清」（掺 96% 背景色，底下透出
		// 的是模糊的轮廓，不是内容的细节），`TextColor` 管「文字跟不跟主题走」，
		// `Border` 管「玻璃的边在浅底上有没有」，`Shadow` 管「它浮不浮得起来」。
		Material(glass.Glass{Tint: t.Background.Alpha(0.96)}).
		TextColor(t.Text).
		Border(1, t.Background.Mix(t.Text, 0.25)).
		Shadow(0, t.Space(2), t.Space(6), 0, ui.RGBA(0, 0, 0, 0.12))

	panel.Children(func() {
		ui.Row(c).FillWidth().Padding(10, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			// 字色跟着面板走（`TextColor(t.Text)`），这里不再自己定。
			ui.Text(c, "分集").FontSize(14).Bold().Grow(1)
			if ui.Button(c, "收起分集").FontSize(11).Clicked() {
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
			ui.Text(c, why).FontSize(12).TextColor(t.TextMuted).Padding(12)
		default:
			s.viewFullPartsList(c)
		}
	})
}

// viewFullPartsList 是面板里那一列。
func (s *Shell) viewFullPartsList(c *ui.Context) {
	t := c.Theme()
	rows := s.full.partsRows
	s.full.partsList.Key = func(i int) any { return rows[i].key }
	s.full.partsList.Label = func(i int) string { return rows[i].title }

	ui.List(c, &s.full.partsList, len(rows), func(i int) {
		r := rows[i]
		row := ui.Column(c).Key(r.key).Grow(1).Height(fullPartRowHeight).
			PaddingX(12).Radius(6)
		switch {
		case r.active:
			// Biu 的 `data-[active=true]:bg-primary/20`。
			//
			// ⚠️ 底色跟着**主题的字色**走（不是写死白）：面板那一层是玻璃，玻璃
			// 在深浅两套下是不同的底，白底和黑底要反过来。
			row = row.Background(t.Text.Alpha(0.22))
		case row.Hovered():
			row = row.Background(t.Text.Alpha(0.10))
		}
		row.Children(func() {
			// 字色跟着面板走（`TextColor(t.Text)`），这里不再自己定。
			ui.Text(c, r.title).FontSize(13).SingleLine()
		})
		if row.Clicked() {
			// 选完就收：Biu 的 PageList `onPressItem={onClose}`。
			s.full.partsOpen = false
			if err := s.app.PlayPart(r.cid); err != nil {
				s.addToast(err.Error())
			}
		}
	}).Grow(1)
}
