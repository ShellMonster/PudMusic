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

// PROBE Q: **续传那一趟的长度对账**。
//
// 续传时 from > 0 → 服务器回 206 + Content-Length = 剩下的长度。
// totalFrom(from, resp) = from + Content-Length = 总长 ✅（这一处是对的）
//
// 但 **Content-Range 的 /total** 没被用 —— 服务器只给 Content-Length 时，
// 206 的 Content-Length 是「剩下的那一段」，totalFrom 加上 from 得到总长。
//
// 真正的坑：**续传那一趟的 `s.size` 一开始是 -1**（Options.Size 没给）。
// commitTotal(total) → s.size = total，sizeKnown = true ✅。
//
// 但 `markTotal` 在 pump 正常读完时被调：markTotal(s.Filled()) ——
// 而 filled 是**这一趟的累计前沿**（resume + 这一趟下的）== 总长 ✅。
//
// 这一条探针量的是：**续传 + 尾部并行段** 时的 size 对账。
func TestProbeQResumeTotalAccounting(t *testing.T) {
	data := payload(64 << 10)
	var mu sync.Mutex
	var gotRange int64 = -1
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs := rangeStart(r)
		mu.Lock()
		gotRange = rs
		mu.Unlock()
		if rs < 0 {
			rs = 0
		}
		w.Header().Set("Content-Length", strconv.FormatInt(int64(len(data))-rs, 10))
		if rs > 0 {
			w.Header().Set("Content-Range", "bytes "+strconv.FormatInt(rs, 10)+"-"+strconv.Itoa(len(data)-1)+"/"+strconv.Itoa(len(data)))
			w.WriteHeader(http.StatusPartialContent)
		}
		w.Write(data[rs:])
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	cachePath := filepath.Join(dir, "song.bin")
	offsetPath := filepath.Join(dir, "song.bin.off")

	// 盘上已有前 16KB。
	if err := os.WriteFile(cachePath, data[:16<<10], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(offsetPath, []byte(strconv.Itoa(16<<10)), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := NewStreamSource(StreamSourceOptions{
		URLs: []string{srv.URL}, Client: srv.Client(),
		CachePath: cachePath, OffsetPath: offsetPath,
		ResumeFrom: 16 << 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 8192)
	got := 0
	deadline := time.Now().Add(5 * time.Second)
	for got < len(data) {
		n, err := s.Read(buf)
		got += n
		if err != nil {
			t.Logf("PROBE Q: 读结束 = %v（读到 %d 字节）", err, got)
			break
		}
		if time.Now().After(deadline) {
			t.Logf("PROBE Q: 卡住（读到 %d）", got)
			break
		}
	}
	t.Logf("PROBE Q: Range 起点 = %d，源报的 Size = %d（整首 %d），读到 %d",
		gotRange, s.Size(), len(data), got)
	s.Close()
	raw, _ := os.ReadFile(offsetPath)
	t.Logf("PROBE Q: 关源后账本 = %q", string(raw))
}
