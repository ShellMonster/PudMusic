package ui

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/imgcache"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
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

	// pageMu 护着下面两样：预览拿到的那一整页。
	//
	// 界面线程收结果时写，后台拉「用户」标签那一趟读（`takePage`）——预览问的和
	// 「用户」标签第 1 页是**同一个请求**，点「查看全部」时再发一遍就是白翻一倍。
	// B 站对搜索有 -412 / -509，翻倍会直接变成「搜索太频繁」。
	pageMu      sync.Mutex
	page        app.SearchPage
	pageKeyword string
	pageValid   bool
}

// verificationBadgeRadius keeps the certification badge circular while matching
// its 21px diameter.
const verificationBadgeRadius = 10.5

// storePage 记下预览那一次请求的整页（`uniqueCreators` 截断之前的那一份）。
func (p *creatorPreview) storePage(keyword string, page app.SearchPage) {
	p.pageMu.Lock()
	p.page, p.pageKeyword, p.pageValid = page, keyword, true
	p.pageMu.Unlock()
}

// takePage 取走预览那一次请求的整页：只给**同一个词的第 1 页**，取一次就清掉。
func (p *creatorPreview) takePage(keyword string) (app.SearchPage, bool) {
	p.pageMu.Lock()
	defer p.pageMu.Unlock()
	page, ok := p.page, p.pageValid && p.pageKeyword == keyword
	p.page, p.pageKeyword, p.pageValid = app.SearchPage{}, "", false
	if !ok {
		return app.SearchPage{}, false
	}
	return page, true
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
			query := app.SearchQuery{Tab: app.SearchTabUser, Keyword: keyword, Page: 1}
			go func() {
				var (
					items []app.SearchItem
					page  app.SearchPage
					err   error
				)
				if fetch != nil {
					items, err = fetch(ctx, keyword)
				} else {
					page, err = s.app.SearchOf().Page(ctx, query)
					items = page.Items
				}
				if ctx.Err() != nil {
					return
				}
				if err == nil {
					p.storePage(keyword, app.SearchPage{Items: items, HasMore: page.HasMore, Total: page.Total})
				}
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

// creatorRetryAvailable 说「相关 UP 主加载失败」要不要给重试键。
//
// 和结果区同一套判据（`viewSearchResultsBody` 里那句 `Retryable`）：风控、太频繁、
// 没登录这一类再按一次还是同一个结果，给键只会让人以为点坏了。
func creatorRetryAvailable(err error) bool {
	if err == nil {
		return false
	}
	searchErr, ok := searchErrorOf(err)
	return !ok || searchErr.Retryable
}

func (s *Shell) searchCreatorLead(keyword string, tab int) *list.Lead {
	p := &s.search.creators
	pg := s.search.lists[tab].list.Paged()
	if p.keyword != keyword || (len(p.items) == 0 && p.result == nil) {
		return nil
	}
	if pg.Loaded == 0 && !pg.Loading {
		return nil
	}
	return &list.Lead{
		Label: "搜索结果页头",
		Height: func() float32 {
			if tab == searchTabUser {
				return 252
			}
			return 292
		},
		Draw: func(c *ui.Context) {
			s.viewSearchLead(c, keyword, tab)
		},
	}
}

func (s *Shell) viewSearchLead(c *ui.Context, keyword string, tab int) {
	ty := tok.Of(c.Theme())
	p := &s.search.creators

	ui.Row(c).FillWidth().AlignItems(ui.Center).
		Padding(space.Px24, space.None, space.Px8, space.None).Children(func() {
		typo.EyebrowText(c, "搜索「"+keyword+"」").SingleLine().Ellipsis("…").Grow(1)
		if ui.ButtonBase(c).Label("查看全部 UP 主").Padding(space.Px8).Children(func() {
			ui.Text(c, "查看全部 ›").TextColor(ty.Text2)
		}).Clicked() {
			s.search.tab = searchTabUser
		}
	})
	if len(p.items) == 0 {
		s.viewCreatorSkeleton(c)
	} else {
		s.viewCreatorRail(c, p.items)
	}

	ui.Column(c).FillWidth().Margin(space.Px12, space.None, space.None, space.None).Children(func() {
		tabs := make([]pud.Tab, searchTabCount)
		for i := range tabs {
			tabs[i] = pud.Tab{Label: searchTabLabels[i], Count: s.searchTabCountText(i)}
		}
		pud.Tabs(c, "search:tabs:"+keyword, &s.search.tab, tabs)
	})
	s.viewSearchToolbar(c, tab)

	t := &s.search.lists[tab]
	if t.list.Layout() != list.ViewCard && tab != searchTabUser && t.list.Paged().Loaded > 0 {
		ui.Box(c).Height(space.Px8)
		t.list.Header(c, "search:header:"+fmt.Sprint(tab))
	}
}

func (s *Shell) viewCreatorRail(c *ui.Context, items []app.SearchItem) {
	ty := tok.Of(c.Theme())
	ui.ScrollHorizontal(c).Key("search:creators").FillWidth().Height(112).
		Gap(space.Px16).Padding(space.None, space.None, space.Px16, space.None).
		Margin(space.Px16, space.None, space.None, space.None).
		ScrollbarStyle(horizontalRailScrollbarStyle()).Children(func() {
		for _, item := range items {
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
						// 中性底色：传空色时 `pud.Avatar` 会兜底成主题强调色，头像边缘
						// 会透出一圈主的红 / 橙。
						pud.Avatar(c, item.Title, 72, ui.RGB(75, 79, 84), cover)
						if item.Verification == app.SearchVerificationPersonal || item.Verification == app.SearchVerificationOrganization {
							color := ui.RGB(229, 163, 39)
							label := "个人认证"
							if item.Verification == app.SearchVerificationOrganization {
								color, label = ui.RGB(48, 142, 204), "机构认证"
							}
							ui.Box(c).Absolute().Right(0).Bottom(0).Size(21, 21).Radius(verificationBadgeRadius).Background(color).Border(2, ty.Bg).Center().Label(label).Children(func() { ui.Text(c, "V").FontSize(11).TextColor(ui.RGB(255, 255, 255)) })
						}
					})
					ui.Text(c, item.Title).FontSize(scale.Font(c, scale.Size13)).TextColor(ty.Text).SingleLine().Ellipsis("…")
				})
			}).Clicked() {
				s.openSearchUser([]app.Item{item.Item})
			}
		}
	})
}

// viewCreatorSkeleton 是相关 UP 主那一排在预览路上的样子：几何和真的一模一样
// （头像 72、项宽 116、缝 16、上边距 16），所以结果回来时下面的 Tab 不动。
func (s *Shell) viewCreatorSkeleton(c *ui.Context) {
	ui.ScrollHorizontal(c).Key("search:creators:sk").FillWidth().Height(112).
		Gap(space.Px16).Padding(space.None, space.None, space.Px16, space.None).
		Margin(space.Px16, space.None, space.None, space.None).
		ScrollbarStyle(horizontalRailScrollbarStyle()).Children(func() {
		for i := 0; i < 8; i++ {
			ui.Column(c).Key(fmt.Sprintf("search:creator:sk:%d", i)).Width(116).Shrink(0).
				AlignItems(ui.Center).Gap(space.Px8).Children(func() {
				pud.Skeleton(c, fmt.Sprintf("search:creator:sk:%d:face", i), 72, 72, 36)
				pud.Skeleton(c, fmt.Sprintf("search:creator:sk:%d:name", i), 64, 13, tok.R4)
			})
		}
	})
}
