package ui

import (
	"bytes"
	"testing"
	"time"

	mygoui "github.com/egoist/mygo/ui"
)

// 404 这一组盯的是 Biu 的 P-30：走到不存在的路径上要有一页看得见的东西，
// 3 秒之后自己回首页。
//
// 🔴 这 3 秒用**假时钟**测，不真 `time.Sleep`。SPEC 的 Testing Decisions 把假
// 时钟列为每个阶段都要留下的回归证据：真等的话一条测试干等 3 秒，一轮几十条
// 就是一分多钟，慢的机器上还会随机地挂。下面每一条都是一行 Advance。

// fakeClockAt 给外壳换上假时钟，返回它。
func fakeClockAt(t *testing.T, s *Shell) *fakeClock {
	t.Helper()
	f := newFakeClock(time.Unix(2026, 0))
	s.clock = f
	return f
}

// TestNotFoundRedirectsHomeAfterThreeSeconds 是这一张的主验收点。
//
// 逐秒推进，每秒都查两件事：还在不在 404 上，倒计时写的对不对。3 秒一到就该
// 自己回首页——不是「再等一会儿」，也不是回到首页还留着倒计时。
func TestNotFoundRedirectsHomeAfterThreeSeconds(t *testing.T) {
	s, tt := newShell(t)
	fake := fakeClockAt(t, s)

	s.navigate("/根本没有这一页")
	tt.Frame()
	if got := s.router.Path(); got != "/根本没有这一页" {
		t.Fatalf("没停在 404 上（在 %q）", got)
	}

	// 3 → 2 → 1：每一跳之前先查数字对不对、还在不在 404 上。第 3 秒走完就该
	// 回首页了，所以那一跳只查回没回。
	for _, want := range []string{"3 秒后回到主页...", "2 秒后回到主页...", "1 秒后回到主页..."} {
		if !tt.HasText(want) {
			t.Fatalf("画面上不是 %q，文字有 %q", want, tt.Texts())
		}
		if got := s.router.Path(); got != "/根本没有这一页" {
			t.Fatalf("还在倒计时却已经不在 404 上了（在 %q），画面上刚才是 %q", got, want)
		}
		fake.Advance(notFoundTick)
		tt.Frame()
	}

	// 第三秒走完就该回首页了。
	if got := s.router.Path(); got != "/" {
		t.Fatalf("3 秒之后还在 %q，没回首页", got)
	}
	if tt.HasText("秒后回到主页...") {
		t.Errorf("已经回首页了，画面上还留着倒计时，文字有 %q", tt.Texts())
	}
	// 判首页画出来了没有看它自己的那一排 Tab：`routes[0].Empty.Title` 那句
	// 「暂无内容」是上游的共享默认文案，13 个页面都画它，拿它当判据等于没断。
	if !tt.HasText(homeTabs[0]) {
		t.Errorf("回首页之后首页没画出来，文字有 %q", tt.Texts())
	}
}

// TestLeavingThe404PageCancelsTheRedirect 验「只在 404 上等才跳」。
//
// 漏了这一条的后果很具体：用户走进 404，看了两秒觉得不对，自己点回「历史记录」
// ——然后三秒后被硬拽回首页。
func TestLeavingThe404PageCancelsTheRedirect(t *testing.T) {
	s, tt := newShell(t)
	fake := fakeClockAt(t, s)

	s.navigate("/根本没有这一页")
	tt.Frame()
	if !tt.HasText("秒后回到主页...") {
		t.Fatalf("没落到 404 页，文字有 %q", tt.Texts())
	}

	s.navigate("/history")
	tt.Frame()

	fake.Advance(10 * notFoundTick)
	tt.Frame()

	if got := s.router.Path(); got != "/history" {
		t.Errorf("离开 404 之后还是被拽走了（在 %q）", got)
	}
	if tt.HasText("秒后回到主页...") {
		t.Errorf("已经不在 404 上了，画面上还留着倒计时，文字有 %q", tt.Texts())
	}
}

