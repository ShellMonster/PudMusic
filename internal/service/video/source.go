package video

import (
	"errors"
	"sync"
)

// ErrUnsupported 是这个平台还没有视频解码器。
var ErrUnsupported = errors.New("video: 这个平台暂不支持视频解码")

// Source 是一路正在解码的视频。
type Source struct {
	Frames <-chan Frame
	frames chan Frame
	free   chan []byte
	quit   chan struct{}
	done   chan struct{}
	mu     sync.Mutex
	err    error
}

// Err 在 Frames 关闭后给出终止原因（正常读完为 nil）。
func (s *Source) Err() error { s.mu.Lock(); defer s.mu.Unlock(); return s.err }

// Close 停掉解码并等线程退出。
func (s *Source) Close() {
	select {
	case <-s.quit:
	default:
		close(s.quit)
	}
	<-s.done
}

// Recycle 把用完的像素缓冲交回，省得每帧分配。
func (s *Source) Recycle(b []byte) {
	select {
	case s.free <- b:
	default:
	}
}

func newSource(pool chan []byte) *Source {
	if pool == nil {
		pool = make(chan []byte, 6)
	}
	s := &Source{frames: make(chan Frame, 3), free: pool, quit: make(chan struct{}), done: make(chan struct{})}
	s.Frames = s.frames
	return s
}
