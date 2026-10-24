// 自绘的滑块：框架的**基座** + 自己画的轨道和旋钮。
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
// ## 三处用它
//
//   - **满屏播放器**那条进度（`fullplayer.go`）：横着，**旋钮只在悬停或按下时
//     画**，轨道 6 像素——照上游满屏那一行传下去的 `trackClassName="h-[6px]"`
//     （`full-screen-player/index.tsx:395`）和 `hideThumb`。
//   - **播放条**那条进度（`playbar.go`）：横着，**旋钮只在悬停或按下时画**，
//     轨道压到 4 像素——照上游 `music-play-progress` 的 `h-[4px]` 和 `hideThumb`。
//   - **音量浮层**里那根竖滑块（`playbar_volume.go`）：竖着——框架的 `ui.Slider`
//     摆不了竖的（`Vertical()` 只认 `SliderBase`）。
//
// ## 画法照框架那一颗抄
//
// 位置 `(value-lo)/(hi-lo)`；旋钮宽的一半留在轨道里（所以有那个 `PaddingX`）；
// 轨道粗、填过的那一段**平时是前景色、悬停/按下才变主色**（上游
// `music-play-progress/index.tsx:38` 的 `color={showThumb ? "primary" : "foreground"}`）。
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

// sliderLook 是一根自绘滑块的样子。
//
// 旋钮的**颜色**不在这里：它跟着主题和这一根滑块的来处走（见 face）。
type sliderLook struct {
	// vertical：竖着（0 在下面、1 在上面）。
	//
	// ⚠️ 框架的 `Vertical()` 只认 `SliderBase`，而它的值映射用的是**内容框**
	// （`mygo/ui/value_input.go:207`：`1-(y-st.cy)/st.ch`）——所以竖着的时候
	// 内容框的高就是全程，旋钮的中心得跟着内容框走，不能自己再留半个旋钮的余量。
	vertical bool

	// trackW 是轨道粗。
	trackW float32
	// hotTrackW 非 0 时，悬停 / 按下 / 有焦点的轨道粗从 trackW 长到它（设计规范 v0.1：4 → 6）。
	hotTrackW float32

	// knobW / knobH 是旋钮的宽和高（横着的时候宽是直径，高是胶囊的高）。
	knobW, knobH float32

	// knobWhenIdle：旋钮是不是一直画着。
	//
	// false = **只在悬停或按下时画**（上游 `hideThumb={!showThumb}` 那个做法）：
	// 一颗一直亮着的圆点在整条进度上全程抢注意力，而它是用户真正要抓的东西。
	knobWhenIdle bool

	// face 是旋钮不透明时那一面的颜色。
	//
	// ⚠️ **播放条那根必须是主色，不能是白色**：播放条的底色在浅色主题下就是白的
	// （`t.Background` = `#ffffff`），一颗白旋钮落在白底上等于没有——而
	// 「旋钮看得见」正是这一层存在的理由。上游 `music-play-progress` 的
	// `thumb: "bg-primary"` 正是这个。
	face func(t *ui.Theme) ui.Color

	// fill 是「填过的那一段」的**两档**颜色：不悬停的那一档、悬停/按下的那一档。
	//
	// 上游 `music-play-progress/index.tsx:38`：
	// `color={showThumb ? "primary" : "foreground"}`——**不悬停是前景色，悬停才
	// 主色**（深色下那条绿杠于是变成一条白杠；上游那张截图实测 #ecedee）。
	//
	// ⚠️ **两档一样 = 一直是那个颜色**：满屏那根和音量那根是上游另外两个组件，
	// 填过的那一段一直是一个颜色（理由见各自那处的注释）。
	fill func(t *ui.Theme) (idle, hot ui.Color)
}

