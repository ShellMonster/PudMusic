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
	// runID 是「当前是第几轮登录」。每次开始或取消都 +1。
	//
	// 它的用处是让**上一轮的 goroutine 闭嘴**：取消只是把 ctx 掐掉，
	// 而那一次在途的请求回来之后，照样会走进 `run` 里改状态——
	// 于是用户点了「取消」，界面过一会儿又变回「等扫码」。
	//
	// 单靠 `ctx.Err()` 挡不住：检查和写状态之间还有时间窗，
	// 而那个窗口正好落在「用户点取消」的那一刻。
	runID uint64

	wake func()

	// onLogin 是「登录成了」那一下要通知出去的那一个。
	//
	// ⚠️ **这一层不认识收藏夹**：登录只管登录态，而「登录之后顺手把侧栏的
	// 收藏夹拉一次」是应用层的事（见 `App.AutoRefreshFavorites`）。所以这里
	// 留的是一个回调，而不是直接去调收藏夹那一摊——那样这一层就得知道
	// 「收藏夹那一摊挂上没有」，而它挂不挂跟登录一点关系都没有。
	onLogin func()
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
	// 登录成了要顺手拉一次侧栏的收藏夹（见 `App.AutoRefreshFavorites`）。
	//
	// ⚠️ **这个回调是在登录成功的那一刻才调的**，而收藏夹那一摊（`MountCollections`）
	// 在生产上是**后**挂的——没关系：回调调的是运行期的事，不是挂载期的事。
	acc.onLogin = a.onLoginSucceeded
	a.account = acc

	if err := acc.restore(cli); err != nil {
		log.Printf("biu: 读不回登录态，先按没登录处理：%v", err)
	}
}

// onLoginSucceeded 是「登录成了」那一下。
//
// 登录之后顺手把侧栏的收藏夹拉一次：用户不该为了看见自己的收藏夹，先去头像
// 下拉里按「刷新数据」——那是一个没人会自己想到的操作，而侧栏上长期空着
// 会被当成「我没有收藏夹」。
func (a *App) onLoginSucceeded() {
	a.AutoRefreshFavorites()
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
	if cli == nil {
		// 没有客户端就没有地方装 cookie。这条路只有测试会走
		//（MountLogin(store, nil)），但**不许**让它 panic：
		// 从这里 panic 出去会是在界面线程上，整个进程没了。
		return errors.New("app: 没有 B 站客户端，读回来的登录态装不进去")
	}
	return cli.SeedCookies(httpCookiesOf(a.mgr.Current()))
}

// Close 停掉在途的轮询。App.Close 调它。
//
// 不调也能靠进程退出收拾掉，但那样「退出应用」到「进程真的没了」之间
// 还会一直往 B 站打请求——而且退出前那一刻用户点了一下登录的话，
// 那个 goroutine 会一直等在 ctx 上。
func (a *Account) Close() {
	a.mu.Lock()
	stop := a.stop
	a.stop = nil
	a.mu.Unlock()
	if stop != nil {
		stop()
	}
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
	a.runID++
	id := a.runID

	// **代次在这时候拿**。它记的是「用户按下登录那一刻」的登录态版本。
	// 扫码成功要写回时对不上（最常见的是中间退了登），Lease 会挡住。
	lease := a.mgr.BeginRenew()
	a.state = LoginState{Phase: LoginWaiting, Hint: "正在向 B 站申请二维码…"}
	a.mu.Unlock()
	a.wakeUp()

	go a.run(ctx, lease, id)
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
	// +1 让在途的那一轮闭嘴：它回来之后写的任何状态都会被挡掉。
	a.runID++
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
func (a *Account) run(ctx context.Context, lease *Lease, id uint64) {
	defer lease.Discard() // 没走到 Commit 的路径一律作废代次

	qr, err := a.cli.NewQRCode(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return // 用户取消了，这不是错误
		}
		a.fail(id, "申请二维码失败："+err.Error())
		return
	}
	code, err := qr.Render()
	if err != nil {
		a.fail(id, "二维码画不出来："+err.Error())
		return
	}

	a.mu.Lock()
	if a.runID != id {
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
				a.note(id, "连不上 B 站，正在重试…")
				continue
			}
			switch res.State {
			case bili.ScanWaiting:
				a.hint(id, LoginWaiting, bili.ScanWaiting.String())
			case bili.ScanScanned:
				a.hint(id, LoginScanned, bili.ScanScanned.String())
			case bili.ScanExpired:
				// 过期是终态：停在这里等用户点刷新。
				a.hint(id, LoginExpired, bili.ScanExpired.String())
				return
			case bili.ScanConfirmed:
				a.finish(ctx, lease, res, id)
				return
			}
		}
	}
}

