// 播放条上的睡眠定时：一枚月亮图标，点开是「15/30/45/60 分钟后停」「放完这一首
// 就停」和「关闭定时」。定了之后图标换成主色，浮层顶上写还剩多久。
package ui

import (
	"fmt"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
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
