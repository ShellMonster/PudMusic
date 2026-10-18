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
// **我们不按登录态隐藏**，六项一直都在：
//
//   - 菜单的**形状**不随登录态变来变去。用户点开下拉，六个位置六个名，记住的
//     是「第 2 项是个人资料」，而不是「有时候有有时候没有」。
//   - Biu 那一套「按登录态隐藏」的另一面是：显示出来的那些项点了都有反应。
//     这里六项都按得动，做不到的那几项把**为什么**说出来。
//
// 「登录」那一项在已登录时也还是进扫码登录页：那一页底下有「已登录（UID …）」
// 和退登按钮（见 login.go 的 viewAccountFooter），所以它也是一个说得清
// 「你现在是什么状态」的地方。
//
// ## 登录态的**唯一**来源是 app，不是这个包
//
// 「有没有登录」只有 `app.App` 知道（`LoggedIn()` / `Session()`，凭据在钥匙串
// 里，界面这一层根本碰不到）。这一层**不存第二份**——存两份的后果是退登之后
// 头像还显示着上一个人的样子，而界面上没有任何一个地方会提示「没退干净」。
package ui

import (
	"github.com/egoist/mygo/ui"
)

const (
	// avatarSize 是头像多大。Biu 是 `size="sm"` 加 `mr-4`，具体值没露出来；
	// 28 是顶栏这个高度里放得下、又不抢戏的一个数。
	avatarSize = 28

	// avatarLabel 是头像的无障碍名字，也是测试点得到它的凭据——无头 Tester 的
	// `Find` 找的是「显示着这个字或者标着这个名字」的元素。
	avatarLabel = "用户头像"

	// avatarInitial 是头像里那个字，**只用在取失败那一支**（红底 + 它）。
	//
	// ⚠️ **恒是「B」**（应用自己的名字）：Biu 那个首字母来自用户昵称，而昵称和
	// 头像地址都在 B 站的用户资料接口里，那一票还没接。登录态里现在只有
	// SESSDATA / bili_jct / DedeUserID 那几样（见 `app.Session`），拿 UID 的
	// 头一个字当头像是在**编**一个用户从没见过的东西，不干。
	//
	// 「还没地址」那一支画的是 `iconUser`（一个「人」），不是它（见
	// viewAvatarFace）。
	avatarInitial = "B"

	// feedbackURL 是「问题反馈」打开的地址。
	//
	// Biu 指的是它自己的仓库（`user/index.tsx:138`）。这里指我们自己的——
	// 用户在 MyGo 里点「问题反馈」，要能开到能提 MyGo 的那个地方去。
	feedbackURL = "https://github.com/ShellMonster/Biu_Go/issues"
)

// avatarMenuItem 是下拉里的一项。
//
// run 收到 Shell 和 Context：换路由的用前者（`navigate`），交给系统开地址的用
// 后者（`Context.OpenURL`）。**不许给 nil**——做不了的那几项也要有 run，只不过
// 它做的是「告诉用户为什么」，而不是一声不响。
type avatarMenuItem struct {
	label string
	run   func(s *Shell, c *ui.Context)
}

