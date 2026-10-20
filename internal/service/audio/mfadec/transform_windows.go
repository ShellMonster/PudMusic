//go:build windows && (amd64 || arm64)

package mfadec

// 用 `IMFTransform` + `CLSID_CMSAACDecMFT` 解 AAC。
//
// # 为什么是这一条路
//
// 原方案是 `IMFSourceReader`：它**只吃 URL**（喂数据要落盘或自写
// `IMFByteStream`）、**要自己写 COM 回调对象**、异步要桥到 Go channel、
// demux 由 MF 做。
//
// 这一条：`IMFTransform` **我们自己喂帧**、**不需要任何自定义 COM 对象**、
// 同步直接调、demux 由 go-m4a 做（已在依赖里）。
//
// # 这一份是 spike，不是产品实现
//
// 票面（GitHub #128）明写「spike 结论出来之前不要铺开写实现」。所以这一份
// **没有**接进 `internal/service/audio/containers.go` 的注册表——它还
// 没有被产品用上，也正因为如此才叫 spike。结论出来之后要不要铺开、
// 铺成什么样，是下一票的事。
//
// # COM 的每一次调用都在一条钉住的 OS 线程上
//
// 见 `mf_windows.go` 里 `comThread` 那一节：COM 的 apartment 是**按线程**
// 分的，而 Go 的 goroutine 不绑线程。

import (
	"encoding/binary"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"unsafe"
)

// Codec 是一个 AAC 解码器。
//
// 它**不是**并发安全的：一个解码器一条线程，所有方法都在那条 COM 线程上跑。
type Codec struct {
	th  *comThread
	mft uintptr

	// rate/chans 是**输出**的采样率和声道数（从 MFT 的当前输出媒体类型读
	// 回来的，不是我们自己设的那个）。
	rate  int
	chans int
	// outputFloat 是输出是不是 IEEE 浮点（不是的话是 16 位定点整数）。
	outputFloat bool

	// inputVariant 记的是**被 MFT 认下的那一种**输入配置（诊断用）。
	inputVariant string
	// callerAllocatesOutput 记的是输出样本由谁分配。
	//
	// `MFT_OUTPUT_STREAM_INFO` 的 dwFlags 说了算：带
	// `MFT_OUTPUT_STREAM_PROVIDES_SAMPLES` 或者
	// `MFT_OUTPUT_STREAM_CAN_PROVIDE_SAMPLES` 的话，**pSample 传 NULL**
	// 让 MFT 自己分配；两个都没有的话，**必须**由调用方分配。
	callerAllocatesOutput bool
	// outputSize 是 `MFT_OUTPUT_STREAM_INFO` 的 cbSize：输出样本最大多少
	// 字节。调用方自己分配的时候要用它。
	outputSize int

	closeOnce sync.Once
	closed    atomic.Bool
}

// OpenAACCodec 开一个 AAC 解码器。
//
// rate/chans 是**核心**码流的采样率和声道数（HE-AAC 的 SBR 会把采样率翻倍、
// PS 会把声道从单声道变成立体声，那是解码之后的事）。asc 是
// AudioSpecificConfig，裸 AAC 那一路必须带它——解码器要靠它知道采样率和声道。
//
// payloadType 是 `MF_MT_AAC_PAYLOAD_TYPE`：0 = 裸 AAC，1 = ADTS。
func OpenAACCodec(rate, chans int, asc []byte, payloadType uint32) (*Codec, error) {
	if err := bindingsError(); err != nil {
		return nil, err
	}
	th, err := newComThread()
	if err != nil {
		return nil, err
	}

	c := &Codec{th: th}
	var openErr error
	th.do(func() { openErr = c.open(rate, chans, asc, payloadType) })
	if openErr != nil {
		th.close()
		return nil, openErr
	}
	return c, nil
}

// ── 建 MFT：挨个试 ──────────────────────────────────────────────────────────

