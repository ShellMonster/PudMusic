// 凭据的存储接口、注入边界和脱敏边界。
//
// 这一阶段**只有接口和纯逻辑，没有平台后端**（macOS Keychain / Windows DPAPI
// 是阶段 3 的 B14）。接口定错一次，后面扫码登录、续期、退出、多窗口同步、
// 诊断导出全都要跟着返工，所以宁可现在把边界写死。
//
// # 为什么要单独立一层（Codex M8）
//
// 标准 `net/http/cookiejar` 只解决「运行中怎么管理 Cookie」。它不管：
//
//   - **重启后还能不能登录**。jar 在内存里，进程一退就没了。所以必须有一份
//     能落盘的 Session（含 cookie + SESSDATA + bili_jct + refresh_token），
//     启动时读回来重建 jar——见 Session.Jar。
//   - **凭据放哪**。明文放配置文件，拷一份给别人就等于把账号交出去。
//     Biu 就是明文（electron-store 加了个 encryptionKey，实测只是混淆，
//     `biu/electron/store.ts:23-27`），**那不构成必须照抄的体验合同**。
//   - **谁能拿到凭据**。歌词源、封面 CDN、B 站写 API、updater 不是同一
//     凭据边界，见 InjectPolicy。
//   - **日志里能出现什么**。Cookie、签名 URL、代理密码一律不许，见 Redactor。
//
// # 错误语义
//
// 调用方要能分清这三件事，因为处理方式完全不同：
//
//	ErrNotLoggedIn     没登录过。这是**正常状态**，不是故障，不该弹错误、
//	                   不该写日志、不该重试。退登之后也是它。
//	ErrBackend         后端不可用（钥匙串被锁、DPAPI 用户变了、权限不对）。
//	                   该提示用户、该考虑让用户手动重登，但不该当成「已退登」。
//	ErrCorrupt         存的东西坏了/解不开。该提示并**按退登处理**——解不开的
//	                   凭据不能当没登录继续用，也不能无限重试。
//	ErrUnavailable     这个平台还没有可用后端（阶段 3 之前就是这个状态）。
//
// 三个都用 errors.Is 判。带路径、带原因的具体错误一律 wrap 上，
// 不要用 == 判，也不要重新造一个同义的错误。
package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// 凭据相关的哨兵错误。语义见包注释。
var (
	ErrNotLoggedIn = errors.New("app: 没有登录")
	ErrBackend     = errors.New("app: 凭据后端不可用")
	ErrCorrupt     = errors.New("app: 凭据数据已损坏")
	ErrUnavailable = errors.New("app: 当前平台没有可用的凭据后端")

	// ErrStaleGeneration：在途的异步续期想把结果写回来，但它开始之后
	// 登录态已经变了（最常见的是用户在这中间退了登）。见 Lease。
	ErrStaleGeneration = errors.New("app: 登录态已经变了，这次续期的结果作废")
)

// CookieName 是几个必须单独拿的名字。
//
// 只认这几个名字，其他 cookie 一律当普通 cookie 走 jar。
const (
	CookieSESSDATA   = "SESSDATA"
	CookieBiliJCT    = "bili_jct"
	CookieDedeUserID = "DedeUserID"
	// CSRFHeaderName 是写接口要带的 CSRF 头。
	CSRFHeaderName = "bili-jct"
)

// Cookie 是一条落盘的 Cookie。
//
// 不用 `http.Cookie`：它的 JSON 标签不全（Raw/Quoted 之类会丢），
// SameSite 是枚举 int，写出来读回去容易变味。
type Cookie struct {
	Name     string    `json:"name"`
	Value    string    `json:"value"`
	Domain   string    `json:"domain"`
	Path     string    `json:"path"`
	Expires  time.Time `json:"expires,omitempty"`
	Secure   bool      `json:"secure,omitempty"`
	HTTPOnly bool      `json:"httpOnly,omitempty"`
	SameSite string    `json:"sameSite,omitempty"`
}

