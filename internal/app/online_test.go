package app_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	// playQueries 记取流接口每次被叫时带的查询串（`withCid` 的用例要看 cid 有没有
	// 真的带上）。
	playQueries []url.Values

	// musicTier 是音乐库取流接口会给回来的那一档（0..3，**-1 是试听片段**）。
	//
	// 🔴 -1 那一条是这一组最要紧的用例：B 站对没权限的那一档不报错，回的是一段
	// 试听——地址能下、字节能播、播放器能出声，然后在用户以为歌还没放完的地方
	// 停住，界面上一个错都没有。
	musicTier int
	// musicCDN 是那条流指向的地址。
	musicCDN string
	// musicGated 为真时这台假服务器模拟**会员曲**：要 320K 或无损会拿回**试听
	// 片段**，要 192K / 128K 才给整首。`serveSongURL` 里的 `qualities` 表和它
	// 一致——表是退档退到哪一档的唯一依据，两边不对上就等于拿假参数在赌。
	musicGated bool
	// songCalls 数音乐库取流接口被打了多少次（退档会打第二次）。
	songCalls atomic.Int32
	// songInfoCalls 数元信息接口被打了多少次。
	songInfoCalls atomic.Int32
	// songInfoFails 为真时元信息那一趟回 500（元信息是锦上添花，问不到不该拦住播放）。
	songInfoFails bool

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
		f.servePlayURL(w, r.URL.Query())

	case r.URL.Path == "/audio/music-service-c/url":
		f.serveSongURL(w, r.URL.Query())

	case r.URL.Path == "/audio/music-service-c/web/song/info":
		f.serveSongInfo(w)

	case strings.HasPrefix(r.URL.Path, "/cdn/"):
		f.serveMedia(w, r)

	default:
		http.NotFound(w, r)
	}
}

// serveSongURL 回音乐库的取流。
//
// ⚠️ **外壳用 `msg` 不是 `message`**：真接口（`audio/music-service-c/url`）回的是
// `msg`。照着投稿那条造的话「错的那一句中文原因」永远测不到。
func (f *streamFixture) serveSongURL(w http.ResponseWriter, q url.Values) {
	f.songCalls.Add(1)
	tier := f.musicTier
	if f.musicGated {
		switch q.Get("quality") {
		case "2", "3":
			// 🔴 B 站对「没权限的那一档」**不报错**，回的是试听片段。
			tier = int(bili.MusicTierPreview)
		}
	}
	writeStreamJSON(w, map[string]any{"code": 0, "msg": "success", "ttl": 1,
		"data": map[string]any{
			"sid":     1293494,
			"type":    tier,
			"timeout": 10800,
			"size":    12345678,
			"cdns":    []string{f.musicCDN},
			"title":   "月光",
			"cover":   "",
			"qualities": []any{
				// ⚠️ **320K 和无损都要会员**：这样「退到不高于用户要的那一档、
				// 又不用会员的最好那一档」才有得算，而它该退到 192K。
				// 普通歌的表在 bili 包自己的用例里。
				map[string]any{"type": 3, "require": 1},
				map[string]any{"type": 2, "require": 1},
				map[string]any{"type": 1, "require": 0},
				map[string]any{"type": 0, "require": 0},
			},
		}})
}

// serveSongInfo 回音乐库的元信息。
func (f *streamFixture) serveSongInfo(w http.ResponseWriter) {
	f.songInfoCalls.Add(1)
	if f.songInfoFails {
		http.Error(w, "这一趟挂了", http.StatusBadGateway)
		return
	}
	writeStreamJSON(w, map[string]any{"code": 0, "msg": "success", "ttl": 1,
		"data": map[string]any{
			"id": 1293494, "uid": 900, "uname": "某某音乐台",
			"author": "夜灯", "title": "月光", "cover": "https://i0.hdslb.com/c.jpg",
			"duration": 214, "bvid": "", "cid": 0, "collectIds": []int{},
		}})
}