// inputVariants 是要挨个试的输入配置，**按这个顺序**。
//
// 为什么要有「挨个试」：MFT 认哪种配置，是这台 Windows 上的事实，不是我们
// 能推出来的——SDK 的文档只写了「`MFAudioFormat_AAC` 认裸 AAC 和 ADTS」，
// 但**`MF_MT_USER_DATA` 该是哪种形状**、**要不要带**，文档没写死
// （`um/mmreg.h` 讲的是 WAVEFORMATEX 那个世界，微软的 AAC Decoder 页面讲的
// 又是「去掉 WAVEFORMATEX 的那一段」——两处对不上，只能问机器）。
//
// ⚠️ **payloadType 是调用方定的，不能换**：调用方手里是裸 AAC 帧还是带 ADTS
// 头的帧，是**它自己的事**——换一种喂法，喂进去的字节就对不上了。所以这里
// 只在**同一个 payload type** 上换 user data 这一位。
//
// 认了哪一个，spike 会打出来（`TestSpikeWhichInputTypesTheDecoderAccepts`
// 那边把各种组合都试一遍），产品那一票照着它接就行。
func inputVariants(payloadType uint32) []inputVariant {
	name := "裸 AAC"
	if payloadType == aacPayloadADTS {
		name = "ADTS"
	}
	return []inputVariant{
		{name: name + " + MF_MT_USER_DATA（12 字节尾巴 + ASC）", payloadType: payloadType, userData: userDataTail},
		{name: name + " + MF_MT_USER_DATA（整个 HEAACWAVEFORMAT）", payloadType: payloadType, userData: userDataFull},
		{name: name + "，不带 MF_MT_USER_DATA", payloadType: payloadType, userData: userDataNone},
	}
}

// open 是真正建 MFT 的那部分，跑在 COM 线程上。
func (c *Codec) open(rate, chans int, asc []byte, payloadType uint32) error {
	// 两种顺序（先输入后输出 / 先输出后输入）× 若干输入配置，挨个试。
	//
	// ⚠️ **每次都建一个全新的 MFT**：换输入媒体类型之前要 flush
	// （微软的 AAC Decoder 页面明写「the AAC decoder does not handle
	// dynamic format changes, and must be flushed or drained before a new
	// input media type is set」），同一个 MFT 上试不出第二个组合。
	//
	// 为什么还要试顺序：有的 MFT 要求**先**设输出类型（输入类型是拿它校验的），
	// 有的是反过来。文档没写死这一条，那就两种都试。
	var lastErr error
	tries := 0
	for _, inputFirst := range []bool{true, false} {
		for _, v := range inputVariants(payloadType) {
			tries++
			mft, err := c.createMFT()
			if err != nil {
				lastErr = err
				continue
			}
			err = c.tryOpen(mft, rate, chans, asc, v, inputFirst)
			if err == nil {
				c.mft = mft
				c.inputVariant = v.name
				return nil
			}
			// ⚠️ 这一步不能省：不放掉的话，失败的每一次都漏一个 COM 引用。
			release(mft)
			lastErr = err
		}
	}
	return fmt.Errorf("mfadec: 建不起一个能用的解码器（试了 %d 种组合，最后一次是）：%w", tries, lastErr)
}

// createMFT 建一个 AAC 解码 MFT。
func (c *Codec) createMFT() (uintptr, error) {
	clsid := guidCLSIDCMSAACDecMFT
	iid := guidIIDIMFTransform
	var mft uintptr
	if hr := coCreateInstance(&clsid, &iid, &mft); hr < 0 {
		return 0, hresultError("CoCreateInstance(CLSID_CMSAACDecMFT)", hr)
	}
	return mft, nil
}