// HTTPCookie 转成 net/http 认识的形式。SameSite 认不出的值当没写。
func (c Cookie) HTTPCookie() *http.Cookie {
	hc := &http.Cookie{
		Name:     c.Name,
		Value:    c.Value,
		Domain:   c.Domain,
		Path:     c.Path,
		Expires:  c.Expires,
		Secure:   c.Secure,
		HttpOnly: c.HTTPOnly,
	}
	switch strings.ToLower(c.SameSite) {
	case "lax":
		hc.SameSite = http.SameSiteLaxMode
	case "strict":
		hc.SameSite = http.SameSiteStrictMode
	case "none":
		hc.SameSite = http.SameSiteNoneMode
	}
	return hc
}

// CookieFromHTTP 把 Set-Cookie 头收下来的 cookie 转成落盘形式。
func CookieFromHTTP(hc *http.Cookie) Cookie {
	c := Cookie{
		Name:     hc.Name,
		Value:    hc.Value,
		Domain:   hc.Domain,
		Path:     hc.Path,
		Expires:  hc.Expires,
		Secure:   hc.Secure,
		HTTPOnly: hc.HttpOnly,
	}
	switch hc.SameSite {
	case http.SameSiteLaxMode:
		c.SameSite = "lax"
	case http.SameSiteStrictMode:
		c.SameSite = "strict"
	case http.SameSiteNoneMode:
		c.SameSite = "none"
	}
	if c.Path == "" {
		c.Path = "/"
	}
	return c
}

// Session 是一份完整的登录态。
//
// SESSDATA / BiliJCT / RefreshToken 三个字段和 Cookies 里的是同一份东西，
// 单独再存一份是为了让读的人一眼能看见、也不必遍历 cookie。写的时候
// 以 Cookies 为准（见 Normalize），别只改其中一个字段。
type Session struct {
	Cookies      []Cookie `json:"cookies"`
	SESSDATA     string   `json:"SESSDATA"`
	BiliJCT      string   `json:"bili_jct"`
	DedeUserID   string   `json:"DedeUserID"`
	RefreshToken string   `json:"refresh_token"`
	// NextCheckRefreshTime 对应 Biu 的 `nextCheckRefreshTime`：
	// 下次该拿 refresh_token 去换 cookie 的时刻。零值表示「不知道，尽快试」。
	NextCheckRefreshTime time.Time `json:"nextCheckRefreshTime"`
	// SavedAt 是这份登录态存下来的时刻。诊断、过期判断、续期退避都用得上。
	SavedAt time.Time `json:"savedAt"`
}

// LoggedIn 报这份登录态能不能算「已登录」。
//
// 判据是 SESSDATA 在不在——只有它能单独表示一个有效的 B 站会话。
// bili_jct 没有也能读公开数据，但写接口不行，所以不能拿它当依据。
func (s Session) LoggedIn() bool { return s.SESSDATA != "" }

// Normalize 让单独的那几个字段和 Cookies 对齐。
//
// 谁是谁由 Cookies 说了算：SESSDATA 是权威，CSRF 值必须等于当前那条
// bili_jct（拿旧的 bili_jct 配新的 SESSDATA，写接口会被拒，而且报的是
// 「风控」不是「CSRF 错了」，极难查）。
func (s *Session) Normalize(now time.Time) {
	for _, c := range s.Cookies {
		switch c.Name {
		case CookieSESSDATA:
			s.SESSDATA = c.Value
		case CookieBiliJCT:
			s.BiliJCT = c.Value
		case CookieDedeUserID:
			s.DedeUserID = c.Value
		}
	}
	if s.SavedAt.IsZero() {
		s.SavedAt = now
	}
}

// Jar 把这份登录态变成一个 cookiejar，接给 http.Client 用。
//
// **这就是「重启后恢复登录」的落点**：启动时从 Store 读回 Session，
// NewClient 拿 Session.Jar() 装上去，此后所有请求的 Cookie 都自动对。
func (s Session) Jar() http.CookieJar {
	jar, err := cookiejar.New(nil)
	if err != nil {
		// cookiejar.New 只有 opts 为 nil 时才会失败，而 nil 是唯一的输入。
		panic("app: cookiejar.New(nil) 失败: " + err.Error())
	}
	u, err := url.Parse("https://www.bilibili.com/")
	if err != nil {
		panic("app: 解析 bilibili.com 失败: " + err.Error())
	}
	for _, c := range s.Cookies {
		jar.SetCookies(u, []*http.Cookie{c.HTTPCookie()})
	}
	return jar
}

