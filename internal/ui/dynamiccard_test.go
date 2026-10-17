// 动态卡片与收藏夹卡片的测试（UP 空间和动态流两页共用那一份画法）。
//
// 假 B 站、壳、等状态的几个工具在 upspace_test.go 里（同一个包，共用）。
package ui

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

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
// （`shell.go` 的 WindowOptions）时列表只有 660 点宽——一个不会换行的 Row
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

// TestClickingAnArchiveDynamicSaysWhyItCannotPlayInsteadOfDoingNothing
// 钉住「点投稿那一块不是没反应」。
//
// ⚠️ **这一版放不了动态里的投稿**，而且是**故意说清楚**而不是装死：动态接口
// 给的 `major.archive` 里**没有 cid**（`bili.DynamicArchive` 的注释里写了
// 为什么），而 B 站的取流接口不给 cid 会取到**第一集**——多 P 投稿的第三集
// 放出来是第一集，且不报错。宁可现在说清楚，也不要悄悄放错的那一集。
//
// 「投稿」Tab 那边的条目是有 cid 的（`bili.UpVideo.Cid`），点得开。
func TestClickingAnArchiveDynamicSaysWhyItCannotPlayInsteadOfDoingNothing(t *testing.T) {
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

	var said string
	for _, toast := range s.toasts {
		said = toast.text
	}
	if said == "" {
		t.Fatalf("点了那一块一句话都没有（队列 %+v）——按了没反应最难解释", a.Queue().Items())
	}
	if !strings.Contains(said, "放不了") {
		t.Errorf("说的是 %q，want 一句「放不了这一首：…」", said)
	}
	// 队列不该被换掉：`PlayFrom` 在播不了的时候会把队列原样换回去
	// （播不了的歌不该把用户原来的队列弄丢）。
	if got := len(a.Queue().Items()); got != 0 {
		t.Errorf("队列里多了 %d 项，want 0（播不了的歌不该把队列换掉）", got)
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
