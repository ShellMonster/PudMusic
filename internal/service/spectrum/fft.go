// Package spectrum 把解码器吐出来的一段 PCM 变成频谱条。
//
// # 这一层不认识 MyGo，也不认识界面
//
// 它只有数学：radix-2 快速傅里叶变换 + 一个把幅度映射成 0~1 的表。解码器那一侧
// 的挂点在 `internal/service/audio/tap.go`，画条那一侧在 `internal/ui/fullplayer_spectrum.go`。
// 三段各自能单独测，才钉得住「频谱为什么不对」。
//
// # 抄的是 Biu 的哪一个
//
// `_research/biu/src/components/audio-waveform/index.tsx` 那一个 `AnalyserNode`：
//
//	fftSize = 512                       （第 37 行）→ 这里的 FFTSize
//	只画低频 60%                        （第 89 行）→ LowFraction
//	boost = 1 + i / barCount            （第 99 行）→ Bars 里那一句
//	保证 2 像素基线                     （第 104 行）→ BaseDip
//	默认色 #666                        （第 110 行）→ internal/ui 那边
//
// 浏览器那个 `AnalyserNode` 默认对每个 fftSize 的窗口加 Blackman 窗、
// 把 dB 从 [-100, -30] 线性映射到 0~255（`getByteFrequencyData`）。这两样也照抄：
// 少了窗，频谱的漏会宽一圈；少了那一段映射，「多响算满格」就和 Biu 对不上，
// 而那正是「默认色的频谱看起来该是什么样」这件事的基准。
//
// # 不抄的是「跨帧平滑」
//
// 浏览器 `AnalyserNode.smoothingTimeConstant` 默认 0.8，也就是上一帧和这一帧按
// 8:2 混。Biu 那一行没改过它，所以它是有平滑的。我们**不加**：规格点名的四个数
// （低频 60%、2 像素基线、boost、#666）照抄就够，多加一个没被要求的平滑只会让
// 「把它改掉这条就红」的断言写不出来。真机上看着闪再说。
package spectrum

import "math"

// FFTSize 是每次变换的点数。抄 Biu `audio-waveform/index.tsx:37` 的
// `analyser.fftSize = 512`。必须是 2 的幂——radix-2 就是靠这个。
const FFTSize = 512

// Bins 是一条谱的格数。实信号的前一半和后一半互为镜像，只有前一半是独立的
// （`AnalyserNode.frequencyBinCount` 报的也是 fftSize/2）。
const Bins = FFTSize / 2

// `getByteFrequencyData` 用的那一段 dB，也是 `AnalyserNode` 的默认值
// （minDecibels=-100、maxDecibels=-30）。
const (
	minDB = -100.0
	maxDB = -30.0
)

// FFT 是一次 radix-2 快速傅里叶变换（Cooley–Tukey，先位反转再逐层蝶形）。
//
// 512 点在 O(n²) 下是 26 万次复数乘法，在 radix-2 下是 512/2×9 = 2304 次蝶形。
// 解码器每秒吐 23 段，一秒不到六万次——不做 FFT 优化也不会有人听出来，
// 但这条 FFT 一秒钟要跑二十三次，所以还是拿真的 radix-2。
type FFT struct {
	n   int
	rev []int     // 位反转表
	cos []float64 // 旋转因子的实部，cos(-2πk/n)
	sin []float64 // 旋转因子的虚部，sin(-2πk/n)
}

// NewFFT 建一个 n 点的变换器。n 必须是 2 的幂，否则直接 panic：
// 它是编译期就知道的常量（FFTSize），不是用户输入。
func NewFFT(n int) *FFT {
	if n <= 0 || n&(n-1) != 0 {
		panic("spectrum: FFT 的长度必须是 2 的幂")
	}
	f := &FFT{
		n:   n,
		rev: make([]int, n),
		cos: make([]float64, n/2),
		sin: make([]float64, n/2),
	}
	bits := 0
	for 1<<bits < n {
		bits++
	}
	for i := range f.rev {
		r := 0
		for b := 0; b < bits; b++ {
			if i&(1<<b) != 0 {
				r |= 1 << (bits - 1 - b)
			}
		}
		f.rev[i] = r
	}
	for k := range f.cos {
		ang := -2 * math.Pi * float64(k) / float64(n)
		f.cos[k], f.sin[k] = math.Cos(ang), math.Sin(ang)
	}
	return f
}