// Secrets 是这份登录态里所有不许出现在日志里的片段。
//
// 顺序按长度降序：短的可能 是长的前缀，先替换短会把长的切成两半。
func (s Session) Secrets() []string {
	out := make([]string, 0, 4)
	for _, v := range []string{s.SESSDATA, s.BiliJCT, s.RefreshToken} {
		if len(v) >= 8 { // 太短的值脱敏了也没意义，还容易把正常文本打成马赛克
			out = append(out, v)
		}
	}
	for _, c := range s.Cookies {
		if c.Name == CookieDedeUserID {
			// DedeUserID 是**公开**的数字账号标识——B 站每个账号的页面上
			// 都印着它，它不是凭据。
			//
			// 不排掉它的后果不是泄露，是**诊断信息变得没用**：
			// 它有 8~10 位，够得上「长到值得脱敏」的门槛，于是每一次
			// 导出诊断信息里的 userId 都是 <redacted>，而那个字段本来
			// 就是给排障的人看的（「用户报的问题是不是这个账号」）。
			continue
		}
		if len(c.Value) >= 8 && c.Value != s.SESSDATA && c.Value != s.BiliJCT {
			out = append(out, c.Value)
		}
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

// Store 是凭据的落地后端。阶段 3 实现 macOS Keychain / Windows DPAPI。
//
// 实现必须守住的三条：
//
//  1. **Load 在没有登录时返回 ErrNotLoggedIn**，不是别的。它是正常状态。
//  2. **Clear 幂等**：本来就没登录也返回 nil。退登按钮连点两次不能报错。
//  3. **不要落明文**。不是「尽量别落」，是这条接口存在的意义。
//     后端自己决定密文格式，本包不关心也不该关心。
type Store interface {
	// Name 是后端的名字（"macos-keychain"、"windows-dpapi"…）。
	// 写进诊断信息，不含任何秘密。
	Name() string

	// Load 读出已保存的登录态。
	Load(ctx context.Context) (Session, error)

	// Save 覆盖保存。没登录过的时候也允许（首次扫码登录就是这条路）。
	Save(ctx context.Context, s Session) error

	// Clear 清掉全部凭据。幂等。
	Clear(ctx context.Context) error
}

// MemoryStore 是内存后端，给测试和「还没接平台后端」时用。
//
// ⚠️ **它不是安全的**：进程退出凭据就没了，dump 内存能拿到。
// 生产代码不许用，只在阶段 3 落地前让整条链路能跑起来。
type MemoryStore struct {
	mu   sync.Mutex
	name string
	cur  Session
	has  bool
}

// NewMemoryStore 建一个内存后端。
func NewMemoryStore(name string) *MemoryStore {
	if name == "" {
		name = "memory"
	}
	return &MemoryStore{name: name}
}

func (m *MemoryStore) Name() string { return m.name }

func (m *MemoryStore) Load(context.Context) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.has || !m.cur.LoggedIn() {
		return Session{}, ErrNotLoggedIn
	}
	return m.cur, nil
}

func (m *MemoryStore) Save(_ context.Context, s Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s.Normalize(time.Now())
	m.cur, m.has = s, true
	return nil
}

func (m *MemoryStore) Clear(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cur, m.has = Session{}, false
	return nil
}

// Manager 守着当前登录态，负责两件后端不管的事：
// 代次（防在途续期写回已退登的账号）和给网络层用的 jar。
//
// 零值不可用，用 NewManager。
type Manager struct {
	store Store

	mu  sync.Mutex
	cur Session
	// gen 是登录态的代次。每次换登录态（登录、续期、退登）都涨一。
	// 在途的异步操作拿着开始时的代次，回来时对不上就说明结果已经过时。
	gen uint64
}

// NewManager 建一个管理器，还没登录。
//
// **store 传 nil 是允许的**，会换成一个「哪儿都存不住」的后端。
//
// 为什么允许：这个平台可能压根没有系统保护存储（非 macOS / Windows），
// 或者符号绑不上。调用方拿到的 `NewSystemStore` 错误本来就是 nil 后端，
// 而应用层要做的是「降级 + 在界面上明说」，不是**启动就崩**。
//
// 不在这里兜住的话，`Manager.Load` 第一次 `m.store.Load(ctx)` 就是
// 空接口上的方法调用 —— SIGSEGV，整进程没了，而用户什么都没做错。
func NewManager(store Store) *Manager {
	if store == nil {
		store = unavailableStore{}
	}
	return &Manager{store: store}
}

// unavailableStore 是「这个平台存不住凭据」的后端。
//
// 三个方法的行为不一样，是刻意的：
//
//	Load   ErrUnavailable —— 调用方据此知道「读不回来是因为没有后端」，
//	                        而不是把它当成「没登录过」安静咽下去。
//	Clear  nil           —— 本来就没有东西，清不清都一样。
//	                        退登必须能成功，不然用户会被卡在登出状态。
//	Save   ErrUnavailable —— 不假装存成功了。
type unavailableStore struct{}

func (unavailableStore) Name() string { return "none" }

func (unavailableStore) Load(context.Context) (Session, error) {
	return Session{}, fmt.Errorf("%w: 这个平台没有可用的凭据后端", ErrUnavailable)
}

func (unavailableStore) Save(context.Context, Session) error {
	return fmt.Errorf("%w: 这个平台没有可用的凭据后端", ErrUnavailable)
}

func (unavailableStore) Clear(context.Context) error { return nil }

// BackendName 是后端的名字，写诊断信息用。
func (m *Manager) BackendName() string { return m.store.Name() }

// Load 从后端把登录态读回来。
//
// **启动时必须调一次**，否则重启后是登出状态。ErrNotLoggedIn 不算失败：
// 没登录过是正常起点，静默忽略就行。
func (m *Manager) Load(ctx context.Context) error {
	s, err := m.store.Load(ctx)
	if err != nil {
		return err
	}
	s.Normalize(time.Now())
	m.mu.Lock()
	m.cur, m.gen = s, m.gen+1
	m.mu.Unlock()
	return nil
}

// Current 是当前登录态。没有登录时 LoggedIn() 是 false，其余字段无意义。
func (m *Manager) Current() Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cur
}

