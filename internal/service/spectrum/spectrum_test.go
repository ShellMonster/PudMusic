package spectrum

import (
	"math"
	"testing"
)

// 这一组测三件事，每一件都钉死在 Biu 的源码上：
//
//	radix-2 FFT 对不对      —— 拿朴素 DFT 逐点核对，外加「纯音落在该落的那一格」
//	那四个抄来的数         —— 低频 60%、2 像素基线、boost、#666（最后一个在 ui 层）
//	dB 映射对不对           —— 和浏览器 `getByteFrequencyData` 的默认值一致

// biuFFTSize / biuBins 是 Biu `audio-waveform/index.tsx:37` 那两个数。
//
// **在这里写死字面量**，不去用生产代码里的常量：常量一改两边跟着变，测试就发现
// 不了了。它们的真值只来自 Biu 的源码。
const (
	biuFFTSize = 512
	biuBins    = 256
	// biuLowBins 是 `Math.floor(256 * 0.6)` = 153（index.tsx:89）。
	biuLowBins = 153
	// biuBaseDip 是 `Math.max(..., 2)`（index.tsx:104）。
	biuBaseDip = 2.0
)

// TestTheFourCopiedNumbers 断那四个数本身。改掉任意一个这里就红。
func TestTheFourCopiedNumbers(t *testing.T) {
	for _, c := range []struct {
		name      string
		got, want float64
		at        string
	}{
		{"fftSize", float64(FFTSize), biuFFTSize, "audio-waveform/index.tsx:37 的 analyser.fftSize"},
		{"frequencyBinCount", float64(Bins), biuBins, "AnalyserNode.frequencyBinCount = fftSize/2"},
		{"低频 60%", LowFraction, 0.6, "index.tsx:89 的 Math.floor(bufferLength * 0.6)"},
		{"2 像素基线", BaseDip, biuBaseDip, "index.tsx:104 的 Math.max(..., 2)"},
	} {
		if c.got != c.want {
			t.Errorf("%s 是 %v，Biu 的是 %v（%s）", c.name, c.got, c.want, c.at)
		}
	}
}

// ---- radix-2 FFT ----

// naiveDFT 是最朴素的 O(n²) 直接变换，用来核对 radix-2 那一份。
func naiveDFT(re, im []float64) ([]float64, []float64) {
	n := len(re)
	outRe, outIm := make([]float64, n), make([]float64, n)
	for k := range n {
		for t := range n {
			ang := -2 * math.Pi * float64(k) * float64(t) / float64(n)
			c, s := math.Cos(ang), math.Sin(ang)
			outRe[k] += re[t]*c - im[t]*s
			outIm[k] += re[t]*s + im[t]*c
		}
	}
	return outRe, outIm
}

// TestTheRadix2TransformMatchesTheDirectOne 逐点核对。
//
// 拿的是一个**不是纯频率**的信号（几个不同频率加起来），因为纯频率恰好落在格点
// 上时两种算法都对，取不出差别。
func TestTheRadix2TransformMatchesTheDirectOne(t *testing.T) {
	const n = 512
	re := make([]float64, n)
	im := make([]float64, n)
	for t := range n {
		x := float64(t) / float64(n)
		re[t] = math.Sin(2*math.Pi*7*x) + 0.4*math.Cos(2*math.Pi*61*x) + 0.2*math.Sin(2*math.Pi*173*x)
	}

	wantRe, wantIm := naiveDFT(re, im)

	gotRe := append([]float64(nil), re...)
	gotIm := append([]float64(nil), im...)
	NewFFT(n).Transform(gotRe, gotIm)

	var worst float64
	for k := range n {
		d := math.Hypot(gotRe[k]-wantRe[k], gotIm[k]-wantIm[k])
		worst = max(worst, d)
	}
	if worst > 1e-9 {
		t.Errorf("和直接变换差 %g（信号幅度 1.6 量级）", worst)
	}
}

// TestTheTransformNeedsAPowerOfTwo 钉住「radix-2」这三个字的前提。
//
// 长度不是 2 的幂时静默算出别的东西，比 panic 难查得多。
func TestTheTransformNeedsAPowerOfTwo(t *testing.T) {
	for _, n := range []int{0, -4, 3, 100} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("长度 %d 没有 panic，而 radix-2 只对 2 的幂成立", n)
				}
			}()
			NewFFT(n)
		}()
	}
}

// ---- 幅度 ----