// knobFullLook 是满屏播放器那根的样子：轨道 6 像素、旋钮只在悬停或按下时画，
// 旋钮是白的。
//
// 轨道 6 照上游满屏那一行传下去的 `trackClassName="h-[6px]"`；「只在悬停或按下
// 时画」照 `music-play-progress` 的 `hideThumb={!showThumb}`——一颗一直亮着的
// 圆点在整条进度上全程抢注意力，而它是用户真正要抓的东西。
//
// 白是那边的对：满屏的底是糊过的封面加一层 55% 的黑（`fullplayer.go` 的
// `fullScrim`），白旋钮压在上面最清楚。
//
// 尺寸照框架的 `coreSlider` 抄（`mygo/ui/widgets.go`）：旋钮 `t.Space(5)` ×
// `t.Space(4)`、元素高 `t.Space(5)`、最小宽 `t.Space(20)`。
var knobFullLook = sliderLook{
	trackW:       6,
	knobW:        20,
	knobH:        16,
	knobWhenIdle: false,
	face: func(t *ui.Theme) ui.Color {
		if t.Dark {
			return ui.RGB(224, 225, 225)
		}
		return ui.RGB(255, 255, 255)
	},
	// ⚠️ **这一根一直是主色，不拆两档**：满屏的底永远是糊过的封面加一层 55% 的
	// 黑（`fullplayer.go` 的 `fullScrim`），而这一根的旋钮是**白的**——前景色
	// （浅色下近黑）压在那上面根本看不见，浅色主题下更是一条黑杠。
	//
	// 上游满屏那一条进度是 `full-screen-player/index.tsx:291-400` 里的另一套
	// markup，而 BMG-124 那份对比报告**只量了播放条那一条**（满屏那一条没有
	// 像素取证）——所以这一根按「一直是主色」留着，票 #218 只动播放条。
	fill: func(t *ui.Theme) (ui.Color, ui.Color) {
		return t.Accent, t.Accent
	},
}

// knobHoverLook 是播放条那根的样子：轨道 4 像素，旋钮只在悬停或按下时画。
//
// 4 像素照上游 `music-play-progress` 的 `track: "h-[4px]"`；旋钮 16 见方照它的
// `thumb: "w-4 h-4"`。
var knobHoverLook = sliderLook{
	trackW:       4,
	hotTrackW:    6,
	knobW:        16,
	knobH:        16,
	knobWhenIdle: false,
	face: func(t *ui.Theme) ui.Color {
		return t.Accent
	},
	// 🔴 **填过的那一段平时是前景色，悬停/按下才变主色**（票 #218）。
	//
	// 上游 `music-play-progress/index.tsx:38`：
	// `color={showThumb ? "primary" : "foreground"}`——**不悬停是前景色**（深色下
	// #ecedee、浅色下近黑），**悬停才主色绿**。我们原来**永远**是主色，于是
	// 一进来就看见一条绿杠，上游那边是白杠。
	//
	// 「悬停」和「出旋钮」是**同一档**：上游那个 `showThumb` 同时管着颜色和
	// `hideThumb`，而我们这边「旋钮只在悬停或按下时画」也是同一个条件
	// （见 `knobWanted`）。所以这一档和旋钮出不出来是一致的，不会出现
	// 「旋钮是主色而那一段是前景色」那种自相矛盾的画面。
	fill: func(t *ui.Theme) (ui.Color, ui.Color) {
		return t.Text, t.Accent
	},
}

// knobMiniVolumeLook 是**迷你播放器里那根音量滑块**的样子（票 #241 / 报告 F-21）。
//
// ⚠️ **它和框架那一颗唯一的不同是填充色**：框架的 `ui.Slider` 把填充色**写死**
// 成 `t.Accent`（`mygo/ui/widgets.go` 的 `coreSlider`：`p.Fill(…, t.Accent, …)`），
// 元素上没有换色的方法——所以迷你窗里那根音量滑块一直是**主色绿的实心填充**，
// 而那是 320×100 那扇窗里**最大的一块颜色**（占 0.97%），比同窗那条播放位置
// （主要）还响。
//
// 现在：**填过的那一段平时是次要色（中性灰），悬停 / 按下才变主色**——和播放条
// 那根进度同一个思路（票 #218）。于是那一扇窗里的主色留给「正在放的位置」。
//
// ⚠️ **轨道和旋钮照框架那一颗抄**：6 像素的轨道、20×16 的旋钮、常驻（旋钮一直
// 画着），而旋钮那面白 / 深灰 #e0e1e1（框架的 `paintKnob` 就是这一档）——
// **迷你窗里那两根滑块看起来是同一套**（进度那根还是框架那一颗）。
var knobMiniVolumeLook = sliderLook{
	trackW:       6,
	knobW:        20,
	knobH:        16,
	knobWhenIdle: true,
	face: func(t *ui.Theme) ui.Color {
		if t.Dark {
			return ui.RGB(224, 225, 225)
		}
		return ui.RGB(255, 255, 255)
	},
	// 平时次要色，悬停 / 按下才主色（理由见上面那段）。
	fill: func(t *ui.Theme) (ui.Color, ui.Color) {
		return t.TextMuted, t.Accent
	},
}

