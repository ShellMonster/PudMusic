// 关于面板：应用菜单里「关于 布丁音乐」点开的那一块。
//
// ## 为什么是自绘的这一块，不是系统那个关于面板
//
// 框架有 `mygo.App.ShowAboutPanel`（macOS 的 NSAboutPanel），但它**带不了链接**：
// 那一栏只有应用名、版本、版权和一段纯文本的说明——仓库链接点不了，第三方
// 许可也没有入口。而这两样恰恰是「关于」在 macOS 上被点开的主要理由。所以
// 这一块自己画，走仓库自己的弹窗族（`dialogPanel`）。
//
// ## 为什么不是设置页末尾那一段「关于应用」
//
// 那一段说的是**能不能自动更新**（当前版本 / 检查更新 / 更新日志），不是
// 「这个应用是谁」；它是设置页里的一行，要往下滚才看得见。而应用菜单是
// **应用级**的——用户在任何一页都能点「关于」，那一段给不了他一个立刻能
// 看见的面板。两者并存：设置页那一段管更新，这一块管「关于」。
//
// ## 版本号和设置页那一段**同一个来源**
//
// 这里调的是**同一个函数**（`s.versionLine`，在 `update_entry.go`）、
// 拿的是**同一份快照**（`s.app.UpdateSnapshot()`）。两处各写一个版本号的
// 話，改了打包配置而忘了改另一处，用户在面板上看到的还是上一个版本。
//
// ## 第三方许可为什么是「打开随包发出去的那一份」
//
// `resources/licenses/THIRD-PARTY-NOTICES.txt` 是**随包发出去**的那份清单
// （`internal/licensing` 生成的）：它列全了每一个编进可执行文件的依赖、
// 每一条许可证原文的名字，还有 Biu 本体那一条 Required Notice
// （PolyForm 的 Notices 一节点名要逐字保留的）。
//
// ⚠️ **不在界面里重排一份**：那份清单里还有几样**不是 Go 模块**的东西
// （图标、vendor 进来的 MP3 解码器、go-image 的改版），它们只在清单的
// 说明段里写着——自己重排一遍的话，那几样就漏了，而漏掉的是**法定义务**
// （理由见 `internal/licensing/licensing.go` 顶上那一节）。
package ui

