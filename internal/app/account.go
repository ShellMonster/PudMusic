// 扫码登录的业务层：申请二维码、轮询、把结果存进系统保护存储。
//
// # 这一层为什么不放在 bili 里
//
// 因为**登录不只是协议**。它牵涉三件协议层不该知道的事：
//
//  1. **代次**。用户在扫码过程中点了退登，扫码结果回来时不能写进去。
//     这是 `Manager` 的 Lease 在管，`bili` 不该也不需要知道代次。
//  2. **唤醒界面**。即时模式的后台状态改了不叫醒它，界面永远停在旧帧
//     （`SPEC.md` 铁律三，实测「只投递不唤醒 = 0 帧刷新」）。
//  3. **轮询节奏**。协议层只提供「问一次」，问几次是产品决定。
//
// `SPEC.md` 的分层也要求这样：`app` 是「界面与服务层之间唯一的接口面」，
// 界面只跟 `app` 打交道，不认识 `bili`。
//
// # 三种状态在界面上的样子
//
//	Biu 上游把「已扫待确认」和「扫码成功」归成同一个文案，
//
// 于是成功之后「二维码已扫码未确认」还会再闪一帧。
// 这里 `LoginPhase` 把四种真实状态分开存，界面想显示哪句就显示哪句，
// 不用靠猜（见 bili/login.go 的 ScanState）。
package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// LoginPhase 是扫码登录在界面上的样子。
type LoginPhase int

const (
	// LoginIdle 是没在登录：还没点，或者已经退出这一轮。
	LoginIdle LoginPhase = iota
	// LoginWaiting 是二维码已经出来了，等用户扫。
	LoginWaiting
	// LoginScanned 是手机扫到了，用户还没在手机上点确认。
	LoginScanned
	// LoginExpired 是二维码失效了，要重新申请一张。
	LoginExpired
	// LoginDone 是登录成功。
	LoginDone
	// LoginFailed 是出了别的问题。
	LoginFailed
)

// String 是给日志看的名字。界面显示的话另有 `LoginState.Hint`。
func (p LoginPhase) String() string {
	switch p {
	case LoginIdle:
		return "空闲"
	case LoginWaiting:
		return "等扫码"
	case LoginScanned:
		return "已扫待确认"
	case LoginExpired:
		return "二维码过期"
	case LoginDone:
		return "登录成功"
	case LoginFailed:
		return "登录失败"
	}
	return "未知"
}

// LoginState 是界面画登录页要读的全部东西。
//
// 一帧里只读这一份拷贝，所以不会出现「二维码是新的、提示是旧的」。
type LoginState struct {
	Phase LoginPhase
	// QR 是本地渲染出来的二维码位图。**不是图片 URL**——
	// 二维码的内容是一次性的登录授权，绝不联网取图。
	QR *bili.Code
	// Hint 是给用户看的一句话。已经带好三态各自的文案。
	Hint string
	// Err 是失败原因。没有失败时是空串。
	Err string
	// LoggedIn 是此刻有没有登录态。登录成功后为 true。
	LoggedIn bool
}

// Busy 报登录流程是不是还在跑。
func (s LoginState) Busy() bool {
	return s.Phase == LoginWaiting || s.Phase == LoginScanned
}

// pollInterval 是轮询二维码状态的间隔。
//
// B 站建议 2 秒。太快会被限速（-412 风控），太慢用户会干等。
const pollInterval = 2 * time.Second

// Account 是登录这件事的全部状态。
type Account struct {
	mgr *Manager
	cli *bili.Client

	// interval 是轮询间隔。默认 pollInterval，测试里改小。
	interval time.Duration

	mu    sync.Mutex
	state LoginState
	// stop 是当前那一轮登录的取消函数。再开一轮之前先调它。
	stop context.CancelFunc

	wake func()
}

// newAccount 建一个登录管理器。
func newAccount(store Store, cli *bili.Client) *Account {
	return &Account{
		mgr:      NewManager(store),
		cli:      cli,
		interval: pollInterval,
		state:    LoginState{Phase: LoginIdle},
	}
}

