// UP 空间与动态流两页的测试。
//
// 断言面是**渲染结果**（画面上有什么字、行身份是什么），不是「调了哪个
// 函数」——`SPEC.md` 的 Testing Decisions 就是这么定的。
package ui

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	mygoui "github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 一台假 B 站
// ─────────────────────────────────────────────────────────────────────────────

// uiExplore 是一台给空间/动态测试用的假 B 站。
type uiExplore struct {
	*httptest.Server
	mu    sync.Mutex
	paths []string
	// queries 和 paths 一一对应：有的接口同一个路径要按查询串回答（首页那三个
	// Tab 里有两个打的是分区推荐，区别只在 from_region 上），只录路径的话
	// 「鬼畜那一档发的是音乐分区」压根测不到。
	queries  []url.Values
	handlers map[string]http.HandlerFunc
}

func newUIExplore(t *testing.T) *uiExplore {
	t.Helper()
	b := &uiExplore{handlers: map[string]http.HandlerFunc{}}
	b.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		b.paths = append(b.paths, r.URL.Path)
		b.queries = append(b.queries, r.URL.Query())
		h := b.handlers[r.URL.Path]
		b.mu.Unlock()
		if h != nil {
			h(w, r)
			return
		}
		uiExploreJSON(w, 0, map[string]any{})
	}))
	t.Cleanup(b.Close)
	// 空间/投稿/动态三个接口都要 WBI 签名，key 从这里来。
	b.on("/x/web-interface/nav", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"wbi_img": map[string]string{
			"img_url": "https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png",
			"sub_url": "https://i0.hdslb.com/bfs/wbi/4932caff0ff746eab6f01bf08b70ac45.png",
		}})
	})
	return b
}

func (b *uiExplore) on(path string, h http.HandlerFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[path] = h
}

// countHit 数打到某条路径上的请求有几回。
func (b *uiExplore) countHit(path string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, p := range b.paths {
		if p == path {
			n++
		}
	}
	return n
}

// lastQueryOf 是最后一次打给某条路径时的查询串。没有打过这一条是 nil。
func (b *uiExplore) lastQueryOf(path string) url.Values {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out url.Values
	for i, p := range b.paths {
		if p == path {
			out = b.queries[i]
		}
	}
	return out
}

func (b *uiExplore) hit(path string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, p := range b.paths {
		if p == path {
			return true
		}
	}
	return false
}

// askedRegionAfter 断的是「**点了那一档之后**打出去的那一趟里，有一趟带着 region」。
// since 是数到这一条路径上的第几次（`countHit` 那个口径）。
//
// ⚠️ **为什么不能问「最后一次」**（`lastQueryOf`）：两档的请求是**同时**在路上的
// （点下一档的时候上一档那一趟还没回来），而 fixture 记的是**到达**的先后——
// 那不是发出去的先后，满载时两趟谁先到看调度。实测（探针，同一个进程里塞满 CPU
// 跑了 200 轮）有 5 轮读到的是**上一档**的 region，于是「等最后一次是 1007」那个
// 条件永远不成立，10 秒之后保险丝断掉——功能是好的，测试挂在「哪一趟先到」上面。
func (b *uiExplore) askedRegionAfter(path string, since int, region string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for i, p := range b.paths {
		if p != path {
			continue
		}
		if n < since {
			n++
			continue
		}
		if b.queries[i].Get("from_region") == region {
			return true
		}
	}
	return false
}

func uiExploreJSON(w http.ResponseWriter, code int, data any) {
	body := map[string]any{"code": code, "message": "0", "ttl": 1, "data": data}
	raw, _ := json.Marshal(body)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

// newSpaceShell 装一个指向假 B 站的壳。
//
// `loggedIn` 决定有没有登录态：**关注按钮只在「登录了且看的不是自己」时画**
// （照 Biu `space-info.tsx:100`），所以要验那个按钮就得带登录态。
//
// `opts` 是给外壳本身的（深浅两套主题等）——**变参**，所以原来那些调用不用改。
func newSpaceShell(t *testing.T, b *uiExplore, loggedIn bool, mid string, opts ...shellOpt) (*Shell, *mygoui.Tester, *app.App) {
	t.Helper()
	sh := newTestShell(t, append([]shellOpt{mounted(func(_ *testing.T, a *app.App) {
		transport := &app.CredentialTransport{Base: b.Server.Client().Transport, Policy: app.DefaultInjectPolicy()}
		cli := bili.NewClient(bili.Options{Transport: transport})
		cli.TestOnlyPointHostsAtFixture(b.URL)
		a.MountLogin(app.NewMemoryStore("test"), cli)
		a.MountExplore(cli)
		// 在线播放也要挂上：`App.PlayFrom` 在没挂取流时会**把队列整个换回去**
		// （那是它该做的——播不了的歌不该把用户原来的队列弄丢），而挂上之后
		// `playOnline` 立刻返回 nil，队列就留住了，接下来是后台 goroutine 的事。
		a.MountStreaming(app.Streaming{Client: cli, Media: b.Server.Client()})
		if loggedIn {
			a.TestOnlyAdoptLogin(mid)
		}
	})}, opts...)...)
	return sh.s, sh.tt, sh.a
}

// showsText 报「画面上**有**这一句」（全等匹配）。
//
// ⚠️ **不要用 `tt.HasText`**：它是**子串**匹配（`ui/headless.go` 的
// HasText 文档原话就是 "containing s"），而这一页上有「我的关注」这个
// 侧栏菜单项——断言 `HasText("关注")` 的话，**关注按钮压根没画出来它也绿**。
// 这一页的作者已经栽过一次同样的坑（BMG-10 的收尾说明里记着）。
func showsText(tt *mygoui.Tester, s string) bool {
	_, ok := tt.Find(s)
	return ok
}

// showsAnyText 报「画面上**有**包含这一串的那一句」。
//
// ⚠️ **`showsText` 是全等匹配**，而「有 N 条落在别的形态上了」那一整句里
// 夹着一个**会变的数**（翻一页就多几条），全等匹配就永远匹配不上。
//
// ⚠️ 用它之前先确认这一串**不会撞上别的元素**：侧栏那个「我的关注」就让
// 「关注」两个字全等匹配不上（见 `showsText` 的注释）。
func showsAnyText(tt *mygoui.Tester, s string) bool {
	return tt.HasText(s)
}

// openSpace 切到某个 UP 的空间并等头加载完。
func openSpace(t *testing.T, s *Shell, tt *mygoui.Tester, mid int64) *upSpace {
	t.Helper()
	s.navigate(upPathOf(mid))
	tt.Frame()
	sp := s.upSpaces[mid]
	waitFor(t, "头加载完", func() bool {
		h := sp.header.Snapshot()
		return h.Loaded || h.Err != ""
	})
	tt.Frame()
	return sp
}

// ─────────────────────────────────────────────────────────────────────────────
// 假接口
// ─────────────────────────────────────────────────────────────────────────────

// uiSpaceData 装好「头 / 关系 / 粉丝数」三个接口。
func uiSpaceData(b *uiExplore, name string, attribute int, follower int64) {
	b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"mid": 42, "name": name, "sign": "这是一个签名", "level": 4,
			// ⚠️ **头像地址是量尺寸用的**：`spaceAvatar` 把它挂在 Label 上，
			// 无头 Tester 靠它找到那个盒子。空串的话头像照样画，但量不到。
			"face":         "https://f/face-42.jpg",
			"top_photo_v2": map[string]string{"l_200h_img": "https://f/banner.jpg"},
			"official":     map[string]any{"role": 0},
		})
	})
	b.on("/x/relation/stat", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"follower": follower, "following": 12})
	})
	b.on("/x/space/wbi/acc/relation", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"relation":    map[string]any{"attribute": attribute},
			"be_relation": map[string]any{"attribute": 0},
		})
	})
	b.on("/x/relation/modify", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, nil)
	})
}

// uiDynamic 是造一条动态。
func uiDynamic(id, text string) map[string]any {
	return map[string]any{
		"id_str": id,
		"type":   "DYNAMIC_TYPE_WORD",
		"modules": map[string]any{
			"module_author": map[string]any{
				"mid": 42, "name": "作者" + id, "pub_time": "3 分钟前",
			},
			"module_dynamic": map[string]any{"desc": map[string]any{"text": text}},
		},
	}
}

// uiDynamicArchive 是造一条投稿型动态。
func uiDynamicArchive(id, bvid string) map[string]any {
	m := uiDynamic(id, "")
	// ⚠️ **顶层 `type` 也要设**：真实 B 站的投稿动态就是
	// `DYNAMIC_TYPE_AV`（`parseDynamic` 拿它当闸门）。早一版这个 fixture 只设了
	// `major.type`，于是那条动态在新的闸门下被当成「未知的纯文字」丢掉了。
	m["type"] = "DYNAMIC_TYPE_AV"
	mods := m["modules"].(map[string]any)
	mods["module_dynamic"].(map[string]any)["major"] = map[string]any{
		"type": "MAJOR_TYPE_ARCHIVE",
		"archive": map[string]any{
			"bvid": bvid, "title": "投稿 " + bvid, "cover": "https://f/c.jpg",
			"desc": "投稿简介", "duration_text": "3:20",
			"stat": map[string]any{"play": "1.2万"},
		},
	}
	return m
}

