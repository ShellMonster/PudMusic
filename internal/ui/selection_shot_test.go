// #197 的出图：列表选中行、网格选中格、键盘焦点环，深浅两套。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderSelectionShots
//
// ⚠️ **深浅两套都要出**：这一票改的是「那一行长什么样」，而主色在两套主题下是同一个
// 值——浅色下那一块绿很刺眼，深色下更刺眼，而「没有实心主色块」这件事在两套下
// 长得完全不一样（浅色下是白底上没东西，深色下是深灰底上没东西）。
//
// ⚠️ **图是给人看的，判据在 `selection_test.go` 里**：这里只负责把画面摆出来，免得
// 「像素对了但画面难看得没人要」。
package ui

import (
	"testing"

	mygoui "github.com/egoist/mygo/ui"
)

// shotLocalTable 造一个停在本地音乐「表格」那一档的外壳，并且点过一行。
//
// ⚠️ **本地音乐那一档的表格是框架的 `ui.Table`**：它给选中的行铺主色底的方式和
// 我们自己画的列表不同（问的是「这一行在选择集里吗」），所以它得单独存一张图。
func shotLocalTable(t *testing.T, dark bool) *mygoui.Tester {
	t.Helper()
	dir := t.TempDir()
	libSong(t, dir, "甲.mp3")
	libSong(t, dir, "乙.mp3")
	tt, s, _, _ := shellOnLocalMusicIn(t, dark, dir)
	s.local.style = localStyleTable
	waitFrames(t, tt, "把文件扫回来", func() bool { return len(s.local.rows) >= 2 })
	tt.Frame()
	if err := tt.Click("甲"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	return tt
}
