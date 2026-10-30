package video

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/perf"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/frag.mp4")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func fetcher(b []byte) func(context.Context, int64, int64) ([]byte, error) {
	return func(_ context.Context, from, to int64) ([]byte, error) {
		to = min(to, int64(len(b)))
		return b[from:to], nil
	}
}

func TestSessionClosesWithNoTemporaryFilesLeft(t *testing.T) {
	data := fixture(t)
	dir := t.TempDir()
	idx, err := ParseIndex(data)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var paths []string
	open := func(path string, pool BufferPool) (*Source, error) {
		mu.Lock()
		paths = append(paths, path)
		mu.Unlock()
		s := newSource(pool)
		go func() {
			close(s.frames)
			close(s.done)
		}()
		return s, nil
	}
	s := Start(Config{Fetch: fetcher(data), Open: open, TempDir: dir}, 0)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(paths)
		mu.Unlock()
		if n == len(idx.Segments) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	s.Close()

	mu.Lock()
	got := append([]string(nil), paths...)
	mu.Unlock()
	if len(got) != len(idx.Segments) {
		t.Fatalf("opened %d segments, want %d", len(got), len(idx.Segments))
	}
	if len(got) == 0 {
		t.Fatal("no segment paths recorded")
	}
	for _, path := range got {
		if path != got[0] {
			t.Fatalf("segment temporary path changed: %q then %q", got[0], path)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary directory has %d entries after close", len(entries))
	}
}

func TestSessionCloseRecyclesPendingAndQueuedFrames(t *testing.T) {
	var g perf.Gauges
	pool := newFramePool(64<<10, &g)
	// 先按四块小缓冲的尺寸借出来，再原样交回，让池子记着它们的账。
	pix := make([][]byte, 4)
	for i := range pix {
		b, err := pool.Acquire(1<<10, nil)
		if err != nil {
			t.Fatal(err)
		}
		pix[i] = b
	}
	s := &Session{
		pool:   pool,
		cancel: func() {},
		done:   make(chan struct{}),
		out:    make(chan Frame, 3),
	}
	close(s.done)
	s.pend = &Frame{Pix: pix[0]}
	s.prevPix = pix[1]
	s.out <- Frame{Pix: pix[2]}
	s.out <- Frame{Pix: pix[3]}

	s.Close()
	if st := pool.stats(); st.Lent != 0 || st.Allocated != 0 {
		t.Fatalf("close 之后池里还有账: %+v", st)
	}
	if got := g.Snapshot().FramePoolBytes; got != 0 {
		t.Fatalf("close 之后 gauge = %d, want 0", got)
	}
	if s.pend != nil || s.prevPix != nil {
		t.Fatal("session retained a pending or displayed frame after close")
	}
}

func TestSourceCloseIsConcurrentAndIdempotent(t *testing.T) {
	s := newSource(nil)
	go func() {
		close(s.frames)
		close(s.done)
	}()

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Close()
		}()
	}
	wg.Wait()
}

func TestSourceCloseReturnsQueuedFramesToThePool(t *testing.T) {
	var g perf.Gauges
	pool := newFramePool(64<<10, &g)
	s := newSource(pool)
	for i := 0; i < frameQueueDepth; i++ {
		b, err := pool.Acquire(1<<10, nil)
		if err != nil {
			t.Fatal(err)
		}
		s.frames <- Frame{Pix: b}
	}
	if st := pool.stats(); st.Lent != frameQueueDepth<<10 {
		t.Fatalf("排队时 stats = %+v", st)
	}
	go func() {
		close(s.frames)
		close(s.done)
	}()
	s.Close()
	st := pool.stats()
	if st.Lent != 0 {
		t.Fatalf("Close 之后队列里的帧没还回去: %+v", st)
	}
	// gauge 只报「借出去还没还」的字节，Close 之后必须归零。空闲缓冲还在池里
	// （关闭后会被 close() 一起丢掉，见下一个断言）。
	if got := g.Snapshot().FramePoolBytes; got != 0 {
		t.Fatalf("Close 之后 gauge = %d, want 0", got)
	}
	pool.close()
	if st := pool.stats(); st.Allocated != 0 {
		t.Fatalf("close 之后池里还持有分配: %+v", st)
	}
}