// Generation 是当前代次。
func (m *Manager) Generation() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gen
}

// Jar 给网络层用。返回的 jar 是当前登录态的快照；之后登录态变了要重新取。
func (m *Manager) Jar() http.CookieJar {
	return m.Current().Jar()
}

// Update 改登录态并立刻存到后端，代次 +1。
//
// 续期拿到新 cookie、用户改了设置，都走这里。存失败就整体回滚并返回错误：
// 内存里留着一个没落盘的登录态，下次启动就登出，这种「看起来成功了」的
// 半成品最难查。
//
// ⚠️ 存盘期间一直拿着 m.mu——这正是「要么全改要么全没改」的前提。
// 代价是存盘期间 Current()/Generation() 一起等；钥匙串写很快，可以接受。
// **Store 的实现不许回调 Manager**，那会死锁。
func (m *Manager) Update(ctx context.Context, fn func(*Session) error) error {
	m.mu.Lock()
	next := m.cur
	err := fn(&next)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	next.Normalize(time.Now())
	if err := m.store.Save(ctx, next); err != nil {
		m.mu.Unlock()
		return fmt.Errorf("app: 保存登录态: %w", err)
	}
	m.cur, m.gen = next, m.gen+1
	m.mu.Unlock()
	return nil
}

// Adopt 把登录态装进内存，但**不落盘**。
//
// 只有一个用途：这个平台压根没有系统保护存储，或者后端此刻不可用
// （钥匙串锁了、DPAPI 解不开）。这时候这次登录是**真的**——B 站认了、
// cookie 也拿到了——只是留不到下次启动。
//
// 与其让用户「扫了半天码换来一句保存失败」然后这次登录也白扫，
// 不如让他登进去用着，同时**明说**存不住（界面上是
// 「凭据后端不可用，登录态关掉应用就没了」）。
func (m *Manager) Adopt(s Session) {
	s.Normalize(time.Now())
	m.mu.Lock()
	m.cur, m.gen = s, m.gen+1
	m.mu.Unlock()
}

