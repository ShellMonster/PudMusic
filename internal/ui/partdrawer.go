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
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/egoist/mygo/ui"
)

// 这一列的尺寸。
//
// ⚠️ **不是 Biu 那个 500**（`page-list.tsx` 的 `min-w-[500px]`）：它那个 500 是
// 给 48×48 的封面 + 标题 + 时量留的，而这里三样只留标题。照抄 500 的话，一列
// 两行字的东西占掉大半个窗口。
const (
	// partDrawerWidth 是这一列多宽。360 是量出来的：分集标题常是「某某合集 第
	// N 集」这种长句，13 号字在 360 里能放下约二十个字，再长才截。
	partDrawerWidth = 360
	// partHeaderHeight 是顶上那一条（照 Biu 的 `px-4 py-3`，够放一行 14 号字）。
	partHeaderHeight = 44
	// partRowHeight 是一行多高。比满屏那个面板的 32 高一档：那个面板是压在满屏
	// 背景上的玻璃条，行挤一点；这个浮层落在普通界面上，行给足 40 才不像一串被
	// 切掉的标签。
	partRowHeight = 40
	// partNumW 是编号那一列的宽。「100」在 12 号字下不到 24，28 是给三位数留的。
	partNumW = 28
	// partListMaxHeight 是下面那一列的最高高度。60vh 是 Biu 那个浮层的
	// `h-[60vh]`，420 是封顶（Biu 那边没有封顶，它靠窗口高度）。
	partListMaxHeight = 420
)

// viewPartButton 是播放条上那个「分集」键，加上它弹出来的那一列。
//
// ⚠️ **入口在「还没问到」和「问失败」时也得摆着**，判据和满屏那个把手一模一样
// （见 `viewFullParts` 的注释）：单 P 的稿子照 Biu 的 `hasMultiPart` 是没有入口
// 的，而「问不出分集：…」这句话要有一个能点开的地方才有地方显示——收掉的话它在
// 真机上是死的。只有**确知只有一集**的时候才收起来，那时候确实没有「选」的意思。
//
// ⚠️ **摆在哪一行是量出来的**：它在歌名下面那一行（`viewTrackMeta`，那一段的注释
// 里有账和那两条钉着它的测试）。而 `viewTrackMeta` **第一行就 return 了**——没有歌
// 的时候它一格都不画，所以「没有歌就不摆」这条在这个函数里已经成立；下面那个
// `HasTrack` 判是给**别的调用方**留的（将来这个键要是被摆到别处去，它自己认得）。
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
	// 尺寸照另外三个键（赞 / 收藏 / 下载）：`iconSm` 的图标 + `iconHit` 的点击区 +
	// 每边 7 的内边距（那一圈留白就是 `TestTheHitAreaIsBiggerThanTheGlyph` 断的
	// 「点击区比图形大」）。
	btn := iconButton(c, iconPageList, "分集", iconSm).
		Size(iconHit, iconHit).Padding(0, 7).
		Tooltip(partEntryTooltip(parts, cur))
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
		Guard(s.viewPartDrawer, s.onPanic)(c)
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

// partEntryTooltip 是那个键悬停时的一句话。
//
// 有几集说几集、正好在第几集，用户在**不点开**的时候就知道自己在一个多长的
// 列表的第几格——分集多的稿子（第 100 集那种）光靠点进去数是数不出来的。
func partEntryTooltip(parts []bili.VideoPart, cur app.Item) string {
	switch {
	case len(parts) == 0:
		return "正在问这一首有哪几集（点一下看）"
	default:
		at := "还没开始放其中一集"
		for i, p := range parts {
			if p.Cid == cur.Cid {
				at = "正在放第 " + strconv.Itoa(i+1) + " 集"
				break
			}
		}
		return "共 " + strconv.Itoa(len(parts)) + " 集，" + at + "（点一下看全部）"
	}
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
		Height(min(winH*0.6, partListMaxHeight)).Children(func() {
		s.viewPartDrawerHeader(c, t)

		switch {
		case len(s.partRows) == 0:
			// 一片空白在界面上和「这一首没有分集」长得一模一样。而分集是问 B 站
			// 问来的，问不到是网络/风控的事，不是这一首没有分集——三句话说开。
			ui.Text(c, s.partDrawerWhy()).FontSize(12).
				TextColor(t.TextMuted).Padding(12)
		default:
			s.viewPartDrawerList(c, t)
		}
	})
}

