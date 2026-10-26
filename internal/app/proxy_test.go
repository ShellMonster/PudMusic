package app

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func proxyReq(t *testing.T, raw string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, raw, nil)
	if err != nil {
		t.Fatalf("造不出请求 %s：%v", raw, err)
	}
	return req
}

func TestProxyForSettingsMapsTheFourProtocols(t *testing.T) {
	port := 7890
	cases := []struct {
		name string
		ps   ProxySettings
		want string // 代理地址（不带 userinfo）
		user string
		pass string
	}{
		{
			name: "http 带端口带认证",
			ps:   ProxySettings{Type: ProxyHTTP, Host: "proxy.lan", Port: &port, Username: "u", Password: "p"},
			want: "proxy.lan:7890", user: "u", pass: "p",
		},
		{
			name: "https 没填端口落 443",
			ps:   ProxySettings{Type: ProxyHTTPS, Host: "proxy.lan"},
			want: "proxy.lan:443",
		},
		{
			name: "socks5 没填端口落 1080",
			ps:   ProxySettings{Type: ProxySOCKS5, Host: "::1"},
			want: "[::1]:1080",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ProxyForSettings(tc.ps, proxyReq(t, "https://api.bilibili.com/x"))
			if err != nil {
				t.Fatalf("要地址要出了错：%v", err)
			}
			if got == nil {
				t.Fatalf("要回了 nil，要的是 %s", tc.want)
			}
			if got.Host != tc.want {
				t.Errorf("代理地址是 %s，要的是 %s", got.Host, tc.want)
			}
			if tc.user != "" {
				pw := ""
				if got.User != nil {
					pw, _ = got.User.Password()
				}
				if got.User == nil || got.User.Username() != tc.user || pw != tc.pass {
					t.Errorf("userinfo 不对（要的是 %s:%s）", tc.user, tc.pass)
				}
			} else if got.User != nil {
				t.Errorf("不该带 userinfo，带了 %v", got.User)
			}
		})
	}

	// 第四种协议在 Proxy 这一半的答案是「不代理」：http.Transport 不认
	// socks4 这个 scheme，隧道由 Socks4Tunnel 那一半接（下面单独测）。
	got, err := ProxyForSettings(ProxySettings{Type: ProxySOCKS4, Host: "proxy.lan"}, proxyReq(t, "https://api.bilibili.com/x"))
	if err != nil || got != nil {
		t.Fatalf("SOCKS4 这一半该是 (nil, nil)，回了 (%v, %v)", got, err)
	}
}

func TestNoProxyFollowsTheEnvironment(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://env-proxy:3128")
	t.Setenv("http_proxy", "http://env-proxy:3128")
	t.Setenv("NO_PROXY", "")

	got, err := ProxyForSettings(ProxySettings{Type: ProxyNone}, proxyReq(t, "http://api.bilibili.com/x"))
	if err != nil || got == nil || got.Host != "env-proxy:3128" {
		t.Fatalf("none 该回落到环境变量，回了 (%v, %v)", got, err)
	}
}

func TestAnEmptyProxyHostIsAnErrorNotSilence(t *testing.T) {
	_, err := ProxyForSettings(ProxySettings{Type: ProxyHTTP, Host: " "}, proxyReq(t, "https://api.bilibili.com/x"))
	if err == nil || !strings.Contains(err.Error(), "代理主机没填") {
		t.Fatalf("主机空着该报一句能读的错，回了 %v", err)
	}
}

func TestLoopbackRequestsAreNeverProxied(t *testing.T) {
	port := 9
	ps := ProxySettings{Type: ProxyHTTP, Host: "proxy.lan", Port: &port}
	for _, raw := range []string{"http://127.0.0.1:1234/x", "http://localhost:1234/x", "https://[::1]:1234/x"} {
		got, err := ProxyForSettings(ps, proxyReq(t, raw))
		if err != nil || got != nil {
			t.Errorf("%s 该直连，回了 (%v, %v)", raw, got, err)
		}
	}
}

