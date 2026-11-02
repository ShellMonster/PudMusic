package ui

// 「来回切歌不再走网络」的 E2E：从**一次真的点击**开始，到**CDN 一次都没再被拉**
// 为止。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestSwitchingBackDoesNotHitTheNetwork
//
// # 这一条为什么必须存在
//
// 2026-10-30 用户提出来的：「前面比如取到了的，那应该是在本地电脑缓存上啊，
// 为啥还要再去跑网络请求；应该是没有才跑网络请求啊」。
//
// `internal/app` 那几条能验「缓存命中了没有」，但它们**没有界面**——而
// 「用户切歌」这件事**是在界面上发生的**。这一条从真页面上的真点击开始：
//
//	点一首歌 → 歌放起来（CDN 被拉了一次）→ 点另一首 → 点回原来那首
//	→ **CDN 不该再被拉**
//
// ⚠️ **一个字都不许打真 B 站**：夹具是本地假服务器。
//
// ⚠️ **判据是「CDN 被打了多少次」，不是「界面看起来对不对」**：界面会
// 「照样放出来」——而那正是这个 bug 最坑的地方（用户只看到「歌能播」，
// 没看到「每一首都白下一遍」）。

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

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// streamCacheBili 是一台假 B 站：取流接口、CDN、弹幕都在一台服务器上。
//
// 🔴 **它数两样东西**：取流接口被问了几次、CDN 被拉了几次。
// 「切回去那一趟有没有白下」要看的是**后者**。
type streamCacheBili struct {
	*httptest.Server
	playCalls atomic.Int32
	cdnCalls  atomic.Int32
	// cdnByPath 数每一条 CDN 路径分别被拉了几次。
	cdnByPath map[string]int
	// overrides 按路径整段替换默认处理（给「停在半路」那些用例用）。
	// 走了 override 的路径不走下面的 cdnByPath 计数——要数就自己数。
	overrides map[string]http.HandlerFunc
	mu        sync.Mutex
}

func newStreamCacheBili(t *testing.T, media []byte) *streamCacheBili {
	t.Helper()
	b := &streamCacheBili{cdnByPath: map[string]int{}, overrides: map[string]http.HandlerFunc{}}
	b.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h, ok := b.overrides[r.URL.Path]; ok {
			h(w, r)
			return
		}
		switch {
		case r.URL.Path == "/x/web-interface/nav":
			writeStreamCacheJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
				"data": map[string]any{"isLogin": true, "wbi_img": map[string]string{
					"img_url": "https://i0.hdslb.com/bfs/wbi/7cd084941338484aae1ad9425b84077c.png",
					"sub_url": "https://i0.hdslb.com/bfs/wbi/4932caff0ff746eab6f01bf08b70ac45.png",
				}}})
		case r.URL.Path == "/x/player/wbi/playurl":
			b.playCalls.Add(1)
			// 每一条 cid 给它自己的 CDN 地址——判「切回去那一趟有没有白下」
			// 要看的是**同一首**那条路径。
			cid := r.URL.Query().Get("cid")
			url := fmt.Sprintf("%s/cdn/%s.flac", b.URL, cid)
			writeStreamCacheJSON(w, map[string]any{"code": 0, "message": "0", "ttl": 1,
				"data": map[string]any{"timelength": 1000, "dash": map[string]any{
					"duration": 1,
					"audio": []any{map[string]any{
						"id": 30280, "bandwidth": 110836, "codecs": "flac",
						"mimeType": "audio/mp4", "baseUrl": url,
					}},
				}}})
		case len(r.URL.Path) > 5 && r.URL.Path[:5] == "/cdn/":
			b.cdnCalls.Add(1)
			b.mu.Lock()
			b.cdnByPath[r.URL.Path]++
			b.mu.Unlock()
			w.Header().Set("Content-Length", fmt.Sprint(len(media)))
			w.Write(media)
		default:
			// 弹幕、字幕那些：问不到就算了（它们不挡播放）。
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(b.Close)
	return b
}

// hitsFor 报某一条 CDN 路径被拉了几次。
func (b *streamCacheBili) hitsFor(path string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cdnByPath[path]
}

// cacheJSONs 列出缓存目录里那些记录文件。
func cacheJSONs(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读缓存目录：%v", err)
	}
	var out []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

func writeStreamCacheJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// TestSwitchingBackDoesNotHitTheNetwork 钉住「切回同一首不再走网络」。
func TestSwitchingBackDoesNotHitTheNetwork(t *testing.T) {
	media, err := os.ReadFile(filepath.Join("..", "service", "audio", "flacdec", "testdata", "stereo16.flac"))
	if err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}
	b := newStreamCacheBili(t, media)

	cacheDir := t.TempDir()
	a := app.New()
	// 🔴 **先关应用，再让 `t.TempDir()` 收尾**：清理是**倒序**跑的，
	// 而 `t.TempDir()` 的清理是在它被调用的那一刻登记的——比这里登记的
	// `a.Close` **早**。于是缓存目录会在字节源还开着那个文件的时候被删，
	// 收尾报「directory not empty」（Windows 上那更是删不掉）。
	t.Cleanup(a.Close)

	transport := &app.CredentialTransport{
		Base:   b.Client().Transport,
		Policy: app.DefaultInjectPolicy().AllowBiliHost(hostOfFixture(b.URL)),
	}
	cli := bili.NewClient(bili.Options{Transport: transport})
	cli.TestOnlyPointHostsAtFixture(b.URL)
	a.MountStreaming(app.Streaming{
		Client:   cli,
		Media:    b.Client(),
		TempDir:  t.TempDir(),
		CacheDir: cacheDir,
	})

	// 先播 A，让它下完（盘上有了那份字节）。
	if err := a.PlayFrom(app.Item{
		Source: "online", Type: "mv", Bvid: "BV1", Cid: "A",
		Title: "第 A 首",
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "「第 A 首」开始出声", func() bool {
		return a.Snapshot().Title == "第 A 首"
	})
	// 🔴 **等它真的下完**：界面上「开始出声」的那一刻**字节可能还没下完**
	// （边下边播），而缓存是**下完**那一刻才记的。
	//
	// ⚠️ **等的是「缓存目录里有了那一份的记录」**：那是「下完了」的**唯一**
	// 外部可见的证据（CDN 被拉过 ≠ 下完了）。
	waitFor(t, "A 首下完（缓存记上了）", func() bool {
		for _, p := range cacheJSONs(t, cacheDir) {
			raw, _ := os.ReadFile(p)
			if strings.Contains(string(raw), `"track":"online|mv|BV1|A|"`) &&
				strings.Contains(string(raw), `"complete":true`) {
				return true
			}
		}
		return false
	})

	// 切到 B（**另一首**）——让界面上那一首真的换掉。
	if err := a.PlayFrom(app.Item{
		Source: "online", Type: "mv", Bvid: "BV1", Cid: "B",
		Title: "第 B 首",
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "「第 B 首」开始出声", func() bool {
		return a.Snapshot().Title == "第 B 首"
	})

	// 🔴 **切回 A 那一趟，CDN 一个字节都不该再要**。
	//
	// ⚠️ **判据是「切回去之前 vs 之后」的差**，不是总数：第一次播 A 的时候
	// CDN 本来就会被拉——而且可能不止一次（字节源会**并行**发一条 Range
	// 请求去取文件末尾那一段，好让解码器立刻拿到 `moov`）。
	//
	// ⚠️ **差值必须是 0**：切回去那一趟**一个字节都不该再要**。
	//
	// ⚠️ **等的是「A 真的在放」**：切回去之前界面上是 B，所以「标题是 A」
	// 这一条**只会在新那一趟装上之后才成立**——而那正是要量的那一趟。
	hitsBefore := b.hitsFor("/cdn/A.flac")
	if err := a.PlayFrom(app.Item{
		Source: "online", Type: "mv", Bvid: "BV1", Cid: "A",
		Title: "第 A 首",
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "「第 A 首」回来", func() bool {
		return a.Snapshot().Title == "第 A 首"
	})
	if got := b.hitsFor("/cdn/A.flac"); got != hitsBefore {
		t.Errorf("切回 A 那一趟，CDN 又被拉了 %d 次——"+
			"下过的字节没有留在盘上，用户每切回去一次就白下一整首", got-hitsBefore)
	}

	// 🔴 **缓存文件真的留在盘上**。
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatalf("读缓存目录：%v", err)
	}
	var biggest int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Size() > biggest {
			biggest = info.Size()
		}
	}
	if biggest != int64(len(media)) {
		t.Errorf("最大的缓存文件 = %d 字节，want %d（整首）——"+
			"下过的字节没有留在盘上", biggest, len(media))
	}
}

