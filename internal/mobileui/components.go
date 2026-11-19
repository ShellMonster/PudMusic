package mobileui

import (
	"runtime"
	"strconv"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/iconset"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/netimage"
)

type palette struct {
	surface, fill, field, text, muted, accent, soft, line ui.Color
}

func phonePalette(c *ui.Context) palette {
	tokens := tok.Of(c.Theme())
	return palette{tokens.Surface, tokens.Bg, tokens.Surface2, tokens.Text, tokens.Text2, tokens.AccentText, tokens.AccentSoft, tokens.Line}
}

func touchSize() float32 {
	if runtime.GOOS == "android" {
		return 48
	}
	return 44
}

func glyph(c *ui.Context, name string, size float32, color ui.Color) ui.Element {
	return ui.Icon(c, iconset.Must("Pud/"+name+".svg")).Size(size, size).TextColor(color).Shrink(0)
}

func iconButton(c *ui.Context, name, label string) ui.Element {
	return ui.Box(c).Role(ui.RoleButton).Label(label).Size(touchSize(), touchSize()).Shrink(0).Radius(touchSize() / 2).Center().Children(func() {
		glyph(c, name, 18, c.Theme().Text)
	})
}

func brand(c *ui.Context) {
	ui.Box(c).Size(24, 24).Radius(12).Background(ui.Hex("#17181b")).Center().Children(func() {
		glyph(c, "disc", 19, ui.Hex("#f26449"))
	})
}

func cover(c *ui.Context, url string, size, radius float32) ui.Element {
	if url == "" {
		palette := phonePalette(c)
		return ui.Box(c).Size(size, size).Shrink(0).Radius(radius).Background(palette.field).Center().Children(func() {
			glyph(c, "disc", min(size*.46, 44), palette.muted)
		})
	}
	return netimage.Image(c, imgcache.FormatURLProtocol(url), netimage.Options{MaxSide: int(size * 3)}).Size(size, size).Radius(radius).Shrink(0)
}

func sectionHeading(c *ui.Context, title, action string) bool {
	clicked := false
	ui.Row(c).AlignItems(ui.Center).MinHeight(32).FillWidth().Children(func() {
		ui.Text(c, title).FontSize(16).FontWeight(600).Grow(1).Basis(0)
		if action != "" {
			clicked = ui.Row(c).Role(ui.RoleButton).Label(action).MinHeight(touchSize()).Gap(4).AlignItems(ui.Center).Children(func() {
				color := phonePalette(c).muted
				ui.Text(c, action).FontSize(13).TextColor(color)
				glyph(c, "right", 12, color)
			}).Clicked()
		}
	})
	return clicked
}

func statusArtwork(c *ui.Context, icon string) {
	palette := phonePalette(c)
	ui.Box(c).Size(104, 92).Draw(func(p *ui.Painter, r ui.Rect) {
		ink := palette.muted
		p.Stroke(ui.Rect{X: r.X + 35, Y: r.Y + 13, W: 56, H: 56}, ink, 28, 1.3)
		p.Stroke(ui.Rect{X: r.X + 43, Y: r.Y + 21, W: 40, H: 40}, palette.line, 20, 1.2)
		cover := ui.Rect{X: r.X + 6, Y: r.Y + 12, W: 58, H: 64}
		p.Fill(cover, palette.fill, 3)
		p.Stroke(cover, ink, 3, 1.3)
		p.Line(r.X+15, r.Y+58, r.X+35, r.Y+58, 1.3, ink)
		p.Line(r.X+15, r.Y+64, r.X+45, r.Y+64, 1.3, ink)
	}).Children(func() {
		ui.Row(c).FillWidth().Justify(ui.End).Padding(12, 5, 0, 0).Children(func() {
			ui.Box(c).Size(28, 28).Radius(14).Background(palette.surface).Border(1, palette.muted).Center().Children(func() {
				if icon == "lock" {
					glyph(c, icon, 15, palette.muted)
				} else {
					networkUnavailableIcon(c, palette.muted)
				}
			})
		})
	})
}

