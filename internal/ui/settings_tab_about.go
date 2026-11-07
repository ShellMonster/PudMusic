// 设置页的「关于」Tab：原型 `SETTINGS.about`——唱片标志 + 名称 + 版本说明 + 检查更新 / 查看更新内容，
// 下面一张卡是「开源许可」「诊断信息」。原来从菜单打开的「关于」弹窗收进了这一页
// （`openAbout` 现在是跳到这个 Tab）。
package ui

import (
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strings"

	"github.com/ShellMonster/PudMusic/internal/licensing"
	"github.com/egoist/mygo"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/leading"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/typo"
	"github.com/egoist/mygo/ui"
)

const (
	aboutNoNotices = "找不到随包发出去的许可清单"
	repoURL        = "https://github.com/ShellMonster/PudMusic"
	aboutMarkSize  = 72
)

// settingsAbout 是关于那一页。
func (s *Shell) settingsAbout(c *ui.Context, st app.Settings, ro bool) {
	p := tok.Of(c.Theme())
	us := s.app.UpdateSnapshot()

	s.setGroup(c, "about", "", func() {
		// `.set-card` 内边距 32、缝 24：标志 72 + 文字块 + 按钮。
		s.card(c, "about", ro, func(cd *setCard) {
			ui.Row(c).Key("about-head").FillWidth().Padding(space.Px32).Gap(space.Px24).AlignItems(ui.Center).Children(func() {
				aboutRecordMark(c, p, aboutMarkSize)
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
					typo.Display(ui.Text(c, appName+" PudMusic"), scale.Font(c, scale.Subhead)).LineHeight(leading.Body)
					ui.Text(c, "为 macOS 与 Windows 打造的原生桌面播放器 · Go + MyGo").
						FontSize(scale.Font(c, scale.Size14)).TextColor(p.Text2).Wrap().
						Margin(space.Px4, space.None, space.None, space.None)
					typo.Mono(ui.Text(c, s.aboutVersionLine(us))).FontSize(scale.Font(c, scale.Size12)).LineHeight(leading.Body).
						TextColor(p.Text3).Wrap().Margin(space.Px4, space.None, space.None, space.None)
				})
				s.aboutUpdateButton(c, us)
			})
		})
	})
	s.setGroup(c, "about-links", "", func() {
		s.card(c, "about-links", ro, func(cd *setCard) {
			cd.row("license", "开源许可", "基于 Biu 重新实现与扩展", func() {
				if pud.Button(c, "set-license", pud.Quiet, pud.Sm, "查看", "").Clicked() {
					s.openNotices(c)
				}
			})
			cd.row("diag", "诊断信息", "遇到问题时附在反馈里", func() {
				if pud.Button(c, "set-diag", pud.Quiet, pud.Sm, "复制", "").Clicked() {
					if err := s.copyText(s.diagnosticText(us)); err != nil {
						s.addToast("复制失败："+err.Error(), notice.Danger)
						return
					}
					s.addToast("已复制诊断信息", notice.Success)
				}
			})
		})
	})
	if s.app.UpdateMounted() {
		s.viewUpdateDialog(c, us)
	}
}

// aboutVersionLine 是名称底下那行等宽小字：当前版本 + 更新状态（`updateHint` 的话照搬）。
func (s *Shell) aboutVersionLine(us app.UpdateState) string {
	v := "当前版本 " + app.VersionLabel(us.Current)
	if us.Available() {
		v += " → " + us.Latest
	}
	if !s.app.UpdateMounted() {
		return v
	}
	return v + " · " + s.updateHint(us)
}

// aboutUpdateButton 是右边那枚按钮：没有更新是「检查更新」（刷新图标），有更新是「查看更新内容」
// （下载图标）；在查 / 在下的时候灰着，字换成「正在检查…」。
func (s *Shell) aboutUpdateButton(c *ui.Context, us app.UpdateState) {
	if !s.app.UpdateMounted() {
		return
	}
	label, onClick, busy := s.updateEntryButton(us)
	icon := "refresh"
	if us.Available() {
		icon = "download"
	}
	if busy {
		// 正在查 / 正在下：幽灵键的样子 + 转圈 + 字（字不能拿掉，否则按钮缩成小方块，宽度会跳）。
		p := tok.Of(c.Theme())
		base := ui.ButtonBase(c).Key("set-update").Height(tok.BtnH).Radius(tok.BtnH/2).
			PaddingX(space.Px20).AlignItems(ui.Center).Justify(ui.Center).Border(1, p.LineStrong).Shrink(0)
		busyButton(c, base, label, p.Text)
		return
	}
	b := pud.ButtonDisabled(c, "set-update", pud.Ghost, pud.Std, label, icon, onClick == nil)
	if onClick != nil && b.Clicked() {
		onClick()
	}
}

