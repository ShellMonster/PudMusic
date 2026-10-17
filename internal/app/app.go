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

	"github.com/ShellMonster/Biu_Go/internal/media"
	"github.com/ShellMonster/Biu_Go/internal/service/audio"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/service/download"
)

// tickInterval 是进度条时钟的间隔。每秒 20 次足够让进度条看起来是滑的，
// 又不至于把 CPU 烧在重绘上（Invalidate 会合并，一帧里叫多少次都只画一次）。
const tickInterval = 50 * time.Millisecond

// RateSteps 是倍率的档位（0.5 / 0.75 / 1 / 1.25 / 1.5 / 1.75 / 2）。
//
// 这是音频层那份的原样转发，界面层只认 app 这一份：界面画的每一行档位都必须
// 和 `SetRate` 认得的档位是同一份，两边各抄一份迟早会漂。
var RateSteps = audio.RateSteps

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
	// Rate 是当前的播放倍率（0.5~2.0，1 是原速）。
	//
	// 它是**用户的一个设置**，不是某一首歌的属性：换歌之后还在，切歌那一瞬间
	// 也不该把它按成 1。所以它每帧都从 App 现读，界面上的按钮也一样。
	Rate float64
	// Err 是最近一次打开文件的失败原因，显示在播放条上，直到下一次打开成功。
	Err string

	// Quality 是当前这条流的音质徽章（「无损 FLAC」/「320K」）。
	//
	// 空串 = 这一首是本地文件，没有音质这回事（本地文件是什么格式和
	// 「音质」不是一回事，不要拿文件格式凑一个）。
	//
	// **换流之后它会变**：B 站偶尔会给出比上次低的一档，用户得看见。
	Quality string

	// QualityPref 是用户**选的那一档**（`AudioQuality` 那组字符串）。
	//
	// 和 Quality 分开是因为它们是两件事：选的是「我要无损」，徽章说的是
	// 「B 站给了无损」。降级发生之后界面上要同时出现两者——混成一句就变成
	// 「用户要无损，系统给了无损」，看不出发生了降级。
	//
	// ⚠️ 它是**偏好**，本地文件播放时也照样有值：徽章只对在线流画，
	// 而换档这个动作不该要求当前正好在放在线流。
	QualityPref AudioQuality
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
	// rate 是播放倍率，和 volume 一样是**用户的选择**，换歌之后还在。
	// 它必须由这一层记着：播放器只管手上这一首（`audio.Player.SetRate`），
	// 而新歌是后开的播放器，得有人告诉它「用户要的是几倍」。
	rate float64
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
	// reportedErr 是已经往界面上报过的那一条。解码器的 Err 记的是**第一条**
	// 错误、不自己清零，所以不比对的话每 50 ms 会重复报一次。
	reportedErr error

	// 本地音乐库，见 localmusic.go。挂上去之前是 nil，界面要判空。
	localMusic *localLibrary

	// 下载这一摊，见 downloads.go。挂上去之前是 nil，界面要判空。
	downloads *download.Manager
	// downloadQueueFile 是队列落盘的位置。退出时写、重开时读。
	downloadQueueFile string
	// downloadFallbackDir 是设置里没选过下载目录时的去处（系统下载目录）。
	downloadFallbackDir string

	// updates 是自动更新那一摊，见 update.go。挂上去之前是 nil，界面要判空。
	//
	// ⚠️ 它和 downloads/account/localMusic 一样是**自己的锁**：后台那个下载
	// goroutine 一边下一边写，而界面每帧要读它，混进 a.mu 会把界面那一帧的
	// 等待时间变成「网络那一趟」。
	updates *updater

	// artist 是当前这首的艺人（在线条目的 UP 主名）。本地文件是空的，
	// 见 media.go 的 setArtist。跟 cover 一样只有媒体会话在用。
	artist string
	// stopped 是「用户按了停止」：和暂停分开，因为它让进度条回到开头，
	// 而系统那边也是两个不同的状态（见 media.go 的 Stop）。
	stopped bool

	// mediaSink 是「该跟系统媒体会话说一声了」的那只手，由界面层装一次。
	// nil = 没装（这一层不参与，Publish 是空操作）。
	mediaSink func(media.NowPlaying)
	// mediaAt 是上一次跟系统说话的��刻。用来限住「多久校正一次进度」。
	mediaAt time.Time

	wake   func() // 界面层装：状态写完后叫它要一帧
	done   chan struct{}
	closed bool
}

