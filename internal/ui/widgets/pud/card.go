package pud

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scrim"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/shadow"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

// GridCols 是卡片网格在**窗口宽** winW 时排几列（原型 `.card-grid`：6 列，`max-width` 1280 / 1180 / 760
// 三条媒体查询降到 5 / 4 / 2 列，index.html 321、911、913、964 行）。窗口宽是视口，不是内容区宽。
//
//	> 1280 → 6   > 1180 → 5   > 760 → 4   ≤ 760 → 2
func GridCols(winW float32) int {
	switch {
	case winW > 1280:
		return 6
	case winW > 1180:
		return 5
	case winW > 760:
		return 4
	}
	return 2
}

// VideoGridCols 是视频卡网格（原型 `.vgrid`）在窗口宽 winW 时的列数：4 列，≤ 1180 时 3 列，
// ≤ 760 时 1 列（index.html 772、907、908 行）。
func VideoGridCols(winW float32) int {
	switch {
	case winW > 1180:
		return 4
	case winW > 760:
		return 3
	}
	return 1
}

// CellWidth 是 n 列、格间距 gap 时每一格的宽。
func CellWidth(w float32, n int, gap float32) float32 {
	if n < 1 {
		n = 1
	}
	return (w - float32(n-1)*gap) / float32(n)
}

// CardEvents 是卡片上这一帧发生的事。
type CardEvents struct {
	// Open 是点了卡片（封面或标题）；Play 是点了封面上那枚播放键。
	Open, Play bool
}

// CoverCard 是一张方形封面卡（原型 `.card` + `.cover` + `.cover-play`）：w 见方的
// 封面（圆角 8、surface-2 底、图完整显示不裁切），悬停 / 正在播放时右下角浮出一枚
// 44 的主色播放键（离边 12、从下 6 淡入）；下面 14 号 600 的标题（离封面 12）和
// 13 号次要色的副题（离标题 2）。
func CoverCard(c *ui.Context, key, title, sub string, w float32, cover Cover, playing bool) (ev CardEvents) {
	p := pal(c)
	col := ui.Column(c).Key(key).Width(w)
	hot := col.Hovered()
	col.Children(func() {
		b := ui.ButtonBase(c).Key(key+":open").Label(title).Size(w, w).Radius(tok.R8).
			Background(p.Surface2).Clip()
		b.Children(func() {
			if cover != nil {
				cover(c, w, w)
			}
			ev.Play = coverPlay(c, key+":play", title, hot || playing)
		})
		ev.Open = b.Clicked()
		cardText(c, title, sub, p)
	})
	return ev
}

// coverPlay 是封面右下角那枚播放键（`.cover-play`）：shown 为假时透明、往下 6。
func coverPlay(c *ui.Context, key, title string, shown bool) bool {
	p := pal(c)
	b := ui.ButtonBase(c).Key(key).Label("播放 "+title).Size(tok.CoverPlay, tok.CoverPlay).
		Radius(tok.CoverPlay / 2).Background(p.Accent).AlignItems(ui.Center).Justify(ui.Center).
		Absolute().Right(tok.CoverPlayInset).Bottom(tok.CoverPlayInset)
	b = shadow.CoverPlay(b)
	// Pose.Fade 是「淡掉多少」（0 = 完全可见，1 = 全透明），和 CSS opacity 相反。
	pose := ui.Pose{Y: 6, Fade: 1}
	if shown {
		pose = ui.Pose{}
	}
	b = b.AnimatePose(pose, ui.Animation{Duration: motion.Med, Ease: motion.EaseOut})
	b.Children(func() { glyph(c, "play", 20, p.AccentInk) })
	return shown && b.Clicked()
}

func cardText(c *ui.Context, title, sub string, p tok.Palette) {
	text(c, title, scale.Size14, p.Text).FontWeight(600).SingleLine().Ellipsis("…").
		Margin(space.Px12, space.None, space.None, space.None)
	if sub != "" {
		text(c, sub, scale.Size13, p.Text2).SingleLine().Ellipsis("…").Margin(space.Px2, space.None, space.None, space.None)
	}
}

