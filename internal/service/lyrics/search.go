// 歌词搜索：两个源（网易云、LRCLib）各能搜歌名、各能给出一份完整歌词。
//
// Biu 的歌词**不是从 B 站拿的**——它是用户嫌「没有歌词」时自己开「搜索歌词」弹窗、按歌名
// 去两个源找的（`_research/biu/src/components/lyrics-search-modal/`）。所以这一层只有
// 「用户找过之后」的歌词。B 站字幕那一条是另一件事（`get-lyrics.ts`，B 站的 web player
// 接口），不在这一票里。
//
// # 两个源的差别只有两处，其余一律同形
//
//	网易云   搜 → 列表里只有歌名；**要再发一次请求**才拿得到歌词（`/api/song/lyric`）
//	LRCLib  搜 → **歌词就在搜索结果里**，预览时一次请求也不用发
//
// 接口对这两条路是同一个形状（`Search` / `Preview`），差别藏在各自那一支里。
//
// # 凭据边界在这一层之外
//
// **必须传 `app.CredentialTransport` 那一个建出来的 client**（界面层
// `newCredentialTransport().Client(nil)`）。两个歌词源在白名单里被**明确列进 referer 档**
// （`app.DefaultInjectPolicy`）：「它们只拿 Referer，B 站的 Cookie 一个都不给」。不走那一
// 层等于把凭据边界关掉，那不是「默认值」，是「默认不安全」。
package lyrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// 两个源的机器名。**不要拿界面上那个字当身份**：界面上写的是「网易云」/「LrcLib」
// （照抄上游 Tab 的写法），而行身份和缓存键都要一个跨版本不变的东西。
const (
	// SourceNetease 是网易云音乐那个源。
	SourceNetease = "netease"
	// SourceLRCLib 是 lrclib.net 那个源。
	SourceLRCLib = "lrclib"
)

// Doc 是一份歌词：原文 + 译文。
//
// **LRC 空串表示这一条没有歌词**（界面上照实说「未找到歌词内容」，不装出一个空弹窗）。
// 译文为空**不是**没有歌词——没有译文的外语歌照样能用（LRCLib 上大部分歌都没有
// 译文），把两者混在一起会让「找到了但没翻译」显示成「没找到」。
type Doc struct {
	LRC        string
	Translated string
}

// Candidate 是搜出来的一条候选。
//
// **ID 是行身份（铁律四）**：列表的选中、滚动和预览都按它认人，绝不能用序号或歌名
// ——序号在头部插入一行之后会让后面所有行的状态整体错配（`widgets/list` 顶上那一段
// 讲的就是这个坑）。
type Candidate struct {
	// ID 是这一条在源那边的稳定身份（网易云是歌曲 id 的十进制，LRCLib 是它的数字
	// id）。同一首歌在同一个源里永远是同一个值。
	ID string
	// Title / Artist / Album 是界面上那一行显示的三样。
	Title  string
	Artist string
	Album  string
	// DurationMS 是时长（毫秒）。两个源给的单位不一样（网易云本来就是毫秒，
	// LRCLib 是秒），**这一层统一成毫秒**——界面上只画一个「时:分」，没有两套
	// 换算的地方，也就没有换错其中一套的机会。
	//
	// 0 = 源没给。界面上照实写「--」而不是「0:00」（上游的 `renderDuration`
	// 就是 `return "--"`）。
	DurationMS int

	// LRC / Translated **只有 LRCLib 那一边有值**（见 `HasLyrics`）：网易云那边留
	// 空、预览时另外发一次请求。它们是**这一条自带的**，换一次搜索就跟着换。
	LRC        string
	Translated string
}