func (f *streamFixture) servePlayURL(w http.ResponseWriter, q url.Values) {
	f.mu.Lock()
	f.playQueries = append(f.playQueries, q)
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

	// Content-Length 写的是**这一次实际会发多少**，不是整段长度。写成总长的话，
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
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh192), 110836))

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
	if st.Quality != "192K" {
		t.Errorf("音质徽章 = %q，want 192K", st.Quality)
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
	f.onPlayURL(dashFor(stale, int(bili.QualityHigh192), 110836))
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
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh192), 110836))

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
	f.onPlayURL(dashFor(cdnURL(f.URL, "v1.flac", time.Hour), int(bili.QualityHigh192), 110836))
	f.onPlayURL(dashFor(cdnURL(f.URL, "v2.flac", time.Hour), int(bili.QualityHigh192), 110836))

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

	// ⚠️ 「位置往前走」**不代表换过流**。源里已经缓冲下来的那几秒还会照放，
	// 而泵是另一条 goroutine：它发现连接断了、正在走换流那条路（一次带
	// 签名的往返，200~400 ms），界面上的位置就先动了。`-race` 下一慢，
	// 这个窗口宽到每次都踩中——于是 `playCalls` 还停在 1，下一句断言就红。
	//
	// 所以这里等的是**两件同时成立**：换流真的发生了（取流接口被打第二次），
	// 而且位置还在往前走。后一句断言才有意义。
	waitFor(t, 30*time.Second, "换流之后继续往前播", func() bool {
		return f.playCalls.Load() >= 2 && a.Snapshot().Position > before
	})
	if st := a.Snapshot(); st.Err != "" {
		t.Errorf("换流之后播放条上有错：%s", st.Err)
	}
	if q := a.Snapshot().Quality; q != "192K" {
		t.Errorf("换流之后徽章 = %q，want 192K", q)
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

	f.onPlayURL(dashFor(cdnURL(f.URL, "v1.flac", time.Hour), int(bili.QualityHigh192), 110836))
	f.onPlayURL(dashFor(cdnURL(f.URL, "v2.flac", time.Hour), int(bili.QualityMid128), 192000))

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "会降级的一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "会降级的一首")
	if q := a.Snapshot().Quality; q != "192K" {
		t.Fatalf("一开始的徽章 = %q，want 192K", q)
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
			"id": int(bili.QualityHigh192), "bandwidth": 110836,
			"codecs": "mp4a.40.2", "mimeType": "audio/mp4", "baseUrl": url,
		}},
		"flac": map[string]any{"display": true, "audio": map[string]any{
			"id": int(bili.QualityHiResLossless), "bandwidth": 1_400_000,
			"codecs": "flac", "mimeType": "audio/mp4", "baseUrl": url,
		}},
	})

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "有无损的一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "有无损的一首")
	if q := a.Snapshot().Quality; q != "Hi-Res 无损" {
		t.Errorf("徽章 = %q，want Hi-Res 无损（有 flac 段时自动档必须先拿它）", q)
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
	f.onPlayURL(dashFor(cdnURL(f.URL, "v1.flac", time.Hour), int(bili.QualityHigh192), 110836))
	f.onPlayURL(dashFor(cdnURL(f.URL, "v2.m4s", time.Hour), int(bili.QualityHigh192), 110836))

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

// TestMissingCidIsResolvedBeforePlaying 盯「只给 bvid 的条目（搜索结果）能真的起播」。
//
// 这是评审挑出来的那条 critical：B 站的搜索结果**只给 bvid**，而 `playableOnline`
// 当年要求 cid，于是搜出来的歌一首都放不了，播放条上红字写「缺稿件 id 或分 P
// id」——用户按的就是搜索结果，说他缺一个他压根拿不到的东西。
//
// 补 cid 的那一步在这里：**放歌的那一刻**问一次分 P（`withCid`），而不是在搜索那一
// 层把一页 24 条都问一遍。判据是取流请求里**真的带上了那个 cid**——只验「放出来了」
// 的话，B 站不给 cid 时按第一集返回也能过，而那正是要消掉的「放错那一集」。
func TestMissingCidIsResolvedBeforePlaying(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh192), 110836))

	var asked sync.Map // bvid → struct{}
	f.on("/x/player/pagelist", func(w http.ResponseWriter, r *http.Request) {
		asked.Store(r.URL.Query().Get("bvid"), struct{}{})
		// 🔴 `data` 是**数组**（上游 `player-pagelist.ts:18`）。写成对象的话补 cid
		// 静默失败，歌放不出来，而这一层把错吞掉就什么都看不见了。
		writeStreamJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
			"data": []any{map[string]any{"cid": 777, "page": 1, "part": "第一集", "duration": 260}}})
	})

	a := newOnlineApp(t, f, t.TempDir())
	// 这一条**只有 bvid**，和搜索结果一模一样。
	if err := a.Enqueue(app.Item{Source: "online", Type: "mv", Bvid: "BVonly", Title: "只有 bvid 的一首"}); err != nil {
		t.Fatalf("只给 bvid 的一条被当场拒了：%v", err)
	}
	playing(t, a, "只有 bvid 的一首")

	if _, ok := asked.Load("BVonly"); !ok {
		t.Errorf("放歌之前没有问过一次分 P")
	}
	if got := lastPlayURLCid(f); got != "777" {
		t.Errorf("取流请求带的 cid 是 %q，want 777（问来的那一集）", got)
	}
}

