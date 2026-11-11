// 播放列表抽屉里，合集组的展开状态不该被「播放组内一首」打断。
//
// 症状：组里选一首歌，高亮先移过去，几百毫秒后整组自己收起来、列表塌一截。
// 根因是起播补回 cid 让队列项的身份从 `mv:<bvid>` 变成 `mv:<bvid>|<cid>`，
// 抽屉把这次身份变化当成了「队列形状变了」，清空了展开状态。
//
// ⚠️ 被点的那一行仍会随「已播放 / 正在播放 / 接下来」三句分段标签换行上下
// 挪一句标签的高度。保留这三句标签和现在的行距就避免不了；这一版接受它。
package ui

import (
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/service/bili"
	mygoui "github.com/egoist/mygo/ui"
)

// clickDrawerText 点抽屉里写着 s 的那一块。用坐标而不是 Tester.Click：
// 抽屉行里的文字不是输入目标本身，`Click` 的目标校验会把它判成「点不到」。
func clickDrawerText(t *testing.T, tt *mygoui.Tester, s string) {
	t.Helper()
	r, ok := tt.Find(s)
	if !ok {
		t.Fatalf("抽屉里找不到 %q，文字：%q", s, tt.Texts())
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
}

// openPlayedCollectionGroup 播起一个三集合集的第一集，离开合集页，打开播放
// 列表抽屉并展开合集组。返回时第一集正在放，组是开着的。
func openPlayedCollectionGroup(t *testing.T) (*Shell, *mygoui.Tester) {
	t.Helper()
	_, s, tt := collectionShellTyped(t, bili.CollectionAlbum, "1227671",
		mediaOf(1, "第一集"), mediaOf(2, "第二集"), mediaOf(3, "第三集"))
	waitSongs(t, tt, s, 3)
	mountMultiPartStreaming(t, s.app)

	s.playWholeCollection(s.collection)
	waitFrames(t, tt, "合集第一首放起来", func() bool {
		return s.app.Snapshot().HasTrack
	})
	waitFrames(t, tt, "第一首的分 P id 写回", func() bool {
		it, ok := s.app.CurrentItem()
		return ok && it.Cid != ""
	})

	// 离开合集页再开抽屉：合集页上也画着「第二集」，而 Tester 按文字找的是第一个。
	s.navigate("/")
	tt.Frame()
	s.openQueueDrawer()
	settleDrawer(t, s, tt)

	clickDrawerText(t, tt, "一个合集")
	tt.Frame()
	return s, tt
}

// TestPlayingInsideACollectionGroupKeepsItOpen 盯住「播组内一首之后组还开着」：
// 点开合集、点第二集、等它起播并把 cid 写回，第三集那一行必须还在。
func TestPlayingInsideACollectionGroupKeepsItOpen(t *testing.T) {
	s, tt := openPlayedCollectionGroup(t)
	if !tt.HasText("第三集") {
		t.Fatalf("组展开后没有第三集，文字：%q", tt.Texts())
	}

	clickDrawerText(t, tt, "第二集")
	waitFrames(t, tt, "第二集放起来且分 P id 写回", func() bool {
		it, ok := s.app.CurrentItem()
		return ok && it.Title == "第二集" && it.Cid != ""
	})
	tt.Frame()

	if !tt.HasText("第三集") {
		t.Fatalf("播组内一首后合集被收起了，文字：%q", tt.Texts())
	}
	if it, ok := s.app.CurrentItem(); !ok || it.Title != "第二集" {
		t.Fatalf("当前项 = %+v，want 第二集", it)
	}
}

// TestRenderQueueGroupAfterPlayingShot 出「组内播了一首之后」的图：
// 合集组仍然展开、当前行是点的那一首。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui -run TestRenderQueueGroupAfterPlayingShot
func TestRenderQueueGroupAfterPlayingShot(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	s, tt := openPlayedCollectionGroup(t)
	clickDrawerText(t, tt, "第二集")
	waitFrames(t, tt, "第二集放起来且分 P id 写回", func() bool {
		it, ok := s.app.CurrentItem()
		return ok && it.Title == "第二集" && it.Cid != ""
	})
	tt.Frame()
	save(t, dir, "queue-group-after-playing.png", tt)
}
