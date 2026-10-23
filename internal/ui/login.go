// 扫码登录那一页。
//
// # 三种状态在屏幕上必须长得不一样
//
// 这是 Biu 上游最刺眼的一处行为差距：`qrcode-login.tsx` 把「已扫待确认」
// 和「扫码成功」归成同一个 `isPendingConfirm`，于是成功之后
// 「二维码已扫码未确认」还会再闪一帧。
//
// 这里把四种状态分开画，各自带各自的话：
//
//	等扫码        「请用 bilibili 手机客户端扫码」
//	已扫待确认    「已扫码，请在手机上确认」  ← 少了这一句，用户以为没扫成功，
//	                                          退出重进，然后卡死在这里
//	二维码失效    「二维码已失效，请刷新」 + 一个显眼的刷新按钮
//	登录成功      「登录成功」+ 去首页
//
// # 二维码在本地画，不联网取图
//
// 见 `bili/qrcode.go` 的说明：二维码的内容就是那条登录地址，
// 在有效期内它是一次性的授权凭据，送出去等于把「用户扫的是哪个账号」
// 交给第三方决定。
package ui

import (
	"image"
	"image/color"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/egoist/mygo/ui"
)

// qrSide 是二维码在屏幕上大约多大（点）。
//
// 真正的整数是 `qrDisplaySize` 算出来的那个——**必须是整模块**，
// 原因见那个函数的注释。
const qrSide = 200

// loginPath 是登录页那条路由，和 routes 表里登记的是同一个字符串。
const loginPath = "/login"

// goLogin 切到登录页。
//
// 不用 `navigate`：那三句是**原样抄**的（清 Toast / 清 crash / Push），
// `navigate` 自己**一条 Toast 都不弹**了（见 `router.go` 的 `navigate`）。
//
// ⚠️ 这一段注释曾经写着「`navigate` 是给『还没做的页面』用的，每切一次都会弹
// 『这一页还没做』」——`navigate` 早就不弹那句了（原型那版才弹）。真照着那句去读
// 代码的人会以为换掉 `navigate` 会少一个提示，而实际上少不掉任何东西。
func (s *Shell) goLogin() {
	s.clearToasts()
	s.crash = nil
	s.router.Push("/login")
}

// viewLogin 画扫码登录那一页。
//
// 整个函数**只读状态、只发命令**：不碰网络、不读文件系统。
// 所有异步的事都在 `app.Account` 里（见 account.go）。
//
// 外面套一层 Scroll：窗口矮的时候（尤其是缩到最小高度）内容会比这一页高。
// 不套的话最后那排按钮会掉到播放条底下去——界面上还看得见，但**点不到**。
func (s *Shell) viewLogin(c *ui.Context, _ *ui.Route) {
	t := c.Theme()
	st := s.app.LoginState()

	loginScroll := ui.Scroll(c).Key("login-scroll").Grow(1)
	loginScroll.Children(func() {
		ui.Column(c).Key("page:"+loginPath).Grow(1).Padding(0, 20).Gap(12).
			AlignItems(ui.Center).Children(func() {
			ui.Text(c, "扫码登录").FontSize(scale.Font(c, pageTitleSize))

			// **登录能换来什么**（票 #242，UX 报告 §3-A 的 A3）。
			//
			// 这一句**每一态都在**（不只是「还没点开始」那一态）：用户点进
			// 登录页的时候，脑子里的问题是「登录了能干什么」——不说的话，登录
			// 像一道没有说明书的考题，用户可能登录了才发现里面是空的（其实有，
			// 只是他不知道）。
			//
			// 说的那几样都是**真的**：关注列表、收藏夹、历史记录、稍后再看这
			// 四处的接口全都要 Cookie，没登录时一律回 -101（见 loginprompt.go
			// 的文件头）。
			ui.Text(c, "登录之后可以看你的关注、收藏、历史记录和稍后再看").
				FontSize(scale.Font(c, 14)).TextColor(t.TextMuted)

			switch {
			case !st.Busy() && st.QR == nil:
				// 一开始（还没点）或已经取消：给一个开始的地方。
				// 不放二维码是因为还没有——凭空画一张占位图会让人以为
				// 「点了没反应」。
				ui.Text(c, "用 bilibili 手机客户端扫一扫就能登进来").
					FontSize(scale.Font(c, 14)).TextColor(t.Text)

			case st.Phase == app.LoginExpired:
				s.qrImage(c, st.QR, true)
				ui.Text(c, "这张二维码已经过期了，扫不了了").FontSize(scale.Font(c, 14)).TextColor(t.Text)

			default:
				// 这一支（等扫码 / 已扫待确认）**永远不盖灰**。
				//
				// 写成 `st.Phase != LoginScanned` 的话，等扫码这一态也被盖上了——
				// 盖完的码手机扫不出来（实测 45% 压暗之后相当一部分 App 直接放弃），
				// 用户看到的却是一张「正常」的二维码。
				s.qrImage(c, st.QR, false)
				ui.Text(c, st.Hint).FontSize(scale.Font(c, 14)).TextColor(qrHintColor(t, st.Phase))
			}

			s.qrActions(c, st)
			s.viewAccountFooter(c, t, st)
		})
	})
	s.trackScrollbar(loginScroll)
}

