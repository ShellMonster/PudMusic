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
	"sync/atomic"
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

// TestTheCompleteCallbackFiresOnlyWhenTheLengthIsKnown 盯「下完了」那一声。
//
// 🔴 **这一条是第一轮那个「截断被当完整」的防线**（2026-10-30 的第二轮
// 审查指出来的：它原来零覆盖）。
//
// 服务器不给 `Content-Length` 时，连接结束**可能**是「被掐了」而不是
// 「下完了」——而那两种从客户端这一侧**长得一模一样**（都是「读完了」）。
//
// ⚠️ **报错了的后果**：上层会把这一份标成「完整的缓存」，而用户切回来
// 听到的是**放了一半的歌**（或者一段噪声）。
//
// ⚠️ **不报的后果**：这一份不会被缓存（下次重新下一遍）——那只是
// 「没省下流量」，而不会让用户听到坏东西。
func TestTheCompleteCallbackFiresOnlyWhenTheLengthIsKnown(t *testing.T) {
	data := payload(32 << 10)

	// ① 服务器给了 `Content-Length` → 报「下完了」，而且长度对得上。
	t.Run("有 Content-Length 就报", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			w.Write(data)
		}))
		t.Cleanup(srv.Close)

		// 🔴 **原子量**：那个回调跑在**下载 goroutine** 里，而这一边在读
		// （普通变量在这里是一个实实在在的数据竞争，`-race` 一跑就红）。
		var got atomic.Int64
		got.Store(-1)
		var calls atomic.Int32
		s, err := NewStreamSource(StreamSourceOptions{
			URLs:   []string{srv.URL},
			Client: srv.Client(),
			OnComplete: func(n int64) {
				got.Store(n)
				calls.Add(1)
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })

		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) && calls.Load() == 0 {
			time.Sleep(5 * time.Millisecond)
		}
		if calls.Load() == 0 {
			t.Fatal("等了 10 秒，「下完了」还没报出来")
		}
		if got.Load() != int64(len(data)) {
			t.Errorf("报的长度 = %d，want %d", got.Load(), len(data))
		}
		// 🔴 **只报一次**。
		time.Sleep(200 * time.Millisecond)
		if calls.Load() != 1 {
			t.Errorf("报 %d 次，want 1", calls.Load())
		}
	})

	// ② 服务器不给 `Content-Length`（chunked）→ **不报**。
	t.Run("不知道有多长就不报", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 不设 Content-Length → Go 用 chunked。
			w.Write(data)
		}))
		t.Cleanup(srv.Close)

		var calls atomic.Int32
		s, err := NewStreamSource(StreamSourceOptions{
			URLs:       []string{srv.URL},
			Client:     srv.Client(),
			OnComplete: func(int64) { calls.Add(1) },
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })

		// 等这一趟真的结束（源报了 io.EOF 之后）。
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if s.Err() != nil {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		// 🔴 **不许报**：长度是猜的，而猜的那个数可能是**截断的长度**。
		if calls.Load() > 0 {
			t.Errorf("长度不知道却报了「下完了」（%d 次）——"+
				"上层会把这一份标成完整的缓存，用户切回来听到的是放了一半的歌", calls.Load())
		}
	})
}

// TestAClosedSourceNeverReportsComplete 盯「**中途被关的源不许报完成**」
// （2026-11-02 审查②的 P0-2）。
//
// # 为什么这一条要紧
//
// 「切歌把上一首收养给后台补完」让「一条正在下的源中途被 Close」从罕见
// （换音质 / 同首重开）变成了**日常**（切走再切回，每一次都是）。而 `pour`
// 的读循环在两次读之间看到「源被关了」时，曾经把它当成「读完了」返回
// nil——pump 于是以**截断的长度**报 `OnComplete`，上层把这一份标成
// 「完整的缓存」：文件大小和记录一致（都是截断的），大小对账**拦不住**，
// 用户从此每次点这一首都播一首被掐断的歌。
//
// # 判据
//
// 服务器把整首**一口气**倒出来（Content-Length 照给，`sizeKnown` 那道
// 防线管不到这里），读循环转得飞快；测试盯着 `Filled()` 过半的那一刻
// 当着泵的面把源关掉。**`OnComplete` 一次都不许响。**
func TestAClosedSourceNeverReportsComplete(t *testing.T) {
	data := payload(4 << 20)
	var completed atomic.Bool
	var completedSize atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		chunk := 4 << 10
		for off := 0; off < len(data); off += chunk {
			end := off + chunk
			if end > len(data) {
				end = len(data)
			}
			if _, err := w.Write(data[off:end]); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	s, err := NewStreamSource(StreamSourceOptions{
		URLs:      []string{srv.URL},
		Client:    srv.Client(),
		CachePath: filepath.Join(t.TempDir(), "song.bin"),
		OnComplete: func(size int64) {
			completed.Store(true)
			completedSize.Store(size)
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// 下载推到一半，当着泵的面把源关掉。
	deadline := time.Now().Add(10 * time.Second)
	for s.Filled() < int64(len(data)/2) {
		if time.Now().After(deadline) {
			t.Fatalf("下载没推进到一半（%d / %d），测不了这个场景", s.Filled(), len(data))
		}
	}
	s.Close()

	// 给泵一点时间：如果它把「被关」当成「读完」，OnComplete 已经在路上了。
	time.Sleep(300 * time.Millisecond)
	if completed.Load() {
		t.Fatalf("中途被关的源报了「下完了」（size=%d，总长 %d）——"+
			"缓存会记下一首截断的歌，用户从此每次点这一首都播它",
			completedSize.Load(), len(data))
	}
}
