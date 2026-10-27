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

	// miniCurtainDuration 是卷帘展开 / 收起那一段的**兜底时长**。
	//
	// ⚠️ **它现在只是「测试推钟用的一把尺」**，不是动画时长——动画是**弹簧**
	// （见 `minicurtain_spring.go`），而弹簧没有固定时长。
	//
	// ⚠️ **为什么还留着它**：`internal/ui/minicurtain_test.go` 里有几处
	// `tt.Advance(miniCurtainDuration)` 用它推钟。⇒ 那些地方要改成「推到
	// 弹簧停稳」为止（推固定时长是**静默失效**的那一类，见 AGENTS.md §2）。
	//
	// ⚠️ **旧版这里是「定时长曲线，不用弹簧」**（BMG-280 §6.3），理由是
	// 「弹簧会过冲，而这一段是一扇窗长高」——票 #307 把弹簧加回来了，而
	// **那条顾虑用镜像解决了**（过冲只发生在弹簧内部的 `p` 上，画出来的高度
	// 永远 ≤ 目标，见 `minicurtain_spring.go` 的文件头）。
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

	// miniCurtainFocusWindow 是「把键盘抢到迷你窗这一扇」那一段的**窗口**
	// （毫秒）。
	//
	// ⚠️ **为什么要有这么一个窗口**（票 #307 修 F1）：评审实测（PR #590 第
	// 1 轮，2026-10-27 真机）——**Finder 保持前台，点封面之后应用压根没被
	// 激活**，于是 Esc 到不了这一扇窗（而票面要的是「点封面 → 应用激活 →
	// Esc → 收起」）。
	//
	// ⚠️ **而它不是每次都失败**（实测四轮里有一轮失败）——所以「**失败了就
	// 再试**」是办法，「多试几次」不是。
	//
	// ⚠️ **1200ms 是怎么来的**：一次激活要 16~50ms（系统给别的应用一个「别
	// 抢回去」的机会），而两次重试之间隔 120ms ⇒ **十次机会**，足够跨过
	// 任何一个「系统正在处理别的事」的窗口；而 1200ms 又**短到不会在用户
	// 已经走开之后还把应用拽回前台**。
	miniCurtainFocusWindow = 1200 * time.Millisecond

	// miniCurtainFocusDelay 是两次「激活应用」重试之间的间隔。
	//
	// ⚠️ **120ms 是怎么来的**：比一次激活慢三倍（那一句是异步的，16~50ms
	// 才落地），所以**下一次重试不会撞上上一次还没落地的那一次**；而比
	// 「用户以为没反应」的阈值（100ms）长一点点。
	miniCurtainFocusDelay = 120 * time.Millisecond

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

	// miniCurtainLoadingKey 是「歌词还在路上」那一块的键。
	//
	// ⚠️ **和空状态那个键分开**：同一个键上换内容，读屏软件读到的是同一句话；
	// 而这两档说的是两件事（「在等」和「没有」）。
	miniCurtainLoadingKey = "mini:curtain:loading"

	// miniCurtainCoverKey 是封面那一列（**展开 / 收起的触发器**）的键。
	miniCurtainCoverKey = "mini:curtain:cover"

	// miniCurtainCollapseKey 是卷帘里「收起」那一枚的键。
	miniCurtainCollapseKey = "mini:curtain:collapse"

	// miniCurtainBodyKey 是卷帘里「歌词 / 空状态」那一块的键。
	miniCurtainBodyKey = "mini:curtain:body"

	// miniCurtainOriginKey 是卷帘左上角「歌词来自 B 站」那一行的键。
	miniCurtainOriginKey = "mini:curtain:origin"
)

// miniCurtainFocusState 是「把键盘抢到迷你窗这一扇」那一档的状态（票 #307
// 修 F1）。
//
// ⚠️ **`need` 与 `done` 是一对**：「点封面那一刻应用**不是**前台」才需要
// 激活，而「**已经激活过**了」就收手。⇒ 两者分开记，用户自己切走
// （`OnDidResignActive`）**不会**让重试重新开始——否则就会**把用户从他自己
// 那件事上拽走**。
type miniCurtainFocusState struct {
	// need 是「点封面那一刻应用**不是**前台」。
	need bool

	// done 是「这一轮已经激活过了」（`OnDidBecomeActive` 到了）。
	//
	// ⚠️ **它和 `Shell.miniCurtainAppActive` 是两件事**：那一位是「应用**此刻**
	// 是前台吗」（用户切走就变假），而这一位是「**这一轮**激活过了吗」（不
	// 会变回假）。
	done bool

	// until 是「重试到那一刻为止」。
	until time.Time

	// next 是**下一次**重试的时刻。
	next time.Time
}

// miniCurtainState 是卷帘自己那一摊。
//
// ⚠️ **三个值是三件事**，别把它们并成一个：
//
//   - open 是**用户的意图**（点封面展开的那一下）；
//   - height 是**这一帧画出来多高**（弹簧算出来的那个值）；
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
	//
	// ⚠️ **它是弹簧算出来的「画出来的高度」**，不是弹簧内部那个 `p`——过冲
	// 只发生在 `p` 上，而 `height` 永远 ≤ 目标（见 `minicurtain_spring.go`）。
	height float32

	// target 是**窗该有多高**（= 84 + 卷帘高）。它在点开之前算好
	// （点开那一刻就要知道能长多高），在收起期间保持不变。
	target int

	// spring 是**卷帘高度那个弹簧**（票 #307）。
	//
	// ⚠️ **旧版这里是 `ui.Animation` 那条通道**（`AnimateTo` / `Jump`），而
	// 票 #307 把它换成了自己积分的弹簧——理由见 `minicurtain_spring.go` 的
	// 文件头（框架那条通道的过冲我们控制不住，而「窗高不超过内容高」这条
	// 恰恰是过冲会破的）。
	spring miniCurtainSpring

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

	// lyricSnap 是「下一帧的歌词要**一次到位**，不重放那段滚过去的动画」。
	//
	// ⚠️ **它只在展开的那一下为真**（票 #307 §②）：展开之前歌词区压根没画
	// 过，滚动位置是**上一首歌**留下的（或 0）——从那儿慢慢滚过来，用户看到
	// 的是「当前句从屏幕外边爬上来」，而不是「卷帘放开，当前句就在下沿」。
	//
	// ⚠️ **它用完就清**（`miniCurtainLyricPlace` 里清）：一次就够。
	lyricSnap bool
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
// `miniCurtainWorkAreaOf` 是**一个函数**（问真的那块工作区）——同名会撞。
type miniCurtainRect = mygo.Rectangle

