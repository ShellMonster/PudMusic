package list

// 这一组跑的是翻页那一层：三重保障、防重入锁、取消与异常解锁、回顶。
//
// 全是 headless 的（MyGo 的 Tester + 假拉页器），不用人守在窗口前面拖。
// 后台拉页是并发的，所以断言走「跑到条件成立为止」而不是死等——见 pump。

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// stubFetcher 是假的拉页器：按页给结果，记下被叫了哪几页。
//
// gate 把某一页卡住，直到 release——用来把「还在拉」这个状态拉长，长到能看清
// 三重保障会不会在同一个在途请求上再叫一次。sticky 则是另一种：**无视取消**，
// 门一放行就给结果，模拟「响应已经回来了、用户这时候才换的筛选条件」。
type stubFetcher struct {
	mu        sync.Mutex
	calls     []int
	pages     map[int]Page  // 页号 → 给它的结果
	errs      map[int]error // 页号 → 给它的错
	gates     map[int]chan struct{}
	sticky    map[int]bool // 页号 → 无视 ctx 的取消
	cancelled map[int]bool // 页号 → 拉的时候有没有看见 ctx 被取消
}

func newStub() *stubFetcher {
	return &stubFetcher{
		pages:     map[int]Page{},
		errs:      map[int]error{},
		gates:     map[int]chan struct{}{},
		sticky:    map[int]bool{},
		cancelled: map[int]bool{},
	}
}

// with 装一页的结果。
func (s *stubFetcher) with(page int, n int, hasMore bool) *stubFetcher {
	return s.serve(page, Page{Songs: pageSongs(page, n), HasMore: hasMore}, nil)
}

// withSongs 装一页自己指定的歌。
func (s *stubFetcher) withSongs(page int, songs []Music, hasMore bool) *stubFetcher {
	return s.serve(page, Page{Songs: songs, HasMore: hasMore}, nil)
}

// fails 装一页的错。
func (s *stubFetcher) fails(page int, err error) *stubFetcher {
	return s.serve(page, Page{}, err)
}

// serve 换掉某一页的答案（重试之后改答案也走它）。
func (s *stubFetcher) serve(page int, pg Page, err error) *stubFetcher {
	s.mu.Lock()
	s.pages[page], s.errs[page] = pg, err
	s.mu.Unlock()
	return s
}

// gate 把这一页卡住，直到 release(page)。
func (s *stubFetcher) gate(page int) *stubFetcher {
	ch := make(chan struct{})
	s.mu.Lock()
	s.gates[page] = ch
	s.mu.Unlock()
	return s
}

// stickyPage 装一页「无视取消」的门：release 之后照样给结果。
func (s *stubFetcher) stickyPage(page int) *stubFetcher {
	s.mu.Lock()
	s.sticky[page] = true
	s.mu.Unlock()
	return s.gate(page)
}

// release 放行某一页。
func (s *stubFetcher) release(page int) {
	s.mu.Lock()
	ch := s.gates[page]
	delete(s.gates, page)
	s.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}

func (s *stubFetcher) fetch(ctx context.Context, page int) (Page, error) {
	s.mu.Lock()
	s.calls = append(s.calls, page)
	ch, sticky := s.gates[page], s.sticky[page]
	pg, err := s.pages[page], s.errs[page]
	s.mu.Unlock()

	if ch != nil {
		if sticky {
			// 已经回来的请求就是这样：取消对它没有意义，它照样会给结果。
			<-ch
		} else {
			select {
			case <-ch:
			case <-ctx.Done():
			}
		}
	}
	if !sticky {
		if cerr := ctx.Err(); cerr != nil {
			s.mu.Lock()
			s.cancelled[page] = true
			s.mu.Unlock()
			return Page{}, cerr
		}
	}
	return pg, err
}

// callsOf 报一共叫了几次、叫的是哪几页（顺序）。
func (s *stubFetcher) callsOf() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.calls...)
}

// sawCancel 报某一页拉的时候有没有看见 ctx 被取消（取消是真的取消）。
func (s *stubFetcher) sawCancel(page int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelled[page]
}

