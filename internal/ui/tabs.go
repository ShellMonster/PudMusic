// 主 Tab 的样子：**一条灰底轨道 + 一块白色滑块**，切换时滑块滑过去。
//
// # 数值的出处
//
// 上游是 HeroUI 的 `<Tabs variant="solid">`（`pages/music-recommend/index.tsx:263`、
// `pages/search/index.tsx:27`、`pages/user-profile/index.tsx:136`、
// `pages/settings/index.tsx:124`）。默认那一套在 `@heroui/theme@2.4.25` 的
// `components/tabs.ts`：
//
//   - 轨道（tabList）：`p-1`（4）+ `gap-2`（8）+ `bg-default-100`（浅色 #f4f4f5）；
//   - 滑块（cursor）：`bg-white`（浅色）、`dark:bg-default`（#3f3f46，zinc-700）；
//   - 动效：`transition-[left,top,width,height]`，250ms `ease-out`；
//   - 高度：`md` 的 Tab 是 `h-8`（32）+ 上下各 4 = **40**；`lg` 是 `h-9`（36）
//   - 上下各 4 = **44**（和 DESIGN.md「Tab」与「动效」两节一致）。
//
// 首页那一排是 `size="lg"`（44），搜索 / 设置 / UP 空间是默认的 `md`（40）——
// 上游那四处就是这么分的。
//
// ⚠️ **动效那一行是上游的，我们没抄**：滑块走的是**弹簧**（`motion.TabSlide`），
// 理由是「打断时接着走」，那是定时长曲线给不了的。详见下面 `tabSlide` 那段。
//
// # 为什么滑块不是子控件
//
// 它要**压在轨道上、文字下**。子控件的画法在文字之后，铺上去就把字盖住了；
// 框架的 `Element.Draw` 正好是「底色之后、子控件之前」，所以轨道是 List 自己
// 的底色，滑块在那里面画。
//
// 滑块的位置取**上一帧量到的选中格**（`LayoutBounds`），再相对**轨道自己的
// 盒子**算，不用窗口坐标：设置页的 Tab 会跟着内容滚，用窗口坐标的话每一次滚动
// 都会被当成「目标变了」，滑块会追着轨道跑一截。
//
// # 减少动态效果
//
// 动画走框架的 `AnimateTo`：系统开着「减少动态效果」时它直接把值给到目标
// （`mygo/ui/motion.go:322`），第一帧就在终点。这里不自己判偏好，也不另写一份
// 过渡——两份判断迟早会有一份忘了听系统设置。
//
// # 次级排序不在这里
//
// 「主 Tab 和次级排序不是同一个样子」（票 #138）：主 Tab 是这一套轨道 + 滑块，
// 次级那三处（歌词来源、歌词原文/译文、搜索结果排序）保持框架 `ui.Tabs` 的
// **下划线**，不退化成同一种。
//
// # 它不是「被选中的选项」那一档
//
// ⚠️ **主 Tab 是导航，不是选项**：它切的是「在哪一页」，而设置页那些**选项**
// （主题三档、窗口关闭两档、下拉项）选的是「哪一个值」——后者在应用里**只有
// 一种画法**（实心主色底 + `t.AccentText` 字，`flatSolid`，票 #239 / 审计 F-14）。
//
// 主 Tab 的选中由**滑块**说（上游 HeroUI 的 `Tabs variant="solid"` 就是灰轨 +
// 滑块，`components/tabs.ts`），不改：把它改成实心主色块就是**偏离上游**，而且
// 一排 Tab 里飘一块绿比滑块响得多。
package ui

import (
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/flat"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/motion"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/egoist/mygo/ui"
)

// tabSize 是主 Tab 的两档尺寸，对应上游的 `size="md"` / `size="lg"`。
//
// 三处高度是一起定的，别单独改一个：
//   - track 是整条轨道（含上下各 4 的内边距）；
//   - block 是每个 Tab / 滑块的盒子；
//   - font 是标签的字号（md 上游 `text-small` = 14，lg `text-medium` = 16）。
type tabSize struct {
	track float32
	block float32
	font  float32
}

var (
	tabMD = tabSize{track: 40, block: 32, font: 14}
	tabLG = tabSize{track: 44, block: 36, font: 16}
)

// tabSlide 是滑块那一下的动画：**一条接近临界阻尼的弹簧**（ζ=0.9923），
// 不是定时长曲线。
//
// 出处、阻尼为什么从 26 上调到 32、以及那几个数实算是多少，都写在
// `internal/ui/widgets/motion` 的 `TabSlide` 那里——值只写一处。
//
// ⚠️ **上游那一档是 250ms `ease-out`**（`@heroui/theme@2.4.25` 的
// `components/tabs.ts`），我们**故意不抄**：曲线中途换目标会从当前值重新起速
// （顿一下），弹簧保留速度——连点三个 Tab 时滑块是接着走的。
var tabSlide = motion.TabSlide

