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
// 六项里「刷新数据」现在是真的做那件事（重新拉一次侧栏的收藏夹，见
// `app/favorites_refresh.go`）；它拒绝的时候也说话，而那几句话是在应用层说的
// （那一层才知道有没有登录、收藏夹那一摊挂上没有），这一层不另写一份。
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
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"sync"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/ui"
)

const (
	// avatarSize 是头像多大。Biu 是 `size="sm"` 加 `mr-4`，具体值没露出来；
	// 28 是顶栏这个高度里放得下、又不抢戏的一个数。
	//
	// ⚠️ **改这个数要连着下面那一堆比例一起看**：人形剪影的每一块都是它的分数
	// 倍，而 `navMinHeight` 是 32——再大一点剪影就被顶栏切了。
	avatarSize = 28

	// avatarLabel 是头像那个**可点的方块**的无障碍名字，也是测试点得到它的
	// 凭据——无头 Tester 的 `Find` 找的是「显示着这个字或者标着这个名字」的
	// 元素。
	//
	// ⚠️ **这一句是菜单按钮的名字，不随登录态变**。变的名字在里层那一格上
	// （`avatarFaceLabel`），理由见那一处的注释。
	avatarLabel = "用户头像"

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
		// 真的是重新拉一次侧栏那些收藏夹（`app/favorites_refresh.go`）。
		// 之前这一项只弹一句「还没有数据可刷新」，而接口是有的——用户改了收藏夹
		// 切回来看不到，说的却是「这里没有数据」。
		//
		// ⚠️ **这里只发起，不等**：网络那一趟在后台，而这一项的菜单**选完就关**，
		// 没有任何地方可以转圈。进行中状态是侧栏那一行「正在刷新收藏夹…」
		// （见 sidebar_favorites.go），跑完的那句话走 `reportFavoritesRefresh`。
		//
		// 已经有一趟在飞时应用层会挡下来，并把「正在刷新收藏夹…」说给用户听——
		// 连点两下只发一次请求，而第二下不至于毫无动静。
		s.app.RefreshFavorites()
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

// ─────────────────────────────────────────────────────────────────────────────
// 头像那一格的状态
// ─────────────────────────────────────────────────────────────────────────────

// avatarLook 是这一帧该画成什么样。
//
// ⚠️ **只有三档，而它们分的是「画什么」不是「为什么」**。为什么在 `note` 里。
// 拆成「未登录一种、载入中一种、没有头像一种、失效一种」四五个相位看起来更齐，
// 可它们在 28 宽的一格里**画出来是同一样东西**——那个人形。相位拆得再细，
// 界面上分不出来，而「界面上分不出来的两种状态用两种相位记着」正是下一次
// 有人把它们合并时找不到证据的那一票。
//
// 该分开的是**看得见**的那三档：人形、图、出问题的人形。
type avatarLook int

const (
	// avatarSilhouette 是那个人形。**没登录、图还在路上、这个账号压根没头像
	// 都是它**——那三种在界面上本来就没有分别。
	avatarSilhouette avatarLook = iota
	// avatarPicture 是封面缓存给的那张真头像。
	avatarPicture
	// avatarTrouble 是出问题了：人形染成告警色，`note` 说清是什么问题。
	avatarTrouble
)

// avatarState 是顶栏右上角那一格跨帧要记的东西。
//
// ⚠️ 跨帧是因为这一格每一帧都会被重画，而头像地址是**问一次**来的——每一帧
// 问一次就是每一帧一个请求（理由见 app/accountnav.go 的文件头）。
type avatarState struct {
	mu sync.Mutex

	// acted 记着「这一格已经按现在这个登录态做过决定了」。它让 `avatarSync`
	// 每帧被叫到时只做一次判断，而不是每帧一次写。
	acted bool
	// loggedIn / mid 是「acted 那一次」面对的是什么。
	//
	// ⚠️ **这一份是界面状态，不许拿它当登录判据**——登录判据只有
	// `App.LoggedIn()`。存这一份只是为了认出「换人了/变了」，好让在途的那一趟
	// 作废。
	loggedIn bool
	mid      string

	// gen 是代次。换一次账号 / 退一次登录就 +1，后台那一趟回来时对不上就闭嘴
	// （和 runID 那套一样，见 `internal/app/account.go` 的注释）。
	gen uint64

	look avatarLook
	// face 是头像地址（空串 = 这一档画不了图）。
	face string
	// note 是给用户看的一句交代。它是这一格的无障碍名，也是鼠标悬停时的那一行。
	note string
}

// snapshot 拿一份拷贝。**每一帧都调**，所以它只锁一次、只拷几个字段。
func (a *avatarState) snapshot() (look avatarLook, face, note string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.look, a.face, a.note
}

// avatarSync 让那一格追上此刻的登录态。**每一帧都会被 view 叫到**
// （`viewAvatar` 的第一句），所以它必须满足两件事：
//
//  1. 没换过登录态时**一个请求都不发**——这里是整个应用重画最勤的一处。
//  2. 换了登录态时**至多发一个**，而且退登那一支**一个都不发**。
//
// 「至多一个」靠 `acted` + `mid`，「在途的那一趟作废」靠 `gen`。两条各自都不够：
// 只有 `acted` 的话，退登之后在途的那一趟会把上一个账号的头像写回来。
func (s *Shell) avatarSync() {
	// ⚠️ `closed` 那道闸门和三个装定时器的地方是同一道（见 shell.go 的字段注释）：
	// 无头 Tester 里 `Close` 之后再 `Frame()` 就会调到这一句，而那时候
	// `AccountNav` 打的是**一个已经关掉的应用**。
	if s.closed {
		return
	}

	loggedIn := s.app.LoggedIn()
	mid := ""
	if loggedIn {
		mid = s.app.Session().DedeUserID
	}

	a := &s.avatar
	a.mu.Lock()
	if a.acted && a.loggedIn == loggedIn && a.mid == mid {
		a.mu.Unlock()
		return
	}
	a.acted, a.loggedIn, a.mid = true, loggedIn, mid
	a.gen++
	gen := a.gen
	// 先摆成「还没问出来」的样子：登录了就是人形等着，登录都没登录是另一句
	// 交代。**不摆一个错的**——退登那一刻界面上如果还挂着上一个账号的头像，
	// 用户会以为退没退干净。
	a.look, a.face = avatarSilhouette, ""
	if loggedIn {
		a.note = avatarNoteWaiting
	} else {
		a.note = avatarNoteGuest
	}
	a.mu.Unlock()

	if !loggedIn {
		// ⚠️ **这一支一个请求都不发**。理由和闸门的位置见
		// `app/accountnav.go` 的文件头（401 噪声 + 每帧）。
		return
	}
	go s.fetchAvatarFace(gen)
}

// fetchAvatarFace 在后台问一次「B 站怎么看我这个账号」，回来之后落进状态。
//
// gen 是发出那一趟时的代次；回来时对不上就**什么都不做**——用户在这中间退登了
// 或者换了账号，而落地之后界面不会自己再画一帧（`gen` 对不上时没有状态变化，
// 没有变化就没有需要唤醒的理由）。
func (s *Shell) fetchAvatarFace(gen uint64) {
	var (
		nav app.AccountNav
		err error
	)
	// 崩了也要有一个交代，不能让这一格永远停在「等」。
	// 抄 follow.go 的那一支（同样的 defer/recover 形状）。
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("biu: 拉用户信息崩了：%v\n%s", rec, debug.Stack())
				err = fmt.Errorf("拉用户信息的代码崩了：%v", rec)
			}
		}()
		nav, err = s.app.AccountNav(context.Background())
	}()

	a := &s.avatar
	a.mu.Lock()
	if a.gen != gen {
		a.mu.Unlock()
		return
	}
	switch {
	case err != nil:
		a.look, a.face = avatarTrouble, ""
		a.note = "问不出 B 站那边的账号信息：" + err.Error()
	case !nav.LoggedIn:
		// ⚠️ 这一档**不能**画成「未登录」：本地是有 SESSDATA 的（正是它让这一趟
		// 发得出去），画成未登录用户会以为是应用把他退了。而画上一个人的头像
		// 更糟——那是上一个**有效**会话的东西。理由见 `app/accountnav.go` 里
		// `AccountNav.LoggedIn` 那段。
		a.look, a.face = avatarTrouble, ""
		a.note = "登录已失效：B 站那边不认这个会话了，重新登录一次就好"
	case nav.Face == "":
		a.look, a.face = avatarSilhouette, ""
		a.note = "这个账号没有头像"
	default:
		// ⚠️ **note 在这一档是空的，而且必须空**：地址有了而图好了的时候画出去的是
		// **那张图**，图自己就是交代，一个字都不占那 28 见方。留着 note 的作用
		// 只是给 `viewAvatarFace` 的另一支兜底，而那一支自己会盖掉它。
		//
		// 顺带记下一件真的没有做的事：**这一档不重试**。nav 问失败（刚开机时网络
		// 还没起来是最常见的）之后那一格就一直停在「出问题了」，要等换账号或者
		// 重开应用。补一个重试钟是另一笔账（三个装定时器的地方，见 shell.go 的
		// 字段注释），这一票的验收没有要它。
		a.look, a.face, a.note = avatarPicture, nav.Face, ""
	}
	a.mu.Unlock()

	s.wake.invalidate()
}

