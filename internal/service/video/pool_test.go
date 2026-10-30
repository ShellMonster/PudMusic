package video

import (
	"errors"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/perf"
)

// waitUntil 轮询一个条件直到成立。时间只用来给失败设上限，不用来当判据：
// 固定睡眠猜不到「解码器已经跑到哪了」。
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("等不到%s", what)
}

func TestFramePoolReusesBuffersWithinTheByteBudget(t *testing.T) {
	var g perf.Gauges
	p := newFramePool(4<<10, &g)
	a, err := p.Acquire(1<<10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := g.Snapshot().FramePoolBytes; got != 1<<10 {
		t.Fatalf("借出后 gauge = %d, want %d", got, 1<<10)
	}
	if st := p.stats(); st.Lent != 1<<10 || st.Allocated != 1<<10 || st.Free != 0 {
		t.Fatalf("借出后 stats = %+v", st)
	}

	p.Release(a)
	if got := g.Snapshot().FramePoolBytes; got != 0 {
		t.Fatalf("归还后 gauge = %d, want 0", got)
	}
	if st := p.stats(); st.Lent != 0 || st.Allocated != 1<<10 || st.Free != 1 {
		t.Fatalf("归还后 stats = %+v", st)
	}

	b, err := p.Acquire(1<<10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if &b[0] != &a[0] {
		t.Fatal("同样大小的帧又新分配了一块，没有复用")
	}
	if st := p.stats(); st.Allocations != 1 || st.PeakLent != 1<<10 || st.Free != 0 {
		t.Fatalf("复用后 stats = %+v", st)
	}
	p.Release(b)
}

func TestFramePoolWaitsForAReleaseInsteadOfGrowing(t *testing.T) {
	var g perf.Gauges
	p := newFramePool(2<<10, &g)
	a, err := p.Acquire(1<<10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Acquire(1<<10, nil); err != nil {
		t.Fatal(err)
	}

	type result struct {
		buf []byte
		err error
	}
	res := make(chan result, 1)
	go func() {
		b, err := p.Acquire(1<<10, nil)
		res <- result{b, err}
	}()
	waitUntil(t, "第三个分配进入等待", func() bool { return p.stats().Waiters == 1 })

	select {
	case r := <-res:
		t.Fatalf("预算已经用满还拿得到缓冲: %v", r.err)
	default:
	}
	if st := p.stats(); st.Allocated != 2<<10 || st.Lent != 2<<10 {
		t.Fatalf("等待期间 stats = %+v，预算 2KiB 不该被突破", st)
	}

	p.Release(a)
	var got result
	select {
	case got = <-res:
		if got.err != nil {
			t.Fatal(got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("归还之后分配还是没醒")
	}
	if st := p.stats(); st.Allocated > 2<<10 || st.PeakLent > 2<<10 || st.Peak > 2<<10 {
		t.Fatalf("峰值超过预算: %+v", st)
	}
	p.Release(got.buf)
}

func TestFramePoolRejectsAFrameThatCannotFitTwice(t *testing.T) {
	p := newFramePool(1<<10, &perf.Gauges{})
	if _, err := p.Acquire(700, nil); !errors.Is(err, ErrFramePoolBudget) {
		t.Fatalf("一帧 700 字节、预算 1KiB 时 err = %v, want ErrFramePoolBudget", err)
	}
	// 正好两帧是允许的：显示中的一帧 + 正在解码的一帧。
	p2 := newFramePool(2<<10, &perf.Gauges{})
	a, err := p2.Acquire(1<<10, nil)
	if err != nil {
		t.Fatal(err)
	}
	p2.Release(a)
}

func TestFramePoolDropsBuffersThatCanNoLongerFit(t *testing.T) {
	var g perf.Gauges
	p := newFramePool(8<<10, &g)
	a, err := p.Acquire(1<<10, nil)
	if err != nil {
		t.Fatal(err)
	}
	p.Release(a)
	if st := p.stats(); st.Allocated != 1<<10 || st.Free != 1 {
		t.Fatalf("先手 stats = %+v", st)
	}

	// 换分辨率之后旧缓冲放不下新帧，留着只会白占预算。
	b, err := p.Acquire(4<<10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st := p.stats(); st.Allocated != 4<<10 || st.Free != 0 || st.Lent != 4<<10 {
		t.Fatalf("换尺寸后 stats = %+v", st)
	}
	if got := g.Snapshot().FramePoolBytes; got != 4<<10 {
		t.Fatalf("换尺寸后 gauge = %d, want %d", got, 4<<10)
	}
	p.Release(b)
}

func TestFramePoolStopsWaitingWhenClosedOrCanceled(t *testing.T) {
	p := newFramePool(2<<10, &perf.Gauges{})
	a, err := p.Acquire(1<<10, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.Acquire(1<<10, nil)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	errs := make(chan error, 1)
	go func() {
		_, err := p.Acquire(1<<10, done)
		errs <- err
	}()
	waitUntil(t, "取消前进入等待", func() bool { return p.stats().Waiters == 1 })
	close(done)
	select {
	case err := <-errs:
		if !errors.Is(err, ErrFramePoolStopped) {
			t.Fatalf("取消 err = %v, want ErrFramePoolStopped", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("取消之后分配没醒")
	}

	closed := make(chan error, 1)
	go func() {
		_, err := p.Acquire(1<<10, nil)
		closed <- err
	}()
	waitUntil(t, "关闭前进入等待", func() bool { return p.stats().Waiters == 1 })
	p.close()
	select {
	case err := <-closed:
		if !errors.Is(err, ErrFramePoolClosed) {
			t.Fatalf("关闭 err = %v, want ErrFramePoolClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("关闭之后分配没醒")
	}
	p.Release(a)
	p.Release(b)
	if st := p.stats(); st.Lent != 0 || st.Allocated != 0 {
		t.Fatalf("关闭归还后 stats = %+v", st)
	}
}

func TestFramePoolIgnoresForeignAndDoubleReleases(t *testing.T) {
	var g perf.Gauges
	p := newFramePool(8<<10, &g)
	a, err := p.Acquire(1<<10, nil)
	if err != nil {
		t.Fatal(err)
	}
	p.Release(a)
	p.Release(a)
	if st := p.stats(); st.Free != 1 || st.Lent != 0 {
		t.Fatalf("重复归还后 stats = %+v", st)
	}
	p.Release(make([]byte, 1<<10))
	if st := p.stats(); st.Allocated != 1<<10 || st.Free != 1 {
		t.Fatalf("外来缓冲被记账了: %+v", st)
	}
	if got := g.Snapshot().FramePoolBytes; got != 0 {
		t.Fatalf("gauge = %d, want 0", got)
	}
}

func TestDefaultFramePoolBudgetFitsThree4KFrames(t *testing.T) {
	if defaultFramePoolBytes < 3*frame4KBytes {
		t.Fatalf("默认预算 %d 放不下三帧 4K（%d）", defaultFramePoolBytes, 3*frame4KBytes)
	}
	// 上限也别给成一整部片的内存：4K 三帧约 95MiB，四帧就该是上限了。
	if defaultFramePoolBytes > 4*frame4KBytes {
		t.Fatalf("默认预算 %d 超过四帧 4K（%d），留的量太大", defaultFramePoolBytes, 4*frame4KBytes)
	}
}
