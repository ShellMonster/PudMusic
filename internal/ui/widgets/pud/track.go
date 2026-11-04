package pud

import (
	"time"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

// 曲目行的列（原型 `.tl-head, .trow`）：
//
//	40 | 标题 2.2fr | 专辑 1.2fr | UP 主 1fr | 播放/弹幕 88 | 时长 56 | 操作 96
//
// 列与列隔 16、左右内边距 12。内容区窄于 1180（原型那条媒体查询减掉侧栏 232，
// 即内容区 < 948）时去掉「专辑」那一列，标题 2fr、UP 主 1fr。
const (
	colIdx   = 40
	colPlays = 88
	colDur   = 56
	colAct   = 96
	colGap   = 16
	rowPadX  = 12
	// HideAlbumBelow 是去掉「专辑」那一列的内容区宽度。
	HideAlbumBelow = 948
)

// AlbumMode 是「专辑」那一列怎么定：默认（自动）按列表宽度判，调用方也可以直接说画不画。
//
// 原型那条媒体查询看的是**窗口**宽（≤ 1180 去掉专辑列），而自动档拿的是**列表**宽——列表
// 比内容区窄一圈页边距，默认 1200 的窗口里列表是 888，会被自动档误判成「太窄」。页面知道
// 窗口宽，所以用 `AlbumShown` / `AlbumHidden` 自己说了算。
type AlbumMode int

const (
	AlbumAuto AlbumMode = iota
	AlbumShown
	AlbumHidden
)

// trackCols 是这一宽度下三列弹性列的宽：标题、专辑（0 = 不画）、UP 主。
func trackCols(w float32) (main, album, owner float32) {
	return trackColsMode(w, AlbumAuto)
}

func trackColsMode(w float32, mode AlbumMode) (main, album, owner float32) {
	hide := w > 0 && w < HideAlbumBelow
	switch mode {
	case AlbumShown:
		hide = false
	case AlbumHidden:
		hide = true
	}
	n := float32(7)
	if hide {
		n = 6
	}
	avail := w - 2*rowPadX - colIdx - colPlays - colDur - colAct - (n-1)*colGap
	if avail < 120 {
		avail = 120
	}
	if hide {
		return avail * 2 / 3, 0, avail / 3
	}
	return avail * 2.2 / 4.4, avail * 1.2 / 4.4, avail / 4.4
}

// TrackHead 是曲目列表的表头（原型 `.tl-head`）：高 40、12 号 text-3、底下一条 line、
// 离第一行 4。w 是列表宽（就是内容区减页边距）。
func TrackHead(c *ui.Context, w float32) { TrackHeadMode(c, w, AlbumAuto) }

// TrackHeadMode 是 TrackHead 加上「专辑列画不画」由调用方说了算（见 `AlbumMode`）。
func TrackHeadMode(c *ui.Context, w float32, mode AlbumMode) {
	p := pal(c)
	main, album, owner := trackColsMode(w, mode)
	ui.Row(c).Width(w).Height(40).PaddingX(space.Px12).Gap(space.Px16).AlignItems(ui.Center).
		Margin(space.None, space.None, space.Px4, space.None).
		Draw(func(pp *ui.Painter, r ui.Rect) {
			pp.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - 1, W: r.W, H: 1}, p.Line, 0)
		}).
		Children(func() {
			cell := func(s string, w float32, end bool) {
				t := text(c, s, scale.Size12, p.Text3).Width(w).SingleLine()
				if end {
					t.TextAlign(ui.End)
				}
			}
			text(c, "#", scale.Size12, p.Text3).Width(colIdx).TextAlign(ui.Center)
			cell("标题", main, false)
			if album > 0 {
				cell("专辑", album, false)
			}
			cell("UP 主", owner, false)
			cell("播放 / 弹幕", colPlays, true)
			cell("时长", colDur, true)
			ui.Box(c).Width(colAct)
		})
}

