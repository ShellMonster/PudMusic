// 顶栏右边的头像 + 下拉菜单：Biu 的 N-08
// （`biu/src/layout/navbar/user/index.tsx`）。
//
// ## 用系统原生菜单，不自绘
//
// MyGo 的 `Element.Menu` 给的是**系统菜单**（`ui/menu.go:64` → `rt.openMenu`），
// 自带键盘导航、系统外观，和别的应用的菜单一模一样的手感。自绘一个浮层要重做
// 这四件事，还做不对其中任何一件。
//
// Biu 那一排菜单项**带图标**（`startContent: <RiSettings3Line size={18}/>`）。
// MyGo 的 `MenuItem` 只有 label / disabled / checked / shortcut / sub
// （`ui/menu.go:131-140`），**没有图标这一格**——照抄不了，也就不画。PLAN.md
// §阶段 4 的风险表里已经写了这一条（「右键菜单项没有图标」）。
//
// ## 六项一项不少，顺序照 Biu
//
// Biu 的 `dropdownItems` 是六条，其中「登录」在登录后隐藏、「个人资料」和
// 「退出登录」在未登录时隐藏（`user/index.tsx:88-158` 的 `hidden`）。
// **我们不按登录态隐藏**：这一版还没有登录（扫码登录是阶段 3 的 3.5）。照它
// 隐藏的话，未登录的用户看到的下拉里只有四项——而且菜单会随着「以后会不会
// 登录」变来变去，比一个固定六项、其中几项说清楚「还没登录」更难认。
//
// 代价说清楚：未登录时「登录」「个人资料」「退出登录」按下去不会跳走，只会告诉
// 用户还没登录。这是这一版的实话，不是假装能用。
package ui

import (
	"github.com/egoist/mygo/ui"
)

const (
	// avatarSize 是头像多大。Biu 是 `size="sm"` 加 `mr-4`，具体值没露出来；
	// 28 是顶栏这个高度里放得下、又不抢戏的一个数。
	avatarSize = 28

	// avatarLabel 是头像的无障碍名字，也是测试点得到它的凭据——无头 Tester
	// 的 `Find` 找的是「显示着这个字或者标着这个名字」的元素。
	avatarLabel = "用户头像"

	// feedbackURL 是「问题反馈」打开的地址。
	//
	// Biu 指的是它自己的仓库（`user/index.tsx:138`）。这里指我们自己的——
	// 用户在 MyGo 里点「问题反馈」，要能开到能提 MyGo 的那个地方去。
	feedbackURL = "https://github.com/ShellMonster/Biu_Go/issues"
)

// navUser 是头像下拉认的那个人。
//
// 这一版**还没有登录**：扫码登录是阶段 3 的 3.5。所以 loggedIn 恒为 false，
// 头像画的是首字母（imgcache 的空态）。字段先立好，接上登录时只要有人往这里
// 填，菜单和头像都不用改。
type navUser struct {
	loggedIn bool
	// mid 是 B 站的用户号，`/user/{id}` 那条路由要的就是它。
	mid string
	// face 是头像图地址，交给 imgcache 走那四态。
	face string
	// name 是显示名，头像没图时取它的第一个字。
	name string
}

// avatarMenuItem 是下拉里的一项。
//
// 三种去处一个都不含糊：**换路由**、**交给系统打开一个地址**、
// **这一版还做不到**。最后一种按下去要说清楚为什么——一个按了没反应的菜单项，
// 用户只会当成应用坏了。
type avatarMenuItem struct {
	label string
	// to 算它该去的路由。个人资料那条要现算（得知道 mid），所以是函数。
	// nil 表示这一项不换路由。
	to func(u navUser) string
	// url 是交给系统打开的地址。和 to 二选一。
	url string
	// note 是还做不到时要说给用户听的那句话。
	note string
}

// avatarMenuItems 是头像下拉的那六项，逐条照 Biu 的
// `src/layout/navbar/user/index.tsx:88-158`。**顺序就是 Biu 的顺序。**
//
// Biu 那六条的 key 依次是 login / profile / settings / refresh / feedback /
// logout，对着一数就知道一条没漏、一条没编。
var avatarMenuItems = []avatarMenuItem{
	{label: "登录", note: "扫码登录还没做，这一版只能离线听本地文件"},
	{label: "个人资料", to: func(u navUser) string { return userProfilePath(u) }},
	{label: "设置", to: func(navUser) string { return settingsPath }},
	{label: "刷新数据", note: "还没有联过 B 站接口，没有数据可刷新"},
	{label: "问题反馈", url: feedbackURL},
	{label: "退出登录", note: "这一版还没有登录"},
}

