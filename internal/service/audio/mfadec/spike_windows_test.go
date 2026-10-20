//go:build windows && (amd64 || arm64)

package mfadec

// 这一份就是 GitHub issue #128 的 spike：**结论 + 真实输出**。
//
// 跑法（必须 `-v`，因为答案全在 t.Logf 里）：
//
//	cd internal/service/audio/mfadec && go test -v -run TestSpike ./...
//
// 它回答票面问的四件事，每一件都用**这台 Windows 真机上的实测数字**回答：
//
//	1. IMFTransform 能不能把 B 站流里的 AAC 解出来
//	   —— TestSpikeAACLCDecodesOutOfMediaFoundation
//	2. 首帧延迟
//	   —— TestSpikeFirstFrameLatency
//	3. 支持哪些 profile
//	   —— TestSpikeWhichProfilesWeCanActuallyTest
//	4. 解出来的 PCM 和另一个解码器逐样本对不对得上
//	   —— TestSpikePCMIsWithinTolerancesOfTheReference
//
// 外加票面点名要的那一条：**COM apartment 在负载下反复跑**
//   —— TestSpikeTheCOMApartmentSurvivesLoad
//
// # 关于「没验到的部分」
//
// 这一票有几件事**在这里验不了**，它们不是「验过没问题」，是「没验」：
//
//   - **HE-AAC**：本机 ffmpeg 的原生 aac 编码器造不出 HE 码流
//     （`ffmpeg -h encoder=aac` 没有 profile 选项，要 libfdk_aac），
//     所以没有素材。TestSpikeWhichProfilesWeCanActuallyTest 会明确
//     打出「UNVERIFIED」。**有一条是查到的、不是验到的**：Windows 自带的
//     这个解码器，微软自己的文档写着它支持 HE-AAC v1 / v2——但**文档说
//     支持不等于我们验过**（见那一段的说明）。
//   - **B 站的真实字节**：这里用的是按 B 站形状造的素材
//     （MP4 + AAC-LC 44100 Hz 立体声）。真去拉 B 站的流要真登录态，
//     而那把钥匙串里躺着真账号（见 AGENTS 的约定），不去碰。
//   - **在线流**：这里的字节是一次性读进内存的。产品里在线流是边下边播，
//     那一段（自己实现一个字节流）不在这张票里。
//
// 跑出来的数字请原样贴进报告，别只写「通过」。

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// ── 1. 能不能解出 AAC-LC ──────────────────────────────────────────────────

// TestSpikeAACLCDecodesOutOfMediaFoundation 是这一票的主结论。
func TestSpikeAACLCDecodesOutOfMediaFoundation(t *testing.T) {
	if err := bindingsError(); err != nil {
		t.Fatalf("系统函数没绑上：%v", err)
	}

	for _, name := range []string{"aaclc-stereo.m4a", "aaclc-stereo.aac"} {
		t.Run(name, func(t *testing.T) {
			started := time.Now()
			src, err := openFixture(name)
			if err != nil {
				t.Fatalf("拆 %s：%v", name, err)
			}
			opened := time.Since(started)

			codec, err := OpenAACCodec(src.rate, src.chans, src.asc, src.payloadType)
			if err != nil {
				t.Fatalf("开解码器：%v", err)
			}
			defer codec.Close()

			t.Logf("拆 %s 用了 %s", name, opened.Round(time.Microsecond))
			t.Logf("payloadType=%d 核心码流：%d Hz / %d 声道，ASC %d 字节",
				src.payloadType, src.rate, src.chans, len(src.asc))
			t.Logf("MFT 认下的输入配置：%s", codec.InputVariant())
			t.Logf("解码器输出：%d Hz / %d 声道（输出样本由调用方分配：%v）",
				codec.SampleRate(), codec.Channels(), codec.CallerAllocatesOutput())

			frames, pcm, dur := decodeAll(t, codec, src)
			rms, peak := stats(pcm)
			t.Logf("解出 %d 帧（%.3f 秒），读耗时 %s，rms=%.6f peak=%.6f",
				frames, float64(frames)/float64(codec.SampleRate()), dur.Round(time.Millisecond), rms, peak)

			if codec.SampleRate() != 44100 {
				t.Errorf("采样率 %d，want 44100", codec.SampleRate())
			}
			if codec.Channels() != 2 {
				t.Errorf("声道数 %d，want 2", codec.Channels())
			}
			if frames == 0 {
				t.Fatal("一帧都没解出来")
			}
			if rms < 0.05 {
				t.Errorf("解出来几乎是静音：rms=%.6f（素材的 rms 是 %.6f）", rms, refRMS)
			}
		})
	}
}

