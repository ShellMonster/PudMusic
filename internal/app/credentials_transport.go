// 凭据边界的**生产实现**：把它套在网络层外面，每个请求、每一跳重定向
// 都按白名单重新判一次。
//
// InjectPolicy 是那张白名单本身（纯逻辑，credentials.go），这一层是
// 「什么时候调它、调几遍」。分开的理由：白名单可以被纯逻辑测试穷举，
// 而「什么时候调」要跑真的 http.Client 才验得到——`credentials_test.go`
// 里 `TestPolicyRoundTripThroughRealRedirect` 走的就是这一层。
//
// # 两条防线，缺一不可
//
//	RoundTrip     每个请求都过一遍。**重定向的每一跳都是一次独立的
//	              RoundTrip**，所以 302 跳到陌生主机时那一跳自己会被拦下。
//	CheckRedirect 在跳转发生前再看一眼目标，抓的是 RoundTrip 管不到的那一类：
//	              **HTTPS 降级到 HTTP**。那种情况下主机名没变、白名单认得它，
//	              `Classify` 会判成 CredBiliFull——凭据就顺着明文链路走了。
//	              `RedirectClass` 里那条硬规则就是为了这个。
//
// # 为什么凭据不是这里加上去的
//
// Cookie 由 `http.Client` 的 Jar 在进 RoundTrip **之前**加上。
// 这一层只负责**删**，不负责加：加是 jar 的事，删是这一层的事。
// 两边都做的话，「哪一份是真的」就没法回答了，而凭据这件事上
// 「有两个来源」等于「有一个来源在偷偷生效」。
package app

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
)

// CredentialTransport 是带凭据边界的 RoundTripper。
type CredentialTransport struct {
	// Base 是真正发出去的那一层。nil 用 http.DefaultTransport。
	Base http.RoundTripper
	// Policy 是那张白名单。零值（没设过任何主机）会**什么都不给**——
	// 那是往严的方向退，不是往宽的方向猜。
	Policy InjectPolicy

	// UserAgent / Referer 是每次都带上的声明头。
	//
	// B 站 CDN 只给 Referer 就放行，不给直接 403；B 站的 API 对空 UA
	// 会区别对待。这两个头不是凭据（见 Sanitize 的注释），但必须带上。
	UserAgent string
	Referer   string
}

// maxRedirects 是跟随重定向的上限。
//
// 用 net/http 自己的 10 是默认值，但这里显式写出来：一旦哪天改成
// 「不限制」，一个跳转环就能把界面线程挂死。
const maxRedirects = 10

var errTooManyRedirects = errors.New("app: 重定向跳太多了")

// RoundTrip 发一个请求，发之前按白名单把不该带的凭据删干净。
func (t *CredentialTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, errors.New("app: 没有请求")
	}
	// 克隆一份再改：RoundTripper 的约定是不能改调用方传进来的 Request。
	// 直接改的话调用方那边看到的请求凭空少了几个头，而
	// 「我明明设了 Cookie」这种 bug 极难查。
	r := req.Clone(req.Context())

	t.setDeclarationHeaders(r)
	t.Policy.SanitizeAs(r, t.classOf(r))

	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}

// classOf 判这个请求按策略能拿到什么。
//
// 判不出来的一律按 CredNone 走——**往最严的方向退**。
//
// 「非 HTTPS 一律 CredNone」是这里的关键一条：主机名没变，白名单认得它，
// 但明文链路上一个字节的凭据都不能留。`InjectPolicy.Classify` 判的是
// 主机、不管协议，所以这条得在这里补。
func (t *CredentialTransport) classOf(req *http.Request) CredentialClass {
	if req == nil || req.URL == nil {
		return CredNone
	}
	if !strings.EqualFold(req.URL.Scheme, "https") {
		return CredNone
	}
	return t.Policy.Classify(req.URL.String())
}

func (t *CredentialTransport) setDeclarationHeaders(r *http.Request) {
	if t.UserAgent != "" && r.Header.Get("User-Agent") == "" {
		r.Header.Set("User-Agent", t.UserAgent)
	}
	if t.Referer != "" && r.Header.Get("Referer") == "" {
		r.Header.Set("Referer", t.Referer)
	}
}

// Client 造一个配好边界的 http.Client。
//
// Jar 可以是 nil（没登录时）。**Jar 只管加 Cookie，不管删**——
// 删由上面那一层做。
func (t *CredentialTransport) Client(j http.CookieJar) *http.Client {
	return &http.Client{
		Transport: t,
		Jar:       j,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errTooManyRedirects
			}
			from := via[len(via)-1].URL
			// 跳过去之后手上该剩什么，按**目标**重新判一次。
			if t.Policy.RedirectClass(from, req.URL) != CredBiliFull {
				// 不是 B 站 API 了：Cookie / Authorization / 代理密码全删。
				// 跟 RoundTrip 里是同一套规则，这里是提前一步——
				// 万一将来在 RoundTrip 之前又往请求上放了什么东西，
				// 它不会跟着跳过去。
				t.Policy.SanitizeAs(req, t.classOf(req))
			}
			return nil
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 请求日志：写出去的每一个地址都先脱敏
// ─────────────────────────────────────────────────────────────────────────────

// RequestLogger 把每个请求的地址写进一个 io.Writer，**先脱敏**。
//
// # 为什么要它，而不是直接 log.Printf
//
// 一个请求地址里能出现的东西：WBI 签名（w_rid / wts / sign）、CSRF、
// 甚至 qrcode_key（那是一次性的登录授权）。B 站接口返回的错误信息里
// 有时还带着 Cookie 片段。直接打出去，诊断文件就等于把账号交出去。
//
// secrets 是**回调**不是切片：退登之后名单要立刻变成空的，
// 拿一个建好就缓存的列表，下一次登录之后的请求全都会带着没脱敏的
// Cookie 值进日志——而那正是「导出诊断信息里出现凭据」最常见的成因。
type RequestLogger struct {
	mu      sync.Mutex
	w       io.Writer
	secrets func() []string
}

// NewRequestLogger 建一个日志器。secrets 返回当前登录态里的秘密片段
// （`Session.Secrets()` 就是一个现成的）。nil 表示没有要脱敏的东西。
func NewRequestLogger(w io.Writer, secrets func() []string) *RequestLogger {
	return &RequestLogger{w: w, secrets: secrets}
}

// Log 写一条。直接把它当 `bili.Options.OnRequest` 用。
func (l *RequestLogger) Log(method, rawURL string) {
	if l == nil || l.w == nil {
		return
	}
	var secrets []string
	if l.secrets != nil {
		secrets = l.secrets()
	}
	line := "biu: " + method + " " + NewRedactor(secrets...).URL(rawURL) + "\n"

	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = io.WriteString(l.w, line)
}
