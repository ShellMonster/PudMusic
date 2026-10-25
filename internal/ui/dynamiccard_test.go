// 动态卡片与收藏夹卡片的测试（UP 空间和动态流两页共用那一份画法）。
//
// 假 B 站、壳、等状态的几个工具在 upspace_test.go 里（同一个包，共用）。
package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// ─────────────────────────────────────────────────────────────────────────────
// 动态卡片
// ─────────────────────────────────────────────────────────────────────────────

// TestOnlyTheThreeTypesReachTheScreenAndTheRestNeverDo 是「动态卡片只渲染
// 三种类型，其余静默丢弃」那条验收在**画面上**的钉子。
//
// 一页里塞了八条：三种画得出来的，外加转发/剧集/直播/广告/课程五种
// 画不出来的。断言的是画面上**有**那三条、**没有**那五条的字，而且整页
// 没有变成错误页——最后一条是关键：抛错的话用户看到的是整页报错，
// 而 B 站每天都在发转发。
func TestOnlyTheThreeTypesReachTheScreenAndTheRestNeverDo(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "发动态的 UP", 0, 1)
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"items": []any{
				uiDynamic("t1", "纯文字的那一条"),
				uiDynamicArchive("v1", "BV1play"),
				uiDynamicOpus("o1", "图文的那一条", "https://f/1.jpg"),
				uiDynamicUnrenderable("f1", "forward"),
				uiDynamicUnrenderable("p1", "pgc"),
				uiDynamicUnrenderable("l1", "live"),
				uiDynamicUnrenderable("a1", "ad"),
				uiDynamicUnrenderable("c1", "courses"),
			},
			"offset": "", "has_more": false,
		})
	})
	s, tt, _ := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]
	waitFrames(t, tt, "动态加载完", func() bool { return sp.dyn.started })
	tt.Frame()

	if len(sp.dynItems) != 3 {
		t.Fatalf("画得出来的留下了 %d 条，want 3（另外五种该被丢掉而不是报错）", len(sp.dynItems))
	}
	for _, want := range []string{"纯文字的那一条", "投稿 BV1play", "图文的那一条"} {
		if !tt.HasText(want) {
			t.Errorf("画面上没有 %q，文字有 %q", want, tt.Texts())
		}
	}
	// 五种画不出来的那几条一个字都不该出现。
	for _, gone := range []string{"f1", "p1", "l1", "a1", "c1"} {
		if tt.HasText(gone) {
			t.Errorf("画不出来的那条 %q 出现在画面上了", gone)
		}
	}
	// 整页还是这一页，不是错误页。
	if tt.HasText("界面出错了") {
		t.Error("一条画不出来的动态把整页带成了错误页（那正是这一条要防的）")
	}
}

// TestTheTextOfAGraphicalPostIsOnScreenOnce 钉住「同一句话只画一遍」。
//
// ⚠️ 判据是**数出来只有一遍**，不是 `HasText` —— 那个是**子串**匹配，两遍
// 也照样为真（BMG-10 的收尾说明里就记着这个坑）。这一版早先的 fixture 又
// 恰好把 `opus.summary.text` 留空，于是「正文被画两遍」那个 bug 被遮住了。
func TestTheTextOfAGraphicalPostIsOnScreenOnce(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "发图文的 UP", 0, 1)
	const body = "今天更新了一期视频，记得来看。"
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		// desc 和 summary 是同一段——B 站正常返回就是这样（上游两处都
		// `desc?.text || opus?.summary?.text`）。
		uiExploreJSON(w, 0, map[string]any{"items": []any{
			uiDynamicOpus("o1", body, "https://f/1.jpg"),
		}, "offset": "", "has_more": false})
	})
	s, tt, _ := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]
	waitFrames(t, tt, "动态加载完", func() bool { return sp.dyn.started })
	tt.Frame()

	// ⚠️ **不能断言「这句话只出现一次」**：卡片正文画一遍，而 `ListState.Label`
	//（行的无障碍名字）也是同一句——那是同一句、两个用途，两处是对的。
	//
	// 真正要抓的是那个 bug 的特征：**同一句被拼进了一个标签**（`a\na`）。
	// `countExactText` 只数全等，天然看不见拼接，所以这里直接找带换行的那一个。
	if doubled := body + "\n" + body; countExactText(tt, doubled) != 0 {
		t.Errorf("画面上有一句是 %q —— desc 和 summary 是同一段话，拼起来显示了两遍",
			doubled)
	}
	if n := countExactText(tt, body); n == 0 {
		t.Errorf("图文的正文一个字都没画出来，文字有 %q", tt.Texts())
	}
	if strings.Contains(sp.dynItems[0].Text, "\n") {
		t.Errorf("模型里的正文里有换行：%q —— 两个字段是同一段话，只该取一次",
			sp.dynItems[0].Text)
	}
}

