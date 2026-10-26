package audio

import (
	"math"
	"sync/atomic"
)

// 音量均衡（可选，默认关）：不同来源的歌响度差很大，切歌时忽大忽小。
//
// 两种情形：
//
//   - 已知响度（本地文件，整首预先量过，见 MeasureFile / Player.SetLoudness）：
//     把整首调到 normTargetLUFS。响的压下来，轻的抬上去——抬的幅度被量到的采样
//     峰值挡着（抬完不超过 0.98），所以不会削波。
//   - 未知（在线流，边下边放）：量开头 normWindowSeconds 秒的积分响度，只压不抬。
//     没有整首的峰值，抬增益可能削波，这一层又没有限幅器。
//
// 它挂在 tap 里（解码器输出这一头、变速层之前），看到的是源 PCM，和倍速无关。
// 增益在每一段内部线性过渡，不会咔哒一声。
const (
	// normTargetLUFS 是目标响度。主流流媒体在 -14 ~ -16，取 -16 偏保守。
	normTargetLUFS = -16.0
	// normMinGain 是最多压多少：约 -12 dB。
	normMinGain = 0.25
	// normMaxBoost 是最多抬多少：约 +9 dB，只在已知峰值时才会抬。
	normMaxBoost = 2.8
	// normPeakCeil 是抬完之后允许的最大采样峰值。
	normPeakCeil = 0.98
	// normWindowSeconds 是未知响度时量多久，量完增益就定住。
	normWindowSeconds = 15
	// normWarmupSeconds 是量到多久才开始动手，太短的样本不可信。
	normWarmupSeconds = 1
)

// knownGain 由已知的响度算整首的增益。
func knownGain(l Loudness) float64 {
	g := math.Pow(10, (normTargetLUFS-l.LUFS)/20)
	if g > 1 && l.Peak > 0 {
		g = math.Max(1, math.Min(g, normPeakCeil/l.Peak))
	}
	return math.Min(math.Max(g, normMinGain), normMaxBoost)
}

// normState 只由解码 goroutine 碰，不加锁，唯一的例外是 known（原子）。
type normState struct {
	meter *loudnessMeter
	gain  float64
	known atomic.Pointer[Loudness]
}

// apply 就地给 buf 乘增益。on 为假时目标是 1。
func (n *normState) apply(buf []float32, channels, rate int, on bool) {
	if n.gain == 0 {
		n.gain = 1
	}
	channels = max(channels, 1)
	if rate <= 0 {
		rate = 48000
	}
	frames := len(buf) / channels
	if frames == 0 {
		return
	}
	target := 1.0
	if on {
		if k := n.known.Load(); k != nil {
			target = knownGain(*k)
		} else {
			if n.meter == nil {
				n.meter = newLoudnessMeter(rate, channels)
			}
			if n.meter.frames < int64(normWindowSeconds*rate) {
				n.meter.add(buf)
			}
			target = n.gain
			if n.meter.frames >= int64(normWarmupSeconds*rate) {
				if l, ok := n.meter.result(); ok && l.LUFS > normTargetLUFS {
					target = math.Max(math.Pow(10, (normTargetLUFS-l.LUFS)/20), normMinGain)
				}
			}
		}
	}
	if n.gain == 1 && target == 1 {
		return
	}
	prev := n.gain
	for f := 0; f < frames; f++ {
		g := float32(prev + (target-prev)*float64(f+1)/float64(frames))
		for c := 0; c < channels; c++ {
			buf[f*channels+c] *= g
		}
	}
	n.gain = target
}