// uiDynamicOpus 是造一条图文动态。
//
// ⚠️ **`desc.text` 和 `opus.summary.text` 造的是同一句话**，那正是 B 站正常
// 返回的形状（上游两处都 `desc?.text || opus?.summary?.text`）。早一版这个
// fixture 把 summary 留空，于是「正文被画两遍」那个 bug 恰好被它遮住了。
func uiDynamicOpus(id, text string, pics ...string) map[string]any {
	m := uiDynamic(id, text)
	items := make([]any, 0, len(pics))
	for _, p := range pics {
		items = append(items, map[string]any{"url_src": p})
	}
	// 顶层 `type` 也要设（见 uiDynamicArchive 那一段的注释）：带图动态在
	// B 站那边是 `DYNAMIC_TYPE_DRAW`。
	m["type"] = "DYNAMIC_TYPE_DRAW"
	mods := m["modules"].(map[string]any)
	mods["module_dynamic"].(map[string]any)["major"] = map[string]any{
		"type": "MAJOR_TYPE_OPUS",
		"opus": map[string]any{
			"summary": map[string]any{"text": text},
			"pics":    items,
		},
	}
	return m
}

// uiDynamicArchiveWithoutBvid 造一条**投稿型、却没有稿件号**的动态。
//
// ⚠️ **它带着正文**（`desc.text` 里有那段话）：这一条和「什么也没有」是两回事
// ——只是 `major.archive.bvid` 是空的，放不出来（票 #344 的评审）。
func uiDynamicArchiveWithoutBvid(id, text string) map[string]any {
	m := uiDynamic(id, text)
	m["type"] = "DYNAMIC_TYPE_AV"
	mods := m["modules"].(map[string]any)
	mods["module_dynamic"].(map[string]any)["major"] = map[string]any{
		"type":    "MAJOR_TYPE_ARCHIVE",
		"archive": map[string]any{"title": "一个没有稿件号的投稿"},
	}
	return m
}

// uiDynamicUnrenderable 是造一条这一版画不出来的动态（转发/直播/剧集…）。
//
// ⚠️ `withText` 决定它带不带正文——**带正文的那一条才是真考题**：
// 转发件在 B 站返回里是有 `desc.text` 的，而早一版的 fixture 把所有未知
// 类型的正文都清空了，于是「未知类型被当成纯文字画出来」那个 bug 恰好
// 被它遮住了。
func uiDynamicUnrenderable(id, kind string, withText ...string) map[string]any {
	m := uiDynamic(id, "")
	if len(withText) > 0 {
		m = uiDynamic(id, withText[0])
	}
	mods := m["modules"].(map[string]any)
	dyn := mods["module_dynamic"].(map[string]any)
	dyn["desc"] = map[string]any{"text": ""}
	dyn["major"] = map[string]any{"type": kind, kind: map[string]any{"title": id}}
	m["type"] = "DYNAMIC_TYPE_" + kind
	return m
}

// uiDynamicUnreadable 造一条**我们读不懂**的动态（B 站改了返回的形状）。
//
// ⚠️ `major.opus` 给的不是对象，而 `parseDynamic` 把 `opus` 声明成结构体
// ——`encoding/json` 遇到类型对不上会让**整条**读不出来（票 #300 踩过的那
// 一类）。这一条是「读不懂」，**不是**「形态不认识」：B 站给的是它一直在给
// 的那种形态，是我们读不出来（票 #344）。
func uiDynamicUnreadable(id string) map[string]any {
	m := uiDynamic(id, "正文")
	mods := m["modules"].(map[string]any)
	mods["module_dynamic"].(map[string]any)["major"] = map[string]any{
		"type": "MAJOR_TYPE_OPUS", "opus": "不是一个对象",
	}
	return m
}

// ─────────────────────────────────────────────────────────────────────────────
// 头部
// ─────────────────────────────────────────────────────────────────────────────

// TestTheSpaceHeaderShowsTheBannerTheNameAndTheStats 是「头图 + 关注按钮 +
// 统计」那条验收的钉子。
func TestTheSpaceHeaderShowsTheBannerTheNameAndTheStats(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "某个 UP 主", 0, 123456)
	s, tt, _ := newSpaceShell(t, b, true, "77")

	openSpace(t, s, tt, 42)

	if !tt.HasText("某个 UP 主") {
		t.Errorf("昵称没画出来，文字有 %q", tt.Texts())
	}
	if !tt.HasText("这是一个签名") {
		t.Errorf("签名没画出来，文字有 %q", tt.Texts())
	}
	if !tt.HasText("粉丝数") || !tt.HasText("12.3万") {
		t.Errorf("粉丝数没画出来（123456 该写成「12.3万」），文字有 %q", tt.Texts())
	}
	if !tt.HasText("等级") || !tt.HasText("Lv4") {
		t.Errorf("等级没画出来，文字有 %q", tt.Texts())
	}
	if !showsText(tt, "关注") {
		t.Errorf("关注按钮没画出来，文字有 %q", tt.Texts())
	}
	// 自己看自己**不画**关注按钮，也不画关注数（上游的 `hidden: !isSelf`）。
	if tt.HasText("关注数") {
		t.Errorf("看自己的时候不该画「关注数」，文字有 %q", tt.Texts())
	}
}

// TestTheStatsAreHiddenWithoutALogin 钉住「统计只在登录后画」。
//
// ⚠️ 这一条是**调研上游之后才发现的漏**：上游 `space-info.tsx:125` 那一支是
// `{user?.isLogin ? stats.map(...) : null}`——整块统计都挂在登录态下面。
//
// 理由不是「没登录不好看」，是**画出来是个假数**：`/x/relation/stat` 要 Cookie，
// 没登录时 B 站给不了粉丝数，而界面上会是一个干干净净的「粉丝数 0」——用户会
// 以为他真的一个粉丝都没有。
func TestTheStatsAreHiddenWithoutALogin(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "没登录看的 UP", 0, 123456)
	s, tt, _ := newSpaceShell(t, b, false, "")

	openSpace(t, s, tt, 42)

	if showsText(tt, "粉丝数") {
		t.Errorf("没登录还画着「粉丝数」（B 站那时给不了这个数，画出来是 0 = 假数），文字有 %q", tt.Texts())
	}
	if showsText(tt, "等级") {
		t.Errorf("统计整块应该一起藏（上游把它算在 stats 里），文字有 %q", tt.Texts())
	}
	// 头图和昵称还是要的——那两样不需要登录。
	if !showsText(tt, "没登录看的 UP") {
		t.Errorf("没登录也该看得见昵称，文字有 %q", tt.Texts())
	}
}

// TestTheStatsAreThereWithALogin 是上一条的反证。
func TestTheStatsAreThereWithALogin(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "登录后看的 UP", 0, 123456)
	s, tt, _ := newSpaceShell(t, b, true, "77")

	openSpace(t, s, tt, 42)

	for _, want := range []string{"粉丝数", "12.3万", "等级", "Lv4"} {
		if !showsText(tt, want) {
			t.Errorf("登录后该有 %q，文字有 %q", want, tt.Texts())
		}
	}
}

// TestTheHeaderTellsBlockedApartFromNotFollowed 钉住「拉黑是一个独立的状态」。
//
// 把拉黑画成「没关注」的话，用户按下去发出去的是**取关**，而 B 站对拉黑中
// 的用户回的是另一个码——界面上就成了一句看不懂的错。
func TestTheHeaderTellsBlockedApartFromNotFollowed(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "被拉黑的 UP", 128, 10)
	s, tt, _ := newSpaceShell(t, b, true, "77")

	openSpace(t, s, tt, 42)

	if !tt.HasText("你把他拉黑了") {
		t.Errorf("拉黑的状态没画出来，文字有 %q", tt.Texts())
	}
	if showsText(tt, "关注") {
		t.Errorf("拉黑了还画一个「关注」按钮：按下去发出去的是取关。文字有 %q", tt.Texts())
	}
}

// TestAMutualFollowStillSaysFollowed 是「已关注是**两个**值」那条在
// 界面上的钉子。
//
// 只认 attribute=2 的话互粉的 UP 主身上按钮会一直写着「关注」，按下去
// B 站回「你已经关注过他了」而关系一点没变。
func TestAMutualFollowStillSaysFollowed(t *testing.T) {
	for _, attr := range []int{2, 6} {
		t.Run("attribute="+strconv.Itoa(attr), func(t *testing.T) {
			b := newUIExplore(t)
			uiSpaceData(b, "已关注的 UP", attr, 1)
			s, tt, _ := newSpaceShell(t, b, true, "77")

			openSpace(t, s, tt, 42)

			if !showsText(tt, "已关注") {
				t.Errorf("attribute=%d 时按钮该写「已关注」，文字有 %q", attr, tt.Texts())
			}
		})
	}
}

// TestFollowingChangesTheButtonOnScreen 钉住「按下去之后按钮真的会变」。
func TestFollowingChangesTheButtonOnScreen(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "要点关注的 UP", 0, 1)
	s, tt, _ := newSpaceShell(t, b, true, "77")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]

	if !showsText(tt, "关注") {
		t.Fatalf("一开始该是「关注」，文字有 %q", tt.Texts())
	}
	if err := tt.Click("关注"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "关系变成已关注", func() bool {
		return sp.header.Snapshot().Relation.Followed()
	})
	tt.Frame()
	if !showsText(tt, "已关注") {
		t.Errorf("按钮没变成「已关注」，文字有 %q", tt.Texts())
	}
}

