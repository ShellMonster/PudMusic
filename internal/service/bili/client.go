// Package bili 是 B 站 API 的**唯一**客户端。
//
// # 为什么只有一份
//
// Biu 上游把同一批接口写了两遍（渲染进程 axios、主进程 got），连签名算法
// 都抄了两份，改一处漏一处就静默失败（`SPEC.md` 的问题陈述第三条）。
// 这里把整个网络面收进一个 `Client`：WBI 签名只有一份、Cookie jar 只有
// 一份、请求头只有一份、错误分类只有一份。
//
// 后面取流、收藏夹、UP 空间、歌词、下载全都在这个包上加方法，
// **不许再起第二个 http.Client**——那是同一份请求头会第二次漂移的开始。
//
// # 分层：这一层不 import internal/app
//
// `SPEC.md` 的四层依赖是单向的：`app` → 服务层。所以这里不能反过来
// import app，否则包依赖成环。那么「凭据注入的白名单在哪管」这个问题
// 的答案是：**在注入进来的 Transport 里管**，不在这个包里管。
//
// 具体分工：
//
//   - `app.InjectPolicy` 持有那张白名单（谁能拿 Cookie、谁只能拿 Referer、
//     谁一个都拿不到），以及重定向逐跳重判的规则；
//   - `app.CredentialTransport` 把它套在 RoundTripper 上，每个请求、
//     每一跳重定向都过一遍；
//   - 这个包只负责**用**它：`Options.Transport` 传进来就带着边界了。
//
// # 错误语义
//
// B 站的业务错误全在 HTTP 200 的 JSON 里面（`{"code":-101,...}`）。
// 不把它分类，调用方只能靠字符串匹配，那是最脆的一种。
//
//	ErrNotLoggedIn  -101，登录态没了。该提示用户重新登录。
//	ErrRiskControl  -352/-412，B 站的风控。该提示但不该重试。
//	ErrTooFrequent  -509，被限速。该退避后重试。
//	其它非 0 code  → *APIError，调用方自己判。
package bili

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 凭据与状态相关的哨兵错误。都拿 errors.Is 判。
var (
	// ErrNotLoggedIn 是 -101：没有登录，或者登录态过期了。
	ErrNotLoggedIn = errors.New("bili: 没有登录")
	// ErrRiskControl 是 -352 / -412：B 站的风控拦下了请求。
	ErrRiskControl = errors.New("bili: 被 B 站风控拦下")
	// ErrTooFrequent 是 -509：请求太频繁。
	ErrTooFrequent = errors.New("bili: 请求太频繁")
	// ErrCSRF 是 -111：CSRF 校验没过。
	ErrCSRF = errors.New("bili: CSRF 校验没过")
	// ErrNetwork 是根本没拿到 HTTP 响应（断网、超时、DNS）。
	ErrNetwork = errors.New("bili: 网络没通")
)

// APIError 是 B 站返回的业务错误。
//
// Endpoint 记的是**脱敏过**的路径，不带查询串——带上的话诊断文件里
// 就有了签名 URL。调用方拼它进日志时不需要自己再处理。
type APIError struct {
	Code     int
	Message  string
	Endpoint string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("bili: %s 返回 %d：%s", e.Endpoint, e.Code, e.Message)
}

// Is 把 B 站的错误码映射到哨兵错误。
//
// 用 `Is` 而不是让调用方逐个 `errors.Is(e, ErrX)`：B 站的错误码到
// 「该怎么办」的映射是这个包的领域知识，泄漏给每个调用方只会让同一个
// 错误在不同页面上被处理成不同结果。
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrNotLoggedIn:
		return e.Code == -101
	case ErrRiskControl:
		return e.Code == -352 || e.Code == -412
	case ErrTooFrequent:
		return e.Code == -509
	case ErrCSRF:
		return e.Code == -111
	}
	return false
}

