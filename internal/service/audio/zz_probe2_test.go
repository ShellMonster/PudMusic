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

// PROBE A: 完整下载（服务器不给 Content-Length）→ 账本留下「整首长度」
// → 下一趟按账本续传 → Range 起点 == 整首 → 416 → 这一首永远放不出来。
func TestProbeACompleteWithoutContentLengthThenResumeLoops(t *testing.T) {
	data := payload(32 << 10)
	var mu sync.Mutex
	var ranges []int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs := rangeStart(r)
		mu.Lock()
		ranges = append(ranges, rs)
		mu.Unlock()
		if rs > 0 && rs >= int64(len(data)) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		// 不给 Content-Length → Go 用 chunked → sizeKnown 永远是 false。
		w.Write(data)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	cachePath := filepath.Join(dir, "song.bin")
	offsetPath := filepath.Join(dir, "song.bin.off")

	// 第一趟：从零下完（服务器不给长度）。
	s1, err := NewStreamSource(StreamSourceOptions{
		URLs: []string{srv.URL}, Client: srv.Client(),
		CachePath: cachePath, OffsetPath: offsetPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 8192)
	for {
		if _, err := s1.Read(buf); err != nil {
			t.Logf("PROBE A: 第一趟结束 = %v", err)
			break
		}
	}
	s1.Close()
	raw, err := os.ReadFile(offsetPath)
	if err != nil {
		t.Fatalf("PROBE A: 账本没了: %v", err)
	}
	st, _ := os.Stat(cachePath)
	t.Logf("PROBE A: 第一趟后 账本=%q 盘上大小=%d 整首=%d", string(raw), st.Size(), len(data))

	// 第二趟：应用按账本续传。
	n, _ := strconv.ParseInt(string(raw), 10, 64)
	s2, err := NewStreamSource(StreamSourceOptions{
		URLs: []string{srv.URL}, Client: srv.Client(),
		CachePath: cachePath, OffsetPath: offsetPath,
		ResumeFrom: n,
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(6 * time.Second)
	for {
		_, err := s2.Read(buf)
		if err != nil {
			t.Logf("PROBE A: 第二趟读结束 = %v", err)
			break
		}
		if time.Now().After(deadline) {
			t.Logf("PROBE A: 第二趟卡住")
			break
		}
	}
	s2.Close()
	mu.Lock()
	t.Logf("PROBE A: 两趟的 Range 起点 = %v", ranges)
	mu.Unlock()
	raw2, _ := os.ReadFile(offsetPath)
	t.Logf("PROBE A: 第二趟后账本 = %q（没变 = 永远放不出来）", string(raw2))
}

// PROBE B: 两代源同时写同一个缓存文件 + 旧一代 Close 迟到
// → 账本被写成旧一代的前沿（比新一代的真实前沿大）
// → 新一代只下到比旧前沿少的地方就死 → 盘上 [新前沿, 旧前沿) 是洞
// → 下一趟按账本续传 → 从旧前沿起 → 洞被跳过 → 解码器读到零。
func TestProbeBTwoGenerationsSameFileLedgerPromotesStaleFrontier(t *testing.T) {
	data := payload(512 << 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs := rangeStart(r)
		if rs < 0 {
			rs = 0
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)-int(rs)))
		w.Write(data[rs:])
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	cachePath := filepath.Join(dir, "song.bin")
	offsetPath := filepath.Join(dir, "song.bin.off")

	// 旧一代：下到接近全部。
	s1, err := NewStreamSource(StreamSourceOptions{
		URLs: []string{srv.URL}, Client: srv.Client(),
		CachePath: cachePath, OffsetPath: offsetPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	deadline := time.Now().Add(5 * time.Second)
	for s1.Filled() < int64(len(data))*9/10 && time.Now().Before(deadline) {
		s1.Read(buf)
		time.Sleep(2 * time.Millisecond)
	}
	oldFilled := s1.Filled()
	t.Logf("PROBE B: 旧一代 filled = %d", oldFilled)

	// 新一代：同一文件（O_TRUNC），只下一点点。
	s2, err := NewStreamSource(StreamSourceOptions{
		URLs: []string{srv.URL}, Client: srv.Client(),
		CachePath: cachePath, OffsetPath: offsetPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	s2.Read(buf)
	time.Sleep(30 * time.Millisecond)
	newFilled := s2.Filled()
	t.Logf("PROBE B: 新一代 filled = %d（新一代还没追上旧一代）", newFilled)

	// 旧一代 Close（迟到）：它把**旧**的前沿写回账本。
	s1.Close()
	raw, _ := os.ReadFile(offsetPath)
	t.Logf("PROBE B: 旧一代关源后账本 = %q（真实连续前缀其实只有 %d）", string(raw), newFilled)

	// 新一代也关掉。
	s2.Close()
	raw2, _ := os.ReadFile(offsetPath)
	st, _ := os.Stat(cachePath)
	t.Logf("PROBE B: 新一代关源后账本 = %q，盘上大小 = %d", string(raw2), st.Size())

	// 盘上 [newFilled, oldFilled) 是不是零（洞）？
	f, err := os.Open(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	buf2 := make([]byte, oldFilled-newFilled)
	if oldFilled > newFilled {
		if _, err := f.ReadAt(buf2, newFilled); err == nil {
			allZero := true
			for _, b := range buf2 {
				if b != 0 {
					allZero = false
					break
				}
			}
			t.Logf("PROBE B: 盘上 [%d,%d) 全是零 = %v（洞！）", newFilled, oldFilled, allZero)
		}
	}

	// 下一趟按账本续传：从 oldFilled 起 → 洞被跳过。
	ledger, _ := strconv.ParseInt(string(raw2), 10, 64)
	t.Logf("PROBE B: 下一趟会从 %d 续传（洞起点 %d 被跳过 → 解码器读到零）", ledger, newFilled)
}
