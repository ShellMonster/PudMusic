// Package motion 是全应用的动效值表：曲线、弹簧、缩放。
//
// # 为什么单开一个包
//
// 动效这一类值散得比间距还彻底，而且**散得没有出处**：时长多半是抄上游某
// 一处的，缓动则一律是**框架的默认**（`mygo/ui/motion.go` 里 `Ease` 留空
// 就是 `EaseOut`）。DESIGN.md §五 在本票之前**一个字都没写曲线**——
// 于是「同一种过渡在四个地方是不是同一条曲线」这件事，没有人能答上来。
//
// 做法跟 `widgets/space`（间距）、`widgets/leading`（行高）、`widgets/scale`
// （字号）一样：**值只写在这里，调用处只说「要哪一档」**。
//
// # 一条统一曲线
//
// `Unified` 是全应用默认的那一条（DESIGN.md §五《一条统一曲线》那一节有
// 出处和理由）。⚠️ **它是最该只写一处的一个**：这一条曲线前 50% 的时间走完
// 95%，所以「同一种过渡在不同地方走的是同一条曲线」这件事**肉眼看得出来**
// ——有一处漏了，两处就「不是一回事」了。
//
// # 弹簧
//
// `TabSlide` 是主 Tab 滑块那一下的动画。它**不是**曲线，是弹簧——因为那一处
// 要的是「打断时接着走」，那是曲线给不了的（理由见它自己的注释）。
//
// # 缩放
//
// `hoverzoom.go` 是「指针移上去放大一点」那一层（1.05 / 300ms / `ui.EaseInOut`），
// `zoom.go` 是「弹出来」和「停在上面长大一点」那两档（0.85 → 1、1 → 1.1，
// 都 150ms / `Unified`）。
//
// ⚠️ **这一包住在 `widgets/` 底下，是因为它得被两个方向都用**：封面那一族在
// `widgets/hover`（package hover），而顶栏头像和 UP 空间那一族在
// `internal/ui`（package ui）——而 `internal/ui` import 了 `widgets/hover`，
// 反过来 import 会成环（票 #467 的那一次下沉就是这件事）。
package motion

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// Unified 是全应用默认的缓动曲线：一条 ease-out。
//
// 出处：《回顾: 制作微信广告官网》——「**统一选择 `0.8s` 与贝塞尔控制点
// `(0.16, 0.9, 0.21, 0.98)`**」（https://wxad.design/abc/wxad）。
//
// ⚠️ **时长和曲线必须一起定**：「运动时间与贝塞尔曲线需同时考虑，抛开一个去
// 单谈另一个是没有意义的」（同上）。所以 DESIGN.md §五 那张表里时长和曲线
// 是**一对**，不要只改其中一个。
var Unified = ui.CubicBezier(0.16, 0.9, 0.21, 0.98)

// TabSlide 是主 Tab 滑块那一下的动画：**一条接近临界阻尼的弹簧**（ζ=0.9923），
// 不是定时长曲线。
//
// 出处：《使用 AI 制作交互动画的建议》内嵌 demo 的 Tab 指示器
// （`useSpring(targetX, { stiffness: 260, damping: 26 })`，
// https://wxad.design/abc/interactive-design-in-ai）。
//
// ⚠️ **阻尼从 26 上调到 32**：那个值的阻尼比 ζ=0.806，**会回弹约 2%**，而
// 手册⓪ 说「Start simple, not springy」、回弹只留给「奖励动量」——点 Tab 是
// **点击触发**，不是甩动，不该弹。
//
// ## 为什么这一处是弹簧而不是曲线
//
// 定时长曲线中途换目标会**从当前值重新起速**（速度归零 → 顿一下）；
// 弹簧保留速度（`mygo/ui/motion.go` 的 `channel.retarget`），连点三个 Tab
// 时滑块是**接着走**的。这是曲线给不了的那一条。
//
// ## 这几个数是多少（实算，不是估的）
//
//	ζ = 32 / (2·√260) = 0.9923   （临界阻尼略偏欠阻尼）
//	过冲 = exp(-πζ/√(1-ζ²)) ≈ 1.2e-11 DIP   （肉眼为零）
//	走完 98%（离目标 2%）要 357ms
//	离目标 0.5 DIP 时收手：453ms
//
// ⚠️ **收手比「看上去走完」晚**：弹簧不像曲线那样有一个明确的终点，它是一路
// 收敛过去的。精度就是「差多少算到了」——框架默认的 0.001 DIP 会让它再跑
// 400ms 才停（那 400ms 里画面一动不动，纯粹在占帧），所以这里写 0.5。
//
// ⚠️ **比旧的曲线慢**：旧的 250ms `EaseOut` 在 250ms 就 100%，这条弹簧在
// 250ms 时才走到 91%、357ms 才到 98%。换来的是「能被打断、接着走」。
var TabSlide = ui.Animation{Spring: ui.SpringOptions{
	Stiffness: 260,
	Damping:   32,
	// 0.5 DIP = 半个像素：肉眼看不出来，但足够让弹簧收手（见上面的实算）。
	Precision: 0.5,
}}

// 一条定时长的曲线：满屏播放器封面那一圈。
//
// # 转速是**自定值**，不是从 QQ音乐 量来的
//
// QQ音乐 客户端播放页那个「透明彩胶」的官方图是**静态图**，录屏里也只有静止帧
// ——**取不到转速**（报告 BMG-254 §3.6 的 QQ-11）。所以这里按**行业惯例**取：
// 一张黑胶 **33⅓ rpm**（一圈 1.8 秒）。
//
// ⚠️ **改这个数之前先想清楚**：它不是抄来的，改它就是改我们的口味——而这一条
// 写在收尾说明里是为了让人知道它是**推的**，不是量的。
const (
	// CoverSpinPeriod 是满屏播放器封面转一圈要多久（33⅓ rpm）。
	CoverSpinPeriod = 1800 * time.Millisecond
	// CoverSpinTurn 是一圈多少度。
	CoverSpinTurn = 360.0
	// CoverSpinSpeed 是每秒转多少度（360 度 ÷ 1.8 秒 = 200 度/秒）。
	//
	// ⚠️ **它和上面那两个是一个数**：改 Period 或 Turn 时要连它一起改
	// （`motion_test.go` 有一条测试钉着这三个数的关系）。
	CoverSpinSpeed = 200.0
)

// CoverSpin 是满屏播放器封面那一圈的动画：**一条线性曲线**，走满一整圈。
//
// ⚠️ **它不跟统一曲线**：匀速转不是一段过渡，没有 ease-out 可言——统一曲线
// 那一族全是「前重后轻」，拿它来转圈等于圈越转越慢。
//
// ⚠️ **时长是一圈**：目标定在「当前角度 + 一整圈」，曲线也走一整圈，于是速度
// 恰好是 `CoverSpinSpeed`（匀速）。见 `internal/ui/fullplayer_spin.go`。
var CoverSpin = ui.Animation{Duration: CoverSpinPeriod, Ease: ui.Linear}

// Snap 是「一次到位」：一个 1ns 的线性曲线。
//
// ⚠️ **不能用 `ui.Animation{}`**：它的零值是 `Smooth` 那条 500ms 的弹簧
// （`mygo/ui/motion.go:33`），而那正好是「慢慢追」——要「立刻」就得写这条
// （框架的 `channel.step` 里 `t >= 1` 直接取 target，下一帧就到位）。
//
// （`internal/ui/lyrics.go` 的 `lyricSnapAnim` 是同一个东西的另写一份。）
var Snap = ui.Animation{Duration: time.Nanosecond, Ease: ui.Linear}
