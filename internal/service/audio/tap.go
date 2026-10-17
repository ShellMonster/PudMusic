// 解码器上的频谱挂点。
//
// # 挂在哪儿：解码器**输出**这一头，变速层**之前**
//
// 整条链路是：`closingDecoder → errorDecoder → tap → varispeed → mygoaudio.Player`。
// tap 夹在中间，看到的是**源 PCM**（WSOLA 之前）。
//
// 为什么不是变速之后：变速层吐出来的帧和源帧**不是一回事**。它的 `Read` 每吐一
// 跳，源就走 `hop × 倍率` 帧（`rate.go` 的类型注释），所以在那一头数「已经读了
// 多少帧」数出来的不是歌里的位置——2 倍速下它比歌里的时间快一倍。而我们要做的
// 恰恰是「按播放时间取对应 PCM」（PLAN 阶段 6 的风险表点名的那一条）：挂在
// 这一头，每一段都能用一个**源帧号**标死，「此刻听到的」就是「源帧号等于播放
// 位置的那一段」，1 倍速、2 倍速、seek 之后、暂停之后都成立。
//
// 代价说清楚：WSOLA 把源在时间上拉长/压短，所以 2 倍速下频谱**变化**比 Biu 慢
// （Biu 的 analyser 挂在 `<audio>` 之后，看的是变速之后的信号）。同一时刻的
// 频谱形状是一样的——WSOLA 逐跳保留短时频谱——只是条子动得快慢差一点。
//
// # 为什么每一段都算好、不留 PCM
//
// MyGo 的 `stream` 会比放音**领先约一秒**地解（`mygo/audio/source.go:76-78`
// 「About a second ahead」），倍率越大它领先的**源**时间越长（2 倍速就是两秒的
// 源）。要「按播放时间取 PCM」就得留一段历史：留着 PCM 是几 MB，留着**算好的
// 一条谱**只有几百 KB（一段 256 个 float32）。所以 tap 每读一段就算一次 FFT，
// 存下来的只有结果。
//
// # 为什么每一段要标源帧号
//
// 没有源帧号，「此刻听到哪一段」就只能靠「最新那一段」——那是一秒前的音乐，
// 鼓点会整整慢一拍。所以每段都记下它覆盖的源帧区间，取的时候按帧号找。
package audio

import (
	"io"
	"math"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/ShellMonster/Biu_Go/internal/service/spectrum"
	mygoaudio "github.com/egoist/mygo/audio"
)

// tapHistorySeconds 是留多少秒的历史。
//
// 取 4 秒：MyGo 领先约一秒的**输出**，而倍率最大 2 倍时那是两秒的**源**，加上
// 分析窗本身要 512 帧（10 毫秒）和段与段之间的空当，3 秒已经贴边，4 秒留点余量。
const tapHistorySeconds = 4

// spectrumBlock 是一段 PCM 算出来的一条谱，标着它覆盖的**源帧**区间。
type spectrumBlock struct {
	from, to int64
	mag      []float32
}

// tap 是套在解码器外面的那一层。它只是**看**一眼：Read 的返回值原样往上传，
// SetPosition 原样往下传，频谱算砸了也只是一片零。
type tap struct {
	mygoaudio.Decoder

	an *spectrum.Analyzer
	// vol 是音量增益：设置走界面线程、读取走解码 goroutine，所以用 atomic。
	vol atomic.Uint32
	// out 是分析器的输出缓冲。**只有解码 goroutine 碰它**，所以不加锁；
	// 环里每段自己有一份 copy，这个要是也指向环里的某一段就全乱了。
	out []float32

	mu sync.Mutex
	// blocks 是历史，按 from **递增**排着；frames 是它们一共盖了多少帧。
	//
	// 为什么按**帧数**裁而不是按段数：一段多少帧不是我们说了算（MyGo 的 stream 给
	// 2048，别的不一定），按段数留的话「留四秒」这件事会随格式变。
	blocks []spectrumBlock
	frames int64
	cur    int64 // 下一段从第几源帧开始
	// spare 是刚被裁掉的那一段的 mag 切片，下一段接着用。稳态于是不分配。
	spare []float32
}

// newTap 在 d 外面套一层频谱挂点。
func newTap(d mygoaudio.Decoder) *tap {
	t := &tap{Decoder: d, an: spectrum.New(1), out: make([]float32, spectrum.Bins)}
	t.vol.Store(math.Float32bits(1))
	return t
}

// SetVolume 设音量增益，让频谱跟着音量缩放。
//
// PLAN 阶段 6 的风险表点名了这一条：「用户把音量拉到 0，Biu 的频谱会变矮，
// 我们的不会变」——Biu 的 analyser 挂在元素之后，拿得到音量。我们挂在解码器上，
// 拿不到，所以在这里手动乘一次。
func (t *tap) SetVolume(v float32) { t.vol.Store(math.Float32bits(v)) }

// Read 读一段，顺手算一条谱记进历史。
//
// ⚠️ `Decoder.Read` 返回的是**写进 buf 的采样数**，不是帧数（`mygo/audio/source.go`
// 的 `n -= n % s.channels; samples := buf[:n]` 就是这么用的）。所以帧数要自己
// 除以声道数。
func (t *tap) Read(buf []float32) (int, error) {
	n, err := t.Decoder.Read(buf)
	if n > 0 {
		ch := max(t.Decoder.Channels(), 1)
		if frames := int64(n / ch); frames > 0 {
			t.an.SetVolume(math.Float32frombits(t.vol.Load()))
			t.push(buf[:n], frames, ch)
		}
	}
	return n, err
}

