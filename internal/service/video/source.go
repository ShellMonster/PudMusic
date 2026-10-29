package video

import (
	"errors"
	"sync"
)

// ErrUnsupported 是这个平台还没有视频解码器。
var ErrUnsupported = errors.New("video: 这个平台暂不支持视频解码")

// frameQueueDepth 是解码器交帧队列的深度（槽位数）。**它不再是内存上限**：
// 真正的上限是帧池的字节预算，队列里每一帧都占着预算。
const frameQueueDepth = 3

// Source 是一路正在解码的视频。
//
// 像素缓冲从 FramePool 借、用完交回 Pool.Release：帧池按总字节而不是槽位数量
// 约束，解码器再也不会自己 make 一块不记账的缓冲。
type Source struct {
	Frames <-chan Frame
	frames chan Frame
	pool   BufferPool
	quit   chan struct{}
	done   chan struct{}
	mu     sync.Mutex
	err    error
	close  sync.Once
}

// Err 在 Frames 关闭后给出终止原因（正常读完为 nil）。
func (s *Source) Err() error { s.mu.Lock(); defer s.mu.Unlock(); return s.err }

// fail 记下终止原因。
func (s *Source) fail(err error) { s.mu.Lock(); s.err = err; s.mu.Unlock() }

// Close 停掉解码、等线程退出，并把队列里还没被取走的帧交回帧池。
// 关闭之后 Frames 里不会再有可取的帧，所以这时排空是安全的。
func (s *Source) Close() {
	s.close.Do(func() { close(s.quit) })
	<-s.done
	s.drain()
}

// drain 把队列里剩下的帧还回池里。非阻塞：读不到就说明没人排队了。
func (s *Source) drain() {
	for {
		select {
		case f, ok := <-s.frames:
			if !ok {
				return
			}
			s.Recycle(f.Pix)
		default:
			return
		}
	}
}

// Recycle 把用完的像素缓冲交回帧池。
func (s *Source) Recycle(b []byte) {
	if s.pool == nil {
		return
	}
	s.pool.Release(b)
}

// acquire 从帧池借一块 n 字节的缓冲。解码线程只在这里拿内存，所以池的字节预算
// 就是这个解码器能占多少像素内存的上限；quit 关了（Close/Seek/会话取消）就不再
// 等预算。
//
// 没有池时（例如只测 Source 自身的单测用 newSource(nil)）退回自己分配：调用方
// 不该为了「没有池」再写一遍分支。
func (s *Source) acquire(n int64) ([]byte, error) {
	if s.pool == nil {
		return make([]byte, n), nil
	}
	return s.pool.Acquire(n, s.quit)
}

func newSource(pool BufferPool) *Source {
	s := &Source{frames: make(chan Frame, frameQueueDepth), pool: pool, quit: make(chan struct{}), done: make(chan struct{})}
	s.Frames = s.frames
	return s
}

// Supported 报告这个平台有没有视频解码器。
func Supported() bool { return supported }
