package audio

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// PROBE P: `done:` 标记是「粘」的。
//
// 形状：上一次完整下完 → reportComplete 写 done: → 缓存层 SaveBytes 想删它
// 但**删不掉**（权限 / 杀毒软件 / 只读挂载）。
// 下一趟：应用看到 done: → resume=0 → O_TRUNC → 盘上那份被清掉重下。
// 新一代写账本时，writeOffset 读到 done: → **拒写**。
// 于是账本永远停在 done: → 这一首的续传功能**永久死掉**（每次都从零重下）。
func TestProbePStickyDoneMarkerBlocksFutureLedgers(t *testing.T) {
	data := payload(32 << 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs := rangeStart(r)
		if rs < 0 {
			rs = 0
		}
		// 只发一半，永远不发完 → 新一代不会 reportComplete。
		end := rs + int64(len(data)/2)
		if end > int64(len(data)) {
			end = int64(len(data))
		}
		w.Header().Set("Content-Length", strconv.FormatInt(int64(len(data))-rs, 10))
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

	// 盘上：完整的字节 + 一个 done: 标记（SaveBytes 删不掉留下的）。
	if err := os.WriteFile(cachePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(offsetPath, []byte("done:"+strconv.Itoa(len(data))), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := NewStreamSource(StreamSourceOptions{
		URLs: []string{srv.URL}, Client: srv.Client(),
		CachePath: cachePath, OffsetPath: offsetPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := s.Read(buf); err != nil {
			t.Logf("PROBE P: 读结束 = %v", err)
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	filled := s.Filled()
	s.Close()
	raw, rerr := os.ReadFile(offsetPath)
	t.Logf("PROBE P: 新一代 filled=%d，关源后账本=%q（err=%v）", filled, string(raw), rerr)
	if rerr != nil {
		t.Logf("PROBE P: 账本没了——不成立")
		return
	}
	st, _ := os.Stat(cachePath)
	t.Logf("PROBE P: 盘上大小=%d", st.Size())
	if string(raw) == "done:"+strconv.Itoa(len(data)) {
		t.Logf("PROBE P: ⟵ 账本还是那个 done: ——这一首的续传**永久死掉**（每次都从零重下）")
	}
}
