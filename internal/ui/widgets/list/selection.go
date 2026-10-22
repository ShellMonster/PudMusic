// 「选中」这一层：选中的一行看起来该是什么样。
//
// # 这一层为什么单独一个文件
//
// 票 #197（2026-10-22 拍板）把「选中」这件事整个翻了过来：**点一行不再整行刷
// 实心主色底**。上游压根没有这一层——点一行不改变它的样子，而我们原来点一下就
// 是一整块绿。
//
// 翻过来之后，「选中」这一层只剩两件事：
//
//  1. **盖掉框架那一块实心主色底**。框架的 `ui.List` 给选中的行铺一整块主色底
//     （`mygo/ui/table.go` 的 node.Selected：`e.Background(t.Accent)`），那一层
//     在库里，我们改不动——所以选中的行自己刷一层底色把它盖掉。
//  2. **键盘走到哪一行，那一行有一圈焦点环**。焦点环是保留的（DESIGN.md §七：
//     「焦点环：保留，不要为了对齐去掉」），只是它落在**那一行**上，而不是框架
//     默认画在整张列表上的那一圈。
//
// # 「正在放」是另一件事
//
// 「正在放」那一行的主色高亮**不在这一层**：它是 flat 的（主色当字色，见
// `card.go` 的 `rowColors`），跟着声音走，不跟着光标走。选中这一层只管「选中」。
//
// # 哪些调用方
//
// `widgets/list` 的列表那一行（`card.go`）和网格那一格（`grid.go`），以及
// `internal/ui` 里自己画行的两页（稍后再看 / 历史记录）——它们说的都是同一件事，
// 所以**走同一个出口**，别各处写一份（理由同 `card.go` 文件头那句「同一个应用里
// 两套列表写着两种说法，用户会以为是两个意思」）。
package list

import (
	"github.com/egoist/mygo/ui"
)

// SelectedRow 把「选中」这一层加到这一行上。
//
// **没选中就原样还回去**：一个字节都不多画——选中的行和没选中的行长得**一个像素
// 都不差**（票 #197：点一行不再整行刷绿）。
//
// 选中时做两件事（理由见文件头）：
//
//  1. 刷一层底色盖掉框架那一块实心主色（`rowCover`）；
//  2. 给它一圈焦点环（`rowFocusRing`）——**ring 由调用方给**。
//
// # ring 为什么由调用方给
//
// 「那一行是不是键盘走到的那一行」要问两件事：**这张列表此刻有没有键盘焦点**，以及
// **上一次输入是键盘还是指针**。这两件事都在**列表**那一层（不是行那一层），
// 而不同的调用方拿着的列表状态不是同一份（`widgets/list` 的 `List` 一份，
// `internal/ui` 的「稍后再看 / 历史记录」另一份），所以由调用方算好再传进来。
//
// ⚠️ **ring 也要 `selected`**：没选中的行不该有环。
func SelectedRow(row ui.Element, t *ui.Theme, selected, hovered, ring bool) ui.Element {
	if !selected {
		return row
	}
	row = row.Background(rowCover(t, hovered))
	if ring {
		row = rowFocusRing(row, t)
	}
	return row
}

// KeyboardMode 记「现在是键盘在管，还是指针在管」。
//
// # 为什么不用框架那个 `focusVisible`
//
// 框架有 `FocusVisible()`（`mygo/ui/input.go` 的 `rt.focusVisible`），它的意思是
// 「焦点是**从键盘来的**」——但它**只被 Tab 设回来**：指针按一下会把它清掉
// （`rt.focusVisible = false`），而**方向键不会设回来**。
//
// 于是就有了这么一个洞：**鼠标点过一行之后再用方向键走位，那一行什么指示都没有**
// ——键盘在管了，屏幕上却一声不响。这比改动之前还差（那时实心主色底跟着键盘走）。
//
// 所以这一份自己记：**来一个键就记「键盘在管」，来一次指针按下就记「指针在管」**。
//
// # 它是怎么知道「来了一按键」的
//
// `Element.HandleInput`（`mygo/ui/handler.go`）：元素有键盘焦点时的按键、以及指针
// 在它身上时的按下，都交给那个回调。**返回 false 是刻意的**——自己不吃这个事件，
// 引擎照常处理（方向键挪游标、点击选行、Tab 换焦点都还在）。
//
// ⚠️ **不要改成「有焦点就画环」**：那会让鼠标点过之后那一行也挂着环，而上游点一行
// 是不改变它的样子的。
type KeyboardMode struct {
	// keyboard 报「现在是键盘在管」。
	keyboard bool

	// pointer 记「指针刚按下」：这一下的选中是它挪的（不是键盘）。
	pointer bool

	// lastSel 是上一帧看到的选中行，started 是已经看过一帧了。
	//
	// ⚠️ **零值不是「没选中」**：没选中是 -1，而零值是 0——所以这里要一个
	// `started` 才分得出「这一帧从 -1 变成 0」和「一直就是 0」。
	lastSel int
	started bool
}