// TestTheAddressWithoutAnIDSaysWhichOneItWas 钉住「路径里没有 id」那一支。
//
// 用户是从一个能点的地方进来的，给一句「暂无内容」等于让他以为自己点错了
// 而其实点对了。
func TestTheAddressWithoutAnIDSaysWhichOneItWas(t *testing.T) {
	b := newUIExplore(t)
	s, tt, _ := newSpaceShell(t, b, false, "")

	s.router.Push("/user/abc")
	tt.Frame()

	if !tt.HasText("这个地址里没有 UP 主的编号") {
		t.Errorf("没说是路径不对，文字有 %q", tt.Texts())
	}
	if !tt.HasText("/user/abc") {
		t.Errorf("没把那一条路径原样说出来，文字有 %q", tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 四个 Tab
// ─────────────────────────────────────────────────────────────────────────────

// TestTheFourTabsAreThereInBiuOrder 钉住四个 Tab 的名字和顺序。
//
// 用户在这个窗口里看过上游，顺序反了他要找的那一格就不在原来的位置上。
func TestTheFourTabsAreThereInBiuOrder(t *testing.T) {
	want := []string{"动态", "投稿", "收藏夹", "合集"}
	if len(upTabNames) != len(want) {
		t.Fatalf("Tab 有 %d 个，want %d", len(upTabNames), len(want))
	}
	for i, w := range want {
		if upTabNames[i] != w {
			t.Errorf("第 %d 个 Tab 是 %q，want %q", i+1, upTabNames[i], w)
		}
	}
}

// TestEachTabReachesItsOwnEndpoint 是「四个 Tab 各自的数据接口都接上了」
// 那条验收的钉子。
//
// 切一遍四个 Tab，断言**每个接口都被请求过**。四个接口的路径长得都像，
// 而打错了的症状是 -403「访问权限不足」——那句话和「路径抄错了」没有关系。
func TestEachTabReachesItsOwnEndpoint(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "四个 Tab 的 UP", 0, 1)
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"items": []any{uiDynamic("d1", "第一条动态")}, "offset": "", "has_more": false,
		})
	})
	b.on("/x/space/wbi/arc/search", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"list": map[string]any{"vlist": []any{
				map[string]any{"bvid": "BV1", "title": "一个投稿", "length": "3:20", "cid": "9"},
			}},
			"page": map[string]any{"count": 1},
		})
	})
	b.on("/x/v3/fav/folder/created/list", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"list": []any{
			map[string]any{"id": 100, "title": "一个收藏夹", "media_count": 3},
		}})
	})
	b.on("/x/polymer/web-space/seasons_series_list", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items_lists": map[string]any{
			"seasons_list": []any{map[string]any{"meta": map[string]any{
				"season_id": 11, "name": "一个合集", "total": 4,
			}}},
			"page": map[string]any{"total": 1},
		}})
	})

	s, tt, _ := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]

	for tab := range upTabNames {
		sp.tab = tab
		tt.Frame()
		// 切 Tab 会 reload 那一摊，第一页是发出去之后回来的。
		waitFrames(t, tt, "Tab "+upTabNames[tab]+" 加载完", func() bool {
			if tab == upTabVideo {
				return sp.videos.Paged().Loaded > 0
			}
			return sp.pagerOf(tab).started
		})
		tt.Frame()
	}

	for _, path := range []string{
		"/x/polymer/web-dynamic/v1/feed/space",
		"/x/space/wbi/arc/search",
		"/x/v3/fav/folder/created/list",
		"/x/polymer/web-space/seasons_series_list",
	} {
		if !b.hit(path) {
			t.Errorf("没有一个请求打到 %s", path)
		}
	}
}

// TestTheEmptyStatesSayWhatToDoInsteadOfWhiteSpace 钉住「空的时候说一句话」。
func TestTheEmptyStatesSayWhatToDoInsteadOfWhiteSpace(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "什么都没有的 UP", 0, 1)
	for _, p := range []string{
		"/x/polymer/web-dynamic/v1/feed/space", "/x/v3/fav/folder/created/list",
		"/x/polymer/web-space/seasons_series_list",
	} {
		b.on(p, func(w http.ResponseWriter, r *http.Request) {
			uiExploreJSON(w, 0, map[string]any{"items": []any{}, "has_more": false})
		})
	}
	b.on("/x/space/wbi/arc/search", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"list": map[string]any{"vlist": nil}, "page": map[string]any{"count": 0},
		})
	})

	s, tt, _ := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]

	for tab, want := range map[int]string{
		upTabDynamic:    "暂无动态",
		upTabVideo:      "暂无投稿",
		upTabCollection: "暂无收藏夹",
		upTabSeries:     "暂无合集",
	} {
		sp.tab = tab
		tt.Frame()
		waitFrames(t, tt, "Tab "+upTabNames[tab]+" 空了", func() bool {
			if tab == upTabVideo {
				p := sp.videos.Paged()
				return !p.Loading && p.Loaded == 0
			}
			p := sp.pagerOf(tab)
			return p.started
		})
		tt.Frame()
		if !tt.HasText(want) {
			t.Errorf("%s 那一张空着时没说一句（want %q），文字有 %q", upTabNames[tab], want, tt.Texts())
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 头图区不能被压成零宽
// ─────────────────────────────────────────────────────────────────────────────

// TestTheHeaderIsNotSqueezedToZeroWidth 钉住「铺满是 `FillWidth()`，不是
// `Width(0)`」。
//
// ⚠️ **`Width(v)` 的单位是 DIP**（`mygo/ui/element.go:755-756`），而布局里
// 「只要设过宽度就按它算」（`layout.go:312`）——所以 `Width(0)` 就是**宽 0**，
// 不是「没设宽度、随容器」。写成那样时整个头图区被压成零宽：昵称只剩一个
// 省略号、头图永远画不出来、统计折成两行。
//
// 判据是**量出来的盒子宽**，不是「有没有画出来」：文字照样在，节点照样建，
// 只有宽度在骗人。
func TestTheHeaderIsNotSqueezedToZeroWidth(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "一个有很长签名的 UP 主", 0, 123456)
	s, tt, _ := newSpaceShell(t, b, true, "77")
	openSpace(t, s, tt, 42)

	// ① 昵称和签名那两句**必须够宽**。零宽时实测是 27.3（只剩一个「⋯」）；
	// 修好之后同一个窗口里是 184.3。取 100 做线：够宽裕，又不会因为字体
	// 差异误报。
	for _, what := range []string{"一个有很长签名的 UP 主", "这是一个签名"} {
		r, ok := tt.Find(what)
		if !ok {
			t.Fatalf("画面上找不到 %q，文字有 %q", what, tt.Texts())
		}
		if r.W < 100 {
			t.Errorf("%q 的盒子宽只有 %.1f，want ≥ 100 —— 头图区被压成零宽了"+
				"（多半是拿 `Width(0)` 当成了「铺满」）", what, r.W)
		}
	}

	// ② 统计那几格必须**在同一条横线上**。零宽时它们被折成两行，而「折行」
	// 是量得出来的：同一个头图区里，数字在「粉丝数」下面多少点。
	fan, ok1 := tt.Find("粉丝数")
	lv, ok2 := tt.Find("等级")
	if !ok1 || !ok2 {
		t.Fatalf("统计没画出来（粉丝数 %v / 等级 %v），文字有 %q", ok1, ok2, tt.Texts())
	}
	if dy := abs32(lv.Y - fan.Y); dy > 2 {
		t.Errorf("「粉丝数」(y=%.1f) 和「等级」(y=%.1f) 不在同一条横线上（差 %.1f）"+
			"——统计被折成了两行", fan.Y, lv.Y, dy)
	}
	if fan.W <= 0 || lv.W <= 0 {
		t.Errorf("统计那一格被压成零宽了（%.0f / %.0f）", fan.W, lv.W)
	}
}

// abs32 是两个 float32 的差的绝对值。
func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// TestTheFailedHeaderSaysSoAndOffersARetry 钉住「失败之后只有人能让它再来一次」。
//
// 两个坑叠在一起：①`viewUpSpace` 每帧问一次「还没加载过吗」，而失败只把
// `Loading` 落回 false、`Loaded` 仍是 false——于是头图一失败就是**每帧发一次
// 请求**（实测 300 毫秒 138 次）；②「再试一次」按钮被零宽压成 0×0，点不到，
// 于是那 138 次永远停不下来。
func TestTheFailedHeaderSaysSoAndOffersARetry(t *testing.T) {
	b := newUIExplore(t)
	b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, -403, nil)
	})
	b.on("/x/relation/stat", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, -403, nil)
	})
	s, tt, _ := newSpaceShell(t, b, true, "77")

	s.navigate(upPathOf(42))
	tt.Frame()
	sp := s.upSpaces[42]
	waitFor(t, "头加载完（失败也是完）", func() bool {
		h := sp.header.Snapshot()
		return h.Loaded || h.Err != ""
	})

	// 画 300 毫秒的帧：失败之后**不许**再发请求。
	for i := 0; i < 30; i++ {
		tt.Frame()
	}
	if n := countRequests(b, "/x/space/wbi/acc/info"); n > 1 {
		t.Errorf("头图加载失败之后还请求了 %d 次（>1）：`viewUpSpace` 每帧都重发"+
			"——失败只把 Loading 落回 false 而 Loaded 仍是 false", n)
	}

	if !showsText(tt, "打不开这个 UP 主的空间") {
		t.Fatalf("没说打不开，文字有 %q", tt.Texts())
	}
	// 按钮必须**量得出来、点得到**。被零宽压成 0×0 的按钮 `tt.Find` 找得到
	// 文字，但那个 0×0 的盒子点下去什么都不会发生。
	r, ok := tt.Find("再试一次")
	if !ok {
		t.Fatalf("没有重试按钮，文字有 %q", tt.Texts())
	}
	if r.W <= 0 || r.H <= 0 {
		t.Fatalf("「再试一次」是 %.0fx%.0f，点不到", r.W, r.H)
	}
	// 修好假 B 站再点一次：它必须能重来。
	b.on("/x/space/wbi/acc/info", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"mid": 42, "name": "修好了", "level": 4,
			"official": map[string]any{"role": 0}})
	})
	if err := tt.Click("再试一次"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "重试之后加载成功", func() bool {
		return sp.header.Snapshot().Loaded
	})
	tt.Frame()
	if !showsText(tt, "修好了") {
		t.Errorf("重试之后还是没加载出来，文字有 %q", tt.Texts())
	}
}