// B 站各接口共用的响应外壳。data 的形状每个接口都不一样，所以留成
// json.RawMessage 交给各方法的解码器。
type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// Options 是建客户端要的零件。零值能用，但那时没有凭据边界也没有 jar。
type Options struct {
	// Transport 是发请求用的网络层。
	//
	// **这里应该传 `app.CredentialTransport`**——它负责白名单和重定向
	// 逐跳重判。传裸的 `http.DefaultTransport` 意味着凭据边界被关掉，
	// 那不是「默认值」，是「默认不安全」。
	Transport http.RoundTripper

	// CSRF 返回当前的 bili_jct。写接口靠它自动带上 CSRF。
	// 返回空串表示没有登录态，那就不带。
	CSRF func() string

	// Now 是签名用的时钟。nil 用 time.Now。
	// 给假时钟是为了让「签名的 wts 随时间走」这件事能被测到。
	Now func() time.Time

	// UserAgent 覆盖默认 UA。B 站对空 UA 的态度是「直接拒绝」。
	UserAgent string
	// Referer 覆盖默认 Referer。B 站很多接口没有 Referer 会返回 -403。
	Referer string

	// OnRequest 每发一个请求调一次，两个参数是已经拼好的地址。
	//
	// ⚠️ **它收到的是未脱敏的地址**（带签名、带 query）。
	// 传进来的回调必须自己脱敏——用 `app.NewRequestLogger`，
	// 它就是干这个的。之所以把责任交给调用方而不是在这里脱敏，
	// 是因为这一层不认识登录态，脱敏需要的正是登录态里的那些秘密。
	OnRequest func(method, rawURL string)
}

const (
	// DefaultUserAgent 是浏览器里会看到的那个 UA。
	//
	// B 站的接口对「像不像一个正常浏览器」有要求：UA 空着或者写着
	// Go-http-client/1.1 会被接口区别对待。抄一个真实浏览器的 UA
	// 是 Biu 上游在做的事（`request-interceptors.ts` 的 header 配置）。
	DefaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

	// DefaultReferer 是默认 Referer。B 站没有 Referer 会拒掉一部分接口。
	DefaultReferer = "https://www.bilibili.com/"

	// defaultTimeout 是单次请求的上限。
	//
	// 10 秒是 Biu 上游用的值。取流接口另有更长的超时（那是另一回事，
	// 不是这里这条路径）。
	defaultTimeout = 10 * time.Second

	// maxResponseBytes 是响应体的上限。
	//
	// **不是防御性编程，是 B 站的接口有时候会返回几百 MB 的东西**（比如
	// 某个接口少写了个参数，返回了整站页面）。不设上限，一次误调用就能
	// 把内存吃光，而那种崩溃在用户那边表现为「点一下就闪退」。
	maxResponseBytes = 32 << 20
)

// Client 是 B 站接口的客户端。**全项目一个**。
//
// 不是并发安全的构造期对象，但 Do 本身可以并发调。
type Client struct {
	http     *http.Client
	ua       string
	referer  string
	csrf     func() string
	now      func() time.Time
	onReq    func(method, rawURL string)
	wbiState *wbiKeyCache
	// jars 是 cookie jar 的可换壳，见 session.go。
	jars *jarHolder

	// 三个主机名做成字段而不是常量，是为了测试能指到 fixture 上。
	//
	// ⚠️ 只有包内的测试能改（它们在同一个包里），对外没有任何入口
	// 能改——生产代码没有办法把请求发到别处去。
	hostAPI, hostPassport, hostMember string

	// sessionBase 是读写登录 cookie 时用的那个地址。
	//
	// 为什么不是常量：B 站的 cookie 域是 `.bilibili.com`，jar 按域匹配，
	// 所以要用一个 B 站域名去读。但 fixture 的请求打在 127.0.0.1 上，
	// 用同一个地址才读得到——否则「jar 是空的」会变成一个查不出来的
	// fixture 假象。跟 host* 一样，只有包内测试能改。
	sessionBase string
}

// cookieURL 是读写登录 cookie 用的地址。
func (c *Client) cookieURL() *url.URL {
	u, err := url.Parse(c.sessionBase)
	if err != nil {
		// 默认值是常量 URL，解析不了只说明 Go 坏了。
		panic("bili: 解析会话地址失败: " + err.Error())
	}
	return u
}

// NewClient 按 Options 建一个客户端。
//
// 默认值：UA/Referer 用上面的常量、10 秒超时、带 WBI 密钥缓存、
// 空的 cookie jar（登录态由调用方 SeedCookies 装上去）。
func NewClient(o Options) *Client {
	jars := &jarHolder{}
	c := &Client{
		http: &http.Client{
			Transport: o.Transport,
			Timeout:   defaultTimeout,
			Jar:       jars,
		},
		ua:           orDefault(o.UserAgent, DefaultUserAgent),
		referer:      orDefault(o.Referer, DefaultReferer),
		csrf:         o.CSRF,
		now:          o.Now,
		onReq:        o.OnRequest,
		wbiState:     &wbiKeyCache{},
		jars:         jars,
		hostAPI:      HostAPI,
		hostPassport: HostPassport,
		hostMember:   HostMember,
		sessionBase:  "https://www.bilibili.com/",
	}
	if c.now == nil {
		c.now = time.Now
	}
	return c
}

