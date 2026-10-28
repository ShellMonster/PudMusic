// 带延迟的悬停提示。**这是界面层唯一一处画 tooltip 的地方**（除了
// `widgets/hover` 里那两枚「从这一首播放」的三角形——那是我们自己加的，上游
// 没有对应物）。
//
// # 为什么不用框架的 `Element.Tooltip`
//
// 框架的时延是**包级固定的 600ms**（`mygo@v0.6.3/ui/tooltip.go:12` 的
// `tooltipDelay`），`Tooltip` / `TooltipBase` 的公开 API 里没有按调用点改的
// 参数；离开时则是下一帧立刻收。上游 Biu 用的是 HeroUI 的 `Tooltip`，两个
// props 分三档：
//
//	delay       出提示前停多久，默认 0；只有音量那一处显式是 300
//	            （`music-volume/index.tsx:119`）
//	closeDelay  指针离开后停多久收，默认 500；侧栏折叠菜单 / 顶栏动态 /
//	            新建收藏夹那几处显式是 0（`menu-item.tsx:78` 等）
//
// 所以这里自己写一份：默认 **0ms 出、500ms 收**，菜单那几处用 **0ms 收**，
// 音量的 300ms 在 `playbar_volume.go` 的 `volHoverDelay`（那是一个自绘浮层，
// 不是 `Tooltip`）。外观和框架那一份逐项一样：反色底、反色字、一层阴影、
// 摆在锚点上方居中。
//
// # 状态挂在 Shell 上，按元素 ID 记
//
// 即时模式每一帧都重建元素，所以「指针停了几毫秒、提示还开着没有」必须跨帧
// 存着。键是 `Element.ID()`（Key / 位置决定的，跨帧稳定），值只在界面线程读写。
// 页面的构造方（Shell）把 `s.tip` 递进去，页面自己不留第二份状态。
//
// # 一个提示一个 overlay，指针穿过去
//
// 面板是 `Overlay` + `PassThrough` + `RoleTooltip`：它画在最上面，但不挡指针
// （否则指针落在提示上，锚点就「离开」了，提示会自己闪掉）。面板上按 Esc 收起。
//
// ⚠️ **面板建在这一遍的**第一个**「要出提示的」那儿**，不是最后（见
// `newTipPanel`）：浮层按键的优先级是「**最后建的那个先拿到**」，提示要是最后
// 建的，它就把 Esc 抢走了——抽屉开着的时候按 Esc 收的该是**抽屉**。
//
// # 同一时刻只出一枚提示（票 #420）
//
// 提示是**逐个元素** build 的：每个带提示的元素在自己的 build 里**登记**一份，
// **这一遍 build 走完之后**才挑出该画的那一枚（见 `pickTip` / `drawTip`）。
//
// ⚠️ **为什么不能就地决定「谁画」**：**指针刚离开的那一枚先被 build**（它就在
// 按钮条的左边）——它 build 的时候还不知道右边那一枚马上要出。两枚悬停提示
// 各自 `shown`，谁也不让谁，于是两枚一起挂在画面上（用户 2026-10-25 报的：
// 「移动到播放循环按钮，再移动到下载，两个 hover 会有重叠」）。
//
// 挑的时候按三档（理由都在 `pickTip` 那里）：
//
//  1. 指针**正停着**的那一枚——它挤掉别的；
//  2. 指针**已经移开、还在收尾期里**的最近那一枚——上游 `closeDelay` 的语义；
//  3. 焦点那一枚——键盘走位来的，指针那一档在的时候它让位（票 #245 的 E3）。
//
// ⚠️ **第 1 档和第 2 档是两回事**：指针落到另一枚上时，上一枚**立刻**走（不用
// 等它那 500ms）；而指针移开、**没有新的提示来**时，上一枚**赖够那 500ms**
// 才走。删掉那 500ms 的话，扫过一排按钮时提示会一闪一闪的。
package ui

