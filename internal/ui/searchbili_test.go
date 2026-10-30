package ui

// 这一组是搜索页**接上真 B 站客户端**的那一半：真的走 `bili.Client`、真的发
// WBI 签名的请求、真的把流拉下来解码。
//
// 上一组（searchpage_test.go）用的是假拉页器，它替掉的只有网络那一段——但代次闸门
// 和 cid 补齐都长在**真的**拉页路里，用假的那一份测它们等于没测（把真实现里的
// 闸门删掉，假拉页器照样给自己补一道，全绿）。这两条评审指出来的缺口就是从这儿漏
// 过去的，所以这一组特意走真的。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// gateWait 是卡住一个响应最多等多久（兜底，别让一条红了的用例把整包卡到超时）。
const gateWait = 5 * time.Second

// testSearchBvid 是这台假 B 站回的那一条结果的 bvid。
const testSearchBvid = "BVsearchResultOnly"

// wbiImgKey / wbiSubKey 是签名用的两个 key。值本身不重要（这一层只是把它们拼进
// 哈希），重要的是 B 站的 nav **给了**它们——不给的话搜索请求过不了签名那一步。
const (
	wbiImgKey = "7cd084941338484aae1ad9425b84077c"
	wbiSubKey = "4932caff0ff746eab6f01bf08b70ac45"
)

// biliFixture 是一台会答 B 站接口的假服务器：WBI 的 key、分类搜索、分 P 列表、
// 取流、CDN 字节，全在这一台上。
type biliFixture struct {
	*httptest.Server

	mu sync.Mutex
	// gates 按搜索词把响应卡住，直到 release：造「旧查询的响应迟到」。
	gates map[string]chan struct{}
	// searchCids 记每次取流带的 cid——「补没补上分 P id」就看你真的带上没有。
	searchCids []string
	// searched 记被叫过的搜索请求的关键词（按顺序）。
	searched []string
	// pagelistCalls 数分 P 列表被叫了几次——「放歌之前有没有问过」就看你叫没叫。
	pagelistCalls int
	// playURLCalls 数取流接口被叫了几次——「这一趟取流真的发生了没有」就靠它
	// （票 #624：失效的条目**不该**触发这一趟，而「没触发」只有数得出来才验得到）。
	playURLCalls int

	// suggestTerms 是每个词该回的那批联想词；suggestAsked 记被问过的词（按顺序）；
	// suggestFail 非 0 时这一路一律回这个业务 code（造「拉不到」）。
	//
	// 三样都是**按词**记的：防抖那一组要量的是「同一个词有没有被问第二遍」，而那
	// 需要把每一次请求分别记下来。
	suggestTerms map[string][]string
	suggestAsked []string
	suggestServe int
	suggestFail  int
	// videoPages 顶掉分 P 列表的回包。nil 是默认那**一集**。
	videoPages []any
	// viewPoints 是播放器信息接口（/x/player/wbi/v2）里 `view_points` 的回包
	// （章节，票 #585）。nil = 没有章节。viewPointsCalls 数它被叫了几次。
	viewPoints      []any
	viewPointsCalls int
	// served 数**已经回了**的搜索响应。放行一个卡住的响应之后靠它知道「旧的回去
	// 了」——不等它的话，断言可能在那一趟还在网络上时就跑完了，看着全绿其实什么
	// 都没验到。
	served int
	media  []byte

	// searchFail 非 0 时「搜索」这一路回这个 **HTTP 状态码**（造「拉不到」）。
	//
	// ⚠️ **它是 HTTP 状态码而不是业务 code**：403 这一档走的是 `bili.ErrNetwork`
	// （压根没拿到一份能解析的响应），而业务 code（比如 -101）走的是另一条路。
	// 两��路都有人在用（联想词那一档走的是业务 code，见 `setSuggestFail`）。
	searchFail int

	// searchTotal 是搜索那一次回包里的**命中总数**（B 站的 `numResults`）。
	// 零 = 1（默认那一份）。
	//
	// ⚠️ **它是「一共多少个」**：默认那一份回的是 1 个结果、总数也是 1，于是
	// 「总数」和「这一页有几行」分不开——而「找到 N 个结果」那一句里的 N 要是
	// 拿「这一页有几行」冒充，用户就知道了个假数。
	searchTotal int

	// viewReply 是 `/x/web-interface/view` 回的 `stat.reply`（评论数，票 #682）。
	//
	// ⚠️ **零值 = 「B 站没给这个数」**：播放条上那一枚键拿不到数就**不画数字**
	// （不是画「0」——那会让用户以为没人评论）。
	//
	// ⚠️ **这一格特意摆在这里**（不跟在点赞那几格后面）：票 #680 在那一带把
	// `lastUp` 改成了 `lastLike` / `lastLikeForm`，两边都往同一处加字段会让
	// 合并必冲突。这一段是**只加不改**——合并时照这一格原样重放即可。
	viewReply int64

	// favoritedIn 是「这一首已经在哪些收藏夹里」；favoriteCalls 记下每一次写。
	favoritedIn   []int64
	favoriteCalls []url.Values

	// 点赞那三个接口的答案（见 like_test.go 的 onLike）。
	//
	// relLiked/relFaved 是「赞没赞、收藏没收藏」那一问的答案；
	// tripleBody 是三连那一趟的回包（可以改成「B 站只做成了一部分」）。
	relLiked, relFaved bool
	tripleBody         string
	// likePosts 记每一次赞的 up（1 赞 / 2 取消赞），triplePosts 数三连。
	likePosts, triplePosts int
	lastUp                 string
}