// HasLyrics 报这一条**自带**歌词没有。调用方（`app.OpenLyricPreview`）靠它决定
// 「当场填上」还是「进后台拉」，而**判错了的代价是不对称的**：把自带的那一种也送进
// 后台拉，用户点一条候选就多等一个网络往返（LRCLib 那条 `Preview` 一个请求都不发，
// 等的是零）；反过来在界面线程上同步调一次 `Preview`，那就是**在界面线程上等一个
// 真的网络往返**（网易云那一次），整块界面卡到它回来。
func (c Candidate) HasLyrics() bool { return strings.TrimSpace(c.LRC) != "" }

// Source 是「能从网上找歌词」的一个源。
type Source interface {
	// ID 是机器名（`SourceNetease` / `SourceLRCLib`），用在缓存键和行身份上。
	ID() string
	// Name 是界面上那个 Tab 上的字。**逐字照上游**：Tab 上是「网易云」和
	// 「LrcLib」（`lyrics-search-modal/index.tsx:177-178`）。
	Name() string
	// Search 按关键词搜候选。词为空时返回一个空列表、**不发请求**（抄上游的
	// `if (query === "") return`）。
	Search(ctx context.Context, query string) ([]Candidate, error)
	// Preview 拿某一条的完整歌词，供预览弹窗用。
	Preview(ctx context.Context, c Candidate) (Doc, error)
}

const (
	// requestTimeout 是单次请求的上限。10 秒抄上游的
	// `timeout: { request: 10000 }`（两个源那两份 got 调用）。
	//
	// ⚠️ 这里**不做上游那个 `retry: { limit: 3 }`**：三次重试配上一个 10 秒的超时
	// 就是 40 秒的转圈，而界面上从头到尾没有一句话说得清「卡在哪」。失败之后界面给的
	// 是「拉不到」加一个重试按钮（用户点的），那比闷头重试三次好解释。
	requestTimeout = 10 * time.Second

	// maxResponseBytes 是响应体的上限。
	//
	// **不是防御性编程，是这两个源本来就可能被问出很大的东西**：LRCLib 的
	// `/api/search` 一条结果里带整首歌词，问上一个宽泛的词就是几十条。
	// 不设上限的话一次手滑就是几百 MB，而那种崩溃在用户那边是「点一下就闪退」。
	maxResponseBytes = 8 << 20
)

var (
	// ErrNoQuery 是拿一个空词去搜。它不是失败，是一个「不去搜」——界面上敲到一半
	// 把框清空时不该弹一句红色的话。
	ErrNoQuery = errors.New("lyrics: 搜索词是空的")

	// ErrNoLyrics 是「这一条在源上没有歌词」。**它和「拉不到」是两件事**，
	// 界面上必须是两句话：上游这两个分支分别是「未找到歌词内容」和「获取歌词失败」。
	// 混成一句的话，用户读到「拉不到」会以为网络有问题、去反复重试，而这一条
	// 重试一万次也不会有歌词——那首歌在那个源上就是没有带时间轴的那一份。
	ErrNoLyrics = errors.New("lyrics: 这一条没有歌词")
)

// sourceHTTP 是两个源共用的那一点 HTTP。主机名做成字段而不是常量，是为了**包内测试**
// 能把请求指到一台本地服务器上（理由和 `bili.Client.hostAPI` 一模一样）。
type sourceHTTP struct {
	client  *http.Client
	host    string
	headers map[string]string
}

// ⚠️ **client 不许自己带超时**：超时在这一层的 ctx 上（见 `get`），那里还要能被测试
// 传一个更短的。两个超时哪一个先到说不清，而「说不清」的那一种最难复现。
func newSourceHTTP(client *http.Client, host string, headers map[string]string) sourceHTTP {
	return sourceHTTP{client: client, host: host, headers: headers}
}

