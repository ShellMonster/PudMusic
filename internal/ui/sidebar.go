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

	// sideDividerW / sideDividerA 是右缘那条分隔线的宽度和浓度。
	//
	// 照 Biu 的 `border-divider/30 border-r-1`（`side/index.tsx:145`）：
	// 1 像素、透明度 30%。**不能照抄 `t.Border` 原色**——深色下 `#3f3f46`
	// 压在 `#17181a` 上是一道亮线，比上游那道明显得多（flatbutton.go 的文件头
	// 记的就是这件事：同一个色在深浅两套里的观感完全不同）。
	sideDividerW = 1
	sideDividerA = 0.3
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
	side.Width(w).AlignSelf(ui.Stretch).Background(c.Theme().Background).
		BorderWidth(0, sideDividerW, 0, 0).
		BorderColor(c.Theme().Border.Alpha(sideDividerA)).
		Children(func() {
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
				// 完整应用名在折叠按钮下面那一条。见 `viewSideFoot`。
				s.viewSideFoot(c, st)
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

// viewSideBrand 是顶上那一条品牌：一个字母的标记。
//
// ⚠️ **这里摆的是字母，不是应用名。** 完整名字在侧栏最下面那一行
// （`viewSideFoot`）。两处别搞混，也别只改一处——2026-10 从 `Biu MyGo` 改名成
// `BiuGo` 时就是只改了下面那行，于是展开的侧栏上方一个 18pt 的「Biu」、下面一个
// 11pt 的「BiuGo」，**同一栏里两个产品名**，而两行都是正常的 Text，没有任何一处
// 会报错。`sidebar_test.go` 的 `TestTheSidebarShowsTheAppNameExactlyOnce` 钉住它。
//
// ⚠️ **为什么摆不下整行应用名**：折叠后的侧栏是 72 像素（`app.CollapsedSideMenuWidth`），
// 而「拖窄到 160 以下自动折叠」这件事要求侧栏**能缩到 72**——内容最小宽度比 72 大
// 的话，它会被顶在 75.4，`TestDraggingTheGripBelowTheMinWidthCollapsesTheSidebar`
// 当场红（实测：顶上换成 18pt 的「BiuGo」就是这个数）。18pt 的「BiuGo」光文字就
// 约 55 宽，加上内边距过不了 72。所以顶上只摆标记——这和折叠态一直以来的做法
// 是同一件事，顺带还保住了 72 这个宽度。
//
// 红绿灯的位置不用让：顶栏已经把 `bar.Left` 让出来了，侧栏从它下面开始。
func (s *Shell) viewSideBrand(c *ui.Context, st app.Settings) {
	if st.SideMenuCollapsed {
		// 折叠态只 72 宽：标记大一号、贴左边，紧挨着下面那一列单字。
		ui.Text(c, "B").FontSize(20).Bold().TextColor(c.Theme().Text).
			Padding(6, 0, 12, 0)
		return
	}
	ui.Text(c, "B").FontSize(18).Bold().Padding(8, 8, 12, 8)
}

// viewSideFoot 是折叠按钮**下面**那一行完整应用名。
//
// （原来那行代码上方写着「应用名贴在折叠按钮上面」，而它其实画在按钮下面——
// 注释与代码不符，这次一并改对。）
//
// 它是侧栏里**唯一**写着应用名的地方，写的就是窗口标题（`shell.go` 的
// `WindowOptions.Title`）那个名字：展开时从上往下扫一眼，名字在最下面，
// 和窗口标题对得上。折叠态不画：72 宽摆不下（Biu 那边也没有这一行）。
func (s *Shell) viewSideFoot(c *ui.Context, st app.Settings) {
	if st.SideMenuCollapsed {
		return
	}
	ui.Text(c, "BiuGo").FontSize(11).
		TextColor(c.Theme().TextMuted).Padding(0, 8, 4, 8)
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
	if flatButton(c, label).Label(tip).Height(sideCollapseHeight).Clicked() {
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
// （`default-menu/index.tsx` 的 `collapsed`）。⚠️ **那一档还没换**：图标资源里
// 已经有清单 / 网格 / 本地音乐 / 下载记录 / 设置那几枚（见 `icons.go`），而
// **每一项该配哪一枚要对着 Biu 逐条抄**——七项抄错一处，折叠态就会有一项
// 配错意思，而那个 72 宽的方块里只有一枚图标，没有字可以让用户对照着认出来。
// 摆全名会被裁成半个字，比一个字更认不出来。
//
// ## 未登录时那三个入口不摆，摆一句「登录后可见」
//
// 「我的关注」「稍后再看」「历史记录」的内容全都要登录才有（`Route.NeedsLogin`）。
// 未登录时照样摆着它们，点了进去是空的——用户会以为是应用坏了，而实际上他只是
// 没登录。所以这里**不摆**，改在原位摆一句提示（见 viewLoggedOutHint）。
//
// ⚠️ **那句提示不许写出那三个名字**：写出来的话「未登录时侧栏里没有这三个词」
// 就成了一句永远不成立的话，而它正是这一条验收的全部（判据是画面上**找不到**
// 那三个词，而提示和它们在同一个侧栏里）。
func (s *Shell) viewSideMenu(c *ui.Context, st app.Settings) {
	cur := s.router.Path()
	collapsed := st.SideMenuCollapsed
	loggedIn := s.app.LoggedIn()
	hidden := 0

	// ⚠️ Key 给的是包在外面的这一格，不是 Button 本身：`Key` 挂在「创建时
	// 就处理了输入」的控件上会 panic（`ui/element.go:537`），要给就包一层。
	ui.Column(c).Key("side-menu").Gap(2).Children(func() {
		for _, rt := range routes {
			// 设置页「菜单设置」里藏起来的那几项（`hiddenMenuKeys`）不摆。
			// 这是 Biu 的 `DefaultMenus` 那一支（`side/index.tsx` 的 filter）。
			if !rt.InMenu || slices.Contains(st.HiddenMenuKeys, rt.Match) {
				continue
			}
			// 未登录时那几项不摆（理由见这一段的抬头）。
			if rt.NeedsLogin && !loggedIn {
				hidden++
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
				item := flatButton(c, label).Grow(1)
				// 选中那一层照上游 HeroUI 的 `variant="flat"` + `color="primary"`
				// （`components/menu/menu-item.tsx:83-84`）：主色很淡的一层底 +
				// 主色的字。**不是实心主色**——原来那一版 `Background(t.Accent)`
				// 就是调研点名的「选中整条刷实心绿」（报告 §2 基线 B 标注 3）。
				if rt.Match == cur {
					item = flatCurrent(c, item)
				}
				if item.Clicked() {
					s.navigate(rt.Match)
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
func (s *Shell) viewLoggedOutHint(c *ui.Context, hidden int) {
	ui.Column(c).Key("side-logged-out").Children(func() {
		ui.Text(c, loggedOutHint(hidden)).FontSize(12).Wrap().
			TextColor(c.Theme().TextMuted).Padding(4, 8)
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
//
// ⚠️ 前缀用 `collectionPathPrefix`（collectionpage.go 里那一处）：那边要从同
// 一个前缀上切出 id，两边各写一遍字符串的话，改了一边就会变成「侧栏点进去
// 404」。
func collectionPath(id int64) string {
	return collectionPathPrefix + strconv.FormatInt(id, 10)
}
