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

// PROBE J: **refreshOnlineNowFor 的形状**——同一首同一档，
// 新源建起来时旧源**还活着**（old.Close() 在建好新源之后才调）。
//
//	旧源 S1：已下到 X，还在写
//	新源 S2：建源 → O_TRUNC 打开**同一个**文件 → 盘上被清零 → S2 从 0 重下
//	S1 继续从 X 往后写 → 盘上 [S2前沿, X) 变成**洞**（零）
//	S1 Close（迟到）：账本被写成 X+（S1 的前沿）——「只许前进」让它赢
//	盘上大小 = X+ ≥ 账本 → 应用层 resumeOffsetFor 放行
//	下一趟：Range 从 X+ 起 → 洞 [S2前沿, X) 永远填不上 → 解码器读零
func TestProbeJRefreshReopenLeavesHoleAndLyingLedger(t *testing.T) {
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
		// 旧源发 300KB；新源发 64KB 就卡住。
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

	// 旧源 S1。
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
	t.Logf("PROBE J: 旧源 S1 前沿 = %d", oldFilled)

	// 新源 S2：账本还没有 → resume=0 → O_TRUNC 打开同一个文件。
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
	t.Logf("PROBE J: 新源 S2 前沿 = %d，盘上大小 = %d", newFilled, st.Size())

	// 旧源 Close（迟到的收尾）。
	s1.Close()
	raw, _ := os.ReadFile(offsetPath)
	t.Logf("PROBE J: S1 关源后账本 = %q（真正连续的只有 %d）", string(raw), newFilled)

	// 新源 Close：只到 64KB < S1 的 300KB → 被「只许前进」挡掉。
	s2.Close()
	raw2, _ := os.ReadFile(offsetPath)
	st2, _ := os.Stat(cachePath)
	ledger, perr := strconv.ParseInt(string(raw2), 10, 64)
	t.Logf("PROBE J: S2 关源后账本 = %q，盘上大小 = %d", string(raw2), st2.Size())
	if perr != nil {
		t.Logf("PROBE J: 账本不是数字（没复现）")
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
			t.Logf("PROBE J: 盘上 [%d,%d) 全零 = %v ← 洞", newFilled, ledger, allZero)
		}
	}
	t.Logf("PROBE J: 应用层 resumeOffsetFor 采信 %d 吗？%v（盘上大小 %d）→ 洞被永久跳过",
		ledger, ledger <= st2.Size(), st2.Size())
}
