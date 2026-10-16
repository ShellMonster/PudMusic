// 在线播放：把收藏夹里的一个条目变成正在响的一首歌。
//
// # 三条换流路径，分工在这一层和 audio 那一层
//
//	播放前预判   开之前先看手上那条流还能不能用（这一层）
//	报错后重试   源下不动 → 换备用地址 → 换流（audio 层）
//	换流续播     换流之后从**同一个字节偏移**接着下，位置一点不动（audio 层）
//
// 为什么换流的机制测试在 `audio` 包里：那里能精确造出「下到第 N 字节时
// 这条地址失效」，而这里测的是它有没有被正确地接上——代次对不对、换到的
// 还是不是同一首、界面上的音质徽章有没有跟着变。
//
// # 界面线程上不碰网络
//
// 取流是一次带 WBI 签名的网络往返（实测 200~400 ms）。`play` 是界面线程
// 直接调的，在它里面同步取流就是每点一次歌冻一下窗口。所以在线这一支是
// **开后台 goroutine** 做的，靠**播放代次**（`gen`）把迟到的结果丢掉：
// 连点五首，前四首的取流结果回来时发现代次已经变了，直接关掉走人。
//
// 这一条也是 `spec.md`「播放状态机」要求的：**异步结果必须带代次，
// 代次不匹配的丢弃**。

package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/audio"
	"github.com/ShellMonster/Biu_Go/internal/service/bili"
)

// errStreamingMissing 是「在线播放没挂上」。见 `MountStreaming`。
var errStreamingMissing = errors.New("在线播放还没接上")

// Streaming 是取流这一摊事的依赖。零值不挂——界面要判空。
type Streaming struct {
	// Client 是 B 站客户端。**全项目那一份**（`bili.NewClient`）。
	Client *bili.Client
	// Media 是取音频字节用的 http.Client。
	//
	// ⚠️ 它**必须**是 `app.CredentialTransport.Client(nil)`——带凭据边界
	// 但**不挂 jar**。B 站 CDN 跟 B 站 API 不是同一个凭据域，把 SESSDATA
	// 发给一台存音频文件的机器没有任何理由（`spec.md`「网络」一节）。
	// Referer 照给：B 站 CDN 不给直接 403（真机验过）。
	Media *http.Client
	// Quality 是默认音质偏好。零值是 AudioAuto（有无损给无损）。
	Quality bili.AudioQuality
	// TempDir 是下载缓存落在哪。空 = 系统临时目录。
	TempDir string
}

// onlineTrack 是「当前正在放的那条在线流」。
type onlineTrack struct {
	item Item
	// ref 是这条流**当前**的引用。换流之后要换掉——它同时是「换回来的
	// 还是不是同一首」的比对基准（见 `refetch`）。
	ref    *bili.StreamRef
	source *audio.StreamSource
	// reopening 是「已经因为换了流而重开过一次了」。
	//
	// 防止「换流 → 又失败 → 再换流 → 再重开」绕圈。一首歌只重开一次：
	// 第二次还不行就该老老实实报错，用户听得到「这首歌放不了了」，
	// 比程序一直自己折腾强。
	reopening bool
	// quality 是当前这条流的音质徽章。换流之后要跟着更新。
	quality string
}

// streamKey 是取流缓存的键。
//
// 用 Biu 的判重标识（`source/type/bvid/cid/sid`）而不是队列下标：换了歌
// 回来时同一条流还能复用，省一次带签名的往返。
func streamKey(it Item) string {
	return it.Source + "|" + it.Type + "|" + it.Bvid + "|" + it.Cid + "|" + it.Sid
}

// MountStreaming 挂上在线播放。**在开窗口之前调**（它自己不读盘，但让
// 第一���点歌不要替「开窗」买单）。
func (a *App) MountStreaming(s Streaming) {
	a.mu.Lock()
	a.streaming = &s
	a.streamCache = map[string]*bili.StreamRef{}
	a.mu.Unlock()
}

// StreamingMounted 报告在线播放挂上没有。
func (a *App) StreamingMounted() bool { return a.streamingSnapshot() != nil }

// SetAudioQuality 换默认音质偏好。**对已经在放的那一首不生效**——
// 正在下的字节不会因为改了偏好而变。要立刻生效得重新取流，那是一次
// 用户可见的中断，交给「换一档音质」那个动作去做，不在这里偷偷做。
func (a *App) SetAudioQuality(q bili.AudioQuality) {
	a.mu.Lock()
	if a.streaming != nil {
		a.streaming.Quality = q
	}
	a.streamCache = map[string]*bili.StreamRef{}
	a.mu.Unlock()
}

// AudioQuality 返回当前偏好。
func (a *App) AudioQuality() bili.AudioQuality {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.streaming == nil {
		return bili.AudioAuto
	}
	return a.streaming.Quality
}

