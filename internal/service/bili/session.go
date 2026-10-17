// jar 的持有、整体替换，以及「原样记下最近一批 Set-Cookie」。
//
// # 为什么要有这一层
//
// 退登不只是「忘掉登录态」，还得**把客户端身上的 cookie 也清掉**。
// 少了这一步会怎样：退登按钮点了，界面上变成登出状态，
// 但 http.Client 的 jar 里 SESSDATA 还在——下一个请求照样带着它发出去，
// B 站照样认得这个会话。表现为「明明退登了，刷新一下又登回来了」，
// 而更糟的是用户在共享电脑上以为退干净了。
//
// # 为什么不是直接换 `http.Client.Jar`
//
// 因为 `http.Client` 读 `Jar` 字段时**不加锁**。在请求还在飞的时候改它
// 就是数据竞争，`-race` 一定会报；而这件事恰恰发生在「退登时刚好有个
// 取流请求在飞」这种最常见的时刻。
//
// 所以 `http.Client.Jar` 从头到尾只被 set 一次，指向一个
// **可换内容的壳**。换登录态只换壳里面的那个 jar，
// 不碰 `http.Client` 自己。
package bili

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// jarHolder 是 jar 之上的一个可换壳，外加一份「最近下发的 cookie 原样」。
type jarHolder struct {
	mu  sync.RWMutex
	jar http.CookieJar
	// issued 是最近一批 Set-Cookie 的**原样**拷贝。
	//
	// 为什么必须单独留一份：`http.CookieJar` 返回的 cookie **不带 Domain**
	// （Go 的 jar 只填 Name/Value/Quoted，域信息不外露）。拿它去重建
	// jar 的话，所有 cookie 会变成 `www.bilibili.com` 的 host-only cookie，
	// 于是发往 `api.bilibili.com`、`passport.bilibili.com` 的请求
	// 一个都带不上 SESSDATA —— 登录看着成功了，**之后每个接口都 -101**。
	issued []*http.Cookie
	// issuedHost 是 issued 那批 cookie 是从哪个主机收到的。
	issuedHost string
}

// SetCookies 满足 http.CookieJar。
//
// 内部 jar 还没有时**就地建一个**：B 站在你登录之前也会下发一堆
// cookie（buvid 之类），它们不是凭据但要留着。
// 早期版本这里在 jar 为 nil 时直接丢弃，症状极其难查——
// 「B 站明明下了 SESSDATA，jar 里却什么都没有」，而所有代码看起来
// 都对：响应有 Set-Cookie，jar 也在，只是它从来没被初始化过。
func (h *jarHolder) SetCookies(u *url.URL, cookies []*http.Cookie) {
	h.mu.Lock()
	if h.jar == nil {
		fresh, err := cookiejar.New(nil)
		if err != nil {
			// cookiejar.New 只有 opts 为 nil 时才会失败，而 nil 是唯一输入。
			h.mu.Unlock()
			return
		}
		h.jar = fresh
	}
	// 拷贝一份再存。net/http 传给 jar 的是响应头里解析出来的切片，
	// 我们把它留在 jarHolder 里就是替别人持有别人的内存。
	h.issued, h.issuedHost = cloneCookies(cookies), u.Hostname()
	jar := h.jar
	h.mu.Unlock()
	jar.SetCookies(u, cookies)
}

// Cookies 满足 http.CookieJar。
func (h *jarHolder) Cookies(u *url.URL) []*http.Cookie {
	h.mu.RLock()
	jar := h.jar
	h.mu.RUnlock()
	if jar == nil {
		return nil
	}
	return jar.Cookies(u)
}

// issuedFrom 是最近一批 cookie（带域），以及它们是从哪个主机收到的。
func (h *jarHolder) issuedFrom() (cookies []*http.Cookie, host string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return cloneCookies(h.issued), h.issuedHost
}

// reset 换成一个全新的 jar，并塞进 cookies。
//
// **换新的而不是逐条删**：`http.CookieJar` 没有删除接口，
// 逐条删要遍历所有域名并猜测路径，漏一条就是一次「退登没退干净」。
func (h *jarHolder) reset(base *url.URL, cookies []*http.Cookie) error {
	fresh, err := cookiejar.New(nil)
	if err != nil {
		// cookiejar.New 只有 opts 为 nil 时才会失败，而 nil 是唯一输入。
		return err
	}
	if len(cookies) > 0 {
		fresh.SetCookies(base, cookies)
	}
	h.mu.Lock()
	h.jar, h.issued, h.issuedHost = fresh, cloneCookies(cookies), base.Hostname()
	h.mu.Unlock()
	return nil
}

