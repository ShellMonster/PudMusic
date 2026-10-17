package app_test

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// 这一组测的是**接线**：取流换来的东西有没有被正确地接进播放，
// 三条换流路径在应用这一层是不是都还成立。
//
// 换流的机制本身（续传、备用地址轮换、长度比对）在
// `internal/service/audio/stream_source_test.go` 里测——那里能把
// 「下到第 N 字节时地址失效」精确造出来。这里测的是它被接对了没有：
// 代次、音质徽章、错误怎么冒到界面上、凭据边界有没有被绕过。
//
// 断网与恢复在 `online_buffer_test.go` 里（它是这一组里唯一会等网络的那种）。

// ─────────────────────────────────────────────────────────────────────────────
// 能播
// ─────────────────────────────────────────────────────────────────────────────

// TestOnlineItemActuallyPlays 盯「在线条目能播」这件事真的成立。
//
// 完整的一条链：队列 → 取流 → CDN → 字节源 → flacdec → MyGo 播放器。
// 断言的是解码器真出了样本（位置在走），不是「没报错」。
func TestOnlineItemActuallyPlays(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh320), 110836))

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1xx", "123", "在线的一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "在线的一首")

	waitFor(t, 15*time.Second, "进度条往前走", func() bool {
		return a.Snapshot().Position > 0
	})

	st := a.Snapshot()
	if st.Err != "" {
		t.Errorf("播放条上有错：%s", st.Err)
	}
	if st.Quality != "320K" {
		t.Errorf("音质徽章 = %q，want 320K", st.Quality)
	}
	if st.Path != "" {
		t.Errorf("在线条目不该有本地路径，却有 %q", st.Path)
	}
	if n := f.playCalls.Load(); n != 1 {
		t.Errorf("取流接口被打了 %d 次，want 1", n)
	}
}

// TestLocalItemHasNoQualityBadge 盯本地文件不画音质徽章。
//
// 那是文件格式（FLAC / AAC），不是「音质」——拿格式凑一个徽章会让
// 「无损」两个字在两个意思之间飘。
func TestLocalItemHasNoQualityBadge(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	if err := a.Open(flac(t)); err != nil {
		t.Fatal(err)
	}
	if q := a.Snapshot().Quality; q != "" {
		t.Errorf("本地文件的音质徽章 = %q，应该为空", q)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 路径 A：播放前预判
// ─────────────────────────────────────────────────────────────────────────────

// TestStaleStreamIsRefetchedBeforePlaying 盯「播放前预判」那条路。
//
// 第一次放，拿到一条**已经越过过期线**的地址；第二次放同一首：必须重新取流，
// 而且不能再去碰那条旧的 CDN 地址——那正是「点一下没声音」（CDN 回 403）
// 而界面上什么提示都没有的那条路。
//
// 不需要真的等两小时过期：`Usable` 留了两分钟提前量，一条「一分钟后过期」
// 的地址从拿到那一刻起就已经算过期了（提前量是规格里的规则，不是测试造出来的）。
func TestStaleStreamIsRefetchedBeforePlaying(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)

	stale := cdnURL(f.URL, "stale.flac", time.Minute)
	fresh := cdnURL(f.URL, "fresh.flac", 2*time.Hour)
	f.forbidden["/cdn/stale.flac"] = true // 旧地址已经失效了
	f.onPlayURL(dashFor(stale, int(bili.QualityHigh320), 110836))
	f.onPlayURL(dashFor(fresh, int(bili.QualityMid128), 192000))

	a := newOnlineApp(t, f, t.TempDir())
	it := onlineItem("BV1", "1", "地址会过期的那首")

	if err := a.PlayFrom(it); err != nil {
		t.Fatal(err)
	}
	// 用 >= 而不是 ==：轮询有 10 ms 的间隔，第二次取流可能已经发生，
	// 写死 == 1 会永远等不到（这类 flaky 测起来最费时间）。
	waitFor(t, 30*time.Second, "第一次取流（哪怕拿到的是过期地址）", func() bool {
		return f.playCalls.Load() >= 1
	})
	// 换成本地，切走。
	if err := a.PlayFrom(app.LocalItem(flac(t))); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, "切回本地", func() bool { return a.Snapshot().Title == "stereo16" })

	if err := a.PlayFrom(it); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "地址会过期的那首")

	if n := f.playCalls.Load(); n < 2 {
		t.Errorf("过期之后没有重新取流（接口只被打了 %d 次）", n)
	}
	if got := a.Snapshot().Quality; got != "128K" {
		t.Errorf("徽章 = %q，want 128K（第二次拿到的是新地址对应的档位）", got)
	}
}