// ColCard 是收藏夹卡（原型 `colCard`）：和 CoverCard 同一个样子，没有封面时画一枚
// 40 的文件夹图标；没有悬停播放键（点开进收藏夹页）。sub 一般是
// 「收藏夹 · 128 首 · 3.2万 人收藏」。
func ColCard(c *ui.Context, key, title, sub string, w float32, cover Cover) (open bool) {
	p := pal(c)
	ui.Column(c).Key(key).Width(w).Children(func() {
		b := ui.ButtonBase(c).Key(key+":open").Label("打开 "+title).Size(w, w).Radius(tok.R8).
			Background(p.Surface2).Clip().AlignItems(ui.Center).Justify(ui.Center)
		b.Children(func() {
			if cover != nil {
				cover(c, w, w)
			} else {
				glyph(c, "folder", 40, p.Text3)
			}
		})
		open = b.Clicked()
		cardText(c, title, sub, p)
	})
	return open
}

// Video 是一张视频卡要画的东西（原型 `videoCard`）。
type Video struct {
	Title, Sub string
	// Plays / Danmaku / Duration 是封面上那两块读数（已经格式化好）。
	Plays, Danmaku, Duration string
	// Tint 是封面平均色：16:9 框里留白的地方用它和 #0B0B0C 混 40% 垫底。
	Tint    ui.Color
	Cover   Cover
	Playing bool
}

// VideoCard 是一张视频卡：16:9 的缩略图（圆角 8，图完整显示，留白处垫封面色），
// 左下角「▶ 播放 · 弹幕」、右下角时长两块 66% 黑底白字 12 号等宽的小牌；下面两行
// 标题（正在播放时 accent-text）和一行副题。
func VideoCard(c *ui.Context, key string, v Video, w float32) (open bool) {
	p := pal(c)
	h := w * 9 / 16
	thumbBg := tok.Full.Base
	if v.Tint.A != 0 {
		thumbBg = tok.Full.Base.Mix(v.Tint, tok.TintVideoThumb)
	}
	ui.Column(c).Key(key).Width(w).Children(func() {
		b := ui.ButtonBase(c).Key(key+":open").Label("播放 "+v.Title).Size(w, h).Radius(tok.R8).
			Background(thumbBg).Clip()
		b.Children(func() {
			if v.Cover != nil {
				v.Cover(c, w, h)
			}
			stat := func() ui.Element {
				return ui.Row(c).Background(ui.RGBA(0, 0, 0, scrim.VideoChip)).Radius(6).Padding(space.Px3, space.Px8).Gap(space.Px10).
					AlignItems(ui.Center)
			}
			stat().Absolute().Left(8).Bottom(8).Children(func() {
				for _, it := range [][2]string{{"play", v.Plays}, {"danmaku", v.Danmaku}} {
					if it[1] == "" {
						continue
					}
					ui.Row(c).Gap(space.Px4).AlignItems(ui.Center).Children(func() {
						glyph(c, it[0], tok.IconXs, ui.RGB(255, 255, 255))
						typo.Mono(ui.Text(c, it[1])).FontSize(12).FontWeight(500).
							TextColor(ui.RGB(255, 255, 255)).SingleLine()
					})
				}
			})
			if v.Duration != "" {
				typo.Mono(ui.Text(c, v.Duration)).FontSize(12).FontWeight(500).TextColor(ui.RGB(255, 255, 255)).
					Background(ui.RGBA(0, 0, 0, scrim.VideoChip)).Radius(6).Padding(space.Px3, space.Px6).SingleLine().
					Absolute().Right(8).Bottom(8)
			}
		})
		open = b.Clicked()
		fg := p.Text
		if v.Playing {
			fg = p.AccentText
		}
		text(c, v.Title, scale.Size14, fg).FontWeight(600).MaxLines(2).Ellipsis("…").
			Margin(space.Px12, space.None, space.None, space.None)
		if v.Sub != "" {
			text(c, v.Sub, scale.Size13, p.Text2).SingleLine().Ellipsis("…").Margin(space.Px2, space.None, space.None, space.None)
		}
	})
	return open
}

