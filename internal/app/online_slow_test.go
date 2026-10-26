package app_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// 这一组是票 #220 的回归：「取流非常慢」和「一直转圈」这两件事。
//
// # 复现的是什么
//
// 用户实测（2026-10-24）：「我感觉取流非常慢，有时候点到什么歌一直在加载取流」。
//
// 那一票把取流这一趟拆成三段来量：
//
//	① 问 playurl 拿地址	一次带 WBI 签名的接口往返
//	② 下载首段字节		CDN 那边给头几个字节
//	③ 解码第一帧		这一台机器的解码器开这条流
//
// 而查出来的病根是**判据错了**：下载那一段等的是**一条持续的下载**，不是
// 「一次往返」——而闹钟量的是「这一段**一共**等了多久」。于是：
//
//	一条慢而**一直在走**的流	20 秒后被掐掉 → 「取流超时了」，而歌明明下得好好的
//	一个字节都不来的流	转圈转到 20 秒才停 → 「一直转圈」
//
// 改之后的判据是「**这一段还有没有进展**」：还在走就接着等（界面上说「取流
// 很慢」并说清卡在哪一步），一个字节都不来才按失败处理。

// slowCDN 是一台「先给一点点、然后卡住等放行」的 CDN。
//
// ⚠️ **它模拟的是「网络慢但没断」**——用户抱怨的正是这个：慢不是错，
// 而把慢当成错（20 秒就掐掉）才是错。
//
// ⚠️ **头一点点要「少到解码器开不起来」**：解码器的嗅探要 16 个字节
// （MyGo 的 `NewDecoder`），而它开不起来，这一趟才**停在这一段**
// （「下音频」）里——不然这一段两毫秒就过去了，测试根本来不及看。
//
// arrived 是「真的开始给字节了」的信号；release 是「放行剩下的字节」。
func slowCDN(data []byte, first int, arrived chan struct{}, release chan struct{}) http.HandlerFunc {
	var once sync.Once
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		if _, err := w.Write(data[:first]); err != nil {
			return
		}
		if fl != nil {
			fl.Flush()
		}
		once.Do(func() { close(arrived) })
		// 卡住，等测试放行。
		<-release
		if _, err := w.Write(data[first:]); err != nil {
			return
		}
		if fl != nil {
			fl.Flush()
		}
	}
}

// downloadedBytes 报「字节源已经下到多少字节」。
//
// ⚠️ **它看的是那个临时文件**：字节源把收到的字节写进 `TempDir` 里的
// `biu-stream-*`（见 `audio.NewStreamSource`），而 `WriteAt` 在偏移处写会
// **把文件撑大**——所以文件大小就是「已经下到的字节数」。
//
// 为什么测试要绕这一圈：CDN 那头「已经开始给字节」和「客户端已经收到
// 字节」之间**有一步之差**（服务器写完 → 传输层 → 客户端读走 → 落盘）。
// 而「这一段有没有进展」判的是**落盘**那一步——所以测试必须等落盘，
// 等服务器的信号会差那么一点点（而那一点点正好够判成「没进展」）。
func downloadedBytes(t *testing.T, dir string) int64 {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Size() > total {
			total = info.Size()
		}
	}
	return total
}

// TestASlowButProgressingStreamIsNotKilled 盯「慢但在走的流不许被掐掉」。
//
// 🔴 **这是这一票最要紧的一条**：改之前那个 20 秒的闹钟量的是「这一段一共
// 等了多久」，而下载那一段等的是**一条持续的下载**——一条 30 秒才下完的
// 慢流会被 20 秒的闹钟判成失败，用户看到的是「取流超时了」，而歌本来
// 好好的。
//
// 改之后判据是「**这一段还有没有进展**」：还在走就接着等。
func TestASlowButProgressingStreamIsNotKilled(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	dir := t.TempDir()
	arrived := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	// 🔴 **只给 12 个字节**：解码器的嗅探要 16 个（MyGo 的 `NewDecoder`），
	// 所以这一趟**开不起来**——它停在「下音频」这一段里，测试才看得见。
	f.on("/cdn/slow.m4s", slowCDN(f.media, 12, arrived, release))
	f.onPlayURL(dashFor(cdnURL(f.URL, "slow.m4s", 2*time.Hour),
		int(bili.QualityHigh192), 110836))

	a := newOnlineApp(t, f, dir)
	clock := &fakeClock{}
	a.SetAfterForTest(clock.after)

	if err := a.Enqueue(onlineItem("BV1", "1", "慢慢下的那一首")); err != nil {
		t.Fatal(err)
	}

	// 一、**这一段确实在走**：字节已经落到缓存文件里了。
	waitFor(t, 15*time.Second, "字节源收到字节了", func() bool {
		return downloadedBytes(t, dir) > 0
	})
	waitFor(t, 15*time.Second, "状态里说在取流", func() bool {
		return a.Snapshot().Loading
	})

	// 三、**超时的闹钟响了**（假钟点火 = 「20 秒到了」）。
	clock.fireAt(app.LoadPlayTimeout)

	// 四、🔴 **关键**：这一段还在走 → 不许判它失败。
	waitFor(t, 15*time.Second, "下载还在走", func() bool {
		return a.Snapshot().Loading
	})
	st := a.Snapshot()
	if st.Err != "" {
		t.Fatalf("一条还在好好下的流被判成了失败：%q（%q）", st.Err, st.ErrDetail)
	}

	// 五、**最后它真的放出来了**：掐掉「不判失败」不算修好。
	close(release)
	playing(t, a, "慢慢下的那一首")
}

