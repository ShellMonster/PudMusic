//go:build darwin

package media

// macOS 那一半：`MPNowPlayingInfoCenter` 写曲目信息，
// `MPRemoteCommandCenter` 接播放命令。两个类都在 MediaPlayer.framework 里。
//
// # 只有一条接收路径，这是刻意的
//
// 耳机线控和控制中心的按钮在 macOS 上走的是同一条路（`MPRemoteCommandCenter`），
// 而键盘上的媒体花键（F7/F8 那一排）**可能**还有另一条路——系统把媒体键
// 编成一个 `NSEventTypeSystemDefined` 事件直接塞给应用，于是想接它就得
// swizzle `-[NSApplication sendEvent:]`。
//
// 本票**不接那条路**。理由不是「接不了」，是「接了会按两次」：两条路都把
// 同一次按键变成一次「下一首」时，用户按一下跳两首，而且界面上看不出任何
// 异样。既然只用一条，就不存在「一次按键切两首」这件事——代价是**键盘上的
// 媒体花键可能收不到**，而它收不收得到在这台机器上无法实测（见任务报告）。
// 头键和控制中心不受影响，那两条本来就只走 `MPRemoteCommandCenter`。

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// nowPlayingInfo 的键。
//
// ⚠️ **这些 key 一半是长名、一半是短名**，Apple 把它们全声明成
// `NSString * const`，头文件里看不到值，而**写错了系统静默忽略、不报错**：
// 调 `setNowPlayingInfo:` 返回得很干脆，锁屏上就是没有那一栏。
//
// 这里写的这一套**不是照着头文件推的**：在 dyld 共享缓存里把 MediaPlayer 的
// 镜像扒了一遍，这八个字符串（`title` / `artist` / `artwork` /
// `playbackDuration` / `albumTitle` 加上那几个 `MPNowPlayingInfoProperty*`
// 长名）**都在那个镜像里**，而那一长串短名在任何别的框架里都不这么成组出现。
// 这是「名字对」的证据，**不是**「控制中心真的画出来了」的证据——那一条在
// 这台机器上验不了（终端没有录屏权限，看不见控制中心），见任务报告。
const (
	keyTitle     = "title"
	keyArtist    = "artist"
	keyArtwork   = "artwork"
	keyDuration  = "playbackDuration"
	keyElapsed   = "MPNowPlayingInfoPropertyElapsedPlaybackTime"
	keyRate      = "MPNowPlayingInfoPropertyPlaybackRate"
	keyDefRate   = "MPNowPlayingInfoPropertyDefaultPlaybackRate"
	keyMediaType = "MPNowPlayingInfoPropertyMediaType"
)

// mediaTypeAudio 是 `MPNowPlayingInfoPropertyMediaType` 的值。
//
// ⚠️ **它是 `MPNowPlayingInfoMediaType` 这个枚举，要装进 NSNumber**，不是字符串
// "audio"。头文件里这个属性声明的是 `MPNowPlayingInfoMediaType`（一个
// NS_ENUM），所以给它一个 NSString，系统认不出来——而它认不出来的表现是
// **静默忽略**：歌名、进度、封面全照常显示，只是这一栏不起作用。
//
// `MPNowPlayingInfoMediaTypeAudio` = 1（None = 0、Video = 2）。
const mediaTypeAudio = 1

// warnPushOffThread 保证「写入跑错线程」只说一次，别把日志刷满。
var warnPushOffThread sync.Once

// mpSession 是 macOS 那一半的实现。
type mpSession struct {
	// center 是 `MPNowPlayingInfoCenter.defaultCenter`。
	center id

	// fetch 是取封面的那只手（见 Options.Fetch）。
	fetch func(string) ([]byte, error)

	// mu 护住下面这几个：apply 会被状态变化、命令回调、封面下好三条路同时调到。
	mu sync.Mutex
	// last 是最近一次 apply 进来的快照。封面下好之后要照它再发一遍。
	last NowPlaying
	// artwork 是现在给系统的那张封面（MPMediaItemArtwork 对象）。
	artwork id
	// artworkURL 是 artwork 对应的地址；跟 last.ArtworkURL 不一样时说明
	// 这张封面还没下好。
	artworkURL string
	// fetching 是正在下的那个地址。非空表示有一个下载在飞。
	fetching string

	// attached 是已经装上的命令处理器（用来判断要不要重复装）。
	attached bool
	targets  []target
}