// fixture 那一摊收藏用的固定值。
const (
	fixtureMid         = 9527
	fixtureAid         = 4242
	fixtureFolderID    = 100
	fixtureFolderTitle = "默认收藏夹"
)

// newBiliFixture 建一台假 B 站。search 造几页，每页 n 条。
func newBiliFixture(t *testing.T, n int) *biliFixture {
	t.Helper()
	f := &biliFixture{gates: map[string]chan struct{}{}, suggestTerms: map[string][]string{}}

	f.media, _ = os.ReadFile(flacAssetPath(t))
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	// ⚠️ 收尾**必须**把所有卡住的响应都放掉：`httptest.Server.Close` 会等所有在途
	// 请求返回，而一个永远等着的 handler 会让它一直等到测试的十分钟上限——于是「这条
	// 用例红了」变成「整个包卡死十分钟」。
	t.Cleanup(func() {
		f.releaseAll()
		f.Close()
	})
	return f
}

func (f *biliFixture) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/x/web-interface/nav":
		// ⚠️ `isLogin` / `mid` 是收藏那一路要的（`bili.FavoriteSong` 先问 nav 拿
		// mid 才挑得了收藏夹）。搜索那一路只要 wbi_img，所以多带两个字段不影响它。
		writeBiliJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{"isLogin": true, "mid": fixtureMid, "uname": "测试账号",
				"wbi_img": map[string]string{
					"img_url": "https://i0.hdslb.com/bfs/wbi/" + wbiImgKey + ".png",
					"sub_url": "https://i0.hdslb.com/bfs/wbi/" + wbiSubKey + ".png",
				}}})

	case r.URL.Path == "/x/web-interface/view":
		// bvid → avid（收藏接口要的是 avid），外加 `stat.reply`——播放条上那一枚
		// 「评论」键的评论数（票 #682）。
		f.mu.Lock()
		reply := f.viewReply
		f.mu.Unlock()
		writeBiliJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{
				"aid": fixtureAid, "bvid": r.URL.Query().Get("bvid"),
				"stat": map[string]any{"reply": reply},
			}})

	case r.URL.Path == "/x/v3/fav/folder/created/list-all":
		writeBiliJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{"count": 2, "list": []any{
				map[string]any{"id": fixtureFolderID, "title": fixtureFolderTitle, "media_count": 1, "attr": 0},
				map[string]any{"id": 200, "title": "第二格", "media_count": 0, "attr": 0},
			}}})

	case r.URL.Path == "/x/v3/fav/resource/ids":
		// 收的是**收藏夹 id**、返回的是**那一格里的内容**（上游
		// `fav-resource.ts:127-152`）。每一项是一个对象 `{id, type}`。
		items := make([]any, 0, len(f.favoritedIn))
		for _, id := range f.favoritedIn {
			items = append(items, map[string]any{"id": id, "type": 2, "bvid": "BV1xx", "bv_id": "BV1xx"})
		}
		writeBiliJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{"ids": items}})

	case r.URL.Path == "/x/v3/fav/resource/deal":
		_ = r.ParseForm()
		f.mu.Lock()
		f.favoriteCalls = append(f.favoriteCalls, r.PostForm)
		f.mu.Unlock()
		writeBiliJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1})

	case r.URL.Path == "/x/web-interface/archive/relation":
		f.serveArchiveRelation(w)

	case r.URL.Path == "/x/web-interface/archive/like":
		f.serveArchiveLike(w, r)

	case r.URL.Path == "/x/web-interface/archive/like/triple":
		f.serveArchiveTriple(w, r)

	case r.URL.Path == "/x/web-interface/wbi/search/type":
		f.serveSearch(w, r)

	case r.URL.Path == "/main/suggest":
		f.serveSuggest(w, r)

	case r.URL.Path == "/x/player/pagelist":
		f.serveVideoPages(w, r)

	case r.URL.Path == "/x/player/wbi/v2":
		f.serveViewPoints(w)

	case r.URL.Path == "/x/player/wbi/playurl":
		f.servePlayURL(w, r)

	case strings.HasPrefix(r.URL.Path, "/cdn/"):
		f.serveMedia(w, r)

	default:
		http.NotFound(w, r)
	}
}