// miniCurtainToggle 是「点封面」那一下：切换展开 / 收起。
//
// 返回「这一下有没有真的切」（关着的时候返回 false，什么都不做）。
//
// ⚠️ **开关关着的时候这一整套压根不该被触发**（票面原话）：所以第一件事
// 就是读设置，关着**直接返回**——不建任何卷帘、不改任何高度、不留半截
// 窗口。
func (s *Shell) miniCurtainToggle(now time.Time) bool {
	st := s.app.SettingsSnapshot()
	if !miniCurtainEnabled(st) {
		// 关着：什么都不做。**连「已经开着」也不管**——开着的时候用户把开关
		// 关掉，那条路走的是 `viewMini` 里那一支（见那个函数的注释）。
		return false
	}
	if s.miniCurtain.open {
		s.miniCurtainCollapse(now)
		return true
	}
	h := s.miniCurtainMaxHeight()
	if h <= 0 {
		// 放不下：不展开（文件头那条「不够就不展开」）。
		return false
	}
	s.miniCurtain.open = true
	s.miniCurtain.target = h
	// 🔴 **歌词要「一次到位」**（票 #307 §②）：展开之前歌词区压根没画过，
	// 滚动位置是**上一首歌**留下的（或 0）——从那儿慢慢滚过来，用户看到的是
	// 「当前句从屏幕外边爬上来」，而不是「卷帘放开，当前句就在下沿」。
	//
	// ⚠️ **而量到的那两个框也要作废**（`forget`）：它们是**上一次**卷帘开着
	// 时候量的（或者是别的歌的）——拿它们算「当前句该在哪儿」，算出来的是
	// 上一首歌的位置。
	s.miniCurtain.lyricSnap = true
	s.miniLyric.forget()
	// ⚠️ **弹簧朝 1 走**（票 #307）：展开是**欠阻尼**的（ζ = 0.55）⇒ 放得快、
	// 到底后回弹两三下、幅度递减。
	//
	// ⚠️ **速度保留**：而此刻弹簧多半停在 0（速度 0）——若是从「收着」直接
	// 开始，速度就是 0，正是「放下去」的起点。
	s.miniCurtain.spring.start(1, now)
	// 🔴 **展开的那一下要把键盘抢到这一扇窗上**（票 #536，评审实测抓到的那个）。
	//
	// ⚠️ **为什么非做不可**：迷你窗是 `Panel`（`MiniWindowOptions`），
	// 点它**不激活应用**（`mygo/internal/darwin/window.go:806` 那个
	// `if !w.opts.Panel` 才 `activateIgnoringOtherApps`）——于是**键盘不在
	// 这一扇窗上**，Esc 走的是**用户原来那个应用**（访达、浏览器…）。
	//
	// ⇒ **窗口级的 Esc 压根到不了这一扇窗**：`c.Shortcut(0, ui.KeyEscape)`
	// 那一层要的是「这一扇窗收得到键」，而它收不到。
	//
	// ⚠️ **实测（2026-10-27 评审在真窗口上复现的）**：访达在前台，点封面
	// 展开到 301 高，然后按 Esc —— **窗口仍然是 301 高，访达仍然在前台**。
	//
	// ⚠️ **这一步要两样，缺一样都不行**：
	//
	//  1. **把应用带到前台**（`mygo.App.Focus()` → `activateIgnoringOtherApps:`，
	//     `mygo/app.go:370`）——不然系统把键盘给**原来那个应用**，而这一扇
	//     窗压根收不到按键。
	//  2. **让这一扇成为 key 窗口**（`w.Focus()` → `makeKeyAndOrderFront:`）——
	//     不然应用是前台了，而**前台那个应用的 key 窗口是别的窗**（主窗口
	//     那一扇，或者压根没有），键盘还是不在这一扇上。
	//
	//     ⚠️ **`w.Focus()` 单独做不够**：它是 `makeKeyAndOrderFront:` **加上**
	//     `if !w.opts.Panel { activateIgnoringOtherApps: }`——而这一扇**是
	//     Panel**，所以它**只做前半截**：把这一扇变成 key 窗口，但**应用仍然
	//     不是前台**。而应用不是前台的时候，key 窗口的键盘事件压根不进来
	//     （Cocoa 把键送给**前台应用**的 key 窗口）。
	//
	// ⚠️ **顺序：先激活应用，再抢 key 窗口**：反过来（先 `makeKeyAndOrderFront:`
	// 再 `activateIgnoringOtherApps:`）的话，激活应用那一刻系统会把 key 窗口
	// 交还给**前台那个应用自己的 key 窗口**——这一扇又变成「前台应用的一个
	// 非 key 窗」。
	//
	// ⚠️ **只在展开的时候做**：收起那一下**不抢**——用户是在「自己刚点过的那
	// 扇窗」上按的（点封面就已经抢过一次了），而**每一次收起都把用户抢到前
	// 台**是更坏的行为（他在写别的东西的时候被拽走）。
	s.miniCurtainActivate(now)
	return true
}

