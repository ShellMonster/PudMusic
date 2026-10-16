// WBI 签名：全项目**只有这一份**。
//
// B 站从 2023 年起给一部分接口加了 WBI：请求参数排序后拼成一串，
// 末尾拼上一个「混合密钥」，取 MD5 当签名 `w_rid` 一起发回去。
// 混合密钥由两张固定的乱序表和 `nav` 接口下发的两个 key 算出。
//
// Biu 上游把这段抄了两份（渲染进程一份、主进程一份），
// 改一处漏一处就静默失败——签名对不上时 B 站返回的是 `-403`，
// 报的是「访问权限不足」，跟签名没有任何关系，极难查。
//
// # 静默失败是这一段最大的风险
//
// 签名错了不报错，只是接口拒绝。所以 `TestWBISignatureMatchesTheKnownVector`
// 拿一组算得出来的公开向量钉住它，`TestWBIKeysComeFromNav` 钉住 key 的来源。
// B 站改了算法的话本地 fixture 侦测不到（这是外部协议漂移，
// 本地测试原理上就测不出来），只能靠真机回归——见任务报告。
package bili

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

// mixinKeyEncTab 是打乱 img_key + sub_key 的那张表。
//
// 64 个下标，最大 63——正好是两个 key 各 32 位。表本身是规范里的常量，
// **不是随机数**：B 站服务端用同一张表，所以改了就不对了。
var mixinKeyEncTab = [64]int{
	46, 47, 18, 2, 53, 8, 23, 32, 15, 50, 10, 31, 58, 3, 45, 35,
	27, 43, 5, 49, 33, 9, 42, 19, 29, 28, 14, 39, 12, 38, 41, 13,
	37, 48, 7, 16, 24, 55, 40, 61, 26, 17, 0, 1, 60, 51, 30, 4,
	22, 25, 54, 21, 56, 59, 6, 63, 57, 62, 11, 36, 20, 34, 44, 52,
}

// mixinKey 算出 32 位的混合密钥。
//
// 下标越界返回空串（调用方当「签名不可用」处理），而不是 panic——
// key 的长度不对是**外部输入**的问题，不是编程错误。
func mixinKey(imgKey, subKey string) string {
	orig := imgKey + subKey
	if len(orig) < 64 {
		return ""
	}
	var b strings.Builder
	b.Grow(32)
	for _, n := range mixinKeyEncTab {
		b.WriteByte(orig[n])
	}
	return b.String()[:32]
}

// wbiFilterChars 是签名时要从**值**里剔掉的五个字符。
//
// B 站的算法里有这一步，漏了它算出来的签名和 B 站算的不一样，
// 而报出来的是 -403「访问权限不足」。
var wbiFilterChars = strings.NewReplacer(
	"!", "", "'", "", "(", "", ")", "", "*", "",
)

// wbiSign 对参数签名，把 wts 和 w_rid 一起加进返回值。
//
// 返回的是**新**的 Values，输入那个不动：调用方常常要把带签名的和
// 不带签名的版本都留着（诊断信息用后者）。
func wbiSign(params url.Values, key string, now time.Time) url.Values {
	out := url.Values{}
	for k, vs := range params {
		for _, v := range vs {
			out.Add(k, v)
		}
	}
	out.Set("wts", fmt.Sprint(now.Unix()))

	// 按 key 排序，值剔掉那五个字符，然后 JS 风格地百分号编码。
	keys := make([]string, 0, len(out))
	for k := range out {
		keys = append(keys, k)
	}
	sortStrings(keys)

	var query strings.Builder
	for i, k := range keys {
		if i > 0 {
			query.WriteByte('&')
		}
		v := wbiFilterChars.Replace(out.Get(k))
		query.WriteString(jsEscape(k))
		query.WriteByte('=')
		query.WriteString(jsEscape(v))
	}

	sum := md5.Sum([]byte(query.String() + key))
	out.Set("w_rid", hex.EncodeToString(sum[:]))
	return out
}

