// 播放条上的分集抽屉：Biu 的 `src/layout/playbar/left/page-list.tsx`。
//
// 一个写着「分集」的键，点开是**向上弹**的浮层（那边是
// `<Popover placement="top" offset={28}>`），头部固定「分集」两个字，下面一列
// 分集，正在放的那一集整行染主色，点一行立刻切过去并收起——
// `onPressItem={onClose}`，一个字不差。
//
// 数据那一侧和满屏里那个分集面板**共用同一份**：`app.Parts()`、`partRowsFor`、
// `app.PlayPart`。共用不是为了少写几行，是为了「正在放的那一集」只有一处认：
// 两处各认一次的话，迟早有一处认错，而那正是用户在找的那一句。
//
// # 和满屏那个面板差在哪，为什么
//
// 满屏那个是**贴在满屏背景上的半透明玻璃面板**（`viewFullPartsPanel`：280 宽、
// 32 行高、不画封面也不画时长，照 Biu 那个 `[&_span.tabular-nums]:hidden`）。
// 这个是**贴在普通界面上的浮层**，宽一些、行高一些，而且**画出编号**。
//
// ⚠️ **编号是这一票的要求，Biu 那一支没有**：上游的分集行画的是封面 + 标题 +
// 时长，`pageIndex` 在它整个界面里只被播放逻辑读（`music-page-list/index.tsx` 的
// `renderItem` 根本没碰它）。而验收写的是「每一集有编号和标题」——分集标题经常
// 是「合集名 + 某某」这种不带序号的写法，第 12 集和第 2 集排在一起时分不清谁在
// 谁前面。
//
// ⚠️ **封面不画**：B 站每一集都有首帧图（`first_frame`），而我们**没取**
// （`bili.VideoPart` 没有那一栏，见 `service/bili/video.go` 的 `pagelistItem`）。
// 拿整个视频的封面去填每一行，画出来是三张一样的图——那不是分集列表，那是坏了。
package ui

