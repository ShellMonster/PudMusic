package pud

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

// Kind 是按钮的四种样子（原型 `.btn-primary` / `.btn-ghost` / `.btn-quiet` / `.btn-danger`）。
type Kind int

const (
	// Primary 是主色底 + accent-ink 字；悬停往正文色推 12%。
	Primary Kind = iota
	// Ghost 是透明底 + line-strong 描边 + 正文色字；悬停 hover 底、描边换 text-3。
	Ghost
	// Quiet 是无底无框的次要色字（左右内边距 12）；悬停正文色 + hover 底。
	Quiet
	// Danger 是 err 底 + 白字。
	Danger
)

// Size 是按钮的两档高（原型 `.btn` 40 / `.btn-sm` 32）。
type Size int

const (
	// Std 是标准按钮：高 40、左右 20、字 14。
	Std Size = iota
	// Sm 是小按钮：高 32、左右 14、字 13。
	Sm
)

// Button 是一枚胶囊按钮（原型 `.btn`）。icon 是原型图标名（"" 不画），画在字的
// 左边、16 号、和字隔 8。返回按钮本身：调用方照常问 `.Clicked()`、链 `.Disabled(…)`。
//
//	if pud.Button(c, "play-all", pud.Primary, pud.Std, "播放全部", "play").Clicked() { … }
func Button(c *ui.Context, key string, kind Kind, size Size, label, icon string) ui.Element {
	return button(c, key, kind, size, label, "", icon, false, false)
}

// ButtonMono 是 Button 加上字后面一小段等宽字（原型 `继续播放「歌名」<span class="mono">0:37</span>`）：
// 字和等宽那段之间隔 8（`.btn` 的 gap）。
func ButtonMono(c *ui.Context, key string, kind Kind, size Size, label, mono, icon string) ui.Element {
	return button(c, key, kind, size, label, mono, icon, false, false)
}

// ButtonDisabled 是 Button 加「禁用」（原型 `.btn:disabled { opacity:.45 }` 且 `:hover:not(:disabled)`）。
// 返回之后再链 `.Disabled(true)` 也一样不亮悬停底（见 `disabledNow`），在建的时候给只是省掉那一帧。
func ButtonDisabled(c *ui.Context, key string, kind Kind, size Size, label, icon string, disabled bool) ui.Element {
	return button(c, key, kind, size, label, "", icon, false, disabled)
}

// RoundButton 是圆形的那一档（原型 `.btn-round`：宽 = 高，只放一枚图标）。label 是
// 无障碍名字。
func RoundButton(c *ui.Context, key string, kind Kind, size Size, icon, label string) ui.Element {
	return button(c, key, kind, size, label, "", icon, true, false)
}

func button(c *ui.Context, key string, kind Kind, size Size, label, mono, icon string, round, disabled bool) ui.Element {
	p := pal(c)
	h, padX, fs := tok.BtnH, float32(20), float32(scale.Size14)
	if size == Sm {
		h, padX, fs = tok.BtnSmH, 14, scale.Size13
	}
	if kind == Quiet {
		padX = 12
	}
	b := ui.ButtonBase(c).Key(key).Height(h).Radius(h / 2).Gap(space.Px8).
		AlignItems(ui.Center).Justify(ui.Center).Shrink(0).Disabled(disabled)
	if round {
		b = b.Width(h).Label(label)
	} else {
		b = b.PaddingX(padX)
	}
	off := disabledNow(c, b)
	hot := b.Hovered() && !off
	var bg, fg, border ui.Color
	switch kind {
	case Primary:
		bg, fg = p.Accent, p.AccentInk
		if hot {
			bg = p.Accent.Mix(p.Text, 0.12)
		}
	case Ghost:
		fg, border = p.Text, p.LineStrong
		if hot {
			bg, border = p.Hover, p.Text3
		}
	case Quiet:
		fg = p.Text2
		if hot {
			fg, bg = p.Text, p.Hover
		}
	case Danger:
		// 原型 `.btn-danger { background: var(--err); color: #fff }`：白字，两套主题都一样。
		bg, fg = p.Err, ui.RGB(255, 255, 255)
	}
	b = b.Background(bg).Border(1, border).TextColor(fg).Transition(motion.Fade)
	if off {
		// 原型 `.btn:disabled { opacity: .45 }`（index.html:289）。框架对禁用元素自己再乘 0.5
		// （mygo ui/paint.go 的 `flagDisabled`），所以这里给 0.9：0.9 × 0.5 = 0.45。
		b = b.Opacity(btnDisabledOpacity / frameworkDisabledOpacity)
	}
	scaleTo := float32(1)
	if b.Pressed() && !off {
		scaleTo = 0.97
	}
	b = b.AnimatePose(ui.Pose{Scale: scaleTo}, ui.Animation{Duration: motion.Fast, Ease: motion.EaseOut})
	b.Children(func() {
		if icon != "" {
			glyph(c, icon, tok.IconSm, fg)
		}
		if !round {
			ui.Text(c, label).FontSize(scale.Font(c, fs)).FontWeight(600).TextColor(fg).SingleLine()
			if mono != "" {
				typo.Mono(ui.Text(c, mono)).FontSize(scale.Font(c, fs)).FontWeight(600).TextColor(fg).SingleLine()
			}
		}
	})
	return b
}

