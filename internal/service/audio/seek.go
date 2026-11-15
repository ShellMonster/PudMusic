package audio

import (
	"sync"
	"time"
)

// seekGap 是 Seek 暂停之后等多久再跳：要盖过混音器的 5ms 淡出
// （`mygo/audio/mixer.go` 的 rampFrames）和几块混音缓冲。
const seekGap = 30 * time.Millisecond

// seekState 是播放中 seek 的状态：代数 + 「暂停是 seek 替用户按的」这一位。
type seekState struct {
	mu sync.Mutex
	// gen 每来一次 Seek 加一。等完静音间隙之后发现 gen 变了，说明有更新的
	// Seek 顶上来了，这一次作废（见 Player.Seek）。
	gen uint64
	// resume 是「播放是被 Seek 暂停的，落地之后要接着放」。
	resume bool
	// wait 是等静音间隙的方式，nil 就是 time.Sleep；测试换成手动推进混音器。
	wait func(time.Duration)
}

func (s *seekState) sleep(d time.Duration) {
	if s.wait != nil {
		s.wait(d)
		return
	}
	time.Sleep(d)
}