// jsEscape 是 JavaScript 的 encodeURIComponent。
//
// ⚠️ 不能直接用 `url.QueryEscape`：它把空格编成 `+`，而 encodeURIComponent
// 编成 `%20`。B 站服务端用的是后者，所以用前者算出来的签名一定对不上。
func jsEscape(s string) string {
	escaped := url.QueryEscape(s)
	return strings.ReplaceAll(escaped, "+", "%20")
}

// sortStrings 是插入排序。
//
// 参数最多几十个，插入排序在这个量级比 sort.Strings 还快，而且不用
// 引 sort 包（整个包目前只需要这一个函数）。
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// key 的获取与缓存
// ─────────────────────────────────────────────────────────────────────────────

// pathNav 是下发 WBI key、也是判定登录态的接口。
// 没登录也返回 key（B 站的设计），所以拿 key 不需要登录。
const pathNav = "/x/web-interface/nav"

// wbiKeyTTL 是缓存有效期。
//
// B 站的 key 每天换一次。取短了只是多发几个请求，取长了会在某个
// 早上突然全体签名失败——所以宁可短。
const wbiKeyTTL = 2 * time.Hour

// wbiKeyCache 缓存从 nav 拿到的两个 key。
//
// 并发安全：同一个 key 过期时可能有几十个请求同时来取，让它们都发一遍
// nav 会直接撞上 -412 风控。所以取的时候按代次去重。
type wbiKeyCache struct {
	mu     sync.Mutex
	key    string
	expiry time.Time
	// fetching 是当前正在取的那一次。nil 表示没人在取。
	// 关掉它就是在告诉所有等待者「拿到了，自己去读」。
	fetching chan struct{}
}

// cached 是当前有效的密钥，**调用者必须已经拿着 mu**。
//
// ⚠️ 这两个方法不加锁，而且锁在 `Client.wbiKey` 里统一管。
// 早一版它们各自 Lock，然后又在一个已经拿着锁的地方被调用——
// `sync.Mutex` 不可重入，于是并发签名那条测试直接死锁。
func (w *wbiKeyCache) cached(now time.Time) string {
	if w.key != "" && now.Before(w.expiry) {
		return w.key
	}
	return ""
}

func (w *wbiKeyCache) store(key string, now time.Time) {
	w.key, w.expiry = key, now.Add(wbiKeyTTL)
}

// navData 是 nav 接口里我们关心的那一小块。
//
// 只声明用得着的字段：B 站的 nav 返回几十个字段，全声明进来等于把
// 别人的接口契约抄一遍，将来改了还会以为是自己改的。
type navData struct {
	WbiImg struct {
		ImgURL string `json:"img_url"`
		SubURL string `json:"sub_url"`
	} `json:"wbi_img"`
	// IsLogin 顺带读一下：判断登录态时不用再发一次请求。
	IsLogin bool   `json:"isLogin"`
	Mid     int64  `json:"mid"`
	Uname   string `json:"uname"`
}

// Nav 是 nav 接口的结果，登录判定的唯一依据。
type Nav struct {
	// LoggedIn 是「此刻这个客户端身上有有效登录态」。
	LoggedIn bool
	// Mid 是用户 ID。未登录时是 0。
	Mid int64
	// Uname 是昵称。
	Uname string
	// WBIKey 是这次顺带拿到的混合密钥。空串表示没拿到。
	WBIKey string
}

// Nav 调 nav 接口。
//
// 它是「登录态还在不在」的判据：SESSDATA 存在不等于会话还有效，
// B 站可以随时让它失效。重启之后恢复登录态靠的就是它，
// 而不是「cookie 读出来了」。
//
// 没登录时 **不返回错误**——B 站那时返回 code=0，只是 data.isLogin=false。
// code=-101 才是「连会话都不认」，那要原样往上抛。
func (c *Client) Nav(ctx context.Context) (Nav, error) {
	data, err := c.Do(ctx, Call{Method: "GET", URL: c.hostAPI + pathNav})
	if err != nil {
		return Nav{}, err
	}
	var raw navData
	if err := json.Unmarshal(data, &raw); err != nil {
		return Nav{}, fmt.Errorf("bili: 解 nav 的结果: %w", err)
	}
	return Nav{
		LoggedIn: raw.IsLogin,
		Mid:      raw.Mid,
		Uname:    raw.Uname,
		WBIKey:   mixinKey(wbiKeyFromURL(raw.WbiImg.ImgURL), wbiKeyFromURL(raw.WbiImg.SubURL)),
	}, nil
}

