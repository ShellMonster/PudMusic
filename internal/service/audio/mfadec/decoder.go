package mfadec

// 解码器这一层：把「一串 AAC 帧」变成「MyGo 的 audio 包能播的交织 float32」。
//
// 这一层是**平台无关**的：拆帧在 `source.go`，解码在 Windows 上是 Media
// Foundation（`transform_windows.go`），而这里只管把两者接起来、把采样交出去。
// 平台无关的意思是**这一层在 macOS 上也能编译、也能测**——用一个假的解码器
// 就能验「采样怎么数、丢多少、跳到哪」这些算术，而那些算术错了症状是
// 「播出来的东西错位几百毫秒」，在真机上极难看出来。
//
// 它刻意**不 import MyGo 的 audio 包**（理由和 `flacdec` 一样）：一是这一层
// 不需要音频设备就能测，二是 `internal/licensing` 的
// `TestWindowsAudioPullsInNothingNew` 盯着「Windows 音频那一包只依赖 go-m4a
// 一个外部模块」，多一个依赖就是多一份许可证要随包发出去。

import (
	"errors"
	"fmt"
	"io"
	"time"
)

// 这一包返回的错误。
var (
	// ErrUnsupported 是「这个平台没有 Media Foundation」时 Open 返回的错误。
	//
	// 它和「文件解不了」是两回事：前者是平台没能力，后者是文件本身不对，
	// 两者在界面上要显示不同的提示，所以分开（和 `extaf.ErrUnsupported` 同一个
	// 道理）。
	ErrUnsupported = errors.New("mfadec: this platform has no Media Foundation audio decoder")

	// ErrFormat 是「这串字节不是 AAC」——不是 MP4 家族里的 AAC，也不是裸 ADTS。
	ErrFormat = errors.New("mfadec: not an AAC stream")

	// ErrSeekRange 是 SetPosition 落在 [0, Len()] 之外。
	ErrSeekRange = errors.New("mfadec: seek out of range")

	errClosed = errors.New("mfadec: decoder is closed")
)

// aacCodec 是「解码器」那一半，Decoder 只看得见这五个方法。
//
// 真正的实现是 `*Codec`（Media Foundation 的 `IMFTransform`），只在 Windows
// 上编译。这一层用接口而不是具体类型，`decoder.go` 才能在没有 Windows 的
// 机器上编译——而这一层恰恰是最需要被测的那一层（见文件顶上那段）。
type aacCodec interface {
	aacDecoder
	// SampleRate 是输出采样率。
	SampleRate() int
	// Channels 是输出声道数。
	Channels() int
	// Close 收掉解码器。
	Close() error
}

// Decoder 解一条 AAC 码流。
//
// 它满足 MyGo 的 `audio.Decoder`：
//
//	func SampleRate() int
//	func Channels() int
//	func Read([]float32) (int, error) // n 是**采样**数，不是帧数
//	func SetPosition(frame int64) error
//	func Len() int64
//
// 外加 `io.Closer`。它不是并发安全的：MyGo 从一条 goroutine 上驱动一个
// 解码器（`audio.NewPlayer`），主播放器和迷你播放器各有一个。
type Decoder struct {
	src   *frameSource
	codec aacCodec
	// newCodec 是「再开一个解码器」用的（跳位置之后要重开）。
	//
	// 生产代码里它就是 `openCodec`；测试换成假的——没有这一层，跳位置那条路
	// 在没有 Windows 的机器上**一条都测不到**。
	newCodec func(*frameSource) (aacCodec, error)

	// buf 是解出来还没交出去的 PCM（交织），head 是下一个要交出去的采样。
	buf  []float32
	head int

	rate  int
	chans int
	// total 是这一条流一共有多少帧（每声道），-1 是不知道。
	total int64
	// pos 是已经交出去多少帧（每声道）。
	pos int64
	// skip 是还要丢掉多少个采样（每声道）才轮到交出去。
	//
	// ⚠️ **它只有一个来源：seek**（跳过去的那一段）。
	//
	// 它**不是**容器的 `EncoderDelay`——解码器的输出本来就从呈现音频的开头起
	//（解码器自己吞掉了它输出的第一帧，也就是编码器的预热），按 `EncoderDelay`
	// 再丢一遍就是丢了两遍。理由见 `Open` 里那段。
	skip int
	// drained 记着「已经 drain 过了」——流读完只 drain 一次。
	drained bool

	atEnd   bool
	closed  bool
	pending error
}