func init() {
	// ⚠️ 自己先叫一次 ensureReady：Go 在同一个包里按**文件名**排 init，
	// 而这一份排在 objc_darwin.go 前面。指望字母序是靠不住的。
	ensureReady()
	if loadErr != nil {
		return
	}
	initArtworkClass()
	initCommandTarget()
}

// newPlatform 装 macOS 那一半。系统那边缺零件时返回错误，
// 调用方（Session）把它记一条日志继续走——媒体会话是加分项，
// 它没了不该拦住应用开窗。
func newPlatform(opts Options) (platform, error) {
	ensureReady()
	if loadErr != nil {
		return nil, loadErr
	}
	if class("MPNowPlayingInfoCenter") == 0 {
		return nil, fmt.Errorf("media: 系统里没有 MPNowPlayingInfoCenter")
	}
	if class("MPRemoteCommandCenter") == 0 {
		return nil, fmt.Errorf("media: 系统里没有 MPRemoteCommandCenter")
	}
	if commandTargetClass == 0 {
		return nil, fmt.Errorf("media: 远程命令的处理器注册不上")
	}
	fetch := opts.Fetch
	if fetch == nil {
		fetch = defaultFetch
	}
	return &mpSession{
		// `MPNowPlayingInfoCenter` 标了 MP_INIT_UNAVAILABLE，只能取
		// 单例，不能 alloc/init（那样抛异常）。
		center: send(id(class("MPNowPlayingInfoCenter")), "defaultCenter"),
		fetch:  fetch,
	}, nil
}

// apply 把一份快照写给系统。
//
// ⚠️ `nowPlayingInfo` 是**替换**语义不是合并：每次都要写全量，
// 只写改了的那个字段的话上一次的内容全丢。
func (p *mpSession) apply(n NowPlaying) {
	// 往系统写的那一段排到那条钉住的 Objective-C 线程上（调用方可能是任意
	// goroutine，而 Objective-C 有每线程的状态）。见 objcthread_darwin.go。
	onObjCThread(func() { p.applyOnObjCThread(n) })
}

// applyOnObjCThread 是 apply 的 ObjC 那一半。**只能在钉住的那条线程上跑。**
func (p *mpSession) applyOnObjCThread(n NowPlaying) {
	p.mu.Lock()
	p.last = n
	p.startFetchLocked(n)
	art := p.artwork
	p.mu.Unlock()

	p.push(n, art)
}

// push 是真正往系统写字的那一步。
//
// ⚠️ 这里造出来的每个对象都是自己持有的，所以**每一次 `set` 之后都要 release**。
// 顺序也有讲究：`setNowPlayingInfo:` 会 retain 住字典和它的内容，所以放掉我们这一份
// 之后，系统手上那份还活着——这正是我们想要的（换歌时它要继续显示旧的那张封面）。
func (p *mpSession) push(n NowPlaying, art id) {
	// 🔴 **闸门**：不在钉住的那条线程上就**拒写**。
	//
	// 原因很具体：一次 push 要发两条系统消息（`setNowPlayingInfo:` 之后
	// `setPlaybackState:`）。别的写插在这两条之间，就会留下「元数据说暂停、
	// 状态说在放」这种对不上的组合——而它**不会自己好回来**，因为校正只在
	// 播放中跑。
	//
	// 所以这里宁可这一次什么也不写（下一��� publish 自然会补上），也不要写坏。
	if !onObjCThreadNow() {
		warnPushOffThread.Do(func() {
			log.Printf("biu: 系统媒体会话有一次写入不在它自己那条线程上，已拒掉（" +
				"否则会出现「进度不动但按钮显示在放」）。这是本票的 bug，请记下来。")
		})
		return
	}

	if n.State == StateNone {
		// 清空用 `setNowPlayingInfo: nil`。**没有** `clearNowPlayingInfo`
		// 这个方法，调它会 unrecognized selector（见
		// `docs/research/GO-MEDIA-SESSION.md` §2.6）。
		sendSel(p.center, sel("setNowPlayingInfo:"))
		sendSel(p.center, sel("setPlaybackState:"), uintptr(playbackStateNumber(StateStopped)))
		return
	}

	info := newMutableDictionary()
	if info == 0 {
		return
	}
	// 字典建不起来就别往下写了：现在发出去的是一份空的，
	// 锁屏上会变成「有歌名没进度」那种半吊子状态，比什么都不发更让人困惑。
	putString(info, keyTitle, n.Title)
	putString(info, keyArtist, n.Artist)
	putNumber(info, keyDuration, seconds(n.Duration))
	putNumber(info, keyElapsed, seconds(position(n.Position, n.Duration)))
	// 播放速率是**必设的**：系统不看我们的音频会话，只看这个数，
	// 而它同时决定进度条要不要自己往前走。
	putNumber(info, keyRate, rateOf(n.State))
	putNumber(info, keyDefRate, 1)
	putInt(info, keyMediaType, mediaTypeAudio)
	if art != 0 {
		dictPut(info, keyArtwork, art)
	}

	sendSel(p.center, sel("setNowPlayingInfo:"), uintptr(info))

	// 🔴 `setPlaybackState:` 是**必设项**，不是可有可无的补充。
	// Apple 头文件原话：macOS 上 "playback state cannot be determined by
	// the application's audio session"，不设的话控制中心的状态会错、
	// 远程控制可能整个失灵。
	sendSel(p.center, sel("setPlaybackState:"), uintptr(playbackStateNumber(n.State)))

	release(info)
}

