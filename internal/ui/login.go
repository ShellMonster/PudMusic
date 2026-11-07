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
//	登录成功      一句「登录成功」的提示 + 回该去的页面（默认首页）
//
// ⚠️ **最后那一支不在这一页上画**：登录成功是**异步的状态变化**（扫码 →
// B 站回调 → 凭据落盘 → 状态变成 `LoginDone`），而这一页是**每帧重画**的——
// 写在 view 里的话，那一下会**每帧做一次**（每帧都看得见「现在是已登录」）。
//
// 所以它在 `syncLoginSuccess` 那一层判**边沿**（「上一帧不是成功、这一帧
// 是」），说完那句话、回完该去的页，就不在这一页上了（见那个函数的注释）。
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
	"strconv"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/leading"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

// qrSide 是二维码在屏幕上大约多大（点）。
//
// 真正的整数是 `qrDisplaySize` 算出来的那个——**必须是整模块**，
// 原因见那个函数的注释。
const qrSide = loginQRSide - 2*loginQRPad

// loginPath 是登录页那条路由，和 routes 表里登记的是同一个字符串。
const loginPath = "/login"

// goLogin 切到登录页，并记下「登录成功了该去哪」。
//
// after 是登录成功之后落到的路由。**空串就是首页**（`homePath`）——用户
// 2026-10-25 原话：「登录成功后应该提示并且回到首页」。
//
// ⚠️ **每个入口自带一个去处**：上游 Biu 那边登录是一个**浮层**，关掉它就回到
// 你原来那一页；我们这边登录是**一页路由**，那个「回去」得由**入口**自己
// 说清楚。现在只有一处不是首页：从「用户动态」那一路进去的，登录完回动态页
// （`dynamicfeed.go` 那一处「去登录」）。
//
// ⚠️ **它只 `Push`**：登录页**会**进历史——用户按「后退」要能回到它（他
// 可能想反悔重扫一次）。真正把它从历史里去掉的是**登录成功那一下**的
// `Replace`（见 `finishLogin`）。
//
// 不用 `navigate`：那三句是**原样抄**的（清 Toast / 清 crash / Push），
// `navigate` 自己**一条 Toast 都不弹**了（见 `router.go` 的 `navigate`）。
//
// ⚠️ 这一段注释曾经写着「`navigate` 是给『还没做的页面』用的，每切一次都会弹
// 『这一页还没做』」——`navigate` 早就不弹那句了（原型那版才弹）。真照着那句去读
// 代码的人会以为换掉 `navigate` 会少一个提示，而实际上少不掉任何东西。
func (s *Shell) goLogin(after string) {
	s.clearToasts()
	s.crash = nil
	s.loginAfter = after
	s.router.Push(loginPath)
}

// syncLoginSuccess 是「**刚刚**登录成功那一下」的那一次性副作用。
//
// # 为什么它不在登录页的 view 里
//
// 登录成功是**异步的状态变化**（扫码 → B 站回调 → 凭据落盘 → 状态变成
// `LoginDone`），而登录页是**每帧重画**的——写在 view 里的话，那一下会
// **每帧做一次**（每帧都看得见「现在是已登录」）。
//
// 所以判据是**边沿**：「上一帧不是 `LoginDone`、这一帧是」。
// `lastLoginPhase` 记着上一帧那一个。
//
// ⚠️ **它在 `syncUnderlay` 那一层跑，不在 `pageSyncs` 里**：登录成功的时候
// 用户**可能已经不在登录页上了**（他扫完码就自己走了），而登录页自己的
// `syncLogin` 只在当前路由是 `/login` 时才跑——放那儿就漏掉了那一半。
//
// ⚠️ **只查相位，不查「有没有登录态」**：`LoggedIn()` 跟着凭据走，而启动
// 时凭据是从盘上读回来的（那也是「已登录」），拿它当判据的话，**应用一开
// 就算登录成功过一次**——而那一次用户什么也没做。
func (s *Shell) syncLoginSuccess() {
	if s.app == nil {
		return
	}
	phase := s.app.LoginState().Phase
	prev := s.lastLoginPhase
	// ⚠️ **每一帧都要更新**：不更新的话，下一帧的「上一帧」还是老的那一个，
	// 边沿就永远判不出来（而这一句是**唯一**记着上一帧相位的地方）。
	s.lastLoginPhase = phase
	if phase != app.LoginDone || prev == app.LoginDone {
		return
	}
	s.finishLogin()
}

