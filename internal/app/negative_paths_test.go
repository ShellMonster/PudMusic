package app_test

import (
	"errors"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// 两条「事情做到一半被打断」的路径。
//
// 它们各自对应一个用户动作：**退出去**和**网断了**。
//
// 两条的共同点也正是它们难测的地方——被打断的那一刻，什么都还没「完成」：
// 流只下了一半、登录还没成功。这时候最容易出的不是崩溃，是**悄悄留下东西**：
// 一个还在跑的下载 goroutine、一个删不掉的临时文件、一句永远不消失的「登录失败」。
// 崩溃会立刻被发现，而这三样不会——它们只是让下一次开机或下一次下载变怪。

// ─────────────────────────────────────────────────────────────────────────────
// 下载中退出
// ─────────────────────────────────────────────────────────────────────────────

// TestQuittingWhileTheStreamIsStillDownloading 盯「退出去」那一刻。
//
// 造的场景：CDN 发了三分之一就**停住不回**——和网络断了的现象一样，但服务端
// 还活着、连接还在。这比「掐断连接」难：掐断那一半 `Close` 靠 cond 叫醒
// 等待者就够了，停住的那一半要靠**真的取消那个 HTTP 请求**才能收干净。
//
// 断言四条，缺一条都可能出现「退出去之后后台还在下」：
//
//	Close 很快回来       它会等下载泵退出的，所以卡住 = 用户关不掉应用
//	服务端看见请求被取消 请求真的断了，不是等着 15 秒超时
//	临时文件删干净        下载缓存是落盘的，不删就是每天攒垃圾
//	我们自己的 goroutine 收干净 下载泵和看门狗都不能留在进程里
func TestQuittingWhileTheStreamIsStillDownloading(t *testing.T) {
	f := newStreamFixture(t)
	data := readFlac(t)
	f.media = data

	sending := make(chan struct{})
	aborted := make(chan struct{})
	var abortOnce sync.Once
	f.on("/cdn/slow.flac", func(w http.ResponseWriter, r *http.Request) {
		start := rangeStartOf(r)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)-start))
		if start > 0 {
			w.Header().Set("Content-Range",
				"bytes "+strconv.Itoa(start)+"-"+strconv.Itoa(len(data)-1)+"/"+strconv.Itoa(len(data)))
			w.WriteHeader(http.StatusPartialContent)
		}
		// 发三分之一，然后一直不发。
		w.Write(data[start : start+len(data)/3])
		w.(http.Flusher).Flush()
		abortOnce.Do(func() { close(sending) })

		// 客户端取消 = 连接断 = 这里醒来。服务端**永远等不到**别的信号，
		// 所以「这一行醒了」本身就是请求被取消了的证据。
		<-r.Context().Done()
		close(aborted)
	})
	f.onPlayURL(dashFor(cdnURL(f.URL, "slow.flac", time.Hour), int(bili.QualityHigh192), 110836))

	dir := t.TempDir()
	a := app.New()
	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	a.MountStreaming(app.Streaming{Client: cli, Media: f.Client(), TempDir: dir})

	if err := a.Enqueue(onlineItem("BV1", "1", "下一半才到")); err != nil {
		t.Fatal(err)
	}

	select {
	case <-sending:
	case <-time.After(30 * time.Second):
		t.Fatal("CDN 一直没开始发，这条测的不是「下载中退出」")
	}
	// 让播放器真的拿着这段半截字节在解（解码器会阻塞在 Read 上等着补齐），
	// 那才是退出去要收拾的现场。
	waitFor(t, 30*time.Second, "这一首被装上了播放器", func() bool {
		return a.Snapshot().HasTrack
	})
	// ⚠️ 这一条是**防自己骗自己**的：没有在途的下载，下面那些「都收干净了」
	// 全都 trivially 成立——测的其实是「什么都没发生时退出」。
	if liveStreamWorkers() == 0 {
		t.Fatal("此刻没有在途的下载，这条测的不是「下载中退出」")
	}

	closed := make(chan struct{})
	go func() { a.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(30 * time.Second):
		t.Fatal("Close 卡住了：退出去要等在途的下载，用户会以为应用死了")
	}
	a.Close() // 重复调用也不许炸

	select {
	case <-aborted:
	case <-time.After(10 * time.Second):
		t.Error("退出之后那个请求还挂着：下载泵不是靠取消收工的，是在等超时")
	}
	waitFor(t, 15*time.Second, "缓存文件被删干净", func() bool {
		entries, err := os.ReadDir(dir)
		return err == nil && len(entries) == 0
	})
	waitFor(t, 15*time.Second, "下载和看门狗都退出", func() bool { return liveStreamWorkers() == 0 })
}

// ─────────────────────────────────────────────────────────────────────────────
// 扫码中途断网
// ─────────────────────────────────────────────────────────────────────────────

