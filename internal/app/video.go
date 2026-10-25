package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/service/bili"
	"github.com/ShellMonster/Biu_Go/internal/service/video"
)

// 边放边看：声音仍然由音频引擎放，视频只是跟着音频的进度出画面。
//
// # 为什么视频不管暂停、音量、切歌
//
// 视频是**纯粹的跟随者**：界面每一帧拿音频进度当时钟问「这会儿该显示哪一帧」
// （`VideoFrame`），拖进度条、暂停、变速都由时钟自己体现，视频这一路一个
// 控制键都没有。代价是视频和音频是两条独立的流，音视频不会严格同步到帧，
// 但对 MV 来说误差在一两帧以内。
//
// # 归属
//
// 一路视频属于**开它时的那一集**（`videoKey`）。当前曲目一变（切歌、换集、
// 停止），下一次界面问状态时就地关掉——不去挂切歌的钩子，因为切歌入口有十几处，
// 漏一处就是上一首的画面叠在下一首的歌上。

// VideoPhase 是视频这一路的状态。
type VideoPhase int

const (
	VideoOff VideoPhase = iota
	VideoLoading
	VideoOn
	VideoFailed
)

// videoQuality 是边放边看默认的画质上限。
const videoQuality = bili.VideoQuality720

type videoRun struct {
	mu     sync.Mutex
	phase  VideoPhase
	key    string
	sess   *video.Session
	gen    int
	err    error
	cancel context.CancelFunc
	// open 换掉取流那一步（测试用）；空就走真的 B 站。
	open func(ctx context.Context, it Item, pos time.Duration, qn bili.VideoQuality) (*video.Session, error)

	// quality 是用户选的画质上限，avail / cur 是这一集实际有的档和正在放的那一档。
	quality bili.VideoQuality
	avail   []bili.VideoQuality
	cur     bili.VideoQuality
}

// SetVideoOpener 换掉「怎么拿到一路视频」。**只给测试用**：界面测试没有真的 B 站，
// 也不该为了点一下角标就去跑一台假 CDN。
func (a *App) SetVideoOpener(f func(ctx context.Context, it Item, pos time.Duration, qn bili.VideoQuality) (*video.Session, error)) {
	a.vid.mu.Lock()
	a.vid.open = f
	a.vid.mu.Unlock()
}

func videoKey(it Item) string { return it.Source + "|" + it.Bvid + "|" + it.Cid }

// VideoAvailable 报告当前这一首能不能看视频。
//
// 判据是「B 站的投稿（不是音频区的歌）、在线取流挂着、这个平台有解码器」。
// 不预先问 B 站有没有视频流：投稿都有，没有的极少，那种在 `StartVideo`
// 里失败、封面上的角标收回去。
func (a *App) VideoAvailable() bool {
	it, ok := a.CurrentItem()
	return ok && it.isOnline() && it.Bvid != "" && it.Type != "audio" && it.Sid == "" &&
		video.Supported() && a.streamingSnapshot() != nil
}

// VideoPhase 是视频这一路此刻的状态。当前曲目变了会顺手把它关掉。
func (a *App) VideoPhase() VideoPhase {
	a.vid.mu.Lock()
	phase, key := a.vid.phase, a.vid.key
	a.vid.mu.Unlock()
	if phase == VideoOff {
		return phase
	}
	if it, ok := a.CurrentItem(); !ok || videoKey(it) != key {
		// Cid 在开播的时候可能还没解出来：只比 Bvid 前缀。
		if !ok || it.Bvid == "" || !sameBvid(key, it) {
			a.StopVideo()
			return VideoOff
		}
	}
	return phase
}

func sameBvid(key string, it Item) bool {
	return len(key) >= len(it.Source)+1+len(it.Bvid) && key[:len(it.Source)+1+len(it.Bvid)] == it.Source+"|"+it.Bvid
}

// StartVideo 开始看视频。已经开着或在加载就什么也不做。
func (a *App) StartVideo() { a.beginVideo(false) }

// VideoQualities 是这一集能选的画质（从低到高）和当前这一档。
// 视频还没取到流之前列表是空的。
func (a *App) VideoQualities() ([]bili.VideoQuality, bili.VideoQuality) {
	a.vid.mu.Lock()
	defer a.vid.mu.Unlock()
	return append([]bili.VideoQuality(nil), a.vid.avail...), a.vid.cur
}

// SetVideoQuality 换画质：就地从当前进度重开，视频窗口不会闪回封面。
// 档位只是「不超过」的上限，这一集没有的档 B 站会给最接近的低档。
func (a *App) SetVideoQuality(q bili.VideoQuality) {
	a.vid.mu.Lock()
	a.vid.quality = q
	live := a.vid.phase == VideoOn || a.vid.phase == VideoLoading
	a.vid.mu.Unlock()
	if live {
		a.beginVideo(true)
	}
}

