// 设置页那格代理的**消费端**：把 `Settings.ProxySettings` 翻成
// `http.Transport` 认的两样东西——每请求问一次的 `Proxy`，和 SOCKS4 那一档
// 的隧道拨号。
//
// # 为什么是「每请求现读设置」而不是「改设置时重建网络层」
//
// 全应用唯一的网络层建一次就不重建（见 `internal/ui/login_mount.go` 的
// `newCredentialTransport`），而 `http.Transport` 的字段不许在请求在飞时改。
// 所以两边都拿快照：`MountSettings` / `UpdateSettings` 把最新一份存进来
// （`storeProxy`），`Proxy` 和 `DialContext` 每次被调到时现取（`ProxySnapshot`）。
// 设置一改，下一条请求就走新路——不需要重启，也不需要任何回调。
//
// # 「不使用代理」为什么不等于「一条请求都不代理」
//
// 那一档回落到环境变量（`HTTP_PROXY` / `HTTPS_PROXY`…），和网络层从前的行为
// 一致：从终端带着 `https_proxy` 起应用的用户，不动设置页代理也照常走代理。
// 显式选了四种协议之一，环境变量就让位——设置页说了算。
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/net/http/httpproxy"
)

// currentProxy 是进程里「当前生效的那份代理设置」。没挂设置时是 nil，
// 按「不使用代理」处理。
//
// 进程里只有一个应用、一份设置（`mountedSettings` 的注释），所以这里存的是
// 值本身而不是按 App 的表——网络层是包级建好的，它没有 App 可查。
var currentProxy atomic.Pointer[ProxySettings]

// storeProxy 记下「现在生效的代理」。MountSettings / UpdateSettings 调。
//
// ⚠️ 认不出的类型一律按 none 存：store 那边的 `normalize()` 本来会把坏值兜回
// none，这里再兜一道，手搓的 Settings（测试里常见）漏进来也不会被当成
// 「某种未知协议」拿去拼地址。
func storeProxy(s Settings) {
	switch s.ProxySettings.Type {
	case ProxyHTTP, ProxyHTTPS, ProxySOCKS4, ProxySOCKS5:
		ps := s.ProxySettings
		currentProxy.Store(&ps)
	default:
		currentProxy.Store(nil)
	}
}

// ProxySnapshot 是此刻生效的代理设置。网络层的两个钩子每请求各调一次。
func ProxySnapshot() ProxySettings {
	if p := currentProxy.Load(); p != nil {
		return *p
	}
	return ProxySettings{Type: ProxyNone}
}

// ProxyForSettings 是 http.Transport.Proxy 那一格的答案。
//
//	none      → 环境变量（文件头那节说了为什么）。
//	http/https/socks5 → 代理地址（SOCKS5 是 http.Transport 原生认的 scheme）。
//	socks4    → (nil, nil)：http.Transport 不认这个 scheme，隧道由
//	            `Socks4Tunnel` 那一半在拨号里做。两半必须成对用。
//
// 🔴 **类型不是 none 但主机空着要报错，不能安静回落**：设置页摆着五个格子，
// 用户填了一半（选了 HTTP、还没填主机），每条请求都失败、失败原因里写着
// 「代理主机没填」——这比「填了没反应」强得多。回环地址（localhost /
// 127.0.0.1）永远不代理，跟 `ProxyFromEnvironment` 的内置规则一致。
func ProxyForSettings(ps ProxySettings, req *http.Request) (*url.URL, error) {
	if req != nil && req.URL != nil && neverProxyHost(req.URL.Hostname()) {
		return nil, nil
	}
	if ps.Type != ProxyNone && strings.TrimSpace(ps.Host) == "" {
		return nil, errProxyHostEmpty
	}
	switch ps.Type {
	case ProxyNone:
		// ⚠️ 不用 `http.ProxyFromEnvironment`：它把环境变量**缓存**在进程级
		// 的 sync.Once 里，第一次被调到之后就再也不读了。这里要的是「每一问
		// 都现读」——测试要靠它（t.Setenv 改完就得算数），语义上也更老实。
		// FromEnvironment 和它同一份实现（x/net 里那份），规则原样：
		// HTTP_PROXY / HTTPS_PROXY / NO_PROXY、localhost 永不代理。
		return httpproxy.FromEnvironment().ProxyFunc()(req.URL)
	case ProxySOCKS4:
		// 隧道在拨号那一半（Socks4Tunnel），这里只管「不代理」。
		return nil, nil
	default:
		return proxyURL(ps)
	}
}

// errProxyHostEmpty 是「选了协议但没填主机」。文字是说给用户看的。
var errProxyHostEmpty = errors.New("app: 代理主机没填——填上主机，或者把代理类型换回「不使用代理」")

// proxyURL 把 http/https/socks5 三档翻成代理地址。带认证就把 userinfo 摆上
// （http 和 socks5 的握手都认它；https 代理是先 TLS 再 CONNECT，同一格）。
func proxyURL(ps ProxySettings) (*url.URL, error) {
	scheme := string(ps.Type)
	u := &url.URL{Scheme: scheme, Host: net.JoinHostPort(ps.Host, defaultPort(scheme, ps.Port))}
	if ps.Username != "" {
		if ps.Password != "" {
			u.User = url.UserPassword(ps.Username, ps.Password)
		} else {
			u.User = url.User(ps.Username)
		}
	}
	return u, nil
}

