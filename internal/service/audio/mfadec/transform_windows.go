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
	"errors"
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
	// 先自己验一遍 `IMFSample` 的虚表下标对不对——**在真机上**，不是靠推。
	if err := verifySampleVTable(); err != nil {
		return err
	}

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

// # COM 的参数：接口指针 vs 指针的指针
//
// ⚠️⚠️ **这一层踩过坑，而且坑的是「指针层级」**——COM 的方法有两种参数：
//
//   - **接口指针**（`IMFMediaBuffer *pBuffer`、`IMFMediaType *pType`、
//     `IMFSample *pSample`）：传**那个对象本身**的值。
//   - **指针的指针**（`IMFMediaBuffer **ppBuffer`、`IMFMediaType **ppType`、
//     `UINT32 *pValue`）：传**指向那个变量的地址**。
//
// 写反了（给接口指针的参数传了 `&obj`）的后果**不是报错**：COM 那边会把
// 「一个指向栈上变量的指针」当成对象来解引用，然后去调它的虚表——
// 读到的是一串栈上的字节当函数指针，跳进去就崩。症状是「一调就死」，而且
// 死在哪一行很难看出来。
//
// 本包用到的每一处，和它的 SDK 签名：
//
//	IMFSample::AddBuffer            (IMFMediaBuffer *pBuffer)          → 传对象
//	IMFSample::GetBufferCount       (UINT32 *pcbBuffers)               → 传地址
//	IMFSample::GetBufferByIndex     (DWORD, IMFMediaBuffer **ppBuffer)  → 传地址
//	IMFSample::ConvertToContiguousBuffer (IMFMediaBuffer **ppBuffer)    → 传地址
//	IMFMediaBuffer::Lock            (BYTE **, DWORD *, DWORD *, DWORD) → 传地址
//	IMFMediaBuffer::SetCurrentLength(DWORD cbCurrentLength)            → 传值
//	IMFTransform::SetInputType      (DWORD, IMFMediaType *pType, DWORD)→ 传对象
//	IMFTransform::SetOutputType     (DWORD, IMFMediaType *pType, DWORD)→ 传对象
//	IMFTransform::GetOutputCurrentType (DWORD, IMFMediaType **ppType)   → 传地址
//	IMFTransform::GetOutputStreamInfo (DWORD, MFT_OUTPUT_STREAM_INFO *) → 传地址
//	IMFTransform::ProcessInput      (DWORD, IMFSample *pSample, DWORD) → 传对象
//	IMFTransform::ProcessOutput     (DWORD, DWORD, MFT_OUTPUT_DATA_BUFFER *, DWORD *) → 传地址
//	IMFTransform::ProcessMessage    (MFT_MESSAGE_TYPE, ULONG_PTR)      → 传值
//
// `verifySampleVTable()`（开解码器的时候跑）就是**在真机上**验这一层：
// 加一个缓冲，数一遍——下标错或者层级错，数的就不是 1。

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

