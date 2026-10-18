package ui

// 联想词那一档的界面测试（#97）。走的是这一包定好的那条缝：一个外壳 + 假时钟 +
// 一台假 B 站，断言**画面上有没有**和**什么时候问出去的**。
//
// ⚠️ 这一组跑的是**真的那条路**：`app.SearchSuggest` → `bili.SearchSuggest` →
// HTTP fixture。不用假拉页器是因为「什么时候问出去」「失败了怎么办」「迟到的那一趟
// 会不会盖掉新的」全长在这条路上，替掉网络那一段的话，三样都验不到。

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// newSuggestShell 造一个「顶栏搜索框 + 那台会答联想词的假 B 站」的外壳。
//
// 时钟换成假的（clock.go）：防抖那 500ms 不能真等，而这一组量的就是那 500ms。
// `fakeClockOn` 会把红点那个节奏拨回零值（造外壳的第一帧已经按真时钟走过一趟）。
func newSuggestShell(t *testing.T, f *biliFixture) (*Shell, *mygoui.Tester) {
	t.Helper()
	b := newTestShell(t, mounted(func(t *testing.T, a *app.App) {
		// 走真的登录那条路（挂一个空登录态 + 那个客户端）：`App.BiliClient` 只有
		// 走过 MountLogin 之后才有值，而那一层问的就是它。
		a.MountLogin(app.NewMemoryStore("suggest-test"), f.client())
	}))
	// ⚠️ **把应用的唤醒句柄接上**：生产上那是 `Attach` 干的（`a.SetWake`），而无头
	// Tester 没有窗口、`Attach` 永远不跑。少了它，「谁改了状态谁要一帧」这件事在
	// 这里压根不发生——于是「界面自己养自己」（每一帧都请求下一帧）那种坏法在这
	// 一包里是验不到的。接上之后 `Tester.FramePending` 才说得清有没有人在要帧。
	b.s.app.SetWake(b.s.wake.invalidate)

	fakeClockOn(b.s, fakeClockStart)
	b.tt.SetFocused(true)
	return b.s, b.tt
}

// typeInSearch 点一下搜索框、往里敲几个字。
//
// ⚠️ **调两次就是往里接**（不是替换）：无头 Tester 的 `Type` 发的是一个 TextInput
// 事件，落在光标处，而光标在框里那些字的末尾。所以「用户又敲了一个字」在这里是
// `typeInSearch(t, tt, "伦")` 这样一个字一个字地接——这正是用户真的在做的事。
func typeInSearch(t *testing.T, tt *mygoui.Tester, text string) {
	t.Helper()
	if err := tt.Click(searchLabel); err != nil {
		t.Fatalf("点不到搜索框：%v，文字有 %q", err, tt.Texts())
	}
	tt.Type(text)
	tt.Frame()
}

// settleSuggestDebounce 推进假时钟，让那 500ms 走完，再画一帧。
func settleSuggestDebounce(s *Shell, tt *mygoui.Tester) {
	advance(s, searchDebounce)
	tt.Frame()
}

// waitAsked 等那台假 B 站被问过**至少** n 次。
//
// ⚠️ **判据是「至少」而不是「正好」**：请求数是一个只增不减的计数器，而
// `waitFrames` 是画一帧问一次——「正好 1 次」只在它恰好停在那儿的那一瞬间成立。
// 用 `== 1` 等的话，「一帧一发」的那种坏实现会等成一句莫名其妙的超时（而它确实
// 是坏的，只是失败的理由会指错地方）。次数由用例自己断言，那是给用户看的那一句。
func waitAsked(t *testing.T, f *biliFixture, n int) {
	t.Helper()
	waitFor(t, fmt.Sprintf("问出去至少 %d 次", n), func() bool { return len(f.suggestCalls()) >= n })
}

// TestTypingBringsUpTheSuggestTerms 钉住规格第一条：输入时浮层里多一档「联想词」。
func TestTypingBringsUpTheSuggestTerms(t *testing.T) {
	f := newBiliFixture(t, 1)
	f.setSuggestTerms("周杰", "周杰伦", "周杰伦 晴天")
	s, tt := newSuggestShell(t, f)
	s.hist.add("林俊杰")
	tt.Frame()

	typeInSearch(t, tt, "周杰")
	settleSuggestDebounce(s, tt)

	waitFrames(t, tt, "联想词那一档出来", func() bool { return tt.HasText(suggestTermsHeading) })
	for _, want := range []string{"周杰伦", "周杰伦 晴天"} {
		if !tt.HasText(want) {
			t.Errorf("浮层上没有 %q，文字有 %q", want, tt.Texts())
		}
	}
	// 历史那一档**同时还在**：它是同一个浮层里的另一档，不该被联想顶掉。
	if !tt.HasText("搜索历史") || !tt.HasText("林俊杰") {
		t.Errorf("联想词把历史那一档顶掉了，文字有 %q", tt.Texts())
	}
	if got := f.suggestCalls(); len(got) != 1 || got[0] != "周杰" {
		t.Errorf("问出去的是 %v，want [周杰]", got)
	}
}