// miniCurtainActivate 把键盘抢到迷你窗这一扇上：先激活应用，再让它成为 key
// 窗口。
//
// ⚠️ **它要的是「键盘在这一扇窗上」**：窗口级的 Esc（`c.Shortcut(0,
// ui.KeyEscape)`）要这一扇窗收得到键，而迷你窗是 `Panel`——点它不激活
// 应用，键盘就留在用户原来那个应用里（见 `miniCurtainToggle` 那一条
// 长注释）。
//
// ⚠️ **两步都要，顺序也不能换**：见 `miniCurtainToggle` 里那一段。
//
// ⚠️ **只有第一步有那个前提**：`mygo.App.Focus()` 头上有 `needsApp`
// （`mygo/app.go:128`）——应用没 ready 的时候调它**直接 panic**，而无头
// Tester 里压根没有应用。所以那一档先看 `mygo.App.IsReady()`。
//
// ⚠️ **第二步（`w.Focus()`）不需要任何前提**：它要的是「这一扇窗成为 key
// 窗口」，而那是**这一扇窗自己的事**——它跟应用 ready 没关系。
//
// ⚠️ **它开了一个「重试」的窗口**（票 #307 修 F1）：`activateIgnoringOtherApps:`
// **不是每次都成**——评审实测（PR #590 第 1 轮，2026-10-27 真机）：点封面
// 之后应用**压根没被激活**，于是 Esc 到不了这一扇窗。
// ⇒ 「**失败了就再试**」是办法，「多试几次」不是（它不是每次都失败）。
// ⇒ 重试那一档在 `miniCurtainFocusTick`（每帧一次）。
func (s *Shell) miniCurtainActivate(now time.Time) {
	// ⚠️ **顺序：先激活应用，再抢 key 窗口**——反过来（先
	// `makeKeyAndOrderFront:` 再 `activateIgnoringOtherApps:`）的话，激活应用
	// 那一刻系统会把 key 窗口交还给**前台那个应用自己的 key 窗口**——这一扇
	// 又变成「前台应用的一个非 key 窗」。
	if mygo.App.IsReady() {
		mygo.App.Focus()
	}
	// 🔴 **记下「我要这一扇拿键盘」**：上面那两句**都可能落空**（见
	// `Shell.miniCurtainWantsKey` 那一条长注释）——`activateIgnoringOtherApps:`
	// 是**异步**的，而 `makeKeyAndOrderFront:` 只在**应用已经是前台**的时候
	// 才把这一扇变成 key 窗口。
	//
	// ⇒ 这一句是**兜底的凭据**：`OnDidBecomeActive` 那一档看到「应用真成了
	// 前台」时会回来再抢一次 key 窗口（那时候这一句**一定**成）。
	s.miniCurtainWantsKey = true
	// 🔴 **开「重试」的窗口**（票 #307 修 F1）。
	//
	// ⚠️ **`need` 是「点封面那一刻应用**不是**前台」**：应用已经是前台的话，
	// 激活那一步**根本不需要**——而如果那时候还去重试，用户点别的应用之后
	// 我们就会**把他从他自己那件事上拽走**（`OnDidResignActive` 那一刻
	// `active` 会变假）。
	//
	// ⚠️ **第一次已经试过了**（上面那两句）——所以下一次重试在
	// `miniCurtainFocusDelay` 之后。
	s.miniCurtainFocus = miniCurtainFocusState{
		need:  !s.miniCurtainAppActive,
		until: now.Add(miniCurtainFocusWindow),
		next:  now.Add(miniCurtainFocusDelay),
	}
	// ⚠️ **激活之后再抢 key 窗口**：`mygo.App.Focus()` 只把**应用**带到前台，
	// 而前台应用的 key 窗口**不是这一扇**——于是键盘还是不在这一扇上。
	// `w.Focus()` 走 `makeKeyAndOrderFront:`，它把这一扇变成 key 窗口（而
	// 因为它是 Panel，**它自己不会**顺带激活应用——所以这一步得排在激活
	// 之后）。
	if w := s.miniWindow(); w != nil && !w.IsDestroyed() {
		w.Focus()
	}
}

// miniCurtainFocusTick 是「激活应用那一步」的重试档（票 #307 修 F1）。
//
// ⚠️ **它为什么存在**：评审实测（PR #590 第 1 轮，2026-10-27 真机）——
// **Finder 保持前台，点封面之后应用压根没被激活**，于是 Esc 到不了这一扇窗
// （而票面要求的就是「点封面 → 应用激活 → Esc → 收起」）。
//
// ⚠️ **而它不是每次都失败**（实测四轮里有一轮失败）——所以**重试**是办法，
// 「多试几次」不是。
//
// ⚠️ **它只重试「激活应用」那一半**：key 窗口那一半（`w.Focus()` →
// `makeKeyAndOrderFront:`）交给 `OnDidBecomeActive` 那一档
// （`miniCurtainEnsureKey`）——因为**在应用已经是前台的时候抢 key 窗口会把
// 键盘从主窗口抢过来**，而那时候用户可能正在主窗口里打字。
//
// ⚠️ **它每帧调一次**（`miniCurtainSyncHeight` 那一处）：迷你窗是**失焦照画**
// 的（`presence.go` 的 `freeWhileBlurred`），所以应用没被激活的时候**帧照出**
// ——于是这一档还有机会跑。
func (s *Shell) miniCurtainFocusTick(now time.Time) {
	if !miniCurtainFocusDue(now, s.miniCurtainWantsKey, s.miniCurtainFocus) {
		return
	}
	s.miniCurtainFocus.next = now.Add(miniCurtainFocusDelay)
	if mygo.App.IsReady() {
		mygo.App.Focus()
	}
}

