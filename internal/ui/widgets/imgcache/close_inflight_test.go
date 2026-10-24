package imgcache

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 「封面还没下完就关窗口」这一条。
//
// # 为什么它单独一组
//
// 封面下载是**唯一一条**会在窗口已经不在了之后才回来的异步：请求在窗口
// 还开着的时候发出去，图是几百毫秒之后才到的，而用户在这中间按了关闭。
//
// 这条路上有两个静默的坑：
//
//	Close 挂着不动   Close 会等下载泵退出，而下载泵阻塞在一个还在等的
//	                 HTTP 请求上——关不掉应用，用户看到的是「退不出」
//	late result 叫醒 窗口都关了还去 Invalidate 一个死窗口。实测不报错、
//	                 不崩溃，只是白做一次功；真正难查的是它把别人的窗口叫醒，
//	                 于是迷你窗口在主窗口关掉之后莫名其妙重画
//
// # 这组用的是「卡住的真服务端」
//
// 不是假响应，是真的 `httptest` 服务端在 handler 里等着。掐断连接那一半
// 由 `TestCancelReallyCancels` 管，这里管的是**连接还活着、就是不回话**
// 的那一种——它只能靠「取消那个请求」收工，不能等它自己超时。

// stalledServer 是一个「收到了请求但不回话」的服务端。
type stalledServer struct {
	srv     *httptest.Server
	arrived chan struct{} // 收到请求
	gone    chan struct{} // 客户端把连接断了
	once    sync.Once
	once2   sync.Once
	hits    atomic.Int32
}

func newStalledServer(t *testing.T) *stalledServer {
	t.Helper()
	s := &stalledServer{arrived: make(chan struct{}), gone: make(chan struct{})}
	s.srv = httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		s.once.Do(func() { close(s.arrived) })
		// 客户端取消请求 → 这里醒来。服务端不会收到别的信号，
		// 所以「这一行醒了」本身就是「请求真的被取消了」的证据。
		<-r.Context().Done()
		s.once2.Do(func() { close(s.gone) })
	}))
	// 收尾一定先让服务端醒过来再关：不这么做 Close 会一直等 handler 返回。
	t.Cleanup(func() {
		select {
		case <-s.gone:
		default:
			close(s.gone)
		}
		s.srv.Close()
	})
	return s
}

func (s *stalledServer) url() string { return s.srv.URL + "/cover.png" }

// TestCloseWhileACoverIsStillLoading 是这一组的主线：关掉的时候图还在下。
//
// 四条断言，缺一条都对应一个「退不出去 / 退出去还留东西」的症状：
//
//	Close 有界地回来   它要等下载泵退出；泵阻塞在一个还在等的请求上
//	请求真的被取消     不是等 15 秒超时之后「自己好了」
//	没有叫醒任何窗口   窗口都没了，唤醒就是对着空气 Invalidate
//	再 Get 明确拒      关掉之后不该又悄悄下一次
func TestCloseWhileACoverIsStillLoading(t *testing.T) {
	s := newStalledServer(t)
	c := New(Options{})
	woke := &atomic.Int32{}
	c.AddWake(func() { woke.Add(1) })

	if st := c.Get(s.url()); !started(st) {
		t.Fatalf("刚 Get 应该是在下，拿到 %+v", st)
	}
	select {
	case <-s.arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("请求一直没发出去，这条测的不是「正在加载时关窗口」")
	}

	done := make(chan struct{})
	go func() { c.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Close 卡住了：关窗口会等在途的下载，用户会觉得应用退不出去")
	}
	c.Close() // 重复调用也不许炸

	select {
	case <-s.gone:
	case <-time.After(10 * time.Second):
		t.Error("退出之后那个请求还挂着：下载是靠超时收的，不是靠取消")
	}

	// 服务端收工之后，给唤醒留一点时间。迟到的那一下如果还来，
	// 就是「窗口关了还在叫醒界面」。
	time.Sleep(300 * time.Millisecond)
	if n := woke.Load(); n != 0 {
		t.Errorf("关掉之后还叫醒了 %d 次窗口", n)
	}

	// ⚠️ 这里问的是**另一个**地址（没见过的那一个），不是刚才那个。
	//
	// 刚才那个地址的条目还在表里，Close 之后 `Get` 会把它的旧状态原样返回
	// ——于是它报的是 `Loading`，而那个下载已经永远不会再来了（`fetch` 在
	// `context.Canceled` 上不落终态，见 cache.go）。界面上那一句就变成
	// 「载入中」永远不变。
	//
	// 现在碰不到：`images.Close()` 只在窗口**真的**关掉时跑（shell.go 的
	// `OnClosed`），选「隐藏到托盘」时压根跑不到；而选「关掉就退出」时窗口
	// 关完就退进程了，没人会再画一帧。所以这是一条**待修的隐患**，不是现在
	// 的故障——已经记在 BMG-43 的报告里，改法是「关掉之后、状态还没落到
	// 终态的那些条目，报 ErrClosed 而不是 Loading」。这里只断新地址那一条
	// （它现在是对的），不去把这个隐患钉成「期望行为」。
	if st := c.Get(s.srv.URL + "/never-seen.png"); st.Err == nil {
		t.Errorf("关掉的缓存不该又去下载，拿到 %+v", st)
	}
	if s.hits.Load() != 1 {
		t.Errorf("一共发了 %d 个请求，want 1（关掉之后不该再发）", s.hits.Load())
	}
}