// streamingSnapshot 拿一份配好的取流零件。没挂或者缺零件时是 nil。
func (a *App) streamingSnapshot() *Streaming {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.streaming == nil || a.streaming.Client == nil || a.streaming.Media == nil {
		return nil
	}
	s := *a.streaming
	return &s
}

// isOnline 报告这个条目是不是一条在线流。
func (it Item) isOnline() bool { return it.Source == "online" }

// playableOnline 报告这个在线条目现在有没有足够的信息去取流。
//
// 只有 **mv**（bvid + cid）这一种是通的：B 站的收藏夹里放的是投稿，
// cid 是分 P 的 id，**不给它会取到第一集**——不报错，只是放错了那一集。
//
// 「audio」（sid，B 站音乐库）是另一种来源，走的是另一个接口，
// 现在还没接。给它一句人话，而不是放一个打不开的条目让人猜。
func playableOnline(it Item) error {
	if it.Type != "mv" || it.Bvid == "" || it.Cid == "" {
		if it.Type == "audio" && it.Sid != "" {
			return errors.New("B 站音乐库那条路还没接上，先放收藏夹里的投稿")
		}
		return errors.New("这一首还不能放：缺稿件 id 或分 P id")
	}
	return nil
}

// playOnline 开一条在线流。**立刻返回**，真正的活儿在后台 goroutine 里。
//
// 返回的错误只包含「同步就能看出来」的那几种（条目不完整、没挂取流），
// 它们会让调用方把队列滚回去；网络上的失败走播放条上的红字。
func (a *App) playOnline(it Item) error {
	if err := playableOnline(it); err != nil {
		return err
	}
	if !a.StreamingMounted() {
		return errStreamingMissing
	}

	// 代次在这里加。后面每一次往状态上写东西都要对一遍。
	a.mu.Lock()
	a.onlineGen++
	gen := a.onlineGen
	a.errText = ""
	a.mu.Unlock()

	go a.runOnline(it, gen)
	return nil
}

// runOnline 是后台那条线：建一套播放链路，然后（代次还对得上的话）换上。
func (a *App) runOnline(it Item, gen int) {
	s := a.streamingSnapshot()
	if s == nil {
		a.failOnline(gen, errStreamingMissing)
		return
	}
	a.mu.Lock()
	vol := a.volume
	a.mu.Unlock()

	src, p, ref, err := a.buildOnline(context.Background(), s, it, vol, gen)
	if err != nil {
		a.failOnline(gen, err)
		return
	}

	a.mu.Lock()
	if a.onlineGen != gen || a.closed {
		// 代次已经变了（切歌了），或者应用正在退出。手上这些一样都不留。
		a.mu.Unlock()
		p.Close()
		return
	}
	old := a.player
	a.player = p
	a.title = it.Title
	a.path = ""
	a.cover = it.Cover
	a.artist = it.Artist
	a.position = 0
	a.duration = p.Duration()
	a.wantPlay, a.playing = true, true
	a.stopped = false
	a.quality = ref.Label
	a.online = &onlineTrack{item: it, ref: ref, source: src, quality: ref.Label}
	a.reportedErr = nil
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
}

// buildOnline 建一套「流引用 + 字节源 + 播放器」，**不往 App 上装**。
//
// 开新歌和「从播放位置重开」走的是同一条路，所以它们共用这一个函数——
// 两份实现迟早会漂移，而漂移的那一半正好是「换流后从原位置继续」这条规格。
func (a *App) buildOnline(ctx context.Context, s *Streaming, it Item, vol float64, gen int) (
	*audio.StreamSource, *audio.Player, *bili.StreamRef, error,
) {
	ref, err := a.resolve(ctx, s, it, s.Quality)
	if err != nil {
		return nil, nil, nil, err
	}
	src, err := audio.NewStreamSource(audio.StreamSourceOptions{
		URLs:      ref.URLs,
		ExpiresAt: ref.ExpiresAt,
		TempDir:   s.TempDir,
		Client:    s.Media,
		// 续传的偏移在源那边，它要接就从同一个字节接着下。
		// 这里重新取的是**整条**流——偏移只是告诉调用方「你从哪里断的」，
		// 而取流接口没有「从第几个字节开始」这个参数。
		Refetch:   func(ctx context.Context, _ int64) *audio.StreamReply { return a.refetch(ctx, s, it) },
		OnRefresh: func(r *audio.StreamReply) { a.noteQuality(gen, r.Quality) },
	})
	if err != nil {
		return nil, nil, nil, err
	}
	p, err := audio.OpenStream(src)
	if err != nil {
		src.Close()
		return nil, nil, nil, err
	}
	p.SetVolume(vol)
	return src, p, ref, nil
}

