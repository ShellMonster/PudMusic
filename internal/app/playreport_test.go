// 播放记录上报这一摊的业务层测试。
//
// 这一组钉的是**「什么时候发、发什么、什么时候不许发」**——也就是协议层
// 管不着的那一半。协议层那边的形状在
// `internal/service/bili/playreport_test.go`。
//
// # 为什么这里不真的放歌
//
// 这一组把「在放哪一首、放到哪儿了」当作**输入**摆进去，而不是真的开一条
// 流：心跳是 30 秒一发的，测试里真等 30 秒既慢又不稳，而这一层的全部判断
// 就是「这一刻和上一刻差在哪」。真的从播放链路走到这里的那一条，在
// `playreport_e2e_test.go` 里钉（那边走的是真解码 + 真进度条时钟）。
//
// ⚠️ 这里所有请求都打在**本地 fixture** 上，一次都不会碰到真的 B 站：
// 上报是写操作，它写进用户真实的账号。见任务报告里「需要真网验」那一节。

package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// ─────────────────────────────────────────────────────────────────────────────
// fixture：一台只答「上报要用的那几个接口」的假 B 站
// ─────────────────────────────────────────────────────────────────────────────

// ⚠️ 这两条路径是**字面量**，没有从 `bili` 包里拿常量。
//
// 那是故意的：这里量的是「打的是不是 `/x/click-interface/web/heartbeat`」
// 这件事本身。跟着 `bili` 里的常量走的话，那边写错了这边也跟着错，
// 一条测试就再也发现不了。
const (
	openPathWanted = "/x/click-interface/click/web/h5"
	beatPathWanted = "/x/click-interface/web/heartbeat"
)

// reportBili 记下收到的每一次「开一轮」和「心跳」。
type reportBili struct {
	*httptest.Server

	mu sync.Mutex
	// opens / beats 是收到的表单，按到达顺序。
	opens []url.Values
	beats []url.Values
	// viewHits 数「bvid → aid」那一次被问了几次。
	viewHits int
	// failBeats 非 0 时心跳那一路一律回这个 code（模拟 B 站不收 / 网抖）。
	failBeats int
	// failOpens 是「开一轮的头」还要再失败几次。用来造网络抖。
	failOpens int
	// viewFails 让「bvid → aid」那一问失败（模拟这个稿件问不着）。
	viewFails bool
	// viewGate 非 nil 时「bvid → aid」那一问先卡在它上面，放开才回。
	//
	// 用来把「开一轮的开头还在路上」这件事**稳定地**造出来——那一发是
	// 网络往返，不卡住它就只能靠运气赶在它前面切歌，那样的测试是 flaky 的。
	viewGate chan struct{}
}

func newReportBili(t *testing.T) *reportBili {
	t.Helper()
	b := &reportBili{}

	mux := http.NewServeMux()
	mux.HandleFunc("/x/web-interface/nav", func(w http.ResponseWriter, r *http.Request) {
		// WBI 签名要先打 nav 拿两把密钥，拿不到签名那一步就报错，
		// 连真正的请求都发不出去。
		writeReportJSON(w, map[string]any{"wbi_img": map[string]string{
			"img_url": "https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png",
			"sub_url": "https://i0.hdslb.com/bfs/wbi/4932caff0ff746eab6f01bf08b70ac45.png",
		}})
	})
	mux.HandleFunc("/x/web-interface/view", func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		b.viewHits++
		gate, fails := b.viewGate, b.viewFails
		b.mu.Unlock()
		if gate != nil {
			<-gate
		}
		if fails {
			writeReportCode(w, -404, "没有这个稿件")
			return
		}
		writeReportJSON(w, map[string]any{"aid": 114514})
	})
	mux.HandleFunc(openPathWanted, func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		b.opens = append(b.opens, formOf(r))
		fail := b.failOpens
		if fail > 0 {
			b.failOpens--
		}
		b.mu.Unlock()
		if fail > 0 {
			writeReportCode(w, -500, "B 站抖了")
			return
		}
		writeReportJSON(w, map[string]any{})
	})
	mux.HandleFunc(beatPathWanted, func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		form := formOf(r)
		fail := b.failBeats
		b.beats = append(b.beats, form)
		b.mu.Unlock()
		if fail != 0 {
			writeReportCode(w, fail, "B 站不收")
			return
		}
		writeReportJSON(w, map[string]any{})
	})

	b.Server = httptest.NewServer(mux)
	t.Cleanup(b.Close)
	return b
}

func writeReportJSON(w http.ResponseWriter, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		raw = []byte("null")
	}
	body, _ := json.Marshal(map[string]any{
		"code": 0, "message": "0", "ttl": 1, "data": json.RawMessage(raw),
	})
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