// tryOpen 试一种组合：建媒体类型、设进去、读回输出类型。
func (c *Codec) tryOpen(mft uintptr, rate, chans int, asc []byte, v inputVariant, inputFirst bool) error {
	in, err := c.buildInputType(rate, chans, asc, v)
	if err != nil {
		return err
	}
	defer release(in)

	out, err := c.buildOutputType(rate, chans)
	if err != nil {
		return err
	}
	defer release(out)

	setIn := func() error {
		if hr := setInputType(mft, in); hr < 0 {
			return hresultError("SetInputType", hr)
		}
		return nil
	}
	setOut := func() error {
		if hr := setOutputType(mft, out); hr < 0 {
			// 退一步：把媒体类型交给 MFT 自己挑（pType 传 NULL）。
			//
			// 文档说 `SetOutputType` 的 pType 可以是 NULL——那表示「你挑一个」。
			// 自带的 AAC 解码器认 PCM，但具体是 16 位还是浮点、采样率多少，
			// 由它自己定；定完再读回来就是了（下面就是读回来的）。
			if hr2 := setOutputType(mft, 0); hr2 < 0 {
				return hresultError("SetOutputType", hr)
			}
		}
		return nil
	}

	if inputFirst {
		if err := setIn(); err != nil {
			return err
		}
		if err := setOut(); err != nil {
			return err
		}
	} else {
		if err := setOut(); err != nil {
			return err
		}
		if err := setIn(); err != nil {
			return err
		}
	}

	// 输出到底是多少采样率、几个声道、什么格式，**读回来**——不要拿自己设的
	// 值当答案。HE-AAC 的 SBR 会把采样率翻倍，设 44100 出来的可能是 88200。
	cur, err := outputCurrentType(mft)
	if err != nil {
		return err
	}
	defer release(cur)

	if rate, ok, _ := getUINT32(cur, &guidMTAudioSamplesPerSecond); ok {
		c.rate = int(rate)
	}
	if chans, ok, _ := getUINT32(cur, &guidMTAudioNumChannels); ok {
		c.chans = int(chans)
	}
	if c.rate == 0 || c.chans == 0 {
		return fmt.Errorf("mfadec: 输出媒体类型里没有采样率或声道数")
	}

	// ⚠️ **输出格式必须核**：不核的话，「MFT 挑了浮点输出」会被当成
	// 「16 位整数 PCM」去读——浮点的比特被当成 int16，得到的是一片看着像
	// PCM 的垃圾，而症状是「RMS 不对」而不是报错。
	sub, ok, _ := getGUID(cur, &guidMTSubtype)
	if !ok {
		return fmt.Errorf("mfadec: 输出媒体类型里没有子类型")
	}
	switch {
	case sub.equal(guidAudioFormatPCM):
		bits, ok, _ := getUINT32(cur, &guidMTAudioBitsPerSample)
		if !ok || bits == 0 {
			bits = 16
		}
		if bits != 16 {
			return fmt.Errorf("mfadec: 输出是 %d 位定点 PCM，本 spike 要的是 16 位", bits)
		}
		c.outputFloat = false
	case sub.equal(guidAudioFormatFloat):
		bits, ok, _ := getUINT32(cur, &guidMTAudioBitsPerSample)
		if !ok || bits == 0 {
			bits = 32
		}
		if bits != 32 {
			return fmt.Errorf("mfadec: 输出是 %d 位浮点，本 spike 要的是 32 位", bits)
		}
		c.outputFloat = true
	default:
		return fmt.Errorf("mfadec: 输出的子类型是 %s，既不是 PCM 也不是浮点", sub)
	}

	// 输出样本由谁分配，看 `MFT_OUTPUT_STREAM_INFO` 的 dwFlags。
	var info mftOutputStreamInfo
	hr := comHR(comCall(mft, vtblTransformGetOutputStreamInfo, 0,
		uintptr(unsafe.Pointer(&info))))
	if hr < 0 {
		return hresultError("GetOutputStreamInfo", hr)
	}
	c.callerAllocatesOutput = info.Flags&(mftOutputStreamProvidesSamples|mftOutputStreamCanProvideSamples) == 0
	c.outputSize = int(info.Size)
	if c.outputSize <= 0 {
		c.outputSize = outputBufferBytes
	}
	return nil
}

