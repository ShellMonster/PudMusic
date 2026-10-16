// 「导出诊断信息」的那一份。
//
// # 验收原文
//
// 「导出的 JSON 里 grep 不到敏感字段名」（`PLAN.md` 阶段 3 回归证据）。
//
// 这条最容易**看起来做完了**：把结构体写出来，字段挑一挑，
// 一个 cookie 值也没放——然后某天有人为了排查加了
// `RawRequest string`，诊断包就带着签名 URL 发出去了。
//
// 所以这里的原则是**白名单而不是黑名单**：`Diagnostics` 的每个字段
// 都是显式列出来的、没有 map、没有 `any`、没有原始字符串。
// 想加一条信息就得往这个结构体上写一个字段——那一行是 review 时
// 唯一能看见的地方。
//
// 另外有一层兜底：导出之前拿 `Redactor` 再过一遍。
// 将来有人往某个字段里塞了 `SESSDATA` 的值，那一层会把它打掉，
// 而不是让一份含凭据的诊断包发到 issue 上。
package app

import (
	"encoding/json"
	"runtime"
	"strconv"
	"time"
)

// Diagnostics 是能安全发给别人的那部分运行状态。
//
// 每一个字段都必须是「知道它没有凭据」才写在这里的。
// 判断方法很简单：**它能不能被还原成一把钥匙或者一个账号**。
// 能，就不放。UserID 只是一个数字，放；refresh_token 能换 cookie，不放。
type Diagnostics struct {
	// 应用
	Version      string `json:"version"`
	GoVersion    string `json:"goVersion"`
	OS           string `json:"os"`
	ExportedAt   string `json:"exportedAt"`
	CredentialIO string `json:"credentialBackend"`
	CredentialUp bool   `json:"credentialBackendAvailable"`

	// 登录
	LoggedIn bool   `json:"loggedIn"`
	UserID   string `json:"userId,omitempty"`
	// SessionSavedAt 是这份登录态存下来的时刻。
	// **只有时刻，没有内容**——时刻能帮用户判断「我是不是登很久了」。
	SessionAge string `json:"sessionAge,omitempty"`
	// NextRefreshIn 是「多久之后该去续期了」。
	NextRefreshIn string `json:"nextRefreshIn,omitempty"`

	// 网络
	RequestsSent  int      `json:"requestsSent"`
	ErrorCodes    []string `json:"bilibiliErrorCodes,omitempty"`
	InjectPolicy  []string `json:"credentialHosts"`
	RedirectClass []string `json:"redirectClasses,omitempty"`
}

// NewDiagnostics 造一份诊断信息。
//
// ⚠️ 它**不读网络层**、不读设置、不读播放队列——只读凭据那一块的状态。
// 别的模块（网络层计数、错误码统计）以后接进来时各自往结构体上加字段，
// 加的时候同样要过一遍「能不能被还原成钥匙」这一关。
func NewDiagnostics(version string) Diagnostics {
	return Diagnostics{
		Version:      version,
		GoVersion:    runtime.Version(),
		OS:           runtime.GOOS + "/" + runtime.GOARCH,
		ExportedAt:   time.Now().Format(time.RFC3339),
		CredentialIO: "unknown",
		InjectPolicy: allowedHostSummary(),
	}
}

// Attach 填上「这个 App 此刻的登录情况」。
//
// 单独一个方法而不是塞进构造函数：诊断信息要在一个**已经跑起来**的
// App 上取，而那一刻才有登录态可言。
func (d *Diagnostics) Attach(a *App) {
	acc := a.Account()
	if acc == nil {
		return
	}
	d.CredentialIO = acc.BackendName()
	d.CredentialUp = true
	s := acc.Session()
	d.LoggedIn = s.LoggedIn()
	// DedeUserID 是一个公开的数字标识（B 站上每个账号都有），
	// 它不是秘密——反过来，SESSDATA / bili_jct / refresh_token
	// 一个都不在这里。
	d.UserID = s.DedeUserID
	if !s.SavedAt.IsZero() {
		d.SessionAge = humanDuration(time.Since(s.SavedAt))
	}
	if !s.NextCheckRefreshTime.IsZero() {
		d.NextRefreshIn = humanDuration(time.Until(s.NextCheckRefreshTime))
	}
}

// JSON 导出成字节。
//
// 导出前用 `ForSession` 再脱敏一次。**这是兜底，不是主要防线**——
// 主要防线是结构体里根本没有那些字段。
func (d Diagnostics) JSON(s Session) ([]byte, error) {
	r := ForSession(s)
	// 先序列化再脱敏：Redactor 认的是文本里的秘密片段，
	// 而序列化之后正好是诊断文件里会出现的那段文本。
	return []byte(r.Text(string(mustJSON(d)))), nil
}

// RedactText 把一段文本里的凭据打掉，给「顺带导出的日志」用。
func (d Diagnostics) RedactText(s Session, text string) string {
	return ForSession(s).Text(text)
}

func mustJSON(v any) []byte {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		// Diagnostics 全是 string/int/bool，Marshal 不可能失败。
		// 万一以后有人加了不能序列化的字段，也不能让它崩掉导出诊断信息
		// 这条路——返回一个说明而不是 panic。
		return []byte(`{"error": "诊断信息序列化失败"}`)
	}
	return b
}

// allowedHostSummary 是白名单里那些主机的名字。
//
// **只给主机名，不给路径、不给参数**。白名单本身不是秘密
// （它是「我们信任哪些域」这份公开事实），但把它整张表连同
// 将来可能加进来的用户自定义主机原样导出去，就成了「告诉我们
// 哪些域能骗到凭据」的地图。
func allowedHostSummary() []string {
	p := DefaultInjectPolicy()
	out := []string{}
	for _, h := range p.biliHosts {
		out = append(out, h+"（完整凭据）")
	}
	for _, h := range p.biliSuffixes {
		out = append(out, h+"（完整凭据）")
	}
	for _, h := range p.refererHosts {
		out = append(out, h+"（只给 Referer）")
	}
	for _, h := range p.refererSuffixes {
		out = append(out, h+"（只给 Referer）")
	}
	return out
}

// humanDuration 说人话：多久之前 / 多久之后。
func humanDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return "不到一分钟"
	case d < time.Hour:
		return plural(int(d.Minutes()), "分钟")
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "小时")
	default:
		return plural(int(d.Hours()/24), "天")
	}
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return itoa(n) + " " + unit
}

func itoa(v int) string { return strconv.Itoa(v) }