func (f *biliFixture) serveSearch(w http.ResponseWriter, r *http.Request) {
	kw := r.URL.Query().Get("keyword")

	f.mu.Lock()
	f.searched = append(f.searched, kw)
	fail := f.searchFail
	f.mu.Unlock()
	if fail != 0 {
		// 403 这一档：压根没拿到一份能解析的响应 → `bili.ErrNetwork`
		// → 界面上一句「连不上 B 站，检查一下网络」。
		w.WriteHeader(fail)
		return
	}

	f.mu.Lock()
	// ⚠️ 这里**不删**门：删了就没人放行了（`release` 从这张表里取），那条响应会一直
	// 挂到 `gateWait` 兜底才自己回去——而用例早就断言完了，看着全绿其实什么都没验到。
	ch := f.gates[kw]
	f.mu.Unlock()
	if ch != nil {
		select {
		case <-ch: // 放行之前这一趟不落地
		case <-time.After(gateWait):
			// 兜底：用例提前失败、没人来放行时，这一条也回去，不把整个包卡死。
		}
	}
	f.mu.Lock()
	f.served++
	total := f.searchTotal
	f.mu.Unlock()
	if total <= 0 {
		total = 1
	}

	// 一页一条。bvid **不带 cid**：这正是搜索结果的形状。
	writeBiliJSON(w, map[string]any{
		"code": 0, "message": "0", "ttl": 1,
		"data": map[string]any{
			"numResults": total, "numPages": 1,
			"result": []any{map[string]any{
				"type": "video", "bvid": testSearchBvid,
				"title":    "只有 bvid 的<em class=\"keyword\">结果</em>",
				"author":   "某UP",
				"mid":      1,
				"pic":      "//i0.hdslb.com/a.jpg",
				"duration": "4:20",
			}},
		},
	})
}