// StoreUnavailable 报「这次没能存下来」的错误。
//
// 判据是 `ErrUnavailable` 和 `ErrBackend` 这两个**后端**级的错误，
// 不含 `ErrStaleGeneration`（那是「登录态已经变了」，处理方式完全不同）。
func StoreUnavailable(err error) bool {
	return err != nil && (errors.Is(err, ErrUnavailable) || errors.Is(err, ErrBackend))
}

// Logout 退登：清后端、清内存、代次 +1。
//
// 代次 +1 是重点：这一刻还在飞的续期请求回来时对不上，会被 Lease 挡掉，
// **不会把已经退掉的账号又写回来**（`PLAN.md` 阶段 3 验收第 2 条）。
//
// 后端清不掉时内存也会清（返回错误），宁可不彻底也不能让用户退不出去。
func (m *Manager) Logout(ctx context.Context) error {
	m.mu.Lock()
	m.cur, m.gen = Session{}, m.gen+1
	m.mu.Unlock()
	if err := m.store.Clear(ctx); err != nil {
		return fmt.Errorf("app: 退出登录: %w", err)
	}
	return nil
}

// Lease 是一次「在途的异步操作」的凭据：续期、扫码轮询、用户信息刷新。
//
// 拿的时候记下代次，回来提交时对不上就作废。这是防「旧结果覆盖新状态」
// 的唯一办法——单靠一把锁挡不住，因为等锁的时候结果早就算好了。
//
// 一次操作一个 Lease，不复用。忘了 Discard 只是代次白涨一次，没有别的后果。
type Lease struct {
	m   *Manager
	gen uint64
}

// BeginRenew 拿一张租约，声明「我要在后台动登录态了」。
func (m *Manager) BeginRenew() *Lease {
	m.mu.Lock()
	defer m.mu.Unlock()
	return &Lease{m: m, gen: m.gen}
}

// Commit 把续期结果写回去。登录态在这期间变过就返回 ErrStaleGeneration，
// **不写**。调用方看到这个错误就直接收工，不要重试。
func (l *Lease) Commit(ctx context.Context, fn func(*Session) error) error {
	l.m.mu.Lock()
	if l.m.gen != l.gen {
		l.m.mu.Unlock()
		return ErrStaleGeneration
	}
	next := l.m.cur
	err := fn(&next)
	if err != nil {
		l.m.mu.Unlock()
		return err
	}
	next.Normalize(time.Now())
	if err := l.m.store.Save(ctx, next); err != nil {
		l.m.mu.Unlock()
		return fmt.Errorf("app: 保存续期结果: %w", err)
	}
	l.m.cur, l.m.gen = next, l.m.gen+1
	l.m.mu.Unlock()
	return nil
}

// Discard 放弃这次操作。什么都写不了，只把代次退回去——
// 没有别人在等这个代次时，留着让后续操作全部作废才是错的。
func (l *Lease) Discard() {
	l.m.mu.Lock()
	if l.m.gen == l.gen {
		l.m.gen++
	}
	l.m.mu.Unlock()
}

// ─────────────────────────────────────────────────────────────────────────────
// 注入边界：这个请求能从凭据存储里拿到什么
// ─────────────────────────────────────────────────────────────────────────────

// CredentialClass 是一个请求的凭据级别。
type CredentialClass int

const (
	// CredNone 一律不带。歌词源、封面以外的陌生主机、自动更新都在这一档。
	CredNone CredentialClass = iota
	// CredRefererOnly 只带 Referer / User-Agent 这类「我不是来偷 Cookie 的」
	// 声明。B 站 CDN 要这个，不给就 403。
	CredRefererOnly
	// CredBiliFull 完整 Cookie + CSRF。只有 B 站读写 API 在这一档。
	CredBiliFull
)

