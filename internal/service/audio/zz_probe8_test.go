package audio

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// PROBE I: 两条源写**同一个**缓存文件（同一首被重新点了一次 → 新源 O_TRUNC）。
// 旧源继续写 → 盘上出现 [Y, X) 的洞；旧源迟到的 Close 把 X 写回账本；
// 盘上大小 ≥ X → 应用层校验放行 → 下一趟从 X 续传 → 洞被跳过 → 零。
func TestProbeIHoleSurvivesIntoTheNextLaunch(t *testing.T) {
	data := payload(1024 << 10)
	var mu sync.Mutex
	var n int
	// 旧源：慢慢发 200KB。  新源：发 64KB。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		which := n
		mu.Unlock()
		rs := rangeStart(r)
		if rs < 0 {
			rs = 0
		}
		w.Header().Set("Content-Length", strconv.FormatInt(int64(len(data))-rs, 10))
		if rs > 0 {
			w.Header().Set("Content-Range", "bytes "+strconv.FormatInt(rs, 10)+"-"+strconv.Itoa(len(data)-1)+"/"+strconv.Itoa(len(data)))
			w.WriteHeader(http.StatusPartialContent)
		}
		chunk := int64(64 << 10)
		if which == 1 {
			chunk = 200 << 10
		}
		end := rs + chunk
		if end > int64(len(data)) {
			end = int64(len(data))
		}
		w.Write(data[rs:end])
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// 卡住，直到这一条被关掉。
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	cachePath := filepath.Join(dir, "song.bin")
	offsetPath := filepath.Join(dir, "song.bin.off")

	// 旧源 S1：写到 200KB。
	s1, err := NewStreamSource(StreamSourceOptions{
		URLs: []string{srv.URL}, Client: srv.Client(),
		CachePath: cachePath, OffsetPath: offsetPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	deadline := time.Now().Add(3 * time.Second)
	for s1.Filled() < 200<<10 && time.Now().Before(deadline) {
		s1.Read(buf)
		time.Sleep(time.Millisecond)
	}
	oldFilled := s1.Filled()
	t.Logf("PROBE I: 旧源 S1 前沿 = %d", oldFilled)

	// 新源 S2：同一个文件 → O_TRUNC（账本还没有）→ 从 0 重下，只下 64KB。
	s2, err := NewStreamSource(StreamSourceOptions{
		URLs: []string{srv.URL}, Client: srv.Client(),
		CachePath: cachePath, OffsetPath: offsetPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	s2.Read(buf)
	time.Sleep(200 * time.Millisecond)
	newFilled := s2.Filled()
	st, _ := os.Stat(cachePath)
	t.Logf("PROBE I: 新源 S2 前沿 = %d，盘上大小 = %d", newFilled, st.Size())

	// 旧源 Close（迟到）：它把旧前沿 X 写回账本。
	s1.Close()
	raw, _ := os.ReadFile(offsetPath)
	t.Logf("PROBE I: S1 关源后账本 = %q", string(raw))

	// 新源 Close：它只到 64KB < X → 被「只许前进」挡掉 → 账本停在 X。
	s2.Close()
	raw2, _ := os.ReadFile(offsetPath)
	st2, _ := os.Stat(cachePath)
	ledger, perr := strconv.ParseInt(string(raw2), 10, 64)
	t.Logf("PROBE I: S2 关源后账本 = %q，盘上大小 = %d", string(raw2), st2.Size())
	if perr != nil {
		t.Logf("PROBE I: 账本不是数字（没复现）")
		return
	}
	if ledger > newFilled {
		f, err := os.Open(cachePath)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		hole := make([]byte, ledger-newFilled)
		if _, err := f.ReadAt(hole, newFilled); err == nil {
			allZero := true
			for _, b := range hole {
				if b != 0 {
					allZero = false
					break
				}
			}
			t.Logf("PROBE I: 盘上 [%d,%d) 全零 = %v（洞！）", newFilled, ledger, allZero)
		}
	}
	t.Logf("PROBE I: 应用层 resumeOffsetFor 会采信 %d 吗？%v（盘上大小 %d）→ 洞被跳过",
		ledger, ledger <= st2.Size(), st2.Size())
}
