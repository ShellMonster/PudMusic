// 满屏进度条用的滑块：框架的**基座** + 自己画的轨道和旋钮。
//
// ## 为什么不用 `ui.Slider`
//
// 框架那一颗旋钮在**被按住**的时候会变成一片透镜：白面整个褪掉，只留一圈黑边
// （`mygo/ui/widgets.go` 的 `paintKnob`：`if held < 1 { … p.Fill(k, face…) }`
// ——按住之后那一支不走了；深色主题下那圈边还是黑的，`RGBA(0,0,0,0.7)`）。
//
// 深色底上那颗黑边透镜**几乎看不见**。实测满屏里（底永远是深的：封面压着 60%
// 的黑）：松手时旋钮是一块 14×16 的白胶囊，按住之后只剩一圈 12×6 的暗环——
// 用户按住旋钮拖，旋钮反而不见了。那就是「拖进度条时旋钮不响应」。
//
// 这一层用 `ui.SliderBase`（框架给自绘留的那个口：值、拖动、点击跳转、键盘、
// 无障碍全是框架的，只有画出来的那部分归我们），画一颗**按住也看得见**的旋钮。
// 和 `static_switch.go` 那个「设置页上的开关」是同一个路数。
//
// ⚠️ 播放条上那一条**还是** `ui.Slider`（见 playbar.go 的 viewProgress），所以在
// 深色主题下它有同一个毛病。这一票只管满屏（票面写的是「满屏里那几个控件」），
// 而播放条那一条本来就归播放条重做那一票（issue #135：「进度条旋钮只在悬停或
// 按下时画……用框架现成的滑块基座」）——两边动同一处会打架。
//
// ## 画法照框架那一颗抄
//
// 位置 `(value-lo)/(hi-lo)`；旋钮宽的一半留在轨道里（所以有那个 `PaddingX`）；
// 轨道粗 `Space(1.5)`、填过的那一段是主色；旋钮是一颗带两道影子的白胶囊
// （深色下是 `RGB(224,225,225)`，和 `coreSlider`/`coreSwitch` 一样）。
//
// ## 按住的时候：**交叉淡入**到一片玻璃
//
// 玻璃垫在底下（`held>0` 才画），不透明的面浮在上面（`held<1` 才画，不透明度是
// `1-held`），两样一起淡。
//
// ⚠️ **少了这个交叉，按住的那一帧旋钮会「闪一下消失」**：不透明的面没了，而玻璃
// 在满屏这种永远深着的底上是暗的——那正是「拖进度条时旋钮不响应」的症状。
//
// 玻璃那一层**掺了旋钮自己的颜色**（`glass.Glass` 的 `Tint` 那一档管的就是「看不
// 看得清」）：纯玻璃在深色底上是看不见的。
//
// ⚠️ **不处理「从右到左」**：那是框架的 `Direction`，本项目不摆那种布局
// （理由同 `static_switch.go`），所以这一支照 `coreSlider` 的默认分支抄。
package ui

import (
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

// knobSlider 造一根旋钮按住也看得见的滑块。
//
// 出来的那个元素照旧可以 `.Grow(1)` / `.Label(…)` / `.Disabled(…)`。
func knobSlider(c *ui.Context, value *float64, lo, hi float64) ui.Element {
	t := c.Theme()
	// 旋钮和轨道那些尺寸照框架的 `coreSlider` 抄（`mygo/ui/widgets.go`）。
	kw, kh := t.Space(5), t.Space(4)
	th := t.Space(1.5)

	sl := ui.SliderBase(c, value, lo, hi).
		Height(t.Space(5)).MinWidth(t.Space(20)).PaddingX(kw / 2).
		// 焦点圈画在**旋钮**上，不是围着整条滑块（框架那一颗也是这么画的，
		// `coreSlider` 的 `.FocusRing(false)` 加 `paintKnob` 里的 `p.FocusRing`）。
		// 围着整条画的话，满屏那条一千多像素宽的滑块会套上一只长方形的圈。
		FocusRing(false)

	// ⚠️ 这两行**在构造之后**算，不在构造里面算：框架的 `coreSlider` 在构造
	// 里面就把位置算好了，而构造的尾巴（`endBuild`）才把这一帧的拖动写进值里
	// ——于是框架那一颗的旋钮总比手慢一帧。我们这里晚一步算，旋钮是**当下**那个。
	frac := float32(0)
	if hi > lo {
		frac = float32((*value - lo) / (hi - lo))
	}
	frac = min(max(frac, 0), 1)
	// 「按住」的量是 0~1 的**动画**值（框架的滑块也是这么做的，`coreSlider` 的
	// `s.Animate("held", …, 150*time.Millisecond)`）——下面那处交叉淡入要的是
	// 它，不是一个开关。
	heldTarget := float32(0)
	if sl.Pressed() {
		heldTarget = 1
	}
	held := sl.Animate("held", heldTarget, 150*time.Millisecond)
	focus := sl.FocusVisible()

	sl.Draw(func(p *ui.Painter, r ui.Rect) {
		cy := r.Y + t.Space(2.5)
		track := ui.Rect{X: r.X, Y: cy - th/2, W: r.W, H: th}
		// 旋钮的中心：frac=0 时压在轨道最左边，frac=1 时在最右边，半个旋钮留在
		// 轨道里（那个 `PaddingX` 就是为它留的）。
		x := r.X + kw/2 + (r.W-kw)*frac

		p.Fill(track, t.Border, th/2)
		p.Fill(ui.Rect{X: track.X, Y: track.Y, W: x - track.X, H: th}, t.Accent, th/2)

		// 按住的时候旋钮长大一点（和框架那一颗一样）。
		g := 1 + 0.35*held
		knob := ui.Rect{X: x - kw*g/2, Y: cy - kh*g/2, W: kw * g, H: kh * g}
		rad := knob.H / 2
		face := ui.RGB(255, 255, 255)
		if t.Dark {
			face = ui.RGB(224, 225, 225)
		}
		// 两道影子：紧的那道描边，软的那道把旋钮托起来（照 `coreSlider` 的
		// `paintKnob`）。按住的时候影子跟着淡（那时候旋钮是一片玻璃）。
		shade := 1 - held
		p.Shadow(knob, rad, 0, 0.5, 1, 0, ui.RGBA(0, 0, 0, 0.08*shade))
		p.Shadow(knob, rad, 0, 1.5, 7, 0, ui.RGBA(0, 0, 0, 0.1*shade))

		// 旋钮按住的时候，从「不透明的面」**交叉淡入**到「一片玻璃」。
		//
		// ⚠️ **交叉是关键**：只画玻璃的话，按住的那一帧不透明的面没了，而玻璃在
		// 满屏这种永远深着的底上是暗的——旋钮「闪一下消失」，那正是「拖进度条时
		// 旋钮不响应」的症状。所以玻璃垫在底下（held>0 才画），不透明的面浮在
		// 上面（held<1 才画，不透明度是 1-held），两样一起淡。
		//
		// 玻璃那一层**掺了旋钮自己的颜色**（`glass.Glass` 的 `Tint` 那一档管的
		// 就是「看不看得清」）：纯玻璃在深色底上是看不见的。
		if held > 0 {
			glass.Paint(p, knob, rad, glass.Glass{Tint: face})
		}
		if held < 1 {
			p.Fill(knob, face.Alpha(1-held), rad)
		}
		if focus {
			p.FocusRing(knob, [4]float32{rad, rad, rad, rad})
		}
	})
	return sl
}
