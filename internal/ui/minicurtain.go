// 迷你播放器的「歌词卷帘」：点封面，迷你窗在同一位置向下展开一块歌词区。
//
// # 它是「同一个窗放大」，不是「另开一个窗」
//
// 调研（BMG-280）给的路径 A：框架有运行时改窗口尺寸的 API
// （`mygo/window.go:858` 的 `SetSize`），所以卷帘就是**这扇窗变高**——
// 于是它跟主窗口之间的那些「两扇窗」问题一个都不存在（不抢焦点、不切
// 路由、歌词和播放状态本来就是同一份）。
//
// # 三件必须自己做、框架不替我们做的事
//
//  1. **窗口高度必须严格等于内容高度**。这一扇窗是**透明**的
//     （`MiniWindowOptions` 的 `Transparent: true`），而透明的那一块
//     **照样吃点击**——窗比内容高 20 像素，底下那个应用的按钮就点不到
//     那 20 像素。⇒ 窗口高度 = 84 + 卷帘高度，**每一帧都跟着动画走**。
//  2. **收起时的尺寸要落盘成 84**。`StateKey: "mini-player"` 会把**宽高
//     也记进去**（`mygo/window_state.go:196` 的 `captureState`），而抓状态
//     是在关窗那一刻（`closeState()`）。⇒ 收起 / 关窗 / 退出三条路都要先
//     把窗高改回 84 再关，否则下次开出来是个 320×320 的透明大窗。
//  3. **卷帘不能长出工作区**。窗被用户拖到屏幕下半部时，往下长 236 会出屏
//     ⇒ 高度按**那扇窗所在显示器的工作区**夹住（`WorkArea`）。
//
// # 主触发是「点一下」，不是「移上去」
//
// 迷你窗是 `Panel`：点它不激活应用（票 #162），于是它几乎永远不是 key 窗口，
// 而 MyGo 的悬停跟踪区带 `NSTrackingActiveInKeyWindow`
// （`mygo/internal/darwin/surface.go:162`）——**失焦时收不到鼠标移动**。
// BMG-280 §2.4 记的就是这一条：悬停展开在迷你窗的主场景里**可能根本不触发**。
//
// ⇒ 这一版**只做点击**。悬停要等它在真窗口上验过 hover 可用之后才谈得上
// 「作为增强」加进来（票面原话：「悬停只在真窗口验证 hover 可用后作为增强」）。
//
// # 设置开关（票 #536 的补充要求）
//
// 设置页有一格「迷你播放器歌词卷帘」，**默认开着**（票面 2026-10-26 的
// 更正消息为准）。关掉之后：
//
//   - 点封面**什么也不发生**（不报错、不弹一下、不改高度）——那档里这一整套
//     代码压根不该被触发；
//   - 展开着的时候把开关关掉 ⇒ **立刻收起**（迷你窗回到 320×84）。
//
// 「立刻收起」怎么做到：设置改完之后 `App.UpdateSettings` → `a.invalidate()`
// **挨个叫醒每个窗口**（铁律三），迷你窗下一帧读到的设置已经是关的 ⇒ 收起。
package ui

