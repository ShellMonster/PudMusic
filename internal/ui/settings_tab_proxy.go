// 设置页的「代理」Tab：原型 `SETTINGS.proxy`。
//
// 三选一（不使用代理 / 使用系统代理 / 自定义代理，选中的那一行开关是亮的）；选「自定义」时
// 出表单：类型、主机、端口、用户名、密码，带校验和「保存并测试」。
//
// ## 三档怎么落到设置上
//
//	不使用代理   → `ProxyDirect`（连环境变量也不看，一律直连）
//	使用系统代理 → `ProxyNone`（老版本就有的默认值：照 HTTP_PROXY / HTTPS_PROXY / NO_PROXY
//	               环境变量走，没设就是直连——这一档没有读 macOS / Windows 的系统代理面板）
//	自定义代理   → `ProxyHTTP / HTTPS / SOCKS4 / SOCKS5` + 主机 / 端口 / 用户名 / 密码
//
// ⚠️ **选「自定义」不写设置，只有「保存并测试」才写**：设置里一旦是 http 而主机空着，之后每条
// 请求都会失败（`errProxyHostEmpty`），所以半填的表单只留在界面状态里（`proxyForm`）。
// 「测试」只拨一下代理自己的地址（TCP 连得上与否），不向任何真实站点发请求。
package ui

import (
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/motion"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/pud"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/scale"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/space"
	"github.com/ShellMonster/PudMusic/internal/ui/widgets/tok"
	"github.com/egoist/mygo/ui"
)

// proxyModes 是三档：名字、说明（原型 `proxy()` 里的三行）。
var proxyModes = []struct{ id, label, help string }{
	{"none", "不使用代理", "直接连接"},
	{"system", "使用系统代理", "跟随系统网络设置"},
	{"custom", "自定义代理", "手动填写类型、地址和端口"},
}

// proxyCustomTypes 是自定义代理的四种协议（原型 `['HTTP','HTTPS','SOCKS4','SOCKS5']`，标签加「 代理」）。
var proxyCustomTypes = []enumItem[app.ProxyType]{
	{app.ProxyHTTP, "HTTP 代理"},
	{app.ProxyHTTPS, "HTTPS 代理"},
	{app.ProxySOCKS4, "SOCKS4 代理"},
	{app.ProxySOCKS5, "SOCKS5 代理"},
}

// proxyForm 是自定义代理表单里还没保存的东西。
type proxyForm struct {
	// custom 是「用户点了自定义、还没保存」：表单要显示，设置里还不是自定义。
	custom bool
	// inited 说表单已经按设置灌过一次（每次进这个 Tab 重灌）。
	inited bool
	typ    app.ProxyType
	host   string
	port   string
	user   string
	pass   string
	// hostErr / portErr 是校验失败的那句话（空 = 没错）。
	hostErr, portErr string

	// mu 保护 result / testing：测试在后台线程里拨，写完要叫醒界面。
	mu      sync.Mutex
	result  string
	testing bool
}

// proxyProbe 是「测试」那一拨：连得上返回 nil。包级变量是为了测试能换掉它，不去碰网络。
var proxyProbe = func(host, port string) error {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 3*time.Second)
	if err != nil {
		return err
	}
	return conn.Close()
}

// proxyModeOf 是这一帧画面上选中的是哪一档。
func (s *Shell) proxyModeOf(st app.Settings) string {
	switch {
	case s.settings.proxy.custom:
		return "custom"
	case st.ProxySettings.Type == app.ProxyDirect:
		return "none"
	case st.ProxySettings.Type == app.ProxyNone:
		return "system"
	}
	return "custom"
}

