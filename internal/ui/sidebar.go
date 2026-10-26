// 侧栏：品牌 + 默认菜单 + 两组收藏夹，右缘一条能拖的把手，底下能折叠。
//
// 结构照 Biu 的 `src/layout/side/index.tsx`：Brand / 可滚的内容区 /
// 折叠按钮 / 右缘的拖拽把手。三个尺寸（Biu 的 `COLLAPSED_WIDTH`、`MIN_WIDTH`、
// `MAX_WIDTH`）和 200ms 的过渡全部照抄，数字在 app 包里（见 app/settings.go
// 的 ClampSideMenuWidth），因为设置读进来也要钳同一套。
package ui

import (
	"runtime"

	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/platform"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/motion"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

const (
	// sideGrip 是右缘拖拽把手多宽。Biu 是 `w-2`，也就是 8。
	//
	// ⚠️ 别把它加宽去迁就别的判断：8 宽正是「Pressed() 在这里不成立」的原因
	// （见 sideTrackDrag 的注释）。MyGo 自己的可拖分隔条也是 6~10 这个量级
	// （`ui/split.go` 的 grip）。
	sideGrip = 8

	// sideWidthTransition 是折叠、展开和拖完松手时的过渡时长。Biu 的
	// `transition-[width] duration-200`，逐字抄。
	//
	// ⚠️ **时长抄上游，曲线不抄**：上游那一档是 Tailwind 默认的 `ease-out`，
	// 我们走全应用的**统一曲线**（`motion.Unified`，理由见 DESIGN.md §五）。
	sideWidthTransition = 200 * time.Millisecond

	// sideWidthKey 是宽度动画在 MyGo 里的键。同一个元素上同一个键的动画值
	// 跨帧续着走，换键就是换一条动画。
	sideWidthKey = "side-width"

	// sideCollapseHeight 是底下那个折叠按钮多高。Biu 是 `py-1`（8+8 加一行
	// 字），这里取 28。
	sideCollapseHeight = 28

	// sideDividerW / sideDividerA 是右缘那条分隔线的宽度和浓度。
	//
	// 照 Biu 的 `border-divider/30 border-r-1`（`side/index.tsx:145`）：
	// 1 像素、透明度 30%。**不能照抄 `t.Border` 原色**——深色下那道线
	// （票 #172 之后是 15% 白叠出来的 `#3b3b3d`）压在 `#17181a` 上是一道亮线，
	// 比上游那道明显得多（flatbutton.go 的文件头记的就是这件事：同一个色在深浅
	// 两套里的观感完全不同）。
	sideDividerW = 1
	sideDividerA = 0.3

	// sideRowHeight 是侧栏菜单里一行的高。
	//
	// 上游那一行是 HeroUI 的 Button **md**：40（`h-10`）——`MenuItem` 没有指定
	// size，而 HeroUI 的默认就是 md（`biu/src/components/menu/menu-item.tsx`）。
	// 我们原来是 6 + 15.3 + 6 ≈ **28**：指针要瞄得准才按得中，而上游那一列
	// 是 40（票 #164 的验收第二条）。
	//
	// ⚠️ **它和 `sideIconCell` 是同一个数，不是巧合**：那一行的高度就是那一格
	// 图标格的高度（上游折叠态那个 `h-10 w-10` 的图标格，和展开态那枚坐在
	// 40 高的按钮里的图标，是同一个 40）。留着两个名字是因为它们说的是两件事：
	// 一个是**行**多高，一个是**格子**多大。
	sideRowHeight = sideIconCell

	// sideIconSize 是菜单项那枚图标的边长。上游 `<Icon size={18}/>`
	// （`menu-item.tsx` 的 `iconContent`）。
	sideIconSize = 18

	// sideIconCell 是**图标格子**的边长：展开态和折叠态都是它。
	//
	// 上游折叠态那一项摆的是一个 `h-10 w-10` 的 Avatar（图标是它的 fallback），
	// 展开态那枚图标则坐在 40 高的按钮里——两边占的是**同一个 40 的格子**，
	// 所以折叠 / 展开时图标不会左右跳。
	sideIconCell = 40

	// sideBrandLogo 是左上角那个 logo 的边长。上游
	// `<LogoIcon className="h-10 w-10" />`（`biu/src/layout/side/logo/index.tsx`）。
	sideBrandLogo = 40
)

// viewSide 是左侧栏。
//
// ⚠️ 必须自己拉满高：外层 Row 默认把子元素**居中**跨轴摆放
// （`Align` 默认不是 Stretch），不写 `AlignSelf(Stretch)` 的话这一栏会按
// 内容高度浮在中间。
func (s *Shell) viewSide(c *ui.Context) {
	st := s.app.SettingsSnapshot()
	// 折叠时画 72，展开时画钳过的宽度。设置里那个数在这一层才说了算。
	target := sideTargetWidth(st)
	// ⚠️ **画出来的那一档**和设置里那个开关**不是同一件事**：拖动的时候看的是
	// 画出来的宽度（上游的 `isCollapsedVisual`），理由见 `sideCollapsedVisual`。
	collapsed := s.sideCollapsedVisual(st)

	side := ui.Row(c).Key("side")
	// 🔴 **方向键的入口在这一层**（票 #245 的 E4）：焦点在侧栏的哪一项上时按 ↓，
	// 就把焦点交给内容区。
	//
	// ⚠️ **为什么绑在容器上而不是每一项上**：容器是**焦点链上的祖先**，所以
	// 焦点在侧栏里的**任何**一处（菜单项、收藏夹、折叠键），这一下都能被它接住
	// ——而焦点链是**从内往外**派发的（`mygo/ui/input.go` 的 `routeKey`），所以
	// **焦点在收藏夹列表上时，方向键先被那个列表接走**（列表要靠方向键挪游标），
	// 走不到这里。
	//
	// ⚠️ **焦点在内容区时更不会误触发**：内容区和侧栏是兄弟，不在一条焦点链上。
	//
	// ⚠️ **副作用**：焦点在侧栏时 ↓ 不再滚侧栏了。那一档滚动靠的是框架的
	// `scrollKey`（方向键在**焦点链上没有任何控件吃**时才轮到它），而现在侧栏
	// 吃了方向键。侧栏要滚的话用滚轮——那一栏本来就窄，滚轮是它主要的方式。
	side.Bind(&s.sideHandle)
	if side.Shortcut(0, ui.KeyDown) {
		s.focusContent()
	}
	// 拖动中不过渡（Biu 的 `transition-none`），松手和折叠才走 200ms。
	//
	// ⚠️ 拖动那一支必须先 `Jump`：动画的值还停在上一段动画的终点，不先钉住
	// 的话，宽度会先从那儿跳到手指的位置，看起来是「一碰就弹一下」。
	var w float32
	if s.sideDragging {
		w = side.Jump(sideWidthKey, float32(s.sideWidth))
	} else {
		w = side.AnimateTo(sideWidthKey, float32(target), ui.Animation{
			Duration: sideWidthTransition,
			Ease:     motion.Unified,
		})
	}

	// 🔴 底色取 **`Background` 而不是 `Surface`**，右缘一条 1px 的分隔线。
	//
	// Biu 的侧栏**没有背景类**（`biu/src/layout/side/index.tsx:144-145` 只有
	// `border-divider/30 … border-r-1`），底色和内容区同源，靠**那条线**分界。
	// 我们原来取 `Surface`：浅色下只差 `#f4f4f5` / `#ffffff` 一档，看不出来；
	// **深色下差的是 `#27272a` / `#18181b`**——深浅两个通道各自跳一级，
	// 于是侧栏、内容区、播放条在深色下裂成**三条带**（票 #127 的头一条）。
	//
	// 三处底色统一到 `t.Background` 之后，这一栏的边界就全靠这条线了——
	// 去掉它，侧栏和内容区在浅色下会**完全一样**（两个都是 `#ffffff`），
	// 于是「统一底色」就变成了「抹掉层次」，那正是这一票要避开的反面。
	side.Width(w).AlignSelf(ui.Stretch).Background(s.sidebarBackground(c.Theme())).
		BorderWidth(0, sideDividerW, 0, 0).
		BorderColor(c.Theme().Border.Alpha(sideDividerA)).
		Children(func() {
			// ⚠️ FillHeight 不能省：Row 是把子元素**垂直居中**摆的（MyGo 的
			// `docs/ui/layout.md`：「Row places them from left to right and
			// centers them vertically」），不拉满高这一栏就按内容高度浮在中间，
			// 底下空出一大块，滚动区也就没高度了。
			ui.Column(c).Key("side-body").Grow(1).FillHeight().Children(func() {
				s.viewSideBrand(c, collapsed)
				// 菜单和收藏夹一起滚。Biu 也是这么分的（ScrollContainer 包住
				// DefaultMenus + Collection），侧栏窄的时候不至于把折叠按钮顶出去。
				sideScroll := ui.Scroll(c).Key("side-scroll").Grow(1).FillWidth()
				sideScroll.Children(func() {
					s.viewSideMenu(c, st, collapsed)
					s.viewFavorites(c, st, collapsed)
				})
				s.trackScrollbar(sideScroll)
				s.viewSideCollapse(c, st, collapsed)
			})

			grip := ui.Column(c).Key("side-grip").FillHeight().Width(sideGrip).
				Cursor(ui.CursorResizeEW)

			// 读拖动要在 Children 之后：元素建好了，它的盒子才有意义。
			s.sideTrackDrag(side, grip, target)
		})
}

// sideTargetWidth 是设置里那个宽度这一帧该画多宽。
//
// 折叠时是 72（不看存的那个数），展开时钳在 160~480。
func sideTargetWidth(st app.Settings) float64 {
	if st.SideMenuCollapsed {
		return app.CollapsedSideMenuWidth
	}
	return app.ClampSideMenuWidth(st.SideMenuWidth)
}

// sideCollapsedVisual 是这一帧**画出来**的那一档（折叠 / 展开）。
//
// 上游是 `isCollapsedVisual`（`biu/src/layout/side/index.tsx:55`）：
// `isDragging ? renderWidth < MIN_WIDTH : sideMenuCollapsed`——**拖动的时候
// 看的是画出来的宽度，不是设置里那个开关**。
//
// ⚠️ **不跟着走的话，拖到 160 以下的那一段里菜单还是展开的样子**：侧栏被拖到
// 72，而菜单那一列摆的还是名字——**名字被截成「推…」「本…」「下…」**
// （上游那一段摆的是图标格，所以它塞得进 72）。
//
// 而这一票把菜单换成了图标格之后，不跟上的话是**另一种坏**：那一列的内容
// （图标格 40 + 缝 8 + 名字 + 内边距）比 63 宽，**把侧栏的内容撑出去**
// （实测那一列量到 143，而整条侧栏量到 152）——比截字更糟。
//
// 现在跟上游一样：拖动的那一段里菜单也换成图标格，侧栏真的折到 72
// （`TestDraggingTheGripBelowTheMinWidthCollapsesTheSidebar` 量到 72）。
func (s *Shell) sideCollapsedVisual(st app.Settings) bool {
	if s.sideDragging {
		return s.sideWidth < app.MinSideMenuWidth
	}
	return st.SideMenuCollapsed
}

// sideDragWidth 是拖动中的那一套算法，逐字照 Biu 的 `computeWidth`
// （`biu/src/layout/side/index.tsx:87-95`）。返回三样东西，缺一样就不对：
//
//   - **view**：画出来的宽。拖到 160 以下直接变 72，所以拖窄时侧栏是「折叠着
//     被继续拖」的样子，而不是卡在 140 不动。
//   - **persist**：存下去的宽。它下限是 160 而不是 view 那个 72——存 72 的话，
//     拖回右边时起点就是折叠宽度，永远展不开了。
//   - **collapsed**：落盘时 `sideMenuCollapsed` 该是什么。
//
// 上限 480 钳在 persist 之前：拖到 600 松手存 600，下次一打开侧栏把整个
// 窗口吃掉，内容区一点不剩。
func sideDragWidth(raw float64) (view, persist float64, collapsed bool) {
	v := min(max(raw, app.CollapsedSideMenuWidth), app.MaxSideMenuWidth)
	persist = max(v, app.MinSideMenuWidth)
	if v < app.MinSideMenuWidth {
		return app.CollapsedSideMenuWidth, persist, true
	}
	return v, persist, false
}

// sideTrackDrag 是把把手上按着的那一段读出来，更新侧栏宽度。
//
// ⚠️ **「按着没有」用 `Dragged()`，不用 `Pressed()`，更不能用 `Dragging()`。**
// 三个是三件事，用错哪一个都会以「能拖、就是拖不动」或者「完全拖不动」的
// 形式混过去：
//
//   - `Dragging()`（`ui/dragdrop.go:64`）报的是 Drag / Draggable 两套机制，
//     普通 Column 永远不碰它——按着把手横移 200 DIP 它一直是 false。项目
//     README 记的就是这一条（MyGo 自带的 `examples/timeline` 踩的也是它）。
//   - `Pressed()`（`ui/input.go:1150`）多一条**指针必须在元素框内**的条件。
//     把手只有 8 宽，指针按下去之后往左挪 9 个像素就出框了，它当场变 false
//     ——侧栏最多只能拖动 16 像素宽就卡住，而「拉到 160 以下自动折叠」正是
//     这一张的验收。进度条那种「轨道横跨整个行程、指针一直留在框内」的控件
//     才适用它（见 playbar.go 的进度条，那里的用法是对的）。
//   - `Dragged()` 的 ok 就是 `s.pressed`（`ui/input.go:1246-1259`），没有框内
//     这一条。
//
// ⚠️ **宽度算的是指针的绝对位置，不是 `Dragged()` 给的位移。**
// `Dragged()` 的 `dragX` 要到一帧结束（`ui/runtime.go:775` 的 forgetInput）
// 才清掉，而**一帧里 view 要跑好几遍**（`runtime.go:524` 的 `for pass := 0;
// pass < 3`），每一遍读到的都是同一个位移——累加起来就是拖快好几倍。所以
// 这里只拿 `Dragged()` 判「按着没有」，位置改用 `PointerPosition`：
//
//   - 它给的是指针相对元素框的坐标，而侧栏左边贴着窗口左边（x=0），所以读
//     出来的就是窗口里的绝对 x，和 Biu 的 `event.clientX` 是一个东西。
//   - 同一个值在一帧的每一遍里都一样，累加不了也不需要累加。
//
// 拿把手自己的框是不行的：把手跟着侧栏的右缘走，框一动，相对坐标就跟着动，
// 算出来是个常数。
func (s *Shell) sideTrackDrag(side, grip ui.Element, target float64) {
	_, _, held := grip.Dragged()
	if !held {
		if s.sideDragging {
			s.sideDragging = false
			s.commitSideWidth()
		}
		return
	}

	x, _, _ := side.PointerPosition()
	if !s.sideDragging {
		s.sideDragging = true
		s.sideDragFromX = x
		// 起手用目标宽度，不是上一帧动画到一半的那个——Biu 的 startWidthRef
		// 取的也是 sidebarWidth。跟着取动画中间值会在动画没走完时被抓住时
		// 先跳一下。
		s.sideDragFromWidth = target
	}
	s.sideWidth, _, _ = sideDragWidth(s.sideDragFromWidth + float64(x-s.sideDragFromX))
}

// commitSideWidth 是松手那一刻把宽度和折叠态写进设置。
//
// 拖拽排序和这里是同一个道理：用户做了个决定，就得立刻存住。写失败
// （设置只读、拿不到目录）必须说给用户听——不然表现就是「松手弹回去了」。
func (s *Shell) commitSideWidth() {
	_, persist, collapsed := sideDragWidth(s.sideWidth)
	if _, err := s.app.UpdateSettings(func(st *app.Settings) {
		st.SideMenuCollapsed = collapsed
		st.SideMenuWidth = persist
	}); err != nil {
		s.addToast("侧栏宽度存不住："+err.Error(), notice.Danger)
	}
}

// viewSideBrand 是左上角那一条品牌：40×40 的 logo + 24px 主色粗体的应用名。
//
// 上游是 `biu/src/layout/side/logo/index.tsx`：一个 `h-10 w-10` 的 logo，
// 旁边 `text-2xl` 粗体的应用名，整个块 `py-3 pr-3 pl-4`。
//
// ⚠️ **这里摆的是应用名，折叠按钮下面那一行早就不摆它了。** 2026-10 从
// `Biu MyGo` 改名成 `BiuGo` 时只改了下面那行，于是展开的侧栏上方一个 18pt 的
// 「Biu」、下面一个 11pt 的「BiuGo」，**同一栏里两个产品名**，而两行都是正常的
// Text，没有任何一处会报错。现在这一处是**唯一**一处写着应用名的地方，
// `sidebar_test.go` 的 `TestTheSidebarShowsTheAppNameExactlyOnce` 钉住它。
//
// ⚠️ **折叠态只摆 logo，不摆名字**：侧栏折叠后是 72 像素
// （`app.CollapsedSideMenuWidth`），而 24px 的「布丁音乐」光文字就约 96 宽——
// 摆不下。上游也是折叠态不摆名字（`{!isCollapsed && <span>Biu</span>}`）。
//
// # 字色：主色，但浅色下走 `ink.AccentOn`
//
// 上游那一行是 `text-primary`，也就是**原样的主色**。浅色下那个绿（`#1ed760`）
// 压在白底上只有 1.9:1——品牌字是全应用最大的一行字之一，糊在那儿最扎眼。
// `ink.AccentOn` 是我们自己的那一档（浅色下从**用户选的那个**主色算一个
// ≥4.5:1 的变体，深色下原样不动），见 `widgets/ink`。
func (s *Shell) viewSideBrand(c *ui.Context, collapsed bool) {
	t := c.Theme()
	// ⚠️ **名字要让开左上角那三枚系统按钮**（macOS 的红黄绿）：它们**浮在内容
	// 上面**，而侧栏是**通栏**的（从窗口最顶开始），所以品牌这一行和它们**同一行**
	// ——不挪开的话名字就**顶在那三枚底下**。
	// `bar.Left` 就是系统控件在左边占掉的宽度（真窗口上约 66 像素；
	// `mygo/ui/context.go` 的 `TitleBar{Height, Left, Right}`）。
	// ⚠️ 顶栏那一行**不让**这几十像素（票 #286：顶栏只盖内容区，侧栏那一片不盖），
	// 而侧栏是通栏的 —— **两处的理由不一样**。
	bar := c.TitleBar()
	row := ui.Row(c).Key("side-brand").Gap(space.Px8).AlignItems(ui.Center).
		Padding(s.sideBrandTopPad(), space.Px12, space.Px12, space.Px16+bar.Left)
	if collapsed {
		// 🔴 **折叠态必须把左右那两格内边距去掉**：侧栏折叠后是 72，而其中
		// **9 像素是右缘那条把手和分隔线**（8 + 1）——内容区只剩 63。
		// 16 + 40 + 12 = 68 > 63，于是品牌那一块**把侧栏撑宽了 5 像素**：
		// 折叠后的侧栏变成 77，而折叠宽度是 72（`app.CollapsedSideMenuWidth`）。
		// 症状是「拖窄了没折叠到 72」，而界面上看不出是哪一块多出来的。
		//
		// 去掉内边距之后 logo 是**居中**的（和折叠态那一列图标格一样），
		// 上下对齐。
		row = row.Padding(s.sideBrandTopPad(), space.None, space.Px12, space.None).Justify(ui.Center)
	}
	row.Children(func() {
		// ⚠️ **不摆 logo 了**（人 2026-10-26：「左上角的不需要用 icon 了，
		// 就用名称就行」）。
		//
		// 折叠态摆**一个字**（不是图）：侧栏折叠后是 72 像素、内容区只剩 63，
		// 24px 的「布丁音乐」四个字放不下，但**一个字**放得下 —— 折叠后那一片
		// 不至于空着。
		if collapsed {
			ui.Text(c, appNameShort).FontSize(scale.Font(c, scale.BrandName)).Bold().
				TextColor(ink.AccentOn(t))
			return
		}
		ui.Text(c, appName).FontSize(scale.Font(c, scale.BrandName)).Bold().
			TextColor(ink.AccentOn(t))
	})
}

// sideBrandTopPad 是品牌那一块的**上**内边距。
//
// 上游是 12（`py-3`），而 **macOS 上还没全屏时是 32**（`pt-8`）——那一格是
// 给红黄绿让开的：上游的侧栏从窗口最上头就开始（`layout/index.tsx` 的
// `flex h-full`），红黄绿浮在它左上角，所以 logo 得往下挪一格。
//
// ⚠️ **这一格现在真的在给红黄绿让位了**（票 #286 / 报告 C-01）：侧栏改成
// **从窗口顶端贯到底**（上游那种通栏），而顶栏只盖内容区那一段——于是红黄绿
// 落在**侧栏这一带**，logo 正好在它们下面。之前侧栏在顶栏下面，红黄绿够不着
// 品牌，这一格是照上游抄下来的数；现在它是那件事本身。
//
// ⚠️ **红黄绿的位置**：`internal/ui/shell_window.go` 那一处把 `TrafficLightPosition`
// 钉在 `{X: 14, Y: 18}`，所以它们占着窗口左上角 y=18..30 那一段，而 logo 的
// 上沿在 y=32——**差 2 像素**。别把这一格调小：调小 logo 就压在红黄绿底下了。
func (s *Shell) sideBrandTopPad() float32 {
	if runtime.GOOS == "darwin" && !s.fullScreen {
		return 32
	}
	return 12
}

// brandLogo 是左上角那个 logo 的图：应用图标本身。
//
// ⚠️ **只做一次**：MyGo 的 `NewBitmap` 会把图转成一张纹理，而框架的文档明写
// 「Create it once: converting an image is not free」——每帧建一张的话，
// 侧栏每帧都多一笔分配，而这一帧本来该是零（界面层里这一处是常驻的）。
//
// 用的是 `AppIconShared`（进程内那一份只读图）而不是 `AppIcon`：托盘图标用的
// 是同一份，两边不能各画各的。
var (
	brandLogoOnce sync.Once
	brandLogoImg  *ui.Bitmap
)

// brandLogo 取那一张 40×40 的图。
func brandLogo() *ui.Bitmap {
	brandLogoOnce.Do(func() {
		brandLogoImg = ui.NewBitmap(platform.AppIconShared(sideBrandLogo))
	})
	return brandLogoImg
}

// viewSideCollapse 是底下那个折叠开关。Biu 的 `RiArrowLeftDoubleLine` /
// `RiArrowRightDoubleLine`（`side/index.tsx:171-173`）没有图标可用，
// 这里用两个双尖括号，意思一样。
func (s *Shell) viewSideCollapse(c *ui.Context, st app.Settings, collapsed bool) {
	label, tip := "«", "折叠侧栏"
	if collapsed {
		label, tip = "»", "展开侧栏"
	}

	// 固定高度，**不写 Grow(1)**：Grow 是「吃掉这一列剩下的空间」，
	// 折叠按钮一长就把上面那块滚动区挤成 0 高，菜单全点不动。
	//
	// `Label` 留着：画面上只有 «/» 这一个符号，念不出是什么，而 tooltip 只是
	// 给鼠标看的（无头 Tester 认按钮也靠它）。
	//
	// ⚠️ **这里不许给描边**。Biu 的折叠键是 `border-y`（`side/index.tsx:171`），
	// 抄过来看着更像——而 `flatButton` 那一条不变量（`flatbutton_test.go` 的
	// `TestTheSidebarRowsHaveNoBorderAndNoFill`）量的是**侧栏最左边那一列**
	// 上有几段不是底色的像素，一道横贯整栏的线在那一列上就是两段，于是它读成
	// 「菜单行还有一圈描边」。给这一枚开特例要改的是那条不变量，而它守的是
	// #123 那票的核心结论（侧栏菜单不该有描边药丸）——为了一枚按钮的两条线去
	// 放宽它，代价大于收益。分界靠上面那条右缘的竖线和它自己的 hover。
	//
	// ⚠️ **翻的是设置里那个开关，不是这一帧画出来的那一档**：拖动的时候视觉档
	// 可能和设置不一致（`sideCollapsedVisual`），而点这一枚要翻的永远是设置
	// （上游的 `onToggleCollapsed` 也是这么写的）。
	if flatButton(c, label).Label(tip).Height(sideCollapseHeight).Clicked() {
		if _, err := s.app.UpdateSettings(func(cur *app.Settings) {
			cur.SideMenuCollapsed = !st.SideMenuCollapsed
		}); err != nil {
			s.addToast("折叠状态存不住："+err.Error(), notice.Danger)
		}
	}
}

// viewSideMenu 是默认菜单那一组（Biu 的 SD-04）。
//
// 每一项前面有一枚 18 的线框图标，**选中那一项换成实心的那一枚**（上游
// `biu/src/common/constants/menus.tsx` 的 `icon` / `activeIcon`）。图标配在
// 路由表里（`router.go` 的 `Route.Icon` / `Route.ActiveIcon`），不在这里写死。
//
// ## 折叠态：整个按钮就是那个 40×40 的图标格
//
// 上游折叠态那一项摆的是一个 `h-10 w-10` 的 Avatar，图标是它的 fallback
// （`menu-item.tsx` 的 `collapsed` 那一支）。我们照着摆成一个 40×40 的图标格
// ——**不再是一个汉字**：原来那一档摆的是每个标题的第一个字（「下」「本」「推」
// 「设」），而那个 72 宽的方块里只有一枚图标时，没有字可以让用户对照着认出来。
//
// ## 未登录时那三个入口不摆，摆一句「登录后可见」
//
// 「我的关注」「稍后再看」「历史记录」的内容全都要登录才有（`Route.HideInMenuWhenLoggedOut`）。
// 未登录时照样摆着它们，点了进去是空的——用户会以为是应用坏了，而实际上他只是
// 没登录。所以这里**不摆**，改在原位摆一句提示（见 viewLoggedOutHint）。
//
// ⚠️ **那句提示不许写出那三个名字**：写出来的话「未登录时侧栏里没有这三个词」
// 就成了一句永远不成立的话，而它正是这一条验收的全部（判据是画面上**找不到**
// 那三个词，而提示和它们在同一个侧栏里）。
func (s *Shell) viewSideMenu(c *ui.Context, st app.Settings, collapsed bool) {
	cur := s.router.Path()
	loggedIn := s.app.LoggedIn()
	hidden := 0
	// first 是「这一帧第一个画出来的菜单项」——它登记成「焦点回侧栏时落回的那
	// 一项」（票 #245 的 E4）。理由见下面那一处。
	first := true
	// lastGroup 是上一个画出来的项所在的分组（空串 = 还没画过）。
	lastGroup := ""

	// ⚠️ Key 给的是包在外面的这一格，不是 Button 本身：`Key` 挂在「创建时
	// 就处理了输入」的控件上会 panic（`ui/element.go:658`），要给就包一层。
	ui.Column(c).Key("side-menu").Gap(space.Px2).Children(func() {
		for _, rt := range routes {
			// 设置页「菜单设置」里藏起来的那几项（`hiddenMenuKeys`）不摆。
			// 这是 Biu 的 `DefaultMenus` 那一支（`side/index.tsx` 的 filter）。
			if !rt.InMenu || slices.Contains(st.HiddenMenuKeys, rt.Match) {
				continue
			}
			// 未登录时那几项不摆（理由见这一段的抬头）。
			if rt.HideInMenuWhenLoggedOut && !loggedIn {
				hidden++
				continue
			}
			// 顺序是 Biu 的 SD-04 定的，所以行 key 用路由而不是序号：
			// 以后菜单项按 hiddenMenuKeys 过滤、顺序会变，序号就不再稳定了。
			//
			// ⚠️ **展开态别给 Button 补 `Label(rt.Title)`**：可见的字就是名字，
			// 补上去会多出一个同样文字的无障碍节点，画面上出现两份。
			// 分组标题（设计规范 §7.23 / 概念图 G16）：发现 / 我的音乐，设置另起一段不带标题。
			// 画在**这一组第一个真画出来的项**前面——藏掉的项不算，所以一组全被藏了标题也不出现。
			if g := sideGroupOf(rt.Match); g != lastGroup {
				s.viewSideGroupTitle(c, g, lastGroup == "" && first, collapsed)
				lastGroup = g
			}
			ui.Column(c).Key("side-item:" + rt.Match).Children(func() {
				item := s.sideMenuItem(c, rt, rt.Match == cur, collapsed)
				if item.Clicked() {
					s.navigate(rt.Match)
				}
				// 🔴 **只有第一项登记成「焦点回侧栏时落回的那一项」**
				// （票 #245 的 E4）：登记是「一个句柄绑一个控件」，而这一帧侧栏
				// 里画着的菜单项有好几个——全登记的话框架会当场 panic。
				//
				// ⚠️ **「第一项」是「第一个画出来的」**：未登录时前面几项被藏起来
				// （`HideInMenuWhenLoggedOut`），落回的那一项要跟着变。
				if first {
					first = false
					s.sideFocusItem(item)
				}
			})
		}
		// 一项都没藏起来就不摆这句：用户自己在设置里把那几项藏了，这时再说
		// 「登录后可见」是拿一句不成立的话去填空白。
		if hidden > 0 && !collapsed {
			s.viewLoggedOutHint(c, hidden)
		}
	})
}

// 侧栏的分组（设计规范 §7.23）。菜单名以仓库为准，分组只决定标题。
const (
	sideGroupDiscover = "发现"
	sideGroupMine     = "我的音乐"
	// sideGroupSystem 是设置那一段：**不带标题**，只是和上面隔开一截（设计稿里设置在底部）。
	sideGroupSystem = "\x00system"
)

// sideGroupOf 是一条菜单路由属于哪一组。不认识的路由归「我的音乐」。
func sideGroupOf(match string) string {
	switch match {
	case homePath:
		return sideGroupDiscover
	case settingsPath:
		return sideGroupSystem
	}
	return sideGroupMine
}

// sideGroupTitleGap 是分组标题上下的留白：标题上方 12（第一组只留 4）、下方 4。
const (
	sideGroupGapTop    = space.Px12
	sideGroupGapTopTop = space.Px4
	sideGroupGapBottom = space.Px4
)

// viewSideGroupTitle 画一个分组标题。
//
// 展开态：`caption` 号、`TextMuted`、Medium，左内缩和菜单项的图标对齐；设置那一段
// （`sideGroupSystem`）不带字，只留上方的缝。折叠态没有地方写字，只留一道同样高的缝。
// 标题是纯装饰：不可点、不抢焦点，`PassThrough` 让点击落到下面的东西上。
func (s *Shell) viewSideGroupTitle(c *ui.Context, group string, firstOfAll, collapsed bool) {
	t := c.Theme()
	top := float32(sideGroupGapTop)
	if firstOfAll {
		top = sideGroupGapTopTop
	}
	ui.Column(c).Key("side-group:"+group).FillWidth().PassThrough().
		Padding(top, space.Px16, sideGroupGapBottom, space.Px16).Children(func() {
		if collapsed || group == sideGroupSystem {
			return
		}
		ui.Text(c, group).FontSize(scale.Font(c, scale.Size12)).FontWeight(weightMedium).
			TextColor(t.TextMuted).SingleLine().PassThrough()
	})
}

// sideMenuItem 是侧栏菜单里的一项。
//
// selected 是「当前就在这一页」——它换的是**图标那一枚**（实心）和**底色**
// （上游 HeroUI 的 `variant="flat"` + `color="primary"`，见 flatCurrent）。
//
// # 展开态：图标在 40×40 的格子里，名字在格子右边
//
// 整行 40 高（上游 HeroUI 的 Button md），内容**左对齐**（上游的
// `justify-start`）——图标要成列，内容居中就没有列了。
//
// # 折叠态：整个按钮就是那一个 40×40 的格子
//
// ⚠️ **折叠态必须给一个无障碍的名字**：画面上只有一枚图标，读屏软件念出来是
// 「图标」——这一格等于**完全没有名字**。`Label` 补上的是**名字**
// （`Element.Label`），**画面上不多画一个字**（折叠态画面上本来就没有那个全名）。
//
// 悬停提示是 `sideItemTip` 给的（贴在那一项的**右边**，上游的
// `placement="right"`）：折叠态那一格只有一个图标，得让人认出它是哪一项。
func (s *Shell) sideMenuItem(c *ui.Context, rt Route, selected, collapsed bool) ui.Element {
	name := rt.Icon
	if selected {
		name = rt.ActiveIcon
	}

	if collapsed {
		// `AlignSelf(Center)`：格子是 40 宽，而侧栏折叠后是 72——不居中的话
		// 它会贴在左边沿上（上游那一格也是居中的：Avatar 在 `justify-center`
		// 的按钮里）。
		item := flatIconCellButton(c, name, rt.Title, sideIconSize, sideIconCell).
			AlignSelf(ui.Center)
		s.sideItemTip(c, item, rt.Title)
		if selected {
			item = flatCurrent(c, item)
		}
		return item
	}
	item := flatIconLabelButton(c, name, rt.Title, sideIconSize, sideIconCell)
	// 选中那一层照上游 HeroUI 的 `variant="flat"` + `color="primary"`
	// （`components/menu/menu-item.tsx:83-84`）：主色很淡的一层底 +
	// 主色的字。**不是实心主色**——原来那一版 `Background(t.Accent)`
	// 就是调研点名的「选中整条刷实心绿」（报告 §2 基线 B 标注 3）。
	if selected {
		item = flatCurrent(c, item)
	}
	return item
}

// sideItemTip 是折叠态侧栏那一项的悬停提示。
//
// # 位置照上游：贴在那一项的**右边**
//
// 上游折叠态那一项外面套的是 `<Tooltip content={title} placement="right">`
// （`biu/src/components/menu/menu-item.tsx:78`）。框架自带的提示摆在**上面**，
// 而折叠态的侧栏贴着窗口左边——摆在上面的话提示会盖住上面那几项，摆在左边
// 又被窗口边钳住。所以走 `tipAt` 的自定义位置那一支。
//
// # 时延也是上游那一档
//
// 那一处还写了 `closeDelay={0}`：指针离开这一项，提示立刻收。菜单是用户顺着
// 一列扫的，晚 500ms 收会压在下一项上。
func (s *Shell) sideItemTip(c *ui.Context, anchor ui.Element, text string) {
	s.tipAt(c, anchor, text, tipMenu, func(panel, anchor ui.Element, t *ui.Theme) {
		panel.AttachTo(anchor, ui.AnchorRight, ui.AnchorLeft).MarginStart(space.Px6)
	}, true)
}

// sideFocusable 是侧栏里那些「鼠标点得动、键盘走不到」的元素**统一的出口**。
//
// # 为什么它们要能进键盘焦点
//
// `Element.Clicked()` 只设 `flagClickable`（`mygo/ui/input.go:1042`）——
// **不设 `flagFocusable`**；而 Tab 只走可聚焦的元素（`mygo/ui/input.go` 的
// `focusChain`）。于是那一行鼠标点得动、键盘却走不到：Tab 跳过它，回车也
// 按不到它。
//
// 报告（BMG-246 的 `audit-side-settings.md` 的 (b) 一节「键盘那一路」）把这一条
// 列成**四个断口**的头一条：**侧栏的收藏夹列表键盘完全走不到**（展开态那一列
// 和折叠态那层浮层里的行都是）。
//
// # ⚠️ **焦点环是框架画的那一圈**
//
// `.Focusable()` 之后框架给这个元素画焦点环（`mygo/ui/paint.go:334`）——
// 和侧栏菜单项、顶栏那几枚图标键**同一档**，不另立一套。
//
// ⚠️ **框架那一圈画在元素外面 3 像素**（`mygo/ui/paint.go` 的 `FocusRing`）：
// 收藏夹那一行左右各有 8 像素的留白，画在外面压不到字上。
//
// # ⚠️ **可聚焦之后，回车 / 空格就能按下去了**
//
// 框架给「有焦点而且可点」的元素补上那一下（`mygo/ui/input.go:741`），
// 所以这一行不但是「键盘走得到」，也是「键盘按得动」。
func sideFocusable(e ui.Element) ui.Element {
	return e.Focusable()
}

// loggedOutHint 是那一句话的正文。
//
// ⚠️ **一个「我的关注」「稍后再看」「历史记录」都不许出现在里面**（理由见
// viewSideMenu 的抬头）。写出来的后果是那一票的验收自己就废了：判据是「画面上
// 找不到这三个词」，而提示和它们摆在同一个侧栏里。
//
// 靠**数字**告诉用户有几个被藏起来了——那一档不必说出是哪几个（说了就犯规），
// 而用户从「侧栏上少了几行」对得上。
func loggedOutHint(hidden int) string {
	return "还有 " + strconv.Itoa(hidden) + " 个入口，登录后可见"
}

// viewLoggedOutHint 是那一行提示。
//
// 折叠态不画：72 宽摆不下这一句，而裁成半个字的那一句比没有更让人以为坏了。
// 展开之后它就在那儿——提示是给「侧栏里少了几项」这件事一个解释，不是必须
// 在任何宽度下都占一格的东西。
//
// ⚠️ **`Wrap()` 不能省**：侧栏拖到最窄是 160，而这一句在 12px 下差不多就要
// 160——不换行的话它会被裁掉半句，而半句「还有 3 个入口，登录后可」比不摆更让人
// 以为侧栏坏了。
//
// # 这一句**是能点的**（票 #242，UX 报告 §3-A 的 A1）
//
// 原来它是一句纯文字：它解释了「侧栏上少了三行」，却**没给下一步**——用户读完
// 只能自己去顶栏找那枚没有文字的图标（而那一枚图形当时还和「退出」只差一个箭头
// 方向，见 `icons.go` 的 `iconLogin`）。现在按它直接去扫码登录那一页。
//
// ⚠️ **别把它改成一枚按钮**：那一句的作用是「解释侧栏上少了什么」，做成药丸
// 按钮会和旁边那些菜单项长得一样，而菜单项是「进去看看」——两件事不该长得一样。
// 它保持成一行字，靠**悬停时变主色 + 下划线**告诉用户它点得动（上游歌名那处
// 可点文字是同一套做法，见 `playbar.go`）。
func (s *Shell) viewLoggedOutHint(c *ui.Context, hidden int) {
	ui.Column(c).Key("side-logged-out").Children(func() {
		//
		// 🔴 **它也得能进键盘焦点**（票 #453）：这一行鼠标点得动（`OnClick`），
		// 而 `OnClick` 只设 `flagClickable`——**键盘走不到**（报告 (b) 一节的
		// 第四个断口：「「还有 N 个入口，登录后可见」那行文字鼠标可点、键盘不可达」）。
		hint := ui.Text(c, loggedOutHint(hidden)).FontSize(scale.Font(c, scale.Size12)).Wrap().
			TextColor(c.Theme().TextMuted).Padding(space.Px4, space.Px16).
			OnClick(func() { s.goLogin(homePath) })
		hint = sideFocusable(hint)
		// 悬停才显出「点得动」：主色 + 下划线。
		//
		// ⚠️ **主色当字色要取 `ink.AccentOn`**：浅色下主色是亮绿，压在浅底上
		// 只有 1.9:1（读不出来），所以取那个「读得出来」的变体——理由和
		// `flatbutton.go` 的 `flatCurrent` 是同一条。
		if hint.Hovered() {
			hint = hint.TextColor(ink.AccentOn(c.Theme())).Underline().
				Cursor(ui.CursorPointer)
		}
	})
}

// collectionPath 是一个收藏夹在侧栏里点进去的路由。
//
// ⚠️ 前缀用 `collectionPathPrefix`（collectionpage.go 里那一处）：那边要从同
// 一个前缀上切出 id，两边各写一遍字符串的话，改了一边就会变成「侧栏点进去
// 404」。
func collectionPath(id int64) string {
	return collectionPathPrefix + strconv.FormatInt(id, 10)
}