import (
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/lyrics"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/motion"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	// miniCurtainH 是卷帘展开时的高度。
	//
	// ⚠️ **236 是 BMG-280 §6.3 定的那一档**（180~300 的中段）：默认位置在屏幕
	// **右上角**（`miniCorner`，工作区顶边往下 12），往下长 236 绰绰有余；而
	// 320 宽里放 20px 的歌词（当前行 ×1.5 = 30px）一行只有十个字——所以
	// 卷帘用自己那一档字号（13px，当前行 19.5px）。
	//
	// ⚠️ **它是「最多」而不是「总是」**：实际高度按工作区夹住（见
	// `miniCurtainHeight`），而夹出来的那个数还受 `miniCurtainMinH` 那一道
	// 门限管着（放不下就不展开，见那个常量的注释）。
	miniCurtainH = 236

	// miniCurtainMinH 是卷帘**最少**有多高才值得展开。
	//
	// ⚠️ **放不下就不展开**：这一扇窗是透明的，而**透明的那一块照样吃点击**
	// ——窗长高 30 像素，底下那个应用的按钮就点不到那 30 像素。所以「只剩
	// 这么点地方」的时候宁可**什么都不做**（用户看不出来少了什么），也不要
	// 拉出一条又窄又矮、还挡着底下应用的卷帘。
	//
	// 120 是 BMG-280 §2.2 给的那一档。
	miniCurtainMinH = 120

	// miniCurtainMargin 是卷帘底边离工作区底边留的那一道。
	//
	// ⚠️ **12 是窗口自己的那道边距**（`miniCorner` 里那个 12）——窗停在
	// 右上角时它离工作区顶边 12，而卷帘底边离工作区底边也留 12，两头一样。
	miniCurtainMargin = 12

	// miniCurtainDuration 是卷帘展开 / 收起那一段的时长。
	//
	// ⚠️ **定时长曲线，不用弹簧**（BMG-280 §6.3）：弹簧会过冲，而这一段是
	// 「一扇窗长高」——过冲意味着窗高短暂地超过目标，而那一帧窗比内容高，
	// 透明的那一块就在吃点击（见文件头第 1 条）。
	miniCurtainDuration = 240 * time.Millisecond

	// miniCurtainFontSize 是卷帘那一档的歌词字号。
	//
	// ⚠️ **13 不是用户偏好里那个字号**（`LyricsSnapshot.FontSize`，12~48）：
	// 满屏那一屏在 1200 宽的窗里，20px 的字一行十几个；而卷帘只有 320 宽，
	// 20px 的字（当前行 ×1.5 = 30px）一行只有**十个**字——一屏三行就没了。
	// 13px（当前行 19.5px）是 BMG-280 §10.2 第 7 条给的那一档。
	//
	// ⚠️ **它不跟用户的字号偏好走**，而用户偏好**也不该**跟它走：卷帘是
	// 「一直挂在桌面上的一小块」，字号太大就挤不下了。
	miniCurtainFontSize = scale.Size13

	// miniCurtainPadX 是卷帘里歌词那一列左右那一份内边距。
	miniCurtainPadX = space.Px12

	// miniCurtainNS 是卷帘那些歌词行的**行键命名空间**。
	//
	// ⚠️ **必须新开一个**：另两处歌词区（迷你播放器那一页 / 满屏播放器那一屏）
	// 用的是 `page` / `full`（`lyrics.go` 的 `lyricNSPage` / `lyricNSFull`）。
	// 三处画的是**同一首歌的同一份歌词**，而行键撞了的话，跨帧认人的那一套
	// 会挑错一个——症状是「一首歌在两个地方显示，滚动的却是同一份」。
	miniCurtainNS = "minicurtain"

	// miniCurtainAreaKey 是卷帘里歌词区那一块的键（铁律四）。
	miniCurtainAreaKey = "mini:curtain:area"

	// miniCurtainEmptyKey 是「暂无歌词」那一块的键（铁律四）。
	miniCurtainEmptyKey = "mini:curtain:empty"

	// miniCurtainCoverKey 是封面那一列（**展开 / 收起的触发器**）的键。
	miniCurtainCoverKey = "mini:curtain:cover"

	// miniCurtainCollapseKey 是卷帘里「收起」那一枚的键。
	miniCurtainCollapseKey = "mini:curtain:collapse"

	// miniCurtainAnimKey 是卷帘高度那个动画值的键。
	//
	// ⚠️ **动画值挂在元素上**（`AnimateTo` 是元素的方法），而那个元素必须
	// **每帧都建、键固定**——框架按「键 + 父」认元素（`mygo/ui/context.go`
	// 的 `newElement` / `rekey`）。所以这个键挂在卷帘那一段的根上，而那一段
	// 每帧都建。
	miniCurtainAnimKey = "mini:curtain:height"

	// miniCurtainBodyKey 是卷帘里「歌词 / 空状态」那一块的键。
	miniCurtainBodyKey = "mini:curtain:body"
)

// miniCurtainAnim 是卷帘展开 / 收起那一段的动画。
//
// ⚠️ **包级变量**：它是每帧都要用的，而每帧新建一个就是每帧一次分配
// （`ui.Animation` 里有一个 `time.Duration` 和一个 `Easing` 接口，接口装箱
// 会逃逸）。而帧预算那条测试数的就是分配次数（`framebudget_test.go`）。
var miniCurtainAnim = ui.Animation{Duration: miniCurtainDuration, Ease: motion.Unified}

