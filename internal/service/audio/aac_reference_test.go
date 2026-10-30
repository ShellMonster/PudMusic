package audio

// 「产品这一层」解出来的 AAC，和 ffmpeg 解出来的参考**逐样本**比。
//
// # 它验的是哪一层
//
// `internal/service/audio/mfadec/spike_windows_test.go` 验的是**解码器本身**
// （Media Foundation 认不认得、逐样本对不对得上、COM apartment 稳不稳）。
// 这一份验的是**产品这一层**：字节从 `openDecoder` 进（MyGo 的格式表 →
// macOS 上是 AudioToolbox，Windows 上是 Media Foundation），采样从 MyGo 的
// `audio.Decoder` 接口出。
//
// 这一层多做了三件事，每一件都可能把对的解码结果弄坏：
//
//  1. **认容器**：头几个字节认错就走错整条路；
//  2. **截到容器说的呈现长度**：MP4 的编辑列表说这支曲子有多长，解码器就得
//     在那儿停——多放出来的那几百个采样不属于这支曲子；
//  3. **丢掉编码器延迟**：开头那 1024 个采样是编码器的预热。
//
// # 为什么要两个平台都跑
//
// 这一条在 macOS 和 Windows 上**各跑一遍**，走的是两条不同的解码器
// （AudioToolbox / Media Foundation），比的是**同一份参考**。所以：
//
//   - 它在 macOS 上就能证明「判据、素材、参考值」这三样是对的（Windows 那
//     一遍换的只是解码器）；
//   - Windows 上那一遍是同一个测试，不用另写一套断言。
//
// # 判据
//
//   - MP4 产品路径的呈现长度是 176400 帧；裸 ADTS 产品路径的完整长度是
//     178176 帧；
//   - 最佳对齐后最大逐样本差 ≤ 0.1，**并且把这个数打出来**；
//   - 声道数和参考一致。
//
// ⚠️ **RMS 当不了判据**：它把左右声道、时间轴、幅度揉成一个数，声道调换了
// 它也测不出来。素材的左右声道每一个样本都不一样，所以只能逐样本比。
//
// # 跨解码器的校准线
//
// 这一条在两个平台上跑，走的是不同的系统解码器，而两个解码器和 ffmpeg 的
// 差异不一样大。macOS 的 AudioToolbox 在同一份素材上实测最大逐样本差
// 0.070597，左右声道换掉之后是 0.240906（3.41 倍）。Windows runner 的
// 历史日志记录最大差 0.066711，换声道后是 0.240906（3.61 倍）。
// 因此跨平台统一使用 0.1 的最大差上限和 2 倍的声道交换倍率；这两条线能覆盖
// 已知系统解码器差异，同时仍能把声道交换显著变差识别出来。

import (
	"bytes"
	"io"
	"math"
	"os"
	"testing"
)

// aacRefPath 是 ffmpeg 解同一份 .m4a 得到的 16 位 PCM。
//
// ⚠️ 它是**另一个解码器**解出来的：当判据用。同一个解码器自己解自己只能证明
// 「它和解码器一致」，证明不了「它解对了」。
const aacRefPath = "mfadec/testdata/aaclc-stereo.ref.wav"

// aacFixturePath 是被解的那份 .m4a（B 站有损流的形状：MP4 + AAC-LC）。
const aacFixturePath = "mfadec/testdata/aaclc-stereo.m4a"

// aacFixtureADTSPath 是被解的那份裸 ADTS（.aac）。
//
// ⚠️ 它和 `.m4a` 里装的是**同一段录音**（同一份 `stereo16.wav` 编出来的），
// 所以可以和同一份参考 WAV 比。
const aacFixtureADTSPath = "mfadec/testdata/aaclc-stereo.aac"