func networkUnavailableIcon(c *ui.Context, ink ui.Color) {
	ui.Box(c).Size(18, 18).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		x, y := r.X, r.Y
		path := (&ui.Path{}).MoveTo(x+2, y+6).QuadTo(x+9, y, x+16, y+6).
			MoveTo(x+4, y+9).QuadTo(x+9, y+5, x+14, y+9).
			MoveTo(x+7, y+12).QuadTo(x+9, y+10, x+11, y+12)
		p.StrokePath(path, 1.2, ink)
		p.Fill(ui.Rect{X: x + 8, Y: y + 14, W: 2, H: 2}, ink, 1)
		p.Line(x+3, y+3, x+15, y+15, 1.4, ink)
	})
}

func (s *shell) trackList(c *ui.Context, items []app.Item) {
	coverSize, height := float32(48), float32(58)
	if s.current().kind == pageCollection {
		coverSize, height = 38, 54
	}
	textSize := interfaceTextSize(s.app.SettingsSnapshot().FontScale)
	occurrences := make(map[string]int, len(items))
	active, activeOK := s.app.Queue().Current()
	for index, item := range items {
		id := item.Identity()
		occurrences[id]++
		key := c.Key(id + ":" + strconv.Itoa(occurrences[id]))
		s.trackRow(key, items, index, coverSize, height, textSize, activeOK && active.Identity() == id)
	}
}

func (s *shell) trackRow(c *ui.Context, items []app.Item, index int, coverSize, height, textSize float32, active bool) {
	item := items[index]
	ui.Row(c).FillWidth().MinHeight(height).Gap(4).AlignItems(ui.Center).Children(func() {
		first := ui.Row(c).Grow(1).Basis(0).MinHeight(touchSize()).Gap(12).AlignItems(ui.Center).Role(ui.RoleButton).Label(item.Title).Disabled(!app.CanQueue(item)).Children(func() {
			cover(c, item.Cover, coverSize, 7)
			trackLabel(c, item.Title, item.Artist, textSize, active)
		})
		first.OnClick(func() { s.play(items, index) })
		more := iconButton(c, "more", "更多操作："+item.Title)
		if first.LongPressed() || more.Clicked() {
			s.player.target = item
			s.player.show(sheetTrack)
		}
	})
}

func interfaceTextSize(scale app.FontScale) float32 {
	switch scale {
	case app.FontScaleSmall:
		return 12
	case app.FontScaleLarge:
		return 17
	default:
		return 13
	}
}

func trackLabel(c *ui.Context, title, subtitle string, size float32, current bool) {
	palette := phonePalette(c)
	ink := palette.text
	if current {
		ink = palette.accent
	}
	ui.Column(c).Grow(1).Basis(0).Gap(2).Children(func() {
		ui.Text(c, title).FontSize(size).FontWeight(500).MaxLines(1).Ellipsis("…").TextColor(ink)
		if subtitle != "" {
			ui.Text(c, subtitle).FontSize(max(size-1, 12)).MaxLines(1).Ellipsis("…").TextColor(palette.muted)
		}
	})
}

func (s *shell) play(items []app.Item, index int) {
	if len(items) == 0 {
		return
	}
	s.report(s.app.PlayFromIndex(items, index))
}

func (s *shell) report(err error) {
	if err != nil {
		s.notice = err.Error()
	}
}

func timeLabel(value time.Duration) string {
	seconds := int(max(value, 0) / time.Second)
	minutes := strconv.Itoa(seconds / 60)
	rest := strconv.Itoa(seconds % 60)
	if len(rest) == 1 {
		rest = "0" + rest
	}
	return minutes + ":" + rest
}

func completedSwipe(element ui.Element) (float32, float32, bool) {
	touches := element.Touches()
	if len(touches) != 1 || touches[0].Phase != ui.TouchEnded {
		return 0, 0, false
	}
	touch := touches[0]
	return touch.X - touch.StartX, touch.Y - touch.StartY, true
}