// Open 打开一条 AAC 码流。
//
// 它认两种容器，和 B 站发下来的两种形状一一对应：
//
//   - **MP4 家族**（`.m4a` / `.m4s`）：`ftyp` / `styp` 开头，里面是裸 AAC 帧
//     （没有 ADTS 头）加上一个 AudioSpecificConfig。B 站有损流和本地下载的
//     `.m4a` 都是它。
//   - **裸 ADTS**（`.aac`）：没有容器，每一帧前面有 7（或 9）字节的 ADTS 头。
//
// r 的位置**不会因为失败而变**：认不出来的时候它会回到自己进来时的位置，
// 这样调用方还能拿同一个源去试别的解码器（`openDecoder` 正是这么用的：
// Media Foundation 认不出来之后还要落回 `flacdec`）。
func Open(r io.ReadSeeker) (*Decoder, error) {
	if err := platformAvailable(); err != nil {
		return nil, err
	}
	start, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	src, err := openFrameSource(r)
	if err != nil {
		// 认不出来的时候要把源**拨回去**：调用方还要拿同一个源去试别的
		// 解码器（`internal/service/audio` 的 `openDecoder` 正是这么用的：
		// Media Foundation 认不出来之后还要落回 `flacdec`）。
		if _, serr := r.Seek(start, io.SeekStart); serr != nil {
			return nil, errors.Join(err, serr)
		}
		return nil, err
	}
	codec, err := openCodec(src)
	if err != nil {
		if _, serr := r.Seek(start, io.SeekStart); serr != nil {
			return nil, errors.Join(err, serr)
		}
		return nil, err
	}
	return &Decoder{
		src:      src,
		codec:    codec,
		newCodec: openCodec,
		rate:     codec.SampleRate(),
		chans:    codec.Channels(),
		total:    src.total,
		// ⚠️ **这里不丢容器的 `EncoderDelay`**——丢两遍就把音频挪早了。
		//
		// 理由（issue #128 的评审 + 本仓库量过的数）：**解码器的输出本来就从
		// 呈现音频的开头起**。AAC 的 MDCT 是有重叠的，解码器**自己吞掉**第一帧
		// 那半个重叠（那一帧是「不完整的」，也就是编码器的预热），所以：
		//
		//   - 原始输出 = 173 帧 = 177152 个采样，**正好等于** ffmpeg 解同一份
		//     .m4a 的结果（`fixture_test.go` 的 `refFrames`）；
		//   - macOS 的 AudioToolbox 给 176400 帧，而它和参考 WAV 的**前 176400
		//     个采样**逐样本对齐（平均差 29/32768、最大 2313/32768）。
		//
		// 也就是说：那 1024 个预热采样**根本不在解码输出里**。再按
		// `EncoderDelay` 丢一遍，就等于**凭空砍掉 1024 个真实采样**：音频比
		// 参考晚 23 毫秒，而且比容器说的呈现长度少 272 个采样。
		//
		// 详见 `mfadec/decoder_test.go` 的 `TestTheDecoderDoesNotDropTheEncoderDelayTwice`。
		skip: 0,
	}, nil
}

// SampleRate 返回这一条流的采样率。
func (d *Decoder) SampleRate() int { return d.rate }

// Channels 返回这一条流的声道数。
func (d *Decoder) Channels() int { return d.chans }

// Len 返回这一条流一共有多少帧（每声道），不知道时是 -1。
//
// 它是**呈现出来的**长度：容器的编码器延迟不算在里面，所以它和 Read
// 交出去的采样对得上（和 `flacdec` 的口径一样）。
//
// ⚠️ **裸 ADTS 的长度是数出来的**：ADTS 没有容器，长度只能靠扫一遍整个流
// 数帧。这一趟是**懒的**——只有真的有人问长度时才扫（问的是界面上那个
// 进度条），扫完把游标拨回原处，解码不受影响。
func (d *Decoder) Len() int64 {
	if d.total < 0 && d.src.measure != nil {
		d.total = d.src.measure()
	}
	return d.total
}

// Close 收掉解码器。可以重复调用。
func (d *Decoder) Close() error {
	if d.closed {
		return nil
	}
	d.closed = true
	return d.codec.Close()
}

// Read 解出下一批采样写进 dst，返回写了多少个**采样**（不是帧）。
//
// 它返回的个数一定是声道数的整数倍：MyGo 的播放器按帧取样，拿到半个帧的
// 声道会把它当成左右声道错位。
//
// 流读完时返回 `io.EOF`；如果正好读完在 dst 的中间，n 少于 len(dst) 且 err
// 是 `io.EOF`。
func (d *Decoder) Read(dst []float32) (int, error) {
	if d.closed {
		return 0, errClosed
	}
	if d.pending != nil {
		return 0, d.pending
	}
	if d.atEnd {
		return 0, io.EOF
	}
	want := len(dst) / d.chans * d.chans
	n := 0
	if d.total >= 0 {
		// ⚠️ **容器说的呈现长度是要当真的**。MP4 的编辑列表会写「呈现
		// media_time=1024 起的 176400 个采样」——而码流里其实还有更多。
		// 多的那几百个采样**不属于这支曲子**（macOS 的 AudioToolbox 也是
		// 按编辑列表截的：同一份素材它给 176400 帧）。
		//
		// 不截的话：`Len()` 说的是 176400 帧，实际播出来 177152 帧——
		// 进度条走完了歌还没放完；而同一个文件在 macOS 和 Windows 上
		// 放出来的长度还不一样。
		left := int(d.total - d.pos)
		if left <= 0 {
			d.atEnd = true
			return n, io.EOF
		}
		if max := left * d.chans; want > max {
			want = max
		}
	}
	for n < want {
		if d.head >= len(d.buf) {
			d.buf, d.head = d.buf[:0], 0
			if err := d.fill(); err != nil {
				if errors.Is(err, io.EOF) {
					d.atEnd = true
					return n, io.EOF
				}
				// 坏掉的流就一直坏着：继续读它的调用方不能拿到一段
				// 「看起来像静音」的采样。
				d.pending = err
				return n, err
			}
			continue
		}
		m := copy(dst[n:want], d.buf[d.head:])
		d.head += m
		n += m
	}
	d.pos += int64(n / d.chans)
	return n, nil
}