// userProfilePath 是「个人资料」要去的那条路由。
//
// Biu 是 `navigate(`/user/${user?.mid}`)`（`user/index.tsx:101`）。
// 没登录就没有 mid——这时候给一条 `/user/` 空着的路径等于把用户送进 404，
// 说一句「还没登录」诚实得多。
func userProfilePath(u navUser) string {
	if !u.loggedIn || u.mid == "" {
		return ""
	}
	return "/user/" + u.mid
}

// viewAvatar 是头像 + 它的下拉。
func (s *Shell) viewAvatar(c *ui.Context) {
	// Key 挂在外面这一格上：`.Menu()` 会把元素变成菜单按钮（`ui/menu.go:66`
	// 置 flagClickable / flagFocusable），Key 挂在这种元素上会 panic
	// （`ui/element.go:537`）。
	ui.Column(c).Key("avatar").Children(func() {
		face := ui.Box(c).Size(avatarSize, avatarSize).
			Radius(avatarSize/2).Clip().Border(1, c.Theme().Border)
		face.Label(avatarLabel).Menu(func(m *ui.Menu) {
			for _, item := range avatarMenuItems {
				if m.Item(item.label).Chosen() {
					s.runAvatarMenu(c, item)
				}
			}
		})
		face.Children(func() { s.viewAvatarFace(c) })
	})
}

// runAvatarMenu 是选中之后要做的事。
//
// 换路由的都走 `navigate`：它先把上一页的提示清掉、把崩过的状态复位，再弹
// 「这一页还没做」（见 router.go）。
func (s *Shell) runAvatarMenu(c *ui.Context, item avatarMenuItem) {
	switch {
	case item.url != "":
		// 用 `Context.OpenURL` 而不是 `Window.Open`：后者收到 nil 会直接
		// SIGSEGV（见 wake.go 的规则二），前者是给 view 用的那一条。
		c.OpenURL(item.url)
	case item.to != nil:
		if path := item.to(s.user); path != "" {
			s.navigate(path)
			return
		}
		s.addToast("还没登录，" + item.label + "用不了")
	default:
		s.addToast(item.label + "：" + item.note)
	}
}

// viewAvatarFace 是头像那一格里的图。
//
// 四态跟着 imgcache 走（同一个地址只下一次、只解一次）：好了画图，载入中和
// 失败都画首字母——Biu 那个 `<Avatar showFallback>` 就是这个意思
// （`user/index.tsx:166-173`）。空地址是第四态，同样是首字母。
//
// ⚠️ 这里读的是 `imgcache.Cache` 而不是直接调 `imgcache.View`：那个 View 的
// 两种失败态画的是「载入中」「封面加载失败」两行字（见 imgcache/view.go 的
// drawCover），那是给**封面**那个尺寸用的。头像只有 28 宽，那两行字塞进去
// 会被裁成一团看不出是什么的色块——比一个首字母糟糕得多。缓存本身（下载、
// 去重、解码、唤醒）还是共享的那一份。
func (s *Shell) viewAvatarFace(c *ui.Context) {
	t := c.Theme()
	size := float32(avatarSize)
	st := s.images.Get(s.user.face)

	if st.Ready() {
		ui.Image(c, st.Bitmap).Fit(ui.Cover).Size(size, size)
		return
	}
	// 失败和「还没下好」要分得开：混成一个色，用户只会以为这个头像本来就是空的，
	// 等网络好了也不会再看它一眼（imgcache 那边也是这么分的，见 drawCover）。
	bg := t.Surface
	if st.Err != nil {
		bg = t.Danger.Alpha(0.18)
	}
	ui.Column(c).Size(size, size).Center().Background(bg).Children(func() {
		ui.Text(c, avatarInitial(s.user)).FontSize(12).Bold().TextColor(t.Text)
	})
}

// avatarInitial 是头像里那个字。
//
// 没名字就用「B」——那是应用自己的名字。摆一个空框的话，头像那一格在顶栏上
// 就是个看不出是什么的洞。
func avatarInitial(u navUser) string {
	if u.name != "" {
		return firstRune(u.name)
	}
	return "B"
}
