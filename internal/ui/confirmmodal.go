// 确认框：**应用自绘**的那一种，不是系统原生的 `AlertDialog`。
//
// # 上游是 `components/confirm-modal/index.tsx`
//
// 上游那一份是一个全局弹窗（`useModalStore`），形状只有三样：
//
//	标题行： [警告图标] 标题        ← 图标的颜色按类型走
//	正文：   `text-sm` 的说明
//	按钮行： [取消（light）] [确认（实心，颜色按类型走）]
//
// 抄的是这四样：图标、标题、正文、两颗按钮的档位。
//
// ⚠️ **「取消」那一颗没照抄**：上游是 `variant="light"`（透明底、无描边），而
// 这一颗**带壳**（常驻底 + 描边）——仓库自己的契约（票 #141「铺开扁平按钮」
// 明确留下了对话框这一批），理由见 `cancelButton`。
//
// # 为什么**不做成全局的**
//
// 上游全局，是因为它整套界面只有一处弹窗逻辑。我们这里每一页自己管自己的确认
// 状态（`p.confirm` / `d.ask` / `p.rmOpen` / `p.rmOpen`），而多一份包级的
// 「当前弹的是哪一个」就多一处真相——那种错是**静默**的：屏幕上弹着另一个框，
// 用户按了确认，动的是别的东西（而那件事多半会动用户的东西）。
//
// # 三种类型
//
// 逐字照上游 `index.tsx:9-14`：
//
//	type ConfirmModalType = "warning" | "danger" | "primary"
//	const colorMap = { warning: "#F5A524", danger: "#dc1258" }
//
// ⚠️ **danger 的色值按 DESIGN.md §四 走**（`#f31260`）而不是上游那一个
// `#dc1258`——票 #172 已经把主题的危险色换成上游那一档了，而 DESIGN.md 是
// 唯一目标值（§一 的两条规矩）。
//
// ⚠️ **primary 那一档的图标不上色**（上游 `colorMap` 里根本没有 "primary" 这一项，
// `colorMap[type]` 是 undefined，图标于是继承文字色）。这里照它：图标用
// `t.Text`。给 primary 也铺一个主色不算发明，但**上游没有**——DESIGN.md §一
// 第二条规矩是「不在这里的项，不要自己发明」。
//
// # 哪一处在用哪一种
//
// 逐条照上游用它自己的那几处（`type` 字段）：
//
//	危险（红）  删收藏夹 / 删选中的歌 / 删下载记录 / 删本地文件
//	警告（橙）  清除失效内容 / 移除本地目录
//	普通（主色） 批量下载 / 从侧栏移除
//
// ⚠️ **「移除本地目录」原来在本地音乐那一页是红色的**（`localmusic_actions.go`
// 把它当 danger 画），而上游那一处是 `type: "warning"`（`local-music/index.tsx:113`）。
// 换过去之后它是橙的——那一档本来就比红色轻（「只从列表里拿掉目录，不删文件」）。
//
// # 🔴 「点外面」是**取消**，不是「什么都不发生」
//
// 上游那个弹窗 `isDismissable={false}`（点外面不关）。我们做不到一模一样：
// 框架的 `ui.Modal` 把「点外面 / 按 Esc 关掉」做好了（`Dismissed` 报它），而
// 自己用 `ui.Overlay` 重做一块遮罩就得连**焦点圈定**（`Modal()` 那个 flag）
// 一起重做——漏了它的后果是「弹窗开着的时候 Tab 还能跑到后面的页上去」。
//
// 所以这里用 `ui.Modal`，点外面 = 取消。**这比上游安全**：上游那一种下，用户在
// 框外面点一下什么也不会发生（框还在），而我们这一种是「当成了取消」——
// 会动用户的东西的那一下因此需要用户**再按一次确认**，而不会因为手滑点空了
// 地方就动它。
//
// ⚠️ **但遮罩照样吃掉那一下点击**：框架的遮罩是 `flagClickable` 的，所以点外面
// 不会「穿过去」按到下面的按钮上。这正是原来那两处 `AlertDialog` 注释里记着的
// 担心（「自绘浮层很容易漏掉，而漏掉的后果是误删一整批」）。
package ui

