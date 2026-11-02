package app_test

// 应用层探针（2026-11-02 对抗性审查）。
//
// PROBE APP-1: **同一首同一档，两条源同时写同一个缓存文件**。
//
//	restreamOnline 的「换到**同一首的另一档**」→ 旧档被收养 → 不同文件。
//	但 **refreshOnlineNowFor**（换流重开）建的是**同曲同档**的替换源，
//	而它的 `old.Close()` 在**建好新源之后**才调 → 两条源同时写一个文件。
//
// 这里要量的是：新源建起来的时候，旧源还在写吗？

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

// slowCdnHandler 造一个「慢慢发」的 CDN：每次请求发 32KB 然后等。
func slowCdnHandler(media []byte, counter *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		counter.Add(1)
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
		end := start + 32<<10
		if end > len(media) {
			end = len(media)
		}
		w.Write(media[start:end])
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}
}

func TestProbeAppTwoWritersSameCacheFile(t *testing.T) {
	media, err := os.ReadFile(flacFixturePath(t))
	if err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}
	f := newStreamFixture(t)
	f.media = media

	var hits atomic.Int32
	f.on("/cdn/a.flac", slowCdnHandler(media, &hits))
	f.onPlayURLForCid("1", dashFor(cdnURL(f.URL, "a.flac", time.Hour), 30280, 110836))

	cacheDir := t.TempDir()
	a := app.New()
	t.Cleanup(a.Close)
	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	a.MountStreaming(app.Streaming{Client: cli, Media: f.Client(), TempDir: t.TempDir(), CacheDir: cacheDir})

	if err := a.PlayFrom(app.Item{Source: "online", Type: "mv", Bvid: "BV1X", Cid: "1", Title: "X"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 30*time.Second, "开始出声", func() bool {
		return a.Snapshot().Title == "X"
	})
	time.Sleep(500 * time.Millisecond)

	// 盘上：字节文件 + 账本 + 记录。
	entries, _ := os.ReadDir(cacheDir)
	for _, e := range entries {
		st, _ := e.Info()
		t.Logf("PROBE APP-1: 缓存目录里有 %s（%d 字节）", e.Name(), st.Size())
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".off") {
			raw, _ := os.ReadFile(filepath.Join(cacheDir, e.Name()))
			t.Logf("PROBE APP-1: 账本 %s = %q", e.Name(), string(raw))
		}
	}
	t.Logf("PROBE APP-1: CDN 被打了 %d 次", hits.Load())
}