// SetPollInterval 改轮询间隔。只给测试用（正式路径是 pollInterval）。
func (a *Account) SetPollInterval(d time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.interval = d
}

// setWake 装唤醒回调。
//
// App.SetWake 会顺手调它一次——因为挂登录的时机早于装窗口
// （挂登录要读盘，装窗口要建窗口），只等 MountLogin 时拿回调
// 的话拿到的是 nil，后面的「状态变了要一帧」就全丢了。
func (a *Account) setWake(fn func()) {
	a.mu.Lock()
	a.wake = fn
	a.mu.Unlock()
}

// BackendName 是凭据后端的名字，写诊断信息用。
func (a *Account) BackendName() string { return a.mgr.BackendName() }

// Session 是当前登录态。没登录时 LoggedIn() 是 false。
func (a *Account) Session() Session { return a.mgr.Current() }

// LoggedIn 报此刻有没有登录态。
func (a *Account) LoggedIn() bool { return a.mgr.Current().LoggedIn() }

// LoginState 返回界面要读的那份拷贝。
func (a *Account) LoginState() LoginState {
	a.mu.Lock()
	st := a.state
	a.mu.Unlock()
	// LoggedIn 不缓存：它跟着凭据走，缓存一份就会出现
	// 「界面说是登出的，取流却带着 SESSDATA」。
	st.LoggedIn = a.mgr.Current().LoggedIn()
	return st
}

// setState 改状态并叫醒界面。
//
// **每一处状态变化都必须经过这里**，包括网络错误。
// 漏掉一处，界面就停在前一帧，而用户看到的是「点了没反应」。
func (a *Account) setState(st LoginState) {
	a.mu.Lock()
	a.state = st
	a.mu.Unlock()
	if a.wake != nil {
		a.wake()
	}
}

// MountLogin 把登录挂到 App 上，并把凭据装进网络层。
//
// 必须在开窗口**之前**调：它要读盘（钥匙串 / DPAPI），那是阻塞 IO，
// 在界面线程上读的表现是开窗时卡一下。
//
// 读回来的是 ErrNotLoggedIn 就当作「没登录过」——那是正常起点。
// 读不出来（后端坏了、凭据解不开）则记下来但**不阻止应用启动**：
// 一个钥匙串出问题的应用起不来，比一个要重新登录一次的应用糟得多。
func (a *App) MountLogin(store Store, cli *bili.Client) {
	acc := newAccount(store, cli)
	acc.wake = a.wakeLocked()
	a.account = acc

	if err := acc.restore(cli); err != nil {
		log.Printf("biu: 读不回登录态，先按没登录处理：%v", err)
	}
}

// wakeLocked 取当前装的唤醒回调。
func (a *App) wakeLocked() func() {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.wake
}

// restore 从后端把登录态读回来，并装进网络层的 cookie jar。
//
// 「重启后还是登录」的落点就在这里，两步缺一不可：
// 读回 Session（拿到 SESSDATA），以及**把 cookie 装进 jar**
// （没有 jar，请求就是匿名发的，B 站一律回 -101）。
func (a *Account) restore(cli *bili.Client) error {
	err := a.mgr.Load(context.Background())
	switch {
	case err == nil:
	case errors.Is(err, ErrNotLoggedIn):
		return nil
	default:
		// 后端坏了 / 凭据解不开：当成没登录继续，但别把用户困在里面——
		// 记一条日志，界面上仍是登出状态，用户重新扫码一次就好。
		_ = a.mgr.Logout(context.Background())
		return err
	}
	return cli.SeedCookies(httpCookiesOf(a.mgr.Current()))
}

// httpCookiesOf 把落盘的 cookie 变成 net/http 认的形式。
func httpCookiesOf(s Session) []*http.Cookie {
	if len(s.Cookies) == 0 {
		return nil
	}
	out := make([]*http.Cookie, 0, len(s.Cookies))
	for _, c := range s.Cookies {
		out = append(out, c.HTTPCookie())
	}
	return out
}

