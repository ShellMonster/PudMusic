// Package mp3dec 解 MP3（MPEG-1/2/2.5 Layer III），并把它注册进 MyGo 的格式表。
//
// # 为什么不用 MyGo 自带的那个
//
// MyGo 自带的 `mygo/audio/mp3` 用的是 `hajimehoshi/go-mp3`（Apache-2.0），
// 而那个库**已经归档**，并且有三件事做不对：
//
//  1. **不支持 MPEG-2.5**——`internal/frame/frame.go` 里明写着遇到 2.5 就报错。
//  2. **完全不做 LAME gapless**——编码器延迟（1105 个样本）原样留在输出里，
//     歌的开头会多出一段静音，结尾也会多出一段。
//  3. **`NewDecoder` 要把整个文件扫一遍**才能算出长度（`ensureFrameStartsAndLength`
//     一帧一帧地读，还给每一帧记一个偏移）。本地音乐列表扫几百个文件时，
//     那是几百次全文件读。
//
// 换掉的是 `tphakala/go-mp3`（MIT），**vendor 进来**、不进 `go.mod`——
// 来路、版本、许可证原文和「为什么 vendor 而不是进 go.mod」写在同目录的
// `PROVENANCE.md` 里。
//
// # 它和 MyGo 的注册表怎么接
//
// 注册在**本包**的 `init()` 里，而不是靠 `mygo/audio/mp3` 那一行 blank import：
// MyGo 的注册表是「先注册先匹配」，两处都注册的话，谁先注册谁说了算——
// 而 `mygo/audio/mp3` 的 init 依赖 `mygo/audio`，本包也依赖 `mygo/audio`，
// 顺序就说不清了。所以那一行 import **撤掉了**，注册只留这一处。
//
// 认容器靠的是**文件头**，不是扩展名：ID3v2 标签（"ID3"）或者一帧 MPEG-1/2/2.5
// Layer III 的同步字。三个 MPEG 版本的同步字都注册了，所以 MPEG-2.5 的
// `0xFF 0xE3` 也认得出来。
package mp3dec

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	mygoaudio "github.com/egoist/mygo/audio"

	"github.com/ShellMonster/Biu_Go/internal/service/audio/mp3dec/gomp3/pcm"
)

// init 把 MP3 注册进 MyGo 的格式表。
//
// ⚠️ **注册顺序有讲究**：MyGo 的表是「按顺序找第一条命中的」，而裸 ADTS 的
// 同步字是 `"\xff??"`（`internal/service/audio/containers.go` 注册的）——它匹配
// **任何**以 0xFF 开头的文件，MP3 也在内。所以 MP3 的那几条必须排在它前面，
// 否则 MP3 会被认成 ADTS 交给系统解码器，然后报一句听不懂的错。
//
// 顺序为什么是这样：本包依赖 `mygo/audio`，所以 `mygo/audio` 先初始化
// （它自己注册 wav），本包接着注册 mp3，而 `internal/service/audio` 包最后
// 初始化（它注册 mp4/aac）。**mp3 一定在 `"\xff??"` 前面**。
func init() {
	open := func(r io.ReadSeeker) (mygoaudio.Decoder, error) { return Open(r) }
	// ID3v2 标签。
	mygoaudio.RegisterFormat("mp3", "ID3", open)
	// MPEG-1/2/2.5 Layer III 的同步字：第 2 字节的高 3 位是 111，第 4~5 位是
	// 版本（11=MPEG-1、10=MPEG-2、00=MPEG-2.5），第 6~7 位是层（01=Layer III），
	// 最后一位是「有没有 CRC」。
	for _, b := range []string{
		"\xff\xfb", "\xff\xfa", // MPEG-1 Layer III
		"\xff\xf3", "\xff\xf2", // MPEG-2 Layer III
		"\xff\xe3", "\xff\xe2", // MPEG-2.5 Layer III
	} {
		mygoaudio.RegisterFormat("mp3", b, open)
	}
}

// Decoder 是一个 MP3 解码器，实现 MyGo 的 `audio.Decoder`。
//
// 它包着 `pcm.Decoder`（vendor 进来的 `tphakala/go-mp3/pcm`）：那一条已经把
// ID3v2 跳过、Xing/Info/VBRI 标签、LAME gapless 的头尾裁剪、精确 seek 全做了。
type Decoder struct {
	d *pcm.Decoder
	// buf 是 Read 的中转站：`pcm` 吐的是**字节**（默认 S16，加 `WithF32`
	// 之后是原生 float32），而 MyGo 要的是 `[]float32`。
	buf []byte
}

// Open 打开一个 MP3。
//
// r 是**从头**读的字节源；它同时得是 `io.Seeker`，因为精确 seek（跳到歌的
// 某一处）要靠帧头走一遍。
func Open(r io.ReadSeeker) (*Decoder, error) {
	src := &source{src: r, allowEnd: canProbeEnd(r)}
	d, err := pcm.NewDecoder(src, pcm.WithF32())
	if err != nil {
		return nil, fmt.Errorf("mp3: %w", err)
	}
	return &Decoder{d: d}, nil
}

// SampleRate 返回采样率（每秒多少个样本）。
func (d *Decoder) SampleRate() int { return d.d.Info().SampleRate }

