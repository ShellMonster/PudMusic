// 合集组在队列抽屉里的画面验收：组头行 + 收起的集行、点开组头放出来。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderQueueGroupShots
package ui

import (
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
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

// TestRenderCollectionRowEnqueueGroupShot 走**真实行级入队**那一路：
// 合集页（`collectionShellTyped`）→ `collectionActions.Add`（打标那一层）→
// 开抽屉出图——和手动 `Enqueue` 造组标的区别就是「标是行级入口打的」。
func TestRenderCollectionRowEnqueueGroupShot(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	_, s, tt := collectionShellTyped(t, bili.CollectionAlbum, "1227671",
		mediaOf(1, "第一集"), mediaOf(2, "第二集"), mediaOf(3, "第三集"))
	waitSongs(t, tt, s, 3)

	acts := s.collectionActions(s.collection)
	for row := 0; row < 3; row++ {
		m, ok := s.collection.list.List().At(row)
		if !ok {
			t.Fatalf("合集页第 %d 行不在", row+1)
		}
		acts.Add([]app.Item{m.Item})
	}
	tt.Frame()

	s.openQueueDrawer()
	settleDrawer(t, s, tt)
	save(t, dir, "queue-group-row-enqueue.png", tt)
}
