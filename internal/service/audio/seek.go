package audio

import (
	"sync"
	"time"
)

// seekGap 是 Seek 暂停之后等多久再跳：要盖过混音器的 5ms 淡出
// （`mygo/audio/mixer.go` 的 rampFrames）和几块混音缓冲。
const seekGap = 30 * time.Millisecond

// seekState 是播放中 seek 的状态机，两个态：
//
//	idle                                  没有等着落地的 seek
//	gapPending{target, resume}            已替用户暂停，等 seekGap 之后落地
//
// idle --Seek(在放)--> gapPending；gapPending --Seek--> 只改 target（不重新计时，
// 连发的 seek 不会把落地一直往后推）；gapPending --定时器到--> 跳到 target，
// resume 为真就接着放，回 idle；gapPending --Close--> idle，定时器作废。
//
// 间隙里 Player 对外说的是「用户眼里的状态」：Position 是 target，Playing 是
// resume（见 Player.Position / Player.Playing）。Play / Pause 只改 resume。
type seekState struct {
	mu      sync.Mutex
	pending bool
	target  time.Duration
	// resume 是「落地之后要接着放」：Seek 暂停时置真；间隙里 Pause 清掉，
	// Play 重新置上。
	resume bool
	closed bool
	// after 是排落地的方式，nil 就是 time.AfterFunc；测试换成手动推进的时钟。
	// 返回的 stop 取消还没到的那一次。
	after func(d time.Duration, f func()) (stop func())
	stop  func()
}

func (s *seekState) schedule(d time.Duration, f func()) {
	if s.after != nil {
		s.stop = s.after(d, f)
		return
	}
	t := time.AfterFunc(d, f)
	s.stop = func() { t.Stop() }
}