// knobSlider 造一根旋钮按住也看得见的滑块（满屏播放器那条进度）。
//
// 出来的那个元素照旧可以 `.Grow(1)` / `.Label(…)` / `.Disabled(…)`。
func knobSlider(c *ui.Context, value *float64, lo, hi float64) ui.Element {
	t := c.Theme()
	return selfSlider(c, value, lo, hi, knobFullLook).
		Height(t.Space(5)).MinWidth(t.Space(20))
}

// playbarProgressSlider 是播放条上那根进度：轨道 4 像素，旋钮只在悬停或按下时画。
//
// ⚠️ **它不是 `ui.Slider`**：框架那一颗的旋钮全程亮着，而且按住的时候会褪成一圈
// 黑边（深色底上等于消失）——理由见文件头。
func playbarProgressSlider(c *ui.Context, value *float64, lo, hi float64) ui.Element {
	return selfSlider(c, value, lo, hi, knobHoverLook).
		Height(20).MinWidth(16)
}

// selfSlider 造一根自绘的滑块：框架的基座（值、拖动、点击跳转、键盘、无障碍
// 全是框架的），画出来的那部分归我们。
//
// 旋钮的**位置和粗细**由 look 定；元素**多大**由调用方摆（`.Width` / `.Height` /
// `.Grow`）——竖着的那根（音量）全程是调用方说了算。
func selfSlider(c *ui.Context, value *float64, lo, hi float64, look sliderLook) ui.Element {
	t := c.Theme()

	sl := ui.SliderBase(c, value, lo, hi).
		// 焦点圈画在**旋钮**上，不是围着整条滑块（框架那一颗也是这么画的，
		// `coreSlider` 的 `.FocusRing(false)` 加 `paintKnob` 里的 `p.FocusRing`）。
		// 围着整条画的话，那条一千多像素宽的滑块会套上一只长方形的圈。
		FocusRing(false)

	// 旋钮的一半留在轨道里（框架的规矩：`coreSlider` 的 `PaddingX(kw / 2)`）。
	// 竖着的时候留的是**上下的**那一份（框架的值映射用的是内容框，见 sliderLook）。
	if look.vertical {
		sl = sl.Vertical().PaddingY(look.knobH / 2)
	} else {
		sl = sl.PaddingX(look.knobW / 2)
	}

	// ⚠️ 这几行**在构造之后**算，不在构造里面算：框架的 `coreSlider` 在构造
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

	// 🔴 **「热」的量**：填过的那一段在**悬停 / 按下 / 有键盘焦点**的时候是主色，
	// 平时是前景色（上游 `music-play-progress/index.tsx:38` 的
	// `color={showThumb ? "primary" : "foreground"}`，票 #218）。
	//
	// ⚠️ **它和「旋钮出不出来」是同一档**：上游那个 `showThumb` 同时管着颜色和
	// `hideThumb`，我们这边「旋钮只在悬停或按下时画」（`knobWanted`）也是同一个
	// 条件。键盘焦点算进来是因为**焦点上旋钮是要画的**（`focus` 那一条）——
	// 旋钮在而那一段还是前景色，画面上是自相矛盾的。
	hotTarget := float32(0)
	if sl.Hovered() || held > 0 || focus {
		hotTarget = 1
	}
	heat := sl.Animate(fillHeatKey, hotTarget, 150*time.Millisecond)

	face := look.face(t)
	idleFill, hotFill := look.fill(t)

	sl.Draw(func(p *ui.Painter, r ui.Rect) {
		var track ui.Rect
		var cx, cy float32 // 旋钮的中心

		// 🔴 **填过的那一段的两档颜色**（票 #218）：平时前景色、悬停/按下才主色。
		//
		// ⚠️ **禁用的滑块不算「热」**：`.Disabled(…)` 是调用方在 `selfSlider` 返回
		// **之后**才设上的（`playbar.go` 的 `viewProgress`），所以**建**的时候
		// 问不出「禁用」，而**画**的时候问得出（框架的 `Hovered()` 里有
		// `if e.IsDisabled() { return false }`）。禁用的滑块没有旋钮（`knobWanted`
		// 那一处），那一段也就不该是主色。
		hot := heat
		if sl.IsDisabled() {
			hot = 0
		}
		fill := idleFill.Mix(hotFill, hot)
		trackW := look.trackW
		if look.hotTrackW > 0 {
			trackW += (look.hotTrackW - look.trackW) * hot
		}

		if look.vertical {
			// 竖着：值从下往上（框架的映射：`1-(y-cy)/ch`，见 value_input.go）。
			cx = r.X + r.W/2
			// 🔴 **旋钮的中心要走内容框的算法**，和横着那一支同一个式子：
			// **半个旋钮留在轨道里**（那个 `PaddingY` 就是为它留的）。
			//
			// ⚠️ **为什么不能直接用 `r`**：框架的竖直映射是
			// `1-(y-st.cy)/st.ch`，那个 `st.cy`/`st.ch` 是**内容框**的
			// （`mygo/ui/runtime.go:1023`），而 `Draw` 回调拿到的 `r` 是**边框框**
			// （含内边距）。两者差着 `knobH/2`：值 0 时旋钮的中心该在内容框的
			// **底**（`r.Y + r.H - knobH/2`），照 `r` 算会落在边框框的底上——
			// **探出内容框 7 像素**，而框架的命中映射也在那儿取值，于是
			// 「旋钮画的地方」和「按下去算出来的值」对不上（评审指出来的）。
			cy = r.Y + look.knobH/2 + (r.H-look.knobH)*(1-frac)
			track = ui.Rect{X: cx - look.trackW/2, Y: r.Y, W: look.trackW, H: r.H}
			p.Fill(track, t.Border, look.trackW/2)
			// 🔴 **填过的那一段是从旋钮往下到轨道底**：值 0 时旋钮压在底上，
			// 填过的那一段是**空的**；值 1 时旋钮在顶上，那一段是**满的**。
			//
			// ⚠️ 原来这里是**从顶上往下填到旋钮**——填成了**剩下的**那一段，
			// 于是 0% 显示满格、100% 显示空格（评审指出来的）。
			p.Fill(ui.Rect{X: track.X, Y: cy, W: look.trackW, H: track.Y + track.H - cy},
				fill, look.trackW/2)
		} else {
			cy = r.Y + r.H/2
			track = ui.Rect{X: r.X, Y: cy - trackW/2, W: r.W, H: trackW}
			// 旋钮的中心：frac=0 时压在轨道最左边，frac=1 时在最右边，半个旋钮留在
			// 轨道里（那个 `PaddingX` 就是为它留的）。
			cx = r.X + look.knobW/2 + (r.W-look.knobW)*frac
			p.Fill(track, t.Border, trackW/2)
			p.Fill(ui.Rect{X: track.X, Y: track.Y, W: cx - track.X, H: trackW},
				fill, trackW/2)
		}

		// 🔴 **键盘焦点也要画旋钮**（评审指出来的）：框架那一颗旋钮是**一直**画着的，
		// 焦点圈画在它上面。而这一票把播放条那根改成「只在悬停或按下时画」——
		// 于是 **Tab 过去的时候旋钮和焦点圈一起没了**，焦点在哪完全看不出来。
		// 框架的 `coreSlider` 明确画焦点圈（`mygo/ui/widgets.go:455`），
		// 焦点圈跟旋钮是一起的，所以旋钮也得画。
		if !look.knobWhenIdle && !knobWanted(sl, held) && !focus {
			return
		}

		// 按住的时候旋钮长大一点（和框架那一颗一样）。
		g := 1 + 0.35*held
		knob := ui.Rect{X: cx - look.knobW*g/2, Y: cy - look.knobH*g/2,
			W: look.knobW * g, H: look.knobH * g}
		paintKnob(p, t, knob, knob.H/2, face, held, focus)
	})
	return sl
}