// verifySampleVTable 断 `IMFSample` 的虚表下标和 SDK 头文件对得上。
//
// # 为什么要有这一条
//
// `IMFSample` 的下标有过一次分歧：SDK 头文件（`um/mfobjects.h`）的类声明写
// 「继承 `IMFAttributes`」——祖先 33 格（3 + 30），所以 `AddBuffer` 在第
// **42** 格。而「`IMFSample` 继承 `IMFMediaType`」的印象会让祖先变成 38 格，
// `AddBuffer` 就落到第 **47** 格；可 `IMFSampleVtbl` **只有 47 格（0~46）**，
// 读第 47 格是**越过数组末尾**，拿到的是一串垃圾，当函数指针调下去就是崩。
//
// 头文件说的和**机器**说的要一致才算数，所以这里在真机上自己验一遍：
//
//  1. 建一个空的 `IMFSample`。
//  2. 往里加一个缓冲。
//  3. 问它现在有几个缓冲——**必须是 1**。
//
// # 先做不会出事的那一半，再做会出事的那一半
//
// **先数一个空样本的缓冲数**（必须是 0）。这一步在两种下标假设下都**不会写坏
// 东西**：
//
//   - 第 39 格真是 `GetBufferCount`：它往我们给的 `UINT32` 里写 0；
//   - 第 39 格是「38 祖先」那套里的 `SetSampleDuration`：它**按值**收一个
//     DWORD，什么都不写——我们放的哨兵原封不动，于是判出「不对」。
//
// **确认了 39 之后才去 `AddBuffer`**。这一步是唯一可能出事的：如果下标错了，
// 第 42 格可能是 `GetSampleDuration`，它会往传进去的指针里写 8 个字节——而
// 我们传的是那个 `IMFMediaBuffer` 对象本身，写进去就**踩坏它的虚表指针**，
// 后面 `release` 时崩。但 39 已经对上了，说明祖先就是 33 格、整套布局就是这个
// 形状，42 也就必然是 `AddBuffer`。
//
// ⚠️ 所以这一条的价值是「在布局确实对的时候，确认运行时行为和头文件一致」；
// **它不能证明布局错的时候会干净地报错**——那要靠 `guid_test.go` 对着 SDK 的
// `IMFSampleVtbl` 一条一条数出来的下标。
func verifySampleVTable() error {
	var buf uintptr
	if hr := mfCreateMemoryBuffer(16, &buf); hr < 0 {
		return hresultError("MFCreateMemoryBuffer", hr)
	}
	defer release(buf)

	var sample uintptr
	if hr := mfCreateSample(&sample); hr < 0 {
		return hresultError("MFCreateSample", hr)
	}
	defer release(sample)

	// ① 空样本：缓冲数必须是 0。哨兵保证「没人写」也能判出来。
	const sentinel = 0xDEADBEEF
	n := uint32(sentinel)
	if hr := comHR(comCall(sample, vtblSampleGetBufferCount,
		uintptr(unsafe.Pointer(&n)))); hr < 0 {
		return hresultError("IMFSample::GetBufferCount", hr)
	}
	if n != 0 {
		return fmt.Errorf("mfadec: IMFSample 的虚表下标不对——一个**空**样本读回来有 %d 个缓冲"+
			"（期望 0；哨兵 %#x 还在就说明第 %d 格根本不是 GetBufferCount）。"+
			"这说明祖先槽位数不是 33，整个 IMFSample 的下标都错了。",
			n, sentinel, vtblSampleGetBufferCount)
	}

	// ② 布局已经确认了，再加一个缓冲、数一遍。
	if hr := comHR(comCall(sample, vtblSampleAddBuffer, buf)); hr < 0 {
		return hresultError("IMFSample::AddBuffer", hr)
	}
	n = uint32(sentinel)
	if hr := comHR(comCall(sample, vtblSampleGetBufferCount,
		uintptr(unsafe.Pointer(&n)))); hr < 0 {
		return hresultError("IMFSample::GetBufferCount", hr)
	}
	if n != 1 {
		return fmt.Errorf("mfadec: IMFSample 的虚表下标不对——加了一个缓冲，读回来有 %d 个"+
			"（期望 1；哨兵 %#x 还在就说明那一格根本不是 GetBufferCount）", n, sentinel)
	}
	return nil
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
	if err := c.adoptOutputType(cur); err != nil {
		return err
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
	return comHR(comCall(mft, vtblTransformSetInputType, 0, mt, 0))
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
	return comHR(comCall(mft, vtblTransformSetOutputType, 0, mt, 0))
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
//
// payloadType 是调用方手里那种字节的喂法（0 = 裸 AAC，1 = ADTS）。**两种都要
// 各问一遍**：裸 AAC 和 ADTS 在解码器眼里是同一栏的两个取值，但「它认不认」
// 是两回事，而报告里要的是**两种容器各认哪一种配置**。
func ProbeInputTypes(rate, chans int, asc []byte, payloadType uint32) []InputProbe {
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
		for _, v := range inputVariants(payloadType) {
			p := InputProbe{Name: v.name}
			// ⚠️ **每一种顺序都建一个全新的 MFT**。
			//
			// 微软的 AAC Decoder 页面明写「the AAC decoder does not handle
			// dynamic format changes, and must be flushed or drained before a
			// new input media type is set」——所以**同一个 MFT 上换一次输入媒体
			// 类型，第二次可能被拒**。而在同一个 MFT 上连试两种顺序，正好会
			// 撞上这个限制：第一遍若在 `SetOutputType` 处失败，输入类型**已经
			// 设进去了**，第二遍再设一次就没有 flush，于是可能把一种其实能认
			// 的配置报成「不认」。
			//
			// `open()` 那条路本来就是每次循环都 `createMFT()`，这里跟它一致。
			var last error
			for _, inputFirst := range []bool{true, false} {
				mft, err := c.createMFT()
				if err != nil {
					last = err
					continue
				}
				err = c.tryOpen(mft, rate, chans, asc, v, inputFirst)
				release(mft)
				if err == nil {
					last = nil
					break
				}
				last = err
			}
			p.Err = last
			probes = append(probes, p)
		}
	})
	return append(out, probes...)
}