// TestTheProxySettingTakesEffectOnTheNextRequest 是「设置改了立刻生效」的
// 那一条：同一个 Transport，设置里换成代理，请求就走代理；换回 none，
// 下一条就直连——不重启、不重建网络层。
//
// 目标不能用 httptest 直接给的 127.0.0.1 地址（回环豁免会把它挡成直连），
// 所以 URL 写成 target.example，由拨号那一层把地址改回本机端口。
func TestTheProxySettingTakesEffectOnTheNextRequest(t *testing.T) {
	defer storeProxy(Settings{})
	// none 那一档回落到环境变量，而这台机器的 shell 里可能本来就设着
	// http_proxy——钉死成空，测试只认设置页那一格。
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("http_proxy", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("https_proxy", "")
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")

	proxyHits := 0
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 不真转发：这一测只关心「请求有没有经过代理」，目标侧有自己的
		// 记账（真转发还得替它解决目标主机名的解析，跟被测的事无关）。
		proxyHits++
	}))
	defer proxySrv.Close()

	targetHits := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits++
	}))
	defer target.Close()

	// 把「target.example」翻译回 httptest 真在监听的那个本机端口；
	// 别的地址（代理自己）原样拨。
	_, targetPort, _ := net.SplitHostPort(strings.TrimPrefix(target.URL, "http://"))
	fakeName := "127.0.0.1:" + targetPort
	tr := &http.Transport{
		Proxy: func(req *http.Request) (*url.URL, error) {
			return ProxyForSettings(ProxySnapshot(), req)
		},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if strings.HasPrefix(addr, "target.example:") {
				addr = fakeName
			}
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},
	}
	client := &http.Client{Transport: tr}
	targetURL := "http://target.example:" + targetPort + "/"

	port := mustPort(t, proxySrv.URL)
	storeProxy(Settings{ProxySettings: ProxySettings{Type: ProxyHTTP, Host: "127.0.0.1", Port: &port}})
	if _, err := client.Get(targetURL); err != nil {
		t.Fatalf("走代理的请求失败了：%v", err)
	}
	if proxyHits != 1 || targetHits != 0 {
		t.Fatalf("这一条该从代理走（代理 %d 条、目标 %d 条）", proxyHits, targetHits)
	}

	storeProxy(Settings{})
	if _, err := client.Get(targetURL); err != nil {
		t.Fatalf("直连的请求失败了：%v", err)
	}
	if proxyHits != 1 || targetHits != 1 {
		t.Fatalf("换回 none 之后该直连（代理 %d 条、目标 %d 条）", proxyHits, targetHits)
	}
}