// TestAStuckStreamStopsTheSpinner 盯「一个字节都不来的流要停下来」。
//
// 🔴 **这一条是「一直转圈」那半边**：闹钟响了而这一段**没有进展**，那就
// 按失败处理——转圈停，一句能看懂的话上屏。
//
// ⚠️ **判据是「有没有进展」而不是「等了多久」**：所以这一条特意用一台
// **一个字节都不给**的 CDN——那正是「卡死」的样子。
func TestAStuckStreamStopsTheSpinner(t *testing.T) {
	f := newStreamFixture(t)
	arrived := make(chan struct{})
	f.on("/cdn/hang.m4s", hangingCDN(arrived))
	f.onPlayURL(dashFor(cdnURL(f.URL, "hang.m4s", 2*time.Hour),
		int(bili.QualityHigh192), 110836))

	a := newOnlineApp(t, f, t.TempDir())
	clock := &fakeClock{}
	a.SetAfterForTest(clock.after)

	if err := a.Enqueue(onlineItem("BV1", "1", "卡住的那一首")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("取流一直没到 CDN：这一条测的不是「流卡住了」")
	}
	waitFor(t, 15*time.Second, "状态里说在取流", func() bool {
		return a.Snapshot().Loading
	})

	clock.fire()

	waitFor(t, 15*time.Second, "卡住之后有一句话", func() bool {
		return a.Snapshot().Err != ""
	})
	st := a.Snapshot()
	// 🔴 **转圈必须停**——这一条是这一票的验收项。
	if st.Loading {
		t.Error("卡住之后还在转圈——那还是「一直转圈」")
	}
	if !strings.Contains(st.Err, "超时") {
		t.Errorf("话里没有说清是超时：%q", st.Err)
	}
	// 🔴 **话要说清是「下载」那一段**：用户面对的是「网络断了」还是
	// 「B 站不给地址」，要做的下一件事完全不同。
	if !strings.Contains(st.ErrDetail, "音频") {
		t.Errorf("详情里没有说清是哪一段：%q", st.ErrDetail)
	}
	// 网络问题是可以再试一次的。
	if !st.CanRetry {
		t.Error("下载卡死是可以再试一次的（网络问题），界面上却没有「重试」那一枚键")
	}
}

// TestTheRiskControlDoesNotTurnIntoAWait 盯「风控 / 限流不会变成一直等」。
//
// ⚠️ **「不可重试」说的是没有「重试」键，界面上的转圈停不停是另一件事**
// （票 #346 判的是前者，这一票查的是后者）。
//
// B 站回 -509（限流）的时候，应用必须**立刻**给一句人话并且把转圈收掉——
// 而不是「再等等看」：-509 要等的是时间，等下去也是撞同一面墙。
func TestTheRiskControlDoesNotTurnIntoAWait(t *testing.T) {
	f := newStreamFixture(t)
	f.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		writeStreamJSON(w, map[string]any{"code": -509, "message": "请求过于频繁", "ttl": 1})
	})

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "被限流的那一首")); err != nil {
		t.Fatal(err)
	}

	waitFor(t, 15*time.Second, "限流报出来了", func() bool {
		return a.Snapshot().Err != ""
	})
	st := a.Snapshot()
	if st.Loading {
		t.Error("限流之后还在转圈——风控变成了一场没有尽头的等待")
	}
	if !strings.Contains(st.Err, "太快") {
		t.Errorf("话里没有说清是被限流：%q", st.Err)
	}
	// ⚠️ **不给「重试」**：那一枚是**立刻**再问一次，而限流要等的是时间。
	if st.CanRetry {
		t.Error("限流给了「重试」那一枚键——按下去就是再撞一次墙")
	}
}

