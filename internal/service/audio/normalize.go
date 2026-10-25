package audio

import "math"

// 音量均衡（可选，默认关）：不同来源的歌响度差很大，切歌时忽大忽小。
//
// 做法是「只压不抬」：量每首歌开头 normWindowSeconds 秒的均方根（RMS），比目标
// 响的就把整首压到目标，比目标轻的原样放。只压不抬是故意的——抬增益会削波，
// 而这一层没有限幅器。
//
// 它挂在 tap 里（解码器输出这一头、变速层之前），所以看到的是源 PCM，和倍速
// 无关。增益在每一段内部线性过渡，不会咔哒一声。
const (
	// normTargetRMS 是目标响度：-18 dBFS。
	normTargetRMS = 0.1259
	// normMinGain 是最多压多少：约 -10 dB。
	normMinGain = 0.3
	// normWindowSeconds 是量多久，量完增益就定住。
	normWindowSeconds = 10
	// normWarmupSeconds 是量到多久才开始动手，太短的样本不可信。
	normWarmupSeconds = 1
)

// normState 只由解码 goroutine 碰，不加锁。
type normState struct {
	sumSq  float64
	frames int64
	gain   float64
}

// apply 就地给 buf 乘增益，返回这一段用的目标增益。on 为假时目标是 1。
func (n *normState) apply(buf []float32, channels, rate int, on bool) {
	if n.gain == 0 {
		n.gain = 1
	}
	if rate <= 0 {
		rate = 48000
	}
	frames := len(buf) / max(channels, 1)
	if frames == 0 {
		return
	}
	target := 1.0
	if on {
		if n.frames < int64(normWindowSeconds*rate) {
			for _, s := range buf {
				n.sumSq += float64(s) * float64(s)
			}
			n.frames += int64(frames)
		}
		if n.frames >= int64(normWarmupSeconds*rate) {
			rms := math.Sqrt(n.sumSq / float64(n.frames*int64(max(channels, 1))))
			if rms > normTargetRMS {
				target = max(normTargetRMS/rms, normMinGain)
			}
		} else {
			target = n.gain
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
