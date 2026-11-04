// 本地音乐页的画面验收（T7）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderLocalMusicPageShots
package ui

import (
	"os"
	"testing"
)

func TestRenderLocalMusicPageShots(t *testing.T) {
	dir := os.Getenv("BIU_SHOT_DIR")
	if dir == "" {
		t.Skip("设 BIU_SHOT_DIR 才会存图")
	}
	for _, dark := range []bool{true, false} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			for _, n := range []string{"夜航星.flac", "富士山下.mp3", "十年.m4a", "浮夸.wav"} {
				libSong(t, root, n)
			}
			tt, _, a, _ := shellOnLocalMusicIn(t, dark, root)
			openLocal(t, tt)
			settleLocal(t, a, tt)
			for i := 0; i < 5; i++ {
				tt.Frame()
			}
			save(t, dir, mode+"-local.png", tt)
		})
	}
}