// resolve 拿到一条能播的流。
//
// 这就是**播放前预判**那一条路径：手上有一条这个条目的流（上次放过、
// 切走又切回来）而且**还没到过期线**，就直接用，省掉一次带签名的往返；
// 过期了或者压根没有，就去问 B 站要一条新的。
func (a *App) resolve(ctx context.Context, s *Streaming, it Item, pref bili.AudioQuality) (*bili.StreamRef, error) {
	key := streamKey(it)

	a.mu.Lock()
	cached := a.streamCache[key]
	a.mu.Unlock()

	if cached != nil && cached.Usable(time.Now()) {
		return cached, nil
	}
	ref, err := s.Client.PlayURL(ctx, it.Bvid, it.Cid, pref)
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	// 缓存只留手上这一条。留着几十条过期地址没有用——过期是常态，
	// 而每一条都在诊断里等着被脱敏。
	a.streamCache = map[string]*bili.StreamRef{key: ref}
	a.mu.Unlock()
	return ref, nil
}

// refetch 是**报错后换流重试**那一条路径的回调：源下不动了，用同一首
// 重新取一条流，源会从它给的那个字节偏移接着下。
//
// 一定要比一次「还是不是同一首」：取流接口在稿件被删、被替换成别的内容时
// 会**静默**给一条看着完全正常的流。不比的话，用户会在毫无提示的情况下
// 听到另一首歌，而界面上什么都看不出来——那比断在这儿糟得多。
func (a *App) refetch(ctx context.Context, s *Streaming, it Item) *audio.StreamReply {
	a.mu.Lock()
	prev := a.currentRef()
	a.mu.Unlock()

	ref, err := s.Client.PlayURL(ctx, it.Bvid, it.Cid, s.Quality)
	if err != nil {
		return nil
	}
	if prev != nil && !prev.SameTrack(ref) {
		return nil
	}
	if ref.Bvid != it.Bvid || ref.Cid != it.Cid {
		return nil
	}

	a.mu.Lock()
	// 和 resolve 一样只留一条：换流之后手上这条就是唯一有用的那条。
	a.streamCache = map[string]*bili.StreamRef{streamKey(it): ref}
	a.mu.Unlock()

	// ⚠️ 这里**不填 Size**：取流接口的返回里没有长度，而 `StreamReply.Size`
	// 的作用是「长度对不上就别硬接」。它在源那边是靠**两个响应各自报出来的
	// Content-Length** 相比出来的（见 `stream_source_pump.go` 的 expectTotal），
	// 所以这一处填 0 不会让那道防线失效——填一个猜的数才会。
	return &audio.StreamReply{
		URLs:      ref.URLs,
		ExpiresAt: ref.ExpiresAt,
		Quality:   ref.Label,
	}
}

// currentRef 返回当前那条在线流的引用。必须在持锁时调。
func (a *App) currentRef() *bili.StreamRef {
	if a.online == nil {
		return nil
	}
	return a.online.ref
}

// noteQuality 是换流成功之后的回调：把播放条上的音质徽章更新掉。
//
// 换流拿回来的可能是**降级过**的那条（会员过期、B 站改了可用档位）。
// 用户得看见——无声地降级会让人以为自己还在听无损。
func (a *App) noteQuality(gen int, label string) {
	a.mu.Lock()
	if a.onlineGen != gen || a.online == nil || label == "" {
		a.mu.Unlock()
		return
	}
	changed := a.quality != label
	a.quality = label
	a.online.quality = label
	wake := a.wake
	a.mu.Unlock()
	if changed && wake != nil {
		wake()
	}
}

// checkPlayback 每 50 ms 看一眼：这一路播放是不是出了错。
//
// **为什么不能靠 `Playing() == false` 判断**：暂停、停止、关闭、播完、
// 解码失败在那边**全都是 false**（`mygo/audio/audio.go` 的 `Playing`）。
// 不做这一步的话，网络一断、解码一失败、容器坏了，界面上就只是「歌忽然
// 停了」，没有任何提示——而用户完全不知道该重试还是该换网。
// `spec.md`「播放状态机」一节说的「解码失败不能被当成播放结束」是它。
func (a *App) checkPlayback() {
	a.mu.Lock()
	p := a.player
	tr := a.online
	a.mu.Unlock()
	if p == nil {
		return
	}
	err := p.Err()
	if err == nil {
		return
	}

	// ⚠️ **在线流要看字节源自己的错，不要看解码器的**。解码器看到的是**后果**：
	// 字节断了之后它多半先报「这一帧的 CRC 对不上」，而真正的原因是网络断了
	// 或者地址失效了。把后果说给用户听，用户会以为歌坏了、缓存坏了，
	// 于是去清缓存重下——而正确���反应是「换个网」。
	if tr != nil && tr.source != nil {
		if srcErr := tr.source.Err(); srcErr != nil && !errors.Is(srcErr, io.EOF) {
			err = srcErr
		}
	}

	// 同一条错误只报一次。解码器的 Err 记的是**第一条**错误、不自己清零，
	// 不比对的话每 50 ms 都会重新冒一次。
	a.mu.Lock()
	if a.reportedErr == err {
		a.mu.Unlock()
		return
	}
	a.reportedErr = err
	a.playing = false
	wake := a.wake
	a.mu.Unlock()

	// 歌停了（解码失败、网络断、换流换不动）。系统那边的播放状态也得跟着
	// 变成「不在放」，否则控制中心上还是「播放中」——用户按暂停，
	// 系统说它已经在暂停了，那一下就没反应。
	a.publishMedia()
	if wake != nil {
		wake()
	}

	switch classifyOnlineError(err) {
	case onlineErrIgnore:
		// 切歌/退出带出来的，不是错。状态已经被 publishMedia 报成「不在放」了。
	case onlineErrReopen:
		// 换到了另一条流：字节偏移接不上，但**播放位置**还在原处。
		// 从那儿重开就是「换流后从原来的位置继续」。
		a.refreshOnlineNow()
	default:
		a.setErr(bili.Friendly(err))
	}
}

