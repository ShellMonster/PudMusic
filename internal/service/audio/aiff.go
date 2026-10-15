package audio

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"math"

	mygoaudio "github.com/egoist/mygo/audio"
)

// AIFF 与 AIFC 里未压缩的 PCM 解码器。
//
// 计划原本写「AIFF 走 macOS ExtAudioFile」（`PLAN.md` §1.2 决策 13）。实测下来不需要：
// AIFF 就是一个 IFF 容器（FORM → COMM + SSND），样本是大端定点数，约 170 行就能解，
// 省掉一整层 purego 绑定，也不必等下一阶段的 AAC 才落地。AIFC 只认 NONE（大端 PCM），
// 压缩过的 AIFC（alaw、ulaw 等）会明确报不支持，而不是播出一段噪声。
func init() {
	mygoaudio.RegisterFormat("aiff", "FORM????AIFF", func(r io.ReadSeeker) (mygoaudio.Decoder, error) {
		return newAIFF(r)
	})
	mygoaudio.RegisterFormat("aifc", "FORM????AIFC", func(r io.ReadSeeker) (mygoaudio.Decoder, error) {
		return newAIFF(r)
	})
}

var errAIFF = errors.New("audio: invalid AIFF")

type aiffDecoder struct {
	r        io.ReadSeeker
	br       *bufio.Reader
	rate     int
	channels int
	bits     int
	start    int64 // 样本数据相对文件头的偏移
	length   int64 // 帧数，未知时为 -1
	pos      int64 // 当前帧
	buf      []byte
}

func newAIFF(r io.ReadSeeker) (*aiffDecoder, error) {
	start, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}
	var form [12]byte
	if _, err := io.ReadFull(r, form[:]); err != nil {
		return nil, errAIFF
	}
	d := &aiffDecoder{r: r, length: -1}
	aifc := string(form[8:12]) == "AIFC"
	off := start + 12
	haveCOMM := false
	for {
		var h [8]byte
		if _, err := io.ReadFull(r, h[:]); err != nil {
			return nil, errAIFF
		}
		size := int64(binary.BigEndian.Uint32(h[4:]))
		off += 8
		switch string(h[:4]) {
		case "COMM":
			if size < 18 {
				return nil, errAIFF
			}
			b := make([]byte, size)
			if _, err := io.ReadFull(r, b); err != nil {
				return nil, errAIFF
			}
			d.channels = int(binary.BigEndian.Uint16(b))
			d.length = int64(binary.BigEndian.Uint32(b[2:]))
			d.bits = int(binary.BigEndian.Uint16(b[6:]))
			// 采样率是 80 位 IEEE 扩展精度浮点，占 10 字节。
			d.rate = int(extended80(b[8:18]))
			if aifc {
				// AIFC 在采样率之后是四字节压缩类型名。NONE 就是本文件。
				if size < 22 || string(b[18:22]) != "NONE" {
					return nil, errors.New("audio: AIFC of a compression other than NONE")
				}
			}
			if d.channels < 1 || d.rate < 1 || d.bits%8 != 0 || d.bits < 8 || d.bits > 32 {
				return nil, errAIFF
			}
			haveCOMM = true
		case "SSND":
			if !haveCOMM {
				return nil, errAIFF
			}
			if size < 8 {
				return nil, errAIFF
			}
			var head [8]byte
			if _, err := io.ReadFull(r, head[:]); err != nil {
				return nil, errAIFF
			}
			// offset 是写这块声音时在文件里留下的空隙，不是当前读到的位置。
			d.start = off + 8 + int64(binary.BigEndian.Uint32(head[:]))
			if d.length > 0 {
				// 文件自己说的帧数不能超过实际有的帧数。
				if have := (size - 8) / int64(d.frameBytes()); d.length > have {
					d.length = have
				}
			}
			d.br = bufio.NewReader(r)
			return d, nil
		}
		size += size & 1 // 每个 chunk 补齐到偶数字节
		if _, err := r.Seek(off+size, io.SeekStart); err != nil {
			return nil, errAIFF
		}
		off += size
	}
}

// extended80 解一个 80 位 IEEE 扩展精度浮点。AIFF 的采样率只有用这种格式才装得下
// 44100 这样的整数值。
func extended80(b []byte) float64 {
	exp := int(b[0]&0x7f)<<8 | int(b[1])
	var mant uint64
	for _, c := range b[2:10] {
		mant = mant<<8 | uint64(c)
	}
	if exp == 0 && mant == 0 {
		return 0
	}
	v := math.Ldexp(float64(mant), exp-16383-63)
	if b[0]&0x80 != 0 {
		return -v
	}
	return v
}

func (d *aiffDecoder) frameBytes() int { return d.channels * d.bits / 8 }
func (d *aiffDecoder) SampleRate() int { return d.rate }
func (d *aiffDecoder) Channels() int   { return d.channels }
func (d *aiffDecoder) Len() int64      { return d.length }

func (d *aiffDecoder) Read(dst []float32) (int, error) {
	fb := d.frameBytes()
	n := len(dst) / d.channels
	if d.length >= 0 {
		n = int(min(int64(n), d.length-d.pos))
	}
	if n <= 0 {
		return 0, io.EOF
	}
	if cap(d.buf) < n*fb {
		d.buf = make([]byte, n*fb)
	}
	b := d.buf[:n*fb]
	got, err := io.ReadFull(d.br, b)
	n = got / fb
	if n == 0 {
		if err == nil || err == io.ErrUnexpectedEOF {
			err = io.EOF
		}
		return 0, err
	}
	bs := d.bits / 8
	for i := range n * d.channels {
		s := b[i*bs:]
		var v float32
		switch bs {
		case 1:
			v = float32(int8(s[0])) / (1 << 7)
		case 2:
			v = float32(int16(binary.BigEndian.Uint16(s))) / (1 << 15)
		case 3:
			// 24 位只有三个字节，符号扩展到 32 位再右移回来。
			v = float32(int32(uint32(s[0])<<24|uint32(s[1])<<16|uint32(s[2])<<8)>>8) / (1 << 23)
		default:
			v = float32(int32(binary.BigEndian.Uint32(s))) / (1 << 31)
		}
		dst[i] = v
	}
	d.pos += int64(n)
	return n * d.channels, nil
}

func (d *aiffDecoder) SetPosition(frame int64) error {
	if frame < 0 || d.length >= 0 && frame > d.length {
		return errors.New("audio: seek out of range")
	}
	if _, err := d.r.Seek(d.start+frame*int64(d.frameBytes()), io.SeekStart); err != nil {
		return err
	}
	d.br = bufio.NewReader(d.r)
	d.pos = frame
	return nil
}