// TestStillValidStreamIsReusedBeforePlaying 盯没过期就别多打一次接口。
//
// 取流要带 WBI 签名，是一次实打实的往返。省下来的每一毫秒都是「点歌到出声」
// 的时间——而那正是这个应用最贵的一段。
func TestStillValidStreamIsReusedBeforePlaying(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh320), 110836))

	a := newOnlineApp(t, f, t.TempDir())
	it := onlineItem("BV1", "1", "还新鲜的那首")

	for i := range 2 {
		if err := a.PlayFrom(it); err != nil {
			t.Fatal(err)
		}
		playing(t, a, "还新鲜的那首")
		if err := a.PlayFrom(app.LocalItem(flac(t))); err != nil {
			t.Fatal(err)
		}
		waitFor(t, 10*time.Second, "切回本地", func() bool { return a.Snapshot().Title == "stereo16" })
		_ = i
	}
	if n := f.playCalls.Load(); n != 1 {
		t.Errorf("取流接口被打了 %d 次，want 1（没过期就不该重新取）", n)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 路径 B/C：报错后换流 + 从原位置继续
// ─────────────────────────────────────────────────────────────────────────────

// TestStreamCutMidwayRefetchesAndKeepsPlaying 盯「换流后从原位置继续」。
//
// CDN 下到一半把连接掐了：源要换一条新地址**从同一个字节偏移接着下**，
// 播放不中断、位置不回退、界面上没有错。
//
// 「位置没回退」是用播放位置直接量的——它只能往前走。
func TestStreamCutMidwayRefetchesAndKeepsPlaying(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.failMediaAfter = len(f.media) / 3

	// 两条地址指向**同一段字节**：B 站换流给的就是同一段。
	f.onPlayURL(dashFor(cdnURL(f.URL, "v1.flac", time.Hour), int(bili.QualityHigh320), 110836))
	f.onPlayURL(dashFor(cdnURL(f.URL, "v2.flac", time.Hour), int(bili.QualityHigh320), 110836))

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "会被掐断的一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "会被掐断的一首")

	// 先让播放真的走起来，再放行掐断。
	waitFor(t, 15*time.Second, "播放位置动起来", func() bool {
		return a.Snapshot().Position > 0
	})
	waitFor(t, 15*time.Second, "下到断点", func() bool { return f.mediaHits.Load() >= 1 })
	before := a.Snapshot().Position
	close(f.mediaGate)

	waitFor(t, 30*time.Second, "换流之后继续往前播", func() bool {
		return a.Snapshot().Position > before
	})
	if st := a.Snapshot(); st.Err != "" {
		t.Errorf("换流之后播放条上有错：%s", st.Err)
	}
	if n := f.playCalls.Load(); n < 2 {
		t.Errorf("地址掐断了却没有换流（取流接口只打了 %d 次）", n)
	}
	if q := a.Snapshot().Quality; q != "320K" {
		t.Errorf("换流之后徽章 = %q，want 320K", q)
	}
}

// TestQualityBadgeFollowsTheRefresh 盯「换流后徽章跟着更新」。
//
// 换流拿回来的可能是**降级过**的那一档。用户得看见——无声地降级会让人
// 以为自己还在听无损。
func TestQualityBadgeFollowsTheRefresh(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.failMediaAfter = len(f.media) / 3

	f.onPlayURL(dashFor(cdnURL(f.URL, "v1.flac", time.Hour), int(bili.QualityHigh320), 110836))
	f.onPlayURL(dashFor(cdnURL(f.URL, "v2.flac", time.Hour), int(bili.QualityMid128), 192000))

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "会降级的一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "会降级的一首")
	if q := a.Snapshot().Quality; q != "320K" {
		t.Fatalf("一开始的徽章 = %q，want 320K", q)
	}
	waitFor(t, 15*time.Second, "播放位置动起来", func() bool { return a.Snapshot().Position > 0 })
	close(f.mediaGate)

	waitFor(t, 30*time.Second, "换流之后徽章降级", func() bool {
		return a.Snapshot().Quality == "128K"
	})
}

// TestAutoPrefersLossless 盯「自动档优先无损」。
func TestAutoPrefersLossless(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	url := cdnURL(f.URL, "a.flac", 2*time.Hour)
	f.onPlayURL(map[string]any{
		"duration": 1,
		"audio": []any{map[string]any{
			"id": int(bili.QualityHigh320), "bandwidth": 110836,
			"codecs": "mp4a.40.2", "mimeType": "audio/mp4", "baseUrl": url,
		}},
		"flac": map[string]any{"display": true, "audio": map[string]any{
			"id": int(bili.QualityFLAC), "bandwidth": 1_400_000,
			"codecs": "flac", "mimeType": "audio/mp4", "baseUrl": url,
		}},
	})

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "有无损的一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "有无损的一首")
	if q := a.Snapshot().Quality; q != "无损 FLAC" {
		t.Errorf("徽章 = %q，want 无损 FLAC（有 flac 段时自动档必须先拿它）", q)
	}
}

// TestStreamChangedReopensFromTheSamePosition 盯规格里点名「这条是重点」的那条：
// **换流后从原来播放的位置继续，不是从头开始。**
//
// 触发方式是「换了另一条流」——B 站给回来的新流和旧的不是同一段字节
// （这里靠后缀不同造出来，`sameContainer` 判它们不是同一条）。字节偏移对不上，
// 所以字节源拒绝硬接并报 `audio.ErrStreamChanged`；这时候能从原位置接着播的
// 只有**播放位置**（时间），而只有 App 知道上一个位置是多少——所以这一层
// 负责：关掉重开 + `Seek` 跳回去。
//
// 断的是「位置不许倒退」，不是「有没有报错」：从头播一遍这个功能看起来
// **完全正常**（歌在响、没有红字），只有拿位置去量才发现。
func TestStreamChangedReopensFromTheSamePosition(t *testing.T) {
	f := newStreamFixture(t)
	data := readFlac(t)
	f.media = data
	// 一半：够歌播上一阵子（有东西可以「保持」），又留得下后半截让泵断在中间。
	f.failMediaAfter = len(data) / 2

	// v1 是 .flac，v2 是 .m4s —— 不同容器，源会拒绝把它们接在一起。
	f.onPlayURL(dashFor(cdnURL(f.URL, "v1.flac", time.Hour), int(bili.QualityHigh320), 110836))
	f.onPlayURL(dashFor(cdnURL(f.URL, "v2.m4s", time.Hour), int(bili.QualityHigh320), 110836))

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "换了流的一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "换了流的一首")

	// 等歌真的播起来了才开始采样。
	//
	// ⚠️ 采样窗口**必须在放行掐断之前**就开始，而断言是「窗口内位置单调不减」。
	// 反过来写（「重开之后采样，别掉回 0」）会量到一个空窗口——歌从头播一遍
	// 这个 bug 看起来完全正常，只有「不许倒退」才量得出来。
	waitFor(t, 30*time.Second, "播放位置走起来", func() bool {
		return a.Snapshot().Position > 700*time.Millisecond
	})

	// 采样在它自己的 goroutine 里跑，**切片只归它所有**：收尾时用一个
	// channel 交回来。用共享变量 + 一个 bool 收尾是最容易写出数据竞争的那种
	// 写法（`-race` 一跑就红，而且红的还是测试而不是产品代码）。
	stop := make(chan struct{})
	collected := make(chan []time.Duration, 1)
	go func() {
		var out []time.Duration
		for {
			if p := a.Snapshot().Position; p > 0 {
				out = append(out, p)
			}
			select {
			case <-stop:
				collected <- out
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	}()

	// 放行掐断：泵看到连接断了 → 换流 → 容器对不上 → ErrStreamChanged
	// → checkPlayback → refreshOnlineNow。
	close(f.mediaGate)

	// 等重开真的发生了：CDN 上第二条流（.m4s）被取过，且取流接口打过第二次。
	waitFor(t, 30*time.Second, "重开时取了第二条流", func() bool {
		return f.playCalls.Load() >= 2
	})
	time.Sleep(3 * time.Second)
	close(stop)
	samples := <-collected

	if len(samples) < 10 {
		t.Fatalf("只采到 %d 个位置，验不了什么：%v", len(samples), samples)
	}
	// 位置不许倒退。相邻两次采样之间最多只走了 50ms，所以给 400ms 的
	// 余量（seek 落点按采样帧算，会差几帧）。
	for i := 1; i < len(samples); i++ {
		if got, prev := samples[i], samples[i-1]; got < prev-400*time.Millisecond {
			t.Fatalf("第 %d 次采样位置从 %v 倒回 %v——这是「从头开始」，"+
				"不是「从原来的位置继续」", i, prev.Round(time.Millisecond), got.Round(time.Millisecond))
		}
	}
	if samples[0] < 500*time.Millisecond {
		t.Errorf("开始采样时歌才放到 %v，还不够证明「从原位置继续」", samples[0].Round(time.Millisecond))
	}

	if a.Snapshot().Title != "换了流的一首" {
		t.Errorf("重开之后在放的歌变了：%q", a.Snapshot().Title)
	}
	if last := samples[len(samples)-1]; last < samples[0] {
		t.Errorf("采样结束时 %v 比开始时 %v 还小", last, samples[0])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 错误怎么变成人话
// ─────────────────────────────────────────────────────────────────────────────

// TestBStationErrorBecomesASentence 盯「取流失败不是空白」。
//
// B 站的错误码是数字，直接显示出来用户什么也不知道。这一条钉住：界面上
// 出现的是一句能照着做的话。
func TestBStationErrorBecomesASentence(t *testing.T) {
	f := newStreamFixture(t)
	f.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		writeStreamJSON(w, map[string]any{"code": -101, "message": "账号未登录", "ttl": 1})
	})
	a := newOnlineApp(t, f, t.TempDir())

	// 在线条目是异步开的，拿不到同步返回值——错走播放条上来。
	_ = a.Enqueue(onlineItem("BV1", "1", "登不上的那一首"))

	waitFor(t, 15*time.Second, "播放条上出现一句人话", func() bool {
		return a.Snapshot().Err != ""
	})
	got := a.Snapshot().Err
	if got == "账号未登录" {
		t.Errorf("用的是 B 站的原话：%q", got)
	}
	if !strings.Contains(got, "登录") {
		t.Errorf("话里没有说下一步做什么：%q", got)
	}
}

// TestNoStreamSaysWhy 盯「查得到但没得放」也有话说。
func TestNoStreamSaysWhy(t *testing.T) {
	f := newStreamFixture(t)
	f.on("/x/player/wbi/playurl", func(w http.ResponseWriter, r *http.Request) {
		writeStreamJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{"timelength": 1, "dash": map[string]any{"duration": 1}}})
	})
	a := newOnlineApp(t, f, t.TempDir())

	_ = a.Enqueue(onlineItem("BV1", "1", "没有音频的一首"))
	waitFor(t, 15*time.Second, "播放条上解释为什么放不了", func() bool {
		return a.Snapshot().Err != ""
	})
	if !strings.Contains(a.Snapshot().Err, "音频") {
		t.Errorf("话里没有说清是「没有音频」：%q", a.Snapshot().Err)
	}
}

// TestUnplayableOnlineItemsAreRefusedSynchronously 盯「参数不全当场拒」。
//
// 这两种是同步就能看出来的，调用方（PlayFrom / Enqueue）靠返回值把队列滚回去。
// 放它们进去的话，用户会看到一个永远打不开的条目躺在队列里。
func TestUnplayableOnlineItemsAreRefusedSynchronously(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	for _, it := range []app.Item{
		{Source: "online", Type: "mv", Bvid: "BV1", Title: "缺 cid"},
		{Source: "online", Type: "mv", Cid: "1", Title: "缺 bvid"},
		{Source: "online", Type: "audio", Sid: "42", Title: "音乐库那条"},
	} {
		err := a.PlayFrom(it)
		if err == nil {
			t.Errorf("%s 居然放进去了", it.Title)
			continue
		}
		if !strings.Contains(err.Error(), "还没接") && !strings.Contains(err.Error(), "缺") {
			t.Errorf("%s 的错没说要紧：%v", it.Title, err)
		}
		if a.Queue().Len() != 0 {
			t.Errorf("放不进去的那一首留在了队列里：%d 项", a.Queue().Len())
		}
	}
}

// TestStreamingNotMountedSaysSo 盯「没挂取流」别静默。
func TestStreamingNotMountedSaysSo(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)
	err := a.PlayFrom(onlineItem("BV1", "1", "还没挂取流"))
	if err == nil || !strings.Contains(err.Error(), "在线播放") {
		t.Errorf("err = %v，want 一句说清「在线播放还没接上」的话", err)
	}
}