// adoptOutputType 从一份输出媒体类型里读出采样率、声道数和格式，记到 Codec 上。
//
// ⚠️ **输出格式必须核**：不核的话，「MFT 挑了浮点输出」会被当成「16 位整数
// PCM」去读——浮点的比特被当成 int16，得到的是一片看着像 PCM 的垃圾，而症状
// 是「RMS 不对」而不是报错。
func (c *Codec) adoptOutputType(typ uintptr) error {
	if v, ok, _ := getUINT32(typ, &guidMTAudioSamplesPerSecond); ok {
		c.rate = int(v)
	}
	if v, ok, _ := getUINT32(typ, &guidMTAudioNumChannels); ok {
		c.chans = int(v)
	}
	if c.rate == 0 || c.chans == 0 {
		return fmt.Errorf("mfadec: 输出媒体类型里没有采样率或声道数")
	}
	sub, ok, _ := getGUID(typ, &guidMTSubtype)
	if !ok {
		return fmt.Errorf("mfadec: 输出媒体类型里没有子类型")
	}
	switch {
	case sub.equal(guidAudioFormatPCM):
		bits, ok, _ := getUINT32(typ, &guidMTAudioBitsPerSample)
		if !ok || bits == 0 {
			bits = 16
		}
		if bits != 16 {
			return fmt.Errorf("mfadec: 输出是 %d 位定点 PCM，本 spike 要的是 16 位", bits)
		}
		c.outputFloat = false
	case sub.equal(guidAudioFormatFloat):
		bits, ok, _ := getUINT32(typ, &guidMTAudioBitsPerSample)
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
	return nil
}

// outputAvailableType 问第 index 个可用的输出媒体类型（`GetOutputAvailableType`）。
//
// 枚举到头时返回负的 `hresult`（`MF_E_NO_MORE_TYPES`）。
//
//go:uintptrescapes
func outputAvailableType(mft uintptr, id, index uint32) (uintptr, hresult) {
	var out uintptr
	hr := comHR(comCall(mft, vtblTransformGetOutputAvailableType,
		uintptr(id), uintptr(index), uintptr(unsafe.Pointer(&out))))
	return out, hr
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

	hr := comHR(comCall(c.mft, vtblTransformProcessInput, 0, sample, 0))
	if hr < 0 && uint32(hr) == hrMFNotAccepting {
		// 「现在不收输入」——微软的 `ProcessInput` 文档说这时候 MFT
		// 「is ready to produce output」，客户端该先 `ProcessOutput` 把已有的
		// 输出取走，再重试 `ProcessInput`。
		//
		// ⚠️⚠️ **取出来的 PCM 必须留着。** 原来这里写的是
		// `if _, err := c.pullOutput(); err != nil`——把那一整段**丢掉了**，
		// 然后只返回重试之后取到的那一份。而那一趟取出来的正是**这一帧之前
		// 攒下的输出**，丢掉就是解码结果里少一段，而症状是「听着像跳了一下」，
		// 不是报错。
		pending, err := c.pullOutput()
		if err != nil {
			return nil, err
		}
		hr = comHR(comCall(c.mft, vtblTransformProcessInput, 0, sample, 0))
		if hr < 0 {
			return nil, hresultError("ProcessInput", hr)
		}
		rest, err := c.pullOutput()
		if err != nil {
			return nil, err
		}
		return append(pending, rest...), nil
	}
	if hr < 0 {
		return nil, hresultError("ProcessInput", hr)
	}
	return c.pullOutput()
}

// errStreamChanged 是「输出流换了格式，得重新设一次输出类型再接着取」。
//
// 它**不是错误**：按文档，`ProcessOutput` 返回 `MF_E_TRANSFORM_STREAM_CHANGE`
// （或 `dwStatus` 里带 `MFT_OUTPUT_DATA_BUFFER_FORMAT_CHANGE`）时，这一趟
// **不产生输出**，客户端要重新问一次可用的输出类型、设上去，然后接着取。
var errStreamChanged = errors.New("mfadec: 输出流换了格式")

// pullOutput 把 MFT 手里的输出全取出来。
//
// ⚠️ **有迭代上限**：MFT 一直说「还有更多」（`MFT_OUTPUT_DATA_BUFFER_INCOMPLETE`）
// 而不往前挪的时候，无限循环就是「CI 跑到超时」而不是「快速失败」。
//
// ⚠️ **换了格式要重设输出类型**：HE-AAC 会走到这里——SBR 把采样率翻倍、PS 把
// 单声道变立体声，前几帧之后输出格式才定下来。原来这里把
// `MF_E_TRANSFORM_STREAM_CHANGE` 当成致命错误，于是 **HE-AAC 在第一帧就失败**；
// 而 `FORMAT_CHANGE` 那一支只是把当前类型读出来再放掉，**从来没有设过新的**
// ——按文档，不重设的话后面的 `ProcessInput` / `ProcessOutput` 会一直失败。
func (c *Codec) pullOutput() ([]float32, error) {
	var pcm []float32
	for range maxOutputPulls {
		chunk, done, err := c.processOutput()
		if errors.Is(err, errStreamChanged) {
			if err := c.renegotiateOutput(); err != nil {
				return nil, err
			}
			continue
		}
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

// renegotiateOutput 在输出流换了格式之后重新挑一个输出类型。
//
// 做法照文档：`GetOutputAvailableType` 枚举一遍，挑一个**我们读得动**的
// （16 位定点 PCM 或 32 位浮点），`SetOutputType` 设上去，再读回当前的
// 输出类型，把采样率 / 声道数 / 格式更新到 `Codec` 上——**HE-AAC 的采样率
// 会在这里变成翻倍之后的那个**。
//
// ⚠️ 候选类型要先**试读**（`adoptOutputType` 会核子类型和位宽），确认读得动
// 再提交；读不动的回滚，试下一个。
func (c *Codec) renegotiateOutput() error {
	var chosen uintptr
	oldRate, oldChans, oldFloat := c.rate, c.chans, c.outputFloat
	for i := uint32(0); i < maxOutputTypes; i++ {
		typ, hr := outputAvailableType(c.mft, 0, i)
		if hr < 0 {
			break // 枚举到头了
		}
		if err := c.adoptOutputType(typ); err != nil {
			c.rate, c.chans, c.outputFloat = oldRate, oldChans, oldFloat
			release(typ)
			continue
		}
		chosen = typ
		break
	}
	if chosen == 0 {
		return fmt.Errorf("mfadec: 输出流换了格式，但 %d 个可用输出类型里没有一个读得动", maxOutputTypes)
	}
	defer release(chosen)

	if hr := setOutputType(c.mft, chosen); hr < 0 {
		// 退一步：让 MFT 自己挑（pType 传 NULL），和 `tryOpen` 一样。
		if hr2 := setOutputType(c.mft, 0); hr2 < 0 {
			return hresultError("SetOutputType（换格式之后）", hr)
		}
	}

	// 设完再读回**当前**的输出类型：MFT 可能没照我们挑的那个来。
	cur, err := outputCurrentType(c.mft)
	if err != nil {
		return err
	}
	defer release(cur)
	if err := c.adoptOutputType(cur); err != nil {
		return err
	}

	// 输出样本的大小也可能变了（采样率翻倍的话一帧的字节数就变了），
	// 「由调用方分配样本」那条路要用新值。
	var info mftOutputStreamInfo
	if hr := comHR(comCall(c.mft, vtblTransformGetOutputStreamInfo, 0,
		uintptr(unsafe.Pointer(&info)))); hr < 0 {
		return hresultError("GetOutputStreamInfo（换格式之后）", hr)
	}
	c.callerAllocatesOutput = info.Flags&(mftOutputStreamProvidesSamples|mftOutputStreamCanProvideSamples) == 0
	if info.Size > 0 {
		c.outputSize = int(info.Size)
	}
	return nil
}

// maxOutputTypes 是枚举可用输出类型时最多试几个。
//
// 解码器一般给两三个（16 位 PCM、浮点、也许还有折叠成立体声的）。给 16 是
// 防「永远枚举不完」——那说明别的地方坏了，该快速失败。
const maxOutputTypes = 16

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
		// ⚠️ **这一条原来被当成致命错误**，于是 HE-AAC（SBR 把采样率翻倍、
		// PS 把单声道变立体声）在第一帧就失败。文档说这时候它**不产生输出**，
		// 要重新设一次输出类型再接着取。
		if uint32(hr) == hrTransformStreamChange {
			return nil, false, errStreamChanged
		}
		return nil, true, hresultError("ProcessOutput", hr)
	}
	// dwStatus 里那三个值（0x100 / 0x200 / 0x300）**不是互斥的位**，
	// 是三个互斥的取值，所以这里按位取一段再比，而不是逐个 &。
	switch out.Status & 0x00000300 {
	case mftOutputDataFormatChange:
		// 输出格式变了：按文档要**重新设一次输出类型**再接着取。
		// 原来这里只把当前类型读出来再放掉，**从来没设过新的**——而按文档，
		// 不重设的话后面的 `ProcessInput` / `ProcessOutput` 会一直失败。
		//
		// ⚠️ 这一趟 MFT 仍可能塞给我们一个样本（`ours == 0` 时是它分配的），
		// 不在这里放掉就漏一个 COM 引用。
		if ours == 0 && out.Sample != 0 {
			release(out.Sample)
		}
		return nil, false, errStreamChanged
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
	hr = comHR(comCall(sample, vtblSampleAddBuffer, buf))
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
	hr := comHR(comCall(sample, vtblSampleAddBuffer, buf))
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