// miniCurtainFocusDue 报「现在该不该再试一次激活应用」（票 #307 修 F1）。
//
// ⚠️ **它是一个纯函数**：那一档要判四件事，而**四件事里最难看清的是
// 「什么时候该收手」**——判错了的两种后果是**把用户从他自己那件事上拽走**
// （收手太晚）和**一次都没试**（收手太早）。⇒ 判据单独拎出来，才测得到。
//
// ⚠️ **四个条件**：
//
//  1. **已经不要键盘了 ⇒ 不再试**（`wantsKey` 为假 ⇒ 那一扇已经收起来了）。
//  2. **压根不需要激活 ⇒ 不试**（`need` 为假）：点封面那一刻应用**已经是**
//     前台——激活那一步不需要重试（而重试它会在用户点别的应用之后把用户拽
//     回来）。
//  3. **已经激活过了 ⇒ 收手**（`done`）：`OnDidBecomeActive` 到了就说明
//     激活成功了；**用户之后自己切走也不要再试**（那正是 `done` 与
//     `appActive` 分开记的原因）。
//  4. **窗口过了 / 还没到下一次 ⇒ 不试**（`until` / `next`）。
func miniCurtainFocusDue(now time.Time, wantsKey bool, f miniCurtainFocusState) bool {
	if !wantsKey {
		return false
	}
	if !f.need || f.done {
		return false
	}
	if !now.Before(f.until) {
		return false
	}
	if now.Before(f.next) {
		return false
	}
	return true
}

// miniCurtainFocusPending 报「激活应用那个重试窗口**还开着**」（票 #307 修
// F1 的第二半）。
//
// ⚠️ **它和 `miniCurtainFocusDue` 差在「到没到下一次重试的时刻」**：
// `Due` 问的是「**这一刻**该不该试」，而 `Pending` 问的是「**这个窗口**还
// 有没有戏」——后者是**帧源**的判据。
//
// 🔴 **为什么需要它**：迷你窗是**失焦照画**的（`presence.go` 的
// `freeWhileBlurred`），而它**自己不会**因为「还有重试要试」就多出一帧——
// 帧是**谁要**才出的。⇒ 展开那一段有帧（弹簧在动，`spring.moving`），弹簧
// 停稳之后**帧源就断了**，而那个 1200ms 的重试窗口**还剩大半**——于是后面
// 那几次重试**一次都没跑**（F1 那个症状还在）。
//
// ⇒ **重试窗口开着就要一帧**（`miniCurtainSyncHeight` 那一处）。
//
// ⚠️ **它不判断「该不该试」**：那还是 `Due` 的事（四个条件一个一个断，见
// 那个函数的注释）。这一位只管「**这个窗口还开着没有**」。
func miniCurtainFocusPending(now time.Time, wantsKey bool, f miniCurtainFocusState) bool {
	if !wantsKey {
		return false
	}
	if !f.need || f.done {
		return false
	}
	return now.Before(f.until)
}

// miniCurtainEnsureKey 在「应用真的成了前台」之后**再抢一次** key 窗口。
//
// ⚠️ **它是谁调用的**：`mygo.App.OnDidBecomeActive`（迷你那个外壳装的，
// 见 `attachMiniWindow`）。那一档跑的时候**应用已经是前台了**——于是
// `makeKeyAndOrderFront:` 这一句**一定**能把这一扇变成 key 窗口。
//
// ⚠️ **为什么还要这一档**：`miniCurtainActivate` 那两步**都可能落空**
// （`activateIgnoringOtherApps:` 是异步的，可能被别人抢回去；系统也可能
// 压根不激活）。⇒ 实测四轮里有一轮落空，而那一轮 Esc 到不了这一扇窗。
//
// ⚠️ **它只做「这一扇成为 key 窗口」**：**不再激活应用**（应用已经是前台
// 了，而 `activateIgnoringOtherApps:` 在这一档是多余的，还会把别的应用
// 从前台挤走）。
func (s *Shell) miniCurtainEnsureKey() {
	if !s.miniCurtainWantsKey {
		return
	}
	// ⚠️ **应用已经是前台了** ⇒ 重试那一档没必要了（`miniCurtainFocusTick`
	// 靠 `done` 收手）。
	s.miniCurtainAppActive = true
	s.miniCurtainFocus.done = true
	w := s.miniWindow()
	if w == nil || w.IsDestroyed() {
		return
	}
	w.Focus()
}

// miniCurtainMaxHeight 算「这一扇窗现在最多能长出多高的卷帘」。
//
// ⚠️ **它走的是「窗现在在哪」**，不是「窗应该在哪」——用户拖到哪儿就在
// 哪儿算，而那个位置是这一扇窗自己的（`StateKey` 记住的）。
func (s *Shell) miniCurtainMaxHeight() int {
	w := s.miniWindow()
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
	return miniCurtainHeight(s.miniCurtainWorkAreaOf(w), w.Bounds())
}

// miniCurtainWorkAreaOf 是那扇窗所在显示器的工作区。
//
// ⚠️ **零值 = 「不知道」**：没有真窗口（无头测试里）、或者窗已经关了。那时
// `miniCurtainHeight` 返回 0（不展开）——而**这不是「卷帘坏了」**，是「这一
// 扇窗的位置没人知道」。
//
// ⚠️ **`mygo.Screen` 头上有 `needsApp`**（`mygo/app.go:128`）：应用还没
// ready 的时候调它直接 panic。真窗口在 `App.WhenReady` 之后才有，所以这一行
// 只在应用 ready 之后才走到。
//
// ⚠️ **它走的是 `miniWindow` 那一层接口**：于是测试实现（假窗）可以把工作区
// **摆进来**，不必真的问系统——而生产那一档问的是真的（`mygo.Screen`）。
func (s *Shell) miniCurtainWorkAreaOf(w miniWindow) mygo.Rectangle {
	if w == nil || w.IsDestroyed() {
		return mygo.Rectangle{}
	}
	return w.WorkArea()
}

