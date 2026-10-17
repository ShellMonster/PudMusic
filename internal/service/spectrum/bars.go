package spectrum

import "math"

// 频谱条：把一条谱（Bins 格 0~1）变成 barCount 根条的高度（占条高的比例 0~1）。
//
// 这里的每一句都对着 `_research/biu/src/components/audio-waveform/index.tsx` 的
// `draw()`，四个数一个都没有自己挑。改这四个数里的任何一个，下面的
// `TestTheFourCopiedNumbersAreTheOnesDrawn` 就红。

// LowFraction 是只画低频那一段的份数。
//
// Biu（`audio-waveform/index.tsx:86-89`）：
//
//	// Focus on the lower 60% of the frequency spectrum (most music energy)
//	// With fftSize=512, bufferLength=256.
//	// 0.6 * 256 * (44100/512) ≈ 13kHz coverage
//	const usefulBufferLength = Math.floor(bufferLength * 0.6)
//
// 上面那两行注释也一并抄在这里：13kHz 是它们自己算出来的覆盖上限，
// 「为什么是 60%」在源文件里就是这么答的（大部分音乐能量在低频）。
const LowFraction = 0.6

// BaseDip 是每根条至少有多高（像素）。
//
// Biu（`audio-waveform/index.tsx:104`）：
//
//	// Ensure a minimum height (e.g. 2px) to show a "base" row like PotPlayer
//	const barHeight = Math.max((value / 255) * height, 2)
//
// 那一行注释也抄了：静的时候底下留一条底排，是 PotPlayer 那样。
// 我们这里是**像素（DIP）**，不是条高的比例——写死成比例的话条高一变，
// 底排就不是 2 像素了。
const BaseDip = 2.0

// usefulBins 是只画到第几格：`floor(Bins * LowFraction)`。
//
// Biu 那句的 `bufferLength` 就是 `frequencyBinCount`，也就是 fftSize/2（Bins）。
func usefulBins() int { return int(math.Floor(float64(Bins) * LowFraction)) }

// Bars 把一条谱变成 barCount 根条的高度比例（0~1），写进 dst 并返回。
//
// dst 必须至少 barCount 长；不够就返回空切片（**不**偷偷扩容——每帧调一次的
// 地方悄悄分配正是要避开的）。
//
// heightDip 是那一条区域有多高（DIP），用来把 BaseDip 那个「至少 2 像素」
// 换算成比例。
func Bars(dst, mag []float32, barCount int, heightDip float32) []float32 {
	if barCount <= 0 || len(dst) < barCount || barCount > len(mag) {
		return dst[:0]
	}
	useful := usefulBins()
	floorDip := float32(0)
	if heightDip > 0 {
		floorDip = BaseDip / heightDip
	}

	for i := range barCount {
		// Biu: dataIndex = Math.floor((i / barCount) * usefulBufferLength)
		idx := int(float64(i) / float64(barCount) * float64(useful))
		v := mag[min(idx, useful-1)]
		// Biu: boost = 1 + i / barCount —— 高频天生弱，右边那几根乘一点上去。
		boost := 1 + float32(i)/float32(barCount)
		// Biu: value = Math.min(255, value * boost)
		v = min(v*boost, 1)
		// Biu: barHeight = Math.max((value / 255) * height, 2)
		dst[i] = max(v, floorDip)
	}
	return dst[:barCount]
}