// countExactText 数画面上**一字不差**等于 s 的文字有几处。
//
// ⚠️ 不是 `HasText`：那是**子串**匹配（`mygo/ui/headless.go` 的 HasText 文档
// 原话就是 "containing s"），数不出来。
//
// ⚠️ 名字带 `Exact` 是因为包里已经有别人的 `countText`
// （`settings_tabs_test.go`，#73 加的），同名会让整个包编不过 ——
// 两边各加一个同名辅助函数，合并时 Git 看不见冲突，编译器才看得见。
// 本包的辅助一律带 `Exact` / `Space` 之类的限定词，免得再撞。
func countExactText(tt *mygoui.Tester, s string) int {
	n := 0
	for _, txt := range tt.Texts() {
		if txt == s {
			n++
		}
	}
	return n
}

// TestTheNinePicturesWrapInsteadOfRunningOffTheEdge 钉住「九张图换行」。
//
// ⚠️ 九张 × 92 点 + 八道 6 点的缝 = 876 点，而窗口缩到 `MinWidth` 900
// （`shell_window.go` 的 WindowOptions）时列表只有 660 点宽——一个不会换行的 Row
// 把最后几张切在窗口边界外，既看不见也点不着。
//
// 判据是量出来的 y：第 4 张必须落在第 1 张**下面**（一行三张）。
func TestTheNinePicturesWrapInsteadOfRunningOffTheEdge(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "发九张图的 UP", 0, 1)
	pics := make([]string, 0, maxDynamicPics)
	for i := range maxDynamicPics {
		pics = append(pics, fmt.Sprintf("https://f/p%d.jpg", i))
	}
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items": []any{
			uiDynamicOpus("o1", "九张图", pics...),
		}, "offset": "", "has_more": false})
	})
	s, tt, _ := newSpaceShell(t, b, false, "")

	// 缩到最小窗口宽度：那一档下列表装不下 876 点。
	tt.SetSize(900, 700)
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]
	waitFrames(t, tt, "动态加载完", func() bool { return sp.dyn.started })
	tt.Frame()

	first, ok1 := tt.Find("https://f/p0.jpg")
	fourth, ok4 := tt.Find("https://f/p3.jpg")
	if !ok1 || !ok4 {
		t.Fatalf("量不到那几张图（%v / %v）——它们有没有 Label？", ok1, ok4)
	}
	if fourth.Y <= first.Y {
		t.Errorf("第 4 张(y=%.1f) 没有落到第 1 张(y=%.1f)下面 —— 九张图仍排在一行里，"+
			"缩到最小窗口时最后几张会被切在边界外", fourth.Y, first.Y)
	}
	if got := first.W; got < dynamicPicSide-1 || got > dynamicPicSide+1 {
		t.Errorf("单张图的宽是 %.1f，want %d", got, dynamicPicSide)
	}
}