// pageSongs 造一页的歌。**ID 和标题都带页号**：翻页是在尾部追加，两页撞了 ID
// 就是铁律四要消掉的那种「状态串在一起」。
func pageSongs(page, n int) []Music {
	out := make([]Music, n)
	for i := range out {
		out[i] = Music{
			ID:    fmt.Sprintf("p%d-%d", page, i),
			Title: fmt.Sprintf("第 %d 页 歌 %d", page, i+1),
		}
	}
	return out
}

// scrollToBottom 是「一次滚到底」的滚动量。正的 DY 是往下滚
// （`platform/surface.go` 的 PointerScroll 注释原话：positive DY moves the …）。
const scrollToBottom = 100000

// newPaged 造一个分页列表并把 fetch 给它。
func newPaged(t *testing.T, fetch Fetcher) (*PagedList, *ui.Tester) {
	t.Helper()
	p := NewPaged(newCache(t), Actions{Notify: func(string) {}}, fetch)
	tt := ui.NewTester(p.View, testerW, testerH)
	tt.Frame()
	return p, tt
}

// pump 跑到 cond 成立为止。每一轮走一帧（顺带让后台那一支有机会跑）。
//
// ⚠️ 不能只 `tt.Frame()` 一次：拉页在后台 goroutine 里，结果要靠唤醒才进
// 下一帧。这里等到条件成立或者两秒为止——真等不到就是测试该红，不是慢。
func pump(t *testing.T, tt *ui.Tester, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		tt.Frame()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等了两秒还没%s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 三重保障
// ─────────────────────────────────────────────────────────────────────────────

// TestFirstRenderKeepsPullingUntilTheContainerIsFilled 是验收第 1 条，也是
// 「筛选后只剩两页」那一条：
//
// 用户没滚过（一次都没滚），第一页只有 3 行，容器是空的。**不能**停在那儿——
// 底下留一大片空白还一直转圈正是要消掉的失败模式。要自己把后面几页拉完，
// 拉到「拉完了」为止。
func TestFirstRenderKeepsPullingUntilTheContainerIsFilled(t *testing.T) {
	st := newStub().
		with(1, 3, true).
		with(2, 3, true).
		with(3, 40, false) // 最后一页：拉到底了

	p, tt := newPaged(t, st.fetch)

	pump(t, tt, "把容器填满并停下来", func() bool { return !p.Paged().Loading && p.Paged().Loaded > 40 })

	// 三页都拉到了，而且没有在拉了（不是「还在转圈」）。
	if got := p.Paged().Loaded; got != 46 {
		t.Errorf("拉完只有 %d 行，want 46（3+3+40）", got)
	}
	if p.Paged().Page != 3 {
		t.Errorf("停在第 %d 页，want 3", p.Paged().Page)
	}
	if p.Paged().HasMore {
		t.Errorf("最后一页说了没有更多，却还在 HasMore")
	}
	if p.Paged().Loading {
		t.Errorf("拉完了还在 Loading，界面会永远转圈")
	}
	// 底部不留空白：第一页那几行现在都被顶到画面上面去了，最后一行在画面上。
	if !onScreen(tt, "第 3 页 歌 1") {
		t.Errorf("最后一页的第一行不在画面上，文字有 %q", tt.Texts())
	}
	// 而且没有多拉：第 3 页拉完就没有第 4 页可拉了（`with` 只装了三页，
	// 装了第 4 页的话调用次数会露馅）。
	if c := st.callsOf(); len(c) != 3 {
		t.Errorf("叫了 %v 页，want [1 2 3]——多拉就是空转", c)
	}
}

// TestWhileTheFirstPageIsInFlightItSaysSo 是「第一页在途时画什么」。
//
// 一行都还没有的时候末尾那一行就是唯一那一行：这时要是还照默认那样说
// 「这里还没有歌」，同一屏上就摆着两句打架的话（一句说没有、一句在转圈）。
func TestWhileTheFirstPageIsInFlightItSaysSo(t *testing.T) {
	st := newStub().with(1, 200, false).gate(1)
	p, tt := newPaged(t, st.fetch)

	pump(t, tt, "第一页发出去", func() bool { return p.Paged().Loading })
	frame(tt)

	if tt.HasText("这里还没有歌") {
		t.Errorf("第一页还没回来就说「这里还没有歌」，文字有 %q", tt.Texts())
	}
	if !tt.HasText("正在加载…") {
		t.Errorf("第一页在途时没有说「正在加载…」，文字有 %q", tt.Texts())
	}

	st.release(1)
	pump(t, tt, "第一页落地", func() bool { return p.Paged().Loaded == 200 && !p.Paged().Loading })
	if tt.HasText("正在加载…") {
		t.Errorf("拉完了还在说「正在加载…」，文字有 %q", tt.Texts())
	}
}

// TestAScrolledListDoesNotPullUntilTheEndIsReached 是保障②：
//
// 第一页就装得满屏幕，这时候**不能**自己往下拉（Biu 只在末尾那一行进画面时
// 才拉）。用户滚到底才拉下一页。
func TestAScrolledListDoesNotPullUntilTheEndIsReached(t *testing.T) {
	st := newStub().with(1, 200, true).with(2, 200, false)
	p, tt := newPaged(t, st.fetch)

	pump(t, tt, "第一页落地", func() bool { return p.Paged().Loaded == 200 && !p.Paged().Loading })
	if c := st.callsOf(); len(c) != 1 {
		t.Fatalf("第一页装得满屏幕就自己往下拉了（叫了 %v），want 只叫第 1 页", c)
	}

	// 滚到底：一次滚一大截，等于滚到了末尾。
	//（mygo 的 PointerScroll：**正的 DY 是往下滚**，`platform/surface.go:168`）
	tt.Scroll(testerW/2, testerH/2, 0, scrollToBottom)
	pump(t, tt, "滚到底之后拉第 2 页", func() bool { return p.Paged().Page == 2 })
	pump(t, tt, "第 2 页拉完", func() bool { return !p.Paged().Loading })

	if p.Paged().Loaded != 400 {
		t.Errorf("滚到底之后一共 %d 行，want 400", p.Paged().Loaded)
	}
}

// TestTheThreeGuardsDoNotSendThreeRequestsForOnePage 是防重入：
//
// 把第 2 页卡在门上，让「正在拉」这个状态横跨很多帧。每一帧三重保障都会重新
// 问一遍「要不要拉」——问了很多遍，就该只发过一个请求。
func TestTheThreeGuardsDoNotSendThreeRequestsForOnePage(t *testing.T) {
	st := newStub().
		with(1, 200, true). // 装得满屏幕：②只在用户滚过去时才命中
		with(2, 200, true).
		gate(2)
	p, tt := newPaged(t, st.fetch)

	pump(t, tt, "第一页落地", func() bool { return p.Paged().Loaded == 200 })
	tt.Scroll(testerW/2, testerH/2, 0, scrollToBottom)
	pump(t, tt, "第 2 页发出去", func() bool { return p.Paged().Loading })

	// 现在第 2 页在途。跑它十几帧，每一帧三重保障都重新问一遍。
	for range 15 {
		frame(tt)
	}
	if c := st.callsOf(); len(c) != 2 {
		t.Fatalf("第 2 页在途时又发了 %d 个请求（叫了 %v），want 只发一个", len(c)-1, c)
	}

	st.release(2)
	pump(t, tt, "第 2 页落地", func() bool { return p.Paged().Loaded == 400 && !p.Paged().Loading })
}

// TestAnEmptyPageWithMoreIsTheEnd 是防空转：一页空的还说「还有」，接口在骗人。
// 再拉就是空转下去，界面上转圈转到天荒地老。
func TestAnEmptyPageWithMoreIsTheEnd(t *testing.T) {
	st := newStub().with(1, 2, true).withSongs(2, nil, true).with(3, 5, false)
	p, tt := newPaged(t, st.fetch)

	pump(t, tt, "拉完停下来", func() bool { return !p.Paged().Loading && len(st.callsOf()) > 1 })
	frame(tt)
	frame(tt)

	if p.Paged().HasMore {
		t.Errorf("空页之后还信「还有」，会一直空转")
	}
	if c := st.callsOf(); len(c) != 2 {
		t.Errorf("叫了 %v 页，want [1 2]：空的那页之后不该再拉", c)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 异常解锁
// ─────────────────────────────────────────────────────────────────────────────

// TestAFailedPageUnlocksAndOffersARetry 是验收第 3 条里最容易漏的那半句：
// **异常时锁要能解开**。
//
// 发一个会失败的请求，断言「正在加载」这个状态回到了 false——不然界面永远
// 转圈，而且再也点不动任何东西。
func TestAFailedPageUnlocksAndOffersARetry(t *testing.T) {
	boom := errors.New("网络炸了")
	st := newStub().with(1, 200, true).fails(2, boom).with(3, 5, false)
	p, tt := newPaged(t, st.fetch)

	pump(t, tt, "第一页落地", func() bool { return p.Paged().Loaded == 200 })
	tt.Scroll(testerW/2, testerH/2, 0, scrollToBottom)
	pump(t, tt, "第 2 页失败", func() bool { return p.Paged().Err != nil })

	if p.Paged().Loading {
		t.Fatalf("失败之后还在 Loading，界面会永远转圈")
	}
	if !errors.Is(p.Paged().Err, boom) {
		t.Errorf("Err = %v，want %v", p.Paged().Err, boom)
	}
	if !tt.HasText("加载失败，点此重试") {
		t.Errorf("失败了却没给人一条重试的路，文字有 %q", tt.Texts())
	}

	// 自动补屏**就此打住**：解锁了不等于可以自己再拉，那样是拉一次失败一次
	// 的死循环（失败 → 解锁 → 下一帧看见还没填满 → 又拉 → 又失败）。
	frame(tt)
	frame(tt)
	frame(tt)
	if c := st.callsOf(); len(c) != 2 {
		t.Errorf("失败之后又自己拉了（叫了 %v），want 停在 [1 2]", c)
	}

	// 用户点一下就继续：这一页换成能成的答案。
	st.serve(2, Page{Songs: pageSongs(2, 30), HasMore: true}, nil)
	if err := tt.Click("加载失败，点此重试"); err != nil {
		t.Fatalf("点不到重试：%v", err)
	}
	pump(t, tt, "重试之后第二页落地", func() bool { return p.Paged().Loaded == 230 && !p.Paged().Loading })
	if p.Paged().Err != nil {
		t.Errorf("重试成功了 Err 还没清掉：%v", p.Paged().Err)
	}
	// 第三页没装答案（拉它就会失败），而且**不该有人去拉它**：我们还停在靠下的
	// 位置，末尾不在画面上，自动补屏不该动。
	//
	// 叫的顺序是 [1 2 2]：第二页拉过一次（失败）＋用户点重试再拉一次。
	frame(tt)
	frame(tt)
	if c := st.callsOf(); len(c) != 3 || c[1] != 2 || c[2] != 2 {
		t.Errorf("叫了 %v，want [1 2 2]", c)
	}
}

// TestATimeoutUnlocksToo 是验收第 3 条的后半句：超时也要把锁解开。
//
// 超时和失败是同一条路（fetch 自己把 ctx 的错报出来），但它是最容易被漏的
// 那种：请求没报错，只是**没回来**——这里用一个一直卡在门上的请求来装。
func TestATimeoutUnlocksToo(t *testing.T) {
	st := newStub().with(1, 200, true).gate(2)
	st.serve(2, Page{}, context.DeadlineExceeded)
	p, tt := newPaged(t, st.fetch)

	pump(t, tt, "第一页落地", func() bool { return p.Paged().Loaded == 200 })
	tt.Scroll(testerW/2, testerH/2, 0, scrollToBottom)
	pump(t, tt, "第 2 页发出去", func() bool { return p.Paged().Loading })

	// 放行的同时请求自己超时了（不是我们取消的：st.sawCancel(2) 仍是 false）。
	st.release(2)
	pump(t, tt, "超时落地", func() bool { return p.Paged().Err != nil })

	if p.Paged().Loading {
		t.Fatalf("超时之后还在 Loading，界面会永远转圈")
	}
	if !errors.Is(p.Paged().Err, context.DeadlineExceeded) {
		t.Errorf("Err = %v，want DeadlineExceeded", p.Paged().Err)
	}
	if p.Paged().Page != 1 {
		t.Errorf("超时那一页不该算数，Page = %d，want 1", p.Paged().Page)
	}
	if st.sawCancel(2) {
		t.Errorf("这个超时是我们自己取消的，那就验的不是超时那条路了")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 取消与旧结果作废
// ─────────────────────────────────────────────────────────────────────────────

// TestReloadCancelsThePageInFlight 是验收第 3 条的前半句：中途改关键词能取消
// 在途请求。取消是真的取消（fetch 的 ctx 收到 Done，服务端收得到）。
func TestReloadCancelsThePageInFlight(t *testing.T) {
	st := newStub().with(1, 200, true).gate(2)
	p, tt := newPaged(t, st.fetch)

	pump(t, tt, "第一页落地", func() bool { return p.Paged().Loaded == 200 })
	tt.Scroll(testerW/2, testerH/2, 0, scrollToBottom)
	pump(t, tt, "第 2 页发出去", func() bool { return p.Paged().Loading })

	p.Reload() // 换关键词了
	pump(t, tt, "看见取消", func() bool { return st.sawCancel(2) })
}

// TestAStaleResultNeverLands 是 PLAN 风险栏点名的那一条：
// **用户连点筛选条件时，旧请求的结果不能覆盖新条件的结果**。
//
// 这一条是 cancel 挡不住的：请求已经回来了、正往信箱里投的时候，cancel 已经
// 来不及。所以靠的是代次（gen）。
func TestAStaleResultNeverLands(t *testing.T) {
	st := newStub().with(1, 200, true).stickyPage(2)
	p, tt := newPaged(t, st.fetch)

	pump(t, tt, "第一页落地", func() bool { return p.Paged().Loaded == 200 })
	tt.Scroll(testerW/2, testerH/2, 0, scrollToBottom)
	pump(t, tt, "第 2 页发出去", func() bool { return p.Paged().Loading })

	// 换关键词：这一页作废。**先**换好新条件的答案再 Reload —— Reload 当场就
	// 发第一页，答案换晚了它会拉到旧条件的那一份（那就是这个测试要抓的东西）。
	st.serve(1, Page{Songs: pageSongs(9, 3), HasMore: false}, nil)
	p.Reload()
	st.release(2) // 旧的那一页这时候才回来（它没理 ctx，照样给了 200 行）
	pump(t, tt, "新条件的第一页落地", func() bool { return p.Paged().Loaded == 3 && !p.Paged().Loading })
	frame(tt)
	frame(tt)

	if got := p.Paged().Loaded; got != 3 {
		t.Errorf("加载了 %d 行，want 3：旧条件那 200 行漏进来了", got)
	}
	if p.Paged().Page != 1 {
		t.Errorf("Page = %d，want 1", p.Paged().Page)
	}
	if onScreen(tt, "第 2 页 歌 1") {
		t.Errorf("旧条件那页的行还在画面上")
	}
}

// TestReloadClearsAndGoesBackToTheTop 是「切路由回顶」：换查询之后从第一页
// 重新来，滚动位置回到顶上。
func TestReloadClearsAndGoesBackToTheTop(t *testing.T) {
	st := newStub().with(1, 200, true)
	p, tt := newPaged(t, st.fetch)

	pump(t, tt, "第一页落地", func() bool { return p.Paged().Loaded == 200 })
	tt.Scroll(testerW/2, testerH/2, 0, scrollToBottom)
	frame(tt)
	if p.scroll.Y < 400 {
		t.Fatalf("没滚下去（Y=%v），后面那句就是假的", p.scroll.Y)
	}

	st.serve(1, Page{Songs: pageSongs(9, 3), HasMore: false}, nil)
	p.Reload()
	pump(t, tt, "新查询落地", func() bool { return p.Paged().Loaded == 3 && !p.Paged().Loading })
	frame(tt)

	if p.scroll.Y != 0 {
		t.Errorf("换查询之后滚到 Y=%v，want 0（切路由要回顶）", p.scroll.Y)
	}
	if !onScreen(tt, "第 9 页 歌 1") {
		t.Errorf("换查询之后第一行不在画面上，文字有 %q", tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 回顶按钮
// ─────────────────────────────────────────────────────────────────────────────

// TestBackToTopAppearsAfter400Pixels 是验收第 4 条的后半句。400 是死数。
func TestBackToTopAppearsAfter400Pixels(t *testing.T) {
	st := newStub().with(1, 200, false)
	p, tt := newPaged(t, st.fetch)

	pump(t, tt, "第一页落地", func() bool { return p.Paged().Loaded == 200 })
	if tt.HasText("返回顶部") {
		t.Fatalf("还没滚就出现回顶按钮了")
	}

	// 滚 300 像素：不够，不出现。
	tt.Scroll(testerW/2, testerH/2, 0, 300)
	frame(tt)
	if p.scroll.Y < 300 {
		t.Skipf("Tester 一次滚不动 300 像素（Y=%v），这一条验不了", p.scroll.Y)
	}
	if tt.HasText("返回顶部") {
		t.Errorf("滚了 %v 像素就出现回顶按钮了，want 400 以后", p.scroll.Y)
	}

	// 再滚到 400 以上：出现。
	tt.Scroll(testerW/2, testerH/2, 0, 200)
	frame(tt)
	if p.scroll.Y <= backTopPx {
		t.Fatalf("滚到 %v 像素还是没有回顶按钮", p.scroll.Y)
	}
	if !tt.HasText("返回顶部") {
		t.Fatalf("滚过 400 像素却没有回顶按钮，文字有 %q", tt.Texts())
	}

	// 点它：回顶。
	if err := tt.Click("返回顶部"); err != nil {
		t.Fatalf("点不到回顶按钮：%v", err)
	}
	frame(tt)
	frame(tt)
	if p.scroll.Y != 0 {
		t.Errorf("点了回顶之后 Y=%v，want 0", p.scroll.Y)
	}
	if tt.HasText("返回顶部") {
		t.Errorf("回到顶上之后按钮还在")
	}
	if !onScreen(tt, "第 1 页 歌 1") {
		t.Errorf("回到顶上之后第一行不在画面上，文字有 %q", tt.Texts())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 行身份（铁律四）
// ─────────────────────────────────────────────────────────────────────────────

// TestAppendingPagesKeepsTheChosenSong 是铁律四在翻页上的验收点：
// **追加是在尾部插 key**，而筛选后重建是在头部插——两条规则不一样。翻完页，
// 选中的那一首还是它。
func TestAppendingPagesKeepsTheChosenSong(t *testing.T) {
	st := newStub().with(1, 200, true).with(2, 200, false)
	p, tt := newPaged(t, st.fetch)

	pump(t, tt, "第一页落地", func() bool { return p.Paged().Loaded == 200 })
	if err := tt.Click("第 1 页 歌 5"); err != nil {
		t.Fatalf("点不到第 5 行：%v", err)
	}
	if m, ok := p.list.Selected(); !ok || m.ID != "p1-4" {
		t.Fatalf("选中的是 %v，want p1-4", m.ID)
	}

	// 滚到底拉第二页：200 行追加在**尾部**。
	tt.Scroll(testerW/2, testerH/2, 0, scrollToBottom)
	pump(t, tt, "第二页落地", func() bool { return p.Paged().Loaded == 400 && !p.Paged().Loading })

	m, ok := p.list.Selected()
	if !ok {
		t.Fatalf("翻完页选中没了")
	}
	if m.ID != "p1-4" {
		t.Errorf("翻完页选中的是 %q，want p1-4（追加不该动到上面那些行的身份）", m.ID)
	}
}