// finish 把扫成功的凭据存进去。
func (a *Account) finish(ctx context.Context, lease *Lease, res bili.ScanResult, id uint64) {
	sess := Session{RefreshToken: res.RefreshToken}
	if !res.LoggedInAt.IsZero() {
		sess.SavedAt = res.LoggedInAt
	}
	// IssuedCookies 已经只包含 B 站自己主机上下发的那一批（见 session.go
	// 里 isBilibiliHost 的注释：按 cookie 自己的域过滤会把登录凭据滤掉）。
	// 这里全都收下——SESSDATA / bili_jct / DedeUserID 三个都在里面，
	// 少一个都会让「登录成功之后第一个要用的接口」失败。
	for _, c := range a.cli.IssuedCookies() {
		sess.Cookies = append(sess.Cookies, CookieFromHTTP(c))
	}
	sess.Normalize(sess.SavedAt)

	if !sess.LoggedIn() {
		// B 站说成功了，jar 里却没有 SESSDATA。两种可能：
		// 接口改了，或者响应被中间层改过。
		// 不落盘——落一份「登不进」的凭据，下次启动会一直 -101，
		// 而用户看到的现象是「一打开就掉登录」。
		a.fail(id, "扫码成功了，但没有拿到登录凭据")
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
		a.hint(id, LoginIdle, "已取消登录")
	case StoreUnavailable(err):
		// 登录**是真的**（B 站认了、cookie 也拿到了），只是存不下来。
		//
		// 报「保存登录态失败」会让用户觉得刚才那一下白扫了——于是他再扫一次，
		// 还是同样的结果。正确的做法是让他登进去用着，同时**明说**存不住：
		// 界面上那一行「凭据后端不可用，登录态关掉应用就没了」就是干这个的。
		log.Printf("biu: 这次登录没能存下来，先只在内存里用着：%v", err)
		a.mgr.Adopt(sess)
		a.setStateFor(id, LoginState{
			Phase: LoginDone,
			Hint:  "登录成功（这次没能存下来，关掉应用要重新登录）",
			Err:   err.Error(),
		})
		a.notifyLogin()
	case err != nil:
		a.fail(id, "保存登录态失败："+err.Error())
	default:
		a.setStateFor(id, LoginState{Phase: LoginDone, Hint: "登录成功", LoggedIn: true})
		a.notifyLogin()
	}
}

// notifyLogin 告诉应用层「登录成了」。
//
// ⚠️ **只在这一层知道成没成**：登录态存没存住、会话有没有被换掉，
// 这一层最清楚；而「登录之后该拉哪些数据」是应用层的决定。所以这里只发
// 一个信号，不替应用层决定拉什么。
//
// ⚠️ **判空**：没装回调时什么都不做（`MountLogin` 装上，但别的路径可能没有）。
func (a *Account) notifyLogin() {
	a.mu.Lock()
	fn := a.onLogin
	a.mu.Unlock()
	if fn != nil {
		fn()
	}
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
	err := acc.logout()
	// 🔴 **播放记录上报要停**（见 playreport.go「二、退登与换人要停」）。
	//
	// 不停的话那一条 goroutine 还会每 30 秒往 B 站打一次心跳，而 jar 刚被
	// 清空——B 站看到的是一次次匿名请求。更要紧的是**换人登录**之后它还在
	// 发：那些进度会被记到**新登录的那个账号**名下，而那是他没听过的歌。
	a.mu.Lock()
	rep := a.report
	a.mu.Unlock()
	if rep != nil {
		rep.stopAccount()
	}
	// 🔴 **侧栏那两组收藏夹也清掉**（见 `Favorites.clear` 的注释）：它们是
	// **上一个账号**的。退登之后还摆着的话，换人登录那几秒里新用户看到的是
	// 别人的收藏夹名。
	a.FavoritesOf().clear()
	return err
}

