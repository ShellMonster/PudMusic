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

// PROBE H: 两条源写**同一个**缓存文件（新源 O_TRUNC 打开），
// 旧源 Close 迟到 → 账本被写成旧源的前沿（大于真正连续的字节数）
// → 下一趟按账本续传 → 洞被跳过 → 解码器读到零。
//
// 关键：**服务器必须卡住**，让两代都下不完（否则 reportComplete 把账本退役了）。
func TestProbeHLedgerCanExceedRealContiguousPrefix(t *testing.T) {
	data := payload(1024 << 10)
	var mu sync.Mutex
	var n int
	gate := make(chan struct{})
	t.Cleanup(func() { close(gate) })
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
		// 只发一段，然后卡住（两代都不下完）。第一个请求（旧源）发得多一些。
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
		select {
		case <-gate:
		case <-r.Context().Done():
		}
		_ = which
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	cachePath := filepath.Join(dir, "song.bin")
	offsetPath := filepath.Join(dir, "song.bin.off")

	// 旧源 S1：先下 64KB，然后卡住。
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
	t.Logf("PROBE H: 旧源 S1 前沿 = %d", oldFilled)

	// 新源 S2：同一个文件，账本还没有 → resume=0 → O_TRUNC。
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
	t.Logf("PROBE H: 新源 S2 前沿 = %d，盘上大小 = %d（O_TRUNC 清零后重下）", newFilled, st.Size())

	// 旧源 Close（迟到的收尾）。
	s1.Close()
	raw, err := os.ReadFile(offsetPath)
	t.Logf("PROBE H: S1 关源后账本 = %q（err=%v）——真正连续的只有 %d", string(raw), err, newFilled)

	// 新源 Close：它只到 newFilled < oldFilled → 被「只许前进」挡掉。
	s2.Close()
	raw2, err := os.ReadFile(offsetPath)
	st2, _ := os.Stat(cachePath)
	t.Logf("PROBE H: S2 关源后账本 = %q（err=%v），盘上大小 = %d", string(raw2), err, st2.Size())

	ledger, perr := strconv.ParseInt(string(raw2), 10, 64)
	if perr != nil {
		t.Logf("PROBE H: 账本不是数字（这一条没复现）")
		return
	}
	// 盘上 [newFilled, ledger) 是洞吗？
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
			t.Logf("PROBE H: 盘上 [%d,%d) 全零 = %v（洞！）", newFilled, ledger, allZero)
		}
	}
	t.Logf("PROBE H: 应用层 resumeOffsetFor 会采信 %d 吗？%v（盘上大小 %d）",
		ledger, ledger <= st2.Size(), st2.Size())
}
