// 播放条上的睡眠定时：一枚月亮图标，点开是「15/30/45/60 分钟后停」「放完这一首
// 就停」和「关闭定时」。定了之后图标换成主色，浮层顶上写还剩多久。
package ui

import (
	"fmt"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/scale"
	"github.com/ShellMonster/Biu_Go/internal/ui/widgets/space"
	"github.com/egoist/mygo/ui"
)

const sleepMenuW = 140

func sleepActive(st app.State) bool { return !st.SleepAt.IsZero() || st.SleepAfterTrack }

func (s *Shell) viewSleep(c *ui.Context, st app.State) {
	btn := iconKey(c, iconSleep, "睡眠定时", iconMd, false).
		Size(iconHitMd, iconHitMd).Padding(space.None, space.Px7)
	if sleepActive(st) {
		btn = flatSolid(c, btn)
	}
	if btn.Clicked() {
		s.sleepOpen = !s.sleepOpen
	}
	ui.Popover(c, btn, &s.sleepOpen, func() {
		s.guardAt("睡眠定时菜单", s.viewSleepMenu)(c)
	})
}

func (s *Shell) viewSleepMenu(c *ui.Context) {
	st := s.app.Snapshot()
	t := c.Theme()
	ui.Column(c).Key("playbar:sleep").Label("睡眠定时菜单").
		Padding(space.Px12, space.Px8).Gap(space.Px2).Width(sleepMenuW).Children(func() {
		if sleepActive(st) {
			ui.Text(c, sleepRemaining(st)).TextColor(t.TextMuted).FontSize(scale.Font(c, scale.RateMenuFont))
		}
		item := func(label string, on bool, do func()) {
			b := flatButton(c, label).Width(sleepMenuW - 16).Radius(t.Radius).
				FontSize(scale.Font(c, scale.RateMenuFont)).Label("睡眠定时 " + label)
			if on {
				b = flatSolid(c, b)
			}
			if b.Clicked() {
				s.sleepOpen = false
				do()
			}
		}
		for _, d := range app.SleepTimerSteps {
			d := d
			item(fmt.Sprintf("%d 分钟后停", int(d/time.Minute)), false, func() { s.app.SetSleepTimer(d) })
		}
		item("放完这一首就停", st.SleepAfterTrack, s.app.SleepAfterTrack)
		if sleepActive(st) {
			item("关闭定时", false, s.app.CancelSleepTimer)
		}
	})
}

func sleepRemaining(st app.State) string {
	if st.SleepAfterTrack {
		return "放完这一首就停"
	}
	m := int(time.Until(st.SleepAt)/time.Minute) + 1
	return fmt.Sprintf("还剩约 %d 分钟", max(m, 1))
}
