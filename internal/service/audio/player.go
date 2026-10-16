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
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/audio/flacdec"
	mygoaudio "github.com/egoist/mygo/audio"

	// MP3 由 MyGo 自带，这里只需要它的注册副作用。
	_ "github.com/egoist/mygo/audio/mp3"
)

// Player 是一首歌的播放链路：解码器 + 变速层 + MyGo 的播放器。
//
// 一个 Player 只服务一条播放路径——MyGo 用一个 goroutine 驱动一个解码器，
// 迷你播放器和主播放器要各开一个。
type Player struct {
	dec *errorDecoder
	// vs 是保调变速层，夹在解码器和 MyGo 之间（见 rate.go）。
	//
	// ⚠️ MyGo 那个 `Player.SetSpeed` **不能**用来做倍速：它是重采样，音高会跟
	// 着一块上去（1.5 倍速的人声是降了半个调的尖嗓）。所以 MyGo 播放器自己的
	// speed 永远是 1，变速全在这一层里做。
	vs   *varispeed
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
// 按扩展名分流：.flac 走自带的 flacdec（它同时吃裸 FLAC 和 MP4 里的 FLAC），
// 其余交给 MyGo 已注册的格式（WAV / MP3 / AIFF / AIFC / MP4 家族 / 裸 ADTS）。
// 解码器持有的文件句柄挂在 Player 上，Close 会把它关掉。
func Open(path string) (*Player, error) {
	if strings.EqualFold(filepath.Ext(path), ".flac") {
		d, err := flacdec.OpenFile(path)
		if err != nil {
			return nil, err
		}
		return New(d), nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	d, _, err := mygoaudio.NewDecoder(f)
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
//
// 变速层是**无条件**套上的，哪怕倍率就是 1。1 倍速下它逐样本透明
// （`rate_test.go` 的 TestUnityRateIsSampleForSample 钉着这一点），所以这里
// 没有「要不要绕开它」的第二条路——两条路迟早会漂移，而漂移的那一份正好是
// 「用户开着 1.5 倍速切回 1 倍速」这条。
func New(d mygoaudio.Decoder) *Player {
	ed := &errorDecoder{Decoder: d}
	vs := newVarispeed(ed)
	return &Player{dec: ed, vs: vs, play: mygoaudio.NewPlayer(vs), vol: 1}
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
// `io.ReadSeeker`，所以本地文件那套解码链一个字都不用改——认容器靠的是
// **头几个字节**（`containers.go` 的 `????ftyp` / `\xff??`），不是后缀。
//
// 这也顺带解决了在线流「是什么格式」的问题：B 站的无损流是裸 FLAC，
// 没有 magic 能注册进 MyGo 的格式表，所以最后再试一次 `flacdec`。
// 顺序不能反过来：MP4 里的 FLAC 头几个字节是 `ftyp`，应该走 extaf，
// 交给 flacdec 反而会把它当裸 FLAC 处理。
func OpenStream(r CloserReadSeeker) (*Player, error) {
	d, _, err := mygoaudio.NewDecoder(r)
	if err != nil {
		if !errors.Is(err, mygoaudio.ErrFormat) {
			return nil, err
		}
		// 认不出来。裸 FLAC 走 flacdec（它同时吃裸 FLAC 和 MP4 里的 FLAC）。
		fd, ferr := flacdec.Open(r)
		if ferr != nil {
			// 两个都不认：报告**第一个**错。那是「走完全套格式表都没认出来」，
			// 比 flacdec 的那句「不像 FLAC」有用得多——后者会让人以为
			// 文件损坏，而它可能只是一段还没下到的字节。
			return nil, err
		}
		d = fd
	}
	p := New(owned(d, r))
	// ⚠️ 见 Player.closers 那段注释：这个必须由 Close 同步做，
	// 等不到解码 goroutine 醒来（它正阻塞在 r 的 Read 上）。
	p.closers = []io.Closer{r}
	return p, nil
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
//
// t 是**歌里的时间**，和倍率无关：变速层报给上层的帧号就是源帧，所以这一句
// 在 0.5 倍速和 2 倍速下拖到同一处都是同一处（见 rate.go 的类型注释）。
func (p *Player) Seek(t time.Duration) { p.play.Seek(t) }

// Position 返回播到哪儿了——**歌里的时间**。
//
// 不能直接用 MyGo 的 `play.Position()`：它数的是变速层吐出去的帧，除以采样率
// 之后永远是「已经播了多少秒」，跟倍率无关（2 倍速下它会走成原速）。这里换算
// 回源帧，见 `varispeed.SourceAt`。
func (p *Player) Position() time.Duration {
	rate := p.vs.SampleRate()
	if rate <= 0 {
		return p.play.Position()
	}
	played := p.play.Position()
	frames := int64(math.Round(played.Seconds() * float64(rate)))
	return framesToTime(p.vs.SourceAt(frames), rate)
}

// framesToTime 把帧号写成时长。
func framesToTime(frames int64, rate int) time.Duration {
	return time.Duration(float64(frames) / float64(rate) * float64(time.Second))
}

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

// SetRate 设播放倍率，夹在 [MinRate, MaxRate]（0.5~2.0）。1 是原速。
//
// **音高不变**：时间轴是在变速层里拉长/压短的（WSOLA），不是重采样。
// 改的时候不用重开播放器，也不用 seek：变速层接着当前这一跳往下铺，位置不跳。
//
// 想让倍率在换歌之后还在，调用方自己存着（见 `App.SetRate`）——这一层只管
// 手上这一首。
func (p *Player) SetRate(r float64) *Player {
	p.vs.setRate(r)
	return p
}

// Rate 返回当前倍率。
func (p *Player) Rate() float64 { return p.vs.rate() }

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
