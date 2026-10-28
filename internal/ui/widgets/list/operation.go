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
//
// 队列抽屉的行尾那一枚也走这里（`internal/ui/queue_drawer.go` 的
// `list.OperationAccent`）——**同一个「⋯」只应该有一个尺寸**（它曾经有三个：
// 这一档 32×32、抽屉头那两枚 32×32、队列抽屉行尾那一枚 44×28，票 #249）。
package list

import (
	"github.com/ShellMonster/PudMusic/internal/ui/iconset"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/flat"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scrim"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// moreIcon 是那枚「⋯」。
//
// ⚠️ **从 `iconset` 取而**不是**从界面层拿常量：这个包在界面层**下面**，
// 反过来 import 是个环（见 `iconset` 文件头）。同一个文件在界面层的常量叫
// `iconMore`。
//
// 🔴 **它是实心那一版，而且名字来自 `iconset.MoreVertical`**（票 BMG-253 /
// 报告第 17 条）：线框那一版（`more-2-line`）的三个点是 24 见方里**半径 1.5**
// 的圆——画成 16 像素就是**三个 2 像素的斑点**，而它旁边就是那个 12 像素的时长
// 数字（也是淡灰），两样东西看起来一样重，用户读不出「这是一枚菜单键」。
//
// 实心那一版（`more-2-fill`）的点半径是 **2**（16 像素下 2.7 像素）：三个实心点
// 一眼就是一个**图形**，而不是三个像标点的碎点。
//
// ⚠️ **名字必须和界面层那两枚一样**（队列抽屉行尾、收藏夹工具栏）：四枚各写
// 一份的话，全应用就有**两种「⋯」并存**，同一页上并排两枚时用户分不出哪一枚
// 是同一个动作（票 BMG-253 的评审点名过）。
var moreIcon = iconset.Must(iconset.MoreVertical)

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

// operationTip 是那枚「⋯」的悬停提示：这一行里唯一的操作入口，得有一句话说明
// 它是什么（票 BMG-253 / 报告第 17 条——它原先**一句提示都没有**，用户只能靠
// 试）。
//
// ⚠️ **它是一句常数**：每帧每一行都现拼一次的话就是每帧十几次字符串拼接
// （票 #313 那一类，见 `rowkey.Memo`）。而这一句不认行——「这一首」就够了。
const operationTip = "这一首的操作"

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
//
// ⚠️ **悬停提示是框架那一份**（`Tooltip`，600ms 出）：这个包在界面层**下面**，
// 拿不到界面层那份提示（`internal/ui` 的 `withTip`，0ms 出 / 500ms 收）——
// 而列表里另外那一枚带提示的控件（UP 名，见 `titleline.go`）走的也是框架那一份，
// 两处一致。
func Operation(c *ui.Context, label string, color ui.Color, build func(m *ui.Menu)) ui.Element {
	return flat.IconButton(c, moreIcon, label, operationIconSize).
		Padding(operationHitPad, operationHitPad).
		TextColor(color).
		Tooltip(operationTip).
		Menu(build)
}

// OperationAccent 是 Operation 加上「**悬停时图形变主色**」。
//
// 上游 HeroUI 的 `IconButton` 是 `hover:text-primary`
// （`src/components/icon-button/index.tsx:16`）——悬停时图形**从前景色变成主色**，
// 不只是底下多一层淡底。播放条上那些图标键早就是这一档（`internal/ui` 的
// `iconKey`），而这一枚原来还是「只有淡底、图形不变色」的那一档——用户从别的
// 图标键挪到它上面会觉得「这一枚不亮」（票 #249）。
//
// # 为什么它**不接字色**
//
// `Operation` 接一个 color，是因为列表行的字色是**那一行自己算出来的**
// （`card.go` 的 `rowColors`），得抄一份给它。而队列抽屉那一行（`internal/ui` 的
// `queue_drawer.go`）的字色是**继承**来的——行自己已经定过了（正在放那一行是主色
// 压深的那一档）。所以这里不接：接了就得在两处各抄一份字色，而抄的那一份迟早
// 和行自己算出来的那一份对不上。
//
// ⚠️ **尺寸和 `Operation` 一样**（图标 16 + 每边 8 = 32×32）：同一个「⋯」在仓库里
// 只应该有一个尺寸——它曾经有三个（这一档 32×32、抽屉头那两枚 32×32、队列抽屉
// 行尾那一枚 44×28）。
func OperationAccent(c *ui.Context, label string, build func(m *ui.Menu)) ui.Element {
	return flat.IconButtonAccent(c, moreIcon, label, operationIconSize, false).
		Padding(operationHitPad, operationHitPad).
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
				Tooltip(operationTip).
				Menu(build)
		})
}
