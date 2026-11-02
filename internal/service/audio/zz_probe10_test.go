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

// PROBE K: **两条源写同一个文件的洞**（refreshOnlineNowFor 的形状）。
//
// 旧源 S1：已下到 300KB，**还在写**（服务器慢慢发）。
// 新源 S2：建源 → O_TRUNC 打开同一个文件 → 盘上清零 → S2 从 0 重下，
//
//	但 S1 **不知道**盘上被截断了，它继续从自己的前沿 300KB 往后写
//	→ 盘上 [S2前沿, 300KB) 是**洞**（零）
//
// S1 Close（迟到的收尾）：账本被写成 300KB+（S1 的前沿）——「只许前进」让它赢。
// 盘上大小 = 300KB+ ≥ 账本 → 应用层 resumeOffsetFor **放行**。
// 下一趟：Range 从 300KB+ 起 → 洞 [S2前沿,300KB) 永远填不上 → 解码器读零。
func TestProbeKHoleInFileWithLedgerPointingPastIt(t *testing.T) {
	data := payload(1024 << 10)
	var mu sync.Mutex
	var n int
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
		// 旧源发 300KB；新源发 64KB。然后都卡住。
		chunk := int64(64 << 10)
		if which == 1 {
			chunk = 300 << 10
		}
		end := rs + chunk
		if end > int64(len(data)) {
			end = int64(len(data))
		}
		w.Write(data[rs:end])
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	cachePath := filepath.Join(dir, "song.bin")
	offsetPath := filepath.Join(dir, "song.bin.off")

	s1, err := NewStreamSource(StreamSourceOptions{
		URLs: []string{srv.URL}, Client: srv.Client(),
		CachePath: cachePath, OffsetPath: offsetPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	deadline := time.Now().Add(3 * time.Second)
	for s1.Filled() < 300<<10 && time.Now().Before(deadline) {
		s1.Read(buf)
		time.Sleep(time.Millisecond)
	}
	oldFilled := s1.Filled()

	// 新源 S2：O_TRUNC 打开同一个文件。
	s2, err := NewStreamSource(StreamSourceOptions{
		URLs: []string{srv.URL}, Client: srv.Client(),
		CachePath: cachePath, OffsetPath: offsetPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	// S2 下到 64KB 卡住。
	deadline = time.Now().Add(3 * time.Second)
	for s2.Filled() < 64<<10 && time.Now().Before(deadline) {
		s2.Read(buf)
		time.Sleep(time.Millisecond)
	}
	newFilled := s2.Filled()
	st, _ := os.Stat(cachePath)
	t.Logf("PROBE K: 旧源 S1 前沿 = %d，新源 S2 前沿 = %d，盘上大小 = %d", oldFilled, newFilled, st.Size())

	// 旧源 Close（迟到）：它把旧前沿写回账本。
	s1.Close()
	raw, _ := os.ReadFile(offsetPath)
	t.Logf("PROBE K: S1 关源后账本 = %q", string(raw))

	s2.Close()
	raw2, _ := os.ReadFile(offsetPath)
	st2, _ := os.Stat(cachePath)
	ledger, perr := strconv.ParseInt(string(raw2), 10, 64)
	t.Logf("PROBE K: S2 关源后账本 = %q，盘上大小 = %d", string(raw2), st2.Size())
	if perr != nil {
		t.Logf("PROBE K: 账本不是数字（没复现）")
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
			t.Logf("PROBE K: 盘上 [%d,%d) 全零 = %v ← 洞", newFilled, ledger, allZero)
		}
	}
	t.Logf("PROBE K: 应用层 resumeOffsetFor 采信 %d 吗？%v（盘上大小 %d）",
		ledger, ledger <= st2.Size(), st2.Size())
}