// serveSuggest 是搜索联想词（搜索站那台，`#97`）。
//
// ⚠️ **回包形状照着真接口抄**：`data` 是一个对象，里面 `tag` 是那一列词；每一项
// 一个干净的 `term`、一个把命中的部分包进 `<em class="keyword">` 的 `name`。
// 少一层或者多一层的话，那边解析出来的就是空的，而界面上的症状是「浮层里只有历史
// 那一档」——和「真的拉不到」长得一模一样。
func (f *biliFixture) serveSuggest(w http.ResponseWriter, r *http.Request) {
	kw := r.URL.Query().Get("term")

	f.mu.Lock()
	f.suggestAsked = append(f.suggestAsked, kw)
	terms, code := f.suggestTerms[kw], f.suggestFail
	f.suggestServe++
	f.mu.Unlock()

	if code != 0 {
		writeBiliJSON(w, map[string]any{"code": code, "message": "请求过于频繁", "ttl": 1})
		return
	}
	tag := make([]any, 0, len(terms))
	for _, term := range terms {
		tag = append(tag, map[string]any{
			"term": term, "name": term, "type": "huashu", "ref": "", "mid": 0,
		})
	}
	writeBiliJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
		"data": map[string]any{"tag": tag}})
}

// setSuggestTerms 改某一词该回的那批联想词。
//
// ⚠️ **带着锁改**：handler 在锁里读这张表，测试在锁外写的话，一趟还在路上的响应
// 就可能和这一写撞上（`go test -race` 报出来的那种）。
func (f *biliFixture) setSuggestTerms(kw string, terms ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.suggestTerms[kw] = terms
}

// setSearchFail 改「搜索」这一路要回的 HTTP 状态码（0 = 正常）。
//
// ⚠️ **状态码不是业务 code**：403 走的是「压根没拿到一份能解析的响应」
// （`bili.ErrNetwork`），而业务 code（比如 -101）在 JSON 里面、走的是另一条路。
// 两条路翻出来的话不一样（见 `bili.Friendly`），所以要分别造。
func (f *biliFixture) setSearchFail(code int) {
	f.mu.Lock()
	f.searchFail = code
	f.mu.Unlock()
}

// setSuggestFail 改这一路要回的业务 code（0 = 正常）。
func (f *biliFixture) setSuggestFail(code int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.suggestFail = code
}

// suggestCalls 记下这一路上被问过的词（按顺序）。
func (f *biliFixture) suggestCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.suggestAsked...)
}

// suggestServed 数这一路上**已经回了**几次（不是「发了几次」）。
//
// 两个数的差就是还在路上的那些。判「那一次问完了」要的是它：只等「发了」的话，
// 界面上的状态还停在请求发出去的那一刻，验的是中间态。
func (f *biliFixture) suggestServed() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.suggestServe
}

// serveVideoPages 是分 P 列表。
//
// 🔴 **`data` 是数组**（上游 `_research/biu/src/service/player-pagelist.ts:18`：
// `data: PlayerPagelistItem[]`）。这里第一版造的是 `data: {pages: [...]}`，而实现
// 也照着那个错形状写，于是端到端那条用例全绿；真接口回数组时补 cid 静默失败，
// 「全部播放」点下去歌放不出来。评审抓到的那条就是这个——**fixture 的形状必须照
// 着真接口抄**。
func (f *biliFixture) serveVideoPages(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.pagelistCalls++
	pages := f.videoPages
	f.mu.Unlock()

	if pages != nil {
		writeBiliJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1, "data": pages})
		return
	}

	writeBiliJSON(w, map[string]any{
		"code": 0, "message": "0", "ttl": 1,
		"data": []any{map[string]any{
			"cid": 777, "page": 1, "part": "第一集", "duration": 260,
			"from": "vupload", "vid": "", "weblink": "",
			"dimension":   map[string]any{"width": 1920, "height": 1080, "rotate": 0},
			"first_frame": "//i0.hdslb.com/bfs/storyff/n10500.jpg",
		}},
	})
}