// Input 是给框架的输入回调（`Element.HandleInput`）。
//
// ⚠️ **永远返回 false**：这个回调只是「听一听」，不吃事件。
//
// ⚠️ **按键看 `InputKeyUp` 也算数**：方向键被框架自己的快捷键登记截走了
// （`mygo/ui/list.go` 的 `f.navigate` 里 `o.Shortcut(0, k)` 登记了方向键，于是
// `rt.claimed(k)` 为真，`InputKeyDown` 到不了这里）——只有**松开**那一下到得了。
// 所以两样都收。
func (k *KeyboardMode) Input(ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputKeyDown:
		k.keyboard = true
	case ui.InputKeyUp:
		// ⚠️ `KeyUnknown` 是修饰键那类（平台只报「修饰键变了」，不报它自己）。
		// 那一下不算「键盘在管」——用户按了 ⇧ 而没按别的键，不该冒出一圈环。
		if ev.Key != ui.KeyUnknown {
			k.keyboard = true
		}
	case ui.InputPointerDown:
		k.keyboard = false
		k.pointer = true
	}
	return false
}

// Moved 报「选中挪到了 sel，而这一挪是键盘挪的」。
//
// # 为什么还要看「选中挪没挪」
//
// 方向键被框架自己的快捷键登记截走了（见 `Input` 那一条），所以「**按住方向键
// 不放**」那一路：键是按了、游标也一格一格挪了，但按键事件只在**松开**时才有。
// 这一支补的就是那一下——游标每挪一格就算一次「键盘在管」。
//
// ⚠️ **指针挪的那一下不算**：指针按下时选中也会挪（点哪行选哪行），而那一下是
// 指针挪的，不能算成键盘挪的——否则「点一行」之后那一行就挂上了环。
//
// ⚠️ **每一帧都要调**（画之前）：第一帧之前 `started` 是假，返回假。
func (k *KeyboardMode) Moved(sel int) bool {
	if !k.started {
		k.started, k.lastSel = true, sel
		return false
	}
	if sel == k.lastSel {
		return false
	}
	k.lastSel = sel
	byPointer := k.pointer
	k.pointer = false
	if byPointer {
		return false
	}
	k.keyboard = true
	return true
}

// On 报「此刻是键盘在管」。
func (k *KeyboardMode) On() bool { return k.keyboard }

// Focused 报这张列表此刻有没有键盘焦点。
//
// # 为什么问的是**列表**这个元素
//
// 焦点在框架里落在**列表**上（`mygo/ui/list.go` 的 buildList：列表有游标时
// `e.Focusable()`），方向键挪的是那个游标（`*ListState.Selected`）。而行的那些
// 按钮（「⋯」）能接焦点——那种情况下焦点**不在**列表上，键盘也就挪不动游标，
// 不该画环。
//
// ⚠️ **用 `Focused` 而不是 `FocusWithin`**：后者把「列表的某个后代有焦点」也算进来，
// 于是焦点落在行尾那枚「⋯」上的时候，行上也会挂着一圈环——而那时候键盘挪不动
// 游标。
func Focused(c *ui.Context, state *ui.ListState) bool {
	return c.Resolve(state.Handle).Focused()
}

// rowCover 是「选中那一行」刷在行身上的底色。
//
// # 盖的是什么色
//
// **页面底色**（`t.Background`）。列表直接摆在内容区里，内容区就是页面底色
// （`internal/ui/theme.go` 的那一层，`chromebg_test.go` 的
// `TestTheChromeSharesOneBackground` 钉着它）——所以盖上去之后，选中的行和旁边
// 没选中的行**一个像素都不差**，看不出有人动过手。
//
// ⚠️ **悬停时换成 `t.SurfaceHover`**：没选中的行被指针压住时，框架刷的就是那个
// 色（`mygo/ui/list.go` 的 styleFn：`w.bg = t.SurfaceHover`）。选中这一行也得跟
// 它一样，否则「指针停在选中行上」和「停在别的行上」是两种反馈。
//
// ⚠️ **别改成主色**：那就是「实心主色底」，票面说的正是去掉它。
func rowCover(t *ui.Theme, hovered bool) ui.Color {
	if hovered {
		return t.SurfaceHover
	}
	return t.Background
}

// rowFocusRing 是「键盘走到哪一行」那一圈环。
//
// # 为什么自己画
//
// 框架自己那一圈是**画在整张列表上**的（`ringShown`），不是画在行上；票面要的是
// 「键盘走到哪一行都要有可见焦点指示」——指示得落在**那一行**上。
//
// ⚠️ **环往里收 3 像素**：画在行沿上的话会盖到隔壁那一行（两行紧挨着），而框架
// 画行焦点环时也是收这 3 像素（`mygo/ui/list.go` 的 `FocusRing(Rect{r.X + 3, …})`）。
func rowFocusRing(row ui.Element, t *ui.Theme) ui.Element {
	return row.DrawOver(func(p *ui.Painter, r ui.Rect) {
		p.FocusRing(ui.Rect{X: r.X + 3, Y: r.Y + 3, W: r.W - 6, H: r.H - 6},
			[4]float32{t.Radius, t.Radius, t.Radius, t.Radius})
	})
}