// aacMaxDiffBudget 是「最佳对齐后最大逐样本差」的上限。
//
// 0.1 覆盖当前已知的 AudioToolbox（0.070597）和 Media Foundation
// （0.066711）与 ffmpeg 参考之间的正常差异。
func aacMaxDiffBudget() float64 {
	return 0.1
}

// aacSwapRatioBudget 是「左右声道换掉之后，差值至少要变成几倍」。
//
// 当前 AudioToolbox 实测 3.41 倍，Media Foundation 实测 3.61 倍；2 倍
// 留出系统版本的余量，同时仍能识别声道交换。
func aacSwapRatioBudget() float64 {
	return 2
}

func TestAACReferenceBudgetsCoverKnownSystemDecoderDifferences(t *testing.T) {
	if got := aacMaxDiffBudget(); got != 0.1 {
		t.Fatalf("AAC 最大差阈值 = %.3f，want 0.1", got)
	}
	if got := aacSwapRatioBudget(); got != 2 {
		t.Fatalf("AAC 声道交换倍率阈值 = %.1f，want 2", got)
	}
	if known := 0.070597; known >= aacMaxDiffBudget() {
		t.Fatalf("已知 AudioToolbox 差异 %.6f 不应超过阈值 %.2f", known, aacMaxDiffBudget())
	}
	if known := 3.41; known <= aacSwapRatioBudget() {
		t.Fatalf("已知 AudioToolbox 声道交换倍率 %.2f 应超过阈值 %.1f", known, aacSwapRatioBudget())
	}
}

// TestTheProductPathDecodesAACSampleForSample 断「产品这一层解出来的 PCM 和
// ffmpeg 的参考逐样本对得上」。
func TestTheProductPathDecodesAACSampleForSample(t *testing.T) {
	data, err := os.ReadFile(aacFixturePath)
	if err != nil {
		t.Fatalf("读不到素材 %s：%v", aacFixturePath, err)
	}

	d, name, err := openDecoder(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("认不出这个 MP4：%v", err)
	}
	defer func() {
		if c, ok := d.(io.Closer); ok {
			c.Close()
		}
	}()
	t.Logf("容器名 = %q，%d Hz / %d 声道", name, d.SampleRate(), d.Channels())

	if d.Channels() != 2 {
		t.Fatalf("解出来 %d 声道，want 2", d.Channels())
	}
	if d.SampleRate() != 44100 {
		t.Fatalf("解出来 %d Hz，want 44100", d.SampleRate())
	}

	pcm := readAllSamples(t, d)
	frames := len(pcm) / d.Channels()
	t.Logf("产品这一层解出来 %d 帧（%.3f 秒）", frames, float64(frames)/float64(d.SampleRate()))

	// MP4 编辑列表的呈现长度是 176400 帧；裸 ADTS 没有编辑列表，完整
	// access unit 解码长度是 178176 帧。分别钉住，避免多出的尾部被宽区间掩盖。
	const wantFrames = 176400
	if frames != wantFrames {
		t.Errorf("产品 MP4 呈现长度 = %d 帧，want %d", frames, wantFrames)
	}

	ref := readReference(t)
	if len(ref) == 0 {
		t.Fatal("参考 WAV 是空的")
	}

	offset, worst := bestAlignment(pcm, ref)
	mean := meanAbsDiffFrom(pcm[max(offset, 0):], ref)
	t.Logf("最佳对齐：产品这一层比参考晚/早 %d 个采样，最大逐样本差 = %.6f（= %.0f 个 16 位量化级），平均逐样本差 = %.6f",
		offset, worst, worst*32768, mean)
	if budget := aacMaxDiffBudget(); worst > budget {
		t.Errorf("最大逐样本差 %.6f 超过 %.2f——这已经不是两个解码器的正常分歧了", worst, budget)
	}
}