// TestTheCacheKeyIsTheSameFromBothEntrances 钉「同一首歌从两个入口进来，
// 键是同一个」（2026-11-02）。
//
// # 它为什么必须存在
//
// 缓存键曾经照抄 `streamKey`（五栏全拼），而**同一个 mv 条目的 sid 在不在场
// 取决于用户从哪个页面点的它**：收藏 / 合集进来的带 sid，队列恢复出来的不带。
// 键跟着入口变，字节写在 A 键下、下一次用 B 键去查——缓存永远不命中
// （实测：同一首歌八天取流九次，缓存目录里同一首存了两份）。
//
// 判据是**画面外的那件事**：带 sid 播完一遍（CDN 被拉了）之后，
// 不带 sid 再点同一首——CDN 不该再被拉。
func TestTheCacheKeyIsTheSameFromBothEntrances(t *testing.T) {
	media, err := os.ReadFile(filepath.Join("..", "service", "audio", "flacdec", "testdata", "stereo16.flac"))
	if err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}
	b := newStreamCacheBili(t, media)

	cacheDir := t.TempDir()
	a := app.New()
	t.Cleanup(a.Close)
	transport := &app.CredentialTransport{
		Base:   b.Client().Transport,
		Policy: app.DefaultInjectPolicy().AllowBiliHost(hostOfFixture(b.URL)),
	}
	cli := bili.NewClient(bili.Options{Transport: transport})
	cli.TestOnlyPointHostsAtFixture(b.URL)
	a.MountStreaming(app.Streaming{
		Client: cli, Media: b.Client(), TempDir: t.TempDir(), CacheDir: cacheDir,
	})

	// 从「收藏」那个入口进来：带 sid。
	if err := a.PlayFrom(app.Item{
		Source: "online", Type: "mv", Bvid: "BV1", Cid: "A", Sid: "999",
		Title: "从收藏进来的那一首",
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "带 sid 那一趟开始出声", func() bool {
		return a.Snapshot().Title == "从收藏进来的那一首"
	})
	waitFor(t, "带 sid 那一趟下完（缓存记上了）", func() bool {
		for _, p := range cacheJSONs(t, cacheDir) {
			raw, _ := os.ReadFile(p)
			if strings.Contains(string(raw), `"complete":true`) {
				return true
			}
		}
		return false
	})

	// 从「队列恢复」那个入口进来：**同一首**，没有 sid。
	// 一个字节都不该再要——键对不上就是这条用例的红。
	hitsBefore := b.hitsFor("/cdn/A.flac")
	if err := a.PlayFrom(app.Item{
		Source: "online", Type: "mv", Bvid: "BV1", Cid: "A",
		Title: "从恢复进来的同一首",
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "不带 sid 那一趟开始出声", func() bool {
		return a.Snapshot().Title == "从恢复进来的同一首"
	})
	if got := b.hitsFor("/cdn/A.flac"); got != hitsBefore {
		t.Errorf("同一首歌换个入口进来，CDN 又被拉了 %d 次——"+
			"键跟着入口变，缓存永远不命中", got-hitsBefore)
	}
}

// TestSwitchingAwayStillFinishesThePreviousDownload 钉「切歌之后，上一首的字节
// 在后台补完」（2026-11-02）。
//
// 切歌的那一瞬间上一首往往只下了一半；原来的行为是直接把源关掉，半截下载
// 作废，缓存永远记不上「完整」。现在切歌时把源收养给后台的 finisher，等它
// 下完再关——用户跳过的歌不再白下，下一次冷启动停在那一首上就是秒开。
//
// 判据是**缓存记录**：切走之后放行字节，A 那一份必须记成 complete:true，
// 而且**不该有新的 CDN 请求**（补完走的是已经开着的那条连接）；
// 再切回 A，一个字节都不该再要。
func TestSwitchingAwayStillFinishesThePreviousDownload(t *testing.T) {
	media, err := os.ReadFile(filepath.Join("..", "service", "audio", "flacdec", "testdata", "stereo16.flac"))
	if err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}
	b := newStreamCacheBili(t, media)

	// A 的 CDN 路径：发一半 → 停在半路等放行 → 发另一半。
	// 自己数命中数（override 不走默认计数）。
	var aHits atomic.Int32
	gate := make(chan struct{})
	mid := len(media) / 2
	b.overrides["/cdn/A.flac"] = func(w http.ResponseWriter, r *http.Request) {
		aHits.Add(1)
		w.Header().Set("Content-Length", fmt.Sprint(len(media)))
		w.Write(media[:mid])
		w.(http.Flusher).Flush()
		// ⚠️ **等放行要同时听请求的取消**（审查②的 P1-4）：前置断言早失败的
		// 话 `srv.Close` 会等这条卡住的连接，等不到就是整个二进制超时 panic。
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}
		w.Write(media[mid:])
	}

	cacheDir := t.TempDir()
	a := app.New()
	t.Cleanup(a.Close)
	transport := &app.CredentialTransport{
		Base:   b.Client().Transport,
		Policy: app.DefaultInjectPolicy().AllowBiliHost(hostOfFixture(b.URL)),
	}
	cli := bili.NewClient(bili.Options{Transport: transport})
	cli.TestOnlyPointHostsAtFixture(b.URL)
	a.MountStreaming(app.Streaming{
		Client: cli, Media: b.Client(), TempDir: t.TempDir(), CacheDir: cacheDir,
	})

	if err := a.PlayFrom(app.Item{
		Source: "online", Type: "mv", Bvid: "BV1", Cid: "A", Title: "第 A 首",
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "「第 A 首」开始出声", func() bool {
		return a.Snapshot().Title == "第 A 首"
	})

	// 切到 B：这一刻 A 的源被收养，A 的下载还停在半路。
	if err := a.PlayFrom(app.Item{
		Source: "online", Type: "mv", Bvid: "BV1", Cid: "B", Title: "第 B 首",
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "「第 B 首」开始出声", func() bool {
		return a.Snapshot().Title == "第 B 首"
	})
	hitsAtSwitch := aHits.Load()

	// 放行：finisher 该把 A 下完并把缓存记成「完整」。
	close(gate)
	waitFor(t, "A 的缓存在后台记成完整", func() bool {
		for _, p := range cacheJSONs(t, cacheDir) {
			raw, _ := os.ReadFile(p)
			if strings.Contains(string(raw), `"track":"online|mv|BV1|A|"`) &&
				strings.Contains(string(raw), `"complete":true`) {
				return true
			}
		}
		return false
	})
	if got := aHits.Load(); got != hitsAtSwitch {
		t.Errorf("后台补完又发了 %d 次新的 CDN 请求", got-hitsAtSwitch)
	}

	// 切回 A：一个字节都不该再要。
	before := aHits.Load()
	if err := a.PlayFrom(app.Item{
		Source: "online", Type: "mv", Bvid: "BV1", Cid: "A", Title: "第 A 首",
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "「第 A 首」从缓存回来", func() bool {
		return a.Snapshot().Title == "第 A 首"
	})
	if got := aHits.Load(); got != before {
		t.Errorf("切回 A 那一趟 CDN 又被拉了 %d 次——后台补完的字节没有被用上", got-before)
	}
}

// TestAColdStartResumesThePartialDownload 盯「**跨启动**的断点续传」
// （2026-11-02）。
//
// # 这条治的是什么
//
// 「切歌收养补完」管的是一个进程之内；进程退出时下到一半的那一份，原来只能
// 整个扔掉——带 CachePath 的源每次打开都从零下。于是听合集听到一半退出，
// 下次冷启动还是从零等（那正是「正在启动应用… 等半天」的形状）。
//
// 现在源把**顺序下载前沿**记在旁挂账本里（`.off`），退出时补最后一笔；
// 下一次冷启动从账本上的数发 Range 接着下。
//
// # 判据
//
//	第一次	播 A（发一半就停住）→ 退出 → 账本上就是半路的字节数
//	第二次	再播 A → CDN 那条请求必须带着 `Range: bytes=<账本>-`
//		→ 下完 → 账本退役 → 第三次播 A 一个字节都不该再要
func TestAColdStartResumesThePartialDownload(t *testing.T) {
	media, err := os.ReadFile(filepath.Join("..", "service", "audio", "flacdec", "testdata", "stereo16.flac"))
	if err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}
	b := newStreamCacheBili(t, media)

	var aHits atomic.Int32
	var lastRangeStart atomic.Int64
	gate := make(chan struct{})
	mid := len(media) / 2
	b.overrides["/cdn/A.flac"] = func(w http.ResponseWriter, r *http.Request) {
		aHits.Add(1)
		start := int64(0)
		if h := r.Header.Get("Range"); strings.HasPrefix(h, "bytes=") {
			// ⚠️ **头里带尾杠**（`bytes=125200-`），先剥掉再解析——不剥的话
			// 每一条续传请求都会在这里解析失败，handler 回 200 全量正文，
			// 源判「服务器不认续传」截断重下，测试就测不到续传了。
			if n, perr := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(h, "bytes="), "-"), 10, 64); perr == nil && n >= 0 {
				start = n
				lastRangeStart.Store(n)
			}
		}
		if start > int64(len(media)) {
			http.Error(w, "越界", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		// 🔴 **Range 必须真认**：续传请求带着 `bytes=<前沿>-` 来，回 200 全量
		// 正文的话，源会判「服务器不认续传」截断重下——那正是这条用例要抓
		// 的失败，不能让 fixture 自己造成它。
		w.Header().Set("Content-Length", fmt.Sprint(int64(len(media))-start))
		if start > 0 {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(media)-1, len(media)))
			w.WriteHeader(http.StatusPartialContent)
		}
		first := int64(mid)
		if first < start {
			first = start
		}
		if start < first {
			w.Write(media[start:first])
		}
		w.(http.Flusher).Flush()
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}
		if start < int64(mid) {
			w.Write(media[mid:])
		} else {
			w.Write(media[start:])
		}
	}

	cacheDir := t.TempDir()
	// ⚠️ **收尾顺序**：`t.TempDir()` 的清理是在**它被调用的那一刻**登记的，
	// 比后面的 `t.Cleanup(a.Close)` 早——于是缓存目录会在字节源还开着那个
	// 文件的时候被删，收尾报「directory not empty」。所以 App 要**先关**
	// （显式 close，不靠 t.Cleanup）。
	var apps []*app.App
	mount := func() *app.App {
		a := app.New()
		apps = append(apps, a)
		transport := &app.CredentialTransport{
			Base:   b.Client().Transport,
			Policy: app.DefaultInjectPolicy().AllowBiliHost(hostOfFixture(b.URL)),
		}
		cli := bili.NewClient(bili.Options{Transport: transport})
		cli.TestOnlyPointHostsAtFixture(b.URL)
		a.MountStreaming(app.Streaming{
			Client: cli, Media: b.Client(), TempDir: t.TempDir(), CacheDir: cacheDir,
		})
		return a
	}

	// 第一次：播 A，字节停在半路，然后**退出**。退出那一刻账本必须写上
	// 半路的字节数（精确等于 mid——发到半路连接就停了，前沿不再推进）。
	a1 := mount()
	if err := a1.PlayFrom(app.Item{
		Source: "online", Type: "mv", Bvid: "BV1", Cid: "A", Title: "第 A 首",
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "「第 A 首」开始出声", func() bool {
		return a1.Snapshot().Title == "第 A 首"
	})
	// 🔴 **账本是退出那一刻写的**（中途节流补笔的门槛是 1 MiB，这首比它小，
	// 没有中途的笔）——所以先退出，再验账。
	a1.Close()
	offPath := ledgerPathFor(t, cacheDir)
	ledger := readLedger(t, offPath)
	if ledger <= 0 || ledger > int64(mid) {
		t.Fatalf("账本 = %d，want (0, %d]（半路）", ledger, mid)
	}

	// 第二次：冷启动，同一份缓存目录。放行闸门，续传要接得上。
	close(gate)
	a2 := mount()
	hitsBefore := aHits.Load()
	if err := a2.PlayFrom(app.Item{
		Source: "online", Type: "mv", Bvid: "BV1", Cid: "A", Title: "第 A 首",
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "第二次下完（缓存记上了）", func() bool {
		for _, p := range cacheJSONs(t, cacheDir) {
			raw, _ := os.ReadFile(p)
			if strings.Contains(string(raw), `"track":"online|mv|BV1|A|"`) &&
				strings.Contains(string(raw), `"complete":true`) {
				return true
			}
		}
		return false
	})
	if got := lastRangeStart.Load(); got != ledger {
		t.Errorf("续传的 Range 起点 = %d，want 账本上的 %d——半截字节没有接上，"+
			"冷启动还是从零等", got, ledger)
	}
	// 🔴 **盘上那份必须逐字节等于源字节**（2026-11-02 审查②的 P0-1）：
	// 「Range 起点对」只验了**机械动作**——而 B 站同一首返回的字节不是契约
	// 稳定的（稿件被替换 / CDN 换对象），一续传就可能拼出一个两段拼接的
	// 文件：大小与整首一致 → 记成 complete → **永久坏缓存**。
	// 所以这里比**内容**：盘上必须是干净的整首。
	if err := assertCacheBytesEqual(t, cacheDir, media); err != nil {
		t.Errorf("盘上那份不是干净的整首：%v", err)
	}
	// 🔴 **两次请求**：一次「头部抽查」（续传前确认这段内容还是不是同一段
	// ——审查②的 P0-1），一次真正的续传。
	if got := aHits.Load(); got != hitsBefore+2 {
		t.Errorf("第二次启动 CDN 被拉了 %d 次，want 2 次（头部抽查 + Range 续传）", got-hitsBefore)
	}
	// 🔴 **账本必须退役**：要么没了，要么标着「已下完」（`done:`）——后者的
	// 意思是「这一份完整了，账本只是还没被清」。而**不能**是一个说「只下到
	// 一半」的前沿：那个数会让下一次续传从偏小的地方起，白下那一段。
	if raw, err := os.ReadFile(offPath); err == nil {
		text := strings.TrimSpace(string(raw))
		if !strings.HasPrefix(text, "done:") {
			t.Errorf("下完了账本还在，而且说的是「只下到一半」：内容=%q", text)
		}
	}

	// 第三次：从缓存直接播，一个字节都不该再要。
	hitsBefore = aHits.Load()
	if err := a2.PlayFrom(app.Item{
		Source: "online", Type: "mv", Bvid: "BV1", Cid: "A", Title: "第 A 首",
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "第三次从缓存回来", func() bool {
		return a2.Snapshot().Title == "第 A 首"
	})
	if got := aHits.Load(); got != hitsBefore {
		t.Errorf("第三次播放 CDN 又被拉了 %d 次——完整缓存没被用上", got-hitsBefore)
	}

	// ⚠️ **应用要在 TempDir 的清理之前关掉**：字节源还开着缓存里的那个文件，
	// 而 `t.TempDir()` 的清理是在**它被调用的那一刻**登记的（比这里的
	// t.Cleanup 早）——不关就是「directory not empty」。
	for _, a := range apps {
		a.Close()
	}
}

// ledgerPathFor 在缓存目录里找那一份 `.off`（这一份 E2E 只有一首，直接扫）。
func ledgerPathFor(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读缓存目录：%v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".bin.off") {
			return filepath.Join(dir, e.Name())
		}
	}
	t.Fatalf("缓存目录里没有账本")
	return ""
}

