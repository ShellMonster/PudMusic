package audio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 这一组盯的是在线播放最关键的一条：**换流之后接着播**。
//
// 为什么用 fixture 而不是真 B 站：过期这件事在真网上要么等两小时，要么
// 改系统时间，两条都不是测试。fixture 能把「下到第 N 字节时这条地址失效」
// 这个瞬间**精确地**造出来，而且能断言「换流之后是从第 N 字节接上的」——
// 真网连这个都断言不了。
//
// ⚠️ 被 mock 掉的是「B 站什么时候让地址失效」，**不是**换流逻辑本身：
// 续传、Range、备份地址轮换、长度比对都在真的跑。
//
// ⚠️ **走重试路径的用例都挂了 `Backoff: fastBackoff()`**：默认的退避是
// 1 秒 / 2 秒 / 4 秒（见 `stream_source_backoff_test.go`），用它的话这一包
// 会慢二十多秒——而这一包要验的是「重试之后接得上」，不是「等了多久」。
// 「等了多久」由那一组单独钉。

// payload 造一段确定内容的字节。
func payload(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7 + i/251)
	}
	return b
}

// rangeStart 读 Range 头里的起点，没有就是 0。
func rangeStart(r *http.Request) int64 {
	h := r.Header.Get("Range")
	if h == "" {
		return 0
	}
	var start int64
	if _, err := fmt.Sscanf(h, "bytes=%d-", &start); err != nil {
		return 0
	}
	return start
}

// serveBytes 写 [from, len(data)) 这段，并按 Content-Length 收尾。
func serveBytes(w http.ResponseWriter, r *http.Request, data []byte, size int) {
	from := rangeStart(r)
	if from > int64(len(data)) {
		http.Error(w, "越界", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	// Content-Length 写的是**这一次实际会发多少**，不是整段的长度。
	// 写成总长的话声明和实际对不上，客户端读到的是一个 unexpected EOF——
	// 而那恰好是这里想造出来的失败形态，会把真正的 bug 藏起来。
	w.Header().Set("Content-Length", fmt.Sprint(int64(len(data))-from))
	if from > 0 {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, len(data)-1, size))
		w.WriteHeader(http.StatusPartialContent)
	}
	w.Write(data[from:])
}

// cutAfter 造一段「发到 cutAt 就断」的服务器。
//
// 断点**不是** handler 一写完就掐连接，而是等 `gate` 被关掉才掐。
// 原因是 TCP：handler `Write` + `Flush` 之后立刻掐连接时，那 120 KB
// 未必都到了客户端，于是「换流从第几个字节接上」就成了一个随机的数——
// 而那正是这里要断言的东西。改成「测试先确认读到了这一段，再放行掐断」，
// 断言才立得住。
//
// cutAt 之后的请求交给 rest。
func cutAfter(data []byte, cutAt, total int, rest http.HandlerFunc) (*httptest.Server, chan struct{}) {
	gate := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if from := rangeStart(r); from < int64(cutAt) {
			w.Header().Set("Content-Length", fmt.Sprint(total))
			if from > 0 {
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, cutAt-1, total))
				w.WriteHeader(http.StatusPartialContent)
			}
			w.Write(data[from:cutAt])
			w.(http.Flusher).Flush()
			once.Do(func() { <-gate })
			panic(http.ErrAbortHandler)
		}
		rest(w, r)
	}))
	return srv, gate
}

