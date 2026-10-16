// 扫码登录的协议层：申请二维码、轮询扫码状态。
//
// # 三种状态，不是两种
//
// 二维码登录在服务端是有**三个**真实分支的，漏掉任何一个用户就卡在
// 转圈里（`PLAN.md` 阶段 3 第 3.5 条：0 成功 / 86038 过期 / 86090 已扫待确认）：
//
//	86101  还没扫 —— 这是「继续等」，不是错误，也不是结束
//	86090  已经扫了，手机上正在等用户点确认 —— 界面必须说清楚，
//	      否则用户以为没扫成功，退出重进，卡死在这
//	0      扫成功，SESSDATA 等 cookie 已经随响应写进 jar
//	86038  二维码失效 —— 唯一需要「重新申请一张」的状态
//
// Biu 上游（`qrcode-login.tsx`）把 86090 和 0 归成同一个
// `isPendingConfirm`，而 0 那一支它同时当成成功处理了——UI 上
// 「二维码已扫码未确认」这句提示在成功之后还会闪一帧。这里把三态
// 分开，让界面没有「靠猜」的地方。
//
// # 状态码藏在哪里
//
// ⚠️ 轮询接口的外层 `code` **恒为 0**，真正的状态在 `data.code` 里。
// 写成 `if resp.Code == 86038` 的话条件永远不成立，程序会永远转圈——
// 而这正是「用户卡在转圈里」最难查的一种。字段形状照
// `biu/src/service/passport-login-web-qrcode-poll.ts`。
package bili

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// 扫码登录的三个接口的路径。
//
// 主机名不在这里拼（`Client` 持有，好让测试指到 fixture 上）。
const (
	pathQRCodeGenerate = "/x/passport-login/web/qrcode/generate"
	pathQRCodePoll     = "/x/passport-login/web/qrcode/poll"
	pathExit           = "/x/passport-login/exit"
)

// Render 把这张二维码在本地画成模块矩阵。**不联网取图。**
//
// 为什么不能问 B 站要图、也不能用在线二维码服务：
// 二维码的内容就是那条登录地址，在有效期内它是一次性的授权凭据。
// 把它送到第三方，等于把「用户扫的是哪个账号」交给别人决定。
//
// 用中档纠错：屏幕反光、手指遮挡、手机拍屏都要留余量。
func (q QRCode) Render() (*Code, error) {
	return Encode(q.URL, ECMid)
}

// ScanState 是扫码的状态。
type ScanState int

const (
	// ScanWaiting 是 86101：还没扫。继续等，不是错误。
	ScanWaiting ScanState = iota
	// ScanScanned 是 86090：手机扫到了，用户还没在手机上点确认。
	ScanScanned
	// ScanConfirmed 是 0：登录成功。
	ScanConfirmed
	// ScanExpired 是 86038：二维码失效，要重新申请。
	ScanExpired
)

// String 是给界面显示的话。
//
// 三种「不是错误」的状态都有各自的话，因为**共用一句「请扫码」是这三态
// 里最容易犯的错**：用户明明已经扫了，界面还说请扫码，他就退出去重来。
func (s ScanState) String() string {
	switch s {
	case ScanWaiting:
		return "请用 bilibili 手机客户端扫码"
	case ScanScanned:
		return "已扫码，请在手机上确认"
	case ScanConfirmed:
		return "登录成功"
	case ScanExpired:
		return "二维码已失效，请刷新"
	}
	return "未知状态"
}

// Terminal 报这个状态是不是「不会再变了」。
//
// 轮询循环靠它决定要不要停：过期和成功要停，等和待确认要接着等。
func (s ScanState) Terminal() bool { return s == ScanConfirmed || s == ScanExpired }

// B 站服务端的状态码。单独列出来是为了让「界面文案」和「协议常量」
// 分开——文案是产品决定，码是 B 站定的，混在一起改文案就会动到协议。
const (
	codeScanWaiting   = 86101
	codeScanScanned   = 86090
	codeScanConfirmed = 0
	codeScanExpired   = 86038
)

// QRCode 是一张刚申请到的二维码。
type QRCode struct {
	// URL 是二维码里的内容——登录地址。
	//
	// ⚠️ 它在有效期内**是一次性的授权凭据**：谁扫到谁登进去。
	// 所以它不进日志、不进诊断导出，也不发给任何第三方（见 qrcode.go
	// 里为什么自己画位图而不联网取图）。
	URL string
	// Key 是轮询用的秘钥，恒为 32 字符。
	Key string
	// TTL 是这张二维码的有效期（秒）。过期后轮询会返回 86038。
	TTL time.Duration
}

// qrcodeGenerateData 是申请接口的响应。
type qrcodeGenerateData struct {
	URL       string `json:"url"`
	QRCodeKey string `json:"qrcode_key"`
}