func writeReportCode(w http.ResponseWriter, code int, msg string) {
	body, _ := json.Marshal(map[string]any{"code": code, "message": msg, "ttl": 1})
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// formOf 收下那一次的表单。
func formOf(r *http.Request) url.Values {
	_ = r.ParseForm()
	return r.PostForm
}

func (b *reportBili) beatsOf(action string) []url.Values {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []url.Values
	for _, f := range b.beats {
		if f.Get("play_type") == action {
			out = append(out, f)
		}
	}
	return out
}

// allBeats 是收到的全部心跳（加锁取快照）。
//
// ⚠️ 记心跳的是 httptest 自己的 goroutine，测试这一边不加锁读它就是一处
// 数据竞争——`-race` 会指出来，而不带 `-race` 跑就是另一种「有时绿有时红」。
func (b *reportBili) allBeats() []url.Values {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]url.Values(nil), b.beats...)
}

func (b *reportBili) opensCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.opens)
}

func (b *reportBili) viewCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.viewHits
}

// ─────────────────────────────────────────────────────────────────────────────
// rig：一个挂了登录与在线播放、并且心跳很快的 App
// ─────────────────────────────────────────────────────────────────────────────

// testMid 是这一组统一的「登录了的那个账号」。
const testMid = "2333"

// newReportRig 装一个 App：已登录（mid 2333）、已挂在线播放、**心跳 20 ms 一次**。
//
// 心跳间隔调小是为了让「多久发一次」这件事在测试里能被看到。正式路径是
// 30 秒（`reportInterval`），真等 30 秒既慢又不稳。
func newReportRig(t *testing.T) (*App, *reportBili) {
	t.Helper()
	b := newReportBili(t)

	a := New()
	t.Cleanup(a.Close)

	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(b.URL)

	// 走的是**真的那条路**：先把一份登录态放进后端，再 `MountLogin`——
	// 和「重启后还是登录」是同一条（`account.go` 的 restore）。
	store := NewMemoryStore("report-test")
	if err := store.Save(context.Background(), Session{
		SESSDATA: "test-sessdata", BiliJCT: "test-jct", DedeUserID: testMid,
	}); err != nil {
		t.Fatal(err)
	}
	a.MountLogin(store, cli)
	a.MountStreaming(Streaming{Client: cli, Media: b.Client(), TempDir: t.TempDir()})

	if err := a.MountSettings(filepath.Join(t.TempDir(), "settings.json")); err != nil {
		t.Fatal(err)
	}
	a.report.setInterval(20 * time.Millisecond)
	return a, b
}

// reportItem 是一条在线投稿。
func reportItem(bvid, cid string) Item {
	return Item{Source: "online", Type: "mv", Bvid: bvid, Cid: cid, Title: "一首"}
}

// putTrack 把「此刻在放什么」摆进 App 的状态里（不真的开流）。
//
// ⚠️ **摆完就完事，测试不去自己调 `reportTick`**：进度条的时钟（`tick`）
// 本来就在跑，它每一拍读的就是 `a.online` / `a.position` / `a.duration` /
// `a.playing` 这几栏。自己再手动调一遍的话是**两个驱动源**——时钟看到的是
// 「没有播放器」，每一拍都会报一次「暂停」，而手动那一遍报的是「在放」。
// 两边对着干，量到的东西和真链路上的不一样。
func putTrack(a *App, it Item, pos, dur time.Duration, playing bool) {
	a.mu.Lock()
	if it.Bvid == "" {
		a.online = nil
	} else {
		a.online = &onlineTrack{item: it}
	}
	a.position, a.duration = pos, dur
	a.playing = playing
	a.wantPlay = playing
	a.mu.Unlock()
}

// play 摆好一首歌在放。
func play(a *App, it Item, pos, dur time.Duration) {
	putTrack(a, it, pos, dur, true)
}

// finishOnce 造一次「真的放完了」的那一拍。
//
// 🔴 这一条**只能**直接进上报器：真链路上的「放完」是解器读到 EOF、
// `tick` 判出 `p.Err() == nil` 才有的，而这里没有真播放器（`putTrack`
// 只摆状态）。所以这是整个仓库里唯一能造出 `finished=true` 的地方。
func finishOnce(t *testing.T, a *App, it Item, pos, dur time.Duration) {
	t.Helper()
	a.mu.Lock()
	r := a.report
	a.mu.Unlock()
	if r == nil {
		t.Fatal("上报器没装上")
	}
	r.sync(it, dur, pos, false, true)
}

// pauseBeat 摆好「暂停了」。
func pauseBeat(a *App, it Item, pos, dur time.Duration) {
	putTrack(a, it, pos, dur, false)
}

