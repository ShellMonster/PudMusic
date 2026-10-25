package ui

import (
	"fmt"
	"log"
	"runtime/debug"
	"strconv"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/list"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
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
	// ⚠️ **下面那 16 是四张 Tab 共用的那一段**：Tab 那一条和内容之间留一段
	// （上游那个 Tab 的**内容面板**是 `px-0 py-4`——上下各 16，左右那 16 由页面
	// 外层给）。原来一点都没有，于是「动态」两个字底下一张卡片就顶上来
	// （用户 2026-10-25 说的）。
	//
	// ⚠️ **上面那 16 是头图区和这一条之间的**：上游那一层是 `p-4`
	// （`user-profile/index.tsx:130`），上下左右各 16——左右那两段由这一列的
	// 左右内边距给，上下两段在这里给。
	//
	// ⚠️ **这一段给在 Tab 那一条上，不在内容面板里**：四张 Tab 的内容都摆在
	// 下面那一个 `space-body` 里（`viewUpSpace`），所以这一段是**四张一起**的
	// ——「一页两套口径」这件事从根上就不可能再有。
	ui.Column(c).Key("space-tabs").Padding(space.Px16, upPad, space.Px16, upPad).Children(func() {
		// ⚠️ **主 Tab 用轨道 + 滑块那一种**（`mainTabs`，见 tabs.go）：这一排
		// 和次级排序那几排不是同一个样子（票 #138）。
		//
		// ⚠️ `AlignSelf(ui.Start)`：同上，Column 默认 Stretch，轨道要包着字。
		mainTabs(c, &sp.tab, tabMD, upTabNames...).AlignSelf(ui.Start)
	})
}

// viewSpaceTab 画当前那一张 Tab 的内容。
//
// ⚠️ **头图区 + Tab 条是这一页的「页内头部」**：上游整页是一个滚动容器，头图跟着
// 列表一起滚走（`user-profile/index.tsx`）；我们此前把它摆在列表**外面**，于是滚
// 列表时头图（横幅、头像、名字、关注）钉在顶上不动。现在它是每张 Tab 那张列表的
// 第 0 行（`list.Lead` / 裸 `ui.List` 的第 0 行）。没有列表可挂的状态（加载中 /
// 失败 / 真的没有）走 `viewSpaceFlat`——那时没有东西可滚。
func (s *Shell) viewSpaceTab(c *ui.Context, sp *upSpace) {
	if sp.tab == upTabVideo {
		s.viewSpaceVideos(c, sp)
		return
	}
	s.viewSpaceList(c, sp, sp.tab)
}

// viewSpaceLead 画页内头部：头图区 + Tab 条。
func (s *Shell) viewSpaceLead(c *ui.Context, sp *upSpace) {
	s.viewSpaceHeader(c, sp, sp.header.Snapshot())
	s.viewSpaceTabs(c, sp)
}

// viewSpaceFlat 画「没有列表可挂」的那几种状态：头图区 + Tab 条在上，状态在下。
func (s *Shell) viewSpaceFlat(c *ui.Context, sp *upSpace, body func()) {
	s.viewSpaceLead(c, sp)
	ui.Row(c).Key("space-body").Grow(1).FillHeight().Children(body)
}

// upLeadKey 是页内头部那一行的身份（类型撞不上 B 站的 id）。
type upLeadKey struct{}

// upLeadRows 是裸 `ui.List` 那三张 Tab 的页内头部占几行（恒为 1）。
const upLeadRows = 1

// viewUpLead 画那三张 Tab 的第 0 行：头图区 + Tab 条（+「画不出来」那一行说明）。
//
// ⚠️ **不画灰底选中**：框架可能给被选中的行铺一整块主色底，页内头部自己刷一层页面
// 底色盖掉（同 `list.List.viewLead`）。
func (s *Shell) viewUpLead(c *ui.Context, sp *upSpace, tab int) {
	//
	// ⚠️ **动态那一张的列表有 8 的行间距**（`GapY`），页内头部是它的第 0 行，于是头部
	// 和第一张卡片之间多出一段 8——下外边距取 -8 抵掉，画面与头部在列表外面时一样。
	var bottom float32
	if tab == upTabDynamic {
		bottom = -space.Px8
	}
	ui.Column(c).Key("space-lead").AlignSelf(ui.Stretch).Background(c.Theme().Background).
		Margin(space.None, -upPad, bottom, -upPad).Children(func() {
		s.viewSpaceLead(c, sp)
		// 「有 N 条我们画不出来」那一行。
		//
		// ⚠️ **只在「一条都没画出来」时出现**：有内容的时候插一行字只是噪音；而一条都
		// 没有的时候，用户看到的是**一片空白加一个转着的圈**——像是坏了，而真相是
		// 「他发的那些我们画不出来」（票 #300）。它不是空状态：哨兵那一行还得留着。
		if upRowCount(sp, tab) == 0 && sp.drops[tab].total() > 0 {
			s.viewDroppedNote(c, sp.drops[tab].counts)
		}
	})
}

