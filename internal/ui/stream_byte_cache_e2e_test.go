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
		<-gate
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
