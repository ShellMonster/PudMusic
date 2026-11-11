// 播放条上的分集抽屉：Biu 的 `src/layout/playbar/left/page-list.tsx`。
//
// 一个写着「分集」的键，点开是**向上弹**的浮层（那边是
// `<Popover placement="top" offset={28}>`），头部固定「分集」两个字，下面一列
// 分集，正在放的那一集整行**淡主色底**（上游 `bg-primary/20`，见
// `playingrow.go`），点一行立刻切过去并收起——
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
	"strings"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/egoist/mygo/ui"
)

// viewPartButton 是播放条上那个「分集」键，加上它弹出来的那一列。
//
// ⚠️ **入口在「还没问到」和「问失败」时也得摆着**，判据和满屏那个把手一模一样
// （见 `viewFullParts` 的注释）：单 P 的稿子照 Biu 的 `hasMultiPart` 是没有入口
// 的，而「问不出分集：…」这句话要有一个能点开的地方才有地方显示——收掉的话它在
// 真机上是死的。只有**确知只有一集**的时候才收起来，那时候确实没有「选」的意思。
//
// ⚠️ **喜欢歌单原型之后挪在右列**（`viewPlayBarRight`）：原来它和「收藏」「赞」
// 横着并排在左列第三段（票 #219，上游 `playbar/left/index.tsx:86`），那两枚键
// 去了之后它跟着音质 / 倍速 / 下载排进右列。
//
// 而 `viewTrackActions` **第一行就 return 了**——没有歌的时候那一段一格都不画，
// 所以「没有歌就不摆」这条在这个函数里已经成立；下面那个 `HasTrack` 判是给**别的
// 调用方**留的（将来这个键要是被摆到别处去，它自己认得）。
func (s *Shell) viewPartButton(c *ui.Context, st app.State) {
	if !st.HasTrack {
		// 没歌的时候连搜索词一起收：词是「这一首的分集里找」，歌都没了词不剩。
		s.partOpen = false
		s.partQuery, s.partQuerySeen, s.partQueryBvid = "", "", ""
		s.partScrollToActive = false
		s.partBox = partPanelBox{}
		return
	}

	parts := s.app.Parts()
	cur, _ := s.app.CurrentItem()
	s.partRows = partRowsFor(parts, cur)
	if len(s.partRows) == 0 {
		// 还在问 / 问不出：这一档的框不是满列表的尺寸，量下来会把之后的分集压扁。
		s.partBox = partPanelBox{}
	}

	if !partsWorthEntry(parts, s.app.PartsWorthAsking()) {
		// 入口不摆的时候**必须把开着的那一列收掉**：否则这一刻开着的是上一首的
		// 浮层状态，等下一首多 P 的歌放起来，它会自己弹出来——用户什么都没按。
		s.partOpen = false
		s.partBox = partPanelBox{}
		return
	}
	if !s.partOpen {
		// 关着的浮层不留词：下次点开看到的永远是完整的那一列。bvid 也要在这一帧
		// 对成当前这首——否则「点开 → 换歌」那一档（下面那一条）会把一首新歌误认成
		// 「浮层开着时换过去了」，而它要守的恰恰是那一档。
		s.partQuery, s.partQuerySeen, s.partQueryBvid = "", "", cur.Bvid
		s.partBox = partPanelBox{}
	} else if cur.Bvid != s.partQueryBvid {
		// 浮层**开着**时歌自己切走了（队列自动进下一首）：旧词不能滤新一首的
		// 分集——「第二」在新稿子里可能一个都滤不出来，而列表看着像坏了。
		s.partQuery, s.partQuerySeen, s.partQueryBvid = "", "", cur.Bvid
		s.partScroll.Y = 0
		s.partBox = partPanelBox{}
	} else if s.partQuery != s.partQuerySeen {
		// 刚敲了 / 刚清了：过滤出来的那一列从第一条看起（不滚动回顶的话，
		// 停在中间的用户看到的可能是一段空白）。
		s.partQuerySeen = s.partQuery
		s.partScroll.Y = 0
	}

	// 🔴 **身份钉死**：这一枚是**逐帧问出来**的（单 P 的稿子不摆），它一出现，
	// 后面几枚的兄弟序号就都变了——而动画通道是存在身份里的（理由见
	// `playbar_keys.go`）。所以键固定是 `partButtonKey`。
	//
	// ⚠️ **图标不是装饰，是它唯一能被认出来的东西**：`ui.Icon` 对辅助技术是不可见
	// 的（`mygo/ui/svg.go`），所以名字「分集」不许省——它既是无障碍名字，也是
	// 无头 Tester 认得这个按钮的唯一凭据（`Find` 按 label 找）。
	//
	// 样子和同排的下载 / 歌词 / 队列一致：`pud.IconBtn`（36 见方、图标 18、平时 text-2、
	// 悬停 hover 底 + 正文色；原型 app.js:929 `<button class="icon-btn" data-pop="parts">`）。
	// 原来是旧的 `iconKey`（32 + 每边 7 的内边距、悬停变主色），和左右两枚对不齐。
	btn := s.withTip(c, pud.IconBtn(c, partButtonKey, "parts", "分集", pud.IconOpts{}),
		partEntryTooltip(parts, cur, s.app.PartsWorthAsking(), s.app.PartsErr()))
	if btn.Clicked() {
		s.partOpen = !s.partOpen
		if s.partOpen {
			// 刚点开：回到第一屏（词清掉了，滚动跟着回顶），并挂上一次性的
			// 「滚到正在放的那一集」——那一帧由 viewPartDrawer 消费，滑过去、
			// 居中停住；分集还没问到（比如正播着而 pagelist 在路上）时它留着，
			// 问到那一帧再滚。
			s.partQuery, s.partQuerySeen = "", ""
			s.partScroll.Y = 0
			s.partScrollToActive = true
			s.partBox = partPanelBox{}
		}
	}
	// 「点外面关掉」是框架那个浮层自己管的（`mygo/ui/base.go` 的 `popover`），
	// 所以这里只管开关本身（和 `viewRate` 同一个写法）。
	//
	// ⚠️ **Escape 那一条有条件**：框架把键先给焦点链，它们都不处理才轮到浮层
	// （`mygo/ui/input.go` 的 `routeKeys`）——焦点在一个自己吃 Esc 的控件里时，
	// 这一块关不掉（理由见 `playbar.go` 里 `viewTrackError` 那处注释）。
	//
	// ⚠️ 浮层那一层外面包一层 Guard（铁律三）：它是在**播放条这一层的布局之外**
	// 单独建的，它一崩坏掉的是整块浮层，而报错的地方在播放条上——两者对不上，
	// 极难查。
	s.pbPopover(c, btn, &s.partOpen, "分集列表", func() {
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

// partPopMaxH 是分集弹层里那一列的高度上限：集数多时这一列自己滚（`.pop` 本身没有上限，
// 原型只有 5 集；真实稿件可能上百集，所以封顶在 min(窗口高 × 0.6, 360)）。
const partPopMaxH = 360

// partPopMaxW 是分集弹层的最宽：标题常是「某某合集 第 N 集」这种长句，超出就省略。
const partPopMaxW = 360

// partSearchLabel 是弹层右上角那个搜索框的无障碍名字 / 占位字。
const partSearchLabel = "搜索分集"

// partSearchW 是那个搜索框里输入框的宽度。88 摆得下「搜索分集」四个字，
// 胶囊总宽约 146，和左边的标题合起来不把弹层撑宽。
const partSearchW = 88

// viewPartDrawer 是分集弹层里的内容（原型 `popHtml('parts')`）：
// `<h5>分集 · N 集</h5>` 加每集一个 `pop-opt`（`P{n} · {标题}`，正在放的那集 accent-text 600 + 对勾）。
// 面板本身（surface 底、1 像素描边、圆角 12、`--shadow-pop`、内边距 8、最小宽 200）由
// `pbPopover` 按 `.pop` 画。
//
// 头部是**一行**：左边标题、右边搜索框（本地过滤，不发请求）。集数多的稿子（上百集）
// 靠翻是翻不到的；「分集 · N 集」说的是**总数**，不是过滤后剩下几条——过滤中标题不跳。
func (s *Shell) viewPartDrawer(c *ui.Context) {
	_, winH := c.Size()
	if len(s.partRows) == 0 {
		// 一片空白和「这一首没有分集」长得一模一样；分集是问 B 站问来的，问不到是网络 / 风控
		// 的事——三句话说开（`partsHowItIs`）。这一支**没有搜索框**：没有东西可滤。
		pud.PopHeading(c, "分集")
		pud.PopText(c, s.partDrawerWhy())
		return
	}
	rows := partRowsMatching(s.partRows, s.partQuery)
	if s.partBox.w == 0 {
		// 量的是满列表：刚点开 / 分集刚回来时哪怕词还在，也先按全部的行摆一帧。
		rows = s.partRows
	}
	ui.Row(c).FillWidth().MaxWidth(partPopMaxW).AlignItems(ui.Center).Children(func() {
		pud.PopHeading(c, "分集 · "+strconv.Itoa(len(s.partRows))+" 集").Grow(1)
		in := pud.PopSearch(c, "part-pop:q", &s.partQuery, partSearchLabel, partSearchW)
		// Esc 收浮层：浮层把键先给焦点链（`routeKeys`），而 `TextInputBase` 不吃
		// Esc（顶栏那一处量过，`navbar_search.go` 文件头），所以 Esc 落到浮层
		// 自己的「按 Esc 关」上——不用接管。
		//
		// 回车放**第一条命中**：和「点一行」走同一个出口（`choosePartRow`）。
		// 读的词是 `k.Text()`——`OnTextKey` 的文档说它可能**比绑定值快一帧**
		// （`ui/textinput.go`），按 `s.partQuery` 滤会漏掉刚敲下的那个字。
		in.OnTextKey(func(k ui.TextKey) bool {
			if k.Key != ui.KeyEnter || k.Mods != 0 {
				return false
			}
			q := k.Text()
			if strings.TrimSpace(q) == "" {
				// 空词放还给输入框自己：框架对单行输入框的 Enter 本来有它
				// 的处理（提交 / 哔声），这儿没有「第一条」可选。
				return false
			}
			hits := partRowsMatching(s.partRows, q)
			if len(hits) == 0 {
				// 一个都滤不到：吃掉这一次回车（不让它落下去干别的事），
				// 画面上一行没换，用户看得懂「没命中」。
				return true
			}
			s.choosePartRow(hits[0])
			return true
		})
	})
	// ⚠️ **头留在被冻的那一块外面**：它不随搜索变，而把它一起套进下面这个 Column
	// 会让它的固有宽度少算 4 像素左右（实测「分集 · 3 集」被挤成两行，外框跟着窄）。
	// 头和列表加起来是外框：头不变、列表这一块冻住，外框就不动。
	body := ui.Column(c).Key("part-pop:body")
	// 量的是**布局框**：`Bounds` 带着进场那段 0.98 的缩放，量下来会把框缩掉 2%。
	if b := body.LayoutBounds(); s.partBox.measure && b.W > 0 {
		s.partBox.w, s.partBox.h = b.W, b.H
		s.partBox.measure = false
	}
	if s.partBox.w > 0 {
		body = body.Width(s.partBox.w).Height(s.partBox.h)
	}
	body.Children(func() {
		if len(rows) == 0 {
			// 整个列表都被滤光了：说清楚是搜索滤的，不是这一首没有分集。
			pud.PopText(c, "没有匹配的分集")
		}
		// 外面这一层 Column 是必需的：滚动容器直接当别的容器的下一层时，里面文字的盒子量出来是 0×0
		// （见 `viewSpaceList` 里同一条）。
		ui.Column(c).Key("part-pop:list").MaxWidth(partPopMaxW).Children(func() {
			// ⚠️ **key 里带上搜索词**：换词就换一个滚动容器，旧的进场滑动（`ScrollIntoView`
			// 那条 wheelGlide）跟着旧容器一起丢掉。只把 `partScroll.Y` 写成 0 取消不掉
			// 它——写进去的 0 和当时的位置一样时容器不认这是一次接管，滑行会继续把
			// 过滤后的列表往旧目标带（实测：点开后马上敲「第 1」，Y 从 0 被带到 37）。
			ui.Scroll(c).Key("part-pop:scroll:" + s.partQuery).TrackScroll(&s.partScroll).
				MaxHeight(min(winH*0.6, partPopMaxH)).Children(func() {
				for _, r := range rows {
					label := "P" + strconv.Itoa(r.page) + " · " + r.title
					el := pud.PopOpt(c, "part:"+r.key, label, "", r.active)
					if r.active && s.partScrollToActive {
						// 刚点开：把正在放的那一集滑到这一列的中间（`ScrollCenter` +
						// `Smooth` 是一段动画，不是一跳到位——跳的话用户看不出「是它
						// 滚过去的」，会以为本来就停在那儿）。
						//
						// ⚠️ **消费一次就完**：用户之后自己滚走时它不再拉回来；挂着
						// 而那一行还没建出来（分集在路上）时它留着，问到那一帧再滚。
						el.ScrollIntoView(ui.ScrollIntoViewOptions{Block: ui.ScrollCenter, Animation: &ui.Smooth})
						s.partScrollToActive = false
					}
					if el.Clicked() {
						s.choosePartRow(r)
					}
				}
			})
		})
	})
	if s.partBox.w == 0 && len(s.partRows) > 0 {
		// 这一帧摆的就是满列表；下一帧量它的框，然后才冻住。
		s.partBox.measure = true
	}
}

// choosePartRow 是「定了要放这一集」的那一个出口：点一行、回车第一条都走它。
//
// 选完就收：收在换集**之前**——`PlayPart` 要清轨道再异步取流，那几百毫秒里
// 摆着上一集的行更让人以为换失败了。
func (s *Shell) choosePartRow(r partRow) {
	s.partOpen = false
	s.partScrollToActive = false
	if err := s.app.PlayPart(r.cid); err != nil {
		s.addToast("换不了这一集："+err.Error(), notice.Danger)
	}
}

// partRowsMatching 按搜索词滤出那一列。纯函数：词空着原样返回，有词时标题（小写）
// 含词、或编号「12」/「P12」含词算命中；`s.partRows` 不被改。滤出来的是**新的一份**
// ——行里的 `page` 还是原来的第几集，不重新编号（过滤后「P2」还叫「P2 · 第二集」）。
func partRowsMatching(rows []partRow, query string) []partRow {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return rows
	}
	out := make([]partRow, 0, len(rows))
	for _, r := range rows {
		page := strconv.Itoa(r.page)
		if strings.Contains(strings.ToLower(r.title), q) ||
			strings.Contains(page, q) ||
			strings.Contains("p"+page, q) {
			out = append(out, r)
		}
	}
	return out
}

// partDrawerWhy 是空状态那一行说的话（问到了就不画它）。
//
// 走的是 `partsHowItIs`——和键上那个悬停提示同一个出口，理由见那一条。
func (s *Shell) partDrawerWhy() string {
	return partsHowItIs(len(s.partRows), s.app.PartsWorthAsking(), s.app.PartsErr())
}