// countRequests 数打到某条路径上的请求有几回。
func countRequests(b *uiExplore, path string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, p := range b.paths {
		if p == path {
			n++
		}
	}
	return n
}

// ─────────────────────────────────────────────────────────────────────────────
// 拉失败要说失败
// ─────────────────────────────────────────────────────────────────────────────

// TestAFailedTabSaysItFailedAndOffersARetry 钉住「拉失败了」不能画成「没有」。
//
// 头一版把空状态排在错误前面：第一页拉失败时界面上是「暂无动态 / 暂无收藏夹
// / 暂无合集」——一句话都不提失败，而闸门因为 `err != nil` 一直挡着重试，
// 切走再切回来也不会重来。用户看到的是「他真的什么都没发」。
func TestAFailedTabSaysItFailedAndOffersARetry(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "接口全挂的 UP", 0, 1)
	for _, p := range []string{
		"/x/polymer/web-dynamic/v1/feed/space",
		"/x/v3/fav/folder/created/list",
		"/x/polymer/web-space/seasons_series_list",
	} {
		b.on(p, func(w http.ResponseWriter, r *http.Request) {
			uiExploreJSON(w, -412, nil) // 被风控
		})
	}
	s, tt, _ := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]

	for _, tab := range []int{upTabDynamic, upTabCollection, upTabSeries} {
		sp.tab = tab
		tt.Frame()
		waitFrames(t, tt, upTabNames[tab]+" 拉完（失败也是完）", func() bool {
			p := sp.pagerOf(tab)
			return p.started || p.err != nil
		})
		tt.Frame()

		if showsText(tt, upTabEmpty(tab, bili.DropCounts{}).Title) {
			t.Errorf("%s 拉失败了，界面上却是空状态 %q——用户会以为他真的没有",
				upTabNames[tab], upTabEmpty(tab, bili.DropCounts{}).Title)
		}
		if !showsText(tt, "这一块没拉出来") {
			t.Errorf("%s 拉失败了却没说失败，文字有 %q", upTabNames[tab], tt.Texts())
		}
		label := "重试加载" + upTabNames[tab]
		r, ok := tt.Find(label)
		if !ok {
			t.Errorf("%s 拉失败了却没有重试按钮（%q），文字有 %q", upTabNames[tab], label, tt.Texts())
			continue
		}
		if r.W <= 0 || r.H <= 0 {
			t.Errorf("%s 的重试按钮是 %.0fx%.0f，点不到", upTabNames[tab], r.W, r.H)
		}
	}
}

// TestARetryAfterAFailureActuallyReloads 钉住「那个重试是真能用的」。
//
// 光画一个按钮不够：闸门的 `want()` 因为 `err != nil` 一直挡着，所以按钮
// 必须先把 err 清掉再开一拉。
func TestARetryAfterAFailureActuallyReloads(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "先挂后好的 UP", 0, 1)
	var fail = true
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		if fail {
			uiExploreJSON(w, -412, nil)
			return
		}
		uiExploreJSON(w, 0, map[string]any{
			"items": []any{uiDynamic("d1", "重试之后拿到了")}, "offset": "", "has_more": false})
	})
	s, tt, _ := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]
	sp.tab = upTabDynamic
	tt.Frame()
	waitFrames(t, tt, "动态拉完（失败也是完）", func() bool {
		return sp.dyn.started || sp.dyn.err != nil
	})
	tt.Frame()

	fail = false
	if err := tt.Click("重试加载" + upTabNames[upTabDynamic]); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "重试之后拉到了", func() bool { return len(sp.dynItems) == 1 })
	tt.Frame()
	if !showsText(tt, "重试之后拿到了") {
		t.Errorf("重试之后没画出来，文字有 %q", tt.Texts())
	}
	if sp.dyn.err != nil {
		t.Errorf("重试成功了但闸门上还留着错：%v", sp.dyn.err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 🔴 铁律四：行身份
// ─────────────────────────────────────────────────────────────────────────────

// TestADynamicKeepsItsIdentityWhenOneIsInsertedAbove 是铁律四在这两页上的
// 钉子，**也是这一票点名要写的那条测试**。
//
// MyGo 默认拿「父 ID + 兄弟序号」当身份。动态流刷新时新动态会插到**前面**
// （B 站就是按时间倒着给的），拿序号当身份的话：顶上那一行换了人，
// 整个视口跟着新来的那条走，用户盯着的那一条整个掉出画面——而界面上
// 看不出任何异样。
//
// 判据是**每一行的 key**：头部插一条之后，原来那些行一条都不能换身份。
// 这正是框架按 key 重新对位（滚动锚点、选中项）所依据的那个东西。
func TestADynamicKeepsItsIdentityWhenOneIsInsertedAbove(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "发动态的 UP", 0, 1)
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		items := make([]any, 0, 3)
		for _, id := range []string{"d1", "d2", "d3"} {
			items = append(items, uiDynamic(id, "动态 "+id))
		}
		uiExploreJSON(w, 0, map[string]any{"items": items, "offset": "O", "has_more": false})
	})
	s, tt, _ := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]
	waitFrames(t, tt, "动态加载完", func() bool { return len(sp.dynItems) == 3 })
	sp.tab = upTabDynamic
	tt.Frame()

	before := rowKeys(sp, upTabDynamic, 3)
	if len(before) != 3 {
		t.Fatalf("头插之前只有 %d 行", len(before))
	}

	// 在**最前面**插一条（模拟刷新：B 站就是按时间倒着给的，新动态排在最前）。
	sp.dynItems = append([]bili.Dynamic{{
		ID: "newest", Kind: bili.DynamicText, Text: "刚发的一条",
	}}, sp.dynItems...)
	tt.Frame()

	after := rowKeys(sp, upTabDynamic, 4)
	if len(after) != 4 {
		t.Fatalf("头插之后有 %d 行，want 4", len(after))
	}
	if after[0] != "newest" {
		t.Errorf("新插的那一条身份是 %v，want newest", after[0])
	}
	// **原来那三条各自往后挪了一格，身份一条都不能变。**
	//
	// 这才是铁律四要的东西：框架按 key 重新对位滚动锚点和选中项，key 跟着
	// 内容走才有着落。拿序号当身份的话这里会全变成「新插那条、d1、d2」，
	// 而顶上那一行换成了刚发的那条——用户盯着的那一条整个掉出画面，
	// 界面上看不出任何异样。
	for i := range before {
		if after[i+1] != before[i] {
			t.Errorf("头部插入一条之后，原来第 %d 行（身份 %v）变成了身份 %v"+
				"（框架按 key 对位，key 变了就是状态整体错配）",
				i, before[i], after[i+1])
		}
	}
}

// TestLoggedOutSpaceDoesNotRefetchTheHeaderOnEveryFrame 钉住「未登录打开空间
// 不会每帧重拉头部」——**在真的帧上**。
//
// ⚠️ 这是评审第三轮抓到的、我自己引入的自激循环。应用层那条
// （`explore_test.go` 的 `TestLoggedOutHeaderDoesNotRefetchForever`）只能钉住
// 状态和那次白花的请求；「每帧重拉」这件事**要跑帧才看得见**：
// `viewUpSpace` 每帧按 `NeedStat()` 问一次「还要不要再拉」，而
// `NeedStat` 一旦为真、`Load` 的结果又唤醒界面（铁律三）——唤醒正是循环的
// 那一环。实测未登录、接口全部成功时，8 次空闲帧发了 10 次请求。
//
// 判据是**请求次数**：8 帧之后仍然只有 1 次。
func TestLoggedOutSpaceDoesNotRefetchTheHeaderOnEveryFrame(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "未登录看的 UP", 0, 100)
	b.on("/x/space/wbi/acc/relation", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, -101, nil) // 没登录：那个接口要 Cookie
	})
	s, tt, _ := newSpaceShell(t, b, false, "")

	openSpace(t, s, tt, 42)
	// 空闲八帧。每一帧都会问一次「还要不要再拉」。
	for range 8 {
		tt.Frame()
	}

	if n := b.countHit("/x/space/wbi/acc/info"); n != 1 {
		t.Errorf("8 帧之后 acc/info 被请求了 %d 次，want 1 —— `NeedStat` 在未登录时"+
			"必须是 false，否则每帧重拉（自己叫醒自己）", n)
	}
	if n := b.countHit("/x/relation/stat"); n != 0 {
		t.Errorf("未登录还查了 %d 次关注数（界面那一块没登录时不画，查它是白花）", n)
	}
}

