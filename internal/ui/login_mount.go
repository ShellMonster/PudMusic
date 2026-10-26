// 把扫码登录挂到 App 上：建系统保护存储、装凭据边界、拉回登录态。
//
// 跟 `MountSettings` 是同一件事的两半——它决定「存哪儿」，这一件决定
// 「存的东西是哪一份、谁能拿到它」。两边都在**开窗口之前**调，
// 因为读盘（凭据文件 / DPAPI 文件）是阻塞 IO。
package ui

import (
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/egoist/mygo"
)

// debugLogEnv 打开请求日志的环境变量。
//
// **默认关着**：每一个 B 站请求都打一行，在正常使用下就是日志噪音。
// 排障时设上它，日志会写进用户数据目录，而地址在写出去之前
// 一定先过了脱敏（见 `app.RequestLogger`）。
const debugLogEnv = "BIU_DEBUG_HTTP"

// MountLogin 装好登录这一摊事。**必须在开窗口之前调。**
//
// 拉不回登录态不是致命的（`MountLogin` 内部记日志就继续），
// 装不出凭据后端也不是致命的——那意味着这一轮只能「登进去、
// 关掉就没了」，界面上会明说（见 viewAccountFooter）。
// 一个凭据后端出问题的应用起不来，比一个要重新登录一次的应用糟得多。
//
// 返回建好的那个 B 站客户端：在线取流（`MountStreaming`）要用的**就是它**。
// 全项目一个客户端这件事（`spec.md`「网络」一节）在这里露出来：换个调用点
// 建第二个，取流就会走第二份请求头和第二份 cookie jar。
func MountLogin(a *app.App) *bili.Client {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Printf("biu: 拿不到应用数据目录，这次的登录态存不住：%v", err)
	}
	store, err := app.NewSystemStore(dir)
	if err != nil {
		log.Printf("biu: 没有可用的凭据后端，登录态只在内存里：%v", err)
	}

	cli := bili.NewClient(bili.Options{
		Transport: newCredentialTransport(),
		// CSRF 在写接口上自动带上。取的是**当前**登录态里那个值，
		// 不是建好就存一份——那样登录之后所有写接口都会带一个空的 csrf，
		// 表现是「登录成功了，一收藏就 -111 CSRF 校验失败」。
		CSRF:      func() string { return a.Session().BiliJCT },
		OnRequest: debugRequestLogger(dir, a),
	})
	a.MountLogin(store, cli)
	return cli
}

// newCredentialTransport 是整个应用的网络层：唯一一个 http.Client 的底座。
//
// 凭据边界在这一层，不在业务代码里。业务代码只管发请求，
// 「这个请求能不能带 Cookie」由白名单回答——
// 靠每个调用点自己判断的话，漏一个就是一次泄露，而漏的那个没人会发现。
var (
	transportOnce sync.Once
	transportVal  *app.CredentialTransport
)

// newCredentialTransport 返回**整个应用唯一**的网络层。
//
// 建一次就够：`app.CredentialTransport` 本身没有状态（白名单是只读的、
// base 是个连接池），但每个 `http.Transport` 各自有一套连接池和空闲连接。
// 建两个就等于把连接数翻倍，而「同一个请求带了哪些头」这件事也就有了
// 两个可能不一样的来源——凭据这件事上「有两个来源」等于「有一个在偷偷生效」。
func newCredentialTransport() *app.CredentialTransport {
	transportOnce.Do(func() { transportVal = buildCredentialTransport() })
	return transportVal
}

func buildCredentialTransport() *app.CredentialTransport {
	base := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		// 代理从环境变量读（HTTP_PROXY / HTTPS_PROXY）。
		// B 站有些地区直连不通，而用户在命令行里设一次代理
		// 比在设置页里找「网络设置」简单得多。
		Proxy: http.ProxyFromEnvironment,
		// B 站的接口全是 HTTPS。关掉 HTTP/2 是因为 HTTP/2 的连接合并
		// 会让「一次请求带了哪些头」变得不可逐跳观察——
		// 而凭据边界那一层恰恰要求逐跳看得见。
		ForceAttemptHTTP2:     false,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		ExpectContinueTimeout: 2 * time.Second,
		MaxIdleConns:          16,
		IdleConnTimeout:       90 * time.Second,
	}
	return &app.CredentialTransport{
		Base:      base,
		Policy:    app.DefaultInjectPolicy(),
		UserAgent: bili.DefaultUserAgent,
		Referer:   bili.DefaultReferer,
	}
}

// debugRequestLogger 在开了调试开关时把请求写进一个文件。
//
// **地址一定先过脱敏**：`app.RequestLogger` 拿到的是未脱敏的
// 完整地址（带 WBI 签名），打码是它的活。
//
// 没开开关时返回 nil——`bili.Options.OnRequest` 为 nil 就完全不记录。
func debugRequestLogger(dir string, a *app.App) func(method, rawURL string) {
	if os.Getenv(debugLogEnv) == "" {
		return nil
	}
	path := filepath.Join(dir, "http-debug.log")
	// 0600：这个文件里可能有排障需要的地址，**在脱敏之后**。
	// 万一哪天脱敏被改坏了，600 至少让别的用户读不到。
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		log.Printf("biu: 开不了请求日志 %s：%v", path, err)
		return nil
	}
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) { _ = f.Close() })

	// secrets 是**回调**：退登之后名单要立刻变空，登录之后新出现的
	// 凭据要立刻进名单。拿一个建好就固定的切片，两边都会漏。
	return app.NewRequestLogger(f, func() []string {
		return a.Session().Secrets()
	}).Log
}