// BeginLogin 开始一轮扫码登录。
//
// 已经在跑的一轮会被取消——用户点了两次「登录」不该有两个轮询循环
// 同时往 B 站打，那会直接撞上限速。
func (a *App) BeginLogin() {
	acc := a.account
	if acc == nil {
		return
	}
	acc.beginLogin()
}

func (a *Account) beginLogin() {
	a.mu.Lock()
	if a.stop != nil {
		a.stop()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.stop = cancel

	// **代次在这时候拿**。它记的是「用户按下登录那一刻」的登录态版本。
	// 扫码成功要写回时对不上（最常见的是中间退了登），Lease 会挡住。
	lease := a.mgr.BeginRenew()
	st := LoginState{Phase: LoginWaiting, Hint: "正在向 B 站申请二维码…"}
	a.state = st
	a.mu.Unlock()
	a.wakeUp()

	go a.run(ctx, lease)
}

// RefreshLoginCode 换一张二维码。
//
// 过期之后**只能**重新申请（B 站没有「让它再活一会儿」那条路），
// 所以这个方法和「开始一轮」走的是同一条代码。
func (a *App) RefreshLoginCode() { a.BeginLogin() }

// CancelLogin 取消当前这一轮。
func (a *App) CancelLogin() {
	acc := a.account
	if acc == nil {
		return
	}
	acc.cancel()
}

func (a *Account) cancel() {
	a.mu.Lock()
	stop := a.stop
	a.stop = nil
	a.state = LoginState{Phase: LoginIdle}
	a.mu.Unlock()
	if stop != nil {
		stop()
	}
	a.wakeUp()
}

// wakeUp 叫醒界面。回调可能在 setWake 之前还没装上，所以判空。
func (a *Account) wakeUp() {
	a.mu.Lock()
	wake := a.wake
	a.mu.Unlock()
	if wake != nil {
		wake()
	}
}

// run 是那一轮登录的完整流程：申请 → 画 → 循环轮询 → 落盘。
func (a *Account) run(ctx context.Context, lease *Lease) {
	defer lease.Discard() // 没走到 Commit 的路径一律作废代次

	qr, err := a.cli.NewQRCode(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return // 用户取消了，这不是错误
		}
		a.fail("申请二维码失败：" + err.Error())
		return
	}
	code, err := qr.Render()
	if err != nil {
		a.fail("二维码画不出来：" + err.Error())
		return
	}

	a.mu.Lock()
	if a.state.Phase == LoginIdle {
		a.mu.Unlock() // 已经被取消了
		return
	}
	a.state = LoginState{Phase: LoginWaiting, QR: code, Hint: bili.ScanWaiting.String()}
	interval := a.interval
	a.mu.Unlock()
	a.wakeUp()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			res, err := a.cli.PollScan(ctx, qr.Key)
			switch {
			case ctx.Err() != nil:
				return
			case err != nil:
				// 网络抖一下不算失败：**继续等**。
				// 在这里报失败的话，用户在地铁里看到的就是
				// 「登录失败」，而实际上扫完码就好了。
				a.note("连不上 B 站，正在重试…")
				continue
			}
			switch res.State {
			case bili.ScanWaiting:
				a.hint(LoginWaiting, bili.ScanWaiting.String())
			case bili.ScanScanned:
				a.hint(LoginScanned, bili.ScanScanned.String())
			case bili.ScanExpired:
				// 过期是终态：停在这里等用户点刷新。
				a.hint(LoginExpired, bili.ScanExpired.String())
				return
			case bili.ScanConfirmed:
				a.finish(ctx, lease, res)
				return
			}
		}
	}
}