// fill 解出下一批 PCM 放进 d.buf。
//
// 它返回 io.EOF 表示「流完了，而且没有剩下任何采样」——**不是**「这一帧
// 没有输出」。这两件事必须分开：AAC 解码器有 MDCT 的重叠状态，某一帧喂进去
// 不一定有 PCM 出来（MFT 自己攒着），那是正常的；而流真的完了，攒着的那一
// 帧要靠 Drain 放出来。
func (d *Decoder) fill() error {
	for {
		frame, err := d.src.next()
		if errors.Is(err, io.EOF) {
			if d.drained {
				return io.EOF
			}
			d.drained = true
			tail, derr := d.codec.Drain()
			if derr != nil {
				return derr
			}
			if len(tail) == 0 {
				return io.EOF
			}
			d.appendPCM(tail)
			return nil
		}
		if err != nil {
			return err
		}
		if len(frame) == 0 {
			continue
		}
		out, err := d.codec.Feed(frame)
		if err != nil {
			return err
		}
		if len(out) == 0 {
			// 这一帧没有输出是正常的：解码器自己攒着。
			continue
		}
		d.appendPCM(out)
		return nil
	}
}

// appendPCM 把一批 PCM 接到 d.buf 后面，顺手丢掉还该丢的采样。
func (d *Decoder) appendPCM(pcm []float32) {
	if d.skip > 0 {
		drop := d.skip * d.chans
		if drop > len(pcm) {
			drop = len(pcm)
		}
		// 只按整帧丢：丢一半会把左右声道拆散。
		drop -= drop % d.chans
		pcm = pcm[drop:]
		d.skip -= drop / d.chans
	}
	d.buf = append(d.buf, pcm...)
}

// SetPosition 跳到第 frame 帧（每声道，从呈现出来的音频算起）。
//
// ⚠️ **跳过去之后解出来的第一段是「热起来的」**：AAC 的 MDCT 有重叠状态，
// 冷启动解出来的第一帧是坏的。所以这里会**往回退一帧**再解，把那一帧丢掉
// ——听起来像浪费，其实不这么做的话每次跳都会「咔」一声。
//
// 解码器的重叠状态不能跨跳转带过去（那是上一帧的残留），所以跳完要**重新
// 开一个解码器**。
func (d *Decoder) SetPosition(frame int64) error {
	if d.closed {
		return errClosed
	}
	if d.pending != nil {
		return d.pending
	}
	if frame < 0 {
		return fmt.Errorf("mfadec: frame %d: %w", frame, ErrSeekRange)
	}
	if d.total >= 0 && frame > d.total {
		return fmt.Errorf("mfadec: frame %d 超出 [0, %d]: %w", frame, d.total, ErrSeekRange)
	}
	skip, err := d.src.seek(frame)
	if err != nil {
		return err
	}
	codec, err := d.newCodec(d.src)
	if err != nil {
		return err
	}
	d.codec.Close()
	d.codec = codec
	d.buf, d.head = d.buf[:0], 0
	d.skip = skip
	d.pos = frame
	d.atEnd = false
	// 新解码器要重新 drain 一次。
	d.drained = false
	return nil
}

// presentedFrames 把容器说的呈现时长换算成帧数。
//
// 先转成 float64 再算：帧数 × 秒 / 采样率用整数算会在几小时的音频上差出一截
// （纳秒 × 帧数早就溢出 int64 了）。
//
// 末尾**向上取整**：容器给的时长是「截断到纳秒」的，截断过的纳秒数总比一帧
// 少一点。
func presentedFrames(d time.Duration, rate int) int64 {
	if d <= 0 || rate <= 0 {
		return -1
	}
	ns := int64(d)
	if ns > 1<<62/int64(rate) {
		return -1
	}
	return (ns*int64(rate) + int64(time.Second) - 1) / int64(time.Second)
}
