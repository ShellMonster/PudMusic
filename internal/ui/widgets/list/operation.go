// 行尾（格子上角）那枚「⋯」。
//
// # ⚠️ **它和右键菜单共用同一份菜单构造**
//
// 上游那份是 `music-list-item/operation.tsx`：一枚 `RiMore2Line` 按钮，点开一个
// `Popover`，里面是 `Listbox`，而**那一份 items 就是右键菜单那一份**
// （`music-list-item/index.tsx:165` 传的是同一个 `menus` 数组）。上游只写了一份
// 菜单，两条路各开一次。
//
// 我们这边走的是框架的**菜单按钮**（`Element.Menu`）：点它弹出来的是系统菜单，
// 和右键这一行弹出来的是同一个系统菜单，两条路走的是**同一个函数**
// （`menu.go` 的 `buildSongMenu`）。写第二份菜单的话，两边迟早会对不上——而那种
// 错在界面上看不出来（两边都能点，只是点的不是同一件事）。
//
// # 为什么在列表包这一层
//
// 网格那一档的格子是 `grid.go` 画的、列表那一档的行是 `card.go` 画的，两边都要有
// 这一枚；而 `internal/ui` 的「稍后再看 / 历史记录」那两页自己也画行（走的是另一
// 套，见 `internal/ui/library_rows.go` 的文件头），它们也要同一枚——所以这一份
// 放在**两边都够得着**的 `widgets/list` 里，界面层 import 它。
package list

import (
	"github.com/ShellMonster/Biu_Go/internal/ui/iconset"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/flat"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scrim"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// moreIcon 是那枚「⋯」。
//
// ⚠️ **从 `iconset` 取而**不是**从界面层拿常量：这个包在界面层**下面**，
// 反过来 import 是个环（见 `iconset` 文件头）。同一个文件在界面层的常量叫
// `iconMore`。
//
// 上游是 `RiMore2Line`（`music-list-item/operation.tsx:22`），16px。
var moreIcon = iconset.Must("System/more-2-line.svg")

const (
	// OperationColWidth 是行尾「操作」那一列的宽。
	//
	// ⚠️ **表头照它留同样宽的空位**（`header.go` 末尾那一个）：上游的表头最后那一格
	// 是 `w-8`（`music-list-item/header.tsx:34`），摆的就是这一列。两边不一样宽的
	// 话，表头和行会差那么几像素——而那种差在画面上只是「表头歪了一点」。
	OperationColWidth = 32

	// operationIconSize 是那枚「⋯」画出来多大。上游 16（`operation.tsx:22`）。
	operationIconSize = 16

	// operationHitPad 是这枚「⋯」四周的内边距。
	//
	// ⚠️ **它算出来正好是 32 见方**（8 + 16 + 8），和 `OperationColWidth` 一样，
	// 和上游那枚 `size="sm"` 的图标键（HeroUI 的 sm 是 `w-8 h-8`）一样。
	// 扁平按钮那份配方默认给的是 6/14（横向 14），那一枚是 44 宽——塞进 32 那一列
	// 就会把它顶出去，而顶出去之后表头和行又对不上了。
	operationHitPad = space.Px8

	// operationHitInset 是网格那一档里这枚「⋯」离格子边的距离。
	//
	// ⚠️ **它压在封面上**，所以给的是一点边距而不是贴着角：贴着角的话那三个点正好
	// 落在封面的边沿上，抗锯齿把它切掉一半，看起来像「少了一个点」。
	operationHitInset = space.Px2
)

// OperationLabel 是那枚「⋯」给无障碍（和 Tester）用的名字。
//
// ⚠️ **带上歌名**：读屏软件读的是标签而不是画出来的符号，只说「操作」的话用户听到
// 的是一句没头没尾的话；而 Tester 靠它认出**哪一行**的那一枚（`Find` 认的是
// 逐字相等，`HasText` 是子串——后者在几千行里分不出是哪一行）。
func OperationLabel(title string) string { return "「" + title + "」的操作" }

// Operation 画行尾那枚「⋯」。
//
// build 是**这一行的菜单怎么建**——和右键这一行走的是同一个函数（`menu.go` 的
// `buildSongMenu`；界面层那两页——稍后再看 / 历史记录——是它们自己的
// `laterMenu` / `historyMenu`，因为它们多两项「移除 / 删除」）。
//
// color 是这枚「⋯」的字色：它跟着这一行歌名的颜色走（`card.go` 的 `rowColors`），
// 所以「正在放」那一行的这枚「⋯」也是主色的。
//
// ⚠️ **不再有「主色底上的反色字」那一档**（票 #197 去掉的那一层）：选中行和没选中的
// 行长得一样，这一枚也是。
//
// label 是给无障碍（和 Tester）用的名字，**带上歌名**：读屏软件读的是标签而不是
// 画出来的符号，只说「操作」的话用户听到的是一句没头没尾的话；Tester 也靠它认出
// **哪一行**的那一枚。
func Operation(c *ui.Context, label string, color ui.Color, build func(m *ui.Menu)) ui.Element {
	return flat.IconButton(c, moreIcon, label, operationIconSize).
		Padding(operationHitPad, operationHitPad).
		TextColor(color).
		Menu(build)
}

// OperationCorner 画网格那一档里、**格子右上角**的那枚「⋯」。
//
// ⚠️ **它压在封面上，所以自带一块半透明的深底、字是白的**：封面是什么颜色都行，
// 而那三个点要是跟着主题走（浅色下是深灰），浅色封面上就看不见了——而「看不见的
// 操作入口」等于没有这一枚。
//
// ⚠️ **压在封面上还意味着它会挡住封面的一角**：所以它只在网格那一档出现，列表那一
// 档的「⋯」在行尾（`Operation`），不占封面。
func OperationCorner(c *ui.Context, label string, build func(m *ui.Menu)) ui.Element {
	return ui.Box(c).Absolute().Top(operationHitInset).Right(operationHitInset).
		Radius(c.Theme().Radius).Background(ui.RGBA(0, 0, 0, scrim.Overlay)).
		Children(func() {
			flat.IconButton(c, moreIcon, label, operationIconSize).
				Padding(operationHitPad, operationHitPad).
				TextColor(ui.RGBA(255, 255, 255, scrim.OnScrim)).
				Menu(build)
		})
}
