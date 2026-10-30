package video

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/ShellMonster/PudMusic/internal/perf"
)

var (
	// ErrFramePoolBudget 表示一帧本身就占掉预算的一半以上，池子里永远放不下
	// 「正在显示的那一帧 + 正在解码的那一帧」这一对。
	ErrFramePoolBudget = errors.New("video: 帧池预算放不下一帧")
	// ErrFramePoolStopped 表示要缓冲的那一方已经收摊（解码线程退出、会话取消）。
	ErrFramePoolStopped = errors.New("video: 解码器已停止")
	// ErrFramePoolClosed 表示帧池所属的会话已经关闭。
	ErrFramePoolClosed = errors.New("video: 帧池已关闭")
	errFramePoolSize   = errors.New("video: 帧尺寸不合法")
)

const (
	// frame4KBytes 是一帧 4K RGBA 的字节数：3840*2160*4 ≈ 31.6 MiB。
	frame4KBytes = int64(3840 * 2160 * 4)
	// defaultFramePoolBytes 是帧池的默认总字节预算，按「同时持有三帧 4K」算。
	//
	// 三帧的来历：一帧正在显示、一帧正在解码、一帧在队列里等着被取。改造前
	// out(8) + 解码器 frames(3) + 显示帧 + pend 帧谁也不按字节算，4K 下最坏能
	// 堆十几帧、约 400 MiB；现在整个池不超过 94.9 MiB。
	//
	// 换到 1080p 一帧 8.3 MiB，同一预算能放 11 帧，和改造前 8+3 的自然深度相当，
	// 普通分辨率不会因为预算而少缓冲。
	defaultFramePoolBytes = 3 * frame4KBytes
)

// BufferPool 是解码器借还像素缓冲的地方。Session 用它把「当前有多少帧字节在飞」
// 收进一个按总字节计量的预算，而不是只数槽位。
type BufferPool interface {
	// Acquire 借一块至少 n 字节、内容随意的缓冲。预算不足时等待别人归还；
	// cancel 关闭时放弃等待；池关闭或一帧大到预算放不下两帧时返回可识别的错误。
	Acquire(n int64, cancel <-chan struct{}) ([]byte, error)
	// Release 把用完的缓冲交回池里。不是这个池发出去的、或已经交回过的会被忽略。
	Release(b []byte)
}

// FramePoolStats 是帧池某一刻的账目，测试与诊断用。
type FramePoolStats struct {
	Budget      int64 // 总预算
	Allocated   int64 // 池持有的全部分配（空闲 + 借出）
	Lent        int64 // 已经借出去、还没还的字节
	Peak        int64 // 历史峰值：池持有的分配（空闲 + 借出，含刚分配那一刻）
	PeakLent    int64 // 历史峰值：同时在飞的帧字节
	Allocations int64 // 一共分配过多少块（复用不算）
	Free        int   // 空闲列表里的块数
	Waiters     int   // 正在等缓冲的调用方
}

// pooledBuffer 是空闲列表里一个待复用的缓冲；size 是它占的预算（= cap）。
type pooledBuffer struct {
	buf  []byte
	size int64
}

// framePool 是按总字节约束的 RGBA 帧缓冲池。
//
// 语义：
//   - 有空闲缓冲放得下就复用；
//   - 剩下的空闲缓冲都比要的大小时把它们丢掉（它们对本帧没用，留着只占预算），
//     再按「池持有的全部分配还够」决定是否新分配；
//   - 预算不够就排队等 release（背压），让解码停在原地，而不是继续向上涨；
//   - 一帧就超过预算一半时直接返回 ErrFramePoolBudget。理由：那种尺寸就算等到
//     池子全空也放不下两帧，等下去只会把解码线程永远挂住；给一个可诊断的错误，
//     由 Session.Err 交给播放器，比无声卡死或悄悄跳过每一帧都更清楚。
//
// **预算**约束「池持有的全部分配」（空闲 + 借出），因为那才是进程真正占着的内存。
// **gauge**报「已经被帧借走、还没还回来」的字节：借出时加，归还或丢弃时减，池里
// 歇着的空闲缓冲不计。这样 Seek / Close / 取消之后 gauge 一定回到 0，而空闲缓冲
// 仍能跨 Seek 复用；#668 的进程快照读到的就是「此刻有多少视频帧内存在用」。
type framePool struct {
	budget int64
	gauge  *perf.Gauges

	mu          sync.Mutex
	free        []pooledBuffer
	sizes       map[uintptr]int64 // 池持有的每块缓冲的基址 -> 预算字节
	out         map[uintptr]bool  // 已经借出去、还没归还的基址
	allocated   int64
	lent        int64
	peak        int64
	peakLent    int64
	allocations int64
	waiting     []chan struct{}
	closed      bool
}

func newFramePool(budget int64, gauge *perf.Gauges) *framePool {
	if budget <= 0 {
		budget = defaultFramePoolBytes
	}
	if gauge == nil {
		gauge = perf.ProcessGauges()
	}
	return &framePool{
		budget: budget,
		gauge:  gauge,
		sizes:  make(map[uintptr]int64),
		out:    make(map[uintptr]bool),
	}
}