// miniCurtainState 是卷帘自己那一摊。
//
// ⚠️ **三个值是三件事**，别把它们并成一个：
//
//   - open 是**用户的意图**（点封面展开的那一下）；
//   - height 是**动画当前走到哪儿**（每帧变的那个值）；
//   - target 是**这一帧窗该有多高**（动画到位之后 = 展开高）。
//
// 把 open 当 height 用的话，收起动画的第一帧窗就一下子缩回 84——而那一帧
// 卷帘还画着 80 高，**窗比内容矮**（卷帘的下半截被裁掉）；把 height 当
// open 用的话，动画走一半时再点一次封面会「再展开一次」而不是收起。
type miniCurtainState struct {
	// open 是用户要不要卷帘开着。
	open bool

	// height 是**这一帧画出来的**卷帘高度（动画值，0 = 完全收起）。
	//
	// ⚠️ **它每帧都变**，而窗口高度跟着它走（文件头第 1 条）。
	height float32

	// target 是**窗该有多高**（= 84 + 卷帘高）。它在点开之前算好
	// （点开那一刻就要知道能长多高），在收起期间保持不变。
	target int

	// animSeen 是「动画通道建过没有」。
	//
	// ⚠️ **它管的是「第一次要不要 `Jump` 一下」**：`AnimateTo` 第一次建通道
	// 时直接给到目标（`mygo/ui/motion.go:302`）——于是「从收着到开着」的第一
	// 帧，卷帘会**一帧就到 236**，没有展开的动画。所以第一次要先 `Jump(0)`
	// 把通道建出来（值 0），之后的 `AnimateTo` 才是从 0 滑上去。
	animSeen bool

	// setHeight 是「窗口高度已经改成多少了」。
	//
	// ⚠️ **为什么要记**：窗口尺寸是**原生**的，而每一帧都会算一次目标高度——
	// 不记的话每一帧都要调一次 `SetSize`（一次原生调用），而高度没变的那
	// 几帧是白调的。
	setHeight int

	// wasOpen 是**上一帧**开着没有。
	//
	// ⚠️ **它是「念一句」那件事的判据**：展开 / 收起各念一次，而念的那一句
	// 只在**状态变了的那一帧**念（不然每一帧都念一遍）。
	wasOpen bool

	// animZeroed 是「**立刻收起**那一下已经发生过了」。
	//
	// ⚠️ **它管的是「动画通道要不要归零」**：`miniCurtainCollapseNow` 把
	// `height` 归零了，但**动画通道里的值还在 236**——而下一帧那个
	// `AnimateTo(0)` 会从 236 往 0 滑，窗于是**先弹回 320 再缩到 84**，
	// 那 240ms 里窗比内容高（透明的那一块在吃点击）。
	//
	// ⇒ 「立刻收起」这一下必须**连动画通道一起钉到 0**（`Jump`），
	// 而 `Jump` 只能对着**元素**调——元素每帧都建，所以这里记的是
	// 「下一次建那个元素时要 `Jump` 一下」。
	animZeroed bool
}

// miniCurtainEnabled 报「迷你播放器歌词卷帘」那一格是开是关。
//
// ⚠️ **默认是开**（票 #536）：`Settings.MiniLyricsCurtainOn()` 那一处是
// 唯一的定义（nil = 没设过 = 默认开）。
func miniCurtainEnabled(st app.Settings) bool {
	return st.MiniLyricsCurtainOn()
}

// miniCurtainHeight 算卷帘该有多高：按那扇窗所在显示器的工作区夹住。
//
// work 是那块工作区（`mygo.Screen.DisplayMatching(win).WorkArea`），win 是
// 窗现在的位置和大小。
//
// ⚠️ **只有往下这一边会不够**：卷帘是往**下**长的，而窗的左右两边它一个
// 像素都不占（宽度还是 320）。
//
// ⚠️ **不够就不展开**（返回 0）：透明窗多出来的那一块照样吃点击，而一条
// 只有几十像素高的卷帘既读不出歌词又挡着底下的应用——那比不展开更坏。
func miniCurtainHeight(work mygo.Rectangle, win mygo.Rectangle) int {
	if work.Width <= 0 || work.Height <= 0 {
		return 0
	}
	// 窗底到工作区底边还剩多少（底下留一道 12 的边距）。
	down := work.Y + work.Height - (win.Y + win.Height) - miniCurtainMargin
	if down < miniCurtainMinH {
		return 0
	}
	if down > miniCurtainH {
		return miniCurtainH
	}
	return down
}

// miniCurtainRect 是「一个矩形」——`mygo.Rectangle` 的别名。
//
// ⚠️ **为什么要别名**：工作区那块和窗那块都是 `mygo.Rectangle`，而
// `miniCurtainWorkArea` 是**一个函数**（问真的那块工作区）——同名会撞。
type miniCurtainRect = mygo.Rectangle

// miniCurtainWorkArea 是那扇窗所在显示器的工作区。
//
// ⚠️ **返回零值 = 「不知道」**：没有真窗口（无头测试里）、或者窗已经关了。
// 那时 `miniCurtainHeight` 返回 0（不展开）——而**这不是「卷帘坏了」**，
// 是「这一扇窗的位置没人知道」。
//
// ⚠️ **`mygo.Screen` 头上有 `needsApp`**（`mygo/app.go:128`）：应用还没
// ready 的时候调它直接 panic。真窗口在 `App.WhenReady` 之后才有，所以
// 这一行只在应用 ready 之后才走到。
func miniCurtainWorkArea(w *mygo.Window) mygo.Rectangle {
	if w == nil || w.IsDestroyed() {
		return mygo.Rectangle{}
	}
	win := w.Bounds()
	return mygo.Screen.DisplayMatching(win).WorkArea
}