// TestNothingWakesAWindowAfterCloseReturns 盯「关掉之后还在解码的那些结果」。
//
// 和上面那条是同一条路的另一半：上面是「请求还挂着（字节一个没到）」，
// 这一条是**字节真的到了、正在解码**。后者更接近真机上会发生的那个情况：
// 用户关窗的那一刻，屏幕上还有几张图正在解码队列里排着。
//
// ⚠️ 这一条**不断言「解码会不会在 Close 之后才落地」**——那是时序，机器一忙
// 就不一样，断言它等于写一条 flaky。它断言的是那条**不管时序如何都成立**的：
//
//	`Close()` 回来之后，不会再有任何人叫醒窗口。
//
// 这一条是 `Close` 的实现给的（它等的是 `lifeWG`（下载）和 `decodeWG`（解码
// worker）两组 goroutine），而它正是「窗口没了之后不该再有人 Invalidate 一个
// 死窗口」这条保证的落脚点。真机上的症状是：主窗口关了、迷你窗口莫名其妙自己
// 刷新了一下。
//
// 所以这一条必须**真的有图在解码**：服务端发的是真的 PNG 字节，缓存是
// `Decoders: 1`（一次解一张），一共发好几张——于是 Close 的时候队列里
// 一定还压着活。
func TestNothingWakesAWindowAfterCloseReturns(t *testing.T) {
	const (
		imgs = 24
		side = 512 // 图要**够大**：8×8 那种一瞬间就解完，关窗时队列是空的，
		//      这条就变成空过的绿（拿掉 Close 里那句 decodeWG.Wait() 它照样绿）
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(pngBytes(t, side, side))
	}))
	defer srv.Close()

	c := New(Options{Decoders: 1, MaxInflight: imgs})
	woke := &atomic.Int32{}
	c.AddWake(func() { woke.Add(1) })

	urls := make([]string, imgs)
	for i := range urls {
		urls[i] = fmt.Sprintf("%s/%d.png", srv.URL, i)
		c.Get(urls[i])
	}

	// 🔴 **前置条件必须成立，否则这条就是空过的。**
	//
	// ⚠️ 判据**不是**「还有图没 Ready」——`entry.state()` 里没 Ready 的条目
	// 既可能正在下载，也可能已经下完正排队等解码（`fetch` 取消时会直接返回，
	// **不进解码队列**，见 cache.go 的 fetch）。用「没 Ready」当下限时，
	// 存在一条执行路径：24 张全都还在下载，一个都没进解码队列，于是
	// 「Close 不等解码 worker」也能过——验的就不是「等解码退出」了。
	//
	// 这里直接看**解码队列的深度**：这个测试和 cache.go 同包，能读 `decodeCh`。
	// 它非空 = 此刻确实有活压着，等 Close 的 `decodeWG.Wait()` 去收。
	waitFor(t, 20*time.Second, "解码队列里确实压着活（否则这条测不到「等解码退出」）", func() bool {
		return len(c.decodeCh) > 0
	})

	c.Close()
	// ⚠️ 基线取在 **Close 之后**，不是之前：Close 等的就是那些还在解码的，
	// 它们在等待期间叫醒几次是正常的（那时候窗口还在）。
	before := woke.Load()
	time.Sleep(300 * time.Millisecond)
	if n := woke.Load(); n != before {
		t.Errorf("Close 回来之后又叫醒了 %d 次窗口：还有解码/下载活着", n-before)
	}
}