// mainTabs 画一排主 Tab。selected 是当前选中的下标，size 决定 40 / 44 那一档。
//
// 点击、左右方向键、Home/End、焦点跟着选中走，仍然是框架 `TabsBase` 那一套；
// 我们只换画法。
//
// ⚠️ **选中那一格不再加粗**：字重一变，格子的宽度就变，滑块既要滑位置又要改
// 宽度，还会带着后面的 Tab 整体挪一下。上游的 `tabContent` 只换字色
// （`text-default-500` → `text-foreground`），所以这里也只换字色。
//
// ⚠️ **悬停反馈只铺在没选中的格子上**：上游是
// `data-[hover-unselected=true]:opacity-disabled`，选中那一格没有这一层。铺上
// 去的话，指针停在刚点过的那一格上，白色滑块就被压成灰的了。
func mainTabs(c *ui.Context, selected *int, size tabSize, labels ...string) ui.Element {
	t := c.Theme()
	tabs := ui.TabsBase(c, selected, len(labels))

	// 每块 Tab 的 Element 留到画滑块时量位置。
	blocks := make([]ui.Element, 0, len(labels))

	tabs.List.
		Gap(t.Space(2)).
		Padding(t.Space(1)).
		Height(size.track).
		Radius(t.Radius).
		Background(trackColor(t)).
		Children(func() {
			for i, label := range labels {
				block := tabs.Tab(i).
					Height(size.block).
					Padding(t.Space(1), t.Space(3)).
					Radius(t.Radius)
				if i == *selected {
					block = block.TextColor(t.Text)
				} else {
					block = flat.Feel(c, block.TextColor(t.TextMuted))
				}
				block.Children(func() {
					ui.Text(c, label).SingleLine().FontSize(scale.Font(c, size.font))
				})
				blocks = append(blocks, block)
			}
		})

	// 滑块：目标取选中那一格**上一帧**的盒子，转成相对轨道的坐标。
	//
	// 第一帧还没有量过（LayoutBounds 是空的），这一帧就不画——否则要先从
	// (0,0) 长出来，开窗时看见滑块凭空滑一下。等下一帧量到了再建动画通道，
	// `AnimateTo` 第一次拿到的目标就是终点，不会再补一段动画。
	if len(labels) == 0 || *selected < 0 || *selected >= len(blocks) {
		return tabs.List
	}
	sel := *selected
	block := blocks[sel].LayoutBounds()
	track := tabs.List.LayoutBounds()
	if block.W <= 0 || block.H <= 0 {
		// 这一帧还量不到（第一次画这一页）。🔴 **要主动要下一帧**：这时候
		// 没有任何动画在跑，界面画完这一帧就安静了，滑块要等到下一次鼠标
		// 移动才出现。
		c.AnimationFrame()
		return tabs.List
	}
	x := tabs.List.AnimateTo("tab-slide-x", block.X-track.X, tabSlide)
	y := tabs.List.AnimateTo("tab-slide-y", block.Y-track.Y, tabSlide)
	w := tabs.List.AnimateTo("tab-slide-w", block.W, tabSlide)
	h := tabs.List.AnimateTo("tab-slide-h", block.H, tabSlide)

	fill := sliderColor(t)
	radius := t.Radius
	tabs.List.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(ui.Rect{X: r.X + x, Y: r.Y + y, W: w, H: h}, fill, radius)
	})
	return tabs.List
}

// trackColor 是那条灰底轨道的颜色。
//
// 上游 `bg-default-100`：浅色是 zinc-100（#f4f4f5），深色是**换过来的** zinc-900
// （#18181b，和页面底色几乎一样）。我们的 `t.Surface` 在浅色下正好是 #f4f4f5，
// 深色下是 #27272a——跟着用户自己挑的底色走，比照抄一个写死的 #18181b 可靠。
func trackColor(t *ui.Theme) ui.Color {
	return t.Surface
}

// sliderColor 是滑块的颜色。
//
// 浅色就是**白色**（上游 `bg-background`，也就是用户挑的页面底色）；深色上游
// 是 `dark:bg-default` = zinc-700（#3f3f46），比轨道亮一档。主题里最接近那一档
// 的是 `SurfacePressed`（默认 #3c3c41），而且它跟着底色一起平移，不会在自定义
// 底色上跳出来。
func sliderColor(t *ui.Theme) ui.Color {
	if t.Dark {
		return t.SurfacePressed
	}
	return t.Background
}
