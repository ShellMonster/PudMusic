// 播放条左边第三段那三枚键（赞 / 收藏 / 评论）各自的**读数**（票 #701 起带数，
// 本票起收成图标正下方的一行小字——方案 A「垂直收束」）。
//
// # 这一族和「播放量那一列」是两件事
//
// 播放量那一列折成「1.2万」（`list.PlayCountText`）——它地方宽，要的是「约数」。
// 这一族折成短档位（`list.CountText`），放在图形**正下方**，宽不许吃布局。
//
// # 本票的形状：**图标在上、数字在下，上下收在同一枚键里**
//
// 上一版（缺口角标）把数字挂在图形盒子的右上角、探出图形外——用户看着是
// 「图标上贴了块补丁」。本票把它收进键内：图形还是 18、居中偏上，数字一行
// 小字压在图形正下方，**整串不探出键的左右边界**——键加高（36 → 50），宽不变。
//
// ⚠️ **数字在流里、不在角上**：它是 `Children` 里排在图形盒子**下面**的那一个
// 元素（`ui.Column` 里上下排），不是 `Attach`——所以它占键内的高度、而不占
// 键外的空间，左右也自动收在键的 padding box 里。
//
// # 🔴 键**加高**，不是加宽
//
// 数字横排那一版的病是「带数的键比不带数的宽」——第三段把歌名 / 进度条挤回去
// （票 #216 的坑：撑出轨道的那一截点不动）。本票数字在**竖直方向**收进键内，
// 键的**宽不变**（还是 36 的点击区），变的是**高**（36 → 50，图形 18 + 数字
// ~11 + 缝 3 + 上下各留一点）。
//
// # 三枚键的**共性**（所以这里有一份共享的配方）
//
//	图形 18（`iconMd`）在一个 18×18 的盒子里，数字一行排在它下面
//
// 折出来的字是 `list.CountText`（那一族的折法只有这一处），而**约束**（字号 /
// 颜色 / 排列）在 `countKeyIconOn` 这一个函数里——**三枚键共用**（赞键的图形
// 那一块是绝对定位的两层，它自己拼盒子，但**数字走同一份排版**）。
//
// # 🔴 「+」不许截：数字那格不封顶
//
// 数字**占整行宽**、随字长——折法最宽的一档「99k+」（≈22）收在 36 宽的键里
// 绰绰有余，结构上**没有给截断发生的格子**（`SingleLine`，一行字、不包折、
// 不省略）。`TestTheCountNeverLosesItsPlus` 的判据照旧：每一档单独画出来，
// 墨迹都不许比字宽窄。
package ui