// settingsProxy 是代理那一页。
func (s *Shell) settingsProxy(c *ui.Context, st app.Settings, ro bool) {
	f := &s.settings.proxy
	if !f.inited {
		f.inited = true
		cur := st.ProxySettings
		f.typ = cur.Type
		if f.typ == app.ProxyNone || f.typ == app.ProxyDirect {
			f.typ = app.ProxyHTTP
		}
		f.host, f.port, f.user, f.pass = cur.Host, portOf(cur.Port), cur.Username, cur.Password
	}
	mode := s.proxyModeOf(st)

	s.setGroup(c, "proxy-mode", "代理设置", func() {
		s.card(c, "proxy-mode", ro, func(cd *setCard) {
			for _, m := range proxyModes {
				id := m.id
				cd.row("proxy:"+id, m.label, m.help, func() {
					on := mode == id
					if pud.Switch(c, "set-radio:proxy:"+id, &on, "代理方式："+m.label) && !ro && mode != id {
						s.chooseProxyMode(id)
					}
				})
			}
		})
	})
	if mode == "custom" {
		s.setGroup(c, "proxy-form", "", func() { s.proxyFormCard(c, ro) })
	}
}

// chooseProxyMode 点了某一档。前两档立刻写设置；「自定义」只开表单（见文件头）。
func (s *Shell) chooseProxyMode(id string) {
	f := &s.settings.proxy
	switch id {
	case "none":
		f.custom = false
		s.applySetting("代理", func(cur *app.Settings) { cur.ProxySettings.Type = app.ProxyDirect })
	case "system":
		f.custom = false
		s.applySetting("代理", func(cur *app.Settings) { cur.ProxySettings.Type = app.ProxyNone })
	case "custom":
		f.custom = true
	}
}

// proxyFormCard 是自定义表单（`.set-card` 内边距 20、两列 1fr + 140、缝 16）。
func (s *Shell) proxyFormCard(c *ui.Context, ro bool) {
	f := &s.settings.proxy
	p := tok.Of(c.Theme())
	s.card(c, "proxy-form", ro, func(cd *setCard) {
		ui.Column(c).Key("proxy-fields").FillWidth().Padding(space.Px20).Gap(space.Px16).Children(func() {
			s.field(c, "px-type", "代理类型", "", func() {
				dropSelectW(c, "set-sel:px-type", &f.typ, proxyCustomTypes, "选择代理类型", ro, true)
			})
			ui.Row(c).Key("px-hp").FillWidth().Gap(space.Px16).AlignItems(ui.Start).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					s.field(c, "px-host", "代理主机", f.hostErr, func() { s.fieldInput(c, p, &f.host, "127.0.0.1", false, f.hostErr != "", ro) }, true)
				})
				ui.Column(c).Width(140).Shrink(0).Children(func() {
					s.field(c, "px-port", "代理端口", f.portErr, func() { s.fieldInput(c, p, &f.port, "7890", false, f.portErr != "", ro) }, true)
				})
			})
			ui.Row(c).Key("px-up").FillWidth().Gap(space.Px16).AlignItems(ui.Start).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					s.field(c, "px-user", "代理用户名", "", func() { s.fieldInput(c, p, &f.user, "", false, false, ro) })
					ui.Text(c, "没有就留空").FontSize(scale.Font(c, scale.Size12)).TextColor(p.Text3).
						Margin(space.Px6, space.None, space.None, space.None)
				})
				ui.Column(c).Width(140).Shrink(0).Children(func() {
					s.field(c, "px-pass", "代理密码", "", func() { s.fieldInput(c, p, &f.pass, "", true, false, ro) })
				})
			})
			ui.Row(c).Key("px-save-row").FillWidth().Gap(space.Px12).AlignItems(ui.Center).Children(func() {
				f.mu.Lock()
				testing, result := f.testing, f.result
				f.mu.Unlock()
				if pud.ButtonDisabled(c, "px-save", pud.Primary, pud.Sm, "保存并测试", "", ro || testing).Clicked() {
					s.saveProxy()
				}
				if result != "" {
					ui.Text(c, result).FontSize(scale.Font(c, scale.Size12)).TextColor(p.Text3).Wrap().Grow(1)
				}
			})
		})
	})
}