// TestBackFromNotFoundDoesNotRedirect 验后退按钮也能解掉那个倒计时。
//
// 这是 syncRoute 为什么不能只写在 navigate 里的原因：后退/前进走的是 Router
// 自己的历史（viewNav 里那对 BackButton / ForwardButton），根本不经过 navigate。
// 只在 navigate 里收的话，这条路径就漏了——用户从 404 后退到首页，三秒后照样
// 被拽走，而且拽回的还是首页，看起来像是「点了没反应又跳回来了」。
func TestBackFromNotFoundDoesNotRedirect(t *testing.T) {
	s, tt := newShell(t)
	fake := fakeClockAt(t, s)

	s.navigate("/根本没有这一页")
	tt.Frame()

	back, ok := tt.Find(navBackLabel)
	if !ok {
		t.Fatalf("导航栏上没有后退，文字有 %q", tt.Texts())
	}
	tt.ClickAt(back.X+back.W/2, back.Y+back.H/2)
	tt.Frame()
	if got := s.router.Path(); got != "/" {
		t.Fatalf("后退之后在 %q，want /", got)
	}

	fake.Advance(10 * notFoundTick)
	tt.Frame()
	if got := s.router.Path(); got != "/" {
		t.Errorf("从 404 后退之后，三秒后还是被拽到了 %q", got)
	}
}

// TestForwardBackIntoNotFoundRearmsTheCountdown 验再走进 404 时倒计时重新开始。
//
// 路由历史里那一条 404 还在，往前再走回去，不重新起算的话会拿上一次的旧时刻
// 当起点——第二次走进去还没站稳就被弹回首页。
func TestForwardBackIntoNotFoundRearmsTheCountdown(t *testing.T) {
	s, tt := newShell(t)
	fake := fakeClockAt(t, s)

	s.navigate("/根本没有这一页")
	tt.Frame()
	fake.Advance(2 * notFoundTick)
	tt.Frame()

	s.router.Back()
	tt.Frame()
	s.router.Forward()
	tt.Frame()

	if got := s.router.Path(); got != "/根本没有这一页" {
		t.Fatalf("前进之后在 %q，没回到 404", got)
	}
	if !tt.HasText("3 秒后回到主页...") {
		t.Errorf("再进 404 时倒计时没有从头开始，文字有 %q", tt.Texts())
	}

	// 这一次从头数满 3 秒照样回首页。
	fake.Advance(3 * notFoundTick)
	tt.Frame()
	if got := s.router.Path(); got != "/" {
		t.Errorf("再进 404 之后 3 秒没回首页（在 %q）", got)
	}
}

// countingClock 数着「现在有几个定时器在跑」。抓的是倒计时落下没停——
// 那种漏在界面上一个症状都没有：数字照样对，跳回首页的时机照样准，只是多一个
// 定时器每秒空叫醒一次界面，直到窗口关掉。
type countingClock struct {
	*fakeClock
	live int
}

func (c *countingClock) EveryFunc(d time.Duration, f func()) func() {
	c.live++
	stop := c.fakeClock.EveryFunc(d, f)
	return func() {
		c.live--
		stop()
	}
}

// TestNotFoundToNotFoundRearmsWithoutLeavingATickerBehind 验 404 → 另一个 404
// 不会把上一个倒计时落下。
//
// `/没有这条` → `/也没有这条` 也算换页，会重新起一个倒计时。少收一次的话，
// 上一次那个每秒一跳的定时器就一直醒到窗口关掉。
func TestNotFoundToNotFoundRearmsWithoutLeavingATickerBehind(t *testing.T) {
	s, tt := newShell(t)
	counter := &countingClock{fakeClock: newFakeClock(time.Unix(2026, 0))}
	s.clock = counter

	s.navigate("/没有这条")
	tt.Frame()
	if counter.live != 1 {
		t.Fatalf("进了 404 之后有 %d 个定时器在跑，want 1", counter.live)
	}

	s.navigate("/也没有这条")
	tt.Frame()
	if got := s.router.Path(); got != "/也没有这条" {
		t.Fatalf("没切到第二个 404（在 %q）", got)
	}
	if !tt.HasText("没有「/也没有这条」这一页") {
		t.Errorf("第二个 404 没画出来，文字有 %q", tt.Texts())
	}
	if !tt.HasText("3 秒后回到主页...") {
		t.Errorf("第二个 404 的倒计时没有从头开始，文字有 %q", tt.Texts())
	}
	if counter.live != 1 {
		t.Errorf("切到第二个 404 之后有 %d 个定时器在跑，want 1（上一个没停）", counter.live)
	}

	// 回到正常页面，定时器该归零。
	s.navigate("/history")
	tt.Frame()
	if counter.live != 0 {
		t.Errorf("离开 404 之后还有 %d 个定时器在跑", counter.live)
	}
}