// Channels 返回声道数（1 或 2）。
func (d *Decoder) Channels() int { return d.d.Info().Channels }

// Len 返回这首歌一共有多少帧（每声道一个样本算一帧），不知道时是 -1。
//
// 「帧」是 MyGo 的说法：`SetPosition` 收的也是这个数，`Read` 一次吐多少个
// `float32` 就是多少帧。
//
// ⚠️ **这个数是「能播的那一段」的长度**，不是文件里音频字节除以帧长：
// 带 LAME gapless 标签的文件，编码器延迟和末尾的填充都已经被裁掉了
// （那是 `pcm` 干的），所以这里报出来的就是真正听得见的那一段。
func (d *Decoder) Len() int64 {
	n := d.d.Info().TotalSamples
	if n == 0 {
		return -1 // 长度不知道
	}
	if n > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(n)
}

// Read 解出接下来的样本填进 buf，返回写了多少个（**每声道一个**算一个）。
//
// buf 的长度得是**帧数**（不是样本数）：双声道一帧两个 `float32`。
// 末尾返回 `(0, io.EOF)`。
func (d *Decoder) Read(buf []float32) (int, error) {
	want := len(buf)
	if want == 0 {
		return 0, nil
	}
	need := want * 4 // float32 是 4 字节
	if cap(d.buf) < need {
		d.buf = make([]byte, need)
	}
	b := d.buf[:need]

	got := 0
	for got < need {
		n, err := d.d.Read(b[got:])
		got += n
		if err != nil {
			if got == 0 {
				return 0, err
			}
			// 手上有货就先交出去：交了一半才报 io.EOF 的话，MyGo 那边的
			// 环形缓冲会丢掉这半段（它把 err != nil 当成「这一轮到此为止」）。
			// 下一轮 Read 再报 io.EOF。
			break
		}
		if n == 0 {
			// `pcm` 的 Read 不会返回 (0, nil)（它要么有货要么报错），但万一
			// 哪天变了，这里不能变成死循环。
			break
		}
	}
	// got 一定是 4 的倍数：`pcm` 吐的样本是整帧的，一次吐的字节数是
	// 帧数 × 声道数 × 4。
	for i := 0; i < got/4; i++ {
		buf[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return got / 4, nil
}

// SetPosition 跳到从开头算起的第 frame 帧。
//
// 跳过去之后解出来的音频和从头解到那儿**逐样本相同**（`pcm` 会先解 16 帧
// 把位 reservoir 和 MDCT 重叠喂饱，所以落点那一帧是对的）。
func (d *Decoder) SetPosition(frame int64) error {
	if _, err := d.d.SeekToSample(frame); err != nil {
		return fmt.Errorf("mp3: %w", err)
	}
	return nil
}

// source 是解码器读的那个字节源。它只挡一件事：「seek 到末尾」。
//
// # 为什么挡
//
// MP3 的长度**不在文件头里**：没有 Xing/Info 标签时，唯一能算出长度的办法是
// 量一遍音频字节的范围，而那需要知道文件末尾在哪。对**本地文件**这是一次
// lseek，代价可以忽略；对**网络流**那是「先把剩下的下完」——用户盯着喇叭
// 等半天，一个样本都没出来。
//
// 所以 `allowEnd` 只对「问末尾不花钱」的源打开（见 `canProbeEnd`）。
// 挡掉之后，`pcm` 那边的定长估算（CBR）就不做了，缺长度时 `Len()` 报 -1。
type source struct {
	src      io.ReadSeeker
	allowEnd bool
}

// Read 读源。
func (s *source) Read(p []byte) (int, error) { return s.src.Read(p) }

// Seek 定位源。「seek 到末尾」在 `allowEnd` 为假时报错，而不是真的去下完整个文件。
func (s *source) Seek(off int64, whence int) (int64, error) {
	if whence == io.SeekEnd && !s.allowEnd {
		return 0, errNoCheapEnd
	}
	return s.src.Seek(off, whence)
}

// errNoCheapEnd 是「这个源问不到末尾」时的错。
//
// 它是**能力**上的错，不是文件坏了：解码器照样能放，只是报不出长度。
var errNoCheapEnd = errors.New("mp3dec: 这个字节源问不到末尾")

// canProbeEnd 报「问这个源末尾在哪」是不是不花钱。
//
// 判据是「这个源能 stat 自己」：本地文件（`*os.File`）能，一次 lseek 就知道
// 多长；网络流不能——它得先把剩下的下完才知道自己多长，而那正是要挡的事。
//
// ⚠️ **别把判据放宽成「源知道自己的长度」**：在线流确实知道（HTTP 的
// Content-Length 就在手上），但知道长度之后还要读**末尾 128 字节**去看有没有
// ID3v1 标签，那一下照样要把整个流下完。所以判据是「能不花钱地读到末尾」，
// 不是「知道长度」。
func canProbeEnd(r io.ReadSeeker) bool {
	s, ok := r.(interface {
		Stat() (os.FileInfo, error)
	})
	if !ok {
		return false
	}
	fi, err := s.Stat()
	return err == nil && fi.Mode().IsRegular()
}
