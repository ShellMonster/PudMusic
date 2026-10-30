package audio

// 字节落盘缓存：下过的字节**留在盘上**，切走之后还能拿回来。
//
// # 这条盯的是真问题（2026-10-30 用户提出来的）
//
// 原来的 `StreamSource` 用 `os.CreateTemp` 建一个**随机名**的临时文件，
// 而 `Close` 把它删掉。于是用户 A → B → A 这样来回切歌时：
//
//	A 第一次播 → 整首下到盘上 → 切走 → **文件删了**
//	A 第二次播 → 又从零开始下一个整首 → 又走一遍网络
//
// 用户的原话是「前面比如取到了的，那应该是在本地电脑缓存上啊，
// 为啥还要再去跑网络请求；应该是没有才跑网络请求啊」。
//
// # 判据
//
// 1. 给了 `CachePath` 时，源用**那个路径**当缓存文件（不是随机名的临时文件）。
// 2. `Close` **不删**那个文件——它是缓存，不是垃圾。
// 3. 没有给 `CachePath` 时，行为**一个字都不变**（临时文件照旧删）。
//
// ⚠️ **第 3 条是底线**：在线播放的临时文件是「切歌不遗留」那道门
// （见 `TestCloseRemovesTheCacheFileAndJoinsEveryGoroutine`），
// 把它改成「不删」会让临时目录越攒越多。

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestTheCachePathIsUsedAndSurvivesClose 盯「持久缓存路径」。
func TestTheCachePathIsUsedAndSurvivesClose(t *testing.T) {
	data := payload(64 << 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveBytes(w, r, data, len(data))
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	cachePath := filepath.Join(dir, "song.bin")

	s, err := NewStreamSource(StreamSourceOptions{
		URLs:      []string{srv.URL},
		Client:    srv.Client(),
		CachePath: cachePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.path != cachePath {
		t.Errorf("缓存文件路径 = %q，want %q（给了 CachePath 就该用它）", s.path, cachePath)
	}

	// 把整首读出来，等它下完。
	//
	// ⚠️ **要一段一段地读**：这个源的 `Read` **只给连续的那一段**
	// （见 `availAt`），所以一次读不满是正常的——把读到的接起来才算数。
	buf := make([]byte, 0, len(data))
	chunk := make([]byte, 4096)
	deadline := time.Now().Add(10 * time.Second)
	for len(buf) < len(data) {
		n, err := s.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if !errors.Is(err, ErrSourceClosed) {
				t.Fatalf("读字节：%v", err)
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("10 秒还没读到整首（读到 %d 字节）", len(buf))
		}
	}
	if len(buf) != len(data) {
		t.Fatalf("读到 %d 字节，want %d", len(buf), len(data))
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// 🔴 **文件必须还在**：它是缓存，不是垃圾。
	st, err := os.Stat(cachePath)
	if err != nil {
		t.Fatalf("Close 之后缓存文件没了：%v", err)
	}
	if st.Size() != int64(len(data)) {
		t.Errorf("缓存文件大小 = %d，want %d", st.Size(), len(data))
	}
	// 盘上的字节要和下到的字节一样。
	got, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(data) {
		t.Fatalf("缓存文件读出来 %d 字节，want %d", len(got), len(data))
	}
	for i := range got {
		if got[i] != data[i] {
			t.Fatalf("第 %d 字节 = %#x，want %#x", i, got[i], data[i])
		}
	}
}

// TestWithoutACachePathTheTempFileIsStillRemoved 盯「没给缓存路径时行为不变」。
//
// 🔴 **这是底线**：临时文件是「切歌不遗留」那道门，改成不删会让临时目录
// 越攒越多（而临时目录是会被系统清掉的，那些字节就白下了）。
func TestWithoutACachePathTheTempFileIsStillRemoved(t *testing.T) {
	data := payload(32 << 10)
	hold := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(1<<20))
		w.Write(data[:4096])
		w.(http.Flusher).Flush()
		<-hold
	}))
	t.Cleanup(func() { close(hold); srv.Close() })

	dir := t.TempDir()
	s, err := NewStreamSource(StreamSourceOptions{
		URLs:    []string{srv.URL},
		Client:  srv.Client(),
		TempDir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	cachePath := s.path
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(cachePath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("临时文件还在：%s（检查：%v）", cachePath, err)
	}
}
