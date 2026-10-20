package mfadec

import (
	"errors"
	"fmt"
)

// 裸 ADTS（没有 MP4 容器的那种 .aac）的头解析。
//
// 为什么要有这一层：**profile 不在 MF 给我们的任何东西里**。Media Foundation 的
// 源阅读器只说「这是 AAC」（subtype 是 `MFAudioFormat_AAC`），它不会说 AAC-LC
// 还是 HE-AAC。而 ISSUE #35 的验收里有一条是「AAC-LC 一定要；HE-AAC 拿不到也要
// 说清楚」——「说清楚」的前提是**先说得清输入是什么**，不然失败时连是解码器不行
// 还是本来就解不了都分不开。
//
// ADTS 头 7 字节（不带 CRC）或 9 字节（带 CRC），同步字 0xFFF 开头。
// 逐字节的排布（ISO/IEC 13818-7 的 Table 1.1）：
//
//	byte 0: 同步字[11:4]（8 bit）
//	byte 1: 同步字[3:0]（4 bit）| MPEG 版本（1）| 层（2）| 无保护（1）
//	byte 2: profile（2）| 采样率下标（4）| 私有位（1）| 声道配置[2]（1）
//	byte 3: 声道配置[1:0]（2）| 原创（1）| 首页（1）
//	        | copyright 标识位（1）| copyright 起始位（1）| 帧长[12:11]（2）
//	byte 4: 帧长[10:3]（8 bit）
//	byte 5: 帧长[2:0]（3 bit）| 缓冲区 fullness[10:6]（5 bit）
//	byte 6: 缓冲区 fullness[5:0]（6 bit）| 帧数（2 bit，恒为 0）
//
// ⚠️ **帧长那 13 bit 横跨三个字节**，而且**不连续**：byte 3 的最低两位、
// byte 4 的全部、byte 5 的最高三位。它**含头**——规范里 frame_length 就是
// 「这一帧总共有多少字节」，7 字节的头算在里面。
//
// ⚠️⚠️ **这里写错过一次（从 BMG-68 接手时带过来的），而且它自己的测试是绿的。**
// 原来取的是 `binary.BigEndian.Uint16(b[5:7]) & 0x1FFF`——那 16 位里混着
// 缓冲区 fullness 的位，取出来是个**完全合法、却谁都不是**的数：
// 素材的第一帧取出来是 8188（+7 = 8195），而真值是 326。
//
// 症状不是报错：**拆帧会整体错位**，从第二帧起就找不到同步字，
// 而「只拆出来一帧」和「解码器解不了」长得一模一样。
// 现在用的 `((b[3]&0x03)<<11) | (b[4]<<3) | (b[5]>>5)` 是对的，
// `adts_test.go` 里那条「走完整个文件、正好停在末尾」钉着它。

// adtsSampleRates 是 4 bit 采样率下标对应的实际采样率（单位 Hz）。
//
// ⚠️ 下标 13、14、15 在规范里是「保留」，这里给 0 而不是编一个数出来——
// 拿到保留下标时 MPEG 帧的采样率只能从别处推，本层推不出来，编一个会变成
// 「解出来了、采样率是 0 Hz」。
var adtsSampleRates = [16]int{
	96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050,
	16000, 12000, 11025, 8000, 7350, 0, 0, 0,
}

// adtsChannelCounts 是 3 bit 声道配置对应的声道数。
//
// 下标 7 是保留；下标 0 是「由程序配置元素（PCE）定义」，那一路本层读不到，
// 所以两者都给 0，让调用方明确看到「这个头没说清是几声道」。
var adtsChannelCounts = [8]int{0, 1, 2, 3, 4, 5, 6, 0}

// ADTSProfile 是 ADTS 头里那个 2 bit 的 profile 字段。
//
// ⚠️ **它说的是「头里写了什么」，不是「码流是什么」。** 实测：本仓库里
// ffmpeg 用 `-profile:a aac_low` 造出来的那份素材，ffprobe 说 profile=LC，
// 头里写的却是 Main（`fixture_test.go` 的 `TestTheADTSProfileFieldIsNotTheTruth`
// 钉着这条）。拿这个字段去决定「这个 AAC 是 LC 还是别的」会得出错的答案。
//
// 它仍然有用：采样率、声道数、帧长那三个字段是可信的，而判断「这份字节
// 是不是裸 ADTS」只需要同步字。所以这一层的定位是**读容器头**，
// 不是给码流做分类。
type ADTSProfile int

