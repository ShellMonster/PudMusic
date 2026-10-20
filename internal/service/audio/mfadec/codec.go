package mfadec

// 解码器和容器之间那一层：平台无关的部分。
//
// 拆帧（`source.go`）、码流长什么样（`adts.go`）、常量（`guid.go`）都在
// 这一层；真正碰 COM 的部分在 `transform_windows.go`（只在 Windows 上编译）。

import (
	"encoding/binary"
	"fmt"
	"io"
)

// payloadType 的两个值（SDK: `MF_MT_AAC_PAYLOAD_TYPE`）。
const (
	// aacPayloadRaw 是 0：码流里只有 `raw_data_block()`，一个样本一帧。
	// B 站的 .m4a 就是它。
	aacPayloadRaw = 0
	// aacPayloadADTS 是 1：码流里是 `adts_sequence()`，每帧前面有 ADTS 头。
	// .aac 文件是它。
	aacPayloadADTS = 1
)

// heaacTail 是 `HEAACWAVEINFO` 去掉 `WAVEFORMATEX` 之后的那 12 字节，
// 紧跟着 AudioSpecificConfig()。
//
// SDK 的 `um/mmreg.h` 里 `HEAACWAVEINFO` 是 `WAVEFORMATEX`（18 字节）加上
// 12 字节：wPayloadType、wAudioProfileLevelIndication、wStructType、
// wReserved1、dwReserved2。
//
// ⚠️ 那 12 个字节的排布是 SDK 头文件里写死的，抄错了解码器不认这个媒体类型，
// 而症状是「说不清的 E_INVALIDARG」。
func heaacTail(payloadType uint32, asc []byte) []byte {
	out := make([]byte, 0, 12+len(asc))
	// wPayloadType
	out = binary.LittleEndian.AppendUint16(out, uint16(payloadType))
	// wAudioProfileLevelIndication：0xFE = 「没指定」。
	out = binary.LittleEndian.AppendUint16(out, 0x00FE)
	// wStructType：0 = 后面跟的是 AudioSpecificConfig()。
	out = binary.LittleEndian.AppendUint16(out, 0)
	// wReserved1
	out = binary.LittleEndian.AppendUint16(out, 0)
	// dwReserved2
	out = binary.LittleEndian.AppendUint32(out, 0)
	return append(out, asc...)
}

// heaacFormatBlock 是**整个** `HEAACWAVEFORMAT`：`WAVEFORMATEX`（18 字节）
// + 那 12 字节 + AudioSpecificConfig()。
//
// `um/mmreg.h` 描述的「格式块」是这一种，而解码器是靠 `wfx.cbSize` 找到
// ASC 的位置的（`dwASCLen = cbSize - sizeof(HEAACWAVEINFO) +
// sizeof(WAVEFORMATEX)`）——`cbSize` 就在 `WAVEFORMATEX` 里面。
func heaacFormatBlock(rate, chans int, payloadType uint32, asc []byte) []byte {
	out := make([]byte, 0, 18+12+len(asc))
	// WAVEFORMATEX：
	out = binary.LittleEndian.AppendUint16(out, waveFormatMPEGHEAAC) // wFormatTag
	out = binary.LittleEndian.AppendUint16(out, uint16(chans))       // nChannels
	out = binary.LittleEndian.AppendUint32(out, uint32(rate))        // nSamplesPerSec
	out = binary.LittleEndian.AppendUint32(out, 0)                   // nAvgBytesPerSec
	out = binary.LittleEndian.AppendUint16(out, 1)                   // nBlockAlign
	out = binary.LittleEndian.AppendUint16(out, 0)                   // wBitsPerSample
	out = binary.LittleEndian.AppendUint16(out, uint16(12+len(asc))) // cbSize
	// 后面是 HEAACWAVEINFO 的那 12 字节 + ASC。
	return append(out, heaacTail(payloadType, asc)...)
}

// waveFormatMPEGHEAAC 是 `WAVE_FORMAT_MPEG_HEAAC`（`um/mmreg.h`）。
const waveFormatMPEGHEAAC = 0x1610

// ascFromADTS 从 ADTS 头里拼出一个 AudioSpecificConfig。
//
// ADTS 头里有采样率下标和声道配置，够拼出 AAC-LC 的 ASC（2 字节）：
// audioObjectType(5) = 2、samplingFrequencyIndex(4)、channelConfiguration(4)、
// 然后三个 0。
//
// ⚠️ 声道配置为 0（意味着有 program_config_element）时拼不出来——那一种 ASC
// 超过 2 字节，这里直接报错，不猜。
func ascFromADTS(info ADTSInfo) ([]byte, error) {
	if info.Channels == 0 {
		return nil, fmt.Errorf("mfadec: ADTS 头的声道配置是 0（PCE），拼不出 AudioSpecificConfig")
	}
	rateIdx := -1
	for i, r := range adtsSampleRates {
		if r == info.SampleRate {
			rateIdx = i
			break
		}
	}
	if rateIdx < 0 {
		return nil, fmt.Errorf("mfadec: 采样率 %d 不在 ADTS 的下标表里", info.SampleRate)
	}
	// 5 + 4 + 4 + 3 = 16 bit。
	v := uint16(2)<<11 | uint16(rateIdx)<<7 | uint16(info.Channels)<<3
	return []byte{byte(v >> 8), byte(v)}, nil
}