// Acquire 借一块至少 n 字节的缓冲。
func (p *framePool) Acquire(n int64, cancel <-chan struct{}) ([]byte, error) {
	if n <= 0 {
		return nil, fmt.Errorf("%w: %d 字节", errFramePoolSize, n)
	}
	if n*2 > p.budget {
		return nil, fmt.Errorf("%w: 一帧 %d 字节，预算 %d 字节里放不下两帧", ErrFramePoolBudget, n, p.budget)
	}
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return nil, ErrFramePoolClosed
		}
		if buf, ok := p.takeFreeLocked(n); ok {
			p.mu.Unlock()
			return buf, nil
		}
		if p.dropFreeLocked() {
			// 空闲列表里剩下的都比 n 小，丢掉之后重算预算。
			p.mu.Unlock()
			continue
		}
		if p.allocated+n <= p.budget {
			buf := make([]byte, n)
			base := bufferBase(buf)
			p.sizes[base] = n
			p.out[base] = true
			p.allocated += n
			p.lent += n
			p.allocations++
			if p.allocated > p.peak {
				p.peak = p.allocated
			}
			if p.lent > p.peakLent {
				p.peakLent = p.lent
			}
			p.mu.Unlock()
			p.gauge.AddFramePoolBytes(n)
			return buf, nil
		}
		w := make(chan struct{}, 1)
		p.waiting = append(p.waiting, w)
		p.mu.Unlock()
		select {
		case <-w:
		case <-cancel:
			p.mu.Lock()
			p.removeWaiterLocked(w)
			p.mu.Unlock()
			return nil, ErrFramePoolStopped
		}
	}
}

// Release 把用完的缓冲交回池里。
func (p *framePool) Release(b []byte) {
	if cap(b) == 0 {
		return
	}
	base := bufferBase(b)
	p.mu.Lock()
	size, owned := p.sizes[base]
	if !owned || !p.out[base] {
		// 不是本池发的，或者已经交回过一次：忽略，免得一块缓冲被记两次。
		p.mu.Unlock()
		return
	}
	delete(p.out, base)
	p.lent -= size
	if p.lent < 0 {
		p.lent = 0
	}
	// 这块缓冲不再是「正在被帧用着」了，gauge 一律先降；空闲列表里的缓冲不算在内。
	p.gauge.AddFramePoolBytes(-size)
	if p.closed {
		// 会话已经关了：这块缓冲不再留着，分配账一起降。
		p.dropLocked(base, size)
		p.mu.Unlock()
		return
	}
	p.free = append(p.free, pooledBuffer{buf: b[:cap(b)], size: size})
	w := p.popWaiterLocked()
	p.mu.Unlock()
	if w != nil {
		w <- struct{}{}
	}
}

// stats 返回一份账目快照。
func (p *framePool) stats() FramePoolStats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return FramePoolStats{Budget: p.budget,
		Allocated:   p.allocated,
		Lent:        p.lent,
		Peak:        p.peak,
		PeakLent:    p.peakLent,
		Allocations: p.allocations,
		Free:        len(p.free),
		Waiters:     len(p.waiting),
	}
}

// isClosed 报告池是不是已经随会话关掉了。
func (p *framePool) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

// close 丢掉池里所有空闲缓冲并叫醒等待者；会话关闭时调。
func (p *framePool) close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	for _, f := range p.free {
		p.dropLocked(bufferBase(f.buf), f.size)
	}
	p.free = nil
	ws := p.waiting
	p.waiting = nil
	p.mu.Unlock()
	for _, w := range ws {
		w <- struct{}{}
	}
}

// takeFreeLocked 取一块放得下 n 的空闲缓冲：挑放得下的里面最小的那块。
func (p *framePool) takeFreeLocked(n int64) ([]byte, bool) {
	best := -1
	for i, f := range p.free {
		if f.size < n {
			continue
		}
		if best < 0 || f.size < p.free[best].size {
			best = i
		}
	}
	if best < 0 {
		return nil, false
	}
	f := p.free[best]
	p.free = append(p.free[:best], p.free[best+1:]...)
	p.out[bufferBase(f.buf)] = true
	p.lent += f.size
	if p.lent > p.peakLent {
		p.peakLent = p.lent
	}
	p.gauge.AddFramePoolBytes(f.size)
	return f.buf[:n], true
}

// dropFreeLocked 丢掉所有空闲缓冲；有空闲才返回 true。空闲缓冲不在 gauge 里
// （它们归还时已经减过），所以这里只降 allocated。
func (p *framePool) dropFreeLocked() bool {
	if len(p.free) == 0 {
		return false
	}
	for _, f := range p.free {
		p.dropLocked(bufferBase(f.buf), f.size)
	}
	p.free = nil
	return true
}

// dropLocked 把一块缓冲从「持有」里去掉，只动 owned 那本账（`allocated`）。
// `lent` 由调用方先减：这里不看这块是不是借出去的。
func (p *framePool) dropLocked(base uintptr, size int64) {
	delete(p.sizes, base)
	delete(p.out, base)
	p.allocated -= size
	if p.allocated < 0 {
		p.allocated = 0
	}
}

func (p *framePool) popWaiterLocked() chan struct{} {
	if len(p.waiting) == 0 {
		return nil
	}
	w := p.waiting[0]
	p.waiting = p.waiting[1:]
	return w
}

func (p *framePool) removeWaiterLocked(w chan struct{}) {
	for i, x := range p.waiting {
		if x == w {
			p.waiting = append(p.waiting[:i], p.waiting[i+1:]...)
			return
		}
	}
}

// bufferBase 是缓冲的 Base 指针。池按基址认自己发出去的每一块；Go 的 GC 不搬
// 堆对象，所以这个地址在一块缓冲活着的时候是稳定的。调用方必须把整块缓冲原样
// 交回（不能是中间截出来的子切片），否则池认不出来、只当没交。
func bufferBase(b []byte) uintptr {
	if cap(b) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(unsafe.SliceData(b)))
}
