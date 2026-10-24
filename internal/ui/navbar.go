// 顶部导航栏：Biu 的 N-01~N-04、N-07、N-08。
//
// 结构照 `biu/src/layout/navbar/index.tsx`：左（Navigation + Search）、
// 右（AppUpdateNotify + Dev + UserFeed + UserCard）。
//
// ## 右边那一排：我们比上游多几枚（票 #289）
//
// 上游未登录时右边**只有一枚头像**，登录后多一枚「动态」；我们这边是
// 打开文件 / 迷你 / 动态红点 / 头像 / 登录入口。**多出来的那几枚各有各的
// 理由**（理由写在每一处代码旁边），而**唯一纯冗余的那一枚是「设置」**——
// 侧栏里有一条、头像下拉里也有第三条（上游把设置收进头像菜单，
// `biu/src/layout/navbar/user/index.tsx:107`）。那一枚在票 #289 里去掉了：
// **同一个动作三个入口，而三个入口会不一致**。剩下几枚要不要挪，是另一票
// 的事（挪入口不是纯对齐，要人拍板）。
//
// ## 整条是拖动区，这件事落在**外层 Row** 上，不是一个空的占位列
//
// Biu 是在那个 `div` 上按状态挂 `window-drag` / `window-no-drag`
// （`index.tsx:25-30`），两个 class 由 `isSearchFocused` 和
// `isUserDropdownOpen` 决定。MyGo 里对应的是 `Element.DragWindow()`。
//
// 落在外层 Row 上、而不是像骨架里那样单独留一列空的拖动占位，靠的是
// `ui/input.go:302` 的命中链：`target` 是链上**最内层**带输入标志的元素。
// 指针落在按钮、搜索框、头像上时 target 是它自己（它没有 flagDragWindow），
// 于是点按钮就是点按钮；落在顶栏的空处时 target 才是这行 Row，窗口才被拖。
// 拖动区和控件因此**天然不打架**，不必像 Biu 那样逐个挂 `window-no-drag`。
//
// 唯一照 Biu 保留的是那条例外：搜索框那边开着东西的时候整条不拖
// （Biu 的 `isNoDrag = isSearchFocused || isUserDropdownOpen`）。那时候用户
// 正准备点浮层里的东西，拖一下窗口就把选择弄没了。
//
// ⚠️ 新加的控件都要放进**这一个 Row 的 Children 里**：漏在别处（比如塞进
// `viewMain` 的别层）就落在拖动区外面，拖不到。
//
// 🔴 铁律二（`SetTheme` 每帧开头调一次）由 `viewMain` 满足，本文件里
// **不许**再调 `c.SetTheme`——见 theme.go 的说明。
package ui

