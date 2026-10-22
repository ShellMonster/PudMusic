// 带延迟的悬停提示。**这是界面层唯一一处画 tooltip 的地方**（除了
// `widgets/hover` 里那两枚「从这一首播放」的三角形——那是我们自己加的，上游
// 没有对应物）。
//
// # 为什么不用框架的 `Element.Tooltip`
//
// 框架的时延是**包级固定的 600ms**（`mygo@v0.5.5/ui/tooltip.go:12` 的
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

// tipState 是一枚提示跨帧的状态。
type tipState struct {
	text string
	// active 是「指针在锚点上或者锚点有焦点」。
	active bool
	// activeSince / inactiveSince 是这一档开始 / 结束的时刻。
	activeSince   time.Time
	inactiveSince time.Time
	// shown 是这一帧画不画。
	shown bool
	// dismissed 是「按过它了」：指针 / 焦点离开之前不再出。
	dismissed bool
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
// （`mygo/ui/input.go:975-985,1085-1090`），而命中时最内层的可点元素才是
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
	active := anchor.Hovered() || anchor.FocusVisible()

	// 指针/焦点进出的那一刻重新计时；离开时把「按过」也清掉，再回来才能出。
	if active != st.active {
		st.active = active
		if active {
			st.activeSince = now
			st.dismissed = false
			st.inactiveSince = time.Time{}
		} else {
			st.inactiveSince = now
			st.dismissed = false
		}
	}

	// 按一下锚点就收起（上游 closeOnClick）。⚠️ 这两个查询会给元素打
	// `flagClickable`，锚点是「别的按钮里面的子元素」时不能问——见 withQuietTip。
	if watchPress && (anchor.Pressed() || anchor.Clicked()) {
		st.dismissed = true
		st.shown = false
	}

	switch {
	case st.dismissed:
		st.shown = false
	case active:
		if !st.shown {
			if wait := timing.open - now.Sub(st.activeSince); wait > 0 {
				c.After(wait)
			} else {
				st.shown = true
			}
		}
	case st.shown:
		if wait := timing.close - now.Sub(st.inactiveSince); wait > 0 {
			c.After(wait)
		} else {
			st.shown = false
		}
	}

	if !st.shown {
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
		panel := ui.Box(c).Absolute().PassThrough().Role(ui.RoleTooltip).
			MaxWidth(t.Space(80)).Padding(t.Space(1.25), t.Space(2)).
			Radius(t.Space(1.25)).
			Background(fill).TextColor(fg).FontSize(t.FontSize-1).
			Shadow(0, 2, 8, 0, ui.RGBA(0, 0, 0, 0.2))
		panel.Children(func() { ui.Text(c, text) })
		if place != nil {
			place(panel, anchor, t)
		} else {
			panel.AttachTo(anchor, ui.AnchorTop, ui.AnchorBottom).
				Margin(0, 0, t.Space(1.5), 0)
		}
		if panel.OverlayShortcut(0, ui.KeyEscape) {
			st.shown = false
			st.dismissed = true
		}
	})
	return anchor
}

// tipFrom 是给页面用的入口：页面拿到 `tipFunc`（可能为 nil——单独造这一页的
// 测试没有外壳）时走这一份，没有就退回框架那枚 600ms 的（只发生在测试里）。
func tipFrom(fn tipFunc, c *ui.Context, anchor ui.Element, text string, timing tipTiming) ui.Element {
	if fn == nil {
		return anchor.Tooltip(text)
	}
	return fn(c, anchor, text, timing)
}