// wbiKeyFromURL 从 `https://i0.hdslb.com/bfs/wbi/<key>.png` 里取出 key。
//
// 剥的是最后一个点之后的后缀，不是 `.png` 四个字——B 站换过格式，
// 写死 `.png` 的话哪天变成 `.webp` 就静默取到空 key。
func wbiKeyFromURL(raw string) string {
	if raw == "" {
		return ""
	}
	name := raw
	if i := strings.LastIndexAny(name, "/?"); i >= 0 {
		name = name[i+1:]
	}
	if i := strings.LastIndex(name, "."); i > 0 {
		name = name[:i]
	}
	return name
}

// signWBI 拿 key、签名，把带签名的参数交回去。
func (c *Client) signWBI(ctx context.Context, params url.Values) (url.Values, error) {
	key, err := c.wbiKey(ctx)
	if err != nil {
		return nil, err
	}
	if key == "" {
		// 拿不到 key 就**原样放行**（不加 wts/w_rid）。
		//
		// 这是 Biu 上游的做法，也是对的：不是每个接口都要签名，
		// 为了一个没登录的用户去签名反而会把接口搞挂。真正要签名的接口
		// 拿不到 key 时会返回 -403，调用方看错误码就知道该提示什么。
		return params, nil
	}
	return wbiSign(params, key, c.now()), nil
}

// wbiKey 取当前可用的混合密钥，带缓存。
//
// **锁只有这一处管**。所有状态转换都在 `mu` 底下做，
// 而网络请求在锁**外面**发——不然一个卡住的 nav 会把所有取流请求
// 一起冻住，而那种卡住在界面上表现为「整个应用没反应」。
func (c *Client) wbiKey(ctx context.Context) (string, error) {
	c.wbiState.mu.Lock()
	if k := c.wbiState.cached(c.now()); k != "" {
		c.wbiState.mu.Unlock()
		return k, nil
	}
	// 已经有人在取了：等那一次，不重复发请求（并发重取会直接撞风控）。
	if wait := c.wbiState.fetching; wait != nil {
		c.wbiState.mu.Unlock()
		select {
		case <-wait:
			// 拿到之后由本函数统一决定要不要再取一次。
			return c.wbiKey(ctx)
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	wait := make(chan struct{})
	c.wbiState.fetching = wait
	c.wbiState.mu.Unlock()

	key, err := c.fetchWBIKey(ctx)

	c.wbiState.mu.Lock()
	c.wbiState.fetching = nil
	if err == nil && key != "" {
		c.wbiState.store(key, c.now())
	}
	c.wbiState.mu.Unlock()
	close(wait)

	return key, err
}

// fetchWBIKey 发一次 nav 把两个 key 取回来。**锁外面调用。**
func (c *Client) fetchWBIKey(ctx context.Context) (string, error) {
	data, err := c.Do(ctx, Call{Method: "GET", URL: c.hostAPI + pathNav})
	if err != nil {
		// ⚠️ **B 站没登录的时候也会把 key 给我们**：`nav` 回的是
		// `{"code":-101,...,"data":{"wbi_img":{...}}}`。把它当成「取不到 key」
		// 的话，未登录用户一个签名接口都调不了，而 `signWBI` 顶上那句
		// 「拿不到 key 就原样放行」也永远没机会执行。
		//
		// 所以这里不是「吞掉错误」，是**去错误里把数据捞出来**：捞到并且
		// 真的含 key 才当成功，捞不到就把错误原样还回去。
		var api *APIError
		if !errors.As(err, &api) || len(api.Data) == 0 {
			return "", err
		}
		data = api.Data
	}
	var raw navData
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", fmt.Errorf("bili: 解 nav 的结果: %w", err)
	}
	return mixinKey(wbiKeyFromURL(raw.WbiImg.ImgURL), wbiKeyFromURL(raw.WbiImg.SubURL)), nil
}
