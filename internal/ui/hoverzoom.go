// 「指针移上去放大一点」那一层：三处共用。
//
// # 上游只有三处
//
//	UP 空间 / 合集网格的封面	`user-profile/grid-card/index.tsx:26`
//	动态流作者栏的头像		`dynamic-feed/user.tsx:46`
//	顶栏那个头像			`layout/navbar/user/index.tsx:178`
//
// 三处的类名都是 `hover:scale-105`（网格那一处还带 `transition-transform
// duration-300`），所以**倍数和时长只有一套**——写三遍就会漂开，而漂开的那一处
// 在画面上只是「这一块好像没那么活」。
//
// # 为什么用 transform 而不是改尺寸
//
// `Scale` 是**画的时候**的变换（`mygo/ui/transform.go`：它改的是画出来的东西，
// 布局留给它的位置不动）。改尺寸的话那一格会**重排**——封面长大 5% 就把旁边的
// 字挤走了，而上游那一处是整块图在框里放大（`overflow-hidden` 裁着）。
//
// ⚠️ DESIGN.md §5 有一条：「动画不要用 transform 动**大子树**」——软件渲染回退
// 时整棵子树会逐像素重绘。这里动的是**一张图 / 一个头像**，不是大子树。
//
// # 动画的值记在节点上
//
// `AnimateWith` 的值存在**节点的状态**里（`mygo/ui/motion.go` 的 `st.motions`），
// 而状态是**按身份**跨帧留下来的（同一个 key / 同一个位置就是同一个身份）。
// 所以这一层要求：
//
//   - 那一格**每一帧都要重建**（不能是「指针不在就不建」——那样节点每帧都是新的，
//     动画直接给到终点，放大变成「啪」地一下出现，见 `widgets/hover` 的文件头）；
//   - **身份要稳定**：给一个自己的 key，别靠「父 ID + 兄弟序号」。
package ui

import (
	"time"

	"github.com/egoist/mygo/ui"
)

const (
	// hoverZoomScale 是放大到的倍数。**105%**，抄上游那三处。
	//
	// ⚠️ 不是 110%：110% 是**列表行**那个播放三角的（上游
	// `music-list-item/index.tsx:105` 的 `group-hover:scale-110`），两处别混。
	hoverZoomScale = 1.05

	// hoverZoomTime 是放大的时长。**300 毫秒**——上游网格那一处写的是
	// `duration-300`（`user-profile/grid-card/index.tsx:26`）。
	//
	// 另两处没写时长，走的是 Tailwind `transition-transform` 的默认那一档
	// （`cubic-bezier(0.4, 0, 0.2, 1)`）。
	//
	// ⚠️ **那条曲线不是 `ui.EaseInOut`**：实算（同一个 mygo v0.6.2）——
	// t=0.25 时上游那条走 23.7%、`ui.EaseInOut` 只走 6.3%；t=0.5 时是
	// 77.6% 对 50.0%。**差得很远**（这条由 `hoverzoom_test.go` 的
	// `TestTheHoverZoomCurveIsNotTheUpstreamOne` 钉着）。
	hoverZoomTime = 300 * time.Millisecond
)

// hoverZoom 给 e 装上「指针在它上面就放大到 105%」这一层，返回同一个元素。
//
// on 是**这一帧**指针在不在它上面。key 是这一格的身份：**每一处都要给自己的**
// ——两处用同一个 key 的话它们会共用一份动画状态（一个放大另一个跟着放大）。
//
// ⚠️ **缓动用 EaseInOut**：框架 `AnimateTo` 的默认是 ease-out（先快后慢），
// 那一档在这儿看着是「弹」一下而不是「涨」上去——所以这一处用起步慢、收尾稳
// 的 ease-in-out。
//
// ⚠️ **这一处不跟统一曲线**（票 #292 定的，DESIGN.md §五 那一节记着）：它是
// **指针跟随**的连续动画，不是点击触发的过渡——手册⓪ 反对 ease-in 的是
// 「我不再是被动的观看者，而是主动的操作者」那一种（关弹窗、点按钮）；指针停
// 上去时放大属于「非交互动画」，手册自己写着「交互动画和非交互（视觉）动画
// 是有差异的」。ease-in-out 那个起步慢、收尾稳的手感正好跟住指针。
//
// （另外两处也不跟统一曲线：主 Tab 滑块走弹簧、音量旋钮那两处 150ms 的交叉
// 淡入走框架默认的 `EaseOut`——见 DESIGN.md §五《逐处》表外那一段。）
//
// ⚠️ **我们这条也不是上游那条**：上游那两处没写时长，走的是 Tailwind
// `transition-transform` 的默认档 `cubic-bezier(0.4, 0, 0.2, 1)`，而我们用的是
// `ui.EaseInOut`（框架那条对称三次曲线）——**两者差得很远**（t=0.5 时 77.6% 对
// 50.0%，见上面 `hoverZoomTime` 那一段）。**这是一个已知偏离**：票 #292 裁决
// 这一处不动（理由如上），但要「精确复刻上游」的话写的是
// `ui.CubicBezier(0.4, 0, 0.2, 1)`，**观感会明显不一样**。
func hoverZoom(c *ui.Context, e ui.Element, key string, on bool) ui.Element {
	target := float32(1)
	if on {
		target = hoverZoomScale
	}
	v := e.AnimateWith(key, target, hoverZoomTime, ui.EaseInOut)
	return e.Scale(v, v)
}
