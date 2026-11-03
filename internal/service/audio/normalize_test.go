package audio

import (
	"math"
	"testing"
)

func sine(frames int, amp float64, freq float64, rate int, from int) []float32 {
	buf := make([]float32, frames*2)
	for f := 0; f < frames; f++ {
		v := float32(amp * math.Sin(2*math.Pi*freq*float64(from+f)/float64(rate)))
		buf[2*f], buf[2*f+1] = v, v
	}
	return buf
}

// EBU Tech 3341 的第一条：1 kHz、双声道、每声道 -23 dBFS 的正弦，积分响度是 -23.0 LUFS。
func TestTheMeterReadsTheEBUReferenceTone(t *testing.T) {
	for _, rate := range []int{44100, 48000} {
		m := newLoudnessMeter(rate, 2)
		m.add(sine(rate*20, math.Pow(10, -23.0/20), 1000, rate, 0))
		l, ok := m.result()
		if !ok || math.Abs(l.LUFS-(-23.0)) > 0.15 {
			t.Errorf("%d Hz：量到 %.2f LUFS（ok=%v），want -23.0", rate, l.LUFS, ok)
		}
	}
}

func TestTheMeterIgnoresSilenceAroundTheMusic(t *testing.T) {
	const rate = 48000
	m := newLoudnessMeter(rate, 2)
	m.add(make([]float32, rate*2*10))
	m.add(sine(rate*20, math.Pow(10, -23.0/20), 1000, rate, 0))
	m.add(make([]float32, rate*2*10))
	if l, ok := m.result(); !ok || math.Abs(l.LUFS-(-23.0)) > 0.15 {
		t.Errorf("两头有静音时量到 %.2f，want -23.0（门限没把静音挡掉）", l.LUFS)
	}
}

// 把一段信号过一遍 apply，再用一只新表量输出的响度。
func normalizedLUFS(amp float64, on bool, known *Loudness) (float64, float64) {
	return normalizedLUFSBoost(amp, on, false, known)
}

func normalizedLUFSBoost(amp float64, on, boost bool, known *Loudness) (float64, float64) {
	const rate, block = 48000, 2048
	var n normState
	n.boost.Store(boost)
	if known != nil {
		n.known.Store(known)
	}
	out := newLoudnessMeter(rate, 2)
	var tail []float32
	for from := 0; from < 20*rate; from += block {
		buf := sine(block, amp, 440, rate, from)
		n.apply(buf, 2, rate, on)
		if from >= 17*rate {
			out.add(buf)
		}
		tail = buf
	}
	_ = tail
	l, _ := out.result()
	return l.LUFS, n.gain
}

func TestNormalizeTurnsALoudStreamDownToTheTarget(t *testing.T) {
	got, gain := normalizedLUFS(0.5, true, nil)
	if math.Abs(got-normTargetLUFS) > 1.0 || gain >= 1 {
		t.Errorf("响的流压完 %.2f LUFS（增益 %.2f），want ≈%.1f", got, gain, normTargetLUFS)
	}
}

func TestNormalizeNeverBoostsAQuietStream(t *testing.T) {
	_, gain := normalizedLUFS(0.02, true, nil)
	if gain != 1 {
		t.Errorf("未知响度的轻的流被动了：增益 %.3f", gain)
	}
}

func TestNormalizeIsAbsentWhenOff(t *testing.T) {
	_, gain := normalizedLUFS(0.9, false, nil)
	if gain != 1 {
		t.Errorf("关着的时候被动了：增益 %.3f", gain)
	}
}

func TestAKnownQuietTrackIsBoostedButNeverPastThePeakCeiling(t *testing.T) {
	g := knownGain(Loudness{LUFS: -26, Peak: 0.2}, true)
	if want := math.Pow(10, 10.0/20); math.Abs(g-want) > 1e-6 && g != normMaxBoost {
		t.Errorf("轻的歌增益 %.3f，want %.3f 或上限 %.3f", g, want, normMaxBoost)
	}
	if g := knownGain(Loudness{LUFS: -26, Peak: 0.7}, true); g*0.7 > normTruePeakCeil+1e-9 {
		t.Errorf("抬完峰值 %.3f 超过 %.2f", g*0.7, normTruePeakCeil)
	}
	if g := knownGain(Loudness{LUFS: -26, Peak: 0.99}, true); g != 1 {
		t.Errorf("本来就顶格的歌不该被抬：%.3f", g)
	}
}

func TestAKnownLoudTrackIsTurnedDownToTheFloorAtMost(t *testing.T) {
	if g := knownGain(Loudness{LUFS: -3, Peak: 1}, true); g != normMinGain {
		t.Errorf("特别响的歌增益 %.3f，want 下限 %.2f", g, normMinGain)
	}
}

func TestAKnownTrackUsesTheMeasuredGainFromTheFirstBlock(t *testing.T) {
	_, gain := normalizedLUFSBoost(0.1, true, true, &Loudness{LUFS: -22, Peak: 0.1})
	if want := knownGain(Loudness{LUFS: -22, Peak: 0.1}, true); math.Abs(gain-want) > 1e-6 {
		t.Errorf("增益 %.3f，want %.3f", gain, want)
	}
}

// 响度提升关着时（默认）：已知响度的轻的歌照样不抬——「只压不抬」对标签
// 那一档也成立，不然开着归一化放一首标了 +6dB 的歌会突然被放大。
func TestABoostOffKnownQuietTrackStaysAtUnity(t *testing.T) {
	if g := knownGain(Loudness{LUFS: -26, Peak: 0.2}, false); g != 1 {
		t.Errorf("提升关着的轻的歌增益 %.3f，want 1", g)
	}
	// 压的那半边不受 boost 影响。
	if g := knownGain(Loudness{LUFS: -6, Peak: 0.9}, false); g >= 1 {
		t.Errorf("提升关着的响的歌增益 %.3f，want < 1", g)
	}
}

// 标签里没写 peak（Peak == 0）时即使开着提升也不抬：没有上限可挡，
// 抬出去可能削波。
func TestAKnownQuietTrackWithoutAPeakIsNotBoosted(t *testing.T) {
	if g := knownGain(Loudness{LUFS: -30, Peak: 0}, true); g != 1 {
		t.Errorf("没写峰值的轻的歌增益 %.3f，want 1", g)
	}
}

// 提升开着时整段走一遍：输出响度应被抬到接近目标，而不是停在 1。
func TestBoostLiftsAQuietStreamTowardsTheTarget(t *testing.T) {
	got, gain := normalizedLUFSBoost(0.05, true, true, &Loudness{LUFS: -30, Peak: 0.05})
	if gain <= 1 || math.Abs(got-normTargetLUFS) > 3 {
		t.Errorf("提升开着的轻的歌：输出 %.2f LUFS（增益 %.2f），want 接近 %.1f",
			got, gain, normTargetLUFS)
	}
}

// 提升开着而歌本来就贴着峰值：增益必须被峰值挡住，不得越 -1 dBTP 的界。
func TestBoostRespectsTheTruePeakCeiling(t *testing.T) {
	const amp = 0.8
	_, gain := normalizedLUFSBoost(amp, true, true, &Loudness{LUFS: -40, Peak: amp})
	if gain*amp > normTruePeakCeil+1e-9 {
		t.Errorf("提升之后峰值 %.3f 越过 %.3f（-1 dBTP）", gain*amp, normTruePeakCeil)
	}
}