// TestTheSettingsMountFeedsTheProxySnapshot 走的是真的挂载路：读盘出来的
// 代理设置进快照，改一笔也进快照——网络层读的就是这一份。
func TestTheSettingsMountFeedsTheProxySnapshot(t *testing.T) {
	defer storeProxy(Settings{})

	dir := t.TempDir()
	path := filepath.Join(dir, SettingsFileName)
	a := New()
	if err := a.MountSettings(path); err != nil {
		t.Fatalf("挂不上设置：%v", err)
	}
	if got := ProxySnapshot(); got.Type != ProxyNone {
		t.Fatalf("没设过代理，快照该是 none，是 %s", got.Type)
	}

	port := 7890
	if _, err := a.UpdateSettings(func(s *Settings) {
		s.ProxySettings = ProxySettings{Type: ProxySOCKS5, Host: "proxy.lan", Port: &port, Username: "u", Password: "p"}
	}); err != nil {
		t.Fatalf("改不了设置：%v", err)
	}
	got := ProxySnapshot()
	if got.Type != ProxySOCKS5 || got.Host != "proxy.lan" || got.Port == nil || *got.Port != port ||
		got.Username != "u" || got.Password != "p" {
		t.Fatalf("改完的快照不对：%+v", got)
	}

	if _, err := a.UpdateSettings(func(s *Settings) { s.ProxySettings.Type = ProxyNone }); err != nil {
		t.Fatalf("改不回 none：%v", err)
	}
	if got := ProxySnapshot(); got.Type != ProxyNone {
		t.Fatalf("换回 none 之后快照还是 %s", got.Type)
	}
	if err := a.CloseSettings(); err != nil {
		t.Fatalf("收不了设置：%v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SOCKS4 隧道：对着一个假代理服务器把协议走一遍
// ─────────────────────────────────────────────────────────────────────────────

// fakeSocks4 是一个只会接一次的 SOCKS4 代理：读请求、记账、回 reply 码，
// 然后把连接当成一根管子——读一段、原样写回（回显）。
type fakeSocks4 struct {
	t        *testing.T
	listener net.Listener
	reply    byte
	// 记下来的请求要素。
	port     int
	ip       net.IP
	userid   string
	hostname string // 4a 那一支才有
}

func startFakeSocks4(t *testing.T, reply byte) *fakeSocks4 {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("假代理起不来：%v", err)
	}
	f := &fakeSocks4{t: t, listener: l, reply: reply}
	go f.serve()
	t.Cleanup(func() { _ = l.Close() })
	return f
}

func (f *fakeSocks4) addr() string { return f.listener.Addr().String() }

func (f *fakeSocks4) serve() {
	conn, err := f.listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	// 请求定长部分：VN CD PORT(2) IP(4)，后面 USERID 到 NUL；4a 再跟一段
	// 主机名到 NUL。读不动就悄悄收工——这是测试脚手架，没有可报的人。
	head := make([]byte, 8)
	if _, err := io.ReadFull(conn, head); err != nil {
		return
	}
	f.port = int(head[2])<<8 | int(head[3])
	f.ip = net.IP(head[4:8])
	rest, err := readUntilNul(conn)
	if err != nil {
		return
	}
	f.userid = rest
	if f.ip[0] == 0 && f.ip[1] == 0 && f.ip[2] == 0 && f.ip[3] != 0 {
		if f.hostname, err = readUntilNul(conn); err != nil {
			return
		}
	}
	_, _ = conn.Write([]byte{0, f.reply, 0, 0, 0, 0, 0, 0})
	if f.reply != 90 {
		return
	}
	// 管子：读一段写回一段，客户端关了就收工。
	buf := make([]byte, 64)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			if _, werr := conn.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func readUntilNul(conn net.Conn) (string, error) {
	var sb strings.Builder
	buf := make([]byte, 1)
	for {
		if _, err := io.ReadFull(conn, buf); err != nil {
			return sb.String(), err
		}
		if buf[0] == 0 {
			return sb.String(), nil
		}
		sb.WriteByte(buf[0])
	}
}

func TestTheSocks4TunnelSpeaksTheProtocol(t *testing.T) {
	f := startFakeSocks4(t, 90)
	tun, ok := Socks4Tunnel(ProxySettings{Type: ProxySOCKS4, Host: "127.0.0.1", Port: intPtr(portOf(t, f.addr())), Username: "jj"})
	if !ok {
		t.Fatal("SOCKS4 档该给出隧道")
	}
	// 目标是 IP 字面量：协议本来只认 IP，这一支不该去查 DNS。
	conn, err := tun(context.Background(), "tcp", "93.184.216.34:80")
	if err != nil {
		t.Fatalf("隧道拨不通：%v", err)
	}
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("写不进隧道：%v", err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("读不回隧道：%v", err)
	}
	if string(buf) != "ping" {
		t.Fatalf("隧道里回的是 %q，要的是 ping", buf)
	}
	_ = conn.Close()

	if f.port != 80 {
		t.Errorf("请求里的端口是 %d，要的是 80", f.port)
	}
	if f.ip.String() != "93.184.216.34" {
		t.Errorf("请求里的地址是 %s，要的是 93.184.216.34", f.ip)
	}
	if f.userid != "jj" {
		t.Errorf("USERID 是 %q，要的是 jj", f.userid)
	}
	if f.hostname != "" {
		t.Errorf("IP 那一支不该带主机名，带了 %q", f.hostname)
	}
}

func TestTheSocks4TunnelSendsTheHostnameWhenItCannotResolve(t *testing.T) {
	f := startFakeSocks4(t, 90)
	old := resolveHost
	resolveHost = func(context.Context, string) ([]net.IPAddr, error) { return nil, os.ErrDeadlineExceeded }
	defer func() { resolveHost = old }()

	tun, _ := Socks4Tunnel(ProxySettings{Type: ProxySOCKS4, Host: "127.0.0.1", Port: intPtr(portOf(t, f.addr()))})
	conn, err := tun(context.Background(), "tcp", "cdn.example.com:443")
	if err != nil {
		t.Fatalf("4a 隧道拨不通：%v", err)
	}
	_ = conn.Close()

	if f.hostname != "cdn.example.com" {
		t.Errorf("主机名是 %q，要的是 cdn.example.com", f.hostname)
	}
	if f.ip[0] != 0 || f.ip[1] != 0 || f.ip[2] != 0 || f.ip[3] == 0 {
		t.Errorf("4a 的地址该是 0.0.0.x，是 %s", f.ip)
	}
}

func TestTheSocks4TunnelUsesAResolvedAddressWhenItCan(t *testing.T) {
	f := startFakeSocks4(t, 90)
	old := resolveHost
	resolveHost = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.IPv4(10, 1, 2, 3).To4()}}, nil
	}
	defer func() { resolveHost = old }()

	tun, _ := Socks4Tunnel(ProxySettings{Type: ProxySOCKS4, Host: "127.0.0.1", Port: intPtr(portOf(t, f.addr()))})
	conn, err := tun(context.Background(), "tcp", "cdn.example.com:443")
	if err != nil {
		t.Fatalf("隧道拨不通：%v", err)
	}
	_ = conn.Close()

	// 查得到就走 SOCKS4 本来的样子：地址是查来的 IP，不带主机名。
	if f.ip.String() != "10.1.2.3" {
		t.Errorf("地址是 %s，要的是查来的 10.1.2.3", f.ip)
	}
	if f.hostname != "" {
		t.Errorf("不该带主机名，带了 %q", f.hostname)
	}
}

func TestTheSocks4ReplyCodeIsSpelledOut(t *testing.T) {
	f := startFakeSocks4(t, 91)
	tun, _ := Socks4Tunnel(ProxySettings{Type: ProxySOCKS4, Host: "127.0.0.1", Port: intPtr(portOf(t, f.addr()))})
	_, err := tun(context.Background(), "tcp", "93.184.216.34:80")
	if err == nil || !strings.Contains(err.Error(), "拒绝") {
		t.Fatalf("码 91 该翻成人话，回了 %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 小工具
// ─────────────────────────────────────────────────────────────────────────────

func mustPort(t *testing.T, raw string) int {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("解析不出 %s：%v", raw, err)
	}
	return portOf(t, u.Host)
}

func portOf(t *testing.T, hostPort string) int {
	t.Helper()
	_, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		t.Fatalf("拆不开 %s：%v", hostPort, err)
	}
	n := 0
	if _, err := fmt.Sscanf(port, "%d", &n); err != nil {
		t.Fatalf("端口不是数字 %s：%v", port, err)
	}
	return n
}

func intPtr(n int) *int { return &n }
