package audio

import (
	"errors"
	"io"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/service/spectrum"
	mygoaudio "github.com/egoist/mygo/audio"
)

// 这一组测频谱挂点本身：**按播放时间取对应 PCM** 是 PLAN 阶段 6 风险表点名的
// 那一件（「`mygo/audio/source.go` 预解约一秒，最新解出来的样本未必是用户正在
// 听到的样本」）。
//
// 所以这里不测「有没有频谱」，测的是「**给定一个源帧号，能不能取到那一刻的那
// 一段**」。

// fakeDec 是一个可控的解码器：一段一整的常数信号，读到哪儿就给出什么。
type fakeDec struct {
	rate, ch   int
	total      int64
	pos        int64
	value      float32
	seekedTo   int64
	seekedOnce int
}

func newFakeDec(rate, ch int, frames int64) *fakeDec {
	return &fakeDec{rate: rate, ch: ch, total: frames, value: 0.05}
}

func (d *fakeDec) SampleRate() int { return d.rate }
func (d *fakeDec) Channels() int   { return d.ch }
func (d *fakeDec) Len() int64      { return d.total }

func (d *fakeDec) SetPosition(f int64) error {
	d.pos, d.seekedTo, d.seekedOnce = f, f, d.seekedOnce+1
	return nil
}

// Read 按框架那一套返回**采样数**（`mygo/audio/source.go` 是
// `n -= n % channels; samples := buf[:n]` 这么用的，返回帧数的那一半会被框架
// 当成采样数再截一次）。
func (d *fakeDec) Read(buf []float32) (int, error) {
	if d.pos >= d.total {
		return 0, io.EOF
	}
	n := len(buf)
	if left := int((d.total - d.pos) * int64(d.ch)); n > left {
		n = left
	}
	n -= n % d.ch
	for i := range n {
		buf[i] = d.value
	}
	d.pos += int64(n) / int64(d.ch)
	return n, nil
}

// feed 读 frames 帧，让挂点把这一段算进去。
func feed(t *testing.T, d mygoaudio.Decoder, frames int) {
	t.Helper()
	buf := make([]float32, frames*d.Channels())
	if _, err := d.Read(buf); err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("读 %d 帧：%v", frames, err)
	}
}

// TestTheTapIsTransparent 断「它只是看一眼」。
//
// 挂点套在链路中间，它多错一次就是用户听到的东西坏了：读的帧数、报的帧号、
// seek 到的位置，一样都不许差。
func TestTheTapIsTransparent(t *testing.T) {
	d := newFakeDec(48000, 2, 48000*3)
	tp := newTap(d)

	// Read 返回的是**采样**数（框架自己会再 `n -= n % channels`），所以比对的是
	// 「采样数 ÷ 声道数」。
	for _, want := range []int{2048, 2048, 512} {
		buf := make([]float32, want*2)
		n, err := tp.Read(buf)
		if err != nil {
			t.Fatalf("读 %d 帧：%v", want, err)
		}
		if n != want*2 {
			t.Errorf("挂点读回来 %d 个采样，want %d（%d 帧 × 2 声道）", n, want*2, want)
		}
	}
	if d.pos != 2048+2048+512 {
		t.Errorf("底层解码器只走到了第 %d 帧", d.pos)
	}
	if tp.SampleRate() != 48000 || tp.Channels() != 2 || tp.Len() != 48000*3 {
		t.Error("挂点没有把 SampleRate/Channels/Len 原样传上去")
	}
	if err := tp.SetPosition(1234); err != nil {
		t.Fatalf("跳位置：%v", err)
	}
	if d.seekedTo != 1234 {
		t.Errorf("seek 没有传到下一层，底层收到的目标帧是 %d，want 1234", d.seekedTo)
	}
}

// TestEachBlockIsTaggedWithItsSourceFrames 是「按播放时间取 PCM」的地基。
//
// 第 0 段盖 0~2047，第 1 段盖 2048~4095。取 0、2047 要拿到第 0 段，
// 取 2048、4095 要拿到第 1 段。
func TestEachBlockIsTaggedWithItsSourceFrames(t *testing.T) {
	d := newFakeDec(48000, 2, 48000*3)
	tp := newTap(d)

	feed(t, tp, 2048)
	first := tp.At(0)
	if first == nil {
		t.Fatal("第 0 帧取不到那一段")
	}
	feed(t, tp, 2048)
	second := tp.At(2048)
	if second == nil {
		t.Fatal("第 2048 帧取不到第二段")
	}
	if &first[0] == &second[0] {
		t.Error("两段拿到的是同一块缓冲")
	}

	// 段**之内**的任意一帧都要落在这一段上（不是只有段首才行）。
	for _, f := range []int64{0, 1023, 2047} {
		if got := tp.At(f); got == nil || &got[0] != &first[0] {
			t.Errorf("第 %d 帧拿到的不是它所在的那一段", f)
		}
	}
	for _, f := range []int64{2048, 3000, 4095} {
		if got := tp.At(f); got == nil || &got[0] != &second[0] {
			t.Errorf("第 %d 帧拿到的不是它所在的那一段", f)
		}
	}
	// 还没解到的那一帧：给**最新**的一段，而不是空。
	// （解码器领先放音约一秒，而播放位置正是在那一段里。）
	if got := tp.At(999999); got == nil || &got[0] != &second[0] {
		t.Error("问一个还没解到的位置时没有回最新的一段")
	}
	// 比留着的最老一段还早：回空。宁可画一条基线，也不要拿错的顶上。
	if got := tp.At(-1); got != nil {
		t.Error("问一个比历史还早的位置时给了结果")
	}
}

