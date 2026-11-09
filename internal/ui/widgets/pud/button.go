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
// 左边、16 号、和字隔 8。返回按钮本身：调用方照常问 `.Clicked()`。
//
// ⚠️ 要禁用就用 `ButtonDisabled` / `RoundButtonDisabled`，不要在返回之后链 `.Disabled(…)`：
// 悬停底、按下缩放和禁用半透明都是建的时候定的，后链的禁用只拦得住点击，指针落上去照样亮。
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

// ButtonDisabled 是 Button 加「禁用」（原型 `.btn:disabled { opacity:.45 }` 且 `:hover:not(:disabled)`）：
// disabled 时没有悬停底 / 悬停色 / 按下缩放，画出来 45% 不透明。
func ButtonDisabled(c *ui.Context, key string, kind Kind, size Size, label, icon string, disabled bool) ui.Element {
	return button(c, key, kind, size, label, "", icon, false, disabled)
}

// RoundButton 是圆形的那一档（原型 `.btn-round`：宽 = 高，只放一枚图标）。label 是
// 无障碍名字。
func RoundButton(c *ui.Context, key string, kind Kind, size Size, icon, label string) ui.Element {
	return button(c, key, kind, size, label, "", icon, true, false)
}

// RoundButtonDisabled 是 RoundButton 加「禁用」（同 `ButtonDisabled`）。
func RoundButtonDisabled(c *ui.Context, key string, kind Kind, size Size, icon, label string, disabled bool) ui.Element {
	return button(c, key, kind, size, label, "", icon, true, disabled)
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
	off := disabled
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
			if mono != "" {
				ui.Text(c, label).FontSize(scale.Font(c, fs)).FontWeight(600).TextColor(fg).
					SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
			} else {
				ui.Text(c, label).FontSize(scale.Font(c, fs)).FontWeight(600).TextColor(fg).
					SingleLine()
			}
			if mono != "" {
				typo.Mono(ui.Text(c, mono)).FontSize(scale.Font(c, fs)).FontWeight(600).
					TextColor(fg).SingleLine().Shrink(0)
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
	// （`WithFull`）里透明底（原型 app.js:1145 `.cnt` 内联 `background:transparent`），字挪到
	// 图形外侧右上角——不然透明底的字压在图形线条上读不出来。
	Count string
	// Size 是图标边长；0 = 18。
	Size float32
	// Color 覆盖平时的图形色；零值 = text-2。
	Color ui.Color
	// HoverColor 覆盖悬停时的图形色；零值 = 悬停换 text（原型 `.icon-btn:hover { color: var(--text) }`，
	// 全屏播放器里 `.full-ctrl .icon-btn { color: #d8d7d4 }` 之上悬停仍然是 `#ecebe8`）。
	HoverColor ui.Color
	// Disabled 是禁用：没有悬停底 / 悬停色 / 按下缩放，画出来 35% 不透明。必须在这里传——
	// 返回之后再链 `.Disabled(true)` 只拦得住点击，悬停底照样亮（悬停是建的时候定的）。
	Disabled bool
}

// countBadgeFont 是 `.cnt` 的字号（index.html:740 `font: 600 10px/1`）。
const countBadgeFont = 10

// countBadgeHook 只给测试用：每画一枚计数角标，把图形和计数文字两个元素交出去量 Bounds。
var countBadgeHook func(glyph, count ui.Element)

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
	off := o.Disabled
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
			// 600 10px/1 等宽 text-2，底色 side。
			// 宽随字走（不先量字：量字用的是正文字体，等宽字体 + 「万」这类字更宽，按量出来的宽
			// 会把「1.2万」截成「1.2…」）。
			label := func() ui.Element {
				return typo.Mono(ui.Text(c, o.Count)).FontSize(countBadgeFont).FixedLineHeight(countBadgeFont).
					FontWeight(600).TextColor(p.Text2).SingleLine()
			}
			var t ui.Element
			if fullMode {
				// 全屏播放器里透明底（app.js:1145 内联 `background:transparent`——那里的底是封面
				// 取色，一块 side 色的方块会很突兀）。没了底，原型那个位置（right -4、top 1）的字
				// 就直接压在图形线条上，「1.2万」根本读不出来——所以字挪到图形**外侧右上角**：
				// 底边落在图形顶边之上 1，左缘从图形中线往右 2 起。不从图形右缘起：「1.2万」宽 ~28，
				// 从右缘起会伸到键外 17，正好顶到隔 8 的下一枚键的图形；从中线起只伸出 ~12。
				// 键本身不加外边距，命中区还是 hit×hit，同排的键位置不变。
				gTop := (hit - size) / 2
				// 用 Bottom 定底边而不是 Top + 字号：「万」走 CJK 回退字体，行盒比 10 高（实测 14），
				// 按字号推顶边的话底边照样压进图形。
				ui.Box(c).Absolute().Left(hit/2 + space.Px2).Bottom(hit - gTop + space.Px1).
					Children(func() { t = label() })
			} else {
				ui.Box(c).Height(countBadgeFont+2*space.Px2).Background(p.Side).
					Padding(space.Px2, space.Px4).Radius(tok.R6).
					Absolute().Top(space.Px1).Right(-space.Px4).Children(func() { t = label() })
			}
			if countBadgeHook != nil {
				countBadgeHook(g, t)
			}
		}
	})
	return b
}

// Chip 是一枚小胶囊（原型 `.chip`）：高 30、左右 12、surface-2 底、次要色 13 号；
// 悬停正文色 + press 底；pressed（选中）时反色（正文色底、bg 色字）。icon 可空，12 号。
//
// ⚠️ 要禁用就用 `ChipDisabled`，不要在返回之后链 `.Disabled(…)`：悬停底是建的时候定的，
// 后链的禁用只拦得住点击，指针落上去照样亮（同 `Button`）。
func Chip(c *ui.Context, key, label, icon string, pressed bool) ui.Element {
	return chip(c, key, label, icon, pressed, false)
}

// ChipDisabled 是 Chip 加「禁用」（同 `ButtonDisabled`）：disabled 时没有悬停底 / 悬停色，
// 画出来 45% 不透明（原型 `.chip` 没写禁用样式，跟 `.btn:disabled` 的 .45）。
func ChipDisabled(c *ui.Context, key, label, icon string, pressed, disabled bool) ui.Element {
	return chip(c, key, label, icon, pressed, disabled)
}

func chip(c *ui.Context, key, label, icon string, pressed, disabled bool) ui.Element {
	p := pal(c)
	b := ui.ButtonBase(c).Key(key).Height(tok.ChipH).PaddingX(space.Px12).Radius(tok.ChipH / 2).
		Gap(space.Px6).AlignItems(ui.Center).Shrink(0).Disabled(disabled)
	bg, fg := p.Surface2, p.Text2
	switch {
	case pressed:
		bg, fg = p.Text, p.Bg
	case b.Hovered() && !disabled:
		bg, fg = p.Press, p.Text
	}
	b = b.Background(bg).Transition(motion.Fade)
	if disabled {
		// 框架对禁用元素自己再乘 0.5，换算见 `frameworkDisabledOpacity`：0.9 × 0.5 = 0.45。
		b = b.Opacity(btnDisabledOpacity / frameworkDisabledOpacity)
	}
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
