package ui

// 本地音乐「删文件」那道确认框的出图（票 #558）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderLocalMusicTrashShots
//
// ⚠️ **深浅两套都要出**：这一票改的是弹窗上**那一行正文**，而它是不是说得清
// 要看它在深浅两套底色上读不读得出来。
//
// ⚠️ 这一族**不删任何东西**：它只把弹窗**打开**（不按确认），而弹窗开着的时候
// 文件在盘上——所以这一族连假回收站都不需要（真正的那一步在别的用例里）。

import (
	"os"
	"testing"
)

func TestRenderLocalMusicTrashShots(t *testing.T) {
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
			root := t.TempDir()
			libSong(t, root, "周杰伦 - 晴天.mp3")
			libSong(t, root, "五月天 - 倔强.mp3")
			tt, _, _, _, _, _ := shellOnLocalMusicWithTrash(t, dark, root)

			if err := tt.Click(deleteRowLabel); err != nil {
				t.Fatalf("点不到行里的「%s」：%v，文字有 %q", deleteRowLabel, err, tt.Texts())
			}
			tt.Frame()

			save(t, dir, "本地音乐-删除文件确认框-"+name+".png", tt)
		})
	}
}