// ── 2. 首帧延迟 ────────────────────────────────────────────────────────────

// TestSpikeFirstFrameLatency 量「用户点了播放到听见声音」里的前一半。
//
// ⚠️ 判据是**打开解码器到第一帧 PCM 出来**，不是「打开到能播」：播放器打开
// 之后还要再缓冲一点，而那一段和这一票验的东西无关。计划阶段 0 的目标是
// 200 毫秒，这里拿它当线。
func TestSpikeFirstFrameLatency(t *testing.T) {
	const budget = 200 * time.Millisecond

	var worst time.Duration
	for i := range 5 {
		started := time.Now()
		src, err := openFixture("aaclc-stereo.m4a")
		if err != nil {
			t.Fatalf("拆素材：%v", err)
		}
		codec, err := OpenAACCodec(src.rate, src.chans, src.asc, src.payloadType)
		if err != nil {
			t.Fatalf("开解码器：%v", err)
		}

		// 喂到第一帧 PCM 出来为止。
		var first int
		for {
			frame, err := src.next()
			if err != nil {
				t.Fatalf("第 %d 次：读帧：%v", i+1, err)
			}
			out, err := codec.Feed(frame)
			if err != nil {
				t.Fatalf("第 %d 次：解码：%v", i+1, err)
			}
			if len(out) > 0 {
				first = len(out) / codec.Channels()
				break
			}
		}
		got := time.Since(started)
		codec.Close()

		t.Logf("第 %d 次：拆容器 + 开解码器 + 解出第一帧 %s（%d 帧样本）",
			i+1, got.Round(time.Microsecond), first)
		if got > worst {
			worst = got
		}
	}
	t.Logf("五次里最慢的一次：%s，预算 %s", worst.Round(time.Microsecond), budget)
	if worst > budget {
		t.Errorf("首帧 %s 超过计划里的 %s", worst.Round(time.Microsecond), budget)
	}
}

// ── 3. 支持哪些 profile ────────────────────────────────────────────────────

// TestSpikeWhichProfilesWeCanActuallyTest 把「支持哪些 profile」这件事**按实际
// 能验的范围**回答，而不是按愿望。
//
// ⚠️ 特别要说清楚的是：Media Foundation 的媒体类型里只有「子类型 = AAC」这一位，
// **它不告诉你 LC 还是 HE**。所以「认 profile」这件事在这条路上根本不存在，
// 只能靠喂不同码流去看能不能解——而 HE-AAC 的素材这台机器造不出来。
func TestSpikeWhichProfilesWeCanActuallyTest(t *testing.T) {
	t.Run("AAC-LC", func(t *testing.T) {
		// ffprobe 说这两份素材的 profile 是 LC（见 fixture_test.go 的说明）。
		// 这就是票面要求「一定要」的那一档。
		src, err := openFixture("aaclc-stereo.m4a")
		if err != nil {
			t.Fatal(err)
		}
		codec, err := OpenAACCodec(src.rate, src.chans, src.asc, src.payloadType)
		if err != nil {
			t.Fatal(err)
		}
		defer codec.Close()
		frames, pcm, _ := decodeAll(t, codec, src)
		rms, peak := stats(pcm)
		t.Logf("AAC-LC ✅ 能解：%d 帧 rms=%.6f peak=%.6f", frames, rms, peak)
		if frames == 0 {
			t.Fatal("AAC-LC 解不出样本")
		}
	})

	t.Run("HE-AAC", func(t *testing.T) {
		t.Skip("UNVERIFIED —— 这台机器上的 ffmpeg 造不出 HE-AAC 码流" +
			"（原生 aac 编码器没有 profile 选项，要 libfdk_aac），没有素材可喂。" +
			"**这一条是「没验」，不是「验过能解」。**" +
			"要补：装一个 libfdk_aac 版本的 ffmpeg，`-profile:a aac_he` 造一份素材再跑这里。")
	})
}

// ── 4. 解出来的 PCM 和参考值对不对得上 ────────────────────────────────────

