package video

import (
	"context"
	"errors"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/perf"
)

// syntheticStream 是一个不碰系统解码器的假解码器：按给定分辨率合成 RGBA 帧，
// 像素从帧池借、用完交回。它让测试能量「4K 帧流」的字节行为，而不用真的解码。
type syntheticStream struct {
	w, h int
	// count 是一条流上最多合成多少帧。
	count int
	// failAfter > 0 时，发到第 failAfter 帧就报错退出（模拟解码失败）。
	failAfter int
	// holdEvery > 0 时每 holdEvery 帧停一拍，给消费者追上的机会（模拟解码节奏）。
	holdEvery int
}

const syntheticFrameDur = 33 * time.Millisecond

// opener 返回 Config.Open。pool 由 Session 传进来，合成器按同一份预算借还。
func (d syntheticStream) opener() func(string, BufferPool) (*Source, error) {
	return func(_ string, pool BufferPool) (*Source, error) {
		s := newSource(pool)
		go func() {
			defer close(s.done)
			defer close(s.frames)
			for i := 0; i < d.count; i++ {
				if d.failAfter > 0 && i == d.failAfter {
					s.fail(errors.New("合成解码器按计划失败"))
					return
				}
				buf, err := s.acquireFrame(int64(d.w) * int64(d.h) * 4)
				if err != nil {
					// 会话取消/关闭是正常收摊；其余（含 ErrFramePoolBudget）
					// 是要交给 Session.Err 的失败。
					if !errors.Is(err, ErrFramePoolStopped) && !errors.Is(err, ErrFramePoolClosed) {
						s.fail(err)
					}
					return
				}
				fillSyntheticRGBA(buf, i)
				select {
				case s.frames <- Frame{PTS: time.Duration(i) * syntheticFrameDur, W: d.w, H: d.h, Pix: buf}:
				case <-s.quit:
					s.Recycle(buf)
					return
				}
				if d.holdEvery > 0 && i%d.holdEvery == 0 {
					select {
					case <-time.After(syntheticFrameDur):
					case <-s.quit:
						return
					}
				}
			}
		}()
		return s, nil
	}
}

// acquireFrame 借一帧的缓冲；quit 关了就不再等预算。
func (s *Source) acquireFrame(n int64) ([]byte, error) {
	if s.pool == nil {
		return make([]byte, n), nil
	}
	return s.pool.Acquire(n, s.quit)
}

// fillSyntheticRGBA 稀疏地写点内容：写满 4K 一帧会让测试把时间花在 memset 上，
// 这里只要证明每帧拿到的是自己的缓冲，不需要真的画出画面。
func fillSyntheticRGBA(buf []byte, frame int) {
	if len(buf) == 0 {
		return
	}
	for off := 0; off+4 <= len(buf); off += 4096 {
		buf[off] = byte(frame)
		buf[off+3] = 0xff
	}
}

func startSynthetic(t *testing.T, d syntheticStream, budget int64, gauge *perf.Gauges, at time.Duration) *Session {
	t.Helper()
	cfg := Config{
		Fetch:          fetcher(fixture(t)),
		Open:           d.opener(),
		TempDir:        t.TempDir(),
		FramePoolBytes: budget,
		Gauges:         gauge,
	}
	s := Start(cfg, at)
	if s.pool == nil {
		t.Fatal("session has no frame pool")
	}
	return s
}

// baselineGoroutines 在开始前记下当前 goroutine 数；测试末尾等它回落。
func baselineGoroutines() int { return runtime.NumGoroutine() }

