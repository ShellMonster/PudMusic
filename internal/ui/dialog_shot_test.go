// 收藏夹相关三个对话框的画面验收（T3，原型 `openFolderPick` / `openFolderEdit` / `openCrop`）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderFolderDialogShots
//
// 深浅两套；对话框原型没有现成的参考图，对照原型 CSS（`dialog.dlg` / `.dlg-body` /
// `.pick-row` / `.form-grid`）逐项量，见收尾报告。
package ui

import (
	"os"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"
	mygoui "github.com/egoist/mygo/ui"
)

// TestRenderFolderDialogShots 把加入收藏夹、新建收藏夹两个对话框存成 PNG。
func TestRenderFolderDialogShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{true, false} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name+"-pick", func(t *testing.T) {
			f := newFolderBili(t, nil)
			f.pages = [][]map[string]any{folderPage(1, 5)}
			f.lock()
			f.info["media_count"] = 1
			f.unlock()
			s, tt := folderShellDark(t, f, dark)
			s.navigate("/collection/42")
			waitSongs(t, tt, s, 1)
			openSelectFromMenu(t, tt)
			waitFrames(t, tt, "收藏夹到了", func() bool { return s.folderSelect != nil && s.folderSelect.loaded })
			settleFrame(tt)
			tt.Move(5, 5)
			settleFrame(tt)
			save(t, dir, name+"-dialog-pick.png", tt)
		})
		t.Run(name+"-edit", func(t *testing.T) {
			f := newFolderBili(t, nil)
			s, tt := folderShellDark(t, f, dark)
			seedAndSettle(s, tt, 42)
			if err := tt.Click("新建收藏夹"); err != nil {
				t.Fatal(err)
			}
			settleFrame(tt)
			s.folderEdit.title = "深夜循环"
			s.folderEdit.intro = "睡前那一个小时，音量调到刚好听见。"
			tt.Move(5, 5)
			settleFrame(tt)
			save(t, dir, name+"-dialog-edit.png", tt)
		})
	}
}

// folderShellDark 是 `folderShell` 加上系统深浅外观。
func folderShellDark(t *testing.T, f *folderBili, dark bool) (*Shell, *mygoui.Tester) {
	t.Helper()
	s, tt := newShell(t, systemIsDark(dark))
	s.app.SetWake(s.wake.invalidate)
	s.app.MountCollections(app.CollectionRefs{Client: fixtureClient(t, f), Settle: 0})
	return s, tt
}