// syncLogin 是登录页每帧的副作用。**这一页没有**。
//
// 理由：这一页只读状态、只发命令（见文件头那段话），所有异步的事都在
// `app.Account` 里，而那一边是**自己叫醒界面**的——二维码回来了界面自己会
// 醒，不需要这一页每帧去收什么。
//
// ⚠️ **它在 `pageSyncs` 表里是个空函数**，不是漏了：那张表和 `builtPage` 是
// 同一件事的两面（见 underlay.go 的注释），加新页的人照着表就知道该往哪儿加。
func (s *Shell) syncLogin(c *ui.Context) {}

// qrImage 把二维码画出来。
//
// # Bitmap 只在换二维码时重建
//
// 它是 GPU 纹理，每帧新建一个的话显存会一直涨。
// 缓存在 Shell 上，键是那张二维码本身——换了码自然重建，没换就复用。
//
// 为什么不用 `ui.Local`（挂在元素上的那份状态）：`Local` 要先有一个元素
// 才能问它的状态，于是「问 Local」和「建那个元素」只能交替着来。
// 不值得为了省两个字段去碰这种来路不明的坑。
//
// # 「过期」那层灰是**烘进位图**的，不是叠一个控件
//
// 早一版是画完图再叠一个同样大小的半透明方块。看起来没差别，后果有两个：
//
//  1. **它把下面 180 像素顶掉了**——过期时「刷新二维码」按钮落到播放条
//     底下去了：看得见，点不到。实测按钮的 y=544，而播放条从 ~490 开始。
//  2. 顺带把「等扫码」那一态也盖上了（当时的条件写反了），
//     结果用户看到的二维码永远是 60% 黑底的——**根本扫不出来**。
//
// 烘进位图就没有这些问题：元素数量不变，宽高不变，画出来就是那样。
func (s *Shell) qrImage(c *ui.Context, code *bili.Code, dimmed bool) {
	if code == nil {
		return
	}
	if s.qrFor != code || s.qrBitmap == nil {
		side := qrDisplaySize(code)
		pix := code.Image(side / (code.Size() + 2*4))
		s.qrBitmap = ui.NewBitmap(pix).Pixelated()
		s.qrDimBitmap = ui.NewBitmap(dimImage(pix, 0.45)).Pixelated()
		s.qrFor = code
	}
	bm := s.qrBitmap
	if dimmed {
		bm = s.qrDimBitmap
	}
	// 位图是多少点，元素就是多少点，所以「一个模块占几个像素」是整数。
	// 见 qrDisplaySize 的注释。
	bw, _ := bm.Size()
	ui.Image(c, bm).Fit(ui.Contain).Size(float32(bw), float32(bw))
}

