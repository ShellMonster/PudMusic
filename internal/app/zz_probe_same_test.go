package app_test

// 应用层探针 3（2026-11-02 对抗性审查）：**同一个 (track,tier) 上两条源**。
//
// PROBE APP-4: 同一首**再点一次**（旧源还在下的时候）：
//
//	buildFromRef → cancelFinishersForCache(T,Q)（旧源此时**不是** finisher，
//	             收养发生在 installOnlinePlayer 里，晚一步）→ 什么也没收
//	             → NewStreamSource → **O_TRUNC 打开同一个缓存文件**
//	installOnlinePlayer → adoptPreviousTrack(prevTrack) → **旧源被收养**
//	                              → 旧源继续往**同一个文件**写（从它自己的前沿）
//
// 于是盘上出现 [新源前沿, 旧源前沿) 的**洞**；
// 旧源 Close（迟到的收尾）把**旧的前沿**写回账本（「只许前进」让它赢）；
// 盘上大小 == 旧源前沿 → 应用层 resumeOffsetFor **放行** →
// 下一趟按账本续传 → **洞被跳过** → 解码器读零。

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

// oldFirstCdnHandler：第 1 个请求（旧源）一直往前爬；后来的（新源）只发一小段就停。
func oldFirstCdnHandler(media []byte, counter *atomic.Int32) http.HandlerFunc {
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
		if n == 1 {
			// 旧源：一路爬。
			for {
				end := start + 16<<10
				if end > int64(len(media)) {
					end = int64(len(media))
				}
				if start >= end {
					return
				}
				w.Write(media[start:end])
				start = end
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
				select {
				case <-r.Context().Done():
					return
				case <-time.After(150 * time.Millisecond):
				}
			}
		}
		// 新源：只发 16KB 就停。
		end := start + 16<<10
		if end > int64(len(media)) {
			end = int64(len(media))
		}
		w.Write(media[start:end])
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(20 * time.Second):
		}
	}
}

func TestProbeAppSameTrackTwice(t *testing.T) {
	media, err := os.ReadFile(flacFixturePath(t))
	if err != nil {
		t.Skipf("找不到 FLAC 测试素材：%v", err)
	}
	f := newStreamFixture(t)
	f.media = media

	var hits atomic.Int32
	f.on("/cdn/a.flac", oldFirstCdnHandler(media, &hits))
	f.onPlayURLForCid("1", dashFor(cdnURL(f.URL, "a.flac", time.Hour), 30280, 110836))

	cacheDir := t.TempDir()
	a := app.New()
	t.Cleanup(a.Close)
	cli := bili.NewClient(bili.Options{})
	cli.TestOnlyPointHostsAtFixture(f.URL)
	a.MountStreaming(app.Streaming{Client: cli, Media: f.Client(), TempDir: t.TempDir(), CacheDir: cacheDir})

	it := app.Item{Source: "online", Type: "mv", Bvid: "BV1S", Cid: "1", Title: "S"}
	if err := a.PlayFrom(it); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 30*time.Second, "S 开始出声", func() bool {
		return a.Snapshot().Title == "S"
	})
	time.Sleep(700 * time.Millisecond)
	t.Logf("PROBE APP-4: 第一次播完，CDN 打了 %d 次", hits.Load())

	dump := func(tag string) {
		entries, _ := os.ReadDir(cacheDir)
		for _, e := range entries {
			st, _ := e.Info()
			t.Logf("PROBE APP-4: %s 目录 %s（%d 字节）", tag, e.Name(), st.Size())
			if strings.HasSuffix(e.Name(), ".off") {
				raw, _ := os.ReadFile(filepath.Join(cacheDir, e.Name()))
				t.Logf("PROBE APP-4: %s 账本 %s = %q", tag, e.Name(), string(raw))
			}
			if strings.HasSuffix(e.Name(), ".bin") {
				b, err := os.ReadFile(filepath.Join(cacheDir, e.Name()))
				if err != nil {
					continue
				}
				mismatch := -1
				for i := 0; i < len(b) && i < len(media); i++ {
					if b[i] != media[i] {
						mismatch = i
						break
					}
				}
				if mismatch >= 0 {
					t.Logf("PROBE APP-4: %s ⟵ 字节文件第 %d 字节起与原始不符（**洞**！）", tag, mismatch)
				} else {
					t.Logf("PROBE APP-4: %s 字节文件前 %d 字节与原始一致", tag, len(b))
				}
			}
		}
	}
	dump("第一次")

	// 同一首再点一次。
	if err := a.PlayFrom(it); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	t.Logf("PROBE APP-4: 第二次点完，CDN 累计打了 %d 次", hits.Load())
	dump("第二次")

	// 关掉应用 → 收养的那条源被收掉 → 它的 Close 把**旧的前沿**写回账本。
	a.Close()
	time.Sleep(300 * time.Millisecond)
	dump("退出后")

	// 下一趟：应用读账本 → resumeOffsetFor。
	for _, e := range entriesOf(cacheDir) {
		if !strings.HasSuffix(e, ".off") {
			continue
		}
		raw, _ := os.ReadFile(filepath.Join(cacheDir, e))
		ledger, _ := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
		bin := strings.TrimSuffix(e, ".off")
		st, _ := os.Stat(filepath.Join(cacheDir, bin))
		t.Logf("PROBE APP-4: 账本 = %d，字节文件大小 = %d → resumeOffsetFor 采信 = %v",
			ledger, st.Size(), ledger <= st.Size())
		if ledger > 0 && ledger <= st.Size() {
			// 洞在 [16384, 81920) —— 下一趟从 ledger 起续传，洞被跳过。
			b, _ := os.ReadFile(filepath.Join(cacheDir, bin))
			bad := -1
			for i := 0; i < len(b) && int64(i) < ledger; i++ {
				if b[i] != media[i] {
					bad = i
					break
				}
			}
			t.Logf("PROBE APP-4: 续传起点 %d 之前就有 %d 字节与原始不符（洞在续传范围**之内** → 洞被永久跳过）", ledger, bad)
		}
	}
}

func entriesOf(dir string) []string {
	es, _ := os.ReadDir(dir)
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}