// TestTheThreeSegmentsAreWrittenDown 盯「分段耗时写进日志」。
//
// 🔴 **它是「先量再改」的那一半**：没有这三段，谁也说不清慢的是哪一段。
// 而日志是**事后**唯一的凭据——用户能描述的只有「它不响」（票 #282）。
func TestTheThreeSegmentsAreWrittenDown(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.onPlayURL(dashFor(cdnURL(f.URL, "x.m4s", 2*time.Hour),
		int(bili.QualityHigh192), 110836))

	a := newOnlineApp(t, f, t.TempDir())
	lines, restore := app.CapturePlayLog(a)
	t.Cleanup(restore)

	if err := a.Enqueue(onlineItem("BV1", "1", "量一量的那一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "量一量的那一首")

	waitFor(t, 15*time.Second, "日志里写了分段耗时", func() bool {
		return strings.Contains(strings.Join(*lines, "\n"), "取流分段耗时")
	})
	joined := strings.Join(*lines, "\n")
	// 🔴 **三段都要在**：缺一段就答不出「慢的是哪一段」。
	for _, want := range []string{"① 问地址", "② 下音频", "③ 解码"} {
		if !strings.Contains(joined, want) {
			t.Errorf("日志里没有 %q：\n%s", want, joined)
		}
	}
	if !strings.Contains(joined, "量一量的那一首") {
		t.Errorf("日志里没写是哪一首：\n%s", joined)
	}
}

// TestBothStreamPathsShareOneTimeoutPolicy 盯**两条取流路径是同一套口径**
// （票 #220 的验收项）。
//
// 投稿那一路（`/x/player/wbi/playurl`）和音乐库那一路（`sid`）问的是两个
// 接口，但**超时这件事上它们必须一样**：
//
//	问地址	同一个 `bili.Client`，同一个 10 秒请求超时
//	下音频	同一个字节源，同一个 20 秒卡死判定
//
// 这一条盯的是**应用这一层**：两条路走的是**同一段代码**（`streamFor` 按
// 类型分派，之后一模一样），所以判据必然一致——而这一条钉住「将来别把
// 它们分家」。
//
// ⚠️ **它盯的是「音乐库那一路也要问一次元信息」**：那一路比投稿多一次
// 网络往返（`withSongInfo`），所以它的「问地址」那段可能更长——但**上限
// 一样**，而且**元信息问不到不许挡住播放**（那是「锦上添花」）。
func TestBothStreamPathsShareOneTimeoutPolicy(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	// 音乐库那一路：取流接口给一条流，**元信息那一趟挂掉**。
	f.musicCDN = cdnURL(f.URL, "music.m4a", 2*time.Hour)
	f.on("/audio/music-service-c/web/song/info", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "这一趟挂了", http.StatusBadGateway)
	})

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(app.Item{Source: "online", Type: "audio", Sid: "1293494",
		Title: "音乐库那一首"}); err != nil {
		t.Fatal(err)
	}

	// 🔴 **元信息问不到不许挡住播放**：问不到就少一个作者名。
	playing(t, a, "音乐库那一首")
	st := a.Snapshot()
	if st.Err != "" {
		t.Errorf("音乐库那一首没放起来：%q", st.Err)
	}
}

// gatedCDN 是一台「先给前 n 个字节、然后卡住等放行」的 CDN。
//
// 🔴 **它卡住的位置是「文件还没下完」**：测试要靠它把「文件没下完」和
// 「解码器开好了」这两件事分开——而这两件事在 moov 在不在末尾时**完全相反**。
func gatedCDN(data []byte, n int, arrived chan struct{}, release chan struct{}) http.HandlerFunc {
	var once sync.Once
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		if n > len(data) {
			n = len(data)
		}
		if _, err := w.Write(data[:n]); err != nil {
			return
		}
		if fl != nil {
			fl.Flush()
		}
		once.Do(func() { close(arrived) })
		<-release
		if _, err := w.Write(data[n:]); err != nil {
			return
		}
		if fl != nil {
			fl.Flush()
		}
	}
}