// TestTheKeywordIsDebouncedBy500ms 钉住规格第三条：500 毫秒防抖，假时钟推。
//
// 判据是「距离最后一次输入过了多久」，不是「有没有问过」——而中间那一段必须真的
// 等一下：请求是异步的，不等就是拿一个还没落地的请求去断言「没问」，那条断言
// 永远不会红，而它正是这一条的全部意义。
func TestTheSuggestKeywordIsDebouncedBy500ms(t *testing.T) {
	f := newBiliFixture(t, 1)
	f.setSuggestTerms("周杰", "周杰伦")
	s, tt := newSuggestShell(t, f)

	typeInSearch(t, tt, "周杰")
	// 离最后一次输入才 400ms：不许问。
	advance(s, 400*time.Millisecond)
	tt.Frame()
	time.Sleep(150 * time.Millisecond)
	if got := f.suggestCalls(); len(got) != 0 {
		t.Fatalf("离最后一次输入才 400ms 就问了（%v）——防抖的钟没有生效", got)
	}

	// 到点了才问。
	advance(s, 125*time.Millisecond)
	waitFrames(t, tt, "满 500ms 之后问出去并画出来", func() bool { return tt.HasText("周杰伦") })
	waitAsked(t, f, 1)
	if got := f.suggestCalls(); len(got) != 1 || got[0] != "周杰" {
		t.Errorf("问出去的是 %v，want 正好一次、词是周杰", got)
	}
}

// TestTheSuggestDebounceRestartsOnEveryKeystroke 盯防抖是从**最后一个字**算起的。
//
// 用户慢慢打的时候（「周杰」想了两秒才接着敲「伦」），从第一个字算起会在中途问一个
// 还没输完的词——而 B 站给回来的联想是按**当时那个词**给的，于是浮层上摆着一批用户
// 早就改掉的答案，而点其中一条搜的是那一批里的词。
func TestTheSuggestDebounceRestartsOnEveryKeystroke(t *testing.T) {
	f := newBiliFixture(t, 1)
	f.setSuggestTerms("周杰", "周杰伦")
	f.setSuggestTerms("周杰伦", "周杰伦 晴天")
	s, tt := newSuggestShell(t, f)

	typeInSearch(t, tt, "周杰")
	advance(s, 400*time.Millisecond)
	tt.Frame()

	// 又敲了一个字：上一个字的 400ms 已经走掉了，这一下从头再等。
	typeInSearch(t, tt, "伦")
	if got := s.searchKeyword; got != "周杰伦" {
		t.Fatalf("框里是 %q，want 周杰伦", got)
	}
	advance(s, 125*time.Millisecond)
	tt.Frame()
	time.Sleep(150 * time.Millisecond)
	if got := f.suggestCalls(); len(got) != 0 {
		t.Fatalf("离最后一次输入才 125ms 就问了（%v）——防抖的钟没有重打", got)
	}

	advance(s, 400*time.Millisecond)
	waitFrames(t, tt, "满 500ms 之后按最后那个词问出去", func() bool {
		return tt.HasText("周杰伦 晴天")
	})
	waitAsked(t, f, 1)
	if got := f.suggestCalls(); len(got) != 1 || got[0] != "周杰伦" {
		t.Errorf("问出去的是 %v，want 正好一次、词是周杰伦（三个字的那个）", got)
	}
}

// TestNoWordMeansNoRequest 钉住「浮层开着、框里空着，一个请求都不发」。
//
// 用户点一下搜索框（浮层就开了）而还没敲字——那是翻历史，不是联想。而 B 站那边
// 那个请求一个空白词回来的是一堆热门推荐，白花一次往返、还把界面上那一档塞满。
func TestNoWordMeansNoRequest(t *testing.T) {
	f := newBiliFixture(t, 1)
	s, tt := newSuggestShell(t, f)
	s.hist.add("林俊杰")
	tt.Frame()

	if err := tt.Click(searchLabel); err != nil {
		t.Fatalf("点不到搜索框：%v，文字有 %q", err, tt.Texts())
	}
	tt.Frame()
	if !s.searchSuggest {
		t.Fatal("点了一下浮层没开")
	}

	advance(s, 5*time.Second)
	tt.Frame()
	time.Sleep(150 * time.Millisecond)
	if got := f.suggestCalls(); len(got) != 0 {
		t.Errorf("框里一个字都没有就问了 %v", got)
	}
	// 而历史那一档照常摆着（浮层本来就是为它开的）。
	if !tt.HasText("林俊杰") {
		t.Errorf("历史那一档没了，文字有 %q", tt.Texts())
	}
}

