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

// navMinHeight 是顶栏多高。Biu 的导航栏是 64px（`h-full`，由外层给），
// MyGo 的隐藏标题栏只让出 `bar.Height` 那一条；32 是 MyGo 自己的例子里的下限
// （`ui/context.go:334`），矮了字会被切。
const navMinHeight = 32

// viewNav 是顶部导航栏：后退/前进 + 页名 + 搜索框 + 打开文件 + 动态红点 + 头像。
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
		if ui.BackButton(c, s.router).Clicked() {
			back()
		}
		if ui.ForwardButton(c, s.router).Clicked() {
			back()
		}
		// 页名。Biu 的顶栏没有这一行（那儿是侧栏高亮当前项），这是骨架里
		// 留下来的：切路由时它是最省事的一处「我在哪」。
		ui.Text(c, lookup(s.router.Path()).Title).FontSize(15).Bold()
		s.viewSearchBox(c)
		// 中间那一段空处也是拖动区：这一列把搜索框和右边那几个按钮推开，
		// 指针落在这里时命中的就是它和外层 Row。
		ui.Column(c).Grow(1).DragWindow()
		// 阶段 1 唯一的入口：选一个本地文件。Biu 里对应侧栏的「本地音乐」。
		// 走的是播放条「加歌」那一条路：多选的文件全都进队列，用它打开的歌
		// 上一首/下一首才是亮的（直接调 app.Open 就进不了队列，见 pickAndEnqueue）。
		if ui.Button(c, "打开文件").Clicked() {
			s.pickAndEnqueue()
		}
		// 播放列表抽屉的开关。Biu 把它放在播放条右边
		//（`layout/playbar/right/index.tsx`），我们放这儿——**播放条右边放不下**：
		// 那三个键（音量 120 + 倍速 64 + 加歌 56）已经把 88 高的一行占满了，再加
		// 一个就把歌名挤成一个字一行（见 playbar.go 里那一段的注释）。
		//
		// 紧挨着「打开文件」：两个都是「往队列里放东西 / 看队列」这一类。
		if ui.Button(c, "播放列表").Clicked() {
			s.drawerOpen = !s.drawerOpen
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
