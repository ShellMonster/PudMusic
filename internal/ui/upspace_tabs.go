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

// viewSpaceTabs 是那一条 Tab。
//
// 用的是 MyGo 自带的 `ui.Tabs`（`selected` 绑的是 sp.tab 那个地址）——点击、
// 左右方向键、Home/End 全是框架给的，我们不去重写。
func (s *Shell) viewSpaceTabs(c *ui.Context, sp *upSpace) {
	// ⚠️ **Key 挂在外面的 Column 上，不能挂在 Tabs 上**：Tabs 是「自己管状态」
	// 的那一类控件（Checkbox/Radio/Slider/List/TextInput 都一样），框架造它
	// 的时候就把状态建好了，事后再挂 key 会 panic（`ui/element.go` 的 `Key`）。
	// 给他外面套一层是最省事的做法。
	ui.Column(c).Key("space-tabs").PaddingX(upPad).Children(func() {
		ui.Tabs(c, &sp.tab, upTabNames...)
	})
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
	// ⚠️ **第一页必须在这里发出去，不能等 `PagedList` 自己那一发**。
	//
	// `PagedList` 是在自己的 `View` 里发第一页的（`if p.page == 0`），而下面
	// 那一支在「还没加载过」时**不调 View**——于是第一页永远发不出去，
	// 界面上是「暂无投稿」停在那儿，而 B 站那边一条都没拉过。
	// `NewPaged` 的注释也写着「新页面不在构造器里拉第一页，由第一帧的 view 拉」。
	if !sp.videosStarted {
		sp.videosStarted = true
		sp.videos.Reload()
	}

	p := sp.videos.Paged()
	// 一条都没有、也没在拉、也没出错 = 真的没有投稿。这时候画空状态而不是
	// 画一个空列表：`PagedList` 在这种时候把列表自己的空状态设成空串
	// （见 paged.go 的 syncList），画出来是一片白。
	if sp.videosStarted && p.Loaded == 0 && !p.Loading && p.Err == nil {
		s.viewEmpty(c, "space:video:"+strconv.FormatInt(sp.mid, 10), emptyState{
			Title: "暂无投稿",
			Hint:  "他还没有投稿，或者换一个 UP 主看看",
		})
		return
	}
	// 🔴 设置里的「数据显示样式」在这一帧生效（理由同搜索页那一处）。只有投稿
	// 这一张 Tab 是歌的列表——动态 / 收藏夹 / 合集那三张画的是卡片和数字，网格那一
	// 档对它们没有意义（见 upspace_tabs.go 的文件头）。
	sp.videos.SetView(s.displayView())
	sp.videos.View(c)
}