// partDrawerWhy 是问不到时说的那一句。
//
// 三个「没有」要分成三句：这一首真的没有分集 / 问了但没问到 / 还在问。
// 混成一句的话，用户会对着一首永远问不出分集的歌反复重试。
func (s *Shell) partDrawerWhy() string {
	switch {
	case !s.app.PartsWorthAsking():
		// 换歌换到本地文件或音乐库了（入口本来就是从那一首的键进来的，
		// 而这一列还开着——见 `viewPartButton` 里收口的那一句）。
		return "这一首没有分集"
	case s.app.PartsErr() != nil:
		return "问不出分集：" + s.app.PartsErr().Error()
	default:
		return "正在问这一首有哪几集"
	}
}

// viewPartDrawerHeader 是顶上那一条：「分集」和共几集。
//
// 共几集**写在浮层上而不是只写在键的悬停提示里**：提示要等鼠标停上去，而列表
// 一打开用户第一眼要的就是「一共几个」——他好判断自己滑到哪里为止。
func (s *Shell) viewPartDrawerHeader(c *ui.Context, t *ui.Theme) {
	ui.Column(c).FillWidth().Children(func() {
		ui.Row(c).FillWidth().Height(partHeaderHeight).PaddingX(12).
			AlignItems(ui.Center).Children(func() {
			ui.Text(c, "分集").FontSize(14).Bold().Grow(1)
			if n := len(s.partRows); n > 0 {
				ui.Text(c, "共 "+strconv.Itoa(n)+" 集").FontSize(12).TextColor(t.TextMuted)
			}
		})
		ui.Box(c).FillWidth().Height(1).Background(t.Border)
	})
}

// viewPartDrawerList 是浮层里那一列。
func (s *Shell) viewPartDrawerList(c *ui.Context, t *ui.Theme) {
	rows := s.partRows
	s.partList.Key = func(i int) any { return rows[i].key }
	s.partList.Label = func(i int) string { return rows[i].title }

	ui.List(c, &s.partList, len(rows), func(i int) {
		s.viewPartRow(c, rows[i], t)
	}).Grow(1).FillWidth()
}

// viewPartRow 是分集列表里的一行：编号 + 标题。
//
// ⚠️ 悬停要**在 Children 之前**问：指针是在布局那一步才知道落在哪儿的
// （和 `queue_drawer.go` 的 `viewQueueRow` 同一个道理）。
func (s *Shell) viewPartRow(c *ui.Context, r partRow, t *ui.Theme) {
	row := ui.Row(c).Key(r.key).Grow(1).Height(partRowHeight).
		PaddingX(12).Gap(8).AlignItems(ui.Center).Radius(6)
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
		ui.Text(c, strconv.Itoa(r.page)).Width(partNumW).FontSize(12).
			TextColor(partRowText(t, r.active, true))
		ui.Text(c, r.title).Grow(1).FontSize(13).
			TextColor(partRowText(t, r.active, false)).SingleLine()
	})

	if row.Clicked() {
		// 选完就收：Biu 的 `onPressItem={onClose}`。
		// 收在换集**之前**——`PlayPart` 要清轨道再异步取流，那几百毫秒里这一列
		// 摆着的是正在放的那一集的空状态（还没有歌），比摆着上一集的行更让人
		// 以为换失败了。
		s.partOpen = false
		if err := s.app.PlayPart(r.cid); err != nil {
			s.addToast("换不了这一集：" + err.Error())
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
