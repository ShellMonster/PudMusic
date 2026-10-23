// 播放记录上报**从真的播放链路走到 B 站**的那一条。
//
// 这一组和 `playreport_test.go` 的分工：
//
//	playreport_test.go     「什么时候发、发什么」——把播放状态当输入摆进去
//	这一份                 「真的在放的时候，那条链路自己会走到上报吗」
//
// 这一份走的是真的队列 → 取流 → CDN → 解码 → 进度条时钟，钉的是
// `tick` 里那一句接线（`app.go` 的 `reportTick`）接对了没有。少了它，
// 上面那一整组全绿而这一页的播放记录仍然是空的——而那正是这一票要修的
// 症状（审计 A7：壳子做完了、数据源没排票）。
//
// ⚠️ 所有请求都打在**本地 fixture** 上，一次都不会碰到真的 B 站。上报是
// 写操作，它写进用户真实的账号（见任务报告「需要真网验」那一节）。

package app_test

import (
	"context"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// 🔴 路径是**字面量**（理由和 `playreport_test.go` 里那两条一样：跟着
// `bili` 里的常量走的话，那边写错了这边也跟着错）。
const (
	e2eBeatPath = "/x/click-interface/web/heartbeat"
	e2eOpenPath = "/x/click-interface/click/web/h5"
)

// newReportOnlineApp 装一个**真的能放 B 站在线流**、而且已登录的 App。
//
// 登录走的是真那条路（先把一份登录态放进后端，再 `MountLogin`）——
// 「上报是登录之后才有的写操作」，测试里硬塞一个 bool 的话，
// `DedeUserID` 那一栏就没了，而代次守卫比的就是它。
func newReportOnlineApp(t *testing.T, f *streamFixture) *app.App {
	t.Helper()
	a := app.New()
	t.Cleanup(a.Close)

	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(f.URL)

	store := app.NewMemoryStore("report-e2e")
	if err := store.Save(context.Background(), app.Session{
		SESSDATA: "e2e-sessdata", BiliJCT: "e2e-jct", DedeUserID: "2333",
	}); err != nil {
		t.Fatal(err)
	}
	a.MountLogin(store, cli)
	a.MountStreaming(app.Streaming{Client: cli, Media: f.Client(), TempDir: t.TempDir()})
	return a
}

// reportSink 记下心跳。
type reportSink struct {
	mu    sync.Mutex
	beats []url.Values
}

func (s *reportSink) add(f url.Values) {
	s.mu.Lock()
	s.beats = append(s.beats, f)
	s.mu.Unlock()
}

func (s *reportSink) all() []url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]url.Values(nil), s.beats...)
}

// serveReporting 让那台 fixture 答得上来上报，并把心跳记在 sink 里。
func serveReporting(t *testing.T, f *streamFixture) *reportSink {
	t.Helper()
	sink := &reportSink{}
	f.on(e2eOpenPath, func(w http.ResponseWriter, r *http.Request) {
		writeStreamJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{"trackId": 1}})
	})
	f.on(e2eBeatPath, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		sink.add(r.PostForm)
		writeStreamJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{}})
	})
	// bvid → aid：上报的请求体里必须有它（见 `playreport.go` 的文件头）。
	f.on("/x/web-interface/view", func(w http.ResponseWriter, r *http.Request) {
		writeStreamJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{"aid": 114514}})
	})
	return sink
}

