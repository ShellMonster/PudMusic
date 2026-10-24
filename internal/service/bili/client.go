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
//	ErrTooFrequent  -509，被限速。该提示，同样不该**立刻**重试。
//	其它非 0 code  → *APIError，调用方自己判。
//
// 「能不能重试」那张表在 `retry.go`（唯一的一处），这里只管分类。
package bili

import (
	"bytes"
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
	// ErrTruncatedResponse 是「响应本来是 JSON，但没传完」。
	//
	// 🔴 **它和 `ErrBadResponse` 必须分开**（票 #346）：一个是网络抖了一下
	// （再问一次有可能好），一个是回了一页压根不是 JSON 的东西（多半是风控
	// 挡的，立刻再问还是撞同一面墙）。混成一个的话，被截断的响应会被判成
	// 不可重试，用户按一次撞一次。
	ErrTruncatedResponse = errors.New("bili: 响应被截断了")
	// ErrBadResponse 是「B 站这次回的不是 JSON」。
	//
	// ⚠️ **它不是 `ErrRiskControl`**：真风控是 -352/-412（业务码，在 JSON
	// 里面），而这一档是**压根没拿到 JSON**——多半是风控挡下来的一页 HTML，
	// 也可能是网关回了个错页。判据在 `retry.go` 那张表里。
	ErrBadResponse = errors.New("bili: 响应不是 JSON")
)