// miniCurtainToggle 是「点封面」那一下：切换展开 / 收起。
//
// 返回「这一下有没有真的切」（关着的时候返回 false，什么都不做）。
//
// ⚠️ **开关关着的时候这一整套压根不该被触发**（票面原话）：所以第一件事
// 就是读设置，关着**直接返回**——不建任何卷帘、不改任何高度、不留半截
// 窗口。
func (s *Shell) miniCurtainToggle() bool {
	st := s.app.SettingsSnapshot()
	if !miniCurtainEnabled(st) {
		// 关着：什么都不做。**连「已经开着」也不管**——开着的时候用户把开关
		// 关掉，那条路走的是 `viewMini` 里那一支（见那个函数的注释）。
		return false
	}
	if s.miniCurtain.open {
		s.miniCurtainCollapse()
		return true
	}
	h := s.miniCurtainMaxHeight()
	if h <= 0 {
		// 放不下：不展开（文件头那条「不够就不展开」）。
		return false
	}
	s.miniCurtain.open = true
	s.miniCurtain.target = h
	return true
}

// miniCurtainMaxHeight 算「这一扇窗现在最多能长出多高的卷帘」。
//
// ⚠️ **它走的是「窗现在在哪」**，不是「窗应该在哪」——用户拖到哪儿就在
// 哪儿算，而那个位置是这一扇窗自己的（`StateKey` 记住的）。
func (s *Shell) miniCurtainMaxHeight() int {
	w := s.win
	if w == nil || w.IsDestroyed() {
		// ⚠️ **没有真窗口也要能算**：无头 Tester 里没有窗，而「这扇窗现在在
		// 哪儿」是**注入的**（`s.miniCurtainWork`）——那一档要量的是「卷帘能
		// 长多高」，而它跟真窗口没关系。
		//
		// ⚠️ **注入的那一块是「工作区」，而窗的位置还是不知道**：所以那一档
		// 按「窗在工作区的顶边」算（最宽松的那一档）——而**夹住工作区**这件
		// 事本身由 `miniCurtainHeight` 单测（那是纯逻辑）。
		if s.miniCurtainWork.Width > 0 {
			return miniCurtainHeight(s.miniCurtainWork, mygo.Rectangle{
				X: s.miniCurtainWork.X, Y: s.miniCurtainWork.Y,
				Width: miniWidth, Height: miniHeight,
			})
		}
		return 0
	}
	return miniCurtainHeight(miniCurtainWorkArea(w), w.Bounds())
}

// miniCurtainCollapse 收起卷帘：窗高回到 84。
//
// **幂等**（收着的时候再调一次什么也不做）。
func (s *Shell) miniCurtainCollapse() {
	if !s.miniCurtain.open && s.miniCurtain.height <= 0 {
		// 已经收着了。⚠️ **但窗高还是得钉回 84**：上一次收起的动画中途窗被
		// 拖走了、或者展开到一半窗被关掉，收起这一句没走完——窗就留在一个
		// 高于 84 的状态，而 `StateKey` 会把那个高度记下来。
		s.miniCurtainSetHeight(miniHeight)
		return
	}
	s.miniCurtain.open = false
}

// miniCurtainCollapseNow **立刻**收起卷帘：不等那 240ms 的动画。
//
// ⚠️ **它用在两条路上**：
//
//  1. **设置开关被关掉**（票面：「展开着的时候把开关关掉 ⇒ **立刻收起**」）；
//  2. **关窗 / 退出**：那里窗马上就要没了，滑下去那 240ms 是白滑的——而
//     更要紧的是**落盘的尺寸**（`StateKey` 记宽高，抓的是关窗那一刻）。
//
// ⚠️ **它把动画值也一起钉到 0**：不然下一帧那个 `AnimateTo` 还会从 236
// 往 0 滑——而那时候窗已经关了或者开关已经关了，滑下去的那一段是纯浪费，
// 而**中间那几帧窗比内容高**（透明的那一块在吃点击）。
func (s *Shell) miniCurtainCollapseNow() {
	s.miniCurtain.open = false
	s.miniCurtain.height = 0
	s.miniCurtain.setHeight = miniHeight
	// ⚠️ **动画通道也要归零**（`animZeroed`）：只把 `height` 归零的话，下一帧
	// 那个 `AnimateTo(0)` 会从 236 往 0 滑——窗于是**先弹回 320 再缩到 84**，
	// 那 240ms 里窗比内容高（透明的那一块在吃点击），而票面要的是「**立刻
	// 收起**」。
	s.miniCurtain.animZeroed = true
	s.miniCurtainSetHeight(miniHeight)
}