// NewQRCode 申请一张新的二维码。
//
// 每张二维码只能扫一次，且有效期很短。过期后**只能重新申请**，
// 没有「让它再活一会儿」的办法——所以调用方过期时直接调本函数。
func (c *Client) NewQRCode(ctx context.Context) (QRCode, error) {
	data, err := c.Do(ctx, Call{Method: "GET", URL: c.hostPassport + pathQRCodeGenerate})
	if err != nil {
		return QRCode{}, fmt.Errorf("bili: 申请二维码: %w", err)
	}
	var raw qrcodeGenerateData
	if err := json.Unmarshal(data, &raw); err != nil {
		return QRCode{}, fmt.Errorf("bili: 解二维码结果: %w", err)
	}
	if raw.URL == "" || raw.QRCodeKey == "" {
		// 少一个字段就是拿不到能用的二维码。B 站在被风控时会给一个
		// code=0 但 url 为空的响应——**不检查就等于拿一张空图去显示**，
		// 用户看到一片白，什么也扫不出来。
		return QRCode{}, fmt.Errorf("bili: 申请二维码: B 站返回了空的结果（多半是被风控挡了）")
	}
	return QRCode{URL: raw.URL, Key: raw.QRCodeKey, TTL: qrcodeTTL}, nil
}

// qrcodeTTL 是二轮码的有效期。//
// B 站在响应里给的 `ttl` 恒为 1（秒），那个值指的是「轮询间隔」而不是
// 「有效期」——照它算的话二维码会在用户还没来得及掏手机时就过期，
// 而 B 站返回的是 86038，界面上表现为「二维码已失效」。
// 实测有效期是 180 秒（3 分钟），这里按 180 秒处理。
const qrcodeTTL = 180 * time.Second

// qrcodePollData 是轮询接口的响应。
//
// ⚠️ 状态码在 **data.code**，不是外层的 code。
type qrcodePollData struct {
	// Code 是扫码状态：0 / 86038 / 86090 / 86101。
	Code int `json:"code"`
	// Message 是状态的中文说明，只用来排查，不该上界面（B 站改文案
	// 我们就跟着变，用户看到的是自己的界面）。
	Message string `json:"message"`
	// URL 是跨域登录地址，登录成功时才有。
	URL string `json:"url"`
	// RefreshToken 是持久化刷新口令，登录成功时才有。
	//
	// 它和 cookie 一样是凭据：**不进日志、不进诊断导出**。
	RefreshToken string `json:"refresh_token"`
	// Timestamp 是登录时间（毫秒）。
	Timestamp int64 `json:"timestamp"`
}

// ScanResult 是一次轮询的结果。
type ScanResult struct {
	State ScanState
	// RefreshToken 在 State 是 ScanConfirmed 时才有。
	RefreshToken string
	// Message 是 B 站给的状态说明，只用于诊断。
	Message string
	// LoggedInAt 是登录时刻（毫秒时间戳）。只在成功时有值。
	LoggedInAt time.Time
}

// PollScan 问一次「这张二维码扫得怎么样了」。
//
// **这是协议层**：它只发一次请求就返回，不循环、不等。
// 「每 2 秒轮询」是业务层的事（app/account.go），放在那里才能被测到
// 时序、退登、过期重发这些真正会出问题的地方。
func (c *Client) PollScan(ctx context.Context, key string) (ScanResult, error) {
	if key == "" {
		return ScanResult{}, fmt.Errorf("bili: 没有 qrcode_key，轮询不了")
	}
	q := url.Values{"qrcode_key": {key}}
	data, err := c.Do(ctx, Call{Method: "GET", URL: c.hostPassport + pathQRCodePoll, Query: q})
	if err != nil {
		return ScanResult{}, fmt.Errorf("bili: 查扫码状态: %w", err)
	}
	var raw qrcodePollData
	if err := json.Unmarshal(data, &raw); err != nil {
		return ScanResult{}, fmt.Errorf("bili: 解扫码状态: %w", err)
	}
	return scanResultOf(raw), nil
}

// scanResultOf 把协议上的码翻成 ScanState。
//
// 单独的函数是为了能被表驱动地测：**每一个码都要有一条用例**，
// 因为漏掉一个码的后果是轮询循环永远不退出，而那在界面上只是
// 「转圈」。
func scanResultOf(raw qrcodePollData) ScanResult {
	out := ScanResult{Message: raw.Message}
	switch raw.Code {
	case codeScanConfirmed:
		out.State = ScanConfirmed
		out.RefreshToken = raw.RefreshToken
		if raw.Timestamp > 0 {
			out.LoggedInAt = time.UnixMilli(raw.Timestamp)
		}
	case codeScanExpired:
		out.State = ScanExpired
	case codeScanScanned:
		out.State = ScanScanned
	case codeScanWaiting:
		out.State = ScanWaiting
	default:
		// 出现了不认识的码。**不猜**：当成「还没扫」继续等，同时把原文
		// 带上（只带 message，不带 refresh_token——那是凭据）。
		// 猜错的后果是用户明明已经扫了却被告知二维码失效。
		out.State = ScanWaiting
		out.Message = fmt.Sprintf("未知的扫码状态码 %d：%s", raw.Code, raw.Message)
	}
	return out
}

// Logout 告诉 B 站「这个会话我不要了」。
//
// 清本地凭据是 `app.Manager.Logout` 的事；这一方法是把**服务端**的
// 会话也作废。少调它不会泄露什么（SESSDATA 过期就失效了），但同一台
// 机器上另一个 B 站客户端还可能看到这个会话在册。
//
// B 站那边注销失败时**不返回错误**：本地已经清干净了，把一个远端
// 的失败报给用户只会让他以为退登没成功，而下次启动照样是登出状态。
// 失败照样交给 OnRequest 那条日志路径记下来。
func (c *Client) Logout(ctx context.Context) {
	_, _ = c.Do(ctx, Call{Method: "POST", URL: c.hostPassport + pathExit, CSRF: true, Form: url.Values{}})
}