// avatarMenuItems 是头像下拉的那六项，逐条照 Biu 的
// `src/layout/navbar/user/index.tsx:88-158`。**顺序就是 Biu 的顺序。**
//
// Biu 那六条的 key 依次是 login / profile / settings / refresh / feedback /
// logout，对着一数就知道一条没漏、一条没编。
var avatarMenuItems = []avatarMenuItem{
	{label: "登录", run: func(s *Shell, _ *ui.Context) { s.goLogin() }},
	{label: "个人资料", run: func(s *Shell, _ *ui.Context) { s.goProfile() }},
	{label: "设置", run: func(s *Shell, _ *ui.Context) { s.navigate(settingsPath) }},
	{label: "刷新数据", run: func(s *Shell, _ *ui.Context) {
		// 收藏夹、关注那些都还没有本地副本，「刷新」没有东西可刷。
		// 说清楚比按了没反应好——后者用户只会当成应用卡了。
		s.addToast("刷新数据：还没联过 B 站接口，没有数据可刷新")
	}},
	{label: "问题反馈", run: func(_ *Shell, c *ui.Context) {
		// 用 `Context.OpenURL` 而不是 `Window.Open`：后者收到 nil 会直接
		// SIGSEGV（见 wake.go 的规则二），前者是给 view 用的那一条。
		c.OpenURL(feedbackURL)
	}},
	{label: "退出登录", run: func(s *Shell, _ *ui.Context) {
		if !s.app.LoggedIn() {
			s.addToast("还没登录，没有可退的")
			return
		}
		// 走 login.go 那一支：它退完会把界面收回未登录的样子，退不干净
		// （钥匙串锁了）也会说出来。
		s.logoutAndStartOver()
	}},
}

// goProfile 去「个人资料」那一页。Biu 是 `navigate(`/user/${user?.mid}`)`
// （`user/index.tsx:101`），mid 在这边是登录态里的 DedeUserID。
//
// 没登录就没有 mid——这时候给一条 `/user/` 空着的路径等于把用户送进 404，
// 说一句「还没登录」诚实得多。
func (s *Shell) goProfile() {
	mid := s.app.Session().DedeUserID
	if mid == "" {
		s.addToast("还没登录，个人资料用不了")
		return
	}
	s.navigate("/user/" + mid)
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
					item.run(s, c)
				}
			}
		})
		face.Children(func() { s.viewAvatarFace(c) })
	})
}

// viewAvatarFace 是头像那一格里的图。
//
// 走 imgcache 的四态：好了画图；**失败画首字母 + 红底**；其余（空地址、还在下）
// 画一个「人」的图标。
//
// ⚠️ **地址这一版恒为空串**：头像地址在 B 站的用户资料接口里，那一票还没接。
// 缓存这一层**已经接好**：地址一有值，四态自己就生效，下面几行一个字都不用动。
//
// ⚠️ 空地址那一支画的是图标**不是首字母**（issue 93 的验收：头像格要有图标）。
// 首字母留给了「取失败」那一支：红底 + 一个字母是「这里出了点事」，而一个
// 灰底上的人形是「这里还没有」——两件事给两种样子，用户不会把出错的当成空的。
//
// ⚠️ 这里读的是 `imgcache.Cache` 而不是直接调 `imgcache.View`：那个 View 的两种
// 失败态画的是「载入中」「封面加载失败」两行字（见 imgcache/view.go 的
// drawCover），那是给**封面**那个尺寸用的。头像只有 28 宽，那两行字塞进去会被
// 裁成一团看不出是什么的色块——比一个人形糟糕得多。缓存本身（下载、去重、
// 解码、唤醒）还是共享的那一份。
func (s *Shell) viewAvatarFace(c *ui.Context) {
	t := c.Theme()
	size := float32(avatarSize)
	st := s.images.Get("")

	if st.Ready() {
		ui.Image(c, st.Bitmap).Fit(ui.Cover).Size(size, size)
		return
	}
	// 失败和「还没下好」要分得开：混成一个色，用户只会以为这个头像本来就是空的，
	// 等网络好了也不会再看它一眼（imgcache 那边也是这么分的，见 drawCover）。
	if st.Err != nil {
		ui.Column(c).Size(size, size).Center().
			Background(t.Danger.Alpha(0.18)).Children(func() {
			ui.Text(c, avatarInitial).FontSize(12).Bold().TextColor(t.Text)
		})
		return
	}
	ui.Column(c).Size(size, size).Center().Background(t.Surface).Children(func() {
		// 装饰：它不可点也不可读——点的是外面那个头像（它有 `avatarLabel`）。
		// 名字在那一格上就够了，这里再加一个就成了两句话说同一件事。
		ui.Icon(c, icon(iconUser)).FontSize(16).TextColor(t.TextMuted)
	})
}