// TestAnEmptySearchBoxDoesNotKeepTheWindowRedrawing 钉住「空框不许自己养自己」。
//
// 联想词那一档每秒最多问一次，而「把上一批收掉」那一支是**每帧**都会到的（框空着
// 的那几百帧全在走它）。少了「已经什么都没有了就别动它」那道闸门，它每一帧都
// `gen++` 加叫醒，而生产里每一次唤醒都画一帧——于是窗口在搜索框空着的时候**永远
// 停不下来**，风扇转着、电费交着，界面上还看不出任何异样。
//
// 判据用 Tester 自己的 `FramePending`（这一帧之后有没有人还要下一帧），不用数秒。
func TestAnEmptySearchBoxDoesNotKeepTheWindowRedrawing(t *testing.T) {
	f := newBiliFixture(t, 1)
	s, tt := newSuggestShell(t, f)
	s.hist.add("林俊杰")
	tt.Frame()

	for i := range 5 {
		tt.Frame()
		if tt.FramePending() {
			t.Fatalf("第 %d 帧之后还有人在要下一帧：框是空的、浮层也是关的，"+
				"没有理由重画（自己养自己的唤醒循环）", i)
		}
	}
}

// overflowLongHistory 是十条**长**的历史词条。
//
// 长度是照评审复现的那一批量的：一条就占掉小半行，十条折成四行——而联想那一档
// 十行，全摆出来就是这个尺寸下撑爆屏幕的那一档场景。
var overflowLongHistory = []string{
	"林俊杰 江南 林俊杰 超大型精选集 全集",
	"周杰伦 跨时代演唱会精选集 全场慢速",
	"薛之谦 天外来物 现场版 高清完整版",
	"邓丽君 绝代佳人  remastered 合集",
	"陈奕迅 演唱会精选 十年精选 全纪录",
	"张学友 醒着做梦 演唱会 高清修复",
	"王菲 红豆 专辑 精选 全碟版",
	"李宗盛 山丘 live 演唱会实录",
	"赵雷 成都 民谣现场 完整版 合集",
	"朴树 那些花儿 现场版 修复 高清",
}

// overflowTerms 是联想那一档摆满的十条。
var overflowTerms = []string{
	"周杰伦 晴天", "周杰伦 以父之名", "周杰伦 彩虹", "周杰伦 稻香",
	"周杰伦 夜曲", "周杰伦 简单爱", "周杰伦 青花瓷", "周杰伦 发如雪",
	"周杰伦 双截棍", "周杰伦 东风破",
}

// overflowPanel 建一个「最小窗口 + 满两档」的外壳，回到刚画完一帧的样子。
//
// ⚠️ **最小窗口**：900×560 是 `WindowOptions` 的 MinWidth/MinHeight（shell.go），
// 也是用户把窗口缩到最小的样子。这一组量的就是**这一个尺寸**下会发生什么——
// 默认的 1200×800 什么都装得下。
func overflowPanel(t *testing.T, history []string, terms []string) (*Shell, *mygoui.Tester) {
	t.Helper()
	f := newBiliFixture(t, 1)
	f.setSuggestTerms("周", terms...)
	b := newTestShell(t,
		at(900, 560),
		withClock(newFakeClock(fakeClockStart)),
		mounted(func(t *testing.T, a *app.App) {
			a.MountLogin(app.NewMemoryStore("suggest-test"), f.client())
		}),
	)
	s, tt := b.s, b.tt
	s.app.SetWake(s.wake.invalidate)
	s.feed.next = time.Time{}
	tt.SetFocused(true)
	for _, w := range history {
		s.hist.add(w)
	}
	tt.Frame()
	return s, tt
}

