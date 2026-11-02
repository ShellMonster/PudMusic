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

// PROBE M: **洞真的出现了**（refreshOnlineNowFor 的形状）。
//
// 旧源 S1：下到 300KB（**还在写**）。
// 新源 S2：建源 → O_TRUNC 打开同一个文件 → 盘上清零 → S2 从 0 重下（慢，只到 64KB）。
//
//	这期间 S1 继续从自己的前沿 300KB 往后写 → 盘上变成：
//	    [0, 64KB)     S2 写的
//	    [64KB, 300KB) **洞**（谁都没写）
//	    [300KB, ...)  S1 写的
//
// S1 Close（迟到的收尾）：账本被写成 S1 的前沿（≈300KB+）——「只许前进」让它赢。
// 盘上大小 ≈ 账本 → 应用层 resumeOffsetFor **放行**。
// 下一趟：Range 从 300KB 起 → 洞 [64KB,300KB) 永远填不上 → 解码器读零。
func TestProbeMRealHoleSurvivesIntoNextLaunch(t *testing.T) {
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
		for {
			end := rs + 32<<10
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
				// 新源慢：让它停在 64KB 附近。
				select {
				case <-release:
				case <-r.Context().Done():
					return
				case <-time.After(200 * time.Millisecond):
				}
				if rs > 64<<10 {
					return
				}
			} else {
				select {
				case <-release:
				case <-r.Context().Done():
					return
				case <-time.After(5 * time.Millisecond):
				}
			}
		}
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
	t.Logf("PROBE M: 截断前 S1 前沿 = %d", atTrunc)

	// 新源 S2：O_TRUNC 打开同一个文件。
	s2, err := NewStreamSource(StreamSourceOptions{
		URLs: []string{srv.URL}, Client: srv.Client(),
		CachePath: cachePath, OffsetPath: offsetPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 等一会儿：S1 继续写，S2 慢慢下。
	time.Sleep(400 * time.Millisecond)
	newFilled := s2.Filled()
	oldNow := s1.Filled()
	st, _ := os.Stat(cachePath)
	t.Logf("PROBE M: 现在 S1=%d，S2=%d，盘上大小=%d", oldNow, newFilled, st.Size())

	s1.Close()
	s2.Close()
	raw, _ := os.ReadFile(offsetPath)
	st2, _ := os.Stat(cachePath)
	ledger, perr := strconv.ParseInt(string(raw), 10, 64)
	t.Logf("PROBE M: 关源后账本 = %q，盘上大小 = %d", string(raw), st2.Size())
	if perr != nil {
		t.Logf("PROBE M: 账本不是数字（没复现）")
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
			t.Logf("PROBE M: 盘上 [%d,%d) 全零 = %v ← 洞（%d 字节）", newFilled, ledger, allZero, ledger-newFilled)
		}
	}
	t.Logf("PROBE M: 应用层 resumeOffsetFor 采信 %d 吗？%v（盘上大小 %d）→ 洞被永久跳过",
		ledger, ledger <= st2.Size(), st2.Size())
}
