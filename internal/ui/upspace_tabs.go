package ui

import (
	"fmt"
	"log"
	"runtime/debug"
	"strconv"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/egoist/mygo/ui"
)

// UP 空间那四个 Tab 的内容：投稿 / 动态 / 收藏夹 / 合集。
//
// 头图和状态在 upspace.go，卡片在 dynamiccard.go，取数在 upspace_fetch.go。
// 这里只管「哪一张 Tab、这一张画什么」。

// 点击、左右方向键、Home/End 全是框架给的，我们不去重写。
func (s *Shell) viewSpaceTabs(c *ui.Context, sp *upSpace) {
	ui.Tabs(c, &sp.tab, upTabNames...).Key("space-tabs").PaddingX(upPad)
}

// viewSpaceTab 画当前那一张 Tab 的内容。
func (s *Shell) viewSpaceTab(c *ui.Context, sp *upSpace) {
	if sp.tab == upTabVideo {
		s.viewSpaceVideos(c, sp)
		return
	}
	s.viewSpaceList(c, sp, sp.tab)
}

// viewSpaceVideos 是「投稿」Tab。
//
// 它直接用 `list.PagedList`：那一条底座已经把翻页的三条保障、页脚重试、
// 回顶按钮、行的稳定 key 全做完了（`internal/ui/widgets/list/paged.go`），
// 这里再写一份就是两套翻页逻辑迟早对不上。
func (s *Shell) viewSpaceVideos(c *ui.Context, sp *upSpace) {
	p := sp.videos.Paged()
	// 一条都没有、也没在拉、也没出错 = 真的没有投稿。这时候画空状态而不是
	// 画一个空列表：`PagedList` 在这种时候把列表自己的空状态设成空串
	// （见 paged.go 的 syncList），画出来是一片白。
	if p.Loaded == 0 && !p.Loading && p.Err == nil {
		s.viewEmpty(c, "space:video:"+strconv.FormatInt(sp.mid, 10), emptyState{
			Title: "暂无投稿",
			Hint:  "他还没有投稿，或者换一个 UP 主看看",
		})
		return
	}
	sp.videos.View(c)
}

// viewSpaceList 画「动态 / 收藏夹 / 合集」这三张之一。
//
// 三张共用同一段翻页逻辑（upPager），各有各的行和 ListState。
func (s *Shell) viewSpaceList(c *ui.Context, sp *upSpace, tab int) {
	p := sp.pagerOf(tab)

	// ① 首屏那一拉，必须在**画之前**发出去。
	if ctx, gen := p.begin(); ctx != nil {
		go s.fetchUp(ctx, sp, tab, gen)
	}

	if !p.started && p.loading {
		// 一条都还没有：第一页在途。这��就是它全部的内容。
		s.viewEmpty(c, upEmptyKey(sp, tab), emptyState{Title: "正在加载" + upTabNames[tab] + "…"})
		return
	}
	if upRowCount(sp, tab) == 0 {
		s.viewEmpty(c, upEmptyKey(sp, tab), upTabEmpty(tab, sp.dropped[tab]))
		return
	}

	// 哨兵行：末尾多出来的那一行，滚到它就拉下一页。它**占了行高**，
	// 所以「还在不在画面里」这件事是能问出来的（末尾藏起来的话
	// 「滚到底」永远触发不了）。
	sentinel := p.started && (p.hasMore || p.err != nil)
	rows := upRowCount(sp, tab)
	if sentinel {
		rows++
	}

	st := &sp.state[tab]
	// 🔴 铁律四：行身份是这一行自己的 key，不是下标。每一帧重装是为了让
	// 「忘了装」这件事不可能发生（漏了的话 `TestRowsKeepTheirIdentity...` 会红）。
	st.Key = func(i int) any { return upRowKey(sp, tab, i) }
	st.Label = func(i int) string { return upRowLabel(sp, tab, i) }

	ui.List(c, st, rows).
		Key("space-list:" + strconv.FormatInt(sp.mid, 10) + ":" + upTabNames[tab]).
		Grow(1).FillHeight().FillWidth().
		Rows(func(r ui.ListRow) {
			// 🔴 行里 panic 会一路穿出 Cocoa 事件循环把进程带走。每一行
			// 自己兜一层，**只废掉这一行**（页级的 Shell.page 接的是整棵树，
			// 两者不重复）。
			defer func() {
				if rec := recover(); rec != nil {
					log.Printf("biu: UP 空间那一行画崩了（%s）：%v\n%s",
						upRowKey(sp, tab, r.Index), rec, debug.Stack())
				}
			}()
			if r.Index >= upRowCount(sp, tab) {
				s.viewUpSentinel(r.Context, sp, tab)
				return
			}
			s.viewUpRow(r.Context, sp, tab, r.Index)
		})

	// ②滚到底 ③加载后再查一遍。
	//
	// ⚠️ **必须排在 List 之后**：`Visible()` 报的是**上一帧**画出来的行
	// （`ListState.Visible` 的注释原话）。少一条的表现不一样：少 ② 是
	// 「滚到底了不加载」，少 ③ 是「他只有一页，底下留一大片空白还一直转圈」。
	if p.want() {
		if _, last := st.Visible(); last >= rows-1 {
			if ctx, gen := p.begin(); ctx != nil {
				go s.fetchUp(ctx, sp, tab, gen)
			}
		}
	}
}

