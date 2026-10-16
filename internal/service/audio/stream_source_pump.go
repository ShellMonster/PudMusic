// 边下边播字节源的下载那一半：泵（谁在下载）、看门狗（下载卡死了怎么办）、
// 换流（这条地址读不动了怎么办）。
//
// 跟 `stream_source.go` 分开是因为那一半讲的是**接口**（能读、能 seek、能
// 关、能取消），这一半讲的是**网络**（会 403、会卡死、会中途断）。两者的
// 契约只在 `recover()` 那一个函数上交叉。

package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"time"
)

// pump 是下载 goroutine。
//
// 它是整个在线播放里唯一碰网络的地方，从头跑到尾只在三种情况下停：
// 下载完了（记 io.EOF）、换流也换不动了（记真错）、源被关了（直接返回，
// 不记错——那样 `Close` 之后的 `Read` 会报 `ErrSourceClosed`，
// 和「切歌」而不是「出错」对得上）。
func (s *StreamSource) pump() {
	defer s.wg.Done()
	buf := make([]byte, copyBufSize)

	for {
		if s.isClosed() {
			return
		}
		from := s.Filled()
		resp, done, err := s.get(from)
		if err != nil {
			done()
			if s.stopped() {
				return
			}
			// ⚠️ 传 `s.Filled()` 而不是上面那个 `from`：这一轮下载过程中
			// 已经补下了一段，用旧的 from 会让换流从一个错的位置接上——
			// 内容能对上（因为是同一段字节），但「从第几个字节接的」
			// 这个断言会变成一个没有意义的 0。
			if !s.recover(s.Filled(), err) {
				return
			}
			continue
		}

		err = s.pour(resp, from, buf)
		resp.Body.Close()
		// ⚠️ 一定要在**读完 body 之后**才取消这次请求。提前取消的话
		// 响应头拿到了、正文一个字节都读不到，而那正是「上不了声」
		// 而界面上什么都不显示的形态。
		done()
		if err == nil {
			// 正常读完。
			s.markTotal(s.Filled())
			s.fail(io.EOF)
			return
		}
		if s.stopped() {
			return
		}
		if !s.recover(s.Filled(), err) {
			return
		}
	}
}

// pour 把一个响应体写进缓存。返回 nil 表示这条流读完了。
func (s *StreamSource) pour(resp *http.Response, from int64, buf []byte) error {
	// 先看状态码。**顺序要紧**：下面那段 Range 的判断要靠状态码来认
	// 「服务器不认续传」，而一个 403 落到那段里会被说成「不支持续传」——
	// 那句话对用户没有用，403 的真正意思是「这条地址过期了」。
	if resp.StatusCode >= 400 {
		return statusErr(resp)
	}

	// ⚠️ **服务器不认 Range 时它会回 200 而不是 206**，而且 body 是**整个文件**。
	// 这时候要是接着往 from 后面写，缓存里就是「前半截 + 整个文件」，
	// 长度对得上、解出来是错的——而且只在歌播到一半时才炸，极难查。
	// 所以这里先把它认出来：直接从 0 重写。
	if from > 0 && resp.StatusCode == http.StatusOK {
		if err := s.restart(); err != nil {
			return err
		}
		from = 0
	} else if from > 0 && resp.StatusCode != http.StatusPartialContent {
		// 既不是 206 也不是 200：206 之外的其它 2xx（B 站偶尔给 204）
		// 一样对不上偏移。
		return fmt.Errorf("audio: 在线流不支持续传（HTTP %d）", resp.StatusCode)
	}
	s.learnTotal(from, resp)

	for {
		if s.stopped() {
			return nil
		}
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if err := s.append(buf[:n]); err != nil {
				return err
			}
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				// 读完了吗？总长知道的话比一下，不知道就当读完了——
				// B 站的 CDN 不给 Content-Length 时只有这一条路。
				size := s.Size()
				if size < 0 || s.Filled() >= size {
					return nil
				}
				// 没到总长：连接在到头之前断了，换流接着下。
				return fmt.Errorf("audio: 在线流下到 %d 字节就断了（还差 %d）", s.Filled(), size-s.Filled())
			}
			return fmt.Errorf("audio: 读在线流：%w", rerr)
		}
		if n == 0 {
			return errors.New("audio: 在线流读出了 0 字节")
		}
	}
}

// append 把一段字节写进缓存，并叫醒等着这些字节的人。
func (s *StreamSource) append(b []byte) error {
	s.mu.Lock()
	off := s.filled
	s.mu.Unlock()

	if _, err := s.file.WriteAt(b, off); err != nil {
		// 磁盘满了是这里最常见的错，而且**必须当成播放失败报出去**：
		// 装在内存里假装成功的话，歌会播到缓存写不下的地方就停住，
		// 界面上表现成「播到一半莫名停住」，跟磁盘满毫无关系。
		return fmt.Errorf("audio: 在线流的缓存写不下去（磁盘满了？）：%w", err)
	}

	s.mu.Lock()
	s.filled = off + int64(len(b))
	if s.size >= 0 && s.filled > s.size {
		s.size = s.filled
	}
	s.cond.Broadcast()
	s.mu.Unlock()
	return nil
}