// String 是给日志和测试断言看的。**不要拿它做解析**。
func (c CredentialClass) String() string {
	switch c {
	case CredNone:
		return "none"
	case CredRefererOnly:
		return "referer-only"
	case CredBiliFull:
		return "bilibili-full"
	}
	return "unknown"
}

// InjectPolicy 是一张**白名单**：默认只有这几类主机能拿到凭据，其余一律
// CredNone。白名单之外的扩展要走显式配置，不许靠「看起来像 B 站」判断。
//
// 匹配一律在 **url.Parse 之后的 hostname** 上做，绝不在原始 URL 字符串上做
// 子串匹配。`https://bilibili.com.evil.net/x` 里含有 "bilibili.com"，
// 字符串匹配会把凭据送给 evil.net；解析出 hostname 就不会。
type InjectPolicy struct {
	// biliHosts 是能拿完整凭据的主机。
	biliHosts []string
	// biliSuffixes 是能拿完整凭据的主机后缀（含前导点，匹配的是
	// `.bilibili.com` 这种完整后缀，不是「以 bilibili.com 结尾」）。
	// ⚠️ 只能填 **注册者自己控制** 的域。填 `.com` 这种就是把自己开了。
	biliSuffixes []string
	// refererHosts 只拿 Referer 的主机，同上两种形式。
	refererHosts    []string
	refererSuffixes []string
}

// DefaultInjectPolicy 是默认那张白名单。
//
// 主机名逐个来自 Biu 源码实际用到的那些（`api.` / `www.` / `passport.` /
// `member.` / `s.search.` / `music.` / `t.`），加两条后缀覆盖 B 站自己的
// 其它子域。歌词的两个源**明确列在 referer 档**里：它们跟 B 站完全无关，
// 明确列出来是为了让「不给它们 B 站凭据」这条规则写在代码里而不是只在心里。
func DefaultInjectPolicy() InjectPolicy {
	return InjectPolicy{
		biliHosts: []string{"bilibili.com"},
		// ⚠️ 只有 bilibili.com 一条。CDN 在 bilivideo / hdslb 那些域上，
		// **不要**为了图省事把 CDN 后缀也塞进这里——那是把完整 Cookie 发给
		// 一台存静态文件的机器。B 站改 CDN 域名的概率远大于新开 API 子域。
		biliSuffixes: []string{".bilibili.com"},
		refererHosts: []string{
			// B 站 CDN：图片、音频。
			"hdslb.com",
			"bilivideo.com",
			"bilivideo.cn",
			// 歌词源——列在这里是为了显式声明「它们只拿 Referer，B 站的
			// Cookie 一个都不给」。删掉它们也还是 CredNone，但规则就不写在
			// 代码里了。
			"music.163.com",
			"lrclib.net",
		},
		refererSuffixes: []string{
			".hdslb.com",
			".bilivideo.com",
			".bilivideo.cn",
			".163.com",
			".lrclib.net",
		},
	}
}

// AllowBiliHost 注册一台额外的主机（用户自己加的 API 服务器等），
// 返回新的策略——InjectPolicy 按值传递，原来的不变。
func (p InjectPolicy) AllowBiliHost(host string) InjectPolicy {
	if h := normHost(host); h != "" {
		p.biliHosts = append(append([]string(nil), p.biliHosts...), h)
	}
	return p
}

// AllowRefererHost 同上，但只给 Referer。
func (p InjectPolicy) AllowRefererHost(host string) InjectPolicy {
	if h := normHost(host); h != "" {
		p.refererHosts = append(append([]string(nil), p.refererHosts...), h)
	}
	return p
}

func normHost(host string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
}

// Classify 判一个 URL 能拿到什么。
//
// 拿不到合法 URL 就当 CredNone——解析失败时**往最严的方向退**，
// 不是往宽松的方向猜。
func (p InjectPolicy) Classify(rawURL string) CredentialClass {
	u, err := url.Parse(rawURL)
	if err != nil {
		return CredNone
	}
	return p.ClassifyHost(u.Hostname())
}