// TestThePanelFitsInASmallWindowAndScrolls 钉住「最下面那几条点得着」。
//
// 🔴 评审在这一点过本票：在应用允许的**最小**窗口（900×560，`WindowOptions` 的
// MinWidth/MinHeight）上灌满两档——历史 10 条**长**词条（折行，一行放不下两条）
// 加联想 10 条——最后一条联想被排到 y=735，而屏幕只有 560 高：它画在屏幕底下，
// **看不见、点不着，也滚不到**（面板原本不是滚动容器）。默认的 1200×800 不溢出，
// 所以只有小窗口会踩到——而那正是用户把窗口缩到最小的时候。
//
// 判据是**滚到底之后它整个在屏幕里**，不是一个写死的 y 值：面板挂在搜索框下面，
// 顶边的确切位置随框架的浮层排布走，写死数值的话改一次布局就红，而红的原因和
// 「点得着」无关。
func TestThePanelFitsInASmallWindowAndScrolls(t *testing.T) {
	// 历史那几条照评审用的长度来：够长到一条就占掉半行，十条折成四行。
	long := []string{
		"林俊杰 江南 林俊杰 超大型精选集 全集",
		"周杰伦 跨时代演唱会精选集 全场慢速",
		"薛之谦 天外来物 现场版 高清完整版",
		"邓丽君 绝代佳人  remastered 合集",
		"陈奕迅 演唱会精选 十年精选 全纪录",
		"张学友 醒着做梦 演唱会 高清修复",
		"王菲 红豆 专辑 精选 全碟版",
		"李宗盛 山丘 live 演唱会实录",
		"赵雷 成都 民谣现场 完整版 合集",
		"朴树 那些花儿 现场版 修复 高清",
	}
	terms := []string{
		"周杰伦 晴天", "周杰伦 以父之名", "周杰伦 彩虹", "周杰伦 稻香",
		"周杰伦 夜曲", "周杰伦 简单爱", "周杰伦 青花瓷", "周杰伦 发如雪",
		"周杰伦 双截棍", "周杰伦 东风破",
	}
	s, tt := overflowPanel(t, long, terms)

	typeInSearch(t, tt, "周")
	settleSuggestDebounce(s, tt)
	waitFrames(t, tt, "两档都画出来", func() bool { return tt.HasText(terms[9]) })

	// 前提本身要钉住：这一档在这个尺寸下**真的是撑爆了的**。不钉的话，哪天历史
	// 那几条变短了，那条断言就变成在验「本来就装得下」——而它本来该防的是溢出。
	if r, ok := tt.Find(terms[9]); ok && r.Y+r.H <= 560 {
		t.Fatalf("最后一条联想 %q 在 %v，屏幕 560 高装得下——"+
			"这一条量不到溢出了（历史那几条变短了？），换一个更长的", terms[9], r)
	}

	// 滚到底之后它整个在屏幕里。
	//
	// ⚠️ **滚到尽头只能等条件**（见 `scrollToEnd`）：判据就是下面那两条断言本身
	// ——最后一条联想整个落在屏幕里。早一版是「滚 6 下 × 每下 600」，而面板能滚
	// 多远取决于它有多高（历史那几条折成几行、联想几条），换个 fixture 那个数就
	// 不够了，于是这一条变成在验「碰巧够」。
	scrollToEnd(t, tt, 450, 300, "联想面板的末尾", func() bool {
		r, ok := tt.Find(terms[9])
		return ok && r.Y >= 0 && r.Y+r.H <= 560
	})
	r, ok := tt.Find(terms[9])
	if !ok {
		t.Fatalf("滚到底之后画面上没有 %q，文字有 %q", terms[9], tt.Texts())
	}
	if r.Y+r.H > 560 || r.Y < 0 {
		t.Errorf("滚到底之后 %q 在 %v，屏幕高 560——它还是有一部分在屏幕外", terms[9], r)
	}
	// 而且点得中：滚到底之后按它，走的是搜索那条路。
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	if got := s.router.Path(); got != searchPath {
		t.Errorf("点滚到底之后的那一条没有选中（在 %q，文字有 %q）", got, tt.Texts())
	}
}

// TestReopeningTheOverlayStartsAtTheTop 钉住「浮层收掉时滚动位置归零」。
//
// 不归零的话：用户翻到第十条、按 Esc、再点开同一个框，看到的还是第十条起——而
// 那一框里的词多半已经换了一个问题。
func TestReopeningTheOverlayStartsAtTheTop(t *testing.T) {
	s, tt := overflowPanel(t, overflowLongHistory, overflowTerms)

	typeInSearch(t, tt, "周")
	settleSuggestDebounce(s, tt)
	waitFrames(t, tt, "两档都画出来", func() bool { return tt.HasText(overflowTerms[9]) })

	// ⚠️ **一下就够**：这一条要证明的是「浮层收掉之后位置归零」，滚多重不重要，
	// 而「位置确实动过」才是它的前提。早一版滚 4 下 × 每下 600，换一个矮一点的
	// 面板就可能一步滚到底、把「归零」这件事变得量不出来。
	tt.Scroll(450, 300, 0, 600)
	tt.Frame()
	if s.terms.scroll.Y == 0 {
		t.Fatal("滚了一下位置还是 0——这一条没量到东西（或者面板根本不滚）")
	}

	tt.Key(0, mygoui.KeyEscape)
	tt.Frame()
	if got := s.terms.scroll.Y; got != 0 {
		t.Errorf("浮层收掉之后滚动位置还停在 %v", got)
	}
}