// TestTheSpaceDynamicTabDoesNotAskTheSamePageAgain 钉住「反复刷」那一条。
//
// 🔴 **实测踩过的坑**（票 #300，2026-10-24）：UP 空间的「动态」那一格
// **一直转圈、一直拿不到内容**，而日志里那个动态接口**每一帧都被问一次**。
// 两个原因叠在一起：
//
//   - **B 站把 `module_author.pub_ts` 从数字改成了字符串**，于是
//     `encoding/json` 整个失败，每一条动态都被当成「读不懂」丢掉——
//     一页 12 条一条不剩（这一条由 `bili` 包的测试钉着）。
//   - **B 站的 `has_more` 和 `offset` 不是一起可信的**：入口没前进还说着
//     「还有下一页」，于是同一个入口被反复问。
//
// 判据是**请求次数**和**画面上那一句话**：三种情形（空响应 / 失败 / 全被
// 丢弃）都必须是「问一次就停」，而且画面上要说清楚是哪一种——不是一直转圈。
func TestTheSpaceDynamicTabDoesNotAskTheSamePageAgain(t *testing.T) {
	cases := []struct {
		name string
		// reply 是那一台假 B 站怎么答。n 是它是第几次被问。
		reply func(w http.ResponseWriter, r *http.Request, n int)
		// wantAsk 是**最多**能问几次。
		wantAsk int
		// wantSay 是画面上该有的一句话（`showsText` 是全等匹配）。
		wantSay string
	}{
		{
			name: "空响应",
			reply: func(w http.ResponseWriter, r *http.Request, n int) {
				uiExploreJSON(w, 0, map[string]any{
					"items": []any{}, "offset": "", "has_more": true,
				})
			},
			wantAsk: 1,
			wantSay: "暂无动态",
		},
		{
			name: "失败",
			reply: func(w http.ResponseWriter, r *http.Request, n int) {
				uiExploreJSON(w, -101, nil)
			},
			wantAsk: 1,
			wantSay: "这一块没拉出来",
		},
		{
			name: "全被丢弃",
			reply: func(w http.ResponseWriter, r *http.Request, n int) {
				uiExploreJSON(w, 0, map[string]any{
					"items":  []any{uiDynamicUnrenderable("f1", "forward", "转发正文")},
					"offset": "", "has_more": true,
				})
			},
			wantAsk: 1,
			wantSay: "落在别的形态上了",
		},
		{
			// ⚠️ **入口不前进的那一种**：第一页给了入口 "O1"，第二页又回了
			// 同一个 "O1"——再问一次还是同一页，所以到第二页就该收手。
			name: "游标不前进",
			reply: func(w http.ResponseWriter, r *http.Request, n int) {
				uiExploreJSON(w, 0, map[string]any{
					"items":  []any{uiDynamicUnrenderable("f1", "forward", "转发正文")},
					"offset": "O1", "has_more": true,
				})
			},
			wantAsk: 2,
			wantSay: "落在别的形态上了",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newUIExplore(t)
			uiSpaceData(b, "某个 UP 主", 0, 1)
			var mu sync.Mutex
			asked := 0
			b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				asked++
				n := asked
				mu.Unlock()
				c.reply(w, r, n)
			})
			s, tt, _ := newSpaceShell(t, b, false, "")
			openSpace(t, s, tt, 42)

			// **画几十帧，一帧都不滚**。
			//
			// ⚠️ 这是这一条的全部意义：早一版那个圈是**每一帧**都要一次那个
			// 接口（60 帧 40 次），而「画几帧就停」是骗不出这个的。
			for range 40 {
				tt.Frame()
			}

			mu.Lock()
			n := asked
			mu.Unlock()
			if n > c.wantAsk {
				t.Errorf("画了 40 帧，那个动态接口被问了 %d 次，want 最多 %d"+
					"（同一个入口被反复问 = 用户看到的就是「一直转圈」）", n, c.wantAsk)
			}
			if !showsAnyText(tt, c.wantSay) {
				t.Errorf("画面上没有 %q，文字有 %q", c.wantSay, tt.Texts())
			}
		})
	}
}

// TestTheFavoritesTabReachesTheSecondPage 钉住「收藏夹能翻到第二页」。
//
// ⚠️ 早一版把这个接口当成「一次给全」的（那**是错的**），于是超过 20 个
// 收藏夹的 UP 主，**第 21 个永远看不到** —— 界面上是「已加载 20、没有更多」。
func TestTheFavoritesTabReachesTheSecondPage(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "有 21 个收藏夹的 UP", 0, 1)
	b.on("/x/v3/fav/folder/created/list", func(w http.ResponseWriter, r *http.Request) {
		pn := r.URL.Query().Get("pn")
		list := []any{}
		switch pn {
		case "1":
			for i := range 20 {
				list = append(list, map[string]any{"id": i + 1, "title": "收藏夹", "media_count": 1})
			}
		case "2":
			list = append(list, map[string]any{"id": 21, "title": "第 21 个收藏夹", "media_count": 1})
		default:
			// 后面没有页了
		}
		uiExploreJSON(w, 0, map[string]any{"count": 21, "list": list, "has_more": pn == "1"})
	})
	s, tt, _ := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]
	sp.tab = upTabCollection
	tt.Frame()
	waitFrames(t, tt, "第一页收藏夹到了", func() bool { return len(sp.cols[upTabCollection]) == 20 })
	tt.Frame()

	// 滚到底 → 哨兵 → 第二页 → **再滚一次**。
	//
	// ⚠️ 第二次滚不是多余的：第二页只有 1 个收藏夹，而断的正是**末尾**那一个。
	// 行数一变，滚动位置就停在旧的高度上，末尾那一个还在画面下面——合成一个
	// 固定的「滚 10 下」就是在赌第二页在那 10 下之内回来（理由见 `scrollToEnd`）。
	scrollUpTabToEnd(t, tt, sp, upTabCollection)
	waitFrames(t, tt, "第二页收藏夹到了", func() bool {
		return len(sp.cols[upTabCollection]) == 21
	})
	tt.Frame()
	scrollUpTabToEnd(t, tt, sp, upTabCollection)
	if !showsText(tt, "第 21 个收藏夹") {
		t.Errorf("第 21 个收藏夹看不到（界面显示 %d 个）", len(sp.cols[upTabCollection]))
	}
}

// 滚轮落在 UP 空间那一格上的那一点。
const upSpaceScrollX, upSpaceScrollY float32 = 600, 500

// scrollUpTabToEnd 滚到 UP 空间某一张 Tab 的列表末尾，**等末尾真的在画面上**。
//
// 判据是 `ListState.AtEnd()`——框架自己判「这一帧露出了末尾」用的那一个。
func scrollUpTabToEnd(t *testing.T, tt *mygoui.Tester, sp *upSpace, tab int) {
	t.Helper()
	scrollToEnd(t, tt, upSpaceScrollX, upSpaceScrollY, "这一档列表的末尾",
		sp.state[tab].AtEnd)
}

// TestSwitchingTabsBackDoesNotDuplicateTheRows 钉住「切 Tab 不重拉」。
//
// ⚠️ 这一条是一个**真踩过的坑**：切 Tab 时把闸门 reload 掉（回到「第一页」）
// 而已经拿到的行还留在 `dynItems` 里，于是下一次拉回来的是**同一页**，被
// 直接追加在后面——两批一模一样的行身份。后果是框架按 key 对位时那几行共用
// 一份状态：Tester 里当场 panic，真窗口里选中一个另一个也亮，而界面上看不出
// 任何异样。
//
// 判据是**行数**和**身份两两不同**。
func TestSwitchingTabsBackDoesNotDuplicateTheRows(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "四个 Tab 的 UP", 0, 1)
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		items := make([]any, 0, 3)
		for _, id := range []string{"d1", "d2", "d3"} {
			items = append(items, uiDynamic(id, "动态 "+id))
		}
		uiExploreJSON(w, 0, map[string]any{"items": items, "offset": "", "has_more": false})
	})
	b.on("/x/v3/fav/folder/created/list", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"list": []any{
			map[string]any{"id": 100, "title": "一个收藏夹", "media_count": 3},
		}})
	})
	s, tt, _ := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]

	sp.tab = upTabDynamic
	tt.Frame()
	waitFrames(t, tt, "动态加载完", func() bool { return len(sp.dynItems) == 3 })

	sp.tab = upTabCollection
	tt.Frame()
	waitFrames(t, tt, "收藏夹加载完", func() bool { return sp.fav.started })
	tt.Frame()

	// 翻回来：既不该重拉第一页，也不该把同一页再追加一遍。
	sp.tab = upTabDynamic
	tt.Frame()
	for i := 0; i < 20; i++ {
		tt.Frame()
	}

	if got := len(sp.dynItems); got != 3 {
		t.Fatalf("切走再切回来之后动态有 %d 条，want 3（同一页被追加了第二遍）", got)
	}
	seen := map[any]bool{}
	for i := range sp.dynItems {
		k := upRowKey(sp, upTabDynamic, i)
		if seen[k] {
			t.Errorf("身份 %v 出现了两次——两行共用一份状态（Tester 里会 panic）", k)
		}
		seen[k] = true
	}
}