func waitForGoroutines(t *testing.T, base int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= base {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("goroutine 数没回落：现在 %d，基线 %d", runtime.NumGoroutine(), base)
}

// sessionDone 报告解码 goroutine 是否已经退出（out 已关闭）。
func sessionDone(s *Session) bool {
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	if done == nil {
		return true
	}
	select {
	case <-done:
		return true
	default:
		return false
	}
}

// drive 像界面那样把播放位置当墙钟往前推，边推边取帧，取满 want 帧或
// 到 maxWall 就停。返回值是取到的帧数；每取到一帧回调一次 after。
func drive(s *Session, want int, maxWall time.Duration, after func(f Frame)) int {
	start := time.Now()
	n := 0
	for {
		pos := time.Since(start) + 100*time.Millisecond
		f, ok := s.Poll(pos)
		if ok {
			n++
			if after != nil {
				after(f)
			}
			if n >= want {
				return n
			}
			continue
		}
		if sessionDone(s) || time.Since(start) > maxWall {
			return n
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSyntheticStreamStaysWithinTheByteBudget(t *testing.T) {
	// 1080p 与 4K 各走一遍同样的路径：唯一的差别是每帧多少字节。
	for _, res := range []struct {
		name string
		w, h int
	}{
		{"1080p", 1920, 1080},
		{"4K", 3840, 2160},
	} {
		t.Run(res.name, func(t *testing.T) {
			base := baselineGoroutines()
			var g perf.Gauges
			frame := int64(res.w) * int64(res.h) * 4
			// 三帧的预算：显示一帧、解码一帧、队列里一帧 —— 和默认预算同一口径。
			// 改造前同一场景能堆 out(8)+source(3)+pend+显示，这里要证明它撞不过预算。
			budget := 3 * frame
			s := startSynthetic(t, syntheticStream{w: res.w, h: res.h, count: 6, holdEvery: 1}, budget, &g, 0)
			pool := s.pool

			// 边取边量：每拿到一帧就查一次账，峰值不能超预算。要 8 帧是为了
			// 跨过分片切换 —— 每个分片都会重开一路解码器。
			worst := int64(0)
			saw := drive(s, 8, 15*time.Second, func(f Frame) {
				if f.W != res.w || f.H != res.h || len(f.Pix) != int(frame) {
					t.Fatalf("帧 %dx%d len %d", f.W, f.H, len(f.Pix))
				}
				worst = max(worst, max(pool.stats().Lent, g.Snapshot().FramePoolBytes))
			})
			if saw < 8 {
				t.Fatalf("只取到 %d 帧，err=%v", saw, s.Err())
			}
			if worst > budget {
				t.Fatalf("峰值借出 %d 超过预算 %d", worst, budget)
			}
			if st := pool.stats(); st.PeakLent > budget || st.Peak > budget {
				t.Fatalf("峰值超过预算 %d: %+v", budget, st)
			}

			s.Close()
			if got := g.Snapshot().FramePoolBytes; got != 0 {
				t.Fatalf("关闭后 gauge = %d, want 0", got)
			}
			if st := pool.stats(); st.Lent != 0 || st.Allocated != 0 {
				t.Fatalf("关闭后池里还有账: %+v", st)
			}
			waitForGoroutines(t, base)
		})
	}
}

func TestSessionSeekReturnsFramesToThePool(t *testing.T) {
	base := baselineGoroutines()
	var g perf.Gauges
	frame := int64(320 * 180 * 4)
	s := startSynthetic(t, syntheticStream{w: 320, h: 180, count: 40, holdEvery: 1}, 4*frame, &g, 0)
	pool := s.pool

	waitUntil(t, "第一批帧进来", func() bool {
		s.Poll(100 * time.Millisecond)
		return g.Snapshot().FramePoolBytes > 0
	})
	// 连拖几次：每次 Seek 都要把手上和队列里的帧还回池里，再重开一路。
	for i := 0; i < 5; i++ {
		s.Seek(time.Duration(i) * 500 * time.Millisecond)
		if st := pool.stats(); st.Lent > pool.budget || st.Peak > pool.budget {
			t.Fatalf("第 %d 次 Seek 之后 %+v 超过预算 %d", i, st, pool.budget)
		}
		s.Poll(time.Duration(i)*500*time.Millisecond + 100*time.Millisecond)
	}
	s.Close()
	if got := g.Snapshot().FramePoolBytes; got != 0 {
		t.Fatalf("关闭后 gauge = %d, want 0", got)
	}
	if st := pool.stats(); st.Lent != 0 || st.Allocated != 0 {
		t.Fatalf("关闭后池里还有账: %+v", st)
	}
	waitForGoroutines(t, base)
}

// TestRapidSessionSwitchingReturnsEverything 模拟「快速切视频」：连着开几路会话、
// 每路取一两帧就关。切完之后 gauge 必须回到 0，goroutine 必须回落，临时文件
// 一个都不剩 —— 旧视频的帧、解码器和分片文件都不该挂在新会话身上。
func TestRapidSessionSwitchingReturnsEverything(t *testing.T) {
	base := baselineGoroutines()
	dir := t.TempDir()
	frame := int64(320 * 180 * 4)
	var gauges perf.Gauges
	for i := 0; i < 6; i++ {
		s := Start(Config{
			Fetch:          fetcher(fixture(t)),
			Open:           syntheticStream{w: 320, h: 180, count: 20, holdEvery: 1}.opener(),
			TempDir:        dir,
			FramePoolBytes: 3 * frame,
			Gauges:         &gauges,
		}, time.Duration(i)*200*time.Millisecond)
		s.Poll(100 * time.Millisecond)
		s.Close()
		if got := gauges.Snapshot().FramePoolBytes; got != 0 {
			t.Fatalf("第 %d 路关掉后 gauge = %d, want 0", i, got)
		}
		if st := s.FramePoolStats(); st.Lent != 0 || st.Allocated != 0 {
			t.Fatalf("第 %d 路关掉后池里还有账: %+v", i, st)
		}
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("切了 6 路之后临时目录里还剩 %v, err=%v", entries, err)
	}
	waitForGoroutines(t, base)
}

func TestSessionDecodeErrorReleasesFramesAndTempFile(t *testing.T) {
	base := baselineGoroutines()
	var g perf.Gauges
	dir := t.TempDir()
	s := Start(Config{
		Fetch:          fetcher(fixture(t)),
		Open:           syntheticStream{w: 320, h: 180, count: 20, failAfter: 2}.opener(),
		TempDir:        dir,
		FramePoolBytes: 4 * 320 * 180 * 4,
		Gauges:         &g,
	}, 0)
	// 取几帧，让池里确实借出过东西。
	for i := 0; i < 10; i++ {
		s.Poll(time.Duration(i) * 100 * time.Millisecond)
	}
	waitUntil(t, "解码错误报上来", func() bool { return s.Err() != nil })
	s.Close()
	if got := g.Snapshot().FramePoolBytes; got != 0 {
		t.Fatalf("解码错误关闭后 gauge = %d, want 0", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("解码错误后临时目录里还有 %d 个文件", len(entries))
	}
	waitForGoroutines(t, base)
}

func TestSessionOpenFailureLeavesNoTempFileOrFrames(t *testing.T) {
	base := baselineGoroutines()
	var g perf.Gauges
	dir := t.TempDir()
	s := Start(Config{
		Fetch:   fetcher(fixture(t)),
		Open:    func(string, BufferPool) (*Source, error) { return nil, errors.New("打不开") },
		TempDir: dir,
		Gauges:  &g,
	}, 0)
	waitUntil(t, "打开失败报上来", func() bool { return s.Err() != nil })
	s.Close()
	if got := g.Snapshot().FramePoolBytes; got != 0 {
		t.Fatalf("打开失败后 gauge = %d, want 0", got)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("打开失败后临时目录 = %v, err=%v", entries, err)
	}
	waitForGoroutines(t, base)
}

func TestSessionCloseIsSafeWhilePollAndSeekRace(t *testing.T) {
	base := baselineGoroutines()
	var g perf.Gauges
	s := startSynthetic(t, syntheticStream{w: 320, h: 180, count: 40, holdEvery: 2}, 4*320*180*4, &g, 0)
	pool := s.pool

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			s.Poll(time.Duration(i) * 20 * time.Millisecond)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			s.Seek(time.Duration(i) * 100 * time.Millisecond)
		}
	}()
	// 让上面两路真的跑起来再 Close，才是「关闭竞态」。
	waitUntil(t, "竞态里已经有帧借出", func() bool { return pool.stats().Allocations > 0 })
	s.Close()
	wg.Wait()

	if got := g.Snapshot().FramePoolBytes; got != 0 {
		t.Fatalf("关闭竞态后 gauge = %d, want 0", got)
	}
	if st := pool.stats(); st.Lent != 0 || st.Allocated != 0 || st.Waiters != 0 {
		t.Fatalf("关闭竞态后池里还有账: %+v", st)
	}
	waitForGoroutines(t, base)
}

func TestDiagnosticSnapshotSeesTheProcessFramePoolGauge(t *testing.T) {
	base := baselineGoroutines()
	// 不传 Gauges：走进程级的那一份，#668 的 TakeSnapshot 读它。
	s := Start(Config{
		Fetch:          fetcher(fixture(t)),
		Open:           syntheticStream{w: 320, h: 180, count: 20, holdEvery: 1}.opener(),
		TempDir:        t.TempDir(),
		FramePoolBytes: 4 * 320 * 180 * 4,
	}, 0)
	waitUntil(t, "快照看到帧池占用", func() bool {
		s.Poll(50 * time.Millisecond)
		return perf.TakeSnapshot().Gauges.FramePoolBytes > 0
	})
	s.Close()
	if got := perf.TakeSnapshot().Gauges.FramePoolBytes; got != 0 {
		t.Fatalf("关闭后进程级 gauge = %d, want 0", got)
	}
	waitForGoroutines(t, base)
}

// TestFrameBudgetRejectsAbsurdResolution 覆盖「一帧就超过预算一半」的可诊断降级：
// 不是悄悄跳过，而是从 Session.Err 给出 ErrFramePoolBudget。
func TestFrameBudgetRejectsAbsurdResolution(t *testing.T) {
	base := baselineGoroutines()
	var g perf.Gauges
	dir := t.TempDir()
	// 预算按 1080p 两帧给，但解码器给的是 4K 帧：一帧 31.6MiB，预算放不下两帧。
	s := Start(Config{
		Fetch:          fetcher(fixture(t)),
		Open:           syntheticStream{w: 3840, h: 2160, count: 3}.opener(),
		TempDir:        dir,
		FramePoolBytes: 2 * 1920 * 1080 * 4,
		Gauges:         &g,
	}, 0)
	waitUntil(t, "预算错误报上来", func() bool { return errors.Is(s.Err(), ErrFramePoolBudget) })
	s.Close()
	if got := g.Snapshot().FramePoolBytes; got != 0 {
		t.Fatalf("预算错误关闭后 gauge = %d, want 0", got)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("预算错误后临时目录 = %v, err=%v", entries, err)
	}
	waitForGoroutines(t, base)
}

// TestSessionStopsOnContextCancel 覆盖「context 取消」这条路径：Close 会取消
// context，取流和解码 goroutine 都必须退出，帧账也必须回零。
func TestSessionStopsOnContextCancel(t *testing.T) {
	base := baselineGoroutines()
	var g perf.Gauges
	fetch := func(ctx context.Context, from, to int64) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	s := Start(Config{Fetch: fetch, TempDir: t.TempDir(), Gauges: &g}, 0)
	waitUntil(t, "取流 goroutine 起来", func() bool { return runtime.NumGoroutine() > base })
	s.Close()
	if got := g.Snapshot().FramePoolBytes; got != 0 {
		t.Fatalf("取消后 gauge = %d, want 0", got)
	}
	waitForGoroutines(t, base)
}