// TestNotFoundPageSaysWhichPathIsMissing 验 404 说的是哪一条路径。
//
// 上游那一页只知道「没匹配上」，不知道用户敲了什么。这一条是这一页唯一非上游
// 的字，也是它最该说清的一件事：不然用户看到「没有这一页」，不知道是自己敲错
// 了还是应用坏了。
func TestNotFoundPageSaysWhichPathIsMissing(t *testing.T) {
	s, tt := newShell(t)
	fakeClockAt(t, s)

	s.navigate("/collection/abc/def")
	tt.Frame()

	if !tt.HasText("没有「/collection/abc/def」这一页") {
		t.Errorf("404 没有说清楚是哪条路径，文字有 %q", tt.Texts())
	}
	if !tt.HasText("404") {
		t.Errorf("404 页上没写 404，文字有 %q", tt.Texts())
	}
}

// TestNotFoundPageIsPaintedNotBlank 验 404 不是一片白。
//
// 和空状态那边同一个理由：只查元素框发现不了「画出来看不见」。
func TestNotFoundPageIsPaintedNotBlank(t *testing.T) {
	s, tt := newShell(t)
	fakeClockAt(t, s)

	s.navigate("/根本没有这一页")
	tt.Frame()

	r, ok := tt.Find("404")
	if !ok {
		t.Fatalf("404 页没画出来，文字有 %q", tt.Texts())
	}
	img := tt.Image()
	window := img.RGBAAt(4, 4)
	text := img.RGBAAt(int(r.X)+2, int(r.Y+r.H/2))
	if text == window {
		t.Errorf("404 的字和窗口底色同色（%v），画出来看不见", text)
	}
}

// TestNotFoundCountdownText 验倒计时那句话和上游逐字一致。
//
// a6c48e1 那一版是 `{count} 秒后回到主页...`，末尾三个是 **ASCII 的点**，不是
// `…`。看着像可以「顺手改成省略号」，改了在中文界面上就是一个明显不一样的
// 字符，而这一句是要照抄上游的。
func TestNotFoundCountdownText(t *testing.T) {
	for n, want := range map[int]string{
		3: "3 秒后回到主页...",
		2: "2 秒后回到主页...",
		1: "1 秒后回到主页...",
		0: "0 秒后回到主页...",
	} {
		if got := notFoundCountdown(n); got != want {
			t.Errorf("倒计时 %d 是 %q，want %q", n, got, want)
		}
	}
}

// TestNotFoundLeftCountsDown 验「还剩几秒」是按时间算的，不是记着减了几次。
//
// 真时钟上那一跳和这一帧之间差几毫秒是随机的，记次数会出现「数字写着 2、实际
// 只剩 0.4 秒」，然后突然跳成 0。
func TestNotFoundLeftCountsDown(t *testing.T) {
	s, _ := newShell(t)
	fake := fakeClockAt(t, s)

	s.armNotFound()
	if got := s.notFoundLeft(); got != 3 {
		t.Errorf("刚进去剩 %d 秒，want 3", got)
	}
	for _, c := range []struct {
		after time.Duration
		want  int
	}{
		{0, 3},                      // 刚进去
		{100 * time.Millisecond, 3}, // 走了不到一秒，仍该写着 3
		{time.Second, 2},
		{1900 * time.Millisecond, 2},
		{2 * time.Second, 1},
		{2900 * time.Millisecond, 1},
		{3 * time.Second, 0},
	} {
		s.disarmNotFound()
		s.armNotFound()
		fake.Advance(c.after)
		if got := s.notFoundLeft(); got != c.want {
			t.Errorf("进去 %v 之后剩 %d 秒，want %d", c.after, got, c.want)
		}
	}
}