// TestAPageOfOnlyUnrenderableOnesStillLoadsTheNextPage 钉住「全被过滤掉的那
// 一页**不是**到底」。
//
// 🔴 实测踩过的坑：一页里全是转发的动态（全被丢弃）时留下 0 条，而闸门拿
// 「留下几条」当「B 站还有没有发东西」的判据，于是 has_more 被覆盖成 false
// ——第二页有一条正常的，**永远不再加载**，用户看到的是「这里没有能显示的
// 动态」而没有任何后续入口。
//
// 判据是**第二页那条真的画出来了**，而且 offset 被原样带回去问过一次。
func TestAPageOfOnlyUnrenderableOnesStillLoadsTheNextPage(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "第一页只发转发的 UP", 0, 1)
	var asked []string
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Query().Get("offset"))
		if r.URL.Query().Get("offset") == "NEXT" {
			uiExploreJSON(w, 0, map[string]any{
				"items":  []any{uiDynamic("good", "第二页的有效动态")},
				"offset": "", "has_more": false,
			})
			return
		}
		uiExploreJSON(w, 0, map[string]any{
			"items":  []any{uiDynamicUnrenderable("f1", "forward", "转发正文")},
			"offset": "NEXT", "has_more": true,
		})
	})
	s, tt, _ := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]

	waitFrames(t, tt, "第二页拉到了", func() bool { return len(sp.dynItems) == 1 })
	tt.Frame()

	if !showsText(tt, "第二页的有效动态") {
		t.Errorf("第一页全被丢弃之后不该停住，文字有 %q", tt.Texts())
	}
	if len(asked) < 2 || asked[1] != "NEXT" {
		t.Errorf("offset 没被原样带回去问第二页（问过 %v）", asked)
	}
}

// TestTheDroppedCountCountsDifferentDynamicsNotHowManyTimesTheyAppear 钉住
// 「丢掉的计数按**不同的动态**算」。
//
// ⚠️ B 站翻页**会重叠**：同一批画不出来的动态在两页里各出现一次。按**出现
// 次数**记的话，界面上那个数会**偏大**——这里是 2，而真相是 1（票 #344）。
//
// 判据是**界面上那句话里的数**，不是「内部记了多少次」。
func TestTheDroppedCountCountsDifferentDynamicsNotHowManyTimesTheyAppear(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "两页重叠的 UP", 0, 1)
	var mu sync.Mutex
	var asked []string
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		off := r.URL.Query().Get("offset")
		mu.Lock()
		asked = append(asked, off)
		mu.Unlock()
		// ⚠️ **两页给的是同一条**（B 站翻页重叠的形状）：同一批画不出来的
		// 动态在两页里各出现一次。
		items := []any{uiDynamicUnrenderable("f1", "forward", "转发正文")}
		if off == "" {
			uiExploreJSON(w, 0, map[string]any{"items": items, "offset": "P2", "has_more": true})
			return
		}
		uiExploreJSON(w, 0, map[string]any{"items": items, "offset": "", "has_more": false})
	})
	s, tt, _ := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]

	// 两页都到齐（第二页是哨兵自己叫起来的：这一页一行都没画出来）。
	waitFrames(t, tt, "两页都到了", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(asked) >= 2
	})
	waitFrames(t, tt, "第二页被收下了", func() bool { return !sp.dyn.hasMore })
	tt.Frame()

	if got := sp.drops[upTabDynamic].total(); got != 1 {
		t.Errorf("丢掉 %d 条，want 1（同一条在两页里各出现一次，只算一条）", got)
	}
	if !showsAnyText(tt, "有 1 条落在别的形态上了") {
		t.Errorf("界面上那个数不是 1，文字有 %q", tt.Texts())
	}
	if showsAnyText(tt, "有 2 条落在别的形态上了") {
		t.Errorf("同一条被数了两遍，文字有 %q", tt.Texts())
	}
}

// TestAnArchiveWithoutABvidIsNotCalledEmpty 钉住「投稿动态没有稿件号」**不**
// 被说成「什么也没有」。
//
// ⚠️ **这一条带着正文**：UP 写的那段话就在 `desc.text` 里，只是稿件号是空的
// ——放不出来。而「什么也没有」那一档说的是「既没有正文也没有图」。
//
// ⚠️ **说错了就是对用户说错话**：「有 1 条什么也没有，画不出来」听起来像是
// 「他什么都没发」，而真相是「他发了一条投稿，我们放不出来」（票 #344 的评审）。
//
// 判据是**画面上那句话**：说的是「投稿…放不出来」，而且**不许**说「什么也
// 没有」。
func TestAnArchiveWithoutABvidIsNotCalledEmpty(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "发了一条投稿的 UP", 0, 1)
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"items": []any{uiDynamicArchiveWithoutBvid("av1", "他写的那段话")},
			"offset": "", "has_more": false,
		})
	})
	s, tt, _ := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]
	waitFrames(t, tt, "动态加载完", func() bool { return sp.dyn.started })
	tt.Frame()

	if !showsAnyText(tt, "有 1 条是投稿却放不出来（没有稿件号）") {
		t.Errorf("没说清是「投稿放不出来」，文字有 %q", tt.Texts())
	}
	if showsAnyText(tt, "什么也没有") {
		t.Errorf("把「投稿没有稿件号」说成了「什么也没有」——"+
			"那一条明明带着正文，文字有 %q", tt.Texts())
	}
	if got := sp.drops[upTabDynamic].counts.NoArchive; got != 1 {
		t.Errorf("「投稿没有稿件号」那一类数了 %d 条，want 1", got)
	}
	if got := sp.drops[upTabDynamic].counts.Empty; got != 0 {
		t.Errorf("「什么也没有」那一类数了 %d 条，want 0（这一条带着正文）", got)
	}
}

// TestAnArchiveWithoutABvidAndWithoutTextIsCalledEmpty 钉住「投稿动态**既**没有
// 稿件号**又**没有正文」说成「什么也没有」。
//
// ⚠️ **它是上一条的那一半**：上一条量的是「有正文的投稿没有稿件号」；这一条量
// 的是**正文也是空的**那一种——它什么也没有，说成「是投稿却放不出来」是**错的**
// （那一句话说的是「他发了东西，只是我们放不出来」）。
func TestAnArchiveWithoutABvidAndWithoutTextIsCalledEmpty(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "发了一条空投稿的 UP", 0, 1)
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"items": []any{uiDynamicArchiveWithoutBvid("av1", "")},
			"offset": "", "has_more": false,
		})
	})
	s, tt, _ := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]
	waitFrames(t, tt, "动态加载完", func() bool { return sp.dyn.started })
	tt.Frame()

	if !showsAnyText(tt, "有 1 条什么也没有") {
		t.Errorf("没说清是「什么也没有」，文字有 %q", tt.Texts())
	}
	if showsAnyText(tt, "放不出来") {
		t.Errorf("把「什么也没有」说成了「是投稿却放不出来」——"+
			"这一条连正文都没有，文字有 %q", tt.Texts())
	}
	if got := sp.drops[upTabDynamic].counts.Empty; got != 1 {
		t.Errorf("「什么也没有」那一类数了 %d 条，want 1", got)
	}
	if got := sp.drops[upTabDynamic].counts.NoArchive; got != 0 {
		t.Errorf("「投稿没有稿件号」那一类数了 %d 条，want 0（这一条什么也没有）", got)
	}
}

// TestWhatWeCannotReadIsNotCalledAnUnknownShape 钉住「读不懂的形状**不**被说成
// 「形态不认识」。
//
// ⚠️ 这两件事是**两回事**：B 站给的是它一直在给的那种形态，是**我们读不出来**
// ——说成「落在别的形态上了」就是**对用户说错话**（那听起来像是那个 UP 主
// 发了我们画不出来的东西，票 #344）。
//
// 判据是**画面上有没有那句话**：说「我们读不懂」，而且**不许**说「落在别的
// 形态上了」。
func TestWhatWeCannotReadIsNotCalledAnUnknownShape(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "发了一条我们读不懂的 UP", 0, 1)
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"items": []any{uiDynamicUnreadable("u1")}, "offset": "", "has_more": false,
		})
	})
	s, tt, _ := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]
	waitFrames(t, tt, "动态加载完", func() bool { return sp.dyn.started })
	tt.Frame()

	if !showsAnyText(tt, "我们读不懂") {
		t.Errorf("没说清是「我们读不懂」，文字有 %q", tt.Texts())
	}
	if showsAnyText(tt, "落在别的形态上了") {
		t.Errorf("把「读不懂」说成了「形态不认识」，文字有 %q", tt.Texts())
	}
	if got := sp.drops[upTabDynamic].counts.Unreadable; got != 1 {
		t.Errorf("「读不懂」那一类数了 %d 条，want 1", got)
	}
	if got := sp.drops[upTabDynamic].counts.ShapeUnknown; got != 0 {
		t.Errorf("「形态不认识」那一类数了 %d 条，want 0（这一条是读不懂）", got)
	}
}