// TestMissingCidSaysWhyWhenThePartCannotBeAsked 盯「问不出分 P 就说清楚，不硬发」。
//
// 🔴 这一条盯的是**不**去赌：第一版在这里问不到分 P 就照样发一个没有 cid 的取流请求，
// 赌「B 站会按第一集给」。那个赌没人验过（上游 `player-playurl.ts:9` 把 cid 写成
// 必填，而本地 fixture 放不放假参数），真接口回一段不知道是哪一集的音频时，用户听到
// 的是一首不是他点的歌，而界面上什么都看不出来。
func TestMissingCidSaysWhyWhenThePartCannotBeAsked(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh192), 110836))
	f.on("/x/player/pagelist", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "风控", http.StatusForbidden)
	})

	a := newOnlineApp(t, f, t.TempDir())
	// 入队**不报错**：缺 cid 不是同步能判出来的「不能放」，是「要去问一次」。
	if err := a.Enqueue(app.Item{Source: "online", Type: "mv", Bvid: "BVonly", Title: "问不到分 P"}); err != nil {
		t.Fatalf("只给 bvid 的一条被当场拒了：%v", err)
	}

	waitFor(t, 15*time.Second, "播放条上说明白原因", func() bool {
		return a.Snapshot().Err != ""
	})
	if a.Snapshot().HasTrack {
		t.Errorf("问不出分 P 还照样出声了")
	}
	if got := a.Snapshot().Err; !strings.Contains(got, "问不出") {
		t.Errorf("播放条上写的是 %q，要说清是「问不出这一集」", got)
	}
	// 一个没有 cid 的取流请求都不该发。
	for i, c := range lastPlayURLCids(f) {
		t.Errorf("第 %d 次取流还是发出去了，cid=%q", i, c)
	}
}

// lastPlayURLCid 是最近一次取流请求带的 cid。取流是异步发的，所以带一个重试。
func lastPlayURLCid(f *streamFixture) string {
	waitFor(f.t, 10*time.Second, "取流接口出请求", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.playQueries) > 0
	})
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.playQueries[len(f.playQueries)-1].Get("cid")
}

// lastPlayURLCids 是录到的每一次取流请求带的 cid（问不出分 P 时应该一个都没有）。
func lastPlayURLCids(f *streamFixture) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.playQueries))
	for _, q := range f.playQueries {
		out = append(out, q.Get("cid"))
	}
	return out
}