// APIError 是 B 站返回的业务错误。
//
// Endpoint 记的是**脱敏过**的路径，不带查询串——带上的话诊断文件里
// 就有了签名 URL。调用方拼它进日志时不需要自己再处理。
type APIError struct {
	Code     int
	Message  string
	Endpoint string

	// Data 是那一段业务数据，**即使 code 不是 0 也带着**。
	//
	// ⚠️ 不要把它当成「错误就是没有数据」。B 站在「没登录」时回的是
	// `{"code":-101,"message":"账号未登录","data":{"isLogin":false,
	// "wbi_img":{...}}}`——**WBI 签名要用的 key 就在里面**。把它丢掉的话，
	// 未登录用户连一个签名接口都调不了（`fetchWBIKey` 会直接把 -101 往上抛，
	// 于是 `signWBI` 那句「拿不到 key 就原样放行」根本没机会执行）。
	Data json.RawMessage
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
	Code    int    `json:"code"`
	Message string `json:"message"`
	// Msg 是**音乐库那几个接口**用的字段名（`audio/music-service-c/url` 和
	// `/web/song/info` 回的是 `msg`，不是 `message`）。
	//
	// 不认它的话 code≠0 时 `Message` 是空串，于是 `Friendly` 拿到的
	// `cleanMessage("")` 返回空、最后落到「B 站返回了一个看不懂的错误（7201006）」
	// ——而 B 站明明在 `msg` 里写了中文原因（见 `Friendly` 的兜底那一段）。
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// message 是这一次的错误说明，两个字段名里挑有值的那个。
func (e envelope) message() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Msg
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

	// 五个主机名做成字段而不是常量，是为了测试能指到 fixture 上。
	//
	// ⚠️ 只有包内的测试能改（它们在同一个包里），对外没有任何入口
	// 能改——生产代码没有办法把请求发到别处去。
	hostAPI, hostPassport, hostMember, hostWWW, hostSearch string

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

// TestOnlyPointHostsAtFixture 把**每一个**主机和 cookie 地址全都指到一台
// 本地服务器上。
//
// # 为什么需要它
//
// jar 是**按域匹配**的。B 站的 cookie 域是 `.bilibili.com`，所以读写
// cookie 必须用一个 B 站域名——而在测试里请求打在 `127.0.0.1` 上。
// 没有这一条，包外的测试（app 那边的登录流程用例）会**打到真的
// passport.bilibili.com 上去**：不报错、结果还挺合理，
// 于是测试变成「取决于能不能连上 B 站」——能连上就一直轮询，
// 连不上就报网络错误，两种都跟被测的逻辑无关。
//
// # 为什么名字里带 TestOnly
//
// 它是一个只给测试用的后门，挂在生产二进制里没有害（只是把主机指到别处，
// 而这个后门没有任何生产调用方），但**必须一眼能看出它是后门**。
// 用 `TestOnly` 前缀是因为 Go 生态约定：带这个前缀的东西被误用时
// 编译器不会拦，但 grep 一下就找得到，而找不到才是真危险。
func (c *Client) TestOnlyPointHostsAtFixture(base string) {
	c.hostAPI = base
	c.hostPassport = base
	c.hostMember = base
	c.hostWWW = base
	c.hostSearch = base
	c.sessionBase = base
	// 已经取过的 WBI key 是从旧主机来的，换了主机就不能再用。
	c.wbiState.mu.Lock()
	c.wbiState.key = ""
	c.wbiState.expiry = time.Time{}
	c.wbiState.mu.Unlock()
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
		hostWWW:      HostWWW,
		hostSearch:   HostSearch,
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
	// JSON 是 JSON 请求体，会被编码成 `{"k":v}` 发出去（Content-Type 是
	// `application/json`）。
	//
	// **和 Form 互斥**：两个都给的话 JSON 赢（`Form` 被丢掉）。写成互斥而不是
	// 报错，是因为 B 站这批接口几乎全是表单，只有「动态点赞」那几个要 JSON——
	// 为它加一道「两个都给就返回错误」的判断，只是把一个不会发生的组合变成
	// 一段谁也走不到的分岔。
	//
	// ⚠️ csrf **不进这个体**：B 站的写接口认的 csrf 是查询串和 `bili-jct` 头
	// （`Client.Do` 里的 `Call.CSRF` 已经放了两处），体里那份只有 web 端自己
	// 转发时才有。
	JSON any
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
	// contentType 非空时由这里设。留空就让 setHeaders 那一句兜底
	// （它认的是「有请求体就补 urlencoded」，对 JSON 来说是错的）。
	switch {
	case call.JSON != nil:
		raw, err := json.Marshal(call.JSON)
		if err != nil {
			return nil, fmt.Errorf("bili: JSON 请求体编不出来: %w", err)
		}
		body = bytes.NewReader(raw)
		call.Form = nil
	case call.Form != nil:
		body = strings.NewReader(call.Form.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return nil, fmt.Errorf("bili: 造请求: %w", err)
	}
	if call.JSON != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// ⚠️ **有表单体就必须带这个头**，不是锦上添花：B 站那边是 servlet，
	// 少了 `application/x-www-form-urlencoded` 它**不会**去解析请求体，
	// 于是 `fid` / `act` 全是空的——而返回多半还是 HTTP 200 + code 0。
	// 表现是「关注按下去什么也没变」，一句错都没有。
	//
	// 这条是 2026-10-16 加的：`ModifyRelation`（关注/取关）是第一个真的带
	// 表单参数的接口。之前那个退登接口发的是空表单，所以缺了也没人发现。
	if call.Form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	c.setHeaders(req, csrf)

	if c.onReq != nil {
		c.onReq(method, endpoint.String())
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// 包一层而不是原样返回：调用方要能分清「没登录」（该提示重新登录）
		// 和「网断了」（该提示检查网络）。原始的 *url.Error 不带这个信息。
		//
		// ⚠️ 末尾那个原始错误用 `%w` 而不是 `%v`：**用户取消也要认得出来**。
		// 下载队列判「这一项是用户停掉的，不是失败」靠的就是
		// `errors.Is(err, context.Canceled)`，而 `%v` 拼进去的只是字——于是用户
		// 在**取流途中**按下的「暂停」会被记成「任务出错」，界面上是一行红字加
		// 一颗「重试」，而不是「下载暂停」。消息一个字没变（两个动词渲染出来的
		// 字符串一样），变的是错误链。
		return nil, fmt.Errorf("%w: %s %s: %w", ErrNetwork, method, endpoint.Path, err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		// 🔴 **读一半断了 = 响应没传完**（票 #346）：那是网络层的事（抖了一下、
		// 连接被掐了），**再问一次有可能好**——所以它归「可重试」那一档，
		// 不跟「压根不是 JSON」混在一起。
		return nil, fmt.Errorf("%w: %s %s: 响应没读全: %w",
			ErrTruncatedResponse, method, endpoint.Path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 非 2xx 时 B 站有时仍然给 JSON 外壳，但绝大多数时候给的是
		// 网关错误页。两种都报网络错——HTTP 层就没成，业务 code 没意义。
		return nil, fmt.Errorf("%w: %s %s: HTTP %d", ErrNetwork, method, endpoint.Path, resp.StatusCode)
	}

	var env envelope
	if err := json.Unmarshal(bodyBytes, &env); err != nil {
		// 🔴 **「不是 JSON」要分成两种**（票 #346）：
		//
		//	截断   响应本来是 JSON，但没传完（网络抖了一下）→ 再问一次有可能好
		//	不是   回的是一页 HTML —— 多半是风控挡的 → 立刻再问还是撞同一面墙
		//
		// ⚠️ **它们都不能归到 `ErrRiskControl`**：真风控是 -352/-412（业务码，
		// 在 JSON 里面），而这一档是压根没拿到 JSON。混成一处的话，被截断的
		// 响应会跟着风控一起被判成不可重试（票 #316 收窄的那一档）。
		if !looksLikeHTML(bodyBytes) {
			return nil, fmt.Errorf("%w: %s %s: 响应被截断了: %w",
				ErrTruncatedResponse, method, endpoint.Path, err)
		}
		return nil, fmt.Errorf("%w: %s %s: 响应不是 JSON（多半是被风控挡了）: %w",
			ErrBadResponse, method, endpoint.Path, err)
	}
	if env.Code != 0 {
		return nil, &APIError{
			Code:     env.Code,
			Message:  env.message(),
			Endpoint: endpoint.Path,
			Data:     env.Data,
		}
	}
	return env.Data, nil
}

// looksLikeHTML 报这个响应体是不是**一页 HTML**（而不是 JSON）。
//
// 判据是**头一个非空白字符**：B 站的业务响应一定是 `{` 开头，而风控挡下来的
// 那一页是 HTML（`<` 开头）。所以「不是 HTML」= 它本来是 JSON，只是没传完
// （或者是空的——连接在第一个字节之前就断了，那也是没传完）。
//
// ⚠️ 别拿 `json.Unmarshal` 的那句错来判（"unexpected end of JSON input"）：
// 那是 Go 的内部措辞，改一次就变，而这里判的是**响应本身**。
func looksLikeHTML(body []byte) bool {
	t := bytes.TrimLeft(body, " \t\r\n")
	if len(t) == 0 {
		return false // 空的：连接在第一个字节之前就断了
	}
	return t[0] != '{' && t[0] != '['
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
	// ⚠️ **带表单体的那几个写接口必须有这一行**。Go 的客户端不会自己补它
	// （`http.NewRequest` 对 `*strings.Reader` 只认长度不认类型），而服务端
	// 看到没有 Content-Type 的请求体**当它是空的**——于是 csrf、media_id、
	// resources 全部收不到，答案是一句 -101 或 -400，谁都看不出是哪一步丢的。
	//
	// Biu 上游发的是 multipart（`request-interceptors.ts` 的 `useFormData`），
	// 那一路自带 Content-Type；我们是 urlencoded，得自己写上。
	//
	// ⚠️ **只在真的发了表单体时才设**：GET 请求带一个
	// `Content-Type: application/x-www-form-urlencoded` 不会出事，但会让
	// `OnRequest` 记下来的每一个地址都多一样不必要的东西，而排障时那些
	// 记录是要人一条一条看的。
	if req.Body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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
	// HostWWW 是 www.bilibili.com。**少数接口在它上面而不是 api 上面**——
	// 音乐库的元信息 `/audio/music-service-c/web/song/info` 就是（见 audio.go）。
	// 凭据边界照样认它（白名单按 `.bilibili.com` 后缀，见 `app.InjectPolicy`）。
	HostWWW = "https://www.bilibili.com"
	// HostSearch 是**搜索站**那一台。搜索联想词（`/main/suggest`）在它上面而不在
	// api 上面（见 suggest.go）。凭据边界照样认它（同一个 `.bilibili.com` 后缀）。
	HostSearch = "https://s.search.bilibili.com"
)

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