// TestAnEmptyPageWhoseOffsetMovesStillLoadsTheNextPage 钉住「0 条 + 入口前进
// 了 = 继续拉下一页」。
//
// 🔴 旧闸门在这一页就收口了：B 站发了 0 条就当「到底了」，**哪怕它给的入口
// 前进了**——后面那一页永远不会被拉（票 #344）。
//
// 判据是**第二页那条真的画出来了**。
func TestAnEmptyPageWhoseOffsetMovesStillLoadsTheNextPage(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "第一页空的 UP", 0, 1)
	var mu sync.Mutex
	var asked []string
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		off := r.URL.Query().Get("offset")
		mu.Lock()
		asked = append(asked, off)
		mu.Unlock()
		if off == "" {
			// **0 条，但入口前进了**（B 站说后面还有）。
			uiExploreJSON(w, 0, map[string]any{"items": []any{}, "offset": "O1", "has_more": true})
			return
		}
		uiExploreJSON(w, 0, map[string]any{
			"items": []any{uiDynamic("d1", "第二页的有效动态")}, "offset": "", "has_more": false,
		})
	})
	s, tt, _ := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]

	waitFrames(t, tt, "第二页拉到了", func() bool { return len(sp.dynItems) == 1 })
	tt.Frame()

	if !showsText(tt, "第二页的有效动态") {
		t.Errorf("0 条 + 入口前进了却停住了，文字有 %q", tt.Texts())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(asked) < 2 || asked[1] != "O1" {
		t.Errorf("第二页没被问（问过 %v）", asked)
	}
}

// TestAnEmptyPageWhoseOffsetStaysIsTheEnd 钉住「0 条 + 入口没前进 = 到底了」。
//
// ⚠️ 这是上一条的反证：入口没前进的时候**必须收手**——再问一次问回来的是
// 同一页，于是那个圈会一直转下去（实测：60 帧发了 40 次请求，票 #300）。
//
// 判据是**请求次数**：画几十帧，只该问一次。
func TestAnEmptyPageWhoseOffsetStaysIsTheEnd(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "空的 UP", 0, 1)
	var mu sync.Mutex
	asked := 0
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked++
		mu.Unlock()
		// **0 条，而且入口没给**（B 站说「还有」，但没给下一次要问的入口）。
		uiExploreJSON(w, 0, map[string]any{"items": []any{}, "offset": "", "has_more": true})
	})
	s, tt, _ := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]

	waitFrames(t, tt, "第一页到了", func() bool { return sp.dyn.started })
	for range 40 {
		tt.Frame()
	}
	mu.Lock()
	n := asked
	mu.Unlock()
	if n != 1 {
		t.Errorf("画了 40 帧，那个动态接口被问了 %d 次，want 1"+
			"（入口没前进就是到底，再问是问同一页）", n)
	}
}

// TestThePagerStopsWhenAnEmptyPageKeepsTheSameOffset 钉住 UI 那一半的
// 「0 条 + 入口没前进 = 到底了」。
//
// # 为什么上面那一条不够
//
// `TestAnEmptyPageWhoseOffsetStaysIsTheEnd` 给的是「入口**空串**」——而那一条
// 在**服务层**就收口了（`bili.DynamicPage.MoreAfter`：入口是空的就没有下一页），
// **UI 那一半根本没被问到**。这一条给的是「入口**没前进**」：入口还在（"O1"），
// 而 B 站又回了同一个 "O1"——再问一次问回来的还是同一页，所以到第二页就该收手。
//
// 🔴 **这一条守着 UI 那一层的闸门**（`upPager.adopt` 里的 `advanced` 那一档）：
// 第一页 B 站发了 **0 条**，而**入口前进了**（给了 "O1"）——所以界面**必须**
// 接着拉下一页（判据是 `sent > 0 || advanced`）。少了 `advanced` 那一档的话，
// 第一页就地收手，第二页永远不会被问，而后面那一页里可能有正常动态。
//
// 判据是**问了几次**：正好两次（第一页 + 入口没前进的那一次），不能再多——
// 一直问就是那个圈转不停（实测：60 帧 40 次，票 #300）。
func TestThePagerStopsWhenAnEmptyPageKeepsTheSameOffset(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "空的 UP", 0, 1)
	var mu sync.Mutex
	asked := 0
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked++
		mu.Unlock()
		// **0 条，而且入口一直是同一个 "O1"**：第一页给了 "O1"，第二页又回了
		// "O1"——再问一次问回来的还是同一页。
		uiExploreJSON(w, 0, map[string]any{"items": []any{}, "offset": "O1", "has_more": true})
	})
	s, tt, _ := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]

	// 第一页到了（它发了 0 条，可它**给了入口**——所以还要问第二页）。
	waitFrames(t, tt, "第一页到了", func() bool { return sp.dyn.started })
	for range 40 {
		tt.Frame()
	}

	mu.Lock()
	n := asked
	mu.Unlock()
	if n != 2 {
		t.Errorf("画了 40 帧，那个动态接口被问了 %d 次，want 2"+
			"（0 条 + 入口没前进就是到底，再问是问同一页；而入口前进了的那一次"+
			"必须问出去——不然后面那一页永远不会被加载）", n)
	}
	// 收手之后画面上说的是「没有动态」那一句，而不是一直转圈。
	if !showsText(tt, "暂无动态") {
		t.Errorf("收手之后画面上不是「暂无动态」，文字有 %q", tt.Texts())
	}
}

// TestTheListDoesNotPullEveryPageWithoutScrolling 钉住「滚动哨兵真的限流」。
//
// 🔴 实测踩过的坑：首屏那一拉写在 view 的开头、每一帧都跑，而闸门只判
// 「不是在拉、后面还有」——于是第一页一落地，下一帧就又发一页，**用户什么都
// 没滚，后面的页全被拉完了**（实测：一页 50 条共 4 页，没滚一下就请求了 4 次、
// 攒了 200 条；哨兵根本没机会起作用，网络和内存都在用户看不见的地方烧掉）。
//
// 判据是**请求次数**：不滚就只能是 1 次。
func TestTheListDoesNotPullEveryPageWithoutScrolling(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "有很多动态的 UP", 0, 1)
	var mu sync.Mutex
	asked := 0
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked++
		mu.Unlock()
		// **一页 50 条**（评审复现的形状）：一屏装不满，末尾那一行在画面外，
		// 哨兵问不到 —— 所以「什么都没滚就只有 1 次请求」。
		page := asked
		items := make([]any, 0, 50)
		for i := range 50 {
			items = append(items, uiDynamic(
				fmt.Sprintf("d%d-%d", page, i),
				fmt.Sprintf("第%d页的一条", page)))
		}
		uiExploreJSON(w, 0, map[string]any{
			"items": items, "offset": fmt.Sprintf("O%d", asked), "has_more": true,
		})
	})
	s, tt, _ := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]

	waitFrames(t, tt, "第一页到了", func() bool { return len(sp.dynItems) > 0 })
	// 再画几十帧，**一帧都不滚**。
	for range 40 {
		tt.Frame()
	}
	mu.Lock()
	n := asked
	mu.Unlock()
	if n > 1 {
		t.Errorf("什么都没滚就请求了 %d 次动态接口，want 1"+
			"（首屏那一拉必须在「一页都还没拉过」时才发，哨兵才是唯一的继续入口）", n)
	}
	if len(sp.dynItems) != 50 {
		t.Errorf("画面上有 %d 条，want 50（只有第一页）", len(sp.dynItems))
	}

	// 滚到底之后哨兵该接着拉下一页 —— 这条是「限流没有把翻页也一起掐死」。
	//
	// ⚠️ **这里不能等「末尾真的露出来了」**（`scrollToEnd` 的判据）：这一份 fixture
	// 的 `has_more` **永远是 true**，于是这个列表**根本没有尽头**——哨兵每次被滚进
	// 画面就再拉一页，一页 50 条。真去等那个条件的话它会一直翻下去（实测一次就滚到
	// 了第 2087 页还在拉），而那个条件永远不成立。这一条要的不是「尽头」，是
	// 「哨兵被叫起来过一次」。
	//
	// ⚠️ **一下就够**：哨兵就在 50 条的末尾（这一份 fixture 一屏装不满，早一版的
	// 注释写着「末尾那一行在画面外，哨兵问不到」），一次滚到头就把它带进画面。
	// 这是一步**动作**、不是一个时序赌——早一版是「滚 10 下」，那 10 下里九下是
	// 白滚的。
	tt.Scroll(upSpaceScrollX, upSpaceScrollY, 0, scrollFar)
	//
	// ⚠️ **等的是「第二页回来了」而不是「第二页发出去了」**：`asked` 记的是**请求**
	// （假 B 站那边记的），而断的 `len(sp.dynItems)` 是**响应**——中间隔着后台那一
	// 趟 goroutine。只等请求、画一帧就断响应，满载时那一帧走不到网络那一层，于是
	// 「限流把翻页掐死了」这个结论是假的（`waitFrames` 那段注释写的是同一件事）。
	//
	// ⚠️ **滚到底会一直翻到填满为止**（每页 50 条，一屏装不满），所以断言的是
	// 「**至少多了一页**」而不是「第 2 页」——早一版写死「第 2 页」，结果滚到
	// 第 6 页，那一句早就被顶出画面了。
	waitFrames(t, tt, "哨兵叫起的下一页回来了", func() bool { return len(sp.dynItems) > 50 })
	tt.Frame()
	mu.Lock()
	after := asked
	mu.Unlock()
	if after < 2 {
		t.Errorf("滚到底之后没有加载下一页（总共只请求了 %d 次）", after)
	}
	if got := len(sp.dynItems); got <= 50 {
		t.Errorf("滚到底之后只有 %d 条，want 超过一页（50）——哨兵被限流掐死了", got)
	}
}