// miniCurtainCollapse 收起卷帘：窗高回到 84。
//
// **幂等**（收着的时候再调一次什么也不做）。
func (s *Shell) miniCurtainCollapse(now time.Time) {
	// ⚠️ **收起了就不再惦记「抢键盘」**：那一格是展开那一下的凭据
	// （`Shell.miniCurtainWantsKey`），而留着它的话，应用**下一次**因为别的
	// 原因成了前台时，`OnDidBecomeActive` 那一档会去抢这一扇的 key 窗口——
	// 而那时候卷帘已经收着了，用户根本没在用它。
	s.miniCurtainWantsKey = false
	if !s.miniCurtain.open && s.miniCurtain.height <= 0 {
		// 已经收着了。⚠️ **但窗高还是得钉回 84**：上一次收起的动画中途窗被
		// 拖走了、或者展开到一半窗被关掉，收起这一句没走完——窗就留在一个
		// 高于 84 的状态，而 `StateKey` 会把那个高度记下来。
		s.miniCurtainSetHeight(miniHeight)
		return
	}
	s.miniCurtain.open = false
	// ⚠️ **弹簧朝 0 走**（票 #307）：收起是**过阻尼**的（ζ = 1.1）⇒ 一路
	// 收回 84，中间**没有过冲**（而「不弹」是票面点名要的）。
	//
	// ⚠️ **弹簧的 `p` 还在 1**（它是从那儿往 0 走的）——而画出来的高度
	// 一路上都在缩，窗也跟着缩 ⇒ **每一帧窗高 == 内容高**。
	s.miniCurtain.spring.start(0, now)
}

// miniCurtainCollapseNow **立刻**收起卷帘：不等那几百毫秒的弹簧。
//
// ⚠️ **它用在两条路上**：
//
//  1. **设置开关被关掉**（票面：「展开着的时候把开关关掉 ⇒ **立刻收起**」）；
//  2. **关窗 / 退出**：那里窗马上就要没了，滑下去那几百毫秒是白滑的——而
//     更要紧的是**落盘的尺寸**（`StateKey` 记宽高，抓的是关窗那一刻）。
//
// ⚠️ **它把弹簧也一起钉到 0**：不然下一帧那个弹簧还会从 1 往 0 走——而
// 那时候窗已经关了或者开关已经关了，滑下去的那一段是纯浪费，而**中间那
// 几帧窗比内容高**（透明的那一块在吃点击）。
func (s *Shell) miniCurtainCollapseNow() {
	s.miniCurtain.open = false
	// 同 `miniCurtainCollapse`：收起了就不再惦记「抢键盘」。
	s.miniCurtainWantsKey = false
	s.miniCurtain.height = 0
	s.miniCurtain.setHeight = miniHeight
	// ⚠️ **弹簧也要归零**：只把 `height` 归零的话，下一帧那个弹簧会从 1
	// 往 0 走——窗于是**先弹回 320 再缩到 84**，那几百毫秒里窗比内容高
	// （透明的那一块在吃点击），而票面要的是「**立刻收起**」。
	s.miniCurtain.spring.reset()
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
	w := s.miniWindow()
	if w == nil || w.IsDestroyed() {
		return
	}
	width, _ := w.Size()
	if width <= 0 {
		width = miniWidth
	}
	w.SetSize(width, h)
}

// miniWindow 是「迷你播放器这一扇窗」那一层接口。
//
// ⚠️ **它为什么存在**：卷帘要改的是**窗的原生尺寸**，而无头 Tester 里**没有
// 真窗口**（`mygo.NewWindow` 在应用没 ready 时直接 panic，
// `mygo/window.go:382`）。于是「关窗之前把窗高改回 84 ⇒ `StateKey` 落盘的
// 是 320×84」这条**在无头那一档量不到**。
//
// ⇒ 抽这一层接口，生产实现是 `*mygo.Window`，而测试实现是一个**假窗**
// （记住最后一次 `SetSize`，能手动触发 `OnClose`）。这样「关窗钩子有没有把
// 高度改回 84」就变成**可测的**——而**删掉那个钩子，这条用例必须红**。
//
// ⚠️ **别把 `s.win` 的类型整个换掉**：`s.win` 在十几处被当 `*mygo.Window`
// 用（`platform.RestoreFromBackground(s.win)` 那一类），换类型会波及一大片
// 与这一票无关的代码。所以这里只抽**迷你那一扇**要的那几样。
type miniWindow interface {
	// Size 是窗现在的宽高。
	Size() (width, height int)
	// SetSize 改窗的尺寸（原生那一边的事）。
	SetSize(width, height int)
	// Bounds 是窗现在在哪、多大。
	Bounds() mygo.Rectangle
	// WorkArea 是这扇窗所在显示器的工作区（**零值 = 「不知道」**）。
	//
	// ⚠️ **它在这一层上，是为了无头那一档**：生产实现问的是
	// `mygo.Screen.DisplayMatching(win).WorkArea`，而 `mygo.Screen` 头上有
	// `needsApp`（`mygo/app.go:128`）——应用没 ready 的时候调它**直接
	// panic**，而无头 Tester 里压根没有应用。所以测试实现把工作区**摆进来**，
	// 而「卷帘不该长出工作区」这件事本身是纯逻辑（`miniCurtainHeight`）。
	WorkArea() mygo.Rectangle
	// Native 是底下那扇**真窗**（测试实现返回 nil）。
	//
	// ⚠️ **它只有一个用处**：生产那一档有几样东西**只对真窗成立**（把
	// `s.win` 指过去、登记唤醒句柄、接可见性回调）——而「接可见性回调」要
	// 的是 `*mygo.Window` 那一套 API。
	Native() *mygo.Window
	// IsDestroyed 报这扇窗还在不在。
	IsDestroyed() bool
	// Focus 让这扇窗成为 key 窗口（**不**激活应用——它是 Panel）。
	Focus()
	// OnClose 装「关窗之前」那个钩子。
	OnClose(fn func(e *mygo.CloseEvent)) (off func())
}

// miniWindowOf 是 `*mygo.Window` 那一层接口的实现。
type miniWindowOf struct{ w *mygo.Window }