// TestUnplayableOnlineItemsAreRefusedSynchronously 盯「参数不全当场拒」。
//
// 这两种是同步就能看出来的，调用方（PlayFrom / Enqueue）靠返回值把队列滚回去。
// 放它们进去的话，用户会看到一个永远打不开的条目躺在队列里。
//
// ⚠️ 「缺 cid」**不在这一列里**了：B 站的搜索结果只给 bvid，而搜索页那整个 Tab 都是
// 这样的条目（见 `service/bili/search.go` 文件头）。缺 cid 不是「同步就能看出来的
// 不能放」，是「还不知道该放哪一集，要问一次」——问的那一趟在
// `TestMissingCidIsResolvedBeforePlaying` 里。把它留在这儿的话，用户搜出来的歌
// 一首都放不了，播放条上红字写「缺稿件 id 或分 P id」（那时 cid 还是判据之一）——说他缺一个他压根拿不到的
// 东西，那句话是错的。
//
// ⚠️ 同样不在这一列里的还有**带 sid 的音乐库条目**：以前它在，是因为那一路
// （`/audio/music-service-c/url`）没接上，于是判据 `bili.Media.Playable` 照抄了
// Biu 的 `[2, 12]` 却只实现了 2——「可放」在界面上成了一句谎话。现在那一路接上了，
// 带 sid 的条目真的放得出来（`TestAMusicLibraryEntryActuallyPlays`），
// **判据和实际能放的一致了**。少 sid 的还在这一列里。
func TestUnplayableOnlineItemsAreRefusedSynchronously(t *testing.T) {
	a := app.New()
	t.Cleanup(a.Close)

	// ⚠️ **每一句都要能回答「为什么」和「还能怎么办」**（票 #209）：只说
	// 「不能放」的话，用户既不知道出了什么事，也不知道自己该干什么。
	// 所以这里两半都钉住——「为什么」按种类各不相同，「怎么办」也各不相同。
	for _, c := range []struct {
		it   app.Item
		why  string
		next string
	}{
		{app.Item{Source: "online", Type: "mv", Cid: "1", Title: "缺 bvid"},
			"不是投稿", "搜同名"},
		{app.Item{Source: "online", Type: "audio", Title: "缺音频 id"},
			"音乐库", "重新收藏"},
		{app.Item{Source: "online", Type: "", Title: "认不出来的类型"},
			"不是投稿", "搜同名"},
	} {
		err := a.PlayFrom(c.it)
		if err == nil {
			t.Errorf("%s 居然放进去了", c.it.Title)
			continue
		}
		if !strings.Contains(err.Error(), c.why) {
			t.Errorf("%s 的错没说清「为什么」：%v", c.it.Title, err)
		}
		if !strings.Contains(err.Error(), c.next) {
			t.Errorf("%s 的错没说清「还能怎么办」：%v", c.it.Title, err)
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
		t.Errorf("err = %v，want 一句说清「在线播放暂时用不了」的话", err)
	}
}

// TestMidPlaybackDeathSurfacesOnThePlaybar 盯规格里的负路径「播放中断网」。
//
// `PLAN.md` 阶段 2 的负路径表里写着：「播放中断网——**不能被当成播完**——
// 要停在缓冲态并提示」。这条钉的就是它：CDN 下到一半掐断连接之后，播放条上
// 要出现一句人话，而不是安静地停住。
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
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh192), 110836))

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "半路死掉的一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "半路死掉的一首")

	// 等到它真的停：位置不再前进。
	waitFor(t, 60*time.Second, "播放停住", func() bool {
		p1 := a.Snapshot().Position
		if p1 == 0 {
			return false
		}
		time.Sleep(700 * time.Millisecond)
		return a.Snapshot().Position == p1
	})
	waitFor(t, 15*time.Second, "播放条上出现一句人话", func() bool {
		return a.Snapshot().Err != ""
	})

	got := a.Snapshot().Err
	// ⚠️ 必须是「拉不动了」而不是解码器那边的抱怨。字节断了之后 flacdec
	// 先报的是「这一帧的 CRC 对不上」——把**后果**说给用户听，用户会以为
	// 歌坏了、缓存坏了，于是去清缓存重下，而正确的反应是「换个网」。
	if strings.Contains(got, "CRC") || strings.Contains(got, "flacdec") {
		t.Errorf("说的是解码器的抱怨，不是真正的原因：%q", got)
	}
	if !strings.Contains(got, "连不上") {
		t.Errorf("话没说清是「拉不动了」：%q", got)
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
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh192), 110836))

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
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh192), 110836))

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
	f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh192), 110836))

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
		"id": int(bili.QualityHigh192), "bandwidth": 110836, "codecs": "flac",
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

// ─────────────────────────────────────────────────────────────────────────────
// B 站音乐库（sid）
// ─────────────────────────────────────────────────────────────────────────────

// musicItem 是收藏夹里的一条音乐库条目（type=12）。
func musicItem(sid, title string) app.Item {
	return app.Item{Source: "online", Type: "audio", Sid: sid, Title: title}
}