// TestAFailedSuggestLeavesOnlyTheHistory 钉住规格第四条：请求失败时静默。
//
// 判据是三样：浮层上**还有**历史那一档、**没有**联想词那一档、**没有**任何一句
// 错误（提示条也不弹）。少一样用户就会以为搜索框坏了——而手动打字搜索照旧能用。
func TestAFailedSuggestLeavesOnlyTheHistory(t *testing.T) {
	f := newBiliFixture(t, 1)
	f.setSuggestFail(-412)
	s, tt := newSuggestShell(t, f)
	s.hist.add("林俊杰")
	tt.Frame()

	typeInSearch(t, tt, "林俊杰")
	settleSuggestDebounce(s, tt)

	// 等到那一次**有问有答**。⚠️ 等的是「回了几次」而不是「问了几次」：只等「问了」
	// 的话下面验的还是请求还在路上那一刻的画面，而那一帧和失败之后的画面长得一样，
	// 于是这一条会「验了但什么都没验到」。
	waitFor(t, "那一次有问有答", func() bool { return f.suggestServed() >= 1 })
	waitFrames(t, tt, "答回来之后那一帧", func() bool { return !s.app.SuggestState().Loading })

	if !tt.HasText("搜索历史") || !tt.HasText("林俊杰") {
		t.Errorf("拉不到之后连历史那一档也没了，文字有 %q", tt.Texts())
	}
	if tt.HasText(suggestTermsHeading) {
		t.Errorf("拉不到却摆了一档联想词出来，文字有 %q", tt.Texts())
	}
	// 一个字都不能有：静默是这一条的全部。
	for _, bad := range []string{"失败", "错误", "拉不到", "风控", "重试"} {
		if tt.HasText(bad) {
			t.Errorf("拉不到却在画面上说了 %q（要静默），文字有 %q", bad, tt.Texts())
		}
	}
	if toasts := s.visibleToasts(); len(toasts) != 0 {
		t.Errorf("拉不到却弹了 %d 条提示", len(toasts))
	}
	// 而失败之后**不许每帧重发**：那是自己养自己的循环，界面上是搜索框一直卡着。
	for range 5 {
		tt.Frame()
	}
	if got := len(f.suggestCalls()); got != 1 {
		t.Errorf("失败之后又问了 %d 次（合计），want 1——那是每帧一发", got)
	}
}

// TestTypingTheSameWordAgainAsksAgain 钉住「失败之后还能再试一次」。
//
// 静默是规格要的，于是**用户看不到任何提示**——他唯一能做的事就是把那几个字删掉
// 重敲一遍。而那一遍必须真的会再问一次：业务层为了挡住「每帧重发」记着「这个词已经
// 问过了」，连失败的那一次也记着，于是不清掉的话他重敲多少遍都是空的，而界面上
// 一个字都没有告诉他为什么。
func TestTypingTheSameWordAgainAsksAgain(t *testing.T) {
	f := newBiliFixture(t, 1)
	f.setSuggestFail(-412) // 先拉不到
	s, tt := newSuggestShell(t, f)
	s.hist.add("林俊杰")
	tt.Frame()

	typeInSearch(t, tt, "林俊杰")
	settleSuggestDebounce(s, tt)
	waitFor(t, "那一次有问有答", func() bool { return f.suggestServed() >= 1 })
	if tt.HasText(suggestTermsHeading) {
		t.Fatalf("拉不到却摆了一档联想词出来，文字有 %q", tt.Texts())
	}

	// 用户把那三个字删干净，重敲一遍：B 站这一次放行。
	//
	// ⚠️ 清空走的是**框上那个「清空」键**（`viewSearchClear`）而不是往框里打一个空串
	// ——`Type` 发的是一个落在光标处的输入事件，空串什么也不做，框里的字还在。
	f.setSuggestFail(0)
	f.setSuggestTerms("林俊杰", "林俊杰 新歌")
	if err := tt.Click("清空"); err != nil {
		t.Fatalf("点不到「清空」：%v，文字有 %q", err, tt.Texts())
	}
	if got := s.searchKeyword; got != "" {
		t.Fatalf("清空之后框里是 %q，want 空", got)
	}
	typeInSearch(t, tt, "林俊杰")
	settleSuggestDebounce(s, tt)

	waitFrames(t, tt, "重敲之后又问了一次", func() bool { return len(f.suggestCalls()) >= 2 })
	waitFrames(t, tt, "重敲之后问出来了", func() bool { return tt.HasText("林俊杰 新歌") })
	if got := f.suggestCalls(); len(got) != 2 {
		t.Errorf("一共问了 %v，want 两次（第二次是重敲之后的那一次）", got)
	}
}