// ClassifyHost 判一个已经剥掉端口的小写主机名能拿到什么。
func (p InjectPolicy) ClassifyHost(host string) CredentialClass {
	host = normHost(host)
	if host == "" {
		return CredNone
	}
	if slices.Contains(p.biliHosts, host) || hasSuffix(p.biliSuffixes, host) {
		return CredBiliFull
	}
	if slices.Contains(p.refererHosts, host) || hasSuffix(p.refererSuffixes, host) {
		return CredRefererOnly
	}
	return CredNone
}

// hasSuffix 里是**带前导点的完整后缀**（`.bilibili.com`），不是「以 … 结尾」——
// 后者会让 `notbilibili.com` 命中，那是个谁都能注册的主机。
func hasSuffix(suffixes []string, host string) bool {
	return slices.ContainsFunc(suffixes, func(s string) bool {
		return strings.HasSuffix(host, s)
	})
}

// Sanitize 按分类把请求上的凭据删干净。
//
// 网络层（`CredentialTransport`）每个请求、**以及每一次 redirect 之后**
// 都要调它一遍——B 站和中间 CDN 有 302 是常态，不重新判就等于把凭据
// 跟着 302 送到了别处。
func (p InjectPolicy) Sanitize(req *http.Request) {
	if req == nil {
		return
	}
	p.SanitizeAs(req, p.Classify(req.URL.String()))
}

// SanitizeAs 按**已经算好的**分类删凭据。
//
// 存在的理由只有一个：`Classify` 只看主机、不看协议，所以「HTTPS 降到
// HTTP 但主机名没变」它判不出来——那一跳主机还是 `api.bilibili.com`，
// 白名单认得，凭据就顺明文链路走了。判协议的那一层在
// `CredentialTransport.classOf` 里，判完之后用这个版本删。
//
// 分类传 CredBiliFull 就什么都不删（该带的已经在 jar 和 CSRF 注入那一步
// 带上了）。别的都删 Cookie / Authorization / Proxy-Authorization。
func (p InjectPolicy) SanitizeAs(req *http.Request, class CredentialClass) {
	if req == nil {
		return
	}
	if class == CredBiliFull {
		return
	}
	// 只给 Referer 的和什么都没有的，Cookie 一律不留。
	//
	// Authorization / Proxy-Authorization 也一起删：它们同样是凭据，
	// 「我们只认 Cookie」这种想法在有代理的机器上会把代理密码发出去。
	req.Header.Del("Cookie")
	req.Header.Del("Authorization")
	req.Header.Del("Proxy-Authorization")
	req.Header.Del(CookieSESSDATA)
	req.Header.Del(CookieBiliJCT)
	req.Header.Del(CSRFHeaderName)
}

// RedirectClass 算跳到 to 之后、凭据该有的级别。
//
// 三条硬规则（`PLAN.md` §3.3）：
//
//  1. **HTTPS 降级到 HTTP 一律清空。** 明文链路上一份凭据都不能留。
//  2. **陌生主机清空。** 不能让 302 把凭据带到别处。
//  3. **白名单内按目标重新判级。** B 站 API → B 站 CDN 是降级，
//     降下来但不能降到零。
//
// 这里**不判断「要不要跳」**——拦不拦重定向是 HTTP 客户端的策略，不是凭据
// 策略的事。这一层只回答「跳过去的时候手上应该剩什么」。
//
// to 为 nil（没有跳转目标）时返回 CredNone。
func (p InjectPolicy) RedirectClass(from, to *url.URL) CredentialClass {
	if to == nil {
		return CredNone
	}
	if from != nil && from.Scheme == "https" && to.Scheme == "http" {
		return CredNone
	}
	return p.ClassifyHost(to.Hostname())
}

// ─────────────────────────────────────────────────────────────────────────────
// 脱敏边界：诊断信息里什么能出现
// ─────────────────────────────────────────────────────────────────────────────

// secretHeaders 是**整个值**不许出现的头。
//
// 这些头的值整体就是凭据或凭据的容器，靠「认出里面的秘密片段」是不可靠的
// （Cookie 可能是分片的、转义的、或者干脆是别人随手起的名字）。见到名字
// 就整个替换掉，不做值匹配。
var secretHeaders = map[string]bool{
	"cookie":              true,
	"set-cookie":          true,
	"authorization":       true,
	"proxy-authorization": true,
	CSRFHeaderName:        true,
	CookieSESSDATA:        true,
	CookieBiliJCT:         true,
}

