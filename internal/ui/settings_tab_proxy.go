// 设置页的第四个 Tab：代理。照 Biu 的 `src/pages/settings/proxy-settings.tsx`。
//
// ## 四种协议，不是两种
//
// Biu 那一页的类型下拉有四档（`PROXY_TYPE_OPTIONS`，`:10-15`）：不使用 / HTTP /
// SOCKS4 / SOCKS5，值和 `app.ProxyType` 一字不差。Biu 自己没给 HTTPS 一档，我们
// 也不加——`app.ProxyType` 里没有它，而**不新造键值**是这一页的第一条规矩
// （见 `internal/app/settings.go` 的文件头）。HTTPS 走的是 HTTP 那一档：Go 的
// 代理拨号对 `http://` 和 `https://` 的处理本来就是同一条路。
//
// ## 密码只有三档才有
//
// 照 Biu 的 `showPassword = type === "http" || type === "socks5"`（`:39`），
// 外加新加的 HTTPS——三者都带用户名密码那一段。SOCKS4 和 SOCKS 协议里本来就没有
// 「用户名密码」这一段，填了也没有地方去。
package ui

import (
	"strconv"
	"strings"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/egoist/mygo/ui"
)

// proxyTypeOptions 是类型那一栏的五档，顺序就是下拉里从上到下的顺序。
//
// 前五档里的四种协议是任务明写的那四种：HTTP / HTTPS / SOCKS5 / SOCKS4；「不使用代理」
// 是关掉的那一档，不算协议。见 `app.ProxyType` 那里为什么比 Biu 多一个 HTTPS。
var proxyTypeOptions = []struct {
	value app.ProxyType
	label string
}{
	{app.ProxyNone, "不使用代理"},
	{app.ProxyHTTP, "HTTP 代理"},
	{app.ProxyHTTPS, "HTTPS 代理"},
	{app.ProxySOCKS4, "SOCKS4 代理"},
	{app.ProxySOCKS5, "SOCKS5 代理"},
}

// proxySelectWidth 是那几栏下拉多宽。Biu 是 `w-[240px]`（`:43`）。
const proxySelectWidth = 240

// settingsProxy 是代理那一页：一张「代理」分组卡片，每个字段一行（设计规范 §7.21）。
func (s *Shell) settingsProxy(c *ui.Context, st app.Settings, ro bool) {
	cur := st.ProxySettings
	s.settingGroup(c, "代理", func() {
		items := make([]enumItem[app.ProxyType], len(proxyTypeOptions))
		for i, o := range proxyTypeOptions {
			items[i] = enumItem[app.ProxyType]{value: o.value, label: o.label}
		}
		chosen := cur.Type
		s.settingRow(c, "代理类型", "", func() {
			enumSelect(c, &chosen, items, "选择代理类型", s.settingsControlFitW(c, proxySelectWidth), 0, ro)
		})
		if chosen != cur.Type {
			s.applySetting("代理类型", func(s *app.Settings) { s.ProxySettings.Type = chosen })
		}
		// 选了「不使用代理」就不画地址那几行——Biu 是 `(value.type === "http" || ...) && (...)`（`:61`）。
		if cur.Type == app.ProxyNone {
			proxyHint(c, "现在不走代理。选了上面那一档才填地址。")
			return
		}

		host := cur.Host
		port := portOf(cur.Port)
		user, pass := cur.Username, cur.Password
		showPass := cur.Type == app.ProxyHTTP || cur.Type == app.ProxyHTTPS || cur.Type == app.ProxySOCKS5
		// ⚠️ **不许给输入框加 Key**：Key 只能挂在「创建时没有处理输入」的
		// 控件上，挂上去直接 panic（`ui/element.go:658`）。
		s.settingRow(c, "主机", "", func() {
			ui.TextInput(c, &host).Width(s.settingsControlFitW(c, settingsControlW)).Label("代理主机").Placeholder("例如 127.0.0.1").Disabled(ro)
		})
		s.settingRow(c, "端口", "", func() {
			ui.TextInput(c, &port).Width(s.settingsControlFitW(c, settingsControlW)).Label("代理端口").Placeholder("例如 7890").Disabled(ro)
		})
		s.settingRow(c, "用户名", "", func() {
			ui.TextInput(c, &user).Width(s.settingsControlFitW(c, settingsControlW)).Label("代理用户名").Placeholder("").Disabled(ro)
		})
		if showPass {
			s.settingRow(c, "密码", "", func() {
				ui.TextInput(c, &pass).Width(s.settingsControlFitW(c, settingsControlW)).Label("代理密码").Placeholder("").Password().Disabled(ro)
			})
		}
		if ro {
			proxyHint(c, "设置只读，这一格改不了。")
		}
		// 端口写的是数字：不是数字就当成「没设」（`port` 在设置里是 `*int`，Biu 也是这么分的），
		// 于是输入框下一帧回到它最后一个合法的样子——和 Biu 一样（`:73-79`）。
		if host != cur.Host {
			s.applySetting("代理主机", func(s *app.Settings) { s.ProxySettings.Host = host })
		}
		if port != portOf(cur.Port) {
			s.applySetting("代理端口", func(s *app.Settings) { s.ProxySettings.Port = parsePort(port) })
		}
		if user != cur.Username {
			s.applySetting("代理用户名", func(s *app.Settings) { s.ProxySettings.Username = user })
		}
		if pass != cur.Password {
			s.applySetting("代理密码", func(s *app.Settings) { s.ProxySettings.Password = pass })
		}
	})
}

func proxyHint(c *ui.Context, text string) {
	ui.Text(c, text).FontSize(scale.Font(c, scale.Size12)).TextColor(c.Theme().TextMuted)
}

// portOf 把设置里那个可选的端口变成输入框上摆的字符串。
func portOf(p *int) string {
	if p == nil {
		return ""
	}
	return strconv.Itoa(*p)
}

// parsePort 把输入框上的字符串变成设置里那个可选的端口。
//
// 不是正整数就是「没设」。这一格最怕的是把 0 或者一段乱码存进去：0 端口在
// 「端口没填」和「端口填成 0」之间分不出区别，而 Biu 的 `port` 默认值是
// `undefined`——我们照它用 `*int`，就是为了这个区别能表达出来。
func parsePort(s string) *int {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || v <= 0 {
		return nil
	}
	return &v
}
