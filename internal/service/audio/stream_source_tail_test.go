package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// 这一组盯的是「**并行把文件尾部取回来**」那一条链路（票 #478 的容器层修法）。
//
// # 它解决的是什么
//
// B 站形状的完整单文件 `.m4a` 的 `moov` 盒**在文件末尾**，而
// AudioToolbox 探 MP4 容器时要扫过所有顶层盒子才找得到 `moov`——
// `moov` 在末尾时它得扫到文件末尾。
//
// ⚠️ **实测它不是从头读到尾的，它是「跳」过去的**（2026-10-25 实测：
// 它读 0 / 8 / 16 / 28 / 36 这几个位置，然后**直接跳到 `moov` 的
// 起点**）。所以：**末尾那一小段在手上，AudioToolbox 立刻就能开口**。
//
// 而在那之前，源只有**顺序下载的前沿**能用——于是「跳过去」的那一步
// 会等**整个文件下完**（票 #220 的实测：200 KB/s 的网上 1.22 秒）。
//
// # 为什么这些判据是「形状」而不是「毫秒」
//
// AGENTS.md §2：不要用固定毫秒当判据（机器一慢就飘）。这里的判据是
// **「文件还没下完，那一段就已经能读了」**——那是一个**事实**，
// 跟机器快慢无关。

// moovAt 造一份素材，说出它的 `moov` 在第几个字节。
//
// ⚠️ **自己走一遍盒子，而不是写死一个数**：素材换了、盒子布局变了，
// 写死的那个数会静默地指错地方——而「指错地方」的形态是
// 「尾部那一段取回来了，但取的其实不是 `moov`」，测试照样绿。
func moovAt(t *testing.T, data []byte) int64 {
	t.Helper()
	off := int64(0)
	for off+8 <= int64(len(data)) {
		size := int64(binary.BigEndian.Uint32(data[off : off+4]))
		typ := string(data[off+4 : off+8])
		head := int64(8)
		if size == 1 {
			size = int64(binary.BigEndian.Uint64(data[off+8 : off+16]))
			head = 16
		} else if size == 0 {
			size = int64(len(data)) - off
		}
		if typ == "moov" {
			return off
		}
		if size < head {
			t.Fatalf("素材的盒子长度不对（第 %d 个字节处）", off)
		}
		off += size
	}
	t.Fatalf("素材里没有 moov 盒")
	return 0
}

// readMP4Fixture 读一份「moov 在末尾」的素材。
func readMP4Fixture(t *testing.T, name string) []byte {
	t.Helper()
	p := filepath.Join("flacdec", "testdata", name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Skipf("拿不到素材 %s：%v", name, err)
	}
	return b
}

// slowRangeCDN 是一台「认 Range、按固定速率给字节」的 CDN。
//
// ⚠️ **它必须认 Range**：不认的话尾部那一段取不回来（服务器会回 200 +
// 整个文件，而那一段只能作废——见 `fetchTail`）。
//
// 限速是这一组的核心：没有它，「文件还没下完」这个状态根本不存在。
func slowRangeCDN(data []byte, bytesPerSec int) http.HandlerFunc {
	chunk := bytesPerSec / 20
	if chunk < 512 {
		chunk = 512
	}
	per := time.Duration(float64(time.Second) * float64(chunk) / float64(bytesPerSec))
	return func(w http.ResponseWriter, r *http.Request) {
		from := rangeStart(r)
		if from > int64(len(data)) {
			http.Error(w, "越界", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		body := data[from:]
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		if from > 0 {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, len(data)-1, len(data)))
			w.WriteHeader(http.StatusPartialContent)
		}
		fl, _ := w.(http.Flusher)
		for off := 0; off < len(body); off += chunk {
			end := off + chunk
			if end > len(body) {
				end = len(body)
			}
			if _, err := w.Write(body[off:end]); err != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
			time.Sleep(per)
		}
	}
}

