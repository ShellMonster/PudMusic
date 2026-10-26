package audio

import (
	"math"
	"testing"
)

func eqRMS(e *eqState, freq float64) float64 {
	const rate = 48000
	buf := sine(rate*2, 0.2, freq, rate, 0)
	e.apply(buf, 2, rate)
	var sum float64
	n := 0
	for i := rate; i < len(buf); i++ { // 跳过头一秒的瞬态
		sum += float64(buf[i]) * float64(buf[i])
		n++
	}
	return math.Sqrt(sum / float64(n))
}

func TestFlatEQLeavesTheSamplesAlone(t *testing.T) {
	var e eqState
	e.set([10]float64{})
	buf := sine(4800, 0.3, 440, 48000, 0)
	want := append([]float32(nil), buf...)
	e.apply(buf, 2, 48000)
	for i := range buf {
		if buf[i] != want[i] {
			t.Fatalf("采样 %d 被动了", i)
		}
	}
}

func TestTheBassPresetLiftsLowsAgainstHighs(t *testing.T) {
	var e eqState
	e.set(EQPresetByID("bass").Gains)
	low, high := eqRMS(&e, 62), eqRMS(&e, 8000)
	if got := 20 * math.Log10(low/high); got < 5.5 || got > 8.5 {
		t.Errorf("低音预设 62 Hz 比 8 kHz 高 %.1f dB，want 约 7（31/125 Hz 两个邻段也有贡献）", got)
	}
}

func TestTheEQNeverMakesThePeakLouderThanTheInput(t *testing.T) {
	var e eqState
	e.set(EQPresetByID("rock").Gains)
	for _, f := range []float64{31, 62, 1000, 8000, 16000} {
		if got := eqRMS(&e, f); got > 0.2/math.Sqrt2*1.12 {
			t.Errorf("%.0f Hz 经过均衡器之后 RMS %.3f，比输入高了 1 dB 以上（前置衰减没起作用）", f, got)
		}
	}
}

func TestAnUnknownPresetIsFlat(t *testing.T) {
	if EQPresetByID("没有这个").ID != "flat" {
		t.Error("认不出的预设该回到关")
	}
}