// TestScanningSurvivesTheNetworkGoingAway 盯扫码这一轮里网络断了又回来。
//
// 断网发生在**二维码已经画出来、用户还没扫**的时候——地铁里、进电梯的那几秒。
// 这时候界面上**绝不能**出现「登录失败」：用户会以为二维码废了，去点刷新，
// 刷新出来还是同一个死循环，最后归因成「这个应用登录不了」。
//
// 正确的那一半是另一半：网络回来之后，**这一轮自己就继续轮询**，用户不用
// 重新扫一次码——他已经扫了，扫的结果还在服务端等着。
//
// ⚠️ 断网是在**传输层**造的（连接建不起来），不是在响应里造一个 5xx：
// 地铁里发生的是前者，两者走的分支不同。
func TestScanningSurvivesTheNetworkGoingAway(t *testing.T) {
	f := newBiliFixture(t)
	// 网络好的时候：还没扫 → 还没扫 → 成功。
	f.onScans(86101, 86101, 0)

	// ⚠️ 断网**按时间**造，不按次数。
	//
	// 按次数（「前三次连不上」）看着更准，实际是个 flaky：测试里的轮询间隔是
	// 2 ms，三次断网总共六毫秒，而测试每 10 ms 才读一次状态——于是「重试」
	// 那句提示完全可能在两次轮询之间被「等待扫码」盖过去，测试就红了，
	// 而且是那种「跑十次红一次、还不知道为什么」的。
	//
	// 断 400 ms 就有四十次机会读到它，而这四百毫秒在用户那里是「地铁进隧道」。
	outage := 400 * time.Millisecond
	net := newFlakyNet(outage, "/qrcode/poll")
	net.base = f.Server.Client().Transport
	a := newTestAppWithBase(t, f, app.NewMemoryStore("test"), net)
	begin := time.Now()
	a.BeginLogin()

	// 等二维码出来：申请二维码那一下网络是好的。
	waitFor(t, waitTimeout, "二维码画出来", func() bool { return a.LoginState().QR != nil })

	waitFor(t, 15*time.Second, "断网这件事被界面上读到了", func() bool {
		return strings.Contains(a.LoginState().Hint, "重试")
	})
	st := a.LoginState()
	if st.Phase != app.LoginWaiting {
		t.Errorf("断网之后阶段变成了 %v，want LoginWaiting——用户会以为二维码废了", st.Phase)
	}
	if st.Err != "" {
		t.Errorf("断网被当成了失败：%q", st.Err)
	}
	if st.QR == nil {
		t.Error("断网之后二维码被清掉了，用户得重新扫一次")
	}

	// 网络回来：不用再扫，这一次轮询就该把登录走完。
	waitFor(t, 15*time.Second, "网络回来之后登录成功", func() bool {
		return a.LoginState().Phase == app.LoginDone
	})
	// 防自己骗自己：一个一直放行的传输层也能让上面几条全绿。
	if n := net.failed(); n < 3 {
		t.Errorf("只真的断了 %d 次（want ≥3）——这一条测的不是「断网」，是一条没断的网", n)
	}
	if d := time.Since(begin); d < outage {
		t.Errorf("整条只跑了 %v，断网那段（%v）根本没发生", d, outage)
	}
	if !a.LoggedIn() {
		t.Error("阶段是登录成功，但拿不到登录态")
	}
	if got := a.LoginState().Err; got != "" {
		t.Errorf("成功之后还留着错：%q", got)
	}
	waitFor(t, 10*time.Second, "下载和看门狗都退出", func() bool { return liveStreamWorkers() == 0 })
}

// flakyNet 是一个会「断网」的传输层：在 until 之前，指定路径的请求直接连不起来；
// 之后放行。
//
// ⚠️ 造的是 `*net.OpError`（dial 失败），不是返回一个 5xx 响应：地铁里
// 发生的是连接建不起来，而那一条走的是「连不上」那一支，不是「B 站回了个错」。
type flakyNet struct {
	base  http.RoundTripper
	only  string
	mu    sync.Mutex
	until time.Time
	tried int
}

func newFlakyNet(outage time.Duration, pathContains string) *flakyNet {
	return &flakyNet{only: pathContains, until: time.Now().Add(outage)}
}

func (n *flakyNet) RoundTrip(r *http.Request) (*http.Response, error) {
	if strings.Contains(r.URL.Path, n.only) {
		n.mu.Lock()
		if time.Now().Before(n.until) {
			n.tried++
			n.mu.Unlock()
			return nil, &net.OpError{Op: "dial", Net: "tcp",
				Err: errors.New("network is unreachable")}
		}
		n.mu.Unlock()
	}
	return n.base.RoundTrip(r)
}

// failed 报真的断了几次。用来断言「断网那几下确实发生了」——不然一个
// 永远放行的传输层也能让上面那条测试变绿。
func (n *flakyNet) failed() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.tried
}
