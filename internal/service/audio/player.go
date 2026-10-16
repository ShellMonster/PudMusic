// Package audio decodes and plays audio — 本地文件和 B 站在线流。
//
// 依赖方向：这一层可以 import platform，但不能被 platform import；它不认识界面，
// 也不知道谁在调用，只负责「给一段字节，拿到一个能播、能停、能跳、能关、
// 能说出自己哪里错了的播放器」。
//
// 在线流走的是同一条解码链：`StreamSource` 是一个可取消的 `io.ReadSeeker`，
// 容器是按**头几个字节**认的（`containers.go`），所以「本地文件」和「B 站 CDN」
// 在这一层没有区别。
package audio

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/audio/flacdec"
	mygoaudio "github.com/egoist/mygo/audio"

	// MP3 由 MyGo 自带，这里只需要它的注册副作用。
	_ "github.com/egoist/mygo/audio/mp3"
)

// Player 是一首歌的播放链路：解码器 + MyGo 的播放器。
//
// 一个 Player 只服务一条播放路径——MyGo 用一个 goroutine 驱动一个解码器，
// 迷你播放器和主播放器要各开一个。
type Player struct {
	dec  *errorDecoder
	play *mygoaudio.Player
	vol  float64

	// closers 是这个播放器**自己**占着的资源，目前只有在线字节源。
	//
	// ⚠️ 它们必须由 Close **同步**关掉，不能指望解码 goroutine 醒来之后
	// 顺手关掉（`closingDecoder` 会做那件事，但那个 goroutine 此刻正阻塞在
	// 字节源的 Read 上——而只有字节源被关掉，它才会醒）。等自己关自己就是
	// 死等：切歌之后下载 goroutine 还在、缓存文件还在、那个解码 goroutine
	// 也在，永远等不到。
	closers []io.Closer
}

// Open 打开一个本地音频文件。
//
// ⚠️ **不按扩展名分流**：认容器的是 `openDecoder` 那一张注册表，和在线流用的是
// 同一个函数。曾经这里有一句「扩展名是 .flac 就走 flacdec」，那是**第二套**
// 认容器的方式，两套迟早漂移——而漂移的那一半正好是在线流（本地放得了、
// 在线放不了，或者反过来）。按头几个字节认，两条路就没有分别了：
// 裸 FLAC 认成 FLAC，MP4 里的 FLAC 认成 MP4（交给系统解码器）。
//
// 解码器持有的文件句柄挂在 Player 上，Close 会把它关掉。
func Open(path string) (*Player, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	d, _, err := openDecoder(f)
	if err != nil {
		f.Close()
		if errors.Is(err, mygoaudio.ErrFormat) {
			return nil, fmt.Errorf("%s: unsupported audio format", path)
		}
		return nil, err
	}
	// mygoaudio.NewDecoder 不负责关文件，得自己挂上，否则换歌会漏句柄。
	return New(owned(d, f)), nil
}

// New 用一个现成的解码器建播放器。解码器归播放器所有，Close 时会被关掉。
func New(d mygoaudio.Decoder) *Player {
	ed := &errorDecoder{Decoder: d}
	return &Player{dec: ed, play: mygoaudio.NewPlayer(ed), vol: 1}
}

// CloserReadSeeker 是「既是字节来源、又必须被一起关掉」的那一类东西。
//
// 在线流的 `StreamSource` 就是它。把它合成一个参数是因为这两件事**必须成对**
// 出现：只接不关的话下载 goroutine、缓存文件和临时目录里的残留全都跟着换歌
// 累积起来；只关不接的话下一首歌读的是上一首的字节源。
type CloserReadSeeker interface {
	io.ReadSeeker
	io.Closer
}

// OpenStream 打开一条**已经在流动的字节流**并建播放器。
//
// 在线流走这条：源是一个 `*StreamSource`（边下边播、可取消），它是
// `io.ReadSeeker`，所以本地文件那套解码链一个字都不用改——**用的就是
// `Open` 那同一个 `openDecoder`**。「本地文件」和「B 站 CDN 的字节」在这一层
// 没有区别，走的注册表也是同一张。
func OpenStream(r CloserReadSeeker) (*Player, error) {
	d, _, err := openDecoder(r)
	if err != nil {
		return nil, err
	}
	p := New(owned(d, r))
	// ⚠️ 见 Player.closers 那段注释：这个必须由 Close 同步做，
	// 等不到解码 goroutine 醒来（它正阻塞在 r 的 Read 上）。
	p.closers = []io.Closer{r}
	return p, nil
}

