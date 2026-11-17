package ui

import (
	"context"
	"strings"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

type creatorResult struct {
	items []app.SearchItem
	err   error
}

type creatorPreview struct {
	keyword string
	cancel  context.CancelFunc
	result  <-chan creatorResult
	items   []app.SearchItem
	err     error
	fetch   func(context.Context, string) ([]app.SearchItem, error)
}

func (p *creatorPreview) stop() {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.result = nil
}

func (s *Shell) syncCreatorPreview(keyword string) {
	p := &s.search.creators
	keyword = strings.TrimSpace(keyword)
	if p.keyword != keyword {
		p.stop()
		p.keyword, p.items, p.err = keyword, nil, nil
		if keyword != "" && !s.closed {
			ctx, cancel := context.WithCancel(context.Background())
			p.cancel = cancel
			ch := make(chan creatorResult, 1)
			p.result = ch
			fetch := p.fetch
			if fetch == nil {
				fetch = func(ctx context.Context, keyword string) ([]app.SearchItem, error) {
					page, err := s.app.SearchOf().Page(ctx, app.SearchQuery{Tab: app.SearchTabUser, Keyword: keyword, Page: 1})
					return page.Items, err
				}
			}
			go func() {
				items, err := fetch(ctx, keyword)
				ch <- creatorResult{items: items, err: err}
				s.wake.invalidate()
			}()
		}
	}
	if p.result != nil {
		select {
		case result := <-p.result:
			p.items = uniqueCreators(result.items)
			p.err = result.err
			p.result = nil
			if p.cancel != nil {
				p.cancel()
			}
			p.cancel = nil
		default:
		}
	}
}

func uniqueCreators(items []app.SearchItem) []app.SearchItem {
	seen := make(map[string]bool, len(items))
	out := make([]app.SearchItem, 0, min(len(items), 8))
	for _, item := range items {
		if item.Item.Type != app.ItemTypeUser || item.Item.Bvid == "" || seen[item.Item.Bvid] {
			continue
		}
		seen[item.Item.Bvid] = true
		out = append(out, item)
		if len(out) == 8 {
			break
		}
	}
	return out
}

func (s *Shell) viewSearchCreators(c *ui.Context, keyword string, winW float32) bool {
	p := &s.search.creators
	if p.keyword != keyword || len(p.items) == 0 {
		return false
	}
	ui.Column(c).FillWidth().Children(func() {
		ty := tok.Of(c.Theme())
		typo.Eyebrow(c, "搜索「"+keyword+"」").SingleLine().Ellipsis("…")
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			typo.Display(ui.Text(c, "相关 UP 主"), scale.Font(c, scale.Fluid(26, 2.6, 36, winW))).TextColor(ty.Text).Grow(1)
			if ui.ButtonBase(c).Label("查看全部 UP 主").Padding(space.Px8).Children(func() {
				ui.Text(c, "查看全部 ›").TextColor(ty.Text2)
			}).Clicked() {
				s.search.tab = searchTabUser
			}
		})
		ui.ScrollHorizontal(c).Key("search:creators").FillWidth().Gap(space.Px16).Margin(space.Px16, space.None, space.None, space.None).Children(func() {
			for _, item := range p.items {
				item := item
				button := ui.ButtonBase(c).Key("search:creator:" + item.Item.Bvid).Label("打开 " + item.Title + " 的空间").Width(116).Shrink(0).
					AlignItems(ui.Center).Radius(tok.R8)
				if button.Hovered() {
					button = button.Background(ty.Hover)
				}
				if button.Children(func() {
					ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(space.Px8).Children(func() {
						ui.Box(c).Size(72, 72).Children(func() {
							var cover pud.Cover
							if item.Cover != "" {
								cover = func(c *ui.Context, w, h, radius float32) {
									imgcache.ViewFlat(c, s.pageImages(), item.Cover, w, h, radius)
								}
							}
							pud.Avatar(c, item.Title, 72, ui.Color{}, cover)
							if item.Verification == bili.VerificationPersonal || item.Verification == bili.VerificationOrganization {
								color := ui.RGB(229, 163, 39)
								label := "个人认证"
								if item.Verification == bili.VerificationOrganization {
									color, label = ui.RGB(48, 142, 204), "机构认证"
								}
								ui.Box(c).Absolute().Right(0).Bottom(0).Size(21, 21).Radius(10.5).Background(color).Border(2, ty.Bg).Center().Label(label).Children(func() { ui.Text(c, "V").FontSize(11).TextColor(ui.RGB(255, 255, 255)) })
							}
						})
						ui.Text(c, item.Title).FontSize(scale.Font(c, scale.Size13)).TextColor(ty.Text).SingleLine().Ellipsis("…")
					})
				}).Clicked() {
					s.openSearchUser([]app.Item{item.Item})
				}
			}
		})
	})
	return true
}
