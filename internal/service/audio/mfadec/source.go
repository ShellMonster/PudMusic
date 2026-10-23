package mfadec

// 两种容器的拆帧：MP4（.m4a）和裸 ADTS（.aac）。
//
// 拆帧**由我们做**，不交给 Media Foundation——这正是从 `IMFSourceReader`
// 换到 `IMFTransform` 换来的好处之一：源阅读器只吃 URL、demux 由 MF 做、
// 还要自己写 COM 回调对象；这一条我们自己喂帧，一条自定义 COM 对象都不用。
//
// .m4a 用 `go-m4a`（MIT，已在 go.mod 里）：它认 MP4 的帧、采样率和
// AudioSpecificConfig。B 站下发的音频有的叫 .m4a 有的叫 .m4s，内容却一样。
//
// # go-m4a v0.8.0 带来的两处变化（2026-10 升的，issue #256）
//
//  1. **帧走一个 64 KiB 的读前窗口**：不是「一帧定位一次、读一次」。于是一条
//     1414 帧的流，对源做的 Seek+Read 从**上千次**降到**几十次**——源是在线
//     流时那是缓存文件上的系统调用。判据在 `read_window_test.go`（那个文件里
//     的三条用例在 go-m4a v0.7.0 上**全是红**的）。
//
//  2. **读帧失败不再报 `ErrCorrupt`**：底层读不动了，报的是**包着底层原因**
//     的错。这一条要紧，因为上层（`internal/service/bili/friendly.go`）靠
//     `errors.Is` 认出 `ErrStreamDead`，才说得出「网络断了，不是歌坏了」；
//     v0.7.0 把「网络断了」一律说成「容器坏了」，那是一句假话。
//
// ⚠️ **本文件里没有一处匹配 `ErrCorrupt`**（那是升级时要查的地方，查过了：
// 一处也没有）。`decoder.go` 的 `fill()` 认的是 `errors.Is(err, io.EOF)`，
// 而 v0.8.0 保证「没有一种读失败会匹配 `io.EOF`」——所以「流拉不动了」不会
// 被误当成「流完了」而安静地停住。

import (
	"errors"
	"fmt"
	"io"
	"time"

	m4a "github.com/tphakala/go-m4a"
)

// aacUnitLen 是 AAC-LC 一帧的采样数（每声道）。
//
// ⚠️ **这是按 AAC-LC 算的**：AAC 的帧长也可以是 960（`frameLengthFlag` 为 1），
// 而 ADTS 的头里**没有**这一位——所以 ADTS 那一路只能按 1024 算。
// 960 帧长在实践中几乎不出现（B 站发的是 AAC-LC），但这不是「不可能」。
const aacUnitLen = 1024

// isoBMFFBoxes 是 MP4 家族的文件可能以哪一种盒子开头。
//
// 正常封装的是 `ftyp`，分片流（DASH 那种 .m4s）是 `styp`，有的封装器把
// `moov` 放在最前面。
var isoBMFFBoxes = []string{"ftyp", "styp", "moov", "free", "skip", "mdat", "wide"}

// isBMFF 报 b 是不是一个 MP4 家族的盒子类型。
func isBMFF(b []byte) bool {
	for _, box := range isoBMFFBoxes {
		if string(b) == box {
			return true
		}
	}
	return false
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
	// unitLen 是一帧（access unit）有多少个采样（每声道）。
	unitLen int
	// total 是这一条流一共有多少帧（每声道），-1 是不知道。
	total int64
	// measure 数一数这一条流一共有多少帧（每声道），-1 是数不出来。
	//
	// 只有裸 ADTS 用得上：它没有容器，没有长度可读，而「不知道有多长」在
	// 界面上的表现是**进度条没有长度**（`Player.Duration()` 返回 0）。
	measure func() int64
	// next 拿下一帧。
	next func() ([]byte, error)
	// seek 把源挪到含第 sample 个采样（每声道，**从呈现出来的音频算起**）的
	// 那一帧，并报「从那一帧的开头起，要丢掉多少个采样（每声道）」。
	//
	// ⚠️ **「sample」是解码器输出的坐标**，而解码器**吞掉了它输出的第一帧**
	//（MDCT 重叠不完整的冷启动帧 = 编码器的预热）——所以输出的第 0 个采样在
	// 码流的第 1 帧里。
	//
	// 实现会**往回退一帧**再解：解码器会吞掉**它输出的第一帧**，所以退一帧
	// 之后它交出来的第一帧正好是目标所在的那一帧（**一退一吞，抵掉**）。
	seek func(sample int64) (int, error)
}

// openFrameSource 认出 r 是哪一种容器，拆成帧源。
//
// 认容器按**头几个字节**，不按扩展名：B 站下载下来的音频有的叫 .m4a 有的
// 叫 .m4s，内容却一样。
//
// 认不出来时它报 `ErrFormat`（不是「这个平台没解码器」——那是 `Open` 管的
// 事），而 r 的位置**不变**：调用方还要拿同一个源去试别的解码器。
func openFrameSource(r io.ReadSeeker) (*frameSource, error) {
	start, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	var head [12]byte
	// 读不满不是错：一个 12 字节都不到的流什么容器都不是。
	n, _ := io.ReadFull(r, head[:])
	if _, err := r.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	switch detectContainer(head[:n]) {
	case "adts":
		return openADTS(r)
	case "mp4":
		return openM4A(r)
	default:
		return nil, fmt.Errorf("%w: starts with % x", ErrFormat, head[:min(n, 12)])
	}
}