// get 发一次 GET 并把响应解进 out。
//
// ⚠️ **状态码的判断绝不能省**：这两个源都用一个 JSON 体报失败（网易云的
// `code != 200`），只解 JSON 的话「拉不到」和「拉到一份空的」长得一模一样。
func (h sourceHTTP) get(ctx context.Context, path string, q url.Values, out any) error {
	u := h.host + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	for k, v := range h.headers {
		req.Header.Set(k, v)
	}
	// 超时由 ctx 控而不是 `http.Client.Timeout`，这样调用方能传一个更短的。
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	resp, err := h.client.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("lyrics: %s 返回 %s", path, resp.Status)
	}
	// 限长：见 maxResponseBytes。
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return err
	}
	if len(body) > maxResponseBytes {
		return fmt.Errorf("lyrics: %s 的响应超过 %d 字节", path, maxResponseBytes)
	}
	return json.Unmarshal(body, out)
}

// ── 网易云音乐 ───────────────────────────────────────────────────────────

// 网易云那几组数，**逐个照抄**上游（`electron/ipc/api/netease-lyric.ts`）：
// 主机、`type=1`（按歌曲搜）、`limit=20`（上游的 `DEFAULT_LIMIT`）。
const (
	neteaseHost     = "https://interface.music.163.com"
	neteaseTypeSong = 1
	neteaseLimit    = 20
)

// neteaseHeaders 是网易云要的那三个声明头。⚠️ **Referer 和 Origin 都是
// music.163.com**，不是 B 站的：这一层不走 B 站的任何凭据，而网易云没有 Referer 会拒。
var neteaseHeaders = map[string]string{
	"Referer":    "https://music.163.com/",
	"Origin":     "https://music.163.com",
	"User-Agent": neteaseUserAgent,
}

// neteaseUserAgent 是一个真实浏览器的 UA。两个源都不给 UA 会区别对待。
const neteaseUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

// Netease 是网易云音乐那个歌词源。
type Netease struct{ http sourceHTTP }

// NewNetease 建网易云那个源。⚠️ **client 必须传 `app.CredentialTransport` 那一个
// 建出来的**（那个 nil 是 cookie jar、不是「用默认的」）。nil 时它退化成
// `http.DefaultTransport`，只是凭据边界被关掉了。
func NewNetease(client *http.Client) *Netease {
	if client == nil {
		client = &http.Client{}
	}
	return &Netease{http: newSourceHTTP(client, neteaseHost, neteaseHeaders)}
}

func (n *Netease) ID() string   { return SourceNetease }
func (n *Netease) Name() string { return "网易云" }

// TestOnlyPointHostsAtFixture 把主机指到一台本地服务器上。不指过去的话包外的测试
// 会**打到真的 lrclib.net 上去**：不报错、结果还挺合理，于是测试变成「取决于能不能
// 连上外网」。名字里带 `TestOnly` 是 Go 生态的约定——编译器不会拦，但 grep 得到。
func (n *Netease) TestOnlyPointHostsAtFixture(base string) { n.http.host = base }

// neteaseSearchResp 是 `/api/search/get` 的响应。字段名逐个照
// `shared/types/lyrics.d.ts` 的 `SearchSongByNeteaseResponse`。
type neteaseSearchResp struct {
	Result struct {
		Songs []neteaseSong `json:"songs"`
	} `json:"result"`
	Code int `json:"code"`
}

// neteaseSong 是搜出来的一条。**只留界面上要用到的那几个字段**（上游 `NeteaseSong`
// 有二十来个，其余在这一层没有落点）。
type neteaseSong struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Artists []struct {
		Name string `json:"name"`
	} `json:"artists"`
	Album struct {
		Name string `json:"name"`
	} `json:"album"`
	// Duration 的单位是**毫秒**（这一层原样搬进 `Candidate.DurationMS`）。
	Duration int64 `json:"duration"`
}

