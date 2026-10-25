package app

// 联想词这一摊的业务层测试。
//
// 这一组钉的是**协议层管不着的那一半**：什么时候问、问了之后状态长什么样、
// 失败之后会不会自己绕圈、迟到的结果会不会盖掉新的。
//
// ⚠️ 这里所有请求都打在**本地 fixture** 上，一次都不碰真的 B 站。真接口那一趟
// （`s.search.bilibili.com/main/suggest` 的地址、参数、回包形状）要真机回归，见
// 任务报告「需要真网验」那一节。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// suggestPathWanted 是这个接口该走的路。
//
// ⚠️ **字面量**，不从 `bili` 包里拿常量——理由和 `openPathWanted` 一样：这里量的是
// 「打的是不是 `/main/suggest`」这件事本身，跟着常量走的话那边写错了这边也跟着错。
const suggestPathWanted = "/main/suggest"

// suggestBili 是一台只答联想词那个接口的假 B 站。
type suggestBili struct {
	*httptest.Server

	mu sync.Mutex
	// asked 记被问过的词，按到达顺序。
	asked []string
	// served 数**已经回了**几次（不是「发了几次」）。两者之差就是还在路上的那些：
	// 判「那一次问完了」要的是它，只等「发了」的话验的还是请求还没回的中间态。
	served int
	// terms 是每个词该回的那批。
	terms map[string][]string
	// gate 非 nil 时响应卡在它上面，放开才回（造「旧的那一趟迟到」）。
	gate map[string]chan struct{}
	// failWith 非 0 时一律回这个业务 code。
	failWith int
	// broken 非 0 时回这个 code，而 data 给一个读不懂的形状。
	broken int
}

func newSuggestBili(t *testing.T) *suggestBili {
	t.Helper()
	b := &suggestBili{terms: map[string][]string{}, gate: map[string]chan struct{}{}}
	b.Server = httptest.NewServer(http.HandlerFunc(b.serve))
	// ⚠️ 收尾必须把所有卡住的响应都放行：`httptest.Server.Close` 会等在途请求回来，
	// 而一个永远等着的 handler 会把整个包卡到十分钟上限。
	t.Cleanup(func() {
		b.releaseAll()
		b.Close()
	})
	return b
}

func (b *suggestBili) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != suggestPathWanted {
		http.NotFound(w, r)
		return
	}
	kw := r.URL.Query().Get("term")

	b.mu.Lock()
	b.asked = append(b.asked, kw)
	b.served++
	ch := b.gate[kw]
	terms, code, broken := b.terms[kw], b.failWith, b.broken
	b.mu.Unlock()

	if ch != nil {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			// 兜底：用例提前失败、没人放行时这一趟也得回去。
		}
	}

	var data any
	switch {
	case code != 0:
		data = nil
	case broken != 0:
		data = "这不是那个形状"
	default:
		tag := make([]any, 0, len(terms))
		for _, term := range terms {
			// B 站会把命中的部分包进 name，而 term 是干净的（见 suggest.go）。
			tag = append(tag, map[string]any{"term": term, "name": term, "type": "huashu"})
		}
		data = map[string]any{"tag": tag}
	}
	body, _ := json.Marshal(map[string]any{"code": code, "message": "0", "ttl": 1, "data": data})
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

func (b *suggestBili) calls() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.asked...)
}

func (b *suggestBili) hold(kw string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.gate[kw] = make(chan struct{})
}

func (b *suggestBili) release(kw string) {
	b.mu.Lock()
	ch := b.gate[kw]
	delete(b.gate, kw)
	b.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}

func (b *suggestBili) releaseAll() {
	b.mu.Lock()
	var chans []chan struct{}
	for kw, ch := range b.gate {
		chans = append(chans, ch)
		delete(b.gate, kw)
	}
	b.mu.Unlock()
	for _, ch := range chans {
		close(ch)
	}
}

// newSuggestRig 装一个指向假 B 站的 App。
func newSuggestRig(t *testing.T) (*App, *suggestBili) {
	t.Helper()
	b := newSuggestBili(t)
	a := New()
	t.Cleanup(a.Close)

	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(b.URL)
	a.MountLogin(NewMemoryStore("suggest-test"), cli)
	return a, b
}

