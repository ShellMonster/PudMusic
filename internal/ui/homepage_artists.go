package ui

import (
	"strings"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

func (h *homePage) hotArtists() []string {
	if h.artistReady && h.artistRevision == h.feed.revision {
		return h.artists
	}
	h.artistReady, h.artistRevision = true, h.feed.revision
	h.artists = nil
	seen := make(map[string]bool)
	for _, song := range h.feed.sec[homeChart].songs {
		name := strings.TrimSpace(song.Owner)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		h.artists = append(h.artists, name)
		if len(h.artists) == 6 {
			break
		}
	}
	return h.artists
}

func (h *homePage) viewHotArtists(c *ui.Context, s *Shell) {
	artists := h.hotArtists()
	if len(artists) == 0 {
		return
	}
	h.section(c, func() {
		pud.SecHead(c, "home:artists", "热门歌手", "来自本周热榜", "")
		ui.ScrollHorizontal(c).Key("home:artists:rail").FillWidth().Gap(space.Px24).Children(func() {
			for _, name := range artists {
				name := name
				button := ui.ButtonBase(c).Key("home:artist:" + name).Label("搜索 " + name).Width(126).Shrink(0).AlignItems(ui.Center).Radius(tok.R12)
				if button.Hovered() {
					button = button.Background(tok.Of(c.Theme()).Hover)
				}
				if button.Children(func() {
					ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(space.Px8).Children(func() {
						pud.Avatar(c, name, 108, ui.RGB(75, 79, 84), nil)
						ui.Text(c, name).FontSize(scale.Font(c, scale.Size15)).TextColor(tok.Of(c.Theme()).Text).SingleLine().Ellipsis("…")
					})
				}).Clicked() {
					s.submitSearch(name)
				}
			}
		})
	})
}