// finishLogin 是「登录成功了」那一下要做的事：说一句、回该去的页面、
// 清掉登录页的临时状态。
//
// ⚠️ **它只做一次**：调用它的是 `syncLoginSuccess` 的那个**边沿**
// （见那个函数的注释）。
func (s *Shell) finishLogin() {
	// 目标页：进登录页时记下的那个。**空串 = 首页**。
	target := s.loginAfter
	s.loginAfter = ""
	if target == "" {
		target = homePath
	}

	// ⚠️ **清掉登录页的临时状态**：二维码那张位图是 **GPU 纹理**，留在壳上
	// 就一直占着显存，而登录页已经不在了——没人会再看它一眼。
	//
	// （**轮询那个计时器不用这里管**：它在 `app.Account` 那一层，登录成功的
	// 时候那一轮 `run` 自己就返回了，`ticker` 跟着 `defer` 收掉。而扫码状态
	// ——那张二维码本身——在 `LoginState` 里，成功那一下就被换成了
	// `Phase: LoginDone` 的新状态，那一份里没有 `QR`。）
	s.qrFor, s.qrBitmap, s.qrDimBitmap = nil, nil, nil

	// ⚠️ **只在他还停在登录页上的时候才把他带走**：他扫完码自己走了的话，
	// 把他从当前页拽走是抢他的操作（边界那一条，见收尾说明）。
	if s.router.Path() == loginPath {
		// ⚠️ **用 `Replace`，不是 `Push`**：登录页**不许留在历史里**——
		// 用户按「后退」不该回到登录页（那里是登录成功之前的样子）。
		//
		// ⚠️ **不走 `navigate`**：它开头会 `clearToasts()`，而「登录成功」
		// 那句话正要弹出来——先跳页再说话，顺序反了。
		s.router.Replace(target)
	}
	// 说话。**放在跳页之后**：跳页那一步要是先说话，这一句会被
	// `navigate`（或者别处的清提示）收掉。
	//
	// ⚠️ **逐字照上游**（`src/layout/navbar/login/qrcode-login.tsx` 的
	// `addToast({ title: "登录成功", color: "success" })`）。
	s.addToast("登录成功", notice.Success)
}

// viewLogin 是登录页：原型 `PAGES.login`（app.js 788 行）+ CSS（index.html 534–544 行）。
//
// 整页居中（`.login`：上下 48、左右 24），中间一张 `.login-card`：左边 240×240 的二维码卡（白底、
// 内边距 16、圆角 12；已扫码时盖一层 86% 的黑罩 + 对勾 + 「已扫码」），右边 Eyebrow `Sign in` +
// 40 号宋体「扫码登录」+ 一句说明 + 三个步骤 + 按钮 + 一行状态，两列隔 48，右列最宽 320。
func (s *Shell) viewLogin(c *ui.Context, _ *ui.Route) {
	st := s.app.LoginState()

	loginScroll := ui.Scroll(c).Key("login-scroll").Grow(1)
	loginScroll.Children(func() {
		// 整页垂直居中（`.login { min-height: 100%; display: grid; place-items: center }`）：滚动容器不给子元素
		// 定高，所以最小高度自己算——窗口高减顶栏、减播放条。
		winW, winH := c.Size()
		// 原型 index.html:943-944：窗口 ≤ 1024 时 `.login-card` 变单列、居中、文字居中、缝 32；
		// 宽窗口两栏、缝 48。
		narrow := loginNarrow(winW)
		ui.Column(c).Key("page:"+loginPath).FillWidth().MinHeight(winH-navMinHeight-playbarHeight).Padding(space.Px48, space.Px24).
			Justify(ui.Center).AlignItems(ui.Center).Transition(motion.PageIn).Children(func() {
			var card ui.Element
			if narrow {
				card = ui.Column(c).Key("login-card").Gap(space.Px32).AlignItems(ui.Center)
			} else {
				card = ui.Row(c).Key("login-card").Gap(space.Px48).AlignItems(ui.Center)
			}
			card.Children(func() {
				s.viewLoginQR(c, st)
				s.viewLoginText(c, st, narrow)
			})
		})
	})
	s.trackScrollbar(loginScroll)
}