// waitSuggest 等联想词那一档变成 want 里的那批。
func waitSuggest(t *testing.T, a *App, want ...string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := a.SuggestState().Items
		if sameWords(got, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等到了 5 秒，联想词还是 %q，want %q", got, want)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func sameWords(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// waitServed 等假 B 站**回了** n 次（请求真的走完了一趟）。
func waitServed(t *testing.T, b *suggestBili, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		b.mu.Lock()
		got := b.served
		b.mu.Unlock()
		if got >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等了 5 秒，回了 %d 次，want %d", got, n)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// idle 报「业务层此刻不在飞」（那一趟已经回来并装上状态了）。
func idle(a *App) bool { return !a.SuggestState().Loading }

// waitCalls 等假 B 站被问过 n 次。
func waitCalls(t *testing.T, b *suggestBili, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if len(b.calls()) == n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等了 5 秒，问了 %d 次，want %d（%v）", len(b.calls()), n, b.calls())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// waitSuggestFailed 等那一趟的失败落地。
//
// ⚠️ **不能只等「问过了」**：`waitCalls` 等的是请求被记下来的那一刻，而失败是
// 背景那一趟回来之后才记上的——早一步读到的是「还没失败」。
func waitSuggestFailed(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if a.SuggestState().Failed {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("等了 5 秒，那一趟还是没记下失败")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestAskingGivesBackWhatBilibiliSaid 钉住最基本的那一条：问了就到。
func TestAskingForSuggestGivesBackWhatBilibiliSaid(t *testing.T) {
	a, b := newSuggestRig(t)
	b.terms["周杰"] = []string{"周杰伦", "周杰伦 晴天"}

	a.SearchSuggest("周杰")
	waitSuggest(t, a, "周杰伦", "周杰伦 晴天")

	if got := a.SuggestState().Query; got != "周杰" {
		t.Errorf("生效的词是 %q，want 周杰", got)
	}
	if got := b.calls(); len(got) != 1 || got[0] != "周杰" {
		t.Errorf("问的是 %v，want [周杰]", got)
	}
}

// TestTheSameWordIsNotAskedForSuggestTwice 钉住「同一个词不许问第二遍」。
//
// 防抖那一层也有这道闸门，而这里是兜底：它挡住的是「界面上那一支的判据写错」
// 那种一帧一发的自激循环（下面那条钉的就是它的后果）。
//
// ⚠️ **判据排在「第一趟真的回来」之后**（评审钉过这一条）：早先这条是
// `waitCalls(1)` 一数到 1 就返回，可多出来的那一趟是从 goroutine 里发出去的，那
// 时候还没打到 fixture 上——于是用例有时红、有时绿（12 次连跑全绿，而请求数确实从 1
// 变成了 2）。所以先等**回包**（`waitServed` + 业务层不在飞），再敲第二遍，然后给
// 那趟在途的请求留一段安静时间，最后才数。
func TestTheSameWordIsNotAskedForSuggestTwice(t *testing.T) {
	a, b := newSuggestRig(t)
	b.terms["周杰伦"] = []string{"周杰伦 晴天"}

	a.SearchSuggest("周杰伦")
	waitServed(t, b, 1)
	waitUntil(t, "第一趟问完", 5*time.Second, func() bool { return idle(a) })
	if got := b.calls(); len(got) != 1 {
		t.Fatalf("第一趟还没问完就数了一次：%v", got)
	}

	// 用户在慢网络下「再按一下试试」：同一个词，同一个源。
	for range 5 {
		a.SearchSuggest("周杰伦")
	}
	// 留一段安静时间：那几趟（如果发了）现在一定已经打到 fixture 上了。
	time.Sleep(300 * time.Millisecond)

	if got := b.calls(); len(got) != 1 {
		t.Errorf("同一个词问了 %d 次（%v），want 1——每次都去问一趟就是白花往返，"+
			"而 B 站那边会开始限速", len(got), got)
	}
	// 而那一档的结果还在（早先那条的另一个坑：这一支顺手把代次作废掉的话，
	// `loading` 再没有人翻回来）。
	if got := a.SuggestState().Items; len(got) != 1 || got[0] != "周杰伦 晴天" {
		t.Errorf("重复提交之后结果成了 %q，want [周杰伦 晴天]", got)
	}
}

// TestAFailedSuggestSaysNoErrorAndDoesNotFireAgain 钉住规格里「静默」那一条。
//
// 两个后果一起钉：
//
//   - 界面上没有任何一句错误——这一层**不产出错误文本**，浮层上不会弹一条错；
//   - 之后不许每帧重发。少了后半句就是自己养自己的循环（生产里每次投递都叫醒
//     界面），于是浮层底下一直刷网络，而用户只看到搜索框卡一下。
//
// ⚠️ **「静默」改过一次口径**（票 #455 / 报告 246 第 12 条）：静默说的是「不弹
// 错误、不转圈、不变红」，而**不是「一个字都不许说」**——所以这一层现在有一个
// `Failed` 位（界面靠它说「没能取到联想词」）。不记这一位的话，「B 站没给联想」
// 和「这一趟没拉到」在画面上长得一模一样。
func TestAFailedSuggestSaysNoErrorAndDoesNotFireAgain(t *testing.T) {
	a, b := newSuggestRig(t)
	b.failWith = -412 // 被风控挡了

	a.SearchSuggest("周杰伦")
	// 失败之后「一个都没有」这件事会立刻成立（items 在发出去时就清空了），
	// 所以等的是「问过了」而不是「拿到了」。
	waitCalls(t, b, 1)
	if got := a.SuggestState().Items; got != nil {
		t.Fatalf("拉不到却摆出了 %q", got)
	}
	// 而那个词仍然记着——界面上「已经问过了」那一支靠它挡住每帧重发。
	if got := a.SuggestState().Query; got != "周杰伦" {
		t.Errorf("生效的词是 %q，want 周杰伦（失败也要记，界面靠它挡住重发）", got)
	}
	// ⚠️ **失败那一位要记上**：界面靠它把两种「没有」分开（票 #455）。
	waitSuggestFailed(t, a)
	if !a.SuggestState().Failed {
		t.Errorf("拉不到却没记下「这一趟失败了」，界面于是分不出「没给联想」和「没拉到」")
	}

	for range 10 {
		a.SearchSuggest("周杰伦")
	}
	time.Sleep(100 * time.Millisecond)
	if got := len(b.calls()); got != 1 {
		t.Errorf("失败之后又问了 %d 次（合计），want 1——那是每帧一发", got)
	}
}

// TestAStaleSuggestNeverLands 钉住代次守卫。
//
// 用户在搜索站那一趟还在路上的时候又敲了几个字：旧的那批回来时不该盖掉新的。
// 少了这一道，界面上是「我打的是『周杰』、浮层上摆的是『周杰伦』的那批」，
// 而点哪一条都是按它自己那行走的。
func TestAStaleSuggestNeverLands(t *testing.T) {
	a, b := newSuggestRig(t)
	b.terms["周杰"] = []string{"周杰伦"}
	b.terms["周杰伦"] = []string{"周杰伦 晴天"}
	b.hold("周杰")

	a.SearchSuggest("周杰")
	waitCalls(t, b, 1)
	a.SearchSuggest("周杰伦")
	waitSuggest(t, a, "周杰伦 晴天")

	// 旧的那一趟现在才回来。
	b.release("周杰")
	waitCalls(t, b, 2)
	time.Sleep(100 * time.Millisecond)

	if got := a.SuggestState().Items; !sameWords(got, []string{"周杰伦 晴天"}) {
		t.Errorf("旧的那一趟盖掉了新的：%q", got)
	}
	if got := a.SuggestState().Query; got != "周杰伦" {
		t.Errorf("生效的词退回了 %q，want 周杰伦", got)
	}
}

// TestEmptyingTheBoxTakesTheListAway 钉住「框里空了，上一批收掉」。
//
// 留着的话用户看着自己刚删干净的那一框底下还挂着一批和它无关的词——而那些词
// 都是**上一个**问题的答案。
func TestEmptyingTheBoxTakesTheSuggestListAway(t *testing.T) {
	a, b := newSuggestRig(t)
	b.terms["周杰"] = []string{"周杰伦"}
	a.SearchSuggest("周杰")
	waitSuggest(t, a, "周杰伦")

	a.SearchSuggest("   ")
	if got := a.SuggestState().Items; got != nil {
		t.Errorf("框里空了还摆着 %q", got)
	}
	if got := a.SuggestState().Query; got != "" {
		t.Errorf("生效的词是 %q，want 空", got)
	}
	// 空词一个请求都不发。
	if got := len(b.calls()); got != 1 {
		t.Errorf("一共问了 %d 次，want 1（空词那一次不该发）", got)
	}
}

// TestClosingTheAppStopsTheSuggestFromComingBack 钉住「退出之后不许再写」。
//
// 后台那一趟还在网络上时关掉应用：它回来时发现应用没了，就该走人。少这一道的话
// 它会往一个已经关掉的状态上写（界面上那个唤醒句柄也已经失效了，写了也没人看见，
// 但那是一次往已收尾的东西上写）。
func TestClosingTheAppStopsTheSuggestFromComingBack(t *testing.T) {
	a, b := newSuggestRig(t)
	b.terms["周杰伦"] = []string{"周杰伦 晴天"}
	b.hold("周杰伦")

	a.SearchSuggest("周杰伦")
	waitCalls(t, b, 1)

	a.Close()
	b.release("周杰伦")
	time.Sleep(100 * time.Millisecond)

	if got := a.SuggestState().Items; got != nil {
		t.Errorf("应用已经关掉，迟到的结果还落了地：%q", got)
	}
}

// TestAskingWithoutABiliClientIsQuiet 钉住「没挂上 B 站客户端也不出声」。
//
// 生产上 `MountLogin` 一定在开窗口之前调，所以这是测试里才有的那种壳。它不许
// panic，也不许让界面每帧重问一遍。
func TestAskingForSuggestWithoutABiliClientIsQuiet(t *testing.T) {
	a := New()
	t.Cleanup(a.Close)

	for range 5 {
		a.SearchSuggest("周杰伦")
	}
	time.Sleep(50 * time.Millisecond)

	st := a.SuggestState()
	if st.Items != nil {
		t.Errorf("没有客户端却摆出了 %q", st.Items)
	}
	if st.Query != "周杰伦" || st.Loading {
		t.Errorf("状态是 %+v：应当是「问过了、没有结果、不在飞」", st)
	}
}
