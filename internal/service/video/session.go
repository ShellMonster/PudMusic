package video

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/ShellMonster/PudMusic/internal/perf"
)

// Config 是一路视频要的外部依赖。
type Config struct {
	// Fetch 取流上 [from, to) 这段字节。地址失效、换 CDN 都由它自己处理。
	Fetch func(ctx context.Context, from, to int64) ([]byte, error)
	// Open 解码一个本地 fMP4 文件；空则用平台默认。像素缓冲一律从这个 pool 借还。
	Open func(path string, pool BufferPool) (*Source, error)
	// TempDir 放分片临时文件；空则用系统临时目录。
	TempDir string
	// FramePoolBytes 是帧池的总字节预算；0 用 defaultFramePoolBytes。
	// 给测试留一个口子，生产路径不传。
	FramePoolBytes int64
	// Gauges 是帧池记账写入的 gauge 集合；空则写进程级的那一份（#668 快照读它）。
	Gauges *perf.Gauges
}

const (
	headProbe = 64 << 10
	headMax   = 4 << 20
	// 追不上这么久就当成用户拖了进度，重开。
	resyncAhead = 3 * time.Second
	resyncBack  = 300 * time.Millisecond
	resyncGap   = 1500 * time.Millisecond
	// 一帧早到这么多以内就算到点。
	frameSlack = 20 * time.Millisecond
	outBuffer  = 8
)

// Session 是一路正在播放的视频：按需取分片、解码、按给定时钟交帧。
// Poll/Seek/Close 只在一个 goroutine（界面线程）里调。
type Session struct {
	cfg  Config
	pool *framePool

	mu      sync.Mutex
	frameMu sync.Mutex
	idx     *Index
	err     error
	cancel  context.CancelFunc
	out     chan Frame
	done    chan struct{}

	pend      *Frame
	shown     Frame
	haveShow  bool
	head      time.Duration
	lastSeek  time.Time
	prevPix   []byte
	tempPath  string
	closeOnce sync.Once
}

// Start 从 at 开始取流解码。
func Start(cfg Config, at time.Duration) *Session {
	if cfg.Open == nil {
		cfg.Open = Open
	}
	s := &Session{cfg: cfg, pool: newFramePool(cfg.FramePoolBytes, cfg.Gauges)}
	s.begin(at)
	return s
}

func (s *Session) begin(at time.Duration) {
	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan Frame, outBuffer)
	done := make(chan struct{})
	s.mu.Lock()
	s.cancel, s.out, s.done = cancel, out, done
	s.mu.Unlock()
	s.pend, s.haveShow, s.head, s.lastSeek = nil, false, at, time.Now()
	go func() {
		defer close(done)
		defer close(out)
		if err := s.run(ctx, out, at); err != nil && ctx.Err() == nil {
			s.mu.Lock()
			s.err = err
			s.mu.Unlock()
		}
	}()
}

// Err 是取流或解码失败的原因（没失败为 nil）。
func (s *Session) Err() error { s.mu.Lock(); defer s.mu.Unlock(); return s.err }

// FramePoolStats 是帧池此刻的账目：预算、持有、借出、峰值与分配次数。
//
// 它是给诊断和测试用的读数，不是播放路径的一部分：Peak 与 PeakLent 能证明这一路
// 相对改造前的槽位上限到底压到了多少字节，Lent 归零能证明关闭、Seek 和取消之后
// 没有帧漏在别人手里。
func (s *Session) FramePoolStats() FramePoolStats { return s.pool.stats() }

// Duration 是整条视频流的时长，目录没读到之前为 0。
func (s *Session) Duration() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idx == nil {
		return 0
	}
	return s.idx.Duration()
}

// Seek 丢掉手上的一切，从 at 重新开始。
func (s *Session) Seek(at time.Duration) {
	s.frameMu.Lock()
	defer s.frameMu.Unlock()
	s.seekLocked(at)
}

func (s *Session) seekLocked(at time.Duration) {
	s.stop()
	s.reclaimFrames()
	if s.pool.isClosed() {
		// Close 之后又 Seek：不再开新的一路（池已经收摊，开了也只会立刻失败）。
		return
	}
	s.begin(at)
}

func (s *Session) stop() {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	if cancel == nil || done == nil {
		return
	}
	cancel()
	<-done
}

// Close stops the decoder, recycles all frames that have not been consumed, drops
// the pool's pooled buffers, and removes the reusable segment file. It is safe to
// call concurrently and more than once.
func (s *Session) Close() {
	s.closeOnce.Do(func() {
		s.frameMu.Lock()
		defer s.frameMu.Unlock()
		s.stop()
		s.reclaimFrames()
		// 池里还留着的空闲缓冲也一并丢掉：Close 之后这块内存不该再算在进程头上。
		s.pool.close()
		if s.tempPath != "" {
			_ = os.Remove(s.tempPath)
			s.tempPath = ""
		}
	})
}

// reclaimFrames 把手上和队列里所有尚未消费的帧交回帧池。stop 之后 out 已关闭，
// 这里的非阻塞排空会把队列中剩下的帧全部回收；没有 stop 时也不会卡住。
func (s *Session) reclaimFrames() {
	if s.pend != nil {
		s.recycle(s.pend.Pix)
		s.pend = nil
	}
	if s.prevPix != nil {
		s.recycle(s.prevPix)
		s.prevPix = nil
	}
	s.shown, s.haveShow = Frame{}, false
	s.mu.Lock()
	out := s.out
	s.mu.Unlock()
	for out != nil {
		select {
		case f, ok := <-out:
			if !ok {
				return
			}
			s.recycle(f.Pix)
		default:
			return
		}
	}
}