// TestMidPlaybackDeathSurfacesOnThePlaybar 盯规格里的负路径「播放中断网」。
//
// `PLAN.md` 阶段 2 的负路径表里写着：「播放中断网——**不能被当成播完**——
// 要停在缓冲态并提示」。这条钉的就是它，而且它现在有**两段**：
//
//	先亮缓冲态  断网不是失败。歌停在原地、网络回来接着放
//	再给一句人话  缓冲预算用完了还是下不动，才说明白为什么
//
// ⚠️ 只有第二段的话，界面就跟没修一样：断网之后用户看到的是「不动，
// 什么也没说」，那和卡死没有区别。
//
// 这条用例是**评审逼出来的**：它自己写了同名探针才发现
// `App.tick()` 里根本没调 `checkPlayback`，而当时所有测试都是绿的——
// 因为它们走的是 `failOnline`（同步那条路），不经过 tick。
func TestMidPlaybackDeathSurfacesOnThePlaybar(t *testing.T) {
	f := newStreamFixture(t)
	data := readFlac(t)
	f.media = data
	alive := len(data) / 2 // 只发一半就掐断

	f.on("/cdn/a.flac", func(w http.ResponseWriter, r *http.Request) {
		start := rangeStartOf(r)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)-start))
		if start > 0 {
			w.Header().Set("Content-Range",
				"bytes "+strconv.Itoa(start)+"-"+strconv.Itoa(len(data)-1)+"/"+strconv.Itoa(len(data)))
			w.WriteHeader(http.StatusPartialContent)
		}
		w.Write(data[start:alive])
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	})
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh320), 110836))

	// 缓冲预算给得比默认短：这一条要验的是「缓冲之后**确实会给一句交代**」，
	// 而那一段的时长是产品决定（默认一分钟），不该让测试等那么久。
	a := app.New()
	t.Cleanup(a.Close)
	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	a.MountStreaming(app.Streaming{
		Client: cli, Media: f.Client(), TempDir: t.TempDir(), MaxWait: 8 * time.Second,
	})

	if err := a.Enqueue(onlineItem("BV1", "1", "半路死掉的一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "半路死掉的一首")

	// ⚠️ **采样必须在任何 waitFor 之前就起来**。
	//
	// 缓冲窗口只有七八秒，而「等播放停住」那个 waitFor 在满负载的机器上
	// （`-race` + 全包并行）可能要几十秒才通过。等它返回了再开始采���的话，
	// 缓冲早开早关，一条记录都收不到——而用例仍然是绿的。
	var seen playbackWatch
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			st := a.Snapshot()
			if st.Buffering && st.Err == "" {
				seen.markBuffering()
			}
			if st.Err != "" {
				seen.markSentence(st.Err)
			}
			select {
			case <-done:
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}()
	defer func() { close(done); <-stopped }()

	// 等到它真的停：位置不再前进。
	waitFor(t, 120*time.Second, "播放停住", func() bool {
		p1 := a.Snapshot().Position
		if p1 == 0 {
			return false
		}
		time.Sleep(700 * time.Millisecond)
		return a.Snapshot().Position == p1
	})

	// ⚠️ 两段是**先后**出现的，而且中间那段「有缓冲态但没有错误」不能被跳过。
	// 分成两次 `waitFor` 是不行的：缓冲窗口只有几秒，第一次等到的时候
	// 第二段可能已经过去了（或者反过来），于是顺序错了也看不出来。
	waitFor(t, 60*time.Second, "缓冲态和那句人话都出现", func() bool {
		buf, sentence := seen.get()
		return buf && sentence != ""
	})
	buf, sentence := seen.get()
	if !buf {
		t.Error("从头到尾没亮过缓冲态——断网和卡死在界面上长得一模一样")
	}
	if a.Snapshot().Buffering {
		t.Error("已经报错了缓冲态还亮着——界面上会一边「正在恢复」一边「打不开」")
	}

	// ⚠️ 必须是「拉不动了」而不是解码器那边的抱怨。字节断了之后 flacdec
	// 先报的是「这一帧的 CRC 对不上」——把**后果**说给用户听，用户会以为
	// 歌坏了、缓存坏了，于是去清缓存重下，而正确的反应是「换个网」。
	if strings.Contains(sentence, "CRC") || strings.Contains(sentence, "flacdec") {
		t.Errorf("说的是解码器的抱怨，不是真正的原因：%q", sentence)
	}
	if !strings.Contains(sentence, "连不上") {
		t.Errorf("话没说清是「拉不动了」：%q", sentence)
	}
}

// TestDeadStreamFromTheStartSaysSo 盯「地址一上来就是死的」。
//
// CDN 立刻 403（过期、被换掉、风控）——连一个字节都没下到。这时播放条上
// 同样要有话，而且是「拉不动」而不是空白。
func TestDeadStreamFromTheStartSaysSo(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.forbidden["/cdn/a.flac"] = true
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh320), 110836))

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "一开始就不行的一首")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 30*time.Second, "播放条上出现一句人话", func() bool {
		return a.Snapshot().Err != ""
	})
	if got := a.Snapshot().Err; got == "" {
		t.Error("话是空的")
	}
}

