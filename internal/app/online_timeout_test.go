package app_test

import (
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// 这一组是票 #282 的回归：「点了歌一直不开始、也不说为什么」。
//
// # 复现的是什么
//
// 用户实测（0.1.0 安装包）：首页「音乐」里点第一首，**加载动画转了好几秒，
// 始终没出声，也没有任何报错提示**；同一列表第二首能立刻播。
//
// 那一首和别的歌不一样的地方在于**它的流一直没准备好**——而应用这一层
// 对「还在准备」和「已经完了」**分都不分**：既没有一句「正在取流」，也没有
// 一个「等够了就当它失败」的上限。于是用户面对的是一个不动的界面和一片
// 沉默。
//
// # 这几条钉的是什么
//
//	「还在取流」这一档要出现在状态里（界面据此说一句话、画一枚转圈）
//	超过阈值就按失败处理（界面换成一句能看懂的话，而不是一直转圈）
//	代次变了要留一句日志（以前是**一声不吭地丢弃**）
//	失败要写进日志（歌名 / 走的哪一段 / 错在哪 / 代次）

// fakeClock 是「排定时器」的假实现：定时器由测试自己点火。
//
// ⚠️ **它和 `a.after` 的签名一样**（`func(time.Duration, func()) func()`），
// 所以装上去之后生产那一套排定时器的代码一行都不用改。
type fakeClock struct {
	mu     sync.Mutex
	timers []*fakeTimer
}

type fakeTimer struct {
	d    time.Duration
	fn   func()
	dead bool
}

// after 排一个定时器，返回停掉它的函数。
func (c *fakeClock) after(d time.Duration, fn func()) func() {
	c.mu.Lock()
	defer c.mu.Unlock()
	tm := &fakeTimer{d: d, fn: fn}
	c.timers = append(c.timers, tm)
	return func() {
		c.mu.Lock()
		tm.dead = true
		c.mu.Unlock()
	}
}

// fire 点燃所有还活着的定时器。
func (c *fakeClock) fire() {
	c.mu.Lock()
	timers := append([]*fakeTimer(nil), c.timers...)
	c.mu.Unlock()
	for _, tm := range timers {
		c.mu.Lock()
		dead := tm.dead
		c.mu.Unlock()
		if dead {
			continue
		}
		tm.fn()
	}
}

// hangingCDN 是一台「接了连接但一个字节都不给」的 CDN。
//
// 它等的是**请求上下文被取消**——客户端（字节源）掐断请求的那一刻。
// 不这么写的话它会一直挂着，测试收尾时 `httptest.Server.Close` 会一直等它。
//
// arrived 是「请求真的到了」的信号：测试要等它，才能确定「取流已经到 CDN
// 那一层了」，而不是还在 B 站接口那一头。
func hangingCDN(arrived chan struct{}) http.HandlerFunc {
	var once sync.Once
	return func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(arrived) })
		<-r.Context().Done()
	}
}

// TestAPlayThatNeverStartsTurnsIntoASentence 盯「点了歌一直不开始」不再是一句都不说。
//
// 这一条钉的是**旧行为和新行为的分界**：改之前，这一首会一直「什么都不显示」
// ——没有转圈、没有话、没有日志；改之后，界面上先说「正在取流」，超过阈值
// 就换成一句失败的话。
func TestAPlayThatNeverStartsTurnsIntoASentence(t *testing.T) {
	f := newStreamFixture(t)
	arrived := make(chan struct{})
	f.on("/cdn/hang.m4s", hangingCDN(arrived))
	f.onPlayURL(dashFor(cdnURL(f.URL, "hang.m4s", 2*time.Hour),
		int(bili.QualityHigh192), 110836))

	a := newOnlineApp(t, f, t.TempDir())
	// 假钟：超时那一段由测试自己点火，不用真的等 15 秒。
	clock := &fakeClock{}
	a.SetAfterForTest(clock.after)

	if err := a.Enqueue(onlineItem("BV1", "1", "取不来的那一首")); err != nil {
		t.Fatal(err)
	}

	// 一、**取流真的到了 CDN**：那之后这一首就「卡在那儿」了——和用户实测
	// 的那一句「加载动画转了好几秒」是同一个形态。
	select {
	case <-arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("取流一直没到 CDN：这一条测的不是「流卡住了」")
	}

	// 二、**在等的这一段**：状态里说「正在取流」，而且**还没有任何错**
	// （没到阈值就报错是另一回事：那样用户连等的机会都没有）。
	waitFor(t, 15*time.Second, "状态里说在取流", func() bool {
		return a.Snapshot().Loading
	})
	if st := a.Snapshot(); st.Err != "" {
		t.Fatalf("还没到阈值就有错了：%q", st.Err)
	}
	if st := a.Snapshot(); st.LoadingTitle != "取不来的那一首" {
		t.Errorf("不知道在取哪一首：%q", st.LoadingTitle)
	}

	// 三、**超过阈值**：按失败处理——一句话，转圈收掉。
	clock.fire()
	waitFor(t, 15*time.Second, "超时之后有一句话", func() bool {
		return a.Snapshot().Err != ""
	})
	st := a.Snapshot()
	if st.Loading {
		t.Error("超时之后还在转圈——那还是「一直转圈、什么都不说」")
	}
	if !strings.Contains(st.Err, "超时") {
		t.Errorf("话里没有说清是超时：%q", st.Err)
	}
	if !st.CanRetry {
		t.Error("超时是可以再试一次的（网络慢），界面上却没有「重试」那一枚键")
	}
}