import (
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scrim"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/shadow"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

// confirmTone 是确认框的类型：它决定警告图标的颜色和确认按钮的颜色。
type confirmTone uint8

const (
	// tonePrimary 是普通的那一种（上游的 "primary"）：图标是文字色，按钮是主色。
	tonePrimary confirmTone = iota
	// toneWarning 是警告（上游的 "warning"）：图标和按钮都是橙的。
	toneWarning
	// toneDanger 是危险（上游的 "danger"）：图标和按钮都是红的。
	toneDanger
)

// confirmSpec 是一道确认框要说的事。
type confirmSpec struct {
	// tone 是这一道的类型（决定图标和确认按钮的颜色）。
	tone confirmTone
	// title 是标题。它**必须点名动的是哪一样**——两个框长得一样而用户分不出
	// 按的是哪一个，是这一类弹窗最贵的那种错（规格 issue #100 点名过）。
	title string
	// body 是正文（`text-sm`）。空着就不画那一行。
	body string
	// detail 是可选的第三行：用户按下按钮之前能用来认人的那一样东西
	// （本地音乐那个「删的是哪个文件」用的就是完整路径）。
	detail string
	// confirm / cancel 是两颗按钮上的字。
	confirm, cancel string
}

// 确认框的尺寸。
//
// ⚠️ **420 是本地音乐那一页原来那个数**（`localmusic.go` 的 `dialogWidth`）：
// 路径那一行在 420 宽下大概能放 45 个字符，再窄就开始一行一截地断。
//
// ⚠️ **上限之外还有一个下限**（票 #241 / 报告 F-20）：原来这里**只有上限**，
// 于是**框宽跟着内容走**——而内容最短的那一类弹窗（短标题）于是看着最小：
// 「确认删除选中的 2 首吗？」那个框在 1200×800 的窗口里只有 **228 宽**，
// 标题几乎顶到边。上游那个弹窗是 HeroUI 的 `max-w-lg`（**512**），短标题
// 那一类在那边也是 512 宽——**上游也是「短标题也是宽框」**。
//
// 现在给一个**下限 300**（加上面板自己的内边距，面板是 **340 宽**，落在报告
// 建议的 320~360 里）：短标题的框不再缩成一条，长内容仍然能长到 420。
//
// ⚠️ **为什么是「下限 + 上限」而不是「定宽」**：定宽的话长标题（本地音乐那个
// 「删的是哪个文件」的完整路径）会被迫折行，而那一行折了之后用户认不出是哪个
// 文件——上限是留给它的。
const (
	confirmMinW = 300
	confirmMaxW = 420
	// confirmGap 是面板里各行之间的距离。
	//
	// ⚠️ **12 是仓库里那两个确认框原来的数**（本地音乐那一页的 `dialogGap`
	// 和下载记录页那一行都是 12）——换过去之后两个地方和这里对得上，而用户
	// 看过的那两个框就是这个疏密。
	confirmGap = space.Px12
	// confirmIconSize 是警告图标的大小。
	//
	// 18 是上游那一枚的大小：`@remixicon/react` 的默认 size 是 `1em`，而
	// HeroUI 的 `ModalHeader` 是 `text-large`（18px）。
	confirmIconSize = 18
)

// confirmOnWarning 是橙色按钮上的字色。
//
// ⚠️ **不能是白的**：上游那个 `warning` 是 `#f5a524`，白字压在上面只有
// **2.04:1**（DESIGN.md §四 那张表里「警告 浅 2.04:1」就是它）。深色档的
// `#27272a` 压在上面是 **8.9:1**——够读。
//
// 这一条和「主色当字色要压到 ≥4.5:1」是同一条规矩（DESIGN.md §四）：
// 浅色下压不住的颜色要另取一档，而不是硬用。
var confirmOnWarning = ui.RGB(24, 24, 27)

// dialogOnDanger 是红色确认按钮上的字。
//
// 主题给的是 `Danger` 那一个颜色，没有配套的「危险底上的前景色」，而这里
// 需要的正是后者。白色在上游那一档 Danger（`#f31260`）上是 4.1:1——比改之前的
// 深色档 `#ef4444`（3.8:1）还高一点，所以就用白。
var dialogOnDanger = ui.RGB(255, 255, 255)

// confirmOutcome 是确认框这一帧的结果。
//
// 🔴 **三档而不是一个 bool**：弹窗**开着而没有人按**也是一种结果，而它和
// 「被取消了」必须分开——调用方要在「被取消了」的时候清掉自己那份状态
// （不然它下一帧还会再弹一次），而在「还开着」的时候**不能**清（清了弹窗就
// 没了，而用户还没点）。
//
// 把它做成一个 bool 的那一版真的红过：确认框在第一帧就「不是确认」，于是
// 调用方立刻把状态清了，弹窗一帧都没活下来。
type confirmOutcome uint8

const (
	// confirmStillOpen 是「弹窗开着，还没有人按任何按钮」。
	confirmStillOpen confirmOutcome = iota
	// confirmCancelled 是「弹窗被关掉了」：按了取消、按了 Esc、点了外面。
	confirmCancelled
	// confirmAccepted 是「按了确认」。
	confirmAccepted
)

// confirmModal 画一道确认框，报这一帧的结果。
//
// ⚠️ **按了「取消」和按了 Esc 都算 `confirmCancelled`**，而它们和「确认」的
// 区别不在同一个返回值里——所以这里给的是三档。
//
// 调用方要做的只有一件：**`confirmAccepted` 才干活**。
func confirmModal(c *ui.Context, open *bool, spec confirmSpec) confirmOutcome {
	if !*open {
		return confirmStillOpen
	}
	pressed := ""

	// 🔴 **用 `ui.DialogBase` 而不是 `ui.Modal`**——因为**面板得有个名字**。
	//
	// 系统原生那一份（`ui.AlertDialog`）建面板时是
	// `Role(RoleAlertDialog)` + `panel.Label(title)` + `panel.description = message`
	//（mygo v0.6.3 的 `ui/feedback.go:186-188`），所以读屏软件报得出「删除确认吗？」
	// 和它下面那一句正文。
	//
	// 而 `ui.Modal`（`coreDialogBase`，`ui/base.go:486`）只给
	// `Role(RoleDialog)`——**既没有名字也没有描述**。拿它换掉原生那一份之后，
	// 读屏软件只报「一个对话框」，标题和正文都不报了。
	//
	// ⚠️ `ui.Modal` 的回调**拿不到面板**（签名是 `fn func()`），而
	// `ui.DialogBase` 给（`fn func(backdrop, panel)`）——所以这里用后者，
	// **自己把 `ui.Modal` 那套样子摆一遍**（下一段就是照抄 `coreModal` 的）。
	// 🔴 **确认框的身份也得钉死**（`c.Key`）：框架给元素的身份是「父的身份 +
	// 兄弟序号」（`mygo/ui/context.go` 的 `newElement`），而**浮层里的东西会
	// 多会少**——头像下拉那一层浮层开着的时候，它占着浮层里的一个位置；它关掉
	// 的时候那个位置就没了。于是**确认框里那几颗键的身份跟着变**，而按下和
	// 抬起之间隔着帧：按下记在旧身份上，抬起之后那一颗已经是新身份——**那一下
	// 点空**（确认框还开着，退登没发生）。
	//
	// ⚠️ **给的是那个「底幕」**：`ui.DialogBase` 先建底幕、再把面板建在底幕
	// **里面**（`mygo/ui/base.go` 的 `coreDialogBase`），而 `Context.Key` 是给
	// **下一个**建出来的元素用的——所以键落在底幕上，而面板的身份是「底幕的
	// 身份 + 兄弟序号」，底幕的身份钉死了它也就钉死了。
	//
	// ⚠️ **这一条不是这一票引入的**（HEAD 上也有），但这一票把「头像下拉」和
	// 「退登确认」这两层浮层接在了一起（顶栏那枚独立的「退出」按钮没了，退登
	// 走下拉里那一项），于是它更容易撞上。
	c.Key("confirm-backdrop:" + spec.title)
	m := ui.DialogBase(c, open, func(backdrop, panel ui.Element) {
		protoDialogPanel(c, backdrop, panel, tok.DialogW).Role(ui.RoleAlertDialog).
			Label(spec.title).
			Description(spec.body)

		ui.Column(c).Key("confirm:" + spec.title).FillWidth().Children(func() {
			pud.DialogBody(c, spec.title, spec.body)
			if spec.detail != "" {
				ui.Text(c, spec.detail).FontSize(scale.Font(c, scale.Size12)).TextColor(tok.Of(c.Theme()).Text3).Wrap().
					Padding(space.None, space.Px24, space.Px16, space.Px24)
			}
		})
		// 按钮行是面板的直接子元素（不套进上面那个 Column：套进去命中区域和面板对不上，看得见、点不动）。
		pud.DialogFoot(c, func() {
			if pud.Button(c, "confirm-cancel:"+spec.title, pud.Ghost, pud.Std, spec.cancel, "").Clicked() {
				pressed = spec.cancel
			}
			if confirmButton(c, spec).Clicked() {
				pressed = spec.confirm
			}
		})
	})

	if m.Dismissed() {
		return confirmCancelled
	}
	if pressed == "" {
		// ⚠️ **这里不能顺手把 `*open` 关掉**：没有人按的时候弹窗还开着，
		// 关掉它就等于「弹窗只活一帧」。
		return confirmStillOpen
	}
	// ⚠️ **先关掉再做那件事**：做的时候可能弹提示（addToast），而弹窗还开着的话
	// 用户看到的是提示压在弹窗底下。
	*open = false
	if pressed == spec.confirm {
		return confirmAccepted
	}
	return confirmCancelled
}

// confirmButton 是确认键。原型的确认键一律是 `.btn-danger`（err 底 + 白字，index.html 288 行）；
// 这里主色调的确认（非破坏性）仍是主色键，警告和危险都是 err 底。无障碍名字是「确认」+ 字，
// 和取消区分开。
func confirmButton(c *ui.Context, spec confirmSpec) ui.Element {
	kind := pud.Danger
	if spec.tone == tonePrimary {
		kind = pud.Primary
	}
	return pud.Button(c, "confirm-ok:"+spec.title, kind, pud.Std, spec.confirm, "").Label("确认" + spec.confirm)
}

// dialogPanel 是弹窗面板的统一外观：遮罩、内边距、圆角、底色、阴影。
//
// `confirmModal` 的那一套抽出来给「更新」「裁剪封面」两个弹窗共用（它们原来走框架的
// `ui.Modal`，面板样式写死成框架那份，拿不到方案里的 `surface1` / `radiusLg`）。
// 角色用 `RoleDialog`，名字用 label；`ui.DialogBase` 的回调拿得到面板，所以能挂名字。
func dialogPanel(c *ui.Context, backdrop, panel ui.Element, label string) {
	t := c.Theme()
	w, h := c.Size()
	card := dialogCard(t)
	backdrop.Background(ui.RGBA(0, 0, 0, scrim.Modal))
	shadow.Modal(panel.Padding(t.Space(5)).Gap(t.Space(3)).Radius(card.radius).
		Background(card.bg).
		MaxWidth(w-t.Space(10)).MaxHeight(h-t.Space(10)), t).
		Role(ui.RoleDialog).Label(label)
}

// protoDialogPanel 给 `ui.DialogBase` 的底幕和面板上原型 `dialog.dlg` 的样子：底幕 50% 黑
// （`dialog.dlg::backdrop`）；面板宽 min(w, 窗口 − 32)、surface 底、1px line 描边、圆角 12、
// `--shadow-pop`，进场 `pop-in`（`motion.PopIn`）。面板自己没有内边距——内边距在 `.dlg-body` /
// `.dlg-foot`（`pud.DialogBody` / `pud.DialogFoot`）里。w 是 `.dlg` 的宽：确认框 440、`.sheet` 480。
func protoDialogPanel(c *ui.Context, backdrop, panel ui.Element, w float32) ui.Element {
	t := c.Theme()
	p := tok.Of(t)
	winW, winH := c.Size()
	backdrop.Background(ui.RGBA(0, 0, 0, scrim.Dialog))
	return shadow.Pop(panel.Width(min(w, winW-32)).MaxHeight(winH-64).Radius(tok.R12).
		Background(p.Surface).Border(1, p.Line).Transition(motion.PopIn), t)
}
