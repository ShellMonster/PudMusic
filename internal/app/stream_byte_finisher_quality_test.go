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
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

// TestTheSameTrackTwiceNeverLeavesAHole 盯「**同一首点两次，缓存里不留洞**」
// （2026-11-02 审查①的 P0-1）。
//
// # 它盯的是什么
//
// 同一首点第二次 → 新源 `O_TRUNC` **截断**那个缓存文件 → 正在播的旧源不知情、
// 继续从自己的偏移往后写 → 盘上是「新源的 [0,128K) ∪ 旧源的 [200K,256K)」，
// **中间 [128K,200K) 是洞**。
//
// 改动前那一段是安全的（旧源随即被关掉，洞只存在一瞬，下一趟从零重下就自愈
// 了）；**收养把它固化了**——而跨启动续传会从洞之后接着下：洞永远填不上，
// 而文件大小与整首一致 → 记成 `complete:true` → **永久坏缓存**（用户此后
// 每次听这首都是坏字节，系统永远不会重下它）。
//
// # 判据
//
// 点两次之后，缓存里那份字节必须**从头到尾连续**（没有洞）。
func TestTheSameTrackTwiceNeverLeavesAHole(t *testing.T) {
	media, err := os.ReadFile(flacFixturePath(t))
	if err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}
	f := newStreamFixture(t)
	f.media = media
	// 🔴 **CDN 发一半就停住**：第一条源要停在「下载中」，第二次点击才会
	// 撞上「旧源还在写、新源截断文件」那个窗口（洞的起点）。
	// FLAC 素材只有 250 KB，瞬间就下完了——不加这个闸门，第二次点击时下载
	// 早就结束，测试抓不到任何东西。
	gate := make(chan struct{})
	var once sync.Once
	f.on("/cdn/A.flac", func(w http.ResponseWriter, r *http.Request) {
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
		mid := len(media) / 2
		first := mid
		if first < start {
			first = start
		}
		if start < first {
			w.Write(media[start:first])
		}
		w.(http.Flusher).Flush()
		once.Do(func() { <-gate })
		if start < mid {
			w.Write(media[mid:])
		} else {
			w.Write(media[start:])
		}
	})
	f.onPlayURLForCid("1", dashFor(cdnURL(f.URL, "A.flac", time.Hour), 30280, 110836))

	cacheDir := t.TempDir()
	a := app.New()
	t.Cleanup(a.Close)
	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	a.MountStreaming(app.Streaming{Client: cli, Media: f.Client(), TempDir: t.TempDir(), CacheDir: cacheDir})

	it := app.Item{Source: "online", Type: "mv", Bvid: "BV1", Cid: "1", Title: "同一首"}
	if err := a.PlayFrom(it); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 30*time.Second, "第一次开始出声", func() bool {
		return a.Snapshot().Title == "同一首"
	})
	// ⚠️ **不等它下完就点第二次**（2026-11-02 审查①的 P0-1）：洞正是
	// 「第一条源还在写 → 第二个源 `O_TRUNC` 截断文件 → 第一条源不知情、
	// 继续从旧偏移写」造出来的。等它下完了再点，走的是缓存命中那一路，
	// 压根不建源——那样的测试抓不到任何东西。
	//
	// ⚠️ **也不等它出声**：FLAC 素材很小，出声时字节早就下完了，而那份
	// 完整字节在第二次点击时会被 `O_TRUNC` 截掉——正是洞的起点。所以这里
	// 只等「开始出声」那一刻就立刻点第二次（下载多半还没完）。
	time.Sleep(50 * time.Millisecond)

	// 同一首再点一次。
	if err := a.PlayFrom(it); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 30*time.Second, "第二次开始出声", func() bool {
		return a.Snapshot().Title == "同一首"
	})
	// 放行闸门：让还活着的那条源把剩下的一半下完。
	close(gate)
	// 给后台那些源一点时间收尾。
	time.Sleep(500 * time.Millisecond)

	// 🔴 **盘上那份必须没有洞**：长度对得上，且**每一段都跟源字节一致**。
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	var bin string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".bin") {
			bin = filepath.Join(cacheDir, e.Name())
		}
	}
	if bin == "" {
		t.Fatal("缓存目录里没有 .bin")
	}
	got, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > len(media) {
		t.Fatalf("盘上 %d 字节，比整首还长", len(got))
	}
	// 已下的那一段必须跟源字节**逐字节一致**（洞的位置会是零或别的内容）。
	for i := range got {
		if got[i] != media[i] {
			t.Fatalf("盘上第 %d 字节 = %d，want %d——缓存里留了个洞",
				i, got[i], media[i])
		}
	}
	// 🔴 **两次点击之后，缓存里不能有两个写者留下的「两份记录」**：
	// 同一首同一档只有一份缓存文件，而它的记录必须说「完整」。
	// （洞的另一种形态是「两份记录各说各的」——那份旧的记录还留在盘上。）
	// 🔴 **字节记录只能有一份**（`b-*.json`；`p-*.json` 是「偏好 → 档」的映射
	// 表，本来就有好几份，不算）。
	//
	// ⚠️ **两份字节记录 = 两个写者**：同一个 (track, tier) 只有一份缓存文件，
	// 而两条源各写一份记录时，字节文件是**两条源写的混合**（洞），而记录说
	// 的 size 是其中一条的——「大小对得上」这道判据就拦不住了。
	recs := 0
	for _, p := range cacheJSONsIn(t, cacheDir) {
		if !strings.HasPrefix(filepath.Base(p), "b-") {
			continue
		}
		raw, _ := os.ReadFile(p)
		if strings.Contains(string(raw), `"track":"online|mv|BV1|1|"`) {
			recs++
		}
	}
	if recs > 1 {
		t.Errorf("同一首同一档有 %d 份字节记录——两个写者各留了一份", recs)
	}
}