// frameSource 是一串 AAC 帧。
//
// 两种来源：.m4a（go-m4a 拆出来的裸帧 + AudioSpecificConfig）和 .aac
// （带 ADTS 头的帧）。
type frameSource struct {
	// payloadType 是 `MF_MT_AAC_PAYLOAD_TYPE`。
	payloadType uint32
	// rate/chans 是核心码流的采样率和声道数。
	rate, chans int
	// asc 是 AudioSpecificConfig（给解码器用）。
	asc []byte
	// next 拿下一帧。
	next func() ([]byte, error)
}

// inputVariant 是一种输入配置。
type inputVariant struct {
	name string
	// payloadType 是 `MF_MT_AAC_PAYLOAD_TYPE`。
	payloadType uint32
	// userData 是 `MF_MT_USER_DATA` 用哪一种形状（见 userDataForm）。
	userData userDataForm
}

// userDataForm 是 `MF_MT_USER_DATA` 的三种形状。
type userDataForm int

const (
	// userDataNone 不带 `MF_MT_USER_DATA`。
	userDataNone userDataForm = iota
	// userDataTail 是 `HEAACWAVEINFO` 去掉 `WAVEFORMATEX` 之后的那 12 字节，
	// 紧跟着 AudioSpecificConfig。
	//
	// 微软那篇「AAC Decoder」的文档写的就是这一种：
	// 「MF_MT_USER_DATA: MFAudioFormat_AAC — Contains the portion of the
	// HEAACWAVEINFO structure that appears **after the WAVEFORMATEX**
	// structure (that is, after the wfx member). This is followed by the
	// AudioSpecificConfig() data.」
	userDataTail
	// userDataFull 是**整个** `HEAACWAVEFORMAT`：`HEAACWAVEINFO`（=
	// `WAVEFORMATEX` 18 字节 + 那 12 字节）紧跟着 AudioSpecificConfig。
	//
	// `um/mmreg.h` 描述的「格式块」是这一种，而解码器是靠 `wfx.cbSize`
	// 找到 ASC 的位置的（`dwASCLen = cbSize - sizeof(HEAACWAVEINFO) +
	// sizeof(WAVEFORMATEX)`）——`cbSize` 就在 `WAVEFORMATEX` 里面。
	userDataFull
)

// userDataBlob 造 `MF_MT_USER_DATA` 的内容（按要试的那一种形状）。
//
// ⚠️ **两种形状都在试**：微软的「AAC Decoder」文档说这一栏是
// `HEAACWAVEINFO` **去掉 WAVEFORMATEX 之后**的那 12 字节 + ASC；而
// `um/mmreg.h` 描述的格式块是**整个** `HEAACWAVEFORMAT`（WAVEFORMATEX 也
// 在里面），因为解码器是靠 `wfx.cbSize` 找 ASC 的。
//
// 两处对不上，而**哪一处对是这台机器上的事实**——所以两种都试。
func userDataBlob(rate, chans int, v inputVariant, asc []byte) []byte {
	if len(asc) == 0 {
		return nil
	}
	switch v.userData {
	case userDataTail:
		return heaacTail(v.payloadType, asc)
	case userDataFull:
		return heaacFormatBlock(rate, chans, v.payloadType, asc)
	default:
		return nil
	}
}

// aacDecoder 是解码器要提供的那一点。
//
// 这一层（容器、码流）是平台无关的，而解码器本身只在 Windows 上有——
// 所以这里用接口而不是具体类型，`codec.go` 才能在没有 Windows 的机器上编译。
type aacDecoder interface {
	// Feed 喂一帧 AAC，返回这一次解出来的 PCM。
	Feed(frame []byte) ([]float32, error)
	// Drain 告诉解码器流结束了。
	Drain() ([]float32, error)
}

// ReadAll 解到源读完，返回交织的 PCM（[-1,1] 的 float32）。
//
// ⚠️ **最后一定要 Drain**：AAC 解码器有 MDCT 的重叠状态，最后一帧是
// 「和前一帧重叠着」的——不 drain 的话它就一直不发，帧数会少一帧。
func (s *frameSource) ReadAll(c aacDecoder) ([]float32, error) {
	var pcm []float32
	for {
		frame, err := s.next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return pcm, err
		}
		if len(frame) == 0 {
			continue
		}
		out, err := c.Feed(frame)
		if err != nil {
			return pcm, err
		}
		pcm = append(pcm, out...)
	}
	tail, err := c.Drain()
	if err != nil {
		return pcm, err
	}
	return append(pcm, tail...), nil
}
