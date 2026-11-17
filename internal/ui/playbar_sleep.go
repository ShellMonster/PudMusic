// 播放条上的睡眠定时：一枚月亮图标，点开是「15/30/45/60 分钟后停」「放完这一首
// 就停」和「关闭定时」。定了之后图标换成主色，浮层顶上写还剩多久。
package ui

import (
	"fmt"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/notice"
)

const sleepMenuW = 140

func sleepActive(st app.State) bool { return !st.SleepAt.IsZero() || st.SleepAfterTrack }

func sleepRemaining(st app.State) string {
	if st.SleepAfterTrack {
		return "放完这一首就停"
	}
	m := int(time.Until(st.SleepAt)/time.Minute) + 1
	return fmt.Sprintf("还剩约 %d 分钟", max(m, 1))
}

// 三个设置动作各说一句话。目标是同一件事：定上之后图标只换颜色，
// 「定的是哪一种」要重新点开浮层才知道。
func (s *Shell) setSleepTimer(d time.Duration) {
	s.app.SetSleepTimer(d)
	s.addToast(fmt.Sprintf("将在 %d 分钟后暂停播放", int(d/time.Minute)), notice.Success)
}

func (s *Shell) sleepAfterTrack() {
	s.app.SleepAfterTrack()
	s.addToast("放完这一首就暂停播放", notice.Success)
}

func (s *Shell) cancelSleepTimer() {
	s.app.CancelSleepTimer()
	s.addToast("已关闭睡眠定时", notice.Success)
}