// TestPlayingARealStreamReachesBilibili 盯那条链路自己会走到上报。
//
// 这一条是这一票的验收项：历史记录页读的是 B 站服务器上的历史，而我们
// 以前**一次都没往里写**。这里走完真的播放链路（队列 → 取流 → CDN →
// 解码 → 进度条时钟），然后量 B 站那边有没有真的收到这一首的心跳。
//
// 素材是真的 FLAC，走真的解码链——拿假的字节测「能出声」等于没测。
func TestPlayingARealStreamReachesBilibili(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	sink := serveReporting(t, f)
	f.onPlayURL(dashFor(cdnURL(f.URL, "e2e.flac", 2*time.Hour), int(bili.QualityHigh192), 110836))

	a := newReportOnlineApp(t, f)
	if err := a.Enqueue(onlineItem("BV1e2e", "999", "上报的那一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "上报的那一首")

	// 心跳默认 30 秒一次，等不起——把间隔调小（这是生产代码上的那个
	// 测试专用入口，`reportInterval` 是 30 秒）。
	a.SetPlayReportInterval(20 * time.Millisecond)

	waitFor(t, 15*time.Second, "B 站那边收到这一首的心跳", func() bool {
		for _, b := range sink.all() {
			if b.Get("bvid") == "BV1e2e" {
				return true
			}
		}
		return false
	})

	var start url.Values
	for _, b := range sink.all() {
		if b.Get("bvid") == "BV1e2e" && b.Get("play_type") == "1" {
			start = b
		}
	}
	if start == nil {
		t.Fatalf("没有 play_type=1 的那一条：%v", sink.all())
	}
	if start.Get("mid") != "2333" {
		t.Errorf("mid = %q，want 2333（登录的那个账号）", start.Get("mid"))
	}
	if start.Get("cid") != "999" {
		t.Errorf("cid = %q，want 999", start.Get("cid"))
	}
	if start.Get("aid") != "114514" {
		t.Errorf("aid = %q，want 114514（从稿件详情换来的）", start.Get("aid"))
	}
	if len(start.Get("session")) != 32 {
		t.Errorf("会话 id 是 %q，want 32 位不带分隔符的小写十六进制", start.Get("session"))
	}
	if st := a.Snapshot(); st.Err != "" {
		t.Errorf("播放条上有错：%s", st.Err)
	}
}

// TestStoppingARealStreamTellsBilibiliItIsOver 盯停止也会走到 B 站。
//
// 走的是真的 `App.Stop`（锁屏上那个方块键那条路）：位置和时长在清零之前
// 抄下来，那一条 PlayEnd 带着的是**用户听到哪儿**，不是 0。
func TestStoppingARealStreamTellsBilibiliItIsOver(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	sink := serveReporting(t, f)
	f.onPlayURL(dashFor(cdnURL(f.URL, "stop.flac", 2*time.Hour), int(bili.QualityHigh192), 110836))

	a := newReportOnlineApp(t, f)
	a.SetPlayReportInterval(20 * time.Millisecond)
	if err := a.Enqueue(onlineItem("BV1stopreal", "888", "停下来的那一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "停下来的那一首")

	// 等到真的听进去几秒，不然「停下来的那一首」和「从头听就停了」分不开。
	waitFor(t, 15*time.Second, "进度条往前走", func() bool {
		return a.Snapshot().Position > 2*time.Second
	})
	a.Stop()

	waitFor(t, 10*time.Second, "B 站那边收到结束那一条", func() bool {
		for _, b := range sink.all() {
			if b.Get("play_type") == "4" && b.Get("bvid") == "BV1stopreal" {
				return true
			}
		}
		return false
	})

	for _, b := range sink.all() {
		if b.Get("play_type") == "4" && b.Get("bvid") == "BV1stopreal" {
			if b.Get("played_time") == "0" {
				t.Errorf("停止那一条的 played_time 是 0——那是清零之后的位置，不是听到哪儿")
			}
			return
		}
	}
}

// TestLoggingOutARealStreamStopsTheReporting 盯退登之后不再写。
//
// 🔴 上报是写操作，它写进用户**真实的**账号。换人登录之后那条 goroutine
// 还在发的话，那些进度会被记到**新登录的那个人**名下——而那是他没听过的歌。
func TestLoggingOutARealStreamStopsTheReporting(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	sink := serveReporting(t, f)
	f.onPlayURL(dashFor(cdnURL(f.URL, "logout.flac", 2*time.Hour), int(bili.QualityHigh192), 110836))

	a := newReportOnlineApp(t, f)
	a.SetPlayReportInterval(20 * time.Millisecond)
	if err := a.Enqueue(onlineItem("BV1logoutreal", "777", "退登的那一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "退登的那一首")
	waitFor(t, 15*time.Second, "B 站那边收到这一首的心跳", func() bool {
		return len(sink.all()) > 0
	})

	if err := a.Logout(); err != nil {
		t.Fatalf("退登失败：%v", err)
	}
	// 歌还在放着（退登不停歌），而退登之后一条都不许再有。
	// 心跳间隔只有 20 ms——真还在发的话这半秒里能发好几条。
	before := len(sink.all())
	time.Sleep(500 * time.Millisecond)
	if got := sink.all()[before:]; len(got) != 0 {
		t.Errorf("退登之后还发了 %d 条心跳", len(got))
	}
	if st := a.Snapshot(); !st.HasTrack {
		t.Errorf("退登把正在放的歌弄没了（这不是退登该做的事）")
	}
}
