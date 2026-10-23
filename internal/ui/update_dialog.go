// 更新日志弹窗：Biu 的 `src/components/release-note-modal/index.tsx`，用
// MyGo 原生控件重画一遍。
//
// ## 上游那一版的形状，一处不差地抄
//
// 标题「✨ 有新版本更新」→ 更新日志（Markdown，渲染成富文本）→ 进度条 → 底部一行
// 左边是速度 / 错误，右边是「已传/总量」和那个按钮。四个状态各有各的样子：
//
// | 状态      | 底部左边               | 底部右边                       |
// |-----------|------------------------|--------------------------------|
// | 有更新    | （空）                 | 「下载更新」                   |
// | 下载中    | 速度                   | 已传/总量 + 「正在下载」+ 百分比 |
// | 下完了    | 「安装包已下载完成」   | 「退出并安装更新」             |
// | 出错了    | 错误（红的）           | 「重试下载」/「知道了」        |
//
// 百分比**两位小数**（`percent.toFixed(2)`）和速度（`filesize(bytesPerSecond)/s`）
// 都是 Biu 那一版就有的，不是我们自己加的装饰——照抄，见 PLAN 的 C-45。
//
// ## 🔴 铁律二：本文件一次都不调 `c.SetTheme`
//
// 主题每帧开头由 `viewMain` 装好（theme.go）。放到某个回调里只调一次会被框架
// **静默**丢掉——不报错、不崩溃、不打日志，就是主题悄悄跳回系统色。
//
// ## 下到一半**不给关**
//
// `ui.Modal` 把「点背景 / 按 Esc 关掉」做好了，而那在下到一半的时候是不能要的：
// 这一关用户以为「取消了」，其实几十 MB 还在后台往磁盘上写。Biu 那个弹窗也是
// 这么写的（`isKeyboardDismissDisabled` / `isDismissable={false}`）。本文件
// 把这一处的处理写成了「关掉的那一帧要一帧把它画回来」，见 viewUpdateDialog。

package ui

