// Package hotwords 是网易云的搜索热搜词，搜索框那个轮换的占位词的数据源。
//
// # 这个源和 B 站无关
//
// 它在 `music.163.com` 上，不在 B 站那几台主机上。走的是**同一张凭据白名单的
// referer 档**（`app.DefaultInjectPolicy` 把 `music.163.com` / `.163.com` 明确列着，
// 和歌词那一层同一个先例）——B 站的 Cookie 一个都不给它，而网易云没有 Referer 会拒。
//
// # 为什么是网易云而不是 B 站自己的热搜
//
// B 站的全站热搜（`s.search.bilibili.com/main/hotword`）实测没有音乐向的变体，
// 词是新闻、体育、电竞——在一个音乐播放器的搜索框里轮换这些词，用户点了一个
// 出来的结果页答非所问。网易云这份是歌名和歌手，人们真的在拿它们搜歌。
// 这个取舍在 PR 里有记录（2026-10-28 梳理拍板）。
//
// # 词不是搜索结果
//
// 这一层只给「词」：点它就是走一遍真搜索（`Shell.submitSearch` 那条路），跟
// 联想词（`bili.SearchSuggest`）是同一个形状。所以这里不做任何 B 站相关的翻译。
package hotwords

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// host 是热搜词接口所在的主机，path 是接口路径，query 里 type=1 是「按歌曲搜」
// （和歌词那一层的 `neteaseTypeSong` 同一个语义；不带 type 网易云回 400「参数错误」，
// 实测）。
const (
	host = "https://music.163.com"
	path = "/api/search/hot"
	// fetchLimit 是发给网易云的条数上限：词池给下面多留一点余量（显示长度那一道
	// 筛在业务层做，筛完可能不满），但也不照单全收。
	fetchLimit = 20
)

// hotwordsLimit 是这一层最多交出去几条词。
const hotwordsLimit = 20

// maxResponseBytes 是一次响应最多读多少。热搜词是一份几十条的列表，
// 这个上限只是「响应失控时别把内存读爆」的保险，和歌词那一层同一个数。
const maxResponseBytes = 1 << 20

// requestTimeout 是一次请求的超时。
const requestTimeout = 10 * time.Second

// headers 是网易云要的声明头，照歌词那一层逐字（`lyrics.search.go` 的
// `neteaseHeaders`）：没有 Referer 它会拒。
var headers = map[string]string{
	"Referer":    "https://music.163.com/",
	"Origin":     "https://music.163.com",
	"User-Agent": userAgent,
}

// userAgent 是一个真实浏览器的 UA。理由见歌词那一层：不给 UA 会被区别对待。
const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

// Client 是热搜词的客户端。
type Client struct {
	http *http.Client
	host string
}

// New 建客户端。⚠️ **client 应当传 `app.CredentialTransport` 建出来的那一个**
// （那个 nil 是 cookie jar、不是「用默认的」）；nil 时退化成一个不挂 jar 的
// `http.Client`，只是凭据边界被关掉了——测试就这么用。
func New(client *http.Client) *Client {
	if client == nil {
		client = &http.Client{}
	}
	return &Client{http: client, host: host}
}

// TestOnlyPointHostsAtFixture 把主机指到一台本地服务器上。不指过去的话测试会
// **打到真的 music.163.com 上去**：不报错、结果还挺合理，于是测试变成「取决于
// 能不能连上外网」。名字里带 `TestOnly` 是 Go 生态的约定——编译器不会拦，
// 但 grep 得到（歌词那一层同一个先例）。
func (c *Client) TestOnlyPointHostsAtFixture(base string) { c.host = base }

// hotRaw 是这个接口的响应。只声明用得着的字段。
type hotRaw struct {
	Code   int `json:"code"`
	Result struct {
		Hots []struct {
			// First 是词本身。`second` / `third` / `iconType` 这些字段这一层
			// 没有落点。
			First string `json:"first"`
		} `json:"hots"`
	} `json:"result"`
}

// Fetch 拉一批热搜词，按网易云给的顺序（就是热度顺序）。
//
// 空白剥掉、空串丢掉、重复去掉、条数压在 `hotwordsLimit` 上。**「一个都没有」
// 不是错误**（返回空切片加 nil），「拉不到」（网络、非 200、形状读不懂）才是——
// 这个分家要在这层做完，业务层和界面才分得清「B 站…网易云没给」和「这一趟没回来」。
func (c *Client) Fetch(ctx context.Context) ([]string, error) {
	u := c.host + path + "?" + url.Values{
		"type":  {"1"},
		"limit": {fmt.Sprint(fetchLimit)},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	resp, err := c.http.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hotwords: %s 返回 %s", path, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("hotwords: %s 的响应超过 %d 字节", path, maxResponseBytes)
	}

	var d hotRaw
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, fmt.Errorf("hotwords: 热搜词接口的返回读不懂：%w", err)
	}
	if d.Code != 200 {
		return nil, fmt.Errorf("hotwords: 热搜词接口回了 code %d", d.Code)
	}

	out := make([]string, 0, len(d.Result.Hots))
	seen := make(map[string]bool, len(d.Result.Hots))
	for _, h := range d.Result.Hots {
		w := strings.TrimSpace(h.First)
		if w == "" || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
		if len(out) == hotwordsLimit {
			break
		}
	}
	return out, nil
}
