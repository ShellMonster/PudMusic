package ui

import (
	"fmt"
	"time"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/ShellMonster/Biu_Go/internal/platform"
	"github.com/egoist/mygo/ui"
)

// viewPlayBar 是底部那条播放条：曲目名、上一首/播放/下一首、可拖的进度条、音量。
//
// 阶段 1 只有这些。播放列表面板、歌词、封面缓存都在后面的阶段。
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

			if ui.Button(c, "加歌").Width(56).Clicked() {
				s.pickAndEnqueue()
			}
			s.viewStepButtons(c)

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

// viewStepButtons 是上一首和下一首。
//
// 能不能按看的是队列：没放过歌、或者只有刚打开的那一首（没进队列）时是灰的。
// 队列里只有一首歌时按钮仍然是亮的——按下去听到的是同一首从头来，
// 表现成按了没反应更让人困惑。
func (s *Shell) viewStepButtons(c *ui.Context) {
	// 宽度要放得下三个汉字：按 64 画出来是「上...」「下...」，看着像坏了。
	if ui.Button(c, "上一首").Width(88).Disabled(!s.app.CanPrev()).Clicked() {
		s.app.Prev()
	}
	if ui.Button(c, "下一首").Width(88).Disabled(!s.app.CanNext()).Clicked() {
		s.app.Next()
	}
}

// pickAndEnqueue 弹文件框，把选中的歌加到队列尾部。
//
// 队列空的时候加歌会直接开始播，所以点一下就能听见声音；之后再加就只是排着。
//
// 这是**唯一**一个弹文件框的入口：播放条的「加歌」和导航栏的「打开文件」
// 都调它。两个入口曾经各写一份，一个只取 paths[0]、还绕过队列直接 app.Open，
// 结果用它打开的歌上一首/下一首永远是灰的。现在只剩这一条路。
//
// ⚠️ 文件框是多选的，选回来的每一条都得进队列——只取 paths[0] 会把用户其余选
// 好的歌静默丢掉，用户在界面上看不出自己选漏了。
//
// ⚠️ 必须放在另一个 goroutine 里：MyGo 把弹框请求 post 到主线程再等系统回调，
// 在主线程里等就是死等，事件循环会卡死。
func (s *Shell) pickAndEnqueue() {
	win := s.win
	go func() {
		paths, err := platform.PickAudioFile(win)
		if err != nil || len(paths) == 0 {
			return
		}
		s.enqueuePicked(paths)
	}()
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
