// jar 的持有与整体替换。
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
	"sync"
)

// jarHolder 是 jar 之上的一个可换壳。
type jarHolder struct {
	mu  sync.RWMutex
	jar http.CookieJar
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
	h.jar = fresh
	h.mu.Unlock()
	return nil
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

// SessionCookies 读出此刻这个客户端身上有效的 B 站 cookie。
//
// 读的是 `https://www.bilibili.com/`：B 站的 cookie 域是
// `.bilibili.com`，任何 `*.bilibili.com` 的地址读到的都是同一批。
//
// ⚠️ 读 jar 而不是读响应：Set-Cookie 是自动的，jar 已经是「此刻这个
// 客户端身上有效的全部登录凭据」，从它读才和之后发出去的请求一致。
func (c *Client) SessionCookies() []*http.Cookie {
	return c.jars.Cookies(c.cookieURL())
}