// diagnosticText 是「复制诊断信息」复制的那段话：只有版本、系统、更新状态和几项非敏感的设置，
// 不含 Cookie、账号、路径之类的东西。
func (s *Shell) diagnosticText(us app.UpdateState) string {
	st := s.app.SettingsSnapshot()
	var b strings.Builder
	fmt.Fprintf(&b, "PudMusic %s\n", app.VersionLabel(us.Current))
	fmt.Fprintf(&b, "系统：%s/%s，Go %s\n", runtime.GOOS, runtime.GOARCH, runtime.Version())
	fmt.Fprintf(&b, "登录：%v\n", s.app.LoggedIn())
	fmt.Fprintf(&b, "音质偏好：%s，视频画质：%d\n", st.AudioQuality, st.VideoQuality)
	fmt.Fprintf(&b, "主题：%s / %s，封面取色：%v，字号：%s\n", st.ThemeMode, st.Accent, st.CoverAccent, st.FontScale)
	fmt.Fprintf(&b, "代理：%s\n", st.ProxySettings.Type)
	if us.Err != "" {
		fmt.Fprintf(&b, "更新：%s\n", us.Err)
	}
	return b.String()
}

// aboutRecordMark 画唱片标志（原型 `brandMark()`，32×32 的 viewBox 缩放到 size）：
// 正文色大圆、两道 35% bg 的细圈（r 12 / 9.5）、主色圆（r 6）、正文色小圆心（r 1.6）。
func aboutRecordMark(c *ui.Context, p tok.Palette, size float32) {
	ui.Box(c).Key("about-mark").Width(size).Height(size).Shrink(0).
		Draw(func(pp *ui.Painter, r ui.Rect) {
			k := size / 32
			cx, cy := r.X+size/2, r.Y+size/2
			disc := func(radius float32, col ui.Color) {
				rr := radius * k
				pp.Fill(ui.Rect{X: cx - rr, Y: cy - rr, W: 2 * rr, H: 2 * rr}, col, rr)
			}
			ring := p.Text.Mix(p.Bg, 0.35)
			disc(15, p.Text)
			disc(12.5, ring)
			disc(11.5, p.Text)
			disc(10, ring)
			disc(9, p.Text)
			disc(6, p.Accent)
			disc(1.6, p.Text)
		})
}

// openNotices 把随包发出去的那份第三方许可清单交给系统打开。找不到就说找不到。
//
// ⚠️ 用 `c.OpenURL` 而不是 `mygo.Shell.OpenPath`：后者要事件循环，无头 Tester 里调它直接 panic。
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

// noticesFile 是许可清单在哪（包级变量：测试能换掉）。
var noticesFile = defaultNoticesFile

func defaultNoticesFile() string {
	dir, err := mygo.App.Path(mygo.PathResources)
	if err != nil {
		return ""
	}
	return licensing.RuntimeNoticesPath(dir)
}

// fileURL 把一个本地路径变成 file:// 地址（路径里有空格时不能直接拼）。
func fileURL(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}

// openAbout 是应用菜单里的「关于」：跳到设置页的「关于」Tab。
//
// ⚠️ 它必须把主窗口叫回来：菜单是应用级的，而迷你播放器开着的时候主窗口是收起来的
// （两个窗口互斥）；先把迷你关掉，主窗口才有地方画。
func (s *Shell) openAbout() {
	s.dropMini()
	s.goToSettingsTab(settingsTabAbout)
	s.restoreMainWindow()
	s.wake.invalidate()
}

// openSettingsFromMenu 是应用菜单里「设置…」那一项：同样要让开迷你，因为路由表是主窗口那一份。
func (s *Shell) openSettingsFromMenu() {
	s.dropMini()
	s.navigate(settingsPath)
	s.restoreMainWindow()
}
