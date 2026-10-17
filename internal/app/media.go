// 系统媒体会话：把控制中心和锁屏需要的那些东西接上。
//
// 这一层是**接缝**：它知道「什么时候该跟系统说一声变了」，
// 而 `internal/media` 知道「跟系统说的时候该说什么、收到命令该怎么回」。
// 两边都不该知道对方的细节——所以这里只有几个方法和一个回调，
// 没有一个 MediaPlayer、也没有一个窗口。
//
// # 🔴 什么时候发：不是「状态变了就发」
//
// App 每 50ms 写一次播放位置（见 `app.go` 的 tick）。一次一次都发给系统，
// 系统每次都要重建整个 nowPlayingInfo 字典，而 Apple 头文件明说进度是
// **外推**出来的、频繁更新「不是必需的（也不推荐）」。
//
// 所以规则是：**语义变化必发**（歌名 / 艺人 / 封面 / 时长 / 在放与否），
// **位置只在跳的时候发**，另外每 5 秒校正一次（见 mediaPublishEvery）。
// 那套规则本身在 `internal/media` 里，那边能单测。

package app

import (
	"time"

	"github.com/ShellMonster/Biu_Go/internal/media"
)

// mediaPublishEvery 是「在放的时候多久校正一次锁屏上的进度」。
//
// 系统会从上一次报的位置和播放速率**自己外推**（Apple 头文件原话）。
// 外推的是它自己的时间：我们这边卡一下解码、换一次流、音频设备被抢占，
// 它都不知道，进度条就慢慢飘走了。5 秒一次足够接得住，又不会多到
// 变成每帧都在重建字典。
const mediaPublishEvery = 5 * time.Second

// SetMediaSink 装上「该跟系统媒体会话说一声」的那只手，由界面层装一次。
//
// 装上之后它立刻会收到一份当前的快照：挂得太晚的话（用户已经放了三首歌
// 才挂上），锁屏上会先空一阵才出现歌名。
func (a *App) SetMediaSink(fn func(media.NowPlaying)) {
	a.mu.Lock()
	a.mediaSink = fn
	a.mu.Unlock()
	a.publishMedia()
}

// publishMedia 把当前状态交给系统媒体会话。
//
// 没装 sink 时它什么都不做（`internal/media` 没挂上、或者这一层不参与）。
func (a *App) publishMedia() {
	a.mu.Lock()
	sink := a.mediaSink
	n := a.mediaSnapshotLocked()
	a.mediaAt = time.Now()
	a.mu.Unlock()
	if sink != nil {
		sink(n)
	}
}

// mediaSnapshotLocked 拼出给系统的那份快照。必须在持锁时调。
func (a *App) mediaSnapshotLocked() media.NowPlaying {
	// 没有播放器 = 没有歌。这时不是「暂停」也不是「停止」，是「什么都没有」：
	// 系统那边应该把曲目信息收掉，而不是留一首已经不存在的歌躺在锁屏上。
	if a.player == nil {
		return media.NowPlaying{State: media.StateNone}
	}
	st := media.StatePaused
	switch {
	case a.playing:
		st = media.StatePlaying
	case a.stopped:
		st = media.StateStopped
	}
	return media.NowPlaying{
		State:      st,
		Title:      a.title,
		Artist:     a.artist,
		ArtworkURL: a.cover,
		Duration:   a.duration,
		Position:   a.position,
	}
}

// setArtist 记下当前这首的艺人（在线条目的 UP 主名）。
//
// 本地文件永远是空的——Biu 上游也不读音频内嵌的标签（`app/open.go` 那条
// 备注），所以本地音乐在那边也是没有艺人的一行。**别拿文件夹名去凑一个**：
// 那是个猜测，而猜出来的东西会一直显示在锁屏上，用户看不出它是猜的。
//
// 值不变就不叫系统：换歌、入队都会走到这里，为一个没变的值重建一次
// 系统字典是白花的。
func (a *App) setArtist(name string) {
	a.mu.Lock()
	if a.artist == name {
		a.mu.Unlock()
		return
	}
	a.artist = name
	a.mu.Unlock()
	a.publishMedia()
}