// get 发一次取字节的请求。
//
// ⚠️ 这一层**不设** `http.Client.Timeout`：那会把一首歌的整个下载算进
// 一个固定上限里，五分钟的歌在慢一点的网络上必然超时。真正的超时是
// 传输层的拨号/握手/响应头那几个（见 `ui.newCredentialTransport`），
// 再加上这里外层的卡死看门狗。
func (s *StreamSource) get(from int64) (*http.Response, func(), error) {
	ctx, cancel := context.WithCancel(s.ctx)
	s.setRequest(cancel)
	done := func() {
		s.clearRequest(cancel)
		cancel()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.currentURL(), nil)
	if err != nil {
		done()
		return nil, func() {}, err
	}
	if from > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(from, 10)+"-")
	}

	resp, err := s.client.Do(req)
	if err != nil {
		done()
		if s.stopped() {
			return nil, func() {}, errSourceStopping
		}
		return nil, func() {}, fmt.Errorf("audio: 连不上在线流：%w", err)
	}
	return resp, done, nil
}

// errSourceStopping 是「源正在关，别把它当成网络故障去换流」。
var errSourceStopping = errors.New("audio: 字节源正在关闭")

// recover 处理「这条地址读不动了」。
//
// 顺序是：**先换备用地址，再换流**。换备用地址只是一次不带签名的普通
// 请求，换流要调一次带 WBI 签名的接口——后者更贵，能用前者解决就别用。
//
// 返回 false 表示「实在没辙了」。**这时候终态错误由 recover 自己记**，
// 调用方直接返回就行：不同的放弃原因要报不同的话（「换到别的流了」和
// 「这条流下到一半断了」对用户是两件事），把 cause 原样报出去等于把
// 这个判断又推回给调用方。
func (s *StreamSource) recover(from int64, cause error) bool {
	s.mu.Lock()
	if s.refreshes >= s.maxRefr {
		s.mu.Unlock()
		s.fail(fmt.Errorf("换了 %d 次流还是下不动：%w", s.maxRefr, cause))
		return false
	}
	s.refreshes++

	// 换备用地址：还有没用过的就换一个，同一条流换个 CDN 节点。
	if s.urlIdx+1 < len(s.urls) {
		s.urlIdx++
		s.mu.Unlock()
		return true
	}

	refetch := s.refetch
	s.mu.Unlock()
	if refetch == nil {
		s.fail(cause)
		return false
	}
	reply := refetch(s.ctx, from)
	if reply == nil || len(reply.URLs) == 0 {
		s.fail(fmt.Errorf("换流没换到新地址：%w", cause))
		return false
	}
	if !s.adopt(reply) {
		s.fail(fmt.Errorf("%w（原来那条流的问题是：%v）", ErrStreamChanged, cause))
		return false
	}
	return true
}

// adopt 换上一条新流。
//
// ⚠️ 关键的一步是**长度比对**：B 站对同一首歌、同一个音质返回的是同一段
// 字节，所以能从原偏移接上；如果新流的长度（或容器）对不上，说明 B 站换了
// 东西——这时候从旧偏移读下去会读到错位数据，解出来是噪声。
// 那就老实报 `ErrStreamChanged`，让上层重新打开解码器并跳回**播放位置**
// （那是时间，不是字节）。
func (s *StreamSource) adopt(reply *StreamReply) bool {
	s.mu.Lock()
	oldSize := s.size
	s.mu.Unlock()

	if reply.Size > 0 && oldSize > 0 && reply.Size != oldSize {
		return false
	}
	if !s.sameContainer(reply.URLs) {
		return false
	}

	s.mu.Lock()
	s.urls = append([]string(nil), reply.URLs...)
	s.urlIdx = 0
	if reply.Size > 0 {
		s.size = reply.Size
	}
	s.expiresAt = reply.ExpiresAt
	s.quality = reply.Quality
	onRefresh := s.onRefr
	s.cond.Broadcast()
	s.mu.Unlock()

	if onRefresh != nil {
		onRefresh(reply)
	}
	return true
}

// sameContainer 拿新旧两条地址的头一个后缀比一下容器。
//
// 比长度比不出「同长度但换了编码」的情况（虽然极罕见），所以再看一眼
// 后缀。判据松一点没关系——猜错了的后果是**重新打开解码器**，
// 那只是多花一点时间；猜对了省下的也是那点时间。
func (s *StreamSource) sameContainer(urls []string) bool {
	if len(s.urls) == 0 || len(urls) == 0 {
		return true
	}
	return streamExt(s.urls) == streamExt(urls)
}