// TestAnAllUnrenderablePageSaysSoInsteadOfClaimingHePostedNothing 钉住
// 「有丢弃」和「真的没有」必须说两句话。
//
// 说成「暂无动态」的话，用户会以为他没发过——而真相是 B 站那二十多种
// 动态形态我们只画得出三种。
func TestAnAllUnrenderablePageSaysSoInsteadOfClaimingHePostedNothing(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "只发转发的 UP", 0, 1)
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items": []any{
			uiDynamicUnrenderable("f1", "forward"),
			uiDynamicUnrenderable("l1", "live"),
		}, "offset": "", "has_more": false})
	})
	s, tt, _ := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]
	waitFrames(t, tt, "动态加载完", func() bool { return sp.dyn.started })
	tt.Frame()

	if tt.HasText("暂无动态") {
		t.Error("有两条被丢弃了却说「暂无动态」——用户会以为他没发过")
	}
	if !tt.HasText("这里没有能显示的动态") {
		t.Errorf("没说清是「我们画不出来」，文字有 %q", tt.Texts())
	}
}

// TestClickingTheAuthorGoesIntoHisSpace 钉住「从动态卡片进 UP 空间」那条路。
func TestClickingTheAuthorGoesIntoHisSpace(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "被点进去的 UP", 0, 1)
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"items": []any{uiDynamic("d1", "正文")}, "offset": "", "has_more": false,
		})
	})
	s, tt, _ := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]
	waitFrames(t, tt, "动态加载完", func() bool { return sp.dyn.started })
	tt.Frame()

	if err := tt.Click("作者d1"); err != nil {
		t.Fatal(err)
	}
	if got := s.router.Path(); got != "/user/42" {
		t.Errorf("点了作者跳到了 %q，want /user/42", got)
	}
}

// TestClickingAnArchiveDynamicQueuesIt 钉住「点投稿那一块 = 加这一首并立即播」。
//
// ⚠️ 这条测试**改过一次断言**：早一版钉的是「点了会说放不了」——那是因为
// 动态接口给的 `major.archive` 里没有 cid，而 B 站的取流接口不给 cid 会取到
// **第一集**（多 P 投稿的第三集放出来是第一集，且不报错）。
//
// 现在 `app.playOnline` 前面有一道 `withCid`（`online.go`）：缺 cid 就拿 bvid
// 去问一次（`FirstVideoCid`），所以这一首**放得开了**。断言跟着改成「点了会进
// 队列」——那才是用户按下那一块想要的。
func TestClickingAnArchiveDynamicQueuesIt(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "发动态的 UP", 0, 1)
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items": []any{
			uiDynamicArchive("v1", "BV1play"),
		}, "offset": "", "has_more": false})
	})
	s, tt, a := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]
	waitFrames(t, tt, "动态加载完", func() bool { return sp.dyn.started })
	tt.Frame()

	if err := tt.Click("投稿 BV1play"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	items := a.Queue().Items()
	if len(items) != 1 {
		t.Fatalf("点了那一块队列里有 %d 项，want 1（队列 %+v）", len(items), items)
	}
	if items[0].Bvid != "BV1play" || items[0].Title != "投稿 BV1play" {
		t.Errorf("进队列的不是那一首：%+v", items[0])
	}
	if items[0].Source != "online" || items[0].Type != "mv" {
		t.Errorf("来源/类型是 %q/%q，want online/mv", items[0].Source, items[0].Type)
	}
	// cid 在这一步还是空的：`withCid` 是在 `playOnline` 里补的，补在**队列之后**
	// ——所以这里断的不是 cid，是「点了之后队列换了没有」。
	if a.Snapshot().HasTrack && a.Queue().Items()[0].Bvid != "BV1play" {
		t.Error("队列换成了别的歌")
	}
}