// finish 把扫成功的凭据存进去。
func (a *Account) finish(ctx context.Context, lease *Lease, res bili.ScanResult) {
	sess := Session{RefreshToken: res.RefreshToken}
	if !res.LoggedInAt.IsZero() {
		sess.SavedAt = res.LoggedInAt
	}
	for _, c := range a.cli.SessionCookies() {
		// 只要 B 站自己的 cookie。B 站会顺带下 buvid 之类的东西，
		// 它们不是登录凭据，存进钥匙串只会让「保存了什么」变得不透明。
		if !isBilibiliHost(c.Domain) {
			continue
		}
		sess.Cookies = append(sess.Cookies, CookieFromHTTP(c))
	}
	sess.Normalize(sess.SavedAt)

	if !sess.LoggedIn() {
		// B 站说成功了，jar 里却没有 SESSDATA。两种可能：
		// 接口改了，或者响应被中间层改过。
		// 不落盘——落一份「登不进」的凭据，下次启动会一直 -101，
		// 而用户看到的现象是「一打开就掉登录」。
		a.fail("扫码成功了，但没有拿到登录凭据")
		return
	}

	err := lease.Commit(ctx, func(cur *Session) error {
		*cur = sess
		return nil
	})
	switch {
	case errors.Is(err, ErrStaleGeneration):
		// 用户在这中间退了登。**什么都不写**，安静收工——
		// 这正是要防的那件事：已经退掉的账号被在途的结果写回来。
		log.Printf("biu: 扫码结果回来时登录态已经变了（多半是退登），这次结果作废")
		a.hint(LoginIdle, "已取消登录")
	case err != nil:
		a.fail("保存登录态失败：" + err.Error())
	default:
		a.setState(LoginState{Phase: LoginDone, Hint: "登录成功", LoggedIn: true})
	}
}

// isBilibiliHost 判 cookie 的域是不是 B 站的。
func isBilibiliHost(domain string) bool {
	d := strings.ToLower(strings.TrimPrefix(domain, "."))
	return d == "bilibili.com" || strings.HasSuffix(d, ".bilibili.com")
}

// Logout 退出登录。
//
// 四件事，一件都不能少：
//
//  1. 清系统存储（Manager.Logout）
//  2. 清 cookie jar —— 少了这一步，**客户端还在带着旧 cookie 发请求**，
//     B 站照样认得这个会话，退登等于没退
//  3. 停掉正在跑的扫码轮询
//  4. 代次 +1 —— 让在途的结果写不回来
func (a *App) Logout() error {
	acc := a.account
	if acc == nil {
		return nil
	}
	return acc.logout()
}

func (a *Account) logout() error {
	a.mu.Lock()
	stop := a.stop
	a.stop = nil
	a.state = LoginState{Phase: LoginIdle}
	a.mu.Unlock()
	if stop != nil {
		stop()
	}

	// 先清 jar：Manager.Logout 可能失败（钥匙串锁了），
	// 但**内存里的 cookie 无论如何都得清掉**——
	// 宁可不彻底，也不能让用户退不出去。
	_ = a.cli.SeedCookies(nil)
	err := a.mgr.Logout(context.Background())
	a.wakeUp()
	return err
}

// ─── 改状态的小工具 ───────────────────────────────────────────────────────────

// hint 换阶段和提示，不动二维码。
func (a *Account) hint(phase LoginPhase, hint string) {
	a.mu.Lock()
	st := a.state
	st.Phase, st.Hint = phase, hint
	a.state = st
	a.mu.Unlock()
	a.wakeUp()
}

// note 是「不是失败的那种提示」：网络抖了、正在重试。
//
// 它**不改阶段**——把「等扫码」变成「失败」再变回来，会让界面上
// 那条提示闪一下，而用户根本不知道发生了什么。
func (a *Account) note(text string) {
	a.mu.Lock()
	st := a.state
	st.Hint = text
	a.state = st
	a.mu.Unlock()
	a.wakeUp()
}

// fail 记一次失败。
func (a *Account) fail(reason string) {
	reason = strings.TrimPrefix(reason, "bili: ")
	log.Printf("biu: 登录失败：%s", reason)
	a.setState(LoginState{Phase: LoginFailed, Err: reason, Hint: reason})
}

// Account 返回登录管理器。没挂过返回 nil（界面要判空）。
func (a *App) Account() *Account { return a.account }

// String 让 fmt 能打出可读的状态。
func (s LoginState) String() string {
	return fmt.Sprintf("登录 %s：%s", s.Phase, s.Hint)
}
