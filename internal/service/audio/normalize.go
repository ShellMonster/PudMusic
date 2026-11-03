package audio

import (
	"math"
	"sync/atomic"
)

// 音量均衡（可选，默认关）：不同来源的歌响度差很大，切歌时忽大忽小。
//
// 两种情形：
//
//   - 已知响度（本地文件：ReplayGain 标签或整首量过的，见 MeasureFile /
//     Player.SetLoudness）：响的压下来；**默认不抬**，开着「响度提升」
//     （SetNormalizeGainBoost）才抬到目标响度——抬的幅度被标签/量到的峰值
//     挡着（抬完不超过 normTruePeakCeil），所以不会削波。标签没写峰值
//     （Peak == 0）时按不抬处理，没有上限可挡。
//   - 未知（在线流，边下边放）：量开头 normWindowSeconds 秒的积分响度，只压不抬。
//     没有整首的峰值，抬增益可能削波，这一层又没有限幅器——开着「响度提升」
//     也一样不抬。
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
	// normTruePeakCeil 是抬完之后允许的最大峰值，按 -1 dBTP 取。
	//
	// ⚠️ 这里用的是**采样峰值**当真峰值：4x 过采样的真峰值测量在这一层
	// 太贵（每一帧插值），而 -1 dBTP 的保守余量对采样峰值 ≈ 0.891 已经
	// 把绝大多数插值峰值挡在 0 dBFS 以内。
	normTruePeakCeil = 0.891
	// normWindowSeconds 是未知响度时量多久，量完增益就定住。
	normWindowSeconds = 15
	// normWarmupSeconds 是量到多久才开始动手，太短的样本不可信。
	normWarmupSeconds = 1
)

// knownGain 由已知的响度算整首的增益。boost 为假时只压不抬。
func knownGain(l Loudness, boost bool) float64 {
	g := math.Pow(10, (normTargetLUFS-l.LUFS)/20)
	if g > 1 {
		if !boost || l.Peak <= 0 {
			// 提升关了，或者没有峰值可挡——都不抬。
			g = 1
		} else {
			g = math.Max(1, math.Min(g, normTruePeakCeil/l.Peak))
		}
	}
	return math.Min(math.Max(g, normMinGain), normMaxBoost)
}

// normState 只由解码 goroutine 碰，不加锁，唯一的例外是 known（原子）。
type normState struct {
	meter *loudnessMeter
	gain  float64
	known atomic.Pointer[Loudness]
	// boost 是「允许抬到目标响度」（Player.SetNormalizeGainBoost），设置
	// 走界面线程、这里走解码 goroutine，所以用原子。
	boost atomic.Bool
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
			target = knownGain(*k, n.boost.Load())
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