// MediaCommand 是系统媒体会话收到一条远程命令时的入口。
//
// **它跑在自己的协程上，不在系统的回调线程上**（见 `internal/media`），
// 所以里面做网络请求（换流取地址）是允许的。
func (a *App) MediaCommand(cmd media.Command, at time.Duration) {
	switch cmd {
	case media.CommandPlay:
		a.Play()
	case media.CommandPause:
		a.Pause()
	case media.CommandToggle:
		a.Toggle()
	case media.CommandNext:
		a.Next()
	case media.CommandPrev:
		a.Prev()
	case media.CommandStop:
		a.Stop()
	case media.CommandSeek:
		a.SeekTo(at)
	}
	// 命令本身的执行多半已经报过一次了（Toggle / Next 都各自报过），
	// 这一句报的是「执行完之后系统该看到的那个状态」。
	a.publishMedia()
}

// SeekTo 跳到从头算起的 t。位置会被夹在整首歌的范围里。
//
// 锁屏上拖进度条走的是这里。它和界面上拖那一条是同一个入口——
// 所以两条路上看到的进度条是同一根，不会一个动了另一个不动。
func (a *App) SeekTo(t time.Duration) {
	a.mu.Lock()
	dur := a.duration
	want := a.wantPlay
	a.mu.Unlock()
	if dur <= 0 || t < 0 {
		return
	}
	if t >= dur {
		// 拖到末尾就是「放完这一首」。走 ScrubEnd 的那条路，它会按
		// wantPlay 决定要不要接着播——拖到末尾和拖到中段不该一个结果。
		a.ScrubEnd(1)
		return
	}
	_ = want
	a.ScrubEnd(float64(t) / float64(dur))
}

// Play 确保在放。已经在放就什么都不做。
//
// ⚠️ 它**不是** Toggle。用户在控制中心点了「播放」而歌本来就在放的时候，
// 按切换会把它按停——那是「按了播放反而停了」，比按了没反应更糟。
func (a *App) Play() {
	a.mu.Lock()
	p := a.player
	a.mu.Unlock()
	if p == nil || p.Playing() {
		return
	}
	p.Play()

	a.mu.Lock()
	a.playing, a.wantPlay, a.stopped = true, true, false
	wake := a.wake
	a.mu.Unlock()
	if wake != nil {
		wake()
	}
}

// Pause 确保停着。本来就停着就什么都不做。
func (a *App) Pause() {
	a.mu.Lock()
	p := a.player
	a.mu.Unlock()
	if p == nil || !p.Playing() {
		return
	}
	p.Pause()

	a.mu.Lock()
	a.playing, a.wantPlay = false, false
	wake := a.wake
	a.mu.Unlock()
	if wake != nil {
		wake()
	}
}

// Stop 停下来，**并且回到开头**。
//
// 这是锁屏和控制中心上那个方块键。和暂停分开是有理由的：暂停的进度条停在
// 原地、按钮回到「播放」；停止之后进度条回到开头——用户按停止的意思是
// 「这首到此为止」，不是「就停在这儿」。
func (a *App) Stop() {
	a.mu.Lock()
	p := a.player
	if p == nil {
		a.mu.Unlock()
		return
	}
	// 🔴 播放记录要在**清零之前**把位置抄下来（下面那句 `stopTrack` 要用）。
	// 用清零之后的 0 报上去，B 站那边这一条就成了「从头开始听然后立刻停
	// 了」，而用户明明听到了 3 分 20 秒。
	at := a.position
	a.playing, a.wantPlay, a.stopped = false, false, true
	a.position = 0
	wake := a.wake
	dur := a.duration
	a.mu.Unlock()

	// 「停止」是**终止**：这一轮到此为止，会话要结掉（见 playreport.go 的
	// 「一、什么时候发」）。暂停留会话、停止结会话，靠的就是这一句——
	// 两者的区别只有在这里才知道。
	a.stopTrack(at, dur)

	p.Pause()
	p.Seek(0)
	if wake != nil {
		wake()
	}
}
