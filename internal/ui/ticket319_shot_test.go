package ui

// 「暂停」与「停止」分得开 + 「这一串放完了」说出来（票 #319）的出图：深浅各一套。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket319Shots
//
// 这一族要看出的是「**这两档和别的档不一样**」：
//
//	播放条上「已停止」那一行 vs「这一串放完了」那一行
//	队列抽屉里「停住了」的那一行 vs「正在播放」的那一行
//
// ⚠️ **出图本身不是判据**：同一份代码两次出图有几十张会不一样（字体 hinting、
// 抗锯齿的随机相位）。判据是 `ticket319_test.go` 里那几条（它们量的是文字和
// 底色），出图是给人看的。

import (
	"os"
	"testing"
	"time"

	"github.com/ShellMonster/PudMusic/internal/app"
)

func TestRenderTicket319Shots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			// 01 **已停止**：用户按了停止（队列里还有歌）。
			stopped := playBarTester(t, app.State{
				HasTrack: true, Title: "一首歌", Source: app.SourceOnline,
				Duration: 3 * time.Minute, Position: 0,
				Rate: 1, Volume: 0.8, PlayMode: app.PlayModeSequence,
				Stopped: true,
			}, 1200, dark)
			save(t, dir, "01-播放条-已停止-"+name+".png", stopped)

			// 02 **这一串放完了**：顺序播放走到队尾（没有下一首了）。
			drained := playBarTester(t, app.State{
				HasTrack: true, Title: "最后一首", Source: app.SourceOnline,
				Duration: 3 * time.Minute, Position: 0,
				Rate: 1, Volume: 0.8, PlayMode: app.PlayModeSequence,
				Stopped: true, QueueFinished: true,
			}, 1200, dark)
			save(t, dir, "02-播放条-这一串放完了-"+name+".png", drained)

			// 03 **暂停**：和上面两档要分得开——**这一行是空的**。
			paused := playBarTester(t, app.State{
				HasTrack: true, Title: "一首歌", Source: app.SourceOnline,
				Duration: 3 * time.Minute, Position: 42 * time.Second,
				Rate: 1, Volume: 0.8, PlayMode: app.PlayModeSequence,
			}, 1200, dark)
			save(t, dir, "03-播放条-暂停-"+name+".png", paused)
		})
	}
}

// TestRenderTicket319DrawerShots 是抽屉那一半：停住了的那一行不再高亮成
// 「正在播放」，而且副题写的是「已停止」。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket319DrawerShots
func TestRenderTicket319DrawerShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{false, true} {
		name := "浅色"
		if dark {
			name = "深色"
		}
		t.Run(name, func(t *testing.T) {
			// 01 **停住了**：当前那一行不高亮，副题是「已停止」。
			tt, s := queueDrawerTester(t, dark)
			s.app.Stop()
			tt.Frame()
			save(t, dir, "01-队列抽屉-停住了-"+name+".png", tt)

			// 02 **正在放**：对照——那一行是淡主色底，副题是来源。
			playing, _ := queueDrawerTester(t, dark)
			save(t, dir, "02-队列抽屉-正在放-"+name+".png", playing)
		})
	}
}