// Track 是一行曲目要画的东西。全是已经格式化好的字（组件不碰数据格式）。
type Track struct {
	// Index 是序号那一格的字（"1"、"01"……）。
	Index string
	// Title / Artist 是歌名和歌手；Album 是专辑那一列（空 = 画「本地文件」那类调用方给的字）。
	Title, Artist, Album string
	// Owner 是 UP 主名；空 = 画一个淡色的「—」。
	Owner string
	// Quality 是音质角标："HI-RES" 画主色那一版、"SQ" 画普通那一版、空不画。
	Quality string
	// Plays / Danmaku 是播放量和弹幕数（"12.3万"）；Plays 为空那一格画「—」。
	Plays, Danmaku string
	// Duration 是时长（"4:29"）。
	Duration string
	// Cover 画 40 的封面；nil 画 surface-2 占位。Compact 时不画封面。
	Cover Cover
	// Playing 是正在放这一首：歌名 accent-text、序号换成跳动的 eq-bars。
	// Paused 时 eq-bars 停住（原型 `.paused .eq-bars i { animation-play-state: paused }`）。
	Playing, Paused bool
	// Liked 是喜欢过：心常亮、填满、accent-text。CanLike 为假就不画心（本地文件）。
	Liked, CanLike bool
	// Selected 是批量选择里选中了：整行 accent-soft 底。
	Selected bool
	// Removable 是行尾多一枚「移除」（稍后再看、历史记录）。
	Removable bool
	// Compact 是紧凑档：行高 44、不画封面（原型 `.tlist.compact`）。
	Compact bool
	// AlbumCol 是「专辑」那一列画不画（默认按列表宽度自动判，见 `AlbumMode`）。
	AlbumCol AlbumMode
}

// TrackEvents 是这一帧在这一行上发生的事。
type TrackEvents struct {
	Play, Like, More, Owner, Remove bool
	// Hovered 是指针在这一行上（调用方要做右键菜单、拖动时可用）。
	Hovered bool
}

// TrackRow 画一行曲目（原型 `.trow`，`app.js` 的 `trackRows`）。key 必须稳定且唯一
// （铁律四：用曲目 ID，不用序号）。w 是列表宽。
//
// 悬停：整行 hover 底；序号换成播放键；心（没喜欢时）、移除、「⋯」这几枚平时隐藏、
// 悬停才出现。正在播放且没悬停时序号位置是 eq-bars。
func TrackRow(c *ui.Context, key string, t Track, w float32) (ev TrackEvents) {
	p := pal(c)
	main, album, owner := trackColsMode(w, t.AlbumCol)
	h := tok.RowH
	if t.Compact {
		h = tok.RowCompactH
	}
	row := ui.Row(c).Key(key).Width(w).Height(h).Radius(tok.R8).PaddingX(space.Px12).
		Gap(space.Px16).AlignItems(ui.Center)
	hot := row.Hovered()
	ev.Hovered = hot
	switch {
	case t.Selected:
		row = row.Background(p.AccentSoft)
	case hot:
		row = row.Background(p.Hover)
	}
	row = row.Transition(motion.Fade)
	row.Children(func() {
		// 序号 / 播放键 / eq-bars。
		ui.Box(c).Size(colIdx, 32).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
			switch {
			case hot:
				b := ui.ButtonBase(c).Key(key+":play").Label("播放 "+t.Title).Size(32, 32).
					Radius(16).AlignItems(ui.Center).Justify(ui.Center)
				b.Children(func() { glyph(c, "play", tok.IconSm, p.Text) })
				ev.Play = b.Clicked()
			case t.Playing:
				EqBars(c, key+":eq", !t.Paused)
			default:
				typo.Mono(ui.Text(c, t.Index)).FontSize(scale.Font(c, scale.Size13)).
					TextColor(p.Text3).SingleLine()
			}
		})
		// 封面 + 歌名 / 角标 + 歌手。
		ui.Row(c).Width(main).Gap(space.Px12).AlignItems(ui.Center).Children(func() {
			if !t.Compact {
				ui.Box(c).Size(tok.CoverRow, tok.CoverRow).Radius(tok.R4).Background(p.Surface2).
					Clip().Shrink(0).Children(func() {
					if t.Cover != nil {
						t.Cover(c, tok.CoverRow, tok.CoverRow)
					}
				})
			}
			ui.Column(c).Grow(1).Shrink(1).Children(func() {
				name := p.Text
				if t.Playing {
					name = p.AccentText
				}
				text(c, t.Title, scale.Size14, name).FontWeight(500).SingleLine().Ellipsis("…")
				ui.Row(c).Gap(space.Px6).AlignItems(ui.Center).Children(func() {
					if t.Quality != "" {
						Badge(c, t.Quality, t.Quality == "HI-RES")
					}
					if !t.Compact || t.Quality == "" {
						text(c, t.Artist, scale.Size12, p.Text2).SingleLine().Ellipsis("…").Shrink(1)
					}
				})
			})
		})
		if album > 0 {
			text(c, t.Album, scale.Size13, p.Text2).Width(album).SingleLine().Ellipsis("…")
		}
		// UP 主：一枚链接（悬停正文色）。
		ui.Column(c).Width(owner).AlignItems(ui.Start).Children(func() {
			if t.Owner == "" {
				text(c, "—", scale.Size13, p.Text3)
				return
			}
			b := ui.ButtonBase(c).Key(key + ":owner")
			fg := p.Text2
			if b.Hovered() {
				fg = p.Text
			}
			b.Children(func() { text(c, t.Owner, scale.Size13, fg).SingleLine().Ellipsis("…") })
			ev.Owner = b.Clicked()
		})
		// 播放 / 弹幕。
		ui.Column(c).Width(colPlays).AlignItems(ui.End).Children(func() {
			if t.Plays == "" {
				text(c, "—", scale.Size12, p.Text3)
				return
			}
			typo.Mono(ui.Text(c, t.Plays)).FontSize(scale.Font(c, scale.Size12)).TextColor(p.Text2).SingleLine()
			if t.Danmaku != "" {
				typo.Mono(ui.Text(c, "弹幕 "+t.Danmaku)).FontSize(scale.Font(c, scale.Size11)).
					TextColor(p.Text3).SingleLine()
			}
		})
		typo.Mono(ui.Text(c, t.Duration)).FontSize(scale.Font(c, scale.Size13)).TextColor(p.Text3).
			Width(colDur).TextAlign(ui.End).SingleLine()
		// 操作。
		ui.Row(c).Width(colAct).Gap(space.Px2).Justify(ui.End).AlignItems(ui.Center).Children(func() {
			if t.Removable && hot {
				ev.Remove = IconBtn(c, key+":remove", "x", "移除 "+t.Title, IconOpts{Row: true}).Clicked()
			}
			if t.CanLike && (t.Liked || hot) {
				ic, label := "heart", "喜欢 "+t.Title
				if t.Liked {
					ic, label = "heart-on", "取消喜欢 "+t.Title
				}
				ev.Like = IconBtn(c, key+":like", ic, label, IconOpts{Row: true, On: t.Liked}).Clicked()
			}
			if hot {
				ev.More = IconBtn(c, key+":more", "more", "更多操作："+t.Title, IconOpts{Row: true}).Clicked()
			}
		})
	})
	return ev
}