// failOnline 报一次在线播放的失败。
//
// 代次对不上就一声不吭地走人：那已经是上一首歌的事了，在播放条上冒一句
// 红字只会让用户以为现在这首选不动了。
func (a *App) failOnline(gen int, err error) {
	a.mu.Lock()
	stale := a.onlineGen != gen
	a.mu.Unlock()
	if stale {
		return
	}
	a.setErr(bili.Friendly(err))
}

// refreshOnlineNow 从当前的播放位置重开一首。
//
// 这是**第三条路径**在应用层的入口：音频层把「换了另一条流，接不上原来的
// 字节」这件事报上来（`audio.ErrStreamChanged`），能不能从**播放位置**继续
// 就得在这一层做——播放位置是时间，只有 App 知道上一次读到哪儿了。
//
// 一首歌只重开一次（`onlineTrack.reopening`）。第二次还不行就该如实报错：
// 用户听得到「这首歌放不了了」，比程序一直自己折腾强。
func (a *App) refreshOnlineNow() {
	a.mu.Lock()
	tr := a.online
	if tr == nil || tr.reopening {
		a.mu.Unlock()
		return
	}
	tr.reopening = true
	gen := a.onlineGen
	pos, vol, want := a.position, a.volume, a.wantPlay
	a.mu.Unlock()

	s := a.streamingSnapshot()
	if s == nil {
		a.setErr(bili.Friendly(errStreamingMissing))
		return
	}

	go func() {
		src, p, ref, err := a.buildOnline(context.Background(), s, tr.item, vol, gen)
		if err != nil {
			a.failOnline(gen, err)
			return
		}

		a.mu.Lock()
		if a.onlineGen != gen || a.closed || a.online == nil {
			a.mu.Unlock()
			p.Close()
			return
		}
		old := a.player
		a.player = p
		a.duration = p.Duration()
		a.position = pos
		a.quality = ref.Label
		a.online = &onlineTrack{item: tr.item, ref: ref, source: src, quality: ref.Label, reopening: true}
		a.reportedErr = nil
		a.errText = ""
		wake := a.wake
		a.mu.Unlock()

		if old != nil {
			old.Close()
		}
		// 跳回原来的位置。这一步是这一整条路径的全部意义：
		// 没有它，用户会在歌播到一半的时候突然从头听到。
		if pos > 0 && pos < p.Duration() {
			p.Seek(pos)
		}
		if want {
			p.Play()
		}
		a.publishMedia()
		if wake != nil {
			wake()
		}
	}()
}

// onlineErrorAction 把一条在线流的错误分成「怎么办」。
//
// 分成三类是因为它们要走的路完全不同，而且**只有三类都处理对了，
// 「换流后从原进度继续」才成立**：错了的那一类会让歌停在半路。
type onlineErrorAction int

const (
	// onlineErrIgnore 什么都不用做：只是切歌/退出带出来的，不是错。
	onlineErrIgnore onlineErrorAction = iota
	// onlineErrReport 报给用户。歌停了，且没法自己救。
	onlineErrReport
	// onlineErrReopen 从当前的播放位置重开一首。
	onlineErrReopen
)

func classifyOnlineError(err error) onlineErrorAction {
	switch {
	case err == nil, errors.Is(err, audio.ErrSourceClosed):
		return onlineErrIgnore
	case errors.Is(err, audio.ErrStreamChanged):
		// 换了另一条流：字节偏移对不上，但**播放位置**还在。
		// 从那儿重开就是「从原来的位置继续」。
		return onlineErrReopen
	}
	// 其余（网络断、地址失效、换流换不动）都在 audio 层自己换过一轮了，
	// 换不动就是换不动，如实说。
	return onlineErrReport
}