// Transform 就地把 re、im 变成它们的频谱。两个切片长度必须都是 n。
func (f *FFT) Transform(re, im []float64) {
	n := f.n
	// 位反转：把输入按位反过来放，之后每一层的蝶形才两两相邻。
	for i := range re {
		if j := f.rev[i]; i < j {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		half := size / 2
		step := n / size
		for start := 0; start < n; start += size {
			k := 0
			for i := start; i < start+half; i++ {
				c, s := f.cos[k], f.sin[k]
				ar, ai := re[i+half], im[i+half]
				tr := ar*c - ai*s
				ti := ar*s + ai*c
				re[i+half] = re[i] - tr
				im[i+half] = im[i] - ti
				re[i] += tr
				im[i] += ti
				k += step
			}
		}
	}
}

// Analyzer 把一段 PCM（**交织**的 float32，一帧 channels 个）变成 Bins 个 0~1。
//
// 它持有全部中间量，于是每帧 `Magnitudes` 不分配：解码器每秒调二十三次，
// 每秒分配二十三块一两千字节的临时数组是白烧的。
//
// 它**不是**并发安全的：一个 Analyzer 归一个解码器 goroutine 所有
// （`internal/service/audio` 那边就是这么装的）。
type Analyzer struct {
	fft    *FFT
	re, im []float64
	mono   []float64
	// window 是 Blackman 窗，`AnalyzerNode` 默认给每个 fftSize 的窗口加的就是它。
	window []float64
	vol    float32
}

// New 建一个分析器。vol 是当前音量增益，见 SetVolume。
func New(vol float32) *Analyzer {
	a := &Analyzer{
		fft:    NewFFT(FFTSize),
		re:     make([]float64, FFTSize),
		im:     make([]float64, FFTSize),
		mono:   make([]float64, FFTSize),
		window: blackman(FFTSize),
		vol:    vol,
	}
	return a
}

// SetVolume 设音量增益。频谱跟着音量缩放，见 tap.go 里那段说明。
func (a *Analyzer) SetVolume(v float32) { a.vol = v }

// blackman 是 Blackman 窗（N 点的标准三系数形式）。
func blackman(n int) []float64 {
	w := make([]float64, n)
	for i := range w {
		x := 2 * math.Pi * float64(i) / float64(n-1)
		w[i] = 0.42 - 0.5*math.Cos(x) + 0.1*math.Cos(2*x) - 0.05*math.Cos(3*x)
	}
	return w
}

// Magnitudes 取 samples 开头 FFTSize 帧，算成 Bins 个 0~1 的幅度。
//
// 交织的多个声道先混成一路（取平均）：频谱画的是一根条一个频段，
// 左右声道在那个尺度上本来就分不开，而分开算等于把条数翻倍。
//
// samples 不足 FFTSize 帧时**按没有更多样本算**（窗按比例缩到实际长度），
// 而不是补零：补零等于说「后面是静音」，频谱会塌成一条线，而歌在放的时候
// 读到的那一段几乎总是满的。
func (a *Analyzer) Magnitudes(samples []float32, channels int, out []float32) []float32 {
	if channels < 1 {
		channels = 1
	}
	if len(out) < Bins {
		out = make([]float32, Bins)
	}
	n := min(FFTSize, len(samples)/channels)
	if n < 2 {
		for i := range out[:Bins] {
			out[i] = 0
		}
		return out[:Bins]
	}

	for i := range n {
		sum := float64(0)
		base := i * channels
		for ch := 0; ch < channels; ch++ {
			sum += float64(samples[base+ch])
		}
		a.mono[i] = sum / float64(channels)
	}
	// 窗长跟着实际取的点数走：少取一段就把窗缩到那一段，幅度不会凭空小一截。
	warp := float64(FFTSize-1) / float64(n-1)
	for i := range n {
		a.re[i] = a.mono[i] * a.window[int(float64(i)*warp)]
		a.im[i] = 0
	}
	for i := n; i < FFTSize; i++ {
		a.re[i], a.im[i] = 0, 0
	}

	a.fft.Transform(a.re, a.im)

	// 单边谱：负频率那一半是镜像，只留一半，所以幅度翻倍补回来。
	inv := 1 / float64(n)
	scale := 2 * inv * float64(a.vol)
	for i := range Bins {
		mag := math.Hypot(a.re[i], a.im[i]) * scale
		out[i] = dbToUnit(mag)
	}
	return out[:Bins]
}

// dbToUnit 把一个幅度变成 0~1，映射那一段抄 `getByteFrequencyData` 的
// [minDecibels, maxDecibels]。
func dbToUnit(mag float64) float32 {
	if mag <= 0 || math.IsNaN(mag) {
		return 0
	}
	db := 20 * math.Log10(mag)
	v := (db - minDB) / (maxDB - minDB)
	return float32(math.Max(0, math.Min(1, v)))
}