// readLedger 读账本上的数（读不出来算失败——那一条由别的用例管）。
func readLedger(t *testing.T, path string) int64 {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读账本：%v", err)
	}
	n, perr := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if perr != nil || n <= 0 {
		t.Fatalf("账本撕了：%q（%v）", string(raw), perr)
	}
	return n
}

// assertCacheBytesEqual 查缓存目录里那份 `.bin`，逐字节比对它和 want。
//
// ⚠️ **目录里可能有多份**（前面的用例留下的）：只看**最大的那个**——而
// 「拼接的那份」也是最大的（它的大小与整首一致）。
func assertCacheBytesEqual(t *testing.T, dir string, want []byte) error {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var biggest string
	var biggestSize int64 = -1
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".bin") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Size() > biggestSize {
			biggestSize = info.Size()
			biggest = filepath.Join(dir, e.Name())
		}
	}
	if biggest == "" {
		return fmt.Errorf("缓存目录里没有 .bin")
	}
	got, err := os.ReadFile(biggest)
	if err != nil {
		return err
	}
	if len(got) != len(want) {
		return fmt.Errorf("盘上 %d 字节，want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			return fmt.Errorf("第 %d 字节 = %d，want %d——续传把两段内容拼起来了", i, got[i], want[i])
		}
	}
	return nil
}

