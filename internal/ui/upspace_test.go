// UP 空间与动态流两页的测试。
//
// 断言面是**渲染结果**（画面上有什么字、行身份是什么），不是「调了哪个
// 函数」——`SPEC.md` 的 Testing Decisions 就是这么定的。
package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

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
	mu       sync.Mutex
	paths    []string
	handlers map[string]http.HandlerFunc
}

func newUIExplore(t *testing.T) *uiExplore {
	t.Helper()
	b := &uiExplore{handlers: map[string]http.HandlerFunc{}}
	b.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		b.paths = append(b.paths, r.URL.Path)
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
func newSpaceShell(t *testing.T, b *uiExplore, loggedIn bool, mid string) (*Shell, *mygoui.Tester, *app.App) {
	t.Helper()
	a := app.New()
	t.Cleanup(a.Close)
	s := New(a)
	t.Cleanup(s.images.Close)
	t.Cleanup(func() { _ = s.hist.Close() })
	s.router.Transition = mygoui.TransitionNone

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
	return s, mygoui.NewTester(s.View, 1200, 800), a
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

// waitSpace 等一个条件成立。
//
// ⚠️ **不是 `Advance` 出动画**：这几页没有动画，要等的是**后台 goroutine**
// 那个网络往返回来。真等（等状态真的变了）之后那一帧画出来的就是换完的样子
// ——即时模式里「异步结果必须唤醒」的那个坑正是在这条路上（不叫醒就停在旧帧）。
func waitSpace(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for i := 0; i < 3000; i++ {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("等了 3 秒还没：%s", what)
}

// waitFrames 等一个条件成立，**每转一圈先画一帧**。
//
// ⚠️ 什么时候必须用这一条而不是 `waitSpace`：凡是「只在 view 里被收进
// 界面的状态」（`PagedList` 的 `Loaded`、`upPager.started`、收进 `dynItems`
// 的那几条）都是这样——后台把结果投进信箱之后，要等**下一帧**的 `adopt`
// 才会落到状态上。不画那一帧的话条件永远不成立，而症状是「等了 3 秒还没」。
//
// 反过来，`app.UpSpace` 的头部状态是后台 goroutine 自己写的
// （见 explore.go 的 Load），那一支不画帧也等得到——两种都要有。
func waitFrames(t *testing.T, tt *mygoui.Tester, what string, cond func() bool) {
	t.Helper()
	for i := 0; i < 3000; i++ {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
		tt.Frame()
	}
	t.Fatalf("画了 3000 帧还没：%s", what)
}

// openSpace 切到某个 UP 的空间并等头加载完。
func openSpace(t *testing.T, s *Shell, tt *mygoui.Tester, mid int64) *upSpace {
	t.Helper()
	s.navigate(upPathOf(mid))
	tt.Frame()
	sp := s.upSpaces[mid]
	waitSpace(t, "头加载完", func() bool {
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
func uiDynamicOpus(id, text string, pics ...string) map[string]any {
	m := uiDynamic(id, text)
	items := make([]any, 0, len(pics))
	for _, p := range pics {
		items = append(items, map[string]any{"url_src": p})
	}
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

// uiDynamicUnrenderable 是造一条这一版画不出来的动态（转发/直播/剧集…）。
func uiDynamicUnrenderable(id, kind string) map[string]any {
	m := uiDynamic(id, "")
	mods := m["modules"].(map[string]any)
	dyn := mods["module_dynamic"].(map[string]any)
	dyn["desc"] = map[string]any{"text": ""}
	dyn["major"] = map[string]any{"type": kind, kind: map[string]any{"title": id}}
	m["type"] = "DYNAMIC_TYPE_" + kind
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
	waitSpace(t, "关系变成已关注", func() bool {
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