// dimImage 把图压暗，返回一份新的。
//
// 只在换二维码时调一次（qrImage 里有缓存），逐像素那一遍的成本可以忽略。
func dimImage(src *image.RGBA, amount float32) *image.RGBA {
	out := image.NewRGBA(src.Bounds())
	for y := range src.Bounds().Dy() {
		for x := range src.Bounds().Dx() {
			p := src.RGBAAt(x, y)
			out.SetRGBA(x, y, color.RGBA{
				R: scaleChannel(p.R, amount),
				G: scaleChannel(p.G, amount),
				B: scaleChannel(p.B, amount),
				A: p.A,
			})
		}
	}
	return out
}

// scaleChannel 把一个颜色通道压暗 amount。
//
// ⚠️ 名字不叫 `scale`：`internal/ui` 这一包现在 import 了 `widgets/scale`
// （图标 / 字号的缩放），包级函数和包名撞上，编译期就过不去。
func scaleChannel(v uint8, amount float32) uint8 {
	return uint8(float32(v) * (1 - amount))
}

// qrDisplaySize 是二维码在屏幕上画多大（点）。
//
// ⚠️ **结果必须是「模块数 × 整数」的倍数**，这是这条函数存在的全部理由。
//
// 让框固定 200 点、位图按 256 像素画、再交给 `Fit(Contain)` 缩放，是
// 很自然的写法，出来的图**看上去也没问题**——但屏幕上一个模块占
// 200/45 = 4.44 个点。后果是：
//
//   - 相邻模块之间会被插值出灰边，深色模块被削掉一截；
//   - 扫码 App 是在手机上拍这个屏幕，低对比度直接拍不出来；
//   - 而且**没法验证**：测试没法把屏幕像素对回模块，只能说「有黑白像素」。
//
// 所以这里反着算：先定每模块几个整数像素，再定整张多大。
// 200 点上下、每模块 ≥ 2 像素，是手机扫码能接受的组合。
func qrDisplaySize(code *bili.Code) int {
	total := code.Size() + 2*4 // 含四周留白
	module := max(qrSide/total, 2)
	return total * module
}

// qrActions 是那一排按钮。
//
// 按钮**跟着状态换**，而不是把所有按钮都摆出来再灰掉几个：
// 「刷新」在二维码还有效时点是没有意义的（换了也白换），
// 摆在那里只会让用户以为扫码失败了。
func (s *Shell) qrActions(c *ui.Context, st app.LoginState) {
	t := c.Theme()
	ui.Row(c).Key("qr-actions").Gap(10).Children(func() {
		switch st.Phase {
		case app.LoginIdle:
			if ui.PrimaryButton(c, "开始扫码登录").Clicked() {
				s.clearToasts()
				s.app.BeginLogin()
			}
		case app.LoginWaiting:
			if ui.Button(c, "取消").Clicked() {
				s.app.CancelLogin()
			}
		case app.LoginScanned:
			ui.Text(c, "别关掉这一页").FontSize(scale.Font(c, 12)).TextColor(t.TextMuted)
			if ui.Button(c, "取消").Clicked() {
				s.app.CancelLogin()
			}
		case app.LoginExpired:
			if ui.PrimaryButton(c, "刷新二维码").Clicked() {
				s.clearToasts()
				s.app.RefreshLoginCode()
			}
		case app.LoginDone:
			if ui.PrimaryButton(c, "去首页").Clicked() {
				s.navigate("/")
			}
			if ui.Button(c, "换一个账号").Clicked() {
				s.logoutAndStartOver()
			}
		case app.LoginFailed:
			if ui.PrimaryButton(c, "重试").Clicked() {
				s.clearToasts()
				s.app.BeginLogin()
			}
		}
	})
}