// field 是原型 `.field`：13/500 的标签 + 控件 + 错误句（12 号 err）。mark 为 true 时标签后面带红星。
func (s *Shell) field(c *ui.Context, key, label, errText string, ctrl func(), mark ...bool) {
	p := tok.Of(c.Theme())
	ui.Column(c).Key("field:" + key).FillWidth().Gap(space.Px6).Children(func() {
		ui.Row(c).Gap(space.Px4).Children(func() {
			ui.Text(c, label).FontSize(scale.Font(c, scale.Size13)).FontWeight(500).TextColor(p.Text)
			if len(mark) > 0 && mark[0] {
				ui.Text(c, "*").FontSize(scale.Font(c, scale.Size13)).TextColor(tok.Of(c.Theme()).Err)
			}
		})
		ctrl()
		if errText != "" {
			ui.Text(c, errText).FontSize(scale.Font(c, scale.Size12)).TextColor(tok.Of(c.Theme()).Err)
		}
	})
}

// fieldInput 是原型 `.field input`：高 40、左右 12、圆角 8、line-strong 描边、surface-2 底、14 号；
// 聚焦描边 accent，校验失败描边 err。
//
// ⚠️ 不许给输入框加 Key（Key 只能挂在创建时没有处理输入的控件上）；外面那一格有 Key 就够了。
func (s *Shell) fieldInput(c *ui.Context, p tok.Palette, v *string, placeholder string, password, invalid, ro bool) {
	in := ui.TextInput(c, v).FillWidth().Height(40).Padding(space.None, space.Px12).Radius(tok.R8).
		Background(p.Surface2).FontSize(scale.Font(c, scale.Size14)).TextColor(p.Text).
		Placeholder(placeholder).Disabled(ro).Label(placeholder + "输入框")
	if password {
		in = in.Password()
	}
	border := p.LineStrong
	switch {
	case invalid:
		border = p.Err
	case in.Focused():
		border = p.Accent
	}
	in.Border(1, border).Transition(motion.Fade)
}

// saveProxy 是「保存并测试」：先校验，通过了写设置，再在后台拨一下代理看连不连得上。
func (s *Shell) saveProxy() {
	f := &s.settings.proxy
	f.hostErr, f.portErr = "", ""
	host := strings.TrimSpace(f.host)
	port := parsePort(f.port)
	if host == "" {
		f.hostErr = "请填写代理主机"
	}
	if port == nil || *port > 65535 {
		f.portErr = "端口是 1–65535 的数字"
	}
	if f.hostErr != "" || f.portErr != "" {
		return
	}
	typ, user, pass := f.typ, f.user, f.pass
	if !s.applySettingOK("代理", func(cur *app.Settings) {
		cur.ProxySettings = app.ProxySettings{Type: typ, Host: host, Port: port, Username: user, Password: pass}
	}) {
		return
	}
	f.custom = false
	f.mu.Lock()
	f.testing, f.result = true, "正在测试…"
	f.mu.Unlock()
	portStr := strconv.Itoa(*port)
	go func() {
		err := proxyProbe(host, portStr)
		msg := "已保存，代理可以连上"
		if err != nil {
			msg = "已保存，但连不上这个代理：" + trimDialErr(err)
		}
		f.mu.Lock()
		f.testing, f.result = false, msg
		f.mu.Unlock()
		s.wake.invalidate()
	}()
}

// trimDialErr 去掉拨号错误前面那一串 `dial tcp 127.0.0.1:7890:`，留说得清的那一句。
func trimDialErr(err error) string {
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 {
		msg = msg[i+2:]
	}
	return msg
}

// portOf 把设置里那个可选的端口变成输入框上摆的字符串。
func portOf(p *int) string {
	if p == nil {
		return ""
	}
	return strconv.Itoa(*p)
}

// parsePort 把输入框上的字符串变成设置里那个可选的端口：不是正整数就是「没设」
// （0 端口和「没填」分不开，Biu 的 `port` 默认值是 `undefined`，我们照它用 `*int`）。
func parsePort(s string) *int {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || v <= 0 {
		return nil
	}
	return &v
}