// openDecoder 是**唯一**认容器的地方：`Open`（本地文件）和 `OpenStream`
// （在线流）都调它。返回值第二项是认出来的容器名，只作记录和给测试看。
//
// 两步，顺序不能反过来：
//
//  1. MyGo 的格式表（`containers.go` 注册的 `????ftyp` / `????styp` / `\xff??`，
//     加上 MP3 / WAV / AIFF 各自包的注册副作用）。
//  2. 认不出来就试 `flacdec`——B 站的无损流是**裸 FLAC**，而 MyGo 的表是对
//     字面量做匹配的，没法给变长的 magic（`fLaC` + STREAMINFO）注册一条。
//
// 反过来的话，MP4 里的 FLAC（头几个字节是 `ftyp`）会被 flacdec 当裸 FLAC 处理。
func openDecoder(r io.ReadSeeker) (mygoaudio.Decoder, string, error) {
	d, name, err := mygoaudio.NewDecoder(r)
	if err == nil {
		return d, name, nil
	}
	if !errors.Is(err, mygoaudio.ErrFormat) {
		return nil, "", err
	}
	fd, ferr := flacdec.Open(r)
	if ferr != nil {
		// 两个都不认：报告**第一个**错。那是「走完全套格式表都没认出来」，
		// 比 flacdec 的那句「不像 FLAC」有用得多——后者会让人以为
		// 文件损坏，而它可能只是一段还没下到的字节。
		return nil, "", err
	}
	return fd, "flac", nil
}

// Err 返回这一路播放里的第一条错误，播完或只是暂停过的话是 nil。
//
// **不要用 `Playing() == false` 去推断「播完了」**：暂停、停止、关闭、
// 解码失败在那边都是 false（见 `decoder.go` 顶上那段）。
func (p *Player) Err() error { return p.dec.Err() }

// owned 把解码器和它依赖的资源绑成一个整体，关的时候一起关。
//
// closers 是解码器之外要关的东西（目前是文件），按依赖顺序排：解码器自己
// 要排在最前，因为 extaf 关 AudioToolbox 句柄的时候还要用到底下的文件。
//
// ⚠️ 解码器实现了 io.Closer 就一定要收进来。曾经只收了文件：播放照常、
// fd 数也不涨，但 extaf 的 AudioFileID / ExtAudioFileRef 一直留在 AudioToolbox
// 里，切到几百首才崩。测试见 handles_test.go。
func owned(d mygoaudio.Decoder, closers ...io.Closer) mygoaudio.Decoder {
	if c, ok := d.(io.Closer); ok {
		closers = append([]io.Closer{c}, closers...)
	}
	return &closingDecoder{Decoder: d, closers: closers}
}

// closingDecoder 让播放器关闭时把该关的全关掉。
type closingDecoder struct {
	mygoaudio.Decoder
	closers []io.Closer
}

// Close 按 owned() 排好的顺序逐个关：解码器自己先关（它关 AudioToolbox 句柄
// 时还要用到底下的文件），文件最后。
func (c *closingDecoder) Close() error {
	var errs []error
	for _, cl := range c.closers {
		if err := cl.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Play 从当前位置继续播；已经播到头的从头再来。
func (p *Player) Play() { p.play.Play() }

// Pause 暂停在原地。
func (p *Player) Pause() { p.play.Pause() }

// Playing 报告当前是不是在出声。暂停、播完、关闭后都是 false。
func (p *Player) Playing() bool { return p.play.Playing() }

// Seek 跳到从头算起的 t。位置会被夹在 [0, Duration] 里，拖到末尾是合法的。
func (p *Player) Seek(t time.Duration) { p.play.Seek(t) }

// Position 返回播到哪儿了。
func (p *Player) Position() time.Duration { return p.play.Position() }

// Duration 返回这首歌有多长；文件没写时长时是 0。
func (p *Player) Duration() time.Duration { return p.play.Duration() }

// SetVolume 设音量增益，1 是解码出来的原样，0 静音。改动会自己淡入淡出，不咔哒。
func (p *Player) SetVolume(v float64) *Player {
	p.vol = v
	p.play.SetVolume(v)
	return p
}

// Volume 返回音量增益。
func (p *Player) Volume() float64 { return p.vol }

// Close 停掉并释放解码器、字节源和文件句柄。可以重复调用。
func (p *Player) Close() {
	// 先放字节源，再停 MyGo：反过来的话，MyGo 那边的解码 goroutine 还会
	// 阻塞在 Read 上，而它退出与否取决于字节源。
	for i := len(p.closers) - 1; i >= 0; i-- {
		_ = p.closers[i].Close()
	}
	p.closers = nil
	p.play.Close()
}