// New 建一个空的应用状态，开后台时钟。
func New() *App {
	a := &App{volume: 1, rate: audio.DefaultRate, queue: NewQueue(), done: make(chan struct{}),
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
	dl := a.downloads
	a.mu.Unlock()
	// 挂登录早于装窗口（挂登录要读盘，装窗口要建窗口），所以挂登录时
	// 这里还是 nil。顺手补一次，否则后台改了登录状态没有一帧会被叫醒，
	// 界面会一直停在旧提示上。下载那一摊同理（`MountDownloads` 也读盘）。
	if acct != nil {
		acct.setWake(fn)
	}
	if dl != nil {
		dl.SetWake(fn)
	}
}

// Snapshot 返回当前状态的拷贝。
func (a *App) Snapshot() State {
	a.mu.Lock()
	defer a.mu.Unlock()
	// ⚠️ 偏好直接从 `a.streaming` 读，**不能**调 `a.AudioQuality()`：那把锁
	// 已经在手里了，sync.Mutex 不可重入，那样写是当场死锁。
	pref := AudioQualityAuto
	if a.streaming != nil {
		pref = BiliToPref(a.streaming.Quality)
	}
	return State{
		HasTrack:    a.player != nil,
		Title:       a.title,
		Path:        a.path,
		CoverURL:    a.cover,
		Playing:     a.playing,
		Position:    a.position,
		Duration:    a.duration,
		Volume:      a.volume,
		Rate:        a.rate,
		Err:         a.errText,
		Quality:     a.quality,
		QualityPref: pref,
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
	// 倍率和音量一样是「用户选的那个值」，换歌之后照旧——见 App.rate 那段。
	p.SetVolume(a.volume).SetRate(a.rate)
	a.player = p
	a.online = nil
	a.quality = ""
	a.reportedErr = nil
	a.title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	a.path = path
	// 手上只有一个本地路径，没有封面地址（见 State.CoverURL）。队列里的项自带
	// 封面的话，App.play 会在开成功之后补上。
	a.cover = ""
	// 本地文件没有艺人（Biu 上游也不读内嵌标签，见 media.go 的 setArtist）。
	// 队列里的项自带的话，App.play 会在开成功之后补上。
	a.artist = ""
	a.position = 0
	a.duration = p.Duration()
	a.wantPlay, a.playing = true, true
	a.stopped = false
	a.errText = ""
	wake := a.wake
	a.mu.Unlock()

	// 先把新歌换上，再关旧的：换歌期间 tick 拿到的一直是有效的播放器。
	if old != nil {
		old.Close()
	}
	p.Play()
	a.publishMedia()
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
	// 按播放把一首停住的歌放起来时，「停过」这件事就过去了。
	if a.playing {
		a.stopped = false
	}
	a.mu.Unlock()
	// 锁屏上的播放/暂停按钮是跟着状态走的，这一句在 App/media.go 的说明里。
	a.publishMedia()
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

// SetRate 设播放倍率，夹在 [audio.MinRate, audio.MaxRate]（0.5~2.0）。1 是原速。
//
// 音高**不变**：时间轴是在音频层拉长/压短的（WSOLA，见
// `internal/service/audio/rate.go`），不是重采样。
//
// 这一层记着它，是因为它是**用户的一个选择**而不是某一首歌的属性：换歌、
// 换流重开之后都得照旧。没歌的时候也照收——用户先按了 2 倍速再点歌，
// 那首歌就应该从第一秒起就是 2 倍速。
func (a *App) SetRate(r float64) {
	r = audio.SnapRate(r)
	a.mu.Lock()
	a.rate = r
	p, wake := a.player, a.wake
	a.mu.Unlock()

	if p != nil {
		p.SetRate(r)
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
	// 用户动过时间线了，「停到开头」这件事就过去��。系统那边跟着回到
	// playing / paused——它看到的进度条此刻在 t 上，状态却说「停在开头」，
	// 那是两句互相打架的话。
	a.stopped = false
	wake := a.wake
	a.mu.Unlock()
	// 锁屏上那根进度条和界面上这根是同一根：不报的话它在原地漂着，
	// 直到下一次校正（mediaPublishEvery）才跟上。
	a.publishMedia()
	if wake != nil {
		wake()
	}
}

// ClearTrack 停掉播放并把当前曲目清空，**队列不动**。没有在放的时候什么都不做。
//
// 「清空播放列表」和「把正在放的那一首删掉」最后都落到这里。停了还留着状态是
// 不行的：那之后播放条上还是那首歌的名字，抽屉里已经没有它了——用户看到的界面
// 和它说的事实对不上，而且再按「播放」会试图重开一个用户已经删掉的文件。
//
// ⚠️ **和 `Stop` 不是一件事，别把两个混起来**（`media.go` 里的那个）：`Stop` 是
// 锁屏和控制中心上那个方块键——暂停并回到开头，这首歌还在、下一句还能接着听；
// 这里是「这一首到此为止，而且界面上不许再留着它」。
//
// ⚠️ **不停后台时钟**。clock 是 Close 的事（进程要退了才停）；这里停了还要能再
// 放下一首。
func (a *App) ClearTrack() {
	a.mu.Lock()
	p := a.player
	a.player = nil
	a.playing = false
	// 「停到开头」这件事也就过去了：没有播放器就是「什么都没有」，不是「停在开头」
	// （mediaSnapshotLocked 那边 player 为 nil 时回的就是 StateNone）。
	a.stopped = false
	// ⚠️ **代次要加**：在途的在线流回来时只对一遍代次（`online.go` 的 runOnline
	// / failOnline / noteQuality 都对）。不加的话，用户把正在放的那首删掉之后
	// 那条流才建好——它照样会装上来，队列是空的、播放器却在放一首不在队列里的歌，
	// 正是这里要避免的那件事。
	a.onlineGen++
	a.online = nil
	a.quality = ""
	a.reportedErr = nil
	a.title, a.path, a.cover = "", "", ""
	a.artist = ""
	a.position, a.duration = 0, 0
	a.wantPlay = false
	a.errText = ""
	wake := a.wake
	a.mu.Unlock()

	// 先把状态摘干净，再关解码器：和 Open 换歌同一个道理，关的过程中 tick 拿到
	// 的不会再是这一个。
	if p != nil {
		p.Close()
	}
	// 锁屏上那一首已经没有了，跟系统说一声（media.go 的那只手；没装就是空操作）。
	a.publishMedia()
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
	dl, dlPath := a.downloads, a.downloadQueueFile
	lib := a.localMusic
	upd := a.updates
	a.mu.Unlock()

	close(a.done)
	if p != nil {
		// Player.Close 会把字节源一起关掉（在线流那条路）。
		p.Close()
	}
	a.mu.Lock()
	a.online = nil
	a.mu.Unlock()
	// 告诉系统「没有歌了」。不做这一步的话，退出去之后锁屏上还会留着
	// 一首已经不存在的歌，而控制中心的播放键按下去还能收到命令。
	a.publishMedia()
	if a.account != nil {
		a.account.Close()
	}
	// 下载要**停掉并落盘**。停掉是为了别让在飞的请求在应用退出时还在往盘上
	// 写；落盘是因为队列文件是「下到哪儿了」的唯一一份，丢了用户重启就得
	// 从头下。写失败不往上报错：退出路径上没有用户在看。
	if dl != nil {
		_ = dl.Close(dlPath)
	}
	// 本地音乐的文件监听也要关：它每个根一个 goroutine，不关的话退出之后
	// 还在那儿收着目录事件。
	if lib != nil {
		lib.close()
	}
	// 更新这一摊也要收尾，而且**排在最后**：它要取消在下的那一次下载并等它真的
	// 停了（SPEC 118「下载中退出应用，状态一致不损坏」）。等的时候不能有别的
	// goroutine 还在往界面上投递状态——界面已经拆了。
	if upd != nil {
		upd.close()
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
			now := time.Now()
			a.mu.Lock()
			p := a.player
			live := a.wantPlay || a.playing
			wake := a.wake
			a.mu.Unlock()

			// ⚠️ `p.Position()` 必须在 `a.mu` **外面**调，理由和下面那句
			// `checkPlayback` 是同一条（它自己要拿 `a.mu`）：
			//
			// 它要过变速层那把锁，而那一把锁可能被一次**网络读**持着——在线流
			// 正在从 CDN 下这一段，CDN 卡一下锁就卡一下。而界面每一帧的第一件
			// 事就是 `Snapshot()`（要 `a.mu`），于是 CDN 卡 2 秒，整个窗口就跟着
			// 卡 2 秒；`StreamSource` 的卡死判定是 20 秒（`stream_source.go` 的
			// `defaultStallTimeout`），最坏就是**窗口卡 20 秒**。
			//
			// 变速层那一边已经改成读内层时不持锁了（`rate.go` 的 `readInner`），
			// 这一句是把最后那一层放大也去掉：即便它还要等，也等的是自己一个
			// goroutine 的事，不占着界面每帧都要的那把锁。
			var pos time.Duration
			var playing bool
			if p != nil && live {
				pos, playing = p.Position(), p.Playing()
			}

			a.mu.Lock()
			// 位置和 playing 是**这一台播放器**的：这中间换歌了（换歌在锁里把
			// `a.player` 换成新的），就不能把旧那台的位置写到新歌头上。
			if p != nil && live && a.player == p {
				a.position, a.playing = pos, playing
			}
			// 锁屏上的进度条是**系统自己外推**出来的（Apple 头文件原话），
			// 而外推的是它的时间：我们这边卡一下解码、换一次流、音频设备
			// 被抢占，它都不知道。所以每隔几秒把真位置报回去校正一次。
			//
			// ⚠️ 只在**在放**的时候校正：暂停时系统不外推，而位置一变
			// publishMedia 那一次就已经报过了。
			publish := a.mediaSink != nil && a.playing && now.Sub(a.mediaAt) >= mediaPublishEvery
			if publish {
				a.mediaAt = now
			}
			a.mu.Unlock()

			if publish {
				a.publishMedia()
			}
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
