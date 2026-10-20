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
	"fmt"
	"unsafe"
)

// Codec 是一个 AAC 解码器。
//
// 它**不是**并发安全的：一个解码器一条线程，所有方法都在那条 COM 线程上跑。
type Codec struct {
	th    *comThread
	mft   uintptr
	rate  int
	chans int
	// callerAllocatesOutput 记的是输出样本由谁分配。
	//
	// `MFT_OUTPUT_STREAM_INFO` 的 dwFlags 说了算：带
	// `MFT_OUTPUT_STREAM_PROVIDES_SAMPLES` 或者
	// `MFT_OUTPUT_STREAM_CAN_PROVIDE_SAMPLES` 的话，**pSample 传 NULL**
	// 让 MFT 自己分配；两个都没有的话，**必须**由调用方分配。
	callerAllocatesOutput bool
	closed                bool
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

// open 是真正建 MFT 的那部分，跑在 COM 线程上。
func (c *Codec) open(rate, chans int, asc []byte, payloadType uint32) error {
	clsid := guidCLSIDCMSAACDecMFT
	iid := guidIIDIMFTransform
	if hr := coCreateInstance(&clsid, &iid, &c.mft); hr < 0 {
		return hresultError("CoCreateInstance(CLSID_CMSAACDecMFT)", hr)
	}

	input, err := c.buildInputType(rate, chans, asc, payloadType)
	if err != nil {
		return err
	}
	defer release(input)

	if hr := c.setInputType(input); hr < 0 {
		return hresultError("SetInputType", hr)
	}

	out, err := c.buildOutputType(rate, chans)
	if err != nil {
		return err
	}
	defer release(out)

	if hr := c.setOutputType(out); hr < 0 {
		// 退一步：把媒体类型交给 MFT 自己挑（pType 传 NULL）。
		//
		// 文档说 `SetOutputType` 的 pType 可以是 NULL——那表示「你挑一个」。
		// 自带的 AAC 解码器认 PCM，但具体是 16 位还是浮点、采样率多少，
		// 由它自己定；定完再读回来就是了（下面就是读回来的）。
		if hr2 := c.setOutputType(0); hr2 < 0 {
			return hresultError("SetOutputType", hr)
		}
	}

	// 输出到底是多少采样率、几个声道，读回来——不要拿自己设的值当答案。
	// HE-AAC 的 SBR 会把采样率翻倍，设 44100 出来的可能是 88200。
	cur, err := c.outputCurrentType()
	if err != nil {
		return err
	}
	defer release(cur)
	if v, ok, _ := getUINT32(cur, &guidMTAudioSamplesPerSecond); ok {
		c.rate = int(v)
	}
	if v, ok, _ := getUINT32(cur, &guidMTAudioNumChannels); ok {
		c.chans = int(v)
	}
	if c.rate == 0 || c.chans == 0 {
		return fmt.Errorf("mfadec: 输出媒体类型里没有采样率或声道数")
	}

	// 输出样本由谁分配，看 `MFT_OUTPUT_STREAM_INFO` 的 dwFlags。
	var info mftOutputStreamInfo
	hr := comHR(comCall(c.mft, vtblTransformGetOutputStreamInfo, 0,
		uintptr(unsafe.Pointer(&info))))
	if hr < 0 {
		return hresultError("GetOutputStreamInfo", hr)
	}
	c.callerAllocatesOutput = info.Flags&(mftOutputStreamProvidesSamples|mftOutputStreamCanProvideSamples) == 0

	return nil
}

// buildInputType 造输入媒体类型。
func (c *Codec) buildInputType(rate, chans int, asc []byte, payloadType uint32) (uintptr, error) {
	return c.buildInputTypeWith(rate, chans, asc, payloadType, len(asc) > 0)
}

// buildInputTypeWith 造输入媒体类型，withUserData 决定要不要带
// `MF_MT_USER_DATA`。
//
// `MF_MT_USER_DATA` 放的是 `HEAACWAVEINFO` 去掉 `WAVEFORMATEX` 之后的那
// 12 字节，紧跟着 AudioSpecificConfig。
//
// ⚠️ **这一段不能省**：解码器要靠 ASC 才知道采样率和声道，而媒体类型上
// 那两个属性说的是「核心」码流的，SBR 展开之后会不一样。
// 但**到底需不需要**是这台机器说了算——所以留一个开关，让 spike 把
// 「带」和「不带」两种都试一遍，把 MFT 的真实回答记下来。
func (c *Codec) buildInputTypeWith(rate, chans int, asc []byte, payloadType uint32, withUserData bool) (uintptr, error) {
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
	if hr := setUINT32(mt, &guidMTAACPayloadType, payloadType); hr < 0 {
		release(mt)
		return 0, hresultError("SetUINT32(MF_MT_AAC_PAYLOAD_TYPE)", hr)
	}
	if withUserData && len(asc) > 0 {
		if hr := setBlob(mt, &guidMTUserData, heaacUserData(payloadType, asc)); hr < 0 {
			release(mt)
			return 0, hresultError("SetBlob(MF_MT_USER_DATA)", hr)
		}
	}
	return mt, nil
}

// TryInputType 试一种输入配置，返回成不成功。
//
// 这是**诊断**用的：spike 要知道 MFT 到底认哪几种配置（裸 AAC / ADTS、
// 带不带 `MF_MT_USER_DATA`），因为「它不认哪一种」和「它不认这一种」
// 是两件完全不同的事，报告里必须分开写。
func (c *Codec) TryInputType(rate, chans int, asc []byte, payloadType uint32, withUserData bool) error {
	var err error
	c.th.do(func() {
		mt, e := c.buildInputTypeWith(rate, chans, asc, payloadType, withUserData)
		if e != nil {
			err = e
			return
		}
		defer release(mt)
		if hr := c.setInputType(mt); hr < 0 {
			err = hresultError("SetInputType", hr)
		}
	})
	return err
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
func (c *Codec) setInputType(mt uintptr) hresult {
	return comHR(comCall(c.mft, vtblTransformSetInputType, 0,
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
func (c *Codec) setOutputType(mt uintptr) hresult {
	var arg uintptr
	if mt != 0 {
		arg = uintptr(unsafe.Pointer(&mt))
	}
	return comHR(comCall(c.mft, vtblTransformSetOutputType, 0, arg, 0))
}

// outputCurrentType 问 MFT 当前用的输出媒体类型。
//
//go:uintptrescapes
func (c *Codec) outputCurrentType() (uintptr, error) {
	var out uintptr
	hr := comHR(comCall(c.mft, vtblTransformGetOutputCurrentType, 0,
		uintptr(unsafe.Pointer(&out))))
	if hr < 0 {
		return 0, hresultError("GetOutputCurrentType", hr)
	}
	return out, nil
}

// SampleRate 是输出采样率。
func (c *Codec) SampleRate() int { return c.rate }

// Channels 是输出声道数。
func (c *Codec) Channels() int { return c.chans }

// CallerAllocatesOutput 记的是输出样本由谁分配（诊断用）。
func (c *Codec) CallerAllocatesOutput() bool { return c.callerAllocatesOutput }

// ThreadSwitches 是「那条 COM 线程在干活的时候被换掉过几次」。
//
// 这是把「偶发的 apartment 错误」变成**一个数**的地方：0 或者不是 0。
// 见 `mf_windows.go` 里 `comThread` 那一节的说明。
func (c *Codec) ThreadSwitches() int64 { return c.th.switches.Load() }

// Feed 喂一帧 AAC，返回这一次解出来的 PCM（16 位定点、交织）。
//
// 返回空切片是**正常**的：MFT 会攒着，某一帧喂进去不一定马上有输出。
func (c *Codec) Feed(frame []byte) ([]byte, error) {
	if c.closed {
		return nil, fmt.Errorf("mfadec: 解码器已经关了")
	}
	var out []byte
	var err error
	c.th.do(func() { out, err = c.feed(frame) })
	return out, err
}

// feed 是真正解码的那部分，跑在 COM 线程上。
func (c *Codec) feed(frame []byte) ([]byte, error) {
	sample, buf, err := c.makeSample(frame)
	if err != nil {
		return nil, err
	}
	defer release(sample)
	defer release(buf)

	hr := comHR(comCall(c.mft, vtblTransformProcessInput, 0,
		uintptr(unsafe.Pointer(&sample)), 0))
	if hr < 0 {
		return nil, hresultError("ProcessInput", hr)
	}

	var pcm []byte
	for {
		chunk, done, err := c.processOutput()
		if err != nil {
			return nil, err
		}
		pcm = append(pcm, chunk...)
		if done {
			break
		}
	}
	return pcm, nil
}

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

// processOutput 拉一次输出。
//
// 返回的 done 是 true 表示「这一趟没有更多输出了」。
//
//go:uintptrescapes
func (c *Codec) processOutput() ([]byte, bool, error) {
	var out mftOutputDataBuffer
	// pSample 传 0 = 让 MFT 自己分配样本。
	//
	// 只有在 `MFT_OUTPUT_STREAM_INFO` 的 dwFlags 里既没有
	// PROVIDES_SAMPLES 也没有 CAN_PROVIDE_SAMPLES 时，调用方才**必须**
	// 自己分配——那一条路下面单独处理。
	var ours uintptr
	var oursBuf uintptr
	if c.callerAllocatesOutput {
		s, buf, err := c.makeEmptySample(outputBufferBytes)
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
		// 输出格式变了：按文档这时要重新问一次输出媒体类型。
		// 这一票的素材不会走到这里——真走到了就是一条**没有处理**的情况，
		// 不能当成「这一趟没输出」糊过去。
		return nil, true, fmt.Errorf("mfadec: 输出格式变了（MFT_OUTPUT_DATA_BUFFER_FORMAT_CHANGE）")
	case mftOutputDataStreamEnd:
		return nil, true, nil
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

	pcm, err := readSamplePCM(out.Sample)
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

// ── 帧的读取 ────────────────────────────────────────────────────────────────

// readSamplePCM 把一个输出样本里的 PCM 读出来。
//
//go:uintptrescapes
func readSamplePCM(sample uintptr) ([]byte, error) {
	var buf uintptr
	hr := comHR(comCall(sample, vtblSampleGetBufferByIndex, 0,
		uintptr(unsafe.Pointer(&buf))))
	if hr < 0 {
		return nil, hresultError("IMFSample::GetBufferByIndex", hr)
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

	return append([]byte(nil), unsafe.Slice(p, curLen)...), nil
}

// Close 把解码器和它那条 COM 线程一起收掉。
func (c *Codec) Close() error {
	if c.closed {
		return nil
	}
	c.closed = true
	c.th.do(func() { release(c.mft) })
	c.th.close()
	return nil
}
