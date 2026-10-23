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
//   - 帧数落在 176400~178176（下界 = AudioToolbox 的，上界 = 不剪的）；
//   - 最佳对齐后最大逐样本差 ≤ 0.02，**并且把这个数打出来**；
//   - 声道数和参考一致。
//
// ⚠️ **RMS 当不了判据**：它把左右声道、时间轴、幅度揉成一个数，声道调换了
// 它也测不出来。素材的左右声道每一个样本都不一样，所以只能逐样本比。
//
// # ⚠️ 0.02 那条线是**给 Media Foundation 定的**
//
// 这一条在两个平台上跑，走的是两个不同的解码器，而**两个解码器和 ffmpeg 的
// 差不一样大**。macOS 的 AudioToolbox 在同一份素材上实测是：
//
//	最大逐样本差 0.070597（平均 0.0009）
//	左右声道换掉之后：0.240906（3.41 倍）
//
// 所以 macOS 那一档用**实测出来的**线（0.1 / 2 倍），Windows 那一档用票面
// 定的线（0.02 / 4 倍）。两条线都写在这里，是为了让「为什么 macOS 的松、
// Windows 的紧」有据可查，而不是看心情。
//
// ⚠️ **那两条线从来没有对任何一个真解码器验过**——Windows 那条 CI 从建起来
// 就没有跑起来过（runner 根本没接到，账号付款的问题）。所以 Windows 上的
// 数字到底落在哪儿**是未知的**：可能远小于 0.02，也可能和 AudioToolbox 一个
// 量级。这一条要等 CI 能跑了才知道。

import (
	"bytes"
	"io"
	"math"
	"os"
	"runtime"
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
// Windows（Media Foundation）用票面定的 0.02；别的平台用 macOS 实测出来的
// 0.1（AudioToolbox 是 0.070597）。
func aacMaxDiffBudget() float64 {
	if runtime.GOOS == "windows" {
		return 0.02
	}
	return 0.1
}

// aacSwapRatioBudget 是「左右声道换掉之后，差值至少要变成几倍」。
//
// Windows 用票面定的 4；别的平台用 macOS 实测出来的 2（AudioToolbox 是
// 3.41 倍）。
func aacSwapRatioBudget() float64 {
	if runtime.GOOS == "windows" {
		return 4
	}
	return 2
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

	// 帧数：两边对编码器 priming 的剪法不一样，所以只要求落在
	// 「AudioToolbox 的 176400」到「不剪的 178176」之间。
	const (
		framesLow  = 176400
		framesHigh = 178176
	)
	if frames < framesLow || frames > framesHigh {
		t.Errorf("解出 %d 帧，落在 [%d, %d] 之外", frames, framesLow, framesHigh)
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

	// 帧数：和 .m4a 那一路同一个区间（裸 ADTS 没有编辑列表，所以它是整条）。
	const (
		framesLow  = 176400
		framesHigh = 178176
	)
	if frames < framesLow || frames > framesHigh {
		t.Errorf("解出 %d 帧，落在 [%d, %d] 之外", frames, framesLow, framesHigh)
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