// detectContainer 认出头几个字节是哪一个容器。
//
// 返回 "adts" / "mp4"，认不出来返回空串。
//
// ⚠️ **MP3 认成 ADTS 是最危险的一种认错**：两者都以 `FF` 开头，只断前 12 个
// 同步位的话 MP3 的帧头（`FF FB` / `FF FA` / `FF F3` …）也认得出来——而 MP3 的
// layer 位是 01/10/11，ADTS 必须是 00。
//
// 认错的后果不是报错，是**解出别的东西**：一个 MP3 被当 ADTS 拆，拆出来的
// 「帧」是垃圾，解出来是噪声。
func detectContainer(head []byte) string {
	switch {
	case len(head) >= 2 && head[0] == 0xFF && head[1]&0xF6 == 0xF0:
		// ADTS 的同步字是 12 个 1，后面接一位「是不是 MPEG-4」、**两位 layer
		// 必须是 00**、一位「有没有 CRC」。
		return "adts"
	case len(head) >= 8 && isBMFF(head[4:8]):
		return "mp4"
	default:
		return ""
	}
}

// openM4A 用 go-m4a 拆一个 MP4 家族的文件。
//
// 返回的 source 里的帧是**裸** AAC（没有 ADTS 头）——MP4 里存的就是那样。
func openM4A(r io.ReadSeeker) (*frameSource, error) {
	rd, err := m4a.NewReader(r)
	if err != nil {
		// ⚠️ **这个错要用 `%w` 原样传上去**：go-m4a v0.8.0 起，「流拉不动了」
		// 报的是包着底层原因的错，而上层靠 `errors.Is` 认出它才说得出
		// 「网络断了，不是歌坏了」。把它换成一句「认不出这个 MP4」的话，
		// 真实原因就没了——而用户看到的正是那句人话。
		return nil, fmt.Errorf("mfadec: 认不出这个 MP4：%w", err)
	}
	info := rd.Info()
	if info.Codec != m4a.CodecAACLC {
		return nil, fmt.Errorf("mfadec: 这个 MP4 里不是 AAC（是 %s）", info.Codec)
	}
	if len(info.ASC) == 0 {
		return nil, fmt.Errorf("mfadec: 这个 MP4 里没有 AudioSpecificConfig")
	}
	unitLen := aacFrameLen(info.ASC)
	src := &frameSource{
		payloadType: aacPayloadRaw,
		rate:        info.SampleRate,
		chans:       info.Channels,
		asc:         info.ASC,
		unitLen:     unitLen,
		// 呈现出来的长度：容器的编辑列表已经把编码器的预热算在外面了。
		total: presentedFrames(info.Duration, info.SampleRate),
		next:  rd.ReadFrame,
	}
	src.seek = func(sample int64) (int, error) {
		d := time.Duration(sample) * time.Second / time.Duration(info.SampleRate)
		idx, off, err := rd.SeekTime(d)
		if err != nil {
			return 0, fmt.Errorf("mfadec: 跳到第 %d 帧：%w", sample, err)
		}
		if idx <= 0 {
			// 目标就在第一帧里：没有前一帧可以退。
			return int(off), nil
		}
		// 往回退一帧：解码器**自己会吞掉**它输出的第一帧（那一帧是 MDCT
		// 重叠不完整的「冷启动」帧，也就是编码器的预热），所以退一帧之后，
		// 解码器交出来的第一帧正好是目标所在的那一帧。
		//
		// ⚠️ **退一帧 + 解码器吞一帧 = 一帧不多一帧不少**。多退一帧的话，
		// 跳过去的位置会**早 1024 个采样**（23 毫秒）——肉耳听不出来，
		// 但拖进度条之后听到的位置和界面上写的差了一拍。
		if err := rd.SeekSample(idx - 1); err != nil {
			return 0, fmt.Errorf("mfadec: 跳到第 %d 帧：%w", sample, err)
		}
		return int(off), nil
	}
	return src, nil
}

// adtsSource 拆裸 ADTS。
//
// 它是个结构体而不是一堆闭包，因为**跳位置**要能把源拨回开头重扫一遍。
type adtsSource struct {
	r     io.ReadSeeker
	start int64
	// pend 是已经读出来、还没交出去的那一帧。
	pend []byte
}

// openADTS 拆一个裸 ADTS 的流。
//
// ADTS 的每一帧前面有一个 7 字节（带 CRC 是 9 字节）的头，头里写着这一帧
// 总共有多长——所以拆帧只要顺着头走，不用找同步字。
func openADTS(r io.ReadSeeker) (*frameSource, error) {
	start, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	first, info, err := readADTSFrame(r)
	if err != nil {
		return nil, err
	}
	asc, err := ascFromADTS(info)
	if err != nil {
		return nil, err
	}
	s := &adtsSource{r: r, start: start, pend: first}
	src := &frameSource{
		payloadType: aacPayloadADTS,
		rate:        info.SampleRate,
		chans:       info.Channels,
		asc:         asc,
		unitLen:     aacUnitLen,
		// 裸 ADTS 里没有容器说的时长，所以长度不知道——要数出来。
		total:   -1,
		measure: s.measure,
		next:    s.next,
		seek:    s.seek,
	}
	return src, nil
}

