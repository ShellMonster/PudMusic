// 设置页 / 偏好面板上的开关：旋钮拨过去会**弹一下**的那一个。
//
// # 为什么不用 `ui.Switch`
//
// 框架那一颗的旋钮是 **140ms 的匀出**（`mygo/ui/widgets.go` 的 `coreSwitch`：
// `sw.Animate("knob", b2f(*on), 140*time.Millisecond)`）——**匀出不是弹**：它从
// 最快开始、慢慢停住，没有「冲过头再回来」的那一下。用户要的是后者
// （2026-10-26：「开关要有那种弹的效果的，就是用户明显看到是打开」）。
//
// 外观照 `coreSwitch` 逐字抄（`Size(Space(9), Space(5))`、`Radius(Space(2.5))`、
// 关态底色 `Border.Mix(Text, 0.15)` 往主色混、旋钮是带一圈影子的白圆），所以
// 静态看起来和 `ui.Switch` 一模一样，只是不再「啪」一下就跳过去。
//
// # 为什么走 `AnimatePose`（而不是 `AnimateTo`）
//
// 判据只有一条：**动画的值会不会被 view 读**（`like.go` / `fullplayer.go`
// 那一段写的就是这一条）。
//
//   - `AnimateTo` 的返回值是**被 view 读**的（旋钮画在哪要读它），所以那几百毫秒
//     里**每一帧都要重建整棵界面**——设置页是全应用最大的一棵树（几十行控件加
//     几十行文字），重建它几十次只为了挪一颗 16 像素的旋钮。
//   - `AnimatePose` 的值**元素自己走**，view 只给**目标**——中间那些帧走的是
//     「只重绘」（框架自己管这档叫 "the cheapest motion there is"）。
//
// 所以旋钮**自己是一个元素**：开关的一个绝对定位的孩子，位置由它的 pose 带着走。
//
// # ⚠️ 轨道底色是**瞬变**的
//
// 轨道底色是节点自己的属性（`Background`），**建的时候写死**——pose 那一族只带得动
// 位置、缩放、旋转和不透明度，带不动颜色。要让底色也一起动画，就得让 view 每帧
// 读一个动画出来的颜色（`AnimateColor`），于是又回到「每帧重建整棵界面」。
//
// 这一处选了瞬变：**看得见的是旋钮飞过去那一下**（它 250ms 走完 90%），而底色在
// 那一帧就已经是主色了——用户看到的是「亮了、旋钮弹过去了」，两件事都在。
//
// # ⚠️ **别给这个开关加 Key**
//
// 开关是 `ui.SwitchBase` 建的（`mygo/ui/base.go`），而框架**明写**「处理输入的
// 控件不能加 Key」（`mygo/ui/element.go` 的 `Key`：Checkbox / Radio / Switch /
// Slider / Select / Link / List / TextInput / TextArea 加了直接 panic）。
// 就算绕过去（把 Key 加在外面的 Row 上），动画状态记在控件节点上，每帧换一个新
// 节点 = 动画每帧从头开始（见 `settings_page.go` 的 `pickAutoStart`）。
package ui

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// switchAnim 是旋钮那一弹的动画：**弹簧**那一族（框架自带 `ui.Snappy` 的那一档）。
//
// ⚠️ **它是 `ui.Snappy`，只是把精度收到 0.05 DIP**：
//
//   - 框架的 `Snappy` = 500ms 的弹簧、`Bounce: 0.15`（实算：阻尼比 ζ=0.85，
//     过冲 **0.10 DIP** = 全程 16 DIP 的 0.63%，肉眼看不见——**看得见的是旋钮
//     真的飞过去**：250ms 走完 90%、333ms 走完 98%）。
//   - 精度默认是 **0.001 DIP**，那会让弹簧一直跑到 **884ms** 才收手，而画面在
//     350ms 之后就已经不动了——多出来的那些帧是白占的（同一件事见 DESIGN.md §五
//     《歌词滚到中间为什么是弹簧》那一节：它也把精度收到 0.05）。
//
// ⚠️ **过冲只有 0.10 DIP，别指望「冲过头」那一下看得见**：这一颗旋钮停下来时离
// 轨道边只有 2 DIP，过冲超过 2 DIP 旋钮就**戳出轨道**了。要更明显的那一下，
// 得先改轨道和旋钮之间的留白——那是另一件事。
var switchAnim = ui.Animation{Spring: ui.SpringOptions{
	Duration:  500 * time.Millisecond,
	Bounce:    0.15,
	Precision: 0.05,
}}

// springSwitch 是一个开关：旋钮拨过去的时候**弹一下**。
//
// 值和 `ui.Switch` 一样：一帧一读的局部 `*bool`，用户点了就写进去，`Changed()`
// 报这一次点击。`Disabled` 照旧由调用方给。
//
// ⚠️ **旋钮是开关的一个孩子（绝对定位）**，不是画在开关自己的 `Draw` 里的——
// 理由见文件头《为什么走 `AnimatePose`》：pose 动的是**元素**，旋钮要自己
// 是一个元素才动得了。
func springSwitch(c *ui.Context, on *bool) ui.Element {
	t := c.Theme()
	sw := ui.SwitchBase(c, on).
		Size(t.Space(9), t.Space(5)).Radius(t.Space(2.5))

	// 关态的底色往主色混：开 = 全主色，关 = 全灰。和 coreSwitch 同一条式子。
	off := t.Border.Mix(t.Text, 0.15)
	pos := float32(0)
	if *on {
		pos = 1
	}
	sw.Background(off.Mix(t.Accent, pos))
	// 钉死：别让排版在这一帧里改出别的宽高（`ui.Switch` 也是这么钉的）。
	sw.Shrink(0)

	// 旋钮：轨道左端那一颗白圆，拨过去的时候弹到右端。
	sw.Children(func() {
		in := t.Space(0.5)
		d := t.Space(5) - 2*in
		knob := ui.Box(c).Absolute().Left(in).Top(in).Size(d, d)
		knob.Draw(func(p *ui.Painter, r ui.Rect) {
			p.Shadow(r, d/2, 0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.25))
			p.Fill(r, ui.RGBA(255, 255, 255, 1), d/2)
		})
		// 走的距离是「轨道宽 − 轨道高」（旋钮从一头挪到另一头）。
		knob = knob.AnimatePose(ui.Pose{X: (t.Space(9) - t.Space(5)) * pos}, switchAnim)
	})
	return sw
}
