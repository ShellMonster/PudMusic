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

// PROBE N: **洞真的出现了，而且账本指的正是洞之后的位置**。
//
// 旧源 S1：下到 300KB 后**继续写**（服务器慢慢发），但**永远下不完**。
// 新源 S2：建源 → O_TRUNC 打开同一个文件 → 盘上清零 → S2 从 0 重下到 64KB。
//
//	盘上此刻：
//	    [0, 64KB)      S2 写的（对）
//	    [64KB, 300KB)  **洞**（谁都没写：S1 的前沿已经越过这儿了）
//	    [300KB, ...)   S1 写的（对）
//
// S1 Close（迟到的收尾）：账本被写成 S1 的前沿 ≈350KB（「只许前进」让它赢）。
// 盘上大小 ≈350KB ≥ 账本 → 应用层 resumeOffsetFor **放行**。
// 下一趟：Range 从 350KB 起 → 洞 [64KB,300KB) **永远填不上** → 解码器读零。
func TestProbeNRealHoleWithLedgerPointingPastIt(t *testing.T) {
	data := payload(2048 << 10)
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
		// 新源只发 64KB；旧源发很多但**永远发不完**。
		limit := int64(len(data))
		if which == 2 {
			limit = 64 << 10
		}
		for {
			end := rs + 16<<10
			if end > limit {
				end = limit
			}
			if rs >= end {
				break
			}
			w.Write(data[rs:end])
			rs = end
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			select {
			case <-release:
				return
			case <-r.Context().Done():
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
		// 发完这一段就不发了（卡住，等着被关掉）——**永远到不了 EOF**。
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
	deadline := time.Now().Add(5 * time.Second)
	for s1.Filled() < 300<<10 && time.Now().Before(deadline) {
		s1.Read(buf)
		time.Sleep(time.Millisecond)
	}
	atTrunc := s1.Filled()
	t.Logf("PROBE N: 截断前 S1 前沿 = %d", atTrunc)

	// 新源 S2：O_TRUNC 打开同一个文件。
	s2, err := NewStreamSource(StreamSourceOptions{
		URLs: []string{srv.URL}, Client: srv.Client(),
		CachePath: cachePath, OffsetPath: offsetPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 等：S1 继续写，S2 下到 64KB 卡住。
	time.Sleep(400 * time.Millisecond)
	newFilled := s2.Filled()
	oldNow := s1.Filled()
	st, _ := os.Stat(cachePath)
	t.Logf("PROBE N: 现在 S1=%d，S2=%d，盘上大小=%d", oldNow, newFilled, st.Size())

	// 先关 S2（它的收尾先到），再关 S1（迟到的收尾）。
	s2.Close()
	s1.Close()
	raw, _ := os.ReadFile(offsetPath)
	st2, _ := os.Stat(cachePath)
	ledger, perr := strconv.ParseInt(string(raw), 10, 64)
	t.Logf("PROBE N: 关源后账本 = %q，盘上大小 = %d", string(raw), st2.Size())
	if perr != nil {
		t.Logf("PROBE N: 账本不是数字（没复现）")
		return
	}
	f, err := os.Open(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if ledger > newFilled {
		// 洞其实只在 [S2前沿, S1截断前的前沿) 这一段。
		hole := make([]byte, atTrunc-newFilled)
		if _, err := f.ReadAt(hole, newFilled); err == nil {
			allZero := true
			for _, b := range hole {
				if b != 0 {
					allZero = false
					break
				}
			}
			t.Logf("PROBE N: 盘上 [%d,%d) 全零 = %v ← 洞（%d 字节）", newFilled, atTrunc, allZero, atTrunc-newFilled)
		}
		// 洞之后 [atTrunc, ledger) 是 S1 写的（内容对）。
		tail := make([]byte, ledger-atTrunc)
		if _, err := f.ReadAt(tail, atTrunc); err == nil {
			same := true
			for i, b := range tail {
				if b != data[atTrunc+int64(i)] {
					same = false
					break
				}
			}
			t.Logf("PROBE N: 盘上 [%d,%d) 与原始字节一致 = %v（S1 写的）", atTrunc, ledger, same)
		}
	}
	t.Logf("PROBE N: 应用层 resumeOffsetFor 采信 %d 吗？%v（盘上大小 %d）→ 洞被永久跳过",
		ledger, ledger <= st2.Size(), st2.Size())
}
