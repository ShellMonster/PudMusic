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

// PROBE L: **两条源写同一个文件，洞真的出现了**。
//
// 旧源 S1：服务器**慢慢**发，它会一直写。
// 新源 S2：建源 → O_TRUNC → 盘上清零 → S2 从 0 重下（慢）。
// 这期间 S1 继续从自己的前沿往后写 → 盘上 [S2前沿, S1前沿) 是**洞**。
// S1 Close：账本 = S1 的前沿。盘上大小 ≥ 账本（因为 S1 写到了那儿）。
// 应用层放行 → 下一趟从 S1 前沿续传 → 洞永远填不上。
func TestProbeLRealHoleSurvivesIntoNextLaunch(t *testing.T) {
	data := payload(1024 << 10)
	var mu sync.Mutex
	var n int
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
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
		// 每 64KB 一块，慢慢发。
		for {
			end := rs + 64<<10
			if end > int64(len(data)) {
				end = int64(len(data))
			}
			if rs >= end {
				return
			}
			w.Write(data[rs:end])
			rs = end
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			if which == 2 {
				// 新源慢一点。
				select {
				case <-release:
				case <-r.Context().Done():
					return
				case <-time.After(150 * time.Millisecond):
				}
			} else {
				select {
				case <-release:
				case <-r.Context().Done():
					return
				case <-time.After(10 * time.Millisecond):
				}
			}
		}
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	cachePath := filepath.Join(dir, "song.bin")
	offsetPath := filepath.Join(dir, "song.bin.off")

	// 旧源 S1：先下到 128KB。
	s1, err := NewStreamSource(StreamSourceOptions{
		URLs: []string{srv.URL}, Client: srv.Client(),
		CachePath: cachePath, OffsetPath: offsetPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	deadline := time.Now().Add(3 * time.Second)
	for s1.Filled() < 128<<10 && time.Now().Before(deadline) {
		s1.Read(buf)
		time.Sleep(time.Millisecond)
	}
	oldAtTrunc := s1.Filled()

	// 新源 S2：O_TRUNC 打开同一个文件。
	s2, err := NewStreamSource(StreamSourceOptions{
		URLs: []string{srv.URL}, Client: srv.Client(),
		CachePath: cachePath, OffsetPath: offsetPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 让 S1 继续写（在 S2 截断之后）。
	time.Sleep(300 * time.Millisecond)
	newFilled := s2.Filled()
	oldNow := s1.Filled()
	st, _ := os.Stat(cachePath)
	t.Logf("PROBE L: 截断时 S1=%d，现在 S1=%d，S2=%d，盘上大小=%d", oldAtTrunc, oldNow, newFilled, st.Size())

	// 现在盘上 [S2前沿, S1前沿) 应该是洞（S1 的偏移 > S2 的前沿，中间没写过）。
	s1.Close()
	s2.Close()
	raw, _ := os.ReadFile(offsetPath)
	st2, _ := os.Stat(cachePath)
	ledger, perr := strconv.ParseInt(string(raw), 10, 64)
	t.Logf("PROBE L: 关源后账本 = %q，盘上大小 = %d", string(raw), st2.Size())
	if perr != nil {
		t.Logf("PROBE L: 账本不是数字（没复现）")
		return
	}
	f, err := os.Open(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if ledger > newFilled {
		hole := make([]byte, ledger-newFilled)
		if _, err := f.ReadAt(hole, newFilled); err == nil {
			allZero := true
			for _, b := range hole {
				if b != 0 {
					allZero = false
					break
				}
			}
			t.Logf("PROBE L: 盘上 [%d,%d) 全零 = %v ← 洞（%d 字节）", newFilled, ledger, allZero, ledger-newFilled)
		}
	}
	t.Logf("PROBE L: 应用层 resumeOffsetFor 采信 %d 吗？%v（盘上大小 %d）",
		ledger, ledger <= st2.Size(), st2.Size())
}