// TestTheTwoSectionsAreToldApart 钉住规格第二条：两档在界面上区分得开。
//
// 判据是**能分开的三件事**，不是「两个标题都在」：
//
//  1. 位置：历史在上、联想在下（Biu 的 topContent 在结果列表上面）；
//  2. 形状：联想是**一整行**（点哪儿都行），历史是一个小 chip（只有那一小块点得到）；
//  3. 后果：「清除全部」只清历史那一档，联想词不受影响——所以它一定不是同一种东西。
func TestTheTwoSectionsAreToldApart(t *testing.T) {
	f := newBiliFixture(t, 1)
	f.setSuggestTerms("周杰", "周杰伦 晴天")
	s, tt := newSuggestShell(t, f)
	s.hist.add("林俊杰")
	tt.Frame()

	typeInSearch(t, tt, "周杰")
	settleSuggestDebounce(s, tt)
	waitFrames(t, tt, "两档都在", func() bool { return tt.HasText(suggestTermsHeading) && tt.HasText("搜索历史") })

	histHead, ok := tt.Find("搜索历史")
	if !ok {
		t.Fatalf("没有「搜索历史」那一栏，文字有 %q", tt.Texts())
	}
	termHead, ok := tt.Find(suggestTermsHeading)
	if !ok {
		t.Fatalf("没有「联想词」那一栏，文字有 %q", tt.Texts())
	}
	chip, ok := tt.Find("林俊杰")
	if !ok {
		t.Fatalf("浮层上没有那条历史，文字有 %q", tt.Texts())
	}
	term, ok := tt.Find("周杰伦 晴天")
	if !ok {
		t.Fatalf("浮层上没有那条联想词，文字有 %q", tt.Texts())
	}

	// ① 历史在上、联想在下。
	if chip.Y+chip.H > termHead.Y {
		t.Errorf("历史那条（Y=%v）在联想词那一栏（Y=%v）下面，两栏的位置和 Biu 反了",
			chip.Y, termHead.Y)
	}
	if term.Y+term.H <= histHead.Y {
		t.Errorf("联想词那条（Y=%v）压在历史标题（Y=%v）上面", term.Y, histHead.Y)
	}

	// ② 形状：联想是整行，历史是一个小 chip。
	//
	// ⚠️ **宽度量不出来**：`ui.Column` 默认让子控件 `Stretch`，所以「整行」和「小块」
	// 量出来的框一样宽（实测把联想词改成 chip 之后仍然是 342）。真正能分开两者的
	// 是**那一行有没有自己的底色**：chip 是 `t.Surface` + 边框的小方块，联想词那一行
	// 除了悬停什么都不画，字左边那片留白透出的是浮层自己的底色。
	//
	// 所以这里取两个「字左边」的像素比：chip 里侧那块是 chip 的底色，联想词那一行
	// 同一处是浮层的底色。判据是「这两个点画出来不一样」——没有猜任何颜色值，
	// 深浅两套主题下都成立。
	chipIn := tt.Image().RGBAAt(int(chip.X)-3, int(chip.Y+chip.H/2))
	termIn := tt.Image().RGBAAt(int(term.X)+3, int(term.Y+term.H/2))
	if chipIn == termIn {
		t.Errorf("历史那条 chip 和联想词那一行在字左边画出来一模一样（%v）——"+
			"两档在界面上分不开（chip 是有底色的小方块，联想词是整行）", chipIn)
	}
	// 整行的另一个含义：**点哪儿都行**——点在离字很远的那一头也一样选得中。
	tt.ClickAt(term.X+term.W-6, term.Y+term.H/2)
	if s.router.Path() != searchPath {
		t.Errorf("点联想词那一行的右端没选中（文字有 %q）", tt.Texts())
	}

	// ③ 后果：「清除全部」只动历史那一档。
	s2, tt2 := newSuggestShell(t, f)
	s2.hist.add("林俊杰")
	tt2.Frame()
	typeInSearch(t, tt2, "周杰")
	settleSuggestDebounce(s2, tt2)
	waitFrames(t, tt2, "两档都在", func() bool { return tt2.HasText(suggestTermsHeading) })

	if err := tt2.Click("清除全部"); err != nil {
		t.Fatalf("点不到「清除全部」：%v，文字有 %q", err, tt2.Texts())
	}
	if got := s2.hist.list(); len(got) != 0 {
		t.Errorf("「清除全部」之后历史还剩 %q", searchValues(got))
	}
	if !tt2.HasText("周杰伦 晴天") {
		t.Errorf("「清除全部」把联想词那一档也清了，文字有 %q", tt2.Texts())
	}
}