// Poll 按时钟 pos 取当前该显示的那一帧：有新帧返回 true。
// 返回的 Pix 到下一次 Poll 返回 true 之前有效。
func (s *Session) Poll(pos time.Duration) (Frame, bool) {
	s.frameMu.Lock()
	defer s.frameMu.Unlock()
	if time.Since(s.lastSeek) > resyncGap {
		behind := s.haveShow && pos+resyncBack < s.shown.PTS
		ahead := pos > s.head+resyncAhead
		if behind || ahead {
			s.seekLocked(pos)
		}

	}
	s.mu.Lock()
	out := s.out
	s.mu.Unlock()
	var got *Frame
	for {
		var f Frame
		if s.pend != nil {
			f, s.pend = *s.pend, nil
		} else {
			select {
			case x, ok := <-out:
				if !ok {
					goto end
				}
				f = x
			default:
				goto end
			}
		}
		s.head = max(s.head, f.PTS)
		if f.PTS > pos+frameSlack {
			s.pend = &f
			break
		}
		if got != nil {
			s.recycle(got.Pix)
		}
		g := f
		got = &g
	}
end:
	if got == nil {
		return Frame{}, false
	}
	if s.prevPix != nil {
		s.recycle(s.prevPix)
	}
	s.prevPix = got.Pix
	s.shown, s.haveShow = *got, true
	return *got, true
}

func (s *Session) recycle(b []byte) {
	s.pool.Release(b)
}

func (s *Session) index(ctx context.Context) (*Index, error) {
	s.mu.Lock()
	idx := s.idx
	s.mu.Unlock()
	if idx != nil {
		return idx, nil
	}
	for n := int64(headProbe); ; n *= 4 {
		b, err := s.cfg.Fetch(ctx, 0, n)
		if err != nil {
			return nil, err
		}
		idx, err = ParseIndex(b)
		if err == nil {
			break
		}
		if err != errNeedMore || n >= headMax || int64(len(b)) < n {
			return nil, err
		}
	}
	s.mu.Lock()
	s.idx = idx
	s.mu.Unlock()
	return idx, nil
}

type fetched struct {
	data []byte
	err  error
}

func (s *Session) run(ctx context.Context, out chan<- Frame, at time.Duration) error {
	idx, err := s.index(ctx)
	if err != nil {
		return err
	}
	get := func(i int) <-chan fetched {
		c := make(chan fetched, 1)
		go func() {
			g := idx.Segments[i]
			b, err := s.cfg.Fetch(ctx, g.Offset, g.Offset+g.Size)
			if err == nil && int64(len(b)) != g.Size {
				err = fmt.Errorf("video: 片段 %d 长度 %d，应为 %d", i, len(b), g.Size)
			}
			c <- fetched{b, err}
		}()
		return c
	}
	first := idx.At(at)
	if first >= len(idx.Segments) {
		return nil
	}
	cur := get(first)
	for i := first; i < len(idx.Segments); i++ {
		var r fetched
		select {
		case r = <-cur:
		case <-ctx.Done():
			return nil
		}
		if r.err != nil {
			return r.err
		}
		if i+1 < len(idx.Segments) {
			cur = get(i + 1)
		}
		start := time.Duration(0)
		if i == first {
			start = at
		}
		if err := s.playSegment(ctx, out, idx.Init, r.data, idx.Segments[i].Start, start); err != nil {
			return err
		}
	}
	return nil
}

func (s *Session) playSegment(ctx context.Context, out chan<- Frame, init, data []byte, segStart, start time.Duration) error {
	if s.tempPath == "" {
		f, err := os.CreateTemp(s.cfg.TempDir, "biu-video-*.mp4")
		if err != nil {
			return err
		}
		s.tempPath = f.Name()
		if err := f.Close(); err != nil {
			_ = os.Remove(s.tempPath)
			s.tempPath = ""
			return err
		}
	}
	f, err := os.OpenFile(s.tempPath, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(init)
	if err == nil {
		_, err = f.Write(data)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	src, err := s.cfg.Open(s.tempPath, s.pool)
	if err != nil {
		return err
	}
	defer src.Close()
	// 解码器把时间轴对到了这个文件开头：第一帧是这个片段里最早显示的那一帧，
	// 它在流上的时刻就是 sidx 给的片段起点。
	off, haveOff := time.Duration(0), false
	for {
		select {
		case fr, ok := <-src.Frames:
			if !ok {
				return src.Err()
			}
			if !haveOff {
				off, haveOff = segStart-fr.PTS, true
			}
			fr.PTS += off
			if fr.PTS < start {
				src.Recycle(fr.Pix)
				continue
			}
			select {
			case out <- fr:
			case <-ctx.Done():
				// 这一帧已经从池里借了缓冲；取消时它不会被任何人消费，
				// 必须在这里还回去，gauge 才能归零。
				src.Recycle(fr.Pix)
				return nil
			}
		case <-ctx.Done():
			return nil
		}
	}
}