import (
	"strconv"
	"strings"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// 弹窗上的字。**逐条照 Biu**（`release-note-modal/index.tsx`），一个字都没改。
const (
	// updateDialogTitle 是那个标题。Biu 头上那个 ✨ 也留着。
	updateDialogTitle = "✨ 有新版本更新"
	// updateNoNotes 是更新源没给日志时的那一句。Biu 是居中的灰字。
	updateNoNotes = "暂无更新日志"
	// updateBtnDownload / updateBtnBusy 是同一个按钮的两句话。
	updateBtnDownload = "下载更新"
	updateBtnBusy     = "正在下载"
	// updateBtnRetry 是装失败了之后那个按钮。Biu 那边的按钮没换字（还是
	// 「下载更新」），本项目换一句：用户刚看过一句红字，按钮上还是「下载更新」
	// 的话他会以为刚才那次其实没失败。
	updateBtnRetry = "重试下载"
	// updateBtnOK 是**查**失败了（还不知道有没有更新）时那个按钮。
	//
	// ⚠️ **这一支今天到不了，它是兜底**：`updateOpen` 只在相位
	// Available/Downloading/Ready/Failed 时开弹窗，而后三档 `Latest` 必然非空
	// （`StartUpdateDownload` 要求它非空；`check()` 只在「已是最新」那一支清空
	// `Latest`，而那一支同时把相位设回 `Idle`）。所以走到 `UpdateFailed` 时
	// `Available()` 恒真，返回的一直是「重试下载」。
	//
	// 留着它是**为了以后**：哪一天查失败也走 `UpdateFailed`（比如说把
	// 「查失败」和「装失败」拆成两个相位，但只留一个弹窗状态），这一支就是那个
	// 出口，没有它用户会卡在一个关不掉的弹窗里。
	//
	// ⚠️ **它没有测试覆盖，也不要当成覆盖到了**：任何一条测试里点不到它，所以
	// 把它写进台账或者报告里的「验收项 → 测试」那张表都是撒谎。
	updateBtnOK = "知道了"
	// updateBtnInstall 是下完之后那一个，也是这张票的验收词之一。
	updateBtnInstall = "退出并安装更新"
	// updateReadyText 是下完之后左边那句话。Biu 那边配了一个 ℹ 图标。
	updateReadyText = "安装包已下载完成"

	// updateFellBack 是增量没能用上、改下整个安装包时说的那句话。
	//
	// ⚠️ 这一句是**故意说给用户听的**，不是日志。原因见
	// `internal/app/update.go` 文件头 §增量：回退时进度条会在用户眼前倒退、
	// 百分比会变小、速度会归零再跳起来。不说话的话那三件事各自看起来都像
	// bug，而用户真正该知道的是「没事，只是这次下的是整个包」。
	updateFellBack = "增量更新没拼出来，已改为下载完整安装包"

	// updateBusyNote 是下完了之后那句「已经装到磁盘上了」。不说的话用户会以为
	// 还得再点什么，而「退出并安装」四个字已经说清了会发生什么。
	updateReadyNote = "新版本已经装好，退出后重新打开就是它"

	// updateBtnView / updateBtnCheck 是设置页上那个按钮的两个名字。Biu 的
	// `<AsyncButton>` 就是这两支：`isUpdateAvailable ? "查看更新内容" : "检查更新"`
	//（`components/update-check-button/index.tsx`）。
	//
	// ⚠️ 它和上面那个「下载更新」**不是同一个词**：「下载更新」是弹窗里那个
	// 按钮（Biu 的 `ReleaseNoteModal` 里 `status === "downloading" ? "正在下载" :
	// "下载更新"`），「检查更新」是设置页上那个（`UpdateCheckButton`）。两个都叫
	// 「更新」但一个是查、一个是下——混成一个的话，用户在设置页上按下的是
	// 「下载更新」，而那一刻他连有没有更新都还不知道。
	updateBtnView  = "查看更新内容"
	updateBtnCheck = "检查更新"
	// updateCheckingLabel 是正在查的那一档。它和 Biu 的 `AsyncButton` 的
	// `isLoading` 一个意思。
	updateCheckingLabel = "正在检查…"
	// updateEntryBusy 是设置页上那个按钮在下载中的样子。
	//
	// ⚠️ 写「正在下载」而不是弹窗那个「% 正在下载」：设置页那一格在弹窗底下，
	// 用户看不见百分比（真的看得见——弹窗关掉的时候），而那一格上写一个带
	// 百分比的按钮会让人以为**两个窗口**都在报进度。
	updateEntryBusy = "正在下载…"
)

// updateDialogWidth 是弹窗面板多宽。
//
// Biu 是 `size="lg"`（HeroUI 的 lg = 512px）。512 在这个内容上正合适：更新日志
// 多半每行两三个短句，窄了到处折行，宽了空一大片。
const updateDialogWidth = 512

// updateNotesMaxH 是更新日志那一块最高多高，再长就自己滚。
//
// **必须有这个上限**：更新日志是发布者写的，长度不受我们控制。没有它，一条
// 几十行的日志会把按钮顶出面板（面板有 `MaxHeight`，于是按钮被挤掉或者滚出
// 可视区），而用户最先看到的按钮应该永远是那一个。
const updateNotesMaxH = 260

// viewUpdateDialog 画更新日志弹窗。没有可看的东西时它什么都不画。
//
// 它被调用的时候**已经**在内容之后了（settings_page.go 在最后调它），所以它
// 不需要管位置：`ui.Modal` 走的是框架的 overlay 层，画在哪一层都会被抬到最上面。
func (s *Shell) viewUpdateDialog(c *ui.Context, st app.UpdateState) {
	if !s.updateOpen(st) {
		return
	}
	// 外面包一层 Guard（铁律三）：这一段里有一个第三方渲染器（markdown.View）在
	// 解析**发布者写的**内容，那是最容易崩的一块；而它崩掉的表现会是整个设置
	// 页半截不画（弹窗是它的最后一段）。
	s.guardAt("更新弹窗", func(cc *ui.Context) { s.drawUpdateDialog(cc, st) })(c)
}

// updateOpen 报弹窗这一刻该不该开着。
//
// 只认四档：查到了、在下、下完了、失败了。**查的那一档不开**——用户按了
// 「检查更新」，按钮当场变成「正在检查…」给的是反馈，而一个为了「正在检查」
// 开起来的空窗只会让人以为按错了。
//
// `wanted` 是**这个窗口**的用户意图：后台自动检查查到新版本时不弹窗（只弹一条
// Toast），因为一个音乐播放器开着窗突然被更新窗盖住是最招人烦的一种做法。
func (s *Shell) updateOpen(st app.UpdateState) bool {
	switch st.Phase {
	case app.UpdateAvailable, app.UpdateDownloading, app.UpdateReady, app.UpdateFailed:
		return s.update.wanted
	}
	return false
}

// drawUpdateDialog 画弹窗本身。
func (s *Shell) drawUpdateDialog(c *ui.Context, st app.UpdateState) {
	t := c.Theme()

	// `open` 是这一帧的副本：`ui.Modal` 会把被关掉的那一份就地置 false，而
	// 「下到一半不许关」要在**下一帧**把它画回来——用副本就是靠这个：这一帧
	// 那个 false 只活到这一帧结束，s.update.wanted 仍然是 true。
	open := s.update.wanted
	m := ui.Modal(c, &open, func() {
		// 和 confirmDialog 同一个摆法（`ui.Modal` 自己已经给面板上了内边距、
		// 圆角和背景，里面再套一层 Column 当**内容**没问题，**按钮那一行必须是
		// 面板的直接子元素**——套进 Column 的话按钮的命中区域和面板对不上，
		// 症状是「看得见、点不动」）。
		ui.Column(c).Key("dialog:update").Width(updateDialogWidth).Gap(dialogGap).
			Children(func() {
				ui.Text(c, updateDialogTitle).FontSize(scale.Font(c, 16)).Bold()
				s.updateVersionLine(c, st)
				s.updateNotes(c, st)
				if st.FellBack {
					ui.Text(c, updateFellBack).FontSize(scale.Font(c, 12)).TextColor(t.TextMuted).Wrap()
				}
				if st.Phase == app.UpdateDownloading && st.Total > 0 {
					ui.Progress(c.Key("update-bar"), st.Percent/100).FillWidth()
				}
			})
		s.updateFooter(c, st)
	})

	if !m.Dismissed() {
		return
	}
	if st.Phase == app.UpdateDownloading {
		// 下到一半不给关。这一帧它已经被关掉了，所以**再要一帧**把它画回来——
		// MyGo 的 build 下一遍是从头重画的（`guard.go` 里同一件事的说明）。
		s.update.wanted = true
		c.Invalidate()
		return
	}
	s.update.wanted = false
	s.app.DismissUpdate()
}

// updateVersionLine 是标题下面那一行。画法和设置页上那一行是同一份
// （`versionLine`），只有字号不同。
func (s *Shell) updateVersionLine(c *ui.Context, st app.UpdateState) {
	s.versionLine(c, "update-versions", 13, 700, st)
}

// updateNotes 画更新日志。空的那一档是居中的灰字，照 Biu。
//
// ⚠️ 换一版日志要回到顶上：滚动位置记在 `s.update.scroll` 上，而版本一变，那一
// 个偏移量指的是**上一版日志的**第几行——留着的表现是用户一打开新版本的日志就
// 在中间某一行，上下文（标题）早滚过去了。
func (s *Shell) updateNotes(c *ui.Context, st app.UpdateState) {
	// ⚠️ 判的是 `TrimSpace` 而不是 `== ""`：更新源给一份「换行 + 空格」是有
	// 的（Biu 那边写的就是 `releaseNotes?.trim()`，见 `release-note-modal`
	// 里那个三元）。不 trim 的话那一份会渲染成一个空白的滚动区，用户看见的是
	// 一块什么都没有的框，而不是「暂无更新日志」这一句能回答他的话。
	if strings.TrimSpace(st.Notes) == "" {
		ui.Text(c, updateNoNotes).FontSize(scale.Font(c, 13)).TextColor(c.Theme().TextMuted).
			TextAlign(ui.Center).FillWidth().Padding(0, 0, 8, 0)
		return
	}
	if s.update.shownNotes != st.Latest {
		s.update.shownNotes, s.update.scroll.Y = st.Latest, 0
	}
	if s.update.notes == nil {
		// Theme 留 nil：由 markdown 包按窗口的主题取（`DefaultTheme`），而主题
		// 这一版换掉是每帧开头的事（铁律二），传一份进来反而会把它冻住。
		//
		// OnLink 也留 nil：默认那一支就是「http / https / mailto 的链接用浏览器
		// 打开」，正是更新日志要的（发布者写的是 GitHub 上那个 issue 的链接）。
		s.update.notes = &markdown.Options{}
	}
	notes := ui.Scroll(c).Key("update-notes").FillWidth().MaxHeight(updateNotesMaxH).
		TrackScroll(&s.update.scroll).
		Children(func() { markdown.View(c, st.Notes, s.update.notes) })
	s.trackScrollbar(notes)
}

// updateFooter 是底部那一行：左边速度 / 错误 / 完成，右边大小和按钮。
//
// Biu 的 `ModalFooter` 是 `items-center justify-between`，左右各一块，中间空出来。
func (s *Shell) updateFooter(c *ui.Context, st app.UpdateState) {
	t := c.Theme()
	label, onClick, busy := s.updateButton(st)

	ui.Row(c).Key("update-foot").FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			switch {
			case st.Phase == app.UpdateFailed && st.Err != "":
				// 出错了就把原因说全（框架那一句可能很长），并让它可以选中复制：
				// 用户要拿这句话去提 issue。
				ui.Text(c, st.Err).FontSize(scale.Font(c, 12)).TextColor(dangerText(t)).Wrap().Selectable()
			case st.Phase == app.UpdateReady:
				ui.Text(c, updateReadyText).FontSize(scale.Font(c, 12)).TextColor(t.TextMuted)
				ui.Text(c, updateReadyNote).FontSize(scale.Font(c, 11)).TextColor(t.TextMuted).Wrap()
			case st.Phase == app.UpdateDownloading:
				ui.Text(c, updateSpeed(st.Speed)).FontSize(scale.Font(c, 12)).TextColor(t.TextMuted)
			}
		})
		if st.Phase == app.UpdateDownloading {
			ui.Text(c, updateSizes(st.Transferred, st.Total)).
				FontSize(scale.Font(c, 12)).TextColor(t.TextMuted)
		}
		// ⚠️ 按钮这一行是**面板的直接子元素**（在上面那个 Column 外面），
		// 见 drawUpdateDialog 的注释：套进 Column 的话点不动。
		if ui.PrimaryButton(c, label).Disabled(busy).Clicked() && onClick != nil {
			onClick()
		}
	})
}