// viewAccountFooter 画底下那一行「现在的状态」。
//
// 已登录时给一个退登按钮——退登是验收项（「主动退出登录能清干净凭据」），
// 藏进设置页等于没有。
func (s *Shell) viewAccountFooter(c *ui.Context, t *ui.Theme, st app.LoginState) {
	if !st.LoggedIn {
		return
	}
	sess := s.app.Session()
	ui.Row(c).Key("account-footer").Gap(10).AlignItems(ui.Center).Children(func() {
		if uid := sess.DedeUserID; uid != "" {
			ui.Text(c, "已登录（UID "+uid+"）").FontSize(scale.Font(c, 12)).TextColor(t.TextMuted)
		} else {
			ui.Text(c, "已登录").FontSize(scale.Font(c, 12)).TextColor(t.TextMuted)
		}
		// 登录态存不存得住，一眼就该看见：后端不可用时用户必须在这里
		// 知道「关掉应用就白登了」，而不是重启之后才发现。
		if backend := s.app.CredentialBackendName(); backend == "none" {
			ui.Text(c, "凭据后端不可用，登录态关掉应用就没了").
				FontSize(scale.Font(c, 12)).TextColor(t.TextMuted)
		}
		if ui.Button(c, "退出登录").Clicked() {
			s.logoutAndStartOver()
		}
	})
}

// logoutAndStartOver 退登，并把界面收回「未登录」的样子。
func (s *Shell) logoutAndStartOver() {
	if err := s.app.Logout(); err != nil {
		// 退不干净要**说出来**：内存和 cookie 已经清了（应用层保证的），
		// 只有系统存储那边可能还留着。悄悄吞掉的话，用户会以为退登
		// 成功了，下次打开发现还在。
		s.addToast("退出登录："+err.Error(), notice.Danger)
	}
	s.clearToasts()
}

// qrHintColor 给「已扫待确认」换一个更醒目的颜色。
//
// 这一条不是审美选择：那是**唯一需要用户去手机上操作**的状态，
// 混在「请扫码」和「已失效」之间，用户最容易忽略的就是它。
func qrHintColor(t *ui.Theme, phase app.LoginPhase) ui.Color {
	switch phase {
	case app.LoginScanned:
		// 主色当**字**：浅色下换成与底色 ≥4.5:1 的变体（理由见
		// `widgets/ink`）。这句话是唯一要用户去手机上操作的那一句，读不出来
		// 等于没说。
		return ink.AccentOn(t)
	case app.LoginExpired, app.LoginFailed:
		return t.Text
	}
	return t.TextMuted
}

// viewLoginEntry 是顶栏右侧那个登录/退出按钮。
//
// 位置跟 Biu 对齐：Biu 把登录挂在导航栏右边的头像下拉里（`navbar/login`），
// 而侧栏是放菜单的地方——把「登录」放进侧栏会让用户为了登录先翻一遍菜单。
//
// 这一段放在 `viewNav` 那一行的末尾，不动 `viewMain` / `viewSide`。
//
// ⚠️ **未登录那一枚也是扁平的**（票 #241 / 报告 F-05 + F-16）：它原来是一块
// **实心主色的方块**（32×24、墨迹 752），是紧挨着它左边那四枚线框图标
// （140~152）的 **5.4 倍**——整条顶栏最响的元素，而它挂的是「登录」这么一
// 个低频次动作。**上游的顶栏右侧压根没有这么一枚按钮**（那边登录在头像下拉
// 里），所以「实心主色」是我们自己加的强调，而它强调错了地方。
//
// 现在两态同形（`iconButton`，无底色，悬停时才变主色），**未登录 / 已登录
// 只有图标不同**。
//
// ⚠️ **别把它改回实心主色**：主色留给「正在播放」那种方向标（DESIGN.md §四），
// 而这一排的视觉重量由**图标**说话——这一枚曾经是那一排里最响的一个。
func (s *Shell) viewLoginEntry(c *ui.Context) {
	if s.app.LoggedIn() {
		if s.withTip(c, iconButton(c, iconLogout, "退出登录", iconMd).
			Size(iconHitMd, iconHitMd).Padding(0, 7), "退出登录").Clicked() {
			s.logoutAndStartOver()
		}
		return
	}
	if s.withTip(c, iconButton(c, iconLogin, "登录", iconMd).
		Size(iconHitMd, iconHitMd).Padding(0, 7), "登录 B 站账号").Clicked() {
		s.goLogin()
	}
}