// setVideoPages 换掉分 P 列表的回包（nil = 回到那一集）。
//
// 全屏播放器的分集面板要的是「多 P 的稿子」（Biu 的 `playItem.hasMultiPart`），
// 而默认那份 fixture 只回一集——面板的入口按 Biu 是不摆的。
func (f *biliFixture) setVideoPages(pages []any) {
	f.mu.Lock()
	f.videoPages = pages
	f.mu.Unlock()
}

// setViewReply 改 `/x/web-interface/view` 回的评论数（`stat.reply`，票 #682）。
//
// ⚠️ **带着锁改**：handler 在锁里读这一格，测试在锁外写的话，一趟还在路上的
// 响应就可能和这一写撞上（`go test -race` 报出来的那种）。
func (f *biliFixture) setViewReply(reply int64) {
	f.mu.Lock()
	f.viewReply = reply
	f.mu.Unlock()
}

// serveViewPoints 回播放器信息接口的章节段（票 #585）。
//
// ⚠️ 响应的 `data` 是**data 段本身**（`Client.Do` 拆过壳了），view_points 就挂在
// 它上面——没有章节的时候整个字段不给（B 站对没有章节的投稿就是这样）。
func (f *biliFixture) serveViewPoints(w http.ResponseWriter) {
	f.mu.Lock()
	f.viewPointsCalls++
	points := f.viewPoints
	f.mu.Unlock()

	data := map[string]any{"bvid": "BVchapters", "cid": 1}
	if points != nil {
		data["view_points"] = points
	}
	writeBiliJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1, "data": data})
}

// setViewPoints 换掉章节段的回包（nil = 没有章节）。
func (f *biliFixture) setViewPoints(points []any) {
	f.mu.Lock()
	f.viewPoints = points
	f.mu.Unlock()
}

func (f *biliFixture) servePlayURL(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.searchCids = append(f.searchCids, r.URL.Query().Get("cid"))
	// ⚠️ **数取流接口被问了几次**（票 #624）：失效的条目**不该**触发这一趟
	// ——而「没触发」只有数得出来才验得到。
	f.playURLCalls++
	f.mu.Unlock()

	writeBiliJSON(w, map[string]any{
		"code": 0, "message": "0", "ttl": 1,
		"data": map[string]any{
			"timelength": 1000,
			"dash": map[string]any{
				"duration": 1,
				"audio": []any{map[string]any{
					"id": 30280, "bandwidth": 110836, "codecs": "mp4a.40.2",
					"mimeType": "audio/mp4", "baseUrl": f.mediaURL(),
				}},
			},
		},
	})
}

// playURLHits 报取流接口被问了几次。
//
// ⚠️ **给测试用**：它是「失效的条目没有白跑一趟取流」那一条的判据（票 #624）。
func (f *biliFixture) playURLHits() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.playURLCalls
}

func (f *biliFixture) mediaURL() string {
	return f.URL + "/cdn/a.flac?deadline=" +
		strconv.FormatInt(time.Now().Add(2*time.Hour).Unix(), 10) + "&upsig=deadbeef"
}

func (f *biliFixture) serveMedia(w http.ResponseWriter, r *http.Request) {
	data := f.media
	start := 0
	if h := r.Header.Get("Range"); strings.HasPrefix(h, "bytes=") {
		start, _ = strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(h, "bytes="), "-"))
	}
	if start > len(data) {
		http.Error(w, "越界", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	w.Header().Set("Content-Length", fmt.Sprint(len(data)-start))
	if start > 0 {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(data)-1, len(data)))
		w.WriteHeader(http.StatusPartialContent)
	}
	w.Write(data[start:])
}

func writeBiliJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// hold 把某个词的搜索响应卡住，release 才放行。
func (f *biliFixture) hold(keyword string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gates[keyword] = make(chan struct{})
}

