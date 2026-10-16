// Package app 是业务状态层：全部状态挂在 App 上，界面层读它、用户操作写它。
//
// 它不认识界面（不 import mygo/ui），也不碰网络。打开文件、播放、跳位置这些
// 活儿都交给服务层；这一层只管「现在在放什么、放到哪儿了、要不要继续」。
package app

import (
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/audio"
)

// tickInterval 是进度条时钟的间隔。每秒 20 次足够让进度条看起来是滑的，
// 又不至于把 CPU 烧在重绘上（Invalidate 会合并，一帧里叫多少次都只画一次）。
const tickInterval = 50 * time.Millisecond

// State 是一帧里界面要读的全部状态。
//
// view 每帧开头调一次 Snapshot，拿到的是一份自洽的拷贝：它不会读到「歌名已经
// 换了但时长还是旧的」这种写了一半的状态。
type State struct {
	HasTrack bool
	Title    string
	Path     string
	Playing  bool
	Position time.Duration
	Duration time.Duration
	Volume   float64
	// Err 是最近一次打开文件的失败原因，显示在播放条上，直到下一次打开成功。
	Err string
}

// App 是应用的全部状态。
type App struct {
	mu       sync.Mutex
	player   *audio.Player
	title    string
	path     string
	position time.Duration
	duration time.Duration
	volume   float64
	// wantPlay 是用户的意图（按过播放），playing 是播放器此刻的实际状态。
	// 两者分开是因为一首歌播完之后 wantPlay 还是 true，而 playing 变回 false；
	// 进度条拖回中间时靠 wantPlay 才能接着播。
	wantPlay bool
	playing  bool
	errText  string

	queue *Queue // 播放队列，见 queue.go

	wake   func() // 界面层装：状态写完后叫它要一帧
	done   chan struct{}
	closed bool
}

// New 建一个空的应用状态，开后台时钟。
func New() *App {
	a := &App{volume: 1, queue: NewQueue(), done: make(chan struct{})}
	go a.tick()
	return a
}

// SetWake 装上「状态变了，要一帧」的回调，由界面层装一次。
//
// 为什么需要它：MyGo 原生 UI 是即时模式的，后台 goroutine 改了状态之后，
// 界面不会自己重画——不叫醒它，界面就永远停在旧帧（实测：只投递不唤醒 = +0 帧）。
//
// ⚠️ 这个回调必须是合并安全的（ui.Services.Invalidate 就是），并且能跨 goroutine 调用。
func (a *App) SetWake(fn func()) {
	a.mu.Lock()
	a.wake = fn
	a.mu.Unlock()
}

// Snapshot 返回当前状态的拷贝。
func (a *App) Snapshot() State {
	a.mu.Lock()
	defer a.mu.Unlock()
	return State{
		HasTrack: a.player != nil,
		Title:    a.title,
		Path:     a.path,
		Playing:  a.playing,
		Position: a.position,
		Duration: a.duration,
		Volume:   a.volume,
		Err:      a.errText,
	}
}

// Open 打开一个本地音频文件并开始播放，上一首连同它的文件句柄一起关掉。
func (a *App) Open(path string) error {
	p, err := audio.Open(path)
	if err != nil {
		a.mu.Lock()
		a.errText = err.Error()
		wake := a.wake
		a.mu.Unlock()
		if wake != nil {
			wake()
		}
		return err
	}

	a.mu.Lock()
	old := a.player
	p.SetVolume(a.volume)
	a.player = p
	a.title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	a.path = path
	a.position = 0
	a.duration = p.Duration()
	a.wantPlay, a.playing = true, true
	a.errText = ""
	wake := a.wake
	a.mu.Unlock()

	// 先把新歌换上，再关旧的：换歌期间 tick 拿到的一直是有效的播放器。
	if old != nil {
		old.Close()
	}
	p.Play()
	if wake != nil {
		wake()
	}
	return nil
}

// Toggle 在播放和暂停之间切换。播放到头之后按它是重头播。
func (a *App) Toggle() {
	a.mu.Lock()
	p, wake := a.player, a.wake
	a.mu.Unlock()
	if p == nil {
		return
	}
	if p.Playing() {
		p.Pause()
	} else {
		p.Play()
	}

	a.mu.Lock()
	a.playing = p.Playing()
	if !a.playing {
		a.wantPlay = false
	}
	a.mu.Unlock()
	if wake != nil {
		wake()
	}
}

// SetVolume 设音量，1 是解码出来的原样，0 静音。
func (a *App) SetVolume(v float64) {
	v = min(max(v, 0), 1)
	a.mu.Lock()
	a.volume = v
	p, wake := a.player, a.wake
	a.mu.Unlock()

	if p != nil {
		p.SetVolume(v)
	}
	if wake != nil {
		wake()
	}
}

// ScrubStart 是进度条被按下的那一刻。
func (a *App) ScrubStart() {}

// ScrubEnd 是松手那一刻，跳到 fraction（0..1）对应的位置。
//
// 松手后会按用户意图决定要不要接着播：拖到末尾播放机会自然结束，往回拖必须
// 能接着播，否则会停在那儿不动。
func (a *App) ScrubEnd(fraction float64) {
	a.mu.Lock()
	p, dur := a.player, a.duration
	wantPlay := a.wantPlay
	a.mu.Unlock()
	if p == nil || dur <= 0 {
		return
	}
	t := min(time.Duration(fraction*float64(dur)), dur)
	p.Seek(t)
	if wantPlay {
		p.Play()
	}

	// 立刻把位置改过来：暂停状态下 tick 不跑，界面下一帧读的就是这里写的值。
	a.mu.Lock()
	a.position = t
	a.playing = p.Playing()
	wake := a.wake
	a.mu.Unlock()
	if wake != nil {
		wake()
	}
}

// Close 停掉播放、关掉解码器和文件句柄，并停掉后台时钟。可以重复调用。
func (a *App) Close() {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.closed = true
	p := a.player
	a.player = nil
	a.playing = false
	a.mu.Unlock()

	close(a.done)
	if p != nil {
		p.Close()
	}
}

// tick 是进度条的时钟：把播放器的当前位置读回来，界面上的进度条才会自己走。
//
// 空闲时（没有歌，或暂停）不唤醒，白刷的帧是白费的。
func (a *App) tick() {
	t := time.NewTicker(tickInterval)
	defer t.Stop()
	for {
		select {
		case <-a.done:
			return
		case <-t.C:
			a.mu.Lock()
			p := a.player
			live := a.wantPlay || a.playing
			if p != nil && live {
				a.position = p.Position()
				a.playing = p.Playing()
			}
			wake := a.wake
			a.mu.Unlock()

			if live && wake != nil {
				wake()
			}
		}
	}
}