// buildInputType 造输入媒体类型。
func (c *Codec) buildInputType(rate, chans int, asc []byte, v inputVariant) (uintptr, error) {
	var mt uintptr
	if hr := mfCreateMediaType(&mt); hr < 0 {
		return 0, hresultError("MFCreateMediaType", hr)
	}
	major := guidMediaTypeAudio
	sub := guidAudioFormatAAC
	if hr := setGUID(mt, &guidMTMajorType, &major); hr < 0 {
		release(mt)
		return 0, hresultError("SetGUID(MF_MT_MAJOR_TYPE)", hr)
	}
	if hr := setGUID(mt, &guidMTSubtype, &sub); hr < 0 {
		release(mt)
		return 0, hresultError("SetGUID(MF_MT_SUBTYPE)", hr)
	}
	if hr := setUINT32(mt, &guidMTAudioSamplesPerSecond, uint32(rate)); hr < 0 {
		release(mt)
		return 0, hresultError("SetUINT32(MF_MT_AUDIO_SAMPLES_PER_SECOND)", hr)
	}
	if hr := setUINT32(mt, &guidMTAudioNumChannels, uint32(chans)); hr < 0 {
		release(mt)
		return 0, hresultError("SetUINT32(MF_MT_AUDIO_NUM_CHANNELS)", hr)
	}
	if hr := setUINT32(mt, &guidMTAACPayloadType, v.payloadType); hr < 0 {
		release(mt)
		return 0, hresultError("SetUINT32(MF_MT_AAC_PAYLOAD_TYPE)", hr)
	}
	blob := userDataBlob(rate, chans, v, asc)
	if len(blob) > 0 {
		if hr := setBlob(mt, &guidMTUserData, blob); hr < 0 {
			release(mt)
			return 0, hresultError("SetBlob(MF_MT_USER_DATA)", hr)
		}
	}
	return mt, nil
}

// buildOutputType 造输出媒体类型：16 位定点 PCM。
func (c *Codec) buildOutputType(rate, chans int) (uintptr, error) {
	var mt uintptr
	if hr := mfCreateMediaType(&mt); hr < 0 {
		return 0, hresultError("MFCreateMediaType", hr)
	}
	major := guidMediaTypeAudio
	sub := guidAudioFormatPCM
	if hr := setGUID(mt, &guidMTMajorType, &major); hr < 0 {
		release(mt)
		return 0, hresultError("SetGUID(MF_MT_MAJOR_TYPE)", hr)
	}
	if hr := setGUID(mt, &guidMTSubtype, &sub); hr < 0 {
		release(mt)
		return 0, hresultError("SetGUID(MF_MT_SUBTYPE)", hr)
	}
	if hr := setUINT32(mt, &guidMTAudioBitsPerSample, 16); hr < 0 {
		release(mt)
		return 0, hresultError("SetUINT32(MF_MT_AUDIO_BITS_PER_SAMPLE)", hr)
	}
	if hr := setUINT32(mt, &guidMTAudioSamplesPerSecond, uint32(rate)); hr < 0 {
		release(mt)
		return 0, hresultError("SetUINT32(MF_MT_AUDIO_SAMPLES_PER_SECOND)", hr)
	}
	if hr := setUINT32(mt, &guidMTAudioNumChannels, uint32(chans)); hr < 0 {
		release(mt)
		return 0, hresultError("SetUINT32(MF_MT_AUDIO_NUM_CHANNELS)", hr)
	}
	if hr := setUINT32(mt, &guidMTAudioBlockAlignment, uint32(chans*2)); hr < 0 {
		release(mt)
		return 0, hresultError("SetUINT32(MF_MT_AUDIO_BLOCK_ALIGNMENT)", hr)
	}
	if hr := setUINT32(mt, &guidMTAudioAvgBytesPerSecond, uint32(rate*chans*2)); hr < 0 {
		release(mt)
		return 0, hresultError("SetUINT32(MF_MT_AUDIO_AVG_BYTES_PER_SECOND)", hr)
	}
	return mt, nil
}

// setInputType 设输入媒体类型。
//
//go:uintptrescapes
func setInputType(mft uintptr, mt uintptr) hresult {
	return comHR(comCall(mft, vtblTransformSetInputType, 0,
		uintptr(unsafe.Pointer(&mt)), 0))
}

// setOutputType 设输出媒体类型。
//
// mt 传 0 表示「你挑一个」（pType 传 **NULL** 给 MFT）。
//
// ⚠️ 传 NULL 时**不能**写 `uintptr(unsafe.Pointer(&mt))`——那传出去的是
// 「一个指向栈上变量的指针」，MFT 会把它当成 IMFMediaType* 去解引用，
// 于是读到一个随机的对象。必须传**字面的 0**。
//
//go:uintptrescapes
func setOutputType(mft uintptr, mt uintptr) hresult {
	var arg uintptr
	if mt != 0 {
		arg = uintptr(unsafe.Pointer(&mt))
	}
	return comHR(comCall(mft, vtblTransformSetOutputType, 0, arg, 0))
}

