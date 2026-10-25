// 顶栏右边的头像 + 它下面那一层下拉：Biu 的 N-08
// （`biu/src/layout/navbar/user/index.tsx`）。
//
// # 这一层是**自绘的浮层**，不是系统原生菜单
//
// 原来这里走的是 MyGo 的 `Element.Menu`（`ui/menu.go:64` → `rt.openMenu`，
// 系统菜单）。那一次的理由有两条，而**两条现在都不成立了**：
//
//  1. 「MyGo 的 Element.Menu 给的是系统菜单，自带键盘导航、系统外观，和别的
//     应用的菜单一模一样的手感。自绘一个浮层要重做这四件事，还做不对其中
//     任何一件」——那四件事是：点外面关、Esc 关、键盘能上下走、选中项有反馈。
//     现在**都做得到**：前两件是 `ui.PopoverBase` 那一层自己管的，后两件接在
//     头像那一枚按钮上（见 `viewAvatarMenuKeys`）。
//  2. 「Biu 那一排菜单项带图标（startContent: <RiSettings3Line size={18}/>），
//     MyGo 的 MenuItem 没有图标这一格，照抄不了，也就不画」——菜单项现在
//     **是我们自己画的行**，图标是自己画的：六枚都取自 `iconset` 里现成的
//     同族素材（RemixIcon，逐字照上游那六个 `RiXxxLine`）。
//
// 而系统菜单的**样式**跟当前的设计规范完全不搭（它是系统外观，不是我们的
// surface1 / radiusLg / shadow.Modal / accentSoft）——那一条从来就没被上面
// 那两条理由覆盖过，只是当时系统菜单省事。
//
// # 六项一项不少，顺序照 Biu
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
// # 「退出登录」多了一步确认
//
// 上游退登之前问一句（「确认退出登录？」，`user/index.tsx:146`），我们原来
// 点一下就退——退登是不可逆的那一下，而确认框是仓库现成的那一道
// （`confirmModal`，见 confirmmodal.go）。取消 / Esc / 点外面都算「取消」：
// 什么都不做。
//
// # 登录态的**唯一**来源是 app，不是这个包
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
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/motion"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

const (
	// avatarSize 是头像那一枚圆有多大。Biu 是 `size="sm"` 加 `mr-4`，具体值没
	// 露出来。
	//
	// ⚠️ **18 是照着「顶栏那一排的视觉重量」定的**（票 #241 / 报告 F-16）：
	// 左边那三枚线框图标（文件 / 迷你 / 动态）都是 16 宽的图形，墨迹
	// 137~141；**头像原来那一枚是 28 直径的一块实心圆盘（墨迹 608）**——是那
	// 三枚的 **4.3 倍**，用户第一眼落在它上面而不是任何一枚功能键上。
	// 18 是那一档：现在这一枚的墨迹是 152~160，和那三枚**同一档**
	// （1.08~1.14 倍，而那三枚彼此之间也有 1.03 倍的差别）。
	//
	// ⚠️ **改这个数要连着下面那一堆比例一起看**：人形剪影的每一块都是它的分数
	// 倍（`avatarPerson`）。⚠️ 这句话原来还接着「28 是顶栏这个高度里放得下、
	// 又不抢戏的一个数」——那是**在它比邻居重四倍的时候**说的，现在那一半
	// 理由已经作废：这一格要和左边那三枚**同一档**，不是比它们大一号。
	//
	// ⚠️ **左边原来有四枚**（多一枚「设置」）：票 #289 拿掉了顶栏那一枚——侧栏
	// 和头像下拉里都有同一个动作，三个入口会不一致。
	avatarSize = 18

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

	// avatarMenuLabel 是那一层浮层的无障碍名字，也是测试认得它的凭据。
	//
	// ⚠️ **它不能和上面任何一项的名字一样**：无头 Tester 的 `Find` 找的是
	// 「显示着这个字**或者**标着这个名字」的元素，两处叫同一个名字的话，
	// 按名字找会撞车（返回先建的那个）。
	avatarMenuLabel = "用户菜单"

	// avatarMenuW 是浮层多宽。
	//
	// ⚠️ **160 是照着上游那一行的形状算出来的**：上游是 `min-w-[140px]`
	//（`user/index.tsx:70` 的 `classNames.content`），而它那一行是
	// 「18 的图标 + 8 的缝 + 四个字（14 号 ≈ 56）+ 左右各 12」= 106；
	// 「问题反馈」那一行右边还要多摆一枚 18 的外链记号 + 8 的缝 = 132。
	// 140 装得下而一点余量都没有，所以这里给 160。
	avatarMenuW = 160

	// avatarMenuRowH 是每一项多高。
	//
	// 上游 HeroUI 的 DropdownItem 是 md 档（`h-10` = 40）；我们是 36
	//（18 的图标 + 上下各 9）。
	avatarMenuRowH = 36

	// avatarMenuIconSize 是菜单项左边那一枚图标多大。
	//
	// ⚠️ **18**，逐字照上游那六项的 `size={18}`（`user/index.tsx:96` 等六处）。
	avatarMenuIconSize = 18
)