// viewSpaceList 画「动态 / 收藏夹 / 合集」这三张之一。
//
// 三张共用同一段翻页逻辑（upPager），各有各的行和 ListState。
func (s *Shell) viewSpaceList(c *ui.Context, sp *upSpace, tab int) {
	p := sp.pagerOf(tab)

	// ① 首屏那一拉，必须在**画之前**发出去。
	//
	// ⚠️ **只在「一页都还没拉过」时发**：这一段每帧都跑，而 `begin()` 只看
	// 「不是在拉、没有错、还有下一页」，于是第一页一落地，下一帧就又发一页
	// ——**用户什么都没滚，后面的页全被拉完了**（实测：一页 50 条共 4 页，
	// 不滚一下就请求了 4 次、攒了 200 条；滚动哨兵根本没机会起作用，
	// 内存和网络都在用户看不见的地方烧掉）。
	//
	// 翻回来的那几张 Tab `started` 是真，也不重拉。
	//
	// ⚠️ **cursor 和 page 在这里读，不在后台那条线上读**：闸门是界面线程的
	// 状态（铁律一），后台碰它就是数据竞争。真机上平时看不出来（拉一趟就
	// 回来了），而在慢帧上那一次读会和这一帧的 `adopt` 写撞上。
	if !p.started && !p.loading {
		if ctx := p.begin(); ctx != nil {
			go s.fetchUp(ctx, sp, tab, p.cursor, p.page)
		}
	}

	// ⚠️ **「拉失败了」必须排在「空」前面**。反过来的话第一页拉失败时
	// 界面上是「暂无动态 / 暂无收藏夹 / 暂无合集」——一句话都不提失败，
	// 而 `started` 仍是 false、`err` 非 nil，切走再切回来也不会重拉，
	// 用户看到的是「他真的什么都没有」。
	if !p.started && p.err != nil {
		s.viewLoadError(c, upLoadErrorKey(sp, tab), "重试加载"+upTabNames[tab], p.err, func() {
			p.err = nil
			if ctx := p.begin(); ctx != nil {
				go s.fetchUp(ctx, sp, tab, p.cursor, p.page)
			}
		})
		return
	}
	if !p.started && p.loading {
		// 一条都还没有：第一页在途。这一句就是它全部的内容。
		s.viewEmpty(c, upEmptyKey(sp, tab), emptyState{Title: "正在加载" + upTabNames[tab] + "…"})
		return
	}
	// 🔴 **「一行都没有」也分两种**：
	//
	//   - **而且没有下一页了** → 画空状态（「暂无动态」那一句）。
	//   - **还有下一页** → 继续画哨兵那一行，**不画空状态**。实测踩过的坑：
	//     一页里全是转发的动态（全被丢弃）时留下 0 行，早一版在这里直接
	//     画空状态并 return，于是哨兵那一行**根本没被画出来**，「滚到底」
	//     和「容器没填满」两条保障一个都问不到 —— 第二页有一条正常的，
	//     永远不再加载。
	keepLooking := p.started && p.hasMore
	if upRowCount(sp, tab) == 0 && !keepLooking {
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
	// 「忘了装」这件事不可能发生（漏了的话 `TestADynamicKeepsItsIdentity...`
	// 会红，见 upspace_test.go）。
	st.Key = func(i int) any { return upRowKey(sp, tab, i) }
	st.Label = func(i int) string { return upRowLabel(sp, tab, i) }

	// ⚠️ **外面套一层 Column 有两个缘故**，少一个都踩坑：
	//   - List 是自己管状态的控件，Key 只能挂在外面的元素上（见 element.go）；
	//   - 滚动容器**直接**当别的容器的下一层时，它里面文字的盒子量出来是
	//     0×0（BMG-16 记着的 Tester 那个坑），套一层就正常。
	ui.Column(c).Key("space-list:" + upTabNames[tab]).Grow(1).FillHeight().
		Children(func() {
			ui.List(c, st, rows).
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
		})

	// ②滚到底 ③加载后再查一遍。
	//
	// ⚠️ **必须排在 List 之后**：`Visible()` 报的是**上一帧**画出来的行
	// （`ListState.Visible` 的注释原话）。少一条的表现不一样：少 ② 是
	// 「滚到底了不加载」，少 ③ 是「他只有一页，底下留一大片空白还一直转圈」。
	if p.want() {
		if _, last := st.Visible(); last >= rows-1 {
			if ctx := p.begin(); ctx != nil {
				go s.fetchUp(ctx, sp, tab, p.cursor, p.page)
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
	// 「动态」那一格读路由表（`router.go` 里那一条）：出处和上游的对应记在
	// 那儿，不在这儿抄一遍。
	return lookup(upPath).Empty
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
			if ui.Button(c, "重试加载"+upTabNames[tab]).FontSize(13).Clicked() {
				p.err = nil
				if ctx := p.begin(); ctx != nil {
					go s.fetchUp(ctx, sp, tab, p.cursor, p.page)
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

// upLoadErrorKey 是「这一张 Tab 第一页就拉失败」那一支的 key。
//
// **和空状态那个 key 分开**：同一个 key 上换文案，读屏软件读到的是同一句话，
// 而「暂无」和「加载失败」必须被当成两件事。
func upLoadErrorKey(sp *upSpace, tab int) string {
	return "space-err:" + strconv.FormatInt(sp.mid, 10) + ":" + upTabNames[tab]
}