func (a *App) beginVideo(restart bool) {
	it, ok := a.CurrentItem()
	if !ok || !a.VideoAvailable() {
		return
	}
	s := a.streamingSnapshot()
	pos := a.Snapshot().Position
	ctx, cancel := context.WithCancel(context.Background())
	a.vid.mu.Lock()
	if !restart && (a.vid.phase == VideoLoading || a.vid.phase == VideoOn) {
		a.vid.mu.Unlock()
		cancel()
		return
	}
	if old, oc := a.vid.sess, a.vid.cancel; restart {
		if oc != nil {
			oc()
		}
		if old != nil {
			go old.Close()
		}
		a.vid.sess = nil
	}
	a.vid.gen++
	gen := a.vid.gen
	open, qn := a.vid.open, a.vid.quality
	a.vid.phase, a.vid.key, a.vid.err, a.vid.cancel = VideoLoading, videoKey(it), nil, cancel
	a.vid.mu.Unlock()
	a.wakeAll()

	go func() {
		var sess *video.Session
		var err error
		if open != nil {
			sess, err = open(ctx, it, pos, qn)
		} else {
			sess, err = a.openVideo(ctx, s, it, pos)
		}
		a.vid.mu.Lock()
		if a.vid.gen != gen {
			a.vid.mu.Unlock()
			if sess != nil {
				sess.Close()
			}
			return
		}
		if err != nil {
			a.vid.phase, a.vid.err = VideoFailed, err
		} else {
			a.vid.phase, a.vid.sess = VideoOn, sess
		}
		a.vid.mu.Unlock()
		a.wakeAll()
	}()
}

// StopVideo 关掉视频，回到只放声音。
func (a *App) StopVideo() {
	a.vid.mu.Lock()
	a.vid.gen++
	sess, cancel := a.vid.sess, a.vid.cancel
	a.vid.sess, a.vid.cancel, a.vid.phase, a.vid.err = nil, nil, VideoOff, nil
	a.vid.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if sess != nil {
		go sess.Close()
	}
	a.wakeAll()
}

// VideoError 是上一次视频失败的原因。
func (a *App) VideoError() error {
	a.vid.mu.Lock()
	defer a.vid.mu.Unlock()
	if a.vid.sess != nil {
		return a.vid.sess.Err()
	}
	return a.vid.err
}

// VideoFrame 按音频进度 pos 取该显示的那一帧；没有新帧返回 false。
// 返回的 Pix 到下一次返回 true 之前有效。只在界面线程里调。
func (a *App) VideoFrame(pos time.Duration) (video.Frame, bool) {
	a.vid.mu.Lock()
	sess := a.vid.sess
	a.vid.mu.Unlock()
	if sess == nil {
		return video.Frame{}, false
	}
	if err := sess.Err(); err != nil {
		a.vid.mu.Lock()
		if a.vid.sess == sess {
			a.vid.phase, a.vid.err, a.vid.sess = VideoFailed, err, nil
			go sess.Close()
		}
		a.vid.mu.Unlock()
		return video.Frame{}, false
	}
	return sess.Poll(pos)
}

func (a *App) openVideo(ctx context.Context, s *Streaming, it Item, pos time.Duration) (*video.Session, error) {
	it, err := a.withCid(ctx, s, it)
	if err != nil {
		return nil, err
	}
	a.vid.mu.Lock()
	qn := a.vid.quality
	a.vid.mu.Unlock()
	if qn == bili.VideoQualityUnknown {
		qn = videoQuality
	}
	ref, avail, err := s.Client.PlayVideoList(ctx, it.Bvid, it.Cid, qn)
	if err != nil {
		return nil, err
	}
	a.vid.mu.Lock()
	a.vid.avail, a.vid.cur = avail, bili.VideoQuality(ref.Quality)
	a.vid.mu.Unlock()
	src := &videoSource{media: s.Media, ref: ref, refresh: func(ctx context.Context) (*bili.StreamRef, error) {
		return s.Client.PlayVideo(ctx, it.Bvid, it.Cid, qn)
	}}
	if s.Media == nil {
		return nil, errStreamingMissing
	}
	sess := video.Start(video.Config{Fetch: src.fetch, TempDir: s.TempDir}, pos)
	return sess, nil
}

// videoSource 按字节段取视频流：先试每个 CDN 地址，都不行就重新取流换一份。
type videoSource struct {
	media   *http.Client
	refresh func(context.Context) (*bili.StreamRef, error)

	mu  sync.Mutex
	ref *bili.StreamRef
}

func (v *videoSource) fetch(ctx context.Context, from, to int64) ([]byte, error) {
	var last error
	for round := 0; round < 2; round++ {
		v.mu.Lock()
		ref := v.ref
		v.mu.Unlock()
		for _, u := range ref.URLs {
			b, err := v.get(ctx, u, from, to)
			if err == nil {
				return b, nil
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			last = err
		}
		if round == 0 {
			nr, err := v.refresh(ctx)
			if err != nil {
				return nil, fmt.Errorf("video: 换流失败: %w", err)
			}
			v.mu.Lock()
			v.ref = nr
			v.mu.Unlock()
		}
	}
	return nil, last
}

func (v *videoSource) get(ctx context.Context, url string, from, to int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", from, to-1))
	resp, err := v.media.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("video: HTTP %d", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusOK && from != 0 {
		return nil, fmt.Errorf("video: 服务器不认 Range")
	}
	return io.ReadAll(io.LimitReader(resp.Body, to-from))
}

// SetVideoQualitiesForTest 直接摆一份「能选的画质」。只给测试用（理由同 SetVideoOpener）。
func (a *App) SetVideoQualitiesForTest(avail []bili.VideoQuality, cur bili.VideoQuality) {
	a.vid.mu.Lock()
	a.vid.avail, a.vid.cur = avail, cur
	a.vid.mu.Unlock()
}