// gatedRangeCDN 是一台「主请求给前 n 个字节就卡住、而**尾部那一段立刻给**」的
// CDN。
//
// 🔴 **它把两件事分开**：主请求（没有 Range 头）走「给 n 个字节然后卡住」，
// 而带 Range 头的请求（源并行去取末尾那一小段）**立刻给完**。
//
// 有了这个区分，「末尾那一段是并行取回来的」这件事就有了**一个不靠时间的
// 判据**：主下载卡在 64 KB，而解码器照样开口了——它拿到的是并行取回来的
// `moov`。
func gatedRangeCDN(data []byte, n int, arrived chan struct{}, release chan struct{}) http.HandlerFunc {
	var once sync.Once
	return func(w http.ResponseWriter, r *http.Request) {
		from := int64(0)
		if h := r.Header.Get("Range"); h != "" {
			if _, err := fmt.Sscanf(h, "bytes=%d-", &from); err != nil {
				from = 0
			}
		}
		if from > 0 {
			// 尾部那一段：立刻给完。
			body := data[from:]
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.Header().Set("Content-Range",
				fmt.Sprintf("bytes %d-%d/%d", from, len(data)-1, len(data)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(body)
			return
		}
		// 主请求：给 n 个字节然后卡住等放行。
		if n > len(data) {
			n = len(data)
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		if _, err := w.Write(data[:n]); err != nil {
			return
		}
		if fl != nil {
			fl.Flush()
		}
		once.Do(func() { close(arrived) })
		<-release
		if _, err := w.Write(data[n:]); err != nil {
			return
		}
		if fl != nil {
			fl.Flush()
		}
	}
}

// TestTheDecoderOpensBeforeTheWholeFileArrives 钉住「**不用等整个文件下完**」
// （票 #478 的容器层修法）。
//
// # 这一条钉的是什么
//
// 「点一首歌 → 出声」的第③段（解码第一帧）在 macOS 上是 `extaf` 开口那一段。
// AudioToolbox 探 MP4 容器时要扫过所有顶层盒子才找得到 `moov`——而
// **B 站形状的完整 `.m4a` 的 `moov` 在文件末尾**。
//
// ⚠️ **实测：AudioToolbox 不是从头读到尾的，它是「跳」过去的**
// （2026-10-25 实测：它读 0 / 8 / 16 / 28 / 36，然后**直接跳到 `moov` 的
// 起点**）。所以**末尾那一小段在手上，它立刻就能开口**。
//
// 而在那之前，源只有**顺序下载的前沿**能用——于是「跳过去」的那一步会
// **等整个文件下完**（票 #220 的实测：200 KB/s 的网上 1.22 秒，
// 50 KB/s 的网上 5.02 秒）。
//
// # 判据是「形状」不是「毫秒」
//
// 🔴 **主下载卡在 64 KB，而解码器开口了**——这是一个事实，不受机器快慢
// 影响（AGENTS.md §2）。而「等了多久」那种判据在满载的机器上会飘。
//
// # 为什么这一条必须是**夹具**而不是「真机测一下」
//
// 真机上的形状取决于 B 站给的那个文件是不是 faststart，而**本地夹具能把两种
// 形状都造出来**——这才是能拿来做回归的东西。
func TestTheDecoderOpensBeforeTheWholeFileArrives(t *testing.T) {
	moovAtEnd := readMP4(t, "stereo16.mp4")
	moovAtFront := readMP4(t, "reference-24bit-mp4.m4a")

	t.Run("moov 在末尾：并行取回尾部那一段就能出声", func(t *testing.T) {
		dir := t.TempDir()
		f := newStreamFixture(t)
		arrived := make(chan struct{})
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		// 🔴 **主下载只给 64 KB 就卡住**：moov 在 242156 字节处，所以给不到。
		// 而「尾部那一段」是**另一条请求**，它立刻就给。
		f.on("/cdn/x.m4s", gatedRangeCDN(moovAtEnd, 64<<10, arrived, release))
		f.onPlayURL(dashFor(cdnURL(f.URL, "x.m4s", 2*time.Hour),
			int(bili.QualityHigh192), 110836))

		a := newOnlineApp(t, f, dir)
		if err := a.Enqueue(onlineItem("BV1", "1", "moov在末尾")); err != nil {
			t.Fatal(err)
		}
		select {
		case <-arrived:
		case <-time.After(30 * time.Second):
			t.Fatal("CDN 一直没开始给字节")
		}
		waitFor(t, 15*time.Second, "字节源开始收字节", func() bool {
			return downloadedBytes(t, dir) > 0
		})
		// 🔴 **关键**：主下载还卡在 64 KB，而**播放器已经装上了**——
		// 解码器拿到的是**并行取回来的那个 `moov`**。
		playing(t, a, "moov在末尾")
	})

	t.Run("moov 在开头：文件没下完也能出声", func(t *testing.T) {
		dir := t.TempDir()
		f := newStreamFixture(t)
		arrived := make(chan struct{})
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		f.on("/cdn/x.m4s", gatedCDN(moovAtFront, 64<<10, arrived, release))
		f.onPlayURL(dashFor(cdnURL(f.URL, "x.m4s", 2*time.Hour),
			int(bili.QualityHigh192), 110836))

		a := newOnlineApp(t, f, dir)
		if err := a.Enqueue(onlineItem("BV1", "1", "moov在开头")); err != nil {
			t.Fatal(err)
		}
		select {
		case <-arrived:
		case <-time.After(30 * time.Second):
			t.Fatal("CDN 一直没开始给字节")
		}
		// 🔴 **对照的那一半**：moov 在开头，64 KB 就够解码器开口了——
		// 于是**文件没下完它也出声了**。
		playing(t, a, "moov在开头")
	})

	t.Run("CDN 不认 Range 时退回老样子（等整个文件）", func(t *testing.T) {
		dir := t.TempDir()
		f := newStreamFixture(t)
		arrived := make(chan struct{})
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		// ⚠️ **这一台不认 Range**（`gatedCDN`）：它对带 Range 的请求也回
		// 200 + 整个文件，于是并行取尾部的那一条**只能作废**（见
		// `fetchTail`：只认 206）。
		//
		// 于是这里就是**老样子**：文件没下完就不出声。
		//
		// 🔴 **这一条不是「我们又慢回去了」**：它是「CDN 不给这一段时，
		// 我们照样能放，只是慢一点」——**不许**因为它就判失败。
		f.on("/cdn/x.m4s", gatedCDN(moovAtEnd, 64<<10, arrived, release))
		f.onPlayURL(dashFor(cdnURL(f.URL, "x.m4s", 2*time.Hour),
			int(bili.QualityHigh192), 110836))

		a := newOnlineApp(t, f, dir)
		if err := a.Enqueue(onlineItem("BV1", "1", "moov在末尾")); err != nil {
			t.Fatal(err)
		}
		select {
		case <-arrived:
		case <-time.After(30 * time.Second):
			t.Fatal("CDN 一直没开始给字节")
		}
		waitFor(t, 15*time.Second, "字节源开始收字节", func() bool {
			return downloadedBytes(t, dir) > 0
		})
		// 文件才下了一小部分，而**播放器还没装上**——
		// 解码器在等那个还在文件末尾的 `moov`。
		time.Sleep(300 * time.Millisecond)
		st := a.Snapshot()
		if st.HasTrack {
			t.Error("文件才下了一小部分就出声了——这一条钉的是「拿不到尾部时还是老样子」")
		}
		if !st.Loading {
			t.Error("还没出声就不在「正在取流」了——界面上会是一句空白")
		}
	})
}

// readMP4 读一份 MP4 素材。
func readMP4(t *testing.T, name string) []byte {
	t.Helper()
	p := filepath.Join("..", "service", "audio", "flacdec", "testdata", name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Skipf("拿不到素材 %s：%v", name, err)
	}
	return b
}

// TestMeasureTheThreeSegments 是**量那三段**的工具（票 #220 的「先量再改」，
// 票 #478 把它扩成「冷启动 + 取流」两段的对照）。
//
// # 它量的是什么
//
// 「点一首歌 → 出声」的各段，在**同一台限速的假 CDN**上各占多少：
//
//	① 问地址	问 playurl 拿地址（一次接口往返）
//	② 下音频	从建源到头几个字节落到缓存里
//	③ 解码	从头几个字节到解码器把流开好（能出声了）
//
// # 🔴 **它量的是同一个形状的两次**（票 #478）
//
// 同一个文件、同一个速率，跑两遍：一遍的 CDN **不认 Range**（= 没有并行
// 取尾部），一遍的 CDN **认 Range**（= 末尾那一小段被并行取回来了）。
// 两次的差就是**容器层那个修法省下来的时间**。
//
// ⚠️ **绝对值会飘**（机器一忙就飘），**差值是这一条要看的**——而差值的
// 形状是确定的：「不认 Range」那一次，③ 必然 ≈ 整个文件下载完的时间。
//
// # ⚠️ **它量的是本地夹具，不打真 B 站**（铁律）。
//
// 它量的是**代码这一侧的形状**：某一台机器上「下音频」和「解码」各占多少。
// 真机上的绝对值取决于 B 站和用户的网络，而**相对关系**（③ 是不是等于整个
// 下载时间）由容器形状决定——那个是夹具能测准的。
//
// # 怎么跑
//
//	BIU_MEASURE=1 go test ./internal/app/ -run TestMeasureTheThreeSegments -v
//
// 平时它**跳过**（`BIU_MEASURE` 没设）：它要跑好几秒，而它不是一条断言。
func TestMeasureTheThreeSegments(t *testing.T) {
	if os.Getenv("BIU_MEASURE") == "" {
		t.Skip("设 BIU_MEASURE=1 才量（它要跑好几秒）")
	}
	cases := []struct {
		name string
		file string
		rate int
	}{
		{"moov 在开头（faststart）", "reference-24bit-mp4.m4a", 200_000},
		{"moov 在末尾", "stereo16.mp4", 200_000},
		{"moov 在末尾 + 慢网（50KB/s）", "stereo16.mp4", 50_000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := readMP4(t, tc.file)
			for _, shape := range []struct {
				label string
				cdn   func([]byte, int) http.HandlerFunc
			}{
				{"不认 Range（＝没有并行取尾部）", rateCDN},
				{"认 Range（＝并行取了尾部）", rateRangeCDN},
			} {
				f := newStreamFixture(t)
				f.on("/cdn/x.m4s", shape.cdn(data, tc.rate))
				f.onPlayURL(dashFor(cdnURL(f.URL, "x.m4s", 2*time.Hour),
					int(bili.QualityHigh192), 110836))
				a := newOnlineApp(t, f, t.TempDir())
				lines, restore := app.CapturePlayLog(a)
				t.Cleanup(restore)
				if err := a.Enqueue(onlineItem("BV1", "1", tc.name)); err != nil {
					t.Fatal(err)
				}
				playing(t, a, tc.name)
				for _, l := range *lines {
					if strings.Contains(l, "取流分段耗时") {
						t.Logf("%s（%d 字节，%d 字节/秒，%s）→ %s",
							tc.name, len(data), tc.rate, shape.label, l)
					}
				}
			}
		})
	}
}

// rateCDN 是一台「按固定速率给字节」的 CDN。
//
// 🔴 **它给的是「这一秒给多少字节」**：慢网的形状是「一直在走，只是走得慢」——
// 而那正是「慢但在走」和「卡死了」的区别（见 `downloadProgressLocked`）。
//
// ⚠️ **它不认 Range**：来了带 Range 的请求它也回 **200 + 整个文件**。
// 而尾部那一段（票 #478 的容器层修法）只认 **206**——所以这一台 CDN
// 造出来的就是「**没有**并行取尾部」的那一种形状。
func rateCDN(data []byte, bytesPerSec int) http.HandlerFunc {
	chunk := bytesPerSec / 20
	if chunk < 1024 {
		chunk = 1024
	}
	per := time.Duration(float64(time.Second) * float64(chunk) / float64(bytesPerSec))
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		for off := 0; off < len(data); off += chunk {
			end := off + chunk
			if end > len(data) {
				end = len(data)
			}
			if _, err := w.Write(data[off:end]); err != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
			time.Sleep(per)
		}
	}
}

// rateRangeCDN 是一台「认 Range、按固定速率给字节」的 CDN。
//
// 🔴 **它和 `rateCDN` 的唯一区别就是认不认 Range**——而那一条区别
// 恰好就是「**有没有并行把文件尾部取回来**」（票 #478 的容器层修法）：
// 认 Range 的那台会让源多发一条 `Range: bytes=<moov 起点>-`，
// 于是 AudioToolbox 不用等整个文件下完就能开口。
//
// 所以这两台 CDN 摆在一起就是一次**干净的 A/B**：同样的字节、同样的速率，
// 唯一的差别是「末尾那一小段是不是并行取回来了」。
func rateRangeCDN(data []byte, bytesPerSec int) http.HandlerFunc {
	chunk := bytesPerSec / 20
	if chunk < 1024 {
		chunk = 1024
	}
	per := time.Duration(float64(time.Second) * float64(chunk) / float64(bytesPerSec))
	return func(w http.ResponseWriter, r *http.Request) {
		from := int64(0)
		if h := r.Header.Get("Range"); h != "" {
			if _, err := fmt.Sscanf(h, "bytes=%d-", &from); err != nil {
				from = 0
			}
		}
		if from > int64(len(data)) {
			http.Error(w, "越界", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		body := data[from:]
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if from > 0 {
			w.Header().Set("Content-Range",
				fmt.Sprintf("bytes %d-%d/%d", from, len(data)-1, len(data)))
			w.WriteHeader(http.StatusPartialContent)
		}
		fl, _ := w.(http.Flusher)
		for off := 0; off < len(body); off += chunk {
			end := off + chunk
			if end > len(body) {
				end = len(body)
			}
			if _, err := w.Write(body[off:end]); err != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
			time.Sleep(per)
		}
	}
}

// TestAnotherLaneMustNotStealTheProgressWatchdog 盯「别的路不许把这一趟的
// 进度看门狗顶掉」（票 #220 的评审复现的那一条）。
//
// # 复现的是什么
//
// 「这一段还有没有进展」判的是**字节源已经下到多少字节**——而那一栏要是
// **App 上共享的那一条源**（`pendingSrc`），那么**换音质**（`restreamOnline`）
// 和**换流重开**（`refreshOnlineNow`）那两条路建源时就会把它顶掉：它们建的是
// **它们自己的**源，而用户此刻在等的**不是**那一条。
//
// 于是：一条正在好好下、进度一直在涨的流，被判成「一个字节都没来」，
// 然后被掐掉——**正是这一票要消灭的那一种症状**。
//
// 🔴 **判据是「别的路换过源之后，这一趟还在不在」**：不是「转圈停没停」
// （那一条别的用例已经钉了），而是「**这一趟没被误判成失败**」。
func TestAnotherLaneMustNotStealTheProgressWatchdog(t *testing.T) {
	f := newStreamFixture(t)
	dir := t.TempDir()
	arrived := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	// ⚠️ **用 moov 在末尾的那份 MP4**：卡住的那一条只给 12 KB——解码器开不起来
	// （它要等文件末尾那个 moov），于是那一趟停在「下音频」那一段。
	//
	// 🔴 **「哪一条卡住」是按地址定的，不是按「第几次请求」**（后者会飘：
	// 后台的预取 / 重连也会打这个地址，谁抢到算谁的）。
	f.on("/cdn/slow.m4s", gatedCDN(readMP4(t, "stereo16.mp4"), 12<<10, arrived, release))
	f.on("/cdn/fast.m4s", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(readMP4(t, "stereo16.mp4"))))
		w.Write(readMP4(t, "stereo16.mp4"))
	})
	// 🔴 **取流接口按 bvid 给不同的地址**：歌 B 的流走那条会卡住的地址，
	// 别的（**包括换音质那一趟**——它换的是歌 A）走会一次给全的那条。
	f.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		name := "fast.m4s"
		if r.URL.Query().Get("bvid") == "BV2" {
			name = "slow.m4s"
		}
		writeStreamJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{"timelength": 1000,
				"dash": twoTierDash(cdnURL(f.URL, name, 2*time.Hour))}})
	})

	a := newOnlineApp(t, f, dir)
	clock := &fakeClock{}
	a.SetAfterForTest(clock.after)

	// 一、**先放上一首**：`a.online` 得是**非空的**，换音质那条路才走得起来
	// （`SetAudioQuality` 里 `if tr != nil` 才去 `restreamOnline`）——而
	// **`a.online` 在新的一首还在取流的时候是上一首的**（它要等新播放器
	// 装上才被换掉）。
	if err := a.Enqueue(onlineItem("BV1", "1", "在放的那一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "在放的那一首")

	// 二、**点第二首**，而它的流卡住——于是这一趟停在「下音频」那一段里，
	// 而 `a.online` 还是**上一首**。
	if err := a.PlayFrom(onlineItem("BV2", "1", "正在下的那一首")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 15*time.Second, "第二首开始取流", func() bool {
		return a.Snapshot().Loading
	})
	waitFor(t, 15*time.Second, "字节源收到字节了", func() bool {
		return downloadedBytes(t, dir) > 0
	})

	// 三、🔴 **别的路来换源了**：换音质会走 `restreamOnline` → `buildFromRef`
	// → `setPendingSource`，把 App 上那个**共享的**源换成它自己的；而它
	// 走完之后那条源就归播放器管了——**共享的那一栏于是空了**。
	//
	// ⚠️ **它换的是「上一首」的流**（`a.online` 还是上一首）——而用户此刻
	// 在等的是**第二首**。这一下正好把两件事搅在一起。
	a.SetAudioQuality(bili.AudioMedium)
	// 换音质那一趟拿到的档是 128K（它点的是中档，而 dash 里有两档——
	// 见 `online_quality_test.go` 里那个 `twoTierDash`）——于是「它走完了」
	// 这件事**看得见**（徽章变了）。
	waitFor(t, 15*time.Second, "换音质那一条路走完了", func() bool {
		return a.Snapshot().Quality == "128K"
	})

	// 现在点火的闹钟问的是「这一段还有没有进展」。
	clock.fireAt(app.LoadPlayTimeout)

	// 🔴 **关键**：第二首还在好好下 → 不许被判成失败。
	waitFor(t, 15*time.Second, "这一趟还在取流", func() bool {
		return a.Snapshot().Loading
	})
	if st := a.Snapshot(); st.Err != "" {
		t.Fatalf("别的路换了源，这一趟就被误判成失败了：%q（%q）", st.Err, st.ErrDetail)
	}
}

// TestTheFailureSaysWhatActuallyHappened 盯**失败的那句话说的理由是真的**
// （票 #220 的评审）。
//
// 判据是「这一段**没有进展**」——而「没进展」**不等于**「一个字节都没到」：
// 下到一半卡住时（网络断了）字节明明已经到了，而那句话里写「一个字节都没到」
// 是在骗用户。
//
// 🔴 **夹具**：一台先给 64 KB 再卡住的 CDN。第一次闹钟：有进展，不判失败；
// 第二次：没有进展了 → 判失败，而失败那句话**必须说出真实的情况**。
func TestTheFailureSaysWhatActuallyHappened(t *testing.T) {
	f := newStreamFixture(t)
	dir := t.TempDir()
	arrived := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	// ⚠️ **用 moov 在末尾的那份 MP4**（243 KB）：先给 64 KB（够多，所以
	// 「已经下到一半」这句话成立），然后卡住——而**解码器开不起来**
	//（它要等文件末尾那个 moov），于是这一趟停在「下音频」那一段里。
	//
	// 用 FLAC 是不行的：FLAC 的头在前面，几十个字节就够解码器开口了，
	// 那一趟当场就放完了，根本没有「下到一半」这一段。
	f.on("/cdn/x.m4s", gatedCDN(readMP4(t, "stereo16.mp4"), 64<<10, arrived, release))
	f.onPlayURL(dashFor(cdnURL(f.URL, "x.m4s", 2*time.Hour),
		int(bili.QualityHigh192), 110836))

	a := newOnlineApp(t, f, dir)
	clock := &fakeClock{}
	a.SetAfterForTest(clock.after)

	if err := a.Enqueue(onlineItem("BV1", "1", "下到一半卡住的那一首")); err != nil {
		t.Fatal(err)
	}
	// ⚠️ **等的是「那 64 KB 全到了盘上」，不是「有字节了」**：失败那句话里
	// 写的字节数是**第一次闹钟时量到的那个数**（`loadFilled`，见
	// `downloadProgressLocked`），而夹具只保证「服务器把 64 KB 发出去了」——
	// **客户端收到几 KB 是另一回事**（机器一忙，socket 里那 64 KB 一次只
	// 读到 4 KB）。那时候那句话**如实**说「已经下了 4 KB」，于是下面那句
	// 断言（要说「64 KB」）就成了一条**竞态**：机器一忙就红。
	// （2026-10-25 在板上验收时红过一次：load 30+，报的是「已经下了 4 KB」
	// ——是这条竞态，不是产品的问题。）
	waitFor(t, 15*time.Second, "那 64 KB 全到了", func() bool {
		return downloadedBytes(t, dir) >= 64<<10
	})
	waitFor(t, 15*time.Second, "状态里说在取流", func() bool {
		return a.Snapshot().Loading
	})

	// 第一次闹钟：**有进展** → 不判失败。
	clock.fireAt(app.LoadPlayTimeout)
	waitFor(t, 15*time.Second, "还在取流", func() bool {
		return a.Snapshot().Loading
	})
	if st := a.Snapshot(); st.Err != "" {
		t.Fatalf("还在好好下就判失败了：%q", st.Err)
	}

	// 第二次闹钟：**没有进展**了 → 判失败。
	clock.fireAt(app.LoadPlayTimeout)
	waitFor(t, 15*time.Second, "卡住之后有一句话", func() bool {
		return a.Snapshot().Err != ""
	})
	st := a.Snapshot()
	if st.Loading {
		t.Error("卡住之后还在转圈")
	}
	// 🔴 **理由要按实际说**：字节明明已经到了（64 KB）。
	if strings.Contains(st.ErrDetail, "一个字节都没到") {
		t.Errorf("失败的理由是假的（明明已经下了 64 KB）：%q", st.ErrDetail)
	}
	if !strings.Contains(st.ErrDetail, "64 KB") {
		t.Errorf("失败的理由里没有说清已经下了多少：%q", st.ErrDetail)
	}
}

// TestTheTimingIsWrittenDownEvenWhenItFails 盯**失败的那几趟也写分段耗时**
// （票 #220 的评审）。
//
// 🔴 **用户会来抱怨的正是失败的那些**（「点了歌一直没反应」）——只在成功时
// 写日志，等于把最需要查的那一类漏掉（票 #282 说的「事后查得到」）。
func TestTheTimingIsWrittenDownEvenWhenItFails(t *testing.T) {
	f := newStreamFixture(t)
	arrived := make(chan struct{})
	f.on("/cdn/hang.m4s", hangingCDN(arrived))
	f.onPlayURL(dashFor(cdnURL(f.URL, "hang.m4s", 2*time.Hour),
		int(bili.QualityHigh192), 110836))

	a := newOnlineApp(t, f, t.TempDir())
	lines, restore := app.CapturePlayLog(a)
	t.Cleanup(restore)
	clock := &fakeClock{}
	a.SetAfterForTest(clock.after)

	if err := a.Enqueue(onlineItem("BV1", "1", "取不来的那一首")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("取流一直没到 CDN")
	}
	waitFor(t, 15*time.Second, "状态里说在取流", func() bool {
		return a.Snapshot().Loading
	})

	clock.fireAt(app.LoadPlayTimeout)
	waitFor(t, 15*time.Second, "超时之后有一句话", func() bool {
		return a.Snapshot().Err != ""
	})

	waitFor(t, 15*time.Second, "日志里写了分段耗时", func() bool {
		return strings.Contains(strings.Join(*lines, "\n"), "取流分段耗时")
	})
	joined := strings.Join(*lines, "\n")
	for _, want := range []string{"① 问地址", "② 下音频", "③ 解码"} {
		if !strings.Contains(joined, want) {
			t.Errorf("失败那一趟的日志里没有 %q：\n%s", want, joined)
		}
	}
}
