package ui

import (
	"fmt"
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
		// 榜单还在路上：骨架占住这一块的高度。不占的话主题曲目回来时下面整片往下跳
		// （每日推荐会挪两百多像素），正要点的那张卡会换掉。
		if h.waiting(homeChart) {
			h.section(c, func() {
				pud.SecHead(c, "home:artists", "热门歌手", "来自本周热榜", "")
				h.skeletonArtists(c)
			})
		}
		return
	}
	itemWidth, avatarSize := float32(126), float32(108)
	if h.win <= 760 {
		itemWidth, avatarSize = 100, 84
	}
	h.section(c, func() {
		pud.SecHead(c, "home:artists", "热门歌手", "来自本周热榜", "")
		ui.ScrollHorizontal(c).Key("home:artists:rail").FillWidth().Height(avatarSize+38).
			Gap(space.Px24).Padding(space.None, space.None, space.Px12, space.None).
			ScrollbarStyle(ui.ScrollbarStyle{TrackWidth: 6, ThumbWidth: 6, MinLength: 24}).Children(func() {
			for _, name := range artists {
				name := name
				button := ui.ButtonBase(c).Key("home:artist:" + name).Label("搜索 " + name).Width(itemWidth).Shrink(0).AlignItems(ui.Center).Radius(tok.R12)
				if button.Hovered() {
					button = button.Background(tok.Of(c.Theme()).Hover)
				}
				if button.Children(func() {
					ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(space.Px8).Children(func() {
						pud.Avatar(c, name, avatarSize, ui.RGB(75, 79, 84), nil)
						ui.Text(c, name).FontSize(scale.Font(c, scale.Size15)).TextColor(tok.Of(c.Theme()).Text).SingleLine().Ellipsis("…")
					})
				}).Clicked() {
					s.submitSearch(name)
				}
			}
		})
	})
}

// skeletonArtists 是「热门歌手」那一排还在等榜单时的样子：宽度、头像、缝和真的一模一样，
// 所以榜单回来时这一块只是换内容，高度不变。
func (h *homePage) skeletonArtists(c *ui.Context) {
	itemWidth, avatarSize := float32(126), float32(108)
	if h.win <= 760 {
		itemWidth, avatarSize = 100, 84
	}
	ui.ScrollHorizontal(c).Key("home:artists:sk:rail").FillWidth().Height(avatarSize+38).
		Gap(space.Px24).Padding(space.None, space.None, space.Px12, space.None).
		ScrollbarStyle(ui.ScrollbarStyle{TrackWidth: 6, ThumbWidth: 6, MinLength: 24}).Children(func() {
		// 6 是这一排的上限（`hotArtists` 取到 6 就停）。
		for i := 0; i < 6; i++ {
			ui.Column(c).Key(fmt.Sprintf("home:artist:sk:%d", i)).Width(itemWidth).Shrink(0).
				AlignItems(ui.Center).Gap(space.Px8).Children(func() {
				pud.Skeleton(c, fmt.Sprintf("home:artist:sk:%d:face", i), avatarSize, avatarSize, avatarSize/2)
				pud.Skeleton(c, fmt.Sprintf("home:artist:sk:%d:name", i), itemWidth*0.6, 15, tok.R4)
			})
		}
	})
}