// IconOpts 是图标键的几样可选项。
type IconOpts struct {
	// Row 是列表行内那一档（命中区 32，原型 `.trow .t-act .icon-btn`）；否则 36。
	Row bool
	// On 是「开着」：图形换 accent-text（原型 `.icon-btn.on`）。
	On bool
	// Count 是右上角的计数角标（原型 `.cnt`，如 "1k+"）；空串不画。平时是 side 底，全屏播放器
	// （`WithFull`）里透明底（原型 app.js:1145 `.cnt` 内联 `background:transparent`）。
	Count string
	// Size 是图标边长；0 = 18。
	Size float32
	// Color 覆盖平时的图形色；零值 = text-2。
	Color ui.Color
	// HoverColor 覆盖悬停时的图形色；零值 = 悬停换 text（原型 `.icon-btn:hover { color: var(--text) }`，
	// 全屏播放器里 `.full-ctrl .icon-btn { color: #d8d7d4 }` 之上悬停仍然是 `#ecebe8`）。
	HoverColor ui.Color
	// Disabled 是禁用。在这里传和返回之后链 `.Disabled(true)` 效果一样（见 `disabledNow`）；
	// 在这里传能省掉「先按可点画一帧」的那一帧。
	Disabled bool
}

const countBadgeFallbackTextW = 20

// countBadgeFont 是 `.cnt` 的字号（index.html:740 `font: 600 10px/1`）。
const countBadgeFont = 10

// countBadgeWidth 是 `.cnt` 角标的宽：字宽 + 左右各 4（原型 index.html:740 `padding: 2px 4px`）。
func countBadgeWidth(measured float32) float32 {
	if measured <= 0 {
		measured = countBadgeFallbackTextW
	}
	return measured + 2*space.Px4
}

// 禁用态的不透明度（原型 index.html:228 `.icon-btn:disabled { opacity: .35 }`、
// index.html:289 `.btn:disabled { opacity: .45 }`）。
//
// ⚠️ 框架对带 `Disabled(true)` 的元素在绘制时**自己再乘 0.5**（mygo ui/paint.go 的
// `flagDisabled` 那一支），所以组件自己给的是「目标 ÷ 0.5」，乘出来才是原型那个数。
// 以前 IconBtn 直接给 0.35，实际成了 0.175，顶栏后退 / 前进禁用时几乎看不见。
const (
	iconBtnDisabledOpacity   = 0.35
	btnDisabledOpacity       = 0.45
	frameworkDisabledOpacity = 0.5
)

// disabledProbe 记着一枚键**上一次绘制时**是不是禁用的。
type disabledProbe struct {
	el   ui.Element
	c    *ui.Context
	seen bool
	draw func(*ui.Painter, ui.Rect)
}

type disabledProbeKey struct{}

// disabledNow 报这枚键这一帧该不该按禁用画（悬停底 / 悬停色 / 按下缩放 / 半透明）。
//
// 调用方常常在**拿到返回值之后**才链 `.Disabled(true)`（`pud.Button(...).Disabled(x)`），
// 而悬停底是建的时候铺的——那一刻 `IsDisabled()` 还是假，禁用的键指针落上去照样亮、按下去照样缩
// （原型是 `:hover:not(:disabled)` / `:active:not(:disabled)`）。这里在绘制回调里看一眼真实的禁用
// 状态（绘制在整棵树建完之后，那时调用方链的 `.Disabled` 已经生效），记下来；和上一次不一样就
// `Invalidate`，下一帧按真的那一档重建。于是调用方不用改链的顺序，最多差一帧（框架自己的
// 点击判定也按「上一帧禁用了没有」算，同一个口径）。
//
// 绘制回调建一次存在元素状态里，之后每帧复用，不产生每帧分配。
func disabledNow(c *ui.Context, b ui.Element) bool {
	s := ui.Local(b, disabledProbeKey{}, func() disabledProbe { return disabledProbe{} })
	if s.draw == nil {
		s.draw = func(*ui.Painter, ui.Rect) {
			if d := s.el.IsDisabled(); d != s.seen {
				s.seen = d
				s.c.Invalidate()
			}
		}
	}
	s.el, s.c = b, c
	b.Draw(s.draw)
	return b.IsDisabled() || s.seen
}