// loginNarrowMax 是登录页改单列的窗口宽上限（原型 `@media (max-width: 1024px)`）。
const loginNarrowMax = 1024

// loginNarrow 报登录页这一帧是不是单列（窗口宽 ≤ 1024）。
func loginNarrow(winW float32) bool { return winW > 0 && winW <= loginNarrowMax }

// loginTitleLH 是登录标题的行高（原型 `.h-page { line-height: 1.15 }`，index.html:268）。
const loginTitleLH = 1.15

const (
	// loginQRSide / loginQRPad 是 `.qr`：240×240、内边距 16（二维码本身画在 208 的框里）。
	loginQRSide = 240
	loginQRPad  = space.Px16
	// loginTextW 是右列的最大宽（`minmax(0, 320px)`）。
	loginTextW = 320
)

// loginStep 是登录进行到第几步（原型 `S.login.step`）：0 等扫码，1 已扫码等确认，2 成功。
func loginStep(st app.LoginState) int {
	switch st.Phase {
	case app.LoginScanned:
		return 1
	case app.LoginDone:
		return 2
	}
	return 0
}

// viewLoginQR 是左边的二维码卡。没有二维码（还没开始 / 失败）时只有白底的空卡。
func (s *Shell) viewLoginQR(c *ui.Context, st app.LoginState) {
	card := ui.Box(c).Key("login-qr").Width(loginQRSide).Height(loginQRSide).Shrink(0).
		Padding(loginQRPad).Radius(tok.R12).Background(ui.RGB(255, 255, 255)).
		Justify(ui.Center).AlignItems(ui.Center).Label("登录二维码")
	card.Children(func() {
		s.qrImage(c, st.QR, st.Phase == app.LoginExpired)
		if st.Phase == app.LoginScanned || st.Phase == app.LoginDone {
			// `.qr-mask`：盖满整张卡、圆角 12、rgba(14,15,17,.86)；对勾 40 + 「已扫码」+ 13 号提示，居中，缝 8。
			// 已经登录（再进登录页）时同一块罩子写「已登录」、不写那句提示。
			title, hint := "已扫码", "请在手机上确认登录"
			if st.Phase == app.LoginDone {
				title, hint = "已登录", ""
			}
			ui.Column(c).Key("login-qr-mask").Absolute().Left(0).Top(0).Right(0).Bottom(0).
				Radius(tok.R12).Background(ui.RGBA(14, 15, 17, 0.86)).Padding(space.Px24).Gap(space.Px8).
				Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				fg := ui.Hex("#ecebe8")
				ui.Icon(c, pud.Icon("check")).FontSize(40).TextColor(fg)
				ui.Text(c, title).FontSize(scale.Font(c, scale.Size14)).LineHeight(leading.Body).FontWeight(700).TextColor(fg)
				if hint != "" {
					ui.Text(c, hint).FontSize(scale.Font(c, scale.Size13)).LineHeight(leading.Body).TextColor(ui.Hex("#b6b5b2"))
				}
			})
		}
	})
}