// rangeStartOf 读 Range 头里的起点，没有就是 0。
func rangeStartOf(r *http.Request) int {
	h := r.Header.Get("Range")
	if !strings.HasPrefix(h, "bytes=") {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(h, "bytes="), "-"))
	return n
}

// ─────────────────────────────────────────────────────────────────────────────
// 播放代次
// ─────────────────────────────────────────────────────────────────────────────

// TestOnlyTheLastTrackWinsWhenSwitchingFast 盯代次守卫。
//
// 连点五首：前四首的取流结果回来时发现代次已经变了，直接关掉走人。
// 不挡的话，第 1 首的取流慢一点回来就会把第 5 首顶掉——用户点的是最后一首，
// 听到的是第一首，而界面上没有任何解释。
//
// 顺带盯「不留东西」：缓存文件、goroutine。
func TestOnlyTheLastTrackWinsWhenSwitchingFast(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh320), 110836))

	dir := t.TempDir()
	a := app.New()
	t.Cleanup(a.Close)
	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	a.MountStreaming(app.Streaming{Client: cli, Media: f.Client(), TempDir: dir})

	for i := range 5 {
		if err := a.PlayFrom(onlineItem("BV1", strconv.Itoa(i), fmt.Sprintf("第 %d 首", i+1))); err != nil {
			t.Fatal(err)
		}
		// 让前几首的取流有机会回来、但被代次挡掉。
		time.Sleep(15 * time.Millisecond)
	}

	waitFor(t, 30*time.Second, "最后一首开始播", func() bool {
		return a.Snapshot().Title == "第 5 首"
	})
	// 多等一会儿，确认没有迟到的那一首把状态改回去。
	time.Sleep(300 * time.Millisecond)
	if got := a.Snapshot().Title; got != "第 5 首" {
		t.Errorf("最后在播的是 %q，want 第 5 首（迟到的结果覆盖了新曲）", got)
	}

	a.Close()
	waitFor(t, 15*time.Second, "缓存文件被清干净", func() bool {
		entries, err := os.ReadDir(dir)
		return err == nil && len(entries) == 0
	})
	// ⚠️ **不用 `runtime.NumGoroutine()` 断这一条**：fixture 的 HTTP 服务器
	// 每条连接都留一个 goroutine，连点五首就是五条，量的是它不是我们。
	// 盯住我们自己的那些（下载泵、看门狗）才是真的。
	waitFor(t, 15*time.Second, "被打掉的下载 goroutine 都退干净", func() bool {
		return liveStreamWorkers() == 0
	})
}

