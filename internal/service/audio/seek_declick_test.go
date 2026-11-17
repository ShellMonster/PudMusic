package audio

import (
	"io"
	"math"
	"sync"
	"testing"
	"time"
	_ "unsafe"
)

// 直接驱动 MyGo 的混音器：它在测试里由测试手动 mix（见
// mygo/audio/audio_test.go 的 setup / render），不开系统输出设备。
//
//go:linkname mygoMix github.com/egoist/mygo/audio.mix
func mygoMix(out []float32) bool

//go:linkname mygoTestMode github.com/egoist/mygo/audio.testMode
var mygoTestMode bool

//go:linkname mygoMu github.com/egoist/mygo/audio.mu
var mygoMu sync.Mutex

const mixRate = 48000

// toneDec 是 440Hz 正弦，相位由帧号决定：seek 到别处，波形在那一帧就接不上。
type toneDec struct {
	mu  sync.Mutex
	pos int64
}

func (d *toneDec) SampleRate() int { return mixRate }
func (d *toneDec) Channels() int   { return 2 }
func (d *toneDec) Len() int64      { return mixRate * 60 }
func (d *toneDec) SetPosition(f int64) error {
	d.mu.Lock()
	d.pos = f
	d.mu.Unlock()
	return nil
}
func (d *toneDec) Read(buf []float32) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.pos >= d.Len() {
		return 0, io.EOF
	}
	n := len(buf) &^ 1
	for i := 0; i < n; i += 2 {
		v := float32(0.5 * math.Sin(2*math.Pi*440*float64(d.pos)/mixRate))
		buf[i], buf[i+1] = v, v
		d.pos++
	}
	return n, nil
}

// recorder 把混音器的输出（左声道）连续记下来。
type recorder struct{ left []float32 }

func (r *recorder) render(frames int) {
	out := make([]float32, frames*2)
	mygoMix(out)
	for i := 0; i < len(out); i += 2 {
		r.left = append(r.left, out[i])
	}
}

// maxStep 是相邻两个输出样本之差的最大值，和它出现的位置。
func (r *recorder) maxStep() (float64, int) {
	var best float64
	at := 0
	for i := 1; i < len(r.left); i++ {
		if d := math.Abs(float64(r.left[i] - r.left[i-1])); d > best {
			best, at = d, i
		}
	}
	return best, at
}

// pumpUntilSound 一块一块地混，直到输出里出现声音（解码 goroutine 异步填环）。
func (r *recorder) pumpUntilSound(t *testing.T) {
	t.Helper()
	waitUntil(t, 5*time.Second, func() bool {
		start := len(r.left)
		r.render(mixRate / 200)
		for _, v := range r.left[start:] {
			if v != 0 {
				return true
			}
		}
		return false
	})
}

// manualMixer 让混音器由测试手动推进，收尾时还原。
func manualMixer(t *testing.T) {
	t.Helper()
	mygoMu.Lock()
	mygoTestMode = true
	mygoMu.Unlock()
	unmuteForTest = true
	t.Cleanup(func() {
		unmuteForTest = false
		mygoMu.Lock()
		mygoTestMode = false
		mygoMu.Unlock()
	})
}

// seekedTone 在播放中 seek 一次，返回整段输出里相邻样本之差的最大值。
func seekedTone(t *testing.T) float64 {
	t.Helper()
	manualMixer(t)

	p := New(&toneDec{})
	defer p.Close()
	rec := &recorder{}
	clk := newSeekClock(p, rec)

	p.play.Play()
	rec.pumpUntilSound(t)
	rec.render(mixRate / 50)
	for math.Abs(float64(rec.left[len(rec.left)-1])) < 0.45 {
		rec.render(1)
	}

	p.Seek(10 * time.Second)
	clk.advance(seekGap)

	rec.pumpUntilSound(t)
	rec.render(mixRate / 50)

	step, at := rec.maxStep()
	t.Logf("max sample step %.4f at index %d of %d", step, at, len(rec.left))
	return step
}

// TestSeekWhilePlayingHasNoDiscontinuity 锁住：播放中 seek 时，混音器输出里
// 相邻样本之差不超过正弦自己的最大斜率的几倍。
//
// 曾经出错的行为：Seek 直接 reset 混音器的声部，5ms 的淡入淡出只管音量和暂停，
// 管不到 reset，输出在一个样本之内从原波形跳到静音或新位置的波形（咔声）。
func TestSeekWhilePlayingHasNoDiscontinuity(t *testing.T) {
	natural := 0.5 * 2 * math.Pi * 440 / mixRate
	if step := seekedTone(t); step > 3*natural {
		t.Fatalf("seek 处最大样本跳变 %.4f，自然斜率 %.4f 的 %.1f 倍", step, natural, step/natural)
	}
}

// seekClock 是注入给 seekState.after 的手动时钟：只有 advance 才会让间隙
// 过去，期间混音器照真实时长推进（淡出走完才算数）。
type seekClock struct {
	rec     *recorder
	pending []*seekTimer
}

type seekTimer struct {
	left    time.Duration
	f       func()
	stopped bool
}

func newSeekClock(p *Player, rec *recorder) *seekClock {
	c := &seekClock{rec: rec}
	p.seek.after = func(d time.Duration, f func()) func() {
		tm := &seekTimer{left: d, f: f}
		c.pending = append(c.pending, tm)
		return func() { tm.stopped = true }
	}
	return c
}