// TestSeekClearsTheHistory 断「拖完进度条频谱不会停在老地方」。
//
// 留着旧的段的话，seek 之后那一帧取到的还是 seek 之前那一段——
// 界面上是「拖完了频谱还停在老地方，一秒后才缓过来」。
func TestSeekClearsTheHistory(t *testing.T) {
	d := newFakeDec(48000, 2, 48000*30)
	tp := newTap(d)
	feed(t, tp, 2048)
	feed(t, tp, 2048)
	if tp.At(100) == nil {
		t.Fatal("喂了两段却取不到第一段")
	}

	if err := tp.SetPosition(48000 * 10); err != nil {
		t.Fatalf("跳位置：%v", err)
	}
	if got := tp.At(100); got != nil {
		t.Error("seek 之后老位置还能取到东西")
	}
	// 新的段按**新的起点**标：seek 到 10 秒，读一段，它就该盖 480000 起的那些帧。
	feed(t, tp, 2048)
	if got := tp.At(480000); got == nil {
		t.Error("seek 之后新读的那一段没有按新位置标上")
	}
	if got := tp.At(0); got != nil {
		t.Error("seek 之后老位置又能取到东西了")
	}
}

// TestTheRingKeepsAboutFourSeconds 是「为什么留四秒」那条。
//
// MyGo 领先放音约一秒，2 倍速时那是两秒的**源**，加上分析窗和块与块之间的空当。
// 留不够的后果不是画错一条频谱，是**整个环被冲掉**：要画的那一段已经被挤出去了。
func TestTheRingKeepsAboutFourSeconds(t *testing.T) {
	d := newFakeDec(48000, 2, 48000*300)
	tp := newTap(d)

	// 多喂两段，让环真的转起来——「刚好四秒」那一刻最老的那一段还在。
	const block = 2048
	for range 4*48000/block + 2 {
		feed(t, tp, block)
	}
	// 四秒之前的那一段已经被挤出去了。
	if got := tp.At(0); got != nil {
		t.Error("四秒以前的位置还能取到东西，环留得太长")
	}
	// 刚过去两秒的那一段还在（这正是 2 倍速下要取的那一段）。
	if got := tp.At(2 * 48000); got == nil {
		t.Error("两秒以前的位置取不到了，环留得太短（2 倍速时要取的就是这一段）")
	}
	if got := tp.At(4*48000 - 1); got == nil {
		t.Error("刚刚那一段取不到")
	}
}

// TestTheTapFollowsTheVolume 是 PLAN 阶段 6 风险表点名的另一条。
func TestTheTapFollowsTheVolume(t *testing.T) {
	d := newFakeDec(48000, 1, 48000*3)
	tp := newTap(d)
	tp.SetVolume(1)
	feed(t, tp, 2048)
	loud := tp.At(0)[spectrum.Bins/4]

	tp.SetVolume(0)
	feed(t, tp, 2048)
	quiet := tp.At(2048)[spectrum.Bins/4]

	if quiet != 0 {
		t.Errorf("音量 0 时那一格还是 %v（loud 是 %v）", quiet, loud)
	}
}

// TestThePlayerAsksTheTapForTheAudibleFrame 是从挂点到界面的那一段接线。
//
// 界面上「此刻在放的是哪一帧」走的是 `Player.AudibleFrame`，它必须和进度条用的
// 那一条路是同一份——不然频谱和进度条各说各话（进度条在第 40 秒、频谱画的是
// 第 35 秒的声音）。
func TestThePlayerAsksTheTapForTheAudibleFrame(t *testing.T) {
	p := New(newFakeDec(48000, 2, 48000*3))
	t.Cleanup(p.Close)

	if p.tap == nil {
		t.Fatal("New 没有把频谱挂点装上")
	}
	if _, ok := p.vs.inner.(*tap); !ok {
		t.Fatalf("变速层底下不是频谱挂点，是 %T —— 挂点得在**变速之前**（tap.go 的顶上）",
			p.vs.inner)
	}
	// 没放音的时候取到的是一排 0，而不是空切片：画出来是一条基线。
	bars := p.Bars(0, 48, 40, make([]float32, 48))
	if len(bars) != 48 {
		t.Fatalf("Bars 给了 %d 根，want 48", len(bars))
	}
	for i, v := range bars {
		if v != 0 {
			t.Fatalf("没在放的时候第 %d 根是 %v，want 0", i, v)
		}
	}
}

// TestTheBarsAreTwoPixelsTallWhenSilent 是「2 像素基线」在真链路上的样子。
//
// 底层解码器给的是一段满格常数信号，音量 0：所有格都是 0，于是每一根条只剩
// 那条 2 像素的底排。
func TestTheBarsAreTwoPixelsTallWhenSilent(t *testing.T) {
	d := newFakeDec(48000, 2, 48000*3)
	p := New(d)
	t.Cleanup(p.Close)
	p.SetVolume(0)

	// 直接把 tap 喂满，让挂点里有几段。
	p.tap.SetVolume(0)
	feed(t, p.tap, 2048)

	const height, n = float32(40), 48
	bars := p.Bars(0, n, height, make([]float32, n))
	if len(bars) != n {
		t.Fatalf("Bars 给了 %d 根，want %d", len(bars), n)
	}
	for i, v := range bars {
		if v != spectrum.BaseDip/height {
			t.Fatalf("第 %d 根是 %v，want %v（2 像素那条底排）", i, v, spectrum.BaseDip/height)
		}
	}
}