// 那一格无障碍名 / 悬停说明用的几句话。
//
// ⚠️ **它们不是装饰**：这一格只有 28 见方，装不下几句话，而「这一格是什么意思」
// 是读代码以外的人唯一能拿到的交代。屏幕阅读器读 `Label`，鼠标用户读 `Tooltip`，
// 测试读 `Find`——三条路读的是同一句话。
const (
	avatarNoteGuest   = "未登录"
	avatarNoteWaiting = "头像加载中"
	// avatarNoteNoPicture 是「**地址有了而图没下来**」那一档的交代。
	//
	// ⚠️ 它和「压根没有地址」是两件事：这一档网络好了会自己好，那一档永远不会。
	// 后者的那一句在 `fetchAvatarFace` 里按 B 站的返回现写，因为它要从返回值
	// 上才分得出来。
	avatarNoteNoPicture = "头像没加载出来（多半是网络）"
)

// viewAvatar 是头像 + 它的下拉。
func (s *Shell) viewAvatar(c *ui.Context) {
	// 每一帧都问一句「登录态变了吗」——判据全在 `avatarSync` 里，那一句只做
	// 两次比较。放在 view 而不是登录/退登的回调里，是因为**这一格必须自己
	// 追得上登录态**：登录成功发生在扫码那一页、退登发生在系统菜单的回调里，
	// 而这个外壳还会被别的地方 `TestOnlyAdoptLogin` 装一份登录态（测试）。
	s.avatarSync()

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
// 它只管**一张图怎么样了**（好了 / 在路上 / 下不下来）；「有没有地址」「登录态
// 怎么了」在 `fetchAvatarFace` 里。两边合起来四种情形，而且**四种互不合并**：
// 「未登录」和「已登录」必须是两回事（issue #95 的第三条验收），而「图没下来」
// 和「压根没有地址」也不是一回事——前者网络好了会自己好，后者永远好不了。
//
// ⚠️ 这里读的是 `imgcache.Cache` 而不是直接调 `imgcache.View`：那个 View 的两种
// 失败态画的是「载入中」「封面加载失败」两行字（见 imgcache/view.go 的
// drawCover），那是给**封面**那个尺寸用的。头像只有 28 宽，那两行字塞进去会被
// 裁成一团看不出是什么的色块——比一个讲得清的东西糟糕得多。
func (s *Shell) viewAvatarFace(c *ui.Context) {
	side := float32(avatarSize)
	look, face, note := s.avatar.snapshot()

	// 没有地址：这一格归状态那一层说，缓存压根不用问（它对空地址不表态，
	// 而 `Get("")` 会拿空串当一个键去查——那是个永远下不下来的地址）。
	if face == "" {
		drawAvatar(c, look, note)
		return
	}

	// 失败和「还没下好」要分得开：混成一个色，用户只会以为这个头像本来就是空的，
	// 等网络好了也不会再看它一眼（imgcache 那边也是这么分的，见 drawCover）。
	switch st := s.images.Get(face); {
	case st.Ready():
		ui.Image(c, st.Bitmap).Fit(ui.Cover).Size(side, side)
	case st.Err != nil:
		// ⚠️ **这一档不能摆成空白**——28 见方的一格空白在顶栏上是一个看不出是
		// 什么的洞，而用户真正缺的不是那张图，是「为什么没有」。所以染成告警色
		// 并且把那句话挂上去。
		drawAvatar(c, avatarTrouble, avatarNoteNoPicture)
	default:
		// 有地址、在路上：这一帧画的是人形，而说的是「加载中」——和未登录
		// 画出来一样、说的话不一样（理由见 avatarLook 那一段）。
		drawAvatar(c, avatarSilhouette, avatarNoteWaiting)
	}
}

// drawAvatar 按那一档的样子画。**三档都在这里定死**，view 不再插嘴。
func drawAvatar(c *ui.Context, look avatarLook, note string) {
	t := c.Theme()
	side := float32(avatarSize)

	bg, ink := t.Surface, t.TextMuted
	if look == avatarTrouble {
		bg, ink = t.Danger.Alpha(0.18), t.Danger
	}

	// ⚠️ `Label` 和 `Tooltip` 用的是**同一句话**：这一格只有 28 见方，写不下
	// 「这个账号没有头像」那几句；而这三样（无障碍名、悬停说明、测试点得到的
	// 凭据）读的是同一件事，给它们三份不同的措辞只会让其中一份开始说谎。
	ui.Column(c).Size(side, side).Center().Background(bg).
		Label(note).Tooltip(note).
		Children(func() { avatarPerson(c, side, ink) })
}

// avatarPerson 画那个人形：一个圆脑袋 + 一块肩膀。
//
// 为什么画人不画字：这一格没登录时**没有一个字是诚实的**。原来的「B」是应用
// 自己的名字（见老注释），用户看到的是一个占位符坏了；换成昵称头一个字的话，
// 那一格在没登录时显示的是**别人的名字的第一个字**——那更糟。用户资料接口
// 明明能给出真实头像，于是没登录就该说「这儿没有人」，而人这个形状在 28 见方
// 里是认得出来的。
//
// ⚠️ **每一块都是 side 的分数倍**，所以换 `avatarSize` 时它跟着等比缩放。
// 肩膀那一块压在底边上，是被外层那个 `Clip()` 切出来的弧——不是它自己带弧。
func avatarPerson(c *ui.Context, side float32, ink ui.Color) {
	const (
		headD    = 0.34 // 脑袋的直径
		shoulder = 0.60 // 肩膀的宽
		torso    = 0.28 // 肩膀的高
		gap      = 0.06 // 脑袋和肩膀之间留的那一条
	)
	head := side * headD
	ui.Box(c).Size(head, head).Radius(head / 2).Background(ink)
	ui.Box(c).Size(side*shoulder, side*torso).Radius(side*0.15).
		Margin(side*gap, 0, 0, 0).Background(ink)
}
