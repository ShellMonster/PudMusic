package audio

import (
	"errors"
	"io"
	"sync"
	"testing"
	"time"

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
	reads      int
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
	// ⚠️ **每读一段换一个幅度**，于是每一段的谱都不一样。
	//
	// 不这么做的话「这一段的内容被后来那一段覆盖了」是**量不到的**：常数信号的
	// 每一段算出来一模一样，重写了也看不出来（而「复用同一块缓冲」这个坑正是
	// 内容被改掉）。
	d.reads++
	d.value = 0.01 + 0.005*float32(d.reads)
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

// gatedDec 是「第一次读要等放行」的假解码器。
//
// 用来把「解码器开始读」和「音量已经落下来」这两件事**排成确定的顺序**：
// 播放器建好之后它自己那条解码 goroutine 就开始读了（`mygo/audio/source.go:82`
// 的 `go s.decode()`，它在 `NewPlayer` 里就起了），而那一读会把**当时的**音量
// 乘进这一段谱里（`tap.Read` 里 `t.an.SetVolume` 就在 `push` 之前）。
//
// 于是「建播放器」和「把音量设成 0」这两句谁先谁后，就成了那条测试绿还是红的
// **唯一**原因：主 goroutine 让出 CPU 的那一瞬间，解码 goroutine 可能已经算出
// 好几段满格的了。实测大约 100 次里飘 1~3 次。
//
// 放行之后读到的每一段都是按**放行那一刻**的音量算的，而那一刻音量早就落下
// 来了——于是这一条量的是「解出来了但是静音」这件事本身，而不是「谁先跑到」。
type gatedDec struct {
	*fakeDec
	release chan struct{}
	once    sync.Once
}

func newGatedDec(rate, ch int, frames int64) *gatedDec {
	return &gatedDec{fakeDec: newFakeDec(rate, ch, frames), release: make(chan struct{})}
}

// open 放行解码器（幂等，收尾时也拿它兜底）。
func (d *gatedDec) open() { d.once.Do(func() { close(d.release) }) }

// Read 先等放行，再读。
//
// ⚠️ **卡住的是「解码器开始读」这一步，不是数据本身**：放行之后读到的每一段
// 都是按当时的音量算的。
func (d *gatedDec) Read(buf []float32) (int, error) {
	<-d.release
	return d.fakeDec.Read(buf)
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

// TestADroppedBlocksSpectrumIsNeverOverwritten 断「结果缓冲一段一份，不回收」。
//
// 回收在性能上划算，但它是**假的**：界面上 `At(frame)` 拿到的是那个切片，解锁
// 之后才去画，而下一段可能正在写它。那是一条真的数据竞争，而且极难复现——
// 两件事隔着一帧的几微秒，一秒二十三次里碰上一次就红一次。
//
// 这一条量的就是那件事本身：拿一份引用（界面那一帧正是这么拿着它的），让环
// 转好几圈把它**裁掉**，再看这份引用里的内容有没有被动过。
func TestADroppedBlocksSpectrumIsNeverOverwritten(t *testing.T) {
	d := newFakeDec(48000, 2, 48000*600)
	tp := newTap(d)

	// 四秒的历史在 48kHz 下是 192000 帧，2048 一段 → 最多 93 段。先喂 90 段
	// （184320 帧，还没到上限，所以第一段还在），再转五圈把它挤出去。
	for range 90 {
		feed(t, tp, 2048)
	}
	held := tp.At(0)
	if held == nil {
		t.Fatal("填满了环却取不到第一段")
	}
	before := append([]float32(nil), held...)

	for range 5 * 94 {
		feed(t, tp, 2048)
	}

	for i, v := range held {
		if v != before[i] {
			t.Fatalf("第 %d 格被改了：这一段早就被裁掉了，内容却被后面那一段覆盖掉", i)
		}
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
	if bars := p.Bars(0, 48, 40, make([]float32, 48)); len(bars) != 48 {
		t.Fatalf("Bars 给了 %d 根，want 48", len(bars))
	}
}

// TestBarsWithNothingDecodedYetAreZero 断「还没解出那一段时给的是一排 0」。
//
// 和「解出来是静音」**不一样**：静音要留那条 2 像素的底排（Biu 那句
// 「show a base row like PotPlayer」），什么数据都还没有时给 0——那时连
// 「这有多高」都说不出来。
//
// ⚠️ 这里手搓一个 Player 而不是调 `New`：`New` 会起一条解码 goroutine，它会去读
// 整条链，而这一条要断的正是「链上一个数据都还没有」——那个状态只在**没人读**
// 的时候存在。
func TestBarsWithNothingDecodedYetAreZero(t *testing.T) {
	tp := newTap(newFakeDec(48000, 2, 4800))
	p := &Player{tap: tp}

	bars := p.Bars(0, 48, 40, make([]float32, 48))
	if len(bars) != 48 {
		t.Fatalf("Bars 给了 %d 根，want 48", len(bars))
	}
	for i, v := range bars {
		if v != 0 {
			t.Fatalf("一个数据都还没有，第 %d 根却是 %v（want 0，不是那条 2 像素的底排）", i, v)
		}
	}
}

// TestTheBarsAreTwoPixelsTallWhenSilent 是「2 像素基线」在真链路上的样子。
//
// 底层解码器给的是一段满格常数信号，音量 0：所有格都是 0，于是每一根条只剩
// 那条 2 像素的底排。
//
// ⚠️ **不要在这一侧自己 feed**：这一条用 `New` 建了播放器，而播放器自己有一条
// 解码 goroutine 在读同一条链。挂点**不是**并发安全的（一个解码 goroutine 一个
// 挂点，这是它能免锁的前提），两处一起读就是 `-race` 直接报——而且报的栈指向
// `tap_test.go` 自己的 `feed`，看起来像生产代码有问题。
func TestTheBarsAreTwoPixelsTallWhenSilent(t *testing.T) {
	d := newGatedDec(48000, 2, 48000*3)
	p := New(d)
	t.Cleanup(p.Close)
	// ⚠️ **收尾一定先放行**（`t.Cleanup` 后进先出，所以这一句排在 `p.Close` 后面）：
	// 关一个还卡在 `Read` 里的解码器，那条解码 goroutine 就永远醒不过来。
	t.Cleanup(d.open)

	// ⚠️ **音量先落，再放行解码——这个顺序是这一条的全部。**
	//
	// 播放器建好之后它自己那条解码 goroutine 就开始读了（`mygo/audio/source.go:82`
	// 的 `go s.decode()`，在 `NewPlayer` 里就起了），而那一读会把**当时的**音量
	// 乘进这一段谱里（`tap.Read` 里 `t.an.SetVolume` 就在 `push` 之前）。
	// 不排这个顺序的话，这一格量到的可能是「音量还没设成 0 时算出来的那一段」——
	// 而 `At(0)` 永远返回**第一段**（它盖着源帧 0），于是绿还是红全看谁先跑到：
	// 实测大约 100 次里飘 1~3 次（主 goroutine 让出 CPU 的那一瞬间就够）。
	p.SetVolume(0)
	d.open()

	// 等**它自己**解出来。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p.tap.At(0) != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if p.tap.At(0) == nil {
		t.Fatal("两秒了挂点上一段都没有 —— 这一条量不到「解出来了但是静音」")
	}

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
