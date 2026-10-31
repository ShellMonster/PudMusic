// 「并行发一条 Range 请求把文件尾部取回来」那一条链路。
//
// # 它解决的是什么
//
// B 站形状的完整单文件 `.m4a`（音乐库 sid 那一路）和 DASH 音频文件都是
// `ftyp` / `free` / `mdat` / **`moov`（在末尾）** 这个形状。而
// AudioToolbox 探 MP4 容器时要**扫过所有顶层盒子**才找得到 `moov`——
// `moov` 在末尾时它得扫到文件末尾。
//
// ⚠️ **实测：它不是从头读到尾的，它是「跳」过去的**（2026-10-25，
// 在 `internal/service/audio/extaf` 上用一个会记位置的字节源量出来的）：
// 它读 0 / 8 / 16 / 28 / 36 这几个位置（`ftyp` 头、`free` 头、`mdat` 头），
// **然后直接跳到 242156**——`moov` 的起点——把 `moov` 读出来。
//
// 也就是说：**只要末尾那一小段在手上，AudioToolbox 立刻就能开口**。
// 而在那之前（票 #220 的余账），源只有**顺序下载的前沿**能用，
// 于是「跳过去」的那一步会**等整个文件下完**——200 KB/s 的网上 1.22 秒，
// 50 KB/s 的网上 5.02 秒。
//
// # 这一层做什么
//
// 1. 等到文件头下到了、总长知道了（`mp4TopLevel` 走一遍顶层盒子）；
// 2. 如果 `moov` 在文件末尾，就**并行**发一条
//    `Range: bytes=<moov 起点>-`，把末尾那一小段单独取回来；
// 3. 把它写进缓存文件的**对应偏移**上（文件于是变成「前面一段 + 一个洞 +
//    末尾一段」），并让 `availAt` 认它。
//
// # 三条纪律
//
//   - **它只是一个加速**。取不到（服务器不认 Range / 地址过期 / 网络断了）
//     就**什么都不做**——顺序下载那条路照样把整个文件下完，一切照旧。
//     它的失败**不许**变成源的失败。
//   - **它不许动错误语义**。这一条请求不经过 `pour`，所以它不碰
//     `expectTotal` / `checkTotal` / `commitTotal` 那一套对账——
//     那套是给「续传 / 换流」用的，而尾部这一段是**同一段字节**，
//     不是另一条流。
//   - **它必须可取消**。它在 `s.wg` 里，`Close` 会等它退出；而它的
//     请求上下文挂在 `s.ctx` 上，`Close` 一取消它就断。

package audio

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// tailFetchTimeout 是「尾部这一条请求」自己的上限。
//
// ⚠️ **它和主下载的卡死判定是两回事**：主下载有 20 秒的看门狗
// （`defaultStallTimeout`），而这一条是**可有可无的加速**——它卡住了
// 不该拖累任何东西，所以给它一个自己的、比较短的截止时间。
//
// 取 10 秒：它要走的路和主下载一样远（一次 CDN 往返），而它拿到的是
// 末尾那一小段（几 KB），正常是毫秒级。
const tailFetchTimeout = 10 * time.Second

// tailFetch 是「并行取回文件尾部」那一条 goroutine。
//
// 它**从不**让源失败：任何一步不行就安静地收工。
func (s *StreamSource) tailFetch() {
	defer s.wg.Done()

	start, ok := s.tailTarget()
	if !ok {
		return
	}
	s.fetchTail(start)
}

// tailTarget 等到「能算出尾部起点」为止，返回那个起点。
//
// 返回 false 表示「不用取尾部」（不是那个形状 / `moov` 在前面 /
// 源已经关了 / 下载已经废了）。
func (s *StreamSource) tailTarget() (int64, bool) {
	for {
		s.mu.Lock()
		// 等「文件头下到了」。
		//
		// ⚠️ **总长不是必须等的**：末尾那一段的起点是从 `mdat` 盒的头
		// 算出来的（`mdat` 自己的长度就写在它的头里），而总长只用来
		// 判「`moov` 是不是真的在 `mdat` 后面」——那一条判错的后果只是
		// 多发一条请求（服务器会对越界的 Range 回 416，而那一路是安全的）。
		//
		// ⚠️ **文件头是必须等的**：`mp4TopLevel` 要读盒子的头，
		// 而头还没下到的时候读到的是**零**——那会被当成「一个长度乱七
		// 八糟的盒子」，得出一个错的结论。
		for !s.closed && s.err == nil && s.filled < mp4HeadMin {
			s.cond.Wait()
		}
		if s.closed || s.err != nil {
			s.mu.Unlock()
			return 0, false
		}
		size := s.size
		s.mu.Unlock()

		moov, tail, err := mp4TopLevel(s.headReader(), size)
		switch {
		case err == nil && moov >= 0:
			// `moov` 在前面（faststart）：不用取尾部。
			return 0, false
		case err == nil && tail >= 0:
			return tail, true
		case errors.Is(err, errMP4NotOurShape):
			return 0, false
		default:
			// 🔴 **「文件头还没下到」要**阻塞**等，不能空转重试**。
			//
			// 空转（`continue` 回去再走一遍）会把**一个 CPU 核占满**，
			// 直到顺序下载追上那个偏移——而那段时间里源唯一在做的事就是
			// 「等字节」（2026-10-25 的评审实测：墙钟 6.1 秒里 CPU 占了
			// 5.8 秒，≈0.95 个核）。
			//
			// 触发条件是**盒子头落在前 512 字节之外**——一个大的
			// `free`/`sidx` 盒子排在 `mdat`/`moov` 前面时，`mdat` 的头
			// 在好几兆之外。
			need, ok := needOffset(err)
			if !ok {
				return 0, false
			}
			s.mu.Lock()
			for !s.closed && s.err == nil && s.filled < need {
				s.cond.Wait()
			}
			s.mu.Unlock()
			continue
		}
	}
}