// TestTheProductPathDecodesADTSSampleForSample 断「**裸 ADTS** 那一路也要逐样本
// 对得上」。
//
// # 为什么不能只比 .m4a
//
// 两种容器走的是**不同的解码器输入**（`MF_MT_AAC_PAYLOAD_TYPE` 的两个取值：
// 裸 AAC 帧 / 带 ADTS 头），而 ADTS 那一路还要自己从 ADTS 头里拼出
// AudioSpecificConfig（`ascFromADTS`）。只比 `.m4a` 的话，**ADTS 那一路解错了
// 也会通过**——这是票面点名的那一条（#253 判据④）。
//
// # 参考值是同一份
//
// `.aac` 和 `.m4a` 里装的是**同一段录音**（同一份 `stereo16.wav` 编出来的），
// 所以可以和同一份参考 WAV 比。
//
// ⚠️ **两种容器呈现出来的长度不一样**：`.m4a` 有编辑列表（呈现 176400 个采样），
// `.aac` 没有容器（整条都是），所以长度差 752 个采样——这在两个平台上也一样。
// 逐样本比对是**对齐之后**比，所以不影响。
func TestTheProductPathDecodesADTSSampleForSample(t *testing.T) {
	data, err := os.ReadFile(aacFixtureADTSPath)
	if err != nil {
		t.Fatalf("读不到素材 %s：%v", aacFixtureADTSPath, err)
	}

	d, name, err := openDecoder(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("认不出这个裸 ADTS：%v", err)
	}
	defer func() {
		if c, ok := d.(io.Closer); ok {
			c.Close()
		}
	}()
	t.Logf("容器名 = %q，%d Hz / %d 声道", name, d.SampleRate(), d.Channels())

	if d.Channels() != 2 {
		t.Fatalf("解出来 %d 声道，want 2", d.Channels())
	}
	if d.SampleRate() != 44100 {
		t.Fatalf("解出来 %d Hz，want 44100", d.SampleRate())
	}

	pcm := readAllSamples(t, d)
	frames := len(pcm) / d.Channels()
	t.Logf("裸 ADTS 解出来 %d 帧（%.3f 秒）", frames, float64(frames)/float64(d.SampleRate()))

	// 裸 ADTS 没有编辑列表，完整 access unit 解码长度是 178176 帧。
	const wantFrames = 178176
	if frames != wantFrames {
		t.Errorf("产品裸 ADTS 解码长度 = %d 帧，want %d", frames, wantFrames)
	}

	ref := readReference(t)
	offset, worst := bestAlignment(pcm, ref)
	mean := meanAbsDiffFrom(pcm[max(offset, 0):], ref)
	t.Logf("最佳对齐：裸 ADTS 比参考晚/早 %d 个采样，最大逐样本差 = %.6f（= %.0f 个 16 位量化级），平均逐样本差 = %.6f",
		offset, worst, worst*32768, mean)
	if budget := aacMaxDiffBudget(); worst > budget {
		t.Errorf("最大逐样本差 %.6f 超过 %.2f——这一路解错了", worst, budget)
	}
}

// TestTheProductPathKeepsTheChannelsInPlace 断「左右声道没调换」。
//
// ⚠️ 这一条断的是**判据本身够不够灵**：它把解出来的采样左右换一下，再比一次
// ——差值必须**大得多**。
//
// 换句话说：如果哪天素材的左右声道变成了同一个信号，这条会先红，提醒人
// 「现在的判据已经抓不住声道调换了」。
func TestTheProductPathKeepsTheChannelsInPlace(t *testing.T) {
	data, err := os.ReadFile(aacFixturePath)
	if err != nil {
		t.Fatalf("读不到素材 %s：%v", aacFixturePath, err)
	}
	d, _, err := openDecoder(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("认不出这个 MP4：%v", err)
	}
	defer func() {
		if c, ok := d.(io.Closer); ok {
			c.Close()
		}
	}()
	pcm := readAllSamples(t, d)
	chans := d.Channels()
	if chans != 2 {
		t.Fatalf("这条测试只管立体声，素材是 %d 声道", chans)
	}
	ref := readReference(t)

	swapped := make([]float32, len(pcm))
	copy(swapped, pcm)
	for i := 0; i+1 < len(swapped); i += 2 {
		swapped[i], swapped[i+1] = swapped[i+1], swapped[i]
	}

	_, straight := bestAlignment(pcm, ref)
	_, flipped := bestAlignment(swapped, ref)

	t.Logf("不换：最大逐样本差 %.6f；换了：最大逐样本差 %.6f（%.2f 倍）",
		straight, flipped, flipped/straight)
	if budget := aacSwapRatioBudget(); flipped <= straight*budget {
		t.Errorf("把左右声道换掉之后差值只从 %.6f 变成 %.6f——"+
			"这个判据抓不住声道调换（素材的左右声道可能已经是同一个信号了）",
			straight, flipped)
	}
	if budget := aacMaxDiffBudget(); straight > budget {
		t.Errorf("不换声道时的最大逐样本差 %.6f 已经超过 %.2f", straight, budget)
	}
}

