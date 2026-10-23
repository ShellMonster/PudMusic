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
// 用户按了确认，动的是别的东西（而那件事多半不可逆）。
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
// 不可逆的那一下因此需要用户**再按一次确认**，而不会因为手滑点空了地方就删掉。
//
// ⚠️ **但遮罩照样吃掉那一下点击**：框架的遮罩是 `flagClickable` 的，所以点外面
// 不会「穿过去」按到下面的按钮上。这正是原来那两处 `AlertDialog` 注释里记着的
// 担心（「自绘浮层很容易漏掉，而漏掉的后果是误删一整批」）。
package ui

import (
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/shadow"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
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
// 上游那个弹窗是**跟着内容走**的（HeroUI 的 `max-width` 只封上限，短标题就窄）。
// 这里给一个上限而不是定宽：短标题的框窄一点、长标题的框宽一点，和上游一样。
const (
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
	t := c.Theme()
	pressed := ""

	// 🔴 **用 `ui.DialogBase` 而不是 `ui.Modal`**——因为**面板得有个名字**。
	//
	// 系统原生那一份（`ui.AlertDialog`）建面板时是
	// `Role(RoleAlertDialog)` + `panel.Label(title)` + `panel.description = message`
	//（mygo v0.6.2 的 `ui/feedback.go:186-188`），所以读屏软件报得出「删除确认吗？」
	// 和它下面那一句正文。
	//
	// 而 `ui.Modal`（`coreDialogBase`，`ui/base.go:486`）只给
	// `Role(RoleDialog)`——**既没有名字也没有描述**。拿它换掉原生那一份之后，
	// 读屏软件只报「一个对话框」，标题和正文都不报了。
	//
	// ⚠️ `ui.Modal` 的回调**拿不到面板**（签名是 `fn func()`），而
	// `ui.DialogBase` 给（`fn func(backdrop, panel)`）——所以这里用后者，
	// **自己把 `ui.Modal` 那套样子摆一遍**（下一段就是照抄 `coreModal` 的）。
	m := ui.DialogBase(c, open, func(backdrop, panel ui.Element) {
		w, h := c.Size()
		backdrop.Background(ui.RGBA(0, 0, 0, 0.4))
		// ⚠️ **影的值从 `widgets/shadow` 取**（`shadow.Modal`）：值只写一处
		// （DESIGN.md §四·五，票 #239 把这一处收进去了）。
		panel = shadow.Modal(panel.Padding(t.Space(5)).Gap(t.Space(3)).Radius(t.Space(2.5)).
			Background(t.Background).
			MaxWidth(w-t.Space(10)).MaxHeight(h-t.Space(10)),
			t).
			// 🔴 **这三样是「读屏软件报得出这个框是什么」**：角色、名字、描述。
			// ⚠️ `Description` 对**不是输入框**的元素不会另外画一行字
			// （`mygo/ui/form.go` 的 `fieldText`：`e.field == nil` 就原样返回），
			// 所以这里只是给面板挂上那句描述，画面上不多东西。
			Role(ui.RoleAlertDialog).
			Label(spec.title).
			Description(spec.body)

		ui.Column(c).Key("confirm:" + spec.title).MaxWidth(confirmMaxW).Gap(confirmGap).
			Children(func() {
				// 标题那一行：按类型变色的警告图标 + 标题。
				//
				// ⚠️ 图标和标题**同一行**（上游的 `flex items-center gap-1`，
				// gap-1 = 4）。
				ui.Row(c).Gap(space.Px4).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, icon(iconWarning)).FontSize(confirmIconSize).
						TextColor(confirmIconColor(t, spec.tone))
					ui.Text(c, spec.title).FontSize(scale.Font(c, 16)).Bold()
				})
				if spec.body != "" {
					// 正文是 `text-sm`（14），颜色是上游那个 `text-zinc-500`
					// ——而 `t.TextMuted` 浅色下正是 `#71717a`，逐字相同。
					ui.Text(c, spec.body).FontSize(scale.Font(c, 14)).
						TextColor(t.TextMuted).Wrap()
				}
				if spec.detail != "" {
					ui.Text(c, spec.detail).FontSize(scale.Font(c, 12)).
						TextColor(t.TextMuted).Wrap()
				}
			})
		// 按钮行。⚠️ **它是面板的直接子元素**（不是 Column 里的第三行）：
		// 塞进 Column 的话这一行的宽度是 Column 的宽度，按钮靠右对齐也就跟着
		// 面板走——那倒是想要的；可 `AlignSelf(End)` 才能让这一行**自己**贴右。
		ui.Row(c).AlignSelf(ui.End).Gap(space.Px8).Children(func() {
			if cancelButton(c, spec).Clicked() {
				pressed = spec.cancel
			}
			if confirmButton(c, spec).Clicked() {
				pressed = spec.confirm
			}
		})
	})

	// 点外面 / 按 Esc：`ui.DialogBase` 已经把 `*open` 关掉了，而那一次**不算确认**。
	//
	// ⚠️ **它算「取消」而不是「还开着」**：调用方要在这一档清掉自己那份状态
	// （不然下一帧它又弹一次）。
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