// Close 转发给内层解码器，没有就什么都不做。
//
// ⚠️ **不能省**：挂点的 `Decoder` 是一个**接口**字段，方法不会被提升（和
// `errorDecoder` 上那段是同一个坑，`handles_test.go` 记着它）。
// 少这一句的后果不是「关得慢一点」——关解码器是顺着链一路往里传的，挂点这里
// 断掉之后，`closingDecoder` 再也没人调，AudioToolbox 的句柄就一直留着：
// 播放照常、fd 数也不涨，切到几百首才崩（实测 `TestOpenAnMP4ReleasesTheAudioToolboxHandle`
// 直接红）。
func (t *tap) Close() error {
	if c, ok := t.Decoder.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

var _ mygoaudio.Decoder = (*tap)(nil)
var _ io.Closer = (*tap)(nil)

// SetPosition 跳到源第 frame 帧。**顺带把历史清空**。
//
// 清空是必须的：seek 之后旧的段还标着老位置的帧号，留着的话「按播放时间取」
// 会取到 seek 之前那一段——表现出来是拖完进度条频谱还停在老地方，一秒后才自己
// 缓过来。清掉之后取不到的那一瞬间返回空，频谱塌成一条基线，就一帧的事。
func (t *tap) SetPosition(frame int64) error {
	err := t.Decoder.SetPosition(frame)
	t.mu.Lock()
	t.cur = frame
	t.blocks, t.frames = nil, 0
	t.mu.Unlock()
	return err
}

// maxFrames 是历史最多留多少帧。
func (t *tap) maxFrames() int64 {
	rate := t.Decoder.SampleRate()
	if rate <= 0 {
		rate = 48000
	}
	return int64(tapHistorySeconds) * int64(rate)
}

// push 把刚读到的一段算成谱，记进历史，必要时把最老的挤出去。
func (t *tap) push(samples []float32, frames int64, channels int) {
	mag := t.an.Magnitudes(samples, channels, t.out)
	from := t.cur

	t.mu.Lock()
	defer t.mu.Unlock()
	t.cur = from + frames

	// 目标缓冲优先用刚被裁掉的那一段的：稳态下一段都不分配。
	dst := t.spare
	t.spare = nil
	if cap(dst) < spectrum.Bins {
		dst = make([]float32, spectrum.Bins)
	}
	dst = dst[:spectrum.Bins]
	copy(dst, mag)

	t.blocks = append(t.blocks, spectrumBlock{from: from, to: from + frames, mag: dst})
	t.frames += frames
	for len(t.blocks) > 1 && t.frames > t.maxFrames() {
		dropped := t.blocks[0]
		copy(t.blocks, t.blocks[1:])
		t.blocks[len(t.blocks)-1] = spectrumBlock{}
		t.blocks = t.blocks[:len(t.blocks)-1]
		t.frames -= dropped.to - dropped.from
		t.spare = dropped.mag
	}
}

// At 返回源帧 frame 那一刻的那条谱。
//
// 取的是**盖住** frame 的那一段（最后一段 from ≤ frame 的）：分析窗取的是段
// 开头那一批样本，所以它标的就是段首，不是段中。
//
// 找不到（比留着的最老一段还早、或者刚 seek 完还没解出来）时返回 nil——界面那一帧
// 画成一条基线，而不是拿一个不知道什么时候的旧频谱顶着。
func (t *tap) At(frame int64) []float32 {
	t.mu.Lock()
	defer t.mu.Unlock()
	i := sort.Search(len(t.blocks), func(i int) bool { return t.blocks[i].from > frame })
	if i == 0 {
		// 比留着的最老一段还早：还没有那一段，返回空而不是拿错的顶上。
		return nil
	}
	return t.blocks[i-1].mag
}

// Bars 是界面上真正要的那一样：**源帧 frame** 那一刻的频谱条（0~1 的高度比例）。
//
// 全程复用传进来的 dst，所以界面那一帧不分配。
func (p *Player) Bars(frame int64, barCount int, heightDip float32, dst []float32) []float32 {
	if p == nil || p.tap == nil || barCount <= 0 || len(dst) < barCount {
		return dst[:0]
	}
	mag := p.tap.At(frame)
	if mag == nil {
		clear(dst[:barCount])
		return dst[:barCount]
	}
	return spectrum.Bars(dst, mag, barCount, heightDip)
}

// AudibleFrame 是「此刻正在放的那一帧」在**源**里的编号。
//
// 走的是 `Position` 用的同一条路（吐出去的帧 → `varispeed.SourceAt`），所以它和
// 进度条上那个位置永远是同一个数——频谱和进度条不会各说各话。
func (p *Player) AudibleFrame() int64 {
	rate := p.vs.SampleRate()
	if rate <= 0 {
		return 0
	}
	played := p.play.Position()
	return p.vs.SourceAt(int64(math.Round(played.Seconds() * float64(rate))))
}