// miniCurtainSetHeight 把这一扇窗的高度改成 h（**宽度不动**）。
//
// ⚠️ **宽度取窗现在那个宽**，不写死 320：用户可以把窗往宽拖（迷你窗故意
// 没有 `DisableResize`，理由见 `MiniWindowOptions`），而卷帘是**贴在窗的
// 下面**的——窗多宽卷帘就多宽。
//
// ⚠️ **高度是「原生」那一边的事**，改完原生会自己出一帧（`setFrame:` →
// `SurfaceResize` → 下一帧按新尺寸重排），而**这一帧的布局**用的还是旧
// 高度——所以窗高要**每帧跟着动画走**（见 `viewMini`）。
func (s *Shell) miniCurtainSetHeight(h int) {
	w := s.win
	if w == nil || w.IsDestroyed() {
		return
	}
	width, _ := w.Size()
	if width <= 0 {
		width = miniWidth
	}
	w.SetSize(width, h)
}

// miniCurtainTargetHeight 是这一帧**窗该有**多高。
func (s *Shell) miniCurtainTargetHeight() int {
	if !s.miniCurtain.open {
		return miniHeight
	}
	h := s.miniCurtain.target
	if h <= 0 {
		h = s.miniCurtainMaxHeight()
		if h <= 0 {
			return miniHeight
		}
		s.miniCurtain.target = h
	}
	return miniHeight + h
}