// untilBeats 等到「至少有 n 条指定动作的心跳」为止。
func untilBeats(t *testing.T, b *reportBili, action string, n int) []url.Values {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := b.beatsOf(action)
		if len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			b.mu.Lock()
			defer b.mu.Unlock()
			t.Fatalf("等了 5 秒还没有 %d 条 play_type=%s 的心跳，实际收到 %d 条：%v",
				n, action, len(b.beats), playTypesOf(b.beats)) // 此刻持着锁
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func playTypesOf(beats []url.Values) []string {
	out := make([]string, 0, len(beats))
	for _, f := range beats {
		out = append(out, f.Get("play_type"))
	}
	return out
}

// untilOpens 等到「已经开过 n 次头」为止。
//
// 🔴 **不能用 `opensCount()` 直接断言**：发送那条 goroutine 是**按顺序**
// 一条一条发的，所以「上一首的结束那一条到了」只能证明它前面那几条到了，
// 证明不了**后面**那一轮的头已经发出去。切歌那一拍的顺序恰恰是
// 「先结束旧的、再开新的」，于是断言必须等到新头真的来了。
func untilOpens(t *testing.T, b *reportBili, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for b.opensCount() < n {
		if time.Now().After(deadline) {
			t.Fatalf("等了 5 秒还是只开了 %d 次头，want %d", b.opensCount(), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// sessionOf 是那条心跳的会话 id。
func sessionOf(f url.Values) string { return f.Get("session") }

// ─────────────────────────────────────────────────────────────────────────────
// 一、什么时候发
// ─────────────────────────────────────────────────────────────────────────────

// TestPlayingOpensTheRoundAndSaysItStarted 盯「开始播」。
//
// 钉的是那一轮的两发：`click/web/h5` 那一发（开一轮）和心跳上的
// play_type=1。B 站那边「开始播」是记在心跳的 play_type 上的，只发前面
// 那一发不算开始。
//
// 顺带钉住三样抄来的东西：`aid`（问一次 `/x/web-interface/view` 换来的）、
// 会话 id 的形状（32 位不带分隔符的小写十六进制）、`fnval`（和取流同值）。
func TestPlayingOpensTheRoundAndSaysItStarted(t *testing.T) {
	a, b := newReportRig(t)
	it := reportItem("BV1start", "101")
	play(a, it, 0, 900*time.Second)

	got := untilBeats(t, b, "1", 1)[0]
	if got.Get("bvid") != "BV1start" || got.Get("cid") != "101" {
		t.Errorf("心跳记的不是这一首：bvid=%q cid=%q", got.Get("bvid"), got.Get("cid"))
	}
	if got.Get("aid") != "114514" {
		t.Errorf("aid = %q，want 114514（从稿件详情换来的）", got.Get("aid"))
	}
	if got.Get("mid") != testMid {
		t.Errorf("mid = %q，want %s", got.Get("mid"), testMid)
	}
	if s := sessionOf(got); len(s) != 32 {
		t.Errorf("会话 id 是 %q，want 32 位不带分隔符的小写十六进制", s)
	}
	if got.Get("fnval") != "4048" {
		t.Errorf("fnval = %q，want 4048（要和取流发的那个一样）", got.Get("fnval"))
	}
	if n := b.opensCount(); n != 1 {
		t.Errorf("开了 %d 次头，want 1", n)
	}
	if n := len(b.beatsOf("1")); n != 1 {
		t.Errorf("play_type=1 的心跳有 %d 条，want 1", n)
	}
}

// TestAHeartbeatKeepsGoingWhileItPlays 盯 30 秒心跳。
//
// 这一条是这一页存在的理由：不发心跳，B 站那边这一轮从头到尾就没有
// 「还在听」这件事，历史记录页上的播放进度也就永远是 0。
func TestAHeartbeatKeepsGoingWhileItPlays(t *testing.T) {
	a, b := newReportRig(t)
	it := reportItem("BV1beat", "202")

	// 连着跑 12 拍（心跳间隔 20 ms）。
	for i := 0; i < 12; i++ {
		play(a, it, time.Duration(i)*time.Second, 900*time.Second)
		time.Sleep(25 * time.Millisecond)
	}
	got := untilBeats(t, b, "0", 3)
	if len(got) < 3 {
		t.Fatalf("只发了 %d 条心跳", len(got))
	}
	// 同一轮里的心跳共用一个会话 id——那是 B 站把它们并成「一次观看」的依据。
	if sessionOf(got[0]) != sessionOf(got[len(got)-1]) {
		t.Errorf("同一轮里会话 id 变了：%q → %q", sessionOf(got[0]), sessionOf(got[len(got)-1]))
	}
	// 进度是真的在往前走，不是原地重复。
	last := got[len(got)-1]
	if last.Get("played_time") == "0" {
		t.Errorf("最后一条心跳的 played_time 还是 0：%v", last)
	}
}

// TestPausingAndResumingAreToldApart 盯暂停 / 继续。
//
// 🔴 **会话 id 必须还是同一个**：暂停不是「这一轮结束了」，用户过一会儿
// 按播放要接着听。换成新一轮的话 B 站那边就成了两次独立的观看，历史记录
// 里那一首会多出一条。
func TestPausingAndResumingAreToldApart(t *testing.T) {
	a, b := newReportRig(t)
	it := reportItem("BV1pause", "303")

	play(a, it, 10*time.Second, 900*time.Second)
	untilBeats(t, b, "1", 1)

	pauseBeat(a, it, 30*time.Second, 900*time.Second)
	paused := untilBeats(t, b, "2", 1)[0]

	play(a, it, 35*time.Second, 900*time.Second)
	resumed := untilBeats(t, b, "3", 1)[0]

	if sessionOf(paused) != sessionOf(resumed) {
		t.Errorf("暂停和继续不是同一轮：%q vs %q", sessionOf(paused), sessionOf(resumed))
	}
	if paused.Get("played_time") != "30" {
		t.Errorf("暂停那一刻的 played_time = %q，want 30", paused.Get("played_time"))
	}
	if n := b.opensCount(); n != 1 {
		t.Errorf("暂停之后又开了 %d 次头（共 %d），want 1", n-1, n)
	}
}

// TestSwitchingEndsTheOldRoundAndOpensANewOne 盯切歌。
//
// 上一轮要收到 play_type=4（它不会自己知道歌换没换），新一轮要用**新的**
// 会话 id——B 站按会话 id 把心跳并成一次观看，混在一个会话里就成了
// 「一首超长的歌」。
func TestSwitchingEndsTheOldRoundAndOpensANewOne(t *testing.T) {
	a, b := newReportRig(t)
	old := reportItem("BV1old", "404")
	neu := reportItem("BV1new", "505")

	play(a, old, 100*time.Second, 900*time.Second)
	first := untilBeats(t, b, "1", 1)[0]

	play(a, neu, 0, 900*time.Second)
	end := untilBeats(t, b, "4", 1)[0]

	if end.Get("bvid") != "BV1old" {
		t.Errorf("结束那一条记的是 %q，want BV1old（上一首）", end.Get("bvid"))
	}
	if sessionOf(end) != sessionOf(first) {
		t.Errorf("结束那一条不是上一轮的那一轮")
	}
	// 🔴 **结束要报「听到了哪儿」，不是此刻的位置**：切歌这一拍
	// `App.position` 已经被新歌清成 0 了，照它报就是「从头听就停了」。
	if end.Get("played_time") != "100" {
		t.Errorf("结束那一条的 played_time = %q，want 100（上一首听到的地方）", end.Get("played_time"))
	}
	untilOpens(t, b, 2)
	if n := b.opensCount(); n != 2 {
		t.Errorf("开了 %d 次头，want 2（两首各一次）", n)
	}
}

// TestTheRoundThatEndsCarriesItsOwnDuration 盯切歌时上一轮发的是**它自己的**时长。
//
// 🔴 这一条钉的是一个会写错数据的坑。「下一首」走的是
// `step` → `playOnline` → `runOnline`，那条路**不清轨道**（点队列里某一行
// 和「删掉正在放的那首」那两条才清）。而 `runOnline` 是在一个临界区里同时
// 换掉「在放哪一首 / 时长 / 位置」的，于是紧跟着的那一拍上，上一轮的结束
// 那一条如果现读时长，读到的是**下一首**的。
//
// 后果：B 站那边一条 30 秒的视频配上一个 600 秒的 `video_duration`，
// 历史记录里那一列播放进度会读成「看了 3%」——用户明明听到了头。
func TestTheRoundThatEndsCarriesItsOwnDuration(t *testing.T) {
	a, b := newReportRig(t)
	old := reportItem("BV1short", "707")
	neu := reportItem("BV1long", "808")

	play(a, old, 20*time.Second, 30*time.Second) // 30 秒的那首
	untilBeats(t, b, "1", 1)

	// 「下一首」：一个 30 秒的视频按到了一首 10 分钟的。
	play(a, neu, 0, 600*time.Second)
	end := untilBeats(t, b, "4", 1)[0]

	if end.Get("video_duration") != "30" {
		t.Errorf("上一首的时长被报成了 %q 秒，want 30（那是**下一首**的时长）",
			end.Get("video_duration"))
	}
	if end.Get("played_time") != "20" {
		t.Errorf("played_time = %q，want 20", end.Get("played_time"))
	}
	// 新一轮自己带着自己的时长。
	if got := untilBeats(t, b, "1", 2)[1]; got.Get("video_duration") != "600" {
		t.Errorf("新一轮的 video_duration = %q，want 600", got.Get("video_duration"))
	}
}

// TestAFailedOpenDoesNotSilenceThatVideoForever 一次网络抖不该让这一首从此不记。
//
// 开不了头多半是抖一下，而那一轮的心跳照样在发（B 站那边靠心跳也能把这一条
// 建出来）。要是失败之后记一笔「这一首别记了」，用户等这一轮结束再放同一首
// 时就再也报不上——一次抖动换来「这一首今天不记了」。
func TestAFailedOpenDoesNotSilenceThatVideoForever(t *testing.T) {
	a, b := newReportRig(t)
	it := reportItem("BV1flaky", "909")

	// 先让「开那一轮的头」失败一次。
	b.mu.Lock()
	b.failOpens = 1
	b.mu.Unlock()

	play(a, it, 0, 900*time.Second)
	// 头没开成，而**心跳照样在发**——那才是真正的进度，B 站那边靠它也能
	// 把这一条建出来。
	untilBeats(t, b, "0", 1)

	// 这一轮结束，再放同一首：这一次得开得起来。
	a.stopTrack(50 * time.Second)
	untilBeats(t, b, "4", 1)
	opensAfterFail := b.opensCount()

	play(a, it, 0, 900*time.Second)
	untilBeats(t, b, "1", 1)
	if n := b.opensCount(); n <= opensAfterFail {
		t.Errorf("开过一次头失败之后，这一首再也不开新一轮了（共 %d 次）", n)
	}
}

// TestStoppingEndsTheRoundAndDoesNotOpenItAgain 盯「停止」。
//
// 🔴 后半句是这里最容易写错的地方：停止之后播放状态是「不在放」，而
// 「不在放」之后每一拍都还在跑。少了 `playing` 那一条判据，下一拍就会把
// 刚才关掉的那一轮又开起来——用户明明已经按了停止，应用还在替他记。
func TestStoppingEndsTheRoundAndDoesNotOpenItAgain(t *testing.T) {
	a, b := newReportRig(t)
	it := reportItem("BV1stop", "606")

	play(a, it, 200*time.Second, 900*time.Second)
	untilBeats(t, b, "1", 1)

	// 用户按停止（`App.Stop` 那条路）：位置先清零，而那一下抄下来的是
	// 清零**之前**的位置。
	at := 200 * time.Second
	putTrack(a, it, 0, 900*time.Second, false)
	a.stopTrack(at)

	end := untilBeats(t, b, "4", 1)[0]
	if end.Get("played_time") != "200" {
		t.Errorf("停止那一条的 played_time = %q，want 200", end.Get("played_time"))
	}

	// 停止之后连着跑十拍：用户**没有**再按播放。
	opensAfterStop := b.opensCount()
	time.Sleep(300 * time.Millisecond)
	if n := b.opensCount(); n != opensAfterStop {
		t.Errorf("停止之后又多开了 %d 次头——用户明明已经停了", n-opensAfterStop)
	}
}

// TestFinishingSaysItFinished 盯播完。
//
// `played_time = -1` 是 B 站把这一条标成「看完」的唯一依据，历史记录页
// 就是照它显示的。
func TestFinishingSaysItFinished(t *testing.T) {
	a, b := newReportRig(t)
	it := reportItem("BV1done", "707")

	play(a, it, 899*time.Second, 900*time.Second)
	untilBeats(t, b, "1", 1)

	finishOnce(t, a, it, 900*time.Second, 900*time.Second)

	end := untilBeats(t, b, "4", 1)[0]
	if end.Get("played_time") != "-1" {
		t.Errorf("播完那一条的 played_time = %q，want -1（B 站据此标「看完」）", end.Get("played_time"))
	}
}

// TestAFailedStreamIsNeverReportedAsFinished 盯「解码失败不许被当成播完」。
//
// 🔴 这一条和 `TestACorruptFileNeverLooksLikeItFinished` 是同一条判据的
// 另一个落点：断流和解码失败都让 `Playing()` 变 false，而拿它去发
// `played_time = -1` 就是往用户**真实的账号**里写一句「他看完了」。
// 他没看完——那句话会一直摆在他的历史记录页上。
func TestAFailedStreamIsNeverReportedAsFinished(t *testing.T) {
	a, b := newReportRig(t)
	it := reportItem("BV1dead", "808")

	play(a, it, 120*time.Second, 900*time.Second)
	untilBeats(t, b, "1", 1)

	// 这一拍是「从在放变成不在放」，但**不是**放完了（`finished=false`，
	// 那是 `tick` 判过 `p.Err() != nil` 之后的结果）。
	putTrack(a, it, 120*time.Second, 900*time.Second, false)
	untilBeats(t, b, "2", 1)

	for _, f := range b.allBeats() {
		if f.Get("played_time") == "-1" {
			t.Fatalf("断流被报成了播完：play_type=%s", f.Get("play_type"))
		}
	}
	// 而它确实发出去了一条——用户听不下去停在那儿，那 120 秒得记上。
	all := b.allBeats()
	if last := all[len(all)-1]; last.Get("played_time") != "120" {
		t.Errorf("断流那一条的 played_time = %q，want 120（听到哪儿就记到哪儿）",
			last.Get("played_time"))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 二、什么不许发
// ─────────────────────────────────────────────────────────────────────────────

// TestLocalFilesAreNeverReported 本地文件在 B 站那边没有对应的东西。
//
// 发出去的 aid 是 0、bvid 是空串，B 站会回 0 而什么也不会记——一个
// 「回成功但没做事」的请求比不发更坏，它让日志看着一切正常。
func TestLocalFilesAreNeverReported(t *testing.T) {
	a, b := newReportRig(t)

	putTrack(a, Item{}, 42*time.Second, 900*time.Second, true)
	time.Sleep(250 * time.Millisecond)
	if got := b.allBeats(); len(got) != 0 {
		t.Errorf("本地文件也发了 %d 条心跳：%v", len(got), playTypesOf(got))
	}
	if n := b.opensCount(); n != 0 {
		t.Errorf("本地文件也开了 %d 次头", n)
	}
}

// TestTheSwitchOffMeansNothingIsSent 「记录播放历史」那个开关。
//
// 它是设置里本来就有的一个键，摆在历史记录那一页的页头（`ui/history.go`）。
// 这一条钉的是「关掉之后真的少记一条」——那一页开关摆在那儿，用户按了
// 却不生效的话比没有这个开关更糟。
func TestTheSwitchOffMeansNothingIsSent(t *testing.T) {
	a, b := newReportRig(t)
	if _, err := a.UpdateSettings(func(s *Settings) { s.ReportPlayHistory = false }); err != nil {
		t.Fatal(err)
	}

	it := reportItem("BV1quiet", "909")
	play(a, it, 10*time.Second, 900*time.Second)
	for i := 0; i < 10; i++ {
		play(a, it, time.Duration(i+1)*time.Second, 900*time.Second)
		time.Sleep(25 * time.Millisecond)
	}
	if n := len(b.allBeats()); n != 0 {
		t.Errorf("开关关着还是发了 %d 条心跳", n)
	}
	if n := b.opensCount(); n != 0 {
		t.Errorf("开关关着还是开了 %d 次头", n)
	}
}

// TestASessionInFlightDiesWithTheRoundThatStartedIt 代次守卫。
//
// 用户连点五首：前四首的「开一轮」还在队列里没发出去（问 aid 那一次是
// 网络往返），而那时候第五首已经开始了。不带代次的话，前四首的历史记录
// 会在**第五首**的会话里被开出来——B 站那边看过去是同一首歌被听了五遍。
func TestASessionInFlightDiesWithTheRoundThatStartedIt(t *testing.T) {
	a, b := newReportRig(t)

	// 卡住「问 aid」那一道，让第一轮的开头一直在路上。
	blocked := make(chan struct{})
	b.mu.Lock()
	b.viewGate = blocked
	b.mu.Unlock()

	// 每换一首之间留一拍（50 ms），让时钟真的看见那一次切换——
	// 全挤在一拍里的话时钟只看得见最后一首，代次守卫就没被量到。
	play(a, reportItem("BV1rushed", "1"), 0, 900*time.Second)
	time.Sleep(80 * time.Millisecond)
	play(a, reportItem("BV1rushed2", "2"), 0, 900*time.Second)
	time.Sleep(80 * time.Millisecond)
	play(a, reportItem("BV1rushed3", "3"), 0, 900*time.Second)

	// 只放开最后一轮：前两轮的问 aid 一回来就发现代次变了。
	go func() {
		time.Sleep(100 * time.Millisecond)
		close(blocked)
	}()
	untilBeats(t, b, "1", 1)

	// 钉的是「**开出去的那一轮**只能是最后一轮」：B 站那边「开一轮」就是往
	// 历史记录里加一条，前两轮各加一条的话，用户点三下就得到三条他没听完的
	// 记录。
	starts := b.beatsOf("1")
	if len(starts) != 1 || starts[0].Get("bvid") != "BV1rushed3" {
		t.Errorf("开出去的轮次是 %v，want 只有 BV1rushed3 一轮", bvidOf(starts))
	}
	// ⚠️ 被跳过的那几轮**排好的结束那一条仍然会发出去**——`live` 给上一轮
	// 放行一格是故意的（每一次切歌都不发结束是更糟的毛病）。所以这里只钉
	// 「第一轮的在途消息一条都不许发」：那一条连头都没开成功，发出去就是
	// 凭空造一条记录。
	for _, f := range b.beatsOf("0") {
		if f.Get("bvid") == "BV1rushed" {
			t.Errorf("第一轮明明已经不在了，还有心跳发出去：%v", f)
		}
	}
}

// bvidOf 列出这些心跳各记的是哪一首。
func bvidOf(beats []url.Values) []string {
	out := make([]string, 0, len(beats))
	for _, f := range beats {
		out = append(out, f.Get("bvid"))
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// 三、退登
// ─────────────────────────────────────────────────────────────────────────────

// TestLoggingOutStopsTheReporting 盯退登。
//
// 🔴 上报是写操作，它写进用户**真实的**账号。不停的话：jar 刚被清空，
// 那条 goroutine 还在每 30 秒往 B 站打一次心跳；而**换人登录之后**它还在
// 发——那些进度会被记到新登录的那个人名下，而那是他没听过的歌。
func TestLoggingOutStopsTheReporting(t *testing.T) {
	a, b := newReportRig(t)
	it := reportItem("BV1logout", "111")

	play(a, it, 60*time.Second, 900*time.Second)
	untilBeats(t, b, "1", 1)

	if err := a.Logout(); err != nil {
		t.Fatalf("退登失败：%v", err)
	}

	// 退登之后这一首还在放着（我们不因为退登就停歌），但不该再记了。
	opensAfter := b.opensCount()
	beatsAfter := len(b.allBeats())
	for i := 0; i < 12; i++ {
		play(a, it, time.Duration(60+i)*time.Second, 900*time.Second)
		time.Sleep(25 * time.Millisecond)
	}
	if n := b.opensCount(); n != opensAfter {
		t.Errorf("退登之后又多开了 %d 次头", n-opensAfter)
	}
	// ⚠️ **不要在外面再套一把 `b.mu`**：`allBeats` 自己已经加了锁，
	// 而 `sync.Mutex` 不可重入 —— 套上去就是一次自死锁，整个包的测试
	// 会挂到 10 分钟超时才报出来，而不是指着你这一行。
	after := b.allBeats()[beatsAfter:]
	if len(after) != 0 {
		t.Errorf("退登之后还发了 %d 条心跳：%v", len(after), playTypesOf(after))
	}
}

// TestTheProgressNeverLandsOnTheNextAccount 盯换人登录。
//
// 比的是**账号身份**（DedeUserID）而不是 `Manager.Generation()`：代次每换
// 一次登录态就涨一，而一次扫码登录（无论成不成功）必然涨一——拿它当判据
// 的话，用户正听着歌、手指点了一下登录，历史记录就从那一刻起静悄悄地不记了。
// 而真正要防的那件事是「A 的进度写进 B 的账号」。
func TestTheProgressNeverLandsOnTheNextAccount(t *testing.T) {
	a, b := newReportRig(t)
	it := reportItem("BV1mine", "222")

	play(a, it, 30*time.Second, 900*time.Second)
	untilBeats(t, b, "1", 1)

	// 退登之后换一个人登录。
	if err := a.Logout(); err != nil {
		t.Fatal(err)
	}
	a.account.mgr.Adopt(Session{
		SESSDATA: "other-sessdata", BiliJCT: "other-jct", DedeUserID: "8888",
	})

	before := len(b.allBeats())
	for i := 0; i < 12; i++ {
		play(a, it, time.Duration(30+i)*time.Second, 900*time.Second)
		time.Sleep(25 * time.Millisecond)
	}
	for _, f := range b.allBeats()[before:] {
		if f.Get("mid") == "8888" {
			t.Fatalf("上一个账号的进度被记到了 8888 名下：%v", f)
		}
	}
	// 也不能给新账号**开一轮**：那一轮从头到尾都是 2333 的。
	if n := b.opensCount(); n != 1 {
		t.Errorf("换人之后又开了 %d 次头（共 %d），want 1", n-1, n)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 四、失败不许影响播放
// ─────────────────────────────────────────────────────────────────────────────

// TestAFailedReportDoesNotReachTheUser 上报失败什么都不许冒出来。
//
// 网络抖一下不该在界面上有一句话，更不该让歌停住。这条钉的是两半：
// 播放条上没有红字，而上报那一路被 B 站拒了（-101）之后**继续在发**——
// 不能因为一次失败就再也不记了。
func TestAFailedReportDoesNotReachTheUser(t *testing.T) {
	a, b := newReportRig(t)
	b.mu.Lock()
	b.failBeats = -101
	b.mu.Unlock()

	it := reportItem("BV1resilient", "333")
	play(a, it, 0, 900*time.Second)
	for i := 0; i < 6; i++ {
		play(a, it, time.Duration(i)*time.Second, 900*time.Second)
		time.Sleep(25 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond)

	if err := a.Snapshot().Err; err != "" {
		t.Errorf("播放条上冒出了一句：%s", err)
	}
	// B 站拒了，但它**一直在拒**——我们这边必须一次都没停过。
	if n := len(b.allBeats()); n < 5 {
		t.Errorf("B 站拒了之后我们只发了 %d 次心跳——失败一次就不记了", n)
	}
	// 位置还在往前走：上报这条路上没有任何东西能卡住播放。
	if st := a.Snapshot(); st.Err != "" {
		t.Errorf("上报失败留在状态里了：%q", st.Err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 五、退出与换客户端
// ─────────────────────────────────────────────────────────────────────────────

// reporterOf 取 App 上那个上报器。
func reporterOf(t *testing.T, a *App) *playReporter {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.report == nil {
		t.Fatal("上报器没装上")
	}
	return a.report
}

// TestClosingTheAppStopsTheReportingGoroutine 盯退出把那条 goroutine 收了。
//
// 上报这一摊自己起了一条 goroutine（`run`）。放着不管的话，应用退出去之后它
// 还在往 B 站打请求——而「退出应用」到「进程真的没了」之间还有一段时间，
// 那一段时间里用户以为已经退出了，账号里却还在长播放记录。
//
// 量的是 `wg` 而不是 `runtime.NumGoroutine`：后者会被解码器、fixture 服务器
// 那几条一并算进来，说不清是谁没走。
func TestClosingTheAppStopsTheReportingGoroutine(t *testing.T) {
	a, _ := newReportRig(t)
	r := reporterOf(t, a)

	// 🔴 **先 Close 再等**。反过来写的话这条测试量的是「等一条永远不结束的
	// goroutine」，它在 3 秒后报红，而那和 `Close` 有没有收干净毫无关系。
	a.Close()

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("App.Close 之后上报那条 goroutine 还在跑")
	}

	// 关掉了之后再 `sync` 一拍：它必须什么都不做，而不是往一条已经没人
	// 读的队列里塞消息。
	r.sync(reportItem("BV1after", "1"), time.Second, time.Second, true, false)

	// Close 可以重复调（`App.Close` 自己那道门也会挡一层）。
	a.Close()
	a.Close()
}

// TestMountingStreamingAgainRetiresTheOldReporter 盯换客户端时上一条得走掉。
//
// 换了客户端却不关旧的：那条 goroutine 还拿着**旧的** B 站客户端在发，而那个
// 客户端的 cookie jar 可能已经被换掉了——于是进度发到了一个说不清是谁的会话上。
func TestMountingStreamingAgainRetiresTheOldReporter(t *testing.T) {
	a, _ := newReportRig(t)
	old := reporterOf(t, a)

	// 换一份客户端重挂（生产里 `MountStreaming` 只在开窗口前调一次，
	// 但「只调一次」不是一条可以被代码依赖的保证）。
	b := newReportBili(t)
	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(b.URL)
	a.MountStreaming(Streaming{Client: cli, Media: b.Client(), TempDir: t.TempDir()})

	done := make(chan struct{})
	go func() {
		old.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("重挂在线播放之后，上一个上报器的那条 goroutine 还在跑")
	}
	if reporterOf(t, a) == old {
		t.Fatal("重挂了却还在用旧的上报器")
	}
}

// TestAViewThatFailsIsNotAskedAgainAndAgain 「问不到 aid」不许每拍问一次。
//
// 进度条的时钟是 20 拍一秒。问一次失败不记住的话，实测那一秒能发 20 次
// `/x/web-interface/view`——把一个「这一首不记」的小毛病变成打爆 B 站。
func TestAViewThatFailsIsNotAskedAgainAndAgain(t *testing.T) {
	a, b := newReportRig(t)
	b.mu.Lock()
	b.viewFails = true
	b.mu.Unlock()

	it := reportItem("BV1noaid", "444")
	for i := 0; i < 40; i++ {
		play(a, it, time.Duration(i)*time.Second, 900*time.Second)
	}
	time.Sleep(150 * time.Millisecond)

	if n := b.viewCount(); n > 2 {
		t.Errorf("问不到 aid 时把稿件详情问了 %d 次——每一拍都在问", n)
	}
	if n := b.opensCount(); n != 0 {
		t.Errorf("问不到 aid 还是开了 %d 次头", n)
	}
}