func cloneCookies(in []*http.Cookie) []*http.Cookie {
	if len(in) == 0 {
		return nil
	}
	out := make([]*http.Cookie, 0, len(in))
	for _, c := range in {
		cp := *c
		out = append(out, &cp)
	}
	return out
}

// SeedCookies 装上一份登录 cookie，并把之前的**全部丢掉**。
//
// 三个地方走这条路：
//
//	启动时      从系统存储读回的登录态 → 装上去，「重启后还是登录」落在这里
//	登录成功    刚从 B 站拿回来的 cookie → 装上去
//	退登        传 nil → 一个都不留
func (c *Client) SeedCookies(cookies []*http.Cookie) error {
	return c.jars.reset(c.cookieURL(), cookies)
}

// SessionCookies 读出此刻这个客户端**会真的带上**的那些 cookie。
//
// 读的是 `https://www.bilibili.com/`：B 站的 cookie 域是
// `.bilibili.com`，任何 `*.bilibili.com` 的地址读到的都是同一批。
//
// ⚠️ **这里读出来的 cookie 不带 Domain**（Go 的 jar 不外露域信息）。
// 要拿能重建 jar 的那份，用 `IssuedCookies`。
func (c *Client) SessionCookies() []*http.Cookie {
	return c.jars.Cookies(c.cookieURL())
}

// IssuedCookies 是 B 站最近一次下发的 cookie **原样**，带域、带路径。
//
// 「登录成功之后要存什么」走的是它而不是 `SessionCookies`：
// jar 那份没有 Domain，拿去重建 jar 会把所有 cookie 变成
// www.bilibili.com 的 host-only cookie，之后发往 api./passport.
// 的请求全部不带 SESSDATA —— 登录显示成功，**下一个接口就 -101**。
//
// 这个坑很难查：登录那一秒一切正常，问题出现在之后第一次用登录态
// 请求的时候，而那已经是另一个功能了。
func (c *Client) IssuedCookies() []*http.Cookie {
	cookies, host := c.jars.issuedFrom()
	if !c.isOwnHost(host) {
		return nil
	}
	return cookies
}

// SelfMid 是当前登录用户的 uid，B 站那边叫 mid。
//
// # 为什么从 cookie 里读，不发一次 `/x/web-interface/nav`
//
// 「我创建的收藏夹」那个接口要 `up_mid`（`CreatedFolders`）。Biu 上游是从
// `useUser` 那个 store 里拿的，而那个 store 是登录时调 nav 接口填的——
// 代价是多一个接口、多一份 fixture、以及登录之后第一次用它之前的一次往返。
//
// `DedeUserID` 这个 cookie 里装的就是 mid，扫码登录成功时 B 站自己下发的。
// 读它是一行、零往返。**没登录时是 0**，而 0 在调用方那里会被挡住不发给
// 接口（见 `CreatedFolders`），界面上是一句「还没登录」，而不是一次 -400。
//
// ⚠️ 认的是**不带哈希后缀的那一个**：B 站同时下发 `DedeUserID` 和
// `DedeUserID__ckMd5`，后者是算过 md5 的，不能当 id 用。
func (c *Client) SelfMid() int64 {
	for _, ck := range c.SessionCookies() {
		if ck.Name != "DedeUserID" {
			continue
		}
		id, err := strconv.ParseInt(ck.Value, 10, 64)
		if err != nil {
			return 0
		}
		return id
	}
	return 0
}

// isOwnHost 判一个主机是不是「这个客户端自己会去聊的主机」。
//
// **不按 cookie 自己的域判**：B 站有时会下 host-only cookie（Domain 为空），
// 按域过滤会把 SESSDATA 一起滤掉，症状是「扫码成功了，但没有拿到登录凭据」。
//
// 判的是「这批 cookie 是不是从我们自己会去的主机上下发来的」——
// 那才是真正的问题：**302 跳到别处时顺手发下来的 cookie 不能收进登录态**。
// 收错的后果是下次登录态里混着一个陌生主机签发的 cookie，
// 而重建 jar 时它会被挂到 B 站的域上。
func (c *Client) isOwnHost(host string) bool {
	if host == "" {
		return false
	}
	for _, base := range []string{c.hostAPI, c.hostPassport, c.hostMember} {
		if u, err := url.Parse(base); err == nil && strings.EqualFold(u.Hostname(), host) {
			return true
		}
	}
	return false
}