// secretParams 是 URL 里**值**要打码的查询参数。
//
// B 站的 WBI 签名（`w_rid` + `wts` + `sign`）拼出来的 URL 本身就是一次性的
// 授权凭据：拿着它能在有效期内直接下到那个资源。所以参数名要留着（诊断时
// 「这条请求带没带签名」是重要信息），值必须打掉。
var secretParams = map[string]bool{
	"w_rid":         true,
	"wts":           true,
	"sign":          true,
	"access_key":    true,
	"accessKey":     true,
	"token":         true,
	"auth_code":     true,
	"csrf":          true,
	"refresh_token": true,
	"csrf_token":    true,
	"bili_jct":      true,
	"accessKey2":    true,
	// qrcode_key 是扫码登录那一轮的**一次性授权**：拿着它就能轮询出
	// 这一张二维码对应的凭据。它跟 w_rid 一样属于「拼出来的地址本身就是
	// 凭据」，所以必须按名字打掉——等登录成功后这个 key 就废了，
	// 而开调试日志的恰恰是那种「登录出了问题」的时候。
	"qrcode_key": true,
}

// Redacted 是打码后的样子。固定成一个词，方便 grep 诊断文件里「这里本该有东西」。
//
// 在 URL 里它会显示成百分号编码（%3Credacted%3E），那是 `url.Values.Encode`
// 的标准行为，断言要拿解码后的值比。
const Redacted = "<redacted>"

// Redactor 负责把秘密从要写出去的文本里拿掉。
//
// 两条防线，缺一不可：
//
//	URL(raw)   —— 按参数名打码。认出名字比认出值可靠得多。
//	Text(s)    —— 按值打码，兜住 header 体、日志里手写的字符串、错误信息。
//
// 两条都要，因为只有值匹配的话，签名 URL 里的 `w_rid`（每次都不一样）
// 永远认不出来；只有参数名匹配的话，日志正文里的 SESSDATA 又会漏出去。
//
// Redactor 不是并发安全的：每条日志各自建一个，别共享。
type Redactor struct {
	secrets []string
}

// NewRedactor 建一个，secrets 一般直接来自 Session.Secrets()。
func NewRedactor(secrets ...string) *Redactor {
	return &Redactor{secrets: sortedSecrets(secrets)}
}

// ForSession 是按当前登录态建的。退登之后要重新建一个——旧的还留着旧的秘密。
func ForSession(s Session) *Redactor { return NewRedactor(s.Secrets()...) }

func sortedSecrets(v []string) []string {
	out := append([]string(nil), v...)
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

// URL 打掉秘密查询参数的值，保留参数名。
//
// URL 本身解不出来就整个换成 <redacted>，不原样放行：解析失败往往意味着
// 它长得就不正常，而诊断信息里「有个奇怪的 URL」没有价值，只有「有」和「没有」。
func (r *Redactor) URL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return Redacted
	}
	q := u.Query()
	hit := false
	for k := range q {
		if secretParams[k] {
			q.Set(k, Redacted)
			hit = true
		}
	}
	if hit {
		u.RawQuery = q.Encode()
	}
	return r.Text(u.String())
}

// Header 打一个头出来。名字敏感就整个替换，否则按值打码。
//
// 头名按 **HTTP 规范是大小写不敏感** 的，所以比的是小写。
func (r *Redactor) Header(name, value string) string {
	if secretHeaders[strings.ToLower(name)] {
		return Redacted
	}
	return r.Text(value)
}

// Text 把所有已知秘密片段换成 <redacted>。
//
// 一个片段都注册不了的时候原样返回：没登录的时候日志里的用户名、错误码之类
// 不该莫名其妙变成一堆 <redacted>。
func (r *Redactor) Text(s string) string {
	if r == nil || len(r.secrets) == 0 {
		return s
	}
	for _, sec := range r.secrets {
		s = strings.ReplaceAll(s, sec, Redacted)
	}
	return s
}
