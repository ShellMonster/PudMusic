package audio

import (
	"math"
	"testing"
)

// sineBlocks 喂 seconds 秒、幅度 amp 的双声道正弦给 n，返回最后一段的 RMS。
func sineBlocks(n *normState, amp float64, seconds int, on bool) float64 {
	const rate, block = 48000, 2048
	phase, last := 0, 0.0
	for done := 0; done < seconds*rate; done += block {
		buf := make([]float32, block*2)
		for f := 0; f < block; f++ {
			v := float32(amp * math.Sin(2*math.Pi*440*float64(phase)/rate))
			phase++
			buf[2*f], buf[2*f+1] = v, v
		}
		n.apply(buf, 2, rate, on)
		var sum float64
		for _, s := range buf {
			sum += float64(s) * float64(s)
		}
		last = math.Sqrt(sum / float64(len(buf)))
	}
	return last
}

func TestNormalizeTurnsALoudTrackDownToTheTarget(t *testing.T) {
	var n normState
	got := sineBlocks(&n, 0.4, 5, true)
	if math.Abs(got-normTargetRMS) > 0.01 {
		t.Errorf("响的歌压完 RMS=%.3f，want ≈%.3f", got, normTargetRMS)
	}
}

func TestNormalizeNeverBoostsAQuietTrack(t *testing.T) {
	var n normState
	amp := 0.1
	got := sineBlocks(&n, amp, 5, true)
	if want := amp / math.Sqrt2; math.Abs(got-want) > 2e-3 {
		t.Errorf("轻的歌被动了：RMS=%.4f want %.4f", got, want)
	}
}

func TestNormalizeIsAbsentWhenOff(t *testing.T) {
	var n normState
	got := sineBlocks(&n, 0.9, 5, false)
	if want := 0.9 / math.Sqrt2; math.Abs(got-want) > 2e-3 {
		t.Errorf("关着的时候被动了：RMS=%.4f want %.4f", got, want)
	}
}

func TestNormalizeNeverCutsBelowTheFloor(t *testing.T) {
	var n normState
	sineBlocks(&n, 1.0, 5, true)
	if n.gain < normMinGain-1e-9 {
		t.Errorf("增益 %.3f 低于下限 %.3f", n.gain, normMinGain)
	}
}