// outputCurrentType 问 MFT 当前用的输出媒体类型。
//
//go:uintptrescapes
func outputCurrentType(mft uintptr) (uintptr, error) {
	var out uintptr
	hr := comHR(comCall(mft, vtblTransformGetOutputCurrentType, 0,
		uintptr(unsafe.Pointer(&out))))
	if hr < 0 {
		return 0, hresultError("GetOutputCurrentType", hr)
	}
	return out, nil
}

// InputProbe 是一次「试一种输入配置」的结果（诊断用）。
type InputProbe struct {
	// Name 是那种配置的名字。
	Name string
	// Err 是成不成功（nil = MFT 认了）。
	Err error
}

// ProbeInputTypes 挨个试各种输入配置，把 MFT 的真实回答记下来。
//
// 这是一条**诊断**：票面要「说清楚」，而「说清楚」的前提是**先问过机器**。
// 「MFT 认哪一种」不是能推出来的——SDK 的文档和 `um/mmreg.h` 在
// `MF_MT_USER_DATA` 该是什么形状这件事上**对不上**，所以两种都试，
// 认了哪一个由这里给出答案。
//
// 它**自己**建 MFT，不依赖某一个已经打开的解码器——所以「主路径打不开」
// 的时候，这条依然能给出答案。
func ProbeInputTypes(rate, chans int, asc []byte) []InputProbe {
	out := []InputProbe{}
	if err := bindingsError(); err != nil {
		return append(out, InputProbe{Name: "（系统函数没绑上）", Err: err})
	}
	th, err := newComThread()
	if err != nil {
		return append(out, InputProbe{Name: "（起不了 COM 线程）", Err: err})
	}
	defer th.close()

	var probes []InputProbe
	th.do(func() {
		c := &Codec{th: th}
		for _, v := range inputVariants(aacPayloadRaw) {
			p := InputProbe{Name: v.name}
			mft, err := c.createMFT()
			if err != nil {
				p.Err = err
				probes = append(probes, p)
				continue
			}
			// 两种顺序都试：先输入后输出，和先输出后输入。
			var last error
			for _, inputFirst := range []bool{true, false} {
				if err := c.tryOpen(mft, rate, chans, asc, v, inputFirst); err != nil {
					last = err
					continue
				}
				last = nil
				break
			}
			release(mft)
			p.Err = last
			probes = append(probes, p)
		}
	})
	return append(out, probes...)
}

// SampleRate 是输出采样率。
func (c *Codec) SampleRate() int { return c.rate }

// Channels 是输出声道数。
func (c *Codec) Channels() int { return c.chans }

// OutputIsFloat 记的是输出是不是 IEEE 浮点（不是的话是 16 位定点整数）。
func (c *Codec) OutputIsFloat() bool { return c.outputFloat }

// CallerAllocatesOutput 记的是输出样本由谁分配（诊断用）。
func (c *Codec) CallerAllocatesOutput() bool { return c.callerAllocatesOutput }

// InputVariant 记的是被 MFT 认下的那一种输入配置（诊断用）。
//
// 「MFT 到底认哪一种」是这台 Windows 上的事实，不是能推出来的——报告里
// 写的那个名字，就是产品那一票该照着接的。
func (c *Codec) InputVariant() string { return c.inputVariant }

// ThreadSwitches 是「那条 COM 线程在干活的时候被换掉过几次」。
//
// 这是把「偶发的 apartment 错误」变成**一个数**的地方：0 或者不是 0。
// 见 `mf_windows.go` 里 `comThread` 那一节的说明。
func (c *Codec) ThreadSwitches() int64 { return c.th.switches.Load() }

// Works 是那条 COM 线程一共做了多少件事（诊断用）。
func (c *Codec) Works() int64 { return c.th.workCount() }

