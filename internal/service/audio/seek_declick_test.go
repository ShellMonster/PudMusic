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
	p.seek.wait = func(d time.Duration) { rec.render(int(d.Seconds() * mixRate)) }

	p.play.Play()
	rec.pumpUntilSound(t)
	rec.render(mixRate / 50)
	for math.Abs(float64(rec.left[len(rec.left)-1])) < 0.45 {
		rec.render(1)
	}

	p.Seek(10 * time.Second)

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

// playingTone 起一个在放的播放器（混音器手动推进），返回它和录音机。
func playingTone(t *testing.T) (*Player, *recorder) {
	t.Helper()
	manualMixer(t)
	p := New(&toneDec{})
	t.Cleanup(p.Close)
	rec := &recorder{}
	p.play.Play()
	rec.pumpUntilSound(t)
	return p, rec
}

// TestStaleSeekIsDropped 锁住：等静音间隙时有更新的 Seek 顶上来，旧的那次
// 不落地，最终位置是新的那次，而且接着放。
func TestStaleSeekIsDropped(t *testing.T) {
	p, rec := playingTone(t)
	nested := false
	p.seek.wait = func(d time.Duration) {
		rec.render(int(d.Seconds() * mixRate))
		if !nested {
			nested = true
			p.Seek(20 * time.Second)
		}
	}
	p.Seek(10 * time.Second)
	if got := p.play.Position(); got < 20*time.Second || got > 21*time.Second {
		t.Errorf("Position = %v，want 新的那次（20s）而不是旧的（10s）", got)
	}
	if !p.Playing() {
		t.Error("被顶掉的 Seek 暂停了播放，却没有人接着放")
	}
}

// TestPauseDuringSeekGapStaysPaused 锁住：用户在 Seek 的静音间隙里按了暂停，
// 落地之后不许替他接着放。
func TestPauseDuringSeekGapStaysPaused(t *testing.T) {
	p, rec := playingTone(t)
	p.seek.wait = func(d time.Duration) {
		rec.render(int(d.Seconds() * mixRate))
		p.Pause()
	}
	p.Seek(10 * time.Second)
	if p.Playing() {
		t.Error("间隙里按了暂停，Seek 落地后又自己放起来了")
	}
}

// TestSeekWhilePausedIsImmediate 锁住：没在放时 Seek 不等间隙，也不开声。
func TestSeekWhilePausedIsImmediate(t *testing.T) {
	p, _ := playingTone(t)
	p.play.Pause()
	p.seek.wait = func(time.Duration) { t.Error("暂停中的 Seek 不该等间隙") }
	p.Seek(10 * time.Second)
	if p.Playing() {
		t.Error("暂停中 Seek 后开始放了")
	}
}