// TestSourceDeliversBytesBeforeTheDownloadFinishes 盯「边下边播」。
//
// 用户点了播放就该听到声音，不是等整首歌下完。断言的是：拿到源之后**不等**
// 下载完就能读出前面的字节。
func TestSourceDeliversBytesBeforeTheDownloadFinishes(t *testing.T) {
	data := payload(256 << 10)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		w.Write(data[:16])
		w.(http.Flusher).Flush()
		<-release // 卡在这儿不往下发：模拟「下载还在进行」
		w.Write(data[16:])
	}))
	t.Cleanup(srv.Close)

	s, err := NewStreamSource(StreamSourceOptions{
		URLs:    []string{srv.URL},
		Client:  srv.Client(),
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	buf := make([]byte, 16)
	if _, err := io.ReadFull(s, buf); err != nil {
		t.Fatalf("下载还没完就该读到前 16 个字节：%v", err)
	}
	if !bytes.Equal(buf, data[:16]) {
		t.Errorf("读到的字节不对：% x", buf)
	}
	close(release)
}

// TestSourceSeekEndLearnsTheLengthFromContentLength 盯 extaf 开口要的那个长度。
//
// `extaf.Open` 第一件事就是 `Seek(0, SeekEnd)`，而 AudioFileID 的长度之后
// 要一直用。拿不到的话 AudioToolbox 会把文件当成 0 字节——不报错，只是没声音。
func TestSourceSeekEndLearnsTheLengthFromContentLength(t *testing.T) {
	data := payload(64 << 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveBytes(w, r, data, len(data))
	}))
	t.Cleanup(srv.Close)

	s, err := NewStreamSource(StreamSourceOptions{
		URLs:    []string{srv.URL},
		Client:  srv.Client(),
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	n, err := s.Seek(0, io.SeekEnd)
	if err != nil {
		t.Fatalf("Seek(0, SeekEnd)：%v", err)
	}
	if n != int64(len(data)) {
		t.Errorf("总长 = %d，want %d", n, len(data))
	}
}

// TestRefetchContinuesFromTheSameByte —— **这一票最要紧的一条**。
//
// 地址下到一半失效 → 换流 → **从原来的字节接着下** → 读的人拿到的是
// 一条连续的字节流，一个字节不多一个字节不少。
//
// 「位置没动」在这条链路上就是这句话：解码器读到的字节偏移没变，
// 它的采样位置也就没变。Biu 上游在这一步是直接换 `src`、**不恢复进度**
// 的（`play-list.ts` 的 `refreshCurrentAudioSource` 只赋 `audio.src`，
// 元素的位置归零），所以这一条是我们要比它做得好的地方。
func TestRefetchContinuesFromTheSameByte(t *testing.T) {
	const total = 300 << 10
	const failAt = 120 << 10
	data := payload(total)

	srv, gate := cutAfter(data, failAt, total, func(w http.ResponseWriter, r *http.Request) {
		serveBytes(w, r, data, total)
	})
	t.Cleanup(srv.Close)

	var (
		mu        sync.Mutex
		refetchAt = int64(-1)
		refetches int
	)
	s, err := NewStreamSource(StreamSourceOptions{
		URLs:    []string{srv.URL + "/v1.m4a"},
		Client:  srv.Client(),
		TempDir: t.TempDir(),
		Backoff: fastBackoff(),
		Refetch: func(_ context.Context, from int64) *StreamReply {
			mu.Lock()
			refetchAt, refetches = from, refetches+1
			mu.Unlock()
			return &StreamReply{
				URLs:      []string{srv.URL + "/v2.m4a"},
				Size:      total,
				ExpiresAt: time.Now().Add(time.Hour),
				Quality:   "320K",
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	// 先把断开之前那一段读完——顺带也是「换流之前用户听到的东西」。
	head := make([]byte, failAt)
	if _, err := io.ReadFull(s, head); err != nil {
		t.Fatalf("读断开前的那一段：%v", err)
	}
	if !bytes.Equal(head, data[:failAt]) {
		t.Fatal("断开前那一段的字节就不对")
	}
	close(gate)

	rest, err := io.ReadAll(s)
	if err != nil {
		t.Fatalf("换流之后应该能接着读完：%v", err)
	}
	got := append(head, rest...)
	if !bytes.Equal(got, data) {
		t.Errorf("换流之后读到的字节不连续：长度 %d，want %d", len(got), len(data))
	}

	mu.Lock()
	defer mu.Unlock()
	if refetches != 1 {
		t.Fatalf("换流了 %d 次，want 1 次", refetches)
	}
	if refetchAt != failAt {
		t.Errorf("换流从第 %d 字节接上的，want %d", refetchAt, failAt)
	}
}

// TestRefetchTriesBackupURLsFirst 盯「换备用地址比换流便宜」。
//
// 换流要调一次带 WBI 签名的接口；换备用地址只是一次普通请求。
// 能用前者解决就别用后者——B 站同一段字节通常配两三个 CDN 节点。
func TestRefetchTriesBackupURLsFirst(t *testing.T) {
	data := payload(8 << 10)
	var mainHits, backupHits, refetches int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/main":
			mainHits++
			http.Error(w, "过期了", http.StatusForbidden)
		case "/backup":
			backupHits++
			serveBytes(w, r, data, len(data))
		default:
			refetches++
			http.Error(w, "不该来这儿", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	s, err := NewStreamSource(StreamSourceOptions{
		URLs:    []string{srv.URL + "/main", srv.URL + "/backup"},
		Client:  srv.Client(),
		TempDir: t.TempDir(),
		Backoff: fastBackoff(),
		Refetch: func(context.Context, int64) *StreamReply {
			refetches++
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	if _, err := io.ReadAll(s); err != nil {
		t.Fatalf("备用地址应该能救回来：%v", err)
	}
	if mainHits != 1 || backupHits != 1 {
		t.Errorf("主地址被打了 %d 次、备用 %d 次，want 各 1 次", mainHits, backupHits)
	}
	if refetches != 0 {
		t.Errorf("备用地址能用的时候不该换流，却换了 %d 次", refetches)
	}
}

// TestRefetchRefusesAStreamOfAnotherLength 盯「换了另一条流就别硬接」。
//
// B 站换了流（比如会员过期降级）时新旧两条流长度不同。从旧偏移接着读
// 会读到错位的字节——解出来是噪声，而且**不在开头出错**，要播到那个偏移
// 才炸，比直接失败难查一百倍。所以这里必须拒绝，并把原因报给上层。
func TestRefetchRefusesAStreamOfAnotherLength(t *testing.T) {
	const total = 16 << 10
	data := payload(total)

	srv, gate := cutAfter(data, 1024, total, func(w http.ResponseWriter, r *http.Request) {
		other := payload(9 << 10)
		serveBytes(w, r, other, len(other))
	})
	t.Cleanup(srv.Close)

	s, err := NewStreamSource(StreamSourceOptions{
		URLs:    []string{srv.URL + "/v1"},
		Size:    total,
		Client:  srv.Client(),
		TempDir: t.TempDir(),
		Backoff: fastBackoff(),
		Refetch: func(context.Context, int64) *StreamReply {
			return &StreamReply{URLs: []string{srv.URL + "/v2"}, Size: 9 << 10}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	// 先把第一段读完再放行掐断，和上面同一条理由。
	if _, err := io.ReadFull(s, make([]byte, 1024)); err != nil {
		t.Fatal(err)
	}
	close(gate)

	_, err = io.ReadAll(s)
	if err == nil {
		t.Fatal("换了长度不同的流却被硬接上了")
	}
	if !errors.Is(err, ErrStreamChanged) {
		t.Errorf("err = %v，want ErrStreamChanged", err)
	}
}

// TestSourceRecoversWhenTheServerIgnoresRange 盯 200 vs 206。
//
// 服务器不认 Range 时回 **200 + 整个文件**，不是 206 + 从头开始的那段。
// 这时候要是接着往旧偏移后面拼，缓存里就是「前半截 + 整个文件」——
// 长度对得上、解出来是错的，而且只在歌播到一半时才炸。
// `PLAN.md` §下载 把这一条列成实测过的坑。
func TestSourceRecoversWhenTheServerIgnoresRange(t *testing.T) {
	const total = 20 << 10
	data := payload(total)

	// 第二次请求**装作没看见 Range**：回 200 + 整个文件。
	srv, gate := cutAfter(data, 8<<10, total, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(total))
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	})
	t.Cleanup(srv.Close)

	s, err := NewStreamSource(StreamSourceOptions{
		URLs:    []string{srv.URL},
		Client:  srv.Client(),
		TempDir: t.TempDir(),
		Backoff: fastBackoff(),
		Refetch: func(context.Context, int64) *StreamReply {
			return &StreamReply{URLs: []string{srv.URL}, Size: total}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	if _, err := io.ReadFull(s, make([]byte, 8<<10)); err != nil {
		t.Fatal(err)
	}
	close(gate)

	got, err := io.ReadAll(s)
	if err != nil {
		t.Fatalf("服务器不认 Range 时应该从头重下：%v", err)
	}
	if !bytes.Equal(got, data[8<<10:]) {
		t.Errorf("读到的不是后半截：长度 %d，want %d（多半是把整文件拼在了半截后面）",
			len(got), len(data)-8<<10)
	}
}

// TestAnExpiredURLSaysSoRatherThanTalkingAboutRanges 盯 403 要说成 403。
//
// 一个 403 落到「不支持续传」那段判断里的话，报出来的是
// 「在线流不支持续传（HTTP 403）」——那句话对用户毫无用处：真正发生的事
// 是「这条播放地址过期了」，而那正是换流该接手的场合。
func TestAnExpiredURLSaysSoRatherThanTalkingAboutRanges(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "地址失效了", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	s, err := NewStreamSource(StreamSourceOptions{
		URLs:    []string{srv.URL},
		Client:  srv.Client(),
		TempDir: t.TempDir(),
		Backoff: fastBackoff(),
		Refetch: func(context.Context, int64) *StreamReply { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	_, err = io.ReadAll(s)
	if err == nil {
		t.Fatal("403 也被当成了读完了")
	}
	if !strings.Contains(err.Error(), "失效") {
		t.Errorf("err = %v，没有说清是「地址失效了」", err)
	}
	if strings.Contains(err.Error(), "续传") {
		t.Errorf("403 被说成了续传的问题：%v", err)
	}
}

// TestTheStreamSaysWhyItStopped 盯「拉不动了」和「放完了」是两件事。
//
// 字节源正常读完是 io.EOF，中途拉不动是一个包着 ErrStreamDead 的错。
// 上层要能把这两者分开说给用户听——而**看解码器是看不出来的**：字节断了
// 之后解码器多半先报「这一帧的 CRC 对不上」，那是后果不是原因。
func TestTheStreamSaysWhyItStopped(t *testing.T) {
	data := payload(64 << 10)
	cut := len(data) / 2

	srv, gate := cutAfter(data, cut, len(data), func(w http.ResponseWriter, r *http.Request) {
		// 换流之后还是只发一半：这一次没有任何办法能拉完。
		serveSome(w, r, data, cut)
	})
	t.Cleanup(srv.Close)

	s, err := NewStreamSource(StreamSourceOptions{
		URLs:    []string{srv.URL},
		Client:  srv.Client(),
		TempDir: t.TempDir(),
		Backoff: fastBackoff(),
		Refetch: func(context.Context, int64) *StreamReply {
			return &StreamReply{URLs: []string{srv.URL + "/again"}}
		},
		MaxRefresh: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	if _, err := io.ReadFull(s, make([]byte, cut)); err != nil {
		t.Fatal(err)
	}
	close(gate)
	_, _ = io.ReadAll(s)

	err = s.Err()
	if err == nil {
		t.Fatal("拉不动了却什么都没说")
	}
	if errors.Is(err, io.EOF) {
		t.Errorf("中途拉不动被说成了「放完了」：%v", err)
	}
	if !errors.Is(err, ErrStreamDead) {
		t.Errorf("err = %v，没有包 ErrStreamDead", err)
	}
}

// TestAFinishedStreamIsNotADeadStream 盯反过来的那一半：放完了不是拉不动了。
func TestAFinishedStreamIsNotADeadStream(t *testing.T) {
	data := payload(32 << 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveBytes(w, r, data, len(data))
	}))
	t.Cleanup(srv.Close)

	s, err := NewStreamSource(StreamSourceOptions{
		URLs:    []string{srv.URL},
		Client:  srv.Client(),
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	if _, err := io.ReadAll(s); err != nil {
		t.Fatal(err)
	}
	if err := s.Err(); !errors.Is(err, io.EOF) {
		t.Errorf("放完了却报了 %v，want io.EOF", err)
	}
	if errors.Is(s.Err(), ErrStreamDead) {
		t.Error("放完了被说成了「拉不动了」")
	}
}

// TestRefetchRefusesAStreamOfAnotherLengthEvenWhenNobodySaidItsSize 盯
// 「换了另一条流就别硬接」这道防线**真的在跑**。
//
// 取流接口的返回里**没有长度**（`bili.StreamRef` 因此不提供 Size），
// 所以调用方填不出大小来。防线得靠两个响应各自报出来的 Content-Length
// 相比——否则它就是一条永远不触发的死代码，而「换到别的流还硬接」的
// 后果是解出噪声、而且只在歌播到那个偏移才炸。
func TestRefetchRefusesAStreamOfAnotherLengthEvenWhenNobodySaidItsSize(t *testing.T) {
	const total = 32 << 10
	data := payload(total)

	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			w.Header().Set("Content-Length", fmt.Sprint(total))
			w.Write(data[:1024])
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		}
		// 第二条流**短得多**——换了另一段字节。
		other := payload(9 << 10)
		serveBytes(w, r, other, len(other))
	}))
	t.Cleanup(srv.Close)

	s, err := NewStreamSource(StreamSourceOptions{
		URLs: []string{srv.URL + "/v1"},
		Size: total,
		// ⚠️ 不填 Size：这就是生产路径上真实的样子。
		Client: srv.Client(),
		Refetch: func(context.Context, int64) *StreamReply {
			return &StreamReply{URLs: []string{srv.URL + "/v2"}}
		},
		TempDir: t.TempDir(),
		Backoff: fastBackoff(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	if _, err := io.ReadFull(s, make([]byte, 1024)); err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(s)
	if !errors.Is(err, ErrStreamChanged) {
		t.Errorf("err = %v，want ErrStreamChanged（长度对不上还硬接了）", err)
	}
}

// serveSome 发 [from, until) 这一段，Content-Length 声明的是整段。
func serveSome(w http.ResponseWriter, r *http.Request, data []byte, until int) {
	from := rangeStart(r)
	w.Header().Set("Content-Length", fmt.Sprint(int64(len(data))-from))
	if from > 0 {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, len(data)-1, len(data)))
		w.WriteHeader(http.StatusPartialContent)
	}
	end := min(int(from)+until, len(data))
	w.Write(data[from:end])
	w.(http.Flusher).Flush()
	panic(http.ErrAbortHandler)
}

// TestTheLengthGuardSurvivesASecondRefresh 盯那道防线**第二次也拦得住**。
//
// 曾经的错：算长度的那一步一边算一边把新流的长度写进 `s.size`，于是
//
//	换流第一次 → 基准是旧流的 250400 → 新流自称 604754 → 对不上，拦住了
//	换流第二次 → 基准变成了**试过那条的** 604754 → 对上了 → 放行
//
// 于是 stereo16.flac 的前三段 + stereo24.flac 的后半段被接在一起，解出来
// 是噪声，而错误出现在歌播到那个偏移的时候——「拦住一次」根本不算拦住。
//
// 断言的是「换了**几次**流都被认出来」，而不是「最后一次是什么错」：
// 拦一次就放行的话，playCalls 只会是 1。
func TestTheLengthGuardSurvivesASecondRefresh(t *testing.T) {
	first := readTestdata(t, "flacdec/testdata/stereo16.flac")
	second := readTestdata(t, "flacdec/testdata/stereo24.flac")
	if len(first) == len(second) {
		t.Fatalf("两个素材一样长，这条用例就验不到东西了")
	}
	cut := len(first) / 3

	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			// 第一条流：发到 cut 就掐断。
			serveThenAbort(w, r, first, cut)
			return
		}
		// 换来的那条是**另一份文件**（长度、内容都不同）。
		serveBytes(w, r, second, len(second))
	}))
	t.Cleanup(srv.Close)

	var refetches atomic.Int32
	s, err := NewStreamSource(StreamSourceOptions{
		URLs:       []string{srv.URL + "/v1"},
		Size:       int64(len(first)),
		Client:     srv.Client(),
		TempDir:    t.TempDir(),
		Backoff:    fastBackoff(),
		MaxRefresh: 3,
		Refetch: func(context.Context, int64) *StreamReply {
			refetches.Add(1)
			return &StreamReply{URLs: []string{srv.URL + "/v2"}}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	// 先把断开之前那一段读完，和 `cutAfter` 一样的理由。
	if _, err := io.ReadFull(s, make([]byte, cut)); err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(s)
	if !errors.Is(err, ErrStreamChanged) {
		t.Fatalf("err = %v，want ErrStreamChanged", err)
	}
	if got := refetches.Load(); got < 2 {
		t.Errorf("只换流 %d 次就放行了——第二次的长度没跟第一次的基准比，"+
			"两段不同文件的字节被接在一起（基准值被试过的那条污染了）", got)
	}
}

// readTestdata 读一份测试素材（路径相对本包，flacdec 的素材就在子目录里）。
func readTestdata(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.FromSlash(rel))
	if err != nil {
		// 这里**不能 Skip**：跳过在输出里长得跟通过一样，而这条用例正是为了
		// 证明「第二次换流也拦得住」，悄悄跳过去等于什么都没验。
		t.Fatalf("找不到素材 %s：%v", rel, err)
	}
	return b
}

// serveThenAbort 发 body 的开头若干字节，然后**掐断连接**。
//
// Content-Length 声明的是整条流，所以客户端那边读到的是 unexpected EOF
// 而不是干净的 io.EOF——那才是「下到一半断了」的真实形态。
func serveThenAbort(w http.ResponseWriter, r *http.Request, body []byte, until int) {
	from := rangeStart(r)
	w.Header().Set("Content-Length", fmt.Sprint(int64(len(body))-from))
	if from > 0 {
		w.Header().Set("Content-Range",
			fmt.Sprintf("bytes %d-%d/%d", from, len(body)-1, len(body)))
		w.WriteHeader(http.StatusPartialContent)
	}
	w.Write(body[from:min(int(from)+until, len(body))])
	w.(http.Flusher).Flush()
	panic(http.ErrAbortHandler)
}

// TestCloseUnblocksAReaderThatIsWaiting 盯「阻塞式读取必须可取消」。
//
// MyGo 的解码 goroutine 就阻塞在 `Read` 上，而它的 `close()`（`audio/source.go`
// 的 `stream.close`）**只置自己的标记并 Signal 自己的 cond，不会解除外部
// reader 的等待**。源自己不叫醒自己的话，快速切歌会留下一堆解码 goroutine。
func TestCloseUnblocksAReaderThatIsWaiting(t *testing.T) {
	hold := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000000")
		w.Write([]byte("fLaC"))
		w.(http.Flusher).Flush()
		<-hold
	}))
	t.Cleanup(func() { close(hold); srv.Close() })

	s, err := NewStreamSource(StreamSourceOptions{
		URLs:    []string{srv.URL},
		Client:  srv.Client(),
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// 先把能读的 4 个字节吃掉，剩下的一律要等。
	buf := make([]byte, 4)
	if _, err := io.ReadFull(s, buf); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := s.Read(make([]byte, 4096))
		done <- err
	}()

	// 给它一点时间真的卡进去。
	time.Sleep(100 * time.Millisecond)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case err := <-done:
		if !errors.Is(err, ErrSourceClosed) {
			t.Errorf("等着的人被叫醒后拿到 %v，want ErrSourceClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close 之后还有人在 Read 上死等")
	}
}

// TestCloseRemovesTheCacheFileAndJoinsEveryGoroutine 盯「切歌不遗留」。
//
// 三个都要：等着的人被叫醒、后台 goroutine 都退了、缓存文件删了。
// 少一样都会在「连点 5 首」或者「退出应用」之后显形。
func TestCloseRemovesTheCacheFileAndJoinsEveryGoroutine(t *testing.T) {
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
	before := goroutines()

	if _, err := s.Read(make([]byte, 64)); err != nil && !errors.Is(err, ErrSourceClosed) {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close 第二次（应该可以重复调用）：%v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		var details []string
		for _, name := range names {
			path := filepath.Join(dir, name)
			_, statErr := os.Stat(path)
			removeErr := os.Remove(path)
			details = append(details, fmt.Sprintf("%s stat=%v remove=%v", name, statErr, removeErr))
		}
		t.Errorf("缓存文件留下了：%v（Close 后再次检查：%s）", names, strings.Join(details, "; "))
	}
	if after := goroutines(); after > before+2 {
		t.Errorf("关掉之后 goroutine 数从 %d 涨到 %d，有下载/看门狗没退出", before, after)
	}
}

// TestWatchdogCutsADownloadThatStopsMakingProgress 盯「卡死」不是「慢」。
//
// 连接断了之后既不报错也不再有数据（NAT 超时、代理静默丢弃），表现是
// 整首歌停在那儿不动、界面上什么都不显示——最难查的一种「不响」。
func TestWatchdogCutsADownloadThatStopsMakingProgress(t *testing.T) {
	// 卡住不往下发：连接还活着，只是永远没有新字节。
	hold := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(1<<20))
		w.Write([]byte("x"))
		w.(http.Flusher).Flush()
		<-hold
	}))
	// 收尾的顺序是「先关源、再放行 handler、最后关服务器」。
	// t.Cleanup 是后进先出，所以注册顺序要反过来写：服务器的 Close 会
	// 等所有 handler 返回，而 handler 在 hold 上等着——顺序写反了
	// 测试会在收尾处死等。
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(hold) })

	s, err := NewStreamSource(StreamSourceOptions{
		URLs:         []string{srv.URL},
		Client:       srv.Client(),
		TempDir:      t.TempDir(),
		Backoff:      fastBackoff(),
		StallTimeout: 300 * time.Millisecond,
		MaxRefresh:   1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	// 看门狗掐断之后，源会走换流那条路；没配 Refetch 就该报终态错误，
	// 而不是永远挂着。
	errCh := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(s)
		errCh <- err
	}()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("读完了？不可能，服务器只给了一个字节")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("下载卡死了也没有人管")
	}
}

// TestSeekCanGoBackwards 盯拖进度条。
//
// 缓存里已经有的部分随便读——拖回去、拖到中间、再拖回来都得是原样。
func TestSeekCanGoBackwards(t *testing.T) {
	data := payload(48 << 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveBytes(w, r, data, len(data))
	}))
	t.Cleanup(srv.Close)

	s, err := NewStreamSource(StreamSourceOptions{
		URLs:    []string{srv.URL},
		Size:    int64(len(data)),
		Client:  srv.Client(),
		TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	if _, err := io.ReadFull(s, make([]byte, len(data))); err != nil {
		t.Fatal(err)
	}
	for _, off := range []int64{0, 12345, 7, 40000, int64(len(data)) - 4} {
		if _, err := s.Seek(off, io.SeekStart); err != nil {
			t.Fatalf("Seek(%d)：%v", off, err)
		}
		buf := make([]byte, 4)
		if _, err := io.ReadFull(s, buf); err != nil {
			t.Fatalf("在 %d 读：%v", off, err)
		}
		if !bytes.Equal(buf, data[off:off+4]) {
			t.Errorf("在 %d 读到 % x，want % x", off, buf, data[off:off+4])
		}
	}
}

// TestRefetchReportsTheNewQuality 盯「换流后音质徽章跟着更新」。
//
// 换流之后接口可能给的是另一档（会员过期降级）。界面上那个徽章要跟着
// 变：降级了用户得看见，不然他以为自己还在听无损。
func TestRefetchReportsTheNewQuality(t *testing.T) {
	const total = 64 << 10
	data := payload(total)

	srv, gate := cutAfter(data, 2048, total, func(w http.ResponseWriter, r *http.Request) {
		serveBytes(w, r, data, total)
	})
	t.Cleanup(srv.Close)

	var got string
	s, err := NewStreamSource(StreamSourceOptions{
		URLs:    []string{srv.URL},
		Size:    total,
		Client:  srv.Client(),
		TempDir: t.TempDir(),
		Backoff: fastBackoff(),
		Refetch: func(context.Context, int64) *StreamReply {
			return &StreamReply{URLs: []string{srv.URL}, Size: total, Quality: "128K"}
		},
		OnRefresh: func(r *StreamReply) { got = r.Quality },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	if _, err := io.ReadFull(s, make([]byte, 2048)); err != nil {
		t.Fatal(err)
	}
	close(gate)

	if _, err := io.ReadAll(s); err != nil {
		t.Fatal(err)
	}
	if got != "128K" {
		t.Errorf("换流后的音质 = %q，want 128K", got)
	}
	if q := s.Quality(); q != "128K" {
		t.Errorf("源上记的音质 = %q，want 128K", q)
	}
}

// TestNewStreamSourceRejectsWhatItCannotStart 盯构造期错误。
//
// 这两个是「一定要报给用户」的实打实的错误，不是内部状态。
func TestNewStreamSourceRejectsWhatItCannotStart(t *testing.T) {
	if _, err := NewStreamSource(StreamSourceOptions{URLs: []string{"http://x/y"}}); err == nil {
		t.Error("没有 http.Client 也建得起来")
	}
	if _, err := NewStreamSource(StreamSourceOptions{Client: http.DefaultClient}); err == nil {
		t.Error("没有地址也建得起来")
	}
	if _, err := NewStreamSource(StreamSourceOptions{
		URLs:    []string{"http://x/y"},
		Client:  http.DefaultClient,
		TempDir: filepath.Join(t.TempDir(), "没有这个目录"),
	}); err == nil {
		t.Error("缓存文件建不出来也不报错")
	}
}

func goroutines() int { return runtime.NumGoroutine() }