// TestAPureToneLandsOnItsOwnBin 验「一条纯音落在它该落的那一格」。
//
// 48000 采样率下第 32 格正好是 3000 Hz。窗函数（Blackman，浏览器那一个）把漏出去
// 的部分压得很低，所以相邻两格应该明显比它矮——那正是「窄峰不会被采样丢掉」在
// 这一层的意义。
func TestAPureToneLandsOnItsOwnBin(t *testing.T) {
	// bin 是格号，第 bin 格的频率是 bin * 采样率 / 点数。
	// ⚠️ 样例写的是 `2π * bin * i / FFTSize`，**不是** `/ 采样率`：后者是
	// 32Hz 的信号（一个周期 1500 个采样点），在 512 点的窗里连一个周期都不到，
	// 谱会摊得到处都是——写错的时候测出来「峰值在第 0 格」，很容易被当成 FFT 坏了。
	const rate, bin = 48000, 32
	a := New(1)
	out := make([]float32, Bins)

	// ⚠️ 幅度取 0.05 而不是 1：满幅正弦的峰值是 −1.5 dB 左右，而 dB 映射在 −30 dB
	// 就到顶了（浏览器 `maxDecibels` 的默认值），满幅时**每一格都是 1**，「找最大」
	// 找出来的是第 0 格，测不出任何东西。0.05 的峰值约 −33.5 dB，正好在映射的
	// 斜坡上。
	const amp = 0.05
	samples := make([]float32, FFTSize)
	for i := range samples {
		samples[i] = float32(amp * math.Sin(2*math.Pi*bin*float64(i)/float64(FFTSize)))
	}
	mag := a.Magnitudes(samples, 1, out)

	if mag[bin] < 0.9 {
		t.Fatalf("3000Hz 的纯音在第 %d 格只有 %v（每格 %.1f Hz）",
			bin, mag[bin], float64(rate)/float64(FFTSize))
	}
	if mag[bin-1] >= mag[bin] || mag[bin+1] >= mag[bin] {
		t.Errorf("相邻两格没有比峰矮：%v / %v / %v", mag[bin-1], mag[bin], mag[bin+1])
	}
}

// TestSilenceIsSilence 断「没在放的时候画出来的是一条基线，而不是一片噪声」。
func TestSilenceIsSilence(t *testing.T) {
	a := New(1)
	mag := a.Magnitudes(make([]float32, FFTSize*2), 2, make([]float32, Bins))
	for i, v := range mag {
		if v != 0 {
			t.Fatalf("全零的输入在第 %d 格给出 %v", i, v)
		}
	}
}

// TestTheVolumeScalesTheSpectrum 是 PLAN 阶段 6 风险表点名的那一条：
// 「用户把音量拉到 0，Biu 的频谱会变矮，我们的不会变」。
//
// 我们挂在解码器上、拿不到播放器那一层的音量，所以挂点自己乘一次——这条钉住
// 那一乘真的在。
func TestTheVolumeScalesTheSpectrum(t *testing.T) {
	// 幅度 0.05：满幅信号在 −30 dB 就被映射夹到 1 了（见上一条的注释），那时候
	// 「音量 0.5 比 1 矮」这条根本量不出来——两根都是满格。
	const amp = 0.05
	samples := make([]float32, FFTSize)
	for i := range samples {
		samples[i] = float32(amp * math.Sin(2*math.Pi*32*float64(i)/float64(FFTSize)))
	}

	loud := New(1)
	want := loud.Magnitudes(samples, 1, make([]float32, Bins))[32]

	quiet := New(0)
	got := quiet.Magnitudes(samples, 1, make([]float32, Bins))[32]
	if got != 0 {
		t.Errorf("音量 0 时第 32 格还是 %v（Biu 那边这时候频谱是平的）", got)
	}
	if want == 0 {
		t.Error("音量 1 时第 32 格是 0，样例信号本身有问题")
	}

	half := New(0.5)
	if h := half.Magnitudes(samples, 1, make([]float32, Bins))[32]; h >= want {
		t.Errorf("音量 0.5 时是 %v，没有比音量 1 的 %v 矮", h, want)
	}
}

// TestStereoIsAveragedDownToOneLane 断「两个声道先混成一路」。
//
// 分开算等于把条数翻倍，而频谱画的是一根条一个频段。
func TestStereoIsAveragedDownToOneLane(t *testing.T) {
	stereo := make([]float32, FFTSize*2)
	mono := make([]float32, FFTSize)
	for i := range mono {
		v := float32(math.Sin(2 * math.Pi * 32 * float64(i) / float64(FFTSize)))
		mono[i] = v
		stereo[i*2], stereo[i*2+1] = v, v
	}

	a := New(1)
	got := a.Magnitudes(stereo, 2, make([]float32, Bins))[32]
	want := New(1).Magnitudes(mono, 1, make([]float32, Bins))[32]
	if got != want {
		t.Errorf("两个声道一样的信号，混完是 %v，不混是 %v", got, want)
	}
}

// ---- 条 ----

