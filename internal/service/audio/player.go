// Package audio decodes and plays the audio files Biu opens from disk.
//
// 依赖方向：这一层可以 import platform，但不能被 platform import；它不认识界面，
// 也不知道谁在调用，只负责「给一个路径，拿到一个能播、能停、能跳、能关的播放器」。
package audio

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	dec  mygoaudio.Decoder
	play *mygoaudio.Player
	vol  float64
}

// Open 打开一个本地音频文件。
//
// 按扩展名分流：.flac 走自带的 flacdec（它同时吃裸 FLAC 和 MP4 里的 FLAC），
// 其余交给 MyGo 已注册的格式（WAV / MP3 / AIFF / AIFC）。解码器持有的文件句柄
// 挂在 Player 上，Close 会把它关掉。
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
	return New(closingDecoder{Decoder: d, Closer: f}), nil
}

// New 用一个现成的解码器建播放器。解码器归播放器所有，Close 时会被关掉。
func New(d mygoaudio.Decoder) *Player {
	return &Player{dec: d, play: mygoaudio.NewPlayer(d), vol: 1}
}

// closingDecoder 让播放器关闭时连着关掉解码器底下的文件。
type closingDecoder struct {
	mygoaudio.Decoder
	io.Closer
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

// Close 停掉并释放解码器和文件句柄。可以重复调用。
func (p *Player) Close() { p.play.Close() }