// fillHeatKey 是「填过的那一段有多热」那个动画的键。
//
// ⚠️ **它得是另一个键**：框架的动画按**键**分道（`mygo/ui/motion.go` 的
// `channel(key, …)`——同一个键就是同一条动画），而这一根上已经有 "held" 那条
// 了（按住时旋钮褪成玻璃）。
const fillHeatKey = "fill-heat"

// knobWanted 这一刻那颗旋钮要不要画（只在悬停或按下时画的那一种）。
//
// ⚠️ **「悬停」和「禁用」都得在画的时候问**：
//
//   - **禁用**是建完之后才设上的（调用方的 `.Disabled(!known)` 写在
//     `selfSlider` 返回**之后**），建的时候问出来恒是「没禁用」。
//   - **悬停**在画的时候问和建的时候问是一个答案（同一帧里指针没动），
//     但画的时候问能拿到「禁用的滑块没有悬停」那一条（框架的 `Hovered()` 里有
//     `if e.IsDisabled() { return false }`）。
//
// 没歌的时候进度条是禁用的——那一条路上不画旋钮是对的：它压根不能拖，画一个
// 旋钮是在骗人。
//
// ⚠️ **「悬停」和「按住」是并联的**：按下去的时候指针本来就在它上面，但按住
// 之后 `Hovered()` 仍然为真——所以这一支取的是「或」，不是「且」。
func knobWanted(sl ui.Element, held float32) bool {
	if sl.IsDisabled() {
		return false
	}
	return sl.Hovered() || held > 0
}