// TestChoosingOneSearchesThatWord 钉住规格第五条：选中联想词等于选中那次的搜索。
//
// 四样一起钉：搜索页去了、词在 URL 上、**框里也是这个词**、浮层收掉了。少任何
// 一样用户看到的都是「选了它但没搜它」。
func TestChoosingOneSearchesThatWord(t *testing.T) {
	f := newBiliFixture(t, 1)
	f.setSuggestTerms("周杰", "周杰伦 晴天")
	s, tt := newSuggestShell(t, f)
	tt.Frame()

	typeInSearch(t, tt, "周杰")
	settleSuggestDebounce(s, tt)
	waitFrames(t, tt, "联想词出来", func() bool { return tt.HasText("周杰伦 晴天") })

	if err := tt.Click("周杰伦 晴天"); err != nil {
		t.Fatalf("点不到那条联想词：%v，文字有 %q", err, tt.Texts())
	}

	if got := s.router.Path(); got != searchPath {
		t.Errorf("选了之后在 %q，want %q", got, searchPath)
	}
	if got := s.router.Query(searchQuery); got != "周杰伦 晴天" {
		t.Errorf("URL 上的 q 是 %q，want 周杰伦 晴天", got)
	}
	// 框里也得是它：不填的话按返回再回来看到的是那个错的词（见 `viewHistoryChip`）。
	if got := s.searchKeyword; got != "周杰伦 晴天" {
		t.Errorf("搜索框里是 %q，want 周杰伦 晴天", got)
	}
	if s.searchSuggest {
		t.Error("选了一条之后浮层还开着")
	}
	if tt.HasText(suggestTermsHeading) {
		t.Errorf("浮层还在画面上，文字有 %q", tt.Texts())
	}
	// 选了就算搜过：历史里多一条（和点历史那一条是同一件事）。
	if got := searchValues(s.hist.list()); len(got) != 1 || got[0] != "周杰伦 晴天" {
		t.Errorf("选完之后历史是 %q，want [周杰伦 晴天]", got)
	}
}

// TestEscapeTakesBothSectionsAway 钉住「两档在同一个浮层里」。
//
// 规格的边界写着：联想词不许是另一个浮层——两套浮层的关闭时机不一样，并排会出现
// 「关了一个另一个还在」。Esc 一次之后，两档都必须不在画面上。
func TestEscapeTakesBothSectionsAway(t *testing.T) {
	f := newBiliFixture(t, 1)
	f.setSuggestTerms("周杰", "周杰伦 晴天")
	s, tt := newSuggestShell(t, f)
	s.hist.add("林俊杰")
	tt.Frame()

	typeInSearch(t, tt, "周杰")
	settleSuggestDebounce(s, tt)
	waitFrames(t, tt, "两档都在", func() bool { return tt.HasText(suggestTermsHeading) })

	tt.Key(0, mygoui.KeyEscape)
	tt.Frame()

	if s.searchSuggest {
		t.Errorf("按了 Esc 浮层还开着，文字有 %q", tt.Texts())
	}
	for _, gone := range []string{suggestTermsHeading, "搜索历史", "周杰伦 晴天", "林俊杰"} {
		if tt.HasText(gone) {
			t.Errorf("按了 Esc 之后 %q 还在画面上，文字有 %q", gone, tt.Texts())
		}
	}
}