// upLeadHeight 是页内头部的估高（回顶按钮判「滚过没有」用，差几十像素无妨）。
const upLeadHeight = upHeaderH + 80

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
	//
	// ⚠️ **「正在加载」那一格不在这儿画**（票 #138）：它由列表自己画，画法是
	// 外面递进去的（`loadingCell`）——和「没有结果」那一格是同一个出口。
	// 而列表**照画**（下面那一支）：后台回来的结果是在列表的 `View` 里收的
	// （`adopt`），不画它这一页就永远停在「正在加载」。
	if sp.videosStarted && p.Loaded == 0 && !p.Loading && p.Err == nil {
		s.viewSpaceFlat(c, sp, func() {
			s.viewEmpty(c, "space:video:"+strconv.FormatInt(sp.mid, 10), emptyState{
				Title: "暂无投稿",
				Hint:  "他还没有投稿，或者换一个 UP 主看看",
			})
		})
		return
	}
	// 🔴 设置里的「数据显示样式」在这一帧生效（理由同搜索页那一处）。只有投稿
	// 这一张 Tab 是歌的列表——动态 / 收藏夹 / 合集那三张画的是卡片和数字，网格那一
	// 档对它们没有意义（见 upspace_tabs.go 的文件头）。
	sp.videos.SetView(s.displayView())
	// ⚠️ **外面这一层 Column 是必需的**：这一页的内容是摆在一个 `ui.Row` 里
	// （`viewUpSpace` 的 `space-body`），而表头那一行是 `FillWidth` 的——直接当兄弟
	// 摆进去的话它把整行的宽都吃掉了，列表被挤成 0 宽，于是**一首歌都看不见**（实测
	// 最小窗口下 `Find` 给回 `{900 … 0 0}` 那个盒子）。裹一层 Column，表头在上面、
	// 列表在下面，两边都在同一列里。
	// ⚠️ **头图区 + Tab 条 + 工具条 + 表头都是列表的第 0 行**（页内头部，
	// `list.Lead`）：跟着内容一起滚走。表头说的是下面那些歌，所以它跟着头图一起走
	// （同「流行」那一档，见 `homePage.viewPopLead`）。
	//
	// ⚠️ **只装一次**：闭包里读的 `p` 是每次画的时候现取的。
	if !sp.leadSet {
		sp.leadSet = true
		sp.videos.SetLead(&list.Lead{
			Label:  "UP 主的头图",
			Height: func() float32 { return upLeadHeight },
			Draw: func(c *ui.Context) {
				s.viewSpaceLead(c, sp)
				// 工具条：全部播放 + 共几首（设计规范 §7.18，和首页、搜索页同一枚主按钮）。
				//
				// ⚠️ **上边那一格是 0**：Tab 那一条和这一段之间那 16 已经给了（`viewSpaceTabs`），
				// 工具条自己再给一份的话，这一张 Tab 的间距就和另外三张不是同一个数了。
				ui.Row(c).Key("upspace:toolbar").FillWidth().Gap(space.Px12).AlignItems(ui.Center).
					Padding(space.None, upPad, space.Px8, upPad).Children(func() {
					p := sp.videos.Paged()
					if playAllButton(c, p.Loading || p.Loaded == 0).Clicked() {
						s.playAllSongs(sp.videos.Items())
					}
					if p.Loaded > 0 {
						n := fmt.Sprintf("已加载 %d 首", p.Loaded)
						if !p.HasMore {
							n = fmt.Sprintf("共 %d 首", p.Loaded)
						}
						ui.Text(c, n).FontSize(scale.Font(c, scale.Size14)).TextColor(c.Theme().TextMuted)
					}
				})
				sp.videos.Header(c, "upspace:header")
			},
		})
	}
	// ⚠️ **外面这一层 Column 是必需的**（理由见 `viewSpaceList`：滚动容器直接当别的
	// 容器的下一层时，里面文字的盒子量出来是 0×0）。
	ui.Column(c).Grow(1).FillHeight().Children(func() {
		sp.videos.View(c)
	})
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
		s.viewSpaceFlat(c, sp, func() {
			s.viewLoadError(c, upLoadErrorKey(sp, tab), "这个 UP 主的"+upTabNames[tab], "重试加载"+upTabNames[tab], p.err, func() {
				p.err = nil
				if ctx := p.begin(); ctx != nil {
					go s.fetchUp(ctx, sp, tab, p.cursor, p.page)
				}
			})
		})
		return
	}
	if !p.started && p.loading {
		// 一条都还没有：第一页在途。这一句就是它全部的内容。
		// ⚠️ **走 `viewLoading`（和「没有结果」同一个出口）**，理由同 `follow.go`
		// 那一处（票 #138）。
		s.viewSpaceFlat(c, sp, func() { s.viewLoading(c, upEmptyKey(sp, tab), "正在加载"+upTabNames[tab]+"…") })
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
		s.viewSpaceFlat(c, sp, func() { s.viewEmpty(c, upEmptyKey(sp, tab), upTabEmpty(tab, sp.drops[tab].counts)) })
		return
	}

	// 哨兵行：末尾多出来的那一行，滚到它就拉下一页。它**占了行高**，
	// 所以「还在不在画面里」这件事是能问出来的（末尾藏起来的话
	// 「滚到底」永远触发不了）。
	sentinel := p.started && (p.hasMore || p.err != nil)
	// ⚠️ **多出来的第 0 行是页内头部**（头图区 + Tab 条，跟着内容一起滚走），
	// 所以下面所有「第 i 行」的框架行号都比内容的下标大 1（`upLeadRows`）。
	rows := upRowCount(sp, tab) + upLeadRows
	if sentinel {
		rows++
	}

	st := &sp.state[tab]
	// 🔴 铁律四：行身份是这一行自己的 key，不是下标。每一帧重装是为了让
	// 「忘了装」这件事不可能发生（漏了的话 `TestADynamicKeepsItsIdentity...`
	// 会红，见 upspace_test.go）。
	st.Key = func(i int) any {
		if i < upLeadRows {
			return upLeadKey{}
		}
		return upRowKey(sp, tab, i-upLeadRows)
	}
	st.Label = func(i int) string {
		if i < upLeadRows {
			return "UP 主的头图"
		}
		return upRowLabel(sp, tab, i-upLeadRows)
	}

	// ⚠️ **外面套一层 Column 有两个缘故**，少一个都踩坑：
	//   - List 是自己管状态的控件，Key 只能挂在外面的元素上（见 element.go）；
	//   - 滚动容器**直接**当别的容器的下一层时，它里面文字的盒子量出来是
	//     0×0（BMG-16 记着的 Tester 那个坑），套一层就正常。
	//
	// ⚠️ **左右 16 是这一层给的，上下那一段不在这一层**（在上面 Tab 那一条的
	// 下内边距里，理由见 `viewSpaceTabs`）：上游那三张 Tab 的内容摆在
	// `user-profile/index.tsx:130` 那个 `p-4` 里，而动态卡片自己左右是 0
	// （动态流那一页的 32 是另一层 16+16）。卡片自己做内边距的话，同一个
	// 组件在两页就会缩成两个宽度。
	//
	// ⚠️ **左右 16 给在列表自己身上**（内容区的内边距，跟着内容滚），而头图区是
	// 通栏的：页内头部那一行用 -16 的左右外边距撑回满宽。
	ui.Column(c).Key("space-list:" + upTabNames[tab]).Grow(1).FillHeight().Children(func() {
		// 登记「内容区里当前那一个能被方向键走位的」（票 #245 的 E4）。
		//
		// ⚠️ **登记的是 `&sp.keys[tab]` 而不是裸的句柄**：这一份的
		// `TakeFocus` 还要把游标摆到第一行（走进来就在第一条上）。
		s.registerContentFocus(&sp.keys[tab])
		// 键盘接线：游标 + 「↑ 到顶回侧栏」（票 #245 的 E4）。
		//
		// ⚠️ **游标不许停在页内头部那一行上**（第 0 行不是内容，选中它按 Enter
		// 什么都不会发生、焦点环也跟错）：落到那一行就挪到第一条内容上；一条内容都
		// 没有就是「没选中」。必须在 `wire` 之前挪——`wire` 里的 `Moved` 看的是这一
		// 遍的游标，先挪回去它才看得出「↑ 已经在顶上了」，把焦点交回侧栏。
		if k := &sp.keys[tab]; k.started && k.sel >= 0 && k.sel < upLeadRows {
			if upRowCount(sp, tab) > 0 {
				k.sel = upLeadRows
				st.ScrollIntoView(k.sel)
			} else {
				k.sel = -1
			}
		}
		sp.keys[tab].wire(c, st, s.focusSide)
		spaceList := ui.List(c, st, rows).
			Grow(1).FillHeight().FillWidth().PaddingX(upPad).
			// ⚠️ **听一听输入**：为了知道「键盘往回走到顶上了」。
			// 返回 false 是刻意的：自己不吃这个事件，方向键挪游标、点击选行、
			// Tab 换焦点都还在。
			HandleInput(sp.keys[tab].keys.Input).
			// 焦点环只留在「还没选中任何一行」的时候（和 `widgets/list` 同一档）。
			FocusRing(sp.keys[tab].sel < 0).
			Rows(func(r ui.ListRow) {
				// 🔴 行里 panic 会一路穿出 Cocoa 事件循环把进程带走。每一行
				// 自己兜一层，**只废掉这一行**（页级的 Shell.page 接的是整棵树，
				// 两者不重复）。
				defer func() {
					if rec := recover(); rec != nil {
						log.Printf("biu: UP 空间那一行画崩了（%s）：%v\n%s",
							upRowKey(sp, tab, r.Index-upLeadRows), rec, debug.Stack())
					}
				}()
				if r.Index < upLeadRows {
					s.viewUpLead(r.Context, sp, tab)
					return
				}
				i := r.Index - upLeadRows
				if i >= upRowCount(sp, tab) {
					s.viewUpSentinel(r.Context, sp, tab)
					return
				}
				s.viewUpRow(r.Context, sp, tab, i)
			})
		// ⚠️ **动态那一张 Tab 的行间距由列表给**（上游 `mb-2` = 8）。
		//
		// 卡片自己的下外边距**不能**留着：那一段属于**行**（不在卡片里），而
		// 框架给「指针压着的那一行」铺一层 `t.SurfaceHover`——留着它，卡片底下
		// 就露出那一层（暖纸主题下是淡棕，用户 2026-10-25 说的那条带子）。
		// 改成列表的行间距之后，行和卡片一样高，那一层被卡片自己盖住了。
		//
		// ⚠️ **收藏夹 / 合集那两张 Tab 不给**：它们那一行是**卡片**（自己的底色、
		// 圆角、描边），行间距还是它自己的下外边距 8（画面不变）。
		if tab == upTabDynamic {
			spaceList = spaceList.GapY(space.Px8)
		}
		s.trackScrollbar(spaceList)
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
		// ⚠️ **身份是预先装箱好的那一份**（`sp.rowKeys[tab]`）：理由见那个字段
		// 的注释（票 #313）。
		return sp.rowKeys[tab].At(i, upRowIdent{kind: 0, id: sp.dynItems[i].ID}, upRowIdentity)
	case upTabCollection, upTabSeries:
		if i >= len(sp.cols[tab]) {
			return upSentinelKey{}
		}
		c := sp.cols[tab][i]
		// ⚠️ 理由同上面那一处。
		return sp.rowKeys[tab].At(i, upRowIdent{kind: 1, typ: c.Type, num: c.ID}, upRowIdentity)
	}
	return upSentinelKey{}
}