func TestParseIndexFindsEverySegment(t *testing.T) {
	b := fixture(t)
	if _, err := ParseIndex(b[:40]); err != errNeedMore {
		t.Fatalf("truncated head: %v", err)
	}
	idx, err := ParseIndex(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Segments) < 3 {
		t.Fatalf("segments=%d", len(idx.Segments))
	}
	var sum int64
	for i, g := range idx.Segments {
		if i > 0 && g.Offset != idx.Segments[i-1].Offset+idx.Segments[i-1].Size {
			t.Fatalf("segment %d not contiguous", i)
		}
		sum += g.Size
	}
	if last := idx.Segments[len(idx.Segments)-1]; last.Offset+last.Size > int64(len(b)) {
		t.Fatalf("segments end at %d, file is %d", last.Offset+last.Size, len(b))
	}
	if d := idx.Duration(); d < 3900*time.Millisecond || d > 4200*time.Millisecond {
		t.Fatalf("duration %v", d)
	}
	if i := idx.At(1500 * time.Millisecond); idx.Segments[i].Start > 1500*time.Millisecond || (i+1 < len(idx.Segments) && idx.Segments[i+1].Start <= 1500*time.Millisecond) {
		t.Fatalf("At picked %d", i)
	}
}

func TestSessionDeliversFramesInOrderAcrossSegments(t *testing.T) {
	s := Start(Config{Fetch: fetcher(fixture(t)), TempDir: t.TempDir()}, 0)
	defer s.Close()
	var last time.Duration = -1
	n := 0
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && last < 3500*time.Millisecond {
		// ⚠️ **问的位置要跟着帧走，不能问「一小时之后」**。
		//
		// `Poll` 里有一段「离上次 seek 超过 resyncGap（1.5 秒）就重新对时」：而
		// 「一小时之后」这个位置**永远在** `s.head+resyncAhead` 前面 —— 于是那一段
		// 一到（机器一忙，第一帧晚 1.5 秒就到）就会 seek 到一小时处（视频早就
		// 放完了）→ 那一趟直接结束 → **一帧都拿不到**：
		//
		//	stopped at -1ns after 0 polls
		//
		// 问「上一帧 + 100 毫秒」就不会触发它（那个位置永远落在 `s.head` 后面
		// 一点点），而这一条要量的「帧按顺序来」一点没变。
		f, ok := s.Poll(last + 100*time.Millisecond)
		if !ok {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		if f.PTS < last {
			t.Fatalf("frame went back: %v after %v", f.PTS, last)
		}
		if f.W != 160 || f.H != 90 || len(f.Pix) != 160*90*4 {
			t.Fatalf("frame %dx%d len %d", f.W, f.H, len(f.Pix))
		}
		last = f.PTS
		n++
	}
	if last < 3500*time.Millisecond {
		t.Fatalf("stopped at %v after %d polls, err=%v", last, n, s.Err())
	}
}

func TestSessionStartsAtTheRequestedTime(t *testing.T) {
	s := Start(Config{Fetch: fetcher(fixture(t)), TempDir: t.TempDir()}, 2*time.Second)
	defer s.Close()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		// ⚠️ **问的位置不能是「一小时之后」**——和上面那一条同一个毛病。
		//
		// `Poll` 里有一段「离上次 seek 超过 resyncGap（1.5 秒）就重新对时」：
		// 而「一小时之后」这个位置**永远在** `s.head+resyncAhead` 前面 ⇒ 机器
		// 一忙（第一帧晚 1.5 秒才到）就 seek 到一小时处（视频早就放完了）
		// → 那一趟直接结束 → **一帧都拿不到**：
		//
		//	no frame, err=<nil>
		//
		// 问「起点 + 100 毫秒」就不会触发它（那个位置落在这一趟刚开始的那个
		// 2 秒之后，而 `s.head` 还没跑过它），而这一条要量的「从要求的那个
		// 时间开始」一点没变。
		if f, ok := s.Poll(2*time.Second + 100*time.Millisecond); ok {
			if f.PTS < 2*time.Second {
				t.Fatalf("first frame %v is before the start", f.PTS)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no frame, err=%v", s.Err())
}

func TestSessionFollowsABackwardSeek(t *testing.T) {
	s := Start(Config{Fetch: fetcher(fixture(t)), TempDir: t.TempDir()}, 0)
	defer s.Close()
	poll := func(pos time.Duration) Frame {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if f, ok := s.Poll(pos); ok {
				return f
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("no frame at %v, err=%v", pos, s.Err())
		return Frame{}
	}
	poll(3 * time.Second)
	time.Sleep(1600 * time.Millisecond)
	if f := poll(500 * time.Millisecond); f.PTS > 600*time.Millisecond {
		t.Fatalf("after seeking back got %v", f.PTS)
	}
}

func TestSessionReportsAFetchFailure(t *testing.T) {
	s := Start(Config{Fetch: func(context.Context, int64, int64) ([]byte, error) { return nil, context.DeadlineExceeded }, TempDir: t.TempDir()}, 0)
	defer s.Close()
	deadline := time.Now().Add(5 * time.Second)
	for s.Err() == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if s.Err() == nil {
		t.Fatal("no error reported")
	}
}