func (m miniWindowOf) Size() (int, int)          { return m.w.Size() }
func (m miniWindowOf) SetSize(width, height int) { m.w.SetSize(width, height) }
func (m miniWindowOf) Bounds() mygo.Rectangle    { return m.w.Bounds() }
func (m miniWindowOf) IsDestroyed() bool         { return m.w.IsDestroyed() }
func (m miniWindowOf) Focus()                    { m.w.Focus() }
func (m miniWindowOf) WorkArea() mygo.Rectangle {
	return mygo.Screen.DisplayMatching(m.w.Bounds()).WorkArea
}
func (m miniWindowOf) Native() *mygo.Window { return m.w }
func (m miniWindowOf) OnClose(fn func(e *mygo.CloseEvent)) (off func()) {
	return m.w.OnClose(fn)
}

// miniWindow 返回迷你播放器这一扇窗的那层接口。
//
// ⚠️ **它只返回 `s.miniWin`，不临时造一个**：那一层接口是**一个结构体**
// （`miniWindowOf`，里面一个指针），而**每次返回一个接口值都要装箱一次**
// ——装箱逃逸到堆上，而这一句**每一帧都要调两三次**（`miniCurtainSetHeight`
// 一次、`miniCurtainMaxHeight` 一次）。⇒ 每帧多一到两次分配，而那一扇窗
// **失焦照画**（面板语义，见 `presence.go`）——于是那一档的分配是**一直**在
// 付的（帧预算那条测试数的就是它）。
//
// ⇒ 所以 `s.miniWin` 是**建窗时存好的那一个**（`attachMiniWindowTo` 装），
// 这里**只是把它读出来**——零分配。
//
// ⚠️ **零值 = 「没有窗」**：迷你窗建出来之前（`ensureMiniWindow` 排进
// `WhenReady` 之前）它就是 nil，而那一档要的是「窗高这一层逻辑对不对」。
func (s *Shell) miniWindow() miniWindow {
	return s.miniWin
}

// miniCurtainTargetHeight 是这一帧**窗该有**多高。
func (s *Shell) miniCurtainTargetHeight() int {
	if !s.miniCurtain.open {
		return miniHeight
	}
	h := s.miniCurtainFullHeight()
	if h <= 0 {
		return miniHeight
	}
	return miniHeight + h
}

// miniCurtainFullHeight 报卷帘**全开**时有多高（像素，**不含**那 84）。
//
// ⚠️ **它跟「开着还是收着」是两件事**（票 #307 的坑）：收起的那一下
// `s.miniCurtain.open` **立刻**就变成 false 了（那是「用户的意图」），而
// **卷帘的高度要再花几百毫秒才缩回 0**（弹簧那一段）。
//
// ⇒ 画出来的高度 = **比例（弹簧）× 全开高（这个函数）**，而**不是**
// 「比例 × 目标高」——目标高在收着的时候是 0（`miniCurtainTargetHeight`），
// 拿它当比例的基数的话，**收起就变成「一帧消失」**（而票面要的是「像手
// 卷上去一样」，几百毫秒的一段）。
func (s *Shell) miniCurtainFullHeight() int {
	if h := s.miniCurtain.target; h > 0 {
		return h
	}
	// ⚠️ **算一次就记住**（`target`）：`miniCurtainMaxHeight` 要问原生那一
	// 边「这一扇窗现在在哪」，而那是**每帧都问不起**的（迷你窗是失焦照画
	// 的，那一笔一直在付）。
	//
	// ⚠️ **而它跟 `miniCurtainTargetHeight` 的差别是「不跟着 `open` 走」**：
	// 收起的那一段（`open` 已经是 false 了）还要靠它算高度。
	h := s.miniCurtainMaxHeight()
	if h <= 0 {
		return 0
	}
	s.miniCurtain.target = h
	return h
}