// confirmButton 是那颗「确认」键。
//
// 上游是 `<Button color={type}>`（`index.tsx:79`），也就是**实心、颜色按类型**。
// 三档分别对到：
//
//	primary → `ui.PrimaryButton`（主色实心，悬停 / 按下是主色自己的两档）
//	danger  → 红底白字（`dialogOnDanger` 那个白：白压在 `#f31260` 上是 4.1:1）
//	warning → 橙底深色字（见 `confirmOnWarning` 的注释）
//
// ⚠️ **为什么 danger / warning 用 `flatButton` 而不是 `ui.Button`**：
// `ui.Button` 的默认底色是 `t.Surface` 加一圈 1px 描边，而 `styleButton` 的
// 悬停那一层只在「底色还是它自己的默认底色」时才生效（`b.bg != base` 就
// 直接 return）——所以改过底色的按钮**没有悬停反馈**。`flatButton` 的反馈是
// 画上去的一层淡色（`widgets/flat` 的 `feel`），改过底色照样有。
//
// 而描边那一圈：上游的实心按钮没有描边，`flatButton` 正好也没有。
//
// 🔴 **无障碍名字是「确认 + 那个词」，不是那个词本身**（`Label`）。
//
// 理由不是「好读」：**同一个画面里会有两颗都叫「删除」的键**——下载记录那一页
// 每一行有一颗「删除」，弹窗上又有一颗「删除」。而这一颗是**不可逆**的那一下，
// 读屏软件念出来两个「删除」时用户分不出自己按的是哪一颗。
//
// 上游没有这一层（HeroUI 的按钮没有单独的无障碍名字，名字就是可见的字），
// 而上游那一页上弹窗和行里的键**不同时在场**（弹窗是模态，底下的键看不见也
// 摸不着）。我们这边一样——所以这一层是**我们加的**，加的理由是「两个同名
// 的不可逆动作」，不是「上游有」。
func confirmButton(c *ui.Context, spec confirmSpec) ui.Element {
	t := c.Theme()
	name := "确认" + spec.confirm
	switch spec.tone {
	case toneDanger:
		return flatButton(c, spec.confirm).Label(name).
			Background(t.Danger).TextColor(dialogOnDanger)
	case toneWarning:
		return flatButton(c, spec.confirm).Label(name).
			Background(t.Warning).TextColor(confirmOnWarning)
	}
	return ui.PrimaryButton(c, spec.confirm).Label(name)
}

// cancelButton 是那颗「取消」键。
//
// 🔴 **它带壳（常驻底 + 描边），不是扁的**——这一条是**仓库自己的**契约
// （票 #141「铺开扁平按钮」），不是上游的。
//
// 上游那个弹窗的取消键是 HeroUI 的 `variant="light"`：透明底、无描边。而 #141
// 那一票把页面上**一批**药丸拍平的时候，**明确留下了对话框这一批**
// （`internal/ui/flatspread_test.go` 的 `TestTheDialogButtonsStayOutlined` 钉着
// 那一件）——理由是「主次分明的地方，去掉描边就没了主次」。
//
// ⚠️ **#176 把两处系统原生 `ui.AlertDialog` 换成自绘模态的时候，把这一件一起
// 丢了**：系统原生那一份的取消键是 `coreButton`
// （`mygo@v0.6.2/ui/feedback.go:201`），也就是 `t.Surface` 的常驻底 + 一圈
// `t.Border`——**带壳**。换成自绘之后它变成了 `flatButton`，那一层底和那一圈
// 描边就都没了（票 #228）。
//
// # 🔴 它和确认键的层级**不是靠颜色分的**
//
// 确认键那一档是危险红 / 警告橙 / 普通主色（上游的 `color={type}`），而**取消键
// 不在那三档里**——把它也涂成主色，两颗键就分不出主次了（票 #228 明确点名
// 「别把它也涂成主色」）。它靠的是「壳」：一颗带壳的中性键摆在一颗实心主键旁边。
//
// ⚠️ **别改回 `flatButton`**：`flatspread_test.go` 的
// `TestTheDialogButtonsStayOutlined` 会红（它量的是「字上面那几行和背景是不是
// 同一个颜色」）。仓库里别处那些「取消」（`covermodal.go` / `folderedit.go` /
// `folderselect.go` / `login.go`）用的都是 `ui.Button`——这一处跟它们一样。
func cancelButton(c *ui.Context, spec confirmSpec) ui.Element {
	return ui.Button(c, spec.cancel)
}

// confirmIconColor 是警告图标的颜色。
//
// ⚠️ **primary 那一档是文字色**：上游的 `colorMap` 里没有 "primary" 这一项
// （`index.tsx:11-14` 的类型就写着 `Exclude<ConfirmModalType, "primary">`），
// `colorMap[type]` 是 undefined，图标于是继承文字色。这里照它。
func confirmIconColor(t *ui.Theme, tone confirmTone) ui.Color {
	switch tone {
	case toneWarning:
		return t.Warning
	case toneDanger:
		return t.Danger
	}
	return t.Text
}
