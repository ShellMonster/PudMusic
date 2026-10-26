package audio

import (
	"math"
	"sync/atomic"
)

// 十段图示均衡器：挂在 tap 里（解码器输出这一头、变速层之前），每段一个峰值
// 滤波器（RBJ 的 peaking EQ，Q = 1.41，约一个倍频程宽）。
//
// 为了不削波，每套增益先按最大的正增益整体下压一个前置衰减（preamp）：
// 抬 6 dB 的预设，整体先降 6 dB。

// EQBands 是十个频段的中心频率（Hz）。
var EQBands = [10]float64{31, 62, 125, 250, 500, 1000, 2000, 4000, 8000, 16000}

// EQPreset 是一套命名的增益（dB，每段 -12..+12）。
type EQPreset struct {
	ID    string
	Name  string
	Gains [10]float64
}

// EQPresets 是界面上给的预设。第一项是「关」（平直，不处理）。
var EQPresets = []EQPreset{
	{"flat", "关", [10]float64{}},
	{"bass", "低音", [10]float64{6, 5, 4, 2, 0, 0, 0, 0, 0, 0}},
	{"vocal", "人声", [10]float64{-2, -1, 0, 2, 4, 4, 3, 1, 0, -1}},
	{"bright", "明亮", [10]float64{0, 0, 0, 0, 0, 1, 2, 4, 5, 5}},
	{"pop", "流行", [10]float64{-1, 2, 4, 4, 1, -1, -1, 1, 2, 3}},
	{"rock", "摇滚", [10]float64{5, 4, 2, -1, -2, -1, 1, 3, 4, 5}},
}

// EQPresetByID 按 ID 找预设，认不出的回到「关」。
func EQPresetByID(id string) EQPreset {
	for _, p := range EQPresets {
		if p.ID == id {
			return p
		}
	}
	return EQPresets[0]
}

const eqQ = 1.41

// eqParams 是一套生效中的增益。gen 变了说明要重算滤波器。
type eqParams struct {
	gains [10]float64
	gen   uint64
}

func (p *eqParams) flat() bool {
	for _, g := range p.gains {
		if g != 0 {
			return false
		}
	}
	return true
}

// eqState 只由解码 goroutine 碰；params 由别的线程换，所以是原子的。
type eqState struct {
	params  atomic.Pointer[eqParams]
	nextGen atomic.Uint64

	gen     uint64
	rate    int
	ch      int
	filters [][10]biquad
	pre     float32
}

func (e *eqState) set(g [10]float64) {
	p := &eqParams{gains: g, gen: e.nextGen.Add(1)}
	if p.flat() {
		e.params.Store(nil)
		return
	}
	e.params.Store(p)
}

func (e *eqState) rebuild(p *eqParams, rate, channels int) {
	e.gen, e.rate, e.ch = p.gen, rate, channels
	e.filters = make([][10]biquad, channels)
	maxBoost := 0.0
	for _, g := range p.gains {
		maxBoost = math.Max(maxBoost, g)
	}
	e.pre = float32(math.Pow(10, -maxBoost/20))
	for b, f := range EQBands {
		if f >= 0.45*float64(rate) || p.gains[b] == 0 {
			continue
		}
		a := math.Pow(10, p.gains[b]/40)
		w0 := 2 * math.Pi * f / float64(rate)
		cos, alpha := math.Cos(w0), math.Sin(w0)/(2*eqQ)
		a0 := 1 + alpha/a
		q := biquad{
			b0: (1 + alpha*a) / a0, b1: -2 * cos / a0, b2: (1 - alpha*a) / a0,
			a1: -2 * cos / a0, a2: (1 - alpha/a) / a0,
		}
		for c := range e.filters {
			e.filters[c][b] = q
		}
	}
}

// apply 就地过一遍均衡器。没开（平直）时一个采样都不碰。
func (e *eqState) apply(buf []float32, channels, rate int) {
	p := e.params.Load()
	if p == nil || channels < 1 || len(buf) == 0 {
		return
	}
	if p.gen != e.gen || e.rate != rate || e.ch != channels {
		e.rebuild(p, rate, channels)
	}
	for i := 0; i+channels <= len(buf); i += channels {
		for c := 0; c < channels; c++ {
			x := float64(buf[i+c])
			for b := range e.filters[c] {
				if e.filters[c][b].b0 != 0 {
					x = e.filters[c][b].step(x)
				}
			}
			buf[i+c] = float32(x) * e.pre
		}
	}
}