// TestSpikePCMIsWithinTolerancesOfTheReference 是「解出来的 PCM 对不对」的判据。
//
// 参考是 ffmpeg 解同一个 .m4a 得到的 16 位 PCM（`aaclc-stereo.ref.wav`），
// 在 macOS 上量过：177152 帧、rms 0.262226、峰值 0.645752
// （`fixture_test.go` 把那几个数钉在仓库里了）。
//
// # ⚠️ 为什么是「在容差内」而不是「逐比特相等」
//
// 两个解码器做不到逐比特相等。AudioToolbox 解同一份素材给的是 176400 帧，
// ffmpeg 给 177152 帧——差 752 帧（17 毫秒），是两边对编码器 priming 该剪多少
// 的判断不一样。所以这里的判据是「**逐样本**在容差内」，而且要把
// **实际的最大逐样本差**打出来，那个数才是真正有信息量的。
//
// # 为什么不能拿 RMS 当判据
//
// RMS 把左右声道、时间轴、幅度全揉成一个数，**左右声道调换了它也测不出来**。
// 素材的左右声道是**每一个样本都不一样**的（`TestTheFixtureChannelsDiffer`
// 钉着这条），所以逐样本比对是能抓住「声道调换」的——下面那条
// `TestSpikeTheChannelsAreNotSwapped` 就是专门干这个的。
func TestSpikePCMIsWithinTolerancesOfTheReference(t *testing.T) {
	src, err := openFixture("aaclc-stereo.m4a")
	if err != nil {
		t.Fatal(err)
	}
	codec, err := OpenAACCodec(src.rate, src.chans, src.asc, src.payloadType)
	if err != nil {
		t.Fatal(err)
	}
	defer codec.Close()
	frames, pcm, _ := decodeAll(t, codec, src)

	_, chans, want := readReferenceWAV(t)
	rms, peak := stats(pcm)

	t.Logf("Media Foundation: %d 帧 rms=%.6f peak=%.6f", frames, rms, peak)
	t.Logf("ffmpeg 参考:     %d 帧 rms=%.6f peak=%.6f", refFrames, refRMS, refPeak)

	// 帧数：两边对 priming 的剪法不一样，所以只要求落在音频工具书
	// 「下界 = AudioToolbox 的 176400」到「上界 = 不剪的 178176」之间。
	const (
		framesLow  = 176400
		framesHigh = 178176
	)
	if frames < framesLow || frames > framesHigh {
		t.Errorf("解出 %d 帧，落在 [%d, %d] 之外", frames, framesLow, framesHigh)
	}
	if chans != codec.Channels() {
		t.Fatalf("参考 %d 声道，解出来 %d 声道，没法比", chans, codec.Channels())
	}

	// 对齐：两个解码器剪掉的 priming 不一样，先找出差多少个样本。
	offset, worst := bestAlignment(pcm, want)
	t.Logf("最佳对齐：MF 比参考晚/早 %d 个样本，最大逐样本差 = %.6f（= %.0f 个 16 位量化级）",
		offset, worst, worst*32768)
	if worst > 0.02 {
		t.Errorf("最大逐样本差 %.6f 超过 %.2f——这已经不是两个解码器的正常分歧了", worst, 0.02)
	}
}

// TestSpikeTheChannelsAreNotSwapped 专门断「左右声道没有调换」。
//
// 上一条断的是「解出来的 PCM 和参考逐样本对得上」。这一条断的是**这个判据
// 本身够不够灵**：把 MF 的输出左右换一下，再比一次——差值必须**大得多**。
//
// 换句话说：如果哪天素材的左右声道变成了同一个信号，这条会先红，
// 提醒人「现在的判据已经抓不住声道调换了」。
func TestSpikeTheChannelsAreNotSwapped(t *testing.T) {
	src, err := openFixture("aaclc-stereo.m4a")
	if err != nil {
		t.Fatal(err)
	}
	codec, err := OpenAACCodec(src.rate, src.chans, src.asc, src.payloadType)
	if err != nil {
		t.Fatal(err)
	}
	defer codec.Close()
	_, pcm, _ := decodeAll(t, codec, src)

	_, chans, want := readReferenceWAV(t)
	swapped := swapChannels(pcm, chans)

	_, straight := bestAlignment(pcm, want)
	_, flipped := bestAlignment(swapped, want)

	t.Logf("不换：最大逐样本差 %.6f；换了：最大逐样本差 %.6f", straight, flipped)
	if flipped <= straight*4 {
		t.Errorf("把左右声道换掉之后差值只从 %.6f 变成 %.6f——"+
			"这个判据抓不住声道调换（素材的左右声道可能已经是同一个信号了）",
			straight, flipped)
	}
	if straight > 0.02 {
		t.Errorf("不换声道时的最大逐样本差 %.6f 已经超过 %.2f", straight, 0.02)
	}
}

// ── 5. COM apartment ───────────────────────────────────────────────────────

