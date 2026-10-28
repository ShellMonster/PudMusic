package app

import (
	"time"

	"github.com/ShellMonster/PudMusic/internal/service/audio"
)

// 切歌淡入淡出（设置里的「切歌淡入淡出」，0 = 关，默认关）：
//
//   - 一首快放完（剩下的墙钟时间 ≤ 设定的秒数）时，音量渐渐降到 0；
//   - 队列自动接上的下一首从 0 渐渐升回用户设的音量。
//
// 只管**自动切歌**：用户自己点下一首 / 点某一首，要的是马上出声，不淡入。
// 淡出走音频层的增益斜坡（audio/fade.go），所以用户之后动音量、拖进度条会把它
// 打断并拉回来，不会卡在 0。

// fadeInNextTrack 标记「下一台新播放器按淡入时长放」。
func (a *App) markFadeIn() {
	a.mu.Lock()
	if a.trackFade > 0 {
		a.fadeInNext, a.fadeInUntil = a.trackFade, time.Now().Add(time.Minute)
	}
	a.mu.Unlock()
}

// takeFadeInLocked 取走那个一次性标记；过了一分钟没用上就作废。须持 a.mu。
func (a *App) takeFadeInLocked() time.Duration {
	d := a.fadeInNext
	a.fadeInNext = 0
	if time.Now().After(a.fadeInUntil) {
		return 0
	}
	return d
}

// applyFadeIn 给刚建好的播放器套上淡入时长。须持 a.mu。
func (a *App) applyFadeInLocked(p *audio.Player) {
	if d := a.takeFadeInLocked(); d > 0 {
		p.SetFadeIn(d)
	}
}

// tickTrackFade 在每一拍里判断要不要开始淡出（或把淡出拉回来）。
// pos / dur 是这一拍读到的位置和时长。
func (a *App) tickTrackFade(p *audio.Player, pos, dur time.Duration, playing bool) {
	a.mu.Lock()
	fade, rate, cur := a.trackFade, a.rate, a.fadeOutFor
	vol := a.gainLocked()
	a.mu.Unlock()
	if p == nil {
		return
	}
	if fade <= 0 || !playing || dur <= 0 {
		if cur == p {
			a.mu.Lock()
			a.fadeOutFor = nil
			a.mu.Unlock()
			if fade <= 0 && playing {
				p.SetVolume(vol)
			}
		}
		return
	}
	if rate <= 0 {
		rate = 1
	}
	remaining := time.Duration(float64(dur-pos) / rate)
	switch {
	case remaining > 0 && remaining <= fade && cur != p:
		a.mu.Lock()
		a.fadeOutFor = p
		a.mu.Unlock()
		p.FadeOut(remaining)
	case remaining > fade && cur == p:
		a.mu.Lock()
		a.fadeOutFor = nil
		a.mu.Unlock()
		p.SetVolume(vol)
	}
}