// TestAVideoFromThePostTabCanBeQueued 是上面那条的**另一半**：投稿 Tab 里
// 的条目是有 cid 的，点得开、进得了队列。
func TestAVideoFromThePostTabCanBeQueued(t *testing.T) {
	b := newUIExplore(t)
	uiSpaceData(b, "有投稿的 UP", 0, 1)
	b.on("/x/space/wbi/arc/search", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{
			"list": map[string]any{"vlist": []any{
				map[string]any{"bvid": "BV1ok", "title": "点我", "length": "3:20", "cid": "9"},
			}},
			"page": map[string]any{"count": 1},
		})
	})
	s, tt, a := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]
	sp.tab = upTabVideo
	tt.Frame()
	waitFrames(t, tt, "投稿加载完", func() bool { return sp.videos.Paged().Loaded > 0 })
	tt.Frame()

	// 双击才播放（单击只选中）——和列表那一行是同一个手势，见
	// list/menu_test.go 的 TestDoubleClickingARowPlaysIt。
	r, ok := tt.Find("点我")
	if !ok {
		t.Fatalf("画面上找不到那一行，文字有 %q", tt.Texts())
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()

	items := a.Queue().Items()
	if len(items) != 1 || items[0].Bvid != "BV1ok" || items[0].Cid != "9" {
		t.Fatalf("队列是 %+v，want 那一首（bvid=BV1ok cid=9）", items)
	}
	if items[0].Source != "online" || items[0].Type != "mv" {
		t.Errorf("队列那一项的来源/类型是 %q/%q，want online/mv", items[0].Source, items[0].Type)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 动态卡片上的动作
// ─────────────────────────────────────────────────────────────────────────────

// thumbCall 是收到的一次「给动态点赞」。
type thumbCall struct {
	id   string
	like bool
}

// recordThumbs 让那台假 B 站答得上来点赞，并记下每一次。fail 是让它一律报错。
//
// ⚠️ **请求体是 JSON 而不是表单**（见 `bili.LikeDynamic`）：这一段自己解，
// 解不出来就回「参数错误」。写成读表单的话 handler 照样答 0，而用例会看见
// 「发出去了、id 是空的」——钉不住任何东西。
func recordThumbs(t *testing.T, b *uiExplore, fail bool) func() []thumbCall {
	t.Helper()
	var (
		mu    sync.Mutex
		calls []thumbCall
	)
	b.on("/x/dynamic/feed/dyn/thumb", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID   string `json:"dyn_id_str"`
			Like int    `json:"up"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			uiExploreJSON(w, 4100001, nil)
			return
		}
		mu.Lock()
		calls = append(calls, thumbCall{id: body.ID, like: body.Like == 1})
		mu.Unlock()
		if fail {
			uiExploreJSON(w, -101, nil)
			return
		}
		uiExploreJSON(w, 0, map[string]any{})
	})
	return func() []thumbCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]thumbCall(nil), calls...)
	}
}

// setLike 给一条造出来的动态装上赞状态（`modules.module_stat.like`）。
func setLike(item map[string]any, count int, liked bool) {
	item["modules"].(map[string]any)["module_stat"] = map[string]any{
		"like": map[string]any{"count": count, "status": liked},
	}
}

// dynamicCardShell 造一个只有一条动态的 UP 空间。
func dynamicCardShell(t *testing.T, b *uiExplore, item map[string]any) (*Shell, *mygoui.Tester, *app.App) {
	// 这一组只需要 Tester 和 App（动作都是往 App 上发的），但外壳仍然要建在
	// 一个 `*Shell` 上——界面状态只在它身上。
	t.Helper()
	uiSpaceData(b, "发动态的 UP", 0, 1)
	b.on("/x/polymer/web-dynamic/v1/feed/space", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, map[string]any{"items": []any{item}, "offset": "", "has_more": false})
	})
	s, tt, a := newSpaceShell(t, b, false, "")
	openSpace(t, s, tt, 42)
	sp := s.upSpaces[42]
	waitFrames(t, tt, "动态加载完", func() bool { return sp.dyn.started })
	tt.Frame()
	return s, tt, a
}

// TestLikingADynamicReallyReachesBili 「赞」不是一句「还没接」。
//
// 钉三样：**按钮摆出来了**、**按下去发的是这一条的 id**、**界面上立刻变了**。
// 最后一条是乐观更新：用户按完到界面变化之间隔着一次网络往返，不立刻变的话他
// 会再按一下，于是他点了三次而他以为点了一次。
func TestLikingADynamicReallyReachesBili(t *testing.T) {
	b := newUIExplore(t)
	thumbs := recordThumbs(t, b, false)
	item := uiDynamicArchive("v1", "BV1play")
	setLike(item, 12, false)

	_, tt, _ := dynamicCardShell(t, b, item)

	if !tt.HasText("赞 12") {
		t.Fatalf("画面上没有「赞 12」，文字有 %q", tt.Texts())
	}
	if err := tt.Click("赞 12"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	// 还没等 B 站回话，界面已经变了。
	if !tt.HasText("赞 13") {
		t.Errorf("按了之后赞数没变，文字有 %q", tt.Texts())
	}
	waitFor(t, "点赞请求到了 B 站", func() bool { return len(thumbs()) == 1 })
	if got := thumbs()[0]; got.id != "v1" || !got.like {
		t.Errorf("发出去的是 %+v，want id=v1 且是点赞", got)
	}
}

// TestUnlikingTakesTheCountBackDown 取消赞也要真的发出去。
func TestUnlikingTakesTheCountBackDown(t *testing.T) {
	b := newUIExplore(t)
	thumbs := recordThumbs(t, b, false)
	item := uiDynamicArchive("v1", "BV1play")
	setLike(item, 12, true)

	_, tt, _ := dynamicCardShell(t, b, item)

	if err := tt.Click("赞 12"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("赞 11") {
		t.Errorf("取消赞之后数字没减，文字有 %q", tt.Texts())
	}
	waitFor(t, "取消赞的请求到了 B 站", func() bool { return len(thumbs()) == 1 })
	if got := thumbs()[0]; got.like {
		t.Errorf("发出去的是赞，不是取消赞：%+v", got)
	}
}

// TestAFailedLikeGoesBackToWhatItWas B 站说不行就**撤回去**。
//
// 不撤的话界面上会停在一个用户从来没设过的状态（他赞过、他没赞过都不是）。
func TestAFailedLikeGoesBackToWhatItWas(t *testing.T) {
	b := newUIExplore(t)
	recordThumbs(t, b, true)
	item := uiDynamicArchive("v1", "BV1play")
	setLike(item, 12, false)

	_, tt, _ := dynamicCardShell(t, b, item)

	if err := tt.Click("赞 12"); err != nil {
		t.Fatal(err)
	}
	waitFrames(t, tt, "赞失败之后撤回去", func() bool { return tt.HasText("赞 12") })
	if !tt.HasText("赞不了") {
		t.Errorf("失败了却一句话不说，文字有 %q", tt.Texts())
	}
}

// TestAVideoDynamicOffersBothDownloads 投稿型动态上才有「下载音频」（纯音频产品，没有「下载视频」）。
func TestAVideoDynamicOffersBothDownloads(t *testing.T) {
	_, tt, _ := dynamicCardShell(t, newUIExplore(t), uiDynamicArchive("v1", "BV1play"))

	for _, want := range []string{"下载音频"} {
		if !tt.HasText(want) {
			t.Errorf("投稿型动态上没有 %q，文字有 %q", want, tt.Texts())
		}
	}
}

// TestATextDynamicHasNoDownloadButtons 纯文字动态上**不摆**下载。
//
// 摆出来按不动（没有取流通道）——Biu 那边的下载按钮也只挂在视频上。
func TestATextDynamicHasNoDownloadButtons(t *testing.T) {
	_, tt, _ := dynamicCardShell(t, newUIExplore(t), uiDynamic("t1", "纯文字的那一条"))

	// 赞还在：文本动态也评赞（B 站给纯文字和图文都评）。
	if !tt.HasText("赞") {
		t.Errorf("纯文字动态上没有赞，文字有 %q", tt.Texts())
	}
	if tt.HasText("下载音频") || tt.HasText("下载视频") {
		t.Errorf("纯文字动态上摆着下载（按了也下不了），文字有 %q", tt.Texts())
	}
}

// TestDownloadingFromADynamicReallyReachesTheQueue 「下载音频」真的进队列。
//
// ⚠️ 动态接口给的稿件**不带 cid**（`major.archive` 里没有这个字段），而下载
// 队列的判重（`<bvid>|<cid>`）要它——所以这一条顺带钉了「它自己去问了一次分 P」
// （`App.DownloadItem` 那一层），而界面线程上一次网络往返都没有。
func TestDownloadingFromADynamicReallyReachesTheQueue(t *testing.T) {
	b := newUIExplore(t)
	b.on("/x/player/pagelist", func(w http.ResponseWriter, r *http.Request) {
		uiExploreJSON(w, 0, []any{map[string]any{
			"cid": 555, "page": 1, "part": "P1", "duration": 200,
		}})
	})
	_, tt, a := dynamicCardShell(t, b, uiDynamicArchive("v1", "BV1play"))

	// 下载这一摊得挂着，否则按下去是一句「下载还没挂上」（那是对的，但验不到接线）。
	if err := a.MountDownloads(app.Downloads{
		Media: b.Server.Client(), FallbackDir: t.TempDir(),
	}); err != nil {
		t.Fatalf("挂下载：%v", err)
	}
	if err := tt.Click("下载音频"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "下载进了队列", func() bool { return len(a.DownloadSnapshots()) == 1 })
	if got := a.DownloadSnapshots()[0].ID; got != "BV1play|555" {
		t.Errorf("队列里那一项是 %q，want BV1play|555（cid 是它自己问来的）", got)
	}
}

// TestPlayNextFromADynamicLandsOnTheNextSlot 「下一首播放」不是「添加到队尾」。
//
// 这一条盯的就是那个分界：按钮上写的是「下一首」，而用户按它是因为想在这支
// 片子之后立刻听它。接成 `Enqueue`（接队尾）的症状是「按了，什么也没发生，
// 直到整张动态队列放完」。
func TestPlayNextFromADynamicLandsOnTheNextSlot(t *testing.T) {
	_, tt, a := dynamicCardShell(t, newUIExplore(t), uiDynamicArchive("v1", "BV1next"))

	// 队列里先有一首在放。
	a.Queue().Reset(app.Item{Source: "online", Type: "mv", Bvid: "BVplaying", Title: "正在放的那首"})
	before := a.Queue().Items()

	if err := tt.Click("下一首播放"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()

	after := a.Queue().Items()
	if len(after) != len(before)+1 {
		t.Fatalf("按了之后队列里有 %d 首，want %d（多了一首）", len(after), len(before)+1)
	}
	// 位置：正在放的那首后面就是它，**不是队尾**。
	if after[0].Bvid != "BVplaying" {
		t.Fatalf("正在放的那首被顶走了，队头是 %q", after[0].Bvid)
	}
	if after[1].Bvid != "BV1next" {
		t.Errorf("第 2 首是 %q，want BV1next（下一首就该是它）", after[1].Bvid)
	}
}

// TestATextDynamicHasNoPlayNextButton 纯文字 / 图文里没有可放的东西。
func TestATextDynamicHasNoPlayNextButton(t *testing.T) {
	_, tt, _ := dynamicCardShell(t, newUIExplore(t), uiDynamic("t1", "纯文字的那一条"))
	if tt.HasText("下一首播放") {
		t.Errorf("纯文字动态上摆着「下一首播放」（那一栏里根本没有东西可放），文字有 %q", tt.Texts())
	}
}