// miniCurtainSyncHeight 把卷帘高度那个**弹簧**推一帧，并把窗口高度改成
// 「84 + 这一帧的卷帘高」。
//
// ⚠️ **它每帧都要调**（`viewMini` 开头那一支），因为两样东西都在动：
// 弹簧在动，而**窗口高度必须跟着它动**——这一扇窗是透明的，而透明的那
// 一块照样吃点击，窗比内容高 20 像素，底下那个应用的按钮就点不到那 20
// 像素（文件头第 1 条）。
//
// ⚠️ **旧版这里走的是框架那条 `ui.Animation` 通道**（`AnimateTo` / `Jump`，
// 那个「空的、键固定的」`Box` 是动画值的宿主）。票 #307 把它换成了**自己
// 积分的弹簧**——而**那个空的 `Box` 那一支整个删掉了**：弹簧的状态挂在
// `Shell.miniCurtain.spring` 上（不是元素上），所以它**不需要**一个每帧
// 都建的宿主元素。
//
// ⚠️ **收起的那一段也要有帧**：弹簧走的时候**这里自己要帧**
// （`c.AnimationFrame()`），而**窗口高度是原生那一边的事**——原生改完
// 自己会出帧（`setFrame:` → `SurfaceResize`），所以两边各走各的那一半，
// 合起来是同步的。
func (s *Shell) miniCurtainSyncHeight(c *ui.Context) {
	// 🔴 **重试「激活应用」那一半**（票 #307 修 F1）。
	//
	// ⚠️ **它每帧都要调**：迷你窗是**失焦照画**的（`presence.go` 的
	// `freeWhileBlurred`），所以**应用没被激活的时候帧照出**——于是这一档
	// 还有机会跑（而那正是 F1 复现出来的那个状态）。
	s.miniCurtainFocusTick(c.Now())

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
	// ⚠️ **Esc 要先到这一扇窗，而它靠的是「展开那一下把键盘抢过来」**：
	// Panel 自己不激活应用（`mygo/internal/darwin/window.go:806` 那个
	// `if !w.opts.Panel` 才 `activateIgnoringOtherApps`），所以**光点封面
	// 是不够的**——键盘还在原来那个应用手里，Esc 走的是那个应用的窗口。
	// ⇒ 展开那一下要 `mygo.App.Focus()` + `w.Focus()`（`miniCurtainActivate`）
	// 把应用和这一扇窗都抢过来；**而那一步不是每次都成**，所以还有
	// `miniCurtainFocusTick` 那个重试档（票 #307 修 F1）。
	//
	// ⚠️ **框架自己那条「Escape 关掉 Panel」别去抢**：`mygo/flyout.go:244`
	// 那个 `escape()` 在**键没人要**的时候才跑（`mygo/ui/input.go:934`），
	// 而它关的是整扇窗（迷你窗关了，用户就没得玩了）。而我们这一句**先**
	// 要了这个键——`rt.consumed` 一置，框架那一条就不会跑。
	if s.miniCurtain.open && c.Shortcut(0, ui.KeyEscape) {
		s.miniCurtainCollapse(c.Now())
	}

	// 🔴 **弹簧推一帧**（票 #307）。
	//
	// ⚠️ **它返回的是「画出来的高度」**（0~1），不是弹簧内部那个 `p`——
	// 过冲被翻成「回弹时略微缩回去一点」（镜像），所以这个值**永远 ≤ 1**
	// ⇒ **窗高永远 ≤ 84 + 卷帘高**（文件头第 1 条那条坑不会回来）。
	//
	// ⚠️ **「立刻收起」那一下已经把弹簧钉到 0 了**（`miniCurtainCollapseNow`
	// 里 `spring.p = 0` / `spring.v = 0` / `moving = false`）——所以这里推
	// 出来的是 0，而**窗于是这一帧就已经是 84**，不会「先弹回 320 再缩」。
	//
	// ⚠️ **目标高度（像素）先算出来**：弹簧走的是**相对量**（0~1），而窗高
	// 是**像素**——所以「这一帧该多高」= 比例 × 目标像素高。
	//
	ratio := s.miniCurtain.spring.step(c.Now())
	// ⚠️ **全开高跟「开着还是收着」无关**（票 #307 的坑）：收起的那一下
	// `open` **立刻**就变成 false 了，而**卷帘还要再花几百毫秒才缩回 0**。
	// ⇒ 基数是 `miniCurtainFullHeight`（**不**是 `miniCurtainTargetHeight`
	// ——后者在收着的时候是 84，拿它当基数，收起就变成「一帧消失」）。
	//
	// ⚠️ **比例是 0 的时候不必问原生**（`miniCurtainFullHeight` 会问）：
	// 比例 0 ⇒ 高度 0 ⇒ 与基数无关。
	full := 0
	if ratio > 0 {
		full = s.miniCurtainFullHeight()
	}
	h := float32(ratio * float64(full))
	s.miniCurtain.height = h

	// ⚠️ **弹簧还在走就要下一帧**（`c.AnimationFrame()`）：不然弹簧停在半路
	// ——而迷你窗是**失焦照画**的（面板语义），多要一帧就是多一次整窗重建，
	// 而**停稳之后就不要了**（`moving` 变 false）。
	if s.miniCurtain.spring.moving {
		c.AnimationFrame()
	}

	// 🔴 **「激活应用」那个重试窗口开着的时候也要一帧**（票 #307 修 F1 的
	// 第二半）。
	//
	// ⚠️ **为什么不能只看弹簧**：弹簧停稳之后帧源就断了（上面那一支只在
	// `moving` 的时候要帧），而那个 1200ms 的重试窗口**还剩大半**——于是
	// 后面那几次重试**一次都没跑**，F1 那个症状（点封面之后应用没被激活）
	// **还在**。
	//
	// ⚠️ **这一支的代价是「多画几帧」**：迷你窗是失焦照画的，每一帧都是
	// 一次整窗重建。而它**只在那个窗口开着的时候**要帧——而那个窗口
	// 一开就是「应用**没有**被激活」（激活成功的话，`OnDidBecomeActive`
	// 那一档会把 `done` 置上，窗口当场收手，见 `miniCurtainEnsureKey`）。
	if miniCurtainFocusPending(c.Now(), s.miniCurtainWantsKey, s.miniCurtainFocus) {
		c.AnimationFrame()
	}

	// ⚠️ **窗口高度跟的是「画出来的高度」，不是「目标高度」**：弹簧走到一半
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
			snap := s.app.Lyrics()
			s.viewMiniCurtainBody(c, st, snap, t, h)
			// ⚠️ **「歌词来自 B 站」那一行是卷帘的**绝对定位**孩子**（票
			// #307 §②）：它**不能**占歌词区的高度——
			//
			// 歌词区的高度是「当前句贴着下沿」那个算法里的一个数（顶上那段
			// 留白正好是**一整屏高**，见 `minicurtain_lyrics.go` 的文件头），
			// 而它得**正好等于**歌词区实际的高。⇒ 说明句只能**画在歌词区
			// 外面**。
			//
			// ⚠️ **它画在卷帘的左上角**（不是右上角）：右边是「收起」那一枚
			// 按钮的地盘（见 `viewMini` 那一支），而左边是歌词的起点。
			s.viewMiniCurtainOrigin(c, snap, t)
		})
}