// defaultPort 是没填端口时的那一档：按代理协议的惯用端口来，
// 跟 curl / 浏览器对同一个空格的答案一致。
func defaultPort(scheme string, port *int) string {
	if port != nil && *port > 0 {
		return strconv.Itoa(*port)
	}
	switch scheme {
	case "https":
		return "443"
	case "socks5", "socks4":
		return "1080"
	default:
		return "80"
	}
}

// neverProxyHost 报这个主机要不要豁免代理。回环永远直连：打在本机的
// 请求（测试的 httptest、本地服务）不该被全局代理截走。
func neverProxyHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ─────────────────────────────────────────────────────────────────────────────
// SOCKS4：http.Transport 不认的第四种协议
// ─────────────────────────────────────────────────────────────────────────────

// Socks4Tunnel 给拨号用的那一半：只在代理类型是 SOCKS4 时给出一个「通过代理
// 连到 addr」的拨号函数，其余类型 ok 是 false（调用方走自己的直连拨号）。
//
// 为什么不能像另外三档那样只给地址：`http.Transport` 的 `Proxy` 只认
// http/https/socks5 三种 scheme，而 SOCKS4 的隧道又只是「先连代理、说一声
// 目标是谁、然后当一根 TCP 管子用」——所以它落在 `DialContext` 里：代理返回
// (nil, nil)（`ProxyForSettings` 那一半），Transport 拿着**目标**地址来拨，
// 我们在这里把它拨进隧道。TLS 和 Host 头都照常，因为隧道之后就是一根
// 通向目标的裸 TCP。
//
// ⚠️ Username 是 SOCKS4 的 USERID（协议里没有密码那一节）；设置页的密码格在
// 这一档不参与。
func Socks4Tunnel(ps ProxySettings) (func(ctx context.Context, network, addr string) (net.Conn, error), bool) {
	if ps.Type != ProxySOCKS4 {
		return nil, false
	}
	addr := net.JoinHostPort(ps.Host, defaultPort("socks4", ps.Port))
	userid := ps.Username
	return func(ctx context.Context, network, target string) (net.Conn, error) {
		return socks4Dial(ctx, addr, userid, network, target)
	}, true
}

// socks4Reply 是回复第二个字节的含义（协议规定的那张表，写成能读的话）。
var socks4Reply = map[byte]string{
	91: "请求被代理拒绝或失败",
	92: "代理连不上 identd（本机的 ident 服务）",
	93: "ident 报告的用户id和请求里的不一致",
}

// socks4Dial 走一遍 SOCKS4/4a 握手，成功返回通向 target 的连接。
//
// 目标是 IP 字面量或者能查到地址时走 SOCKS4（协议本来只认 IP）；查不到才走
// 4a（把主机名原样递过去，靠代理解析）——有些代理不认 4a，但目标主机名解析
// 不出来时这是唯一的路。USERID 允许为空。
func socks4Dial(ctx context.Context, proxy, userid, network, target string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return nil, fmt.Errorf("app: SOCKS4 目标地址不合法 %q：%w", target, err)
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, network, proxy)
	if err != nil {
		return nil, fmt.Errorf("app: 连不上 SOCKS4 代理 %s：%w", proxy, err)
	}
	// 握手要有底线：借用 ctx 的截止时间，没有就给一个。握手完了撤掉——
	// 这根管子接下来的主人（通常是 TLS 握手和响应体）不该背这个钟。
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	}

	ip := net.ParseIP(host)
	if ip == nil {
		if ips, lookupErr := resolveHost(ctx, host); lookupErr == nil && len(ips) > 0 {
			ip = ips[0].IP
		}
	}

	portNum := 0
	_, _ = fmt.Sscanf(port, "%d", &portNum)
	req := []byte{4, 1, byte(portNum >> 8), byte(portNum)}
	if ip4 := ip.To4(); ip != nil && len(ip4) == 4 {
		req = append(req, ip4...)
		req = append(req, userid...)
		req = append(req, 0)
	} else {
		// 4a：地址摆 0.0.0.x（x≠0 是约定），主机名跟在 USERID 后面。
		req = append(req, 0, 0, 0, 1)
		req = append(req, userid...)
		req = append(req, 0)
		req = append(req, host...)
		req = append(req, 0)
	}
	if _, err := conn.Write(req); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("app: SOCKS4 握手发不出去：%w", err)
	}
	rep := make([]byte, 8)
	if _, err := io.ReadFull(conn, rep); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("app: SOCKS4 握手读不回话：%w", err)
	}
	if rep[1] != 90 {
		_ = conn.Close()
		if why, ok := socks4Reply[rep[1]]; ok {
			return nil, fmt.Errorf("app: SOCKS4 代理没接：%s（码 %d）", why, rep[1])
		}
		return nil, fmt.Errorf("app: SOCKS4 代理没接（码 %d）", rep[1])
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

// resolveHost 是主机名解析那一跳，单独拎出来是为了测试能替掉它——
// 测试环境里没有可控的 DNS。
var resolveHost = func(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}
