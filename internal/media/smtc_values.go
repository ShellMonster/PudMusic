// Windows 系统媒体会话（SMTC）说的那几句话，和我们的话怎么对得上。
//
// **这一段刻意不带任何平台标签**：值是 Windows 的，可它们是这一层对外的
// 词汇表的一部分（「在放」在系统那边是哪一个数、系统按了哪个键对应我们的哪条
// 命令）。放在没有 Windows 的机器上就能断映射对不对——而断错了的后果
// （系统里的播放状态一直停在「暂停」、按了下一首没反应）在真机上要盯很久
// 才发现，而且**界面这边一点异样都看不出来**。
//
// 同一个道理，darwin 那一半也把 `MPNowPlayingInfoPlaybackState` 留在了
// media.go 里（见「系统的词汇」那一段）。两个平台各占一个文件，不互相引用。
package media

// smtcStatus 是 `Windows.Media.MediaPlaybackStatus` 的值。
//
// ⚠️ **这些数字和 macOS 的不是一回事，两边差一位**：Windows 是
// Closed=0 起、Playing=3，macOS 是 Unknown=0 起、Playing=1。照着另一边的
// 数字写过来，在真机上表现为「歌在放，系统显示暂停」。
type smtcStatus int32

const (
	// smtcClosed 是「没有媒体会话」。和 macOS 的 StateNone 一个意思。
	smtcClosed smtcStatus = 0
	// smtcChanging 是「正在换源」。这一版不主动用它。
	smtcChanging smtcStatus = 1
	// smtcStopped 是停了。
	smtcStopped smtcStatus = 2
	// smtcPlaying 是正在出声。
	smtcPlaying smtcStatus = 3
	// smtcPaused 是暂停在原地。
	smtcPaused smtcStatus = 4
)

// smtcPlaybackStatus 把我们的话翻成系统的话。
//
// StateNone 翻成 **Closed** 而不是 Stopped：Windows 上 Closed 的意思是
// 「这个应用现在根本没有媒体会话」，系统会把它从媒体浮窗里整个拿掉；Stopped
// 则是一个还在、只是停着的会话，会在浮窗里留下一条永远不动的记录。
//
// 这一句**必设**——不是「建议设」。不设的话系统按默认的 Closed 处理，
// 浮窗里根本不出现我们的歌，而应用这边完全看不出发生过什么。
func smtcPlaybackStatus(s State) smtcStatus {
	switch s {
	case StatePlaying:
		return smtcPlaying
	case StatePaused:
		return smtcPaused
	case StateStopped:
		return smtcStopped
	}
	return smtcClosed
}

// smtcButton 是 `Windows.Media.SystemMediaTransportControlsButton` 的值。
//
// 只有五个能接的写在这儿。剩下的（Record / FastForward / Rewind / ChannelUp /
// ChannelDown）在 media.go 里就被排除在 Command 之外了——Windows 那边它们是
// 同一串整数里的邻居，写下来只是多几个「按了没反应」的按钮。
type smtcButton int32

const (
	smtcButtonPlay  smtcButton = 0
	smtcButtonPause smtcButton = 1
	smtcButtonStop  smtcButton = 2
	smtcButtonNext  smtcButton = 6
	smtcButtonPrev  smtcButton = 7
)

// smtcCommand 把系统按下的那个键翻成我们的命令。
//
// 第二个返回值是「这个键我们接不接」。接不住的返回 false 的原因在调用点：
// 那是要**显式关掉**的（把 IsFastForwardEnabled 之类设成 false），不是忽略。
// 不关的话 Windows 自己的媒体浮窗上会摆出一个按下去没反应的键。
//
// ⚠️ Play 和 Pause **不合成一条 Toggle**：用户在浮窗上看到的是两个不同的
// 键（或者一个随播放状态改字的键），系统告诉我们的就是它按的那个。
// 合成 Toggle 的话会出现「按了播放反而把歌按停」。
//
// ⚠️ Windows 上**没有 seek**：SMTC 的时间轴不发 seek 请求，浮窗上拖进度条
// 走的是另一条事件（PlaybackPositionChangeRequested），走 seekPosition 那个
// 口，不是这一张表。所以 smtcCommand 永远给不出 CommandSeek。
func smtcCommand(b smtcButton) (Command, bool) {
	switch b {
	case smtcButtonPlay:
		return CommandPlay, true
	case smtcButtonPause:
		return CommandPause, true
	case smtcButtonStop:
		return CommandStop, true
	case smtcButtonNext:
		return CommandNext, true
	case smtcButtonPrev:
		return CommandPrev, true
	}
	return CommandPlay, false
}
