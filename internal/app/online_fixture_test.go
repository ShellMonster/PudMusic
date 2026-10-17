package app_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// 一台假装的「B 站」，在线播放这一层的所有用例都用它。
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

	// dark 是「CDN 掉线」：请求照收，续传的头也给，但一个字节都不发就掐断。
	//
	// 为什么不是「什么都不回」：那种形态要靠看门狗掐断（默认 20 秒），
	// 而这一层要验的是缓冲态和位置，连不上的形态更快也更贴近「网络断了」。
	// 看门狗那条路在 audio 包里测（`TestANetworkOutageBuffers…`）。
	//
	// ⚠️ 续传头必须照给：不发 `Content-Range` 的话客户端会看到 200，
	// 然后按「服务器不认续传」把缓存清空从头下——那就不是「从断点接上」了。
	dark atomic.Bool
}

// setDark 开关「CDN 掉线」。
//
// ⚠️ 这里**不需要**放行闸：dark 期间 `serveMedia` 是直接掐连接的（请求收下、
// 续传的头给上、正文一个字节不发就 `ErrAbortHandler`），没有请求会挂着。
// 早先这里有一个没人读的 `darkGate`，注释却说它「让请求卡住」——注释与
// 代码不符，比没有更糟。
func (f *streamFixture) setDark(dark bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dark.Store(dark)
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

	f.mu.Lock()
	data, cut := f.media, f.failMediaAfter
	f.mu.Unlock()

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

	if f.dark.Load() {
		// 头给了、字节不给：客户端那边是一次「连上了但下不动」。
		panic(http.ErrAbortHandler)
	}

	if cut > 0 && start < int(cut) {
		w.Write(data[start:cut])
		w.(http.Flusher).Flush()
		f.mediaOnce.Do(func() { <-f.mediaGate })
		panic(http.ErrAbortHandler)
	}
	w.Write(data[start:])
}

// setFailAfter 改「CDN 只发到第几个字节就掐断」。
//
// 走锁是因为 `serveMedia` 在别的 goroutine 里读它，直接赋值会被 `-race`
// 报成数据竞争（而红的会是测试，不是产品代码）。
func (f *streamFixture) setFailAfter(n int) {
	f.mu.Lock()
	f.failMediaAfter = n
	f.mu.Unlock()
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
		f.setDark(false)
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
//
// ⚠️ **必须排除 `Opening`**：用户点一首之后、播放器建好之前，歌名和 `HasTrack`
// 就已经是对的（`pendingTitle`），而那时一个采样都还没出来。没排除的话
// `playing` 会在「刚点下去」那一刻就返回，后面所有「它在放」的断言就都跑在
// 一个还没打开的播放器上了。
func playing(t *testing.T, a *app.App, title string) {
	t.Helper()
	waitFor(t, 30*time.Second, "「"+title+"」开始出声", func() bool {
		st := a.Snapshot()
		return st.HasTrack && st.Title == title && !st.Opening
	})
}

// playbackWatch 是采样 goroutine 和测试主线程之间的那一小块共享状态。
//
// 为什么要有它：采样 goroutine 写、主线程在 `waitFor` 里读，**裸变量
// 就是一次数据竞争**，`-race` 一跑就红，而且红的还是测试而不是产品代码。
// 用一把锁换掉「同步读到」那点运气。
type playbackWatch struct {
	mu        sync.Mutex
	buffering bool
	sentence  string
}

func (w *playbackWatch) markBuffering() {
	w.mu.Lock()
	w.buffering = true
	w.mu.Unlock()
}

func (w *playbackWatch) markSentence(s string) {
	w.mu.Lock()
	if w.sentence == "" {
		w.sentence = s
	}
	w.mu.Unlock()
}

func (w *playbackWatch) get() (bool, string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffering, w.sentence
}