// eqPeriod 是 eq-bars 一个来回（原型 `eqb 900ms … alternate`：去 900 回 900）。
const eqPeriod = 1800 * time.Millisecond

// EqBars 是「正在播放」那三根跳动的柱（原型 `.eq-bars`）：宽 3、缝 2、高 14、主色、
// 圆角 1，高度在 25%~100% 之间来回，三根错开 0 / -300 / -600ms。moving 为假
// （暂停）或系统开了「减少动态效果」时停在 40%。
func EqBars(c *ui.Context, key string, moving bool) ui.Element {
	p := pal(c)
	still := !moving || motion.Reduced(c)
	box := ui.Box(c).Key(key).Size(3*3+2*2, 14).Shrink(0)
	var phase float32
	if !still {
		phase = box.Loop("eq", eqPeriod, ui.Linear)
	}
	box.Draw(func(pp *ui.Painter, r ui.Rect) {
		for i := 0; i < 3; i++ {
			frac := float32(0.4)
			if !still {
				// 三角波：0→1→0 一个周期，每根错开 300ms（= 1/6 周期）。
				ph := phase + float32(i)/6
				ph -= float32(int(ph))
				tri := 1 - abs32(2*ph-1)
				// 原型是 ease-in-out：这里用平滑插值近似。
				tri = tri * tri * (3 - 2*tri)
				frac = 0.25 + 0.75*tri
			}
			bh := r.H * frac
			pp.Fill(ui.Rect{X: r.X + float32(i)*5, Y: r.Y + r.H - bh, W: 3, H: bh}, p.Accent, 1)
		}
	})
	return box
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