// TestTheTrailingMoovIsFetchedInParallel 盯「末尾那一段是**并行**取回来的」。
//
// # 这一条钉的是什么
//
// 「`moov` 在文件末尾」这件事以前意味着：**解码器要等整个文件下完**
// （票 #220 的实测：200 KB/s 的网上 1.22 秒，50 KB/s 的网上 5.02 秒）。
//
// 修法是**并行发一条 Range 请求**把末尾那一小段单独取回来。
//
// # 判据是「形状」不是「毫秒」
//
// **「文件还没下完，那一段就已经能读了」**——这是一个事实，
// 不受机器快慢影响（AGENTS.md §2）。而「等了多久」那种判据会飘。
func TestTheTrailingMoovIsFetchedInParallel(t *testing.T) {
	data := readMP4Fixture(t, "stereo16.mp4")
	moov := moovAt(t, data)
	// 这份素材的 `moov` 必须在文件末尾（否则这一条测的不是那个形状）。
	if moov < int64(len(data))/2 {
		t.Fatalf("这份素材的 moov 不在末尾（%d / %d）", moov, len(data))
	}

	srv := httptest.NewServer(slowRangeCDN(data, 50_000))
	defer srv.Close()

	src, err := NewStreamSource(StreamSourceOptions{
		URLs:   []string{srv.URL},
		Client: srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	// 等尾部那一段到。
	deadline := time.Now().Add(20 * time.Second)
	for {
		if src.tailBytes() > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("尾部那一段一直没取回来")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// 🔴 **关键**：文件**远没有**下完，而末尾那一段已经能读了。
	if filled := src.Filled(); filled >= int64(len(data)) {
		t.Fatalf("文件已经整个下完了（%d / %d）——这一条测的是「并行」", filled, len(data))
	}
	if _, err := src.Seek(moov, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, err := src.Read(buf)
	if err != nil {
		t.Fatalf("读末尾那一段：%v", err)
	}
	if n == 0 {
		t.Fatal("末尾那一段读出来是空的")
	}
	// 读出来的应该是 `moov` 盒的头（长度 + 类型）。
	if got := string(buf[4:8]); got != "moov" {
		t.Errorf("读到的不是 moov 盒的头：%q", got)
	}
}

// TestTheTailIsFetchedEvenWhenTheBoxHeaderIsFarAway 盯「**盒子头在很后面**
// 时，末尾那一段照样取得到」。
//
// # 这一条钉的是什么
//
// 盒子头不一定在前 512 字节里——一个大的 `free`/`sidx` 盒子排在
// `mdat`/`moov` 前面时，`mdat` 的头在**好几兆之外**。而那时候源要**等**
// 顺序下载追上那个偏移。
//
// ⚠️ **「等」必须是阻塞的**：写成「空转重试」的话，那个 goroutine 会
// **把一个 CPU 核占满**，直到下载追上（2026-10-25 的评审实测：墙钟
// 6.1 秒里 CPU 占了 5.8 秒）。CPU 那半边由
// `stream_source_tail_cpu_test.go`（Unix 才有 `getrusage`）钉着；
// 这一条钉的是**功能**：等到了之后，末尾那一段照样取得到。
func TestTheTailIsFetchedEvenWhenTheBoxHeaderIsFarAway(t *testing.T) {
	// 一份「`free` 盒子很大、`mdat` 的头在很后面」的素材。
	rest := concat(
		box("ftyp", make([]byte, 20)),
		box("free", make([]byte, 256<<10)),
		box("mdat", make([]byte, 64<<10)),
		box("moov", make([]byte, 100)),
	)
	moov := moovAt(t, rest)
	// 前提：`mdat` 的头落在前 512 字节之外（否则走不到「等」那一条路）。
	if moov <= mp4HeadMin {
		t.Fatalf("这份素材的 moov 不在够远的地方（%d）", moov)
	}

	srv := httptest.NewServer(slowRangeCDN(rest, 200_000))
	defer srv.Close()

	src, err := NewStreamSource(StreamSourceOptions{
		URLs:   []string{srv.URL},
		Client: srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	// 末尾那一段最后还是取回来了（等到了那个偏移之后）。
	deadline := time.Now().Add(30 * time.Second)
	for src.tailBytes() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("盒子头在很后面时，末尾那一段一直没取回来")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := src.Seek(moov, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	if _, err := src.Read(buf); err != nil {
		t.Fatalf("读末尾那一段：%v", err)
	}
	if got := string(buf[4:8]); got != "moov" {
		t.Errorf("读到的不是 moov 盒的头：%q", got)
	}
}

// TestTheTrailingMoovIsNotFetchedWhenItIsAlreadyAtTheFront 盯「`moov` 在
// 文件前面时**不要**多发那条请求」。
//
// ⚠️ **那是一条白花的请求**：faststart 的文件（`moov` 在开头）解码器
// 几十个字节就能开口，多发一条 Range 请求纯属浪费——而用户那边看到的是
// 「B 站那边多了一个请求」。
func TestTheTrailingMoovIsNotFetchedWhenItIsAlreadyAtTheFront(t *testing.T) {
	data := readMP4Fixture(t, "reference-24bit-mp4.m4a")
	moov := moovAt(t, data)
	if moov > 1024 {
		t.Fatalf("这份素材的 moov 不在开头（%d）", moov)
	}

	var mu sync.Mutex
	var ranges []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ranges = append(ranges, r.Header.Get("Range"))
		mu.Unlock()
		slowRangeCDN(data, 200_000)(w, r)
	}))
	defer srv.Close()

	src, err := NewStreamSource(StreamSourceOptions{
		URLs:   []string{srv.URL},
		Client: srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	// 等文件头下到（那之后才会判断要不要取尾部）。
	deadline := time.Now().Add(20 * time.Second)
	for src.Filled() < mp4HeadMin {
		if time.Now().After(deadline) {
			t.Fatal("文件头一直没下到")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// 再给它一点时间：判断「要不要取尾部」就是在这一段里做的。
	time.Sleep(300 * time.Millisecond)

	if off := src.tailOffset(); off >= 0 {
		t.Errorf("`moov` 在开头的文件不该去取尾部（尾部起点=%d）", off)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, r := range ranges {
		if r != "" {
			t.Errorf("多发了一条 Range 请求：%q", r)
		}
	}
}