const (
	// ProfileMain 是 AAC Main。
	ProfileMain ADTSProfile = 1
	// ProfileLC 是 AAC-LC。B 站绝大多数用户下的是它。
	ProfileLC ADTSProfile = 2
	// ProfileSSR 是 AAC SSR。早就没人用了。
	ProfileSSR ADTSProfile = 3
	// ProfileLTP 是 AAC LTP。基本没人用。
	ProfileLTP ADTSProfile = 4
)

// String 把 profile 说成人能看的那一串。
func (p ADTSProfile) String() string {
	switch p {
	case ProfileMain:
		return "AAC Main"
	case ProfileLC:
		return "AAC-LC"
	case ProfileSSR:
		return "AAC SSR"
	case ProfileLTP:
		return "AAC LTP"
	default:
		return fmt.Sprintf("profile %d", int(p))
	}
}

// ErrNotADTS 是这批字节开头就不是 ADTS 同步字。
var ErrNotADTS = errors.New("mfadec: these bytes do not start with an ADTS sync word")

// ADTSInfo 是裸 ADTS 头里的那几个字段。
type ADTSInfo struct {
	Profile    ADTSProfile
	SampleRate int
	Channels   int
	// FrameLen 是这一帧的**总**长度（头 + 数据）。
	//
	// ⚠️ 写代码时最容易漏的就是把它当成「数据有多长」——按数据长度去切，
	// 会越切越偏，最后在一个完全不存在的字节上认同步字。
	//
	// ⚠️ 规范里 frame_length 那 13 bit **已经含头**了，所以这里**不再加 7**。
	// 加了会整体错位 7 字节（见文件头那段）。
	FrameLen int
	// HasCRC 说的是有没有带 CRC 校验。
	HasCRC bool
}

// ParseADTS 读 b 开头的那个 ADTS 帧头。
//
// ⚠️ b 必须**从一帧的开头**开始。文件里第二个往后的帧前面还有位，所以这层
// 只负责开头那一帧；往后要一帧一帧走，得先有第一帧的 FrameLen。
func ParseADTS(b []byte) (ADTSInfo, error) {
	if len(b) < 7 {
		return ADTSInfo{}, fmt.Errorf("mfadec: %d bytes is too short for an ADTS header", len(b))
	}
	if b[0] != 0xFF || b[1]&0xF0 != 0xF0 {
		return ADTSInfo{}, ErrNotADTS
	}

	hasCRC := b[1]&0x01 == 0
	info := ADTSInfo{
		Profile:    ADTSProfile((b[2] >> 6) & 0x03),
		SampleRate: adtsSampleRates[(b[2]>>2)&0x0F],
		Channels:   adtsChannelCounts[((b[2]&0x01)<<2)|((b[3]>>6)&0x03)],
		// 帧长那 13 bit 横跨 byte 3/4/5，而且**不连续**。
		//
		// ⚠️ 每一段都要**先转成 uint32** 再移位：`b[4] << 3` 在 byte 上算
		// 会溢出（0x28 << 3 = 0x140，截成 byte 就是 0x40）——于是帧长变成
		// 70 而不是 326，症状是「拆出来全是碎的」。
		FrameLen: int(uint32(b[3]&0x03)<<11 | uint32(b[4])<<3 | uint32(b[5])>>5),
		HasCRC:   hasCRC,
	}
	if info.SampleRate <= 0 {
		return ADTSInfo{}, fmt.Errorf("mfadec: the ADTS header points at sample-rate index %d, which is reserved",
			(b[2]>>2)&0x0F)
	}
	if info.Channels <= 0 {
		return ADTSInfo{}, errors.New("mfadec: the ADTS header does not say how many channels there are")
	}
	if info.FrameLen < 7 || (hasCRC && info.FrameLen < 9) {
		return ADTSInfo{}, fmt.Errorf("mfadec: the ADTS header claims a %d byte frame, which cannot hold its own header",
			info.FrameLen)
	}
	return info, nil
}
