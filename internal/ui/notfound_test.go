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
//   - `sleepOpen` / `errDetailOpen`（播放条上同款的 Popover）。
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
// ⚠️ **其余几枚是「置位就够了」**：`sleepOpen` / `errDetailOpen` /
// `avatarMenuOpen` / `dynMenuOpen` / `queueClearOpen` /
// `full.partsOpen` / `lyric` 与 `fullLyric` 的 `offsetOpen`/`fontOpen`——
// 它们都是「点了开、不点不收」的普通标志（音量浮层已随原型改成内联滑块删除），没有 `partOpen`/`searchSuggest` 那种每帧收回的分支。
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

// DESIGN.md §0′.T7：404 按原型 `PAGES.notfound`——插画、「这一页走丢了」、说明和主色「回到推荐音乐」；
// 没有倒计时、不自动跳走（原来那几条倒计时 / 自动回首页的用例随旧设计移除）。
func TestThe404PageIsThePrototypeOne(t *testing.T) {
	s, tt := newShell(t)
	s.navigate("/根本没有这一页")
	tt.Frame()
	for _, want := range []string{"这一页走丢了", "链接可能已经失效，或者这一页还没有内容。", "回到推荐音乐"} {
		if !tt.HasText(want) {
			t.Errorf("404 页上没有 %q，文字有 %q", want, tt.Texts())
		}
	}
	if tt.HasText("秒后回到主页") {
		t.Errorf("404 页不该有倒计时：%q", tt.Texts())
	}
}

func TestThe404PageDoesNotRedirectByItself(t *testing.T) {
	s, tt := newShell(t)
	fake := fakeClockAt(t, s)
	s.navigate("/根本没有这一页")
	tt.Frame()
	path := s.router.Path()
	fake.Advance(10 * time.Second)
	tt.Frame()
	if got := s.router.Path(); got != path {
		t.Errorf("404 页自己跳走了：%q → %q", path, got)
	}
}

func TestThe404ButtonGoesHome(t *testing.T) {
	s, tt := newShell(t)
	s.navigate("/根本没有这一页")
	tt.Frame()
	if err := tt.Click("回到推荐音乐"); err != nil {
		t.Fatalf("点不了「回到推荐音乐」：%v", err)
	}
	tt.Frame()
	if got := s.router.Path(); got != "/" {
		t.Errorf("点了之后在 %q，want /", got)
	}
}