// measure 数一数这条流一共有多少帧。
//
// 它要**从头扫一遍**（ADTS 没有索引表），扫完把游标拨回原处——所以扫完
// 之后解码还能接着往下走。
func (s *adtsSource) measure() int64 {
	pos, err := s.r.Seek(0, io.SeekCurrent)
	if err != nil {
		return -1
	}
	if _, err := s.r.Seek(s.start, io.SeekStart); err != nil {
		return -1
	}
	var frames int64
	for {
		if _, _, err := readADTSFrame(s.r); err != nil {
			break
		}
		frames++
	}
	if _, err := s.r.Seek(pos, io.SeekStart); err != nil {
		return -1
	}
	if frames == 0 {
		return -1
	}
	// ⚠️ **减一帧**：解码器**吞掉了它输出的第一帧**（MDCT 重叠不完整的冷启动
	// 帧 = 编码器的预热），那一帧不属于呈现出来的音频。数帧数的是码流里的帧，
	// 呈现出来的比它少一帧。
	return (frames - 1) * int64(aacUnitLen)
}

// next 拿下一帧。
func (s *adtsSource) next() ([]byte, error) {
	if s.pend != nil {
		out := s.pend
		s.pend = nil
		return out, nil
	}
	f, _, err := readADTSFrame(s.r)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// seek 把源挪到含第 sample 个采样（每声道）的那一帧。
//
// ADTS 里**没有**采样索引：每一帧都是 1024 个采样（按 AAC-LC 算），所以
// 第几帧是算得出来的。代价是要**从头再扫一遍**——ADTS 没有索引表可查。
func (s *adtsSource) seek(sample int64) (int, error) {
	if sample < 0 {
		return 0, fmt.Errorf("mfadec: 跳到第 %d 帧：%w", sample, ErrSeekRange)
	}
	// ⚠️ **「第 sample 个采样」是解码器输出的坐标**，而解码器**吞掉了它输出的
	// 第一帧**（MDCT 重叠不完整的冷启动帧 = 编码器的预热）。所以输出的第 0 个
	// 采样在**码流的第 1 帧**里——坐标差一帧。
	skip := int(sample % int64(aacUnitLen))
	unit := int(sample/int64(aacUnitLen)) + 1
	// 拨回开头。
	if _, err := s.r.Seek(s.start, io.SeekStart); err != nil {
		return 0, fmt.Errorf("mfadec: 跳到第 %d 帧：%w", sample, err)
	}
	s.pend = nil
	// 停在目标帧的**前一帧**：解码器会吞掉它输出的第一帧（冷启动帧），
	// 所以退一帧之后，解码器交出来的第一帧正好是目标所在的那一帧。
	for i := 0; i < unit-1; i++ {
		if _, _, err := readADTSFrame(s.r); err != nil {
			return 0, fmt.Errorf("mfadec: 跳到第 %d 帧：%w", sample, err)
		}
	}
	return skip, nil
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
		// ⚠️ **干净的末尾要原样返回 `io.EOF`**，不能包一层。
		//
		// 包了之后 `err == io.EOF` 就不成立了（`errors.Is` 才成立），而调用方
		// `ReadAll` 认的是 `==`——于是「正常读完」被当成「读出错」，
		// 下面那次 `Drain()`（把 MFT 手里攒着的最后一帧放出来）**执行不到**，
		// 解出来的 PCM 少最后一帧。
		if errors.Is(err, io.EOF) {
			return nil, ADTSInfo{}, io.EOF
		}
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

// aacFrameLen 从 AudioSpecificConfig 里读出一帧有多少个采样。
//
// AAC-LC 是 1024；`frameLengthFlag` 为 1 时是 960。
//
// 那一位在 ASC 的**第二个字节**的第 2 位（从低位数）：ASC 是大端的，第二
// 个字节是低半字节，位 0~2 是 `frameLengthFlag` / `dependsOnCoreCoder` /
// `extensionFlag`。
//
// ⚠️ **只有 2 字节的 ASC 才有这个布局**：HE-AAC 的 ASC 更长（多一个扩展），
// 里面的位含义不一样。遇到不是 AAC-LC 的一律按 1024 算——那一档本项目
// **还没验过**（素材造不出来），所以这里的偏差不会被真机抓到。
func aacFrameLen(asc []byte) int {
	if len(asc) != 2 {
		return aacUnitLen
	}
	// audioObjectType 占头一个字节的高 5 位：2 是 AAC-LC。
	if aot := (asc[0] >> 3) & 0x1F; aot != 2 {
		return aacUnitLen
	}
	if (asc[1]>>2)&1 == 1 {
		return 960
	}
	return aacUnitLen
}