import (
	"strconv"

	"github.com/ShellMonster/PudMusic/internal/ui/widgets/flat"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/list"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

// 这一族的几个尺寸。
const (
	// playbarCountFont 是数字的字号（**固定 11，不乘系统文本缩放**）。
	//
	// 🔴 **为什么这一格不跟 `scale.Font` 走**（而界面别处的字都跟）：
	//
	// 「+」是这一族的**语义**（「至少这么多」）——而**截断切的是末尾，「+」永远
	// 是最后一个字符**。数字整行宽只有 36，系统文本一放大，「99k+」那档就装
	// 不下、先切那个「+」。⇒ 字号钉死，任何 scale 下都装得下（账同上一版
	// `playbarBadgeFont` 那一栏——那时是 9，本票图形和数字上下分开了、垂直
	// 方向有地方了，读到 11，和原型「12px」那一档只差一档，而实测 11 是
	// 「读得出是数字、又不压过图形」的那一档）。
	playbarCountFont = 11

	// playbarCountKeyH 是带数那一枚键的**高**（50）：图形 18 + 数字 ~14 +
	// 缝 3 + 上下各 ~7 的余量。
	//
	// ⚠️ **它比点击区 36 高**：数字在键**里面**，所以键要吃得下「图形 + 数字」
	// 两层。50 是三枚键同高的那一档（不带数的时候键还是 36 高——见
	// `countKeyIconOn`：没有数字就只摆图形那一层，列照旧居中）。
	playbarCountKeyH = 50
)

// playbarCountText 是那个读数该显示的字。**空串 = 没有这个数**（不画）。
//
// ⚠️ **折法只有一处**（`list.CountText`）：这一族三枚键用的是**同一套**折法，
// 而它和播放量那一族的（`list.PlayCountText`）**不是同一个函数**——后者折成
// 「1.2万」，宽度差着一截。
func playbarCountText(n int64) string { return list.CountText(n) }

// countKeyLabel 是「图形 + 数字」那一枚键的无障碍名字。
//
// 数字是独立的视觉读数，按钮本身只保留动作名称，避免读屏和悬停入口被折算
// 数字撑长成一串不可扫描的标签。
func countKeyLabel(name string, _ int64) string {
	return name
}

// countKeyTip 是那一枚键的**悬停提示**（tip 是那一句本身）。
//
// ⚠️ **提示句里也要有那个数**：数字只画在画面上，指针停在别处时用户读不到它。
//
// ⚠️ **提示句要说清那个数是「整个稿件」的**（票 #701 的语义要求）：多 P 的视频
// 里放的是第 3 集，而显示的是整稿的数字——不说清的话，用户会以为那是「这一 P」
// 的。
//
// 🔴 **提示里给的是**完整数字**，不是画面上那个折过的字**（票 #701 的要求）：
//
//	画面上   「99k+」
//	提示里   「整个稿件 99999」
//
// ⇒ 而「1亿+」那一档**更是必须这样**：那一档是**夸大的近似**（对 100000 那个数
// 说「至少 1 亿」**不成立**——理由见 `widgets/list/count.go` 的文件头），所以
// **只有完整数字**说得清那一档到底是多少。
func countKeyTip(name string, n int64, tip string) string {
	if n > 0 {
		return tip + "（整个稿件 " + strconv.FormatInt(n, 10) + "）"
	}
	return tip
}

// countLine 是图形下面那一行数字。**它不占键外的空间**：排在 `Children` 里图形
// 盒子的下面（`ui.Column` 上下排），左右收在键的 padding box 里。
//
// ⚠️ **颜色是 `TextMuted`**：数字是**附属读数**，不是那一枚键的身份——图标才是。
// 它和播放条上别的次要读数（UP 名、时间）同一个色阶。
//
// ⚠️ **字号是**固定的**（`playbarCountFont`），不是 `scale.Font(c, …)`**——
// 理由见那个常量的注释（系统文本放大 125% 就会切掉那个「+」，而「+」是这一族
// 的语义）。
//
// ⚠️ **`SingleLine` 不省略**：数字一整行摆得下（最宽档 ≈22 < 36），结构上
// 「+」截不掉。
func countLine(c *ui.Context, n int64) {
	t := playbarCountText(n)
	if t == "" {
		return
	}
	p := tok.Of(c.Theme())
	typo.Mono(ui.Text(c, t)).FontSize(playbarCountFont).FontWeight(600).
		LetterSpacing(-0.2).TextColor(p.Text3).SingleLine().
		AlignSelf(ui.Center)
}

// countKeyIcon 是计数键里的图形盒子（18×18，图形居中）+ 它下面的数字。
// on 是「开着」（收藏了 / 赞了）：图形 accent-text；平时次要色，悬停正文色。
//
// ⚠️ **没有数字时还是 36 的一枚**：`countLine` 空串时一格都不画，列里只剩
// 图形——按钮的高度由父列的 `AlignItems(Center)` 收在 36 那档。
func countKeyIcon(c *ui.Context, iconName string, hovered, pressed bool, n int64, bg ui.Color) {
	countKeyIconOn(c, iconName, hovered, pressed, false, n, bg)
}

func countKeyIconOn(c *ui.Context, iconName string, hovered, pressed, on bool, n int64, bg ui.Color) {
	// ⚠️ **竖着排**：图形在上、数字在下——`ui.Column` 把它们收在同一枚键的
	// 中轴上，而不是数字漂到图形的角上。
	ui.Column(c).Gap(space.Px2).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
		ui.Box(c).Size(iconMd, iconMd).Children(func() {
			ic := ui.Icon(c, icon(iconName)).FontSize(iconMd).
				Attach(ui.AnchorCenter, ui.AnchorCenter).TextColor(pbIconColor(c, hovered, on))
			flat.PressScale(ic, pressed, false)
		})
		countLine(c, n)
	})
}