func (c *seekClock) advance(d time.Duration) {
	c.rec.render(int(d.Seconds() * mixRate))
	for _, tm := range c.pending {
		if tm.stopped || tm.left <= 0 {
			continue
		}
		if tm.left -= d; tm.left <= 0 {
			tm.f()
		}
	}
}

// playingTone 起一个在放的播放器（混音器手动推进），返回它、录音机和时钟。
func playingTone(t *testing.T) (*Player, *recorder, *seekClock) {
	t.Helper()
	manualMixer(t)
	p := New(&toneDec{})
	t.Cleanup(p.Close)
	rec := &recorder{}
	clk := newSeekClock(p, rec)
	p.play.Play()
	rec.pumpUntilSound(t)
	return p, rec, clk
}

// TestSeekWhilePlayingDoesNotBlock 锁住：播放中 Seek 立刻返回、时钟没走，
// 此时还没落地；时钟走完 seekGap 才落地并接着放。
// 曾经出错的行为：Seek 在界面 goroutine 里睡 30ms，连发的键盘 seek 掉帧。
func TestSeekWhilePlayingDoesNotBlock(t *testing.T) {
	p, _, clk := playingTone(t)
	before := p.play.Position()
	p.Seek(10 * time.Second)
	if got := p.play.Position(); got >= 5*time.Second {
		t.Fatalf("时钟没走 Seek 就落地了：%v（之前 %v）", got, before)
	}
	clk.advance(seekGap)
	if got := p.play.Position(); got < 10*time.Second || got > 11*time.Second {
		t.Errorf("落地位置 %v，want 10s 附近", got)
	}
	if !p.Playing() {
		t.Error("落地后没有接着放")
	}
}

// TestSeekGapReportsUserView 锁住：间隙里 Position 是目标、Playing 是 true，
// 界面不会回弹也不会把这 30ms 当成停了。
func TestSeekGapReportsUserView(t *testing.T) {
	p, _, _ := playingTone(t)
	p.Seek(10 * time.Second)
	if got := p.Position(); got != 10*time.Second {
		t.Errorf("间隙里 Position = %v，want 目标 10s", got)
	}
	if !p.Playing() {
		t.Error("间隙里 Playing = false，tick 会误判成停了")
	}
}

// TestPlayDuringSeekGapWaitsForLanding 锁住：ScrubEnd 在 Seek 之后马上 Play，
// 不能在落地之前把声音放出来，只是记下落地后要放。
func TestPlayDuringSeekGapWaitsForLanding(t *testing.T) {
	p, _, clk := playingTone(t)
	p.Seek(10 * time.Second)
	p.Play()
	if p.play.Playing() {
		t.Error("间隙里 Play 提前放起来了")
	}
	clk.advance(seekGap)
	if !p.play.Playing() || p.play.Position() < 10*time.Second {
		t.Errorf("落地后 playing=%v pos=%v", p.play.Playing(), p.play.Position())
	}
}

// TestStaleSeekIsDropped 锁住：间隙里更新的 Seek 顶上来，只落地新的那次，
// 并且接着放。
func TestStaleSeekIsDropped(t *testing.T) {
	p, _, clk := playingTone(t)
	p.Seek(10 * time.Second)
	p.Seek(20 * time.Second)
	clk.advance(seekGap)
	if got := p.play.Position(); got < 20*time.Second || got > 21*time.Second {
		t.Errorf("Position = %v，want 新的那次（20s）而不是旧的（10s）", got)
	}
	if !p.Playing() {
		t.Error("被顶掉的 Seek 暂停了播放，却没有人接着放")
	}
}

// TestPauseDuringSeekGapStaysPaused 锁住：用户在 Seek 的静音间隙里按了暂停，
// 落地之后不许替他接着放，但位置仍然落到目标。
func TestPauseDuringSeekGapStaysPaused(t *testing.T) {
	p, _, clk := playingTone(t)
	p.Seek(10 * time.Second)
	p.Pause()
	if p.Playing() {
		t.Error("间隙里按暂停之后 Playing 还是 true")
	}
	clk.advance(seekGap)
	if p.Playing() {
		t.Error("间隙里按了暂停，Seek 落地后又自己放起来了")
	}
	if got := p.play.Position(); got < 10*time.Second {
		t.Errorf("暂停后落地位置 %v，want >= 10s", got)
	}
}

// TestCloseDuringSeekGap 锁住：间隙里 Close，定时器到了也不落地、不 panic，
// 之后的 Seek 也是空操作。
func TestCloseDuringSeekGap(t *testing.T) {
	p, _, clk := playingTone(t)
	p.Seek(10 * time.Second)
	p.Close()
	clk.advance(seekGap)
	p.Seek(5 * time.Second)
	if p.seek.pending {
		t.Error("Close 之后 seek 还挂着")
	}
}

// TestSeekWhilePausedIsImmediate 锁住：没在放时 Seek 不等间隙，也不开声。
func TestSeekWhilePausedIsImmediate(t *testing.T) {
	p, _, _ := playingTone(t)
	p.play.Pause()
	p.Seek(10 * time.Second)
	if p.seek.pending {
		t.Error("暂停中的 Seek 不该等间隙")
	}
	if p.Playing() {
		t.Error("暂停中 Seek 后开始放了")
	}
}