// updateButton 是底部那个按钮这一帧的三样东西：上面写什么、按下去做什么、
// 这一刻是不是关着的。
//
// 「这一刻关着」只有下载中那一种：Biu 那边是 `isLoading={status === "downloading"}`，
// 同一个意思——下到一半那个按钮按下去是错的，所以干脆关掉。
func (s *Shell) updateButton(st app.UpdateState) (label string, onClick func(), busy bool) {
	switch st.Phase {
	case app.UpdateReady:
		return updateBtnInstall, s.app.QuitAndInstall, false
	case app.UpdateDownloading:
		return updateBusyLabel(st), nil, true
	case app.UpdateFailed:
		if st.Available() {
			return updateBtnRetry, s.app.StartUpdateDownload, false
		}
		return updateBtnOK, s.closeUpdateDialog, false
	default:
		return updateBtnDownload, s.app.StartUpdateDownload, false
	}
}

// updateBusyLabel 是下载中那个按钮上的字：`18.21% 正在下载`。
//
// 百分比放在前面是照 Biu 的 `startContent`（`{percent.toFixed(2)}%` 在 label
// 前面）——它把「下到哪儿了」放在这一格里，而下面那一行左边是速度、右边是
// 已传/总量。
//
// ⚠️ **总是画**，连 0.00% 也画。Biu 那一支是 `Boolean(downloadProgress?.percent)`
// （0 是假值，所以 0% 时不画）。这里不跟：那一帧进度条是空的、速度是 0 B/s，
// 而验收要的就是百分比本身始终在画面上；写一个 0.00% 不是撒谎，它是此刻真实的
// 进度，而**不写**才是——0 那个数在「刚点下」和「卡住了」两种情况下长得一模一样。
func updateBusyLabel(st app.UpdateState) string {
	if st.Total <= 0 {
		return updateBtnBusy
	}
	return strconv.FormatFloat(st.Percent, 'f', 2, 64) + "% " + updateBtnBusy
}

// closeUpdateDialog 只关弹窗，不动更新器那一摊。
func (s *Shell) closeUpdateDialog() { s.update.wanted = false }

// updateSpeed 把速度写成 `1.2 MB/s`。
//
// 0 写成 `0 B/s` 而不是 `-/s`：此刻刚点下、或网络还没吐出第一个字节，速度就是
// 0，而「-」在这个包里一直表示「还不知道」（`fileSize` 里那样用的）。同一个
// 字在同一个界面上表示两件事，比偶尔难看一点更难解释。
func updateSpeed(bytesPerSecond int64) string {
	if bytesPerSecond <= 0 {
		return "0 B/s"
	}
	return fileSize(bytesPerSecond) + "/s"
}

// updateSizes 写成 `12.4 MB/68.0 MB`。
//
// 还不知道总量时是 `-/-`，照 Biu（`transferred ? filesize(transferred) : "-"`）。
// 用「-」而不是「0 B」是因为它说的确实是另一件事：不是「一个 0 字节的文件」，
// 是「还不知道」。
func updateSizes(transferred, total int64) string {
	return fileSize(transferred) + "/" + fileSize(total)
}
