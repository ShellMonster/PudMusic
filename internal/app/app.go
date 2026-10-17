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
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
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
	// CoverURL 是当前这首的封面地址，交给界面的封面缓存去取。空串表示没有封面。
	//
	// ⚠️ 本地文件永远是空的，而且这是照抄 Biu 的行为，不是省事：Biu 上游也不读
	// 音频文件里内嵌的图（`electron/ipc/local-music.ts` 用 music-metadata 但只取
	// title 和 duration，result.push 里没有任何 picture 字段），所以本地音乐在
	// Biu 里也是占位。别拿文件路径去凑一个假地址——缓存拿到空串画的是空框，
	// 不是「加载失败」，这两个状态在 imgcache 的 drawCover 里是分开的两支。
	//
	// 字段本身先立好：接 B 站在线歌时那才有真封面，那时只要让队列项把它带下来，
	// 界面和缓存都不用改。
	CoverURL string
	Playing  bool
	Position time.Duration
	Duration time.Duration
	Volume   float64
	// Err 是最近一次打开文件的失败原因，显示在播放条上，直到下一次打开成功。
	Err string

	// Quality 是当前这条流的音质徽章（「无损 FLAC」/「320K」）。
	//
	// 空串 = 这一首是本地文件，没有音质这回事（本地文件是什么格式和
	// 「音质」不是一回事，不要拿文件格式凑一个）。
	//
	// **换流之后它会变**：B 站偶尔会给出比上次低的一档，用户得看见。
	Quality string

	// Buffering 是「这一首正在缓冲」。
	//
	// 网络断了或者 B 站那边一时下不动时，歌停在原地等着，网络回来接着走
	// （见 `audio.StreamSource.Waiting`）。这时候**没有错误**——所以
	// `Err` 是空串而这里是 true，两者说的不是一件事：
	//
	//	Err == ""            什么都没出错
	//	Err == "" && Buffering  正在等网络（用户该知道歌为什么停着）
	//	Err != ""            放弃了，而且有一条能照着做的话
	//
	// 本地文件永远是 false。
	Buffering bool
}

// App 是应用的全部状态。
type App struct {
	mu       sync.Mutex
	player   *audio.Player
	title    string
	path     string
	cover    string
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

	// account 是扫码登录那摊事（状态、轮询、落盘），见 account.go。
	// 挂上去之前是 nil，界面要判空。
	account *Account

	// 在线播放这一摊，见 online.go。挂上去之前 streaming 是 nil。
	streaming   *Streaming
	streamCache map[string]*bili.StreamRef
	// onlineGen 是**播放代次**。每开一次在线流就加一，异步回来的结果
	// 先对一遍，对不上就走人（快速切歌时旧曲的结果不能覆盖新曲）。
	onlineGen int
	// online 是当前正在放的那条在线流，nil 表示放的是本地文件。
	online *onlineTrack
	// quality 是 State.Quality 的存放处，跟着换流更新。
	quality string
	// buffering 是 State.Buffering 的存放处，字节源在等网络时为 true。
	//
	// 它和 `errText` 是两回事：等网络**不是出错**，播放条上的话也不该是
	// 红字（见 State.Buffering）。
	buffering bool
	// reportedErr 是已经往界面上报过的那一条。解码器的 Err 记的是**第一条**
	// 错误、不自己清零，所以不比对的话每 50 ms 会重复报一次。
	reportedErr error

	wake   func() // 界面层装：状态写完后叫它要一帧
	done   chan struct{}
	closed bool
}

// New 建一个空的应用状态，开后台时钟。
func New() *App {
	a := &App{volume: 1, queue: NewQueue(), done: make(chan struct{}),
		streamCache: map[string]*bili.StreamRef{}}
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
	acct := a.account
	a.mu.Unlock()
	// 挂登录早于装窗口（挂登录要读盘，装窗口要建窗口），所以挂登录时
	// 这里还是 nil。顺手补一次，否则后台改了登录状态没有一帧会被叫醒，
	// 界面会一直停在旧提示上。
	if acct != nil {
		acct.setWake(fn)
	}
}

// Snapshot 返回当前状态的拷贝。
func (a *App) Snapshot() State {
	a.mu.Lock()
	defer a.mu.Unlock()
	return State{
		HasTrack:  a.player != nil,
		Title:     a.title,
		Path:      a.path,
		CoverURL:  a.cover,
		Playing:   a.playing,
		Position:  a.position,
		Duration:  a.duration,
		Volume:    a.volume,
		Err:       a.errText,
		Quality:   a.quality,
		Buffering: a.buffering,
	}
}

// setCover 记下当前这首的封面地址。
//
// 值不变就不叫醒界面：本地文件的封面是空串，每一次换歌、每一次入队都会走到
// 这里，为一个没变的值多要一帧是白要的。
func (a *App) setCover(url string) {
	a.mu.Lock()
	if a.cover == url {
		a.mu.Unlock()
		return
	}
	a.cover = url
	wake := a.wake
	a.mu.Unlock()
	if wake != nil {
		wake()
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
	a.online = nil
	a.quality = ""
	a.reportedErr = nil
	a.buffering = false
	a.title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	a.path = path
	// 手上只有一个本地路径，没有封面地址（见 State.CoverURL）。队列里的项自带
	// 封面的话，App.play 会在开成功之后补上。
	a.cover = ""
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
	a.buffering = false
	a.mu.Unlock()

	close(a.done)
	if p != nil {
		// Player.Close 会把字节源一起关掉（在线流那条路）。
		p.Close()
	}
	a.mu.Lock()
	a.online = nil
	a.mu.Unlock()
	if a.account != nil {
		a.account.Close()
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
			// ⚠️ 这一句**不能省**。`Playing() == false` 有五种情况：暂停、
			// 停止、关闭、播完、解码失败（`mygo/audio/audio.go`）。前四种
			// 什么都不用做，第五种必须变成播放条上的一句话——不然一次断网
			// 表现成「歌忽然放完了」，用户以为歌放完了。
			//
			// 放在锁外面调：它自己要拿 `a.mu`，而 `Player.Err()` 可能去碰
			// 字节源。
			a.checkPlayback()
		}
	}
}
