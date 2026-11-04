// 404 页的画面验收（T7）。
//
//	BIU_SHOT_DIR=/some/dir go test ./internal/ui/ -run TestRenderNotFoundShots
package ui

import (
	"os"
	"testing"
)

func TestRenderNotFoundShots(t *testing.T) {
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
			s, tt := newShell(t, systemIsDark(dark))
			s.navigate("/根本没有这一页")
			tt.Frame()
			tt.Frame()
			save(t, dir, mode+"-notfound.png", tt)
		})
	}
}
