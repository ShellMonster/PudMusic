package audio

import (
	"errors"
	"io"
	"math"
	"os"
)

// 响度测量：ITU-R BS.1770 / EBU R128 的积分响度（单位 LUFS）。
//
// 流程：K 加权（一级高频搁架 + 一级高通）→ 400ms 的块（75% 重叠，也就是每 100ms
// 一块）算平均功率 → 绝对门限 -70 LUFS → 相对门限（先前平均再 -10 LU）→ 过门限的块
// 取平均。声道权重：所有声道按 1 算（立体声就是标准的；多声道环绕的 1.41
// 权重没做，应用里放的都是立体声）。

// Loudness 是一首歌测出来的响度。
type Loudness struct {
	// LUFS 是积分响度。
	LUFS float64
	// Peak 是采样峰值（0..1，不是真峰值）。
	Peak float64
}

type biquad struct {
	b0, b1, b2, a1, a2 float64
	z1, z2             float64
}

func (q *biquad) step(x float64) float64 {
	y := q.b0*x + q.z1
	q.z1 = q.b1*x - q.a1*y + q.z2
	q.z2 = q.b2*x - q.a2*y
	return y
}

// kWeighting 返回某个采样率下的两级 K 加权滤波器系数（libebur128 的算法）。
func kWeighting(fs float64) (shelf, hp biquad) {
	f0, g, q := 1681.974450955533, 3.999843853973347, 0.7071752369554196
	k := math.Tan(math.Pi * f0 / fs)
	vh := math.Pow(10, g/20)
	vb := math.Pow(vh, 0.4996667741545416)
	a0 := 1 + k/q + k*k
	shelf = biquad{
		b0: (vh + vb*k/q + k*k) / a0,
		b1: 2 * (k*k - vh) / a0,
		b2: (vh - vb*k/q + k*k) / a0,
		a1: 2 * (k*k - 1) / a0,
		a2: (1 - k/q + k*k) / a0,
	}
	f0, q = 38.13547087602444, 0.5003270373238773
	k = math.Tan(math.Pi * f0 / fs)
	a0 = 1 + k/q + k*k
	hp = biquad{b0: 1, b1: -2, b2: 1, a1: 2 * (k*k - 1) / a0, a2: (1 - k/q + k*k) / a0}
	return
}

type loudnessMeter struct {
	ch      int
	sub     int // 100ms 一份有多少帧
	shelf   []biquad
	hp      []biquad
	acc     float64 // 当前这 100ms 的功率和（跨声道）
	accN    int
	powers  []float64 // 每 100ms 的平均功率（每帧各声道之和）
	peak    float64
	frames  int64
	scratch []float64
}

func newLoudnessMeter(rate, channels int) *loudnessMeter {
	if rate <= 0 {
		rate = 48000
	}
	channels = max(channels, 1)
	m := &loudnessMeter{ch: channels, sub: rate / 10, shelf: make([]biquad, channels), hp: make([]biquad, channels)}
	for c := range m.shelf {
		m.shelf[c], m.hp[c] = kWeighting(float64(rate))
	}
	return m
}

// add 喂一段交错的采样。
func (m *loudnessMeter) add(buf []float32) {
	for i := 0; i+m.ch <= len(buf); i += m.ch {
		var p float64
		for c := 0; c < m.ch; c++ {
			s := float64(buf[i+c])
			if a := math.Abs(s); a > m.peak {
				m.peak = a
			}
			y := m.hp[c].step(m.shelf[c].step(s))
			p += y * y
		}
		m.acc += p
		m.accN++
		m.frames++
		if m.accN >= m.sub {
			m.powers = append(m.powers, m.acc/float64(m.accN))
			m.acc, m.accN = 0, 0
		}
	}
}

func lufs(power float64) float64 { return -0.691 + 10*math.Log10(power) }

// result 算积分响度。不足一个 400ms 的块时 ok 为假。
func (m *loudnessMeter) result() (Loudness, bool) {
	n := len(m.powers) - 3
	if n < 1 {
		return Loudness{}, false
	}
	blocks := m.scratch[:0]
	for i := 0; i < n; i++ {
		blocks = append(blocks, (m.powers[i]+m.powers[i+1]+m.powers[i+2]+m.powers[i+3])/4)
	}
	m.scratch = blocks
	mean := func(thr float64) (float64, int) {
		var sum float64
		cnt := 0
		for _, p := range blocks {
			if p > thr {
				sum += p
				cnt++
			}
		}
		if cnt == 0 {
			return 0, 0
		}
		return sum / float64(cnt), cnt
	}
	abs := math.Pow(10, (-70+0.691)/10)
	m1, c := mean(abs)
	if c == 0 {
		return Loudness{}, false
	}
	rel := m1 * math.Pow(10, -1.0)
	m2, c := mean(math.Max(rel, abs))
	if c == 0 {
		return Loudness{}, false
	}
	return Loudness{LUFS: lufs(m2), Peak: m.peak}, true
}

// MeasureFile 把一个本地文件整首解一遍，量出积分响度。不出声，不开声卡。
func MeasureFile(path string) (Loudness, error) {
	f, err := os.Open(path)
	if err != nil {
		return Loudness{}, err
	}
	defer f.Close()
	d, _, err := openDecoder(f)
	if err != nil {
		return Loudness{}, err
	}
	if c, ok := d.(io.Closer); ok {
		defer c.Close()
	}
	m := newLoudnessMeter(d.SampleRate(), d.Channels())
	buf := make([]float32, 4096*max(d.Channels(), 1))
	for {
		n, err := d.Read(buf)
		m.add(buf[:n])
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return Loudness{}, err
			}
			break
		}
		if n == 0 {
			break
		}
	}
	l, ok := m.result()
	if !ok {
		return Loudness{}, errors.New("audio: 太短或太安静，量不出响度")
	}
	return l, nil
}