// upSentinelKey 是末尾那一行的身份。**不用字符串**：行的 key 全是 B 站的
// id，理论上能撞上一个叫这个名字的；类型撞不上。
type upSentinelKey struct{}

// upRowCount 是第 tab 张 Tab 有几行内容（不含哨兵）。
func upRowCount(sp *upSpace, tab int) int {
	if tab == upTabDynamic {
		return len(sp.dynItems)
	}
	return len(sp.cols[tab])
}

// upRowKey 是第 tab 张 Tab 第 i 行的身份（铁律四）。
//
// 动态用 B 站的 `id_str`；收藏夹/合集用「类型 + id」。**加上类型是必须的**：
// 收藏夹的 id 和合集的 id 是两个不同的编号空间（B 站那边各自从 1 开始数），
// 混进同一张列表就会撞上——撞上时 MyGo 在 Tester 里当场 panic，真窗口里则
// 是那两行共用一份状态，选中一个另一个也亮。
func upRowKey(sp *upSpace, tab, i int) any {
	switch tab {
	case upTabDynamic:
		if i >= len(sp.dynItems) {
			return upSentinelKey{}
		}
		return sp.dynItems[i].ID
	case upTabCollection, upTabSeries:
		if i >= len(sp.cols[tab]) {
			return upSentinelKey{}
		}
		c := sp.cols[tab][i]
		return fmt.Sprintf("coll:%d:%d", c.Type, c.ID)
	}
	return upSentinelKey{}
}

// upRowLabel 是第 tab 张 Tab 第 i 行给无障碍和「打字选中」用的名字。
func upRowLabel(sp *upSpace, tab, i int) string {
	switch tab {
	case upTabDynamic:
		if i < len(sp.dynItems) {
			d := sp.dynItems[i]
			if d.Kind == bili.DynamicVideo && d.Video != nil && d.Video.Title != "" {
				return d.Video.Title
			}
			if d.Text != "" {
				return d.Text
			}
			return d.Author.Name
		}
		return "加载更多" + upTabNames[tab]
	default:
		if i < len(sp.cols[tab]) {
			return sp.cols[tab][i].Title
		}
		return "加载更多" + upTabNames[tab]
	}
}

// upEmptyKey 是某张 Tab 空状态的 key。
func upEmptyKey(sp *upSpace, tab int) string {
	return "space:" + strconv.FormatInt(sp.mid, 10) + ":" + upTabNames[tab]
}

// upTabEmpty 是某张 Tab 一个格子都没有时说的话。
//
// ⚠️ **「有丢弃」和「真的没有」必须说两句话**：B 站的动态有二十多种形态，
// 这一版只画三种。说成「暂无动态」的话，用户会以为他没发过。
func upTabEmpty(tab int, dropped int) emptyState {
	if dropped > 0 {
		return emptyState{
			Title: "这里没有能显示的" + upTabNames[tab],
			Hint: fmt.Sprintf("B 站的动态有好几种形态，这一版只画得出其中三种"+
				"（纯文字、图文、投稿），刚才那 %d 条都落在别的形态上了", dropped),
		}
	}
	switch tab {
	case upTabCollection:
		return emptyState{Title: "暂无收藏夹", Hint: "他还没有公开收藏夹"}
	case upTabSeries:
		return emptyState{Title: "暂无合集", Hint: "他还没有建合集或者系列"}
	}
	return emptyState{Title: "暂无动态", Hint: "回首页听歌，或者换一个 UP 主的空间看看"}
}

// viewUpSentinel 是末尾那一行：还有就拉，拉失败让人点重试。
//
// 空着也**占一行**（Biu 的 `count = hasMore ? items.length + 1 : items.length`）：
// 它是哨兵，滚到它就拉下一页。把它藏起来的话「滚到底」永远触发不了。
func (s *Shell) viewUpSentinel(c *ui.Context, sp *upSpace, tab int) {
	t := c.Theme()
	p := sp.pagerOf(tab)
	ui.Row(c).Key("space-sentinel").Center().Gap(8).Padding(12).Children(func() {
		switch {
		case p.err != nil:
			ui.Text(c, "加载失败："+p.err.Error()).FontSize(13).TextColor(t.TextMuted)
			if ui.Button(c, "重试").FontSize(13).Clicked() {
				p.err = nil
				if ctx, gen := p.begin(); ctx != nil {
					go s.fetchUp(ctx, sp, tab, gen)
				}
			}
		case p.loading:
			ui.Spinner(c)
			ui.Text(c, "正在加载…").FontSize(13).TextColor(t.TextMuted)
		default:
			ui.Text(c, "继续往下滚，加载更多").FontSize(13).TextColor(t.TextMuted)
		}
	})
}

// viewUpRow 画一行：动态卡片或者收藏夹卡片。
func (s *Shell) viewUpRow(c *ui.Context, sp *upSpace, tab, i int) {
	if tab == upTabDynamic {
		s.viewDynamicCard(c, sp.dynItems[i])
		return
	}
	s.viewCollectionRow(c, sp.cols[tab][i])
}