// TestRowKeysAreUniqueIncludingTheSentinel 钉住「唯一」。
//
// key 撞上时 MyGo 在 Tester 里当场 panic，真窗口里则是那两行共用一份状态
// ——选中一个另一个也亮，而界面上看不出任何异样。
func TestRowKeysAreUniqueIncludingTheSentinel(t *testing.T) {
	sp := &upSpace{}
	sp.dynItems = []bili.Dynamic{{ID: "d1"}, {ID: "d2"}}
	sp.cols[upTabCollection] = []bili.UpCollection{{ID: 1, Type: 1}, {ID: 2, Type: 1}}
	sp.cols[upTabSeries] = []bili.UpCollection{{ID: 1, Type: 3}, {ID: 2, Type: 3}}

	// 每一张 Tab 里：内容 + 哨兵，两两不同。
	for _, tab := range []int{upTabDynamic, upTabCollection, upTabSeries} {
		seen := map[any]int{}
		for i := 0; i <= upRowCount(sp, tab); i++ { // 末尾那一行是哨兵
			k := upRowKey(sp, tab, i)
			if prev, dup := seen[k]; dup {
				t.Errorf("%s 里第 %d 行和第 %d 行身份撞上了（%v）", upTabNames[tab], i, prev, k)
			}
			seen[k] = i
		}
	}

	// ⚠️ **内容那一行的身份不能和哨兵那个撞上**：哨兵占了**同一个 ListState**
	// 里的一个位置，撞上的话框架会把「加载更多」那一行当成第一条内容，
	// 于是画面上是「加载更多」顶着动态，而真正的那一条永远排在它后面。
	if upRowKey(sp, upTabCollection, upRowCount(sp, upTabCollection)) == fmt.Sprint("coll:1:1") {
		t.Error("哨兵那一行用了一个内容可能用着的字符串身份")
	}
}

// TestACollectionKeyCarriesItsType 钉住「收藏夹和合集的 key 带上 type」。
//
// 两者的 id 是 B 站**两个不同的编号空间**，各自从 1 开始数——不带 type 的话
// `id=1` 的收藏夹和 `id=1` 的合集身份一样。眼下两张 Tab 各有一份
// ListState（撞不上），但 key 是要跟着内容走的：**这一页哪一天把两样摆进
// 同一张列表**（比如「合集和收藏夹」合并成一页），撞上就是那两行共用一份
// 状态，而界面上看不出任何异样。
func TestACollectionKeyCarriesItsType(t *testing.T) {
	sp := &upSpace{}
	sp.cols[upTabCollection] = []bili.UpCollection{{ID: 1, Type: 1}}
	sp.cols[upTabSeries] = []bili.UpCollection{{ID: 1, Type: 3}}

	fav := upRowKey(sp, upTabCollection, 0)
	ser := upRowKey(sp, upTabSeries, 0)
	if fav == ser {
		t.Errorf("id 都是 1 的收藏夹和合集身份撞上了：%v", fav)
	}
	if !strings.HasPrefix(fmt.Sprint(fav), "coll:1:") {
		t.Errorf("收藏夹那一行的身份是 %v，want coll:1:…", fav)
	}
}

// TestEveryRowHasALabelAndACollectionRowGoesToItsPage 钉住两样小事：
// 每一行都给无障碍留了名字；收藏夹那一行点了会跳。
func TestEveryRowHasALabelAndACollectionRowGoesToItsPage(t *testing.T) {
	sp := &upSpace{}
	sp.cols[upTabCollection] = []bili.UpCollection{{ID: 100, Type: 1, Title: "我的收藏夹", Count: 8}}
	sp.dynItems = []bili.Dynamic{{ID: "d1", Kind: bili.DynamicText, Text: "正文"}}

	if got := upRowLabel(sp, upTabCollection, 0); got != "我的收藏夹" {
		t.Errorf("收藏夹那一行的名字是 %q", got)
	}
	if got := upRowLabel(sp, upTabDynamic, 0); got != "正文" {
		t.Errorf("动态那一行的名字是 %q（正文优先）", got)
	}
	if got := upRowLabel(sp, upTabDynamic, 1); got == "" {
		t.Error("哨兵那一行也得有名字（读屏软件会念它）")
	}

	b := newUIExplore(t)
	uiSpaceData(b, "有收藏夹的 UP", 0, 1)
	b.on("/x/v3/fav/folder/created/list", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"list": []any{
			map[string]any{"id": 100, "title": "点我", "media_count": 8},
		}})
	})
	s, tt, _ := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp = s.upSpaces[42]
	sp.tab = upTabCollection
	tt.Frame()
	waitFrames(t, tt, "收藏夹加载完", func() bool { return sp.fav.started })
	tt.Frame()

	if err := tt.Click("点我"); err != nil {
		t.Fatal(err)
	}
	if got := s.router.Path(); got != "/collection/100" {
		t.Errorf("点了收藏夹跳到了 %q，want /collection/100", got)
	}
}

// rowKeys 是前 n 行的身份。
func rowKeys(sp *upSpace, tab, n int) []any {
	rows := min(n, upRowCount(sp, tab))
	out := make([]any, 0, rows)
	for i := range rows {
		out = append(out, upRowKey(sp, tab, i))
	}
	return out
}

// TestTheSpaceAvatarIs140AndTheHeaderPads32 钉住头图区那两个数：
// 头像 140（上游 `space-info.tsx:89` 的 `h-[140px] w-[140px]`）、左右留白 32
// （同一行的 `px-8`，`space-info.tsx:83`）。
func TestTheSpaceAvatarIs88AndTheHeaderPads32(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "量尺寸的 UP 主", 0, 123456)
	s, tt, _ := newSpaceShell(t, b, true, "77")
	openSpace(t, s, tt, 42)

	av, ok := tt.Find("https://f/face-42.jpg")
	if !ok {
		t.Fatalf("画面上找不到头像，文字有 %q", tt.Texts())
	}
	if av.W != 88 || av.H != 88 {
		t.Errorf("头像量出来是 %.1f×%.1f，want 88×88", av.W, av.H)
	}

	winW := float32(tt.Image().Bounds().Dx())
	pageLeft := float32(sideTargetWidth(s.app.SettingsSnapshot()))
	if got := av.X - pageLeft; got != 32+upAvatarRing {
		t.Errorf("头像离页面左缘 %.1f，want 35（左右留白 32 + 3 的底色环）", got)
	}
	name, ok := tt.Find("量尺寸的 UP 主")
	if !ok {
		t.Fatalf("画面上找不到昵称，文字有 %q", tt.Texts())
	}
	if got := name.X - (av.X + av.W); got != 16+upAvatarRing {
		t.Errorf("昵称离头像右边 %.1f，want 19（间距 16 + 3 的底色环）", got)
	}
	lv, ok := tt.Find("Lv4")
	if !ok {
		t.Fatalf("画面上找不到「Lv4」，文字有 %q", tt.Texts())
	}
	if got := winW - (lv.X + lv.W); got != 32 {
		t.Errorf("统计离页面右缘 %.1f，want 32（上游同一行 px-8）", got)
	}

	// 头像压在 132 高的横幅下沿：环的上沿在 88，头像再进去 3。
	if av.Y-64 != upAvatarTop+upAvatarRing {
		t.Errorf("头像上沿在 %.1f（页面顶 64 往下 %.1f），want %d", av.Y, av.Y-64, upAvatarTop+upAvatarRing)
	}

	// 头像那圈 `shadow-lg`：头像正下方那一带的底色，要比同一块头图的别处暗。
	// 没有影子的版本实测是 0 个暗像素——判据取 200，离「有」和「没有」都远。
	img := tt.Image()
	ref := img.RGBAAt(int(av.X+av.W+60), int(av.Y+av.H+3))
	shadowed := mygoui.Rect{X: av.X + 20, Y: av.Y + av.H + 1, W: av.W - 40, H: 8}
	plain := mygoui.Rect{X: av.X + 250, Y: av.Y + av.H + 1, W: 100, H: 8}
	if n := darkerIn(img, shadowed, ref, 2); n < 200 {
		t.Errorf("头像底下的影子只有 %d 个暗像素（没有影子的地方是 %d 个）——"+
			"`shadow-lg` 没画出来", n, darkerIn(img, plain, ref, 2))
	}
}

// darkerIn 数 box 里比 ref 暗（每个通道都低过 tol）的像素个数。
func darkerIn(img *image.RGBA, box mygoui.Rect, ref color.RGBA, tol int) int {
	n := 0
	for y := int(box.Y); y < int(box.Y+box.H); y++ {
		for x := int(box.X); x < int(box.X+box.W); x++ {
			c := img.RGBAAt(x, y)
			if int(c.R)+tol < int(ref.R) && int(c.G)+tol < int(ref.G) && int(c.B)+tol < int(ref.B) {
				n++
			}
		}
	}
	return n
}
