package mfadec

// 两种容器的拆帧：MP4（.m4a）和裸 ADTS（.aac）。
//
// 拆帧**由我们做**，不交给 Media Foundation——这正是从 `IMFSourceReader`
// 换到 `IMFTransform` 换来的好处之一：源阅读器只吃 URL、demux 由 MF 做、
// 还要自己写 COM 回调对象；这一条我们自己喂帧，一条自定义 COM 对象都不用。
//
// .m4a 用 `go-m4a`（MIT，已在 go.mod 里）：它认 MP4 的帧、采样率和
// AudioSpecificConfig。B 站下发的音频有的叫 .m4a 有的叫 .m4s，内容却一样。

import (
	"fmt"
	"io"

	m4a "github.com/tphakala/go-m4a"
)

// openM4A 用 go-m4a 拆一个 MP4 家族的文件。
//
// 返回的 source 里的帧是**裸** AAC（没有 ADTS 头）——MP4 里存的就是那样。
func openM4A(r io.ReadSeeker) (*frameSource, error) {
	rd, err := m4a.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("mfadec: 认不出这个 MP4：%w", err)
	}
	info := rd.Info()
	if info.Codec != m4a.CodecAACLC {
		return nil, fmt.Errorf("mfadec: 这个 MP4 里不是 AAC（是 %s）", info.Codec)
	}
	if len(info.ASC) == 0 {
		return nil, fmt.Errorf("mfadec: 这个 MP4 里没有 AudioSpecificConfig")
	}
	return &frameSource{
		payloadType: aacPayloadRaw,
		rate:        info.SampleRate,
		chans:       info.Channels,
		asc:         info.ASC,
		next:        rd.ReadFrame,
	}, nil
}

// openADTS 拆一个裸 ADTS 的流。
//
// ADTS 的每一帧前面有一个 7 字节（带 CRC 是 9 字节）的头，头里写着这一帧
// 总共有多长——所以拆帧只要顺着头走，不用找同步字。
func openADTS(r io.Reader) (*frameSource, error) {
	first, info, err := readADTSFrame(r)
	if err != nil {
		return nil, err
	}
	asc, err := ascFromADTS(info)
	if err != nil {
		return nil, err
	}

	src := &frameSource{
		payloadType: aacPayloadADTS,
		rate:        info.SampleRate,
		chans:       info.Channels,
		asc:         asc,
	}
	pending := first
	src.next = func() ([]byte, error) {
		if pending != nil {
			out := pending
			pending = nil
			return out, nil
		}
		f, _, err := readADTSFrame(r)
		if err != nil {
			return nil, err
		}
		return f, nil
	}
	return src, nil
}

// readADTSFrame 读一帧 ADTS（头 + 数据），返回整帧和头里的那几个字段。
//
// ⚠️ **头是 7 字节还是 9 字节，取决于有没有 CRC**——而 CRC 标志就在那 7 个
// 字节的最后一个里。所以只能先读 7 个字节、看标志，再决定要不要再读 2 个。
//
// 一开始这里是「一次读 9 个字节」，于是无 CRC 的流会**多读走 2 个字节的
// 载荷**：后面每一帧都错位 2 字节，症状是「拆出来只有一帧」或者
// 「第二帧的头认不出来」——和「解码器不行」长得一模一样。
func readADTSFrame(r io.Reader) ([]byte, ADTSInfo, error) {
	head := make([]byte, 7)
	if _, err := io.ReadFull(r, head); err != nil {
		return nil, ADTSInfo{}, fmt.Errorf("mfadec: 读 ADTS 头：%w", err)
	}
	info, err := ParseADTS(head)
	if err != nil {
		return nil, ADTSInfo{}, fmt.Errorf("mfadec: 认 ADTS 头：%w", err)
	}
	frame := make([]byte, 0, info.FrameLen)
	frame = append(frame, head...)
	if info.HasCRC {
		crc := make([]byte, 2)
		if _, err := io.ReadFull(r, crc); err != nil {
			return nil, ADTSInfo{}, fmt.Errorf("mfadec: 读 ADTS 的 CRC：%w", err)
		}
		frame = append(frame, crc...)
	}
	for len(frame) < info.FrameLen {
		b := make([]byte, info.FrameLen-len(frame))
		n, err := io.ReadFull(r, b)
		frame = append(frame, b[:n]...)
		if err != nil {
			return nil, ADTSInfo{}, fmt.Errorf("mfadec: 这一帧只读到 %d 字节（要 %d）：%w",
				len(frame), info.FrameLen, err)
		}
	}
	return frame, info, nil
}