// newMusicFixture 备好一台会放音乐库的「B 站」。
//
// ⚠️ 默认模拟的是**会员曲**：自动档要 320K，拿回来的是试听片段，然后退到 192K。
// 让「退档」那条路默认就走到了——它是最容易写错的一段（退错了用户就听到一档
// 自己没点的音质，而界面上看不出），而 fixture 不这么设的话那条路一次都不跑。
func newMusicFixture(t *testing.T) *streamFixture {
	t.Helper()
	f := newStreamFixture(t)
	f.media = readFlac(t)
	f.musicCDN = cdnURL(f.URL, "song.m4a", 2*time.Hour)
	f.musicTier = int(bili.MusicTier192)
	f.musicGated = true
	return f
}

// TestAMusicLibraryEntryActuallyPlays 是这一票的主线用例。
//
// 收藏夹里 `type=12` 的条目以前照常画、照常进队列，按播放必然报错。现在整条链
// 走通了：队列 → 元信息 → 音乐库取流 → CDN → 字节源 → flacdec → 播放器。
//
// ⚠️ 断言的是解码器真出了样本（位置在走），不是「没报错」。
func TestAMusicLibraryEntryActuallyPlays(t *testing.T) {
	f := newMusicFixture(t)
	a := newOnlineApp(t, f, t.TempDir())

	if err := a.Enqueue(musicItem("1293494", "音乐库的一首")); err != nil {
		t.Fatalf("音乐库条目被当场拒了：%v", err)
	}
	playing(t, a, "音乐库的一首")

	waitFor(t, 15*time.Second, "进度条往前走", func() bool {
		return a.Snapshot().Position > 0
	})

	st := a.Snapshot()
	if st.Err != "" {
		t.Errorf("播放条上有错：%s", st.Err)
	}
	// 退到 192K 之后徽章要跟着改。写死的话界面上显示的是用户**没点的那一档**。
	if st.Quality != "192K" {
		t.Errorf("音质徽章 = %q，want 192K", st.Quality)
	}
	if n := f.playCalls.Load(); n != 0 {
		t.Errorf("投稿那条取流接口被打了 %d 次，want 0（音乐库不走它）", n)
	}
	if n := f.songInfoCalls.Load(); n != 1 {
		t.Errorf("元信息接口被打了 %d 次，want 1（放歌之前问一次）", n)
	}
	// 一次要 + 一次退档。
	if n := f.songCalls.Load(); n != 2 {
		t.Errorf("音乐库取流接口被打了 %d 次，want 2（一次要 320K、一次退到 192K）", n)
	}
}

// TestAMusicLibraryEntryFillsInTheAuthor 盯「问一次元信息」是真的补上了东西。
//
// 播放条的艺人名和系统媒体会话的艺人字段都吃它，而收藏夹那一行给不出
// 作者名（`Media` 里只有 UP 名）。
func TestAMusicLibraryEntryFillsInTheAuthor(t *testing.T) {
	f := newMusicFixture(t)
	a := newOnlineApp(t, f, t.TempDir())
	s := newSpy()
	a.SetMediaSink(s.publish)

	// 这一条**故意不带标题和作者**：它们得靠元信息补。
	if err := a.Enqueue(musicItem("1293494", "")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "月光")

	// 艺人名一路走到系统媒体会话（锁屏上那一栏）。
	waitFor(t, 15*time.Second, "艺人名送到媒体会话", func() bool {
		return s.last().Artist == "夜灯"
	})
	if got := s.last().Artist; got != "夜灯" {
		t.Errorf("艺人 = %q，want 夜灯（song/info 的 author）", got)
	}
}

// TestAMusicLibraryEntryStillPlaysWhenSongInfoFails 元信息是锦上添花。
//
// 🔴 **问不到不许拦住播放**：取流只要 sid，元信息不是它的前提。把它变成硬依赖
// 的后果是「网络抖一下就放不出歌」，而那时候歌其实取得到——少一个作者名
// （播放条上那一句空着）比放不出歌好得多。
func TestAMusicLibraryEntryStillPlaysWhenSongInfoFails(t *testing.T) {
	f := newMusicFixture(t)
	f.songInfoFails = true
	a := newOnlineApp(t, f, t.TempDir())

	if err := a.Enqueue(musicItem("1293494", "标题本来就有")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "标题本来就有")

	if st := a.Snapshot(); st.Err != "" {
		t.Errorf("元信息问不到就不放了，播放条上写着：%s", st.Err)
	}
}

