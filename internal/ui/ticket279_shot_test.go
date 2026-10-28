package ui

// 票 #279 的出图：**历史记录页第一行误显示「正在播放」**修好之后长什么样。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderTicket279Shots
//
// 这一族要看出的是「**没在放的那一行不再跳**」：
//
//	01 历史记录页，队列的指针指着第一首，而**没有声音** → 第一行是序号「1」，不是波形
//	02 历史记录页，**真在放**第一首 → 第一行换成跳动的波形，歌名是主色
//	03 稍后再看页，同上两档
//
// ⚠️ **出图本身不是判据**：同一份代码两次出图有几十张会不一样（字体 hinting、
// 抗锯齿的随机相位）。判据是 `ticket279_test.go` 里那几条（它们量的是标签和
// 位置），出图是给人看的。

import (
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
)

// shotLibraryPage 造一个停在 path 上的历史/稍后再看页面，并把「哪一首在放」摆好。
//
// ⚠️ **「真在放」是换接线给的**：判据认的是「此刻有声音的那一首」，而这一层
// 放不了在线的歌（在线播放要走真取流）——所以换成一份说「第一首在放」的接线。
// 判据本身由 `internal/app` 那一组量。
func shotLibraryPage(t *testing.T, dir, name string, dark bool, path string, playing bool) {
	t.Helper()
	f := newLibFake(t).
		withLater(1, 4, libTitles...).
		withHistory(1, 30, 185, libTitles...)
	tt, s := libraryShellOpts(t, f, path, systemIsDark(dark))
	waitFrames(t, tt, "把内容拉回来", func() bool {
		return rowsShown(s.pageOf(path)) == len(libTitles)
	})

	if playing {
		items := s.pageOf(path).rows.Rows()
		id := items[0].Item.Identity()
		s.pageOf(path).rows.acts.IsPlaying = func(it app.Item) bool { return it.Identity() == id }
		tt.Frame()
	}
	save(t, dir, name+".png", tt)
}

func TestRenderTicket279Shots(t *testing.T) {
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
			// 01 **没在放**：队列的指针指着第一首，而没有声音 → 第一行不该跳。
			shotLibraryPage(t, dir, "01-历史记录-没在放-"+name, dark, historyPath, false)

			// 02 **真在放**：第一行换成跳动的波形。
			shotLibraryPage(t, dir, "02-历史记录-真在放-"+name, dark, historyPath, true)

			// 03 稍后再看那一页的同一档。
			shotLibraryPage(t, dir, "03-稍后再看-没在放-"+name, dark, laterPath, false)
			shotLibraryPage(t, dir, "04-稍后再看-真在放-"+name, dark, laterPath, true)
		})
	}
}
