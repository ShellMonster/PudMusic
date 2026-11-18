package ui

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/ShellMonster/PudMusic/internal/app"
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

// viewSearchCreators 画搜索页头那一排相关 UP 主，返回 true 表示这一帧占了页头。
//
// 预览还在路上时画页头 + 骨架：结果回来时页头几何不变（同一支
// `pud.PageHeadFnEyebrow`），只有骨架换成真人，下面的 Tab 不会往下跳。没有匹配用户
// （空结果 / 失败）时返回 false，由调用方画普通的「搜索结果」页头。
func (s *Shell) viewSearchCreators(c *ui.Context, keyword string, winW float32) bool {
	p := &s.search.creators
	if p.keyword != keyword || (len(p.items) == 0 && p.result == nil) {
		return false
	}
	ty := tok.Of(c.Theme())
	pud.PageHeadFnEyebrow(c, func() {
		// 关键词原样画：`typo.Eyebrow` 会转全大写，搜索词的大小写是有意义的。
		typo.EyebrowText(c, "搜索「"+keyword+"」").SingleLine().Ellipsis("…")
	}, "相关 UP 主", winW, nil, func() {
		if ui.ButtonBase(c).Label("查看全部 UP 主").Padding(space.Px8).Children(func() {
			ui.Text(c, "查看全部 ›").TextColor(ty.Text2)
		}).Clicked() {
			s.search.tab = searchTabUser
		}
	})
	if len(p.items) == 0 {
		s.viewCreatorSkeleton(c)
		return true
	}
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
						// 中性底色：传空色时 `pud.Avatar` 会兜底成主题强调色，头像边缘
						// 会透出一圈主的红 / 橙。
						pud.Avatar(c, item.Title, 72, ui.RGB(75, 79, 84), cover)
						if item.Verification == app.SearchVerificationPersonal || item.Verification == app.SearchVerificationOrganization {
							color := ui.RGB(229, 163, 39)
							label := "个人认证"
							if item.Verification == app.SearchVerificationOrganization {
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
	return true
}

// viewCreatorSkeleton 是相关 UP 主那一排在预览路上的样子：几何和真的一模一样
// （头像 72、项宽 116、缝 16、上边距 16），所以结果回来时下面的 Tab 不动。
func (s *Shell) viewCreatorSkeleton(c *ui.Context) {
	ui.ScrollHorizontal(c).Key("search:creators:sk").FillWidth().Gap(space.Px16).Margin(space.Px16, space.None, space.None, space.None).Children(func() {
		for i := 0; i < 8; i++ {
			ui.Column(c).Key(fmt.Sprintf("search:creator:sk:%d", i)).Width(116).Shrink(0).
				AlignItems(ui.Center).Gap(space.Px8).Children(func() {
				pud.Skeleton(c, fmt.Sprintf("search:creator:sk:%d:face", i), 72, 72, 36)
				pud.Skeleton(c, fmt.Sprintf("search:creator:sk:%d:name", i), 64, 13, tok.R4)
			})
		}
	})
}
