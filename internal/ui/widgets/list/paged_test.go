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

// TestTheVeryFirstFrameAlreadySaysLoading 是验收里那条「用户第一眼看到什么」。
//
// 第一页在途的时候，第一帧画出来就该是「正在加载…」，**不是**「这里还没有歌」。
//
// ⚠️ 这一条不是洁癖：拉第一页**必须**在画之前发出去。画完再发的话——
//
//   - 这一帧的 loading 还是 false，空状态装不上「正在加载…」；
//   - 而且画完到这一页回来之间**没有任何东西会叫醒界面**（唤醒只有「结果
//     到了」那一次），所以那句「这里还没有歌」会一直摆到第一页回来。
//
// ⚠️ `ui.NewTester` 自己就跑过一帧（构造函数里的 settle），所以**这里不能再
// `tt.Frame()`**：要看的正是那第一帧。
func TestTheVeryFirstFrameAlreadySaysLoading(t *testing.T) {
	st := newStub().with(1, 200, false).gate(1)
	p := NewPaged(newCache(t), Actions{Notify: func(string) {}}, st.fetch)
	tt := ui.NewTester(p.View, testerW, testerH)

	// loading 是 load 里同步置的（拉页本身在后台 goroutine 里），所以这一句
	// 不用等：第一帧画的时候它已经是 true 了。
	if !p.Paged().Loading {
		t.Errorf("第一帧画的时候还没在拉页：第一页是画完之后才发出去的吧")
	}
	if tt.HasText("这里还没有歌") {
		t.Errorf("第一帧说的是「这里还没有歌」，文字有 %q", tt.Texts())
	}
	if !tt.HasText("正在加载…") {
		t.Errorf("第一帧没有说「正在加载…」，文字有 %q", tt.Texts())
	}
	pump(t, tt, "第一页真的发出去了", func() bool { return len(st.callsOf()) == 1 })
	if c := st.callsOf(); c[0] != 1 {
		t.Errorf("叫了 %v 页，want [1]", c)
	}

	// 拉回来之后，那句话要换掉。
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
	// 🔴 **等第 2 页那一趟真的记到账上**（评审在满载的整包跑里撞到过这一条，见下面）。
	//
	// `Loading` 是**主线程**上那一格，而把调用记进 `calls` 的是拉页那个**后台
	// goroutine**——两者之间没有先后保证。第 1 页落地的那一帧，③「容器没填满」
	// 就已经把第 2 页叫起来了（`afterFrame` 排在 `List.View` 之后，读的是**上一帧**
	// 量到的可见行），所以滚到底那一下常常什么也没做，而 `Loading` 从那时起就是
	// true。于是「跑十几帧之后数一数」数到的是**调度快慢**：机器一忙，那十几帧
	// （纯 CPU，几毫秒）跑完了而那个 goroutine 一次都没被调度，账上就还是只有第 1 页
	// ——用例于是报「第 2 页在途时又发了 0 个请求」，而它其实一次都没验到。
	pumpCalls(t, st, 2)

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

// pumpCalls 等到**后台那个拉页的 goroutine**把第 n 次调用记到账上。
//
// 与 `pump` 的区别是它等的是后台那一支：`pump` 走一帧就问一次，而这里数的是
// stub 里的账——那几行是拉页的 goroutine 写的，它什么时候跑起来和主线程画第几帧
// 没有关系。**在满载的整包跑里量过**：不等它的话，那条用例会在「账上只有第 1 页」
// 的时刻就去数（十几帧纯 CPU 只要几毫秒，而 goroutine 可能一次都没被调度）。
func pumpCalls(t *testing.T, st *stubFetcher, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if len(st.callsOf()) >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等了两秒，后台只记了 %d 次调用（%v），want %d 次",
				len(st.callsOf()), st.callsOf(), n)
		}
		time.Sleep(time.Millisecond)
	}
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