// TestSpikeTheCOMApartmentSurvivesLoad 是票面点名要的那一条：
// **在负载下反复跑**。
//
// # 为什么这一条要单独一个用例
//
// COM 的 apartment 是**按线程**分的，而 Go 的 goroutine 不绑线程。三条叠加：
//
//   - goroutine 不绑线程；
//   - Go 1.14 起的异步抢占能在线性代码中间把 goroutine 换个 M
//     （所以「我这个函数没有 channel 所以线程不会变」是错的）；
//   - `syscall.SyscallN` 自己的运行时注释就写着
//     「may reschedule us on to a different M」。
//
// 后果是**偶发**的 `CO_E_NOTINITIALIZED` / `RPC_E_CHANGED_MODE`，
// **小规模测试跑不出来**——所以这一条必须「在负载下反复跑」。
//
// # 这一条量的是什么
//
// 不是「有没有报错」，是**「那条 COM 线程有没有被换掉」**：
// 每次做工作之前比一次 `GetCurrentThreadId`，换过就记一笔。
// 这是把「偶发」变成**一个数**——0 或者不是 0。
func TestSpikeTheCOMApartmentSurvivesLoad(t *testing.T) {
	const (
		workers = 4
		rounds  = 40
		burners = 8
	)

	// 负载：把机器的核占掉，逼调度器换线程。
	stop := make(chan struct{})
	var burn sync.WaitGroup
	for range burners {
		burn.Add(1)
		go func() {
			defer burn.Done()
			x := 0
			for {
				select {
				case <-stop:
					return
				default:
				}
				for range 2000 {
					x = x*1664525 + 1013904223
				}
				_ = x
			}
		}()
	}
	defer func() {
		close(stop)
		burn.Wait()
	}()

	before := comApartmentErrors.Load()

	var wg sync.WaitGroup
	errs := make(chan error, workers)
	switches := make(chan int64, workers)
	workCounts := make(chan int64, workers)
	for w := range workers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			src, err := openFixture("aaclc-stereo.m4a")
			if err != nil {
				errs <- err
				return
			}
			var localSwitches, localWorks int64
			for range rounds {
				codec, err := OpenAACCodec(src.rate, src.chans, src.asc, src.payloadType)
				if err != nil {
					errs <- fmt.Errorf("worker %d: 开解码器：%w", w, err)
					return
				}
				// 喂一帧：线程被换掉的话，COM 那一步最容易露馅。
				frame, err := src.next()
				if err != nil {
					errs <- fmt.Errorf("worker %d: 读帧：%w", w, err)
					codec.Close()
					return
				}
				if _, err := codec.Feed(frame); err != nil {
					errs <- fmt.Errorf("worker %d: 解码：%w", w, err)
					codec.Close()
					return
				}
				localSwitches += codec.ThreadSwitches()
				localWorks += codec.Works()
				codec.Close()
			}
			switches <- localSwitches
			workCounts <- localWorks
		}(w)
	}
	wg.Wait()
	close(errs)
	close(switches)
	close(workCounts)

	for err := range errs {
		t.Errorf("负载下的解码：%v", err)
	}
	var totalSwitches, totalWorks int64
	for n := range switches {
		totalSwitches += n
	}
	for n := range workCounts {
		totalWorks += n
	}
	apartment := comApartmentErrors.Load() - before

	t.Logf("%d 个 worker × %d 轮（共 %d 次开/关解码器），同时 %d 个 goroutine 在烧 CPU",
		workers, rounds, workers*rounds, burners)
	t.Logf("COM 线程被换掉的次数：%d", totalSwitches)
	t.Logf("CO_E_NOTINITIALIZED / RPC_E_CHANGED_MODE 的次数：%d", apartment)
	t.Logf("COM 线程一共做了 %d 件事", totalWorks)
	t.Logf("Go 的线程数：%d，逻辑核：%d", runtime.NumGoroutine(), runtime.NumCPU())

	if totalSwitches != 0 {
		t.Errorf("COM 线程被换掉了 %d 次——`runtime.LockOSThread` 没有守住", totalSwitches)
	}
	if apartment != 0 {
		t.Errorf("出了 %d 次 apartment 错误（CO_E_NOTINITIALIZED / RPC_E_CHANGED_MODE）", apartment)
	}
}

// ── 6. MFT 到底认哪几种输入配置 ────────────────────────────────────────────