// Feed 喂一帧 AAC，返回这一次解出来的 PCM（[-1,1] 之间的 float32、交织）。
//
// 返回空切片是**正常**的：MFT 会攒着，某一帧喂进去不一定马上有输出。
func (c *Codec) Feed(frame []byte) ([]float32, error) {
	if c.closed.Load() {
		return nil, fmt.Errorf("mfadec: 解码器已经关了")
	}
	var out []float32
	var err error
	c.th.do(func() { out, err = c.feed(frame) })
	return out, err
}

// feed 是真正解码的那部分，跑在 COM 线程上。
func (c *Codec) feed(frame []byte) ([]float32, error) {
	sample, buf, err := c.makeSample(frame)
	if err != nil {
		return nil, err
	}
	defer release(sample)
	defer release(buf)

	hr := comHR(comCall(c.mft, vtblTransformProcessInput, 0,
		uintptr(unsafe.Pointer(&sample)), 0))
	if hr < 0 {
		if uint32(hr) == hrMFNotAccepting {
			// 「现在不收输入」——先把已有的输出取干净，再重试一次。
			if _, err := c.pullOutput(); err != nil {
				return nil, err
			}
			hr = comHR(comCall(c.mft, vtblTransformProcessInput, 0,
				uintptr(unsafe.Pointer(&sample)), 0))
		}
		if hr < 0 {
			return nil, hresultError("ProcessInput", hr)
		}
	}
	return c.pullOutput()
}

// pullOutput 把 MFT 手里的输出全取出来。
//
// ⚠️ **有迭代上限**：MFT 一直说「还有更多」（`MFT_OUTPUT_DATA_BUFFER_INCOMPLETE`）
// 而不往前挪的时候，无限循环就是「CI 跑到超时」而不是「快速失败」。
func (c *Codec) pullOutput() ([]float32, error) {
	var pcm []float32
	for range maxOutputPulls {
		chunk, done, err := c.processOutput()
		if err != nil {
			return nil, err
		}
		pcm = append(pcm, chunk...)
		if done {
			return pcm, nil
		}
	}
	return nil, fmt.Errorf("mfadec: 取了 %d 趟输出还没取完（MFT 一直说 INCOMPLETE）", maxOutputPulls)
}

// maxOutputPulls 是一次取输出最多拉几趟。
//
// 一帧 AAC 是 1024 个样本，输出也就那么点——拉几十趟还拉不完，就是**卡住了**，
// 该快速失败而不是把 CI 拖到超时。
const maxOutputPulls = 64

// Drain 告诉 MFT 流结束了，把它手里攒着的帧放出来。
//
// ⚠️ **不 drain 会丢最后一帧**：AAC 解码器有 MDCT 的重叠状态，最后一帧是
// 「和前一帧重叠着」的，只喂不解就没有输出。少了它，解出来的帧数会少一帧
// （172 帧而不是 173 帧），于是「帧数落在参考区间里」那条判据就红。
//
// 这条对应 SDK 的 `MFT_MESSAGE_COMMAND_DRAIN`（值 0x1）加上
// `MFT_DRAIN_PRODUCE_TAILS`（值 0）。
func (c *Codec) Drain() ([]float32, error) {
	if c.closed.Load() {
		return nil, fmt.Errorf("mfadec: 解码器已经关了")
	}
	var out []float32
	var err error
	c.th.do(func() {
		hr := comHR(comCall(c.mft, vtblTransformProcessMessage,
			mftMessageCommandDrain, mftDrainProduceTails))
		if hr < 0 {
			err = hresultError("ProcessMessage(MFT_MESSAGE_COMMAND_DRAIN)", hr)
			return
		}
		out, err = c.pullOutput()
	})
	return out, err
}

// SDK 的 `MFT_MESSAGE_TYPE` 和 `MFT_DRAIN_TYPE`。
const (
	mftMessageCommandDrain = 0x1
	mftDrainProduceTails   = 0
)