// restart 把缓存清空、下载从头再来。服务器不认 Range 时用。
//
// ⚠️ **游标不动**。缓存从头重下，但正在读的人还在原来那个位置等着：
// 解码器的读游标往前挪就等于把这首歌倒回开头——那正是「换流后从原位置
// 继续」要避免的事。清缓存是为了把字节补齐，不是为了把进度倒回去。
func (s *StreamSource) restart() error {
	if err := s.file.Truncate(0); err != nil {
		return fmt.Errorf("audio: 清空在线流的缓存：%w", err)
	}
	s.mu.Lock()
	s.filled = 0
	s.cond.Broadcast()
	s.mu.Unlock()
	return nil
}

// watchdog 是卡死看门狗。
//
// 没有它，一个「既不报错也不再送数据」的连接会让整首歌停在那儿不动，
// 界面上什么都不显示——那是最难查的一种「不响」。
//
// 它只做一件事：连续 stallCheckInterval 没有新字节就把当前这次请求掐了。
// 掐了之后泵会走 `recover` 那条路：换备用地址，换完了还没有进展就换流，
// 换到 maxRefr 次为止。
func (s *StreamSource) watchdog() {
	defer s.wg.Done()

	interval := s.stall / 4
	if interval < 250*time.Millisecond {
		interval = 250 * time.Millisecond
	}
	if interval > 2*time.Second {
		interval = 2 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()

	last := s.Filled()
	stalls := 0
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			if s.stopped() {
				return
			}
			if n := s.Filled(); n != last {
				last, stalls = n, 0
				continue
			}
			// 没进展。再看总长：下满了就是完了，不是卡死。
			if size := s.Size(); size >= 0 && last >= size {
				continue
			}
			if !s.requestInFlight() {
				continue
			}
			stalls++
			if time.Duration(stalls)*interval >= s.stall {
				s.cancelRequest()
				// 重数一遍：掐断请求之后泵会重连，可能又在推数据了。
				stalls = 0
				last = s.Filled()
			}
		}
	}
}

func (s *StreamSource) setRequest(cancel context.CancelFunc) {
	s.reqMu.Lock()
	s.reqCancel = cancel
	s.reqMu.Unlock()
}

// clearRequest 只清掉**自己那一次**的取消句柄。
//
// 看门狗掐断上一次请求之后泵会重连；这时新请求已经占了坑，
// 上一次那个 cancel 再清就会把新请求的句柄抹掉，于是这一次永远
// 掐不断——表现是「第一次卡死能救，第二次就救不了了」。
func (s *StreamSource) clearRequest(cancel context.CancelFunc) {
	s.reqMu.Lock()
	if sameCancel(s.reqCancel, cancel) {
		s.reqCancel = nil
	}
	s.reqMu.Unlock()
}

func (s *StreamSource) cancelRequest() {
	s.reqMu.Lock()
	c := s.reqCancel
	s.reqCancel = nil
	s.reqMu.Unlock()
	if c != nil {
		c()
	}
}

// sameCancel 比两个取消函数是不是同一个。
func sameCancel(a, b context.CancelFunc) bool {
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}

func (s *StreamSource) requestInFlight() bool {
	s.reqMu.Lock()
	defer s.reqMu.Unlock()
	return s.reqCancel != nil
}

// ---- 小工具 ----

func (s *StreamSource) currentURL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.urls[min(s.urlIdx, len(s.urls)-1)]
}

func (s *StreamSource) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *StreamSource) stopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed || s.ctx.Err() != nil
}

// markTotal 在读完之后把总长定下来。
func (s *StreamSource) markTotal(n int64) {
	s.mu.Lock()
	if s.size < 0 {
		s.size = n
	}
	s.mu.Unlock()
}

// learnTotal 从响应头里学总长。
//
// 有 Range 时 Content-Length 是**剩下的那一段**，所以要加上已经下的。
// 写成「直接用 Content-Length」的话，一首续传的歌会被当成只有尾巴那么长，
// 表现是提前「播完了」。
func (s *StreamSource) learnTotal(from int64, resp *http.Response) {
	n := resp.ContentLength
	if n < 0 {
		if v := resp.Header.Get("Content-Length"); v != "" {
			if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
				n = parsed
			}
		}
	}
	if n < 0 {
		return
	}
	s.mu.Lock()
	total := from + n
	// 只在「还不知道」或者「服务器说得更长」时更新。已经下完的那条
	// 不该被一个续传响应改回去。
	if s.size < 0 || total > s.size {
		s.size = total
	}
	s.cond.Broadcast()
	s.mu.Unlock()
}

// statusErr 把一个错误的状态码说成人话。
func statusErr(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusForbidden, http.StatusUnauthorized:
		// B 站的播放地址过期之后就是这个。不区分 401/403：对我们来说
		// 都是「这条地址不管用了，换一条」。
		return fmt.Errorf("audio: 播放地址已经失效（HTTP %d）", resp.StatusCode)
	case http.StatusNotFound:
		return errors.New("audio: 播放地址在 CDN 上已经不存在了")
	case http.StatusTooManyRequests:
		return errors.New("audio: CDN 说我们请求太快了")
	}
	return fmt.Errorf("audio: 在线流返回 HTTP %d", resp.StatusCode)
}
