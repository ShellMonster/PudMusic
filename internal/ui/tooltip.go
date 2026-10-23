// 带延迟的悬停提示。**这是界面层唯一一处画 tooltip 的地方**（除了
// `widgets/hover` 里那两枚「从这一首播放」的三角形——那是我们自己加的，上游
// 没有对应物）。
//
// # 为什么不用框架的 `Element.Tooltip`
//
// 框架的时延是**包级固定的 600ms**（`mygo@v0.6.2/ui/tooltip.go:12` 的
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
package ui

import (
	"time"

	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/shadow"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
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
// （`mygo/ui/input.go:1040-1050,1150-1160`），而命中时最内层的可点元素才是
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

	// 🔴 **同一时刻只出一枚提示**（票 #245 的 E3）。
	//
	// ⚠️ **指针优先于键盘焦点**（票 #245 的评审）：焦点那一枚的提示，在**别处**
	// 有悬停提示时让位——**指针是比键盘焦点更贴近当下操作的信号**：鼠标停在
	// 「迷你」那一枚上，用户要的就是「迷你」那一句说明，而屏幕上留着「打开文件」
	// 那一句（焦点还停在那儿）是在讲一件用户当下没在做的事。
	//
	// ⚠️ **「别处」不含自己**：指针正停在焦点那一枚上时，那一枚的提示两个来源
	// 都指着它——它当然显示（而且显示的是同一枚）。
	//
	// ⚠️ **登记的是元素 ID**：一枚提示换了一个锚点就是另一枚。
	if st.hover.shown {
		s.tipHoverNow = id
	}
	if st.focus.shown && s.tipHoverBusy(id) {
		st.focus.shown = false
	}

	if !st.hover.shown && !st.focus.shown {
		return anchor
	}

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

	ui.Overlay(c, func() {
		// ⚠️ **影的值从 `widgets/shadow` 取**（`shadow.Tooltip`）：值只写一处
		// （DESIGN.md §四·五，票 #239 把这一处收进去了）。
		panel := shadow.Tooltip(ui.Box(c).Absolute().PassThrough().Role(ui.RoleTooltip).
			MaxWidth(t.Space(80)).Padding(t.Space(1.25), t.Space(2)).
			Radius(t.Space(1.25)).
			Background(fill).TextColor(fg).FontSize(t.FontSize-1), t)
		panel.Children(func() { ui.Text(c, text) })
		if place != nil {
			place(panel, anchor, t)
		} else {
			panel.AttachTo(anchor, ui.AnchorTop, ui.AnchorBottom).
				Margin(space.None, space.None, t.Space(1.5), space.None)
		}
		if panel.OverlayShortcut(0, ui.KeyEscape) {
			st.hover.shown = false
			st.focus.shown = false
			st.dismissed = true
		}
	})
	return anchor
}

// tipHoverBusy 报「**别处**有悬停着的提示」（self 是这一枚自己的元素 ID）。
//
// 🔴 **两个都要看**，因为提示是**逐个元素** build 的，而**指针悬停着的那一枚
// 不一定先被 build**：焦点所在的那一枚先被 build 时，它看不到「这一遍已经有
// 悬停提示了」。
//
// 「刚才那一遍」那一项靠的是框架的**多遍 build**（`mygo/ui/runtime.go` 的
// `for pass := 0; pass < 3`）：每一遍的开头都把上一遍登记的那一枚挪到
// `tipHoverPrev`，于是**真正留在画面上的那一遍**（最后一遍）一定看得见悬停
// 提示已经登记过了——它是在前面那遍里登记的。
//
// ⚠️ 代价是「指针**刚刚**移到别处」的那一遍，焦点那一枚的提示还亮着（十几
// 毫秒，肉眼看不见）；而「指针停着不动」的那一路从第二遍起就只剩一枚。
func (s *Shell) tipHoverBusy(self uint64) bool {
	return (s.tipHoverNow != 0 && s.tipHoverNow != self) ||
		(s.tipHoverPrev != 0 && s.tipHoverPrev != self)
}

// tipFrom 是给页面用的入口：页面拿到 `tipFunc`（可能为 nil——单独造这一页的
// 测试没有外壳）时走这一份，没有就退回框架那枚 600ms 的（只发生在测试里）。
func tipFrom(fn tipFunc, c *ui.Context, anchor ui.Element, text string, timing tipTiming) ui.Element {
	if fn == nil {
		return anchor.Tooltip(text)
	}
	return fn(c, anchor, text, timing)
}