// miniCurtainSyncHeight 把卷帘高度那一段动画推一帧，并把窗口高度改成
// 「84 + 这一帧的卷帘高」。
//
// ⚠️ **它每帧都要调**（`viewMini` 开头那一支），因为两样东西都在动：
// 动画值在动，而**窗口高度必须跟着它动**——这一扇窗是透明的，而透明的那
// 一块照样吃点击，窗比内容高 20 像素，底下那个应用的按钮就点不到那 20
// 像素（文件头第 1 条）。
//
// ⚠️ **动画值挂在元素上**，而那个元素必须**每帧都建、键固定**——框架按
// 「键 + 父」认元素（`mygo/ui/context.go` 的 `newElement` / `rekey`）。所以
// 这一段是**一个空的 `Box`**，键是 `miniCurtainAnimKey`，它建在**根的下面**
// （根的键是 `mini-root`，每帧都一样）。
//
// ⚠️ **`AnimateTo` 第一次建通道时直接给到目标**（`mygo/ui/motion.go:302`
// 的 `channel(...)` 返回 `made=true` → `return target`）——所以要让它**从 0
// 滑上去**，得先 `Jump` 一下把它钉在 0，再 `AnimateTo`。
//
// ⚠️ **收起的那一段也要有帧**：动画走的时候框架会自己要帧
// （`AnimateTo` → `c.AnimationFrame()`），而**窗口高度是原生那一边的事**
// ——原生改完自己会出帧（`setFrame:` → `SurfaceResize`），所以两边各走各的
// 那一半，合起来是同步的。
func (s *Shell) miniCurtainSyncHeight(c *ui.Context) {
	// 🔴 **Esc 收起**（票面：「Esc、再次点击封面、点收起键都能收」）。
	//
	// ⚠️ **用的是窗口级 `Shortcut` 而不是 `OverlayShortcut`**：后者要一个
	// **浮层**（`mygo/ui/scope.go` 的 `overlayShortcut` 走的是「浮层链」，
	// 那是抽屉 / 弹窗那一套），而迷你窗没有浮层——卷帘是这扇窗**自己的一
	// 段**，不是浮在上面的一层。所以走 `c.Shortcut(0, ui.KeyEscape)`：那是
	// **窗口级**的键（`mygo/ui/context.go:462` 的 `context.Shortcut` ⇒
	// `rt.shortcut(0, …)`，`0` 就是窗口）。
	//
	// ⚠️ **窗口级 Esc 会先被焦点链抢走**（`mygo/ui/input.go:890` 的
	// `routeKey`：**焦点链优先，浮层其次，窗口最后**）。而迷你窗是 `Panel`
	// ——点它不激活应用，于是它几乎永远不是 key 窗口，**焦点链是空的** ⇒
	// Esc 落到窗口这一级。
	//
	// ⚠️ **但这一条在真机上有一个限制**：Esc 要**先到这一扇窗**。Panel
	// 不激活应用（`mygo/internal/darwin/window.go:806` 那个
	// `if !w.opts.Panel` 才 `activateIgnoringOtherApps`），所以用户点了迷你窗
	// 之后，**键盘仍然在原来那个应用手里**——Esc 走的是那个应用的窗口，不是
	// 这一扇。⇒ **只有迷你窗是 key 窗口的时候 Esc 才到得了**（用户用 ⌘Tab
	// 或别的办法把键盘焦点切到这一扇时）。
	//
	// ⚠️ **框架自己那条「Escape 关掉 Panel」别去抢**：`mygo/flyout.go:244`
	// 那个 `escape()` 在**键没人要**的时候才跑（`mygo/ui/input.go:934`），
	// 而它关的是整扇窗（迷你窗关了，用户就没得玩了）。而我们这一句**先**
	// 要了这个键——`rt.consumed` 一置，框架那一条就不会跑。
	if s.miniCurtain.open && c.Shortcut(0, ui.KeyEscape) {
		s.miniCurtainCollapse()
	}

	// 目标高度：开着就是「84 + 卷帘高」，收着就是 84。
	target := float32(s.miniCurtainTargetHeight() - miniHeight)

	// 那个「空的、键固定的」元素：动画值挂在它身上。
	holder := ui.Box(c).Key(miniCurtainAnimKey)
	// ⚠️ **「立刻收起」的那一下要把动画通道钉到 0**（`animZeroed`）：
	// `miniCurtainCollapseNow` 只归零了 `height`，而**通道里的值还在 236**
	// ——`AnimateTo(0)` 会从 236 往 0 滑，窗于是**先弹回 320 再缩到 84**，
	// 那 240ms 里窗比内容高（透明的那一块在吃点击）。
	//
	// ⚠️ **`Jump` 会打断正在走的动画**（`ch.moving = false`）——所以它只
	// 在「立刻收起」这一下用；**正常收起**（再点封面 / Esc / 收起键）走的是
	// 那条要滑下去的动画，不 `Jump`。
	if s.miniCurtain.animZeroed {
		s.miniCurtain.animZeroed = false
		holder.Jump(miniCurtainAnimKey, 0)
	}
	// ⚠️ **`Jump` 在第一次建通道时返回那个值**（`mygo/ui/motion.go:372`），
	// 而它建的通道值就是 0 ——所以第一次 `Jump(0)` 返回 0，之后 `AnimateTo`
	// 从 0 起步。
	//
	// ⚠️ **收着的时候不 `Jump`**：`Jump` 会**打断**正在走的动画（它把通道的
	// 值直接钉住，`ch.moving = false`）。收起的那一段是要**滑下去**的，而
	// `Jump(0)` 会让它一帧到位。
	//
	// ⚠️ **但「从开着变成收着」的第一帧不能跳**：那一帧通道是 0（从没建过），
	// `AnimateTo` 会**直接给到目标**（`made=true`）——于是卷帘一帧就没了，
	// 没有收起的动画。所以「从来没有过动画值」的时候要先 `Jump(0)` 建通道。
	first := !s.miniCurtain.animSeen
	if first {
		s.miniCurtain.animSeen = true
		holder.Jump(miniCurtainAnimKey, 0)
	}
	h := holder.AnimateTo(miniCurtainAnimKey, target, miniCurtainAnim)
	s.miniCurtain.height = h

	// ⚠️ **窗口高度跟的是「画出来的高度」，不是「目标高度」**：动画走到一半
	// 的时候窗高就是一半——窗高跟目标走的话，卷帘还在滑，窗就已经到位了，
	// 那多出来的一截透明区在吃点击。
	//
	// ⚠️ **四舍五入到整像素**：窗口尺寸是整数，而动画值是浮点——每一帧
	// 都调 `SetSize` 的话，同一个高度会被反复设成同一个整数（原生那一边
	// 自己比），而**真的变了才设**省掉一次原生调用。
	want := miniHeight + int(h+0.5)
	if s.miniCurtain.setHeight != want {
		s.miniCurtain.setHeight = want
		s.miniCurtainSetHeight(want)
	}

	// 念一句：展开 / 收起各念一次（读屏软件）。
	if s.miniCurtain.open != s.miniCurtain.wasOpen {
		s.miniCurtain.wasOpen = s.miniCurtain.open
		s.miniCurtainAnnounce(s.miniCurtain.open)
	}
}