func (f *biliFixture) release(keyword string) {
	f.mu.Lock()
	ch := f.gates[keyword]
	delete(f.gates, keyword)
	f.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}

// releaseAll 放掉所有还卡着的（收尾用）。
func (f *biliFixture) releaseAll() {
	f.mu.Lock()
	chans := f.gates
	f.gates = map[string]chan struct{}{}
	f.mu.Unlock()
	for _, ch := range chans {
		close(ch)
	}
}

// pagelists 是分 P 列表被叫过几次。
func (f *biliFixture) pagelists() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pagelistCalls
}

// servedCount 是已经回了多少个搜索响应。
func (f *biliFixture) servedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.served
}

func (f *biliFixture) cids() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.searchCids...)
}

// setSearchTotal 设搜索那一次回包里的命中总数。
func (f *biliFixture) setSearchTotal(n int) {
	f.mu.Lock()
	f.searchTotal = n
	f.mu.Unlock()
}

// client 建一个指着这台服务器的 B 站客户端。
func (f *biliFixture) client() *bili.Client {
	c := bili.NewClient(bili.Options{Transport: stickyTransport{}})
	c.TestOnlyPointHostsAtFixture(f.URL)
	return c
}

// stickyTransport 是「无视取消」的那一层网络。
//
// 换筛选条件时 `PagedList.Reload` 会 cancel 掉在途的那一趟请求——**真的 http
// 往返会因此报错**，于是旧结果自然落不下来。要复现「响应已经回来了、用户这时候才
// 换的筛选条件」这一种（cancel 拦不住的那一种，评审的探针就是这么造的），这一层
// 把 ctx 上的取消摘掉再发。
type stickyTransport struct{}

func (stickyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	// **无条件**摘掉取消，不是「已经取消时才摘」：请求是在 cancel 之前发出去的，
	// 摘晚了等于没摘（base 会在 cancel 到达的那一刻把连接掐了，err 直接从 Do 出来，
	// 旧结果自然就落不下来了——那样这条用例测不到任何东西）。
	return http.DefaultTransport.RoundTrip(r.Clone(context.WithoutCancel(r.Context())))
}

// ─────────────────────────────────────────────────────────────────────────────
// 搜出来的歌放得起来（评审的 critical）
// ─────────────────────────────────────────────────────────────────────────────

// TestASearchResultActuallyStartsPlaying 盯「搜出来的歌真的放得起来」。
//
// 这是评审挑出来的那条 critical：B 站的搜索结果**只给 bvid**，而在线播放当年要求
// cid，于是搜出来的歌一首都放不了，播放条上红字写「缺稿件 id 或分 P id」（那时 cid 还是判据之一）——用户按的
// 就是搜索结果，说他缺一个他压根拿不到的东西。
//
// 这一条走的是**整条链**：真的搜索接口 → 真的映射成列表行 → 真的按「全部播放」→
// 真的补 cid → 真的取流 → 真的解码出声。判据是 `HasTrack` 与取流请求里**真的带上**
// 那个 cid——只验「放出来了」不够，B 站不给 cid 时按第一集给也能过，而那正是要消掉
// 的「放错那一集」。
func TestASearchResultActuallyStartsPlaying(t *testing.T) {
	f := newBiliFixture(t, 1)
	s, tt := newShell(t)
	cli := f.client()
	searchOnRealClient(s, cli)
	s.app.MountStreaming(app.Streaming{Client: cli, Media: f.Client(), TempDir: t.TempDir()})

	s.navigate(searchPath + "?" + searchQuery + "=" + url.QueryEscape("周杰伦"))
	settleText(t, tt, "只有 bvid 的结果") // 剥掉 <em> 之后的标题，见 bili 的 stripHTML

	if err := tt.Click("全部播放"); err != nil {
		t.Fatalf("点不到「全部播放」：%v，文字有 %q", err, tt.Texts())
	}

	// 等它真的出声。
	waitFrames(t, tt, "搜出来的那首开始出声", func() bool { return s.app.Snapshot().HasTrack })
	if got := s.app.Snapshot().Err; got != "" {
		t.Errorf("播放条上有错误：%s", got)
	}
	cids := f.cids()
	if len(cids) == 0 {
		t.Fatalf("取流接口一次都没被叫")
	}
	for i, c := range cids {
		if c != "777" {
			t.Errorf("第 %d 次取流带的 cid 是 %q，want 777（问来的那一集）", i+1, c)
		}
	}
}

