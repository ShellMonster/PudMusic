package ui

import (
	"fmt"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/ui"
)

// viewPlayBar 是底部那条播放条：曲目名、播放/暂停、可拖的进度条、音量。
//
// 阶段 1 只有这些。队列、列表、歌词、封面缓存都在后面的阶段。
func (s *Shell) viewPlayBar(c *ui.Context, st app.State) {
	t := c.Theme()

	ui.Column(c).Key("playbar").Padding(16).Gap(10).
		Background(t.Surface).Radius(12).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			title := st.Title
			if !st.HasTrack {
				title = "还没有打开文件"
			}
			ui.Text(c, title).FontSize(15).Bold().Grow(1).
				TextColor(textColor(t, st.HasTrack))

			// 音量条自己带无障碍标签，不用旁边再摆一个「音量」字。
			vol := st.Volume
			if ui.Slider(c, &vol, 0, 1).Width(120).Label("音量").Changed() {
				s.app.SetVolume(vol)
			}

			if st.Playing {
				if ui.Button(c, "暂停").Width(76).Clicked() {
					s.app.Toggle()
				}
			} else {
				if ui.PrimaryButton(c, "播放").Width(76).Disabled(!st.HasTrack).Clicked() {
					s.app.Toggle()
				}
			}
		})

		s.viewProgress(c, st)

		if st.Err != "" {
			ui.Text(c, "打不开："+st.Err).FontSize(12).TextColor(t.Danger)
		}
	})
}

// viewProgress 是那一行进度。
//
// 进度条拖起来是这样分工的：按住的时候值归用户，松手才 seek，seek 完交还给
// 播放器。不这么分的话，后台时钟每 50ms 写回一次位置，会和拖动打架——
// 表现就是拖不动，或者松手又弹回去。
func (s *Shell) viewProgress(c *ui.Context, st app.State) {
	known := st.Duration > 0

	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, clock(st.Position)).FontSize(12).TextColor(c.Theme().TextMuted).Width(48)

		sl := ui.Slider(c, &s.scrub, 0, 1).Grow(1).Label("播放进度").Disabled(!known)
		// ⚠️ 这里必须用 Pressed()，不能用 Dragging()。
		//
		// Dragging() 报的是「这个控件正在拖一个值 / 正在被拖动」（Drag 与
		// Draggable 两套机制），普通 Slider 只用 flagDraggable 做拖动**源**，
		// 它自己的 dragRec 是 nil，所以 Dragging() 对滑块**永远是 false**
		//（实测：按下滑块移动 200 DIP，Dragging 仍为 false，Pressed 为 true）。
		// MyGo 自带的 examples/timeline 用的就是 Dragging，那一处的判断是失效的。
		switch held := sl.Pressed(); {
		case held && !s.held:
			s.held = true
			s.app.ScrubStart()
		case !held && s.held:
			s.held = false
			s.app.ScrubEnd(s.scrub)
		case !s.held && known:
			// 松手之后进度条跟着播放器走。
			s.scrub = float64(st.Position) / float64(st.Duration)
		}

		total := clock(st.Duration)
		if !known {
			total = "--:--"
		}
		ui.Text(c, total).FontSize(12).TextColor(c.Theme().TextMuted).Width(48)
	})
}

// textColor 是一条曲目名该用的颜色：没歌的时候用弱化的，别让人以为能点。
func textColor(t *ui.Theme, has bool) ui.Color {
	if has {
		return t.Text
	}
	return t.TextMuted
}

// clock 把时长写成 m:ss。
func clock(d time.Duration) string {
	if d <= 0 {
		return "0:00"
	}
	s := int(d.Seconds())
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