// viewMiniCurtain 画卷帘那一段（迷你卡片下面那一块）。
//
// h 是**这一帧**卷帘该有多高（动画值）。
//
// ⚠️ **h <= 0 的时候一个字都不画**：窗高已经回到 84，而这一段要是还画着，
// 窗就比内容矮——下半截卷帘被裁掉，而裁掉的那一截在命中链里**还在**
// （框架的命中链是按上一帧的盒子算的），于是用户按到的是一块看不见的东西。
func (s *Shell) viewMiniCurtain(c *ui.Context, st app.State, t *ui.Theme, h float32) {
	if h <= 0 {
		return
	}
	// ⚠️ **卷帘自己装一份底**（`t.Surface`）：这一扇窗是**透明**的，而透明
	// 的地方透出的是桌面——歌词压在桌面壁纸上读不出来（深色壁纸上的深色
	// 字）。
	//
	// ⚠️ **但它自己不再画底**：整扇窗是**一块**面板（底、圆角、影全挂在根
	// 那一段上，见 `viewMini` 那一段长注释）——而这一段是根的**第二个孩子**，
	// 根的底已经铺满整扇窗了，它再画一份底是多余的。
	//
	// ⚠️ **圆角只有上面两个角**：卷帘是**从那张卡片底下拉出来**的一块，
	// 上面两个角跟着卡片的圆角，下面两个角是方的（它贴着卡片的下沿）。
	//
	// ⚠️ **宽度是 100% 而不是写死 320**：窗可以往宽拖（迷你窗故意没有
	// `DisableResize`），而卷帘是贴在窗下面的——窗多宽它就多宽。
	//
	// ⚠️ **四个角都是方的**（视觉审查量出来的，2026-10-27）：两张各有自己
	// 圆角的板拼在一起，接缝处会出现**两个相对的圆弧**——卡片的下沿两个角
	// 是**凹**进去的，卷帘的上沿两个角是**凸**出来的，中间那一小块是**空的**。
	//   - 画面上量得到：接缝那一行（y=83）左沿是 231/225/215，而卷帘的表面
	//     是 241/235/224——**深了 10 个色阶**，而那是**桌面透出来的地方**
	//     （这一扇窗是透明的）。
	//   - 而那一小块**照样吃点击**——这一扇窗是 `Transparent: true`，而透明
	//     的那一块照样拦点击（文件头第 1 条）。
	// ⇒ 卷帘上面两个角改成方的，而**迷你卡片那 84 高的一段圆角改成「上面两个
	// 角圆、下面两个角方」**——两张拼起来是**一块**面板，形状是「上面圆、
	// 下面方」，接缝处一条缝都没有。
	//
	// ⚠️ **影落在接缝上，量出来是一道灰痕**（浅色下 y=83 那一行比表面深了
	// 10 个色阶，卡片那一段的右下角也是）：`shadow.Popover` 是 0/6/20 的
	// 一道影，落在**卷帘这一块**上，而卷帘紧贴着卡片——影于是压在两者的
	// 接缝上。⇒ **影改挂到迷你卡片那一段上**（那一段才是浮在桌面上的那块），
	// 而卷帘这一段不挂影。
	//
	ui.Box(c).Key(miniCurtainAreaKey).
		FillWidth().Height(h).
		// ⚠️ **四个角都是方的**：见上面那一段（接缝处的两个相对圆弧会漏出
		// 桌面，而那一小块照样吃点击）。
		Radius(space.None).
		// ⚠️ **名字挂在这块上**：读屏软件念得出「迷你播放器歌词」，而
		// 无头 Tester 也靠它量卷帘那一块的框（BMG-280 §10.5 第 19 条）。
		Label("迷你播放器歌词").
		// ⚠️ **它也是拖动区**：迷你窗是 frameless 的，没有标题栏——用户要
		// 能从卷帘上把窗拖走（上面那张卡片上的拖动区在 84 高那一段，而
		// 展开之后用户的手多半落在卷帘这一块上）。
		DragWindow().
		Children(func() {
			s.viewMiniCurtainBody(c, st, t)
		})
}