// Call 是一次接口调用。
type Call struct {
	// Method 是 HTTP 方法。B 站的查询接口大多是 GET，写接口是 POST。
	Method string
	// URL 是完整地址（含主机）。
	URL string
	// Query 是查询参数。会按 WBI 规则处理，nil 也可以。
	Query url.Values
	// Form 是表单体。只有 POST 用；非 nil 时 CSRF 也写进表单。
	Form url.Values
	// CSRF 表示这次调用是写操作：自动带上 csrf 参数和 bili-jct 头。
	CSRF bool
	// WBI 表示这次调用要 WBI 签名。
	WBI bool
}

// Do 发一次请求，返回解好的 data 段。
//
// 返回的 `json.RawMessage` 是**调用方自己解**的那部分——B 站 90 个接口
// 的 data 形状各不相同，在这一层统一成一个结构体只会造出一个什么都装
// 下的空壳。
func (c *Client) Do(ctx context.Context, call Call) (json.RawMessage, error) {
	method := call.Method
	if method == "" {
		method = http.MethodGet
	}

	q := call.Query
	if q == nil {
		q = url.Values{}
	}
	csrf := ""
	if call.CSRF && c.csrf != nil {
		csrf = c.csrf()
		if csrf != "" {
			q.Set("csrf", csrf)
		}
	}
	if call.WBI {
		var err error
		q, err = c.signWBI(ctx, q)
		if err != nil {
			return nil, err
		}
	}

	endpoint, err := url.Parse(call.URL)
	if err != nil {
		return nil, fmt.Errorf("bili: 地址不合法 %q: %w", call.URL, err)
	}
	// 查询串是**签名的一部分**（WBI 把整个查询串哈希了），所以必须
	// 在拼地址时原样带上，不能让 net/url 再排序或重新编码一次。
	endpoint.RawQuery = q.Encode()

	var body io.Reader
	if call.Form != nil {
		body = strings.NewReader(call.Form.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return nil, fmt.Errorf("bili: 造请求: %w", err)
	}
	c.setHeaders(req, csrf)

	if c.onReq != nil {
		c.onReq(method, endpoint.String())
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// 包一层而不是原样返回：调用方要能分清「没登录」（该提示重新登录）
		// 和「网断了」（该提示检查网络）。原始的 *url.Error 不带这个信息。
		return nil, fmt.Errorf("%w: %s %s: %v", ErrNetwork, method, endpoint.Path, err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("bili: 读响应: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 非 2xx 时 B 站有时仍然给 JSON 外壳，但绝大多数时候给的是
		// 网关错误页。两种都报网络错——HTTP 层就没成，业务 code 没意义。
		return nil, fmt.Errorf("%w: %s %s: HTTP %d", ErrNetwork, method, endpoint.Path, resp.StatusCode)
	}

	var env envelope
	if err := json.Unmarshal(bodyBytes, &env); err != nil {
		// 拿到的是 HTML 或空页。B 站在被风控时会给一页 HTML，
		// 而那正是 ErrRiskControl 最典型的形态。
		return nil, fmt.Errorf("%w: %s %s: 响应不是 JSON（多半是被风控挡了）", ErrRiskControl, method, endpoint.Path)
	}
	if env.Code != 0 {
		return nil, &APIError{Code: env.Code, Message: env.Message, Endpoint: endpoint.Path}
	}
	return env.Data, nil
}

// setHeaders 装上每个请求都要带的头。
//
// ⚠️ CSRF 头走的是 `bili-jct` 而不是 `X-CSRF-Token`——写错了不报错，
// 只是一律返回 -111「CSRF 校验失败」，而 B 站对同一个 -111 有时给的是
// 「风控」，两者混在一起极难查。
func (c *Client) setHeaders(req *http.Request, csrf string) {
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Referer", c.referer)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	req.Header.Set("Origin", "https://www.bilibili.com")
	if csrf != "" {
		req.Header.Set(CSRFHeaderName, csrf)
	}
}

// CSRFHeaderName 是写接口的 CSRF 头名。
const CSRFHeaderName = "bili-jct"

// B 站几个主机名的常量。散在各处容易写错成 `api.bili.com` 之类。
const (
	// HostAPI 是读写接口的主机。
	HostAPI = "https://api.bilibili.com"
	// HostPassport 是登录相关的接口。
	HostPassport = "https://passport.bilibili.com"
	// HostMember 是会员相关接口。
	HostMember = "https://member.bilibili.com"
)

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