import (
	"net/url"
	"os"

	"github.com/ShellMonster/Biu_Go/internal/licensing"
	"github.com/ShellMonster/Biu_Go/internal/notice"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/ink"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// 关于面板上的字。
const (
	// aboutTitle 是面板的名字（读屏软件报出来的那一句）。
	aboutTitle = "关于 " + appName
	// aboutPanelWidth 是面板多宽。
	//
	// 360：确认框那一族是 300~420（`confirmMinW` / `confirmMaxW`），这里取
	// 中间那一档——下面那两行（仓库地址、许可清单说明）都要放得下，又不至于
	// 像更新日志那个 512 那样空一大片。
	aboutPanelWidth = 360
	// aboutRepoLabel / aboutRepoText 是「代码仓库」那一行：左边是名字，
	// 下面是那个能点的地址。
	aboutRepoLabel = "代码仓库"
	aboutRepoText  = "github.com/ShellMonster/Biu_Go"
	// aboutLicenseLabel / aboutLicenseText 是「第三方许可」那一行。
	aboutLicenseLabel = "第三方许可"
	aboutLicenseText  = "打开随包发出的许可清单"
	// aboutOK 是底下那颗按钮上的字。
	aboutOK = "好"
	// aboutNoNotices 是找不到那份清单时说的话。
	//
	// ⚠️ **要说清楚是「找不到」而不是「没有」**：用户点的是「第三方许可」，
	// 而这一句是他唯一能知道结果的提示——「这一版没有随包发出去许可清单」
	// 和「这一版没找到随包发出去的那份」是两件事，而后者是这一版自己出了
	// 问题（构建时漏了资源）。
	aboutNoNotices = "找不到随包发出去的许可清单"
)

// repoURL 是这个项目的仓库地址。
//
// ⚠️ **它和 `feedbackURL`（`navbar_avatar.go`）是同一个仓库的两个入口**：
// 那一个去 issue 区，这一个去代码。改仓库地址要两处一起改。
const repoURL = "https://github.com/ShellMonster/Biu_Go"

// noticesFile 报告随包发出去的那份第三方许可清单在哪儿。
//
// 做成变量而不是每次现算：资源目录要问框架（`mygo.App.Path`），而**无头
// Tester 里那个目录不是真的包里的那一个**（测试进程的 CWD 是包目录）——
// 测试要把它指到临时目录上，才能验「点一下真的会打开那份清单」。
//
// 理由和 `coverCacheDir` 那个接缝一样（见 shelltest_test.go 顶上那一段）。
var noticesFile = defaultNoticesFile

// defaultNoticesFile 是真的那一份：随包资源目录下的 licenses/THIRD-PARTY-NOTICES.txt。
func defaultNoticesFile() string {
	dir, err := mygo.App.Path(mygo.PathResources)
	if err != nil {
		return ""
	}
	return licensing.RuntimeNoticesPath(dir)
}

// openAbout 打开关于面板。
//
// ⚠️ **它必须把主窗口叫回来**：菜单是**应用级**的（`SetMenu` 设在 `mygo.App`
// 上），而迷你播放器开着的时候主窗口是收起来的（两个窗口互斥，见
// miniplayer.go）。面板画在主窗口上，主窗口不在的时候用户什么也看不见——
// 症状是「点了关于，什么都没发生」。
//
// ⚠️ **迷你那一步排在最前面**：面板开在主窗口上，而迷你那一扇和主窗口互斥，
// 先把迷你关掉，主窗口才有地方画它。
func (s *Shell) openAbout() {
	s.about.wanted = true
	s.dropMini()
	s.restoreMainWindow()
	s.wake.invalidate()
}

// openSettingsFromMenu 是应用菜单里「设置…」那一项。
//
// ⚠️ **和 openAbout 一样要让开迷你**：路由表是**主窗口**那一份（`navigate`
// 改的是 `s.router`），迷你那一扇有自己的外壳和自己的路由。在迷你开着的时候
// 换路由，主窗口那一页是换了，而用户眼前还是迷你——于是「点了设置，什么都
// 没发生」。
func (s *Shell) openSettingsFromMenu() {
	s.dropMini()
	s.navigate(settingsPath)
	s.restoreMainWindow()
}

// viewAbout 画关于面板。没开的时候它什么都不画。
//
// ⚠️ 外面包一层 Guard（铁律三）：这一段是模态，崩了的话整个窗口的界面就
// 停在半截上——而它画的东西是**从菜单进来的**，用户没点任何界面上的东西，
// 出了错他连「是哪一块」都说不出来。
func (s *Shell) viewAbout(c *ui.Context) {
	if !s.about.wanted {
		return
	}
	s.guardAt("关于面板", s.drawAbout)(c)
}

// drawAbout 画关于面板本身。
func (s *Shell) drawAbout(c *ui.Context) {
	// 这一帧的更新状态：设置页「关于应用」那一段读的是**同一份快照**
	// （`s.app.UpdateSnapshot()`），所以两处显示的版本号永远一致。
	st := s.app.UpdateSnapshot()

	// `open` 是这一帧的副本：`ui.DialogBase` 会把被关掉的那一份就地置 false，
	// 而「关掉了」和「用户按了『好』」都要把 `s.about.wanted` 收掉——用副本
	// 就是靠这个（和 `update_dialog.go` 那个弹窗同一个办法）。
	open := s.about.wanted
	// 🔴 **身份钉死**（`c.Key`）：框架给元素的身份是「父的身份 + 兄弟序号」，
	// 而**浮层里的东西会多会少**——于是面板里那两枚链接的身份会跟着变，
	// 而按下和抬起之间隔着帧：按下记在旧身份上，抬起之后那一枚已经是新
	// 身份——**那一下点空**（链接点了没反应）。
	//
	// ⚠️ **给的是那个「底幕」**：`ui.DialogBase` 先建底幕、再把面板建在底幕
	// **里面**，而 `Context.Key` 是给**下一个**建出来的元素用的——所以键落在
	// 底幕上，而面板的身份是「底幕的身份 + 兄弟序号」，底幕的身份钉死了它
	// 也就钉死了。理由见 `confirmmodal.go` 那一段（同一个道理）。
	c.Key("about-backdrop")
	m := ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		dialogPanel(c, backdrop, panel, aboutTitle)
		// 和更新弹窗同一个摆法（`ui.Modal` 自己已经给面板上了内边距、圆角和
		// 背景，里面再套一层 Column 当**内容**没问题，**按钮那一行必须是
		// 面板的直接子元素**——套进 Column 的话按钮的命中区域和面板对不上，
		// 症状是「看得见、点不动」）。
		ui.Column(c).Key("about-panel").Width(aboutPanelWidth).Gap(dialogGap).
			Children(func() {
				// ⚠️ **字重不写**（400）：弹窗标题是 16/400（DESIGN.md §二），
				// 层次靠字号拉开。
				ui.Text(c, appName).FontSize(scale.Font(c, scale.Size16))
				// 版本号和设置页「关于应用」那一段**同一个来源、同一份画法**
				// （`versionLine`）：有更新的时候它连新版本号一起写上。
				s.versionLine(c, "about-version", 13, 400, st)
				ui.Divider(c).FillWidth()
				s.aboutEntry(c, aboutRepoLabel, aboutRepoText, func() { c.OpenURL(repoURL) })
				s.aboutEntry(c, aboutLicenseLabel, aboutLicenseText, func() { s.openNotices(c) })
			})
		// 按钮行。⚠️ **它是面板的直接子元素**（不在上面那个 Column 里），
		// 理由同 drawUpdateDialog 那一句。
		ui.Row(c).AlignSelf(ui.End).Children(func() {
			if ui.PrimaryButton(c, aboutOK).Clicked() {
				s.about.wanted = false
			}
		})
	})

	// 点外面 / 按 Esc：`ui.DialogBase` 已经把 `*open` 关掉了，那一次**不算**
	// 「按了『好』」，但面板得收掉。
	if m.Dismissed() {
		s.about.wanted = false
	}
}

