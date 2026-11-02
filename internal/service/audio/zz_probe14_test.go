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

// PROBE O: `done:` 标记是「粘」的——writeOffset 见到 done: 就永远拒写。
// 场景：完整下载 → reportComplete 写 done: → SaveBytes 删账本失败（模拟：
// 缓存目录只读 / 删不掉）→ 账本里留着 done:。
// 下一趟：应用看到 done: → resume=0 → O_TRUNC → 盘上的完整字节被清掉，从零重下。
// 而新一代的账本**永远写不进去**（done: 挡着）→ 这一首的续传功能永久死掉。
func TestProbeOStickyDoneMarker(t *testing.T) {
	data := payload(32 << 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs := rangeStart(r)
		if rs < 0 {
			rs = 0
		}
		w.Header().Set("Content-Length", strconv.FormatInt(int64(len(data))-rs, 10))
		w.Write(data[rs:])
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	cachePath := filepath.Join(dir, "song.bin")
	offsetPath := filepath.Join(dir, "song.bin.off")

	// 盘上：完整的字节 + 一个 done: 标记（SaveBytes 删不掉的形状）。
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
	for i := 0; i < 4; i++ {
		s.Read(buf)
	}
	time.Sleep(30 * time.Millisecond)
	filled := s.Filled()
	s.Close()
	raw, rerr := os.ReadFile(offsetPath)
	t.Logf("PROBE O: 新一代 filled=%d，关源后账本=%q（err=%v）", filled, string(raw), rerr)
	if rerr != nil {
		t.Logf("PROBE O: 账本被删了（SaveBytes 那次删成功了）——这一条不成立")
		return
	}
	st, _ := os.Stat(cachePath)
	t.Logf("PROBE O: 盘上大小=%d（原本是完整的 %d 字节）", st.Size(), len(data))
	if string(raw) != "done:"+strconv.Itoa(len(data)) {
		t.Logf("PROBE O: 账本被更新了（%q）——不是粘的", string(raw))
	}
}
