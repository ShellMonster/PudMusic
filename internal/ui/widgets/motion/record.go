package motion

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// 「深夜唱片行」的动效令牌（DESIGN.md §0′，原型 `index.html` 64–66 行和各处
// `@keyframes` / `animation`）。
//
// # 和 `Unified` 的关系
//
// 原型和 DESIGN.md §五 冲突时以原型为准：**新写的过渡一律用 `EaseOut` +
// `Fast` / `Med` / `Slow`**。`Unified` 留给还没改版的那几处（主 Tab、缩放），
// 各页的票改到时换掉。
//
// # 减少动态效果
//
// 下面这些 `ui.ElementTransition` 交给框架的 `Element.Transition`：系统开着
// 「减少动态效果」（`Preferences.ReduceMotion`）时框架**不位移、不缩放**，只在
// 原地淡入淡出——所以调用方不用自己判。自己用 `Animate` / `AnimateTo` 画的
// 动画框架也会直接跳到终点；只有自绘的循环动画（eq-bars、骨架屏的光）要自己问
// `Reduced(c)`。
var (
	// EaseOut 是原型的 `--ease-out: cubic-bezier(.2, .8, .2, 1)`：进场、悬停、展开。
	EaseOut = ui.CubicBezier(0.2, 0.8, 0.2, 1)
	// EaseIn 是原型的 `--ease-in: cubic-bezier(.4, 0, 1, 1)`：出场（抽屉收回、Toast 消失）。
	EaseIn = ui.CubicBezier(0.4, 0, 1, 1)
	// SwitchSpring 是开关旋钮那一下（原型 `cubic-bezier(.3, 1.4, .5, 1)`，带一点回弹）。
	SwitchSpring = ui.CubicBezier(0.3, 1.4, 0.5, 1)
)

// 三档时长（原型 `--t-fast` / `--t-med` / `--t-slow`）。
const (
	// Fast 是颜色、底色、描边的过渡，按钮按下的缩放（160ms）。
	Fast = 160 * time.Millisecond
	// Med 是弹层、Toast、对话框进场，开关，Tab 下划线（240ms）。
	Med = 240 * time.Millisecond
	// Slow 是抽屉、页面切换、全屏播放器进场（360ms）。
	Slow = 360 * time.Millisecond
	// ExitFast 是 Toast 出场（原型 `toast-out 180ms`）。
	ExitFast = 180 * time.Millisecond
	// ExitDrawer 是抽屉 / 遮罩出场（原型 `drawer-out 220ms`）。
	ExitDrawer = 220 * time.Millisecond
)

// Fade 是颜色那一档的过渡（底色、描边、字色）：`Fast` + `EaseOut`。
//
//	row.Transition(motion.Fade)
var Fade = ui.ElementTransition{Duration: Fast, Ease: EaseOut, Colors: true}

// PopIn 是弹层、对话框、搜索联想、批量操作条、回到顶部的进场
// （原型 `@keyframes pop-in`：从上 4、缩 0.98、透明 → 原位）。出场是同一程反着走。
//
//	panel.Transition(motion.PopIn)
//
// ⚠️ 元素要有稳定 Key（框架靠 ID 认出「这是刚出现的那一个」）。
var PopIn = ui.ElementTransition{
	Duration: Med, Ease: EaseOut,
	Enter: &ui.Motion{Y: -4, Scale: 0.98, Opacity: 0},
	Exit:  &ui.Motion{Y: -4, Scale: 0.98, Opacity: 0},
}

// ToastIn 是 Toast 的进出场（原型 `toast-in`：从下 8 淡入；`toast-out`：往下 4 淡出）。
//
// ⚠️ 框架的一个 Transition 只有一个时长，进场 240 / 出场 180 取进场那一档。
var ToastIn = ui.ElementTransition{
	Duration: Med, Ease: EaseOut,
	Enter: &ui.Motion{Y: 8, Opacity: 0},
	Exit:  &ui.Motion{Y: 4, Opacity: 0},
}

// ScrimIn 是抽屉、对话框后面那层遮罩的淡入淡出（原型 `fade-in` / `fade-out`）。
var ScrimIn = ui.ElementTransition{
	Duration: Med, Ease: EaseOut,
	Enter: &ui.Motion{Opacity: 0},
	Exit:  &ui.Motion{Opacity: 0},
}

// PageIn 是页面切换（原型 `page-in`：从下 8 淡入，360ms）。
var PageIn = ui.ElementTransition{
	Duration: Slow, Ease: EaseOut,
	Enter: &ui.Motion{Y: 8, Opacity: 0},
}

// FullIn 是全屏播放器的进出场（原型 `full-in` / `full-out`：从下 24 淡入）。
var FullIn = ui.ElementTransition{
	Duration: Slow, Ease: EaseOut,
	Enter: &ui.Motion{Y: 24, Opacity: 0},
	Exit:  &ui.Motion{Y: 24, Opacity: 0},
}

// DrawerIn 是右侧抽屉的进出场（原型 `drawer-in`：从右边整块滑进来）。width 是
// 抽屉宽（一般是 `tok.DrawerW`）。
//
//	panel.Key("queue-drawer").Transition(motion.DrawerIn(tok.DrawerW))
//
// ⚠️ 进场 360 / 出场 220 合成一个时长（框架一个 Transition 只有一个），取 Slow。
func DrawerIn(width float32) ui.ElementTransition {
	return ui.ElementTransition{
		Duration: Slow, Ease: EaseOut,
		Enter: &ui.Motion{X: width, Opacity: 1},
		Exit:  &ui.Motion{X: width, Opacity: 1},
	}
}

// Reduced 报系统是不是开着「减少动态效果」。只有自绘的循环动画（eq-bars、骨架屏
// 的光带）需要自己问；`Transition` / `Animate` 框架已经处理了。
func Reduced(c *ui.Context) bool { return c.Preferences().ReduceMotion }