// viewLoginText 是右边那一列：标题、说明、三个步骤、按钮、状态。narrow（窗口 ≤ 1024）时整列
// 居中、文字居中（原型 index.html:943-944 `.login-card { justify-items: center; text-align: center }`、
// `.steps li { justify-content: center }`）。
func (s *Shell) viewLoginText(c *ui.Context, st app.LoginState, narrow bool) {
	p := tok.Of(c.Theme())
	step := loginStep(st)
	col := ui.Column(c).Key("login-text").Width(loginTextW).Shrink(1)
	align := ui.Start
	if narrow {
		col, align = col.AlignItems(ui.Center), ui.Center
	}
	col.Children(func() {
		typo.Eyebrow(c, "Sign in")
		// 原型 `.h-page`（index.html:268）+ app.js:792 `font-size:40px`：行高 1.15、字距 .01em、上 8。
		// 框架压不下宋体的自然行高，用 `typo.DisplayTrim` 的负外边距补回去（同 `pud.PageHead`）。
		size := scale.Font(c, scale.Size40)
		trim := typo.DisplayTrim(size, loginTitleLH)
		typo.Display(ui.Text(c, "扫码登录"), size).LineHeight(loginTitleLH).LetterSpacing(size*0.01).
			Margin(space.Px8-trim, space.None, -trim, space.None)
		ui.Text(c, "继续你的音乐旅程：登录后同步收藏夹、关注和历史记录，并解锁 Hi-Res 无损音质。").
			FontSize(scale.Font(c, scale.Size14)).TextColor(p.Text2).Wrap().FillWidth().TextAlign(align).
			Margin(space.Px8, space.None, space.None, space.None)

		// `.steps`：上下各 24、步骤之间 12。
		ui.Column(c).Key("login-steps").FillWidth().Gap(space.Px12).Margin(space.Px24, space.None, space.Px24, space.None).Children(func() {
			for i, label := range []string{"打开手机上的 B 站 App", "扫描左侧二维码", "在手机上确认登录"} {
				s.loginStepRow(c, p, i+1, label, i == 0 || step >= i, narrow)
			}
		})

		ui.Row(c).Key("qr-actions").Gap(space.Px12).AlignItems(ui.Center).Children(func() { s.qrActions(c, st) })
		ui.Text(c, loginStatusLine(st)).FontSize(scale.Font(c, scale.Size12)).LineHeight(leading.Body).TextColor(p.Text3).Wrap().
			FillWidth().TextAlign(align).Margin(space.Px24, space.None, space.None, space.None)
		s.viewAccountFooter(c, st)
	})
}

// loginStepRow 是一个步骤（`.steps li`）：22 的圆圈里写序号（等宽 12），缝 12；做完的圆圈填主色、
// 字变正文色。圆圈里的序号和步骤文字按基线对齐（原型 index.html:541 `align-items: baseline`）；
// narrow 时整行居中。
func (s *Shell) loginStepRow(c *ui.Context, p tok.Palette, n int, label string, done, narrow bool) {
	row := ui.Row(c).Key("login-step:" + label).FillWidth().Gap(space.Px12).AlignItems(ui.Baseline)
	if narrow {
		row = row.Justify(ui.Center)
	}
	row.Children(func() {
		circle := ui.Row(c).Width(22).Height(22).Radius(tok.Pill).Shrink(0).Justify(ui.Center).AlignItems(ui.Center)
		fg := p.Text2
		if done {
			circle = circle.Background(p.Accent).Border(1, p.Accent)
			fg = p.AccentInk
		} else {
			circle = circle.Border(1, p.LineStrong)
		}
		circle.Children(func() {
			typo.Mono(ui.Text(c, strconv.Itoa(n))).FontSize(scale.Font(c, scale.Size12)).LineHeight(leading.Body).TextColor(fg)
		})
		txt := p.Text2
		if done {
			txt = p.Text
		}
		ui.Text(c, label).FontSize(scale.Font(c, scale.Size14)).LineHeight(leading.Body).TextColor(txt).Wrap().Shrink(1)
	})
}

// loginStatusLine 是最底下那行 12 号的状态（原型 `aria-live` 那一句）。
func loginStatusLine(st app.LoginState) string {
	switch st.Phase {
	case app.LoginScanned:
		return "已扫码，等待确认…"
	case app.LoginDone:
		// 原型第 3 步的「登录成功，正在同步…」是一闪而过的：登录成功时页面马上换到去向页并弹「登录成功」，
		// 这一行只会在已登录时再进登录页才看到，所以写现状。
		return "已登录，可以换一个账号。"
	case app.LoginExpired:
		return "二维码已过期，点「刷新二维码」重新获取。"
	case app.LoginFailed:
		if st.Err != "" {
			return "登录没成功：" + st.Err
		}
		return "登录没成功，点「重试」再来一次。"
	case app.LoginIdle:
		return "点「开始扫码登录」获取二维码。"
	}
	return "正在等待扫码…"
}