// TestAFailureOnTheVeryFirstPageLeavesOneWayOut 是「一行都还没有的时候失败」
// （开 app 时连不上网就是这一种）。
//
// 这时候屏幕上只该留那一个重试按钮：既不能说「这里还没有歌」（还不知道有没有
// 歌），也不能一直转圈。
func TestAFailureOnTheVeryFirstPageLeavesOneWayOut(t *testing.T) {
	st := newStub().fails(1, errors.New("连不上"))
	p, tt := newPaged(t, st.fetch)

	pump(t, tt, "第一页失败", func() bool { return p.Paged().Err != nil })

	if p.Paged().Loading {
		t.Fatalf("第一页失败之后还在 Loading，界面会永远转圈")
	}
	if tt.HasText("这里还没有歌") {
		t.Errorf("第一页就失败却说「这里还没有歌」，文字有 %q", tt.Texts())
	}
	if tt.HasText("正在加载…") {
		t.Errorf("第一页就失败却还在说「正在加载…」，文字有 %q", tt.Texts())
	}
	if !tt.HasText("加载失败，点此重试") {
		t.Errorf("第一页就失败，屏幕上没有一条走得出去的路，文字有 %q", tt.Texts())
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

// TestAFetcherThatPanicDoesNotTakeTheProcessDown 是「Fetcher 在后台 goroutine
// 里 panic 会直接带走整个进程」那条。
//
// 这一支是**本票里唯一一处没有 recover 的 panic**：fetch 跑在后台 goroutine
// 里，行级 / 页级 / view 自己那三层 recover 一个都接不到它（没有人在跑那个
// goroutine）。而 Fetcher 正是「解析接口返回」的那一段——接口一变就在这里炸。
//
// 这条测试要是没有那层 recover，跑它的时候整个测试进程当场就没了。
func TestAFetcherThatPanicDoesNotTakeTheProcessDown(t *testing.T) {
	fetch := func(ctx context.Context, page int) (Page, error) {
		if page == 2 {
			panic("接口返回了预期外的东西")
		}
		return Page{Songs: pageSongs(page, 200), HasMore: page == 1}, nil
	}
	p, tt := newPaged(t, fetch)

	pump(t, tt, "第一页落地", func() bool { return p.Paged().Loaded == 200 })
	tt.Scroll(testerW/2, testerH/2, 0, scrollToBottom)
	pump(t, tt, "第 2 页崩了", func() bool { return p.Paged().Err != nil })

	// 崩了也要走 apply 那条正常路：解锁、记一笔、页脚上出现重试。
	if p.Paged().Loading {
		t.Fatalf("拉页器崩了之后还在 Loading，界面会永远转圈")
	}
	if !errors.Is(p.Paged().Err, errFetcherPanic) {
		t.Errorf("Err = %v，want 含有 %v", p.Paged().Err, errFetcherPanic)
	}
	if !tt.HasText("加载失败，点此重试") {
		t.Errorf("拉页器崩了之后没有给人一条重试的路，文字有 %q", tt.Texts())
	}
}

// TestAPanicWhileDrawingDoesNotTakeTheProcessDown 验的是 view 外面那道
// guard.Wrap：控件自己画崩了，不该把进程带走，而且下一帧还能画出来。
func TestAPanicWhileDrawingDoesNotTakeTheProcessDown(t *testing.T) {
	st := newStub().fails(1, errors.New("连不上"))
	notified := 0
	acts := Actions{Notify: func(string) {
		notified++
		if notified == 1 {
			panic("提示画崩了") // 只有第一次崩：要看得见它之后活下来了
		}
	}}
	p := NewPaged(newCache(t), acts, st.fetch)
	tt := ui.NewTester(p.View, testerW, testerH)

	// apply 里 notify 炸的那一帧：没有这道 recover，测试进程当场就没了。
	pump(t, tt, "第一页失败（那一帧里 notify 崩了一次）", func() bool { return notified > 0 })
	frame(tt)

	if p.Paged().Loading {
		t.Errorf("画崩之后还在 Loading，界面会永远转圈")
	}
	if !tt.HasText("加载失败，点此重试") {
		t.Errorf("画崩之后下一帧没画出来，文字有 %q", tt.Texts())
	}
	if tt.HasText("这里还没有歌") {
		t.Errorf("画崩之后说「这里还没有歌」，文字有 %q", tt.Texts())
	}
}

// TestPagesThatOverlapAreDeduped 是铁律四在翻页上最容易踩的那一处：
// **两页返回同一首歌**（B 站的翻页本来就可能重叠，同一首歌挪个名次就出现两次）。
//
// 不去重的话同一个 ID 出现两次：Tester 当场 panic（这条测试就是靠那个 panic
// 兜住的），真窗口里那两行共用一份状态——选中一个另一个也亮。
func TestPagesThatOverlapAreDeduped(t *testing.T) {
	first := pageSongs(1, 200)
	st := newStub().withSongs(1, first, true).withSongs(2, first, false)
	p, tt := newPaged(t, st.fetch)

	pump(t, tt, "第一页落地", func() bool { return p.Paged().Loaded == 200 })
	tt.Scroll(testerW/2, testerH/2, 0, scrollToBottom)
	pump(t, tt, "第 2 页落地", func() bool { return !p.Paged().Loading && p.Paged().Page == 2 })
	frame(tt)

	if got := p.Paged().Loaded; got != 200 {
		t.Errorf("两页返回同一批歌，加载了 %d 行，want 200", got)
	}
	// 画面上还是那一批歌（滚到底了，所以看的是末尾那一行）。
	if !onScreen(tt, "第 1 页 歌 200") {
		t.Errorf("翻完页末尾那一行不在画面上，文字有 %q", tt.Texts())
	}
}

// TestEveryWindowThatDrewItGetsWoken 是铁律三的后半句：**逐窗口通知**。
//
// 一个列表可能被两个窗口画（迷你播放器那条路由将来就是第二个窗口）。只叫醒
// 「最后画过的那个」，另一个窗口就停在旧帧——实测那样是 0 帧刷新。
//
// ⚠️ 这里只验通知这一段：同一个窗口登记两次只占一条（认的是 Services），
// 有两条就都要叫醒。真有两个窗口的 Tester 是另一票的事（那时候才有）。
func TestEveryWindowThatDrewItGetsWoken(t *testing.T) {
	st := newStub().with(1, 200, false)
	p, tt := newPaged(t, st.fetch)

	p.wakeMu.Lock()
	one := len(p.wakes)
	p.wakeMu.Unlock()
	if one != 1 {
		t.Fatalf("一个窗口画了一帧之后记了 %d 条唤醒句柄，want 1", one)
	}

	frame(tt) // 同一个窗口再画一帧：还是一条
	p.wakeMu.Lock()
	again := len(p.wakes)
	p.wakeMu.Unlock()
	if again != 1 {
		t.Fatalf("同一个窗口画了第二帧之后记了 %d 条，want 1（覆盖式登记）", again)
	}

	// 手工加一条「另一个窗口」，两个都得叫醒。
	var woke [2]int
	p.wakeMu.Lock()
	p.wakes[0].wake = func() { woke[0]++ }
	p.wakes = append(p.wakes, wakeEntry{wake: func() { woke[1]++ }})
	p.wakeMu.Unlock()

	p.invalidate()
	if woke[0] != 1 || woke[1] != 1 {
		t.Errorf("叫醒了 %v，want [1 1]：逐窗口通知，两个窗口都得醒", woke)
	}
}

// TestTheLogSaysWhoAskedForThePage 是排查用的小钉子：用户点的重试不能被记成
// 「容器没填满叫的」——那句话会把人往错的方向引。
func TestTheLogSaysWhoAskedForThePage(t *testing.T) {
	want := map[trigger]string{
		trigFirstRender: "首次渲染检查叫的",
		trigScrollEnd:   "滚到底叫的",
		trigRefill:      "容器没填满叫的",
		trigRetry:       "用户点了重试",
	}
	for got, text := range want {
		if got.String() != text {
			t.Errorf("trigger %d 说的是 %q，want %q", got, got.String(), text)
		}
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
	if m, ok := p.Selected(); !ok || m.ID != "p1-4" {
		t.Fatalf("选中的是 %v，want p1-4", m.ID)
	}

	// 滚到底拉第二页：200 行追加在**尾部**。
	tt.Scroll(testerW/2, testerH/2, 0, scrollToBottom)
	pump(t, tt, "第二页落地", func() bool { return p.Paged().Loaded == 400 && !p.Paged().Loading })

	m, ok := p.Selected()
	if !ok {
		t.Fatalf("翻完页选中没了")
	}
	if m.ID != "p1-4" {
		t.Errorf("翻完页选中的是 %q，want p1-4（追加不该动到上面那些行的身份）", m.ID)
	}
}