// avatarMenuItem 是下拉里的一项。
//
// run 收到 Shell 和 Context：换路由的用前者（`navigate`），交给系统开地址的用
// 后者（`Context.OpenURL`）。**不许给 nil**——做不了的那几项也要有 run，只不过
// 它做的是「告诉用户为什么」，而不是一声不响。
type avatarMenuItem struct {
	// label 是那一项的名字（可见的字，也是无障碍名字）。
	label string
	// icon 是左边那一枚图标的名字（`icons.go` 里的常量）。
	icon string
	// end 是右边那一枚。上游只有「问题反馈」有：外链记号
	//（`RiExternalLinkLine`）。空串 = 右边没有。
	end string
	// danger 是「这一项是危险动作」——字走危险色那一档（上游的
	// `color="danger"` + `className="text-danger"`）。
	danger bool
	// run 是点了这一项要做的事。
	run func(s *Shell, c *ui.Context)
}

// avatarMenuItems 是头像下拉的那六项，逐条照 Biu 的
// `src/layout/navbar/user/index.tsx:88-158`。**顺序就是 Biu 的顺序。**
//
// Biu 那六条的 key 依次是 login / profile / settings / refresh / feedback /
// logout，对着一数就知道一条没漏、一条没编。
//
// ⚠️ **图标逐字照上游那六项的 `startContent`**：登录 `RiLoginCircleLine`、
// 个人资料 `RiProfileLine`、设置 `RiSettings3Line`、刷新数据 `RiRefreshLine`、
// 问题反馈 `RiFeedbackLine`（右边还有一枚 `RiExternalLinkLine`）、退出登录
// `RiLogoutCircleLine`。
var avatarMenuItems = []avatarMenuItem{
	{label: "登录", icon: iconLoginCircle, run: func(s *Shell, _ *ui.Context) { s.goLogin() }},
	{label: "个人资料", icon: iconProfile, run: func(s *Shell, _ *ui.Context) { s.goProfile() }},
	{label: "设置", icon: iconSettings, run: func(s *Shell, _ *ui.Context) { s.navigate(settingsPath) }},
	{label: "刷新数据", icon: iconRefresh, run: func(s *Shell, _ *ui.Context) {
		// 真的是重新拉一次侧栏那些收藏夹（`app/favorites_refresh.go`）。
		// 之前这一项只弹一句「还没有数据可刷新」，而接口是有的——用户改了收藏夹
		// 切回来看不到，说的却是「这里没有数据」。
		//
		// ⚠️ **这里只发起，不等**：网络那一趟在后台，而这一项的菜单**选完就关**，
		// 没有任何地方可以转圈。进行中状态是侧栏那一行「正在刷新收藏夹…」
		//（见 sidebar_favorites.go），跑完的那句话走 `reportFavoritesRefresh`。
		//
		// 已经有一趟在飞时应用层会挡下来，并把「正在刷新收藏夹…」说给用户听——
		// 连点两下只发一次请求，而第二下不至于毫无动静。
		s.app.RefreshFavorites()
	}},
	{label: "问题反馈", icon: iconFeedback, end: iconExternal, run: func(s *Shell, c *ui.Context) {
		// 走 `Shell.openFeedback`：它把地址交给系统（`Context.OpenURL`），
		// **而且记一笔**（`lastFeedbackURL`）——无头 Tester 里 `OpenURL` 走不了
		// 事件循环，于是「点得动」这件事除了「这一项在」之外一句都验不到。
		// 理由见 shell.go 的 `openFeedback`。
		s.openFeedback(c)
	}},
	{label: "退出登录", icon: iconLogoutCircle, danger: true, run: func(s *Shell, _ *ui.Context) {
		s.askLogout()
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
		s.addToast("还没登录，个人资料用不了", notice.Warning)
		return
	}
	s.navigate("/user/" + mid)
}

// askLogout 是「退出登录」那一项做的事：**先问一句**再退。
//
// ⚠️ **没登录的时候不問**：没登录就没有可退的，而问一句「确认退出登录？」
// 再答「确认」是一趟没有意义的往返——直接说一句（和原来一样）。
func (s *Shell) askLogout() {
	if !s.app.LoggedIn() {
		s.addToast("还没登录，没有可退的", notice.Warning)
		return
	}
	s.avatarLogoutAsk = true
}

// ─────────────────────────────────────────────────────────────────────────────
// 头像那一格的状态
// ─────────────────────────────────────────────────────────────────────────────

// avatarLook 是这一帧该画成什么样。
//
// ⚠️ **只有三档，而它们分的是「画什么」不是「为什么」**。为什么在 `note` 里。
// 拆成「未登录一种、载入中一种、没有头像一种、失效一种」四五个相位看起来更齐，
// 可它们在 18 宽的一格里**画出来是同一样东西**——那个人形。相位拆得再细，
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
	//（和 runID 那套一样，见 `internal/app/account.go` 的注释）。
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

// avatarSync 让那一格追上此刻的登录态。**每一帧都会被叫到**
// （`syncUnderlay` 里那一句，票 #161：满屏开着的时候顶栏不画，而这一格必须
// 自己追得上登录态），所以它必须满足两件事：
//
//  1. 没换过登录态时**一个请求都不发**——这里是整个应用重画最勤的一处。
//  2. 换了登录态时**至多发一个**，而且退登那一支**一个都不发**。
//
// 「至多一个」靠 `acted` + `mid`，「在途的那一趟作废」靠 `gen`。两条各自都不够：
// 只有 `acted` 的话，退登之后在途的那一趟会把上一个账号的头像写回来。
//
// ⚠️ **它原来在 `viewAvatar` 的第一句**：那是「画这一格」的动作，而它要做的
// 其实是「问一次登录态变了没有」——一件跟画不画无关的事（票 #161）。
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
		// **那张图**，图自己就是交代，一个字都不占那 18 见方。留着 note 的作用
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
// ⚠️ **它们不是装饰**：这一格只有 18 见方，装不下几句话，而「这一格是什么意思」
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

// viewAvatar 是头像 + 它下面那一层下拉。
//
// ⚠️ **命中区 32×32，里面那枚圆 20**（票 #241 / 报告 F-16）：顶栏那一排的图标
// 键命中区都是 32（`iconHitMd`），头像原来那一格只有 28 见方——**它缩小之后
// 命中区反而更小了**，于是把命中区留在 32、只把**画出来的那一枚圆**收到 20。
func (s *Shell) viewAvatar(c *ui.Context) {
	// Key 挂在外面这一格上：里面那一枚是**按钮**，而 Key 挂在「创建时处理过
	// 输入」的控件上会 panic（`ui/element.go:658`）。
	ui.Column(c).Key("avatar").Children(func() {
		hit := ui.ButtonBase(c).Size(iconHitMd, iconHitMd).Padding(space.None).
			Label(avatarLabel)
		// ⚠️ **指针移上去时那个圆头像放大 5%**：上游顶栏那一枚是
		// `<Avatar ... className="mr-4 cursor-pointer transition-transform
		// hover:scale-105" />`（`layout/navbar/user/index.tsx:178`）。放大的是
		// **整个圆**（上游那个类加在 Avatar 上），不是里面那张图。
		//
		// ⚠️ hover 要在 Children **之前**问（理由同 `widgets/list` 的 grid.go）。
		on := hit.Hovered()
		hit = motion.HoverZoom(hit, "avatar-zoom", on)
		// 🔴 **悬停时头像外面多一圈主色的环**（票 BMG-253）：放大 5% 是这一排里
		// **最弱**的反馈——一个 18 像素的圆涨到 19 像素，指针压上去几乎看不出
		// 区别，而这一格是顶栏里唯一的「个人入口」。
		hit = avatarHoverRing(c, hit, on, float32(avatarSize))
		hit.Children(func() {
			// 那一枚圆：**裁成圆的**（里面的图要按圆裁），外面留一圈 1 像素的描边。
			face := ui.Box(c).Size(avatarSize, avatarSize).
				Radius(avatarSize/2).Clip().Border(1, c.Theme().Border)
			face.Children(func() { s.viewAvatarFace(c) })
		})
		// 点它开 / 关那一层浮层。
		//
		// ⚠️ **按下这一枚会把键盘焦点给它**（框架的规矩：指针按下的那条链上
		// 第一个可聚焦的元素拿到焦点，见 `mygo/ui/input.go` 的 setFocus）——
		// 而键盘那一半正是靠这个焦点（见 `viewAvatarMenuKeys`）。
		if hit.Clicked() {
			s.avatarMenuOpen = !s.avatarMenuOpen
			// ⚠️ **打开的时候从「什么也没选中」开始**：上游 HeroUI 打开下拉时
			// 不高亮任何一项。而「一打开就亮着第一项」的话，回车直接进扫码
			// 登录页——那一下用户根本不是想登录。
			s.avatarMenuIndex = -1
			// ⚠️ **指针停在第几项也要清掉**：留着上一次那一份的话，浮层刚
			// 打开时指针明明停在一项上，却因为「和上一帧一样」而不亮。
			s.avatarMenuHover = -1
		}
		// ⚠️ **键盘那一半排在浮层前面**：方向键改的是「当前是第几项」，而浮层
		// 里那几行是照着那个下标画亮底的——键盘在前、浮层在后的话，按下 ↓ 的
		// 那一帧亮底就跟着走了（反过来则要等到下一帧，而下一帧不一定有）。
		s.viewAvatarMenuKeys(c, hit)
		s.viewAvatarMenu(c, hit)
		s.viewAvatarLogoutConfirm(c)
	})
}

// avatarHoverRing 是头像那一枚圆外面那圈**悬停时的主色环**（票 BMG-253）。
//
// # 为什么是画在图**上面**的一圈，而不是改外面那一圈描边
//
// 头像那一格本来有一圈 1 像素的描边（`t.Border`，见 `viewAvatar`），而**图是
// 18×18、正好盖满那一格**——描边被图压在底下，改它的颜色在**有图的时候**一个
// 像素都看不见（而那正是常态：登录之后一直有图）。
//
// # 为什么不用「铺一层淡底」
//
// 那一格的命中区是 32×32，而图只有 18：铺出来的淡底要么是一块 32 的**方块**
// （方的，和圆头像不搭），要么是一圈 7 像素宽的晕（太宽，抢戏——票 #241 刚把
// 头像从 28 收到 18 就是为了让它和旁边那三枚同一档）。
//
// 一圈 2 像素的主色环贴着头像的外沿，是「这一格是活的」最省事也最清楚的一档。
//
// ⚠️ **深浅两套都取 `ink.AccentOn`**：亮绿压白底只有 1.9:1，直接用等于看不见
// （浅色下那一档是压深到 ≥4.5:1 的，深色下就是主色本身）。
//
// side 是**那一枚圆的多大**（顶栏那个是 `avatarSize`，收藏夹头图区那个是 24）。
func avatarHoverRing(c *ui.Context, e ui.Element, on bool, side float32) ui.Element {
	if !on {
		return e
	}
	ring := ink.AccentOn(c.Theme())
	const width = float32(2)
	// 环的**内沿**正好贴着头像那一枚圆的外沿：描边画在那个圆的**外边** width
	// 那一圈上。
	d := side + 2*width
	return e.DrawOver(func(p *ui.Painter, r ui.Rect) {
		p.Stroke(ui.Rect{
			X: r.X + (r.W-d)/2,
			Y: r.Y + (r.H-d)/2,
			W: d,
			H: d,
		}, ring, d/2, width)
	})
}

// viewAvatarMenu 是头像点开之后的那一层浮层。
//
// ⚠️ **它走 `ui.PopoverBase`**（而不是自己拼一个浮层）：框架那一份自带
// 「点外面关 / 按 Esc 关」，而自己拼的话得自己实现那两件事——漏了就是面板
// 关不掉（点外面也没反应）。理由同 `sidebar_favorites.go` 的
// `viewFavoritesPanel`。
func (s *Shell) viewAvatarMenu(c *ui.Context, anchor ui.Element) {
	if !s.avatarMenuOpen {
		return
	}
	t := c.Theme()
	ui.PopoverBase(c, anchor, &s.avatarMenuOpen, func(panel ui.Element) {
		// ⚠️ **锚在头像的右下角**：头像在顶栏最右端，面板要是左对齐就有
		// 一半挂在窗口外面（上游 HeroUI 那个下拉是靠 shift 摆回来的，我们
		// 直接锚在右端）。
		//
		// ⚠️ **面板和锚点之间留一道缝**（上游那个浮层也留 4，见框架的
		// `stylePanel`）：贴着头像的下拉看着像头像长出了一块。
		popupPanelCard(s, t, panel.
			AttachTo(anchor, ui.AnchorBottomEnd, ui.AnchorTopEnd).
			Margin(t.Space(1), space.None, space.None, space.None).
			Width(avatarMenuW).
			Padding(space.Px4).
			// 面板自己有个名字：读屏软件念得出「用户菜单」。
			Label(avatarMenuLabel)).
			Children(func() {
				s.viewAvatarMenuItems(c)
			})
	})
}

// avatarMenuMark 是菜单里那一项的「图标 + 名字」两个元素。
//
// ⚠️ **留着它们是为了在「当前是第几项」定下来之后再铺底和字色**：悬停是在建
// 那一行的时候才发现的，而底色和字色都要按「当前是第几项」算——先铺后问的
// 话，鼠标移过去的那一帧会有两项都亮着，而危险色的字压在淡底上会掉到 4.5:1
// 以下。
type avatarMenuMark struct {
	icon ui.Element
	text ui.Element
}

// viewAvatarMenuItems 是浮层里那六项。
//
// ⚠️ **每一项都是一整行按钮**（不是一枚图标键）：上游 HeroUI 的 DropdownItem
// 是「图标 + 名字」的一行，`startContent` 在左、`endContent` 在右。
//
// # 🔴 指针和键盘**共用一个下标**，但**指针不动的时候键盘说了算**
//
// 两条各自都会改「当前是第几项」：指针停在某一项上是悬停，方向键是键盘。
// 每一帧都让悬停改一遍下标的话，**键盘刚走的那一格会被指针改回去**——
// 「键盘能上下走」于是只在指针不在菜单项上的时候才成立（而那恰恰是键盘用户
// 和鼠标混着来时最常遇到的两种情形）。
//
// 所以悬停**只在指针移到另一项上时**才改下标（`avatarMenuHover` 记着上一帧
// 指针停在哪一项）。这正是框架自己那个下拉的做法（`mygo/ui/base.go` 的
// `selectParts.Item`：`item.Hovered() && p != *s.pointer`——指针不动，方向键
// 说了算）。
func (s *Shell) viewAvatarMenuItems(c *ui.Context) {
	t := c.Theme()
	panelBG := s.cardBackground(t)
	rows := make([]ui.Element, 0, len(avatarMenuItems))
	marks := make([]avatarMenuMark, 0, len(avatarMenuItems))
	hovered := -1
	for i, item := range avatarMenuItems {
		row := ui.ButtonBase(c).Key("avatar-menu:"+item.label).
			FillWidth().Height(avatarMenuRowH).
			Padding(space.None, space.Px12).Gap(space.Px8).
			Justify(ui.Start).Radius(t.Radius)
		if row.Hovered() {
			hovered = i
		}
		var ic, tx ui.Element
		row.Children(func() {
			ic = ui.Icon(c, icon(item.icon)).FontSize(avatarMenuIconSize)
			tx = ui.Text(c, item.label).FontSize(scale.Font(c, scale.Size14)).Grow(1)
			if item.end != "" {
				ui.Icon(c, icon(item.end)).FontSize(avatarMenuIconSize).TextColor(t.TextMuted)
			}
		})
		if row.Clicked() {
			s.runAvatarMenuItem(c, item)
		}
		rows = append(rows, row)
		marks = append(marks, avatarMenuMark{icon: ic, text: tx})
	}
	// ⚠️ **指针移到新的一项上时，当前项跟着它走；指针不动的时候，键盘说了算**。
	if hovered >= 0 && hovered != s.avatarMenuHover {
		s.avatarMenuIndex = hovered
	}
	s.avatarMenuHover = hovered

	// 当前那一项：底是强调色的淡底（规范 §2 的 `accentSoft`），而**字要按
	// 这层底现算**（仓库的规矩：「底换了，字按这层底现算」，见 `flatbutton.go`
	// 的 `flatCurrentText`）。
	//
	// ⚠️ **底和字都在这一轮建完之后才定**：悬停是在建这一行的时候才发现的，
	// 而「当前是第几项」要到那一刻才知道——边建边问边铺的话，鼠标移过去的
	// 那一帧会有两项都亮着（上一项还带着上一帧的亮底）。
	for i := range rows {
		current := i == s.avatarMenuIndex
		bg := panelBG
		if current {
			bg = accentSoft(t)
			rows[i] = rows[i].Background(bg)
		}
		col := avatarMenuTextColor(t, avatarMenuItems[i], bg, current)
		marks[i].icon = marks[i].icon.TextColor(col)
		marks[i].text = marks[i].text.TextColor(col)
	}
}

// avatarMenuTextColor 是那一项的字色。
//
// bg 是这一行**实际的底色**，current 是「它是不是当前那一项」。
//
// ⚠️ **危险色当字色要压深那一档**（`dangerText`）：主题给的 `t.Danger` 在浅色
// 下只有 4.14:1，达不到正文的 4.5:1（DESIGN.md §四 那张表）。上游那一项是
// `text-danger`，而我们这一档是仓库自己的规矩（票 #241 / 报告 F-22）。
//
// ⚠️ **当前那一项的危险字要按那层底现算**（仓库的规矩：「底换了，字按这层底
// 现算」，见 `flatbutton.go` 的 `flatCurrentText`）：同一个危险字压在**面板底**
// 上是 4.97~5.75:1，而压在当前项那层 `accentSoft` 淡底上只有 **4.34~4.45:1**
// ——差一点点到不了 4.5。而那一档正是「键盘走到退出登录」或者「鼠标悬停它」
// 的那一瞬。
func avatarMenuTextColor(t *ui.Theme, item avatarMenuItem, bg ui.Color, current bool) ui.Color {
	if !item.danger {
		return t.Text
	}
	if current {
		return ink.Ensure(t.Danger, bg, 4.5)
	}
	return dangerText(t)
}

// viewAvatarMenuKeys 是那一层浮层的**键盘那一半**：上下走、回车选中。
//
// ⚠️ **这四条注册在头像那一枚按钮上，不注册在浮层上**——
//
// 浮层能收键的方式是 `OverlayShortcut`，而它只在「焦点那一圈没人要这个键」的
// 时候才拿得到键。焦点在头像那一枚按钮上，而框架对**有焦点的按钮**是先把
// 回车自己吃掉的（`mygo/ui/input.go` 的 handleKey：回车/空格 → 那枚按钮
// clicks++），于是那一下回车会变成「再点一下头像」——菜单直接关掉，而选中项
// 什么也没发生。
//
// 注册在按钮上就不一样了：`Shortcut` 的登记会让框架知道「这个键有人要」
// （`claimedBy`），按钮那一下 clicks++ 就不做了，键于是按快捷键走回来。
//
// ⚠️ **菜单关着的时候这四条一条都不许注册**：注册了的话按钮上的回车就被
// 「有人要」了，于是**回车打不开这一层浮层**（那正是它唯一的键盘入口）。
func (s *Shell) viewAvatarMenuKeys(c *ui.Context, anchor ui.Element) {
	if !s.avatarMenuOpen {
		return
	}
	// ⚠️ **四条都要每帧登记**：键是按**上一帧**的登记路由的（`rt.regs`），
	// 这一帧只登记「这一帧按下的那一条」的话，下一次按另一个键就路由不到
	// 这里了。
	down := anchor.Shortcut(0, ui.KeyDown)
	up := anchor.Shortcut(0, ui.KeyUp)
	enter := anchor.Shortcut(0, ui.KeyEnter)
	space := anchor.Shortcut(0, ui.KeySpace)
	switch {
	case down:
		// ⚠️ **-1（什么也没选中）往下走一步就是第一项**。
		s.avatarMenuIndex = min(s.avatarMenuIndex+1, len(avatarMenuItems)-1)
	case up:
		// ⚠️ **往上不越过第一项**（菜单的老规矩：不在顶上绕回去）。
		s.avatarMenuIndex = max(s.avatarMenuIndex-1, 0)
	case enter, space:
		// ⚠️ **没有当前项的时候回车什么都不做**：浮层刚打开时是什么也没选中
		// （上游 HeroUI 也是这样），而回车的作用是「做当前那一项」——
		// 没有当前项就没有可做的。
		if s.avatarMenuIndex >= 0 && s.avatarMenuIndex < len(avatarMenuItems) {
			s.runAvatarMenuItem(c, avatarMenuItems[s.avatarMenuIndex])
		}
	}
}

// runAvatarMenuItem 跑下拉里的那一项：先收浮层，再做那件事。
//
// ⚠️ **先收浮层**：那一层盖在顶栏上，而好几项做完之后会弹一句提示
// （「还没登录，个人资料用不了」），浮层还开着的话提示压在浮层底下。
func (s *Shell) runAvatarMenuItem(c *ui.Context, item avatarMenuItem) {
	s.avatarMenuOpen = false
	// ⚠️ **下标回到「什么也没选中」**：浮层下一次打开时是从零开始的
	// （见 `viewAvatar` 里那一句），而这一项刚被按过——留着它的下标的话，
	// 下一次打开时那一项会**亮着**，而用户已经不在那一项上了。
	s.avatarMenuIndex = -1
	item.run(s, c)
}

// viewAvatarLogoutConfirm 画「退出登录」那道确认框。
//
// ⚠️ **用的是仓库现成的那一道**（`confirmModal`，走 `dialogPanel`）：底色
// surface1、圆角 radiusLg、影 shadow.Modal，两颗按钮的档位都是仓库自己的
// 契约。取消 / Esc / 点外面都算「取消」——什么都不做。
func (s *Shell) viewAvatarLogoutConfirm(c *ui.Context) {
	if !s.avatarLogoutAsk {
		return
	}
	switch confirmModal(c, &s.avatarLogoutAsk, confirmSpec{
		// ⚠️ **danger（红）**：上游那一步是 `type: "danger"`
		//（`user/index.tsx:149`）。
		tone:    toneDanger,
		title:   "确认退出登录？",
		confirm: "退出登录",
		cancel:  "取消",
	}) {
	case confirmAccepted:
		s.avatarLogoutAsk = false
		// 走 login.go 那一支：它退完会把界面收回未登录的样子，退不干净
		// （钥匙串锁了）也会说出来。
		s.logoutAndStartOver()
	case confirmCancelled:
		// 取消 / Esc / 点外面：**什么都不做**。
		s.avatarLogoutAsk = false
	}
}

// viewAvatarFace 是头像那一格里的图。
//
// 它只管**一张图怎么样了**（好了 / 在路上 / 下不下来）；「有没有地址」「登录态
// 怎么了」在 `fetchAvatarFace` 里。两边合起来四种情形，而且**四种互不合并**：
// 「未登录」和「已登录」必须是两回事（issue #95 的第三条验收），而「图没下来」
// 和「压根没有地址」也不是一回事——前者网络好了会自己好，后者永远好不了。
//
// ⚠️ 这里读的是 `imgcache.Cache` 而不是直接调 `imgcache.View`：那个 View 是给
// **封面**那一档尺寸定的（占位和失败态都是没有字的一格，见 imgcache/view.go 的
// drawCover）。头像只有 18 宽，而且它要说的是「未登录 / 图没下来 / 在路上」
// 三件不同的事，有自己的一套画法（见下面 drawAvatar）。
func (s *Shell) viewAvatarFace(c *ui.Context) {
	side := float32(avatarSize)
	look, face, note := s.avatar.snapshot()

	// 没有地址：这一格归状态那一层说，缓存压根不用问（它对空地址不表态，
	// 而 `Get("")` 会拿空串当一个键去查——那是个永远下不下来的地址）。
	if face == "" {
		s.drawAvatar(c, look, note)
		return
	}

	// 失败和「还没下好」要分得开：混成一个色，用户只会以为这个头像本来就是空的，
	// 等网络好了也不会再看它一眼（imgcache 那边也是这么分的，见 drawCover）。
	switch st := s.images.Get(face); {
	case st.Ready():
		ui.Image(c, st.Bitmap).Fit(ui.Cover).Size(side, side)
	case st.Err != nil:
		// ⚠️ **这一档不能摆成空白**——18 见方的一格空白在顶栏上是一个看不出是
		// 什么的洞，而用户真正缺的不是那张图，是「为什么没有」。所以染成告警色
		// 并且把那句话挂上去。
		s.drawAvatar(c, avatarTrouble, avatarNoteNoPicture)
	default:
		// 有地址、在路上：这一帧画的是人形，而说的是「加载中」——和未登录
		// 画出来一样、说的话不一样（理由见 avatarLook 那一段）。
		s.drawAvatar(c, avatarSilhouette, avatarNoteWaiting)
	}
}

// drawAvatar 按那一档的样子画。**三档都在这里定死**，view 不再插嘴。
//
// ⚠️ **那一格不铺底色**（票 #241 / 报告 F-16）：原来占位那一档在圆里铺的是
// `t.Surface`——在深色下那是 #27272a，而顶栏的底是 #18181b，**整块圆盘都是
// 「墨迹」**：28 直径的一块实心圆 = 608 像素，是左边那三枚线框图标（137~141）
// 的 **4.3 倍**。现在只画那个人形（外加外面那一圈 1 像素的描边），墨迹是
// 152~160，和邻居同一档。
//
// ⚠️ **「出问题」那一档仍然铺底色**（危险色 18%）：那一档**要**是显眼的——
// 它说的是「这儿出事了」，而那一档平时不出现。
func (s *Shell) drawAvatar(c *ui.Context, look avatarLook, note string) {
	t := c.Theme()
	side := float32(avatarSize)

	ink := t.TextMuted
	col := ui.Column(c).Size(side, side).Center().Label(note)
	if look == avatarTrouble {
		ink = t.Danger
		col = col.Background(t.Danger.Alpha(0.18))
	}

	// ⚠️ `Label` 和悬停提示用的是**同一句话**：这一格只有 18 见方，写不下
	// 「这个账号没有头像」那几句；而这三样（无障碍名、悬停说明、测试点得到的
	// 凭据）读的是同一件事，给它们三份不同的措辞只会让其中一份开始说谎。
	col.Children(func() { avatarPerson(c, side, ink) })
	// ⚠️ **quiet**：锚点是外面那个头像菜单按钮里的子元素。普通的提示会问
	// `Pressed()` / `Clicked()`（那会给元素打上可点标志），命中目标就从菜单
	// 按钮变成它——头像下拉根本不开（见 withQuietTip）。
	s.withQuietTip(c, col, note)
}

// avatarPerson 画那个人形：一个圆脑袋 + 一块肩膀。
//
// 为什么画人不画字：这一格没登录时**没有一个字是诚实的**。原来的「B」是应用
// 自己的名字（见老注释），用户看到的是一个占位符坏了；换成昵称头一个字的话，
// 那一格在没登录时显示的是**别人的名字的第一个字**——那更糟。用户资料接口
// 明明能给出真实头像，于是没登录就该说「这儿没有人」，而人这个形状在 18 见方
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
		Margin(side*gap, space.None, space.None, space.None).Background(ink)
}