// paintKnob 画一颗旋钮：松手时不透明的面，按住时一片玻璃，中间按 held 交叉淡入。
//
// ⚠️ **交叉是关键**：只画玻璃的话，按住的那一帧不透明的面没了，而玻璃在深色底上
// 是暗的——旋钮「闪一下消失」，那正是「拖进度条时旋钮不响应」的症状。所以玻璃垫在
// 底下（`held>0` 才画），不透明的面浮在上面（`held<1` 才画，不透明度是 `1-held`），
// 两样一起淡。
//
// 那几层影子也跟着 `held` 走（松手时一层很淡的、按下时一层更深的），旋钮才有
// 「被按下去」的实感。
func paintKnob(p *ui.Painter, t *ui.Theme, knob ui.Rect, rad float32, face ui.Color, held float32, focus bool) {
	// ⚠️ **三层影子**（照框架 `coreSlider` 的 `paintKnob`，
	// `mygo/ui/widgets.go:435`）：
	//
	//	held > 0  一层**更深的**（`drop.Alpha(held)`）——票面原话：「松手时一层很淡的、
	//	         按下时一层更深的」。**少了它，按下去旋钮不但没有更深的影子，
	//	         反而一层都不剩**，观感是「变平了」而不是「被按下去」。
	//	held < 1  紧的那道描边 + 软的那道托起来（这两层随 held 淡掉）
	//
	// ⚠️ 原来这里两层都乘 `1-held`，按住到 held=1 时浓度归 0 —— 影子全没了 ✗。
	drop := ui.RGBA(0, 0, 0, 0.14)
	rim := ui.RGBA(0, 0, 0, 0.22)
	if t.Dark {
		drop, rim = ui.RGBA(0, 0, 0, 0.3), ui.RGBA(0, 0, 0, 0.7)
	}
	if held > 0 {
		// 按住时**更深的那层影子**。
		p.Shadow(knob, rad, 0, 8, 14, -3, drop.Alpha(held))
		// 玻璃那一层**垫在底下**：它**掺了旋钮自己的颜色**（`glass.Glass` 的 `Tint`
		// 那一档管的就是「看不看得清」）——纯玻璃在深色底上是看不见的。
		glass.Paint(p, knob, rad, glass.Glass{Tint: face})
	}
	if held < 1 {
		// 两道影子：紧的那道描边，软的那道把旋钮托起来。
		shade := 1 - held
		p.Shadow(knob, rad, 0, 0.5, 1, 0, ui.RGBA(0, 0, 0, 0.08*shade))
		p.Shadow(knob, rad, 0, 1.5, 7, 0, ui.RGBA(0, 0, 0, 0.1*shade))
		// 不透明的那一面浮在玻璃**上面**：松手时它就是旋钮的全部，按住时它淡掉、
		// 玻璃从底下浮上来——**交叉淡入**，少了交叉按住的瞬间旋钮会闪一下消失。
		p.Fill(knob, face.Alpha(1-held), rad)
	}
	if held > 0 {
		// 透镜的边：从里面透出来的一圈亮边（框架那一颗也有）。
		p.Stroke(knob, rim.Alpha(held), rad, 1)
		p.Stroke(ui.Rect{X: knob.X + 1, Y: knob.Y + 1, W: knob.W - 2, H: knob.H - 2},
			ui.RGBA(255, 255, 255, 0.25*held), rad-1, 1)
	}
	if focus {
		p.FocusRing(knob, [4]float32{rad, rad, rad, rad})
	}
}