func (a *Account) logout() error {
	a.mu.Lock()
	stop := a.stop
	a.stop = nil
	a.runID++
	a.state = LoginState{Phase: LoginIdle}
	a.mu.Unlock()
	if stop != nil {
		stop()
	}

	// 先让 B 站那边把这个会话也作废，**然后**再清本地的。
	//
	// 顺序不能反：注销请求自己也要带 SESSDATA 和 csrf，
	// jar 先清了就变成一次匿名请求，服务端的会话原封不动地留着。
	//
	// 只清本地不留服务端会话也不是「没事」：
	// 同一台机器上的另一个 B 站客户端仍然看得见这个会话在册，
	// 而用户在这边看到的界面是「已经退出了」。
	//
	// 失败**不往上传**：本地已经清干净了，把一个远端的失败报给用户
	// 只会让他以为退登没成功，而下次启动照样是登出状态。
	// 这里给它 2 秒预算：网络正常时是百来毫秒，断网时最多卡 2 秒，
	// 不会把「点退出登录」变成一次看起来像卡死的等待。
	if a.cli != nil {
		ctx, cancel := context.WithTimeout(context.Background(), serverLogoutBudget)
		a.cli.Logout(ctx)
		cancel()
	}

	// 然后清 jar：Manager.Logout 可能失败（钥匙串锁了），
	// 但**内存里的 cookie 无论如何都得清掉**——
	// 宁可不彻底，也不能让用户退不出去。
	_ = a.cli.SeedCookies(nil)
	err := a.mgr.Logout(context.Background())
	a.wakeUp()
	return err
}

// serverLogoutBudget 是「让服务端作废这个会话」的时间上限。
//
// 退登是用户点了就要看到结果的操作，所以这一步不能无限等。
// 2 秒是个折中：正常的 B 站响应在 100~300 ms，
// 而断网/被墙时也不会把界面冻住超过两秒。
const serverLogoutBudget = 2 * time.Second

// ─── 改状态的小工具 ───────────────────────────────────────────────────────────
//
// ⚠️ 这几个函数**全都带 runID**，而且对不上就**什么都不做**。
//
// 原因：那一次在途的请求回来的时候，用户可能早就点了取消或者退登、
// 或者又点了一次登录。这时候旧的一轮如果照样写状态，
// 界面上就会出现「点了取消，过一会儿又变回等扫码」
// 或者「刷新之后，旧二维码又被画回来了」——
// 而且它写完就结束了，新的一轮随后也写一次，用户看到的是闪一下。
//
// 不写 id 就得靠调用方每次自己查 `ctx.Err()`，而查和写之间
// 还是有窗口——而那个窗口正好是用户点按钮的那一刻。

// hint 换阶段和提示，不动二维码。
func (a *Account) hint(id uint64, phase LoginPhase, hint string) {
	a.mu.Lock()
	if a.runID != id {
		a.mu.Unlock()
		return
	}
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
func (a *Account) note(id uint64, text string) {
	a.mu.Lock()
	if a.runID != id {
		a.mu.Unlock()
		return
	}
	st := a.state
	st.Hint = text
	a.state = st
	a.mu.Unlock()
	a.wakeUp()
}

// fail 记一次失败。
func (a *Account) fail(id uint64, reason string) {
	reason = strings.TrimPrefix(reason, "bili: ")
	log.Printf("biu: 登录失败：%s", reason)
	a.setStateFor(id, LoginState{Phase: LoginFailed, Err: reason, Hint: reason})
}

// setStateFor 写状态，但只在这一轮还是当前那一轮时才写。
func (a *Account) setStateFor(id uint64, st LoginState) {
	a.mu.Lock()
	if a.runID != id {
		a.mu.Unlock()
		return
	}
	a.state = st
	a.mu.Unlock()
	a.wakeUp()
}

// Account 返回登录管理器。没挂过返回 nil（界面要判空）。
func (a *App) Account() *Account { return a.account }

// LoginState 是界面画登录页要读的那份拷贝。没挂登录时是 LoginIdle。
func (a *App) LoginState() LoginState {
	acc := a.account
	if acc == nil {
		return LoginState{Phase: LoginIdle}
	}
	return acc.LoginState()
}

// LoggedIn 报此刻有没有登录态。没挂登录时是 false。
func (a *App) LoggedIn() bool {
	acc := a.account
	return acc != nil && acc.LoggedIn()
}

// Session 是当前登录态。没登录时 LoggedIn() 是 false。
func (a *App) Session() Session {
	acc := a.account
	if acc == nil {
		return Session{}
	}
	return acc.Session()
}

// CredentialBackendName 是凭据后端的名字，写诊断信息用。
func (a *App) CredentialBackendName() string {
	acc := a.account
	if acc == nil {
		return "none"
	}
	return acc.BackendName()
}

// SetLoginPollInterval 改扫码轮询的间隔。
//
// ⚠️ **只给测试用**：真等待就 2 秒。存在的理由是那 2 秒没法写进测试
// ——「等了 2 秒」既慢又不稳定，而这条逻辑的三态、退登竞速都要跑很多遍。
func (a *App) SetLoginPollInterval(d time.Duration) {
	if acc := a.account; acc != nil {
		acc.SetPollInterval(d)
	}
}

// String 让 fmt 能打出可读的状态。
func (s LoginState) String() string {
	return fmt.Sprintf("登录 %s：%s", s.Phase, s.Hint)
}