// TestAMusicPreviewNeverReachesThePlayer 这一路最要紧的一条。
//
// B 站对「没权限的那一档」不报错，回的是一段试听片段：地址能下、字节能播、
// 播放器能出声，然后在用户以为歌还没放完的地方停住。放它过去，「放不了」就
// 换成了一个更难解释的样子——界面上一个错都没有。
func TestAMusicPreviewNeverReachesThePlayer(t *testing.T) {
	f := newMusicFixture(t)
	f.musicTier = int(bili.MusicTierPreview)
	a := newOnlineApp(t, f, t.TempDir())

	if err := a.Enqueue(musicItem("1293494", "会员曲")); err != nil {
		t.Fatal(err)
	}

	waitFor(t, 15*time.Second, "播放条上说明白原因", func() bool {
		return a.Snapshot().Err != ""
	})
	if a.Snapshot().HasTrack {
		t.Error("只拿得到试听片段，却照样出声了")
	}
	got := a.Snapshot().Err
	if !strings.Contains(got, "会员") {
		t.Errorf("播放条上写的是 %q，要说清差在哪（会员）", got)
	}
	if !strings.Contains(got, "试听") {
		t.Errorf("播放条上写的是 %q，要说清拿到的是试听片段而不是整首", got)
	}
}

// TestChangingQualityKeepsAMusicLibraryTrack 换音质在音乐库这一路上**整条跑得通**。
//
// 换回来还是同一档时不该打断正在放的歌（`SameTier`），而这一条盯的是那一整条路
// 在音乐库条目上没被弄坏：换了档、没报错、歌还在放、徽章没变、位置没退。
//
// ⚠️ **它不钉 `SameTier` 的对错**：真让 `SameTier` 恒假（等于每次点音质都重开
// 播放器），这一条仍然是绿的——重开那一步会把歌**从原位置接着放**，界面上看得出
// 的只有一瞬间的接缝，无头环境量不到。钉那个逻辑的是
// `bili.TestSameTierComparesBothNumberings`（把 `SameTier` 改成恒假就红）。
// 这一条钉的是它的**四周**：接线、缓存、徽章、位置。
func TestChangingQualityKeepsAMusicLibraryTrack(t *testing.T) {
	f := newMusicFixture(t)
	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(musicItem("1293494", "换音质")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "换音质")
	waitFor(t, 15*time.Second, "先走起来", func() bool { return a.Snapshot().Position > 0 })

	before := a.Snapshot().Position
	// 点「省流量」：这台假服务器上要到的 128K 会退到 192K——和现在这一档一样。
	//
	// ⚠️ **判据一律是结果，不是睡固定时长**（同一个道理见 `collectionpage_test.go`
	// 的 `waitFrameFor` 顶上那段）：这一趟走完了 = 取流接口又被打了一次。原来
	// 这里睡两秒，慢机器上要么白等、要么不够；而这一组整体还要跑十几分钟，
	// 白等两秒是实打实的账。
	wantCalls := f.songCalls.Load() + 1
	a.SetAudioQuality(bili.AudioLow)
	waitFor(t, 15*time.Second, "换音质那一趟走完", func() bool {
		return f.songCalls.Load() >= wantCalls
	})
	// 而它**没有打断歌**：位置还在往前走。真被打断的话 `Playing` 会变 false
	// 并且不会自己回来，这一等就会超时——这比「睡完再看一眼」可靠，而且顺带
	// 给了那条 goroutine 跑完的时间（不然会偶发地红）。
	waitFor(t, 15*time.Second, "歌没被打断（位置还在往前走）", func() bool {
		st := a.Snapshot()
		return st.Playing && st.Position > before
	})

	st := a.Snapshot()
	if st.Err != "" {
		t.Errorf("换音质失败：%s", st.Err)
	}
	if st.Quality != "192K" {
		t.Errorf("徽章 = %q，want 192K", st.Quality)
	}
	if st.Position < before {
		t.Errorf("播放位置从 %v 退到了 %v：换同一档不该动位置", before, st.Position)
	}
}