// TestASearchResultSaysWhyWhenThePartCannotBeAsked 盯「补 cid 那一趟问不到就说清楚」。
//
// 🔴 这一条是第一版那个错的方向：问不到分 P 的时候**照样发一个没有 cid 的取流请求**，
// 赌「B 站会按第一集给」。那个赌没人验过——上游 `player-playurl.ts:9` 把 cid 写成
// 必填，而本地 fixture 放不放假参数，所以「它会不会老实按第一集给」根本没人知道。
// 真接口要是回一段不知道是哪一集的音频，用户会听到一首不是他点的歌，而且界面上什么都
// 看不出来。
//
// 所以现在的约定是：**问不出来就不取流**，播放条上说明白是「问不出这一集是哪一集」。
func TestASearchResultSaysWhyWhenThePartCannotBeAsked(t *testing.T) {
	f := newBiliFixture(t, 1)
	// 分 P 列表这一段直接顶掉：挡掉它（和真被风控一样）。
	f.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/x/player/pagelist" {
			http.Error(w, "风控", http.StatusForbidden)
			return
		}
		f.serve(w, r)
	})

	s, tt := newShell(t)
	cli := f.client()
	searchOnRealClient(s, cli)
	s.app.MountStreaming(app.Streaming{Client: cli, Media: f.Client(), TempDir: t.TempDir()})

	s.navigate(searchPath + "?" + searchQuery + "=" + url.QueryEscape("周杰伦"))
	settleText(t, tt, "只有 bvid 的结果")
	if err := tt.Click("全部播放"); err != nil {
		t.Fatalf("点不到「全部播放」：%v，文字有 %q", err, tt.Texts())
	}

	// 播放条上要说明白是「问不出这一集」，而不是一句看不懂的「取不到流」。
	waitFrames(t, tt, "播放条上说明白原因", func() bool { return s.app.Snapshot().Err != "" })
	if got := s.app.Snapshot().Err; !strings.Contains(got, "问不出") {
		t.Errorf("播放条上写的是 %q，要说清是「问不出这一集是哪一集」", got)
	}
	if s.app.Snapshot().HasTrack {
		t.Errorf("问不出分 P 还照样出声了")
	}
	// 🔴 **一个没有 cid 的取流请求都不该发**。
	for i, c := range f.cids() {
		t.Errorf("第 %d 次取流还是发出去了，cid=%q —— 问不出来就不该取流", i+1, c)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 拉失败时界面上那一句是人话（票 #455）
// ─────────────────────────────────────────────────────────────────────────────

// TestTheSearchFailureSaysSomethingTheUserCanUnderstand 盯「搜索拉失败时，界面上
// 摆出来的那一句是**用户看得懂的话**」。
//
// ⚠️ **这一条走真的那条链**（真的 `bili.Client` → 真的 HTTP → 真的 403）：上一组
// （`searchpage_test.go`）用的是假拉页器，而假拉页器把自己写的那句错直接递上来
// ——那一支**压根不经过 `bili.Friendly`**。于是「原始的错翻成人话」这一段在那边
// 一条用例都没有：它要是坏了（忘了翻，或者翻出来的是「bili: 网络没通: GET …」
// 这种给开发者看的话），仓库里没有一条用例会红。
//
// 403 那一档走的是 `bili.ErrNetwork`（压根没拿到一份能解析的响应）→ 一句
// 「连不上 B 站，检查一下网络」。
func TestTheSearchFailureSaysSomethingTheUserCanUnderstand(t *testing.T) {
	f := newBiliFixture(t, 1)
	f.setSearchFail(http.StatusForbidden)

	s, tt := newShell(t)
	searchOnRealClient(s, f.client())
	s.navigate(searchPath + "?" + searchQuery + "=" + url.QueryEscape("周杰伦"))

	// 等失败落地（那一趟是真的网络往返）。
	waitFrames(t, tt, "拉失败了", func() bool {
		return s.search.lists[searchTabSong].list.Paged().Err != nil
	})

	want := "连不上 B 站，检查一下网络"
	if !tt.HasText(want) {
		t.Errorf("失败那一格没有说人话（want %q），文字有 %q", want, tt.Texts())
	}
	// 而原始的那一句**不许**出现在界面上：那是给开发者看的。
	if tt.HasText("bili:") {
		t.Errorf("失败那一格把内部那一句原样摆出来了，文字有 %q", tt.Texts())
	}
	// 而它不是「没有结果」。
	if tt.HasText(noSearchResults.Title) {
		t.Errorf("拉不到却画成「没有结果」那一格，文字有 %q", tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 旧查询的响应迟到（评审的 major）
// ─────────────────────────────────────────────────────────────────────────────

// TestStaleResponseDoesNotReachPlayAllOnTheRealPath 是「旧响应不进全部播放」那条的
// **真实现**版本（走真的 fetchSearchTab）。
//
// 另有一条用假拉页器的同名用例在 searchpage_test.go：那条验的是界面上看得见的
// 结果（队列里只有新词那几首），这一条验的是闸门本身在真的拉页路里——假拉页器会
// 给自己照抄一道闸门，真实现里删掉了它，假的那条照样绿。
//
// 做法和评审的探针一样：先把旧查询的第一页卡住，换词让新查询先落地，再放行旧的那一趟。
func TestStaleResponseDoesNotReachPlayAllOnTheRealPath(t *testing.T) {
	f := newBiliFixture(t, 1)
	f.hold("旧词")

	s, tt := newShell(t)
	searchOnRealClient(s, f.client())
	s.navigate(searchPath + "?" + searchQuery + "=" + url.QueryEscape("旧词"))
	settleText(t, tt, "正在加载") // 旧查询那一页被卡住了

	s.navigate(searchPath + "?" + searchQuery + "=" + url.QueryEscape("新词"))
	settleText(t, tt, "只有 bvid 的结果")

	// 放行旧的：先等它真的回到应用，再往下判。
	before := f.servedCount()
	f.release("旧词")
	waitFrames(t, tt, "迟到的旧响应回到应用", func() bool { return f.servedCount() > before })
	for range 2 {
		tt.Frame() // 从网络里回来的那一下落地（或被代次作废）
	}

	if got := len(s.search.lists[searchTabSong].snapshot()); got != 1 {
		t.Fatalf("「全部播放」那份副本里有 %d 行，want 1（迟到的旧响应漏进来了）", got)
	}
	// ⚠️ **挂的是手里这一台夹具**（`f.client()` 就是页面正在用的那个客户端），
	// 不是另起一台——生产环境取流和搜索用的是同一个客户端（见 `main.go` 那段注释）。
	s.app.MountStreaming(app.Streaming{Client: f.client(), Media: f.Client(), TempDir: t.TempDir()})
	if err := tt.Click("全部播放"); err != nil {
		t.Fatalf("点不到「全部播放」：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
	tt.Frame()
	if n := s.app.Queue().Len(); n != 1 {
		t.Errorf("队列里有 %d 项，want 1（迟到的旧响应被加进队列了）", n)
	}
}

// flacAssetPath 是那份 FLAC 素材。真解码出声要用真的字节。
func flacAssetPath(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "service", "audio", "flacdec", "testdata", "stereo16.flac")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}
	return p
}
