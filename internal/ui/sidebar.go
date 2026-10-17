// 侧栏：品牌 + 默认菜单 + 两组收藏夹，右缘一条能拖的把手，底下能折叠。
//
// 结构照 Biu 的 `src/layout/side/index.tsx`：Brand / 可滚的内容区 /
// 折叠按钮 / 右缘的拖拽把手。三个尺寸（Biu 的 `COLLAPSED_WIDTH`、`MIN_WIDTH`、
// `MAX_WIDTH`）和 200ms 的过渡全部照抄，数字在 app 包里（见 app/settings.go
// 的 ClampSideMenuWidth），因为设置读进来也要钳同一套。
package ui

import (
	"slices"
	"strconv"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
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
	sideWidthTransition = 200 * time.Millisecond

	// sideWidthKey 是宽度动画在 MyGo 里的键。同一个元素上同一个键的动画值
	// 跨帧续着走，换键就是换一条动画。
	sideWidthKey = "side-width"

	// sideCollapseHeight 是底下那个折叠按钮多高。Biu 是 `py-1`（8+8 加一行
	// 字），这里取 28。
	sideCollapseHeight = 28
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

	side := ui.Row(c).Key("side")
	// 拖动中不过渡（Biu 的 `transition-none`），松手和折叠才走 200ms。
	//
	// ⚠️ 拖动那一支必须先 `Jump`：动画的值还停在上一段动画的终点，不先钉住
	// 的话，宽度会先从那儿跳到手指的位置，看起来是「一碰就弹一下」。
	var w float32
	if s.sideDragging {
		w = side.Jump(sideWidthKey, float32(s.sideWidth))
	} else {
		w = side.AnimateTo(sideWidthKey, float32(target), ui.Animation{Duration: sideWidthTransition})
	}

	side.Width(w).AlignSelf(ui.Stretch).Background(c.Theme().Surface).Children(func() {
		// ⚠️ FillHeight 不能省：Row 是把子元素**垂直居中**摆的（MyGo 的
		// `docs/ui/layout.md`：「Row places them from left to right and
		// centers them vertically」），不拉满高这一栏就按内容高度浮在中间，
		// 底下空出一大块，滚动区也就没高度了。
		ui.Column(c).Key("side-body").Grow(1).FillHeight().Children(func() {
			s.viewSideBrand(c, st)
			// 菜单和收藏夹一起滚。Biu 也是这么分的（ScrollContainer 包住
			// DefaultMenus + Collection），侧栏窄的时候不至于把折叠按钮顶出去。
			ui.Scroll(c).Key("side-scroll").Grow(1).FillWidth().Children(func() {
				s.viewSideMenu(c, st)
				s.viewFavorites(c, st)
			})
			s.viewSideCollapse(c, st)
			// 应用名贴在折叠按钮上面。折叠态摆不下（72 宽），Biu 那边也没有
			// 这一行——这里留着是为了窗口标题和侧栏对得上。
			if !st.SideMenuCollapsed {
				ui.Text(c, "Biu MyGo").FontSize(11).
					TextColor(c.Theme().TextMuted).Padding(0, 8, 4, 8)
			}
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
//   - `Pressed()`（`ui/input.go:1028`）多一条**指针必须在元素框内**的条件。
//     把手只有 8 宽，指针按下去之后往左挪 9 个像素就出框了，它当场变 false
//     ——侧栏最多只能拖动 16 像素宽就卡住，而「拉到 160 以下自动折叠」正是
//     这一张的验收。进度条那种「轨道横跨整个行程、指针一直留在框内」的控件
//     才适用它（见 playbar.go 的进度条，那里的用法是对的）。
//   - `Dragged()` 的 ok 就是 `s.pressed`（`ui/input.go:1124-1136`），没有框内
//     这一条。
//
// ⚠️ **宽度算的是指针的绝对位置，不是 `Dragged()` 给的位移。**
// `Dragged()` 的 `dragX` 要到一帧结束（`ui/runtime.go:670` 的 forgetInput）
// 才清掉，而**一帧里 view 要跑好几遍**（`runtime.go:657` 的 `for pass := 0;
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
		s.addToast("侧栏宽度存不住：" + err.Error())
	}
}

// viewSideBrand 是顶上那一条品牌。Biu 的 SD-03。
//
// 红绿灯的位置不用让：顶栏已经把 `bar.Left` 让出来了，侧栏从它下面开始。
func (s *Shell) viewSideBrand(c *ui.Context, st app.Settings) {
	if st.SideMenuCollapsed {
		// 折叠后 72 宽，「Biu」摆得下但和一个菜单项分不出来，退成一个字母，
		// 和下面那一列单字对齐。
		ui.Text(c, "B").FontSize(20).Bold().TextColor(c.Theme().Text).
			Padding(6, 0, 12, 0)
		return
	}
	ui.Text(c, "Biu").FontSize(18).Bold().Padding(8, 8, 12, 8)
}

// viewSideCollapse 是底下那个折叠开关。Biu 的 `RiArrowLeftDoubleLine` /
// `RiArrowRightDoubleLine`（`side/index.tsx:171-173`）没有图标可用，
// 这里用两个双尖括号，意思一样。
func (s *Shell) viewSideCollapse(c *ui.Context, st app.Settings) {
	collapsed := st.SideMenuCollapsed
	label, tip := "«", "折叠侧栏"
	if collapsed {
		label, tip = "»", "展开侧栏"
	}

	// 固定高度，**不写 Grow(1)**：Grow 是「吃掉这一列剩下的空间」，
	// 折叠按钮一长就把上面那块滚动区挤成 0 高，菜单全点不动。
	if ui.Button(c, label).Label(tip).Height(sideCollapseHeight).Clicked() {
		if _, err := s.app.UpdateSettings(func(cur *app.Settings) {
			cur.SideMenuCollapsed = !collapsed
		}); err != nil {
			s.addToast("折叠状态存不住：" + err.Error())
		}
	}
}

// viewSideMenu 是默认菜单那一组（Biu 的 SD-04）。
//
// 折叠后只摆每个标题的第一个字：Biu 折叠态摆的是图标
// （`default-menu/index.tsx` 的 `collapsed`），本项目还没有图标资源，
// 摆全名会被裁成半个字，比一个字更认不出来。
func (s *Shell) viewSideMenu(c *ui.Context, st app.Settings) {
	t := c.Theme()
	cur := s.router.Path()
	collapsed := st.SideMenuCollapsed

	// ⚠️ Key 给的是包在外面的这一格，不是 Button 本身：`Key` 挂在「创建时
	// 就处理了输入」的控件上会 panic（`ui/element.go:537`），要给就包一层。
	ui.Column(c).Key("side-menu").Gap(2).Children(func() {
		for _, rt := range routes {
			// 设置页「菜单设置」里藏起来的那几项（`hiddenMenuKeys`）不摆。
			// 这是 Biu 的 `DefaultMenus` 那一支（`side/index.tsx` 的 filter）。
			if !rt.InMenu || slices.Contains(st.HiddenMenuKeys, rt.Match) {
				continue
			}
			label := rt.Title
			if collapsed {
				label = firstRune(rt.Title)
			}
			// 顺序是 Biu 的 SD-04 定的，所以行 key 用路由而不是序号：
			// 以后菜单项按 hiddenMenuKeys 过滤、顺序会变，序号就不再稳定了。
			//
			// ⚠️ 别给 Button 再补一个 `Label(rt.Title)`：可见的字就是名字，
			// 补上去会多出一个同样文字的无障碍节点，画面上出现两份。
			ui.Column(c).Key("side-item:" + rt.Match).Children(func() {
				item := ui.Button(c, label).Grow(1)
				if rt.Match == cur {
					item = item.Background(t.Accent)
				}
				if item.Clicked() {
					s.navigate(rt.Match)
				}
			})
		}
	})
}

// firstRune 是一个标题折叠后摆出来的那一个字。
func firstRune(s string) string {
	for _, r := range s {
		return string(r)
	}
	return ""
}

// collectionPath 是一个收藏夹在侧栏里点进去的路由。
func collectionPath(id int64) string {
	return "/collection/" + strconv.FormatInt(id, 10)
}