// TestAnotherSongsFailureDoesNotCancelTheWatchdog 盯「别的歌的失败不许把这一首的看门狗取消掉」。
//
// 🔴 **这是票 #282 评审写的那条探针**：歌 A 在放 → 点歌 B（CDN 挂住，取流卡
// 在代次 2）→ 这期间换音质、而换音质失败（B 站回 -404）→ `failOnline` 拿
// 代次 2 判「不是陈旧的」。
//
// 改之前 `failOnline` 会顺手把「正在取流」和它的闹钟一起收掉——于是 B
// **既不转圈也不报错，闹钟还被人替它取消了**：用户点了歌，什么都没发生，
// 也永远不会有人再问（票面「不许无限转圈」的那条保证在这条路上失效）。
func TestAnotherSongsFailureDoesNotCancelTheWatchdog(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	// CDN：第一次请求给歌 A，后面的都挂住（歌 B 卡在「头几个字节还没到」）。
	// `arrived` 是「歌 B 的请求真的到了 CDN」的信号——不等它就换音质的话，
	// 换音质那一次失败会和歌 B 自己那一次取流失败混在一起。
	var cdnCalls int32
	var arrivedOnce sync.Once
	arrived := make(chan struct{})
	f.on("/cdn/x.m4s", func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&cdnCalls, 1) == 1 {
			f.serveMedia(w, r)
			return
		}
		arrivedOnce.Do(func() { close(arrived) })
		<-r.Context().Done()
	})
	// 取流接口：默认给一条流；`failNext` 立起来之后的那一次回 -404
	// （换音质那一次）。用开关而不是「数第几次」——后台的换流（refetch）也会
	// 打这个接口，数次数会把它的往返算进来。
	var failNext atomic.Bool
	f.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		if failNext.Load() {
			writeStreamJSON(w, map[string]any{"code": -404, "message": "稿件不可见", "ttl": 1})
			return
		}
		writeStreamJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{"timelength": 1, "dash": dashFor(
				cdnURL(f.URL, "x.m4s", 2*time.Hour), int(bili.QualityHigh192), 110836)}})
	})

	a := newOnlineApp(t, f, t.TempDir())
	clock := &fakeClock{}
	a.SetAfterForTest(clock.after)

	// 一、歌 A 在放。
	if err := a.Enqueue(onlineItem("BV1", "1", "在放的那一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "在放的那一首")

	// 二、点歌 B：CDN 挂住，B 卡在取流。
	if err := a.PlayFrom(onlineItem("BV2", "1", "取不来的那一首")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 15*time.Second, "第二首开始取流", func() bool { return a.Snapshot().Loading })
	// 等它真的卡在 CDN 那一层（那时候它正等「头几个字节」）。
	select {
	case <-arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("歌 B 的取流一直没到 CDN：这一条测的不是「在途的那一趟」")
	}

	// 三、这期间换音质，而换音质失败。
	failNext.Store(true)
	a.SetAudioQuality(bili.AudioHigh)
	waitFor(t, 15*time.Second, "换音质的失败报出来了", func() bool {
		return a.Snapshot().Err != ""
	})

	// 🔴 **关键**：歌 B 的「正在取流」和它的闹钟都得还在。
	if st := a.Snapshot(); !st.Loading {
		t.Fatalf("别的歌的失败把这一首的「正在取流」收掉了（%q）——"+
			"用户点的那一首从此既不转圈也不报错", st.Err)
	}
	// 点火闹钟：歌 B 的超时必须响。
	clock.fire()
	waitFor(t, 15*time.Second, "歌 B 超时了", func() bool {
		return strings.Contains(a.Snapshot().Err, "超时")
	})
	if st := a.Snapshot(); st.Loading {
		t.Error("超时之后还在转圈")
	}
}

// TestTheDiscardedPlayIsWrittenDown 盯「代次变了」不再是无声丢弃。
//
// 那一头（`runOnline` 里装播放器之前）以前是 `p.Close(); return`——不报错、
// 不提示、**不写日志**。于是「点了歌，什么都没发生」这件事在事后完全查不了：
// 用户能说的只有「它不响」，而应用这边一个字都没留下。
//
// ⚠️ **代次变了本身不是错**：用户连点两首时，前一首的结果就是要丢掉的。
// 丢得对，但**要留下痕迹**。
func TestTheDiscardedPlayIsWrittenDown(t *testing.T) {
	f := newStreamFixture(t)
	// 一台慢吞吞的 CDN：先把头几个字节拖着，等我们把代次换掉再给。
	release := make(chan struct{})
	arrived := make(chan struct{})
	var arrivedOnce, releaseOnce sync.Once
	f.on("/cdn/slow.m4s", func(w http.ResponseWriter, r *http.Request) {
		arrivedOnce.Do(func() { close(arrived) })
		<-release
		f.serveMedia(w, r)
	})
	f.media = readFlac(t)
	f.onPlayURL(dashFor(cdnURL(f.URL, "slow.m4s", 2*time.Hour),
		int(bili.QualityHigh192), 110836))

	a := newOnlineApp(t, f, t.TempDir())
	lines, restore := app.CapturePlayLog(a)
	t.Cleanup(restore)

	if err := a.Enqueue(onlineItem("BV1", "1", "被作废的那一首")); err != nil {
		t.Fatal(err)
	}
	// 等它真的走到 CDN 那一层（那时候它正卡在「头几个字节还没到」）。
	select {
	case <-arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("取流一直没到 CDN：这一条测的不是「在途的那一趟」")
	}

	// 换歌 = 代次变了（`PlayFrom` 是界面上「在列表里点了一首」走的那条路）。
	if err := a.PlayFrom(onlineItem("BV2", "1", "换上来的那一首")); err != nil {
		t.Fatal(err)
	}
	// 把流放出来：它回来时对不上代次，会被丢弃。
	releaseOnce.Do(func() { close(release) })

	waitFor(t, 30*time.Second, "日志里说清在途的播放被作废了", func() bool {
		return strings.Contains(strings.Join(*lines, "\n"), "作废")
	})
	joined := strings.Join(*lines, "\n")
	if !strings.Contains(joined, "代次") {
		t.Errorf("日志里没说是「代次变了」：\n%s", joined)
	}
	if !strings.Contains(joined, "被作废的那一首") {
		t.Errorf("日志里没写是哪一首被作废了：\n%s", joined)
	}
}

// TestTheFailureIsWrittenDown 盯「播放失败」写进日志。
//
// 票 #282 的另一半：应用日志以前**一个字都不写播放路径**，所以「点了不播」
// 这类问题事后完全查不了。这一条钉住：失败的时候日志里有歌名、走的哪一段、
// 错在哪、代次。
func TestTheFailureIsWrittenDown(t *testing.T) {
	f := newStreamFixture(t)
	f.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		writeStreamJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{"timelength": 1, "dash": map[string]any{"duration": 1}}})
	})
	a := newOnlineApp(t, f, t.TempDir())
	lines, restore := app.CapturePlayLog(a)
	t.Cleanup(restore)

	if err := a.Enqueue(onlineItem("BV1", "1", "放不出来的那一首")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 15*time.Second, "播放条上出现一句人话", func() bool {
		return a.Snapshot().Err != ""
	})
	waitFor(t, 15*time.Second, "日志里写清这一次失败", func() bool {
		return strings.Contains(strings.Join(*lines, "\n"), "播放失败")
	})
	joined := strings.Join(*lines, "\n")
	for _, want := range []string{"放不出来的那一首", "取流", "代次"} {
		if !strings.Contains(joined, want) {
			t.Errorf("日志里没有 %q：\n%s", want, joined)
		}
	}
}
