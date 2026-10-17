package app_test

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// 这一组盯的是**断网与恢复在界面那一层的表现**。
//
// 机制本身（退避、缓冲态、预算上限）在
// `internal/service/audio/stream_source_wait_test.go` 里测——那里能把
// 「下到第 N 字节时网络断了」精确造出来。这里测的是它有没有被正确地
// **接进状态**：缓冲态有没有冒到 `State` 上、断网期间有没有被误报成错误、
// 恢复之后位置有没有倒回去、切歌会不会留下东西。
//
// # 为什么断「位置不许倒退」而不是「有没有报错」
//
// 从头播一遍这个 bug 看起来**完全正常**：歌在响、界面没有红字、进度条照走。
// 只有拿位置去量才发现回到了 0。而用户在意的也正是这个——重放一遍意味着
// 他要在同一首歌上再等一遍。

// newBufferApp 建一个缓冲预算很宽的 App。
//
// 预算给得宽是刻意的：这一组要验的是「恢复」，而「等多久才放弃」是
// 另一个决定（默认一分钟）。给窄了歌会在恢复之前就被判死，量到的是
// 「报错」不是「缓冲」。
func newBufferApp(t *testing.T, f *streamFixture, dir string) *app.App {
	t.Helper()
	a := app.New()
	t.Cleanup(a.Close)
	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	a.MountStreaming(app.Streaming{
		Client: cli, Media: f.Client(), TempDir: dir, MaxWait: 30 * time.Second,
	})
	return a
}

// TestAnOutageShowsBufferingAndResumesFromTheSamePosition 盯主线。
//
// 断网 → 界面亮缓冲态、**没有错误** → 网络回来 → **位置不许倒退**。
//
// 采样窗口必须在断网**之前**就开始，而且从歌刚播起来就开始。反过来写
// （恢复之后才采样、断言「别掉回 0」）量到的是一个空窗口——歌从头播一遍
// 那个 bug 看起来完全正常，只有「不许倒退」才量得出来。
func TestAnOutageShowsBufferingAndResumesFromTheSamePosition(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	// ⚠️ 必须先把下载**卡在半路**，否则整首歌在本地毫秒级就下完了，
	// 等测试想断网时早下完了——缓冲态一次都没亮过，用例却是绿的。
	f.setFailAfter(len(f.media) / 2)
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh320), 110836))

	a := newBufferApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "会断网的一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "会断网的一首")
	waitFor(t, 30*time.Second, "播放位置走起来", func() bool {
		return a.Snapshot().Position > 700*time.Millisecond
	})

	// 采样在它自己的 goroutine 里跑，**切片只归它所有**：收尾时用一个
	// channel 交回来。共享变量 + 一个 bool 收尾是最容易写出数据竞争的
	// 那种写法（`-race` 一跑就红，而且红的还是测试而不是产品代码）。
	stop := make(chan struct{})
	collected := make(chan []time.Duration, 1)
	problems := make(chan string, 8)
	go func() {
		var out []time.Duration
		wasBuffering := false
		for {
			st := a.Snapshot()
			if st.Position > 0 {
				out = append(out, st.Position)
			}
			switch {
			case st.Buffering && st.Err != "":
				select {
				case problems <- "缓冲和错误同时出现：" + st.Err:
				default:
				}
			case st.Buffering:
				wasBuffering = true
			case wasBuffering && st.Err == "":
				// 缓冲结束而且没有错误 = 网络恢复、接着放了。这一段是主线的
				// 目的本身，少了它这条用例就只是「停了会儿」。
				wasBuffering = false
			}
			select {
			case <-stop:
				collected <- out
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	}()

	// 断网：卡在半路的那个请求死掉，之后的请求全是黑的。
	f.setDark(true)
	close(f.mediaGate)
	waitFor(t, 30*time.Second, "播放条上出现缓冲态", func() bool {
		return a.Snapshot().Buffering
	})
	// 缓冲着的时候**不许有错误**：断网不是失败，用户看到红色第一反应是
	// 「歌坏了」，于是去清缓存重下——而正确的反应是等一等。
	if got := a.Snapshot().Err; got != "" {
		t.Errorf("缓冲着就有错误了：%q", got)
	}

	// ⚠️ 先等播放位置**停稳**再开始量「没有字节就不该往前走」。
	//
	// MyGo 的解码器会提前解一秒多的音（`mygo/audio/source.go` 里那个一秒的
	// 环），所以缓冲刚开始的那一秒位置本来就在走——那是存货放完，不是 bug。
	//
	// 而判「停稳」的窗口**必须比那个环还长**：短于它就会抓到两次补货之间的
	// 一瞬（环刚好排空、下一批字节还没到），量成「位置又在走了」。
	// 500ms 的窗口栽过这一下——满负载下跑十次有一次红在
	// 「1.068s → 1.482s」，差的 414ms 就是环里剩下的存货。
	var settled time.Duration
	waitFor(t, 60*time.Second, "缓冲期间播放位置停稳", func() bool {
		p1 := a.Snapshot().Position
		if p1 == 0 {
			return false
		}
		time.Sleep(1600 * time.Millisecond)
		if a.Snapshot().Position == p1 {
			settled = p1
			return true
		}
		return false
	})

	// 停稳之后不许再走。没有新字节，解码器就没有新样本。
	// 连续采两秒：整个窗口里一次都不许动，不给它「动一点点」的机会。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := a.Snapshot().Position; got != settled {
			t.Fatalf("缓冲期间位置还在走：%v → %v（没有字节进来，不该往前走）",
				settled.Round(time.Millisecond), got.Round(time.Millisecond))
		}
		time.Sleep(200 * time.Millisecond)
	}
	if a.Snapshot().Err != "" {
		t.Errorf("缓冲着就有错误了：%q", a.Snapshot().Err)
	}

	// 网络回来。顺手把「只发一半」也撤掉——那是断网时用的机关。
	f.setFailAfter(0)
	f.setDark(false)

	// ⚠️ 恢复的判据是**缓冲态解除**，不是「位置往前了」。
	// 反过来会立刻成立：解码器手里还有存货，即使一个字节都没补上，
	// 位置也会先往前走一小段——那正是「以为恢复了其实没有」的样子。
	waitFor(t, 60*time.Second, "网络恢复之后缓冲态解除", func() bool {
		return !a.Snapshot().Buffering
	})
	// ⚠️ 缓冲态解除之后位置**不会立刻**往前走：字节刚到，解码器得先解
	// 出来，播放输出还要把它按真实时间放完（一秒左右）。所以这里要**等**，
	// 不是一比就下结论。
	waitFor(t, 30*time.Second, "从断点接着往前走", func() bool {
		return a.Snapshot().Position > settled
	})
	time.Sleep(2 * time.Second)
	close(stop)
	samples := <-collected

	for _, msg := range drain(problems) {
		t.Error(msg)
	}
	if len(samples) < 20 {
		t.Fatalf("只采到 %d 个位置，验不了什么", len(samples))
	}
	// 位置不许倒退。相邻两次采样之间最多走了 50ms，所以给 400ms 的余量。
	for i := 1; i < len(samples); i++ {
		if got, prev := samples[i], samples[i-1]; got < prev-400*time.Millisecond {
			t.Fatalf("第 %d 次采样位置从 %v 倒回 %v——这是「从头开始」，"+
				"不是「从断点继续」", i,
				prev.Round(time.Millisecond), got.Round(time.Millisecond))
		}
	}
	if st := a.Snapshot(); st.Title != "会断网的一首" || st.Err != "" {
		t.Errorf("恢复之后在放的变成了 %q，错误 %q", st.Title, st.Err)
	}
}