// TestTheSearchWatchStopsWhenTheOverlayGoesAway 盯那个 125ms 的钟不会醒到进程退出。
//
// 用户敲了几个字、还没到 500ms 就按了 Esc：那个钟必须跟着停。停不掉的话用户看不出
// 任何异样，只是这个应用从那以后再没省过电。
func TestTheSearchWatchStopsWhenTheOverlayGoesAway(t *testing.T) {
	f := newBiliFixture(t, 1)
	s, tt := newSuggestShell(t, f)
	s.hist.add("林俊杰")
	tt.Frame()

	typeInSearch(t, tt, "周杰")
	if s.searchWatchStop == nil {
		t.Fatal("敲了字却没有排防抖的钟——防抖靠它到点")
	}

	tt.Key(0, mygoui.KeyEscape)
	tt.Frame()
	if s.searchWatchStop != nil {
		t.Error("收掉浮层之后那个钟还醒着")
	}
	// 醒着的时候它才发请求：收掉之后推多少时间都不该再问出去。
	advance(s, 5*time.Second)
	tt.Frame()
	if got := f.suggestCalls(); len(got) != 0 {
		t.Errorf("收掉浮层之后还是问了 %v", got)
	}
}

// TestTheHistorySettingDoesNotTakeTheSuggestWithIt 钉住设置的边界。
//
// 「显示搜索历史」管的是「我不想看见我搜过什么」，而联想词是 B 站这次给的。把它
// 一起关掉的话，用户在一个只想关掉历史的人那儿连联想都看不到，而界面上没有任何
// 地方能让他知道还有这一档。
func TestTheHistorySettingDoesNotTakeTheSuggestWithIt(t *testing.T) {
	f := newBiliFixture(t, 1)
	f.setSuggestTerms("周杰", "周杰伦 晴天")
	b := newTestShell(t,
		withSettings(app.DefaultSettings("")),
		withClock(newFakeClock(fakeClockStart)),
		mounted(func(t *testing.T, a *app.App) {
			a.MountLogin(app.NewMemoryStore("suggest-test"), f.client())
		}),
	)
	s, tt := b.s, b.tt
	s.feed.next = time.Time{}
	tt.SetFocused(true)
	if _, err := s.app.UpdateSettings(func(st *app.Settings) { st.ShowSearchHistory = false }); err != nil {
		t.Fatal(err)
	}
	s.hist.add("林俊杰")
	tt.Frame()

	typeInSearch(t, tt, "周杰")
	advance(s, searchDebounce)
	tt.Frame()
	waitFrames(t, tt, "联想词出来", func() bool { return tt.HasText(suggestTermsHeading) })

	if tt.HasText("搜索历史") || tt.HasText("林俊杰") {
		t.Errorf("设置关了还摆着历史那一档，文字有 %q", tt.Texts())
	}
	if !tt.HasText("周杰伦 晴天") {
		t.Errorf("设置关了连联想词也没了，文字有 %q", tt.Texts())
	}
	// 而历史一条都不能少（设置管的是显不显示，不是存不存，见 searchhistory.go）。
	if got := len(s.hist.list()); got != 1 {
		t.Errorf("关掉设置之后历史只剩 %d 条，设置管了存", got)
	}
}

// TestTheSuggestTermsCannotBeDeleted 钉住「联想词没有 ×」。
//
// 历史那一档的每一条都能单删（那是用户自己存的），而联想词是 B 站这次给的——摆一
// 个删不掉的东西上去比不摆还糟，而摆一个删了也没用的更是骗人。
func TestTheSuggestTermsCannotBeDeleted(t *testing.T) {
	f := newBiliFixture(t, 1)
	f.setSuggestTerms("周杰", "周杰伦 晴天")
	s, tt := newSuggestShell(t, f)
	s.hist.add("林俊杰")
	tt.Frame()

	typeInSearch(t, tt, "周杰")
	settleSuggestDebounce(s, tt)
	waitFrames(t, tt, "联想词出来", func() bool { return tt.HasText("周杰伦 晴天") })

	// 历史那条的删除按钮在（这是「两档不同」的又一处可观察后果）。
	if _, ok := tt.Find("删除「林俊杰」"); !ok {
		t.Fatalf("历史那条的删除按钮没了，文字有 %q", tt.Texts())
	}
	// 联想词那一档一个都没有。
	for _, term := range []string{"周杰伦 晴天"} {
		if _, ok := tt.Find("删除「" + term + "」"); ok {
			t.Errorf("联想词 %q 上有一个删除按钮——它删不掉（那是 B 站这次给的）", term)
		}
	}
	// 而且画面上除了「×」那个无障碍名字之外，不该有别的尖括号。
	if got := tt.Texts(); containsAny(got, "<em", "</em>") {
		t.Errorf("画面上露出了 B 站回包里的标签：%q", got)
	}
}

// containsAny 判这一堆文字里有没有哪一句含着其中一个片段。
func containsAny(texts []string, subs ...string) bool {
	for _, s := range texts {
		for _, sub := range subs {
			if strings.Contains(s, sub) {
				return true
			}
		}
	}
	return false
}
