package app_test

// 「换音质不丢下载」的用例（2026-11-02，换档收养）。
//
// # 它盯的两件事
//
// 用户在下载进行到一半时点了另一档音质，这一瞬间有**两条**字节源：
//
//	旧档的源	被换下去——原来的行为是直接 Close，半截下载作废
//	新档的源	正在建——换完档的那一首再被切走时，它也该能被收养
//
// 两条都该活着把各自的字节下完（不同档 = 不同缓存文件，互不冲突）：
//
//	播 A（192K，停在半路）→ 点 128K（也停在半路）→ 切到 B
//	→ 放行两道闸 → **两条缓存记录都必须记成「完整」**
//
// ⚠️ **两首的 bvid 刻意不同**：粗粒度的身份回收（cid 没问出来那一路的兜底）
// 按 bvid 匹配，同 bvid 会把 A 的在补源错杀掉——那不是这条用例要造的场景。

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
)

// gatedHalfHandler 造一个「发一半 → 停在半路等放行 → 发另一半」的 CDN handler，
// 自带 Range 续传语义与命中计数。等待放行同时听请求的取消（见
// stream_byte_finisher_test.go 那一段的理由）。
func gatedHalfHandler(media []byte, counter *atomic.Int32, gate chan struct{}) http.HandlerFunc {
	mid := len(media) / 2
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
		first := mid
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
		if start < mid {
			w.Write(media[mid:])
		} else {
			w.Write(media[start:])
		}
	}
}

// twoPathDash 造一份「192K 和 128K 各指向**不同**字节」的 dash——换档之后
// 新旧两条流落在不同的缓存文件上，互不冲突，才能各自补完。
func twoPathDash(x, y string) map[string]any {
	return map[string]any{
		"duration": 1,
		"audio": []any{
			map[string]any{
				"id": int(bili.QualityHigh192), "bandwidth": 320000,
				"codecs": "flac", "mimeType": "audio/mp4", "baseUrl": x,
			},
			map[string]any{
				"id": int(bili.QualityMid128), "bandwidth": 128000,
				"codecs": "flac", "mimeType": "audio/mp4", "baseUrl": y,
			},
		},
	}
}

func TestSwitchingQualityKeepsBothDownloadsAlive(t *testing.T) {
	media, err := os.ReadFile(flacFixturePath(t))
	if err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}
	f := newStreamFixture(t)
	f.media = media

	var xHits, yHits atomic.Int32
	gateX, gateY := make(chan struct{}), make(chan struct{})
	f.on("/cdn/x.flac", gatedHalfHandler(media, &xHits, gateX))
	f.on("/cdn/y.flac", gatedHalfHandler(media, &yHits, gateY))
	f.onPlayURLForCid("1", twoPathDash(cdnURL(f.URL, "x.flac", time.Hour), cdnURL(f.URL, "y.flac", time.Hour)))
	f.onPlayURLForCid("2", dashFor(cdnURL(f.URL, "b.flac", time.Hour), 30280, 110836))

	cacheDir := t.TempDir()
	a := app.New()
	t.Cleanup(a.Close)
	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	a.MountStreaming(app.Streaming{Client: cli, Media: f.Client(), TempDir: t.TempDir(), CacheDir: cacheDir})

	// 播 A@192K：出了声就算数，字节停在半路。
	if err := a.PlayFrom(app.Item{Source: "online", Type: "mv", Bvid: "BV1A", Cid: "1", Title: "A"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 30*time.Second, "A@192K 开始出声", func() bool {
		return a.Snapshot().Title == "A" && a.Snapshot().Quality == "192K"
	})

	// 换到 128K：旧档源被换下去，新档源也在半路停住。
	a.SetAudioQuality(bili.AudioMedium)
	waitFor(t, 30*time.Second, "A@128K 接管", func() bool {
		return a.Snapshot().Quality == "128K"
	})

	// 切到 B：A@128K 的源在这一刻被收养（A@192K 的早已收养）。
	if err := a.PlayFrom(app.Item{Source: "online", Type: "mv", Bvid: "BV1B", Cid: "2", Title: "B"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 30*time.Second, "B 开始出声", func() bool {
		return a.Snapshot().Title == "B"
	})
	xAtSwitch, yAtSwitch := xHits.Load(), yHits.Load()

	// 放行两道闸：两条在补的源各自下完，两条缓存记录都必须记成「完整」。
	close(gateX)
	close(gateY)
	waitFor(t, 30*time.Second, "192K 那份在后台记成完整", func() bool {
		return cacheCompleteFor(t, cacheDir, "online|mv|BV1A|1|", "q30280")
	})
	waitFor(t, 30*time.Second, "128K 那份在后台记成完整", func() bool {
		return cacheCompleteFor(t, cacheDir, "online|mv|BV1A|1|", "q30232")
	})
	if got := xHits.Load(); got != xAtSwitch {
		t.Errorf("192K 补完发了 %d 次新的 CDN 请求", got-xAtSwitch)
	}
	if got := yHits.Load(); got != yAtSwitch {
		t.Errorf("128K 补完发了 %d 次新的 CDN 请求", got-yAtSwitch)
	}
}

// cacheCompleteFor 报「这一首这一档」的缓存记录是不是完整的。
func cacheCompleteFor(t *testing.T, dir, track, tier string) bool {
	t.Helper()
	for _, p := range cacheJSONsIn(t, dir) {
		raw, _ := os.ReadFile(p)
		if strings.Contains(string(raw), `"track":"`+track+`"`) &&
			strings.Contains(string(raw), `"tier":"`+tier+`"`) &&
			strings.Contains(string(raw), `"complete":true`) {
			return true
		}
	}
	return false
}