// IconBtn 是一枚只有图标的键（原型 `.icon-btn`）：圆角 8 的方块，平时次要色，
// 悬停 hover 底 + 正文色，按下图形缩到 0.92，禁用 35% 不透明（index.html:228，
// 换算见 `iconBtnDisabledOpacity`）。label 是无障碍名字。
func IconBtn(c *ui.Context, key, icon, label string, o IconOpts) ui.Element {
	p := pal(c)
	hit := tok.IconHit
	if o.Row {
		hit = tok.IconHitRow
	}
	size := o.Size
	if size == 0 {
		size = tok.Icon
	}
	b := ui.ButtonBase(c).Key(key).Label(label).Size(hit, hit).Radius(tok.R8).
		AlignItems(ui.Center).Justify(ui.Center).Shrink(0)
	if o.Disabled {
		b = b.Disabled(true)
	}
	fg := p.Text2
	if o.Color.A != 0 {
		fg = o.Color
	}
	off := disabledNow(c, b)
	hot := b.Hovered() && !off
	if hot {
		b = b.Background(p.Hover)
		switch {
		case o.HoverColor.A != 0:
			fg = o.HoverColor
		case o.Color.A == 0:
			fg = p.Text
		}
	}
	if o.On {
		fg = p.AccentText
	}
	if off {
		b = b.Opacity(iconBtnDisabledOpacity / frameworkDisabledOpacity)
	}
	b = b.Transition(motion.Fade)
	b.Children(func() {
		// 「开着」时原型还给路径填 22% 的淡色（`.icon-btn.on path { fill-opacity: .22 }`）；
		// 框架的图标是单色遮罩，做不出半透明填充——心 / 星 / 赞请直接传 "heart-on" 这类
		// 填满的那一版。
		g := glyph(c, icon, size, fg)
		pressed := float32(1)
		if b.Pressed() && !off {
			pressed = 0.92
		}
		g.AnimatePose(ui.Pose{Scale: pressed}, ui.Animation{Duration: motion.Fast, Ease: motion.EaseOut})
		if o.Count != "" {
			// `.cnt`（index.html:740）：右上角小角标，top 1、right -4、内边距 2/4、圆角 6、
			// 600 10px/1 等宽 text-2。底色 side；全屏播放器里透明（app.js:1145 内联
			// `background:transparent`——那里的底是封面取色，一块 side 色的方块会很突兀）。
			textW, _ := c.MeasureText(0, ui.Span{Text: o.Count, Size: countBadgeFont, Weight: 600})
			bg := p.Side
			if fullMode {
				bg = ui.Color{}
			}
			ui.Box(c).Width(countBadgeWidth(textW)).Height(countBadgeFont+2*space.Px2).Background(bg).
				Padding(space.Px2, space.Px4).Radius(tok.R6).
				Absolute().Top(space.Px1).Right(-space.Px4).Children(func() {
				typo.Mono(ui.Text(c, o.Count)).FontSize(countBadgeFont).FixedLineHeight(countBadgeFont).
					FontWeight(600).TextColor(p.Text2).SingleLine()
			})
		}
	})
	return b
}

// Chip 是一枚小胶囊（原型 `.chip`）：高 30、左右 12、surface-2 底、次要色 13 号；
// 悬停正文色 + press 底；pressed（选中）时反色（正文色底、bg 色字）。icon 可空，12 号。
func Chip(c *ui.Context, key, label, icon string, pressed bool) ui.Element {
	p := pal(c)
	b := ui.ButtonBase(c).Key(key).Height(tok.ChipH).PaddingX(space.Px12).Radius(tok.ChipH / 2).
		Gap(space.Px6).AlignItems(ui.Center).Shrink(0)
	bg, fg := p.Surface2, p.Text2
	switch {
	case pressed:
		bg, fg = p.Text, p.Bg
	case b.Hovered():
		bg, fg = p.Press, p.Text
	}
	b = b.Background(bg).Transition(motion.Fade)
	b.Children(func() {
		if icon != "" {
			glyph(c, icon, tok.IconXs, fg)
		}
		text(c, label, scale.Size13, fg).SingleLine()
	})
	return b
}

