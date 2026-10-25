// 缩放那一档的两条：「弹」出来，和「指针停上去时长大一点」。
//
// # 两条都是**画的时候的变换**
//
// `Scale` 改的是画出来的东西，布局留给它的位置不动（`mygo/ui/transform.go`）。
// 所以缩放不会把旁边的字挤走——而改尺寸会。
//
// # 为什么它们走统一曲线，而 `HoverZoom` 不走
//
// 这两条是**点击 / 悬停触发的过渡**（「这一枚控件出现了」），跟统一曲线那一族是
// 同一件事（DESIGN.md §五《一条统一曲线》）。而 `HoverZoom` 是**指针跟随**的
// 连续动画，票 #292 裁决它不跟统一曲线（理由见 `hoverzoom.go`）。
//
// # 两条可以叠在同一个元素上
//
// 框架的变换是**相乘**的（`Scale` 接在已有变换之后，`mygo/ui/transform.go` 的
// `node.Scale`）：所以「弹出来」和「指针停在上面时长大一点」叠在那一枚三角上
// 就是 0.85 → 1 → 1.1，两档各自走各自的动画。
package motion

import (
	"time"

	"github.com/egoist/mygo/ui"
)

const (
	// PopScale 是「弹出来」那一档的**起点**：**85%**。
	//
	// ⚠️ **不是 70%**：QQ音乐 那一侧是 `scale(0.7) → scale(1)`（它的「封面模块」
	// 那一套），但 0.7 起步在**40 像素的圆钮**上看着像「掉了一块」——圆钮本身
	// 就小，缩到七成再长回来，中间那一段像在闪。85% 是「缩了一点但还认得出是
	// 那一枚」的那一档。
	PopScale = 0.85

	// PopTime 是弹出来 / 缩回去的时长。**150 毫秒**。
	//
	// ⚠️ **比淡入那 200ms 略短**（`widgets/hover` 的 `Fade`）：缩放要**先**到位，
	// 那一枚才像「弹」出来；淡入还没走完的时候它已经长到位了，两条通道叠起来
	// 才是「弹」而不是「亮」。
	//
	// ⚠️ **退场方向相反、同样 150ms**：缩回去和弹出来是同一个动作的两头，
	// 时长不一样的话那一枚钮在指针离开时看着是「塌」下去的。
	PopTime = 150 * time.Millisecond
)

// Pop 给 e 装上「弹出来」那一层，返回同一个元素。
//
// on 是**这一帧**那一圈反馈在不在（指针在不在它上面）。key 是这一格的身份：
// **每一处都要给自己的**——两处用同一个 key 的话它们会共用一份动画状态。
//
// ⚠️ **这一层是「缩放」，不是「不透明度」**：不透明度是另一条通道
// （`widgets/hover` 的 `Fade`），两条并行——QQ音乐 那一侧也是两个通道并行
// （`opacity 0→1` + `transform scale(0.7)→scale(1)`）。
func Pop(e ui.Element, key any, on bool) ui.Element {
	return zoomTo(e, key, on, 1, PopScale, PopTime, Unified)
}

const (
	// GrowScale 是「指针停在它上面时长大一点」那一档的倍数：**110%**。
	//
	// 出处：上游 `music-list-item/index.tsx:105` 的 `group-hover:scale-110`
	// （列表行那个播放三角）。
	//
	// ⚠️ **别跟 `HoverZoomScale`（105%）混**：那一处是封面 / 头像那一族的，
	// 这一处是列表行那枚三角的（票 #179 那一族抄的是上游这两个不同的数）。
	GrowScale = 1.1

	// GrowTime 是那一档的时长。**150 毫秒**——和 `PopTime` 同一档：那一枚三角
	// 上两档缩放（弹出来 + 停在上面长大）叠在一起，两个时长一样才不会一半先到
	// 一半后到。
	GrowTime = 150 * time.Millisecond
)

// Grow 给 e 装上「指针停在它上面时长大到 110%」这一层，返回同一个元素。
//
// on 是**这一帧**指针在不在它上面。
//
// ⚠️ **这一层以前是瞬时的**（每帧直接算 `Scale`，不走动画）：那样它看着是
// 「啪」地一下变大的——和它旁边那一枚「淡进来」的三角不是一个节奏。
func Grow(e ui.Element, key any, on bool) ui.Element {
	return zoomTo(e, key, on, GrowScale, 1, GrowTime, Unified)
}

// zoomTo 是那三档缩放共用的那一支：on 时走到 to，不在时回到 from。
//
// ⚠️ **动画的值记在节点上**（`mygo/ui/motion.go` 的 `st.motions`），而状态是
// **按身份**跨帧留下来的——所以调用方必须保证那一格**每一帧都重建**、身份稳定
// （见 `hoverzoom.go` 的文件头那三条）。
//
// # ⚠️ **这一档换不了 `AnimatePose`**（票 #276 查过）
//
// 判据只有一条：**换完之后那个动画值还会不会被 view 读**——会被读就还是每帧重建
// 整棵界面，就白换（`mygo/ui/pose.go`：「the cheapest motion there is」是
// `AnimatePose` 的好处，而它的前提是**view 不读那个值**）。
//
// 而这一档的返回值**就是要被读**的：`e.Scale(v, v)` 把这一帧的倍数写进元素
// 自己的变换里——**view 在建这一帧的时候读它**。换成 pose 的话，pose 的缩放会
// 和元素自己的变换**相乘**（`mygo/ui/transform.go` 的 `transformAt`：
// `poseM.Mul(e.transform.flat())`），于是理论上可以把「这一档的缩放」整个交给
// pose——**但那样这一档的当前值就没有公开的读法了**（pose 的值读不出来），
// 而「弹出来」那一档要和「按下缩放」相乘（`flat.PressScaleValue`），两个数
// 都得是「这一刻的值」——相乘的那一头必须是能读的。
//
// ⇒ **这一档留在 `AnimateTo` 那一档**：换过去省不下重建（view 照样每帧读那个
// 乘出来的倍数），却把一个能读的数变成读不出来的。
func zoomTo(e ui.Element, key any, on bool, to, from float32, d time.Duration, ease ui.Easing) ui.Element {
	target := from
	if on {
		target = to
	}
	v := e.AnimateWith(key, target, d, ease)
	return e.Scale(v, v)
}