// processOutput 拉一次输出。
//
// 返回的 done 是 true 表示「这一趟没有更多输出了」。
//
//go:uintptrescapes
func (c *Codec) processOutput() ([]float32, bool, error) {
	var out mftOutputDataBuffer
	// pSample 传 0 = 让 MFT 自己分配样本。
	//
	// 只有在 `MFT_OUTPUT_STREAM_INFO` 的 dwFlags 里既没有
	// PROVIDES_SAMPLES 也没有 CAN_PROVIDE_SAMPLES 时，调用方才**必须**
	// 自己分配——那一条路下面单独处理。
	var ours, oursBuf uintptr
	if c.callerAllocatesOutput {
		s, buf, err := c.makeEmptySample(c.outputSize)
		if err != nil {
			return nil, true, err
		}
		ours, oursBuf = s, buf
		defer release(ours)
		defer release(oursBuf)
		out.Sample = ours
	}

	var status uint32
	hr := comHR(comCall(c.mft, vtblTransformProcessOutput, 0, 1,
		uintptr(unsafe.Pointer(&out)),
		uintptr(unsafe.Pointer(&status))))
	if hr < 0 {
		if uint32(hr) == hrTransformNeedMoreInput {
			return nil, true, nil
		}
		return nil, true, hresultError("ProcessOutput", hr)
	}
	// dwStatus 里那三个值（0x100 / 0x200 / 0x300）**不是互斥的位**，
	// 是三个互斥的取值，所以这里按位取一段再比，而不是逐个 &。
	switch out.Status & 0x00000300 {
	case mftOutputDataFormatChange:
		// 输出格式变了：按文档这时要**重新问一次输出媒体类型**然后接着走。
		// 这一票的素材不会走到这里（解码器不处理动态格式变化），但真走到了
		// 不能当成「这一趟没输出」糊过去。
		cur, err := outputCurrentType(c.mft)
		if err != nil {
			return nil, true, err
		}
		release(cur)
		return nil, false, nil
	case mftOutputDataStreamEnd:
		// 流到头了——但**这一趟可能还带着样本**，所以先把它读完再收工。
		if out.Sample == 0 {
			return nil, true, nil
		}
		defer release(out.Sample)
		pcm, err := c.readSample(out.Sample)
		if err != nil {
			return nil, true, err
		}
		return pcm, true, nil
	case mftOutputDataNoSample:
		return nil, true, nil
	}
	if out.Sample == 0 {
		return nil, true, nil
	}
	// MFT 自己分配的样本要放掉；我们自己分配的由上面的 defer 放。
	if ours == 0 {
		defer release(out.Sample)
	}

	pcm, err := c.readSample(out.Sample)
	if err != nil {
		return nil, true, err
	}
	// INCOMPLETE 说明还有输出没取完。
	return pcm, out.Status&mftOutputDataIncomplete == 0, nil
}

// outputBufferBytes 是「由调用方分配输出样本」那条路里给的缓冲大小。
//
// AAC 一帧是 1024 个样本，最多 6 个声道、16 位定点：1024 * 6 * 2 = 12288
// 字节。给 64 KB 是为了让「解码器多给几帧」也装得下。
const outputBufferBytes = 64 * 1024

// makeSample 把一帧 AAC 装成一个 IMFSample。
//
//go:uintptrescapes
func (c *Codec) makeSample(frame []byte) (uintptr, uintptr, error) {
	var buf uintptr
	if hr := mfCreateMemoryBuffer(uint32(len(frame)), &buf); hr < 0 {
		return 0, 0, hresultError("MFCreateMemoryBuffer", hr)
	}
	var p *byte
	var maxLen, curLen uint32
	hr := comHR(comCall(buf, vtblBufferLock,
		uintptr(unsafe.Pointer(&p)),
		uintptr(unsafe.Pointer(&maxLen)),
		uintptr(unsafe.Pointer(&curLen)), 0))
	if hr < 0 {
		release(buf)
		return 0, 0, hresultError("IMFMediaBuffer::Lock", hr)
	}
	// 缓冲是我们自己按 len(frame) 要来的，理论上装得下——但**万一装不下**，
	// 往里 copy 就是越界写（踩的是 MF 的堆）。
	if uint32(len(frame)) > maxLen {
		comCall(buf, vtblBufferUnlock)
		release(buf)
		return 0, 0, fmt.Errorf("mfadec: 缓冲只有 %d 字节，装不下 %d 字节的一帧", maxLen, len(frame))
	}
	copy(unsafe.Slice(p, len(frame)), frame)
	comCall(buf, vtblBufferUnlock)

	hr = comHR(comCall(buf, vtblBufferSetCurrentLength, uintptr(len(frame))))
	if hr < 0 {
		release(buf)
		return 0, 0, hresultError("IMFMediaBuffer::SetCurrentLength", hr)
	}

	var sample uintptr
	if hr := mfCreateSample(&sample); hr < 0 {
		release(buf)
		return 0, 0, hresultError("MFCreateSample", hr)
	}
	hr = comHR(comCall(sample, vtblSampleAddBuffer, uintptr(unsafe.Pointer(&buf))))
	if hr < 0 {
		release(sample)
		release(buf)
		return 0, 0, hresultError("IMFSample::AddBuffer", hr)
	}
	return sample, buf, nil
}

