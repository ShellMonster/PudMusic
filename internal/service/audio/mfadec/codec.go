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

// heaacUserData 造 `MF_MT_USER_DATA` 的内容。
//
// SDK 的 `um/mmreg.h` 里 `HEAACWAVEINFO` 是 `WAVEFORMATEX`（18 字节）加上
// 12 字节：wPayloadType、wAudioProfileLevelIndication、wStructType、
// wReserved1、dwReserved2。而 `MF_MT_USER_DATA` 要的是**去掉
// WAVEFORMATEX 之后的那 12 字节**，紧跟着 AudioSpecificConfig()。
//
// ⚠️ 那 12 个字节的排布是 SDK 头文件里写死的，抄错了解码器不认这个媒体类型，
// 而症状是「说不清的 E_INVALIDARG」。
func heaacUserData(payloadType uint32, asc []byte) []byte {
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

// aacDecoder 是解码器要提供的那一点。
//
// 这一层（容器、码流）是平台无关的，而解码器本身只在 Windows 上有——
// 所以这里用接口而不是具体类型，`codec.go` 才能在没有 Windows 的机器上编译。
type aacDecoder interface {
	// Feed 喂一帧 AAC，返回这一次解出来的 PCM。
	Feed(frame []byte) ([]byte, error)
}

// ReadAll 解到源读完，返回交织的 16 位 PCM。
func (s *frameSource) ReadAll(c aacDecoder) ([]byte, error) {
	var pcm []byte
	for {
		frame, err := s.next()
		if err == io.EOF {
			return pcm, nil
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
}