// viewMiniCurtainBody 是卷帘里面那一块：歌词（或者「暂无歌词」）。
//
// ⚠️ **它底下留了 14 的边距**（下轴那一道）：票面 §② 要的是「歌词在下轴
// **之上**」——而**贴着下沿**就是贴着**下轴的上沿**。
//
// ⚠️ **这 14 也决定了「歌词区有多高」那个数**（`viewMiniCurtainLyrics` 的
// `viewH`）：而那个数必须是**正好**的（顶上那段留白要等于它），所以这里
// 的边距和 `viewH` 必须是同一个数。
func (s *Shell) viewMiniCurtainBody(c *ui.Context, st app.State, snap app.LyricsSnapshot, t *ui.Theme, h float32) {
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
		// ⚠️ **底下留出下轴那一道**（14）：歌词**贴着下沿**，而下沿就是下轴
		// 的上沿（票面 §②）。
		Padding(space.None, space.None, miniCurtainAxisH, space.None).
		Children(func() {
			if active < 0 {
				// ⚠️ **「还没好」和「没有」两档分开**（2026-10-27）：换歌之后歌词
				// 要先读盘、等字幕、必要时自动找一次。原先那段时间直接写「暂无
				// 歌词」——用户读到的是结论，而真相是过程。
				//
				// ⚠️ **加载中是一枚转圈**（不是一行字）：这一档说的是过程，
				// 而「暂无歌词」是一个结论——两档摆成一样就是把过程说成结论。
				if snap.Loading {
					s.viewMiniCurtainLoading(c, t)
					return
				}
				s.viewMiniCurtainEmpty(c, t)
				return
			}
			// 🔴 **歌词贴着下沿排**（票 #307 §②）：见
			// `minicurtain_lyrics.go` 的文件头。
			//
			// ⚠️ **`viewH` = 歌词区这一帧有多高** = 卷帘高 − 下轴那一道
			// （14）——而它**必须正好**是歌词区实际的高（顶上那段留白要等于
			// 它，见那个文件头）。
			s.viewMiniCurtainLyrics(c, snap, active, t, h-miniCurtainAxisH)
		})

	if active < 0 {
		return
	}
	// ⚠️ 放在整棵树建完之后：它要用容器和正在唱那一句的框，而那两个是
	// **上一帧**量到的（理由同 lyrics.go 里那一处）。
	s.miniCurtainLyricPlace(c, active, lyricShapeOf(snap.Rows))
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

// viewMiniCurtainOrigin 是卷帘左上角那一行「歌词来自 B 站 / 歌词来自搜索」。
//
// ⚠️ **它是卷帘的绝对定位孩子**（票 #307 §②）：它**不能**占歌词区的高度
// ——「当前句贴着下沿」那个算法里，「歌词区有多高」是一个**要正好对上**的
// 数（顶上那段留白正好是**一整屏高**，见 `minicurtain_lyrics.go` 的文件
// 头），而这一行要是占了高度，那个数就差了 20 多像素 ⇒ 当前句就贴不到
// 下沿。
//
// ⚠️ **它画在左上角**：右边留给「收起」那一枚按钮（它在**下轴**上，见
// `viewMini` 那一支），而左边是歌词的起点。
//
// ⚠️ **它跟满屏那一屏的那一行是同一个来源**（`lyricOriginText`）：那一句
// 说的是「这份歌词是怎么来的」，而用户分不清 B 站带来的和搜来的那两种。
func (s *Shell) viewMiniCurtainOrigin(c *ui.Context, snap app.LyricsSnapshot, t *ui.Theme) {
	text := lyricOriginText(snap.Origin)
	if text == "" {
		return
	}
	ui.Box(c).Key(miniCurtainOriginKey).
		Absolute().Top(space.Px4).Left(miniCurtainPadX).
		Children(func() {
			ui.Text(c, text).FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted)
		})
}

// viewMiniCurtainLoading 是「这一首的歌词还在路上」那一档。
//
// ⚠️ **只有一枚转圈**：空状态那一句「暂无歌词」是一个结论，而这一档说的是
// 过程——摆成一样的话用户会把「还没好」读成「没有」（这条规矩见
// `empty.go` 的 `emptyState.Loading`）。
//
// ⚠️ **它和 `miniCurtainEmptyKey` 分开**：同一个键上换内容，读屏软件读到的是
// 同一句话，而这两档说的是两件事。
func (s *Shell) viewMiniCurtainLoading(c *ui.Context, t *ui.Theme) {
	ui.Box(c).Key(miniCurtainLoadingKey).Grow(1).FillWidth().Center().
		Children(func() {
			ui.Spinner(c).Label(lyricsLoadingText).TextColor(t.TextMuted)
		})
}

// viewMiniCurtainCollapse 是卷帘里那一枚「收起」。
//
// ⚠️ **它和「再点封面」是同一件事**（收起），而 Esc 是第三件。三条路都在。
//
// 🔴 **它画在**下轴**上**（票 #307 §②）：而它**原来**在卷帘的右下角
// （下轴之上 4 像素）——那是「歌词当前句居中」时候的位置。
//
// ⚠️ **而现在歌词贴着下沿**（票面 §②：当前句的下沿就压在下轴的上沿上）
// ⇒ 那一枚键**正好压在当前句那一行上**（当前句是放大的 19.5px，而键是
// 16px）——一个长句子（320 宽里能放十五个字）会从**键底下穿过去**。
//
// ⇒ **它挪到下轴那根杆上**（根的绝对定位孩子，排在轴**之后**画，所以它
// 压在那根杆上面）：「把杆子往上提，收起来」——而它**不再挡歌词**。
func (s *Shell) viewMiniCurtainCollapse(c *ui.Context, t *ui.Theme) {
	// ⚠️ **它绝对定位在窗的右下角**（不是跟着词滚）：它是**控件**（要按得
	// 到），而歌词区是内容——放进滚动容器的话它会跟着词滚走。
	//
	// ⚠️ **它坐在下轴那根杆上**：下轴是 14 高、贴着窗的下沿，而这一枚是
	// 14 ——**一样高**，于是它正好在那根杆上（理由见上面那一段）。
	//
	// ⚠️ **右边留 8**：窗的下两个角是 7 的圆角（`miniplayer.go` 那一段），
	// 而根是 `Clip()` 的——再往右一点，这一枚的角会被那个圆角裁掉。
	ui.Box(c).Key(miniCurtainCollapseKey).
		Absolute().Right(space.Px8).Bottom(space.None).
		Children(func() {
			if flatIconButton(c, iconChevronUp, "收起歌词", miniCurtainAxisH).Clicked() {
				s.miniCurtainCollapse(c.Now())
			}
		})
}