// syncLogin 是登录页的每帧副作用。
//
// 登录页第一次出现时直接开始申请二维码。用跨帧标记只触发一次，避免每帧
// 重新申请并取消上一轮轮询；用户主动刷新、重试或取消后也不会被下一帧偷偷
// 重新启动。
func (s *Shell) syncLogin(c *ui.Context) {
	if s.loginAutoDisabled {
		return
	}
	if s.loginAutoStarted {
		return
	}
	s.loginAutoStarted = true
	if s.app.LoginState().Phase == app.LoginIdle {
		s.clearToasts()
		s.app.BeginLogin()
	}
}

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

// qrActions 是按钮行（原型：幽灵键「刷新二维码」+ 安静键「暂不登录」）。
//
// 失败后是主色「重试」；
// 其余阶段都是「刷新二维码」。「暂不登录」取消这一轮登录并回到上一页。
func (s *Shell) qrActions(c *ui.Context, st app.LoginState) {
	switch st.Phase {
	case app.LoginIdle:
		// syncLogin 会在首次进入页面时自动开始；这一帧仍处于 idle 时保留
		// 同一个入口作为网络启动失败后的手动兜底。
		if pud.Button(c, "login-begin", pud.Primary, pud.Std, "开始扫码登录", "").Clicked() {
			s.clearToasts()
			s.app.BeginLogin()
		}
	case app.LoginFailed:
		if pud.Button(c, "login-retry", pud.Primary, pud.Std, "重试", "refresh").Clicked() {
			s.clearToasts()
			s.app.BeginLogin()
		}
	case app.LoginDone:
		if pud.Button(c, "login-home", pud.Primary, pud.Std, "去首页", "").Clicked() {
			s.navigate("/")
		}
		if pud.Button(c, "login-switch", pud.Ghost, pud.Std, "换一个账号", "").Clicked() {
			s.logoutAndStartOver()
		}
		return
	default:
		if pud.Button(c, "login-refresh", pud.Ghost, pud.Std, "刷新二维码", "refresh").Clicked() {
			s.clearToasts()
			s.app.RefreshLoginCode()
		}
	}
	// 原型 app.js:795：只有有地方可回（返回目标 / 历史）时才摆「暂不登录」，否则点了也没处去。
	if !s.router.CanGoBack() {
		return
	}
	if pud.Button(c, "login-skip", pud.Quiet, pud.Std, "暂不登录", "").Clicked() {
		s.app.CancelLogin()
		if s.router.Path() == loginPath {
			s.router.Back()
		}
	}
}

// viewAccountFooter 是已登录时多出来的一行：UID + 凭据后端提示 + 退出登录。
func (s *Shell) viewAccountFooter(c *ui.Context, st app.LoginState) {
	if !st.LoggedIn {
		return
	}
	p := tok.Of(c.Theme())
	sess := s.app.Session()
	ui.Row(c).Key("account-footer").Gap(space.Px12).AlignItems(ui.Center).
		Margin(space.Px16, space.None, space.None, space.None).Children(func() {
		who := "已登录"
		if uid := sess.DedeUserID; uid != "" {
			who = "已登录（UID " + uid + "）"
		}
		ui.Text(c, who).FontSize(scale.Font(c, scale.Size12)).LineHeight(leading.Body).TextColor(p.Text3)
		if s.app.CredentialBackendName() == "none" {
			ui.Text(c, "凭据后端不可用，登录态关掉应用就没了").FontSize(scale.Font(c, scale.Size12)).LineHeight(leading.Body).TextColor(p.Text3)
		}
		if pud.Button(c, "login-logout", pud.Quiet, pud.Sm, "退出登录", "").Clicked() {
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
