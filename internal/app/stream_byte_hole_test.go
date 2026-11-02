package app_test

// 「**缓存里不留洞**」的用例（2026-11-02 对抗性审查的 P0-1）。
//
// # 它盯的是什么
//
// 同一首点第二次 → 第二个源 `O_TRUNC` **截断**那个缓存文件 → 第一条源不知情、
// 继续从自己的偏移往后写 → 盘上是「新源的 [0,128K) ∪ 旧源的 [200K,250K)」，
// **中间 [128K,200K) 是洞**。
//
// 改动前那一段是安全的（旧源随即被关掉，洞只存在一瞬，下一趟从零重下就自愈
// 了）；**收养把它固化了**——而跨启动续传会从洞之后接着下：洞永远填不上，
// 而文件大小与整首一致 → 记成 `complete:true` → **永久坏缓存**（用户此后
// 每次听这首都是坏字节，系统永远不会重下它）。
//
// # 为什么夹具这么绕
//
// 要造出那个洞，必须让**两条源同时活着**、第一条写到一个偏移、第二条把它
// 的文件截断、而第一条**继续写**。所以服务器的两次请求行为必须不同：
// 第一次发到 200K 等一等再发到 250K（停下），第二次发 128K（停下）——
// 而第一条那 50K 的「续写」必须落在第二条截断之后的空档里。
//
// 素材也要够大（`stereo24.flac`，604 KB）：小了的话第一条瞬间下完，根本
// 撞不上那个窗口。

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

// twoShotCdnHandler 造那个「两次请求行为不同」的 CDN：
//
//	第 1 个请求（第一条源）	发 200K → 等 2.5s → 发到 250K → 停住（**不下完**）
//	第 2 个请求（第二条源）	发 128K → 停住
//	第 3 个及以后		完整发（配合 Range）
func twoShotCdnHandler(media []byte, counter *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n := counter.Add(1)
		start := int64(0)
		if h := r.Header.Get("Range"); strings.HasPrefix(h, "bytes=") {
			start, _ = strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(h, "bytes="), "-"), 10, 64)
		}
		if start > int64(len(media)) {
			http.Error(w, "越界", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(int64(len(media))-start))
		if start > 0 {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(media)-1, len(media)))
			w.WriteHeader(http.StatusPartialContent)
		}
		switch n {
		case 1:
			end := min64(start+200<<10, int64(len(media)))
			w.Write(media[start:end])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			select {
			case <-r.Context().Done():
				return
			case <-time.After(2500 * time.Millisecond):
			}
			end2 := min64(end+50<<10, int64(len(media)))
			w.Write(media[end:end2])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			select {
			case <-r.Context().Done():
			case <-time.After(30 * time.Second):
			}
		case 2:
			end := min64(start+128<<10, int64(len(media)))
			w.Write(media[start:end])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			select {
			case <-r.Context().Done():
			case <-time.After(30 * time.Second):
			}
		default:
			w.Write(media[start:])
		}
	}
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// TestTheCacheNeverKeepsAHole 盯「**缓存里不留洞**」。
//
// # 判据
//
//	第一次会话	退出后盘上那份字节必须**从头到尾跟源字节一致**（没有洞）
//	第二次会话	冷启动续传之后，盘上那份仍然**跟源字节一致**
//
// 🔴 **第二条判据是关键**：第一遍没洞只是「这一次没撞上」，而洞的杀伤力
// 全在续传上——从洞之后接着下，洞永远填不上，而大小与整首一致 → 坏缓存。
func TestTheCacheNeverKeepsAHole(t *testing.T) {
	media, err := os.ReadFile(filepath.Join("..", "service", "audio", "flacdec", "testdata", "stereo24.flac"))
	if err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}
	f := newStreamFixture(t)
	f.media = media
	var hits atomic.Int32
	f.on("/cdn/a.flac", twoShotCdnHandler(media, &hits))
	f.onPlayURLForCid("1", dashFor(cdnURL(f.URL, "a.flac", time.Hour), 30280, 110836))

	cacheDir := t.TempDir()
	a := app.New()
	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	a.MountStreaming(app.Streaming{Client: cli, Media: f.Client(), TempDir: t.TempDir(), CacheDir: cacheDir})

	it := app.Item{Source: "online", Type: "mv", Bvid: "BV1S", Cid: "1", Title: "S"}
	play := func(a *app.App, tag string) {
		t.Helper()
		if err := a.PlayFrom(it); err != nil {
			t.Fatal(err)
		}
		waitFor(t, 30*time.Second, tag+" 出声", func() bool {
			return a.Snapshot().Title == "S"
		})
	}
	play(a, "第 1 次")
	time.Sleep(700 * time.Millisecond)
	play(a, "第 2 次")
	time.Sleep(3200 * time.Millisecond)
	a.Close()
	time.Sleep(600 * time.Millisecond)

	binPath := theOnlyBin(t, cacheDir)
	assertNoHole(t, binPath, media, "退出后")

	// ---- 冷启动：同一份缓存目录，续传 ----
	f2 := newStreamFixture(t)
	f2.media = media
	var hits2 atomic.Int32
	f2.on("/cdn/a.flac", func(w http.ResponseWriter, r *http.Request) {
		hits2.Add(1)
		start := int64(0)
		if h := r.Header.Get("Range"); strings.HasPrefix(h, "bytes=") {
			start, _ = strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(h, "bytes="), "-"), 10, 64)
		}
		if start > int64(len(media)) {
			http.Error(w, "越界", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(int64(len(media))-start))
		if start > 0 {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(media)-1, len(media)))
			w.WriteHeader(http.StatusPartialContent)
		}
		w.Write(media[start:])
	})
	f2.onPlayURLForCid("1", dashFor(cdnURL(f2.URL, "a.flac", time.Hour), 30280, 110836))

	a2 := app.New()
	t.Cleanup(a2.Close)
	cli2 := bili.NewClient(bili.Options{})
	cli2.TestOnlyPointHostsAtFixture(f2.URL)
	a2.MountStreaming(app.Streaming{Client: cli2, Media: f2.Client(), TempDir: t.TempDir(), CacheDir: cacheDir})
	play(a2, "冷启动那一次")
	time.Sleep(2500 * time.Millisecond)

	// 🔴 **续传之后仍然没有洞**——这一条才是要害：洞的杀伤力全在续传上。
	assertNoHole(t, binPath, media, "冷启动续传之后")
}

// theOnlyBin 找出缓存目录里那一份字节文件。
func theOnlyBin(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".bin") {
			return filepath.Join(dir, e.Name())
		}
	}
	t.Fatal("缓存目录里没有 .bin")
	return ""
}

// assertNoHole 报「盘上那份从头到尾跟源字节一致」。
func assertNoHole(t *testing.T, path string, media []byte, when string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := range got {
		if i >= len(media) {
			t.Errorf("%s：盘上 %d 字节，比整首还长", when, len(got))
			return
		}
		if got[i] != media[i] {
			t.Errorf("%s：第 %d 字节 = %d，want %d——缓存里留了个洞"+
				"（续传会从洞之后接着下，它永远填不上，而大小与整首一致 → 坏缓存）",
				when, i, got[i], media[i])
			return
		}
	}
}