// putString 往字典里放一个字符串值，放完把自己那一份释放掉。
func putString(dict id, key, value string) {
	v := newString(value)
	if v == 0 {
		return
	}
	defer release(v)
	dictPut(dict, key, v)
}

// putInt 往字典里放一个整数盒子，放完释放。
func putInt(dict id, key string, value int) {
	v := newNumberInt(value)
	if v == 0 {
		return
	}
	defer release(v)
	dictPut(dict, key, v)
}

// putNumber 往字典里放一个数字值，放完释放。
func putNumber(dict id, key string, value float64) {
	v := newNumberDouble(value)
	if v == 0 {
		return
	}
	defer release(v)
	dictPut(dict, key, v)
}

// seconds 把时长换成系统要的那个单位（秒，浮点）。
func seconds(d time.Duration) float64 {
	return float64(d) / float64(time.Second)
}

// position 把播放位置夹进 [0, 时长] 再给系统。
//
// 夹的理由：拖到末尾之后位置可能正好等于时长，而解码器那一刻的位置可能
// 比时长大一点（多解了最后几帧）。给系统一个大于时长的进度，进度条的
// 最后一格会空出来。
func position(p, dur time.Duration) time.Duration {
	if p < 0 {
		return 0
	}
	if dur > 0 && p > dur {
		return dur
	}
	return p
}

// rateOf 是这个状态下系统该看到的播放速率。
//
// 1 = 在放，0 = 不在放。**不要**乘音量：音量是我们自己的事，
// 而速率在系统那里是「进度会不会自己走」的开关。
func rateOf(s State) float64 {
	if s == StatePlaying {
		return 1
	}
	return 0
}

// ---- 封面 ----

// startFetchLocked 看要不要为这一首取封面。必须在持锁时调。
//
// ⚠️ **下载不在这里做**：网络往返不该挡在「切歌」这条路中间。
// 切歌之后锁屏上的标题和进度立刻就位，封面几百毫秒之后自己补上去
// （补的时候要再 push 一次，见 onArtworkReady）。
func (p *mpSession) startFetchLocked(n NowPlaying) {
	url := n.ArtworkURL
	switch {
	case url == "":
		// 没有封面。清掉手上那张，否则换了首没封面的歌之后锁屏上还是
		// 上一首的图——那比空着糟得多。
		p.artwork = 0
		p.artworkURL = ""
		p.fetching = ""
	case url == p.artworkURL:
		// 已经有了，或者正在下。
	case url == p.fetching:
		// 正在下，别再来一遍。
	default:
		p.artwork = 0
		p.artworkURL = ""
		p.fetching = url
		go p.fetchArtwork(url)
	}
}

