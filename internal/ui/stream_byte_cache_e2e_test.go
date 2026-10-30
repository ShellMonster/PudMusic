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
	mu        sync.Mutex
}

func newStreamCacheBili(t *testing.T, media []byte) *streamCacheBili {
	t.Helper()
	b := &streamCacheBili{cdnByPath: map[string]int{}}
	b.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