// readReference 读 ffmpeg 解出来的 16 位 PCM，归一化成 [-1,1] 的 float32。
func readReference(t *testing.T) []float32 {
	t.Helper()
	w := readWAV(t, aacRefPath)
	out := make([]float32, len(w.samples))
	for i, v := range w.samples {
		out[i] = float32(v) / 32768.0
	}
	t.Logf("ffmpeg 参考：%d 帧，%d Hz / %d 声道", len(w.samples)/w.channels, w.rate, w.channels)
	return out
}

// bestAlignment 找出两条 PCM 之间差多少个采样对齐最好，返回（偏移，最大逐样本差）。
//
// 偏移是**样本**（不是帧）。正数表示「产品这一层的比参考的晚这么多」。
//
// 为什么要对齐：两个解码器对编码器 priming 该剪多少的判断不一样，所以同一段
// 音频的两条 PCM 之间**本来就差几个样本**。不先对齐就逐样本比，比的是
// 「错位」而不是「解码得对不对」。
//
// ⚠️ **搜索时只看前面一小段**：全量比对 8192 个偏移 × 35 万个采样 = 几十亿次
// 运算，CI 上会跑到超时。所以先用一小段找出最佳偏移，再在那个偏移上比全量
// ——**判据仍然是全量的最大逐样本差**。
func bestAlignment(a, b []float32) (offset int, worst float64) {
	const (
		span  = 4096
		probe = 8192
	)
	best := math.Inf(1)
	for off := -span; off <= span; off++ {
		if off >= len(a) || -off >= len(b) {
			continue
		}
		var m float64
		switch {
		case off >= 0:
			m = meanAbsDiffFrom(a[off:min(off+probe, len(a))], b[:min(probe, len(b))])
		default:
			m = meanAbsDiffFrom(a[:min(probe, len(a))], b[-off:min(-off+probe, len(b))])
		}
		if m < best {
			best = m
			offset = off
		}
	}
	switch {
	case offset >= 0:
		worst = maxAbsDiffFrom(a[offset:], b)
	default:
		worst = maxAbsDiffFrom(a, b[-offset:])
	}
	return offset, worst
}

// meanAbsDiffFrom 两条 PCM 的平均逐样本差（长度不等时只比公共的那一段）。
func meanAbsDiffFrom(a, b []float32) float64 {
	n := min(len(a), len(b))
	if n == 0 {
		return math.Inf(1)
	}
	var sum float64
	for i := range n {
		sum += math.Abs(float64(a[i] - b[i]))
	}
	return sum / float64(n)
}

// maxAbsDiffFrom 两条 PCM 的最大逐样本差（长度不等时只比公共的那一段）。
func maxAbsDiffFrom(a, b []float32) float64 {
	n := min(len(a), len(b))
	var worst float64
	for i := range n {
		if d := math.Abs(float64(a[i] - b[i])); d > worst {
			worst = d
		}
	}
	return worst
}