// fetchArtwork 下好一张封面之后把它装上，再往系统补推一次。
func (p *mpSession) fetchArtwork(url string) {
	b, err := p.fetch(url)
	if err != nil {
		p.mu.Lock()
		if p.fetching == url {
			p.fetching = ""
		}
		p.mu.Unlock()
		// 一张封面取不到不是错误状态，只是这一首在锁屏上没有图。
		// 记一条就完事——用户看得到歌名和进度。
		log.Printf("biu: 锁屏上的封面没取到（%s）：%v", url, err)
		return
	}

	var art id
	onObjCThread(func() { art = newArtworkFromBytes(b) })
	if art == 0 {
		p.mu.Lock()
		if p.fetching == url {
			p.fetching = ""
		}
		p.mu.Unlock()
		log.Printf("biu: 锁屏上的封面取到了但解不开（%s，%d 字节）", url, len(b))
		return
	}

	p.mu.Lock()
	// 期间用户可能已经切走、或者切到同一首去了。对不上就这张图白下了，
	// 直接丢掉——留着的话会出现「下一首的进度配上一首的图」。
	if p.fetching != url || p.last.ArtworkURL != url {
		p.mu.Unlock()
		forgetImage(art)
		return
	}
	old := p.artwork
	p.artwork, p.artworkURL, p.fetching = art, url, ""
	n := p.last
	p.mu.Unlock()

	if old != 0 {
		forgetImage(old)
	}
	// ⚠️ 这一步**必须**排在钉住的那条线程上，和 apply 走的是同一条路。
	//
	// 少了这一步，封面补推就变成**下载协程在写系统**，而 apply（暂停、停止、
	// 清空）是另一条路在写。`push` 一次要发两条消息（`setNowPlayingInfo:` 之后
	// `setPlaybackState:`），两次 push 一交错就出这种结果：
	//
	//	封面补推 写「在放」的元数据 →
	//	暂停    写「暂停」的元数据和状态 →
	//	封面补推 写「在放」的状态
	//
	// 最后 rate=0（进度条不动）而 playbackState=playing（按钮显示在放）。
	// 而暂停之后**没有定期校正**（校正只在播放中跑），所以它不会自己好回来。
	onObjCThread(func() { p.push(n, art) })
}

// ---- 远程命令：往系统上装、摘下来 ----

// target 是「一个命令 + 装上去时拿到的那个句柄」。
//
// 句柄是 `addTarget:action:` 的返回值，摘掉的时候要**原样**传回去，
// 所以不能只按命令对象去找——同一个命令可以装好几个处理器。
type target struct {
	cmd   id
	token id
}

// attach 装上远程命令的处理器。可以重复调，只有第一次真的往系统上装。
func (p *mpSession) attach(fn func(Command, time.Duration)) {
	p.mu.Lock()
	already := p.attached
	p.attached = true
	p.mu.Unlock()

	setCommandHandler(fn)
	if already {
		return
	}
	onObjCThread(func() { p.addTargetsOnObjCThread() })
}

// addTargetsOnObjCThread 是 attach 的 ObjC 那一半。**只能在钉住的那条线程上跑。**
func (p *mpSession) addTargetsOnObjCThread() {

	// ⚠️ 不接的那些命令**默认是开着的**，控制中心会摆出按了没反应的按钮。
	// 所以每次 attach 都顺手把它们关一遍。
	for _, name := range commandsOff {
		if cmd := commandIfPresent(name); cmd != 0 {
			sendBool(cmd, "setEnabled:", 0)
		}
	}

	var added []target
	for _, t := range commandTargets {
		cmd := commandIfPresent(t.selector)
		if cmd == 0 {
			log.Printf("biu: 命令中心上没有 %s，控制中心上那一个键按了不会有用", t.selector)
			continue
		}
		// `addTarget:action:` **不 retain target**，所以那个 target 实例
		// 是整个进程生命周期里都活着的那一个（见 command_darwin.go）。
		tk := send(cmd, "addTarget:action:", uintptr(commandTarget()), uintptr(sel(commandSelector)))
		added = append(added, target{cmd: cmd, token: tk})
	}
	p.mu.Lock()
	p.targets = added
	p.mu.Unlock()
}

// detach 摘掉处理器。
//
// ⚠️ 这里**不**清系统里那份曲目信息：那是 `Session.Clear` 的活儿，由它
// 显式地调一次 `apply(没有歌)`。detach 只管把自己装的那些拆干净——
// 「拆干净」和「把界面状态告诉系统」是两件事，混在一起的话将来一定会
// 有一处忘了发。
func (p *mpSession) detach() {
	p.mu.Lock()
	if !p.attached {
		p.mu.Unlock()
		return
	}
	p.attached = false
	added := p.targets
	p.targets = nil
	p.mu.Unlock()

	setCommandHandler(nil)
	onObjCThread(func() {
		for _, t := range added {
			if t.cmd != 0 && t.token != 0 {
				send(t.cmd, "removeTarget:", uintptr(t.token))
			}
		}
	})
}