// TestEveryPageViewIsGuarded 验每一页 view 里 panic 都不会带走进程。
//
// 🔴 SPEC 的「界面崩溃」那一条：原生 UI 下 view 里 panic 会直接杀进程，框架的
// Dispatch 和 drain 都没有 recover。这里逐条跑一遍 14 条路由，每一条都往那一页
// 的 pageBody 里塞一个 panic，验进程活着、切到了错误页、而且换一页就出来。
//
// ⚠️ 它守的是**行为**，不是 page 里那道 Guard 本身：panic 从 page 穿出去之后，
// viewMain 外层那道 Guard（`s.draw = Guard(s.viewMain, …)`）一样接得住，把
// page 里面那道换成裸调用这一条照样全绿。里面那道是按 SPEC 的要求加的
// （「每个页面 view 外包一层 recover」），留着是为了 page 这一层自己站得住，
// 不依赖调用它的那一层恰好也有 Guard。
func TestEveryPageViewIsGuarded(t *testing.T) {
	paths := map[string]string{
		"/user/{id}":       "/user/1",
		"/collection/{id}": "/collection/2",
	}
	for _, rt := range append(append([]Route{}, routes...), notFound) {
		t.Run(rt.Match, func(t *testing.T) {
			// 这一组故意 panic 14 次，把日志接到内存里，免得刷一屏栈出来。
			restore := logOutput(&bytes.Buffer{})
			t.Cleanup(restore)

			s, tt := newShell(t)
			target, ok := paths[rt.Match]
			if !ok {
				target = rt.Match
			}

			s.pageBody = func(*mygoui.Context, *mygoui.Route, Route) { panic("这一页画崩了") }
			s.navigate(target)
			tt.Frame()

			if s.crash == nil {
				t.Fatalf("%s 里 panic 没有被 recover 住", rt.Match)
			}
			if !tt.HasText("界面出错了") {
				t.Errorf("%s 没有切到错误页，文字有 %q", rt.Match, tt.Texts())
			}

			// 换一页就该出来——一页崩了不能连累后面那些页。
			s.pageBody = s.pageContent
			s.navigate("/history")
			tt.Frame()
			if s.crash != nil {
				t.Errorf("换页之后崩溃状态没复位：%v", s.crash)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 换页收浮层
// ─────────────────────────────────────────────────────────────────────────────
//
// # 为什么收在 syncRoute 而不是 navigate
//
// `navigate` 是**一条**换页的路：它清 Toast、复位 crash、`router.Push`——
// **它不碰浮层**（那是这一票之前的事）。而「换页」不止那一条道：
//
//   - 顶栏那对 `ui.BackButton` / `ForwardButton`（navbar.go）是**框架控件**——
//     按下去直接 `r.Go(±1)`，走 Router 自己的历史，**根本不经过 Shell**。
//   - `navigateBack`（shortcut_dispatch.go 的后退快捷键）走 `s.router.Back()`。
//   - `goLogin`（login.go）走 `s.router.Push`。
//   - 404 的倒计时回首页（本文件上面那一段）也是 `s.navigate`。
//
// ⇒ 所以「换页了」唯一的收口是 `syncRoute`（理由同上面那段注释）：它在
// `syncUnderlay` 里**每帧**跑，拿 `s.shownPath` 的边沿认出「换页了」。
//
// # 收哪几层
//
// 换页了旧页的浮层都不该留着——它们盖的是**上一页**的东西，下一页上悬着一块
// 上一页的浮层，用户第一眼看到的是「这页坏了」。
//
//   - `commentsOpen`（评论抽屉，`ui.Overlay`）：置 false 之后
//     `syncCommentsDrawerPhase` 会走 `drawerOut` 正常滑出，不是硬消失。
//   - `drawerOpen`（队列抽屉，同款 Overlay）：同上。
//   - `queueClearOpen`（抽屉里的「清空播放列表」确认）：**不是抽屉的一部分**——
//     抽屉收了它状态还活着，下次开抽屉那个没回答的问题直接复活。
//   - `partOpen`（分集浮层）：框架 `Popover`——「点外面自己关」那一条对**换页**
//     无效（换页不是「点外面」），所以得显式收。
//   - `rateOpen`（倍速菜单）：同上，框架 Popover。
//   - `sleepOpen` / `volOpen` / `errDetailOpen`（播放条上同款的 Popover）。
//   - `searchSuggest`（搜索联想，`PopoverBase`）/ `avatarMenuOpen` /
//     `dynMenuOpen`（同款 PopoverBase）。
//   - `full.partsOpen`（满屏的分集面板）与 `lyric` / `fullLyric` 各自的
//     `offsetOpen` / `fontOpen`（歌词区右下角那两个 `PopoverBase`）。
//
// 没问的（**显式不收**，理由在 `closeOverlaysForNav` 顶上那段）：收藏夹管理
// 那几个装着输入的弹窗、各页的破坏性确认框、歌词搜索/预览、关于面板、
// 满屏播放器本身。
//
// ⚠️ **收的是「意图」（open=false），不是「阶段」**：`commentsPhase` /
// `drawerPhase` 是各自的 `syncXxx` 阶段机推的——把阶段机也硬掰成 `drawerGone`
// 的话面板是**硬消失**的（没有滑出那一段），而「抽屉该收」收的是「该走了」
// 这个意图，滑出那 200ms 照旧走。
//
// ⚠️ **抽屉里没有「自己发起换页」的控件**：评论抽屉里能点的只有「展开/收起/
// 楼中楼/刷新」（`comments.go` / `comments_body.go`），队列抽屉里能点的只有
// 队列行和清空——**没有一处调 `s.navigate`**。所以「先收抽屉、换页在下一帧
// 才发生」的顺序不存在「收早了把正在导航的那下关动作弄闪烁」的问题。

// openAllOverlays 把这一票管的浮层**全都打开**。
//
// ⚠️ **外壳得是真的在线那一档**（`onlineTester`，pagelist 顶成 403）：
// `viewPartButton` 在「没有歌 / 这一首问不到分集」那一支**每帧都把 `partOpen`
// 收掉**——拿 `newShell` 直接置 `s.partOpen = true` 的话，下一帧它又被播放条
// 自己收回去，「换页收没收」就量不出来（收的是播放条，不是换页）。而
// 「pagelist 顶成 403」那一档 `PartsWorthAsking` 仍是 true——入口还在、
// `partOpen` 留得住。
//
// ⚠️ **三个浮层是真的点开的，不是置位**：`partOpen`（`openPartsDrawer`，点开
// 顺道把「入口还摆着」也验了）和**搜索联想**（窗口得有焦点 + 点一下搜索框——
// `searchSuggest` 在「框没焦点」那一支每帧自动收，直接置位下一帧就被搜索框
// 自己收回去，和 `partOpen` 是同一个坑；而 `tt.SetFocused(true)` 无头窗口默认
// 是没焦点的，少这一句点了也白点）。
//
// ⚠️ **其余几枚是「置位就够了」**：`sleepOpen` / `volOpen` / `errDetailOpen` /
// `avatarMenuOpen` / `dynMenuOpen` / `queueClearOpen` /
// `full.partsOpen` / `lyric` 与 `fullLyric` 的 `offsetOpen`/`fontOpen`——
// 它们都是「点了开、不点不收」的普通标志（`volOpen` 有个闲置宽限，但下一帧
// 断言时远没到点），没有 `partOpen`/`searchSuggest` 那种每帧收回的分支。
func openAllOverlays(t *testing.T, s *Shell, tt *mygoui.Tester) {
	t.Helper()
	// ⚠️ **搜索联想不在这里开**（单独有 `TestNavigatingClosesTheSuggest`）：
	// 它要点搜索框 + 敲字才置位，而抽屉那块蒙版盖住整窗（播放条除外）——抽屉
	// 开着的时候搜索框点不到，两个状态在无头 Tester 里**造不出来同框**。它
	// 单独断「开着换页就丢」，见那条测试。
	s.commentsOpen = true
	s.drawerOpen = true
	s.queueClearOpen = true
	s.rateOpen = true
	s.sleepOpen = true
	s.volOpen = true
	s.errDetailOpen = true
	s.avatarMenuOpen = true
	s.dynMenuOpen = true
	s.full.partsOpen = true
	s.lyric.offsetOpen = true
	s.lyric.fontOpen = true
	s.fullLyric.offsetOpen = true
	s.fullLyric.fontOpen = true
	openPartsDrawer(t, tt)
	if !s.partOpen {
		t.Fatal("分集浮层没点开——起点就不对（入口被收了的话，换页收它这条就量不到）")
	}
}

// overlaysStillOpen 报这些浮层里还有几个开着。
func overlaysStillOpen(s *Shell) map[string]bool {
	return map[string]bool{
		"评论抽屉":    s.commentsOpen,
		"队列抽屉":    s.drawerOpen,
		"清空队列确认框": s.queueClearOpen,
		"分集浮层":    s.partOpen,
		"倍速菜单":    s.rateOpen,
		"睡眠定时":    s.sleepOpen,
		"音量浮层":    s.volOpen,
		"错误详情":    s.errDetailOpen,
		"头像下拉":    s.avatarMenuOpen,
		"动态卡片菜单":  s.dynMenuOpen,
		"满屏分集面板":  s.full.partsOpen,
		"歌词字号浮层":  s.lyric.fontOpen,
		"歌词偏移浮层":  s.lyric.offsetOpen,
		"满屏歌词字号":  s.fullLyric.fontOpen,
		"满屏歌词偏移":  s.fullLyric.offsetOpen,
	}
}

// TestNavigatingClosesTheOverlays 断「换页 → 浮层全收」。
//
// 🔴 **这是这一票的主验收**：`openAllOverlays` 把该收的那一整片打开（两个
// 抽屉、清空确认、五个 Popover、五个 PopoverBase、满屏分集、两对歌词旋钮）——
// 换一页，一层不剩。
//
// ⚠️ **走的是 `syncRoute` 那条路**（不是 `navigate`）：换页不止 `navigate`
// 一条道（见上面那段），所以这里模拟的是**路由历史直接变**（顶栏那对
// BackButton / ForwardButton 就是这么走的）——`s.router.Push` 之后推一帧，
// `syncRoute` 拿 `shownPath` 的边沿认出换页、把浮层收掉。
func TestNavigatingClosesTheOverlays(t *testing.T) {
	s, tt := onlineTester(t, nil, true)
	openAllOverlays(t, s, tt)

	// 换页：不经过 `navigate`（它只清 Toast 和 crash），直接改路由历史——
	// 顶栏那对 BackButton / ForwardButton 就是这么走的（`r.Go(±1)`）。
	s.router.Push("/history")
	tt.Frame()

	for name, open := range overlaysStillOpen(s) {
		if open {
			t.Errorf("换页之后 %s 还开着", name)
		}
	}
}

// TestNavigatingBackClosesTheOverlays 断「**后退**也收浮层」。
//
// 🔴 **这是 `syncRoute` 为什么必须管这件事的另一半**：后退走的是 Router
// 自己的历史（`s.router.Back()`，顶栏 `ui.BackButton` 就是按它），
// 根本不经过 `navigate`——只在 `navigate` 里收的话这条路径就漏了。
func TestNavigatingBackClosesTheOverlays(t *testing.T) {
	s, tt := onlineTester(t, nil, true)

	s.router.Push("/history")
	tt.Frame()
	openAllOverlays(t, s, tt)

	// 后退：Router 自己的历史，不经过 navigate。
	s.router.Back()
	tt.Frame()

	for name, open := range overlaysStillOpen(s) {
		if open {
			t.Errorf("后退之后 %s 还开着", name)
		}
	}
}

// TestNavigatingClosesTheSuggest 断「搜索联想开着换页也收」。
//
// ⚠️ **它单独一条，不在 `openAllOverlays` 里**：联想面板要点搜索框 + 敲字才
// 开，而两个抽屉的蒙版盖住整窗（播放条除外）——抽屉开着的时候搜索框根本点
// 不到，「抽屉 + 联想同时开」在无头 Tester 里造不出来（真人那里也一样：
// 抽屉蒙版下面那一层点不动）。所以这一条单独断「它开着换页就丢」——同一
// 个 `closeOverlaysForNav`，起点不同罢了。
func TestNavigatingClosesTheSuggest(t *testing.T) {
	s, tt := onlineTester(t, nil, true)
	tt.SetFocused(true)
	if err := tt.Click(searchLabel); err != nil {
		t.Fatalf("点不到搜索框：%v，文字有 %q", err, tt.Texts())
	}
	tt.Type("周")
	tt.Frame()
	if !s.searchSuggest {
		t.Fatal("敲了字联想浮层没开——起点就不对")
	}

	s.router.Push("/history")
	tt.Frame()

	if s.searchSuggest {
		t.Error("换页之后搜索联想还开着")
	}
}

// TestNavigatingDoesNotLeaveTheDrawerPhasesStuck 断「收的是意图，不是硬掰阶段」。
//
// 🔴 **置 open=false 之后，阶段机该自己走「退场」那一段**——`commentsPhase`
// 从 `drawerShown` 变 `drawerOut`（不是直接 `drawerGone`），面板滑出那 200ms
// 照旧走；硬掰成 `drawerGone` 的话是「啪」地没了，不是滑出去。
func TestNavigatingDoesNotLeaveTheDrawerPhasesStuck(t *testing.T) {
	s, tt := newShell(t)

	// 抽屉先开到「到位了」那一档。
	s.commentsOpen = true
	tt.Frame()
	s.commentsPhase = drawerShown
	s.drawerOpen = true
	tt.Frame()
	s.drawerPhase = drawerShown

	s.router.Push("/history")
	tt.Frame()

	if s.commentsPhase != drawerOut {
		t.Errorf("换页之后 commentsPhase 是 %d，want %d（它该开始往窗口外面滑，不是硬消失）",
			s.commentsPhase, drawerOut)
	}
	if s.drawerPhase != drawerOut {
		t.Errorf("换页之后 drawerPhase 是 %d，want %d（同上）", s.drawerPhase, drawerOut)
	}
}

// TestThe404PageHasAWayOutRightNow 钉住「404 上立刻能回去」。
//
// # 为什么这一条要紧
//
// 原来这一页**只有一条出路**：等 3 秒自动跳首页。而 404 一般是**用户点错了
// 链接**——他多半想去别的页，不是主页；那个倒计时把他丢到主页，他刚才想找的
// 东西没了（UX 报告 §3-A 的 A4）。
//
// ⚠️ **判据是「点下去立刻就回」**：光断言「页面上有那枚按钮」的话，一枚按了
// 没反应的按钮也照样绿——而那正是原来那一版的病。
//
// ⚠️ **推钟推十秒也不该再被拽走**：倒计时在按下去的那一刻就要收掉，否则它会在
// 用户已经回首页之后**再拽一次**。
func TestThe404PageHasAWayOutRightNow(t *testing.T) {
	s, tt := newShell(t)
	fake := fakeClockAt(t, s)

	s.navigate("/根本没有这一页")
	tt.Frame()

	if !tt.HasText("回到主页") {
		t.Fatalf("404 页上没有「回到主页」那一枚，文字有 %q", tt.Texts())
	}
	if err := tt.Click("回到主页"); err != nil {
		t.Fatalf("点不了「回到主页」：%v", err)
	}
	tt.Frame()

	if got := s.router.Path(); got != "/" {
		t.Errorf("点了「回到主页」之后在 %q，want /", got)
	}

	fake.Advance(10 * notFoundTick)
	tt.Frame()
	if got := s.router.Path(); got != "/" {
		t.Errorf("回了首页之后倒计时还活着，又把用户拽到了 %q", got)
	}
}