// TestClosingTheAppReleasesEverything 盯「退出不留东西」。
func TestClosingTheAppReleasesEverything(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh320), 110836))

	dir := t.TempDir()
	a := app.New()
	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	a.MountStreaming(app.Streaming{Client: cli, Media: f.Client(), TempDir: dir})

	if err := a.Enqueue(onlineItem("BV1", "1", "退出去的一首")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 30*time.Second, "开始播", func() bool { return a.Snapshot().HasTrack })

	a.Close()
	a.Close() // 可以重复调用

	waitFor(t, 15*time.Second, "缓存文件被删干净", func() bool {
		entries, err := os.ReadDir(dir)
		return err == nil && len(entries) == 0
	})
	waitFor(t, 15*time.Second, "下载和看门狗都退出", func() bool {
		return liveStreamWorkers() == 0
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// 偏好
// ─────────────────────────────────────────────────────────────────────────────

// TestAudioQualityIsHonouredAndChangeable 盯用户选的档位算数。
func TestAudioQualityIsHonouredAndChangeable(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	url := cdnURL(f.URL, "a.flac", 2*time.Hour)
	f.onPlayURL(map[string]any{"duration": 1, "audio": []any{map[string]any{
		"id": int(bili.QualityHigh320), "bandwidth": 110836, "codecs": "flac",
		"mimeType": "audio/mp4", "baseUrl": url,
	}, map[string]any{
		"id": int(bili.QualityLow64), "bandwidth": 51651, "codecs": "flac",
		"mimeType": "audio/mp4", "baseUrl": url,
	}}})

	a := newOnlineApp(t, f, t.TempDir())
	if a.AudioQuality() != bili.AudioAuto {
		t.Errorf("默认偏好 = %v，want AudioAuto", a.AudioQuality())
	}
	a.SetAudioQuality(bili.AudioLow)
	if a.AudioQuality() != bili.AudioLow {
		t.Errorf("改完偏好 = %v，want AudioLow", a.AudioQuality())
	}
	if err := a.Enqueue(onlineItem("BV1", "1", "省流量的一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "省流量的一首")
	if q := a.Snapshot().Quality; q != "64K" {
		t.Errorf("徽章 = %q，want 64K（用户选了省流量）", q)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 凭据边界
// ─────────────────────────────────────────────────────────────────────────────

// TestTheMediaClientGivesTheCDNNoCookie 盯「B 站 CDN 不是同一个凭据域」。
//
// 取音频字节的那个客户端**不挂 cookie jar**——SESSDATA 发给一台存音频文件的
// CDN 没有任何理由（`spec.md`「网络」一节）。但 Referer 要给：B 站 CDN 不给
// 就 403，而 Referer 不是凭据。
//
// 这一条走的是**生产那一份**客户端配置（`CredentialTransport.Client(nil)`，
// 也就是 `ui.MountStreaming` 里建的那一个），不是现造一个——凭据边界这件事上
// 「测的和跑的不是一个」等于没测。
//
// 主机名用**真的 CDN 域名**（`newHostFixture` 会把所有主机名拨到同一台服务器），
// 因为白名单判的是 hostname，`127.0.0.1` 上「陌生主机」根本不存在。
func TestTheMediaClientGivesTheCDNNoCookie(t *testing.T) {
	const cdnHost = "upos-sz-mirrorhw.bilivideo.com"

	f := newHostFixture(t, map[string]http.HandlerFunc{
		cdnHost: func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) },
	})
	c := f.client(t)

	req, err := http.NewRequest(http.MethodGet, "https://"+cdnHost+"/upgcxcode/x.m4s", nil)
	if err != nil {
		t.Fatal(err)
	}
	// 手动挂上凭据：模拟请求上已经有一份登录态（或者调用方手滑挂上了）。
	// 正确的行为是这一层**把它删掉**。
	resp, err := c.Do(withCookie(req, testSecret))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	g := f.recv(cdnHost)
	if strings.Contains(g.Cookie, testSecret) {
		t.Errorf("CDN 收到了 B 站的登录 Cookie：%q", g.Cookie)
	}
	if g.Referer == "" {
		t.Error("CDN 没拿到 Referer（B 站不给就 403）")
	}
	if g.Auth != "" {
		t.Errorf("CDN 收到了 Authorization：%q", g.Auth)
	}
}

// liveStreamWorkers 数还活着的「下载泵 + 看门狗」有多少个。
//
// 为什么不用 `runtime.NumGoroutine()`：fixture 的 HTTP 服务器每条连接留一个
// goroutine，连点五首就是五条——量的是它不是我们。盯住我们自己起的那些才对。
func liveStreamWorkers() int {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	dump := string(buf[:n])
	return strings.Count(dump, "audio.(*StreamSource).pump") +
		strings.Count(dump, "audio.(*StreamSource).watchdog")
}
