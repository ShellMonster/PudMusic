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

// ButtonDisabled 是 Button 加「禁用」。禁用要在建的时候就告诉它，而不是返回之后再 `.Disabled(…)`：
// 悬停底是建的时候按 `Hovered()` 铺的，返回之后再禁用就晚了——禁用的键指针落上去照样亮，
// 等于告诉用户这里点得动（原型 `.btn:disabled { opacity:.45 }` 且 `:hover:not(:disabled)`）。
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
	hot := b.Hovered() && !b.IsDisabled()
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
	scaleTo := float32(1)
	if b.Pressed() && !b.IsDisabled() {
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
	// Count 是右上角的计数角标（原型 `.cnt`，如 "1k+"）；空串不画。
	Count string
	// Size 是图标边长；0 = 18。
	Size float32
	// Color 覆盖平时的图形色；零值 = text-2。
	Color ui.Color
	// HoverColor 覆盖悬停时的图形色；零值 = 悬停换 text（原型 `.icon-btn:hover { color: var(--text) }`，
	// 全屏播放器里 `.full-ctrl .icon-btn { color: #d8d7d4 }` 之上悬停仍然是 `#ecebe8`）。
	HoverColor ui.Color
	// Disabled 是禁用。⚠️ 必须在这里传，不能在返回之后链 `.Disabled(...)`：悬停底和悬停色是
	// 构建时按 `IsDisabled()` 判的，返回之后再禁用，禁用的键悬停时照样亮。
	Disabled bool
}

// IconBtn 是一枚只有图标的键（原型 `.icon-btn`）：圆角 8 的方块，平时次要色，
// 悬停 hover 底 + 正文色，按下图形缩到 0.92，禁用 35% 不透明。label 是无障碍名字。
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
	hot := b.Hovered() && !b.IsDisabled()
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
	if b.IsDisabled() {
		b = b.Opacity(0.35)
	}
	b = b.Transition(motion.Fade)
	b.Children(func() {
		// 「开着」时原型还给路径填 22% 的淡色（`.icon-btn.on path { fill-opacity: .22 }`）；
		// 框架的图标是单色遮罩，做不出半透明填充——心 / 星 / 赞请直接传 "heart-on" 这类
		// 填满的那一版。
		g := glyph(c, icon, size, fg)
		pressed := float32(1)
		if b.Pressed() && !b.IsDisabled() {
			pressed = 0.92
		}
		g.AnimatePose(ui.Pose{Scale: pressed}, ui.Animation{Duration: motion.Fast, Ease: motion.EaseOut})
		if o.Count != "" {
			// `.cnt`：top 1 / right -4，侧栏底、次要色、600 10 等宽、2/4 内边距、圆角 6。
			typo.Mono(ui.Text(c, o.Count)).FontSize(10).FontWeight(600).TextColor(p.Text2).
				Background(p.Side).Padding(space.Px2, space.Px4).Radius(tok.R6).SingleLine().
				Absolute().Top(1).Right(-4)
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
	return typo.Mono(ui.Text(c, label)).FontSize(10).FontWeight(700).LetterSpacing(0.4).
		TextColor(fg).Border(1, border).Radius(tok.R4).PaddingX(space.Px6).Height(tok.BadgeH).
		SingleLine().Shrink(0)
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
