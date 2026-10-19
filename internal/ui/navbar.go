// 顶部导航栏：Biu 的 N-01~N-04、N-07、N-08。
//
// 结构照 `biu/src/layout/navbar/index.tsx`：左（Navigation + Search）、
// 右（AppUpdateNotify + Dev + UserFeed + UserCard）。
//
// ## 整条是拖动区，这件事落在**外层 Row** 上，不是一个空的占位列
//
// Biu 是在那个 `div` 上按状态挂 `window-drag` / `window-no-drag`
// （`index.tsx:25-30`），两个 class 由 `isSearchFocused` 和
// `isUserDropdownOpen` 决定。MyGo 里对应的是 `Element.DragWindow()`。
//
// 落在外层 Row 上、而不是像骨架里那样单独留一列空的拖动占位，靠的是
// `ui/input.go:262` 的命中链：`target` 是链上**最内层**带输入标志的元素。
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
// （`biu/src/layout/index.tsx:32`），而那一格里的 navbar 写的是 `h-full`——
// 也就是「有多高就是多高」，而外面钉死的就是 64。我们原来是 32（MyGo 自己
// 例子里的下限，`ui/context.go:334` 的 `max(bar.Height, 32)`），矮了整整一半。
//
// 这一条不只是「好看一点」：顶栏是**内容区上面那一行**，它矮 32，内容区就
// 少 32 ——而 `settings_page_test.go` 里量得着的可视区上下两个数
// （`pageTop` / `pageBottom`）就是从它推出来的。
//
// 仍然取 `max(bar.Height, …)`：macOS 上红黄绿是浮在内容上的（`TitleBarHidden`），
// 那一块系统自己留的高度比 64 大的话，得让开它，不然字会压在红黄绿底下。
const navMinHeight = 64

// viewNav 是顶部导航栏：后退/前进 + 搜索框 + 打开文件 + 迷你 + 设置 + 动态红点 + 头像。
//
// ⚠️ **这里没有页名**（票 #127）。每一页自己写着标题，顶栏再写一遍就是同一个词在
// 屏幕上出现两次；上游的顶栏也没有（`layout/navbar/index.tsx:26-42`）。
// ⚠️ **这里也没有播放列表开关**：它在播放条右边那一组（`playbar.go` 的
// `viewPlayBarRight`）。理由见 Children 里那两条注释。
func (s *Shell) viewNav(c *ui.Context) {
	// 后退/前进也是换页：上一页的提示同样不该留着，崩过的状态同样该复位。
	back := func() { s.clearToasts(); s.crash = nil }

	// ⚠️ 隐藏标题栏的窗口里，红黄绿是叠在内容上面的。左边让出 bar.Left、
	// 右边让出 bar.Right，才不会把它们压住。
	bar := c.TitleBar()

	nav := ui.Row(c).Key("nav").Height(max(bar.Height, navMinHeight)).
		Padding(0, bar.Right+8, 0, bar.Left+8).
		Gap(8).AlignItems(ui.Center)

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
		if iconButton(c, iconFolder, "打开文件", iconMd).
			Size(iconHitMd, iconHitMd).Padding(0, 5).
			Tooltip("打开本地音频文件").Clicked() {
			s.pickAndEnqueue()
		}
		//
		// ⚠️ **这里原来还有两样，都搬走了**（票 #127）：
		//
		//   - **页名**：每一页自己都写着标题（设置 / 下载记录 / 历史记录…），
		//     顶栏再写一遍就是同一个词在屏幕上出现两次。上游的顶栏压根没有
		//     这一行（`layout/navbar/index.tsx:26-42`：左边只有
		//     `Navigation` + `Search`）——「我在哪」是侧栏高亮那一项的事。
		//   - **播放列表开关**：上游在播放条右边那一组（`playbar/right/index.tsx:13`），
		//     它当初在这儿是因为右边挤不下。两处都留着就是同一个开关两个位置，
		//     而它们会不一致（顶栏那枚在播放条被满屏盖住时还点得到）。
		// 迷你播放器的开关。Biu 那边在窗口按钮那一排里（`components/window-action/
		// index.tsx:46` 的 PictureInPicture 图标），而我们那条顶栏是自己画的，
		// 所以摆在右边这一列末尾——和「窗口变小了」这件事一样是窗口级动作。
		//
		// ⚠️ **顶栏是拖动区**（`nav.DragWindow()`），而这一句在它的 Children
		// 里，所以按它就是按它、不会变成拖窗口（见本文件文件头那一段）。
		//
		// ⚠️ **它还留着文字键，没跟着图标化**。图标化那一票的边界是「换图标、不重排
		// 界面」，而这一格是**新加的键**；而 `icons.go` 那份图标集是按票挑的（每个
		// 常量都要有一份 SVG，`icons_test.go` 逐个过 embed），里面没有画中画 / 小窗
		// 这一类。往那份清单里塞一个资源是另一样活（谁维护谁说了算），所以这里先
		// 用文字键——和左边那几个图标键并排，样子不齐但每个都点得着、都说得出是啥。
		if flatButton(c, "迷你").Tooltip("缩成 320×100 的小窗贴边悬浮，音乐不停").
			Clicked() {
			s.toggleMiniPlayer()
		}
		// 设置。Biu 的设置在侧栏菜单和头像下拉里（`layout/side/index.tsx`、
		// `navbar/user/index.tsx`），顶栏没有。⚠️ **这一段原来拿「顶栏收着播放条
		// 放不下的控件（播放列表开关就在这儿）」当理由**——票 #127 把那个开关搬回
		// 播放条之后，这一条论证就落空了，而它说的话**本身仍然成立**：设置是用户
		// 改任何东西都要走一遍的那个入口，藏在头像下拉第三项里是两级。
		// 侧栏里也有一条「设置」（那是我们自己多摆的，见 routes 表），顶栏这一枚
		// 是给已经在别的页上、不想先回侧栏的人用的。
		if iconButton(c, iconSettings, "设置", iconMd).
			Size(iconHitMd, iconHitMd).Padding(0, 5).
			Tooltip("设置").Clicked() {
			s.navigate(settingsPath)
		}
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
func (s *Shell) viewNavBack(c *ui.Context, back func()) {
	if ui.BackButton(c, s.router).Label(navBackLabel).Tooltip(navBackLabel).Clicked() {
		back()
	}
}

func (s *Shell) viewNavForward(c *ui.Context, back func()) {
	if ui.ForwardButton(c, s.router).Label(navForwardLabel).Tooltip(navForwardLabel).Clicked() {
		back()
	}
}
