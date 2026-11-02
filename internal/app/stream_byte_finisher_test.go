package app_test

// 「切歌之后，上一首的字节在后台补完」的用例（2026-11-02）。
//
// # 它盯的是什么
//
// 用户切歌的那一瞬间，上一首的字节源往往**只下了一半**。原来的行为是直接
// `Close`：半截 download 作废，缓存永远不完整，下一次冷启动停在那一首上就
// 要**从零再下一整首**——那正是「正在启动应用… 等半天」的元凶。
//
// 现在的行为：切歌时把旧源**收养**给后台的 finisher，等它下完（缓存记上
// 「完整」）再关。这条用例从真播放、真切歌走到真缓存记录：
//
//	播 A（CDN 发一半就停住）→ 切到 B → 放行 A 的字节 → A 的缓存必须记成「完整」
//	→ 再切回 A：CDN 一个字节都不该再要
//
// ⚠️ **判据是缓存记录**，不是「CDN 被拉了几次」：被拉过 ≠ 下完了。

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

func TestSwitchingAwayStillFinishesThePreviousDownload(t *testing.T) {
	media, err := os.ReadFile(flacFixturePath(t))
	if err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}
	f := newStreamFixture(t)
	f.media = media

	// A 的 CDN 路径：发一半 → **停在半路等放行** → 发另一半。
	// 自己数这一条路径的命中数（override 不走默认计数）。
	var aHits atomic.Int32
	gate := make(chan struct{})
	mid := len(media) / 2
	f.on("/cdn/A.flac", func(w http.ResponseWriter, r *http.Request) {
		aHits.Add(1)
		start := 0
		if h := r.Header.Get("Range"); strings.HasPrefix(h, "bytes=") {
			start, _ = strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(h, "bytes="), "-"))
		}
		if start > len(media) {
			http.Error(w, "越界", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(media)-start))
		if start > 0 {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(media)-1, len(media)))
			w.WriteHeader(http.StatusPartialContent)
		}
		// 先发到半路（Range 请求可能一上来就在半路之后，那一段没有可发的），
		// 停在半路等放行，再把剩下的一半发完。前后两段**拼起来**正好是
		// Content-Length 说的那么多——多一笔或少一笔客户端都会当成传输错误。
		first := mid
		if first < start {
			first = start
		}
		if start < first {
			w.Write(media[start:first])
		}
		w.(http.Flusher).Flush()
		// ⚠️ **等放行要同时听请求的取消**（审查②的 P1-4）：任何一个前置断言
		// 早失败的话，`t.Cleanup` 里的 `srv.Close` 会**等**这条卡住的连接——
		// 等不到就是整个测试二进制超时 panic，真正的失败原因被盖掉。
		// 测试进程退出时请求的 context 会取消，handler 从这儿走人。
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}
		if start < mid {
			w.Write(media[mid:])
		} else {
			w.Write(media[start:])
		}
	})
	// A 和 B 各自的取流返回（按 cid 分，判「白下」要看同一首的路径）。
	f.onPlayURLForCid("1", dashFor(cdnURL(f.URL, "A.flac", time.Hour), 30280, 110836))
	f.onPlayURLForCid("2", dashFor(cdnURL(f.URL, "B.flac", time.Hour), 30280, 110836))

	cacheDir := t.TempDir()
	a := app.New()
	t.Cleanup(a.Close)
	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	a.MountStreaming(app.Streaming{Client: cli, Media: f.Client(), TempDir: t.TempDir(), CacheDir: cacheDir})

	// 播 A：出了声就算数——那一刻字节只下到一半。
	if err := a.PlayFrom(app.Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1", Title: "第 A 首"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 30*time.Second, "「第 A 首」开始出声", func() bool {
		return a.Snapshot().Title == "第 A 首"
	})

	// 切到 B：A 的源在这一刻被收养，A 的下载还停在半路。
	if err := a.PlayFrom(app.Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "2", Title: "第 B 首"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 30*time.Second, "「第 B 首」开始出声", func() bool {
		return a.Snapshot().Title == "第 B 首"
	})
	hitsAtSwitch := aHits.Load()

	// 放行 A 的字节：finisher 该把它下完，并把缓存记成「完整」。
	close(gate)
	waitFor(t, 30*time.Second, "A 的缓存在后台记成完整", func() bool {
		for _, p := range cacheJSONsIn(t, cacheDir) {
			raw, _ := os.ReadFile(p)
			if strings.Contains(string(raw), `"track":"online|mv|BV1|1|"`) &&
				strings.Contains(string(raw), `"complete":true`) {
				return true
			}
		}
		return false
	})
	// 🔴 **补完走的是已经开着的那条连接**：放行之后不该有新的 CDN 请求
	// （Range 续传那一趟是同一发请求的后半段，不另计一次命中——所以这里的
	// 判据是「放行前后命中数不涨」，而不是恒等于某个数）。
	if got := aHits.Load(); got != hitsAtSwitch {
		t.Errorf("后台补完又发了 %d 次新的 CDN 请求", got-hitsAtSwitch)
	}

	// 切回 A：缓存命中，一个字节都不该再要。
	before := aHits.Load()
	if err := a.PlayFrom(app.Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1", Title: "第 A 首"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 30*time.Second, "「第 A 首」从缓存回来", func() bool {
		return a.Snapshot().Title == "第 A 首"
	})
	if got := aHits.Load(); got != before {
		t.Errorf("切回 A 那一趟 CDN 又被拉了 %d 次——后台补完的字节没有被用上", got-before)
	}
}

// cacheJSONsIn 列出缓存目录里的记录文件（和 internal/ui 那份 E2E 的同名辅助
// 各自为政：这一份不依赖界面那一层的测试文件）。
func cacheJSONsIn(t *testing.T, dir string) []string {
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