import (
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

// navBackLabel / navForwardLabel 是那两个箭头的无障碍名字。
//
// ⚠️ **中文**：框架自带的 `ui.BackButton` / `ui.ForwardButton` 给的是 "Back" /
// "Forward"，中文界面里读屏念出来是英文。这两个名字同时也是无头 Tester 认得
// 它们的凭据（顶栏那几个测试 `Find(navBackLabel)`）。
const (
	navBackLabel    = "后退"
	navForwardLabel = "前进"
)

// navMinHeight 是顶栏多高。
//
// 🔴 **64 是照 Biu 量出来的**：它的顶栏那一格是 `h-16 flex-none`
// （`biu/src/layout/index.tsx:40`），而那一格里的 navbar 写的是 `h-full`——
// 也就是「有多高就是多高」，而外面钉死的就是 64。我们原来是 32（MyGo 自己
// 例子里的下限，`ui/context.go:357` 的 `max(bar.Height, 32)`），矮了整整一半。
//
// 这一条不只是「好看一点」：顶栏是**内容区上面那一行**，它矮 32，内容区就
// 少 32 ——而 `settings_page_test.go` 里量得着的可视区上下两个数
// （`pageTop` / `pageBottom`）就是从它推出来的。
//
// 仍然取 `max(bar.Height, …)`：macOS 上红黄绿是浮在内容上的（`TitleBarHidden`），
// 那一块系统自己留的高度比 64 大的话，得让开它，不然字会压在红黄绿底下。
const navMinHeight = 64

// navArrowW / navArrowH 是顶栏左边那两个箭头的**盒子**尺寸。
//
// 🔴 **32×40 是照上游量出来的**（票 #289 / 报告 C-03）：上游只有一枚后退，
// 盒 32×40、**无描边**（`biu/src/layout/navbar/navigation/index.tsx:15-24`：
// `isIconOnly variant="flat" radius="md" className="w-8 min-w-8"`，实图逐像素
// 量出来 32×40、底色 `#f7f7f7`、无描边）。
//
// 我们原来是 **34×30、带一圈 1 像素描边**——那 2 像素是框架 `ui.Button` 的
// 默认描边（`mygo/ui/widgets.go:152-168` 的 `styleButton`），而「描边药丸」
// 正是这一轮要收掉的那类毛病（`flatbutton.go:8-14` 记的就是这个）。
//
// ⚠️ **盒高 40 是上游的形状，不是我们的**：顶栏本身 64 高，一枚 40 高的键
// 在里面是**居中**的（`viewNav` 的 `AlignItems(ui.Center)`），上下各留 12。
// 改成 30（我们原来的高）反而比上游矮一截。
const (
	navArrowW = 32
	navArrowH = 40
)

// viewNav 是顶部导航栏：后退/前进 + 搜索框 + 打开文件 + 迷你 + 动态红点 + 头像 +
// 登录入口。
//
// ⚠️ **这里没有页名**（票 #127）。每一页自己写着标题，顶栏再写一遍就是同一个词在
// 屏幕上出现两次；上游的顶栏也没有（`layout/navbar/index.tsx:31-34`）。
// ⚠️ **这里也没有播放列表开关**：它在播放条右边那一组（`playbar.go` 的
// `viewPlayBarRight`）。理由见 Children 里那两条注释。
// ⚠️ **这里也没有「设置」**（票 #289）：侧栏里有一条、头像下拉里也有第三条
// （上游就是把设置收进头像菜单的，`biu/src/layout/navbar/user/index.tsx:107`），
// 顶栏这一枚是**同一个动作的第三个入口**，而三个入口会不一致。理由见 Children
// 里那一段。
func (s *Shell) viewNav(c *ui.Context) {
	// 后退/前进也是换页：上一页的提示同样不该留着，崩过的状态同样该复位。
	back := func() { s.clearToasts(); s.crash = nil }

	// ⚠️ 隐藏标题栏的窗口里，红黄绿是叠在内容上面的。左边让出 bar.Left、
	// 右边让出 bar.Right，才不会把它们压住。
	bar := c.TitleBar()

	nav := ui.Row(c).Key("nav").Height(max(bar.Height, navMinHeight)).
		Padding(space.None, bar.Right+8, space.None, bar.Left+8).
		Gap(space.Px8).AlignItems(ui.Center)

	// Biu 的 `window-no-drag`（`index.tsx:22`）：建议浮层开着的时候整条不拖。
	//
	// 读的是**上一帧**的 searchSuggest：viewSearchBox 在 Children 里才跑，那一刻
	// 还没法知道这一帧的结果。一帧的差在这里没有后果——刚点开浮层的那一帧顶栏
	// 还能拖，从第二帧起就不拖了。
	if !s.searchSuggest {
		nav = nav.DragWindow()
	}

	nav.Children(func() {
		s.viewNavBack(c, back)
		s.viewNavForward(c, back)
		s.viewSearchBox(c)
		// 中间那一段空处也是拖动区：这一列把搜索框和右边那几个按钮推开，
		// 指针落在这里时命中的就是它和外层 Row。
		ui.Column(c).Grow(1).DragWindow()
		// 阶段 1 唯一的入口：选一个本地文件。Biu 里对应侧栏的「本地音乐」。
		// 走的是播放条「加歌」那一条路：多选的文件全都进队列，用它打开的歌
		// 上一首/下一首才是亮的（直接调 app.Open 就进不了队列，见 pickAndEnqueue）。
		if s.withTip(c, iconButton(c, iconFolder, "打开文件", iconMd).
			Size(iconHitMd, iconHitMd).Padding(space.None, space.Px7), "打开本地音频文件").Clicked() {
			s.pickAndEnqueue()
		}
		//
		// ⚠️ **这里原来还有两样，都搬走了**（票 #127）：
		//
		//   - **页名**：每一页自己都写着标题（设置 / 下载记录 / 历史记录…），
		//     顶栏再写一遍就是同一个词在屏幕上出现两次。上游的顶栏压根没有
		//     这一行（`layout/navbar/index.tsx:31-34`：左边只有
		//     `Navigation` + `Search`）——「我在哪」是侧栏高亮那一项的事。
		//   - **播放列表开关**：上游在播放条右边那一组（`playbar/right/index.tsx:16`），
		//     它当初在这儿是因为右边挤不下。两处都留着就是同一个开关两个位置，
		//     而它们会不一致（顶栏那枚在播放条被满屏盖住时还点得到）。
		// 迷你播放器的开关。Biu 那边在窗口按钮那一排里（`components/window-action/
		// index.tsx:46` 的 PictureInPicture 图标），而我们那条顶栏是自己画的，
		// 所以摆在右边这一列里——和「窗口变小了」这件事一样是窗口级动作。
		//
		// ⚠️ **顶栏是拖动区**（`nav.DragWindow()`），而这一句在它的 Children
		// 里，所以按它就是按它、不会变成拖窗口（见本文件文件头那一段）。
		//
		// ⚠️ **它原来是文字键**（「迷你」）。图标集补齐之后这一格接上了
		// `iconPiP`——**那时候这一排里只有它不是图标键**，看上去像忘了换
		// （#134 的第七单元点名了这个）。文字没有消失：`Label` 和悬停提示
		// 说的是同一件事，读屏软件和测试都还叫得出它。
		//
		// （票 #289 拿掉顶栏那一枚「设置」之后，这一排**全是图标键**了。）
		if s.withTip(c, iconButton(c, iconPiP, "迷你", iconMd).
			Size(iconHitMd, iconHitMd).Padding(space.None, space.Px7),
			"缩成 320×100 的小窗贴边悬浮，音乐不停").Clicked() {
			s.toggleMiniPlayer()
		}
		// ⚠️ **这里原来还有一枚「设置」，票 #289 拿掉了**（报告 C-02）。
		//
		// **为什么它是纯冗余**：上游的设置在**侧栏菜单**和**头像下拉**里
		// （`biu/src/layout/side/index.tsx`、`biu/src/layout/navbar/user/
		// index.tsx:107`），**顶栏没有**。我们这边侧栏有一条（我们自己多摆的，
		// DESIGN.md §九「侧栏多一条设置」）、头像下拉里也有第三条
		// （`navbar_avatar.go` 的 `avatarMenuItems`，第三项就是「设置」），
		// 顶栏这一枚是**同一个动作的第三个入口**。
		//
		// **三个入口为什么是坏事**：它们会**不一致**——侧栏那一条会被「菜单设置」
		// 里的「隐藏菜单项」藏起来（`sidebar.go` 的 `hiddenMenuKeys`），头像下拉
		// 那一条不会；而顶栏这一枚谁也藏不掉。用户按「设置」能到设置页，可他在
		// 侧栏上找不到那一行，于是以为侧栏坏了。
		//
		// ⚠️ **这一票只拿掉重复的那一份**：侧栏那一条**留着**（DESIGN.md §九
		// 已经拍板「侧栏多一条设置」，那是我们的入口，不是上游那一套），
		// 头像下拉那一条也留着（那是上游自己的位置）。想改入口位置是另一票的
		// 事——**挪入口不是纯对齐，要人拍板**。
		s.viewDynamicDot(c)
		s.viewAvatar(c)
		// 登录 / 退出登录那个入口（login.go 的 viewLoginEntry）。它在 router.go
		// 那一行末尾——顶栏那一行是从那儿搬过来的，所以这一句也跟着搬过来。
		// 放在头像**旁边**而不是头像**里面**：头像是原生菜单的触发点，
		// 按下是弹菜单不是切路由（见 navbar_avatar.go）。
		s.viewLoginEntry(c)
	})
}

// viewNavBack / viewNavForward 是顶栏左边那两个箭头。
//
// ⚠️ **留着 `ui.BackButton` / `ui.ForwardButton`，不换成图标按钮**——这不是省事，
// 是换不了：框架那一支按下去除了 `r.Go(±1)`，还挂着**右键的「去过那些页」**列表
// （`mygo/ui/router.go:799-808`，照 Biu 浏览器工具栏那样）。而那一列要读历史的
// 路径和标题，`Router` 的 `entries` / `at` 是**私有**的（`Router` 上只有
// `CanGoBack` / `Go` / `Path`），所以自己重画一个按钮只能把那份列表丢掉。
//
// **拿功能去换图标样式是反的**（issue 93 的约束：不弄坏已经能用的东西）。所以
// 这里是这一票里**唯一没换成图标资源**的一处：那个箭头是框架用 `Draw` 画的
// 一段 path，不是 SVG。它满足验收的实质那几条——单色、按文字色走（`t.Text` /
// `t.TextMuted`）、`Padding(Space(1.5), Space(2))` 撑着比图形大的点击区。
//
// 这里改的只有**名字**：框架给的是英文的 "Back" / "Forward"，中文界面里读屏
// 念出来是英文。
//
// ## 形状：照上游的 32×40、无描边（票 #289 / 报告 C-03）
//
// 框架那一支出来是 **34×30、带一圈 1 像素描边**（`styleButton` 给的是
// `t.Border` + `t.Surface`）。上游那一枚是 **32×40、无描边**。所以这里在框架
// 那一支上覆盖两样：**盒子钉成 32×40**、**描边去掉**。
//
// ⚠️ **前进键留着**（上游没有前进键）：砍掉它就是拿功能换样式——框架那一支
// 挂着 `r.Go(1)` 和它的「去过那些页」列表，而上游没有前进键是**上游自己的
// 浏览器历史模型**决定的，不是我们该抄的那一半。剩下的差别写在这一票的收尾
// 说明里，等人拍板。
func (s *Shell) viewNavBack(c *ui.Context, back func()) {
	if s.withTip(c, navArrowStyle(ui.BackButton(c, s.router).Label(navBackLabel)),
		navBackLabel).Clicked() {
		back()
	}
}

func (s *Shell) viewNavForward(c *ui.Context, back func()) {
	if s.withTip(c, navArrowStyle(ui.ForwardButton(c, s.router).Label(navForwardLabel)),
		navForwardLabel).Clicked() {
		back()
	}
}

// navArrowStyle 把框架那一支后退/前进改成上游的样子：**盒 32×40、无描边**。
//
// ⚠️ **只改外观，不改行为**：框架那一支还挂着 `r.Go(±1)` 和右键的「去过那些页」
// 历史列表（理由见 viewNavBack 顶上那段）。
//
// ⚠️ **描边是「去掉」而不是「换成别的颜色」**：`Border(0, …)` 把宽度钉成 0，
// 框架就不画它了（`mygo/ui/paint.go:471` 的 `scene.HasBorder`）。留一个宽度
// 0 的描边和一个没有描边，在画面上一模一样，但在**布局**上差 2 像素——
// 描边是「占在盒子里面」的（CSS 的 border-box），留着它盒子就变成 34×42。
func navArrowStyle(e ui.Element) ui.Element {
	return e.Size(navArrowW, navArrowH).Border(0, ui.Color{})
}