// Search 搜网易云。
func (n *Netease) Search(ctx context.Context, query string) ([]Candidate, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, ErrNoQuery
	}
	var resp neteaseSearchResp
	err := n.http.get(ctx, "/api/search/get", url.Values{
		"s":      {query},
		"type":   {strconv.Itoa(neteaseTypeSong)},
		"limit":  {strconv.Itoa(neteaseLimit)},
		"offset": {"0"},
	}, &resp)
	if err != nil {
		return nil, err
	}
	// 网易云用体里的 `code` 报失败。code 200 之外的一切（含 401 没登录、
	// 405 参数不对）都是「拉不到」，界面上照实说那一句。
	if resp.Code != 200 {
		return nil, fmt.Errorf("lyrics: 网易云搜索返回 code %d", resp.Code)
	}
	out := make([]Candidate, 0, len(resp.Result.Songs))
	for _, s := range resp.Result.Songs {
		out = append(out, Candidate{
			ID:         strconv.FormatInt(s.ID, 10),
			Title:      orDash(s.Name),
			Artist:     neteaseArtists(s.Artists),
			Album:      orDash(s.Album.Name),
			DurationMS: int(s.Duration),
		})
	}
	return out, nil
}

// neteaseArtists 把艺人拼成一行。空的是「--」（上游的 `renderArtists`
// 就是 `return "--"`）。
func neteaseArtists(list []struct {
	Name string `json:"name"`
}) string {
	names := make([]string, 0, len(list))
	for _, a := range list {
		if a.Name != "" {
			names = append(names, a.Name)
		}
	}
	if len(names) == 0 {
		return "--"
	}
	return strings.Join(names, " / ")
}

// neteaseLyricResp 是 `/api/song/lyric` 的响应，逐条照 `GetLyricsByNeteaseResponse`。
type neteaseLyricResp struct {
	LRC struct {
		Lyric string `json:"lyric"`
	} `json:"lrc"`
	// Klyric 是逐字歌词，**拿它当原文的备胎**（上游 `netease-tab.tsx:53` 那个
	// `lrc?.lyric?.trim() || klyric?.lyric?.trim()`）：有些歌只有 klyric。
	Klyric struct {
		Lyric string `json:"lyric"`
	} `json:"klyric"`
	// TLyric 是译文。
	Tlyric struct {
		Lyric string `json:"lyric"`
	} `json:"tlyric"`
	Code int `json:"code"`
}

// Preview 拿网易云那一条的歌词。
func (n *Netease) Preview(ctx context.Context, c Candidate) (Doc, error) {
	if c.ID == "" {
		return Doc{}, errors.New("lyrics: 这一条没有歌曲 id")
	}
	var resp neteaseLyricResp
	// 那五个参数逐字照上游（`netease-lyric.ts`）：`-1` 是「都要」，`_nmclfl` 是要
	// 那个「含翻译标记」的版本。少一个的话译文那一栏是空的，而空译文在界面上长得
	// 和「这首歌没有翻译」一模一样。
	err := n.http.get(ctx, "/api/song/lyric", url.Values{
		"id":      {c.ID},
		"tv":      {"-1"},
		"lv":      {"-1"},
		"rv":      {"-1"},
		"kv":      {"-1"},
		"_nmclfl": {"1"},
	}, &resp)
	if err != nil {
		return Doc{}, err
	}
	if resp.Code != 200 {
		return Doc{}, fmt.Errorf("lyrics: 网易云歌词返回 code %d", resp.Code)
	}
	return Doc{
		LRC:        strings.TrimSpace(firstNonEmpty(resp.LRC.Lyric, resp.Klyric.Lyric)),
		Translated: strings.TrimSpace(resp.Tlyric.Lyric),
	}, nil
}

// ── LRCLib ───────────────────────────────────────────────────────────────

const lrclibHost = "https://lrclib.net"

// lrclibLimit 是一次搜多少条。上游**没写这个数**（`lrclib-tab.tsx` 直接把整个响应
// 铺开），这里取一个和网易云一样的 20：那一条 340px 高的面板放不下几十行，而多出来的
// 每一条都是一份解出来的 LRC——LRCLib 的搜索响应里每条都带整首歌词。
const lrclibLimit = 20

// LRCLib 是 lrclib.net 那个歌词源。
type LRCLib struct{ http sourceHTTP }