// TestSpikeWhichInputTypesTheDecoderAccepts 把「MFT 认哪一种输入配置」问出来。
//
// 这一条是 spike 最有信息量的一条：`SetInputType` 失败时只给一个 HRESULT，
// 而**哪一种配置被认**决定了产品里该怎么喂帧（比如「原来不必合成 ADTS 头」
// 或者「原来 `MF_MT_USER_DATA` 是必需的」）。
//
// 每一种都试一遍，把 MFT 的真实回答记下来——**认和不认都记**。
func TestSpikeWhichInputTypesTheDecoderAccepts(t *testing.T) {
	src, err := openFixture("aaclc-stereo.m4a")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ProbeInputTypes(src.rate, src.chans, src.asc) {
		if p.Err != nil {
			t.Logf("❌ %s：%v", p.Name, p.Err)
			continue
		}
		t.Logf("✅ %s：MFT 认", p.Name)
	}
}

// ── 工具 ────────────────────────────────────────────────────────────────────

// openFixture 打开 testdata 里的一份素材，拆成帧源。
//
// 素材整个读进内存（最大 65 KB）——这一票验的是「能不能解」，不是流式，
// 而读进内存顺带免掉了「文件句柄什么时候关」这件事。
func openFixture(name string) (*frameSource, error) {
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		return nil, err
	}
	switch filepath.Ext(name) {
	case ".m4a":
		return openM4A(bytes.NewReader(b))
	case ".aac":
		return openADTS(bytes.NewReader(b))
	default:
		return nil, fmt.Errorf("不认识的素材 %s", name)
	}
}

// decodeAll 解到源读完，返回帧数、交织样本和耗时。
//
// ⚠️ **最后一定要 Drain**：AAC 解码器有 MDCT 的重叠状态，最后一帧是
// 「和前一帧重叠着」的——不 drain 的话它一直不发，帧数会少一帧
// （172 而不是 173），而判据的下界是 176400 帧（= AudioToolbox 的帧数）。
func decodeAll(t *testing.T, c *Codec, src *frameSource) (int64, []float32, time.Duration) {
	t.Helper()
	started := time.Now()
	var (
		all    []float32
		frames int64
	)
	for {
		frame, err := src.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("第 %d 帧: %v", frames, err)
		}
		out, err := c.Feed(frame)
		if err != nil {
			t.Fatalf("第 %d 帧: %v", frames, err)
		}
		// ⚠️ **某一帧没有输出是正常的**：MFT 会自己攒着，某一帧喂进去不一定
		// 马上有 PCM 出来。真出了问题（解不出任何东西）由下面「一帧都没解
		// 出来」那条兜住——不是由这一帧的空输出兜住。
		all = append(all, out...)
		frames += int64(len(out) / c.Channels())
	}
	tail, err := c.Drain()
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	all = append(all, tail...)
	frames += int64(len(tail) / c.Channels())
	return frames, all, time.Since(started)
}

// swapChannels 把交织样本的左右声道换过来。
func swapChannels(pcm []float32, chans int) []float32 {
	if chans != 2 {
		return pcm
	}
	out := append([]float32(nil), pcm...)
	for i := 0; i+1 < len(out); i += 2 {
		out[i], out[i+1] = out[i+1], out[i]
	}
	return out
}

// bestAlignment 找出两条 PCM 之间差多少个样本对齐最好，返回（偏移，最大逐样本差）。
//
// 偏移是**样本**（不是帧）。正数表示「MF 的比参考的晚这么多」。
//
// 为什么要有这一步：两个解码器对编码器 priming 该剪多少的判断不一样，
// 所以同一段音频的两条 PCM 之间**本来就差几个样本**。不先对齐就逐样本比，
// 比的是「错位」而不是「解码得对不对」。
//
// 搜索范围是 ±4096 个样本（约 ±93 毫秒）—— priming 的差是几十个样本量级。
//
// ⚠️ **搜索时只看前面一小段**：全量比对 8192 个偏移 × 35 万个样本 =
// 几十亿次运算，CI 上会跑到超时。所以先用一小段（`probe`）找出最佳偏移，
// 再在那个偏移上比全量——**判据仍然是全量的最大逐样本差**。
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
	// 在最佳偏移上比全量。
	switch {
	case offset >= 0:
		worst = maxAbsDiffFrom(a[offset:], b)
	default:
		worst = maxAbsDiffFrom(a, b[-offset:])
	}
	return offset, worst
}

// meanAbsDiffFrom 两条 PCM 的平均逐样本差（长度不等时只比公共的那一段）。
//
// 找偏移用它而不是最大差：平均差是**平滑**的，而最大差会被一两个尖峰
// 带着跑，偏移会选得抖。
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