// makeEmptySample 造一个**空**的 IMFSample（带一个够大的缓冲）。
//
// 「输出样本由调用方分配」那条路要用它：文档说「如果 MFT 两种标志都没有，
// 调用方必须分配样本，而且样本里的缓冲要装得下输出数据」。
//
//go:uintptrescapes
func (c *Codec) makeEmptySample(size int) (uintptr, uintptr, error) {
	var buf uintptr
	if hr := mfCreateMemoryBuffer(uint32(size), &buf); hr < 0 {
		return 0, 0, hresultError("MFCreateMemoryBuffer", hr)
	}
	var sample uintptr
	if hr := mfCreateSample(&sample); hr < 0 {
		release(buf)
		return 0, 0, hresultError("MFCreateSample", hr)
	}
	hr := comHR(comCall(sample, vtblSampleAddBuffer, uintptr(unsafe.Pointer(&buf))))
	if hr < 0 {
		release(sample)
		release(buf)
		return 0, 0, hresultError("IMFSample::AddBuffer", hr)
	}
	return sample, buf, nil
}

// readSample 把一个输出样本里的 PCM 读成 [-1,1] 的 float32 交织样本。
//
// 用的是 `IMFSample::ConvertToContiguousBuffer` 而不是
// `GetBufferByIndex(0)`：前一个拿到的是**整个样本**（哪怕它有多个缓冲），
// 后一个只拿到第 0 个——MFT 给的样本不止一个缓冲时，PCM 会被悄悄截断。
//
//go:uintptrescapes
func (c *Codec) readSample(sample uintptr) ([]float32, error) {
	var buf uintptr
	hr := comHR(comCall(sample, vtblSampleConvertToContig,
		uintptr(unsafe.Pointer(&buf))))
	if hr < 0 {
		return nil, hresultError("IMFSample::ConvertToContiguousBuffer", hr)
	}
	defer release(buf)

	var p *byte
	var maxLen, curLen uint32
	hr = comHR(comCall(buf, vtblBufferLock,
		uintptr(unsafe.Pointer(&p)),
		uintptr(unsafe.Pointer(&maxLen)),
		uintptr(unsafe.Pointer(&curLen)), 0))
	if hr < 0 {
		return nil, hresultError("IMFMediaBuffer::Lock", hr)
	}
	defer comCall(buf, vtblBufferUnlock)

	raw := unsafe.Slice(p, curLen)
	if c.outputFloat {
		if len(raw)%4 != 0 {
			return nil, fmt.Errorf("mfadec: 浮点输出有 %d 字节，不是 4 的倍数", len(raw))
		}
		out := make([]float32, len(raw)/4)
		for i := range out {
			out[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
		}
		return out, nil
	}
	if len(raw)%2 != 0 {
		return nil, fmt.Errorf("mfadec: 16 位输出有 %d 字节，不是 2 的倍数", len(raw))
	}
	out := make([]float32, len(raw)/2)
	for i := range out {
		out[i] = float32(int16(binary.LittleEndian.Uint16(raw[i*2:]))) / 32768.0
	}
	return out, nil
}

// Close 把解码器和它那条 COM 线程一起收掉。
//
// **调几次都安全**（内部是 `sync.Once`）——原来的写法调第二次会
// `panic: close of closed channel`。
func (c *Codec) Close() error {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		c.th.do(func() { release(c.mft) })
		c.th.close()
	})
	return nil
}