// Kbd 是一枚键帽（原型 `.kbd`）：11 号等宽 text-3、line-strong 描边、圆角 4、1/6 内边距。
func Kbd(c *ui.Context, keys string) ui.Element {
	p := pal(c)
	return typo.Mono(ui.Text(c, keys)).FontSize(scale.Font(c, scale.Size11)).TextColor(p.Text3).
		Border(1, p.LineStrong).Radius(tok.R4).Padding(space.Px1, space.Px6).SingleLine()
}

// Badge 是音质角标（原型 `.badge`）：高 18、左右 6、圆角 4、10 号等宽 700、字距
// 0.04em、line-strong 描边、次要色。hires 时描边是主色 50%、字是 accent-text。
//
//	pud.Badge(c, "HI-RES", true); pud.Badge(c, "SQ", false)
func Badge(c *ui.Context, label string, hires bool) ui.Element {
	p := pal(c)
	border, fg := p.LineStrong, p.Text2
	if hires {
		border, fg = p.Accent.Alpha(0.5), p.AccentText
	}
	// ⚠️ **字居中靠包一层盒子**，不靠 Text 的行高：Text 的内容块在自己的盒子
	// 里是顶对齐的——18 高的徽章放 10 号字（行高 ≈ 15.5），上面留白比下面多，
	// 视觉上字就贴顶（用户截图实测：上 5px / 下 1px）。原型 `.badge` 是
	// inline-flex + align-items:center，所以这里把描边/圆角/高度挂在外面的
	// Box 上、字在盒子里居中，而不是往 Text 上想办法。
	return ui.Box(c).Height(tok.BadgeH).PaddingX(space.Px6).Border(1, border).Radius(tok.R4).
		AlignItems(ui.Center).Justify(ui.Center).Shrink(0).
		Children(func() {
			typo.Mono(ui.Text(c, label)).FontSize(10).FontWeight(700).LetterSpacing(0.4).
				TextColor(fg).SingleLine()
		})
}

// DropButton 是带下拉箭头的小幽灵键（原型 `.btn.btn-ghost.btn-sm` 里放「图标 + 文字 + down」，
// 如历史记录的日期范围）：高 32、左右 14、icon 16 + 文字 13 号 600 + 右边 16 的向下箭头，缝 8。
func DropButton(c *ui.Context, key, label, icon string) ui.Element {
	p := pal(c)
	b := ui.ButtonBase(c).Key(key).Label(label).Height(tok.BtnSmH).Radius(tok.BtnSmH / 2).PaddingX(space.Px14).
		Gap(space.Px8).AlignItems(ui.Center).Justify(ui.Center).Shrink(0)
	var bg, border ui.Color
	border = p.LineStrong
	if b.Hovered() {
		bg, border = p.Hover, p.Text3
	}
	b = b.Background(bg).Border(1, border).TextColor(p.Text).Transition(motion.Fade)
	b.Children(func() {
		if icon != "" {
			glyph(c, icon, tok.IconSm, p.Text)
		}
		ui.Text(c, label).FontSize(scale.Font(c, scale.Size13)).FontWeight(600).TextColor(p.Text).SingleLine()
		glyph(c, "down", tok.IconSm, p.Text)
	})
	return b
}

// ChipCount 是带计数的 Chip（原型 `.chip` 里「标签 + <span class="mono">N</span>」）：计数是
// 12 号等宽，和标签隔 6（`.chip` 的 gap）；颜色跟着 Chip 走。
func ChipCount(c *ui.Context, key, label, count string, pressed bool) ui.Element {
	p := pal(c)
	b := ui.ButtonBase(c).Key(key).Label(label).Height(tok.ChipH).PaddingX(space.Px12).Radius(tok.ChipH / 2).
		Gap(space.Px6).AlignItems(ui.Center).Shrink(0)
	bg, fg := p.Surface2, p.Text2
	switch {
	case pressed:
		bg, fg = p.Text, p.Bg
	case b.Hovered():
		bg, fg = p.Press, p.Text
	}
	b = b.Background(bg).Transition(motion.Fade)
	b.Children(func() {
		text(c, label, scale.Size13, fg).SingleLine()
		typo.Mono(ui.Text(c, count)).FontSize(scale.Font(c, scale.Size13)).TextColor(fg).SingleLine()
	})
	return b
}
