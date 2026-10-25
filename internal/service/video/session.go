package video

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"
)

// Config 是一路视频要的外部依赖。
type Config struct {
	// Fetch 取流上 [from, to) 这段字节。地址失效、换 CDN 都由它自己处理。
	Fetch func(ctx context.Context, from, to int64) ([]byte, error)
	// Open 解码一个本地 fMP4 文件；空则用平台默认。
	Open func(path string, pool chan []byte) (*Source, error)
	// TempDir 放分片临时文件；空则用系统临时目录。
	TempDir string
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
	cfg Config

	mu     sync.Mutex
	idx    *Index
	err    error
	cancel context.CancelFunc
	out    chan Frame
	done   chan struct{}

	pend     *Frame
	shown    Frame
	haveShow bool
	head     time.Duration
	lastSeek time.Time
	prevPix  []byte
	free     chan []byte
}

// Start 从 at 开始取流解码。
func Start(cfg Config, at time.Duration) *Session {
	if cfg.Open == nil {
		cfg.Open = Open
	}
	s := &Session{cfg: cfg, free: make(chan []byte, 8)}
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
	s.stop()
	s.begin(at)
}

func (s *Session) stop() {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	cancel()
	<-done
}

// Close 停掉一切并清掉临时文件。
func (s *Session) Close() { s.stop() }

// Poll 按时钟 pos 取当前该显示的那一帧：有新帧返回 true。
// 返回的 Pix 到下一次 Poll 返回 true 之前有效。
func (s *Session) Poll(pos time.Duration) (Frame, bool) {
	if time.Since(s.lastSeek) > resyncGap {
		behind := s.haveShow && pos+resyncBack < s.shown.PTS
		ahead := pos > s.head+resyncAhead
		if behind || ahead {
			s.Seek(pos)
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
	select {
	case s.free <- b:
	default:
	}
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
	f, err := os.CreateTemp(s.cfg.TempDir, "biu-video-*.mp4")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
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
	src, err := s.cfg.Open(f.Name(), s.free)
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
				return nil
			}
		case <-ctx.Done():
			return nil
		}
	}
}