import (
	"strconv"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// 这一列的尺寸。
//
// 宽度和行高都是上游那两个数（`layout/playbar/left/page-list.tsx:29` 的
// `min-w-[500px]`、同一处传给 `MusicPageList` 的 `itemHeight={64}`），
// DESIGN.md 的 `parts_panel` 也是这两个数。
//
// ⚠️ **原来这里写着「不是 Biu 那个 500」**，理由是「500 是给封面 + 标题 + 时长
// 留的，而我们不画封面和时长，只留标题」。那个理由只在「只留标题」成立时才成立，
// 而用户看到的是**同一个浮层**：它在 Biu 里是 500 宽、64 一行，在我们这里只有
// 360 / 40——长标题截得比上游早，行挤得像一串标签。这一票按 DESIGN.md 对齐到
// 上游的数，代价是浮层更宽——它仍然比最小窗口（900）窄得多。
const (
	// partDrawerWidth 是这一列多宽。500 是上游的 `min-w-[500px]`：分集标题常是
	// 「某某合集 第 N 集」这种长句，500 里放得下三十多个字才截。
	partDrawerWidth = 500
	// partHeaderHeight 是顶上那一条（照 Biu 的 `px-4 py-3`，够放一行 14 号字）。
	partHeaderHeight = 44
	// partRowHeight 是一行多高。64 是上游传给 `MusicPageList` 的
	// `itemHeight={64}`，和列表行、队列行同一个数（DESIGN.md 的 `parts_panel`）。
	partRowHeight = 64
	// partNumW 是编号那一列的宽。「100」在 12 号字下不到 24，28 是给三位数留的。
	partNumW = 28
	// partListMaxHeight 是下面那一列的**上限**。60vh 是 Biu 那个浮层的
	// `h-[60vh]`，420 是封顶（Biu 那边没有封顶，它靠窗口高度）。
	partListMaxHeight = 420
	// partDividerH 是顶上那条分隔线。
	partDividerH = 1
)

// viewPartButton 是播放条上那个「分集」键，加上它弹出来的那一列。
//
// ⚠️ **入口在「还没问到」和「问失败」时也得摆着**，判据和满屏那个把手一模一样
// （见 `viewFullParts` 的注释）：单 P 的稿子照 Biu 的 `hasMultiPart` 是没有入口
// 的，而「问不出分集：…」这句话要有一个能点开的地方才有地方显示——收掉的话它在
// 真机上是死的。只有**确知只有一集**的时候才收起来，那时候确实没有「选」的意思。
//
// ⚠️ **摆在左边第三段**（票 #219，上游 `playbar/left/index.tsx:86`）：封面右边
// **横着并排**的那一行（分集 / 收藏 / 赞），和「歌名/UP 名」那一列平级。
// 改之前它在**歌名下面那一行**——那一段的注释里有账和那两条钉着它的测试。
//
// 而 `viewTrackActions` **第一行就 return 了**——没有歌的时候那一段一格都不画，
// 所以「没有歌就不摆」这条在这个函数里已经成立；下面那个 `HasTrack` 判是给**别的
// 调用方**留的（将来这个键要是被摆到别处去，它自己认得）。
func (s *Shell) viewPartButton(c *ui.Context, st app.State) {
	if !st.HasTrack {
		s.partOpen = false
		return
	}

	parts := s.app.Parts()
	cur, _ := s.app.CurrentItem()
	s.partRows = partRowsFor(parts, cur)

	if !partsWorthEntry(parts, s.app.PartsWorthAsking()) {
		// 入口不摆的时候**必须把开着的那一列收掉**：否则这一刻开着的是上一首的
		// 浮层状态，等下一首多 P 的歌放起来，它会自己弹出来——用户什么都没按。
		s.partOpen = false
		return
	}

	// ⚠️ **图标不是装饰，是它唯一能被认出来的东西**：`ui.Icon` 对辅助技术是不可见
	// 的（`mygo/ui/svg.go`），所以 `Label("分集")` 不许省——它既是无障碍名字，也是
	// 无头 Tester 认得这个按钮的唯一凭据（`Find` 按 label 找）。
	//
	// 🔴 **图标 18、点击区 32**（票 #219，报告 §3.6）：上游那一枚是
	// `RiListRadio size={18}`（`layout/playbar/left/page-list.tsx:25`），而
	// **播放条上全部图标键都是 18**。改之前是 14（`iconSm`）——比上游小 4 像素，
	// 在 32 的盒子里每边留 9，看起来像「画糊了」。
	//
	// 尺寸照同一段上另外两枚（赞 / 收藏）：`iconMd` 的图标 + `iconHitMd` 的点击区
	// + 每边 7 的内边距（那一圈留白就是 `TestTheHitAreaIsBiggerThanTheGlyph` 断的
	// 「点击区比图形大」）。
	btn := s.withTip(c, iconKey(c, iconPageList, "分集", iconMd, false).
		Size(iconHitMd, iconHitMd).Padding(space.None, space.Px7),
		partEntryTooltip(parts, cur, s.app.PartsWorthAsking(), s.app.PartsErr()))
	if btn.Clicked() {
		s.partOpen = !s.partOpen
	}
	// Popover 自己处理「点外面关掉」和 Escape，所以这里只管开关本身
	// （和 `viewRate` 同一个写法）。
	//
	// ⚠️ 浮层那一层外面包一层 Guard（铁律三）：它是在**播放条这一层的布局之外**
	// 单独建的，它一崩坏掉的是整块浮层，而报错的地方在播放条上——两者对不上，
	// 极难查。
	ui.Popover(c, btn, &s.partOpen, func() {
		s.guardAt("分集抽屉", s.viewPartDrawer)(c)
	})
}

// partsWorthEntry 报「这一首摆不摆分集入口」。
//
// 三种情形：问到多于一条（摆）；一条都还没问到、而这一首问得到（摆，那是在问
// 或者问失败）；**确知只有一条**（不摆）。
//
// 最后一档是验收里那一条「单 P 稿件不显示这个入口」：留一个点了没反应的键，
// 用户会以为应用坏了。
func partsWorthEntry(parts []bili.VideoPart, worthAsking bool) bool {
	return len(parts) > 1 || (len(parts) == 0 && worthAsking)
}

// partsHowItIs 说「这一首的分集问到哪儿了」，**只有一个出口**。
//
// 返回空串 = 问到了，那一句话交给调用方自己说（有几集、第几集是另一回事）。
// 返回别的那三句里的一句 = 还没问到，而三句要分清，因为它们对用户的下一步不一样：
//
//	这一首没有分集    换到本地文件 / 音乐库了，不是没问到
//	问不出分集：…     问了，B 站没给（网络 / 风控）；`App` 隔 15 秒会再问一次
//	正在问这一首有哪几集 还没回来
//
// ⚠️ **键上的悬停提示和抽屉里那一行字都调它**。这两处说的是同一件事，而同一时刻说
// 两种说法就等于在界面上撒两个矛盾的谎——评审抓到过一次：把 pagelist 顶成 403 之后，
// 悬停提示还在说「正在问这一首有哪几集」，而抽屉里说的是「问不出分集：HTTP 403」。
// 而 `App` 失败之后 15 秒内不会再问（`partsRetryAfter`），那句「正在问」会挂很久。
//
// ⚠️ **收的是「几集」而不是那份列表**：两个调用点手上一个有 `[]bili.VideoPart`
// 一个只有 `[]partRow`，而「有没有问到」两边都只是长度——`partRowsFor` 是一集一行。
func partsHowItIs(n int, worthAsking bool, err error) string {
	switch {
	case !worthAsking:
		return "这一首没有分集"
	case err != nil:
		return "问不出分集：" + err.Error()
	case n == 0:
		return "正在问这一首有哪几集"
	}
	return ""
}

// partEntryTooltip 是那个键悬停时的一句话。
//
// 有几集说几集、正好在第几集，用户在**不点开**的时候就知道自己在一个多长的列表的
// 第几格——分集多的稿子（第 100 集那种）光靠点进去数是数不出来的。
//
// 还没问到的那三档走 `partsHowItIs`（和抽屉里那一行字同一句），末尾多一句「点一下
// 看」——因为那句话本身就是这个键的用处：它说清现在是什么状态，而点开才看得到详情。
func partEntryTooltip(parts []bili.VideoPart, cur app.Item, worthAsking bool, err error) string {
	if how := partsHowItIs(len(parts), worthAsking, err); how != "" {
		return how + "（点一下看）"
	}
	at := "还没开始放其中一集"
	for i, p := range parts {
		if p.Cid == cur.Cid {
			at = "正在放第 " + strconv.Itoa(i+1) + " 集"
			break
		}
	}
	return "共 " + strconv.Itoa(len(parts)) + " 集，" + at + "（点一下看全部）"
}

// partDrawerHeight 是这一块该有多高：**按内容算**，不是一块固定高的死面板。
//
// ⚠️ **为什么不能照抄一个固定高度**：这个上限（`partListMaxHeight`）是照 Biu 那个
// 浮层的 `h-[60vh]` 抄的，而 Biu 那一列**每行带一张 48×48 的封面**、行高 64——三集就
// 占 192，一块 420 高的面板正好装得下。我们**不画封面**（`bili.VideoPart` 没有每集的
// 首帧图，见文件头），行仍然是上游那个 64：三集是 3×64 = 192，固定画 420 的话会在
// 下面**空出 183 像素（一屏的 44%）**，而那块空白在界面上和「这个面板下面还有东西
// 没画出来」长得一模一样。所以高度按行数算。
//
// 集数多的时候封顶在 `min(winH*0.6, partListMaxHeight)`，那一列照旧滚动（`ui.List`）。
//
// `n` 是这一帧要画的几行；空状态那一句话也算一行的位置（它是一行 12 号字带 12 的
// 内边距），所以这里按**至少一行**算——否则那句「问不出分集」会被挤到面板外面。
func partDrawerHeight(n int, winH float32) float32 {
	head := float32(partHeaderHeight + partDividerH)
	cap := min(winH*0.6, partListMaxHeight)
	if cap <= head {
		return cap
	}
	// 一屏放得下几行。
	if fits := int((cap - head) / partRowHeight); n > fits {
		n = fits
	}
	return head + float32(max(n, 1))*partRowHeight
}

// viewPartDrawer 是浮层里的那一列。
//
// ⚠️ **背景和圆角是 `ui.Popover` 给的**（`corePopover` → `stylePanel`：主题底色
// + 圆角 + 一圈边 + 阴影），这里只摆内容——自己再画一层底色的话，底下那一层透
// 不出来，浮层看着就比实际大一圈。
func (s *Shell) viewPartDrawer(c *ui.Context) {
	t := c.Theme()
	_, winH := c.Size()

	ui.Column(c).Key("part-drawer").Width(partDrawerWidth).
		Height(partDrawerHeight(len(s.partRows), winH)).
		// ⚠️ 这一对**不许省**：它是无头 Tester 量这一块高度的**唯一凭据**
		// （`Tester.Find` 认的是文字和 `Label`，不认 `Key`），而 `TestTheDrawerIsOnly
		// AsTallAsItsContent` 量的就是它。它本身也该有——`queue_drawer.go` 那个抽屉
		// 同样挂着 `Role(ui.RoleDialog).Label(...)`，读屏软件据此知道这块是一个弹层。
		Role(ui.RoleDialog).Label("分集列表").
		Children(func() {
			s.viewPartDrawerHeader(c, t)

			switch {
			case len(s.partRows) == 0:
				// 一片空白在界面上和「这一首没有分集」长得一模一样。而分集是问 B 站
				// 问来的，问不到是网络/风控的事，不是这一首没有分集——三句话说开。
				ui.Text(c, s.partDrawerWhy()).FontSize(scale.Font(c, scale.Size12)).
					TextColor(t.TextMuted).Padding(space.Px12)
			default:
				s.viewPartDrawerList(c, t)
			}
		})
}

// partDrawerWhy 是空状态那一行说的话（问到了就不画它）。
//
// 走的是 `partsHowItIs`——和键上那个悬停提示同一个出口，理由见那一条。
func (s *Shell) partDrawerWhy() string {
	return partsHowItIs(len(s.partRows), s.app.PartsWorthAsking(), s.app.PartsErr())
}

// viewPartDrawerHeader 是顶上那一条：「分集」和共几集。
//
// 共几集**写在浮层上而不是只写在键的悬停提示里**：提示要等鼠标停上去，而列表
// 一打开用户第一眼要的就是「一共几个」——他好判断自己滑到哪里为止。
func (s *Shell) viewPartDrawerHeader(c *ui.Context, t *ui.Theme) {
	ui.Column(c).FillWidth().Children(func() {
		ui.Row(c).FillWidth().Height(partHeaderHeight).PaddingX(space.Px12).
			AlignItems(ui.Center).Children(func() {
			// ⚠️ **字重不写**（400）：抽屉标题那一档是 400（DESIGN.md §二）。
			ui.Text(c, "分集").FontSize(scale.Font(c, scale.Size14)).Grow(1)
			if n := len(s.partRows); n > 0 {
				ui.Text(c, "共 "+strconv.Itoa(n)+" 集").FontSize(scale.Font(c, scale.Size12)).TextColor(t.TextMuted)
			}
		})
		// 框架的 `ui.Divider`：1 DIP、无圆角、无内边距、取 `t.Border`——和原来
		// 手搓的那一行逐字一样。外面是 `Column`，所以它给的是高 1 的那条横线。
		ui.Divider(c)
	})
}

// viewPartDrawerList 是浮层里那一列。
func (s *Shell) viewPartDrawerList(c *ui.Context, t *ui.Theme) {
	rows := s.partRows
	s.partList.Key = func(i int) any { return rows[i].key }
	s.partList.Label = func(i int) string { return rows[i].title }

	e := ui.List(c, &s.partList, len(rows), func(i int) {
		s.viewPartRow(c, rows[i], t)
	}).Grow(1).FillWidth()
	s.trackScrollbar(e)
}

// viewPartRow 是分集列表里的一行：编号 + 标题。
//
// ⚠️ 悬停要**在 Children 之前**问：指针是在布局那一步才知道落在哪儿的
// （和 `queue_drawer.go` 的 `viewQueueRow` 同一个道理）。
func (s *Shell) viewPartRow(c *ui.Context, r partRow, t *ui.Theme) {
	row := ui.Row(c).Key(r.key).Grow(1).Height(partRowHeight).
		PaddingX(space.Px12).Gap(space.Px8).AlignItems(ui.Center).Radius(t.Radius)
	switch {
	case r.active:
		// Biu 那一支正在放的那一行是 `variant="flat" color="primary"`，
		// 全屏版是 `data-[active=true]:bg-primary/20`——都是「整行染主色」。
		// 别的记号（左边一条色杠、单独一个图标）都没有用：这一列没有封面，
		// 一行字加一个数字，靠底色认人最快。
		row = row.Background(t.Accent)
	case row.Hovered():
		row = row.Background(t.SurfaceHover)
	}

	row.Children(func() {
		ui.Text(c, strconv.Itoa(r.page)).Width(partNumW).FontSize(scale.Font(c, scale.Size12)).
			TextColor(partRowText(t, r.active, true))
		ui.Text(c, r.title).Grow(1).FontSize(scale.Font(c, scale.Size13)).
			TextColor(partRowText(t, r.active, false)).SingleLine()
	})

	if row.Clicked() {
		// 选完就收：Biu 的 `onPressItem={onClose}`。
		// 收在换集**之前**——`PlayPart` 要清轨道再异步取流，那几百毫秒里这一列
		// 摆着的是正在放的那一集的空状态（还没有歌），比摆着上一集的行更让人
		// 以为换失败了。
		s.partOpen = false
		if err := s.app.PlayPart(r.cid); err != nil {
			s.addToast("换不了这一集："+err.Error(), notice.Danger)
		}
	}
}

// partRowText 是一行里那两段字各该用什么颜色。
//
// numbered 那一位（编号）在非选中时比标题更弱：它是**定位**用的（找第几集），
// 而标题才是**读**的那一句。把两者画成一个粗细，用户扫一列的时候先看到的是
// 一排数字。
func partRowText(t *ui.Theme, active, numbered bool) ui.Color {
	switch {
	case active:
		return t.AccentText
	case numbered:
		return t.TextMuted
	default:
		return t.Text
	}
}
