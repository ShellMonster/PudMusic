// 合集组在队列抽屉里的画面验收：组头行 + 收起的集行、点开组头放出来。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderQueueGroupShots
package ui

import (
	"os"
	"testing"
)

// TestRenderQueueGroupShots 把「队列里有一个合集」的抽屉存成 PNG：
// 收着一帧（组头 + 散歌）、点开一帧（集行放出来）。
func TestRenderQueueGroupShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	h := newHarness(t)
	dir2 := t.TempDir()
	enqueue(t, h, localSong(t, dir2, "散歌一首.flac"))
	h.app.Enqueue(
		groupSong(t, dir2, "第1集.flac", "season:1", 1, 3),
		groupSong(t, dir2, "第2集.flac", "season:1", 2, 3),
		groupSong(t, dir2, "第3集.flac", "season:1", 3, 3),
	)
	h.app.Enqueue(localSong(t, dir2, "队尾的歌.flac"))
	h.tt.Frame()
	openDrawer(t, h)
	save(t, dir, "queue-group-collapsed.png", h.tt)

	// 点开组头：集行放出来。
	if err := h.tt.Click("「测试合集」"); err != nil {
		t.Fatalf("点不到组头：%v（画面上的字：%q）", err, h.tt.Texts())
	}
	h.tt.Frame()
	save(t, dir, "queue-group-open.png", h.tt)
}