// Avatar 是一枚圆头像（原型 `avatar()`）：没有图时是 tint 底 + 名字首字（宋体 700、
// 字号是边长的 0.42、白字）；有图时 image 在圆里画满。tint 零值用主色。
func Avatar(c *ui.Context, name string, size float32, tint ui.Color, image Cover) ui.Element {
	if tint.A == 0 {
		tint = pal(c).Accent
	}
	first := ""
	for _, r := range name {
		first = string(r)
		break
	}
	return ui.Box(c).Size(size, size).Radius(size / 2).Background(tint).Clip().Shrink(0).
		AlignItems(ui.Center).Justify(ui.Center).Children(func() {
		if image != nil {
			image(c, size, size)
			return
		}
		typo.Display(ui.Text(c, first), size*0.42).TextColor(ui.RGB(255, 255, 255))
	})
}

// UpCard 是一张 UP 主卡要画的东西（原型 `ucard`）。
type UpCard struct {
	Name, Sign, Level string
	// Stats 是「3.2万 粉丝 · 1.1万 获赞」那一行（12 号等宽 text-3）。
	Stats     string
	Tint      ui.Color
	Face      Cover
	Following bool
	// HideFollow 不画关注键（拿不到关注状态、也没有关注接口的场合，如搜索结果）。
	HideFollow bool
}

// UpCardEvents 是 UP 主卡上这一帧发生的事。
type UpCardEvents struct{ Open, Follow bool }

// UCard 是一张 UP 主卡（原型 `.ucard`）：56 头像 | 名字（16 号 600 + LV 角标）/
// 两行签名（13 号次要色）/ 统计 | 关注键（小号，关注了是幽灵键「已关注」、没关注
// 是主色「关注」）。内边距 16、圆角 12、line 描边；悬停描边换 line-strong、底换 surface。
func UCard(c *ui.Context, key string, u UpCard, w float32) (ev UpCardEvents) {
	p := pal(c)
	card := ui.ButtonBase(c).Key(key).Label("打开 " + u.Name + " 的空间").Width(w).Padding(space.Px16).
		Gap(space.Px16).AlignItems(ui.Center).Radius(tok.R12)
	border, bg := p.Line, ui.Color{}
	if card.Hovered() {
		border, bg = p.LineStrong, p.Surface
	}
	card = card.Border(1, border).Background(bg).Transition(motion.Fade)
	card.Children(func() {
		Avatar(c, u.Name, 56, u.Tint, u.Face)
		ui.Column(c).Grow(1).Shrink(1).Children(func() {
			ui.Row(c).Gap(space.Px6).AlignItems(ui.Center).Children(func() {
				text(c, u.Name, scale.Size16, p.Text).FontWeight(600).SingleLine().Ellipsis("…").Shrink(1)
				if u.Level != "" {
					Badge(c, "LV"+u.Level, false)
				}
			})
			if u.Sign != "" {
				text(c, u.Sign, scale.Size13, p.Text2).MaxLines(2).Ellipsis("…").Margin(space.Px2, space.None, space.None, space.None)
			}
			if u.Stats != "" {
				typo.Mono(ui.Text(c, u.Stats)).FontSize(scale.Font(c, scale.Size12)).TextColor(p.Text3).
					SingleLine().Margin(space.Px2, space.None, space.None, space.None)
			}
		})
		if u.HideFollow {
			return
		}
		kind, label := Primary, "关注"
		if u.Following {
			kind, label = Ghost, "已关注"
		}
		ev.Follow = Button(c, key+":follow", kind, Sm, label, "").Clicked()
	})
	ev.Open = card.Clicked() && !ev.Follow
	return ev
}
