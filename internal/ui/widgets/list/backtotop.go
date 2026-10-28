// 「回到顶部」那一枚。
//
// # ⚠️ **那四个数照上游抄，不要重新量**
//
// Biu `src/components/scroll-container/index.tsx` 的那一枚是
// `className="bg-default/50 fixed right-10 bottom-[120px] z-20 shadow-xl shadow-black/30"`，
// 出现的条件是 `setShowBackToTop((viewport?.scrollTop ?? 0) > 400)`。于是：
//
//	触发阈值 400（像素）	距右 40（`right-10` 是 2.5rem）
//	距下 120（`bottom-[120px]`，躲开播放条）	圆角全圆（`radius="full"`）
//
// **重新量一遍只会得到另一个数**，而用户是在同一个应用里从一页走到另一页的——
// 上一页的按钮在右边 40，下一页在右边 44，那就是「两个页面不是一套东西」。
package list

import (
	"github.com/ShellMonster/PudMusic/internal/ui/iconset"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/flat"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/shadow"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// backTopPx 是滚过多少像素才出现回顶按钮。
//
// **400 是死数，照抄** Biu `src/components/scroll-container/index.tsx`。
const backTopPx = 400

// 回顶按钮摆在哪儿。Biu 是 `fixed right-10 bottom-[120px]`：右边 40（`right-10`
// 是 2.5rem），离底 120（躲开播放条）。我们照抄这两个数。
const (
	backTopRight  = 40
	backTopBottom = 120
	// backTopRadius 是按钮的圆角：Biu 的 `radius="full"` 是药丸形，圆角给到
	// 比按钮高度大就等于它。
	backTopRadius = 999

	// backTopSize 是那一枚的点击区边长，backTopGlyph 是里面那个箭头的边长。
	//
	// 上游是 `<Button isIconOnly radius="full"><RiArrowUpSLine size={20}/></Button>`
	// ——`size` 默认那一档是 40，图标 20，每边留 10。
	backTopSize  = 40
	backTopGlyph = 20
)

// BackToTopIcon 是回顶那一枚里的向上箭头（上游 `RiArrowUpLine`），也是**这份
// 资源的唯一出处**。
//
// ⚠️ **界面层登记的那个名字直接取它**（`internal/ui/icons.go` 的
// `iconToTop = list.BackToTopIcon`）。两处各写一遍文件名的话，改一边不会有任何
// 编译错误——而画面上就出现两种「回到顶部」，谁也看不出来是哪一次改错的。
// 这个包在界面层**下面**（见 `iconset` 文件头），所以常量只能住在这里、界面层
// 过来取，不能反过来。
const BackToTopIcon = "Arrows/arrow-up-line.svg"

// backToTopIcon 是上面那个名字解析出来的图（`iconset` 按名字缓存，同一个名字
// 永远是同一个指针）。
var backToTopIcon = iconset.Must(BackToTopIcon)

// BackToTop 画「回到顶部」那一枚：滚过 400 像素才出现。
//
// offset 是**这一屏滚过了多少像素**——每一页给的是**当前画着的那一个列表**的那
// 一份（`ui.ScrollState` 的 Y，或者行数乘行高）。给错了的话，按钮会在一个没滚过
// 的页面上冒出来。
//
// top 是「回到顶上」要做的事：各页各接（列表那一档是 `List.ScrollTo`，分页那一层
// 是 `ScrollTop`，滚动容器那一档是把 `ScrollState.Y` 归零）。
//
// ⚠️ **这一份是唯一的实现**：这一枚原先在两个地方各写了一份（`widgets/list` 的
// 分页那一层，和界面层那两页自己的分页列表），两份的数一样、代码不一样——
// 改一份就有一页对不上。
func BackToTop(c *ui.Context, offset float32, top func()) {
	if offset <= backTopPx {
		return
	}
	t := c.Theme()
	// Overlay 把按钮浮在整窗之上（不占布局），Biu 的 `fixed` 就是这个意思。
	// Radius 给一个大数就是 Biu 的 `radius="full"`（药丸形）。
	ui.Overlay(c, func() {
		//
		// 一枚圆形图标键：`flat` 的配方（无描边）+ `radius="full"`（上游那一枚
		// 就是 `rounded-full`），里面是 `RiArrowUpLine`。`Label` 仍是「返回顶部」
		// ——图标对辅助技术是不可见的，而它同时也是无头 Tester 认得这一枚的
		// 唯一凭据。
		//
		// ⚠️ **底色和阴影留着**（上游自己也有 `bg-default/50` + `shadow-xl`）：
		// 这一枚浮在列表内容**上面**，去掉底之后箭头会压在某一行歌名上，读不清
		// 也看不出那儿有个按钮。`flat` 去掉的是「常驻的控件底色 + 一圈描边」，
		// 不是「浮层不能有自己的底」。
		btn := flat.IconButton(c, backToTopIcon, "返回顶部", backTopGlyph).
			Padding(space.None, space.None).Size(backTopSize, backTopSize).TextColor(t.Text)
		if btn.Absolute().
			Right(backTopRight).Bottom(backTopBottom).
			Radius(backTopRadius).Background(t.SurfaceHover).
			Clicked() {
			top()
		}
		// ⚠️ **影子在 `Clicked()` 之后装**：那一圈影是**外面**的（`Shadow` 只画在
		// 盒子外面），而 `Absolute` 那一支是这一格的位置——两件事互不相干，装的
		// 先后也一样。放在后面只是因为读的时候「先摆位置，再加影」顺一点。
		//
		// 数值照上游 `shadow-xl shadow-black/30`（`components/scroll-container/
		// index.tsx:82`）：**30% 黑**、两档（`0 20px 25px -5px` 与
		// `0 8px 10px -6px`）。原来这里是 `0 2 6 0 18%`——那是我们自己的一档，
		// 比上游淡得多，而这一枚是**浮在内容上面**的按钮：它和底下那一行歌名之间
		// 就靠这圈影分界，影太淡时它看着像印在歌名上的一个箭头。
		shadow.TailwindXLBlack30(btn)
	})
}
