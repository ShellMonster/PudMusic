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
	"os"
	"testing"

	"github.com/ShellMonster/Biu_Go/internal/app"
	"github.com/egoist/mygo/ui"
	mygoui "github.com/egoist/mygo/ui"
)

// TestRenderSelectionShots 把这一票动的三处各存一张 PNG，深浅各一套。
func TestRenderSelectionShots(t *testing.T) {
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
			// ① 列表：点过一行——那一行**不再有实心主色底**。
			_, tt := shotHomeList(t, dark)
			first := songName(homeTabs[0], 1)
			if err := tt.Click(first); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			save(t, dir, "80-"+name+"-列表-点过的那一行.png", tt)

			// ② 键盘：Tab 到列表，方向键走到第二行——那一行有一圈**焦点环**。
			_, tt2 := shotHomeList(t, dark)
			focusList(t, tt2, first)
			tt2.Key(0, ui.KeyDown)
			tt2.Frame()
			tt2.Key(0, ui.KeyDown)
			tt2.Frame()
			save(t, dir, "81-"+name+"-列表-键盘焦点环.png", tt2)

			// ③ 网格：点过一格——那一格**不再有实心主色底**。
			_, tt3 := shotHomeGrid(t, dark)
			card := songName(homeTabs[0], 3)
			if err := tt3.Click(card); err != nil {
				t.Fatal(err)
			}
			tt3.Frame()
			save(t, dir, "82-"+name+"-网格-点过的那一格.png", tt3)

			// ④ 鼠标点过一行之后再按方向键：环**要回到键盘走到的那一行**上
			//（框架那个「焦点从键盘来」的标志方向键不会设回来，靠它那一路是哑的）。
			_, tt4 := shotHomeList(t, dark)
			if err := tt4.Click(songName(homeTabs[0], 1)); err != nil {
				t.Fatal(err)
			}
			tt4.Frame()
			tt4.Key(0, ui.KeyDown)
			tt4.Frame()
			save(t, dir, "83-"+name+"-列表-鼠标点过之后按方向键.png", tt4)

			// ⑤ 本地音乐的「表格」那一档：点一行同样**不再有实心主色底**
			// （框架的 `ui.Table` 铺主色底的方式和列表那一档不同）。
			tt5 := shotLocalTable(t, dark)
			save(t, dir, "84-"+name+"-本地音乐表格-点过的那一行.png", tt5)
		})
	}
}

// shotLocalTable 造一个停在本地音乐「表格」那一档的外壳，并且点过一行。
//
// ⚠️ **本地音乐那一档的表格是框架的 `ui.Table`**：它给选中的行铺主色底的方式和
// 我们自己画的列表不同（问的是「这一行在选择集里吗」），所以它得单独存一张图。
func shotLocalTable(t *testing.T, dark bool) *mygoui.Tester {
	t.Helper()
	dir := t.TempDir()
	libSong(t, dir, "甲.mp3")
	libSong(t, dir, "乙.mp3")
	tt, s, _, _ := shellOnLocalMusic(t, dir)
	s.local.style = localStyleTable
	waitFrames(t, tt, "把文件扫回来", func() bool { return len(s.local.rows) >= 2 })
	tt.Frame()
	if err := tt.Click("甲"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	return tt
}

// shotHomeGrid 造一个装着首页推荐音乐**网格**那一档的外壳。
func shotHomeGrid(t *testing.T, dark bool) (*Shell, *ui.Tester) {
	t.Helper()
	st := app.DefaultSettings("")
	st.DisplayMode = app.DisplayModeCard
	b := newTestShell(t, systemIsDark(dark), withSettings(st))
	s := b.s
	s.home.SetSongs(fullSongs())
	homeFrame(b.tt)
	return s, b.tt
}