// TestEveryPlayableEntryCanActuallyBeStreamed 是这一票的**不变量**。
//
// `bili.Media.Playable` 说什么能放，界面上就照着画、照着进队列——所以它判「能放」
// 的那几种，**取流那一层必须真的放得出来**。当初 sid 那一路没接时判据抄对了、
// 那一路没跟上，于是「可放」成了一句界面上的谎话，一路绿过所有测试，直到真去播。
//
// 这里把两种身份都真放一遍：走的接口不一样，漏掉哪一个都会回到当初那个形状。
func TestEveryPlayableEntryCanActuallyBeStreamed(t *testing.T) {
	t.Run("投稿", func(t *testing.T) {
		f := newStreamFixture(t)
		f.media = readFlac(t)
		f.onPlayURL(dashFor(cdnURL(f.URL, "a.flac", 2*time.Hour), int(bili.QualityHigh192), 110836))
		a := newOnlineApp(t, f, t.TempDir())
		if err := a.Enqueue(onlineItem("BV1xx", "123", "投稿")); err != nil {
			t.Fatal(err)
		}
		playing(t, a, "投稿")
	})

	t.Run("音乐库", func(t *testing.T) {
		f := newMusicFixture(t)
		a := newOnlineApp(t, f, t.TempDir())
		if err := a.Enqueue(musicItem("1293494", "音乐库")); err != nil {
			t.Fatal(err)
		}
		playing(t, a, "音乐库")
	})
}

// TestAFailedQualitySwitchDoesNotLeaveItsWordsBehind 换档失败的红字不许粘住。
//
// 🔴 场景：点「无损」→ 取流失败 → 播放条上写「这一首没有无损」，**歌照常放着**
// （那一档拿不到，但手上那条是好的）→ 用户改主意点「高音质」→ 拿回来的还是
// 正在放的那一档，于是**不打断**，直接走人。
//
// 走人的那一支原来不清 `errText`，于是那句「这一首没有无损」会一直挂到下一首
// 歌为止——而歌好好地在放、用户点的档也拿到了。**一句错的红字，比没有更糟**。
//
// `failQualitySwitch` 那段注释为失败那一半操过心（迟到的失败不许盖住后一次
// 成功），这一条是它漏掉的另一半：成功的那一半也可能留下一句旧的。
func TestAFailedQualitySwitchDoesNotLeaveItsWordsBehind(t *testing.T) {
	f := newStreamFixture(t)
	f.media = readFlac(t)
	url := cdnURL(f.URL, "a.flac", 2*time.Hour)
	// 只有 192K 这一档：**点无损一定拿不到**，而「高音质」拿回来的正是现在这一档。
	f.onPlayURL(map[string]any{"duration": 1, "audio": []any{map[string]any{
		"id": int(bili.QualityHigh192), "bandwidth": 110836, "codecs": "flac",
		"mimeType": "audio/mp4", "baseUrl": url,
	}}})

	a := newOnlineApp(t, f, t.TempDir())
	if err := a.Enqueue(onlineItem("BV1", "1", "只有 192K 的一首")); err != nil {
		t.Fatal(err)
	}
	playing(t, a, "只有 192K 的一首")

	// 点「无损」：拿不到，播放条上如实说，歌照放。
	a.SetAudioQuality(bili.AudioLossless)
	waitFor(t, 15*time.Second, "播放条上说清没有无损", func() bool {
		return a.Snapshot().Err != ""
	})
	if got := a.Snapshot().Err; !strings.Contains(got, "无损") {
		t.Fatalf("播放条上写的是 %q，want 一句说清「没有无损」的话", got)
	}

	// 改主意点「高音质」：拿回来的就是正在放的那一档 → 不打断。
	a.SetAudioQuality(bili.AudioHigh)
	waitFor(t, 15*time.Second, "那句不成立的话被清掉", func() bool {
		return a.Snapshot().Err == ""
	})
	if st := a.Snapshot(); !st.Playing || st.Quality != "192K" {
		t.Errorf("清掉那句话的时候把歌弄坏了：playing=%v 徽章=%q", st.Playing, st.Quality)
	}
}