// TestBarsOnlyReadsTheLowSixtyPercent 断「只画低频 60%」。
//
// 在第 200 格（Bins 256 里的，超出 153）放一个满格的值，条子必须**一条都不高**——
// 全都只剩那条 2 像素基线。反过来在第 100 格放满格，最左边那几根就该蹿起来。
func TestBarsOnlyReadsTheLowSixtyPercent(t *testing.T) {
	const height = 40.0
	// 153 及以上的每一格都填满。
	//
	// ⚠️ **不能**只填一格（比如第 200 格）：取下标是 `floor(i/barCount*153)`，
	// 48 根条落在 {0,5,10,16,…} 这一串下标上，第 200 格**一根都碰不到**——
	// 于是把 LowFraction 从 60% 改成 100% 之后这一条照样绿，而它本该是最直接
	// 盯着那一个数的那一条。
	mag := make([]float32, Bins)
	for i := biuLowBins; i < Bins; i++ {
		mag[i] = 1
	}

	bars := Bars(make([]float32, 48), mag, 48, height)
	for i, v := range bars {
		if v != biuBaseDip/height {
			t.Fatalf("第 %d 根是 %v，低频以外的东西漏进来了（基线是 %v）",
				i, v, biuBaseDip/height)
		}
	}
}

// TestTheTwoPixelBaselineHoldsEvenForSilence 是 Biu 那句注释（
// 「show a base row like PotPlayer」）在代码里的样子。
func TestTheTwoPixelBaselineHoldsEvenForSilence(t *testing.T) {
	const height = 40.0 // Biu 的 `height={40}`（index.tsx:377）
	bars := Bars(make([]float32, 64), make([]float32, Bins), 64, height)
	for i, v := range bars {
		if v != biuBaseDip/height {
			t.Fatalf("全静的第 %d 根是 %v，want %v（2 像素那条底排）", i, v, biuBaseDip/height)
		}
	}
}

// TestBarsBoostsTheRightHandEnd 断 `boost = 1 + i/barCount`（index.tsx:99）。
//
// 每格给同一个值，右边那几根就得更高，而最右边正好是 ×1.975（i = barCount-1）。
// 顺带钉住 `Math.min(255, value * boost)`：乘到 1 之后不许再涨。
func TestBarsBoostsTheRightHandEnd(t *testing.T) {
	const height, n = 40.0, 40
	mag := make([]float32, Bins)
	for i := range mag {
		mag[i] = 0.5
	}

	bars := Bars(make([]float32, n), mag, n, height)
	if bars[0] != 0.5 {
		t.Errorf("最左边一根是 %v，want 0.5（boost 是 1 + 0/40）", bars[0])
	}
	want20 := float32(0.5 * (1 + 20.0/40.0))
	if d := bars[20] - want20; d > 1e-6 || d < -1e-6 {
		t.Errorf("第 20 根是 %v，want %v（boost = 1 + 20/40）", bars[20], want20)
	}
	if bars[39] <= bars[20] {
		t.Errorf("最右边一根 %v 没有比第 20 根 %v 高——boost 没生效", bars[39], bars[20])
	}

	full := make([]float32, Bins)
	for i := range full {
		full[i] = 1
	}
	fb := Bars(make([]float32, n), full, n, height)
	for i, v := range fb {
		if v != 1 {
			t.Fatalf("满格信号的第 %d 根是 %v —— boost 之后没有被夹在 1", i, v)
		}
	}
}

// TestBarsMapsEachBarOntoTheSameBinAsBiu 断那个取下标的写法。
//
// Biu（`index.tsx:94`）：
//
//	dataIndex = Math.floor((i / barCount) * usefulBufferLength)   // useful = 153
//
// 所以 i=0 → 第 0 格、i=39（barCount=40）→ floor(39/40*153) = 第 149 格。
// 取成「均匀铺满 153 格」的话高频那几根全不一样，而那正是「只画低频 60%」
// 这一段里最容易抄错的一处。
//
// 逐根验：只把**它该读的那一格**填满，看画出来的是不是只有它。
func TestBarsMapsEachBarOntoTheSameBinAsBiu(t *testing.T) {
	const n = 40
	// 高度给到极大：底排那 2 像素被压成 0，画面上「有没有读到」才分得清。
	const height = 1e9

	floor := float32(biuBaseDip / height)
	for i := range n {
		want := int(float64(i) / float64(n) * float64(biuLowBins))
		mag := make([]float32, Bins)
		mag[want] = 1

		bars := Bars(make([]float32, n), mag, n, height)
		for j, v := range bars {
			switch {
			case j == i && v == floor:
				t.Fatalf("第 %d 根该读到第 %d 格，画出来却只有基线", j, want)
			case j != i && v != floor:
				t.Fatalf("第 %d 根也读到了第 %d 格（%v）——取下标的写法不对", j, want, v)
			}
		}
	}
}