// NewLRCLib 建 LRCLib 那个源。⚠️ **client 必须传 `app.CredentialTransport` 那一个
// 建出来的**（理由同 `NewNetease`）。
func NewLRCLib(client *http.Client) *LRCLib {
	if client == nil {
		client = &http.Client{}
	}
	return &LRCLib{http: newSourceHTTP(client, lrclibHost, nil)}
}

func (l *LRCLib) ID() string   { return SourceLRCLib }
func (l *LRCLib) Name() string { return "LrcLib" }

// TestOnlyPointHostsAtFixture 见 `Netease.TestOnlyPointHostsAtFixture`。
//
//nolint:unused // 包外（app 的凭据边界那条）用得到。
func (l *LRCLib) TestOnlyPointHostsAtFixture(base string) { l.http.host = base }

// lrclibItem 是搜索结果里的一条，逐个照 `SearchSongByLrclibResponse`。
type lrclibItem struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	TrackName  string `json:"trackName"`
	ArtistName string `json:"artistName"`
	AlbumName  string `json:"albumName"`
	// Duration 的单位是**秒**——和网易云的毫秒不是一回事（上游两个 Tab 的
	// `renderDuration` 一个除 1000 一个不除）。这里乘 1000 统一成毫秒。
	Duration float64 `json:"duration"`
	// SyncedLyrics 是**带时间轴**的歌词。界面上要的是这一份。
	SyncedLyrics string `json:"syncedLyrics"`
	// PlainLyrics 是没有时间轴的纯文本。这里**不用**它：不能跟着唱。
	PlainLyrics string `json:"plainLyrics"`
}

// Search 搜 LRCLib。
func (l *LRCLib) Search(ctx context.Context, query string) ([]Candidate, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, ErrNoQuery
	}
	var resp []lrclibItem
	err := l.http.get(ctx, "/api/search", url.Values{
		"q":     {query},
		"limit": {strconv.Itoa(lrclibLimit)},
	}, &resp)
	if err != nil {
		return nil, err
	}
	out := make([]Candidate, 0, len(resp))
	for _, it := range resp {
		// 标题优先 trackName（上游也是 `item.trackName || item.name || "--"`，
		// `lrclib-tab.tsx:66`）：`name` 有时是「歌名 - 歌手」拼起来的一整串，
		// 当歌名用会和右边那一列重复。
		out = append(out, Candidate{
			ID:         strconv.FormatInt(it.ID, 10),
			Title:      orDash(firstNonEmpty(it.TrackName, it.Name)),
			Artist:     orDash(it.ArtistName),
			Album:      orDash(it.AlbumName),
			DurationMS: int(it.Duration * 1000),
			LRC:        strings.TrimSpace(it.SyncedLyrics),
		})
	}
	return out, nil
}

// Preview 拿 LRCLib 那一条的歌词。🔴 **一个请求都不发**，歌词是搜索时就带在
// `Candidate` 里的（上游 `lrclib-tab.tsx:27-37` 也是直接读 `item.syncedLyrics`）——
// 多发一次 `/api/get/:id` 拿的是同一份东西，代价是每点一条候选多一个网络往返。
func (l *LRCLib) Preview(_ context.Context, c Candidate) (Doc, error) {
	if c.LRC == "" {
		// ⚠️ 必须是 `ErrNoLyrics` 而不是一句随便的错误：调用方靠它把「这首歌在源上
		// 没有」和「网络不通」分开说，而它们要用户做的事完全相反。
		return Doc{}, ErrNoLyrics
	}
	return Doc{LRC: c.LRC, Translated: strings.TrimSpace(c.Translated)}, nil
}

// ─────────────────────────────────────────────────────────────────────────────

// firstNonEmpty 返回第一个去掉首尾空白之后还剩下东西的串。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// orDash 是「源没给这一项」时的显示值，抄上游那几个 `renderX` 的 `return "--"`：
// 界面上「不知道」要**写出来**，留一个空格的话那一列看着像渲染坏了。
func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "--"
	}
	return s
}