// TestAColdStartDoesNotSpliceDifferentContent 钉「**这一首换了内容就不许
// 续传**」（2026-11-02 审查②的 P0-1）。
//
// B 站同一首（同一个 bvid+cid）返回的字节**不是契约稳定的**：稿件被替换、
// CDN 换对象、会员降级后拿到另一段字节——而缓存键和长度都没变。于是上一次
// 退出时留下的半截 + 这一次的新内容 = **一个两段拼接的文件**，大小与整首
// 一致 → 记成 `complete:true` → **永久坏缓存**：用户此后每次切回这一首都播
// 那段拼接货，而且因为「大小对得上」，它**永远不会被重下**。
//
// # 判据
//
// 第一次会话在 A 上留下半截（**旧内容**）；第二次会话 CDN 给**另一份内容**
// ——续传必须被放弃（头部抽查认出「不是同一段」），盘上最后必须是**干净的
// 新整首**，而不是两段拼接。
func TestAColdStartDoesNotSpliceDifferentContent(t *testing.T) {
	media, err := os.ReadFile(filepath.Join("..", "service", "audio", "flacdec", "testdata", "stereo16.flac"))
	if err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}
	// 第二份内容：同样长，但字节不同（改掉开头几个——那正是「盘上留着的」
	// 那一段，续传**不会重下**它）。
	other := make([]byte, len(media))
	copy(other, media)
	for i := 0; i < 64; i++ {
		other[i] = other[i] ^ 0xFF
	}

	b := newStreamCacheBili(t, media)
	var aHits atomic.Int32
	gate := make(chan struct{})
	mid := len(media) / 2
	served := media
	b.overrides["/cdn/A.flac"] = func(w http.ResponseWriter, r *http.Request) {
		aHits.Add(1)
		start := int64(0)
		if h := r.Header.Get("Range"); strings.HasPrefix(h, "bytes=") {
			if n, perr := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(h, "bytes="), "-"), 10, 64); perr == nil && n >= 0 {
				start = n
			}
		}
		if start > int64(len(served)) {
			http.Error(w, "越界", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(int64(len(served))-start))
		if start > 0 {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(served)-1, len(served)))
			w.WriteHeader(http.StatusPartialContent)
		}
		first := int64(mid)
		if first < start {
			first = start
		}
		if start < first {
			w.Write(served[start:first])
		}
		w.(http.Flusher).Flush()
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}
		if start < int64(mid) {
			w.Write(served[mid:])
		} else {
			w.Write(served[start:])
		}
	}

	cacheDir := t.TempDir()
	var apps []*app.App
	mount := func() *app.App {
		a := app.New()
		apps = append(apps, a)
		transport := &app.CredentialTransport{
			Base:   b.Client().Transport,
			Policy: app.DefaultInjectPolicy().AllowBiliHost(hostOfFixture(b.URL)),
		}
		cli := bili.NewClient(bili.Options{Transport: transport})
		cli.TestOnlyPointHostsAtFixture(b.URL)
		a.MountStreaming(app.Streaming{
			Client: cli, Media: b.Client(), TempDir: t.TempDir(), CacheDir: cacheDir,
		})
		return a
	}

	// 第一次：旧内容，下到一半就退出。
	a1 := mount()
	if err := a1.PlayFrom(app.Item{
		Source: "online", Type: "mv", Bvid: "BV1", Cid: "A", Title: "第 A 首",
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "「第 A 首」开始出声", func() bool {
		return a1.Snapshot().Title == "第 A 首"
	})
	a1.Close()
	ledger := readLedger(t, ledgerPathFor(t, cacheDir))
	if ledger <= 0 || ledger > int64(mid) {
		t.Fatalf("账本 = %d，want (0, %d]", ledger, mid)
	}

	// 第二次：这一首换了内容。
	served = other
	close(gate)
	a2 := mount()
	if err := a2.PlayFrom(app.Item{
		Source: "online", Type: "mv", Bvid: "BV1", Cid: "A", Title: "第 A 首",
	}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "第二次下完（缓存记上了）", func() bool {
		for _, p := range cacheJSONs(t, cacheDir) {
			raw, _ := os.ReadFile(p)
			if strings.Contains(string(raw), `"track":"online|mv|BV1|A|"`) &&
				strings.Contains(string(raw), `"complete":true`) {
				return true
			}
		}
		return false
	})

	// 🔴 **盘上必须是干净的新整首**，不是「旧前缀 + 新内容」。
	if err := assertCacheBytesEqual(t, cacheDir, other); err != nil {
		t.Errorf("盘上那份是拼起来的：%v", err)
	}
	for _, a := range apps {
		a.Close()
	}
}