// upRowIdent 是 UP 空间那一列行身份的原材（三个 Tab 同一把尺子）。
//
// ⚠️ **可比较**：它要当「记着算好的那一份」那把钥匙（见 `rowkey.Memo` 的
// 文件头），所以里面不能有切片。
type upRowIdent struct {
	// kind 是哪一档：0 动态，1 收藏夹·合集·系列。
	kind int
	// id 是动态的 `id_str`。
	id string
	// typ / num 是收藏夹那一档的类型和 id。
	typ int
	num int64
}

// upRowIdentity 是 UP 空间那一列的行身份（铁律四）。
//
// ⚠️ **动态用 B 站的 `id_str`；收藏夹/合集用「类型 + id」**。加上类型是必须的：
// 收藏夹的 id 和合集的 id 是两个不同的编号空间（B 站那边各自从 1 开始数），
// 混进同一张列表就会撞上——撞上时 MyGo 在 Tester 里当场 panic，真窗口里则是
// 那两行共用一份状态，选中一个另一个也亮。
//
// 这一步会分配（拼字符串 + 装箱）——所以它只在身份变了的时候才走
// （见 `rowkey.Memo` 的文件头）。
func upRowIdentity(k upRowIdent) any {
	if k.kind == 0 {
		return k.id
	}
	return fmt.Sprintf("coll:%d:%d", k.typ, k.num)
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
//
// ⚠️ **带上「有没有丢弃」那一档**：同一个 key 上换文案，读屏软件读到的是
// 同一句话，而「暂无动态」和「有 N 条没显示」必须被当成两件事（理由同
// `feedEmptyKey`）。
func upEmptyKey(sp *upSpace, tab int) string {
	if sp.drops[tab].total() > 0 {
		return "space-dropped:" + strconv.FormatInt(sp.mid, 10) + ":" + upTabNames[tab]
	}
	return "space:" + strconv.FormatInt(sp.mid, 10) + ":" + upTabNames[tab]
}

// upTabEmpty 是某张 Tab 一个格子都没有时说的话。
//
// ⚠️ **「有丢弃」和「真的没有」必须说两句话**：B 站的动态有二十多种形态，
// 这一版只画三种。说成「暂无动态」的话，用户会以为他没发过。
//
// ⚠️ **那个数是累计的，而且按「不同的动态」算**：翻了好几页各丢几条就是几十
// 条，说「有 N 条落在别的形态上了」比说「刚才那几条」诚实；而 B 站翻页会
// 重叠，同一条在两页里各出现一次——按出现次数记的话那个数会偏大（票 #344）。
//
// ⚠️ **按类别说**（票 #344）：「形态不认识」和「我们读不懂」是**两回事**，
// 合成一句就是把「B 站改了返回的形状」说成「他发了我们画不出来的形态」。
func upTabEmpty(tab int, counts bili.DropCounts) emptyState {
	if counts.Total() > 0 {
		return emptyState{
			Title: "这里没有能显示的" + upTabNames[tab],
			Hint:  dropSentence(counts),
			Info:  true,
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
	ui.Row(c).Key("space-sentinel").Center().Gap(space.Px8).Padding(space.Px12).Children(func() {
		switch {
		case p.err != nil:
			ui.Text(c, "加载失败："+p.err.Error()).FontSize(scale.Font(c, scale.Size13)).TextColor(t.TextMuted)
			if ui.Button(c, "重试加载"+upTabNames[tab]).FontSize(scale.Font(c, scale.Size13)).Clicked() {
				p.err = nil
				if ctx := p.begin(); ctx != nil {
					go s.fetchUp(ctx, sp, tab, p.cursor, p.page)
				}
			}
		case p.loading:
			ui.Spinner(c)
			ui.Text(c, "正在加载…").FontSize(scale.Font(c, scale.Size13)).TextColor(t.TextMuted)
		default:
			ui.Text(c, "继续往下滚，加载更多").FontSize(scale.Font(c, scale.Size13)).TextColor(t.TextMuted)
		}
	})
}

// viewUpRow 画一行：动态卡片或者收藏夹卡片。
func (s *Shell) viewUpRow(c *ui.Context, sp *upSpace, tab, i int) {
	if tab == upTabDynamic {
		s.viewDynamicCard(c, sp.dynItems[i],
			sp.keys[tab].selected(i), sp.keys[tab].ring(c))
		return
	}
	s.viewCollectionRow(c, sp.cols[tab][i],
		sp.keys[tab].selected(i), sp.keys[tab].ring(c))
}

// upLoadErrorKey 是「这一张 Tab 第一页就拉失败」那一支的 key。
//
// **和空状态那个 key 分开**：同一个 key 上换文案，读屏软件读到的是同一句话，
// 而「暂无」和「加载失败」必须被当成两件事。
func upLoadErrorKey(sp *upSpace, tab int) string {
	return "space-err:" + strconv.FormatInt(sp.mid, 10) + ":" + upTabNames[tab]
}