// aboutEntry 是关于面板里的一行：上面一个名字，下面那个能点的东西。
//
// ⚠️ **名字和能点的那一句分成两行**，不是并排：并排的话那个地址（33 个字符）
// 挤在名字右边，面板得宽到 480 才放得下——而这一块不需要那么宽。
//
// ⚠️ **名字是灰的 13、那一句是正文号的 14**：能点的那一句才是这一行里
// 真正有用的东西，它得比说明更像正题（反过来的话——名字大、链接小——用户
// 第一眼看到的是「代码仓库」四个字，而那四个字点不动）。
func (s *Shell) aboutEntry(c *ui.Context, label, text string, run func()) {
	t := c.Theme()
	card := dialogCard(t)
	ui.Column(c).FillWidth().Gap(space.Px4).Children(func() {
		ui.Text(c, label).FontSize(scale.Font(c, scale.Size13)).TextColor(t.TextMuted)
		// ⚠️ **主色当字色**：这是一个能点的东西，得**一眼看得出点得动**。
		// 压不住的那个色按面板自己的底算（`AccentOnBase`），不是按页面底
		// （`AccentOn`）——面板的底是 `surface1`，和页面底不是同一个色。
		if flatLink(c, text).FontSize(scale.Font(c, scale.Size14)).
			TextColor(ink.AccentOnBase(t, card.bg, ink.TextContrast)).Clicked() {
			run()
		}
	})
}

// openNotices 把随包发出去的那份第三方许可清单交给系统打开。
//
// ⚠️ **用 `c.OpenURL` 而不是 `mygo.Shell.OpenPath`**：后者要事件循环
// （框架的 `needsApp`），无头 Tester 里调它直接 panic；而 `c.OpenURL` 在
// Tester 里把地址记下来（「点得动」这件事除了「这一项在」之外一句都验不到）。
// 两者在 macOS 上落到同一件事：NSWorkspace 用默认程序打开那个文件。
//
// ⚠️ **找不到就说找不到**：静悄悄地什么都不发生，用户会以为是这个应用坏了。
func (s *Shell) openNotices(c *ui.Context) {
	path := noticesFile()
	if path == "" {
		s.addToast(aboutNoNotices, notice.Warning)
		return
	}
	if _, err := os.Stat(path); err != nil {
		s.addToast(aboutNoNotices, notice.Warning)
		return
	}
	c.OpenURL(fileURL(path))
}

// fileURL 把一个本地路径变成 file:// 地址。
//
// ⚠️ **不能直接拼**：路径里有空格（用户数据目录就有）的话，NSURL 认不出来，
// 于是「点了没反应」。
func fileURL(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}
