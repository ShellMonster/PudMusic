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
	if !tt.HasText(routes[0].Empty.Title) {
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

	back, ok := tt.Find("Back")
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
