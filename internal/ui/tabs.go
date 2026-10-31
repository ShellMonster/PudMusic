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
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/flat"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
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

// tabSlide 是滑块那一下的动画。
//
// ⚠️ **首页那一排改成曲线了**（票 #706）：切 Tab 的内容那一块走**横向滑动**，
// 而用户要的是「顶部与底部动画同步进行」——**同起同落**。
//
// 而弹簧**做不到同起同落**：这一条弹簧走 453ms 才收手（实测，见下面那张表），
// 而内容那一侧走 200ms ⇒ 两边**不同步**（指示器还在走，内容已经停住了）。
//
// ⇒ **两边用同一个数**：`homePagerDuration` + `motion.Unified`。
//
// ⚠️ **「打断时接着走」那条好处没有丢**：框架的**曲线**分支被打断时也是**从
// 当前值接着走**（`mygo/ui/motion.go` 的 `channel.retarget`：曲线那支把 `from`
// 设成**当前值**），只是它「重新起速」而不是「保留速度」——而那一档的取舍
// 见 DESIGN.md §五《主 Tab 滑块为什么是弹簧》那一节。
//
// ⚠️ **别的三处主 Tab（设置页 / 搜索页 / UP 空间）不走这一条**：它们没有
// 「底下也在动」的那一档内容，而「切了 Tab 但底下不动」是**另一种**语义
// （见下面 `mainTabs` 的注释）。
var tabSlide = motion.TabSlide

// tabSlideSynced 是**首页那一排**滑块的动画：和内容那一块同起同落。
//
// ⚠️ **它是包级变量，不能每帧现造**：`motion.Unified` 是闭包，每帧调一次就是
// 每帧一个分配（每帧分配是本仓库唯一的性能判据）。
var tabSlideSynced = homePagerAnim

// mainTabs 画一排主 Tab。selected 是当前选中的下标，size 决定 40 / 44 那一档。
//
// 点击、左右方向键、Home/End、焦点跟着选中走，仍然是框架 `TabsBase` 那一套；
// 我们只换画法。
//
// ⚠️ **synced 是「这一排下面的内容也在动」那一档**：滑块走**和内容同一个**
// 曲线和时长，于是两者**同起同落**（票 #706，首页那一排就是它）。
// 别的三处（设置页 / 搜索页 / UP 空间）传 false——它们底下没有「跟着一起动」
// 的那一屏，而**切了 Tab 但底下不动**是另一种语义（见上面 `tabSlide` 的
// 注释）。
//
// ⚠️ **选中那一格不再加粗**：字重一变，格子的宽度就变，滑块既要滑位置又要改
// 宽度，还会带着后面的 Tab 整体挪一下。上游的 `tabContent` 只换字色
// （`text-default-500` → `text-foreground`），所以这里也只换字色。
//
// ⚠️ **悬停反馈只铺在没选中的格子上**：上游是
// `data-[hover-unselected=true]:opacity-disabled`，选中那一格没有这一层。铺上
// 去的话，指针停在刚点过的那一格上，白色滑块就被压成灰的了。
func mainTabs(c *ui.Context, selected *int, size tabSize, labels ...string) ui.Element {
	return mainTabsSynced(c, selected, size, false, labels...)
}

// mainTabsSynced 是 mainTabs 加一个「和底下同步」那一档。
//
// ⚠️ **单独一个函数而不是加一个参数**：加参数会改掉**四处**调用点，而其中
// 三处传的是 false——那三处每次都要写一个 `false`，而那个 `false` 的含义
// （「底下没有跟着动的东西」）不是从调用点看得出来的。
func mainTabsSynced(c *ui.Context, selected *int, size tabSize, synced bool, labels ...string) ui.Element {
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
	// ⚠️ **动画参数**：首页那一排要和底下的内容**同起同落**（票 #706），
	// 所以走 `tabSlideSynced`（和内容同一个曲线 + 同一个时长）；别的三处走
	// 弹簧（`tabSlide`）——理由见上面那两个变量的注释。
	slide := tabSlide
	if synced {
		slide = tabSlideSynced
	}
	if block.W <= 0 || block.H <= 0 {
		// 这一帧还量不到（第一次画这一页）。🔴 **要主动要下一帧**：这时候
		// 没有任何动画在跑，界面画完这一帧就安静了，滑块要等到下一次鼠标
		// 移动才出现。
		//
		// ⚠️ **这一句不能改成 `p.AnimationFrame()`**（票 #392 的那一批里，
		// 别处都改了，就这一处没改——**理由不是「忘了」，是它要的东西不同**）：
		// 局部重绘（`mygo/ui/runtime.go` 的 `repaintFrame`）**只重画、不重建**，
		// 于是**不重排**——而这一句要的是「**再排一遍**好量到那一格的盒子」。
		//
		// 局部重绘那一帧里，元素树还是上一帧那一份，量到的还是**上一帧**的
		// 盒子（空的）；于是这一句要的那一帧永远不会来，滑块就一直不出现。
		//
		// 而它**也不值得**改：这一句是**每次切页一次**（不是每帧），省下来的
		// 是一次整窗重建——四张界面里最贵的那一屏的一次，约一毫秒。
		c.AnimationFrame()
		return tabs.List
	}
	x := tabs.List.AnimateTo("tab-slide-x", block.X-track.X, slide)
	y := tabs.List.AnimateTo("tab-slide-y", block.Y-track.Y, slide)
	w := tabs.List.AnimateTo("tab-slide-w", block.W, slide)
	h := tabs.List.AnimateTo("tab-slide-h", block.H, slide)

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