// viewMiniCurtainBody 是卷帘里面那一块：歌词（或者「暂无歌词」）+ 收起键。
func (s *Shell) viewMiniCurtainBody(c *ui.Context, st app.State, t *ui.Theme) {
	snap := s.app.Lyrics()
	lv := &s.miniLyric
	// 🔴 认换歌在这一帧的最开头，早于任何一句决定（同 lyrics.go 那一处）。
	lv.onSong(snap.Key)

	active := lyrics.ActiveAt(snap.Rows, lyricNow(st.Position, snap.OffsetMS))
	if active < 0 {
		lv.forget()
	}

	// ⚠️ **这一块必须是一个 `Grow(1)` 的 Column**（理由见 `viewLyricLines`
	// 的文件头）：Box 里套一个 Row 的话，Row 的 `Grow(1)` 走的是横轴，高度
	// 由内容撑着，而内容的高度里有那份居中留白——于是「高 → 留白 → 内容更高」
	// 一帧大一截。
	ui.Column(c).Key(miniCurtainBodyKey).Grow(1).FillWidth().
		Children(func() {
			// ⚠️ **收起键画在**这一段的 `Children` 里面**：元素的父节点是
			// **建它那一刻** `c.parent` 指着的那个——`Children` 之外建的元素
			// 挂在**根**上（`mygo/ui/context.go` 的 `Children` 把 `c.parent`
			// 存下来又还原），而根是整个窗口——那一枚键会跑到窗口的右下角去。
			//
			// ⚠️ **它绝对定位在卷帘的右下角**（不是跟着词滚）：它是**控件**
			// （要按得到），而歌词区是内容——放进滚动容器的话它会跟着词滚走。
			s.viewMiniCurtainCollapse(c, t)
			if active < 0 {
				s.viewMiniCurtainEmpty(c, t)
				return
			}
			// ⚠️ **卷帘自己那一档字号**：满屏那一屏用的是**用户偏好里的字号**
			//（12~48），而 320 宽里放 20px 的字（当前行 ×1.5 = 30px）一行只有
			// 十个字。这里传一份**改了字号的快照副本**进去，而共用代码
			// （`viewLyricLines` / `lyricLine`）一个字都不改。
			cur := snap
			cur.FontSize = miniCurtainFontSize
			s.viewLyricLines(c, lv, cur, active, lyricStyle{
				color: t.Text,
				muted: t.TextMuted,
				// ⚠️ **渐隐盖到 `t.Surface`**：卷帘的底是 `Surface`（上面
				// 那一段），盖到 `t.Background` 的话，那两条渐隐带子会是
				// **另一个色**——一眼就看出「这一块不是一张卡片」。
				fade: t.Surface,
				area: miniCurtainAreaKey,
				ns:   miniCurtainNS,
				// ⚠️ **居中**：卷帘只有 320 宽，左对齐的那些句子左沿咬在
				// 内容盒的左沿上，右边空一大块——而「正在唱的那一句」是放大的，
				// 居中才看得出它是当前句。
				center: true,
				padX:   miniCurtainPadX,
			})
		})

	if active < 0 {
		return
	}
	// ⚠️ 放在整棵树建完之后：它要用容器和正在唱那一句的框，而那两个是
	// **上一帧**量到的（理由同 lyrics.go 里那一处）。
	lv.centre(c, active, lyricShapeOf(snap.Rows), miniCurtainFontSize, s.snapScroll)
}

// miniCurtainAnnounce 念一句「歌词卷帘展开 / 收起」。
//
// ⚠️ **只能在建视图的那一段里有效**（`s.announce` 的注释）：帧外调它什么
// 也不念。而这一句是**给读屏软件**的——这一扇窗是 Panel，读屏用户多半
// 在键盘上，展开 / 收起是一件该被说出来的事。
func (s *Shell) miniCurtainAnnounce(open bool) {
	if open {
		s.announce("已展开歌词卷帘")
		return
	}
	s.announce("已收起歌词卷帘")
}

// viewMiniCurtainEmpty 是「这一首没有歌词」那一档。
//
// ⚠️ **不放「找歌词」那一枚**：那个弹窗画在**主窗口**那一层（`lyricssearch.go`
// 走 `viewMain`），而这一扇是迷你窗——在小窗里点它，弹窗会弹到主窗口上去，
// 而主窗口那时候是收着的（进迷你就是 `HideToBackground`）。⇒ 那个入口在
// 这一档是**兑现不了的**，所以不放。
func (s *Shell) viewMiniCurtainEmpty(c *ui.Context, t *ui.Theme) {
	ui.Box(c).Key(miniCurtainEmptyKey).Grow(1).FillWidth().Center().
		Children(func() {
			ui.Text(c, noLyrics.Title).FontSize(scale.Font(c, scale.Size13)).
				TextColor(t.TextMuted)
		})
}

// viewMiniCurtainCollapse 是卷帘里那一枚「收起」。
//
// ⚠️ **它和「再点封面」是同一件事**（收起），而 Esc 是第三件。三条路都在。
func (s *Shell) viewMiniCurtainCollapse(c *ui.Context, t *ui.Theme) {
	ui.Box(c).Key(miniCurtainCollapseKey).Absolute().Right(space.Px4).Bottom(space.Px4).
		Children(func() {
			if flatIconButton(c, iconChevronUp, "收起歌词", 16).Clicked() {
				s.miniCurtainCollapse()
			}
		})
}
