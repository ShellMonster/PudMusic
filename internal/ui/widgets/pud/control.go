package pud

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

// Switch 是开关（原型 `.switch`）：44×26 的胶囊，关是 line-strong、开是主色；
// 20 的白色旋钮，离边 3，开时右移 18，带一点回弹（`cubic-bezier(.3,1.4,.5,1)`）。
// 点一下改 *on，返回这一帧是否被点了。label 是无障碍名字。
func Switch(c *ui.Context, key string, on *bool, label string) (toggled bool) {
	p := pal(c)
	c.Key(key)
	sw := ui.SwitchBase(c, on).Label(label).Size(tok.SwitchW, tok.SwitchH).
		Radius(tok.SwitchH / 2).Shrink(0)
	pos := float32(0)
	if *on {
		pos = 1
	}
	sw = sw.Background(p.LineStrong.Mix(p.Accent, pos)).Transition(motion.Fade)
	sw.Children(func() {
		d := tok.SwitchKnob
		knob := ui.Box(c).Key(key+":knob").Absolute().Left(3).Top(3).Size(d, d).Radius(d / 2).
			Background(ui.RGB(255, 255, 255))
		knob = knob.Draw(func(pp *ui.Painter, r ui.Rect) {
			pp.Shadow(r, d/2, 0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.3))
		})
		knob.AnimatePose(ui.Pose{X: 18 * pos}, ui.Animation{Duration: motion.Med, Ease: motion.SwitchSpring})
	})
	return sw.Changed()
}

// SelectBox 是下拉框的**外观**（原型 `.select`）：高 36、左 12 右 32、圆角 8、
// line-strong 描边、surface-2 底、13 号字，右边一枚 14 的向下箭头（text-3）。
// 它只画框和当前值，展开的列表由调用方用 `PopPanel` + `PopOpt` 摆。
func SelectBox(c *ui.Context, key, current string, w float32) ui.Element {
	p := pal(c)
	b := ui.ButtonBase(c).Key(key).Label(current).Height(tok.SelectH).Radius(tok.R8).
		Padding(space.None, space.Px10, space.None, space.Px12).Gap(space.Px8).AlignItems(ui.Center).Background(p.Surface2)
	if w > 0 {
		b = b.Width(w)
	}
	border := p.LineStrong
	if b.Hovered() {
		border = p.Text3
	}
	b = b.Border(1, border).Transition(motion.Fade)
	b.Children(func() {
		text(c, current, scale.Size13, p.Text).SingleLine().Ellipsis("…").Grow(1)
		glyph(c, "down", 14, p.Text3)
	})
	return b
}

// MiniSearch 是列表工具栏里的小搜索框（原型 `.mini-search`）：高 32、左右 12、
// 胶囊、surface-2 底，14 的放大镜（text-3）加一个 13 号输入框（默认 140 宽）。
// value 是输入框里的字；返回输入框本身，调用方问 `.Changed()`。
func MiniSearch(c *ui.Context, key string, value *string, placeholder string, inputW float32) ui.Element {
	p := pal(c)
	if inputW == 0 {
		inputW = 140
	}
	var in ui.Element
	ui.Row(c).Key(key).Height(tok.MiniSearchH).PaddingX(space.Px12).Gap(space.Px8).AlignItems(ui.Center).
		Radius(tok.MiniSearchH / 2).Background(p.Surface2).Children(func() {
		glyph(c, "search", 14, p.Text3)
		c.Key(key + ":input")
		in = ui.TextInputBase(c, value).Label(placeholder).Placeholder(placeholder).
			Width(inputW).FontSize(scale.Font(c, scale.Size13)).TextColor(p.Text)
	})
	return in
}

// RangeFill 画一条进度 / 音量轨道（原型 `.range`）：轨道高 4、圆角 2，已走的那一段
// 平时是正文色、悬停时换主色（`.range:hover` 把填充换成 accent），没走的是 line-strong；
// 悬停 / 按下时在末端出一颗 12 的圆钮。frac 是 0..1。hot 为真时悬停态。
// 滑块的交互用 `internal/ui` 的 selfSlider（拖动、键盘）——它的画法已经调成这个样子。
func RangeFill(p *ui.Painter, r ui.Rect, t tok.Palette, frac float32, hot bool) {
	h := tok.RangeTrack
	y := r.Y + (r.H-h)/2
	frac = min(max(frac, 0), 1)
	p.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: h}, t.LineStrong, h/2)
	fill := t.Text
	if hot {
		fill = t.Accent
	}
	p.Fill(ui.Rect{X: r.X, Y: y, W: r.W * frac, H: h}, fill, h/2)
	if hot {
		d := tok.RangeThumb
		p.Fill(ui.Rect{X: r.X + r.W*frac - d/2, Y: r.Y + (r.H-d)/2, W: d, H: d}, t.Text, d/2)
	}
}
