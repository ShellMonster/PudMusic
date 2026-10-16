package app_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
// # fixture 的形状从哪来
//
// `/x/player/wbi/playurl` 的字段名和取值抄的是 2026-10-16 打真 B 站拿到的
// 响应（`dash.audio[].id/bandwidth/codecs/baseUrl`、地址上的 `deadline`）。
// 素材用的是真的 FLAC 文件，走真的解码链——用假的字节测「能播」等于没测。
//
// 但**没有任何一条断言是在证明「B 站真的这么返回」**，那要真机回归。

// ─────────────────────────────────────────────────────────────────────────────
// fixture
// ─────────────────────────────────────────────────────────────────────────────

// flacFixturePath 是那份 FLAC 素材。
func flacFixturePath(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "service", "audio", "flacdec", "testdata", "stereo16.flac")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}
	return p
}

// streamFixture 是一台「B 站」：取流接口、扫码接口和 CDN 都在一台服务器上。
type streamFixture struct {
	*httptest.Server
	t *testing.T

	mu sync.Mutex
	// playURLs 是取流接口**依次**该回的 dash。最后一条会一直用。
	playURLs []map[string]any
	// overrides 是按路径整段替换掉默认处理。
	overrides map[string]http.HandlerFunc
	// playCalls 数取流接口被打了多少次。
	playCalls atomic.Int32

	// media 是 CDN 上那份字节。
	media []byte
	// mediaHits 数 CDN 被打了多少次。
	mediaHits atomic.Int32
	// forbidden 的 CDN 路径一律回 403（模拟「这条地址过期了」）。
	forbidden map[string]bool

	// failMediaAfter > 0 时，发到这个字节就掐断连接（模拟地址中途失效）。
	failMediaAfter int
	// mediaGate 关掉之后才真的掐断。理由见 audio 包里同名 helper。
	mediaGate chan struct{}
	mediaOnce sync.Once
}

func (f *streamFixture) onPlayURL(dash map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.playURLs = append(f.playURLs, dash)
}

func (f *streamFixture) on(path string, h http.HandlerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.overrides[path] = h
}

func (f *streamFixture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	h := f.overrides[r.URL.Path]
	f.mu.Unlock()
	if h != nil {
		h(w, r)
		return
	}

	switch {
	case r.URL.Path == "/x/web-interface/nav":
		writeStreamJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": map[string]any{"wbi_img": map[string]string{
				"img_url": "https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png",
				"sub_url": "https://i0.hdslb.com/bfs/wbi/4932caff0ff746eab6f01bf08b70ac45.png",
			}}})

	case r.URL.Path == "/x/player/wbi/playurl":
		f.servePlayURL(w)

	case strings.HasPrefix(r.URL.Path, "/cdn/"):
		f.serveMedia(w, r)

	default:
		http.NotFound(w, r)
	}
}

func (f *streamFixture) servePlayURL(w http.ResponseWriter) {
	f.mu.Lock()
	n := int(f.playCalls.Add(1)) - 1
	if n >= len(f.playURLs) {
		n = len(f.playURLs) - 1
	}
	var dash map[string]any
	if n >= 0 {
		dash = f.playURLs[n]
	}
	f.mu.Unlock()

	writeStreamJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
		"data": map[string]any{"timelength": 1000, "dash": dash}})
}

func (f *streamFixture) serveMedia(w http.ResponseWriter, r *http.Request) {
	f.mediaHits.Add(1)
	if f.forbidden[r.URL.Path] {
		http.Error(w, "地址失效了", http.StatusForbidden)
		return
	}

	data := f.media
	start := 0
	if h := r.Header.Get("Range"); strings.HasPrefix(h, "bytes=") {
		start, _ = strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(h, "bytes="), "-"))
	}
	if start > len(data) {
		http.Error(w, "越界", http.StatusRequestedRangeNotSatisfiable)
		return
	}

	// Content-Length 写的是**这一次实际会发多少**，不是整段长度。写成总长的���，
	// 声明和实际对不上，客户端读到的是 unexpected EOF。
	w.Header().Set("Content-Length", fmt.Sprint(len(data)-start))
	if start > 0 {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(data)-1, len(data)))
		w.WriteHeader(http.StatusPartialContent)
	}

	if f.failMediaAfter > 0 && start < f.failMediaAfter {
		w.Write(data[start:f.failMediaAfter])
		w.(http.Flusher).Flush()
		f.mediaOnce.Do(func() { <-f.mediaGate })
		panic(http.ErrAbortHandler)
	}
	w.Write(data[start:])
}

func writeStreamJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func newStreamFixture(t *testing.T) *streamFixture {
	t.Helper()
	f := &streamFixture{
		t:         t,
		overrides: map[string]http.HandlerFunc{},
		forbidden: map[string]bool{},
		mediaGate: make(chan struct{}),
	}
	f.Server = httptest.NewServer(f)
	t.Cleanup(func() {
		select {
		case <-f.mediaGate:
		default:
			close(f.mediaGate)
		}
		f.Close()
	})
	return f
}

// dashFor 造一份取流的 dash 段：一条有损流。
func dashFor(mediaURL string, qualityID, bandwidth int) map[string]any {
	return map[string]any{
		"duration": 1,
		"audio": []any{map[string]any{
			"id": qualityID, "bandwidth": bandwidth, "codecs": "flac",
			"mimeType": "audio/mp4", "baseUrl": mediaURL,
		}},
	}
}

// cdnURL 造一个带 deadline 的 B 站 CDN 地址。
func cdnURL(base, name string, expiresIn time.Duration) string {
	return base + "/cdn/" + name + "?deadline=" +
		strconv.FormatInt(time.Now().Add(expiresIn).Unix(), 10) + "&upsig=deadbeef"
}

// newOnlineApp 建一个挂了在线播放的 App。
//
// ⚠️ 这里 Media 直接用 fixture 的 client，不走 `CredentialTransport`——
// 凭据边界有它自己的一条用例（`TestTheMediaClientGivesTheCDNNoCookie`），
// 这里要验的是换流与接线。
func newOnlineApp(t *testing.T, f *streamFixture, dir string) *app.App {
	t.Helper()
	a := app.New()
	t.Cleanup(a.Close)

	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(f.URL)

	a.MountStreaming(app.Streaming{Client: cli, Media: f.Client(), TempDir: dir})
	return a
}

// onlineItem 是收藏夹里的一条投稿。
func onlineItem(bvid, cid, title string) app.Item {
	return app.Item{Source: "online", Type: "mv", Bvid: bvid, Cid: cid, Title: title}
}

func readFlac(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(flacFixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// playing 盯到 App 真的在放队列里那一条。
func playing(t *testing.T, a *app.App, title string) {
	t.Helper()
	waitFor(t, 30*time.Second, "「"+title+"」开始出声", func() bool {
		st := a.Snapshot()
		return st.HasTrack && st.Title == title
	})
}

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