// mp4HeadMin 是「至少要下到多少字节才走顶层盒子」。
//
// 一个正常的 MP4 的头几个盒子加起来不超过一百来字节（`ftyp` 32 +
// `free` 8 + `mdat` 头 8/16）。给 512 是个宽裕的余量，而它小到
// 「第一块数据一到就够」——顺序下载的第一块是 64 KB。
const mp4HeadMin = 512

// headReader 把缓存文件包成「只读已经下到的那部分」的读者。
//
// ⚠️ **读不到还没下的字节时必须报 `headNotReady`，不能报 io.EOF**：
// io.EOF 在 `mp4TopLevel` 那一层是「这个文件不用取尾部」，
// 而「还没下到」是「再等等」——混起来会把这一票的机会关掉。
//
// ⚠️ **它带着「要等第几个字节」**：调用方据此**阻塞**等顺序下载追上，
// 而不是空转重试（空转会把一个 CPU 核占满）。
type headReader struct {
	s *StreamSource
}

// headReader 造一个「只读已经下到的那部分」的读者。
func (s *StreamSource) headReader() headReader { return headReader{s: s} }

func (h headReader) ReadAt(p []byte, off int64) (int, error) {
	h.s.mu.Lock()
	filled := h.s.filled
	h.s.mu.Unlock()
	if off+int64(len(p)) > filled {
		return 0, headNotReady{need: off + int64(len(p))}
	}
	return h.s.file.ReadAt(p, off)
}

// fetchTail 发一条 Range 请求把 [start, 文件末尾) 取回来，写进缓存文件。
//
// ⚠️ **它不许失败整条源**：任何一步出错都只是「没有这一段加速」，
// 而顺序下载那条路照样把整个文件下完。
func (s *StreamSource) fetchTail(start int64) {
	ctx, cancel := context.WithTimeout(s.ctx, tailFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.currentURL(), nil)
	if err != nil {
		return
	}
	req.Header.Set("Range", "bytes="+strconv.FormatInt(start, 10)+"-")

	resp, err := s.client.Do(req)
	if err != nil {
		// 地址过期 / 网络断了 —— 什么都不做，顺序下载会把它补上。
		return
	}
	defer resp.Body.Close()

	// ⚠️ **服务器不认 Range 时它回 200 而不是 206**，而 body 是**整个文件**。
	// 这时候要是照着 start 写下去，缓存里就是错的字节——而且**不报错**，
	// 只在歌播到那一段时才炸（极难查）。所以这里只认 206。
	if resp.StatusCode != http.StatusPartialContent {
		return
	}
	// 校一下服务器真的从我们要的那个偏移开始给。
	// ⚠️ **不许只信请求头**：`Content-Range` 才是服务器说的实话。
	got, ok := contentRangeStart(resp.Header.Get("Content-Range"))
	if !ok || got != start {
		return
	}

	s.writeMu.Lock()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		s.writeMu.Unlock()
		return
	}
	// 记下尾部这一段从哪儿开始（`tailGot` 还是 0，所以**还没有任何字节
	// 可用**——`availAt` 认的是「已经落盘的那部分」）。
	if s.tailStart < 0 {
		s.tailStart = start
	}
	s.mu.Unlock()
	s.writeMu.Unlock()

	buf := make([]byte, copyBufSize)
	off := start
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			s.writeMu.Lock()
			s.mu.Lock()
			valid := !s.closed && s.tailStart == start
			s.mu.Unlock()
			if !valid {
				s.writeMu.Unlock()
				return
			}
			if _, werr := s.file.WriteAt(buf[:n], off); werr != nil {
				s.writeMu.Unlock()
				return
			}
			gaugeDelta := s.recordWrite(off, int64(n))
			if gauge := s.gauge; gauge != nil && gaugeDelta > 0 {
				gauge.AddMediaTempBytes(gaugeDelta)
			}
			off += int64(n)

			// 🔴 **先落盘、再记账**：顺序是反的话，读者会拿到一段
			// 「说是有了、其实还是零」的字节——那是解不出来的噪声。
			s.mu.Lock()
			s.tailGot = off - start
			s.cond.Broadcast()
			s.mu.Unlock()
			s.writeMu.Unlock()
		}

		if rerr != nil {
			return
		}

	}
}

// contentRangeStart 从 `Content-Range: bytes 100-199/1000` 里取出起点。
//
// 解析不出来就返回 false——**宁可不写，也不要写错的位置**。
func contentRangeStart(v string) (int64, bool) {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "bytes ") {
		return 0, false
	}
	v = strings.TrimPrefix(v, "bytes ")
	// 形如 `100-199/1000` 或 `100-199/*`。
	dash := strings.IndexByte(v, '-')
	if dash <= 0 {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(v[:dash]), 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// tailBytes 报「尾部那一段已经取回来多少字节」（只作记录用）。
func (s *StreamSource) tailBytes() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tailGot
}

// tailOffset 报「尾部那一段从哪个偏移开始」（-1 = 没在取尾部）。
func (s *StreamSource) tailOffset() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tailStart
}

// restartTail 把「尾部那一段」的记账清掉。
//
// ⚠️ **服务器不认续传时会 `restart()`（把缓存文件截成 0 重下）**——
// 而截断会把已经取回来的尾部那一段**一起抹掉**。不记账的话，
// `availAt` 会说「那一段有」，而盘上已经没有了：读出来是零，
// 解出来是噪声，而且**不报错**。
func (s *StreamSource) restartTail() {
	s.mu.Lock()
	s.tailStart = -1
	s.tailGot = 0
	s.mu.Unlock()
}