import (
	"time"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/shadow"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// 上游那三档。名字按「这个数在上游是干什么的」取，不按大小取。
const (
	// tipDelayNone 是 HeroUI `delay` 的默认值：指针一停就出提示。
	tipDelayNone = time.Duration(0)
	// tipDelayVolume 是音量那一处 `delay={300}`。播放条上的音量浮层用它
	// （`playbar_volume.go` 的 volHoverDelay），这里留着同一个数给别的提示用。
	tipDelayVolume = 300 * time.Millisecond
	// tipDelayClose 是 HeroUI `closeDelay` 的默认值：离开之后停 500ms 再收。
	tipDelayClose = 500 * time.Millisecond
)

// tipTiming 是一枚提示的两档时延。
type tipTiming struct {
	open  time.Duration
	close time.Duration
}

var (
	// tipDefault 是上游没显式写 props 的那些：0 出、500 收。
	tipDefault = tipTiming{open: tipDelayNone, close: tipDelayClose}
	// tipMenu 是上游显式 `closeDelay={0}` 的那几处（侧栏折叠菜单、顶栏动态、
	// 新建收藏夹）：0 出、离开立刻收。
	tipMenu = tipTiming{open: tipDelayNone, close: tipDelayNone}
)

// tipFunc 是页面拿到的那份提示入口（Shell 把 `(*Shell).tip` 递进来）。
type tipFunc func(c *ui.Context, anchor ui.Element, text string, timing tipTiming) ui.Element

// 🔴 **焦点那一档的时延是 0 出、0 收**，和悬停那一档不一样。
//
// 为什么收也要 0：悬停那一档的 500ms 等的是「**指针**移开之后」——指针移开了，
// 提示还挂一会儿，免得用户手一抖划过就看不见了。而键盘走位路过一枚控件时，
// 焦点是**移开**的：等 500ms 的话，走过三枚控件就留下三条提示叠在屏幕右上角
// （票 #245 的 E3，用户看到的是三句挤在一起的、读不出哪句对哪枚图标的字）。
//
// 键盘走位是**一格一格**的，每一格停留的时间本来就短，提示还赖着不走的话，
// 叠起来的那几行就是纯噪声——而键盘用户恰恰是最需要那些提示的人。
const (
	tipFocusOpen  = time.Duration(0)
	tipFocusClose = time.Duration(0)
)

// tipState 是一枚提示跨帧的状态。
//
// 🔴 **两个来源分开记**，不是合并成一个 `active`：
//
//   - `hover` 是「指针在锚点上」那一档：走上游 HeroUI 的时延（0 出、500 收）。
//   - `focus` 是「锚点有键盘焦点」那一档：0 出、0 收（见上面那两个常数）。
//
// 分开是因为两件事：**出提示的时机不一样**（指针停着才出，焦点一到就出），
// **收提示的时机也不一样**（指针移开还赖 500ms，焦点一走立刻收）。合成一档的
// 时候，「键盘路过三枚控件留下三条提示」就是这么来的。
type tipState struct {
	text string
	// hover 是「指针在锚点上」那一档的状态机。
	hover tipPhase
	// focus 是「锚点有键盘焦点」那一档的状态机。
	focus tipPhase
	// dismissed 是「按过它了」：指针 / 焦点离开之前不再出。
	dismissed bool
}

// tipPhase 是一枚提示的一个来源的状态机。
type tipPhase struct {
	// active 是「这个来源现在指着这一枚」。
	active bool
	// activeSince / inactiveSince 是这一档开始 / 结束的时刻。
	activeSince   time.Time
	inactiveSince time.Time
	// shown 是这一档这一帧画不画。
	shown bool
}

// step 把这一档的状态机推进一步，并报「这一档是不是**刚刚**失去了 active」。
//
// 报这个是因为「按过它了就收起」那一档要靠它复位（上游的 closeOnClick）：
// 指针或焦点离开之后再来，这一枚才重新出提示。
//
// open / close 是这一档的两档时延。
//
// 🔴 **`c.After(wait)` 那两句不能少**（票 #245 的评审）：延时到了之后得**有人画
// 那一帧**，提示才出得来 / 收得掉。界面在**空闲时是没有帧的**——没人安排的话，
// 指针一离开，提示就赖在屏幕上直到下一次碰巧的帧（而 300ms 那一档则永远出不来）。
//
// 少了它的症状是「静默」：仓库自己的验收命令全绿，因为那些用例 `Advance` 之后
// 又**手动画了一帧**，把缺的那一帧补上了。
func (p *tipPhase) step(c *ui.Context, active bool, now time.Time, open, close time.Duration) bool {
	lost := false
	if active != p.active {
		p.active = active
		if active {
			p.activeSince = now
			p.inactiveSince = time.Time{}
		} else {
			p.inactiveSince = now
			lost = true
		}
	}
	switch {
	case p.active:
		if !p.shown {
			if wait := open - now.Sub(p.activeSince); wait > 0 {
				// 还没到出提示的时刻：这一帧不画，等到了再画。
				// ⚠️ **这一句就是那一帧的安排**（理由见上面那段）。
				c.After(wait)
				return lost
			}
			p.shown = true
		}
	case p.shown:
		if wait := close - now.Sub(p.inactiveSince); wait > 0 {
			// 同上：收提示的那一帧也要有人安排。
			c.After(wait)
			return lost
		}
		p.shown = false
	}
	return lost
}

// withTip 是默认那一档（0 出、500 收）。界面上绝大多数提示走它。
func (s *Shell) withTip(c *ui.Context, anchor ui.Element, text string) ui.Element {
	return s.tip(c, anchor, text, tipDefault)
}

// withMenuTip 是菜单那一档（0 出、离开立刻收）：侧栏折叠菜单、顶栏动态、
// 新建收藏夹——上游那几处显式写了 `closeDelay={0}`。
func (s *Shell) withMenuTip(c *ui.Context, anchor ui.Element, text string) ui.Element {
	return s.tip(c, anchor, text, tipMenu)
}

// withQuietTip 是不看按下的那一支：锚点是**别的可点元素里面的一个子元素**
// 时用它（头像那一格里面的画法）。
//
// ⚠️ 理由：`Pressed()` / `Clicked()` 给元素打 `flagClickable`
// （`mygo/ui/input.go:1042-1052,1152-1162`），而命中时最内层的可点元素才是
// target。给「菜单按钮里面的子元素」打上这个标志，点下去的目标就从按钮变成了
// 子元素——实测头像下拉直接不开（`TestAvatarMenuHasBiuSixItems`）。这种锚点
// 只跟悬停/离开走，按下由外面那个真按钮自己处理。
func (s *Shell) withQuietTip(c *ui.Context, anchor ui.Element, text string) ui.Element {
	return s.tipAt(c, anchor, text, tipDefault, nil, false)
}

// tip 画一枚带延迟的提示，返回锚点本身（好接在链式调用里）。
//
// 调用方拿到的仍然是 anchor，所以 `s.tip(c, e, text, tipDefault).Clicked()`
// 问的是**锚点**有没有被按。
func (s *Shell) tip(c *ui.Context, anchor ui.Element, text string, timing tipTiming) ui.Element {
	return s.tipAt(c, anchor, text, timing, nil, true)
}

// tipAt 是 tip 加「提示摆哪儿」和「看不看按下」。place 为 nil 时摆在锚点上方
// 居中（框架那一份的位置）；侧栏折叠态要摆在右边，就在那儿传一个自己
// AttachTo 的闭包。watchPress 为 false 见 withQuietTip。
func (s *Shell) tipAt(c *ui.Context, anchor ui.Element, text string, timing tipTiming,
	place func(panel, anchor ui.Element, t *ui.Theme), watchPress bool) ui.Element {
	if text == "" {
		return anchor
	}
	// 读屏软件看到的是描述；框架那份 Tooltip 也这么做（描述为空才写，这里
	// 直接写——我们所有调用点的描述要么为空，要么就是同一句话）。
	anchor = anchor.Description(text)

	if s.tips == nil {
		s.tips = map[uint64]*tipState{}
	}
	id := anchor.ID()
	st := s.tips[id]
	if st == nil || st.text != text {
		st = &tipState{text: text}
		s.tips[id] = st
	}

	now := c.Now()
	hovered := anchor.Hovered()
	focused := anchor.FocusVisible()

	// 悬停那一档：走上游的时延（默认 0 出、500 收，菜单那几处 0/0）。
	hoverLost := st.hover.step(c, hovered, now, timing.open, timing.close)
	// 焦点那一档：**0 出、0 收**（理由见 tipFocusOpen 那两个常数的注释）。
	focusLost := st.focus.step(c, focused, now, tipFocusOpen, tipFocusClose)

	// 指针或焦点离开之后，「按过它了就收起」那一档复位——再回来才重新出提示。
	if hoverLost || focusLost {
		st.dismissed = false
	}

	// 按一下锚点就收起（上游 closeOnClick）。⚠️ 这两个查询会给元素打
	// `flagClickable`，锚点是「别的按钮里面的子元素」时不能问——见 withQuietTip。
	if watchPress && (anchor.Pressed() || anchor.Clicked()) {
		st.dismissed = true
		st.hover.shown = false
		st.focus.shown = false
	}

	if st.dismissed {
		st.hover.shown = false
		st.focus.shown = false
	}

	// 🔴 **这里只登记，不决定画哪一枚**（票 #420）：画哪一枚要等这一遍 build
	// 走完才知道。
	//
	// ⚠️ **不能就地决定**：**指针刚离开的那一枚先被 build**（它就在按钮条的左
	// 边）——它 build 的时候还不知道右边那一枚马上要出。两枚悬停提示各自
	// `shown`，就地画就是两枚一起挂在画面上。理由见文件头上那一节。
	//
	// 🔴 **面板在这一遍的**第一个**「要出提示的」那儿建**（不是最后建，也不是
	// 在「该画的那一枚」那儿建）：
	//
	//   - **不在最后建**：面板是**浮层**，而浮层按键的优先级是「**最后建的那个
	//     先拿到**」（`mygo/ui/input.go:882` 的 `r.serial >= top`，serial 是节点
	//     建出来的次序）。提示要是最后建的，它就把 Esc 抢走了——抽屉开着的时候
	//     按 Esc 收的该是**抽屉**，不是那一枚提示（`queue_drawer.go` 的
	//     `back.OverlayShortcut(0, ui.KeyEscape)`）。
	//   - **不在「该画的那一枚」那儿建**：那一枚要等整遍 build 走完才知道（见
	//     `pickTip`）。所以建在「第一个要出提示的」那儿，**内容**（那句话、摆在
	//     哪儿）留到最后填（见 `drawTip`）——面板在浮层里的位置和按键优先级都跟
	//     从前一样。
	if (st.hover.shown || st.focus.shown) && !s.tipPanel.Valid() {
		s.tipPanel = s.newTipPanel(c)
	}
	s.tipCandidates = append(s.tipCandidates, tipCandidate{
		anchor: anchor,
		text:   text,
		st:     st,
		place:  place,
	})
	return anchor
}

// newTipPanel 建提示面板（**内容还空着**）。
//
// ⚠️ **它建在 `tipAt` 里（第一个要出提示的那一处）**，理由见那里那一段。
// 内容（那句话、摆在哪儿）由 `drawTip` 在整遍 build 之后填。
func (s *Shell) newTipPanel(c *ui.Context) ui.Element {
	t := c.Theme()
	// 反色兜底照框架 `Theme.inverse()`（`mygo/ui/theme.go:71-79`）：那一对留空
	// 时提示会是一块透明的底。
	fill, fg := t.Inverse, t.InverseText
	if fill == ui.Transparent {
		fill = t.Text
	}
	if fg == ui.Transparent {
		fg = t.Background
	}
	var panel ui.Element
	ui.Overlay(c, func() {
		// ⚠️ **影的值从 `widgets/shadow` 取**（`shadow.Tooltip`）：值只写一处
		// （DESIGN.md §四·五，票 #239 把这一处收进去了）。
		panel = shadow.Tooltip(ui.Box(c).Absolute().PassThrough().Role(ui.RoleTooltip).
			MaxWidth(t.Space(80)).Padding(t.Space(1.25), t.Space(2)).
			Radius(t.Space(1.25)).
			Background(fill).TextColor(fg).FontSize(t.FontSize-1), t)
	})
	return panel
}

// tipCandidate 是这一遍 build 里登记下来的一份「有可能要画的提示」。
//
// ⚠️ **不是「要画的那些」**：每一个带提示的元素都会登记一份，最后只挑一枚
// （见 `pickTip`）。
type tipCandidate struct {
	// anchor 是锚点本身——面板要挂在它上面。
	anchor ui.Element
	// text 是提示那句话。
	text string
	// st 是这一枚跨帧的状态（`tipState`）。
	st *tipState
	// place 是「提示摆哪儿」（nil = 摆在锚点上方居中，框架那一份的位置）。
	place func(panel, anchor ui.Element, t *ui.Theme)
}

// pickTip 挑出该画的那一枚，返回它在那一份里的下标（没有就 -1）。
//
// 🔴 **同一时刻只出一枚提示**（票 #420）。三档，按先后：
//
//  1. **指针正停着的那一枚**。它是「用户当下指着的东西」，别的给它让位。
//  2. **指针已经移开、还在收尾期里的最近那一枚**。上游 `closeDelay=500` 的
//     语义：指针移开后提示还赖一会儿（手一抖划过时不至于看不见）。
//  3. **焦点那一枚**。键盘走位来的；指针那一档在的时候它让位（票 #245 的 E3）。
//
// ⚠️ **第 1 档和第 2 档分开，是这一票的要害**：指针落到另一枚上时，上一枚
// **立刻**走（不用等它那 500ms）；指针移开、**没有新的提示来**时，上一枚
// **赖够那 500ms** 才走。合成一档的话，要么两枚叠着，要么扫过一排按钮时
// 提示一闪一闪。
//
// ⚠️ **第 2 档取「最近的那一枚」**：指针从 A 挪到 B 再挪走，该赖着的是 B
// （它是最后被指到的那一枚）。
//
// ⚠️ **第 1 档取「第一个」**：同一时刻**只可能有一枚**被指着，除非一枚套着
// 另一枚（头像那一格就是：外面是菜单按钮，里面那一格自己带提示）。那时里面
// 那一枚更贴切——而它**先被登记**（外面那一枚的提示是它整个子树建完之后才
// 登记的）。
func pickTip(list []tipCandidate) int {
	hoverNow, linger, focus := -1, -1, -1
	for i := range list {
		st := list[i].st
		switch {
		case st.hover.shown && st.hover.active:
			// 指针正停在它上面。
			if hoverNow < 0 {
				hoverNow = i
			}
		case st.hover.shown:
			// 指针已经移开，它还在收尾期里。
			if linger < 0 || st.hover.activeSince.After(list[linger].st.hover.activeSince) {
				linger = i
			}
		}
		if st.focus.shown && focus < 0 {
			focus = i
		}
	}
	switch {
	case hoverNow >= 0:
		return hoverNow
	case linger >= 0:
		return linger
	default:
		return focus
	}
}

// drawTip 填面板、挑出该画的那一枚。
//
// ⚠️ **排在整遍 build 之后**（见 `Shell.View` 和 `shelltest_test.go` 的
// `withView`）：挑的那一下要看得见这一遍登记的**全部**候选。
//
// ⚠️ **面板是 `tipAt` 建好的**（见那一段）：这里只填内容（那句话、摆在哪儿）。
// 建在最后的话，浮层按键的优先级会反过来（见 `tipAt` 里那一段）。
//
// ⚠️ **画完就把候选清空**：这样同一个函数可以被两处调用（生产路和测试路）而
// 不会画两遍——第二遍看到的是空的那一份。
func (s *Shell) drawTip(c *ui.Context) {
	list := s.tipCandidates
	s.tipCandidates = s.tipCandidates[:0]
	panel := s.tipPanel
	s.tipPanel = ui.Element{}
	i := pickTip(list)
	if i < 0 || !panel.Valid() {
		return
	}
	p := list[i]
	st := p.st

	t := c.Theme()
	panel.Children(func() { ui.Text(c, p.text) })
	if p.place != nil {
		p.place(panel, p.anchor, t)
	} else {
		panel.AttachTo(p.anchor, ui.AnchorTop, ui.AnchorBottom).
			Margin(space.None, space.None, t.Space(1.5), space.None)
	}
	if panel.OverlayShortcut(0, ui.KeyEscape) {
		st.hover.shown = false
		st.focus.shown = false
		st.dismissed = true
	}
}

// tipFrom 是给页面用的入口：页面拿到 `tipFunc`（可能为 nil——单独造这一页的
// 测试没有外壳）时走这一份，没有就退回框架那枚 600ms 的（只发生在测试里）。
func tipFrom(fn tipFunc, c *ui.Context, anchor ui.Element, text string, timing tipTiming) ui.Element {
	if fn == nil {
		return anchor.Tooltip(text)
	}
	return fn(c, anchor, text, timing)
}