// TestSwitchingTracksWhileBufferingLeavesNothingBehind 盯「缓冲中的切歌不留东西」。
//
// 退避最长 8 秒。源不等退避睡醒就退出的话，切歌会假死，而缓存文件要等
// 睡醒才删——连点五首就是五个下载 goroutine 和五个临时文件留在那儿。
//
// 断的是三样：缓存文件、下载 goroutine、看门狗。少一样都会在「断网之后
// 快速换歌」之后显形。
func TestSwitchingTracksWhileBufferingLeavesNothingBehind(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.setFailAfter(len(f.media) / 2)
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh320), 110836))

	dir := t.TempDir()
	a := newBufferApp(t, f, dir)
	if err := a.Enqueue(onlineItem("BV1", "1", "断网的那一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "断网的那一首")

	// 先让它真的缓冲起来：那时候切歌才是「从缓冲里切走」。
	f.setDark(true)
	close(f.mediaGate)
	waitFor(t, 30*time.Second, "播放条上出现缓冲态", func() bool {
		return a.Snapshot().Buffering
	})

	for i := range 5 {
		if err := a.PlayFrom(onlineItem("BV1", strconv.Itoa(i), "第 "+strconv.Itoa(i+1)+" 首")); err != nil {
			t.Fatal(err)
		}
		// 让前几首有机会进缓冲态——那才是这条用例最难受的形态。
		time.Sleep(120 * time.Millisecond)
	}

	// 网络回来，让最后一首真的播起来：确认链路是好的，不只是没崩。
	f.setFailAfter(0)
	f.setDark(false)
	waitFor(t, 60*time.Second, "最后一首开始播", func() bool {
		st := a.Snapshot()
		return st.Title == "第 5 首" && st.Err == ""
	})

	a.Close()
	waitFor(t, 20*time.Second, "缓存文件被清干净", func() bool {
		entries, err := os.ReadDir(dir)
		return err == nil && len(entries) == 0
	})
	// ⚠️ **不用 `runtime.NumGoroutine()` 断这一条**：fixture 的 HTTP 服务器
	// 每条连接都留一个 goroutine，量的是它不是我们。
	waitFor(t, 20*time.Second, "被打掉的下载 goroutine 都退干净", func() bool {
		return liveStreamWorkers() == 0
	})
}

// drain 把一个 channel 里已经积着的东西全取出来。
func drain[T any](ch chan T) []T {
	var out []T
	for {
		select {
		case v := <-ch:
			out = append(out, v)
		default:
			return out
		}
	}
}

// TestAStreamThatNeverStartsIsNotASilentPlaybar 盯**取流之后、播放器之前**那一段。
//
// # 这个洞是怎么来的
//
// `audio.OpenStream` 会去 CDN 探容器：`mygoaudio.NewDecoder` 的 `io.ReadFull`
// 等头 16 个字节，字节下不来就一直阻塞。而 `a.online` 要等建完才装上，
// `syncBuffering` 读的就是它——于是那一整段里界面上是**三样全空**：
// 没有歌名（还没装上）、没有错误（没失败）、没有缓冲态（读不到）。
//
// 本票把源的下限从「换几次流就报错」放宽成「最多等一分钟」，顺手把这段
// 从几十毫秒的空窗拉成了一分钟。评审实测：生产默认下「三者全空」累计 55.5s，
// 缓冲态一次都没亮过。
func TestAStreamThatNeverStartsIsNotASilentPlaybar(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	// 取流接口照常返回，CDN **头给正文掐断**——`OpenStream` 就卡在这儿。
	f.setDark(true)
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh320), 110836))

	a := app.New()
	t.Cleanup(a.Close)
	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	// 缓冲预算给得短：一分钟是产品决定，测试不该为它等一分钟。
	a.MountStreaming(app.Streaming{
		Client: cli, Media: f.Client(), TempDir: t.TempDir(), MaxWait: 4 * time.Second,
	})

	if err := a.Enqueue(onlineItem("BV1", "1", "下不来的那一首")); err != nil {
		t.Fatal(err)
	}

	// ⚠️ 第一段：点了之后**很快**就该有话说。
	//
	// 断的是「两秒之内」，不是「有没有」：只有信号通路没有回音的话，
	// 这个 bug 会在 55 秒之后自己好起来，而那正是要修的东西。
	waitFor(t, 2*time.Second, "播放条上有话说", func() bool {
		st := a.Snapshot()
		return st.Title == "下不来的那一首" && st.Buffering && st.Err == ""
	})

	// `Opening` 单独看一眼：它是「还在建播放链路」那个**独立**信号——
	// 界面靠它把播放键按灰（此刻播放器还不存在，按下去什么都不会发生）。
	if !a.Snapshot().Opening {
		t.Error("一个采样都还没出来，Opening 却是 false——" +
			"播放键会亮成「按下去没反应」的样子")
	}

	// 第二段：缓冲不是失败——预算没用完之前不能冒错误。
	time.Sleep(2 * time.Second)
	if st := a.Snapshot(); st.Err != "" {
		t.Fatalf("缓冲着就有错误了：%q", st.Err)
	} else if !st.Buffering {
		t.Fatalf("缓冲态没了（歌名 %q，Opening=%v）", st.Title, st.Opening)
	}

	// 第三段：预算用完，给一句人话，而且缓冲态要跟着灭。
	waitFor(t, 20*time.Second, "缓冲预算用完之后给一句人话", func() bool {
		return a.Snapshot().Err != ""
	})
	st := a.Snapshot()
	if st.Buffering {
		t.Error("已经报错了缓冲态还亮着——界面上会一边「正在恢复」一边「打不开」")
	}
	if st.Title != "下不来的那一首" {
		t.Errorf("报错之后歌名丢了：%q——那句话会没有主语", st.Title)
	}
}

// TestTheTrackYouClickedIsTheOneThatBuffers 盯换歌时**不留上一首的歌名**。
//
// 用户在放 A，点了一首在线的 B，而 B 的字节下不来。界面上要是还写着 A
// 配一句「缓冲中……」，用户会以为**A** 在缓冲——而他明明刚点了 B。
func TestTheTrackYouClickedIsTheOneThatBuffers(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh320), 110836))

	a := newBufferApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "先在放的一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "先在放的一首")

	// 现在让 CDN 闭嘴，再点一首别的。
	f.setDark(true)
	if err := a.PlayFrom(onlineItem("BV1", "2", "点的那一首")); err != nil {
		t.Fatal(err)
	}

	waitFor(t, 10*time.Second, "歌名换成刚点的那一首", func() bool {
		return a.Snapshot().Title == "点的那一首"
	})
	if st := a.Snapshot(); !st.Buffering {
		t.Errorf("歌名换过来了却没有缓冲态：%+v", st)
	}
}
