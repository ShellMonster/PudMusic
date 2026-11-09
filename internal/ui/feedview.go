// 用户动态页的外观部件（「深夜唱片行」，原型 `PAGES.feed` / CSS `.filter-rail` `.chip`，
// DESIGN.md §0′.T3）：顶上按 UP 主筛选的 chip 横排（chip 里带头像）、三种尾部状态、
// 未登录页。取数、翻页、卡片在 `dynamicfeed.go` / `dynamiccard.go`。
package ui

import (
	"context"
	"strconv"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

func (s *Shell) viewFeedHeader(c *ui.Context, f *dynamicFeed) {
	w := windowW(c)
	pud.PageHead(c, "Feed", "用户动态", "关注的 UP 主最近分享了什么。", w, nil)
	s.viewFeedFilter(c, f)
}

// feedChipLimit 是 chip 横排最多画几位 UP 主：横排会自动一页页往后拉，但不无限拉。
const feedChipLimit = 40

const feedAuthorChipMaxW = 240

// visible 是这一帧要画的动态：筛了某位 UP 主就只留他的。
func (f *dynamicFeed) visible() []app.Dynamic {
	if f.filterMid == 0 {
		return f.items
	}
	out := make([]app.Dynamic, 0, len(f.items))
	for _, d := range f.items {
		if d.Author.Mid == f.filterMid {
			out = append(out, d)
		}
	}
	return out
}

// viewFeedFilter 是页头下面那排 chip（原型 `.filter-rail`：离页头 24，缝 8，放不下换行）：
// 「全部」加每位关注的 UP 主（chip 左内边距 4，带 22 的头像）；选中的反色。
// 数据是「我关注的人」（作者那一拉），没拉到时只有「全部」。
func (s *Shell) viewFeedFilter(c *ui.Context, f *dynamicFeed) {
	ui.Column(c).Key("feed:filter").FillWidth().
		Margin(space.Px24, space.None, space.None, space.None).Gap(space.Px8).Children(func() {
		authors := f.authors
		if len(authors) > feedChipLimit {
			authors = authors[:feedChipLimit]
		}
		if !f.authorsExpanded && len(authors) > 12 {
			s.viewFeedAuthorRow(c, f, authors[:6], true)
			remaining := len(authors) - 12
			ui.Row(c).Key("feed:authors-row:second").FillWidth().Wrap().Gap(space.Px8).AlignItems(ui.Center).
				Children(func() {
					for _, a := range authors[6:12] {
						if s.feedAuthorChip(c, a, f.filterMid == a.Mid).Clicked() {
							f.filterMid = a.Mid
						}
					}
					if pud.Chip(c, "feed:authors-expand", "+"+strconv.Itoa(remaining), "", false).Clicked() {
						f.authorsExpanded = true
					}
				})
			return
		}
		s.viewFeedAuthorRow(c, f, authors, true)
		if f.authorsExpanded && len(authors) > 12 {
			ui.Row(c).Key("feed:authors-collapse").FillWidth().Justify(ui.End).Children(func() {
				if pud.Chip(c, "feed:authors-collapse", "收起", "", false).Clicked() {
					f.authorsExpanded = false
				}
			})
		}
	})
}

func (s *Shell) viewFeedAuthorRow(c *ui.Context, f *dynamicFeed, authors []app.FollowAuthor, includeAll bool) {
	ui.Row(c).Key("feed:authors-row:" + strconv.FormatBool(includeAll)).FillWidth().Wrap().Gap(space.Px8).AlignItems(ui.Center).
		Children(func() {
			if includeAll {
				if pud.Chip(c, "feed:chip:all", "全部", "", f.filterMid == 0).Clicked() {
					f.filterMid = 0
				}
			}
			for _, a := range authors {
				if s.feedAuthorChip(c, a, f.filterMid == a.Mid).Clicked() {
					f.filterMid = a.Mid
				}
			}
		})
}

// feedAuthorChip 是带头像的 chip（原型 `style="padding-left:4px"` + `avatar(k, 22)`）。
func (s *Shell) feedAuthorChip(c *ui.Context, a app.FollowAuthor, on bool) ui.Element {
	pl := tok.Of(c.Theme())
	name := a.Name
	if name == "" {
		name = "这个 UP 主"
	}
	b := ui.ButtonBase(c).Key("feed:chip:"+strconv.FormatInt(a.Mid, 10)).Label(name).Height(tok.ChipH).
		Padding(space.None, space.Px12, space.None, space.Px4).Radius(tok.ChipH / 2).Gap(space.Px6).
		AlignItems(ui.Center).Shrink(0).MaxWidth(feedAuthorChipMaxW)
	bg, fg := pl.Surface2, pl.Text2
	switch {
	case on:
		bg, fg = pl.Text, pl.Bg
	case b.Hovered():
		bg, fg = pl.Press, pl.Text
	}
	b = b.Background(bg).Transition(motion.Fade)
	b.Children(func() {
		pud.Avatar(c, name, 22, faceTint(a.Mid), s.upFaceDraw(a.Face))
		ui.Text(c, name).FontSize(scale.Font(c, scale.Size13)).TextColor(fg).
			SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
	})
	return b.Tooltip(name)
}

// feedEndNote 是「没有更多动态了」（原型：居中、13 号 text-3、上下 24）。
func feedEndNote(c *ui.Context) {
	ui.Row(c).Key("feed:end").FillWidth().Justify(ui.Center).Padding(space.Px24, space.None).Children(func() {
		ui.Text(c, "没有更多动态了").FontSize(scale.Font(c, scale.Size13)).TextColor(tok.Of(c.Theme()).Text3)
	})
}

// viewLoggedOutPage 是需要登录的页面的未登录样子（原型 `loginPrompt`）：页头 + 虚线框的
// 「登录后查看…」。back 是登录完回哪一页（`goLogin` 记下它，票 #277）。
func (s *Shell) viewLoggedOutPage(c *ui.Context, key, eyebrow, title, lede, what, back string) {
	w := windowW(c)
	// 窗口矮的时候「扫码登录」会落到播放条下面：页面要能滚（原型的 `.page` 就是滚动容器）。
	scroll := ui.Scroll(c).Key(key).Grow(1).FillHeight().Children(func() {
		ui.Column(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Column(c).Width(s.pageBodyW(c)).
				Padding(space.PageTop, space.PageHorizontalForWindow(w), space.PageBottomForWindow(w), space.PageHorizontalForWindow(w)).Children(func() {
				pud.PageHead(c, eyebrow, title, lede, w, nil)
				if pud.LoginPrompt(c, key+":login", what) {
					s.goLogin(back)
				}
			})
		})
	})
	s.trackScrollbar(scroll)
}

// feedMoreAuthors 在作者那一拉有下一页时自动接着拉：chip 横排没有滚动可以「滚到底」，
// 所以一页页拉到够用为止（上限 `feedChipLimit`）。在界面线程调，每帧至多发一拉。
func (s *Shell) feedMoreAuthors(f *dynamicFeed) {
	auth := &f.auth
	if !auth.started || !auth.hasMore || auth.loading || auth.err != nil || len(f.authors) >= feedChipLimit {
		return
	}
	if ctx := auth.begin(); ctx != nil {
		go s.fetchFeed(ctx, f, feedAuthors, auth.cursor, auth.page)
	}
}

var _ = context.Background
